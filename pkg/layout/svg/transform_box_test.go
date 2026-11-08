package svg

import (
	"math"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// rectShape returns an SVGShape for <rect x y w h> with the given style
// declarations.
func rectShape(x, y, w, h float64, decls map[string]string) *SVGShape {
	s := &SVGShape{TagName: "rect", GeometryType: GeometryRect, Style: css.NewStyle(), Viewport: geometry.SizeF{Width: 400, Height: 300}}
	s.Path.MoveTo(x, y)
	s.Path.LineTo(x+w, y)
	s.Path.LineTo(x+w, y+h)
	s.Path.LineTo(x, y+h)
	s.Path.Close()
	s.FillBoundingBox = s.Path.BoundingBox()
	s.transform = geometry.Identity()
	for k, v := range decls {
		s.Style.Set(k, v)
	}
	return s
}

// ComputeReferenceBox picks the object bounding box for fill-box, the
// stroke bounding box for stroke-box and the nearest viewport for view-box,
// after UsedTransformBox maps content-box to fill-box and border-box to
// stroke-box (TransformHelper::ComputeReferenceBox, transform_helper.cc:93-117
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestComputeReferenceBox_SVG(t *testing.T) {
	for _, tc := range []struct {
		decls map[string]string
		want  geometry.RectF
	}{
		{map[string]string{}, geometry.NewRectF(0, 0, 400, 300)},
		{map[string]string{"transform-box": "view-box"}, geometry.NewRectF(0, 0, 400, 300)},
		{map[string]string{"transform-box": "fill-box"}, geometry.NewRectF(100, 50, 100, 60)},
		{map[string]string{"transform-box": "content-box"}, geometry.NewRectF(100, 50, 100, 60)},
		// No stroke: the stroke bounding box is the fill bounding box.
		{map[string]string{"transform-box": "stroke-box"}, geometry.NewRectF(100, 50, 100, 60)},
		{map[string]string{"transform-box": "stroke-box", "stroke": "black", "stroke-width": "20"}, geometry.NewRectF(90, 40, 120, 80)},
		{map[string]string{"transform-box": "border-box", "stroke": "black", "stroke-width": "20"}, geometry.NewRectF(90, 40, 120, 80)},
	} {
		s := rectShape(100, 50, 100, 60, tc.decls)
		if got := ComputeReferenceBox(s, s.Style, s.Viewport); got != tc.want {
			t.Errorf("%v: reference box %v, want %v", tc.decls, got, tc.want)
		}
	}
}

// fill-box geometry away from the viewport centre: the reference box
// origin is the bounding box origin, so the UA transform-origin 0 0 pivots
// on the shape's top-left corner and percentages resolve against its size.
// (WPT fill-box-002 places its fill box so its centre coincides with the
// viewport's and cannot tell fill-box from view-box.)
func TestElementTransform_FillBoxOffCentre(t *testing.T) {
	for _, tc := range []struct {
		name  string
		decls map[string]string
		attr  string
		in    geometry.PointF
		want  geometry.PointF
	}{
		// rotate(90deg) about the box origin (100, 50).
		{"rotate-about-box-origin", map[string]string{"transform": "rotate(90deg)"}, "",
			geometry.PointF{X: 150, Y: 50}, geometry.PointF{X: 100, Y: 100}},
		// Percentages of the 100x60 box, not the 400x300 viewport.
		{"translate-percent", map[string]string{"transform": "translate(50%, -50%)"}, "",
			geometry.PointF{X: 100, Y: 50}, geometry.PointF{X: 150, Y: 20}},
		// An authored origin resolves within the box: 25% 25% is (125, 65).
		{"authored-origin", map[string]string{"transform": "rotate(180deg)", "transform-origin": "25% 25%"}, "",
			geometry.PointF{X: 100, Y: 50}, geometry.PointF{X: 150, Y: 80}},
		// The transform attribute (a presentation attribute for
		// `transform`) also pivots on the fill-box origin.
		{"attribute", map[string]string{}, "rotate(90)",
			geometry.PointF{X: 150, Y: 50}, geometry.PointF{X: 100, Y: 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := rectShape(100, 50, 100, 60, tc.decls)
			s.Style.Set("transform-box", "fill-box")
			s.transform = ParseSVGTransform(tc.attr)
			m := ElementTransform(s, s.Style, s.Viewport)
			got := m.MapPoint(tc.in)
			if absDiff(got.X, tc.want.X) > 1e-9 || absDiff(got.Y, tc.want.Y) > 1e-9 {
				t.Errorf("%v mapped to %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// ApproximateStrokeBoundingBox outsets by half the stroke width, scaled
// for miters (paths) and square caps (paths and lines)
// (LayoutSVGShape::ApproximateStrokeBoundingBox, layout_svg_shape.cc:201-229
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestApproximateStrokeBoundingBox(t *testing.T) {
	bounds := geometry.NewRectF(0, 0, 100, 100)
	for _, tc := range []struct {
		name     string
		geometry SVGGeometryType
		decls    map[string]string
		delta    float64
	}{
		{"rect-miter", GeometryRect, map[string]string{}, 5},
		{"path-miter-default-limit", GeometryPath, map[string]string{}, 20},
		{"path-round-join", GeometryPath, map[string]string{"stroke-linejoin": "round"}, 5},
		{"path-square-cap", GeometryPath, map[string]string{"stroke-linejoin": "round", "stroke-linecap": "square"}, 5 * math.Sqrt2},
		{"path-small-miter-square-cap", GeometryPath, map[string]string{"stroke-miterlimit": "1.2", "stroke-linecap": "square"}, 5 * math.Sqrt2},
		{"line-square-cap", GeometryLine, map[string]string{"stroke-linecap": "square"}, 5 * math.Sqrt2},
		{"line-miter", GeometryLine, map[string]string{}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &SVGShape{GeometryType: tc.geometry, Style: css.NewStyle()}
			s.Style.Set("stroke-width", "10")
			for k, v := range tc.decls {
				s.Style.Set(k, v)
			}
			got := s.ApproximateStrokeBoundingBox(bounds)
			want := geometry.NewRectF(-tc.delta, -tc.delta, 100+2*tc.delta, 100+2*tc.delta)
			if absDiff(got.X(), want.X()) > 1e-9 || absDiff(got.Y(), want.Y()) > 1e-9 ||
				absDiff(got.Width(), want.Width()) > 1e-9 || absDiff(got.Height(), want.Height()) > 1e-9 {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// A container's stroke bounding box is the union of its rendered
// children's, each mapped into the container's space
// (SVGContentContainer::ComputeStrokeBoundingBox,
// svg_content_container.cc:254-266 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestSVGContainer_StrokeBoundingBox(t *testing.T) {
	a := rectShape(100, 100, 100, 50, map[string]string{"stroke": "black", "stroke-width": "20"})
	b := rectShape(0, 0, 10, 10, map[string]string{})
	b.transform = geometry.Translate(300, 0)
	c := &SVGContainer{}
	c.AppendChild(a)
	c.AppendChild(b)
	c.UpdateSVGLayout(SVGLayoutInfo{}) // the stroke box is cached at layout, as the object box is
	if got, want := c.StrokeBoundingBox(), geometry.NewRectF(90, 0, 220, 160); got != want {
		t.Errorf("container stroke bbox %v, want %v", got, want)
	}
}

// lineShape returns an SVGShape for <line x1 y x2 y> with the given style
// declarations.
func lineShape(x1, x2, y float64, decls map[string]string) *SVGShape {
	s := &SVGShape{TagName: "line", GeometryType: GeometryLine, Style: css.NewStyle(), Viewport: geometry.SizeF{Width: 400, Height: 300}}
	s.Path.MoveTo(x1, y)
	s.Path.LineTo(x2, y)
	s.FillBoundingBox = s.Path.BoundingBox()
	s.transform = geometry.Identity()
	for k, v := range decls {
		s.Style.Set(k, v)
	}
	return s
}

// A container's object bounding box unions its valid children even when
// their boxes are empty (UpdateObjectBoundingBox's UnionEvenIfEmpty,
// svg_content_container.cc:77-86 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// a horizontal <line> at y 0 extends the box of a rect at (40, 40) to the
// line.
func TestSVGContainer_ObjectBoundingBoxUnionsEmptyChildBoxes(t *testing.T) {
	c := &SVGContainer{}
	c.AppendChild(lineShape(0, 100, 0, map[string]string{"stroke": "black", "stroke-width": "2"}))
	c.AppendChild(rectShape(40, 40, 60, 20, map[string]string{}))
	c.UpdateSVGLayout(SVGLayoutInfo{})
	if got, want := c.ObjectBoundingBox(), geometry.NewRectF(0, 0, 100, 60); got != want {
		t.Errorf("container bbox %v, want %v", got, want)
	}
}

// A nested container counts toward its parent when its object bounding box
// is valid, empty or not (IsObjectBoundingBoxValid, svg_content_container.cc:
// 95-98): a <g> holding only a horizontal <line> adds the line's stroke to
// its parent's stroke box. (The line's stroke box is louis14's uniform
// approximation, half the width on every side: LOU-667.)
func TestSVGContainer_NestedContainerWithEmptyBoxCounts(t *testing.T) {
	inner := NewSVGContainer("g")
	inner.AppendChild(lineShape(10, 90, 20, map[string]string{"stroke": "black", "stroke-width": "10"}))
	outer := NewSVGContainer("g")
	outer.AppendChild(inner)
	outer.UpdateSVGLayout(SVGLayoutInfo{})
	if got, want := outer.StrokeBoundingBox(), geometry.NewRectF(5, 15, 90, 10); got != want {
		t.Errorf("outer stroke bbox %v, want %v", got, want)
	}
}

// Content never laid out (a <mask>'s, a <pattern>'s, a <clipPath>'s) still
// reports its stroke box: the cache is filled on demand, never returned
// unfilled.
func TestSVGContainer_StrokeBoundingBoxWithoutLayout(t *testing.T) {
	c := &SVGContainer{}
	c.AppendChild(rectShape(10, 10, 80, 80, map[string]string{}))
	if got, want := c.StrokeBoundingBox(), geometry.NewRectF(10, 10, 80, 80); got != want {
		t.Errorf("stroke bbox without layout %v, want %v", got, want)
	}
}

// A container's stroke box unions its children's stroke boxes with plain
// Union, which absorbs empty ones (ComputeStrokeBoundingBox,
// svg_content_container.cc:254-266; gfx::RectF::Union, rect_f.cc:134-143 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an unstroked horizontal line
// does not stretch it to a rect far below; only the object box extends by
// empty boxes (UnionEvenIfEmpty).
func TestSVGContainer_StrokeBoxAbsorbsEmptyChildBoxes(t *testing.T) {
	c := NewSVGContainer("g")
	c.AppendChild(lineShape(0, 100, 0, map[string]string{}))
	c.AppendChild(rectShape(40, 200, 60, 20, map[string]string{}))
	c.UpdateSVGLayout(SVGLayoutInfo{})
	if got, want := c.StrokeBoundingBox(), geometry.NewRectF(40, 200, 60, 20); got != want {
		t.Errorf("stroke bbox %v, want %v", got, want)
	}
}

// Appending a child refills a container's boxes, filled lazily or at
// layout.
func TestSVGContainer_AppendChildRefillsBoxes(t *testing.T) {
	for _, c := range []interface {
		SVGNode
		AppendChild(SVGNode)
	}{NewSVGContainer("g"), &SVGViewportContainer{}} {
		c.AppendChild(rectShape(10, 10, 10, 10, map[string]string{}))
		_ = c.StrokeBoundingBox()
		c.AppendChild(rectShape(50, 50, 10, 10, map[string]string{}))
		if got, want := c.StrokeBoundingBox(), geometry.NewRectF(10, 10, 50, 50); got != want {
			t.Errorf("%T: stroke bbox after AppendChild %v, want %v", c, got, want)
		}
	}
}

// Appending to a nested container refills its ancestors' boxes too.
func TestSVGContainer_AppendChildRefillsAncestors(t *testing.T) {
	inner := NewSVGContainer("g")
	outer := NewSVGContainer("g")
	outer.AppendChild(inner)
	inner.AppendChild(rectShape(10, 10, 10, 10, map[string]string{}))
	_ = outer.StrokeBoundingBox()
	inner.AppendChild(rectShape(50, 50, 10, 10, map[string]string{}))
	if got, want := outer.StrokeBoundingBox(), geometry.NewRectF(10, 10, 50, 50); got != want {
		t.Errorf("outer stroke bbox after a nested AppendChild %v, want %v", got, want)
	}
}

// Setting a container's transform invalidates its ancestors' boxes, which
// map its boxes through it.
func TestSVGContainer_SetSVGTransformRefillsAncestors(t *testing.T) {
	inner := NewSVGContainer("g")
	inner.AppendChild(rectShape(10, 10, 10, 10, map[string]string{}))
	outer := NewSVGContainer("g")
	outer.AppendChild(inner)
	_ = outer.StrokeBoundingBox()
	inner.SetSVGTransform(geometry.Translate(100, 0))
	if got, want := outer.StrokeBoundingBox(), geometry.NewRectF(110, 10, 10, 10); got != want {
		t.Errorf("outer stroke bbox after SetSVGTransform %v, want %v", got, want)
	}
}
