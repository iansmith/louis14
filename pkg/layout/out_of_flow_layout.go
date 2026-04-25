package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// OutOfFlowCandidate records an absolutely or fixed positioned child
// discovered during normal layout. Collected by BoxFragmentBuilder,
// resolved by OutOfFlowLayoutPart after the containing block's size
// is known.
//
// Mirrors Blink's OutOfFlowPositionedCandidate.
type OutOfFlowCandidate struct {
	// Node is the layout tree node for the out-of-flow child.
	Node *LayoutInputNode

	// StaticPosition is the position the element would have in normal flow,
	// with edge annotations for alignment. Used when insets are 'auto'.
	// Expressed in the containing block's logical coordinates.
	//
	// Mirrors Blink's LogicalStaticPosition.
	StaticPosition LogicalStaticPosition

	// Alignment carries align-self / justify-self for the child, driving
	// IMCB inset bias when both insets are specified. Zero value
	// (ItemPositionNormal on both axes) yields BiasStart, matching the
	// pre-IMCB behavior.
	//
	// Mirrors Blink's LogicalAlignment passed to ComputeOofDimensions.
	Alignment LogicalAlignment

	// IsFixedPosition distinguishes position:fixed from position:absolute.
	// Fixed elements propagate past positioned ancestors to the viewport (ICB),
	// unless an ancestor with transform/filter/will-change creates a containing
	// block. Absolute elements stop at the nearest positioned ancestor.
	//
	// Mirrors Blink's NGOutOfFlowPositionedNode::is_for_fragmentation / inline_container
	// type tracking in the candidate.
	IsFixedPosition bool

	// InlineContainer, when non-nil, marks the candidate's containing block
	// as an inline element (CSS 2.1 §10.1.4 / CSS Position 3 §def-cb). The
	// CB geometry is derived from the inline's first-line and last-line
	// fragment rects at OOF resolution time via ComputeInlineContainerGeometry.
	//
	// The DOM node is stored (not the LayoutInputNode) because span
	// background fragments are matched by DOMNode identity during the
	// fragment walk; the style of the inline is available on those fragments.
	//
	// Mirrors Blink's NGOutOfFlowPositionedNode::inline_container.
	InlineContainer *html.Node
}

// OutOfFlowLayoutPart handles layout of absolutely and fixed positioned
// elements. It runs after the containing block's layout is complete,
// using the containing block's resolved dimensions.
//
// Ported from Blink's OutOfFlowLayoutPart.
type OutOfFlowLayoutPart struct {
	ctx                 *LayoutContext
	containingBlockWDM  WritingDirectionMode
	containingBlockSize    LogicalSize    // padding-box of the containing block (CSS spec §10.3.7)
	containingBlockPadding LogicalEdges   // CB's padding, used to adjust child offsets to content-box-relative
	geom                FragmentGeometry

	// resolvesFixed controls whether fixed-position descendants discovered
	// while laying out OOF children are absorbed by this CB (true) or
	// propagated to the caller for a higher CB to resolve (false).
	// True at the ICB (root) and at transform/containment CBs.
	// False at ordinary positioned ancestors which only contain absolute.
	resolvesFixed bool

	// cbOriginInBuilder is the offset of the CB's inline-start/block-start
	// corner within the resolving block's content-box. Non-zero only when
	// the CB is an inline (CSS 2.1 §10.1.4): the inline CB's origin is the
	// start-line union rect's start corner, while the resolved fragment is
	// added as a child of the block — so we shift the final per-candidate
	// AddChild offset by this amount to land it in the block's coordinate
	// space. Zero for normal block CBs (CB == this builder's content box).
	cbOriginInBuilder LogicalOffset
}

// LayoutCandidates positions all out-of-flow candidates and adds their
// fragments to the builder. Returns any fixed-position descendants that
// could not be resolved here and must propagate to a higher CB.
//
// Mirrors Blink's OutOfFlowLayoutPart::LayoutOOFNodes worklist pattern:
// laying out an OOF child can surface further OOF descendants (collected
// during the child's normal-flow pass), which must themselves be resolved
// against this CB (or propagated upward).
func (p *OutOfFlowLayoutPart) LayoutCandidates(
	candidates []OutOfFlowCandidate,
	builder *BoxFragmentBuilder,
) []OutOfFlowCandidate {
	var propagated []OutOfFlowCandidate
	worklist := candidates
	for len(worklist) > 0 {
		next := worklist
		worklist = nil
		descendants := p.layoutCandidatesOnce(next, builder)
		for _, d := range descendants {
			if p.resolvesFixed || !d.IsFixedPosition {
				worklist = append(worklist, d)
			} else {
				propagated = append(propagated, d)
			}
		}
	}
	return propagated
}

// layoutCandidatesOnce processes a single batch of candidates, adding their
// fragments to the builder and returning any OOF descendants the children
// surfaced via PropagatedOOFCandidates.
//
// Mirrors Blink's ComputeOutOfFlow{Inline,Block}Dimensions pipeline from
// absolute_utils.cc: build an Inset-Modified Containing Block (IMCB) from
// the resolved insets + static position + alignment; layout the child with
// an available-size derived from that IMCB when both insets are specified
// and the size is auto; then resolve margins and final insets via
// ComputeMargins / ComputeInsets.
//
// The IMCB math runs in CB-padding-box coordinates. Static positions from
// the candidate are content-box-relative, so we shift them by the CB's
// padding on input and subtract that padding on output — cancelling out in
// the static-based paths, and properly adjusting the inset-based paths.
func (p *OutOfFlowLayoutPart) layoutCandidatesOnce(
	candidates []OutOfFlowCandidate,
	builder *BoxFragmentBuilder,
) []OutOfFlowCandidate {
	var descendants []OutOfFlowCandidate
	cbInline := p.containingBlockSize.InlineSize
	cbBlock := p.containingBlockSize.BlockSize
	wdm := p.containingBlockWDM

	// Convert logical CB size to physical for percentage resolution of
	// inset properties (top/right/bottom/left percentages resolve against
	// physical width/height, not logical axes).
	cbPhys := ToPhysicalSize(p.containingBlockSize, wdm.WM)

	for _, candidate := range candidates {
		child := candidate.Node
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)

		// Static position in CB-content-box coords. Shift to padding-box for
		// IMCB math; we undo the shift when we convert back to the
		// content-box offset that builder.AddChild expects.
		//
		// For inline-CB resolutions the candidate's StaticPosition was captured
		// in the OWNING BLOCK's content-box coords (the line-breaker doesn't
		// know about the inline CB), so subtract cbOriginInBuilder to bring it
		// into the inline CB's coords before IMCB math runs. The final offset
		// is shifted back by the same amount at AddChild time below.
		staticInlinePad := candidate.StaticPosition.Offset.InlineOffset +
			p.containingBlockPadding.InlineStart -
			p.cbOriginInBuilder.InlineOffset
		staticBlockPad := candidate.StaticPosition.Offset.BlockOffset +
			p.containingBlockPadding.BlockStart -
			p.cbOriginInBuilder.BlockOffset
		staticInlineBias := BiasFromStaticEdge(candidate.StaticPosition.InlineEdge)
		staticBlockBias := BiasFromStaticEdge(candidate.StaticPosition.BlockEdge)

		// Resolve physical insets, then convert to logical in CB's WM.
		physOffset := childStyle.GetPositionOffsetResolved(cbPhys.Width, cbPhys.Height)
		insets := PhysicalInsetsToLogical(physOffset, wdm)
		oofInsets := insets.AsOofInsets()

		// Margins and auto-margin flags in the CB's logical coordinates.
		childMargins := ResolveMargins(childStyle, wdm, cbInline)
		rawMargin := childStyle.GetMargin()
		autoInlineStart, autoInlineEnd, autoBlockStart, autoBlockEnd :=
			PhysicalAutoMarginsToLogical(rawMargin, wdm)

		// Child's border+padding in the CB's directions.
		childGeom := ComputeFragmentGeometry(childStyle, childWDM)
		parallel := wdm.IsVertical() == childWDM.IsVertical()
		var childBPInline, childBPBlock float64
		if parallel {
			childBPInline = childGeom.InlineBorderPadding()
			childBPBlock = childGeom.BlockBorderPadding()
		} else {
			childBPInline = childGeom.BlockBorderPadding()
			childBPBlock = childGeom.InlineBorderPadding()
		}

		// IMCB (padding-box coords). align_self_direction defaults to Block —
		// align-self binds to the block axis in Blink unless explicitly set.
		imcb := ComputeUnclampedIMCB(
			LogicalSize{InlineSize: cbInline, BlockSize: cbBlock},
			candidate.Alignment,
			oofInsets,
			staticInlinePad, staticBlockPad,
			staticInlineBias, staticBlockBias,
			false, // alignSelfDirectionIsInline — block axis default
			wdm, childWDM,
		)

		// --- Pre-layout: constraint-space sizing ---
		// Fix the child's size on an axis only when both insets are specified
		// AND the size is auto. The fixed size equals IMCB.size minus non-auto
		// margins minus child border+padding (auto margins are zero for this
		// step, per CSS 2.1 §10.3.7). Other axis configurations (one inset
		// specified, or both auto) let the child size itself and fall through
		// to the post-layout IMCB resolution.
		availInline := cbInline
		availBlock := cbBlock
		useFixedInline := false
		useFixedBlock := false

		// Tables (and inline-tables) are intrinsically sized: auto width/height
		// means "size to content", not "fill available". Skip the stretched-fit
		// path so the child's own layout pass determines the size. Auto margins
		// then absorb leftover space as usual (CSS 2 §17.4 / Align 3 §8.2).
		// Replaced elements follow CSS 2.2 §10.3.7 / §10.6.5: size is resolved
		// by ComputeReplacedSize (intrinsic size + ratio + specified sizes),
		// never by stretch-fit to the IMCB. Mirrors Blink's abs-replaced
		// dispatch in absolute_utils.cc.
		stretchable := !isNonStretchableDisplay(childStyle)
		if child.DOMNode != nil && isReplacedElement(child.DOMNode) {
			stretchable = false
		}

		if stretchable && insets.HasInlineStart && insets.HasInlineEnd &&
			isAutoSizeInDirection(childStyle, wdm, true) {
			mStart := childMargins.InlineStart
			mEnd := childMargins.InlineEnd
			if autoInlineStart {
				mStart = 0
			}
			if autoInlineEnd {
				mEnd = 0
			}
			contentInline := imcb.InlineSize() - mStart - mEnd - childBPInline
			if contentInline < 0 {
				contentInline = 0
			}
			availInline = contentInline + childBPInline
			useFixedInline = true
		}

		if stretchable && insets.HasBlockStart && insets.HasBlockEnd && cbBlock != Indefinite &&
			isAutoSizeInDirection(childStyle, wdm, false) {
			mStart := childMargins.BlockStart
			mEnd := childMargins.BlockEnd
			if autoBlockStart {
				mStart = 0
			}
			if autoBlockEnd {
				mEnd = 0
			}
			contentBlock := imcb.BlockSize() - mStart - mEnd - childBPBlock
			if contentBlock < 0 {
				contentBlock = 0
			}
			availBlock = contentBlock + childBPBlock
			useFixedBlock = true
		}

		csb := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetAvailableSize(LogicalSize{
				InlineSize: availInline,
				BlockSize:  availBlock,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: cbInline,
				BlockSize:  cbBlock,
			}).
			SetPercentageResolutionInlineSize(cbInline)
		if useFixedInline {
			csb.SetIsFixedInlineSize(true)
		}
		if useFixedBlock {
			csb.SetIsFixedBlockSize(true)
		}
		childSpace := csb.Build()

		childResult := layoutElement(p.ctx, child, childSpace)
		childLogical := NewLogicalFragment(wdm, childResult.Fragment)
		usedInline := childLogical.InlineSize()
		usedBlock := childLogical.BlockSize()

		// --- Post-layout: resolve margins and insets via the IMCB ---
		// ComputeMargins handles auto-margin redistribution (centering,
		// one-auto absorption, negative-space start-dominant fallback);
		// ComputeInsets handles non-auto-margin leftover-space distribution
		// via the IMCB's bias (alignment / static edge / one-auto fallback).

		// Inline axis. Start-dominance in CSS 2.1 §10.3.7 picks the
		// direction-start edge in LTR / direction-end in RTL.
		isStartDominantInline := !wdm.IsRTL()
		marginStartI, marginEndI, appliedAutoI := ComputeMargins(
			imcb.InlineSize(),
			childMargins.InlineStart, childMargins.InlineEnd,
			autoInlineStart, autoInlineEnd,
			usedInline, imcb.HasAutoInlineInset,
			isStartDominantInline,
		)
		insetStartI, _ := ComputeInsets(
			cbInline,
			imcb.InlineStart, imcb.InlineEnd,
			imcb.InlineInsetBias,
			imcb.InlineHasDefaultAlignmentOverflow,
			imcb.InlineDefaultInsetBias, imcb.HasInlineDefaultInsetBias,
			imcb.InlineSafeInsetBias, imcb.HasInlineSafeInsetBias,
			marginStartI, marginEndI, usedInline, appliedAutoI,
		)

		// Block axis. When the CB block size is indefinite the IMCB math is
		// not meaningful — fall back to the simple per-case formulas the
		// pre-IMCB code used (which only consults insets that have values).
		var finalBlockOffset float64
		if cbBlock != Indefinite {
			marginStartB, marginEndB, appliedAutoB := ComputeMargins(
				imcb.BlockSize(),
				childMargins.BlockStart, childMargins.BlockEnd,
				autoBlockStart, autoBlockEnd,
				usedBlock, imcb.HasAutoBlockInset,
				true, // block axis is always start-dominant
			)
			insetStartB, _ := ComputeInsets(
				cbBlock,
				imcb.BlockStart, imcb.BlockEnd,
				imcb.BlockInsetBias,
				imcb.BlockHasDefaultAlignmentOverflow,
				imcb.BlockDefaultInsetBias, imcb.HasBlockDefaultInsetBias,
				imcb.BlockSafeInsetBias, imcb.HasBlockSafeInsetBias,
				marginStartB, marginEndB, usedBlock, appliedAutoB,
			)
			finalBlockOffset = insetStartB + marginStartB - p.containingBlockPadding.BlockStart
		} else {
			// Indefinite-block fallback. staticBlockPad is
			// content-box-relative-plus-padding; in either inset-based or
			// static-based case the final conversion subtracts padding so the
			// result comes out content-box-relative.
			var blockPad float64
			switch {
			case insets.HasBlockStart:
				blockPad = insets.BlockStart + childMargins.BlockStart
			case insets.HasBlockEnd:
				// cbBlock is Indefinite here — the child is positioned from
				// bottom, but we have no definite CB. Best effort: anchor the
				// child at inset-end from its own block-size (matches the
				// pre-IMCB formula).
				blockPad = -insets.BlockEnd - childMargins.BlockEnd - usedBlock
			default:
				switch candidate.StaticPosition.BlockEdge {
				case StaticEdgeEnd:
					blockPad = staticBlockPad - childMargins.BlockEnd - usedBlock
				default:
					blockPad = staticBlockPad + childMargins.BlockStart
				}
			}
			finalBlockOffset = blockPad - p.containingBlockPadding.BlockStart
		}

		finalInlineOffset := insetStartI + marginStartI -
			p.containingBlockPadding.InlineStart

		builder.AddChild(childResult.Fragment, LogicalOffset{
			InlineOffset: finalInlineOffset + p.cbOriginInBuilder.InlineOffset,
			BlockOffset:  finalBlockOffset + p.cbOriginInBuilder.BlockOffset,
		})

		// Descendant OOFs the laid-out child surfaced need their static
		// positions translated from the child's content-box coordinates into
		// this CB's content-box coordinates before the worklist re-processes
		// them here. Mirrors block_layout.go's PropagateOOFCandidates, except
		// the adjustment is applied against the freshly-resolved child offset
		// rather than a normal-flow block cursor.
		if len(childResult.PropagatedOOFCandidates) > 0 {
			childBPLogicalInChild := LogicalEdges{
				InlineStart: childGeom.Border.InlineStart + childGeom.Padding.InlineStart,
				InlineEnd:   childGeom.Border.InlineEnd + childGeom.Padding.InlineEnd,
				BlockStart:  childGeom.Border.BlockStart + childGeom.Padding.BlockStart,
				BlockEnd:    childGeom.Border.BlockEnd + childGeom.Padding.BlockEnd,
			}
			physBPEdges := ToPhysicalEdges(childBPLogicalInChild, childWDM)
			parentBP := ToLogicalEdges(physBPEdges, wdm)
			// Inline-CB resolutions add cbOriginInBuilder to AddChild offsets;
			// match that here so propagated descendants' static positions land
			// in the resolving block's content-box coords (not the inline CB's).
			inlineAdj := finalInlineOffset + parentBP.InlineStart + p.cbOriginInBuilder.InlineOffset
			blockAdj := finalBlockOffset + parentBP.BlockStart + p.cbOriginInBuilder.BlockOffset

			needsConv := childWDM.WM != wdm.WM || childWDM.Dir != wdm.Dir
			var childContentPhys PhysicalSize
			if needsConv {
				childContentPhys = PhysicalSize{
					Width:  childResult.Fragment.Size.WidthF64() - physBPEdges.Left - physBPEdges.Right,
					Height: childResult.Fragment.Size.HeightF64() - physBPEdges.Top - physBPEdges.Bottom,
				}
				if childContentPhys.Width < 0 {
					childContentPhys.Width = 0
				}
				if childContentPhys.Height < 0 {
					childContentPhys.Height = 0
				}
			}

			for _, cand := range childResult.PropagatedOOFCandidates {
				adj := cand
				if needsConv {
					physSP := adj.StaticPosition.ConvertToPhysical(childWDM, childContentPhys)
					adj.StaticPosition = physSP.ConvertToLogical(wdm, childContentPhys)
				}
				adj.StaticPosition.Offset.InlineOffset += inlineAdj
				adj.StaticPosition.Offset.BlockOffset += blockAdj
				descendants = append(descendants, adj)
			}
		}
	}
	return descendants
}

// isNonStretchableDisplay reports whether a display type has intrinsic
// (content-based) sizing and therefore should not stretch to fill the IMCB
// when both insets are specified. Tables follow CSS 2 §17.5 table sizing,
// independent of the inset-based stretch-fit rule for block-level non-replaced
// elements. Mirrors Blink's node.IsTable() gate in absolute_utils.cc's
// ComputeOofBlockDimensions/ComputeOofInlineDimensions.
func isNonStretchableDisplay(style *css.Style) bool {
	d := style.GetDisplay()
	return d == css.DisplayTable || d == css.DisplayInlineTable
}

// isAutoSizeInDirection checks if the child element has auto size in the
// given direction of the containing block's writing mode.
// If inline is true, checks the CB's inline axis; otherwise the block axis.
func isAutoSizeInDirection(childStyle interface {
	GetLength(string) (float64, bool)
	GetPercentage(string) (float64, bool)
}, cbWDM WritingDirectionMode, inline bool) bool {
	// Determine the physical CSS property that controls this axis.
	var prop string
	if inline {
		// CB's inline axis: width in HTB, height in vertical.
		prop = "width"
		if cbWDM.IsVertical() {
			prop = "height"
		}
	} else {
		// CB's block axis: height in HTB, width in vertical.
		prop = "height"
		if cbWDM.IsVertical() {
			prop = "width"
		}
	}

	if _, ok := childStyle.GetLength(prop); ok {
		return false
	}
	if _, ok := childStyle.GetPercentage(prop); ok {
		return false
	}
	return true
}
