package svg

import (
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// ParseSVGTransform parses the value of an SVG `transform` attribute as
// defined by SVG 1.1 §7.6 (preserved by SVG 2 §8.5 / §7.5). The
// grammar admits a whitespace- or comma-separated list of transform
// functions; the functions compose left-to-right (the leftmost is the
// outermost — i.e. for `transform="translate(10,0) rotate(45)"` a point
// p is mapped as translate(rotate(p)), matching how nested <g> elements
// would stack the same transforms).
//
// Supported function forms (every form taking optional values applies
// the spec-defined default for the omitted ones):
//
//	translate(tx[, ty])       — ty defaults to 0
//	scale(sx[, sy])           — sy defaults to sx
//	rotate(angle[, cx, cy])   — when (cx, cy) given:
//	                              translate(cx, cy) · rotate(angle) · translate(-cx, -cy)
//	skewX(angle)
//	skewY(angle)
//	matrix(a, b, c, d, e, f)
//
// Whitespace, commas, scientific notation, signs, and negative
// coordinates are all accepted (§7.6 grammar). Anything we can't parse
// — unknown function, wrong arity, malformed number — terminates the
// parse early and returns the transforms accumulated up to that point
// (mirrors Blink's SVGTransformList::Parse behaviour of producing a
// best-effort transform list rather than throwing on each error).
//
// Returns Identity() for empty / unparsable input.
//
// Mirrors core/svg/svg_transform_list.{h,cc} (the SVGTransformList
// parser) + core/svg/svg_transform.{h,cc} (per-function matrix build).
func ParseSVGTransform(value string) geometry.AffineTransform {
	value = strings.TrimSpace(value)
	if value == "" {
		return geometry.Identity()
	}
	p := &svgTransformParser{src: value, i: 0}
	result := geometry.Identity()
	first := true
	for {
		p.skipSeparatorsOrComma()
		if p.atEnd() {
			break
		}
		name, ok := p.readFunctionName()
		if !ok {
			break
		}
		p.skipWhitespace()
		if !p.consume('(') {
			break
		}
		args, ok := p.readArgs()
		if !ok {
			break
		}
		t, ok := buildSVGTransformFunction(name, args)
		if !ok {
			break
		}
		if first {
			result = t
			first = false
		} else {
			result = result.Compose(t)
		}
	}
	return result
}

// ParseCSSTransformForSVG converts the already-parsed CSS `transform`
// list on the supplied computed style into a single AffineTransform.
// Mirrors what Blink's TransformHelper::ComputeTransform does for the
// CSS half of an SVG element's transform: collect the parsed
// css::Transform list, fold each function into an AffineTransform,
// and compose them left-to-right (same convention as
// ParseSVGTransform). Percent translates are not supported here
// because SVG-context CSS transforms resolve % against the reference
// box (Blink: TransformHelper::ComputeReferenceBox) — Phase 3 doesn't
// plumb the reference box yet, so percentages map to pixels through
// the typical CSS box translate semantics (treat percent as the
// scalar value as-is, mirroring how render.go's applyTransforms
// does on a CSS box that hasn't been laid out — those cases don't
// exercise SVG percent translates).
//
// Returns Identity() for nil style or an empty / "none" transform list.
//
// Per SVG 2 §7.5, when both the SVG `transform` attribute and a CSS
// `transform` are present, the effective transform is `cssT ∘ svgT`
// (the SVG attribute is the inner transform; CSS applies on top of
// it). Callers should compose accordingly.
func ParseCSSTransformForSVG(style *css.Style) geometry.AffineTransform {
	if style == nil {
		return geometry.Identity()
	}
	list := style.GetTransforms()
	if len(list) == 0 {
		return geometry.Identity()
	}
	result := geometry.Identity()
	first := true
	for _, t := range list {
		af, ok := cssTransformToAffine(t)
		if !ok {
			continue
		}
		if first {
			result = af
			first = false
		} else {
			result = result.Compose(af)
		}
	}
	return result
}

// cssTransformToAffine converts a single css.Transform into an
// AffineTransform. Mirrors the Blink TransformOperation → matrix
// conversion path. Percent translates fall back to treating the
// percentage as the raw float (no reference-box plumbing in Phase 3
// — the SVG transform attribute is the dominant path for the Phase 3
// gates; CSS percent translate on an SVG element is uncommon and
// will be addressed when Phase 4+ wires reference-box-aware transform
// origins).
func cssTransformToAffine(t css.Transform) (geometry.AffineTransform, bool) {
	switch t.Type {
	case "translate":
		if len(t.Values) < 2 {
			return geometry.Identity(), false
		}
		return geometry.Translate(t.Values[0], t.Values[1]), true
	case "scale":
		if len(t.Values) < 2 {
			return geometry.Identity(), false
		}
		return geometry.Scale(t.Values[0], t.Values[1]), true
	case "rotate":
		if len(t.Values) < 1 {
			return geometry.Identity(), false
		}
		return geometry.Rotate(t.Values[0]), true
	case "skew":
		if len(t.Values) < 2 {
			return geometry.Identity(), false
		}
		// skew(ax, ay) = matrix(1, tan(ay), tan(ax), 1, 0, 0).
		// Use the skewX·skewY decomposition by composing the two.
		return geometry.SkewY(t.Values[1]).Compose(geometry.SkewX(t.Values[0])), true
	case "matrix":
		if len(t.Values) < 6 {
			return geometry.Identity(), false
		}
		return geometry.NewAffineTransform(
			t.Values[0], t.Values[1], t.Values[2],
			t.Values[3], t.Values[4], t.Values[5],
		), true
	}
	return geometry.Identity(), false
}

// buildSVGTransformFunction builds the AffineTransform for a single
// parsed SVG transform function (already split into a name and an
// args list of float64). Returns false on bad arity / unknown name.
func buildSVGTransformFunction(name string, args []float64) (geometry.AffineTransform, bool) {
	switch name {
	case "translate":
		switch len(args) {
		case 1:
			return geometry.Translate(args[0], 0), true
		case 2:
			return geometry.Translate(args[0], args[1]), true
		}
	case "scale":
		switch len(args) {
		case 1:
			return geometry.Scale(args[0], args[0]), true
		case 2:
			return geometry.Scale(args[0], args[1]), true
		}
	case "rotate":
		switch len(args) {
		case 1:
			return geometry.Rotate(args[0]), true
		case 3:
			// rotate(angle, cx, cy) = translate(cx, cy) · rotate(angle) · translate(-cx, -cy)
			t1 := geometry.Translate(args[1], args[2])
			t2 := geometry.Rotate(args[0])
			t3 := geometry.Translate(-args[1], -args[2])
			return t1.Compose(t2).Compose(t3), true
		}
	case "skewX":
		if len(args) == 1 {
			return geometry.SkewX(args[0]), true
		}
	case "skewY":
		if len(args) == 1 {
			return geometry.SkewY(args[0]), true
		}
	case "matrix":
		if len(args) == 6 {
			return geometry.NewAffineTransform(
				args[0], args[1], args[2],
				args[3], args[4], args[5],
			), true
		}
	}
	return geometry.Identity(), false
}

// svgTransformParser is a tiny tokenizer over the SVG transform
// attribute string. Mirrors the structure of Blink's parsing in
// SVGTransformList::Parse: cursor-based, lenient on whitespace and
// commas, strict on grammar (unknown function or wrong arity stops
// the parse).
type svgTransformParser struct {
	src string
	i   int
}

func (p *svgTransformParser) atEnd() bool { return p.i >= len(p.src) }

func (p *svgTransformParser) peek() byte {
	if p.atEnd() {
		return 0
	}
	return p.src[p.i]
}

func (p *svgTransformParser) consume(b byte) bool {
	if p.atEnd() || p.src[p.i] != b {
		return false
	}
	p.i++
	return true
}

func (p *svgTransformParser) skipWhitespace() {
	for !p.atEnd() {
		switch p.src[p.i] {
		case ' ', '\t', '\n', '\r', '\f':
			p.i++
		default:
			return
		}
	}
}

// skipSeparatorsOrComma skips whitespace plus an optional single comma
// (§7.6 wsp*,?wsp* separator between functions). Multiple commas in a
// row are not allowed and would be caught by readFunctionName's empty
// check.
func (p *svgTransformParser) skipSeparatorsOrComma() {
	p.skipWhitespace()
	if !p.atEnd() && p.src[p.i] == ',' {
		p.i++
		p.skipWhitespace()
	}
}

func (p *svgTransformParser) readFunctionName() (string, bool) {
	start := p.i
	for !p.atEnd() {
		c := p.src[p.i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			p.i++
			continue
		}
		break
	}
	if p.i == start {
		return "", false
	}
	return p.src[start:p.i], true
}

// readArgs reads a parenthesized arglist (the opening '(' has already
// been consumed): a sequence of numbers separated by whitespace and/or
// commas, terminated by ')'. Returns false on unterminated input or a
// non-number where a number was expected.
func (p *svgTransformParser) readArgs() ([]float64, bool) {
	var args []float64
	for {
		p.skipSeparatorsOrComma()
		if p.atEnd() {
			return nil, false
		}
		if p.peek() == ')' {
			p.i++ // consume ')'
			return args, true
		}
		v, ok := p.readNumber()
		if !ok {
			return nil, false
		}
		args = append(args, v)
	}
}

// readNumber consumes an SVG number per §4.2: optional sign, digits,
// optional fractional part, optional exponent. Returns false if no
// number was found.
func (p *svgTransformParser) readNumber() (float64, bool) {
	start := p.i
	// Optional sign.
	if !p.atEnd() && (p.src[p.i] == '+' || p.src[p.i] == '-') {
		p.i++
	}
	// Integer part.
	digitsBefore := 0
	for !p.atEnd() && isDigit(p.src[p.i]) {
		p.i++
		digitsBefore++
	}
	// Fractional part.
	digitsAfter := 0
	if !p.atEnd() && p.src[p.i] == '.' {
		p.i++
		for !p.atEnd() && isDigit(p.src[p.i]) {
			p.i++
			digitsAfter++
		}
	}
	if digitsBefore == 0 && digitsAfter == 0 {
		p.i = start
		return 0, false
	}
	// Exponent.
	if !p.atEnd() && (p.src[p.i] == 'e' || p.src[p.i] == 'E') {
		save := p.i
		p.i++
		if !p.atEnd() && (p.src[p.i] == '+' || p.src[p.i] == '-') {
			p.i++
		}
		expDigits := 0
		for !p.atEnd() && isDigit(p.src[p.i]) {
			p.i++
			expDigits++
		}
		if expDigits == 0 {
			// Bare 'e' is not part of this number — roll back.
			p.i = save
		}
	}
	v, err := strconv.ParseFloat(p.src[start:p.i], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
