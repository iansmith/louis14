package layout

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/text"
)

// calculateLeadingSpace splits the leading between a line-height and a font
// height: the ascent side gets the floor of half of it, in whole pixels, and
// the descent side the rest, computed in LayoutUnits. Mirrors
// CalculateLeadingSpace (core/layout/inline/line_utils.cc:36-45 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which
// InlineBoxState::ComputeTextMetrics adds to the text metrics
// (inline_box_state.cc:148-155).
func calculateLeadingSpace(lineHeight, fontHeight float64) (ascentLeading, descentLeading float64) {
	leading := layoutunit.FromFloat64Round(lineHeight).Sub(layoutunit.FromFloat64Round(fontHeight))
	ascent := math.Floor(leading.DivInt(2).Float64())
	return ascent, leading.Float64() - ascent
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
