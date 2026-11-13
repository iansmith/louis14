package css

import "testing"

// A minmax() bound keeps its sizing function: a length, auto, min-content or
// max-content, as GridTrackSize keeps each breadth as a Length
// (core/style/grid_track_size.h:132-133; Length::Type,
// platform/geometry/length.h:116-126 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Both track parsers agree.
func TestGridTrackMinMaxBreadthTypes(t *testing.T) {
	cases := []struct {
		val      string
		min, max GridTrackBreadthType
	}{
		{"minmax(auto, 10px)", GridTrackBreadthAuto, GridTrackBreadthFixed},
		{"minmax(min-content, 10px)", GridTrackBreadthMinContent, GridTrackBreadthFixed},
		{"minmax(max-content, 1fr)", GridTrackBreadthMaxContent, GridTrackBreadthFixed},
		{"minmax(10px, min-content)", GridTrackBreadthFixed, GridTrackBreadthMinContent},
		{"minmax(0px, max-content)", GridTrackBreadthFixed, GridTrackBreadthMaxContent},
		// Guard: a length minimum is the zero value, as before.
		{"minmax(10px, auto)", GridTrackBreadthFixed, GridTrackBreadthFixed},
	}
	for _, c := range cases {
		for name, tracks := range map[string][]GridTrack{
			"parseGridTracks":          parseGridTracks(c.val),
			"parseGridTracksWithNames": func() []GridTrack { tr, _ := parseGridTracksWithNames(c.val); return tr }(),
		} {
			if len(tracks) != 1 || !tracks[0].IsMinMax || tracks[0].MinBreadthType != c.min || tracks[0].MaxBreadthType != c.max {
				t.Errorf("%s(%q) = %+v; want min %v, max %v", name, c.val, tracks, c.min, c.max)
			}
		}
	}
}
