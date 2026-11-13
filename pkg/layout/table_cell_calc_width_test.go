package layout

import "testing"

// A cell width that is a calculation with a percentage, other than a
// calc() whose pixel part is 0, is neither a fixed length nor a percentage
// for the table: InlineSizesFromStyle reads only IsFixed lengths,
// percentages and pixels-and-percent calc()s with no pixels (table/
// table_layout_algorithm_types.cc:24-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the cell is auto. In a
// 400px table over cells of max-content 20 and 60 both columns are auto:
// 100 and 300. A calculation of lengths only (min(40px, 50px)) is a fixed
// length, a 40px column.
func TestTableCellCalcExpressionWidth(t *testing.T) {
	cases := []struct {
		name, width   string
		table, c0, c1 float64
	}{
		{"min() with percentages is auto", "min(50%, 60%)", 400, 100, 300},
		{"max() of a length and a percentage is auto", "max(10px, 5%)", 400, 100, 300},
		{"min() of lengths is fixed", "min(40px, 50px)", 400, 40, 360},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := `<table id="t" style="border-spacing:0;width:400px"><tr>` +
				`<td id="c0" style="padding:0;width:` + c.width + `">` + inlineBlock("20px") + `</td>` +
				`<td id="c1" style="padding:0">` + inlineBlock("60px") + `</td></tr></table>`
			checkTwoColumnWidths(t, html, c.table, c.c0, c.c1)
		})
	}
}

// The same holds for a cell's block size: only a fixed height sizes its
// row (ComputeMinimumRowBlockSize, table/table_layout_utils.cc:265-292 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a max(20px, 5%) cell
// over 5px of content makes a 5px row.
func TestTableCellCalcExpressionHeight(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><table style="border-spacing:0"><tr id="r">`+
		`<td style="padding:0;height:max(20px, 5%)"><div style="width:10px;height:5px"></div></td></tr></table>`)
	row := findBoxByID(root, "r")
	if row == nil {
		t.Fatal("missing #r")
	}
	if row.Height != 5 {
		t.Errorf("row height = %v, want 5", row.Height)
	}
}
