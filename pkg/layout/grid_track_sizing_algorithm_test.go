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
		// A spanning item's extra space goes past the growth limits once
		// every spanned track has reached its own: columns 30 and 10 from
		// their items, then the spanner's remaining 60 is shared equally
		// (distribute space beyond limits), so 60 and 40.
		{"spanner beyond limits", `display:inline-grid; grid-template-columns:auto auto`,
			item("a", "width:30px") + item("s", "grid-column:1/3; width:100px") + item("c", "grid-column:2; width:10px"), "c", 60, 100},
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

// ResolveIntrinsicTrackSizes (grid_track_sizing_algorithm.cc:911-1022 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): single-span items set each
// track's base size and growth limit, then a spanner's extra space goes to
// the tracks with growth potential; a fit-content track's growth limit stops
// at its argument.
func TestResolveIntrinsicTrackSizes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tracks []css.GridTrack
		items  []*gridSizingItem
		want   [][2]float64 // base size, growth limit per track
	}{
		{"spanner", []css.GridTrack{{Auto: true}, {Auto: true}},
			[]*gridSizingItem{{start: 0, end: 1, minContent: 30, maxContent: 30}, {start: 0, end: 2, minContent: 100, maxContent: 100}},
			[][2]float64{{30, 30}, {70, 70}}},
		{"spanner across a gap and a fixed track", []css.GridTrack{{Size: 100}, {Auto: true}},
			[]*gridSizingItem{{start: 0, end: 2, minContent: 150, maxContent: 150}},
			[][2]float64{{100, 100}, {50, 50}}},
		{"fit-content", []css.GridTrack{{IsFitContent: true, FitContentMax: 60}},
			[]*gridSizingItem{{start: 0, end: 1, minContent: 40, maxContent: 100}},
			[][2]float64{{40, 60}}},
		{"min-content and max-content", []css.GridTrack{{Auto: true}},
			[]*gridSizingItem{{start: 0, end: 1, minContent: 40, maxContent: 100}},
			[][2]float64{{40, 100}}},
	} {
		a := &gridTrackSizingAlgorithm{sets: newGridSets(tc.tracks, 500), available: 500}
		a.resolveIntrinsicTrackSizes(tc.items)
		for i, w := range tc.want {
			if got := [2]float64{a.sets[i].baseSize, a.sets[i].growthLimit}; got != w {
				t.Errorf("%s: track %d base, growth limit %v, want %v", tc.name, i, got, w)
			}
		}
	}
}

// MaximizeTracks (grid_track_sizing_algorithm.cc:1024-1052 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): free space grows base sizes
// equally up to their growth limits; under a max-content constraint they
// reach their limits, and under a min-content one they stay.
func TestMaximizeTracks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		constraint gridSizingConstraint
		available  float64
		want       [2]float64
	}{
		{"layout, room for both", gridSizingLayout, 500, [2]float64{100, 60}},
		{"layout, the smaller freezes", gridSizingLayout, 150, [2]float64{90, 60}},
		{"max-content", gridSizingMaxContent, gridIndefinite, [2]float64{100, 60}},
		{"min-content", gridSizingMinContent, gridIndefinite, [2]float64{20, 20}},
	} {
		a := &gridTrackSizingAlgorithm{sets: newGridSets([]css.GridTrack{{Auto: true}, {Auto: true}}, tc.available),
			constraint: tc.constraint, available: tc.available}
		a.resolveIntrinsicTrackSizes([]*gridSizingItem{{start: 0, end: 1, minContent: 20, maxContent: 100}, {start: 1, end: 2, minContent: 20, maxContent: 60}})
		a.setIndefiniteGrowthLimitsToBaseSize()
		a.maximizeTracks()
		if got := [2]float64{a.sets[0].baseSize, a.sets[1].baseSize}; got != tc.want {
			t.Errorf("%s: base sizes %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Size containment sizes the grid container as if empty, but its tracks
// still size from its items in layout: GridLayoutAlgorithm::Layout builds
// the sizing tree with its children, ignoring them only for display locks
// (grid_layout_algorithm.cc:416-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Mirrors WPT
// css-contain/contain-size-064.
func TestSizeContainedGridSizesColumnsFromItems(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0; font-size:0"><div style="display:grid; width:300px; contain:size; grid-template-columns:max-content auto">`+
		`<div style="width:70px;height:10px"></div><div id=b style="height:10px"></div></div></body>`, nil)
	if b := findBox(fl.root, byID("b")); b == nil || b.X != 70 {
		t.Errorf("#b %s, want at x=70", boxDims(b))
	}
}

// Size containment sizes the grid container as if empty, but its rows still
// size from its items, as its columns do (GridLayoutAlgorithm::Layout builds
// the sizing tree with its children, grid_layout_algorithm.cc:416-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the second auto row starts
// below the first item's 20px. Mirrors WPT css-contain/contain-size-064.
func TestSizeContainedGridSizesRowsFromItems(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:grid; width:100px; contain:size; grid-template-rows:auto auto">`+
		`<div style="height:20px"></div><div id=b style="height:30px"></div></div></body>`, nil)
	if b := findBox(fl.root, byID("b")); b == nil || b.Y != 20 {
		t.Errorf("#b %s, want at y=20", boxDims(b))
	}
}

// Round 2 of the #410 review.
//   - Size containment sizes only the container: a contained grid's auto
//     row still holds its item's margin box.
//   - An item's intrinsic-minimum contribution, when the automatic minimum
//     does not apply (it spans a flexible track and more than one track) or
//     it is a scroll container, is its min-size plus its margins, not 0
//     (ContributionSizeForGridItem's kForIntrinsicMinimums,
//     grid_layout_algorithm.cc:727-778; CalculateIntrinsicMinimumContribution).
//   - The contained intrinsic block size sizes the explicit rows ignoring
//     children, so every auto-fit row collapses
//     (BuildGridSizingTreeIgnoringChildren, grid_layout_algorithm.cc:508-530)
//
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestGridSizingReviewRound2(t *testing.T) {
	for _, tc := range []struct {
		name, html, id string
		check          func(b *Box) bool
		want           string
	}{
		{"contained row holds the item's margins",
			`<div style="display:grid; width:100px; contain:size; grid-template-rows:auto auto"><div style="height:20px; margin-bottom:10px"></div><div id=b style="height:10px"></div></div>`,
			"b", func(b *Box) bool { return b.Y == 30 }, "at y=30"},
		{"spanning item's margins at min-content",
			`<div id=g style="display:inline-grid; width:min-content; grid-template-columns:1fr 1fr"><div style="grid-column:1/3; margin-left:20px; height:10px"></div></div>`,
			"g", func(b *Box) bool { return b.Width == 20 }, "20 wide"},
		// The item overflows both axes, so it has both overflow:auto
		// scrollbars (LOU-307), and its auto min-size resolves to its
		// border + scrollbar + padding (ResolveMinInlineLength,
		// length_utils.h:111-125): margins plus the vertical scrollbar.
		{"scroll container's margins at min-content",
			`<div id=g style="display:inline-grid; width:min-content"><div style="overflow:auto; margin:0 15px; height:10px"><div style="width:100px;height:10px"></div></div></div>`,
			"g", func(b *Box) bool { return b.Width == 30+classicScrollbarWidth(&css.Style{}) }, "30 + the scrollbar wide"},
		{"contained auto-fit rows collapse",
			`<div id=g style="display:grid; contain:size; grid-template-rows:repeat(auto-fit, 100px)"><div style="height:10px"></div></div><div id=after style="height:5px"></div>`,
			"after", func(b *Box) bool { return b.Y == 0 }, "at y=0 (the contained grid 0 tall)"},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">`+tc.html+`</body>`, nil)
		if b := findBox(fl.root, byID(tc.id)); b == nil || !tc.check(b) {
			t.Errorf("%s: #%s %s, want %s", tc.name, tc.id, boxDims(b), tc.want)
		}
	}
}

// Round 3 of the #410 review: the minimum contribution's inputs
// (CalculateIntrinsicMinimumContribution, grid_layout_utils.cc:506-600;
// StyleBuilderConverter::ConvertLengthSizing, style_builder_converter.cc:
// 2130-2156 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A scroll
// container's minimum is its min-width: a percentage one is unresolvable
// against the indefinite grid area (0); min-content is its min-content
// size. A width of calc(50px) is fixed, so the min-content contribution
// applies; -webkit-fill-available is stretch, so auto-like. And the
// contained intrinsic block size has no gap beside collapsed auto-fit rows.
func TestGridSizingReviewRound3(t *testing.T) {
	kid := `<div style="width:100px;height:10px"></div>`
	for _, tc := range []struct {
		name, html, id string
		check          func(b *Box) bool
		want           string
	}{
		{"percentage min-width is unresolvable",
			`<div style="width:800px"><div id=g style="display:inline-grid; width:min-content"><div style="overflow:auto; min-width:50%">` + kid + `</div></div></div>`,
			"g", func(b *Box) bool { return b.Width == 0 }, "0 wide"},
		{"min-width:min-content",
			`<div id=g style="display:inline-grid; width:min-content"><div style="overflow:auto; min-width:min-content">` + kid + `</div></div>`,
			"g", func(b *Box) bool { return b.Width == 100 }, "100 wide"},
		{"calc(50px) is fixed",
			`<div id=g style="display:inline-grid; width:min-content"><div style="overflow:auto; width:calc(50px)">` + kid + `</div></div>`,
			"g", func(b *Box) bool { return b.Width == 50 }, "50 wide"},
		{"-webkit-fill-available is stretch",
			`<div id=g style="display:inline-grid; width:min-content"><div style="overflow:auto; width:-webkit-fill-available">` + kid + `</div></div>`,
			"g", func(b *Box) bool { return b.Width == 0 }, "0 wide"},
		{"contained auto-fit gutter collapses",
			`<div style="display:grid; contain:size; row-gap:10px; grid-template-rows:repeat(auto-fit, 100px) 50px"><div style="height:10px"></div></div><div id=after style="height:5px"></div>`,
			"after", func(b *Box) bool { return b.Y == 50 }, "at y=50"},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">`+tc.html+`</body>`, nil)
		if b := findBox(fl.root, byID(tc.id)); b == nil || !tc.check(b) {
			t.Errorf("%s: #%s %s, want %s", tc.name, tc.id, boxDims(b), tc.want)
		}
	}
}
