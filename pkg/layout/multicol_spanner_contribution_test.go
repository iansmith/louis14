package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-411 (plan WI-6, G14): the block-size a line of columns contributes.
// Blink @ a9f50e522efa:
//   - column-fill:balance balances also under column-height; the used
//     column-height only caps the balanced size (ColumnLayoutAlgorithm::
//     LayoutLine, column_layout_algorithm.cc:840-856; ConstrainColumnBlockSize
//     :1813-1816).
//   - a line contributes its column block-size (:949), shrunk to the first
//     column's BlockSizeForFragmentation when shrink-to-fit (:953-971).
//   - a line is empty when its column block-size is 0, it has one column, and
//     that column has no children or only an empty spanner parent
//     (:1218-1231); only a non-empty line moves the layout position to
//     line_offset + contribution and resets the margin strut (:1233-1251).
// Expected geometry is Chrome 155 headless (probe files in the LOU-411
// design scratchpad: probe.html, probe2.html, probe4.html).

// multicolHeight returns the multicol fragment's border-box block-size.
func multicolHeight(frag *PhysicalFragment) float64 { return frag.Size.HeightF64() }

func checkMulticolHeight(t *testing.T, frag *PhysicalFragment, want float64) {
	t.Helper()
	if got := multicolHeight(frag); got != want {
		t.Errorf("multicol height = %v, want %v (Chrome 155)", got, want)
	}
}

// TestColumnHeightBalancesUnderColumnFillBalance_LOU411: column-height:40px
// with column-fill:balance balances a 30px block into two 15px pieces; with
// column-fill:auto it fills the first column. The row is 40px either way.
// probe4.html K, K2.
func TestColumnHeightBalancesUnderColumnFillBalance_LOU411(t *testing.T) {
	mc := props("column-count", "2", "column-gap", "0", "width", "200px", "column-height", "40px")
	t.Run("balance", func(t *testing.T) {
		frag, ids := layoutColumnsSpec(t, mc, ms("a", props("height", "30px")))
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"a": {{0, 0, 100, 15}, {100, 0, 100, 15}},
		})
		checkMulticolHeight(t, frag, 40)
	})
	t.Run("auto", func(t *testing.T) {
		frag, ids := layoutColumnsSpec(t, append(mc, "column-fill", "auto"), ms("a", props("height", "30px")))
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"a": {{0, 0, 100, 30}},
		})
		checkMulticolHeight(t, frag, 40)
	})
}

// emptySpannerParentFixture is [w[s1 10px margin-bottom:20px, s2 10px
// margin-top:30px], b 20px]: the line between the two spanners holds only
// w's empty piece, so it is empty and s1's bottom margin collapses through
// it with s2's top margin (30px gap).
func emptySpannerParentFixture() []marginSpec {
	return []marginSpec{
		ms("w", nil,
			ms("s1", props("column-span", "all", "height", "10px", "margin-bottom", "20px")),
			ms("s2", props("column-span", "all", "height", "10px", "margin-top", "30px")),
		),
		ms("b", props("height", "20px")),
	}
}

// TestEmptySpannerParentLineUnderColumnHeight_LOU411 (DoD D3 ii): under
// column-height the empty spanner-parent line balances to 0 and stays empty,
// so the margins collapse through it. probe4.html E6 (balance) and E7
// (column-fill:auto: post-spanner content fills, it is not balanced).
func TestEmptySpannerParentLineUnderColumnHeight_LOU411(t *testing.T) {
	mc := props("column-count", "2", "column-gap", "0", "width", "200px", "column-height", "200px")
	t.Run("balance", func(t *testing.T) {
		frag, ids := layoutColumnsSpec(t, mc, emptySpannerParentFixture()...)
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"s1": {{0, 0, 200, 10}},
			"s2": {{0, 40, 200, 10}},
			"b":  {{0, 50, 100, 10}, {100, 50, 100, 10}},
		})
		checkMulticolHeight(t, frag, 200)
	})
	t.Run("auto", func(t *testing.T) {
		frag, ids := layoutColumnsSpec(t, append(mc, "column-fill", "auto"), emptySpannerParentFixture()...)
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"s1": {{0, 0, 200, 10}},
			"s2": {{0, 40, 200, 10}},
			"b":  {{0, 50, 100, 20}},
		})
		checkMulticolHeight(t, frag, 200)
	})
}

// TestEmptySpannerParentLineCollapsesMargins_LOU411 (DoD D3 ii, guard):
// balanced auto-height columns. probe2.html E3 (both spanners in w) and
// probe.html E (s1 before w: w's leading piece holds nothing before s2 —
// IsEmptySpannerParent — so that line is empty too).
func TestEmptySpannerParentLineCollapsesMargins_LOU411(t *testing.T) {
	mc := props("column-count", "2", "column-gap", "0", "width", "200px")
	t.Run("E3", func(t *testing.T) {
		frag, ids := layoutColumnsSpec(t, mc, emptySpannerParentFixture()...)
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"s1": {{0, 0, 200, 10}},
			"s2": {{0, 40, 200, 10}},
			"b":  {{0, 50, 100, 10}, {100, 50, 100, 10}},
		})
		checkMulticolHeight(t, frag, 60)
	})
	t.Run("E", func(t *testing.T) {
		frag, ids := layoutColumnsSpec(t, mc,
			ms("s1", props("column-span", "all", "height", "10px", "margin-bottom", "20px")),
			ms("w", nil,
				ms("s2", props("column-span", "all", "height", "10px", "margin-top", "30px")),
				ms("b", props("height", "20px")),
			),
		)
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"s1": {{0, 0, 200, 10}},
			"s2": {{0, 60, 200, 10}},
			"b":  {{0, 70, 100, 10}, {100, 70, 100, 10}},
		})
		checkMulticolHeight(t, frag, 80)
	})
}

// TestSpannerAfterParallelFlowOverflowLine_LOU411 (DoD D3 i): the line
// before the spanner holds only content overflowing a height:0 box
// (spanner-in-child-after-parallel-flow-004's shape). Its columns balance to
// the overflow (BlockSizeForFragmentation, column_layout_algorithm.cc:1652-
// 1672), so the line contributes 90px and the spanner sits below it.
// probe.html D.
func TestSpannerAfterParallelFlowOverflowLine_LOU411(t *testing.T) {
	p1 := makeNode("div")
	p2 := makeNode("div")
	z := makeNode("div", p1, p2)
	spanner := makeNode("div")
	span := makeNode("span", spanner)
	mc := makeNode("div", z, span)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2", "width", "100px", "column-gap", "0",
			"orphans", "1", "widows", "1"),
		z:       makeStyle("display", "block", "height", "0"),
		p1:      makeStyle("display", "block", "height", "90px"),
		p2:      makeStyle("display", "block", "height", "90px", "break-before", "column"),
		span:    makeStyle("display", "inline"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "10px"),
	}
	frag := layoutMulticolForTest(t, mc, styles).Fragment
	checkRects(t, borderBoxRectsByID(frag, map[*html.Node]string{spanner: "s"}), map[string][]fragRect{
		"s": {{0, 90, 100, 10}},
	})
	checkMulticolHeight(t, frag, 100)
}

// TestSpannerRowContributionIgnoresMonolithicOverflow_LOU411: a
// column-fill:auto line ending at a spanner contributes the used column
// block-size (50px), not the monolithic overflow of its content (80px):
// Blink's contribution is column_size.block_size
// (column_layout_algorithm.cc:949 @ a9f50e522efa). probe2.html G4 (only the
// spanner and the multicol are asserted; Chrome spreads the post-spanner
// block over 1px columns).
func TestSpannerRowContributionIgnoresMonolithicOverflow_LOU411(t *testing.T) {
	frag, ids := layoutColumnsSpec(t,
		props("column-count", "2", "column-gap", "0", "width", "200px", "height", "50px", "column-fill", "auto"),
		ms("z", props("height", "0"), ms("m", props("height", "80px", "contain", "size", "width", "40px"))),
		ms("s", props("column-span", "all", "height", "10px")),
		ms("b", props("height", "20px")),
	)
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"s": {{0, 50, 200, 10}},
	})
	checkMulticolHeight(t, frag, 50)
}
