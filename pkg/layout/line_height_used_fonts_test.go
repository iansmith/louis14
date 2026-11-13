package layout

import "testing"

// Under line-height: normal a text run's line box unites the primary
// font's box with every font the run is drawn in, each with that font's
// own line-spacing leading (InlineBoxState::ComputeTextMetrics /
// AccumulateUsedFonts, core/layout/inline/inline_box_state.cc:116-162,
// :248-264; LogicalLineBuilder, logical_line_builder.cc:239-240 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). SpaceOnly is Ahem limited to
// U+0020, so it is the primary font of "SpaceOnly, Liberation Serif" but
// "XXX" is drawn in Liberation Serif; the line is as tall as Liberation
// Serif's own normal line, not Ahem's line spacing laid over Liberation
// Serif's glyph box.
func TestLineHeightNormal_UnitesUsedFontLeading(t *testing.T) {
	tb, rb := layoutWithFontFace(t, `@font-face { font-family: SpaceOnly; src: url(Ahem.ttf); unicode-range: U+20 }`, "SpaceOnly", `<!doctype html><style>
body { margin: 0 }
#t { font: 20px SpaceOnly, "Liberation Serif"; line-height: normal }
#r { font: 20px "Liberation Serif"; line-height: normal }
</style><div id=t>XXX</div><div id=r>XXX</div>`)
	if tb.Height != rb.Height {
		t.Errorf("line height %v, want Liberation Serif's normal line height %v", tb.Height, rb.Height)
	}
}
