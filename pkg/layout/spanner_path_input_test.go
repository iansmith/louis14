package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-406: content after a spanner is never laid out before it. A block on
// the spanner path (found by the balancing pass and handed to every column
// layout, column_layout_algorithm.cc:942, :1637 @ a9f50e522efa) forces its
// child breaks into the same flow (block_layout_algorithm.cc:820-827,
// box_fragment_builder.cc:503-510), so its overflowing child does not open a
// parallel flow that later siblings could fill.
//
// Mirrors css-multicol/multicol-span-all-children-height-009: a 250px box
// holding a 1000px child and a spanner (margin-top:-100px), followed by ten
// 50px inline-blocks, in a 10-column, 100px-wide multicol. Chrome 155: the
// 1000px child fills all ten columns (row 100), the spanner sits at y=0, and
// every inline-block is at y=50 after it; the multicol is 100px tall.
func TestSpannerPathKeepsLaterContentAfterSpanner_LOU406(t *testing.T) {
	tall := makeNode("div")
	spanner := makeNode("div")
	box := makeNode("div", tall, spanner)
	kids := []*html.Node{box}
	styles := map[*html.Node]*css.Style{
		box:     makeStyle("display", "block", "height", "250px"),
		tall:    makeStyle("display", "block", "height", "1000px"),
		spanner: makeStyle("display", "block", "column-span", "all", "margin-top", "-100px", "height", "50px"),
	}
	var ibs []*html.Node
	for i := 0; i < 10; i++ {
		ib := makeNode("div")
		ibs = append(ibs, ib)
		kids = append(kids, ib)
		styles[ib] = makeStyle("display", "inline-block", "vertical-align", "top", "width", "100%", "height", "50px")
	}
	mc := makeNode("div", kids...)
	styles[mc] = makeStyle("display", "block", "width", "100px", "column-count", "10", "column-gap", "0")
	result := layoutMulticolForTest(t, mc, styles)

	spanners := spannerChildren(result)
	if len(spanners) != 1 {
		t.Fatalf("got %d spanner fragments, want 1", len(spanners))
	}
	if got := spanners[0].Offset.Top.Float64(); got != 0 {
		t.Errorf("spanner at y=%v, want 0 (100px row, -100px margin)", got)
	}
	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("multicol block-size = %v, want 100", got)
	}
	isIB := map[*html.Node]bool{}
	for _, ib := range ibs {
		isIB[ib] = true
	}
	var tops []float64
	var walk func(f *PhysicalFragment, top float64)
	walk = func(f *PhysicalFragment, top float64) {
		for _, c := range f.Children {
			y := top + c.Offset.Top.Float64()
			if c.Fragment.Node != nil && isIB[c.Fragment.Node] {
				tops = append(tops, y)
			}
			walk(c.Fragment, y)
		}
	}
	walk(result.Fragment, 0)
	if len(tops) != len(ibs) {
		t.Fatalf("got %d inline-block fragments at %v, want %d", len(tops), tops, len(ibs))
	}
	for _, y := range tops {
		if y != 50 {
			t.Errorf("inline-blocks at %v, want all at y=50 (after the spanner)", tops)
			break
		}
	}
}
