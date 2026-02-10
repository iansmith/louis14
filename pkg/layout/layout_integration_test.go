package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

func TestLayoutInlineContent_SimpleText(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
	}

	// Simple text node
	textNode := &html.Node{
		Type: html.TextNode,
		Text: "Hello World",
	}

	constraint := NewConstraintSpace(400, 300)
	fragments := le.LayoutInlineContent([]*html.Node{textNode}, constraint, 0, nil, nil)

	if len(fragments) < 1 {
		t.Fatalf("Expected at least 1 fragment, got %d", len(fragments))
	}

	// Should have text fragments
	foundText := false
	for _, frag := range fragments {
		if frag.Type == FragmentText {
			foundText = true
			break
		}
	}

	if !foundText {
		t.Error("Expected to find text fragments")
	}
}

func TestLayoutInlineContent_MultipleChildren(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
	}

	// Multiple text nodes
	children := []*html.Node{
		{Type: html.TextNode, Text: "Hello"},
		{Type: html.TextNode, Text: " "},
		{Type: html.TextNode, Text: "World"},
	}

	constraint := NewConstraintSpace(400, 300)
	fragments := le.LayoutInlineContent(children, constraint, 0, nil, nil)

	if len(fragments) < 1 {
		t.Fatalf("Expected at least 1 fragment, got %d", len(fragments))
	}

	// Count text fragments
	textFragCount := 0
	for _, frag := range fragments {
		if frag.Type == FragmentText {
			textFragCount++
		}
	}

	if textFragCount < 3 {
		t.Errorf("Expected at least 3 text fragments, got %d", textFragCount)
	}
}

func TestLayoutInlineContent_NoSideEffects(t *testing.T) {
	// Verify the entire pipeline is pure (no side effects)
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
		floats:      []FloatInfo{}, // Should remain empty
	}

	textNode := &html.Node{
		Type: html.TextNode,
		Text: "Test",
	}

	constraint := NewConstraintSpace(400, 300)

	// Store original state
	originalFloatCount := len(le.floats)

	// Run the pipeline
	le.LayoutInlineContent([]*html.Node{textNode}, constraint, 0, nil, nil)

	// Verify no side effects
	if len(le.floats) != originalFloatCount {
		t.Errorf("LayoutInlineContent modified le.floats! "+
			"Original: %d, After: %d", originalFloatCount, len(le.floats))
	}
}

func TestLayoutInlineContent_WithConstraintSpace(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
	}

	textNode := &html.Node{
		Type: html.TextNode,
		Text: "Test",
	}

	// Create constraint with float
	es := NewExclusionSpace()
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es = es.Add(excl)

	constraint := &ConstraintSpace{
		AvailableSize:  Size{Width: 400, Height: 300},
		ExclusionSpace: es,
	}

	fragments := le.LayoutInlineContent([]*html.Node{textNode}, constraint, 10, nil, nil)

	if len(fragments) < 1 {
		t.Fatalf("Expected at least 1 fragment, got %d", len(fragments))
	}

	// Should have created fragments (constraint was used internally)
	foundText := false
	for _, frag := range fragments {
		if frag.Type == FragmentText {
			foundText = true
			break
		}
	}

	if !foundText {
		t.Error("Expected to find text fragment")
	}
}
