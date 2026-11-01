package layout

import (
	"louis14/pkg/css"
)

// Relative positioning (CSS Position 3 §3.4 "Relative positioning",
// CSS 2.1 §9.4.3). Ports Blink's core/layout/relative_utils.{h,cc}. As in
// Blink, the offset is computed by the PARENT when it places the child
// (BoxFragmentBuilder.AddChild, the line builder for inline-level content,
// grid and table rows/cells with their own containing block) and is baked
// into the child's link offset — there is no paint-time relative offset.

// ComputeRelativeOffset returns the relative offset of a box with
// childStyle, in the logical coordinates of its container
// (containerWDM), whose percentages resolve against availableSize (the
// container's child available size, logical in containerWDM). Zero unless
// childStyle is position:relative. Over-constrained insets resolve "start
// wins" in containerWDM. Mirrors Blink's ComputeRelativeOffset
// (relative_utils.cc:18-88 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeRelativeOffset(childStyle *css.Style, containerWDM WritingDirectionMode, availableSize LogicalSize) LogicalOffset {
	if childStyle == nil || childStyle.GetPosition() != css.PositionRelative {
		return LogicalOffset{}
	}

	physicalSize := ToPhysicalSize(availableSize, containerWDM.WM)

	// ResolveInset (relative_utils.cc:28-36): auto, or a percentage of an
	// indefinite size, is unset.
	insets := childStyle.GetPositionOffsetResolved(max(0, physicalSize.Width), max(0, physicalSize.Height))
	for _, inset := range []struct {
		property string
		size     float64
		has      *bool
	}{
		{"left", physicalSize.Width, &insets.HasLeft},
		{"right", physicalSize.Width, &insets.HasRight},
		{"top", physicalSize.Height, &insets.HasTop},
		{"bottom", physicalSize.Height, &insets.HasBottom},
	} {
		if *inset.has && inset.size == Indefinite && propertyHasPercent(childStyle, inset.property) {
			*inset.has = false
		}
	}

	// Common case optimization.
	if !insets.HasLeft && !insets.HasRight && !insets.HasTop && !insets.HasBottom {
		return LogicalOffset{}
	}

	// Conflict resolution rules: https://www.w3.org/TR/css-position-3/#rel-pos
	switch {
	case !insets.HasLeft && !insets.HasRight:
		insets.Left, insets.Right = 0, 0
	case !insets.HasLeft:
		insets.Left = -insets.Right
	case !insets.HasRight:
		insets.Right = -insets.Left
	}
	switch {
	case !insets.HasTop && !insets.HasBottom:
		insets.Top, insets.Bottom = 0, 0
	case !insets.HasTop:
		insets.Top = -insets.Bottom
	case !insets.HasBottom:
		insets.Bottom = -insets.Top
	}

	// The offset is the inline-start and block-start insets in
	// containerWDM (relative_utils.cc:70-87).
	logical := PhysicalInsetsToLogical(insets, containerWDM)
	return LogicalOffset{InlineOffset: logical.InlineStart, BlockOffset: logical.BlockStart}
}

// ComputeRelativeOffsetForBoxFragment returns the relative offset of a
// position:relative box fragment placed in a container with containerWDM
// and child available size availableSize. Mirrors Blink's
// ComputeRelativeOffsetForBoxFragment (relative_utils.cc:90-98 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeRelativeOffsetForBoxFragment(fragment *PhysicalFragment, containerWDM WritingDirectionMode, availableSize LogicalSize) LogicalOffset {
	return ComputeRelativeOffset(fragment.Style, containerWDM, availableSize)
}

// ComputeRelativeOffsetForInline returns the relative offset of an
// inline-level box with childStyle in line-logical coordinates. The
// conflict resolution works off the block container's writing direction
// (blockWDM) and available size, not the inline's own container. Mirrors
// Blink's ComputeRelativeOffsetForInline (relative_utils.cc:100-124 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): louis14 line boxes are built
// LTR in the block's writing mode (inline_layout.go lineWDM), so an RTL
// block reverses the inline offset. Blink's IsFlippedLines block-offset
// reversal has no louis14 analog: louis14 line boxes keep the block's
// block-flow direction rather than a line-over-to-line-under one.
func ComputeRelativeOffsetForInline(blockWDM WritingDirectionMode, availableSize LogicalSize, childStyle *css.Style) LogicalOffset {
	relativeOffset := ComputeRelativeOffset(childStyle, blockWDM, availableSize)
	if blockWDM.IsRTL() {
		relativeOffset.InlineOffset = -relativeOffset.InlineOffset
	}
	return relativeOffset
}
