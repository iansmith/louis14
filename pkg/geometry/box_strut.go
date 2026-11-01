package geometry

import (
	"image"

	"louis14/pkg/geometry/layoutunit"
)

// PhysicalBoxStrut stores a per-physical-edge LayoutUnit amount (margins,
// borders, padding, or outsets). Mirrors Blink's PhysicalBoxStrut
// (core/layout/geometry/box_strut.h:159 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type PhysicalBoxStrut struct {
	Top, Right, Bottom, Left LayoutUnit
}

// Offset returns the strut's top-left corner as an offset
// (PhysicalBoxStrut::Offset, box_strut.h @ a9f50e52).
func (s PhysicalBoxStrut) Offset() PhysicalOffset {
	return PhysicalOffset{Left: s.Left, Top: s.Top}
}

// IsZero reports whether every edge is zero (PhysicalBoxStrut::IsZero,
// box_strut.h @ a9f50e52).
func (s PhysicalBoxStrut) IsZero() bool {
	return s.Top.IsZero() && s.Right.IsZero() && s.Bottom.IsZero() && s.Left.IsZero()
}

// Add returns the edge-wise sum (PhysicalBoxStrut::operator+, box_strut.h
// @ a9f50e52).
func (s PhysicalBoxStrut) Add(o PhysicalBoxStrut) PhysicalBoxStrut {
	return PhysicalBoxStrut{
		Top:    s.Top.Add(o.Top),
		Right:  s.Right.Add(o.Right),
		Bottom: s.Bottom.Add(o.Bottom),
		Left:   s.Left.Add(o.Left),
	}
}

// Contract returns r shrunk inward by the strut. Mirrors Blink's
// PhysicalRect::Contract (core/layout/geometry/physical_rect.h:127 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r PhysicalRect) Contract(s PhysicalBoxStrut) PhysicalRect {
	z := layoutunit.Zero
	return r.ExpandEdges(z.Sub(s.Top), z.Sub(s.Right), z.Sub(s.Bottom), z.Sub(s.Left))
}

// ToPixelSnappedRect snaps r to device pixels: the offset rounded, each size
// snapped against its unsnapped offset. Mirrors Blink's ToPixelSnappedRect
// (core/layout/geometry/physical_rect.h:158-167, :228-230 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ToPixelSnappedRect(r PhysicalRect) image.Rectangle {
	x, y := int(r.Offset.Left.Round()), int(r.Offset.Top.Round())
	w := int(layoutunit.SnapSizeToPixel(r.Size.Width, r.Offset.Left))
	h := int(layoutunit.SnapSizeToPixel(r.Size.Height, r.Offset.Top))
	// gfx::Rect clamps a negative size to zero.
	return image.Rect(x, y, x+max(0, w), y+max(0, h))
}

// PhysicalRectFromRect converts an integer rect to a PhysicalRect (Blink's
// explicit PhysicalRect(const gfx::Rect&) constructor,
// core/layout/geometry/physical_rect.h @ a9f50e52).
func PhysicalRectFromRect(r image.Rectangle) PhysicalRect {
	return NewPhysicalRect(r.Min.X, r.Min.Y, r.Dx(), r.Dy())
}
