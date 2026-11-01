package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-474 (a): a multicol clones its block-end decorations only when it is
// fragmented by an outer context, outside the initial column-balancing pass,
// and not known to fit in the outer fragmentainer. Blink computes the
// known-to-fit bit from ComputeBlockSizeForFragment with an infinite
// intrinsic block-size, so a definite height or a max-height can make the box
// known to fit (SetupFragmentBuilderForFragmentation,
// fragmentation_utils.cc:440-498 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestMulticolCloneEndDecorationsGating_LOU474(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	for _, tc := range []struct {
		name          string
		fragBlockSize float64 // Indefinite: no outer fragmentation
		balancingPass bool
		mcProps       []string
		wantClone     bool
		wantFits      bool
	}{
		{name: "unfragmented", fragBlockSize: Indefinite,
			mcProps: []string{"height", "40px"}},
		{name: "initial balancing pass", fragBlockSize: Indefinite, balancingPass: true,
			mcProps: []string{"height", "40px"}},
		{name: "definite height known to fit", fragBlockSize: 100,
			mcProps: []string{"height", "40px"}, wantFits: true},
		{name: "max-height known to fit", fragBlockSize: 100,
			mcProps: []string{"max-height", "40px"}, wantFits: true},
		{name: "taller than the fragmentainer", fragBlockSize: 100,
			mcProps: []string{"height", "150px"}, wantClone: true},
		{name: "auto height", fragBlockSize: 100,
			mcProps: []string{}, wantClone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kid := makeNode("div")
			mc := makeNode("div", kid)
			styles := map[*html.Node]*css.Style{
				mc: makeStyle(append([]string{"display", "block", "column-count", "2", "column-gap", "0",
					"column-fill", "auto", "box-decoration-break", "clone",
					"padding-top", "5px", "padding-bottom", "5px"}, tc.mcProps...)...),
				kid: makeStyle("display", "block", "height", "20px"),
			}
			b := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite})
			if tc.fragBlockSize != Indefinite || tc.balancingPass {
				b = b.SetHasBlockFragmentation(true).
					SetShouldPropagateChildBreakValues(true).
					SetFragmentainerBlockSize(tc.fragBlockSize).
					SetIsAtFragmentainerStart(true).
					SetIsInitialColumnBalancingPass(tc.balancingPass).
					SetBlockFragmentationType(FragmentColumn)
			}
			mla := NewMulticolLayoutAlgorithm(testContext(), buildTestTree(mc, styles), b.Build())
			if res := mla.Layout(); res == nil || res.Fragment == nil {
				t.Fatal("layout returned nil")
			}
			if got := mla.containerBuilder.ShouldCloneBoxEndDecorations(); got != tc.wantClone {
				t.Errorf("ShouldCloneBoxEndDecorations = %v, want %v", got, tc.wantClone)
			}
			if got := mla.isKnownToFitInFragmentainer; got != tc.wantFits {
				t.Errorf("isKnownToFitInFragmentainer = %v, want %v", got, tc.wantFits)
			}
		})
	}
}

// LOU-474 (b): when a definite-height multicol's content budget is spent
// (multicol-span-all-children-height-003's post-spanner block2), Blink lays
// the line out with a 0px column block-size: the column box takes the
// fragmentainer block-size, 0 (FinishFragmentationForFragmentainer,
// fragmentation_utils.cc:830-843), while its content still progresses 1px
// per column through the capacity floor (ClampedToValidFragmentainerCapacity,
// fragmentation_utils.h:153-158 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// There is no 1px column block-size floor in ColumnLayoutAlgorithm.
func TestExhaustedBudgetColumnsAreZeroTall_LOU474(t *testing.T) {
	block1 := makeNode("div")
	spanner := makeNode("div")
	block2 := makeNode("div")
	mc := makeNode("div", block1, spanner, block2)
	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
			"width", "400px", "height", "200px"),
		block1:  makeStyle("display", "block", "height", "600px"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "50px"),
		block2:  makeStyle("display", "block", "height", "4px"),
	}
	frag := layoutMulticolForTest(t, mc, styles).Fragment
	var postSpanner []float64
	for _, ch := range frag.Children {
		if ch.Fragment.BoxType == BoxTypeColumn && ch.Offset.Top.Float64() >= 250 {
			postSpanner = append(postSpanner, ch.Fragment.Size.HeightF64())
		}
	}
	if len(postSpanner) != 4 {
		t.Fatalf("post-spanner columns = %v, want 4 (block2's 4px, 1px per column)", postSpanner)
	}
	for _, h := range postSpanner {
		if h != 0 {
			t.Errorf("post-spanner column heights = %v, want all 0 (fragmentainer block-size)", postSpanner)
			break
		}
	}
	got := borderBoxRectsByID(frag, map[*html.Node]string{block2: "b2"})["b2"]
	if len(got) != 4 {
		t.Errorf("block2 fragments = %v, want four 1px slices", got)
	}
}
