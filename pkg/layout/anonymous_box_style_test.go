package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"strings"
	"testing"
)

// TestAnonymousTableBoxesInheritCustomProperties (LOU-429): the CSS 2.1
// §17.2.1 anonymous row-group, row and cell around stray table content carry
// the table's custom properties, so a var() on them resolves. Blink builds
// each wrapper with StyleResolver::CreateAnonymousStyleWithDisplay
// (LayoutTableSection/Row/Cell::CreateAnonymousWithParent,
// core/layout/table/layout_table_section.cc:20, layout_table_row.cc:21,
// layout_table_cell.cc:30 @ 5992657f556bcd9d207a4aca187488462f027cab), whose
// builder starts from the parent's inherited values, custom properties
// included (core/css/resolver/style_resolver.cc:3610 @ 5992657f556b).
func TestAnonymousTableBoxesInheritCustomProperties(t *testing.T) {
	text := &html.Node{Type: html.TextNode, Text: "abc"}
	table := makeNode("div", text)
	styles := map[*html.Node]*css.Style{
		table: makeStyle("display", "table", "--x", "10px"),
		text:  makeStyle("display", "inline"),
	}
	tree := buildTestTree(table, styles)

	node := tree
	for _, want := range []css.DisplayType{css.DisplayTableRowGroup, css.DisplayTableRow, css.DisplayTableCell} {
		if len(node.Children()) != 1 {
			t.Fatalf("%s parent: got %d children, want 1", want, len(node.Children()))
		}
		node = node.Children()[0]
		if !node.IsAnonymous() || node.Style().GetDisplay() != want {
			t.Fatalf("got anonymous=%v display %q, want anonymous %q", node.IsAnonymous(), node.Style().GetDisplay(), want)
		}
		if got, ok := node.Style().Get("--x"); !ok || got != "10px" {
			t.Errorf("anonymous %s: --x = %q (present %v), want 10px", want, got, ok)
		}
	}
	cell := node.Style()
	cell.Set("width", "var(--x)")
	if got, _ := cell.GetLength("width"); got != 10 {
		t.Errorf("anonymous table-cell: width: var(--x) = %v, want 10", got)
	}
}

// TestAnonymousBoxesCopyUnicodeBidi (LOU-429): every anonymous box takes its
// parent's unicode-bidi, as Blink's builder does
// (builder.SetUnicodeBidi(parent_style.GetUnicodeBidi()),
// core/css/resolver/style_resolver.cc:3611 @
// 5992657f556bcd9d207a4aca187488462f027cab), for anonymous table cells
// (layout_table_cell.cc:30), anonymous blocks (layout_block.cc:515) and
// block-ruby's anonymous inline ruby (layout_ruby_as_block.cc:28), all @
// 5992657f556b.
func TestAnonymousBoxesCopyUnicodeBidi(t *testing.T) {
	t.Run("anonymous table cell under bidi-override row", func(t *testing.T) {
		text := &html.Node{Type: html.TextNode, Text: "abc"}
		row := makeNode("div", text)
		table := makeNode("div", row)
		styles := map[*html.Node]*css.Style{
			table: makeStyle("display", "table"),
			row:   makeStyle("display", "table-row", "unicode-bidi", "bidi-override", "direction", "rtl"),
			text:  makeStyle("display", "inline"),
		}
		tree := buildTestTree(table, styles)
		// table > anonymous row-group > row > anonymous cell.
		cell := tree.Children()[0].Children()[0].Children()[0]
		if !cell.IsAnonymous() || cell.Style().GetDisplay() != css.DisplayTableCell {
			t.Fatalf("got anonymous=%v display %q, want an anonymous table-cell", cell.IsAnonymous(), cell.Style().GetDisplay())
		}
		if got, ok := cell.Style().Get("unicode-bidi"); !ok || got != "bidi-override" {
			t.Errorf("anonymous cell unicode-bidi = %q (present %v), want bidi-override", got, ok)
		}
		data := CollectInlines(cell)
		injectBlockBidiControls(cell.Style(), data)
		if !strings.Contains(data.TextContent, "‮") || !strings.Contains(data.TextContent, "‬") {
			t.Errorf("anonymous cell inline content %q lacks the RLO/PDF override the row's bidi-override requires", data.TextContent)
		}
	})

	for _, bidi := range []string{"isolate", "plaintext"} {
		t.Run("anonymous block under "+bidi+" block", func(t *testing.T) {
			text := &html.Node{Type: html.TextNode, Text: "abc"}
			inner := makeNode("div")
			outer := makeNode("div", text, inner)
			styles := map[*html.Node]*css.Style{
				outer: makeStyle("display", "block", "unicode-bidi", bidi, "--x", "10px"),
				text:  makeStyle("display", "inline"),
				inner: makeStyle("display", "block"),
			}
			tree := buildTestTree(outer, styles)
			anon := tree.Children()[0]
			if !anon.IsAnonymous() || anon.Style().GetDisplay() != css.DisplayBlock {
				t.Fatalf("got anonymous=%v display %q, want an anonymous block", anon.IsAnonymous(), anon.Style().GetDisplay())
			}
			if got, ok := anon.Style().Get("unicode-bidi"); !ok || got != bidi {
				t.Errorf("anonymous block unicode-bidi = %q (present %v), want %s", got, ok, bidi)
			}
			if got, ok := anon.Style().Get("--x"); !ok || got != "10px" {
				t.Errorf("anonymous block --x = %q (present %v), want 10px", got, ok)
			}
		})
	}

	t.Run("block ruby's anonymous inline ruby", func(t *testing.T) {
		text := &html.Node{Type: html.TextNode, Text: "abc"}
		ruby := makeNode("ruby", text)
		styles := map[*html.Node]*css.Style{
			ruby: makeStyle("display", "block ruby", "unicode-bidi", "isolate", "--x", "10px"),
			text: makeStyle("display", "inline"),
		}
		tree := buildTestTree(ruby, styles)
		anon := tree.Children()[0]
		if !anon.IsAnonymous() || anon.Style().GetDisplay() != css.DisplayRuby {
			t.Fatalf("got anonymous=%v display %q, want the anonymous inline ruby", anon.IsAnonymous(), anon.Style().GetDisplay())
		}
		if got, ok := anon.Style().Get("unicode-bidi"); !ok || got != "isolate" {
			t.Errorf("anonymous inline ruby unicode-bidi = %q (present %v), want isolate", got, ok)
		}
		if got, ok := anon.Style().Get("--x"); !ok || got != "10px" {
			t.Errorf("anonymous inline ruby --x = %q (present %v), want 10px", got, ok)
		}
	})
}
