package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// inlineFragmentationProbe returns a 50px-wide block of seven 20px Ahem
// lines (one 40px word each, beside the float) whose inline content starts
// with a 10px-wide float of height floatHeight.
func inlineFragmentationProbe(floatHeight string) *LayoutInputNode {
	fl := makeNode("div")
	block := makeNode("div", fl, &html.Node{Type: html.TextNode, Text: "aa aa aa aa aa aa aa"})
	styles := map[*html.Node]*css.Style{
		block: makeStyle("display", "block", "width", "50px", "font-family", "Ahem",
			"font-size", "20px", "line-height", "20px", "orphans", "1", "widows", "1"),
		fl: makeStyle("display", "block", "float", "right", "width", "10px", "height", floatHeight),
	}
	return buildTestTree(block, styles)
}

// layoutInlineFragmentationProbe lays node out in a 100px column
// fragmentainer, resuming token.
func layoutInlineFragmentationProbe(node *LayoutInputNode, token *BlockBreakToken) *LayoutResult {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	b := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(100)
	if token != nil {
		b.SetBreakToken(token)
	}
	return NewBlockLayoutAlgorithm(testContext(), node, b.Build()).Layout()
}

// TestInlineBreakToken_LineAndFloatContinuation (LOU-513): a block whose
// inline content breaks resumes through inline break tokens, as Blink's
// (inline/inline_break_token.h:46-160 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): its block break token holds,
// last, the inline break token at the start of the next line, and before
// it, for a float that broke inside, an inline break token in a parallel
// block flow at the float's item wrapping the float's block break token
// (CreateForParallelBlockFlow). The next fragment resumes both.
func TestInlineBreakToken_LineAndFloatContinuation(t *testing.T) {
	node := inlineFragmentationProbe("150px")
	result := layoutInlineFragmentationProbe(node, nil)
	tok := result.BreakToken
	if tok == nil || len(tok.ChildBreakTokens) != 2 {
		t.Fatalf("break token = %+v, want two child break tokens", tok)
	}
	floatTok, ok := tok.ChildBreakTokens[0].(*InlineBreakToken)
	if !ok || !floatTok.IsInParallelBlockFlow || floatTok.Start.ItemIndex < 0 ||
		floatTok.GetBlockBreakToken() == nil || floatTok.GetBlockBreakToken().Node != node.Children()[0] {
		t.Errorf("first child token = %+v, want the float's continuation in a parallel block flow", tok.ChildBreakTokens[0])
	}
	lineTok, ok := tok.ChildBreakTokens[1].(*InlineBreakToken)
	if !ok || lineTok.IsInParallelBlockFlow || lineTok.Node != node || lineTok.Start.IsZero() {
		t.Fatalf("last child token = %+v, want the next line's start", tok.ChildBreakTokens[1])
	}

	next := layoutInlineFragmentationProbe(node, tok)
	floats, lines := 0, 0
	for _, c := range next.Fragment.Children {
		switch {
		case c.Fragment.IsFloating:
			floats++
			if got := c.Fragment.Size.HeightF64(); got != 50 {
				t.Errorf("resumed float height = %v, want the 50px left", got)
			}
		case c.Fragment.Type == FragmentLineBox:
			lines++
		}
	}
	if floats != 1 || lines != 2 {
		t.Errorf("next fragment: %d floats, %d lines; want the float resumed and the 2 lines left", floats, lines)
	}
}
