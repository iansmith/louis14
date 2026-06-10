package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// collectTextFragments recursively collects all FragmentText children,
// returning (offsetX, offsetY) for each.
func collectTextFragments(frag *PhysicalFragment, baseX, baseY float64) [][2]float64 {
	var results [][2]float64
	for _, ch := range frag.Children {
		x := baseX + ch.Offset.Left.Float64()
		y := baseY + ch.Offset.Top.Float64()
		if ch.Fragment.Type == FragmentText {
			results = append(results, [2]float64{x, y})
			continue // text fragments have no children to recurse into
		}
		results = append(results, collectTextFragments(ch.Fragment, x, y)...)
	}
	return results
}

// TestGridLayout_JustifyItemsStart_PlainText mirrors the WPT test
// firefox-bug-1881495 for grid 3 (inline-size: 3em = 90px).  The item
// content is plain text "X " followed by a span with margin-right:-1px
// wrapping text "X".  With justify-items:start, ShrinkToFit sizes the item
// to its max-content (< column width), so the span's "X" must wrap to line 2.
// This also validates that the re-layout pass (needed for align-self:stretch)
// preserves the ShrinkToFit inline size rather than reverting to the full
// column width.
func TestGridLayout_JustifyItemsStart_PlainText(t *testing.T) {
	// DOM: grid > item > ("X " , span.negMargin > "X")
	textBefore := makeTextNode("X ")
	textInSpan := makeTextNode("X")
	spanNode := makeNode("span", textInSpan)
	itemDiv := makeNode("div", textBefore, spanNode)
	grid := makeNode("div", itemDiv)

	styles := map[*html.Node]*css.Style{
		grid:     makeStyle("display", "grid", "width", "90px", "justify-items", "start"),
		itemDiv:  makeStyle("font-size", "30px", "line-height", "1"),
		spanNode: makeStyle("display", "inline", "font-size", "30px", "line-height", "1", "margin-right", "-1px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(grid, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	// With justify-items:start, ShrinkToFit sizes the item to max-content
	// (< column width), so the span's "X" exceeds the remaining space and
	// wraps to line 2.  Grid height = 2 lines × 30px = 60px.
	if result.Fragment.Size.HeightF64() != 60 {
		t.Errorf("grid height: got %.1f, want 60 (plain-text case: span X must wrap to line 2)",
			result.Fragment.Size.HeightF64())
	}

	// Verify the X on line 2 is at x=0 within the item (not offset to the right).
	// Both text fragments (line 1 "X" and line 2 "X") should start at x=0
	// within the item (which is at x=0 within the column, x=4 from grid border).
	textFrags := collectTextFragments(result.Fragment, 0, 0)
	// There should be exactly 2 text fragments (one per line).
	if len(textFrags) < 2 {
		t.Fatalf("expected at least 2 text fragments, got %d (fragments: %v)", len(textFrags), textFrags)
	}
	t.Logf("text fragments: %v", textFrags)
	// Both text fragments should be at x=0 (start of line).
	// The grid has no border in this unit test, item is at x=0 in the column,
	// and each line's text starts at x=0 (text-align:start, no indent).
	for i, tf := range textFrags {
		if tf[0] != 0 {
			t.Errorf("text fragment %d: got x=%.4f, want 0.0 (text must start at inline-start of line)",
				i, tf[0])
		}
	}
}

// TestGridLayout_JustifyItemsStart_WrapsAtMaxContent verifies that a grid item
// with justify-self != stretch is sized to its max-content (ShrinkToFit), not
// to the full column track width.
//
// Setup: 90px grid, justify-items:start.
// Item contains:
//   - child1: inline-block 60×30px
//   - wrapperSpan: display:inline, margin-right:-1px, line-height:0
//   - child2: inline-block 30×30px
//
// The wrapperSpan's margin-right:-1px makes the effective advance of the
// (child1 + wrapperSpan{child2}) sequence = 60 + 30 - 1 = 89px.  line-height:0
// on the item and wrapperSpan drives the strut descent to ≤0, so each line
// box height = exactly 30px (the inline-block height).
//
// The line-breaker checks `totalInlineSize > remaining` (strict >).
//
// With the bug (item laid out at column width = 90px):
//
//	position after child1 = 60; remaining = 90-60 = 30; 30 > 30 = FALSE → no break.
//	Both children fit on one line → item height = 30px → grid height = 30px.
//
// With the fix (item laid out at max-content = 89px via ShrinkToFit):
//
//	position after child1 = 60; remaining = 89-60 = 29; 30 > 29 = TRUE → break.
//	child2 wraps to line 2 → item height = 60px → grid height = 60px.
func TestGridLayout_JustifyItemsStart_WrapsAtMaxContent(t *testing.T) {
	child1 := makeNode("span")
	child2 := makeNode("span")
	wrapperSpan := makeNode("span", child2)
	item := makeNode("div", child1, wrapperSpan)
	grid := makeNode("div", item)

	// Notes on style choices:
	//   - wrapperSpan MUST have display:inline so CollectInlines emits it as an
	//     OpenTag/CloseTag pair (inline wrapper), not an AtomicInline (block).
	//     GetDisplay() falls back to DisplayBlock when the property is absent, so
	//     not setting it makes wrapperSpan behave as a block atomic-inline —
	//     the margin-right:-1px would then NOT affect the line-breaker's wrapping
	//     check for child2.
	//   - makeStyle sets properties directly (no shorthand expansion), so
	//     margin-right:-1px is the correct form (not margin-inline-end).
	//   - line-height:0 on item and wrapperSpan drives the strut descent ≤ 0, so
	//     each line box height = exactly the inline-block height (30px).  Without
	//     this the font-ascent/descent would make each line ~33-35px depending on
	//     the test font, and the exact-equality assertion would need fuzz.
	styles := map[*html.Node]*css.Style{
		grid:        makeStyle("display", "grid", "width", "90px", "justify-items", "start"),
		item:        makeStyle("line-height", "0"),
		child1:      makeStyle("display", "inline-block", "width", "60px", "height", "30px"),
		wrapperSpan: makeStyle("display", "inline", "margin-right", "-1px", "line-height", "0"),
		child2:      makeStyle("display", "inline-block", "width", "30px", "height", "30px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(grid, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	// With justify-items:start the item must be sized to its max-content (89px).
	// At 89px, child2's inline-size (30px) exceeds remaining (29px) → wrap.
	// Grid height must be 60px (2 × 30px lines), not 30px (1 line, the bug).
	if result.Fragment.Size.HeightF64() != 60 {
		t.Errorf("grid height: got %.1f, want 60 (justify-items:start item must be sized to max-content 89px so child2 wraps to line 2; at column 90px it fits: 30 > 30 = false)",
			result.Fragment.Size.HeightF64())
	}
}
