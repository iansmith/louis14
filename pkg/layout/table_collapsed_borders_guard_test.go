package layout

import "testing"

// Collapsed borders resolve across row groups: a thead's block-end border is
// the edge between its last row and the tbody's first, split in half
// between their cells (TableBorders::ComputeTableBorders merging section
// borders, table/table_borders.cc:115-248, :433-503 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestCollapsedBordersAcrossRowGroups(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/1 Ahem">`+
		`<table style="border-collapse:collapse"><thead style="border-bottom:8px solid"><tr><td id="a" style="padding:0">x</td></tr></thead>`+
		`<tbody><tr><td id="b" style="padding:0">x</td></tr></tbody></table>`)
	a, b := findBoxByID(root, "a"), findBoxByID(root, "b")
	assertBoxGeometry(t, "#a", a, 0, 0, a.Width, 14)
	assertBoxGeometry(t, "#b", b, 0, 14, b.Width, 14)
}

// A collapsed table laid out more than once (its min/max sizes, then its
// layout, inside a shrink-to-fit box) resolves its borders from the cells'
// own styles each time, not from the share it gave them the first time
// (LayoutInputNode.collapsedBorderSourceStyle; Blink computes the borders
// from the styles, TableBorders::ComputeTableBorders, table_borders.cc:
// 115-248 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the 6px outer edge
// still splits 3 / 3. The cell's border is the wider one, so resolving from
// its 3px share would give a 3px edge.
func TestCollapsedBordersResolvedAgainFromSourceStyles(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/1 Ahem">`+
		`<div id="w" style="display:inline-block"><table id="t" style="border-collapse:collapse;border:2px solid">`+
		`<tr><td id="d" style="padding:0;border:6px solid">x</td></tr></table></div>`)
	assertBoxGeometry(t, "#d", findBoxByID(root, "d"), 3, 3, 16, 16)
	assertBoxGeometry(t, "#t", findBoxByID(root, "t"), 0, 0, 22, 22)
	if w := findBoxByID(root, "w"); w == nil || w.Width != 22 {
		t.Errorf("#w width %v, want 22", w)
	}
}
