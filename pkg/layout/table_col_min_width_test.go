package layout

import "testing"

// A <col>'s min-width alone does not constrain its column: CreateColumn
// sets is_constrained only from the width (table/
// table_layout_algorithm_types.cc:86-115 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so in a fixed table the
// first-row cells still size the column (Column::Encompass skips cells only
// for a constrained column, :264-266). Two 100px cells fill the 400px table
// as 200 and 200.
func TestTableColMinWidthAloneDoesNotConstrain(t *testing.T) {
	html := `<table id="t" style="table-layout:fixed;border-spacing:0;width:400px"><col style="min-width:50px"><tr>` +
		`<td id="c0" style="padding:0;width:100px"></td><td id="c1" style="padding:0;width:100px"></td></tr></table>`
	checkTwoColumnWidths(t, html, 400, 200, 200)
}
