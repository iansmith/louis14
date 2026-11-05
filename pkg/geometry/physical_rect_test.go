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

// PhysicalRect::Contains (physical_rect.cc:41-44 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) is inclusive on every edge.
func TestPhysicalRectContains(t *testing.T) {
	r := NewPhysicalRect(0, 0, 10, 10)
	if !r.Contains(r) || !r.Contains(NewPhysicalRect(2, 2, 8, 8)) {
		t.Error("rect should contain itself and an inner rect")
	}
	if r.Contains(NewPhysicalRect(-1, 0, 10, 10)) || r.Contains(NewPhysicalRect(0, 0, 10, 11)) {
		t.Error("rect should not contain a rect crossing an edge")
	}
}

// PhysicalSize::IsEmpty (physical_size.h:65) and FitToAspectRatio
// (physical_size.cc:12-26) @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestPhysicalSizeIsEmptyAndFitToAspectRatio(t *testing.T) {
	if !NewPhysicalSize(0, 5).IsEmpty() || NewPhysicalSize(-1, 5).IsEmpty() {
		t.Error("IsEmpty is true only for a zero dimension")
	}
	box, ratio := NewPhysicalSize(48, 32), NewPhysicalSize(16, 8)
	if got, want := box.FitToAspectRatio(ratio, AspectRatioFitShrink), NewPhysicalSize(48, 24); got != want {
		t.Errorf("shrink = %+v, want %+v", got, want)
	}
	if got, want := box.FitToAspectRatio(ratio, AspectRatioFitGrow), NewPhysicalSize(64, 32); got != want {
		t.Errorf("grow = %+v, want %+v", got, want)
	}
}

// TestPhysicalRect_Unite mirrors PhysicalRect::Unite (core/layout/geometry/
// physical_rect.cc:59-68 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an empty
// operand is ignored, so its origin does not stretch the result.
func TestPhysicalRect_Unite(t *testing.T) {
	a := NewPhysicalRect(10, 10, 20, 20)
	empty := NewPhysicalRect(0, 0, 0, 0)
	if got := a.Unite(empty); got != a {
		t.Errorf("a.Unite(empty) = %v, want %v", got, a)
	}
	if got := empty.Unite(a); got != a {
		t.Errorf("empty.Unite(a) = %v, want %v", got, a)
	}
	if got, want := a.Unite(NewPhysicalRect(40, 0, 10, 5)), NewPhysicalRect(10, 0, 40, 30); got != want {
		t.Errorf("a.Unite(b) = %v, want %v", got, want)
	}
}
