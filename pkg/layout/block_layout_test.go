package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// makeNode creates a simple element node with the given tag name.
func makeNode(tag string, children ...*html.Node) *html.Node {
	n := &html.Node{
		Type:    html.ElementNode,
		TagName: tag,
	}
	for _, c := range children {
		c.Parent = n
		n.Children = append(n.Children, c)
	}
	return n
}

// makeStyle creates a style with the given CSS property/value pairs.
func makeStyle(props ...string) *css.Style {
	s := css.NewStyle()
	for i := 0; i+1 < len(props); i += 2 {
		s.Properties[props[i]] = props[i+1]
	}
	return s
}

func TestBlockLayout_SingleDivExplicitSize(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("width", "200px", "height", "100px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, root, space).Layout()

	if result.Fragment.Size.Width != 200 || result.Fragment.Size.Height != 100 {
		t.Errorf("got %vx%v, want 200x100", result.Fragment.Size.Width, result.Fragment.Size.Height)
	}
}

func TestBlockLayout_AutoInlineSize(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("height", "50px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, root, space).Layout()

	// Auto width should fill available (800), height explicit (50).
	if result.Fragment.Size.Width != 800 {
		t.Errorf("width: got %v, want 800", result.Fragment.Size.Width)
	}
	if result.Fragment.Size.Height != 50 {
		t.Errorf("height: got %v, want 50", result.Fragment.Size.Height)
	}
}

func TestBlockLayout_NestedBlocks(t *testing.T) {
	child1 := makeNode("div")
	child2 := makeNode("div")
	parent := makeNode("div", child1, child2)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle("width", "400px", "display", "block"),
		child1: makeStyle("height", "100px", "display", "block"),
		child2: makeStyle("height", "150px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	// Parent: 400px wide, auto height = 100 + 150 = 250.
	if result.Fragment.Size.Width != 400 {
		t.Errorf("parent width: got %v, want 400", result.Fragment.Size.Width)
	}
	if result.Fragment.Size.Height != 250 {
		t.Errorf("parent height: got %v, want 250", result.Fragment.Size.Height)
	}

	// Two children.
	if len(result.Fragment.Children) != 2 {
		t.Fatalf("children: got %d, want 2", len(result.Fragment.Children))
	}

	// Child 1 at Y=0.
	c1 := result.Fragment.Children[0]
	if c1.Offset.Y != 0 {
		t.Errorf("child1 Y: got %v, want 0", c1.Offset.Y)
	}
	if c1.Fragment.Size.Height != 100 {
		t.Errorf("child1 height: got %v, want 100", c1.Fragment.Size.Height)
	}

	// Child 2 at Y=100.
	c2 := result.Fragment.Children[1]
	if c2.Offset.Y != 100 {
		t.Errorf("child2 Y: got %v, want 100", c2.Offset.Y)
	}
	if c2.Fragment.Size.Height != 150 {
		t.Errorf("child2 height: got %v, want 150", c2.Fragment.Size.Height)
	}

	// Both children should fill parent width (400).
	if c1.Fragment.Size.Width != 400 {
		t.Errorf("child1 width: got %v, want 400", c1.Fragment.Size.Width)
	}
	if c2.Fragment.Size.Width != 400 {
		t.Errorf("child2 width: got %v, want 400", c2.Fragment.Size.Width)
	}
}

func TestBlockLayout_MarginCollapsing(t *testing.T) {
	child1 := makeNode("div")
	child2 := makeNode("div")
	parent := makeNode("div", child1, child2)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle("width", "400px", "display", "block"),
		child1: makeStyle("height", "100px", "display", "block", "margin-bottom", "30px"),
		child2: makeStyle("height", "100px", "display", "block", "margin-top", "20px"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	// Margins collapse: max(30, 20) = 30.
	// Total height = 100 + 30 + 100 = 230.
	if result.Fragment.Size.Height != 230 {
		t.Errorf("height: got %v, want 230", result.Fragment.Size.Height)
	}

	c2 := result.Fragment.Children[1]
	// Child 2 at Y = 100 + 30 = 130.
	if c2.Offset.Y != 130 {
		t.Errorf("child2 Y: got %v, want 130", c2.Offset.Y)
	}
}

func TestBlockLayout_WithPadding(t *testing.T) {
	child := makeNode("div")
	parent := makeNode("div", child)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle(
			"width", "400px", "display", "block",
			"padding-top", "10px", "padding-bottom", "20px",
			"padding-left", "15px", "padding-right", "15px",
		),
		child: makeStyle("height", "100px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	// Width: 400 (content) + 15 + 15 (padding) = 430.
	// Height: 100 (content) + 10 + 20 (padding) = 130.
	if result.Fragment.Size.Width != 430 {
		t.Errorf("width: got %v, want 430", result.Fragment.Size.Width)
	}
	if result.Fragment.Size.Height != 130 {
		t.Errorf("height: got %v, want 130", result.Fragment.Size.Height)
	}
}

func TestBlockLayout_WithBorder(t *testing.T) {
	child := makeNode("div")
	parent := makeNode("div", child)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle(
			"width", "300px", "display", "block",
			"border-top-width", "5px", "border-bottom-width", "5px",
			"border-left-width", "3px", "border-right-width", "3px",
			"border-top-style", "solid", "border-bottom-style", "solid",
			"border-left-style", "solid", "border-right-style", "solid",
		),
		child: makeStyle("height", "80px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	// Width: 300 + 3 + 3 = 306. Height: 80 + 5 + 5 = 90.
	if result.Fragment.Size.Width != 306 {
		t.Errorf("width: got %v, want 306", result.Fragment.Size.Width)
	}
	if result.Fragment.Size.Height != 90 {
		t.Errorf("height: got %v, want 90", result.Fragment.Size.Height)
	}
}

func TestBlockLayout_DisplayNoneSkipped(t *testing.T) {
	child1 := makeNode("div")
	hidden := makeNode("div")
	child2 := makeNode("div")
	parent := makeNode("div", child1, hidden, child2)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle("width", "400px", "display", "block"),
		child1: makeStyle("height", "50px", "display", "block"),
		hidden: makeStyle("height", "999px", "display", "none"),
		child2: makeStyle("height", "50px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	// Only 2 children laid out, display:none skipped.
	if len(result.Fragment.Children) != 2 {
		t.Fatalf("children: got %d, want 2", len(result.Fragment.Children))
	}

	// Height = 50 + 50 = 100 (no 999px).
	if result.Fragment.Size.Height != 100 {
		t.Errorf("height: got %v, want 100", result.Fragment.Size.Height)
	}
}

func TestBlockLayout_BoxSizingBorderBox(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle(
			"width", "400px", "height", "300px",
			"display", "block", "box-sizing", "border-box",
			"padding-left", "20px", "padding-right", "20px",
			"padding-top", "10px", "padding-bottom", "10px",
			"border-left-width", "5px", "border-right-width", "5px",
			"border-top-width", "5px", "border-bottom-width", "5px",
			"border-left-style", "solid", "border-right-style", "solid",
			"border-top-style", "solid", "border-bottom-style", "solid",
		),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, root, space).Layout()

	// border-box: 400px total width, 300px total height.
	if result.Fragment.Size.Width != 400 {
		t.Errorf("width: got %v, want 400", result.Fragment.Size.Width)
	}
	if result.Fragment.Size.Height != 300 {
		t.Errorf("height: got %v, want 300", result.Fragment.Size.Height)
	}
}

func TestBlockLayout_ExplicitBlockSize(t *testing.T) {
	child := makeNode("div")
	parent := makeNode("div", child)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle("width", "400px", "height", "500px", "display", "block"),
		child:  makeStyle("height", "100px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	// Explicit height takes precedence over intrinsic (100px child).
	if result.Fragment.Size.Height != 500 {
		t.Errorf("height: got %v, want 500", result.Fragment.Size.Height)
	}
}

func TestBlockLayout_ChildWithMargins(t *testing.T) {
	child := makeNode("div")
	parent := makeNode("div", child)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle("width", "400px", "display", "block"),
		child: makeStyle(
			"height", "100px", "display", "block",
			"margin-left", "20px", "margin-right", "30px",
			"margin-top", "10px",
		),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	c := result.Fragment.Children[0]

	// Child positioned with margin-left inline offset and margin-top block offset.
	if c.Offset.X != 20 {
		t.Errorf("child X: got %v, want 20", c.Offset.X)
	}
	if c.Offset.Y != 10 {
		t.Errorf("child Y: got %v, want 10", c.Offset.Y)
	}
}

func TestEngine_LayoutProducesBoxes(t *testing.T) {
	child := makeNode("div")
	body := makeNode("body", child)
	htmlNode := makeNode("html", body)

	doc := &html.Document{
		Root: htmlNode,
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 root box, got %d", len(boxes))
	}

	root := boxes[0]
	// Root fills viewport width; height is 0 for empty content (correct).
	if root.Width != 800 {
		t.Errorf("root width: got %v, want 800", root.Width)
	}
	if root.Height < 0 {
		t.Errorf("root height should be non-negative, got %v", root.Height)
	}
}

func TestBlockLayout_VRL_SwapsAxes(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle(
			"width", "200px", "height", "300px",
			"display", "block", "writing-mode", "vertical-rl",
		),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 300, BlockSize: 200}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 300, BlockSize: 200}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, root, space).Layout()

	// VRL: inline-size=height(300), block-size=width(200).
	// Physical: width=block(200)+border/padding, height=inline(300)+border/padding.
	// With no border/padding: width=200, height=300.
	if result.Fragment.Size.Width != 200 || result.Fragment.Size.Height != 300 {
		t.Errorf("got %vx%v, want 200x300", result.Fragment.Size.Width, result.Fragment.Size.Height)
	}
}

func TestLayoutElement_DispatchesBlock(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("width", "100px", "height", "50px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, root, space)

	if result.Fragment.Size.Width != 100 || result.Fragment.Size.Height != 50 {
		t.Errorf("got %vx%v, want 100x50", result.Fragment.Size.Width, result.Fragment.Size.Height)
	}
}

func TestLayoutElement_DisplayNone(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "none"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, root, space)

	if result.Fragment.Size.Width != 0 || result.Fragment.Size.Height != 0 {
		t.Errorf("display:none should produce zero-size, got %vx%v",
			result.Fragment.Size.Width, result.Fragment.Size.Height)
	}
}

func TestBlockLayout_TextNodesSkipped(t *testing.T) {
	textNode := &html.Node{Type: html.TextNode, Text: "hello"}
	child := makeNode("div")
	parent := makeNode("div", textNode, child)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle("width", "400px", "display", "block"),
		child:  makeStyle("height", "50px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, parent, space).Layout()

	// Only the element child produces a fragment; text node is skipped.
	if len(result.Fragment.Children) != 1 {
		t.Errorf("children: got %d, want 1", len(result.Fragment.Children))
	}
}

func TestBlockLayout_DeeplyNested(t *testing.T) {
	inner := makeNode("div")
	mid := makeNode("div", inner)
	outer := makeNode("div", mid)

	styles := map[*html.Node]*css.Style{
		outer: makeStyle("width", "600px", "display", "block"),
		mid:   makeStyle("display", "block"),
		inner: makeStyle("height", "40px", "display", "block"),
	}

	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, outer, space).Layout()

	// Outer: 600 wide, auto height = intrinsic from children.
	if result.Fragment.Size.Width != 600 {
		t.Errorf("outer width: got %v, want 600", result.Fragment.Size.Width)
	}
	// Mid: auto size, fills outer's 600px.
	// Inner: 40px tall.
	// Heights bubble up: inner=40, mid=40, outer=40.
	if result.Fragment.Size.Height != 40 {
		t.Errorf("outer height: got %v, want 40", result.Fragment.Size.Height)
	}

	// Check mid fills width.
	midFrag := result.Fragment.Children[0].Fragment
	if midFrag.Size.Width != 600 {
		t.Errorf("mid width: got %v, want 600", midFrag.Size.Width)
	}

	// Check inner fills width.
	innerFrag := midFrag.Children[0].Fragment
	if innerFrag.Size.Width != 600 {
		t.Errorf("inner width: got %v, want 600", innerFrag.Size.Width)
	}
}
