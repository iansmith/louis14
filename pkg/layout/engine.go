package layout

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
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

	// Phase 1: Compute styles.
	computedStyles := css.ApplyStylesToDocument(doc, le.viewport.width, le.viewport.height)
	css.ResolveLogicalPropertiesInTree(doc.Root, computedStyles)

	// Phase 1b: Compute ch widths from actual font metrics.
	fontConfig := le.fontConfig
	if fontConfig.Regular == "" {
		fontConfig = text.DefaultFontConfig()
	}
	computeChWidths(computedStyles, fontConfig)

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
	rootBox := fragmentToBox(result.Fragment, nil, rootOffsetX, rootOffsetY)

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

// layoutNestedDocument parses and lays out a nested HTML document (for iframe/object)
// using the given physical dimensions as the viewport. nestedDocURI is the src/data
// URI of the nested document, used to re-root relative image/document URIs so that
// sub-resources of the nested document resolve correctly relative to its location.
// Returns the root layout result, or nil if parsing/layout fails.
func layoutNestedDocument(ctx *LayoutContext, htmlContent string, vpWidth, vpHeight float64, nestedDocURI string) *NestedDocumentResult {
	// Compute the nested document's BaseDir from its fully-qualified URL
	// (outer base + iframe src). URLs inside the nested doc resolve against
	// this base, so the outer renderer's single fetcher can serve sub-
	// resources for any depth of nesting. Mirrors how Blink threads the
	// owner-document base through `CSSParserContext` for each parse pass.
	nestedBaseDir := css.URLDir(css.CompleteURL(nestedDocURI, ctx.BaseDir))
	if nestedBaseDir == "." {
		nestedBaseDir = ""
	}

	doc, err := html.Parse(htmlContent)
	if err != nil {
		return nil
	}
	doc.BaseDir = nestedBaseDir

	computedStyles := css.ApplyStylesToDocument(doc, vpWidth, vpHeight)
	css.ResolveLogicalPropertiesInTree(doc.Root, computedStyles)
	computeChWidths(computedStyles, ctx.FontConfig)

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
	}
	layoutRoot := treeBuilder.BuildLayoutTree(rootElement)

	// Build nested context — inherit fonts; re-root fetchers to the nested doc's URI
	// so that relative sub-resources (images, stylesheets, child iframes) resolve
	// correctly relative to the nested document's location, not the outer document's.
	nestedCtx := &LayoutContext{
		ViewportWidth:   vpWidth,
		ViewportHeight:  vpHeight,
		BaseDir:         doc.BaseDir,
		ImageFetcher:    ReRootedImageFetcher(ctx.ImageFetcher, nestedDocURI),
		DocumentFetcher: ReRootedDocumentFetcher(ctx.DocumentFetcher, nestedDocURI),
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
		Node:                   frag.Node,
		Style:                  frag.Style,
		X:                      absX,
		Y:                      absY,
		Width:                  frag.Size.WidthF64(),
		Height:                 frag.Size.HeightF64(),
		Parent:                 parent,
		ClipContentToBorderBox: frag.ClipContentToBorderBox,
		RenderedColumnCount:    frag.RenderedColumnCount,
		GapGeometry:            frag.GapGeometry,
		IsColumnBox:            frag.IsColumnBox(),
		IsMulticolContainer:    frag.IsMulticolContainer,
		AppliedTextDecorations: frag.AppliedTextDecorations,
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
		if lin.MarkerStyle != nil {
			box.MarkerStyle = lin.MarkerStyle
		}
		if lin.MarkerContent != "" {
			box.MarkerContent = lin.MarkerContent
		}
	}

	// CSS 2.1 §9.4.3: Apply position:relative offset from the fragment.
	// The offset was computed during layout and stored on the fragment.
	if !frag.RelativeOffset.Left.IsZero() || !frag.RelativeOffset.Top.IsZero() {
		box.X += frag.RelativeOffset.LeftF64()
		box.Y += frag.RelativeOffset.TopF64()
		absX = box.X
		absY = box.Y
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

// computeChWidths measures per-font metrics needed to resolve CSS font-
// relative units (ex, ch) and applies CSS Fonts 5 §1.7 `font-size-adjust`
// to produce a used font-size. Populates `ChWidth`, `XHeight`, and
// `UsedFontSize` on every Style.
//
// Mirrors Blink's `FontDescription::EffectiveSize()` (font_description.cc) and
// `FontMetrics::XHeight()` (font_metrics.h) at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f: the used font-size is computed by
// scaling the specified size by `adjust_value / actual_aspect_ratio_of_first
// _available_font`, then ex/ch are sourced from the used-size font's metrics
// so font-dependent CSS units track the visible glyph size.
func computeChWidths(styles map[*html.Node]*css.Style, fc text.FontConfig) {
	// Per-font metric cache keyed by (path, sizeRounded). Sub-pixel size jitter
	// is rare; rounding stabilises the cache for tests rendering many elements
	// at one size.
	type fontKey struct {
		path     string
		fontSize int32
	}
	type fontMetrics struct {
		chWidth float64
		xHeight float64
		capHght float64
	}
	cache := make(map[fontKey]fontMetrics)

	measure := func(fontPath string, fontSize float64) fontMetrics {
		k := fontKey{path: fontPath, fontSize: int32(math.Round(fontSize))}
		if m, ok := cache[k]; ok {
			return m
		}
		chLU, _ := text.MeasureText("0", fontSize, fontPath)
		m := fontMetrics{
			chWidth: chLU.Float64(),
			xHeight: text.FontXHeightFromFont(fontSize, fontPath),
			capHght: text.FontCapHeightFromFont(fontSize, fontPath),
		}
		cache[k] = m
		return m
	}

	for _, style := range styles {
		if style == nil {
			continue
		}
		specifiedSize := style.GetFontSize()
		family, _ := style.Get("font-family")
		bold := style.GetFontWeight() == css.FontWeightBold
		italic := style.GetFontStyle() == css.FontStyleItalic
		mono := style.IsMonospaceFamily()
		ahem := style.IsAhemFamily()
		synth := style.GetFontSynthesis()
		fontPath := fc.FontPathForFamilyWithSynthesis(family, bold, italic, mono, ahem, synth.Weight, synth.Style)

		// Measure metrics at the specified size so we can read the first font's
		// actual aspect ratio for font-size-adjust resolution.
		baseMetrics := measure(fontPath, specifiedSize)

		// CSS Fonts 5 §1.7: derive the used font-size from font-size-adjust.
		// `none` and `from-font` leave the used size equal to the specified
		// size (from-font matches the first font's own aspect → factor 1).
		usedSize := specifiedSize
		adjust := style.GetFontSizeAdjust()
		if adjust.IsActive() && specifiedSize > 0 {
			actualRatio := firstFontAspectRatio(adjust.Metric, baseMetrics, specifiedSize, fontPath)
			if actualRatio > 0 {
				usedSize = specifiedSize * adjust.Value / actualRatio
			} else {
				// Spec: when the first font lacks the requested metric, the used
				// size is undefined; clamp to 0 to match Blink's behavior of
				// hiding the glyphs (matches font-size-adjust-zero-* refs).
				usedSize = 0
			}
		}

		usedMetrics := baseMetrics
		if usedSize != specifiedSize {
			usedMetrics = measure(fontPath, usedSize)
		}

		style.UsedFontSize = usedSize
		style.UsedFontSizeSet = true
		style.ChWidth = usedMetrics.chWidth
		style.XHeight = usedMetrics.xHeight
	}
}

// firstFontAspectRatio returns the first available font's actual aspect ratio
// for the requested font-size-adjust metric (CSS Fonts 5 §1.7). Returns 0 when
// the font has no usable value for the metric — callers treat that as a
// zero-sized used font-size.
func firstFontAspectRatio(metric css.FontSizeAdjustMetric, m struct {
	chWidth float64
	xHeight float64
	capHght float64
}, fontSize float64, fontPath string) float64 {
	if fontSize <= 0 {
		return 0
	}
	switch metric {
	case css.FontSizeAdjustCapHeight:
		return m.capHght / fontSize
	case css.FontSizeAdjustChWidth:
		return m.chWidth / fontSize
	case css.FontSizeAdjustIcWidth, css.FontSizeAdjustIcHeight:
		// CSS Fonts 5 §1.7: ic-width / ic-height use the advance width or
		// height of the U+6C34 (水) glyph. Mirror Blink's behavior: the metric
		// is sourced from the same fontPath; when 水 isn't present (e.g. Ahem),
		// the metric is undefined and the spec calls for the user agent to
		// behave as if `font-size-adjust: none` — Blink resolves the ratio to
		// 0 which clamps the used size to 0. We follow Blink.
		w, h := text.MeasureText("水", fontSize, fontPath)
		wf, hf := w.Float64(), h.Float64()
		if wf == 0 || hf == 0 {
			return 0
		}
		if metric == css.FontSizeAdjustIcWidth {
			return wf / fontSize
		}
		return hf / fontSize
	default:
		// ex-height (default)
		return m.xHeight / fontSize
	}
}
