package layout

import (
	"fmt"
	"testing"

	"louis14/pkg/text"
)

// Blink sizes a text fragment from its primary font's integer metrics, not
// the em box: the fragment's block size is int ascent + int descent and its
// line-over edge is the baseline's ascent away from the baseline
// (InlineBoxState::ComputeTextMetrics, core/layout/inline/inline_box_state.cc:
// 131-136 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b: text_top = -ascent,
// text_height = text_metrics.LineHeight(); placed by LogicalLineBuilder,
// core/layout/inline/logical_line_builder.cc:262-266 @ same SHA). The height
// is FontMetrics::Height() = int_ascent_ + int_descent_ for every baseline
// type (platform/fonts/font_metrics.h:109-125 @ same SHA).

// onlyTextBox lays src out and returns its single rendered text box.
func onlyTextBox(t *testing.T, src string) *Box {
	t.Helper()
	boxes := collectTextBoxes(layoutRootTestHTML(t, src), nil)
	if len(boxes) != 1 {
		t.Fatalf("got %d text boxes, want 1", len(boxes))
	}
	return boxes[0]
}

// textFragmentFontHeight is the primary font's int ascent + int descent, Blink's text
// fragment block size.
func fontHeight(b *Box) float64 {
	size, _, _, _, _ := fontPropsFromStyle(b.Style)
	path := resolveFontPath(b.Style, text.DefaultFontConfig())
	return text.FontAscentFromFont(size, path, nil) + text.FontDescentFromFont(size, path, nil)
}

func TestTextFragment_BlockSizeIsAscentPlusDescent(t *testing.T) {
	b := onlyTextBox(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div style="font: 16px serif">X</div>`)
	if want := fontHeight(b); b.Height != want {
		t.Errorf("text fragment height = %v, want ascent + descent = %v (not the 16px em box)", b.Height, want)
	}
}

// TestTextFragment_UprightVerticalBlockSize: in vertical text the central
// baseline's ascent and descent still sum to FontMetrics::Height()
// (font_metrics.cc:193-194 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b:
// Height() - Height() / 2), so the fragment is ascent + descent wide.
func TestTextFragment_UprightVerticalBlockSize(t *testing.T) {
	b := onlyTextBox(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div style="writing-mode: vertical-rl; font: 16px serif">X</div>`)
	if want := fontHeight(b); b.Width != want {
		t.Errorf("text fragment width = %v, want ascent + descent = %v", b.Width, want)
	}
}

// TestTextFragment_SidewaysVerticalLRMatchesRL: line-over is the physical
// right in both vertical-rl and vertical-lr, and a single line's leading is
// split the same way in both, so a sideways text fragment occupies the same
// physical rect in each (Chrome renders text-emphasis-position-property-005a
// and -005d identically).
func TestTextFragment_SidewaysVerticalLRMatchesRL(t *testing.T) {
	const doc = `<!DOCTYPE html><style>body { margin: 0 }</style>
<div style="writing-mode: %s; text-orientation: sideways; line-height: 5; font: 16px serif">X</div>`
	rl := onlyTextBox(t, fmt.Sprintf(doc, "vertical-rl"))
	lr := onlyTextBox(t, fmt.Sprintf(doc, "vertical-lr"))
	if lr.X != rl.X || lr.Width != rl.Width {
		t.Errorf("vertical-lr text fragment x=%v w=%v, want vertical-rl's x=%v w=%v", lr.X, lr.Width, rl.X, rl.Width)
	}
	if want := fontHeight(rl); rl.Width != want {
		t.Errorf("text fragment width = %v, want ascent + descent = %v", rl.Width, want)
	}
}
