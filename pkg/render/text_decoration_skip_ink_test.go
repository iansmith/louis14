package render

import (
	"reflect"
	"testing"
)

func TestSkipInkSegments(t *testing.T) {
	cases := []struct {
		name      string
		start     float64
		end       float64
		thickness float64
		glyphs    []glyphExtent
		want      []inkSegment
	}{
		{
			name:   "no glyphs returns full segment",
			start:  0,
			end:    100,
			glyphs: nil,
			want:   []inkSegment{{Start: 0, End: 100}},
		},
		{
			name:   "one glyph fully spans line returns empty",
			start:  0,
			end:    100,
			glyphs: []glyphExtent{{Start: 0, End: 100}},
			want:   []inkSegment{},
		},
		{
			name:   "two glyphs with gap returns the gap",
			start:  0,
			end:    100,
			glyphs: []glyphExtent{{Start: 0, End: 40}, {Start: 60, End: 100}},
			want:   []inkSegment{{Start: 40, End: 60}},
		},
		{
			name:   "glyph at start edge — segment truncated on left",
			start:  0,
			end:    100,
			glyphs: []glyphExtent{{Start: 0, End: 30}},
			want:   []inkSegment{{Start: 30, End: 100}},
		},
		{
			name:   "glyph at end edge — segment truncated on right",
			start:  0,
			end:    100,
			glyphs: []glyphExtent{{Start: 70, End: 100}},
			want:   []inkSegment{{Start: 0, End: 70}},
		},
		{
			name:      "adjacent glyphs collapse under thickness inflation",
			start:     0,
			end:       100,
			thickness: 4, // inflate = 2; glyphs at [40,50] and [52,60] expand to [38,52] and [50,62], merging to [38,62]
			glyphs:    []glyphExtent{{Start: 40, End: 50}, {Start: 52, End: 60}},
			want:      []inkSegment{{Start: 0, End: 38}, {Start: 62, End: 100}},
		},
		{
			name:   "out-of-range glyph ignored",
			start:  10,
			end:    90,
			glyphs: []glyphExtent{{Start: -5, End: 0}, {Start: 95, End: 110}},
			want:   []inkSegment{{Start: 10, End: 90}},
		},
		{
			name:   "Ahem 4× full-em squares cover whole line — empty result",
			start:  0,
			end:    80,
			glyphs: []glyphExtent{{Start: 0, End: 20}, {Start: 20, End: 40}, {Start: 40, End: 60}, {Start: 60, End: 80}},
			want:   []inkSegment{},
		},
		{
			name:   "unsorted glyphs handled correctly",
			start:  0,
			end:    100,
			glyphs: []glyphExtent{{Start: 60, End: 80}, {Start: 20, End: 40}},
			want:   []inkSegment{{Start: 0, End: 20}, {Start: 40, End: 60}, {Start: 80, End: 100}},
		},
		{
			name:   "overlapping glyphs merged",
			start:  0,
			end:    100,
			glyphs: []glyphExtent{{Start: 10, End: 50}, {Start: 30, End: 70}},
			want:   []inkSegment{{Start: 0, End: 10}, {Start: 70, End: 100}},
		},
		{
			name:   "empty range returns nil",
			start:  50,
			end:    50,
			glyphs: nil,
			want:   nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := skipInkSegments(c.start, c.end, c.thickness, c.glyphs)
			// Treat empty/nil interchangeably for comparison.
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("skipInkSegments(%v..%v, th=%v, glyphs=%v) = %v, want %v",
					c.start, c.end, c.thickness, c.glyphs, got, c.want)
			}
		})
	}
}
