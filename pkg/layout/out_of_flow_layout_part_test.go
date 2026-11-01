package layout

import "testing"

// TestFixedContainerFlexGridTable pins flex, grid and table containers as
// containing blocks of their fixed-position descendants when they are fixed
// containers (here: a transform). Blink derives IsFixedContainer from style
// for every box kind (LayoutObject::ComputeIsFixedContainer,
// layout_object.cc:1849-1900 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// and the OutOfFlowLayoutPart every container runs reads it
// (out_of_flow_layout_part.cc:542-545; IsContainingBlockForCandidate,
// :2746-2791 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). `f`
// (top:10px; left:5px) is placed against the container's padding box at
// (30,20), so at (35,30); against the viewport it would be at (5,10).
func TestFixedContainerFlexGridTable(t *testing.T) {
	for _, display := range []string{"flex", "grid", "table"} {
		t.Run(display, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="height:20px"></div>`+
				`<div style="display:`+display+`;width:100px;height:100px;margin-left:30px;transform:translateX(0)">`+
				`<div id=f style="position:fixed;top:10px;left:5px;width:10px;height:10px"></div></div>`)
			checkRects(t, got, map[string][]fragRect{
				"f": {{35, 30, 10, 10}},
			})
		})
	}
}
