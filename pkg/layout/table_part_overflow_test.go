package layout

import "testing"

// Table rows and row groups ignore overflow: LayoutTableRow and
// LayoutTableSection override RespectsCSSOverflow() to false
// (table/layout_table_row.h:70-73, table/layout_table_section.h:52-55 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so overflow:hidden on a <tr>
// does not clip its cells (css-ui/text-overflow-025). Table cells and
// tables still respect it.
func TestTablePartsIgnoreOverflow(t *testing.T) {
	for _, c := range []struct {
		display string
		want    bool
	}{
		{"table-row", false},
		{"table-row-group", false},
		{"table-header-group", false},
		{"table-footer-group", false},
		{"table-cell", true},
		{"table", true},
	} {
		style := makeStyle("display", c.display, "overflow", "hidden")
		if got := hasNonVisibleOverflow(style, nil); got != c.want {
			t.Errorf("display:%s; overflow:hidden: hasNonVisibleOverflow = %v, want %v", c.display, got, c.want)
		}
	}
}
