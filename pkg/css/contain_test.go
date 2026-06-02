package css

import "testing"

// TestShouldApplyStyleContainment_DisplayContentsExcluded verifies that
// ShouldApplyStyleContainment returns false for display:contents elements.
//
// CSS Containment 1 §3: style containment requires a principal box.
// display:contents elements generate no principal box, so containment
// must not apply. Mirrors Blink LayoutObject::ShouldApplyStyleContainment()
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestShouldApplyStyleContainment_DisplayContentsExcluded(t *testing.T) {
	s := NewStyle()
	s.Set("contain", "style")
	s.Set("display", "contents")

	if s.ShouldApplyStyleContainment() {
		t.Error("ShouldApplyStyleContainment() returned true for display:contents — must return false; containment requires a principal box")
	}
}

// TestShouldApplyStyleContainment_NormalBlockApplies verifies that
// ShouldApplyStyleContainment returns true for a block element.
func TestShouldApplyStyleContainment_NormalBlockApplies(t *testing.T) {
	s := NewStyle()
	s.Set("contain", "style")
	s.Set("display", "block")

	if !s.ShouldApplyStyleContainment() {
		t.Error("ShouldApplyStyleContainment() returned false for display:block — must return true")
	}
}

// TestShouldApplyStyleContainment_NoContain verifies that an element
// without style containment returns false regardless of display type.
func TestShouldApplyStyleContainment_NoContain(t *testing.T) {
	s := NewStyle()
	s.Set("display", "block")
	// no contain property

	if s.ShouldApplyStyleContainment() {
		t.Error("ShouldApplyStyleContainment() returned true for element without contain:style")
	}
}
