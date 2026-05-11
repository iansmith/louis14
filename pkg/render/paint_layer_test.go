package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

func styleWithPos(pos string) *css.Style {
	s := css.NewStyle()
	s.Properties["position"] = pos
	return s
}

func styleWithPosZ(pos string, z int) *css.Style {
	s := css.NewStyle()
	s.Properties["position"] = pos
	s.Properties["z-index"] = string(rune('0' + z)) // single digit only
	return s
}

func styleWithOverflow(overflow string) *css.Style {
	s := css.NewStyle()
	s.Properties["overflow"] = overflow
	return s
}

func TestBuildPaintTree_Nil(t *testing.T) {
	layer := BuildPaintTree(nil)
	if layer != nil {
		t.Fatal("expected nil for nil root")
	}
}

func TestBuildPaintTree_AllFlow(t *testing.T) {
	// Root with two non-positioned styled children.
	root := &layout.Box{Style: css.NewStyle()}
	root.Children = []*layout.Box{
		{Style: css.NewStyle(), Parent: root},
		{Style: css.NewStyle(), Parent: root},
	}

	layer := BuildPaintTree(root)
	if len(layer.FlowChildren) != 2 {
		t.Fatalf("expected 2 flow children, got %d", len(layer.FlowChildren))
	}
	if len(layer.NegativeZ) != 0 || len(layer.AutoZero) != 0 || len(layer.PositiveZ) != 0 {
		t.Fatal("expected no z-list children for all-flow tree")
	}
}

func TestBuildPaintTree_PositionedAutoZ(t *testing.T) {
	// Positioned child with z-index:auto escapes to ancestor SC's AutoZero.
	root := &layout.Box{Style: css.NewStyle()}
	child := &layout.Box{
		Style:    styleWithPos("relative"),
		Position: css.PositionRelative,
		Parent:   root,
	}
	root.Children = []*layout.Box{child}

	layer := BuildPaintTree(root)
	if len(layer.FlowChildren) != 0 {
		t.Fatalf("expected 0 flow children, got %d", len(layer.FlowChildren))
	}
	if len(layer.AutoZero) != 1 {
		t.Fatalf("expected 1 AutoZero child, got %d", len(layer.AutoZero))
	}
	if layer.AutoZero[0].Box != child {
		t.Fatal("AutoZero[0] should be the positioned child")
	}
}

func TestBuildPaintTree_StackingContextZIndex(t *testing.T) {
	// Children with explicit z-index create stacking contexts and sort correctly.
	root := &layout.Box{Style: css.NewStyle()}

	negChild := &layout.Box{
		Style:    styleWithPosZ("absolute", 0), // we'll override
		Position: css.PositionAbsolute,
		ZIndex:   -1,
		Parent:   root,
	}
	negChild.Style.Properties["z-index"] = "-1"

	posChild := &layout.Box{
		Style:    styleWithPosZ("relative", 5),
		Position: css.PositionRelative,
		ZIndex:   5,
		Parent:   root,
	}

	zeroChild := &layout.Box{
		Style:    styleWithPosZ("absolute", 0),
		Position: css.PositionAbsolute,
		ZIndex:   0,
		Parent:   root,
	}

	root.Children = []*layout.Box{posChild, negChild, zeroChild}

	layer := BuildPaintTree(root)
	if len(layer.NegativeZ) != 1 {
		t.Fatalf("expected 1 NegativeZ, got %d", len(layer.NegativeZ))
	}
	if len(layer.PositiveZ) != 1 {
		t.Fatalf("expected 1 PositiveZ, got %d", len(layer.PositiveZ))
	}
	if len(layer.AutoZero) != 1 {
		t.Fatalf("expected 1 AutoZero, got %d", len(layer.AutoZero))
	}
	if layer.NegativeZ[0].ZIndex != -1 {
		t.Fatalf("NegativeZ[0].ZIndex = %d, want -1", layer.NegativeZ[0].ZIndex)
	}
	if layer.PositiveZ[0].ZIndex != 5 {
		t.Fatalf("PositiveZ[0].ZIndex = %d, want 5", layer.PositiveZ[0].ZIndex)
	}
}

func TestBuildPaintTree_UnstyedSkipped(t *testing.T) {
	// Unstyled children (line boxes) don't get PaintLayers.
	root := &layout.Box{Style: css.NewStyle()}
	lineBox := &layout.Box{Parent: root} // Style == nil
	styledGrandchild := &layout.Box{Style: css.NewStyle(), Parent: lineBox}
	lineBox.Children = []*layout.Box{styledGrandchild}
	root.Children = []*layout.Box{lineBox}

	layer := BuildPaintTree(root)
	// The styled grandchild should appear as a FlowChild of root's layer
	// (since the line box was skipped).
	if len(layer.FlowChildren) != 1 {
		t.Fatalf("expected 1 flow child (grandchild promoted), got %d", len(layer.FlowChildren))
	}
	if layer.FlowChildren[0].Box != styledGrandchild {
		t.Fatal("FlowChildren[0] should be the styled grandchild")
	}
}

func TestBuildPaintTree_OverflowContainment(t *testing.T) {
	// Positioned child stays in FlowChildren when parent has overflow:hidden.
	root := &layout.Box{Style: css.NewStyle()}
	clipParent := &layout.Box{
		Style:    styleWithOverflow("hidden"),
		Position: css.PositionRelative,
		Parent:   root,
	}
	clipParent.Style.Properties["position"] = "relative"
	absChild := &layout.Box{
		Style:    styleWithPos("absolute"),
		Position: css.PositionAbsolute,
		Parent:   clipParent,
	}
	clipParent.Children = []*layout.Box{absChild}
	root.Children = []*layout.Box{clipParent}

	layer := BuildPaintTree(root)
	// clipParent is positioned → goes to root SC's AutoZero.
	if len(layer.AutoZero) != 1 {
		t.Fatalf("expected 1 AutoZero (clipParent), got %d", len(layer.AutoZero))
	}
	clipLayer := layer.AutoZero[0]
	// absChild is contained by clipParent's overflow → stays in FlowChildren.
	if len(clipLayer.FlowChildren) != 1 {
		t.Fatalf("expected 1 flow child (contained abs), got %d", len(clipLayer.FlowChildren))
	}
	if clipLayer.FlowChildren[0].Box != absChild {
		t.Fatal("contained abs-pos child should be in FlowChildren")
	}
}

func TestBuildPaintTree_PositionNormalization(t *testing.T) {
	// Position="" (Go zero value) is normalized to PositionStatic.
	root := &layout.Box{Style: css.NewStyle()}
	child := &layout.Box{Style: css.NewStyle(), Parent: root}
	// child.Position is "" (zero value)
	root.Children = []*layout.Box{child}

	layer := BuildPaintTree(root)
	if layer.Position != css.PositionStatic {
		t.Fatalf("root position = %q, want %q", layer.Position, css.PositionStatic)
	}
	if layer.FlowChildren[0].Position != css.PositionStatic {
		t.Fatalf("child position = %q, want %q", layer.FlowChildren[0].Position, css.PositionStatic)
	}
}

func TestBuildPaintTree_ClipRect(t *testing.T) {
	root := &layout.Box{
		Style: styleWithOverflow("hidden"),
		X:     10, Y: 20,
		Width: 100, Height: 80,
		Border: css.BoxEdge{Top: 2, Right: 3, Bottom: 2, Left: 3},
	}

	layer := BuildPaintTree(root)
	if !layer.HasClip {
		t.Fatal("expected HasClip=true")
	}
	wantX, wantY := 13.0, 22.0
	wantW, wantH := 94.0, 76.0
	if layer.ClipRect[0] != wantX || layer.ClipRect[1] != wantY ||
		layer.ClipRect[2] != wantW || layer.ClipRect[3] != wantH {
		t.Fatalf("ClipRect = %v, want [%v %v %v %v]", layer.ClipRect, wantX, wantY, wantW, wantH)
	}
}
