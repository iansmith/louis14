package render

// LOU-531: one BackgroundImageGeometry path for images and gradients.
// Each test pins a behavior of Blink's BackgroundImageGeometry /
// DrawTiledBackground (core/paint/background_image_geometry.cc,
// box_painter_base.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// splitPNGDataURI returns a data: URI for a w×h PNG whose left half is
// `left` and right half is `right`.
func splitPNGDataURI(t *testing.T, w, h int, left, right color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := left
			if x >= w/2 {
				c = right
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func wantRGB(t *testing.T, img *image.RGBA, x, y int, r, g, b uint8, why string) {
	t.Helper()
	if gr, gg, gb := rgbAt(img, x, y); gr != r || gg != g || gb != b {
		t.Errorf("pixel (%d,%d) = rgb(%d,%d,%d), want rgb(%d,%d,%d): %s", x, y, gr, gg, gb, r, g, b, why)
	}
}

// background-repeat: round on both axes with background-size: auto 61px
// (background-size-025). The Y round pass rescales the tile height to
// 210/3 = 70; the auto width follows the ratio only when the X axis is not
// itself round — X rounds 61 → 210/3 = 70 independently. Tiles are 70×70,
// so the second tile's blue half starts at x=70. CalculateRepeatAndPosition
// background_image_geometry.cc:547-587.
func TestBackgroundGeometry_RoundBothAxes_AutoWidthNotRescaledByY(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	orange := color.RGBA{255, 165, 0, 255}
	uri := splitPNGDataURI(t, 100, 100, blue, orange)
	src := `<!DOCTYPE html><body style="margin:0">
<div style="width:210px; height:210px; background-color:red; background-image:url(` + uri + `);
 background-repeat:round; background-size:auto 61px"></div>`
	img := renderColumnRuleHTML(t, src, 300, 300)
	wantRGB(t, img, 75, 5, 0, 0, 255, "second 70px tile starts with its blue half at x=70")
	wantRGB(t, img, 60, 5, 255, 165, 0, "first tile's orange half spans x 35..70")
	wantRGB(t, img, 205, 205, 255, 165, 0, "third tile ends flush at 210 (orange half)")
}

// A no-repeat gradient with background-size 200px auto, centered in the
// content box (background-size-041 shape). Gradients have no natural size,
// so the auto height is the positioning-area height (400) and the tile is
// 200×400 at x = 40 + (400-200)/2 = 140. The area outside the tile shows
// the background color. CalculateFillTileSize :454-472, SetNoRepeatX :101.
func TestBackgroundGeometry_GradientNoRepeat_SizedAndPositioned(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0">
<div style="background-color:red; background-image:linear-gradient(lime, lime);
 background-origin:content-box; background-position:center; background-repeat:no-repeat;
 background-size:200px auto; border:solid 40px transparent; width:400px; height:400px"></div>`
	img := renderColumnRuleHTML(t, src, 500, 500)
	wantRGB(t, img, 200, 200, 0, 255, 0, "inside the 200x400 tile at x 140..340")
	wantRGB(t, img, 100, 200, 255, 0, 0, "left of the tile: background-color only")
	wantRGB(t, img, 360, 200, 255, 0, 0, "right of the tile: background-color only")
	wantRGB(t, img, 200, 20, 255, 0, 0, "above the tile (top border area)")
}

// A gradient clipped to a rounded content box (background-rounded-image-
// clip-001 shape): gradient pixels must honor the rounded clip, not only
// its bounding box. The content box starts at (10,10) with an inner
// top-left radius of 80, so (14,14) is inside the content rect but outside
// the curve. Gradients paint as images through DrawTiledBackground under
// the context clip, box_painter_base.cc:814.
func TestBackgroundGeometry_GradientHonorsRoundedClip(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0">
<div style="width:300px; height:200px; background-image:linear-gradient(lime, lime);
 background-clip:content-box; border-top-left-radius:90px; border:10px solid transparent"></div>`
	img := renderColumnRuleHTML(t, src, 400, 300)
	wantRGB(t, img, 14, 14, 255, 255, 255, "outside the rounded content-box clip")
	wantRGB(t, img, 150, 100, 0, 255, 0, "inside the content box")
}

// A background on a transformed box away from the origin: the composed
// tile buffer must be drawn once at its dest offset, not offset twice by
// the transformed DrawImage path (transform-background-001..004
// regression). rotate(180deg) about the center swaps the image halves.
func TestBackgroundGeometry_TransformedBoxImageNotDoubleOffset(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	orange := color.RGBA{255, 165, 0, 255}
	uri := splitPNGDataURI(t, 40, 40, blue, orange)
	src := `<!DOCTYPE html><body style="margin:0">
<div style="margin-left:100px; margin-top:20px; width:40px; height:40px; background:url(` + uri + `); transform:rotate(180deg)"></div>`
	img := renderColumnRuleHTML(t, src, 300, 100)
	wantRGB(t, img, 110, 40, 255, 165, 0, "rotated: orange half now on the left")
	wantRGB(t, img, 130, 40, 0, 0, 255, "rotated: blue half now on the right")
}
