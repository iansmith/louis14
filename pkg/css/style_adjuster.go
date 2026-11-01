package css

import "louis14/pkg/html"

// This file ports the display adjustments of Blink's StyleAdjuster
// (core/css/resolver/style_adjuster.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the computed-style fixups that
// run once an element's cascade is complete and before its children are
// styled. Only the display-related parts are ported; the rest of Blink's
// AdjustComputedStyle (overflow, touch-action, text-decoration skip-ink,
// columns on SVG text, ...) is either handled elsewhere in louis14 or not
// yet ported.

// IsInBlockifyingDisplay mirrors ComputedStyle::IsInBlockifyingDisplay: true
// when AdjustStyleForDisplay ran under a layout parent that blockifies its
// children (style_adjuster.cc:765-766).
func (s *Style) IsInBlockifyingDisplay() bool { return s.isInBlockifyingDisplay }

// IsDisplayMath mirrors ComputedStyle::IsDisplayMath (kMath / kBlockMath).
func (s *Style) IsDisplayMath() bool {
	d := s.GetDisplay()
	return d == DisplayMath || d == DisplayBlockMath
}

// OriginalDisplay mirrors ComputedStyle::OriginalDisplay: the display
// before StyleAdjuster blockified or otherwise adjusted it. A style that
// never went through AdjustComputedStyle reports its current display.
func (s *Style) OriginalDisplay() DisplayType {
	if s.originalDisplay != "" {
		return s.originalDisplay
	}
	return s.GetDisplay()
}

// IsOriginalDisplayInlineType mirrors ComputedStyle::
// IsOriginalDisplayInlineType: IsDisplayInlineType (core/style/
// computed_style.h:2756-2759 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// applied to OriginalDisplay:
// an out-of-flow box that was inline-level before blockification takes
// its static position beside the inline cursor
// (inline_layout_algorithm.cc:807, block_layout_algorithm.cc:1663).
func (s *Style) IsOriginalDisplayInlineType() bool {
	switch s.OriginalDisplay() {
	case DisplayInline, DisplayInlineBlock, DisplayInlineFlex, DisplayInlineGrid,
		DisplayInlineTable, DisplayInlineListItem, DisplayRuby, DisplayRubyText,
		DisplayMath:
		return true
	}
	return false
}

// BlockifiesChildren mirrors ComputedStyle::BlockifiesChildren
// (core/style/computed_style.h:1798-1803 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): flex, grid and math containers,
// and a display:contents box that was itself blockified. louis14 has no
// grid-lanes, layout-custom or ForcesBlockifiesChildren (<mtd>) yet.
func (s *Style) BlockifiesChildren() bool {
	switch d := s.GetDisplay(); d {
	case DisplayFlex, DisplayInlineFlex, DisplayGrid, DisplayInlineGrid,
		DisplayMath, DisplayBlockMath:
		return true
	case DisplayContents:
		return s.isInBlockifyingDisplay
	}
	return false
}

// LayoutParentStyle returns the style Blink's StyleResolverState::
// LayoutParentStyle would hold for a box whose flat-tree parent is parent:
// the style of the nearest ancestor-or-self of parent that is a layout
// parent, i.e. not display:contents (LayoutTreeBuilderTraversal::
// IsLayoutParent / LayoutParent, core/dom/layout_tree_builder_traversal.cc:
// 65-80 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b, read by
// ElementResolveContext, element_resolve_context.cc:107). With no layout
// parent it falls back to parent's own style (style_resolver_state.cc:
// 135-137). For a pseudo-element, parent is its originating element.
func LayoutParentStyle(parent *html.Node, styles map[*html.Node]*Style) *Style {
	for p := parent; p != nil; p = p.Parent {
		s := styles[p]
		if s == nil {
			break
		}
		if s.GetDisplay() != DisplayContents {
			return s
		}
	}
	if parent == nil {
		return nil
	}
	return styles[parent]
}

// AdjustComputedStyle applies StyleAdjuster's display adjustments to style,
// the computed style of element (nil for a pseudo-element, which is neither
// an HTML, SVG nor MathML element) whose layout parent has style
// layoutParentStyle (see LayoutParentStyle; nil at the root). Mirrors the
// display steps of StyleAdjuster::AdjustComputedStyle in Blink's order
// (style_adjuster.cc:1118-1199, :1316-1325 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//  1. AdjustStyleForHTMLElement (:1126-1130);
//  2. the document element and floated / out-of-flow boxes get
//     EquivalentBlockDisplay (:1172-1178);
//  3. math display values on non-MathML elements compute to inline / block
//     (:1186-1191);
//  4. AdjustStyleForDisplay against the layout parent (:1196-1199);
//  5. AdjustStyleForSvgElement, or display:contents → none on MathML
//     elements (:1316-1325).
func AdjustComputedStyle(element *html.Node, style, layoutParentStyle *Style) {
	style.originalDisplay = style.GetDisplay()
	ns := ""
	if element != nil {
		ns = element.NamespaceURI()
	}
	if ns == html.XHTMLNamespaceURI && style.GetDisplay() != DisplayNone {
		AdjustStyleForHTMLElement(element, style)
	}

	if style.GetDisplay() != DisplayNone {
		display := style.GetDisplay()
		pos := style.GetPosition()
		hasOutOfFlowPosition := pos == PositionAbsolute || pos == PositionFixed
		if isDocumentElement(element) ||
			(display != DisplayContents && (hasOutOfFlowPosition || style.GetFloat() != FloatNone)) {
			setDisplay(style, EquivalentBlockDisplay(display))
		}
		if ns != html.MathMLNamespaceURI && style.IsDisplayMath() {
			if style.GetDisplay() == DisplayBlockMath {
				setDisplay(style, DisplayBlock)
			} else {
				setDisplay(style, DisplayInline)
			}
		}
		AdjustStyleForDisplay(element, style, layoutParentStyle)
	}

	switch ns {
	case html.SVGNamespaceURI:
		AdjustStyleForSvgElement(element, style)
	case html.MathMLNamespaceURI:
		// https://drafts.csswg.org/css-display/#unbox-mathml
		if style.GetDisplay() == DisplayContents {
			setDisplay(style, DisplayNone)
		}
	}
}

// AdjustStyleForHTMLElement mirrors the display cases of
// StyleAdjuster::AdjustStyleForHTMLElement (style_adjuster.cc:539-650 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): unusual elements turn
// display:contents into none (css-display "Unboxing in HTML"), frames never
// honor position / display / float, and a legend is blockified.
// Image collapse (IsCollapsed) is not ported.
func AdjustStyleForHTMLElement(element *html.Node, style *Style) {
	switch element.TagName {
	case "frame", "frameset":
		// :560-567.
		style.Set("position", "static")
		setDisplay(style, DisplayBlock)
		style.Set("float", "none")
	case "legend":
		// :579-589.
		if d := style.GetDisplay(); d != DisplayContents {
			setDisplay(style, EquivalentBlockDisplay(d))
		}
	case "img", "textarea", "embed", "object",
		"br", "wbr", "meter", "progress", "canvas", "audio", "video",
		"input", "select", "iframe":
		// :542-548, :598-622, :631-645.
		if style.GetDisplay() == DisplayContents {
			setDisplay(style, DisplayNone)
		}
	}
}

// AdjustStyleForDisplay mirrors the blockification half of
// StyleAdjuster::AdjustStyleForDisplay (style_adjuster.cc:757-772, :817-824 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a box whose layout parent
// blockifies its children is marked IsInBlockifyingDisplay and, unless it is
// display:contents, takes its EquivalentBlockDisplay; an inline box whose
// writing mode differs from its layout parent's becomes inline-block (CSS
// Writing Modes 4 §3.1; not for pseudo-elements, element == nil). Not ported: SetIsFlexOrGridOrCustomItem and
// SetIsInsideDisplayIgnoringFloatingChildren (louis14 reads the parent's
// display at layout time instead), the input-file / canvas-subtree
// exceptions, and inlinification (:788-806), which louis14 still applies
// on the layout tree (layout.normalizeRubySubtrees).
func AdjustStyleForDisplay(element *html.Node, style, layoutParentStyle *Style) {
	if layoutParentStyle != nil && layoutParentStyle.BlockifiesChildren() {
		style.isInBlockifyingDisplay = true
		if d := style.GetDisplay(); d != DisplayContents {
			setDisplay(style, EquivalentBlockDisplay(d))
		}
	}
	// No layout parent (the root): compare against the initial horizontal-tb.
	parentIsVertical := layoutParentStyle != nil && isVerticalWritingMode(layoutParentStyle)
	if element != nil && style.GetDisplay() == DisplayInline &&
		isVerticalWritingMode(style) != parentIsVertical {
		setDisplay(style, DisplayInlineBlock)
	}
}

// AdjustStyleForSvgElement mirrors the display and position parts of
// StyleAdjuster::AdjustStyleForSvgElement (style_adjuster.cc:176-216 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): only the outermost <svg>
// honors CSS position, and display:contents computes to none on the
// outermost <svg> and on every SVG element except nested <svg>, <g>, <use>
// and <tspan>, whose children are hoisted (css-display "Unboxing in SVG").
// The text-decoration resets and foreignObject / text blockification are
// not ported.
func AdjustStyleForSvgElement(element *html.Node, style *Style) {
	isSVGRoot := element.TagName == "svg" &&
		(element.Parent == nil || element.Parent.NamespaceURI() != html.SVGNamespaceURI)
	if !isSVGRoot {
		style.Set("position", "static")
	}
	if style.GetDisplay() != DisplayContents {
		return
	}
	switch element.TagName {
	case "svg", "g", "use", "tspan":
		if !isSVGRoot {
			return
		}
	}
	setDisplay(style, DisplayNone)
}

// isDocumentElement reports whether element is the document's root <html>
// element (element->GetDocument().documentElement() == element). louis14
// parses fragments without an implicit <html>; their top-level elements
// are not document elements.
func isDocumentElement(element *html.Node) bool {
	return element != nil && element.TagName == "html" &&
		element.NamespaceURI() == html.XHTMLNamespaceURI &&
		(element.Parent == nil || element.Parent.Type != html.ElementNode || element.Parent.TagName == "document")
}

func isVerticalWritingMode(s *Style) bool {
	switch wm, _ := s.Get("writing-mode"); wm {
	case "vertical-rl", "vertical-lr", "sideways-rl", "sideways-lr":
		return true
	}
	return false
}

func setDisplay(s *Style, d DisplayType) {
	if s.GetDisplay() != d {
		s.Set("display", string(d))
	}
}
