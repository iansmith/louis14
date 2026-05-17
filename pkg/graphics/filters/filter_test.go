package filters

import (
	"image"
	"testing"
)

// TestFilter_TaintedNonDisplacementProducesNormalOutput asserts the
// Blink-correct semantics for tainting: tainting is metadata that
// propagates through the graph, but the ONLY primitive whose output
// changes when tainted is feDisplacementMap (which becomes a
// pass-through). All other primitives must produce normal output even
// when their input or themselves carry the tainted bit.
//
// Pre-fix: filter.go's blanket `if e.OriginTainted() { out = newRGBA(...) }`
// fallback replaces feBlend's output with transparent black, so the
// blend result over a non-zero base is fully zero.
//
// Post-fix: feBlend produces the normal blend of its inputs; the
// tainted bit on its output is metadata only.
//
// Mirrors Blink fe_displacement_map.cc::CreateImageFilter where the
// only early-return on OriginTainted is in FEDisplacementMap; no other
// FE* primitive consults the bit at render time.
func TestFilter_TaintedNonDisplacementProducesNormalOutput(t *testing.T) {
	region := image.Rect(0, 0, 4, 4)

	// Build a SourceGraphic filled opaque red. This is in2 (bottom layer)
	// to the blend and is NOT tainted.
	src := NewSourceGraphic(InterpolationSpaceSRGB)
	src.image = solidImage(region, 255, 0, 0, 255)

	// Build a transparent flood. This is the TOP layer (in1) to the blend.
	// Mark it tainted manually to simulate Phase 1 propagation from a
	// currentcolor flood. With Mode=Normal and the top layer fully
	// transparent (alpha=0), the normal blend formula
	// `cr = (1-qa)*cb + ca` reduces to `cr = cb` — i.e. the output
	// should be IDENTICAL to the bottom (red) layer.
	flood := NewFEFlood(InterpolationSpaceSRGB, 0, 0, 0, 0)
	flood.SetOriginTainted()

	blend := NewFEBlend(flood, src, InterpolationSpaceSRGB, BlendModeNormal)
	// Simulate the SVGFilterBuilder.BuildGraph one-line propagation
	// (svg_filter_builder.go:240-243) explicitly so this unit test does
	// not depend on the SVG builder; the builder runs the equivalent of
	//   if blend.InputsTaintOrigin() || prim.TaintsOrigin() {
	//       blend.SetOriginTainted()
	//   }
	// at construction time.
	if blend.InputsTaintOrigin() {
		blend.SetOriginTainted()
	}
	if !blend.OriginTainted() {
		t.Fatal("test setup error: blend should be tainted via InputsTaintOrigin propagation")
	}

	f := &Filter{FilterRegion: region, Source: src, LastEffect: blend}
	out := f.Apply()
	if out == nil {
		t.Fatal("Apply returned nil")
	}

	// Every pixel should be opaque red (the bottom layer, unchanged by
	// blending with a fully-transparent top). Pre-fix the blanket
	// fallback zeroes the buffer.
	for i := 0; i+3 < len(out.Pix); i += 4 {
		r, g, b, a := out.Pix[i+0], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3]
		if r != 255 || g != 0 || b != 0 || a != 255 {
			t.Fatalf("pixel %d: got (%d, %d, %d, %d); expected (255, 0, 0, 255) — tainted feBlend output must be normal blend, not transparent black", i/4, r, g, b, a)
		}
	}
}
