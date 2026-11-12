package layout

import "testing"

// Only the first table-header-group is the table's header and only the
// first table-footer-group its footer; later ones are bodies, in document
// order. Mirrors Blink's TableGroupedChildren
// (table/table_layout_algorithm_types.cc:296-330 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); css-tables/row-group-order.
func TestTableGroupedChildrenFirstHeaderAndFooterOnly(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0"><table>` +
		`<tbody><tr><td id="b1">b1</td></tr></tbody>` +
		`<thead><tr><td id="h1">h1</td></tr></thead>` +
		`<thead><tr><td id="h2">h2</td></tr></thead>` +
		`<tfoot><tr><td id="f1">f1</td></tr></tfoot>` +
		`<tbody><tr><td id="b2">b2</td></tr></tbody>` +
		`<tfoot><tr><td id="f2">f2</td></tr></tfoot>` +
		`</table>`
	root := layoutRootTestHTML(t, src)
	// Header h1, then the bodies in document order (b1, h2, b2, f2), then
	// footer f1.
	want := []string{"h1", "b1", "h2", "b2", "f2", "f1"}
	prev := -1.0
	for _, id := range want {
		b := findBoxByID(root, id)
		if b == nil {
			t.Fatalf("no #%s box", id)
		}
		if b.Y <= prev {
			t.Errorf("#%s at y=%v, not below the previous row (y=%v); want row order %v", id, b.Y, prev, want)
		}
		prev = b.Y
	}
}
