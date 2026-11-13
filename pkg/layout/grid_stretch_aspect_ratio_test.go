package layout

import "testing"

// `normal` stretches a grid item implicitly (kStretchImplicit), which an
// aspect-ratio with a definite size in the other axis overrides; `stretch`
// stretches explicitly (kStretchExplicit) and wins. Blink passes the item's
// column_auto_behavior / row_auto_behavior from AxisEdgeFromItemPosition and
// fixes neither size (grid/grid_item.cc:120-141; GridLayoutAlgorithm::
// CreateConstraintSpace, grid/grid_layout_algorithm.cc:1358-1409;
// ComputeInlineSizeForFragmentInternal, length_utils.cc:535-569 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each case is a 100x100 grid
// area with an aspect-ratio:1 item; want is its size, from Chrome.
func TestGridItemImplicitStretchKeepsAspectRatio(t *testing.T) {
	cases := []struct {
		name, item string
		w, h       float64
	}{
		{"normal inline, definite height", "height:20px;justify-self:normal", 20, 20},
		{"normal block, definite width", "width:20px;align-self:normal", 20, 20},
		// Guards: explicit stretch wins over the ratio.
		{"stretch inline, definite height", "height:20px;justify-self:stretch", 100, 20},
		{"stretch block, definite width", "width:20px;align-self:stretch", 20, 100},
		{"stretch both", "justify-self:stretch;align-self:stretch", 100, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:100px;width:100px">`+
				`<div id="i" style="aspect-ratio:1;`+c.item+`"></div></div>`)
			i := findBoxByID(r, "i")
			if i == nil {
				t.Fatal("no #i box")
			}
			if i.Width != c.w || i.Height != c.h {
				t.Errorf("#i %vx%v, want %vx%v", i.Width, i.Height, c.w, c.h)
			}
		})
	}
}
