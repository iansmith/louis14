package layout

import "testing"

// width: 0 on a cell or <col> is a specified width, so the column is
// constrained at zero, not auto: InlineSizesFromStyle reads any fixed
// length, 0 included (table/table_layout_algorithm_types.cc:24-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and is_constrained follows
// from it (:86-115, :188-199).
func TestTableZeroWidthColumn(t *testing.T) {
	cases := []struct {
		name, html    string
		table, c0, c1 float64
	}{
		// Fixed layout: a zero-width constrained column gets 0
		// (IsZeroInlineSizeConstrained, table/table_layout_utils.cc:
		// 792-795, :893-917) and the fixed 100px column scales up to 400.
		{"fixed layout cell", `<table id="t" style="table-layout:fixed;border-spacing:0;width:400px"><tr>` +
			`<td id="c0" style="padding:0;width:0"></td><td id="c1" style="padding:0;width:100px"></td></tr></table>`, 400, 0, 400},
		{"fixed layout col", `<table id="t" style="table-layout:fixed;border-spacing:0;width:400px"><col style="width:0"><tr>` +
			`<td id="c0" style="padding:0"></td><td id="c1" style="padding:0;width:100px"></td></tr></table>`, 400, 0, 400},
		// Auto layout: the zero-width cell is constrained at its content
		// minimum (20px), so the space above every max goes to the auto
		// column (DistributeInlineSizeToComputedInlineSizeAuto).
		{"auto layout cell", `<table id="t" style="border-spacing:0;width:400px"><tr>` +
			`<td id="c0" style="padding:0;width:0">` + inlineBlock("20px") + `</td>` +
			`<td id="c1" style="padding:0">` + inlineBlock("60px") + `</td></tr></table>`, 400, 20, 380},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkTwoColumnWidths(t, c.html, c.table, c.c0, c.c1) })
	}
}
