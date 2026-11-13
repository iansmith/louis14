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
			name:      "adjacent glyphs collapse under thickness dilation",
			start:     0,
			end:       100,
			thickness: 4, // dilation = min(4, 13) (TextPainter::ClipDecorationLine); glyphs at [40,50] and [52,60] expand to [36,54] and [48,64], merging to [36,64]
			glyphs:    []glyphExtent{{Start: 40, End: 50}, {Start: 52, End: 60}},
			want:      []inkSegment{{Start: 0, End: 36}, {Start: 64, End: 100}},
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

// TextPainter::ClipDecorationLine looks for glyph intercepts in the line's
// bounds inset 0.5px across the line (core/paint/text_painter.cc:588-593 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a line that overlaps the
// glyphs by less than half a pixel skips no ink.
func TestDecorationBandCrossesGlyphs_IgnoresUnderHalfPixel(t *testing.T) {
	for _, c := range []struct {
		name        string
		top, bottom float64
		want        bool
	}{
		{"0.4px into the glyphs", 49.6, 55.6, false},
		{"0.6px into the glyphs", 49.4, 55.4, true},
		{"touching the glyphs", 50, 56, false},
		{"inside the glyphs", 20, 26, true},
		{"0.4px above the glyphs' top", 4.4, 10.4, false},
	} {
		if got := decorationBandCrossesGlyphs(c.top, c.bottom, 10, 50); got != c.want {
			t.Errorf("%s: band %v-%v over glyphs 10-50 = %v, want %v", c.name, c.top, c.bottom, got, c.want)
		}
	}
}
