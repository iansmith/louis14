package layout

// BreakAppeal scores how desirable a break point is. Mirrors Blink's
// BreakAppeal enum (third_party/blink/renderer/core/layout/break_appeal.h).
// Ordering low→high: LastResort < ViolatingOrphansAndWidows <
// ViolatingBreakAvoid < Perfect.
type BreakAppeal int

const (
	BreakAppealLastResort BreakAppeal = iota
	BreakAppealViolatingOrphansAndWidows
	BreakAppealViolatingBreakAvoid
	BreakAppealPerfect
)

// BreakStatus is the result of attempting to break before a child.
// Mirrors Blink's BreakStatus enum (fragmentation_utils.h).
type BreakStatus int

const (
	// BreakStatusContinue means no break was inserted; keep laying out
	// further children.
	BreakStatusContinue BreakStatus = iota
	// BreakStatusBrokeBefore means a break was inserted before this child.
	// The caller should finalize the current fragment and propagate the
	// outgoing break token.
	BreakStatusBrokeBefore
	// BreakStatusNeedsEarlierBreak means an earlier (more appealing)
	// breakpoint was found; the caller should re-layout with that earlier
	// break. Used by the EarlyBreak retry path (Phase 12g).
	BreakStatusNeedsEarlierBreak
	// BreakStatusDisableFragmentation indicates fragmentation should be
	// disabled for this subtree (e.g. monolithic content). Reserved.
	BreakStatusDisableFragmentation
)

// FragmentainerBreakPrecedence returns the precedence of a break-between
// value, used by JoinFragmentainerBreakValues to pick the dominant value.
// Mirrors Blink's FragmentainerBreakPrecedence (fragmentation_utils.cc).
//
// Order: auto(0) < avoid-column(1) < avoid-page(2) < avoid(3) < column(4) <
// page(5) < left/right/recto/verso(6).
func FragmentainerBreakPrecedence(v string) int {
	switch v {
	case "", "auto":
		return 0
	case "avoid-column":
		return 1
	case "avoid-page":
		return 2
	case "avoid":
		return 3
	case "column":
		return 4
	case "page":
		return 5
	case "left", "right", "recto", "verso":
		return 6
	default:
		return 0
	}
}

// JoinFragmentainerBreakValues combines two adjacent break-between values
// (typically previous element's break-after and next element's break-before),
// returning the dominant one. Mirrors Blink's JoinFragmentainerBreakValues.
func JoinFragmentainerBreakValues(first, second string) string {
	if FragmentainerBreakPrecedence(second) >= FragmentainerBreakPrecedence(first) {
		return second
	}
	return first
}

// IsForcedBreakValue returns true if the given break-between value forces a
// break in the current fragmentation context. Mirrors Blink's
// IsForcedBreakValue<Property>(ConstraintSpace, Property).
func IsForcedBreakValue(space ConstraintSpace, breakValue string) bool {
	if space.ShouldIgnoreForcedBreaks {
		return false
	}
	switch breakValue {
	case "column":
		return space.BlockFragmentationType == FragmentColumn
	case "page", "left", "right", "recto", "verso":
		return space.BlockFragmentationType == FragmentPage
	}
	return false
}

// IsAvoidBreakValue returns true if the given break-between or break-inside
// value asks to avoid breaks in the current fragmentation context. Mirrors
// Blink's IsAvoidBreakValue<Property>(ConstraintSpace, Property).
func IsAvoidBreakValue(space ConstraintSpace, breakValue string) bool {
	switch breakValue {
	case "avoid":
		return space.HasBlockFragmentation
	case "avoid-column":
		return space.BlockFragmentationType == FragmentColumn
	case "avoid-page":
		return space.BlockFragmentationType == FragmentPage
	}
	return false
}
