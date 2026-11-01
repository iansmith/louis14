package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestHasSeenAllChildren_ChildIteratorAtEnd (LOU-501, worklist C9; ledger
// L53): the outgoing break token says it has seen all children exactly when
// the child iterator is at its end after the loop stops
// (block_layout_algorithm.cc:1150-1161 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). BlockChildIterator::NextChild
// advances past the child the loop stopped at (block_child_iterator.cc:
// 96-98, :141-145), so it is at its end when that child — the one that broke
// inside (:1108-1115), or the sibling a spanner stop breaks before
// (:1059-1068) — is the last child, or when a spanner stop found no sibling
// to resume at.
func TestHasSeenAllChildren_ChildIteratorAtEnd(t *testing.T) {
	block := func(h string) []string { return []string{"display", "block", "height", h} }
	span := []string{"display", "block", "height", "10px", "column-span", "all"}
	for _, tc := range []struct {
		name string
		kids [][]string
		want bool
	}{
		{"last child breaks inside", [][]string{block("50px"), block("100px")}, true},
		{"non-last child breaks inside", [][]string{block("50px"), block("100px"), block("10px")}, false},
		{"spanner stop, resume sibling is the last child", [][]string{block("20px"), span, block("10px")}, true},
		{"spanner stop, siblings after the resume sibling", [][]string{block("20px"), span, block("10px"), block("10px")}, false},
		{"spanner stop, spanner is the last child", [][]string{block("20px"), span}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			var kids []*html.Node
			for _, k := range tc.kids {
				n := makeNode("div")
				styles[n] = makeStyle(k...)
				kids = append(kids, n)
			}
			root := makeNode("div", kids...)
			styles[root] = makeStyle("display", "block")
			wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
				SetHasBlockFragmentation(true).
				SetShouldPropagateChildBreakValues(true).
				SetFragmentainerBlockSize(100).
				SetIsAtFragmentainerStart(true).
				SetBlockFragmentationType(FragmentColumn).
				Build()
			result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(root, styles), space).Layout()
			if result.BreakToken == nil {
				t.Fatal("no outgoing break token")
			}
			if got := result.BreakToken.HasSeenAllChildren; got != tc.want {
				t.Errorf("HasSeenAllChildren = %v, want %v", got, tc.want)
			}
		})
	}
}
