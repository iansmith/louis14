package layout

// Indicator tests for LOU-215: column-height / column-wrap row-wrap path.
//
// Three bugs targeted (all confirmed RED before implementation):
//
//   Bug A — Phase 18 ConsumedRowBlockSize carrier fires incorrectly on second
//   outer fragment when consumedRowBlockSize > 0 (offsetInCurrentRow > 0 on
//   resume). Fires for every remaining-token even when the row fits in the
//   outer space. Mirrors Blink cla.cc:764-773 overflow check.
//   Affects: column-height-007, column-height-008.
//
//   Bug B — Spanner pre-commit row snap fires unconditionally whenever
//   offsetInCurrentRow > 0, but Blink only snaps when the spanner does NOT
//   fit in the remaining row height (remaining < spanner height).
//   Mirrors Blink cla.cc:1365-1395 IsPastStartInWrappingRow + fit check.
//   Affects: column-height-006, column-height-013, column-height-018.
//
//   Bug C — Missing addMainGap call on row-wrap: Blink calls
//   gap_accumulator_->AddMainGap(line_offset - row_gap_size_) inside
//   LayoutLine when has_wrapped=true (cla.cc:1263-1268). Louis14 never
//   records this gap, causing column rules to span wrong block extents.
//   Affects: column-height-009.

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestColumnHeight_PhaseA_ConsumedRowBlockSizeNoSpuriousFire tests Bug A.
//
// Mirrors column-height-007: inner mc with column-height:100px, column-wrap,
// row-gap:10px nested in outer mc (height:150px, 2 cols, auto). Inner mc
// with 360px content placed in outer col-1 (75px space). Resumed in outer
// col-2 (75px space). On resume, consumedRowBlockSize=75, remaining row
// height is only 25px (100-75). Phase 18 must NOT fire again — the remaining
// row fits in the outer space (outerAvailable=75 > remainingRowHeight=25).
//
// Contract: second outer column produces NO outgoing multicolData (no
// ConsumedRowBlockSize carrier) because the row fits entirely in the second
// outer column's space.
func TestColumnHeight_PhaseA_ConsumedRowBlockSizeNoSpuriousFire(t *testing.T) {
	t.Run("column-height-007", func(t *testing.T) {
		child := makeNode("div")
		innerMC := makeNode("div", child)

		styles := map[*html.Node]*css.Style{
			innerMC: makeStyle(
				"display", "block",
				"column-count", "2",
				"column-fill", "auto",
				"column-height", "100px",
				"column-wrap", "wrap",
				"row-gap", "10px",
				"column-gap", "0",
			),
			child: makeStyle(
				"display", "block",
				"height", "360px",
				"background", "green",
			),
		}

		ctx := testContext()
		wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
		innerNode := buildTestTree(innerMC, styles)

		// First outer column: 75px fragmentainer (outer mc height:150px / 2 cols).
		// Produces a break token with ConsumedRowBlockSize = 75 (row height 100,
		// placed 75 — the row was cut short by the outer fragmentainer boundary).
		space1 := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 75}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: 75}).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(75).
			SetFragmentainerOffset(0).
			SetBlockFragmentationType(FragmentColumn).
			Build()

		result1 := NewMulticolLayoutAlgorithm(ctx, innerNode, space1).Layout()
		if result1 == nil || result1.Fragment == nil {
			t.Fatal("first outer col layout returned nil")
		}
		if result1.BreakToken == nil {
			t.Fatal("expected break token from first outer col")
		}
		bt1 := result1.BreakToken
		if bt1.MulticolData == nil {
			t.Fatal("expected MulticolData on break token (Phase 18)")
		}
		// The carrier should be 75 (outerAvailable=75, rowHeight=100, painted=75).
		if got := bt1.MulticolData.ConsumedRowBlockSize; got != 75 {
			t.Errorf("first outer col: ConsumedRowBlockSize = %v, want 75", got)
		}

		// Second outer column: resume with break token from col-1.
		// consumedRowBlockSize=75; remaining row height = 100-75 = 25.
		// outerAvailable = 75. 25 < 75, so Phase 18 must NOT fire.
		space2 := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 75}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: 75}).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(75).
			SetFragmentainerOffset(0).
			SetBlockFragmentationType(FragmentColumn).
			SetBreakToken(bt1).
			Build()

		result2 := NewMulticolLayoutAlgorithm(ctx, innerNode, space2).Layout()
		if result2 == nil || result2.Fragment == nil {
			t.Fatal("second outer col layout returned nil")
		}

		// KEY CONTRACT: the second outer column must not emit a spurious
		// ConsumedRowBlockSize carrier. If it does, the third outer col would
		// start at the wrong row offset (150 consumed vs the correct 100).
		if result2.BreakToken != nil && result2.BreakToken.MulticolData != nil {
			t.Errorf("Bug A: second outer col spuriously emitted ConsumedRowBlockSize=%v, want none",
				result2.BreakToken.MulticolData.ConsumedRowBlockSize)
		}
	})
}

// TestColumnHeight_PhaseB_SpannerSnapOnlyWhenDoesNotFit tests the spanner row-snap
// behavior with explicit column-height.
//
// column-height:20px, column-wrap, row-gap:10px, columns:2, rowStride=30.
// Layout: 10px content + 5px spanner + 20px content + 5px spanner.
//
// Trace with column-height=20, row-gap=10 (stride=30):
//
//	Row 0 (y=0–20): content1(10px). colBlockSize=20 (full row). Balanced to 5px/col.
//	  rowBlockAdvance=20. blockCursor=20. Spanner1 detected.
//	Spanner1(5px) pre-snap: offsetInCurrentRow(20)=20, remaining=0, 5>0 → snap.
//	  offsetToNextRow(20)=0+10=10. blockCursor=30. Spanner1 at y=30–34.
//	  blockCursor=35. isFirstRow reset to true.
//	Row 1 (content2): lineOffset=35. colBlockSize=remainingRowHeight(35)=20-5=15.
//	  content2=20px. col1 gets 15px, col2 resumes 5px then detects spanner2.
//	  rowBlockAdvance=15. blockCursor=50. Spanner2 detected.
//	Spanner2(5px) pre-snap: offsetInCurrentRow(50)=Mod(50,30)=20, remaining=0.
//	  5>0 → snap fires. offsetToNextRow(50)=0+10=10. blockCursor=60.
//	  Spanner2 at y=60 (correct: no remaining row space, snap to next row boundary).
//
// Contract: spanner1 at y=30, spanner2 at y=60.
func TestColumnHeight_PhaseB_SpannerSnapOnlyWhenDoesNotFit(t *testing.T) {
	t.Run("column-height-013", func(t *testing.T) {
		content1 := makeNode("div")
		spanner1 := makeNode("div")
		content2 := makeNode("div")
		spanner2 := makeNode("div")
		mc := makeNode("div", content1, spanner1, content2, spanner2)

		styles := map[*html.Node]*css.Style{
			mc: makeStyle(
				"display", "block",
				"background", "green",
				"column-count", "2",
				"column-height", "20px",
				"column-wrap", "wrap",
				"row-gap", "10px",
				"column-gap", "0",
			),
			// content1: 10px → 5px/col balanced in 20px row
			content1: makeStyle("display", "block", "height", "10px"),
			// spanner1 lands at y=30 (remaining=0 at y=20, snap correct)
			spanner1: makeStyle("display", "block", "height", "5px", "column-span", "all"),
			// content2: 20px in colBlockSize=15 → col1 15px, col2 5px + spanner2
			content2: makeStyle("display", "block", "height", "20px"),
			// spanner2: 5px at y=60 (remaining=0 at y=50, snap correct)
			spanner2: makeStyle("display", "block", "height", "5px", "column-span", "all"),
		}

		ctx := testContext()
		wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
		mcNode := buildTestTree(mc, styles)

		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			Build()

		result := NewMulticolLayoutAlgorithm(ctx, mcNode, space).Layout()
		if result == nil || result.Fragment == nil {
			t.Fatal("layout returned nil")
		}

		// Collect non-column children (spanners) in order.
		var spannerOffsets []float64
		for _, ch := range result.Fragment.Children {
			if ch.Fragment.BoxType == BoxTypeColumn {
				continue
			}
			spannerOffsets = append(spannerOffsets, ch.Offset.Top.Float64())
		}

		if len(spannerOffsets) < 2 {
			t.Fatalf("expected 2 spanner fragments, got %d (children: %d total)",
				len(spannerOffsets), len(result.Fragment.Children))
		}

		// spanner1: remaining=0 at y=20, snap fires → y=30.
		if spannerOffsets[0] != 30 {
			t.Errorf("spanner1 at y=%v, want y=30", spannerOffsets[0])
		}

		// spanner2: col1 consumes 15px → blockCursor=50, remaining=0, snap fires → y=60.
		if spannerOffsets[1] != 60 {
			t.Errorf("spanner2 at y=%v, want y=60", spannerOffsets[1])
		}
	})
}

// TestColumnHeight_PhaseC_RowWrapMainGapRecorded tests Bug C.
//
// Mirrors column-height-009: column-height:40px, column-wrap, 3 columns,
// gap:20px (column-rule:2px solid). With 16 lines × 20px = 320px content,
// the layout wraps across multiple rows. Each row-to-row boundary must
// produce a MainGap entry in GapGeometry so column rules are painted per-row
// (not spanning the full block height across rows).
//
// Contract: after layout, GapGeometry.MainGaps must contain at least one
// entry between the first and last column rows (one per row boundary).
func TestColumnHeight_PhaseC_RowWrapMainGapRecorded(t *testing.T) {
	t.Run("column-height-009", func(t *testing.T) {
		child := makeNode("div")
		mc := makeNode("div", child)

		styles := map[*html.Node]*css.Style{
			mc: makeStyle(
				"display", "block",
				"width", "340px",
				"column-count", "3",
				"column-height", "40px",
				"column-wrap", "wrap",
				"row-gap", "20px",
				"column-gap", "20px",
				"column-rule-style", "solid",
				"column-rule-width", "2px",
				"column-rule-color", "black",
			),
			child: makeStyle(
				"display", "block",
				"width", "50px",
				// 16 lines × 20px = 320px total content
				"height", "320px",
			),
		}

		ctx := testContext()
		wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
		mcNode := buildTestTree(mc, styles)

		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 340, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 340, BlockSize: Indefinite}).
			Build()

		result := NewMulticolLayoutAlgorithm(ctx, mcNode, space).Layout()
		if result == nil || result.Fragment == nil {
			t.Fatal("layout returned nil")
		}

		gg := result.Fragment.GapGeometry
		if gg == nil {
			t.Fatal("GapGeometry is nil — column-rule produced no gap geometry")
		}

		// With 320px content, 3 columns, column-height:40px → 320/3 ≈ 107px per
		// column → ceil(107/40) = 3 rows minimum. Each wrap boundary must add a
		// MainGap entry (Blink cla.cc:1263-1268 has_wrapped path).
		// Bug C: MainGaps is empty (no row-wrap gap recorded).
		if len(gg.MainGaps) == 0 {
			t.Errorf("Bug C: GapGeometry.MainGaps is empty — row-wrap boundary gaps not recorded")
		}

		// Sanity: must have at least 2 main-gap entries (one per row boundary
		// for 3 rows = 2 boundaries).
		if len(gg.MainGaps) < 2 {
			t.Errorf("Bug C: expected >= 2 MainGap entries for 3-row wrap, got %d", len(gg.MainGaps))
		}
	})
}
