package layout

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

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

// A cell's fixed block size snaps to a LayoutUnit before it sizes the row,
// as every other resolved length does, so rows of a fractional height
// stack without drift (css-color/t422-rgba-values-meaning-b: 1.2em cells).
func TestTableCellFractionalFixedBlockSize(t *testing.T) {
	row := `<tr><td style="padding:0;height:1.2em">&nbsp;</td></tr>`
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font-size:16px"><table style="border-spacing:0">`+
		row+row+row+`<tr id="r"><td style="padding:0;height:1.2em">&nbsp;</td></tr></table>`)
	last := findBoxByID(root, "r")
	if last == nil {
		t.Fatal("missing #r")
	}
	if want := 3 * layoutunit.FromFloat64Round(19.2).Float64(); last.Y != want {
		t.Errorf("fourth row y = %v, want %v", last.Y, want)
	}
}

// An orthogonal cell's block size is the column width, a fixed block size
// that its table-cell space does not mark as an override: it is laid out at
// that size, not its intrinsic one (ComputeBlockSizeForFragment checks
// IsFixedBlockSize before IsTableCell, length_utils.cc:909-919 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Its height sizes the row
// (css-writing-modes/ch-units-vrl-003).
func TestTableOrthogonalCellBlockSize(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><table style="border-spacing:0"><col style="width:110px">`+
		`<tr id="r"><td id="c" style="padding:0;height:110px;writing-mode:vertical-rl">x</td></tr></table>`)
	row, cell := findBoxByID(root, "r"), findBoxByID(root, "c")
	if row == nil || cell == nil {
		t.Fatal("missing #r/#c")
	}
	if row.Height != 110 || cell.Width != 110 || cell.Height != 110 {
		t.Errorf("row height, cell width x height = %v, %v x %v; want 110, 110 x 110", row.Height, cell.Width, cell.Height)
	}
}
