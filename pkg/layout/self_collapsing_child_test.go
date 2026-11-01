package layout

import "testing"

// TestSelfCollapsingChild_ChromeGeometry_LOU445 pins self-collapsing
// (zero-block-size, margin-collapsing-through) block children against Chrome
// 155.0.8059.41 (headless, getClientRects relative to the multicol / root).
//
// Blink lays a self-collapsing child out like any in-flow child: it is
// positioned at its parent's BFC offset estimate plus the margins collapsed
// up to it (PositionSelfCollapsingChildWithParentBfc,
// block_layout_algorithm.cc:3066-3081 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), considered for a break before it
// (BreakBeforeChildIfNeeded in FinishInflow, :2622-2653), added to the
// fragment tree with its break values propagated (AddResult, :2732-2736;
// box_fragment_builder.cc:212-232), and only then passed through by the
// inflow position (ComputeInflowPosition, :2894-2898).
func TestSelfCollapsingChild_ChromeGeometry_LOU445(t *testing.T) {
	h := func(v string, more ...string) []string { return append([]string{"height", v}, more...) }
	auto2 := []string{"width", "200px", "column-count", "2", "column-gap", "0", "column-fill", "auto", "height", "100px"}
	bal2 := []string{"width", "200px", "column-count", "2", "column-gap", "0", "column-fill", "balance"}
	for _, tc := range []struct {
		name string
		mc   []string // nil: lay out in a 200px flow-root instead
		kids []marginSpec
		want map[string][]fragRect
	}{
		{
			// Chrome probe nr.html: an empty break-before:column block moves
			// the next box to column 2.
			name: "NR_empty_forced_break_before_auto",
			mc:   auto2,
			kids: []marginSpec{
				ms("a", h("20px")),
				ms("e", props("break-before", "column")),
				ms("b", h("20px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 100, 20}},
				"e": {{100, 0, 100, 0}},
				"b": {{100, 0, 100, 20}},
			},
		},
		{
			// Chrome probe p1.html: the same, balanced.
			name: "P1_empty_forced_break_before_balanced",
			mc:   bal2,
			kids: []marginSpec{
				ms("a", h("20px")),
				ms("e", props("break-before", "column")),
				ms("b", h("20px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 100, 20}},
				"e": {{100, 0, 100, 0}},
				"b": {{100, 0, 100, 20}},
			},
		},
		{
			// Chrome probe p2.html: margins after the forced break are kept
			// and collapse through the empty block.
			name: "P2_forced_break_keeps_margins",
			mc:   auto2,
			kids: []marginSpec{
				ms("a", h("20px", "margin-bottom", "10px")),
				ms("e", props("break-before", "column", "margin-top", "30px", "margin-bottom", "30px")),
				ms("b", h("20px", "margin-top", "5px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 100, 20}},
				"e": {{100, 30, 100, 0}},
				"b": {{100, 30, 100, 20}},
			},
		},
		{
			// Chrome probe p3.html (guard): a forced break before the first
			// child is no class A breakpoint.
			name: "P3_first_child_forced_break_ignored",
			mc:   auto2,
			kids: []marginSpec{
				ms("e", props("break-before", "column")),
				ms("b", h("20px")),
			},
			want: map[string][]fragRect{
				"e": {{0, 0, 100, 0}},
				"b": {{0, 0, 100, 20}},
			},
		},
		{
			// Chrome probe p4.html: an empty block's break-after joins the
			// next sibling's break-before.
			name: "P4_empty_forced_break_after",
			mc:   auto2,
			kids: []marginSpec{
				ms("a", h("20px")),
				ms("e", props("break-after", "column")),
				ms("b", h("20px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 100, 20}},
				"e": {{0, 20, 100, 0}},
				"b": {{100, 0, 100, 20}},
			},
		},
		{
			// Chrome probe p5.html: the break-before of an empty block's
			// first (empty) child propagates to the wrapper.
			name: "P5_nested_empty_forced_break_before",
			mc:   auto2,
			kids: []marginSpec{
				ms("a", h("20px")),
				ms("w", nil, ms("e", props("break-before", "column"))),
				ms("b", h("20px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 100, 20}},
				"w": {{100, 0, 100, 0}},
				"e": {{100, 0, 100, 0}},
				"b": {{100, 0, 100, 20}},
			},
		},
		{
			// Chrome probe p6.html: an empty block sits where the margins
			// collapsed up to it resolve.
			name: "P6_position_at_collapsed_margins",
			mc:   auto2,
			kids: []marginSpec{
				ms("a", h("20px", "margin-bottom", "10px")),
				ms("e", props("margin-top", "30px", "margin-bottom", "30px")),
				ms("b", h("20px", "margin-top", "5px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 100, 20}},
				"e": {{0, 50, 100, 0}},
				"b": {{0, 50, 100, 20}},
			},
		},
		{
			// Chrome probe q1.html: a nested empty block; its own
			// block-end margin does not move it.
			name: "Q1_nested_empty_in_flow_root",
			kids: []marginSpec{
				ms("a", h("20px", "margin-bottom", "10px")),
				ms("w", props("margin-top", "15px", "margin-bottom", "5px"),
					ms("e", props("margin-top", "20px", "margin-bottom", "40px"))),
				ms("b", h("20px", "margin-top", "5px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 200, 20}},
				"w": {{0, 60, 200, 0}},
				"e": {{0, 60, 200, 0}},
				"b": {{0, 60, 200, 20}},
			},
		},
		{
			// Chrome probe q3.html: negative margins; the empty block sits
			// before its own negative block-end margin applies.
			name: "Q3_negative_margins_in_flow_root",
			kids: []marginSpec{
				ms("a", h("20px", "margin-bottom", "-10px")),
				ms("e", props("margin-top", "30px", "margin-bottom", "-25px")),
				ms("b", h("20px")),
			},
			want: map[string][]fragRect{
				"a": {{0, 0, 200, 20}},
				"e": {{0, 40, 200, 0}},
				"b": {{0, 25, 200, 20}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.mc == nil {
				frag, ids := layoutInFlowRoot(t, ms("root", nil, tc.kids...))
				checkRects(t, borderBoxRectsByID(frag, ids), tc.want)
				return
			}
			frag, ids := layoutColumnsSpec(t, tc.mc, tc.kids...)
			checkRects(t, borderBoxRectsByID(frag, ids), tc.want)
		})
	}
}

// TestSelfCollapsingChildClearance_ChromeGeometry_LOU445 pins clearance of a
// self-collapsing child against Chrome 155.0.8059.41. Blink decides it from
// the child's hypothetical border edge past every margin collapsed up to it,
// inner margins included (FinishInflow, block_layout_algorithm.cc:2440-2454);
// with clearance the following content starts at the clearance offset minus
// the child's own end margin strut (ComputeInflowPosition, :2900-2950), and
// that strut then stays inside the parent (FinishLayout, :1287-1294 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestSelfCollapsingChildClearance_ChromeGeometry_LOU445(t *testing.T) {
	float := ms("f", props("float", "left", "width", "100px", "height", "100px"))
	for _, tc := range []struct {
		name string
		kids []marginSpec
		want map[string][]fragRect
	}{
		{
			// Chrome probe c1.html: the inner 100px margin alone clears the
			// float, so there is no clearance.
			name: "C1_inner_margin_reaches_float",
			kids: []marginSpec{
				float,
				ms("c", props("clear", "both"), ms("e", props("margin-bottom", "100px"))),
				ms("b", props("height", "10px")),
			},
			want: map[string][]fragRect{
				"c": {{0, 100, 200, 0}},
				"e": {{0, 100, 200, 0}},
				"b": {{0, 100, 200, 10}},
			},
		},
		{
			// Chrome probe c2.html: 75px falls short; 25px clearance.
			name: "C2_inner_margin_short_of_float",
			kids: []marginSpec{
				float,
				ms("c", props("clear", "both"), ms("e", props("margin-bottom", "75px"))),
				ms("b", props("height", "10px")),
			},
			want: map[string][]fragRect{
				"c": {{0, 100, 200, 0}},
				"e": {{0, 100, 200, 0}},
				"b": {{0, 100, 200, 10}},
			},
		},
		{
			// Chrome probe c3.html: the cleared child's own strut {10, 40}
			// collapses with the next sibling's 20px margin.
			name: "C3_cleared_strut_collapses_with_sibling",
			kids: []marginSpec{
				float,
				ms("c", props("clear", "both", "margin-top", "10px"), ms("e", props("margin-bottom", "40px"))),
				ms("b", props("height", "10px", "margin-top", "20px")),
			},
			want: map[string][]fragRect{
				"c": {{0, 100, 200, 0}},
				"e": {{0, 100, 200, 0}},
				"b": {{0, 100, 200, 10}},
			},
		},
		{
			// Chrome probe c4.html: a trailing cleared self-collapsing child
			// keeps its end margin strut inside the wrapper.
			name: "C4_trailing_cleared_child_sizes_parent",
			kids: []marginSpec{
				ms("w", nil, float,
					ms("c", props("clear", "both"), ms("e", props("margin-bottom", "75px")))),
				ms("b", props("height", "10px", "margin-top", "30px")),
			},
			want: map[string][]fragRect{
				"w": {{0, 0, 200, 100}},
				"c": {{0, 100, 200, 0}},
				"e": {{0, 100, 200, 0}},
				"b": {{0, 130, 200, 10}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutInFlowRoot(t, ms("root", nil, tc.kids...))
			checkRects(t, borderBoxRectsByID(frag, ids), tc.want)
		})
	}
}
