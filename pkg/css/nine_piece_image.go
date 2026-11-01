package css

import (
	"strconv"
	"strings"
)

// ENinePieceImageRule is the border-image-repeat keyword for one axis.
// Mirrors ENinePieceImageRule (core/style/nine_piece_image.h:39-44 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type ENinePieceImageRule int

const (
	StretchImageRule ENinePieceImageRule = iota
	RoundImageRule
	SpaceImageRule
	RepeatImageRule
)

// Length is the computed form of a <length-percentage> (or a border-image-slice
// <number>, stored as a fixed length): Fixed px plus Percent of the reference
// extent. A calc() mixing both folds into the two terms. Mirrors the subset of
// Blink's Length (platform/geometry/length.h) that nine-piece images use:
// Length::Fixed, Length::Percent and a linear calc.
type Length struct {
	Fixed   float64
	Percent float64
}

// IsPercent reports whether the length has a percentage component
// (Length::IsPercent / HasPercent).
func (l Length) IsPercent() bool { return l.Percent != 0 }

// ValueForLength resolves the length against extent. Mirrors
// ValueForLength / FloatValueForLength (platform/geometry/length_functions.cc).
func (l Length) ValueForLength(extent float64) float64 {
	return l.Fixed + l.Percent*extent/100
}

// LengthBox holds one Length per physical side. Mirrors Blink's LengthBox
// (platform/geometry/length_box.h).
type LengthBox struct {
	Top, Right, Bottom, Left Length
}

// BorderImageLength is a border-image-width / -outset side: a <number>
// (multiplier of the border width), a Length, or auto. Mirrors Blink's
// BorderImageLength (core/style/border_image_length.h).
type BorderImageLength struct {
	isNumber bool
	isAuto   bool
	number   float64
	length   Length
}

// BorderImageLengthNumber returns a <number> BorderImageLength.
func BorderImageLengthNumber(n float64) BorderImageLength {
	return BorderImageLength{isNumber: true, number: n}
}

// BorderImageLengthAuto returns the auto BorderImageLength.
func BorderImageLengthAuto() BorderImageLength { return BorderImageLength{isAuto: true} }

// BorderImageLengthOf returns a <length-percentage> BorderImageLength.
func BorderImageLengthOf(l Length) BorderImageLength { return BorderImageLength{length: l} }

func (b BorderImageLength) IsNumber() bool  { return b.isNumber }
func (b BorderImageLength) IsAuto() bool    { return b.isAuto }
func (b BorderImageLength) Number() float64 { return b.number }
func (b BorderImageLength) Length() Length  { return b.length }

// BorderImageLengthBox holds one BorderImageLength per physical side. Mirrors
// Blink's BorderImageLengthBox (core/style/border_image_length_box.h).
type BorderImageLengthBox struct {
	Top, Right, Bottom, Left BorderImageLength
}

// NinePieceImage is the computed border-image (minus its source). Mirrors
// Blink's NinePieceImage (core/style/nine_piece_image.h:67-160 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the source image stays on the
// paint layer (BorderImageSource / BorderImageGenerated) because louis14
// resolves images at paint time.
type NinePieceImage struct {
	ImageSlices    LengthBox
	Fill           bool
	BorderSlices   BorderImageLengthBox
	Outset         BorderImageLengthBox
	HorizontalRule ENinePieceImageRule
	VerticalRule   ENinePieceImageRule
}

// ComputeOutset resolves one outset side against its border width. Mirrors
// NinePieceImage::ComputeOutset (core/style/nine_piece_image.h:154-161 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeOutset(outsetSide BorderImageLength, borderSide float64) float64 {
	if outsetSide.IsNumber() {
		return outsetSide.Number() * borderSide
	}
	return outsetSide.Length().Fixed
}

// ImageOutsets returns the [top, right, bottom, left] outsets for borderWidths.
// Mirrors ComputedStyle::ImageOutsets (core/style/computed_style.cc:2665-2673 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (n NinePieceImage) ImageOutsets(borderWidths [4]float64) [4]float64 {
	return [4]float64{
		ComputeOutset(n.Outset.Top, borderWidths[0]),
		ComputeOutset(n.Outset.Right, borderWidths[1]),
		ComputeOutset(n.Outset.Bottom, borderWidths[2]),
		ComputeOutset(n.Outset.Left, borderWidths[3]),
	}
}

// GetBorderImage maps the border-image-slice / -width / -outset / -repeat
// longhands into a NinePieceImage. Each longhand follows its Blink consumer
// (css_parsing_utils.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b:
// ConsumeBorderImageSlice :5648, ConsumeBorderImageWidth :5688,
// ConsumeBorderImageOutset :5721, ConsumeBorderImageRepeat :5635) and its
// CSSToStyleMap mapper (css_to_style_map.cc @ same SHA:
// MapNinePieceImageSlice :630, MapNinePieceImageQuad :670,
// MapNinePieceImageRepeat :685). An invalid longhand keeps its initial value.
func (s *Style) GetBorderImage() NinePieceImage {
	ctx := s.cssToLengthConversionData()
	n := NinePieceImage{
		ImageSlices: LengthBox{Length{Percent: 100}, Length{Percent: 100}, Length{Percent: 100}, Length{Percent: 100}},
		BorderSlices: BorderImageLengthBox{BorderImageLengthNumber(1), BorderImageLengthNumber(1),
			BorderImageLengthNumber(1), BorderImageLengthNumber(1)},
		Outset: BorderImageLengthBox{BorderImageLengthNumber(0), BorderImageLengthNumber(0),
			BorderImageLengthNumber(0), BorderImageLengthNumber(0)},
	}
	if v, ok := s.Get("border-image-slice"); ok {
		if slices, fill, ok := consumeBorderImageSlice(v, ctx); ok {
			n.ImageSlices, n.Fill = slices, fill
		}
	}
	if v, ok := s.Get("border-image-width"); ok {
		if box, ok := consumeBorderImageQuad(v, ctx, true); ok {
			n.BorderSlices = box
		}
	}
	if v, ok := s.Get("border-image-outset"); ok {
		if box, ok := consumeBorderImageQuad(v, ctx, false); ok {
			n.Outset = box
		}
	}
	if v, ok := s.Get("border-image-repeat"); ok {
		if h, vr, ok := consumeBorderImageRepeat(v); ok {
			n.HorizontalRule, n.VerticalRule = h, vr
		}
	}
	return n
}

// parseBorderImageNumber parses a non-negative <number>, bare or as a
// unitless math function (ConsumeNumber).
func parseBorderImageNumber(tok string, ctx calcContext) (float64, bool) {
	if f, err := strconv.ParseFloat(tok, 64); err == nil {
		return f, f >= 0
	}
	if !IsMathFunction(tok) || hasUnitOrPercent(tok) {
		return 0, false
	}
	f, ok := EvalMathFunction(tok, ctx)
	return f, ok && f >= 0
}

// hasUnitOrPercent reports whether a math expression carries a dimension or
// percentage term (letters outside the function names, or %), i.e. is not a
// pure <number>.
func hasUnitOrPercent(expr string) bool {
	for _, fn := range []string{"calc(", "min(", "max(", "clamp(", "sign(", "abs("} {
		expr = strings.ReplaceAll(expr, fn, "(")
	}
	return strings.ContainsAny(strings.ToLower(expr), "abcdefghijklmnopqrstuvwxyz%")
}

// parseBorderImageLengthPercent parses a non-negative <length-percentage>
// into a Length (ConsumeLengthOrPercent + StyleBuilderConverter). A calc()
// mixing px and % is folded linearly into the Fixed / Percent terms.
func parseBorderImageLengthPercent(tok string, ctx calcContext, allowPercent bool) (Length, bool) {
	if pct, ok := ParsePercentage(tok); ok {
		return Length{Percent: pct}, allowPercent && pct >= 0
	}
	if IsMathFunctionWithPercent(tok) {
		if !allowPercent {
			return Length{}, false
		}
		ctx.percentBase, ctx.percentResolvesToZero = 0, true
		fixed, ok := EvalMathFunction(tok, ctx)
		if !ok {
			return Length{}, false
		}
		ctx.percentBase, ctx.percentResolvesToZero = 100, false
		at100, ok := EvalMathFunction(tok, ctx)
		if !ok {
			return Length{}, false
		}
		return Length{Fixed: fixed, Percent: at100 - fixed}, true
	}
	if _, err := strconv.ParseFloat(tok, 64); err == nil && tok != "0" {
		return Length{}, false // unitless non-zero is a <number>, not a length
	}
	f, ok := parseLengthFullWithCh(tok, ctx)
	return Length{Fixed: f}, ok && f >= 0
}

// consumeBorderImageSlice parses border-image-slice:
// [<number> | <percentage>]{1,4} && fill?. Numbers are rounded and stored as
// fixed lengths (ConsumeBorderImageSlice css_parsing_utils.cc:5648-5686;
// ConvertBorderImageSliceSide css_to_style_map.cc:618-628 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func consumeBorderImageSlice(v string, ctx calcContext) (LengthBox, bool, bool) {
	toks := splitTopLevelFields(strings.TrimSpace(v))
	fill := false
	if len(toks) > 0 && toks[0] == "fill" {
		fill, toks = true, toks[1:]
	}
	var sides []Length
	for len(toks) > 0 && len(sides) < 4 {
		tok := toks[0]
		if pct, ok := ParsePercentage(tok); ok && pct >= 0 {
			sides = append(sides, Length{Percent: pct})
		} else if IsMathFunctionWithPercent(tok) {
			ctx.percentBase = 100
			f, ok := EvalMathFunction(tok, ctx)
			if !ok || f < 0 {
				break
			}
			sides = append(sides, Length{Percent: f})
		} else if f, ok := parseBorderImageNumber(tok, ctx); ok {
			sides = append(sides, Length{Fixed: float64(int(f + 0.5))})
		} else {
			break
		}
		toks = toks[1:]
	}
	if len(sides) == 0 {
		return LengthBox{}, false, false
	}
	if len(toks) > 0 && toks[0] == "fill" {
		if fill {
			return LengthBox{}, false, false
		}
		fill, toks = true, toks[1:]
	}
	if len(toks) > 0 {
		return LengthBox{}, false, false
	}
	f := expandFourValues(sides)
	return LengthBox{f[0], f[1], f[2], f[3]}, fill, true
}

// consumeBorderImageQuad parses border-image-width ([<length-percentage> |
// <number> | auto]{1,4}, isWidth) or border-image-outset ([<length> |
// <number>]{1,4}). Mirrors ConsumeBorderImageWidth / ConsumeBorderImageOutset
// (css_parsing_utils.cc:5688-5751) and ToBorderImageLength
// (css_to_style_map.cc:660-668) @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func consumeBorderImageQuad(v string, ctx calcContext, isWidth bool) (BorderImageLengthBox, bool) {
	toks := splitTopLevelFields(strings.TrimSpace(v))
	if len(toks) == 0 || len(toks) > 4 {
		return BorderImageLengthBox{}, false
	}
	sides := make([]BorderImageLength, 0, 4)
	for _, tok := range toks {
		if f, ok := parseBorderImageNumber(tok, ctx); ok {
			sides = append(sides, BorderImageLengthNumber(f))
		} else if l, ok := parseBorderImageLengthPercent(tok, ctx, isWidth); ok {
			sides = append(sides, BorderImageLengthOf(l))
		} else if isWidth && tok == "auto" {
			sides = append(sides, BorderImageLengthAuto())
		} else {
			return BorderImageLengthBox{}, false
		}
	}
	f := expandFourValues(sides)
	return BorderImageLengthBox{f[0], f[1], f[2], f[3]}, true
}

// consumeBorderImageRepeat parses border-image-repeat (ConsumeBorderImageRepeat
// css_parsing_utils.cc:5635-5646; MapNinePieceImageRepeat
// css_to_style_map.cc:685 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func consumeBorderImageRepeat(v string) (ENinePieceImageRule, ENinePieceImageRule, bool) {
	rule := func(kw string) (ENinePieceImageRule, bool) {
		switch kw {
		case "stretch":
			return StretchImageRule, true
		case "round":
			return RoundImageRule, true
		case "space":
			return SpaceImageRule, true
		case "repeat":
			return RepeatImageRule, true
		}
		return 0, false
	}
	toks := strings.Fields(strings.ToLower(v))
	if len(toks) == 0 || len(toks) > 2 {
		return 0, 0, false
	}
	h, ok := rule(toks[0])
	if !ok {
		return 0, 0, false
	}
	vr := h
	if len(toks) == 2 {
		if vr, ok = rule(toks[1]); !ok {
			return 0, 0, false
		}
	}
	return h, vr, true
}
