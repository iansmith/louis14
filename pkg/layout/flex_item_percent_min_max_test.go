package layout

import "testing"

// LOU-844. A flex item's min/max main lengths resolve against the
// container's content box, child_percentage_size_ (BuildSpaceForFlexBasis,
// flex/flex_layout_algorithm.cc:685-697; BuildSpaceForLayout :720, :802 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose block size is the
// container's definite block size.
func TestFlexItemPercentMaxMainSize(t *testing.T) {
	tests := []struct {
		name   string
		html   string
		width  float64 // the item's border-box width; 0 = not checked
		height float64 // the item's border-box height
	}{
		{
			name:   "column item max-height 100% of a 100px container",
			html:   `<div style="display:flex;flex-direction:column;width:100px;height:100px"><div id="t" style="max-height:100%"><div style="height:200px"></div></div></div>`,
			height: 100,
		},
		{
			// Short content: the max length must not clamp it (nor
			// collapse it).
			name:   "column item max-height 100%, short content",
			html:   `<div style="display:flex;flex-direction:column;width:100px;height:100px"><div id="t" style="max-height:100%"><div style="height:20px"></div></div></div>`,
			height: 20,
		},
		{
			name:   "column item min-height 50%, short content",
			html:   `<div style="display:flex;flex-direction:column;width:100px;height:100px"><div id="t" style="min-height:50%"><div style="height:20px"></div></div></div>`,
			height: 50,
		},
		{
			name:   "column item max-height 50%",
			html:   `<div style="display:flex;flex-direction:column;width:100px;height:100px"><div id="t" style="max-height:50%"><div style="height:200px"></div></div></div>`,
			height: 50,
		},
		{
			// intrinsic-percent-replaced-017 (canvas for the img)
			name:   "centered column item max-height 100% holding a max-height 100% canvas",
			html:   `<div style="display:flex;flex-direction:column;width:100px;height:100px;align-items:center"><div id="t" style="max-height:100%"><canvas width="200" height="200" style="max-height:100%"></canvas></div></div>`,
			width:  100,
			height: 100,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+tc.html), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Height != tc.height {
				t.Errorf("item height = %v, want %v", b.Height, tc.height)
			}
			if tc.width != 0 && b.Width != tc.width {
				t.Errorf("item width = %v, want %v", b.Width, tc.width)
			}
		})
	}
}

// A column item's descendants resolve their percentages against the item's
// flexed (fixed) block size, not the container's: the item's own
// percentage size is childPercentageSize, but its children see its fixed
// block size (CalculateChildPercentageSize over the item's
// ChildAvailableSize, length_utils.cc:1810-1828 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFlexColumnItemChildPercentHeight(t *testing.T) {
	for _, container := range []string{"height:200px", "height:auto"} {
		t.Run(container, func(t *testing.T) {
			src := `<!DOCTYPE html><body style="margin:0"><div style="display:flex;flex-direction:column;width:100px;` + container + `"><div style="flex:0 0 80px"><div id="t" style="height:50%"></div></div></div>`
			b := findBoxByID(layoutRootTestHTML(t, src), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Height != 40 {
				t.Errorf("child height = %v, want 40 (50%% of the item's flexed 80px)", b.Height)
			}
		})
	}
}
