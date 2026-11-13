package layout

import (
	"strings"
	"testing"
)

// A scrollable percentage-height child of a "restricted" table cell (its
// table has a specified block size, or the cell a fixed height) is measured
// at its min-height, so it does not grow the row; laid out again at the
// row's size, its percentage resolves. SetupTableCellConstraintSpaceBuilder
// marks the cell (table/table_layout_utils.cc:1418-1419),
// CreateConstraintSpaceForChild the child (block_layout_algorithm.cc:
// 3474-3487), and ComputeBlockSizeForFragmentInternal sizes it to its min
// block size (length_utils.cc:813-821), all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestTableRestrictedCellScrollableChild(t *testing.T) {
	child := `<div id="c" style="height:50%;overflow:auto;min-height:7px"><div style="height:200px"></div></div>`
	cases := []struct {
		name, html   string
		table, wantH float64
	}{
		// The 100px table's row is sized without the child's content:
		// the child is 50% of it.
		{"specified table height", `<table id="t" style="border-spacing:0;height:100px"><tr><td style="padding:0">` + child + `</td></tr></table>`, 100, 50},
		// A fixed-height cell restricts too.
		{"fixed cell height", `<table id="t" style="border-spacing:0"><tr><td style="padding:0;height:40px">` + child + `</td></tr></table>`, 40, 20},
		// Guard: overflow:hidden is not scrollable for this rule, so
		// the child is measured at its content and grows the table.
		{"overflow hidden (control)", `<table id="t" style="border-spacing:0;height:100px"><tr><td style="padding:0">` +
			strings.Replace(child, "overflow:auto", "overflow:hidden", 1) + `</td></tr></table>`, 200, 200},
		// Control: an auto table and cell are not restricted; the
		// percentage is auto and the child its content.
		{"auto table (control)", `<table id="t" style="border-spacing:0"><tr><td style="padding:0">` + child + `</td></tr></table>`, 200, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+c.html)
			tb, ch := findBoxByID(root, "t"), findBoxByID(root, "c")
			if tb == nil || ch == nil {
				t.Fatal("missing #t/#c")
			}
			if tb.Height != c.table || ch.Height != c.wantH {
				t.Errorf("table/child heights = %v/%v, want %v/%v", tb.Height, ch.Height, c.table, c.wantH)
			}
		})
	}
}
