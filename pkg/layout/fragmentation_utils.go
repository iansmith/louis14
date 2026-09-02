package layout

// fragmentation_utils.go — Blink-parity port of forced-break + break-appeal
// dispatch. Mirrors third_party/blink/renderer/core/layout/fragmentation_utils.{h,cc}.
//
// Phase 12d scope:
//   - BreakBeforeChildIfNeeded (the master hook)
//   - CalculateBreakBetweenValue (joins prev break-after + child break-before)
//   - CalculateBreakAppealBefore + CalculateBreakAppealInside
//   - MovePastBreakpoint (the simplified column-context decision)
//   - BreakBeforeChild + AttemptSoftBreak helpers
//
// Out of scope (deferred):
//   - EarlyBreak storage / RelayoutAndBreakEarlier retry → Phase 12g
//   - Paginated PageName interaction → not multicol
//   - FlexColumnBreakInfo per-column flex info → not multicol
//   - LayoutResult.InitialBreakBefore / FinalBreakAfter propagation across
//     resumed fragments → defer; for now we read break-before / break-after
//     directly off the child's style at the call site, which is correct for
//     non-resumed children (the only case 12d's drivers exercise).

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
//   - multicol_layout.go layoutSpanner / layoutSpannerInFrag when the
//     spanner's measured intrinsic block-size exceeds its declared box
//     (implicit monolithic for column-balance).
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
// v2 B2.5 extension: when the fragment is monolithic AND its declared
// block-size is shorter than the natural content height (IntrinsicBlockSize
// > fragment block-size), use the IntrinsicBlockSize instead. Captures the
// spanner-with-content-overflow case where the spanner's box stays at
// declared height but its content paints past it; the column needs to grow
// to accommodate the visible content area.
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
	blockSize := NewLogicalFragment(space.WritingDirection, layoutResult.Fragment).BlockSize()
	// Blink BlockSizeForFragmentation prefers the result's explicit
	// fragmentation size when one was computed (fragmentation_utils.cc:1548).
	if layoutResult.BlockSizeForFragmentation > blockSize {
		blockSize = layoutResult.BlockSizeForFragmentation
	}
	// v2 B2.5: monolithic + intrinsic > fragment block-size → use intrinsic.
	if layoutResult.Fragment.IsMonolithic && layoutResult.IntrinsicBlockSize > blockSize {
		blockSize = layoutResult.IntrinsicBlockSize
	}
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
// boundary BEFORE this child, joining the parent's previous-break-after (set
// when the previous in-flow child was added) with the child's break-before.
// Mirrors Blink's CalculateBreakBetweenValue (fragmentation_utils.cc).
//
// We omit the IsFirstForNode() / paginated PageName interactions: the caller
// in block_layout.go only invokes this for non-resumed children in column
// fragmentation context.
func CalculateBreakBetweenValue(child *LayoutInputNode, builder *BoxFragmentBuilder) string {
	childBreakBefore := "auto"
	if s := child.Style(); s != nil {
		childBreakBefore = s.GetBreakBefore()
	}
	return builder.JoinedBreakBetweenValue(childBreakBefore)
}

// CalculateBreakAppealBefore scores the appeal of breaking before the given
// child. Mirrors Blink's CalculateBreakAppealBefore (fragmentation_utils.cc).
//
// Args:
//   - hasContainerSeparation: there is at least one prior in-flow child in
//     this container (so a break before this child is a real boundary, not
//     the start-of-container).
//   - breakableAtStartOfResumedContainer: a break before the first child of
//     a resumed container is allowed (IsBreakableAtStartOfResumedContainer);
//     the appeal then floors at the space's MinBreakAppeal instead of
//     LastResort.
func CalculateBreakAppealBefore(
	space ConstraintSpace,
	breakBetween string,
	hasContainerSeparation bool,
	breakableAtStartOfResumedContainer bool,
) BreakAppeal {
	appeal := BreakAppealPerfect
	if !hasContainerSeparation {
		if !breakableAtStartOfResumedContainer {
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

// MovePastBreakpoint decides whether the laid-out child can be accepted in
// the current fragmentainer. Returns true when the child fits and no break
// is needed; false when a break must be inserted before this child.
// Mirrors Blink's MovePastBreakpoint (fragmentation_utils.cc) — simplified:
// no EarlyBreak retry, no FlexColumnBreakInfo, no paginated paths.
func MovePastBreakpoint(
	space ConstraintSpace,
	layoutResult *LayoutResult,
	fragmentainerBlockOffset float64,
	fragmentainerBlockSize float64,
	appealBefore BreakAppeal,
	builder *BoxFragmentBuilder,
) bool {
	if !space.HasBlockFragmentation || fragmentainerBlockSize == Indefinite {
		return true
	}
	if layoutResult == nil || layoutResult.Fragment == nil {
		return true
	}

	fragment := NewLogicalFragment(space.WritingDirection, layoutResult.Fragment)
	blockSize := fragment.BlockSize()
	spaceLeft := fragmentainerBlockSize - fragmentainerBlockOffset

	// refuseBreakBefore: a break before this child would land at the start of
	// a fresh fragmentainer (entire fragmentainer free). Without an
	// orphans/widows reason to break, refuse — otherwise we'd loop forever
	// emitting empty fragmentainers.
	refuseBreakBefore := spaceLeft >= fragmentainerBlockSize &&
		!IsBreakableAtStartOfResumedContainer(space)

	mustBreakBefore := false
	if spaceLeft < 0 {
		mustBreakBefore = true
	} else if spaceLeft == 0 {
		// Zero space left: only break if the child has internal break content
		// that won't fit (i.e. it broke and isn't at-block-end).
		hasBreakInside := layoutResult.BreakToken != nil &&
			!layoutResult.BreakToken.IsBreakBefore
		mustBreakBefore = layoutResult.ColumnSpannerPath == nil && hasBreakInside
	}
	if mustBreakBefore && !refuseBreakBefore {
		return false
	}

	appealInside := CalculateBreakAppealInside(space, layoutResult)
	movePast := refuseBreakBefore

	if !movePast {
		if blockSize <= spaceLeft {
			hasBreakInside := layoutResult.BreakToken != nil &&
				!layoutResult.BreakToken.IsBreakBefore
			if hasBreakInside || appealInside < BreakAppealPerfect {
				if appealInside >= appealBefore {
					movePast = true
				}
			} else {
				movePast = true
			}
		} else if appealBefore == BreakAppealLastResort &&
			space.RequiresContentBeforeBreaking {
			// Container demands SOMETHING before allowing a break (avoid empty
			// fragmentainer). Accept the overflow.
			movePast = true
		}
	}

	if movePast {
		// NOTE: Blink calls PropagateSpaceShortage when blockSize > spaceLeft;
		// we don't, because block_layout.go's existing overflow path already
		// records MinSpaceShortage on the LayoutResult once the child fragment
		// is committed. Adding it here would double-count and over-stretch the
		// multicol balancing loop (regressed spanner-fragmentation-006/008).

		// Propagate the appeal of breaking inside this child up to the column
		// result so the multicol stretch loop's hasViolatingBreak check
		// (cla.cc:1019) sees break-inside:avoid violations and stretches.
		// Mirrors Blink's flow where each column's overall BreakAppeal is the
		// worst of its descendants' inside-appeals.
		if appealInside < BreakAppealPerfect {
			builder.ClampBreakAppeal(appealInside)
		}
	}
	return movePast
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
	breakBetween := CalculateBreakBetweenValue(child, builder)
	if hasContainerSeparation && IsForcedBreakValue(space, breakBetween) {
		builder.SetBreakAppeal(BreakAppealPerfect)
		return BreakStatusBrokeBefore, true
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

	// A child that is breakable at the start of a resumed container
	// (IsBreakableAtStartOfResumedContainer) is treated like one with
	// container separation below, and the refuse-at-fragmentainer-start rule
	// (MovePastBreakpoint, fragmentation_utils.cc:1168-1171 @ a9f50e522efa:
	// no progress yet in this fragmentainer, so a break here would risk an
	// infinite run of empty fragmentainers) is lifted for it.
	breakableAtStart := IsBreakableAtStartOfResumedContainer(space)
	spaceLeft := fragmentainerBlockSize - fragmentainerBlockOffset
	refuseBreakBefore := fragmentainerBlockOffset <= 0 && !breakableAtStart
	canBreakBefore := (hasContainerSeparation || breakableAtStart) &&
		!refuseBreakBefore && fragmentainerBlockSize != Indefinite && layoutResult != nil
	var appealBefore BreakAppeal
	if canBreakBefore {
		appealBefore = CalculateBreakAppealBefore(
			space, breakBetween, hasContainerSeparation, breakableAtStart)
	}
	breakBefore := func() (BreakStatus, bool) {
		builder.SetBreakAppeal(appealBefore)
		return BreakStatusBrokeBefore, false
	}
	hasFragment := canBreakBefore && layoutResult.Fragment != nil
	var childBlockSize float64
	if hasFragment {
		childBlockSize = NewLogicalFragment(space.WritingDirection, layoutResult.Fragment).BlockSize()
	}

	appealInside := CalculateBreakAppealInside(space, layoutResult)

	// Blink MovePastBreakpoint (fragmentation_utils.cc:1198-1213): the child
	// fits but broke inside (here or in an inner fragmentation context), or
	// its inside-appeal is sub-perfect. Keep that break only if it is at
	// least as appealing as breaking before the child; otherwise break
	// before it so the whole child moves to the next fragmentainer.
	if hasFragment && childBlockSize <= spaceLeft &&
		(layoutResult.BreakToken.IsBreakInside() || appealInside < BreakAppealPerfect) &&
		appealInside < appealBefore {
		return breakBefore()
	}

	// Propagate any break-inside:avoid violation up to the column builder so
	// the multicol stretch loop sees it.
	if appealInside < BreakAppealPerfect {
		builder.ClampBreakAppeal(appealInside)
	}

	// Phase 14b: child signaled "I need more block-size than my fragment shows"
	// via LayoutResult.BlockSizeForFragmentation. Set today only by inner multicol
	// containers with column-fill:auto + explicit height that can't fit their
	// declared column block-size in the remaining outer fragmentainer space.
	// When that hint exceeds the actual remaining space, push the child to the
	// next outer fragmentainer instead of placing a clipped fragment whose
	// contents would leak into the outer overflow area. Mirrors Blink's
	// MovePastBreakpoint, which uses BlockSizeForFragmentation (not the
	// fragment's own size) for the "block_size > space_left → break before"
	// decision.
	if canBreakBefore && layoutResult.BlockSizeForFragmentation > 0 &&
		layoutResult.BlockSizeForFragmentation > spaceLeft {
		return breakBefore()
	}

	// break-inside avoidance on the child: if the child overflows the
	// fragmentainer, break before to push the whole child to the next column
	// instead of splitting it. Fires for break-inside:avoid-column style
	// values AND for monolithic fragments — ShouldAvoidBreakInside covers
	// both, mirroring Blink's fragmentation_utils.cc:188-196 (a monolithic
	// fragment always avoids inside-breaks) @ a9f50e522efa. Mirrors Blink's
	// MovePastBreakpoint, scoped narrowly to the avoid-inside case so we
	// don't take over normal overflow handling (which block_layout.go's
	// existing overflow handler owns — taking it over regressed
	// spanner-fragmentation-006/008).
	if hasFragment && childBlockSize > spaceLeft && ShouldAvoidBreakInside(space, layoutResult) {
		return breakBefore()
	}
	return BreakStatusContinue, false
}
