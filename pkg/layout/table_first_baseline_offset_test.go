package layout

import "testing"

// A table's first baseline is its first row's baseline where that row sits:
// below the top captions and the leading border spacing (the first
// section's baseline at its block offset, TableLayoutAlgorithm::
// GenerateFragment, table/table_layout_algorithm.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An inline-table's line aligns
// x with it: a 10px top caption moves x down 10, a 7px leading border
// spacing 7.
func TestTableFirstBaselineBelowCaptionsAndSpacing(t *testing.T) {
	xY := func(tbl string) float64 {
		r := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="font:10px/1 Ahem"><span id="x">x</span>`+tbl+`</div>`)
		return findBoxByID(r, "x").Y
	}
	plain := xY(`<table style="display:inline-table;border-spacing:0"><tr><td style="padding:0">y</td></tr></table>`)
	if got := xY(`<table style="display:inline-table;border-spacing:0"><caption>cc</caption><tr><td style="padding:0">y</td></tr></table>`); got != plain+10 {
		t.Errorf("top caption: x at %v, want %v", got, plain+10)
	}
	if got := xY(`<table style="display:inline-table;border-spacing:0 7px"><tr><td style="padding:0">y</td></tr></table>`); got != plain+7 {
		t.Errorf("border spacing: x at %v, want %v", got, plain+7)
	}
}
