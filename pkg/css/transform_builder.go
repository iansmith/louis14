package css

import (
	"strconv"
	"strings"

	"louis14/pkg/geometry"
)

// CreateTransformOperations builds the computed transform list for a
// `transform` value: each function becomes a TransformOperation with its
// lengths converted against ctx (em, rem, viewport units and calc() become
// px; percentages stay Lengths for the reference box). Mirrors
// TransformBuilder::CreateTransformOperations and the per-function
// CreateTransformOperation (core/css/resolver/transform_builder.cc:96-372 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// louis14 parses the value text here; Blink's parser
// (ConsumeTransformList) has already rejected an invalid list, whereas this
// drops an unparseable function and keeps the rest.
func CreateTransformOperations(value string, ctx calcContext) TransformOperations {
	var ops TransformOperations
	value = strings.TrimSpace(value)
	if value == "" || value == "none" {
		return ops
	}
	for _, fn := range splitTransformFunctions(value) {
		if op := createTransformOperation(fn.name, fn.args, ctx); op != nil {
			ops.Operations = append(ops.Operations, op)
		}
	}
	return ops
}

type transformFunction struct{ name, args string }

// splitTransformFunctions splits a transform list into name(args) pieces.
// An unclosed function takes the rest of the value.
func splitTransformFunctions(val string) []transformFunction {
	var fns []transformFunction
	for {
		val = strings.TrimSpace(val)
		open := strings.IndexByte(val, '(')
		if open < 0 {
			return fns
		}
		name := strings.ToLower(strings.TrimSpace(val[:open]))
		end := findMatchingParen(val[open:])
		if end < 0 {
			return append(fns, transformFunction{name: name, args: strings.TrimSpace(val[open+1:])})
		}
		fns = append(fns, transformFunction{name: name, args: strings.TrimSpace(val[open+1 : open+end])})
		val = val[open+end+1:]
	}
}

// transformComputeLength converts a z / perspective argument, which takes
// no percentage: CSSPrimitiveValue::ComputeLength<double>.
func transformComputeLength(tok string, ctx calcContext) (float64, bool) {
	if strings.Contains(tok, "%") || isUnitlessNonZeroNumber(tok) {
		return 0, false
	}
	return parseLengthFullWithCh(tok, ctx)
}

func transformNumber(tok string) (float64, bool) {
	v, err := strconv.ParseFloat(tok, 64)
	return v, err == nil
}

// createTransformOperation mirrors the anonymous CreateTransformOperation
// (transform_builder.cc:96-350). It returns nil for an unknown function or
// unparseable arguments.
func createTransformOperation(name, rawArgs string, ctx calcContext) TransformOperation {
	args := SplitTopLevelCommas(rawArgs)
	switch name {
	case "scale", "scalex", "scaley":
		if len(args) > 2 || (name != "scale" && len(args) != 1) {
			return nil
		}
		first, ok := parseScaleFactor(args[0])
		if !ok {
			return nil
		}
		switch name {
		case "scalex":
			return &ScaleTransformOperation{X: first, Y: 1, Z: 1, typ: TransformOperationScaleX}
		case "scaley":
			return &ScaleTransformOperation{X: 1, Y: first, Z: 1, typ: TransformOperationScaleY}
		}
		sy := first
		if len(args) > 1 {
			if sy, ok = parseScaleFactor(args[1]); !ok {
				return nil
			}
		}
		return &ScaleTransformOperation{X: first, Y: sy, Z: 1, typ: TransformOperationScale}
	case "scalez":
		if len(args) != 1 {
			return nil
		}
		sz, ok := transformNumber(args[0])
		if !ok {
			return nil
		}
		return &ScaleTransformOperation{X: 1, Y: 1, Z: sz, typ: TransformOperationScaleZ}
	case "scale3d":
		if len(args) != 3 {
			return nil
		}
		sx, okx := transformNumber(args[0])
		sy, oky := transformNumber(args[1])
		sz, okz := transformNumber(args[2])
		if !okx || !oky || !okz {
			return nil
		}
		return &ScaleTransformOperation{X: sx, Y: sy, Z: sz, typ: TransformOperationScale3D}
	case "translate", "translatex", "translatey":
		if len(args) > 2 || (name != "translate" && len(args) != 1) {
			return nil
		}
		first, ok := convertLength(args[0], ctx)
		if !ok {
			return nil
		}
		switch name {
		case "translatex":
			return &TranslateTransformOperation{X: first, typ: TransformOperationTranslateX}
		case "translatey":
			return &TranslateTransformOperation{Y: first, typ: TransformOperationTranslateY}
		}
		var ty Length
		if len(args) > 1 {
			if ty, ok = convertLength(args[1], ctx); !ok {
				return nil
			}
		}
		return &TranslateTransformOperation{X: first, Y: ty, typ: TransformOperationTranslate}
	case "translatez":
		if len(args) != 1 {
			return nil
		}
		tz, ok := transformComputeLength(args[0], ctx)
		if !ok {
			return nil
		}
		return &TranslateTransformOperation{Z: tz, typ: TransformOperationTranslateZ}
	case "translate3d":
		if len(args) != 3 {
			return nil
		}
		tx, okx := convertLength(args[0], ctx)
		ty, oky := convertLength(args[1], ctx)
		tz, okz := transformComputeLength(args[2], ctx)
		if !okx || !oky || !okz {
			return nil
		}
		return &TranslateTransformOperation{X: tx, Y: ty, Z: tz, typ: TransformOperationTranslate3D}
	case "rotate", "rotatez", "rotatex", "rotatey":
		if len(args) != 1 {
			return nil
		}
		angle, ok := parseAngleValue(args[0])
		if !ok {
			return nil
		}
		switch name {
		case "rotatex":
			return &RotateTransformOperation{X: 1, Angle: angle, typ: TransformOperationRotateX}
		case "rotatey":
			return &RotateTransformOperation{Y: 1, Angle: angle, typ: TransformOperationRotateY}
		case "rotatez":
			return &RotateTransformOperation{Z: 1, Angle: angle, typ: TransformOperationRotateZ}
		}
		return &RotateTransformOperation{Z: 1, Angle: angle, typ: TransformOperationRotate}
	case "rotate3d":
		if len(args) != 4 {
			return nil
		}
		x, okx := transformNumber(args[0])
		y, oky := transformNumber(args[1])
		z, okz := transformNumber(args[2])
		angle, oka := parseAngleValue(args[3])
		if !okx || !oky || !okz || !oka {
			return nil
		}
		return &RotateTransformOperation{X: x, Y: y, Z: z, Angle: angle, typ: TransformOperationRotate3D}
	case "skew", "skewx", "skewy":
		if len(args) > 2 || (name != "skew" && len(args) != 1) {
			return nil
		}
		angle, ok := parseAngleValue(args[0])
		if !ok {
			return nil
		}
		switch name {
		case "skewy":
			return &SkewTransformOperation{AngleY: angle, typ: TransformOperationSkewY}
		case "skewx":
			return &SkewTransformOperation{AngleX: angle, typ: TransformOperationSkewX}
		}
		var angleY float64
		if len(args) > 1 {
			if angleY, ok = parseAngleValue(args[1]); !ok {
				return nil
			}
		}
		return &SkewTransformOperation{AngleX: angle, AngleY: angleY, typ: TransformOperationSkew}
	case "matrix":
		v, ok := transformNumbers(args, 6)
		if !ok {
			return nil
		}
		return &MatrixTransformOperation{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}
	case "matrix3d":
		v, ok := transformNumbers(args, 16)
		if !ok {
			return nil
		}
		return &Matrix3DTransformOperation{Matrix: geometry.TransformColMajor(v[0], v[1], v[2], v[3],
			v[4], v[5], v[6], v[7],
			v[8], v[9], v[10], v[11],
			v[12], v[13], v[14], v[15])}
	case "perspective":
		if len(args) != 1 {
			return nil
		}
		if strings.EqualFold(args[0], "none") {
			return &PerspectiveTransformOperation{}
		}
		p, ok := transformComputeLength(args[0], ctx)
		if !ok || p < 0 {
			return nil
		}
		return &PerspectiveTransformOperation{Perspective: p, HasPerspective: true}
	}
	return nil
}

// transformNumbers parses exactly n <number> arguments.
func transformNumbers(args []string, n int) ([]float64, bool) {
	if len(args) != n {
		return nil, false
	}
	v := make([]float64, n)
	for i, a := range args {
		f, ok := transformNumber(a)
		if !ok {
			return nil, false
		}
		v[i] = f
	}
	return v, true
}

// TransformOrigin is the computed transform-origin: x and y Lengths of the
// reference box plus an absolute z. Mirrors blink::TransformOrigin
// (core/style/transform_origin.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type TransformOrigin struct {
	X, Y Length
	Z    float64
}

// initialTransformOrigin is `50% 50% 0` (ComputedStyle::InitialTransformOrigin).
var initialTransformOrigin = TransformOrigin{X: Length{Percent: 50}, Y: Length{Percent: 50}}

// convertTransformOrigin mirrors StyleBuilderConverter::ConvertTransformOrigin
// (style_builder_converter.cc:3276-3295): keyword or length-percentage x and
// y via ConvertPositionLength, z a computed length.
func convertTransformOrigin(value string, ctx calcContext) TransformOrigin {
	parts := splitShorthandParts(value)
	xPart, yPart := classifyOriginParts(parts)
	origin := initialTransformOrigin
	if xPart != "" {
		if l, ok := convertPositionLength(originAxis(xPart), "left", "right", ctx); ok {
			origin.X = l
		}
	}
	if yPart != "" {
		if l, ok := convertPositionLength(originAxis(yPart), "top", "bottom", ctx); ok {
			origin.Y = l
		}
	}
	if len(parts) >= 3 {
		if z, ok := transformComputeLength(parts[2], ctx); ok {
			origin.Z = z
		}
	}
	return origin
}

// originAxis wraps one classified transform-origin token as the positionAxis
// convertPositionLength takes: a keyword edge or a <length-percentage>.
func originAxis(tok string) positionAxis {
	switch tok = strings.TrimSpace(tok); tok {
	case "left", "right", "top", "bottom", "center":
		return positionAxis{edge: tok}
	}
	return positionAxis{offset: tok}
}
