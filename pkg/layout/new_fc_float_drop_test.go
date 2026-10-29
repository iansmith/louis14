package layout

import (
	"testing"

	"louis14/pkg/html"
)

// TestNewFC_DroppedBelowFloat pins, against Chrome 155, where a new
// formatting context lands when it cannot fit beside floats. Blink places
// the border box at the start of the first layout opportunity that fits and
// never re-adds the block-start margin (child_bfc_offset =
// opportunity.rect.BlockStartOffset(), block_layout_algorithm.cc:2228-2229);
// the search starts from the adjoining estimate, the cursor plus the pending
// strut collapsed with the child's margin (:1854-1861); the fit test uses the
// border box only (:2180); afterwards the cursor is the border-box end and
// the strut its margin-block-end (ComputeInflowPosition, :2953, :2983-2991)
// @ a9f50e522efa.
//
// Flow cases sit in a 200px flow-root root; multicol cases in the 100x100
// two-column multicol (column-fill:auto). Every rect is relative to that
// root.
func TestNewFC_DroppedBelowFloat(t *testing.T) {
	flowRoot := func(w string, kv ...string) []string {
		return append([]string{"display", "flow-root", "width", w}, kv...)
	}
	float := func(side, w, h string) []string {
		return []string{"float", side, "width", w, "height", h}
	}
	for _, tc := range []struct {
		name      string
		inColumns bool
		tree      marginSpec
		rects     map[string][]fragRect
	}{
		// --- The margin is not applied a second time below the float.
		{name: "r01 explicit width (pre-layout drop)",
			tree: ms("c", flowRoot("50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "40px")), ms("a", props("height", "10px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 20}}, "a": {{0, 80, 50, 10}}, "c": {{0, 0, 50, 90}}}},
		{name: "r02 max-content (post-layout drop and re-layout)",
			tree: ms("c", flowRoot("50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("max-content", "margin-top", "40px"), ms("i", props("width", "80px", "height", "20px"))),
				ms("a", props("height", "10px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 20}}, "a": {{0, 80, 50, 10}}, "c": {{0, 0, 50, 90}}}},
		{name: "u01 block container",
			tree: ms("c", props("width", "50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "40px")), ms("a", props("height", "10px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 20}}, "a": {{0, 80, 50, 10}}, "c": {{0, 0, 50, 90}}}},

		// --- Variants.
		{name: "r03 margin past the float end: no drop",
			tree: ms("c", flowRoot("50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "80px"))),
			rects: map[string][]fragRect{"n": {{0, 80, 80, 20}}, "c": {{0, 0, 50, 100}}}},
		{name: "r04 collapsed with the previous margin-bottom",
			tree: ms("c", flowRoot("50px"), ms("p", props("height", "10px", "margin-bottom", "30px")),
				ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "20px"))),
			rects: map[string][]fragRect{"f": {{0, 40, 50, 60}}, "n": {{0, 100, 80, 20}}, "c": {{0, 0, 50, 120}}}},
		{name: "r05 negative margin does not fit above the float",
			tree: ms("c", flowRoot("50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "30px", "margin-top", "-20px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 30}}, "c": {{0, 0, 50, 90}}}},
		{name: "r06 negative margin fits above the float",
			tree: ms("c", flowRoot("50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "-20px"))),
			rects: map[string][]fragRect{"n": {{0, -20, 80, 20}}, "c": {{0, 0, 50, 60}}}},
		{name: "r07 negative margin: drop to a float end above the cursor",
			tree: ms("c", flowRoot("50px"), ms("p", props("height", "50px"), ms("f", float("left", "50px", "40px"))),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "-20px"))),
			rects: map[string][]fragRect{"n": {{0, 40, 80, 20}}, "c": {{0, 0, 50, 60}}}},
		{name: "r08 inline-end float",
			tree: ms("c", flowRoot("100px"), ms("f", float("right", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "40px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 20}}, "c": {{0, 0, 100, 80}}}},
		{name: "r09 rtl: float:left is inline-end",
			tree: ms("c", flowRoot("100px", "direction", "rtl"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "40px"))),
			rects: map[string][]fragRect{"n": {{20, 60, 80, 20}}, "c": {{0, 0, 100, 80}}}},
		{name: "r10 staggered floats",
			tree: ms("c", flowRoot("100px"), ms("f", float("left", "40px", "30px")), ms("g", float("right", "40px", "60px")),
				ms("n", flowRoot("50px", "height", "20px", "margin-top", "10px"))),
			rects: map[string][]fragRect{"n": {{0, 30, 50, 20}}, "c": {{0, 0, 100, 60}}}},
		{name: "r11 margin-bottom is not part of the fit",
			tree: ms("c", flowRoot("100px"), ms("f", float("left", "60px", "10px")), ms("g", float("left", "70px", "10px")),
				ms("n", flowRoot("40px", "height", "10px", "margin-bottom", "30px"))),
			rects: map[string][]fragRect{"n": {{60, 0, 40, 10}}, "c": {{0, 0, 100, 40}}}},
		{name: "r12 negative margin: float starts inside the border box",
			tree: ms("c", flowRoot("50px"), ms("p", props("height", "10px", "margin-bottom", "30px")),
				ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "-10px"))),
			rects: map[string][]fragRect{"n": {{0, 100, 80, 20}}, "c": {{0, 0, 50, 120}}}},
		{name: "r13 next sibling collapses only with margin-bottom",
			tree: ms("c", flowRoot("50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "height", "20px", "margin-top", "40px", "margin-bottom", "10px")),
				ms("a", props("height", "10px", "margin-top", "5px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 20}}, "a": {{0, 90, 50, 10}}, "c": {{0, 0, 50, 100}}}},
		{name: "r14 clearance then a drop",
			tree: ms("c", flowRoot("100px"), ms("f", float("left", "30px", "30px")), ms("g", float("right", "40px", "60px")),
				ms("n", flowRoot("80px", "clear", "left", "height", "20px", "margin-top", "10px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 20}}, "c": {{0, 0, 100, 80}}}},
		{name: "o01 orthogonal child (post-layout drop only)",
			tree: ms("c", flowRoot("50px"), ms("f", float("left", "50px", "60px")),
				ms("n", flowRoot("80px", "writing-mode", "vertical-rl", "height", "20px", "margin-top", "40px"))),
			rects: map[string][]fragRect{"n": {{0, 60, 80, 20}}, "c": {{0, 0, 50, 80}}}},

		// --- In columns: the drop decides breaks at the border edge.
		{name: "m01 fits below the float in column 1", inColumns: true,
			tree: ms("c", nil, ms("f", float("left", "30px", "70px")),
				ms("n", flowRoot("40px", "height", "20px", "margin-top", "20px"))),
			rects: map[string][]fragRect{"n": {{0, 70, 40, 20}}, "c": {{0, 0, 50, 90}}}},
		{name: "m02 slices at the column end", inColumns: true,
			tree: ms("c", nil, ms("f", float("left", "30px", "90px")),
				ms("n", flowRoot("40px", "height", "20px", "margin-top", "20px"))),
			rects: map[string][]fragRect{"n": {{0, 90, 40, 10}, {50, 0, 40, 10}}}},
		{name: "m05 post-layout drop re-lays out at the border edge", inColumns: true,
			tree: ms("c", props("display", "flow-root"), ms("f", float("left", "50px", "30px")),
				ms("n", flowRoot("max-content", "margin-top", "20px"), ms("i", props("width", "80px", "height", "150px")))),
			rects: map[string][]fragRect{"n": {{0, 30, 80, 70}, {50, 0, 80, 80}}}},
		{name: "m07 break-inside:avoid child fits below the float", inColumns: true,
			tree: ms("c", nil, ms("f", float("left", "50px", "40px")),
				ms("n", flowRoot("80px", "height", "60px", "margin-top", "20px", "break-inside", "avoid")),
				ms("a", props("height", "10px"))),
			rects: map[string][]fragRect{"n": {{0, 40, 80, 60}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var frag *PhysicalFragment
			var ids map[*html.Node]string
			if tc.inColumns {
				frag, ids = layoutTwoColumnSpec(t, tc.tree, false)
			} else {
				frag, ids = layoutInFlowRoot(t, tc.tree)
			}
			checkRects(t, borderBoxRectsByID(frag, ids), tc.rects)
		})
	}
}
