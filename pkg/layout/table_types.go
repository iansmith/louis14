package layout

// Types for single-pass table sizing, mirroring Blink's
// core/layout/table/table_layout_algorithm_types.h and
// core/layout/table/table_constraint_space_data.h.
//
// These types are introduced in checkpoint 1 of the table-layout refactor
// (see docs/PROMPT-table-blink-alignment.md). In this checkpoint the types
// exist but are not yet wired into the layout algorithm — the current
// build-then-mutate flow in TableLayoutAlgorithm.Layout() continues to run
// unchanged. Checkpoint 2 adds a computeRows + distributeTableBlockSize
// pipeline guarded by a feature flag, and checkpoint 3 flips the flag and
// deletes the legacy path.
//
// Blink uses LayoutUnit for sizes; louis14 uses plain float64 throughout
// (see LogicalSize, Box, ConstraintSpace). No custom LayoutUnit type.

// kTableMaxInlineSize mirrors Blink's TableTypes::kTableMaxInlineSize
// (table_layout_algorithm_types.h:26). It is the "effectively infinite"
// sentinel used as the max-content inline-size for tables with
// table-layout:fixed + percentage width, so ancestors doing max-content
// sizing (flex basis, shrink-to-fit) see the table as unbounded.
const kTableMaxInlineSize = 1_000_000

// TableTypesRow mirrors Blink's TableTypes::Row. Each Row represents the
// finalized intrinsic + distributed block metrics for a single table row
// so that, once computed, fragment construction reads sizes directly
// rather than mutating already-appended fragments (as the legacy path
// does via rowInfos.childIdx/blockOffset).
//
// Field names mirror Blink's struct verbatim where possible.
type TableTypesRow struct {
	// BlockSize is the row's finalized block-size after intrinsic sizing
	// plus any distribution. In the legacy path this is rowInfos[i].height;
	// in the single-pass path this is the value computed by computeRows
	// and then mutated in place by distributeTableBlockSizeToRows.
	BlockSize float64

	// StartCellIndex / CellCount locate this row's cells inside the
	// section's flat cell vector. Mirrors Blink's
	// TableTypes::Row::start_cell_index / cell_count (used by
	// GenerateFragment to slice the section's cell list).
	StartCellIndex int
	CellCount      int

	// Baseline is the row's first-baseline offset from the row's
	// block-start edge. Used for vertical-align:baseline cells per
	// CSS 2.1 §17.5.4.
	Baseline float64

	// Percent is the row's percentage block-size in [0, 100], or -1
	// when the row has no percentage height. Blink stores this as an
	// optional Percent; we use -1 as the absent sentinel to stay
	// allocation-free.
	Percent float64

	// IsConstrained is true if the row has an explicit (non-auto)
	// block-size — set by the cascade (row's own height) or implied
	// by any cell having an explicit height. Constrained rows are
	// excluded from the "unconstrained non-empty rows" bucket in the
	// 5-priority distribution (DistributeExcessBlockSizeToRows).
	IsConstrained bool

	// HasRowspanStart is true if any cell in this row has rowspan > 1
	// and originates here. Used by distribution step 2 to prioritize
	// rows that are "seeds" of row-spanning cells.
	HasRowspanStart bool

	// IsCollapsed mirrors CSS 2.1 §17.6.3 visibility:collapse on a row:
	// the row's block-size collapses to zero and its cells are elided
	// from layout. Row still occupies its index for indexing purposes.
	IsCollapsed bool
}

// TableTypesRows is the row vector for one section (or the whole table
// in the simple single-section case). Mirrors Blink's
// `using Rows = Vector<Row>`.
type TableTypesRows []TableTypesRow

// TableTypesSection mirrors Blink's TableTypes::Section. A section is
// one <thead>, <tbody>, <tfoot>, or anonymous row-group. The fields
// cover the subset louis14 needs today; more can be added as the
// refactor fills in rowspan / percentage-section handling.
type TableTypesSection struct {
	// StartRowIndex / RowCount locate this section's rows inside the
	// table's flat row vector (TableConstraintSpaceData.Rows).
	StartRowIndex int
	RowCount      int

	// BlockSize is the section's finalized block-size (sum of its row
	// block-sizes plus any per-section intrinsic contribution).
	BlockSize float64

	// Percent is the section's percentage block-size or -1 if absent.
	Percent float64

	// IsConstrained mirrors TableTypesRow.IsConstrained for the section.
	IsConstrained bool

	// IsTableBlockSizeDistributed is true after
	// distributeTableBlockSizeToSections has run on this section — a
	// sanity-check flag for the single-pass path.
	IsTableBlockSizeDistributed bool
}

// TableConstraintSpaceData is the immutable, single-pass sizing output
// threaded from the table algorithm into its section/row sub-spaces.
// Mirrors Blink's TableConstraintSpaceData (ref-counted there; in Go we
// pass a *TableConstraintSpaceData and rely on callers not mutating it
// after publication — distributeTableBlockSize produces one Data object
// which is then read-only during fragment generation).
//
// In checkpoint 1 this struct is a type-level placeholder: no code
// constructs or threads it yet. Checkpoints 2 and 3 wire it through.
type TableConstraintSpaceData struct {
	// Sections in render order (thead → tbody(s) → tfoot).
	Sections []TableTypesSection

	// Rows flattened across sections in the same order as Sections.
	// A given section's rows are Rows[S.StartRowIndex : S.StartRowIndex+S.RowCount].
	Rows TableTypesRows

	// ColumnInlineSizes is the finalized column inline-size vector
	// (CSS 2.1 §17.5.2, already computed in the auto/fixed column-width
	// pass today). Stored here so cell sub-spaces can read it without
	// re-traversing the column/colgroup list.
	ColumnInlineSizes []float64
}

// TableTypesRowAbsentPercent is the sentinel value meaning "row has no
// percentage block-size". Matches Blink's optional-but-absent semantics
// without introducing a pointer or wrapper type.
const TableTypesRowAbsentPercent float64 = -1
