package layout

import "testing"

// line-height: normal resolves to the primary font's line spacing on every
// baseline (ComputedStyle::ComputedLineHeightAsFixed →
// FontMetrics::FixedLineSpacing, core/style/computed_style.cc:2440-2449 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which
// InlineBoxState::ComputeTextMetrics uses for the leading
// (core/layout/inline/inline_box_state.cc:141-151, same SHA). For
// Liberation Serif at 16px (hhea 0.891 / 0.216 / line gap 0.042 em) that is
// round(14.26) + round(3.46) + round(0.67) = 18px, not the 1.2em (19.2px)
// approximation, so an upright vertical line (central baseline) is 18px
// wide, the same as a horizontal one is tall.
func TestLineHeightNormal_CentralBaselineUsesFontLineSpacing(t *testing.T) {
	for _, tc := range []struct{ name, style string }{
		{"horizontal", ""},
		{"upright vertical", "writing-mode: vertical-rl; text-orientation: upright;"},
	} {
		root := layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="font: 16px 'Liberation Serif'; line-height: normal; `+tc.style+`">X</div>`)
		line := collectTextBoxes(root, nil)[0].Parent
		if !line.IsLineBox {
			t.Fatalf("%s: text box parent is not a line box", tc.name)
		}
		got := line.Height
		if tc.style != "" {
			got = line.Width
		}
		if got != 18 {
			t.Errorf("%s: line block size = %v, want 18 (Liberation Serif 16px line spacing)", tc.name, got)
		}
	}
}
