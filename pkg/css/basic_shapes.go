package css

import (
	"math"
	"strings"

	"louis14/pkg/geometry"
)

// BasicShapeInset is an inset() basic shape: offsets from the reference
// box's edges. Mirrors Blink's BasicShapeInset (core/style/basic_shapes.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The `round` corner radii are
// parsed but not kept: the one consumer, object-view-box, reads only the
// path's bounding rect, which radii do not move.
type BasicShapeInset struct {
	Top, Right, Bottom, Left Length
}

// PathBoundingRect returns the bounding rect of the inset's path in
// boundingBox: BasicShapeInset::GetPath(bounding_box, zoom, 1).BoundingRect()
// (core/style/basic_shapes.cc:349-380 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose width and height clamp
// at zero.
func (b *BasicShapeInset) PathBoundingRect(boundingBox geometry.RectF) geometry.RectF {
	w, h := boundingBox.Width(), boundingBox.Height()
	left := b.Left.ValueForLength(w)
	top := b.Top.ValueForLength(h)
	return geometry.NewRectF(left+boundingBox.X(), top+boundingBox.Y(),
		math.Max(w-left-b.Right.ValueForLength(w), 0),
		math.Max(h-top-b.Bottom.ValueForLength(h), 0))
}

// consumeInsetLikeBasicShape consumes inset(), rect() or xywh() — the
// basic shapes object-view-box accepts (ObjectViewBox::ParseSingleValue,
// longhands_custom.cc:7339-7357 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b,
// via ConsumeBasicShape) — and computes it to the equivalent inset, as
// BasicShapeForValue does (core/css/basic_shape_functions.cc:698-760):
// rect(t r b l) → inset(t calc(100% - r) calc(100% - b) l) with auto as 0%,
// and xywh(x y w h) → inset(y calc(100% - x - w) calc(100% - y - h) x).
// Returns nil when value is not one of the three or is invalid.
func consumeInsetLikeBasicShape(value string, ctx calcContext) *BasicShapeInset {
	value = strings.TrimSpace(value)
	open := strings.IndexByte(value, '(')
	if open < 0 || !strings.HasSuffix(value, ")") {
		return nil
	}
	name := strings.ToLower(value[:open])
	args := splitComponentValues(value[open+1 : len(value)-1])
	if i := indexOfIdent(args, "round"); i >= 0 {
		if i == len(args)-1 {
			return nil // `round` needs a <'border-radius'>
		}
		args = args[:i]
	}
	// lengths converts every argument; auto (rect() only) is reported in
	// isAuto and left as the zero Length.
	lengths := func(nonNegativeFrom int, allowAuto bool) (out []Length, isAuto []bool, ok bool) {
		out, isAuto = make([]Length, len(args)), make([]bool, len(args))
		for i, tok := range args {
			if allowAuto && strings.EqualFold(tok, "auto") {
				isAuto[i] = true
				continue
			}
			if !isLengthPercentToken(tok, i >= nonNegativeFrom, false) {
				return nil, nil, false
			}
			if out[i], ok = convertLength(tok, ctx); !ok {
				return nil, nil, false
			}
		}
		return out, isAuto, true
	}
	switch name {
	case "inset":
		if len(args) < 1 || len(args) > 4 {
			return nil
		}
		ls, _, ok := lengths(len(args), false)
		if !ok {
			return nil
		}
		// Margin-style expansion of 1–4 values (top, right, bottom, left).
		e := expandFourValues(ls)
		return &BasicShapeInset{Top: e[0], Right: e[1], Bottom: e[2], Left: e[3]}
	case "rect":
		if len(args) != 4 {
			return nil
		}
		ls, isAuto, ok := lengths(len(args), true)
		if !ok {
			return nil
		}
		// An auto edge coincides with the reference box edge: inset 0.
		subtract := func(i int) Length {
			if isAuto[i] {
				return Length{}
			}
			return ls[i].SubtractFromOneHundredPercent()
		}
		return &BasicShapeInset{Top: ls[0], Right: subtract(1), Bottom: subtract(2), Left: ls[3]}
	case "xywh":
		if len(args) != 4 {
			return nil
		}
		ls, _, ok := lengths(2, false)
		if !ok {
			return nil
		}
		x, y, w, h := ls[0], ls[1], ls[2], ls[3]
		return &BasicShapeInset{
			Left:   x,
			Right:  x.Add(w).SubtractFromOneHundredPercent(),
			Top:    y,
			Bottom: y.Add(h).SubtractFromOneHundredPercent(),
		}
	}
	return nil
}

// indexOfIdent returns the index of the first token equal (ASCII
// case-insensitively) to ident, or -1.
func indexOfIdent(toks []string, ident string) int {
	for i, t := range toks {
		if strings.EqualFold(t, ident) {
			return i
		}
	}
	return -1
}

// isValidObjectViewBoxValue validates an object-view-box declaration:
// none | inset() | rect() | xywh() (ObjectViewBox::ParseSingleValue,
// longhands_custom.cc:7339-7357 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// CSS-wide keywords and var()/attr() are accepted as-is.
func isValidObjectViewBoxValue(value string) bool {
	if isDeferredOrCSSWideValue(value) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(value), "none") {
		return true
	}
	return consumeInsetLikeBasicShape(value, calcContext{}) != nil
}

// ObjectViewBox returns the computed object-view-box, or nil for none.
// Mirrors ComputedStyle::ObjectViewBox with
// StyleBuilderConverter::ConvertObjectViewBox (style_builder_converter.cc:
// 3489-3496 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (s *Style) ObjectViewBox() *BasicShapeInset {
	v, ok := s.Get("object-view-box")
	if !ok {
		return nil
	}
	return consumeInsetLikeBasicShape(v, s.cssToLengthConversionData())
}
