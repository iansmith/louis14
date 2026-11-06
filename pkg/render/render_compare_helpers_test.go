package render_test

import (
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
