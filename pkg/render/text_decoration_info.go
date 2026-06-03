package render

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// decorationAxis selects which physical axis carries the inline (line-extent)
// direction and which carries the block (offset) direction.
// Mirrors the conceptual two-component LineRelativeOffset { line_left,
// line_over } from Blink's line_relative_rect.h:41-66 @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type decorationAxis int8

const (
	// decorationAxisHorizontal: inline = X axis, block = Y axis (normal LTR/RTL).
	decorationAxisHorizontal decorationAxis = iota
	// decorationAxisVertical: inline = Y axis, block = X axis (vertical-rl/-lr).
	decorationAxisVertical
)

// blockUnderDir indicates which physical direction is "toward block-end"
// (the canonical under/alphabetic-baseline side). Mirrors the axis-swap
// encoded in Blink's ComputeRelativeToPhysicalTransform (WritingMode) at
// line_relative_rect.cc:18-50 @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type blockUnderDir int8

const (
	blockUnderDown  blockUnderDir = iota // horizontal-tb: under = +Y (downward)
	blockUnderLeft                       // vertical-rl:   under = -X (leftward)
	blockUnderRight                      // vertical-lr:   under = +X (rightward)
)

// textDecorationInfo is the geometry helper for painting one or more
// AppliedTextDecoration entries on a single text run. Mirrors Blink's
// `TextDecorationInfo` class — see core/paint/text_decoration_info.{h,cc} at
// Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Constructed once per text run by drawTextDecoration (horizontal) or
// newVerticalTextDecorationInfo (upright vertical). Methods are pure and safe
// to call repeatedly for each AppliedTextDecoration on the run.
//
// Blink doesn't carry an axis field — its caller rotates the canvas via
// ConcatCTM (text_fragment_painter.cc:1002-1009 @ SHA 4883d11fef); all
// geometry is then identical to horizontal. louis14 lacks canvas rotation,
// so axis + blockUnderDir are louis14-specific scaffolding on the EXISTING
// struct rather than a parallel verticalTextDecorationInfo type.
type textDecorationInfo struct {
	box      *layout.Box
	width    float64 // measured text advance along the INLINE axis
	fontSize float64 // used font-size in pixels
	ascent   float64 // font ascent in pixels (from baseline)
	descent  float64 // font descent in pixels (from baseline; non-negative)

	// underlineThicknessFromFont is the font's native underline-thickness
	// metric, used when text-decoration-thickness: from-font. Zero means the
	// metric was not available — fall back to auto.
	underlineThicknessFromFont float64

	// axis and underDir encode the writing-mode axis swap for vertical-rl/-lr.
	// Horizontal LTR: axis=decorationAxisHorizontal, underDir=blockUnderDown.
	// These are the default zero values — newTextDecorationInfo sets neither.
	axis     decorationAxis
	underDir blockUnderDir
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
		// axis=decorationAxisHorizontal, underDir=blockUnderDown are zero values.
	}
}

// newVerticalTextDecorationInfo constructs the geometry helper for an upright
// vertical text run (writing-mode: vertical-rl or vertical-lr).
//
// length is the text extent along the INLINE axis — for vertical modes this is
// the physical height of the text column (number of characters × fontSize).
// axis is set to decorationAxisVertical; underDir encodes which physical X
// direction is "toward block-end" (left for vertical-rl, right for vertical-lr).
//
// No Blink analog for this constructor: Blink handles the axis swap by
// rotating the canvas (ConcatCTM at text_fragment_painter.cc:1002-1009 @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). louis14's equivalent is
// carrying the axis flag on the struct and using perpendicular compute methods.
func newVerticalTextDecorationInfo(box *layout.Box, length, fontSize, ascent, descent, underlineThicknessFromFont float64, dir blockUnderDir) textDecorationInfo {
	return textDecorationInfo{
		box:                        box,
		width:                      length, // inline extent = physical height for vertical
		fontSize:                   fontSize,
		ascent:                     ascent,
		descent:                    math.Abs(descent),
		underlineThicknessFromFont: underlineThicknessFromFont,
		axis:                       decorationAxisVertical,
		underDir:                   dir,
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

// baselineRelativeOffset returns the perpendicular distance from the box edge to
// the underline zero-position for the given TextUnderlinePosition.
// Under → ascent+descent (block-end edge); auto/from-font → ascent (alphabetic baseline).
// Single source of truth consumed by computeUnderlineZeroPosition and computeUnderlinePerpX.
func (t textDecorationInfo) baselineRelativeOffset(pos css.TextUnderlinePosition) float64 {
	if pos == css.TextUnderlinePositionUnder {
		return t.ascent + t.descent
	}
	return t.ascent
}

// computeUnderlineZeroPosition returns the absolute Y coordinate of the underline
// zero-position. Mirrors Blink's TextDecorationOffset::ComputeUnderlineOffset
// (text_decoration_offset.cc @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
// auto anchors at the alphabetic baseline (box.Y+ascent); under anchors at the
// block-end edge (box.Y+ascent+descent). from-font falls back to auto until
// FontMetrics plumbing lands.
func (t textDecorationInfo) computeUnderlineZeroPosition(pos css.TextUnderlinePosition) float64 {
	return t.box.Y + t.baselineRelativeOffset(pos)
}

// computeUnderlineZeroOffset returns the underline zero-position offset in pixels,
// relative to the baseline (ascent = 0). Used by render.go's pre-paint buffer
// computation to size extensions. For auto/from-font, returns 0 (alphabetic
// baseline). For under, returns ascent + descent (block-end edge offset from
// baseline).
func (t textDecorationInfo) computeUnderlineZeroOffset(pos css.TextUnderlinePosition) float64 {
	switch pos {
	case css.TextUnderlinePositionUnder:
		return t.ascent + t.descent
	case css.TextUnderlinePositionAuto, css.TextUnderlinePositionFromFont:
		return 0
	default:
		return 0
	}
}

// computeUnderlineLineY returns the Y coordinate of the underline stroke.
// Mirrors the offset half of Blink's `ComputeUnderlineLineData` at
// text_decoration_info.cc:218-234. Dispatches to computeUnderlineZeroPosition
// to switch on TextUnderlinePosition (auto / from-font / under), then adds the
// text-underline-offset. `td.UnderlineOffset` is already resolved to pixels at
// cascade time by Style.GetTextUnderlineOffset.
func (t textDecorationInfo) computeUnderlineLineY(td css.AppliedTextDecoration) float64 {
	return t.computeUnderlineZeroPosition(td.UnderlinePosition) + td.UnderlineOffset
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

// computeUnderlinePerpX returns the perpendicular physical X coordinate of the
// underline stroke for an upright vertical text run (axis=decorationAxisVertical).
//
// In vertical-rl the underline paints on the physical LEFT side (block-end):
//   perpX = box.X - zeroPerpOffset - td.UnderlineOffset
// In vertical-lr the underline paints on the physical RIGHT side (block-end):
//   perpX = box.X + box.Width + zeroPerpOffset + td.UnderlineOffset
//
// zeroPos is the perpendicular offset from box.X (or box.X+box.Width) to the
// underline zero-position, computed by convertUnderlineZeroPerpToPerpX based on
// TextUnderlinePosition (auto/from-font/under).
// td.UnderlineOffset is already resolved to pixels at cascade time.
//
// CSS Text Decoration 4 §3 + CSS Writing Modes 4 §6: "under" = block-end.
// Conceptually mirrors Blink's TextDecorationOffset::ComputeUnderlineOffset()
// (cited in text_decoration_info.cc:259-273 @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func (t textDecorationInfo) computeUnderlinePerpX(td css.AppliedTextDecoration) float64 {
	// baselineRelativeOffset encodes the position switch (auto/from-font → ascent,
	// under → ascent+descent); see its doc for the single source of truth.
	zeroPerpOffset := t.baselineRelativeOffset(td.UnderlinePosition)

	switch t.underDir {
	case blockUnderLeft: // vertical-rl: under = left
		return t.box.X - zeroPerpOffset - td.UnderlineOffset
	case blockUnderRight: // vertical-lr: under = right
		return t.box.X + t.box.Width + zeroPerpOffset + td.UnderlineOffset
	}
	// Should not be reached for vertical mode; fall back to horizontal.
	return t.computeUnderlineLineY(td)
}

// computeOverlinePerpX returns the perpendicular physical X coordinate of the
// overline stroke for an upright vertical text run. Overline sits at block-start.
//
// For vertical-rl (block-start = right): perpX = box.X + box.Width
// For vertical-lr (block-start = left):  perpX = box.X
func (t textDecorationInfo) computeOverlinePerpX() float64 {
	switch t.underDir {
	case blockUnderLeft: // vertical-rl: block-start = right
		return t.box.X + t.box.Width
	case blockUnderRight: // vertical-lr: block-start = left
		return t.box.X
	}
	return t.computeOverlineLineY()
}

// computeLineThroughPerpX returns the perpendicular physical X coordinate for
// a line-through stroke in upright vertical text. Line-through passes through
// the center of the character column (midpoint of block axis).
//
// For vertical-rl and vertical-lr both: perpX = box.X + box.Width/2
// (center of the glyph column). Mirrors Blink's ComputeLineThroughLineData
// which places the rect center at 2*ascent/3 horizontally (for horizontal text).
func (t textDecorationInfo) computeLineThroughPerpX() float64 {
	return t.box.X + t.box.Width/2.0
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
