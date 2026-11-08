package render_test

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// Rendering comparisons shared by this package's tests.

// wantTranslateZMatchesScale2 renders page(perspective, transform) with
// translateZ(50px) under perspective:100px — a 2× scale about the
// perspective origin, w a constant 0.5 — and with scale(2), and wants the
// two to match.
func wantTranslateZMatchesScale2(t *testing.T, page func(perspective, transform string) string, why string) {
	t.Helper()
	wantImagesWithin(t, render.RenderPage(t, page("perspective:100px", "translateZ(50px)"), 220, 220),
		render.RenderPage(t, page("", "scale(2)"), 220, 220), 2, why)
}

// countPixels is how many of img's pixels match.
func countPixels(img *image.RGBA, match func(color.RGBA) bool) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if match(img.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// fixedSVGFetcher serves doc for every external SVG document fetched,
// recording each requested URI in uris when it is non-nil.
func fixedSVGFetcher(doc string, uris *[]string) render.RenderOption {
	return render.WithExternalSVGFetcher(func(uri string) ([]byte, error) {
		if uris != nil {
			*uris = append(*uris, uri)
		}
		return []byte(doc), nil
	})
}
