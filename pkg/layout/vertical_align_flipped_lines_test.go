package layout

import "testing"

// vertical-align: top and bottom align to the line box's line-over and
// line-under edges. In vertical-lr (flipped lines) line-over is the block-end
// side, so top is the right edge and bottom the left; in vertical-rl they are
// block-start and block-end. Blink places top/bottom children by their
// line-over/under metrics (InlineLayoutStateStack::ApplyPendingPositions,
// core/layout/inline/inline_box_state.cc:1185-1209), and line-over is
// block-end only in flipped lines (IsFlippedLinesWritingMode,
// platform/text/writing_mode.h:67-69), both @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. Each item is a 20px-wide
// horizontal-tb inline-block or an Ahem span; want is its offset from its
// 200px-tall container and its size, from Chrome 157 headless.
func TestVerticalAlignTopBottomFlippedLines(t *testing.T) {
	r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}.v{height:200px;margin-bottom:10px}`+
		`.b{display:inline-block;writing-mode:horizontal-tb;width:20px;height:10px}</style>`+
		`<div class=v id=v1 style="writing-mode:vertical-lr">X<span class=b id=a style="height:40px;vertical-align:bottom"></span>`+
		`<span class=b id=b style="vertical-align:top"></span><span class=b id=c style="vertical-align:bottom;margin-left:3px;margin-right:5px"></span></div>`+
		`<div class=v id=v2 style="writing-mode:vertical-rl">X<span class=b id=d style="height:40px;vertical-align:bottom"></span>`+
		`<span class=b id=e style="vertical-align:top"></span><span class=b id=f style="vertical-align:bottom"></span></div>`+
		`<div class=v id=v3 style="writing-mode:vertical-lr">X<span class=b style="height:40px"></span>`+
		`<span id=g style="vertical-align:bottom">Y</span><span id=h style="vertical-align:top">Z</span></div>`)
	for _, c := range []struct {
		id, container string
		dx, dy, w, h  float64
	}{
		{"a", "v1", 0, 10, 20, 40}, {"b", "v1", 8, 50, 20, 10}, {"c", "v1", 3, 60, 20, 10},
		{"d", "v2", 0, 10, 20, 40}, {"e", "v2", 0, 50, 20, 10}, {"f", "v2", 0, 60, 20, 10},
		{"g", "v3", 0, 50, 10, 10}, {"h", "v3", 10, 60, 10, 10},
	} {
		b, p := findBoxByID(r, c.id), findBoxByID(r, c.container)
		if b == nil || p == nil {
			t.Fatalf("no #%s or #%s box", c.id, c.container)
		}
		if dx, dy := b.X-p.X, b.Y-p.Y; dx != c.dx || dy != c.dy || b.Width != c.w || b.Height != c.h {
			t.Errorf("#%s at (%v, %v) %vx%v, want (%v, %v) %vx%v", c.id, dx, dy, b.Width, b.Height, c.dx, c.dy, c.w, c.h)
		}
	}
}
