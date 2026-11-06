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
