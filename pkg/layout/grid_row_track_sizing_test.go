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
