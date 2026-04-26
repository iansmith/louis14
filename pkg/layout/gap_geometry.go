package layout

// GapGeometry carries the per-gap layout data needed by GapDecorationsPainter
// to draw column-rule lines.
//
// Mirrors Blink's GapGeometry (core/layout/gap/gap_geometry.h), restricted to
// the kMultiColumn case. kGrid and kFlex exist as placeholders for future work.
//
// For multicol:
//   - MainDirection is always GapForRows (block axis).
//   - CrossGaps holds one entry per inter-column gap (column-rule positions).
//   - MainGaps holds one entry per inter-row gap or spanner boundary.
//   - The painter iterates CrossGaps for column-rule painting.
type GapContainerType int

const (
	GapContainerGrid    GapContainerType = iota
	GapContainerFlex    GapContainerType = iota
	GapContainerMulticol GapContainerType = iota
)

// GapDirection identifies whether a gap runs between columns (cross direction
// in multicol) or between rows (main direction in multicol).
//
// Mirrors Blink's GridTrackSizingDirection {kForColumns, kForRows} as used
// by GapGeometry and GapDecorationsPainter.
type GapDirection int

const (
	// GapForColumns is the cross direction in multicol — the column-rule gaps.
	GapForColumns GapDirection = iota
	// GapForRows is the main direction in multicol — the row-rule gaps.
	GapForRows GapDirection = iota
)

// GapGeometry is the geometry descriptor that the painter consumes to draw
// column/row rules. Produced at the end of ColumnLayoutAlgorithm::Layout()
// and stored on the multicol container's LayoutResult → PhysicalBoxFragment.
//
// Mirrors Blink's GapGeometry (core/layout/gap/gap_geometry.h).
type GapGeometry struct {
	ContainerType GapContainerType

	// InlineGapSize is the column-gap width (kMultiColumn: column_gap_size_).
	InlineGapSize float64
	// BlockGapSize is the row-gap height (kMultiColumn: row_gap_size_).
	BlockGapSize float64

	// MainGaps is the list of inter-row / spanner-boundary gaps.
	MainGaps []MainGap
	// CrossGaps is the list of inter-column gaps (column rules).
	CrossGaps []CrossGap

	// Content inline/block extents: the painter uses these to size rules.
	ContentInlineStart float64
	ContentInlineEnd   float64
	ContentBlockStart  float64
	ContentBlockEnd    float64

	// MainDirection is always GapForRows for multicol.
	MainDirection GapDirection

	// SpannerAdjacentIntersections caches cross-gap-intersection indices that
	// abut a spanner; populated by IsMultiColSpanner. Mirrors Blink's mutable
	// HashSet<wtf_size_t> multicol_spanner_adjacent_intersections_.
	SpannerAdjacentIntersections map[int]struct{}
}

// IsMainDirection returns true when direction matches the container's main
// axis (row axis for multicol). Used by the painter to choose the gap list.
func (gg *GapGeometry) IsMainDirection(direction GapDirection) bool {
	return direction == gg.MainDirection
}

// IsMultiColSpanner returns true when the cross-gap at gapIndex is adjacent
// to a column-span:all spanner and should not draw a rule segment.
//
// Mirrors Blink's GapGeometry::IsMultiColSpanner which checks whether the
// cross-gap index appears in the spanner-adjacent set populated during paint.
// We pre-compute during layout instead of lazily at paint time.
func (gg *GapGeometry) IsMultiColSpanner(gapIndex int, direction GapDirection) bool {
	if direction == gg.MainDirection {
		return false
	}
	if gg.SpannerAdjacentIntersections == nil {
		return false
	}
	_, ok := gg.SpannerAdjacentIntersections[gapIndex]
	return ok
}

// GetGapCenterOffset returns the logical center offset of a gap.
//
// For column (cross) gaps: returns the inline center offset of the rule.
// For row (main) gaps: returns the block center offset.
//
// Mirrors Blink's GapGeometry::GetGapCenterOffset.
func (gg *GapGeometry) GetGapCenterOffset(direction GapDirection, gapIndex int) float64 {
	if direction == GapForColumns {
		if gapIndex < len(gg.CrossGaps) {
			return gg.CrossGaps[gapIndex].GapInlineOffset
		}
		return 0
	}
	if gapIndex < len(gg.MainGaps) {
		return gg.MainGaps[gapIndex].GapOffset
	}
	return 0
}

// --- MainGap ---

// SpannerMainGapType tags a MainGap as the start or end boundary of a
// column-span:all spanner, or as an ordinary row gap.
//
// Mirrors Blink's SpannerMainGapType (main_gap.h).
type SpannerMainGapType int

const (
	SpannerGapNone  SpannerMainGapType = iota
	SpannerGapStart SpannerMainGapType = iota
	SpannerGapEnd   SpannerMainGapType = iota
)

// MainGap is one entry in GapGeometry.MainGaps — a row-axis gap between two
// column rows or at the boundary of a column-span:all spanner.
//
// Mirrors Blink's MainGap (core/layout/gap/main_gap.h).
type MainGap struct {
	// GapOffset is the block-axis center of this gap.
	GapOffset float64
	// SpannerType tags the gap as part of a spanner boundary.
	SpannerType SpannerMainGapType
}

func (m *MainGap) IsStartSpanner() bool { return m.SpannerType == SpannerGapStart }
func (m *MainGap) IsEndSpanner() bool   { return m.SpannerType == SpannerGapEnd }
func (m *MainGap) IsSpanner() bool      { return m.SpannerType != SpannerGapNone }

// --- CrossGap ---

// EdgeIntersectionState records whether the start/end of this cross-gap
// abuts a spanner, used by the painter to skip or trim rule segments.
//
// Mirrors Blink's CrossGap::EdgeIntersectionState (cross_gap.h).
type EdgeIntersectionState int

const (
	EdgeNone  EdgeIntersectionState = 0
	EdgeStart EdgeIntersectionState = 1
	EdgeEnd   EdgeIntersectionState = 2
	EdgeBoth  EdgeIntersectionState = 3
)

// CrossGap is one entry in GapGeometry.CrossGaps — a column-axis gap between
// two adjacent columns (the column-rule position).
//
// Mirrors Blink's CrossGap (core/layout/gap/cross_gap.h).
type CrossGap struct {
	// GapInlineOffset is the logical inline center of this inter-column gap.
	GapInlineOffset float64
	// GapBlockOffset is the logical block offset from the container top.
	GapBlockOffset float64
	// EdgeState records whether the start/end of this gap abuts a spanner.
	EdgeState EdgeIntersectionState
}
