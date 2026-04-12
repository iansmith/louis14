package layout

import (
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
		ImageFetcher:    le.imageFetcher,
		DocumentFetcher: le.documentFetcher,
		FontConfig:      fontConfig,
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

	// Build the root constraint space in the root's own logical coordinate system.
	// We use rootWDM as both parent and child so IsOrthogonalWritingModeRoot = false,
	// ensuring the root element fills the viewport inline-size (stretch sizing).
	// The root's block-size is auto (determined by content); the canvas background
	// covers any remaining viewport area per CSS 2.1 §14.2.
	//
	// Available sizes are expressed in the root's logical coordinates:
	//   HTB:      InlineSize = viewport.width,  BlockSize = viewport.height
	//   vertical: InlineSize = viewport.height, BlockSize = viewport.width
	var rootInlineSize, rootBlockSize float64
	if rootWDM.IsHorizontal() {
		rootInlineSize = le.viewport.width
		rootBlockSize = le.viewport.height
	} else {
		rootInlineSize = le.viewport.height
		rootBlockSize = le.viewport.width
	}

	rootSpace := NewConstraintSpaceBuilder(rootWDM, rootWDM, true).
		SetIsRootElement(true).
		SetAvailableSize(LogicalSize{
			InlineSize: rootInlineSize,
			BlockSize:  rootBlockSize,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: rootInlineSize,
			BlockSize:  rootBlockSize,
		}).
		SetPercentageResolutionInlineSize(rootInlineSize).
		Build()

	// Phase 6: Run layout.
	result := layoutElement(ctx, layoutRoot, rootSpace)

	// Phase 7: Convert fragment tree to box tree.
	// In vertical-rl, the root's block-start is the right edge of the ICB.
	// If the root is narrower than the viewport, offset it so its right edge
	// aligns with the viewport's right edge (block-start = right).
	var rootOffsetX float64
	if rootWDM.IsFlippedBlocks() {
		rootWidth := result.Fragment.Size.Width
		if rootWidth < le.viewport.width {
			rootOffsetX = le.viewport.width - rootWidth
		}
	}
	rootBox := fragmentToBox(result.Fragment, nil, rootOffsetX, 0)

	return []*Box{rootBox}
}

// layoutNestedDocument parses and lays out a nested HTML document (for iframe/object)
// using the given physical dimensions as the viewport. Returns the root layout result,
// or nil if parsing/layout fails.
func layoutNestedDocument(ctx *LayoutContext, htmlContent string, vpWidth, vpHeight float64) *LayoutResult {
	doc, err := html.Parse(htmlContent)
	if err != nil {
		return nil
	}

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

	// Build nested context — inherit image/document fetchers and fonts.
	nestedCtx := &LayoutContext{
		ViewportWidth:   vpWidth,
		ViewportHeight:  vpHeight,
		ImageFetcher:    ctx.ImageFetcher,
		DocumentFetcher: ctx.DocumentFetcher,
		FontConfig:      ctx.FontConfig,
	}

	rootStyle := layoutRoot.Style()
	rootWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	if rootStyle != nil {
		rootWDM = NewWritingDirectionMode(rootStyle)
	}

	var rootInlineSize, rootBlockSize float64
	if rootWDM.IsHorizontal() {
		rootInlineSize = vpWidth
		rootBlockSize = vpHeight
	} else {
		rootInlineSize = vpHeight
		rootBlockSize = vpWidth
	}

	rootSpace := NewConstraintSpaceBuilder(rootWDM, rootWDM, true).
		SetIsRootElement(true).
		SetAvailableSize(LogicalSize{
			InlineSize: rootInlineSize,
			BlockSize:  rootBlockSize,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: rootInlineSize,
			BlockSize:  rootBlockSize,
		}).
		SetPercentageResolutionInlineSize(rootInlineSize).
		Build()

	return layoutElement(nestedCtx, layoutRoot, rootSpace)
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
		Node:   frag.Node,
		Style:  frag.Style,
		X:      absX,
		Y:      absY,
		Width:  frag.Size.Width,
		Height: frag.Size.Height,
		Parent: parent,
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
	if frag.RelativeOffset.X != 0 || frag.RelativeOffset.Y != 0 {
		box.X += frag.RelativeOffset.X
		box.Y += frag.RelativeOffset.Y
		absX = box.X
		absY = box.Y
	}

	// Content area origin = border-box + border + padding.
	contentX := absX + box.Border.Left + box.Padding.Left
	contentY := absY + box.Border.Top + box.Padding.Top

	// Convert children. Child offsets are relative to the content area.
	for _, childLink := range frag.Children {
		childAbsX := contentX + childLink.Offset.X
		childAbsY := contentY + childLink.Offset.Y
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

// computeChWidths measures the actual advance width of "0" for each style's
// font and stores it on the Style. This makes the CSS ch unit resolve using
// real font metrics instead of a fixed heuristic.
func computeChWidths(styles map[*html.Node]*css.Style, fc text.FontConfig) {
	// Cache ch width per (fontPath, fontSize) to avoid redundant measurements.
	type fontKey struct {
		path     string
		fontSize float64
	}
	cache := make(map[fontKey]float64)

	for _, style := range styles {
		if style == nil {
			continue
		}
		fontSize := style.GetFontSize()
		family, _ := style.Get("font-family")
		bold := style.GetFontWeight() == css.FontWeightBold
		italic := style.GetFontStyle() == css.FontStyleItalic
		mono := style.IsMonospaceFamily()
		ahem := style.IsAhemFamily()
		fontPath := fc.FontPathForFamily(family, bold, italic, mono, ahem)

		key := fontKey{path: fontPath, fontSize: fontSize}
		ch, ok := cache[key]
		if !ok {
			ch, _ = text.MeasureText("0", fontSize, fontPath)
			cache[key] = ch
		}
		style.ChWidth = ch
	}
}

