package render

import "testing"

// TestOutsetBoxShadowBlurIsNotHollowedByTheBorderBox checks that a blurred
// outset shadow is blurred whole and only then clipped to outside the border
// box, so it stays opaque just outside the box. Mirrors
// BoxPainterBase::PaintNormalBoxShadow (core/paint/box_painter_base.cc:
// 283-384 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the blur is a draw
// looper on the fill of the shadow shape (:348-353, :383), and the border
// box is removed by a geometric ClipOut (:175-197), not cut from the fill
// before blurring.
//
// The shadow extends 20px past the 100px box (spread 20) with a 20px blur
// (sigma 10). 5px left of the box, the shadow edge is 15px away, so the
// blurred coverage is about 93%: red and green stay near 18 on white.
// TestOutsetBoxShadow_NoLeakInsideBorderBox covers the other side of the
// same clip: no shadow inside the box.
func TestOutsetBoxShadowBlurIsNotHollowedByTheBorderBox(t *testing.T) {
	img := RenderPage(t, `<!doctype html><style>body{margin:0;background:white}`+
		`div{position:absolute;left:50px;top:50px;width:100px;height:100px;box-shadow:0 0 20px 20px blue}</style><div></div>`,
		200, 200)
	for _, p := range [][2]int{{45, 100}, {100, 45}, {155, 100}, {100, 155}} {
		c := img.RGBAAt(p[0], p[1])
		if c.R > 30 || c.G > 30 {
			t.Errorf("pixel %v = %v, want near-opaque blue (R,G <= 30)", p, c)
		}
	}
}
