package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
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
}

// NewBlockLayoutAlgorithm creates a block layout algorithm for the given
// layout node with the given constraint space.
func NewBlockLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *BlockLayoutAlgorithm {
	return &BlockLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: node.Style(),
		space: space,
	}
}

// childPercResolutionBlockSize returns the percentage-resolution block size
// to pass to children. When this block is an anonymous wrap (e.g. block-in-
// inline split) with auto block size, the parent's resolution base passes
// through so percent CB-block resolution (CSS 2.1 §9.4.3 relative-offset,
// height:% etc.) reaches the real containing block. Mirrors Blink's
// PercentageResolutionBlockSize propagation for anonymous auto-height blocks.
// Mirrors Blink's ConstraintSpace::PercentageResolutionBlockSize() separation
// from AvailableSize: when an anonymous block's height was imposed by a parent
// algorithm (IsBlockSizeOverride, e.g. multicol column fragmentainer), the
// dedicated PercentageResolutionSize carries the containing block's CSS height,
// NOT the fragmentainer height.
func childPercResolutionBlockSize(bla *BlockLayoutAlgorithm, hasExplicitBlock bool, explicitBlockSize float64) float64 {
	// For anonymous blocks whose height comes from IsBlockSizeOverride (the
	// multicol column fragmentainer case), the override height is the column
	// height — not the CSS containing-block height for percentage resolution.
	// Use the dedicated PercentageResolutionSize.BlockSize which carries the
	// multicol container's explicit height.
	// We do NOT apply this to named (non-anonymous) elements: flex items and
	// other real elements that get IsBlockSizeOverride already have their CSS
	// height correctly expressed as the override, so explicitBlockSize is right.
	if bla.space.IsBlockSizeOverride && bla.node != nil && bla.node.isAnonymous {
		pctRes := bla.space.PercentageResolutionSize.BlockSize.Float64()
		if pctRes >= 0 {
			return pctRes
		}
		return Indefinite
	}
	if hasExplicitBlock {
		return explicitBlockSize
	}
	if bla.node != nil && bla.node.isAnonymous {
		if bla.space.PercentageResolutionSize.BlockSize.Float64() > 0 {
			return bla.space.PercentageResolutionSize.BlockSize.Float64()
		}
	}
	return explicitBlockSize // 0 if auto and not an anonymous passthrough
}

// clampToRemainingBlockBudget clamps value to the declared block-size budget
// (explicitBlockSize) still unconsumed after the prior fragments accounted for
// in consumed. A definite-height block fragmented across column-span splits
// distributes its CSS height across the inter-spanner segments, so each segment
// may hold no more than the budget that remains. When a positive budget is
// exhausted or over-consumed (remaining <= 0) the result is 0 — a resumed
// fragment must not regain full size after the budget is spent; when value
// already fits, value is returned unchanged. A non-positive declared budget
// (explicitBlockSize <= 0, e.g. a height:0 box whose content overflows) is not
// a height to distribute at all, so value is returned untouched — the resumed
// fragment shows its overflowing content (spanner-in-child-after-parallel-flow-004).
func clampToRemainingBlockBudget(value, explicitBlockSize, consumed float64) float64 {
	// A non-positive declared budget is not a height to distribute: the box was
	// declared height:0 (or smaller, after border/padding clamping) and any
	// content shows as overflow, so the resumed fragment keeps its intrinsic
	// value untouched. Only a genuine positive budget gets distributed/exhausted.
	if explicitBlockSize <= 0 {
		return value
	}
	remaining := explicitBlockSize - consumed
	if remaining <= 0 {
		// Budget exhausted or over-consumed: a resumed fragment must not regain
		// full size after a positive budget has been spent.
		return 0
	}
	if remaining < value {
		return remaining
	}
	return value
}

// Layout performs block layout and returns the result.
func (bla *BlockLayoutAlgorithm) Layout() *LayoutResult {
	wdm := bla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(bla.ctx, bla.node, bla.style, wdm, bla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(bla.node)

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
			builder.SetUnpositionedListMarker(&UnpositionedListMarker{
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
		builder.SetIsMonolithic(true)
	}
	// Phase 16.e+18 v2 B2.6 (SetupFragmentation contribution): during the
	// initial column-balancing pass, propagate the container's own border-
	// block-start and border-block-end as unbreakable floors. Mirrors Blink's
	// SetupFragmentation hook (fragmentation_utils.cc:510-514):
	//
	//   if (space.IsInitialColumnBalancingPass()) {
	//     const BoxStrut& unbreakable = builder->BorderScrollbarPadding();
	//     builder->PropagateTallestUnbreakableBlockSize(unbreakable.block_start);
	//     builder->PropagateTallestUnbreakableBlockSize(unbreakable.block_end);
	//   }
	//
	// The column box must be at least as tall as either edge; the max()
	// semantics inside PropagateTallestUnbreakableBlockSize keeps the
	// tallest of all propagated values.
	if bla.space.IsInitialColumnBalancingPass {
		blockStart := geom.Border.BlockStart + geom.Padding.BlockStart
		blockEnd := geom.Border.BlockEnd + geom.Padding.BlockEnd
		builder.PropagateTallestUnbreakableBlockSize(blockStart)
		builder.PropagateTallestUnbreakableBlockSize(blockEnd)
	}

	contentInlineSize := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if contentInlineSize < 0 {
		contentInlineSize = 0
	}

	// Block-size: use geom if definite, else auto.
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
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
				// The fill is left untouched ONLY when it collapsed to 0 because the
				// containing block's inline size is indefinite (a max-content/min-content
				// measurement pass) and no block constraint transfers: forcing the intrinsic
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
					if contentInlineSize > 0 || blockFixed || blockConstraintTransfers {
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

	// Build child constraint space.
	childAvailableInline := contentInlineSize
	childAvailableBlock := Indefinite
	if hasExplicitBlock {
		childAvailableBlock = explicitBlockSize
	}

	// §10.3.2: Compute the available block-size for orthogonal children.
	// This accounts for the parent's height/min-height/max-height constraints
	// and walks up to the nearest ancestor scroller or ICB when needed.
	orthogonalAvailableBlock := computeOrthogonalAvailableBlock(
		bla.style, wdm, bla.space, geom, bla.ctx,
		childAvailableBlock, hasExplicitBlock, explicitBlockSize)

	// Float exclusion tracking.
	// Inherit exclusion space from parent, or start fresh for new BFCs.
	exclusionSpace := bla.space.ExclusionSpace
	if bla.space.IsNewFormattingContext || exclusionSpace == nil {
		exclusionSpace = &ExclusionSpace{}
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

	// Fragmentation state: track incoming break token for resume.
	//
	// LOU-365 (CodeRabbit 3514362231): ALL child break tokens are consumed,
	// not just the first — parallel flows can put several continuations on
	// one token. Mirrors Blink's BlockChildIterator: resumption walks every
	// child break token in order, then proceeds to the sibling after the
	// last tokened child only when !HasSeenAllChildren
	// (block_child_iterator.cc:80-95 @ a9f50e522efa). resumeChildTokens maps
	// each tokened child to its continuation; untokened children at or
	// before the last tokened index completed in earlier fragmentainers and
	// are skipped.
	incomingBreakToken := bla.space.BreakToken
	resumeChildIdx := -1 // index in Children() to resume from (-1 = start from beginning)
	resumeLastTokenIdx := -1
	var resumeChildTokens map[*LayoutInputNode]*BlockBreakToken
	if incomingBreakToken != nil {
		blockCursor = 0 // We start at 0 in the new fragmentainer; consumed is tracked by the token.
		if len(incomingBreakToken.ChildBreakTokens) > 0 {
			resumeChildTokens = make(map[*LayoutInputNode]*BlockBreakToken, len(incomingBreakToken.ChildBreakTokens))
			for _, ct := range incomingBreakToken.ChildBreakTokens {
				resumeChildTokens[ct.Node] = ct
			}
		}
	}

	// Pending outgoing-break state, populated by the children fragmentation
	// loop when overflow / IFC zero-progress / forced-break is detected and
	// consumed by the post-loop FinishFragmentation analogue (option-b
	// step6_option_b_plan.md §3.2). Mirrors Blink's container_builder_
	// break fields (HasInflowChildBreakInside, ShouldBreakInside,
	// ChildBreakTokens, AtBlockEnd, MinSpaceShortage, BreakAppeal,
	// HasForcedBreak). Unused until sub-steps 6.2 / 6.3 / 6.4 wire them in.
	var (
		pendingHasInflowChildBreakInside bool
		pendingShouldBreakInside         bool
		pendingIsAtBlockEnd              bool
		pendingChildBreakTokens          []*BlockBreakToken
		pendingHasSeenAllChildren        bool
		pendingMinSpaceShortage          float64
		pendingBreakAppeal               BreakAppeal
		pendingHasForcedBreak            bool
		pendingDropAtBlockOffset         float64
		pendingDropAtBlockOffsetEnabled  bool
		pendingIntrinsicAtBreak          float64
		pendingHaveIntrinsicAtBreak      bool
		// pendingParallelFlowChildTokens collects break tokens of children
		// that finished their own box (at-block-end / parallel flow) while
		// their CONTENT continues in later fragmentainers, when the walk
		// itself never hit a fragmentainer boundary. Promoted into the
		// unified outgoing-token reader after Gates A/B so the parent's
		// natural-path margin handling is preserved (the parent's own flow
		// IS complete). LOU-365; mirrors Blink FinishFragmentation building
		// the outgoing token from all child break tokens
		// (fragmentation_utils.cc:677-815 @ a9f50e522efa).
		pendingParallelFlowChildTokens []*BlockBreakToken
		// inlineFullyPlaced is set to true when this block has only inline
		// children AND the IFC completed without overflow (no inline break
		// token emitted). Used when the block self-fragments via didBreakSelf
		// to set HasSeenAllChildren=true on the outgoing break token, so the
		// continuation fragment's BLA does not re-lay out the same inline
		// content. Mirrors Blink's `InlineBreakToken::is_finished_` carried
		// on the block break token via the inline child path.
		inlineFullyPlaced bool
	)
	pendingDropAtBlockOffset = -1
	pendingBreakAppeal = BreakAppealPerfect
	_ = pendingHasInflowChildBreakInside
	_ = pendingShouldBreakInside
	_ = pendingIsAtBlockEnd
	_ = pendingChildBreakTokens
	_ = pendingHasSeenAllChildren
	_ = pendingMinSpaceShortage
	_ = pendingBreakAppeal
	_ = pendingHasForcedBreak
	_ = pendingDropAtBlockOffset
	_ = pendingDropAtBlockOffsetEnabled
	_ = pendingIntrinsicAtBreak
	_ = pendingHaveIntrinsicAtBreak

	// CSS 2.1 §8.3.1: Parent-child top margin collapsing.
	// When a block has no block-start border/padding and isn't a new BFC,
	// the first child's margin propagates upward.
	canPropagateTop := !bla.space.IsNewFormattingContext &&
		geom.Border.BlockStart == 0 && geom.Padding.BlockStart == 0
	firstNonEmptyChild := true
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
	isInlineSVGRoot := bla.node.DOMNode != nil && bla.node.DOMNode.TagName == "svg" && IsInlineSVG(bla.node)

	// Iframe/object with a document source: lay out the nested document
	// instead of this element's DOM children.
	if isInlineSVGRoot {
		// No children added — see doc comment above.
	} else if nested := bla.tryLayoutNestedDocument(contentInlineSize, wdm, geom); nested != nil {
		// For vertical-rl/sideways-rl nested roots, the root is anchored to the
		// right edge of the iframe viewport. Apply the X offset as an inline offset.
		offset := LogicalOffset{InlineOffset: nested.rootOffsetX}
		builder.AddChild(nested.fragment, offset)
	} else if hasOnlyInlineChildren(bla.node) {
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
		prevES := exclusionSpace
		var inlineAscent, lastBaselineOff float64
		var inlineBreakToken *BlockBreakToken
		if incomingBreakToken != nil && incomingBreakToken.HasSeenAllChildren {
			// Skip IFC; everything was placed in a prior fragment.
		} else {
			blockCursor, exclusionSpace, inlineAscent, lastBaselineOff, inlineBreakToken = bla.layoutInlineChildren(wdm, contentInlineSize, exclusionSpace, builder, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize, inlineStartIdx, inlineStartTextOffset)
		}
		// IFC completed without overflow: track so we can flag the outgoing
		// break token's HasSeenAllChildren if the block self-fragments. See
		// the inlineFullyPlaced declaration above.
		if inlineBreakToken == nil {
			inlineFullyPlaced = true
		}
		if exclusionSpace != prevES && bla.space.IsNewFormattingContext {
			hasOwnFloats = true
		}
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

		// CSS Lists §3 / Blink PositionOrPropagateListMarker: a list item with
		// inline content places its OUTSIDE marker against the first line box's
		// baseline. The first line box sits at content-relative block offset 0
		// and its baseline is firstLineAscent. The marker fragment is added as
		// a content-relative child of the list-item builder, with a negative
		// inline offset (InlineMarginsForOutside) placing it left of the
		// content-box start.
		if firstLineAscent > 0 {
			marker := builder.GetUnpositionedListMarker()
			if marker.IsValid() {
				if markerResult := marker.Layout(bla.ctx, bla.space); markerResult != nil {
					// AddToBox returns the amount the content must be pushed DOWN
					// when the marker's ascent is TALLER than the first line's
					// (Blink's content-adjust: an image marker, e.g.
					// list-style-image / ::marker content:url(), is taller than a
					// text line). The block-CONTENT path applies this via
					// positionOrPropagateListMarker; the INLINE-content path must
					// do the equivalent — there is no single content child, so
					// shift every line box down, grow the content block size, and
					// carry the push into the propagated first/last baselines so
					// the list item's own baseline stays aligned with the marker.
					// LOU-337 / css-pseudo marker-content-012: without this the
					// text baseline sat ~4px too high from the outside-image row
					// down. Mirrors BlockLayoutAlgorithm::
					// PositionOrPropagateListMarker (block_layout_algorithm.cc /
					// unpositioned_list_marker.cc:81-123 @ 4883d11f).
					contentPush := marker.AddToBox(bla.ctx, markerResult, firstLineAscent, 0, builder)
					builder.ClearUnpositionedListMarker()
					if contentPush > 0 {
						builder.ShiftLineBoxChildrenBlockOffset(contentPush)
						blockCursor += contentPush
						firstLineAscent += contentPush
						// If the IFC fragmented (an inline-only list item overflowing
						// a fragmentainer), the break token captured its consumed
						// height / shortage BEFORE this push. Advance both so the
						// continuation resumes from the post-push height; otherwise a
						// fragmented item with a tall outside marker resumes too high
						// and misplaces later fragments. The early-return below reads
						// InlineShortage when >0, else ConsumedBlockSize, so both must
						// move with the content.
						if inlineBreakToken != nil {
							inlineBreakToken.ConsumedBlockSize = inlineBreakToken.ConsumedBlockSize.Add(
								layoutunit.FromFloat64Round(contentPush))
							if inlineBreakToken.InlineShortage > 0 {
								inlineBreakToken.InlineShortage += contentPush
							}
						}
						if hasLastChildBaseline {
							lastChildBlockOffset += contentPush
						}
					}
				}
			}
		}

		// Inline fragmentation: if the inline layout stopped mid-content due to
		// column overflow, build a partial fragment and return early with the
		// inline break token. Mirrors the block-children fragmentation path.
		if inlineBreakToken != nil {
			// Use the per-break-point inline shortage when set (Blink-aligned),
			// otherwise fall back to per-fragment consumed (not cumulative).
			shortage := inlineBreakToken.InlineShortage
			if shortage <= 0 {
				consumedInFragment := inlineBreakToken.ConsumedBlockSize.Float64()
				if incomingBreakToken != nil {
					consumedInFragment -= incomingBreakToken.ConsumedBlockSize.Float64()
				}
				shortage = consumedInFragment - (bla.space.FragmentainerBlockSize - bla.space.FragmentainerOffset)
				if shortage < 0 {
					shortage = 0
				}
			}
			if hasExplicitBlock {
				builder.SetSize(geom.BorderBoxSize)
			} else {
				borderBoxBlock := blockCursor + geom.BlockBorderPadding()
				builder.SetSize(LogicalSize{
					InlineSize: geom.BorderBoxSize.InlineSize,
					BlockSize:  borderBoxBlock,
				})
			}
			builder.SetIntrinsicBlockSize(blockCursor)
			builder.SetNode(bla.node.DOMNode)
			builder.SetStyle(bla.style)
			builder.SetLayoutNode(bla.node)
			builder.SetBoxData(&PhysicalBoxData{
				Border:    ToPhysicalEdges(geom.Border, wdm),
				Padding:   ToPhysicalEdges(geom.Padding, wdm),
				Scrollbar: ToPhysicalEdges(geom.Scrollbar, wdm),
			})
			result := builder.Build()
			result.BreakToken = inlineBreakToken
			result.MinSpaceShortage = shortage
			result.PropagatedOOFCandidates = builder.outOfFlowCandidates
			return result
		}
	} else {
		// Block formatting context: block-level children.
		children := bla.node.Children()

		// When resuming from break tokens, find the first and last tokened
		// children (BlockChildIterator semantics,
		// block_child_iterator.cc:80-95 @ a9f50e522efa).
		if incomingBreakToken != nil && len(resumeChildTokens) > 0 {
			resumeChildIdx, resumeLastTokenIdx = resumeTokenIndexRange(children, resumeChildTokens)
		} else if incomingBreakToken != nil && incomingBreakToken.HasSeenAllChildren {
			// All children were seen in a previous fragment; nothing to lay out.
			resumeChildIdx = len(children)
		}

		for childIdx, child := range children {
			// When resuming, skip children completed in previous fragments
			// (and, between/before tokened children, those without their own
			// continuation token — see resumeSkipsChild).
			if resumeSkipsChild(childIdx, child, resumeChildIdx, resumeLastTokenIdx,
				resumeChildTokens, incomingBreakToken) {
				continue
			}

			// Skip text nodes (handled by inline layout in anonymous blocks).
			if child.IsText() {
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
				// Phase 25 (C38): when the CB is itself fragmented (this BLA is a continuation),
				// `blockCursor` is fragment-LOCAL (resets to 0 at the start of each fragment).
				// For the deferred OOF-fragmentation drain at the outer multicol — which slices
				// the OOF across CB fragments using stitched-CB-relative coords — we need the
				// static position in CB-STITCHED coords. Add the incoming break token's
				// ConsumedBlockSize so the captured offset cumulates across CB fragments.
				// Non-resumed passes (`incomingBreakToken == nil`) contribute 0 — no behavioral
				// change. Mirrors Blink's static-position capture which uses
				// stitched coords throughout (block_layout_algorithm.cc).
				staticBlockOffset := blockCursor + prevMarginStrut.Resolve()
				if incomingBreakToken != nil {
					staticBlockOffset += incomingBreakToken.ConsumedBlockSize.Float64()
				}

				// Static inline offset: block-level abspos gets inline-start (0).
				// Inline-level abspos (display:inline/inline-*) in a block FC would
				// have established an anonymous inline line box — its hypothetical
				// inline cursor is where that line box would begin, which is past
				// any inline-start float exclusions active at this block position.
				// Mirrors Blink's InlineLayoutAlgorithm sub-pass which queries the
				// exclusion space for the line's inline-start when placing the
				// hypothetical inline-level OOF.
				staticInlineOffset := 0.0
				if isInlineLevelDisplay(childStyle.GetDisplay()) && exclusionSpace != nil {
					// FindAvailableInlineSize returns inline-start offset in the
					// same coordinate system the enclosing block uses when
					// placing in-flow inline content (mirrors inline_layout.go
					// line-start recomputation after placing floats).
					bfcBlock := bfcBlockOrigin + staticBlockOffset
					floatStartOff, _ := exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, bfcContainerInlineSize)
					if floatStartOff > 0 {
						staticInlineOffset = floatStartOff
					}
				}

				builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
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

			// Handle floats.
			if childStyle.GetFloat() != css.FloatNone {
				bla.layoutFloat(child, childStyle, wdm, contentInlineSize, childAvailableBlock,
					blockCursor, &prevMarginStrut, exclusionSpace, builder, &exclusionSpace, bfcBlockOrigin)
				// Only BFC roots extend auto block-size to clear floats
				// (CSS 2.1 §10.6.7). Non-BFC parents let floats overflow;
				// hasOwnFloats is already true for BFC roots (initialized
				// from IsNewFormattingContext).

				continue
			}

			// Spanner detection: inside a column fragmentation context, a child
			// with column-span:all must be laid out by the multicol algorithm at
			// full container width, not as column content. Return early so multicol
			// can extract + lay out the spanner, then resume from after it.
			// Mirrors Blink's BlockLayoutAlgorithm detecting child.IsColumnSpanAll()
			// which calls IsValidColumnSpannerInTree() → IsSelfValidColumnSpanner()
			// + DoesAncestryAllowColumnSpanner() (layout_box.cc:2956-3030).
			if bla.space.HasBlockFragmentation &&
				bla.space.BlockFragmentationType == FragmentColumn &&
				childStyle.GetColumnSpan() == "all" &&
				isSelfValidColumnSpanner(childStyle) &&
				!bla.space.ColumnSpannerDescendantsBlocked &&
				!shouldPreventColumnSpannerDescendants(bla.node) {

				// LOU-320: pre-spanner content extent for THIS segment, clamped to
				// the remaining declared budget when this is a resumed fragment of a
				// definite-height block distributing its height across spanner splits.
				// A middle segment (e.g. 004b's block2: 200px content, but only
				// 350-200=150px of the container budget remains) must occupy only its
				// remaining budget so the trailing segments get their correct share and
				// consumed accumulates the clamped amount, not the raw content height.
				//
				// Scoped to a genuine CSS height (!IsBlockSizeOverride): when this
				// block is the multicol's anonymous column-content holder, the
				// explicit block-size is the column-height the multicol imposes via
				// IsBlockSizeOverride (createConstraintSpaceForColumn), not a CSS
				// height to distribute across splits — multicol handles per-row
				// distribution, so this clamp must not fire (TestColumnHeight_PhaseB).
				preSpannerExtent := blockCursor
				if hasExplicitBlock && !bla.space.IsBlockSizeOverride &&
					bla.space.BlockFragmentationType == FragmentColumn {
					// Clamp the FIRST inter-spanner segment too (not only resumed
					// ones): a first block exceeding the definite height must record
					// a clamped ConsumedBlockSize, else later fragments resume from a
					// negative remaining budget. consumed is 0 on the first segment.
					consumed := 0.0
					if incomingBreakToken != nil {
						consumed = incomingBreakToken.ConsumedBlockSize.Float64()
					}
					preSpannerExtent = clampToRemainingBlockBudget(
						preSpannerExtent, explicitBlockSize, consumed)
				}

				// Break token resumes AFTER the spanner on the next LayoutLine call.
				var spannerBreakToken *BlockBreakToken
				if childIdx+1 < len(children) {
					spannerBreakToken = &BlockBreakToken{
						Node:              bla.node,
						ConsumedBlockSize: layoutunit.FromFloat64Round(preSpannerExtent),
						ChildBreakTokens: []*BlockBreakToken{{
							Node:          children[childIdx+1],
							IsBreakBefore: true,
						}},
					}
					if incomingBreakToken != nil {
						spannerBreakToken.ConsumedBlockSize = spannerBreakToken.ConsumedBlockSize.Add(incomingBreakToken.ConsumedBlockSize)
						spannerBreakToken.SequenceNumber = incomingBreakToken.SequenceNumber + 1
					}
				}
				// Build the partial fragment for content laid out before the spanner.
				intrinsicBlock := preSpannerExtent
				builder.SetIntrinsicBlockSize(intrinsicBlock)
				builder.SetNode(bla.node.DOMNode)
				builder.SetStyle(bla.style)
				builder.SetLayoutNode(bla.node)
				// LOU-320: size the pre-spanner partial fragment to the content
				// actually placed before the spanner, NOT the full CSS height. A
				// definite-height block split by a column-span distributes its
				// height across the inter-spanner fragments; using geom.BorderBoxSize
				// here painted the full declared height (e.g. a 450px container's
				// box) into every column that the pre-spanner content spanned,
				// overflowing the column row.
				//
				// Scoped to a genuine CSS height (!IsBlockSizeOverride). When the
				// block IS the multicol's anonymous column-content holder, its
				// "explicit" block-size is the column-height imposed by multicol via
				// IsBlockSizeOverride+IsFixedBlockSize (createConstraintSpaceForColumn,
				// multicol_layout.go:2200-2207). That override block-size is the
				// column ROW height, not a CSS height to distribute across spanner
				// splits — the pre-spanner fragment must report the full row extent
				// (geom.BorderBoxSize) so multicol snaps the spanner past the whole
				// row (column-height/column-wrap path, TestColumnHeight_PhaseB). The
				// 004a/005 container, by contrast, carries a real CSS height:450px
				// laid out as an ordinary child (IsBlockSizeOverride=false), so it
				// takes the intrinsic-content branch and distributes its height.
				if hasExplicitBlock && bla.space.IsBlockSizeOverride {
					builder.SetSize(geom.BorderBoxSize)
				} else {
					builder.SetSize(LogicalSize{
						InlineSize: geom.BorderBoxSize.InlineSize,
						BlockSize:  intrinsicBlock + geom.BlockBorderPadding(),
					})
				}
				builder.SetBoxData(&PhysicalBoxData{
					Border:    ToPhysicalEdges(geom.Border, wdm),
					Padding:   ToPhysicalEdges(geom.Padding, wdm),
					Scrollbar: ToPhysicalEdges(geom.Scrollbar, wdm),
				})
				builder.SetEndMarginStrut(prevMarginStrut)
				builder.SetExclusionSpace(exclusionSpace)
				result := builder.Build()
				result.BreakToken = spannerBreakToken
				result.ColumnSpannerPath = &ColumnSpannerPath{Box: child}
				result.PropagatedTopMargin = propagatedTopMargin
				if intrinsicBlock > NewLogicalFragment(wdm, result.Fragment).BlockSize() {
					result.BlockSizeForFragmentation = intrinsicBlock
				}
				return result
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

			// Handle clear property.
			// CSS 2.1 §8.3.1: The element is placed so its border edge is at or below
			// the bottom outer edge of all relevant floats. The clearance amount is
			// max(0, floatEnd - (blockCursor + collapsedMargin)). If the collapsed
			// margin already places the element past the float, clearance = 0, but
			// the clear property still inhibits parent-child margin collapsing.
			hasClearance := false
			clearType := childStyle.GetClear()
			if clearType != css.ClearNone && !exclusionSpace.IsEmpty() {
				bfcCursor := bfcBlockOrigin + blockCursor
				clearedBlockBfc := exclusionSpace.ClearanceOffset(clearType, layoutunit.FromFloat64Round(bfcCursor), wdm).Float64()
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
						blockCursor = clearedBlock - childMargins.BlockStart
						prevMarginStrut = MarginStrut{} // consumed by clearance
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

			// Compute available inline for this child, accounting for floats.
			// floatCheckBlock is BFC-relative so exclusion space queries are correct.
			isOrthogonal := wdm.IsOrthogonalTo(childWDM)
			floatCheckBlock := bfcBlockOrigin + blockCursor
			if isChildNewFC {
				tentativeStrut := prevMarginStrut
				tentativeStrut.Append(childMargins.BlockStart)
				floatCheckBlock = bfcBlockOrigin + blockCursor + tentativeStrut.Resolve()
			}
			floatStartOff, floatEndOff := exclusionSpace.FindAvailableInlineSize(floatCheckBlock, 0, childAvailableInline)
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
					availBesideFloats := childAvailableInline - floatStartOff - floatEndOff
					if neededInline > availBesideFloats {
						// Doesn't fit — find the earliest block position
						// where the BFC fits alongside remaining floats.
						// Use FindFloatPosition which iterates through float
						// boundaries (CSS 2.1 §9.5 Rule 5).
						newBlockBfc := exclusionSpace.FindFloatPosition(
							css.FloatLeft, neededInline, 0,
							childAvailableInline, floatCheckBlock)
						newBlock := newBlockBfc - bfcBlockOrigin
						if newBlock > blockCursor {
							blockCursor = newBlock
							prevMarginStrut = MarginStrut{}
							hasClearance = true
							floatStartOff, floatEndOff, childInlineForSpace = reQueryFloatsAfterDrop(
								exclusionSpace, bfcBlockOrigin+blockCursor, 0,
								childAvailableInline, childMargins.InlineSum())
						}
					}
				}
			}
			blockForChild := childAvailableBlock
			if isOrthogonal {
				blockForChild = orthogonalAvailableBlock
			}
			csBuilder := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
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
				SetExclusionSpace(exclusionSpace)

			// For non-BFC children, propagate the BFC block and inline offsets
			// so they can correctly query the exclusion space in their own layout.
			if !isChildNewFC {
				// The child's tentative BFC offset = parent's BFC origin +
				// child's local position (blockCursor + pending margin).
				tentativeChildBfcOff := bfcBlockOrigin + blockCursor + prevMarginStrut.Resolve() + childMargins.BlockStart
				csBuilder.SetBfcBlockOffset(layoutunit.FromFloat64Round(tentativeChildBfcOff))
				// The child's BFC inline offset = parent's BFC inline origin +
				// child's inline position. Per CSS 2.1 §9.5, non-BFC blocks
				// position as if floats don't exist, so use margin only.
				csBuilder.SetBfcInlineOffset(layoutunit.FromFloat64Round(bfcInlineOrigin + childMargins.InlineStart))
				csBuilder.SetBfcContainerInlineSize(layoutunit.FromFloat64Round(bfcContainerInlineSize))
			}

			// Propagate fragmentation context to children — except
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
			if bla.space.HasBlockFragmentation && !childIsMonolithic {
				childFragOffset := bla.space.FragmentainerOffset + blockCursor + prevMarginStrut.Resolve()
				csBuilder.
					SetHasBlockFragmentation(true).
					SetFragmentainerBlockSize(bla.space.FragmentainerBlockSize).
					SetFragmentainerOffset(childFragOffset).
					SetBlockFragmentationType(bla.space.BlockFragmentationType).
					SetIsInitialColumnBalancingPass(bla.space.IsInitialColumnBalancingPass).
					SetIsInsideBalancedColumns(bla.space.IsInsideBalancedColumns)

				// Propagate the "spanner descendants blocked" flag: set it when
				// any ancestor already blocks, or when the current block itself
				// would block — so descendants at any depth see the flag.
				// Mirrors Blink's ShouldPreventColumnSpannerDescendants walk in
				// DoesAncestryAllowColumnSpanner (layout_box.cc:2987-3001).
				if bla.space.ColumnSpannerDescendantsBlocked ||
					shouldPreventColumnSpannerDescendants(bla.node) {
					csBuilder.SetColumnSpannerDescendantsBlocked(true)
				}

				// Propagate "inside column-spanner" so the Phase 16.d.1 clamp
				// stays disabled on every spanner descendant, not just the
				// spanner itself. Driver: spanner-fragmentation-006 — without
				// this propagation the spanner's grand-leaf descendants (e.g.
				// the 360h leaf inside spanner 1) self-fragment and confuse
				// the existing pendingContentOverflow resume mechanism.
				if bla.space.IsInsideColumnSpanner {
					csBuilder.SetIsInsideColumnSpanner(true)
				}

				// Pass child break token if resuming this specific child.
				if tok, ok := resumeChildTokens[child]; ok {
					csBuilder.SetBreakToken(tok)
				}
			}

			childSpace := csBuilder.Build()

			// Recursively lay out the child.
			childResult := layoutElement(bla.ctx, child, childSpace)

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

			// Phase 16.d.2/3 (v2 B2): propagate child's accumulated tallest
			// unbreakable block-size during the initial column-balancing pass,
			// regardless of whether the child itself avoids breaks. The child's
			// own break-inside:avoid contribution is added separately inside
			// BreakBeforeChildIfNeeded (fragmentation_utils.go); this site
			// handles the carrier propagation up the layout tree (deeper
			// descendants whose floors were already aggregated into the
			// child's result). Mirrors Blink box_fragment_builder.cc:566-569.
			if bla.space.IsInitialColumnBalancingPass && childResult != nil {
				builder.PropagateTallestUnbreakableBlockSize(childResult.TallestUnbreakableBlockSize)
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
				bfcBlockExtent := childLogicalTmp.BlockSize() + childMargins.BlockSum()
				origFloatStartOff, origFloatEndOff := exclusionSpace.FindAvailableInlineSize(
					floatCheckBlock, bfcBlockExtent, childAvailableInline)
				if origFloatStartOff > 0 || origFloatEndOff > 0 {
					neededInline := childLogicalTmp.InlineSize() + childMargins.InlineSum()
					availableInline := childAvailableInline - origFloatStartOff - origFloatEndOff
					if neededInline > availableInline {
						// Child doesn't fit — find the earliest block position
						// where the BFC fits alongside remaining floats.
						newBlockBfc := exclusionSpace.FindFloatPosition(
							css.FloatLeft, neededInline, bfcBlockExtent,
							childAvailableInline, floatCheckBlock)
						newBlock := newBlockBfc - bfcBlockOrigin
						if newBlock > blockCursor {
							blockCursor = newBlock
							prevMarginStrut = MarginStrut{}
							hasClearance = true
							floatStartOff, floatEndOff, childInlineForSpace = reQueryFloatsAfterDrop(
								exclusionSpace, bfcBlockOrigin+blockCursor, bfcBlockExtent,
								childAvailableInline, childMargins.InlineSum())
							// Re-layout with the new available size.
							csBuilder2 := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
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
								SetExclusionSpace(exclusionSpace)
							childSpace = csBuilder2.Build()
							childResult = layoutElement(bla.ctx, child, childSpace)
						}
					}
				}
			}

			// Propagate nested ColumnSpannerPath: if a descendant of `child`
			// returned column-span:all, wrap the path and return early so the
			// multicol algorithm can extract the spanner at full container width.
			// Mirrors Blink's BlockLayoutAlgorithm setting SetColumnSpannerPath
			// on container_builder_ when child layout_result has a non-nil
			// GetColumnSpannerPath() (block_layout_algorithm.cc).
			if bla.space.HasBlockFragmentation &&
				bla.space.BlockFragmentationType == FragmentColumn &&
				childResult.ColumnSpannerPath != nil {

				// Build a break token so multicol can resume after the spanner:
				// if the child still has content (its own break token) carry that;
				// otherwise point to the next sibling.
				var resumeToken *BlockBreakToken
				if childResult.BreakToken != nil {
					resumeToken = childResult.BreakToken
				} else if childIdx+1 < len(children) {
					resumeToken = &BlockBreakToken{
						Node:          children[childIdx+1],
						IsBreakBefore: true,
					}
				}
				// LOU-320: place this child's PRE-spanner fragment before
				// extracting the nested spanner. The child (e.g. a fixed-height
				// container) holds the column content up to the spanner; this is
				// the partial fragment `childResult.Fragment` produced when the
				// child's own layout stopped at the spanner. Without placing it,
				// a spanner nested inside this child leaves the pre-spanner column
				// row empty and reports intrinsicBlock=0, collapsing the row (the
				// container's blocks vanish). Direct-child spanners don't need this
				// because their pre-spanner siblings were placed in earlier loop
				// iterations. Mirrors Blink keeping pre-spanner content in-flow:
				// block_layout_algorithm.cc:820-827 (SetShouldForceSameFragmentation
				// Flow) + AddChild before SetColumnSpannerPath (:1028-1032), Chromium
				// main @ d694f1edc784ebb2ce84dedde5ae3905d50c14f2.
				childBlockOffset := blockCursor + prevMarginStrut.Resolve()
				childOffset := LogicalOffset{
					InlineOffset: childMargins.InlineStart,
					BlockOffset:  childBlockOffset,
				}
				if isChildNewFC {
					// Same line-left clamp as the main in-flow path (LOU-327):
					// a negative inline margin cannot pull the new-FC border box
					// into a line-left float's margin area.
					childOffset.InlineOffset = newFCInlineOffsetBesideFloats(childMargins.InlineStart, floatStartOff)
				}
				builder.AddChild(childResult.Fragment, childOffset)
				builder.PropagateChildBlockSizeForFragmentation(childResult, childOffset)
				// Advance by the child's PRE-spanner content extent
				// (IntrinsicBlockSize), not its fragment border-box size: a
				// definite-height child split by a spanner reports its full CSS
				// height as the fragment size but only placed `IntrinsicBlockSize`
				// of content before the spanner. Using the full size would make
				// the pre-spanner column row the whole container height.
				preSpannerExtent := childResult.IntrinsicBlockSize
				if preSpannerExtent <= 0 {
					preSpannerExtent = NewLogicalFragment(wdm, childResult.Fragment).BlockSize()
				}
				blockCursor = childBlockOffset + preSpannerExtent

				var outToken *BlockBreakToken
				if resumeToken != nil {
					outToken = &BlockBreakToken{
						Node:              bla.node,
						ConsumedBlockSize: layoutunit.FromFloat64Round(blockCursor),
					}
					if incomingBreakToken != nil {
						outToken.ConsumedBlockSize = outToken.ConsumedBlockSize.Add(incomingBreakToken.ConsumedBlockSize)
						outToken.SequenceNumber = incomingBreakToken.SequenceNumber + 1
					}
					outToken.ChildBreakTokens = []*BlockBreakToken{resumeToken}
				}
				intrinsicBlock := blockCursor
				builder.SetIntrinsicBlockSize(intrinsicBlock)
				builder.SetNode(bla.node.DOMNode)
				builder.SetStyle(bla.style)
				builder.SetLayoutNode(bla.node)
				if !hasExplicitBlock {
					builder.SetSize(LogicalSize{
						InlineSize: geom.BorderBoxSize.InlineSize,
						BlockSize:  intrinsicBlock + geom.BlockBorderPadding(),
					})
				} else {
					builder.SetSize(geom.BorderBoxSize)
				}
				builder.SetBoxData(&PhysicalBoxData{
					Border:    ToPhysicalEdges(geom.Border, wdm),
					Padding:   ToPhysicalEdges(geom.Padding, wdm),
					Scrollbar: ToPhysicalEdges(geom.Scrollbar, wdm),
				})
				builder.SetEndMarginStrut(prevMarginStrut)
				builder.SetExclusionSpace(exclusionSpace)
				result := builder.Build()
				result.BreakToken = outToken
				result.ColumnSpannerPath = &ColumnSpannerPath{
					Box:   child,
					Child: childResult.ColumnSpannerPath,
				}
				result.PropagatedTopMargin = propagatedTopMargin
				if intrinsicBlock > NewLogicalFragment(wdm, result.Fragment).BlockSize() {
					result.BlockSizeForFragmentation = intrinsicBlock
				}
				return result
			}

			// CSS 2.1 §9.5: Floats from non-BFC children escape to the
			// parent BFC. After laying out a non-BFC child, update our
			// exclusion space so subsequent siblings see the child's floats.
			// Mirrors Blink's: exclusion_space_ = layout_result->ExclusionSpace()
			if !isChildNewFC && childResult.ExclusionSpace != nil {
				exclusionSpace = childResult.ExclusionSpace
			}

			// Step 1: Append child's block-start margin to the strut.
			prevMarginStrut.Append(childMargins.BlockStart)

			// Step 2: Include any propagated top margin from the child
			// (recursive parent-child collapsing).
			if !childResult.PropagatedTopMargin.IsEmpty() {
				prevMarginStrut.AppendStrut(childResult.PropagatedTopMargin)
			}

			// Step 3: Check if margins collapse through this element.
			// CSS 2.1 §8.3.1: An element's margins collapse through it if
			// it has no height, no border, no padding, does not establish
			// a new block formatting context, does not contain a line box,
			// and all of its in-flow children's margins (if any) are collapsed.
			// We approximate this by checking that the element has no fragment
			// children (no content).
			//
			// Paint-isolation properties (filter, opacity<1, transform, mask,
			// clip-path, mix-blend-mode, isolation, backdrop-filter) produce
			// visible output independently of the box's content — e.g. a
			// 0x0 box with `filter:url(#flood)` paints flood pixels via the
			// SVG filter's own region. Such boxes must NOT collapse through
			// because louis14's collapse-through path drops the fragment
			// entirely (the `if collapseThrough { ... continue }` branch
			// below), which would hide the box from the paint walk.
			// The same applies to a visible outline: a 0x0 box with
			// `outline: solid green 50px` paints a 100x100 ring (Blink keeps
			// empty rects for normal outlines — outline_painter.cc:960
			// "Keep empty rect for normal outline").
			// Mirrors Blink's effective behavior where the fragment is
			// always emitted regardless of self-collapse — LayoutNG's
			// `BlockLayoutAlgorithm::Layout` adds every child fragment via
			// `AddChild`, and self-collapse only affects margin resolution,
			// not fragment-tree membership. Blink @
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
			childLogical := NewLogicalFragment(wdm, childResult.Fragment)
			childBlockSize := childLogical.BlockSize()
			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			collapseThrough := childBlockSize == 0 &&
				childResult.BreakToken == nil &&
				childResult.BlockSizeForFragmentation == 0 &&
				len(childResult.Fragment.Children) == 0 &&
				childGeom.Border.BlockStart == 0 && childGeom.Border.BlockEnd == 0 &&
				childGeom.Padding.BlockStart == 0 && childGeom.Padding.BlockEnd == 0 &&
				!isChildNewFC &&
				!hasPaintIsolation(childStyle) &&
				(childStyle == nil || childStyle.GetOutlineWidth() == 0)

			// Step 4: Position child in the inline direction.
			// CSS 2.1 §9.5: Non-BFC block boxes flow as if floats don't exist.
			// Only BFC children (isChildNewFC) are offset past floats.
			// Non-BFC children are positioned at their margin; their line boxes
			// shorten around floats instead.
			// CSS 2.1 §10.3.3: If both margin-inline-start and margin-inline-end are auto,
			// and the element has a definite inline-size, center it.
			// NOTE: computed before collapse-through check so it can be used for OOF
			// propagation even when the element collapses through.
			childInlineOffset := childMargins.InlineStart
			if isChildNewFC {
				// CSS 2.1 §9.5 / Blink LayoutNewFormattingContext: a new-FC box's
				// inline margins are measured from the content box and cannot pull
				// its border box into a line-left float's margin area. A negative
				// margin is clamped; floatStartOff is not simply added (LOU-327).
				childInlineOffset = newFCInlineOffsetBesideFloats(childMargins.InlineStart, floatStartOff)
			}

			if collapseThrough {
				// Margins collapse through: append block-end margin and continue
				// without resolving or advancing the cursor.
				// Pick up any propagated OOF candidates before continuing.
				if len(childResult.PropagatedOOFCandidates) > 0 ||
					(childResult.Fragment != nil && childResult.Fragment.FragmentedOofData != nil) {
					approxBlock := blockCursor + prevMarginStrut.Resolve()
					bla.inheritPropagatedOOF(childResult, childStyle, wdm,
						childInlineOffset, approxBlock, builder)
				}
				prevMarginStrut.Append(childMargins.BlockEnd)
				continue
			}

			// Phase 12d: forced-break / break-appeal dispatch in column context.
			// Mirrors Blink's BreakBeforeChildIfNeeded called by every block
			// algorithm right after laying out an in-flow child. Skipped for
			// resumed children (those with an incoming token) — break-before at
			// the start of a fragmentainer is a no-op per CSS Fragmentation §3.
			if bla.space.HasBlockFragmentation &&
				bla.space.BlockFragmentationType == FragmentColumn &&
				resumeChildTokens[child] == nil {

				hasContainerSeparation := !firstNonEmptyChild
				tentativeBlockOff := blockCursor + prevMarginStrut.Resolve()
				fragOff := bla.space.FragmentainerOffset + tentativeBlockOff
				status, isForced := BreakBeforeChildIfNeeded(
					bla.space, child, childResult,
					fragOff, bla.space.FragmentainerBlockSize,
					hasContainerSeparation, builder)

				if status == BreakStatusBrokeBefore {
					// Emit a partial fragment without placing this child;
					// outgoing break token points at THIS child with
					// IsBreakBefore=true (and IsForcedBreak when the value
					// was column/page).
					outToken := &BlockBreakToken{
						Node:              bla.node,
						ConsumedBlockSize: layoutunit.FromFloat64Round(blockCursor),
					}
					if incomingBreakToken != nil {
						outToken.ConsumedBlockSize = outToken.ConsumedBlockSize.Add(incomingBreakToken.ConsumedBlockSize)
						outToken.SequenceNumber = incomingBreakToken.SequenceNumber + 1
					}
					outToken.ChildBreakTokens = append(outToken.ChildBreakTokens, &BlockBreakToken{
						Node:          child,
						IsBreakBefore: true,
						IsForcedBreak: isForced,
					})

					intrinsicBlock := blockCursor
					if !hasExplicitBlock {
						// CSS Fragmentation §3.4.2: a non-last fragment's background
						// extends to the fragmentainer boundary — but only when content
						// was actually placed (intrinsicBlock > 0). If nothing was placed
						// before the break (e.g. break-inside:avoid on the first child),
						// extending an empty fragment would confuse the balance loop.
						extendedBlock := intrinsicBlock
						if intrinsicBlock > 0 && bla.space.FragmentainerBlockSize != Indefinite &&
							bla.space.IsInsideBalancedColumns {
							remaining := bla.space.FragmentainerBlockSize - bla.space.FragmentainerOffset - intrinsicBlock
							if remaining > 0 {
								extendedBlock += remaining
							}
						}
						borderBoxBlock := extendedBlock + geom.BlockBorderPadding()
						builder.SetSize(LogicalSize{
							InlineSize: geom.BorderBoxSize.InlineSize,
							BlockSize:  borderBoxBlock,
						})
					} else {
						builder.SetSize(geom.BorderBoxSize)
					}
					builder.SetIntrinsicBlockSize(intrinsicBlock)
					builder.SetNode(bla.node.DOMNode)
					builder.SetStyle(bla.style)
					builder.SetLayoutNode(bla.node)
					builder.SetBoxData(&PhysicalBoxData{
						Border:    ToPhysicalEdges(geom.Border, wdm),
						Padding:   ToPhysicalEdges(geom.Padding, wdm),
						Scrollbar: ToPhysicalEdges(geom.Scrollbar, wdm),
					})
					builder.SetEndMarginStrut(prevMarginStrut)
					builder.SetExclusionSpace(exclusionSpace)
					result := builder.Build()
					result.BreakToken = outToken
					if isForced {
						result.HasForcedBreak = true
					}
					// Phase 12g: when a soft break-before fires (not forced), the
					// child didn't fit in the remaining fragmentainer space and
					// was pushed to the next one. The space we WOULD have needed
					// is the child's block-size, and the space we HAVE is
					// (fragmentainerBlockSize − fragOff). Report the difference
					// as MinSpaceShortage so the multicol balancing loop can
					// stretch just enough to keep the child whole with its
					// siblings. Without this, avoid-inside violations on
					// subsequent children report shortage=0 and the stretch
					// loop has no signal to grow the column.
					if !isForced && childResult != nil && childResult.Fragment != nil &&
						bla.space.FragmentainerBlockSize != Indefinite {
						childBlock := NewLogicalFragment(wdm, childResult.Fragment).BlockSize()
						spaceLeft := bla.space.FragmentainerBlockSize - fragOff
						if childBlock > spaceLeft {
							result.MinSpaceShortage = childBlock - spaceLeft
						}
					}
					result.PropagatedTopMargin = propagatedTopMargin
					return result
				}
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

			// Step 5: Handle parent-child top margin collapsing.
			// CSS 2.1 §8.3.1: parent-child collapsing requires that the
			// child has no clearance.
			var actualChildBlockOff float64
			if firstNonEmptyChild && canPropagateTop && !hasClearance {
				// Propagate the accumulated margin strut upward.
				propagatedTopMargin = prevMarginStrut
				actualChildBlockOff = 0
				// Position child at offset 0 (margin moves outside parent).
				childOffset := LogicalOffset{
					InlineOffset: childInlineOffset,
					BlockOffset:  0,
				}
				builder.AddChild(childResult.Fragment, childOffset)
				// LOU-111 step 4: propagate child's overflow extent into
				// the parent's BSFF so parallel-flow / monolithic overflow
				// reaches outer fragmentation contexts. Gated inside the
				// propagator to fire only when the child shows true
				// overflow past its physical extent (see fragment_builder
				// .go docs); harmless for normal stacking.
				builder.PropagateChildBlockSizeForFragmentation(childResult, childOffset)
				blockCursor = childBlockSize
			} else {
				// Step 6: Normal margin resolution.
				collapsedMargin := prevMarginStrut.Resolve()
				actualChildBlockOff = blockCursor + collapsedMargin
				childOffset := LogicalOffset{
					InlineOffset: childInlineOffset,
					BlockOffset:  actualChildBlockOff,
				}
				builder.AddChild(childResult.Fragment, childOffset)
				// LOU-111 step 4: propagate child's overflow. See note above.
				builder.PropagateChildBlockSizeForFragmentation(childResult, childOffset)
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
				bla.inheritPropagatedOOF(childResult, childStyle, wdm,
					childInlineOffset, actualChildBlockOff, builder)
			}

			// Phase 12d: record this child's break-after on the builder so the
			// next sibling's break-between value can join with it. Mirrors
			// Blink's BoxFragmentBuilder::AddChild → SetPreviousBreakAfter
			// (which reads layout_result.FinalBreakAfter — for non-fragmented
			// children that's the same as the child's style break-after). Only
			// fire when non-auto so a default-everywhere "auto" doesn't
			// inadvertently overwrite a non-default previous value.
			if bla.space.HasBlockFragmentation &&
				bla.space.BlockFragmentationType == FragmentColumn {
				if ba := childStyle.GetBreakAfter(); ba != "auto" {
					builder.SetPreviousBreakAfter(ba)
				}
			}

			firstNonEmptyChild = false

			// For a <fieldset>, the first/last baseline comes from the
			// anonymous content block, not the <legend>. Mirrors Blink's
			// FieldsetLayoutAlgorithm, which exports the content fragment's
			// baselines (fieldset_layout_algorithm.cc:409-419).
			isLegendOfFieldset := bla.node.DOMNode != nil &&
				bla.node.DOMNode.TagName == "fieldset" &&
				child.DOMNode != nil && child.DOMNode.TagName == "legend"

			// CSS Inline §4.2: propagate the first in-flow block child's
			// first baseline as this container's first baseline.
			// CSS Align §9.1: orthogonal children don't contribute baselines
			// in the parent's writing mode, so skip them.
			// Shared guard: capture first baseline in layout order (mirrors Blink).
			if !isLegendOfFieldset && !isOrthogonal && !hasFirstChildBaseline && childResult.HasBaseline {
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
					builder, childResult.Fragment, childResult, actualChildBlockOff,
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
			if !isLegendOfFieldset && !isOrthogonal {
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

			// Reset margin strut to the child's block-end margin.
			prevMarginStrut = childResult.EndMarginStrut
			prevMarginStrut.Append(childMargins.BlockEnd)

			// Fragmentation: check if we've overflowed the fragmentainer.
			if bla.space.HasBlockFragmentation {
				fragSize := bla.space.FragmentainerBlockSize
				fragEnd := fragSize - bla.space.FragmentainerOffset
				// Stop when blockCursor overflows OR when blockCursor exactly
				// reaches the fragmentainer boundary AND the child still has
				// remaining content (its BreakToken signals it didn't fully fit).
				// Without the second condition, a child that exactly fills the
				// column would let the loop continue to the next sibling (e.g. a
				// column-span:all spanner), causing the child's remaining content
				// to be discarded and only one column to be populated.
				childHasBreak := childResult.BreakToken != nil
				// LOU-365: ALSO stop when the child broke inside but its slice
				// ends SHORT of the boundary (line-granular breaking leaves the
				// last partial line's space unused: cursor < fragEnd). The
				// fragmentainer is still consumed — falling through used to
				// silently DROP the child's break token (losing its remaining
				// content) and its space shortage. Blink treats any inside
				// break as consuming the fragmentainer unless the token is
				// at-block-end / a parallel flow (MovePastBreakpoint,
				// fragmentation_utils.cc:1178-1186 @ a9f50e522efa). Zero-
				// progress slices keep their dedicated branch below.
				childBrokeShortOfBoundary := childHasBreak && blockCursor < fragEnd &&
					childBlockSize > 0 &&
					!IsParallelFlowContinuation(childResult.BreakToken)
				if fragSize != Indefinite && (blockCursor > fragEnd ||
					(blockCursor == fragEnd && childHasBreak) ||
					childBrokeShortOfBoundary) {
					// Content overflowed (or exactly filled with break).
					//
					// Option-b step 6.4.b: record pending state and break.
					// The post-loop frag-overflow reader (added in this
					// same step, sitting before the 6.2 forced-break
					// reader) builds the outgoing LayoutResult from the
					// pending-* fields below. This is a verbatim port of
					// the pre-6.4.b inline body — every read/write maps
					// 1:1 per plan §3.3.0 / §3.3.1. Step 6.4.c will
					// migrate this onto the unified §3.6 reader.
					shortage := blockCursor - fragEnd
					if shortage < 0 {
						shortage = 0
					}
					// LOU-365: if space shortage was reported inside the
					// child, use that. The boundary arithmetic above is zero
					// when the child's slice ends exactly at the fragmentainer
					// boundary, which would starve the multicol stretch loop
					// of its grow-by amount. Mirrors Blink's
					// CalculateSpaceShortage (fragmentation_utils.cc:1027-1049
					// @ a9f50e522efa): "if we broke inside the child ...
					// space shortage was reported inside the child, use that."
					//
					// Narrowed vs Blink to children whose slicing violated a
					// break-avoidance (break-inside:avoid / monolithic): Blink
					// merges unconditionally but its stretch loop is budget-
					// clamped by ConstrainColumnBlockSize (cla.cc:1799-1809 @
					// a9f50e522efa — the CurrentContentBlockOffset/consumed
					// subtraction louis14 does not implement yet; see the
					// css-multicol round-1 Target 1 plan). Merging for
					// non-violating breaks over-stretches budget-clamped
					// definite-height rows (LOU-320/LOU-325 unit tests).
					if childResult.BreakToken != nil && childResult.MinSpaceShortage > 0 &&
						ShouldAvoidBreakInside(bla.space, childResult) {
						shortage = childResult.MinSpaceShortage
					}
					pendingShouldBreakInside = true
					pendingHasInflowChildBreakInside = true
					pendingIntrinsicAtBreak = blockCursor
					pendingHaveIntrinsicAtBreak = true
					pendingMinSpaceShortage = shortage

					// Step 3.5.A — uniform IsAtBlockEnd emitter (parallel-flow
					// signaling). Mirrors Blink's FinishFragmentation at
					// `fragmentation_utils.cc:744-755`: when a child broke but
					// the PARENT's own box fits inside the fragmentainer
					// (`desired_block_size <= space_left`), the parent itself
					// is at-block-end and the carried child continuation runs
					// in a parallel flow.
					//
					// `desired_block_size` here is the parent's declared
					// box-content extent for the current fragment (`explicit
					// - prevConsumed`); `space_left` is the remaining outer
					// fragmentainer space. When parent box overflows
					// (`desired > space_left`) the parent needs to self-break,
					// so IsAtBlockEnd is NOT set — see test 006 spanner-3.
					//
					// LOU-multicol-nested (C51) gate: skip when this BLA's
					// explicitBlockSize was synthesized from
					// IsBlockSizeOverride (e.g. anonymous multicol column
					// wrapper). In that case the "explicit" size is the
					// per-fragment column slot height the parent multicol
					// forced on us, NOT the box's CSS declared total height.
					// Treating it as a CSS-declared size triggers IsAtBlockEnd
					// at the column boundary even though the column wrapper
					// still has more content for the next column — the
					// resumed fragment then gets zero-clamped by 3.5.B and
					// the multicol algorithm sees an empty column.
					// Mirrors Blink's FinishFragmentation, which reads
					// `box_block_size_` from the wrapper's CSS, not from the
					// ConstraintSpace fragmentainer override.
					if childResult.BreakToken != nil && hasExplicitBlock &&
						!bla.space.IsBlockSizeOverride {
						spaceLeft := bla.space.FragmentainerBlockSize - bla.space.FragmentainerOffset
						if spaceLeft < 0 {
							spaceLeft = 0
						}
						desiredBlockSize := explicitBlockSize
						if incomingBreakToken != nil {
							desiredBlockSize -= incomingBreakToken.ConsumedBlockSize.Float64()
							if desiredBlockSize < 0 {
								desiredBlockSize = 0
							}
						}
						if desiredBlockSize <= spaceLeft {
							pendingIsAtBlockEnd = true
						}
					}

					// Child-break dispatch tree. Verbatim from pre-6.4.b
					// lines 1196-1299 — every `outToken.ChildBreakTokens
					// = append(...)` rewritten as `pendingChildBreakTokens
					// = append(...)`, every `outToken.HasSeenAllChildren
					// = true` as `pendingHasSeenAllChildren = true`,
					// every `outToken.IsAtBlockEnd = true` as
					// `pendingIsAtBlockEnd = true`. Decision tree
					// unchanged.
					if childResult.BreakToken != nil {
						pendingChildBreakTokens = append(pendingChildBreakTokens, childResult.BreakToken)
					} else if childResult.Fragment != nil && childResult.Fragment.IsMonolithic {
						// LOU-365: a monolithic child is moved past the break
						// point AS A UNIT — it overflows the fragmentainer and
						// is DONE; never emit a consumed-progress resume token
						// for it (fragmentation_utils.cc:329-339 @
						// a9f50e522efa). The leaf-slicing branch below would
						// emit one, but the space gate (correctly) refuses to
						// slice the child on resume, so that token can never
						// finish — an infinite fragmentainer loop
						// (multicol-nested-011 watchdog hang). The parent's
						// own continuation resumes at the next sibling.
						if childIdx+1 < len(children) {
							pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
								Node:          children[childIdx+1],
								IsBreakBefore: true,
							})
						} else {
							pendingHasSeenAllChildren = true
						}
					} else if len(child.Children()) == 0 && !hasOnlyInlineChildren(child) {
						// Leaf block: child completed but its declared size overflowed.
						childConsumed := fragEnd - actualChildBlockOff
						if childConsumed == 0 && (fragSize > 0 || !bla.space.IsBlockSizeOverride) {
							// Child starts exactly at fragEnd. Fresh start
							// next fragmentainer (also covers the balance-
							// estimate fragSize==0 path).
							pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
								Node:          child,
								IsBreakBefore: true,
							})
						} else if childConsumed == 0 && fragSize == 0 && bla.space.IsBlockSizeOverride {
							// True zero-height fragmentainer: advance past
							// the leaf to the next sibling.
							if childIdx+1 < len(children) {
								nextChild := children[childIdx+1]
								pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
									Node:          nextChild,
									IsBreakBefore: true,
								})
							} else {
								pendingHasSeenAllChildren = true
							}
						} else if bla.space.IsBlockSizeOverride {
							// Inner column context: fragment leaf at column
							// boundary; accumulate consumed across fragments.
							totalConsumed := childConsumed
							if tok := resumeChildTokens[child]; tok != nil {
								totalConsumed += tok.ConsumedBlockSize.Float64()
							}
							pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
								Node:              child,
								ConsumedBlockSize: layoutunit.FromFloat64Round(totalConsumed),
							})
						} else {
							// Non-column context: parallel-flow vs legacy
							// monolithic-leaf fallback.
							parentBoxSatisfied := hasExplicitBlock &&
								actualChildBlockOff+childBlockSize > explicitBlockSize
							if parentBoxSatisfied {
								pendingIsAtBlockEnd = true
								totalConsumed := childConsumed
								if tok := resumeChildTokens[child]; tok != nil {
									totalConsumed += tok.ConsumedBlockSize.Float64()
								}
								pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
									Node:              child,
									ConsumedBlockSize: layoutunit.FromFloat64Round(totalConsumed),
									IsInParallelFlow:  true,
								})
							} else if childIdx+1 < len(children) {
								nextChild := children[childIdx+1]
								pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
									Node:          nextChild,
									IsBreakBefore: true,
								})
							} else {
								pendingHasSeenAllChildren = true
							}
						}
					} else {
						// Child completed; resume at next sibling.
						if childIdx+1 < len(children) {
							nextChild := children[childIdx+1]
							pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
								Node:          nextChild,
								IsBreakBefore: true,
							})
						} else {
							pendingHasSeenAllChildren = true
						}
					}

					// Phase 12g BreakAppeal demotion. Verbatim from
					// pre-6.4.b lines 1367-1427, swapping `worstAppeal`
					// for `pendingBreakAppeal` (initialized to
					// BreakAppealPerfect at function entry).
					if childResult != nil && childResult.BreakAppeal < pendingBreakAppeal {
						pendingBreakAppeal = childResult.BreakAppeal
					}
					isInsideCurrent := childResult != nil && childResult.BreakToken != nil
					if !isInsideCurrent && len(child.Children()) == 0 &&
						!hasOnlyInlineChildren(child) && bla.space.IsBlockSizeOverride {
						if fragEnd-actualChildBlockOff > 0 {
							isInsideCurrent = true
						}
					}
					if isInsideCurrent && childStyle != nil {
						if IsAvoidBreakValue(bla.space, childStyle.GetBreakInside()) &&
							pendingBreakAppeal > BreakAppealViolatingBreakAvoid {
							pendingBreakAppeal = BreakAppealViolatingBreakAvoid
						}
					}
					isBreakBeforeCurrent := !isInsideCurrent &&
						len(child.Children()) == 0 &&
						!hasOnlyInlineChildren(child) &&
						fragEnd-actualChildBlockOff == 0
					if isBreakBeforeCurrent && childStyle != nil {
						breakBetween := builder.JoinedBreakBetweenValue(
							childStyle.GetBreakBefore())
						if IsAvoidBreakValue(bla.space, breakBetween) &&
							pendingBreakAppeal > BreakAppealViolatingBreakAvoid {
							pendingBreakAppeal = BreakAppealViolatingBreakAvoid
						}
					}
					deferredNextSibling := childIdx+1 < len(children)
					if !isInsideCurrent && !isBreakBeforeCurrent && deferredNextSibling {
						nextChild := children[childIdx+1]
						var curAfter, nextBefore string
						if childStyle != nil {
							curAfter = childStyle.GetBreakAfter()
						}
						if s := nextChild.Style(); s != nil {
							nextBefore = s.GetBreakBefore()
						}
						between := JoinFragmentainerBreakValues(curAfter, nextBefore)
						if IsAvoidBreakValue(bla.space, between) &&
							pendingBreakAppeal > BreakAppealViolatingBreakAvoid {
							pendingBreakAppeal = BreakAppealViolatingBreakAvoid
						}
					}

					// LOU-110: drop placed children at-or-past the boundary
					// (paint dedup vs the next fragmentainer's IsBreakBefore
					// entries). Gated on fragSize > 0 to skip the balance-
					// estimate path.
					if fragSize > 0 {
						pendingDropAtBlockOffsetEnabled = true
						pendingDropAtBlockOffset = fragEnd
					}

					// childResult.HasForcedBreak passthrough (frag-overflow
					// intersecting a forced break — preserve original
					// result.HasForcedBreak signal).
					if childResult != nil && childResult.HasForcedBreak {
						pendingHasForcedBreak = true
					}

					break
				} else if fragSize != Indefinite && childHasBreak && childBlockSize == 0 &&
					!isPureParallelFlowResume(childResult, incomingBreakToken) &&
					!bla.space.IsInitialColumnBalancingPass {
					// IFC broke before making any forward progress (zero-height
					// fragment + break token). The fragmentainer is full from
					// the parent's perspective even though blockCursor did not
					// advance.
					//
					// LOU-365 (CodeRabbit 3514362231 follow-on): pure
					// parallel-flow continuations are NOT zero progress and
					// must not stop the walk — see isPureParallelFlowResume;
					// they are collected by the parallel-flow carry instead.
					//

					// Option-b step 6.3 (plan §3.4 + step6_3_ifc_zero_progress.md):
					// structurally a subset of 6.4 — only the
					// `childResult.BreakToken != nil` dispatch branch fires, no
					// Fab D, no DropChildren, no BreakAppeal demotion, no
					// MinSpaceShortage, no HasForcedBreak. The unified §3.6
					// post-loop reader handles the build + outToken + return.
					//
					// Step 3.5.A IsAtBlockEnd predicate applies uniformly per
					// the audit's analysis of Blink Site C (frag_utils.cc:755):
					// when the parent's own box fits but the child broke, the
					// parent is at-block-end and the carried child runs in
					// parallel flow.
					pendingShouldBreakInside = true
					pendingHasInflowChildBreakInside = true
					pendingIntrinsicAtBreak = blockCursor
					pendingHaveIntrinsicAtBreak = true
					if hasExplicitBlock {
						spaceLeft := bla.space.FragmentainerBlockSize - bla.space.FragmentainerOffset
						if spaceLeft < 0 {
							spaceLeft = 0
						}
						desiredBlockSize := explicitBlockSize
						if incomingBreakToken != nil {
							desiredBlockSize -= incomingBreakToken.ConsumedBlockSize.Float64()
							if desiredBlockSize < 0 {
								desiredBlockSize = 0
							}
						}
						if desiredBlockSize <= spaceLeft {
							pendingIsAtBlockEnd = true
						}
					}
					pendingChildBreakTokens = append(pendingChildBreakTokens, childResult.BreakToken)
					break
				}

				// Forced column break propagated from a child (break-before/after:column).
				// Fires even when blockCursor < fragEnd (column isn't full yet).
				// Also fires during IsInitialColumnBalancingPass (fragSize=Indefinite) so the
				// ContentRuns measure loop records one run per forced-break segment.
				//
				// Option-b step 6.2: record pending state and break out of the
				// children loop. The post-loop forced-break reader (added in
				// this same step, immediately after the loop close) constructs
				// the outgoing LayoutResult from the pending state — a verbatim
				// move of the original inline body. Step 6.4 will subsume this
				// reader into the unified FinishFragmentation analogue.
				if (fragSize != Indefinite || bla.space.IsInitialColumnBalancingPass) && childResult.HasForcedBreak {
					pendingShouldBreakInside = true
					pendingHasInflowChildBreakInside = true
					pendingHasForcedBreak = true
					pendingIntrinsicAtBreak = blockCursor
					pendingHaveIntrinsicAtBreak = true
					if childResult.BreakToken != nil {
						pendingChildBreakTokens = append(pendingChildBreakTokens, childResult.BreakToken)
					} else if childIdx+1 < len(children) {
						pendingChildBreakTokens = append(pendingChildBreakTokens, &BlockBreakToken{
							Node:          children[childIdx+1],
							IsBreakBefore: true,
						})
					} else {
						pendingHasSeenAllChildren = true
					}
					break
				}

				// LOU-365: no boundary branch fired — the child was accepted
				// and the walk continues. If the child nevertheless carries a
				// parallel-flow continuation (its box is at block-end but its
				// content goes on), collect the token; dropping it here loses
				// the continuation entirely (fixed-size-child-with-overflow:
				// content rendered in column 1 only). Promoted into the
				// outgoing token after the walk.
				if fragSize != Indefinite && IsParallelFlowContinuation(childResult.BreakToken) {
					pendingParallelFlowChildTokens = append(pendingParallelFlowChildTokens, childResult.BreakToken)
				}
			}
		}
	}

	// Option-b step 6.4.c: the two ad-hoc post-loop readers (6.2
	// forced-break + 6.4.b frag-overflow) are removed here. The broken
	// path now flows through the natural post-loop — margin propagation
	// and float clearance are already suppressed by Gates A + B (6.4.a);
	// the Fab D override below preserves the column-spanner clamp until
	// 6.5.C deletes it; DropChildren and the unified §3.6 outToken
	// construction sit before/after `builder.Build()` further down.

	// CSS 2.1 §8.3.1: The bottom margin of an in-flow block box with a
	// 'height' of 'auto' collapses with its last child's bottom margin if
	// the box has no bottom border and no bottom padding. When block-size
	// is explicit (not auto), OR there is border/padding at block-end,
	// the trailing margin does NOT propagate out as EndMarginStrut.
	canPropagateBottom := !bla.space.IsNewFormattingContext &&
		!hasExplicitBlock &&
		geom.Border.BlockEnd == 0 && geom.Padding.BlockEnd == 0

	// Gate A (option-b step 6.4.a, plan §3.3.3): trailing-margin
	// propagation is suppressed on the broken path. Pre-restructure,
	// the frag-overflow / forced-break early-returns bypassed this
	// block; post-restructure, `pendingShouldBreakInside` flags the
	// broken path and the natural-path margin propagation must not
	// extend the broken fragment by trailing margin. Currently
	// dormant: the only setter of `pendingShouldBreakInside` is the
	// 6.2 forced-break path, which returns before reaching here.
	// 6.4.c activates the gate when frag-overflow flows through the
	// natural post-loop.
	if !pendingShouldBreakInside {
		if bla.space.IsNewFormattingContext && !prevMarginStrut.IsEmpty() {
			// BFC roots: trailing margins don't propagate out, and they
			// extend the auto height (CSS 2.1 §10.6.7).
			blockCursor += prevMarginStrut.Resolve()
			prevMarginStrut = MarginStrut{} // consumed
		} else if !canPropagateBottom && !prevMarginStrut.IsEmpty() {
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
	if !pendingShouldBreakInside && !hasExplicitBlock && hasOwnFloats {
		bfcCursor := bfcBlockOrigin + blockCursor
		clearedBlockBfc := exclusionSpace.ClearanceOffset(css.ClearBoth, layoutunit.FromFloat64Round(bfcCursor), wdm).Float64()
		clearedBlock := clearedBlockBfc - bfcBlockOrigin
		if clearedBlock > blockCursor {
			blockCursor = clearedBlock
		}
	}
	// Compute final block-size.
	intrinsicBlockSize := blockCursor
	// CSS Contain 1 §4.2: size containment — collapse intrinsic block-size to 0
	// when no explicit block-size is set (see contain_utils.go).
	intrinsicBlockSize = sizeContainedIntrinsicBlockSize(bla.style, hasExplicitBlock, intrinsicBlockSize)

	// CSS Lists §3 / Blink PositionListMarkerWithoutLineBoxes: if a list item
	// produced no line boxes / no child with a baseline, its still-pending
	// OUTSIDE marker is top-aligned at block offset 0 and contributes its
	// block-size to the list item's intrinsic block size. This MUST run before
	// builder.SetIntrinsicBlockSize / SetSize so the bump propagates (Blink
	// runs it from FinishLayout, before the builder is finalized).
	bla.positionListMarkerWithoutLineBoxes(builder, &intrinsicBlockSize)

	finalBlockSize := intrinsicBlockSize
	if hasExplicitBlock {
		finalBlockSize = explicitBlockSize
		// LOU-111 step 6.5.B — Blink S0 alignment for resumed column-spanners.
		// Mirrors fragmentation_utils.cc:551-557: when the spanner is resumed
		// in a subsequent outer fragmentainer, the declared CSS height
		// represents the WHOLE spanner box, not per-fragment. Subtract
		// previously consumed to get the per-fragment desired extent.
		//
		// This ports Fab D's `boxBlockSize -= ConsumedBlockSize` arithmetic
		// (block_layout.go:1284-1288) into the natural post-loop, so that
		// 6.5.C can delete Fab D without regressing spanner-fragmentation-006.
		// Non-spanner resumed blocks keep the existing intrinsic-as-fragment-
		// size / explicit-remaining branches below — those approximate the
		// per-fragment box extent for non-column contexts.
		isColumnSpanner := bla.style != nil && bla.style.GetColumnSpan() == "all"
		if isColumnSpanner && incomingBreakToken != nil {
			finalBlockSize -= incomingBreakToken.ConsumedBlockSize.Float64()
			if finalBlockSize < 0 {
				finalBlockSize = 0
			}
		} else if incomingBreakToken != nil && !bla.space.IsBlockSizeOverride && intrinsicBlockSize > 0 {
			// Resumed non-column block (e.g. spanner content in outer fragmentainer):
			// use the actual content placed in this fragment, not the CSS explicit height.
			// The CSS height belonged to the first fragment; this resumed fragment shows
			// whatever content was placed (its children, which may overflow the CSS height).
			finalBlockSize = intrinsicBlockSize
			// LOU-320: a definite-height block fragmented across column-span splits
			// distributes its CSS height across the inter-spanner segments — the
			// total over all fragments equals the declared height. When declared
			// budget remains (explicitBlockSize - consumed >= 0) but is smaller than
			// this segment's content, clamp to the remaining budget so the content
			// overflows the box rather than the box growing past its share (a fully
			// exhausted budget, remaining == 0, yields a zero-height trailing box —
			// e.g. 004b's block3 after a 350px container is spent).
			//
			// Gated to FragmentColumn: only multicol column fragmentation distributes
			// a definite block-size this way. In other fragmentation contexts (outer
			// page/region resume of overflowing spanner content) the resumed fragment
			// legitimately shows its actual content, so leave finalBlockSize =
			// intrinsicBlockSize — keeping this change provably multicol-scoped.
			if bla.space.BlockFragmentationType == FragmentColumn {
				finalBlockSize = clampToRemainingBlockBudget(
					finalBlockSize, explicitBlockSize, incomingBreakToken.ConsumedBlockSize.Float64())
			}
		} else if incomingBreakToken != nil && !incomingBreakToken.ConsumedBlockSize.IsZero() && intrinsicBlockSize == 0 {
			// Resumed leaf block: show remaining declared height (CSS height - consumed).
			finalBlockSize = clampToRemainingBlockBudget(
				finalBlockSize, explicitBlockSize, incomingBreakToken.ConsumedBlockSize.Float64())
		}
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

	// Step 3.5.B — parent zero-clamp on resume after IsAtBlockEnd.
	//
	// Mirrors Blink's `fragmentation_utils.cc:599-600` (`is_past_end`
	// path): when the PREVIOUS fragment of this block was marked
	// at-block-end (step 3.5.A on the outgoing token), the box's
	// visible bounds were completed there. The resumed fragment is a
	// phantom zero-block-size container that exists only to carry
	// parallel-flow children's continuations via incoming
	// ChildBreakTokens. Applied AFTER min/max so it survives any
	// min-block-size re-inflation — past-end fragments are stitching
	// nodes, not CSS-constrained boxes.
	if incomingBreakToken != nil && incomingBreakToken.IsAtBlockEnd {
		finalBlockSize = 0
	}

	// Phase 16.d.1 — per-fragment block-size clamp + DidBreakSelf carrier.
	//
	// Mirrors Blink's FinishFragmentation (fragmentation_utils.cc:542-657):
	// when a block's desired border-box size exceeds the fragmentainer's
	// remaining space and we're inside a block-fragmentation context, clamp
	// the fragment to space_left and set DidBreakSelf so a continuation
	// BlockBreakToken is emitted below. The next fragmentainer resumes the
	// same block via incomingBreakToken.ConsumedBlockSize.
	//
	// Gates:
	//   - !IsBlockSizeOverride: the multicol's column-fragmentainer
	//     authoritatively sets the size; the child shouldn't second-guess.
	//   - hasExplicitBlock: only when CSS declared a finite block-size.
	//   - HasBlockFragmentation + finite FragmentainerBlockSize: must be in
	//     an active fragmentation context.
	//   - !IsInitialColumnBalancingPass: measurement pass MUST NOT emit
	//     break-tokens (would corrupt the balance estimate).
	//
	// LOU-111 step 7: removed the `(len(Children)==0 || isColumnSpanner)`
	// gate (Fabrication C). Blink's FinishFragmentation applies uniformly
	// to every node with HasBlockFragmentation, and the earlier
	// break-token-misalignment concerns this gate guarded against are
	// resolved by step 3.5.B's parent zero-clamp on resume.
	var didBreakSelf bool
	var selfBreakShortage float64
	if bla.space.HasBlockFragmentation && !bla.space.IsBlockSizeOverride &&
		bla.space.FragmentainerBlockSize != Indefinite &&
		bla.space.FragmentainerBlockSize > 0 && hasExplicitBlock &&
		!bla.space.IsInitialColumnBalancingPass {
		spaceLeft := bla.space.FragmentainerBlockSize - bla.space.FragmentainerOffset
		if spaceLeft < 0 {
			spaceLeft = 0
		}
		contentSpaceLeft := spaceLeft - geom.BlockBorderPadding()
		if contentSpaceLeft < 0 {
			contentSpaceLeft = 0
		}
		if finalBlockSize > contentSpaceLeft {
			// LOU-365: report how much more space would have fit this box
			// whole, so the multicol stretch loop knows its grow-by amount.
			// Mirrors Blink's FinishFragmentation "No child inside broke,
			// but we're too tall to fit" → PropagateSpaceShortage(
			// desired_block_size - space_left) for column fragmentation
			// (fragmentation_utils.cc:785-800 @ a9f50e522efa).
			if bla.space.BlockFragmentationType == FragmentColumn {
				selfBreakShortage = finalBlockSize - contentSpaceLeft
			}
			finalBlockSize = contentSpaceLeft
			didBreakSelf = true
		}
	}

	// Set the fragment size.
	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})

	builder.SetIntrinsicBlockSize(intrinsicBlockSize)

	// Set first baseline: for flex align-items:baseline (uses first line).
	// CSS Inline §4.2: the first baseline of a block container is:
	//   1. The first line box's baseline (inline children), OR
	//   2. The first in-flow block child's propagated first baseline.
	if firstLineAscent > 0 {
		builder.SetBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
	} else if hasFirstChildBaseline {
		builder.SetBaseline(geom.Border.BlockStart + geom.Padding.BlockStart +
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
			builder.SetLastBaseline(finalBlockSize + geom.BlockBorderPadding() + logMargins.BlockEnd)
		} else if hasLastChildBaseline {
			builder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart +
				lastChildBlockOffset + lastChildBaseline)
		} else if firstLineAscent > 0 {
			builder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
		}
	} else if hasLastChildBaseline {
		builder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart +
			lastChildBlockOffset + lastChildBaseline)
	} else if firstLineAscent > 0 {
		builder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
	}

	// Set box data for the renderer.
	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
	physMargin := ToPhysicalEdges(logMargins, wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:    physMargin,
		Border:    physBorder,
		Padding:   physPadding,
		Scrollbar: physScrollbar,
	})

	builder.SetEndMarginStrut(prevMarginStrut)
	builder.SetExclusionSpace(exclusionSpace)

	// CSS 2.1 §17.5.3 + CSS Tables 3 §5.4.1 — orphan table-cell vertical-align.
	//
	// A `display: table-cell` box whose ancestor is NOT a proper <table>
	// falls through to block layout here (see layout_tree_builder.go's
	// `normalizeTableSubtrees` — reverse §17.2.1 anonymous-table generation
	// is not implemented yet). For orphan cells we still need to honour
	// vertical-align so content centres within the cell's explicit
	// block-size, matching what browsers render after they wrap the
	// standalone cell in anon table/row boxes.
	//
	// Proper-table cells take the equivalent shift in table_layout.go's
	// per-row sweep (see the `vaBlockShift` block). We skip this branch
	// for those (distinguished by a non-nil TableSectionData on the
	// constraint space), so the shift is applied exactly once.
	//
	// Mirrors Blink's `TableCellLayoutAlgorithm` applying
	// `intrinsic_padding_before` to the cell's content when the row grows
	// past the cell's intrinsic block-size.
	if bla.style != nil && bla.style.GetDisplay() == css.DisplayTableCell &&
		bla.space.TableSectionData == nil && finalBlockSize > intrinsicBlockSize {
		va := bla.style.GetVerticalAlign()
		var vaShift float64
		switch va {
		case css.VerticalAlignMiddle:
			vaShift = (finalBlockSize - intrinsicBlockSize) / 2
		case css.VerticalAlignBottom:
			vaShift = finalBlockSize - intrinsicBlockSize
		}
		if vaShift > 0 {
			for i := range builder.children {
				builder.children[i].offset.BlockOffset += vaShift
			}
			for i := range builder.outOfFlowCandidates {
				builder.outOfFlowCandidates[i].StaticPosition.Offset.BlockOffset += vaShift
			}
		}
	}

	// CSS 2.1 §10.6.4 / CSS-POSITION-3: Lay out out-of-flow children.
	//
	// Mirrors Blink's distinction between absolute and fixed candidates:
	//   - position:absolute → containing block is nearest positioned ancestor
	//   - position:fixed → containing block is the ICB (viewport), skipping
	//     positioned ancestors. (Transform/filter ancestors that create a CB
	//     for fixed are a future enhancement.)
	//
	// A positioned element resolves absolute candidates but must propagate
	// fixed candidates upward. The root resolves ALL candidates.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		// CSS 2.1 §10.1.4 / CSS Position 3 §def-cb: an OOF whose CB resolves
		// to an inline element is sized against the inline's first/last-line
		// fragment union rect. Only the block that owns the inline's line
		// fragments has the geometry — resolve here when this block is
		// non-anonymous (anonymous blocks pass through to the parent which
		// walks transparently into them via ComputeInlineContainerGeometry).
		var inlineCBCandidates, regularCandidates []OutOfFlowCandidate
		for _, c := range builder.outOfFlowCandidates {
			if c.InlineContainer != nil {
				inlineCBCandidates = append(inlineCBCandidates, c)
			} else {
				regularCandidates = append(regularCandidates, c)
			}
		}

		if len(inlineCBCandidates) > 0 {
			if bla.node.IsAnonymous() {
				// Anonymous block — propagate so the nearest non-anon ancestor
				// can compute the inline CB across its full child set
				// (including a block-in-inline split's sibling anon blocks).
				propagatedOOF = append(propagatedOOF, inlineCBCandidates...)
			} else {
				blockContentPhys := ToPhysicalSize(LogicalSize{
					InlineSize: contentInlineSize,
					BlockSize:  finalBlockSize,
				}, wdm.WM)
				for _, cand := range inlineCBCandidates {
					inlineGeom := ComputeInlineContainerGeometry(
						builder.children,
						cand.InlineContainer,
						wdm,
						blockContentPhys,
					)
					if inlineGeom == nil {
						// Inline produced no line-box fragments in this block.
						// CSS 2.1 §9.4.2 suppresses a line box whose only content
						// is OOF items (e.g. <span pos:relative><div pos:abs/></span>
						// with no text). In that case the inline has no geometry
						// to derive a CB from, so fall through to normal
						// non-inline CB handling: clear InlineContainer and
						// propagate as a regular candidate so an enclosing
						// positioned ancestor (or the ICB) can resolve it
						// against its own content box.
						cand.InlineContainer = nil
						regularCandidates = append(regularCandidates, cand)
						continue
					}
					cbSize, cbOriginLogical := inlineGeom.InlineCBLogical(wdm, blockContentPhys)
					oofPart := &OutOfFlowLayoutPart{
						ctx:                    bla.ctx,
						containingBlockWDM:     wdm,
						containingBlockSize:    cbSize,
						containingBlockPadding: LogicalEdges{},
						geom:                   geom,
						resolvesFixed:          false,
						cbOriginInBuilder:      cbOriginLogical,
					}
					if extra := oofPart.LayoutCandidates([]OutOfFlowCandidate{cand}, builder); len(extra) > 0 {
						propagatedOOF = append(propagatedOOF, extra...)
					}
				}
			}
		}

		isPositioned := bla.style != nil && bla.style.GetPosition() != css.PositionStatic
		// CSS Containment: layout and paint containment establish a containing
		// block for absolutely positioned descendants (same as positioned elements).
		isContainmentCB := bla.style != nil && (bla.style.ShouldApplyLayoutContainment() || bla.style.ShouldApplyPaintContainment())
		// CSS Transforms §2.1 / Filter Effects 1+2 §"The filter property" /
		// Will Change §2.2: transform, perspective, filter, backdrop-filter,
		// and any will-change of a CB-establishing property create a
		// containing block for all positioned descendants (including fixed).
		isTransformCB := false
		if bla.style != nil && !isContainmentCB {
			if transforms := bla.style.GetTransforms(); len(transforms) > 0 {
				isTransformCB = true
			} else if filters := bla.style.GetFilter(); len(filters) > 0 {
				isTransformCB = true
			} else if bdFilters := bla.style.GetBackdropFilter(); len(bdFilters) > 0 {
				isTransformCB = true
			} else if bla.style.WillChangeEstablishesContainingBlock(true) {
				isTransformCB = true
			} else if bla.style.GetTransformStyle() == css.TransformStylePreserve3D {
				// CSS Transforms L2 §11 / Blink's HasTransformRelatedProperty:
				// transform-style:preserve-3d establishes a containing block
				// for all positioned descendants (including fixed). Mirrors
				// Blink's ComputedStyle::HasTransformRelatedProperty at SHA
				// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
				isTransformCB = true
			} else if bla.style.GetBackfaceVisibility() == css.BackfaceVisibilityHidden {
				// CSS Transforms L2 §11: backface-visibility:hidden establishes
				// a containing block (and stacking context) when the element
				// participates in a 3D rendering context. WPT
				// backface-visibility-hidden-003 covers the case where the
				// parent has transform-style:preserve-3d. Blink's
				// HasTransformRelatedProperty treats backface-visibility:hidden
				// as CB-establishing unconditionally; we mirror that.
				isTransformCB = true
			}
		}
		// CSS Will Change §2.2: `will-change: position` (and other abspos-only
		// CB triggers) establishes a CB for abs-pos descendants but NOT fixed
		// — mirrors position:relative semantics. Handled separately from
		// isTransformCB because fixed candidates must still propagate.
		isWillChangeAbsposCB := false
		if bla.style != nil && !isContainmentCB && !isTransformCB && !isPositioned {
			if bla.style.WillChangeEstablishesContainingBlock(false) {
				isWillChangeAbsposCB = true
			}
		}
		isRoot := bla.space.IsRootElement

		if isRoot {
			// Root element: resolve ALL OOF candidates (both absolute and fixed)
			// with ICB (viewport) dimensions.
			var icbInline, icbBlock float64
			if wdm.IsHorizontal() {
				icbInline = bla.ctx.ViewportWidth
				icbBlock = bla.ctx.ViewportHeight
			} else {
				icbInline = bla.ctx.ViewportHeight
				icbBlock = bla.ctx.ViewportWidth
			}
			oofPart := &OutOfFlowLayoutPart{
				ctx:                bla.ctx,
				containingBlockWDM: wdm,
				// ICB is the viewport: no border or padding, so padding-box == content-box.
				containingBlockSize:    LogicalSize{InlineSize: icbInline, BlockSize: icbBlock},
				containingBlockPadding: LogicalEdges{},
				geom:                   geom,
				resolvesFixed:          true,
			}
			oofPart.LayoutCandidates(regularCandidates, builder)
		} else if isContainmentCB || isTransformCB {
			// CSS Containment / CSS Transforms: containment, transforms, filters,
			// and will-change:transform/perspective/filter make this element a
			// containing block for ALL positioned descendants, including fixed.
			//
			// Per CSS 2.1 §10.3.7 / Blink's GetContainingBlockInfo():
			// CB size = padding-box = content + padding (borders excluded).
			oofPart := &OutOfFlowLayoutPart{
				ctx:                bla.ctx,
				containingBlockWDM: wdm,
				containingBlockSize: LogicalSize{
					InlineSize: contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd,
					BlockSize:  finalBlockSize + geom.Padding.BlockStart + geom.Padding.BlockEnd,
				},
				containingBlockPadding: geom.Padding,
				geom:                   geom,
				resolvesFixed:          true,
			}
			oofPart.LayoutCandidates(regularCandidates, builder)
		} else if isPositioned || isWillChangeAbsposCB {
			// Positioned element OR will-change:position-like CB: resolve
			// absolute candidates here, but propagate fixed candidates
			// upward toward the ICB. (Will Change §2.2: abspos-only CB
			// triggers mimic position:relative — fixed still escapes.)
			var absoluteCandidates, fixedCandidates []OutOfFlowCandidate
			for _, cand := range regularCandidates {
				if cand.IsFixedPosition {
					fixedCandidates = append(fixedCandidates, cand)
				} else {
					absoluteCandidates = append(absoluteCandidates, cand)
				}
			}
			// Phase 25 Cmt-3: when this CB is itself inside a block-fragmentation
			// context (a column flow, paged context, etc.), abspos descendants
			// can't be resolved here — their layout must wait until we reach the
			// outer fragmentation root, which will lay them out per fragmentainer
			// (per column). Promote each absolute candidate to a fragmentainer
			// descendant so the descendant payload bubbles up via
			// FragmentedOofData. Mirrors Blink's
			// `OutOfFlowLayoutPart::LayoutCandidates` promotion at
			// `out_of_flow_layout_part.cc:1158-1170` (the
			// `should_add_outer_fragmentainer_children_` branch). The static
			// position is already in this CB's content-box coords (it accumulated
			// upward via PropagateOOFCandidates); preserve as-is. The CB's
			// outgoing fragment isn't yet built, so leave Fragment=nil — the
			// parent's PropagateOOFFragmentainerDescendants will fill it in.
			if bla.space.HasBlockFragmentation && len(absoluteCandidates) > 0 {
				for _, cand := range absoluteCandidates {
					builder.AddOutOfFlowFragmentainerDescendant(LogicalOofNodeForFragmentation{
						Candidate:       cand,
						ContainingBlock: LogicalOofContainingBlock{
							// Fragment: nil — assigned by parent's propagator.
							// Offset: zero — CB origin within its own content-box.
						},
					})
				}
				absoluteCandidates = nil
			}
			if len(absoluteCandidates) > 0 {
				// Per CSS 2.1 §10.3.7 / Blink's GetContainingBlockInfo():
				// CB size = padding-box = content + padding (borders excluded).
				oofPart := &OutOfFlowLayoutPart{
					ctx:                bla.ctx,
					containingBlockWDM: wdm,
					containingBlockSize: LogicalSize{
						InlineSize: contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd,
						BlockSize:  finalBlockSize + geom.Padding.BlockStart + geom.Padding.BlockEnd,
					},
					containingBlockPadding: geom.Padding,
					geom:                   geom,
				}
				if extra := oofPart.LayoutCandidates(absoluteCandidates, builder); len(extra) > 0 {
					fixedCandidates = append(fixedCandidates, extra...)
				}
			}
			propagatedOOF = append(propagatedOOF, fixedCandidates...)
		} else {
			// Not positioned, not root: propagate ALL candidates upward.
			propagatedOOF = append(propagatedOOF, regularCandidates...)
		}
	}

	// LOU-365: promote collected parallel-flow continuations into the
	// unified reader. Placed AFTER Gates A/B on purpose: the parent's own
	// flow completed normally, so trailing-margin propagation and float
	// clearance must behave as on the natural path; only the outgoing
	// break token differs (it carries the children's continuations).
	// When a boundary branch also fired, merge — the parallel tokens
	// belong to earlier children, so they go first (DOM order).
	if len(pendingParallelFlowChildTokens) > 0 {
		pendingChildBreakTokens = append(pendingParallelFlowChildTokens, pendingChildBreakTokens...)
		if !pendingShouldBreakInside {
			pendingShouldBreakInside = true
			pendingHasSeenAllChildren = true
			pendingIsAtBlockEnd = true
		}
	}

	// Option-b step 6.4.c: apply DropChildren for the broken path,
	// just before Build(). Recorded by the frag-overflow record-phase
	// (pendingDropAtBlockOffsetEnabled gated on `fragSize > 0`).
	if pendingDropAtBlockOffsetEnabled {
		builder.DropChildrenAtOrPastBlockOffset(pendingDropAtBlockOffset)
	}

	result := builder.Build()

	// Option-b step 6.4.c: unified outgoing-break-token construction
	// (plan §3.6). Mirrors Blink's FinishFragmentation building the
	// outgoing break info from container_builder_ state
	// (frag_utils.cc:677-815). Subsumes:
	//   - the pre-6.4.c didBreakSelf reader (Phase 16.d.1 box self-clamp)
	//   - the 6.2 forced-break post-loop reader
	//   - the 6.4.b frag-overflow post-loop reader
	// Fires when the children loop recorded an in-flow break
	// (pendingShouldBreakInside) OR the late clamp self-broke
	// (didBreakSelf). When both fire (e.g. spanner-3 in 006), the
	// pending state takes precedence — consumed = pendingIntrinsicAtBreak
	// preserves the pre-6.4.c frag-overflow reader's emitted value.
	if pendingShouldBreakInside || didBreakSelf {
		prevConsumed := 0.0
		seqNum := 0
		if incomingBreakToken != nil {
			prevConsumed = incomingBreakToken.ConsumedBlockSize.Float64()
			seqNum = incomingBreakToken.SequenceNumber + 1
		}
		var childBreakTokens []*BlockBreakToken
		var hasSeenAllChildren bool
		var isAtBlockEnd bool
		consumed := finalBlockSize
		if pendingShouldBreakInside {
			childBreakTokens = pendingChildBreakTokens
			hasSeenAllChildren = pendingHasSeenAllChildren
			isAtBlockEnd = pendingIsAtBlockEnd
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
		// didBreakSelf on an inline-only block whose IFC completed without
		// overflow: signal HasSeenAllChildren so the continuation fragment
		// does not re-lay out the same inline content. Without this, an
		// inline-only block (e.g. `<div>block1</div>`) with CSS height taller
		// than its only line of text and taller than the column box self-
		// fragments at the column boundary, and the continuation column
		// re-renders the text at the top of the next column. Driver:
		// multicol-span-all-children-height-001.
		if didBreakSelf && inlineFullyPlaced {
			hasSeenAllChildren = true
		}
		// Option-b step 6.5.A — Site B setter (plan §1.9 / §3.3 row 4).
		// Blink's FinishFragmentation at fragmentation_utils.cc:735-739:
		// when the previous fragment was already at block-end
		// (`is_past_end`), carry IsAtBlockEnd forward onto the outgoing
		// token. louis14's step 3.5.B at the natural-path zero-clamp
		// covers the size half ("at-end → final=0"); 6.5.A adds the
		// missing tag half.
		if incomingBreakToken != nil && incomingBreakToken.IsAtBlockEnd {
			isAtBlockEnd = true
		}
		result.BreakToken = &BlockBreakToken{
			Node:               bla.node,
			ConsumedBlockSize:  layoutunit.FromFloat64Round(prevConsumed + consumed),
			SequenceNumber:     seqNum,
			ChildBreakTokens:   childBreakTokens,
			HasSeenAllChildren: hasSeenAllChildren,
			IsAtBlockEnd:       isAtBlockEnd,
		}
		// Carry the unpositioned outside ::marker into the break token: a
		// continuation that resumes before any baseline-bearing child claims
		// the marker must re-seed it (Blink BlockBreakToken::
		// has_unpositioned_list_marker_).
		if builder.GetUnpositionedListMarker().IsValid() {
			result.BreakToken.HasUnpositionedListMarker = true
		}
		if didBreakSelf {
			result.DidBreakSelf = true
			// LOU-365: surface the self-break shortage (Blink
			// FinishFragmentation → PropagateSpaceShortage,
			// fragmentation_utils.cc:798-800 @ a9f50e522efa).
			result.MinSpaceShortage = MinPositiveSpaceShortage(result.MinSpaceShortage, selfBreakShortage)
		}
		if pendingShouldBreakInside {
			if pendingBreakAppeal < BreakAppealPerfect {
				result.BreakAppeal = pendingBreakAppeal
			}
			result.MinSpaceShortage = MinPositiveSpaceShortage(result.MinSpaceShortage, pendingMinSpaceShortage)
			if pendingHasForcedBreak {
				result.HasForcedBreak = true
			}
			if pendingIntrinsicAtBreak > NewLogicalFragment(wdm, result.Fragment).BlockSize() {
				result.BlockSizeForFragmentation = pendingIntrinsicAtBreak
			}
		}
	}

	// Attach propagated OOF candidates for the parent to resolve.
	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	// CSS 2.1 §8.3.1: Propagate first child's margin for parent-child collapsing.
	if !propagatedTopMargin.IsEmpty() {
		result.PropagatedTopMargin = propagatedTopMargin
	}

	// CSS 2.1 §9.4.3: Compute position:relative offset during layout.
	// Stored on the fragment for paint-time application (not baked into positions).
	// Percentages resolve against the containing block's PHYSICAL dimensions:
	// left/right against physical width, top/bottom against physical height.
	//
	// The overconstrained resolution rule is "start wins over end" in logical
	// coordinates, following Blink's ComputeRelativeOffset (relative_utils.cc).
	//
	// position:sticky is NOT included — its offset is scroll-time, not
	// layout-time. At layout we leave RelativeOffset zero (normal-flow
	// position); the scroll-time path is deferred until scroll-based tests
	// appear.
	if bla.style != nil && bla.style.GetPosition() == css.PositionRelative {
		// Percentages resolve against the containing block's physical size.
		// Mirrors Blink's ComputeRelativeOffset, which reads AvailableSize —
		// but we use PercentageResolutionSize instead so that anonymous
		// auto-height wraps (e.g. block-in-inline splits) do not collapse the
		// block-axis CB to zero for their descendants.
		cbInline := bla.space.PercentageResolutionSize.InlineSize.Float64()
		if cbInline == Indefinite {
			cbInline = 0
		}
		cbBlock := bla.space.PercentageResolutionSize.BlockSize.Float64()
		if cbBlock == Indefinite {
			cbBlock = 0
		}
		physCB := ToPhysicalSize(LogicalSize{
			InlineSize: cbInline,
			BlockSize:  cbBlock,
		}, wdm.WM)
		offset := bla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
		result.Fragment.RelativeOffset = computeRelativeOffset(offset, wdm)
	}

	return result
}

// computeRelativeOffset computes the physical offset for position:relative.
//
// CSS Writing Modes §7.1 maps CSS 2.1 §9.4.3's overconstrained rules to
// logical axes. The "start wins" rule applies in the logical space:
//   - inline-start wins over inline-end (direction determines start)
//   - block-start wins over block-end (always)
//
// In horizontal writing modes:
//   - left/right are inline (direction-dependent: LTR→left wins, RTL→right wins)
//   - top/bottom are block (top = block-start, always wins)
//
// In vertical writing modes (vlr, vrl):
//   - left/right are block (block-start wins: vlr→left, vrl→right, always)
//   - top/bottom are inline (direction-dependent: LTR→top wins, RTL→bottom wins)
//
// In sideways-lr:
//   - inline direction is inverted (bottom-to-top for LTR)
//   - left/right are block (left = block-start, always wins)
//   - top/bottom are inline (LTR→bottom wins, RTL→top wins)
func computeRelativeOffset(offset css.PositionOffset, wdm WritingDirectionMode) geometry.PhysicalOffset {
	var dx, dy float64

	switch wdm.WM {
	case WritingModeHorizontalTB:
		// Inline axis = horizontal: direction determines which of left/right wins.
		if wdm.Dir == DirectionLTR {
			if offset.HasLeft {
				dx = offset.Left
			} else if offset.HasRight {
				dx = -offset.Right
			}
		} else {
			if offset.HasRight {
				dx = -offset.Right
			} else if offset.HasLeft {
				dx = offset.Left
			}
		}
		// Block axis = vertical: top = block-start, always wins.
		if offset.HasTop {
			dy = offset.Top
		} else if offset.HasBottom {
			dy = -offset.Bottom
		}

	case WritingModeVerticalLR:
		// Block axis = horizontal: left = block-start in vlr, always wins.
		if offset.HasLeft {
			dx = offset.Left
		} else if offset.HasRight {
			dx = -offset.Right
		}
		// Inline axis = vertical: direction determines which of top/bottom wins.
		if wdm.Dir == DirectionLTR {
			// LTR: top = inline-start.
			if offset.HasTop {
				dy = offset.Top
			} else if offset.HasBottom {
				dy = -offset.Bottom
			}
		} else {
			// RTL: bottom = inline-start.
			if offset.HasBottom {
				dy = -offset.Bottom
			} else if offset.HasTop {
				dy = offset.Top
			}
		}

	case WritingModeVerticalRL, WritingModeSidewaysRL:
		// Block axis = horizontal: right = block-start in vrl, always wins.
		if offset.HasRight {
			dx = -offset.Right
		} else if offset.HasLeft {
			dx = offset.Left
		}
		// Inline axis = vertical: direction determines which of top/bottom wins.
		if wdm.Dir == DirectionLTR {
			// LTR: top = inline-start.
			if offset.HasTop {
				dy = offset.Top
			} else if offset.HasBottom {
				dy = -offset.Bottom
			}
		} else {
			// RTL: bottom = inline-start.
			if offset.HasBottom {
				dy = -offset.Bottom
			} else if offset.HasTop {
				dy = offset.Top
			}
		}

	case WritingModeSidewaysLR:
		// Block axis = horizontal: left = block-start, always wins.
		if offset.HasLeft {
			dx = offset.Left
		} else if offset.HasRight {
			dx = -offset.Right
		}
		// Inline axis = vertical but inverted (bottom-to-top for LTR).
		if wdm.Dir == DirectionLTR {
			// LTR: bottom = inline-start (inverted).
			if offset.HasBottom {
				dy = -offset.Bottom
			} else if offset.HasTop {
				dy = offset.Top
			}
		} else {
			// RTL: top = inline-start (inverted).
			if offset.HasTop {
				dy = offset.Top
			} else if offset.HasBottom {
				dy = -offset.Bottom
			}
		}
	}

	return geometry.PhysicalOffsetFromF64Round(dx, dy)
}

// inheritPropagatedOOF adjusts and adopts OOF candidates from a child result.
// Converts static positions from the child's content-box coordinates to this
// element's content-box coordinates.
func (bla *BlockLayoutAlgorithm) inheritPropagatedOOF(
	childResult *LayoutResult,
	childStyle *css.Style,
	parentWDM WritingDirectionMode,
	childInlineOff, childBlockOff float64,
	builder *BoxFragmentBuilder,
) {
	PropagateOOFCandidates(childResult, childStyle, parentWDM, childInlineOff, childBlockOff, builder)
}

// PropagateOOFCandidates adjusts and adopts OOF candidates from a child result.
// Converts static positions from the child's content-box coordinates to the
// parent's content-box coordinates. Shared by block, table, and other layout algorithms.
//
// When the child's writing mode differs from the parent's, the static position
// is converted from child-logical to physical, then from physical to
// parent-logical before adding the parent-logical offset adjustments.
//
// Mirrors Blink's propagation of OutOfFlowPositionedCandidates through the tree
// and its cross-writing-mode static position conversion in
// OutOfFlowLayoutPart::PropagateOOFPositionedInfo.
func PropagateOOFCandidates(
	childResult *LayoutResult,
	childStyle *css.Style,
	parentWDM WritingDirectionMode,
	childInlineOff, childBlockOff float64,
	builder *BoxFragmentBuilder,
) {
	// Compute the child's WDM first so we can use it for both BP computation
	// and cross-WM static position conversion.
	childWDM := NewWritingDirectionMode(childStyle)

	// Compute child's border+padding in the CHILD's logical axes (not parent's),
	// then convert to physical, then to parent-logical coordinates.
	//
	// The bug with using parentWDM here: when child and parent have orthogonal or
	// parallel-but-different writing modes, interpreting the child's physical borders
	// using the parent's WDM gives wrong logical edge assignments.
	//
	// Mirrors Blink's use of child's own WDM for border/padding in
	// OutOfFlowLayoutPart::PropagateOOFPositionedInfo.
	childGeomBP := ComputeFragmentGeometry(childStyle, childWDM)
	childBPLogical := LogicalEdges{
		InlineStart: childGeomBP.Border.InlineStart + childGeomBP.Padding.InlineStart,
		InlineEnd:   childGeomBP.Border.InlineEnd + childGeomBP.Padding.InlineEnd,
		BlockStart:  childGeomBP.Border.BlockStart + childGeomBP.Padding.BlockStart,
		BlockEnd:    childGeomBP.Border.BlockEnd + childGeomBP.Padding.BlockEnd,
	}
	// Convert child-logical BP edges to physical, then to parent-logical.
	physBPEdges := ToPhysicalEdges(childBPLogical, childWDM)
	parentLogicalBP := ToLogicalEdges(physBPEdges, parentWDM)

	// If the child has a position:relative/sticky RelativeOffset, the child is
	// visually displaced from its normal-flow position. Propagated OOF
	// descendants whose CB is the parent or higher (e.g. fixed descendants of a
	// relative ancestor) anchor at the child's VISUAL position per the
	// hypothetical-box algorithm. Mirrors Blink's
	// OutOfFlowLayoutPart::PropagateOOFPositionedInfo accumulating the
	// container's relative offset into the descendant's static position.
	relOffPhys := PhysicalOffset{
		X: childResult.Fragment.RelativeOffset.LeftF64(),
		Y: childResult.Fragment.RelativeOffset.TopF64(),
	}
	relOffsetLog := NewConverter(parentWDM, PhysicalSize{}).
		ToLogicalOffset(relOffPhys, PhysicalSize{})

	blockAdj := childBlockOff + parentLogicalBP.BlockStart + relOffsetLog.BlockOffset
	inlineAdj := childInlineOff + parentLogicalBP.InlineStart + relOffsetLog.InlineOffset

	// Detect when the child's writing direction differs from the parent's.
	// This includes both orthogonal writing modes (e.g. HTB inside VRL) and
	// same-axis writing modes with different directions (e.g. VLR-LTR inside
	// VLR-RTL, or VLR inside VRL). In all these cases the static position must be re-expressed in
	// the parent's logical coordinate system via a physical round-trip.
	needsConversion := childWDM.WM != parentWDM.WM || childWDM.Dir != parentWDM.Dir

	// Pre-compute the child's content-box physical size for coordinate
	// conversion. The static position is measured within this box.
	var childContentPhys PhysicalSize
	if needsConversion {
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

		if needsConversion {
			// Convert static position: child-logical → physical → parent-logical.
			// The container for both conversions is the child's content-box
			// (the space within which the static position was accumulated).
			physSP := adj.StaticPosition.ConvertToPhysical(childWDM, childContentPhys)
			adj.StaticPosition = physSP.ConvertToLogical(parentWDM, childContentPhys)
		}

		adj.StaticPosition.Offset.BlockOffset += blockAdj
		adj.StaticPosition.Offset.InlineOffset += inlineAdj
		builder.AddOutOfFlowCandidate(adj)
	}

	// Phase 25 Cmt-2: also forward any OOF fragmentainer descendants the
	// child carries on its outgoing fragment (an inner multicol that
	// deferred its abspos descendants to the outer fragmentation root).
	// blockAdj/inlineAdj already include the child's offset, border-padding,
	// and relative offset — pass them as the combined translation. CB
	// defaults are nil here; descendants entered with their CB resolved at
	// the deferral site, and the drain pipeline (Cmt-3) supplies defaults
	// for any that didn't.
	if childResult.Fragment != nil && childResult.Fragment.FragmentedOofData != nil {
		builder.PropagateOOFFragmentainerDescendants(
			childResult.Fragment,
			LogicalOffset{InlineOffset: inlineAdj, BlockOffset: blockAdj},
			LogicalOffset{},
			layoutunit.LayoutUnit{},
			nil,
			nil,
		)
	}
}

// layoutFloat handles layout and positioning of a float child within the
// block formatting context. CSS 2.1 §9.5.
func (bla *BlockLayoutAlgorithm) layoutFloat(
	child *LayoutInputNode,
	childStyle *css.Style,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	availableBlock float64,
	blockCursor float64,
	prevMarginStrut *MarginStrut,
	es *ExclusionSpace,
	builder *BoxFragmentBuilder,
	outES **ExclusionSpace,
	bfcBlockOrigin float64,
) {
	childWDM := NewWritingDirectionMode(childStyle)
	// Resolve float margins in the parent's coordinates for positioning.
	childMargins := ResolveMargins(childStyle, parentWDM, contentInlineSize)

	// Floats establish a new BFC. buildFloatSpace is shared by the initial
	// (tentative-position) layout and the relayout at the float's final
	// position below.
	//
	// LOU-365: non-monolithic floats receive the fragmentainer. In Blink,
	// floats participate in block fragmentation through the same
	// SetupSpaceBuilderForFragmentation as in-flow children, with the
	// same monolithic gate (fragmentation_utils.cc:321-345 @
	// a9f50e522efa). Louis14 does not implement float SLICING (a float
	// whose content overflows the fragmentainer keeps one whole
	// fragment), but an inner fragmentation context inside the float —
	// e.g. a floated multicol with column-fill:auto — must see the outer
	// fragmentainer's remaining space to clamp its column block-size
	// (Blink ConstrainColumnBlockSize; driver:
	// nested-floated-multicol-with-monolithic-child). The balancing-pass
	// flag keeps measure-pass semantics consistent with the in-flow site.
	floatIsMonolithic := child.IsMonolithic(parentWDM.WM != childWDM.WM)
	floatHasFragmentation := bla.space.HasBlockFragmentation && !floatIsMonolithic
	buildFloatSpace := func(fragOffset float64) ConstraintSpace {
		b := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
			SetOrthogonalFallbackInlineSize(
				orthogonalFallbackSize(childWDM, bla.ctx)).
			SetOrthogonalFallbackBlockSize(
				bla.space.OrthogonalFallbackBlockSize).
			SetAvailableSize(LogicalSize{
				InlineSize: contentInlineSize,
				BlockSize:  availableBlock,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: contentInlineSize,
				BlockSize:  availableBlock, // resolves height:100% against container's explicit height
			}).
			SetPercentageResolutionInlineSize(contentInlineSize)
		if floatHasFragmentation {
			b = b.SetHasBlockFragmentation(true).
				SetFragmentainerBlockSize(bla.space.FragmentainerBlockSize).
				SetFragmentainerOffset(fragOffset).
				SetBlockFragmentationType(bla.space.BlockFragmentationType).
				SetIsInitialColumnBalancingPass(bla.space.IsInitialColumnBalancingPass).
				SetIsInsideBalancedColumns(bla.space.IsInsideBalancedColumns)
		}
		return b.Build()
	}

	// Layout the float's contents at its tentative block position.
	tentativeFragOffset := bla.space.FragmentainerOffset + blockCursor +
		prevMarginStrut.Resolve() + childMargins.BlockStart
	childResult := layoutElement(bla.ctx, child, buildFloatSpace(tentativeFragOffset))
	childLogical := NewLogicalFragment(parentWDM, childResult.Fragment)

	// LOU-365 (CodeRabbit 3514362226): parent-side monolithic tag for
	// writing-mode roots the float's own layout cannot see (parallel
	// flips) — same supplement as the in-flow child walk.
	if floatIsMonolithic && childResult.Fragment != nil {
		childResult.Fragment.IsMonolithic = true
	}

	// Compute the float's margin-box sizes.
	floatInlineSize := childMargins.InlineSum() + childLogical.InlineSize()
	floatBlockSize := childMargins.BlockSum() + childLogical.BlockSize()

	// Resolve the collapsed margin at the current position.
	// Float positioning uses BFC-relative coordinates since the exclusion space
	// stores floats in BFC coordinates.
	collapsedMargin := prevMarginStrut.Resolve()
	floatBlockStart := bfcBlockOrigin + blockCursor + collapsedMargin

	// CSS 2.1 §9.5.2: The clear property applies to floats too.
	// If this float has clear, advance past matching floats before positioning.
	clearType := childStyle.GetClear()
	if clearType != css.ClearNone {
		clearedBlock := es.ClearanceOffset(clearType, layoutunit.FromFloat64Round(floatBlockStart), parentWDM).Float64()
		if clearedBlock > floatBlockStart {
			floatBlockStart = clearedBlock
		}
	}

	// Find where the float fits (CSS 2.1 §9.5.1 Rule 6: as high as possible).
	floatSide := childStyle.GetFloat()
	floatBlockOffset := es.FindFloatPosition(floatSide, floatInlineSize, floatBlockSize,
		contentInlineSize, floatBlockStart)

	// LOU-365 (CodeRabbit 3514362236): clear / exclusion repositioning can
	// move the float past its tentative position, and a stale fragmentainer
	// offset lets fragmentation contexts inside the float see too much
	// remaining space. Re-lay the float once at its FINAL block position —
	// mirrors Blink's PositionFloat, which performs the fragmentation-aware
	// layout only after the float's BFC offset is resolved. The position
	// itself is kept from the first pass: relayout at a deeper offset can
	// only shrink the fragment, which cannot invalidate the fit.
	if floatHasFragmentation {
		finalFragOffset := bla.space.FragmentainerOffset +
			(floatBlockOffset - bfcBlockOrigin) + childMargins.BlockStart
		if finalFragOffset != tentativeFragOffset {
			childResult = layoutElement(bla.ctx, child, buildFloatSpace(finalFragOffset))
			childLogical = NewLogicalFragment(parentWDM, childResult.Fragment)
			floatInlineSize = childMargins.InlineSum() + childLogical.InlineSize()
			floatBlockSize = childMargins.BlockSum() + childLogical.BlockSize()
		}
	}

	// LOU-365: during the initial column-balancing pass, an unbreakable
	// float floors the column block-size at its MARGIN box — float
	// margins do not break or truncate (Blink BlockSizeForFragmentation,
	// fragmentation_utils.cc:1545-1583 @ a9f50e522efa: "Margins on floats
	// do not break or truncate" → block_size += margins.BlockSum()). In
	// Blink the floor flows through BreakBeforeChildIfNeeded
	// (:1105-1113); louis14's block-path floats bypass that walk, so
	// propagate here. The carrier propagation mirrors the in-flow child
	// site (Blink box_fragment_builder.cc:579-582).
	// Driver: multicol-fill-balance-037.
	if bla.space.IsInitialColumnBalancingPass {
		if ShouldAvoidBreakInside(bla.space, childResult) {
			unbreakable := CalculateUnbreakableBlockSize(bla.space, childResult, 0) +
				childMargins.BlockSum()
			builder.PropagateTallestUnbreakableBlockSize(unbreakable)
		}
		builder.PropagateTallestUnbreakableBlockSize(childResult.TallestUnbreakableBlockSize)
	}

	// CSS float:left/right are physical, but the positioning logic uses logical
	// (inline-start/end) coordinates. In RTL, swap: physical left = inline-end,
	// physical right = inline-start. The margins are already in the parent's
	// logical frame so they align correctly after the swap.
	logicalSide := floatSide
	if parentWDM.Dir == DirectionRTL {
		if floatSide == css.FloatLeft {
			logicalSide = css.FloatRight
		} else {
			logicalSide = css.FloatLeft
		}
	}

	// Compute the float's inline position.
	var floatInlineOffset float64
	if logicalSide == css.FloatLeft {
		startOff, _ := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
		floatInlineOffset = startOff + childMargins.InlineStart
	} else {
		_, endOff := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
		floatInlineOffset = contentInlineSize - endOff - childMargins.InlineEnd - childLogical.InlineSize()
	}

	// Add the float fragment to the builder. Convert the BFC-relative
	// block offset to local coordinates for fragment positioning.
	localFloatBlockOffset := floatBlockOffset - bfcBlockOrigin
	builder.AddChild(childResult.Fragment, LogicalOffset{
		InlineOffset: floatInlineOffset,
		BlockOffset:  localFloatBlockOffset + childMargins.BlockStart,
	})

	// Add an exclusion for this float. The exclusion uses BFC-relative
	// coordinates so it works correctly throughout the BFC.
	exclusion := Exclusion{
		InlineOffset: floatInlineOffset - childMargins.InlineStart,
		BlockOffset:  floatBlockOffset, // BFC-relative
		InlineSize:   floatInlineSize,
		BlockSize:    floatBlockSize,
		Side:         PhysicalFloatToExclusionSide(floatSide, parentWDM),
	}
	*outES = es.Add(exclusion)
}

// resumeTokenIndexRange returns the indices of the first and last children
// that carry an incoming continuation token. Mirrors the token walk in
// Blink's BlockChildIterator (block_child_iterator.cc:80-95 @ a9f50e522efa):
// resumption visits the tokened children in order; everything before the
// first is complete.
func resumeTokenIndexRange(
	children []*LayoutInputNode,
	tokens map[*LayoutInputNode]*BlockBreakToken,
) (first, last int) {
	first, last = -1, -1
	for ci, ch := range children {
		if _, ok := tokens[ch]; ok {
			if first < 0 {
				first = ci
			}
			last = ci
		}
	}
	return first, last
}

// resumeSkipsChild implements the resumption skip rule (BlockChildIterator,
// block_child_iterator.cc:80-95 @ a9f50e522efa): children before the first
// tokened child are complete; within the tokened range, only children with
// their own continuation token re-enter layout; after the last token,
// siblings lay out fresh unless the container has already seen all children.
func resumeSkipsChild(
	childIdx int,
	child *LayoutInputNode,
	resumeChildIdx, resumeLastTokenIdx int,
	resumeChildTokens map[*LayoutInputNode]*BlockBreakToken,
	incomingBreakToken *BlockBreakToken,
) bool {
	if resumeChildIdx < 0 {
		return false
	}
	if childIdx < resumeChildIdx {
		return true
	}
	if _, hasToken := resumeChildTokens[child]; hasToken {
		return false
	}
	// Untokened child in the resume range: completed earlier (between
	// tokened children), or — under HasSeenAllChildren — fully seen
	// before any token.
	return childIdx <= resumeLastTokenIdx ||
		(incomingBreakToken != nil && incomingBreakToken.HasSeenAllChildren)
}

// isPureParallelFlowResume reports whether a zero-height inside-broken child
// is a PURE parallel-flow continuation that must not be treated as
// zero-progress: at-block-end (its box is done, content continues), no
// column-spanner involvement (the spanner protocol owns those stops), and
// the container has seen all children (so continuing can only reach tokened
// siblings, never lay a fresh one into a column owed to this continuation).
// Mirrors Blink's MovePastBreakpoint must_break_before (checks
// GetColumnSpannerPath() first, then !IsAtBlockEnd —
// fragmentation_utils.cc:1178-1186 @ a9f50e522efa) combined with
// BlockChildIterator's seen-all iteration (block_child_iterator.cc:80-95).
// Driver for the seen-all narrowing: the pre-spanner columnization of
// multicol-span-all-children-height-012's zero-height wrapper relies on
// stopping when unseen siblings are still pending.
func isPureParallelFlowResume(childResult *LayoutResult, incomingBreakToken *BlockBreakToken) bool {
	return IsParallelFlowContinuation(childResult.BreakToken) &&
		!childResult.BreakToken.IsCausedByColumnSpanner &&
		childResult.ColumnSpannerPath == nil &&
		incomingBreakToken != nil && incomingBreakToken.HasSeenAllChildren
}

// tryLayoutNestedDocument checks if this element is an iframe/object with a
// document source. If so, fetches + lays out the nested document and returns
// the root fragment and its X offset within the iframe viewport. Returns nil if not applicable.
func (bla *BlockLayoutAlgorithm) tryLayoutNestedDocument(contentInlineSize float64, wdm WritingDirectionMode, geom FragmentGeometry) *nestedDocFragment {
	if bla.node.DOMNode == nil {
		return nil
	}
	dom := bla.node.DOMNode
	var uri string
	var htmlContent string

	switch dom.TagName {
	case "iframe":
		// srcdoc takes priority over src per HTML spec.
		if srcdoc, ok := dom.GetAttribute("srcdoc"); ok && srcdoc != "" {
			htmlContent = srcdoc
			uri = ""
		} else {
			uri, _ = dom.GetAttribute("src")
		}
	case "object":
		if dataType, _ := dom.GetAttribute("type"); dataType == "text/html" || dataType == "" {
			uri, _ = dom.GetAttribute("data")
		}
	default:
		return nil
	}

	// If we have inline srcdoc content, skip the fetcher entirely.
	if htmlContent == "" {
		if uri == "" || bla.ctx.DocumentFetcher == nil {
			return nil
		}
		var err error
		htmlContent, err = bla.ctx.DocumentFetcher(uri)
		if err != nil {
			return nil
		}
	}

	// Compute the content-box physical size for the nested viewport.
	_, blockSize := ComputeReplacedSize(bla.ctx, bla.node, bla.style, bla.space)
	physSize := ToPhysicalSize(LogicalSize{
		InlineSize: contentInlineSize,
		BlockSize:  blockSize,
	}, wdm.WM)

	res := layoutNestedDocument(bla.ctx, htmlContent, physSize.Width, physSize.Height, uri)
	if res == nil {
		return nil
	}
	// Retain the parsed nested document on the iframe/object DOM node so that
	// JS can access iframe.contentDocument after layout completes.
	if res.Doc != nil {
		dom.NestedDocument = res.Doc
	}
	return &nestedDocFragment{fragment: res.Result.Fragment, rootOffsetX: res.RootOffsetX}
}

// layoutElement dispatches to the appropriate layout algorithm based on
// the element's display type.
func layoutElement(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *LayoutResult {
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
		if isMulticolContainer(style) {
			return NewMulticolLayoutAlgorithm(ctx, node, space).Layout()
		}
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayTable, css.DisplayInlineTable:
		return NewTableLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayTableRow, css.DisplayTableRowGroup, css.DisplayTableHeaderGroup,
		css.DisplayTableFooterGroup, css.DisplayTableCell, css.DisplayTableCaption:
		// Table internals laid out by their parent TableLayoutAlgorithm.
		// If encountered at top level, treat as block.
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
func reQueryFloatsAfterDrop(exclusionSpace *ExclusionSpace, dropBlockBfc, blockExtent, childAvailableInline, childMarginInlineSum float64) (floatStartOff, floatEndOff, childInlineForSpace float64) {
	floatStartOff, floatEndOff = exclusionSpace.FindAvailableInlineSize(dropBlockBfc, blockExtent, childAvailableInline)
	childInlineForSpace = childAvailableInline - childMarginInlineSum - floatStartOff - floatEndOff
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

	return false
}

// hasPaintIsolation returns true if the style has any property that produces
// visible paint output independently of the box's content — filter,
// opacity<1, transform, mask, clip-path, mix-blend-mode, isolation:isolate,
// backdrop-filter. Such boxes must NOT be dropped by margin collapse-through
// (the `collapseThrough` branch in BlockLayoutAlgorithm.Layout, which
// `continue`s without appending the child fragment) because their paint
// depends only on the box itself, not its descendants.
//
// Mirrors the property set tested in Blink's
// `ComputedStyle::HasPropertyThatCreatesStackingContext` (computed_style.cc
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f), restricted to those that
// produce pixels (z-index, will-change auto-layer creation, etc. are
// excluded — they create a stacking context but don't paint independently).
func hasPaintIsolation(style *css.Style) bool {
	if style == nil {
		return false
	}
	if len(style.GetFilter()) > 0 {
		return true
	}
	if len(style.GetBackdropFilter()) > 0 {
		return true
	}
	if style.GetOpacity() < 1.0 {
		return true
	}
	if len(style.GetTransforms()) > 0 {
		return true
	}
	if cp := style.GetClipPath(); cp != nil && cp.Type != css.ClipPathNone {
		return true
	}
	if m := style.GetMaskImage(); m != "" && m != "none" {
		return true
	}
	if bm := style.GetMixBlendMode(); bm != css.MixBlendModeNormal && bm != "" {
		return true
	}
	if iso, ok := style.Get("isolation"); ok && iso == "isolate" {
		return true
	}
	return false
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
	d := style.GetDisplay()
	switch d {
	case css.DisplayTableCell, css.DisplayTableCaption,
		css.DisplayTableRow, css.DisplayTableHeaderGroup,
		css.DisplayTableFooterGroup, css.DisplayTableRowGroup:
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
	builder *BoxFragmentBuilder, child *PhysicalFragment, childResult *LayoutResult,
	childBlockOffset float64) float64 {
	marker := builder.GetUnpositionedListMarker()
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
		baseline, childBlockOffset, builder)
	if contentPush > 0 {
		// The marker's ascent is taller than the content's: Blink pushes the
		// content child down (*block_offset -= baseline_adjust). The content
		// child was already added to the builder, so re-offset it in place;
		// the marker was added at childBlockOffset (its block offset is correct
		// relative to the content's pre-push position, which is also where the
		// content's baseline now sits after the push).
		builder.ShiftChildBlockOffset(child, contentPush)
	}
	builder.ClearUnpositionedListMarker()
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
	builder *BoxFragmentBuilder, intrinsicBlockSize *float64) {
	marker := builder.GetUnpositionedListMarker()
	if !marker.IsValid() {
		return
	}
	markerResult := marker.Layout(bla.ctx, bla.space)
	if markerResult == nil {
		return
	}
	marker.AddToBoxWithoutLineBoxes(bla.ctx, markerResult, builder, intrinsicBlockSize)
	builder.ClearUnpositionedListMarker()
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
