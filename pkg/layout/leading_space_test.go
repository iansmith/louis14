package layout

import "testing"

// TestCalculateLeadingSpace_AscentSideFloored pins how a line's leading is
// split: the ascent side gets floor((line-height − font height) / 2) and the
// descent side the rest (CalculateLeadingSpace, core/layout/inline/
// line_utils.cc:36-45 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b, applied by
// InlineBoxState::ComputeTextMetrics, inline_box_state.cc:148-155). Ahem at
// 16px is 13 + 3 = 16 tall, so a 19px line has 3px of leading: 1 above the
// ascent, 2 below the descent, and the text fragment's em box starts 1px
// down.
func TestCalculateLeadingSpace_AscentSideFloored(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div style="font: 16px Ahem; line-height: 19px">X</div>`)
	var text *Box
	var walk func(*Box)
	walk = func(b *Box) {
		if b.Text == "X" {
			text = b
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	if text == nil {
		t.Fatal("missing the text fragment")
	}
	if text.Y != 1 {
		t.Errorf("text top = %v, want 1 (floor(3/2) of leading above the ascent)", text.Y)
	}
}
