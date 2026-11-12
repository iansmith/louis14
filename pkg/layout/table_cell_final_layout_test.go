package layout

import "testing"

// A table cell is laid out a second time at its final block size, the sum
// of its rows (ComputeCellBlockSize, table/table_layout_utils.cc:1333-1361 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), in the space
// SetupTableCellConstraintSpaceBuilder builds (:1363-1425). That size is the
// cell's definite initial block size, which its children's percentage
// block sizes resolve against, when the cell is constrained (a specified
// block size) or it grew in a table with a specified block size;
// otherwise the initial block size is the intrinsic one and percentages
// stay auto.
func TestTableCellFinalLayout(t *testing.T) {
	cases := []struct {
		name, html string
		wantH      float64
	}{
		// Constrained cell (height:100%) in a 100px table: 40% is 40px
		// (css-sizing/intrinsic-percent-replaced-018).
		{"constrained cell", `<table style="height:100px;border-spacing:0"><tr><td style="height:100%;padding:0">` +
			`<div id="c" style="width:10px;height:40%"></div></td></tr></table>`, 40},
		// Unconstrained cell that grew in a table with a specified height:
		// its height:100% child is the row's 300px. (The same in an
		// anonymous cell, css-sizing/table-child-percentage-height-with-
		// border-box, waits on LOU-900, which stops unwrapping a lone block
		// into the cell.)
		{"grown cell, specified table height", `<table style="height:300px;border-spacing:0"><tr>` +
			`<td style="padding:0"><div id="c" style="height:100%"></div></td></tr></table>`, 300},
		// Guard: the cell grew but the table's height is auto, so the
		// initial block size stays indefinite and the 100% child is auto.
		{"grown cell, auto table height", `<table style="border-spacing:0"><tr>` +
			`<td style="padding:0"><div style="height:50px"></div></td>` +
			`<td style="padding:0"><div id="c" style="height:100%"></div></td></tr></table>`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+c.html), "c")
			if b == nil {
				t.Fatal("no #c box")
			}
			if b.Height != c.wantH {
				t.Errorf("#c height = %v, want %v", b.Height, c.wantH)
			}
		})
	}

	// Guard: vertical-align:middle centers the content in the final row
	// block size (FinalizeTableCellLayout, table_layout_utils.cc:1575-1641).
	// Unlike TestTableCell_ExplicitHeightVerticalAlign, the cell has no
	// height of its own: the row grows to the table's 100px.
	t.Run("middle in a grown row", func(t *testing.T) {
		src := `<!DOCTYPE html><body style="margin:0"><table style="height:100px;border-spacing:0"><tr>` +
			`<td style="padding:0;vertical-align:middle"><div id="c" style="width:10px;height:20px"></div></td></tr></table>`
		b := findBoxByID(layoutRootTestHTML(t, src), "c")
		if b == nil {
			t.Fatal("no #c box")
		}
		if b.Y != 40 {
			t.Errorf("#c y = %v, want 40 (centered in the 100px row)", b.Y)
		}
	})
}
