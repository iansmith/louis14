package layout

import (
	"fmt"
	"testing"
)

// LOU-840. flex-basis: content is the item's max-content size, measured in
// the same item space flex-basis: auto with an auto main size uses: the
// kContent case of ResolveMainInlineLength → MinMaxSizesFunc(kContent)
// (flex/flex_layout_algorithm.cc:960-1008 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The item's own height:100%
// resolves against the container, and a height:100% canvas transfers that
// height through its ratio.
func TestFlexBasisContent_SameAsAutoWithAutoMainSize(t *testing.T) {
	const item = `<div id="t" style="border:5px solid green;height:100%%;flex-basis:%s;box-sizing:border-box;min-width:0;flex-shrink:0"><canvas width="10" height="10" style="width:100%%;height:100%%;border:5px solid green;box-sizing:border-box"></canvas></div>`
	for _, basis := range []string{"auto", "content"} {
		t.Run(basis, func(t *testing.T) {
			// intrinsic-percent-replaced-019, without the abspos wrapper; a
			// wide container, flex-shrink:0 and min-width:0 make the
			// item's width its flex basis.
			src := `<!DOCTYPE html><body style="margin:0"><div style="width:300px;height:90px;display:flex;border:5px solid green;box-sizing:border-box">` +
				fmt.Sprintf(item, basis) + `</div>`
			b := findBoxByID(layoutRootTestHTML(t, src), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Width != 80 {
				t.Errorf("item width = %v, want 80 (canvas 70 = its 100%% height, plus the item's border)", b.Width)
			}
		})
	}
}
