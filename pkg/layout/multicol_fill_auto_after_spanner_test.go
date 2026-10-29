package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-409: in an auto-height column-fill:auto multicol, only the line that
// ends at a spanner is balanced (column_layout_algorithm.cc:1071-1081 @
// a9f50e522efa); content after the spanner fills columns sequentially.
// Chrome 155: [div 20, spanner 10, div 80] in 2 columns of a 100px-wide
// multicol gives a 10px balanced row, the spanner at y=10, and the 80px div
// in one column at y=20 — a 100px multicol.
func TestFillAutoContentAfterSpannerIsNotBalanced_LOU409(t *testing.T) {
	before := makeNode("div")
	spanner := makeNode("div")
	after := makeNode("div")
	mc := makeNode("div", before, spanner, after)
	styles := map[*html.Node]*css.Style{
		mc:      makeStyle("display", "block", "width", "100px", "column-count", "2", "column-fill", "auto", "column-gap", "0"),
		before:  makeStyle("display", "block", "height", "20px"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "10px"),
		after:   makeStyle("display", "block", "height", "80px"),
	}
	result := layoutMulticolForTest(t, mc, styles)
	spanners := spannerChildren(result)
	if len(spanners) != 1 || spanners[0].Offset.Top.Float64() != 10 {
		t.Fatalf("spanners %v, want one at y=10", spanners)
	}
	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("multicol block-size = %v, want 100 (the 80px div fills one column)", got)
	}
}
