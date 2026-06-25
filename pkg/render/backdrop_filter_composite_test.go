package render

import (
	"image"
	"image/color"
	"testing"

	"mazarin/textshape"

	"louis14/pkg/css"
)

// TestSetColorTranslucentFillOverOpaque is the RED reproduction for the
// backdrop-filter isolation cluster bug: a translucent CSS background painted
// over already-painted opaque content (the inverted backdrop) must composite
// with Porter-Duff source-over. mazarin's DrawContext stores fill colours as
// color.RGBA, whose RGBA() contract is alpha-PREMULTIPLIED. If setColor hands
// over a non-premultiplied color.RGBA (R,G,B at full value with A<255), the
// rasterizer composites src_rgb + dst_rgb*(1-a) which overflows uint8 and
// wraps, producing garbage (e.g. yellow #ff08 over inverted-green renders as
// dark purple instead of the expected orange).
//
// Expected: #ff08 (rgba(255,255,0,0.533)) over inverted green (255,127,255)
// = (255, 195, 119) per source-over. Blink composites the element's own
// background after ApplyBackdropFilter via SkBlendMode::kSrcOver on
// non-premultiplied paint colours (filter_painter.cc / GraphicsContext, @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func TestSetColorTranslucentFillOverOpaque(t *testing.T) {
	target := image.NewRGBA(image.Rect(0, 0, 4, 4))
	// Pre-fill with opaque inverted green (255,127,255,255).
	for i := 0; i+3 < len(target.Pix); i += 4 {
		target.Pix[i+0] = 255
		target.Pix[i+1] = 127
		target.Pix[i+2] = 255
		target.Pix[i+3] = 255
	}
	dc := textshape.NewDrawContextForImage(target, newProvider())
	r := &Renderer{dc: dc, target: target}

	// Paint #ff08 over the whole region.
	r.setColor(css.Color{R: 255, G: 255, B: 0, A: float64(0x88) / 255.0})
	dc.DrawRectangle(0, 0, 4, 4)
	dc.Fill()

	got := target.RGBAAt(2, 2)
	want := color.RGBA{R: 255, G: 195, B: 119, A: 255}
	if !approxColor(got, want, 2) {
		t.Fatalf("translucent fill over opaque: got %v, want ~%v (Porter-Duff source-over)", got, want)
	}
}

func approxColor(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}
