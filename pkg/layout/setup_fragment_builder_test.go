package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
)

// setupFragmentBuilderFixture lays the ground for one
// SetupFragmentBuilderForFragmentation call: a block styled with props,
// in a column fragmentation space with 100px fragmentainers, at
// fragmentainerOffset, resuming token (nil for a first fragment). edit,
// when given, adjusts the space builder before the space is built.
func setupFragmentBuilderFixture(t *testing.T, fragmentainerOffset float64, token *BlockBreakToken,
	edit func(*ConstraintSpaceBuilder), props ...string) (ConstraintSpace, *LayoutInputNode, *BoxFragmentBuilder) {
	t.Helper()
	el := makeNode("div")
	root := makeNode("div", el)
	styles := map[*html.Node]*css.Style{
		root: makeStyle("display", "block"),
		el:   makeStyle(append([]string{"display", "block"}, props...)...),
	}
	node := buildTestTree(root, styles).Children()[0]
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	b := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(100).
		SetFragmentainerOffset(fragmentainerOffset)
	if token != nil {
		b.SetBreakToken(token)
	}
	if edit != nil {
		edit(b)
	}
	space := b.Build()
	geom := CalculateInitialFragmentGeometry(testContext(), node, node.Style(), wdm, space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(node)
	builder.SetConstraintSpace(&space)
	builder.SetInitialFragmentGeometry(&geom)
	return space, node, builder
}

// TestSetupFragmentBuilderForFragmentation_MaxHeightKnownToFit (LOU-505,
// LOU-511): an auto-height box limited by max-height is known to fit when
// its max-height fits in the space left, as Blink probes the block-size
// with an "infinite" intrinsic block-size (ComputeBlockSizeForFragment(...,
// LayoutUnit::Max(), ...), fragmentation_utils.cc:463-490 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); its first fragment must then
// stay in the fragmentainer and requires content before breaking
// (MustStayInCurrentFragmentainer, :484-487, box_fragment_builder.h:
// 401-413).
func TestSetupFragmentBuilderForFragmentation_MaxHeightKnownToFit(t *testing.T) {
	space, node, builder := setupFragmentBuilderFixture(t, 40, nil, nil, "max-height", "50px")
	SetupFragmentBuilderForFragmentation(space, node, nil, builder)
	if !builder.IsKnownToFitInFragmentainer() {
		t.Error("max-height 50px box with 60px left: IsKnownToFitInFragmentainer = false, want true")
	}
	if !builder.MustStayInCurrentFragmentainer() || !builder.RequiresContentBeforeBreaking() {
		t.Errorf("first fragment known to fit: (MustStay, RequiresContentBeforeBreaking) = (%v, %v), want (true, true)",
			builder.MustStayInCurrentFragmentainer(), builder.RequiresContentBeforeBreaking())
	}

	space, node, builder = setupFragmentBuilderFixture(t, 60, nil, nil, "max-height", "50px")
	SetupFragmentBuilderForFragmentation(space, node, nil, builder)
	if builder.IsKnownToFitInFragmentainer() || builder.RequiresContentBeforeBreaking() {
		t.Error("max-height 50px box with 40px left: want neither known to fit nor content required")
	}
}

// TestSetupFragmentBuilderForFragmentation_AutoHeightUnknown: with no
// definite or maximum block-size only the content can size the box, so it
// is not known to fit (the probe stays "infinite", :476-481).
func TestSetupFragmentBuilderForFragmentation_AutoHeightUnknown(t *testing.T) {
	space, node, builder := setupFragmentBuilderFixture(t, 0, nil, nil)
	SetupFragmentBuilderForFragmentation(space, node, nil, builder)
	if builder.IsKnownToFitInFragmentainer() || builder.RequiresContentBeforeBreaking() {
		t.Error("auto-height box: want neither known to fit nor content required")
	}
}

// TestSetupFragmentBuilderForFragmentation_ResumedFragment: a resumed
// fragment is not the first for its node: what its earlier fragments
// consumed comes off the probe (:476-483), it does not have to stay, and in
// the slice model it drops its block-start border (:504-510).
func TestSetupFragmentBuilderForFragmentation_ResumedFragment(t *testing.T) {
	token := &BlockBreakToken{ConsumedBlockSize: layoutunit.FromFloat64Round(80)}
	space, node, builder := setupFragmentBuilderFixture(t, 0, token, nil,
		"height", "150px", "border-top-width", "10px", "border-top-style", "solid")
	SetupFragmentBuilderForFragmentation(space, node, token, builder)
	if !builder.IsKnownToFitInFragmentainer() {
		t.Error("160px box that consumed 80px, 100px left: IsKnownToFitInFragmentainer = false, want true")
	}
	if builder.MustStayInCurrentFragmentainer() || builder.RequiresContentBeforeBreaking() {
		t.Error("resumed fragment: want it not required to stay")
	}
	if got := builder.BorderScrollbarPadding().BlockStart; got != 0 {
		t.Errorf("resumed slice fragment: block-start edge = %v, want cleared", got)
	}
}

// TestSetupFragmentBuilderForFragmentation_InheritsRequiresContent: the
// space's RequiresContentBeforeBreaking carries over whether or not the box
// fits (:453-454, :500).
func TestSetupFragmentBuilderForFragmentation_InheritsRequiresContent(t *testing.T) {
	space, node, builder := setupFragmentBuilderFixture(t, 0, nil, func(b *ConstraintSpaceBuilder) {
		b.SetRequiresContentBeforeBreaking(true)
	})
	SetupFragmentBuilderForFragmentation(space, node, nil, builder)
	if !builder.RequiresContentBeforeBreaking() {
		t.Error("space requires content before breaking: builder does not")
	}
}

// TestSetupFragmentBuilderForFragmentation_InitialBalancingPass: the
// initial column balancing pass knows no fragmentainer block-size, so
// nothing is known to fit (:451-452), and the box's block edges are unbreakable
// floors for the column block-size (:515-519).
func TestSetupFragmentBuilderForFragmentation_InitialBalancingPass(t *testing.T) {
	space, node, builder := setupFragmentBuilderFixture(t, 0, nil, func(b *ConstraintSpaceBuilder) {
		b.SetIsInitialColumnBalancingPass(true)
	}, "height", "20px", "border-top-width", "7px", "border-top-style", "solid", "padding-bottom", "12px")
	SetupFragmentBuilderForFragmentation(space, node, nil, builder)
	if builder.IsKnownToFitInFragmentainer() {
		t.Error("initial balancing pass: IsKnownToFitInFragmentainer = true")
	}
	if got := builder.TallestUnbreakableBlockSize(); got != 12 {
		t.Errorf("TallestUnbreakableBlockSize = %v, want the 12px block-end edge", got)
	}
}

// TestSetupFragmentBuilderForFragmentation_ForcedOffIsMonolithic: a box
// laid out with fragmentation forced off at its first fragment is treated
// as monolithic (:408-424).
func TestSetupFragmentBuilderForFragmentation_ForcedOffIsMonolithic(t *testing.T) {
	space, node, builder := setupFragmentBuilderFixture(t, 0, nil, func(b *ConstraintSpaceBuilder) {
		b.space.IsBlockFragmentationForcedOff = true
	})
	SetupFragmentBuilderForFragmentation(space, node, nil, builder)
	if !builder.isMonolithic {
		t.Error("fragmentation forced off at the first fragment: builder not monolithic")
	}
}

// TestCreateConstraintSpaceForColumn_ZeroColumnBlockSizeStaysZero
// (LOU-511): a column block-size of 0 — an auto-height multicol whose row
// holds no content before a spanner — is passed to the fragmentainer space
// as is, as Blink's CreateConstraintSpaceForFragmentainer does
// (column_layout_algorithm.cc:929-934, fragmentation_utils.cc:1377-1397 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 used to map it to
// Indefinite.
func TestCreateConstraintSpaceForColumn_ZeroColumnBlockSizeStaysZero(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	mla := &MulticolLayoutAlgorithm{style: makeStyle("column-count", "2"), remainingContentBlockSize: Indefinite}
	if !mla.hasAutoColumnHeight() {
		t.Fatal("fixture: column-height is not auto")
	}
	col := mla.createConstraintSpaceForColumn(wdm, 100, 0, Indefinite, nil, true, BreakAppealLastResort)
	if col.AvailableSize.BlockSize.Float64() != 0 || col.FragmentainerBlockSize != 0 {
		t.Errorf("column space for a 0 column block-size: (available, fragmentainer) = (%v, %v), want (0, 0)",
			col.AvailableSize.BlockSize.Float64(), col.FragmentainerBlockSize)
	}
}
