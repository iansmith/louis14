package geometry

import "louis14/pkg/geometry/layoutunit"

// PhysicalOffset is a sub-pixel-precise (x, y) position from the top-left
// corner of the document. Mirrors Blink's PhysicalOffset
// (core/layout/geometry/physical_offset.h).
type PhysicalOffset struct {
	Left LayoutUnit // physical x
	Top  LayoutUnit // physical y
}

// NewPhysicalOffset constructs from integer pixel values.
func NewPhysicalOffset(left, top int) PhysicalOffset {
	return PhysicalOffset{
		Left: layoutunit.New(left),
		Top:  layoutunit.New(top),
	}
}

// PhysicalOffsetFromF64Round / Ceil / Floor — explicit-rounding entry from float64.
func PhysicalOffsetFromF64Round(left, top float64) PhysicalOffset {
	return PhysicalOffset{
		Left: layoutunit.FromFloat64Round(left),
		Top:  layoutunit.FromFloat64Round(top),
	}
}

func PhysicalOffsetFromF64Ceil(left, top float64) PhysicalOffset {
	return PhysicalOffset{
		Left: layoutunit.FromFloat64Ceil(left),
		Top:  layoutunit.FromFloat64Ceil(top),
	}
}

func PhysicalOffsetFromF64Floor(left, top float64) PhysicalOffset {
	return PhysicalOffset{
		Left: layoutunit.FromFloat64Floor(left),
		Top:  layoutunit.FromFloat64Floor(top),
	}
}

func (o PhysicalOffset) LeftF64() float64 { return o.Left.Float64() }
func (o PhysicalOffset) TopF64() float64  { return o.Top.Float64() }

func (o PhysicalOffset) Add(b PhysicalOffset) PhysicalOffset {
	return PhysicalOffset{
		Left: o.Left.Add(b.Left),
		Top:  o.Top.Add(b.Top),
	}
}

func (o PhysicalOffset) Sub(b PhysicalOffset) PhysicalOffset {
	return PhysicalOffset{
		Left: o.Left.Sub(b.Left),
		Top:  o.Top.Sub(b.Top),
	}
}

// PhysicalSize is a sub-pixel-precise (width, height) extent.
type PhysicalSize struct {
	Width  LayoutUnit
	Height LayoutUnit
}

// NewPhysicalSize constructs from integer pixel values.
func NewPhysicalSize(width, height int) PhysicalSize {
	return PhysicalSize{
		Width:  layoutunit.New(width),
		Height: layoutunit.New(height),
	}
}

// PhysicalSizeFromF64Round / Ceil / Floor — explicit-rounding entry from float64.
func PhysicalSizeFromF64Round(width, height float64) PhysicalSize {
	return PhysicalSize{
		Width:  layoutunit.FromFloat64Round(width),
		Height: layoutunit.FromFloat64Round(height),
	}
}

func PhysicalSizeFromF64Ceil(width, height float64) PhysicalSize {
	return PhysicalSize{
		Width:  layoutunit.FromFloat64Ceil(width),
		Height: layoutunit.FromFloat64Ceil(height),
	}
}

func PhysicalSizeFromF64Floor(width, height float64) PhysicalSize {
	return PhysicalSize{
		Width:  layoutunit.FromFloat64Floor(width),
		Height: layoutunit.FromFloat64Floor(height),
	}
}

func (s PhysicalSize) WidthF64() float64  { return s.Width.Float64() }
func (s PhysicalSize) HeightF64() float64 { return s.Height.Float64() }

// PhysicalRect is an offset + size in physical coordinates.
type PhysicalRect struct {
	Offset PhysicalOffset
	Size   PhysicalSize
}

// NewPhysicalRect constructs from integer pixel values.
func NewPhysicalRect(left, top, width, height int) PhysicalRect {
	return PhysicalRect{
		Offset: NewPhysicalOffset(left, top),
		Size:   NewPhysicalSize(width, height),
	}
}

// Right returns left + width.
func (r PhysicalRect) Right() LayoutUnit { return r.Offset.Left.Add(r.Size.Width) }

// Bottom returns top + height.
func (r PhysicalRect) Bottom() LayoutUnit { return r.Offset.Top.Add(r.Size.Height) }

// IsEmpty reports whether the rect has a non-positive width or height.
// Mirrors Blink's PhysicalRect::IsEmpty (core/layout/geometry/
// physical_rect.h:45 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r PhysicalRect) IsEmpty() bool {
	return r.Size.Width.LessEqual(layoutunit.Zero) || r.Size.Height.LessEqual(layoutunit.Zero)
}

// UniteEvenIfEmpty returns the smallest rect containing both r and other,
// even when either is empty. Mirrors Blink's PhysicalRect::UniteEvenIfEmpty
// (core/layout/geometry/physical_rect.cc:81-95 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), including its offset
// derivation from the far edges.
func (r PhysicalRect) UniteEvenIfEmpty(other PhysicalRect) PhysicalRect {
	left := layoutunit.Min(r.Offset.Left, other.Offset.Left)
	top := layoutunit.Min(r.Offset.Top, other.Offset.Top)
	right := layoutunit.Max(r.Right(), other.Right())
	bottom := layoutunit.Max(r.Bottom(), other.Bottom())
	size := PhysicalSize{Width: right.Sub(left), Height: bottom.Sub(top)}
	return PhysicalRect{
		Offset: PhysicalOffset{Left: right.Sub(size.Width), Top: bottom.Sub(size.Height)},
		Size:   size,
	}
}

// Intersect returns the intersection of r and other, or the zero rect when
// they do not intersect. Mirrors Blink's PhysicalRect::Intersect
// (core/layout/geometry/physical_rect.cc:116-131 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r PhysicalRect) Intersect(other PhysicalRect) PhysicalRect {
	left := layoutunit.Max(r.Offset.Left, other.Offset.Left)
	top := layoutunit.Max(r.Offset.Top, other.Offset.Top)
	right := layoutunit.Min(r.Right(), other.Right())
	bottom := layoutunit.Min(r.Bottom(), other.Bottom())
	if right.LessEqual(left) || bottom.LessEqual(top) {
		return PhysicalRect{}
	}
	return PhysicalRect{
		Offset: PhysicalOffset{Left: left, Top: top},
		Size:   PhysicalSize{Width: right.Sub(left), Height: bottom.Sub(top)},
	}
}

// ExpandEdges returns r grown outward by the given per-side amounts
// (negative amounts shrink it). Mirrors Blink's PhysicalRect::ExpandEdges
// (core/layout/geometry/physical_rect.h:115-123 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r PhysicalRect) ExpandEdges(top, right, bottom, left LayoutUnit) PhysicalRect {
	return PhysicalRect{
		Offset: PhysicalOffset{Left: r.Offset.Left.Sub(left), Top: r.Offset.Top.Sub(top)},
		Size: PhysicalSize{
			Width:  r.Size.Width.Add(left).Add(right),
			Height: r.Size.Height.Add(top).Add(bottom),
		},
	}
}

// PhysicalRectEnclosingRect returns the smallest LayoutUnit rect that
// encloses rect: its origin floored, its far edges ceiled. Mirrors Blink's
// static PhysicalRect::EnclosingRect (core/layout/geometry/
// physical_rect.h:177-183 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func PhysicalRectEnclosingRect(rect RectF) PhysicalRect {
	offset := PhysicalOffset{
		Left: layoutunit.FromFloat64Floor(rect.X()),
		Top:  layoutunit.FromFloat64Floor(rect.Y()),
	}
	return PhysicalRect{
		Offset: offset,
		Size: PhysicalSize{
			Width:  layoutunit.FromFloat64Ceil(rect.Right()).Sub(offset.Left),
			Height: layoutunit.FromFloat64Ceil(rect.Bottom()).Sub(offset.Top),
		},
	}
}

// ToRectF converts r to a float rect. Mirrors Blink's explicit
// PhysicalRect → gfx::RectF conversion (core/layout/geometry/
// physical_rect.h:173-175 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r PhysicalRect) ToRectF() RectF {
	return NewRectF(r.Offset.Left.Float64(), r.Offset.Top.Float64(), r.Size.Width.Float64(), r.Size.Height.Float64())
}
