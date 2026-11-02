package render_test

import "testing"

// TestRenderingContext3D_RowGroupBreaksContext: a 3D rendering context
// follows the DOM ancestor chain, and a non-anonymous element that does
// not preserve 3D resets it (PaintPropertyTreeBuilder's
// rendering_context_id reset, core/paint/paint_property_tree_builder.cc:
// 1630-1651 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The parser's
// implied <tbody> is such an element even though louis14's box tree has
// no box for it, so the inner rotateX(90deg) does not join the outer
// rotateX(90deg)'s context: both flatten edge-on and nothing shows
// (css-transforms/transform-table-007). With preserve-3d on the tbody the
// rotations compose to 180deg and the text shows mirrored
// (transform-table-009).
func TestRenderingContext3D_RowGroupBreaksContext(t *testing.T) {
	page := func(tbody string) string {
		return `<!DOCTYPE html><html><head><style>` +
			`table, tbody, tr, td { margin: 0; padding: 0; border-spacing: 0; }` +
			`div { transform: rotateX(90deg); }` +
			`body > div, table, tr, td` + tbody + ` { transform-style: preserve-3d; }` +
			`.box { width: 50px; height: 50px; background: red; }` +
			`</style></head><body style="margin:0"><div><table><tr><td><div class="box"></div></td></tr></table></div></body></html>`
	}
	red := func(src string) int {
		img := renderToImage(t, src, 200, 200)
		n := 0
		for y := 0; y < 200; y++ {
			for x := 0; x < 200; x++ {
				if isRed(img.RGBAAt(x, y)) {
					n++
				}
			}
		}
		return n
	}
	if n := red(page("")); n != 0 {
		t.Errorf("implicit tbody without preserve-3d: %d red pixels, want none (context broken, edge-on)", n)
	}
	if n := red(page(", tbody")); n == 0 {
		t.Error("tbody with preserve-3d: no red, want the box rotated 180deg and visible")
	}
}
