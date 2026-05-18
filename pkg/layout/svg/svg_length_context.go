package svg

import (
	"math"
	"strconv"
	"strings"

	"louis14/pkg/geometry"
)

// SVGLengthMode selects which viewport axis a percentage <length> resolves
// against. Mirrors Blink's SVGLengthMode (core/svg/svg_length.h):
//
//   - LengthModeWidth — % resolves against viewport.Width()
//   - LengthModeHeight — % resolves against viewport.Height()
//   - LengthModeOther — % resolves against the normalized diagonal
//     sqrt((w² + h²) / 2). Used by length attributes that have no axis
//     (e.g. <length>s in path commands).
type SVGLengthMode int

const (
	LengthModeWidth SVGLengthMode = iota
	LengthModeHeight
	LengthModeOther
)

// SVGLengthContext resolves SVG `<length>` values against the current
// viewport. Mirrors Blink's SVGLengthContext
// (core/svg/svg_length_context.{h,cc}).
//
// Phase 1 scope: bare numbers, `px`, and `%` units against a static
// viewport. Other CSS-derived absolute units (`cm`, `mm`, `Q`, `in`, `pt`,
// `pc`) and font-relative (`em`, `ex`, `ch`, `rem`) are not yet plumbed;
// Phase 2/3 grows the parser when shape geometry attributes start
// consuming them. The existing CSS length parser in pkg/css is not reused
// here because SVG attribute lengths are evaluated against the SVG
// viewport, not the CSS containing block.
type SVGLengthContext struct {
	viewport geometry.SizeF
}

// NewSVGLengthContext builds a length context for a given viewport size
// (in CSS px).
func NewSVGLengthContext(viewport geometry.SizeF) SVGLengthContext {
	return SVGLengthContext{viewport: viewport}
}

// ViewportSize returns the viewport size in CSS px.
func (c SVGLengthContext) ViewportSize() geometry.SizeF {
	return c.viewport
}

// Resolve parses an SVG <length> attribute value and returns its value in
// user units (CSS px in the viewport coordinate system). For an
// unparseable input it returns 0. Mirrors the value-resolution side of
// Blink's SVGLengthContext::ConvertValueToUserUnits.
func (c SVGLengthContext) Resolve(value string, mode SVGLengthMode) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	// Split number portion from optional unit suffix.
	num, unit := splitNumberAndUnit(value)
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}

	switch unit {
	case "", "px":
		return v
	case "%":
		return v / 100.0 * c.percentageBasis(mode)
	default:
		// Phase 1: unsupported units fall back to the raw number (treated
		// as user units). Phase 2/3 will plumb em/ex/cm/mm/in/pt/pc/rem
		// once shape geometry attributes need them.
		return v
	}
}

// percentageBasis returns the viewport scalar that `%` resolves against
// for the given mode. Per SVG 2 §4.5.10:
//   - Width  → viewport.Width
//   - Height → viewport.Height
//   - Other  → sqrt((w² + h²) / 2) — the "normalized diagonal".
func (c SVGLengthContext) percentageBasis(mode SVGLengthMode) float64 {
	w, h := c.viewport.Width, c.viewport.Height
	switch mode {
	case LengthModeWidth:
		return w
	case LengthModeHeight:
		return h
	default:
		return math.Sqrt((w*w + h*h) / 2.0)
	}
}

// splitNumberAndUnit divides a length token into a numeric prefix and an
// optional unit suffix. The numeric prefix includes a sign, digits, a
// decimal point, and an exponent. Any trailing alphabetic / `%` text is
// the unit.
func splitNumberAndUnit(s string) (num, unit string) {
	// Find the boundary between the numeric prefix and the unit.
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
			i++
		}
	}
	return s[:i], strings.TrimSpace(s[i:])
}
