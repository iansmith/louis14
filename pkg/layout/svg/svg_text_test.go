package svg

import (
	"testing"

	"louis14/pkg/geometry"
)

// TestBuildSVGTree_Text confirms BuildSVGTree routes `<text>` to
// SVGText (not the generic SVGContainer fallback) and resolves its
// x/y attributes plus direct text-node content. Mirrors the LOU-345
// confirmed-root-cause scenario: `<text x="20" y="20">Text to
// select</text>` inside an `<svg>` — before this ticket, BuildSVGTree
// had no `case "text":` at all, so the tag fell through to
// NewSVGContainer, whose SVGChildren() recursion never sees the text
// node (SVGChildren filters to ElementNode only), producing a
// childless, contentless container.
func TestBuildSVGTree_Text(t *testing.T) {
	text := &fakeAdapter{
		tag:   "text",
		attrs: map[string]string{"x": "20", "y": "20"},
		text:  "Text to select",
	}
	lengthCtx := NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 60})
	node := BuildSVGTree(text, lengthCtx, nil)

	svgText, ok := node.(*SVGText)
	if !ok {
		t.Fatalf("BuildSVGTree(<text>) type = %T, want *SVGText", node)
	}
	if svgText.X != 20 {
		t.Errorf("X = %v, want 20", svgText.X)
	}
	if svgText.Y != 20 {
		t.Errorf("Y = %v, want 20", svgText.Y)
	}
	if svgText.Text != "Text to select" {
		t.Errorf("Text = %q, want %q", svgText.Text, "Text to select")
	}
	if svgText.TagName != "text" {
		t.Errorf("TagName = %q, want %q", svgText.TagName, "text")
	}
}

// TestBuildSVGTree_Text_DefaultsXY confirms missing x/y attributes
// resolve to 0 per SVG 1.1 §10.5 (the `<text>` x/y default value).
func TestBuildSVGTree_Text_DefaultsXY(t *testing.T) {
	text := &fakeAdapter{tag: "text", text: "hi"}
	lengthCtx := NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 60})
	node := BuildSVGTree(text, lengthCtx, nil)

	svgText, ok := node.(*SVGText)
	if !ok {
		t.Fatalf("BuildSVGTree(<text>) type = %T, want *SVGText", node)
	}
	if svgText.X != 0 || svgText.Y != 0 {
		t.Errorf("X,Y = %v,%v, want 0,0", svgText.X, svgText.Y)
	}
}

// TestBuildSVGRoot_Text_ViaResourceAwareDispatch confirms
// buildSVGTreeWithResources (the resource-aware variant BuildSVGRoot
// actually calls) also dispatches `<text>` to SVGText — the ticket's
// root-cause investigation found BuildSVGTree AND
// buildSVGTreeWithResources both need the new case (they're separate
// dispatch switches).
func TestBuildSVGRoot_Text_ViaResourceAwareDispatch(t *testing.T) {
	text := &fakeAdapter{
		tag:   "text",
		attrs: map[string]string{"x": "20", "y": "20"},
		text:  "Text to select",
	}
	svgElt := &fakeAdapter{tag: "svg", children: []*fakeAdapter{text}}
	root := BuildSVGRoot(svgElt, geometry.SizeF{Width: 100, Height: 60}, nil)
	if len(root.Children) != 1 {
		t.Fatalf("root.Children len = %d, want 1", len(root.Children))
	}
	svgText, ok := root.Children[0].(*SVGText)
	if !ok {
		t.Fatalf("root.Children[0] type = %T, want *SVGText", root.Children[0])
	}
	if svgText.X != 20 || svgText.Y != 20 || svgText.Text != "Text to select" {
		t.Errorf("got X=%v Y=%v Text=%q, want X=20 Y=20 Text=%q",
			svgText.X, svgText.Y, svgText.Text, "Text to select")
	}
}
