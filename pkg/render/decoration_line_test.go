package render_test

import (
	"image"
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
	assertPixel(t, img, 106, 29, black, "last dot, centre x 96.92")
	assertPixel(t, img, 111, 29, white, "past the last dot, which ends at x 99.92")
}

// Upright vertical text paints its decorations line-relative under the
// writing-mode rotation, through the same DrawLineAsStroke as horizontal
// text (TextFragmentPainter::Paint, core/paint/text_fragment_painter.cc:
// 369-379, :686-692 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a 3px
// dashed underline down a 100px upright run from y 10 has the horizontal
// case's fitted dashes along y: 6px dashes, 3.4px gaps, the last ending at
// the run's end. The underline is offset 6px to the left of the glyph
// column (x 40-60), so the first dark column left of x 40 is the line's.
func TestDecorationLine_UprightVerticalDashed(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="position:absolute;left:40px;top:10px;writing-mode:vertical-rl;text-orientation:upright;font:20px/20px Ahem;`+
		`text-decoration:underline dashed black;text-decoration-thickness:3px;text-underline-offset:6px;text-decoration-skip-ink:none">XXXXX</div></body></html>`, 70, 130)
	col := -1
	for x := 39; x >= 0 && col < 0; x-- {
		for y := 10; y < 110; y++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r>>8 < 64 {
				col = x
				break
			}
		}
	}
	if col < 0 {
		t.Fatal("no underline found left of the glyph column")
	}
	assertPixel(t, img, col, 13, black, "first dash, y 10-16")
	assertPixel(t, img, col, 17, white, "first gap, y 16-19.4")
	assertPixel(t, img, col, 22, black, "second dash, y 19.4-25.4")
	assertPixel(t, img, col, 107, black, "last dash, y 104-110")
}

// A double decoration is two lines of the decoration's thickness, the
// second double_offset = thickness + 1 further from the text (underline:
// line-under) — TextDecorationInfo::ComputeLineData and
// DecorationLinePainter::Paint's kDoubleStroke (core/paint/
// text_decoration_info.cc:255-282, decoration_line_painter.cc:464-473 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), verified in Chrome
// 2026-11-13: a 6px upright vertical-rl double underline is the solid
// underline's 6px column plus a 6px column 1px further left. Measured
// against the solid underline's own column, so this pins the shape, not
// where the line sits.
func TestDecorationLine_UprightVerticalDouble(t *testing.T) {
	page := func(style string) string {
		return `<!DOCTYPE html><html><body style="margin:0">` +
			`<div style="position:absolute;left:100px;top:10px;writing-mode:vertical-rl;text-orientation:upright;font:40px/40px Ahem;` +
			`text-decoration:underline ` + style + ` black;text-decoration-thickness:6px;text-underline-offset:10px;text-decoration-skip-ink:none">XXX</div></body></html>`
	}
	dark := func(img *image.RGBA, x int) bool {
		r, _, _, _ := img.At(x, 60).RGBA()
		return r>>8 < 64
	}
	solid := render.RenderPage(t, page("solid"), 160, 140)
	right := -1
	for x := 99; x >= 0 && right < 0; x-- {
		if dark(solid, x) {
			right = x
		}
	}
	if right < 0 {
		t.Fatal("no solid underline left of the glyph column")
	}
	left := right - 5 // the 6px solid column is [left, right]
	if dark(solid, left-1) {
		t.Fatalf("x %d: the solid underline is wider than 6px", left-1)
	}
	double := render.RenderPage(t, page("double"), 160, 140)
	for x := left; x <= right; x++ {
		if !dark(double, x) {
			t.Errorf("x %d: the first line, on the solid underline's column, is clear", x)
		}
	}
	if dark(double, left-1) {
		t.Errorf("x %d: want the 1px gap between the lines", left-1)
	}
	for x := left - 7; x <= left-2; x++ {
		if !dark(double, x) {
			t.Errorf("x %d: the second 6px line, 7px further from the text, is clear", x)
		}
	}
	if dark(double, left-8) {
		t.Errorf("x %d: past the second line, want clear", left-8)
	}
}
