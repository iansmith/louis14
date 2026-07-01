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

// TestMarkerInheritsAppliedTextDecorationsByPosition is the Phase-0 red test for
// LOU-315 cluster 4b. CSS Text Decor 3 §2.1 (decorating-box / propagation):
// an INSIDE ::marker is a non-atomic inline that accumulates the originating
// list item's text-decoration, so the marker text is underlined; an OUTSIDE
// marker is inline-block (atomic) and must NOT inherit it
// (StopPropagateTextDecorations). Regression:
// css-pseudo/marker-text-decoration-skip-ink (all four <ol>s rendered the marker
// with no underline because the marker style never carried the ol's decoration).
// Blink: AppliedTextDecorations accumulation in computed_style.cc, gated by
// StyleAdjuster::AdjustStyleForMarker making outside markers inline-block
// (@ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func TestMarkerInheritsAppliedTextDecorationsByPosition(t *testing.T) {
	build := func(position string) *LayoutInputNode {
		ol := &html.Node{Type: html.ElementNode, TagName: "ol"}
		li := &html.Node{Type: html.ElementNode, TagName: "li", Parent: ol}
		text := &html.Node{Type: html.TextNode, Text: "x", Parent: li}
		li.Children = []*html.Node{text}
		ol.Children = []*html.Node{li}
		liStyle := makeStyle("display", "list-item", "list-style-type", "decimal", "list-style-position", position)
		// Simulate the cascade having accumulated the ol's `text-decoration:
		// underline` onto the li — the test harness builds styles directly and
		// bypasses ResolveAppliedTextDecorations, which the real cascade runs.
		liStyle.AppliedTextDecorations = []css.AppliedTextDecoration{{Lines: css.TextDecorationLineUnderline}}
		styles := map[*html.Node]*css.Style{
			ol: makeStyle("display", "block", "counter-reset", "list-item 0"),
			li: liStyle,
		}
		root := buildTestTree(ol, styles)
		if root == nil || len(root.Children()) == 0 {
			t.Fatalf("no layout tree built for position=%q", position)
		}
		return root.Children()[0]
	}

	// Inside: marker is the first in-flow child and MUST carry the decoration.
	insideKids := build("inside").Children()
	if len(insideKids) == 0 || !insideKids[0].IsMarkerNode() {
		t.Fatalf("inside marker is not the first in-flow child")
	}
	if got := len(insideKids[0].Style().AppliedTextDecorations); got == 0 {
		t.Errorf("inside ::marker did not inherit the list item's text-decoration (AppliedTextDecorations empty)")
	}

	// Outside: marker is inline-block (atomic) and must NOT carry the decoration.
	outsideMarker := build("outside").MarkerNode()
	if outsideMarker == nil {
		t.Fatalf("outside list-item has no markerNode")
	}
	if got := len(outsideMarker.Style().AppliedTextDecorations); got != 0 {
		t.Errorf("outside ::marker must not inherit text-decoration (atomic inline-block), got %d entries", got)
	}
}

// TestMarkerWhiteSpaceByPosition is the red test for LOU-358 category A1.
// Blink core/css/marker.css @ 43cee02dc59fdad798675a735737510ecf0c9064 does
// NOT set white-space on ::marker; only StyleAdjuster::AdjustStyleForMarker's
// outside branch stamps white-space:pre ("Do not break inside the marker, and
// honor the trailing spaces", style_adjuster.cc:502-507 @ 43cee02d). An INSIDE
// marker keeps the inherited value, so its trailing suffix space is
// end-of-line-collapsible and is not underlined past the marker text
// (WPT css-pseudo/marker-text-decoration-skip-ink).
func TestMarkerWhiteSpaceByPosition(t *testing.T) {
	inside := buildSingleListItem(t, "list-item", "list-style-position", "inside", "white-space", "normal")
	kids := inside.Children()
	if len(kids) == 0 || !kids[0].IsMarkerNode() {
		t.Fatalf("inside marker is not the first in-flow child")
	}
	if v, _ := kids[0].Style().Get("white-space"); v == "pre" {
		t.Errorf("inside ::marker white-space = %q, want the inherited value (no UA pre stamp)", v)
	}

	outside := buildSingleListItem(t, "list-item", "white-space", "normal")
	marker := outside.MarkerNode()
	if marker == nil {
		t.Fatalf("outside list-item has no markerNode")
	}
	if v, _ := marker.Style().Get("white-space"); v != "pre" {
		t.Errorf("outside ::marker white-space = %q, want %q (AdjustStyleForMarker outside branch)", v, "pre")
	}
}

// TestFloatedListItemPseudoKeepsMarker is the red test for LOU-358 category B.
// A ::before/::after pseudo with `display: list-item; float: left` must KEEP
// its list-item display when the float blockifies it: Blink blockifies floats
// through EquivalentBlockDisplay (style_adjuster.cc:1172-1178 @
// 43cee02dc59fdad798675a735737510ecf0c9064), and EquivalentBlockDisplay
// returns kListItem unchanged (:258-262 @ 43cee02d). The pseudo therefore
// still generates its own ::marker and takes the implicit list-item counter
// increment (WPT css-pseudo/marker-font-variant-numeric-default/-normal, the
// .before/.after columns).
func TestFloatedListItemPseudoKeepsMarker(t *testing.T) {
	sheet, err := css.ParseStylesheet(`li::before { content: "\200B"; display: list-item; float: left; }`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}

	ol := &html.Node{Type: html.ElementNode, TagName: "ol"}
	li1 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: ol}
	li2 := &html.Node{Type: html.ElementNode, TagName: "li", Parent: ol}
	ol.Children = []*html.Node{li1, li2}

	// Mirrors the WPT tests: the li itself is display:block (so it does NOT
	// increment list-item); only its floated ::before does.
	liStyle := func() *css.Style {
		return makeStyle("display", "block", "list-style-type", "decimal", "list-style-position", "inside")
	}
	styles := map[*html.Node]*css.Style{
		ol:  makeStyle("display", "block", "counter-reset", "list-item 0"),
		li1: liStyle(),
		li2: liStyle(),
	}

	builder := &LayoutTreeBuilder{styles: styles, stylesheets: []*css.Stylesheet{sheet}}
	root := builder.BuildLayoutTree(ol)
	if root == nil || len(root.Children()) != 2 {
		t.Fatalf("layout tree does not have the two list items")
	}

	wantOrdinal := []string{"1", "2"}
	for i, li := range root.Children() {
		kids := li.Children()
		if len(kids) == 0 {
			t.Fatalf("li%d has no children (no ::before generated)", i+1)
		}
		pseudo := kids[0]
		if pseudo.DOMNode == nil || pseudo.DOMNode.TagName != "::before" {
			t.Fatalf("li%d first child is %q, want the ::before pseudo", i+1, pseudo.DOMNode.TagName)
		}
		if !pseudo.Style().IsListItemDisplay() {
			t.Errorf("li%d ::before display = %q; float blockification must preserve list-item", i+1, pseudo.Style().GetDisplay())
		}
		pk := pseudo.Children()
		if len(pk) == 0 || !pk[0].IsMarkerNode() {
			t.Fatalf("li%d ::before has no inside ::marker as its first child", i+1)
		}
		if got := markerText(pk[0]); !strings.HasPrefix(got, wantOrdinal[i]) {
			t.Errorf("li%d ::before marker text = %q, want ordinal starting with %q", i+1, got, wantOrdinal[i])
		}
	}
}

// TestMarkerAnimatedStyleFiltered is the red test for LOU-358 category C's
// style-resolution half: values sampled from a Web Animation targeting
// ::marker (node.MarkerAnimatedStyle) must be applied to the marker style
// through the ::marker allowed-property filter — color applies, opacity is
// rejected. Mirrors Blink StyleResolver::ApplyAnimatedStyle adding
// CSSProperty::kValidForMarker to the cascade filter for kPseudoIdMarker
// styles (style_resolver.cc:2626-2636 @ 43cee02dc59fdad798675a735737510ecf0c9064;
// opacity is not valid_for_marker in css_properties.json5, color is).
// WPT css-pseudo/marker-animate-002.html.
func TestMarkerAnimatedStyleFiltered(t *testing.T) {
	ol := &html.Node{Type: html.ElementNode, TagName: "ul"}
	li := &html.Node{Type: html.ElementNode, TagName: "li", Parent: ol}
	text := &html.Node{Type: html.TextNode, Text: "Item", Parent: li}
	li.Children = []*html.Node{text}
	ol.Children = []*html.Node{li}
	li.MarkerAnimatedStyle = map[string]string{"color": "green", "opacity": "0.1"}

	styles := map[*html.Node]*css.Style{
		ol: makeStyle("display", "block", "counter-reset", "list-item 0"),
		li: makeStyle("display", "list-item", "list-style-type", "disc"),
	}
	root := buildTestTree(ol, styles)
	if root == nil || len(root.Children()) == 0 {
		t.Fatalf("no layout tree built")
	}
	marker := root.Children()[0].MarkerNode()
	if marker == nil {
		t.Fatalf("list item has no markerNode")
	}
	if v, _ := marker.Style().Get("color"); v != "green" {
		t.Errorf("marker color = %q, want %q (marker-valid animated property must apply)", v, "green")
	}
	if v, ok := marker.Style().Get("opacity"); ok && v == "0.1" {
		t.Errorf("marker opacity = %q; opacity is not ::marker-valid and must be rejected", v)
	}
}

// TestInsideMarkerTextBreaksPerOverflowWrapAnywhere is the RED marker-context
// reproduction for WPT css-pseudo/marker-overflow-wrap (and the same shape as
// marker-line-break): an INSIDE marker's text must soft-wrap per the inherited
// overflow-wrap:anywhere when the list item is too narrow — list-style-type
// "ab" at inline-size 0 renders "a" / "b" on two lines.
//
// The plain-span equivalent (TestInlineLayout_OverflowWrapAnywhereAfterOpenTag)
// already passes; what fails is the marker shape: breakTextAtWord's
// remaining<=0 short-circuit keys off len(line.Results)>0, which the ::marker
// box's zero-width open tag satisfies, ending the line before the char-break
// dispatch is ever consulted (the dispatch itself also keys off
// len(line.Results)==0 instead of !line.HasContent). Blink keys both off
// placed content, and overflow-wrap:anywhere re-breaks the overflowing line
// with LineBreakType::kBreakCharacter (LineBreaker::SetCurrentStyleForce,
// line_breaker.cc:4601-4616, RewindOverflow :4241-4341 @
// 43cee02dc59fdad798675a735737510ecf0c9064).
//
// BLOCKED-ON-COORDINATOR (LOU-358): the fix lives in pkg/layout/
// line_breaker.go, which is owned by in-flight LOU-346. Red until the gated
// change lands.
func TestInsideMarkerTextBreaksPerOverflowWrapAnywhere(t *testing.T) {
	ol := &html.Node{Type: html.ElementNode, TagName: "ol"}
	li := &html.Node{Type: html.ElementNode, TagName: "li", Parent: ol}
	ol.Children = []*html.Node{li}

	styles := map[*html.Node]*css.Style{
		ol: makeStyle("display", "block", "counter-reset", "list-item 0"),
		li: makeStyle(
			"display", "list-item",
			"list-style-position", "inside",
			"list-style-type", `"ab"`,
			"overflow-wrap", "anywhere",
			"width", "0px",
		),
	}

	layoutRoot := buildTestTree(ol, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: 300}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: 300}).
		Build()

	result := NewBlockLayoutAlgorithm(testContext(), layoutRoot, space).Layout()

	liFrag := findFragmentByNode(result.Fragment, li)
	if liFrag == nil {
		t.Fatal("list item fragment not found")
	}
	lines := 0
	for i := range liFrag.Children {
		if f := liFrag.Children[i].Fragment; f != nil && f.Type == FragmentLineBox {
			lines++
		}
	}
	if lines < 2 {
		t.Errorf("inside marker text %q at inline-size 0 with overflow-wrap:anywhere must break onto %d+ lines, got %d", "ab", 2, lines)
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
