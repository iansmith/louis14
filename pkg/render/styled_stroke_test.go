package render_test

import (
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// A dashed border side fits its dashes to the side's length
// (DrawLineWithStyle, core/paint/box_border_painter.cc:501-560;
// DashEffectFromStrokeStyle / SelectBestDashGap,
// platform/graphics/styled_stroke_data.cc:40-58, :81-134 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 3px dashed top side 100px
// long gets 6px dashes (DashLengthRatio 2) and, of 3px nominal gaps, the
// gap count that best fits: 11 dashes with 3.4px gaps, the last ending at
// the corner. So x 6-9.4 is a gap, 9.4-15.4 the second dash, 94-100 the
// last one.
func TestDrawLineWithStyle_DashedSideFitsGaps(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="position:absolute;left:10px;top:10px;width:100px;height:20px;border-top:3px dashed black"></div></body></html>`, 140, 40)
	assertPixel(t, img, 12, 11, black, "first dash, x 0-6")
	assertPixel(t, img, 17, 11, white, "first gap, x 6-9.4")
	assertPixel(t, img, 22, 11, black, "second dash, x 9.4-15.4")
	assertPixel(t, img, 107, 11, black, "last dash, x 94-100, ends at the corner")
}

// A dotted side wider than 3px draws round dots (zero-length dashes with
// round caps) spaced by the best-fitting gap, the line pulled in by half a
// dot at each end (DrawLineWithStyle, box_border_painter.cc:520-540;
// DashEffectFromStrokeStyle, styled_stroke_data.cc:115-132 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 6px dotted side 100px long
// gets nine 6px dots with centres 11.74px apart from x = 3, so the first
// gap runs x 6-11.74 and the last dot centres at 96.9.
func TestDrawLineWithStyle_DottedSideFitsDots(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="position:absolute;left:10px;top:10px;width:100px;height:20px;border-top:6px dotted black"></div></body></html>`, 140, 40)
	assertPixel(t, img, 13, 13, black, "first dot, centre x 3")
	assertPixel(t, img, 18, 13, white, "first gap, x 6-11.74")
	assertPixel(t, img, 24, 13, black, "second dot, centre x 14.74")
	assertPixel(t, img, 106, 13, black, "last dot, centre x 96.9")
}

// Round dots are separate subpaths of one fill (Skia strokes each
// zero-length dash of SkDashPathEffect as its own capped contour,
// src/utils/SkDashPath.cpp:309-458 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e): nothing joins one dot to the
// next. 5px dotted sides centre their dots 2.5px into the border, so the
// dots stop at the padding edge and the padding beside them keeps its
// background (css-backgrounds/background-334). Once textshape dashes
// strokes itself (MAZ-219), this guards the move onto SetDash.
func TestDrawLineWithStyle_DotsAreSeparate(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="position:absolute;left:10px;top:10px;width:40px;height:40px;background:lime;border-left:5px dotted black;border-top:5px dotted black"></div></body></html>`, 70, 70)
	lime := color.RGBA{0, 255, 0, 255}
	assertPixel(t, img, 15, 35, lime, "padding beside the left side's dots")
	assertPixel(t, img, 35, 15, lime, "padding below the top side's dots")
}
