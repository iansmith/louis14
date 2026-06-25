package svg

import (
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGUnitType enumerates the coordinate-space resolution mode for an
// SVG resource element's geometry attributes. Mirrors Blink's
// SVGUnitTypes::SVGUnitType (core/svg/svg_unit_types.h).
//
// Per SVG 1.1 §13.2.3 the modes determine how the resource's geometry
// attributes (gradient endpoints, pattern tile rect, mask box, clip
// box) are interpreted:
//
//   - SVGUnitObjectBoundingBox: coordinates are fractions of the
//     referencing element's object bounding box. (0,0) is the
//     top-left of the bbox; (1,1) is the bottom-right.
//   - SVGUnitUserSpaceOnUse: coordinates are in the user-space
//     coordinate system of the referencing element at the moment
//     the resource is applied.
//
// gradientUnits/maskUnits/clipPathUnits default to objectBoundingBox;
// patternContentUnits defaults to userSpaceOnUse (the reverse). The
// per-element default is established by the constructor.
type SVGUnitType int

const (
	SVGUnitObjectBoundingBox SVGUnitType = iota
	SVGUnitUserSpaceOnUse
)

// SVGSpreadMethod enumerates how gradient colors extend past the [0,1]
// stop-offset range. Mirrors Blink's SVGSpreadMethodType
// (core/svg/svg_gradient_element.h):
//
//   - SVGSpreadPad (default): clamp to the end-stop color (the SVG
//     equivalent of CSS gradient's "extend").
//   - SVGSpreadReflect: mirror at every integer boundary.
//   - SVGSpreadRepeat: tile (modulo 1).
type SVGSpreadMethod int

const (
	SVGSpreadPad SVGSpreadMethod = iota
	SVGSpreadReflect
	SVGSpreadRepeat
)

// SVGGradientStop is a single parsed `<stop>` child of a gradient
// element. Mirrors Blink's SVGGradientStop (core/svg/svg_stop_element.h)
// and what SVGGradientStop::ToCSSGradientStop produces for the CSS
// gradient ramp.
//
// `Offset` is clamped to [0, 1] at parse time (SVG 2 §13.4.2: out-of-range
// offsets are clamped to the [0, 1] range AND non-monotonic offsets are
// clamped to the previous stop's offset). The painter receives a sorted,
// clamped, monotonic list and can rely on it.
//
// `Color` already has `stop-opacity` multiplied into `Color.A` — the
// painter does not need to compose the two separately. This mirrors how
// Blink's SVGGradientStop::Color() returns the pre-multiplied color.
type SVGGradientStop struct {
	Offset float64
	Color  css.Color
}

// SVGResourcePaintServer is the interface implemented by every SVG
// resource element that can supply a fill or stroke. Mirrors Blink's
// SVGResourcePaintServer interface + LayoutSVGResourceContainer
// abstract base (core/layout/svg/layout_svg_resource_*.{h,cc}).
//
// The interface is intentionally small: ID() identifies the resource
// for registry lookup, and the painter side (pkg/render) walks the
// concrete type to build the per-pixel shader. The Prepare* methods on
// the SVGResourcePaintServer interface are intentionally NOT defined
// here — the louis14 DrawContext doesn't expose Skia-style "shader on
// PaintFlags" objects, so the render-side painter (svg_paint_server.go)
// reads the concrete server type directly and builds a sampling
// function. This keeps the layout package free of *image.RGBA imports
// and the render package free of Phase 6-style "registry callback" hooks.
//
// Phase 4: registry lookup is by ID + concrete type assertion at the
// render-side painter; Phase 6 formalizes the registry with cycle
// detection (LayoutSVGResourceContainer::FindCycle in Blink) — paint
// servers now also satisfy the umbrella `SVGResource` so the registry
// can store them alongside clippers / maskers / (Phase 7) filters in a
// single id-namespace.
type SVGResourcePaintServer interface {
	SVGNode
	SVGResource
}

// gradientCommon holds the geometry-mode and spread settings shared by
// both linear and radial gradients. Mirrors the slice of state that
// LayoutSVGResourceGradient pulls out of GradientAttributes in Blink
// (core/layout/svg/layout_svg_resource_gradient.{h,cc}).
type gradientCommon struct {
	// Stops are the parsed `<stop>` children, sorted by offset with
	// stop-opacity premultiplied into Color.A. Empty list → SVG 1.1
	// §13.2.4 says "treat as paint server with no effect" — Phase 4
	// implements this as "skip paint" at the render-side server lookup.
	Stops []SVGGradientStop

	// GradientUnits is the coordinate-space mode for the gradient
	// endpoints. Default objectBoundingBox per SVG 1.1 §13.2.3.
	GradientUnits SVGUnitType

	// GradientTransform is the parsed `gradientTransform` attribute
	// applied to the gradient AFTER the units-mode mapping. Identity
	// when absent. Uses the same parser as the SVG `transform`
	// attribute (ParseSVGTransform).
	GradientTransform geometry.AffineTransform

	// SpreadMethod controls behaviour outside the [0,1] offset range.
	SpreadMethod SVGSpreadMethod

	// ResourceID is the gradient's `id` attribute. Empty if absent.
	ResourceID string

	// elementStyle is the gradient element's resolved style. Currently
	// unused by the painter but kept for parity with the Blink
	// abstraction (which carries ComputedStyle on every LayoutObject).
	elementStyle *css.Style

	// cycleState carries the cycle-detection state machine for the
	// containing resource. Initialized to SVGCycleNeedCheck (zero value).
	// Phase 6: the resource registry's cycle solver flips this through
	// the {NeedCheck, PerformingCheck, HasCycle, NoCycle} states once
	// per paint frame.
	cycleState SVGCycleState
}

// CycleState implements SVGResource: returns the gradient's
// cycle-detection state. Phase 6 — see svg_resource.go.
func (g *gradientCommon) CycleState() SVGCycleState { return g.cycleState }

// SetCycleState implements SVGResource.
func (g *gradientCommon) SetCycleState(s SVGCycleState) { g.cycleState = s }

// SVGLinearGradient is the layout node for a `<linearGradient>` resource
// element. Mirrors Blink's LayoutSVGResourceLinearGradient
// (core/layout/svg/layout_svg_resource_linear_gradient.{h,cc}).
//
// The geometry attributes (x1, y1, x2, y2) are stored unresolved as
// raw strings + parsed lengths because their resolution depends on the
// reference box at paint time (objectBoundingBox vs userSpaceOnUse).
// The render-side painter resolves them once the reference rect is known.
type SVGLinearGradient struct {
	gradientCommon

	// X1, Y1 — start point of the gradient line. Defaults per SVG 1.1
	// §13.2.2: x1=0%, y1=0%.
	// X2, Y2 — end point. Defaults: x2=100%, y2=0%.
	//
	// Values are stored as the raw attribute string AND a pre-parsed
	// float when objectBoundingBox is in use (the fraction). For
	// userSpaceOnUse the value is a length resolved at paint time
	// against the current viewport — kept as raw attribute so the
	// length context at paint time can resolve it correctly. The
	// `RawX1` form wins when present; otherwise the parsed default
	// applies.
	X1, Y1 SVGGradientLength
	X2, Y2 SVGGradientLength
}

// ID implements SVGResourcePaintServer.
func (g *SVGLinearGradient) ID() string { return g.ResourceID }

// ObjectBoundingBox returns an empty bbox — gradient resource elements
// do not contribute to the SVG tree's paint bounds. Mirrors Blink's
// LayoutSVGHiddenContainer::ObjectBoundingBox (always empty).
func (g *SVGLinearGradient) ObjectBoundingBox() geometry.RectF {
	return geometry.RectF{}
}

// LocalTransform returns identity — gradient elements are never in the
// paint tree, so their LocalTransform is irrelevant. Mirrors how
// LayoutSVGHiddenContainer ignores its own transform attribute.
func (g *SVGLinearGradient) LocalTransform() geometry.AffineTransform {
	return geometry.Identity()
}

// UpdateSVGLayout is a no-op for gradient resource elements.
func (g *SVGLinearGradient) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	return SVGLayoutResult{}
}

// Paint is a no-op — the render-side painter resolves paint servers
// via the registry, not via the SVG tree walk. Mirrors the
// staleness-note convention established in Phase 3 (SVGNode.Paint is a
// no-op everywhere; render-side dispatches directly).
func (g *SVGLinearGradient) Paint(ctx *SVGPaintContext) {}

// Kind implements SVGResource — Phase 6.
func (g *SVGLinearGradient) Kind() SVGResourceKind { return SVGResourceKindLinearGradient }

// SVGRadialGradient is the layout node for a `<radialGradient>` resource
// element. Mirrors Blink's LayoutSVGResourceRadialGradient
// (core/layout/svg/layout_svg_resource_radial_gradient.{h,cc}).
//
// SVG 2 §13.4.3 introduces `fr` (the focal radius) for an inner ring
// around (fx, fy) that produces a focal-cone effect; fr=0 reproduces
// SVG 1.1 behavior (focal point on a single pixel).
type SVGRadialGradient struct {
	gradientCommon

	// Cx, Cy — center of the largest enclosing circle. Defaults
	// cx=50%, cy=50% per SVG 1.1 §13.2.3.
	Cx, Cy SVGGradientLength

	// R — radius of the largest enclosing circle. Default r=50%.
	R SVGGradientLength

	// Fx, Fy — focal point inside the largest circle. Defaults to
	// (cx, cy) if absent per SVG 1.1 §13.2.3.
	Fx, Fy SVGGradientLength
	HasFx  bool
	HasFy  bool

	// Fr — focal-radius (SVG 2 §13.4.3). Default 0.
	Fr SVGGradientLength
}

// ID implements SVGResourcePaintServer.
func (g *SVGRadialGradient) ID() string                                    { return g.ResourceID }
func (g *SVGRadialGradient) ObjectBoundingBox() geometry.RectF             { return geometry.RectF{} }
func (g *SVGRadialGradient) LocalTransform() geometry.AffineTransform      { return geometry.Identity() }
func (g *SVGRadialGradient) UpdateSVGLayout(SVGLayoutInfo) SVGLayoutResult { return SVGLayoutResult{} }
func (g *SVGRadialGradient) Paint(*SVGPaintContext)                        {}

// Kind implements SVGResource — Phase 6.
func (g *SVGRadialGradient) Kind() SVGResourceKind { return SVGResourceKindRadialGradient }

// SVGPattern is the layout node for a `<pattern>` resource element.
// Mirrors Blink's LayoutSVGResourcePattern
// (core/layout/svg/layout_svg_resource_pattern.{h,cc}).
//
// A pattern tiles its child subtree across the target's geometry.
// Per SVG 1.1 §13.3 the tile rect (x, y, width, height) is resolved in
// `patternUnits` space, the child subtree's user-space coords are
// resolved in `patternContentUnits` space, and the optional viewBox /
// preserveAspectRatio scale the children inside each tile.
type SVGPattern struct {
	// TileX, TileY, TileWidth, TileHeight — the tile rect.
	// Defaults per SVG 1.1 §13.3: x=0, y=0, width=0, height=0 (a 0-sized
	// tile produces no paint).
	TileX, TileY          SVGGradientLength
	TileWidth, TileHeight SVGGradientLength

	// PatternUnits resolves the tile rect; default objectBoundingBox.
	PatternUnits SVGUnitType

	// PatternContentUnits resolves the child subtree's user-space
	// coordinates; default userSpaceOnUse (the reverse of PatternUnits).
	PatternContentUnits SVGUnitType

	// PatternTransform is the parsed `patternTransform` attribute,
	// applied AFTER the units-mode mapping. Identity when absent.
	PatternTransform geometry.AffineTransform

	// ViewBox / PreserveAspectRatio scale the children inside the tile.
	// When ViewBox.Valid is false, no extra scaling is applied (the
	// children paint in patternContentUnits space directly).
	ViewBox             ViewBox
	PreserveAspectRatio PreserveAspectRatio

	// Children are the pattern's child subtree, built via BuildSVGTree
	// — same machinery as the main SVG paint tree. Painted into the
	// tile via the render-side pattern painter.
	Children []SVGNode

	// ResourceID is the pattern's `id` attribute.
	ResourceID string

	elementStyle *css.Style

	// cycleState carries the cycle-detection state machine; flipped
	// by the SVGResourcesCycleSolver DFS once per paint frame.
	cycleState SVGCycleState
}

func (p *SVGPattern) ID() string                                    { return p.ResourceID }
func (p *SVGPattern) ObjectBoundingBox() geometry.RectF             { return geometry.RectF{} }
func (p *SVGPattern) LocalTransform() geometry.AffineTransform      { return geometry.Identity() }
func (p *SVGPattern) UpdateSVGLayout(SVGLayoutInfo) SVGLayoutResult { return SVGLayoutResult{} }
func (p *SVGPattern) Paint(*SVGPaintContext)                        {}

// Kind implements SVGResource — Phase 6.
func (p *SVGPattern) Kind() SVGResourceKind { return SVGResourceKindPattern }

// CycleState implements SVGResource — Phase 6.
func (p *SVGPattern) CycleState() SVGCycleState { return p.cycleState }

// SetCycleState implements SVGResource — Phase 6.
func (p *SVGPattern) SetCycleState(s SVGCycleState) { p.cycleState = s }

// SVGGradientLength stores a single positional attribute on a gradient
// or pattern resource element. Holds both the raw attribute string and
// (for the objectBoundingBox path) a pre-parsed numeric value as a
// fraction [0,1] (or outside that range — Blink doesn't clamp until
// the paint-time conversion). For the userSpaceOnUse path the raw
// string is resolved at paint time against the active SVGLengthContext.
//
// Why not just a float? Because the same attribute is interpreted
// differently depending on the unit mode, and the unit mode is on the
// gradient element itself — not on the length. The painter chooses
// which form to use once it knows the mode.
type SVGGradientLength struct {
	// Raw is the original attribute text (already trimmed). Empty
	// when the attribute was absent on the element.
	Raw string
	// Value is the parsed numeric value (signed, scientific notation
	// accepted). For percent-suffixed values this is the percent
	// number / 100. For length-suffixed values without unit it is
	// the raw number. The exact resolution rule depends on the unit
	// mode — see resolveGradientLength.
	Value float64
	// IsPercent reports whether the original value carried a `%`
	// suffix. Used by the render-side painter to distinguish
	// `x1="50%"` (percent of bbox / viewport) from `x1="50"` (50 user
	// units) in userSpaceOnUse mode.
	IsPercent bool
	// HasValue reports whether the attribute was present on the
	// element. False values use the spec default at paint time.
	HasValue bool
	// AbsPx carries the additive absolute (px / user-unit) term of a
	// `calc(P% + Lpx)` expression. For a plain length or plain percent
	// this is 0 and the value lives in `Value`. For a calc mixing a
	// percent and a length, `Value` holds the percent fraction
	// (IsPercent=true) and `AbsPx` holds the length term in user units.
	// Mirrors how Blink's CSSMathExpressionNode keeps the percent and
	// length parts of a calc separate so each resolves against its own
	// basis (percent → axis basis, length → user units).
	AbsPx float64
}

// parseSVGCalc parses a `calc(...)` expression for an SVG geometry
// length attribute (filter region x/y/width/height, gradient/pattern
// coords). It supports a sum/difference of one percent term and one
// length term — the shape WPT's filter-region-calc tests exercise (e.g.
// `calc(25% + 0.25px)`). Returns (length, true) on a recognized calc, or
// (_, false) when the input is not a calc expression so the caller falls
// through to the plain length/percent path.
//
// Resolution model mirrors Blink's CSSMathExpressionNode: the percent
// and length terms are kept separate (Value holds the percent fraction,
// AbsPx holds the length term in user units) so each resolves against
// its own basis at ResolveAgainst time. Per the SVG2 coords
// objectBoundingBox model both terms become fractions of the unit
// square there; in userSpaceOnUse the percent resolves against the
// viewport and the length stays in user units.
//
// Font-relative terms (em/ex) inside the calc are converted using the
// supplied fontSize, matching the bare-length path in
// parseGradientLengthWithFontSize.
func parseSVGCalc(value string, fontSize float64) (SVGGradientLength, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(lower, "calc(") || !strings.HasSuffix(lower, ")") {
		return SVGGradientLength{}, false
	}
	inner := strings.TrimSpace(value[len("calc(") : len(value)-1])
	if inner == "" {
		return SVGGradientLength{}, false
	}

	// CSS calc() requires whitespace around the binary +/- operators
	// (CSS Values 4 §10.1), so we tokenize on whitespace and read the
	// stream as: term (op term)*. We only support a flat sum/difference
	// of percent and length terms — the shape the filter-region calc
	// tests exercise; `*`, `/`, nested parens, and bare unspaced signs
	// bail so the caller's plain-length path can attempt a best effort.
	tokens := strings.Fields(inner)
	if len(tokens) == 0 || len(tokens)%2 == 0 {
		// Even count means a dangling operator or empty term.
		return SVGGradientLength{}, false
	}
	r := SVGGradientLength{Raw: value, HasValue: true}
	sign := 1.0
	for idx, tok := range tokens {
		if idx%2 == 1 {
			switch tok {
			case "+":
				sign = 1.0
			case "-":
				sign = -1.0
			default:
				return SVGGradientLength{}, false
			}
			continue
		}
		percent, abs, isPercent, ok := parseSVGCalcTerm(tok, fontSize)
		if !ok {
			return SVGGradientLength{}, false
		}
		if isPercent {
			r.Value += sign * percent
			r.IsPercent = true
		} else {
			r.AbsPx += sign * abs
		}
	}
	return r, true
}

// parseSVGCalcTerm parses a single calc term into either a percent
// fraction or an absolute length (in user units). `isPercent` reports
// which form the term produced: a `%` term sets isPercent and fills
// `percent`; every other unit fills `abs`.
func parseSVGCalcTerm(term string, fontSize float64) (percent, abs float64, isPercent, ok bool) {
	if strings.HasSuffix(term, "%") {
		num := strings.TrimSpace(strings.TrimSuffix(term, "%"))
		v, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, 0, false, false
		}
		return v / 100.0, 0, true, true
	}
	if numStr, unit, fontRel := parseFontRelativeUnit(term); fontRel {
		v, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, 0, false, false
		}
		switch unit {
		case "em":
			return 0, v * fontSize, false, true
		case "ex":
			return 0, v * fontSize * 0.5, false, true
		}
	}
	num, unit := splitNumberAndUnit(term)
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, 0, false, false
	}
	switch strings.ToLower(unit) {
	case "", "px":
		return 0, v, false, true
	default:
		// Unknown unit inside calc — reject so the caller's plain
		// length path can attempt a best-effort parse.
		return 0, 0, false, false
	}
}

// parseGradientLength parses a single gradient/pattern coordinate
// attribute into an SVGGradientLength. Empty strings produce a
// !HasValue result so the caller can fall back to the spec default.
func parseGradientLength(value string) SVGGradientLength {
	value = strings.TrimSpace(value)
	if value == "" {
		return SVGGradientLength{}
	}
	if calc, ok := parseSVGCalc(value, 16); ok {
		return calc
	}
	r := SVGGradientLength{Raw: value, HasValue: true}
	if strings.HasSuffix(value, "%") {
		num := strings.TrimSpace(strings.TrimSuffix(value, "%"))
		if v, err := strconv.ParseFloat(num, 64); err == nil {
			r.Value = v / 100.0
			r.IsPercent = true
		}
		return r
	}
	// Strip non-percent unit suffixes for the numeric extraction;
	// the existing splitNumberAndUnit understands `px`, `em`, `ex`, etc.
	// — for gradient lengths we treat any non-percent unit as user
	// units after the unit's CSS-px conversion which Phase 1's
	// SVGLengthContext.Resolve already performs.
	num, _ := splitNumberAndUnit(value)
	if v, err := strconv.ParseFloat(num, 64); err == nil {
		r.Value = v
	}
	return r
}

// parseGradientLengthWithFontSize is like parseGradientLength but
// resolves font-relative units (em, ex) using the supplied fontSize (in
// CSS px). Mirrors Blink's SVGLengthContext::ConvertValueToUserUnits for
// the kEms / kExs cases (core/svg/svg_length_context.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Used by
// NewSVGResourceFilter to resolve filter region lengths (x/y/width/height)
// that may carry font-relative units against the filter element's own
// font-size — Blink resolves these in SVGFilterElement::SetFilterRes /
// SVGLengthContext::ResolveLength before building the filter region rect.
//
// ex (x-height) is approximated as 0.5 em per CSS Values 4 §7.4 (the
// UA default when no glyph metrics are available), matching what Blink
// does for SVG lengths when no font data is loaded.
// parseFontRelativeUnit checks whether value ends with a known
// font-relative unit suffix (em or ex) and, if so, returns the numeric
// prefix and the unit. It handles the case where splitNumberAndUnit
// misparses "1em" as number="1e"/unit="m" because it greedily treats
// `e`/`E` as the start of a scientific-notation exponent. Using
// strings.HasSuffix avoids that ambiguity.
func parseFontRelativeUnit(value string) (num, unit string, ok bool) {
	v := strings.ToLower(value)
	switch {
	case strings.HasSuffix(v, "em"):
		return strings.TrimSpace(value[:len(value)-2]), "em", true
	case strings.HasSuffix(v, "ex"):
		return strings.TrimSpace(value[:len(value)-2]), "ex", true
	}
	return "", "", false
}

func parseGradientLengthWithFontSize(value string, fontSize float64) SVGGradientLength {
	value = strings.TrimSpace(value)
	if value == "" {
		return SVGGradientLength{}
	}
	if calc, ok := parseSVGCalc(value, fontSize); ok {
		return calc
	}
	r := SVGGradientLength{Raw: value, HasValue: true}
	if strings.HasSuffix(value, "%") {
		num := strings.TrimSpace(strings.TrimSuffix(value, "%"))
		if v, err := strconv.ParseFloat(num, 64); err == nil {
			r.Value = v / 100.0
			r.IsPercent = true
		}
		return r
	}
	// Handle em/ex before splitNumberAndUnit to avoid the "1em" → "1e"+"m"
	// misparsing in splitNumberAndUnit (it treats 'e' as scientific-notation
	// start). Mirrors Blink's SVGLengthContext::ConvertValueToUserUnits for
	// kEms / kExs cases.
	if numStr, unit, fontRel := parseFontRelativeUnit(value); fontRel {
		if v, err := strconv.ParseFloat(numStr, 64); err == nil {
			switch unit {
			case "em":
				r.Value = v * fontSize
			case "ex":
				// ex ≈ 0.5em when no font metrics are available — CSS Values 4 §7.4.
				r.Value = v * fontSize * 0.5
			}
		}
		return r
	}
	num, _ := splitNumberAndUnit(value)
	if v, err := strconv.ParseFloat(num, 64); err == nil {
		r.Value = v
	}
	return r
}

// ResolveAgainst resolves the length against a reference rect using
// the specified unit mode + axis. For objectBoundingBox it returns a
// FRACTION value (typically [0, 1]) that the caller multiplies into
// the reference rect; for userSpaceOnUse it returns a value in user
// units.
//
// The `axis` selects which extent a percent value resolves against
// (width / height / normalized diagonal for percent in objectBBox is
// rare but well-defined: a percent gradient coord in objectBBox space
// resolves against the same extent the fraction would, per SVG 2 §11.6).
func (l SVGGradientLength) ResolveAgainst(mode SVGUnitType, axis SVGLengthMode, ctx SVGLengthContext, defaultValue float64) float64 {
	if !l.HasValue {
		return defaultValue
	}
	switch mode {
	case SVGUnitObjectBoundingBox:
		// In objectBoundingBox mode the fraction is the value (a
		// percent and a fraction are the same thing — SVG 2 §11.6.1).
		// A calc()'s absolute term (AbsPx) is, per the SVG2 coords
		// objectBoundingBox model (the unit square [0,1]² mapped onto
		// the bbox), also a fraction of the bbox dimension — so it adds
		// directly to the fraction here.
		return l.Value + l.AbsPx
	case SVGUnitUserSpaceOnUse:
		if l.IsPercent {
			// Percent resolves against the current viewport's extent
			// for the chosen axis; the calc absolute term stays in user
			// units and adds on top.
			basis := percentageBasisForAxis(ctx, axis)
			return l.Value*basis + l.AbsPx
		}
		return l.Value + l.AbsPx
	}
	return defaultValue
}

// percentageBasisForAxis exposes SVGLengthContext's percentage basis
// to callers in the same package without making the helper exported on
// the type itself (the SVGLengthContext methods are intentionally tight).
func percentageBasisForAxis(ctx SVGLengthContext, mode SVGLengthMode) float64 {
	return ctx.percentageBasis(mode)
}

// ParseGradientUnits parses a `gradientUnits` / `patternUnits` /
// `maskUnits` / `clipPathUnits` attribute value. Unknown / missing
// values map to the spec default supplied by the caller (typically
// SVGUnitObjectBoundingBox for gradients/masks, SVGUnitUserSpaceOnUse
// for patternContentUnits).
func ParseGradientUnits(value string, def SVGUnitType) SVGUnitType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "userspaceonuse":
		return SVGUnitUserSpaceOnUse
	case "objectboundingbox":
		return SVGUnitObjectBoundingBox
	}
	return def
}

// ParseSpreadMethod parses a `spreadMethod` attribute value. Unknown /
// missing values map to SVGSpreadPad (the SVG 1.1 §13.2.2 default).
func ParseSpreadMethod(value string) SVGSpreadMethod {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "reflect":
		return SVGSpreadReflect
	case "repeat":
		return SVGSpreadRepeat
	}
	return SVGSpreadPad
}

// NewSVGLinearGradient builds an SVGLinearGradient from an
// ElementAdapter. Parses all geometry/units/spread attributes and the
// `<stop>` children. The childCtx argument is the SVGLengthContext
// active where the gradient was declared — used for userSpaceOnUse
// length resolution at paint time IF the painter doesn't override it
// with a closer viewport. Stops are parsed via the supplied style
// resolver so `stop-color` / `stop-opacity` honour the CSS cascade.
func NewSVGLinearGradient(elt ElementAdapter, styleResolver StyleResolver) *SVGLinearGradient {
	g := &SVGLinearGradient{}
	if id, ok := elt.Attribute("id"); ok {
		g.ResourceID = strings.TrimSpace(id)
	}
	if v, ok := elt.Attribute("x1"); ok {
		g.X1 = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("y1"); ok {
		g.Y1 = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("x2"); ok {
		g.X2 = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("y2"); ok {
		g.Y2 = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("gradientUnits"); ok {
		g.GradientUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	} else if v, ok := elt.Attribute("gradientunits"); ok {
		g.GradientUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	}
	if v, ok := elt.Attribute("gradientTransform"); ok {
		g.GradientTransform = ParseSVGTransform(v)
	} else if v, ok := elt.Attribute("gradienttransform"); ok {
		g.GradientTransform = ParseSVGTransform(v)
	} else {
		g.GradientTransform = geometry.Identity()
	}
	if v, ok := elt.Attribute("spreadMethod"); ok {
		g.SpreadMethod = ParseSpreadMethod(v)
	} else if v, ok := elt.Attribute("spreadmethod"); ok {
		g.SpreadMethod = ParseSpreadMethod(v)
	}
	if styleResolver != nil {
		g.elementStyle = styleResolver(elt)
	}
	g.Stops = parseGradientStops(elt, styleResolver)
	return g
}

// NewSVGRadialGradient builds an SVGRadialGradient from an
// ElementAdapter. See NewSVGLinearGradient for the rationale on
// per-attribute parsing.
func NewSVGRadialGradient(elt ElementAdapter, styleResolver StyleResolver) *SVGRadialGradient {
	g := &SVGRadialGradient{}
	if id, ok := elt.Attribute("id"); ok {
		g.ResourceID = strings.TrimSpace(id)
	}
	if v, ok := elt.Attribute("cx"); ok {
		g.Cx = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("cy"); ok {
		g.Cy = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("r"); ok {
		g.R = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("fx"); ok {
		g.Fx = parseGradientLength(v)
		g.HasFx = true
	}
	if v, ok := elt.Attribute("fy"); ok {
		g.Fy = parseGradientLength(v)
		g.HasFy = true
	}
	if v, ok := elt.Attribute("fr"); ok {
		g.Fr = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("gradientUnits"); ok {
		g.GradientUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	} else if v, ok := elt.Attribute("gradientunits"); ok {
		g.GradientUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	}
	if v, ok := elt.Attribute("gradientTransform"); ok {
		g.GradientTransform = ParseSVGTransform(v)
	} else if v, ok := elt.Attribute("gradienttransform"); ok {
		g.GradientTransform = ParseSVGTransform(v)
	} else {
		g.GradientTransform = geometry.Identity()
	}
	if v, ok := elt.Attribute("spreadMethod"); ok {
		g.SpreadMethod = ParseSpreadMethod(v)
	} else if v, ok := elt.Attribute("spreadmethod"); ok {
		g.SpreadMethod = ParseSpreadMethod(v)
	}
	if styleResolver != nil {
		g.elementStyle = styleResolver(elt)
	}
	g.Stops = parseGradientStops(elt, styleResolver)
	return g
}

// NewSVGPattern builds an SVGPattern from an ElementAdapter, building
// the pattern's child subtree through BuildSVGTree against a length
// context derived from patternContentUnits + the tile size. The
// content tree is painted into a tile by the render-side pattern
// painter at fill/stroke time.
func NewSVGPattern(elt ElementAdapter, parentCtx SVGLengthContext, styleResolver StyleResolver) *SVGPattern {
	p := &SVGPattern{
		PatternContentUnits: SVGUnitUserSpaceOnUse,
		PatternTransform:    geometry.Identity(),
		PreserveAspectRatio: DefaultPreserveAspectRatio(),
	}
	if id, ok := elt.Attribute("id"); ok {
		p.ResourceID = strings.TrimSpace(id)
	}
	if v, ok := elt.Attribute("x"); ok {
		p.TileX = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("y"); ok {
		p.TileY = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("width"); ok {
		p.TileWidth = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("height"); ok {
		p.TileHeight = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("patternUnits"); ok {
		p.PatternUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	} else if v, ok := elt.Attribute("patternunits"); ok {
		p.PatternUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	}
	if v, ok := elt.Attribute("patternContentUnits"); ok {
		p.PatternContentUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	} else if v, ok := elt.Attribute("patterncontentunits"); ok {
		p.PatternContentUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	}
	if v, ok := elt.Attribute("patternTransform"); ok {
		p.PatternTransform = ParseSVGTransform(v)
	} else if v, ok := elt.Attribute("patterntransform"); ok {
		p.PatternTransform = ParseSVGTransform(v)
	}
	if vb, ok := elt.Attribute("viewBox"); ok {
		if x, y, w, h, parsed := ParseViewBox(vb); parsed {
			p.ViewBox = ViewBox{X: x, Y: y, Width: w, Height: h, Valid: true}
		}
	} else if vb, ok := elt.Attribute("viewbox"); ok {
		if x, y, w, h, parsed := ParseViewBox(vb); parsed {
			p.ViewBox = ViewBox{X: x, Y: y, Width: w, Height: h, Valid: true}
		}
	}
	if par, ok := elt.Attribute("preserveAspectRatio"); ok {
		p.PreserveAspectRatio = ParsePreserveAspectRatio(par)
	} else if par, ok := elt.Attribute("preserveaspectratio"); ok {
		p.PreserveAspectRatio = ParsePreserveAspectRatio(par)
	}
	if styleResolver != nil {
		p.elementStyle = styleResolver(elt)
	}
	// Build the pattern's child subtree. The length context for the
	// children is the parent context — the render-side painter will
	// switch to the tile's length context once the tile size is known
	// (so percent lengths inside the pattern's children resolve
	// against the tile when patternContentUnits=objectBoundingBox).
	for _, child := range elt.SVGChildren() {
		if c := BuildSVGTree(child, parentCtx, styleResolver); c != nil {
			p.Children = append(p.Children, c)
		}
	}
	return p
}

// parseGradientStops walks the gradient's `<stop>` children in DOM
// order, resolving each stop's offset/color/opacity. The returned
// list is clamped + monotonized per SVG 2 §13.4.2:
//   - Offsets are clamped to [0, 1].
//   - Each subsequent stop's offset is clamped to be ≥ the previous
//     stop's offset.
//
// `stop-color` defaults to black; `stop-opacity` defaults to 1.0.
// `stop-opacity` is premultiplied into the stop's Color.A so the
// render-side painter does not need to read style again.
func parseGradientStops(elt ElementAdapter, styleResolver StyleResolver) []SVGGradientStop {
	var stops []SVGGradientStop
	prevOffset := 0.0
	for _, child := range elt.SVGChildren() {
		if child.TagName() != "stop" {
			continue
		}
		offset := 0.0
		if v, ok := child.Attribute("offset"); ok {
			offset = parseStopOffset(v)
		}
		// Clamp to [0, 1] then monotonize.
		if offset < 0 {
			offset = 0
		}
		if offset > 1 {
			offset = 1
		}
		if len(stops) > 0 && offset < prevOffset {
			offset = prevOffset
		}
		prevOffset = offset

		// Resolve stop-color: prefer the element's `stop-color`
		// presentation attribute (already routed through the
		// cascade by Phase 2's presentation-attribute application),
		// then the `style` attribute, then the SVG default black.
		color := css.Color{R: 0, G: 0, B: 0, A: 1.0}
		opacity := 1.0
		if styleResolver != nil {
			if style := styleResolver(child); style != nil {
				if v, ok := style.Get("stop-color"); ok && strings.TrimSpace(v) != "" {
					trimmed := strings.TrimSpace(v)
					// `currentColor` resolves against the element's
					// own `color` value (SVG 2 §13.4.2).
					if strings.EqualFold(trimmed, "currentcolor") {
						color = style.GetColor()
					} else if c, ok := css.ParseColor(trimmed); ok {
						color = c
					}
				}
				if v, ok := style.Get("stop-opacity"); ok {
					if f, ok := parseFloatAttr(v); ok {
						opacity = clampFloat(f, 0, 1)
					}
				}
			}
		}
		// Fall back to direct attribute lookups when the resolver
		// didn't supply the value (test fixtures may bypass the
		// cascade for resource elements that are inside <defs>).
		if v, ok := child.Attribute("stop-color"); ok {
			trimmed := strings.TrimSpace(v)
			if strings.EqualFold(trimmed, "currentcolor") {
				// Without a style we can't resolve currentColor; fall back
				// to black per SVG 1.1 §13.2.2 of "color" defaulting.
			} else if c, ok := css.ParseColor(trimmed); ok {
				color = c
			}
		}
		if v, ok := child.Attribute("stop-opacity"); ok {
			if f, ok := parseFloatAttr(v); ok {
				opacity = clampFloat(f, 0, 1)
			}
		}
		color.A *= opacity
		stops = append(stops, SVGGradientStop{Offset: offset, Color: color})
	}
	return stops
}

// parseStopOffset parses a `<stop>` `offset` attribute value. Accepts
// `<number>` (interpreted as [0, 1]) and `<percentage>` (e.g. "50%").
func parseStopOffset(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		num := strings.TrimSpace(strings.TrimSuffix(s, "%"))
		if f, err := strconv.ParseFloat(num, 64); err == nil {
			return f / 100.0
		}
		return 0
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return 0
}

// parseFloatAttr parses a generic float attribute value.
func parseFloatAttr(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	return 0, false
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
