package layout

import (
	"louis14/pkg/css"
	"testing"
)

// TestInitialLetterTextShift covers the block-start-adjust applied to the
// surrounding paragraph text for an initial letter. Only `raise` shifts (by
// ceil(size)-sink lines); drop / explicit-sink do not. The fractional-drop case
// is a regression guard: GetInitialLetter sets Sink=floor(size), so a naive
// size-sink would shift a plain fractional drop — it must not.
func TestInitialLetterTextShift(t *testing.T) {
	const lh = 24.0
	cases := []struct {
		name string
		il   css.InitialLetterValue
		want float64
	}{
		{"raise 3", css.InitialLetterValue{Size: 3, Sink: 1, Raise: true, Set: true}, 48},     // ceil(3)-1 = 2 lines
		{"raise 2.5", css.InitialLetterValue{Size: 2.5, Sink: 1, Raise: true, Set: true}, 48}, // ceil(2.5)=3, 3-1 = 2 lines
		{"drop 3", css.InitialLetterValue{Size: 3, Sink: 3, Set: true}, 0},                    // bare integer drop: no shift
		{"drop 2.5 (regression)", css.InitialLetterValue{Size: 2.5, Sink: 2, Set: true}, 0},   // fractional drop: NO phantom shift
		{"explicit sink 3 2", css.InitialLetterValue{Size: 3, Sink: 2, Set: true}, 0},         // not raise → not modelled, no shift
		{"unset", css.InitialLetterValue{}, 0},
	}
	for _, c := range cases {
		if got := initialLetterTextShift(c.il, lh); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

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
