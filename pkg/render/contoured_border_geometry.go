package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
)

// This file ports Blink's ContouredBorderGeometry
// (core/paint/contoured_border_geometry.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and the slice of
// FloatRoundedRect (platform/geometry/float_rounded_rect.{h,cc} @ a9f50e52)
// the border and background painters consume.
//
// Blink returns a ContouredRect (platform/geometry/contoured_rect.h @
// a9f50e52): a FloatRoundedRect plus per-corner superellipse curvature for
// `corner-shape`. louis14 has no corner-shape, so every corner has round
// curvature and a ContouredRect reduces to its FloatRoundedRect; this port
// keeps only the FloatRoundedRect.

// FloatRoundedRect is a rect with elliptical corner radii. Mirrors Blink's
// FloatRoundedRect (platform/geometry/float_rounded_rect.h @ a9f50e52).
// Radii use css.EllipticalRadii ([TL, TR, BR, BL]), louis14's single radii
// type; it lives in pkg/css, which pkg/geometry may not import, so this
// type sits beside its only consumers in pkg/render.
type FloatRoundedRect struct {
	Rect  geometry.RectF
	Radii css.EllipticalRadii
}

// IsRounded reports whether any corner has a radius
// (FloatRoundedRect::IsRounded, float_rounded_rect.h @ a9f50e52).
func (r FloatRoundedRect) IsRounded() bool { return !r.Radii.IsZero() }

// IsEmpty reports whether the rect has no area
// (FloatRoundedRect::IsEmpty, float_rounded_rect.h @ a9f50e52).
func (r FloatRoundedRect) IsEmpty() bool { return r.Rect.IsEmpty() }

// IsRenderable reports whether the adjacent radii on every side fit inside
// that side, so Skia can draw the rrect without rescaling its radii.
// Mirrors FloatRoundedRect::IsRenderable (float_rounded_rect.cc:356-367 @
// a9f50e52).
func (r FloatRoundedRect) IsRenderable() bool {
	const tolerance = 1.0001
	rd, w, h := r.Radii, r.Rect.Width()*tolerance, r.Rect.Height()*tolerance
	return rd[0].Rx+rd[1].Rx <= w && rd[3].Rx+rd[2].Rx <= w &&
		rd[0].Ry+rd[3].Ry <= h && rd[1].Ry+rd[2].Ry <= h
}

// ConstrainRadii scales all radii by one factor so they fit the rect
// (FloatRoundedRect::ConstrainRadii, float_rounded_rect.cc:336-354 @
// a9f50e52).
func (r *FloatRoundedRect) ConstrainRadii() {
	r.Radii = r.Radii.ConstrainRadii(r.Rect.Width(), r.Rect.Height())
}

// renderedRadii returns the radii Skia draws the rrect with: SkRRect's
// setRectRadii scales radii that do not fit the rect down by the same
// factor ConstrainRadii uses, so a non-renderable rrect paints (and clips)
// as its constrained form (FloatRoundedRect's SkRRect conversion,
// float_rounded_rect.h @ a9f50e52).
func (r FloatRoundedRect) renderedRadii() css.EllipticalRadii {
	return r.Radii.ConstrainRadii(r.Rect.Width(), r.Rect.Height())
}

// addPath appends the rrect as a clockwise closed subpath.
func (r FloatRoundedRect) addPath(dc pathBuilder) {
	buildRoundedRectPathOnDC(dc, r.Rect.X(), r.Rect.Y(), r.Rect.Width(), r.Rect.Height(), r.renderedRadii())
}

// addReversePath appends the rrect as a counter-clockwise closed subpath,
// which cuts a hole out of a clockwise subpath under the nonzero rule the
// louis14 rasterizer uses.
func (r FloatRoundedRect) addReversePath(dc pathBuilder) {
	buildRoundedRectPathReverseDC(dc, r.Rect.X(), r.Rect.Y(), r.Rect.Width(), r.Rect.Height(), r.renderedRadii())
}

// pathBuilder is the path-building subset of textshape.DrawContext.
type pathBuilder interface {
	MoveTo(x, y float64)
	LineTo(x, y float64)
	CubicTo(x1, y1, x2, y2, x3, y3 float64)
	ClosePath()
}

// rectFFromInts converts a snapped integer rect to a RectF.
func rectFFromInts(x, y, w, h int) geometry.RectF {
	return geometry.NewRectF(float64(x), float64(y), float64(w), float64(h))
}

// PixelSnappedContouredBorder returns the pixel-snapped border-box rrect:
// the snapped rect with the style radii constrained to fit it. radii are
// the style's corner radii already resolved against border_rect's size
// (CalcRadiiFor, contoured_border_geometry.cc:27-46). Mirrors
// ContouredBorderGeometry::PixelSnappedContouredBorder
// (contoured_border_geometry.cc:178-185, ComputeContouredBorderFromStyle
// :135-176 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func PixelSnappedContouredBorder(radii css.EllipticalRadii, borderRect geometry.PhysicalRect) FloatRoundedRect {
	s := geometry.ToPixelSnappedRect(borderRect)
	rr := FloatRoundedRect{Rect: rectFFromInts(s.Min.X, s.Min.Y, s.Dx(), s.Dy())}
	if !radii.IsZero() {
		rr.Radii = radii
		rr.ConstrainRadii()
	}
	return rr
}

// PixelSnappedContouredBorderWithOutsets returns border_rect expanded by
// outsets and snapped (offset rounded, sizes snapped without the thin-line
// clause so a thin inner border can snap to zero), with the snapped border
// radii outset by the same amounts and NOT re-constrained: an inner rrect
// may come out non-renderable. Mirrors
// ContouredBorderGeometry::PixelSnappedContouredBorderWithOutsets
// (contoured_border_geometry.cc:219-255 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func PixelSnappedContouredBorderWithOutsets(radii css.EllipticalRadii, borderRect geometry.PhysicalRect, outsets geometry.PhysicalBoxStrut) FloatRoundedRect {
	r := borderRect.ExpandEdges(outsets.Top, outsets.Right, outsets.Bottom, outsets.Left)
	r.Size.Width = layoutunit.Max(r.Size.Width, layoutunit.Zero)
	r.Size.Height = layoutunit.Max(r.Size.Height, layoutunit.Zero)
	rr := FloatRoundedRect{Rect: rectFFromInts(
		int(r.Offset.Left.Round()), int(r.Offset.Top.Round()),
		int(layoutunit.SnapSizeToPixelAllowingZero(r.Size.Width, r.Offset.Left)),
		int(layoutunit.SnapSizeToPixelAllowingZero(r.Size.Height, r.Offset.Top)))}
	if !radii.IsZero() {
		o := PixelSnappedContouredBorder(radii, borderRect).Radii
		t, rt, b, l := outsets.Top.Float64(), outsets.Right.Float64(), outsets.Bottom.Float64(), outsets.Left.Float64()
		// Radii::Outset (float_rounded_rect.cc:115-132): grow (or, for a
		// negative outset, shrink) only the non-zero radii. css.Inset
		// zeroes a corner once either of its radii reaches zero, which is
		// how SkRRect then draws such a corner.
		if t <= 0 && rt <= 0 && b <= 0 && l <= 0 {
			rr.Radii = o.Inset(-t, -rt, -b, -l)
		} else {
			rr.Radii = o.Outset(t, rt, b, l)
		}
	}
	return rr
}

// snappedBorderWidths returns the used border widths [top, right, bottom,
// left] as whole pixels. Blink's are ints at computed-value time
// (ComputedStyle::BorderTopWidth(), computed_style.h:606 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; ConvertBorderWidth rounds a
// sub-pixel width up to 1 and floors the rest). louis14 lays boxes out
// with the unfloored widths, so each width is instead snapped against its
// unsnapped edge (SnapSizeToPixel, with its thin-line clause standing in
// for the sub-pixel round-up): the border then covers exactly the pixels
// between the snapped border edge and the snapped padding edge the
// background clips to.
func snappedBorderWidths(borderRect geometry.PhysicalRect, borders geometry.PhysicalBoxStrut) [4]int {
	return [4]int{
		int(layoutunit.SnapSizeToPixel(borders.Top, borderRect.Offset.Top)),
		int(layoutunit.SnapSizeToPixel(borders.Right, borderRect.Right().Sub(borders.Right))),
		int(layoutunit.SnapSizeToPixel(borders.Bottom, borderRect.Bottom().Sub(borders.Bottom))),
		int(layoutunit.SnapSizeToPixel(borders.Left, borderRect.Offset.Left)),
	}
}

// innerBorderOutsets returns the outsets from the border edge to the
// padding edge, the negated used border widths
// (PhysicalBoxStrut::FromInts(-style.BorderTopWidth(), ...),
// contoured_border_geometry.cc:211-216 @ a9f50e52).
func innerBorderOutsets(borderRect geometry.PhysicalRect, borders geometry.PhysicalBoxStrut) geometry.PhysicalBoxStrut {
	w := snappedBorderWidths(borderRect, borders)
	return geometry.PhysicalBoxStrut{Top: layoutunit.New(-w[0]), Right: layoutunit.New(-w[1]), Bottom: layoutunit.New(-w[2]), Left: layoutunit.New(-w[3])}
}

// PixelSnappedContouredInnerBorder returns the pixel-snapped padding-box
// rrect: border_rect contracted by the used border widths. Mirrors
// ContouredBorderGeometry::PixelSnappedContouredInnerBorder
// (contoured_border_geometry.cc:207-217 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func PixelSnappedContouredInnerBorder(radii css.EllipticalRadii, borderRect geometry.PhysicalRect, borders geometry.PhysicalBoxStrut) FloatRoundedRect {
	return PixelSnappedContouredBorderWithOutsets(radii, borderRect, innerBorderOutsets(borderRect, borders))
}
