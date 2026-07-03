package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// TestChildrenHeight003_Trace replicates multicol-span-all-children-height-003
// with resolved px heights to diagnose the post-spanner budget for block2.
//
// article: width 400px, height 200px, column-count 2.
// block1: 300% -> 600px (overflows into a 3rd column; row height 200px).
// spanner: 25% -> 50px.
// block2: 100% -> 200px, but no budget left (200 - 200 - 50 < 0), so per the
// reference it collapses to 1px column boxes.
func TestChildrenHeight003_Trace(t *testing.T) {
	block1 := makeNode("div")
	spanner := makeNode("div")
	block2 := makeNode("div")
	mc := makeNode("div", block1, spanner, block2)

	styles := map[*html.Node]*css.Style{
		mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0",
			"width", "400px", "height", "200px"),
		block1:  makeStyle("display", "block", "height", "600px"),
		spanner: makeStyle("display", "block", "column-span", "all", "height", "50px"),
		block2:  makeStyle("display", "block", "height", "200px"),
	}

	result := layoutMulticolForTest(t, mc, styles)

	// Diagnostic dump, recursing into column fragments to see block2's own box.
	for i, ch := range result.Fragment.Children {
		t.Logf("child[%d] boxType=%v offset=(%v,%v) size=%vx%v", i,
			ch.Fragment.BoxType, ch.Offset.Left.Float64(), ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64())
		for j, gc := range ch.Fragment.Children {
			t.Logf("    grandchild[%d.%d] offset=(%v,%v) size=%vx%v", i, j,
				gc.Offset.Left.Float64(), gc.Offset.Top.Float64(),
				gc.Fragment.Size.WidthF64(), gc.Fragment.Size.HeightF64())
		}
	}

	// The reference places block2 in 1px column boxes below the spanner. The
	// post-spanner columns must NOT be 200px tall.
	spanners := spannerChildren(result)
	if len(spanners) != 1 {
		t.Fatalf("expected 1 spanner, got %d", len(spanners))
	}
	spannerBottom := spanners[0].Offset.Top.Float64() + spanners[0].Fragment.Size.HeightF64()
	t.Logf("spanner bottom = %v", spannerBottom)
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.BoxType == BoxTypeColumn && ch.Offset.Top.Float64() >= spannerBottom {
			if h := ch.Fragment.Size.HeightF64(); h > 1 {
				t.Errorf("post-spanner column at y=%v has height %v, want <=1 (no budget left)",
					ch.Offset.Top.Float64(), h)
			}
		}
	}
}
