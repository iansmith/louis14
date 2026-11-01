package layout

import "testing"

// TestFillAutoMaxHeightIsContentBased pins Blink's column block-size for an
// auto-height column-fill:auto multicol that has only a max-height and no
// outer fragmentation context. ColumnLayoutAlgorithm::LayoutLine starts from
// remaining_content_block_size_, which is indefinite for block-size:auto
// (column_layout_algorithm.cc:320-321 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the line has a content-based
// block-size (has_content_based_block_size, :849-856); max-height only clamps
// it afterwards, in ConstrainColumnBlockSize (:1774-1775, :1812). It is
// never used as the column block-size itself (WPT
// columnfill-auto-max-height-003).
//
// Chrome 159 getClientRects.
func TestFillAutoMaxHeightIsContentBased(t *testing.T) {
	t.Run("content shorter than max-height", func(t *testing.T) {
		got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
			`<div id=mc style="columns:2;gap:0;column-fill:auto;max-height:200px;width:100px">`+
			`<div id=c style="width:200%;height:100px"></div></div>`)
		checkRects(t, got, map[string][]fragRect{
			"mc": {{0, 0, 100, 100}},
			"c":  {{0, 0, 100, 100}},
		})
	})
	t.Run("content taller than max-height", func(t *testing.T) {
		got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
			`<div id=mc style="columns:2;gap:0;column-fill:auto;max-height:200px;width:100px">`+
			`<div id=c style="height:300px"></div></div>`)
		checkRects(t, got, map[string][]fragRect{
			"mc": {{0, 0, 100, 200}},
			"c":  {{0, 0, 50, 200}, {50, 0, 50, 100}},
		})
	})
}
