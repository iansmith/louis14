package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// RenderPage registers the page's @font-face rules, as a document's
// StyleEngine::AddUserFontFaceRules does (and louis14's production and WPT
// paths do): a face declared in the page draws its text. Ahem's "X" is a
// solid em square, so its top-left pixel is filled; a fallback face's "X"
// leaves it clear.
func TestRenderPage_RegistersFontFaces(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><html><head><style>
@font-face { font-family: PageFace; src: url(Ahem.ttf) }
body { margin: 0 }
div { font: 20px/20px PageFace; color: black }
</style></head><body><div>X</div></body></html>`, 40, 30)
	assertPixel(t, img, 1, 1, black, "Ahem's em square, drawn in the page's @font-face")
}
