package graphics

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/geometry"
)

// A fractional rect edge covers its pixel by area: a rect from x=0.5 fills
// half of pixel 0 (alpha 128) and all of pixel 1.
func TestFillRect_AreaCoverage(t *testing.T) {
	dst := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	FillRect(dst, geometry.NewRectF(0.5, 0, 1.5, 1), color.NRGBA{R: 255, A: 255})
	if got := dst.NRGBAAt(0, 0); got.R != 255 || got.A != 128 {
		t.Errorf("half-covered pixel = %v, want R255 A128", got)
	}
	if got := dst.NRGBAAt(1, 0); got != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("covered pixel = %v, want opaque red", got)
	}
	if got := dst.NRGBAAt(2, 0); got.A != 0 {
		t.Errorf("uncovered pixel = %v, want transparent", got)
	}
}

// Source-over: a half-transparent fill over opaque blue mixes.
func TestFillRect_SourceOver(t *testing.T) {
	dst := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	FillRect(dst, geometry.NewRectF(0, 0, 1, 1), color.NRGBA{B: 255, A: 255})
	FillRect(dst, geometry.NewRectF(0, 0, 1, 1), color.NRGBA{R: 255, A: 128})
	if got := dst.NRGBAAt(0, 0); got.A != 255 || got.R != 128 || got.B != 127 {
		t.Errorf("pixel = %v, want R128 B127 A255", got)
	}
}

// DrawImageRect copies a 1:1 source rect exactly and scales one 2× with
// nearest sampling unblended at pixel centres.
func TestDrawImageRect(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	dst := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	DrawImageRect(dst, src, geometry.NewRectF(0, 0, 2, 1), geometry.NewRectF(0, 0, 4, 1), false)
	for x, want := range []color.NRGBA{{R: 255, A: 255}, {R: 255, A: 255}, {G: 255, A: 255}, {G: 255, A: 255}} {
		if got := dst.NRGBAAt(x, 0); got != want {
			t.Errorf("pixel %d = %v, want %v", x, got, want)
		}
	}
	one := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	DrawImageRect(one, src, geometry.NewRectF(0, 0, 2, 1), geometry.NewRectF(1, 0, 2, 1), true)
	if got := one.NRGBAAt(1, 0); got != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("1:1 smoothed copy pixel = %v, want exact red", got)
	}
}
