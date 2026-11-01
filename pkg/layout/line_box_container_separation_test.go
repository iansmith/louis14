package layout

import "testing"

// TestLineBox_ContainerSeparation pins, against Chrome 155, that only a
// line box with non-zero block-size gives the line after it container
// separation. A block C (padding-top, inline-block lines, orphans/widows 1)
// follows a 50px sibling A in a 100x100 two-column multicol with gap 0 and
// a wrapper R. When C's first non-zero-height line does not fit, breaking
// before it has no container separation (last-resort appeal), so R breaks
// before C instead and C moves whole to column 2; once a non-zero line is
// placed, the break between lines is perfect and C breaks inside.
//
// Blink @ a9f50e522efa: HasBreakOpportunityBeforeNextChild's line-box branch
// (fragmentation_utils.cc:885-892), updated after every line in HandleInflow
// (block_layout_algorithm.cc:2767-2772); CalculateBreakAppealBefore
// (fragmentation_utils.cc:235-266) gives last resort without separation;
// MovePastBreakpoint (:1141-1240) then prefers the break before C.
func TestLineBox_ContainerSeparation(t *testing.T) {
	h := func(v string) []string { return []string{"height", v} }
	line := func(id, height string) marginSpec {
		return ms(id, []string{"display", "inline-block", "width", "50px", "height", height, "vertical-align", "top"})
	}
	c := func(padTop string, lines ...marginSpec) marginSpec {
		return ms("C", []string{"padding-top", padTop, "font-size", "0", "line-height", "0", "orphans", "1", "widows", "1"}, lines...)
	}
	for _, tc := range []struct {
		name    string
		balance bool
		tree    marginSpec
		rects   map[string][]fragRect
	}{
		{name: "first line does not fit: break before the block",
			tree: ms("R", nil, ms("A", h("50px")), c("30px", line("l1", "30px"), line("l2", "30px"))),
			rects: map[string][]fragRect{"R": {{0, 0, 50, 100}, {50, 0, 50, 90}}, "A": {{0, 0, 50, 50}},
				"C": {{50, 0, 50, 90}}, "l1": {{50, 30, 50, 30}}, "l2": {{50, 60, 50, 30}}}},
		{name: "a zero-height line gives no separation",
			tree: ms("R", nil, ms("A", h("50px")), c("30px", line("l0", "0px"), line("l1", "30px"), line("l2", "30px"))),
			rects: map[string][]fragRect{"R": {{0, 0, 50, 100}, {50, 0, 50, 90}}, "A": {{0, 0, 50, 50}},
				"C": {{50, 0, 50, 90}}, "l0": {{50, 30, 50, 0}}, "l1": {{50, 30, 50, 30}}, "l2": {{50, 60, 50, 30}}}},
		{name: "after a non-zero line the block breaks inside",
			tree: ms("R", nil, ms("A", h("20px")), c("30px", line("l1", "30px"), line("l2", "30px"), line("l3", "30px"))),
			rects: map[string][]fragRect{"A": {{0, 0, 50, 20}}, "C": {{0, 20, 50, 80}, {50, 0, 50, 60}},
				"l1": {{0, 50, 50, 30}}, "l2": {{50, 0, 50, 30}}, "l3": {{50, 30, 50, 30}}}},
		{name: "balanced: first line does not fit", balance: true,
			tree: ms("R", nil, ms("A", h("50px")), c("30px", line("l1", "30px"), line("l2", "30px"))),
			rects: map[string][]fragRect{"R": {{0, 0, 50, 90}, {50, 0, 50, 90}}, "A": {{0, 0, 50, 50}},
				"C": {{50, 0, 50, 90}}, "l1": {{50, 30, 50, 30}}, "l2": {{50, 60, 50, 30}}}},
		{name: "balanced: a zero-height line gives no separation", balance: true,
			tree: ms("R", nil, ms("A", h("50px")), c("30px", line("l0", "0px"), line("l1", "30px"), line("l2", "30px"))),
			rects: map[string][]fragRect{"R": {{0, 0, 50, 90}, {50, 0, 50, 90}}, "A": {{0, 0, 50, 50}},
				"C": {{50, 0, 50, 90}}, "l0": {{50, 30, 50, 0}}, "l1": {{50, 30, 50, 30}}, "l2": {{50, 60, 50, 30}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutTwoColumnSpec(t, tc.tree, tc.balance)
			checkRects(t, borderBoxRectsByID(frag, ids), tc.rects)
		})
	}
}
