package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// stroke-dashoffset starts a dashed stroke that far into its dash pattern:
// SVGLayoutSupport::ApplyStrokeStyleToStrokeData passes it to
// StrokeData::SetLineDash as the dash effect's phase
// (core/layout/svg/svg_layout_support.cc:293-320,
// platform/geometry/stroke_data.cc:35-50 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Dashed 10 on, 10 off at
// offset 5, a stroke starting at (0,10) and running right is drawn at x
// 0-5, 15-25, 35-45 and 55-60: a line's whole length, or a rect's top
// edge (its path starts at its top-left corner).
func TestSVGStrokeDashOffset(t *testing.T) {
	const dash = `stroke-width="4" stroke-dasharray="10 10" stroke-dashoffset="5"`
	for _, tc := range []struct{ name, shape string }{
		{"color", `<line x1="0" y1="10" x2="60" y2="10" stroke="black" ` + dash + `/>`},
		// A paint server goes through the shader stroke path
		// (strokePathWithShader).
		{"paint server", `<defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" x2="80">` +
			`<stop offset="0" stop-color="black"/><stop offset="1" stop-color="black"/></linearGradient></defs>` +
			`<rect x="0" y="10" width="60" height="10" fill="none" stroke="url(#g)" ` + dash + `/>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0">`+
				`<svg width="80" height="30" style="display:block">`+tc.shape+`</svg></body></html>`, 80, 30)
			assertPixel(t, img, 2, 10, black, "first dash, cut to x 0-5")
			assertPixel(t, img, 8, 10, white, "first gap, x 5-15")
			assertPixel(t, img, 18, 10, black, "second dash, x 15-25")
			assertPixel(t, img, 28, 10, white, "second gap, x 25-35")
		})
	}
}
