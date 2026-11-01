package layout

import (
	"fmt"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-487: block-level early breaks. Mirrors Blink @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b: MovePastBreakpoint records the
// breakpoint before each block child it moves past, or the best one inside
// it (UpdateEarlyBreakAtBlockChild, fragmentation_utils.cc:1126-1135,
// :1249-1314); when a later child does not fit and breaking before it is
// less appealing (a break-before/after:avoid), AttemptSoftBreak returns
// kNeedsEarlierBreak (:1316-1353) and the block lays out again, breaking
// at the early break (RelayoutAndBreakEarlier, block_layout_algorithm.cc:
// 617-626, :1074-1090), entering it in the child that holds it
// (EnterEarlyBreakInChild, fragmentation_utils.cc:1355-1365).
//
// Expected geometry is the WPT css-break break-between-avoid-* reference
// (a 100x100 green square; Chrome 156 passes every one, wpt.fyi
// 2026-10-31), measured relative to the page with body margin 0.

// earlyBreakRects lays out a 2-column, 100px-wide, column-fill:auto
// multicol of height mcHeight holding body, and returns the page rect of
// every element with an id.
func earlyBreakRects(t *testing.T, mcHeight, body string) map[string][]fragRect {
	t.Helper()
	return oofPageRects(t, `<!DOCTYPE html><body style="margin:0">
<style>.sq { height:50px; break-inside:avoid; }</style>
<div style="columns:2; column-fill:auto; column-gap:0; width:100px; height:`+mcHeight+`;">`+body+`</div>`)
}

func wantRects(t *testing.T, got map[string][]fragRect, want map[string][]fragRect) {
	t.Helper()
	for id, w := range want {
		if fmt.Sprint(got[id]) != fmt.Sprint(w) {
			t.Errorf("%s = %v, want %v", id, got[id], w)
		}
	}
}

// break-between-avoid-000: three 50px squares fit a 160px column, but
// break-before:avoid between the third and fourth moves the break to
// between the second and third.
func TestEarlyBreakBeforeAvoid_LOU487(t *testing.T) {
	got := earlyBreakRects(t, "160px", `
  <div id=a class=sq></div>
  <div id=b class=sq></div>
  <div id=c class=sq></div>
  <div id=d class=sq style="break-before:avoid;"></div>`)
	wantRects(t, got, map[string][]fragRect{
		"a": {{0, 0, 50, 50}}, "b": {{0, 50, 50, 50}},
		"c": {{50, 0, 50, 50}}, "d": {{50, 50, 50, 50}},
	})
}

// break-between-avoid-001: the same with break-after:avoid on the third.
func TestEarlyBreakAfterAvoid_LOU487(t *testing.T) {
	got := earlyBreakRects(t, "160px", `
  <div id=a class=sq></div>
  <div id=b class=sq></div>
  <div id=c class=sq style="break-after:avoid;"></div>
  <div id=d class=sq></div>`)
	wantRects(t, got, map[string][]fragRect{
		"b": {{0, 50, 50, 50}}, "c": {{50, 0, 50, 50}}, "d": {{50, 50, 50, 50}},
	})
}

// break-between-avoid-003: the best breakpoint is inside content already
// moved past, two levels down: the early break chains through the
// containers, and the relayout enters it to break before the third square.
func TestEarlyBreakInsideChild_LOU487(t *testing.T) {
	got := earlyBreakRects(t, "160px", `
  <div id=w>
    <div id=a class=sq></div>
    <div><div>
      <div id=b class=sq></div>
      <div id=c class=sq></div>
    </div></div>
  </div>
  <div id=d class=sq style="break-before:avoid;"></div>`)
	wantRects(t, got, map[string][]fragRect{
		"b": {{0, 50, 50, 50}}, "c": {{50, 0, 50, 50}}, "d": {{50, 50, 50, 50}},
	})
}

// break-between-avoid-009: the break-inside:avoid block C does not fit,
// and breaking before it violates the break-before:avoid of its first
// descendant, propagated up as C's initial break-before
// (PropagateChildBreakValues); the early break before B wins.
func TestEarlyBreakBeforeAvoidInsideChild_LOU487(t *testing.T) {
	got := earlyBreakRects(t, "250px", `
  <div id=a style="height:100px;"></div>
  <div id=b style="height:100px;"></div>
  <div id=c style="break-inside:avoid;">
    <div><div id=e style="break-before:avoid; height:100px;"></div></div>
  </div>`)
	wantRects(t, got, map[string][]fragRect{
		"a": {{0, 0, 50, 100}}, "b": {{50, 0, 50, 100}}, "c": {{50, 100, 50, 100}},
	})
}

// break-between-avoid-012: whitespace around a float between blocks of a
// multicol is not an in-flow child, so the float is no breakpoint: the
// early break goes before B and B, the float and D all move to column 2.
func TestEarlyBreakSkipsFloat_LOU487(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">
<div style="columns:2; gap:0; column-fill:auto; width:100px; height:150px;">
  <div id=a style="height:100px;"></div>
  <div id=b style="height:30px;"></div>
  <div id=fl style="float:left; width:50%;">
    <div style="height:10px; contain:size;"></div>
    <div style="height:10px; contain:size;"></div>
  </div>
  <div id=d style="break-before:avoid; break-inside:avoid;">
    <div style="margin-left:auto; width:50%; height:20px;"></div>
    <div style="height:50px;"></div>
  </div>
</div>`)
	wantRects(t, got, map[string][]fragRect{
		"a": {{0, 0, 50, 100}}, "b": {{50, 0, 50, 30}},
		"fl": {{50, 30, 25, 20}}, "d": {{50, 30, 50, 70}},
	})
}

// UpdateEarlyBreakBetweenLines (block_layout_algorithm.cc:3291-3325 @
// a9f50e522efa): four 30px lines fit a 160px column, a break-before:avoid
// square after them does not; the early break between the lines
// (orphans 2, widows 2: before line max(4-2, min(3, 2)) = 2) wins over
// breaking before the square. The lines block is the first child, so
// breaking before it is a last resort.
func TestEarlyBreakBetweenLines_LOU487(t *testing.T) {
	got := earlyBreakRects(t, "160px", `
  <div id=l style="line-height:30px;">x<br>x<br>x<br>x</div>
  <div id=d class=sq style="break-before:avoid;"></div>`)
	// got["l"] also lists the text boxes; check the block's two fragments.
	if l := got["l"]; len(l) < 4 || l[0] != (fragRect{0, 0, 50, 160}) || l[3] != (fragRect{50, 0, 50, 60}) {
		t.Errorf("l = %v, want fragments {0 0 50 160} (two lines, stretched) and {50 0 50 60}", l)
	}
	wantRects(t, got, map[string][]fragRect{"d": {{50, 60, 50, 50}}})
}

// widows-orphans-020: a line pushed below a float is separated from its
// container (block_layout_algorithm.cc:2616-2626 @ a9f50e522efa), so
// breaking before it, with no line before it in the fragment, is perfect:
// the float stays in column 1 and only the line moves to column 2.
func TestLinePushedByFloatsHasContainerSeparation_LOU487(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">
<div style="columns:2; column-fill:auto; width:190px; height:100px; gap:10px; orphans:2; widows:1;">
  <div id=a style="height:20px;"></div>
  <div id=fl style="float:left; width:100%; height:80px;"></div>
  &nbsp;<br>
</div>`)
	wantRects(t, got, map[string][]fragRect{
		"a": {{0, 0, 90, 20}}, "fl": {{0, 20, 90, 80}},
	})
}

// C6: a soft break before a child propagates the pushed child's break
// values (BreakBeforeChild, fragmentation_utils.cc:970-972 @
// a9f50e522efa): the container's final break-after is the pushed child's.
func TestSoftBreakPropagatesChildBreakValues_LOU487(t *testing.T) {
	a := makeNode("div")
	x := makeNode("div")
	n := makeNode("div", a, x)
	styles := map[*html.Node]*css.Style{
		n: makeStyle("display", "block"),
		a: makeStyle("display", "block", "height", "60px"),
		x: makeStyle("display", "block", "height", "60px", "break-inside", "avoid", "break-after", "avoid"),
	}
	res := layoutBlockInFragmentainer(t, n, styles, 100, 0)
	if res.BreakToken == nil {
		t.Fatal("no break; want a soft break before the second child")
	}
	if res.FinalBreakAfter != "avoid" {
		t.Errorf("FinalBreakAfter = %q, want %q (the pushed child's break-after)", res.FinalBreakAfter, "avoid")
	}
}
