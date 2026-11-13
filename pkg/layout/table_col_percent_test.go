package layout

import "testing"

// A <col>'s percentage width makes its column a percentage column, as a
// cell's does: ColumnConstraintsBuilder::VisitCol builds each column with
// TableTypes::CreateColumn (table/table_layout_utils.cc:338-351), which
// reads the percentage through InlineSizesFromStyle (table/
// table_layout_algorithm_types.cc:24-75, :86-115), both @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestTableColPercentageWidth(t *testing.T) {
	cells := `<td id="c0" style="padding:0">` + inlineBlock("20px") + `</td><td id="c1" style="padding:0">` + inlineBlock("60px") + `</td>`
	cases := []struct {
		name, html    string
		table, c0, c1 float64
	}{
		// Auto layout: a 50% column of a 400px table is 200px.
		{"auto layout", `<table id="t" style="border-spacing:0;width:400px"><col style="width:50%"><tr>` + cells + `</tr></table>`, 400, 200, 200},
		// A <colgroup>'s percentage applies to its column too.
		{"colgroup", `<table id="t" style="border-spacing:0;width:400px"><colgroup style="width:50%"></colgroup><tr>` + cells + `</tr></table>`, 400, 200, 200},
		// Fixed layout: the 25% column is 100px; the fixed 100px column
		// scales up into the rest (SynchronizeAssignableTableInlineSize-
		// AndColumnsFixed).
		{"fixed layout", `<table id="t" style="table-layout:fixed;border-spacing:0;width:400px"><col style="width:25%"><tr>` +
			`<td id="c0" style="padding:0"></td><td id="c1" style="padding:0;width:100px"></td></tr></table>`, 400, 100, 300},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkTwoColumnWidths(t, c.html, c.table, c.c0, c.c1) })
	}
}
