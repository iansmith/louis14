package css

import "strings"

// TransformBox is the `transform-box` property: the box transform-origin and
// transform percentages resolve against (CSS Transforms 1 §transform-box).
// Mirrors Blink's ETransformBox, generated from css_properties.json5:6596-6604
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b in its keyword order.
type TransformBox int

const (
	TransformBoxContentBox TransformBox = iota
	TransformBoxBorderBox
	TransformBoxFillBox
	TransformBoxStrokeBox
	TransformBoxViewBox
)

var transformBoxKeywords = [...]string{
	TransformBoxContentBox: "content-box",
	TransformBoxBorderBox:  "border-box",
	TransformBoxFillBox:    "fill-box",
	TransformBoxStrokeBox:  "stroke-box",
	TransformBoxViewBox:    "view-box",
}

// String returns b's CSS keyword, its computed-value serialization
// (TransformBox::CSSValueFromComputedStyleInternal).
func (b TransformBox) String() string { return transformBoxKeywords[b] }

// GetTransformBox returns the computed transform-box (initial: view-box),
// its keyword matched ASCII case-insensitively.
func (s *Style) GetTransformBox() TransformBox {
	if v, ok := s.Get("transform-box"); ok {
		v = asciiToLower(strings.TrimSpace(v))
		for b, keyword := range transformBoxKeywords {
			if v == keyword {
				return TransformBox(b)
			}
		}
	}
	return TransformBoxViewBox
}

// TransformBoxContext says which boxes an element has: a CSS layout box, or
// none (an SVG element). Mirrors ComputedStyle::TransformBoxContext
// (core/style/computed_style.h:2176-2179 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type TransformBoxContext int

const (
	TransformBoxContextLayoutBox TransformBoxContext = iota
	TransformBoxContextSvg
)

// UsedTransformBox maps the computed transform-box onto a box the element
// has: on an SVG element content-box is fill-box and border-box is
// stroke-box; on a CSS box fill-box is content-box and stroke-box and
// view-box are border-box. Mirrors ComputedStyle::UsedTransformBox
// (core/style/computed_style.cc:1377-1420 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), except that louis14 has no
// vector-effect, so stroke-box never becomes fill-box for a
// non-scaling-stroke element (LOU-666).
func (s *Style) UsedTransformBox(context TransformBoxContext) TransformBox {
	b := s.GetTransformBox()
	if context == TransformBoxContextSvg {
		switch b {
		case TransformBoxContentBox:
			return TransformBoxFillBox
		case TransformBoxBorderBox:
			return TransformBoxStrokeBox
		}
		return b
	}
	switch b {
	case TransformBoxFillBox:
		return TransformBoxContentBox
	case TransformBoxStrokeBox, TransformBoxViewBox:
		return TransformBoxBorderBox
	}
	return b
}
