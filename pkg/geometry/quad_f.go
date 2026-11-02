package geometry

// QuadF is a quadrilateral, gfx::QuadF (ui/gfx/geometry/quad_f.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type QuadF struct {
	P1, P2, P3, P4 PointF
}

// QuadFFromRect mirrors QuadF(const RectF&): origin, top right, bottom
// right, bottom left.
func QuadFFromRect(r RectF) QuadF {
	return QuadF{
		P1: r.Origin,
		P2: PointF{r.Right(), r.Y()},
		P3: PointF{r.Right(), r.Bottom()},
		P4: PointF{r.X(), r.Bottom()},
	}
}
