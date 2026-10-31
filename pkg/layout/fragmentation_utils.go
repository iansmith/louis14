package layout

import (
	"math"

	"louis14/pkg/css"
)

// fragmentation_utils.go — Blink-parity port of forced-break + break-appeal
// dispatch. Mirrors third_party/blink/renderer/core/layout/fragmentation_utils.{h,cc}.
//
// Phase 12d scope:
//   - BreakBeforeChildIfNeeded (the master hook)
//   - CalculateBreakBetweenValue (joins prev break-after + child break-before)
//   - CalculateBreakAppealBefore + CalculateBreakAppealInside
//   - MovePastBreakpoint (the fit / break-before decision BreakBeforeChildIfNeeded
//     delegates to)
//   - BreakBeforeChild + AttemptSoftBreak helpers
//
// Out of scope (deferred):
//   - EarlyBreak storage / RelayoutAndBreakEarlier retry → Phase 12g
//   - Paginated PageName interaction → not multicol
//   - FlexColumnBreakInfo per-column flex info → not multicol
//   - The soft-break half of LayoutResult.InitialBreakBefore /
//     FinalBreakAfter propagation: Blink's BreakBeforeChild propagates the
//     pushed child's break values when the break is not forced
//     (fragmentation_utils.cc:970-972 @ a9f50e522efa); louis14 does not
//     (ledger L37). Propagation from added children is ported
//     (BoxFragmentBuilder.PropagateChildBreakValues, LOU-400).
//   - Flex/grid row-item break propagation → not multicol

// ShouldAvoidBreakInside returns true if the layout result should not be
// broken across fragmentainers — either because its fragment is monolithic
// or because its style says break-inside:avoid (or avoid-column / avoid-page,
// matching the current fragmentation context).
//
// Mirrors Blink's ShouldAvoidBreakInside (fragmentation_utils.h):
//
//	return result.GetPhysicalFragment().IsMonolithic() ||
//	       IsAvoidBreakValue(space, ResolvedBreakInside(result));
//
// The IsMonolithic clause was added in v2 B2.5. PhysicalFragment.IsMonolithic
// is set by:
//   - block_layout.go BLA Layout entry when style.HasSizeContainment()
//     (CSS Containment 2 §2.6).
//   - (Future) replaced elements (img, video, canvas, iframe).
func ShouldAvoidBreakInside(space ConstraintSpace, layoutResult *LayoutResult) bool {
	if layoutResult == nil || layoutResult.Fragment == nil {
		return false
	}
	if layoutResult.Fragment.IsMonolithic {
		return true
	}
	breakInside := "auto"
	if s := layoutResult.Fragment.Style; s != nil {
		breakInside = s.GetBreakInside()
	}
	return IsAvoidBreakValue(space, breakInside)
}

// CalculateUnbreakableBlockSize returns the block-size that this layout
// result contributes as an unbreakable floor for column-balancing.
//
// Mirrors Blink's CalculateUnbreakableBlockSize
// (fragmentation_utils.cc:979-993 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - The contribution is the child's full border-box block-size, per
//     Blink's BlockSizeForFragmentation free function (:1545-1583): the
//     result's explicit BlockSizeForFragmentation when set, else the
//     fragment's own size. There is deliberately NO short-circuit on the
//     child result's accumulated TallestUnbreakableBlockSize — that
//     carrier flows up separately (block_layout.go, Blink
//     box_fragment_builder.cc:566-569) and
//     PropagateTallestUnbreakableBlockSize keeps the max. (LOU-365: the
//     previous short-circuit made a break-inside:avoid box with padding
//     report only its accumulated descendant/edge floors instead of its
//     full size — multicol-overflow-clip-auto-sized.)
//   - The fragmentainer offset is added ONLY when negative ("whatever is
//     before the block-start of the fragmentainer isn't considered to
//     intersect with the fragmentainer, so subtract it"). A POSITIVE
//     offset must not be added: a break before the child can always
//     place it at the top of a fresh column, so the column only needs
//     to be as tall as the child itself.
//
// A monolithic box records no BlockSizeForFragmentation (it is laid out
// without block fragmentation), so its border box counts, not its
// overflowing content.
//
// Used at the BreakBeforeChildIfNeeded propagation site
// (fragmentation_utils.cc:1105-1113).
func CalculateUnbreakableBlockSize(
	space ConstraintSpace,
	layoutResult *LayoutResult,
	fragmentainerBlockOffset float64,
) float64 {
	if layoutResult == nil || layoutResult.Fragment == nil {
		return 0
	}
	blockSize := BlockSizeForFragmentation(space.WritingDirection, layoutResult)
	if fragmentainerBlockOffset < 0 {
		blockSize += fragmentainerBlockOffset
	}
	return blockSize
}

// IsParallelFlowContinuation reports whether an outgoing child break token
// represents a parallel flow: the child's own box is at block-end (or
// explicitly tagged in-parallel-flow) while content inside it resumes in
// later fragmentainers. Such a token does NOT mean the fragmentainer is
// full — mirrors the `!break_token->IsAtBlockEnd()` escape in Blink's
// MovePastBreakpoint (fragmentation_utils.cc:1178-1186 @ a9f50e522efa).
func IsParallelFlowContinuation(tok *BlockBreakToken) bool {
	return tok != nil && (tok.IsAtBlockEnd || tok.IsInParallelFlow)
}

// IsInParallelFlow reports whether a child result's break continues in a
// parallel flow: the child is at its block end (or explicitly in a parallel
// flow) and does not contain a column spanner, which forces its breaks into
// the parent's flow. Mirrors child_is_in_same_flow in Blink's
// BoxFragmentBuilder::PropagateBreakInfo (box_fragment_builder.cc:503-510 @
// a9f50e522efa).
func IsInParallelFlow(childResult *LayoutResult) bool {
	return IsParallelFlowContinuation(childResult.BreakToken) &&
		!childResult.ShouldForceSameFragmentationFlow
}

// AdjustMarginsForFragmentation truncates the block margins of a child that
// resumes with breakToken. A leading margin belongs in front of the first
// fragment only, and is truncated after an unforced break too (floats never
// truncate); a trailing margin was already applied in the fragmentainer
// where the block ended when the token is past its block end (a parallel
// flow). Mirrors Blink's AdjustMarginsForFragmentation
// (fragmentation_utils.h:279-296 @ a9f50e522efa).
func AdjustMarginsForFragmentation(breakToken *BlockBreakToken, margins *LogicalEdges) {
	if breakToken == nil {
		return
	}
	isFloating := func() bool {
		return breakToken.Node != nil && breakToken.Node.Style() != nil &&
			breakToken.Node.Style().GetFloat() != css.FloatNone
	}
	if !breakToken.IsBreakBefore || (!breakToken.IsForcedBreak && !isFloating()) {
		margins.BlockStart = 0
	}
	if breakToken.IsAtBlockEnd {
		margins.BlockEnd = 0
	}
}

// ShouldCloneBlockStartBorderPadding reports whether this fragment repeats
// its block-start border/padding: box-decoration-break:clone, unless the
// fragment is past the box's block end. Mirrors Blink's
// ShouldCloneBlockStartBorderPadding (fragmentation_utils.cc:61-78 @
// a9f50e522efa); its MonolithicOverflow clause is omitted because louis14
// never sets BlockBreakToken.MonolithicOverflow.
func ShouldCloneBlockStartBorderPadding(hasClonedBoxDecorations bool, previousBreakToken *BlockBreakToken) bool {
	if !hasClonedBoxDecorations {
		return false
	}
	return previousBreakToken == nil || !previousBreakToken.IsAtBlockEnd
}

// HasBreakOpportunityBeforeNextChild reports whether placing a block child
// leaves a class A breakpoint before its next sibling, giving that sibling
// container separation: it does unless the child resumed at its block end,
// i.e. sits in a parallel flow that must not affect this flow. Mirrors the
// box-fragment branch of Blink's HasBreakOpportunityBeforeNextChild
// (fragmentation_utils.cc:870-883 @ a9f50e522efa); the line-box branch
// (:885-892) has no louis14 caller, since inline content is placed as one
// unit rather than line by line.
func HasBreakOpportunityBeforeNextChild(incomingChildBreakToken *BlockBreakToken) bool {
	return incomingChildBreakToken == nil || !incomingChildBreakToken.IsAtBlockEnd
}

// ShouldIncludeBlockStartBorderPadding reports whether this fragment carries
// its block-start border/padding: the first fragment does, a resumed one
// only when cloned. Mirrors Blink fragmentation_utils.cc:522-525 @
// a9f50e522efa.
func ShouldIncludeBlockStartBorderPadding(hasClonedBoxDecorations bool, previousBreakToken *BlockBreakToken) bool {
	return !previousBreakToken.IsBreakInside() ||
		ShouldCloneBlockStartBorderPadding(hasClonedBoxDecorations, previousBreakToken)
}

// ShouldIncludeBlockEndBorderPadding reports whether this fragment carries
// its block-end border/padding: not past the block end; always when it does
// not break, is known to fit, or clones its end decorations; never when a
// new formatting context breaks; otherwise only when no in-flow child broke
// inside it. Mirrors Blink fragmentation_utils.cc:527-545 @ a9f50e522efa.
func ShouldIncludeBlockEndBorderPadding(previousBreakToken *BlockBreakToken, shouldBreakInside, isKnownToFitInFragmentainer, shouldCloneBoxEndDecorations, isNewFormattingContext, hasInflowChildBreakInside bool) bool {
	if previousBreakToken != nil && previousBreakToken.IsAtBlockEnd {
		return false
	}
	if !shouldBreakInside || isKnownToFitInFragmentainer || shouldCloneBoxEndDecorations {
		return true
	}
	if isNewFormattingContext {
		return false
	}
	return !hasInflowChildBreakInside
}

// FragmentainerBlockSizeForFragmentation returns how far a fragmentainer's
// (column's) content reaches: the furthest block-end of its in-flow
// children, each contributing its own border-box block-size or, when it
// does not clip block-axis overflow, the block-end of its own content if
// that is further — walked down the fragment tree, through monolithic boxes
// too. no Blink analog: it stands in for the column's
// BlockEndScrollableOverflow() in LayoutLine's regrow
// (column_layout_algorithm.cc:1043-1046 @ a9f50e522efa), which louis14 does
// not compute. Unlike the column result's BlockSizeForFragmentation
// (box_fragment_builder.cc:743-761), it includes a monolithic box's
// overflowing content, as scrollable overflow does.
func FragmentainerBlockSizeForFragmentation(wdm WritingDirectionMode, fragmentainer *PhysicalFragment) float64 {
	if fragmentainer == nil {
		return 0
	}
	conv := NewConverter(wdm, geomSizeToOld(fragmentainer.Size))
	extent := 0.0
	for _, child := range fragmentainer.Children {
		if child.Fragment == nil || isOutOfFlowFragment(child.Fragment) {
			continue
		}
		physOffset := PhysicalOffset{X: child.Offset.Left.Float64(), Y: child.Offset.Top.Float64()}
		offset := conv.ToLogicalOffset(physOffset, geomSizeToOld(child.Fragment.Size)).BlockOffset
		extent = math.Max(extent, offset+blockSizeForFragmentationOfFragment(wdm, child.Fragment))
	}
	return extent
}

// blockSizeForFragmentationOfFragment is a non-fragmentainer box's block-size
// for fragmentation: its border-box block-size, extended by its children's
// unless it clips block-axis overflow (box_fragment_builder.cc:746-761 @
// a9f50e522efa).
func blockSizeForFragmentationOfFragment(wdm WritingDirectionMode, frag *PhysicalFragment) float64 {
	own := NewLogicalFragment(wdm, frag).BlockSize()
	// A line box contributes its own block-size; the glyph boxes of its
	// text may overhang it (box_fragment_builder.cc:484-495 propagates a
	// line box's own size).
	if frag.Type != FragmentBox || clipsBlockAxisOverflow(frag.Style, wdm) {
		return own
	}
	return math.Max(own, FragmentainerBlockSizeForFragmentation(wdm, frag))
}

// clipsBlockAxisOverflow reports whether style clips overflow along the block
// axis of wdm (Blink BlockNode::GetOverflowClipAxes & block_axis).
func clipsBlockAxisOverflow(style *css.Style, wdm WritingDirectionMode) bool {
	if style == nil {
		return false
	}
	overflow := style.GetOverflowY()
	if wdm.WM != WritingModeHorizontalTB {
		overflow = style.GetOverflowX()
	}
	return overflow != css.OverflowVisible
}

// isOutOfFlowFragment reports whether frag is an absolutely or fixed
// positioned box, which does not contribute to its container's in-flow
// block-size for fragmentation.
func isOutOfFlowFragment(frag *PhysicalFragment) bool {
	if frag.Style == nil {
		return false
	}
	pos := frag.Style.GetPosition()
	return pos == css.PositionAbsolute || pos == css.PositionFixed
}

// MinPositiveSpaceShortage merges two space-shortage reports, keeping the
// smallest positive value. Non-positive inputs mean "no shortage reported"
// and the result is clamped to zero so a negative operand never leaks
// through (CodeRabbit 3514362242). Mirrors
// BoxFragmentBuilder::PropagateSpaceShortage's min-merge semantics
// (box_fragment_builder.h @ a9f50e522efa).
func MinPositiveSpaceShortage(a, b float64) float64 {
	if a <= 0 && b <= 0 {
		return 0
	}
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

// CalculateBreakBetweenValue returns the effective break-between value for the
// boundary BEFORE this child: the parent's previous break-after (propagated
// when the previous in-flow child was added) joined with the child's
// break-before and the break-before the child's layout propagated up from
// its first descendant (LayoutResult.InitialBreakBefore). Mirrors Blink's
// CalculateBreakBetweenValue (fragmentation_utils.cc:131-160 @
// a9f50e522efa).
//
// Blink returns auto for a child whose fragment is not first-for-node
// (:143-153); here both callers (BlockLayoutAlgorithm and the multicol
// spanner site) only reach BreakBeforeChildIfNeeded for a first-for-node
// child. Inline children and paginated page names are not ported.
func CalculateBreakBetweenValue(child *LayoutInputNode, layoutResult *LayoutResult, builder *BoxFragmentBuilder) string {
	childBreakBefore := "auto"
	if s := child.Style(); s != nil {
		childBreakBefore = s.GetBreakBefore()
	}
	if layoutResult != nil {
		childBreakBefore = JoinFragmentainerBreakValues(childBreakBefore, layoutResult.InitialBreakBefore)
	}
	return builder.JoinedBreakBetweenValue(childBreakBefore)
}

// CalculateBreakAppealBefore scores the appeal of breaking before the given
// child. Mirrors Blink's child overload of CalculateBreakAppealBefore
// (fragmentation_utils.cc:222-233 @ a9f50e522efa): it derives the
// break-between value and whether the start of a resumed container is
// breakable, then defers to CalculateBreakAppealBeforeWithStatus.
// hasContainerSeparation: there is at least one prior in-flow child in this
// container (so a break before this child is a real boundary, not the
// start-of-container).
func CalculateBreakAppealBefore(
	space ConstraintSpace,
	child *LayoutInputNode,
	layoutResult *LayoutResult,
	builder *BoxFragmentBuilder,
	hasContainerSeparation bool,
) BreakAppeal {
	status := LayoutResultSuccess
	if layoutResult != nil {
		status = layoutResult.Status
	}
	// Result overload of IsBreakableAtStartOfResumedContainer
	// (fragmentation_utils.cc:198-212 @ a9f50e522efa): an aborted result is
	// never breakable at the start. Callers only pass first-for-node
	// children.
	breakableAtStartOfContainer := status == LayoutResultSuccess &&
		IsBreakableAtStartOfResumedContainer(space)
	breakBetween := CalculateBreakBetweenValue(child, layoutResult, builder)
	return CalculateBreakAppealBeforeWithStatus(
		space, status, breakBetween, hasContainerSeparation, breakableAtStartOfContainer)
}

// CalculateBreakAppealBeforeWithStatus is Blink's status overload of
// CalculateBreakAppealBefore (fragmentation_utils.cc:235-266 @
// a9f50e522efa); Go has no overloading. layoutResultStatus is
// LayoutResultSuccess or LayoutResultOutOfFragmentainerSpace.
// breakableAtStartOfContainer: a break before the first child of a resumed
// container is allowed (IsBreakableAtStartOfResumedContainer); the appeal
// then floors at the space's MinBreakAppeal instead of LastResort.
func CalculateBreakAppealBeforeWithStatus(
	space ConstraintSpace,
	layoutResultStatus EStatus,
	breakBetween string,
	hasContainerSeparation bool,
	breakableAtStartOfContainer bool,
) BreakAppeal {
	appeal := BreakAppealPerfect
	if !hasContainerSeparation {
		if !breakableAtStartOfContainer {
			return BreakAppealLastResort
		}
		appeal = space.MinBreakAppeal
	}
	if IsAvoidBreakValue(space, breakBetween) {
		if appeal > BreakAppealViolatingBreakAvoid {
			appeal = BreakAppealViolatingBreakAvoid
		}
	}
	return appeal
}

// CalculateBreakAppealInside returns the appeal of breaking INSIDE the given
// child's laid-out result, considering its break-inside style. Mirrors Blink's
// CalculateBreakAppealInside (fragmentation_utils.cc).
//
// We don't yet plumb LayoutResult.HasForcedBreak through every algorithm, so
// we use a simple proxy: if the child's result has a non-nil BreakToken (it
// fragmented internally), check break-inside:avoid.
func CalculateBreakAppealInside(space ConstraintSpace, layoutResult *LayoutResult) BreakAppeal {
	if layoutResult == nil {
		return BreakAppealPerfect
	}
	if layoutResult.HasForcedBreak {
		return BreakAppealPerfect
	}
	appeal := layoutResult.BreakAppeal
	considerInsideAvoid := layoutResult.BreakToken != nil &&
		!layoutResult.BreakToken.IsBreakBefore
	if considerInsideAvoid && appeal > BreakAppealViolatingBreakAvoid {
		breakInside := "auto"
		if f := layoutResult.Fragment; f != nil && f.Style != nil {
			breakInside = f.Style.GetBreakInside()
		}
		if IsAvoidBreakValue(space, breakInside) {
			appeal = BreakAppealViolatingBreakAvoid
		}
	}
	return appeal
}

// SetupSpaceBuilderForFragmentation copies a parent's block-fragmentation
// context into a child's constraint-space builder: the fragmentainer
// block-size and the child's offset in the fragmentainer (with
// IsAtFragmentainerStart for an offset at or before its start), the
// fragmentation type, the column-balancing flags and MinBreakAppeal, so a
// container that is breakable at the start of a resumed fragment stays
// breakable through its descendants. Mirrors Blink's
// SetupSpaceBuilderForFragmentation (fragmentation_utils.cc:321-372 @
// a9f50e522efa). The monolithic-child early return (:336-338) stays with the
// callers, which then leave fragmentation off. Not ported (no louis14 field
// or consumer): RequiresContentBeforeBreaking, IsInsideBreakAvoid,
// ShouldIgnoreForcedBreaks and PageName.
func SetupSpaceBuilderForFragmentation(parentSpace ConstraintSpace, fragmentainerOffset, fragmentainerBlockSize float64, b *ConstraintSpaceBuilder) {
	b.SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(fragmentainerBlockSize).
		SetFragmentainerOffset(fragmentainerOffset).
		SetBlockFragmentationType(parentSpace.BlockFragmentationType).
		SetIsInitialColumnBalancingPass(parentSpace.IsInitialColumnBalancingPass).
		SetIsInsideBalancedColumns(parentSpace.IsInsideBalancedColumns).
		SetMinBreakAppeal(parentSpace.MinBreakAppeal).
		SetIsAtFragmentainerStart(fragmentainerOffset <= 0)
}

// IsBreakableAtStartOfResumedContainer reports whether a break is allowed
// before the first child of a container that resumed after a break. Normally
// that is not a valid breakpoint (nothing would be placed in the
// fragmentainer), but when the enclosing column algorithm passed down a
// MinBreakAppeal better than LastResort — a nested multicol that may have
// more space in the next outer fragmentainer (cla.cc:930-935) — breaking here
// is permitted so content can be pushed to that outer fragmentainer instead
// of taking a worse break. Mirrors Blink's
// IsBreakableAtStartOfResumedContainer (fragmentation_utils.cc:214-219 @
// a9f50e522efa): MinBreakAppeal != LastResort && IsBreakInside(previous
// break token) && is_first_for_node. `space` is the container's own space, so
// its BreakToken is the previous break token; callers only invoke this for
// children that are first-for-node (no incoming token, or a break-before one).
func IsBreakableAtStartOfResumedContainer(space ConstraintSpace) bool {
	return space.MinBreakAppeal != BreakAppealLastResort && space.BreakToken.IsBreakInside()
}

// MovePastBreakpoint decides whether the laid-out child can stay in the
// current fragmentainer (true), or whether a break must be inserted before it
// (false). Mirrors Blink's MovePastBreakpoint (fragmentation_utils.cc:1141-1240
// @ a9f50e522efa); the child-overload pre-checks (:1068-1139) live in
// BreakBeforeChildIfNeeded and at its block_layout.go call site. Not ported:
// early breaks (no EarlyBreak / RelayoutAndBreakEarlier yet) and
// SetIsBlockSizeForFragmentationClamped.
func MovePastBreakpoint(
	space ConstraintSpace,
	layoutResult *LayoutResult,
	fragmentainerBlockOffset float64,
	fragmentainerBlockSize float64,
	appealBefore BreakAppeal,
	builder *BoxFragmentBuilder,
) bool {
	if !space.HasBlockFragmentation || fragmentainerBlockSize == Indefinite ||
		layoutResult == nil || layoutResult.Fragment == nil {
		return true
	}
	spaceLeft := fragmentainerBlockSize - fragmentainerBlockOffset

	// No space used in the fragmentainer yet: breaking before would make no
	// progress, unless the container is breakable at its resumed start.
	refuseBreakBefore := spaceLeft >= fragmentainerBlockSize &&
		!IsBreakableAtStartOfResumedContainer(space)

	// A child that starts past the end (e.g. pushed by its margin) must be
	// broken before; so must one that starts exactly at the end and broke
	// inside without being at its block end or a spanner — an empty
	// fragment would be resumed in the next fragmentainer anyway.
	breakToken := layoutResult.BreakToken
	mustBreakBefore := spaceLeft < 0 ||
		(spaceLeft == 0 && layoutResult.ColumnSpannerPath == nil &&
			breakToken.IsBreakInside() && !breakToken.IsAtBlockEnd)
	if mustBreakBefore {
		return refuseBreakBefore
	}

	// louis14: propagate any break-inside:avoid violation up to the column
	// builder so the multicol stretch loop sees it (a break before
	// overwrites it via SetBreakAppeal).
	appealInside := CalculateBreakAppealInside(space, layoutResult)
	if appealInside < BreakAppealPerfect {
		builder.ClampBreakAppeal(appealInside)
	}

	blockSize := BlockSizeForFragmentation(space.WritingDirection, layoutResult)
	if refuseBreakBefore {
		propagateSpaceShortageIfOverflowing(space, layoutResult, blockSize, fragmentainerBlockOffset, fragmentainerBlockSize, builder)
		return true
	}
	switch {
	case blockSize <= spaceLeft:
		// The child fits. If it broke inside (here or in an inner
		// fragmentation context), or its inside-appeal is sub-perfect, keep
		// that break only if it is at least as appealing as breaking before.
		brokeOrSubPerfect := breakToken.IsBreakInside() || appealInside < BreakAppealPerfect
		return !brokeOrSubPerfect || appealInside >= appealBefore
	case appealBefore == BreakAppealLastResort && space.RequiresContentBeforeBreaking:
		// Doesn't fit, but content is required here.
		propagateSpaceShortageIfOverflowing(space, layoutResult, blockSize, fragmentainerBlockOffset, fragmentainerBlockSize, builder)
		return true
	}
	return false
}

// propagateSpaceShortageIfOverflowing reports the space shortage of a child
// that is moved past although it does not fit (monolithic content at the
// start of the fragmentainer), so column balancing can stretch. Mirrors
// MovePastBreakpoint → PropagateSpaceShortage → CalculateSpaceShortage
// (fragmentation_utils.cc:1016-1053, :1230-1240 @ a9f50e522efa), with
// louis14's TrustedChildSpaceShortage narrowing of the child-reported value.
func propagateSpaceShortageIfOverflowing(space ConstraintSpace, layoutResult *LayoutResult, blockSize, fragmentainerBlockOffset, fragmentainerBlockSize float64, builder *BoxFragmentBuilder) {
	if blockSize <= fragmentainerBlockSize-fragmentainerBlockOffset || space.BlockFragmentationType != FragmentColumn {
		return
	}
	shortage := TrustedChildSpaceShortage(space, layoutResult)
	if shortage <= 0 {
		shortage = fragmentainerBlockOffset + blockSize - fragmentainerBlockSize
	}
	builder.PropagateSpaceShortage(shortage)
}

// BlockSizeForFragmentation is the block-size a parent attributes to a
// child result when fitting it into a fragmentainer: the result's recorded
// block-size for fragmentation (its content, overflow included), or the
// fragment's own border-box block-size when none was recorded. Mirrors
// Blink's free BlockSizeForFragmentation(result, writing_direction)
// (fragmentation_utils.cc:1545-1583 @ a9f50e522efa). louis14 records 0 where
// Blink has kIndefiniteSize (no block fragmentation, a line box); since a box
// records at least its own block-size (BoxFragmentBuilder.Build, Blink
// box_fragment_builder.cc:758-761), the max equals Blink's value. A
// fragmentainer (column) records its children only (:743-745), so its value
// is read as is.
func BlockSizeForFragmentation(wdm WritingDirectionMode, layoutResult *LayoutResult) float64 {
	if layoutResult.Fragment.BoxType == BoxTypeColumn {
		return layoutResult.BlockSizeForFragmentation
	}
	return math.Max(NewLogicalFragment(wdm, layoutResult.Fragment).BlockSize(),
		layoutResult.BlockSizeForFragmentation)
}

// TrustedChildSpaceShortage returns the space shortage a broken child
// reported inside itself, or 0 when the parent should derive the shortage
// from the child's size instead. Blink always trusts the child's report
// (CalculateSpaceShortage, fragmentation_utils.cc:1046-1051 @ a9f50e522efa);
// louis14 trusts it only when the child's break violated a break-avoidance
// (break-inside:avoid / monolithic, or a sub-perfect appeal deeper inside).
// louis14's inline layout reports no shortage for plain line breaks and its
// stretch loop lacks Blink's CurrentContentBlockOffset budget clamp, so an
// unconditional merge lets one column's report over-stretch a balanced row
// (LOU-320/LOU-325 unit tests; LOU-383: multicol-span-all-margin-bottom-001).
func TrustedChildSpaceShortage(space ConstraintSpace, childResult *LayoutResult) float64 {
	if childResult.BreakToken != nil && childResult.MinSpaceShortage > 0 &&
		(ShouldAvoidBreakInside(space, childResult) || childResult.BreakAppeal < BreakAppealPerfect) {
		return childResult.MinSpaceShortage
	}
	return 0
}

// BreakBeforeChildIfNeeded is the master fragmentation entry point a per-child
// loop calls right after laying out a child. Returns:
//   - status: Continue (keep this child + keep going), BrokeBefore (insert a
//     break before this child), or NeedsEarlierBreak (re-layout with an
//     earlier break — 12g, never returned today).
//   - isForced: true when the break was forced by break-before/after value
//     (column/page/etc.); false when it was a soft break driven by fit /
//     break-avoid violation.
//
// Mirrors Blink's BreakBeforeChildIfNeeded (fragmentation_utils.cc); the
// is_forced_break info is the same arg Blink passes to AddBreakBeforeChild.
func BreakBeforeChildIfNeeded(
	space ConstraintSpace,
	child *LayoutInputNode,
	layoutResult *LayoutResult,
	fragmentainerBlockOffset float64,
	fragmentainerBlockSize float64,
	hasContainerSeparation bool,
	builder *BoxFragmentBuilder,
) (status BreakStatus, isForced bool) {
	if !space.HasBlockFragmentation {
		return BreakStatusContinue, false
	}
	// Forced break-before / break-after takes precedence over fit-based logic.
	// This must run even during IsInitialColumnBalancingPass so the ContentRuns
	// measure loop (Phase 17) gets one continuation token per forced-break segment.
	// Soft breaks are gated on definite fragmentainerBlockSize below, so they
	// won't fire when FragmentainerBlockSize=Indefinite (as set in the measure pass).
	breakBetween := CalculateBreakBetweenValue(child, layoutResult, builder)
	if hasContainerSeparation && IsForcedBreakValue(space, breakBetween) {
		builder.SetBreakAppeal(BreakAppealPerfect)
		return BreakStatusBrokeBefore, true
	}

	// fragmentation_utils.cc:919-920 @ a9f50e522efa.
	appealBefore := CalculateBreakAppealBefore(space, child, layoutResult, builder, hasContainerSeparation)

	// Child-overload MovePastBreakpoint (fragmentation_utils.cc:1077-1083 @
	// a9f50e522efa): layout aborted, so no fragment was produced and there
	// is nothing to move past — break before the child.
	if layoutResult != nil && layoutResult.Status != LayoutResultSuccess {
		builder.SetBreakAppeal(appealBefore)
		return BreakStatusBrokeBefore, false
	}

	// Phase 16.d.2/3 (v2 B2): during initial column-balancing pass, propagate
	// unbreakable block-size up to the column algorithm so it can floor the
	// auto column block-size. Mirrors Blink's BreakBeforeChildIfNeeded
	// (fragmentation_utils.cc:1105-1113).
	if space.IsInitialColumnBalancingPass && ShouldAvoidBreakInside(space, layoutResult) {
		blockSize := CalculateUnbreakableBlockSize(space, layoutResult, fragmentainerBlockOffset)
		builder.PropagateTallestUnbreakableBlockSize(blockSize)
	}

	// Soft-break logic requires a definite fragmentainer size; during the initial
	// column balancing pass FragmentainerBlockSize=Indefinite so nothing below fires.
	if space.IsInitialColumnBalancingPass {
		return BreakStatusContinue, false
	}

	if MovePastBreakpoint(space, layoutResult, fragmentainerBlockOffset, fragmentainerBlockSize, appealBefore, builder) {
		return BreakStatusContinue, false
	}
	builder.SetBreakAppeal(appealBefore)
	return BreakStatusBrokeBefore, false
}

// FinishFragmentationInput is the builder state Blink's FinishFragmentation
// reads (fragmentation_utils.cc:547-822 @ a9f50e522efa). All sizes are
// border-box block sizes.
type FinishFragmentationInput struct {
	// SpaceLeft is FragmentainerSpaceLeft(builder, is_for_children=false):
	// the space left from this box's border edge, or Indefinite when the
	// fragmentainer block-size is unknown (initial column balancing pass).
	SpaceLeft             float64
	HasBlockFragmentation bool
	// IsColumnFragmentation and IsInitialColumnBalancingPass gate the space
	// shortage report (only multicol balancing consumes it).
	IsColumnFragmentation        bool
	IsInitialColumnBalancingPass bool
	PreviousBreakToken           *BlockBreakToken
	// FragmentsTotalBlockSize is the box's block-size over all fragments
	// (ComputeBlockSizeForFragment), IntrinsicBlockSize this fragment's
	// content extent including its block-start and block-end edges.
	FragmentsTotalBlockSize float64
	IntrinsicBlockSize      float64
	// TrailingBorderPadding is BorderScrollbarPadding().block_end;
	// BlockStartBorderPadding the initial fragment geometry's block-start
	// border + scrollbar + padding (no valid breakpoint inside it).
	TrailingBorderPadding                 float64
	BlockStartBorderPadding               float64
	PreventBreakBeforeBlockEndDecorations bool
	FoundColumnSpanner                    bool
	// ShouldBreakInside: a child broke inside or before (child break
	// tokens exist). HasInflowChildBreakInside: an in-flow child broke
	// inside (not a parallel flow); a new formatting context also folds in
	// a fragmentainer break among its floats (Blink's
	// GetExclusionSpace().HasFragmentainerBreak(), :689-691).
	ShouldBreakInside            bool
	HasInflowChildBreakInside    bool
	IsKnownToFitInFragmentainer  bool
	ShouldCloneBoxEndDecorations bool
}

// FinishFragmentationResult is what Blink's FinishFragmentation writes to the
// builder.
type FinishFragmentationResult struct {
	FragmentBlockSize  float64
	IntrinsicBlockSize float64
	ConsumedBlockSize  float64
	DidBreakSelf       bool
	IsAtBlockEnd       bool
	IncludeBlockEnd    bool
	// SpaceShortage > 0 is the amount reported via PropagateSpaceShortage.
	SpaceShortage           float64
	ClampAppealToLastResort bool
}

// FinishFragmentation decides this fragment's block-size and whether the box
// breaks, is at its block end, and which block edges it carries. Mirrors
// Blink's FinishFragmentation (fragmentation_utils.cc:547-822 @
// a9f50e522efa). Not ported: monolithic overflow (louis14 has no
// BlockBreakToken.MonolithicOverflow), kDisableFragmentation for
// block-overflow-clipping boxes, and early breaks (kNeedsEarlierBreak).
func FinishFragmentation(in FinishFragmentationInput) FinishFragmentationResult {
	spaceLeft := in.SpaceLeft
	previous := in.PreviousBreakToken
	previouslyConsumed := 0.0
	if previous != nil && !previous.IsBreakBefore {
		previouslyConsumed = previous.ConsumedBlockSize.Float64()
	}
	isPastEnd := previous != nil && previous.IsAtBlockEnd

	desiredBlockSize := math.Max(0, in.FragmentsTotalBlockSize-previouslyConsumed)
	desiredIntrinsic := in.IntrinsicBlockSize
	out := FinishFragmentationResult{
		FragmentBlockSize:  desiredBlockSize,
		IntrinsicBlockSize: desiredIntrinsic,
	}

	// There is a last-resort breakpoint before the trailing border/padding,
	// if progress can still be guaranteed.
	subtractable := 0.0
	if !in.PreventBreakBeforeBlockEndDecorations && desiredBlockSize > in.TrailingBorderPadding {
		subtractable = in.TrailingBorderPadding
	}
	// Tall unbreakable content may make this fragment take up more space.
	if spaceLeft != Indefinite {
		spaceLeft = math.Max(spaceLeft, desiredIntrinsic-subtractable)
	}

	if in.FoundColumnSpanner {
		out.DidBreakSelf = true
	}
	switch {
	case isPastEnd:
		out.FragmentBlockSize = 0
	case in.FoundColumnSpanner:
		// Layout was interrupted by a spanner; resume inside this block.
		out.FragmentBlockSize = math.Max(0, math.Min(desiredBlockSize, desiredIntrinsic)-in.TrailingBorderPadding)
	case spaceLeft != Indefinite && desiredBlockSize > spaceLeft && in.HasBlockFragmentation:
		// Taller than the space left; overflow only for unbreakable content.
		modifiedIntrinsic := math.Max(spaceLeft, desiredIntrinsic-subtractable)
		out.IntrinsicBlockSize = modifiedIntrinsic
		out.FragmentBlockSize = math.Min(desiredBlockSize-subtractable, modifiedIntrinsic)
		if out.FragmentBlockSize < desiredBlockSize {
			out.DidBreakSelf = true
		}
	}

	out.IncludeBlockEnd = !((out.DidBreakSelf && !in.ShouldCloneBoxEndDecorations) || isPastEnd)
	out.ConsumedBlockSize = previouslyConsumed + out.FragmentBlockSize

	if in.FoundColumnSpanner || !in.HasBlockFragmentation {
		return out
	}

	if spaceLeft == Indefinite {
		// Unknown space (initial column balancing pass): no break.
		if !in.HasInflowChildBreakInside || isPastEnd {
			out.IsAtBlockEnd = true
		}
		return out
	}

	if in.ShouldBreakInside {
		// A break token is needed to resume broken or unstarted children;
		// decide whether this box itself is at its block end.
		if isPastEnd {
			out.IsAtBlockEnd = true
		} else if desiredBlockSize <= spaceLeft &&
			(!in.HasInflowChildBreakInside || in.IsKnownToFitInFragmentainer) {
			out.IsAtBlockEnd = true
		}
		if out.IsAtBlockEnd {
			// Consumed block-size includes the rest of the fragmentainer.
			out.ConsumedBlockSize = previouslyConsumed + math.Max(out.FragmentBlockSize, spaceLeft)
		} else if !in.ShouldCloneBoxEndDecorations {
			out.IncludeBlockEnd = false
		}
		return out
	}

	if desiredBlockSize > spaceLeft {
		// No child broke inside, but this box is too tall to fit.
		if previouslyConsumed == 0 && spaceLeft < in.BlockStartBorderPadding {
			// No valid breakpoint inside the block-start edge.
			out.ClampAppealToLastResort = true
		}
		if in.IsColumnFragmentation && !in.IsInitialColumnBalancingPass {
			out.SpaceShortage = desiredBlockSize - spaceLeft
		}
		if desiredBlockSize <= desiredIntrinsic {
			// Breaking here is inside or right before the block-end edge.
			out.ClampAppealToLastResort = true
		}
		return out
	}

	// The end of the block fits in the current fragmentainer.
	out.IsAtBlockEnd = true
	return out
}
