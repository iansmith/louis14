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
// (inline_box_state.cc:148-155). Like Blink's, it is line-relative and takes
// no writing mode.
func calculateLeadingSpace(lineHeight, fontHeight float64) (overLeading, underLeading float64) {
	leading := layoutunit.FromFloat64Round(lineHeight).Sub(layoutunit.FromFloat64Round(fontHeight))
	overLeading = math.Floor(leading.DivInt(2).Float64())
	return overLeading, leading.Float64() - overLeading
}

// blockLeadingSpace is calculateLeadingSpace mapped onto louis14's line
// metrics, which measure from the line's block-start
// (WritingDirectionMode.lineToBlock).
func blockLeadingSpace(lineHeight, fontHeight float64, wdm WritingDirectionMode) (ascentLeading, descentLeading float64) {
	return wdm.lineToBlock(calculateLeadingSpace(lineHeight, fontHeight))
}

// usedLineHeight is style's line height for a run at fontSize: the
// computed line-height, or, for line-height: normal, the line spacing
// (text.FontHeightFromFont) of the font at fontPath, whatever the
// baseline (ComputedStyle::ComputedLineHeightAsFixed →
// FontMetrics::FixedLineSpacing, core/style/computed_style.cc:2440-2449 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func usedLineHeight(style *css.Style, fontSize float64, fontPath string, reg *text.FontRegistry) float64 {
	if style.IsLineHeightNormal() {
		return text.FontHeightFromFont(fontSize, fontPath, reg)
	}
	return style.GetLineHeight()
}
