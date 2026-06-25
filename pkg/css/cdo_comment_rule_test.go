package css

import (
	"strings"
	"testing"

	"louis14/pkg/html"
)

// TestXHTMLStyleComment_KeepsFollowingRule is the regression test for LOU-322.
//
// The Mozilla flexbox-align-self-vert(-rtl)-NNN-ref.xhtml reference files wrap a
// prose note inside their <style> block using an XML comment:
//
//	<!-- We center shrinkwrapped text ... "text-align:center" set. -->
//	.centerParent { text-align: center; }
//
// These are XHTML documents (they open with an `<?xml …?>` prolog). In XML,
// <style> content is element text, so the XML processor removes whole comments
// — delimiters AND the prose inside them — before the CSS parser ever sees the
// text. The `.centerParent { text-align: center }` rule that follows therefore
// survives, and real browsers center their emulated align-self:center items.
//
// louis14 previously treated <style> as HTML rawtext unconditionally: only the
// CDO/CDC `<!--`/`-->` delimiters were stripped (by the CSS tokenizer), leaving
// the prose between them in the stream. That orphan prose became the prelude of
// an invalid qualified rule that swallowed the following `.centerParent { … }`
// block, so text-align:center was silently dropped and the references
// mis-rendered. That was the entire source of the 0.8–2.1% diff on all nine
// flexbox-align-self-vert tests — the flex layout itself is correct.
//
// The fix lives in the HTML/XML parse layer (pkg/html): for XML/XHTML
// documents, <style> content has its whole XML comments stripped before it
// becomes a stylesheet. This test drives that real path — it parses a minimal
// XHTML document through html.Parse and then parses the extracted <style> text
// as CSS — and asserts the `.centerParent { text-align: center }` rule
// survives.
func TestXHTMLStyleComment_KeepsFollowingRule(t *testing.T) {
	// Mirrors flexbox-align-self-vert-001-ref.xhtml: an `<?xml?>` prolog marks
	// the document as XML, and the <style> block carries the same prose-in-
	// comment shape that previously corrupted the following rule.
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
  <head>
    <style>
      .center { background: lightblue; margin: auto; }

      <!-- We center shrinkwrapped text by putting it into an inline-block, and
           then wrapping that inline-block in a helper-div that has
           "text-align:center" set. -->
      .centerParent {
        text-align: center;
      }
    </style>
  </head>
  <body></body>
</html>`

	parsed, err := html.Parse(doc)
	if err != nil {
		t.Fatalf("html.Parse error: %v", err)
	}
	if len(parsed.Stylesheets) == 0 {
		t.Fatalf("no <style> stylesheet extracted from the XHTML document")
	}

	ss, err := ParseStylesheet(parsed.Stylesheets[0].Text, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet error: %v", err)
	}

	// The surviving rule's selector must be exactly `.centerParent` so that it
	// matches the real element. A rule whose Raw is prose-contaminated (e.g.
	// "...has \"text-align:center\" set.  .centerParent") parses but never
	// matches, so text-align:center is effectively lost.
	var found bool
	for _, r := range ss.Rules {
		if strings.TrimSpace(r.Selector.Raw) == ".centerParent" &&
			r.Declarations["text-align"] == "center" {
			found = true
		}
	}
	if !found {
		t.Fatalf("clean `.centerParent { text-align: center }` rule was dropped: the prose "+
			"inside the in-<style> XML comment contaminated the following rule's selector, so "+
			"it no longer matches the element. Parsed rules: %s", describeRules(ss.Rules))
	}
}

func describeRules(rules []Rule) string {
	var b strings.Builder
	for i, r := range rules {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(r.Selector.Raw)
		b.WriteString(" {")
		first := true
		for k, v := range r.Declarations {
			if !first {
				b.WriteString("; ")
			}
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			first = false
		}
		b.WriteString("}")
	}
	return b.String()
}
