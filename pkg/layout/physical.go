package layout

import "louis14/pkg/geometry"

// PhysicalSize represents a width and height in physical screen coordinates.
type PhysicalSize struct {
	Width  float64
	Height float64
}

// geomSizeToOld bridges geometry.PhysicalSize to pkg/layout.PhysicalSize.
// Used during the Phase 13c migration to feed converter / writing-mode
// helpers that still take the float64-pair pkg/layout.PhysicalSize.
// Goes away in 13d when those helpers migrate.
func geomSizeToOld(s geometry.PhysicalSize) PhysicalSize {
	return PhysicalSize{Width: s.WidthF64(), Height: s.HeightF64()}
}

// oldSizeToGeom bridges pkg/layout.PhysicalSize to geometry.PhysicalSize via
// FromFloat64Round. Same migration purpose as geomSizeToOld.
func oldSizeToGeom(s PhysicalSize) geometry.PhysicalSize {
	return geometry.PhysicalSizeFromF64Round(s.Width, s.Height)
}

// geomLogicalToOld bridges geometry.LogicalSize to pkg/layout.LogicalSize.
// Used during the Phase 13d migration to feed legacy float64-based logical-side
// algorithms (margin resolvers, fragment-builder LogicalSize accumulators)
// from the new LayoutUnit-backed ConstraintSpace fields. Goes away as those
// consumers migrate.
func geomLogicalToOld(s geometry.LogicalSize) LogicalSize {
	return LogicalSize{InlineSize: s.InlineF64(), BlockSize: s.BlockF64()}
}

// oldLogicalToGeom bridges pkg/layout.LogicalSize to geometry.LogicalSize via
// FromFloat64Round on each axis. Same migration purpose as geomLogicalToOld.
func oldLogicalToGeom(s LogicalSize) geometry.LogicalSize {
	return geometry.LogicalSizeFromF64Round(s.InlineSize, s.BlockSize)
}

// PhysicalOffset represents an (x, y) position in physical screen coordinates,
// measured from the top-left corner.
type PhysicalOffset struct {
	X float64
	Y float64
}

// PhysicalRect represents a rectangle in physical screen coordinates.
type PhysicalRect struct {
	Offset PhysicalOffset
	Size   PhysicalSize
}

// PhysicalEdges represents four physical edges (top, right, bottom, left).
// Used for margins, borders, and padding in physical coordinates.
type PhysicalEdges struct {
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
}
