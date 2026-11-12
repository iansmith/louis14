package layout

import "testing"

// A table's bare cells share one anonymous row group and row, also when the
// table is nested in a table cell: LayoutTable::AddChild and
// LayoutTableSection::AddChild put a child that cannot be their direct
// child into the last anonymous child when there is one (table/
// layout_table.cc:234-266, table/layout_table_section.cc:73-102 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Two cells of 10px and 20px with
// 2px border spacing make a 36px-wide, one-row table.
func TestTableNestedBareCellsShareRow(t *testing.T) {
	inner := `<div id="t" style="display:table;border-spacing:2px">` +
		`<div style="display:table-cell"><span style="display:inline-block;width:10px;height:5px"></span></div>` +
		`<div style="display:table-cell"><span style="display:inline-block;width:20px;height:5px"></span></div></div>`
	cases := []struct{ name, html string }{
		{"top level", inner},
		{"nested in a table cell", `<div style="display:table"><div style="display:table-cell">` + inner + `</div></div>`},
		{"nested in a caption", `<div style="display:table"><div style="display:table-caption">` + inner + `</div></div>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tb := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+c.html), "t")
			if tb == nil {
				t.Fatal("missing #t")
			}
			if tb.Width != 36 {
				t.Errorf("#t width = %v, want 36 (one row of two cells)", tb.Width)
			}
		})
	}
}
