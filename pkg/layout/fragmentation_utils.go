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
// Mirrors Blink's ShouldAvoidBreakInside (fragmentation_utils.h). Phase
// 16.d.2/3 (v2 B2) port: louis14 doesn't yet have an IsMonolithic flag on
// PhysicalFragment (replaced elements aren't tagged as such; spanner
// descendants are gated via ConstraintSpace.IsInsideColumnSpanner from
// Phase 16.d.1, which is a different mechanism). For now we only check
// the style-level break-inside property; extend to monolithic fragments
// when the v2 B3 clip-removal exposes a test that needs it.
func ShouldAvoidBreakInside(space ConstraintSpace, layoutResult *LayoutResult) bool {
	if layoutResult == nil || layoutResult.Fragment == nil {
		return false
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
// Mirrors Blink's CalculateUnbreakableBlockSize (fragmentation_utils.cc):
//   - If the child has already-propagated unbreakable contribution from
//     its own descendants, return that directly (the deeper floor was
//     measured at the child's coordinate space and doesn't need
//     re-offsetting at the parent level).
//   - Otherwise, the child's contribution is its fragment's block-size
//     plus its position within the fragmentainer. The column box must
//     extend to cover both the offset and the child, so the floor is
//     `fragmentainerOffset + childBlockSize`.
//
// Used at the BreakBeforeChildIfNeeded propagation site
// (fragmentation_utils.cc:1105-1113).
func CalculateUnbreakableBlockSize(
	space ConstraintSpace,
	layoutResult *LayoutResult,
	fragmentainerBlockOffset float64,
) float64 {
	if layoutResult == nil {
		return 0
	}
	if layoutResult.TallestUnbreakableBlockSize > 0 {
		return layoutResult.TallestUnbreakableBlockSize
	}
	if layoutResult.Fragment == nil {
		return 0
	}
	blockSize := NewLogicalFragment(space.WritingDirection, layoutResult.Fragment).BlockSize()
	return fragmentainerBlockOffset + blockSize
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
//   - breakable: whether a break at the start of a resumed container is
//     allowed (mirrors IsBreakableAtStartOfResumedContainer). For 12d's
//     simplified path we pass false — start-of-container breaks aren't
//     supported until orphans/widows machinery (12g) needs them.
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
	refuseBreakBefore := spaceLeft >= fragmentainerBlockSize

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
			if !builder.hasBreakAppeal || appealInside < builder.breakAppeal {
				builder.SetBreakAppeal(appealInside)
			}
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
	if hasContainerSeparation {
		breakBetween := CalculateBreakBetweenValue(child, builder)
		if IsForcedBreakValue(space, breakBetween) {
			builder.SetBreakAppeal(BreakAppealPerfect)
			return BreakStatusBrokeBefore, true
		}
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

	// Compute appeal-inside + propagate any break-inside:avoid violation up
	// to the column builder so the multicol stretch loop sees it.
	appealInside := CalculateBreakAppealInside(space, layoutResult)
	if appealInside < BreakAppealPerfect {
		if !builder.hasBreakAppeal || appealInside < builder.breakAppeal {
			builder.SetBreakAppeal(appealInside)
		}
	}

	// Phase 14b: child signaled "I need more block-size than my fragment shows"
	// via LayoutResult.BlockSizeForFragmentation. Set today only by inner multicol
	// containers with column-fill:auto + explicit height that can't fit their
	// declared column block-size in the remaining outer fragmentainer space.
	// When that hint exceeds the actual remaining space and we have container
	// separation, push the child to the next outer fragmentainer instead of
	// placing a clipped fragment whose contents would leak into the outer
	// overflow area. Mirrors Blink's MovePastBreakpoint, which uses
	// BlockSizeForFragmentation (not the fragment's own size) for the
	// "block_size > space_left → break before" decision.
	if hasContainerSeparation && fragmentainerBlockSize != Indefinite &&
		layoutResult != nil && layoutResult.BlockSizeForFragmentation > 0 {
		spaceLeft := fragmentainerBlockSize - fragmentainerBlockOffset
		refuseBreakBefore := spaceLeft >= fragmentainerBlockSize
		if layoutResult.BlockSizeForFragmentation > spaceLeft && !refuseBreakBefore {
			appealBefore := CalculateBreakAppealBefore(
				space,
				CalculateBreakBetweenValue(child, builder),
				hasContainerSeparation,
				false,
			)
			builder.SetBreakAppeal(appealBefore)
			return BreakStatusBrokeBefore, false
		}
	}

	// break-inside:avoid-column on the child: if the child overflows the
	// fragmentainer AND we have container separation (so a break before it is
	// a real boundary), break before to push the whole child to the next
	// column instead of splitting it. Mirrors Blink's MovePastBreakpoint,
	// scoped narrowly to the avoid-column case so we don't take over normal
	// overflow handling (which block_layout.go's existing overflow handler
	// owns — taking it over regressed spanner-fragmentation-006/008).
	if hasContainerSeparation && fragmentainerBlockSize != Indefinite &&
		layoutResult != nil && layoutResult.Fragment != nil {
		childBreakInside := "auto"
		if s := layoutResult.Fragment.Style; s != nil {
			childBreakInside = s.GetBreakInside()
		}
		if IsAvoidBreakValue(space, childBreakInside) {
			fragment := NewLogicalFragment(space.WritingDirection, layoutResult.Fragment)
			blockSize := fragment.BlockSize()
			spaceLeft := fragmentainerBlockSize - fragmentainerBlockOffset
			refuseBreakBefore := spaceLeft >= fragmentainerBlockSize
			if blockSize > spaceLeft && !refuseBreakBefore {
				appealBefore := CalculateBreakAppealBefore(
					space,
					CalculateBreakBetweenValue(child, builder),
					hasContainerSeparation,
					/*breakableAtStartOfResumedContainer=*/ false,
				)
				builder.SetBreakAppeal(appealBefore)
				return BreakStatusBrokeBefore, false
			}
		}
	}
	return BreakStatusContinue, false
}
