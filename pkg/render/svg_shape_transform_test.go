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

// TestSVGShape_CSSTransformReplacesAttribute: the `transform` attribute is
// a presentation attribute for the CSS `transform` property
// (SVGElement::CollectStyleForPresentationAttribute maps it to
// CSSPropertyID::kTransform @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so
// an author CSS transform replaces it rather than composing with it.
func TestSVGShape_CSSTransformReplacesAttribute(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><svg width="300" height="300" style="position:absolute;top:0;left:0">` +
		`<rect x="100" y="100" width="50" height="50" transform="translate(50,0)" style="fill:lime;transform:translateY(0px)"></rect>` +
		`<g transform="translate(50,0)" style="transform:translateY(0px)"><rect x="100" y="200" width="50" height="50" style="fill:lime"></rect></g>` +
		`</svg></body></html>`
	img := renderToImage(t, src, 300, 300)
	for _, p := range [][2]int{{110, 110}, {110, 210}} {
		if c := img.RGBAAt(p[0], p[1]); !isGreen(c) {
			t.Errorf("pixel %v = %v: the attribute transform composed with the CSS one", p, c)
		}
	}
}

// TestSVGShape_TransformInObjectBoundingBoxClipPath: on the clipPath fast
// path a child's transform applies in clipPathUnits space, before the
// objectBoundingBox mapping (LayoutSVGResourceClipper::AsPath composes the
// units transform, the clipPath's transform and the child's local
// transform in that order). translate(0.5, 0) of a half-width rect clips
// to the right half of the target.
func TestSVGShape_TransformInObjectBoundingBoxClipPath(t *testing.T) {
	src := `<!DOCTYPE html><html><body style="margin:0"><svg width="300" height="300" style="position:absolute;top:0;left:0">` +
		`<clipPath id="c" clipPathUnits="objectBoundingBox"><rect x="0" y="0" width="0.5" height="1" transform="translate(0.5, 0)"></rect></clipPath>` +
		`<rect x="100" y="100" width="100" height="100" clip-path="url(#c)" style="fill:lime"></rect>` +
		`</svg></body></html>`
	img := renderToImage(t, src, 300, 300)
	if c := img.RGBAAt(175, 150); !isGreen(c) {
		t.Errorf("pixel (175,150) = %v, want inside the right-half clip", c)
	}
	if c := img.RGBAAt(125, 150); isGreen(c) {
		t.Errorf("pixel (125,150) = %v, want clipped away", c)
	}
}
