package layout

import "testing"

// An inline-block's baseline is its last baseline, but a child contributes
// its last baseline to it only when the child's fragment uses its last
// baseline for an inline baseline: the blocks laid out in the inline-block
// do (SetUseLastBaselineForInlineBaseline in kInlineBlock,
// block_layout_algorithm.cc:1510-1513), a flex or grid container does not,
// so it contributes its first (PropagateBaselineFromBlockChild, :3762-3768).
// A block-axis scroll container that does contributes its block-end margin
// edge (ForceInlineBaselineSynthesis, physical_box_fragment.h:167-173,
// block_layout_algorithm.cc:3770-3776). All @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestInlineBlockBaselineFromChildFragments(t *testing.T) {
	cases := []struct {
		name, child string
		xy          float64
	}{
		{"flex child gives its first baseline", `<div style="display:flex;flex-direction:column"><div>a</div><div>b</div></div>`, 0},
		{"grid child gives its first baseline", `<div style="display:grid"><div>a</div><div>b</div></div>`, 0},
		// Guard: a scrollable block child gives its block-end margin edge
		// (40), so x's baseline (8) sits at 40.
		{"scroll container child gives its margin edge", `<div>a</div><div style="overflow:auto;height:30px">b</div>`, 32},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="font:10px/1 Ahem"><span id="x">x</span><span id="ib" style="display:inline-block">`+c.child+`</span></div>`)
			if x := r["x"]; len(x) != 1 || x[0].y != c.xy {
				t.Errorf("#x %+v; want y %v", x, c.xy)
			}
		})
	}
}
