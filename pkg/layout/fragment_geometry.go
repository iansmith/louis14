package layout

import "louis14/pkg/css"

// FragmentGeometry holds the resolved box model edges for an element.
// Computed once before the layout algorithm runs.
//
// Ported from Blink's FragmentGeometry.
type FragmentGeometry struct {
	Border  LogicalEdges
	Padding LogicalEdges
}

// BorderBoxPadding returns the total border + padding on each logical side.
func (fg FragmentGeometry) BorderBoxPadding() LogicalEdges {
	return LogicalEdges{
		InlineStart: fg.Border.InlineStart + fg.Padding.InlineStart,
		InlineEnd:   fg.Border.InlineEnd + fg.Padding.InlineEnd,
		BlockStart:  fg.Border.BlockStart + fg.Padding.BlockStart,
		BlockEnd:    fg.Border.BlockEnd + fg.Padding.BlockEnd,
	}
}

// InlineBorderPadding returns the total inline-direction border + padding.
func (fg FragmentGeometry) InlineBorderPadding() float64 {
	return fg.Border.InlineStart + fg.Border.InlineEnd +
		fg.Padding.InlineStart + fg.Padding.InlineEnd
}

// BlockBorderPadding returns the total block-direction border + padding.
func (fg FragmentGeometry) BlockBorderPadding() float64 {
	return fg.Border.BlockStart + fg.Border.BlockEnd +
		fg.Padding.BlockStart + fg.Padding.BlockEnd
}

// ComputeFragmentGeometry resolves border and padding from a CSS style
// into logical edges for the given writing direction.
func ComputeFragmentGeometry(style *css.Style, wdm WritingDirectionMode) FragmentGeometry {
	if style == nil {
		return FragmentGeometry{}
	}

	physBorder := style.GetBorderWidth()
	physPadding := style.GetPadding()

	return FragmentGeometry{
		Border: ToLogicalEdges(PhysicalEdges{
			Top:    physBorder.Top,
			Right:  physBorder.Right,
			Bottom: physBorder.Bottom,
			Left:   physBorder.Left,
		}, wdm),
		Padding: ToLogicalEdges(PhysicalEdges{
			Top:    physPadding.Top,
			Right:  physPadding.Right,
			Bottom: physPadding.Bottom,
			Left:   physPadding.Left,
		}, wdm),
	}
}

// ResolveMargins resolves CSS margins into logical edges.
// containingBlockInlineSize is needed for percentage margin resolution
// (CSS 2.1 §8.3: percentages resolve against containing block's inline-size).
func ResolveMargins(style *css.Style, wdm WritingDirectionMode, containingBlockInlineSize float64) LogicalEdges {
	if style == nil {
		return LogicalEdges{}
	}

	physMargin := style.GetAllMarginsForWidth(containingBlockInlineSize)

	return ToLogicalEdges(PhysicalEdges{
		Top:    physMargin.Top,
		Right:  physMargin.Right,
		Bottom: physMargin.Bottom,
		Left:   physMargin.Left,
	}, wdm)
}

// ResolveInlineSize resolves the element's inline-size from CSS.
// Returns the content inline-size and whether it was explicitly set.
// If not explicit (auto), the caller should use the available inline-size.
func ResolveInlineSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (float64, bool) {
	if style == nil {
		return 0, false
	}

	// Determine which CSS property controls inline-size.
	prop := "width"
	if wdm.IsVertical() {
		prop = "height"
	}

	// Check for explicit length.
	if v, ok := style.GetLength(prop); ok {
		result := v
		if style.GetBoxSizing() == "border-box" {
			result -= geom.InlineBorderPadding()
			if result < 0 {
				result = 0
			}
		}
		return result, true
	}

	// Check for percentage.
	if pct, ok := style.GetPercentage(prop); ok {
		result := space.PercentageResolutionSize.InlineSize * pct / 100
		if style.GetBoxSizing() == "border-box" {
			result -= geom.InlineBorderPadding()
			if result < 0 {
				result = 0
			}
		}
		return result, true
	}

	return 0, false
}

// ResolveBlockSize resolves the element's block-size from CSS.
// Returns the content block-size and whether it was explicitly set.
func ResolveBlockSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (float64, bool) {
	if style == nil {
		return 0, false
	}

	prop := "height"
	if wdm.IsVertical() {
		prop = "width"
	}

	if v, ok := style.GetLength(prop); ok {
		result := v
		if style.GetBoxSizing() == "border-box" {
			result -= geom.BlockBorderPadding()
			if result < 0 {
				result = 0
			}
		}
		return result, true
	}

	if pct, ok := style.GetPercentage(prop); ok {
		if !space.IsBlockSizeIndefinite() {
			result := space.PercentageResolutionSize.BlockSize * pct / 100
			if style.GetBoxSizing() == "border-box" {
				result -= geom.BlockBorderPadding()
				if result < 0 {
					result = 0
				}
			}
			return result, true
		}
		// Percentage against indefinite → auto.
		return 0, false
	}

	return 0, false
}
