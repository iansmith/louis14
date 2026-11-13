package css

import (
	"slices"
	"testing"
)

// Escapes resolve per token: a string token's value is its unescaped text
// (CSSTokenizer::ConsumeStringTokenUntil, core/css/parser/css_tokenizer.cc:
// 393-435 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so an escaped quote or
// backslash stays inside its string. A declaration stores a string as Blink
// serializes it (SerializeString, core/css/css_markup.cc:186-204;
// SerializeCharacterAsCodePoint, :140-142): in double
// quotes, `"` and `\` escaped, control characters as code points.
func TestStringTokenEscapes(t *testing.T) {
	for _, tc := range []struct {
		name, css, stored string
		text              []string
	}{
		{"escaped quote", `"\""`, `"\""`, []string{`"`}},
		{"escaped single quote", `'it\'s'`, `"it's"`, []string{"it's"}},
		{"quote inside", `"x\"y"`, `"x\"y"`, []string{`x"y`}},
		{"escaped backslash", `"\\"`, `"\\"`, []string{`\`}},
		{"escaped newline continues", "\"q\\\nr\"", `"qr"`, []string{"qr"}},
		{"hex newline", `"a\A b"`, `"a\a b"`, []string{"a\nb"}},
		// Guards: green before the change.
		{"hex escape", `"a\41 b"`, `"aAb"`, []string{"aAb"}},
		{"two strings", `"a" "b"`, `"a" "b"`, []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ss := mustParseSheet(t, `p::before { content: `+tc.css+` }`, nil)
			stored := ss.Rules[0].Declarations["content"]
			if stored != tc.stored {
				t.Errorf("stored content = %q, want %q", stored, tc.stored)
			}
			var text []string
			for _, v := range ParseContentValues(stored) {
				if v.Type == "text" {
					text = append(text, v.Value)
				}
			}
			if !slices.Equal(text, tc.text) {
				t.Errorf("content strings = %q, want %q", text, tc.text)
			}
		})
	}
}

// Every string token is stored as SerializeString writes it, not only
// content's: a font-family string keeps its escaped quote.
func TestStringTokenEscapesFontFamily(t *testing.T) {
	ss := mustParseSheet(t, `p { font-family: "a\"b", serif }`, nil)
	if got, want := ss.Rules[0].Declarations["font-family"], `"a\"b", serif`; got != want {
		t.Errorf("stored font-family = %q, want %q", got, want)
	}
}

// A custom property's value is kept as authored (Blink stores its tokens'
// original text), so its strings are not re-serialized.
func TestCustomPropertyKeepsAuthoredStrings(t *testing.T) {
	ss := mustParseSheet(t, `p { --x: 'a' }`, nil)
	if got := ss.Rules[0].Declarations["--x"]; got != `'a'` {
		t.Errorf("stored --x = %q, want 'a'", got)
	}
}
