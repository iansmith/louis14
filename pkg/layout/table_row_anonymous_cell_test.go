package layout

import "testing"

// A table row's non-cell child is laid out inside an anonymous table cell,
// an ordinary cell and block formatting context root, never as the cell
// itself: Blink's LayoutTableRow::AddChild wraps it with
// LayoutTableCell::CreateAnonymousWithParent (table/layout_table_row.cc:
// 87-115 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An empty block's
// 50px block-start and block-end margins collapse through to one 50px
// margin inside that cell, so the row is 50px tall, whether the block is
// the row's only child or one of two
// (css-tables/anonymous-table-cell-margin-collapsing).
func TestTableRowNonCellChildGetsAnonymousCell(t *testing.T) {
	for _, c := range []struct{ name, children string }{
		{"lone child", `<div style="margin:50px 0"></div>`},
		{"two children", `<div style="margin:50px 0"></div><div style="margin:50px 0"></div>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := `<!DOCTYPE html><body style="margin:0"><div style="display:table">` +
				`<div id="row" style="display:table-row">` + c.children + `</div></div>`
			row := findBoxByID(layoutRootTestHTML(t, src), "row")
			if row == nil {
				t.Fatal("no #row box")
			}
			if row.Height != 50 {
				t.Errorf("row height = %v, want 50 (the child's margins collapse through inside the anonymous cell)", row.Height)
			}
		})
	}
}
