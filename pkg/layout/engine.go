package layout

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
	urlpkg "louis14/pkg/url"
)

// LayoutEngine performs CSS layout, producing a tree of positioned Box fragments.
type LayoutEngine struct {
	viewport        viewport
	imageFetcher    images.ImageFetcher
	documentFetcher DocumentFetcher
	fontConfig      text.FontConfig
	scrollY         float64

	lastStyles    map[*html.Node]*css.Style
	lastNodeBoxes map[*html.Node]*Box

	// prevStyles holds the computed styles of the PREVIOUS layout pass
	// (before script-driven DOM mutation) — the "before-change styles" CSS
	// Transitions compare against. Set by the render pipelines on the
	// second (post-script) pass via SetPreviousStyles; nil on a first
	// pass, in which case no transitions start. See
	// css.ApplyTransitionsFirstFrame for the Blink citations.
	prevStyles map[*html.Node]*css.Style
}

// SetPreviousStyles supplies the previous pass's computed styles so the
// next Layout call can start CSS transitions (first-frame semantics).
func (le *LayoutEngine) SetPreviousStyles(styles map[*html.Node]*css.Style) {
	le.prevStyles = styles
}

type viewport struct {
	width  float64
	height float64
}

// NewLayoutEngine creates a new layout engine with the given viewport dimensions.
func NewLayoutEngine(viewportWidth, viewportHeight float64) *LayoutEngine {
	return &LayoutEngine{
		viewport: viewport{width: viewportWidth, height: viewportHeight},
	}
}

// SetImageFetcher sets the image fetcher for loading network images during layout.
func (le *LayoutEngine) SetImageFetcher(fetcher images.ImageFetcher) {
	le.imageFetcher = fetcher
}

// SetDocumentFetcher sets the document fetcher for loading nested documents
// (iframe, object) during layout.
func (le *LayoutEngine) SetDocumentFetcher(fetcher DocumentFetcher) {
	le.documentFetcher = fetcher
}

// SetFontConfig sets the font configuration for text measurement during layout.
// This should include any @font-face fonts loaded from the document.
func (le *LayoutEngine) SetFontConfig(fc text.FontConfig) {
	le.fontConfig = fc
}

// SetScrollY sets the vertical scroll offset for fixed positioning.
func (le *LayoutEngine) SetScrollY(scrollY float64) {
	le.scrollY = scrollY
}

// GetScrollY returns the current vertical scroll offset.
func (le *LayoutEngine) GetScrollY() float64 {
	return le.scrollY
}

// Layout performs CSS layout on the document and returns a tree of positioned boxes.
func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
	if doc == nil || doc.Root == nil {
		return []*Box{{Width: le.viewport.width, Height: le.viewport.height}}
	}

	// Phase 1: Compute styles. The cascade builds each element's font from
	// the font selector as it goes (Blink's StyleResolverState::UpdateFont),
	// so font metrics and font-relative units are final when it returns.
	fontConfig := le.fontConfig
	if fontConfig.Regular == "" {
		fontConfig = text.DefaultFontConfig()
	}
	computedStyles := computeDocumentStyles(doc, le.viewport.width, le.viewport.height, le.imageFetcher, text.NewCSSFontSelector(fontConfig))
	// CSS Transitions (first-frame): when this is a post-script re-layout,
	// compare against the previous pass's styles and override transitioned
	// properties with their value at local time 0. Runs AFTER logical-
	// property resolution — prevStyles came from a pass that already
	// resolved logical keys to physical, so both maps must be physical-
	// keyed for the per-property diff (and resolution must not clobber a
	// transitioned value). ApplyTransitionsFirstFrame re-builds the font of
	// every style it rewrites; the text-decoration insets the cascade already
	// recorded are then re-resolved against the transitioned values.
	if le.prevStyles != nil {
		css.ApplyTransitionsFirstFrame(doc.Root, computedStyles, le.prevStyles)
		recomputeTextDecorationInsets(computedStyles)
	}

	// Phase 2: Build layout context.
	ctx := &LayoutContext{
		ViewportWidth:   le.viewport.width,
		ViewportHeight:  le.viewport.height,
		BaseDir:         doc.BaseDir,
		ImageFetcher:    le.imageFetcher,
		DocumentFetcher: le.documentFetcher,
		FontConfig:      fontConfig,
		ComputedStyles:  computedStyles,
	}

	// Phase 3: Find the root element (skip document-level wrapper nodes).
	rootElement := findRootElement(doc.Root)
	if rootElement == nil {
		return []*Box{{Width: le.viewport.width, Height: le.viewport.height}}
	}

	// Phase 4: Build the layout tree from the DOM.
	// Parse stylesheets for pseudo-element generation (::before, ::after).
	stylesheets := css.ParseDocumentStylesheets(doc)
	treeBuilder := &LayoutTreeBuilder{
		styles:         computedStyles,
		stylesheets:    stylesheets,
		viewportWidth:  le.viewport.width,
		viewportHeight: le.viewport.height,
		fontConfig:     fontConfig,
	}
	layoutRoot := treeBuilder.BuildLayoutTree(rootElement)

	// Phase 5: Build initial constraint space for the root.
	rootStyle := layoutRoot.Style()
	rootWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	if rootStyle != nil {
		rootWDM = NewWritingDirectionMode(rootStyle)
	}

	rootSpace, rootPositioned := buildRootConstraintSpace(
		rootStyle, rootWDM, le.viewport.width, le.viewport.height,
	)

	// Phase 6: Run layout.
	result := layoutElement(ctx, layoutRoot, rootSpace)

	// Phase 7: Convert fragment tree to box tree.
	// When the root is position:absolute/fixed with resolved insets, place it
	// per the IMCB math against the ICB. Otherwise, apply the vertical-rl
	// right-anchoring offset used for shrink-wrapped vertical roots.
	var rootOffsetX, rootOffsetY float64
	if rootPositioned {
		rootOffsetX, rootOffsetY = resolvePositionedRootOffset(
			rootStyle, rootWDM, le.viewport.width, le.viewport.height, result.Fragment,
		)
	} else if rootWDM.IsFlippedBlocks() {
		rootWidth := result.Fragment.Size.WidthF64()
		if rootWidth < le.viewport.width {
			rootOffsetX = le.viewport.width - rootWidth
		}
	}
	// A position:relative root is shifted by the LayoutView when it adds the
	// root against the initial containing block, as any parent does
	// (BoxFragmentBuilder::AddChild, box_fragment_builder.cc:255-267 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	rootRelative := NewConverter(rootWDM, PhysicalSize{}).ToPhysicalOffset(
		ComputeRelativeOffsetForBoxFragment(result.Fragment, rootWDM, geomLogicalToOld(rootSpace.AvailableSize)), PhysicalSize{})
	rootOffsetX += rootRelative.X
	rootOffsetY += rootRelative.Y
	rootBox := fragmentToBox(result.Fragment, nil, rootOffsetX, rootOffsetY)

	// Phase 8: Apply sticky offsets. Louis14 is a static renderer (scroll=0);
	// sticky elements that are above their top/left inset threshold are pushed
	// into the viewport. Mirrors Blink's StickyPositionScrollingConstraints
	// computation, applied post-layout because absolute positions are not known
	// until the full box tree is built.
	applyStickyOffsets(rootBox, le.viewport.width, le.viewport.height)

	le.lastStyles = computedStyles
	le.lastNodeBoxes = buildNodeBoxIndex([]*Box{rootBox})

	return []*Box{rootBox}
}

// ComputedStyles returns the per-node computed style map produced by the most
// recent Layout() call. Used by JS bindings to back getComputedStyle().
func (le *LayoutEngine) ComputedStyles() map[*html.Node]*css.Style {
	return le.lastStyles
}

// NodeBoxes returns a node→principal-box index from the most recent Layout()
// call. Used by JS bindings to back getComputedStyle() for layout-dependent
// used values (width, margins, padding, borders).
func (le *LayoutEngine) NodeBoxes() map[*html.Node]*Box {
	return le.lastNodeBoxes
}

// buildNodeBoxIndex walks the box tree and returns a map from DOM node to the
// first (principal) box produced by that node. Anonymous boxes and line boxes
// are skipped. When an element produces multiple fragments (split inlines,
// continuations), the first one encountered wins.
func buildNodeBoxIndex(boxes []*Box) map[*html.Node]*Box {
	idx := make(map[*html.Node]*Box)
	var walk func(*Box)
	walk = func(b *Box) {
		if b == nil {
			return
		}
		if b.Node != nil {
			if _, ok := idx[b.Node]; !ok {
				idx[b.Node] = b
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range boxes {
		walk(b)
	}
	return idx
}

// NestedDocumentResult wraps the root layout result for a nested document
// together with the physical X offset of the root element within the viewport.
// For vertical-rl roots, rootOffsetX = vpWidth - root.Width (right-anchored).
// For all other writing modes, rootOffsetX = 0.
type NestedDocumentResult struct {
	Result      *LayoutResult
	RootOffsetX float64
	// Doc is the parsed *html.Document for the nested document. Retained so
	// callers (e.g. iframe layout) can expose it via iframe.contentDocument.
	Doc *html.Document
}

// openFrameOwnerURL selects and fetches the document an <iframe> or <object>
// embeds, and returns its HTML and its base (the Document::BaseURL analog).
//
//   - <iframe srcdoc>: the srcdoc text; its base is the container
//     document's base, as Document::FallbackBaseURL returns
//     same_origin_parent->BaseURL() for a srcdoc document
//     (third_party/blink/renderer/core/dom/document.cc:5046, :5063).
//   - <iframe src> / <object data> (type "" or text/html): the URL is
//     completed against the embedding document's base before it is
//     fetched, as HTMLFrameElementBase::OpenURL does with
//     GetDocument().CompleteURL(url_)
//     (third_party/blink/renderer/core/html/html_frame_element_base.cc:70)
//     and HTMLPlugInElement::RequestObject with
//     GetDocument().CompleteURL(url_)
//     (third_party/blink/renderer/core/html/html_plugin_element.cc:702).
//     The base is the completed URL's directory, so each nesting level
//     composes onto its parent's base.
//
// Citations @ d4ecdfed88f962439247c2ad36b8fe47805b1520. ok is false when
// owner embeds no HTML document or the fetch fails.
func openFrameOwnerURL(ctx *LayoutContext, owner *html.Node) (htmlContent, baseDir string, ok bool) {
	var raw string
	switch owner.TagName {
	case "iframe":
		// srcdoc takes priority over src per HTML spec.
		if srcdoc, has := owner.GetAttribute("srcdoc"); has && srcdoc != "" {
			return srcdoc, ctx.BaseDir, true
		}
		raw, _ = owner.GetAttribute("src")
	case "object":
		if dataType, _ := owner.GetAttribute("type"); dataType == "text/html" || dataType == "" {
			raw, _ = owner.GetAttribute("data")
		}
	default:
		return "", "", false
	}

	uri := urlpkg.CompleteURL(raw, ctx.BaseDir)
	if uri == "" || ctx.DocumentFetcher == nil {
		return "", "", false
	}
	htmlContent, err := ctx.DocumentFetcher(uri)
	if err != nil {
		return "", "", false
	}
	baseDir = urlpkg.URLDir(uri)
	if baseDir == "." {
		baseDir = ""
	}
	return htmlContent, baseDir, true
}

// layoutFrameOwnerDocument lays out the document openFrameOwnerURL returned
// in a vpW x vpH viewport, retains it on owner for iframe.contentDocument,
// and returns the root fragment with its X offset, or nil.
func layoutFrameOwnerDocument(ctx *LayoutContext, owner *html.Node, htmlContent, baseDir string, vpW, vpH float64) *nestedDocFragment {
	res := layoutNestedDocument(ctx, htmlContent, vpW, vpH, baseDir)
	if res == nil {
		return nil
	}
	if res.Doc != nil {
		owner.NestedDocument = res.Doc
	}
	return &nestedDocFragment{fragment: res.Result.Fragment, rootOffsetX: res.RootOffsetX}
}

// layoutNestedDocument parses and lays out a nested HTML document (for
// iframe/object) using the given physical dimensions as the viewport.
// baseDir is the nested document's base, the analog of Document::BaseURL
// (third_party/blink/renderer/core/dom/document.cc:5083 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), computed by
// openFrameOwnerURL. Every relative URL in the document is completed
// against it where it is used (Document::CompleteURL, document.cc:7553), so
// the fetchers stay document-agnostic. Returns the root layout result, or
// nil if parsing/layout fails.
func layoutNestedDocument(ctx *LayoutContext, htmlContent string, vpWidth, vpHeight float64, baseDir string) *NestedDocumentResult {
	doc, err := html.Parse(htmlContent)
	if err != nil {
		return nil
	}
	doc.BaseDir = baseDir

	computedStyles := computeDocumentStyles(doc, vpWidth, vpHeight, ctx.ImageFetcher, text.NewCSSFontSelector(ctx.FontConfig))

	rootElement := findRootElement(doc.Root)
	if rootElement == nil {
		return nil
	}

	stylesheets := css.ParseDocumentStylesheets(doc)
	treeBuilder := &LayoutTreeBuilder{
		styles:         computedStyles,
		stylesheets:    stylesheets,
		viewportWidth:  vpWidth,
		viewportHeight: vpHeight,
		fontConfig:     ctx.FontConfig,
	}
	layoutRoot := treeBuilder.BuildLayoutTree(rootElement)

	// Build the nested context: inherit fonts and the document-agnostic
	// fetchers; relative URLs reach them already completed against
	// doc.BaseDir.
	nestedCtx := &LayoutContext{
		ViewportWidth:   vpWidth,
		ViewportHeight:  vpHeight,
		BaseDir:         doc.BaseDir,
		ImageFetcher:    ctx.ImageFetcher,
		DocumentFetcher: ctx.DocumentFetcher,
		FontConfig:      ctx.FontConfig,
	}

	rootStyle := layoutRoot.Style()
	rootWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	if rootStyle != nil {
		rootWDM = NewWritingDirectionMode(rootStyle)
	}

	rootSpace, rootPositioned := buildRootConstraintSpace(rootStyle, rootWDM, vpWidth, vpHeight)

	result := layoutElement(nestedCtx, layoutRoot, rootSpace)
	if result == nil || result.Fragment == nil {
		return nil
	}

	// Compute the physical X offset for the root element within the viewport.
	// For a positioned root with resolved insets, use IMCB-based placement.
	// Otherwise, anchor vertical-rl/sideways-rl shrink-wrapped roots to the
	// right edge (block-start in those writing modes).
	var rootOffsetX float64
	if rootPositioned {
		rx, _ := resolvePositionedRootOffset(rootStyle, rootWDM, vpWidth, vpHeight, result.Fragment)
		rootOffsetX = rx
	} else if rootWDM.WM == WritingModeVerticalRL || rootWDM.WM == WritingModeSidewaysRL {
		marginRight := 0.0
		if result.Fragment.BoxData != nil {
			marginRight = result.Fragment.BoxData.Margin.Right
		}
		rootOffsetX = vpWidth - result.Fragment.Size.WidthF64() - marginRight
		if rootOffsetX < 0 {
			rootOffsetX = 0
		}
	}

	return &NestedDocumentResult{Result: result, RootOffsetX: rootOffsetX, Doc: doc}
}

// computeDocumentStyles is the style phase shared by the top-level and
// nested-document layouts: it decides each <img>'s layout disposition, runs
// the cascade (building fonts from fontSelector), resolves logical
// properties, and applies the fallback-content host style callback. The disposition must be known before the cascade
// because it changes replaced-ness (text-decoration propagation) and the
// host's style, as in Blink where SetLayoutDisposition triggers a style
// recalc (html_image_element.cc:1165-1170 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). The host callback runs after
// logical-property resolution so it sees the final width/height, as
// Blink's AdjustHostStyle does.
func computeDocumentStyles(doc *html.Document, vpWidth, vpHeight float64, fetcher images.ImageFetcher, fontSelector css.FontSelector) map[*html.Node]*css.Style {
	resetImageLayoutDispositions(doc.Root, doc.BaseDir, fetcher)
	styles := css.ApplyStylesToDocumentWithFontSelector(doc, vpWidth, vpHeight, fontSelector)
	css.ResolveLogicalPropertiesInTree(doc.Root, styles)
	css.AdjustImageFallbackHostStylesInTree(doc.Root, styles)
	return styles
}

// resetImageLayoutDispositions sets every <img> under root to primary
// content when its image is potentially available and to fallback content
// otherwise. Mirrors HTMLImageElement::ResetLayoutDisposition
// (html_image_element.cc:1066-1088 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): EnsurePrimaryContent when
// ImageLoader::ImageIsPotentiallyAvailable, else
// EnsureCollapsedOrFallbackContent (kCollapsed is not ported). louis14 lays
// out statically, so the disposition is recomputed on every layout pass
// rather than on load completion; the pass is idempotent. src is completed
// against baseDir before the availability check, as
// ImageLoader::ImageSourceToKURL does (image_loader.cc:746 @ the same SHA).
func resetImageLayoutDispositions(root *html.Node, baseDir string, fetcher images.ImageFetcher) {
	if root.Type == html.ElementNode && root.TagName == "img" {
		src, _ := root.GetAttribute("src")
		if images.ImageIsPotentiallyAvailable(urlpkg.CompleteURL(src, baseDir), fetcher) {
			root.LayoutDisposition = html.LayoutDispositionPrimaryContent
		} else {
			root.LayoutDisposition = html.LayoutDispositionFallbackContent
		}
	}
	for _, c := range root.Children {
		resetImageLayoutDispositions(c, baseDir, fetcher)
	}
}

// nonLayoutTags are elements that never participate in layout as root content.
var nonLayoutTags = map[string]bool{
	"link": true, "meta": true, "style": true, "script": true,
	"title": true, "head": true, "base": true, "noscript": true,
}

// findRootElement finds the root layout element in the document tree.
// Prefers <html>, then <body>, then falls back to the first renderable element.
// Skips elements that never participate in layout (link, meta, style, etc.).
func findRootElement(node *html.Node) *html.Node {
	if node.Type == html.ElementNode && node.TagName != "document" && !nonLayoutTags[node.TagName] {
		return node
	}
	// First pass: look for <html>.
	for _, child := range node.Children {
		if child.Type == html.ElementNode && child.TagName == "html" {
			return child
		}
	}
	// Second pass: look for <body>.
	for _, child := range node.Children {
		if child.Type == html.ElementNode && child.TagName == "body" {
			return child
		}
	}
	// Third pass: look for any renderable element child (skip non-layout tags).
	for _, child := range node.Children {
		if child.Type == html.ElementNode && !nonLayoutTags[child.TagName] {
			return child
		}
	}
	// Fall back: first element child recursively, still skipping non-layout tags.
	for _, child := range node.Children {
		if child.Type == html.ElementNode && !nonLayoutTags[child.TagName] {
			if found := findRootElement(child); found != nil {
				return found
			}
		}
	}
	return nil
}

// fragmentToBox converts a PhysicalFragment tree into the Box tree
// expected by the renderer. absX/absY are the absolute position of
// this fragment's border-box top-left corner.
func fragmentToBox(frag *PhysicalFragment, parent *Box, absX, absY float64) *Box {
	box := &Box{
		Node:                            frag.Node,
		Style:                           frag.Style,
		X:                               absX,
		Y:                               absY,
		Width:                           frag.Size.WidthF64(),
		Height:                          frag.Size.HeightF64(),
		Parent:                          parent,
		ClipContentToBorderBox:          frag.ClipContentToBorderBox,
		GapGeometry:                     frag.GapGeometry,
		IsColumnBox:                     frag.IsColumnBox(),
		IsMulticolContainer:             frag.IsMulticolContainer,
		AppliedTextDecorations:          frag.AppliedTextDecorations,
		CollapsedBorderOutwardExtension: frag.CollapsedBorderOutwardExtension,
		PaintGroup:                      frag.PaintGroup,
	}

	// Text fragments carry their rendered text content.
	if frag.Type == FragmentText {
		text := frag.TextContent
		// UAX#9 L4: RTL runs (odd bidi level) must be drawn in visual order.
		// The fragment text is in logical (Unicode) order; reverse rune order
		// so the renderer draws characters left-to-right in visual order.
		if frag.BidiLevel%2 == 1 {
			text = reverseAndMirrorRunes(text)
		}
		box.Text = text
		box.IsVerticalText = frag.WritingDirection.IsVertical()
		box.IsSidewaysLR = frag.WritingDirection.WM == WritingModeSidewaysLR
		box.IsSidewaysRL = frag.WritingDirection.WM == WritingModeSidewaysRL
		// CSS Writing Modes §5.1: in vertical-rl/lr with text-orientation: mixed
		// (the default), Latin/etc. characters are rendered sideways (90° CW).
		// Only text-orientation: upright uses the vertical stacking path.
		if box.IsVerticalText && !box.IsSidewaysRL && !box.IsSidewaysLR {
			to := ""
			if frag.Style != nil {
				to, _ = frag.Style.Get("text-orientation")
			}
			if to != "upright" {
				box.IsSidewaysRL = true
				// Track when the original CSS writing-mode was vertical-lr so
				// decoration painters can place the underline on the correct side.
				// Both vertical-rl and vertical-lr with mixed text-orientation end
				// up with IsSidewaysRL=true; IsWritingModeVerticalLR disambiguates.
				box.IsWritingModeVerticalLR = frag.WritingDirection.WM == WritingModeVerticalLR
			}
		}
	}

	// Apply box model edges if present.
	if frag.BoxData != nil {
		box.Margin = css.BoxEdge{
			Top: frag.BoxData.Margin.Top, Right: frag.BoxData.Margin.Right,
			Bottom: frag.BoxData.Margin.Bottom, Left: frag.BoxData.Margin.Left,
		}
		box.Border = css.BoxEdge{
			Top: frag.BoxData.Border.Top, Right: frag.BoxData.Border.Right,
			Bottom: frag.BoxData.Border.Bottom, Left: frag.BoxData.Border.Left,
		}
		box.Padding = css.BoxEdge{
			Top: frag.BoxData.Padding.Top, Right: frag.BoxData.Padding.Right,
			Bottom: frag.BoxData.Padding.Bottom, Left: frag.BoxData.Padding.Left,
		}
		box.Scrollbar = css.BoxEdge{
			Top: frag.BoxData.Scrollbar.Top, Right: frag.BoxData.Scrollbar.Right,
			Bottom: frag.BoxData.Scrollbar.Bottom, Left: frag.BoxData.Scrollbar.Left,
		}
	}

	// CSS position applies to boxes (elements), never to text runs.
	// In Blink, NGPhysicalTextFragment does not carry a position property —
	// only NGPhysicalBoxFragment does. Text fragments inherit their parent's
	// complete style (including position: relative), but must not be classified
	// as positioned elements in the paint layer, or they corrupt paint order.
	if box.Style != nil && frag.Type != FragmentText {
		box.Position = box.Style.GetPosition()
		if box.Style.HasExplicitZIndex() {
			box.ZIndex = box.Style.GetZIndex()
		}
	}

	// Connect LayoutInputNode ↔ Box bidirectional link for DOM-ordered paint.
	// Only for element nodes: text nodes can produce multiple fragments (split
	// across lines), and anonymous boxes don't have a stable identity.
	if lin := frag.LayoutNode; lin != nil && !lin.IsText() && !lin.IsAnonymous() {
		box.LayoutNode = lin
		box.DOMIndex = lin.DOMIndex
		lin.Box = box
		// Propagate ::marker style and resolved content from layout input to box.
	}

	// Content area origin = border-box + border + padding.
	contentX := absX + box.Border.Left + box.Padding.Left
	contentY := absY + box.Border.Top + box.Padding.Top

	// Convert children. Child offsets are relative to the content area.
	for _, childLink := range frag.Children {
		childAbsX := contentX + childLink.Offset.LeftF64()
		childAbsY := contentY + childLink.Offset.TopF64()
		childBox := fragmentToBox(childLink.Fragment, box, childAbsX, childAbsY)
		box.Children = append(box.Children, childBox)
	}

	return box
}

// stickyScrollport holds the scrollport rect for a sticky scroll container.
// At scroll=0, the scrollport top-left is (0,0) for any scroll container.
type stickyScrollport struct {
	x, y, width, height float64
}

// isScrollContainer returns true if the box establishes a scroll container,
// i.e., has overflow: auto or overflow: scroll on either axis.
// Per CSS Overflow 3, only auto/scroll create scroll containers; hidden/clip do not.
func isScrollContainer(box *Box) bool {
	if box.Style == nil {
		return false
	}
	isScroll := func(o css.OverflowType) bool {
		return o == css.OverflowAuto || o == css.OverflowScroll
	}
	return isScroll(box.Style.GetOverflowX()) || isScroll(box.Style.GetOverflowY())
}

// stickyContainingBlock returns the bounds of the element's sticky containing
// block — the nearest block ancestor that is not the scroll container.
// Per CSS Position 3 §6.3, the sticky containing block is the padding-box of
// the nearest block-level proper ancestor of the sticky element (not the scroll
// container itself). We approximate this as the parent box's content area.
func stickyContainingBlock(box *Box) (x, y, width, height float64) {
	p := box.Parent
	if p == nil {
		return 0, 0, 0, 0
	}
	// CB is the parent's padding-box (border-box minus border).
	cbX := p.X + p.Border.Left
	cbY := p.Y + p.Border.Top
	cbW := p.Width - p.Border.Left - p.Border.Right
	cbH := p.Height - p.Border.Top - p.Border.Bottom
	return cbX, cbY, cbW, cbH
}

// computeStickyOffset computes the sticky push delta (dx, dy) for a sticky box
// within its scroll container's scrollport. At scroll=0, the scrollport's
// top-left is at (spX, spY) in document coordinates.
//
// Mirrors Blink's StickyPositionScrollingConstraints::ComputeStickyOffset @
// third_party/blink/renderer/core/paint/sticky_position_scrolling_constraints.cc
// (Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f), simplified for scroll=0.
func computeStickyOffset(box *Box, sp stickyScrollport) (dx, dy float64) {
	if box.Style == nil {
		return 0, 0
	}
	off := box.Style.GetPositionOffsetResolved(sp.width, sp.height)

	cbX, cbY, cbW, cbH := stickyContainingBlock(box)

	// Top constraint: push down if element's top edge is above (spY + top inset).
	if off.HasTop {
		threshold := sp.y + off.Top
		if box.Y < threshold {
			// Clamp: cannot push element below containing block's bottom edge.
			maxDY := (cbY + cbH) - (box.Y + box.Height)
			if maxDY < 0 {
				maxDY = 0
			}
			dy = math.Min(threshold-box.Y, maxDY)
		}
	}

	// Bottom constraint: push up if element's bottom edge is below
	// (spY + spHeight - bottom inset). Bottom constraint is applied after top.
	if off.HasBottom {
		threshold := sp.y + sp.height - off.Bottom
		elemBottom := box.Y + dy + box.Height
		if elemBottom > threshold {
			// Clamp: top constraint takes priority; cannot push above CB top.
			minDY := cbY - box.Y
			if minDY > 0 {
				minDY = 0
			}
			dy = math.Max(dy-(elemBottom-threshold), minDY)
		}
	}

	// Left constraint: push right if element's left edge is left of (spX + left inset).
	if off.HasLeft {
		threshold := sp.x + off.Left
		if box.X < threshold {
			maxDX := (cbX + cbW) - (box.X + box.Width)
			if maxDX < 0 {
				maxDX = 0
			}
			dx = math.Min(threshold-box.X, maxDX)
		}
	}

	// Right constraint: push left if element's right edge is right of
	// (spX + spWidth - right inset). Right constraint is applied after left.
	if off.HasRight {
		threshold := sp.x + sp.width - off.Right
		elemRight := box.X + dx + box.Width
		if elemRight > threshold {
			minDX := cbX - box.X
			if minDX > 0 {
				minDX = 0
			}
			dx = math.Max(dx-(elemRight-threshold), minDX)
		}
	}

	return dx, dy
}

// shiftBox recursively shifts a box and all its descendants by (dx, dy).
func shiftBox(box *Box, dx, dy float64) {
	if box == nil {
		return
	}
	box.X += dx
	box.Y += dy
	for _, child := range box.Children {
		shiftBox(child, dx, dy)
	}
}

// applyStickyOffsets walks the box tree and applies sticky positioning offsets
// for a static (scroll=0) renderer. The scroll container is the nearest
// overflow:auto/scroll ancestor, or the viewport if none exists.
//
// This mirrors Blink's paint-time sticky offset application from
// PaintLayer::ApplyTransform (paint_layer.cc), adapted for post-layout box-tree
// traversal in louis14's static rendering model.
func applyStickyOffsets(box *Box, viewportWidth, viewportHeight float64) {
	applyStickyOffsetsWithScrollport(box, stickyScrollport{
		x:      0,
		y:      0,
		width:  viewportWidth,
		height: viewportHeight,
	})
}

// applyStickyOffsetsWithScrollport recursively walks the box tree, passing the
// nearest scroll container's scrollport to sticky descendants.
func applyStickyOffsetsWithScrollport(box *Box, scrollport stickyScrollport) {
	if box == nil {
		return
	}

	// Apply sticky offset to this element before processing children.
	if box.Position == css.PositionSticky {
		dx, dy := computeStickyOffset(box, scrollport)
		if dx != 0 || dy != 0 {
			shiftBox(box, dx, dy)
		}
	}

	// Determine scrollport for children. If this box is a scroll container,
	// the children's sticky scrollport is this box's content area (at scroll=0).
	childScrollport := scrollport
	if isScrollContainer(box) {
		// Content area: border-box minus border and padding.
		childScrollport = stickyScrollport{
			x:      box.X + box.Border.Left + box.Padding.Left,
			y:      box.Y + box.Border.Top + box.Padding.Top,
			width:  box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right,
			height: box.Height - box.Border.Top - box.Border.Bottom - box.Padding.Top - box.Padding.Bottom,
		}
	}

	for _, child := range box.Children {
		applyStickyOffsetsWithScrollport(child, childScrollport)
	}
}

// reverseAndMirrorRunes returns s with its Unicode code points reversed and
// bidi-mirrored per UAX#9 rule L4. Characters with the Bidi_Mirrored property
// are substituted with their mirror glyph during reversal. This converts RTL
// text from logical (Unicode) order to visual order for left-to-right rendering.
func reverseAndMirrorRunes(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i <= j; i, j = i+1, j-1 {
		ri, rj := runes[i], runes[j]
		ri = bidiMirror(ri)
		rj = bidiMirror(rj)
		runes[i], runes[j] = rj, ri
	}
	return string(runes)
}

// bidiMirror returns the bidi-mirrored glyph for paired bracket and
// punctuation characters (Unicode Bidi_Mirrored property, UAX#9 L4).
// Returns the same rune if no mirror exists.
func bidiMirror(r rune) rune {
	switch r {
	case '(':
		return ')'
	case ')':
		return '('
	case '<':
		return '>'
	case '>':
		return '<'
	case '[':
		return ']'
	case ']':
		return '['
	case '{':
		return '}'
	case '}':
		return '{'
	case '\u00AB': // «
		return '\u00BB' // »
	case '\u00BB': // »
		return '\u00AB' // «
	case '\u2039': // ‹
		return '\u203A' // ›
	case '\u203A': // ›
		return '\u2039' // ‹
	case '\u2045': // ⁅
		return '\u2046' // ⁆
	case '\u2046': // ⁆
		return '\u2045' // ⁅
	case '\u207D': // ⁽
		return '\u207E' // ⁾
	case '\u207E': // ⁾
		return '\u207D' // ⁽
	case '\u208D': // ₍
		return '\u208E' // ₎
	case '\u208E': // ₎
		return '\u208D' // ₍
	case '\u2308': // ⌈
		return '\u2309' // ⌉
	case '\u2309': // ⌉
		return '\u2308' // ⌈
	case '\u230A': // ⌊
		return '\u230B' // ⌋
	case '\u230B': // ⌋
		return '\u230A' // ⌊
	case '\u2329': // 〈
		return '\u232A' // 〉
	case '\u232A': // 〉
		return '\u2329' // 〈
	case '\u27E8': // ⟨
		return '\u27E9' // ⟩
	case '\u27E9': // ⟩
		return '\u27E8' // ⟨
	}
	return r
}

// recomputeTextDecorationInsets re-resolves the text-decoration-inset of every
// AppliedTextDecoration after ApplyTransitionsFirstFrame has rewritten
// properties post-cascade: the cascade recorded each inset
// (ResolveAppliedTextDecorations) from the after-change font and inset, and a
// transitioned font-size or text-decoration-inset changes it. Iterates the
// styles map directly rather than walking the DOM. No Blink analog: Blink
// applies interpolations inside the cascade, before the decoration is built.
//
// Each AppliedTextDecoration entry carries a SourceStyle pointer (set in
// computeOwnTextDecorationContribution) so we can re-resolve even on
// descendant styles that only hold an inherited copy of the vector — without
// needing to walk the DOM tree or track originating nodes separately.
func recomputeTextDecorationInsets(styles map[*html.Node]*css.Style) {
	// Cache resolved insets by SourceStyle pointer: many descendant styles may
	// share the same originating SourceStyle (e.g. an ancestor <a> element), so
	// computing GetTextDecorationInset() once per unique source avoids O(depth)
	// redundant string parses.
	seen := make(map[*css.Style]css.TextDecorationInset)
	for _, style := range styles {
		if style == nil || len(style.AppliedTextDecorations) == 0 {
			continue
		}
		for i := range style.AppliedTextDecorations {
			src := style.AppliedTextDecorations[i].SourceStyle
			if src == nil {
				continue
			}
			inset, ok := seen[src]
			if !ok {
				inset = src.GetTextDecorationInset()
				seen[src] = inset
			}
			style.AppliedTextDecorations[i].Inset = inset
		}
	}
}
