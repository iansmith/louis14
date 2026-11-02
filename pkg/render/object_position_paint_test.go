package render

// object-position on <img> (LOU-590, css-images worklist D1).
//
// object-position is a <position> (ObjectPosition::ParseSingleValue →
// ConsumePosition, longhands_custom.cc:7317 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) computed to a LengthPoint
// (StyleBuilderConverter::ConvertPosition, style_builder_converter.cc:2471)
// and applied by LayoutReplaced::ComputeObjectFitAndPositionRect
// (layout_replaced.cc:422): offset = MinimumValueForLength(position,
// content size - object size) per axis. Each test uses its own image colour
// so the URL-keyed image cache (LOU-528) never shares a decode.

import (
	"image"
	"image/color"
	"testing"
)

// opPixel asserts the pixel at (x, y) is c (wantRGB with an RGBA colour).
func opPixel(t *testing.T, img *image.RGBA, x, y int, c color.RGBA, why string) {
	t.Helper()
	wantRGB(t, img, x, y, c.R, c.G, c.B, why)
}

var opWhite = color.RGBA{255, 255, 255, 255}

// renderObjectPositionImg paints a 40×40 <img> at the origin with the given
// declarations and a w×h image of colour c.
func renderObjectPositionImg(t *testing.T, w, h int, c color.RGBA, decls string) *image.RGBA {
	t.Helper()
	uri := splitPNGDataURI(t, w, h, c, c)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; width:40px; height:40px; ` + decls + `}</style>
<img src="` + uri + `">`
	return renderColumnRuleHTML(t, src, 100, 100)
}

// "top right": a vertical keyword first is swapped onto the y axis
// (PositionFromTwoValues, css_parsing_utils.cc:2586). contain scales the 4×2
// image to 40×20, so the image sits in the top half.
func TestObjectPosition_KeywordOrderSwapped(t *testing.T) {
	c := color.RGBA{201, 11, 12, 255}
	img := renderObjectPositionImg(t, 4, 2, c, "object-fit:contain; object-position:top right")
	opPixel(t, img, 20, 5, c, "image")
	opPixel(t, img, 20, 35, opWhite, "outside the image")
}

// "bottom 1px right 2px": edge offsets resolve from the far edge
// (ConvertPositionLength → Length::SubtractFromOneHundredPercent), so the
// unscaled 4×2 image occupies x 34..37, y 37..38.
func TestObjectPosition_FarEdgeOffsets(t *testing.T) {
	c := color.RGBA{12, 202, 13, 255}
	img := renderObjectPositionImg(t, 4, 2, c, "object-fit:none; object-position:bottom 1px right 2px")
	opPixel(t, img, 34, 37, c, "image")
	opPixel(t, img, 37, 38, c, "image")
	opPixel(t, img, 38, 38, opWhite, "outside the image")
	opPixel(t, img, 37, 39, opWhite, "outside the image")
}

// "top 3px left 50%": x = 50% of (40 - 4) = 18, y = 3.
func TestObjectPosition_NearEdgeOffsetAndPercent(t *testing.T) {
	c := color.RGBA{13, 14, 203, 255}
	img := renderObjectPositionImg(t, 4, 2, c, "object-fit:none; object-position:top 3px left 50%")
	opPixel(t, img, 18, 3, c, "image")
	opPixel(t, img, 21, 4, c, "image")
	opPixel(t, img, 17, 3, opWhite, "outside the image")
	opPixel(t, img, 18, 2, opWhite, "outside the image")
}

// object-position does not permit three-value positions (ConsumePosition with
// no three_value_position feature, css_parsing_utils.cc:2649-2728): "left 10px
// top" is invalid, so the earlier "right bottom" declaration survives and the
// image sits in the bottom-right corner.
func TestObjectPosition_ThreeValueInvalid(t *testing.T) {
	c := color.RGBA{204, 205, 14, 255}
	img := renderObjectPositionImg(t, 4, 2, c, "object-fit:none; object-position:right bottom; object-position:left 10px top")
	opPixel(t, img, 37, 39, c, "image")
	opPixel(t, img, 1, 19, opWhite, "outside the image")
}
