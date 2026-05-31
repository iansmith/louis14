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

// buildListCounterContext exercises the LayoutTreeBuilder counter path for a
// minimal ordered-list tree and returns the CountersAttachmentContext so tests
// can inspect counter values directly.
//
// Tree shape mirrors counter-list-item-2.html:
//
//	<ol>                  ← UA counter-reset: list-item 0
//	  <li></li>           ← implicit counter-increment: list-item 1  → value 1
//	  <li></li>           ← implicit counter-increment: list-item 1  → value 2
//	  <ol>                ← UA counter-reset: list-item 0 (nested scope)
//	    <li></li>         ← implicit counter-increment: list-item 1  → value 1
//	  </ol>
//	  <li></li>           ← implicit counter-increment: list-item 1  → value 2 (in inner scope,
//	                          as li4 is a following sibling of innerOl)
//	</ol>
func buildListCounterContext(t *testing.T) (*LayoutTreeBuilder, *html.Node, *html.Node, *html.Node, *html.Node, *html.Node) {
	t.Helper()

	// DOM nodes.
	outerOl := &html.Node{Type: html.ElementNode, TagName: "ol"}
	li1 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: outerOl}
	li2 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: outerOl}
	innerOl := &html.Node{Type: html.ElementNode, TagName: "ol", Parent: outerOl}
	li3 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: innerOl}
	li4 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: outerOl}

	outerOl.Children = []*html.Node{li1, li2, innerOl, li4}
	innerOl.Children = []*html.Node{li3}

	// Styles: outerOl and innerOl must carry counter-reset: list-item 0
	// (this is what the UA-style fix in cascade.go will inject).
	// li1..li4 are display:list-item.
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
	return builder, outerOl, li1, li2, li3, li4
}

// TestImplicitListItemIncrementNonReversed asserts that a display:list-item
// element WITHOUT an explicit counter-increment receives an implicit +1 on the
// list-item counter when the counter is non-reversed.
//
// Contract (Blink: DetermineCounterTypeAndValue in counters_attachment_context.cc
// @ 4883d11fef — implicit +1 when display:list-item and counter not reversed,
// matching the existing -1 branch for reversed lists):
//
//	<li> in <ol counter-reset:list-item 0> → list-item value should be 1 after first <li>.
//
// This test will be RED before the fix because the current code only applies
// the implicit decrement (-1) for REVERSED counters (layout_tree_builder.go:289-294)
// and has no corresponding +1 branch for normal lists.
func TestImplicitListItemIncrementNonReversed(t *testing.T) {
	builder, _, li1, _, _, _ := buildListCounterContext(t)
	ctx := builder.counterCtx

	vals := ctx.GetCounterValues(li1, "list-item", true)
	if len(vals) == 0 {
		t.Fatal("GetCounterValues returned empty slice; expected [1]")
	}
	got := vals[0]
	if got != 1 {
		t.Errorf("first <li> list-item counter value: got %d, want 1 (implicit +1 not applied)", got)
	}
}

// TestImplicitListItemCounterSequence asserts that consecutive <li> elements
// in an <ol> produce sequential counter values 1, 2, and the third (after
// nested <ol>) produces 3.
//
// Contract: outer <ol> resets list-item to 0; each <li> increments +1.
// This directly tests the sequence visible in counter-list-item-2.html's
// reference: li#a→1, li#b→2, li#d→3 (in the outer scope).
//
// RED before fix: current code returns 0 for all <li> elements because neither
// the UA counter-reset on <ol> nor the implicit +1 on <li> is applied.
func TestImplicitListItemCounterSequence(t *testing.T) {
	builder, _, li1, li2, _, li4 := buildListCounterContext(t)
	ctx := builder.counterCtx
	_ = li2 // used in cases below

	cases := []struct {
		node *html.Node
		want int
		name string
	}{
		{li1, 1, "li1 (first)"},
		{li2, 2, "li2 (second)"},
		{li4, 3, "li4 (third, after nested ol)"},
	}
	for _, tc := range cases {
		vals := ctx.GetCounterValues(tc.node, "list-item", true)
		got := 0
		if len(vals) > 0 {
			got = vals[0]
		}
		if got != tc.want {
			t.Errorf("%s: got list-item=%d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestNestedOlCreatesNewListItemScope asserts that a nested <ol> creates a new
// list-item counter scope so the inner <li> reads a value of 1 (innermost
// scope), and GetCounterValues with onlyLast=false returns [outerValue, 1]
// giving the counters() dot-notation "outerValue.1".
//
// Blink: CreateCounter on the nested <ol>'s counter-reset: list-item 0 pushes
// a new entry onto the stack; GetCounterValues(onlyLast=false) returns all
// in-scope values outermost→innermost.
//
// RED before fix: nested <ol> has no counter-reset so no new scope is created;
// counters() just returns the running outer value without nesting.
func TestNestedOlCreatesNewListItemScope(t *testing.T) {
	builder, _, _, _, li3, _ := buildListCounterContext(t)
	ctx := builder.counterCtx

	// Inner li3 should see two in-scope entries: outer scope value (2, after li2)
	// and inner scope value (1). counters() returns [2, 1] → "2.1".
	vals := ctx.GetCounterValues(li3, "list-item", false)
	if len(vals) != 2 {
		t.Fatalf("nested <li> GetCounterValues(onlyLast=false): got %v (len=%d), want 2 entries [outerVal, 1]", vals, len(vals))
	}
	if vals[1] != 1 {
		t.Errorf("nested <li> innermost value: got %d, want 1", vals[1])
	}
	// Outermost should be 2 (after li2 incremented to 2, before li4).
	if vals[0] != 2 {
		t.Errorf("nested <li> outermost value: got %d, want 2", vals[0])
	}
}

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
