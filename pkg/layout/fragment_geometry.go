package layout

import "louis14/pkg/css"

// FragmentGeometry holds the resolved box model edges for an element.
// Computed once before the layout algorithm runs.
//
// Ported from Blink's FragmentGeometry.
type FragmentGeometry struct {
	Border    LogicalEdges
	Scrollbar LogicalEdges // classic scrollbar reservation per CSS Overflow §3.3
	Padding   LogicalEdges

	// BorderBoxSize is the resolved border-box size. InlineSize is always
	// definite. BlockSize is Indefinite when auto (layout determines it).
	// Populated by CalculateInitialFragmentGeometry; zero when computed via
	// the lightweight ComputeFragmentGeometry (which only resolves edges).
	BorderBoxSize LogicalSize
}

// BorderBoxPadding returns the total border + scrollbar + padding on each
// logical side. This is the inset from border-box to content-box per
// CSS Overflow §3.3 (scrollbars sit between the border and padding edges).
// Matches Blink's BorderScrollbarPadding().
func (fg FragmentGeometry) BorderBoxPadding() LogicalEdges {
	return LogicalEdges{
		InlineStart: fg.Border.InlineStart + fg.Scrollbar.InlineStart + fg.Padding.InlineStart,
		InlineEnd:   fg.Border.InlineEnd + fg.Scrollbar.InlineEnd + fg.Padding.InlineEnd,
		BlockStart:  fg.Border.BlockStart + fg.Scrollbar.BlockStart + fg.Padding.BlockStart,
		BlockEnd:    fg.Border.BlockEnd + fg.Scrollbar.BlockEnd + fg.Padding.BlockEnd,
	}
}

// InlineBorderPadding returns the total inline-direction border + scrollbar
// + padding (i.e., the inset from border-box to content-box on the inline
// axis). Matches Blink's BorderScrollbarPadding().InlineSum().
func (fg FragmentGeometry) InlineBorderPadding() float64 {
	return fg.Border.InlineStart + fg.Border.InlineEnd +
		fg.Scrollbar.InlineStart + fg.Scrollbar.InlineEnd +
		fg.Padding.InlineStart + fg.Padding.InlineEnd
}

// BlockBorderPadding returns the total block-direction border + scrollbar
// + padding (i.e., the inset from border-box to content-box on the block
// axis). Matches Blink's BorderScrollbarPadding().BlockSum().
func (fg FragmentGeometry) BlockBorderPadding() float64 {
	return fg.Border.BlockStart + fg.Border.BlockEnd +
		fg.Scrollbar.BlockStart + fg.Scrollbar.BlockEnd +
		fg.Padding.BlockStart + fg.Padding.BlockEnd
}

// InlineScrollbarSum returns the inline-direction scrollbar reservation.
func (fg FragmentGeometry) InlineScrollbarSum() float64 {
	return fg.Scrollbar.InlineStart + fg.Scrollbar.InlineEnd
}

// BlockScrollbarSum returns the block-direction scrollbar reservation.
func (fg FragmentGeometry) BlockScrollbarSum() float64 {
	return fg.Scrollbar.BlockStart + fg.Scrollbar.BlockEnd
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
		Scrollbar: ComputeScrollbarLogicalEdges(style, wdm),
		Padding: ToLogicalEdges(PhysicalEdges{
			Top:    physPadding.Top,
			Right:  physPadding.Right,
			Bottom: physPadding.Bottom,
			Left:   physPadding.Left,
		}, wdm),
	}
}

// ComputeScrollbarLogicalEdges returns the classic-scrollbar reservation as
// logical edges for the given element's writing-direction. Per CSS Overflow §3
// scrollbars sit between the inner border edge and the outer padding edge.
//
// The horizontal scrollbar (overflow-x) is always placed on the physical
// bottom edge; the vertical scrollbar (overflow-y) is placed on the physical
// right edge by default, or on the physical left edge when the vertical
// scrollbar belongs on the left per Blink's ShouldPlaceVerticalScrollbarOnLeft
// (RTL horizontal-tb, or vertical-rl).
func ComputeScrollbarLogicalEdges(style *css.Style, wdm WritingDirectionMode) LogicalEdges {
	if style == nil {
		return LogicalEdges{}
	}
	width := classicScrollbarWidth(style)
	if width == 0 {
		return LogicalEdges{}
	}
	var phys PhysicalEdges
	if reservesClassicScrollbar(style.GetOverflowX()) {
		phys.Bottom = width
	}
	if reservesClassicScrollbar(style.GetOverflowY()) {
		if placeVerticalScrollbarOnLeft(wdm) {
			phys.Left = width
		} else {
			phys.Right = width
		}
	}
	return ToLogicalEdges(phys, wdm)
}

// reservesClassicScrollbar reports whether the overflow value reserves
// a classic-scrollbar gutter. Per CSS Overflow, only "scroll" unconditionally
// reserves; "auto" reserves only when content actually overflows (which we
// cannot determine before layout and therefore treat as non-reserving here,
// matching Blink's pre-layout assumption for non-auto-reserved gutters).
func reservesClassicScrollbar(overflow css.OverflowType) bool {
	return overflow == css.OverflowScroll
}

// classicScrollbarWidth returns the per-edge width of the classic scrollbar.
// WPT reftests use a fixed 15px (matching kScrollbarThicknessForWebTests in
// scrollbar_theme_aura.cc); "thin" uses 10px; "none" disables the gutter.
func classicScrollbarWidth(style *css.Style) float64 {
	switch style.GetScrollbarWidth() {
	case "none":
		return 0
	case "thin":
		return 10
	default: // "auto" (and any unknown value)
		return 15
	}
}

// placeVerticalScrollbarOnLeft mirrors Blink's
// LayoutBox::ShouldPlaceVerticalScrollbarOnLeft. Vertical scrollbars go on the
// physical-left edge for RTL horizontal-tb and for vertical-rl writing modes.
func placeVerticalScrollbarOnLeft(wdm WritingDirectionMode) bool {
	if wdm.IsFlippedBlocks() {
		return true
	}
	if !wdm.IsVertical() && wdm.IsRTL() {
		return true
	}
	return false
}

// IsIntrinsicKeyword returns true if val is a CSS intrinsic sizing keyword:
// min-content, max-content, fit-content, -webkit-min-content, -webkit-max-content,
// -webkit-fill-available, -moz-min-content, -moz-max-content, -moz-fit-content.
func IsIntrinsicKeyword(val string) bool {
	switch val {
	case "min-content", "max-content", "fit-content",
		"-webkit-min-content", "-webkit-max-content", "-webkit-fill-available",
		"-moz-min-content", "-moz-max-content", "-moz-fit-content":
		return true
	}
	return false
}

// ResolveIntrinsicInlineSize resolves an intrinsic sizing keyword for inline-size
// given pre-computed MinMaxSizes. Returns the content-box inline-size.
func ResolveIntrinsicInlineSize(keyword string, minMax MinMaxSizes, available float64) float64 {
	switch keyword {
	case "min-content", "-webkit-min-content", "-moz-min-content":
		return minMax.MinContent
	case "max-content", "-webkit-max-content", "-moz-max-content":
		return minMax.MaxContent
	case "fit-content", "-moz-fit-content", "-webkit-fill-available":
		return minMax.ShrinkToFit(available)
	}
	return 0
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

// applyBoxSizingInline converts a user-declared inline size into a content-box
// inline size, honoring box-sizing and scrollbar reservation. Per CSS Overflow
// §3.3 the classic scrollbar reduces the content area without enlarging the
// border-box, so for content-box sizing the scrollbar reservation steals from
// the declared content (matching Blink).
func applyBoxSizingInline(style *css.Style, geom FragmentGeometry, declared float64) float64 {
	if style.GetBoxSizing() == "border-box" {
		declared -= geom.InlineBorderPadding()
	} else {
		declared -= geom.InlineScrollbarSum()
	}
	if declared < 0 {
		declared = 0
	}
	return declared
}

// applyBoxSizingBlock is the block-direction counterpart of applyBoxSizingInline.
func applyBoxSizingBlock(style *css.Style, geom FragmentGeometry, declared float64) float64 {
	if style.GetBoxSizing() == "border-box" {
		declared -= geom.BlockBorderPadding()
	} else {
		declared -= geom.BlockScrollbarSum()
	}
	if declared < 0 {
		declared = 0
	}
	return declared
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

	val, ok := style.Get(prop)
	if !ok || val == "" || val == "auto" {
		return 0, false
	}

	// CSS calc() with percentage terms: resolve against containing block's
	// inline-size. GetLength alone resolves percentages with base=0, so
	// calc(52px + 100% + 52px) would lose the percentage term.
	if css.IsCalcWithPercent(val) {
		result, calcOK := css.EvalCalcWithPercent(
			val[5:len(val)-1], // strip "calc(" and ")"
			style.GetFontSize(),
			space.PercentageResolutionSize.InlineSize,
		)
		if calcOK {
			return applyBoxSizingInline(style, geom, result), true
		}
	}

	// Check for explicit length (handles calc without percentages, px, em, etc.).
	if v, ok := style.GetLength(prop); ok {
		return applyBoxSizingInline(style, geom, v), true
	}

	// Check for percentage.
	if pct, ok := style.GetPercentage(prop); ok {
		result := space.PercentageResolutionSize.InlineSize * pct / 100
		return applyBoxSizingInline(style, geom, result), true
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
		return applyBoxSizingInline(style, geom, v)
	}
	if pct, ok := style.GetPercentage(prop); ok {
		result := space.PercentageResolutionSize.InlineSize * pct / 100
		return applyBoxSizingInline(style, geom, result)
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
		return applyBoxSizingInline(style, geom, v), true
	}
	if pct, ok := style.GetPercentage(prop); ok {
		result := space.PercentageResolutionSize.InlineSize * pct / 100
		return applyBoxSizingInline(style, geom, result), true
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
		return applyBoxSizingBlock(style, geom, v)
	}
	if pct, ok := style.GetPercentage(prop); ok && !space.IsBlockSizeIndefinite() {
		result := space.PercentageResolutionSize.BlockSize * pct / 100
		return applyBoxSizingBlock(style, geom, result)
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
		return applyBoxSizingBlock(style, geom, v), true
	}
	if pct, ok := style.GetPercentage(prop); ok && !space.IsBlockSizeIndefinite() {
		result := space.PercentageResolutionSize.BlockSize * pct / 100
		return applyBoxSizingBlock(style, geom, result), true
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

	val, _ := style.Get(prop)

	// CSS calc() with percentage terms: resolve against containing block's
	// block-size. GetLength alone resolves percentages with base=0, so
	// calc(100% - 16px) would lose the percentage term.
	if css.IsCalcWithPercent(val) && !space.IsBlockSizeIndefinite() {
		result, calcOK := css.EvalCalcWithPercent(
			val[5:len(val)-1], // strip "calc(" and ")"
			style.GetFontSize(),
			space.PercentageResolutionSize.BlockSize,
		)
		if calcOK {
			return applyBoxSizingBlock(style, geom, result), true
		}
	}

	if v, ok := style.GetLength(prop); ok {
		return applyBoxSizingBlock(style, geom, v), true
	}

	if pct, ok := style.GetPercentage(prop); ok {
		if !space.IsBlockSizeIndefinite() {
			result := space.PercentageResolutionSize.BlockSize * pct / 100
			return applyBoxSizingBlock(style, geom, result), true
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
	// CSS Writing Modes 3 §7.2 + CSS 2.1 §8.3/§8.4: Percentage paddings
	// resolve against the containing block's inline-size. Use the dedicated
	// PercentageResolutionInlineSize which is never axis-swapped for
	// orthogonal children (unlike PercentageResolutionSize which gets
	// swapped and may put the CB's inline-size into the BlockSize field).
	pctBase := space.PercentageResolutionInlineSize
	geom := ComputeFragmentGeometry(style, wdm, pctBase)

	// Lazy cache for ComputeMinMaxSizes — computed at most once per element.
	var minMaxCache *MinMaxSizes

	// --- Resolve inline-size (produces border-box) ---
	var borderBoxInline float64
	inlineSizeIsAuto := false // true if inline-size was not explicitly set

	// Check for intrinsic sizing keywords (min-content, max-content, fit-content)
	// before the normal resolution path. These keywords are not lengths/percentages
	// so ResolveInlineSize returns (0, false) for them — we handle them here where
	// ComputeMinMaxSizes is available.
	inlineProp := "width"
	if wdm.IsVertical() {
		inlineProp = "height"
	}
	inlineVal, _ := style.Get(inlineProp)

	if space.IsFixedInlineSize {
		// Parent (e.g. flex) predetermined the size. AvailableSize is border-box.
		borderBoxInline = space.AvailableSize.InlineSize
	} else if IsIntrinsicKeyword(inlineVal) {
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = ResolveIntrinsicInlineSize(inlineVal, minMax, available) + geom.InlineBorderPadding()
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
		inlineSizeIsAuto = true
	} else if space.IsInsideFlexibleBox && !space.IsFixedInlineSize {
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = minMax.ShrinkToFit(available) + geom.InlineBorderPadding()
		inlineSizeIsAuto = true
	} else {
		// Auto inline-size: fill available space.
		borderBoxInline = space.AvailableSize.InlineSize
		inlineSizeIsAuto = true
	}

	// Apply min/max inline constraints (content-box comparison).
	// CSS 2.1 §10.4: Apply max first, then min. This ensures min wins when min > max.
	contentInline := borderBoxInline - geom.InlineBorderPadding()
	if contentInline < 0 {
		contentInline = 0
	}

	// Resolve max-inline-size first (so min can override it).
	maxInlineProp := "max-width"
	if wdm.IsVertical() {
		maxInlineProp = "max-height"
	}
	if maxInlineVal, ok := style.Get(maxInlineProp); ok && IsIntrinsicKeyword(maxInlineVal) {
		minMax := computeMinMaxOnce(ctx, node, space, &minMaxCache)
		available := space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		maxInline := ResolveIntrinsicInlineSize(maxInlineVal, minMax, available)
		if contentInline > maxInline {
			contentInline = maxInline
		}
	} else if maxInline, ok := ResolveMaxInlineSize(style, wdm, space, geom); ok {
		if contentInline > maxInline {
			contentInline = maxInline
		}
	}

	// Resolve min-inline-size second (min wins over max per CSS 2.1 §10.4).
	minInlineProp := "min-width"
	if wdm.IsVertical() {
		minInlineProp = "min-height"
	}
	minInline := ResolveMinInlineSize(style, wdm, space, geom)
	if minInlineVal, ok := style.Get(minInlineProp); ok && IsIntrinsicKeyword(minInlineVal) {
		minMax := computeMinMaxOnce(ctx, node, space, &minMaxCache)
		available := space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		minInline = ResolveIntrinsicInlineSize(minInlineVal, minMax, available)
	}
	if contentInline < minInline {
		contentInline = minInline
	}
	borderBoxInline = contentInline + geom.InlineBorderPadding()

	// --- Resolve block-size (produces border-box, or Indefinite) ---
	// Order matches existing algorithms: explicit CSS first, then IsFixedBlockSize fallback.
	//
	// Intrinsic keywords (min-content, max-content, fit-content) on block-size
	// effectively mean "auto" for block-level layout — the element sizes to its
	// content. We detect them here and leave borderBoxBlock = Indefinite so that
	// the layout algorithm determines the block-size from content.
	blockProp := "height"
	if wdm.IsVertical() {
		blockProp = "width"
	}
	blockVal, _ := style.Get(blockProp)

	var borderBoxBlock float64 = Indefinite
	if IsIntrinsicKeyword(blockVal) {
		// Treat as auto — layout will determine block-size from content.
	} else if space.IsContentSuggestionLayout {
		// §4.5 content suggestion: ignore the element's own explicit CSS
		// block-size — only content determines the size.
	} else if space.IsBlockSizeOverride && space.IsFixedBlockSize && !space.IsFixedBlockSizeIndefinite {
		// Parent algorithm (e.g., flex column) has determined the block-size
		// authoritatively — it overrides the child's own CSS block-size.
		borderBoxBlock = space.AvailableSize.BlockSize
	} else if explicitBlock, ok := ResolveBlockSize(style, wdm, space, geom); ok {
		// CSS Tables: browsers (Blink, WebKit, Gecko) treat table/inline-table
		// height as border-box — the specified height already includes borders.
		// Per Blink's TableLayoutAlgorithm, table height is resolved with
		// kIncludingBorderPadding (border-box semantics).
		display := css.DisplayBlock
		if style != nil {
			display = style.GetDisplay()
		}
		isTable := display == css.DisplayTable || display == css.DisplayInlineTable
		if isTable && style.GetBoxSizing() != "border-box" {
			// Table border-box quirk: treat height as border-box even when
			// box-sizing is content-box. The explicitBlock from ResolveBlockSize
			// is content-box, so use it directly as border-box (don't add BP).
			borderBoxBlock = explicitBlock
		} else {
			borderBoxBlock = explicitBlock + geom.BlockBorderPadding()
		}
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

	// CSS Box Sizing 4 §5.1: aspect-ratio — when one dimension is definite
	// and the other is auto, compute the auto dimension from the ratio.
	// ratio = width / height (in physical terms).
	// Per spec, if box-sizing: border-box, the ratio applies to the border-box;
	// if box-sizing: content-box (default), it applies to the content-box.
	if ar := style.GetAspectRatio(); ar.IsSet && ar.Width > 0 && ar.Height > 0 {
		ratio := ar.Width / ar.Height
		if borderBoxBlock == Indefinite {
			var resolvedBlock float64
			if style.GetBoxSizing() == "border-box" {
				// Ratio applies to border-box dimensions directly.
				if wdm.IsHorizontal() {
					resolvedBlock = borderBoxInline / ratio
				} else {
					resolvedBlock = borderBoxInline * ratio
				}
			} else {
				// Ratio applies to content-box dimensions.
				contentInline := borderBoxInline - geom.InlineBorderPadding()
				if contentInline < 0 {
					contentInline = 0
				}
				var contentBlock float64
				if wdm.IsHorizontal() {
					contentBlock = contentInline / ratio
				} else {
					contentBlock = contentInline * ratio
				}
				resolvedBlock = contentBlock + geom.BlockBorderPadding()
			}
			borderBoxBlock = resolvedBlock
		} else if inlineSizeIsAuto && borderBoxBlock != Indefinite {
			// CSS Sizing 4 §5.1: reverse direction — derive inline from block
			// when inline-size is auto and block-size is definite.
			if style.GetBoxSizing() == "border-box" {
				if wdm.IsHorizontal() {
					borderBoxInline = borderBoxBlock * ratio
				} else {
					borderBoxInline = borderBoxBlock / ratio
				}
			} else {
				contentBlock := borderBoxBlock - geom.BlockBorderPadding()
				if contentBlock < 0 {
					contentBlock = 0
				}
				var contentInline float64
				if wdm.IsHorizontal() {
					contentInline = contentBlock * ratio
				} else {
					contentInline = contentBlock / ratio
				}
				borderBoxInline = contentInline + geom.InlineBorderPadding()
			}
		}
	}

	// Apply min/max block constraints only when block-size is definite.
	// CSS 2.1 §10.7: Apply max first, then min. This ensures min wins when min > max.
	if borderBoxBlock != Indefinite {
		contentBlock := borderBoxBlock - geom.BlockBorderPadding()
		if contentBlock < 0 {
			contentBlock = 0
		}
		if maxBlock, ok := ResolveMaxBlockSize(style, wdm, space, geom); ok {
			if contentBlock > maxBlock {
				contentBlock = maxBlock
			}
		}
		minBlock := ResolveMinBlockSize(style, wdm, space, geom)
		if contentBlock < minBlock {
			contentBlock = minBlock
		}
		borderBoxBlock = contentBlock + geom.BlockBorderPadding()
	}

	geom.BorderBoxSize = LogicalSize{InlineSize: borderBoxInline, BlockSize: borderBoxBlock}
	return geom
}

// computeMinMaxOnce lazily computes MinMaxSizes, caching the result.
func computeMinMaxOnce(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace, cache **MinMaxSizes) MinMaxSizes {
	if *cache != nil {
		return **cache
	}
	mm := ComputeMinMaxSizes(ctx, node, space)
	*cache = &mm
	return mm
}
