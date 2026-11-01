package layout

import "math"

// spannerRowColumns marks a spanner "row" in ColumnGapAccumulator's
// columns-per-row list. Mirrors Blink's use of kNotFound there
// (column_gap_accumulator.cc:52-53 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
const spannerRowColumns = -1

// ColumnGapAccumulator accumulates the gap-decoration state (main gaps, cross
// gaps, segment states) of a multicol container while its lines are laid
// out, and builds the fragment's GapGeometry. Instantiated only when the
// style has a gap rule. Mirrors Blink's ColumnGapAccumulator
// (core/layout/column_gap_accumulator.h, .cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type ColumnGapAccumulator struct {
	columnGapSize        float64
	rowGapSize           float64
	specifiedColumnCount int
	hasAutoColumnCount   bool

	gapGeometry          *GapGeometry
	firstColumnOffset    LogicalOffset
	hasFirstColumnOffset bool
	maxColumnsInRow      int
	// columnsPerRow is nil until the first row is recorded (Blink's
	// optional<Vector<wtf_size_t>> columns_per_row_).
	columnsPerRow []int
}

// NewColumnGapAccumulator mirrors the ColumnGapAccumulator constructor
// (column_gap_accumulator.cc:12-21).
func NewColumnGapAccumulator(columnGapSize, rowGapSize float64, specifiedColumnCount int, hasAutoColumnCount bool) *ColumnGapAccumulator {
	return &ColumnGapAccumulator{
		columnGapSize:        columnGapSize,
		rowGapSize:           rowGapSize,
		specifiedColumnCount: specifiedColumnCount,
		hasAutoColumnCount:   hasAutoColumnCount,
		gapGeometry:          NewGapGeometry(GapContainerMulticol),
	}
}

// AddMainGap records a row gap (centered in the gap) or a spanner boundary.
// Mirrors ColumnGapAccumulator::AddMainGap (column_gap_accumulator.cc:23-32).
func (a *ColumnGapAccumulator) AddMainGap(blockOffset float64, gapType SpannerMainGapType) {
	if gapType == SpannerGapNone {
		blockOffset += a.rowGapSize / 2
	}
	a.gapGeometry.AddMainGap(blockOffset, gapType)
}

// AddEndSpannerMainGapIfNeeded mirrors
// ColumnGapAccumulator::AddEndSpannerMainGapIfNeeded (:34-41).
func (a *ColumnGapAccumulator) AddEndSpannerMainGapIfNeeded(blockOffset float64) {
	if !a.lastMainGapIsStartSpanner() {
		return
	}
	a.AddMainGap(blockOffset, SpannerGapEnd)
}

// AddStartSpannerMainGapIfNeeded mirrors
// ColumnGapAccumulator::AddStartSpannerMainGapIfNeeded (:43-54): spanner
// rows count as kNotFound columns since they span the full column count.
func (a *ColumnGapAccumulator) AddStartSpannerMainGapIfNeeded(blockOffset float64) {
	if a.lastMainGapIsStartSpanner() {
		return
	}
	a.AddMainGap(blockOffset, SpannerGapStart)
	a.AddNumberOfColumnsForCurrentRow(spannerRowColumns)
}

// AddCrossGap records the column gap before a column starting at
// columnInlineStartOffset. Mirrors ColumnGapAccumulator::AddCrossGap
// (:56-63); every cross gap starts at the first column's block offset.
func (a *ColumnGapAccumulator) AddCrossGap(columnInlineStartOffset float64) {
	gapCenter := columnInlineStartOffset - a.columnGapSize/2
	a.gapGeometry.AddCrossGap(LogicalOffset{InlineOffset: gapCenter, BlockOffset: a.firstColumnOffset.BlockOffset})
}

// AddNumberOfColumnsForCurrentRow mirrors
// ColumnGapAccumulator::AddNumberOfColumnsForCurrentRow (:65-73).
func (a *ColumnGapAccumulator) AddNumberOfColumnsForCurrentRow(colsInRow int) {
	a.columnsPerRow = append(a.columnsPerRow, colsInRow)
	a.finalizeMainGapSegmentStateForCurrentRow(colsInRow)
}

// SetFirstColumnOffsetIfNeeded mirrors
// ColumnGapAccumulator::SetFirstColumnOffsetIfNeeded (:75-79).
func (a *ColumnGapAccumulator) SetFirstColumnOffsetIfNeeded(offset LogicalOffset) {
	if !a.hasFirstColumnOffset {
		a.firstColumnOffset = offset
		a.hasFirstColumnOffset = true
	}
}

// UpdateMaxColumnsInRow mirrors ColumnGapAccumulator::UpdateMaxColumnsInRow (:81-83).
func (a *ColumnGapAccumulator) UpdateMaxColumnsInRow(count int) {
	a.maxColumnsInRow = max(a.maxColumnsInRow, count)
}

// ShouldAddCrossGapAt reports whether no earlier row already recorded the
// cross gap before column columnIndexInRow (column gaps line up across
// rows). Mirrors ColumnGapAccumulator::ShouldAddCrossGapAt (:85-88).
func (a *ColumnGapAccumulator) ShouldAddCrossGapAt(columnIndexInRow int) bool {
	return columnIndexInRow >= a.maxColumnsInRow
}

func (a *ColumnGapAccumulator) lastMainGapIsStartSpanner() bool {
	gg := a.gapGeometry
	return gg.MainGapCount() > 0 && gg.MainGaps[gg.MainGapCount()-1].IsStartSpannerMainGap()
}

// BuildGapGeometry finishes the geometry for the fragment, or returns nil
// when no gaps (or no columns) were recorded. Mirrors
// ColumnGapAccumulator::BuildGapGeometry (column_gap_accumulator.cc:95-167).
// contentInlineEnd / contentBlockEnd are Blink's
// FragmentInlineSize() - BorderScrollbarPadding().inline_end and
// FragmentBlockSize() - the applicable block-end border, scrollbar and
// padding, expressed in the content-box-relative offsets GapGeometry uses.
func (a *ColumnGapAccumulator) BuildGapGeometry(contentInlineEnd, contentBlockEnd, columnInlineSize float64) *GapGeometry {
	gg := a.gapGeometry
	if (gg.CrossGapCount() == 0 && gg.MainGapCount() == 0) || !a.hasFirstColumnOffset {
		return nil
	}

	// Fewer columns than column-count were created: add a cross gap for
	// each column that would have been created, up to column-count.
	for i := a.maxColumnsInRow; i < a.specifiedColumnCount; i++ {
		inlineOffset := 0.0
		if gg.CrossGapCount() > 0 {
			inlineOffset = gg.CrossGaps[gg.CrossGapCount()-1].GapOffset.InlineOffset +
				a.columnGapSize/2 + columnInlineSize + a.columnGapSize
		}
		a.AddCrossGap(inlineOffset)
	}

	if gg.CrossGapCount() > 0 {
		contentInlineEnd = math.Max(contentInlineEnd, gg.CrossGaps[gg.CrossGapCount()-1].GapOffset.InlineOffset)
		if a.columnsPerRow != nil {
			a.updateCrossGapSegmentStates()
		}
		gg.InlineGapSize = a.columnGapSize
	}

	if gg.MainGapCount() > 0 {
		// Blink TODO(crbug.com/357648037): content beyond the last main gap
		// is not accounted for.
		contentBlockEnd = math.Max(contentBlockEnd, gg.MainGaps[gg.MainGapCount()-1].GapOffset)
		gg.BlockGapSize = a.rowGapSize
	}

	gg.SetContentInlineOffsets(a.firstColumnOffset.InlineOffset, contentInlineEnd)
	gg.SetContentBlockOffsets(a.firstColumnOffset.BlockOffset, contentBlockEnd)
	gg.MainDirection = GapForRows
	gg.Finalize()
	return gg
}

// finalizeMainGapSegmentStateForCurrentRow marks the part of the main gap
// above this row that borders only the longer of the two rows as empty on
// the shorter row's side. Mirrors
// ColumnGapAccumulator::FinalizeMainGapSegmentStateForCurrentRow (:169-207).
func (a *ColumnGapAccumulator) finalizeMainGapSegmentStateForCurrentRow(colsInRow int) {
	gg := a.gapGeometry
	if colsInRow == spannerRowColumns || gg.MainGapCount() == 0 ||
		gg.MainGaps[gg.MainGapCount()-1].IsSpannerMainGap() {
		return
	}
	// Walk back to the last column row, skipping interleaved spanner rows.
	prevIndex := len(a.columnsPerRow) - 2
	for a.columnsPerRow[prevIndex] == spannerRowColumns {
		if prevIndex == 0 {
			// The gap is between this row and a spanner above.
			return
		}
		prevIndex--
	}
	colsAbove := a.columnsPerRow[prevIndex]
	if colsAbove == colsInRow {
		return
	}
	state := GapSegmentEmptyBefore
	if colsAbove > colsInRow {
		state = GapSegmentEmptyAfter
	}
	gg.MainGapAt(gg.MainGapCount() - 1).AddGapSegmentStateRange(GapSegmentStateRange{
		Start: min(colsAbove, colsInRow),
		End:   max(colsAbove, colsInRow),
		State: state,
	})
}

// updateCrossGapSegmentStates computes, per row, the segment state of each
// cross gap from the number of columns in that row. Mirrors
// ColumnGapAccumulator::UpdateCrossGapSegmentStates (:209-240).
func (a *ColumnGapAccumulator) updateCrossGapSegmentStates() {
	gg := a.gapGeometry
	for crossGapIndex := range gg.CrossGaps {
		crossGap := gg.CrossGapAt(crossGapIndex)
		for rowIndex, colsInRow := range a.columnsPerRow {
			// Columns on both sides of this cross gap in this row.
			if colsInRow != spannerRowColumns && crossGapIndex+1 < colsInRow {
				continue
			}
			isOutsideSpecifiedColumnCount := crossGapIndex+1 >= a.specifiedColumnCount && !a.hasAutoColumnCount
			var state GapSegmentState
			switch {
			case colsInRow == spannerRowColumns && !isOutsideSpecifiedColumnCount:
				state = GapSegmentBlocked
			case crossGapIndex+1 == colsInRow:
				// After the last column of this row.
				state = GapSegmentEmptyAfter
			default:
				state = GapSegmentEmptyBoth
			}
			crossGap.AddGapSegmentStateRange(GapSegmentStateRange{Start: rowIndex, End: rowIndex + 1, State: state})
		}
	}
}
