package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

func TestComputeMinMaxSizes_TextNode(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// Create a text node
	node := &html.Node{
		Type: html.TextNode,
		Text: "Hello World Test",
	}

	style := css.NewStyle()
	style.Set("font-size", "16px")

	sizes := le.ComputeMinMaxSizes(node, constraint, style)

	// Max size should be full text width
	expectedMaxWidth, _ := text.MeasureTextWithWeight("Hello World Test", 16, false)
	if sizes.MaxContentSize != expectedMaxWidth {
		t.Errorf("Expected max width %f, got %f", expectedMaxWidth, sizes.MaxContentSize)
	}

	// Min size should be width of longest word ("Hello", "World", or "Test")
	helloWidth, _ := text.MeasureTextWithWeight("Hello", 16, false)
	worldWidth, _ := text.MeasureTextWithWeight("World", 16, false)
	testWidth, _ := text.MeasureTextWithWeight("Test", 16, false)

	expectedMinWidth := helloWidth
	if worldWidth > expectedMinWidth {
		expectedMinWidth = worldWidth
	}
	if testWidth > expectedMinWidth {
		expectedMinWidth = testWidth
	}

	if sizes.MinContentSize != expectedMinWidth {
		t.Errorf("Expected min width %f, got %f", expectedMinWidth, sizes.MinContentSize)
	}

	// Min should be <= max
	if sizes.MinContentSize > sizes.MaxContentSize {
		t.Error("Min content size should not exceed max content size")
	}
}

func TestComputeMinMaxSizes_TextNode_SingleWord(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	node := &html.Node{
		Type: html.TextNode,
		Text: "Hello",
	}

	style := css.NewStyle()
	style.Set("font-size", "16px")

	sizes := le.ComputeMinMaxSizes(node, constraint, style)

	// For single word, min should equal max
	if sizes.MinContentSize != sizes.MaxContentSize {
		t.Errorf("Single word: min (%f) should equal max (%f)",
			sizes.MinContentSize, sizes.MaxContentSize)
	}
}

func TestComputeMinMaxSizes_TextNode_Empty(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	node := &html.Node{
		Type: html.TextNode,
		Text: "",
	}

	style := css.NewStyle()
	style.Set("font-size", "16px")

	sizes := le.ComputeMinMaxSizes(node, constraint, style)

	// Empty text: both should be 0
	if sizes.MinContentSize != 0 || sizes.MaxContentSize != 0 {
		t.Errorf("Empty text: expected (0, 0), got (%f, %f)",
			sizes.MinContentSize, sizes.MaxContentSize)
	}
}

func TestComputeMinMaxSizes_InlineWithTextChildren(t *testing.T) {
	le := &LayoutEngine{}
	le.stylesheets = []*css.Stylesheet{} // Empty stylesheets
	constraint := NewConstraintSpace(400, 300)

	// Create: <span>Hello World</span>
	textNode := &html.Node{
		Type: html.TextNode,
		Text: "Hello World",
	}

	span := &html.Node{
		Type:     html.ElementNode,
		TagName:  "span",
		Children: []*html.Node{textNode},
	}
	textNode.Parent = span

	style := css.NewStyle()
	style.Set("display", "inline")
	style.Set("font-size", "16px")

	sizes := le.ComputeMinMaxSizes(span, constraint, style)

	// Should match text node dimensions (no padding/border)
	textMaxWidth, _ := text.MeasureTextWithWeight("Hello World", 16, false)
	worldWidth, _ := text.MeasureTextWithWeight("World", 16, false)

	if sizes.MaxContentSize != textMaxWidth {
		t.Errorf("Expected max width %f, got %f", textMaxWidth, sizes.MaxContentSize)
	}

	if sizes.MinContentSize != worldWidth {
		t.Errorf("Expected min width %f (longest word), got %f",
			worldWidth, sizes.MinContentSize)
	}
}

func TestComputeMinMaxSizes_InlineWithPadding(t *testing.T) {
	le := &LayoutEngine{}
	le.stylesheets = []*css.Stylesheet{}
	constraint := NewConstraintSpace(400, 300)

	textNode := &html.Node{
		Type: html.TextNode,
		Text: "Test",
	}

	span := &html.Node{
		Type:     html.ElementNode,
		TagName:  "span",
		Children: []*html.Node{textNode},
	}
	textNode.Parent = span

	style := css.NewStyle()
	style.Set("display", "inline")
	style.Set("font-size", "16px")
	style.Set("padding-left", "10px")
	style.Set("padding-right", "15px")
	style.Set("padding-top", "5px")
	style.Set("padding-bottom", "5px")

	sizes := le.ComputeMinMaxSizes(span, constraint, style)

	// Should include padding in both min and max
	textWidth, _ := text.MeasureTextWithWeight("Test", 16, false)
	expectedWidth := textWidth + 10 + 15 // left + right padding

	if sizes.MaxContentSize != expectedWidth {
		t.Errorf("Expected max width %f (text + padding), got %f",
			expectedWidth, sizes.MaxContentSize)
	}

	if sizes.MinContentSize != expectedWidth {
		t.Errorf("Expected min width %f (text + padding), got %f",
			expectedWidth, sizes.MinContentSize)
	}
}

func TestComputeMinMaxSizes_BlockWithExplicitWidth(t *testing.T) {
	le := &LayoutEngine{}
	le.stylesheets = []*css.Stylesheet{}
	constraint := NewConstraintSpace(400, 300)

	div := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
	}

	style := css.NewStyle()
	style.Set("display", "block")
	style.Set("width", "200px")

	sizes := le.ComputeMinMaxSizes(div, constraint, style)

	// Explicit width: min = max = width
	if sizes.MinContentSize != 200 || sizes.MaxContentSize != 200 {
		t.Errorf("Expected (200, 200), got (%f, %f)",
			sizes.MinContentSize, sizes.MaxContentSize)
	}
}

func TestComputeMinMaxSizes_DisplayNone(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
	}

	style := css.NewStyle()
	style.Set("display", "none")

	sizes := le.ComputeMinMaxSizes(node, constraint, style)

	// display:none should return 0 sizes
	if sizes.MinContentSize != 0 || sizes.MaxContentSize != 0 {
		t.Errorf("display:none should return (0, 0), got (%f, %f)",
			sizes.MinContentSize, sizes.MaxContentSize)
	}
}

func TestComputeMinMaxSizes_NilNode(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)
	style := &css.Style{}

	sizes := le.ComputeMinMaxSizes(nil, constraint, style)

	// Nil node should return 0 sizes
	if sizes.MinContentSize != 0 || sizes.MaxContentSize != 0 {
		t.Errorf("nil node should return (0, 0), got (%f, %f)",
			sizes.MinContentSize, sizes.MaxContentSize)
	}
}

func TestComputeMinMaxSizes_NilStyle(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
	}

	sizes := le.ComputeMinMaxSizes(node, constraint, nil)

	// Nil style should return 0 sizes
	if sizes.MinContentSize != 0 || sizes.MaxContentSize != 0 {
		t.Errorf("nil style should return (0, 0), got (%f, %f)",
			sizes.MinContentSize, sizes.MaxContentSize)
	}
}

func TestComputeMinMaxSizes_NoSideEffects(t *testing.T) {
	// This test verifies the CRITICAL property: ComputeMinMaxSizes does NOT
	// modify LayoutEngine state (unlike layoutNode which adds to le.floats)

	le := &LayoutEngine{
		floats: []FloatInfo{},
	}
	constraint := NewConstraintSpace(400, 300)

	node := &html.Node{
		Type: html.TextNode,
		Text: "Test",
	}

	style := css.NewStyle()
	style.Set("font-size", "16px")

	// Record initial state
	initialFloatCount := len(le.floats)

	// Call ComputeMinMaxSizes
	le.ComputeMinMaxSizes(node, constraint, style)

	// Verify no side effects
	if len(le.floats) != initialFloatCount {
		t.Errorf("ComputeMinMaxSizes should not modify le.floats! "+
			"Initial: %d, After: %d", initialFloatCount, len(le.floats))
	}
}

func TestComputeMinMaxSizes_InlineWithMultipleChildren(t *testing.T) {
	le := &LayoutEngine{}
	le.stylesheets = []*css.Stylesheet{}
	constraint := NewConstraintSpace(400, 300)

	// Create: <span>Hello <strong>World</strong> Test</span>
	text1 := &html.Node{Type: html.TextNode, Text: "Hello "}
	strong := &html.Node{
		Type:     html.ElementNode,
		TagName:  "strong",
		Children: []*html.Node{{Type: html.TextNode, Text: "World"}},
	}
	strong.Children[0].Parent = strong
	text2 := &html.Node{Type: html.TextNode, Text: " Test"}

	span := &html.Node{
		Type:     html.ElementNode,
		TagName:  "span",
		Children: []*html.Node{text1, strong, text2},
	}
	text1.Parent = span
	strong.Parent = span
	text2.Parent = span

	style := css.NewStyle()
	style.Set("display", "inline")
	style.Set("font-size", "16px")

	sizes := le.ComputeMinMaxSizes(span, constraint, style)

	// Max: sum of all text widths (inline flow)
	// Min: width of longest word across all text

	// Verify max is reasonable (should be > 0)
	if sizes.MaxContentSize <= 0 {
		t.Error("Max content size should be > 0 for text content")
	}

	// Verify min is reasonable (should be > 0)
	if sizes.MinContentSize <= 0 {
		t.Error("Min content size should be > 0 for text content")
	}

	// Verify min <= max
	if sizes.MinContentSize > sizes.MaxContentSize {
		t.Errorf("Min (%f) should not exceed max (%f)",
			sizes.MinContentSize, sizes.MaxContentSize)
	}
}

func TestComputeMinMaxSizes_BlockWithPadding(t *testing.T) {
	le := &LayoutEngine{}
	le.stylesheets = []*css.Stylesheet{}
	constraint := NewConstraintSpace(400, 300)

	// Block with text child and padding
	textNode := &html.Node{Type: html.TextNode, Text: "Content"}
	div := &html.Node{
		Type:     html.ElementNode,
		TagName:  "div",
		Children: []*html.Node{textNode},
	}
	textNode.Parent = div

	style := css.NewStyle()
	style.Set("display", "block")
	style.Set("font-size", "16px")
	style.Set("padding-left", "20px")
	style.Set("padding-right", "20px")
	style.Set("padding-top", "10px")
	style.Set("padding-bottom", "10px")
	style.Set("border-left-width", "2px")
	style.Set("border-right-width", "2px")
	style.Set("border-top-width", "2px")
	style.Set("border-bottom-width", "2px")
	style.Set("border-left-style", "solid")
	style.Set("border-right-style", "solid")
	style.Set("border-top-style", "solid")
	style.Set("border-bottom-style", "solid")

	sizes := le.ComputeMinMaxSizes(div, constraint, style)

	// Should include padding and border
	textWidth, _ := text.MeasureTextWithWeight("Content", 16, false)
	expectedWidth := textWidth + 20 + 20 + 2 + 2 // padding + border

	if sizes.MaxContentSize != expectedWidth {
		t.Errorf("Expected max width %f (content + padding + border), got %f",
			expectedWidth, sizes.MaxContentSize)
	}
}
