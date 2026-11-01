package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestSetLayoutResult pins LayoutBox::SetLayoutResult's slot semantics
// (layout_box.cc:2658-2703 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// results past the end are appended; replacing one that still has results
// after it keeps them while it breaks (they may be read before layout
// reaches them), and drops them when it ends the box, broke for a column
// spanner, or is a fragmentation context root.
func TestSetLayoutResult(t *testing.T) {
	broke := func() *LayoutResult {
		return &LayoutResult{Fragment: &PhysicalFragment{}, BreakToken: &BlockBreakToken{}}
	}
	ended := func() *LayoutResult { return &LayoutResult{Fragment: &PhysicalFragment{}} }
	three := func() *LayoutInputNode {
		n := &LayoutInputNode{}
		n.SetLayoutResult(broke(), 0)
		n.SetLayoutResult(broke(), 1)
		n.SetLayoutResult(ended(), 2)
		return n
	}

	n := three()
	if got := len(n.GetLayoutResults()); got != 3 {
		t.Fatalf("appended %d results, want 3", got)
	}
	r := broke()
	n.SetLayoutResult(r, 0)
	if got := n.GetLayoutResults(); len(got) != 3 || got[0] != r {
		t.Errorf("replacing a breaking result: %d results, first replaced %v; want 3, true", len(got), got[0] == r)
	}

	n = three()
	n.SetLayoutResult(ended(), 1)
	if got := len(n.GetLayoutResults()); got != 2 {
		t.Errorf("replacing with an ending result left %d results, want 2", got)
	}

	n = three()
	spanner := broke()
	spanner.BreakToken.IsCausedByColumnSpanner = true
	n.SetLayoutResult(spanner, 0)
	if got := len(n.GetLayoutResults()); got != 1 {
		t.Errorf("replacing with a spanner break left %d results, want 1", got)
	}

	n = three()
	root := broke()
	root.Fragment.IsFragmentationContextRoot = true
	n.SetLayoutResult(root, 0)
	if got := len(n.GetLayoutResults()); got != 1 {
		t.Errorf("replacing with a fragmentation context root left %d results, want 1", got)
	}
}

// TestLayoutResultsAreTheTreeFragments pins the per-box results after
// layout to the fragments in the fragment tree, in order, for a block
// broken across the columns of an auto-filled and of a balanced multicol
// (whose balancing passes lay the block out several times), and for an
// abspos whose containing block is in the columns, laid out at the multicol
// (sized unfragmented, then laid out in the columns). Blink stores each layout pass's result on the box
// (BlockNode::StoreResultInLayoutBox, block_node.cc:916-937 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and reads them back as
// LayoutBox::PhysicalFragments.
func TestLayoutResultsAreTheTreeFragments(t *testing.T) {
	for _, tc := range []struct {
		name, columnFill string
		childStyle       []string
		inRelpos         bool
	}{
		{"auto", "auto", []string{"display", "block", "height", "150px"}, false},
		{"balance", "balance", []string{"display", "block", "height", "150px"}, false},
		{"abspos", "auto", []string{"display", "block", "position", "absolute", "width", "10px", "height", "150px"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := makeNode("div")
			styles := map[*html.Node]*css.Style{x: makeStyle(tc.childStyle...)}
			content := x
			if tc.inRelpos {
				content = makeNode("div", x)
				styles[content] = makeStyle("display", "block", "position", "relative", "height", "10px")
			}
			mc := twoColumnMulticol(content, styles, tc.columnFill)
			ctx := testContext()
			root := buildTestTree(mc, styles)
			wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				Build()
			result := layoutElement(ctx, root, space)
			node := findLayoutNode(root, x)
			if node == nil {
				t.Fatal("no layout node for the child")
			}
			var inTree []*PhysicalFragment
			walkFragmentsForNode(result.Fragment, node, LogicalOffset{}, func(f *PhysicalFragment, _ LogicalOffset) {
				inTree = append(inTree, f)
			})
			stored := node.GetLayoutResults()
			if len(inTree) != 2 || len(stored) != len(inTree) {
				t.Fatalf("%d fragments in the tree, %d stored; want 2 each", len(inTree), len(stored))
			}
			for i := range inTree {
				if stored[i].Fragment != inTree[i] {
					t.Errorf("stored result %d is not the tree's fragment %d", i, i)
				}
			}
		})
	}
}

// TestDisableLayoutSideEffects pins that a layout inside a
// DisableLayoutSideEffectsScope stores no result on the box
// (BlockNode::FinishLayout, block_node.cc:837-840 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestDisableLayoutSideEffects(t *testing.T) {
	x := makeNode("div")
	styles := map[*html.Node]*css.Style{x: makeStyle("display", "block", "height", "10px")}
	ctx := testContext()
	root := buildTestTree(x, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()
	enable := ctx.DisableLayoutSideEffects()
	layoutElement(ctx, root, space)
	enable()
	if got := len(root.GetLayoutResults()); got != 0 {
		t.Errorf("a side-effect-free layout stored %d results, want 0", got)
	}
	result := layoutElement(ctx, root, space)
	if got := root.GetLayoutResults(); len(got) != 1 || got[0] != result {
		t.Errorf("a layout stored %d results (first is the result: %v), want 1, true", len(got), len(got) > 0 && got[0] == result)
	}
}

// walkFragmentsForNode calls visit for every fragment of node under frag (a
// test oracle for the boxes' stored fragments),
// with its offset in frag's content box. Horizontal-tb only, like the rest
// of louis14's OOF fragmentation (plan RC-8).
func walkFragmentsForNode(frag *PhysicalFragment, node *LayoutInputNode, parentOffset LogicalOffset, visit func(*PhysicalFragment, LogicalOffset)) {
	for _, ch := range frag.Children {
		if ch.Fragment == nil {
			continue
		}
		off := LogicalOffset{
			InlineOffset: parentOffset.InlineOffset + ch.Offset.LeftF64(),
			BlockOffset:  parentOffset.BlockOffset + ch.Offset.TopF64(),
		}
		if ch.Fragment.isFragmentOf(node) {
			visit(ch.Fragment, off)
			continue
		}
		if bd := ch.Fragment.BoxData; bd != nil {
			off.InlineOffset += bd.Border.Left + bd.Padding.Left
			off.BlockOffset += bd.Border.Top + bd.Padding.Top
		}
		walkFragmentsForNode(ch.Fragment, node, off, visit)
	}
}
