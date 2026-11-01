package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestInflowChildBreakStop pins Blink's general in-flow child loop stop
// against Chrome 155 geometry (getClientRects() relative to the multicol
// border box, headless, 2026-10-31). Blink stops laying out siblings only
// when a child broke inside in the same flow (HasInflowChildBreakInside,
// block_layout_algorithm.cc:1108-1115 @ a9f50e522efa; set by
// PropagateBreakInfo for a break token that is not at the child's block end,
// box_fragment_builder.cc:507-510, :567-570). A child whose box ended in the
// fragmentainer while its content overflows continues in a parallel flow and
// leaves the walk going; whether the next sibling still fits is decided by
// that sibling's BreakBeforeChildIfNeeded (fragmentation_utils.cc:895-942).
func TestInflowChildBreakStop(t *testing.T) {
	t.Run("parallel flow at the fragmentainer end: next sibling breaks before", func(t *testing.T) {
		// <div columns:2; column-fill:auto; height:100px; gap:0; width:200px>
		//   <div a height:100px><div a1 height:240px></div></div>
		//   <div b height:50px></div>
		a1 := makeNode("div")
		a := makeNode("div", a1)
		b := makeNode("div")
		mc := makeNode("div", a, b)
		styles := map[*html.Node]*css.Style{
			mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
				"column-fill", "auto", "width", "200px", "height", "100px"),
			a:  makeStyle("display", "block", "height", "100px"),
			a1: makeStyle("display", "block", "height", "240px"),
			b:  makeStyle("display", "block", "height", "50px"),
		}
		ids := map[*html.Node]string{a: "a", a1: "a1", b: "b"}
		got := borderBoxRectsByID(layoutMulticolForTest(t, mc, styles).Fragment, ids)
		checkRects(t, got, map[string][]fragRect{
			"a":  {{0, 0, 100, 100}, {100, 0, 100, 0}, {200, 0, 100, 0}},
			"a1": {{0, 0, 100, 100}, {100, 0, 100, 100}, {200, 0, 100, 40}},
			"b":  {{100, 0, 100, 50}},
		})
	})

	t.Run("parallel flow short of the fragmentainer end: next sibling follows it", func(t *testing.T) {
		// <div columns:2; column-fill:auto; height:100px; gap:0; width:200px>
		//   <div p height:60px></div>
		//   <div q height:20px><div q1 height:100px></div></div>
		//   <div r height:30px></div>
		p := makeNode("div")
		q1 := makeNode("div")
		q := makeNode("div", q1)
		r := makeNode("div")
		mc := makeNode("div", p, q, r)
		styles := map[*html.Node]*css.Style{
			mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
				"column-fill", "auto", "width", "200px", "height", "100px"),
			p:  makeStyle("display", "block", "height", "60px"),
			q:  makeStyle("display", "block", "height", "20px"),
			q1: makeStyle("display", "block", "height", "100px"),
			r:  makeStyle("display", "block", "height", "30px"),
		}
		ids := map[*html.Node]string{p: "p", q: "q", q1: "q1", r: "r"}
		got := borderBoxRectsByID(layoutMulticolForTest(t, mc, styles).Fragment, ids)
		checkRects(t, got, map[string][]fragRect{
			"p":  {{0, 0, 100, 60}},
			"q":  {{0, 60, 100, 20}, {100, 0, 100, 0}},
			"q1": {{0, 60, 100, 40}, {100, 0, 100, 60}},
			"r":  {{0, 80, 100, 20}, {100, 0, 100, 10}},
		})
	})
}
