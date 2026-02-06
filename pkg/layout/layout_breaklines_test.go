package layout

import (
	"testing"

	"louis14/pkg/css"
)

func TestBreakLines_EmptyItems(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	lines := le.BreakLines([]*InlineItem{}, constraint, 0)

	if len(lines) != 0 {
		t.Errorf("Expected 0 lines for empty items, got %d", len(lines))
	}
}

func TestBreakLines_SingleTextItem_FitsOnLine(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// Single text item that fits on one line
	item := &InlineItem{
		Type:   InlineItemText,
		Text:   "Hello",
		Width:  50,
		Height: 16,
	}

	lines := le.BreakLines([]*InlineItem{item}, constraint, 0)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	line := lines[0]
	if line.Y != 0 {
		t.Errorf("Expected Y=0, got Y=%f", line.Y)
	}

	if len(line.Items) != 1 {
		t.Errorf("Expected 1 item on line, got %d", len(line.Items))
	}

	if line.Items[0] != item {
		t.Error("Item on line should be the original item")
	}

	if line.Height != 16 {
		t.Errorf("Expected line height 16, got %f", line.Height)
	}
}

func TestBreakLines_MultipleTextItems_FitOnOneLine(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// Three text items that fit on one line (total 150px in 400px)
	items := []*InlineItem{
		{Type: InlineItemText, Text: "Hello", Width: 50, Height: 16},
		{Type: InlineItemText, Text: " ", Width: 10, Height: 16},
		{Type: InlineItemText, Text: "World", Width: 90, Height: 16},
	}

	lines := le.BreakLines(items, constraint, 0)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	if len(lines[0].Items) != 3 {
		t.Errorf("Expected 3 items on line, got %d", len(lines[0].Items))
	}
}

func TestBreakLines_MultipleTextItems_WrapToTwoLines(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(100, 300) // Narrow width

	// Three text items, last one wraps to second line
	items := []*InlineItem{
		{Type: InlineItemText, Text: "Hello", Width: 50, Height: 16},
		{Type: InlineItemText, Text: " ", Width: 10, Height: 16},
		{Type: InlineItemText, Text: "World", Width: 90, Height: 16}, // Won't fit
	}

	lines := le.BreakLines(items, constraint, 0)

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	// First line: "Hello "
	if len(lines[0].Items) != 2 {
		t.Errorf("Expected 2 items on first line, got %d", len(lines[0].Items))
	}
	if lines[0].Y != 0 {
		t.Errorf("Expected first line at Y=0, got Y=%f", lines[0].Y)
	}

	// Second line: "World"
	if len(lines[1].Items) != 1 {
		t.Errorf("Expected 1 item on second line, got %d", len(lines[1].Items))
	}
	if lines[1].Y != 16 {
		t.Errorf("Expected second line at Y=16, got Y=%f", lines[1].Y)
	}
	if lines[1].Items[0].Text != "World" {
		t.Errorf("Expected 'World' on second line, got '%s'", lines[1].Items[0].Text)
	}
}

func TestBreakLines_WithLeftFloat(t *testing.T) {
	le := &LayoutEngine{}

	// Start with empty exclusion space
	es := NewExclusionSpace()

	// Add a left float: 100px wide, at Y=0-50
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es = es.Add(excl)

	// Create constraint with float
	constraint := &ConstraintSpace{
		AvailableSize:  Size{Width: 400, Height: 300},
		ExclusionSpace: es,
	}

	// Text items
	items := []*InlineItem{
		{Type: InlineItemText, Text: "After", Width: 50, Height: 16},
		{Type: InlineItemText, Text: "Float", Width: 50, Height: 16},
	}

	lines := le.BreakLines(items, constraint, 10) // Y=10, within float range

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	// Both items should fit on one line (100px total in 300px available after float)
	if len(lines[0].Items) != 2 {
		t.Errorf("Expected 2 items on line, got %d", len(lines[0].Items))
	}

	// Line should account for float (available width = 400 - 100 = 300)
	availableWidth := constraint.AvailableInlineSize(lines[0].Y, lines[0].Height)
	if availableWidth != 300 {
		t.Errorf("Expected available width 300, got %f", availableWidth)
	}
}

func TestBreakLines_WithLeftFloat_CausesWrap(t *testing.T) {
	le := &LayoutEngine{}

	// Add a left float: 250px wide
	es := NewExclusionSpace()
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 250, Height: 50},
		Side: css.FloatLeft,
	}
	es = es.Add(excl)

	constraint := &ConstraintSpace{
		AvailableSize:  Size{Width: 400, Height: 300},
		ExclusionSpace: es,
	}

	// Two text items, total 200px
	// Available width = 400 - 250 = 150px, so second item wraps
	items := []*InlineItem{
		{Type: InlineItemText, Text: "First", Width: 100, Height: 16},
		{Type: InlineItemText, Text: "Second", Width: 100, Height: 16},
	}

	lines := le.BreakLines(items, constraint, 10)

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines (float causes wrap), got %d", len(lines))
	}

	// First line has first item
	if len(lines[0].Items) != 1 {
		t.Errorf("Expected 1 item on first line, got %d", len(lines[0].Items))
	}

	// Second line has second item
	if len(lines[1].Items) != 1 {
		t.Errorf("Expected 1 item on second line, got %d", len(lines[1].Items))
	}
}

func TestBreakLines_AtomicItem(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(200, 300)

	// Text + atomic item that fits
	items := []*InlineItem{
		{Type: InlineItemText, Text: "Before", Width: 50, Height: 16},
		{Type: InlineItemAtomic, Width: 100, Height: 20}, // Inline-block
	}

	lines := le.BreakLines(items, constraint, 0)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	if len(lines[0].Items) != 2 {
		t.Errorf("Expected 2 items on line, got %d", len(lines[0].Items))
	}

	// Line height should be max of item heights (20 > 16)
	if lines[0].Height != 20 {
		t.Errorf("Expected line height 20, got %f", lines[0].Height)
	}
}

func TestBreakLines_AtomicItem_WrapsToNewLine(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(100, 300) // Narrow width

	// Text + atomic item that doesn't fit
	items := []*InlineItem{
		{Type: InlineItemText, Text: "Before", Width: 60, Height: 16},
		{Type: InlineItemAtomic, Width: 80, Height: 20}, // Won't fit
	}

	lines := le.BreakLines(items, constraint, 0)

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	// First line: text
	if len(lines[0].Items) != 1 || lines[0].Items[0].Type != InlineItemText {
		t.Error("First line should have text item")
	}

	// Second line: atomic item
	if len(lines[1].Items) != 1 || lines[1].Items[0].Type != InlineItemAtomic {
		t.Error("Second line should have atomic item")
	}
}

func TestBreakLines_FloatItem(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// Float item + text
	items := []*InlineItem{
		{Type: InlineItemFloat, Width: 100, Height: 50},
		{Type: InlineItemText, Text: "After", Width: 50, Height: 16},
	}

	lines := le.BreakLines(items, constraint, 0)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	// Both float and text should be on same line
	if len(lines[0].Items) != 2 {
		t.Errorf("Expected 2 items on line, got %d", len(lines[0].Items))
	}

	// Line height should be max (50 > 16)
	if lines[0].Height != 50 {
		t.Errorf("Expected line height 50, got %f", lines[0].Height)
	}
}

func TestBreakLines_ControlItem_ForcesLineBreak(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// Text + <br> + text
	items := []*InlineItem{
		{Type: InlineItemText, Text: "Before", Width: 50, Height: 16},
		{Type: InlineItemControl, Width: 0, Height: 0}, // <br>
		{Type: InlineItemText, Text: "After", Width: 50, Height: 16},
	}

	lines := le.BreakLines(items, constraint, 0)

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines (<br> forces break), got %d", len(lines))
	}

	// First line: text + control
	if len(lines[0].Items) != 2 {
		t.Errorf("Expected 2 items on first line, got %d", len(lines[0].Items))
	}

	// Second line: text
	if len(lines[1].Items) != 1 {
		t.Errorf("Expected 1 item on second line, got %d", len(lines[1].Items))
	}
}

func TestBreakLines_TagItems(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	// <span>Text</span>
	items := []*InlineItem{
		{Type: InlineItemOpenTag},
		{Type: InlineItemText, Text: "Text", Width: 50, Height: 16},
		{Type: InlineItemCloseTag},
	}

	lines := le.BreakLines(items, constraint, 0)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	// All three items on one line
	if len(lines[0].Items) != 3 {
		t.Errorf("Expected 3 items on line, got %d", len(lines[0].Items))
	}
}

func TestBreakLines_ComplexMix(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(200, 300)

	// Mix of different item types
	items := []*InlineItem{
		{Type: InlineItemText, Text: "Hello", Width: 50, Height: 16},
		{Type: InlineItemAtomic, Width: 80, Height: 20},
		{Type: InlineItemText, Text: "World", Width: 100, Height: 16}, // Wraps
		{Type: InlineItemFloat, Width: 50, Height: 30},
	}

	lines := le.BreakLines(items, constraint, 0)

	// First line: "Hello" + atomic (130px fits in 200px)
	// Second line: "World" + float
	if len(lines) < 2 {
		t.Fatalf("Expected at least 2 lines, got %d", len(lines))
	}

	// Verify first line has text + atomic
	firstLineHasText := false
	firstLineHasAtomic := false
	for _, item := range lines[0].Items {
		if item.Type == InlineItemText {
			firstLineHasText = true
		}
		if item.Type == InlineItemAtomic {
			firstLineHasAtomic = true
		}
	}

	if !firstLineHasText || !firstLineHasAtomic {
		t.Error("First line should have both text and atomic items")
	}
}

func TestBreakLines_StartYOffset(t *testing.T) {
	le := &LayoutEngine{}
	constraint := NewConstraintSpace(400, 300)

	item := &InlineItem{
		Type:   InlineItemText,
		Text:   "Test",
		Width:  50,
		Height: 16,
	}

	// Start at Y=100
	lines := le.BreakLines([]*InlineItem{item}, constraint, 100)

	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	if lines[0].Y != 100 {
		t.Errorf("Expected line at Y=100, got Y=%f", lines[0].Y)
	}
}

func TestBreakLines_NoSideEffects(t *testing.T) {
	// Verify BreakLines is pure - doesn't modify input or engine state
	le := &LayoutEngine{
		floats: []FloatInfo{}, // Should remain empty
	}
	constraint := NewConstraintSpace(400, 300)

	items := []*InlineItem{
		{Type: InlineItemText, Text: "Test", Width: 50, Height: 16},
		{Type: InlineItemFloat, Width: 100, Height: 50},
	}

	// Store original state
	originalFloatCount := len(le.floats)
	originalItemCount := len(items)

	// Call BreakLines
	le.BreakLines(items, constraint, 0)

	// Verify no side effects
	if len(le.floats) != originalFloatCount {
		t.Errorf("BreakLines modified le.floats! Original: %d, After: %d",
			originalFloatCount, len(le.floats))
	}

	if len(items) != originalItemCount {
		t.Errorf("BreakLines modified items array! Original: %d, After: %d",
			originalItemCount, len(items))
	}
}
