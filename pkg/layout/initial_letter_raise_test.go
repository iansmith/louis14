package layout

import "testing"

// TestInitialLetterBlockStartMargin covers the drop block-start margin for an
// initial-letter float: max(0, lineHeight*size - bigAscent - paraLineDescent).
// This is the letter's absolute position for both drop and raise — the raise
// text-shift (LOU-289 part 3) moves the text and the letter by equal-and-opposite
// block-start-adjust, leaving the letter's own margin at the drop offset. Mirrors
// Blink ComputeInitialLetterBoxBlockOffset (initial_letter_utils.cc:24-105 @ 4883d11fef).
func TestInitialLetterBlockStartMargin(t *testing.T) {
	const lineHeight = 24.0
	const bigAscent = 64.0
	const paraLineDescent = 6.0
	const size = 3.0

	// Ahem 20px / line-height 24px, initial-letter 3: drop offset = 72 - 64 - 6 = 2.
	got := initialLetterBlockStartMargin(size, lineHeight, bigAscent, paraLineDescent)
	want := lineHeight*size - bigAscent - paraLineDescent
	if got != want {
		t.Errorf("drop margin: got %v, want %v", got, want)
	}

	// Negative offset clamps to 0 (float margin-top cannot be negative).
	if z := initialLetterBlockStartMargin(1, lineHeight, 100, 50); z != 0 {
		t.Errorf("clamp: got %v, want 0", z)
	}
}
