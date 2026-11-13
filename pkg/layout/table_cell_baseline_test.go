package layout

import "testing"

// Baseline-aligned cells (vertical-align: baseline, the initial value)
// share their row's baseline: RowBaselineTabulator takes the row baseline
// as the largest ascent of the baseline-aligned cells and grows the row to
// fit the largest ascent plus the largest descent (table/
// table_layout_utils.cc:1680-1724), and FinalizeTableCellLayout moves a
// baseline cell's content by the row baseline less its own first baseline
// (:1606-1628), both @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestTableCellBaselineAlignment(t *testing.T) {
	ib := func(id, h string) string {
		return `<span id="` + id + `" style="display:inline-block;width:10px;height:` + h + `"></span>`
	}
	table := func(cells string) string {
		return `<!DOCTYPE html><body style="margin:0;font-size:0"><div style="display:table;border-spacing:0">` +
			`<div id="r" style="display:table-row">` + cells + `</div></div>`
	}
	cell := func(content string) string { return `<div style="display:table-cell">` + content + `</div>` }

	t.Run("shared baseline", func(t *testing.T) {
		// Baselines 10 and 30: the 10px block moves down to 20.
		root := layoutRootTestHTML(t, table(cell(ib("a", "10px"))+cell(ib("b", "30px"))))
		a, b, r := findBoxByID(root, "a"), findBoxByID(root, "b"), findBoxByID(root, "r")
		if a == nil || b == nil || r == nil {
			t.Fatal("missing #a/#b/#r")
		}
		if a.Y != 20 || b.Y != 0 || r.Height != 30 {
			t.Errorf("a y, b y, row height = %v, %v, %v; want 20, 0, 30", a.Y, b.Y, r.Height)
		}
	})
	t.Run("row fits ascent plus descent", func(t *testing.T) {
		// Cell 1: ascent 10, descent 40 (a 40px block below the line).
		// Cell 2: ascent 30, descent 0. The row is 30 + 40 = 70, more than
		// either cell, and cell 1's content moves down by 20.
		root := layoutRootTestHTML(t, table(
			cell(`<div>`+ib("a", "10px")+`</div><div style="height:40px"></div>`)+cell(ib("b", "30px"))))
		a, r := findBoxByID(root, "a"), findBoxByID(root, "r")
		if a == nil || r == nil {
			t.Fatal("missing #a/#r")
		}
		if a.Y != 20 || r.Height != 70 {
			t.Errorf("a y, row height = %v, %v; want 20, 70", a.Y, r.Height)
		}
	})
	t.Run("middle cell does not set the baseline", func(t *testing.T) {
		// Only baseline-aligned cells count: a 50px middle-aligned cell
		// leaves the baseline at the 10px cell's own, so it does not move.
		root := layoutRootTestHTML(t, table(
			cell(ib("a", "10px"))+`<div style="display:table-cell;vertical-align:middle">`+ib("b", "50px")+`</div>`))
		a, r := findBoxByID(root, "a"), findBoxByID(root, "r")
		if a == nil || r == nil {
			t.Fatal("missing #a/#r")
		}
		if a.Y != 0 || r.Height != 50 {
			t.Errorf("a y, row height = %v, %v; want 0, 50", a.Y, r.Height)
		}
	})
}

// A table gives no baseline to an enclosing inline-block: when computing
// an inline-block's baseline, PropagateBaselineFromBlockChild skips table
// children (block_layout_algorithm.cc:3740-3745 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the inline-block's
// baseline is its bottom margin edge, as with no table at all
// (css-position/position-relative-table-tbody-top). The row's own
// baseline (its content end, RowBaselineTabulator) stays inside the table.
func TestTableGivesNoInlineBlockBaseline(t *testing.T) {
	line := func(content string) float64 {
		root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div id="o">`+
			`<div style="display:inline-block;width:150px;height:200px"><div>`+content+`</div></div></div>`)
		o := findBoxByID(root, "o")
		if o == nil {
			t.Fatal("missing #o")
		}
		return o.Height
	}
	withTable := line(`<table style="border-collapse:collapse"><tbody><tr><td style="padding:0"><div style="width:50px;height:50px"></div></td></tr></tbody></table>`)
	without := line(``)
	if withTable != without {
		t.Errorf("line height with a table = %v, want %v (as without one)", withTable, without)
	}
}
