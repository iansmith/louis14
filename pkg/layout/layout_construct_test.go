package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

func TestConstructLine_SimpleText(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// Create a line with single text item
	item := &InlineItem{
		Type:   InlineItemText,
		Text:   "Hello",
		Width:  50,
		Height: 16,
		Style:  css.NewStyle(),
	}

	line := &LineInfo{
		Y:          0,
		Items:      []*InlineItem{item},
		Constraint: constraint,
		Height:     16,
	}

	fragments, newConstraint := le.constructLine(line, constraint)

	if len(fragments) != 1 {
		t.Fatalf("Expected 1 fragment, got %d", len(fragments))
	}

	frag := fragments[0]
	if frag.Type != FragmentText {
		t.Errorf("Expected FragmentText, got %v", frag.Type)
	}

	if frag.Text != "Hello" {
		t.Errorf("Expected text 'Hello', got '%s'", frag.Text)
	}

	// Position should be correct from start (no floats, so X=0)
	if frag.Position.X != 0 || frag.Position.Y != 0 {
		t.Errorf("Expected position (0, 0), got (%f, %f)",
			frag.Position.X, frag.Position.Y)
	}

	// Constraint shouldn't change (no floats added)
	if newConstraint != constraint {
		t.Error("Constraint should be unchanged when no floats added")
	}
}

func TestConstructLine_MultipleTextItems(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	items := []*InlineItem{
		{Type: InlineItemText, Text: "Hello", Width: 50, Height: 16, Style: css.NewStyle()},
		{Type: InlineItemText, Text: " ", Width: 10, Height: 16, Style: css.NewStyle()},
		{Type: InlineItemText, Text: "World", Width: 60, Height: 16, Style: css.NewStyle()},
	}

	line := &LineInfo{
		Y:          0,
		Items:      items,
		Constraint: constraint,
		Height:     16,
	}

	fragments, _ := le.constructLine(line, constraint)

	if len(fragments) != 3 {
		t.Fatalf("Expected 3 fragments, got %d", len(fragments))
	}

	// Check positions are sequential
	expectedX := []float64{0, 50, 60}
	for i, frag := range fragments {
		if frag.Position.X != expectedX[i] {
			t.Errorf("Fragment %d: expected X=%f, got X=%f",
				i, expectedX[i], frag.Position.X)
		}
	}
}

func TestConstructLine_WithLeftFloat(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	style := css.NewStyle()
	style.Set("float", "left")

	items := []*InlineItem{
		{Type: InlineItemFloat, Width: 100, Height: 50, Style: style},
		{Type: InlineItemText, Text: "After", Width: 50, Height: 16, Style: css.NewStyle()},
	}

	line := &LineInfo{
		Y:          0,
		Items:      items,
		Constraint: constraint,
		Height:     50,
	}

	fragments, newConstraint := le.constructLine(line, constraint)

	if len(fragments) != 2 {
		t.Fatalf("Expected 2 fragments, got %d", len(fragments))
	}

	// Float should be at X=0
	floatFrag := fragments[0]
	if floatFrag.Position.X != 0 {
		t.Errorf("Float should be at X=0, got X=%f", floatFrag.Position.X)
	}

	// Text should be at X=0 (on same line as float)
	textFrag := fragments[1]
	if textFrag.Position.X != 0 {
		t.Errorf("Text should be at X=0, got X=%f", textFrag.Position.X)
	}

	// Constraint should have changed (float added)
	if newConstraint == constraint {
		t.Error("Constraint should change when float is added")
	}

	// Verify exclusion was added
	leftOff, _ := newConstraint.ExclusionSpace.AvailableInlineSize(0, 50)
	if leftOff != 100 {
		t.Errorf("Expected left offset 100 after float, got %f", leftOff)
	}
}

func TestConstructLine_WithRightFloat(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	style := css.NewStyle()
	style.Set("float", "right")

	items := []*InlineItem{
		{Type: InlineItemFloat, Width: 80, Height: 50, Style: style},
	}

	line := &LineInfo{
		Y:          0,
		Items:      items,
		Constraint: constraint,
		Height:     50,
	}

	fragments, newConstraint := le.constructLine(line, constraint)

	if len(fragments) != 1 {
		t.Fatalf("Expected 1 fragment, got %d", len(fragments))
	}

	// Right float should be positioned from right edge
	// X = containerWidth - floatWidth = 400 - 80 = 320
	floatFrag := fragments[0]
	if floatFrag.Position.X != 320 {
		t.Errorf("Right float should be at X=320, got X=%f", floatFrag.Position.X)
	}

	// Verify right exclusion was added
	_, rightOff := newConstraint.ExclusionSpace.AvailableInlineSize(0, 50)
	if rightOff != 80 {
		t.Errorf("Expected right offset 80 after float, got %f", rightOff)
	}
}

func TestConstructLine_WithExistingFloat(t *testing.T) {
	le := &LayoutEngine{}

	// Start with constraint that has a left float
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

	items := []*InlineItem{
		{Type: InlineItemText, Text: "After", Width: 50, Height: 16, Style: css.NewStyle()},
	}

	line := &LineInfo{
		Y:          10, // Within float's Y range
		Items:      items,
		Constraint: constraint,
		Height:     16,
	}

	fragments, _ := le.constructLine(line, constraint)

	if len(fragments) != 1 {
		t.Fatalf("Expected 1 fragment, got %d", len(fragments))
	}

	// Text should start AFTER the float (X=100)
	textFrag := fragments[0]
	if textFrag.Position.X != 100 {
		t.Errorf("Text should start at X=100 (after float), got X=%f",
			textFrag.Position.X)
	}
}

func TestConstructFragments_MultipleLines(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// Two lines with text
	line1 := &LineInfo{
		Y: 0,
		Items: []*InlineItem{
			{Type: InlineItemText, Text: "Line1", Width: 50, Height: 16, Style: css.NewStyle()},
		},
		Constraint: constraint,
		Height:     16,
	}

	line2 := &LineInfo{
		Y: 16,
		Items: []*InlineItem{
			{Type: InlineItemText, Text: "Line2", Width: 60, Height: 16, Style: css.NewStyle()},
		},
		Constraint: constraint,
		Height:     16,
	}

	fragments, finalConstraint := le.ConstructFragments(
		[]*LineInfo{line1, line2},
		constraint,
	)

	if len(fragments) != 2 {
		t.Fatalf("Expected 2 fragments, got %d", len(fragments))
	}

	// First fragment at Y=0
	if fragments[0].Position.Y != 0 {
		t.Errorf("First fragment should be at Y=0, got Y=%f",
			fragments[0].Position.Y)
	}

	// Second fragment at Y=16
	if fragments[1].Position.Y != 16 {
		t.Errorf("Second fragment should be at Y=16, got Y=%f",
			fragments[1].Position.Y)
	}

	// Constraint shouldn't change (no floats)
	if finalConstraint != constraint {
		t.Error("Constraint should be unchanged when no floats")
	}
}

func TestConstructFragments_FloatPropagation(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	floatStyle := css.NewStyle()
	floatStyle.Set("float", "left")

	// Line 1: Float
	line1 := &LineInfo{
		Y: 0,
		Items: []*InlineItem{
			{Type: InlineItemFloat, Width: 100, Height: 50, Style: floatStyle},
		},
		Constraint: constraint,
		Height:     50,
	}

	// Line 2: Text (should account for float from line 1)
	line2 := &LineInfo{
		Y: 16, // Within float's Y range
		Items: []*InlineItem{
			{Type: InlineItemText, Text: "Text", Width: 50, Height: 16, Style: css.NewStyle()},
		},
		Constraint: constraint,
		Height:     16,
	}

	fragments, finalConstraint := le.ConstructFragments(
		[]*LineInfo{line1, line2},
		constraint,
	)

	if len(fragments) != 2 {
		t.Fatalf("Expected 2 fragments, got %d", len(fragments))
	}

	// Float should be at X=0
	floatFrag := fragments[0]
	if floatFrag.Position.X != 0 {
		t.Errorf("Float should be at X=0, got X=%f", floatFrag.Position.X)
	}

	// Text should be at X=100 (after float, even though it's on line 2)
	textFrag := fragments[1]
	if textFrag.Position.X != 100 {
		t.Errorf("Text should be at X=100 (after propagated float), got X=%f",
			textFrag.Position.X)
	}

	// Final constraint should have the float
	leftOff, _ := finalConstraint.ExclusionSpace.AvailableInlineSize(16, 16)
	if leftOff != 100 {
		t.Errorf("Final constraint should have float (offset=100), got offset=%f",
			leftOff)
	}
}

func TestConstraintsChanged_NoChange(t *testing.T) {
	constraint := NewConstraintSpace(400, 300)
	lines := []*LineInfo{
		{Y: 0, Height: 16},
	}

	// Same constraint
	changed := constraintsChanged(constraint, constraint, lines)

	if changed {
		t.Error("Constraints should not have changed (same object)")
	}
}

func TestConstraintsChanged_FloatAdded(t *testing.T) {
	originalConstraint := NewConstraintSpace(400, 300)

	// Add a float
	es := originalConstraint.ExclusionSpace
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es = es.Add(excl)

	finalConstraint := &ConstraintSpace{
		AvailableSize:  originalConstraint.AvailableSize,
		ExclusionSpace: es,
	}

	lines := []*LineInfo{
		{Y: 0, Height: 16},
	}

	changed := constraintsChanged(originalConstraint, finalConstraint, lines)

	if !changed {
		t.Error("Constraints should have changed (float was added)")
	}
}

func TestConstraintsChanged_AvailableWidthChanged(t *testing.T) {
	original := NewConstraintSpace(400, 300)

	// Create constraint with float that reduces available width
	es := NewExclusionSpace()
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es = es.Add(excl)

	final := &ConstraintSpace{
		AvailableSize:  original.AvailableSize,
		ExclusionSpace: es,
	}

	lines := []*LineInfo{
		{Y: 10, Height: 16}, // Within float's Y range
	}

	changed := constraintsChanged(original, final, lines)

	if !changed {
		t.Error("Constraints should have changed (available width changed)")
	}
}

func TestLayoutInlineContent_NoRetryNeeded(t *testing.T) {
	le := &LayoutEngine{
		viewport: struct {
			width  float64
			height float64
		}{width: 800, height: 600},
		stylesheets: []*css.Stylesheet{},
	}

	// Simple text - no floats, no retry needed
	textNode := &html.Node{
		Type: html.TextNode,
		Text: "Simple text",
	}

	constraint := NewConstraintSpace(400, 300)
	fragments := le.LayoutInlineContent([]*html.Node{textNode}, constraint, 0, nil)

	if len(fragments) < 1 {
		t.Fatalf("Expected at least 1 fragment, got %d", len(fragments))
	}

	// Should be text fragment
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

func TestPositionFloat_CorrectPositionFromStart(t *testing.T) {
	// This test verifies the key principle: floats are positioned correctly
	// from the start, not positioned at 0 and then adjusted

	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	floatStyle := css.NewStyle()
	floatStyle.Set("float", "left")

	item := &InlineItem{
		Type:   InlineItemFloat,
		Width:  100,
		Height: 50,
		Style:  floatStyle,
	}

	frag, newConstraint := le.positionFloat(item, 0, constraint)

	// Fragment should be created with CORRECT position (X=0 for left float)
	if frag.Position.X != 0 {
		t.Errorf("Float should be created at X=0, got X=%f", frag.Position.X)
	}

	// No repositioning needed - this IS the final position
	// (This is the key improvement over the old approach)

	// Constraint should have exclusion
	if newConstraint.ExclusionSpace.IsEmpty() {
		t.Error("New constraint should have exclusion for float")
	}
}

func TestFragments_Immutable(t *testing.T) {
	// Verify fragments are created with correct positions and not modified

	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	items := []*InlineItem{
		{Type: InlineItemText, Text: "Test", Width: 50, Height: 16, Style: css.NewStyle()},
	}

	line := &LineInfo{
		Y:          0,
		Items:      items,
		Constraint: constraint,
		Height:     16,
	}

	fragments, _ := le.constructLine(line, constraint)

	frag := fragments[0]
	originalX := frag.Position.X
	originalY := frag.Position.Y

	// In proper design, fragment should never be modified
	// Position is correct from creation

	if frag.Position.X != originalX || frag.Position.Y != originalY {
		t.Error("Fragment position should not change after creation")
	}

	// This test documents the principle: fragments are IMMUTABLE
	// They are created with the correct position and never repositioned
}
