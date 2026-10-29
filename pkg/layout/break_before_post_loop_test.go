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

// TestBreakBeforeFragmentLaysOutOutOfFlow_LOU410: an abspos child placed
// before a break-before:column sibling is propagated from the fragment that
// breaks and laid out, as Blink propagates out-of-flow descendants in the
// single FinishLayout whichever way the child loop ended
// (block_layout_algorithm.cc:1108-1115, 2646-2659 @ a9f50e522efa).
//
// Chrome 155.0.8059.41 probe abs2.html (columns:2, column-gap:0, width:200px,
// position:relative; abs 10x10 at its static position): abs = (0,20,10,10).
func TestBreakBeforeFragmentLaysOutOutOfFlow_LOU410(t *testing.T) {
	frag, ids := layoutColumnsSpec(t,
		[]string{"width", "200px", "column-count", "2", "column-gap", "0", "position", "relative"},
		ms("W", nil,
			ms("a", props("height", "20px")),
			ms("abs", props("position", "absolute", "width", "10px", "height", "10px")),
			ms("b", props("break-before", "column", "height", "20px"))))
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"W":   {{0, 0, 100, 20}, {100, 0, 100, 20}},
		"abs": {{0, 20, 10, 10}},
		"b":   {{100, 0, 100, 20}},
	})
}

// TestBreakBeforeFragmentKeepsParallelFlow_LOU410: a fixed-height sibling
// whose content overflows into the next column leaves a parallel-flow
// continuation, which the fragment breaking before a later sibling must
// still carry: Blink's FinishLayout adds every child break token, the
// parallel flow's included, to the outgoing token whichever way the loop
// ended (block_layout_algorithm.cc:1108-1115 @ a9f50e522efa).
//
// Chrome 155.0.8059.41 probe pf.html (columns:2, column-gap:0, width:200px,
// height:50px, column-fill:auto): F = (0,0,100,30) | (100,0,100,0),
// inner = (0,0,100,50) | (100,0,100,30), b = (100,0,100,20).
func TestBreakBeforeFragmentKeepsParallelFlow_LOU410(t *testing.T) {
	frag, ids := layoutColumnsSpec(t,
		[]string{"width", "200px", "column-count", "2", "column-gap", "0", "height", "50px", "column-fill", "auto"},
		ms("W", nil,
			ms("F", props("height", "30px"), ms("inner", props("height", "80px"))),
			ms("b", props("break-before", "column", "height", "20px"))))
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"F":     {{0, 0, 100, 30}, {100, 0, 100, 0}},
		"inner": {{0, 0, 100, 50}, {100, 0, 100, 30}},
		"b":     {{100, 0, 100, 20}},
	})
}

// TestBreakBeforeFragmentCarriesListMarker_LOU410: a list item whose
// content before a break-before:column child has no baseline leaves its
// outside ::marker unpositioned; the break token carries it (Blink
// BlockBreakToken::has_unpositioned_list_marker_, set in FinishLayout
// whichever way the loop ended, block_layout_algorithm.cc:1108-1115 @
// a9f50e522efa), so the marker is placed with the continuation.
//
// Chrome 155.0.8059.41 screenshot probe li.html (columns:2, column-gap:0,
// width:200px; li display:list-item, children a and b 20px tall and empty,
// b break-before:column): the red marker's pixels span x=144-148, y=1-7
// with the multicol at x=50, i.e. outside the second column (x=150) at the
// top of li's second fragment; nothing in the first column.
func TestBreakBeforeFragmentCarriesListMarker_LOU410(t *testing.T) {
	frag, ids := layoutColumnsSpec(t,
		[]string{"width", "200px", "column-count", "2", "column-gap", "0"},
		ms("li", props("display", "list-item", "list-style-type", "decimal"),
			ms("a", props("height", "20px")),
			ms("b", props("break-before", "column", "height", "20px"))))
	var markersPerFragment []int
	walkFragmentsByID(frag, ids, func(id string, _, _ float64, f *PhysicalFragment) {
		if id != "li" {
			return
		}
		n := 0
		for _, ch := range f.Children {
			if ch.Fragment != nil && ch.Fragment.LayoutNode.IsMarkerNode() {
				n++
			}
		}
		markersPerFragment = append(markersPerFragment, n)
	})
	if len(markersPerFragment) != 2 || markersPerFragment[0] != 0 || markersPerFragment[1] != 1 {
		t.Errorf("markers per li fragment = %v, want [0 1] (Chrome 155: marker beside the second column)", markersPerFragment)
	}
}
