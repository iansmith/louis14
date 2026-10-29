package layout

import (
	"slices"
	"testing"

	"louis14/pkg/html"
)

// blockEndEdgesByID returns, per id, the block-end border+padding each
// fragment of the node paints (its box data), in tree order.
func blockEndEdgesByID(frag *PhysicalFragment, ids map[*html.Node]string) map[string][]float64 {
	out := map[string][]float64{}
	walkFragmentsByID(frag, ids, func(id string, _, _ float64, f *PhysicalFragment) {
		edge := 0.0
		if f.BoxData != nil {
			edge = f.BoxData.Border.Bottom + f.BoxData.Padding.Bottom
		}
		out[id] = append(out[id], edge)
	})
	return out
}

// TestBreakingFragment_BlockEndEdge pins, against Chrome 155, that in slice
// mode a fragment that breaks carries no block-end border/padding — neither in
// its size nor in what it paints — on every breaking path: a block child
// breaking, a line break, a self-break of a definite-height box, a forced
// break, a column spanner interrupting the box, a past-end (parallel-flow)
// fragment, and a breaking new formatting context. Every fixture is a
// 100x100 multicol with column-count 2 and column-gap 0 whose single child is
// C. EDGE means padding:10px 0 plus border-bottom:5px solid.
//
// `rects` pins Chrome's getClientRects() relative to the multicol border box;
// `heights` pins only fragment heights, notChecked skipping a fragment whose
// size is out of scope here; `edges` pins the block-end border+padding each
// fragment paints (Chrome draws the edge only on the last fragment).
//
// Blink @ a9f50e522efa: ShouldIncludeBlockEndBorderPadding
// (fragmentation_utils.cc:527-545) decides the edge, FinishFragmentation
// sizes the fragment and may break before trailing border/padding
// (:559-662), ConsumeRemainingFragmentainerSpace stretches a breaking
// fragment to the fragmentainer end (block_layout_algorithm.cc:3084-3095),
// and the sides mask stops the edge from painting (fragmentation_utils.cc:
// 773-779).
func TestBreakingFragment_BlockEndEdge(t *testing.T) {
	bottomEdge := []string{"padding-bottom", "10px", "border-bottom-width", "5px", "border-bottom-style", "solid"}
	with := func(base []string, kv ...string) []string { return append(slices.Clone(base), kv...) }
	edge := func(kv ...string) []string {
		return with(bottomEdge, append([]string{"padding-top", "10px"}, kv...)...)
	}
	h := func(v string, kv ...string) []string { return append([]string{"height", v}, kv...) }
	clone := []string{"box-decoration-break", "clone"}
	selfBreak := []string{"height", "85px", "padding-top", "10px", "padding-bottom", "10px"}
	// Seven 50x20 inline-block lines (each fills a 50px line) in a block
	// with the given padding-top, padding-bottom 10 and border-bottom 5.
	lines := func(padTop string, kv ...string) marginSpec {
		var kids []marginSpec
		for range 7 {
			kids = append(kids, ms("line", []string{"display", "inline-block", "width", "50px", "height", "20px", "vertical-align", "top"}))
		}
		return ms("C", with(bottomEdge, append([]string{"padding-top", padTop, "font-size", "0", "line-height", "0"}, kv...)...), kids...)
	}
	// C (padding-bottom 10, border-bottom 5) holding A 40, a column-span:all
	// SP 20 and B 45.
	span := func() marginSpec {
		return ms("C", bottomEdge,
			ms("A", h("40px")),
			ms("SP", h("20px", "column-span", "all")),
			ms("B", h("45px")))
	}
	for _, tc := range []struct {
		name    string
		balance bool // column-fill:balance; auto otherwise
		tree    marginSpec
		rects   map[string][]fragRect
		heights map[string][]float64
		edges   map[string][]float64
	}{
		// --- A block child breaks.
		{name: "f1 child break: no end edge on the breaking fragment",
			tree:  ms("C", edge(), ms("K", h("150px"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 75}}, "K": {{0, 10, 50, 90}, {50, 0, 50, 60}}},
			edges: map[string][]float64{"C": {0, 15}}},
		{name: "f2 balanced child break: no false end-edge shortage", balance: true,
			tree:  ms("C", edge(), ms("K", h("155px"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 90}, {50, 0, 50, 90}}, "K": {{0, 10, 50, 80}, {50, 0, 50, 75}}}},
		{name: "f2b balanced child break in a new formatting context", balance: true,
			tree:  ms("C", edge("display", "flow-root"), ms("K", h("155px"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 90}, {50, 0, 50, 90}}, "K": {{0, 10, 50, 80}, {50, 0, 50, 75}}}},
		{name: "f14 definite-height child break paints no end edge",
			tree:  ms("C", edge("height", "150px"), ms("K", h("150px"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 75}}},
			edges: map[string][]float64{"C": {0, 15}}},

		// --- A line break.
		{name: "f3 line break: no end edge on the breaking fragment",
			tree:  lines("20px"),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 75}}},
			edges: map[string][]float64{"C": {0, 15}}},
		{name: "f19 line break stretches the breaking fragment to the column end",
			tree:  lines("10px"),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 75}}}},
		{name: "f3b balanced line break keeps its column height", balance: true,
			tree:  lines("20px"),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 95}, {50, 0, 50, 95}}}},
		{name: "f4 line break in a definite-height box fills the column",
			tree:    lines("20px", "height", "150px"),
			heights: map[string][]float64{"C": {100, notChecked}},
			edges:   map[string][]float64{"C": {0, notChecked}}},

		// --- A definite-height box breaks itself.
		{name: "f5 self-break: the slice drops the end edge",
			tree:  ms("C", edge("height", "150px")),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 75}}},
			edges: map[string][]float64{"C": {0, 15}}},
		{name: "f5b balanced self-break fits two columns", balance: true,
			tree:  ms("C", edge("height", "155px")),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 90}, {50, 0, 50, 90}}}},
		{name: "f6 self-break before trailing padding",
			tree:  ms("C", selfBreak),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 95}, {50, 0, 50, 10}}}},
		{name: "f5c cloned decorations keep both edges",
			tree:  ms("C", with(edge("height", "150px"), clone...)),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 100}}}},
		{name: "f6b cloned decorations do not break before trailing padding",
			tree:  ms("C", with(selfBreak, clone...)),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 25}}}},

		// --- A forced break, and a break before a pushed child.
		{name: "f10b forced break: no end edge on the breaking fragment",
			tree: ms("C", edge(), ms("A", h("90px")), ms("B", h("10px", "break-before", "column"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 100}, {50, 0, 50, 25}},
				"A": {{0, 10, 50, 90}}, "B": {{50, 0, 50, 10}}},
			edges: map[string][]float64{"C": {0, 15}}},
		{name: "f15 balanced nested forced break excludes the end edge", balance: true,
			tree: ms("C", edge(), ms("W", nil, ms("A", h("70px")), ms("B", h("10px", "break-before", "column")))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 80}, {50, 0, 50, 25}},
				"W": {{0, 10, 50, 70}, {50, 0, 50, 10}}}},
		{name: "f16 break before a pushed child in a definite-height box fills the column",
			tree:    ms("C", edge("height", "150px"), ms("F", h("30px")), ms("K", h("80px", "break-inside", "avoid"))),
			rects:   map[string][]fragRect{"F": {{0, 10, 50, 30}}, "K": {{50, 0, 50, 80}}},
			heights: map[string][]float64{"C": {100, notChecked}},
			edges:   map[string][]float64{"C": {0, notChecked}}},
		{name: "f20 balanced: a pushed avoid box reports its shortage", balance: true,
			tree:  ms("C", nil, ms("F", h("30px")), ms("K", h("80px", "break-inside", "avoid"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 80}, {50, 0, 50, 80}}, "K": {{50, 0, 50, 80}}}},

		// --- A column spanner interrupts the box.
		{name: "f7 spanner: fragments before it carry no end edge", balance: true,
			tree: span(),
			rects: map[string][]fragRect{
				"C":  {{0, 0, 50, 20}, {50, 0, 50, 20}, {0, 40, 50, 30}, {50, 40, 50, 30}},
				"SP": {{0, 20, 100, 20}},
				"B":  {{0, 40, 50, 30}, {50, 40, 50, 15}}}},
		{name: "f7b spanner nested in a wrapper", balance: true,
			tree: ms("W", nil, span()),
			rects: map[string][]fragRect{
				"W":  {{0, 0, 50, 20}, {50, 0, 50, 20}, {0, 40, 50, 30}, {50, 40, 50, 30}},
				"C":  {{0, 0, 50, 20}, {50, 0, 50, 20}, {0, 40, 50, 30}, {50, 40, 50, 30}},
				"SP": {{0, 20, 100, 20}}}},
		{name: "f17 spanner nested in a definite-height box", balance: true,
			tree: ms("C", with(bottomEdge, "height", "120px"),
				ms("W", nil, ms("A", h("40px")), ms("SP", h("20px", "column-span", "all")), ms("B", h("20px")))),
			heights: map[string][]float64{"C": {20, 20, notChecked, notChecked}},
			edges:   map[string][]float64{"C": {0, 0, notChecked, notChecked}}},

		// --- Past the box's block end (parallel flow).
		{name: "f8 past-end fragment has no end edge",
			tree:  ms("C", edge("height", "50px"), ms("K", h("150px"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 75}, {50, 0, 50, 0}}, "K": {{0, 10, 50, 90}, {50, 0, 50, 60}}},
			edges: map[string][]float64{"C": {15, 0}}},
		{name: "f9 past-end fragment with cloned decorations has neither edge",
			tree:  ms("C", with(edge("height", "50px"), clone...), ms("K", h("150px"))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 75}, {50, 0, 50, 0}}, "K": {{0, 10, 50, 90}, {50, 0, 50, 60}}}},
		{name: "f12 new formatting context whose child continues in a parallel flow",
			tree:  ms("C", edge("display", "flow-root"), ms("K", h("50px"), ms("G", h("150px")))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 60}, {50, 0, 50, 0}}, "G": {{0, 10, 50, 90}, {50, 0, 50, 60}}}},
		{name: "f13 block whose child continues in a parallel flow keeps its end edge",
			tree:  ms("C", edge(), ms("K", h("50px"), ms("G", h("150px")))),
			rects: map[string][]fragRect{"C": {{0, 0, 50, 75}, {50, 0, 50, 0}}, "G": {{0, 10, 50, 90}, {50, 0, 50, 60}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutTwoColumnSpec(t, tc.tree, tc.balance)
			got := borderBoxRectsByID(frag, ids)
			checkRects(t, got, tc.rects)
			for id, want := range tc.heights {
				var hs []float64
				for _, r := range got[id] {
					hs = append(hs, r.h)
				}
				if !slices.EqualFunc(hs, want, matchesUnlessSkipped) {
					t.Errorf("%s fragment heights = %v, want %v (%v = not checked; Chrome 155)", id, hs, want, notChecked)
				}
			}
			gotEdges := blockEndEdgesByID(frag, ids)
			for id, want := range tc.edges {
				if !slices.EqualFunc(gotEdges[id], want, matchesUnlessSkipped) {
					t.Errorf("%s painted block-end border+padding = %v, want %v (%v = not checked; Chrome 155)", id, gotEdges[id], want, notChecked)
				}
			}
		})
	}
}

// notChecked in a `heights` or `edges` entry skips that fragment.
const notChecked = -1

func matchesUnlessSkipped(got, want float64) bool {
	return want == notChecked || got == want
}

// TestEffectiveBlockBorderPadding_BreakInsideZeroConsumed: a continuation is
// any break-inside resume, even one that consumed nothing, and a past-end
// (parallel-flow) fragment carries neither block edge. Blink decides both
// from the break token, not from consumed size: ShouldIncludeBlockStart
// BorderPadding (fragmentation_utils.cc:522-525) and
// ShouldIncludeBlockEndBorderPadding (:527-545) @ a9f50e522efa.
func TestEffectiveBlockBorderPadding_BreakInsideZeroConsumed(t *testing.T) {
	geom := FragmentGeometry{
		Padding: LogicalEdges{BlockStart: 8, BlockEnd: 8},
	}
	if got := EffectiveBlockBorderPadding(geom, false, &BlockBreakToken{}, false); got != 8 {
		t.Errorf("zero-consumed break-inside continuation = %v, want 8 (block-end edge only)", got)
	}
	if got := EffectiveBlockBorderPadding(geom, true, &BlockBreakToken{IsAtBlockEnd: true}, false); got != 0 {
		t.Errorf("past-end fragment with cloned decorations = %v, want 0 (neither edge)", got)
	}
}
