package css

import "testing"

// ConsumeURL consumes a url() as Blink's tokenizer does, on input whose NULs
// are U+FFFD (CSS Syntax 3 §3.3 preprocessing): a url-token, or the url(
// function with one string token, its block closed by ')' or end of input (CSSTokenizer::ConsumeIdentLikeToken,
// ConsumeUrlToken, ConsumeStringTokenUntil, css_tokenizer.cc:374-560; the
// escapes, ConsumeEscape, css_parser_idioms.cc:27-56 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestConsumeURL(t *testing.T) {
	for _, c := range []struct {
		in, value, rest string
		ok              bool
	}{
		{`url(#f)`, "#f", "", true},
		{`URL( a.svg#f ) blur(2px)`, "a.svg#f", "blur(2px)", true},
		{`url(#\31 23)`, "#123", "", true},
		{`url(a\).svg#f)`, "a).svg#f", "", true},
		{`url("a b.svg#f")`, "a b.svg#f", "", true},
		{`url( 'q\'s.svg#f' )`, "q's.svg#f", "", true},
		{`url("'#a'")`, "'#a'", "", true},
		{`url("a" "b")`, "", "", false},
		{`url("a" x)`, "", "", false},
		{`url(a b)`, "", "", false},
		{`url(a"b)`, "", "", false},
		{`url(a(b)`, "", "", false},
		{`url("a`, "a", "", true},
		{"url(a\x00b)", "a\uFFFDb", "", true},
		{"url(\"a\x00b\")", "a\uFFFDb", "", true},
		{"url(a\\\x00)", "a\uFFFD", "", true},
		{`url(a\`, "a\uFFFD", "", true},
		{`#f`, "", "", false},
	} {
		v, rest, ok := ConsumeURL(c.in)
		if ok != c.ok || (ok && (v != c.value || rest != c.rest)) {
			t.Errorf("ConsumeURL(%q) = %q, %q, %v; want %q, %q, %v", c.in, v, rest, ok, c.value, c.rest, c.ok)
		}
	}
}

// A url() in a declaration value reaches the tokenizer raw: its escapes are
// decoded once, by ConsumeURL, not first by the declaration's unescaping
// (Blink tokenizes the raw text once). url(#a\5c 66) is #a\66, not #af.
func TestFilterURLEscapesDecodedOnce(t *testing.T) {
	sheet, err := ParseStylesheet(`p { filter: url(#a\5c 66) }`, nil)
	if err != nil || len(sheet.Rules) != 1 {
		t.Fatalf("ParseStylesheet: %v, %d rules", err, len(sheet.Rules))
	}
	ops := parseFilterList(sheet.Rules[0].Declarations["filter"])
	if len(ops) != 1 || ops[0].URL.Absolute != `#a\66` {
		t.Errorf("filter = %+v, want one url() of #a\\66", ops)
	}
}

// A filter list's url() stages tokenize as a lone url() does: a malformed
// one makes the list invalid (ConsumeFilterFunctionList stops, and the
// declaration fails, css_parsing_utils.cc:3993-4017 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and a hex escape is decoded.
func TestParseFilterListURLTokens(t *testing.T) {
	if ops := parseFilterList(`url('a.svg#f' x) blur(1px)`); len(ops) != 0 {
		t.Errorf("malformed url() list = %+v, want none", ops)
	}
	if ops := parseFilterList(`url(#\31 23) blur(1px)`); len(ops) != 2 || ops[0].URL.Absolute != "#123" {
		t.Errorf("escaped url() list = %+v, want #123 then blur", ops)
	}
}
