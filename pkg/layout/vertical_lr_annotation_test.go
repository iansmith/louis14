package layout

import (
	"fmt"
	"testing"
)

// In vertical-lr, Blink's one flipped-lines writing mode, line-over is the
// block-end side (IsFlippedLinesWritingMode, platform/text/writing_mode.h:
// 67-69 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Over annotations and
// over emphasis marks take their space on the over side, which
// InlineLayoutAlgorithm maps to block-end there (overflow_over ->
// block-end, core/layout/inline/inline_layout_algorithm.cc:981-992, same
// SHA). So a vertical-lr line is as wide as the same line in vertical-rl,
// with the annotation on the physical right in both.

// verticalLineProbe lays out body in a 16px serif div in writing mode wm with
// text orientation to, and returns the div's width and the text boxes by
// text.
func verticalLineProbe(t *testing.T, wm, to, body string) (width float64, text map[string]*Box) {
	t.Helper()
	root := layoutRootTestHTML(t, fmt.Sprintf(`<!DOCTYPE html><style>body { margin: 0 }</style>
<div id="d" style="writing-mode: %s; text-orientation: %s; font: 16px serif">%s</div>`, wm, to, body))
	text = map[string]*Box{}
	for _, b := range collectTextBoxes(root, nil) {
		text[b.Text] = b
	}
	return findBoxByID(root, "d").Width, text
}

func TestVerticalLR_OverAnnotationOnLineOverSide(t *testing.T) {
	for _, to := range []string{"sideways", "mixed"} {
		rlWidth, rl := verticalLineProbe(t, "vertical-rl", to, `<ruby>X<rt>a</rt></ruby>Y`)
		lrWidth, lr := verticalLineProbe(t, "vertical-lr", to, `<ruby>X<rt>a</rt></ruby>Y`)
		if lrWidth != rlWidth {
			t.Errorf("%s: vertical-lr ruby line width = %v, want vertical-rl's %v", to, lrWidth, rlWidth)
		}
		// The annotation sits on the base's em height on the right in both
		// modes, so a one-line vertical-lr layout matches vertical-rl's.
		for _, s := range []string{"X", "a"} {
			if lr[s] == nil || rl[s] == nil {
				t.Fatalf("%s: missing text box %q", to, s)
			}
			if lr[s].X != rl[s].X {
				t.Errorf("%s: vertical-lr %q x = %v, want vertical-rl's %v", to, s, lr[s].X, rl[s].X)
			}
		}
	}
}

func TestVerticalLR_OverEmphasisSpaceOnLineOverSide(t *testing.T) {
	for _, to := range []string{"sideways", "mixed"} {
		body := `<span style="text-emphasis: dot">XY</span>`
		rlWidth, rl := verticalLineProbe(t, "vertical-rl", to, body)
		lrWidth, lr := verticalLineProbe(t, "vertical-lr", to, body)
		if lrWidth != rlWidth {
			t.Errorf("%s: vertical-lr emphasis line width = %v, want vertical-rl's %v", to, lrWidth, rlWidth)
		}
		// The mark space is on the right in both, so the text sits at the
		// same distance from the line's left edge.
		if lr["XY"].X != rl["XY"].X {
			t.Errorf("%s: vertical-lr emphasized text x = %v, want vertical-rl's %v", to, lr["XY"].X, rl["XY"].X)
		}
	}
}
