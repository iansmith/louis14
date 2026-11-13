package layout

import "testing"

// A table's border and padding wrap its grid, not its captions: the
// captions lay out at the table's border-box inline size and flow before
// and after the grid's border and padding (BeginTableBoxLayout /
// EndTableBoxLayout and the caption loops, table/table_layout_algorithm.cc:
// 466-482, :966-993, :1585-1589; captions' available size, :114-130). The
// fragment records the grid's rect, where the table's background and border
// are painted (TableGridRect, :1657-1676; TablePainter::
// PaintBoxDecorationBackground, paint/table_painters.cc:421-435). All @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestTableCaptionsOutsideTheBorderBox(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0;font:10px/1 Ahem">`+
		`<table id="t" style="border:5px solid;padding:3px;border-spacing:0">`+
		`<caption id="top">cap</caption><caption id="bottom" style="caption-side:bottom">cap</caption>`+
		`<tr><td id="d" style="padding:0">x</td></tr></table>`)
	tbl, top, bottom, cell := findBoxByID(root, "t"), findBoxByID(root, "top"), findBoxByID(root, "bottom"), findBoxByID(root, "d")
	if tbl == nil || top == nil || bottom == nil || cell == nil {
		t.Fatal("missing boxes")
	}
	assertBoxGeometry(t, "#top", top, 0, 0, tbl.Width, 10)
	// The grid: border 5 + padding 3 + the 10px cell + padding 3 + border 5.
	assertBoxGeometry(t, "#d", cell, 8, 18, cell.Width, 10)
	assertBoxGeometry(t, "#bottom", bottom, 0, 36, tbl.Width, 10)
	if tbl.Height != 46 {
		t.Errorf("table height %v, want 46", tbl.Height)
	}
	want := PhysicalRect{Offset: PhysicalOffset{X: 0, Y: 10}, Size: PhysicalSize{Width: tbl.Width, Height: 26}}
	if tbl.TableGridRect == nil || *tbl.TableGridRect != want {
		t.Errorf("table grid rect %+v, want %+v", tbl.TableGridRect, want)
	}
}
