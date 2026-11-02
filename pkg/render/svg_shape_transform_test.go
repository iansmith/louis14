package render_test

import "testing"

// TestSVGShape_TransformAttribute: every SVG graphics element is
// transformable, shapes included — the `transform` attribute of a <rect>
// rotates it (SVGGraphicsElement's transform, read into the shape's local
// transform by LayoutSVGShape::UpdateLocalTransform, core/layout/svg/
// layout_svg_shape.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// rotate(45, 150, 150) turns the 100px square about its center into a
// diamond: its top vertex reaches y≈79 and its corners are cut.
func TestSVGShape_TransformAttribute(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><svg width="300" height="300" style="position:absolute;top:0;left:0">` +
		`<rect x="100" y="100" width="100" height="100" transform="rotate(45, 150, 150)" style="fill:lime"></rect>` +
		`</svg></body></html>`
	img := renderToImage(t, src, 300, 300)
	if c := img.RGBAAt(150, 90); !isGreen(c) {
		t.Errorf("pixel (150,90) = %v, want the rotated square's top vertex", c)
	}
	if c := img.RGBAAt(105, 105); isGreen(c) {
		t.Errorf("pixel (105,105) = %v: the square's corner was not rotated away", c)
	}
}

// TestSVGShape_TransformAttributeInClipPath: a clipPath's single shape
// clips with its transform, on the AsPath fast path as on the content
// path (LayoutSVGResourceClipper::AsPath maps the child's path through its
// LocalSVGTransform).
func TestSVGShape_TransformAttributeInClipPath(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><svg width="300" height="300" style="position:absolute;top:0;left:0">` +
		`<clipPath id="c"><rect x="100" y="100" width="100" height="100" transform="rotate(45, 150, 150)"></rect></clipPath>` +
		`<rect x="50" y="50" width="200" height="200" clip-path="url(#c)" style="fill:lime"></rect>` +
		`</svg></body></html>`
	img := renderToImage(t, src, 300, 300)
	if c := img.RGBAAt(150, 90); !isGreen(c) {
		t.Errorf("pixel (150,90) = %v, want inside the rotated clip", c)
	}
	if c := img.RGBAAt(105, 105); isGreen(c) {
		t.Errorf("pixel (105,105) = %v: clipped by the unrotated rect", c)
	}
}
