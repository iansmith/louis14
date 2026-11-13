package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// decorationLinePage is a 100px Ahem run at (10,10) with an underline of
// the given style and thickness, its glyphs transparent and skip-ink off
// so only the line paints. The underline's rect starts at the baseline,
// y 26 (Ahem's ascent is 16px).
func decorationLinePage(style, thickness string) string {
	return `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:absolute;left:10px;top:10px;font:20px/20px Ahem;color:transparent;` +
		`text-decoration:underline ` + style + ` black;text-decoration-thickness:` + thickness + `;text-decoration-skip-ink:none">XXXXX</div></body></html>`
}

// A dashed text decoration fits its dashes to the line's length, as a
// dashed border side does: DecorationLinePainter::Paint draws it with
// DrawLineAsStroke (core/paint/decoration_line_painter.cc:437-476,
// :55-101 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), sized by
// StyledStrokeData::ConvertToStrokeData on the 100px line. 3px thick, the
// dashes are 6px with 3.4px gaps (SelectBestDashGap,
// platform/graphics/styled_stroke_data.cc:40-58), the last ending at the
// line's end; the line centres on y 27.5 (GetSnappedPointsForTextLine
// floors 26 + 1.5, and an odd thickness shifts down half a pixel).
func TestDecorationLine_DashedFitsGaps(t *testing.T) {
	img := render.RenderPage(t, decorationLinePage("dashed", "3px"), 130, 40)
	assertPixel(t, img, 13, 27, black, "first dash, x 0-6")
	assertPixel(t, img, 17, 27, white, "first gap, x 6-9.4")
	assertPixel(t, img, 22, 27, black, "second dash, x 9.4-15.4")
	assertPixel(t, img, 107, 27, black, "last dash, x 94-100")
}

// A dotted decoration thicker than 3px draws round dots with the
// best-fitting gap, the line pulled in by half a dot at each end
// (DrawLineAsStroke, decoration_line_painter.cc:78-84; DashEffectFromStrokeStyle,
// styled_stroke_data.cc:115-132). 6px thick over 100px: nine dots centred
// 11.74px apart from x 3, the last at 96.92, on y 29 (floor(26 + 3)).
func TestDecorationLine_DottedFitsDots(t *testing.T) {
	img := render.RenderPage(t, decorationLinePage("dotted", "6px"), 130, 40)
	assertPixel(t, img, 13, 29, black, "first dot, centre x 3")
	assertPixel(t, img, 19, 29, white, "gap between the dots at x 3 and 14.74")
	assertPixel(t, img, 104, 29, black, "last dot, centre x 96.92")
	assertPixel(t, img, 111, 29, white, "past the last dot, which ends at x 99.92")
}
