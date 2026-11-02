package render

// background-position resolution (LOU-598, css-images worklist B3).
//
// FillLayer::PositionX/Y are Lengths and BackgroundXOrigin/YOrigin the edge
// they are measured from (core/style/fill_layer.h:72-80, built by
// CSSToStyleMap::MapFillPositionX/Y, core/css/resolver/
// css_to_style_map.cc:200-238 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// ResolveXPosition/ResolveYPosition (core/paint/
// background_image_geometry.cc:63-87) resolve them with
// MinimumValueForLength against (positioning area - tile size).

import (
	"image"
	"image/color"
	"testing"
)

// renderPositionedBackground paints a 20×20 div whose 10×10 no-repeat
// background image (colour c) is placed by pos.
func renderPositionedBackground(t *testing.T, c color.RGBA, pos string) *image.RGBA {
	t.Helper()
	uri := splitPNGDataURI(t, 10, 10, c, c)
	src := `<!DOCTYPE html><body style="margin:0">
<div style="width:20px; height:20px; background:url(` + uri + `) no-repeat; background-size:10px 10px; background-position:` + pos + `"></div>`
	return renderColumnRuleHTML(t, src, 50, 50)
}

// A negative percentage is a percentage of (area - tile): -50% of 10px is
// -5px, so only image rows 5..9 show, at y 0..4.
func TestBackgroundPosition_NegativePercentage(t *testing.T) {
	c := color.RGBA{211, 21, 22, 255}
	img := renderPositionedBackground(t, c, "0 -50%")
	wantRGB(t, img, 5, 0, c.R, c.G, c.B, "image top row at -5px offset")
	wantRGB(t, img, 5, 4, c.R, c.G, c.B, "image bottom row")
	wantRGB(t, img, 5, 5, 255, 255, 255, "below the shifted image")
}

// A negative percentage on the x axis: -20% of 10px is -2px.
func TestBackgroundPosition_NegativePercentageX(t *testing.T) {
	c := color.RGBA{22, 212, 23, 255}
	img := renderPositionedBackground(t, c, "-20% 0")
	wantRGB(t, img, 7, 5, c.R, c.G, c.B, "image right column at x 7")
	wantRGB(t, img, 8, 5, 255, 255, 255, "right of the shifted image")
}

// An offset from the right edge is measured from the right (BackgroundEdgeOrigin
// kRight): "right 3px top" puts the tile at x 7..16.
func TestBackgroundPosition_RightEdgeOffset(t *testing.T) {
	c := color.RGBA{23, 24, 213, 255}
	img := renderPositionedBackground(t, c, "right 3px top")
	wantRGB(t, img, 7, 2, c.R, c.G, c.B, "image left column")
	wantRGB(t, img, 16, 2, c.R, c.G, c.B, "image right column")
	wantRGB(t, img, 6, 2, 255, 255, 255, "left of the image")
	wantRGB(t, img, 17, 2, 255, 255, 255, "right of the image")
}
