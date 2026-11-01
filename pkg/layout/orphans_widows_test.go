package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-480: orphans / widows in block fragmentation. Mirrors Blink's
// BlockLayoutAlgorithm line-box handling @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b:
//   - BreakBeforeChildIfNeeded's line-box branch
//     (block_layout_algorithm.cc:3187-3279): too few lines before the break
//     (orphans; widows too on a resumed fragment) lowers the break appeal to
//     kBreakAppealViolatingOrphansAndWidows; too few lines after it (widows)
//     keeps laying out lines;
//   - FinalizeForFragmentation (:3106-3127): having overflowed at line N
//     but broken (or finished) later, re-lay out and break before the line
//     that honors widows (RelayoutAndBreakEarlier with a line EarlyBreak).

// linesBlock is a block of n 30px line boxes ("x<br>x<br>...").
func linesBlock(n int, props ...string) (*html.Node, map[*html.Node]*css.Style) {
	var children []*html.Node
	for i := 0; i < n; i++ {
		if i > 0 {
			children = append(children, makeNode("br"))
		}
		children = append(children, makeTextNode("x"))
	}
	div := makeNode("div", children...)
	styles := map[*html.Node]*css.Style{
		div: makeStyle(append([]string{"display", "block", "line-height", "30px"}, props...)...),
	}
	for _, c := range children {
		if c.Type == html.ElementNode {
			styles[c] = makeStyle("display", "inline")
		}
	}
	return div, styles
}

func lineBoxCount(f *PhysicalFragment) int {
	n := 0
	for _, ch := range f.Children {
		if ch.Fragment != nil && ch.Fragment.Type == FragmentLineBox {
			n++
		}
	}
	return n
}

// Only one of three lines fits before the break; orphans:2 (the initial
// value) is violated, so the break is kept but its appeal is lowered.
func TestOrphansViolationLowersBreakAppeal_LOU480(t *testing.T) {
	n, styles := linesBlock(3, "widows", "1")
	res := layoutBlockInFragmentainer(t, n, styles, 40, 0)
	if res.BreakToken == nil {
		t.Fatal("no break; want a break after the first line")
	}
	if got := lineBoxCount(res.Fragment); got != 1 {
		t.Errorf("line boxes = %d, want 1", got)
	}
	if res.BreakAppeal != BreakAppealViolatingOrphansAndWidows {
		t.Errorf("BreakAppeal = %v, want ViolatingOrphansAndWidows (%v)", res.BreakAppeal, BreakAppealViolatingOrphansAndWidows)
	}
}

// orphans:1 is satisfied by a single line: the break is perfect.
func TestOrphansSatisfiedKeepsPerfectAppeal_LOU480(t *testing.T) {
	n, styles := linesBlock(3, "orphans", "1", "widows", "1")
	res := layoutBlockInFragmentainer(t, n, styles, 40, 0)
	if got := lineBoxCount(res.Fragment); got != 1 {
		t.Errorf("line boxes = %d, want 1", got)
	}
	if res.BreakAppeal != BreakAppealPerfect {
		t.Errorf("BreakAppeal = %v, want Perfect", res.BreakAppeal)
	}
}

// Two of three lines fit, but widows:2 needs two lines after the break, so
// the break moves up to before the second line.
func TestWidowsPushesLinesToNextFragment_LOU480(t *testing.T) {
	n, styles := linesBlock(3, "orphans", "1", "widows", "2")
	res := layoutBlockInFragmentainer(t, n, styles, 70, 0)
	if res.BreakToken == nil {
		t.Fatal("no break; want a break before the second line")
	}
	if got := lineBoxCount(res.Fragment); got != 1 {
		t.Errorf("line boxes = %d, want 1 (two lines left for widows:2)", got)
	}
	if res.BreakAppeal != BreakAppealPerfect {
		t.Errorf("BreakAppeal = %v, want Perfect", res.BreakAppeal)
	}
}

// Three of four lines fit; orphans:2 widows:2 break before line
// max(4-2, min(4, 2)) = 2 (block_layout_algorithm.cc:3119-3121).
func TestOrphansAndWidowsBreakLine_LOU480(t *testing.T) {
	n, styles := linesBlock(4)
	res := layoutBlockInFragmentainer(t, n, styles, 100, 0)
	if got := lineBoxCount(res.Fragment); got != 2 {
		t.Errorf("line boxes = %d, want 2", got)
	}
}

// Lines that fit before a break are not affected by widows when plenty of
// lines follow: break exactly where space runs out.
func TestWidowsSatisfiedBreaksAtFirstOverflow_LOU480(t *testing.T) {
	n, styles := linesBlock(6)
	res := layoutBlockInFragmentainer(t, n, styles, 100, 0)
	if got := lineBoxCount(res.Fragment); got != 3 {
		t.Errorf("line boxes = %d, want 3", got)
	}
	if res.BreakAppeal != BreakAppealPerfect {
		t.Errorf("BreakAppeal = %v, want Perfect", res.BreakAppeal)
	}
}

// multicol-nested-018: the inner multicol starts 100px down a 190px outer
// column; only one of its two 50px lines fits, violating orphans:2, so the
// outer flow breaks before the inner multicol instead of inside it. Both
// lines then sit in the inner multicol's first column, in the second outer
// column.
func TestNestedMulticolPushedByOrphans_LOU480(t *testing.T) {
	a := makeNode("div")
	br := makeNode("br")
	inner := makeNode("div", makeTextNode("x"), br, makeTextNode("x"))
	outer := makeNode("div", a, inner)
	ow := []string{"orphans", "2", "widows", "2", "line-height", "50px"}
	styles := map[*html.Node]*css.Style{
		outer: makeStyle(append([]string{"display", "block", "width", "100px", "height", "190px",
			"column-count", "2", "column-gap", "0", "column-fill", "auto"}, ow...)...),
		a: makeStyle("display", "block", "height", "100px"),
		inner: makeStyle(append([]string{"display", "block",
			"column-count", "2", "column-gap", "0", "column-fill", "auto"}, ow...)...),
		br: makeStyle("display", "inline"),
	}
	frag := layoutMulticolForTest(t, outer, styles).Fragment
	rects := borderBoxRectsByID(frag, map[*html.Node]string{inner: "inner"})["inner"]
	if len(rects) == 0 || rects[0].x != 50 || rects[0].y != 0 || rects[0].w != 50 {
		t.Errorf("inner multicol fragments = %v, want the first at (50, 0), 50px wide (pushed to the second outer column)", rects)
	}
	for _, r := range rects {
		if r.x < 50 {
			t.Errorf("inner multicol content %v in the first outer column; want none", r)
		}
	}
}
