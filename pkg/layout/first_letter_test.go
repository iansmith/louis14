package layout

import "testing"

// TestFirstLetterLength verifies the byte-length extraction of the leading
// typographic letter unit per CSS Pseudo-4 §3.2 and the cross-text-node
// Punctuation state transitions. Mirrors Blink's
// FirstLetterPseudoElement::FirstLetterLength @ Chromium 4883d11fef.
func TestFirstLetterLength(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		state       firstLetterPunctuationState
		preserve    bool
		wantSkipped int
		wantLen     int
		wantLetter  string
		wantState   firstLetterPunctuationState
	}{
		{
			name:        "simple ASCII",
			text:        "abc",
			wantSkipped: 0,
			wantLen:     1,
			wantLetter:  "a",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "leading whitespace",
			text:        "  abc",
			wantSkipped: 2,
			wantLen:     1,
			wantLetter:  "a",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:      "empty string",
			text:      "",
			wantState: firstLetterPunctNotSeen,
		},
		{
			name:      "only whitespace",
			text:      "   ",
			wantState: firstLetterPunctNotSeen,
		},
		{
			name:        "leading open paren plus letter plus close paren (wpt 005)",
			text:        "(£)78.90",
			wantSkipped: 0,
			wantLen:     len("(£)"),
			wantLetter:  "(£)",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "dollar sign as letter unit (wpt 005)",
			text:        "$1,234.56",
			wantSkipped: 0,
			wantLen:     1,
			wantLetter:  "$",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "Indian rupee as letter unit (wpt 005)",
			text:        "₹10,000",
			wantSkipped: 0,
			wantLen:     len("₹"),
			wantLetter:  "₹",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "copyright sign as letter unit (wpt 005)",
			text:        "©2021",
			wantSkipped: 0,
			wantLen:     len("©"),
			wantLetter:  "©",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "letter with combining mark",
			text:        "ébc",
			wantSkipped: 0,
			wantLen:     len("é"),
			wantLetter:  "é",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "Myanmar punct + combining + T + combining + Myanmar + combining (wpt 004)",
			text:        "၎̀T̀၎̀est",
			wantSkipped: 0,
			wantLen:     len("၎̀T̀၎̀"),
			wantLetter:  "၎̀T̀၎̀",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "leading punctuation then letter then close-quote",
			text:        "“abc”",
			wantSkipped: 0,
			wantLen:     len("“a"),
			wantLetter:  "“a",
			wantState:   firstLetterPunctDisallow,
		},
		{
			// Punctuation-only text node now signals kSeen so a subsequent
			// text node may provide the letter (Blink Punctuation::kSeen).
			name:        "leading punct alone signals kSeen",
			text:        "(((",
			wantSkipped: 0,
			wantLen:     3,
			wantLetter:  "(((",
			wantState:   firstLetterPunctSeen,
		},
		{
			name:      "leading punct then space breaks unit",
			text:      "( abc",
			wantState: firstLetterPunctDisallow,
		},
		{
			name:        "leading whitespace then letter then trailing punct",
			text:        " a.b",
			wantSkipped: 1,
			wantLen:     len("a."),
			wantLetter:  "a.",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "tab whitespace",
			text:        "\ta",
			wantSkipped: 1,
			wantLen:     1,
			wantLetter:  "a",
			wantState:   firstLetterPunctDisallow,
		},
		{
			name:        "NBSP is whitespace",
			text:        " a",
			wantSkipped: len(" "),
			wantLen:     1,
			wantLetter:  "a",
			wantState:   firstLetterPunctDisallow,
		},
		{
			// Em-dash is Pd which Blink excludes from first-letter punctuation;
			// the dash itself is taken as the letter, with no trailing chars.
			name:        "em-dash treated as letter not punctuation",
			text:        "– Test",
			wantSkipped: 0,
			wantLen:     len("–"),
			wantLetter:  "–",
			wantState:   firstLetterPunctDisallow,
		},
		{
			// kSeen carry: a subsequent text node containing the letter.
			name:        "kSeen carry then letter found",
			text:        "abc",
			state:       firstLetterPunctSeen,
			wantSkipped: 0,
			wantLen:     1,
			wantLetter:  "a",
			wantState:   firstLetterPunctDisallow,
		},
		{
			// kSeen carry: a second punctuation-only node accumulates more.
			name:        "kSeen carry then more punct",
			text:        "“",
			state:       firstLetterPunctSeen,
			wantSkipped: 0,
			wantLen:     len("“"),
			wantLetter:  "“",
			wantState:   firstLetterPunctSeen,
		},
		{
			// kSeen carry: leading whitespace is no longer allowed.
			name:      "kSeen carry then leading whitespace aborts",
			text:      " abc",
			state:     firstLetterPunctSeen,
			wantState: firstLetterPunctDisallow,
		},
		{
			// white-space: pre — a leading LF terminates the search.
			name:      "preserve_breaks leading LF aborts",
			text:      "\nabc",
			preserve:  true,
			wantState: firstLetterPunctDisallow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skipped, length, gotState := firstLetterLength(tc.text, tc.state, tc.preserve)
			if skipped != tc.wantSkipped || length != tc.wantLen {
				t.Errorf("firstLetterLength(%q, %v, %v) = (%d, %d); want (%d, %d)",
					tc.text, tc.state, tc.preserve, skipped, length, tc.wantSkipped, tc.wantLen)
			}
			if length > 0 {
				got := tc.text[skipped : skipped+length]
				if got != tc.wantLetter {
					t.Errorf("extracted letter = %q; want %q", got, tc.wantLetter)
				}
			}
			if gotState != tc.wantState {
				t.Errorf("state = %v; want %v", gotState, tc.wantState)
			}
		})
	}
}
