package layout

import "testing"

// TestInlineTable_MarginTopPlacedBelowLineTop mirrors the reference of WPT
// css-backgrounds/border-image-repeat-round-003: a baseline-aligned
// display:inline-table with margin-top:16px alone on its line. Its ascent
// (margin-top + first baseline) defines the line's ascent, so its border box
// must start margin-top (16px) below the line top.
//
// Blink places an atomic inline from one set of baseline metrics:
// LogicalBoxFragment::BaselineMetrics (logical_box_fragment.cc:14-49 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) picks FirstBaseline() unless the
// fragment UseLastBaselineForInlineBaseline() (set only by block / column
// layout for the inline-block baseline algorithm, block_layout_algorithm.cc:
// 1510-1513 @ a9f50e52), so a table always uses its first baseline, and
// adds margins.line_over to the ascent. The same metrics position the box
// (InlineBoxState / LineBoxFragmentBuilder), so ascent and placement agree.
//
// louis14 computed the line ascent from the table's first baseline but placed
// the box from its LastBaseline (unset when no cell has a text baseline),
// falling back to the bottom margin edge and clamping the offset to 0: the
// table sat 16px too high.
func TestInlineTable_MarginTopPlacedBelowLineTop(t *testing.T) {
	src := `<!DOCTYPE html><style>body{margin:0}
#t{display:inline-table;margin-top:16px;height:272px;width:296px}</style>
<div id=t><div style="display:table-row"><div style="display:table-cell;height:64px;width:64px"></div></div>
<div style="display:table-row"><div style="display:table-cell;height:64px"></div></div></div>`
	l := layoutFallbackHTML(t, src, nil)
	b := findBox(l.root, byID("t"))
	if b == nil {
		t.Fatal("inline-table #t not found")
	}
	if b.Y != 16 {
		t.Errorf("inline-table border-box y = %v; want 16 (line top + margin-top, placement must use the same first baseline as the line ascent)", b.Y)
	}
}
