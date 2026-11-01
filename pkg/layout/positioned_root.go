package layout

import "louis14/pkg/css"

// layoutRootInView lays out the document root node in a vpWidth x vpHeight
// viewport (buildRootConstraintSpace) and places its border box in the view
// (placeRootInView). Shared by the top-level and nested-document layouts.
func layoutRootInView(ctx *LayoutContext, root *LayoutInputNode, vpWidth, vpHeight float64) (*LayoutResult, ConstraintSpace, PhysicalOffset) {
	rootStyle := root.Style()
	rootWDM := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	if rootStyle != nil {
		rootWDM = NewWritingDirectionMode(rootStyle)
	}
	space := buildRootConstraintSpace(rootStyle, rootWDM, vpWidth, vpHeight)
	result := layoutElement(ctx, root, space)
	if result == nil || result.Fragment == nil {
		return result, space, PhysicalOffset{}
	}
	return result, space, placeRootInView(rootStyle, rootWDM, vpWidth, vpHeight, result.Fragment)
}

// rootIsOutOfFlow reports whether the root is `position:absolute` /
// `position:fixed`: the view lays it out as an out-of-flow child against the
// ICB rather than as its in-flow block child.
func rootIsOutOfFlow(rootStyle *css.Style) bool {
	if rootStyle == nil {
		return false
	}
	pos := rootStyle.GetPosition()
	return pos == css.PositionAbsolute || pos == css.PositionFixed
}

// buildRootConstraintSpace builds the constraint space for the document root
// element given the viewport size.
//
// An in-flow root is laid out as the LayoutView's block layout lays out its
// single block child: the view is fixed at the initial containing block size
// (LayoutView::LayoutRoot, layout_view.cc:848-909 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the root's available and
// percentage-resolution sizes are the ICB, its auto inline-size stretches to
// the ICB less its margins, and its auto block-size is its content's — the
// root establishes a new formatting context (LayoutBlockFlow::
// CreatesNewFormattingContext's IsDocumentElement, layout_block_flow_hot.cc:
// 18-21), so nothing collapses through it. louis14 children take a
// margin-free available inline-size (BlockLayoutAlgorithm's child space), so
// the root's non-auto inline margins are taken off here. rootOffsetInView
// places it.
//
// For positioned roots the viewport IS the containing block (ICB), and the
// root runs through the same IMCB sizing logic used by OutOfFlowLayoutPart:
// when both insets on an axis are specified and the root's size is auto, the
// constraint space is fixed-size on that axis to stretch the root to the
// inset-modified CB (viewport − start − end). This mirrors Blink, where
// LayoutView hands `<html>` to OutOfFlowLayoutPart and the generic OOF
// resolver handles IMCB stretch-fit uniformly.
func buildRootConstraintSpace(
	rootStyle *css.Style,
	rootWDM WritingDirectionMode,
	vpWidth, vpHeight float64,
) ConstraintSpace {
	var rootInlineSize, rootBlockSize float64
	if rootWDM.IsHorizontal() {
		rootInlineSize = vpWidth
		rootBlockSize = vpHeight
	} else {
		rootInlineSize = vpHeight
		rootBlockSize = vpWidth
	}

	if !rootIsOutOfFlow(rootStyle) {
		margins := ResolveMargins(rootStyle, rootWDM, rootInlineSize)
		return NewConstraintSpaceBuilder(rootWDM, rootWDM, true).
			SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
			SetIsRootElement(true).
			SetAvailableSize(LogicalSize{
				InlineSize: max(0, rootInlineSize-margins.InlineSum()),
				BlockSize:  rootBlockSize,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: rootInlineSize,
				BlockSize:  rootBlockSize,
			}).
			SetPercentageResolutionInlineSize(rootInlineSize).
			Build()
	}

	cbLogical := LogicalSize{InlineSize: rootInlineSize, BlockSize: rootBlockSize}

	imcb, useFixedInline, useFixedBlock, availInline, availBlock :=
		computePositionedRootIMCB(rootStyle, rootWDM, cbLogical, vpWidth, vpHeight)
	_ = imcb // imcb is consumed by resolvePositionedRootOffset via the same inputs

	csb := NewConstraintSpaceBuilder(rootWDM, rootWDM, true).
		SetIsRootElement(true).
		SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: availBlock}).
		SetPercentageResolutionSize(cbLogical).
		SetPercentageResolutionInlineSize(cbLogical.InlineSize)
	if useFixedInline {
		csb.SetIsFixedInlineSize(true)
	}
	if useFixedBlock {
		csb.SetIsFixedBlockSize(true)
	}
	return csb.Build()
}

// rootOffsetInView returns the logical offset of an in-flow root's border
// box within the LayoutView (the ICB, icb in the root's writing mode), given
// the root's laid-out border-box inline-size: its block-start margin, and its
// inline-start margin with `auto` inline margins resolved against the ICB.
// The view's BlockLayoutAlgorithm places its single new-formatting-context
// child this way (LayoutNewFormattingContext → ResolveInlineAutoMargins,
// block_layout_algorithm.cc:2063, :2209-2213, length_utils.cc:1551-1570 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the view has no border or
// padding, and the root is its first child, so no margin collapses into
// the view.
func rootOffsetInView(rootStyle *css.Style, rootWDM WritingDirectionMode, icb LogicalSize, borderBoxInlineSize float64) LogicalOffset {
	margins := resolveInlineAutoMargins(rootStyle, rootWDM, icb.InlineSize, borderBoxInlineSize,
		ResolveMargins(rootStyle, rootWDM, icb.InlineSize))
	return LogicalOffset{InlineOffset: margins.InlineStart, BlockOffset: margins.BlockStart}
}

// placeRootInView returns the physical offset of the root's border box in
// the vpWidth x vpHeight viewport after layout produced fragment: by the
// IMCB math for a positioned root (resolvePositionedRootOffset), otherwise
// at rootOffsetInView converted against the ICB — so a vertical-rl root
// sits at the ICB's right (block-start) edge.
func placeRootInView(rootStyle *css.Style, rootWDM WritingDirectionMode, vpWidth, vpHeight float64, fragment *PhysicalFragment) PhysicalOffset {
	if rootIsOutOfFlow(rootStyle) {
		x, y := resolvePositionedRootOffset(rootStyle, rootWDM, vpWidth, vpHeight, fragment)
		return PhysicalOffset{X: x, Y: y}
	}
	icbPhys := PhysicalSize{Width: vpWidth, Height: vpHeight}
	offset := rootOffsetInView(rootStyle, rootWDM, ToLogicalSize(icbPhys, rootWDM.WM),
		NewLogicalFragment(rootWDM, fragment).InlineSize())
	return NewConverter(rootWDM, icbPhys).ToPhysicalOffset(offset, geomSizeToOld(fragment.Size))
}

// computePositionedRootIMCB runs the IMCB sizing step for a positioned root
// against the viewport (ICB). Returns the IMCB, whether each axis is
// pre-sized to IMCB (fixed available-size), and the resulting logical
// available size.
//
// Shared between buildRootConstraintSpace (pre-layout) and
// resolvePositionedRootOffset (post-layout), so both paths see the same IMCB.
func computePositionedRootIMCB(
	rootStyle *css.Style,
	rootWDM WritingDirectionMode,
	cbLogical LogicalSize,
	vpWidth, vpHeight float64,
) (imcb InsetModifiedContainingBlock, useFixedInline, useFixedBlock bool, availInline, availBlock float64) {
	cbPhys := PhysicalSize{Width: vpWidth, Height: vpHeight}
	wdm := rootWDM

	// Physical insets resolved against the viewport, converted to logical.
	physOffset := rootStyle.GetPositionOffsetResolved(cbPhys.Width, cbPhys.Height)
	insets := PhysicalInsetsToLogical(physOffset, wdm)
	oofInsets := insets.AsOofInsets()

	// Root's margins (percentages resolve against CB inline-size) and auto flags.
	rootMargins := ResolveMargins(rootStyle, wdm, cbLogical.InlineSize)
	rawMargin := rootStyle.GetMargin()
	autoInlineStart, autoInlineEnd, autoBlockStart, autoBlockEnd :=
		PhysicalAutoMarginsToLogical(rawMargin, wdm)

	// Root's border + padding. Root is its own child → parallel WDM.
	rootGeom := ComputeFragmentGeometry(rootStyle, wdm)
	rootBPInline := rootGeom.InlineBorderPadding()
	rootBPBlock := rootGeom.BlockBorderPadding()

	// Static position is (0, 0) with BiasStart — the root has no normal-flow
	// placement in its CB. CB's padding is zero (viewport has no padding).
	imcb = ComputeUnclampedIMCB(
		cbLogical,
		LogicalAlignment{}, // ItemPositionNormal on both axes → BiasStart
		oofInsets,
		0, 0,
		BiasStart, BiasStart,
		false, // align-self direction defaults to block
		wdm, wdm,
	)

	availInline = cbLogical.InlineSize
	availBlock = cbLogical.BlockSize

	stretchable := !isNonStretchableDisplay(rootStyle)

	if stretchable && insets.HasInlineStart && insets.HasInlineEnd &&
		isAutoSizeInDirection(rootStyle, wdm, true) {
		mStart := rootMargins.InlineStart
		mEnd := rootMargins.InlineEnd
		if autoInlineStart {
			mStart = 0
		}
		if autoInlineEnd {
			mEnd = 0
		}
		contentInline := imcb.InlineSize() - mStart - mEnd - rootBPInline
		if contentInline < 0 {
			contentInline = 0
		}
		availInline = contentInline + rootBPInline
		useFixedInline = true
	}

	if stretchable && insets.HasBlockStart && insets.HasBlockEnd &&
		isAutoSizeInDirection(rootStyle, wdm, false) {
		mStart := rootMargins.BlockStart
		mEnd := rootMargins.BlockEnd
		if autoBlockStart {
			mStart = 0
		}
		if autoBlockEnd {
			mEnd = 0
		}
		contentBlock := imcb.BlockSize() - mStart - mEnd - rootBPBlock
		if contentBlock < 0 {
			contentBlock = 0
		}
		availBlock = contentBlock + rootBPBlock
		useFixedBlock = true
	}

	return
}

// resolvePositionedRootOffset returns the physical (x, y) position of a
// positioned root's border-box within the viewport, after layout has produced
// `fragment`. Uses the same IMCB the constraint-space builder used, then
// applies ComputeMargins (auto-margin centering / absorption) and
// ComputeInsets (bias / default-overflow resolution) to get the logical
// inset-start + margin-start, converted to physical via a viewport-sized
// WritingModeConverter.
func resolvePositionedRootOffset(
	rootStyle *css.Style,
	rootWDM WritingDirectionMode,
	vpWidth, vpHeight float64,
	fragment *PhysicalFragment,
) (float64, float64) {
	cbLogical := LogicalSize{
		InlineSize: vpWidth,
		BlockSize:  vpHeight,
	}
	if !rootWDM.IsHorizontal() {
		cbLogical = LogicalSize{InlineSize: vpHeight, BlockSize: vpWidth}
	}

	imcb, _, _, _, _ := computePositionedRootIMCB(rootStyle, rootWDM, cbLogical, vpWidth, vpHeight)

	wdm := rootWDM
	rawMargin := rootStyle.GetMargin()
	rootMargins := ResolveMargins(rootStyle, wdm, cbLogical.InlineSize)
	autoInlineStart, autoInlineEnd, autoBlockStart, autoBlockEnd :=
		PhysicalAutoMarginsToLogical(rawMargin, wdm)

	rootLogical := NewLogicalFragment(wdm, fragment)
	usedInline := rootLogical.InlineSize()
	usedBlock := rootLogical.BlockSize()

	// Inline axis: start-dominance picks direction-start in LTR, end in RTL.
	isStartDominantInline := !wdm.IsRTL()
	marginStartI, marginEndI, appliedAutoI := ComputeMargins(
		imcb.InlineSize(),
		rootMargins.InlineStart, rootMargins.InlineEnd,
		autoInlineStart, autoInlineEnd,
		usedInline, imcb.HasAutoInlineInset,
		isStartDominantInline,
	)
	insetStartI, _ := ComputeInsets(
		cbLogical.InlineSize,
		imcb.InlineStart, imcb.InlineEnd,
		imcb.InlineInsetBias,
		imcb.InlineHasDefaultAlignmentOverflow,
		imcb.InlineDefaultInsetBias, imcb.HasInlineDefaultInsetBias,
		imcb.InlineSafeInsetBias, imcb.HasInlineSafeInsetBias,
		marginStartI, marginEndI, usedInline, appliedAutoI,
	)

	marginStartB, marginEndB, appliedAutoB := ComputeMargins(
		imcb.BlockSize(),
		rootMargins.BlockStart, rootMargins.BlockEnd,
		autoBlockStart, autoBlockEnd,
		usedBlock, imcb.HasAutoBlockInset,
		true, // block axis is always start-dominant
	)
	insetStartB, _ := ComputeInsets(
		cbLogical.BlockSize,
		imcb.BlockStart, imcb.BlockEnd,
		imcb.BlockInsetBias,
		imcb.BlockHasDefaultAlignmentOverflow,
		imcb.BlockDefaultInsetBias, imcb.HasBlockDefaultInsetBias,
		imcb.BlockSafeInsetBias, imcb.HasBlockSafeInsetBias,
		marginStartB, marginEndB, usedBlock, appliedAutoB,
	)

	conv := NewConverter(wdm, PhysicalSize{Width: vpWidth, Height: vpHeight})
	phys := conv.ToPhysicalOffset(LogicalOffset{
		InlineOffset: insetStartI + marginStartI,
		BlockOffset:  insetStartB + marginStartB,
	}, geomSizeToOld(fragment.Size))
	return phys.X, phys.Y
}
