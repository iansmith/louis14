package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-404: in a wrapping multicol nested in an outer fragmentation context, a
// spanner that does not fit in the rest of its row moves to the next row, and
// when that row does not fit in the outer fragmentainer the spanner is pushed
// to the next outer fragmentainer (Blink LayoutSpanner,
// column_layout_algorithm.cc:1364-1394 @ a9f50e522efa).
//
// Mirrors css-multicol/column-height-029's inner multicol in a 100px outer
// column: a 100px block balances to a 50px row of the 60px row height; the
// 20px spanner would start the next row at 60+13=73, and a 60px row does not
// fit in the 27px left. Chrome 155 places the spanner at the top of the next
// outer column, laid out once.
func TestSpannerRowSnapPushedToNextOuterFragmentainer_LOU404(t *testing.T) {
	block := makeNode("div")
	spanner := makeNode("div")
	after := makeNode("div")
	mc := makeNode("div", block, spanner, after)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "width", "50px", "column-count", "2",
			"column-fill", "auto", "column-height", "60px", "column-wrap", "wrap",
			"row-gap", "13px", "column-gap", "0"),
		block:   makeStyle("display", "block", "height", "100px"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "20px"),
		after:   makeStyle("display", "block", "height", "80px"),
	}
	result := layoutMulticolInFragmentainer(t, mc, styles, 100, 0)
	if got := spannerChildren(result); len(got) != 0 {
		t.Errorf("spanner laid out in this outer fragmentainer at y=%v; want it pushed to the next one",
			got[0].Offset.Top.Float64())
	}
	if result.BreakToken == nil {
		t.Fatal("no break token: the spanner and the content after it are lost")
	}
	pushed := false
	for _, t := range result.BreakToken.ChildBreakTokens {
		if tok, ok := t.(*BlockBreakToken); ok && tok.Node != nil && tok.Node.DOMNode == spanner && tok.IsBreakBefore {
			pushed = true
		}
	}
	if !pushed {
		t.Errorf("outgoing token %+v does not break before the spanner", result.BreakToken)
	}
}
