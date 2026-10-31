package text

import "testing"

// TestHarfBuzzFace_ReRegisterReplacesVerticalData (LOU-424): re-registering
// an @font-face under the same family and variant — the same synthetic font
// path — replaces the vertical data with the new face's, as the
// GlyphProvider's registered map is last-wins. Vertical data cached by path
// would keep the first face's tables and leak them across reftests that reuse
// a family name.
func TestHarfBuzzFace_ReRegisterReplacesVerticalData(t *testing.T) {
	hasVerticalMetrics := func(path string) bool {
		return fontPlatformData{fontPath: path, size: 20}.GetHarfBuzzFace().VerticalData().HasVerticalMetrics()
	}
	fr := NewFontRegistry()
	first := registerTestFace(t, fr, "LOU424-Swap", "ExTest.woff", fetchCSSValuesResource)
	if hasVerticalMetrics(first) {
		t.Fatal("ExTest: HasVerticalMetrics = true, want false")
	}
	second := registerTestFace(t, fr, "LOU424-Swap", "ChTestShortZero.woff", fetchCSSValuesResource)
	if second != first {
		t.Fatalf("fixture: re-registration path %q != %q", second, first)
	}
	if !hasVerticalMetrics(second) {
		t.Error("after re-registering ChTestShortZero: HasVerticalMetrics = false, want true")
	}
}

// TestHarfBuzzFace_VerticalDataIgnoresAscentOverride (LOU-424): the height
// fallback of a font with no vmtx is FontMetrics::Height() of the font's own
// metrics — HarfBuzzFontData::UpdateFallbackMetricsAndScale calls
// AscentDescentWithHacks with no ascent/descent override (platform/fonts/
// shaping/harfbuzz_font_data.h:62-69 @
// 5992657f556bcd9d207a4aca187488462f027cab) — so an @font-face
// ascent-override does not change the upright "0" advance. Liberation Serif
// (no vmtx) is registered as an @font-face so it can carry an override; its
// native Height at 20px differs from 1em, so the assertion also discriminates
// the old 1em stand-in.
func TestHarfBuzzFace_VerticalDataIgnoresAscentOverride(t *testing.T) {
	fr := NewFontRegistry()
	p := registerTestFace(t, fr, "LOU424-Override", "LiberationSerif-Regular.ttf", fetchTestFont)
	ascent := 1.5
	fr.SetFontFaceOverride(p, MetricsOverride{Ascent: &ascent})
	want := FontAscentFromFont(20, p, nil) + FontDescentFromFont(20, p, nil)
	if want == 20 {
		t.Fatalf("fixture: native Height at 20px = 20, must differ from 1em")
	}
	if overridden := FontAscentFromFont(20, p, fr) + FontDescentFromFont(20, p, fr); overridden == want {
		t.Fatalf("fixture: the ascent-override must change the registry ascent (%v)", overridden)
	}
	f := createFontWith(NewCSSFontSelector(configWithRegistry(fr)), "20px", "LOU424-Override",
		"writing-mode", "vertical-rl", "text-orientation", "upright")
	if f.ZeroWidth != want {
		t.Errorf("upright ZeroWidth = %v, want the native Height %v (no ascent-override)", f.ZeroWidth, want)
	}
}
