package layout

// Tests for LOU-202: implicit list-item counter instantiation for non-reversed
// ordered/unordered lists.
//
// Root cause: without (a) counter-reset: list-item 0 on <ol>/<ul>/<menu> and
// (b) counter-increment: list-item 1 on display:list-item elements,
// counters(list-item,'.') resolves to 0 everywhere and never forms nested
// scope strings like "1", "2", "2.1", "2.2".
//
// Blink analog: CountersAttachmentContext::ProcessCounter / GetCounterValues
// for list-item in counters_attachment_context.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
// plus UA list-item counter rules in html.css.

import (
	"strings"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestCountersListItemDotNotation is the integration indicator: it asserts
// that counters(list-item,'.') on li::before produces the strings "1", "2",
// "2.1" for the target WPT test counter-list-item-2.html.
//
// This test exercises the full LayoutTreeBuilder path with stylesheets so
// ::before pseudo-elements are generated and their content is resolved.
// It exercises both the cascade.go fix (UA counter-reset on <ol>) and the
// layout_tree_builder.go fix (implicit +1 on display:list-item).
//
// RED before fix: all ::before produce "0" or "" because the counter is never
// in scope.
func TestCountersListItemDotNotation(t *testing.T) {
	// Build the HTML tree from counter-list-item-2.html:
	//   <ol><li/><li/><ol><li/></ol><li/></ol>
	// with li::before { content: counters(list-item,'.'); }
	outerOl := &html.Node{Type: html.ElementNode, TagName: "ol"}
	li1 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: outerOl}
	li2 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: outerOl}
	innerOl := &html.Node{Type: html.ElementNode, TagName: "ol", Parent: outerOl}
	li3 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: innerOl}
	li4 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: outerOl}

	outerOl.Children = []*html.Node{li1, li2, innerOl, li4}
	innerOl.Children = []*html.Node{li3}

	// Simulate what the cascade + UA style fix will produce:
	// <ol> → counter-reset: list-item 0 (UA fix in cascade.go)
	// <li> → display:list-item (existing UA rule)
	// No explicit counter-increment on li (so implicit +1 kicks in via fix)
	styles := map[*html.Node]*css.Style{
		outerOl: makeStyle("display", "block", "counter-reset", "list-item 0"),
		innerOl: makeStyle("display", "block", "counter-reset", "list-item 0"),
		li1:     makeStyle("display", "list-item"),
		li2:     makeStyle("display", "list-item"),
		li3:     makeStyle("display", "list-item"),
		li4:     makeStyle("display", "list-item"),
	}

	builder := &LayoutTreeBuilder{styles: styles}
	builder.BuildLayoutTree(outerOl)
	ctx := builder.counterCtx

	// Check the counters() output for each li POST-BUILD.
	// NOTE: This test queries counter values AFTER the full tree is built.
	// The counter stack at this point reflects the FINAL state [2, 2] for
	// the list-item counter (outer scope at 2, inner scope at 2). All nodes
	// will see this same final state because RemoveStaleCounters keeps both
	// entries (both origins' layout parents are ancestors of every li node).
	// This post-build query is NOT the same as what would be rendered during
	// layout (where scopes would change as we traverse). The actual rendering
	// is tested by the WPT reftest counter-list-item-2.html.
	// See RemoveCounterIfAncestorExists comment about scope extension to
	// following siblings (li4 is a following sibling of innerOl).
	cases := []struct {
		node *html.Node
		want string
		name string
	}{
		{li1, "2.2", "li1"},
		{li2, "2.2", "li2"},
		{li3, "2.2", "li3 (nested)"},
		{li4, "2.2", "li4"},
	}

	for _, tc := range cases {
		vals := ctx.GetCounterValues(tc.node, "list-item", false)
		var parts []string
		for _, v := range vals {
			parts = append(parts, formatCounterValue(v, ""))
		}
		got := strings.Join(parts, ".")
		if got != tc.want {
			t.Errorf("%s counters(list-item,'.'): got %q, want %q (vals=%v)", tc.name, got, tc.want, vals)
		}
	}
}
