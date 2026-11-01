package geometry

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

// TestPhysicalRectUniteEvenIfEmpty (LOU-469): the union keeps an empty
// operand's position (physical_rect.cc:81-95 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestPhysicalRectUniteEvenIfEmpty(t *testing.T) {
	got := NewPhysicalRect(10, 10, 20, 20).UniteEvenIfEmpty(NewPhysicalRect(50, 0, 0, 0))
	if want := NewPhysicalRect(10, 0, 40, 30); got != want {
		t.Errorf("UniteEvenIfEmpty = %+v, want %+v", got, want)
	}
}

// TestPhysicalRectIntersect (LOU-469): overlapping rects intersect;
// disjoint ones give the zero rect (physical_rect.cc:116-131 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestPhysicalRectIntersect(t *testing.T) {
	if got, want := NewPhysicalRect(0, 0, 50, 50).Intersect(NewPhysicalRect(20, 30, 50, 50)), NewPhysicalRect(20, 30, 30, 20); got != want {
		t.Errorf("Intersect = %+v, want %+v", got, want)
	}
	if got := NewPhysicalRect(0, 0, 10, 10).Intersect(NewPhysicalRect(20, 0, 10, 10)); got != (PhysicalRect{}) {
		t.Errorf("disjoint Intersect = %+v, want zero rect", got)
	}
}

// TestPhysicalRectExpandEdgesAndIsEmpty (LOU-469): physical_rect.h:45,
// :115-123 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestPhysicalRectExpandEdgesAndIsEmpty(t *testing.T) {
	r := NewPhysicalRect(10, 10, 20, 20)
	if got, want := r.ExpandEdges(layoutunit.New(1), layoutunit.New(2), layoutunit.New(3), layoutunit.New(4)), NewPhysicalRect(6, 9, 26, 24); got != want {
		t.Errorf("ExpandEdges = %+v, want %+v", got, want)
	}
	if r.IsEmpty() {
		t.Error("20x20 rect reported empty")
	}
	if !NewPhysicalRect(0, 0, 10, 0).IsEmpty() {
		t.Error("10x0 rect reported non-empty")
	}
}

// TestPhysicalRectEnclosingRect (LOU-469): the origin floors and the far
// edges ceil, both to LayoutUnit precision (physical_rect.h:177-183 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestPhysicalRectEnclosingRect(t *testing.T) {
	quantum := layoutunit.FromRaw(1)
	got := PhysicalRectEnclosingRect(NewRectF(-0.001, 0.001, 10, 10))
	want := PhysicalRect{
		Offset: PhysicalOffset{Left: quantum.MulInt(-1), Top: layoutunit.Zero},
		Size:   PhysicalSize{Width: layoutunit.New(10).Add(quantum), Height: layoutunit.New(10).Add(quantum)},
	}
	if got != want {
		t.Errorf("EnclosingRect = %+v, want %+v", got, want)
	}
}
