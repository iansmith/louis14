package visualtest

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestSVGText_FontFaceFeatureSettingsDescriptor (LOU-465): SVG <text> shapes
// with its face's @font-face font-feature-settings descriptor, exactly as
// HTML text does — Blink resolves the descriptor into the face's
// FontPlatformData::ResolvedFeatures and every ShapeRange call prepends
// them (platform/fonts/shaping/harfbuzz_shaper.cc:327-340 @
// 5992657f556bcd9d207a4aca187488462f027cab), whichever painter asks.
// Lato kerns AV/AW/AY/To and ligates fi, so the descriptor face (lato-ffs,
// 'kern' 0, 'liga' 0) must draw what the plain face (lato) draws under the
// same font-feature-settings property, and something different from the
// plain face without it.
func TestSVGText_FontFaceFeatureSettingsDescriptor(t *testing.T) {
	const features = `'kern' 0, 'liga' 0`
	doc := func(family, style string) string {
		return `<!DOCTYPE html><style>
  @font-face { font-family: lato; src: url(/fonts/Lato-Bold.ttf); }
  @font-face { font-family: lato-ffs; src: url(/fonts/Lato-Bold.ttf);
    font-feature-settings: ` + features + `; }
  body { margin: 0; }
</style><svg width="400" height="60"><text x="10" y="45" style="font-size: 36px; font-family: ` + family + `; ` + style + `">AVAWAY To fi</text></svg>`
	}
	base := filepath.Join("testdata", "wpt-css3", "css-fonts", "font-feature-settings-descriptor-01.html")
	render := func(name, html string) *image.RGBA {
		out := filepath.Join(t.TempDir(), name+".png")
		if err := RenderHTMLToFileWithBase(html, out, 800, 600, base); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		rgba := image.NewRGBA(img.Bounds())
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
		return rgba
	}
	descriptor := render("descriptor", doc("lato-ffs", ""))
	property := render("property", doc("lato", "font-feature-settings: "+features))
	plain := render("plain", doc("lato", ""))

	if bytes.Equal(property.Pix, plain.Pix) {
		t.Fatal("sanity: the font-feature-settings property does not change SVG text; the test cannot observe features")
	}
	if !bytes.Equal(descriptor.Pix, property.Pix) {
		t.Error("SVG <text> in the descriptor face does not render like the property: the @font-face font-feature-settings descriptor is ignored")
	}
}
