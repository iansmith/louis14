package render

import (
	"image"
	"image/color"
	"testing"

	"mazarin/textshape"

	"louis14/pkg/css"
)

// TestGradientMathUsesElementConversionData (LOU-454): font-relative units in
// gradient stop positions and radial centres resolve against the element's
// font (Ahem 20px: ch = 20px), not a font-size of 0. Blink resolves gradient
// lengths through the element's CSSToLengthConversionData
// (core/css/css_gradient_value.cc AddStops / ComputeEndPoint with
// conversion_data, core/css/css_to_length_conversion_data.cc:117-126 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestGradientMathUsesElementConversionData(t *testing.T) {
	style := css.NewStyle()
	style.Set("font-size", "20px")
	style.SetZeroWidth(20)
	red := color.RGBA{255, 0, 0, 255}

	for _, tc := range []struct {
		name, grad string
		x, y       int
	}{
		// Hard stop at calc(2ch) = 40px: x=20 is red, not blue.
		{"linear stop", "linear-gradient(to right, red calc(2ch), blue calc(2ch))", 20, 50},
		// Circle centre at calc(10% + 2ch) = 50px: (50,50) is inside.
		{"radial centre", "radial-gradient(circle 5px at calc(10% + 2ch) 50%, red 99%, blue 100%)", 50, 50},
	} {
		target := image.NewRGBA(image.Rect(0, 0, 100, 100))
		r := &Renderer{dc: textshape.NewDrawContextForImage(target, newProvider()), target: target}
		r.drawGradient(tc.grad, 0, 0, 100, 100, style)
		if got := target.RGBAAt(tc.x, tc.y); !approxColor(got, red, 8) {
			t.Errorf("%s: pixel (%d,%d) = %v, want red", tc.name, tc.x, tc.y, got)
		}
	}
}
