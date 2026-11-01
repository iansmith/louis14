package layout

import (
	"fmt"
	"testing"
)

// TestCollapsedRowBordersOnlyOnRowEdges pins TableBorders::MergeBorders for a
// row source: a row merges its inline-start/end borders into the column
// edges at the table's first and last column only, never into the inner
// column edges (MergeColumnAxisBorder at start_column and
// start_column + colspan, table_borders.cc:494-498 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The rows' 30px left/right
// borders beat the table's 29px; the inner edges stay empty, so each cell's
// content sits 30px in from the table edge or flush against its neighbor.
// Geometry from WPT box-shadow-table-border-collapse-001.
func TestCollapsedRowBordersOnlyOnRowEdges(t *testing.T) {
	box := `<div id=%s style="width:50px;height:50px"></div>`
	cell := func(id string) string { return `<td>` + fmt.Sprintf(box, id) + `</td>` }
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<style>table{border:red solid 29px;border-collapse:collapse}tr{border-color:blue;border-style:solid}td{padding:0}</style>`+
		`<table><tr style="border-width:30px 30px 0px">`+cell("a")+cell("b")+
		`<tr style="border-width:0px 30px 30px">`+cell("c")+cell("d")+`</table>`)
	checkRectsFrom(t, got, map[string][]fragRect{
		"a": {{30, 30, 50, 50}},
		"b": {{80, 30, 50, 50}},
		"c": {{30, 80, 50, 50}},
		"d": {{80, 80, 50, 50}},
	}, "derived from Blink TableBorders")
}

// TestCollapsedBorderUnsetWidthIsMedium pins TableBorders::BorderWidth
// reading the computed border width, whose initial value is medium (3px):
// a row with border-style:solid and no border-width contributes 3px edges
// (table_borders.h:97-113 → ComputedStyle::BorderLeftWidth @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; LOU-551 item 1).
func TestCollapsedBorderUnsetWidthIsMedium(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<table style="border-collapse:collapse"><tr style="border-style:solid">`+
		`<td style="padding:0"><div id=a style="width:10px;height:10px"></div></td></tr></table>`)
	checkRectsFrom(t, got, map[string][]fragRect{"a": {{3, 3, 10, 10}}}, "derived from Blink TableBorders")
}
