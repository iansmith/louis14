package layout

import (
	"slices"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
)

// BlockLayoutAlgorithm implements the block formatting context layout.
// It positions block-level children sequentially in the block direction,
// handling margin collapsing, auto sizing, floats, and clear.
//
// Ported from Blink's BlockLayoutAlgorithm.
type BlockLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace

	// fragmentainerOffsetForChildren is the fragmentainer offset of this
	// block's content edge — the origin every child block offset, line
	// offset and float offset is measured from. bla.space.FragmentainerOffset
	// is the block's own border edge (Blink ConstraintSpace::
	// FragmentainerOffset(), constraint_space.h:335-346 @ a9f50e522efa), so
	// children sit one block-start border+scrollbar+padding further in.
	// Mirrors Blink's LayoutAlgorithm::FragmentainerOffsetForChildren()
	// (layout_algorithm.h:164-166) as consumed by BreakBeforeChildIfNeeded
	// and CreateConstraintSpaceForChild, whose BFC block offsets are past
	// the container's BorderScrollbarPadding() (block_layout_algorithm.cc:
	// 3156-3158, :3620-3633). Set once in Layout() from the fragment
	// geometry; meaningful only when HasBlockFragmentation.
	//
	// The block-start edge counts only on the first fragment: Layout()
	// clears it from the fragment geometry on a slice-mode continuation
	// (Blink ClearBorderScrollbarPaddingBlockStart, fragmentation_utils.cc:
	// 504-510), so a resumed fragment has consumed nothing before its first
	// child and MovePastBreakpoint's refuse_break_before (:1169-1171) holds
	// again — counting a repeated edge re-pushes the first child from every
	// column and never terminates. A box-decoration-break:clone box repeats
	// the edge on every fragment, so FragmentainerOffset(is_for_children)
	// pulls the offset back by it (fragmentation_utils.h:205-235) and
	// children measure against fragmentainerCapacityForChildren, which
	// excludes the cloned edges: a child at the content edge is again at
	// the fragmentainer start.
	fragmentainerOffsetForChildren float64

	// containerBuilder is the builder of the fragment being laid out,
	// created in Layout(). Mirrors Blink's LayoutAlgorithm::
	// container_builder_ (layout_algorithm.h:299 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which Blink's
	// FragmentainerCapacity / FragmentainerOffset / FragmentainerSpaceLeft
	// read (fragmentation_utils.h:153-257). The only access path to the
	// builder: Layout and its helpers read the field.
	containerBuilder *BoxFragmentBuilder

	// boxType is the type of fragment this algorithm produces. A column box
	// (fragmentainer) is sized by its multicol, not by FinishFragmentation.
	// Mirrors Blink's LayoutAlgorithm::SetBoxType (column_layout_algorithm.cc:
	// 945, :1639 @ a9f50e522efa).
	boxType BoxType

	// earlyBreak, when set, is where to break: this algorithm is the
	// relayout of a block that found a better breakpoint than where it ran
	// out of space (a line for orphans / widows, or a child block, before
	// or inside it), or this block's parent is such a relayout and the
	// breakpoint is inside this block (ConstraintSpace.EarlyBreak). Mirrors
	// Blink's LayoutAlgorithm::early_break_ (layout_algorithm.h:295-297 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	earlyBreak *EarlyBreak

	// relayoutSpaceShortage is the space shortage the pass that found
	// earlyBreak detected. Breaking earlier, the relayout does not run out
	// of space, so it repeats that shortage. Mirrors LayoutAlgorithm::
	// SetupRelayoutData's kRelayoutForEarlyBreak branch (layout_algorithm.h:
	// 210-216 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	relayoutSpaceShortage float64

	// firstOverflowingLine is the line box index at which the inline
	// content ran out of fragmentainer space (0: none yet). That is where
	// the break goes, unless widows require an earlier one. Mirrors Blink's
	// BlockLayoutAlgorithm::first_overflowing_line_ (block_layout_algorithm.h:
	// 550-553 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	firstOverflowingLine int

	// fitAllLines is set when as many lines as fit are to stay in this
	// fragment (no earlier break for widows): there are enough widows for
	// the next fragment, or the orphans / widows request cannot be met.
	// Mirrors Blink's BlockLayoutAlgorithm::fit_all_lines_
	// (block_layout_algorithm.h:555-559).
	fitAllLines bool

	// inlineItemsCache holds this block's prepared inline items once
	// prepareInlineItems has run.
	inlineItemsCache *inlineItems
}

// NewBlockLayoutAlgorithm creates a block layout algorithm for the given
// layout node with the given constraint space.
func NewBlockLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *BlockLayoutAlgorithm {
	return &BlockLayoutAlgorithm{
		ctx:        ctx,
		node:       node,
		style:      node.Style(),
		space:      space,
		earlyBreak: space.EarlyBreak,
	}
}

// SetBoxType sets the type of fragment this algorithm produces. Mirrors
// Blink's LayoutAlgorithm::SetBoxType.
func (bla *BlockLayoutAlgorithm) SetBoxType(t BoxType) {
	bla.boxType = t
}

// childPercResolutionBlockSize returns the percentage-resolution block size
// to pass to children. When this block is an anonymous wrap (e.g. block-in-
// inline split) with auto block size, the parent's resolution base passes
// through so percent CB-block resolution (CSS 2.1 §9.4.3 relative-offset,
// height:% etc.) reaches the real containing block. Mirrors Blink's
// PercentageResolutionBlockSize propagation for anonymous auto-height blocks.
// An anonymous space (a column box, whose own block-size is the
// fragmentainer's, not the containing block's CSS height) passes its own
// percentage-resolution block-size to its children: the dedicated one the
// multicol passes in (CalculateChildPercentageSize, length_utils.cc:
// 1810-1818; CreateConstraintSpaceForFragmentainer, fragmentation_utils.cc:
// 1388, :1393 @ a9f50e522efa).
func childPercResolutionBlockSize(bla *BlockLayoutAlgorithm, hasExplicitBlock bool, explicitBlockSize float64) float64 {
	if bla.space.IsAnonymous {
		return bla.space.PercentageResolutionSize.BlockSize.Float64()
	}
	if hasExplicitBlock {
		return explicitBlockSize
	}
	if bla.space.IsBlockSizeIndefinite() {
		return Indefinite
	}
	if bla.node != nil && bla.node.isAnonymous {
		if bla.space.PercentageResolutionSize.BlockSize.Float64() > 0 {
			return bla.space.PercentageResolutionSize.BlockSize.Float64()
		}
	}
	// An auto block-size gives children an indefinite percentage block-size
	// (CalculateChildPercentageSize returns the child available size,
	// length_utils.cc:1810-1828 @ a9f50e522efa), never a definite 0.
	return Indefinite
}

// previouslyConsumedBlockSize is the border-box block-size earlier fragments
// of a box consumed: Blink's previously_consumed_block_size, read from a
// break-inside token only (fragmentation_utils.cc:553-555 @ a9f50e522efa).
func previouslyConsumedBlockSize(incomingBreakToken *BlockBreakToken) float64 {
	if !incomingBreakToken.IsBreakInside() {
		return 0
	}
	return incomingBreakToken.ConsumedBlockSize.Float64()
}

// previousConsumedContentBlockSize is the content block-size the earlier
// fragments of a box laid out in space consumed: the consumed block-size
// less the declared block-start border, scrollbar and padding
// (declaredBlockStart, taken before a continuation clears its block-start
// edge from the fragment geometry).
func previousConsumedContentBlockSize(space ConstraintSpace, declaredBlockStart float64) float64 {
	return max(0, previouslyConsumedBlockSize(space.BreakToken)-declaredBlockStart)
}

// Layout performs block layout and returns the result.
func (bla *BlockLayoutAlgorithm) Layout() *LayoutResult {
	wdm := bla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(bla.ctx, bla.node, bla.style, wdm, bla.space)
	// The declared content block-size is the border box minus the full
	// edges, whatever this fragment carries.
	declaredBlockBorderPadding := geom.BlockBorderPadding()
	declaredBlockStart := geom.BorderBoxPadding().BlockStart
	bla.containerBuilder = NewBoxFragmentBuilder(wdm)
	bla.containerBuilder.SetLayoutNode(bla.node)
	bla.containerBuilder.SetConstraintSpace(&bla.space)
	bla.containerBuilder.SetInitialFragmentGeometry(&geom)
	bla.containerBuilder.SetBoxType(bla.boxType)
	// Fragmentation setup: monolithic / first-for-node, cloned decorations,
	// known to fit (then a first fragment requires content before
	// breaking), the slice continuation's cleared block-start edge, and the
	// initial balancing pass's edge floors (LayoutAlgorithm constructor →
	// SetupFragmentBuilderForFragmentation, layout_algorithm.h:91-95 @
	// a9f50e522efa). Blink runs it only with block fragmentation or a break
	// inside; without them it changes nothing here but the monolithic bit,
	// which a Blink builder has by default.
	shouldCloneBlockStart := SetupFragmentBuilderForFragmentation(bla.space, bla.node, bla.space.BreakToken, bla.containerBuilder)
	bla.fragmentainerOffsetForChildren = FragmentainerOffset(bla.containerBuilder, true) + geom.BorderBoxPadding().BlockStart
	bla.containerBuilder.PropagateSpaceShortage(bla.relayoutSpaceShortage)
	// A node on the spanner path keeps its breaks in its parent's flow, so
	// content after the spanner is not laid out before it
	// (block_layout_algorithm.cc:820-827 @ a9f50e522efa).
	if bla.space.ColumnSpannerPath != nil {
		bla.containerBuilder.SetShouldForceSameFragmentationFlow()
	}

	// CSS Lists §3 / Blink BlockLayoutAlgorithm constructor (bla.cc:319-327):
	// if this is a list item with an OUTSIDE marker, seed the
	// UnpositionedListMarker on the builder. The marker is laid out and placed
	// by the carry/claim protocol — claimed against the content's first
	// baseline once the first in-flow child with a baseline is known
	// (positionOrPropagateListMarker), or top-aligned when the list item
	// produced no line boxes (positionListMarkerWithoutLineBoxes). Skipped for
	// inside markers (ListMarkerOccupiesWholeLine) — those flow through the
	// inline path as ordinary inline-level boxes.
	if markerNode := bla.node.ListMarkerBlockNodeIfListItem(); markerNode != nil &&
		!markerNode.ListMarkerOccupiesWholeLine() {
		incoming := bla.space.BreakToken
		if incoming == nil || incoming.HasUnpositionedListMarker {
			bla.containerBuilder.SetUnpositionedListMarker(&UnpositionedListMarker{
				MarkerNode: markerNode,
				Item:       bla.node,
			})
		}
	}
	// LOU-365: tag monolithic boxes (scroll containers, size containment,
	// orthogonal writing-mode roots) so the parent-side break machinery
	// (ShouldAvoidBreakInside, CalculateUnbreakableBlockSize floor
	// propagation during column balancing) treats the box as a unit. Blink
	// reaches the same state from the opposite default: its fragment
	// builder starts monolithic (box_fragment_builder.h:789) and
	// SetupFragmentBuilderForFragmentation clears the bit only for nodes
	// that participate in fragmentation (fragmentation_utils.cc:399-425);
	// louis14's builder defaults to false, so the node predicate tags
	// explicitly — equivalent, because the space gate in the child walk
	// (see "Propagate fragmentation context" below) guarantees a monolithic
	// node never participates in fragmentation. All Blink cites @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
	//
	// This restores the pre-Cmt-5a unconditional contain:size clause: at
	// the pinned SHA, ShouldApplySizeContainment() IS in
	// LayoutBox::IsMonolithic (layout_box.cc:3661). The break-token
	// chaining bug the Cmt-5a narrowing worked around cannot recur — the
	// space gate means a monolithic box is laid out without fragmentation
	// and produces no break token to chain.
	//
	// The writing-mode-root clause is approximated with the space's
	// orthogonal flag: BLA cannot see its parent's writing mode, only
	// whether the parent flagged this space as an orthogonal root. A
	// parallel flip (e.g. vertical-rl child of vertical-lr parent) is
	// still laid out unfragmented via the parent's gate (which compares
	// full writing modes) but its fragment goes untagged, so it
	// contributes no balancing floor.
	if bla.node.IsMonolithic(bla.space.IsOrthogonalWritingModeRoot) {
		bla.containerBuilder.SetIsMonolithic(true)
	}
	contentInlineSize := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if contentInlineSize < 0 {
		contentInlineSize = 0
	}

	// Block-size: use geom if definite, else auto.
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - declaredBlockBorderPadding
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
	}

	// Replaced elements (img, etc.) with auto inline/block size: derive from
	// intrinsic dimensions and aspect ratio.
	// CSS 2.1 §10.3.2 (replaced inline): inline-size = intrinsic width.
	// CSS 2.1 §10.6.2: if height is auto and there is an intrinsic ratio, use it.
	// CSS Containment: size containment overrides intrinsic sizing — treat as 0.
	hasSizeContain := bla.style != nil && bla.style.ShouldApplySizeContainment()
	isReplaced := bla.node.DOMNode != nil && IsReplacedElement(bla.node.DOMNode)
	if isReplaced {
		if hasSizeContain {
			// CSS Contain 1 §4.2: under size containment, a replaced element's
			// intrinsic dimensions are treated as 0. ComputeFragmentGeometry
			// already collapses intrinsic-keyword widths (`min-content` /
			// `max-content` / `fit-content`) to 0 via the ComputeMinMaxSizes
			// short-circuit at min_max_sizing.go:99 (and applies `min-width`
			// afterward). The only case still landing on parent-fill here is
			// `width: auto`, which the geometry pass at fragment_geometry.go:666
			// fills with the available size. Override that branch to 0; leave
			// the geometry-resolved value alone for explicit / intrinsic-keyword
			// widths so `min-width` / `max-width` clamps survive.
			//
			// Mirrors Blink's `LayoutReplaced::ComputeNaturalSizingInfo` contract
			// (layout_replaced.cc, `DCHECK(!ShouldApplySizeContainment())` at
			// SHA 4883d11fef): callers must short-circuit before computing
			// natural dimensions when size containment applies.
			widthIsAuto := isAutoOrUnsetInline(bla.style, wdm)
			if widthIsAuto && !bla.space.IsFixedInlineSize {
				contentInlineSize = 0
				// Re-apply min-width if set, since the auto branch in
				// ComputeFragmentGeometry chose available-size before any
				// min-inline clamp.
				minInline := ResolveMinInlineSize(bla.style, wdm, bla.space, geom).Float64()
				if contentInlineSize < minInline {
					contentInlineSize = minInline
				}
			}
			if !hasExplicitBlock {
				explicitBlockSize = 0
				hasExplicitBlock = true
			}
		} else {
			// Check if inline-size is explicitly set. ResolveInlineSize returns false
			// for auto/unset, which is when we should use the intrinsic inline-size.
			_, explicitInlineOK := ResolveInlineSize(bla.style, wdm, bla.space, geom)
			if !explicitInlineOK && !bla.space.IsFixedInlineSize {
				// CSS 2.1 §10.3.2 / CSS Sizing 4 §6.3: a replaced element with auto
				// inline-size uses its intrinsic width as the USED width (computed by
				// ComputeReplacedSize, which already applies §10.4 min/max and any
				// aspect-ratio block->inline transfer). It OVERFLOWS its container rather
				// than clamping to fit, so the geometry pass's available-fill value must
				// be overridden whenever there is a real (positive) fill: a positive-but-
				// too-small fill must not win, or an over-wide replaced element in a narrow
				// (e.g. width:0) inline-block shrinks to a sliver instead of overflowing
				// (LOU-337 / css-pseudo marker-content-012's `::before{display:inline-block;
				// width:0;content:url(32px)}` reference).
				//
				// The fill is left untouched ONLY when the available inline-size collapsed
				// to 0 because the containing block's inline size is indefinite (a
				// max-content/min-content measurement pass) and no block constraint
				// transfers. The test reads the space's available size, not the
				// geometry's: a fit-content space shrinks an element with no intrinsic
				// size (an <svg> with only attribute sizes) to 0 even when space is
				// available (LOU-504, css-transforms skew-test1). Forcing the intrinsic
				// there would make an auto-sized replaced element wrongly contribute its
				// natural width to its container's intrinsic size (css-sizing
				// svg-intrinsic-size-005: `<div width:max-content><svg width:100%>` stays 0,
				// not 300). Mirrors Blink LayoutReplaced::ComputeReplacedLogicalWidth (SHA
				// 4883d11fef).
				inlineSize, _ := ComputeReplacedSize(bla.ctx, bla.node, bla.style, bla.space)
				if inlineSize > 0 {
					blockFixed := bla.space.IsFixedBlockSize && !bla.space.IsFixedBlockSizeIndefinite
					blockConstraintTransfers := replacedBlockConstraintTransfersToInline(
						bla.ctx, bla.node, bla.style, wdm, bla.space, geom)
					hasAvailableInline := bla.space.AvailableSize.InlineSize.Float64()-geom.InlineBorderPadding() > 0
					if hasAvailableInline || blockFixed || blockConstraintTransfers {
						contentInlineSize = inlineSize
					}
				}
			}
			if !hasExplicitBlock {
				_, blockSize := ComputeReplacedSize(bla.ctx, bla.node, bla.style, bla.space)
				if blockSize > 0 {
					explicitBlockSize = blockSize
					hasExplicitBlock = true
				}
			}
		}
	}

	// fragmentsBorderPadding is the block border/padding of all fragments
	// up to this one: cloned decorations repeat in every fragment, so the
	// box's stitched block-size includes each copy. Mirrors Blink's
	// UpdateBorderPaddingForClonedBoxDecorations (box_fragment_builder.cc:
	// 88-99 @ a9f50e522efa).
	fragmentsBorderPadding := declaredBlockBorderPadding
	if shouldCloneBlockStart {
		fragmentsBorderPadding += clonedEdgesOfPreviousFragments(bla.space.BreakToken, declaredBlockBorderPadding)
	}
	// clearBlockEndIfExcluded clears an excluded block-end edge from geom,
	// so it neither sizes nor paints the fragment.
	clearBlockEndIfExcluded := func(shouldBreakInside, hasInflowChildBreakInside bool) {
		if !ShouldIncludeBlockEndBorderPadding(bla.space.BreakToken, shouldBreakInside,
			bla.containerBuilder.IsKnownToFitInFragmentainer(), bla.containerBuilder.ShouldCloneBoxEndDecorations(),
			bla.space.IsNewFormattingContext, hasInflowChildBreakInside) {
			bla.containerBuilder.ClearBorderScrollbarPaddingBlockEnd()
		}
	}
	// consumeRemainingFragmentainerSpace stretches a breaking fragment's
	// content to the fragmentainer end (less cloned end decorations).
	// Mirrors Blink's ConsumeRemainingFragmentainerSpace
	// (block_layout_algorithm.cc:3084-3095) over
	// FragmentainerSpaceLeftForChildren (layout_algorithm.h:168-170) @
	// a9f50e522efa; the content-relative cursor is measured from
	// fragmentainerOffsetForChildren.
	consumeRemainingFragmentainerSpace := func(intrinsic float64) float64 {
		capacity := bla.fragmentainerCapacityForChildren()
		if capacity == Indefinite {
			return intrinsic
		}
		return max(intrinsic, capacity-bla.fragmentainerOffsetForChildren)
	}
	previouslyConsumed := previouslyConsumedBlockSize(bla.space.BreakToken)
	// finishFragmentation sizes this fragment from its content block-size,
	// clearing an excluded block-end edge from geom first: FinishLayout's
	// intrinsic_block_size_ (the content plus the edges this fragment carries,
	// block_layout_algorithm.cc:1163-1165, :1352-1357) and
	// FragmentsTotalBlockSize (ComputeBlockSizeForFragment over the stitched
	// intrinsic size, :1401-1407), then FinishFragmentation, or
	// FinishFragmentationForFragmentainer for a column box (:3137-3138).
	// Every size is border-box, and so is the consumed block-size it
	// returns @ a9f50e522efa. Every box in a fragmentation context, a column
	// box included, goes through it (FinishLayout → FinalizeForFragmentation,
	// :1479-1480).
	finishFragmentation := func(contentBlockSize float64, shouldBreakInside, hasInflowChildBreakInside bool) FinishFragmentationResult {
		clearBlockEndIfExcluded(shouldBreakInside, hasInflowChildBreakInside)
		edges := geom.BorderBoxPadding()
		intrinsic := edges.BlockStart + contentBlockSize + edges.BlockEnd
		if bla.boxType == BoxTypeColumn {
			// A fragmentainer stitches in no consumed block-size (:1383-1399)
			// and, its space being anonymous, ComputeBlockSizeForFragment
			// returns the intrinsic size (length_utils.cc:921-922 @
			// a9f50e522efa).
			return FinishFragmentationForFragmentainer(bla.containerBuilder, intrinsic)
		}
		var total float64
		switch {
		case hasExplicitBlock && bla.space.IsBlockSizeOverride:
			// A size imposed by the parent is this fragment's own.
			total = previouslyConsumed + geom.BorderBoxSize.BlockSize
		case hasExplicitBlock:
			total = explicitBlockSize + fragmentsBorderPadding
		default:
			total = previouslyConsumed + intrinsic
			if maxBlock, ok := ResolveMaxBlockSizeWithIntrinsic(bla.style, wdm, bla.space, geom, contentBlockSize); ok {
				total = min(total, maxBlock+fragmentsBorderPadding)
			}
			total = max(total, ResolveMinBlockSizeWithIntrinsic(bla.style, wdm, bla.space, geom, contentBlockSize)+fragmentsBorderPadding)
		}
		// FragmentainerSpaceLeft(builder, is_for_children=false)
		// (fragmentation_utils.cc:550-551): a box cloning its block-end
		// decorations always has room for them plus 1px of content.
		spaceLeft := Indefinite
		if !bla.space.IsInitialColumnBalancingPass {
			spaceLeft = FragmentainerSpaceLeft(bla.containerBuilder, false)
		}
		res := FinishFragmentation(FinishFragmentationInput{
			SpaceLeft:                             spaceLeft,
			FragmentainerCapacity:                 FragmentainerCapacity(bla.containerBuilder, false),
			HasBlockFragmentation:                 bla.space.HasBlockFragmentation,
			IsColumnFragmentation:                 bla.space.BlockFragmentationType == FragmentColumn,
			IsInitialColumnBalancingPass:          bla.space.IsInitialColumnBalancingPass,
			PreviousBreakToken:                    bla.space.BreakToken,
			FragmentsTotalBlockSize:               total,
			IntrinsicBlockSize:                    intrinsic,
			TrailingBorderPadding:                 edges.BlockEnd,
			BlockStartBorderPadding:               declaredBlockStart,
			PreventBreakBeforeBlockEndDecorations: bla.containerBuilder.ShouldCloneBoxEndDecorations(),
			FoundColumnSpanner:                    bla.containerBuilder.FoundColumnSpanner(),
			ShouldBreakInside:                     shouldBreakInside,
			HasInflowChildBreakInside:             hasInflowChildBreakInside,
			IsKnownToFitInFragmentainer:           bla.containerBuilder.IsKnownToFitInFragmentainer(),
			ShouldCloneBoxEndDecorations:          bla.containerBuilder.ShouldCloneBoxEndDecorations(),
			HasEarlyBreak:                         bla.containerBuilder.HasEarlyBreak(),
			HasNonVisibleBlockOverflow:            clipsBlockAxisOverflow(bla.style, bla.node.DOMNode, wdm),
		})
		if !res.IncludeBlockEnd {
			bla.containerBuilder.ClearBorderScrollbarPaddingBlockEnd()
		}
		if res.ClampAppealToLastResort {
			bla.containerBuilder.ClampBreakAppeal(BreakAppealLastResort)
		}
		return res
	}
	// setBreakingSize sizes a fragment that breaks inside by
	// finishFragmentation.
	setBreakingSize := func(contentBlockSize float64) FinishFragmentationResult {
		res := finishFragmentation(contentBlockSize, true, true)
		bla.containerBuilder.SetSize(LogicalSize{InlineSize: geom.BorderBoxSize.InlineSize, BlockSize: res.FragmentBlockSize})
		return res
	}

	// Build child constraint space.
	childAvailableInline := contentInlineSize
	childAvailableBlock := Indefinite
	if hasExplicitBlock {
		childAvailableBlock = explicitBlockSize
	}
	// An anonymous space (a fragmentainer's) gives its children its
	// available block-size (CalculateChildAvailableSize, length_utils.cc:
	// 1781-1785 @ a9f50e522efa; CreateConstraintSpaceForFragmentainer sets
	// IsAnonymous, fragmentation_utils.cc:1393).
	if bla.space.IsAnonymous {
		childAvailableBlock = bla.space.AvailableSize.BlockSize.Float64()
	}
	// The builder's child available size, against which AddChild resolves
	// the children's relative offsets. BLA's own content sizes (replaced
	// and fragmentainer cases above) stand in for CalculateChildAvailableSize's
	// border-box shrink; the anonymous-block step is shared.
	bla.containerBuilder.SetChildAvailableSize(LogicalSize{
		InlineSize: contentInlineSize,
		BlockSize:  anonymousChildAvailableBlockSize(bla.space, bla.node, childAvailableBlock),
	})

	// §10.3.2: Compute the available block-size for orthogonal children.
	// This accounts for the parent's height/min-height/max-height constraints
	// and walks up to the nearest ancestor scroller or ICB when needed.
	orthogonalAvailableBlock := computeOrthogonalAvailableBlock(
		bla.style, wdm, bla.space, geom, bla.ctx,
		childAvailableBlock, hasExplicitBlock, explicitBlockSize)

	// Float exclusion tracking.
	// Inherit exclusion space from parent, or start fresh for new BFCs.
	// The builder owns the exclusion space while the children are laid out
	// (Blink's container_builder_ exclusion space, fragment_builder.h:
	// 148-151 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if es := bla.space.ExclusionSpace; es != nil && !bla.space.IsNewFormattingContext {
		bla.containerBuilder.SetExclusionSpace(es)
	} else {
		bla.containerBuilder.SetExclusionSpace(&ExclusionSpace{})
	}
	// Track whether this element owns any floats (added during its layout).
	// Used by auto-height-clear: only clear our own floats, not inherited ones.
	hasOwnFloats := bla.space.IsNewFormattingContext

	// BFC block origin: the block offset of this element's content box
	// relative to the BFC origin. For new BFC roots this is 0; for
	// non-BFC children it is the parent's BFC offset plus our position.
	// Used to translate local block positions to BFC-relative coordinates
	// when passing the exclusion space to children.
	bfcBlockOrigin := 0.0
	if !bla.space.IsNewFormattingContext {
		bfcBlockOrigin = bla.space.BfcBlockOffset.Float64() + geom.Border.BlockStart + geom.Padding.BlockStart
	}

	// BFC inline origin: the inline offset of this element's content box
	// relative to the BFC origin. Used to translate float exclusion inline
	// offsets to local coordinates for line box computation.
	bfcInlineOrigin := 0.0
	bfcContainerInlineSize := contentInlineSize
	if !bla.space.IsNewFormattingContext {
		bfcInlineOrigin = bla.space.BfcInlineOffset.Float64() + geom.Border.InlineStart + geom.Padding.InlineStart
		if !bla.space.BfcContainerInlineSize.IsZero() {
			bfcContainerInlineSize = bla.space.BfcContainerInlineSize.Float64()
		}
	}

	// Lay out children in the block direction.
	// CSS 2.1 §9.2.1.1: a block container has either all block-level or
	// all inline-level children.
	blockCursor := 0.0 // current block position within content box
	var prevMarginStrut MarginStrut

	// Fragmentation state: the incoming break token to resume. Which
	// children resume, and with which child break token, is the
	// BlockChildIterator's call.
	incomingBreakToken := bla.space.BreakToken
	if incomingBreakToken != nil {
		blockCursor = 0 // We start at 0 in the new fragmentainer; consumed is tracked by the token.
		// Resumed after an unforced break that no column spanner caused:
		// margins adjoining the fragmentainer start are discarded. Mirrors
		// Blink BlockLayoutAlgorithm::Layout (block_layout_algorithm.cc:
		// 859-866 @ a9f50e522efa).
		if incomingBreakToken.IsBreakInside() && !incomingBreakToken.IsForcedBreak &&
			!incomingBreakToken.IsCausedByColumnSpanner {
			prevMarginStrut.DiscardMargins = true
		}
	}

	// The outgoing break state — child break tokens, whether a child broke
	// in this box's own flow, a forced break, space shortage, appeal — lives
	// on the builder (AddBreakBeforeChild, AddBreakToken, AddResult →
	// PropagateBreakInfo), as on Blink's container_builder_. The content
	// extent where an in-flow child break stopped the walk is the
	// consumed block-size of the outgoing token.
	var (
		pendingIntrinsicAtBreak     float64
		pendingHaveIntrinsicAtBreak bool
	)

	// CSS 2.1 §8.3.1: Parent-child top margin collapsing.
	// When a block has no block-start border/padding and isn't a new BFC,
	// the first child's margin propagates upward. A resumed fragment never
	// collapses through its block-start: margins generated inside it are
	// dealt with in this fragment (Blink rule C, block_layout_algorithm.cc:
	// 879-891 @ a9f50e522efa).
	canPropagateTop := !bla.space.IsNewFormattingContext &&
		geom.Border.BlockStart == 0 && geom.Padding.BlockStart == 0 &&
		!incomingBreakToken.IsBreakInside()
	firstNonEmptyChild := true
	// Blink PreviousInflowPosition::self_collapsing_child_had_clearance: the
	// last in-flow children are self-collapsing and one of them had clearance.
	selfCollapsingChildHadClearance := false
	// Whether a child placed so far left a breakpoint before the next one,
	// giving it container separation (Blink BlockLayoutAlgorithm::
	// has_break_opportunity_before_next_child_, block_layout_algorithm.cc:
	// 311, :2767-2772 @ a9f50e522efa). Unlike firstNonEmptyChild, a child
	// resumed in a parallel flow does not set it. A container pushed by
	// monolithic overflow from an earlier fragmentainer behaves as if there
	// were a perfect breakpoint before its first child (:868-876).
	hasBreakOpportunityBeforeNextChild := incomingBreakToken.HasMonolithicOverflow()
	var propagatedTopMargin MarginStrut

	var firstLineAscent float64
	var firstChildBaseline float64    // Baseline of the first in-flow block child (for propagation).
	var firstChildBlockOffset float64 // Block offset of the first in-flow block child.
	hasFirstChildBaseline := false
	var lastChildBaseline float64    // Baseline of the last in-flow block child.
	var lastChildBlockOffset float64 // Block offset of the last in-flow block child.
	hasLastChildBaseline := false

	// An outermost inline <svg>'s DOM children (<rect>, <g>, <text>, ...)
	// are NOT ordinary CSS content — they belong exclusively to the SVG
	// layout tree SVGRootAlgorithm builds via svg.BuildSVGRoot (see
	// svg_root_algorithm.go), painted by the separate SVG paint walk
	// (pkg/render/svg_root_painter.go). SVGRootAlgorithm.Layout() calls
	// this BlockLayoutAlgorithm ONLY for the <svg> element's own OUTER
	// replaced-box sizing (byte-identical to pre-SVG-support behavior);
	// it must not also walk <svg>'s DOM children as HTML block/inline
	// content, or an element with visible content (e.g. <text>) gets a
	// second, unwanted "shadow" CSS box painted via the ordinary
	// drawText path — invisible for shapes like <rect> (no text content)
	// but very visible once <text> support landed (LOU-345): the text
	// rendered twice, once correctly via svgTextPainter (fill=lightgreen)
	// and once as plain black CSS text via this exact fallthrough.
	// Mirrors how tryLayoutNestedDocument below intercepts iframe/object
	// before the generic children-layout branches — <svg> needs the same
	// kind of interception since it establishes its own foreign content
	// model, not an HTML one. Blink's LayoutSVGRoot never generates
	// LayoutObjects for descendants via the HTML block/inline layout
	// path at all (core/layout/svg/layout_svg_root.h @ blob
	// f53f8184c42dcf29c57c84f832fd84773d38b9cf) — this mirrors that by
	// simply adding no children to the fragment, leaving the outer
	// sizing computed above untouched.
	// An inline formatting context lays out through the inline path,
	// unless it holds block-in-inline items: then its runs of lines and
	// block-in-inline wrappers are in-flow children of the block walk.
	isInlineFormattingContext := hasOnlyInlineChildren(bla.node)
	hasBlockInInline := isInlineFormattingContext && containsBlockInInline(bla.node)
	isInlineSVGRoot := bla.node.DOMNode != nil && bla.node.DOMNode.TagName == "svg" && IsInlineSVG(bla.node)

	// Iframe/object with a document source: lay out the nested document
	// instead of this element's DOM children.
	if isInlineSVGRoot {
		// No children added — see doc comment above.
	} else if nested := bla.tryLayoutNestedDocument(contentInlineSize, wdm, geom); nested != nil {
		// For vertical-rl/sideways-rl nested roots, the root is anchored to the
		// right edge of the iframe viewport. Apply the X offset as an inline offset.
		offset := LogicalOffset{InlineOffset: nested.rootOffsetX}
		bla.containerBuilder.AddChild(nested.fragment, offset)
	} else if isInlineFormattingContext && !hasBlockInInline {
		// Inline formatting context: text nodes and inline-level children.
		// When resuming from a column break, start the line breaker at the
		// saved item index + text offset from the incoming break token.
		// Mirror Blink's `InlineBreakToken.start_` which carries both the
		// item-index AND the text-offset (cla.cc path, cf. findings §F4).
		// Resume when EITHER is non-zero — a single-text-item IFC (e.g.
		// `<div>xx xx<br/>xx xx<br/>xx xx</div>`) advances only the text
		// offset while the item index stays 0, so gating on
		// `InlineItemStartIndex > 0` alone would re-start at idx=0 and the
		// next column re-lays the same prefix.
		// The inline state on the break token is only meaningful when the
		// token refers to THIS block's own IFC (Node == bla.node); when a
		// mixed block+inline multicol forwards a break token whose inline
		// state belongs to a sibling block's IFC, this block must ignore
		// those fields to avoid resuming with the wrong cursor.
		inlineStartIdx := 0
		inlineStartTextOffset := 0
		// Honor the inline resume state on the incoming break token.
		// Mirrors Blink's `InlineBreakToken.start_` which carries both
		// item-index AND text-offset — single-text-item IFCs advance only
		// the text offset across columns, so gating on
		// `InlineItemStartIndex > 0` alone re-starts at idx=0 and loses
		// the resume point (findings §F4).
		if incomingBreakToken != nil &&
			(incomingBreakToken.InlineItemStartIndex > 0 ||
				incomingBreakToken.InlineTextOffset > 0) {
			inlineStartIdx = incomingBreakToken.InlineItemStartIndex
			inlineStartTextOffset = incomingBreakToken.InlineTextOffset
		}
		// HasSeenAllChildren on the incoming token: a previous fragment of
		// this block self-fragmented (didBreakSelf) AFTER the IFC fully placed
		// all inline content (no overflow → no inline break token). The
		// remaining fragments are pure box continuation — empty area below the
		// last line. Skip the IFC entirely so the text is not re-laid at the
		// top of every continuation column. Driver:
		// multicol-span-all-children-height-001 — block1 (CSS height:100px,
		// one line of "block1" text) self-fragments at the 50px column
		// boundary; col1 must show empty 50px box continuation, not the text
		// repeated. Mirrors Blink's `InlineBreakToken::is_finished_` on the
		// block's child path.
		prevES := bla.containerBuilder.GetExclusionSpace()
		// Floats that broke in the previous fragmentainer resume first, in
		// their parallel flows, even when all the lines were placed before.
		var inlineContext InlineChildLayoutContext
		bla.containerBuilder.SetExclusionSpace(bla.resumeInlineFloats(wdm, contentInlineSize, prevES,
			bfcBlockOrigin+blockCursor, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize, &inlineContext))
		var inlineAscent, lastBaselineOff float64
		var inlineBreakToken *BlockBreakToken
		if incomingBreakToken != nil && incomingBreakToken.HasSeenAllChildren {
			// Skip IFC; everything was placed in a prior fragment.
		} else {
			var es *ExclusionSpace
			blockCursor, es, inlineAscent, lastBaselineOff, inlineBreakToken = bla.layoutInlineChildren(wdm, contentInlineSize, bla.containerBuilder.GetExclusionSpace(), bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize, inlineStartIdx, inlineStartTextOffset, inlineSegment{}, &inlineContext)
			bla.containerBuilder.SetExclusionSpace(es)
			// Widows moved the break earlier than where the lines ran out
			// of space: lay out again and break there
			// (FinalizeForFragmentation → kNeedsEarlierBreak →
			// RelayoutAndBreakEarlier, block_layout_algorithm.cc:3106-3127,
			// :617-626 @ a9f50e522efa).
			if earlyBreak := bla.lineEarlyBreak(); earlyBreak != nil {
				return bla.relayoutAndBreakEarlier(earlyBreak)
			}
			// Everything fit, but depending on what comes after, the best
			// place to break may be between two of the lines
			// (block_layout_algorithm.cc:3128-3134 @ a9f50e522efa).
			if bla.earlyBreak == nil && bla.space.HasBlockFragmentation &&
				inlineBreakToken == nil && bla.firstOverflowingLine == 0 {
				bla.UpdateEarlyBreakBetweenLines()
			}
		}
		if bla.containerBuilder.GetExclusionSpace() != prevES && bla.space.IsNewFormattingContext {
			hasOwnFloats = true
		}
		// Floats that broke before or inside continue in parallel flows
		// (block_layout_algorithm.cc:2666-2672 @ a9f50e522efa): with the
		// lines when they break too, else as the only continuation.
		if inlineBreakToken == nil {
			inlineContext.propagateTo(bla.containerBuilder)
		}
		// The inline formatting context lays out all its content at once
		// (BlockChildIterator's inline mode, block_child_iterator.cc:47-70
		// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		bla.containerBuilder.SetHasSeenAllChildren()
		firstLineAscent = inlineAscent
		// Shared guard: capture first baseline in layout order (mirrors Blink).
		if !hasFirstChildBaseline && inlineAscent > 0 {
			firstChildBaseline = inlineAscent
			firstChildBlockOffset = 0
			hasFirstChildBaseline = true
		}
		// Track the last line's baseline offset for inline-block alignment.
		lastChildBaseline = lastBaselineOff
		lastChildBlockOffset = 0 // Already included in lastBaselineOff.
		hasLastChildBaseline = lastBaselineOff > 0
		firstNonEmptyChild = false // inline content is "content"
		// The inline content ends the margins adjoining this block's
		// start, so a discarding strut (a resumed block) does not leak out
		// as its end margin strut. Mirrors Blink ComputeInflowPosition
		// replacing the strut with the inline child's end margin strut
		// (block_layout_algorithm.cc:2983 @ a9f50e522efa).
		prevMarginStrut = MarginStrut{}

		// Inline fragmentation: if the inline layout stopped mid-content due to
		// column overflow, build a partial fragment and return early with the
		// inline break token. Mirrors the block-children fragmentation path.
		if inlineBreakToken != nil {
			// A line break breaks this block inside, as an in-flow child
			// break (Blink AddBreakBeforeChild on the inline node,
			// box_fragment_builder.cc:121-143 @ a9f50e522efa); the fragment
			// stretches to the fragmentainer end (block_layout_algorithm.cc:
			// 3281-3288).
			finish := setBreakingSize(consumeRemainingFragmentainerSpace(blockCursor))
			if finish.DisableFragmentation {
				// kDisableFragmentation (block_layout_algorithm.cc:
				// 1485-1486, :652-654 @ a9f50e522efa).
				return bla.relayoutWithoutFragmentation()
			}
			if bla.space.HasBlockFragmentation {
				inlineBreakToken.ConsumedBlockSize = layoutunit.FromFloat64Round(finish.ConsumedBlockSize)
				inlineBreakToken.IsAtBlockEnd = finish.IsAtBlockEnd
				inlineBreakToken.MonolithicOverflow = finish.MonolithicOverflow
			}
			bla.containerBuilder.SetIntrinsicBlockSize(blockCursor)
			bla.containerBuilder.SetNode(bla.node.DOMNode)
			bla.containerBuilder.SetStyle(bla.style)
			bla.containerBuilder.SetLayoutNode(bla.node)
			bla.containerBuilder.SetBoxData(&PhysicalBoxData{
				Border:    ToPhysicalEdges(geom.Border, wdm),
				Padding:   ToPhysicalEdges(geom.Padding, wdm),
				Scrollbar: ToPhysicalEdges(geom.Scrollbar, wdm),
			})
			inlineBreakToken.ChildBreakTokens = slices.Concat(inlineContext.ParallelFlowBreakTokens(), inlineBreakToken.ChildBreakTokens)
			result := bla.containerBuilder.Build()
			result.BreakToken = inlineBreakToken
			bla.containerBuilder.AdjustSpaceShortageForBreakAvoidRoot(bla.ctx, result)
			result.PropagatedOOFCandidates = bla.containerBuilder.outOfFlowCandidates
			return result
		}
	} else {
		// Block formatting context: block-level children. An inline
		// formatting context with block-in-inline items lays out its runs of
		// lines and its block-in-inline wrappers as in-flow children.
		children := bla.node.Children()
		if hasBlockInInline {
			children = bla.node.blockInInlineChildren()
		}

		// The children to lay out, resuming the incoming break token
		// (block_layout_algorithm.cc:982 @ a9f50e522efa).
		childIterator := NewBlockChildIterator(children, incomingBreakToken)

		// addBreakBeforeChild breaks before child without placing it (the
		// builder's AddBreakBeforeChild), records the content extent at the
		// break, and ConsumeRemainingFragmentainerSpace
		// (block_layout_algorithm.cc:3084-3095 @ a9f50e522efa): the breaking
		// fragment occupies the rest of the fragmentainer, on every
		// break-before (forced :3167, soft :3287, early :1088) — a no-op
		// while the column height is unknown. The caller then stops the
		// child loop.
		addBreakBeforeChild := func(child *LayoutInputNode, appeal *BreakAppeal, isForced bool) {
			bla.containerBuilder.AddBreakBeforeChild(child, appeal, isForced)
			// The content placed before the break.
			pendingIntrinsicAtBreak = blockCursor
			pendingHaveIntrinsicAtBreak = true
			blockCursor = consumeRemainingFragmentainerSpace(blockCursor)
		}

		// A float of the inline formatting context that broke in the
		// previous fragmentainer resumes first, in its parallel flow.
		if hasBlockInInline {
			var inlineContext InlineChildLayoutContext
			bla.containerBuilder.SetExclusionSpace(bla.resumeInlineFloats(wdm, contentInlineSize, bla.containerBuilder.GetExclusionSpace(),
				bfcBlockOrigin+blockCursor, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize, &inlineContext))
			inlineContext.propagateTo(bla.containerBuilder)
		}

		// Text children are not block children: the iterator skips them
		// (they are laid out by inline layout in anonymous blocks).
		for entry := childIterator.NextChild(); !entry.AtEnd(); entry = childIterator.NextChild() {
			child := entry.Node

			// A run of lines of the inline formatting context, up to the
			// next block-in-inline. The lines are in-flow content: they end
			// the margins pending before them, unless they adjoin this
			// block's block-start, where those margins stay with the block;
			// a run that places no line box leaves the margins collapsing
			// through. A line break inside the run breaks this block inside,
			// as an in-flow child break (Blink HandleInflow on the
			// InlineNode, block_layout_algorithm.cc:2597-2772 @
			// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
			if child.isInlineSegment {
				startIdx, startTextOffset := 0, 0
				if tok := entry.Token; tok != nil && !tok.IsBreakBefore {
					startIdx, startTextOffset = tok.InlineItemStartIndex, tok.InlineTextOffset
				}
				adjoining := firstNonEmptyChild && canPropagateTop
				lineTop := blockCursor
				if !adjoining {
					lineTop += prevMarginStrut.Resolve()
				}
				prevES := bla.containerBuilder.GetExclusionSpace()
				var inlineContext InlineChildLayoutContext
				end, es, ascent, lastBaselineOff, lineBreakToken := bla.layoutInlineChildren(wdm, contentInlineSize, prevES, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize, startIdx, startTextOffset, inlineSegment{
					blockStart:                lineTop,
					hasBreakOpportunityBefore: hasBreakOpportunityBeforeNextChild,
					node:                      child,
				}, &inlineContext)
				bla.containerBuilder.SetExclusionSpace(es)
				if es != prevES && bla.space.IsNewFormattingContext {
					hasOwnFloats = true
				}
				inlineContext.propagateTo(bla.containerBuilder)
				if earlyBreak := bla.lineEarlyBreak(); earlyBreak != nil {
					return bla.relayoutAndBreakEarlier(earlyBreak)
				}
				if end > lineTop || lineBreakToken != nil {
					if adjoining {
						propagatedTopMargin = prevMarginStrut
					}
					prevMarginStrut = MarginStrut{}
					firstNonEmptyChild = false
					blockCursor = end
					hasBreakOpportunityBeforeNextChild = true
					if !hasFirstChildBaseline && ascent > 0 {
						firstChildBaseline = ascent
						firstChildBlockOffset = lineTop
						hasFirstChildBaseline = true
					}
					if lastBaselineOff > 0 {
						lastChildBaseline = lastBaselineOff
						lastChildBlockOffset = 0
						hasLastChildBaseline = true
					}
				}
				if lineBreakToken != nil {
					lineBreakToken.Node = child
					bla.containerBuilder.AddBreakToken(lineBreakToken, false)
					if lineBreakToken.IsForcedBreak {
						bla.containerBuilder.SetHasForcedBreak()
					}
					pendingIntrinsicAtBreak = blockCursor
					pendingHaveIntrinsicAtBreak = true
					blockCursor = consumeRemainingFragmentainerSpace(blockCursor)
					break
				}
				continue
			}

			childStyle := child.Style()

			// Collect absolutely positioned elements for deferred layout.
			childPos := childStyle.GetPosition()
			if childPos == css.PositionAbsolute || childPos == css.PositionFixed {
				// Static block offset includes any pending collapsed margin from preceding siblings.
				// The abs-pos element's in-flow position would be after the resolved margin, just
				// like the next in-flow sibling. CSS §10.6.4: static position uses the hypothetical
				// in-flow position.
				//
				// The static position is fragment-local: Blink records
				// previous_inflow_position.logical_block_offset with no
				// consumed block-size (block_layout_algorithm.cc:1654-1656 @
				// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). It is made
				// stitched only for a fragmentainer descendant, at its
				// containing block (AdjustFragmentainerDescendant).
				staticBlockOffset := blockCursor + prevMarginStrut.Resolve()

				// Static inline offset: block-level abspos gets inline-start (0).
				// Inline-level abspos (display:inline/inline-*) in a block FC would
				// have established an anonymous inline line box — its hypothetical
				// inline cursor is where that line box would begin, which is past
				// any inline-start float exclusions active at this block position.
				// Mirrors Blink's InlineLayoutAlgorithm sub-pass which queries the
				// exclusion space for the line's inline-start when placing the
				// hypothetical inline-level OOF.
				staticInlineOffset := 0.0
				if exclusionSpace := bla.containerBuilder.GetExclusionSpace(); isInlineLevelDisplay(childStyle.GetDisplay()) && exclusionSpace != nil {
					// FindLocalFloatOffsets returns inline-start offset in the
					// same coordinate system the enclosing block uses when
					// placing in-flow inline content (mirrors inline_layout.go
					// line-start recomputation after placing floats).
					bfcBlock := bfcBlockOrigin + staticBlockOffset
					floatStartOff, _ := exclusionSpace.FindLocalFloatOffsets(bfcBlock, 0, bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize)
					if floatStartOff > 0 {
						staticInlineOffset = floatStartOff
					}
				}

				bla.containerBuilder.AddOutOfFlowCandidate(OutOfFlowCandidate{
					Node: child,
					StaticPosition: LogicalStaticPosition{
						Offset:     LogicalOffset{InlineOffset: staticInlineOffset, BlockOffset: staticBlockOffset},
						InlineEdge: StaticEdgeStart,
						BlockEdge:  StaticEdgeStart,
					},
					IsFixedPosition: childPos == css.PositionFixed,
				})
				continue
			}

			// Handle floats. A float that broke before or inside continues
			// in the next fragmentainer in a parallel flow: its token is
			// carried out, and the walk goes on (Blink HandleFloat,
			// block_layout_algorithm.cc:1792-1800; AddResult →
			// PropagateBreakInfo, fragment_builder.cc:455-464 @
			// a9f50e522efa). Only BFC roots extend auto block-size to clear
			// floats (CSS 2.1 §10.6.7); hasOwnFloats is already true for
			// BFC roots (initialized from IsNewFormattingContext).
			if childStyle.GetFloat() != css.FloatNone {
				bla.handleFloat(child, entry.Token, incomingBreakToken,
					bfcBlockOrigin+blockCursor+prevMarginStrut.Resolve(),
					contentInlineSize, childAvailableBlock, bfcBlockOrigin, bfcInlineOrigin,
					bfcContainerInlineSize)
				continue
			}

			// The child is a column spanner. Mirrors Blink's spanner branch
			// (block_layout_algorithm.cc:1013-1070 @ a9f50e522efa), detected
			// as IsColumnSpanAll → IsValidColumnSpannerInTree →
			// IsSelfValidColumnSpanner + DoesAncestryAllowColumnSpanner
			// (layout_box.cc:2956-3030).
			if bla.isColumnSpannerChild(child) {
				// The content extent at the break.
				pendingIntrinsicAtBreak = blockCursor
				pendingHaveIntrinsicAtBreak = true
				if bla.space.IsPastBreak || bla.containerBuilder.HasInsertedChildBreak() {
					// Something broke inside, in a parallel flow. Finish
					// what comes before the spanner first: break before it
					// (forced) and note the spanner without a path
					// (:1025-1040).
					bla.containerBuilder.AddBreakBeforeChild(child, nil, true)
					bla.containerBuilder.SetHasColumnSpanner(true)
					break
				}
				// Establish the spanner path, and note whether nothing in
				// this resumed fragment precedes the spanner (:1042-1055).
				bla.containerBuilder.SetColumnSpannerPath(&ColumnSpannerPath{Box: child})
				bla.containerBuilder.SetIsEmptySpannerParent(len(bla.containerBuilder.children) == 0 && incomingBreakToken.IsBreakInside())
				// Resume in front of the first following sibling that is not
				// a spanner; the break is forced, so the sibling keeps its
				// block-start margin. With no such sibling, resume after all
				// the children (:1059-1068, :1150-1161).
				for sibling := childIterator.NextChild(); !sibling.AtEnd(); sibling = childIterator.NextChild() {
					if bla.isColumnSpannerChild(sibling.Node) {
						continue
					}
					bla.containerBuilder.AddBreakBeforeChild(sibling.Node, nil, true)
					break
				}
				break
			}

			// The child this relayout determined to break before: break
			// now and finish the fragment, as a soft break with perfect
			// appeal. Mirrors Blink BlockLayoutAlgorithm::Layout
			// (block_layout_algorithm.cc:1074-1090 @ a9f50e522efa). Not
			// ported: the abort to resolve the BFC block-offset first
			// (:1078-1084); louis14 places the break at the
			// margin-collapsed cursor.
			if bla.earlyBreak != nil && IsEarlyBreakTarget(bla.earlyBreak, bla.containerBuilder, child) {
				perfect := BreakAppealPerfect
				addBreakBeforeChild(child, &perfect, false)
				break
			}

			// Determine child's writing direction.
			childWDM := NewWritingDirectionMode(childStyle)

			// Resolve child's margins in the PARENT's logical coordinates.
			// CSS 2.1 §8.3: percentage margins resolve against the containing
			// block's inline-size. The logical mapping uses the parent's WDM
			// because the parent's block layout positions children in its own
			// coordinate system. Mirrors Blink's ComputeMargins which uses
			// ConstraintSpace().GetWritingDirection() (the parent's).
			childMargins := ResolveMargins(childStyle, wdm, childAvailableInline)
			// A resumed child's margins are truncated at the break, except
			// after a forced break. Mirrors Blink ComputeChildData (block_layout_algorithm.cc:
			// 2836-2847 @ a9f50e522efa).
			if childToken := entry.Token; childToken != nil {
				AdjustMarginsForFragmentation(childToken, &childMargins)
				if childToken.IsForcedBreak {
					prevMarginStrut = MarginStrut{}
				}
				// Every container pushed past monolithic overflow records it
				// in its token; only the outermost one is offset here
				// (block_layout_algorithm.cc:2848-2855). louis14 has no
				// paginated root, so Blink's IsPaginatedRoot() term is false.
				if childToken.HasMonolithicOverflow() && !incomingBreakToken.HasMonolithicOverflow() {
					blockCursor += childToken.MonolithicOverflow
				}
			}

			// Handle clear property.
			// CSS 2.1 §8.3.1: The element is placed so its border edge is at or below
			// the bottom outer edge of all relevant floats. The clearance amount is
			// max(0, floatEnd - (blockCursor + collapsedMargin)). If the collapsed
			// margin already places the element past the float, clearance = 0, but
			// the clear property still inhibits parent-child margin collapsing.
			hasClearance := false
			// isPushedByFloats: clearance or a float drop actually moved the
			// child down — Blink's is_pushed_by_floats, set only when
			// clearance past floats applies (block_layout_algorithm.cc:
			// 2326-2337 @ a9f50e522efa). Unlike hasClearance it stays false
			// when `clear` inserts no gap.
			isPushedByFloats := false
			// The cursor and strut before clearance was applied, and the
			// content offset clearance moved the child to: a self-collapsing
			// child re-decides its clearance once its margin strut is known.
			var preClearanceCursor, clearanceOffset float64
			var preClearanceStrut MarginStrut
			clearType := childStyle.GetClear()
			if clearType != css.ClearNone && !bla.containerBuilder.GetExclusionSpace().IsEmpty() {
				bfcCursor := bfcBlockOrigin + blockCursor
				clearedBlockBfc := bla.containerBuilder.GetExclusionSpace().ClearanceOffset(clearType, layoutunit.FromFloat64Round(bfcCursor), wdm).Float64()
				if clearedBlockBfc > bfcCursor {
					// Convert BFC-relative cleared position to local coordinates.
					clearedBlock := clearedBlockBfc - bfcBlockOrigin
					// There are floats to be cleared. Compute where the element
					// would land naturally via its collapsed margin.
					tempStrut := prevMarginStrut
					tempStrut.Append(childMargins.BlockStart)
					tentativeBlockOff := blockCursor + tempStrut.Resolve()
					if clearedBlock > tentativeBlockOff {
						// Margin alone is insufficient; inject clearance gap.
						// Set blockCursor so that blockCursor + childMargins.BlockStart
						// = clearedBlock after the strut is reset and the child's
						// block-start margin is re-appended at step 1 below.
						preClearanceCursor, preClearanceStrut = blockCursor, prevMarginStrut
						clearanceOffset = clearedBlock
						blockCursor = clearedBlock - childMargins.BlockStart
						prevMarginStrut = MarginStrut{} // consumed by clearance
						isPushedByFloats = true
					}
					// Whether or not a gap was inserted, the clear property
					// inhibits parent-child margin collapsing (CSS 2.1 §8.3.1).
					hasClearance = true
				}
			}

			// Build constraint space for this child.
			// CSS Writing Modes §4.3: a block container with a different
			// writing-mode than its parent establishes a new BFC.
			isChildNewFC := createsFormattingContext(childStyle, child) ||
				wdm.WM != childWDM.WM

			// A child's border edge within this block's content box: a child
			// adjoining this block's block-start shares this block's border
			// edge, whose fragmentainer offset already resolves the margins
			// collapsing through it; any other child sits past the collapsed
			// strut. Every box's fragmentainer offset is its resolved border
			// edge, as in Blink (FragmentainerOffsetAtBfc + bfc_block_offset,
			// block_layout_algorithm.cc:3156-3158, :3619-3633 @ a9f50e522efa).
			var childBlockOffset float64
			isAdjoiningBlockStart := func() bool {
				return firstNonEmptyChild && canPropagateTop && !hasClearance && !isPushedByFloats
			}
			borderEdgeAt := func(collapsed MarginStrut) float64 {
				if isAdjoiningBlockStart() {
					return blockCursor
				}
				if isChildNewFC {
					// A new FC propagates no margins; its edge is
					// childBlockOffset, which float drops move.
					return childBlockOffset
				}
				return blockCursor + collapsed.Resolve()
			}
			// collapsedWithChild is the pending strut collapsed with the
			// child's block-start margin and the margins collapsing up through
			// its block-start (Steps 1-2 below).
			collapsedWithChild := func(childPropagated MarginStrut) MarginStrut {
				strut := prevMarginStrut
				strut.Append(childMargins.BlockStart)
				if !childPropagated.IsEmpty() {
					strut.AppendStrut(childPropagated)
				}
				return strut
			}
			// childBlockOffset is the child's border edge in this block's
			// content box past the pending strut collapsed with its own
			// margin: Blink's child_bfc_offset_estimate, which a drop below
			// floats moves to the start of the layout opportunity that fits
			// (child_bfc_offset; block_layout_algorithm.cc:1854-1861,
			// :2228-2229 @ a9f50e522efa). A drop never re-adds the margin.
			childBlockOffset = blockCursor + collapsedWithChild(MarginStrut{}).Resolve()
			// childBfcBlockOff is a non-FC child's BFC offset estimate,
			// before its own margin.
			childBfcBlockOff := blockCursor + prevMarginStrut.Resolve()

			// Compute available inline for this child, accounting for floats.
			// floatCheckBlock is BFC-relative so exclusion space queries are correct.
			isOrthogonal := wdm.IsOrthogonalTo(childWDM)
			floatCheckBlock := bfcBlockOrigin + blockCursor
			if isChildNewFC {
				floatCheckBlock = bfcBlockOrigin + childBlockOffset
			}
			floatStartOff, floatEndOff := bla.containerBuilder.GetExclusionSpace().FindLocalFloatOffsets(floatCheckBlock, 0, bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize)
			// CSS 2.1 §9.5: Non-BFC block boxes flow as if floats don't exist;
			// only their line boxes shorten around floats. So only BFC children
			// have their available inline-size reduced by float exclusions.
			childInlineForSpace := childAvailableInline - childMargins.InlineSum()
			if isChildNewFC {
				childInlineForSpace -= floatStartOff + floatEndOff
			}
			if childInlineForSpace < 0 {
				childInlineForSpace = 0
			}

			// CSS 2.1 §9.5: BFC float avoidance.
			//
			// The pre-layout inline-size check uses ResolveInlineSize in the
			// child's writing mode, which gives the wrong dimension for
			// orthogonal children (their inline axis is perpendicular to the
			// parent's float axis). Skip the pre-layout check for orthogonal
			// children; the post-layout check handles them correctly.
			if isChildNewFC && !isOrthogonal && (floatStartOff > 0 || floatEndOff > 0) {
				childGeomForBFC := ComputeFragmentGeometry(childStyle, childWDM)
				tmpSpace := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
					SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
					SetAvailableSize(LogicalSize{
						InlineSize: contentInlineSize,
						BlockSize:  Indefinite,
					}).
					SetPercentageResolutionSize(LogicalSize{
						InlineSize: contentInlineSize,
						BlockSize:  explicitBlockSize,
					}).
					SetPercentageResolutionInlineSize(contentInlineSize).
					Build()
				if resolvedInline, ok := ResolveInlineSize(childStyle, childWDM, tmpSpace, childGeomForBFC); ok {
					neededInline := resolvedInline.Float64() + childGeomForBFC.InlineBorderPadding() + childMargins.InlineSum()
					availBesideFloats := contentInlineSize - floatStartOff - floatEndOff
					if neededInline > availBesideFloats {
						// Doesn't fit — find the earliest block position
						// where the BFC fits alongside remaining floats.
						// Use FindFloatPositionLocal which iterates through
						// float boundaries (CSS 2.1 §9.5 Rule 5).
						newBlockBfc := bla.containerBuilder.GetExclusionSpace().FindFloatPositionLocal(
							css.FloatLeft, neededInline, 0,
							contentInlineSize, floatCheckBlock,
							bfcInlineOrigin, bfcContainerInlineSize)
						newBlock := newBlockBfc - bfcBlockOrigin
						if newBlock > childBlockOffset {
							childBlockOffset = newBlock
							isPushedByFloats = true
							floatStartOff, floatEndOff, childInlineForSpace = reQueryFloatsAfterDrop(
								bla.containerBuilder.GetExclusionSpace(), bfcBlockOrigin+childBlockOffset, 0,
								bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize, childMargins.InlineSum())
						}
					}
				}
			}
			blockForChild := childAvailableBlock
			if isOrthogonal {
				blockForChild = orthogonalAvailableBlock
			}
			// newChildSpaceBuilder is shared by the initial layout and the
			// re-layouts below floats and at the resolved border edge:
			// borderEdgeOff is the child's border edge within this block's
			// content box with every margin collapsing through its
			// block-start resolved (its fragmentainer offset), and
			// childInlineForSpace is read at call time (the drop re-queries
			// it). Blink lays out each
			// layout opportunity through the same
			// CreateConstraintSpaceForChild (LayoutNewFormattingContext,
			// block_layout_algorithm.cc:2158-2163 @ a9f50e522efa), so the
			// re-layout keeps the fragmentation context and the resume token.
			//
			// Fragmentation context is propagated to children — except
			// monolithic ones. A monolithic non-inline child's constraint
			// space gets no fragmentainer block-size/offset/type at all:
			// it is laid out unfragmented, and if too tall to fit it
			// either overflows the fragmentainer or is pushed whole to the
			// next one (BreakBeforeChildIfNeeded), never sliced. Mirrors
			// Blink's SetupSpaceBuilderForFragmentation early return for
			// `child.IsMonolithic() && !child.IsInline()`
			// (fragmentation_utils.cc:321-345 @ a9f50e522efa). Inline
			// children never reach this walk (they are grouped into the
			// inline path), so the !IsInline half of Blink's condition is
			// structural here. The child's full block-size still reaches
			// the column balancer: its fragment is tagged IsMonolithic
			// (Layout() entry above), so the parent-side floor propagation
			// in BreakBeforeChildIfNeeded fires.
			childIsMonolithic := child.IsMonolithic(wdm.WM != childWDM.WM)
			// An anonymous block, or a parallel child the block container
			// stretches, stretches an auto inline-size; any other child
			// (replaced, table, form control) is fit-content. Mirrors Blink's
			// CreateConstraintSpaceForChild (block_layout_algorithm.cc:
			// 3428-3449 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
			// justify-self (:3430-3443) is not ported — louis14 does not align
			// block children by justify-self.
			childInlineAutoBehavior := AutoSizeBehaviorFitContent
			if child.IsAnonymous() || (!isOrthogonal && ShouldBlockContainerChildStretchAutoInlineSize(child)) {
				childInlineAutoBehavior = AutoSizeBehaviorStretchImplicit
			}
			newChildSpaceBuilder := func(borderEdgeOff float64) *ConstraintSpaceBuilder {
				b := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
					SetOrthogonalFallbackInlineSize(
						orthogonalFallbackSize(childWDM, bla.ctx)).
					SetOrthogonalFallbackBlockSize(
						computeOrthogonalFallbackBlockForChildren(
							bla.style, wdm, bla.space, geom, bla.ctx,
							hasExplicitBlock, explicitBlockSize)).
					SetAvailableSize(LogicalSize{
						InlineSize: childInlineForSpace,
						BlockSize:  blockForChild,
					}).
					SetPercentageResolutionSize(LogicalSize{
						InlineSize: contentInlineSize,
						BlockSize:  childPercResolutionBlockSize(bla, hasExplicitBlock, explicitBlockSize),
					}).
					SetPercentageResolutionInlineSize(contentInlineSize).
					SetExclusionSpace(bla.containerBuilder.GetExclusionSpace())

				b.SetInlineAutoBehavior(childInlineAutoBehavior)

				// For non-BFC children, propagate the BFC block and inline
				// offsets so they can correctly query the exclusion space in
				// their own layout.
				if !isChildNewFC {
					// The child's tentative BFC offset = parent's BFC origin +
					// child's local position (blockCursor + pending margin).
					tentativeChildBfcOff := bfcBlockOrigin + childBfcBlockOff + childMargins.BlockStart
					b.SetBfcBlockOffset(layoutunit.FromFloat64Round(tentativeChildBfcOff))
					// The child's BFC inline offset = parent's BFC inline origin +
					// child's inline position. Per CSS 2.1 §9.5, non-BFC blocks
					// position as if floats don't exist, so use margin only.
					b.SetBfcInlineOffset(layoutunit.FromFloat64Round(bfcInlineOrigin + childMargins.InlineStart))
					b.SetBfcContainerInlineSize(layoutunit.FromFloat64Round(bfcContainerInlineSize))
				}

				// Pass child break token if resuming this specific child.
				// Blink passes it in LayoutAlgorithmParams whether or not the
				// space fragments, so a box relaid out with fragmentation
				// forced off still resumes its children (LayoutBlockChild,
				// block_layout_algorithm.cc:109-122 @ a9f50e522efa).
				if tok := entry.Token; tok != nil {
					b.SetBreakToken(tok)
				}
				// The early break inside the child, if the relayout breaks
				// there (LayoutBlockChild → EnterEarlyBreakInChild,
				// block_layout_algorithm.cc:109-122 @ a9f50e522efa).
				b.SetEarlyBreak(EnterEarlyBreakInChild(child, bla.earlyBreak))
				if !bla.space.HasBlockFragmentation || childIsMonolithic {
					return b
				}
				bla.setupSpaceBuilderForFragmentation(b, bla.fragmentainerOffsetForChildren+borderEdgeOff)

				// Propagate the "spanner descendants blocked" flag: set it when
				// any ancestor already blocks, or when the current block itself
				// would block — so descendants at any depth see the flag.
				// Mirrors Blink's ShouldPreventColumnSpannerDescendants walk in
				// DoesAncestryAllowColumnSpanner (layout_box.cc:2987-3001).
				if bla.space.ColumnSpannerDescendantsBlocked ||
					shouldPreventColumnSpannerDescendants(bla.node) {
					b.SetColumnSpannerDescendantsBlocked(true)
				}

				// A break already inserted in this fragmentainer (in a
				// parallel flow) holds back any spanner in the child too
				// (block_layout_algorithm.cc:3643-3650 @ a9f50e522efa).
				if bla.space.BlockFragmentationType == FragmentColumn &&
					(bla.space.IsPastBreak || bla.containerBuilder.HasInsertedChildBreak()) {
					b.SetIsPastBreak(true)
				}

				// Step the spanner path down to the child when the child is
				// the next node on it (FollowColumnSpannerPath,
				// fragmentation_utils.h:560-570 @ a9f50e522efa).
				if path := bla.space.ColumnSpannerPath; path != nil && path.Box == child {
					b.SetColumnSpannerPath(path.Child)
				}

				return b
			}

			borderEdgeOff := borderEdgeAt(collapsedWithChild(MarginStrut{}))
			childSpace := newChildSpaceBuilder(borderEdgeOff).Build()

			// Recursively lay out the child.
			childResult := layoutElement(bla.ctx, child, childSpace)

			// Margins collapsing up through the child's block-start move its
			// border edge, which is known only now: lay it out once more at
			// the resolved edge, so its breaks are decided where it is
			// placed. Mirrors Blink aborting a fragmented child whose BFC
			// block offset resolves differently from the estimate and
			// re-laying it out at ForcedBfcBlockOffset
			// (block_layout_algorithm.cc:844-849, :2539-2580 @ a9f50e522efa).
			if bla.space.HasBlockFragmentation && !childIsMonolithic {
				if resolved := borderEdgeAt(collapsedWithChild(childResult.PropagatedTopMargin)); resolved != borderEdgeOff {
					childSpace = newChildSpaceBuilder(resolved).Build()
					childResult = layoutElement(bla.ctx, child, childSpace)
				}
			}

			// LOU-365 (CodeRabbit 3514362226): supplement the child-side
			// builder tagging for writing-mode roots the child cannot see.
			// The space gate above compares full writing modes, but the
			// child-side tag only sees IsOrthogonalWritingModeRoot, so a
			// PARALLEL flip (e.g. vertical-rl parent, vertical-lr child)
			// came back untagged — no balancing floor, no
			// ShouldAvoidBreakInside. In Blink the fragment is monolithic
			// by construction (builder default true, cleared only when
			// fragmentation is set up — box_fragment_builder.h:789 @
			// a9f50e522efa); tagging the returned fragment from the parent,
			// which knows both writing modes, is the louis14 equivalent.
			// Idempotent for children the child-side tag already covered.
			if childIsMonolithic && childResult != nil && childResult.Fragment != nil {
				childResult.Fragment.IsMonolithic = true
			}

			// CSS 2.1 §9.5 Rule 5 / §10.3.3: A new BFC must not overlap
			// float margin boxes. If the child doesn't fit alongside
			// floats at the current block position, push it below them.
			// This check uses NewLogicalFragment(parentWDM, ...) which
			// correctly maps the child's dimensions to the parent's frame,
			// so it works for both same-mode and orthogonal children.
			//
			// The overlap query uses the BFC's full block extent (margin box)
			// so that floats further down the block axis are caught — a
			// zero-extent query at the BFC's top can miss floats whose block
			// range overlaps the BFC but starts below it. Mirrors Blink's
			// ExclusionSpace::FindLayoutOpportunity, which uses the full
			// availability_block_size (cf. exclusion_space.cc).
			if isChildNewFC {
				childLogicalTmp := NewLogicalFragment(wdm, childResult.Fragment)
				bfcBlockExtent := childLogicalTmp.BlockSize()
				origFloatStartOff, origFloatEndOff := bla.containerBuilder.GetExclusionSpace().FindLocalFloatOffsets(
					bfcBlockOrigin+childBlockOffset, bfcBlockExtent, bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize)
				if origFloatStartOff > 0 || origFloatEndOff > 0 {
					neededInline := childLogicalTmp.InlineSize() + childMargins.InlineSum()
					availableInline := contentInlineSize - origFloatStartOff - origFloatEndOff
					if neededInline > availableInline {
						// Child doesn't fit — find the earliest block position
						// where the BFC fits alongside remaining floats.
						newBlockBfc := bla.containerBuilder.GetExclusionSpace().FindFloatPositionLocal(
							css.FloatLeft, neededInline, bfcBlockExtent,
							contentInlineSize, bfcBlockOrigin+childBlockOffset,
							bfcInlineOrigin, bfcContainerInlineSize)
						newBlock := newBlockBfc - bfcBlockOrigin
						if newBlock > childBlockOffset {
							childBlockOffset = newBlock
							isPushedByFloats = true
							floatStartOff, floatEndOff, childInlineForSpace = reQueryFloatsAfterDrop(
								bla.containerBuilder.GetExclusionSpace(), bfcBlockOrigin+childBlockOffset, bfcBlockExtent,
								bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize, childMargins.InlineSum())
							// Re-layout at the opportunity with the new
							// available size.
							childSpace = newChildSpaceBuilder(childBlockOffset).Build()
							childResult = layoutElement(bla.ctx, child, childSpace)
						}
					}
				}
			}

			// A block-in-inline sits inside its inline boxes: it moves with
			// their relative offsets, paints in their opacity group, and its
			// out-of-flow descendants take an inline box that contains them
			// as their containing block.
			var blockInInlineRelativeOffset LogicalOffset
			if child.IsBlockInInline() {
				blockInInlineRelativeOffset = bla.adoptBlockInInlineResult(wdm, child, childResult)
			}

			// Step 1: Append child's block-start margin to the strut.
			// Step 2: Include any propagated top margin from the child
			// (recursive parent-child collapsing).
			prevMarginStrut = collapsedWithChild(childResult.PropagatedTopMargin)

			// Step 3: Check if margins collapse through this element.
			// CSS 2.1 §8.3.1: An element's margins collapse through it if
			// it has no height, no border, no padding, does not establish
			// a new block formatting context, does not contain a line box,
			// and all of its in-flow children's margins (if any) are collapsed.
			// We approximate this by checking that the element holds no
			// fragment other than self-collapsing ones. A self-collapsing
			// child is still placed, broken before and added like any
			// in-flow child; it only passes the margin strut through
			// (Blink FinishInflow / ComputeInflowPosition,
			// block_layout_algorithm.cc:2622-2653, :2732-2736, :2894-2898 @
			// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
			childLogical := NewLogicalFragment(wdm, childResult.Fragment)
			childBlockSize := childLogical.BlockSize()
			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			// A resumed child resolves its BFC block-offset at its start, so
			// it never self-collapses (Blink is_resuming_,
			// block_layout_algorithm.cc:888-898 @ a9f50e522efa): the 0px
			// fragment of a spanner parent after its spanner is kept.
			isSelfCollapsing := childBlockSize == 0 &&
				!entry.Token.IsBreakInside() &&
				childResult.BreakToken == nil &&
				childResult.BlockSizeForFragmentation == 0 &&
				childGeom.Border.BlockStart == 0 && childGeom.Border.BlockEnd == 0 &&
				childGeom.Padding.BlockStart == 0 && childGeom.Padding.BlockEnd == 0 &&
				!isChildNewFC &&
				hasOnlySelfCollapsingChildren(childResult.Fragment)
			if isSelfCollapsing {
				childResult.Fragment.IsSelfCollapsing = true
				// The margins collapsing through the child's content join
				// the strut now, before the child is positioned and broken
				// before: Blink passes the strut into the child and positions
				// it by the EndMarginStrut it reads back
				// (PositionSelfCollapsingChildWithParentBfc,
				// block_layout_algorithm.cc:3066-3081 @ a9f50e522efa).
				prevMarginStrut.AppendStrut(childResult.EndMarginStrut)
				if isPushedByFloats {
					// A self-collapsing child gets clearance only when its
					// hypothetical border edge, past every margin collapsed
					// up to it, is above the floats (FinishInflow,
					// block_layout_algorithm.cc:2440-2454). With clearance,
					// the margins before it separate from its own, and the
					// cursor is set so that its own strut lands it at the
					// clearance offset (ComputeInflowPosition, :2900-2950;
					// relayout with a strut of its block-start margin only,
					// :2496-2515 @ a9f50e522efa).
					unclearedStrut := preClearanceStrut
					unclearedStrut.Append(childMargins.BlockStart)
					unclearedStrut.AppendStrut(childResult.EndMarginStrut)
					if preClearanceCursor+unclearedStrut.Resolve() >= clearanceOffset {
						blockCursor, prevMarginStrut = preClearanceCursor, unclearedStrut
						isPushedByFloats = false
					} else {
						blockCursor = clearanceOffset - prevMarginStrut.Resolve()
					}
				}
			}

			// Step 4: Position child in the inline direction.
			// CSS 2.1 §9.5: Non-BFC block boxes flow as if floats don't exist.
			// Only BFC children (isChildNewFC) are offset past floats.
			// Non-BFC children are positioned at their margin; their line boxes
			// shorten around floats instead.
			// CSS 2.1 §10.3.3: If both margin-inline-start and margin-inline-end are auto,
			// and the element has a definite inline-size, center it.
			childInlineOffset := childMargins.InlineStart
			if isChildNewFC {
				// CSS 2.1 §9.5 / Blink LayoutNewFormattingContext: a new-FC box's
				// inline margins are measured from the content box and cannot pull
				// its border box into a line-left float's margin area. A negative
				// margin is clamped; floatStartOff is not simply added (LOU-327).
				childInlineOffset = newFCInlineOffsetBesideFloats(childMargins.InlineStart, floatStartOff)
			}

			// Whether to break before the child, asked for every in-flow
			// child after its layout (Blink HandleInflow /
			// HandleNewFormattingContext → BreakBeforeChildIfNeeded,
			// block_layout_algorithm.cc:2599-2660, :1993-2006 @
			// a9f50e522efa); a resumed child moves past.
			if bla.space.HasBlockFragmentation {

				// A child that clearance or a float drop moved down is
				// separated from the container even when it is the first
				// child (Blink: IsPushedByFloats() in has_container_separation,
				// block_layout_algorithm.cc:1994-1997 and :2622-2625 @
				// a9f50e522efa). A `clear` that inserted no gap does not
				// separate it.
				hasContainerSeparation := hasBreakOpportunityBeforeNextChild || isPushedByFloats
				// Steps 1-2 have collapsed the child's margins into the strut.
				fragOff := bla.fragmentainerOffsetForChildren + borderEdgeAt(prevMarginStrut)
				// The floats before the child decide whether its clearance
				// goes past the fragmentainer: the builder takes the child's
				// exclusion space only past this check.
				status, isForced := BreakBeforeChildIfNeeded(
					bla.space, child, childResult,
					fragOff, bla.fragmentainerCapacityForChildren(),
					hasContainerSeparation, bla.containerBuilder)

				if status == BreakStatusNeedsEarlierBreak {
					// A more appealing breakpoint was passed: report the
					// space shortage from where we would break here
					// (AttemptSoftBreak, fragmentation_utils.cc:1334-1340
					// @ a9f50e522efa), then lay out again and break there
					// (HandleInflow → kNeedsEarlierBreak → Abort →
					// HandleNonsuccessfulLayoutResult →
					// RelayoutAndBreakEarlier, block_layout_algorithm.cc:
					// 2004-2006, :1102-1105, :617-626).
					if bla.space.HasKnownFragmentainerBlockSize() {
						PropagateSpaceShortage(bla.space, childResult, fragOff, bla.fragmentainerCapacityForChildren(), bla.containerBuilder)
					}
					if bla.earlyBreak == nil {
						return bla.relayoutAndBreakEarlier(bla.containerBuilder.GetEarlyBreak())
					}
					// A relayout breaks at its target before running out of
					// space, so it never gets here (Blink DCHECKs
					// !early_break_, layout_algorithm.h:276); should it, break
					// before this child rather than relayout again.
					status = BreakStatusBrokeBefore
				}

				if status == BreakStatusBrokeBefore {
					// Break before this child without placing it, and finish
					// this fragment through the single post-loop exit, as
					// Blink's HandleInflow returns kSuccess on kBrokeBefore
					// (block_layout_algorithm.cc:2646-2659 @ a9f50e522efa) and
					// the child loop then stops (:1108-1115).
					// BreakBeforeChild → AddBreakBeforeChild: this block
					// breaks inside, so in slice mode it carries no block-end
					// edge, and FinishFragmentation decides its size, consumed
					// block-size and whether it is at its block end, the
					// pushed child then continuing in a parallel flow
					// (fragmentation_utils.cc:717-777).
					// A soft break before the child reports its space
					// shortage so the multicol balancing loop can stretch
					// (Blink BreakBeforeChild → PropagateSpaceShortage,
					// fragmentation_utils.cc:944-977 @ a9f50e522efa); a
					// forced break drops it.
					if !isForced && childResult != nil && childResult.Fragment != nil &&
						bla.space.FragmentainerBlockSize != Indefinite {
						bla.containerBuilder.PropagateSpaceShortage(CalculateSpaceShortage(bla.space, childResult, fragOff, bla.fragmentainerCapacityForChildren()))
					}
					// The appeal was set by BreakBeforeChildIfNeeded.
					addBreakBeforeChild(child, nil, isForced)
					break
				}
			}

			// It is now safe to take over the child's exclusion space (Blink
			// HandleInflow, block_layout_algorithm.cc:2675-2677 @
			// a9f50e522efa); floats do not escape a new formatting context.
			if !isChildNewFC && childResult.ExclusionSpace != nil {
				bla.containerBuilder.SetExclusionSpace(childResult.ExclusionSpace)
			}

			rawMargin := childStyle.GetMargin()
			autoInlineStart, autoInlineEnd, _, _ := PhysicalAutoMarginsToLogical(rawMargin, wdm)
			if autoInlineStart || autoInlineEnd {
				childInlineSize := NewLogicalFragment(wdm, childResult.Fragment).InlineSize()
				// CSS 2.1 §10.3.3: auto margins center within the containing block.
				// For BFC children, float exclusions reduce the available space (they
				// must not overlap float margin boxes per §9.5). For non-BFC children,
				// use the full containing block width — they flow as if floats don't exist.
				floatStartForMargin, floatEndForMargin := floatStartOff, floatEndOff
				if !isChildNewFC {
					floatStartForMargin, floatEndForMargin = 0, 0
				}
				remaining := childAvailableInline - childInlineSize - floatStartForMargin - floatEndForMargin
				if remaining > 0 {
					if autoInlineStart && autoInlineEnd {
						childInlineOffset = floatStartForMargin + remaining/2
					} else if autoInlineStart {
						childInlineOffset = floatStartForMargin + remaining - childMargins.InlineEnd
					}
					// autoEnd && !autoStart: start margin is already used, end absorbs remaining (no change)
				}
			}

			// The child's relative offset, plus its inline boxes' for a
			// block-in-inline.
			relativeOffset := bla.containerBuilder.relativeOffsetFor(childResult.Fragment)
			relativeOffset.InlineOffset += blockInInlineRelativeOffset.InlineOffset
			relativeOffset.BlockOffset += blockInInlineRelativeOffset.BlockOffset

			// Step 5: Handle parent-child top margin collapsing.
			// CSS 2.1 §8.3.1: parent-child collapsing requires that the
			// child has no clearance (a `clear` value, or a float drop).
			// Step 6: Normal margin resolution: the child's border edge
			// (Blink LogicalFromBfcOffsets of child_bfc_offset,
			// block_layout_algorithm.cc:2016-2018 @ a9f50e522efa). A child
			// adjoining this block's block-start (whose BFC block-offset
			// Blink has not resolved yet) sits at 0 (CalculateLogicalOffset,
			// :571-592) and propagates the strut upward (the margin moves
			// outside the parent). A self-collapsing child sits where the
			// margins collapsed up to it resolve
			// (PositionSelfCollapsingChildWithParentBfc, :3066-3081),
			// propagates nothing and leaves the cursor where it was
			// (ComputeInflowPosition, :2894-2898).
			var actualChildBlockOff float64
			if isAdjoiningBlockStart() {
				if !isSelfCollapsing {
					propagatedTopMargin = prevMarginStrut
				}
			} else {
				actualChildBlockOff = borderEdgeAt(prevMarginStrut)
			}
			childOffset := LogicalOffset{
				InlineOffset: childInlineOffset,
				BlockOffset:  actualChildBlockOff,
			}
			// Add the child and take over its break token, break info
			// (block-end for fragmentation, a break inside it, forced break,
			// shortage, appeal, a column spanner inside it) and break
			// values (Blink BoxFragmentBuilder::AddResult,
			// box_fragment_builder.cc:180-233 @ a9f50e522efa).
			bla.containerBuilder.AddResult(child, childResult, childOffset, relativeOffset)
			if !isSelfCollapsing {
				blockCursor = actualChildBlockOff + childBlockSize
			}

			// Inherit propagated OOF candidates from child.
			// Non-positioned children propagate their abspos descendants
			// upward for resolution by the containing block (this element
			// or a higher ancestor). Phase 25 Cmt-3: also fire when the
			// child carries FragmentedOofData (an OOF whose CB was promoted
			// inside it, deferred for outer-fragmentation-root drain).
			if len(childResult.PropagatedOOFCandidates) > 0 ||
				(childResult.Fragment != nil && childResult.Fragment.FragmentedOofData != nil) {
				bla.containerBuilder.PropagateOOFPositionedInfo(childResult, childStyle,
					LogicalOffset{InlineOffset: childInlineOffset, BlockOffset: actualChildBlockOff}, relativeOffset)
			}

			// A self-collapsing child cleared past floats resolves this
			// block's BFC block-offset (has_clearance_past_adjoining_floats
			// forces it, FinishInflow, block_layout_algorithm.cc:2436-2465):
			// later children no longer adjoin its block-start. With actual
			// clearance its end margin strut also stays inside this block
			// (ComputeInflowPosition :3044-3047, FinishLayout :1287-1294 @
			// a9f50e522efa).
			if !isSelfCollapsing || hasClearance {
				firstNonEmptyChild = false
			}
			selfCollapsingChildHadClearance = isSelfCollapsing &&
				(isPushedByFloats || selfCollapsingChildHadClearance)
			// A block-in-inline line counts toward orphans and widows, as a
			// line box (FragmentBuilder::PropagateFromFragment,
			// fragment_builder.cc:487-492 @ a9f50e522efa).
			if child.IsBlockInInline() && bla.space.HasBlockFragmentation {
				bla.containerBuilder.SetLineCount(bla.containerBuilder.LineCount() + 1)
			}
			// A child resumed in a parallel flow leaves no breakpoint before
			// the next sibling, so a forced break-before on that sibling does
			// not fire again (Blink HandleInflow, block_layout_algorithm.cc:
			// 2767-2772 @ a9f50e522efa).
			if bla.space.HasBlockFragmentation && !hasBreakOpportunityBeforeNextChild {
				hasBreakOpportunityBeforeNextChild = HasBreakOpportunityBeforeNextChild(wdm, childResult.Fragment, entry.Token)
			}

			// CSS Inline §4.2: propagate the first in-flow block child's
			// first baseline as this container's first baseline.
			// CSS Align §9.1: orthogonal children don't contribute baselines
			// in the parent's writing mode, so skip them.
			// Shared guard: capture first baseline in layout order (mirrors Blink).
			if !isOrthogonal && !hasFirstChildBaseline && childResult.HasBaseline {
				firstChildBaseline = childResult.Baseline
				firstChildBlockOffset = actualChildBlockOff
				hasFirstChildBaseline = true
			}

			// CSS Lists §3 / Blink PositionOrPropagateListMarker: claim a
			// pending OUTSIDE marker against this block child's first baseline.
			// The marker is added as a content-relative child of the list-item
			// builder; if the marker's ascent is taller than the content's,
			// the content child is pushed down by contentPush instead (the
			// child fragment is re-offset inside positionOrPropagateListMarker).
			if !isOrthogonal {
				if contentPush := bla.positionOrPropagateListMarker(
					childResult.Fragment, childResult, actualChildBlockOff,
				); contentPush > 0 {
					blockCursor += contentPush
					if hasFirstChildBaseline && firstChildBlockOffset == actualChildBlockOff {
						firstChildBlockOffset += contentPush
					}
					if hasLastChildBaseline && lastChildBlockOffset == actualChildBlockOff {
						lastChildBlockOffset += contentPush
					}
					actualChildBlockOff += contentPush
				}
			}

			// Track the last in-flow block child's baseline for
			// CSS 2.1 §10.8.1 inline-block baseline propagation.
			// Only a genuine last-baseline (from a line box, propagated up
			// through block descendants) counts — never the first-baseline.
			// If no descendant has a last-baseline, the enclosing
			// inline-block falls back to its bottom margin edge at
			// atomic-inline placement time (inline_layout.go).
			// CSS Align §9.1: orthogonal children don't contribute baselines.
			//
			// Flex/grid containers that don't set LastBaseline fall back to
			// Baseline (first baseline): they export content-derived baseline
			// regardless of overflow per CSS Align §4.3.
			if !isOrthogonal {
				childLastBL := childResult.LastBaseline
				if childLastBL == 0 && childStyle != nil {
					d := childStyle.GetDisplay()
					if d == css.DisplayFlex || d == css.DisplayGrid ||
						d == css.DisplayInlineFlex || d == css.DisplayInlineGrid {
						childLastBL = childResult.Baseline
					}
				}
				if childLastBL > 0 {
					lastChildBaseline = childLastBL
					lastChildBlockOffset = actualChildBlockOff
					hasLastChildBaseline = true
				}
			}

			// Reset margin strut to the child's block-end margin; a
			// self-collapsing child passes the strut through
			// (ComputeInflowPosition, block_layout_algorithm.cc:2894-2898 @
			// a9f50e522efa).
			if !isSelfCollapsing {
				prevMarginStrut = childResult.EndMarginStrut
			}
			prevMarginStrut.Append(childMargins.BlockEnd)

			// A child that broke inside before its block end applies no
			// trailing margin (ComputeInflowPosition,
			// block_layout_algorithm.cc:3014-3031 @ a9f50e522efa).
			if token := childResult.BreakToken; bla.space.HasBlockFragmentation &&
				token.IsBreakInside() && !token.IsAtBlockEnd {
				prevMarginStrut = MarginStrut{}
			}
			// A break in this box's own flow (AddResult → PropagateBreakInfo
			// decided) finishes the fragment: no more siblings are laid out
			// (block_layout_algorithm.cc:1108-1115 @ a9f50e522efa). A child
			// that broke only in a parallel flow leaves the walk going; a
			// child that did not break is never stopped here — whether it or
			// the next sibling fits was BreakBeforeChildIfNeeded's call.
			if bla.space.HasBlockFragmentation && bla.containerBuilder.HasInflowChildBreakInside() {
				pendingIntrinsicAtBreak = blockCursor
				pendingHaveIntrinsicAtBreak = true
				break
			}
		}

		// Past the child the walk stopped at (or at its end), no child is
		// left to start: all children are seen, even if parallel flows still
		// need more fragmentainers (block_layout_algorithm.cc:1150-1161 @
		// a9f50e522efa).
		if childIterator.NextChild().AtEnd() {
			bla.containerBuilder.SetHasSeenAllChildren()
		}
	}

	// CSS 2.1 §8.3.1: The bottom margin of an in-flow block box with a
	// 'height' of 'auto' collapses with its last child's bottom margin if
	// the box has no bottom border and no bottom padding. When block-size
	// is explicit (not auto), OR there is border/padding at block-end,
	// the trailing margin does NOT propagate out as EndMarginStrut.
	canPropagateBottom := !bla.space.IsNewFormattingContext &&
		!hasExplicitBlock && !selfCollapsingChildHadClearance &&
		geom.Border.BlockEnd == 0 && geom.Padding.BlockEnd == 0

	// Gate A: a fragment that breaks inside adds no trailing margin.
	// A fragment interrupted by a spanner keeps its trailing margins and
	// float clearance, as any finished fragment does (Blink's FinishLayout
	// adds the end margin strut whether or not the box breaks,
	// block_layout_algorithm.cc:1298-1349 @ a9f50e522efa).
	breaksBeforeEnd := bla.containerBuilder.HasInflowChildBreakInside() && !bla.containerBuilder.FoundColumnSpanner()
	if !breaksBeforeEnd {
		// The strut is reset even when it resolves to nothing: a
		// discarding strut (DiscardMargins) must not leak out as the end
		// margin strut, or it would discard the next sibling's margin.
		if bla.space.IsNewFormattingContext {
			// BFC roots: trailing margins don't propagate out, and they
			// extend the auto height (CSS 2.1 §10.6.7).
			blockCursor += prevMarginStrut.Resolve()
			prevMarginStrut = MarginStrut{} // consumed
		} else if !canPropagateBottom {
			if !hasExplicitBlock {
				// Auto block-size with block-end border/padding: the last child's
				// margin is trapped inside the parent and extends the auto height
				// (CSS 2.1 §10.6.3). The margin does not propagate out.
				blockCursor += prevMarginStrut.Resolve()
			}
			// Explicit block-size: margin doesn't propagate and doesn't extend
			// height (the height is already fixed).
			prevMarginStrut = MarginStrut{}
		}
	}

	// Gate B (option-b step 6.4.a, plan §3.3.3): float-clearance
	// extension for auto block-size is suppressed on the broken path.
	// Same rationale as Gate A. Currently dormant.
	// CSS 2.1 §10.6.7: For elements that own floats (BFC roots or elements
	// that contain their own floats), auto block-size extends to clear them.
	// Elements that only inherit floats from a parent BFC do not extend.
	// clearedBlock (the content block offset past those floats) is also
	// computed under an explicit block-size: the table-cell vertical-align
	// below measures content extent including floats, as Blink's
	// intrinsic_block_size_ does for every new formatting context
	// (block_layout_algorithm.cc:1255-1261 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520).
	clearedBlock := blockCursor
	if !breaksBeforeEnd && hasOwnFloats {
		bfcCursor := bfcBlockOrigin + blockCursor
		clearedBlockBfc := bla.containerBuilder.GetExclusionSpace().ClearanceOffset(css.ClearBoth, layoutunit.FromFloat64Round(bfcCursor), wdm).Float64()
		clearedBlock = max(blockCursor, clearedBlockBfc-bfcBlockOrigin)
		if !hasExplicitBlock {
			blockCursor = clearedBlock
		}
	}
	// Compute final block-size.
	intrinsicBlockSize := blockCursor
	// CSS Contain 1 §4.2: size containment — collapse intrinsic block-size to 0
	// when no explicit block-size is set (see contain_utils.go).
	// The content block-size before containment clamps it: what
	// AlignBlockContent aligns (Blink's unconstrained_intrinsic_block_size,
	// block_layout_algorithm.cc:1504 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	unconstrainedIntrinsicBlockSize := intrinsicBlockSize
	intrinsicBlockSize = sizeContainedIntrinsicBlockSize(bla.style, hasExplicitBlock, intrinsicBlockSize)

	// CSS Lists §3 / Blink PositionListMarkerWithoutLineBoxes: if a list item
	// produced no line boxes / no child with a baseline, its still-pending
	// OUTSIDE marker is top-aligned at block offset 0 and contributes its
	// block-size to the list item's intrinsic block size. This MUST run before
	// builder.SetIntrinsicBlockSize / SetSize so the bump propagates (Blink
	// runs it from FinishLayout, before the builder is finalized).
	// Not when a child broke inside: the marker goes with the content of a
	// later fragment (block_layout_algorithm.cc:1440-1448 @ a9f50e522efa).
	if !bla.containerBuilder.HasInflowChildBreakInside() {
		bla.positionListMarkerWithoutLineBoxes(&intrinsicBlockSize)
	}

	// A fragment that breaks — a child broke inside, or a child continues
	// in a parallel flow past this box's end — carries no block-end edge in
	// slice mode; nor does a fragment past the box's block end. Read after
	// canPropagateBottom, which needs the box's own edge. Mirrors Blink's
	// FinishFragmentation sides (block_layout_algorithm.cc:1351-1357;
	// fragmentation_utils.cc:527-545, :773-779 @ a9f50e522efa).
	shouldBreakInside := bla.containerBuilder.ShouldBreakInside()
	var finish FinishFragmentationResult
	var finalBlockSize float64
	if InvolvedInBlockFragmentation(bla.space) {
		// In a fragmentation context, or resuming after a break inside
		// (InvolvedInBlockFragmentation, block_layout_algorithm.cc:1479 @
		// a9f50e522efa), the stitched block-size (declared, or earlier
		// fragments plus this content, clamped by min/max) is
		// distributed by FinishFragmentation, which breaks the box itself
		// when the rest does not fit (fragmentation_utils.cc:547-822); a
		// column box takes its fragmentainer's block-size
		// (FinishFragmentationForFragmentainer, :823-866).
		// A new formatting context is broken by a float of its own that
		// broke before or inside, too (fragmentation_utils.cc:689-691 @
		// a9f50e522efa).
		wasBrokenByChild := bla.containerBuilder.HasInflowChildBreakInside() ||
			(bla.space.IsNewFormattingContext && bla.containerBuilder.GetExclusionSpace().HasFragmentainerBreak())
		finish = finishFragmentation(intrinsicBlockSize, shouldBreakInside, wasBrokenByChild)
		// The only breakpoint left is in the block-end edge, and a better
		// one was found earlier: lay out again and break there
		// (FinalizeForFragmentation → kNeedsEarlierBreak,
		// block_layout_algorithm.cc:1479-1484, :617-626 @ a9f50e522efa).
		// FinishFragmentation reported this box's space shortage to the
		// builder first (fragmentation_utils.cc:798-800), so the relayout
		// repeats it.
		if finish.NeedsEarlierBreak && bla.earlyBreak == nil {
			bla.containerBuilder.PropagateSpaceShortage(finish.SpaceShortage)
			return bla.relayoutAndBreakEarlier(bla.containerBuilder.GetEarlyBreak())
		}
		// A block-axis clipping box reached its end with content still
		// breaking inside: lay out again without fragmentation
		// (FinalizeForFragmentation → kDisableFragmentation →
		// RelayoutWithoutFragmentation, block_layout_algorithm.cc:1485-1486,
		// :652-654 @ a9f50e522efa).
		if finish.DisableFragmentation {
			return bla.relayoutWithoutFragmentation()
		}
		finalBlockSize = max(0, finish.FragmentBlockSize-geom.BlockBorderPadding())
	} else {
		finalBlockSize = intrinsicBlockSize
		if hasExplicitBlock {
			finalBlockSize = explicitBlockSize
		}

		// Apply min/max block-size constraints per CSS 2.1 §10.7.
		// Order matters: max-height is applied first (step 2), then min-height (step 3).
		// When min-height > max-height, min-height wins because step 3 overrides step 2.
		// Intrinsic keywords (min-content / max-content / fit-content) resolve
		// against intrinsicBlockSize — the natural content height we just computed.
		minBlock := ResolveMinBlockSizeWithIntrinsic(bla.style, wdm, bla.space, geom, intrinsicBlockSize)
		// The root element must fill at least the ICB block-size (ForcedMinBlockSize).
		if bla.space.ForcedMinBlockSize > minBlock {
			minBlock = bla.space.ForcedMinBlockSize
		}
		if maxBlock, hasMax := ResolveMaxBlockSizeWithIntrinsic(bla.style, wdm, bla.space, geom, intrinsicBlockSize); hasMax {
			if finalBlockSize > maxBlock {
				finalBlockSize = maxBlock
			}
		}
		if finalBlockSize < minBlock {
			finalBlockSize = minBlock
		}
		clearBlockEndIfExcluded(shouldBreakInside, bla.containerBuilder.HasInflowChildBreakInside())
	}
	didBreakSelf := finish.DidBreakSelf
	selfBreakShortage := finish.SpaceShortage

	// Set the fragment size.
	bla.containerBuilder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})

	bla.containerBuilder.SetIntrinsicBlockSize(intrinsicBlockSize)

	// Set first baseline: for flex align-items:baseline (uses first line).
	// CSS Inline §4.2: the first baseline of a block container is:
	//   1. The first line box's baseline (inline children), OR
	//   2. The first in-flow block child's propagated first baseline.
	if firstLineAscent > 0 {
		bla.containerBuilder.SetBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
	} else if hasFirstChildBaseline {
		bla.containerBuilder.SetBaseline(geom.Border.BlockStart + geom.Padding.BlockStart +
			firstChildBlockOffset + firstChildBaseline)
	}
	// Set last baseline: for inline-block alignment §10.8.1 (uses last line box).
	// For inline children, lastChildBaseline is the last line's baseline offset.
	// For block children, it's the last child's propagated baseline.
	//
	// Non-flex/grid scroll containers synthesize their §10.8.1 baseline from
	// block end — mirrors Blink LayoutBox::UseLogicalBottomMarginEdgeForInlineBlockBaseline.
	// This lets an enclosing non-scrollable inline-block propagate the correct
	// synthesized value via its own LastBaseline.
	logMargins := ResolveMargins(bla.style, wdm, bla.space.AvailableSize.InlineSize.Float64())
	if bla.style != nil {
		d := bla.style.GetDisplay()
		isFlexGrid := d == css.DisplayFlex || d == css.DisplayGrid ||
			d == css.DisplayInlineFlex || d == css.DisplayInlineGrid
		if !isFlexGrid && (bla.style.GetOverflowX() != css.OverflowVisible || bla.style.GetOverflowY() != css.OverflowVisible) {
			bla.containerBuilder.SetLastBaseline(finalBlockSize + geom.BlockBorderPadding() + logMargins.BlockEnd)
		} else if hasLastChildBaseline {
			bla.containerBuilder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart +
				lastChildBlockOffset + lastChildBaseline)
		} else if firstLineAscent > 0 {
			bla.containerBuilder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
		}
	} else if hasLastChildBaseline {
		bla.containerBuilder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart +
			lastChildBlockOffset + lastChildBaseline)
	} else if firstLineAscent > 0 {
		bla.containerBuilder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
	}

	// Set box data for the renderer.
	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
	physMargin := ToPhysicalEdges(logMargins, wdm)
	bla.containerBuilder.SetBoxData(&PhysicalBoxData{
		Margin:    physMargin,
		Border:    physBorder,
		Padding:   physPadding,
		Scrollbar: physScrollbar,
	})

	bla.containerBuilder.SetEndMarginStrut(prevMarginStrut)

	// Block-axis content alignment (align-content, the internal button
	// centering) for block containers other than table cells, after sizing
	// and before out-of-flow layout. Mirrors BlockLayoutAlgorithm::
	// FinishLayout's AlignBlockContent call (block_layout_algorithm.cc:
	// 1500-1506 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); Blink moves
	// children only for a new formatting context (MoveChildrenInDirection
	// DCHECKs is_new_fc_).
	if bla.space.IsNewFormattingContext && bla.space.TableSectionData == nil {
		includeBlockEnd := !InvolvedInBlockFragmentation(bla.space) || finish.IncludeBlockEnd
		AlignBlockContent(bla.style, bla.node, bla.space.BreakToken, finalBlockSize,
			unconstrainedIntrinsicBlockSize, includeBlockEnd, bla.containerBuilder)
	}

	// CSS 2.1 §17.5.4 — table-cell vertical-align within the cell's own
	// block-size. Mirrors Blink FinalizeTableCellLayout
	// (core/layout/table/table_layout_utils.cc:1575-1640 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520), which
	// BlockLayoutAlgorithm::FinishLayout runs for every table cell
	// (core/layout/block_layout_algorithm.cc:1457-1458, gated on
	// ConstraintSpace::IsTableCell()): the free space between the cell's
	// block-size (e.g. an explicit `height` above its content) and its
	// intrinsic block-size moves the children by free/2 (middle) or free
	// (bottom). louis14's IsTableCell() signal is a non-nil
	// TableSectionData, which only TableLayoutAlgorithm sets on a cell's
	// space. Table parts outside a table are wrapped in an anonymous table
	// at tree-build time (generateMissingTableParents), so a display
	// table-cell box reaching block layout without one is a layout root or a
	// box Blink blockifies, and gets no cell alignment.
	//
	// When distribution grows the row past the cell's block-size,
	// table_layout.go's Phase-3 resize shifts the children again by the
	// row-growth part; the two shifts sum to Blink's single shift against
	// the final row block-size.
	//
	// The content extent includes the cell's own floats: a cell is a new
	// formatting context, whose Blink intrinsic block-size encloses its
	// floats even under an explicit height (intrinsicBlockSize already
	// does without one).
	cellContentBlockSize := intrinsicBlockSize
	if hasExplicitBlock {
		cellContentBlockSize = max(cellContentBlockSize, clearedBlock)
	}
	if bla.style != nil && bla.style.GetDisplay() == css.DisplayTableCell &&
		bla.space.TableSectionData != nil && finalBlockSize > cellContentBlockSize {
		va := bla.style.GetVerticalAlign()
		var vaShift float64
		switch va {
		case css.VerticalAlignMiddle:
			vaShift = (finalBlockSize - cellContentBlockSize) / 2
		case css.VerticalAlignBottom:
			vaShift = finalBlockSize - cellContentBlockSize
		}
		if vaShift > 0 {
			for i := range bla.containerBuilder.children {
				bla.containerBuilder.children[i].offset.BlockOffset += vaShift
			}
			for i := range bla.containerBuilder.outOfFlowCandidates {
				bla.containerBuilder.outOfFlowCandidates[i].StaticPosition.Offset.BlockOffset += vaShift
			}
		}
	}

	// CSS 2.1 §10.6.4 / CSS-POSITION-3: Lay out out-of-flow children.
	propagatedOOF := resolveOutOfFlowCandidates(bla.ctx, bla.containerBuilder, bla.space, geom,
		LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize}, previousConsumedContentBlockSize(bla.space, declaredBlockStart))

	result := bla.containerBuilder.Build()

	// The outgoing break token, built from the builder's break state
	// (Blink BoxFragmentBuilder::ToBoxFragment → BlockBreakToken::Create,
	// box_fragment_builder.cc:707-711 @ a9f50e522efa), when a child broke
	// (in this flow or a parallel one), the box broke itself
	// (FinishFragmentation), or monolithic overflow is still to steer
	// clear of (ShouldBreakInside, box_fragment_builder.h:494-497). The
	// consumed block-size of an in-flow child break is the content extent
	// where the walk stopped.
	if shouldBreakInside || didBreakSelf || finish.MonolithicOverflow > 0 {
		var childBreakTokens []*BlockBreakToken
		var hasSeenAllChildren bool
		consumed := finalBlockSize
		if shouldBreakInside {
			childBreakTokens = bla.containerBuilder.ChildBreakTokens()
			hasSeenAllChildren = bla.containerBuilder.HasSeenAllChildren()
			if pendingHaveIntrinsicAtBreak {
				consumed = pendingIntrinsicAtBreak
			}
		} else if result.BreakToken != nil {
			// didBreakSelf alone (no children-loop break). Defensive
			// copy of any pre-existing BreakToken fields — pre-6.4.c
			// the line-2012 reader did the same.
			childBreakTokens = result.BreakToken.ChildBreakTokens
			hasSeenAllChildren = result.BreakToken.HasSeenAllChildren
		}
		// A box that breaks itself with no child broken has gone through
		// all its children, so the continuation lays none of them out
		// again (Blink SetHasSeenAllChildren once the child iterator is at
		// its end, block_layout_algorithm.cc:1150-1161 @ a9f50e522efa).
		// Driver: multicol-span-all-children-height-001, where a
		// one-line block taller than its column would otherwise repeat
		// its text in the next column.
		// So has one breaking only for monolithic overflow.
		if !shouldBreakInside {
			hasSeenAllChildren = true
		}
		consumedBlockSize := previouslyConsumed + consumed
		var isAtBlockEnd bool
		if bla.space.HasBlockFragmentation {
			// FinishFragmentation's border-box consumed block-size and
			// block-end decision (fragmentation_utils.cc:682-684, :717-771);
			// FinishFragmentationForFragmentainer never sets IsAtBlockEnd
			// (:823-866 @ a9f50e522efa).
			consumedBlockSize = finish.ConsumedBlockSize
			isAtBlockEnd = finish.IsAtBlockEnd
		}
		result.BreakToken = &BlockBreakToken{
			Node:                    bla.node,
			IsCausedByColumnSpanner: bla.containerBuilder.FoundColumnSpanner(),
			ConsumedBlockSize:       layoutunit.FromFloat64Round(consumedBlockSize),
			SequenceNumber:          sequenceNumberAfter(incomingBreakToken),
			ChildBreakTokens:        childBreakTokens,
			HasSeenAllChildren:      hasSeenAllChildren,
			IsAtBlockEnd:            isAtBlockEnd,
			MonolithicOverflow:      finish.MonolithicOverflow,
		}
		// Carry the unpositioned outside ::marker into the break token: a
		// continuation that resumes before any baseline-bearing child claims
		// the marker must re-seed it (Blink BlockBreakToken::
		// has_unpositioned_list_marker_).
		if bla.containerBuilder.GetUnpositionedListMarker().IsValid() {
			result.BreakToken.HasUnpositionedListMarker = true
		}
		if didBreakSelf {
			result.DidBreakSelf = true
			// LOU-365: surface the self-break shortage (Blink
			// FinishFragmentation → PropagateSpaceShortage,
			// fragmentation_utils.cc:798-800 @ a9f50e522efa).
			result.MinSpaceShortage = MinPositiveSpaceShortage(result.MinSpaceShortage, selfBreakShortage)
		}
		if shouldBreakInside {
			// Raise only: Build already recorded the accumulated value.
			if pendingIntrinsicAtBreak > max(NewLogicalFragment(wdm, result.Fragment).BlockSize(), result.BlockSizeForFragmentation) {
				result.BlockSizeForFragmentation = pendingIntrinsicAtBreak
			}
		}
	}

	bla.containerBuilder.AdjustSpaceShortageForBreakAvoidRoot(bla.ctx, result)

	// A fragment that did not break offers the best breakpoint found
	// inside it to its parent (UpdateEarlyBreakAtBlockChild), unless it is
	// known to fit: then any break inside it is a parallel flow, which
	// cannot help honor break avoidance after it. Mirrors LayoutResult's
	// constructor (layout_result.cc:265-274) and BoxFragmentBuilder::Build
	// (box_fragment_builder.cc:764-769 @ a9f50e522efa).
	if result.BreakToken == nil && bla.space.HasBlockFragmentation && !bla.containerBuilder.IsKnownToFitInFragmentainer() {
		result.EarlyBreak = bla.containerBuilder.GetEarlyBreak()
	}

	// Attach propagated OOF candidates for the parent to resolve.
	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	// CSS 2.1 §8.3.1: Propagate first child's margin for parent-child collapsing.
	if !propagatedTopMargin.IsEmpty() {
		result.PropagatedTopMargin = propagatedTopMargin
	}

	return result
}

// resolveOutOfFlowCandidates runs the OutOfFlowLayoutPart of the block
// container builder lays out and returns the candidates it must propagate
// to an ancestor containing block. contentSize is the content-box size of
// the fragment and previousConsumedContent the content block-size its
// earlier fragments consumed. Shared by BlockLayoutAlgorithm and
// FieldsetLayoutAlgorithm, standing in for the OutOfFlowLayoutPart run of
// Blink's BoxFragmentBuilder::HandleOofsAndSpecialDescendants
// (box_fragment_builder.cc:660-671, called from block_layout_algorithm.cc
// and fieldset_layout_algorithm.cc:147 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func resolveOutOfFlowCandidates(ctx *LayoutContext, builder *BoxFragmentBuilder, space ConstraintSpace, geom FragmentGeometry, contentSize LogicalSize, previousConsumedContent float64) []OutOfFlowCandidate {
	if !builder.NeedsOutOfFlowLayoutPart() {
		return nil
	}
	paddingBoxSize := LogicalSize{
		InlineSize: contentSize.InlineSize + geom.Padding.InlineSum(),
		BlockSize:  contentSize.BlockSize + geom.Padding.BlockSum(),
	}
	return NewOutOfFlowLayoutPart(ctx, builder, space, paddingBoxSize, geom.Padding, previousConsumedContent).Run()
}

// adoptBlockInInlineResult applies to the laid-out block-in-inline wrapper
// what Blink applies to the block-in-inline item on its line: the inline
// boxes around it paint it in their opacity group, their relative offsets
// move it (InlineLayoutStateStack::ApplyRelativePositioning,
// inline/inline_box_state.cc:940-984), and the innermost of them that
// contains an out-of-flow descendant becomes its inline containing block
// (BoxData::CreateBoxFragment propagating the block-in-inline result into
// the inline box fragment, :1066-1091 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Returns the relative offset.
func (bla *BlockLayoutAlgorithm) adoptBlockInInlineResult(wdm WritingDirectionMode, wrapper *LayoutInputNode, result *LayoutResult) LogicalOffset {
	bla.prepareInlineItems(wdm)
	c := bla.inlineItemsCache
	bii, ok := c.blockInInline[wrapper]
	if !ok {
		return LogicalOffset{}
	}
	item := c.data.Items[bii.index]
	if result.Fragment != nil {
		result.Fragment.PaintGroup = item.EnclosingPaintGroup
	}
	for i := range result.PropagatedOOFCandidates {
		if cand := &result.PropagatedOOFCandidates[i]; cand.InlineContainer == nil {
			cand.InlineContainer = c.atomicInlineContainers[item].For(*cand)
		}
	}
	var offset LogicalOffset
	for _, open := range bii.openInlineStack {
		rel := ComputeRelativeOffsetForInline(wdm, bla.containerBuilder.ChildAvailableSize(), open.Style)
		offset.InlineOffset += rel.InlineOffset
		offset.BlockOffset += rel.BlockOffset
	}
	return offset
}

// handleFloat lays out and positions a float child of this block
// formatting context (CSS 2.1 §9.5) through PositionFloat into the builder's
// exclusion space, and adds it unless it breaks before. A float that breaks
// before or inside continues in the next fragmentainer in a parallel flow:
// its token joins the builder's child break tokens, and the walk goes on.
// originBfcBlockOffset is the next border edge after the preceding in-flow
// content. Mirrors Blink's BlockLayoutAlgorithm::HandleFloat
// (block_layout_algorithm.cc:1722-1822 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (bla *BlockLayoutAlgorithm) handleFloat(
	child *LayoutInputNode,
	childBreakToken *BlockBreakToken,
	incomingBreakToken *BlockBreakToken,
	originBfcBlockOffset float64,
	contentInlineSize float64,
	availableBlock float64,
	bfcBlockOrigin float64,
	bfcInlineOrigin float64,
	bfcContainerInlineSize float64,
) {
	space := bla.space
	if childBreakToken != nil {
		// Move past monolithic content inside the float overflowing from a
		// previous fragmentainer, less any such overflow of the parent
		// flow: floats establish a parallel flow (:1740-1746).
		originBfcBlockOffset += childBreakToken.MonolithicOverflow
		if incomingBreakToken != nil {
			originBfcBlockOffset -= incomingBreakToken.MonolithicOverflow
		}
	}

	if space.HasBlockFragmentation {
		// A forced break after the preceding block breaks before the
		// float: break-before cannot be specified on floats (:1748-1759).
		if IsForcedBreakValue(space, bla.containerBuilder.JoinedBreakBetweenValue("auto")) {
			bla.containerBuilder.AddBreakBeforeChild(child, nil, true)
			return
		}
	}

	childSize := LogicalSize{InlineSize: contentInlineSize, BlockSize: availableBlock}
	uf := bla.newUnpositionedFloat(child, childBreakToken, childSize, childSize, bfcInlineOrigin,
		originBfcBlockOffset, bfcBlockOrigin, bfcContainerInlineSize)
	es, _, breakToken := bla.addPositionedFloat(uf, bla.containerBuilder.GetExclusionSpace(), bfcBlockOrigin, bfcInlineOrigin)
	bla.containerBuilder.SetExclusionSpace(es)
	// A break before the float (:1792-1800), or inside it (AddResult →
	// FragmentBuilder::PropagateFromFragment, fragment_builder.cc:455-464).
	if breakToken != nil {
		bla.containerBuilder.AddBreakToken(breakToken, true)
	}
}

// placeListMarkerAtFirstLine places the unpositioned outside list marker,
// if any, against the first line box, whose top is at firstLineTop and whose
// ascent is firstLineAscent, and returns by how much the line must go down:
// when the marker's ascent is taller than the line's (an image marker on a
// text line) the content is pushed instead of the marker. The inline layout
// adds the push to the first line's offset, and so to every later line's.
// LOU-337 / css-pseudo marker-content-012. Mirrors BlockLayoutAlgorithm::
// PositionOrPropagateListMarker applying UnpositionedListMarker::AddToBox's
// content adjust to the child's offset (block_layout_algorithm.cc:
// 2720-2724, :3878-3920; unpositioned_list_marker.cc:81-123 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (bla *BlockLayoutAlgorithm) placeListMarkerAtFirstLine(firstLineAscent, firstLineTop float64) float64 {
	marker := bla.containerBuilder.GetUnpositionedListMarker()
	if !marker.IsValid() {
		return 0
	}
	markerResult := marker.Layout(bla.ctx, bla.space)
	if markerResult == nil {
		return 0
	}
	contentPush := marker.AddToBox(bla.ctx, markerResult, firstLineAscent, firstLineTop, bla.containerBuilder)
	bla.containerBuilder.ClearUnpositionedListMarker()
	return contentPush
}

// newUnpositionedFloat returns the UnpositionedFloat for a float of this
// block's formatting context: its origin, this block's space, and this
// block's fragmentainer position.
func (bla *BlockLayoutAlgorithm) newUnpositionedFloat(
	child *LayoutInputNode,
	token *BlockBreakToken,
	availableSize, percentageSize LogicalSize,
	originBfcLineOffset, originBfcBlockOffset, expectedBfcBlockOffset, bfcContainerInlineSize float64,
) *UnpositionedFloat {
	return &UnpositionedFloat{
		Node:                     child,
		Token:                    token,
		AvailableSize:            availableSize,
		PercentageSize:           percentageSize,
		OriginBfcLineOffset:      originBfcLineOffset,
		OriginBfcBlockOffset:     originBfcBlockOffset,
		ParentSpace:              bla.space,
		FragmentainerBlockSize:   bla.fragmentainerCapacityForChildren(),
		FragmentainerBlockOffset: bla.fragmentainerOffsetForChildren,
		ExpectedBfcBlockOffset:   expectedBfcBlockOffset,
		BfcContainerInlineSize:   bfcContainerInlineSize,
		Ctx:                      bla.ctx,
	}
}

// addPositionedFloat positions uf through PositionFloat and, unless it
// breaks before, adds it to this block, propagating its break info. It
// returns the updated exclusion space, the positioned float and the break
// token to resume the float with in the next fragmentainer (a parallel
// flow), if any; the caller collects the token. Shared by
// a float in a block flow (BlockLayoutAlgorithm::HandleFloat,
// block_layout_algorithm.cc:1761-1821) and one in an inline formatting
// context (LineBreaker::HandleFloat → PositionFloat, inline/
// line_breaker.cc:3815-3839; InlineLayoutAlgorithm::PositionFloat,
// inline/inline_layout_algorithm.cc:1782-1819 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the AddResult part is
// BoxFragmentBuilder::PropagateBreakInfo (box_fragment_builder.cc:479-617).
func (bla *BlockLayoutAlgorithm) addPositionedFloat(
	uf *UnpositionedFloat,
	es *ExclusionSpace,
	bfcBlockOrigin, bfcInlineOrigin float64,
) (*ExclusionSpace, PositionedFloat, *BlockBreakToken) {
	space := bla.space
	positioned := PositionFloat(uf, &es)

	if positioned.MinimumSpaceShortage > 0 {
		bla.containerBuilder.PropagateSpaceShortage(positioned.MinimumSpaceShortage)
	} else if positioned.TallestUnbreakableBlockSize > 0 {
		bla.containerBuilder.PropagateTallestUnbreakableBlockSize(positioned.TallestUnbreakableBlockSize)
	}

	if positioned.BreakBeforeToken != nil {
		// The float goes to the next fragmentainer; siblings that fit may
		// still follow here (:1792-1800).
		return es, positioned, positioned.BreakBeforeToken
	}

	result := positioned.LayoutResult
	offset := LogicalOffset{
		InlineOffset: positioned.BfcLineOffset - bfcInlineOrigin,
		BlockOffset:  positioned.BfcBlockOffset - bfcBlockOrigin,
	}
	bla.containerBuilder.AddChild(result.Fragment, offset)
	if !space.HasBlockFragmentation {
		return es, positioned, nil
	}
	bla.containerBuilder.PropagateBreakInfo(uf.Node, result, offset)
	return es, positioned, result.BreakToken
}

// tryLayoutNestedDocument checks if this element is an iframe/object with a
// document source. If so, fetches + lays out the nested document and returns
// the root fragment and its X offset within the iframe viewport. Returns nil if not applicable.
func (bla *BlockLayoutAlgorithm) tryLayoutNestedDocument(contentInlineSize float64, wdm WritingDirectionMode, geom FragmentGeometry) *nestedDocFragment {
	if bla.node.DOMNode == nil {
		return nil
	}
	htmlContent, baseDir, ok := openFrameOwnerURL(bla.ctx, bla.node.DOMNode)
	if !ok {
		return nil
	}

	// Compute the content-box physical size for the nested viewport.
	_, blockSize := ComputeReplacedSize(bla.ctx, bla.node, bla.style, bla.space)
	physSize := ToPhysicalSize(LogicalSize{
		InlineSize: contentInlineSize,
		BlockSize:  blockSize,
	}, wdm.WM)

	return layoutFrameOwnerDocument(bla.ctx, bla.node.DOMNode, htmlContent, baseDir, physSize.Width, physSize.Height)
}

// layoutElement lays node out in space with the layout algorithm for its
// display type and records the result on the node. Mirrors BlockNode::Layout
// (block_node.cc:405-666) → DetermineAlgorithmAndRun, then FinishLayout's
// StoreResultInLayoutBox (:837-875, :916-937 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A box without style or with
// display:none generates no fragment and stores nothing.
func layoutElement(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *LayoutResult {
	result := determineAlgorithmAndRun(ctx, node, space)
	if style := node.Style(); style != nil && style.GetDisplay() != css.DisplayNone {
		StoreResultInLayoutBox(ctx, node, result, space.BreakToken)
	}
	return result
}

// determineAlgorithmAndRun dispatches to the layout algorithm for the
// element's display type. Mirrors DetermineAlgorithmAndRun
// (block_node.cc:156-188 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func determineAlgorithmAndRun(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *LayoutResult {
	style := node.Style()
	if style == nil {
		return emptyResult(space.WritingDirection)
	}

	// Inline <svg> routes through SVGRootAlgorithm before the display
	// switch. The algorithm delegates outer sizing to BlockLayoutAlgorithm
	// (byte-identical to pre-Phase-1 behavior) and additionally builds the
	// SVG layout subtree (viewBox / preserveAspectRatio /
	// localToBorderBoxTransform / SVGNode children) into node.SVGRoot.
	// Mirrors Blink's LayoutSVGRoot : LayoutReplaced — the <svg> remains a
	// CSS replaced-element box to its HTML parent and an SVG root to its
	// children. See pkg/layout/svg_root_algorithm.go.
	if IsInlineSVG(node) {
		return NewSVGRootAlgorithm(ctx, node, space).Layout()
	}

	display := style.GetDisplay()

	// A fieldset container is laid out by FieldsetLayoutAlgorithm whatever
	// its display; its anonymous content box carries the display type and
	// multicol. Mirrors DetermineAlgorithmAndRun's box.IsFieldset() branch
	// (block_node.cc:177-178 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if node.IsFieldsetContainer() {
		return NewFieldsetLayoutAlgorithm(ctx, node, space).Layout()
	}

	switch display {
	case css.DisplayNone:
		return emptyResult(space.WritingDirection)
	case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayListItem,
		css.DisplayInlineBlock, css.DisplayInlineListItem:
		// DisplayInlineListItem (CSS Display L3 `display: inline list-item`) is
		// laid out by the block algorithm — it is an inline-level box that
		// internally is a list-item block-flow (the LayoutInlineListItem
		// analogue), shrink-to-fit sized and establishing its own FC, so the
		// UnpositionedListMarker carry/claim runs inside it just like a
		// block-level list item.
		if shouldBeMulticol(node, style) {
			return NewMulticolLayoutAlgorithm(ctx, node, space).Layout()
		}
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayTable, css.DisplayInlineTable:
		return NewTableLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayTableRow, css.DisplayTableRowGroup, css.DisplayTableHeaderGroup,
		css.DisplayTableFooterGroup, css.DisplayTableCell, css.DisplayTableCaption:
		// Table internals are laid out by their table's
		// TableLayoutAlgorithm: generateMissingTableParents wraps every
		// misparented one in an anonymous table at tree-build time. One
		// reaches here only as the layout root or as a floated,
		// absolutely positioned, flex or grid item — boxes Blink
		// blockifies (style_adjuster.cc:765-772, 1174-1178 @
		// d4ecdfed88f962439247c2ad36b8fe47805b1520) — so lay it out as a
		// block.
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayFlex, css.DisplayInlineFlex:
		return NewFlexLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayGrid, css.DisplayInlineGrid:
		return NewGridLayoutAlgorithm(ctx, node, space).Layout()
	default:
		// For now, treat unknown display types as block.
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	}
}

// reQueryFloatsAfterDrop re-queries the float offsets for a new-FC child that
// was just pushed below an intruding float (to dropBlockBfc, the box's new
// BFC-relative block-start), and returns those offsets plus the resulting
// available inline-size (margins and float offsets removed, clamped to >= 0).
//
// blockExtent is 0 for the pre-layout estimate (the box height isn't known yet)
// and the laid-out margin-box extent for the post-layout check.
//
// Blink's LayoutNewFormattingContext takes the child's inline offset from the
// chosen opportunity's own LineStartOffset, so a box pushed fully below the
// float lands in the full-width opportunity (offsets 0) rather than staying at
// the float-blocked inline position; a float that still overlaps the new block
// range keeps its (possibly smaller) offset.
// block_layout_algorithm.cc:2082-2108 @ d694f1edc784ebb2ce84dedde5ae3905d50c14f2.
func reQueryFloatsAfterDrop(exclusionSpace *ExclusionSpace, dropBlockBfc, blockExtent, bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize, childMarginInlineSum float64) (floatStartOff, floatEndOff, childInlineForSpace float64) {
	floatStartOff, floatEndOff = exclusionSpace.FindLocalFloatOffsets(dropBlockBfc, blockExtent, bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize)
	childInlineForSpace = contentInlineSize - childMarginInlineSum - floatStartOff - floatEndOff
	if childInlineForSpace < 0 {
		childInlineForSpace = 0
	}
	return floatStartOff, floatEndOff, childInlineForSpace
}

// newFCInlineOffsetBesideFloats returns the line-left inline offset (relative to
// the container content box) for a new-formatting-context child placed beside
// line-left floats. floatStartOff is the inline-start opportunity shift imposed
// by those floats (0 when none intrude on the line-left side).
//
// Mirrors Blink BlockLayoutAlgorithm::LayoutNewFormattingContext: when a float
// reduces the line-left opportunity, the child's inline margins are measured
// from the content box and cannot pull the BFC border box into the float's
// margin area:
//
//	line_left = max(opportunity.LineStartOffset(),
//	                origin.line_offset + line_left_margin.ClampNegativeToZero())
//
// With floatStartOff > 0 (content edge == 0) this is max(floatStartOff, margin):
// a negative inline margin is clamped (it cannot escape left across the float)
// and a positive margin only advances the box past the float once it exceeds
// the float clearance. With no line-left float (floatStartOff == 0) the raw
// margin is used, so a negative margin pulls the box left as usual (LOU-327).
// block_layout_algorithm.cc:2107-2124 @ d694f1edc784ebb2ce84dedde5ae3905d50c14f2.
func newFCInlineOffsetBesideFloats(marginInlineStart, floatStartOff float64) float64 {
	if floatStartOff <= 0 || marginInlineStart > floatStartOff {
		return marginInlineStart
	}
	return floatStartOff
}

// emptyResult returns a zero-sized layout result.
func emptyResult(wdm WritingDirectionMode) *LayoutResult {
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetSize(LogicalSize{})
	return builder.Build()
}

// createsFormattingContext returns true if the element establishes a new
// block formatting context (CSS 2.1 §9.4.1).
// The optional node parameter is used for the body element overflow
// propagation rule (CSS Overflow 3 §3.1).
func createsFormattingContext(style *css.Style, nodes ...*LayoutInputNode) bool {
	if style == nil {
		return false
	}

	// EstablishesNewFormattingContext handles flow-root (including variants
	// like `display: flow-root list-item` which GetDisplay() folds to
	// DisplayListItem, losing the flow-root bit), inline-block, table /
	// inline-table, table-cell, table-caption, flex/grid.
	if style.EstablishesNewFormattingContext() {
		return true
	}

	// A fieldset container always establishes a new formatting context
	// (LayoutFieldset::CreatesNewFormattingContext, forms/layout_fieldset.h:
	// 25-28 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if len(nodes) > 0 && nodes[0].IsFieldsetContainer() {
		return true
	}

	// Floats create a BFC.
	if style.GetFloat() != css.FloatNone {
		return true
	}

	// Absolutely/fixed positioned elements create a BFC.
	pos := style.GetPosition()
	if pos == css.PositionAbsolute || pos == css.PositionFixed {
		return true
	}

	// overflow != visible on either axis creates a BFC.
	// CSS Overflow 3 §3.1: The <body> element's overflow value is propagated
	// to the viewport (when <html> has overflow:visible), so the body element
	// itself does NOT establish a BFC from its overflow property.
	isBody := false
	if len(nodes) > 0 && nodes[0] != nil && nodes[0].DOMNode != nil {
		isBody = nodes[0].DOMNode.TagName == "body"
	}
	if !isBody && (style.GetOverflowX() != css.OverflowVisible || style.GetOverflowY() != css.OverflowVisible) {
		return true
	}

	// CSS Containment: layout and paint containment establish a BFC.
	if style.ShouldApplyLayoutContainment() || style.ShouldApplyPaintContainment() {
		return true
	}

	// align-content other than normal forces an independent formatting
	// context (CSS Align 3 §5.4), and a replaced element or form control
	// (a <button> block flow) always establishes one. Mirrors
	// LayoutBlockFlow::CreatesNewFormattingContext
	// (layout_block_flow_hot.cc:39-53 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if len(nodes) > 0 && nodes[0] != nil && html.IsReplacedOrFormControl(nodes[0].DOMNode) {
		return true
	}
	if v, ok := style.Properties["align-content"]; ok && strings.TrimSpace(v) != "normal" {
		return true
	}

	return false
}

// hasOnlySelfCollapsingChildren reports whether every child fragment of f
// is self-collapsing (vacuously true for none): margins collapse through f's
// content (CSS 2.1 §8.3.1).
func hasOnlySelfCollapsingChildren(f *PhysicalFragment) bool {
	for _, ch := range f.Children {
		if !ch.Fragment.IsSelfCollapsing {
			return false
		}
	}
	return true
}

// isColumnSpannerChild reports whether child is a column spanner laid out by
// the enclosing multicol: column-span:all on a valid spanner inside a column
// fragmentation context whose ancestry allows spanners. Mirrors Blink's
// IsColumnSpanAll() && IsInColumnBfc() && HasBlockFragmentation()
// (block_layout_algorithm.cc:1013-1014 @ a9f50e522efa).
func (bla *BlockLayoutAlgorithm) isColumnSpannerChild(child *LayoutInputNode) bool {
	return bla.space.HasBlockFragmentation &&
		bla.space.BlockFragmentationType == FragmentColumn &&
		isColumnSpanAll(child) &&
		!bla.space.ColumnSpannerDescendantsBlocked &&
		!shouldPreventColumnSpannerDescendants(bla.node)
}

// isColumnSpanAll reports whether n is a column-span:all box that may span
// columns on its own account. Mirrors the style half of Blink's
// LayoutObject::IsColumnSpanAll (layout_object.h:1752-1757 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with isSelfValidColumnSpanner
// standing in for IsValidColumnSpannerInTree.
func isColumnSpanAll(n *LayoutInputNode) bool {
	style := n.Style()
	return style != nil && style.GetColumnSpan() == "all" && isSelfValidColumnSpanner(style)
}

// isSelfValidColumnSpanner returns true when an element with column-span:all
// is a valid spanner candidate based on its own properties.
// Mirrors Blink's LayoutBox::IsSelfValidColumnSpanner (layout_box.cc:2968).
func isSelfValidColumnSpanner(style *css.Style) bool {
	if style == nil {
		return false
	}
	d := style.GetDisplay()
	// Inline and inline-level boxes cannot span columns. `inline list-item`
	// (DisplayInlineListItem) is inline-level too — it is laid out by the block
	// algorithm internally, but column-span:all is not valid on it.
	switch d {
	case css.DisplayInline, css.DisplayInlineBlock, css.DisplayInlineFlex,
		css.DisplayInlineGrid, css.DisplayInlineTable, css.DisplayInlineListItem:
		return false
	}
	// Floats cannot span columns.
	if style.GetFloat() != css.FloatNone {
		return false
	}
	// Out-of-flow positioned elements cannot span columns.
	pos := style.GetPosition()
	if pos == css.PositionAbsolute || pos == css.PositionFixed {
		return false
	}
	return true
}

// shouldPreventColumnSpannerDescendants returns true when a block node
// prevents its descendants from being column spanners.
// Mirrors Blink's LayoutBlockFlow::ShouldPreventColumnSpannerDescendants
// (layout_box.cc:3003). Called with the direct parent block of the candidate.
func shouldPreventColumnSpannerDescendants(node *LayoutInputNode) bool {
	if node == nil {
		return false
	}
	style := node.Style()
	if style == nil {
		return false
	}
	// Condition 1: the node is itself a column-span:all spanner — nested
	// spanners in the same multicol context are not allowed.
	if style.GetColumnSpan() == "all" {
		return true
	}
	// Condition 2: non-block-flow elements (table internals).
	// CSS table display types that are not the table box itself.
	if isTablePartDisplay(style.GetDisplay()) {
		return true
	}
	// Condition 4: elements creating a new block formatting context.
	if createsFormattingContext(style, node) {
		return true
	}
	// Condition 5: elements that can contain fixed-position objects
	// (transforms, will-change:transform, filter). Mirrors Blink's
	// CanContainFixedPositionObjects check in ShouldPreventColumnSpannerDescendants.
	if len(style.GetTransforms()) > 0 || len(style.GetFilter()) > 0 {
		return true
	}
	if v, ok := style.Get("will-change"); ok && v == "transform" {
		return true
	}
	return false
}

// needsShrinkToFit returns true if an element with auto inline-size should
// use shrink-to-fit sizing (CSS 2.1 §10.3.5). This applies to inline-block,
// floating, and absolutely positioned elements — NOT to regular block-level
// elements even if they establish a new formatting context.
// positionOrPropagateListMarker tries to claim a pending UnpositionedListMarker
// against a content child that has a baseline. Mirrors Blink's
// BlockLayoutAlgorithm::PositionOrPropagateListMarker
// (block_layout_algorithm.cc:3872-3924).
//
// contentBaseline is the content child's first baseline in the list item's
// CONTENT-box coordinates (childBlockOffset + childResult.Baseline);
// childBlockOffset is the child's block offset within the content box.
// Returns the amount the content child must be pushed down (>= 0, usually 0
// when the marker's font is no taller than the content's).
//
// If the child has no baseline the marker stays unpositioned for the next
// child (Blink: re-Set the marker on the builder and return).
func (bla *BlockLayoutAlgorithm) positionOrPropagateListMarker(
	child *PhysicalFragment, childResult *LayoutResult,
	childBlockOffset float64) float64 {
	marker := bla.containerBuilder.GetUnpositionedListMarker()
	if !marker.IsValid() {
		return 0
	}
	baseline, ok := marker.ContentAlignmentBaseline(child, childResult)
	if !ok {
		// No baseline on this child — keep the marker unpositioned and try the
		// next child (Blink leaves it set on the builder).
		return 0
	}
	markerResult := marker.Layout(bla.ctx, bla.space)
	if markerResult == nil {
		return 0
	}
	// baseline is the child fragment's first baseline RELATIVE TO THE CHILD
	// FRAGMENT (Blink's ContentAlignmentBaseline returns the line box ascent /
	// FirstBaseline, both fragment-relative). childBlockOffset is the child's
	// block offset within the list item's content box. AddToBox places the
	// marker at childBlockOffset + (baseline - markerAscent).
	contentPush := marker.AddToBox(bla.ctx, markerResult,
		baseline, childBlockOffset, bla.containerBuilder)
	if contentPush > 0 {
		// The marker's ascent is taller than the content's: Blink pushes the
		// content child down (*block_offset -= baseline_adjust). The content
		// child was already added to the builder, so re-offset it in place;
		// the marker was added at childBlockOffset (its block offset is correct
		// relative to the content's pre-push position, which is also where the
		// content's baseline now sits after the push).
		bla.containerBuilder.ShiftChildBlockOffset(child, contentPush)
	}
	bla.containerBuilder.ClearUnpositionedListMarker()
	return contentPush
}

// positionListMarkerWithoutLineBoxes places a still-pending marker when the
// list item produced no line boxes / no child with a baseline. The marker is
// top-aligned at block offset 0 and contributes to the intrinsic block size.
// Mirrors Blink's BlockLayoutAlgorithm::PositionListMarkerWithoutLineBoxes
// (block_layout_algorithm.cc:3926-3954). intrinsicBlockSize is updated in
// place — this MUST run before builder.SetIntrinsicBlockSize / SetSize so the
// marker's block-size contribution propagates.
func (bla *BlockLayoutAlgorithm) positionListMarkerWithoutLineBoxes(
	intrinsicBlockSize *float64) {
	marker := bla.containerBuilder.GetUnpositionedListMarker()
	if !marker.IsValid() {
		return
	}
	markerResult := marker.Layout(bla.ctx, bla.space)
	if markerResult == nil {
		return
	}
	marker.AddToBoxWithoutLineBoxes(bla.ctx, markerResult, bla.containerBuilder, intrinsicBlockSize)
	bla.containerBuilder.ClearUnpositionedListMarker()
}

// isAutoOrUnsetInline reports whether the style's inline-size property
// (`width` in horizontal writing modes, `height` in vertical) is `auto`,
// the empty string, or genuinely unset. Used by the size-containment
// path to distinguish "fill parent" (auto) from intrinsic-keyword or
// explicit-length cases, where the geometry pass already produced the
// correct value before this code re-evaluates it.
func isAutoOrUnsetInline(style *css.Style, wdm WritingDirectionMode) bool {
	if style == nil {
		return true
	}
	prop := "width"
	if wdm.IsVertical() {
		prop = "height"
	}
	v, ok := style.Get(prop)
	if !ok {
		return true
	}
	return v == "" || v == "auto"
}

func needsShrinkToFit(style *css.Style) bool {
	if style == nil {
		return false
	}
	d := style.GetDisplay()
	if d == css.DisplayInlineBlock || d == css.DisplayInlineFlex ||
		d == css.DisplayInlineGrid ||
		d == css.DisplayInlineTable || d == css.DisplayInlineListItem {
		return true
	}
	if style.GetFloat() != css.FloatNone {
		return true
	}
	pos := style.GetPosition()
	if pos == css.PositionAbsolute || pos == css.PositionFixed {
		return true
	}
	return false
}

// orthogonalFallbackSize returns the ICB size to use as the fallback
// inline-size for an orthogonal child, per CSS Writing Modes §10.3.2.
func orthogonalFallbackSize(childWDM WritingDirectionMode, ctx *LayoutContext) float64 {
	if childWDM.IsHorizontal() {
		return ctx.ViewportWidth
	}
	return ctx.ViewportHeight
}

// icbBlockSize returns the ICB (initial containing block) size in the
// parent's block direction. This is the upper bound for orthogonal child
// available inline-size.
func icbBlockSize(parentWDM WritingDirectionMode, ctx *LayoutContext) float64 {
	if parentWDM.IsHorizontal() {
		// HTB parent: block direction is vertical → ICB block = viewport height
		return ctx.ViewportHeight
	}
	// Vertical parent: block direction is horizontal → ICB block = viewport width
	return ctx.ViewportWidth
}

// computeOrthogonalAvailableBlock computes the available block-size that
// should be used for orthogonal children per CSS Writing Modes §10.3.2.
//
// The algorithm considers:
//  1. If parent has definite height: use it, raised by min-height
//  2. If parent is a scroller with max-height: use max-height, raised by min-height
//  3. If parent has min-height (no overflow, no definite height): use min-height
//  4. Fall back to nearest ancestor scroller (via OrthogonalFallbackBlockSize) or ICB
//
// The result is capped at the ICB size.
func computeOrthogonalAvailableBlock(
	style *css.Style,
	wdm WritingDirectionMode,
	space ConstraintSpace,
	geom FragmentGeometry,
	ctx *LayoutContext,
	childAvailableBlock float64,
	hasExplicitBlock bool,
	explicitBlockSize float64,
) float64 {
	icb := icbBlockSize(wdm, ctx)
	minBlock := ResolveMinBlockSize(style, wdm, space, geom).Float64()
	maxBlockLU, hasMax := ResolveMaxBlockSize(style, wdm, space, geom)
	maxBlock := maxBlockLU.Float64()
	isScroller := style.GetOverflowX() != css.OverflowVisible || style.GetOverflowY() != css.OverflowVisible

	if hasExplicitBlock {
		// Parent has definite height.
		// Apply min/max clamping per CSS 2.1 §10.4:
		// used = max(min-height, min(height, max-height))
		result := explicitBlockSize
		if hasMax && maxBlock < result {
			result = maxBlock
		}
		if minBlock > result {
			result = minBlock
		}
		return result
	}

	if isScroller {
		// Parent is a scroller (overflow != visible) without definite height.
		// Use max-height as the base constraint if available.
		var result float64
		if hasMax {
			result = maxBlock
		} else {
			result = icb
		}
		// Raise by min-height.
		if minBlock > result {
			result = minBlock
		}
		// Cap at ICB: the orthogonal child can't see more than the viewport.
		if result > icb {
			result = icb
		}
		return result
	}

	// Parent is not a scroller and has no definite height.
	// Per §7.3: use the tightest size constraint (max-height if present,
	// otherwise min-height), capped at ICB.
	if hasMax {
		result := maxBlock
		if minBlock > result {
			result = minBlock
		}
		if result > icb {
			result = icb
		}
		return result
	}
	if minBlock > 0 {
		result := minBlock
		if result > icb {
			result = icb
		}
		return result
	}

	// Fall back to ancestor scroller's constraint or ICB.
	if space.OrthogonalFallbackBlockSize > 0 {
		result := space.OrthogonalFallbackBlockSize
		if result > icb {
			result = icb
		}
		return result
	}

	return icb
}

// computeOrthogonalFallbackBlockForChildren computes the
// OrthogonalFallbackBlockSize value to propagate to children's constraint
// spaces. Per CSS Writing Modes §10.3.2, the nearest ancestor scroller
// (overflow != visible) with a constrained block-size overrides the ICB
// fallback for orthogonal descendants.
func computeOrthogonalFallbackBlockForChildren(
	style *css.Style,
	wdm WritingDirectionMode,
	space ConstraintSpace,
	geom FragmentGeometry,
	ctx *LayoutContext,
	hasExplicitBlock bool,
	explicitBlockSize float64,
) float64 {
	isScroller := style.GetOverflowX() != css.OverflowVisible || style.GetOverflowY() != css.OverflowVisible

	if isScroller {
		// This element is a scroller. Compute its effective block-size
		// constraint and propagate it to descendants.
		minBlock := ResolveMinBlockSize(style, wdm, space, geom).Float64()
		maxBlockLU, hasMax := ResolveMaxBlockSize(style, wdm, space, geom)
		maxBlock := maxBlockLU.Float64()
		icb := icbBlockSize(wdm, ctx)

		var result float64
		if hasExplicitBlock {
			result = explicitBlockSize
		} else if hasMax {
			result = maxBlock
		} else {
			result = icb
		}
		// Apply min-height.
		if minBlock > result {
			result = minBlock
		}
		// Cap at ICB.
		if result > icb {
			result = icb
		}
		return result
	}

	// Not a scroller: inherit the ancestor's fallback unchanged.
	return space.OrthogonalFallbackBlockSize
}

// setupSpaceBuilderForFragmentation copies this block's fragmentation context
// into a child's constraint-space builder at the given fragmentainer offset,
// with the capacity left to children (SetupSpaceBuilderForFragmentation,
// fragmentation_utils.cc:379-397 @ a9f50e522efa).
func (bla *BlockLayoutAlgorithm) setupSpaceBuilderForFragmentation(b *ConstraintSpaceBuilder, fragmentainerOffset float64) {
	SetupSpaceBuilderForFragmentationFromBuilder(bla.containerBuilder, fragmentainerOffset, bla.fragmentainerCapacityForChildren(), b)
}

// fragmentainerCapacityForChildren is the fragmentainer block-size available
// to this block's children: cloned box decorations shrink it, and it is never
// less than 1px; Indefinite when unknown. Mirrors Blink's
// LayoutAlgorithm::FragmentainerCapacityForChildren() (layout_algorithm.h:
// 160-162 @ a9f50e522efa).
func (bla *BlockLayoutAlgorithm) fragmentainerCapacityForChildren() float64 {
	return FragmentainerCapacity(bla.containerBuilder, true)
}
