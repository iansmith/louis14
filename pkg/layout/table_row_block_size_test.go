package layout

import "testing"

// A table cell's min-height and max-height do not size its row. The cell
// is measured at its intrinsic block size, as ComputeBlockSizeForFragment
// ignores a table cell's block-size properties (length_utils.cc:918-919),
// and ComputeMinimumRowBlockSize reads only the cell's fixed height ("Ignore
// min/max-height for determining the |cell_css_block_size|", table/
// table_layout_utils.cc:265-292), both @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. The cell is then laid out at
// its row's block size.
func TestTableCellMinMaxBlockSizeIgnoredByRow(t *testing.T) {
	cases := []struct {
		name, cellStyle string
		want            float64
	}{
		{"min-height", "min-height:100px", 5},
		{"max-height below height", "height:50px;max-height:20px", 50},
		{"height (control)", "height:50px", 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><table style="border-spacing:0"><tr id="r">`+
				`<td id="c" style="padding:0;`+c.cellStyle+`"><div style="width:10px;height:5px"></div></td></tr></table>`)
			row, cell := findBoxByID(root, "r"), findBoxByID(root, "c")
			if row == nil || cell == nil {
				t.Fatal("missing #r/#c")
			}
			if row.Height != c.want || cell.Height != c.want {
				t.Errorf("row/cell heights = %v/%v, want %v", row.Height, cell.Height, c.want)
			}
		})
	}
}
