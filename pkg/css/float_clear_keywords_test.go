package css

import "testing"

// TestFloatClear_KeywordsMatchCaseInsensitively pins that the keywords match
// ASCII case-insensitively, as CssValueKeywordID does (ParseKeywordValue,
// parser/css_parser_fast_paths.cc:1959 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), through to the used value.
func TestFloatClear_KeywordsMatchCaseInsensitively(t *testing.T) {
	s := NewStyle()
	s.Set("float", "INLINE-End")
	s.Set("clear", "LEFT")
	if got := s.Floating(DirectionLTR); got != FloatRight {
		t.Errorf("float:INLINE-End = %v, want right", got)
	}
	if !s.IsFloating() {
		t.Errorf("float:INLINE-End: IsFloating = false, want true")
	}
	if got := s.Clear(DirectionLTR); got != ClearLeft {
		t.Errorf("clear:LEFT = %v, want left", got)
	}
}
