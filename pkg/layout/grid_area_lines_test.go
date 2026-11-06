package layout

import "testing"

// grid-area is a shorthand for grid-row-start / grid-column-start /
// grid-row-end / grid-column-end; an omitted column-start or row-end copies
// a <custom-ident> row-start, an omitted column-end copies a <custom-ident>
// column-start, and anything else is auto (GridArea::ParseShorthand,
// css/properties/shorthands/shorthands_custom.cc:3620-3675 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Mirrors WPT
// css-contain/contain-size-064.
func TestGridAreaLineNumbers(t *testing.T) {
	for _, tc := range []struct {
		name, area   string
		wantX, wantY float64
		wantW        float64
	}{
		{"row / column", `1 / 3`, 30, 0, -1},    // column line 3: an implicit third column
		{"row only", `2`, -1, 10, -1},           // column auto
		{"all four", `1 / 1 / 2 / 3`, 0, 0, 30}, // spans both columns
		{"span end", `1 / 1 / auto / span 2`, 0, 0, 30},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; width:100px; grid-template-columns:10px 20px; grid-auto-rows:10px">`+
			`<div id=c style="grid-area:`+tc.area+`"></div></div></body>`, nil)
		c := findBox(fl.root, byID("c"))
		if c == nil || (tc.wantX >= 0 && c.X != tc.wantX) || c.Y != tc.wantY || (tc.wantW >= 0 && c.Width != tc.wantW) {
			t.Errorf("grid-area:%s: #c %s; want x=%v y=%v width=%v", tc.area, boxDims(c), tc.wantX, tc.wantY, tc.wantW)
		}
	}
}
