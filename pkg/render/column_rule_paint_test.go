package render

// Column-rule painting parity tests (LOU-366).
//
// drawColumnRules mirrors Blink BoxFragmentPainter::PaintColumnRules
// (box_fragment_painter.cc:1775-1913 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// per-row rule segments derived from the column fragmentainer children,
// logical-to-physical conversion through WritingModeConverter, and
// ToPixelSnappedRect before filling. These tests pin the three behaviors
// against full HTML->layout->paint pipeline renders.

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// renderColumnRuleHTML lays out and paints src on a white w x h canvas.
func renderColumnRuleHTML(t *testing.T, src string, w, h int) *image.RGBA {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	engine := layout.NewLayoutEngine(float64(w), float64(h))
	boxes := engine.Layout(doc)
	target := image.NewRGBA(image.Rect(0, 0, w, h))
	r := NewRendererForImage(target)
	r.Render(boxes)
	return target
}

func rgbAt(img *image.RGBA, x, y int) (uint8, uint8, uint8) {
	c := img.RGBAAt(x, y)
	return c.R, c.G, c.B
}

// TestColumnRule_FractionalCenter_SnappedEdges: a 3px rule whose gap center
// falls on a half pixel (column width 47, gap 7 -> center 50.5) must be
// painted as a crisp pixel-snapped bar, not an anti-aliased band. Mirrors
// Blink's ToPixelSnappedRect(rule) at bfp.cc:1904: origin rounded, size
// snapped against the unsnapped origin -> the bar covers x [50,53) exactly,
// every pixel either pure rule color or pure background.
func TestColumnRule_FractionalCenter_SnappedEdges(t *testing.T) {
	src := `<!DOCTYPE html>
<body style="margin:0">
<div style="width:101px; height:50px; columns:2; column-gap:7px; column-rule:3px solid blue; column-fill:auto;">
  <div style="height:200px"></div>
</div>`
	img := renderColumnRuleHTML(t, src, 300, 100)

	// Scan the row through the first rule (logical rect x 49..52 unsnapped).
	const y = 25
	blue := 0
	for x := 40; x < 64; x++ {
		r, g, b := rgbAt(img, x, y)
		isBlue := r == 0 && g == 0 && b == 255
		isWhite := r == 255 && g == 255 && b == 255
		if !isBlue && !isWhite {
			t.Errorf("pixel (%d,%d) = (%d,%d,%d): anti-aliased rule edge; want pure blue or pure white", x, y, r, g, b)
		}
		if isBlue {
			blue++
		}
	}
	if blue != 3 {
		t.Errorf("rule width at y=%d: got %d blue pixels, want 3 (snapped 3px rule)", y, blue)
	}
}

// TestColumnRule_HTBSpanner_PerRowSegments: a column-span:all child splits
// the columns into two rows. Blink paints one rule segment per row
// (bfp.cc:1837-1889): between the row-1 columns (y 0..40), and between the
// row-2 columns stretched to the content block end (y 60..100). No rule may
// be painted across the spanner band (y 40..60).
//
// Layout (verified): multicol (0,0,100,100); row-1 columns (0,0,45,40) and
// (55,0,45,40); spanner (0,40,100,20); row-2 columns (0,60,45,40) and
// (55,60,45,40). Rule center (45+55)/2 = 50, 10px wide -> x 45..55.
func TestColumnRule_HTBSpanner_PerRowSegments(t *testing.T) {
	src := `<!DOCTYPE html>
<body style="margin:0">
<div style="width:100px; columns:2; column-gap:10px; column-rule:10px solid blue;">
  <div style="height:80px"></div>
  <div style="column-span:all; height:20px"></div>
  <div style="height:80px"></div>
</div>`
	img := renderColumnRuleHTML(t, src, 300, 150)

	assertColor := func(x, y int, wantR, wantG, wantB uint8, what string) {
		t.Helper()
		r, g, b := rgbAt(img, x, y)
		if r != wantR || g != wantG || b != wantB {
			t.Errorf("pixel (%d,%d) = (%d,%d,%d), want (%d,%d,%d): %s", x, y, r, g, b, wantR, wantG, wantB, what)
		}
	}
	assertColor(50, 20, 0, 0, 255, "row-1 rule segment")
	assertColor(50, 80, 0, 0, 255, "row-2 rule segment (stretched to content block end)")
	assertColor(50, 50, 255, 255, 255, "spanner band must not carry a rule")
}

// TestColumnRule_VerticalLR_PhysicalHorizontalSegments: in vertical-lr the
// inline axis is vertical, so the inter-column gap runs horizontally and the
// rule is a horizontal bar. All rule geometry is computed logically and
// converted through the multicol's writing mode (WritingModeConverter,
// bfp.cc:1803-1804, 1891-1894), per row of columns.
//
// Layout (verified): section (0,0,100,400); row-1 columns (0,0,40,195) and
// (0,205,40,195); spanner (40,0,20,400); row-2 columns (60,0,40,195) and
// (60,205,40,195). Logical center (195+205)/2 = 200, 10px rule -> physical
// bar y 195..205; row-1 segment x 0..40, row-2 segment x 60..100 (stretched
// to the content block end). Nothing may paint at the HTB-mapped position
// (a vertical bar at x 195..205).
func TestColumnRule_VerticalLR_PhysicalHorizontalSegments(t *testing.T) {
	src := `<!DOCTYPE html>
<body style="margin:0">
<section style="writing-mode:vertical-lr; width:100px; height:400px; columns:2; column-gap:10px; column-rule:10px solid orange;">
<div style="block-size:80px"></div>
<div style="column-span:all; block-size:20px"></div>
<div style="block-size:80px"></div>
</section>`
	img := renderColumnRuleHTML(t, src, 300, 450)

	orange := color.RGBA{255, 165, 0, 255}
	assertColor := func(x, y int, want color.RGBA, what string) {
		t.Helper()
		c := img.RGBAAt(x, y)
		if c.R != want.R || c.G != want.G || c.B != want.B {
			t.Errorf("pixel (%d,%d) = (%d,%d,%d), want (%d,%d,%d): %s", x, y, c.R, c.G, c.B, want.R, want.G, want.B, what)
		}
	}
	white := color.RGBA{255, 255, 255, 255}
	assertColor(20, 200, orange, "row-1 rule segment (horizontal bar in the gap)")
	assertColor(80, 200, orange, "row-2 rule segment (stretched to content block end)")
	assertColor(50, 200, white, "spanner band must not carry a rule")
	assertColor(200, 100, white, "no HTB-mapped vertical rule at logical inline offset")
}

// TestColumnRule_FractionalWidth_ClampedLikeBorderWidth: column-rule-width
// computed values snap like border-width — 0 < w < 1 rounds up to 1, w >= 1
// floors to an integer. Mirrors Blink StyleBuilderConverter::ClampLineWidth
// (style_builder_converter.cc:1964-1971 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// which column-rule-width reaches through ConvertBorderWidth (:2600-2601).
// Each multicol is 100px wide with 2 columns and a 10px gap, so the rule is
// centered at x=50; the painted pixel width must equal the clamped value.
// (WPT: subpixel-column-rule-width.tentative.html.)
func TestColumnRule_FractionalWidth_ClampedLikeBorderWidth(t *testing.T) {
	src := `<!DOCTYPE html>
<body style="margin:0">
<div style="width:100px; height:50px; columns:2; column-gap:10px; column-rule:0.5px solid blue; column-fill:auto;">
  <div style="height:200px"></div>
</div>
<div style="width:100px; height:50px; columns:2; column-gap:10px; column-rule:1.3px solid blue; column-fill:auto;">
  <div style="height:200px"></div>
</div>
<div style="width:100px; height:50px; columns:2; column-gap:10px; column-rule:3.9px solid blue; column-fill:auto;">
  <div style="height:200px"></div>
</div>`
	img := renderColumnRuleHTML(t, src, 300, 200)

	for _, tc := range []struct {
		name      string
		y         int
		wantWidth int
	}{
		{"0.5px rounds up to 1", 25, 1},
		{"1.3px floors to 1", 75, 1},
		{"3.9px floors to 3", 125, 3},
	} {
		blue := 0
		for x := 40; x < 60; x++ {
			r, g, b := rgbAt(img, x, tc.y)
			if r == 0 && g == 0 && b == 255 {
				blue++
			}
		}
		if blue != tc.wantWidth {
			t.Errorf("%s: got %d blue pixels at y=%d, want %d", tc.name, blue, tc.y, tc.wantWidth)
		}
	}
}

// TestColumnRule_EmptyCrossGaps_SuppressesPhantomRule: louis14's multicol
// layout can emit an empty trailing column fragment in a pre-spanner row
// without recording a cross gap for it (multicol_layout.go; observed in
// multicol-span-all-006/007/008, where a phantom 6px column appears next to
// the summary row). Blink's layout never produces such fragments, so its
// painter has no analog of this situation (no Blink analog: guard against a
// louis14 layout artifact). GapGeometry.CrossGaps is layout's authoritative
// record of inter-column gaps: a column pair with no recorded cross gap gets
// no rule segment; with a recorded gap, the segment paints.
func TestColumnRule_EmptyCrossGaps_SuppressesPhantomRule(t *testing.T) {
	paint := func(gg *layout.GapGeometry) *image.RGBA {
		target := image.NewRGBA(image.Rect(0, 0, 100, 50))
		r := NewRendererForImage(target)
		box := &layout.Box{
			Style: css.NewStyle(),
			X:     0, Y: 0, Width: 100, Height: 30,
			Children: []*layout.Box{
				{IsColumnBox: true, X: 0, Y: 0, Width: 40, Height: 30},
				{IsColumnBox: true, X: 60, Y: 0, Width: 40, Height: 30},
			},
			GapGeometry: gg,
		}
		layer := &PaintLayer{
			Box:             box,
			ColumnRuleWidth: 10,
			ColumnRuleStyle: "solid",
			ColumnRuleColor: css.Color{R: 0, G: 0, B: 255, A: 1},
			GapGeometry:     gg,
		}
		r.drawColumnRules(layer)
		return target
	}

	// No recorded cross gaps: the second column box is a layout artifact.
	img := paint(&layout.GapGeometry{ContainerType: layout.GapContainerMulticol, MainDirection: layout.GapForRows})
	if c := img.RGBAAt(50, 15); c.A != 0 {
		t.Errorf("empty CrossGaps: rule painted at (50,15) = %v, want nothing", c)
	}

	// One recorded cross gap: the same geometry paints its rule segment.
	img = paint(&layout.GapGeometry{
		ContainerType: layout.GapContainerMulticol,
		MainDirection: layout.GapForRows,
		CrossGaps:     []layout.CrossGap{{GapInlineOffset: 50}},
	})
	if c := img.RGBAAt(50, 15); c.R != 0 || c.G != 0 || c.B != 255 {
		t.Errorf("recorded CrossGap: pixel (50,15) = %v, want blue rule", c)
	}
}
