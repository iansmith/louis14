package text

import (
	"slices"
	"testing"
)

// A font-family value's quoted family is its string token's unescaped value
// (ConsumeFamilyName, core/css/properties/css_parsing_utils.cc:6406-6419 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The cascade stores a family
// string as SerializeString writes it, escapes and all, so
// ParseFontFamilyList unescapes it.
func TestParseFontFamilyListUnescapes(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{`"Foo\"Bar", serif`, []string{`Foo"Bar`, "serif"}},
		{`"a\\b"`, []string{`a\b`}},
		// Control: plain quote stripping.
		{`'Times New Roman', serif`, []string{"Times New Roman", "serif"}},
	} {
		if got := ParseFontFamilyList(tc.raw); !slices.Equal(got, tc.want) {
			t.Errorf("ParseFontFamilyList(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// A font-family value is a comma-separated list of family names, and a comma
// inside a quoted family is string text: the list splits only at commas
// outside string tokens (ConsumeFontFamily / ConsumeFamilyName,
// core/css/properties/css_parsing_utils.cc:6406-6419 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestParseFontFamilyListSplitsOutsideStrings(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{`"Foo, Bar", serif`, []string{"Foo, Bar", "serif"}},
		{`'a,b','c'`, []string{"a,b", "c"}},
		{`"Foo\", Bar", serif`, []string{`Foo", Bar`, "serif"}},
		{`"a(b", serif`, []string{"a(b", "serif"}},
	} {
		if got := ParseFontFamilyList(tc.raw); !slices.Equal(got, tc.want) {
			t.Errorf("ParseFontFamilyList(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
