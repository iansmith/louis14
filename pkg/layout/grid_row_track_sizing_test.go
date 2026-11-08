package layout

import "testing"

// Grid rows run the track sizing algorithm (GridLayoutAlgorithm::Layout's
// CompleteTrackSizingAlgorithm(kForRows, kLayout), grid_layout_algorithm.cc:
// 429-430; GridTrackSizingAlgorithm, grid_track_sizing_algorithm.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - free space is added equally to auto rows (StretchAutoTracks), so rows
//     of 20 and 50 in a 200px grid become 85 and 115 (LOU-757);
//   - with an indefinite block size, auto rows stretch into a definite
//     min-height instead (:1088-1095);
//   - a spanning item's extra space, past both rows' growth limits, is
//     shared equally: rows of 30 and 10 with a 100px spanner become 60 and
//     40;
//   - a flexible row takes its fr share of the definite block size, after
//     its item's minimum.
func TestGridRowTrackSizing(t *testing.T) {
	item := func(id, style string) string {
		return `<div id=` + id + ` style="` + style + `"></div>`
	}
	for _, tc := range []struct {
		name, grid, items, id string
		wantY, wantH          float64 // wantH < 0: height not checked
	}{
		{"additive stretch", `height:200px; grid-template-rows:auto auto`,
			item("a", "height:20px") + item("b", "height:50px"), "b", 85, -1},
		{"stretch into min-height", `min-height:200px; grid-template-rows:auto auto`,
			item("a", "height:20px") + item("b", "height:50px"), "b", 85, -1},
		{"spanner", `grid-template-rows:auto auto; align-content:start`,
			item("a", "height:30px") + item("s", "grid-row:1/3; height:100px") + item("c", "grid-row:2; height:10px"), "c", 60, -1},
		{"flexible rows", `height:100px; grid-template-rows:1fr 3fr`,
			item("a", "") + item("b", ""), "b", 25, 75},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; width:100px; `+tc.grid+`">`+tc.items+`</div></body>`, nil)
		if b := findBox(fl.root, byID(tc.id)); b == nil || b.Y != tc.wantY || (tc.wantH >= 0 && b.Height != tc.wantH) {
			t.Errorf("%s: #%s %s, want y=%v height=%v", tc.name, tc.id, boxDims(b), tc.wantY, tc.wantH)
		}
	}
}

// Row contributions are measured with the item's grid area block size
// indefinite (CreateConstraintSpaceForMeasure, grid_layout_algorithm.cc:
// 1460-1500 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a percentage
// height contributes as auto: in a 100px grid with rows 30px 1fr, a
// height:100% item in the 1fr row leaves it its 70 of free space. Mirrors
// WPT css-grid/grid-definition/fr-unit.
func TestGridRowContributionPercentageHeightIsAuto(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; width:100px; height:100px; grid-template-rows:30px 1fr">`+
		`<div style="height:30px"></div><div id=b style="height:100%"></div></div></body>`, nil)
	if b := findBox(fl.root, byID("b")); b == nil || b.Y != 30 || b.Height != 70 {
		t.Errorf("#b %s, want y=30 height=70", boxDims(b))
	}
}

// With an indefinite block size, rows that depend on the available size
// (flexible or percentage rows) are sized again at the grid's resolved
// block size (NeedsAdditionalLayoutPass, grid_layout_utils.cc; the
// additional pass, grid_layout_algorithm.cc:443-490); under size
// containment that block size is resolved before rows are sized (:41-55) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
//   - minmax(0, .5fr) rows: the first pass sizes the row to half the
//     item's 200 (100); at that 100, the rerun gives the row 50.
//   - a 1fr row with min-height:100px: 100 at the resolved block size.
//   - the same contained: 100 too.
func TestGridRowsRerunAtResolvedBlockSize(t *testing.T) {
	for _, tc := range []struct {
		name, grid, items string
		wantRow2          float64 // #b's y: the first row's size
	}{
		{"flexible rerun", `grid-template-rows:minmax(0, .5fr) auto`, `<div style="height:200px"></div><div id=b></div>`, 50},
		{"1fr into min-height", `min-height:100px; grid-template-rows:1fr auto`, `<div style="height:10px"></div><div id=b></div>`, 100},
		{"contained 1fr into min-height", `contain:size; min-height:100px; grid-template-rows:1fr auto`, `<div style="height:10px"></div><div id=b></div>`, 100},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; width:100px; `+tc.grid+`">`+tc.items+`</div></body>`, nil)
		if b := findBox(fl.root, byID("b")); b == nil || b.Y != tc.wantRow2 {
			t.Errorf("%s: #b %s, want y=%v", tc.name, boxDims(b), tc.wantRow2)
		}
	}
}

// A grid's intrinsic inline size runs the additional column pass when an
// item's column contribution depends on its rows (ComputeTotalColumnSize,
// grid_layout_algorithm.cc:287-314 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an orthogonal item with two
// 50x100 inline-blocks first measures one line, 50 wide; the grid's 100px
// row then makes it wrap to two, 100 wide, so the grid's min-content is
// 100. Mirrors WPT css-grid/grid-with-orthogonal-child-within-flexbox.
func TestGridAdditionalColumnPassForOrthogonalItem(t *testing.T) {
	ib := `<span style="display:inline-block; width:50px; height:100px"></span>`
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div id=g style="display:inline-grid; height:100px; width:min-content">`+
		`<div style="writing-mode:vertical-rl; line-height:0">`+ib+ib+`</div></div></body>`, nil)
	if g := findBox(fl.root, byID("g")); g == nil || g.Width != 100 {
		t.Errorf("grid %s, want 100 wide", boxDims(g))
	}
}

// Layout runs the additional column pass too: the row pass sees an
// orthogonal item's rows become definite (MayChangeBlockSizeDependentGrid-
// ItemContributions, grid_layout_algorithm.cc:1146-1162, from
// CompleteTrackSizingAlgorithm, :1191-1262), so the columns are sized again
// with the item measured against its row and the rows after them (:478-484
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). In a 300x100 grid with
// columns auto 1fr, the orthogonal item first measures one line, 50 wide;
// against its 100px row it wraps to two, 100 wide, so the 1fr column
// starts at 100.
func TestGridLayoutAdditionalColumnPass(t *testing.T) {
	ib := `<span style="display:inline-block; width:50px; height:100px"></span>`
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; width:300px; height:100px; grid-template-columns:auto 1fr">`+
		`<div id=o style="writing-mode:vertical-rl; line-height:0">`+ib+ib+`</div><div id=b></div></div></body>`, nil)
	o, b := findBox(fl.root, byID("o")), findBox(fl.root, byID("b"))
	if o == nil || b == nil || o.Width != 100 || b.X != 100 {
		t.Errorf("orthogonal item %s, #b %s; want the item 100 wide and #b at x=100", boxDims(o), boxDims(b))
	}
}

// Round 1 of the #411 review.
//   - An orthogonal child of a block is measured against the block's own
//     available block size (ChildAvailableSize, block_layout_algorithm.cc:
//     459-462): a width:max-content, height:100px block around vertical-rl
//     text wider than 100 wraps it, so the block is two lines wide.
//   - A percentage height is re-measured with the grid area indefinite and
//     at its columns, not at the inline size the percentage inflated: an
//     aspect-ratio:1, height:100% box in an auto row of a 300px grid
//     contributes as auto, 0, not 300 (CreateConstraintSpaceForMeasure,
//     grid_layout_algorithm.cc:1460-1500).
//   - Rows are aligned against the resolved block size: min-height:200px,
//     align-content:center, rows of 70 start at 65 (:458-473).
//   - A percentage row gap resolves against the grid's block size, so with
//     an indefinite one it is resolved in the rerun (:443-490): 10% of the
//     grid's 100px min-height is 10.
//
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestGridReviewRound1(t *testing.T) {
	ib := `<span style="display:inline-block; width:20px; height:60px"></span>`
	for _, tc := range []struct {
		name, html, id string
		check          func(b *Box) bool
		want           string
	}{
		{"orthogonal child of a definite-height block",
			`<div id=p style="width:max-content; height:100px"><div style="writing-mode:vertical-rl; line-height:0">` + ib + ib + `</div></div>`,
			"p", func(b *Box) bool { return b.Width == 40 }, "40 wide"},
		{"percentage height re-measured at its columns",
			`<div style="display:grid; height:300px; grid-template-rows:auto 1fr; align-content:start"><div style="height:100%; aspect-ratio:1; justify-self:start"></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 0 }, "at y=0 (the first row 0)"},
		{"align-content against min-height",
			`<div style="display:grid; min-height:200px; align-content:center; grid-template-rows:30px 40px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 95 }, "at y=95 (rows start at 65)"},
		{"percentage row gap",
			`<div style="display:grid; min-height:100px; row-gap:10%; grid-template-rows:20px 20px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 30 }, "at y=30"},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0; font-size:0">`+tc.html+`</body>`, nil)
		if b := findBox(fl.root, byID(tc.id)); b == nil || !tc.check(b) {
			t.Errorf("%s: #%s %s, want %s", tc.name, tc.id, boxDims(b), tc.want)
		}
	}
}

// The additional pass sizes the rows again at the RESOLVED block size
// (grid_layout_algorithm.cc:443-490 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), not at the indefinite one: an
// orthogonal item in row 1 first measures against the ICB, wrapping to two
// lines (100 wide); against its 900px row it is one line (50), so column 2
// widens and the wrapping item in row 2 needs one line, not two. The grid's
// block size stays the first pass's, so the rerun stretches row 2 back to
// its first-pass 40, and the auto-height item with it.
func TestGridAdditionalPassRowsAtResolvedBlockSize(t *testing.T) {
	tall := `<span style="display:inline-block; width:50px; height:200px"></span>`
	wide := `<span style="display:inline-block; width:50px; height:20px"></span>`
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0; font-size:0; line-height:0"><div style="display:grid; width:300px; grid-template-columns:auto 1fr; grid-template-rows:minmax(0, 900px) auto">`+
		`<div style="writing-mode:vertical-rl">`+tall+tall+tall+tall+`</div>`+
		`<div id=w style="grid-column:2; grid-row:2">`+wide+wide+wide+wide+wide+`</div></div></body>`, nil)
	if w := findBox(fl.root, byID("w")); w == nil || w.Height != 40 || w.X != 50 {
		t.Errorf("row-2 item %s, want at x=50, 40 tall (row 2 stretched to its first-pass size)", boxDims(w))
	}
}

// TestGapResolutionAlignmentAndChildBlockSizes covers how gaps parse and
// resolve, how grid tracks align in the resolved size, and the block sizes
// a container hands its children while it is intrinsically sized.
//   - gap and grid-gap expand into row-gap / column-gap at cascade time
//     (grid-gap is gap's alias), so a later grid-gap replaces an earlier
//     row-gap, and a calc() value is one component.
//   - A gap resolves with the element's conversion data (1em) and a
//     calc() percentage against its axis (CalculateGutterSize,
//     grid_track_sizing_algorithm.cc:156-176). In an auto-height grid a
//     percentage row gap is 0 in the first pass, so calc(5px + 1%) gives a
//     25px block size, and the additional pass it forces sizes the rows
//     again with 5px + 1% of 25 (GridLayoutAlgorithm::LayoutInternal,
//     grid_layout_algorithm.cc:443-487): 15.25.
//   - Under size containment a percentage row gap resolves against the
//     contained block size (grid_available_size_, :41-55).
//   - Each gap component is normal or a non-negative <length-percentage>,
//     or the declaration drops and the earlier longhand stays
//     (Gap::ParseShorthand, shorthands_custom.cc:3583-3605;
//     ConsumeGapLength, css_parsing_utils.cc:6102-6110).
//   - Rows align with negative free space too (unsafe by default):
//     center places 200px of rows in 100px at -50. space-between, -around
//     and -evenly distribute only non-negative free space, start-aligned
//     with their gaps otherwise, rows and columns alike
//     (ComputeFirstSetGeometry, grid_track_sizing_algorithm.cc:221-261).
//   - An orthogonal child is measured against its parent's min/max-clamped
//     definite block size (ChildAvailableSize): height:300px with
//     max-height:100px wraps vertical-rl content taller than 100. A
//     percentage block size resolves against that clamped size too
//     (CalculateChildPercentageSize, length_utils.cc:1810-1828).
//   - A multicol's percentage row gap resolves against its definite block
//     size (ResolveRowGapForMulticol with ChildAvailableSize().block_size,
//     column_layout_algorithm.cc:268-269).
//
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestGapResolutionAlignmentAndChildBlockSizes(t *testing.T) {
	ib := `<span style="display:inline-block; width:20px; height:60px"></span>`
	for _, tc := range []struct {
		name, css, html, id string
		check               func(b *Box) bool
		want                string
	}{
		{"1em row gap", ``, `<div style="display:grid; font-size:20px; row-gap:1em; grid-template-rows:10px 10px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 30 }, "at y=30"},
		{"later grid-gap beats row-gap", `.a{row-gap:5px} .b{grid-gap:30px}`, `<div class="a b" style="display:grid; grid-template-rows:10px 10px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 40 }, "at y=40"},
		{"calc() column gap with a percentage", ``, `<div style="display:grid; width:200px; column-gap:calc(10% + 4px); grid-template-columns:50px 50px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.X == 74 }, "at x=74"},
		{"grid-gap with a calc() component", ``, `<div style="display:grid; width:200px; grid-gap:calc(5px + 1%) 20px; grid-template-columns:50px 50px; grid-template-rows:10px 10px"><div></div><div id=c></div><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.X == 70 && b.Y == 15.25 }, "at (70,15.25)"},
		{"contained percentage row gap", ``, `<div style="display:grid; contain:size; min-height:100px; row-gap:10%; grid-template-rows:20px 20px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 30 }, "at y=30"},
		{"unsafe row alignment", ``, `<div style="display:grid; height:100px; align-content:center; grid-template-rows:100px 100px"><div id=b></div><div></div></div>`,
			"b", func(b *Box) bool { return b.Y == -50 }, "at y=-50"},
		{"orthogonal child of a max-height block", ``, `<div id=p style="width:max-content; height:300px; max-height:100px"><div style="writing-mode:vertical-rl; line-height:0">` + ib + ib + `</div></div>`,
			"p", func(b *Box) bool { return b.Width == 40 }, "40 wide"},
		{"space-between with negative free space", ``, `<div style="display:grid; height:100px; align-content:space-between; grid-template-rows:100px 100px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 100 }, "at y=100"},
		{"space-around with negative free space", ``, `<div style="display:grid; height:100px; align-content:space-around; grid-template-rows:100px 100px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 100 }, "at y=100"},
		{"space-evenly columns with negative free space", ``, `<div style="display:grid; width:100px; justify-content:space-evenly; grid-template-columns:100px 100px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.X == 100 }, "at x=100"},
		{"negative grid-gap drops", `.a{row-gap:10px} .b{grid-gap:-5px}`, `<div class="a b" style="display:grid; grid-template-rows:10px 10px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 20 }, "at y=20"},
		{"gap with an invalid component drops", `.a{row-gap:10px} .b{gap:5px auto}`, `<div class="a b" style="display:grid; grid-template-rows:10px 10px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 20 }, "at y=20"},
		{"negative row-gap drops", `.a{row-gap:10px} .b{row-gap:-5px}`, `<div class="a b" style="display:grid; grid-template-rows:10px 10px"><div></div><div id=b></div></div>`,
			"b", func(b *Box) bool { return b.Y == 20 }, "at y=20"},
		{"percentage child of a max-height block", ``, `<div id=p style="width:max-content; height:300px; max-height:100px"><div style="height:50%; aspect-ratio:1"></div></div>`,
			"p", func(b *Box) bool { return b.Width == 50 }, "50 wide (its child 50% of 100 tall, square)"},
		{"multicol percentage row gap", ``, `<div style="height:400px; columns:1; column-gap:0; column-fill:auto; column-height:100px; column-wrap:wrap; row-gap:10%"><div style="height:100px"></div><div id=b style="height:100px"></div></div>`,
			"b", func(b *Box) bool { return b.Y == 140 }, "at y=140"},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><style>`+tc.css+`</style><body style="margin:0; font-size:0">`+tc.html+`</body>`, nil)
		if b := findBox(fl.root, byID(tc.id)); b == nil || !tc.check(b) {
			t.Errorf("%s: #%s %s, want %s", tc.name, tc.id, boxDims(b), tc.want)
		}
	}
}
