package render

import (
	"os"
	"reflect"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
	"louis14/pkg/text"
	"mazarin/textshape"
)

func feature(tag string, v uint32) textshape.FontFeature {
	return textshape.FontFeature{Tag: [4]byte{tag[0], tag[1], tag[2], tag[3]}, Value: v}
}

// TestResolveFontFeatureSettingsDescriptor ports Blink's
// ResolveFontFeatureSettingsDescriptor (platform/fonts/
// resolved_font_features.cc:14-43 @ 5992657f556bcd9d207a4aca187488462f027cab):
// a descriptor feature is kept unless the font-feature-settings property
// names the same tag.
func TestResolveFontFeatureSettingsDescriptor(t *testing.T) {
	descriptor := []textshape.FontFeature{feature("smcp", 1), feature("liga", 0), feature("onum", 1)}
	property := []textshape.FontFeature{feature("liga", 1)}
	want := []textshape.FontFeature{feature("smcp", 1), feature("onum", 1)}
	if got := resolveFontFeatureSettingsDescriptor(property, descriptor); !reflect.DeepEqual(got, want) {
		t.Errorf("resolve(property liga, descriptor smcp/liga/onum) = %v, want %v", got, want)
	}
	if got := resolveFontFeatureSettingsDescriptor(property, nil); len(got) != 0 {
		t.Errorf("resolve(no descriptor) = %v, want none", got)
	}
}

// descriptorTestRenderer is a renderer whose registry holds two Ahem faces:
// LOU465-FFS with a font-feature-settings descriptor, LOU465-Plain without.
func descriptorTestRenderer(t *testing.T, descriptor string) *Renderer {
	t.Helper()
	fr := text.NewFontRegistry()
	src := []css.FontFaceSrcValue{{URL: css.CSSURIValue{Data: css.URLData{Relative: "Ahem.ttf", Absolute: "Ahem.ttf"}}}}
	fr.AddUserFontFaceRules([]css.FontFaceRule{
		{Family: "LOU465-FFS", Src: src, Weight: "normal", Style: "normal", FeatureSettings: descriptor},
		{Family: "LOU465-Plain", Src: src, Weight: "normal", Style: "normal"},
	}, func(url string) ([]byte, error) { return os.ReadFile("../../fonts/" + url) })
	fc := text.DefaultFontConfig()
	fc.Registry = fr
	r := NewRenderer(10, 10)
	r.SetFonts(fc)
	t.Cleanup(r.closeTempFonts)
	return r
}

func layerWithStyle(props map[string]string) *PaintLayer {
	s := css.NewStyle()
	for k, v := range props {
		s.Properties[k] = v
	}
	return newPaintLayer(&layout.Box{Style: s, Text: "x"})
}

// TestShapingFeatures_BlinkOrder pins the final feature list a text run is
// shaped with, descriptor + property + font-variant-* together, in Blink's
// order:
//
//  1. the face's ResolvedFeatures — font-variant-alternates, then the
//     @font-face descriptor features named by neither the property nor the
//     alternates (FontDescription::ResolveFontFeatures /
//     MergeFontFeatureSettingsWithDescriptor, platform/fonts/
//     font_description.cc:549-576), inserted first by ShapeRange
//     (platform/fonts/shaping/harfbuzz_shaper.cc:327-340);
//  2. the font-variant-caps features CapsFeatureSettingsScopedOverlay
//     prepends to the font's features (harfbuzz_shaper.cc:823-857);
//  3. the font's features, FontFeatureRange::FromFontDescription order:
//     font-kerning, font-variant-ligatures, -east-asian, -numeric, then the
//     font-feature-settings property last so it wins
//     (platform/fonts/shaping/font_features.cc:33-235),
//
// all @ 5992657f556bcd9d207a4aca187488462f027cab.
func TestShapingFeatures_BlinkOrder(t *testing.T) {
	r := descriptorTestRenderer(t, `"hist" 0, "liga" 1, "onum" 1, "kern" 0`)
	layer := layerWithStyle(map[string]string{
		"font-family":             "LOU465-FFS",
		"font-feature-settings":   `"liga" 0, "smcp" 0`,
		"font-variant-caps":       "small-caps",
		"font-variant-ligatures":  "discretionary-ligatures",
		"font-variant-alternates": "historical-forms",
	})
	fontID := r.fontIDForLayer(layer)
	if fontID < 0 {
		t.Fatal("LOU465-FFS did not open")
	}
	want := []textshape.FontFeature{
		feature("hist", 1),                     // alternates
		feature("onum", 1), feature("kern", 0), // descriptor leftovers
		feature("smcp", 1),                     // caps
		feature("dlig", 1),                     // ligatures
		feature("liga", 0), feature("smcp", 0), // property
	}
	if got := r.shapeFeatures(fontID, layer.FontFeatures); !reflect.DeepEqual(got, want) {
		t.Errorf("shaping features = %v\nwant %v", got, want)
	}
}

// TestShapingFeatures_KeyedToFace: the descriptor features belong to the
// face a run is shaped with (its FontPlatformData), not to the layer — the
// same layer's features shaped with a face lacking the descriptor get none,
// and two property values over one descriptor face resolve independently.
func TestShapingFeatures_KeyedToFace(t *testing.T) {
	r := descriptorTestRenderer(t, `"smcp" 1, "liga" 0`)
	a := layerWithStyle(map[string]string{"font-family": "LOU465-FFS", "font-feature-settings": `"liga" 1`})
	b := layerWithStyle(map[string]string{"font-family": "LOU465-FFS"})
	plain := layerWithStyle(map[string]string{"font-family": "LOU465-Plain", "font-feature-settings": `"liga" 1`})

	idA, idB, idPlain := r.fontIDForLayer(a), r.fontIDForLayer(b), r.fontIDForLayer(plain)
	if got, want := r.shapeFeatures(idA, a.FontFeatures), []textshape.FontFeature{feature("smcp", 1), feature("liga", 1)}; !reflect.DeepEqual(got, want) {
		t.Errorf("descriptor face, property liga 1: %v, want %v", got, want)
	}
	if got, want := r.shapeFeatures(idB, b.FontFeatures), []textshape.FontFeature{feature("smcp", 1), feature("liga", 0)}; !reflect.DeepEqual(got, want) {
		t.Errorf("descriptor face, no property: %v, want %v", got, want)
	}
	if got, want := r.shapeFeatures(idPlain, a.FontFeatures), []textshape.FontFeature{feature("liga", 1)}; !reflect.DeepEqual(got, want) {
		t.Errorf("a's features shaped with the plain face: %v, want %v", got, want)
	}
}

// TestPaintLayer_FontFeatureSettingsIsItsOwnInput: the font-feature-settings
// property (Blink's FontDescription::FeatureSettings(), which decides which
// descriptor features survive) is an input of its own, not a view into the
// shaping list — rewriting the shaping list leaves it untouched.
func TestPaintLayer_FontFeatureSettingsIsItsOwnInput(t *testing.T) {
	layer := layerWithStyle(map[string]string{
		"font-feature-settings": `"liga" 0`,
		"font-variant-caps":     "small-caps",
	})
	want := []textshape.FontFeature{feature("liga", 0)}
	if !reflect.DeepEqual(layer.FontFeatureSettings, want) {
		t.Fatalf("FontFeatureSettings = %v, want %v", layer.FontFeatureSettings, want)
	}
	for i := range layer.FontFeatures {
		layer.FontFeatures[i] = feature("zzzz", 9)
	}
	if !reflect.DeepEqual(layer.FontFeatureSettings, want) {
		t.Errorf("FontFeatureSettings aliases FontFeatures: now %v", layer.FontFeatureSettings)
	}
}
