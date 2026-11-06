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

// Round 2: lines before the explicit grid, and the explicit grid's size.
// An in-flow item with a line before line 1 starts at line 1 and keeps its
// end line (louis14 has no implicit tracks before the explicit grid); an
// out-of-flow item's start line before the grid is its padding edge, with
// its end line kept (GridPlacement::ResolveOutOfFlowItemGridLines,
// grid_placement.cc:684-700). The explicit grid is as large as its
// template areas (GridLineResolver::ExplicitGridColumnCount,
// grid_line_resolver.cc:525-539) @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestGridLinesOutsideTheExplicitGrid(t *testing.T) {
	for _, tc := range []struct {
		name, grid, item string
		wantX, wantW     float64
	}{
		{"in-flow -5 / 2", `grid-template-columns:10px 20px 30px`, `grid-column:-5 / 2`, 0, 10},
		{"in-flow span 2 / 2", `grid-template-columns:10px 20px 30px`, `grid-column:span 2 / 2`, 0, 10},
		{"abspos -5 / 2", `grid-template-columns:10px 20px 30px; position:relative`, `position:absolute; inset:0; grid-column:-5 / 2`, 0, 10},
		{"abspos span 2 / 1", `grid-template-columns:10px 20px 30px; position:relative`, `position:absolute; inset:0; grid-column:span 2 / 1`, 0, 0},
		{"abspos 2 / -5", `grid-template-columns:10px 20px 30px; position:relative`, `position:absolute; inset:0; grid-column:2 / -5`, 0, 10},
		{"areas widen the explicit grid", `width:100px; grid-template-columns:10px; grid-template-areas:'a b c'`, `grid-column:2 / -1`, 10, 90},
		{"areas alone", `width:90px; grid-template-areas:'a b c'`, `grid-column:-2 / -1`, 60, 30},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="width:200px"><div style="display:grid; grid-auto-rows:10px; `+tc.grid+`">`+
			`<div id=c style="height:10px; `+tc.item+`"></div></div></div></body>`, nil)
		if c := findBox(fl.root, byID("c")); c == nil || c.X != tc.wantX || c.Width != tc.wantW {
			t.Errorf("%s: #c %s, want x=%v width=%v", tc.name, boxDims(c), tc.wantX, tc.wantW)
		}
	}
}

// The explicit grid's tracks exist whatever is placed, the template areas
// included (GridLineResolver::ExplicitGridColumnCount / RowCount,
// grid_line_resolver.cc:525-555 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// areas 'a b c' make three columns, so three auto-placed children share one
// row, and an out-of-flow grid-column:1/2 with no in-flow child is the first
// of the three 30px columns.
func TestExplicitGridTracksFromAreasExist(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; width:90px; grid-auto-rows:10px; grid-template-areas:'a b c'">`+
		`<div id=a></div><div id=b></div><div id=c></div></div></body>`, nil)
	for i, id := range []string{"a", "b", "c"} {
		if b := findBox(fl.root, byID(id)); b == nil || b.X != float64(30*i) || b.Y != 0 {
			t.Errorf("auto-placed #%s %s, want at (%d,0)", id, boxDims(b), 30*i)
		}
	}
	fl = layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; position:relative; width:90px; height:10px; grid-template-areas:'a b c'">`+
		`<div id=o style="position:absolute; inset:0; grid-column:1 / 2"></div></div></body>`, nil)
	if o := findBox(fl.root, byID("o")); o == nil || o.X != 0 || o.Width != 30 {
		t.Errorf("abspos grid-column:1/2 %s, want x=0 width=30", boxDims(o))
	}
}
