package layout

import (
	"testing"

	"louis14/pkg/css"
)

// Column track sizing, as Blink's GridTrackSizingAlgorithm::
// ComputeUsedTrackSizes runs it: each track's base size and growth limit are
// resolved from its items' contributions (ResolveIntrinsicTrackSizes,
// spanning items after single-span ones, distributed by growth potential),
// free space then grows base sizes up to their growth limits
// (MaximizeTracks), flexible tracks take an fr share (ExpandFlexibleTracks),
// and any space left is added equally to auto tracks (StretchAutoTracks).
// layout/grid/grid_track_sizing_algorithm.cc:305-345 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestGridColumnTrackSizing(t *testing.T) {
	item := func(id, style string) string {
		return `<div id=` + id + ` style="height:10px; ` + style + `"></div>`
	}
	for _, tc := range []struct {
		name, grid, items string
		id                string
		wantX, wantGridW  float64
	}{
		// Each auto column is sized from its own item, not an equal share:
		// columns 50 and 80.
		{"auto columns, unequal items", `display:inline-grid; grid-auto-flow:column`,
			item("a", "width:50px") + item("b", "width:80px"), "b", 50, 130},
		// A spanning item's extra space goes to the track that can still
		// grow: column 1 is 30 from its own item, so column 2 takes the
		// spanner's remaining 70.
		{"spanner fills the growable track", `display:inline-grid; grid-template-columns:auto auto`,
			item("a", "width:30px") + item("s", "grid-column:1/3; width:100px") + item("c", "grid-column:2; width:10px"), "c", 30, 100},
		// Free space in a definite 300px grid: columns 50 and 80 reach their
		// growth limits, then the remaining 170 is added equally, so 135
		// and 165.
		{"stretch is additive", `display:grid; width:300px; grid-template-columns:auto auto`,
			item("a", "width:50px") + item("b", "width:80px"), "b", 135, 300},
		// fit-content(60px) grows its base from min-content toward its
		// limit: the item's 100px max-content is clamped to 60.
		{"fit-content clamps", `display:inline-grid; grid-template-columns:fit-content(60px) auto`,
			`<div style="height:10px"><span style="display:inline-block;width:40px"></span><span style="display:inline-block;width:60px"></span></div>` +
				item("b", "width:20px"), "b", 60, 80},
		// The ticket's case: an indefinite grid sizes minmax(0, .5fr) from
		// the item's max-content (200 / max(0.5, 1) = 200 → the track is
		// 100), and the rerun at that definite size gives the track 50.
		// grid-template-flexible-rerun-track-sizing.html.
		{"flex fraction rerun", `display:inline-grid; grid-template-columns:minmax(0, .5fr) auto`,
			item("a", "width:200px") + item("b", "width:0"), "b", 50, 100},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0; font-size:0"><div id=g style="`+tc.grid+`">`+tc.items+`</div></body>`, nil)
		g, b := findBox(fl.root, byID("g")), findBox(fl.root, byID(tc.id))
		if g == nil || b == nil || b.X != tc.wantX || g.Width != tc.wantGridW {
			t.Errorf("%s: grid %s, #%s %s; want grid width %v, #%s at x=%v", tc.name, boxDims(g), tc.id, boxDims(b), tc.wantGridW, tc.id, tc.wantX)
		}
	}
}

// GridSizingTrackCollection::InitializeSets: a fixed minimum is the initial
// base size and a fixed maximum the initial growth limit; intrinsic minimums
// start at 0 and intrinsic or flexible maximums are infinite
// (grid_track_collection.cc:1207-1252 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestNewGridSetsInitialSizes(t *testing.T) {
	inf := gridIndefinite
	sets := newGridSets([]css.GridTrack{
		{Size: 40}, {Auto: true}, {Fr: 1}, {IsMinMax: true, MinSize: 10, MaxSize: 30},
		{IsMinMax: true, MinSize: 50, MaxSize: 30}, {IsFitContent: true, FitContentMax: 60},
		{Percent: 50}, {Percent: 50},
	}, 200)
	sets = append(sets, newGridSets([]css.GridTrack{{Percent: 50}}, gridIndefinite)...)
	for i, want := range [][2]float64{{40, 40}, {0, inf}, {0, inf}, {10, 30}, {50, 50}, {0, inf}, {100, 100}, {100, 100}, {0, inf}} {
		if got := [2]float64{sets[i].baseSize, sets[i].growthLimit}; got != want {
			t.Errorf("set %d: base, growth limit %v, want %v", i, got, want)
		}
	}
	if sets[5].fitContentLimit != 60 {
		t.Errorf("fit-content limit %v, want 60", sets[5].fitContentLimit)
	}
}
