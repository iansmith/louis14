package css

import "strings"

// CSS Containment 1 (https://drafts.csswg.org/css-contain-1/) eligibility
// helpers — extend the bit-test functions `HasLayoutContainment`,
// `HasPaintContainment`, `HasSizeContainment`, `HasInlineSizeContainment`
// (declared in style.go) with the per-display-type applicability filter.
//
// Mirrors Blink's split between `ComputedStyle::Contains{Layout,Paint,Size,
// InlineSize}()` (the raw bit on the computed style) and the `LayoutObject::
// ShouldApply{Layout,Paint,Size}Containment()` family (bit AND eligibility
// per the box's principal display type), at
// `third_party/blink/renderer/core/layout/layout_object.h` and `.cc` SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Reused at the layout / paint call sites where containment is *applied*;
// the `Has*Containment` functions remain the bit accessors (used by
// `HasAnyContainment`, C45 propagation, @supports feature detection).

// ShouldApplyLayoutContainment reports whether layout containment is in
// effect for this element — `HasLayoutContainment` AND eligibility per
// CSS Containment 1 §4.1. Mirrors Blink LayoutObject::
// ShouldApplyLayoutContainment().
//
// Layout containment does not apply to:
//   - non-atomic inline-level boxes (`display: inline` without atomic inner)
//   - internal ruby boxes: `ruby-base`, `ruby-base-container`,
//     `ruby-text-container`, `ruby-text`
//   - internal table boxes EXCEPT table-cell: `table-row`, `table-row-group`,
//     `table-header-group`, `table-footer-group`, `table-column`,
//     `table-column-group`. `table-cell` and `table-caption` ARE eligible.
func (s *Style) ShouldApplyLayoutContainment() bool {
	if !s.HasLayoutContainment() {
		return false
	}
	return isEligibleForLayoutOrPaintContainment(s)
}

// ShouldApplyPaintContainment reports whether paint containment is in
// effect for this element. Mirrors Blink LayoutObject::
// ShouldApplyPaintContainment(); eligibility rules are identical to
// layout containment (CSS Containment 1 §3.1 + §5.1).
func (s *Style) ShouldApplyPaintContainment() bool {
	if !s.HasPaintContainment() {
		return false
	}
	return isEligibleForLayoutOrPaintContainment(s)
}

// ShouldApplySizeContainment reports whether full (both-axis) size
// containment is in effect — `HasSizeContainment` AND eligibility per
// CSS Containment 1 §6.1. Mirrors Blink LayoutObject::
// ShouldApplySizeContainment().
//
// Size containment is strictly more restrictive than layout/paint:
// in addition to the layout/paint exclusions above, `table-cell` is
// also NOT eligible. Confirmed by WPT contain-size-006 (table-cell)
// expecting normal layout, and by Blink's IsEligibleForSizeContainment
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func (s *Style) ShouldApplySizeContainment() bool {
	if !s.HasSizeContainment() {
		return false
	}
	return isEligibleForSizeContainment(s)
}

// ShouldApplyInlineSizeContainment reports whether inline-axis size
// containment is in effect. Same eligibility as full size containment.
func (s *Style) ShouldApplyInlineSizeContainment() bool {
	if !s.HasInlineSizeContainment() {
		return false
	}
	return isEligibleForSizeContainment(s)
}

// isEligibleForLayoutOrPaintContainment encodes the display-type
// eligibility filter shared by layout and paint containment per CSS
// Containment 1 §3 (preamble: "containment does not apply" list).
//
// Reads the *raw* `display` property because louis14's GetDisplay()
// collapses ruby-internal values (`ruby-base`, `ruby-base-container`,
// `ruby-text-container`) to DisplayBlock — there is no kRubyBase analog
// in the enum, mirroring the pkg/css/style.go GetDisplay comment.
func isEligibleForLayoutOrPaintContainment(s *Style) bool {
	disp, ok := s.Get("display")
	if !ok {
		return true
	}
	switch strings.TrimSpace(disp) {
	case "ruby-base", "ruby-base-container",
		"ruby-text-container", "ruby-text":
		// Internal ruby boxes (CSS Containment 1 §3 / Blink IsRuby family).
		return false
	case "table-row", "table-row-group",
		"table-header-group", "table-footer-group",
		"table-column", "table-column-group":
		// Internal table tracks (excludes table-cell / table-caption,
		// which are eligible per WPT contain-layout-cell-001 and
		// Blink IsTableSection/IsTableRow family).
		return false
	}
	// CSS Containment 1 §3 also excludes non-atomic inline-level boxes.
	// We do NOT exclude `display: inline` here because in louis14's
	// callsite model a UA-stylesheet-assigned `display: inline` on an
	// atomic-inline replaced element (`<img>`, `<video>`, etc.) would
	// be wrongly disqualified. The non-atomic-inline cases that need
	// suppression don't reach the containment-applying layout paths
	// (they have no width/height / no fragment geometry of their own).
	return true
}

// isEligibleForSizeContainment is the stricter filter for size
// containment: layout/paint eligibility PLUS exclusion of `table-cell`.
// Mirrors Blink LayoutObject::IsEligibleForSizeContainment which adds
// the table-cell branch on top of the shared filter. Confirmed by WPT
// contain-size-006 (table-cell expects normal layout, not size-contained).
func isEligibleForSizeContainment(s *Style) bool {
	if !isEligibleForLayoutOrPaintContainment(s) {
		return false
	}
	disp, ok := s.Get("display")
	if !ok {
		return true
	}
	if strings.TrimSpace(disp) == "table-cell" {
		return false
	}
	return true
}
