package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// layoutBlockFragments lays out root in successive fragmentainers of
// fragmentainerBlockSize, feeding each outgoing break token into the next
// fragment, and returns every fragment's layout result (at most max).
func layoutBlockFragments(t *testing.T, root *html.Node, styles map[*html.Node]*css.Style, fragmentainerBlockSize float64, max int) []*LayoutResult {
	t.Helper()
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	node := buildTestTree(root, styles)
	var results []*LayoutResult
	var token *BlockBreakToken
	for i := 0; i < max; i++ {
		space := NewConstraintSpaceBuilder(wdm, wdm, false).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(fragmentainerBlockSize).
			SetIsAtFragmentainerStart(true).
			SetBlockFragmentationType(FragmentColumn).
			SetBreakToken(token).
			Build()
		result := NewBlockLayoutAlgorithm(ctx, node, space).Layout()
		results = append(results, result)
		t.Logf("fragment %d: block-size %v, token %+v", i,
			NewLogicalFragment(wdm, result.Fragment).BlockSize(), result.BreakToken)
		if result.BreakToken == nil {
			break
		}
		token = result.BreakToken
	}
	return results
}

// LOU-400: a fragmented block is sized by Blink's FinishFragmentation, with
// border-box consumed block-size (fragmentation_utils.cc:547-822 @
// a9f50e522efa). A height:250px box with 20px block borders (290px border
// box) holding a 200px child, in 100px fragmentainers:
//
//	fragment 1: top border + 80px of the child, stretched to the
//	            fragmentainer end → 100, consumed 100 (border-box)
//	fragment 2: no edges, 100px of the child                → 100, consumed 200
//	fragment 3: the rest of the declared height (290−200) incl. the bottom
//	            border, which now fits                      → 90, no token
//
// The fragments add up to the declared border box.
func TestBlockLayout_FinishFragmentationBorderBox_LOU400(t *testing.T) {
	child := makeNode("div")
	container := makeNode("div", child)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block", "height", "250px",
			"border-top", "20px solid black", "border-bottom", "20px solid black"),
		child: makeStyle("display", "block", "height", "200px"),
	}
	results := layoutBlockFragments(t, container, styles, 100, 5)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	wantSizes := []float64{100, 100, 90}
	wantConsumed := []float64{100, 200}
	if len(results) != len(wantSizes) {
		t.Fatalf("got %d fragments, want %d", len(results), len(wantSizes))
	}
	for i, r := range results {
		if got := NewLogicalFragment(wdm, r.Fragment).BlockSize(); got != wantSizes[i] {
			t.Errorf("fragment %d block-size = %v, want %v", i, got, wantSizes[i])
		}
		if i < len(wantConsumed) {
			if r.BreakToken == nil {
				t.Fatalf("fragment %d has no break token", i)
			}
			if got := r.BreakToken.ConsumedBlockSize.Float64(); got != wantConsumed[i] {
				t.Errorf("fragment %d consumed = %v, want %v (border-box)", i, got, wantConsumed[i])
			}
		}
	}
}
