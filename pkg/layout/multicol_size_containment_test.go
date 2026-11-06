package layout

import "testing"

// A size-contained multicol still takes its intrinsic inline size from the
// column formula: its column content counts as empty
// (CalculateMinMaxSizesIgnoringChildren, length_utils.cc:1929-1934), then
// column-width and column-count x width + gaps apply
// (ColumnLayoutAlgorithm::ComputeMinMaxSizes, column_layout_algorithm.cc:
// 433-495 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So a shrink-to-fit
// `columns: 2 40px; column-gap: 20px` box is 100px wide whatever its content.
// Mirrors WPT css-contain/contain-size-multicol-001/002.
func TestSizeContainedMulticolIntrinsicWidth(t *testing.T) {
	const content = `<div style="width:300px; height:30px"></div>`
	for _, pos := range []string{"position:absolute", "float:left"} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div id=m style="`+pos+
			`; contain:size; columns:2 40px; column-gap:20px; min-height:10px">`+content+`</div></body>`, nil)
		if m := findBox(fl.root, byID("m")); m == nil || m.Width != 100 {
			t.Errorf("%s size-contained multicol: %s, want 100 wide", pos, boxDims(m))
		}
	}
}
