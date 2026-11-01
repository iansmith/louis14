package layout

// GapSegmentState is the occupancy of the two sides of one gap segment.
// Mirrors Blink's GapSegmentState (core/layout/gap/gap_utils.h:63-104 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The zero value is kNone (both
// sides occupied); Blink's default constructor is kEmptyBoth, which
// louis14 spells GapSegmentEmptyBoth.
type GapSegmentState uint

const (
	// GapSegmentNone: occupied on both sides.
	GapSegmentNone GapSegmentState = 0
	// GapSegmentEmptyBefore: empty cell before the segment.
	GapSegmentEmptyBefore GapSegmentState = 1 << 0
	// GapSegmentEmptyAfter: empty cell after the segment.
	GapSegmentEmptyAfter GapSegmentState = 1 << 1
	// GapSegmentBlocked: blocked by a spanning item.
	GapSegmentBlocked GapSegmentState = 1 << 2
	// GapSegmentEmptyBoth mirrors GapSegmentState::kEmptyBoth.
	GapSegmentEmptyBoth = GapSegmentEmptyBefore | GapSegmentEmptyAfter
)

// HasGapStatus mirrors GapSegmentState::HasGapStatus.
func (s GapSegmentState) HasGapStatus(status GapSegmentState) bool { return s&status != 0 }

// HasEmptyStatus reports one or more empty sides (GapSegmentState::HasEmptyStatus).
func (s GapSegmentState) HasEmptyStatus() bool {
	return s.HasGapStatus(GapSegmentEmptyBefore) || s.HasGapStatus(GapSegmentEmptyAfter)
}

// IsEmpty reports both sides empty (GapSegmentState::IsEmpty).
func (s GapSegmentState) IsEmpty() bool { return s == GapSegmentEmptyBoth }

// GapSegmentStateRange is a contiguous range [Start, End) of gap segments
// sharing one state. Mirrors Blink's GapSegmentStateRange
// (gap_utils.h:106-116).
type GapSegmentStateRange struct {
	Start, End int
	State      GapSegmentState
}

// GapSegmentStateCursor walks sorted, non-overlapping ranges forward,
// returning the state of each successive segment (kNone outside every
// range). Mirrors Blink's GapSegmentStateCursor (gap_utils.h:120-153).
type GapSegmentStateCursor struct {
	ranges          []GapSegmentStateRange
	rangeIndex      int
	currentGapIndex int
}

// NewGapSegmentStateCursor mirrors the GapSegmentStateCursor constructor.
func NewGapSegmentStateCursor(ranges []GapSegmentStateRange) GapSegmentStateCursor {
	return GapSegmentStateCursor{ranges: ranges}
}

// GetNextGapSegmentState mirrors GapSegmentStateCursor::GetNextGapSegmentState.
func (c *GapSegmentStateCursor) GetNextGapSegmentState() GapSegmentState {
	for c.rangeIndex < len(c.ranges) && c.ranges[c.rangeIndex].End <= c.currentGapIndex {
		c.rangeIndex++
	}
	state := GapSegmentNone
	if c.rangeIndex < len(c.ranges) {
		r := c.ranges[c.rangeIndex]
		if r.Start <= c.currentGapIndex && c.currentGapIndex < r.End {
			state = r.State
		}
	}
	c.currentGapIndex++
	return state
}

// GapIntersection is a point where a gap meets a content edge or an
// orthogonal gap, with the state of the segment that starts there. Mirrors
// Blink's GapIntersection (core/layout/gap/gap_intersection.h @
// a9f50e522efa) without the flex-only ExtraIntersectionState.
type GapIntersection struct {
	Offset       float64
	SegmentState GapSegmentState
}
