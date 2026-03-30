package layout

import (
	"testing"

	"louis14/pkg/css"
)

func TestConstraintSpace_New(t *testing.T) {
	cs := NewConstraintSpace(800, 600)

	if cs.AvailableSize.Width != 800 || cs.AvailableSize.Height != 600 {
		t.Errorf("Expected size (800, 600), got (%f, %f)",
			cs.AvailableSize.Width, cs.AvailableSize.Height)
	}

	if cs.ExclusionSpace == nil || !cs.ExclusionSpace.IsEmpty() {
		t.Error("Expected empty exclusion space")
	}

	if cs.TextAlign != css.TextAlignLeft {
		t.Errorf("Expected default TextAlign to be Left, got %v", cs.TextAlign)
	}
}

func TestConstraintSpace_WithExclusion(t *testing.T) {
	cs1 := NewConstraintSpace(400, 300)

	// Add a left float
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	cs2 := cs1.WithExclusion(excl)

	// Verify immutability: cs1 unchanged
	if !cs1.ExclusionSpace.IsEmpty() {
		t.Error("Original constraint space should have empty exclusion space")
	}

	// Verify cs2 has the exclusion
	leftOff, rightOff := cs2.ExclusionSpace.AvailableInlineSize(25, 10)
	if leftOff != 100 || rightOff != 0 {
		t.Errorf("Expected (100, 0) for float offset, got (%f, %f)", leftOff, rightOff)
	}

	// Verify other fields unchanged
	if cs2.AvailableSize.Width != 400 || cs2.AvailableSize.Height != 300 {
		t.Error("AvailableSize should be unchanged")
	}
	if cs2.TextAlign != cs1.TextAlign {
		t.Error("TextAlign should be unchanged")
	}
}

func TestConstraintSpace_WithAvailableWidth(t *testing.T) {
	cs1 := NewConstraintSpace(400, 300)
	cs2 := cs1.WithAvailableWidth(500)

	// Verify immutability: cs1 unchanged
	if cs1.AvailableSize.Width != 400 {
		t.Errorf("Original width should be 400, got %f", cs1.AvailableSize.Width)
	}

	// Verify cs2 has new width
	if cs2.AvailableSize.Width != 500 {
		t.Errorf("New width should be 500, got %f", cs2.AvailableSize.Width)
	}

	// Verify height unchanged
	if cs2.AvailableSize.Height != 300 {
		t.Errorf("Height should be unchanged (300), got %f", cs2.AvailableSize.Height)
	}
}

func TestConstraintSpace_WithTextAlign(t *testing.T) {
	cs1 := NewConstraintSpace(400, 300)
	cs2 := cs1.WithTextAlign(css.TextAlignCenter)

	// Verify immutability: cs1 unchanged
	if cs1.TextAlign != css.TextAlignLeft {
		t.Errorf("Original TextAlign should be Left, got %v", cs1.TextAlign)
	}

	// Verify cs2 has new alignment
	if cs2.TextAlign != css.TextAlignCenter {
		t.Errorf("New TextAlign should be Center, got %v", cs2.TextAlign)
	}
}

func TestConstraintSpace_AvailableInlineSize(t *testing.T) {
	cs := NewConstraintSpace(400, 300)

	// Initially, full width available
	avail := cs.AvailableInlineSize(0, 100)
	if avail != 400 {
		t.Errorf("Expected 400px available initially, got %f", avail)
	}

	// Add left float: 100px wide
	leftExcl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	cs2 := cs.WithExclusion(leftExcl)

	// At Y=25, should have 400 - 100 = 300px available
	avail = cs2.AvailableInlineSize(25, 10)
	if avail != 300 {
		t.Errorf("Expected 300px available with left float, got %f", avail)
	}

	// Add right float: 80px wide
	rightExcl := Exclusion{
		Rect: Rect{X: 320, Y: 0, Width: 80, Height: 60},
		Side: css.FloatRight,
	}
	cs3 := cs2.WithExclusion(rightExcl)

	// At Y=30, should have 400 - 100 - 80 = 220px available
	avail = cs3.AvailableInlineSize(30, 10)
	if avail != 220 {
		t.Errorf("Expected 220px available with both floats, got %f", avail)
	}

	// Below both floats (Y=70), should have full 400px
	avail = cs3.AvailableInlineSize(70, 10)
	if avail != 400 {
		t.Errorf("Expected 400px available below floats, got %f", avail)
	}
}

func TestConstraintSpace_ChainedModifications(t *testing.T) {
	// Test chaining multiple modifications
	cs1 := NewConstraintSpace(400, 300)

	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}

	// Chain: add exclusion, change width, change alignment
	cs2 := cs1.WithExclusion(excl).
		WithAvailableWidth(500).
		WithTextAlign(css.TextAlignRight)

	// Verify cs1 unchanged
	if !cs1.ExclusionSpace.IsEmpty() {
		t.Error("Original should have no exclusions")
	}
	if cs1.AvailableSize.Width != 400 {
		t.Error("Original width should be 400")
	}
	if cs1.TextAlign != css.TextAlignLeft {
		t.Error("Original alignment should be Left")
	}

	// Verify cs2 has all modifications
	leftOff, _ := cs2.ExclusionSpace.AvailableInlineSize(25, 10)
	if leftOff != 100 {
		t.Error("cs2 should have exclusion")
	}
	if cs2.AvailableSize.Width != 500 {
		t.Error("cs2 width should be 500")
	}
	if cs2.TextAlign != css.TextAlignRight {
		t.Error("cs2 alignment should be Right")
	}
}

func TestConstraintSpace_MultipleExclusions(t *testing.T) {
	// Test that multiple exclusions can be added and each creates new space
	cs1 := NewConstraintSpace(400, 300)

	excl1 := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	cs2 := cs1.WithExclusion(excl1)

	excl2 := Exclusion{
		Rect: Rect{X: 100, Y: 20, Width: 80, Height: 40},
		Side: css.FloatLeft,
	}
	cs3 := cs2.WithExclusion(excl2)

	// Verify immutability chain
	if !cs1.ExclusionSpace.IsEmpty() {
		t.Error("cs1 should have no exclusions")
	}

	leftOff, _ := cs2.ExclusionSpace.AvailableInlineSize(25, 10)
	if leftOff != 100 {
		t.Errorf("cs2 should have 1 exclusion (offset=100), got %f", leftOff)
	}

	leftOff, _ = cs3.ExclusionSpace.AvailableInlineSize(25, 10)
	if leftOff != 180 {
		t.Errorf("cs3 should have 2 exclusions (offset=180), got %f", leftOff)
	}

	// Verify cs2 still unchanged after creating cs3
	leftOff, _ = cs2.ExclusionSpace.AvailableInlineSize(25, 10)
	if leftOff != 100 {
		t.Errorf("cs2 should still have offset=100, got %f", leftOff)
	}
}
