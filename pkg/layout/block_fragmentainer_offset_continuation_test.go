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
//
// louis14's BLA still repeats the padding when sizing the continuation
// (20 + line > 100), so the multicol adds an overflow column after column 2;
// only placement is asserted here — the line appears exactly once, in the
// continuation — not the column count.
func TestPaddedContainer_ContinuationRefusesBreakBeforeFirstLine(t *testing.T) {
	ib := makeNode("span")
	container := makeNode("div", ib)
	mc := makeNode("div", container)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle(
			"display", "block",
			"width", "100px",
			"height", "100px",
			"column-count", "2",
			"column-gap", "0",
			"column-fill", "auto",
		),
		container: makeStyle("display", "block", "padding-top", "20px"),
		ib:        makeStyle("display", "inline-block", "width", "10px", "height", "90px"),
	}
	result := layoutMulticolForTest(t, mc, styles)
	cols := result.Fragment.Children
	if len(cols) < 2 {
		t.Fatalf("multicol has %d columns, want at least 2", len(cols))
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
