package layout

import "testing"

// An inline box's own metrics, even an empty one's, are its primary font's
// (LogicalLineBuilder::HandleOpenTag → InlineBoxState::ComputeTextMetrics,
// which measures Font::PrimaryFont(): core/layout/inline/
// logical_line_builder.cc:355-370, inline_box_state.cc:116-131 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), the first face covering
// U+0020, as for its text. XOnly is Liberation Serif limited to U+0058, so
// "XOnly, Ahem" has Ahem as its primary font: the empty span's box is
// Ahem's (ascent 32, descent 8 at 40px), and with a 40px Liberation Serif
// "X" beside it (ascent ~33.5, descent ~6.5 at line-height 40px) the line
// is as tall as with a plain Ahem span.
func TestInlineBoxStrut_UsesPrimaryFont(t *testing.T) {
	tb, rb := layoutWithFontFace(t, `@font-face { font-family: XOnly; src: url(LiberationSerif-Regular.ttf); unicode-range: U+58 }`, "XOnly", `<!doctype html><style>
body { margin: 0 }
div { font: 10px/10px Ahem }
.x { font: 40px/40px "Liberation Serif" }
</style>
<div id=t><span style="font: 40px/40px XOnly, Ahem"></span><span class=x>X</span></div>
<div id=r><span style="font: 40px/40px Ahem"></span><span class=x>X</span></div>`)
	if tb.Height != rb.Height {
		t.Errorf("line height %v, want the plain-Ahem span's %v", tb.Height, rb.Height)
	}
}
