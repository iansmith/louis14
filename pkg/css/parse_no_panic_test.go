package css

import "testing"

// Blink's tokenizer-based parser never indexes past its input: a `}` in
// a string token is not a block's end, and a function token left open runs
// to EOF (CSSParserTokenStream @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// These inputs, found by fuzzing, panicked louis14's text-slicing parser.
var noPanicInputs = []string{
	"\"\r}\"{0",          // `}` in a string before the block's `{`
	"a:is( {color:red}",  // :is( never closed
	"a { b: \"}\" } c {", // `}` in a string, then an open block
}

// TestParseDoesNotPanic covers expandNesting with a parent selector, which
// FuzzParseStylesheet does not reach.
func TestParseDoesNotPanic(t *testing.T) {
	for _, css := range noPanicInputs {
		t.Run(css, func(t *testing.T) { expandNesting(css, ".p") })
	}
}

// A `}` inside a string or a comment does not end a block: Blink sees it
// inside a string token, or not at all (comments are not tokens).
func TestBraceInStringOrCommentDoesNotEndBlock(t *testing.T) {
	for _, css := range []string{
		`a { content: "}" } b { color: red }`,
		`a { /* } */ content: "x" } b { color: red }`,
		`@media all { a { content: "}" } b { color: red } }`,
		`.p { & a { content: "}" } } b { color: red }`,
	} {
		t.Run(css, func(t *testing.T) {
			b := ruleBySelector(mustParseSheet(t, css, nil), "b")
			if b == nil || b.Declarations["color"] != "red" {
				t.Errorf("rule b { color: red } missing or wrong: %+v", b)
			}
		})
	}
}

// FuzzParseStylesheet checks that ParseStylesheet never panics. `go test`
// runs only the seeds (testdata/fuzz/FuzzParseStylesheet included); run
// `go test -fuzz FuzzParseStylesheet ./pkg/css` to search further.
func FuzzParseStylesheet(f *testing.F) {
	for _, s := range append([]string{
		".p{width:1px; & c{width:2px}}",
		"@media screen{a{width:2px} b{width:2px}}",
		"a{} } b {",
		"@supports (display:grid){.q{&:hover{width:2px}}}",
		"@scope (.a) to (.b) { :scope { color: red } }",
		`a{b:"}"} c{d:e}`,
		`a{/*}*/b:c}`,
		`a}{content:"}"`,
	}, noPanicInputs...) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, css string) {
		ParseStylesheet(css, NewParserContext(""))
	})
}

// FuzzParseSelector checks that ParseSelector never panics, notably on a
// functional pseudo-class left open (its argument runs to EOF in Blink).
func FuzzParseSelector(f *testing.F) {
	for _, s := range []string{"a:is(", ":where(", "b:not(:is(", ":nth-child(", ":has(> a", "a:is(.b, .c)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, sel string) {
		ParseSelector(sel)
	})
}

// ruleBySelector is the last rule of sheet whose selector is raw, nil if none.
func ruleBySelector(sheet *Stylesheet, raw string) *Rule {
	var found *Rule
	for i := range sheet.Rules {
		if sheet.Rules[i].Selector.Raw == raw {
			found = &sheet.Rules[i]
		}
	}
	return found
}

// A `{` inside a string does not open a rule's block: Blink sees it inside a
// string token, so the block opens at the first `{` outside strings
// (CSSParserImpl::ConsumeQualifiedRule @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBraceInStringDoesNotOpenBlock(t *testing.T) {
	for _, q := range []string{`"`, `'`} {
		sel := `a[title=` + q + `{` + q + `]`
		for _, css := range []string{
			sel + ` { color: red }`,
			sel + `, b { color: red }`,
			`@media all { ` + sel + ` { color: red } }`,
			`@supports (display: block) { ` + sel + ` { color: red } }`,
			`@layer l { ` + sel + ` { color: red } }`,
			`@container (min-width: 0) { ` + sel + ` { color: red } }`,
			`.p { & ` + sel + ` { color: red } }`,
		} {
			want := sel
			if css[0] == '.' {
				want = `:is(.p) ` + sel
			}
			t.Run(css, func(t *testing.T) {
				sheet := mustParseSheet(t, css, nil)
				if r := ruleBySelector(sheet, want); r == nil || r.Declarations["color"] != "red" {
					t.Errorf("rule %s { color: red } missing or wrong: %+v", want, sheet.Rules)
				}
			})
		}
	}
}

// A string ends at an unescaped newline as a bad-string token, so a `}` on
// the next line closes the block; an escaped newline continues the string
// (CSSTokenizer::ConsumeStringTokenUntil, css_tokenizer.cc:393 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBadStringEndsAtNewline(t *testing.T) {
	for _, q := range []string{`"`, `'`} {
		for _, tc := range []struct{ css, sel string }{
			{"a { content: " + q + "x\n} b { color: red }", "b"},
			{"@media all { a { content: " + q + "x\n} b { color: red } }", "b"},
			{"a { content: " + q + "x\\\n}" + q + "; color: red }", "a"},
		} {
			t.Run(tc.css, func(t *testing.T) {
				sheet := mustParseSheet(t, tc.css, nil)
				if r := ruleBySelector(sheet, tc.sel); r == nil || r.Declarations["color"] != "red" {
					t.Errorf("rule %s { color: red } missing or wrong: %+v", tc.sel, sheet.Rules)
				}
			})
		}
	}
}
