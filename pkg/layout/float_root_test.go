package layout

import "testing"

// TestFloatingRoot pins a floated document element: the LayoutView's block
// layout handles it as a float (LayoutBox::ShouldBeHandledAsFloating,
// layout_box.cc:938-943 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so its
// auto inline-size shrinks to fit (CreateConstraintSpaceForFloat leaves the
// builder's fit-content default, floats_utils.cc:64-104) and a line-right
// float's margin box ends at the opportunity's line-right edge
// (PositionFloat, floats_utils.cc:382-388, :428-432). The view is the whole
// ICB (LayoutView::LayoutRoot, layout_view.cc:848-909). Viewport 800x600.
func TestFloatingRoot(t *testing.T) {
	cases := []struct {
		name        string
		rootStyle   string
		wantX, want float64 // root border box x and width
	}{
		{"float right shrinks to its content", "float:right", 800 - 50, 50},
		{"float left shrinks to its content", "float:left", 0, 50},
		{"float right with margins", "float:right; margin:0 10px 0 4px", 800 - 10 - 50, 50},
		{"float inline-end, ltr", "float:inline-end", 800 - 50, 50},
		{"float inline-start, rtl", "float:inline-start; direction:rtl", 800 - 50, 50},
		{"not floated stretches", "", 0, 800},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fl := layoutFallbackHTML(t, `<!DOCTYPE html><html id="r" style="`+c.rootStyle+`"><body style="margin:0"><div style="width:50px; height:20px"></div></body></html>`, nil)
			b := findBox(fl.root, byID("r"))
			if b == nil || b.X != c.wantX || b.Width != c.want {
				t.Errorf("root = %v, want x=%v width=%v", boxDims(b), c.wantX, c.want)
			}
		})
	}
}
