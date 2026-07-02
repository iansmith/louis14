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
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	mcNode := buildTestTree(mc, styles)
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		Build()
	result := NewMulticolLayoutAlgorithm(ctx, mcNode, space).Layout()
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
