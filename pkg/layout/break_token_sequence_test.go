package layout

import "testing"

// TestBreakToken_SequenceNumberAfterBreakBefore pins, against Chrome 155,
// that a box pushed by a break before it starts its fragment count afresh:
// a break-before token is not a fragment of the box, so the box's first
// fragment still has sequence number 0 and its next fragment repeats its
// cloned box decorations once, not twice. Each fixture is a 150x100
// three-column multicol (gap 0, column-fill:auto) holding a 20px A and a
// box C with box-decoration-break:clone, padding 10px 0 and
// break-before:column, so C starts in column 2 and breaks into column 3.
//
// Blink @ a9f50e522efa: SetupFragmentBuilderForFragmentation leaves the
// sequence number at 0 after a break-before token
// (fragmentation_utils.cc:426-430); UpdateBorderPaddingForClonedBoxDecorations
// counts the cloned copies from it (fragmentation_utils.cc:440-449).
func TestBreakToken_SequenceNumberAfterBreakBefore(t *testing.T) {
	mc := func(c marginSpec) marginSpec {
		return ms("mc", props("width", "150px", "height", "100px", "column-count", "3", "column-gap", "0", "column-fill", "auto"),
			ms("A", props("height", "20px")), c)
	}
	clone := func(kv ...string) []string {
		return append([]string{"box-decoration-break", "clone", "padding-top", "10px", "padding-bottom", "10px", "break-before", "column"}, kv...)
	}
	line := func(id string) marginSpec {
		return ms(id, props("display", "inline-block", "width", "50px", "height", "30px", "vertical-align", "top"))
	}
	twoFragments := []fragRect{{50, 0, 50, 100}, {100, 0, 50, 90}}
	for _, tc := range []struct {
		name  string
		tree  marginSpec
		rects map[string][]fragRect
	}{
		{name: "definite-height box breaking itself",
			tree:  mc(ms("C", clone("height", "150px"))),
			rects: map[string][]fragRect{"C": twoFragments}},
		{name: "definite-height box with a breaking child",
			tree:  mc(ms("C", clone("height", "150px"), ms("K", props("height", "150px")))),
			rects: map[string][]fragRect{"C": twoFragments, "K": {{50, 10, 50, 80}, {100, 10, 50, 70}}}},
		{name: "definite-height box with breaking lines",
			tree: mc(ms("C", clone("height", "150px", "font-size", "0", "line-height", "0", "orphans", "1", "widows", "1"),
				line("l1"), line("l2"), line("l3"), line("l4"), line("l5"))),
			rects: map[string][]fragRect{"C": twoFragments,
				"l1": {{50, 10, 50, 30}}, "l2": {{50, 40, 50, 30}}, "l3": {{100, 10, 50, 30}}, "l4": {{100, 40, 50, 30}}, "l5": {{100, 70, 50, 30}}}},
		{name: "nested multicol",
			tree: mc(ms("C", clone("height", "200px", "column-count", "2", "column-gap", "0", "column-fill", "auto"),
				ms("K", props("height", "300px")))),
			rects: map[string][]fragRect{"C": {{50, 0, 50, 100}, {100, 0, 50, 100}, {150, 0, 50, 60}},
				"K": {{50, 10, 25, 80}, {75, 10, 25, 80}, {100, 10, 25, 80}, {125, 10, 25, 60}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutInFlowRoot(t, tc.tree)
			checkRects(t, borderBoxRectsByID(frag, ids), tc.rects)
		})
	}
}
