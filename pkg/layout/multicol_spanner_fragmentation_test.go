package layout

import "testing"

// Worklist C10 (LOU-488): ColumnLayoutAlgorithm::LayoutSpanner's
// fragmentation pieces (column_layout_algorithm.cc:1334-1447 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Expected geometry is Chrome
// 155 getClientRects.

// TestLayoutSpanner_RelayoutAfterRowSnap: a spanner that does not fit in
// the rest of a wrapping row moves to the next row and is laid out again
// there (result = layout(), cla.cc:1393), so its fragmentation follows the
// snapped offset. Inner rows are 100px; the balanced 40px block ends the
// line at 20, the 150px spanner snaps to 100 and breaks at the end of the
// 200px outer column, continuing 50px into the next one.
func TestLayoutSpanner_RelayoutAfterRowSnap(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div style="columns:2;column-fill:auto;height:200px;width:400px;gap:0">`+
		`<div id=inner style="columns:2;column-height:100px;column-wrap:wrap;row-gap:0;gap:0">`+
		`<div id=a style="height:40px"></div>`+
		`<div id=s style="column-span:all;height:150px"></div>`+
		`<div id=c style="height:20px"></div>`+
		`</div></div>`)
	checkRects(t, got, map[string][]fragRect{
		"inner": {{0, 0, 200, 200}, {200, 0, 200, 100}},
		"a":     {{0, 0, 100, 20}, {100, 0, 100, 20}},
		"s":     {{0, 100, 200, 100}, {200, 0, 200, 50}},
		"c":     {{200, 50, 100, 10}, {300, 50, 100, 10}},
	})
}

// TestLayoutSpanner_BreakBeforeChildIfNeededInOuterBalancingPass: the
// spanner's breakpoint is examined whenever the multicol takes part in
// block fragmentation (HasBlockFragmentation, cla.cc:1397), also in the
// outer multicol's initial balancing pass, where MovePastBreakpoint
// propagates the break-inside:avoid spanner's unbreakable block-size, its
// offset included (fragmentation_utils.cc:1110-1118, CalculateUnbreakable
// BlockSize). The outer columns are 10 + 80 = 90px tall.
func TestLayoutSpanner_BreakBeforeChildIfNeededInOuterBalancingPass(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div id=outer style="columns:2;width:400px;gap:0">`+
		`<div id=inner style="columns:2;gap:0">`+
		`<div id=a style="height:20px"></div>`+
		`<div id=s style="column-span:all;break-inside:avoid"><div style="height:40px"></div><div style="height:40px"></div></div>`+
		`<div id=c style="height:20px"></div>`+
		`</div></div>`)
	checkRects(t, got, map[string][]fragRect{
		"outer": {{0, 0, 400, 90}},
		"inner": {{0, 0, 200, 90}, {200, 0, 200, 10}},
		"a":     {{0, 0, 100, 10}, {100, 0, 100, 10}},
		"s":     {{0, 10, 200, 80}},
		"c":     {{200, 0, 100, 10}, {300, 0, 100, 10}},
	})
}
