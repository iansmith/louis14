package layout

import "testing"

// A collapsed table splits every edge in half between its two sides: a cell
// gets half of each edge around it, outer edges included
// (TableBorders::GetCellBorders, table/table_borders.cc:335-387), and the
// table gets the other half of each outer edge as its own border
// (UpdateTableBorder :389-398; ComputeBorders' table branch, length_utils.cc:
// 1495-1497), with no padding (ComputePadding :1522-1525). Each edge width is
// a LayoutUnit (table_borders.h:97-104, :151-154) and the half is truncated
// to it (LayoutUnit /= 2 through the float constructor, layout_unit.h:
// 125-128, 795-798): a 5px edge splits 2.5 / 2.5 and a 1px edge 0.5 / 0.5.
// All @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestCollapsedBordersSplitBetweenCellAndTable(t *testing.T) {
	t.Run("odd widths split exactly", func(t *testing.T) {
		root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/1 Ahem">`+
			`<table id="t" style="border-collapse:collapse;border:5px solid">`+
			`<tr><td id="a" style="padding:0;border:1px solid">x</td><td id="b" style="padding:0;border:1px solid">x</td></tr></table>`)
		assertBoxGeometry(t, "#a", findBoxByID(root, "a"), 2.5, 2.5, 13, 15)
		assertBoxGeometry(t, "#b", findBoxByID(root, "b"), 15.5, 2.5, 13, 15)
		assertBoxGeometry(t, "#t", findBoxByID(root, "t"), 0, 0, 31, 20)
	})
	t.Run("no table padding", func(t *testing.T) {
		root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/1 Ahem">`+
			`<table id="t" style="border-collapse:collapse;border:6px solid;padding:3px">`+
			`<tr><td id="d" style="padding:0;border:2px solid">x</td></tr></table>`)
		assertBoxGeometry(t, "#d", findBoxByID(root, "d"), 3, 3, 16, 16)
		assertBoxGeometry(t, "#t", findBoxByID(root, "t"), 0, 0, 22, 22)
	})
}
