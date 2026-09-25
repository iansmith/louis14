package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestPaddedContainer_ContinuationRefusesBreakBeforeFirstLine pins the
// continuation half of fragmentainerOffsetForChildren: a resumed fragment
// consumes nothing before its first child, so a break before that child is
// refused again and layout makes progress.
//
// Column: 100px tall. Container: padding-top 20px whose only content is a
// 90px-tall inline-block line. First fragment: the line does not fit below
// the padding (20 + 90 > 100) and the padding is consumed space, so the line
// is pushed (Blink MovePastBreakpoint: refuse_break_before only when
// space_left >= fragmentainer_block_size, fragmentation_utils.cc:1169-1171 @
// a9f50e522efa). Continuation in column 2: Blink clears the block-start
// border/padding on a slice-mode continuation (fragmentation_utils.cc:504-510,
// ClearBorderScrollbarPaddingBlockStart), so nothing is consumed before the
// line, the break is refused, and the line is placed. Counting the repeated
// edge on the continuation instead re-pushes the line from every column and
// never terminates.
func TestPaddedContainer_ContinuationRefusesBreakBeforeFirstLine(t *testing.T) {
	ib := makeNode("span")
	container := makeNode("div", ib)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block", "padding-top", "20px"),
		ib:        makeStyle("display", "inline-block", "width", "10px", "height", "90px"),
	}
	cols := layoutTwoColumnAutoFillColumns(t, container, styles)
	if len(cols) != 2 {
		t.Errorf("multicol has %d columns, want 2 (the continuation carries no repeated padding, so the line fits)", len(cols))
	}
	lineBoxes := func(col *PhysicalFragment) int {
		n := 0
		for _, ch := range col.Children {
			n += len(ch.Fragment.Children)
		}
		return n
	}
	if len(cols[0].Fragment.Children) != 1 || lineBoxes(cols[0].Fragment) != 0 {
		t.Errorf("column 1 holds %d children with %d line boxes, want the container with 0 (the line is pushed past the padding)",
			len(cols[0].Fragment.Children), lineBoxes(cols[0].Fragment))
	}
	if len(cols[1].Fragment.Children) != 1 || lineBoxes(cols[1].Fragment) != 1 {
		t.Errorf("column 2 holds %d children with %d line boxes, want the continuation with 1 (the line placed at the column start)",
			len(cols[1].Fragment.Children), lineBoxes(cols[1].Fragment))
	}
	for i := 2; i < len(cols); i++ {
		if n := lineBoxes(cols[i].Fragment); n != 0 {
			t.Errorf("column %d holds %d line boxes, want 0 (the line must appear once)", i+1, n)
		}
	}
}

// layoutTwoColumnAutoFillColumns is layoutTwoColumnAutoFill without the
// two-column check, for fixtures that may need overflow columns.
func layoutTwoColumnAutoFillColumns(t *testing.T, container *html.Node, styles map[*html.Node]*css.Style) []ChildLink {
	t.Helper()
	mc := makeNode("div", container)
	styles[mc] = makeStyle(
		"display", "block",
		"width", "100px",
		"height", "100px",
		"column-count", "2",
		"column-gap", "0",
		"column-fill", "auto",
	)
	cols := layoutMulticolForTest(t, mc, styles).Fragment.Children
	if len(cols) < 2 {
		t.Fatalf("multicol has %d columns, want at least 2", len(cols))
	}
	return cols
}

// TestNestedMulticol_FirstChildOfPaddedBlockIsNotLost: an inner multicol
// with a declared height taller than the outer column's remaining space,
// sitting as the FIRST child of a padding-top container, must still lay out
// all of its declared height across the outer columns.
//
// louis14's multicol defers such a container (zero-height fragment) for the
// parent to push when it starts past the outer fragmentainer origin; with the
// container's padding now counted, that gate opens for a first child, and the
// parent must push it even without container separation. Blink's
// MovePastBreakpoint breaks before any child that does not fit unless the
// break is refused for lack of progress (space_left >= fragmentainer_block_size);
// container separation only shapes appeal_before (fragmentation_utils.cc:
// 1169-1213 @ a9f50e522efa). Gating the push on separation instead placed the
// empty deferred fragment and dropped 300px of content.
func TestNestedMulticol_FirstChildOfPaddedBlockIsNotLost(t *testing.T) {
	content := makeNode("div")
	innerMC := makeNode("div", content)
	container := makeNode("div", innerMC)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block", "padding-top", "20px"),
		innerMC: makeStyle(
			"display", "block",
			"height", "200px",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		content: makeStyle("display", "block", "height", "300px"),
	}
	total := 0.0
	for _, col := range layoutTwoColumnAutoFillColumns(t, container, styles) {
		for _, ch := range col.Fragment.Children {
			for _, inner := range ch.Fragment.Children {
				if inner.Fragment.Node == innerMC {
					total += inner.Fragment.Size.HeightF64()
				}
			}
		}
	}
	if total != 200 {
		t.Errorf("inner multicol fragments total %v of block-size across the outer columns, want 200 (its declared height)", total)
	}
}

// TestFloatDroppedAvoidChild_PushedWhole: a break-inside:avoid new-FC child
// dropped below a float it cannot fit beside must be pushed whole to the next
// column when it does not fit below the float, not sliced.
//
// Blink: the drop grants container separation (block offset past the
// estimate / IsPushedByFloats, block_layout_algorithm.cc:1993-1997 @
// a9f50e522efa) and MovePastBreakpoint breaks before a child that does not
// fit (fragmentation_utils.cc:1169-1213).
func TestFloatDroppedAvoidChild_PushedWhole(t *testing.T) {
	float := makeNode("div")
	inner := makeNode("div")
	avoid := makeNode("div", inner)
	container := makeNode("div", float, avoid)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block"),
		float:     makeStyle("display", "block", "float", "left", "width", "50px", "height", "50px"),
		avoid:     makeStyle("display", "flow-root", "width", "max-content", "break-inside", "avoid"),
		inner:     makeStyle("display", "block", "width", "80px", "height", "60px"),
	}
	mcFrag := layoutTwoColumnAutoFill(t, container, styles)
	col1, col2 := mcFrag.Children[0].Fragment, mcFrag.Children[1].Fragment
	for _, ch := range col1.Children[0].Fragment.Children {
		if ch.Fragment.Node == avoid {
			t.Errorf("avoid box present in column 1 (%v tall); want it pushed whole to column 2", ch.Fragment.Size.HeightF64())
		}
	}
	if len(col2.Children) != 1 {
		t.Fatalf("column 2 has %d children, want 1 (the container's continuation)", len(col2.Children))
	}
	cont2 := col2.Children[0].Fragment
	if n := len(cont2.Children); n != 1 || cont2.Children[0].Fragment.Node != avoid {
		t.Fatalf("container continuation in column 2 has %d children, want the avoid box", n)
	}
	if h := cont2.Children[0].Fragment.Size.HeightF64(); h != 60 {
		t.Errorf("avoid box height in column 2 = %v, want 60 (whole)", h)
	}
}

// TestInlineBreakToken_ConsumedExcludesContainerOffset: an inline-formatted
// block's break token reports what the block itself consumed, not its
// container's fragmentainer offset, so a definite-height block under a padded
// parent keeps its remaining budget for the last continuation.
//
// Blink: a break token's consumed block-size is the sum of the node's own
// fragment sizes (BlockBreakToken::ConsumedBlockSize); the fragmentainer
// offset of the container never enters it.
//
// Setup: 100px columns; padding-top:20px parent; a 10px-wide, 210px-tall block
// holding 21 lines of 10px inline-blocks (8 fit below the padding in column
// 1). Whatever the split, the last fragment must be 210 minus the lines the
// earlier fragments placed — with the parent's 20px offset folded into each
// fragment's consumed size it was clamped to 10px with lines still to place.
func TestInlineBreakToken_ConsumedExcludesContainerOffset(t *testing.T) {
	var lines []*html.Node
	for i := 0; i < 21; i++ {
		lines = append(lines, makeNode("span"))
	}
	block := makeNode("div", lines...)
	container := makeNode("div", block)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block", "padding-top", "20px"),
		block:     makeStyle("display", "block", "width", "10px", "height", "210px", "font-size", "0", "line-height", "0"),
	}
	for _, l := range lines {
		styles[l] = makeStyle("display", "inline-block", "width", "10px", "height", "10px", "vertical-align", "top")
	}
	var last *PhysicalFragment
	linesBefore := 0
	for _, col := range layoutTwoColumnAutoFillColumns(t, container, styles) {
		for _, ch := range col.Fragment.Children {
			for _, b := range ch.Fragment.Children {
				if b.Fragment.Node == block {
					if last != nil {
						linesBefore += len(last.Children)
					}
					last = b.Fragment
				}
			}
		}
	}
	if last == nil {
		t.Fatal("block not found in any column")
	}
	if linesBefore+len(last.Children) != 21 {
		t.Errorf("%d lines placed across the fragments, want 21", linesBefore+len(last.Children))
	}
	want := 210 - 10*float64(linesBefore)
	if h := last.Size.HeightF64(); h != want {
		t.Errorf("last fragment of the 210px block is %v tall, want %v (210 minus %d lines consumed before it)", h, want, linesBefore)
	}
}

// TestPaddedDefiniteContainer_NotAtBlockEndWhileHeightRemains: a definite-
// height padded container whose child breaks must not be marked at-block-end
// while its declared height is not yet consumed.
//
// Column 100px; container padding-top:20px; height:90px; child 150px block.
// The child slices at 80 (the 80px below the padding), so the container has
// 10px of declared height left and continues in column 2 with a 10px
// fragment holding the child's continuation. Blink compares the box's
// desired border-box size against the space left (FinishFragmentation,
// fragmentation_utils.cc:744-755 @ a9f50e522efa); comparing the content size
// against border-edge space (90 <= 100) marked the box at-block-end and
// zero-clamped the continuation.
func TestPaddedDefiniteContainer_NotAtBlockEndWhileHeightRemains(t *testing.T) {
	child := makeNode("div")
	container := makeNode("div", child)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block", "padding-top", "20px", "height", "90px"),
		child:     makeStyle("display", "block", "height", "150px"),
	}
	cols := layoutTwoColumnAutoFillColumns(t, container, styles)
	if len(cols[1].Fragment.Children) != 1 {
		t.Fatalf("column 2 has %d children, want 1 (the container's continuation)", len(cols[1].Fragment.Children))
	}
	cont2 := cols[1].Fragment.Children[0].Fragment
	if h := cont2.Size.HeightF64(); h != 10 {
		t.Errorf("container continuation height = %v, want 10 (90 declared - 80 consumed, no repeated padding)", h)
	}
	if n := len(cont2.Children); n != 1 || cont2.Children[0].Fragment.Node != child {
		t.Fatalf("container continuation has %d children, want the child's continuation", n)
	}
}
