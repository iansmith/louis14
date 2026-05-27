package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
	"testing"
)

// TestBlockLayout_OOFStaticPositionStitchedAcrossCBFragments verifies that
// an OOF candidate's static block-offset captured by a continuation BLA pass
// is expressed in CB-STITCHED coords (cumulative across CB fragments), not
// in fragment-local coords. This is the invariant relied on by the outer
// multicol's OOF-fragmentation drain — `column-balancing-with-span-and-oof-002`
// (C38) regressed because the captured value was fragment-local (`50` for
// col 1's pass), causing the drain to misplace the OOF inside the CB's first
// fragment instead of past it.
//
// Setup: a positioned CB containing a 50×30 in-flow div followed by an
// abspos. The CB has already consumed 50px in a previous fragment
// (`ConsumedBlockSize = 50`). The in-flow div's column-local position is
// y=0 (col-local), block-size 30, leaving the abspos at col-local y=30.
// In CB-STITCHED coords the abspos sits at y=80 (50 consumed + 30 in-flow).
//
// Mirrors Blink's static-position capture in `BlockLayoutAlgorithm` —
// the static position is recorded in stitched-CB coords so deferred OOF
// resolution at the outer fragmentation root finds the OOF in the right
// column fragmentainer.
func TestBlockLayout_OOFStaticPositionStitchedAcrossCBFragments(t *testing.T) {
	abspos := makeNode("div")
	inflow := makeNode("div")
	cb := makeNode("div", inflow, abspos)

	styles := map[*html.Node]*css.Style{
		cb: makeStyle("position", "relative", "display", "block"),
		// In-flow div is small enough to fit in the 50px col-local space
		// so the BLA loop continues to the abspos in the same pass.
		inflow: makeStyle("height", "30px", "display", "block", "width", "50px"),
		abspos: makeStyle("position", "absolute", "width", "50px", "height", "20px", "display", "block"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(cb, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// Simulate a continuation pass: the CB has already consumed 50px in
	// a previous fragment (col 0). Resume with a parent break token whose
	// ConsumedBlockSize = 50.
	consumed := layoutunit.FromFloat64Round(50)
	resumeToken := &BlockBreakToken{
		Node:              layoutRoot,
		ConsumedBlockSize: consumed,
	}

	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 50, BlockSize: 50}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 50, BlockSize: 50}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(50).
		SetFragmentainerOffset(0).
		SetBlockFragmentationType(FragmentColumn).
		SetIsInsideBalancedColumns(true).
		SetIsFixedBlockSize(true).
		SetBreakToken(resumeToken).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil result")
	}

	// The abspos must have been promoted to a fragmentainer descendant on
	// the outgoing fragment's FragmentedOofData (CB is positioned + this
	// BLA is inside a block-fragmentation context).
	if result.Fragment.FragmentedOofData == nil {
		t.Fatalf("expected FragmentedOofData with the abspos descendant; got nil")
	}
	descs := result.Fragment.FragmentedOofData.OofPositionedFragmentainerDescendants
	if len(descs) != 1 {
		t.Fatalf("expected 1 OOF fragmentainer descendant; got %d", len(descs))
	}
	gotBlock := descs[0].Candidate.StaticPosition.Offset.BlockOffset
	// Stitched-CB y for the abspos = consumed (50) + col-local blockCursor
	// after the in-flow (30) = 80. Without the C38 stitched-coords fix the
	// value would be 30 (col-local only), which the drain would misinterpret
	// as a position inside the FIRST CB fragment instead of past it.
	wantBlock := 80.0
	if gotBlock != wantBlock {
		t.Errorf("abspos StaticPosition.BlockOffset: got %v, want %v (CB-stitched coords)",
			gotBlock, wantBlock)
	}
}
