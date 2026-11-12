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
			sheet := mustParseSheet(t, css, nil)
			var b *Rule
			for i := range sheet.Rules {
				if sheet.Rules[i].Selector.Raw == "b" {
					b = &sheet.Rules[i]
				}
			}
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
