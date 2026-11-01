package layout

import "testing"

// TestColumnWrap_ForcedBreaksWrapWithoutRowHeight pins Blink's row loop in
// ColumnLayoutAlgorithm::LayoutFragmentationContext for column-wrap:wrap with
// neither a block-size nor a column-height: the loop continues while there is
// a column break token and ShouldWrapColumns() (column_layout_algorithm.cc:776
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), independent of HasRowHeight().
// Each further row starts OffsetToNextRow() below, which without a row height
// is just the row gap (:1944-1955). So forced breaks produce several
// content-sized rows (WPT column-wrap-no-constraints-002).
//
// Chrome 159 getClientRects.
func TestColumnWrap_ForcedBreaksWrapWithoutRowHeight(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div id=mc style="columns:2;gap:10px 0;column-wrap:wrap;width:100px">`+
		`<div id=a style="break-after:column;height:20px"></div>`+
		`<div id=b style="break-after:column;height:20px"></div>`+
		`<div id=c style="break-after:column;height:50px"></div>`+
		`<div id=d style="break-after:column;height:50px"></div>`+
		`<div id=e style="break-after:column;height:10px"></div>`+
		`<div id=f style="break-after:column;height:10px"></div></div>`)
	checkRects(t, got, map[string][]fragRect{
		"mc": {{0, 0, 100, 100}},
		"a":  {{0, 0, 50, 20}},
		"b":  {{50, 0, 50, 20}},
		"c":  {{0, 30, 50, 50}},
		"d":  {{50, 30, 50, 50}},
		"e":  {{0, 90, 50, 10}},
		"f":  {{50, 90, 50, 10}},
	})
}
