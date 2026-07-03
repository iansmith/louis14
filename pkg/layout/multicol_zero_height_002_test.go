package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestZeroHeightUnbreakableNoContinuation reproduces multicol-zero-height-002:
// an UNBREAKABLE block with a definite block-size in a zero-height multicol
// must OVERFLOW column 1 as a whole — it must NOT produce a continuation
// fragment in a second column (which paints the forbidden "green strip in the
// second column"). WPT assert: "the unbreakable block element with definite
// block-size doesn't ask for an extra continuation in the second column."
//
// Blink FinishFragmentation (fragmentation_utils.cc:589-599 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) raises space_left to encompass
// tall unbreakable content, so desired_block_size does not exceed space_left
// and SetDidBreakSelf is skipped — the block overflows rather than continuing.
//
// Regression guard for the LOU-370 Phase 16.d.1 `space_left > 0` relaxation:
// relaxing it for 0-height FragmentColumn spaces (as an earlier iteration did)
// forced this unbreakable child to self-break → a continuation → the strip.
// The `> 0` guard must stay; children-height-003's budget-exhausted post-
// spanner block reaches the clamp with a 1px column (not 0), so it is
// unaffected.
func TestZeroHeightUnbreakableNoContinuation(t *testing.T) {
	// .multicol { column-width:100px; inline-size:300px; block-size:0 }
	// with one unbreakable .child { inline-size:100px; block-size:100px }.
	// The inner block stands in for the single-glyph Ahem line (an
	// unbreakable 100px unit) so the test needs no font.
	glyph := makeNode("div")
	child := makeNode("div", glyph)
	mc := makeNode("div", child)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-width", "100px",
			"width", "300px", "height", "0"),
		child: makeStyle("display", "block", "width", "100px", "height", "100px"),
		glyph: makeStyle("display", "block", "height", "100px"),
	}

	result := layoutMulticolForTest(t, mc, styles)

	// Count column fragmentainers that carry a non-zero-height copy of the
	// child, and record the child's per-column heights. The unbreakable child
	// must overflow the single zero-height column WHOLE (one 100px copy) — not
	// clip to 0 and reappear as a continuation copy in a second column (the
	// forbidden strip).
	var childHeights []float64
	contentCols := 0
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType != BoxTypeColumn {
			continue
		}
		colHasContent := false
		for _, gc := range ch.Fragment.Children {
			childHeights = append(childHeights, gc.Fragment.Size.HeightF64())
			if gc.Fragment.Size.HeightF64() > 0 {
				colHasContent = true
			}
		}
		if colHasContent {
			contentCols++
		}
	}
	if contentCols != 1 {
		t.Errorf("content-bearing columns = %d (child copies: %v), want 1 — the "+
			"unbreakable child overflows the single zero-height column; a 2nd "+
			"content column is the forbidden green strip", contentCols, childHeights)
	}
	// The single overflowing copy must keep the child's full 100px block-size
	// (overflow), not be clipped to 0 (which triggers the continuation).
	if len(childHeights) == 0 || childHeights[0] != 100 {
		t.Errorf("first-column child height = %v, want 100 (overflow whole, not "+
			"clipped to 0)", childHeights)
	}
}
