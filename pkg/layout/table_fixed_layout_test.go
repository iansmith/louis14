package layout

import "testing"

// A fixed-layout table distributes its assignable inline size with
// SynchronizeAssignableTableInlineSizeAndColumnsFixed (table/
// table_layout_utils.cc:776-923 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// fixed columns scale up to fill the table when there are no auto columns
// (:822-830), percentage columns take their share of the table (:861-892),
// and auto columns share what is left equally (:893-917). The table's used
// width is never below its captions' min-content (table/
// table_layout_algorithm.cc:248-250).
func TestTableFixedLayoutDistribution(t *testing.T) {
	td := func(id, style string) string {
		return `<td id="` + id + `" style="padding:0;` + style + `"></td>`
	}
	table := func(style, caption, cells string) string {
		return `<table id="t" style="table-layout:fixed;border-spacing:0;` + style + `">` + caption + `<tr>` + cells + `</tr></table>`
	}
	cases := []struct {
		name, html    string
		table, c0, c1 float64
	}{
		{"fixed columns scale up", table("width:400px", "", td("c0", "width:50px")+td("c1", "width:150px")), 400, 100, 300},
		{"fixed and auto columns", table("width:400px", "", td("c0", "width:50px")+td("c1", "")), 400, 50, 350},
		{"percent and fixed columns", table("width:400px", "", td("c0", "width:25%")+td("c1", "width:100px")), 400, 100, 300},
		{"caption wider than the table", table("width:100px",
			`<caption style="padding:0">`+inlineBlock("200px")+`</caption>`,
			td("c0", "width:50px")+td("c1", "width:50px")), 200, 100, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkTwoColumnWidths(t, c.html, c.table, c.c0, c.c1)
		})
	}
}
