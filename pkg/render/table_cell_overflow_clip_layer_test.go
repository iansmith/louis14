package render_test

import (
	"testing"

	"louis14/pkg/render"
)

// A table part with non-visible overflow has a paint layer of its own (an
// overflow-clip layer, LayoutBox::LayerTypeRequired, layout/layout_box.cc:
// 556-568 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a collapsed-border
// cell's background is clipped to inside its borders
// (TableCellBackgroundClipper, paint/table_painters.cc:382-413 @ same SHA):
// none of the cell's red shows under the table's transparent 10px border.
func TestCollapsedCellWithOverflowClipLayerClipsItsBackground(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<table style="border-collapse:collapse;border:10px solid transparent">` +
		`<tr><td style="overflow:hidden;background:red;padding:0;width:50px;height:50px"></td></tr></table></body></html>`
	img := render.RenderPage(t, src, 100, 100)
	// The cell's half of the 10px edge is 5..10 from the table edge.
	for _, p := range [][2]int{{7, 30}, {30, 7}, {62, 30}, {30, 62}} {
		if isRed(img.RGBAAt(p[0], p[1])) {
			t.Errorf("red at (%d,%d), under the collapsed border", p[0], p[1])
		}
	}
	if !isRed(img.RGBAAt(30, 30)) {
		t.Error("no red at (30,30), inside the cell")
	}
}
