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

// buildTestTree constructs a LayoutInputNode tree from a DOM tree and style map.
func buildTestTree(root *html.Node, styles map[*html.Node]*css.Style) *LayoutInputNode {
	builder := &LayoutTreeBuilder{styles: styles}
	return builder.BuildLayoutTree(root)
}

// testContext creates a LayoutContext for tests.
func testContext() *LayoutContext {
	return &LayoutContext{ViewportWidth: 800, ViewportHeight: 600}
}

func TestBlockLayout_SingleDivExplicitSize(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("width", "200px", "height", "100px", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 200 || result.Fragment.Size.HeightF64() != 100 {
		t.Errorf("got %vx%v, want 200x100", result.Fragment.Size.WidthF64(), result.Fragment.Size.HeightF64())
	}
}

func TestBlockLayout_AutoInlineSize(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("height", "50px", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 800 {
		t.Errorf("width: got %v, want 800", result.Fragment.Size.WidthF64())
	}
	if result.Fragment.Size.HeightF64() != 50 {
		t.Errorf("height: got %v, want 50", result.Fragment.Size.HeightF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 400 {
		t.Errorf("parent width: got %v, want 400", result.Fragment.Size.WidthF64())
	}
	if result.Fragment.Size.HeightF64() != 250 {
		t.Errorf("parent height: got %v, want 250", result.Fragment.Size.HeightF64())
	}

	if len(result.Fragment.Children) != 2 {
		t.Fatalf("children: got %d, want 2", len(result.Fragment.Children))
	}

	c1 := result.Fragment.Children[0]
	if c1.Offset.TopF64() != 0 {
		t.Errorf("child1 Y: got %v, want 0", c1.Offset.TopF64())
	}
	if c1.Fragment.Size.HeightF64() != 100 {
		t.Errorf("child1 height: got %v, want 100", c1.Fragment.Size.HeightF64())
	}

	c2 := result.Fragment.Children[1]
	if c2.Offset.TopF64() != 100 {
		t.Errorf("child2 Y: got %v, want 100", c2.Offset.TopF64())
	}
	if c2.Fragment.Size.HeightF64() != 150 {
		t.Errorf("child2 height: got %v, want 150", c2.Fragment.Size.HeightF64())
	}

	if c1.Fragment.Size.WidthF64() != 400 {
		t.Errorf("child1 width: got %v, want 400", c1.Fragment.Size.WidthF64())
	}
	if c2.Fragment.Size.WidthF64() != 400 {
		t.Errorf("child2 width: got %v, want 400", c2.Fragment.Size.WidthF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.HeightF64() != 230 {
		t.Errorf("height: got %v, want 230", result.Fragment.Size.HeightF64())
	}

	c2 := result.Fragment.Children[1]
	if c2.Offset.TopF64() != 130 {
		t.Errorf("child2 Y: got %v, want 130", c2.Offset.TopF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 430 {
		t.Errorf("width: got %v, want 430", result.Fragment.Size.WidthF64())
	}
	if result.Fragment.Size.HeightF64() != 130 {
		t.Errorf("height: got %v, want 130", result.Fragment.Size.HeightF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 306 {
		t.Errorf("width: got %v, want 306", result.Fragment.Size.WidthF64())
	}
	if result.Fragment.Size.HeightF64() != 90 {
		t.Errorf("height: got %v, want 90", result.Fragment.Size.HeightF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if len(result.Fragment.Children) != 2 {
		t.Fatalf("children: got %d, want 2", len(result.Fragment.Children))
	}

	if result.Fragment.Size.HeightF64() != 100 {
		t.Errorf("height: got %v, want 100", result.Fragment.Size.HeightF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 400 {
		t.Errorf("width: got %v, want 400", result.Fragment.Size.WidthF64())
	}
	if result.Fragment.Size.HeightF64() != 300 {
		t.Errorf("height: got %v, want 300", result.Fragment.Size.HeightF64())
	}
}

func TestBlockLayout_ExplicitBlockSize(t *testing.T) {
	child := makeNode("div")
	parent := makeNode("div", child)

	styles := map[*html.Node]*css.Style{
		parent: makeStyle("width", "400px", "height", "500px", "display", "block"),
		child:  makeStyle("height", "100px", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.HeightF64() != 500 {
		t.Errorf("height: got %v, want 500", result.Fragment.Size.HeightF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	c := result.Fragment.Children[0]

	if c.Offset.LeftF64() != 20 {
		t.Errorf("child X: got %v, want 20", c.Offset.LeftF64())
	}
	if c.Offset.TopF64() != 10 {
		t.Errorf("child Y: got %v, want 10", c.Offset.TopF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 300, BlockSize: 200}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 300, BlockSize: 200}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 200 || result.Fragment.Size.HeightF64() != 300 {
		t.Errorf("got %vx%v, want 200x300", result.Fragment.Size.WidthF64(), result.Fragment.Size.HeightF64())
	}
}

func TestLayoutElement_DispatchesBlock(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("width", "100px", "height", "50px", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if result.Fragment.Size.WidthF64() != 100 || result.Fragment.Size.HeightF64() != 50 {
		t.Errorf("got %vx%v, want 100x50", result.Fragment.Size.WidthF64(), result.Fragment.Size.HeightF64())
	}
}

func TestLayoutElement_DisplayNone(t *testing.T) {
	root := makeNode("div")
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "none"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if result.Fragment.Size.WidthF64() != 0 || result.Fragment.Size.HeightF64() != 0 {
		t.Errorf("display:none should produce zero-size, got %vx%v",
			result.Fragment.Size.WidthF64(), result.Fragment.Size.HeightF64())
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

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	// Text node + block child = mixed content. Anonymous block wrapping
	// generates: [anonymous-block("hello"), block-div].
	if len(result.Fragment.Children) != 2 {
		t.Errorf("children: got %d, want 2", len(result.Fragment.Children))
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

	ctx := testContext()
	layoutRoot := buildTestTree(outer, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	if result.Fragment.Size.WidthF64() != 600 {
		t.Errorf("outer width: got %v, want 600", result.Fragment.Size.WidthF64())
	}
	if result.Fragment.Size.HeightF64() != 40 {
		t.Errorf("outer height: got %v, want 40", result.Fragment.Size.HeightF64())
	}

	midFrag := result.Fragment.Children[0].Fragment
	if midFrag.Size.WidthF64() != 600 {
		t.Errorf("mid width: got %v, want 600", midFrag.Size.WidthF64())
	}

	innerFrag := midFrag.Children[0].Fragment
	if innerFrag.Size.WidthF64() != 600 {
		t.Errorf("inner width: got %v, want 600", innerFrag.Size.WidthF64())
	}
}

// TestBlockLayout_PaintIsolationInhibitsCollapseThrough proves that a zero-
// sized child with `filter` (or other paint-isolation property) is NOT
// dropped via margin collapse-through, so its fragment remains in the
// parent's children and reaches the paint walk. Regression guard for the
// WPT filter-effects/empty-element-with-filter-{002,004} tests, where a
// 0x0 div with `filter:url(#flood)` must still paint flood pixels.
func TestBlockLayout_PaintIsolationInhibitsCollapseThrough(t *testing.T) {
	flooded := makeNode("div")
	sibling := makeNode("div")
	parent := makeNode("div", flooded, sibling)

	styles := map[*html.Node]*css.Style{
		parent:  makeStyle("width", "400px", "display", "block"),
		flooded: makeStyle("width", "0px", "height", "0px", "display", "block", "filter", "url(#f)"),
		sibling: makeStyle("height", "10px", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	// The filtered child must remain in the fragment tree even though
	// its own block size is zero — paint isolation inhibits collapse-
	// through. Without this guard, the paint walk has no way to reach
	// the layer and the filter never runs.
	if len(result.Fragment.Children) != 2 {
		t.Fatalf("filtered 0x0 child dropped by collapse-through: got %d children, want 2", len(result.Fragment.Children))
	}
}

// TestBlockLayout_EmptyDivStillCollapsesThrough proves the negative — an
// element with no paint-isolation property still collapses through, so
// the new guard is narrowly targeted and doesn't perturb the baseline
// CSS 2.1 §8.3.1 behavior.
func TestBlockLayout_EmptyDivStillCollapsesThrough(t *testing.T) {
	empty := makeNode("div")
	sibling := makeNode("div")
	parent := makeNode("div", empty, sibling)

	styles := map[*html.Node]*css.Style{
		parent:  makeStyle("width", "400px", "display", "block"),
		empty:   makeStyle("width", "0px", "height", "0px", "display", "block"),
		sibling: makeStyle("height", "10px", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	// CSS 2.1 §8.3.1 collapse-through: the empty 0x0 div with no
	// paint-isolation property must still be dropped from the
	// fragment tree (existing behavior; ensures the new guard is
	// not a blanket "always emit").
	if len(result.Fragment.Children) != 1 {
		t.Fatalf("plain empty 0x0 child unexpectedly kept: got %d children, want 1", len(result.Fragment.Children))
	}
}
