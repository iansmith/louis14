package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestTallOutsideMarkerPushesLaterLinesBeforeTheirBreakCheck (LOU-507): an
// outside image marker taller than the first line pushes the content down
// as soon as the first line is placed, so every later line is laid out —
// and checked against the fragmentainer end — below the push. Blink places
// the marker against the first line box right after that line's
// BreakBeforeChildIfNeeded (PositionOrPropagateListMarker, block_layout_
// algorithm.cc:2634, :2720; UnpositionedListMarker::AddToBox content
// adjust) and lays out the next line from the pushed position, so the line
// that breaks reports a space shortage that includes the push (BreakBefore
// Child → PropagateSpaceShortage, fragmentation_utils.cc:944-968 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// 20px Ahem lines, a 40px-tall image marker (ascent 40 vs the line's 16:
// push 24), a 100px fragmentainer: lines at 24, 44, 64 fit; the fourth at
// 84 ends at 104 and breaks, short by 4.
func TestTallOutsideMarkerPushesLaterLinesBeforeTheirBreakCheck(t *testing.T) {
	li := &html.Node{Type: html.ElementNode, TagName: "li"}
	txt := &html.Node{Type: html.TextNode, Text: "a a a a a a a", Parent: li}
	li.Children = []*html.Node{txt}
	styles := map[*html.Node]*css.Style{
		li: makeStyle(
			"display", "list-item",
			"list-style-position", "outside",
			"list-style-image", "url(lou507-marker.png)", // unique: images are cached by URL across tests
			"font-family", "Ahem", "font-size", "20px", "line-height", "20px",
			"width", "20px", "orphans", "1", "widows", "1",
		),
	}
	ctx := imgContextWithIntrinsic(32, 40)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetFragmentainerBlockSize(100).
		Build()
	result := NewBlockLayoutAlgorithm(ctx, buildTestTree(li, styles), space).Layout()
	if result == nil || result.Fragment == nil || result.BreakToken == nil {
		t.Fatal("list item did not break")
	}
	lines := 0
	for _, c := range result.Fragment.Children {
		if c.Fragment != nil && c.Fragment.Type == FragmentLineBox {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("lines before the break = %d, want 3 (the pushed fourth line does not fit)", lines)
	}
	if result.MinSpaceShortage != 4 {
		t.Errorf("MinSpaceShortage = %v, want 4 (84+20-100)", result.MinSpaceShortage)
	}
}
