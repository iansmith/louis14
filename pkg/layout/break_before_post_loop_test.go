package layout

import "testing"

// TestBreakBeforeFragmentKeepsRelativeOffset_LOU410: a fragment that ends by
// breaking before a child finishes through the same FinishLayout as any
// other, so position:relative applies to it. In Blink, HandleInflow returns
// kSuccess on kBrokeBefore (block_layout_algorithm.cc:2646-2659 @
// a9f50e522efa), AddBreakBeforeChild sets has_inflow_child_break_inside_
// (box_fragment_builder.cc:121-143), and the child loop stops
// (block_layout_algorithm.cc:1108-1115) into the single FinishLayout.
//
// Chrome 155.0.8059.41 probe rel.html (columns:2, column-gap:0, width:200px):
// rel getClientRects = (0,10,100,20) | (100,10,100,20): both fragments are
// shifted down 10px; their border boxes are at y=0 before the shift.
func TestBreakBeforeFragmentKeepsRelativeOffset_LOU410(t *testing.T) {
	frag, ids := layoutColumnsSpec(t,
		[]string{"width", "200px", "column-count", "2", "column-gap", "0"},
		ms("rel", props("position", "relative", "top", "10px"),
			ms("a", props("height", "20px")),
			ms("b", props("break-before", "column", "height", "20px"))))
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"rel": {{0, 0, 100, 20}, {100, 0, 100, 20}},
		"a":   {{0, 0, 100, 20}},
		"b":   {{100, 0, 100, 20}},
	})
	var offsets []float64
	walkFragmentsByID(frag, ids, func(id string, _, _ float64, f *PhysicalFragment) {
		if id == "rel" {
			offsets = append(offsets, f.RelativeOffset.TopF64())
		}
	})
	if len(offsets) != 2 || offsets[0] != 10 || offsets[1] != 10 {
		t.Errorf("rel fragment relative offsets = %v, want [10 10] (Chrome 155)", offsets)
	}
}
