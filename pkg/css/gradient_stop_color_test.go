package css

import "testing"

// TestGradientColorStopAcceptsAnyColor (LOU-550): a gradient color stop
// accepts any <color> — currentcolor, color functions that reference it,
// and color functions whose arguments contain spaces — so a background-image
// carrying one survives the parse-time <image> gate. Mirrors Blink's
// ConsumeGradientColorStops, which consumes the stop color with ConsumeColor
// (core/css/properties/css_parsing_utils.cc:3028-3075 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); currentcolor resolves later, at
// paint time (ResolveStopColor, core/css/css_gradient_value.cc:394-409 @
// same SHA).
func TestGradientColorStopAcceptsAnyColor(t *testing.T) {
	for _, grad := range []string{
		"linear-gradient(white, currentColor)",
		"linear-gradient(currentcolor 10px, red 50%)",
		"linear-gradient(rgb(0, 128, 0), red)",
		"linear-gradient(rgb(0, 128, 0) 50%, red)",
		"linear-gradient(color-mix(in srgb, currentcolor 50%, blue), red)",
		"radial-gradient(white, currentColor)",
		"radial-gradient(circle, currentColor 0 50%, rgb(0, 0, 255))",
		"conic-gradient(white, currentColor)",
		"conic-gradient(rgb(0, 128, 0) 0deg, currentcolor 90deg)",
		"repeating-linear-gradient(currentcolor 0 10px, red 10px 20px)",
	} {
		if !isValidImageValue(grad) {
			t.Errorf("isValidImageValue(%q) = false, want true", grad)
		}
	}
	for _, grad := range []string{
		"linear-gradient(notacolor, red)",
		"linear-gradient(currentcolorx, red)",
	} {
		if isValidImageValue(grad) {
			t.Errorf("isValidImageValue(%q) = true, want false", grad)
		}
	}
}
