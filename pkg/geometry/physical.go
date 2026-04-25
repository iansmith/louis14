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
