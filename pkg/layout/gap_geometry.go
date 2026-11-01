package layout

// GapGeometry is the Main-Cross gap geometry the gap-decorations painter
// consumes to draw column (and row) rules. Mirrors Blink's GapGeometry
// (core/layout/gap/gap_geometry.h, gap_geometry.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), restricted to the kMultiColumn
// container type: louis14 builds gap geometry only for multicol
// (ColumnGapAccumulator). kGrid and kFlex are kept so the container-type
// switches read like Blink's.
//
// For multicol (gap/README.md "Multicol"):
//   - the main direction is kForRows: MainGaps are the row gaps of
//     column-wrap plus the start/end boundaries of column-span:all spanners;
//   - CrossGaps are the column gaps, shared by every row because the
//     columns of all rows line up.
//
// Intersections are computed at paint time, not stored
// (GenerateIntersectionListForGap).
//
// Offsets are logical and relative to the multicol's content box (louis14's
// child-offset space, see BoxFragmentBuilder.Build); Blink's are relative to
// the border box. The painter converts over the content box accordingly.
type GapContainerType int

const (
	GapContainerGrid GapContainerType = iota
	GapContainerFlex
	GapContainerMulticol
)

// GapDirection identifies the axis a gap runs along. Mirrors Blink's
// GridTrackSizingDirection {kForColumns, kForRows} as used by GapGeometry
// and GapDecorationsPainter.
type GapDirection int

const (
	// GapForColumns: column gaps (the cross direction in multicol).
	GapForColumns GapDirection = iota
	// GapForRows: row gaps (the main direction in multicol).
	GapForRows
)

// BlockedStatus records whether the gap segments before/after an
// intersection are blocked by a spanning item. Mirrors Blink's BlockedStatus
// (gap_geometry.h:32-55 @ a9f50e522efa).
type BlockedStatus uint

const (
	BlockedNone   BlockedStatus = 0
	BlockedBefore BlockedStatus = 1 << 0
	BlockedAfter  BlockedStatus = 1 << 1
)

// HasBlockedStatus mirrors BlockedStatus::HasBlockedStatus.
func (s BlockedStatus) HasBlockedStatus(status BlockedStatus) bool { return s&status != 0 }

// GapGeometry mirrors Blink's GapGeometry class (gap_geometry.h:64-577 @
// a9f50e522efa).
type GapGeometry struct {
	ContainerType GapContainerType

	// InlineGapSize is the column-gap (inline_gap_size_).
	InlineGapSize float64
	// BlockGapSize is the row-gap (block_gap_size_).
	BlockGapSize float64

	MainGaps  []MainGap
	CrossGaps []CrossGap

	// Content extents (content_inline_start_ ... content_block_end_): the
	// container-edge intersections of every gap.
	ContentInlineStart float64
	ContentInlineEnd   float64
	ContentBlockStart  float64
	ContentBlockEnd    float64

	// MainDirection is kForRows for multicol (SetMainDirection).
	MainDirection GapDirection

	// multicolSpannerAdjacentIntersections is transient paint state: the
	// cross-gap intersection indices adjacent to a spanner main gap, filled
	// by GenerateIntersectionListForGap and reset by InitPaintState. Mirrors
	// Blink's mutable multicol_spanner_adjacent_intersections_
	// (gap_geometry.h:572-576 @ a9f50e522efa).
	multicolSpannerAdjacentIntersections map[int]struct{}
}

// NewGapGeometry mirrors Blink's GapGeometry(ContainerType) constructor.
func NewGapGeometry(containerType GapContainerType) *GapGeometry {
	return &GapGeometry{ContainerType: containerType}
}

// AddMainGap mirrors GapGeometry::AddMainGap (gap_geometry.h:178-182).
func (gg *GapGeometry) AddMainGap(offset float64, gapType SpannerMainGapType) *MainGap {
	gg.MainGaps = append(gg.MainGaps, MainGap{GapOffset: offset, SpannerType: gapType})
	return &gg.MainGaps[len(gg.MainGaps)-1]
}

// AddCrossGap mirrors GapGeometry::AddCrossGap(LogicalOffset)
// (gap_geometry.h:184-187).
func (gg *GapGeometry) AddCrossGap(offset LogicalOffset) *CrossGap {
	gg.CrossGaps = append(gg.CrossGaps, CrossGap{GapOffset: offset})
	return &gg.CrossGaps[len(gg.CrossGaps)-1]
}

// MainGapAt / CrossGapAt / MainGapCount / CrossGapCount mirror the
// gap_geometry.h:200-211 accessors.
func (gg *GapGeometry) MainGapAt(index int) *MainGap   { return &gg.MainGaps[index] }
func (gg *GapGeometry) CrossGapAt(index int) *CrossGap { return &gg.CrossGaps[index] }
func (gg *GapGeometry) MainGapCount() int              { return len(gg.MainGaps) }
func (gg *GapGeometry) CrossGapCount() int             { return len(gg.CrossGaps) }

// SetContentInlineOffsets / SetContentBlockOffsets mirror
// gap_geometry.cc:185-195.
func (gg *GapGeometry) SetContentInlineOffsets(start, end float64) {
	gg.ContentInlineStart, gg.ContentInlineEnd = start, end
}

func (gg *GapGeometry) SetContentBlockOffsets(start, end float64) {
	gg.ContentBlockStart, gg.ContentBlockEnd = start, end
}

// Finalize mirrors GapGeometry::Finalize (gap_geometry.h:228), which only
// initialises the flex-only main_gap_running_index_ cursor; nothing to do
// for multicol.
func (gg *GapGeometry) Finalize() {}

// InitPaintState resets transient per-paint state. Mirrors
// GapGeometry::InitPaintState (gap_geometry.h:233-236); the flex
// main_gap_running_index_ cursor has no louis14 counterpart.
func (gg *GapGeometry) InitPaintState() {
	gg.multicolSpannerAdjacentIntersections = nil
}

// IsMainDirection mirrors GapGeometry::IsMainDirection.
func (gg *GapGeometry) IsMainDirection(direction GapDirection) bool {
	return direction == gg.MainDirection
}

// IsMultiColSpanner reports whether main gap gapIndex is a spanner boundary,
// which is not painted (it only produces cross-gap intersections). Mirrors
// GapGeometry::IsMultiColSpanner (gap_geometry.cc:96-104).
func (gg *GapGeometry) IsMultiColSpanner(gapIndex int, direction GapDirection) bool {
	if gg.ContainerType == GapContainerMulticol && gg.IsMainDirection(direction) {
		return gg.MainGaps[gapIndex].IsSpannerMainGap()
	}
	return false
}

// GetGapCenterOffset mirrors GapGeometry::GetGapCenterOffset
// (gap_geometry.cc:197-207).
func (gg *GapGeometry) GetGapCenterOffset(direction GapDirection, gapIndex int) float64 {
	if gg.IsMainDirection(direction) {
		return gg.MainGaps[gapIndex].GapOffset
	}
	if direction == GapForColumns {
		return gg.CrossGaps[gapIndex].GapOffset.InlineOffset
	}
	return gg.CrossGaps[gapIndex].GapOffset.BlockOffset
}

// GenerateIntersectionListForGap fills intersections with the points where
// gap gapIndex meets the content edges or orthogonal gaps. Mirrors
// GapGeometry::GenerateIntersectionListForGap (gap_geometry.cc:209-221) for
// the multicol cross direction (GenerateCrossIntersectionListForMulticol,
// :551-591). Not ported: the main-direction list (:223-272), only needed to
// paint row rules, which louis14 does not support (no row-rule properties).
func (gg *GapGeometry) GenerateIntersectionListForGap(direction GapDirection, gapIndex int, intersections []GapIntersection) []GapIntersection {
	intersections = intersections[:0]
	if gg.IsMainDirection(direction) || gg.ContainerType != GapContainerMulticol {
		return intersections
	}
	cursor := NewGapSegmentStateCursor(gg.getGapSegmentStateRangesForGap(direction, gapIndex))
	crossGap := gg.CrossGaps[gapIndex]
	intersections = append(intersections, GapIntersection{Offset: crossGap.GapOffset.BlockOffset, SegmentState: cursor.GetNextGapSegmentState()})
	for _, mainGap := range gg.MainGaps {
		intersections = append(intersections, GapIntersection{Offset: mainGap.GapOffset, SegmentState: cursor.GetNextGapSegmentState()})
		// Intersections adjacent to spanner main gaps have no crossing gap
		// and are treated as edges (gap_geometry.cc:575-587).
		if mainGap.IsSpannerMainGap() {
			if gg.multicolSpannerAdjacentIntersections == nil {
				gg.multicolSpannerAdjacentIntersections = map[int]struct{}{}
			}
			gg.multicolSpannerAdjacentIntersections[len(intersections)-1] = struct{}{}
		}
	}
	intersections = append(intersections, GapIntersection{Offset: gg.ContentBlockEnd, SegmentState: cursor.GetNextGapSegmentState()})
	return intersections
}

// IsIntersectionAtContainerEdge mirrors
// GapGeometry::IsIntersectionAtContainerEdge (gap_geometry.cc:631-683) for
// grid-like edges and the multicol cross-gap spanner edges.
func (gg *GapGeometry) IsIntersectionAtContainerEdge(gapIndex, intersectionIndex, intersectionCount int, isMainGap bool) bool {
	last := intersectionCount - 1
	if intersectionIndex == 0 || intersectionIndex == last {
		return true
	}
	if gg.ContainerType == GapContainerMulticol && !isMainGap {
		_, ok := gg.multicolSpannerAdjacentIntersections[intersectionIndex]
		return ok
	}
	return false
}

// GetCrossWidthForIntersection is the width of the crossing gap at an
// intersection: zero at container edges, else the cross gutter. Mirrors
// GapGeometry::GetCrossWidthForIntersection (gap_geometry.cc:772-797); the
// flex per-line arm is not ported.
func (gg *GapGeometry) GetCrossWidthForIntersection(direction GapDirection, gapIndex, intersectionIndex int, isMainGap bool, intersections []GapIntersection) float64 {
	if gg.IsIntersectionAtContainerEdge(gapIndex, intersectionIndex, len(intersections), isMainGap) {
		return 0
	}
	if direction == GapForRows {
		return gg.InlineGapSize
	}
	return gg.BlockGapSize
}

// BlockedStatusFromGapStates mirrors GapGeometry::BlockedStatusFromGapStates
// (gap_geometry.cc:875-890).
func BlockedStatusFromGapStates(intersections []GapIntersection, index int) BlockedStatus {
	status := BlockedNone
	if index > 0 && intersections[index-1].SegmentState.HasGapStatus(GapSegmentBlocked) {
		status |= BlockedBefore
	}
	if intersections[index].SegmentState.HasGapStatus(GapSegmentBlocked) {
		status |= BlockedAfter
	}
	return status
}

// getGapSegmentStateRangesForGap mirrors
// GapGeometry::GetGapSegmentStateRangesForGap (gap_geometry.cc:892-907).
func (gg *GapGeometry) getGapSegmentStateRangesForGap(direction GapDirection, gapIndex int) []GapSegmentStateRange {
	if gg.IsMainDirection(direction) {
		return gg.MainGaps[gapIndex].GapSegmentStateRanges
	}
	return gg.CrossGaps[gapIndex].GapSegmentStateRanges
}

// --- MainGap ---

// SpannerMainGapType tags a multicol MainGap as the start or end boundary of
// a column-span:all spanner, or as an ordinary row gap. Mirrors Blink's
// SpannerMainGapType (main_gap.h).
type SpannerMainGapType int

const (
	SpannerGapNone SpannerMainGapType = iota
	SpannerGapStart
	SpannerGapEnd
)

// MainGap mirrors Blink's MainGap (core/layout/gap/main_gap.h @
// a9f50e522efa), without the flex-only cross-gap ranges.
type MainGap struct {
	// GapOffset is the block-axis center of the gap (gap_offset_).
	GapOffset float64
	// GapSegmentStateRanges slices the gap by segment state; nil when the
	// whole gap is kNone (gap_segment_state_ranges_ absent).
	GapSegmentStateRanges []GapSegmentStateRange
	// hasBlockedRange mirrors has_blocked_range_.
	hasBlockedRange bool
	// SpannerType: multicol only (spanner_main_gap_type_).
	SpannerType SpannerMainGapType
}

func (m *MainGap) IsStartSpannerMainGap() bool { return m.SpannerType == SpannerGapStart }
func (m *MainGap) IsEndSpannerMainGap() bool   { return m.SpannerType == SpannerGapEnd }
func (m *MainGap) IsSpannerMainGap() bool      { return m.SpannerType != SpannerGapNone }

// HasBlockedRange mirrors MainGap::HasBlockedRange.
func (m *MainGap) HasBlockedRange() bool { return m.hasBlockedRange }

// AddGapSegmentStateRange mirrors MainGap::AddGapSegmentStateRange
// (main_gap.cc:20-29).
func (m *MainGap) AddGapSegmentStateRange(r GapSegmentStateRange) {
	if r.State.HasGapStatus(GapSegmentBlocked) {
		m.hasBlockedRange = true
	}
	m.GapSegmentStateRanges = append(m.GapSegmentStateRanges, r)
}

// --- CrossGap ---

// CrossGap mirrors Blink's CrossGap (core/layout/gap/cross_gap.h @
// a9f50e522efa), without the flex-only edge state.
type CrossGap struct {
	// GapOffset is the inline center of the gap and the block offset where
	// it starts (gap_logical_offset_).
	GapOffset LogicalOffset
	// GapSegmentStateRanges slices the gap per row by segment state; nil
	// when every segment is kNone (gap_segment_state_ranges_ absent).
	GapSegmentStateRanges []GapSegmentStateRange
}

// AddGapSegmentStateRange mirrors CrossGap::AddGapSegmentStateRange
// (cross_gap.cc:74-80).
func (c *CrossGap) AddGapSegmentStateRange(r GapSegmentStateRange) {
	c.GapSegmentStateRanges = append(c.GapSegmentStateRanges, r)
}
