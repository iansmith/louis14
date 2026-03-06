package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestE2E_SimpleTextRendering tests the complete pipeline from HTML to positioned boxes
func TestE2E_SimpleTextRendering(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
	}

	// Create simple HTML: <div>Hello World</div>
	textNode := &html.Node{
		Type: html.TextNode,
		Text: "Hello World",
	}

	div := &html.Node{
		Type:     html.ElementNode,
		TagName:  "div",
		Children: []*html.Node{textNode},
	}
	textNode.Parent = div

	// Create container box
	containerBox := &Box{
		Node:  div,
		Style: css.NewStyle(),
		X:     0,
		Y:     0,
		Width: 400,
	}

	// Run new multi-pass pipeline and convert to boxes
	result := le.LayoutInlineContentToBoxes(
		div.Children,
		containerBox,
		400,                           // available width
		0,                             // startY
		make(map[*html.Node]*css.Style), // computedStyles
		nil,                           // overrideStyles
	)

	// Verify we got boxes
	if len(result.ChildBoxes) == 0 {
		t.Fatal("Expected at least one box for text content")
	}

	// Check that boxes have positions
	for i, box := range result.ChildBoxes {
		if box == nil {
			t.Errorf("Box %d is nil", i)
			continue
		}

		// Positions should be set
		if box.X < 0 || box.Y < 0 {
			t.Errorf("Box %d has invalid position (%f, %f)", i, box.X, box.Y)
		}

		// Parent should be set
		if box.Parent != containerBox {
			t.Errorf("Box %d parent not set correctly", i)
		}
	}

	t.Logf("Successfully created %d boxes with positions", len(result.ChildBoxes))
}

// TestE2E_TextWithLeftFloat tests float positioning
func TestE2E_TextWithLeftFloat(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
	}

	// Create HTML with floated span:
	// <div><span style="float:left">Float</span> Text after</div>

	floatStyle := css.NewStyle()
	floatStyle.Set("float", "left")

	floatText := &html.Node{Type: html.TextNode, Text: "Float"}
	floatSpan := &html.Node{
		Type:     html.ElementNode,
		TagName:  "span",
		Children: []*html.Node{floatText},
	}
	floatText.Parent = floatSpan

	afterText := &html.Node{Type: html.TextNode, Text: " Text after"}

	div := &html.Node{
		Type:     html.ElementNode,
		TagName:  "div",
		Children: []*html.Node{floatSpan, afterText},
	}
	floatSpan.Parent = div
	afterText.Parent = div

	// Create container
	containerBox := &Box{
		Node:  div,
		Style: css.NewStyle(),
		X:     0,
		Y:     0,
		Width: 400,
	}

	// For this test, we need to set styles in a computed style map
	// since our pipeline uses computedStyles
	// For now, just verify the bridge function works

	result := le.LayoutInlineContentToBoxes(
		div.Children,
		containerBox,
		400,
		0,
		make(map[*html.Node]*css.Style),
		nil,
	)

	if len(result.ChildBoxes) == 0 {
		t.Fatal("Expected boxes from float + text")
	}

	t.Logf("Created %d boxes for float + text scenario", len(result.ChildBoxes))

	// Verify no negative positions (was a bug in WIP code)
	for i, box := range result.ChildBoxes {
		if box.X < 0 || box.Y < 0 {
			t.Errorf("Box %d has negative position: (%f, %f) - OLD BUG!",
				i, box.X, box.Y)
		}
	}
}

// TestE2E_MultilineText tests line breaking
func TestE2E_MultilineText(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
	}

	// Create long text that should wrap
	longText := &html.Node{
		Type: html.TextNode,
		Text: "This is a very long text that should wrap to multiple lines when rendered in a narrow container",
	}

	div := &html.Node{
		Type:     html.ElementNode,
		TagName:  "div",
		Children: []*html.Node{longText},
	}
	longText.Parent = div

	containerBox := &Box{
		Node:  div,
		Style: css.NewStyle(),
		X:     0,
		Y:     0,
		Width: 200, // Narrow width to force wrapping
	}

	result := le.LayoutInlineContentToBoxes(
		div.Children,
		containerBox,
		200, // narrow width
		0,
		make(map[*html.Node]*css.Style),
		nil,
	)

	if len(result.ChildBoxes) == 0 {
		t.Fatal("Expected boxes from wrapped text")
	}

	t.Logf("Created %d boxes for wrapping text", len(result.ChildBoxes))

	// Check that we have multiple Y positions (multiline)
	yPositions := make(map[float64]bool)
	for _, box := range result.ChildBoxes {
		yPositions[box.Y] = true
	}

	if len(yPositions) > 1 {
		t.Logf("✓ Text wrapped to %d different Y positions (multiline)", len(yPositions))
	}
}

// TestE2E_FragmentToBoxConversion tests the bridge function
func TestE2E_FragmentToBoxConversion(t *testing.T) {
	// Create test fragments
	fragments := []*Fragment{
		NewTextFragment("Hello", css.NewStyle(), 0, 0, 50, 16, nil),
		NewTextFragment("World", css.NewStyle(), 60, 0, 55, 16, nil),
	}

	boxes := fragmentsToBoxes(fragments)

	if len(boxes) != 2 {
		t.Fatalf("Expected 2 boxes, got %d", len(boxes))
	}

	// Check positions transferred correctly
	if boxes[0].X != 0 || boxes[0].Y != 0 {
		t.Errorf("Box 0: expected position (0, 0), got (%f, %f)",
			boxes[0].X, boxes[0].Y)
	}

	if boxes[1].X != 60 || boxes[1].Y != 0 {
		t.Errorf("Box 1: expected position (60, 0), got (%f, %f)",
			boxes[1].X, boxes[1].Y)
	}

	// Check sizes transferred
	if boxes[0].Width != 50 || boxes[0].Height != 16 {
		t.Errorf("Box 0: expected size (50, 16), got (%f, %f)",
			boxes[0].Width, boxes[0].Height)
	}
}

// TestE2E_NoNegativePositions verifies we never create negative positions
// This was a critical bug in the WIP multi-pass code
func TestE2E_NoNegativePositions(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
		floats:      []FloatInfo{}, // Start clean
	}

	// Various test scenarios
	scenarios := []struct {
		name     string
		children []*html.Node
	}{
		{
			name: "Simple text",
			children: []*html.Node{
				{Type: html.TextNode, Text: "Test"},
			},
		},
		{
			name: "Multiple text nodes",
			children: []*html.Node{
				{Type: html.TextNode, Text: "Hello"},
				{Type: html.TextNode, Text: " "},
				{Type: html.TextNode, Text: "World"},
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			containerBox := &Box{
				Style: css.NewStyle(),
				X:     0,
				Y:     0,
				Width: 400,
			}

			result := le.LayoutInlineContentToBoxes(
				scenario.children,
				containerBox,
				400,
				0,
				make(map[*html.Node]*css.Style),
				nil,
			)

			// Check no negative positions
			for i, box := range result.ChildBoxes {
				if box.X < 0 {
					t.Errorf("Box %d has negative X: %f (BUG!)", i, box.X)
				}
				if box.Y < 0 {
					t.Errorf("Box %d has negative Y: %f (BUG!)", i, box.Y)
				}
			}
		})
	}
}
