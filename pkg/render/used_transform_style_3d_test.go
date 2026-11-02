package render_test

import (
	"testing"
)

// TestUsedTransformStyle3D_GroupingPropertyFlattens: a grouping property on a
// `transform-style: preserve-3d` box overrides the used value to flat, so its
// children are flattened into its plane instead of sharing a 3D rendering
// context with it. Mirrors Blink's ComputedStyle::UsedTransformStyle3D /
// HasGroupingPropertyForUsedTransformStyle3D
// (core/style/computed_style.h:2131-2147, :2264-2267 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). WPT
// css-transforms/transform3d-preserve3d-008/-009/-013.
//
// The parent is rotateX(45deg) about its top edge and the 200px-tall child
// rotateX(-45deg) about its top edge. Flattened at each level the child is
// 200·cos²45° = 100px tall on screen; composed in one 3D context the two
// rotations cancel and it stays 200px tall, though an overflow clip still cuts
// it at the parent's projected 200·cos45° ≈ 141px. The probe at y=120 is
// lime only in the 3D-context case.
func TestUsedTransformStyle3D_GroupingPropertyFlattens(t *testing.T) {
	const probeY = 120
	for _, tc := range []struct {
		name     string
		grouping string
		wantLime bool
	}{
		{"no grouping property keeps the 3D context", "", true},
		{"overflow:hidden", "overflow: hidden;", false},
		{"overflow:auto", "overflow: auto;", false},
		{"overflow:clip", "overflow: clip;", false},
		{"isolation:isolate", "isolation: isolate;", false},
		{"mix-blend-mode:multiply", "mix-blend-mode: multiply;", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `<!DOCTYPE html><html><body style="margin:0">` +
				`<div style="transform: rotateX(45deg); transform-origin: top; transform-style: preserve-3d; ` + tc.grouping + `">` +
				`<div style="transform: rotateX(-45deg); transform-origin: top; height: 200px; width: 100px; background: lime"></div>` +
				`</div></body></html>`
			img := renderToImage(t, src, 200, 300)
			c := img.RGBAAt(50, probeY)
			isLime := c.R < 40 && c.G > 200 && c.B < 40
			if isLime != tc.wantLime {
				t.Errorf("pixel (50,%d) = %v; lime=%v, want lime=%v", probeY, c, isLime, tc.wantLime)
			}
		})
	}
}

// TestPreserve3D_EdgeOnParentComposesWithChild: a rotateX(90deg)
// preserve-3d parent is exactly edge-on (gfx::SinCosDegrees), yet its
// rotateX(90deg) scale(2) child composes with it to scale(2, -2) and must
// paint. WPT css-transforms/transform3d-preserve3d-014.
func TestPreserve3D_EdgeOnParentComposesWithChild(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="transform: rotatex(90deg); transform-style: preserve-3d; margin: 50px">` +
		`<div style="transform: rotatex(90deg) scale(2); width: 50px; height: 50px; background: lime"></div>` +
		`</div></body></html>`
	img := renderToImage(t, src, 200, 200)
	c := img.RGBAAt(75, 75)
	if !(c.R < 40 && c.G > 200 && c.B < 40) {
		t.Errorf("pixel (75,75) = %v, want lime", c)
	}
}
