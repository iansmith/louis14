package layout

import (
	"testing"

	"louis14/pkg/text"
)

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
	text := findTextBox(root, "X")
	if text == nil {
		t.Fatal("missing the text fragment")
	}
	if text.Y != 1 {
		t.Errorf("text top = %v, want 1 (floor(3/2) of leading above the ascent)", text.Y)
	}
}

// TestCalculateLeadingSpace_VerticalLRFloorsTheRightSide pins the split in
// vertical-lr, Blink's one flipped-lines writing mode
// (IsFlippedLinesWritingMode, platform/text/writing_mode.h:67-69 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the ascent side of
// CalculateLeadingSpace is line-over, the physical right, which is the
// line's block-end. So the floored half of Ahem 16px's 3px leading on a
// 19px line goes on the right, and the text starts 2px from the line's
// left edge.
func TestCalculateLeadingSpace_VerticalLRFloorsTheRightSide(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div style="writing-mode: vertical-lr; font: 16px Ahem; line-height: 19px">X</div>`)
	text := findTextBox(root, "X")
	if text == nil {
		t.Fatal("missing the text fragment")
	}
	if text.X != 2 {
		t.Errorf("text left = %v, want 2 (the floored 1px of leading on the right)", text.X)
	}
}

// findTextBox returns the first text box under b whose text is s.
func findTextBox(b *Box, s string) *Box {
	if b.Text == s {
		return b
	}
	for _, c := range b.Children {
		if f := findTextBox(c, s); f != nil {
			return f
		}
	}
	return nil
}

// TestApplyInitialLetter_LeadingNotFlippedInVerticalLR: the initial letter's
// paragraph line descent takes CalculateLeadingSpace's descent share, which
// Blink computes without reference to the writing mode (core/layout/inline/
// line_utils.cc:36-45 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the
// paragraph's ascent and descent are line-relative, so vertical-lr's
// flipped-lines mapping does not apply. Ahem 16px (13 + 3) on a 19px line has
// 3px of leading: 1 over, 2 under, in every writing mode.
func TestApplyInitialLetter_LeadingNotFlippedInVerticalLR(t *testing.T) {
	fc := text.DefaultFontConfig()
	marginTop := func(wm string) string {
		para := newFontTestStyle(fc, map[string]string{"font-family": "Ahem", "font-size": "16px", "line-height": "19px", "writing-mode": wm})
		fl := newFontTestStyle(fc, map[string]string{"font-family": "Ahem", "font-size": "16px", "initial-letter": "2"})
		(&LayoutTreeBuilder{fontConfig: fc}).applyInitialLetter(fl, para)
		v, _ := fl.Get("margin-top")
		return v
	}
	if h, v := marginTop("horizontal-tb"), marginTop("vertical-lr"); v != h {
		t.Errorf("vertical-lr initial-letter margin-top = %q, want horizontal-tb's %q", v, h)
	}
}
