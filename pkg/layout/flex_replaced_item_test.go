package layout

import "testing"

// A replaced flex item is sized by ComputeReplacedSize in the item's space,
// for its flex basis (BlockSizeFunc, flex/flex_layout_algorithm.cc:890-899)
// and for its cross size (PlaceFlexItems, :1490-1510), both @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. An image with only an aspect
// ratio (a viewBox-only SVG, 2:1) stretches to the available inline size
// (StretchFit, length_utils.cc:1173-1196): 100 wide in a 100px column
// flexbox, so 50 tall (css-flexbox/align-items-007).
// ratioOnlySVG is an image with only a 2:1 aspect ratio (a viewBox-only SVG).
const ratioOnlySVG = `data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><rect width="200" height="100"/></svg>`

func TestFlexReplacedItemSize(t *testing.T) {
	const ratioOnly = ratioOnlySVG
	const sized = `data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="20" height="10"/></svg>`
	flex := func(style, src string) string {
		return `<div style="display:flex;width:100px;height:100px;` + style + `"><img id="i" src='` + src + `'></div>`
	}
	cases := []struct {
		name, html string
		x, y, w, h float64
	}{
		{"ratio-only, column, centered", flex("flex-direction:column;align-items:center", ratioOnly), 0, 0, 100, 50},
		{"ratio-only, column, stretched", flex("flex-direction:column", ratioOnly), 0, 0, 100, 50},
		// Guards: a row flexbox, and an image with a natural size.
		{"ratio-only, row, centered", flex("align-items:center", ratioOnly), 0, 25, 100, 50},
		{"natural size, column, centered", flex("flex-direction:column;align-items:center", sized), 40, 0, 20, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			img := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+c.html), "i")
			assertBoxGeometry(t, "#i", img, c.x, c.y, c.w, c.h)
		})
	}
}
