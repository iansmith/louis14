package layout

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/text"
)

// calculateLeadingSpace splits the leading between a line-height and a font
// height: the line-over side gets the floor of half of it, in whole pixels,
// and the line-under side the rest, computed in LayoutUnits. Mirrors
// CalculateLeadingSpace (core/layout/inline/line_utils.cc:36-45 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which
// InlineBoxState::ComputeTextMetrics adds to the text metrics
// (inline_box_state.cc:148-155). louis14 measures a line's ascent from its
// block-start, which is line-over except in the flipped-lines writing mode
// (WritingDirectionMode.IsFlippedLines); there the floored half goes to the
// descent (block-end) side.
func calculateLeadingSpace(lineHeight, fontHeight float64, wdm WritingDirectionMode) (ascentLeading, descentLeading float64) {
	leading := layoutunit.FromFloat64Round(lineHeight).Sub(layoutunit.FromFloat64Round(fontHeight))
	over := math.Floor(leading.DivInt(2).Float64())
	under := leading.Float64() - over
	if wdm.IsFlippedLines() {
		return under, over
	}
	return over, under
}

// usedLineHeight is style's line height for a run at fontSize: the
// computed line-height, or, for line-height: normal outside a central
// baseline, the font's line spacing (text.FontHeightFromFont) of the font
// fontPath resolves.
func usedLineHeight(style *css.Style, fontSize float64, fonts text.FontConfig, centralBaseline bool, fontPath func() string) float64 {
	if style.IsLineHeightNormal() && !centralBaseline {
		return text.FontHeightFromFont(fontSize, fontPath(), fonts.Registry)
	}
	return style.GetLineHeight()
}
