package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-503 (worklist C16 + C14): LayoutNG block-in-inline replaces the CSS 2.1
// continuation split. Blink @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b:
//   - LayoutInline::AddChildAsBlockInInline (layout_inline.cc:320-345): a
//     block child of an inline goes into an anonymous block-in-inline
//     wrapper that stays a child of the inline; consecutive block children
//     share one wrapper. The inline is not split.
//   - InlineItemsBuilder::AppendBlockInInline (inline/inline_items_builder.cc:
//     1307-1330) emits one kBlockInInline item for the wrapper.
//   - LineBreaker::HandleBlockInInline (inline/line_breaker.cc:3219-3301)
//     forces a break before it and gives it a line of its own.

func blockInInlineSpace(inline float64) ConstraintSpace {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	return NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: inline, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: inline, BlockSize: 600}).
		Build()
}

// The builder keeps the inline whole and wraps its block child in an
// anonymous block-in-inline box.
func TestBlockInInline_BuilderWrapsBlockInsideInline(t *testing.T) {
	p := makeNode("p", makeTextNode("b"))
	span := makeNode("span", makeTextNode("a"), p, makeTextNode("c"))
	root := makeNode("div", span)
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		span: makeStyle("display", "inline"),
		p:    makeStyle("display", "block"),
	}
	tree := buildTestTree(root, styles)
	if got := len(tree.Children()); got != 1 {
		t.Fatalf("div children: got %d, want 1 (the unsplit span)", got)
	}
	spanNode := tree.Children()[0]
	if spanNode.DOMNode != span {
		t.Fatalf("div child is not the span")
	}
	kids := spanNode.Children()
	if len(kids) != 3 {
		t.Fatalf("span children: got %d, want 3 (text, wrapper, text)", len(kids))
	}
	w := kids[1]
	if !w.IsBlockInInline() || !w.IsAnonymous() {
		t.Fatalf("middle span child: IsBlockInInline=%v IsAnonymous=%v, want both", w.IsBlockInInline(), w.IsAnonymous())
	}
	if len(w.Children()) != 1 || w.Children()[0].DOMNode != p {
		t.Fatalf("wrapper does not hold the <p>")
	}
}

// Consecutive block children of one inline share a wrapper; a float between
// them (added to the inline directly) starts a new one.
func TestBlockInInline_ConsecutiveBlocksShareWrapper(t *testing.T) {
	p1 := makeNode("p")
	p2 := makeNode("p")
	f := makeNode("i")
	p3 := makeNode("p")
	span := makeNode("span", p1, p2, f, p3)
	root := makeNode("div", span)
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		span: makeStyle("display", "inline"),
		p1:   makeStyle("display", "block"),
		p2:   makeStyle("display", "block"),
		f:    makeStyle("display", "block", "float", "left"),
		p3:   makeStyle("display", "block"),
	}
	kids := buildTestTree(root, styles).Children()[0].Children()
	if len(kids) != 3 {
		t.Fatalf("span children: got %d, want 3 (wrapper{p1,p2}, float, wrapper{p3})", len(kids))
	}
	if !kids[0].IsBlockInInline() || len(kids[0].Children()) != 2 {
		t.Errorf("first wrapper should hold p1 and p2")
	}
	if kids[1].DOMNode != f {
		t.Errorf("float should stay a direct child of the span")
	}
	if !kids[2].IsBlockInInline() || len(kids[2].Children()) != 1 {
		t.Errorf("second wrapper should hold p3")
	}
}

// The lines around a block-in-inline are line boxes of the containing block
// itself, not of anonymous blocks: the block's fragment children are
// line, block-in-inline wrapper, line.
func TestBlockInInline_LinesAreChildrenOfContainingBlock(t *testing.T) {
	p := makeNode("p", makeTextNode("b"))
	span := makeNode("span", makeTextNode("a"), p, makeTextNode("c"))
	root := makeNode("div", span)
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block", "width", "200px", "font-family", "Ahem", "font-size", "10px"),
		span: makeStyle("display", "inline", "font-family", "Ahem", "font-size", "10px"),
		p:    makeStyle("display", "block", "font-family", "Ahem", "font-size", "10px"),
	}
	result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(root, styles), blockInInlineSpace(200)).Layout()
	var kinds []string
	for _, ch := range result.Fragment.Children {
		switch {
		case ch.Fragment.Type == FragmentLineBox:
			if ch.Fragment.Size.HeightF64() > 0 {
				kinds = append(kinds, "line")
			}
		case ch.Fragment.LayoutNode != nil && ch.Fragment.LayoutNode.IsBlockInInline():
			kinds = append(kinds, "bii")
		default:
			kinds = append(kinds, "other")
		}
	}
	want := []string{"line", "bii", "line"}
	if len(kinds) != len(want) {
		t.Fatalf("div fragment children: got %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("div fragment children: got %v, want %v", kinds, want)
		}
	}
	lineHeight := result.Fragment.Children[0].Fragment.Size.HeightF64()
	if got := result.Fragment.Size.HeightF64(); got != 3*lineHeight {
		t.Errorf("div height: got %v, want %v (three %vpx lines)", got, 3*lineHeight, lineHeight)
	}
}

// LOU-412: floats that sit beside a block inside an inline belong to the
// inline formatting context and must be laid out (multicol-span-float-003
// lost all four).
func TestBlockInInline_FloatsBesideBlockAreLaidOut(t *testing.T) {
	f1 := makeNode("i", makeTextNode("x"))
	p := makeNode("p", makeTextNode("b"))
	f2 := makeNode("b", makeTextNode("y"))
	span := makeNode("span", makeTextNode(" "), f1, makeTextNode(" "), p, makeTextNode(" "), f2, makeTextNode(" "))
	root := makeNode("div", span)
	floatStyle := func() *css.Style {
		return makeStyle("display", "block", "float", "left", "width", "20px", "height", "10px")
	}
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block", "width", "200px", "font-family", "Ahem", "font-size", "10px"),
		span: makeStyle("display", "inline", "font-family", "Ahem", "font-size", "10px"),
		f1:   floatStyle(),
		p:    makeStyle("display", "block", "font-family", "Ahem", "font-size", "10px"),
		f2:   floatStyle(),
	}
	result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(root, styles), blockInInlineSpace(200)).Layout()
	for name, n := range map[string]*html.Node{"first float": f1, "second float": f2} {
		if findFragmentByNode(result.Fragment, n) == nil {
			t.Errorf("%s has no fragment", name)
		}
	}
}

// C14: a break-after on the block inside an inline arrives at the boundary
// before the next line (Blink CalculateBreakBetweenValue's inline-child
// branch, fragmentation_utils.cc:133-136 @ a9f50e522efa), forcing a break
// there.
func TestBlockInInline_BreakAfterForcesBreakBeforeNextLine(t *testing.T) {
	p := makeNode("p")
	span := makeNode("span", p, makeTextNode("c"))
	root := makeNode("div", span)
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block", "font-family", "Ahem", "font-size", "10px"),
		span: makeStyle("display", "inline", "font-family", "Ahem", "font-size", "10px"),
		p:    makeStyle("display", "block", "height", "20px", "break-after", "column"),
	}
	res := layoutBlockInFragmentainer(t, root, styles, 200, 0)
	if res.BreakToken == nil {
		t.Fatalf("no break token: the forced break after the block was ignored")
	}
	if got := res.Fragment.Size.HeightF64(); got < 20 {
		t.Errorf("first fragment height %v, want at least the 20px block", got)
	}
	for _, ch := range res.Fragment.Children {
		if ch.Fragment.Type == FragmentLineBox && ch.Fragment.Size.HeightF64() > 0 {
			t.Errorf("the line after the forced break was placed in the first fragment")
		}
	}
}
