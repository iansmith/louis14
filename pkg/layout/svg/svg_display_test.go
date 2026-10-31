package svg

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// hiddenStyleResolver returns a StyleResolver that gives every adapter in
// hidden a `display: none` style and every other adapter a default style.
func hiddenStyleResolver(hidden ...*fakeAdapter) StyleResolver {
	return func(elt ElementAdapter) *css.Style {
		s := css.NewStyle()
		for _, h := range hidden {
			if elt == ElementAdapter(h) {
				s.Set("display", "none")
			}
		}
		return s
	}
}

// TestBuildSVGRoot_DisplayNoneChildNotBuilt pins Blink's
// SVGElement::LayoutObjectIsNeeded (core/svg/svg_element.cc:873-875 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), which defers to
// Element::LayoutObjectIsNeeded (core/dom/element.cc:4211-4214 @ same SHA):
// an SVG element whose computed display is none gets no layout object, so
// it is not painted, while its displayed siblings still are
// (selectors/is-default-ns-003 hides a <text> with `display:none`).
func TestBuildSVGRoot_DisplayNoneChildNotBuilt(t *testing.T) {
	text := &fakeAdapter{tag: "text", text: "t"}
	rect := &fakeAdapter{tag: "rect"}
	g := &fakeAdapter{tag: "g", children: []*fakeAdapter{text, rect}}
	topRect := &fakeAdapter{tag: "rect"}
	svgElt := &fakeAdapter{tag: "svg", children: []*fakeAdapter{g, topRect}}

	root := BuildSVGRoot(svgElt, geometry.SizeF{Width: 100, Height: 100}, hiddenStyleResolver(text, topRect))

	if len(root.Children) != 1 {
		t.Fatalf("root.Children len = %d, want 1 (the <g>; the display:none <rect> is not built)", len(root.Children))
	}
	gNode, ok := root.Children[0].(*SVGContainer)
	if !ok {
		t.Fatalf("root.Children[0] type = %T, want *SVGContainer", root.Children[0])
	}
	if len(gNode.Children) != 1 {
		t.Fatalf("g.Children len = %d, want 1 (the <rect>; the display:none <text> is not built)", len(gNode.Children))
	}
	if shape, ok := gNode.Children[0].(*SVGShape); !ok || shape.TagName != "rect" {
		t.Errorf("g.Children[0] = %T, want the *SVGShape for <rect>", gNode.Children[0])
	}
}

// TestBuildSVGRoot_DisplayNoneGroupKeepsResources pins Blink's
// SVGGElement::CreateLayoutObject / LayoutObjectIsNeeded
// (core/svg/svg_g_element.cc:32-50 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): a display:none <g> becomes a
// LayoutSVGHiddenContainer, so it paints nothing but the resources inside
// it can still be referenced. <defs> (SVGDefsElement::CreateLayoutObject,
// core/svg/svg_defs_element.cc:31-33 @ same SHA) keeps registering too.
func TestBuildSVGRoot_DisplayNoneGroupKeepsResources(t *testing.T) {
	grad := &fakeAdapter{tag: "lineargradient", attrs: map[string]string{"id": "gr"}}
	rect := &fakeAdapter{tag: "rect"}
	g := &fakeAdapter{tag: "g", children: []*fakeAdapter{grad, rect}}
	defsGrad := &fakeAdapter{tag: "lineargradient", attrs: map[string]string{"id": "dg"}}
	defs := &fakeAdapter{tag: "defs", children: []*fakeAdapter{defsGrad}}
	svgElt := &fakeAdapter{tag: "svg", children: []*fakeAdapter{g, defs}}

	root := BuildSVGRoot(svgElt, geometry.SizeF{Width: 100, Height: 100}, hiddenStyleResolver(g))

	if len(root.Children) != 0 {
		t.Errorf("root.Children len = %d, want 0 (display:none <g> and <defs> paint nothing)", len(root.Children))
	}
	if _, ok := root.Resources.LookupAsPaintServer("gr"); !ok {
		t.Error(`gradient "gr" inside the display:none <g> is not registered`)
	}
	if _, ok := root.Resources.LookupAsPaintServer("dg"); !ok {
		t.Error(`gradient "dg" inside <defs> is not registered`)
	}
}

// TestBuildSVGTree_DisplayNoneReturnsNil pins the same
// SVGElement::LayoutObjectIsNeeded gate on BuildSVGTree, the builder for
// clipPath / mask / pattern content (core/svg/svg_element.cc:873-875 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). A nil styleResolver, as in
// the geometry-only tests, means no style and so the element is built.
func TestBuildSVGTree_DisplayNoneReturnsNil(t *testing.T) {
	lengthCtx := NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 100})
	rect := &fakeAdapter{tag: "rect"}

	if node := BuildSVGTree(rect, lengthCtx, hiddenStyleResolver(rect)); node != nil {
		t.Errorf("BuildSVGTree(display:none <rect>) = %T, want nil", node)
	}
	if node, ok := BuildSVGTree(rect, lengthCtx, nil).(*SVGShape); !ok || node == nil {
		t.Errorf("BuildSVGTree(<rect>, nil resolver) is not an *SVGShape")
	}
}

// TestBuildSVGRoot_DisplayNoneNonGroupDropsResources pins that only <g>
// keeps a hidden container when display:none. Any other container, such as
// a nested <svg> or an <a>, gets no layout object at all
// (SVGElement::LayoutObjectIsNeeded, core/svg/svg_element.cc:873-875 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), so its subtree is never
// built and a gradient inside it never becomes a
// LayoutSVGResourceContainer: it cannot be referenced. The displayed rows
// are the control: the same subtree registers its gradient when shown.
func TestBuildSVGRoot_DisplayNoneNonGroupDropsResources(t *testing.T) {
	for _, tag := range []string{"svg", "a"} {
		for _, hidden := range []bool{false, true} {
			grad := &fakeAdapter{tag: "lineargradient", attrs: map[string]string{"id": "gr"}}
			container := &fakeAdapter{tag: tag, children: []*fakeAdapter{grad}}
			svgElt := &fakeAdapter{tag: "svg", children: []*fakeAdapter{container}}
			resolver := hiddenStyleResolver()
			if hidden {
				resolver = hiddenStyleResolver(container)
			}

			root := BuildSVGRoot(svgElt, geometry.SizeF{Width: 100, Height: 100}, resolver)

			if _, got := root.Resources.LookupAsPaintServer("gr"); got == hidden {
				t.Errorf("<%s> display:none=%v: gradient registered = %v, want %v", tag, hidden, got, !hidden)
			}
		}
	}
}
