package css

import "testing"

// ConsumeURL consumes a url() as Blink's tokenizer does: a url-token, or the
// url( function with one string token (CSSTokenizer::ConsumeIdentLikeToken,
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
		{`url("a`, "", "", false},
		{`#f`, "", "", false},
	} {
		v, rest, ok := ConsumeURL(c.in)
		if ok != c.ok || (ok && (v != c.value || rest != c.rest)) {
			t.Errorf("ConsumeURL(%q) = %q, %q, %v; want %q, %q, %v", c.in, v, rest, ok, c.value, c.rest, c.ok)
		}
	}
}
