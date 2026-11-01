package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// clipRoundedInnerRect intersects the clip with clipRect, a rrect inside
// the border-box rect. A renderable clipRect clips directly. A
// non-renderable one (inset radii that overlap) keeps its unconstrained
// radii and clips corner by corner instead: each corner's radius becomes a
// single-corner rrect reaching to rect's far edges, opposite corners
// clipped together. Mirrors Blink's RoundedInnerRectClipper
// (core/paint/rounded_inner_rect_clipper.cc:14-66 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The caller owns the
// Push/Pop around it (the clipper's Save / Restore).
func (r *Renderer) clipRoundedInnerRect(rect geometry.RectF, clipRect FloatRoundedRect) {
	if clipRect.IsRenderable() {
		clipRect.addPath(r.dc)
		r.dc.Clip()
		return
	}
	c, rd := clipRect.Rect, clipRect.Radii
	corner := func(x, y, right, bottom float64, idx int) {
		var radii css.EllipticalRadii
		radii[idx] = rd[idx]
		FloatRoundedRect{Rect: geometry.NewRectF(x, y, right-x, bottom-y), Radii: radii}.addPath(r.dc)
		r.dc.Clip()
	}
	if !isEmptyRadius(rd[0]) || !isEmptyRadius(rd[2]) {
		corner(c.X(), c.Y(), rect.Right(), rect.Bottom(), 0)
		corner(rect.X(), rect.Y(), c.Right(), c.Bottom(), 2)
	}
	if !isEmptyRadius(rd[1]) || !isEmptyRadius(rd[3]) {
		corner(rect.X(), c.Y(), c.Right(), rect.Bottom(), 1)
		corner(c.X(), rect.Y(), rect.Right(), c.Bottom(), 3)
	}
}

// isEmptyRadius mirrors gfx::SizeF::IsEmpty: true when either radius is
// zero.
func isEmptyRadius(r css.EllipticalRadius) bool { return r.Rx == 0 || r.Ry == 0 }
