package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
)

// Float positioning and fragmentation. Mirrors Blink's floats_utils.cc
// (@ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// createConstraintSpaceForFloat builds the constraint space for laying out
// an unpositioned float. A fragmentable float whose border edge is at
// fragmentainerOffset gets the fragmentainer, unless it is monolithic
// (SetupSpaceBuilderForFragmentation's early return,
// fragmentation_utils.cc:336-339); it is at the fragmentainer start when
// its block-start MARGIN edge is. Mirrors Blink's
// CreateConstraintSpaceForFloat (floats_utils.cc:64-104).
func createConstraintSpaceForFloat(uf *UnpositionedFloat, isFragmentable, isMonolithic bool, fragmentainerOffset float64, margins LogicalEdges) ConstraintSpace {
	parentSpace := uf.ParentSpace
	childWDM := NewWritingDirectionMode(uf.Node.Style())
	b := NewConstraintSpaceBuilder(parentSpace.WritingDirection, childWDM, true).
		SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, uf.Ctx)).
		SetOrthogonalFallbackBlockSize(parentSpace.OrthogonalFallbackBlockSize).
		SetAvailableSize(uf.AvailableSize).
		SetPercentageResolutionSize(uf.PercentageSize).
		SetPercentageResolutionInlineSize(uf.PercentageSize.InlineSize)
	if uf.Token != nil {
		b.SetBreakToken(uf.Token)
	}
	if isFragmentable && !isMonolithic {
		SetupSpaceBuilderForFragmentation(parentSpace, fragmentainerOffset, uf.FragmentainerBlockSize,
			/*requiresContentBeforeBreaking=*/ false, b) // floats_utils.cc:86
		if isFloatAtFragmentainerStart(fragmentainerOffset, margins.BlockStart) {
			b.SetIsAtFragmentainerStart(true)
		}
	}
	return b.Build()
}

// isFloatAtFragmentainerStart reports whether a float whose border edge sits
// at fragmentainerOffset is at the fragmentainer start: for floats it is the
// block-start margin edge that matters, since float margins are unbreakable
// and never truncated. Mirrors floats_utils.cc:89-96.
func isFloatAtFragmentainerStart(fragmentainerOffset, marginBlockStart float64) bool {
	return fragmentainerOffset-marginBlockStart <= 0
}

// findLayoutOpportunityForFloat returns the BFC block offset of the float's
// margin box: the highest position at or below its origin, past any
// clearance, where its margin box fits beside the floats already placed.
// Mirrors Blink's FindLayoutOpportunityForFloat (floats_utils.cc:40-58);
// louis14's exclusion space has no top-edge-alignment bookkeeping
// (LastFloatBlockStart) and searches with the margin-box block-size too.
func findLayoutOpportunityForFloat(uf *UnpositionedFloat, es *ExclusionSpace, margins LogicalEdges, fragment LogicalFragment) float64 {
	wdm := uf.ParentSpace.WritingDirection
	blockStart := uf.OriginBfcBlockOffset
	if clearType := uf.ClearType(); clearType != css.ClearNone {
		blockStart = max(blockStart,
			es.ClearanceOffset(clearType, layoutunit.FromFloat64Round(blockStart), wdm).Float64())
	}
	return es.FindFloatPositionLocal(uf.FloatSide(),
		fragment.InlineSize()+margins.InlineSum(), fragment.BlockSize()+margins.BlockSum(),
		uf.AvailableSize.InlineSize, blockStart, uf.OriginBfcLineOffset, uf.BfcContainerInlineSize)
}

// layoutFloat lays the float out in space and tags its fragment as a
// float. LOU-365 (CodeRabbit 3514362226): the parent-side monolithic tag is
// for writing-mode roots the float's own layout cannot see (parallel
// flips), as at the in-flow child walk.
func layoutFloat(uf *UnpositionedFloat, space ConstraintSpace) *LayoutResult {
	result := layoutElement(uf.Ctx, uf.Node, space)
	if result.Fragment != nil {
		result.Fragment.IsFloating = true
		wdm := uf.ParentSpace.WritingDirection
		if uf.Node.IsMonolithic(wdm.WM != NewWritingDirectionMode(uf.Node.Style()).WM) {
			result.Fragment.IsMonolithic = true
		}
	}
	return result
}

// layoutFloatWithoutFragmentation lays the float out without block
// fragmentation, once, caching the result and margins on uf. Mirrors
// Blink's LayoutFloatWithoutFragmentation (floats_utils.cc:171-189).
func layoutFloatWithoutFragmentation(uf *UnpositionedFloat) {
	if uf.LayoutResult != nil {
		return
	}
	uf.LayoutResult = layoutFloat(uf, createConstraintSpaceForFloat(uf, false, false, 0, LogicalEdges{}))
	uf.Margins = ResolveMargins(uf.Node.Style(), uf.ParentSpace.WritingDirection, uf.PercentageSize.InlineSize)
}

// ComputeMarginBoxInlineSizeForUnpositionedFloat returns the float's
// margin-box inline-size, laying it out without fragmentation (the line
// breaker needs it before the float is positioned). Mirrors Blink's
// ComputeMarginBoxInlineSizeForUnpositionedFloat (floats_utils.cc:193-210).
func ComputeMarginBoxInlineSizeForUnpositionedFloat(uf *UnpositionedFloat) float64 {
	layoutFloatWithoutFragmentation(uf)
	return max(0, NewLogicalFragment(uf.ParentSpace.WritingDirection, uf.LayoutResult.Fragment).InlineSize()+
		uf.Margins.InlineSum())
}

// PositionFloat lays out and positions a float, adding its exclusion to
// *exclusionSpace. Under block fragmentation the float is laid out at its
// final position with the fragmentainer, so it may break inside (its
// result then carries a break token: the float resumes in the next
// fragmentainer, in a parallel flow) — or, if it does not fit and is past
// the fragmentainer start, it breaks before: it is not placed, and a
// break-before token is returned instead. Mirrors Blink's PositionFloat
// (floats_utils.cc:212-475).
func PositionFloat(uf *UnpositionedFloat, exclusionSpace **ExclusionSpace) PositionedFloat {
	es := *exclusionSpace
	parentSpace := uf.ParentSpace
	wdm := parentSpace.WritingDirection
	style := uf.Node.Style()
	childWDM := NewWritingDirectionMode(style)
	isFragmentable := childWDM.WM == wdm.WM && parentSpace.HasBlockFragmentation
	isMonolithic := uf.Node.IsMonolithic(wdm.WM != childWDM.WM)

	// Without block fragmentation the float is laid out once, reusing the
	// result from measuring its inline-size in an inline formatting
	// context. Under fragmentation its block-offset must be known before
	// layout: from the measured inline-size when there is one, else
	// optimistically at the offset it gets if no other float affects it,
	// laying it out again when it got pushed down (relayout deeper in the
	// fragmentainer can only shrink the fragment). A monolithic float gets
	// no fragmentainer, so its layout does not depend on the offset
	// (floats_utils.cc:230-312).
	var result *LayoutResult
	var margins LogicalEdges
	var opportunityBlockOffset float64
	if !isFragmentable {
		layoutFloatWithoutFragmentation(uf)
		result, margins = uf.LayoutResult, uf.Margins
		opportunityBlockOffset = findLayoutOpportunityForFloat(uf, es, margins, NewLogicalFragment(wdm, result.Fragment))
	} else {
		margins = ResolveMargins(style, wdm, uf.PercentageSize.InlineSize)
		AdjustMarginsForFragmentation(uf.Token, &margins)
		layout := func(bfcBlockOffset float64) *LayoutResult {
			return layoutFloat(uf, createConstraintSpaceForFloat(uf, true, isMonolithic,
				uf.FragmentainerOffsetAtBfc()+bfcBlockOffset, margins))
		}
		originBorderBfcOffset := uf.OriginBfcBlockOffset + margins.BlockStart
		measured := uf.LayoutResult
		if measured == nil {
			result = layout(originBorderBfcOffset)
			measured = result
		}
		opportunityBlockOffset = findLayoutOpportunityForFloat(uf, es, margins, NewLogicalFragment(wdm, measured.Fragment))
		if bfcBlockOffset := opportunityBlockOffset + margins.BlockStart; isMonolithic {
			// Laid out without the fragmentainer either way.
			result = measured
		} else if result == nil || bfcBlockOffset > originBorderBfcOffset {
			result = layout(bfcBlockOffset)
		}
	}
	fragment := NewLogicalFragment(wdm, result.Fragment)
	bfcBlockOffset := opportunityBlockOffset + margins.BlockStart
	fragmentainerBlockOffset := uf.FragmentainerOffsetAtBfc() + bfcBlockOffset
	// At the fragmentainer start: the border edge for a float laid out
	// with the fragmentainer (SetupSpaceBuilderForFragmentation), else —
	// and in any case — the margin edge (floats_utils.cc:89-96).
	isAtFragmentainerStart := isFloatAtFragmentainerStart(fragmentainerBlockOffset, margins.BlockStart) ||
		(!isMonolithic && fragmentainerBlockOffset <= 0)

	// Past the fragmentainer start the float may break before; at the
	// start it cannot, or there would be no progress. Float margins never
	// collapse with or split at a fragmentainer boundary
	// (floats_utils.cc:321-389).
	needBreakBefore := false
	if isFragmentable && !isAtFragmentainerStart {
		breakToken := result.BreakToken
		isAtBlockEnd := breakToken == nil || breakToken.IsAtBlockEnd
		if !isAtBlockEnd {
			// It resumes in the next fragmentainer (or is pushed there
			// whole): no block-end margin here.
			margins.BlockEnd = 0
		}
		if !MovePastBreakpoint(parentSpace, result, fragmentainerBlockOffset,
			uf.FragmentainerBlockSize, BreakAppealPerfect, nil) {
			needBreakBefore = true
		} else if isAtBlockEnd && uf.FragmentainerBlockSize != Indefinite {
			// A block-end margin of a float does not collapse with the
			// fragmentainer end: avoid breaking inside it.
			outerBlockEnd := fragmentainerBlockOffset + fragment.BlockSize() + margins.BlockEnd
			if outerBlockEnd > uf.FragmentainerBlockSize && !uf.Token.IsBreakInside() {
				needBreakBefore = true
			}
		}
	}

	// A float cannot start above a preceding float pushed to the next
	// fragmentainer, nor clear past one continuing there
	// (floats_utils.cc:390-393).
	if parentSpace.HasBlockFragmentation && !needBreakBefore && !uf.Token.IsBreakInside() &&
		es.NeedsBreakBeforeFloat(uf.ClearType(), wdm) {
		needBreakBefore = true
	}

	floatSide := uf.FloatSide()
	exclusionSide := PhysicalFloatToExclusionSide(floatSide, wdm)
	positioned := PositionedFloat{
		LayoutResult:   result,
		BfcBlockOffset: bfcBlockOffset,
	}
	if needBreakBefore {
		// A zero-size exclusion past everything, so the containers may
		// grow to encompass the floats, and a record that no later float
		// fits in this fragmentainer (floats_utils.cc:397-411). It takes
		// no inline space: an inline-end one sits at the line-right edge.
		pastEverything := Exclusion{
			BlockOffset: uf.FragmentainerSpaceLeft() + uf.ExpectedBfcBlockOffset,
			Side:        exclusionSide,
		}
		if exclusionSide == ExclusionInlineEnd {
			pastEverything.InlineOffset = uf.BfcContainerInlineSize
		}
		es = es.Add(pastEverything).SetHasBreakBeforeFloat(exclusionSide)
		positioned.BreakBeforeToken = &BlockBreakToken{Node: uf.Node, IsBreakBefore: true}
	} else {
		// The float's inline position. float:left/right are physical;
		// placement works in inline-start/end, swapped in RTL.
		floatInlineSize := margins.InlineSum() + fragment.InlineSize()
		floatBlockSize := margins.BlockSum() + fragment.BlockSize()
		localStartOff, localEndOff := es.FindLocalFloatOffsets(opportunityBlockOffset, floatBlockSize,
			uf.OriginBfcLineOffset, uf.AvailableSize.InlineSize, uf.BfcContainerInlineSize)
		if exclusionSide == ExclusionInlineStart {
			positioned.BfcLineOffset = uf.OriginBfcLineOffset + localStartOff + margins.InlineStart
		} else {
			containerEnd := uf.OriginBfcLineOffset + uf.AvailableSize.InlineSize
			positioned.BfcLineOffset = containerEnd - localEndOff - margins.InlineEnd - fragment.InlineSize()
		}

		// The float's exclusion (CreateExclusionArea, floats_utils.cc:
		// 149-169); if it broke inside, it takes up space in the next
		// fragmentainer too, so nothing clearing it fits here
		// (:412-424).
		es = es.Add(Exclusion{
			InlineOffset: positioned.BfcLineOffset - margins.InlineStart,
			BlockOffset:  opportunityBlockOffset,
			InlineSize:   floatInlineSize,
			BlockSize:    floatBlockSize,
			Side:         exclusionSide,
		})
		if breakToken := result.BreakToken; breakToken != nil && !breakToken.IsAtBlockEnd {
			es = es.SetHasBreakInsideFloat(exclusionSide)
		}
	}
	*exclusionSpace = es

	// What column balancing needs: the space shortage when the float broke
	// before or inside, or — while the column block-size is unknown — the
	// unbreakable block-size, margins included but not the part above the
	// fragmentainer start (floats_utils.cc:442-470).
	if parentSpace.HasBlockFragmentation && parentSpace.BlockFragmentationType == FragmentColumn {
		if uf.FragmentainerBlockSize != Indefinite {
			if positioned.BreakBeforeToken != nil || result.BreakToken != nil {
				positioned.MinimumSpaceShortage = CalculateSpaceShortage(
					parentSpace, result, fragmentainerBlockOffset, uf.FragmentainerBlockSize)
			}
		} else if ShouldAvoidBreakInside(parentSpace, result) {
			marginBlockStart := margins.BlockStart
			if result.Fragment.BoxData != nil {
				marginBlockStart = ToLogicalEdges(result.Fragment.BoxData.Margin, wdm).BlockStart
			}
			positioned.TallestUnbreakableBlockSize = CalculateUnbreakableBlockSize(
				parentSpace, result, fragmentainerBlockOffset-marginBlockStart)
		}
	}
	return positioned
}
