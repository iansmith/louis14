package layout

import "testing"

// A cell's specified width is only its column's max guess: the column's
// minimum stays the cell's content minimum, raised by its min-width.
// CreateCellInlineConstraint resolves min = max(content min, min-width) and
// max = max(min, width) (table/table_layout_algorithm_types.cc:172-199 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a 10px-wide cell holding a
// 20px inline-block makes a 20px column, and a 30px min-width a 30px one.
func TestTableCellWidthBelowContentMinimum(t *testing.T) {
	cases := []struct {
		name, cellStyle string
		want            float64
	}{
		{"content minimum", "width:10px", 20},
		{"min-width above the content", "width:10px;min-width:30px", 30},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><table id="t" style="border-spacing:0">`+
				`<tr><td id="c" style="padding:0;`+c.cellStyle+`">`+inlineBlock("20px")+`</td></tr></table>`)
			tb, cell := findBoxByID(root, "t"), findBoxByID(root, "c")
			if tb == nil || cell == nil {
				t.Fatal("missing #t/#c")
			}
			if tb.Width != c.want || cell.Width != c.want {
				t.Errorf("table/cell widths = %v/%v, want %v", tb.Width, cell.Width, c.want)
			}
		})
	}
}

// In a fixed-layout table a cell's width is its column's width whatever
// its content: CreateCellInlineConstraint measures no content minimum and
// takes the width as the max (table/table_layout_algorithm_types.cc:
// 172-199 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 10px cell holding
// 20px of content beside a 20px cell fills a 30px table as 10 and 20.
func TestTableFixedLayoutCellWidthBelowContent(t *testing.T) {
	html := `<table id="t" style="table-layout:fixed;border-spacing:0;width:30px"><tr>` +
		`<td id="c0" style="padding:0;width:10px">` + inlineBlock("20px") + `</td>` +
		`<td id="c1" style="padding:0;width:20px"></td></tr></table>`
	checkTwoColumnWidths(t, html, 30, 10, 20)
}
