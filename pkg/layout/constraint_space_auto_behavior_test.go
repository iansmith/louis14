package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-490 / LOU-504 item 1 (worklist D5): the auto-size behaviour and the
// anonymous bit of a constraint space, as Blink's ConstraintSpace carries
// them (constraint_space.h:401, :463-474, :1619-1621 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// The builder starts both auto behaviours at kFitContent; every stretching
// parent sets them (constraint_space.h:1619-1621 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestConstraintSpaceBuilder_DefaultAutoBehaviorIsFitContent_LOU504(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	cs := NewConstraintSpaceBuilder(wdm, wdm, false).Build()
	if cs.InlineAutoBehavior != AutoSizeBehaviorFitContent {
		t.Errorf("InlineAutoBehavior = %v, want fit-content", cs.InlineAutoBehavior)
	}
	if cs.BlockAutoBehavior != AutoSizeBehaviorFitContent {
		t.Errorf("BlockAutoBehavior = %v, want fit-content", cs.BlockAutoBehavior)
	}
	if cs.IsAnonymous {
		t.Error("IsAnonymous defaults to false")
	}
}

// For an orthogonal child the parent's inline axis is the child's block axis,
// so SetInlineAutoBehavior writes BlockAutoBehavior and vice versa
// (constraint_space_builder.h:272-290 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestConstraintSpaceBuilder_AutoBehaviorAxisSwap_LOU504(t *testing.T) {
	htb := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	vrl := WritingDirectionMode{WritingModeVerticalRL, DirectionLTR}

	par := NewConstraintSpaceBuilder(htb, htb, false).
		SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
		SetBlockAutoBehavior(AutoSizeBehaviorStretchExplicit).
		Build()
	if par.InlineAutoBehavior != AutoSizeBehaviorStretchImplicit || par.BlockAutoBehavior != AutoSizeBehaviorStretchExplicit {
		t.Errorf("parallel: inline %v block %v, want stretch-implicit / stretch-explicit",
			par.InlineAutoBehavior, par.BlockAutoBehavior)
	}

	orth := NewConstraintSpaceBuilder(htb, vrl, false).
		SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
		SetBlockAutoBehavior(AutoSizeBehaviorStretchExplicit).
		Build()
	if orth.InlineAutoBehavior != AutoSizeBehaviorStretchExplicit || orth.BlockAutoBehavior != AutoSizeBehaviorStretchImplicit {
		t.Errorf("orthogonal: inline %v block %v, want stretch-explicit / stretch-implicit",
			orth.InlineAutoBehavior, orth.BlockAutoBehavior)
	}
}

// A column fragmentainer's space is anonymous
// (CreateConstraintSpaceForFragmentainer, fragmentation_utils.cc:1393 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a spanner's is not.
func TestColumnSpaceIsAnonymous_LOU490(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	sp := makeNode("div")
	mc := makeNode("div", sp)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0"),
		sp: makeStyle("display", "block", "column-span", "all"),
	}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
		Build()
	mcNode := buildTestTree(mc, styles)
	mla := NewMulticolLayoutAlgorithm(testContext(), mcNode, space)
	mla.Layout()
	col := mla.createConstraintSpaceForColumn(wdm, 100, 50, Indefinite, nil, false, BreakAppealLastResort)
	if !col.IsAnonymous {
		t.Error("column space: IsAnonymous = false, want true")
	}
	if got := mla.CreateConstraintSpaceForSpanner(mcNode.Children()[0], 0, nil); got.IsAnonymous {
		t.Error("spanner space: IsAnonymous = true, want false")
	}
}

// fixedWidthChild returns a div holding one 80px-wide block child, so its
// max-content inline-size is 80px.
func fixedWidthChild(styles map[*html.Node]*css.Style, props ...string) (*html.Node, *LayoutInputNode) {
	kid := makeNode("div")
	styles[kid] = makeStyle("display", "block", "width", "80px", "height", "10px")
	box := makeNode("div", kid)
	styles[box] = makeStyle(props...)
	return box, buildTestTree(box, styles)
}

func initialInlineSize(node *LayoutInputNode, space ConstraintSpace) float64 {
	return CalculateInitialFragmentGeometry(testContext(), node, node.Style(), space.WritingDirection, space).BorderBoxSize.InlineSize
}

// An anonymous space's inline-size is its available inline-size, whatever
// the style says (ComputeInlineSizeForFragment, length_utils.cc:606-608 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestCalculateInitialFragmentGeometry_AnonymousSpaceTakesAvailableInline_LOU490(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	_, node := fixedWidthChild(map[*html.Node]*css.Style{}, "display", "block", "width", "50px")
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetIsAnonymous(true).
		Build()
	if got := initialInlineSize(node, space); got != 200 {
		t.Errorf("anonymous space inline-size = %v, want 200", got)
	}
}

// The auto inline-size follows the parent's space, not the child's style:
// a float- or inline-block-styled box, or a flex item, stretches in a space
// that says stretch, and shrinks to fit in one that does not
// (ComputeInlineSizeForFragmentInternal, length_utils.cc:556-569 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestCalculateInitialFragmentGeometry_AutoInlineSizeFollowsSpace_LOU504(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	for _, tc := range []struct {
		name     string
		props    []string
		flexItem bool
	}{
		{name: "float", props: []string{"display", "block", "float", "left"}},
		{name: "inline-block", props: []string{"display", "inline-block"}},
		{name: "abspos", props: []string{"display", "block", "position", "absolute"}},
		{name: "flex item", props: []string{"display", "block"}, flexItem: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, node := fixedWidthChild(map[*html.Node]*css.Style{}, tc.props...)
			for _, b := range []struct {
				behavior AutoSizeBehavior
				want     float64
			}{
				{AutoSizeBehaviorStretchImplicit, 200},
				{AutoSizeBehaviorFitContent, 80},
			} {
				space := NewConstraintSpaceBuilder(wdm, wdm, true).
					SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
					SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
					SetIsInsideFlexibleBox(tc.flexItem).
					SetInlineAutoBehavior(b.behavior).
					Build()
				if got := initialInlineSize(node, space); got != b.want {
					t.Errorf("behavior %v: inline-size = %v, want %v", b.behavior, got, b.want)
				}
			}
		})
	}
}

// may_apply_aspect_ratio (length_utils.cc:535-551 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an auto inline-size takes its
// size from the aspect-ratio and a definite block-size only when that
// block-size is not auto, or the space stretches the auto block-size
// explicitly. A block-size the parent merely fixes leaves a stretching
// auto inline-size stretched.
func TestCalculateInitialFragmentGeometry_MayApplyAspectRatio_LOU504(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	for _, tc := range []struct {
		name          string
		height        []string
		blockBehavior AutoSizeBehavior
		want          float64
	}{
		{name: "fixed auto block-size", blockBehavior: AutoSizeBehaviorFitContent, want: 200},
		{name: "explicitly stretched block-size", blockBehavior: AutoSizeBehaviorStretchExplicit, want: 50},
		{name: "definite height", height: []string{"height", "50px"}, blockBehavior: AutoSizeBehaviorFitContent, want: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, node := fixedWidthChild(map[*html.Node]*css.Style{},
				append([]string{"display", "block", "aspect-ratio", "1/1"}, tc.height...)...)
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: 50}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: 50}).
				SetIsFixedBlockSize(true).
				SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
				SetBlockAutoBehavior(tc.blockBehavior).
				Build()
			if got := initialInlineSize(node, space); got != tc.want {
				t.Errorf("inline-size = %v, want %v", got, tc.want)
			}
		})
	}
}
