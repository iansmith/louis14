package layout

import "testing"

// A table's min/max-content sizes come from its columns, not from
// measuring it as a block: TableLayoutAlgorithm::ComputeMinMaxSizes
// (table/table_layout_algorithm.cc:701-732 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) sums the column constraints in
// ComputeGridInlineMinMax (table/table_layout_utils.cc:1743-1830), where a
// percentage column raises the max-content size.
func TestTableComputeMinMaxSizes(t *testing.T) {
	ib := inlineBlock
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
		// A cell width of calc(N% + 0px) is a percentage column, as N% is:
		// Blink's InlineSizesFromStyle reads a calc() whose pixel part is 0
		// as its percent (table/table_layout_algorithm_types.cc:62-74;
		// css-tables/calc-percent-plus-0px-auto). A 50% column beside a
		// 100px one makes the table 100 / (100% - 50%) = 200px wide.
		{"percent beside px", `<table id="t" style="border-spacing:0"><tr><td style="padding:0;width:50%">x</td>` +
			`<td style="padding:0;width:100px">y</td></tr></table>`, 200},
		{"calc percent plus 0px beside px", `<table id="t" style="border-spacing:0"><tr><td style="padding:0;width:calc(50% + 0px)">x</td>` +
			`<td style="padding:0;width:100px">y</td></tr></table>`, 200},
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
		`<div style="display:table-cell">` + inlineBlock("10px") + `</div></div></div>`
	root := layoutRootTestHTML(t, src)
	table, pct := findBoxByID(root, "t"), findBoxByID(root, "p")
	if table == nil || pct == nil {
		t.Fatal("no #t or #p box")
	}
	if table.Width != 100 || pct.Width != 90 {
		t.Errorf("table width = %v, percent cell width = %v; want 100 and 90", table.Width, pct.Width)
	}
}

// inlineBlock is a 5px-high inline-block w wide.
func inlineBlock(w string) string {
	return `<span style="display:inline-block;width:` + w + `;height:5px"></span>`
}

// twoColumnTable is a table #t with cells #c0 and #c1 whose only content
// is an inline-block 20px and 60px wide, so its grid min/max-content are
// 80px; style is appended to the table's style.
func twoColumnTable(style string) string { return twoColumnTableWithCaption(style, "") }

// twoColumnTableWithCaption is twoColumnTable with a caption #cap whose
// content is caption, when not empty.
func twoColumnTableWithCaption(style, caption string) string {
	td := func(id, w string) string {
		return `<td id="` + id + `" style="padding:0">` + inlineBlock(w) + `</td>`
	}
	if caption != "" {
		caption = `<caption id="cap" style="padding:0">` + caption + `</caption>`
	}
	return `<table id="t" style="border-spacing:0;` + style + `">` + caption + `<tr>` + td("c0", "20px") + td("c1", "60px") + `</tr></table>`
}

// checkTwoColumnWidths lays out body and checks the widths of
// twoColumnTable's table and cells.
func checkTwoColumnWidths(t *testing.T, body string, table, c0, c1 float64) {
	t.Helper()
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+body)
	tb, b0, b1 := findBoxByID(root, "t"), findBoxByID(root, "c0"), findBoxByID(root, "c1")
	if tb == nil || b0 == nil || b1 == nil {
		t.Fatal("missing #t/#c0/#c1")
	}
	if tb.Width != table || b0.Width != c0 || b1.Width != c1 {
		t.Errorf("table/c0/c1 widths = %v/%v/%v, want %v/%v/%v", tb.Width, b0.Width, b1.Width, table, c0, c1)
	}
}

// When the parent fixes the table's inline size (a stretched grid item, a
// flex item that grew), that size is the assignable table inline size, not
// capped at the grid max-content: ComputeAssignableTableInlineSize returns
// early when space.IsFixedInlineSize() (table/table_layout_algorithm.cc:
// 231-234 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Above the max guess the
// columns (max-content 20 and 60) share the 400px in proportion to their
// max-content (DistributeInlineSizeToComputedInlineSizeAuto): 100 and 300.
func TestTableFixedInlineSizeIsAssignable(t *testing.T) {
	cases := []struct{ name, html string }{
		{"stretched grid item", `<div style="display:grid;width:400px">` + twoColumnTable("") + `</div>`},
		{"flex item that grew", `<div style="display:flex;width:400px">` + twoColumnTable("flex-grow:1") + `</div>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkTwoColumnWidths(t, c.html, 400, 100, 300) })
	}
}

// An auto table's min-width enters the used table width before the columns
// are distributed: ComputeUsedInlineSizeForTableFragment clamps the
// fit-content width over the grid min/max to the table's min/max inline
// sizes (length_utils.cc:527-598, :618-649 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and
// ComputeAssignableTableInlineSize distributes that (table/
// table_layout_algorithm.cc:236-257). A 200px min-width over columns of
// max-content 20 and 60 grows them in proportion: 50 and 150.
func TestTableMinWidthIsDistributed(t *testing.T) {
	for _, display := range []string{"table", "inline-table"} {
		t.Run(display, func(t *testing.T) {
			checkTwoColumnWidths(t, `<div style="width:400px">`+twoColumnTable("display:"+display+";min-width:200px")+`</div>`, 200, 50, 150)
		})
	}
}

// A caption's min-content width widens the used table width before the
// columns are distributed: ComputeAssignableTableInlineSize takes the max of
// the used width and the caption constraint's min (ComputeCaptionConstraint,
// table/table_layout_algorithm.cc:38-58, :248-250 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A caption holding a 200px
// inline-block over columns of max-content 20 and 60 grows them in
// proportion: 50 and 150.
func TestTableCaptionMinWidthIsDistributed(t *testing.T) {
	caption := inlineBlock("200px")
	for _, display := range []string{"table", "inline-table"} {
		t.Run(display, func(t *testing.T) {
			checkTwoColumnWidths(t, `<div style="width:400px">`+twoColumnTableWithCaption("display:"+display, caption)+`</div>`, 200, 50, 150)
		})
	}
}

// A caption lays out at the table's final border-box inline size, not the
// table's container: GenerateFragment gives it an available inline size of
// container_builder_.InlineSize() (table/table_layout_algorithm.cc:
// 1230-1243 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and an auto-width
// caption stretches to it (CreateCaptionConstraintSpace, :61-90).
func TestTableCaptionLaysOutAtTableInlineSize(t *testing.T) {
	cases := []struct {
		name, caption string
		want          float64
	}{
		{"caption wider than the columns", inlineBlock("200px"), 200},
		{"caption narrower than the columns", inlineBlock("10px"), 80},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="width:400px">`+twoColumnTableWithCaption("", c.caption)+`</div>`)
			tb, capBox := findBoxByID(root, "t"), findBoxByID(root, "cap")
			if tb == nil || capBox == nil {
				t.Fatal("missing #t/#cap")
			}
			if tb.Width != c.want || capBox.Width != c.want {
				t.Errorf("table/caption widths = %v/%v, want %v", tb.Width, capBox.Width, c.want)
			}
		})
	}
}
