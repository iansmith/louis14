package render

import (
	"os"
	"reflect"
	"testing"

	"louis14/pkg/css"
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

// TestWithFontFaceFeatures: a layer drawn with a face whose @font-face has
// a font-feature-settings descriptor shapes with the resolved descriptor
// features inserted before its own features, as Blink's ShapeRange inserts
// FontPlatformData::ResolvedFeatures before the font's features
// (platform/fonts/shaping/harfbuzz_shaper.cc:327-340 @
// 5992657f556bcd9d207a4aca187488462f027cab). The layer itself is not
// mutated.
func TestWithFontFaceFeatures(t *testing.T) {
	fr := text.NewFontRegistry()
	src := &css.CSSURIValue{Data: css.URLData{Relative: "Ahem.ttf", Absolute: "Ahem.ttf"}}
	fr.AddUserFontFaceRules([]css.FontFaceRule{
		{Family: "LOU424-FFS", Src: src, Weight: "normal", Style: "normal", FeatureSettings: `"smcp" 1, "liga" 0`},
	}, func(url string) ([]byte, error) { return os.ReadFile("../../fonts/" + url) })
	fc := text.DefaultFontConfig()
	fc.Registry = fr
	r := &Renderer{fonts: fc}

	own := []textshape.FontFeature{feature("liga", 1), feature("c2sc", 1)}
	layer := &PaintLayer{FontFamily: "LOU424-FFS", FontSize: 16,
		FontFeatureSettings: own[:1], FontFeatures: own}
	got := r.withFontFaceFeatures(layer)
	want := []textshape.FontFeature{feature("smcp", 1), feature("liga", 1), feature("c2sc", 1)}
	if !reflect.DeepEqual(got.FontFeatures, want) {
		t.Errorf("FontFeatures = %v, want %v", got.FontFeatures, want)
	}
	if !reflect.DeepEqual(layer.FontFeatures, own) {
		t.Errorf("input layer mutated: FontFeatures = %v", layer.FontFeatures)
	}

	plain := &PaintLayer{FontFamily: "serif", FontSize: 16, FontFeatures: own}
	if got := r.withFontFaceFeatures(plain); got != plain {
		t.Errorf("a face without the descriptor must return the layer unchanged")
	}
}
