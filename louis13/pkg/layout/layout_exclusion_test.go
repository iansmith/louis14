package layout

import (
	"testing"

	"louis14/pkg/css"
)

func TestExclusionSpace_Empty(t *testing.T) {
	es := NewExclusionSpace()

	if !es.IsEmpty() {
		t.Error("Expected new exclusion space to be empty")
	}

	leftOff, rightOff := es.AvailableInlineSize(0, 100)
	if leftOff != 0 || rightOff != 0 {
		t.Errorf("Expected (0, 0) for empty space, got (%f, %f)", leftOff, rightOff)
	}
}

func TestExclusionSpace_SingleLeftFloat(t *testing.T) {
	es := NewExclusionSpace()

	// Add a left float: 100px wide, from Y=0 to Y=50
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es2 := es.Add(excl)

	// Original should be unchanged (immutability)
	if !es.IsEmpty() {
		t.Error("Original ExclusionSpace should be unchanged after Add()")
	}

	// New space should have the float
	if es2.IsEmpty() {
		t.Error("New ExclusionSpace should not be empty")
	}

	// Query at Y=25 (middle of float) - should see 100px left offset
	leftOff, rightOff := es2.AvailableInlineSize(25, 10)
	if leftOff != 100 || rightOff != 0 {
		t.Errorf("Expected (100, 0) for left float, got (%f, %f)", leftOff, rightOff)
	}

	// Query at Y=60 (below float) - should see no offset
	leftOff, rightOff = es2.AvailableInlineSize(60, 10)
	if leftOff != 0 || rightOff != 0 {
		t.Errorf("Expected (0, 0) below float, got (%f, %f)", leftOff, rightOff)
	}
}

func TestExclusionSpace_SingleRightFloat(t *testing.T) {
	es := NewExclusionSpace()

	// Add a right float: 80px wide, from Y=0 to Y=60
	excl := Exclusion{
		Rect: Rect{X: 320, Y: 0, Width: 80, Height: 60}, // X=320 is position from left
		Side: css.FloatRight,
	}
	es2 := es.Add(excl)

	// Query at Y=30 (middle of float) - should see 80px right offset
	leftOff, rightOff := es2.AvailableInlineSize(30, 10)
	if leftOff != 0 || rightOff != 80 {
		t.Errorf("Expected (0, 80) for right float, got (%f, %f)", leftOff, rightOff)
	}

	// Query at Y=70 (below float) - should see no offset
	leftOff, rightOff = es2.AvailableInlineSize(70, 10)
	if leftOff != 0 || rightOff != 0 {
		t.Errorf("Expected (0, 0) below float, got (%f, %f)", leftOff, rightOff)
	}
}

func TestExclusionSpace_LeftAndRightFloats(t *testing.T) {
	es := NewExclusionSpace()

	// Add left float: 100px wide, Y=0-50
	leftExcl := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es2 := es.Add(leftExcl)

	// Add right float: 80px wide, Y=0-60
	rightExcl := Exclusion{
		Rect: Rect{X: 320, Y: 0, Width: 80, Height: 60},
		Side: css.FloatRight,
	}
	es3 := es2.Add(rightExcl)

	// Verify immutability: es and es2 unchanged
	if !es.IsEmpty() {
		t.Error("Original space should be unchanged")
	}
	leftOff, rightOff := es2.AvailableInlineSize(25, 10)
	if leftOff != 100 || rightOff != 0 {
		t.Error("Intermediate space should only have left float")
	}

	// Query at Y=30 (both floats active) - should see both offsets
	leftOff, rightOff = es3.AvailableInlineSize(30, 10)
	if leftOff != 100 || rightOff != 80 {
		t.Errorf("Expected (100, 80) for both floats, got (%f, %f)", leftOff, rightOff)
	}

	// Query at Y=55 (only right float active) - should see only right offset
	leftOff, rightOff = es3.AvailableInlineSize(55, 5)
	if leftOff != 0 || rightOff != 80 {
		t.Errorf("Expected (0, 80) for only right float, got (%f, %f)", leftOff, rightOff)
	}

	// Query at Y=65 (no floats active) - should see no offset
	leftOff, rightOff = es3.AvailableInlineSize(65, 10)
	if leftOff != 0 || rightOff != 0 {
		t.Errorf("Expected (0, 0) below all floats, got (%f, %f)", leftOff, rightOff)
	}
}

func TestExclusionSpace_StackedLeftFloats(t *testing.T) {
	es := NewExclusionSpace()

	// Add first left float: 100px wide, Y=0-50
	excl1 := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es2 := es.Add(excl1)

	// Add second left float: 80px wide, Y=20-70 (overlaps first)
	// This float is positioned AFTER the first one horizontally
	excl2 := Exclusion{
		Rect: Rect{X: 100, Y: 20, Width: 80, Height: 50},
		Side: css.FloatLeft,
	}
	es3 := es2.Add(excl2)

	// Query at Y=30 (both floats overlap vertically)
	// Should see the rightmost edge: X=100+80=180
	leftOff, rightOff := es3.AvailableInlineSize(30, 10)
	if leftOff != 180 || rightOff != 0 {
		t.Errorf("Expected (180, 0) for stacked left floats, got (%f, %f)", leftOff, rightOff)
	}

	// Query at Y=10 (only first float)
	leftOff, rightOff = es3.AvailableInlineSize(10, 5)
	if leftOff != 100 || rightOff != 0 {
		t.Errorf("Expected (100, 0) for only first float, got (%f, %f)", leftOff, rightOff)
	}

	// Query at Y=60 (only second float)
	leftOff, rightOff = es3.AvailableInlineSize(60, 5)
	if leftOff != 180 || rightOff != 0 {
		t.Errorf("Expected (180, 0) for only second float, got (%f, %f)", leftOff, rightOff)
	}
}

func TestExclusionSpace_VerticalOverlapDetection(t *testing.T) {
	es := NewExclusionSpace()

	// Add float at Y=100-150
	excl := Exclusion{
		Rect: Rect{X: 0, Y: 100, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es2 := es.Add(excl)

	// Test various query ranges for vertical overlap
	tests := []struct {
		name           string
		y              float64
		height         float64
		expectedLeft   float64
		expectedRight  float64
	}{
		{"Before float", 0, 50, 0, 0},           // Y=0-50, no overlap
		{"Touches top", 50, 50, 0, 0},           // Y=50-100, touches but doesn't overlap
		{"Overlaps top", 90, 20, 100, 0},        // Y=90-110, overlaps
		{"Inside float", 110, 20, 100, 0},       // Y=110-130, inside
		{"Overlaps bottom", 140, 20, 100, 0},    // Y=140-160, overlaps
		{"Touches bottom", 150, 50, 0, 0},       // Y=150-200, touches but doesn't overlap
		{"After float", 200, 50, 0, 0},          // Y=200-250, no overlap
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leftOff, rightOff := es2.AvailableInlineSize(tt.y, tt.height)
			if leftOff != tt.expectedLeft || rightOff != tt.expectedRight {
				t.Errorf("Expected (%f, %f), got (%f, %f)",
					tt.expectedLeft, tt.expectedRight, leftOff, rightOff)
			}
		})
	}
}

func TestExclusionSpace_Immutability(t *testing.T) {
	// This test verifies that Add() truly creates a new space
	es1 := NewExclusionSpace()

	excl1 := Exclusion{
		Rect: Rect{X: 0, Y: 0, Width: 100, Height: 50},
		Side: css.FloatLeft,
	}
	es2 := es1.Add(excl1)

	excl2 := Exclusion{
		Rect: Rect{X: 100, Y: 0, Width: 80, Height: 50},
		Side: css.FloatLeft,
	}
	es3 := es2.Add(excl2)

	// Verify each space has correct number of exclusions
	if !es1.IsEmpty() {
		t.Error("es1 should be empty")
	}

	leftOff, _ := es2.AvailableInlineSize(25, 10)
	if leftOff != 100 {
		t.Errorf("es2 should have 1 float (offset=100), got offset=%f", leftOff)
	}

	leftOff, _ = es3.AvailableInlineSize(25, 10)
	if leftOff != 180 {
		t.Errorf("es3 should have 2 floats (offset=180), got offset=%f", leftOff)
	}

	// Verify es2 is still unchanged after creating es3
	leftOff, _ = es2.AvailableInlineSize(25, 10)
	if leftOff != 100 {
		t.Errorf("es2 should still have offset=100 after creating es3, got offset=%f", leftOff)
	}
}
