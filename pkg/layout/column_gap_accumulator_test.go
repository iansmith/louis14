package layout

import (
	"reflect"
	"testing"
)

// TestColumnGapAccumulator_RowWrapSegmentStates (LOU-486): three rows of a
// 3-column column-wrap multicol, the last with two columns. Cross gaps are
// recorded once per column position (ShouldAddCrossGapAt); row gaps are
// centered in the gap; the cross gap after the last column of the short row
// is kEmptyAfter there, and the row gap above that row is kEmptyAfter over
// the missing column (ColumnGapAccumulator::UpdateCrossGapSegmentStates /
// FinalizeMainGapSegmentStateForCurrentRow, column_gap_accumulator.cc:169-240
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestColumnGapAccumulator_RowWrapSegmentStates(t *testing.T) {
	acc := NewColumnGapAccumulator(20, 20, 3, false)
	addRow := func(blockOffset float64, cols int) {
		for i := 0; i < cols; i++ {
			if i > 0 && acc.ShouldAddCrossGapAt(i) {
				acc.AddCrossGap(float64(i) * 120)
			}
			acc.SetFirstColumnOffsetIfNeeded(LogicalOffset{InlineOffset: float64(i) * 120, BlockOffset: blockOffset})
		}
		acc.UpdateMaxColumnsInRow(cols)
		acc.AddNumberOfColumnsForCurrentRow(cols)
	}
	addRow(0, 3)
	acc.AddMainGap(40, SpannerGapNone)
	addRow(60, 3)
	acc.AddMainGap(100, SpannerGapNone)
	addRow(120, 2)
	gg := acc.BuildGapGeometry(340, 160, 100)
	if gg == nil {
		t.Fatal("BuildGapGeometry = nil")
	}

	if got := []float64{gg.CrossGaps[0].GapOffset.InlineOffset, gg.CrossGaps[1].GapOffset.InlineOffset}; !reflect.DeepEqual(got, []float64{110, 230}) {
		t.Errorf("cross gap centers = %v, want [110 230]", got)
	}
	if got := []float64{gg.MainGaps[0].GapOffset, gg.MainGaps[1].GapOffset}; !reflect.DeepEqual(got, []float64{50, 110}) {
		t.Errorf("main gap centers = %v, want [50 110]", got)
	}
	if want := []GapSegmentStateRange{{Start: 2, End: 3, State: GapSegmentEmptyAfter}}; !reflect.DeepEqual(gg.MainGaps[1].GapSegmentStateRanges, want) {
		t.Errorf("main gap 1 ranges = %v, want %v", gg.MainGaps[1].GapSegmentStateRanges, want)
	}

	// Cross gap 1: segments per row, the third after the short row's last
	// column.
	inter := gg.GenerateIntersectionListForGap(GapForColumns, 1, nil)
	want := []GapIntersection{
		{Offset: 0, SegmentState: GapSegmentNone},
		{Offset: 50, SegmentState: GapSegmentNone},
		{Offset: 110, SegmentState: GapSegmentEmptyAfter},
		{Offset: 160, SegmentState: GapSegmentNone},
	}
	if !reflect.DeepEqual(inter, want) {
		t.Errorf("cross gap 1 intersections = %v, want %v", inter, want)
	}
	// Row-gap junctions are inset by half the row gap; edges are not.
	if got := gg.GetCrossWidthForIntersection(GapForColumns, 1, 1, false, inter); got != 20 {
		t.Errorf("junction cross width = %v, want 20", got)
	}
	if got := gg.GetCrossWidthForIntersection(GapForColumns, 1, 0, false, inter); got != 0 {
		t.Errorf("edge cross width = %v, want 0", got)
	}
}

// TestColumnGapAccumulator_SpannerBlocksCrossGap (LOU-486): a spanner
// between two rows records start/end main gaps and a kNotFound row; the
// cross gap is kBlocked across the spanner, and the spanner-adjacent
// intersections are edges (no crossing gap)
// (column_gap_accumulator.cc:34-54, gap_geometry.cc:551-591, :631-683 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestColumnGapAccumulator_SpannerBlocksCrossGap(t *testing.T) {
	acc := NewColumnGapAccumulator(10, 0, 2, false)
	addRow := func(blockOffset float64) {
		if acc.ShouldAddCrossGapAt(1) {
			acc.AddCrossGap(55)
		}
		acc.SetFirstColumnOffsetIfNeeded(LogicalOffset{BlockOffset: blockOffset})
		acc.UpdateMaxColumnsInRow(2)
		acc.AddNumberOfColumnsForCurrentRow(2)
	}
	addRow(0)
	acc.AddStartSpannerMainGapIfNeeded(40)
	acc.AddStartSpannerMainGapIfNeeded(40) // adjacent spanner: no second start
	acc.AddEndSpannerMainGapIfNeeded(60)
	acc.AddEndSpannerMainGapIfNeeded(60) // already ended
	addRow(60)
	gg := acc.BuildGapGeometry(100, 100, 45)

	if len(gg.MainGaps) != 2 || !gg.MainGaps[0].IsStartSpannerMainGap() || !gg.MainGaps[1].IsEndSpannerMainGap() {
		t.Fatalf("main gaps = %+v, want [start@40 end@60]", gg.MainGaps)
	}
	inter := gg.GenerateIntersectionListForGap(GapForColumns, 0, nil)
	want := []GapIntersection{
		{Offset: 0, SegmentState: GapSegmentNone},
		{Offset: 40, SegmentState: GapSegmentBlocked},
		{Offset: 60, SegmentState: GapSegmentNone},
		{Offset: 100, SegmentState: GapSegmentNone},
	}
	if !reflect.DeepEqual(inter, want) {
		t.Errorf("intersections = %v, want %v", inter, want)
	}
	for _, i := range []int{1, 2} {
		if got := gg.GetCrossWidthForIntersection(GapForColumns, 0, i, false, inter); got != 0 {
			t.Errorf("spanner-adjacent intersection %d cross width = %v, want 0", i, got)
		}
	}
}
