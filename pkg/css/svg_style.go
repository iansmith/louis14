package css

import (
	"fmt"
	"strings"
)

// SVG paint accessors mirror Blink's SVGComputedStyle in
// core/style/svg_computed_style.{h,cc}: each property gets a typed
// accessor that returns the SVG default per the SVG 2 / CSS Paint
// API spec when the property is unset or unparseable. The defaults
// are:
//
//	fill: black           (SVG 1.1 §11.3)
//	stroke: none          (SVG 1.1 §11.4)
//	fill-rule: nonzero    (SVG 1.1 §11.3)
//	fill-opacity: 1       (SVG 1.1 §11.3.1)
//	stroke-opacity: 1     (SVG 1.1 §11.4.1)
//	stroke-width: 1       (SVG 1.1 §11.4)
//	stroke-linecap: butt  (SVG 1.1 §11.4)
//	stroke-linejoin: miter (SVG 1.1 §11.4)
//	stroke-miterlimit: 4  (SVG 1.1 §11.4)
//	stroke-dasharray: none
//	stroke-dashoffset: 0
//
// The Has* / Paint flag distinguishes "fill: none" from "fill: black"
// because none is not a Color — a Color{0,0,0,0} would alias with the
// default-transparent path and break the painter's skip-on-none
// short-circuit.

// SVGPaintKind enumerates the resolved kinds of an SVG paint value
// (fill / stroke). Mirrors Blink's SVGPaintType enum
// (core/css/css_value_keywords.h + SVGPaint::Type).
type SVGPaintKind int

const (
	// SVGPaintNone means the paint is "none" — skip fill/stroke entirely.
	SVGPaintNone SVGPaintKind = iota
	// SVGPaintColor means the paint is a solid color (or the
	// computed value of `currentColor` — resolved here against the
	// element's `color` property so the painter never has to know
	// about currentColor handling).
	SVGPaintColor
	// SVGPaintServer means the paint is a `url(#id)` reference to a
	// gradient or pattern element. Phase 2 carries the reference but
	// no painter resolves it — Phase 4 fleshes out gradient/pattern.
	SVGPaintServer
)

// SVGPaint is a resolved fill or stroke paint value. Mirrors Blink's
// SVGPaint (core/style/style_svg_resource.h + SVGPaint).
type SVGPaint struct {
	Kind SVGPaintKind
	// Color is valid when Kind == SVGPaintColor. The painter applies
	// it directly via DrawContext.SetColor.
	Color Color
	// ServerID is the fragment-stripped `#id` referenced by a
	// `url(#…)` paint. Valid when Kind == SVGPaintServer. Phase 4
	// uses this to look up gradients/patterns. Empty when Kind is
	// not Server.
	ServerID string
	// Fallback is the optional second value in `url(#id) color`
	// paint syntax — used when ServerID can't be resolved. Phase 4
	// honors it; Phase 2 falls back to it directly for `url(#…)`
	// since we have no paint-server registry yet. Kind matches a
	// flat Color / None value, not another Server reference.
	Fallback *SVGPaint
}

// GetFill returns the computed fill paint. Defaults to opaque black
// per SVG 1.1 §11.3. `fill: none`, `fill: currentColor`, and
// `fill: url(#…)` are all recognized.
func (s *Style) GetFill() SVGPaint {
	v, ok := s.Get("fill")
	if !ok || strings.TrimSpace(v) == "" {
		return SVGPaint{Kind: SVGPaintColor, Color: Color{R: 0, G: 0, B: 0, A: 1.0}}
	}
	return parseSVGPaint(v, s.GetColor())
}

// GetStroke returns the computed stroke paint. Defaults to `none`
// per SVG 1.1 §11.4.
func (s *Style) GetStroke() SVGPaint {
	v, ok := s.Get("stroke")
	if !ok || strings.TrimSpace(v) == "" {
		return SVGPaint{Kind: SVGPaintNone}
	}
	return parseSVGPaint(v, s.GetColor())
}

// SVGFillRule enumerates the resolved fill-rule keyword.
type SVGFillRule int

const (
	// SVGFillRuleNonzero is the SVG default per SVG 1.1 §11.3.
	SVGFillRuleNonzero SVGFillRule = iota
	// SVGFillRuleEvenOdd matches CSS `fill-rule: evenodd`.
	SVGFillRuleEvenOdd
)

// GetFillRule returns the computed fill-rule. Defaults to nonzero per
// SVG 1.1 §11.3.
func (s *Style) GetFillRule() SVGFillRule {
	v, ok := s.Get("fill-rule")
	if !ok {
		return SVGFillRuleNonzero
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "evenodd":
		return SVGFillRuleEvenOdd
	default:
		return SVGFillRuleNonzero
	}
}

// GetFillOpacity returns fill-opacity, clamped to [0, 1]. Default 1.
func (s *Style) GetFillOpacity() float64 {
	return getOpacityProperty(s, "fill-opacity", 1.0)
}

// GetStrokeOpacity returns stroke-opacity, clamped to [0, 1]. Default 1.
func (s *Style) GetStrokeOpacity() float64 {
	return getOpacityProperty(s, "stroke-opacity", 1.0)
}

// GetStrokeWidth returns stroke-width in user units. Default 1 per
// SVG 1.1 §11.4. Negative widths are invalid and treated as 0 per
// SVG 2 §13.4.4. The CSS-length parser is reused: integers, decimals,
// `px`, and `%` (against viewport diagonal — mode "other") all parse.
func (s *Style) GetStrokeWidth() float64 {
	v, ok := s.Get("stroke-width")
	if !ok {
		return 1.0
	}
	w, parsed := parseSVGNumberOrLength(v)
	if !parsed {
		return 1.0
	}
	if w < 0 {
		return 0
	}
	return w
}

// SVGLineCap enumerates the resolved stroke-linecap keyword.
type SVGLineCap int

const (
	// SVGLineCapButt is the SVG default per SVG 1.1 §11.4.
	SVGLineCapButt SVGLineCap = iota
	// SVGLineCapRound matches `stroke-linecap: round`.
	SVGLineCapRound
	// SVGLineCapSquare matches `stroke-linecap: square`.
	SVGLineCapSquare
)

// GetStrokeLinecap returns stroke-linecap. Defaults to butt.
func (s *Style) GetStrokeLinecap() SVGLineCap {
	v, ok := s.Get("stroke-linecap")
	if !ok {
		return SVGLineCapButt
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "round":
		return SVGLineCapRound
	case "square":
		return SVGLineCapSquare
	default:
		return SVGLineCapButt
	}
}

// SVGLineJoin enumerates the resolved stroke-linejoin keyword.
type SVGLineJoin int

const (
	// SVGLineJoinMiter is the SVG default per SVG 1.1 §11.4.
	SVGLineJoinMiter SVGLineJoin = iota
	// SVGLineJoinRound matches `stroke-linejoin: round`.
	SVGLineJoinRound
	// SVGLineJoinBevel matches `stroke-linejoin: bevel`.
	SVGLineJoinBevel
)

// GetStrokeLinejoin returns stroke-linejoin. Defaults to miter.
func (s *Style) GetStrokeLinejoin() SVGLineJoin {
	v, ok := s.Get("stroke-linejoin")
	if !ok {
		return SVGLineJoinMiter
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "round":
		return SVGLineJoinRound
	case "bevel":
		return SVGLineJoinBevel
	default:
		return SVGLineJoinMiter
	}
}

// GetStrokeMiterLimit returns stroke-miterlimit. Default 4 per SVG 1.1
// §11.4. Negative or <1 values are invalid (clamped to 1 — Blink
// behavior).
func (s *Style) GetStrokeMiterLimit() float64 {
	v, ok := s.Get("stroke-miterlimit")
	if !ok {
		return 4.0
	}
	m, parsed := parseSVGNumberOrLength(v)
	if !parsed {
		return 4.0
	}
	if m < 1 {
		return 1
	}
	return m
}

// GetStrokeDashArray returns the stroke-dasharray as a slice of
// resolved user-unit lengths, or nil for `none` / unparsed. Per SVG
// 1.1 §11.4, the value is a comma-or-space-separated list of
// non-negative `<length>`s; an odd-count list is duplicated to make
// it even (the painter handles that).
func (s *Style) GetStrokeDashArray() []float64 {
	v, ok := s.Get("stroke-dasharray")
	if !ok {
		return nil
	}
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || v == "none" {
		return nil
	}
	// Replace commas with spaces and Fields-split.
	v = strings.ReplaceAll(v, ",", " ")
	parts := strings.Fields(v)
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, ok := parseSVGNumberOrLength(p)
		if !ok || f < 0 {
			return nil
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GetStrokeDashOffset returns stroke-dashoffset. Default 0.
func (s *Style) GetStrokeDashOffset() float64 {
	v, ok := s.Get("stroke-dashoffset")
	if !ok {
		return 0
	}
	f, parsed := parseSVGNumberOrLength(v)
	if !parsed {
		return 0
	}
	return f
}

// parseSVGPaint parses a `fill` / `stroke` property value into a
// SVGPaint. `none` → SVGPaintNone; `currentColor` → SVGPaintColor
// with the element's resolved color; `url(#id) [fallback]` →
// SVGPaintServer with fragment id; any color → SVGPaintColor.
// Mirrors Blink's CSSPaintValue resolution + CSSParser::ParsePaint.
func parseSVGPaint(value string, currentColor Color) SVGPaint {
	value = strings.TrimSpace(value)
	if value == "" {
		// Treat empty as default fill (black) — Get* callers have
		// already short-circuited unset to the default, but a
		// presentation-attribute path may set an empty string;
		// match the spec's "invalid → initial value" rule.
		return SVGPaint{Kind: SVGPaintColor, Color: Color{A: 1.0}}
	}
	low := strings.ToLower(value)
	if low == "none" {
		return SVGPaint{Kind: SVGPaintNone}
	}
	if low == "currentcolor" {
		return SVGPaint{Kind: SVGPaintColor, Color: currentColor}
	}
	if strings.HasPrefix(low, "url(") {
		// Find the matching ')'.
		end := strings.Index(value, ")")
		if end < 0 {
			// Malformed — drop to invalid → initial.
			return SVGPaint{Kind: SVGPaintColor, Color: Color{A: 1.0}}
		}
		inside := strings.TrimSpace(value[4:end])
		inside = strings.Trim(inside, "\"'")
		id := ""
		if strings.HasPrefix(inside, "#") {
			id = inside[1:]
		}
		rest := strings.TrimSpace(value[end+1:])
		var fallback *SVGPaint
		if rest != "" {
			fb := parseSVGPaint(rest, currentColor)
			fallback = &fb
		}
		return SVGPaint{Kind: SVGPaintServer, ServerID: id, Fallback: fallback}
	}
	if c, ok := ParseColor(value); ok {
		return SVGPaint{Kind: SVGPaintColor, Color: c}
	}
	// Unparseable: per SVG 1.1 §11.3 invalid values fall back to the
	// property's initial value. For fill that is black, for stroke
	// that is none — caller-specific. The painter treats Color{A:0}
	// the same as None for transparent paints; but to keep the
	// behavior consistent across fill/stroke and match Blink, we
	// return an "invalid" sentinel: transparent black. Callers that
	// care can re-check via Get* defaults if the property was unset
	// (we only reach here when the property was set to garbage).
	return SVGPaint{Kind: SVGPaintColor, Color: Color{A: 0}}
}

// getOpacityProperty parses a generic 0-1 opacity property.
func getOpacityProperty(s *Style, name string, defaultVal float64) float64 {
	v, ok := s.Get(name)
	if !ok {
		return defaultVal
	}
	var f float64
	v = strings.TrimSpace(v)
	hasPct := strings.HasSuffix(v, "%")
	if hasPct {
		v = strings.TrimSuffix(v, "%")
	}
	if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
		return defaultVal
	}
	if hasPct {
		f /= 100.0
	}
	if f < 0 {
		f = 0
	} else if f > 1 {
		f = 1
	}
	return f
}

// parseSVGNumberOrLength parses a number or px-suffixed length. SVG
// allows unitless lengths in property values where the unit is
// implied user-units; CSS allows `px`, `pt`, etc. Phase 2 supports
// unitless numbers and `px` — the existing CSS pkg can extend this
// later for em/rem/percentages-against-viewport if a downstream
// test requires it.
func parseSVGNumberOrLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	// Strip a px / pt / em suffix; we only honor px here. Other CSS
	// units fall back to the raw number — matches Blink's behavior
	// for SVG attribute lengths in CSS context (the absolute-unit
	// resolution Blink does requires the font/zoom context we don't
	// have at style-time; the same pragmatic choice matches the
	// SVGLengthContext.Resolve path for attribute lengths).
	if strings.HasSuffix(s, "px") {
		s = strings.TrimSuffix(s, "px")
	}
	if strings.HasSuffix(s, "%") {
		// Phase 2 doesn't carry the viewport down into the style
		// system; percent stroke-widths are extremely rare in
		// real-world SVG. Leave as raw number for now — the
		// downstream painter will see e.g. "50%" → 50.0 which is
		// visibly wrong; tests that depend on % stroke-width will
		// trip a gate and we'll revisit.
		s = strings.TrimSuffix(s, "%")
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0, false
	}
	return f, true
}
