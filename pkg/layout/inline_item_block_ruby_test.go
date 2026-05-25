package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestCollectInlines_BlockRubyEmitsRubyColumn is the regression guard for
// the W1.2 block-ruby integration: when a `display: block ruby` element
// is laid out, its principal box is rewritten to `display: block` and a
// single anonymous inline `display: ruby` child holds the actual ruby
// content. The inline-item collection must descend into that anonymous
// child and emit InlineItemOpenRubyColumn / InlineItemCloseRubyColumn
// just as it would for a real `<ruby>` element — otherwise the line
// breaker never invokes handleRuby and the ruby base + annotation render
// as unstyled inline text.
//
// Mirrors Blink's LayoutRubyAsBlock + InlineItemsBuilder pipeline
// (`core/layout/layout_ruby_as_block.cc`,
// `core/layout/inline/inline_items_builder.cc:1550-1595`) at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestCollectInlines_BlockRubyEmitsRubyColumn(t *testing.T) {
	// DOM: <ruby style="display: block ruby">べ<rt>る</rt></ruby>
	be := &html.Node{Type: html.TextNode, Text: "べ"}
	ru := &html.Node{Type: html.TextNode, Text: "る"}
	rt := makeNode("rt", ru)
	ruby := makeNode("ruby", be, rt)

	styles := map[*html.Node]*css.Style{
		ruby: makeStyle("display", "block ruby"),
		be:   makeStyle("display", "inline"),
		rt:   makeStyle("display", "ruby-text"),
		ru:   makeStyle("display", "inline"),
	}

	tree := buildTestTree(ruby, styles)

	// After box-fixup the principal box display has been rewritten to
	// `block` and a single anonymous inline `display:ruby` child wraps
	// the original children.
	if got := tree.Style().GetDisplay(); got != css.DisplayBlock {
		t.Fatalf("principal display: got %q, want %q (wrapBlockRubyAsTwoBox should have rewritten)", got, css.DisplayBlock)
	}
	if len(tree.Children()) != 1 {
		t.Fatalf("principal children: got %d, want 1 (anon inline-ruby wrapper)", len(tree.Children()))
	}
	anon := tree.Children()[0]
	if !anon.IsAnonymous() {
		t.Fatalf("first child should be anonymous wrapper")
	}
	if got := anon.Style().GetDisplay(); got != css.DisplayRuby {
		t.Fatalf("anon child display: got %q, want %q", got, css.DisplayRuby)
	}

	// Collect inline items as the IFC would. The collection must descend
	// into the anonymous ruby wrapper and emit the ruby-column items.
	data := CollectInlines(tree)
	foundOpen, foundClose := 0, 0
	for _, item := range data.Items {
		switch item.Type {
		case InlineItemOpenRubyColumn:
			foundOpen++
		case InlineItemCloseRubyColumn:
			foundClose++
		}
	}
	if foundOpen == 0 || foundClose == 0 {
		t.Errorf("expected at least one OpenRubyColumn/CloseRubyColumn pair, got open=%d close=%d", foundOpen, foundClose)
		t.Log("items emitted:")
		for i, item := range data.Items {
			t.Logf("  [%d] type=%v", i, item.Type)
		}
	}
}
