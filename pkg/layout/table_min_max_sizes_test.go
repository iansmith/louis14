package layout

import "testing"

// A table's min/max-content sizes come from its columns, not from
// measuring it as a block: TableLayoutAlgorithm::ComputeMinMaxSizes
// (table/table_layout_algorithm.cc:701-732 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) sums the column constraints in
// ComputeGridInlineMinMax (table/table_layout_utils.cc:1743-1830), where a
// percentage column raises the max-content size.
func TestTableComputeMinMaxSizes(t *testing.T) {
	cell := func(w string) string {
		return `<div style="display:table-cell"><span style="display:inline-block;width:` + w + `;height:5px"></span></div>`
	}
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
		// container. (The table's own layout width also needs the
		// percentage in the layout pass: LOU-901 T3b.)
		{"percent column", `<div style="width:100px"><div style="float:left" id="t"><div style="display:table">` + pctCell + cell10 + `</div></div></div>`, 100},
		// Guard: inside a flexbox, column percentages do not count while
		// sizing intrinsically (TableNode::AllowColumnPercentages,
		// table/table_node.cc:63-84), so the table item is 10px.
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
