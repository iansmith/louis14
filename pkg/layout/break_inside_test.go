package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
	"testing"
)

// TestIsBreakInside_BreakBeforeSizesFresh is the LOU-370 sub-item (a) unit
// test. A definite-height block resumed under a break-BEFORE token has never
// started laying out in a previous fragmentainer, so its fragment in this
// fragmentainer must size to its full explicit height — NOT be shrunk by the
// resumed-fragment sizing branches (which subtract consumed size / substitute
// intrinsic content). Those branches must gate on break-INSIDE only.
//
// Blink: resumed-fragment sizing (consumed-size subtraction, intrinsic
// substitution) applies only under IsBreakInside(token) =
// `token && !token->IsBreakBefore() && !token->IsRepeated()`
// (fragmentation_utils.h:63-67 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// Bug (pre-fix): block_layout.go's resumed-sizing branch fires on bare
// `incomingBreakToken != nil`. A break-before token (ConsumedBlockSize=0,
// intrinsic content < explicit height) wrongly selects the
// `finalBlockSize = intrinsicBlockSize` branch and shrinks the fragment to
// the intrinsic content height instead of the declared 100px.
func TestIsBreakInside_BreakBeforeSizesFresh(t *testing.T) {
	// A definite-height 100px block whose only child is a 40px block, so the
	// intrinsic content (40px) is shorter than the explicit height (100px).
	inner := makeNode("div")
	block := makeNode("div", inner)
	styles := map[*html.Node]*css.Style{
		block: makeStyle("display", "block", "width", "200px", "height", "100px"),
		inner: makeStyle("display", "block", "height", "40px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(block, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	// A break-BEFORE token: the block never started in a previous
	// fragmentainer (ConsumedBlockSize=0, IsBreakBefore=true).
	breakBefore := &BlockBreakToken{
		Node:              layoutRoot,
		ConsumedBlockSize: layoutunit.FromFloat64Round(0),
		IsBreakBefore:     true,
	}

	// A column fragmentation context (the branch that shrinks to intrinsic is
	// gated to FragmentColumn).
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(1000).
		SetBreakToken(breakBefore).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil")
	}

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("break-before block-size = %v, want 100 "+
			"(a break-before child never started — it must size fresh to its "+
			"declared height, not shrink to the 40px intrinsic content)", got)
	}
}

// TestIsBreakInside_HelperSemantics locks the helper's truth table against
// Blink fragmentation_utils.h:63-67 (louis14 has no repeated-content
// fragmentation, so the IsRepeated clause is absent).
func TestIsBreakInside_HelperSemantics(t *testing.T) {
	cases := []struct {
		name  string
		token *BlockBreakToken
		want  bool
	}{
		{"nil token", nil, false},
		{"break-before", &BlockBreakToken{IsBreakBefore: true}, false},
		{"break-inside", &BlockBreakToken{IsBreakBefore: false}, true},
	}
	for _, tc := range cases {
		if got := tc.token.IsBreakInside(); got != tc.want {
			t.Errorf("%s: IsBreakInside() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
