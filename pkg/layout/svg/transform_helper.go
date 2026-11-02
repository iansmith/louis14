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

// ParseCSSTransformForSVG returns the CSS transform (the translate, rotate
// and scale properties and the `transform` list) of an SVG element's
// computed style as an AffineTransform, applied
// through ComputedStyle::ApplyTransform and flattened with
// AffineTransform::FromTransform, as Blink's SVGElement::CalculateTransform
// does (core/svg/svg_element.cc; computed_style.cc:1434-1496 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Percentages and
// transform-origin resolve against referenceBox, which callers compute as
// TransformHelper::ComputeReferenceBox does for the default transform-box,
// view-box: the size of the nearest SVG viewport (transform_helper.cc:93-117;
// louis14 has no fill-box / stroke-box). The UA stylesheet gives SVG
// elements transform-origin: 0 0, the reference box's origin.
func ParseCSSTransformForSVG(style *css.Style, referenceBox geometry.RectF) geometry.AffineTransform {
	if style == nil || !style.HasTransform() {
		return geometry.Identity()
	}
	return aroundSVGTransformOrigin(style, referenceBox, svgCSSTransform(style, css.IncludeTransformOperations, referenceBox))
}

// ViewBoxReferenceBox returns TransformHelper::ComputeReferenceBox for
// transform-box: view-box — the nearest SVG viewport's size at the origin
// (transform_helper.cc:104-108).
func ViewBoxReferenceBox(viewport geometry.SizeF) geometry.RectF {
	return geometry.RectF{Size: viewport}
}

// svgCSSTransform evaluates style's CSS transform for an SVG element —
// ComputedStyle::ApplyTransform flattened by AffineTransform::FromTransform —
// with or without the `transform` list, its percentages against
// referenceBox and without transform-origin, which
// aroundSVGTransformOrigin applies around the whole transform (the one
// definition both ParseCSSTransformForSVG and ElementTransform use).
func svgCSSTransform(style *css.Style, operations css.ApplyTransformOperations, referenceBox geometry.RectF) geometry.AffineTransform {
	var m geometry.Transform
	style.ApplyTransform(&m, referenceBox, operations, css.ExcludeTransformOrigin, css.IncludeIndependentTransformProperties)
	return geometry.AffineTransformFromTransform(m)
}

// aroundSVGTransformOrigin returns T(origin)·t·T(-origin) for the author's
// transform-origin resolved against referenceBox
// (TransformHelper::ComputeTransformOrigin, transform_helper.cc:173-184);
// with none set, the SVG UA default 0 0 is the reference box origin, and t
// is returned as is.
func aroundSVGTransformOrigin(style *css.Style, referenceBox geometry.RectF, t geometry.AffineTransform) geometry.AffineTransform {
	if _, authored := style.Properties["transform-origin"]; !authored {
		return t
	}
	o := style.GetTransformOrigin()
	ox := o.X.ValueForLength(referenceBox.Width()) + referenceBox.X()
	oy := o.Y.ValueForLength(referenceBox.Height()) + referenceBox.Y()
	return geometry.Translate(ox, oy).Compose(t).Compose(geometry.Translate(-ox, -oy))
}

// ElementTransform returns the transform an SVG node paints its content
// with, the louis14 counterpart of Blink's LocalSVGTransform. The
// `transform` attribute is a presentation attribute for the CSS
// `transform` property (SVGElement::CollectStyleForPresentationAttribute
// maps it to CSSPropertyID::kTransform, core/svg/svg_element.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a specified CSS `transform`
// replaces it; otherwise the attribute stands in for the transform list,
// after the translate, rotate and scale properties
// (ComputedStyle::ApplyTransform's order). transform-origin wraps the
// whole, attribute included. louis14 keeps the attribute on the node
// rather than in the cascade (LOU-648).
func ElementTransform(n SVGNode, style *css.Style, referenceBox geometry.RectF) geometry.AffineTransform {
	if style == nil {
		return n.LocalTransform()
	}
	_, specified := style.Properties["transform"]
	t := svgCSSTransform(style, css.ApplyTransformOperations(specified), referenceBox)
	if !specified {
		t = t.Compose(n.LocalTransform())
	}
	return aroundSVGTransformOrigin(style, referenceBox, t)
}
