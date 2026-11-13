package layout

import "testing"

// line-height: normal is the primary font's line spacing in the computed
// line height the lh unit reads, as in line layout:
// ComputedStyle::ComputedLineHeightAsFixed → FontMetrics::FixedLineSpacing
// (core/style/computed_style.cc:2440-2449, platform/fonts/font_metrics.h:
// 152-154 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Liberation Serif
// 16px spaces lines 18px apart, so the lh unit is 18px and 2lh is 36px,
// not 2 × 1.2em = 38.4px.
func TestLhUnit_LineHeightNormalIsFontLineSpacing(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="font: 16px 'Liberation Serif'"><div id="c" style="width: 2lh; height: 1px"></div></div>`)
	c := findBoxByID(root, "c")
	if c == nil {
		t.Fatal("no #c")
	}
	if c.Width != 36 {
		t.Errorf("2lh = %v, want 36 (Liberation Serif 16px line spacing 18)", c.Width)
	}
}
