package svg

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGGeometryType enumerates the resolved geometry classes of an
// SVGShape, mirroring Blink's LayoutSVGShape::GeometryType
// (core/layout/svg/layout_svg_shape.h). Carries enough information for
// downstream painters (and Phase 5 mask/clip) to skip cheap
// AABB-vs-path tests against the path's intrinsic shape (a rect can
// short-circuit a stroke-path test, etc.).
type SVGGeometryType int

const (
	// GeometryPath is the fallback — a free-form parsed `d` path. No
	// short-circuit possible for fill/clip queries.
	GeometryPath SVGGeometryType = iota
	// GeometryLine is a single-segment line from `<line>`.
	GeometryLine
	// GeometryRect is a non-rounded rectangle.
	GeometryRect
	// GeometryRoundedRect is a rectangle with non-zero rx/ry.
	GeometryRoundedRect
	// GeometryEllipse is a fully general ellipse — `<ellipse>` and
	// `<circle>` (which is a square-radii ellipse) collapse into here.
	GeometryEllipse
	// GeometryCircle distinguishes a `<circle>` (rx == ry) from
	// `<ellipse>` for the cheap circle-vs-AABB hit-test path. Phase 2
	// itself does not branch on this — preserved for parity with Blink.
	GeometryCircle
)

// SVGShape is the layout node for a single SVG shape element: `<rect>`,
// `<circle>`, `<ellipse>`, `<line>`, `<polyline>`, `<polygon>`,
// `<path>`. Mirrors Blink's LayoutSVGShape (core/layout/svg/layout_svg_shape.{h,cc}):
// the shape resolves its geometry attributes into a Path at layout
// time and caches the fill bounding box for upstream consumers.
//
// Phase 2: layout populates path + fillBoundingBox; the painter
// (pkg/render/svg_shape_painter.go) consumes the path. Stroke
// bounding box is computed at paint time from style.stroke-width
// because per SVG 2 §13.4.2 the stroke bbox depends on style which
// can change without rebuilding geometry.
type SVGShape struct {
	// TagName is the shape's element tag (lowercased), e.g. "rect".
	// Used for debug logging and by the painter for tag-specific
	// fast paths (none in Phase 2 — painters dispatch through Path).
	TagName string

	// GeometryType is the shape's intrinsic geometry class.
	GeometryType SVGGeometryType

	// Path is the resolved geometry as a normalized SVG Path
	// (move/line/quad/cubic/close). Built by UpdateShapeFromElement.
	// For `<line>` and `<polyline>`/`<polygon>` this is a sequence of
	// line segments; for `<rect>`/`<ellipse>`/`<circle>` it is the
	// canonical Path approximation that fill/stroke consume.
	Path Path

	// FillBoundingBox is the tight AABB of Path control points in
	// user-space coords, computed at layout time. Mirrors Blink's
	// LayoutSVGShape::fill_bounding_box_.
	FillBoundingBox geometry.RectF

	// Element holds a reference to the original SVG element adapter
	// so the SVGObjectPainter can read presentation attributes off it
	// at paint time (until they're folded into computed style by
	// the cascade-side wiring). Kept as an ElementAdapter to honor
	// the package's no-import-cycle rule.
	Element ElementAdapter

	// Style is the resolved computed style for the shape's element,
	// produced by the same css.ComputeStyle path as ordinary CSS
	// boxes. Set by the layout-side tree builder (which has the
	// per-node styles map) — the layout/svg package itself doesn't
	// run the cascade. Mirrors how Blink's LayoutSVGShape reaches
	// its style via the element's ComputedStyle().
	Style *css.Style
}

// NewSVGShape builds an SVGShape from a shape ElementAdapter, resolving
// geometry attributes against the supplied SVGLengthContext (the
// active viewport at this point in the tree). Mirrors Blink's
// LayoutSVGShape construction + UpdateShapeFromElement: the element's
// geometry attributes are read, the path is built, and the fill
// bounding box is precomputed. Tags this constructor doesn't recognize
// produce nil — caller falls back to SVGContainer.
func NewSVGShape(elt ElementAdapter, lengthCtx SVGLengthContext) *SVGShape {
	if elt == nil {
		return nil
	}
	tag := elt.TagName()
	shape := &SVGShape{TagName: tag, Element: elt}
	switch tag {
	case "rect":
		buildRectPath(shape, elt, lengthCtx)
	case "circle":
		buildCirclePath(shape, elt, lengthCtx)
	case "ellipse":
		buildEllipsePath(shape, elt, lengthCtx)
	case "line":
		buildLinePath(shape, elt, lengthCtx)
	case "polyline":
		buildPolyPath(shape, elt, false)
	case "polygon":
		buildPolyPath(shape, elt, true)
	case "path":
		buildPath(shape, elt)
	default:
		return nil
	}
	shape.FillBoundingBox = shape.Path.BoundingBox()
	return shape
}

// ObjectBoundingBox returns the shape's fill bounding box.
func (s *SVGShape) ObjectBoundingBox() geometry.RectF {
	return s.FillBoundingBox
}

// LocalTransform returns identity — shape elements do not have their
// own SVG transform attribute (only containers do). Mirrors Blink's
// LayoutSVGShape::LocalSVGTransform which returns identity for
// non-transformable shapes (transform is on the containing
// LayoutSVGTransformableContainer / <g>).
func (s *SVGShape) LocalTransform() geometry.AffineTransform {
	return geometry.Identity()
}

// UpdateSVGLayout is a no-op in Phase 2: the path was built at
// construction time and louis14 doesn't yet incrementally invalidate
// SVG geometry. Mirrors what Blink's LayoutSVGShape::UpdateLayout does
// for the initial pass: nothing changes between construction and
// first paint.
func (s *SVGShape) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	return SVGLayoutResult{}
}

// Paint is a no-op here — actual painting is driven by
// pkg/render/svg_shape_painter.go which holds the DrawContext.
// The SVGShape only owns the geometry; render owns the rasterization.
func (s *SVGShape) Paint(ctx *SVGPaintContext) {
	// Intentionally empty. The render-side painter walks the SVG
	// tree directly to keep the DrawContext out of the layout
	// package (mirrors Blink's split between LayoutSVGShape and
	// SVGShapePainter).
}

// buildRectPath builds the geometry for `<rect>`. Per SVG 1.1 §9.2:
//   - x, y default to 0
//   - width, height default to 0 (zero-sized rect produces nothing)
//   - rx defaults to ry (and vice versa) if either is missing; if both
//     are missing they're 0 (sharp corners). Both are clamped to half
//     the corresponding side length.
//
// Negative width/height makes the rect invalid (the spec says "shall be
// treated as an error" — Blink's behavior is to skip the path).
func buildRectPath(shape *SVGShape, elt ElementAdapter, ctx SVGLengthContext) {
	x := getLength(elt, "x", LengthModeWidth, ctx, 0)
	y := getLength(elt, "y", LengthModeHeight, ctx, 0)
	w := getLength(elt, "width", LengthModeWidth, ctx, 0)
	h := getLength(elt, "height", LengthModeHeight, ctx, 0)
	if w <= 0 || h <= 0 {
		shape.GeometryType = GeometryRect
		return
	}

	rx, hasRx := getOptLength(elt, "rx", LengthModeWidth, ctx)
	ry, hasRy := getOptLength(elt, "ry", LengthModeHeight, ctx)
	if !hasRx && hasRy {
		rx = ry
	}
	if !hasRy && hasRx {
		ry = rx
	}
	if rx < 0 {
		rx = 0
	}
	if ry < 0 {
		ry = 0
	}
	// Clamp to half-side.
	if rx > w/2 {
		rx = w / 2
	}
	if ry > h/2 {
		ry = h / 2
	}

	if rx == 0 || ry == 0 {
		// Sharp-corner rect: M x,y → x+w,y → x+w,y+h → x,y+h → Z.
		shape.GeometryType = GeometryRect
		shape.Path.MoveTo(x, y)
		shape.Path.LineTo(x+w, y)
		shape.Path.LineTo(x+w, y+h)
		shape.Path.LineTo(x, y+h)
		shape.Path.Close()
		return
	}

	shape.GeometryType = GeometryRoundedRect
	// Build a rounded rectangle using four 90° arc segments. The
	// constant k = 0.5522847498... = 4·(√2−1)/3 is the standard cubic
	// approximation to a quarter circle/ellipse (relative to radius).
	// Mirrors gfx::Path::AddRoundedRect.
	const k = 0.5522847498307933
	cx := rx * k
	cy := ry * k

	// Start at top-edge just past the top-left curve, walk clockwise.
	shape.Path.MoveTo(x+rx, y)
	shape.Path.LineTo(x+w-rx, y)
	shape.Path.CubicTo(x+w-rx+cx, y, x+w, y+ry-cy, x+w, y+ry)
	shape.Path.LineTo(x+w, y+h-ry)
	shape.Path.CubicTo(x+w, y+h-ry+cy, x+w-rx+cx, y+h, x+w-rx, y+h)
	shape.Path.LineTo(x+rx, y+h)
	shape.Path.CubicTo(x+rx-cx, y+h, x, y+h-ry+cy, x, y+h-ry)
	shape.Path.LineTo(x, y+ry)
	shape.Path.CubicTo(x, y+ry-cy, x+rx-cx, y, x+rx, y)
	shape.Path.Close()
}

// buildCirclePath builds the geometry for `<circle>`. Per SVG 1.1 §9.3:
//   - cx, cy default to 0
//   - r default to 0 (zero-radius circle produces nothing)
//   - negative r is invalid.
func buildCirclePath(shape *SVGShape, elt ElementAdapter, ctx SVGLengthContext) {
	cx := getLength(elt, "cx", LengthModeWidth, ctx, 0)
	cy := getLength(elt, "cy", LengthModeHeight, ctx, 0)
	r := getLength(elt, "r", LengthModeOther, ctx, 0)
	if r <= 0 {
		shape.GeometryType = GeometryCircle
		return
	}
	shape.GeometryType = GeometryCircle
	buildEllipseAt(&shape.Path, cx, cy, r, r)
}

// buildEllipsePath builds the geometry for `<ellipse>`. Per SVG 1.1 §9.4:
//   - cx, cy default to 0
//   - rx, ry default to 0; either negative or zero leaves the ellipse
//     unrendered.
func buildEllipsePath(shape *SVGShape, elt ElementAdapter, ctx SVGLengthContext) {
	cx := getLength(elt, "cx", LengthModeWidth, ctx, 0)
	cy := getLength(elt, "cy", LengthModeHeight, ctx, 0)
	rx := getLength(elt, "rx", LengthModeWidth, ctx, 0)
	ry := getLength(elt, "ry", LengthModeHeight, ctx, 0)
	if rx <= 0 || ry <= 0 {
		shape.GeometryType = GeometryEllipse
		return
	}
	shape.GeometryType = GeometryEllipse
	buildEllipseAt(&shape.Path, cx, cy, rx, ry)
}

// buildEllipseAt appends a clockwise ellipse centered at (cx, cy) with
// radii (rx, ry) onto the path, using the standard 4-cubic Bezier
// approximation (k = 4·(√2−1)/3). Maximum error vs. true ellipse is
// approximately 2.7e-4 · max(rx, ry), well under sub-pixel for typical
// SVG sizes — Blink uses the same approximation in
// gfx::Path::AddEllipse.
func buildEllipseAt(p *Path, cx, cy, rx, ry float64) {
	const k = 0.5522847498307933
	kx := rx * k
	ky := ry * k
	// Start at rightmost point, walk clockwise.
	p.MoveTo(cx+rx, cy)
	p.CubicTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	p.CubicTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	p.CubicTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	p.CubicTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	p.Close()
}

// buildLinePath builds the geometry for `<line>`. Per SVG 1.1 §9.5:
//   - x1, y1, x2, y2 default to 0.
//
// A line with coincident endpoints renders nothing for fill but may
// still produce a stroke cap dot if `stroke-linecap: round/square` is
// set.
func buildLinePath(shape *SVGShape, elt ElementAdapter, ctx SVGLengthContext) {
	x1 := getLength(elt, "x1", LengthModeWidth, ctx, 0)
	y1 := getLength(elt, "y1", LengthModeHeight, ctx, 0)
	x2 := getLength(elt, "x2", LengthModeWidth, ctx, 0)
	y2 := getLength(elt, "y2", LengthModeHeight, ctx, 0)
	shape.GeometryType = GeometryLine
	shape.Path.MoveTo(x1, y1)
	shape.Path.LineTo(x2, y2)
}

// buildPolyPath builds the geometry for `<polyline>` (close=false) and
// `<polygon>` (close=true). Per SVG 1.1 §9.6 + §9.7: the `points`
// attribute is a list of (x, y) coordinate pairs; a polyline draws
// open line segments through them; a polygon additionally closes with
// a `Z` back to the first point.
//
// A `points` value that doesn't yield at least one pair produces an
// empty path (the element renders nothing) — Blink does the same.
func buildPolyPath(shape *SVGShape, elt ElementAdapter, close bool) {
	shape.GeometryType = GeometryPath
	val, ok := elt.Attribute("points")
	if !ok {
		return
	}
	pts := parsePointList(val)
	if len(pts) == 0 {
		return
	}
	shape.Path.MoveTo(pts[0].X, pts[0].Y)
	for i := 1; i < len(pts); i++ {
		shape.Path.LineTo(pts[i].X, pts[i].Y)
	}
	if close {
		shape.Path.Close()
	}
}

// buildPath builds the geometry for `<path>`. Delegates to the full
// SVG path-data mini-parser in svg_path.go.
func buildPath(shape *SVGShape, elt ElementAdapter) {
	shape.GeometryType = GeometryPath
	if d, ok := elt.Attribute("d"); ok {
		shape.Path = ParsePathData(d)
	}
}

// getLength reads an SVG length attribute and resolves it against the
// length context using the supplied mode. Missing attributes return
// the default value.
func getLength(elt ElementAdapter, name string, mode SVGLengthMode, ctx SVGLengthContext, defaultVal float64) float64 {
	v, ok := elt.Attribute(name)
	if !ok {
		return defaultVal
	}
	return ctx.Resolve(v, mode)
}

// getOptLength reads an SVG length attribute and reports whether it
// was set. Used for rx/ry where the spec distinguishes "absent" from
// "explicit 0" (the symmetry rule rx-defaults-to-ry only applies when
// one is absent).
func getOptLength(elt ElementAdapter, name string, mode SVGLengthMode, ctx SVGLengthContext) (float64, bool) {
	v, ok := elt.Attribute(name)
	if !ok {
		return 0, false
	}
	return ctx.Resolve(v, mode), true
}
