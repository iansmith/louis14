package render_test

import (
	"image/color"
	"testing"
)

// LOU-730 (filter-effects worklist D3): a backdrop filter lives on an effect
// node in the element's local transform space (PropertyTreeManager::
// PopulateCcEffectNode, property_tree_manager.cc:1346-1360 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a rotated element inverts the
// backdrop under its rotated box, not under its untransformed border box.
func TestBackdropFilter_RotatedElementSamplesUnderItsRotatedBox(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}
div{position:absolute;width:100px;height:100px;left:100px;top:100px}</style>
<div style="background:green"></div>
<div style="top:140px;backdrop-filter:invert(1);transform:rotate(30deg)"></div>`, 300, 300)
	// (120,195) is inside the rotated box and over the green box: green
	// inverted. (160,230) is inside the rotated box, over white: black.
	wantPixelWithinOne(t, img, 120, 195, color.RGBA{255, 127, 255, 255}, "green under the rotated box, inverted")
	wantPixelWithinOne(t, img, 160, 230, color.RGBA{0, 0, 0, 255}, "white under the rotated box, inverted")
}

// A scaled element's backdrop still paints when its layer-space size passes
// the 8000px buffer limit: the region is clamped to the device clip mapped
// into layer space, as a filter's is (filterLayerClip).
func TestBackdropFilter_ScaledElementPastBufferLimit(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0;background:green}</style>
<div style="position:absolute;left:0;top:0;width:1000px;height:1000px;transform-origin:0 0;transform:scale(10);backdrop-filter:invert(1)"></div>`, 100, 100)
	wantPixelWithinOne(t, img, 50, 50, color.RGBA{255, 127, 255, 255}, "green under the scaled box, inverted")
}

// rotatedBlurPage puts a rotated backdrop-filtered box over a larger lime
// box of the same centre and rotation, on a red page: the box's backdrop is
// lime well past its anti-aliased edge, and red only beyond.
func rotatedBlurPage(filter string) string {
	return `<!DOCTYPE html><style>body{margin:0;background:red}
div{position:absolute;transform:rotate(45deg)}</style>
<div style="left:30px;top:30px;width:140px;height:140px;background:lime"></div>
<div style="left:50px;top:50px;width:100px;height:100px;backdrop-filter:` + filter + `"></div>`
}

// A rotated blur reads its backdrop bilinearly (ScaleTranslate layer, rotated
// LayerToDev) and duplicates only pixels inside the rotated box: the backdrop
// inside stays lime, with no red pulled in from outside the box.
func TestBackdropFilter_RotatedBlurReadsOnlyInsideTheBox(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   color.RGBA
	}{
		{"blur(4px)", color.RGBA{0, 255, 0, 255}},
		{"blur(4px) invert(1)", color.RGBA{255, 0, 255, 255}},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			img := renderToImage(t, rotatedBlurPage(tc.filter), 200, 200)
			// The centre, and 4px inside the top corner (100, 29.3).
			for _, p := range [][2]int{{100, 100}, {100, 34}} {
				if got := img.RGBAAt(p[0], p[1]); !colorWithin(got, tc.want, 2) {
					t.Errorf("(%d,%d) = %v, want %v within 2: red from outside the box", p[0], p[1], got, tc.want)
				}
			}
		})
	}
}

// An element with both backdrop-filter and filter filters its backdrop with
// the backdrop ops followed by the filter ops (PaintLayer::
// UpdateBackdropFilters, paint_layer.cc:2601-2614 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), read from the real backdrop: a
// green backdrop inverted, then greyscaled, is grey.
func TestBackdropFilter_PlusFilterFiltersTheBackdrop(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}
div{position:absolute;left:0;top:0;width:100px;height:100px}</style>
<div style="background:green"></div>
<div style="backdrop-filter:invert(1);filter:grayscale(1)"></div>`, 120, 120)
	// (0,128,0) inverted is (255,127,255), greyscaled (163,163,163).
	wantPixelWithinOne(t, img, 50, 50, color.RGBA{163, 163, 163, 255}, "the inverted green backdrop greyscaled")
}

// A blurred backdrop mirrors at the box's edges (Blink's compositor crops
// the input to the bounds with a mirror edge mode): a 2px red strip along
// the box's left edge, over a lime backdrop, is not smeared across the
// blur's pad, so next to the edge lime still dominates.
func TestBackdropFilter_BlurMirrorsAtTheEdge(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0;background:lime}</style>
<div style="position:absolute;left:0;top:0;width:2px;height:100px;background:red"></div>
<div style="position:absolute;left:0;top:0;width:100px;height:100px;backdrop-filter:blur(10px)"></div>`, 120, 120)
	if c := img.RGBAAt(1, 50); c.G <= c.R || c.R == 0 {
		t.Errorf("pixel (1,50) = %v: want lime dominating with the red edge blurred in (mirrored, not duplicated)", c)
	}
}
