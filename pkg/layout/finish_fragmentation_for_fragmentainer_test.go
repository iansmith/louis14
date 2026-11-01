package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestFinishFragmentationForFragmentainer_LOU478 pins how a column box
// (fragmentainer) is sized and what its break token carries: Blink's
// FinishFragmentationForFragmentainer (fragmentation_utils.cc:823-866 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), called from
// BlockLayoutAlgorithm::FinalizeForFragmentation for a fragmentainer
// (block_layout_algorithm.cc:3137-3138 @ the same SHA). With a known
// fragmentainer block-size the fragment takes it whatever its content, the
// consumed block-size advances by the fragmentainer capacity, and the token is
// never at its block end.
func TestFinishFragmentationForFragmentainer_LOU478(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	// columnSpace is a fragmentainer space of a known block-size. Blink's
	// CreateConstraintSpaceForFragmentainer (fragmentation_utils.cc:1377-1411
	// @ a9f50e522efa) sets neither IsFixedBlockSize nor a block-size
	// override; fixed adds louis14's MulticolLayoutAlgorithm override
	// (createConstraintSpaceForColumn).
	columnSpace := func(fragmentainerBlockSize float64, fixed bool, token *BlockBreakToken) ConstraintSpace {
		b := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: fragmentainerBlockSize}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetHasBlockFragmentation(true).
			SetBlockFragmentationType(FragmentColumn).
			SetFragmentainerBlockSize(fragmentainerBlockSize).
			SetFragmentainerOffset(0)
		if fixed {
			b = b.SetIsFixedBlockSize(true).SetIsBlockSizeOverride(true)
		}
		if token != nil {
			b = b.SetBreakToken(token)
		}
		return b.Build()
	}
	layoutColumn := func(root *LayoutInputNode, space ConstraintSpace) *LayoutResult {
		bla := NewBlockLayoutAlgorithm(testContext(), root, space)
		bla.SetBoxType(BoxTypeColumn)
		return bla.Layout()
	}

	// Row a: content shorter than the column. The fragment is the
	// fragmentainer block-size (:830-840), not the 30px content.
	t.Run("a_fragment_takes_fragmentainer_block_size", func(t *testing.T) {
		a := makeNode("div")
		root := makeNode("div", a)
		styles := map[*html.Node]*css.Style{
			root: makeStyle("display", "block"),
			a:    makeStyle("display", "block", "height", "30px"),
		}
		res := layoutColumn(buildTestTree(root, styles), columnSpace(100, false, nil))
		if got := NewLogicalFragment(wdm, res.Fragment).BlockSize(); got != 100 {
			t.Errorf("column fragment block-size = %v, want 100", got)
		}
	})

	// Row b: a 50px box whose 150px child overflows it finishes its own box
	// in the first column while the child continues in a parallel flow. The
	// column token is never at its block end, and the next column is a full
	// column (100), not a 0px past-the-end fragment.
	t.Run("b_token_never_at_block_end", func(t *testing.T) {
		inner := makeNode("div")
		c := makeNode("div", inner)
		root := makeNode("div", c)
		styles := map[*html.Node]*css.Style{
			root:  makeStyle("display", "block"),
			c:     makeStyle("display", "block", "height", "50px"),
			inner: makeStyle("display", "block", "height", "150px"),
		}
		rootNode := buildTestTree(root, styles)
		first := layoutColumn(rootNode, columnSpace(100, true, nil))
		tok := first.BreakToken
		if tok == nil {
			t.Fatal("first column has no break token")
		}
		if tok.IsAtBlockEnd {
			t.Error("first column token IsAtBlockEnd = true, want false")
		}
		if got := tok.ConsumedBlockSize.Float64(); got != 100 {
			t.Errorf("first column ConsumedBlockSize = %v, want 100", got)
		}
		second := layoutColumn(rootNode, columnSpace(100, true, tok))
		if got := NewLogicalFragment(wdm, second.Fragment).BlockSize(); got != 100 {
			t.Errorf("second column fragment block-size = %v, want 100", got)
		}
		if second.BreakToken != nil && second.BreakToken.IsAtBlockEnd {
			t.Error("second column token IsAtBlockEnd = true, want false")
		}
	})

	// Row c: a column of lines breaks between lines at 40px of 50. The
	// consumed block-size advances by the fragmentainer capacity (50,
	// :838-841), not the 40px of lines.
	t.Run("c_inline_break_consumes_capacity", func(t *testing.T) {
		root := makeNode("div", makeTextNode("X X X X"))
		styles := map[*html.Node]*css.Style{
			root: makeStyle("display", "block", "font-family", "Ahem", "font-size", "10px",
				"line-height", "20px", "width", "10px"),
		}
		res := layoutColumn(buildTestTree(root, styles), columnSpace(50, true, nil))
		tok := res.BreakToken
		if tok == nil {
			t.Fatal("column has no break token")
		}
		if got := tok.ConsumedBlockSize.Float64(); got != 50 {
			t.Errorf("column ConsumedBlockSize = %v, want 50", got)
		}
		if got := NewLogicalFragment(wdm, res.Fragment).BlockSize(); got != 50 {
			t.Errorf("column fragment block-size = %v, want 50", got)
		}
	})
}

// TestFinishFragmentationForFragmentainerKeepsSpannerParentWithOOFs_LOU478:
// an empty spanner parent that holds out-of-flow fragmentainer descendants is
// not empty: FinishFragmentationForFragmentainer clears the flag
// (fragmentation_utils.cc:863-865 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); without such descendants the
// flag stays.
func TestFinishFragmentationForFragmentainerKeepsSpannerParentWithOOFs_LOU478(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: 100}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(100).
		Build()
	for _, tc := range []struct {
		name    string
		hasOOFs bool
		want    bool
	}{
		{"oof_descendants_clear_flag", true, false},
		{"no_oof_descendants_keep_flag", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewBoxFragmentBuilder(wdm)
			builder.SetConstraintSpace(&space)
			builder.SetBoxType(BoxTypeColumn)
			builder.SetIsEmptySpannerParent(true)
			if tc.hasOOFs {
				builder.AddOutOfFlowFragmentainerDescendant(LogicalOofNodeForFragmentation{})
			}
			FinishFragmentationForFragmentainer(builder, 0)
			if got := builder.IsEmptySpannerParent(); got != tc.want {
				t.Errorf("IsEmptySpannerParent = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestColumnSpaceHasNoBlockSizeOverride_LOU484: a column box's space is
// Blink's CreateConstraintSpaceForFragmentainer (fragmentation_utils.cc:
// 1377-1411 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which sets the
// fragmentainer block-size and available size but neither IsFixedBlockSize
// nor a block-size override; FinishFragmentationForFragmentainer sizes the
// fragment instead.
func TestColumnSpaceHasNoBlockSizeOverride_LOU484(t *testing.T) {
	mc := makeNode("div")
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2"),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()
	mla := NewMulticolLayoutAlgorithm(testContext(), buildTestTree(mc, styles), space)
	colSpace := mla.createConstraintSpaceForColumn(wdm, 50, 100, Indefinite, nil, true, BreakAppealLastResort)
	if colSpace.IsFixedBlockSize || colSpace.IsBlockSizeOverride {
		t.Errorf("column space IsFixedBlockSize=%v IsBlockSizeOverride=%v, want neither", colSpace.IsFixedBlockSize, colSpace.IsBlockSizeOverride)
	}
	if got := colSpace.FragmentainerBlockSize; got != 100 {
		t.Errorf("column space fragmentainer block-size = %v, want 100", got)
	}
}
