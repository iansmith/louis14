package render

import (
	"image"
	"testing"

	"mazarin/textshape"

	"louis14/pkg/css"
	"louis14/pkg/text"
)

// TestAhemGlyphNoFractionalSeam is the RED reproduction for LOU-367: the
// Ahem em box must rasterize onto whole device rows so that a full-em glyph
// painted at a fractional baseline produces fully-opaque interior rows and
// NO sub-opaque seam row at the em-box top/bottom edges.
//
// Ahem's uppercase glyphs (U+0041 'A') fill the entire em square solid black,
// from ascent (0.8em above the baseline) to descent (0.2em below). At
// font-size 16px that is a 16px-tall solid column spanning [baseline-12.8,
// baseline+3.2]. `drawTextStr` rounds the baseline (math.Round) but the glyph
// raster mask is baked with its 12.8px top edge landing on a fractional
// device row, so the rasterizer antialiases the top row to ~0.8 coverage and
// the bottom row to ~0.2 coverage — a visible seam (LOU-366's pixel-exact
// analysis: 144px, max diff 50 on multicol-rule-px-001).
//
// Chrome grid-fits the glyph so the em box lands on integer rows: every
// interior row is fully opaque and there is no partial-coverage seam.
//
// No Blink analog at the layout level: this is rasterizer grid-fitting
// (Freetype hinting in Skia's glyph pipeline). The louis14-side remedy is to
// snap the glyph raster origin consistently with the already-rounded
// baseline so the em box edges land on whole rows.
func TestAhemGlyphNoFractionalSeam(t *testing.T) {
	// LOU-367: the α=0.804/0.2 seam was baked INTO the glyph alpha mask by
	// mazzy's rasterizer (RenderGlyph in mazarin/textshape/rasterize.go floored
	// the outline bounds onto the pixel grid, so Ahem's 12.7969px ascent edge
	// antialiased into the mask itself — the raw mask for 'A' @16px was 17x18
	// with row 0 alpha=205 and row 16 alpha=51, present before any baseline
	// positioning). drawTextStr's baseline round only chooses which integer
	// device row the mask lands on; it cannot alter the mask's per-row
	// coverage. The fix grid-fits the mask in RenderGlyph (rasterizes with the
	// exact fractional top edge as the y origin so the em box lands on whole
	// rows). This test guards that mazzy-side grid-fit against regression.
	const fontSize = 16
	const canvasW, canvasH = 32, 48

	fc := text.DefaultFontConfig()
	if fc.Ahem == "" {
		t.Skip("Ahem font not configured")
	}

	// Paint 'A' (a solid full-em Ahem square) at several fractional
	// baselines. Whatever the fraction, after the render-side origin snap the
	// painted em box must have fully-opaque interior rows and no sub-opaque
	// seam row at its top/bottom edges.
	for _, frac := range []float64{0.0, 0.2, 0.4, 0.5, 0.8} {
		t.Run("", func(t *testing.T) {
			target := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
			dc := textshape.NewDrawContextForImage(target, newProvider())
			r := &Renderer{
				dc:        dc,
				target:    target,
				fonts:     fc,
				fontCache: make(map[fontCacheKey]int32),
			}

			fontID := r.openFont(fc.Ahem, fontSize)
			if fontID < 0 {
				t.Fatalf("openFont(Ahem, %d) failed", fontSize)
			}
			m := dc.GetFontMetrics(fontID)
			ascent := float64(m.Ascent) / 64.0

			// Baseline chosen so the intended em-box top sits at an integer
			// row plus `frac`: baseline = topRow + frac + ascent. drawTextStr
			// rounds this, so the raster origin must be snapped to match.
			const topRow = 8
			baseline := float64(topRow) + frac + ascent

			// Fill black-on-white so glyph coverage reads directly off the
			// alpha/darkness of each pixel.
			for i := 0; i+3 < len(target.Pix); i += 4 {
				target.Pix[i+0] = 255
				target.Pix[i+1] = 255
				target.Pix[i+2] = 255
				target.Pix[i+3] = 255
			}
			r.setColor(css.Color{R: 0, G: 0, B: 0, A: 1})

			r.drawTextStr("A", fontID, 4, baseline, nil)

			// Coverage of the darkest column (glyph interior) per row: 255 for
			// a fully-black (fully-covered) pixel, 0 for untouched white.
			rowCoverage := func(y int) int {
				maxCov := 0
				for x := 0; x < canvasW; x++ {
					p := target.RGBAAt(x, y)
					cov := 255 - int(p.R) // black-on-white → darkness == coverage
					if cov > maxCov {
						maxCov = cov
					}
				}
				return maxCov
			}

			// Locate the painted em box: first and last rows with any ink.
			firstInk, lastInk := -1, -1
			for y := 0; y < canvasH; y++ {
				if rowCoverage(y) > 0 {
					if firstInk < 0 {
						firstInk = y
					}
					lastInk = y
				}
			}
			if firstInk < 0 {
				t.Fatalf("frac=%.1f: no ink painted", frac)
			}

			// Every row of the em box (including the top and bottom edges)
			// must be fully opaque — no antialiased seam. Grid-fitting makes
			// the box span whole rows, so there is no partial-coverage edge.
			for y := firstInk; y <= lastInk; y++ {
				cov := rowCoverage(y)
				if cov < 255 {
					t.Errorf("frac=%.1f: row %d has sub-opaque coverage %d (want 255); fractional em-box seam not grid-fitted",
						frac, y, cov)
				}
			}

			// The solid Ahem em box is exactly fontSize rows tall once
			// grid-fitted.
			gotHeight := lastInk - firstInk + 1
			if gotHeight != fontSize {
				t.Errorf("frac=%.1f: em box height %d rows (want %d); fractional rasterization spread the box across an extra AA row",
					frac, gotHeight, fontSize)
			}
		})
	}
}
