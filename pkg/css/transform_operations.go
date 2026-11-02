package css

import (
	"math"

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

// Apply post-multiplies t by every operation in list order. Mirrors
// TransformOperations::Apply (transform_operations.h:58-61).
func (o TransformOperations) Apply(borderBoxSize geometry.SizeF, t *geometry.Transform) {
	for _, op := range o.Operations {
		op.Apply(t, borderBoxSize)
	}
}
