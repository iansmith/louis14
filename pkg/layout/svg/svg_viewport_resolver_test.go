package svg

import (
	"testing"

	"louis14/pkg/geometry"
)

// Percentage lengths resolve against the nearest viewport's viewBox size
// when it has a non-empty one, not its CSS size: SVGViewportResolver::
// ResolveViewport (core/svg/svg_length_functions.cc:44-72 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 50×100 root with viewBox
// 0 0 200 400 gives a 100%×100% rect 200×400 user units.
func TestBuildSVGRoot_PercentagesResolveAgainstViewBox(t *testing.T) {
	rect := &fakeAdapter{tag: "rect", attrs: map[string]string{"width": "100%", "height": "100%"}}
	inner := &fakeAdapter{tag: "rect", attrs: map[string]string{"width": "50%", "height": "50%"}}
	nested := &fakeAdapter{tag: "svg", attrs: map[string]string{"width": "10", "height": "10", "viewBox": "0 0 40 20"}, children: []*fakeAdapter{inner}}
	svgElt := &fakeAdapter{tag: "svg", attrs: map[string]string{"viewBox": "0 0 200 400"}, children: []*fakeAdapter{rect, nested}}

	root := BuildSVGRoot(svgElt, geometry.SizeF{Width: 50, Height: 100}, nil)

	if len(root.Children) != 2 {
		t.Fatalf("root.Children len = %d, want 2", len(root.Children))
	}
	if got := root.Children[0].(*SVGShape).FillBoundingBox; got.Width() != 200 || got.Height() != 400 {
		t.Errorf("100%% rect = %vx%v, want 200x400 (the root viewBox size)", got.Width(), got.Height())
	}
	vc := root.Children[1].(*SVGViewportContainer)
	if got := vc.Children[0].(*SVGShape).FillBoundingBox; got.Width() != 20 || got.Height() != 10 {
		t.Errorf("nested 50%% rect = %vx%v, want 20x10 (the nested viewBox size)", got.Width(), got.Height())
	}
}
