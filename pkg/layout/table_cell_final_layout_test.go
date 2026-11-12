package layout

import (
	"strings"
	"testing"
)

// A table cell is laid out a second time at its final block size, the sum
// of its rows (ComputeCellBlockSize, table/table_layout_utils.cc:1333-1361 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), in the space
// SetupTableCellConstraintSpaceBuilder builds (:1363-1425). That size is the
// cell's definite initial block size, which its children's percentage
// block sizes resolve against, when the cell is constrained (a specified
// block size) or it grew in a table with a specified block size;
// otherwise the initial block size is the intrinsic one and percentages
// stay auto.
func TestTableCellFinalLayout(t *testing.T) {
	cases := []struct {
		name, html string
		wantH      float64
	}{
		// Constrained cell (height:100%) in a 100px table: 40% is 40px
		// (css-sizing/intrinsic-percent-replaced-018).
		{"constrained cell", `<table style="height:100px;border-spacing:0"><tr><td style="height:100%;padding:0">` +
			`<div id="c" style="width:10px;height:40%"></div></td></tr></table>`, 40},
		// Unconstrained cell that grew in a table with a specified height:
		// its height:100% child is the row's 300px.
		{"grown cell, specified table height", `<table style="height:300px;border-spacing:0"><tr>` +
			`<td style="padding:0"><div id="c" style="height:100%"></div></td></tr></table>`, 300},
		// The same in an anonymous cell, whose lone block child is laid out
		// inside it (css-sizing/table-child-percentage-height-with-border-box).
		{"grown anonymous cell, specified table height", `<div style="height:300px"><div style="display:table;height:100%;width:100px">` +
			`<div style="display:table-row"><div id="c" style="height:100%"></div></div></div></div>`, 300},
		// A replaced child of a fixed-height cell resolves against the
		// cell's own 50px, not the 150px row it grew to: 200% is 100px
		// (CalculateReplacedChildPercentageSize, length_utils.cc:1830-1867;
		// css-tables/percent-height-replaced-in-percent-cell-003).
		{"replaced child of a fixed-height cell", `<table style="border-spacing:0"><tr>` +
			`<td style="padding:0;height:50px"><canvas id="c" width=1 height=1 style="display:block;height:200%"></canvas></td>` +
			`<td style="padding:0"><div style="height:150px"></div></td></tr></table>`, 100},
		// The row's size is fixed for the cell: its max-height does not clamp
		// it (ComputeBlockSizeForFragment, length_utils.cc:906-916;
		// css-tables/table-cell-overflow-explicit-height-002).
		{"max-height cell in a taller row", `<table style="border-spacing:0"><tr>` +
			`<td id="c" style="padding:0;height:20px;max-height:20px"><div style="height:100px"></div></td></tr></table>`, 100},
		// A percentage cell height does not constrain the cell (only a
		// fixed one does, CellBlockConstraint::is_constrained,
		// table/table_layout_utils.cc:248-253), so in an auto-height table
		// its 50% child stays auto.
		{"percentage-height cell", `<table style="border-spacing:0"><tr>` +
			`<td style="padding:0;height:50%"><div id="c" style="height:50%"></div></td>` +
			`<td style="padding:0"><div style="height:100px"></div></td></tr></table>`, 0},
		// The replaced percentage base of a fixed-height cell is its block
		// size clamped by its max-block-size: 20px, not 40px
		// (ComputeBlockSizeForFragmentInternal in
		// CalculateReplacedChildPercentageSize, length_utils.cc:1851-1858).
		{"replaced child of a max-height cell", `<table style="border-spacing:0"><tr>` +
			`<td style="padding:0;height:40px;max-height:20px"><canvas id="c" width=1 height=1 style="display:block;height:100%"></canvas></td>` +
			`</tr></table>`, 20},
		// Guard: the cell grew but the table's height is auto, so the
		// initial block size stays indefinite and the 100% child is auto.
		{"grown cell, auto table height", `<table style="border-spacing:0"><tr>` +
			`<td style="padding:0"><div style="height:50px"></div></td>` +
			`<td style="padding:0"><div id="c" style="height:100%"></div></td></tr></table>`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+c.html), "c")
			if b == nil {
				t.Fatal("no #c box")
			}
			if b.Height != c.wantH {
				t.Errorf("#c height = %v, want %v", b.Height, c.wantH)
			}
		})
	}

	// Guard: vertical-align:middle centers the content in the final row
	// block size (FinalizeTableCellLayout, table_layout_utils.cc:1575-1641).
	// Unlike TestTableCell_ExplicitHeightVerticalAlign, the cell has no
	// height of its own: the row grows to the table's 100px.
	t.Run("middle in a grown row", func(t *testing.T) {
		src := `<!DOCTYPE html><body style="margin:0"><table style="height:100px;border-spacing:0"><tr>` +
			`<td style="padding:0;vertical-align:middle"><div id="c" style="width:10px;height:20px"></div></td></tr></table>`
		b := findBoxByID(layoutRootTestHTML(t, src), "c")
		if b == nil {
			t.Fatal("no #c box")
		}
		if b.Y != 40 {
			t.Errorf("#c y = %v, want 40 (centered in the 100px row)", b.Y)
		}
	})
}

// Laying a cell out again at its final size must not repeat work that
// cannot change, as Blink's layout cache keeps it from doing
// (BlockNode::Layout's CachedLayoutResult, layout_result_caching.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): each level of nested tables
// adds the same number of layout-algorithm runs. Relaying every level's
// cells makes the count exponential in the depth (4x per level: 77, 317,
// 1277 runs at depths 4, 6, 8, against 17, 25, 33). Counting runs, not
// timing them, keeps the check independent of machine load.
func TestTableCellFinalLayoutNestedTablesStayLinear(t *testing.T) {
	runs := func(depth int) int64 {
		src := `<!DOCTYPE html><body>` + strings.Repeat(`<table><tr><td>a</td><td>`, depth) + "x" + strings.Repeat(`</td></tr></table>`, depth)
		before := layoutAlgorithmRuns.Load()
		layoutRootTestHTML(t, src)
		return layoutAlgorithmRuns.Load() - before
	}
	r4, r8, r12 := runs(4), runs(8), runs(12)
	if r12-r8 != r8-r4 {
		t.Errorf("layout runs at depths 4, 8, 12 = %d, %d, %d; want equal steps (linear in the depth)", r4, r8, r12)
	}
}
