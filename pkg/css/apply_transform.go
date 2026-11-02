package css

import (
	"strings"

	"louis14/pkg/geometry"
)

// ApplyTransformOperations, ApplyTransformOrigin and
// ApplyIndependentTransformProperties select the parts ApplyTransform
// composes. They mirror the ComputedStyle enums of the same names
// (core/style/computed_style.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type ApplyTransformOperations bool

const (
	IncludeTransformOperations ApplyTransformOperations = true
	ExcludeTransformOperations ApplyTransformOperations = false
)

type ApplyTransformOrigin bool

const (
	IncludeTransformOrigin ApplyTransformOrigin = true
	ExcludeTransformOrigin ApplyTransformOrigin = false
)

type ApplyIndependentTransformProperties bool

const (
	IncludeIndependentTransformProperties ApplyIndependentTransformProperties = true
	ExcludeIndependentTransformProperties ApplyIndependentTransformProperties = false
)

// ApplyTransform post-multiplies result by this style's transform for a box
// whose reference box is boundingBox: transform-origin, then the translate,
// rotate and scale properties, then the transform list, then the origin
// back out. Mirrors ComputedStyle::ApplyTransform
// (core/style/computed_style.cc:1434-1496 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). louis14 has no offset-path,
// so the motion-path step (ApplyMotionPath) is absent.
func (s *Style) ApplyTransform(result *geometry.Transform, boundingBox geometry.RectF,
	applyOperations ApplyTransformOperations, applyOrigin ApplyTransformOrigin,
	applyIndependent ApplyIndependentTransformProperties) {
	boxSize := boundingBox.Size
	var originX, originY, originZ float64
	if applyOrigin {
		origin := s.GetTransformOrigin()
		originX = origin.X.ValueForLength(boxSize.Width) + boundingBox.Origin.X
		originY = origin.Y.ValueForLength(boxSize.Height) + boundingBox.Origin.Y
		originZ = origin.Z
		result.Translate3d(originX, originY, originZ)
	}
	if applyIndependent {
		if t := s.Translate(); t != nil {
			t.Apply(result, boxSize)
		}
		if r := s.Rotate(); r != nil {
			r.Apply(result, boxSize)
		}
		if sc := s.Scale(); sc != nil {
			sc.Apply(result, boxSize)
		}
	}
	if applyOperations {
		s.Transform().Apply(boxSize, result)
	}
	if applyOrigin {
		result.Translate3d(-originX, -originY, -originZ)
	}
}

// HasTransform mirrors ComputedStyle::HasTransform: a transform list or any
// of the translate, rotate and scale properties.
func (s *Style) HasTransform() bool {
	return s.HasTransformOperations() || s.Translate() != nil || s.Rotate() != nil || s.Scale() != nil
}

// computedIndependentTransform is a computed translate, rotate or scale
// property (nil op = none) with the property text it was computed from, and
// whether `inherit` copied it from the parent — the computedTransform
// scheme for the independent transform properties.
type computedIndependentTransform struct {
	op        TransformOperation
	source    string
	inherited bool
}

// independentTransformProperties are the properties ComputedStyle keeps as
// Translate(), Rotate() and Scale(), in ApplyTransform order.
var independentTransformProperties = [3]string{"translate", "rotate", "scale"}

// independentTransform returns the computed value of independent transform
// property i (0 translate, 1 rotate, 2 scale), from the cache when it was
// computed for the current text.
func (s *Style) independentTransform(i int) TransformOperation {
	prop := independentTransformProperties[i]
	if c := s.independent[i]; c != nil && c.source == s.Properties[prop] {
		return c.op
	}
	return s.convertIndependentTransform(i)
}

// convertIndependentTransform builds independent transform property i from
// its text: StyleBuilderConverter::ConvertTranslate / ConvertRotate /
// ConvertScale (style_builder_converter.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (s *Style) convertIndependentTransform(i int) TransformOperation {
	val, ok := s.Get(independentTransformProperties[i])
	val = strings.TrimSpace(val)
	if !ok || val == "" || val == "none" {
		return nil
	}
	parts := splitShorthandParts(val)
	switch i {
	case 0:
		if op := convertTranslate(parts, s.cssToLengthConversionData()); op != nil {
			return op
		}
	case 1:
		if op := convertRotate(parts); op != nil {
			return op
		}
	case 2:
		if op := convertScale(parts); op != nil {
			return op
		}
	}
	return nil
}

// Translate returns the computed `translate` property, nil for none. Mirrors
// ComputedStyle::Translate.
func (s *Style) Translate() *TranslateTransformOperation {
	op, _ := s.independentTransform(0).(*TranslateTransformOperation)
	return op
}

// Rotate returns the computed `rotate` property, nil for none. Mirrors
// ComputedStyle::Rotate.
func (s *Style) Rotate() *RotateTransformOperation {
	op, _ := s.independentTransform(1).(*RotateTransformOperation)
	return op
}

// Scale returns the computed `scale` property, nil for none. Mirrors
// ComputedStyle::Scale.
func (s *Style) Scale() *ScaleTransformOperation {
	op, _ := s.independentTransform(2).(*ScaleTransformOperation)
	return op
}

// convertTranslate mirrors StyleBuilderConverter::ConvertTranslate: x and y
// Lengths, z a computed length, as a kTranslate3D operation.
func convertTranslate(parts []string, ctx calcContext) *TranslateTransformOperation {
	if len(parts) > 3 {
		return nil
	}
	op := &TranslateTransformOperation{typ: TransformOperationTranslate3D}
	var ok bool
	if op.X, ok = convertLength(parts[0], ctx); !ok {
		return nil
	}
	if len(parts) > 1 {
		if op.Y, ok = convertLength(parts[1], ctx); !ok {
			return nil
		}
	}
	if len(parts) > 2 {
		if op.Z, ok = transformComputeLength(parts[2], ctx); !ok {
			return nil
		}
	}
	return op
}

// convertRotate mirrors StyleBuilderConverter::ConvertRotate: an angle about
// the z axis, or about x, y, z or a <number>{3} axis, as kRotate3D.
func convertRotate(parts []string) *RotateTransformOperation {
	op := &RotateTransformOperation{Z: 1, typ: TransformOperationRotate3D}
	var axis []string
	angleSeen := false
	for _, p := range parts {
		if a, ok := parseAngleValue(p); ok && !angleSeen {
			op.Angle, angleSeen = a, true
			continue
		}
		axis = append(axis, p)
	}
	if !angleSeen {
		return nil
	}
	switch len(axis) {
	case 0:
	case 1:
		switch strings.ToLower(axis[0]) {
		case "x":
			op.X, op.Y, op.Z = 1, 0, 0
		case "y":
			op.X, op.Y, op.Z = 0, 1, 0
		case "z":
		default:
			return nil
		}
	case 3:
		v, ok := transformNumbers(axis, 3)
		if !ok {
			return nil
		}
		op.X, op.Y, op.Z = v[0], v[1], v[2]
	default:
		return nil
	}
	return op
}

// convertScale mirrors StyleBuilderConverter::ConvertScale: one to three
// numbers or percentages, y defaulting to x, as kScale3D.
func convertScale(parts []string) *ScaleTransformOperation {
	if len(parts) > 3 {
		return nil
	}
	v := []float64{1, 1, 1}
	for i, p := range parts {
		f, ok := parseScaleFactor(p)
		if !ok {
			return nil
		}
		v[i] = f
	}
	if len(parts) == 1 {
		v[1] = v[0]
	}
	return &ScaleTransformOperation{X: v[0], Y: v[1], Z: v[2], typ: TransformOperationScale3D}
}
