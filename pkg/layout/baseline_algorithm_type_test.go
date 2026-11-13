package layout

import "testing"

// A table's baseline counts toward a block's last baseline except when the
// block is laid out for an inline-block's baseline: Blink skips table
// children only in BaselineAlgorithmType::kInlineBlock
// (PropagateBaselineFromBlockChild, block_layout_algorithm.cc:3740-3745 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which the line breaker sets on
// an atomic inline's space (inline/line_breaker.cc:3110-3115). So a flex
// item aligned by its last baseline, laid out in the default type, takes
// the last baseline of its trailing table.
func TestBaselineAlgorithmTypeTableChild(t *testing.T) {
	// Item a: a 10px line, then a table whose 20px line has its baseline
	// at 10 + 16 = 26. Item b: one 10px line, baseline 8. Aligned by last
	// baseline, b moves down by 26 - 8 = 18.
	src := `<!DOCTYPE html><body style="margin:0"><div style="display:flex;align-items:last baseline;font:10px/1 Ahem">` +
		`<div><div>x</div><table style="border-spacing:0"><tr><td style="padding:0;font-size:20px;vertical-align:baseline">y</td></tr></table></div>` +
		`<div id="b">z</div></div>`
	b := findBoxByID(layoutRootTestHTML(t, src), "b")
	if b == nil {
		t.Fatal("missing #b")
	}
	if b.Y != 18 {
		t.Errorf("#b y = %v, want 18 (aligned to the table's last baseline)", b.Y)
	}
}
