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

// Grid line placement, resolved from the four longhands as Blink's
// GridLineResolver::ResolveGridPositionsFromStyle does
// (core/layout/grid/grid_line_resolver.cc:760-879 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), after grid-area / grid-row /
// grid-column expand into them at cascade time (GridArea::ParseShorthand,
// css/properties/shorthands/shorthands_custom.cc:3620-3690;
// ConsumeGridItemPositionShorthand, css_parsing_utils.cc:7569-7595). The
// grid is 10px 20px 30px by three 10px rows unless the case says otherwise.
func TestGridLinePlacement(t *testing.T) {
	for _, tc := range []struct {
		name, css, grid, item string
		want                  [4]float64 // x, y, width, height
	}{
		// Cascade order: a later grid-area replaces an earlier grid-column's
		// longhands, and a later longhand replaces one of grid-area's.
		{"grid-area after grid-column", `.a{grid-column:3} .b{grid-area:1/1/2/2}`, ``, `class="a b"`, [4]float64{0, 0, 10, 10}},
		{"grid-column-start after grid-area", `.a{grid-area:1/1/2/3} .b{grid-column-start:2}`, ``, `class="a b"`, [4]float64{10, 0, 20, 10}},
		// inherit on grid-area sets each longhand to the parent's.
		{"inherit", ``, `grid-area:1/2/2/3`, `style="grid-area:inherit"`, [4]float64{10, 0, 20, 10}},
		// A lone <custom-ident> start is copied to the omitted end; equal
		// lines then span one track (:871-874).
		{"custom-ident copied to the end", ``, `grid-template-columns:10px [foo] 20px 30px`, `style="grid-column:foo"`, [4]float64{10, 0, 20, 10}},
		{"named line, equal ends", ``, `grid-template-columns:10px [b] 20px 30px; grid-template-rows:10px [b] 10px 10px`, `style="grid-area:b"`, [4]float64{10, 10, 20, 10}},
		// Reversed lines are swapped.
		{"reversed lines", ``, ``, `style="grid-area:3 / 1 / 1 / 2"`, [4]float64{0, 0, 10, 20}},
		// Negative lines count from the explicit grid's end (:776-779).
		{"negative lines", ``, ``, `style="grid-area:1 / 1 / -1 / -1"`, [4]float64{0, 0, 60, 30}},
		// "spanner" is an identifier, not a span; "1 foo" is the first line
		// named foo, not a <custom-ident>, so its end is auto.
		{"ident starting with span", ``, `grid-template-columns:10px [spanner] 20px 30px`, `style="grid-column:spanner"`, [4]float64{10, 0, 20, 10}},
		{"integer and name", ``, `grid-template-columns:10px [foo] 20px 30px`, `style="grid-column:1 foo"`, [4]float64{10, 0, 20, 10}},
		// A <custom-ident> tries the implicit <ident>-start / <ident>-end
		// lines first (:780-800).
		{"implicit area lines", ``, `grid-template-columns:[main-start] 10px 20px [main-end] 30px; grid-template-rows:[main-start] 10px [main-end] 10px 10px`, `style="grid-area:main"`, [4]float64{0, 0, 30, 10}},
	} {
		grid := `display:grid; grid-template-columns:10px 20px 30px; grid-template-rows:10px 10px 10px; ` + tc.grid
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><style>`+tc.css+`</style><body style="margin:0"><div style="width:200px"><div style="`+grid+`">`+
			`<div id=c `+tc.item+`></div></div></div></body>`, nil)
		c := findBox(fl.root, byID("c"))
		if c == nil || [4]float64{c.X, c.Y, c.Width, c.Height} != tc.want {
			t.Errorf("%s: #c %s, want %v", tc.name, boxDims(c), tc.want)
		}
	}
}

// An absolutely positioned grid child's containing block is the grid area
// its longhands give, grid-area included: an auto end is the padding edge,
// a definite one its line (GridPlacement::ResolveOutOfFlowItemGridLines,
// core/layout/grid/grid_placement.cc:659-703 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestOutOfFlowGridAreaLines(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; position:relative; width:60px; grid-template-columns:10px 20px 30px; grid-template-rows:10px 10px 10px">`+
		`<div id=c style="position:absolute; inset:0; grid-area:2 / 2 / 3 / 3"></div></div></body>`, nil)
	if c := findBox(fl.root, byID("c")); c == nil || [4]float64{c.X, c.Y, c.Width, c.Height} != [4]float64{10, 10, 20, 10} {
		t.Errorf("abspos grid-area:2/2/3/3 %s, want (10,10) 20x10", boxDims(c))
	}
}
