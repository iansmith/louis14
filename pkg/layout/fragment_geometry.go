package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
)

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

// EffectiveBlockBorderPadding returns the effective block-direction border +
// scrollbar + padding sum that should contribute to this fragment's geometric
// block-size, accounting for box-decoration-break and fragment position.
//
// In clone mode every fragment behaves as an independent decorated box — all
// borders appear on every fragment regardless of position.
//
// In slice mode (default) borders belong to the whole element — block-start
// appears only on the first fragment, block-end only on the last.
//
// Mirrors Blink's FinishFragmentation sides bitmask via
// ShouldIncludeBlockEndBorderPadding / ShouldCloneBlockStartBorderPadding.
func EffectiveBlockBorderPadding(geom FragmentGeometry, hasClonedBoxDecorations bool, breakToken *BlockBreakToken, isBreaking bool) float64 {
	if hasClonedBoxDecorations {
		return geom.BlockBorderPadding()
	}
	border := geom.Border
	padding := geom.Padding
	isContinuation := breakToken != nil && !breakToken.ConsumedBlockSize.IsZero()
	if isContinuation {
		border.BlockStart = 0
		padding.BlockStart = 0
	}
	if isBreaking {
		border.BlockEnd = 0
		padding.BlockEnd = 0
	}
	return border.BlockStart + border.BlockEnd + padding.BlockStart + padding.BlockEnd
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
// Per CSS Scrollbars Styling Module Level 1, scrollbar-width: auto uses the
// platform-default classic scrollbar width (17 px). thin uses 10 px; none
// uses 0. This is an intentional shift from overlay (0 px) to classic scrollbar
// sizing for the "auto" case.
func classicScrollbarWidth(style *css.Style) float64 {
	switch style.GetScrollbarWidth() {
	case "thin":
		return 10
	case "none":
		return 0
	default: // "auto" and any unknown value
		return 17
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

// ResolveIntrinsicBlockSize resolves an intrinsic sizing keyword for block-size
// given the element's intrinsic (content) block-size. On the block axis,
// min-content / max-content / fit-content all collapse to the content-derived
// block-size — there is no inline wrapping/breaking to differentiate them.
// Returns the content-box block-size.
//
// Mirrors Blink's ComputeMinMaxSizes block-axis treatment: the keyword resolves
// against the natural block-axis content size (what auto block-size produces).
func ResolveIntrinsicBlockSize(keyword string, intrinsicBlockSize float64) float64 {
	switch keyword {
	case "min-content", "-webkit-min-content", "-moz-min-content",
		"max-content", "-webkit-max-content", "-moz-max-content",
		"fit-content", "-moz-fit-content", "-webkit-fill-available":
		return intrinsicBlockSize
	}
	return 0
}

// minBlockProperty returns the CSS property name controlling min-block-size
// for the given writing mode.
func minBlockProperty(wdm WritingDirectionMode) string {
	if wdm.IsVertical() {
		return "min-width"
	}
	return "min-height"
}

// maxBlockProperty returns the CSS property name controlling max-block-size
// for the given writing mode.
func maxBlockProperty(wdm WritingDirectionMode) string {
	if wdm.IsVertical() {
		return "max-width"
	}
	return "max-height"
}

// ResolveMinBlockSizeWithIntrinsic resolves min-block-size including the
// intrinsic sizing keywords (min-content / max-content / fit-content), which
// the length-only ResolveMinBlockSize cannot handle. intrinsicBlockSize is the
// element's natural block-axis content size (content-box). Returns the
// content-box min-block-size as a float64 — callers already work in float64
// from this point on (post-layout sizing).
//
// Intrinsic keywords resolve directly to the content's intrinsic block-size
// regardless of box-sizing: the keyword measures actual content, not a
// user-declared length that needs border-box conversion.
func ResolveMinBlockSizeWithIntrinsic(
	style *css.Style, wdm WritingDirectionMode, space ConstraintSpace,
	geom FragmentGeometry, intrinsicBlockSize float64,
) float64 {
	if style == nil {
		return 0
	}
	if v, ok := style.Get(minBlockProperty(wdm)); ok && IsIntrinsicKeyword(v) {
		return ResolveIntrinsicBlockSize(v, intrinsicBlockSize)
	}
	return ResolveMinBlockSize(style, wdm, space, geom).Float64()
}

// ResolveMaxBlockSizeWithIntrinsic resolves max-block-size including the
// intrinsic sizing keywords. Returns (content-box value, true) when the
// property is set; (0, false) when unset / "none". See
// ResolveMinBlockSizeWithIntrinsic for box-sizing rationale.
func ResolveMaxBlockSizeWithIntrinsic(
	style *css.Style, wdm WritingDirectionMode, space ConstraintSpace,
	geom FragmentGeometry, intrinsicBlockSize float64,
) (float64, bool) {
	if style == nil {
		return 0, false
	}
	if v, ok := style.Get(maxBlockProperty(wdm)); ok && IsIntrinsicKeyword(v) {
		return ResolveIntrinsicBlockSize(v, intrinsicBlockSize), true
	}
	if maxLU, ok := ResolveMaxBlockSize(style, wdm, space, geom); ok {
		return maxLU.Float64(), true
	}
	return 0, false
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
//
// Returns LayoutUnit at the boundary (Phase 13e′). Internal arithmetic
// stays float64 — `applyBoxSizingInline` consumes float64 from the CSS
// length / calc / percentage paths. The boundary uses `FromFloat64Round`
// (round-half-away-from-zero) rather than Blink's truncating
// `LayoutUnit(value)` ctor: louis14 stores `Length.value` as float64
// (Blink uses float32), so em-times-fontSize products like `8.2 * 50` land
// at `409.999999999...` in IEEE 754 — Trunc would amplify the rounding
// error to 409.984 (off by 1/64 raw), Round snaps to 410.0 exact. Already
// 1/64-clean values (the dominant case: percentages from `ResolvePercent`,
// integer pixel values from CSS) round-trip identically under either mode.
func ResolveInlineSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (layoutunit.LayoutUnit, bool) {
	if style == nil {
		return layoutunit.LayoutUnit{}, false
	}

	// Determine which CSS property controls inline-size.
	prop := "width"
	if wdm.IsVertical() {
		prop = "height"
	}

	val, ok := style.Get(prop)
	if !ok || val == "" || val == "auto" {
		return layoutunit.LayoutUnit{}, false
	}

	// CSS Values 4 §10: math functions (calc/min/max/clamp) with a %
	// term: resolve against the containing block's inline-size. GetLength
	// alone resolves percentages with base=0, so calc(52px + 100% + 52px)
	// or max(50%, 100px) would lose the percentage term.
	if css.IsMathFunctionWithPercent(val) {
		if result, calcOK := css.EvalMathFunctionWithPercent(
			val, style.GetFontSize(),
			space.PercentageResolutionSize.InlineSize.Float64(),
		); calcOK {
			return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, result)), true
		}
	}

	// Check for explicit length (handles calc without percentages, px, em, etc.).
	if v, ok := style.GetLength(prop); ok {
		return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, v)), true
	}

	// Check for percentage.
	if pct, ok := style.GetPercentage(prop); ok {
		result := layoutunit.ResolvePercent(
			space.PercentageResolutionSize.InlineSize, pct).Float64()
		return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, result)), true
	}

	return layoutunit.LayoutUnit{}, false
}

// ResolveMinInlineSize resolves min-width/min-height as min-inline-size.
// Returns 0 if not set (CSS 2.1 §10.4: min-width defaults to 0).
//
// Returns LayoutUnit at the boundary (Phase 13e′.2) — see ResolveInlineSize
// for the FromFloat64Round rounding-mode rationale.
func ResolveMinInlineSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) layoutunit.LayoutUnit {
	if style == nil {
		return layoutunit.LayoutUnit{}
	}
	prop := "min-width"
	if wdm.IsVertical() {
		prop = "min-height"
	}
	// CSS Values 4 §10: math functions with a % term need the
	// containing block's inline-size as the percent base. GetLength
	// alone resolves percentages against base=0 and rejects the
	// expression. Mirrors the ResolveInlineSize calc-with-percent
	// branch above but for the broader math-function family —
	// calc(50% - 3px), max(50%, 100px), clamp(0px, 50%, 200px), etc.
	// Flex item branch is skipped — see ResolveMaxInlineSize.
	if val, ok := style.Get(prop); ok && css.IsMathFunctionWithPercent(val) &&
		!space.IsInsideFlexibleBox &&
		space.PercentageResolutionSize.InlineSize.Float64() > 0 {
		if result, calcOK := css.EvalMathFunctionWithPercent(
			val, style.GetFontSize(),
			space.PercentageResolutionSize.InlineSize.Float64(),
		); calcOK {
			return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, result))
		}
	}
	if v, ok := style.GetLength(prop); ok {
		return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, v))
	}
	if pct, ok := style.GetPercentage(prop); ok {
		result := layoutunit.ResolvePercent(
			space.PercentageResolutionSize.InlineSize, pct).Float64()
		return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, result))
	}
	return layoutunit.LayoutUnit{}
}

// ResolveMaxInlineSize resolves max-width/max-height as max-inline-size.
// Returns (value, true) if set; (0, false) if "none" or not specified.
//
// Returns LayoutUnit at the boundary (Phase 13e′.2) — see ResolveInlineSize
// for the FromFloat64Round rounding-mode rationale.
func ResolveMaxInlineSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (layoutunit.LayoutUnit, bool) {
	if style == nil {
		return layoutunit.LayoutUnit{}, false
	}
	prop := "max-width"
	if wdm.IsVertical() {
		prop = "max-height"
	}
	val, ok := style.Get(prop)
	if !ok || val == "none" || val == "" {
		return layoutunit.LayoutUnit{}, false
	}
	// CSS Values 4 §10: math functions with a % term need the
	// containing block's inline-size as the percent base. Mirrors
	// ResolveMinInlineSize above.
	//
	// Skip flex items: their max-* values are resolved against an
	// intermediate (possibly cyclic) parent size that doesn't match the
	// final flex container content size, and engaging the math-with-%
	// path here causes spurious truncation (css-values/calc-rounding-002).
	// Flex layout has its own multi-pass max-width resolution that
	// computes the right values once the container size is determined.
	if css.IsMathFunctionWithPercent(val) && !space.IsInsideFlexibleBox &&
		space.PercentageResolutionSize.InlineSize.Float64() > 0 {
		if result, calcOK := css.EvalMathFunctionWithPercent(
			val, style.GetFontSize(),
			space.PercentageResolutionSize.InlineSize.Float64(),
		); calcOK {
			return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, result)), true
		}
	}
	if v, ok := style.GetLength(prop); ok {
		return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, v)), true
	}
	if pct, ok := style.GetPercentage(prop); ok {
		result := layoutunit.ResolvePercent(
			space.PercentageResolutionSize.InlineSize, pct).Float64()
		return layoutunit.FromFloat64Round(applyBoxSizingInline(style, geom, result)), true
	}
	return layoutunit.LayoutUnit{}, false
}

// ResolveMinBlockSize resolves min-height/min-width as min-block-size.
//
// Returns LayoutUnit at the boundary (Phase 13e′.3) — see ResolveInlineSize
// for the FromFloat64Round rounding-mode rationale.
func ResolveMinBlockSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) layoutunit.LayoutUnit {
	if style == nil {
		return layoutunit.LayoutUnit{}
	}
	prop := "min-height"
	if wdm.IsVertical() {
		prop = "min-width"
	}
	// CSS Values 4 §10: math functions with a % term resolve against
	// block-size (not inline-size) for min-height. A percentage block
	// dimension against an indefinite CB block-size is ignored per
	// CSS 2.1 §10.7 — same gate the bare-percentage branch below uses.
	// Flex item branch is skipped — see ResolveMaxInlineSize.
	if val, ok := style.Get(prop); ok && css.IsMathFunctionWithPercent(val) &&
		!space.IsBlockSizeIndefinite() && !space.IsInsideFlexibleBox {
		if result, calcOK := css.EvalMathFunctionWithPercent(
			val, style.GetFontSize(),
			space.PercentageResolutionSize.BlockSize.Float64(),
		); calcOK {
			return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, result))
		}
	}
	if v, ok := style.GetLength(prop); ok {
		return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, v))
	}
	if pct, ok := style.GetPercentage(prop); ok && !space.IsBlockSizeIndefinite() {
		result := layoutunit.ResolvePercent(
			space.PercentageResolutionSize.BlockSize, pct).Float64()
		return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, result))
	}
	return layoutunit.LayoutUnit{}
}

// ResolveMaxBlockSize resolves max-height/max-width as max-block-size.
// Returns (value, true) if set; (0, false) if "none" or not specified.
//
// Returns LayoutUnit at the boundary (Phase 13e′.3) — see ResolveInlineSize
// for the FromFloat64Round rounding-mode rationale.
func ResolveMaxBlockSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (layoutunit.LayoutUnit, bool) {
	if style == nil {
		return layoutunit.LayoutUnit{}, false
	}
	prop := "max-height"
	if wdm.IsVertical() {
		prop = "max-width"
	}
	val, ok := style.Get(prop)
	if !ok || val == "none" || val == "" {
		return layoutunit.LayoutUnit{}, false
	}
	// CSS Values 4 §10: math functions with a % term resolve against
	// the containing block's block-size for max-height. Mirrors
	// ResolveMinBlockSize. Flex item branch is skipped — see
	// ResolveMaxInlineSize.
	if css.IsMathFunctionWithPercent(val) && !space.IsBlockSizeIndefinite() &&
		!space.IsInsideFlexibleBox {
		if result, calcOK := css.EvalMathFunctionWithPercent(
			val, style.GetFontSize(),
			space.PercentageResolutionSize.BlockSize.Float64(),
		); calcOK {
			return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, result)), true
		}
	}
	if v, ok := style.GetLength(prop); ok {
		return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, v)), true
	}
	if pct, ok := style.GetPercentage(prop); ok && !space.IsBlockSizeIndefinite() {
		result := layoutunit.ResolvePercent(
			space.PercentageResolutionSize.BlockSize, pct).Float64()
		return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, result)), true
	}
	return layoutunit.LayoutUnit{}, false
}

// ResolveBlockSize resolves the element's block-size from CSS.
// Returns the content block-size and whether it was explicitly set.
//
// Returns LayoutUnit at the boundary (Phase 13e′) — see ResolveInlineSize
// for the FromFloat64Round rounding-mode rationale.
func ResolveBlockSize(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace, geom FragmentGeometry) (layoutunit.LayoutUnit, bool) {
	if style == nil {
		return layoutunit.LayoutUnit{}, false
	}

	prop := "height"
	if wdm.IsVertical() {
		prop = "width"
	}

	val, _ := style.Get(prop)

	// CSS Values 4 §10: math functions (calc/min/max/clamp) with a %
	// term: resolve against the containing block's block-size. GetLength
	// alone resolves percentages with base=0, so calc(100% - 16px) or
	// max(100%, 50px) would lose the percentage term. The %-against-
	// indefinite-CB gate matches the bare-percentage branch below.
	if css.IsMathFunctionWithPercent(val) && !space.IsBlockSizeIndefinite() {
		if result, calcOK := css.EvalMathFunctionWithPercent(
			val, style.GetFontSize(),
			space.PercentageResolutionSize.BlockSize.Float64(),
		); calcOK {
			return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, result)), true
		}
	}

	if v, ok := style.GetLength(prop); ok {
		return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, v)), true
	}

	if pct, ok := style.GetPercentage(prop); ok {
		if !space.IsBlockSizeIndefinite() {
			result := layoutunit.ResolvePercent(
				space.PercentageResolutionSize.BlockSize, pct).Float64()
			return layoutunit.FromFloat64Round(applyBoxSizingBlock(style, geom, result)), true
		}
		// Percentage against indefinite → auto.
		return layoutunit.LayoutUnit{}, false
	}

	return layoutunit.LayoutUnit{}, false
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

	// Lazy cache for content-based MinMaxSizes — used by min/max-inline-size
	// intrinsic keyword resolution (which must bypass the explicit-inline-size
	// short-circuit) and computed at most once per element.
	var contentMinMaxCache *MinMaxSizes

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
		borderBoxInline = space.AvailableSize.InlineSize.Float64()
	} else if IsIntrinsicKeyword(inlineVal) {
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = ResolveIntrinsicInlineSize(inlineVal, minMax, available) + geom.InlineBorderPadding()
	} else if css.IsFitContentFunction(inlineVal) {
		// fit-content(<length-percentage>): min(max-content, max(min-content, resolved-arg))
		// CSS Sizing 3 §5.1.5. The argument resolves against the CB's inline-size.
		minMax := ComputeMinMaxSizes(ctx, node, space)
		cbInline := space.PercentageResolutionSize.InlineSize.Float64()
		contentSize := ResolveFitContentInlineSize(inlineVal, style, minMax, cbInline)
		borderBoxInline = contentSize + geom.InlineBorderPadding()
	} else if explicitInline, ok := ResolveInlineSize(style, wdm, space, geom); ok {
		borderBoxInline = explicitInline.Float64() + geom.InlineBorderPadding()
	} else if space.IsOrthogonalWritingModeRoot && !needsShrinkToFit(style) {
		// §7.3: block-level orthogonal auto inline-size fills available space
		// minus inline margins. Constrained by min/max inline-size.
		margins := ResolveMargins(style, wdm, pctBase)
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize.Float64() - margins.InlineStart - margins.InlineEnd - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = minMax.ShrinkToFit(available) + geom.InlineBorderPadding()
		inlineSizeIsAuto = true
	} else if needsShrinkToFit(style) || space.IsOrthogonalWritingModeRoot {
		// Shrink-to-fit: floats, abs-pos, inline-block, or orthogonal
		// positioned/float elements.
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = minMax.ShrinkToFit(available) + geom.InlineBorderPadding()
		inlineSizeIsAuto = true
	} else if space.IsInsideFlexibleBox && !space.IsFixedInlineSize {
		minMax := ComputeMinMaxSizes(ctx, node, space)
		available := space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		borderBoxInline = minMax.ShrinkToFit(available) + geom.InlineBorderPadding()
		inlineSizeIsAuto = true
	} else {
		// Auto inline-size: fill available space.
		borderBoxInline = space.AvailableSize.InlineSize.Float64()
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
		// Intrinsic keyword min/max constraints must see the element's
		// CONTENT intrinsic sizes (its children's contributions), not the
		// element's clamped explicit inline-size. ComputeMinMaxSizes's fast
		// path returns explicitInline for both min/max when width is set,
		// which would defeat the constraint.
		minMax := computeContentMinMaxOnce(ctx, node, space, &contentMinMaxCache)
		available := space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		maxInline := ResolveIntrinsicInlineSize(maxInlineVal, minMax, available)
		if contentInline > maxInline {
			contentInline = maxInline
		}
	} else if ok && css.IsFitContentFunction(maxInlineVal) {
		// fit-content(<length-percentage>) as max-inline-size.
		// Must use content-based MinMaxSizes (same reason as bare intrinsic keywords above).
		minMax := computeContentMinMaxOnce(ctx, node, space, &contentMinMaxCache)
		cbInline := space.PercentageResolutionSize.InlineSize.Float64()
		maxInline := ResolveFitContentInlineSize(maxInlineVal, style, minMax, cbInline)
		if contentInline > maxInline {
			contentInline = maxInline
		}
	} else if maxInline, ok := ResolveMaxInlineSize(style, wdm, space, geom); ok {
		mi := maxInline.Float64()
		if contentInline > mi {
			contentInline = mi
		}
	}

	// Resolve min-inline-size second (min wins over max per CSS 2.1 §10.4).
	minInlineProp := "min-width"
	if wdm.IsVertical() {
		minInlineProp = "min-height"
	}
	minInline := ResolveMinInlineSize(style, wdm, space, geom).Float64()
	if minInlineVal, ok := style.Get(minInlineProp); ok && IsIntrinsicKeyword(minInlineVal) {
		// Intrinsic keyword min/max constraints must see the element's
		// CONTENT intrinsic sizes (its children's contributions), not the
		// element's clamped explicit inline-size. ComputeMinMaxSizes's fast
		// path returns explicitInline for both min/max when width is set,
		// which would defeat the constraint.
		minMax := computeContentMinMaxOnce(ctx, node, space, &contentMinMaxCache)
		available := space.AvailableSize.InlineSize.Float64() - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		minInline = ResolveIntrinsicInlineSize(minInlineVal, minMax, available)
	} else if ok && css.IsFitContentFunction(minInlineVal) {
		// fit-content(<length-percentage>) as min-inline-size.
		// Must use content-based MinMaxSizes (same reason as bare intrinsic keywords above).
		minMax := computeContentMinMaxOnce(ctx, node, space, &contentMinMaxCache)
		cbInline := space.PercentageResolutionSize.InlineSize.Float64()
		minInline = ResolveFitContentInlineSize(minInlineVal, style, minMax, cbInline)
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
	if IsIntrinsicKeyword(blockVal) || css.IsFitContentFunction(blockVal) {
		// Treat as auto — layout will determine block-size from content.
		// fit-content(<L>) on the block axis resolves against the natural content
		// block-size (same as min-content/max-content/fit-content bare keyword).
		// The clamping is applied post-layout via ResolveMinBlockSizeWithIntrinsic /
		// ResolveMaxBlockSizeWithIntrinsic when the result feeds min/max-block-size.
	} else if space.IsContentSuggestionLayout {
		// §4.5 content suggestion: ignore the element's own explicit CSS
		// block-size — only content determines the size.
	} else if space.IsBlockSizeOverride && space.IsFixedBlockSize && !space.IsFixedBlockSizeIndefinite {
		// Parent algorithm (e.g., flex column) has determined the block-size
		// authoritatively — it overrides the child's own CSS block-size.
		// This is checked BEFORE ResolveBlockSize so the flex-resolved main
		// size takes priority over the item's explicit CSS block property.
		// Critical for sideways-lr: the CSS block property is 'width', and
		// without this override, the item's explicit width would replace the
		// flex-resolved size.
		//
		// Mirrors Blink's handling of ConstraintSpace::IsBlockSizeOverride.
		borderBoxBlock = space.AvailableSize.BlockSize.Float64()
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
			borderBoxBlock = explicitBlock.Float64()
		} else {
			borderBoxBlock = explicitBlock.Float64() + geom.BlockBorderPadding()
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
			borderBoxBlock = space.AvailableSize.BlockSize.Float64()
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
		if maxBlockLU, ok := ResolveMaxBlockSize(style, wdm, space, geom); ok {
			maxBlock := maxBlockLU.Float64()
			if contentBlock > maxBlock {
				contentBlock = maxBlock
			}
		}
		minBlock := ResolveMinBlockSize(style, wdm, space, geom).Float64()
		if contentBlock < minBlock {
			contentBlock = minBlock
		}
		borderBoxBlock = contentBlock + geom.BlockBorderPadding()
	}

	geom.BorderBoxSize = LogicalSize{InlineSize: borderBoxInline, BlockSize: borderBoxBlock}
	return geom
}

// computeContentMinMaxOnce lazily computes content-based MinMaxSizes (bypassing
// the explicit-inline-size short-circuit), caching the result. Used by min/max
// inline-size intrinsic-keyword resolution, which must see the element's
// children's intrinsic sizes — the standard ComputeMinMaxSizes fast-path would
// clamp both ends to the element's own explicit inline-size and the constraint
// would have no effect.
func computeContentMinMaxOnce(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace, cache **MinMaxSizes) MinMaxSizes {
	if *cache != nil {
		return **cache
	}
	mm := computeContentMinMaxSizes(ctx, node, space)
	*cache = &mm
	return mm
}

// resolveFitContentArgInline resolves the argument of a fit-content(<L>) value
// against the containing block's inline-size. Returns (resolvedPx, true) on
// success; (0, false) if the argument cannot be resolved (e.g. a percentage
// against an indefinite CB — caller should treat as max-content).
//
// Mirrors Blink's length_utils.cc ResolveInlineLengthInternal kFitContent branch
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func resolveFitContentArgInline(argStr string, style *css.Style, cbInlineSize float64) (float64, bool) {
	// calc() with percentage terms
	if css.IsCalcWithPercent(argStr) {
		if result, ok := css.EvalCalcWithPercent(
			argStr[5:len(argStr)-1], // strip "calc(" and ")"
			style.GetFontSize(),
			cbInlineSize,
		); ok {
			return result, true
		}
		return 0, false
	}
	// Plain length (px, em, rem, vw, etc.)
	if v, ok := style.GetLengthForVal(argStr); ok {
		return v, true
	}
	// Percentage
	if pct, ok := css.ParsePercentage(argStr); ok {
		if cbInlineSize <= 0 {
			// Indefinite CB: percentage is unresolvable → treat as max-content.
			return 0, false
		}
		return pct / 100.0 * cbInlineSize, true
	}
	return 0, false
}

// ResolveFitContentInlineSize applies the CSS Sizing 3 §5.1.5 formula to a
// fit-content(<length-percentage>) value on the inline axis:
//
//	min(max-content, max(min-content, resolved-arg))
//
// When the argument is unresolvable (e.g. percentage against indefinite CB),
// returns max-content per CSS Sizing 3 §5.1.5 (treat as if arg ≥ max-content).
func ResolveFitContentInlineSize(fitContentVal string, style *css.Style, minMax MinMaxSizes, cbInlineSize float64) float64 {
	argStr, ok := css.ParseFitContentArg(fitContentVal)
	if !ok {
		// Malformed — fall back to shrink-to-fit (bare fit-content behaviour).
		return minMax.ShrinkToFit(cbInlineSize)
	}
	resolved, argOK := resolveFitContentArgInline(argStr, style, cbInlineSize)
	if !argOK {
		// Unresolvable percentage against indefinite CB → clamp to max-content.
		return minMax.MaxContent
	}
	// min(max-content, max(min-content, resolved))
	result := resolved
	if result < minMax.MinContent {
		result = minMax.MinContent
	}
	if result > minMax.MaxContent {
		result = minMax.MaxContent
	}
	return result
}
