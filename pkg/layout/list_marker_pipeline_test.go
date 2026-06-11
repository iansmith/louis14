package layout

// Tests for LOU-236: the outside-marker pipeline and inline list-item markers.
//
// Root cause: layout_tree_builder.go never assigns lin.markerNode, so
// ListMarkerBlockNodeIfListItem() always returns nil and the
// UnpositionedListMarker carry/claim protocol (block_layout.go:86, :439)
// never runs — ALL outside list markers render as nothing. Additionally,
// createMarkerPseudoElement refuses display:inline list-item, so inline list
// items have no marker in any position.
//
// Blink analog: ComputedStyle::MarkerShouldBeInside (computed_style.cc:2817 —
// an inline list item's outside marker is equivalent to inside),
// StyleAdjuster::AdjustStyleForMarker (style_adjuster.cc:478 — outside
// markers become inline-block, white-space:pre), BlockNode::
// ListMarkerBlockNodeIfListItem feeding UnpositionedListMarker.
// All @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.

import (
	"strings"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// buildSingleListItem builds <ol><li>text</li></ol> with the given li display
// value and returns the li's LayoutInputNode.
func buildSingleListItem(t *testing.T, liDisplay string, extraLiProps ...string) *LayoutInputNode {
	t.Helper()
	ol := &html.Node{Type: html.ElementNode, TagName: "ol"}
	li := &html.Node{Type: html.ElementNode, TagName: "li", Parent: ol}
	text := &html.Node{Type: html.TextNode, Text: "alpha", Parent: li}
	li.Children = []*html.Node{text}
	ol.Children = []*html.Node{li}

	// The UA stylesheet gives `ol li` list-style-type:decimal; tests build
	// styles directly so declare it explicitly.
	liProps := append([]string{"display", liDisplay, "list-style-type", "decimal"}, extraLiProps...)
	styles := map[*html.Node]*css.Style{
		ol: makeStyle("display", "block", "counter-reset", "list-item 0"),
		li: makeStyle(liProps...),
	}
	root := buildTestTree(ol, styles)
	if root == nil || len(root.Children()) == 0 {
		t.Fatalf("no layout tree built")
	}
	return root.Children()[0]
}

// A block-level list item with default list-style-position (outside) must
// expose its marker through ListMarkerBlockNodeIfListItem so the block
// layout algorithm's carry/claim protocol can place it. The marker must NOT
// appear among the item's in-flow children.
func TestOutsideMarkerWiredForBlockListItem(t *testing.T) {
	li := buildSingleListItem(t, "list-item")

	marker := li.MarkerNode()
	if marker == nil {
		t.Fatalf("block list-item has no markerNode; outside-marker pipeline is dead")
	}
	if !marker.MarkerIsOutside {
		t.Errorf("marker.MarkerIsOutside = false, want true for list-style-position:outside")
	}
	if li.ListMarkerBlockNodeIfListItem() == nil {
		t.Errorf("ListMarkerBlockNodeIfListItem() = nil; carry/claim protocol cannot run")
	}
	for _, c := range li.Children() {
		if c.IsMarkerNode() {
			t.Errorf("outside marker must not be an in-flow child of the list item")
		}
	}
	if got := markerText(marker); !strings.HasPrefix(got, "1") {
		t.Errorf("marker text = %q, want ordinal starting with %q", got, "1")
	}
}

// display:inline list-item: css-lists §list-style-position-outside says
// outside is equivalent to inside for inline list items (Blink
// ComputedStyle::MarkerShouldBeInside). The marker must be the first in-flow
// child and must NOT be offered to the carry/claim protocol.
func TestInsideMarkerForInlineListItem(t *testing.T) {
	li := buildSingleListItem(t, "inline list-item")

	marker := li.MarkerNode()
	if marker == nil {
		t.Fatalf("inline list-item has no markerNode")
	}
	if li.ListMarkerBlockNodeIfListItem() != nil {
		t.Errorf("inline list-item marker must not enter the outside carry/claim protocol")
	}
	kids := li.Children()
	if len(kids) == 0 || !kids[0].IsMarkerNode() {
		t.Fatalf("inline list-item's first child is not the ::marker (inside placement)")
	}
	if got := markerText(kids[0]); !strings.HasPrefix(got, "1") {
		t.Errorf("marker text = %q, want ordinal starting with %q", got, "1")
	}
}

// display:inline flow-root list-item is NOT an inline box (it is an atomic
// inline block container), so its marker stays outside — Blink
// MarkerShouldBeInside checks kInlineListItem only, not
// kInlineFlowRootListItem.
func TestOutsideMarkerForInlineFlowRootListItem(t *testing.T) {
	li := buildSingleListItem(t, "inline flow-root list-item")

	marker := li.MarkerNode()
	if marker == nil {
		t.Fatalf("inline flow-root list-item has no markerNode")
	}
	if !marker.MarkerIsOutside {
		t.Errorf("inline flow-root list-item marker must be outside")
	}
	if li.ListMarkerBlockNodeIfListItem() == nil {
		t.Errorf("ListMarkerBlockNodeIfListItem() = nil for inline flow-root list-item")
	}
}

// list-style-position:inside on a block list item keeps the existing inside
// behavior: marker is the first in-flow child, not in the outside protocol.
func TestInsideMarkerForBlockListItemInsidePosition(t *testing.T) {
	li := buildSingleListItem(t, "list-item", "list-style-position", "inside")

	if li.ListMarkerBlockNodeIfListItem() != nil {
		t.Errorf("position:inside marker must not enter the outside carry/claim protocol")
	}
	kids := li.Children()
	if len(kids) == 0 || !kids[0].IsMarkerNode() {
		t.Fatalf("inside marker is not the first in-flow child")
	}
}

// markerText returns the concatenated text content of a marker node's
// children.
func markerText(marker *LayoutInputNode) string {
	var sb strings.Builder
	for _, c := range marker.Children() {
		sb.WriteString(c.TextContent())
	}
	return sb.String()
}
