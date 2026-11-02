package geometry

import "testing"

// gfx::Vector3dF / QuadF / Transform::MapVector (ui/gfx/geometry @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestVector3dF(t *testing.T) {
	a, b := Vector3dF{1, 0, 0}, Vector3dF{0, 1, 0}
	if got := CrossProduct(a, b); got != (Vector3dF{0, 0, 1}) {
		t.Errorf("CrossProduct = %v, want (0,0,1)", got)
	}
	if got := DotProduct(Vector3dF{1, 2, 3}, Vector3dF{4, 5, 6}); got != 32 {
		t.Errorf("DotProduct = %v, want 32", got)
	}
	v := Vector3dF{3, 4, 0}
	if v.Length() != 5 || v.LengthSquared() != 25 {
		t.Errorf("Length = %v, LengthSquared = %v", v.Length(), v.LengthSquared())
	}
	v.InvScale(5)
	if v != (Vector3dF{0.6000000238418579, 0.800000011920929, 0}) {
		t.Errorf("InvScale = %v, want float (0.6, 0.8, 0)", v)
	}
	if got := (Point3F{3, 5, 7}).Sub(Point3F{1, 1, 1}); got != (Vector3dF{2, 4, 6}) {
		t.Errorf("Point3F.Sub = %v", got)
	}
}

func TestQuadFFromRect(t *testing.T) {
	q := QuadFFromRect(NewRectF(1, 2, 3, 4))
	want := QuadF{P1: PointF{1, 2}, P2: PointF{4, 2}, P3: PointF{4, 6}, P4: PointF{1, 6}}
	if q != want {
		t.Errorf("QuadFFromRect = %v, want %v", q, want)
	}
}

func TestTransformMapVectorIgnoresTranslation(t *testing.T) {
	var tr Transform
	tr.Translate3d(10, 20, 30)
	tr.Scale3d(2, 3, 4)
	if got := tr.MapVector(Vector3dF{1, 1, 1}); got != (Vector3dF{2, 3, 4}) {
		t.Errorf("MapVector = %v, want (2,3,4)", got)
	}
	v := [4]float64{1, 1, 1, 1}
	tr.TransformVector4(&v)
	if v != [4]float64{12, 23, 34, 1} {
		t.Errorf("TransformVector4 = %v, want [12 23 34 1]", v)
	}
}
