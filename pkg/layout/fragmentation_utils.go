package layout

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/html"
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
// Early breaks (LOU-487): UpdateEarlyBreakAtBlockChild, AttemptSoftBreak
// and FinishFragmentation's kNeedsEarlierBreak; the EarlyBreak type and
// EnterEarlyBreakInChild / IsEarlyBreakTarget are in early_break.go, the
// relayout in BlockLayoutAlgorithm.relayoutAndBreakEarlier.
//
// Out of scope (deferred):
//   - Paginated PageName interaction → not multicol
//   - FlexColumnBreakInfo per-column flex info → not multicol
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
// fragment is past the box's block end, or follows monolithic overflow
// with less space left than its block border+padding. Mirrors Blink's
// ShouldCloneBlockStartBorderPadding (fragmentation_utils.cc:61-78 @
// a9f50e522efa), which reads builder.Node().Style(); louis14's builder
// style is its node's.
func ShouldCloneBlockStartBorderPadding(builder *BoxFragmentBuilder) bool {
	if !hasCloneBoxDecorationBreak(builder.Style()) {
		return false
	}
	previousBreakToken := builder.PreviousBreakToken()
	if previousBreakToken == nil {
		return true
	}
	// Pushed by monolithic overflow into a fragmentainer with no room for
	// the edges: don't repeat them.
	if previousBreakToken.HasMonolithicOverflow() &&
		FragmentainerSpaceLeft(builder, false) < builder.BorderScrollbarPadding().BlockSum() {
		return false
	}
	return !previousBreakToken.IsAtBlockEnd
}

// HasBreakOpportunityBeforeNextChild reports whether placing a child leaves
// a class A breakpoint before its next sibling, giving that sibling
// container separation. A box does unless it resumed at its block end, i.e.
// sits in a parallel flow that must not affect this flow; a line box does
// only when it has non-zero block-size (the zero-height lines around a
// block-in-inline do not count). Mirrors Blink's
// HasBreakOpportunityBeforeNextChild (fragmentation_utils.cc:870-892 @
// a9f50e522efa). Blink reads the line box's writing direction off its
// style; louis14 line boxes carry none, so wdm is the container's, which a
// line box shares.
func HasBreakOpportunityBeforeNextChild(wdm WritingDirectionMode, childFragment *PhysicalFragment, incomingChildBreakToken *BlockBreakToken) bool {
	if childFragment.Type != FragmentLineBox {
		return incomingChildBreakToken == nil || !incomingChildBreakToken.IsAtBlockEnd
	}
	return NewLogicalFragment(wdm, childFragment).BlockSize() != 0
}

// ShouldIncludeBlockStartBorderPadding reports whether this fragment carries
// its block-start border/padding: the first fragment does, a resumed one
// only when cloned. Mirrors Blink fragmentation_utils.cc:522-525 @
// a9f50e522efa.
func ShouldIncludeBlockStartBorderPadding(builder *BoxFragmentBuilder) bool {
	return !builder.PreviousBreakToken().IsBreakInside() || ShouldCloneBlockStartBorderPadding(builder)
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

// clipsBlockAxisOverflow reports whether a box with style, generated by
// node, clips overflow along the block axis of wdm (Blink
// BlockNode::GetOverflowClipAxes & block_axis).
func clipsBlockAxisOverflow(style *css.Style, node *html.Node, wdm WritingDirectionMode) bool {
	blockAxis := OverflowClipY
	if !wdm.IsHorizontal() {
		blockAxis = OverflowClipX
	}
	return computeOverflowClipAxes(style, node)&blockAxis != 0
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
// An inline child — a line of the inline formatting context — carries no
// break-before of its own, only what its result propagated (:133-136). A
// child whose fragment resumes its node after a break is no longer *before*
// the node, so its values are ignored (:144-157). Paginated page names are
// not ported.
func CalculateBreakBetweenValue(child *LayoutInputNode, layoutResult *LayoutResult, builder *BoxFragmentBuilder) string {
	if child.IsInline() {
		initialBreakBefore := "auto"
		if layoutResult != nil {
			initialBreakBefore = layoutResult.InitialBreakBefore
		}
		return builder.JoinedBreakBetweenValue(initialBreakBefore)
	}
	if layoutResult != nil && layoutResult.Status == LayoutResultSuccess &&
		!layoutResult.Fragment.IsFirstForNode() {
		return "auto"
	}
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
	breakableAtStartOfContainer := IsBreakableAtStartOfResumedContainer(space, layoutResult)
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
	// Without container separation this is the start of the container: not
	// a valid break point, unless it is the start of a resumed container. A
	// child that ran out of fragmentainer space must move on regardless.
	if !hasContainerSeparation && layoutResultStatus == LayoutResultSuccess {
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
	considerInsideAvoid := layoutResult.BreakToken != nil &&
		!layoutResult.BreakToken.IsBreakBefore
	return calculateBreakAppealInside(space, layoutResult, layoutResult.BreakAppeal, considerInsideAvoid)
}

// CalculateBreakAppealInsideWithHypotheticalAppeal is the appeal of breaking
// inside a child that did not break, at a breakpoint of hypotheticalAppeal
// found inside it (its early break): break-inside:avoid on the child always
// applies. Blink's CalculateBreakAppealInside with hypothetical_appeal set
// (fragmentation_utils.cc:274-294 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// Go has no optional parameters.
func CalculateBreakAppealInsideWithHypotheticalAppeal(space ConstraintSpace, layoutResult *LayoutResult, hypotheticalAppeal BreakAppeal) BreakAppeal {
	return calculateBreakAppealInside(space, layoutResult, hypotheticalAppeal, true)
}

// calculateBreakAppealInside is the shared body of the two
// CalculateBreakAppealInside forms (fragmentation_utils.cc:278-313).
func calculateBreakAppealInside(space ConstraintSpace, layoutResult *LayoutResult, appeal BreakAppeal, considerInsideAvoid bool) BreakAppeal {
	if layoutResult.HasForcedBreak {
		return BreakAppealPerfect
	}
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

// ClampedToValidFragmentainerCapacity floors a fragmentainer capacity at
// 1px: to ensure content progression a fragmentainer must accept at least
// 1px of content (css-break-3 §4.4). Mirrors Blink's
// ClampedToValidFragmentainerCapacity(LayoutUnit)
// (fragmentation_utils.h:153-158 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ClampedToValidFragmentainerCapacity(length float64) float64 {
	return max(length, 1)
}

// ClampedToValidFragmentainerCapacityForBuilder is Blink's builder overload
// of ClampedToValidFragmentainerCapacity; Go has no overloading. For the
// fragment itself (isForChildren false) a box that clones its block-end
// decorations must also fit its repeated border/padding, so it always makes
// 1px of content progress between the cloned edges. Mirrors Blink's
// ClampedToValidFragmentainerCapacity(builder, length, is_for_children)
// (fragmentation_utils.h:159-173 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ClampedToValidFragmentainerCapacityForBuilder(builder *BoxFragmentBuilder, length float64, isForChildren bool) float64 {
	if builder.ShouldCloneBoxEndDecorations() && !isForChildren {
		return max(length, 1+builder.BorderScrollbarPadding().BlockSum())
	}
	return ClampedToValidFragmentainerCapacity(length)
}

// shouldCloneBoxEndDecorations reports whether a box repeats its block-end
// border/padding in this fragment: cloned block-start decorations make the
// block-end ones cloned too, unless the fragment is known to be the box's
// last (known to fit), and only when it is fragmented outside the initial
// column-balancing pass. Mirrors SetupFragmentBuilderForFragmentation
// (fragmentation_utils.cc:440-443, :450-451, :484-495 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func shouldCloneBoxEndDecorations(space ConstraintSpace, cloneBoxStartDecorations, isKnownToFitInFragmentainer bool) bool {
	return space.HasBlockFragmentation && !space.IsInitialColumnBalancingPass &&
		cloneBoxStartDecorations && !isKnownToFitInFragmentainer
}

// hasCloneBoxDecorationBreak reports whether style has
// box-decoration-break:clone (Blink: Style().BoxDecorationBreak() ==
// EBoxDecorationBreak::kClone); a builder without a style slices.
func hasCloneBoxDecorationBreak(style *css.Style) bool {
	return style != nil && style.GetBoxDecorationBreak() == css.BoxDecorationBreakClone
}

// InvolvedInBlockFragmentation reports whether a box laid out in space
// takes part in block fragmentation: the space fragments, or the box resumes
// after a break inside it (a box relaid out with fragmentation forced off
// still finishes its last fragment from the consumed block-size). Mirrors
// Blink's InvolvedInBlockFragmentation (fragmentation_utils.h:75-79 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func InvolvedInBlockFragmentation(space ConstraintSpace) bool {
	return space.HasBlockFragmentation || space.BreakToken.IsBreakInside()
}

// FragmentainerCapacity is the fragmentainer block-size to use during layout,
// or Indefinite when it is unknown (initial column balancing). For children
// (isForChildren) cloned box decorations shrink the space, as children steer
// clear of them; for the fragment itself they are included. Mirrors Blink's
// FragmentainerCapacity (fragmentation_utils.h:175-203 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func FragmentainerCapacity(builder *BoxFragmentBuilder, isForChildren bool) float64 {
	space := builder.GetConstraintSpace()
	if !space.HasBlockFragmentation || space.FragmentainerBlockSize == Indefinite {
		return Indefinite
	}
	size := space.FragmentainerBlockSize
	if hasCloneBoxDecorationBreak(builder.Style()) && isForChildren {
		bsp := builder.BorderScrollbarPadding()
		size -= bsp.BlockStart
		if builder.ShouldCloneBoxEndDecorations() {
			size -= bsp.BlockEnd
		}
	}
	return ClampedToValidFragmentainerCapacityForBuilder(builder, size, isForChildren)
}

// FragmentainerOffset is the box's border-edge offset from the fragmentainer
// start. For children of a box with cloned decorations it is pulled back by
// the block-start edge, so a child at the block-start content edge is at
// offset 0 and cannot be broken before until there has been content
// progress. Mirrors Blink's FragmentainerOffset (fragmentation_utils.h:
// 205-235 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func FragmentainerOffset(builder *BoxFragmentBuilder, isForChildren bool) float64 {
	space := builder.GetConstraintSpace()
	if !space.HasBlockFragmentation {
		return 0
	}
	offset := space.FragmentainerOffset
	if hasCloneBoxDecorationBreak(builder.Style()) && isForChildren {
		if breakToken := builder.PreviousBreakToken(); breakToken == nil || !breakToken.IsAtBlockEnd {
			offset -= builder.BorderScrollbarPadding().BlockStart
		}
	}
	return offset
}

// FragmentainerSpaceLeft is the fragmentainer space left at the box's
// block-start (clamped to zero), or Indefinite when the fragmentainer
// block-size is unknown. Mirrors Blink's FragmentainerSpaceLeft
// (fragmentation_utils.h:237-257 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func FragmentainerSpaceLeft(builder *BoxFragmentBuilder, isForChildren bool) float64 {
	capacity := FragmentainerCapacity(builder, isForChildren)
	if capacity == Indefinite {
		return Indefinite
	}
	return max(0, capacity-FragmentainerOffset(builder, isForChildren))
}

// SetupSpaceBuilderForFragmentation copies a parent's block-fragmentation
// context into a child's constraint-space builder: the fragmentainer
// block-size and the child's offset in the fragmentainer (with
// IsAtFragmentainerStart for an offset at or before its start), the
// fragmentation type, the propagate-child-break-values bit (:346),
// RequiresContentBeforeBreaking (:347-349), the column-balancing flags,
// IsInsideBreakAvoid (:353), ShouldIgnoreForcedBreaks (:364-367) and
// MinBreakAppeal, so a container that is breakable at the start of a resumed
// fragment stays breakable through its descendants. Mirrors Blink's
// SetupSpaceBuilderForFragmentation (fragmentation_utils.cc:321-372 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The monolithic-child early
// return (:336-338) stays with the callers, which then leave fragmentation
// off. Not ported: the out-of-flow child in the initial column balancing
// pass clause of :364-366 (louis14 lays out no OOF through this function in
// that pass) and PageName (no paged media).
func SetupSpaceBuilderForFragmentation(parentSpace ConstraintSpace, fragmentainerOffset, fragmentainerBlockSize float64, requiresContentBeforeBreaking bool, b *ConstraintSpaceBuilder) {
	b.SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(fragmentainerBlockSize).
		SetFragmentainerOffset(fragmentainerOffset).
		SetBlockFragmentationType(parentSpace.BlockFragmentationType).
		SetShouldPropagateChildBreakValues(true).
		SetRequiresContentBeforeBreaking(requiresContentBeforeBreaking).
		SetIsInitialColumnBalancingPass(parentSpace.IsInitialColumnBalancingPass).
		SetIsInsideBalancedColumns(parentSpace.IsInsideBalancedColumns).
		SetIsInsideBreakAvoid(parentSpace.IsInsideBreakAvoid).
		SetMinBreakAppeal(parentSpace.MinBreakAppeal).
		SetIsAtFragmentainerStart(fragmentainerOffset <= 0)
	if parentSpace.ShouldIgnoreForcedBreaks {
		b.SetShouldIgnoreForcedBreaks()
	}
}

// SetupSpaceBuilderForFragmentationFromBuilder is Blink's parent-builder
// overload of SetupSpaceBuilderForFragmentation (fragmentation_utils.cc:
// 379-397 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b; Go has no
// overloading): the parent builder supplies its RequiresContentBeforeBreaking,
// and a break-inside:avoid parent puts the child inside break avoidance.
// The caller supplies the fragmentainer offset and capacity for children.
func SetupSpaceBuilderForFragmentationFromBuilder(parentBuilder *BoxFragmentBuilder, fragmentainerOffset, fragmentainerBlockSize float64, b *ConstraintSpaceBuilder) {
	parentSpace := *parentBuilder.GetConstraintSpace()
	SetupSpaceBuilderForFragmentation(parentSpace, fragmentainerOffset, fragmentainerBlockSize,
		parentBuilder.RequiresContentBeforeBreaking(), b)
	if parentStyle := parentBuilder.Style(); parentStyle != nil &&
		IsAvoidBreakValue(parentSpace, parentStyle.GetBreakInside()) {
		b.SetIsInsideBreakAvoid(true)
	}
}

// IsBreakableAtStartOfResumedContainer reports whether a break is allowed
// before the given child, laid out first in a container that resumed after
// a break. Normally that is not a valid breakpoint (nothing would be placed
// in the fragmentainer), but when the enclosing column algorithm passed down
// a MinBreakAppeal better than LastResort — a nested multicol that may have
// more space in the next outer fragmentainer (cla.cc:930-935) — breaking
// here is permitted so content can be pushed to that outer fragmentainer
// instead of taking a worse break. Mirrors Blink's result overload
// (fragmentation_utils.cc:198-212 @ a9f50e522efa): an aborted result is
// never breakable, and is_first_for_node comes from the child's fragment.
// Blink's builder argument only supplies the previous break token, which
// louis14 keeps on the container's own space.
func IsBreakableAtStartOfResumedContainer(space ConstraintSpace, childLayoutResult *LayoutResult) bool {
	if childLayoutResult == nil {
		return IsBreakableAtStartOfResumedContainerWithFirstForNode(space, true)
	}
	if childLayoutResult.Status != LayoutResultSuccess {
		return false
	}
	return IsBreakableAtStartOfResumedContainerWithFirstForNode(space, childLayoutResult.Fragment.IsFirstForNode())
}

// IsBreakableAtStartOfResumedContainerWithFirstForNode is Blink's builder
// overload of IsBreakableAtStartOfResumedContainer (fragmentation_utils.cc:
// 214-219 @ a9f50e522efa); Go has no overloading. `space` is the
// container's own space, so its BreakToken is the previous break token.
func IsBreakableAtStartOfResumedContainerWithFirstForNode(space ConstraintSpace, isFirstForNode bool) bool {
	return space.MinBreakAppeal != BreakAppealLastResort && space.BreakToken.IsBreakInside() && isFirstForNode
}

// MovePastBreakpoint decides whether the laid-out child can stay in the
// current fragmentainer (true), or whether a break must be inserted before it
// (false). Mirrors Blink's MovePastBreakpoint (fragmentation_utils.cc:1141-1240
// @ a9f50e522efa); the child-overload pre-checks (:1068-1139) and its
// UpdateEarlyBreakAtBlockChild call (:1126-1135) live in
// BreakBeforeChildIfNeeded and at its block_layout.go call site. builder may
// be nil, as Blink's PositionFloat passes (floats_utils.cc:377-380): breaking
// before is then refused only at the fragmentainer start, and nothing is
// propagated.
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
		(builder == nil || !IsBreakableAtStartOfResumedContainer(space, layoutResult))

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
	if appealInside < BreakAppealPerfect && builder != nil {
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
		// that break only if it is at least as appealing as breaking before
		// it or at the best earlier breakpoint (:1202-1220).
		brokeOrSubPerfect := breakToken.IsBreakInside() || appealInside < BreakAppealPerfect
		return !brokeOrSubPerfect || (appealInside >= appealBefore &&
			(builder == nil || !builder.HasEarlyBreak() || appealInside >= builder.GetEarlyBreak().GetBreakAppeal()))
	case appealBefore == BreakAppealLastResort && builder != nil && builder.RequiresContentBeforeBreaking():
		// Doesn't fit, but content is required here: force it to stay
		// (:1224-1228).
		builder.SetIsBlockSizeForFragmentationClamped()
		propagateSpaceShortageIfOverflowing(space, layoutResult, blockSize, fragmentainerBlockOffset, fragmentainerBlockSize, builder)
		return true
	}
	return false
}

// propagateSpaceShortageIfOverflowing reports the space shortage of a child
// that is moved past although it does not fit (monolithic content at the
// start of the fragmentainer), so column balancing can stretch. Mirrors
// MovePastBreakpoint → PropagateSpaceShortage → CalculateSpaceShortage
// (fragmentation_utils.cc:995-1053, :1230-1240 @ a9f50e522efa).
func propagateSpaceShortageIfOverflowing(space ConstraintSpace, layoutResult *LayoutResult, blockSize, fragmentainerBlockOffset, fragmentainerBlockSize float64, builder *BoxFragmentBuilder) {
	if builder == nil || blockSize <= fragmentainerBlockSize-fragmentainerBlockOffset {
		return
	}
	PropagateSpaceShortage(space, layoutResult, fragmentainerBlockOffset, fragmentainerBlockSize, builder)
}

// PropagateSpaceShortage reports how much more fragmentainer space would have
// let the child fit, for column balancing (only multicol cares). Mirrors
// Blink's PropagateSpaceShortage (fragmentation_utils.cc:995-1014 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the builder keeps the minimum
// positive value. space is the builder's constraint space (Blink reads
// builder->GetConstraintSpace(); not every louis14 builder carries one).
// Blink's block_size_override parameter has no louis14 caller.
func PropagateSpaceShortage(space ConstraintSpace, layoutResult *LayoutResult, fragmentainerBlockOffset, fragmentainerBlockSize float64, builder *BoxFragmentBuilder) {
	if space.BlockFragmentationType != FragmentColumn {
		return
	}
	// Only a positive shortage can stretch the columns; this drops an
	// aborted layout's Indefinite, as Blink does here (:1008-1013) on top of
	// the builder's own <= 0 filter (UpdateMinimalSpaceShortage, :1058).
	if spaceShortage := CalculateSpaceShortage(space, layoutResult, fragmentainerBlockOffset, fragmentainerBlockSize); spaceShortage > 0 {
		builder.PropagateSpaceShortage(spaceShortage)
	}
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
//
// A float laid out without block fragmentation (Blink's kIndefiniteSize:
// monolithic, or not in a fragmentation context) also counts its margins,
// since margins on floats do not break or truncate (:1562-1570). The
// margins are the fragment's own (UpdateMarginPaddingInfoIfNeeded,
// block_node.cc:1504-1520), untruncated.
func BlockSizeForFragmentation(wdm WritingDirectionMode, layoutResult *LayoutResult) float64 {
	if layoutResult.Fragment.BoxType == BoxTypeColumn {
		return layoutResult.BlockSizeForFragmentation
	}
	blockSize := math.Max(NewLogicalFragment(wdm, layoutResult.Fragment).BlockSize(),
		layoutResult.BlockSizeForFragmentation)
	if frag := layoutResult.Fragment; !layoutResult.HasBlockSizeForFragmentation &&
		frag.IsFloating && frag.BoxData != nil {
		blockSize += ToLogicalEdges(frag.BoxData.Margin, wdm).BlockSum()
	}
	return blockSize
}

// CalculateSpaceShortage returns how much more fragmentainer space would have
// let the child fit here: the space shortage the child reported inside itself
// when it has one (it broke inside, or something inside it was moved past
// without fitting, so a whole-child figure is impossible or pointless),
// otherwise the child's block-size for fragmentation past the fragmentainer
// end. Mirrors Blink's CalculateSpaceShortage (fragmentation_utils.cc:1021-1053
// @ a9f50e522efa); the trusted child report is :1046-1051. When layout
// aborted (no fragment) and the child reported nothing, nothing can be
// calculated: Indefinite, Blink's kIndefiniteSize (:1037-1040). Blink's
// block_size_override case has no louis14 caller.
func CalculateSpaceShortage(space ConstraintSpace, layoutResult *LayoutResult, fragmentainerBlockOffset, fragmentainerBlockSize float64) float64 {
	if layoutResult.MinSpaceShortage > 0 {
		return layoutResult.MinSpaceShortage
	}
	if layoutResult.Status != LayoutResultSuccess {
		return Indefinite
	}
	return fragmentainerBlockOffset + BlockSizeForFragmentation(space.WritingDirection, layoutResult) - fragmentainerBlockSize
}

// BreakBeforeChildIfNeeded is the master fragmentation entry point a per-child
// loop calls right after laying out a child. Returns:
//   - status: Continue (keep this child + keep going), BrokeBefore (insert a
//     break before this child), or NeedsEarlierBreak (an earlier sibling
//     breakpoint is more appealing: re-lay out and break there, the
//     builder's EarlyBreak).
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

	// Child-overload MovePastBreakpoint pre-check: a resumed child is
	// never broken before; once it has found room for its first fragment it
	// cannot skip fragmentainers, or its layout would restart from scratch
	// (fragmentation_utils.cc:1086-1100 @ a9f50e522efa). Every louis14
	// caller passes a block child.
	if layoutResult != nil && !layoutResult.Fragment.IsFirstForNode() {
		return BreakStatusContinue, false
	}

	// Clearance past a float that continues in the next fragmentainer
	// pushes the child there too: retry it in the next fragmentainer
	// (fragmentation_utils.cc:1102-1107 @ a9f50e522efa). The builder's
	// exclusion space is the one current at this child.
	if style := child.Style(); style != nil &&
		builder.GetExclusionSpace().NeedsClearancePastFragmentainer(style.GetClear(), space.WritingDirection) {
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
		// Not breaking before this child, but breaking there may be the
		// best breakpoint yet: keep it in mind (fragmentation_utils.cc:
		// 1126-1135 @ a9f50e522efa). Every louis14 caller passes a block
		// child, never a row item.
		UpdateEarlyBreakAtBlockChild(space, child, layoutResult, appealBefore, builder)
		return BreakStatusContinue, false
	}
	// Out of space, and breaking inside the child is not appealing: break
	// before it, or before an earlier sibling if a more appealing
	// breakpoint was found there (:927-936).
	if !AttemptSoftBreak(space, layoutResult, appealBefore, builder) {
		return BreakStatusNeedsEarlierBreak, false
	}
	return BreakStatusBrokeBefore, false
}

// UpdateEarlyBreakAtBlockChild records the breakpoint before child, or the
// best one inside it, as the builder's early break when it is at least as
// appealing as the one already found. A breakpoint inside the child
// (layoutResult.EarlyBreak, from a child that did not break) wins over the
// one before it at equal appeal: whatever takes us further wins. Mirrors
// Blink's UpdateEarlyBreakAtBlockChild (fragmentation_utils.cc:1249-1314 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); FlexColumnBreakInfo has no
// louis14 caller.
func UpdateEarlyBreakAtBlockChild(space ConstraintSpace, child *LayoutInputNode, layoutResult *LayoutResult, appealBefore BreakAppeal, builder *BoxFragmentBuilder) {
	appealInside := BreakAppealLastResort
	if breakpoint := layoutResult.EarlyBreak; breakpoint != nil {
		appealInside = CalculateBreakAppealInsideWithHypotheticalAppeal(space, layoutResult, breakpoint.GetBreakAppeal())
		if !builder.HasEarlyBreak() || builder.GetEarlyBreak().GetBreakAppeal() <= breakpoint.GetBreakAppeal() {
			// A good breakpoint inside the child: chain the child to it.
			builder.SetEarlyBreak(NewBlockEarlyBreak(child, appealInside, breakpoint))
		}
	}
	// Breaking before the child is no better if it is less appealing than
	// what was found already, or as appealing as the breakpoint inside it.
	if appealBefore <= appealInside {
		return
	}
	if builder.HasEarlyBreak() && builder.GetEarlyBreak().GetBreakAppeal() > appealBefore {
		return
	}
	builder.SetEarlyBreak(NewBlockEarlyBreak(child, appealBefore, nil))
}

// AttemptSoftBreak breaks before the child (returning true), unless the
// builder holds an earlier breakpoint more appealing than breaking here, in
// which case it returns false: lay out again and break there. Mirrors
// Blink's AttemptSoftBreak (fragmentation_utils.cc:1316-1353 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with BreakBeforeChild's
// builder updates (:944-973): the appeal (AddBreakBeforeChild's) and, the
// break not being forced, the pushed child's break values. Blink reports
// the space shortage here (:1337-1339, :962-966); louis14's
// BlockLayoutAlgorithm reports it at its call site, from where it would
// actually break.
func AttemptSoftBreak(space ConstraintSpace, layoutResult *LayoutResult, appealBefore BreakAppeal, builder *BoxFragmentBuilder) bool {
	if builder.HasEarlyBreak() && builder.GetEarlyBreak().GetBreakAppeal() > appealBefore {
		return false
	}
	// Break before the child. A better break further up (not known yet)
	// may replace this one.
	builder.SetBreakAppeal(appealBefore)
	if space.ShouldPropagateChildBreakValues() {
		builder.PropagateChildBreakValues(layoutResult)
	}
	return true
}

// FinishFragmentationInput is the builder state Blink's FinishFragmentation
// reads (fragmentation_utils.cc:547-822 @ a9f50e522efa). All sizes are
// border-box block sizes.
type FinishFragmentationInput struct {
	// SpaceLeft is FragmentainerSpaceLeft(builder, is_for_children=false):
	// the space left from this box's border edge, or Indefinite when the
	// fragmentainer block-size is unknown (initial column balancing pass).
	SpaceLeft float64
	// FragmentainerCapacity is FragmentainerCapacity(builder,
	// is_for_children=false), read to steer clear of incoming monolithic
	// overflow.
	FragmentainerCapacity float64
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
	// HasEarlyBreak: the builder holds a breakpoint found earlier in this
	// fragment (BoxFragmentBuilder::HasEarlyBreak).
	HasEarlyBreak bool
	// HasNonVisibleBlockOverflow: the box clips overflow along its block
	// axis (BlockNode::HasNonVisibleBlockOverflow, block_node.cc:1356-1361
	// @ a9f50e522efa).
	HasNonVisibleBlockOverflow bool
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
	// NeedsEarlierBreak: the only breakpoint left is inside or right
	// before the block-end edge, and an earlier one was found; lay out
	// again and break there (BreakStatus::kNeedsEarlierBreak, :811-812).
	NeedsEarlierBreak bool
	// MonolithicOverflow > 0 is the space reserved with
	// ReserveSpaceForMonolithicOverflow (box_fragment_builder.h:439-442 @
	// a9f50e522efa): incoming monolithic overflow still to steer clear of in
	// the next fragmentainer. The outgoing break token carries it
	// (block_break_token.cc:77), and it makes the box break inside
	// (BoxFragmentBuilder::ShouldBreakInside, box_fragment_builder.h:494-497).
	MonolithicOverflow float64
	// DisableFragmentation: a block-overflow-clipping box reached its end
	// while something still broke inside it; lay it out again without
	// fragmentation so it generates no further (zero-sized, clipped)
	// fragments (BreakStatus::kDisableFragmentation, :748-758).
	DisableFragmentation bool
}

// FinishFragmentation decides this fragment's block-size and whether the box
// breaks, is at its block end, and which block edges it carries. Mirrors
// Blink's FinishFragmentation (fragmentation_utils.cc:547-822 @
// a9f50e522efa).
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
	// Incoming monolithic overflow always guarantees progress: layout moves
	// past it, adding as many fragments as it needs (:577-586).
	hasIncomingMonolithicOverflow := previous.HasMonolithicOverflow()
	subtractable := 0.0
	if !in.PreventBreakBeforeBlockEndDecorations &&
		(desiredBlockSize > in.TrailingBorderPadding || hasIncomingMonolithicOverflow) {
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

	if out.FragmentBlockSize == 0 && hasIncomingMonolithicOverflow {
		// Pushed wholly past what this fragmentainer holds of the incoming
		// overflow: steer clear of the rest in the next one (:704-716).
		out.MonolithicOverflow = max(0, previous.MonolithicOverflow-in.FragmentainerCapacity)
	}

	if in.ShouldBreakInside || out.MonolithicOverflow > 0 {
		// A break token is needed to resume broken or unstarted children,
		// or to add fragments for monolithic overflow (ShouldBreakInside,
		// box_fragment_builder.h:494-497);
		// decide whether this box itself is at its block end.
		if isPastEnd {
			out.IsAtBlockEnd = true
		} else if desiredBlockSize <= spaceLeft &&
			(!in.HasInflowChildBreakInside || in.IsKnownToFitInFragmentainer) {
			if in.HasNonVisibleBlockOverflow {
				// The end of a fragmentable box that clips block-axis
				// overflow, with something broken inside: no more
				// fragments from it (:748-758).
				out.DisableFragmentation = true
				return out
			}
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
			// Breaking here is inside or right before the block-end edge:
			// break at the better early break if there is one, else mark
			// the break as unappealing.
			if in.HasEarlyBreak {
				out.NeedsEarlierBreak = true
				return out
			}
			out.ClampAppealToLastResort = true
		}
		return out
	}

	// The end of the block fits in the current fragmentainer.
	out.IsAtBlockEnd = true
	return out
}

// FinishFragmentationForFragmentainer sizes a fragmentainer (column box)
// fragment. With a known fragmentainer block-size the fragment takes it,
// whatever its content, and the consumed block-size advances by the
// fragmentainer capacity, so a break token is never produced by sizing alone;
// otherwise (initial column balancing) the fragment takes its content-based
// fragmentsTotalBlockSize, which for a fragmentainer has no previously
// consumed block-size baked in. Only FragmentBlockSize and ConsumedBlockSize
// are set: a fragmentainer is never at its block end and never breaks
// itself. Mirrors Blink's
// FinishFragmentationForFragmentainer (fragmentation_utils.cc:823-866 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). With incoming monolithic
// overflow larger than its capacity it reserves the rest (:844-852), so it
// breaks and its token carries the remainder.
func FinishFragmentationForFragmentainer(builder *BoxFragmentBuilder, fragmentsTotalBlockSize float64) FinishFragmentationResult {
	space := builder.GetConstraintSpace()
	previous := builder.PreviousBreakToken()
	consumedBlockSize := previouslyConsumedBlockSize(previous)
	var out FinishFragmentationResult
	if space.HasKnownFragmentainerBlockSize() {
		capacity := FragmentainerCapacity(builder, false)
		out.FragmentBlockSize = space.FragmentainerBlockSize
		out.ConsumedBlockSize = consumedBlockSize + capacity
		if previous.HasMonolithicOverflow() {
			// Add fragmentainers as long as monolithic overflow requires it.
			out.MonolithicOverflow = max(0, previous.MonolithicOverflow-capacity)
		}
	} else {
		out.FragmentBlockSize = fragmentsTotalBlockSize
		out.ConsumedBlockSize = consumedBlockSize + fragmentsTotalBlockSize
	}
	if builder.IsEmptySpannerParent() && builder.HasOutOfFlowFragmentainerDescendants() {
		builder.SetIsEmptySpannerParent(false)
	}
	return out
}
