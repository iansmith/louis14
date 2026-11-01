package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
)

// FieldsetBreakTokenData is the fieldset-specific part of a fieldset
// container's break token: the block-size the rendered legend took from the
// first fragment's content box. Mirrors Blink's FieldsetBreakTokenData
// (core/layout/forms/fieldset_break_token_data.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type FieldsetBreakTokenData struct {
	LegendBlockSizeContribution float64
}

// legendBlockAlignment is where a rendered legend sits along the fieldset's
// block-start border. Mirrors the anonymous-namespace LegendBlockAlignment
// (fieldset_layout_algorithm.cc:23-27 @ a9f50e522efa).
type legendBlockAlignment int

const (
	legendBlockAlignmentStart legendBlockAlignment = iota
	legendBlockAlignmentCenter
	legendBlockAlignmentEnd
)

// computeLegendBlockAlignment aligns the legend by its auto inline margins,
// else by its text-align. Mirrors ComputeLegendBlockAlignment
// (fieldset_layout_algorithm.cc:29-56 @ a9f50e522efa).
func computeLegendBlockAlignment(legendStyle *css.Style, fieldsetWDM WritingDirectionMode) legendBlockAlignment {
	startAuto, endAuto, _, _ := PhysicalAutoMarginsToLogical(legendStyle.GetMargin(), fieldsetWDM)
	switch {
	case startAuto && endAuto:
		return legendBlockAlignmentCenter
	case startAuto:
		return legendBlockAlignmentEnd
	case endAuto:
		return legendBlockAlignmentStart
	}
	isLTR := fieldsetWDM.Dir == DirectionLTR
	align, _ := legendStyle.Get("text-align")
	switch align {
	case "left":
		if isLTR {
			return legendBlockAlignmentStart
		}
		return legendBlockAlignmentEnd
	case "right":
		if isLTR {
			return legendBlockAlignmentEnd
		}
		return legendBlockAlignmentStart
	case "center":
		return legendBlockAlignmentCenter
	}
	return legendBlockAlignmentStart
}

// FieldsetLayoutAlgorithm lays out a fieldset container: a child fragment for
// the rendered legend (if any), placed over the block-start border, and one
// for the anonymous fieldset content box, which carries the fieldset's
// padding, scrollbars, multicol and display type. The fieldset fragment
// itself holds only the border. Ported from Blink's FieldsetLayoutAlgorithm
// (core/layout/forms/fieldset_layout_algorithm.{h,cc} @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// Coordinates: Blink's child offsets are border-box relative; louis14's are
// relative to the fragment's content edge, which for a fieldset container is
// its padding edge (the fragment carries no padding, see Layout). The
// algorithm keeps Blink's border-box-relative intrinsicBlockSize and
// converts when adding children (toContentOffset).
type FieldsetLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace

	containerBuilder *BoxFragmentBuilder

	// geom is the fieldset's full fragment geometry (padding included);
	// the builder's geometry has padding and scrollbars removed.
	geom FragmentGeometry
	// borders are the borders this fragment carries (block-start cleared on
	// a continuation), Blink's Borders().
	borders                   LogicalEdges
	intrinsicBlockSize        float64
	minimumBorderBoxBlockSize float64
	consumedBlockSize         float64

	// legendBlockSizeContribution is recorded on the outgoing break token
	// when the fieldset takes part in block fragmentation.
	legendBlockSizeContribution float64

	earlyBreak             *EarlyBreak
	relayoutSpaceShortage  float64
	contentBreakToken      *BlockBreakToken
	contentBrokeBefore     bool
	hasSeenAllChildren     bool
	fragmentainerOffsetFor float64
}

// NewFieldsetLayoutAlgorithm creates the algorithm for a fieldset container.
// Mirrors FieldsetLayoutAlgorithm's constructor (fieldset_layout_algorithm.cc:
// 60-69 @ a9f50e522efa).
func NewFieldsetLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *FieldsetLayoutAlgorithm {
	fla := &FieldsetLayoutAlgorithm{
		ctx:        ctx,
		node:       node,
		style:      node.Style(),
		space:      space,
		earlyBreak: space.EarlyBreak,
	}
	if space.BreakToken != nil {
		fla.consumedBlockSize = space.BreakToken.ConsumedBlockSize.Float64()
	}
	return fla
}

// toContentOffset converts a Blink border-box-relative offset to louis14's
// content-edge-relative child offset.
func (fla *FieldsetLayoutAlgorithm) toContentOffset(inline, block float64) LogicalOffset {
	return LogicalOffset{
		InlineOffset: inline - fla.borders.InlineStart,
		BlockOffset:  block - fla.borders.BlockStart,
	}
}

// childAvailableSize is the size the legend is laid out in: what would have
// been the content box had the fieldset been a regular block. Blink's
// ChildAvailableSize() (box_fragment_builder.h:94-97 @ a9f50e522efa) is
// computed from border + padding + scrollbar, padding included.
func (fla *FieldsetLayoutAlgorithm) childAvailableSize() LogicalSize {
	return CalculateChildAvailableSize(fla.space, fla.node, fla.geom)
}

// Layout performs fieldset layout. Mirrors FieldsetLayoutAlgorithm::Layout
// (fieldset_layout_algorithm.cc:71-160 @ a9f50e522efa).
func (fla *FieldsetLayoutAlgorithm) Layout() *LayoutResult {
	wdm := fla.space.WritingDirection
	fla.geom = CalculateInitialFragmentGeometry(fla.ctx, fla.node, fla.style, wdm, fla.space)
	// The fieldset container is never a scroll container (it does not
	// respect CSS overflow), so it has no scrollbars
	// (fieldset_layout_algorithm.cc:67 DCHECKs this), and its padding takes
	// up no layout space: the anonymous content box applies it
	// (box_fragment_builder.h:85-89 @ a9f50e522efa).
	builderGeom := fla.geom
	builderGeom.Padding = LogicalEdges{}
	builderGeom.Scrollbar = LogicalEdges{}

	fla.containerBuilder = NewBoxFragmentBuilder(wdm)
	fla.containerBuilder.SetLayoutNode(fla.node)
	fla.containerBuilder.SetConstraintSpace(&fla.space)
	fla.containerBuilder.SetInitialFragmentGeometry(&builderGeom)
	fla.containerBuilder.PropagateSpaceShortage(fla.relayoutSpaceShortage)
	// LayoutAlgorithm constructor → SetupFragmentBuilderForFragmentation
	// (layout_algorithm.h:91-95 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	cloneBoxStartDecorations := SetupFragmentBuilderForFragmentation(fla.space, fla.node, fla.space.BreakToken, fla.containerBuilder)
	includeBlockStart := !fla.space.BreakToken.IsBreakInside() || cloneBoxStartDecorations
	fla.borders = builderGeom.Border
	fla.fragmentainerOffsetFor = FragmentainerOffset(fla.containerBuilder, true)
	if fla.node.IsMonolithic(fla.space.IsOrthogonalWritingModeRoot) {
		fla.containerBuilder.SetIsMonolithic(true)
	}
	fla.containerBuilder.SetChildAvailableSize(fla.childAvailableSize())

	// Layout of a fieldset container consists of two parts: a child fragment
	// for the rendered legend (if any), and one for the fieldset contents
	// anonymous box (fieldset_layout_algorithm.cc:72-82).
	if includeBlockStart {
		fla.intrinsicBlockSize = fla.borders.BlockStart
	}

	if status := fla.layoutChildren(); status == BreakStatusNeedsEarlierBreak {
		// No Abort status consumer exists in louis14's parents, so re-lay
		// out here and break at the better breakpoint, as BlockLayout-
		// Algorithm does (block_layout_algorithm.cc:617-626 @ a9f50e522efa).
		return fla.relayoutAndBreakEarlier(fla.containerBuilder.GetEarlyBreak())
	}

	// ClampIntrinsicBlockSize (fieldset_layout_algorithm.cc:98-101): size
	// containment ignores the contents.
	hasExplicitBlock := fla.geom.BorderBoxSize.BlockSize != Indefinite
	borderPaddingBlock := fla.geom.Border.BlockSum() + fla.geom.Padding.BlockSum()
	if fla.style.ShouldApplySizeContainment() && !hasExplicitBlock {
		fla.intrinsicBlockSize = borderPaddingBlock
	} else {
		fla.intrinsicBlockSize += fla.borders.BlockEnd
	}

	// ComputeBlockSizeForFragment (fieldset_layout_algorithm.cc:104-107):
	// the stitched border-box block-size, clamped by min/max.
	contentBlock := fla.intrinsicBlockSize + fla.consumedBlockSize - borderPaddingBlock
	var total float64
	if hasExplicitBlock {
		total = fla.geom.BorderBoxSize.BlockSize
	} else {
		total = fla.intrinsicBlockSize + fla.consumedBlockSize
		if maxBlock, ok := ResolveMaxBlockSizeWithIntrinsic(fla.style, wdm, fla.space, fla.geom, contentBlock); ok {
			total = min(total, maxBlock+borderPaddingBlock)
		}
		total = max(total, ResolveMinBlockSizeWithIntrinsic(fla.style, wdm, fla.space, fla.geom, contentBlock)+borderPaddingBlock)
	}
	if fla.space.ForcedMinBlockSize > total {
		total = fla.space.ForcedMinBlockSize
	}
	// The legend may eat from the available content box block size; make
	// room for it (fieldset_layout_algorithm.cc:109-118). layoutChildren
	// leaves the minimum at 0 under size containment.
	total = max(total, fla.minimumBorderBoxBlockSize)

	fragmentBlockSize := total
	var finish FinishFragmentationResult
	shouldBreakInside := fla.contentBreakToken != nil || fla.contentBrokeBefore
	if InvolvedInBlockFragmentation(fla.space) {
		finish = fla.finishFragmentation(total, shouldBreakInside)
		if finish.NeedsEarlierBreak && fla.earlyBreak == nil {
			fla.containerBuilder.PropagateSpaceShortage(finish.SpaceShortage)
			return fla.relayoutAndBreakEarlier(fla.containerBuilder.GetEarlyBreak())
		}
		if finish.DisableFragmentation {
			return fla.relayoutWithoutFragmentation()
		}
		fragmentBlockSize = finish.FragmentBlockSize
		if !finish.IncludeBlockEnd {
			builderGeom.ClearBorderScrollbarPaddingBlockEnd()
		}
	} else {
		fragmentBlockSize = total - fla.consumedBlockSize
	}

	fla.containerBuilder.SetSize(LogicalSize{
		InlineSize: fla.geom.BorderBoxSize.InlineSize,
		BlockSize:  fragmentBlockSize,
	})
	fla.containerBuilder.SetIntrinsicBlockSize(fla.intrinsicBlockSize)
	fla.containerBuilder.SetBoxData(&PhysicalBoxData{
		Margin: ToPhysicalEdges(ResolveMargins(fla.style, wdm, fla.space.AvailableSize.InlineSize.Float64()), wdm),
		Border: ToPhysicalEdges(builderGeom.Border, wdm),
	})

	// HandleOofsAndSpecialDescendants (fieldset_layout_algorithm.cc:147):
	// the fieldset is the containing block of a legend's out-of-flow
	// descendants (the content box contains the content's own).
	propagatedOOF := resolveOutOfFlowCandidates(fla.ctx, fla.containerBuilder, fla.space, builderGeom,
		LogicalSize{
			InlineSize: max(0, fla.geom.BorderBoxSize.InlineSize-builderGeom.InlineBorderPadding()),
			BlockSize:  max(0, fragmentBlockSize-builderGeom.BlockBorderPadding()),
		}, previousConsumedContentBlockSize(fla.space, fla.geom.Border.BlockStart))

	result := fla.containerBuilder.Build()
	result.Fragment.IsFieldsetContainer = true
	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	if InvolvedInBlockFragmentation(fla.space) && (shouldBreakInside || finish.DidBreakSelf || finish.MonolithicOverflow > 0) {
		var childTokens []*BlockBreakToken
		if fla.contentBreakToken != nil {
			childTokens = []*BlockBreakToken{fla.contentBreakToken}
		}
		consumed := finish.ConsumedBlockSize
		if !fla.space.HasBlockFragmentation {
			consumed = fla.consumedBlockSize + fragmentBlockSize
		}
		result.BreakToken = &BlockBreakToken{
			Node:               fla.node,
			ConsumedBlockSize:  layoutunit.FromFloat64Round(consumed),
			SequenceNumber:     sequenceNumberAfter(fla.space.BreakToken),
			ChildBreakTokens:   childTokens,
			HasSeenAllChildren: fla.hasSeenAllChildren,
			IsAtBlockEnd:       finish.IsAtBlockEnd,
			MonolithicOverflow: finish.MonolithicOverflow,
			FieldsetData:       &FieldsetBreakTokenData{LegendBlockSizeContribution: fla.legendBlockSizeContribution},
		}
		if finish.DidBreakSelf {
			result.DidBreakSelf = true
			result.MinSpaceShortage = MinPositiveSpaceShortage(result.MinSpaceShortage, finish.SpaceShortage)
		}
	}
	fla.containerBuilder.AdjustSpaceShortageForBreakAvoidRoot(fla.ctx, result)
	if result.BreakToken == nil && fla.space.HasBlockFragmentation {
		result.EarlyBreak = fla.containerBuilder.GetEarlyBreak()
	}
	return result
}

// finishFragmentation sizes this fragment of the fieldset. Mirrors the
// FinishFragmentation call of fieldset_layout_algorithm.cc:128-145 @
// a9f50e522efa, with the inputs BlockLayoutAlgorithm passes.
func (fla *FieldsetLayoutAlgorithm) finishFragmentation(fragmentsTotal float64, shouldBreakInside bool) FinishFragmentationResult {
	spaceLeft := Indefinite
	if !fla.space.IsInitialColumnBalancingPass {
		spaceLeft = FragmentainerSpaceLeft(fla.containerBuilder, false)
	}
	return FinishFragmentation(FinishFragmentationInput{
		SpaceLeft:                    spaceLeft,
		FragmentainerCapacity:        FragmentainerCapacity(fla.containerBuilder, false),
		HasBlockFragmentation:        fla.space.HasBlockFragmentation,
		IsColumnFragmentation:        fla.space.BlockFragmentationType == FragmentColumn,
		IsInitialColumnBalancingPass: fla.space.IsInitialColumnBalancingPass,
		PreviousBreakToken:           fla.space.BreakToken,
		FragmentsTotalBlockSize:      fragmentsTotal,
		IntrinsicBlockSize:           fla.intrinsicBlockSize,
		TrailingBorderPadding:        fla.borders.BlockEnd,
		BlockStartBorderPadding:      fla.geom.Border.BlockStart,
		ShouldBreakInside:            shouldBreakInside,
		HasInflowChildBreakInside:    fla.contentBreakToken.IsBreakInside(),
		HasEarlyBreak:                fla.containerBuilder.HasEarlyBreak(),
	})
}

// layoutChildren lays out the rendered legend and the fieldset content box.
// Mirrors FieldsetLayoutAlgorithm::LayoutChildren
// (fieldset_layout_algorithm.cc:162-238 @ a9f50e522efa).
func (fla *FieldsetLayoutAlgorithm) layoutChildren() BreakStatus {
	incoming := fla.space.BreakToken
	contentBreakToken := incoming.InputBreakToken()
	fla.hasSeenAllChildren = incoming != nil && incoming.HasSeenAllChildren

	adjustedPaddingBoxSize := LogicalSize{
		InlineSize: max(0, fla.geom.BorderBoxSize.InlineSize-fla.borders.InlineSum()),
		BlockSize:  Indefinite,
	}
	if fla.geom.BorderBoxSize.BlockSize != Indefinite {
		adjustedPaddingBoxSize.BlockSize = max(0, fla.geom.BorderBoxSize.BlockSize-fla.geom.Border.BlockSum())
	}

	legend := fla.node.GetRenderedLegend()
	if legend != nil {
		var legendSizeContribution float64
		if incoming.IsBreakInside() {
			if incoming.FieldsetData != nil {
				legendSizeContribution = incoming.FieldsetData.LegendBlockSizeContribution
			}
		} else {
			fla.layoutLegend(legend)
			// The first fragment: intrinsicBlockSize is at the outer
			// block-end edge of the legend or just after the block-start
			// border, whichever is larger.
			legendSizeContribution = fla.intrinsicBlockSize - fla.borders.BlockStart
		}
		fla.legendBlockSizeContribution = legendSizeContribution

		if adjustedPaddingBoxSize.BlockSize != Indefinite {
			adjustedPaddingBoxSize.BlockSize = max(adjustedPaddingBoxSize.BlockSize-legendSizeContribution,
				fla.geom.Padding.BlockSum())
		}
		// The legend may eat from the available content box block size.
		if !fla.style.ShouldApplySizeContainment() {
			fla.minimumBorderBoxBlockSize = fla.geom.Border.BlockSum() +
				fla.geom.Padding.BlockSum() + legendSizeContribution
		}
	}

	// Proceed with normal fieldset children (excluding the rendered
	// legend). They all live inside the anonymous content box.
	if contentBreakToken != nil || !fla.hasSeenAllChildren {
		if content := fla.node.GetFieldsetContent(); content != nil {
			return fla.layoutFieldsetContent(content, contentBreakToken, adjustedPaddingBoxSize)
		}
	}
	return BreakStatusContinue
}

// layoutLegend lays out the rendered legend and places it over the
// block-start border. Mirrors FieldsetLayoutAlgorithm::LayoutLegend
// (fieldset_layout_algorithm.cc:240-314 @ a9f50e522efa).
func (fla *FieldsetLayoutAlgorithm) layoutLegend(legend *LayoutInputNode) {
	wdm := fla.space.WritingDirection
	available := fla.childAvailableSize()
	legendMargins := ResolveMargins(legend.Style(), wdm, available.InlineSize)

	legendSpace := fla.createConstraintSpaceForLegend(legend, available)
	result := layoutElement(fla.ctx, legend, legendSpace)
	logical := NewLogicalFragment(wdm, result.Fragment)
	legendBorderBoxBlockSize := logical.BlockSize()
	legendMarginBoxBlockSize := legendMargins.BlockStart + legendBorderBoxBlockSize + legendMargins.BlockEnd

	// The legend's border box is centered over the block-start border when
	// it is thinner than the border (HTML §15.3.13).
	var blockOffset float64
	if spaceLeft := fla.borders.BlockStart - legendBorderBoxBlockSize; spaceLeft > 0 {
		blockOffset += spaceLeft / 2
	}
	// A legend margin box taller than the border sets the content start.
	if legendMarginEndOffset := blockOffset + legendMarginBoxBlockSize - legendMargins.BlockStart; legendMarginEndOffset > fla.borders.BlockStart {
		fla.intrinsicBlockSize = legendMarginEndOffset
	}

	// Padding is ignored by the fieldset container, but the legend honors
	// it; scrollbars never occur at the inline-start.
	legendInlineStart := fla.borders.InlineStart + fla.geom.Padding.InlineStart + legendMargins.InlineStart
	if availableSpace := available.InlineSize - logical.InlineSize(); availableSpace > 0 {
		switch computeLegendBlockAlignment(legend.Style(), wdm) {
		case legendBlockAlignmentCenter:
			legendInlineStart += availableSpace / 2
		case legendBlockAlignmentEnd:
			legendInlineStart += availableSpace - legendMargins.InlineEnd
		}
	}

	fla.addChildResult(legend, result, legendInlineStart, blockOffset)
}

// addChildResult adds a child layout result at a Blink border-box-relative
// offset, adopting its out-of-flow candidates (Blink AddResult).
func (fla *FieldsetLayoutAlgorithm) addChildResult(child *LayoutInputNode, result *LayoutResult, inline, block float64) {
	offset := fla.toContentOffset(inline, block)
	relativeOffset := fla.containerBuilder.relativeOffsetFor(result.Fragment)
	fla.containerBuilder.AddChildWithRelativeOffset(result.Fragment, offset, relativeOffset)
	fla.containerBuilder.PropagateOOFPositionedInfo(result, child.Style(), offset, relativeOffset)
}

// layoutFieldsetContent lays out the anonymous fieldset content box below the
// legend. Mirrors FieldsetLayoutAlgorithm::LayoutFieldsetContent
// (fieldset_layout_algorithm.cc:316-428 @ a9f50e522efa).
func (fla *FieldsetLayoutAlgorithm) layoutFieldsetContent(content *LayoutInputNode, contentBreakToken *BlockBreakToken, adjustedPaddingBoxSize LogicalSize) BreakStatus {
	wdm := fla.space.WritingDirection
	var earlyBreakInChild *EarlyBreak
	if fla.earlyBreak != nil {
		if IsEarlyBreakTarget(fla.earlyBreak, fla.containerBuilder, content) {
			fla.contentBrokeBefore = true
			fla.contentBreakToken = CreateBreakBefore(content, false)
			fla.containerBuilder.SetBreakAppeal(BreakAppealPerfect)
			fla.consumeRemainingFragmentainerSpace()
			return BreakStatusContinue
		}
		earlyBreakInChild = EnterEarlyBreakInChild(content, fla.earlyBreak)
	}

	var result *LayoutResult
	isPastEnd := fla.space.BreakToken != nil && fla.space.BreakToken.IsAtBlockEnd

	// With an indefinite block-size, the content is limited by
	// max-block-size (fieldset_layout_algorithm.cc:339-393).
	maxContentBlockSize := Indefinite
	if adjustedPaddingBoxSize.BlockSize == Indefinite {
		if maxBlock, ok := ResolveMaxBlockSize(fla.style, wdm, fla.space, fla.geom); ok {
			maxContentBlockSize = maxBlock.Float64() + fla.geom.Border.BlockSum() + fla.geom.Padding.BlockSum()
		}
	}
	if !isPastEnd || maxContentBlockSize == Indefinite {
		space := fla.createConstraintSpaceForFieldsetContent(content, adjustedPaddingBoxSize, contentBreakToken, earlyBreakInChild)
		result = layoutElement(fla.ctx, content, space)
	}
	if maxContentBlockSize != Indefinite {
		if maxContentBlockSize > fla.geom.Padding.BlockSum() {
			maxContentBlockSize = max(maxContentBlockSize-(fla.intrinsicBlockSize+fla.borders.BlockEnd), fla.geom.Padding.BlockSum())
		}
		if result != nil {
			total := NewLogicalFragment(wdm, result.Fragment).BlockSize()
			if contentBreakToken != nil {
				total += contentBreakToken.ConsumedBlockSize.Float64()
			}
			if total >= maxContentBlockSize {
				result = nil
			}
		}
		if result == nil {
			adjustedPaddingBoxSize.BlockSize = maxContentBlockSize
			space := fla.createConstraintSpaceForFieldsetContent(content, adjustedPaddingBoxSize, contentBreakToken, earlyBreakInChild)
			result = layoutElement(fla.ctx, content, space)
		}
	}

	status := BreakStatusContinue
	if fla.space.HasBlockFragmentation && fla.earlyBreak == nil {
		status, _ = BreakBeforeChildIfNeeded(fla.space, content, result,
			fla.fragmentainerOffsetFor+fla.intrinsicBlockSize,
			FragmentainerCapacity(fla.containerBuilder, true), false, fla.containerBuilder)
	}

	switch status {
	case BreakStatusContinue:
		fla.addChildResult(content, result, fla.borders.InlineStart, fla.intrinsicBlockSize)
		offsetBlock := fla.intrinsicBlockSize
		if result.HasBaseline {
			fla.containerBuilder.SetBaseline(offsetBlock + result.Baseline)
		}
		if result.LastBaseline != 0 || result.HasBaseline {
			fla.containerBuilder.SetLastBaseline(offsetBlock + result.LastBaseline)
		}
		if result.UseLastBaselineForInlineBaseline {
			fla.containerBuilder.SetUseLastBaselineForInlineBaseline()
		}
		if fla.space.ShouldPropagateChildBreakValues() {
			fla.containerBuilder.PropagateChildBreakValues(result)
		}
		fla.intrinsicBlockSize += NewLogicalFragment(wdm, result.Fragment).BlockSize()
		fla.contentBreakToken = result.BreakToken
		if result.BreakToken != nil {
			fla.containerBuilder.PropagateSpaceShortage(result.MinSpaceShortage)
		}
		fla.hasSeenAllChildren = true
	case BreakStatusBrokeBefore:
		fla.contentBrokeBefore = true
		fla.contentBreakToken = CreateBreakBefore(content, false)
		fla.consumeRemainingFragmentainerSpace()
	}
	return status
}

// consumeRemainingFragmentainerSpace makes this fragment occupy the rest of
// the fragmentainer after a break before the content. Mirrors
// FieldsetLayoutAlgorithm::ConsumeRemainingFragmentainerSpace and
// FragmentainerSpaceAvailable (fieldset_layout_algorithm.cc:430-443 @
// a9f50e522efa).
func (fla *FieldsetLayoutAlgorithm) consumeRemainingFragmentainerSpace() {
	if fla.space.HasKnownFragmentainerBlockSize() {
		fla.intrinsicBlockSize += max(0, FragmentainerSpaceLeft(fla.containerBuilder, true)-fla.intrinsicBlockSize)
	}
}

// createConstraintSpaceForLegend builds the legend's space: a new formatting
// context whose auto inline-size shrinks to fit (the ConstraintSpaceBuilder
// default). Mirrors CreateConstraintSpaceForLegend
// (fieldset_layout_algorithm.cc:504-516 @ a9f50e522efa). The legend is laid
// out without fragmentation (legends are monolithic, :254-255).
func (fla *FieldsetLayoutAlgorithm) createConstraintSpaceForLegend(legend *LayoutInputNode, available LogicalSize) ConstraintSpace {
	wdm := fla.space.WritingDirection
	legendWDM := NewWritingDirectionMode(legend.Style())
	return NewConstraintSpaceBuilder(wdm, legendWDM, true).
		SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(legendWDM, fla.ctx)).
		SetAvailableSize(available).
		SetPercentageResolutionSize(available).
		SetPercentageResolutionInlineSize(available.InlineSize).
		Build()
}

// createConstraintSpaceForFieldsetContent builds the content box's space:
// the padding box, stretched, resolving percentages as the fieldset does.
// Mirrors CreateConstraintSpaceForFieldsetContent
// (fieldset_layout_algorithm.cc:518-547 @ a9f50e522efa).
func (fla *FieldsetLayoutAlgorithm) createConstraintSpaceForFieldsetContent(content *LayoutInputNode, paddingBoxSize LogicalSize, breakToken *BlockBreakToken, earlyBreak *EarlyBreak) ConstraintSpace {
	wdm := fla.space.WritingDirection
	contentWDM := NewWritingDirectionMode(content.Style())
	b := NewConstraintSpaceBuilder(wdm, contentWDM, true).
		SetAvailableSize(paddingBoxSize).
		SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: fla.space.PercentageResolutionSize.InlineSize.Float64(),
			BlockSize:  fla.space.PercentageResolutionSize.BlockSize.Float64(),
		}).
		SetPercentageResolutionInlineSize(fla.space.PercentageResolutionSize.InlineSize.Float64()).
		SetIsFixedBlockSize(paddingBoxSize.BlockSize != Indefinite).
		SetBreakToken(breakToken).
		SetEarlyBreak(earlyBreak)
	if fla.space.HasBlockFragmentation && !content.IsMonolithic(wdm.WM != contentWDM.WM) {
		SetupSpaceBuilderForFragmentationFromBuilder(fla.containerBuilder,
			fla.fragmentainerOffsetFor+fla.intrinsicBlockSize,
			FragmentainerCapacity(fla.containerBuilder, true), b)
		b.SetColumnSpannerDescendantsBlocked(true)
	}
	return b.Build()
}

// relayoutAndBreakEarlier lays the fieldset out again, breaking at
// earlyBreak. Mirrors LayoutAlgorithm::RelayoutAndBreakEarlier
// (layout_algorithm.h:272-281 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (fla *FieldsetLayoutAlgorithm) relayoutAndBreakEarlier(earlyBreak *EarlyBreak) *LayoutResult {
	space := fla.space
	space.EarlyBreak = earlyBreak
	relayout := NewFieldsetLayoutAlgorithm(fla.ctx, fla.node, space)
	relayout.relayoutSpaceShortage = fla.containerBuilder.minimalSpaceShortage
	return relayout.Layout()
}

// relayoutWithoutFragmentation lays the fieldset out again with block
// fragmentation disabled. Mirrors LayoutAlgorithm::
// RelayoutWithoutFragmentation (layout_algorithm.h:283-291 @ a9f50e522efa).
func (fla *FieldsetLayoutAlgorithm) relayoutWithoutFragmentation() *LayoutResult {
	return NewFieldsetLayoutAlgorithm(fla.ctx, fla.node, fla.space.CloneWithoutFragmentation()).Layout()
}

// measureFieldsetMinMax is the fieldset container's content-box min/max
// inline sizes (callers add the fieldset's border and padding). Mirrors
// FieldsetLayoutAlgorithm::ComputeMinMaxSizes (fieldset_layout_algorithm.cc:
// 445-502 @ a9f50e522efa): the legend's margin-box contribution, encompassed
// with the content box's, which includes the fieldset padding — so in the
// content-box convention the content contribution sheds that padding.
func measureFieldsetMinMax(node *LayoutInputNode, ctx *LayoutContext, space ConstraintSpace) MinMaxSizes {
	var result MinMaxSizes
	nodeBlockSize := resolveNodeBlockSizeForPercent(node, space)
	if legend := node.GetRenderedLegend(); legend != nil {
		result.MinContent, result.MaxContent = childInlineContribution(ctx, legend, space, nodeBlockSize, false)
	}
	if content := node.GetFieldsetContent(); content != nil {
		paddingInline := ComputeFragmentGeometry(node.Style(), space.WritingDirection).Padding
		minC, maxC := childInlineContribution(ctx, content, space, nodeBlockSize, false)
		pad := paddingInline.InlineStart + paddingInline.InlineEnd
		result.MinContent = max(result.MinContent, minC-pad)
		result.MaxContent = max(result.MaxContent, maxC-pad)
	}
	return result
}
