package render

import (
	"image"
	"image/color"
	"testing"

	"mazarin/textshape"

	"louis14/pkg/css"
)

// TestGradientStopResolvesCurrentColor (LOU-550): a gradient color stop of
// currentcolor — bare or inside a color function — resolves at paint time
// against the element's (visited-dependent) computed color, instead of
// failing to parse and dropping the whole gradient. Mirrors Blink's
// ResolveStopColor (core/css/css_gradient_value.cc:394-409 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), called from
// CSSGradientValue::AddStops (:656-694 @ same SHA).
func TestGradientStopResolvesCurrentColor(t *testing.T) {
	style := css.NewStyle()
	style.Set("color", "rgb(0, 128, 0)")
	green := color.RGBA{0, 128, 0, 255}

	for _, tc := range []struct {
		name, grad string
	}{
		{"linear", "linear-gradient(currentcolor, currentColor)"},
		{"repeating linear", "repeating-linear-gradient(currentcolor 0 10px, currentcolor 10px 20px)"},
		{"radial", "radial-gradient(currentColor, currentcolor)"},
		{"conic", "conic-gradient(currentcolor, CurrentColor)"},
		{"color-mix", "linear-gradient(color-mix(in srgb, currentcolor 100%, blue), currentcolor)"},
	} {
		target := image.NewRGBA(image.Rect(0, 0, 100, 100))
		r := &Renderer{dc: textshape.NewDrawContextForImage(target, newProvider()), target: target}
		r.drawGradient(tc.grad, 0, 0, 100, 100, style)
		if got := target.RGBAAt(50, 50); !approxColor(got, green, 2) {
			t.Errorf("%s: pixel (50,50) = %v, want currentcolor %v", tc.name, got, green)
		}
	}
}
