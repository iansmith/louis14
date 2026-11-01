package layout

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
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

	// BreakToken is the resume-from break token for this OOF, populated
	// when the OOF spans multiple fragmentainers (e.g. an abspos taller
	// than a column). At resume time it carries OofStartOffset (set on a
	// previous fragment's break token) so the OOF picks up where it left
	// off. Phase 25 — mirrors Blink's `OofPositionedNode::break_token_`
	// (oof_positioned_node.h:171-235; persisted on the base of every
	// derived OOF descendant type).
	BreakToken *BlockBreakToken

	// RequiresContentBeforeBreaking signals that this OOF cannot be
	// pushed past the next fragmentainer break before any content has
	// been placed in the current one. Consumed by break-decision code
	// in the fragmentation utilities. Phase 25 — mirrors Blink's
	// `OofPositionedNode::RequiresContentBeforeBreaking()`
	// (oof_positioned_node.h:212-214).
	RequiresContentBeforeBreaking bool
}

// OutOfFlowLayoutPart lays out the out-of-flow candidates a container
// collected once the container's size is known: those it contains are laid
// out against its padding box (or, in block fragmentation, deferred to the
// fragmentation context root), the others are handed to an ancestor.
//
// Ported from Blink's OutOfFlowLayoutPart (out_of_flow_layout_part.h,
// out_of_flow_layout_part.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Every container constructs one from its builder with
// NewOutOfFlowLayoutPart and calls Run.
type OutOfFlowLayoutPart struct {
	ctx                    *LayoutContext
	containerBuilder       *BoxFragmentBuilder
	containingBlockWDM     WritingDirectionMode
	containingBlockSize    LogicalSize  // padding-box of the containing block (CSS spec §10.3.7)
	containingBlockPadding LogicalEdges // CB's padding, used to adjust child offsets to content-box-relative

	// isAbsoluteContainer / isFixedContainer are the container node's
	// IsAbsoluteContainer / IsFixedContainer (Blink's is_absolute_container_
	// / is_fixed_container_, out_of_flow_layout_part.cc:544-545).
	isAbsoluteContainer bool
	isFixedContainer    bool

	// shouldAddOuterFragmentainerChildren defers the candidates the
	// container contains to the fragmentation context root, because the
	// container takes part in block fragmentation (Blink's
	// should_add_outer_fragmentainer_children_, :546-548).
	shouldAddOuterFragmentainerChildren bool
	// isInitialColumnBalancingPass lays the contained candidates out here
	// even then (:1179-1203).
	isInitialColumnBalancingPass bool
	// previousConsumedBlockSize / previousConsumedContentBlockSize are what
	// the container's earlier fragments consumed (border-box and
	// content-box), for AdjustFragmentainerDescendant.
	previousConsumedBlockSize        float64
	previousConsumedContentBlockSize float64

	// cbOriginInBuilder is the offset of the CB's inline-start/block-start
	// corner within the resolving block's content-box. Non-zero only when
	// the CB is an inline (CSS 2.1 §10.1.4): the inline CB's origin is the
	// start-line union rect's start corner, while the resolved fragment is
	// added as a child of the block — so we shift the final per-candidate
	// AddChild offset by this amount to land it in the block's coordinate
	// space. Zero for normal block CBs (CB == this builder's content box).
	cbOriginInBuilder LogicalOffset
}

// NewOutOfFlowLayoutPart returns the OutOfFlowLayoutPart of the container
// builder lays out, laid out in space. paddingBoxSize and padding are the
// container's padding-box size and padding (the containing block for the
// candidates it contains); the root element's containing block is the
// initial containing block (the viewport) instead, and the root contains
// every candidate. previousConsumedContentBlockSize is the content
// block-size the container's earlier fragments consumed. Mirrors Blink's
// OutOfFlowLayoutPart constructor (out_of_flow_layout_part.cc:542-613 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14's root element stands
// in for the LayoutView, and the caller supplies the padding box because
// louis14 builders do not all have their size and borders set yet.
func NewOutOfFlowLayoutPart(ctx *LayoutContext, builder *BoxFragmentBuilder, space ConstraintSpace, paddingBoxSize LogicalSize, padding LogicalEdges, previousConsumedContentBlockSize float64) *OutOfFlowLayoutPart {
	p := &OutOfFlowLayoutPart{
		ctx:                    ctx,
		containerBuilder:       builder,
		containingBlockWDM:     space.WritingDirection,
		containingBlockSize:    paddingBoxSize,
		containingBlockPadding: padding,
	}
	if space.IsRootElement {
		// InitialContainingBlockFixedSize (:531-540): the viewport, with no
		// border or padding.
		p.containingBlockSize = ToLogicalSize(PhysicalSize{Width: ctx.ViewportWidth, Height: ctx.ViewportHeight}, space.WritingDirection.WM)
		p.containingBlockPadding = LogicalEdges{}
		p.isAbsoluteContainer, p.isFixedContainer = true, true
		return p
	}
	if node := builder.layoutNode; node != nil {
		p.isAbsoluteContainer = node.IsAbsoluteContainer()
		p.isFixedContainer = node.IsFixedContainer()
	}
	p.shouldAddOuterFragmentainerChildren = InvolvedInBlockFragmentation(space)
	p.isInitialColumnBalancingPass = space.IsInitialColumnBalancingPass
	p.previousConsumedBlockSize = previouslyConsumedBlockSize(space.BreakToken)
	p.previousConsumedContentBlockSize = previousConsumedContentBlockSize
	return p
}

// Run lays out the candidates collected on the container builder that the
// container contains and returns the others, for an ancestor containing
// block (Blink's AddOutOfFlowDescendant; louis14 hands them up as the
// result's PropagatedOOFCandidates). Mirrors Blink's
// OutOfFlowLayoutPart::Run (out_of_flow_layout_part.cc:615-687 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a multicol runs
// HandleFragmentation first itself (MulticolLayoutAlgorithm.
// HandleOofFragmentation).
func (p *OutOfFlowLayoutPart) Run() []OutOfFlowCandidate {
	var candidates []OutOfFlowCandidate
	candidates, p.containerBuilder.outOfFlowCandidates = p.containerBuilder.outOfFlowCandidates, nil
	if len(candidates) > 0 {
		return p.LayoutCandidates(candidates)
	}
	p.containerBuilder.AdjustFixedposContainingBlockForFragmentainerDescendants(p.previousConsumedBlockSize)
	p.containerBuilder.AdjustFixedposContainingBlockForInnerMulticols(p.previousConsumedBlockSize, p.previousConsumedContentBlockSize)
	return nil
}

// IsContainingBlockForCandidate reports whether the container contains
// candidate: a fixed container contains fixed-position boxes, an absolute
// container absolutely positioned ones. Mirrors Blink's
// OutOfFlowLayoutPart::IsContainingBlockForCandidate
// (out_of_flow_layout_part.cc:2746-2791 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); an inline containing block is
// handled by LayoutCandidates, and fragmentainers never construct one.
func (p *OutOfFlowLayoutPart) IsContainingBlockForCandidate(candidate OutOfFlowCandidate) bool {
	if candidate.IsFixedPosition {
		return p.isFixedContainer
	}
	return p.isAbsoluteContainer
}

// LayoutCandidates lays out, in order, the candidates the container
// contains — sweeping the candidates each one surfaces (typically fixed
// descendants it does not contain) before its siblings, to keep tree order
// — defers them when the container is involved in block fragmentation, and
// returns the candidates it does not contain. Mirrors Blink's
// OutOfFlowLayoutPart::LayoutCandidates (out_of_flow_layout_part.cc:
// 1178-1291 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A candidate whose
// containing block is an inline is laid out at the first block holding the
// inline's line boxes, against their union rect (the inline branch of
// IsContainingBlockForCandidate, :2780-2788, with ComputeInlineContainingBlocks,
// :916-941); with no line boxes here it is treated as a regular candidate.
func (p *OutOfFlowLayoutPart) LayoutCandidates(candidates []OutOfFlowCandidate) []OutOfFlowCandidate {
	var propagated []OutOfFlowCandidate
	for _, candidate := range candidates {
		if candidate.InlineContainer != nil {
			if inlinePart := p.inlineContainingBlockPart(candidate.InlineContainer); inlinePart != nil {
				propagated = append(propagated, p.LayoutCandidates(inlinePart.layoutCandidate(candidate))...)
				continue
			}
			// CSS 2.1 §9.4.2 suppresses a line box whose only content is
			// OOF items (e.g. <span pos:relative><div pos:abs/></span>
			// with no text), so the inline has no geometry to derive a CB
			// from: an enclosing containing block resolves it.
			candidate.InlineContainer = nil
		}
		if !p.IsContainingBlockForCandidate(candidate) {
			propagated = append(propagated, candidate)
			continue
		}
		if p.shouldAddOuterFragmentainerChildren && !p.isInitialColumnBalancingPass {
			// The static position arrives fragment-local in this CB's
			// content-box coordinates; AdjustFragmentainerDescendant makes
			// it CB-stitched. The CB's outgoing fragment isn't built yet, so
			// the parent's PropagateOOFFragmentainerDescendants fills it in.
			d := LogicalOofNodeForFragmentation{Candidate: candidate}
			p.containerBuilder.AdjustFragmentainerDescendant(&d, false, p.previousConsumedBlockSize, p.previousConsumedContentBlockSize)
			p.containerBuilder.AdjustFixedposContainingBlockForInnerMulticols(p.previousConsumedBlockSize, p.previousConsumedContentBlockSize)
			p.containerBuilder.AddOutOfFlowFragmentainerDescendant(d)
			continue
		}
		propagated = append(propagated, p.LayoutCandidates(p.layoutCandidate(candidate))...)
	}
	return propagated
}

// inlineContainingBlockPart returns the OutOfFlowLayoutPart that lays out a
// candidate against the union rect of inline's line-box fragments in the
// container builder, or nil when inline produced none here.
func (p *OutOfFlowLayoutPart) inlineContainingBlockPart(inline *html.Node) *OutOfFlowLayoutPart {
	wdm := p.containingBlockWDM
	contentSize := LogicalSize{
		InlineSize: max(0, p.containingBlockSize.InlineSize-p.containingBlockPadding.InlineSum()),
		BlockSize:  max(0, p.containingBlockSize.BlockSize-p.containingBlockPadding.BlockSum()),
	}
	blockContentPhys := ToPhysicalSize(contentSize, wdm.WM)
	inlineGeom := ComputeInlineContainerGeometry(p.containerBuilder.children, inline, wdm, blockContentPhys)
	if inlineGeom == nil {
		return nil
	}
	inlinePart := *p
	inlinePart.containingBlockSize, inlinePart.cbOriginInBuilder = inlineGeom.InlineCBLogical(wdm, blockContentPhys)
	inlinePart.containingBlockPadding = LogicalEdges{}
	return &inlinePart
}

// layoutCandidate lays candidate out against this containing block, adds
// its fragment to the container builder and returns the candidates it
// surfaced, in the container's content-box coordinates. The part of Blink's
// LayoutCandidates after CalculateOffset (out_of_flow_layout_part.cc:
// 1205-1283 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *OutOfFlowLayoutPart) layoutCandidate(candidate OutOfFlowCandidate) []OutOfFlowCandidate {
	info, ok := p.CalculateOffset(candidate)
	if !ok {
		return nil
	}
	// Inline-CB resolutions add cbOriginInBuilder to the offset, so
	// propagated descendants' static positions land in the resolving
	// block's content-box coords (not the inline CB's).
	offset := LogicalOffset{
		InlineOffset: info.Offset.InlineOffset + p.cbOriginInBuilder.InlineOffset,
		BlockOffset:  info.Offset.BlockOffset + p.cbOriginInBuilder.BlockOffset,
	}
	p.containerBuilder.AddChild(info.result.Fragment, offset)
	if p.isInitialColumnBalancingPass {
		p.containerBuilder.PropagateTallestUnbreakableBlockSize(info.result.TallestUnbreakableBlockSize)
	}
	return p.propagatedDescendants(info.result, info, offset)
}

// propagatedDescendants returns the OOF descendants childResult surfaced,
// with their static positions translated from the child's content-box
// coordinates into the coordinates childOffset (the child's border-box
// offset) is expressed in. Mirrors BoxFragmentBuilder.PropagateOOFPositionedInfo,
// except the adjustment is applied against the resolved child offset rather
// than a normal-flow block cursor.
func (p *OutOfFlowLayoutPart) propagatedDescendants(childResult *LayoutResult, info OffsetInfo, childOffset LogicalOffset) []OutOfFlowCandidate {
	if len(childResult.PropagatedOOFCandidates) == 0 {
		return nil
	}
	wdm := p.containingBlockWDM
	childGeom, childWDM := info.childGeom, info.childWDM
	childBPLogicalInChild := LogicalEdges{
		InlineStart: childGeom.Border.InlineStart + childGeom.Padding.InlineStart,
		InlineEnd:   childGeom.Border.InlineEnd + childGeom.Padding.InlineEnd,
		BlockStart:  childGeom.Border.BlockStart + childGeom.Padding.BlockStart,
		BlockEnd:    childGeom.Border.BlockEnd + childGeom.Padding.BlockEnd,
	}
	physBPEdges := ToPhysicalEdges(childBPLogicalInChild, childWDM)
	parentBP := ToLogicalEdges(physBPEdges, wdm)
	inlineAdj := childOffset.InlineOffset + parentBP.InlineStart
	blockAdj := childOffset.BlockOffset + parentBP.BlockStart

	needsConv := childWDM.WM != wdm.WM || childWDM.Dir != wdm.Dir
	var childContentPhys PhysicalSize
	if needsConv {
		childContentPhys = PhysicalSize{
			Width:  max(0, childResult.Fragment.Size.WidthF64()-physBPEdges.Left-physBPEdges.Right),
			Height: max(0, childResult.Fragment.Size.HeightF64()-physBPEdges.Top-physBPEdges.Bottom),
		}
	}

	var out []OutOfFlowCandidate
	for _, cand := range childResult.PropagatedOOFCandidates {
		adj := cand
		if needsConv {
			physSP := adj.StaticPosition.ConvertToPhysical(childWDM, childContentPhys)
			adj.StaticPosition = physSP.ConvertToLogical(wdm, childContentPhys)
		}
		adj.StaticPosition.Offset.InlineOffset += inlineAdj
		adj.StaticPosition.Offset.BlockOffset += blockAdj
		out = append(out, adj)
	}
	return out
}

// OffsetInfo is the result of CalculateOffset: the candidate's offset in
// the containing block's content box, the space it was sized in and its
// unfragmented layout result. Mirrors Blink's OutOfFlowLayoutPart::OffsetInfo
// (out_of_flow_layout_part.h:236-268 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 keeps the sizing
// layout's result and space, which a fragmented re-layout starts from
// (GenerateFragment).
type OffsetInfo struct {
	// Offset is the margin-box-adjusted border-box offset in the containing
	// block's content-box coordinates (cbOriginInBuilder not applied).
	Offset LogicalOffset
	// spaceBuilder is the constraint-space builder the candidate was sized
	// with; GenerateFragment copies it.
	spaceBuilder ConstraintSpaceBuilder
	// availableSize is the space's available size, in the containing
	// block's logical coordinates.
	availableSize LogicalSize
	// result is the unfragmented layout result.
	result    *LayoutResult
	childGeom FragmentGeometry
	childWDM  WritingDirectionMode
}

// CalculateOffset sizes the candidate and resolves its offset against this
// containing block. Mirrors Blink's OutOfFlowLayoutPart::CalculateOffset
// (out_of_flow_layout_part.cc:2150-2342 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) with the IMCB math of
// absolute_utils.cc: build an Inset-Modified Containing Block (IMCB) from
// the resolved insets + static position + alignment; lay out the child with
// an available-size derived from that IMCB when both insets are specified
// and the size is auto; then resolve margins and final insets via
// ComputeMargins / ComputeInsets. ok is false for a candidate without style.
//
// The IMCB math runs in CB-padding-box coordinates. Static positions from
// the candidate are content-box-relative, so we shift them by the CB's
// padding on input and subtract that padding on output — cancelling out in
// the static-based paths, and properly adjusting the inset-based paths.
func (p *OutOfFlowLayoutPart) CalculateOffset(candidate OutOfFlowCandidate) (OffsetInfo, bool) {
	cbInline := p.containingBlockSize.InlineSize
	cbBlock := p.containingBlockSize.BlockSize
	wdm := p.containingBlockWDM

	// Convert logical CB size to physical for percentage resolution of
	// inset properties (top/right/bottom/left percentages resolve against
	// physical width/height, not logical axes).
	cbPhys := ToPhysicalSize(p.containingBlockSize, wdm.WM)

	child := candidate.Node
	childStyle := child.Style()
	if childStyle == nil {
		return OffsetInfo{}, false
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
	// Exception: form controls are semi-replaced (input, textarea, select, button).
	// They have intrinsic sizes but must stretch when positioned absolutely with
	// explicit insets and auto width/height (CSS Position §10.3.7, csswg#6789).
	// Blink does not treat them as LayoutReplaced, so they naturally take the
	// stretch path; we mirror that by not blocking stretch for semi-replaced elements.
	stretchable := !isNonStretchableDisplay(childStyle)
	if child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
		// Check if this is a semi-replaced element (form control).
		if !html.IsSemiReplacedElementTag(child.DOMNode.TagName) {
			stretchable = false
		}
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

	return OffsetInfo{
		Offset:        LogicalOffset{InlineOffset: finalInlineOffset, BlockOffset: finalBlockOffset},
		spaceBuilder:  *csb,
		availableSize: LogicalSize{InlineSize: availInline, BlockSize: availBlock},
		result:        childResult,
		childGeom:     childGeom,
		childWDM:      childWDM,
	}, true
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
	Get(string) (string, bool)
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
	// CSS Values 4 §10: a math function (calc / min / max / clamp) on
	// width/height is also a definite (non-auto) size — GetLength returns
	// false here because the percent term won't resolve without a base.
	// Mirrors fragment_geometry.Resolve* which DOES resolve such values
	// downstream. Treating it as auto would route the OOF child through
	// the "fill IMCB" stretch-fit path, which then ignores the explicit
	// width entirely (regression in css-values/vh-calc-support-pct).
	if val, ok := childStyle.Get(prop); ok && css.IsMathFunction(val) {
		return false
	}
	return true
}

// NodeToLayout is one fragmentainer descendant being laid out at the
// fragmentation context root: the descendant, its offset info, and the
// fragmentainer-relative offset and break token it starts or resumes with.
// Mirrors Blink's OutOfFlowLayoutPart::NodeToLayout / NodeInfo
// (out_of_flow_layout_part.h:198-290 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 keeps the containing
// block's relative offset and the sizing part beside it.
type NodeToLayout struct {
	descendant LogicalOofNodeForFragmentation
	offsetInfo OffsetInfo
	// offset is relative to the fragmentainer the node starts or resumes
	// in. It includes the containing block's position:relative offset
	// (Blink's base_container_info.relative_offset): the containing block's
	// fragments are found at link offsets with relative positioning applied.
	offset LogicalOffset
	// originalOffset is the offset in stacked-fragmentainer coordinates
	// (Blink's offset_info.original_offset, out_of_flow_layout_part.cc:
	// 1643-1644).
	originalOffset LogicalOffset
	breakToken     *BlockBreakToken
}

// containingBlockInfo is a fragmentainer descendant's containing block,
// stitched across the fragmentainers: its padding-box size (block-size from
// the total of its fragments, Blink's BoxTotalBlockSize), its padding, and its
// content-box offset in stacked-fragmentainer coordinates (as if every
// fragmentainer were stacked in the block direction). Mirrors the fragmentation branch of Blink's
// OutOfFlowLayoutPart::GetContainingBlockInfo (out_of_flow_layout_part.cc:
// 842-899 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with
// layout_box_utils.h:15-19 BoxTotalBlockSize. louis14 has no per-box
// fragment list (plan G20), so the CB's fragments are found by walking the
// fragmentainers' fragment trees.
type containingBlockInfo struct {
	size                 LogicalSize
	padding              LogicalEdges
	contentStackedOffset LogicalOffset
}

// fragmentationContextOOFPart is the OutOfFlowLayoutPart of a block
// fragmentation context root (louis14: the outermost multicol, or the
// limited builder holding an inner multicol's columns in
// LayoutOOFsInMulticol): it lays out the fragmentainer descendants bubbled
// up to the root in its fragmentainers. Mirrors the fragmentation half of
// Blink's OutOfFlowLayoutPart, constructed on the root's container_builder_
// (out_of_flow_layout_part.cc:542-613 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type fragmentationContextOOFPart struct {
	OutOfFlowLayoutPart
	// childFragmentStorage, when set, holds the fragmentainers instead of
	// the container builder's children (Blink's child_fragment_storage_).
	childFragmentStorage *[]logicalChildLink
	// columnBalancingInfo, when set, makes this a column-balancing dry run
	// over childFragmentStorage (Blink's column_balancing_info_).
	columnBalancingInfo *ColumnBalancingInfo
	// isContainingBlockUnfinished reports whether a containing block's last
	// fragment so far has a break token, so its fragmentainer descendants
	// wait (Blink's delayed_descendants_, out_of_flow_layout_part.cc:
	// 1611-1621). Nil when every containing block has finished.
	isContainingBlockUnfinished func(*LayoutInputNode) bool
	// delayedDescendants are the descendants that waited.
	delayedDescendants []LogicalOofNodeForFragmentation
}

// ColumnBalancingInfo collects what laying out a column line's
// fragmentainer descendants would need — new columns, the minimal space
// shortage, whether a monolithic OOF overflowed — without adding their
// results, so that OOFs affect column balancing. Mirrors Blink's
// OutOfFlowLayoutPart::ColumnBalancingInfo (out_of_flow_layout_part.h:47-84
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// louis14 passes the line's descendants to LayoutFragmentainerDescendants
// rather than storing them here (Blink's
// out_of_flow_fragmentainer_descendants).
type ColumnBalancingInfo struct {
	// MinimalSpaceShortage is the smallest positive space shortage found
	// laying them out in the line's columns (0: none).
	MinimalSpaceShortage float64
	// NumNewColumns is the number of columns they need past the line's.
	NumNewColumns int
	// HasViolatingBreak is set when a monolithic OOF had to overflow.
	HasViolatingBreak bool
}

// PropagateSpaceShortage records a space shortage. Mirrors Blink's
// ColumnBalancingInfo::PropagateSpaceShortage
// (out_of_flow_layout_part.cc:3291-3294 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (info *ColumnBalancingInfo) PropagateSpaceShortage(spaceShortage float64) {
	info.MinimalSpaceShortage = MinPositiveSpaceShortage(info.MinimalSpaceShortage, spaceShortage)
}

// fragmentationContextChildren returns the fragmentainers (and spanners)
// being laid out into. Mirrors Blink's
// OutOfFlowLayoutPart::FragmentationContextChildren.
func (p *fragmentationContextOOFPart) fragmentationContextChildren() *[]logicalChildLink {
	if p.childFragmentStorage != nil {
		return p.childFragmentStorage
	}
	return &p.containerBuilder.children
}

// HandleFragmentation lays out the fragmentainer descendants bubbled up to
// this fragmentation context root, and those of the inner multicols with
// pending OOFs, until none remain, then hands the delayed ones back to the
// container builder. Mirrors Blink's OutOfFlowLayoutPart::HandleFragmentation
// (out_of_flow_layout_part.cc:707-788 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the early return for a nested
// root is the caller's (MulticolLayoutAlgorithm.HandleOofFragmentation). A
// column balancing dry run calls LayoutFragmentainerDescendants directly
// with the line's descendants (Blink's ColumnBalancingInfo branch).
func (p *fragmentationContextOOFPart) HandleFragmentation(fragmentainerProgression LogicalOffset) {
	for p.containerBuilder.HasOutOfFlowFragmentainerDescendants() || p.containerBuilder.HasMulticolsWithPendingOOFs() {
		p.HandleMulticolsWithPendingOOFs()
		var descendants []LogicalOofNodeForFragmentation
		p.containerBuilder.SwapOutOfFlowFragmentainerDescendants(&descendants)
		if len(descendants) > 0 {
			p.LayoutFragmentainerDescendants(&descendants, fragmentainerProgression)
		}
	}
	for _, d := range p.delayedDescendants {
		p.containerBuilder.AddOutOfFlowFragmentainerDescendant(d)
	}
}

// HandleMulticolsWithPendingOOFs lays out the OOFs of every inner multicol
// with pending OOFs, including inner multicols that surface while doing so,
// each once. Mirrors Blink's OutOfFlowLayoutPart::
// HandleMulticolsWithPendingOOFs (out_of_flow_layout_part.cc:1293-1322 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The multicols are handled in
// fragment-tree order (Blink iterates a hash map; louis14 keeps layout
// deterministic).
func (p *fragmentationContextOOFPart) HandleMulticolsWithPendingOOFs() {
	handled := map[*LayoutInputNode]bool{}
	var pending map[*LayoutInputNode]*MulticolWithPendingOOFs
	p.containerBuilder.SwapMulticolsWithPendingOOFs(&pending)
	for len(pending) > 0 {
		multicols, fragments := p.multicolFragments(pending)
		for i, multicol := range multicols {
			p.LayoutOOFsInMulticol(fragments[i], pending[multicol])
			handled[multicol] = true
		}
		// Inner multicols may have been added while handling outer ones.
		pending = nil
		p.containerBuilder.SwapMulticolsWithPendingOOFs(&pending)
		for multicol := range handled {
			delete(pending, multicol)
		}
	}
}

// multicolFragments returns the multicols of pending, in the order their
// first fragment appears in this fragmentation context's fragment tree, with
// each one's fragments in order: Blink's LayoutBox::PhysicalFragments, which
// louis14 lacks (plan G20), found in one walk. No Blink analog for the
// ordering, which keeps HandleMulticolsWithPendingOOFs deterministic.
func (p *fragmentationContextOOFPart) multicolFragments(pending map[*LayoutInputNode]*MulticolWithPendingOOFs) ([]*LayoutInputNode, [][]*PhysicalFragment) {
	var multicols []*LayoutInputNode
	var fragments [][]*PhysicalFragment
	index := map[*LayoutInputNode]int{}
	var walk func(*PhysicalFragment)
	walk = func(f *PhysicalFragment) {
		for _, ch := range f.Children {
			if ch.Fragment == nil {
				continue
			}
			if n := ch.Fragment.LayoutNode; n != nil && pending[n] != nil {
				i, ok := index[n]
				if !ok {
					i = len(multicols)
					index[n] = i
					multicols = append(multicols, n)
					fragments = append(fragments, nil)
				}
				fragments[i] = append(fragments[i], ch.Fragment)
			}
			walk(ch.Fragment)
		}
	}
	for _, ch := range *p.fragmentationContextChildren() {
		walk(ch.fragment)
	}
	return multicols, fragments
}

// LayoutOOFsInMulticol lays out the fragmentainer descendants pending in an
// inner multicol in its columns: the columns of every fragment of the
// multicol (multicolFragments, in order) are collected into a limited
// builder, and the descendants are laid out in them. Columns created for
// OOFs that run past the last one are appended to the last multicol fragment
// that has columns, so they overflow in the inline direction (Blink does not
// put columns into more outer fragmentainers, a documented limitation). The
// candidates the laid-out OOFs surface (a fixed child of an abspos) and the
// inner multicols they bring go to this part's container builder, at the
// multicol's offset. Mirrors Blink's OutOfFlowLayoutPart::LayoutOOFsInMulticol
// (out_of_flow_layout_part.cc:1324-1557 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A fixed box whose fixedpos
// containing block is outside the inner multicol is handed back as a
// fragmentainer descendant contained by it (TransferOutOfFlowCandidates).
// Not ported: outer_context_has_fixedpos_container_'s column consumed-size
// adjustment for an inner multicol that itself breaks across outer
// fragmentainers (:3071-3087).
func (p *fragmentationContextOOFPart) LayoutOOFsInMulticol(multicolFragments []*PhysicalFragment, info *MulticolWithPendingOOFs) {
	first := multicolFragments[0]
	wdm := NewWritingDirectionMode(first.Style)
	limitedBuilder := NewBoxFragmentBuilder(wdm)
	limitedBuilder.SetLayoutNode(first.LayoutNode)
	availableInlineSize := NewLogicalFragment(wdm, first).InlineSize() - ComputeFragmentGeometry(first.Style, wdm).InlineBorderPadding()
	columnInlineProgression := ColumnInlineProgression(first.Style, availableInlineSize)

	var multicolChildren []logicalChildLink
	var lastFragmentWithFragmentainer *PhysicalFragment
	var oofNodesToLayout []LogicalOofNodeForFragmentation
	for _, multicolFragment := range multicolFragments {
		// Collect the children of the multicol fragments, with the
		// fragmentainer descendants pending in them. Child offsets are
		// relative to their multicol fragment's content box (louis14's
		// child-link convention; horizontal-tb, like the rest of OOF
		// fragmentation, plan RC-8).
		for _, child := range multicolFragment.Children {
			if child.Fragment == nil {
				continue
			}
			offset := LogicalOffset{InlineOffset: child.Offset.LeftF64(), BlockOffset: child.Offset.TopF64()}
			if child.Fragment.IsFragmentainerBox() {
				lastFragmentWithFragmentainer = multicolFragment
			}
			multicolChildren = append(multicolChildren, logicalChildLink{offset: offset, fragment: child.Fragment})
			var descendants []LogicalOofNodeForFragmentation
			limitedBuilder.PropagateOOFFragmentainerDescendants(child.Fragment, offset, LogicalOffset{}, layoutunit.LayoutUnit{}, nil, nil, &descendants)
			for _, d := range descendants {
				// A descendant whose containing block is inside a column
				// spanner is laid out further up the tree.
				if d.ContainingBlock.Fragment == nil || d.ContainingBlock.IsInsideColumnSpanner {
					continue
				}
				oofNodesToLayout = append(oofNodesToLayout, d)
			}
		}
	}
	if len(oofNodesToLayout) == 0 {
		return
	}

	oldFragmentCount := len(multicolChildren)
	innerPart := &fragmentationContextOOFPart{
		OutOfFlowLayoutPart:  OutOfFlowLayoutPart{ctx: p.ctx, containerBuilder: limitedBuilder, containingBlockWDM: wdm},
		childFragmentStorage: &multicolChildren,
	}
	innerPart.LayoutFragmentainerDescendants(&oofNodesToLayout, LogicalOffset{InlineOffset: columnInlineProgression})

	// Append the columns created for the OOFs to the last multicol fragment
	// that has columns (:1488-1535).
	if len(multicolChildren) > oldFragmentCount && lastFragmentWithFragmentainer != nil {
		for _, child := range multicolChildren[oldFragmentCount:] {
			lastFragmentWithFragmentainer.Children = append(lastFragmentWithFragmentainer.Children, ChildLink{
				Offset:   geometry.PhysicalOffsetFromF64Round(child.offset.InlineOffset, child.offset.BlockOffset),
				Fragment: child.fragment,
			})
		}
		lastFragmentWithFragmentainer.updateScrollableOverflow()
	}

	limitedBuilder.TransferOutOfFlowCandidates(p.containerBuilder, info.MulticolOffset, info)
	var multicolsWithPendingOOFs map[*LayoutInputNode]*MulticolWithPendingOOFs
	limitedBuilder.SwapMulticolsWithPendingOOFs(&multicolsWithPendingOOFs)
	for node, pending := range multicolsWithPendingOOFs {
		p.containerBuilder.AddMulticolWithPendingOOFs(node, pending)
	}
}

// maxOOFFragmentainers bounds the fragmentainers one OOF layout round may
// reach. No Blink analog: Blink relies on every fragmented OOF making
// progress; louis14 guards against a non-advancing break token hanging the
// engine.
const maxOOFFragmentainers = 1000

// LayoutFragmentainerDescendants lays out every fragmentainer descendant in
// the fragmentainers (column boxes) of the container builder, creating
// fragmentainers for those that run past the last one, and repeats for the
// fragmentainer descendants the laid-out OOFs surface. Mirrors Blink's
// OutOfFlowLayoutPart::LayoutFragmentainerDescendants
// (out_of_flow_layout_part.cc:1559-1784 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Not ported: inline
// containing blocks, anchors and repeated fixedpos (pagination).
func (p *fragmentationContextOOFPart) LayoutFragmentainerDescendants(descendants *[]LogicalOofNodeForFragmentation, fragmentainerProgression LogicalOffset) {
	for len(*descendants) > 0 {
		var descendantsToLayout [][]NodeToLayout
		for _, d := range *descendants {
			if p.isContainingBlockUnfinished != nil && d.ContainingBlock.Fragment != nil &&
				p.isContainingBlockUnfinished(d.ContainingBlock.Fragment.LayoutNode) {
				p.delayedDescendants = append(p.delayedDescendants, d)
				continue
			}
			cbInfo, ok := p.GetContainingBlockInfo(d)
			if !ok {
				continue
			}
			cbPart := &OutOfFlowLayoutPart{
				ctx:                    p.ctx,
				containingBlockWDM:     p.containingBlockWDM,
				containingBlockSize:    cbInfo.size,
				containingBlockPadding: cbInfo.padding,
			}
			offsetInfo, ok := cbPart.CalculateOffset(d.Candidate)
			if !ok {
				continue
			}
			// The offset in stacked-fragmentainer coordinates
			// (out_of_flow_layout_part.cc:2636-2641).
			offset := LogicalOffset{
				InlineOffset: cbInfo.contentStackedOffset.InlineOffset + offsetInfo.Offset.InlineOffset,
				BlockOffset:  cbInfo.contentStackedOffset.BlockOffset + offsetInfo.Offset.BlockOffset,
			}
			originalOffset := offset
			blockEstimate := NewLogicalFragment(p.containingBlockWDM, offsetInfo.result.Fragment).BlockSize()
			startIndex := p.ComputeStartFragmentIndexAndRelativeOffset(blockEstimate, &offset)
			if startIndex >= maxOOFFragmentainers {
				continue
			}
			for len(descendantsToLayout) <= startIndex {
				descendantsToLayout = append(descendantsToLayout, nil)
			}
			descendantsToLayout[startIndex] = append(descendantsToLayout[startIndex], NodeToLayout{
				descendant:     d,
				offsetInfo:     offsetInfo,
				offset:         offset,
				originalOffset: originalOffset,
			})
		}

		var fragmentedDescendants []NodeToLayout
		for index := 0; index < len(descendantsToLayout) && index < maxOOFFragmentainers; index++ {
			children := *p.fragmentationContextChildren()
			if index >= len(children) && p.columnBalancingInfo != nil {
				p.columnBalancingInfo.NumNewColumns++
			}
			lastHasBreakInside := false
			// Skip over any column spanners.
			if index >= len(children) || children[index].fragment.IsFragmentainerBox() {
				lastHasBreakInside = p.LayoutOOFsInFragmentainer(descendantsToLayout[index], index, fragmentainerProgression, &fragmentedDescendants)
			}
			// Extend descendantsToLayout when an OOF fragments into a
			// fragmentainer past the last one listed (:1730-1735).
			if index == len(descendantsToLayout)-1 &&
				(lastHasBreakInside || (len(fragmentedDescendants) > 0 && index+1 < len(*p.fragmentationContextChildren()))) {
				descendantsToLayout = append(descendantsToLayout, nil)
			}
		}

		// Sweep the fragmentainer descendants the laid-out OOFs bubbled up
		// to the container builder (nested abspos) — not in a column
		// balancing dry run, which propagates none (:1771-1773).
		*descendants = nil
		if p.columnBalancingInfo == nil {
			p.containerBuilder.SwapOutOfFlowFragmentainerDescendants(descendants)
		}
	}
}

// GetContainingBlockInfo builds the stitched containing-block info for a
// fragmentainer descendant (see containingBlockInfo). ok is false when none
// of the containing block's fragments is inside a fragmentainer (e.g. it is
// inside a column spanner, plan WI-6).
func (p *fragmentationContextOOFPart) GetContainingBlockInfo(d LogicalOofNodeForFragmentation) (containingBlockInfo, bool) {
	cbFragment := d.ContainingBlock.Fragment
	if cbFragment == nil || cbFragment.LayoutNode == nil || cbFragment.Style == nil {
		return containingBlockInfo{}, false
	}
	cbNode := cbFragment.LayoutNode
	type cbFragmentInFragmentainer struct {
		fragment      *PhysicalFragment
		offsetInFrag  LogicalOffset
		stackedOffset float64 // the fragmentainer's stacked block offset
	}
	var found []cbFragmentInFragmentainer
	stacked := 0.0
	for _, ch := range *p.fragmentationContextChildren() {
		if !ch.fragment.IsFragmentainerBox() {
			continue
		}
		walkFragmentsForNode(ch.fragment, cbNode, LogicalOffset{}, func(f *PhysicalFragment, off LogicalOffset) {
			found = append(found, cbFragmentInFragmentainer{f, off, stacked})
		})
		stacked += ClampedToValidFragmentainerCapacity(NewLogicalFragment(p.containingBlockWDM, ch.fragment).BlockSize())
	}
	if len(found) == 0 {
		return containingBlockInfo{}, false
	}
	first := found[0]
	totalBlockSize := 0.0
	for _, f := range found {
		totalBlockSize += NewLogicalFragment(p.containingBlockWDM, f.fragment).BlockSize()
	}
	// The padding box: borders and scrollbars contracted
	// (out_of_flow_layout_part.cc:870-878).
	cbGeom := ComputeFragmentGeometry(cbFragment.Style, p.containingBlockWDM)
	bsp := cbGeom.BorderBoxPadding()
	inlineSize := NewLogicalFragment(p.containingBlockWDM, first.fragment).InlineSize()
	return containingBlockInfo{
		size: LogicalSize{
			InlineSize: max(0, inlineSize-cbGeom.Border.InlineStart-cbGeom.Border.InlineEnd-cbGeom.Scrollbar.InlineStart-cbGeom.Scrollbar.InlineEnd),
			BlockSize:  max(0, totalBlockSize-cbGeom.Border.BlockStart-cbGeom.Border.BlockEnd-cbGeom.Scrollbar.BlockStart-cbGeom.Scrollbar.BlockEnd),
		},
		padding: cbGeom.Padding,
		contentStackedOffset: LogicalOffset{
			InlineOffset: first.offsetInFrag.InlineOffset + bsp.InlineStart,
			BlockOffset:  first.stackedOffset + first.offsetInFrag.BlockOffset + bsp.BlockStart,
		},
	}, true
}

// walkFragmentsForNode calls visit for every fragment of node under frag,
// with its offset in frag's content box. Horizontal-tb only, like the rest
// of louis14's OOF fragmentation (plan RC-8).
func walkFragmentsForNode(frag *PhysicalFragment, node *LayoutInputNode, parentOffset LogicalOffset, visit func(*PhysicalFragment, LogicalOffset)) {
	for _, ch := range frag.Children {
		if ch.Fragment == nil {
			continue
		}
		off := LogicalOffset{
			InlineOffset: parentOffset.InlineOffset + ch.Offset.LeftF64(),
			BlockOffset:  parentOffset.BlockOffset + ch.Offset.TopF64(),
		}
		if ch.Fragment.LayoutNode == node {
			visit(ch.Fragment, off)
			continue
		}
		if bd := ch.Fragment.BoxData; bd != nil {
			off.InlineOffset += bd.Border.Left + bd.Padding.Left
			off.BlockOffset += bd.Border.Top + bd.Padding.Top
		}
		walkFragmentsForNode(ch.Fragment, node, off, visit)
	}
}

// ComputeStartFragmentIndexAndRelativeOffset returns the index of the
// fragmentainer an OOF at the stacked-coordinate offset starts in, and makes
// offset relative to that fragmentainer. Past the last fragmentainer it
// extrapolates with the last fragmentainer's block-size. Mirrors Blink's
// OutOfFlowLayoutPart::ComputeStartFragmentIndexAndRelativeOffset
// (out_of_flow_layout_part.cc:3183-3250 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the clipped-container clamp
// is paginated-only and not ported.
func (p *fragmentationContextOOFPart) ComputeStartFragmentIndexAndRelativeOffset(blockEstimate float64, offset *LogicalOffset) int {
	usedBlockSize, currentMaxBlockSize, fragmentainerBlockSize := 0.0, 0.0, 0.0
	children := *p.fragmentationContextChildren()
	childIndex := 0
	for ; childIndex < len(children); childIndex++ {
		child := children[childIndex].fragment
		if !child.IsFragmentainerBox() {
			continue
		}
		fragmentainerBlockSize = ClampedToValidFragmentainerCapacity(NewLogicalFragment(p.containingBlockWDM, child).BlockSize())
		currentMaxBlockSize += fragmentainerBlockSize
		if offset.BlockOffset < currentMaxBlockSize ||
			(offset.BlockOffset == currentMaxBlockSize && blockEstimate == 0) {
			offset.BlockOffset -= usedBlockSize
			return childIndex
		}
		usedBlockSize = currentMaxBlockSize
	}
	remaining := offset.BlockOffset - usedBlockSize
	additional := 0
	if fragmentainerBlockSize > 0 {
		additional = int(math.Floor(remaining / fragmentainerBlockSize))
	}
	offset.BlockOffset = remaining - float64(additional)*fragmentainerBlockSize
	return childIndex + additional
}

// LayoutOOFsInFragmentainer lays out, in the fragmentainer at index, the
// OOFs continuing from the previous one (continuations first) and those
// starting in it, creating the fragmentainer when index is past the last
// one. Continuations it produces replace *fragmentedDescendants. The early
// return omits Blink's !is_last_fragmentainer_so_far condition, which only
// refreshes the last fragmentainer's outgoing break token (louis14 column
// fragments carry none). Reports
// whether an OOF broke inside it. Mirrors Blink's
// OutOfFlowLayoutPart::LayoutOOFsInFragmentainer
// (out_of_flow_layout_part.cc:2904-3026 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *fragmentationContextOOFPart) LayoutOOFsInFragmentainer(pending []NodeToLayout, index int, fragmentainerProgression LogicalOffset, fragmentedDescendants *[]NodeToLayout) bool {
	childrenPtr := p.fragmentationContextChildren()
	numChildren := len(*childrenPtr)
	isNewFragment := index >= numChildren
	continued := *fragmentedDescendants
	*fragmentedDescendants = nil
	if len(pending) == 0 && len(continued) == 0 && !isNewFragment {
		return false
	}

	// Find a non-spanner fragmentainer as a basis.
	lastFragmentainerIndex := min(index, numChildren-1)
	for lastFragmentainerIndex > 0 && !(*childrenPtr)[lastFragmentainerIndex].fragment.IsFragmentainerBox() {
		lastFragmentainerIndex--
	}
	fragmentainerOffset := (*childrenPtr)[lastFragmentainerIndex].offset
	if isNewFragment {
		previous := (*childrenPtr)[lastFragmentainerIndex].fragment
		fragmentainerOffset.InlineOffset += fragmentainerProgression.InlineOffset
		fragmentainerOffset.BlockOffset += fragmentainerProgression.BlockOffset
		*childrenPtr = append(*childrenPtr, logicalChildLink{offset: fragmentainerOffset, fragment: CreateEmptyColumn(p.containingBlockWDM, previous)})
	}
	fragmentainer := (*childrenPtr)[index].fragment
	fragmentainerSize := NewLogicalFragment(p.containingBlockWDM, fragmentainer)

	hasActualBreakInside := false
	for _, list := range [][]NodeToLayout{continued, pending} {
		for i := range list {
			if p.AddOOFToFragmentainer(&list[i], fragmentainer, fragmentainerSize.BlockSize(), fragmentainerOffset, fragmentedDescendants) {
				hasActualBreakInside = true
			}
		}
	}
	// Don't update the fragmentainer when performing column balancing.
	if p.columnBalancingInfo == nil {
		fragmentainer.updateScrollableOverflow()
	}
	return hasActualBreakInside
}

// AddOOFToFragmentainer lays the OOF out in the fragmentainer, merges its
// fragment into the fragmentainer (Blink's SimplifiedOofLayoutAlgorithm +
// Merge, simplified_oof_layout_algorithm.cc:14-66 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; louis14 fragments are
// mutable, so the link is appended in place), queues a continuation when it
// broke, and propagates the OOF descendants it surfaced to the container
// builder: regular candidates (e.g. a fixed child whose containing block is
// outside the fragmentation context) at their offset in the root, and
// fragmentainer descendants for the next round. Mirrors Blink's
// OutOfFlowLayoutPart::AddOOFToFragmentainer (out_of_flow_layout_part.cc:
// 3028-3152 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Reports whether
// the OOF broke. Blink's single PropagateOOFPositionedInfo call is split in
// two here (regular candidates, fragmentainer descendants); the
// containing-block adjustment and fixedpos containing block it passes are
// not ported (plan WI-3).
func (p *fragmentationContextOOFPart) AddOOFToFragmentainer(n *NodeToLayout, fragmentainer *PhysicalFragment, fragmentainerBlockSize float64, fragmentainerOffset LogicalOffset, fragmentedDescendants *[]NodeToLayout) bool {
	result := p.GenerateFragment(n, fragmentainerBlockSize)
	if result == nil || result.Fragment == nil {
		return false
	}
	brokeInside := false
	if result.BreakToken != nil {
		continuation := *n
		continuation.breakToken = result.BreakToken
		// Resume at the block-start of the next fragmentainer, past any
		// monolithic overflow (:3092-3106).
		continuation.offset.BlockOffset = result.BreakToken.MonolithicOverflow
		*fragmentedDescendants = append(*fragmentedDescendants, continuation)
		brokeInside = true
	}

	// In a column balancing dry run, only note how the OOF affects
	// balancing (:3108-3130). OOFs ignore break rules, but a monolithic one
	// that does not fit overflows instead of moving to the next
	// fragmentainer: that is a violating break.
	if info := p.columnBalancingInfo; info != nil {
		spaceShortage := CalculateSpaceShortage(p.fragmentainerSpace(), result, n.offset.BlockOffset, fragmentainerBlockSize)
		info.PropagateSpaceShortage(spaceShortage)
		if spaceShortage > 0 && result.BreakToken == nil {
			info.HasViolatingBreak = true
		}
		return brokeInside
	}

	offset := n.offset
	conv := NewConverter(p.containingBlockWDM, geomSizeToOld(fragmentainer.Size))
	physOff := conv.ToPhysicalOffset(offset, geomSizeToOld(result.Fragment.Size))
	fragmentainer.Children = append(fragmentainer.Children, ChildLink{
		Offset:   geometry.PhysicalOffsetFromF64Round(physOff.X, physOff.Y),
		Fragment: result.Fragment,
	})

	inRoot := LogicalOffset{
		InlineOffset: fragmentainerOffset.InlineOffset + offset.InlineOffset,
		BlockOffset:  fragmentainerOffset.BlockOffset + offset.BlockOffset,
	}
	// A fixed descendant whose fixedpos containing block is inside this
	// fragmentation context becomes a fragmentainer descendant contained by
	// it, its static position relative to that containing block: the OOF's
	// stacked offset less the containing block's, plus what the OOF's
	// earlier fragments consumed (additional_fixedpos_offset and the
	// fixedpos branch of PropagateOOFPositionedInfo,
	// out_of_flow_layout_part.cc:3057-3070, fragment_builder.cc:783-816 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Relative offsets between
	// the OOF and the containing block are not applied (louis14 finds the
	// containing block's fragments where they were placed).
	fixedposCB := n.descendant.FixedposContainingBlock
	var fixedpos []OutOfFlowCandidate
	if fixedposCB.Fragment != nil {
		if fixedposCBInfo, ok := p.GetContainingBlockInfo(LogicalOofNodeForFragmentation{ContainingBlock: fixedposCB}); ok {
			additional := LogicalOffset{
				InlineOffset: n.originalOffset.InlineOffset - fixedposCBInfo.contentStackedOffset.InlineOffset,
				BlockOffset:  n.originalOffset.BlockOffset - fixedposCBInfo.contentStackedOffset.BlockOffset,
			}
			if n.breakToken != nil {
				additional.BlockOffset += n.breakToken.ConsumedBlockSize.Float64()
			}
			fixedpos = p.propagatedDescendants(result, n.offsetInfo, additional)
		}
	}
	for i, cand := range p.propagatedDescendants(result, n.offsetInfo, inRoot) {
		if fixedpos != nil && cand.IsFixedPosition {
			p.containerBuilder.AddOutOfFlowFragmentainerDescendant(LogicalOofNodeForFragmentation{
				Candidate:               fixedpos[i],
				ContainingBlock:         fixedposCB,
				FixedposContainingBlock: fixedposCB,
				FixedposInlineContainer: n.descendant.FixedposInlineContainer,
			})
			continue
		}
		p.containerBuilder.AddOutOfFlowCandidate(cand)
	}
	p.containerBuilder.PropagateOOFFragmentainerDescendants(result.Fragment, inRoot, LogicalOffset{}, layoutunit.LayoutUnit{}, nil, nil, nil)
	return brokeInside
}

// GenerateFragment lays the OOF out in a fragmentainer: at its resolved
// inline size, with the space it was sized in, set up for fragmentation at
// its offset in the fragmentainer, resuming from its break token. Mirrors
// Blink's OutOfFlowLayoutPart::GenerateFragment (out_of_flow_layout_part.cc:
// 2793-2902 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and the space of
// GetFragmentainerConstraintSpace (:3154-3181, kBreakAppealLastResort).
// Divergences: the fragmentainer space is a column one (louis14's only
// fragmentation context root); the available block-size is the one the OOF
// was sized with (the CB's, or the IMCB's when stretched) rather than
// offset_info.imcb_block_size, and the block auto behaviour is not ported.
func (p *fragmentationContextOOFPart) GenerateFragment(n *NodeToLayout, fragmentainerBlockSize float64) *LayoutResult {
	info := n.offsetInfo
	csb := info.spaceBuilder
	usedInline := NewLogicalFragment(p.containingBlockWDM, info.result.Fragment).InlineSize()
	csb.SetAvailableSize(LogicalSize{InlineSize: usedInline, BlockSize: info.availableSize.BlockSize}).
		SetIsFixedInlineSize(true)
	SetupSpaceBuilderForFragmentation(p.fragmentainerSpace(), n.offset.BlockOffset, fragmentainerBlockSize,
		n.descendant.Candidate.RequiresContentBeforeBreaking, &csb)
	csb.SetBreakToken(n.breakToken)
	return layoutElement(p.ctx, n.descendant.Candidate.Node, csb.Build())
}

// fragmentainerSpace is the part of a fragmentainer's constraint space the
// OOFs laid out in it read: a column, with kBreakAppealLastResort (OOFs
// never break and resume in a subsequent outer fragmentainer). Mirrors the
// space of Blink's GetFragmentainerConstraintSpace
// (out_of_flow_layout_part.cc:3154-3181 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *fragmentationContextOOFPart) fragmentainerSpace() ConstraintSpace {
	return ConstraintSpace{
		WritingDirection:       p.containingBlockWDM,
		BlockFragmentationType: FragmentColumn,
		MinBreakAppeal:         BreakAppealLastResort,
	}
}
