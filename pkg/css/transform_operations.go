package css

import (
	"math"
	"strings"

	"louis14/pkg/geometry"
)

// Transform operations: the computed value of the `transform` property.
// Ports Blink's platform/transforms TransformOperation hierarchy
// (transform_operation.h and the *_transform_operation.h subclasses @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Blink keeps them beside Length
// in platform/; louis14's Length lives in pkg/css, so these do too.

// TransformOperationType mirrors TransformOperation::OperationType
// (transform_operation.h:41-64).
type TransformOperationType int

const (
	TransformOperationScaleX TransformOperationType = iota
	TransformOperationScaleY
	TransformOperationScale
	TransformOperationTranslateX
	TransformOperationTranslateY
	TransformOperationTranslate
	TransformOperationRotate
	TransformOperationRotateZ
	TransformOperationSkewX
	TransformOperationSkewY
	TransformOperationSkew
	TransformOperationMatrix
	TransformOperationScaleZ
	TransformOperationScale3D
	TransformOperationTranslateZ
	TransformOperationTranslate3D
	TransformOperationRotateX
	TransformOperationRotateY
	TransformOperationRotate3D
	TransformOperationMatrix3D
	TransformOperationPerspective
)

// TransformOperation is one function of a computed transform list. Mirrors
// blink::TransformOperation.
type TransformOperation interface {
	// Type mirrors TransformOperation::GetType.
	Type() TransformOperationType
	// Apply post-multiplies t by this operation's matrix; borderBoxSize is
	// the percentage basis. Mirrors TransformOperation::Apply.
	Apply(t *geometry.Transform, borderBoxSize geometry.SizeF)
}

// TranslateTransformOperation mirrors blink::TranslateTransformOperation
// (translate_transform_operation.h): x and y are Lengths resolved against
// the border box at Apply time, z is an absolute length.
type TranslateTransformOperation struct {
	X, Y Length
	Z    float64
	typ  TransformOperationType
}

func (o *TranslateTransformOperation) Type() TransformOperationType { return o.typ }

// XForSize and YForSize mirror TranslateTransformOperation::X(size) / Y(size).
func (o *TranslateTransformOperation) XForSize(size geometry.SizeF) float64 {
	return o.X.ValueForLength(size.Width)
}
func (o *TranslateTransformOperation) YForSize(size geometry.SizeF) float64 {
	return o.Y.ValueForLength(size.Height)
}

func (o *TranslateTransformOperation) Apply(t *geometry.Transform, size geometry.SizeF) {
	t.Translate3d(o.XForSize(size), o.YForSize(size), o.Z)
}

// RotateTransformOperation mirrors blink::RotateTransformOperation
// (rotate_transform_operation.h): an axis and an angle in degrees.
type RotateTransformOperation struct {
	X, Y, Z float64 // axis
	Angle   float64 // degrees
	typ     TransformOperationType
}

func (o *RotateTransformOperation) Type() TransformOperationType { return o.typ }

func (o *RotateTransformOperation) Apply(t *geometry.Transform, _ geometry.SizeF) {
	if o.typ == TransformOperationRotate {
		t.Rotate(o.Angle)
		return
	}
	t.RotateAbout(o.X, o.Y, o.Z, o.Angle)
}

// ScaleTransformOperation mirrors blink::ScaleTransformOperation.
type ScaleTransformOperation struct {
	X, Y, Z float64
	typ     TransformOperationType
}

func (o *ScaleTransformOperation) Type() TransformOperationType { return o.typ }

func (o *ScaleTransformOperation) Apply(t *geometry.Transform, _ geometry.SizeF) {
	t.Scale3d(o.X, o.Y, o.Z)
}

// SkewTransformOperation mirrors blink::SkewTransformOperation (angles in
// degrees).
type SkewTransformOperation struct {
	AngleX, AngleY float64
	typ            TransformOperationType
}

func (o *SkewTransformOperation) Type() TransformOperationType { return o.typ }

func (o *SkewTransformOperation) Apply(t *geometry.Transform, _ geometry.SizeF) {
	t.Skew(o.AngleX, o.AngleY)
}

// MatrixTransformOperation mirrors blink::MatrixTransformOperation:
// matrix(a, b, c, d, e, f).
type MatrixTransformOperation struct {
	A, B, C, D, E, F float64
}

func (o *MatrixTransformOperation) Type() TransformOperationType { return TransformOperationMatrix }

// Matrix mirrors MatrixTransformOperation::Matrix.
func (o *MatrixTransformOperation) Matrix() geometry.Transform {
	return geometry.TransformAffine(o.A, o.B, o.C, o.D, o.E, o.F)
}

func (o *MatrixTransformOperation) Apply(t *geometry.Transform, _ geometry.SizeF) {
	t.PreConcat(o.Matrix())
}

// Matrix3DTransformOperation mirrors blink::Matrix3DTransformOperation.
type Matrix3DTransformOperation struct {
	Matrix geometry.Transform
}

func (o *Matrix3DTransformOperation) Type() TransformOperationType {
	return TransformOperationMatrix3D
}

func (o *Matrix3DTransformOperation) Apply(t *geometry.Transform, _ geometry.SizeF) {
	t.PreConcat(o.Matrix)
}

// PerspectiveTransformOperation mirrors blink::PerspectiveTransformOperation;
// HasPerspective false is perspective(none).
type PerspectiveTransformOperation struct {
	Perspective    float64
	HasPerspective bool
}

func (o *PerspectiveTransformOperation) Type() TransformOperationType {
	return TransformOperationPerspective
}

// UsedPerspective mirrors PerspectiveTransformOperation::UsedPerspective
// (perspective_transform_operation.h:44-47): at least 1px.
func (o *PerspectiveTransformOperation) UsedPerspective() float64 {
	return math.Max(1, o.Perspective)
}

func (o *PerspectiveTransformOperation) Apply(t *geometry.Transform, _ geometry.SizeF) {
	if o.HasPerspective {
		t.ApplyPerspectiveDepth(o.UsedPerspective())
	}
}

// TransformOperations is a computed transform list. Mirrors
// blink::TransformOperations (transform_operations.h).
type TransformOperations struct {
	Operations []TransformOperation
}

// IsEmpty mirrors TransformOperations::IsEmpty.
func (o TransformOperations) IsEmpty() bool { return len(o.Operations) == 0 }

// Is3DOperation reports whether op's type is a 3D operation type, whatever
// its values. Mirrors TransformOperation::Is3DOperation
// (transform_operation.h:109-116 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// louis14 has no kInterpolated operation.
func Is3DOperation(op TransformOperation) bool {
	switch op.Type() {
	case TransformOperationScaleZ, TransformOperationScale3D,
		TransformOperationTranslateZ, TransformOperationTranslate3D,
		TransformOperationRotateX, TransformOperationRotateY, TransformOperationRotate3D,
		TransformOperationMatrix3D, TransformOperationPerspective:
		return true
	}
	return false
}

// HasNonPerspective3DOperation reports whether any operation has a 3D
// operation type other than perspective. Mirrors
// TransformOperations::HasNonPerspective3DOperation (transform_operations.h:
// 81-88 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (o TransformOperations) HasNonPerspective3DOperation() bool {
	for _, op := range o.Operations {
		if Is3DOperation(op) && op.Type() != TransformOperationPerspective {
			return true
		}
	}
	return false
}

// Apply post-multiplies t by every operation in list order. Mirrors
// TransformOperations::Apply (transform_operations.h:58-61).
func (o TransformOperations) Apply(borderBoxSize geometry.SizeF, t *geometry.Transform) {
	for _, op := range o.Operations {
		op.Apply(t, borderBoxSize)
	}
}

// Interpolation: TransformOperation::Blend and TransformOperations::Blend
// (transform_operations.cc:124-215 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// for the operations louis14 blends component-wise. Lists that need matrix
// interpolation (BlendRemainingByUsingMatrixInterpolation, which uses
// gfx::Transform::Blend's decomposition) and rotations about different axes
// (Rotation::Slerp) are not ported: Blend reports false and the caller
// falls back to discrete interpolation, as Blink does when matrix blending
// fails.

// blendNumber mirrors blink::Blend(from, to, progress).
func blendNumber(from, to, progress float64) float64 { return from + (to-from)*progress }

// blendLength mirrors Length::Blend for fixed and percent lengths: louis14's
// Length holds a fixed and a percent part, so a px/% mix blends exactly (as
// Blink's calc() result does). A calculated length does not blend.
func blendLength(from, to Length, progress float64) (Length, bool) {
	if from.IsCalculated() || to.IsCalculated() {
		return Length{}, false
	}
	return Length{
		Fixed:   blendNumber(from.Fixed, to.Fixed, progress),
		Percent: blendNumber(from.Percent, to.Percent, progress),
	}, true
}

// primitiveType mirrors TransformOperation::PrimitiveType: translate and
// scale operations share a 3D primitive, every rotation is kRotate3D, the
// rest are their own type.
func primitiveType(op TransformOperation) TransformOperationType {
	switch op.(type) {
	case *TranslateTransformOperation:
		return TransformOperationTranslate3D
	case *ScaleTransformOperation:
		return TransformOperationScale3D
	case *RotateTransformOperation:
		return TransformOperationRotate3D
	}
	return op.Type()
}

// canBlendWith mirrors TransformOperation::CanBlendWith.
func canBlendWith(a, b TransformOperation) bool { return primitiveType(a) == primitiveType(b) }

// commonPrimitiveForInterpolation mirrors the translate and scale
// CommonPrimitiveForInterpolation: the same type is kept, otherwise the 2D
// or 3D primitive.
func commonPrimitiveForInterpolation(to, from TransformOperationType, is3D bool, twoD, threeD TransformOperationType) TransformOperationType {
	switch {
	case to == from:
		return to
	case is3D:
		return threeD
	}
	return twoD
}

func (o *TranslateTransformOperation) is3DOperation() bool {
	return o.typ == TransformOperationTranslate3D || o.typ == TransformOperationTranslateZ
}

func (o *ScaleTransformOperation) is3DOperation() bool {
	return o.typ == TransformOperationScale3D || o.typ == TransformOperationScaleZ
}

// blendOperation blends from (nil = the matching identity) to to (nil =
// blend from toward identity). Mirrors the per-type Blend overrides.
func blendOperation(from, to TransformOperation, progress float64) (TransformOperation, bool) {
	if to == nil {
		return blendToIdentity(from, progress)
	}
	switch t := to.(type) {
	case *TranslateTransformOperation:
		f, _ := from.(*TranslateTransformOperation)
		if f == nil {
			f = &TranslateTransformOperation{typ: t.typ}
		}
		x, okx := blendLength(f.X, t.X, progress)
		y, oky := blendLength(f.Y, t.Y, progress)
		if !okx || !oky {
			return nil, false
		}
		typ := commonPrimitiveForInterpolation(t.typ, f.typ, t.is3DOperation() || f.is3DOperation(), TransformOperationTranslate, TransformOperationTranslate3D)
		return &TranslateTransformOperation{X: x, Y: y, Z: blendNumber(f.Z, t.Z, progress), typ: typ}, true
	case *ScaleTransformOperation:
		f, _ := from.(*ScaleTransformOperation)
		if f == nil {
			f = &ScaleTransformOperation{X: 1, Y: 1, Z: 1, typ: t.typ}
		}
		typ := commonPrimitiveForInterpolation(t.typ, f.typ, t.is3DOperation() || f.is3DOperation(), TransformOperationScale, TransformOperationScale3D)
		return &ScaleTransformOperation{X: blendNumber(f.X, t.X, progress), Y: blendNumber(f.Y, t.Y, progress), Z: blendNumber(f.Z, t.Z, progress), typ: typ}, true
	case *SkewTransformOperation:
		f, _ := from.(*SkewTransformOperation)
		if f == nil {
			f = &SkewTransformOperation{typ: t.typ}
		}
		return &SkewTransformOperation{AngleX: blendNumber(f.AngleX, t.AngleX, progress), AngleY: blendNumber(f.AngleY, t.AngleY, progress), typ: t.typ}, true
	case *RotateTransformOperation:
		f, _ := from.(*RotateTransformOperation)
		if f == nil {
			return &RotateTransformOperation{X: t.X, Y: t.Y, Z: t.Z, Angle: t.Angle * progress, typ: t.typ}, true
		}
		// Rotation::GetCommonAxis: a zero rotation shares the other's axis.
		axis := t
		switch {
		case t.Angle == 0:
			axis = f
		case f.Angle != 0 && !sameRotationAxis(f, t):
			return nil, false // Rotation::Slerp across axes is not ported.
		}
		typ := t.typ
		if f.typ != t.typ {
			typ = TransformOperationRotate3D
		}
		return &RotateTransformOperation{X: axis.X, Y: axis.Y, Z: axis.Z, Angle: blendNumber(f.Angle, t.Angle, progress), typ: typ}, true
	}
	return nil, false
}

// blendToIdentity mirrors Blend(nullptr, progress, blend_to_identity=true).
func blendToIdentity(from TransformOperation, progress float64) (TransformOperation, bool) {
	switch f := from.(type) {
	case *TranslateTransformOperation:
		x, okx := blendLength(f.X, Length{}, progress)
		y, oky := blendLength(f.Y, Length{}, progress)
		if !okx || !oky {
			return nil, false
		}
		return &TranslateTransformOperation{X: x, Y: y, Z: blendNumber(f.Z, 0, progress), typ: f.typ}, true
	case *ScaleTransformOperation:
		return &ScaleTransformOperation{X: blendNumber(f.X, 1, progress), Y: blendNumber(f.Y, 1, progress), Z: blendNumber(f.Z, 1, progress), typ: f.typ}, true
	case *SkewTransformOperation:
		return &SkewTransformOperation{AngleX: blendNumber(f.AngleX, 0, progress), AngleY: blendNumber(f.AngleY, 0, progress), typ: f.typ}, true
	case *RotateTransformOperation:
		return &RotateTransformOperation{X: f.X, Y: f.Y, Z: f.Z, Angle: f.Angle * (1 - progress), typ: f.typ}, true
	}
	return nil, false
}

// sameRotationAxis reports whether two rotations share a normalized axis,
// the case Rotation::Slerp reduces to an angle blend.
func sameRotationAxis(a, b *RotateTransformOperation) bool {
	la := a.X*a.X + a.Y*a.Y + a.Z*a.Z
	lb := b.X*b.X + b.Y*b.Y + b.Z*b.Z
	if la == 0 || lb == 0 {
		return false
	}
	const eps = 1e-9
	return math.Abs(a.X*b.Y-a.Y*b.X) < eps && math.Abs(a.Y*b.Z-a.Z*b.Y) < eps &&
		math.Abs(a.Z*b.X-a.X*b.Z) < eps && a.X*b.X+a.Y*b.Y+a.Z*b.Z > 0
}

// MatchingPrefixLength mirrors TransformOperations::MatchingPrefixLength.
func (o TransformOperations) MatchingPrefixLength(other TransformOperations) int {
	n := min(len(o.Operations), len(other.Operations))
	for i := 0; i < n; i++ {
		if !canBlendWith(o.Operations[i], other.Operations[i]) {
			return i
		}
	}
	return max(len(o.Operations), len(other.Operations))
}

// Blend interpolates from toward o at progress (TransformOperations::Blend).
// It reports false where Blink would need matrix interpolation, which
// louis14 does not port; Blink then also falls back to discrete.
func (o TransformOperations) Blend(from TransformOperations, progress float64) (TransformOperations, bool) {
	if from.IsEmpty() && o.IsEmpty() {
		return o, true
	}
	n := max(len(o.Operations), len(from.Operations))
	if o.MatchingPrefixLength(from) < n {
		return TransformOperations{}, false
	}
	result := TransformOperations{Operations: make([]TransformOperation, 0, n)}
	for i := 0; i < n; i++ {
		var f, t TransformOperation
		if i < len(from.Operations) {
			f = from.Operations[i]
		}
		if i < len(o.Operations) {
			t = o.Operations[i]
		}
		op, ok := blendOperation(f, t, progress)
		if !ok {
			return TransformOperations{}, false
		}
		result.Operations = append(result.Operations, op)
	}
	return result, true
}

// CSSText serializes the list as a `transform` value that
// CreateTransformOperations parses back to the same operations; an empty
// list is `none`. The interpolation path writes blended lists as text.
func (o TransformOperations) CSSText() string {
	if o.IsEmpty() {
		return "none"
	}
	parts := make([]string, 0, len(o.Operations))
	for _, op := range o.Operations {
		parts = append(parts, transformOperationCSSText(op))
	}
	return strings.Join(parts, " ")
}

// lengthCSSText serializes a Length: px, %, or calc() for a px/% mix.
func lengthCSSText(l Length) string {
	switch {
	case l.IsCalculated():
		return l.calculation.expr
	case l.Percent != 0 && l.Fixed != 0:
		return "calc(" + formatPx(l.Fixed) + " + " + formatNum(l.Percent) + "%)"
	case l.Percent != 0:
		return formatNum(l.Percent) + "%"
	}
	return formatPx(l.Fixed)
}

func transformOperationCSSText(op TransformOperation) string {
	deg := func(v float64) string { return formatNum(v) + "deg" }
	nums := func(vs ...float64) string {
		s := make([]string, len(vs))
		for i, v := range vs {
			s[i] = formatNum(v)
		}
		return strings.Join(s, ", ")
	}
	switch o := op.(type) {
	case *TranslateTransformOperation:
		switch o.typ {
		case TransformOperationTranslateX:
			return "translateX(" + lengthCSSText(o.X) + ")"
		case TransformOperationTranslateY:
			return "translateY(" + lengthCSSText(o.Y) + ")"
		case TransformOperationTranslateZ:
			return "translateZ(" + formatPx(o.Z) + ")"
		case TransformOperationTranslate3D:
			return "translate3d(" + lengthCSSText(o.X) + ", " + lengthCSSText(o.Y) + ", " + formatPx(o.Z) + ")"
		}
		return "translate(" + lengthCSSText(o.X) + ", " + lengthCSSText(o.Y) + ")"
	case *RotateTransformOperation:
		switch o.typ {
		case TransformOperationRotate:
			return "rotate(" + deg(o.Angle) + ")"
		case TransformOperationRotateZ:
			return "rotateZ(" + deg(o.Angle) + ")"
		case TransformOperationRotateX:
			return "rotateX(" + deg(o.Angle) + ")"
		case TransformOperationRotateY:
			return "rotateY(" + deg(o.Angle) + ")"
		}
		return "rotate3d(" + nums(o.X, o.Y, o.Z) + ", " + deg(o.Angle) + ")"
	case *ScaleTransformOperation:
		switch o.typ {
		case TransformOperationScaleX:
			return "scaleX(" + formatNum(o.X) + ")"
		case TransformOperationScaleY:
			return "scaleY(" + formatNum(o.Y) + ")"
		case TransformOperationScaleZ:
			return "scaleZ(" + formatNum(o.Z) + ")"
		case TransformOperationScale3D:
			return "scale3d(" + nums(o.X, o.Y, o.Z) + ")"
		}
		return "scale(" + nums(o.X, o.Y) + ")"
	case *SkewTransformOperation:
		switch o.typ {
		case TransformOperationSkewX:
			return "skewX(" + deg(o.AngleX) + ")"
		case TransformOperationSkewY:
			return "skewY(" + deg(o.AngleY) + ")"
		}
		return "skew(" + deg(o.AngleX) + ", " + deg(o.AngleY) + ")"
	case *MatrixTransformOperation:
		return "matrix(" + nums(o.A, o.B, o.C, o.D, o.E, o.F) + ")"
	case *Matrix3DTransformOperation:
		v := make([]float64, 16)
		for i := range v {
			v[i] = o.Matrix.Rc(i%4, i/4)
		}
		return "matrix3d(" + nums(v...) + ")"
	case *PerspectiveTransformOperation:
		if !o.HasPerspective {
			return "perspective(none)"
		}
		return "perspective(" + formatPx(o.Perspective) + ")"
	}
	return ""
}
