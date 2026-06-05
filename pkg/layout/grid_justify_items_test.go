package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestGridLayout_JustifyItemsStart_WrapsAtMaxContent verifies that a grid item
// with justify-self != stretch is sized to its max-content (ShrinkToFit), not
// to the full column track width.
//
// Setup: 90px grid, justify-items:start.
// Item contains:
//   - child1: inline-block 60×30px
//   - wrapperSpan: display:inline, margin-right:-1px, line-height:0
//     - child2: inline-block 30×30px
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
