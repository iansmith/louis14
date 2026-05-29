package css

import "testing"

// TestGetInitialLetter exercises the CSS Inline Layout 3 `initial-letter`
// grammar: `normal | [ <number> <integer>? ] | [ <number> && [ drop | raise ]? ]`.
// Mirrors the values produced by Blink's StyleBuilderConverter::ConvertInitialLetter
// at Chromium 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestGetInitialLetter(t *testing.T) {
	cases := []struct {
		val      string
		wantSet  bool
		wantSize float64
		wantSink int
		wantRaise bool
	}{
		{"", false, 0, 0, false},
		{"normal", false, 0, 0, false},
		// Bare <number>: sink defaults to floor(size); drop alignment.
		{"3", true, 3, 3, false},
		{"2.5", true, 2.5, 2, false},
		// <number> <integer>: explicit sink.
		{"3 2", true, 3, 2, false},
		{"5 7", true, 5, 7, false}, // size < sink → over-aligned
		// drop keyword: sink == floor(size) (the drop default), matching
		// Blink StyleInitialLetter(size, kDrop).
		{"3 drop", true, 3, 3, false},
		// raise keyword: Blink StyleInitialLetter(size, kRaise) forces sink=1.
		{"3 raise", true, 3, 1, true},
		// keyword first, number second is also valid per the && combinator.
		{"drop 3", true, 3, 3, false},
		{"raise 3", true, 3, 1, true},
		// invalid → not set.
		{"0", false, 0, 0, false},
		{"-2", false, 0, 0, false},
	}
	for _, c := range cases {
		s := NewStyle()
		if c.val != "" {
			s.Set("initial-letter", c.val)
		}
		got := s.GetInitialLetter()
		if got.Set != c.wantSet {
			t.Errorf("initial-letter %q: Set=%v want %v", c.val, got.Set, c.wantSet)
			continue
		}
		if !c.wantSet {
			continue
		}
		if got.Size != c.wantSize {
			t.Errorf("initial-letter %q: Size=%v want %v", c.val, got.Size, c.wantSize)
		}
		if got.Sink != c.wantSink {
			t.Errorf("initial-letter %q: Sink=%v want %v", c.val, got.Sink, c.wantSink)
		}
		if got.Raise != c.wantRaise {
			t.Errorf("initial-letter %q: Raise=%v want %v", c.val, got.Raise, c.wantRaise)
		}
	}
}
