package layout

import (
	"fmt"
	"strings"
	"testing"
)

// TestColumnWrap_BalancedLastRowKeepsAllContent (LOU-488 item 2): a balanced
// multicol with column-height and column-wrap:wrap lays every row out with
// Blink's balancing loop. The last row holds four unbreakable 20px lines; the
// initial balanced estimate (80/3 ≈ 26.7px) fits one line per column, so the
// loop stretches by the minimal space shortage to 40px and no line is dropped
// (column_layout_algorithm.cc:1134-1191 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each row is capped by the row
// height (ConstrainColumnBlockSize, :1806-1821).
//
// Layout: 340px wide, 3 columns, 20px gaps -> 100px columns at x 0/120/240;
// rows of 40px at y 0/60/120 (20px row gap). Rows 1-2 hold six lines each
// (two per column); row 3 holds b12..b15 balanced two per column.
func TestColumnWrap_BalancedLastRowKeepsAllContent(t *testing.T) {
	var lines strings.Builder
	for i := range 16 {
		fmt.Fprintf(&lines, `<div id=b%d style="line-height:20px">x</div>`, i)
	}
	rects := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div style="width:340px;columns:3;gap:20px;column-height:40px;column-wrap:wrap;orphans:1;widows:1">`+
		lines.String()+`</div>`)
	got := map[string][]fragRect{}
	want := map[string][]fragRect{}
	for i := range 16 {
		id := fmt.Sprintf("b%d", i)
		// The block's own box comes first; its text box follows.
		if len(rects[id]) > 0 {
			got[id] = rects[id][:1]
		}
		row, inRow := i/6, i%6
		x := float64(inRow/2) * 120
		y := float64(row)*60 + float64(inRow%2)*20
		want[id] = []fragRect{{x, y, 100, 20}}
	}
	checkRects(t, got, want)
}
