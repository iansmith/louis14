package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// dumpFragmentTree logs a fragment subtree (offsets and sizes) for test
// diagnostics.
func dumpFragmentTree(t *testing.T, indent string, frag *PhysicalFragment) {
	t.Helper()
	if frag == nil {
		return
	}
	for i, ch := range frag.Children {
		t.Logf("%schild %d: pos=(%v,%v) size=%vx%v",
			indent, i, ch.Offset.Left.Float64(), ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64())
		dumpFragmentTree(t, indent+"  ", ch.Fragment)
	}
}

// layoutTwoColumnAutoFill lays out a 100x100 two-column column-fill:auto
// multicol (column-gap:0, so each column is 50 wide and 100 tall) whose
// single child is `container`, and returns the multicol fragment.
func layoutTwoColumnAutoFill(t *testing.T, container *html.Node, styles map[*html.Node]*css.Style) *PhysicalFragment {
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
	ctx := testContext()
	root := buildTestTree(mc, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, root, space)
	if result == nil || result.Fragment == nil {
		t.Fatal("multicol layout returned nil")
	}
	dumpFragmentTree(t, "", result.Fragment)
	if len(result.Fragment.Children) != 2 {
		t.Fatalf("multicol has %d columns, want 2", len(result.Fragment.Children))
	}
	return result.Fragment
}

// TestBreakBeforeChild_PaddedContainerOffset (LOU-385 item 1): a child's
// fragmentainer offset must include its container's block-start
// border/scrollbar/padding.
//
// Column: 100px tall. Container: padding-top 20px. Children: a 30px block,
// then a 60px break-inside:avoid block. The avoid block starts at
// fragmentainer offset 20+30 = 50, leaving 50px — it does not fit, so it is
// pushed whole to column 2 (Chrome renders it there). With the container's
// padding omitted, louis14 believes 70px remain, neither pushes nor slices
// the box, and the column overflows by 10px while column 2 stays empty.
//
// Blink: the child's fragmentainer offset is derived from BFC offsets, which
// include the container's border/padding —
// `FragmentainerOffsetAtBfc(container_builder_) + bfc_block_offset`
// (block_layout_algorithm.cc:3156-3158 @ a9f50e522efa) where
// `bfc_block_offset` is past the container's BorderScrollbarPadding().
func TestBreakBeforeChild_PaddedContainerOffset(t *testing.T) {
	first := makeNode("div")
	avoid := makeNode("div")
	container := makeNode("div", first, avoid)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block", "padding-top", "20px"),
		first:     makeStyle("display", "block", "height", "30px"),
		avoid:     makeStyle("display", "block", "height", "60px", "break-inside", "avoid"),
	}
	mcFrag := layoutTwoColumnAutoFill(t, container, styles)

	col1, col2 := mcFrag.Children[0].Fragment, mcFrag.Children[1].Fragment
	if len(col1.Children) != 1 {
		t.Fatalf("column 1 has %d children, want 1 (the container)", len(col1.Children))
	}
	cont1 := col1.Children[0].Fragment
	if n := len(cont1.Children); n != 1 {
		t.Errorf("container in column 1 has %d children, want 1 (the 30px block only; the avoid box must be pushed)", n)
	}
	if len(col2.Children) != 1 {
		t.Fatalf("column 2 has %d children, want 1 (the container's continuation holding the avoid box)", len(col2.Children))
	}
	cont2 := col2.Children[0].Fragment
	if n := len(cont2.Children); n != 1 {
		t.Fatalf("container continuation in column 2 has %d children, want 1 (the avoid box)", n)
	}
	got := cont2.Children[0]
	if top := got.Offset.Top.Float64(); top != 0 {
		t.Errorf("avoid box block offset in column 2 = %v, want 0 (no padding-top on a continuation)", top)
	}
	if h := got.Fragment.Size.HeightF64(); h != 60 {
		t.Errorf("avoid box height in column 2 = %v, want 60 (whole, unsliced)", h)
	}
}

// TestClearedChild_FragmentsAtColumnBoundary (LOU-385 item 2): a new-FC
// child that is re-laid out after being dropped below a float it cannot fit
// beside must keep its fragmentation context, so it breaks at the column
// boundary exactly like the non-dropped path.
//
// Column: 100px tall, 50px wide. Container children: a 50x50 left float,
// then a flow-root block with width:max-content whose content is 80px wide
// and 150px tall. The pre-layout inline-size check cannot resolve
// max-content, so the block is first laid out beside the float, found too
// wide (80 > 50 remaining), and re-laid out below the float at block offset
// 50. That re-layout must see 50px of fragmentainer space left: the block's
// column-1 fragment is 50px tall, and the remaining 100px
// resume in column 2.
//
// Blink: LayoutNewFormattingContext re-lays the child for each layout
// opportunity through CreateConstraintSpaceForChild with the opportunity's
// block offset, which always goes through SetupSpaceBuilderForFragmentation
// (block_layout_algorithm.cc:2158-2163, :3620-3633 @ a9f50e522efa).
func TestClearedChild_FragmentsAtColumnBoundary(t *testing.T) {
	float := makeNode("div")
	inner := makeNode("div")
	dropped := makeNode("div", inner)
	container := makeNode("div", float, dropped)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block"),
		float:     makeStyle("display", "block", "float", "left", "width", "50px", "height", "50px"),
		dropped:   makeStyle("display", "flow-root", "width", "max-content"),
		inner:     makeStyle("display", "block", "width", "80px", "height", "150px"),
	}
	mcFrag := layoutTwoColumnAutoFill(t, container, styles)

	col1, col2 := mcFrag.Children[0].Fragment, mcFrag.Children[1].Fragment
	if len(col1.Children) != 1 {
		t.Fatalf("column 1 has %d children, want 1 (the container)", len(col1.Children))
	}
	cont1 := col1.Children[0].Fragment
	var droppedLink *ChildLink
	for i := range cont1.Children {
		if cont1.Children[i].Fragment.Node == dropped {
			droppedLink = &cont1.Children[i]
		}
	}
	if droppedLink == nil {
		t.Fatal("dropped flow-root block not found in the container's column-1 fragment")
	}
	if top := droppedLink.Offset.Top.Float64(); top != 50 {
		t.Errorf("dropped block offset in column 1 = %v, want 50 (below the float)", top)
	}
	if h := droppedLink.Fragment.Size.HeightF64(); h != 50 {
		t.Errorf("dropped block height in column 1 = %v, want 50 (sliced at the column boundary)", h)
	}
	if len(col2.Children) != 1 {
		t.Fatalf("column 2 has %d children, want 1 (the container's continuation)", len(col2.Children))
	}
	cont2 := col2.Children[0].Fragment
	if n := len(cont2.Children); n != 1 || cont2.Children[0].Fragment.Node != dropped {
		t.Fatalf("container continuation in column 2 has %d children, want the dropped block's continuation", n)
	}
	if h := cont2.Children[0].Fragment.Size.HeightF64(); h != 100 {
		t.Errorf("dropped block continuation height in column 2 = %v, want 100", h)
	}
}
