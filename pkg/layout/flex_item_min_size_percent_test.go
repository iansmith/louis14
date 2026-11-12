package layout

import "testing"

// LOU-832. A flex item's content size suggestion is measured in the item's
// layout space, whose percentage size is the container's content box
// (child_percentage_size_, BuildSpaceForLayout, flex/flex_layout_algorithm.cc:
// 720, :802; MinMaxSizesFunc :869-881 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An item's own height:100%
// resolves against the container's definite height, so a height:100%
// replaced child transfers that height through its ratio.
//
// Each container is 10px wide, so the item shrinks to its automatic minimum
// size and the test pins that size, not the flex basis.
func TestFlexItemMinSize_PercentHeightReplacedChild(t *testing.T) {
	tests := []struct {
		name  string
		html  string
		width float64 // the item's border-box width
	}{
		{
			// intrinsic-percent-replaced-024 (canvas for the img)
			name:  "item height 100%",
			html:  `<div style="display:flex;width:10px;height:100px"><div id="t" style="height:100%"><canvas width="200" height="200" style="height:100%"></canvas></div></div>`,
			width: 100,
		},
		{
			name:  "item height 50%",
			html:  `<div style="display:flex;width:10px;height:100px"><div id="t" style="height:50%"><canvas width="200" height="200" style="height:100%"></canvas></div></div>`,
			width: 50,
		},
		{
			// intrinsic-percent-replaced-027
			name:  "item aspect-ratio and height 100% in a column flex",
			html:  `<div style="display:flex;flex-direction:column"><div style="display:flex;width:10px;height:100px"><div id="t" style="aspect-ratio:1/1;height:100%"><canvas width="200" height="200" style="height:100%"></canvas></div></div></div>`,
			width: 100,
		},
		{
			// Control: a stretched item already measures against the
			// container's height.
			name:  "stretched item",
			html:  `<div style="display:flex;width:10px;height:100px"><div id="t"><canvas width="200" height="200" style="height:100%"></canvas></div></div>`,
			width: 100,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+tc.html), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Width != tc.width {
				t.Errorf("item width = %v, want %v", b.Width, tc.width)
			}
		})
	}
}
