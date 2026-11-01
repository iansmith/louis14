package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
	"testing"
)

// lineWidthForTest lays out parent's inline content on one 800px line and
// returns that line's LineInfo.Width — the inline size the line breaker
// shaped the content to.
func lineWidthForTest(t *testing.T, parent *html.Node, styles map[*html.Node]*css.Style) float64 {
	t.Helper()
	data := CollectInlines(buildTestTree(parent, styles))
	space := ConstraintSpace{
		AvailableSize:    oldLogicalToGeom(LogicalSize{InlineSize: 800, BlockSize: Indefinite}),
		WritingDirection: WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR},
	}
	lb := NewLineBreaker(data, testContext(), space, text.DefaultFontConfig(), LineBreakerContent)
	var line LineInfo
	if !lb.NextLine(&line) {
		t.Fatal("no line produced")
	}
	return line.Width
}

// kerningSpansWidth lays out <div><span>A</span><span>V</span></div> with
// the given extra style declarations on every element and returns the line
// width. "AV" is a kerning pair in the default (Liberation) faces.
func kerningSpansWidth(t *testing.T, decls ...string) float64 {
	t.Helper()
	spanA := makeNode("span", makeTextNode("A"))
	spanV := makeNode("span", makeTextNode("V"))
	parent := makeNode("div", spanA, spanV)
	props := append([]string{"font-size", "40px"}, decls...)
	styles := map[*html.Node]*css.Style{
		parent: makeStyle(append([]string{"display", "block"}, props...)...),
		spanA:  makeStyle(append([]string{"display", "inline"}, props...)...),
		spanV:  makeStyle(append([]string{"display", "inline"}, props...)...),
	}
	return lineWidthForTest(t, parent, styles)
}

// singleItemWidth lays out <div>s</div> with the given extra declarations.
func singleItemWidth(t *testing.T, s string, decls ...string) float64 {
	t.Helper()
	parent := makeNode("div", makeTextNode(s))
	props := append([]string{"display", "block", "font-size", "40px"}, decls...)
	return lineWidthForTest(t, parent, map[*html.Node]*css.Style{parent: makeStyle(props...)})
}

// TestLineBreaker_ShapingHonoursFontKerningNone: layout must shape text
// with the font's features, as paint does. Blink's InlineNode::ShapeText
// (core/layout/inline/inline_node.cc:1551 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) shapes each run with its Font,
// whose GetFontFeatures() — part of the shape cache key, :171-177 — carries
// FontFeatureRange::FromFontDescription's kern=0 for font-kerning:none
// (platform/fonts/shaping/font_features.cc:40-50 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14 measured with HarfBuzz
// defaults (kern on) while paint shaped kern-off, so text split across
// inline items was positioned kerned and painted unkerned (LOU-565: the
// css-display display-contents-* reftests drift ~2px).
func TestLineBreaker_ShapingHonoursFontKerningNone(t *testing.T) {
	a := singleItemWidth(t, "A")
	v := singleItemWidth(t, "V")
	kerned := kerningSpansWidth(t)
	if kerned >= a+v {
		t.Fatalf("precondition: AV across spans with default kerning = %v, want < A+V = %v (AV must kern in the default face)", kerned, a+v)
	}

	if got := kerningSpansWidth(t, "font-kerning", "none"); got != a+v {
		t.Errorf("<span>A</span><span>V</span> with font-kerning:none: line width %v, want unkerned A+V = %v", got, a+v)
	}
	if got := kerningSpansWidth(t, "font-feature-settings", `"kern" off`); got != a+v {
		t.Errorf(`<span>A</span><span>V</span> with font-feature-settings:"kern" off: line width %v, want unkerned A+V = %v`, got, a+v)
	}
	if got := singleItemWidth(t, "AV", "font-kerning", "none"); got != a+v {
		t.Errorf("single text item AV with font-kerning:none: line width %v, want unkerned A+V = %v", got, a+v)
	}
}

// TestCanMergeShapingContext_FontFeatures: two text items whose fonts
// differ only in their feature list are different Fonts in Blink — Font::
// operator== compares the FontDescription, which carries kerning and the
// font-variant-* / font-feature-settings state — so ShouldBreakShapingBefore
// Text breaks the shape between them (core/layout/inline/inline_node.cc:
// 470-491 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). They must not merge
// into one shaping context.
func TestCanMergeShapingContext_FontFeatures(t *testing.T) {
	base := &InlineItem{Type: InlineItemText, Style: makeStyle("font-size", "16px")}
	same := &InlineItem{Type: InlineItemText, Style: makeStyle("font-size", "16px")}
	if !canMergeShapingContext(base, same) {
		t.Fatal("precondition: identical styles must merge")
	}
	for _, decl := range [][2]string{
		{"font-kerning", "none"},
		{"font-feature-settings", `"liga" 0`},
		{"font-variant-ligatures", "none"},
		{"font-variant-numeric", "tabular-nums"},
		{"font-variant-caps", "small-caps"},
	} {
		other := &InlineItem{Type: InlineItemText, Style: makeStyle("font-size", "16px", decl[0], decl[1])}
		if canMergeShapingContext(base, other) {
			t.Errorf("items differing only in %s: %s merged; want a shaping break", decl[0], decl[1])
		}
	}
}
