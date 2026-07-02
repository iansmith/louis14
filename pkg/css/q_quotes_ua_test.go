package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestQElementUAQuoteContent: the HTML UA stylesheet generates <q>'s
// quotation marks as pseudo-element content — `q:before { content:
// open-quote }` / `q:after { content: close-quote }` (Blink
// core/html/resources/html.css:115-121 @ Chromium
// 906a32d8634edf17db094507908f439bd9b52de5). An author `content`
// declaration on q::before overrides the UA default (normal cascade
// origin order). WPT: css-pseudo/first-letter-with-quote.html.
//
// RED before fix: louis14 has no UA pseudo rules for q, so the pseudo style
// carries no content and no quotes render.
func TestQElementUAQuoteContent(t *testing.T) {
	q := &html.Node{Type: html.ElementNode, TagName: "q"}

	before := ComputePseudoElementStyle(q, "before", nil, 800, 600)
	if got, _ := before.Get("content"); got != "open-quote" {
		t.Errorf("q::before UA content = %q, want %q", got, "open-quote")
	}
	after := ComputePseudoElementStyle(q, "after", nil, 800, 600)
	if got, _ := after.Get("content"); got != "close-quote" {
		t.Errorf("q::after UA content = %q, want %q", got, "close-quote")
	}

	// Author declaration overrides the UA default.
	sheet, err := ParseStylesheet(`q::before { content: "X"; }`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	authored := ComputePseudoElementStyle(q, "before", []*Stylesheet{sheet}, 800, 600)
	if got, _ := authored.Get("content"); got != `"X"` {
		t.Errorf("author q::before content = %q, want %q", got, `"X"`)
	}

	// Other elements are unaffected.
	span := &html.Node{Type: html.ElementNode, TagName: "span"}
	if got, ok := ComputePseudoElementStyle(span, "before", nil, 800, 600).Get("content"); ok && got != "" {
		t.Errorf("span::before UA content = %q, want none", got)
	}
}
