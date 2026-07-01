package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

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

// firstLetterSplitFixture builds a block container with one text child and
// runs applyFirstLetterSplit over it with a matching div::first-letter rule.
// normalized/raw become the text node's parser fields (html.Node.Text /
// html.Node.RawText — the tokenizer stores the collapsed form in Text and the
// original source in RawText).
func firstLetterSplitFixture(t *testing.T, normalized, raw string, containerProps ...string) []*LayoutInputNode {
	t.Helper()
	div := &html.Node{Type: html.ElementNode, TagName: "div"}
	txt := &html.Node{Type: html.TextNode, Text: normalized, RawText: raw, Parent: div}
	div.Children = []*html.Node{txt}

	divStyle := makeStyle(containerProps...)
	sheet, err := css.ParseStylesheet("div::first-letter { color: green }", nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	b := &LayoutTreeBuilder{
		styles:         map[*html.Node]*css.Style{div: divStyle},
		stylesheets:    []*css.Stylesheet{sheet},
		viewportWidth:  800,
		viewportHeight: 600,
	}
	children := []*LayoutInputNode{{DOMNode: txt, style: divStyle}}
	return b.applyFirstLetterSplit(children, div, divStyle)
}

// TestFirstLetterSplitPreservedLeadingNewline: with white-space: pre, the
// ::first-letter scan must inspect the ORIGINAL text (html.Node.RawText — the
// louis14 analog of Blink LayoutText::OriginalText) so a leading newline
// terminates first-letter discovery. Mirrors Blink
// FirstLetterPseudoElement::FirstLetterTextLayoutObject @ Chromium
// 906a32d8634edf17db094507908f439bd9b52de5
// (first_letter_pseudo_element.cc:270-273: OriginalText + per-text-node
// ShouldPreserveBreaks). WPT: first-letter-with-preceding-new-line.html.
//
// RED before fix: the walker scans the parser-normalized html.Node.Text
// (" The second line. ...") where the newline is already collapsed, so the
// wrap is wrongly applied to the 'T'.
func TestFirstLetterSplitPreservedLeadingNewline(t *testing.T) {
	out := firstLetterSplitFixture(t,
		" The second line. The third line. ",
		"\n  The second line.\n  The third line.\n  ",
		"display", "block", "white-space", "pre")
	if len(out) != 1 || !out[0].IsText() {
		t.Fatalf("expected the single text child to be left unwrapped (leading newline in pre terminates ::first-letter); got %d children", len(out))
	}
	if got := out[0].DOMNode.RawText; got != "\n  The second line.\n  The third line.\n  " {
		t.Errorf("text child RawText mangled: %q", got)
	}
}

// TestFirstLetterSplitPreservedTextSlicesRawText: when a wrap IS applied in a
// preserved-white-space context, the split must slice the original text and
// keep RawText on the synthesized prefix/rest nodes so downstream `pre`
// processing (inline_item.go's RawText substitution) still sees the newlines
// and leading indentation. Same Blink mirror as above.
//
// RED before fix: the split slices the collapsed Text and drops RawText, so
// the pre block loses all its line breaks.
func TestFirstLetterSplitPreservedTextSlicesRawText(t *testing.T) {
	out := firstLetterSplitFixture(t,
		" Hello World", "  Hello\nWorld",
		"display", "block", "white-space", "pre")
	// Expected: [prefix "  ", ::first-letter span "H", rest "ello\nWorld"]
	if len(out) != 3 {
		t.Fatalf("expected 3 children (prefix, letter span, rest); got %d", len(out))
	}
	if got := out[0].DOMNode.RawText; got != "  " {
		t.Errorf("prefix RawText = %q; want %q (pre indentation must survive)", got, "  ")
	}
	if !out[1].IsElement() || len(out[1].children) != 1 {
		t.Fatalf("expected out[1] to be the ::first-letter span")
	}
	if got := out[1].children[0].TextContent(); got != "H" {
		t.Errorf("letter = %q; want %q", got, "H")
	}
	if got := out[2].DOMNode.RawText; got != "ello\nWorld" {
		t.Errorf("rest RawText = %q; want %q", got, "ello\nWorld")
	}
}
