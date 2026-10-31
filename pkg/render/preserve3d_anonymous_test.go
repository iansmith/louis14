package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// TestPreserve3DAncestorTransforms_AnonymousAncestorsPassThrough: a 3D
// rendering context follows the DOM ancestor chain, so an anonymous box
// without a transform (here the anonymous table generated around a
// display:table-cell, CSS 2.1 §17.2.1) neither joins nor flattens it. Blink
// resets should_flatten_inherited_transform only for non-anonymous objects
// (PaintPropertyTreeBuilder::UpdateTransform,
// core/paint/paint_property_tree_builder.cc:1606-1620 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). WPT
// css-transforms/transform-table-011.html.
//
// A box that stands for a DOM element still flattens even when louis14
// built it as an anonymous LayoutInputNode (no Box.LayoutNode): the
// block-in-inline wrapper keeps the inline's DOM node and style, and in
// Blink that inline is a non-anonymous LayoutInline.
func TestPreserve3DAncestorTransforms_AnonymousAncestorsPassThrough(t *testing.T) {
	style := func(props ...string) *css.Style {
		s := css.NewStyle()
		for i := 0; i+1 < len(props); i += 2 {
			s.Properties[props[i]] = props[i+1]
		}
		return s
	}
	element := func(s *css.Style, parent *layout.Box) *layout.Box {
		return &layout.Box{
			Style: s, Parent: parent,
			Node:       &html.Node{Type: html.ElementNode, TagName: "div"},
			LayoutNode: &layout.LayoutInputNode{},
		}
	}
	for _, tc := range []struct {
		name          string
		mid           func(outer *layout.Box) *layout.Box
		wantAncestors int
	}{
		{"anonymous flat box between preserve-3d boxes", func(outer *layout.Box) *layout.Box {
			return &layout.Box{Style: css.CreateAnonymousStyleWithDisplay(outer.Style, "table"), Parent: outer}
		}, 1},
		{"element flat box between preserve-3d boxes", func(outer *layout.Box) *layout.Box {
			return element(style("display", "block"), outer)
		}, 0},
		{"block-in-inline wrapper keeping its DOM node between preserve-3d boxes", func(outer *layout.Box) *layout.Box {
			span := &html.Node{Type: html.ElementNode, TagName: "span"}
			return &layout.Box{Style: style("display", "block"), Parent: outer, Node: span}
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outer := element(style("transform", "rotateX(90deg)", "transform-style", "preserve-3d"), nil)
			cell := element(style("display", "table-cell", "transform-style", "preserve-3d"), tc.mid(outer))
			inner := element(style("transform", "rotateX(90deg)"), cell)
			if got := preserve3DAncestorTransforms(inner); len(got) != tc.wantAncestors {
				t.Errorf("preserve3DAncestorTransforms = %v, want %d ancestor transform(s)", got, tc.wantAncestors)
			}
		})
	}
}
