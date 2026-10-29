package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-405: a spanner found after something broke in a parallel flow is not
// taken yet: Blink adds a forced break before it and lets the parallel flow
// finish in the following columns first (block_layout_algorithm.cc:1025-1040
// @ a9f50e522efa).
//
// Mirrors css-multicol/multicol-span-all-children-height-013: a 20px box
// whose 160px child overflows it, a 20px sibling, then the spanner, in a
// 4-column, 100px-wide multicol. Chrome 155 (headless, getClientRects): the
// 160px child is split 10/50/50/50 over all four columns, the spanner sits at
// y=50 and the multicol is 100px tall.
func TestSpannerDeferredUntilParallelFlowFinishes_LOU405(t *testing.T) {
	gap := makeNode("div")
	tall := makeNode("div")
	box := makeNode("div", gap, tall)
	sibling := makeNode("div")
	spanner := makeNode("div")
	mc := makeNode("div", box, sibling, spanner)
	styles := map[*html.Node]*css.Style{
		mc:      makeStyle("display", "block", "width", "100px", "column-count", "4", "column-gap", "0"),
		box:     makeStyle("display", "block", "height", "20px"),
		gap:     makeStyle("display", "block", "height", "40px"),
		tall:    makeStyle("display", "block", "height", "160px"),
		sibling: makeStyle("display", "block", "height", "20px"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "50px"),
	}
	result := layoutMulticolForTest(t, mc, styles)

	spanners := spannerChildren(result)
	if len(spanners) != 1 {
		t.Fatalf("got %d spanner fragments, want 1", len(spanners))
	}
	if got := spanners[0].Offset.Top.Float64(); got != 50 {
		t.Errorf("spanner at y=%v, want 50", got)
	}
	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("multicol block-size = %v, want 100", got)
	}
	// The 160px child appears in every column: 10 in the first, then 50 in
	// each of the other three.
	var tallHeights []float64
	var walk func(f *PhysicalFragment)
	walk = func(f *PhysicalFragment) {
		for _, c := range f.Children {
			if c.Fragment.Node == tall {
				tallHeights = append(tallHeights, c.Fragment.Size.HeightF64())
			}
			walk(c.Fragment)
		}
	}
	walk(result.Fragment)
	want := []float64{10, 50, 50, 50}
	if len(tallHeights) != len(want) {
		t.Fatalf("160px child fragments = %v, want %v", tallHeights, want)
	}
	for i := range want {
		if tallHeights[i] != want[i] {
			t.Errorf("160px child fragments = %v, want %v", tallHeights, want)
			break
		}
	}
}
