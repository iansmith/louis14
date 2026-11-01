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

// TestParallelFlowResumeLaysOutUnseenSiblings pins against Chrome 155 that a
// resumed child continuing only in a parallel flow (zero-height fragment at
// its block end) does not stop the walk even when the container has unseen
// siblings and no later child token: the unseen sibling lays out in the same
// column. Blink's loop stops on HasInflowChildBreakInside only
// (block_layout_algorithm.cc:1108-1115 @ a9f50e522efa); BlockChildIterator
// then advances past the last resumed child to the next unstarted sibling
// because the container token has not seen all children
// (block_child_iterator.cc:80-95).
//
// W breaks in flow at C2's forced break in column 1; in column 2, C2
// (height:0, known to fit) is at its block end with C3 overflowing, so W's
// zero-height fragment is at its block end too, a parallel flow.
func TestParallelFlowResumeLaysOutUnseenSiblings(t *testing.T) {
	// <div columns:3; column-fill:auto; height:100px; gap:0; width:300px>
	//   <div w>
	//     <div c1 height:20px></div>
	//     <div c2 break-before:column; height:0><div c3 height:250px></div></div>
	//   </div>
	//   <div s height:30px></div>
	c1 := makeNode("div")
	c3 := makeNode("div")
	c2 := makeNode("div", c3)
	w := makeNode("div", c1, c2)
	s := makeNode("div")
	mc := makeNode("div", w, s)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "3", "column-gap", "0",
			"column-fill", "auto", "width", "300px", "height", "100px"),
		w:  makeStyle("display", "block"),
		c1: makeStyle("display", "block", "height", "20px"),
		c2: makeStyle("display", "block", "break-before", "column", "height", "0"),
		c3: makeStyle("display", "block", "height", "250px"),
		s:  makeStyle("display", "block", "height", "30px"),
	}
	ids := map[*html.Node]string{c3: "c3", s: "s"}
	got := borderBoxRectsByID(layoutMulticolForTest(t, mc, styles).Fragment, ids)
	checkRects(t, got, map[string][]fragRect{
		"c3": {{100, 0, 100, 100}, {200, 0, 100, 100}, {300, 0, 100, 50}},
		"s":  {{100, 0, 100, 30}},
	})
}
