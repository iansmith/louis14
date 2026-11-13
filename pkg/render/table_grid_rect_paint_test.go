package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// A table paints its background and border on its grid rect, the table box
// without its captions (TablePainter::PaintBoxDecorationBackground,
// paint/table_painters.cc:421-435 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): nothing of the table's red
// background or border shows behind its 20px-tall top caption.
func TestTablePaintsOnItsGridRect(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<table style="background:red;border:5px solid red;border-spacing:0">` +
		`<caption style="height:20px"></caption>` +
		`<tr><td style="padding:0;width:50px;height:50px"></td></tr></table></body></html>`
	img := render.RenderPage(t, src, 100, 100)
	for y := 0; y < 20; y++ {
		for x := 0; x < 60; x++ {
			if isRed(img.RGBAAt(x, y)) {
				t.Fatalf("red at (%d,%d), inside the caption", x, y)
			}
		}
	}
	if !isRed(img.RGBAAt(30, 50)) {
		t.Error("no red at (30,50), inside the table box")
	}
}
