package layout

import "testing"

// A table's min/max-content sizes come from its columns, not from
// measuring it as a block: TableLayoutAlgorithm::ComputeMinMaxSizes
// (table/table_layout_algorithm.cc:701-732 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) sums the column constraints in
// ComputeGridInlineMinMax (table/table_layout_utils.cc:1743-1830), where a
// percentage column raises the max-content size.
func TestTableComputeMinMaxSizes(t *testing.T) {
	ib := func(w string) string {
		return `<span style="display:inline-block;width:` + w + `;height:5px"></span>`
	}
	cell := func(w string) string { return `<div style="display:table-cell">` + ib(w) + `</div>` }
	cell10, cell20 := cell("10px"), cell("20px")
	const pctCell = `<div style="display:table-cell;width:100%"></div>`
	cases := []struct {
		name, html string
		want       float64
	}{
		// The max-content size sums the columns: 10 + 20. The float's
		// shrink-to-fit width shows the table's contribution (the table
		// itself already lays out at 30px).
		{"columns sum", `<div style="float:left" id="t"><div style="display:table">` + cell10 + cell20 + `</div></div>`, 30},
		// A 100% column next to a 10px one makes the table's max-content
		// size kTableMaxInlineSize, so a float around it fills its 100px
		// container. (The table's own layout width:
		// TestTableLayoutWidthWithPercentColumn.)
		{"percent column", `<div style="width:100px"><div style="float:left" id="t"><div style="display:table">` + pctCell + cell10 + `</div></div></div>`, 100},
		// Guard: inside a flexbox, column percentages do not count while
		// sizing intrinsically (TableNode::AllowColumnPercentages,
		// table/table_node.cc:63-84), so the table item is 10px.
		// A collapsed table's outer borders are counted once: half in the
		// table's border box and half in its edge cells (TableBorders), so
		// a 10px cell in a 6px collapsed border contributes 6 + 10 + 6.
		{"collapsed borders", `<div id="t" style="float:left"><table style="border-collapse:collapse;border:6px solid">` +
			`<tr><td style="padding:0;border:1px solid">` + ib("10px") + `</td></tr></table></div>`, 22},
		// A colspan cell's min/max spreads across the columns it spans
		// (DistributeColspanCellsToColumns, table/table_layout_utils.cc:
		// 1826-1846), so a 100px colspan=2 cell over 10px and 20px cells
		// makes the table 100px wide.
		{"colspan cell", `<div id="t" style="float:left"><table style="border-spacing:0">` +
			`<tr><td colspan=2 style="padding:0">` + ib("100px") + `</td></tr>` +
			`<tr><td style="padding:0">` + ib("10px") + `</td><td style="padding:0">` + ib("20px") + `</td></tr></table></div>`, 100},
		{"no percentages in a flex item", `<div style="display:flex;width:100px"><div id="t" style="display:table">` + pctCell + cell10 + `</div></div>`, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+c.html), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Width != c.want {
				t.Errorf("#t width = %v, want %v", b.Width, c.want)
			}
		})
	}
}

// An auto-width table's layout width is fit-content over its grid min/max
// in the layout pass, where percentage columns count, and its columns share
// it by the width distribution algorithm (TableLayoutAlgorithm::
// ComputeTableInlineSize → ComputeAssignableTableInlineSize →
// SynchronizeAssignableTableInlineSizeAndColumns, table/
// table_layout_algorithm.cc:497-543; table/table_layout_utils.cc:1849-1864
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 100% column next to a
// 10px one makes an inline-table fill its 100px container
// (css-tables/percent-width-cell-dynamic): the percent column gets 90px.
func TestTableLayoutWidthWithPercentColumn(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0"><div style="width:100px"><div id="t" style="display:inline-table">` +
		`<div id="p" style="display:table-cell;width:100%"></div>` +
		`<div style="display:table-cell"><span style="display:inline-block;width:10px;height:5px"></span></div></div></div>`
	root := layoutRootTestHTML(t, src)
	table, pct := findBoxByID(root, "t"), findBoxByID(root, "p")
	if table == nil || pct == nil {
		t.Fatal("no #t or #p box")
	}
	if table.Width != 100 || pct.Width != 90 {
		t.Errorf("table width = %v, percent cell width = %v; want 100 and 90", table.Width, pct.Width)
	}
}
