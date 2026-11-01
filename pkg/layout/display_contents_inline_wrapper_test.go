package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

// displayContentsFirstLine002Tree builds the box tree of WPT
// css-display/display-contents-first-line-002.html:
//
//	<div id=container><span class=contents>P</span><span>ASS</span></div>
//
// with `span { color: green }`, `.contents { display: contents }` and a
// ::first-line color of red on #container.
func displayContentsFirstLine002Tree() (tree *LayoutInputNode, container *css.Style) {
	p := &html.Node{Type: html.TextNode, Text: "P"}
	ass := &html.Node{Type: html.TextNode, Text: "ASS"}
	contents := makeNode("span", p)
	span := makeNode("span", ass)
	div := makeNode("div", contents, span)

	container = makeStyle("display", "block")
	contentsStyle := makeStyle("display", "contents", "color", "green")
	contentsStyle.CascadedProps = map[string]bool{"display": true, "color": true}
	spanStyle := makeStyle("display", "inline", "color", "green")
	spanStyle.CascadedProps = map[string]bool{"color": true}
	styles := map[*html.Node]*css.Style{
		div:      container,
		contents: contentsStyle,
		span:     spanStyle,
	}
	return buildTestTree(div, styles), container
}

// TestDisplayContentsTextGetsAnonymousInlineWrapper (LOU-567): a text child
// of a display:contents element whose inherited style differs from the
// layout parent's is wrapped in an anonymous LayoutInline carrying the
// contents element's inherited properties. Mirrors Blink
// LayoutTreeBuilderForText::CreateInlineWrapperForDisplayContentsIfNeeded
// (core/dom/layout_tree_builder.cc:165-184 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and
// StyleResolver::CreateInheritedDisplayContentsStyleIfNeeded
// (core/css/resolver/style_resolver.cc:3351-3358 @ a9f50e52).
func TestDisplayContentsTextGetsAnonymousInlineWrapper(t *testing.T) {
	tree, _ := displayContentsFirstLine002Tree()
	if len(tree.Children()) != 2 {
		t.Fatalf("container: got %d children, want 2 (wrapper, span)", len(tree.Children()))
	}
	wrapper := tree.Children()[0]
	if !wrapper.IsAnonymous() || wrapper.IsText() {
		t.Fatalf("first child: anonymous=%v text=%v, want an anonymous inline wrapper", wrapper.IsAnonymous(), wrapper.IsText())
	}
	if got := wrapper.Style().GetDisplay(); got != css.DisplayInline {
		t.Errorf("wrapper display = %q, want inline", got)
	}
	if got, _ := wrapper.Style().Get("color"); got != "green" {
		t.Errorf("wrapper color = %q, want the contents element's inherited green", got)
	}
	if len(wrapper.Children()) != 1 || !wrapper.Children()[0].IsText() {
		t.Fatalf("wrapper children = %d, want exactly the text node", len(wrapper.Children()))
	}
	if wrapper.Children()[0].Style() != wrapper.Style() {
		t.Errorf("wrapped text must take the wrapper style (layout_tree_builder.cc:196-197 @ a9f50e52)")
	}
}

// TestDisplayContentsTextNoWrapperWhenInheritedEqual (LOU-567): no wrapper
// when the contents element changes no inherited property
// (style_resolver.cc:3354-3356 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestDisplayContentsTextNoWrapperWhenInheritedEqual(t *testing.T) {
	txt := &html.Node{Type: html.TextNode, Text: "x"}
	contents := makeNode("span", txt)
	div := makeNode("div", contents)
	styles := map[*html.Node]*css.Style{
		div:      makeStyle("display", "block", "color", "blue"),
		contents: makeStyle("display", "contents", "color", "blue", "border-top-width", "5px"),
	}
	tree := buildTestTree(div, styles)
	if len(tree.Children()) != 1 || !tree.Children()[0].IsText() {
		t.Fatalf("got %d children (first text=%v), want the bare text node", len(tree.Children()), len(tree.Children()) > 0 && tree.Children()[0].IsText())
	}
}

// TestFirstLineSkipsDisplayContentsInlineWrapper (LOU-567): the text in the
// anonymous display:contents wrapper gets no ::first-line style at all —
// Blink's LayoutObject::FirstLineStyleWithoutFallback returns the parent's
// first-line style for text (layout_object.cc:4324-4328 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and null for an anonymous
// LayoutInline (the !IsAnonymous() guard at :4384), so FirstLineStyle falls
// back to the wrapper's own style. The "P" of
// display-contents-first-line-002 therefore stays green, not red.
func TestFirstLineSkipsDisplayContentsInlineWrapper(t *testing.T) {
	tree, container := displayContentsFirstLine002Tree()
	data := CollectInlines(tree)
	line := &LineInfo{}
	for _, item := range data.Items {
		line.Results = append(line.Results, InlineItemResult{Item: item})
	}
	firstLine := makeStyle("color", "red")
	applyFirstLineStyles(line, firstLine, container, nil, text.FontConfig{})

	for i := range line.Results {
		r := &line.Results[i]
		if r.Item.Type != InlineItemText {
			continue
		}
		got, _ := r.EffectiveStyle().Get("color")
		if got != "green" {
			t.Errorf("text %q: color %q, want green", data.TextContent[r.Item.StartOffset:r.Item.EndOffset], got)
		}
	}
}

// TestDisplayContentsWrapperItemsSkipFirstLineMeasuring (LOU-567): the line
// breaker measures items in the anonymous wrapper with their own style, the
// same rule applyFirstLineStyles follows, and the flag survives the bidi
// level split that runs before line breaking.
func TestDisplayContentsWrapperItemsSkipFirstLineMeasuring(t *testing.T) {
	tree, _ := displayContentsFirstLine002Tree()
	data := CollectInlines(tree)
	ResolveBidiLevels(data, DirectionLTR)
	SplitItemsAtLevelBoundaries(data)

	lb := &LineBreaker{useFirstLineStyle: true, firstLineStyle: makeStyle("color", "red")}
	sawWrapped := false
	for _, item := range data.Items {
		if item.Type != InlineItemText {
			continue
		}
		txt := data.TextContent[item.StartOffset:item.EndOffset]
		got, _ := lb.measuringStyle(item).Get("color")
		switch txt {
		case "P":
			sawWrapped = true
			if !item.inAnonymousInline || got != "green" {
				t.Errorf("text P: inAnonymousInline=%v measuring color %q, want true and green", item.inAnonymousInline, got)
			}
		case "ASS":
			if item.inAnonymousInline || got != "red" {
				t.Errorf("text ASS: inAnonymousInline=%v measuring color %q, want false and the unblocked ::first-line red", item.inAnonymousInline, got)
			}
		}
	}
	if !sawWrapped {
		t.Fatalf("no text item for P in %q", data.TextContent)
	}
}
