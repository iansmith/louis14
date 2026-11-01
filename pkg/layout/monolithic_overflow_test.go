package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
)

// TestShouldCloneBlockStartBorderPaddingAfterMonolithicOverflow_LOU420: a
// fragment resumed after monolithic overflow does not clone its block-start
// border/padding when the fragmentainer space left is less than the box's
// block border+padding. Mirrors Blink's ShouldCloneBlockStartBorderPadding
// (fragmentation_utils.cc:70-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Edges are 5+5; the
// fragmentainer is 100 tall.
func TestShouldCloneBlockStartBorderPaddingAfterMonolithicOverflow_LOU420(t *testing.T) {
	for _, tc := range []struct {
		name               string
		monolithicOverflow float64
		fragmentainerOff   float64
		want               bool
	}{
		{"monolithic overflow, space left 5 < 10: no clone", 50, 95, false},
		{"monolithic overflow, space left 10 = 10: clone", 50, 90, true},
		{"monolithic overflow, space left 20 > 10: clone", 50, 80, true},
		{"no monolithic overflow, space left 5: clone", 0, 95, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			space := ConstraintSpace{
				HasBlockFragmentation:  true,
				BlockFragmentationType: FragmentPage,
				FragmentainerBlockSize: 100,
				FragmentainerOffset:    tc.fragmentainerOff,
				BreakToken: &BlockBreakToken{
					ConsumedBlockSize:  layoutunit.FromFloat64Round(40),
					MonolithicOverflow: tc.monolithicOverflow,
				},
			}
			builder := newFragmentationTestBuilder(&space, LogicalEdges{BlockStart: 5, BlockEnd: 5}, true)
			if got := ShouldCloneBlockStartBorderPadding(builder); got != tc.want {
				t.Errorf("ShouldCloneBlockStartBorderPadding = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFinishFragmentationMonolithicOverflow_LOU485: FinishFragmentation's
// monolithic-overflow terms (fragmentation_utils.cc:572-587, :704-716 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A box pushed wholly past the
// incoming overflow (zero fragment block-size) reserves what the next
// fragmentainer must still steer clear of, and so breaks inside; with
// incoming overflow there is always a last-resort breakpoint before the
// trailing border/padding. Sizes are border-box, trailing border/padding 10,
// the fragmentainer 100. Louis14 has no page fragmentation, so tokens
// carrying overflow are hand-built.
func TestFinishFragmentationMonolithicOverflow_LOU485(t *testing.T) {
	pushed := func(overflow float64) *BlockBreakToken {
		return &BlockBreakToken{ConsumedBlockSize: layoutunit.FromFloat64Round(40), MonolithicOverflow: overflow}
	}
	base := FinishFragmentationInput{
		HasBlockFragmentation:   true,
		FragmentainerCapacity:   100,
		TrailingBorderPadding:   10,
		BlockStartBorderPadding: 10,
	}
	for _, tc := range []struct {
		name string
		in   func(FinishFragmentationInput) FinishFragmentationInput
		want FinishFragmentationResult
	}{
		{"overflow beyond this fragmentainer: reserve the rest, consume the fragmentainer", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize, in.PreviousBreakToken = 100, 40, 0, pushed(250)
			return in
		}, FinishFragmentationResult{ConsumedBlockSize: 140, IsAtBlockEnd: true, IncludeBlockEnd: true, MonolithicOverflow: 150}},
		{"overflow ending in this fragmentainer: nothing reserved", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize, in.PreviousBreakToken = 100, 40, 0, pushed(80)
			return in
		}, FinishFragmentationResult{ConsumedBlockSize: 40, IsAtBlockEnd: true, IncludeBlockEnd: true}},
		{"incoming overflow: last-resort breakpoint before the trailing edge", func(in FinishFragmentationInput) FinishFragmentationInput {
			in.SpaceLeft, in.FragmentsTotalBlockSize, in.IntrinsicBlockSize, in.PreviousBreakToken = 5, 50, 10, pushed(50)
			return in
		}, FinishFragmentationResult{IntrinsicBlockSize: 5, ConsumedBlockSize: 40, DidBreakSelf: true, ClampAppealToLastResort: true}},
	} {
		if got := FinishFragmentation(tc.in(base)); got != tc.want {
			t.Errorf("%s:\n got  %+v\n want %+v", tc.name, got, tc.want)
		}
	}
}

// TestFragmentainerMonolithicOverflow_LOU485: a fragmentainer resumed after
// monolithic overflow reserves what is left of it beyond its own capacity
// (FinishFragmentationForFragmentainer, fragmentation_utils.cc:844-852 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so it breaks and its token
// carries the remainder (block_break_token.cc:77); once the overflow ends
// within the fragmentainer no token is needed. The column is empty.
func TestFragmentainerMonolithicOverflow_LOU485(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	root := makeNode("div")
	rootNode := buildTestTree(root, map[*html.Node]*css.Style{root: makeStyle("display", "block")})
	layoutColumn := func(overflow float64) *LayoutResult {
		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 100}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetHasBlockFragmentation(true).
			SetBlockFragmentationType(FragmentColumn).
			SetFragmentainerBlockSize(100).
			SetFragmentainerOffset(0).
			SetBreakToken(&BlockBreakToken{
				Node:               rootNode,
				ConsumedBlockSize:  layoutunit.FromFloat64Round(100),
				SequenceNumber:     0,
				HasSeenAllChildren: true,
				MonolithicOverflow: overflow,
			}).
			Build()
		bla := NewBlockLayoutAlgorithm(testContext(), rootNode, space)
		bla.SetBoxType(BoxTypeColumn)
		return bla.Layout()
	}
	res := layoutColumn(250)
	if res.BreakToken == nil {
		t.Fatal("column pushed by 250px of overflow: no break token, want one carrying 150")
	}
	if got := res.BreakToken.MonolithicOverflow; got != 150 {
		t.Errorf("outgoing MonolithicOverflow = %v, want 150", got)
	}
	if got := res.BreakToken.ConsumedBlockSize.Float64(); got != 200 {
		t.Errorf("outgoing ConsumedBlockSize = %v, want 200", got)
	}
	if res := layoutColumn(50); res.BreakToken != nil {
		t.Errorf("column pushed by 50px of overflow: break token %+v, want none", res.BreakToken)
	}
}

// TestResumedChildPushedPastMonolithicOverflow_LOU485: the outermost
// container with monolithic overflow recorded in its break token is pushed
// past it in its parent (BlockLayoutAlgorithm::ComputeChildData,
// block_layout_algorithm.cc:2848-2855 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a child whose parent token
// records the overflow too is not pushed again.
func TestResumedChildPushedPastMonolithicOverflow_LOU485(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	for _, tc := range []struct {
		name           string
		parentOverflow float64
		want           float64
	}{
		{"outermost container with overflow is pushed", 0, 30},
		{"parent records the overflow: not pushed again", 30, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := makeNode("div")
			root := makeNode("div", child)
			rootNode := buildTestTree(root, map[*html.Node]*css.Style{
				root:  makeStyle("display", "block"),
				child: makeStyle("display", "block", "height", "20px"),
			})
			childNode := rootNode.children[0]
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetHasBlockFragmentation(true).
				SetBlockFragmentationType(FragmentColumn).
				SetFragmentainerBlockSize(100).
				SetFragmentainerOffset(0).
				SetBreakToken(&BlockBreakToken{
					Node:               rootNode,
					ConsumedBlockSize:  layoutunit.FromFloat64Round(100),
					MonolithicOverflow: tc.parentOverflow,
					ChildBreakTokens: []*BlockBreakToken{{
						Node:               childNode,
						ConsumedBlockSize:  layoutunit.FromFloat64Round(0),
						MonolithicOverflow: 30,
					}},
				}).
				Build()
			res := NewBlockLayoutAlgorithm(testContext(), rootNode, space).Layout()
			if len(res.Fragment.Children) == 0 {
				t.Fatal("no child fragment")
			}
			if got := res.Fragment.Children[0].Offset.Top.Float64(); got != tc.want {
				t.Errorf("resumed child block offset = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMonolithicOverflowGivesContainerSeparation_LOU485: a container pushed
// by monolithic overflow behaves as if there were a valid breakpoint before
// its first child (block_layout_algorithm.cc:868-876 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a forced break before that
// child takes effect; without overflow the first child has no container
// separation and the forced break is ignored.
func TestMonolithicOverflowGivesContainerSeparation_LOU485(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	for _, tc := range []struct {
		name      string
		overflow  float64
		wantBreak bool
	}{
		{"pushed by overflow: forced break before the first child", 30, true},
		{"no overflow: first child has no container separation", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := makeNode("div")
			root := makeNode("div", child)
			rootNode := buildTestTree(root, map[*html.Node]*css.Style{
				root:  makeStyle("display", "block"),
				child: makeStyle("display", "block", "height", "20px", "break-before", "column"),
			})
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetHasBlockFragmentation(true).
				SetBlockFragmentationType(FragmentColumn).
				SetFragmentainerBlockSize(100).
				SetFragmentainerOffset(0).
				SetBreakToken(&BlockBreakToken{
					Node:               rootNode,
					ConsumedBlockSize:  layoutunit.FromFloat64Round(100),
					MonolithicOverflow: tc.overflow,
				}).
				Build()
			res := NewBlockLayoutAlgorithm(testContext(), rootNode, space).Layout()
			gotBreak := res.BreakToken != nil && len(res.BreakToken.ChildBreakTokens) == 1 &&
				res.BreakToken.ChildBreakTokens[0].IsBreakBefore
			if gotBreak != tc.wantBreak {
				t.Errorf("forced break before the first child = %v, want %v (token %+v)", gotBreak, tc.wantBreak, res.BreakToken)
			}
		})
	}
}
