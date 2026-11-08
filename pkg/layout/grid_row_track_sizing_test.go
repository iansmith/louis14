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
