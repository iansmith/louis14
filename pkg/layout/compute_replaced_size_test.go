package layout

import (
	"testing"

	"louis14/pkg/html"
)

// LOU-504 item 2: a replaced element is sized by ComputeReplacedSize inside
// CalculateInitialFragmentGeometry (length_utils.cc:1701-1707 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose min/max-content sizes
// are the replaced size (ComputeReplacedSizeInternal's MinMaxSizesFunc /
// BlockSizeFunc, :1027-1043, :1103-1126) and whose aspect-ratio applies to
// the content box unless `aspect-ratio` names a ratio with box-sizing
// (BoxSizingForAspectRatio, computed_style.h:2623-2628).
//
// Every expectation is the geometry of a css-sizing reftest Chrome passes
// on wpt.fyi (Chrome 159 master run 5077563219902464).
func TestComputeReplacedSizeInInitialFragmentGeometry_LOU504(t *testing.T) {
	tests := []struct {
		name string
		// html is the body content; the element under test carries id="t".
		html          string
		width, height float64 // border-box
	}{
		{
			// replaced-max-height-min-content: max-height:min-content is the
			// block-size the 100px width transfers through the 1:1 ratio.
			name:  "max-height min-content transfers inline through ratio",
			html:  `<canvas id="t" width="10" height="10" style="width:100px;height:1000px;max-height:min-content"></canvas>`,
			width: 100, height: 100,
		},
		{
			// replaced-min-height-min-content.
			name:  "min-height min-content transfers inline through ratio",
			html:  `<canvas id="t" width="10" height="10" style="width:100px;height:10px;min-height:min-content"></canvas>`,
			width: 100, height: 100,
		},
		{
			// svg-no-ar-max-height-min-content: no ratio, so min-content is
			// the natural block-size, 100px.
			name:  "max-height min-content without ratio is the natural block-size",
			html:  `<svg id="t" width="0" height="100" style="width:100px;height:1000px;max-height:min-content"></svg>`,
			width: 100, height: 100,
		},
		{
			// intrinsic-ratio-replaced-box-sizing: a ratio-only svg stretches
			// to its 90px margin-box-less available inline-size; the natural
			// ratio applies to the content box.
			name:  "ratio-only svg stretches; natural ratio is content-box",
			html:  `<div style="display:inline-block;width:100px;height:100px"><svg id="t" viewBox="0 0 100 100" style="margin:5px;border:10px solid grey"></svg></div>`,
			width: 90, height: 90,
		},
		{
			name:  "ratio-only svg, border-box sizing still content-box ratio",
			html:  `<div style="display:inline-block;width:100px;height:100px"><svg id="t" viewBox="0 0 100 100" style="margin:5px;border:10px solid grey;box-sizing:border-box"></svg></div>`,
			width: 90, height: 90,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse(`<!DOCTYPE html><body style="margin:0">` + tc.html)
			if err != nil {
				t.Fatalf("html.Parse: %v", err)
			}
			var target *Box
			var walk func(b *Box)
			walk = func(b *Box) {
				if b.Node != nil {
					if id, _ := b.Node.GetAttribute("id"); id == "t" && target == nil {
						target = b
					}
				}
				for _, c := range b.Children {
					walk(c)
				}
			}
			for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
				walk(b)
			}
			if target == nil {
				t.Fatal("no #t box")
			}
			// Box.Width / Height are the fragment's border-box size.
			if w, h := target.Width, target.Height; w != tc.width || h != tc.height {
				t.Errorf("border-box = %vx%v, want %vx%v", target.Width, target.Height, tc.width, tc.height)
			}
		})
	}
}
