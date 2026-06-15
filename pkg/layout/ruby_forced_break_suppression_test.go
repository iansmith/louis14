package layout

import (
	"strings"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// These tests pin the CSS Ruby forced-break-suppression contract exercised
// by the WPT reftests css-ruby/ruby-line-break-suppression-001 / -003 / -004.
//
// Per CSS Ruby 1 box-fixup (https://drafts.csswg.org/css-ruby-1/#box-fixup)
// and Blink's `kDisableForcedBreakInRubyColumn` gate
// (core/layout/inline/inline_items_builder.cc:74,801,1068,1587 @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
//
//   - A `<br>` inside a ruby base or annotation is suppressed entirely
//     (no forced break, and NO space — matches the -001 reference "ab").
//   - A preserved `\n` (white-space:pre) inside a ruby base/annotation is
//     rewritten to a space (matches the -003 reference "a b").
//   - A ruby box that contains ONLY whitespace does NOT form a base, so a
//     forced break inside it is NOT suppressed (matches the -004 reference,
//     "whitespaces wrapped but not contained in ruby boxes").

func collectForNodes(t *testing.T, root *html.Node, styles map[*html.Node]*css.Style) *InlineItemsData {
	t.Helper()
	tree := buildTestTree(root, styles)
	return CollectInlines(tree)
}

func countControlItems(data *InlineItemsData) int {
	n := 0
	for _, it := range data.Items {
		if it.Type == InlineItemControl {
			n++
		}
	}
	return n
}

func textItemContents(data *InlineItemsData) []string {
	var out []string
	for _, it := range data.Items {
		if it.Type == InlineItemText {
			out = append(out, data.TextContent[it.StartOffset:it.EndOffset])
		}
	}
	return out
}

// <ruby>a<br>b</ruby>: the <br> inside the ruby base is suppressed entirely —
// no forced break and no space. Base text is "ab".
func TestRubyForcedBreak_BrInRubyBaseSuppressedNoSpace(t *testing.T) {
	a := &html.Node{Type: html.TextNode, Text: "a"}
	br := makeNode("br")
	b := &html.Node{Type: html.TextNode, Text: "b"}
	ruby := makeNode("ruby", a, br, b)
	root := makeNode("div", ruby)

	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		ruby: makeStyle("display", "ruby"),
		a:    makeStyle("display", "inline"),
		br:   makeStyle("display", "inline"),
		b:    makeStyle("display", "inline"),
	}

	data := collectForNodes(t, root, styles)

	if got := countControlItems(data); got != 0 {
		t.Errorf("br in ruby base should be suppressed: got %d control items, want 0", got)
	}
	for _, s := range textItemContents(data) {
		if s == " " {
			t.Errorf("br in ruby base must emit nothing, not a space (text items: %q)", textItemContents(data))
		}
	}
}

// Standalone <rbc>a<br>b</rbc> (no enclosing <ruby>): the <br> is still
// suppressed because the standalone ruby-family box forms a base (has
// non-whitespace content). Covers -001 lines 2-5 (rbc/rtc/rb/rt).
func TestRubyForcedBreak_BrInStandaloneRubyBoxSuppressed(t *testing.T) {
	for _, tag := range []string{"rbc", "rtc", "rb", "rt"} {
		a := &html.Node{Type: html.TextNode, Text: "a"}
		br := makeNode("br")
		b := &html.Node{Type: html.TextNode, Text: "b"}
		el := makeNode(tag, a, br, b)
		root := makeNode("div", el)

		styles := map[*html.Node]*css.Style{
			root: makeStyle("display", "block"),
			el:   makeStyle("display", "inline"),
			a:    makeStyle("display", "inline"),
			br:   makeStyle("display", "inline"),
			b:    makeStyle("display", "inline"),
		}

		data := collectForNodes(t, root, styles)
		if got := countControlItems(data); got != 0 {
			t.Errorf("<%s>a<br>b</%s>: br should be suppressed, got %d control items, want 0", tag, tag, got)
		}
	}
}

// Whitespace-only standalone ruby box: <rb> containing only a preserved "\n"
// does NOT form a base, so the forced break is NOT suppressed. This is the
// -004 contract ("whitespaces wrapped but not contained in ruby boxes").
func TestRubyForcedBreak_WhitespaceOnlyRubyBoxNotSuppressed(t *testing.T) {
	nl := &html.Node{Type: html.TextNode, Text: "\n", RawText: "\n"}
	rb := makeNode("rb", nl)
	root := makeNode("div", rb)

	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		rb:   makeStyle("display", "inline", "white-space", "pre"),
		nl:   makeStyle("display", "inline", "white-space", "pre"),
	}

	data := collectForNodes(t, root, styles)
	if got := countControlItems(data); got != 1 {
		t.Errorf("preserved \\n in whitespace-only <rb> must remain a forced break: got %d control items, want 1", got)
	}
}

// Regression: a standalone ruby box (suppress-only sentinel, no real ruby
// column) containing an element with explicit `display:ruby-text` must not
// drive the annotation-column close/reopen machinery — doing so would close a
// column that was never opened (zero checkpoint) and strip unrelated items.
// The collection must keep the box's content intact.
func TestRubyForcedBreak_SentinelWithExplicitRubyTextKeepsItems(t *testing.T) {
	x := &html.Node{Type: html.TextNode, Text: "x"}
	span := makeNode("span", x)
	rb := makeNode("rb", span)
	root := makeNode("div", rb)

	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		rb:   makeStyle("display", "inline"),
		span: makeStyle("display", "ruby-text"),
		x:    makeStyle("display", "inline"),
	}

	data := collectForNodes(t, root, styles)
	if !strings.Contains(data.TextContent, "x") {
		t.Errorf("standalone <rb> content was stripped: TextContent=%q, items=%d", data.TextContent, len(data.Items))
	}
	foundOpen := false
	for _, it := range data.Items {
		if it.Type == InlineItemOpenTag && it.Node == rb {
			foundOpen = true
		}
	}
	if !foundOpen {
		t.Errorf("standalone <rb> OpenTag missing — items were truncated by a spurious column close")
	}
}

// Preserved \n inside a ruby base (white-space:pre) is rewritten to a space,
// not suppressed entirely — matches the -003 reference "a b".
func TestRubyForcedBreak_PreservedNewlineInRubyBaseBecomesSpace(t *testing.T) {
	txt := &html.Node{Type: html.TextNode, Text: "a\nb", RawText: "a\nb"}
	span := makeNode("span", txt)
	ruby := makeNode("ruby", span)
	root := makeNode("div", ruby)

	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		ruby: makeStyle("display", "ruby"),
		span: makeStyle("display", "inline", "white-space", "pre"),
		txt:  makeStyle("display", "inline", "white-space", "pre"),
	}

	data := collectForNodes(t, root, styles)
	if got := countControlItems(data); got != 0 {
		t.Errorf("preserved \\n in ruby base should not be a forced break: got %d control items, want 0", got)
	}
	if !strings.Contains(strings.Join(textItemContents(data), ""), "a b") {
		t.Errorf("preserved \\n in ruby base should become a space: text items %q, want to contain %q", textItemContents(data), "a b")
	}
}
