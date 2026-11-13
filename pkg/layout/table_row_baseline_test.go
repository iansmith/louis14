package layout

import "testing"

// zeroBaselineRow's only cell's only line has a real baseline of 0, above
// padding (a style for the cell, e.g. 20px of block-end padding).
func zeroBaselineRow(cellPadding string) string {
	return `<tr><td style="padding:` + cellPadding + `"><div style="font-size:0;line-height:0">X</div></td></tr>`
}

// A table's first and last baselines are its first and last rows', and
// every row has one: its largest baseline-aligned cell ascent, else its
// block size less the fallback cell descent, else its top. A baseline of 0
// is a real one, not "none". Mirrors RowBaselineTabulator::ComputeBaseline
// (table/table_layout_utils.cc:1725-1733), TableTypes::Row::baseline
// (table_layout_algorithm_types.h:186) and the sections' and table's
// first/last baselines (table_section_layout_algorithm.cc:113-158,
// table_layout_algorithm.cc:1391-1394, :1623-1629 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each inline-table #c follows
// an "X" and a 30px inline-block in #w (the harness of
// TestAtomicInlineBaseline); want is #c's offset in #w and #w's height,
// from Chrome headless.
func TestTableRowBaseline(t *testing.T) {
	inlineTable := func(rows string) string {
		return `<table id="c" style="display:inline-table;border-spacing:0">` + rows + `</table>`
	}
	cases := []struct {
		name, c string
		y, h    float64
	}{
		{"row baseline 0 in a 0px row", inlineTable(zeroBaselineRow("0")), 30, 32},
		{"row baseline 0 above 20px of padding", inlineTable(zeroBaselineRow("0 0 20px 0")), 30, 50},
		{"an empty cell's row baseline is its bottom",
			inlineTable(`<tr><td style="padding:0;height:20px;width:20px"></td></tr>`), 10, 32},
		{"control: a cell with text", inlineTable(`<tr><td style="padding:0">X</td></tr>`), 20, 32},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
				`<div id="w">X<span style="display:inline-block;height:30px"></span>`+c.c+`</div>`)
			w, cb := findBoxByID(r, "w"), findBoxByID(r, "c")
			if w == nil || cb == nil {
				t.Fatal("no #w or #c box")
			}
			if got := cb.Y - w.Y; got != c.y {
				t.Errorf("#c at %v, want %v", got, c.y)
			}
			if w.Height != c.h {
				t.Errorf("#w height %v, want %v", w.Height, c.h)
			}
		})
	}
}

// A table flex item aligns by its first (last) baseline, its first (last)
// row's: here 0, above 20px of padding. The other item's baseline is 33
// (25px of padding and an Ahem ascent of 8), so the line's shared baseline
// puts #c 33 below #w's top; want is from Chrome headless.
func TestTableRowBaselineInFlex(t *testing.T) {
	for _, align := range []string{"baseline", "last baseline"} {
		t.Run(align, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
				`<div id="w" style="display:flex;align-items:`+align+`"><div style="padding-top:25px">X</div>`+
				`<table id="c" style="border-spacing:0">`+zeroBaselineRow("0 0 20px 0")+`</table></div>`)
			w, cb := findBoxByID(r, "w"), findBoxByID(r, "c")
			if w == nil || cb == nil {
				t.Fatal("no #w or #c box")
			}
			if got := cb.Y - w.Y; got != 33 {
				t.Errorf("#c at %v, want 33", got)
			}
			if w.Height != 53 {
				t.Errorf("#w height %v, want 53", w.Height)
			}
		})
	}
}
