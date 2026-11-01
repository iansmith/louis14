package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestInlineFloatCommittedOnlyWithItsLine (LOU-513): a float placed while
// laying out a line that is then broken before goes to the next
// fragmentainer with the line, leaving nothing behind in this fragment:
// not its fragment, and none of its break info (block-size for
// fragmentation, break appeal, space shortage). Blink adds a line's floats
// to the block with the line box, once BreakBeforeChildIfNeeded has kept
// the line (HandleInflow → AddResult, block_layout_algorithm.cc:2634-2736 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// 30px Ahem lines at 0, 30, 60 fit a 100px column; the fourth, at 90, does
// not, and is broken before together with the 40px float placed on it.
func TestInlineFloatCommittedOnlyWithItsLine(t *testing.T) {
	fl := makeNode("div")
	block := makeNode("div",
		&html.Node{Type: html.TextNode, Text: "a a a"},
		fl,
		&html.Node{Type: html.TextNode, Text: " a"})
	styles := map[*html.Node]*css.Style{
		block: makeStyle("display", "block", "width", "20px", "font-family", "Ahem",
			"font-size", "20px", "line-height", "30px", "orphans", "1", "widows", "1"),
		fl: makeStyle("display", "block", "float", "left", "width", "10px", "height", "40px"),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(100).
		Build()
	result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(block, styles), space).Layout()
	if result == nil || result.Fragment == nil || result.BreakToken == nil {
		t.Fatal("block did not break")
	}
	lines := 0
	for _, c := range result.Fragment.Children {
		if c.Fragment.Type == FragmentLineBox {
			lines++
		}
		if c.Fragment.IsFloating {
			t.Errorf("float fragment at %v left in the fragment its line was pushed out of", c.Offset.Top.Float64())
		}
	}
	if lines != 3 {
		t.Fatalf("lines = %d, want 3", lines)
	}
	if result.BlockSizeForFragmentation != 100 {
		t.Errorf("BlockSizeForFragmentation = %v, want 100 (the float's 130 block-end went with its line)", result.BlockSizeForFragmentation)
	}
}
