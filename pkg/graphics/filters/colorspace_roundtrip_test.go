package filters

import (
	"image"
	"testing"
)

// TestSourceGraphic_OperatesInSRGB asserts the Blink-correct contract
// that the SourceGraphic / SourceAlpha built-ins operate in sRGB
// (kInterpolationSpaceSRGB) regardless of the filter's nominal operating
// space. The element's rendered content is device sRGB; Blink's
// SourceGraphic constructor calls SetOperatingInterpolationSpace(
// kInterpolationSpaceSRGB) unconditionally, and SourceAlpha inherits its
// source's space (source_graphic.cc / source_alpha.cc @ Chromium main).
//
// Pre-fix NewSVGFilterBuilder created SourceGraphic / SourceAlpha with
// the filter's operating space (linearRGB for the SVG default). Because
// the graph only converts across edges where the spaces DIFFER, an sRGB
// source feeding a "linearRGB" SourceGraphic skipped the sRGB->linearRGB
// input conversion while the compositor still applied linearRGB->sRGB on
// output — net brightening (green 128 -> ~188). This test is RED in that
// state.
func TestSourceGraphic_OperatesInSRGB(t *testing.T) {
	b := NewSVGFilterBuilder(InterpolationSpaceLinearRGB)
	if got := b.Source().OperatingSpace(); got != InterpolationSpaceSRGB {
		t.Errorf("SourceGraphic operating space = %v; want sRGB (%v) — device content is sRGB regardless of color-interpolation-filters", got, InterpolationSpaceSRGB)
	}
	if got := b.SourceAlpha().OperatingSpace(); got != InterpolationSpaceSRGB {
		t.Errorf("SourceAlpha operating space = %v; want sRGB (%v)", got, InterpolationSpaceSRGB)
	}
}

// TestSourceGraphic_SRGBRoundTripsThroughLinearBlur is the end-to-end
// consequence: a green (#008000) sRGB SourceGraphic fed into a no-op
// linearRGB feGaussianBlur (stdDeviation=0) must, after the final
// linearRGB->sRGB conversion the compositor applies, round-trip back to
// exactly sRGB green (0,128,0). This is the graph-level invariant the
// builder fix makes hold for filter-scale-001 / filter-scaling-001.
func TestSourceGraphic_SRGBRoundTripsThroughLinearBlur(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)

	src := NewSourceGraphic(InterpolationSpaceSRGB)
	src.image = solidImage(region, 0, 128, 0, 255) // sRGB green

	// A no-op linearRGB blur (the SVG default color-interpolation-filters
	// space). With src in sRGB the graph must convert sRGB->linearRGB on
	// the way in, then the compositor converts linearRGB->sRGB on output.
	blur := NewFEGaussianBlur(src, InterpolationSpaceLinearRGB, 0, 0)

	f := &Filter{FilterRegion: region, Source: src, LastEffect: blur}
	out := f.ApplyToSRGB()
	if out == nil {
		t.Fatal("ApplyToSRGB returned nil")
	}

	i := out.PixOffset(2, 2)
	r, g, b, a := out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3]
	if r != 0 || !near(g, 128, 1) || b != 0 || a != 255 {
		t.Fatalf("sRGB green through no-op linearRGB blur: got (%d,%d,%d,%d); want (0,128,0,255) — SourceGraphic must operate in sRGB so the graph round-trips the color", r, g, b, a)
	}
}

// near reports whether v is within tol of target (off-by-one rounding).
func near(v, target uint8, tol int) bool {
	d := int(v) - int(target)
	if d < 0 {
		d = -d
	}
	return d <= tol
}
