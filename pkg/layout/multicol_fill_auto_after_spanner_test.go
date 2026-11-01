package layout

import "testing"

// TestFillAutoContentAfterSpannerIsNotBalanced pins two pieces of Blink's
// per-line column sizing in ColumnLayoutAlgorithm::LayoutLine
// (column_layout_algorithm.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - balance_columns is recomputed for every line from column-fill (and an
//     outer balancing pass) (:840-843); only a column-fill:auto line that
//     ends at a spanner is switched to balancing (:1071-1081), so content
//     after the spanner is not balanced (worklist B5);
//   - an unconstrained column-fill:auto line still gets a content-based,
//     unbalanced column block-size (has_content_based_block_size,
//     :845-856; no DistributeImplicitBreaks when not balancing,
//     :1726-1729), so the content stays in one column (worklist B4).
//
// Chrome 155 getClientRects.
func TestFillAutoContentAfterSpannerIsNotBalanced(t *testing.T) {
	t.Run("20px, spanner, 80px", func(t *testing.T) {
		got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
			`<div id=mc style="column-count:2;column-fill:auto;column-gap:0;width:100px">`+
			`<div style="height:20px"></div><div id=sp style="column-span:all;height:10px"></div>`+
			`<div id=after style="height:80px"></div></div>`)
		checkRects(t, got, map[string][]fragRect{
			"mc":    {{0, 0, 100, 100}},
			"sp":    {{0, 10, 100, 10}},
			"after": {{0, 20, 50, 80}},
		})
	})
	t.Run("OOF-stretched line, spanner, 100px", func(t *testing.T) {
		got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
			`<div id=mc style="column-count:2;column-fill:auto;column-gap:0;width:100px">`+
			`<div style="position:relative"><div style="height:100px"></div>`+
			`<div style="position:absolute;width:50px;height:200px;top:0"></div></div>`+
			`<div style="column-span:all"></div><div id=t style="height:100px"></div></div>`)
		checkRects(t, got, map[string][]fragRect{
			"mc": {{0, 0, 100, 200}},
			"t":  {{0, 100, 50, 100}},
		})
	})
}
