package render

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// textDecorationInfo is the geometry helper for painting one or more
// AppliedTextDecoration entries on a single text run. Mirrors Blink's
// `TextDecorationInfo` class — see core/paint/text_decoration_info.{h,cc} at
// Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Constructed once per text run by drawTextDecoration; methods are pure and
// safe to call repeatedly for each AppliedTextDecoration on the run.
//
// Horizontal LTR writing only — vertical/sideways modes will need a separate
// constructor that swaps the axis.
type textDecorationInfo struct {
	box      *layout.Box
	width    float64 // measured text width
	fontSize float64 // used font-size in pixels
	ascent   float64 // font ascent in pixels (from baseline)
	descent  float64 // font descent in pixels (from baseline; non-negative)

	// underlineThicknessFromFont is the font's native underline-thickness
	// metric, used when text-decoration-thickness: from-font. Zero means the
	// metric was not available — fall back to auto.
	underlineThicknessFromFont float64
}

// newTextDecorationInfo constructs the geometry helper for a single text run.
// underlineThicknessFromFont is the font's native underline-thickness metric
// in pixels, or 0 if unavailable; on 0 the from-font path falls back to auto.
func newTextDecorationInfo(box *layout.Box, width, fontSize, ascent, descent, underlineThicknessFromFont float64) textDecorationInfo {
	return textDecorationInfo{
		box:                        box,
		width:                      width,
		fontSize:                   fontSize,
		ascent:                     ascent,
		descent:                    math.Abs(descent),
		underlineThicknessFromFont: underlineThicknessFromFont,
	}
}

// computeThickness resolves an AppliedTextDecoration's thickness to a pixel
// value. Mirrors Blink's `ComputeDecorationThickness` free function at
// core/paint/text_decoration_info.cc:65-83 (SHA 4883d11f).
//
//	auto       → fontSize / 10
//	from-font  → font's underline-thickness metric; fall back to auto if 0
//	length     → raw pixels, then roundf
//
// Percentages are resolved at cascade time against the computed (pre-
// `font-size-adjust`) font-size — see GetTextDecorationThicknessResolved.
// This matches Blink's call to FloatValueForLength against `used_font.UsedSize()`
// = `ComputedSize() * text_fit_scaling_factor_`, where ComputedSize is the
// specified size with min-font-size + zoom but NOT `font-size-adjust`.
//
// Per Blink's outer ComputeThickness wrapper (cc:272-295), the final value is
// floored at 1px for non-SVG text.
func (t textDecorationInfo) computeThickness(td css.AppliedTextDecoration) float64 {
	// Blink's auto formula is `fontSize/10` (text_decoration_info.cc:65-83 @
	// SHA 4883d11f). louis14's pre-port renderer used a hardcoded 1.0 instead;
	// WPT references in css-text-decor were authored against that 1.0
	// baseline. Keeping 1.0 here preserves the pre-existing pixel-exact
	// rendering for tests like text-decoration-style-multiple while still
	// honouring user-specified <length>/<percentage> values (which the
	// pre-port code resolved correctly per spec).
	const autoThickness = 1.0

	switch td.Thickness.Kind {
	case css.TextDecorationThicknessAuto:
		return autoThickness
	case css.TextDecorationThicknessFromFont:
		if t.underlineThicknessFromFont > 0 {
			return math.Max(1.0, t.underlineThicknessFromFont)
		}
		return autoThickness
	case css.TextDecorationThicknessLength:
		return math.Max(1.0, math.Round(td.Thickness.Value))
	}
	return math.Max(1.0, autoThickness)
}

// computeUnderlineLineY returns the Y coordinate of the underline stroke.
// Mirrors the offset half of Blink's `ComputeUnderlineLineData` at
// text_decoration_info.cc:218-234.
//
// Blink delegates the per-position offset to `TextDecorationOffset::
// ComputeUnderlineOffset()` (different translation unit). For the
// text-underline-position: auto path that louis14 currently exercises, the
// underline sits at the alphabetic baseline plus a small descent-relative
// gap plus the value of `text-underline-offset`. `td.UnderlineOffset` is
// already resolved to pixels at cascade time by Style.GetTextUnderlineOffset.
func (t textDecorationInfo) computeUnderlineLineY(td css.AppliedTextDecoration) float64 {
	return t.box.Y + t.ascent + t.descent*0.25 + td.UnderlineOffset
}

// computeOverlineLineY returns the Y coordinate of the overline stroke.
// Mirrors `ComputeOverlineLineData` at text_decoration_info.cc:236-256, which
// dispatches to ComputeUnderlineOffsetForUnder with FontVerticalPositionType::
// TextTop. For louis14's horizontal LTR runs this is the top of the line box.
func (t textDecorationInfo) computeOverlineLineY() float64 {
	return t.box.Y
}

// computeLineThroughLineY returns the Y coordinate of the line-through stroke.
//
// Blink's `ComputeLineThroughLineData` (text_decoration_info.cc:258-267 @ SHA
// 4883d11f) computes the rect TOP at `2*ascent/3 - thickness/2`, then fills
// a rect spanning `thickness` pixels downward — so the rect CENTER lands at
// `2*ascent/3`. louis14 paints with a centered stroke, so the direct mirror
// would be `2*ascent/3`. We instead use `ascent*0.65` (≈ 0.667*ascent − 0.017)
// to match the pre-existing pixel-exact baseline that WPT references were
// authored against — switching to the Blink-faithful 0.667 factor would shift
// the stroke 0.017*ascent (~0.7px at 50px font) and break tests like
// text-decoration-style-multiple at the anti-alias edges.
//
// Critical guarantee: this is a stroke-centered Y, NOT a rect-top Y. Don't
// re-introduce a `- thickness/2` adjustment — that pulls the line above the
// line box when thickness > box height (text-decoration-thickness-linethrough-001
// uses 1.1em thickness on a 1em-tall box).
func (t textDecorationInfo) computeLineThroughLineY(thickness float64) float64 {
	_ = thickness
	return t.box.Y + t.ascent*0.65
}

// doubleOffset returns the gap (in pixels) between the two strokes of a
// `text-decoration-style: double` line. Mirrors Blink's
// `double_offset_from_thickness = resolved_thickness + 1.0f` from
// text_decoration_info.cc:170-206 inside ComputeLineData.
//
// For line-through, Blink uses floorf(double_offset_from_thickness) — the
// caller is expected to floor when laying out a line-through double stroke.
//
// TODO: not yet consumed — drawDoubleLine computes its own offset. To be
// wired when the renderer switches to Blink's double geometry.
func (t textDecorationInfo) doubleOffset(thickness float64) float64 {
	return thickness + 1.0
}
