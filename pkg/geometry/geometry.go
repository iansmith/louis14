// Package geometry implements LayoutUnit-based logical and physical
// coordinate types and writing-mode conversions.
//
// This is the louis14 port of Blink's core/layout/geometry/* — the layered
// design built on top of pkg/geometry/layoutunit's scalar LayoutUnit type.
//
// Two coordinate systems:
//
//   - **Physical** (top-left origin, x grows right, y grows down) — what
//     the painter and the screen ultimately see. PhysicalOffset, PhysicalSize,
//     PhysicalRect.
//   - **Logical** (writing-mode-relative: inline grows in the inline axis,
//     block grows in the block axis) — what layout algorithms reason about.
//     LogicalOffset, LogicalSize, LogicalRect.
//
// Conversion between them requires knowing both the container's outer
// physical size and the box's inner physical size (because flipped writing
// modes anchor the logical origin at a different physical corner). The
// WritingModeConverter struct + the per-type ConvertToPhysical /
// ConvertToLogical methods do this.
//
// Reference: third_party/blink/renderer/core/layout/geometry/{logical_offset,
// logical_size,physical_offset,physical_size,physical_rect,
// writing_mode_converter}.{h,cc}.
package geometry

import "louis14/pkg/geometry/layoutunit"

// LayoutUnit is re-exported from pkg/geometry/layoutunit so callers can use
// the geometry package as a one-stop shop for fixed-point geometry without
// taking two imports for trivial cases.
type LayoutUnit = layoutunit.LayoutUnit
