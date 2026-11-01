package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

// TestLeadingSpaceAfterOpenTagFitsMaxContent (LOU-567): a collapsible space
// that starts a line inside an inline box (here after the span's open tag)
// is removed before the line breaker measures the line, so content laid out
// at its own max-content width fits on one line. Blink's LineBreaker skips
// leading collapsible spaces while the line has no content, whatever
// open-tag items precede them (LineBreaker::HandleText's
// state_ == LineBreakState::kLeading path → HandleLeadingSpaces). Exposed by
// css-display/display-contents-inline-flex-001, whose display:contents text
// now sits in anonymous inline wrappers: "0 1" fitted and "2" wrapped.
func TestLeadingSpaceAfterOpenTagFitsMaxContent(t *testing.T) {
	s0 := makeNode("span", makeTextNode("\n  0\n  "))
	s1 := makeNode("span", makeTextNode("1"))
	s2 := makeNode("span", makeTextNode("\n  2\n  "))
	div := makeNode("div", s0, s1, s2)
	styles := map[*html.Node]*css.Style{
		div: makeStyle("display", "block", "font-size", "16px"),
		s0:  makeStyle("display", "inline", "font-size", "16px"),
		s1:  makeStyle("display", "inline", "font-size", "16px"),
		s2:  makeStyle("display", "inline", "font-size", "16px"),
	}
	tree := buildTestTree(div, styles)
	ctx := testContext()
	ctx.FontConfig = text.DefaultFontConfig()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := ConstraintSpace{
		AvailableSize:    oldLogicalToGeom(LogicalSize{InlineSize: 800, BlockSize: Indefinite}),
		WritingDirection: wdm,
	}
	maxContent := measureInlineMinMax(tree, ctx, space).MaxContent
	if maxContent <= 0 {
		t.Fatalf("max-content = %v, want > 0", maxContent)
	}

	data := CollectInlines(tree)
	lb := NewLineBreaker(data, ctx, space, ctx.FontConfig, LineBreakerContent)
	lb.availableWidth = maxContent
	var line LineInfo
	lines := 0
	for lb.NextLine(&line) {
		lines++
	}
	if lines != 1 {
		t.Errorf("laid out at max-content %v: got %d lines, want 1", maxContent, lines)
	}
}
