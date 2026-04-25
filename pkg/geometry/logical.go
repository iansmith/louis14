package geometry

import "louis14/pkg/geometry/layoutunit"

// LogicalOffset is a sub-pixel-precise position in writing-mode-relative
// (inline, block) coordinates. Mirrors Blink's LogicalOffset
// (core/layout/geometry/logical_offset.h).
type LogicalOffset struct {
	InlineOffset LayoutUnit
	BlockOffset  LayoutUnit
}

// NewLogicalOffset constructs from integer pixel values.
func NewLogicalOffset(inline, block int) LogicalOffset {
	return LogicalOffset{
		InlineOffset: layoutunit.New(inline),
		BlockOffset:  layoutunit.New(block),
	}
}

// LogicalOffsetFromF64Round constructs from float64 with round-to-nearest.
func LogicalOffsetFromF64Round(inline, block float64) LogicalOffset {
	return LogicalOffset{
		InlineOffset: layoutunit.FromFloat64Round(inline),
		BlockOffset:  layoutunit.FromFloat64Round(block),
	}
}

// LogicalOffsetFromF64Ceil constructs from float64 with ceil rounding.
func LogicalOffsetFromF64Ceil(inline, block float64) LogicalOffset {
	return LogicalOffset{
		InlineOffset: layoutunit.FromFloat64Ceil(inline),
		BlockOffset:  layoutunit.FromFloat64Ceil(block),
	}
}

// LogicalOffsetFromF64Floor constructs from float64 with floor rounding.
func LogicalOffsetFromF64Floor(inline, block float64) LogicalOffset {
	return LogicalOffset{
		InlineOffset: layoutunit.FromFloat64Floor(inline),
		BlockOffset:  layoutunit.FromFloat64Floor(block),
	}
}

// InlineF64 / BlockF64 are lossless exit accessors to float64.
func (o LogicalOffset) InlineF64() float64 { return o.InlineOffset.Float64() }
func (o LogicalOffset) BlockF64() float64  { return o.BlockOffset.Float64() }

// Add / Sub are componentwise.
func (o LogicalOffset) Add(b LogicalOffset) LogicalOffset {
	return LogicalOffset{
		InlineOffset: o.InlineOffset.Add(b.InlineOffset),
		BlockOffset:  o.BlockOffset.Add(b.BlockOffset),
	}
}

func (o LogicalOffset) Sub(b LogicalOffset) LogicalOffset {
	return LogicalOffset{
		InlineOffset: o.InlineOffset.Sub(b.InlineOffset),
		BlockOffset:  o.BlockOffset.Sub(b.BlockOffset),
	}
}

// LogicalSize is a sub-pixel-precise extent in (inline, block) directions.
// Mirrors Blink's LogicalSize.
type LogicalSize struct {
	InlineSize LayoutUnit
	BlockSize  LayoutUnit
}

// NewLogicalSize constructs from integer pixel values.
func NewLogicalSize(inline, block int) LogicalSize {
	return LogicalSize{
		InlineSize: layoutunit.New(inline),
		BlockSize:  layoutunit.New(block),
	}
}

// LogicalSizeFromF64Round / Ceil / Floor — explicit-rounding entry from float64.
func LogicalSizeFromF64Round(inline, block float64) LogicalSize {
	return LogicalSize{
		InlineSize: layoutunit.FromFloat64Round(inline),
		BlockSize:  layoutunit.FromFloat64Round(block),
	}
}

func LogicalSizeFromF64Ceil(inline, block float64) LogicalSize {
	return LogicalSize{
		InlineSize: layoutunit.FromFloat64Ceil(inline),
		BlockSize:  layoutunit.FromFloat64Ceil(block),
	}
}

func LogicalSizeFromF64Floor(inline, block float64) LogicalSize {
	return LogicalSize{
		InlineSize: layoutunit.FromFloat64Floor(inline),
		BlockSize:  layoutunit.FromFloat64Floor(block),
	}
}

func (s LogicalSize) InlineF64() float64 { return s.InlineSize.Float64() }
func (s LogicalSize) BlockF64() float64  { return s.BlockSize.Float64() }

// LogicalRect is an offset + size in logical coordinates.
type LogicalRect struct {
	Offset LogicalOffset
	Size   LogicalSize
}

// NewLogicalRect constructs from integer pixel values.
func NewLogicalRect(inlineOffset, blockOffset, inlineSize, blockSize int) LogicalRect {
	return LogicalRect{
		Offset: NewLogicalOffset(inlineOffset, blockOffset),
		Size:   NewLogicalSize(inlineSize, blockSize),
	}
}

// InlineEnd returns inline-start + inline-size.
func (r LogicalRect) InlineEnd() LayoutUnit {
	return r.Offset.InlineOffset.Add(r.Size.InlineSize)
}

// BlockEnd returns block-start + block-size.
func (r LogicalRect) BlockEnd() LayoutUnit {
	return r.Offset.BlockOffset.Add(r.Size.BlockSize)
}
