package css

import (
	"strconv"
	"strings"
)

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

// ShouldApplyStyleContainment reports whether style containment is in
// effect for this element — `HasStyleContainment` AND eligibility per
// CSS Containment 1 §3.3. Mirrors Blink LayoutObject::
// ShouldApplyStyleContainment() at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Style containment requires a principal box. display:contents elements
// generate no principal box and must not be style-contained (CSS Contain 1
// §3: "containment does not apply to ... elements that do not generate a
// box"). In Blink, display:contents elements produce no LayoutObject so
// ShouldApplyStyleContainment is never called for them; in louis14 we
// explicitly filter at this layer because EnterObject IS called for
// display:contents nodes.
func (s *Style) ShouldApplyStyleContainment() bool {
	if !s.HasStyleContainment() {
		return false
	}
	return isEligibleForStyleContainment(s)
}

// isEligibleForStyleContainment reports whether the element's display type
// permits style containment. The principal exclusion is display:contents,
// which generates no box (CSS Containment 1 §3). Style containment does
// not share the ruby-internal / table-internal-track exclusions of
// layout/paint containment (those boxes have no quote or counter context
// to contain anyway, but the spec text §3.3 only carves out the
// principal-box requirement).
func isEligibleForStyleContainment(s *Style) bool {
	disp, ok := s.Get("display")
	if !ok {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(disp), "contents")
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

// ContainIntrinsicWidth / ContainIntrinsicHeight return the length of
// contain-intrinsic-width / -height (StyleIntrinsicLength::GetLength), ok
// false for none. `auto <length>` gives the length: louis14 has no display
// locking, so there is never a remembered size to prefer
// (LayoutBox::OverrideIntrinsicContentInlineSize, layout_box.cc:1271-1300 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (s *Style) ContainIntrinsicWidth() (float64, bool) {
	return s.containIntrinsicLength("contain-intrinsic-width")
}

func (s *Style) ContainIntrinsicHeight() (float64, bool) {
	return s.containIntrinsicLength("contain-intrinsic-height")
}

func (s *Style) containIntrinsicLength(prop string) (float64, bool) {
	v, ok := s.Get(prop)
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if f := strings.Fields(v); len(f) == 2 && strings.EqualFold(f[0], "auto") {
		v = f[1]
	}
	if strings.EqualFold(v, "none") {
		return 0, false
	}
	return s.GetLengthForVal(v)
}

// parseContainIntrinsicSizeAxes splits a contain-intrinsic-size value into
// one or two axes, each [auto]? [none | <length [0,∞]>]; ok is false when
// the value does not match (a lone auto, a percentage, a negative or
// unitless non-zero length, extra tokens).
func parseContainIntrinsicSizeAxes(value string) ([]string, bool) {
	parts := splitShorthandParts(value)
	var axes []string
	for i := 0; i < len(parts); i++ {
		axis := parts[i]
		if strings.EqualFold(axis, "auto") {
			if i+1 >= len(parts) {
				return nil, false
			}
			i++
			axis = "auto " + parts[i]
			if !validContainIntrinsicLength(parts[i]) {
				return nil, false
			}
		} else if !validContainIntrinsicLength(axis) {
			return nil, false
		}
		axes = append(axes, axis)
	}
	if len(axes) == 0 || len(axes) > 2 {
		return nil, false
	}
	return axes, true
}

// validContainIntrinsicLength reports whether v is none or a non-negative
// <length> (no percentage; a bare number only when 0).
func validContainIntrinsicLength(v string) bool {
	if strings.EqualFold(v, "none") {
		return true
	}
	if strings.HasSuffix(v, "%") {
		return false
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v == "0" || strings.Trim(v, "+-.0") == ""
	}
	n, ok := ParseLength(v)
	return ok && n >= 0
}
