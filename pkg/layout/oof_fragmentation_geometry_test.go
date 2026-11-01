package layout

import (
	"testing"

	"louis14/pkg/html"
)

// oofPageRects parses src, lays it out at 800x600 and returns, per id, the
// page-space rect of every box the element generated (one per fragment), in
// box-tree order.
func oofPageRects(t *testing.T, src string) map[string][]fragRect {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	out := map[string][]fragRect{}
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil && b.Node.Type == html.ElementNode {
			if id, ok := b.Node.GetAttribute("id"); ok {
				out[id] = append(out[id], fragRect{b.X, b.Y, b.Width, b.Height})
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
		walk(b)
	}
	return out
}

// oofProbeMulticol is the column-count:2; column-gap:0; column-fill:auto
// multicol the OOF-fragmentation probes are built on.
const oofProbeMulticol = "column-count:2;column-gap:0;column-fill:auto;"

// TestOOFStaticPositionIsFragmentLocal pins Blink's fragment-local static
// position for an OOF whose containing block is outside the fragmentation
// context. The abspos follows a 120px block inside a non-positioned wrapper
// that breaks at 100, so its static position is 20px into column 2 and,
// with the ICB as its containing block, that is where it is placed. Blink
// records (BorderScrollbarPadding().inline_start,
// previous_inflow_position.logical_block_offset) with no consumed
// block-size (block_layout_algorithm.cc:1654-1656 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the static position is made
// stitched only for a fragmentainer descendant, at its containing block, by
// BoxFragmentBuilder::AdjustFragmentainerDescendant
// (box_fragment_builder.cc:781-809 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Chrome 155 getClientRects: a = [50,20,50,80] (plan probe pA).
func TestOOFStaticPositionIsFragmentLocal(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div style="`+oofProbeMulticol+`width:100px;height:100px"><div>`+
		`<div style="height:120px"></div>`+
		`<div id=a style="position:absolute;width:50px;height:80px"></div>`+
		`</div></div>`)
	checkRects(t, got, map[string][]fragRect{"a": {{50, 20, 50, 80}}})
}
