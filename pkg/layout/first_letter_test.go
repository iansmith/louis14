package layout

import "testing"

// TestFirstLetterLength verifies the byte-length extraction of the leading
// typographic letter unit per CSS Pseudo-4 §3.2. Mirrors Blink's
// FirstLetterPseudoElement::FirstLetterLength @ Chromium 4883d11fef.
func TestFirstLetterLength(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantSkipped int
		wantLen     int
		wantLetter  string
	}{
		{
			name:        "simple ASCII",
			text:        "abc",
			wantSkipped: 0,
			wantLen:     1,
			wantLetter:  "a",
		},
		{
			name:        "leading whitespace",
			text:        "  abc",
			wantSkipped: 2,
			wantLen:     1,
			wantLetter:  "a",
		},
		{
			name:        "empty string",
			text:        "",
			wantSkipped: 0,
			wantLen:     0,
			wantLetter:  "",
		},
		{
			name:        "only whitespace",
			text:        "   ",
			wantSkipped: 0,
			wantLen:     0,
			wantLetter:  "",
		},
		{
			name:        "leading open paren plus letter plus close paren (wpt 005)",
			text:        "(£)78.90",
			wantSkipped: 0,
			wantLen:     len("(£)"),
			wantLetter:  "(£)",
		},
		{
			name:        "dollar sign as letter unit (wpt 005)",
			text:        "$1,234.56",
			wantSkipped: 0,
			wantLen:     1,
			wantLetter:  "$",
		},
		{
			name:        "Indian rupee as letter unit (wpt 005)",
			text:        "₹10,000",
			wantSkipped: 0,
			wantLen:     len("₹"),
			wantLetter:  "₹",
		},
		{
			name:        "copyright sign as letter unit (wpt 005)",
			text:        "©2021",
			wantSkipped: 0,
			wantLen:     len("©"),
			wantLetter:  "©",
		},
		{
			name:        "letter with combining mark",
			text:        "ébc",
			wantSkipped: 0,
			wantLen:     len("é"),
			wantLetter:  "é",
		},
		{
			name:        "Myanmar punct + combining + T + combining + Myanmar + combining (wpt 004)",
			text:        "၎̀T̀၎̀est",
			wantSkipped: 0,
			wantLen:     len("၎̀T̀၎̀"),
			wantLetter:  "၎̀T̀၎̀",
		},
		{
			name:        "leading punctuation then letter then close-quote",
			text:        "“abc”",
			wantSkipped: 0,
			wantLen:     len("“a"),
			wantLetter:  "“a",
		},
		{
			name:        "leading punct alone (no letter)",
			text:        "(((",
			wantSkipped: 0,
			wantLen:     0,
			wantLetter:  "",
		},
		{
			name:        "leading punct then space breaks unit",
			text:        "( abc",
			wantSkipped: 0,
			wantLen:     0,
			wantLetter:  "",
		},
		{
			name:        "leading whitespace then letter then trailing punct",
			text:        " a.b",
			wantSkipped: 1,
			wantLen:     len("a."),
			wantLetter:  "a.",
		},
		{
			name:        "tab whitespace",
			text:        "\ta",
			wantSkipped: 1,
			wantLen:     1,
			wantLetter:  "a",
		},
		{
			name:        "NBSP is whitespace",
			text:        " a",
			wantSkipped: len(" "),
			wantLen:     1,
			wantLetter:  "a",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			skipped, length := firstLetterLength(tc.text)
			if skipped != tc.wantSkipped || length != tc.wantLen {
				t.Errorf("firstLetterLength(%q) = (%d, %d); want (%d, %d)",
					tc.text, skipped, length, tc.wantSkipped, tc.wantLen)
			}
			if length > 0 {
				got := tc.text[skipped : skipped+length]
				if got != tc.wantLetter {
					t.Errorf("extracted letter = %q; want %q", got, tc.wantLetter)
				}
			}
		})
	}
}
