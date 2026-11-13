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
	col := firstDarkColumn(img, 39, -1, 13) // y 13 is in the first dash
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
	right := firstDarkColumn(solid, 99, -1, 60)
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

// Upright vertical text measures its decorations from the line-over edge
// (the right side, in vertical-rl and vertical-lr alike) with its central
// baseline: an auto underline resolves to under (ResolveUnderlinePosition,
// core/paint/text_decoration_info.cc:23-53 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and sits
// floor(central ascent + em/2 + text-underline-offset) + 1 in
// (TextDecorationOffset::ComputeUnderlineOffsetForUnder with
// BottomOfEmHeight, core/layout/text_decoration_offset.cc:52-89); a
// line-through sits 2 * ascent / 3 - thickness / 2 in
// (ComputeLineThroughLineData, text_decoration_info.cc:379-389). Measured
// in Chrome 2026-11-13: a 40px Ahem column at x 100-140 with 6px lines and
// a 10px offset has its underline at x 83-88 and its line-through at x
// 116-121.
func TestDecorationLine_UprightVerticalPlacement(t *testing.T) {
	for _, wm := range []string{"vertical-rl", "vertical-lr"} {
		for _, c := range []struct {
			line   string
			x0, x1 int
		}{{"underline", 83, 88}, {"line-through", 116, 121}} {
			img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
				`<div style="position:absolute;left:100px;top:10px;writing-mode:`+wm+`;text-orientation:upright;font:40px/40px Ahem;color:white;`+
				`text-decoration:`+c.line+` solid black;text-decoration-thickness:6px;text-underline-offset:10px;text-decoration-skip-ink:none">XXX</div></body></html>`, 250, 160)
			first, last := darkColumnSpan(img, 60)
			if first != c.x0 || last != c.x1 {
				t.Errorf("%s %s: dark x %d-%d, want Chrome's %d-%d", wm, c.line, first, last, c.x0, c.x1)
			}
		}
	}
}

// A wavy decoration is Blink's WavyGeometry (core/paint/
// decoration_line_painter.cc:125-370 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// MakeWave(t) sets the wavelength to 1 + 2·round(2t + 0.5) and the
// control-point distance to 0.5 + round(3t + 0.5), one cubic per
// wavelength whose centreline sits wavy_offset (t+1, TextDecorationInfo::
// ComputeLineData, core/paint/text_decoration_info.cc:255-280) + 0.5 below
// the line's top and starts each run at the line's start heading away from
// the text. The underline's top is y 28 (baseline 26 + 2px offset). At 3px
// the wave is 15px long, centred on y 32.5, its first trough at x ≈ 13 and
// first crest at x ≈ 22; at 5px it is 23px long, centred on y 34.5, trough
// x ≈ 15, crest x ≈ 27. Pixels from Chrome.
func TestDecorationLine_WavyGeometry(t *testing.T) {
	for _, tc := range []struct {
		thickness                  string
		height                     int
		trough, aboveTrough, crest image.Point
		belowCrest, nextTrough     image.Point
	}{
		{"3px", 40, image.Pt(14, 36), image.Pt(13, 29), image.Pt(21, 29), image.Pt(21, 35), image.Pt(29, 36)},
		{"5px", 50, image.Pt(15, 40), image.Pt(15, 30), image.Pt(27, 28), image.Pt(27, 38), image.Pt(38, 40)},
	} {
		t.Run(tc.thickness, func(t *testing.T) {
			img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
				`<div style="position:absolute;left:10px;top:10px;font:20px/20px Ahem;color:transparent;`+
				`text-decoration:underline wavy black;text-decoration-thickness:`+tc.thickness+`;text-underline-offset:2px;text-decoration-skip-ink:none">XXXXX</div></body></html>`, 130, tc.height)
			assertWaveInk(t, img, tc.trough.X, tc.trough.Y, "first trough, bottom")
			assertPixel(t, img, tc.aboveTrough.X, tc.aboveTrough.Y, white, "above the first trough")
			assertWaveInk(t, img, tc.crest.X, tc.crest.Y, "first crest, top")
			assertPixel(t, img, tc.belowCrest.X, tc.belowCrest.Y, white, "below the first crest")
			assertWaveInk(t, img, tc.nextTrough.X, tc.nextTrough.Y, "second trough, one wavelength on")
		})
	}
}

// Upright vertical text paints the same wave line-relative, rotated
// clockwise, so it runs down from the run's start with its first trough
// away from the glyphs (left, line-under) and its first crest towards
// them. The wave is measured from the solid underline's left edge, so the
// test pins the wave, not where the line sits (LOU-1027): Chrome draws the
// 3px line at x 31-34 and the wave's first trough reaches x 25, its first
// crest x 32.
func TestDecorationLine_UprightVerticalWavy(t *testing.T) {
	page := func(style string) string {
		return `<!DOCTYPE html><html><body style="margin:0">` +
			`<div style="position:absolute;left:40px;top:10px;writing-mode:vertical-rl;text-orientation:upright;font:20px/20px Ahem;color:transparent;` +
			`text-decoration:underline ` + style + ` black;text-decoration-thickness:3px;text-underline-offset:6px;text-decoration-skip-ink:none">XXXXX</div></body></html>`
	}
	solid := render.RenderPage(t, page("solid"), 70, 130)
	left := firstDarkColumn(solid, 0, 40, 50)
	if left < 0 {
		t.Fatal("no solid underline left of the glyph column")
	}
	img := render.RenderPage(t, page("wavy"), 70, 130)
	assertWaveInk(t, img, left-6, 13, "first trough, leftmost")
	assertPixel(t, img, left+1, 13, white, "right of the first trough")
	assertWaveInk(t, img, left-2, 17, "centreline crossing")
	assertWaveInk(t, img, left+1, 20, "first crest, rightmost")
	assertPixel(t, img, left-5, 20, white, "left of the first crest")
	assertWaveInk(t, img, left-6, 28, "second trough, one 15px wavelength on")
}

// darkColumnSpan returns the first and last columns of row y at least half
// covered by black (red below 128), or -1, -1. Half coverage is how the
// Chrome screenshots were probed: a line-through rect at 115.67-121.67
// covers a third of pixel 115 (left out) and two thirds of pixel 121
// (counted).
func darkColumnSpan(img image.Image, y int) (first, last int) {
	first, last = -1, -1
	for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
		if r, _, _, _ := img.At(x, y).RGBA(); r>>8 < 128 {
			if first < 0 {
				first = x
			}
			last = x
		}
	}
	return first, last
}

// firstDarkColumn scans row y from column from towards to (exclusive)
// and returns the first column darker than 64, or -1.
func firstDarkColumn(img image.Image, from, to, y int) int {
	step := 1
	if to < from {
		step = -1
	}
	for x := from; x != to; x += step {
		if r, _, _, _ := img.At(x, y).RGBA(); r>>8 < 64 {
			return x
		}
	}
	return -1
}

// assertWaveInk checks that the pixel at (x, y) is wave ink. Chrome paints
// it black; louis14 may leave up to 16 levels of background at the ink's
// outer edge, since textshape strokes a 32-segment flattening where Skia
// offsets the curve: a real divergence, tracked by MAZ-222 — tighten to an
// exact match when it lands. A stroke join missing the outer side of a
// turn (MAZ-221) leaves ~100.
func assertWaveInk(t *testing.T, img image.Image, x, y int, why string) {
	t.Helper()
	if r, _, _, _ := img.At(x, y).RGBA(); r>>8 > 16 {
		t.Errorf("pixel (%d,%d) = %d, want ink (≤ 16): %s", x, y, r>>8, why)
	}
}

// Upright vertical text resolves text-underline-position by script first
// (ResolveUnderlinePosition, core/paint/text_decoration_info.cc:23-53 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): Japanese (Katakana/Hiragana)
// or Korean text puts the underline over unless the position has left;
// any other text puts it over only for right. Han (zh) is neither, and
// left never flips, in vertical-lr either. An over underline paints with
// the overline's geometry from the em box's top, keeping its
// text-underline-offset, and the overline swaps to the under side without
// one (flip_underline_and_overline_, :224-235; ComputeUnderlineLineData /
// ComputeOverlineLineData, :335-377). Measured in Chrome 2026-11-13 on a
// 40px Ahem column at x 100-140 with 6px lines and a 10px offset.
func TestDecorationLine_UprightUnderlinePosition(t *testing.T) {
	for _, c := range []struct {
		name, wm, lang, line, pos string
		x0, x1                    int
	}{
		{"en underline", "vertical-rl", "en", "underline", "auto", 83, 88},
		{"en overline", "vertical-rl", "en", "overline", "auto", 140, 145},
		{"ja underline goes over", "vertical-rl", "ja", "underline", "auto", 151, 156},
		{"en right goes over", "vertical-rl", "en", "underline", "right", 151, 156},
		{"ja overline goes under", "vertical-rl", "ja", "overline", "auto", 93, 98},
		{"ja left stays under", "vertical-rl", "ja", "underline", "left", 83, 88},
		{"zh stays under", "vertical-rl", "zh", "underline", "auto", 83, 88},
		{"vertical-lr left stays under", "vertical-lr", "en", "underline", "left", 83, 88},
		{"vertical-lr right goes over", "vertical-lr", "en", "underline", "right", 151, 156},
	} {
		img := render.RenderPage(t, `<!DOCTYPE html><html lang="`+c.lang+`"><body style="margin:0">`+
			`<div style="position:absolute;left:100px;top:10px;writing-mode:`+c.wm+`;text-orientation:upright;font:40px/40px Ahem;color:white;`+
			`text-decoration:`+c.line+` solid black;text-decoration-thickness:6px;text-underline-offset:10px;text-underline-position:`+c.pos+`;text-decoration-skip-ink:none">XXX</div></body></html>`, 250, 160)
		first, last := darkColumnSpan(img, 60)
		if first != c.x0 || last != c.x1 {
			t.Errorf("%s: dark x %d-%d, want Chrome's %d-%d", c.name, first, last, c.x0, c.x1)
		}
	}
}

// Skip-ink clips each glyph intercept out dilated by the line's thickness,
// at most 13px (TextPainter::ClipDecorationLine,
// core/paint/text_painter.cc:46, :606-622 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 6px underline crossing "X
// X"'s 40px Ahem glyphs (x 10-50, 90-130) keeps x 56-83 in Chrome
// 2026-11-13.
func TestDecorationLine_SkipInkDilatesByThickness(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><html lang="en"><body style="margin:0">`+
		`<div style="position:absolute;left:10px;top:10px;font:40px/40px Ahem;color:transparent;`+
		`text-decoration:underline solid black;text-decoration-thickness:6px;text-underline-offset:-15px">X X</div></body></html>`, 200, 80)
	first, last := darkColumnSpan(img, 30)
	if first != 56 || last != 83 {
		t.Errorf("underline kept x %d-%d, want Chrome's 56-83", first, last)
	}
}
