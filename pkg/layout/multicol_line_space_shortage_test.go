package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-399: line boxes in column balancing and the trusted child space
// shortage. Blink @ a9f50e522efa:
//   - line boxes are monolithic (PhysicalFragment::IsMonolithic,
//     physical_fragment.cc:586-596), so during the initial column-balancing
//     pass every line floors the column block-size at its own block-size
//     (BreakBeforeChildIfNeeded, fragmentation_utils.cc:1110-1118);
//   - a line that does not fit but is moved past (first in the
//     fragmentainer) reports its space shortage (MovePastBreakpoint,
//     fragmentation_utils.cc:1233-1241 → PropagateSpaceShortage :995-1014);
//   - a parent breaking before a child trusts every space shortage the child
//     reported (CalculateSpaceShortage, fragmentation_utils.cc:1046-1051);
//   - a line of columns contributes its column block-size, also when it ends
//     at a spanner (ColumnLayoutAlgorithm::LayoutLine,
//     column_layout_algorithm.cc:949, :1238).
// Expected geometry is Chrome 155.0.8059.41 headless (LOU-399 scratchpad
// probe.html).

// tallLineBlock is a block holding one 30px line box.
func tallLineBlock() (*html.Node, map[*html.Node]*css.Style) {
	n := makeNode("div", makeTextNode("x"))
	return n, map[*html.Node]*css.Style{n: makeStyle("display", "block", "line-height", "30px")}
}

// TestLineBoxIsUnbreakableInInitialBalancingPass_LOU399: in the initial
// column-balancing pass a line box propagates its block-size as the tallest
// unbreakable block-size, also without atomic inlines.
func TestLineBoxIsUnbreakableInInitialBalancingPass_LOU399(t *testing.T) {
	n, styles := tallLineBlock()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(Indefinite).
		SetIsInitialColumnBalancingPass(true).
		SetBlockFragmentationType(FragmentColumn).
		Build()
	res := layoutElement(testContext(), buildTestTree(n, styles), space)
	if res == nil || res.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	if got := res.TallestUnbreakableBlockSize; got != 30 {
		t.Errorf("TallestUnbreakableBlockSize = %v, want 30 (the line box)", got)
	}
}

// TestMovedPastLineReportsSpaceShortage_LOU399: a 30px line at the start of a
// 10px column is moved past (breaking before it makes no progress) and
// reports a 20px space shortage.
func TestMovedPastLineReportsSpaceShortage_LOU399(t *testing.T) {
	n, styles := tallLineBlock()
	res := layoutBlockInFragmentainer(t, n, styles, 10, 0)
	if res.BreakToken != nil {
		t.Fatalf("block broke inside; want the only line moved past")
	}
	if got := res.MinSpaceShortage; got != 20 {
		t.Errorf("MinSpaceShortage = %v, want 20 (30px line in a 10px column)", got)
	}
}

// TestBreakBeforeTrustsChildSpaceShortage_LOU399 (balance-break-avoidance-002):
// [25][50][25 break-before:avoid][50] in two 50px columns. Chrome: the
// break-before:avoid block stays after the 50px block, so the columns are
// 100px tall.
func TestBreakBeforeTrustsChildSpaceShortage_LOU399(t *testing.T) {
	frag, ids := layoutColumnsSpec(t,
		props("width", "100px", "column-count", "2", "column-gap", "0"),
		ms("a", props("height", "25px")),
		ms("b", props("height", "50px")),
		ms("c", props("height", "25px", "break-before", "avoid")),
		ms("d", props("height", "50px")),
	)
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"a": {{0, 0, 50, 25}},
		"b": {{0, 25, 50, 50}},
		"c": {{0, 75, 50, 25}},
		"d": {{50, 0, 50, 50}},
	})
	checkMulticolHeight(t, frag, 100)
}

// TestSpannerLineContributesColumnBlockSize_LOU399: a 30px line before a
// spanner in three balanced columns. The tallest unbreakable line sizes the
// columns at 30px, so the line ends at the spanner with one 30px column and
// contributes that size; Chrome places the spanner at 30 and the multicol is
// 70px tall.
func TestSpannerLineContributesColumnBlockSize_LOU399(t *testing.T) {
	a := makeNode("div", makeTextNode("x"))
	sp := makeNode("div")
	b := makeNode("div", makeTextNode("y"))
	mc := makeNode("div", a, sp, b)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "width", "400px", "column-count", "3", "column-gap", "0", "line-height", "30px"),
		a:  makeStyle("display", "block", "line-height", "30px"),
		sp: makeStyle("display", "block", "height", "10px", "column-span", "all"),
		b:  makeStyle("display", "block", "line-height", "30px"),
	}
	res := layoutMulticolForTest(t, mc, styles)
	var columns []float64
	spannerTop := -1.0
	for _, ch := range res.Fragment.Children {
		switch {
		case ch.Fragment.BoxType == BoxTypeColumn && ch.Offset.Top.Float64() == 0:
			columns = append(columns, ch.Fragment.Size.HeightF64())
		case ch.Fragment.Node == sp:
			spannerTop = ch.Offset.Top.Float64()
		}
	}
	if len(columns) != 1 || columns[0] != 30 {
		t.Errorf("pre-spanner columns = %v, want one 30px column", columns)
	}
	if spannerTop != 30 {
		t.Errorf("spanner top = %v, want 30 (Chrome)", spannerTop)
	}
	if got := res.Fragment.Size.HeightF64(); got != 70 {
		t.Errorf("multicol height = %v, want 70 (Chrome)", got)
	}
}
