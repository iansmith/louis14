package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// Tests for multicol container intrinsic min/max inline sizes (LOU-364).
// The expected values mirror Blink's ColumnLayoutAlgorithm::ComputeMinMaxSizes
// (column_layout_algorithm.cc:433-498 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//  1. min/max of one column's content, spanners skipped;
//  2. column-width non-auto: min = min(min, column-width), max = max(max, column-width);
//  3. min ×= column-count + gaps only when column-width is auto;
//     max ×= column-count + gaps always (auto count = 1);
//  4. encompass column-span:all descendants' contributions.

// multicolMinMax builds a multicol container with the given container style
// and children (style pairs), and returns ComputeMinMaxSizes for it.
func multicolMinMax(t *testing.T, containerStyle *css.Style, children ...*css.Style) MinMaxSizes {
	t.Helper()
	childNodes := make([]*html.Node, len(children))
	styles := map[*html.Node]*css.Style{}
	for i, cs := range children {
		childNodes[i] = makeNode("div")
		styles[childNodes[i]] = cs
	}
	container := makeNode("div", childNodes...)
	styles[container] = containerStyle

	ctx := testContext()
	layoutRoot := buildTestTree(container, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()
	return ComputeMinMaxSizes(ctx, layoutRoot, space)
}

// Auto column-width: both min and max multiply by column-count, plus gaps.
// cla.cc:473-488. Shape of WPT intrinsic-size-001 (columns:2; column-gap:0;
// 20px child → 40px wide).
func TestMulticolMinMax_AutoColumnWidthMultipliesCountAndGaps(t *testing.T) {
	mm := multicolMinMax(t,
		makeStyle("display", "block", "column-count", "2", "column-gap", "10px"),
		makeStyle("display", "block", "width", "20px"),
	)
	if mm.MinContent != 50 {
		t.Errorf("MinContent: got %.2f, want 50 (20×2 + 10 gap)", mm.MinContent)
	}
	if mm.MaxContent != 50 {
		t.Errorf("MaxContent: got %.2f, want 50 (20×2 + 10 gap)", mm.MaxContent)
	}
}

// Non-auto column-width may SHRINK min-content below the content's own
// min-content requirement, and column-count multiplication is skipped for
// min (cla.cc:458-471, 480-486; 2016 css-sizing-3 WD #multicol-intrinsic).
func TestMulticolMinMax_ColumnWidthShrinksMinBelowContent(t *testing.T) {
	mm := multicolMinMax(t,
		makeStyle("display", "block", "column-width", "10px", "column-gap", "0"),
		makeStyle("display", "block", "width", "20px"),
	)
	if mm.MinContent != 10 {
		t.Errorf("MinContent: got %.2f, want 10 (min(content 20, column-width 10))", mm.MinContent)
	}
	if mm.MaxContent != 20 {
		t.Errorf("MaxContent: got %.2f, want 20 (max(content 20, column-width 10) × auto count 1)", mm.MaxContent)
	}
}

// Non-auto column-width raises max-content when wider than the content
// (cla.cc:466-470). Shape of WPT multicol-width-005 case 2.
func TestMulticolMinMax_ColumnWidthRaisesMax(t *testing.T) {
	mm := multicolMinMax(t,
		makeStyle("display", "block", "column-width", "100px", "column-gap", "0"),
		makeStyle("display", "block", "width", "20px"),
	)
	if mm.MinContent != 20 {
		t.Errorf("MinContent: got %.2f, want 20 (min(content 20, column-width 100))", mm.MinContent)
	}
	if mm.MaxContent != 100 {
		t.Errorf("MaxContent: got %.2f, want 100 (max(content 20, column-width 100))", mm.MaxContent)
	}
}

// column-span:all children are excluded from the column content measurement
// (so they don't get multiplied by column-count); block_layout_algorithm.cc
// :418-419 @ a9f50e522efa. Their own contribution is encompassed separately.
func TestMulticolMinMax_SpannerSkippedFromColumnMultiplication(t *testing.T) {
	mm := multicolMinMax(t,
		makeStyle("display", "block", "column-count", "2", "column-gap", "0"),
		makeStyle("display", "block", "width", "20px"),
		makeStyle("display", "block", "column-span", "all", "width", "30px"),
	)
	// Column content = 20 (spanner skipped) ×2 = 40; encompass spanner 30 → 40.
	// If the spanner leaked into the column content: max(20,30)=30 ×2 = 60.
	if mm.MinContent != 40 {
		t.Errorf("MinContent: got %.2f, want 40 (20×2, spanner excluded from multiplication)", mm.MinContent)
	}
	if mm.MaxContent != 40 {
		t.Errorf("MaxContent: got %.2f, want 40 (20×2, spanner excluded from multiplication)", mm.MaxContent)
	}
}

// Spanner contributions are encompassed AFTER the column-width/count rules
// (cla.cc:490-494 + ComputeSpannersMinMaxSizes cla.cc:523-549): a spanner
// wider than the shrunken min raises it.
func TestMulticolMinMax_SpannerEncompassRaisesMin(t *testing.T) {
	mm := multicolMinMax(t,
		makeStyle("display", "block", "column-width", "10px", "column-gap", "0"),
		makeStyle("display", "block", "width", "20px"),
		makeStyle("display", "block", "column-span", "all", "width", "15px"),
	)
	// min = min(content 20, column-width 10) = 10 → encompass spanner 15 → 15.
	if mm.MinContent != 15 {
		t.Errorf("MinContent: got %.2f, want 15 (column min 10 encompassed by spanner 15)", mm.MinContent)
	}
	// max = max(content 20, column-width 10) = 20 → encompass spanner 15 → 20.
	if mm.MaxContent != 20 {
		t.Errorf("MaxContent: got %.2f, want 20", mm.MaxContent)
	}
}

// A column-span:all descendant inside a new-formatting-context child is NOT
// a valid spanner (it becomes regular column content): the spanner walk skips
// new-FC children (cla.cc:536-540). Shape of WPT intrinsic-size-004
// (columns:4; flow-root wrapper > column-span:all > 25px child → 100px wide).
func TestMulticolMinMax_SpannerInsideNewFCIsColumnContent(t *testing.T) {
	inner := makeNode("div")
	invalidSpanner := makeNode("div", inner)
	flowRoot := makeNode("div", invalidSpanner)
	container := makeNode("div", flowRoot)
	styles := map[*html.Node]*css.Style{
		container:      makeStyle("display", "block", "column-count", "4", "column-gap", "0"),
		flowRoot:       makeStyle("display", "flow-root"),
		invalidSpanner: makeStyle("display", "block", "column-span", "all"),
		inner:          makeStyle("display", "block", "width", "25px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(container, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()
	mm := ComputeMinMaxSizes(ctx, layoutRoot, space)

	// The flow-root wrapper is regular column content (25px min/max) and its
	// column-span:all descendant must not be encompassed: 25×4 = 100.
	if mm.MinContent != 100 {
		t.Errorf("MinContent: got %.2f, want 100 (25×4, no spanner encompass through new FC)", mm.MinContent)
	}
	if mm.MaxContent != 100 {
		t.Errorf("MaxContent: got %.2f, want 100 (25×4, no spanner encompass through new FC)", mm.MaxContent)
	}
}

// Percentage column-gap resolves against a zero base during intrinsic sizing
// (cla.cc:477 ResolveColumnGapForMulticol(Style(), LayoutUnit()) → 0).
func TestMulticolMinMax_PercentGapResolvesToZero(t *testing.T) {
	mm := multicolMinMax(t,
		makeStyle("display", "block", "column-count", "2", "column-gap", "50%"),
		makeStyle("display", "block", "width", "20px"),
	)
	if mm.MinContent != 40 {
		t.Errorf("MinContent: got %.2f, want 40 (20×2 + percent gap resolved to 0)", mm.MinContent)
	}
	if mm.MaxContent != 40 {
		t.Errorf("MaxContent: got %.2f, want 40 (20×2 + percent gap resolved to 0)", mm.MaxContent)
	}
}
