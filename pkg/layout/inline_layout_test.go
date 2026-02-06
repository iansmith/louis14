package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestInlineLayoutBaseline tests the current inline layout behavior
// before refactoring. These tests capture the expected behavior that
// must be preserved during refactoring.

// createTestEngine creates a minimal LayoutEngine for testing
func createTestEngine() *LayoutEngine {
	return &LayoutEngine{
		floats:      make([]FloatInfo, 0),
		floatBase:   0,
		stylesheets: []*css.Stylesheet{},
		viewport:    struct{ width, height float64 }{width: 800, height: 600},
		counters:    make(map[string][]int),
	}
}

// createTestNode creates a simple HTML node for testing
func createTestNode(tagName string, children ...*html.Node) *html.Node {
	node := &html.Node{
		Type:     html.ElementNode,
		TagName:  tagName,
		Children: children,
	}
	return node
}

// createTextNode creates a text node
func createTextNode(text string) *html.Node {
	return &html.Node{
		Type: html.TextNode,
		Text: text,
	}
}

// TestInlineLayoutNoChildren tests inline layout with no children
func TestInlineLayoutNoChildren(t *testing.T) {
	le := createTestEngine()

	// Create a simple div with no children
	node := createTestNode("div")

	computedStyles := map[*html.Node]*css.Style{
		node: css.NewStyle(),
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should have no children
	if len(box.Children) != 0 {
		t.Errorf("Expected 0 children, got %d", len(box.Children))
	}
}

// TestInlineLayoutTextOnly tests inline layout with a single text node
func TestInlineLayoutTextOnly(t *testing.T) {
	le := createTestEngine()

	// Create a div with text
	textNode := createTextNode("Hello World")
	node := createTestNode("div", textNode)

	computedStyles := map[*html.Node]*css.Style{
		node: css.NewStyle(),
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should have text rendered (implementation may create text boxes or not)
	// For now, just verify it doesn't crash
	if box.Width < 0 || box.Height < 0 {
		t.Error("Box has invalid dimensions")
	}
}

// TestInlineLayoutSingleInlineChild tests inline layout with one inline child
func TestInlineLayoutSingleInlineChild(t *testing.T) {
	le := createTestEngine()

	// Create a div with an inline span
	span := createTestNode("span", createTextNode("Text"))
	node := createTestNode("div", span)

	spanStyle := css.NewStyle()
	spanStyle.Set("display", "inline")

	computedStyles := map[*html.Node]*css.Style{
		node: css.NewStyle(),
		span: spanStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should have at least one child (the span)
	if len(box.Children) == 0 {
		t.Error("Expected at least one child box")
	}
}

// TestInlineLayoutWithFloat tests inline layout with a floated child
func TestInlineLayoutWithFloat(t *testing.T) {
	le := createTestEngine()

	// Create a div with inline text and a floated span
	text1 := createTextNode("Before")
	floatSpan := createTestNode("span", createTextNode("Float"))
	text2 := createTextNode("After")
	node := createTestNode("div", text1, floatSpan, text2)

	floatStyle := css.NewStyle()
	floatStyle.Set("display", "inline")
	floatStyle.Set("float", "left")
	floatStyle.Set("width", "100px")

	computedStyles := map[*html.Node]*css.Style{
		node:      css.NewStyle(),
		floatSpan: floatStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should have children and float should be positioned
	if len(box.Children) == 0 {
		t.Error("Expected children boxes")
	}

	// Check that float was added to float list
	if len(le.floats) == 0 {
		t.Error("Expected float to be added to float list")
	}
}

// TestInlineLayoutBlockInInline tests block-in-inline scenario
func TestInlineLayoutBlockInInline(t *testing.T) {
	le := createTestEngine()

	// Create inline element with block child (should split inline box)
	blockDiv := createTestNode("div", createTextNode("Block"))
	span := createTestNode("span", createTextNode("Before"), blockDiv, createTextNode("After"))
	node := createTestNode("div", span)

	spanStyle := css.NewStyle()
	spanStyle.Set("display", "inline")

	blockStyle := css.NewStyle()
	blockStyle.Set("display", "block")

	computedStyles := map[*html.Node]*css.Style{
		node:     css.NewStyle(),
		span:     spanStyle,
		blockDiv: blockStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should handle block-in-inline (creates fragments)
	// Just verify it doesn't crash for now
}

// TestInlineLayoutComplexNesting tests deeply nested inline elements
func TestInlineLayoutComplexNesting(t *testing.T) {
	le := createTestEngine()

	// Create nested structure: div > span > span > text
	innerSpan := createTestNode("span", createTextNode("Nested"))
	middleSpan := createTestNode("span", innerSpan)
	outerSpan := createTestNode("span", middleSpan)
	node := createTestNode("div", outerSpan)

	spanStyle := css.NewStyle()
	spanStyle.Set("display", "inline")

	computedStyles := map[*html.Node]*css.Style{
		node:       css.NewStyle(),
		outerSpan:  spanStyle,
		middleSpan: spanStyle,
		innerSpan:  spanStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should handle nesting without crashing
	if box.Width < 0 || box.Height < 0 {
		t.Error("Box has invalid dimensions")
	}
}

// TestInlineLayoutMixedContent tests mix of inline, block, and text
func TestInlineLayoutMixedContent(t *testing.T) {
	le := createTestEngine()

	// Create mixed content: text, inline span, block div, text
	text1 := createTextNode("Text1")
	span := createTestNode("span", createTextNode("Inline"))
	blockDiv := createTestNode("div", createTextNode("Block"))
	text2 := createTextNode("Text2")
	node := createTestNode("div", text1, span, blockDiv, text2)

	spanStyle := css.NewStyle()
	spanStyle.Set("display", "inline")

	blockStyle := css.NewStyle()
	blockStyle.Set("display", "block")

	computedStyles := map[*html.Node]*css.Style{
		node:     css.NewStyle(),
		span:     spanStyle,
		blockDiv: blockStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should handle mixed content
	if len(box.Children) == 0 {
		t.Error("Expected children boxes")
	}
}

// TestInlineLayoutMultipleFloats tests multiple floated children
func TestInlineLayoutMultipleFloats(t *testing.T) {
	le := createTestEngine()

	// Create div with multiple floats
	float1 := createTestNode("span", createTextNode("Float1"))
	float2 := createTestNode("span", createTextNode("Float2"))
	text := createTextNode("Text")
	node := createTestNode("div", float1, text, float2)

	floatStyle := css.NewStyle()
	floatStyle.Set("display", "inline")
	floatStyle.Set("float", "left")
	floatStyle.Set("width", "50px")

	computedStyles := map[*html.Node]*css.Style{
		node:   css.NewStyle(),
		float1: floatStyle,
		float2: floatStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should have both floats in float list
	if len(le.floats) < 2 {
		t.Errorf("Expected at least 2 floats, got %d", len(le.floats))
	}
}

// TestInlineLayoutEmptyElements tests elements with no content
func TestInlineLayoutEmptyElements(t *testing.T) {
	le := createTestEngine()

	// Create div with empty spans
	span1 := createTestNode("span")
	span2 := createTestNode("span")
	node := createTestNode("div", span1, span2)

	spanStyle := css.NewStyle()
	spanStyle.Set("display", "inline")

	computedStyles := map[*html.Node]*css.Style{
		node:  css.NewStyle(),
		span1: spanStyle,
		span2: spanStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should handle empty elements gracefully
}

// TestInlineLayoutWithMarginsPadding tests inline elements with box model
func TestInlineLayoutWithMarginsPadding(t *testing.T) {
	le := createTestEngine()

	// Create span with margins and padding
	span := createTestNode("span", createTextNode("Text"))
	node := createTestNode("div", span)

	spanStyle := css.NewStyle()
	spanStyle.Set("display", "inline")
	spanStyle.Set("margin-left", "10px")
	spanStyle.Set("margin-right", "10px")
	spanStyle.Set("padding-left", "5px")
	spanStyle.Set("padding-right", "5px")

	computedStyles := map[*html.Node]*css.Style{
		node: css.NewStyle(),
		span: spanStyle,
	}
	computedStyles[node].Set("display", "block")

	// Layout the node
	box := le.layoutNode(node, 0, 0, 800, computedStyles, nil)

	if box == nil {
		t.Fatal("Expected box to be created")
	}

	// Should apply margins and padding
	if len(box.Children) == 0 {
		t.Error("Expected at least one child box")
	}
}
