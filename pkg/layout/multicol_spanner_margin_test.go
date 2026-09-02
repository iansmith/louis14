package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// layoutMulticolForTest runs MulticolLayoutAlgorithm on a multicol built from
// the given DOM + styles at a 400px-wide auto-height available size and
// returns the layout result.
func layoutMulticolForTest(t *testing.T, mc *html.Node, styles map[*html.Node]*css.Style) *LayoutResult {
	t.Helper()
	return layoutMulticolInFragmentainer(t, mc, styles, Indefinite, 0)
}

// layoutMulticolInFragmentainer runs MulticolLayoutAlgorithm on a multicol at a
// 400px-wide auto-height available size, optionally nested inside an OUTER
// fragmentation context of block-size fragBlockSize starting at fragOffset. A
// fragBlockSize of Indefinite leaves outer fragmentation off (hasOuterFrag
// false), matching a top-level auto-height multicol.
func layoutMulticolInFragmentainer(t *testing.T, mc *html.Node, styles map[*html.Node]*css.Style, fragBlockSize, fragOffset float64) *LayoutResult {
	t.Helper()
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	mcNode := buildTestTree(mc, styles)
	b := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite})
	if fragBlockSize != Indefinite {
		b = b.SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(fragBlockSize).
			SetFragmentainerOffset(fragOffset).
			SetBlockFragmentationType(FragmentColumn)
	}
	result := NewMulticolLayoutAlgorithm(ctx, mcNode, b.Build()).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	for i, ch := range result.Fragment.Children {
		t.Logf("child[%d] boxType=%v offset=(%v,%v) size=%vx%v",
			i, ch.Fragment.BoxType, ch.Offset.Left.Float64(), ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64())
	}
	return result
}

// spannerChildren returns the non-column children (spanner fragments) of a
// multicol fragment in document order.
func spannerChildren(result *LayoutResult) []ChildLink {
	var spanners []ChildLink
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType != BoxTypeColumn {
			spanners = append(spanners, ch)
		}
	}
	return spanners
}

// LOU-363 defect 1: adjacent spanners' block margins must COLLAPSE, not sum.
//
// Blink threads a single MarginStrut through the child walk
// (cla.cc:551-708 @ a9f50e522efa): a spanner's block-start margin is appended
// to the strut holding the previous spanner's block-end margin
// (cla.cc:1345-1347), and the margin collapses through the empty column line
// between the two spanners (cla.cc:717-724, 1245-1251: the strut is only
// reset when a line places content).
//
// Mirrors css-multicol/remove-inline-with-block-beside-spanners and
// non-adjacent-spanners-000/001.
//
// Bug: multicol_layout.go adds spanner margins as raw cursor advances
// (blockCursor += BlockStart / BlockEnd), so 100px + 60px stack to 160px
// between the spanners instead of collapsing to max(100,60) = 100px.
func TestSpannerMarginStrut_AdjacentSpannersCollapse_LOU363(t *testing.T) {
	spanner1 := makeNode("div")
	spanner2 := makeNode("div")
	mc := makeNode("div", spanner1, spanner2)

	styles := map[*html.Node]*css.Style{
		mc:       makeStyle("display", "block", "column-count", "2", "column-gap", "0", "width", "400px"),
		spanner1: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-bottom", "100px"),
		spanner2: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-top", "60px"),
	}

	result := layoutMulticolForTest(t, mc, styles)
	spanners := spannerChildren(result)
	if len(spanners) != 2 {
		t.Fatalf("expected 2 spanner fragments, got %d", len(spanners))
	}
	if got := spanners[0].Offset.Top.Float64(); got != 0 {
		t.Errorf("spanner1 at y=%v, want y=0", got)
	}
	// spanner1 bottom (10) + collapsed margin max(100,60)=100.
	if got := spanners[1].Offset.Top.Float64(); got != 110 {
		t.Errorf("spanner2 at y=%v, want y=110 (margins collapse to max(100,60)=100)", got)
	}
	if got := result.Fragment.Size.HeightF64(); got != 120 {
		t.Errorf("multicol block-size = %v, want 120 (10+100+10)", got)
	}
}

// LOU-363 defect 1 (non-collapse guard): a spanner's trailing margin does NOT
// collapse with a following spanner's leading margin when a non-empty column
// line sits between them — and a line containing a FRESH parent block that
// wraps a nested spanner is non-empty.
//
// Blink: is_empty (cla.cc:1218-1231 @ a9f50e522efa) requires the single
// column fragment to have no children (or be an "empty spanner parent",
// which bla.cc:1050-1055 sets only when RESUMING with no children). A fresh
// div wrapping a spanner leaves its zero-height box in the column, so the
// line places content, the strut resets, and the two spanner margins stack.
//
// Mirrors css-multicol/multicol-span-all-margin-nested-001 (its assert:
// "the bottom margin of the first h4 element should not collapse with the
// top margin of div#child").
func TestSpannerMarginStrut_NoCollapseThroughFreshSpannerParent_LOU363(t *testing.T) {
	spanner1 := makeNode("div")
	spanner2 := makeNode("div")
	wrapper := makeNode("div", spanner2)
	mc := makeNode("div", spanner1, wrapper)

	styles := map[*html.Node]*css.Style{
		mc:       makeStyle("display", "block", "column-count", "2", "column-gap", "0", "width", "400px"),
		spanner1: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-bottom", "100px"),
		wrapper:  makeStyle("display", "block"),
		spanner2: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-top", "60px"),
	}

	result := layoutMulticolForTest(t, mc, styles)
	spanners := spannerChildren(result)
	if len(spanners) != 2 {
		t.Fatalf("expected 2 spanner fragments, got %d", len(spanners))
	}
	if got := spanners[0].Offset.Top.Float64(); got != 0 {
		t.Errorf("spanner1 at y=%v, want y=0", got)
	}
	// spanner1 bottom (10) + margin-bottom 100 (the fresh-wrapper line resets
	// the strut) + spanner2 margin-top 60.
	if got := spanners[1].Offset.Top.Float64(); got != 170 {
		t.Errorf("spanner2 at y=%v, want y=170 (margins must NOT collapse across the fresh wrapper line)", got)
	}
	if got := result.Fragment.Size.HeightF64(); got != 180 {
		t.Errorf("multicol block-size = %v, want 180 (10+100+60+10)", got)
	}
}

// LOU-363 defect 1 (empty spanner parent): two spanners nested inside the
// SAME wrapper block still collapse their margins — the line between them
// contains only the resumed wrapper's empty continuation box.
//
// Blink: is_empty_spanner_parent (bla.cc:1050-1055 @ a9f50e522efa) is set
// when the block directly containing the spanner was RESUMING with no
// children placed; box_fragment_builder.cc:596-605 propagates the flag up to
// the column result, and cla.cc:1229-1231 accepts the line as empty despite
// the continuation box child, so the strut survives and the margins collapse.
//
// Mirrors css-multicol/multicol-span-all-margin-003 (chromium bug 1052834).
func TestSpannerMarginStrut_CollapseThroughResumedSpannerParent_LOU363(t *testing.T) {
	spanner1 := makeNode("div")
	spanner2 := makeNode("div")
	wrapper := makeNode("div", spanner1, spanner2)
	mc := makeNode("div", wrapper)

	styles := map[*html.Node]*css.Style{
		mc:       makeStyle("display", "block", "column-count", "3", "column-gap", "0", "width", "400px"),
		wrapper:  makeStyle("display", "block"),
		spanner1: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-bottom", "50px"),
		spanner2: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-top", "80px"),
	}

	result := layoutMulticolForTest(t, mc, styles)
	spanners := spannerChildren(result)
	if len(spanners) != 2 {
		t.Fatalf("expected 2 spanner fragments, got %d", len(spanners))
	}
	if got := spanners[0].Offset.Top.Float64(); got != 0 {
		t.Errorf("spanner1 at y=%v, want y=0", got)
	}
	// spanner1 bottom (10) + collapsed margin max(50,80)=80: the line between
	// them holds only the resumed wrapper's empty continuation.
	if got := spanners[1].Offset.Top.Float64(); got != 90 {
		t.Errorf("spanner2 at y=%v, want y=90 (margins collapse to max(50,80)=80 through the resumed wrapper)", got)
	}
	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("multicol block-size = %v, want 100 (10+80+10)", got)
	}
}

// LOU-363 defect 1 (completion path): a trailing spanner's block-end margin
// contributes to the multicol's intrinsic block-size.
//
// Blink: LayoutSpanner leaves the block-end margin in the strut
// (cla.cc:1436-1437 @ a9f50e522efa) and LayoutChildren adds the leftover
// strut to intrinsic_block_size_ once all children are seen (cla.cc:704).
// Guards the completion-path strut flush across the strut rewrite.
func TestSpannerMarginStrut_TrailingSpannerMargin_LOU363(t *testing.T) {
	block1 := makeNode("div")
	spanner := makeNode("div")
	mc := makeNode("div", block1, spanner)

	styles := map[*html.Node]*css.Style{
		mc:      makeStyle("display", "block", "column-count", "2", "column-gap", "0", "width", "400px"),
		block1:  makeStyle("display", "block", "height", "100px"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-top", "20px", "margin-bottom", "30px"),
	}

	result := layoutMulticolForTest(t, mc, styles)
	spanners := spannerChildren(result)
	if len(spanners) != 1 {
		t.Fatalf("expected 1 spanner fragment, got %d", len(spanners))
	}
	// block1 balances into 2 columns of 50px; spanner below at 50+20.
	if got := spanners[0].Offset.Top.Float64(); got != 70 {
		t.Errorf("spanner at y=%v, want y=70 (50px column row + 20px margin-top)", got)
	}
	if got := result.Fragment.Size.HeightF64(); got != 110 {
		t.Errorf("multicol block-size = %v, want 110 (50+20+10+30: trailing margin included)", got)
	}
}

// LOU-363 defect 3 / LOU-370 sub-item (b): a post-spanner column row in a
// definite-height multicol gets only the REMAINING height budget, not the full
// container height — and content that doesn't fit the budgeted columns
// overflows into extra columns along the inline axis instead of being dropped.
//
// Blink: LayoutLine starts the column block-size from
// remaining_content_block_size_ minus CurrentContentBlockOffset(line_offset)
// (cla.cc:805-816 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// ConstrainColumnBlockSize subtracts the break-token consumed size and the
// line offset from the specified-height cap (cla.cc:1774-1809); and
// ColumnsOverflowInInlineDirection permits overflow columns when not nested
// (cla.cc:1823-1842, loop condition :1022-1025).
//
// Mirrors css-multicol/multicol-span-all-children-height-002 (percentage
// heights replaced with the resolved px values).
func TestPostSpannerRowBudget_LOU363(t *testing.T) {
	block1 := makeNode("div")
	spanner := makeNode("div")
	block2 := makeNode("div")
	mc := makeNode("div", block1, spanner, block2)

	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
			"width", "400px", "height", "200px"),
		block1:  makeStyle("display", "block", "height", "200px"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "50px"),
		block2:  makeStyle("display", "block", "height", "200px"),
	}

	result := layoutMulticolForTest(t, mc, styles)

	// Container height stays at the declared 200px; pre-fix the post-spanner
	// row got the full 200px budget (100px balanced columns), inflating the
	// fragment to 250px.
	if got := result.Fragment.Size.HeightF64(); got != 200 {
		t.Errorf("multicol block-size = %v, want 200 (declared height)", got)
	}

	spanners := spannerChildren(result)
	if len(spanners) != 1 {
		t.Fatalf("expected 1 spanner fragment, got %d", len(spanners))
	}
	if got := spanners[0].Offset.Top.Float64(); got != 100 {
		t.Errorf("spanner at y=%v, want y=100 (block1 balanced into 2x100px columns)", got)
	}

	// Post-spanner row: budget = 200 - (100+50) = 50px per column; block2's
	// 200px fragments into 4 columns of 50px (2 overflowing the inline axis).
	var postCols []ChildLink
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType == BoxTypeColumn && ch.Offset.Top.Float64() == 150 {
			postCols = append(postCols, ch)
		}
	}
	if len(postCols) != 4 {
		t.Fatalf("expected 4 post-spanner columns (2 overflow), got %d", len(postCols))
	}
	for i, col := range postCols {
		if got := col.Fragment.Size.HeightF64(); got != 50 {
			t.Errorf("post-spanner column %d height = %v, want 50 (remaining budget)", i, got)
		}
		if want := float64(i) * 200; col.Offset.Left.Float64() != want {
			t.Errorf("post-spanner column %d at x=%v, want %v", i, col.Offset.Left.Float64(), want)
		}
	}
}

// LOU-363 defect 2: spanner inline margins must be resolved and used for the
// spanner's inline offset — including `auto` centering.
//
// Blink: LayoutSpanner resolves inline auto margins against the multicol's
// content-box inline size (ResolveInlineAutoMargins, length_utils.cc:1551-1570
// @ a9f50e522efa) and places the spanner at
// BorderScrollbarPadding().inline_start + margins.inline_start
// (cla.cc:1420-1426).
//
// Bug: multicol_layout.go pins spannerOffset.InlineOffset to 0.
func TestSpannerInlineMargins_LOU363(t *testing.T) {
	tests := []struct {
		name     string
		style    *css.Style
		wantLeft float64
	}{
		{
			name: "explicit margin-left",
			style: makeStyle("display", "block", "column-span", "all",
				"width", "100px", "height", "10px", "margin-left", "30px"),
			wantLeft: 30,
		},
		{
			name: "auto margins center",
			style: makeStyle("display", "block", "column-span", "all",
				"width", "100px", "height", "10px", "margin-left", "auto", "margin-right", "auto"),
			wantLeft: 150,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spanner := makeNode("div")
			mc := makeNode("div", spanner)
			styles := map[*html.Node]*css.Style{
				mc:      makeStyle("display", "block", "column-count", "2", "column-gap", "0", "width", "400px"),
				spanner: tc.style,
			}
			result := layoutMulticolForTest(t, mc, styles)
			spanners := spannerChildren(result)
			if len(spanners) != 1 {
				t.Fatalf("expected 1 spanner fragment, got %d", len(spanners))
			}
			if got := spanners[0].Offset.Left.Float64(); got != tc.wantLeft {
				t.Errorf("spanner inline offset = %v, want %v", got, tc.wantLeft)
			}
		})
	}
}

// LOU-363 defect 4: position:relative on a multicol container must produce a
// RelativeOffset on its fragment, applied at paint time by engine.go's box
// conversion (engine.go:439-441).
//
// no direct Blink analog inside column_layout_algorithm.cc — Blink applies
// relative offsets generically when the parent adds the child
// (relative_utils.cc ComputeRelativeOffset). Louis14's established local
// pattern is the per-algorithm tail: block_layout.go:2469-2489 (BLA),
// grid_layout.go (grid), flex_layout.go (flex). MulticolLayoutAlgorithm has
// no such tail, so `position:relative` offsets are silently dropped
// (multicol-rule-004 paints at the static position).
func TestMulticolRelativeOffset_LOU363(t *testing.T) {
	tests := []struct {
		name              string
		props             []string
		wantLeft, wantTop float64
	}{
		{
			name:     "left/top",
			props:    []string{"position", "relative", "left", "30px", "top", "40px"},
			wantLeft: 30, wantTop: 40,
		},
		{
			// Mirrors multicol-rule-004.xht (right:80px; bottom:100px).
			name:     "right/bottom",
			props:    []string{"position", "relative", "right", "80px", "bottom", "100px"},
			wantLeft: -80, wantTop: -100,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block1 := makeNode("div")
			mc := makeNode("div", block1)
			mcProps := append([]string{"display", "block", "column-count", "2",
				"column-gap", "0", "width", "400px"}, tc.props...)
			styles := map[*html.Node]*css.Style{
				mc:     makeStyle(mcProps...),
				block1: makeStyle("display", "block", "height", "100px"),
			}
			result := layoutMulticolForTest(t, mc, styles)
			if got := result.Fragment.RelativeOffset.LeftF64(); got != tc.wantLeft {
				t.Errorf("RelativeOffset.Left = %v, want %v", got, tc.wantLeft)
			}
			if got := result.Fragment.RelativeOffset.TopF64(); got != tc.wantTop {
				t.Errorf("RelativeOffset.Top = %v, want %v", got, tc.wantTop)
			}
		})
	}
}

// LOU-363 (Claude-fallback-review finding): under OUTER fragmentation the
// break-before / fragmentainer-offset sites must include the collapsed margin
// strut, not bare blockCursor.
//
// Blink LayoutSpanner (cla.cc:1347-1356, 1402-1406 @ a9f50e522efa) appends the
// spanner's block-start margin to the strut, then computes
// block_offset = intrinsic_block_size_ + margin_strut->Sum() and uses THAT for
// both the fragmentation break decision (fragmentainer_block_offset =
// FragmentainerOffsetForChildren() + block_offset) and placement. A preceding
// spanner's block-end margin lives in the strut (not baked into blockCursor),
// so a bare-blockCursor check under-counts by the collapsed margin.
//
// Scenario: nested multicol; spanner A (h=10, margin-bottom=100) immediately
// followed by spanner B (h=10) with the outer fragmentainer only 100px tall.
// B's start offset is 10 + 100 = 110 > 100, so it must break BEFORE B and
// resume it in the next outer fragment. Bug: multicol_layout.go:883 tested bare
// blockCursor (10 < 100), so B was placed at y=110, overflowing the outer
// column, and no outer break token was emitted.
func TestSpannerMarginStrut_OuterFragmentationIncludesPendingMargin_LOU363(t *testing.T) {
	spanner1 := makeNode("div")
	spanner2 := makeNode("div")
	mc := makeNode("div", spanner1, spanner2)

	styles := map[*html.Node]*css.Style{
		mc:       makeStyle("display", "block", "column-count", "2", "column-gap", "0", "width", "400px"),
		spanner1: makeStyle("display", "block", "column-span", "all", "height", "10px", "margin-bottom", "100px"),
		spanner2: makeStyle("display", "block", "column-span", "all", "height", "10px"),
	}

	// Outer fragmentainer 100px tall, starting at its origin.
	result := layoutMulticolInFragmentainer(t, mc, styles, 100, 0)
	spanners := spannerChildren(result)
	if got := len(spanners); got != 1 {
		t.Fatalf("expected 1 spanner in the first outer fragment (spanner B must break before to the next outer fragment); got %d", got)
	}
	if got := spanners[0].Offset.Top.Float64(); got != 0 {
		t.Errorf("spanner1 at y=%v, want y=0", got)
	}
	if result.BreakToken == nil {
		t.Error("expected an outer break token deferring spanner B to the next outer fragment (spanner A's 100px block-end margin pushes B's start past the 100px outer boundary)")
	}
}
