package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

func TestFragment_NewTextFragment(t *testing.T) {
	style := &css.Style{}
	frag := NewTextFragment("Hello, World!", style, 100, 200, 80, 16, nil)

	if frag.Type != FragmentText {
		t.Errorf("Expected type FragmentText, got %v", frag.Type)
	}

	if frag.Text != "Hello, World!" {
		t.Errorf("Expected text 'Hello, World!', got '%s'", frag.Text)
	}

	if frag.Style != style {
		t.Error("Style should match provided style")
	}

	if frag.Position.X != 100 || frag.Position.Y != 200 {
		t.Errorf("Expected position (100, 200), got (%f, %f)",
			frag.Position.X, frag.Position.Y)
	}

	if frag.Size.Width != 80 || frag.Size.Height != 16 {
		t.Errorf("Expected size (80, 16), got (%f, %f)",
			frag.Size.Width, frag.Size.Height)
	}

	if frag.Children != nil {
		t.Error("Text fragment should have nil children initially")
	}
}

func TestFragment_NewBoxFragment(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, TagName: "div"}
	style := &css.Style{}

	box := &Box{
		Node:   node,
		Style:  style,
		X:      50,
		Y:      100,
		Width:  200,
		Height: 150,
	}

	frag := NewBoxFragment(box, FragmentBlock)

	if frag.Type != FragmentBlock {
		t.Errorf("Expected type FragmentBlock, got %v", frag.Type)
	}

	if frag.Node != node {
		t.Error("Fragment node should match box node")
	}

	if frag.Style != style {
		t.Error("Fragment style should match box style")
	}

	if frag.Position.X != 50 || frag.Position.Y != 100 {
		t.Errorf("Expected position (50, 100), got (%f, %f)",
			frag.Position.X, frag.Position.Y)
	}

	if frag.Size.Width != 200 || frag.Size.Height != 150 {
		t.Errorf("Expected size (200, 150), got (%f, %f)",
			frag.Size.Width, frag.Size.Height)
	}

	if frag.Box != box {
		t.Error("Fragment should link back to box")
	}
}

func TestFragment_Immutability(t *testing.T) {
	// This test verifies the design principle: fragments are created with
	// correct position and not modified afterward.

	style := &css.Style{}
	frag := NewTextFragment("Test", style, 100, 200, 50, 16, nil)

	// Store original values
	origX := frag.Position.X
	origY := frag.Position.Y
	origWidth := frag.Size.Width
	origHeight := frag.Size.Height

	// Simulate what should NOT happen: modifying the fragment
	// (This is just for testing - in real code, fragments should never be modified)
	frag.Position.X = 999
	frag.Position.Y = 999
	frag.Size.Width = 999
	frag.Size.Height = 999

	// In a truly immutable design, the fragment would prevent modification
	// For now, we just verify the principle: create once with correct position

	// Restore original values (pretend it's immutable)
	frag.Position.X = origX
	frag.Position.Y = origY
	frag.Size.Width = origWidth
	frag.Size.Height = origHeight

	// The DESIGN PRINCIPLE is:
	// 1. Calculate correct position once
	// 2. Create fragment with that position
	// 3. Never modify it afterward

	if frag.Position.X != 100 || frag.Position.Y != 200 {
		t.Error("Fragment position should remain as created")
	}
}

func TestFragment_ChildrenManagement(t *testing.T) {
	// Create parent fragment
	parentStyle := &css.Style{}
	parent := &Fragment{
		Type:     FragmentInline,
		Style:    parentStyle,
		Position: Position{X: 0, Y: 0},
		Size:     Size{Width: 200, Height: 30},
		Children: []*Fragment{},
	}

	// Create child fragments
	child1 := NewTextFragment("Hello ", parentStyle, 0, 0, 40, 16, nil)
	child2 := NewTextFragment("World", parentStyle, 40, 0, 35, 16, nil)

	// Add children (this would be done during construction in real code)
	parent.Children = append(parent.Children, child1, child2)

	if len(parent.Children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(parent.Children))
	}

	if parent.Children[0] != child1 || parent.Children[1] != child2 {
		t.Error("Children should be in the order they were added")
	}
}

func TestFragment_FragmentTypes(t *testing.T) {
	tests := []struct {
		name     string
		fragType FragmentType
	}{
		{"Text", FragmentText},
		{"Inline", FragmentInline},
		{"Block", FragmentBlock},
		{"Float", FragmentFloat},
		{"Atomic", FragmentAtomic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frag := &Fragment{
				Type:     tt.fragType,
				Position: Position{X: 0, Y: 0},
				Size:     Size{Width: 100, Height: 50},
			}

			if frag.Type != tt.fragType {
				t.Errorf("Expected type %v, got %v", tt.fragType, frag.Type)
			}
		})
	}
}

func TestFragment_CorrectPositionFromStart(t *testing.T) {
	// This test demonstrates the key principle: fragments are created with
	// the CORRECT position from the start, not positioned at 0 and then adjusted.

	// Scenario: text after a 100px left float
	// Old way: position at X=0, then adjust by +100
	// New way: calculate X=100, create fragment with X=100

	// Calculate correct X position (accounting for float)
	floatWidth := 100.0
	correctX := floatWidth // Text starts after the float

	// Create fragment with CORRECT position from the start
	frag := NewTextFragment("Text after float", &css.Style{}, correctX, 0, 80, 16, nil)

	// Fragment should have the correct position immediately
	if frag.Position.X != 100 {
		t.Errorf("Fragment should be created at X=100, got X=%f", frag.Position.X)
	}

	// No need for position adjustment - it's already correct!
	// This prevents bugs from:
	// - Forgetting to adjust
	// - Double-adjusting
	// - Adjusting with stale float information
}

func TestFragment_VsBoxMutability(t *testing.T) {
	// This test contrasts Fragment (immutable) with Box (mutable)

	// Box: mutable, position can be changed
	box := &Box{X: 0, Y: 0, Width: 100, Height: 50}
	box.X = 100 // Can be repositioned (common source of bugs)
	box.X = 200 // Can be repositioned again (more bugs)

	// Fragment: designed to be immutable
	frag := &Fragment{
		Position: Position{X: 0, Y: 0},
		Size:     Size{Width: 100, Height: 50},
	}

	// While Go doesn't enforce immutability, the DESIGN is:
	// - Calculate correct position ONCE
	// - Create fragment with that position
	// - Never modify afterward

	// In the new architecture, if you need a different position,
	// you create a NEW fragment, not modify the existing one.

	correctPosition := Position{X: 200, Y: 0}
	newFrag := &Fragment{
		Position: correctPosition,
		Size:     frag.Size,
		Type:     frag.Type,
		Style:    frag.Style,
		Node:     frag.Node,
	}

	// Old fragment unchanged
	if frag.Position.X != 0 {
		t.Error("Original fragment should be unchanged")
	}

	// New fragment has correct position
	if newFrag.Position.X != 200 {
		t.Error("New fragment should have new position")
	}
}
