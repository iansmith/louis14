package layout

import "testing"

// TestLogicalSizeSidewaysWritingModes (LOU-488 item 4,
// multicol-span-all-rule-002): sideways-lr / sideways-rl are vertical
// writing modes, so block-size maps to width and inline-size to height, as
// for vertical-lr / vertical-rl (Blink IsHorizontalWritingMode is false for
// all four, platform/text/writing_mode.h:56-58 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestLogicalSizeSidewaysWritingModes(t *testing.T) {
	for _, wm := range []string{"vertical-lr", "vertical-rl", "sideways-lr", "sideways-rl"} {
		t.Run(wm, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
				`<div style="writing-mode:`+wm+`;width:200px;height:100px">`+
				`<div id=p style="block-size:80px;inline-size:60px"></div>`+
				`</div>`)
			if len(got["p"]) != 1 || got["p"][0].w != 80 || got["p"][0].h != 60 {
				t.Errorf("p = %v, want 80x60 (block-size -> width, inline-size -> height)", got["p"])
			}
		})
	}
}
