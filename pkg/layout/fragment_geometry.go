package layout

import "louis14/pkg/css"

// FragmentGeometry holds the resolved box model edges for an element.
// Computed once before the layout algorithm runs.
//
// Ported from Blink's FragmentGeometry.
type FragmentGeometry struct {
	Border  LogicalEdges
	Padding LogicalEdges

	// BorderBoxSize is the resolved border-box size. InlineSize is always
	// definite. BlockSize is Indefinite when auto (layout determines it).
	// Populated by CalculateInitialFragmentGeometry; zero when computed via
	// the lightweight ComputeFragmentGeometry (which only resolves edges).
	BorderBoxSize LogicalSize
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
// An optional percentageBase (containing block inline-size) can be passed
// to resolve percentage padding values. If not provided (or zero), percentage
// padding resolves to zero.
func ComputeFragmentGeometry(style *css.Style, wdm WritingDirectionMode, percentageBase ...float64) FragmentGeometry {
	if style == nil {
		return FragmentGeometry{}
	}

	physBorder := style.GetBorderWidth()
	var physPadding css.BoxEdge
	if len(percentageBase) > 0 && percentageBase[0] > 0 {
		physPadding = style.GetPaddingForWidth(percentageBase[0])
	} else {
		physPadding = style.GetPadding()
	}

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

// MinMaxSizes holds the intrinsic min-content and max-content inline sizes
// for a layout node. Used for shrink-to-fit width computation.
//
// Mirrors Blink's MinMaxSizes (min_max_sizes.h).
type MinMaxSizes struct {
	MinContent float64 // smallest inline-size without overflow
	MaxContent float64 // largest inline-size without wrapping
}

// ShrinkToFit computes shrink-to-fit width per CSS 2.1 §10.3.5:
//
//	min(max(minContent, available), maxContent)
func (mm MinMaxSizes) ShrinkToFit(available float64) float64 {
	result := mm.MaxContent
	if result > available {
		result = available
	}
	if result < mm.MinContent {
		result = mm.MinContent
	}
	return result
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

// ResolveMinInlineSize resolves min-width/min-height as min-inline-size.
// Returns 0 if not set (CSS 2.1 §10.4: min-width defaults to 0).
func ResolveMinInlineSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) float64 {
	if style == nil {
		return 0
	}
	prop := "min-width"
	if wdm.IsVertical() {
		prop = "min-height"
	}
	if v, ok := style.GetLength(prop); ok {
		result := v
		if style.GetBoxSizing() == "border-box" {
			result -= geom.InlineBorderPadding()
			if result < 0 {
				result = 0
			}
		}
		return result
	}
	if pct, ok := style.GetPercentage(prop); ok {
		result := space.PercentageResolutionSize.InlineSize * pct / 100
		if style.GetBoxSizing() == "border-box" {
			result -= geom.InlineBorderPadding()
			if result < 0 {
				result = 0
			}
		}
		return result
	}
	return 0
}

// ResolveMaxInlineSize resolves max-width/max-height as max-inline-size.
// Returns (value, true) if set; (0, false) if "none" or not specified.
func ResolveMaxInlineSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (float64, bool) {
	if style == nil {
		return 0, false
	}
	prop := "max-width"
	if wdm.IsVertical() {
		prop = "max-height"
	}
	val, ok := style.Get(prop)
	if !ok || val == "none" || val == "" {
		return 0, false
	}
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

// ResolveMinBlockSize resolves min-height/min-width as min-block-size.
func ResolveMinBlockSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) float64 {
	if style == nil {
		return 0
	}
	prop := "min-height"
	if wdm.IsVertical() {
		prop = "min-width"
	}
	if v, ok := style.GetLength(prop); ok {
		result := v
		if style.GetBoxSizing() == "border-box" {
			result -= geom.BlockBorderPadding()
			if result < 0 {
				result = 0
			}
		}
		return result
	}
	if pct, ok := style.GetPercentage(prop); ok && !space.IsBlockSizeIndefinite() {
		result := space.PercentageResolutionSize.BlockSize * pct / 100
		if style.GetBoxSizing() == "border-box" {
			result -= geom.BlockBorderPadding()
			if result < 0 {
				result = 0
			}
		}
		return result
	}
	return 0
}

// ResolveMaxBlockSize resolves max-height/max-width as max-block-size.
// Returns (value, true) if set; (0, false) if "none" or not specified.
func ResolveMaxBlockSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (float64, bool) {
	if style == nil {
		return 0, false
	}
	prop := "max-height"
	if wdm.IsVertical() {
		prop = "max-width"
	}
	val, ok := style.Get(prop)
	if !ok || val == "none" || val == "" {
		return 0, false
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
	if pct, ok := style.GetPercentage(prop); ok && !space.IsBlockSizeIndefinite() {
		result := space.PercentageResolutionSize.BlockSize * pct / 100
		if style.GetBoxSizing() == "border-box" {
			result -= geom.BlockBorderPadding()
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

// CalculateInitialFragmentGeometry computes the full FragmentGeometry including
// the resolved border-box size. This mirrors Blink's CalculateInitialFragmentGeometry
// and is used by block, flex, and table layout algorithms to resolve their own
// inline-size and block-size before layout begins.
func CalculateInitialFragmentGeometry(
	ctx *LayoutContext,
	node *LayoutInputNode,
	style *css.Style,
	wdm WritingDirectionMode,
	space ConstraintSpace,
) FragmentGeometry {
	// CSS 2.1 §8.4: Percentage paddings resolve against containing block's
	// inline-size (width in horizontal mode). The PercentageResolutionSize's
	// InlineSize carries this value. Convert to physical width for the
	// percentage resolution base.
	pctBase := ToPhysicalSize(space.PercentageResolutionSize, space.WritingDirection.WM).Width
	geom := ComputeFragmentGeometry(style, wdm, pctBase)

	// --- Resolve inline-size (produces border-box) ---
	var borderBoxInline float64
	if space.IsFixedInlineSize {
		// Parent (e.g. flex) predetermined the size. AvailableSize is border-box.
		borderBoxInline = space.AvailableSize.InlineSize
	} else if explicitInline, ok := ResolveInlineSize(style, wdm, space, geom); ok {
		borderBoxInline = explicitInline + geom.InlineBorderPadding()
	} else if needsShrinkToFit(style) || space.IsOrthogonalWritingModeRoot {
		// CSS Writing Modes §7.3.1: orthogonal flows with auto inline-size
		// use shrink-to-fit, constrained by the available inline-size (which
		// is the ICB fallback for indefinite parents).
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = minMax.ShrinkToFit(available) + geom.InlineBorderPadding()
	} else if space.IsInsideFlexibleBox && !space.IsFixedInlineSize {
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = minMax.ShrinkToFit(available) + geom.InlineBorderPadding()
	} else {
		// Auto inline-size: fill available space.
		borderBoxInline = space.AvailableSize.InlineSize
	}

	// Apply min/max inline constraints (content-box comparison).
	contentInline := borderBoxInline - geom.InlineBorderPadding()
	if contentInline < 0 {
		contentInline = 0
	}
	minInline := ResolveMinInlineSize(style, wdm, space, geom)
	if contentInline < minInline {
		contentInline = minInline
	}
	if maxInline, ok := ResolveMaxInlineSize(style, wdm, space, geom); ok {
		if contentInline > maxInline {
			contentInline = maxInline
		}
	}
	borderBoxInline = contentInline + geom.InlineBorderPadding()

	// --- Resolve block-size (produces border-box, or Indefinite) ---
	// Order matches existing algorithms: explicit CSS first, then IsFixedBlockSize fallback.
	var borderBoxBlock float64 = Indefinite
	if explicitBlock, ok := ResolveBlockSize(style, wdm, space, geom); ok {
		borderBoxBlock = explicitBlock + geom.BlockBorderPadding()
	} else if space.IsFixedBlockSize && !space.IsFixedBlockSizeIndefinite {
		// Parent (e.g. flex) has fixed the block-size. Check for max-block-size keywords
		// that override the fixed constraint (e.g. max-height: min-content).
		maxProp := "max-height"
		if wdm.IsVertical() {
			maxProp = "max-width"
		}
		useFixed := true
		if v, ok := style.Get(maxProp); ok && v != "" && v != "none" {
			if _, resolved := ResolveMaxBlockSize(style, wdm, space, geom); !resolved {
				useFixed = false
			}
		}
		if useFixed {
			borderBoxBlock = space.AvailableSize.BlockSize
		}
	}

	// Apply min/max block constraints only when block-size is definite.
	if borderBoxBlock != Indefinite {
		contentBlock := borderBoxBlock - geom.BlockBorderPadding()
		if contentBlock < 0 {
			contentBlock = 0
		}
		minBlock := ResolveMinBlockSize(style, wdm, space, geom)
		if contentBlock < minBlock {
			contentBlock = minBlock
		}
		if maxBlock, ok := ResolveMaxBlockSize(style, wdm, space, geom); ok {
			if contentBlock > maxBlock {
				contentBlock = maxBlock
			}
		}
		borderBoxBlock = contentBlock + geom.BlockBorderPadding()
	}

	geom.BorderBoxSize = LogicalSize{InlineSize: borderBoxInline, BlockSize: borderBoxBlock}
	return geom
}
