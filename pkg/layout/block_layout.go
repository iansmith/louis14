package layout

import (
	"louis14/pkg/css"
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

// Layout performs block layout and returns the result.
func (bla *BlockLayoutAlgorithm) Layout() *LayoutResult {
	wdm := bla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(bla.ctx, bla.node, bla.style, wdm, bla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(bla.node)

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

	// Replaced elements (img, etc.) with auto block-size: derive from aspect ratio.
	// CSS 2.1 §10.6.2: if height is auto and there is an intrinsic ratio, use it.
	if !hasExplicitBlock && bla.node.DOMNode != nil && isReplacedElement(bla.node.DOMNode) {
		_, blockSize := ComputeReplacedSize(bla.ctx, bla.node, bla.style, bla.space)
		if blockSize > 0 {
			explicitBlockSize = blockSize
			hasExplicitBlock = true
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

	// Lay out children in the block direction.
	// CSS 2.1 §9.2.1.1: a block container has either all block-level or
	// all inline-level children.
	blockCursor := 0.0 // current block position within content box
	var prevMarginStrut MarginStrut

	// CSS 2.1 §8.3.1: Parent-child top margin collapsing.
	// When a block has no block-start border/padding and isn't a new BFC,
	// the first child's margin propagates upward.
	canPropagateTop := !bla.space.IsNewFormattingContext &&
		geom.Border.BlockStart == 0 && geom.Padding.BlockStart == 0
	firstNonEmptyChild := true
	var propagatedTopMargin MarginStrut

	var firstLineAscent float64
	var lastChildBaseline float64     // Baseline of the last in-flow block child.
	var lastChildBlockOffset float64  // Block offset of the last in-flow block child.
	hasLastChildBaseline := false

	// Iframe/object with a document source: lay out the nested document
	// instead of this element's DOM children.
	if nestedFrag := bla.tryLayoutNestedDocument(contentInlineSize, wdm, geom); nestedFrag != nil {
		builder.AddChild(nestedFrag, LogicalOffset{})
	} else if hasOnlyInlineChildren(bla.node) {
		// Inline formatting context: text nodes and inline-level children.
		prevES := exclusionSpace
		var inlineAscent, lastBaselineOff float64
		blockCursor, exclusionSpace, inlineAscent, lastBaselineOff = bla.layoutInlineChildren(wdm, contentInlineSize, exclusionSpace, builder)
		if exclusionSpace != prevES && bla.space.IsNewFormattingContext {
			hasOwnFloats = true
		}
		firstLineAscent = inlineAscent
		// Track the last line's baseline offset for inline-block alignment.
		lastChildBaseline = lastBaselineOff
		lastChildBlockOffset = 0 // Already included in lastBaselineOff.
		hasLastChildBaseline = lastBaselineOff > 0
		firstNonEmptyChild = false // inline content is "content"
	} else {
		// Block formatting context: block-level children.
		for _, child := range bla.node.Children() {
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
				// For block-level OOF children, the static inline offset is at inline-start (0)
				// and the static block offset is at the current block cursor position.
				// Edge annotations are both Start (the default for block-level OOF).
				staticBlockOffset := blockCursor + prevMarginStrut.Resolve()
				builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
					Node: child,
					StaticPosition: LogicalStaticPosition{
						Offset:     LogicalOffset{InlineOffset: 0, BlockOffset: staticBlockOffset},
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
					blockCursor, &prevMarginStrut, exclusionSpace, builder, &exclusionSpace)
				// Only BFC roots extend auto block-size to clear floats
				// (CSS 2.1 §10.6.7). Non-BFC parents let floats overflow;
				// hasOwnFloats is already true for BFC roots (initialized
				// from IsNewFormattingContext).

				continue
			}

			// Handle clear property.
			// CSS 2.1 §8.3.1: clearance prevents parent-child margin collapsing.
			hasClearance := false
			clearType := childStyle.GetClear()
			if clearType != css.ClearNone {
				clearedBlock := exclusionSpace.ClearanceOffset(clearType, blockCursor)
				if clearedBlock > blockCursor {
					blockCursor = clearedBlock
					prevMarginStrut = MarginStrut{} // Clear resets margin collapsing.
					hasClearance = true
				}
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

			// Compute available inline for this child, accounting for floats.
			floatStartOff, floatEndOff := exclusionSpace.FindAvailableInlineSize(blockCursor, 0, childAvailableInline)
			childInlineForSpace := childAvailableInline - childMargins.InlineSum() - floatStartOff - floatEndOff
			if childInlineForSpace < 0 {
				childInlineForSpace = 0
			}

			// Build constraint space for this child.
			// CSS Writing Modes §4.3: a block container with a different
			// writing-mode than its parent establishes a new BFC.
			isChildNewFC := createsFormattingContext(childStyle) ||
				wdm.WM != childWDM.WM
			blockForChild := childAvailableBlock
			if wdm.IsOrthogonalTo(childWDM) {
				blockForChild = orthogonalAvailableBlock
			}
			childSpace := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
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
					BlockSize:  explicitBlockSize, // 0 if auto
				}).
				SetExclusionSpace(exclusionSpace).
				Build()

			// Recursively lay out the child.
			childResult := layoutElement(bla.ctx, child, childSpace)

			// Step 1: Append child's block-start margin to the strut.
			prevMarginStrut.Append(childMargins.BlockStart)

			// Step 2: Include any propagated top margin from the child
			// (recursive parent-child collapsing).
			if !childResult.PropagatedTopMargin.IsEmpty() {
				prevMarginStrut.AppendStrut(childResult.PropagatedTopMargin)
			}

			// Step 3: Check if margins collapse through this element.
			// CSS 2.1 §8.3.1: An element's margins collapse through it if
			// it has no height, no border, no padding, does not contain a
			// line box, and all of its in-flow children's margins (if any)
			// are collapsed. We approximate this by checking that the
			// element has no fragment children (no content).
			childLogical := NewLogicalFragment(wdm, childResult.Fragment)
			childBlockSize := childLogical.BlockSize()
			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			collapseThrough := childBlockSize == 0 &&
				len(childResult.Fragment.Children) == 0 &&
				childGeom.Border.BlockStart == 0 && childGeom.Border.BlockEnd == 0 &&
				childGeom.Padding.BlockStart == 0 && childGeom.Padding.BlockEnd == 0

			if collapseThrough {
				// Margins collapse through: append block-end margin and continue
				// without resolving or advancing the cursor.
				// Pick up any propagated OOF candidates before continuing.
				if len(childResult.PropagatedOOFCandidates) > 0 {
					approxBlock := blockCursor + prevMarginStrut.Resolve()
					bla.inheritPropagatedOOF(childResult, childStyle, wdm,
						0, approxBlock, builder)
				}
				prevMarginStrut.Append(childMargins.BlockEnd)
				continue
			}

			// Step 4: Position child in the inline direction.
			childInlineOffset := childMargins.InlineStart + floatStartOff

			// Step 5: Handle parent-child top margin collapsing.
			// CSS 2.1 §8.3.1: parent-child collapsing requires that the
			// child has no clearance.
			var actualChildBlockOff float64
			if firstNonEmptyChild && canPropagateTop && !hasClearance {
				// Propagate the accumulated margin strut upward.
				propagatedTopMargin = prevMarginStrut
				actualChildBlockOff = 0
				// Position child at offset 0 (margin moves outside parent).
				builder.AddChild(childResult.Fragment, LogicalOffset{
					InlineOffset: childInlineOffset,
					BlockOffset:  0,
				})
				blockCursor = childBlockSize
			} else {
				// Step 6: Normal margin resolution.
				collapsedMargin := prevMarginStrut.Resolve()
				actualChildBlockOff = blockCursor + collapsedMargin
				builder.AddChild(childResult.Fragment, LogicalOffset{
					InlineOffset: childInlineOffset,
					BlockOffset:  actualChildBlockOff,
				})
				blockCursor = actualChildBlockOff + childBlockSize
			}

			// Inherit propagated OOF candidates from child.
			// Non-positioned children propagate their abspos descendants
			// upward for resolution by the containing block (this element
			// or a higher ancestor).
			if len(childResult.PropagatedOOFCandidates) > 0 {
				bla.inheritPropagatedOOF(childResult, childStyle, wdm,
					childInlineOffset, actualChildBlockOff, builder)
			}

			firstNonEmptyChild = false

			// Track the last in-flow block child's baseline for
			// CSS 2.1 §10.8.1 inline-block baseline propagation.
			// Use LastBaseline (last line box) if available, else Baseline.
			lb := childResult.LastBaseline
			if lb <= 0 {
				lb = childResult.Baseline
			}
			if lb > 0 {
				lastChildBaseline = lb
				lastChildBlockOffset = actualChildBlockOff
				hasLastChildBaseline = true
			}

			// Reset margin strut to the child's block-end margin.
			prevMarginStrut = childResult.EndMarginStrut
			prevMarginStrut.Append(childMargins.BlockEnd)
		}
	}

	// CSS 2.1 §8.3.1: The bottom margin of an in-flow block box with a
	// 'height' of 'auto' collapses with its last child's bottom margin if
	// the box has no bottom border and no bottom padding. When block-size
	// is explicit (not auto), OR there is border/padding at block-end,
	// the trailing margin does NOT propagate out as EndMarginStrut.
	// We consume it without extending the auto height.
	canPropagateBottom := !bla.space.IsNewFormattingContext &&
		!hasExplicitBlock &&
		geom.Border.BlockEnd == 0 && geom.Padding.BlockEnd == 0

	if bla.space.IsNewFormattingContext && !prevMarginStrut.IsEmpty() {
		// BFC roots: trailing margins don't propagate out, and they
		// extend the auto height (CSS 2.1 §10.6.7).
		blockCursor += prevMarginStrut.Resolve()
		prevMarginStrut = MarginStrut{} // consumed
	} else if !canPropagateBottom && !prevMarginStrut.IsEmpty() {
		// Non-BFC with explicit block-size or block-end border/padding:
		// margins don't propagate (CSS 2.1 §8.3.1). Consume without
		// extending auto height.
		prevMarginStrut = MarginStrut{}
	}

	// CSS 2.1 §10.6.7: For elements that own floats (BFC roots or elements
	// that contain their own floats), auto block-size extends to clear them.
	// Elements that only inherit floats from a parent BFC do not extend.
	if !hasExplicitBlock && hasOwnFloats {
		clearedBlock := exclusionSpace.ClearanceOffset(css.ClearBoth, blockCursor)
		if clearedBlock > blockCursor {
			blockCursor = clearedBlock
		}
	}
	// Compute final block-size.
	intrinsicBlockSize := blockCursor
	finalBlockSize := intrinsicBlockSize
	if hasExplicitBlock {
		finalBlockSize = explicitBlockSize
	}

	// Apply min/max block-size constraints (CSS 2.1 §10.7).
	minBlock := ResolveMinBlockSize(bla.style, wdm, bla.space, geom)
	// The root element must fill at least the ICB block-size (ForcedMinBlockSize).
	if bla.space.ForcedMinBlockSize > minBlock {
		minBlock = bla.space.ForcedMinBlockSize
	}
	if finalBlockSize < minBlock {
		finalBlockSize = minBlock
	}
	if maxBlock, hasMax := ResolveMaxBlockSize(bla.style, wdm, bla.space, geom); hasMax {
		if finalBlockSize > maxBlock {
			finalBlockSize = maxBlock
		}
	}

	// Set the fragment size.
	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})

	builder.SetIntrinsicBlockSize(intrinsicBlockSize)

	// Set first baseline: for flex align-items:baseline (uses first line).
	if firstLineAscent > 0 {
		builder.SetBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
	}
	// Set last baseline: for inline-block alignment §10.8.1 (uses last line box).
	// For inline children, lastChildBaseline is the last line's baseline offset.
	// For block children, it's the last child's propagated baseline.
	if hasLastChildBaseline {
		builder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart +
			lastChildBlockOffset + lastChildBaseline)
	} else if firstLineAscent > 0 {
		// Single-line case: last baseline = first baseline.
		builder.SetLastBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
	}

	// Set box data for the renderer.
	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(bla.style, wdm, bla.space.AvailableSize.InlineSize), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})

	builder.SetEndMarginStrut(prevMarginStrut)
	builder.SetExclusionSpace(exclusionSpace)

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
		isPositioned := bla.style != nil && bla.style.GetPosition() != css.PositionStatic
		isRoot := bla.space.ForcedMinBlockSize > 0

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
				ctx:                 bla.ctx,
				containingBlockWDM:  wdm,
				containingBlockSize: LogicalSize{InlineSize: icbInline, BlockSize: icbBlock},
				geom:                geom,
			}
			oofPart.LayoutCandidates(builder.outOfFlowCandidates, builder)
		} else if isPositioned {
			// Positioned element: resolve absolute candidates here, but
			// propagate fixed candidates upward toward the ICB.
			var absoluteCandidates, fixedCandidates []OutOfFlowCandidate
			for _, cand := range builder.outOfFlowCandidates {
				if cand.IsFixedPosition {
					fixedCandidates = append(fixedCandidates, cand)
				} else {
					absoluteCandidates = append(absoluteCandidates, cand)
				}
			}
			if len(absoluteCandidates) > 0 {
				oofPart := &OutOfFlowLayoutPart{
					ctx:                 bla.ctx,
					containingBlockWDM:  wdm,
					containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
					geom:                geom,
				}
				oofPart.LayoutCandidates(absoluteCandidates, builder)
			}
			propagatedOOF = fixedCandidates
		} else {
			// Not positioned, not root: propagate ALL candidates upward.
			propagatedOOF = builder.outOfFlowCandidates
		}
	}

	result := builder.Build()

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
	if bla.style != nil && bla.style.GetPosition() == css.PositionRelative {
		logicalBlock := bla.space.AvailableSize.BlockSize
		if logicalBlock == Indefinite {
			logicalBlock = 0 // auto height → percentages compute to 0
		}
		physCB := ToPhysicalSize(LogicalSize{
			InlineSize: bla.space.AvailableSize.InlineSize,
			BlockSize:  logicalBlock,
		}, wdm.WM)
		offset := bla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
		var dx, dy float64
		// Left wins over right.
		if offset.HasLeft {
			dx = offset.Left
		} else if offset.HasRight {
			dx = -offset.Right
		}
		// Top wins over bottom.
		if offset.HasTop {
			dy = offset.Top
		} else if offset.HasBottom {
			dy = -offset.Bottom
		}
		result.Fragment.RelativeOffset = PhysicalOffset{X: dx, Y: dy}
	}

	return result
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
	// Compute child's border+padding in the parent's logical axes so we can
	// translate from child content-box origin to parent content-box origin.
	childBP := ComputeFragmentGeometry(childStyle, parentWDM)
	blockAdj := childBlockOff + childBP.Border.BlockStart + childBP.Padding.BlockStart
	inlineAdj := childInlineOff + childBP.Border.InlineStart + childBP.Padding.InlineStart

	// Detect cross-writing-mode propagation. When the child's writing mode
	// is orthogonal to the parent's, static positions must be converted
	// from child-logical coordinates to parent-logical coordinates.
	childWDM := NewWritingDirectionMode(childStyle)
	needsConversion := parentWDM.IsOrthogonalTo(childWDM)

	// Pre-compute the child's content-box physical size for coordinate
	// conversion. The static position is measured within this box.
	var childContentPhys PhysicalSize
	if needsConversion {
		childGeom := ComputeFragmentGeometry(childStyle, childWDM)
		physBP := ToPhysicalEdges(LogicalEdges{
			InlineStart: childGeom.Border.InlineStart + childGeom.Padding.InlineStart,
			InlineEnd:   childGeom.Border.InlineEnd + childGeom.Padding.InlineEnd,
			BlockStart:  childGeom.Border.BlockStart + childGeom.Padding.BlockStart,
			BlockEnd:    childGeom.Border.BlockEnd + childGeom.Padding.BlockEnd,
		}, childWDM)
		childContentPhys = PhysicalSize{
			Width:  childResult.Fragment.Size.Width - physBP.Left - physBP.Right,
			Height: childResult.Fragment.Size.Height - physBP.Top - physBP.Bottom,
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
) {
	childWDM := NewWritingDirectionMode(childStyle)
	// Resolve float margins in the parent's coordinates for positioning.
	childMargins := ResolveMargins(childStyle, parentWDM, contentInlineSize)

	// Floats establish a new BFC.
	childSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
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
		Build()

	// Layout the float's contents.
	childResult := layoutElement(bla.ctx, child, childSpace)
	childLogical := NewLogicalFragment(parentWDM, childResult.Fragment)

	// Compute the float's margin-box sizes.
	floatInlineSize := childMargins.InlineSum() + childLogical.InlineSize()
	floatBlockSize := childMargins.BlockSum() + childLogical.BlockSize()

	// Resolve the collapsed margin at the current position.
	collapsedMargin := prevMarginStrut.Resolve()
	floatBlockStart := blockCursor + collapsedMargin

	// Find where the float fits (CSS 2.1 §9.5.1 Rule 6: as high as possible).
	floatSide := childStyle.GetFloat()
	floatBlockOffset := es.FindFloatPosition(floatSide, floatInlineSize, floatBlockSize,
		contentInlineSize, floatBlockStart)

	// Compute the float's inline position.
	var floatInlineOffset float64
	if floatSide == css.FloatLeft {
		startOff, _ := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
		floatInlineOffset = startOff + childMargins.InlineStart
	} else {
		_, endOff := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
		floatInlineOffset = contentInlineSize - endOff - childMargins.InlineEnd - childLogical.InlineSize()
	}

	// Add the float fragment to the builder.
	builder.AddChild(childResult.Fragment, LogicalOffset{
		InlineOffset: floatInlineOffset,
		BlockOffset:  floatBlockOffset + childMargins.BlockStart,
	})

	// Add an exclusion for this float.
	exclusion := Exclusion{
		InlineOffset: floatInlineOffset - childMargins.InlineStart,
		BlockOffset:  floatBlockOffset,
		InlineSize:   floatInlineSize,
		BlockSize:    floatBlockSize,
		Side:         floatSide,
	}
	*outES = es.Add(exclusion)
}

// tryLayoutNestedDocument checks if this element is an iframe/object with a
// document source. If so, fetches + lays out the nested document and returns
// the root fragment. Returns nil if not applicable.
func (bla *BlockLayoutAlgorithm) tryLayoutNestedDocument(contentInlineSize float64, wdm WritingDirectionMode, geom FragmentGeometry) *PhysicalFragment {
	if bla.ctx.DocumentFetcher == nil || bla.node.DOMNode == nil {
		return nil
	}
	dom := bla.node.DOMNode
	var uri string
	switch dom.TagName {
	case "iframe":
		uri, _ = dom.GetAttribute("src")
	case "object":
		if dataType, _ := dom.GetAttribute("type"); dataType == "text/html" || dataType == "" {
			uri, _ = dom.GetAttribute("data")
		}
	default:
		return nil
	}
	if uri == "" {
		return nil
	}

	htmlContent, err := bla.ctx.DocumentFetcher(uri)
	if err != nil {
		return nil
	}

	// Compute the content-box physical size for the nested viewport.
	_, blockSize := ComputeReplacedSize(bla.ctx, bla.node, bla.style, bla.space)
	physSize := ToPhysicalSize(LogicalSize{
		InlineSize: contentInlineSize,
		BlockSize:  blockSize,
	}, wdm.WM)

	result := layoutNestedDocument(bla.ctx, htmlContent, physSize.Width, physSize.Height)
	if result == nil || result.Fragment == nil {
		return nil
	}
	return result.Fragment
}

// layoutElement dispatches to the appropriate layout algorithm based on
// the element's display type.
func layoutElement(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *LayoutResult {
	style := node.Style()
	if style == nil {
		return emptyResult(space.WritingDirection)
	}

	display := style.GetDisplay()

	switch display {
	case css.DisplayNone:
		return emptyResult(space.WritingDirection)
	case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayListItem, css.DisplayInlineBlock:
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayTable:
		return NewTableLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayTableRow, css.DisplayTableRowGroup, css.DisplayTableHeaderGroup,
		css.DisplayTableFooterGroup, css.DisplayTableCell, css.DisplayTableCaption:
		// Table internals laid out by their parent TableLayoutAlgorithm.
		// If encountered at top level, treat as block.
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	case css.DisplayFlex, css.DisplayInlineFlex:
		return NewFlexLayoutAlgorithm(ctx, node, space).Layout()
	// TODO: DisplayGrid
	default:
		// For now, treat unknown display types as block.
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	}
}

// emptyResult returns a zero-sized layout result.
func emptyResult(wdm WritingDirectionMode) *LayoutResult {
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetSize(LogicalSize{})
	return builder.Build()
}

// createsFormattingContext returns true if the element establishes a new
// block formatting context (CSS 2.1 §9.4.1).
func createsFormattingContext(style *css.Style) bool {
	if style == nil {
		return false
	}

	// Flow root always creates a BFC.
	if style.GetDisplay() == css.DisplayFlowRoot {
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

	// overflow != visible creates a BFC.
	if style.GetOverflow() != css.OverflowVisible {
		return true
	}

	// Inline-block creates a BFC.
	d := style.GetDisplay()
	if d == css.DisplayInlineBlock {
		return true
	}

	// Flex/grid items create a BFC.
	if d == css.DisplayFlex || d == css.DisplayInlineFlex ||
		d == css.DisplayGrid || d == css.DisplayInlineGrid {
		return true
	}

	return false
}

// needsShrinkToFit returns true if an element with auto inline-size should
// use shrink-to-fit sizing (CSS 2.1 §10.3.5). This applies to inline-block,
// floating, and absolutely positioned elements — NOT to regular block-level
// elements even if they establish a new formatting context.
func needsShrinkToFit(style *css.Style) bool {
	if style == nil {
		return false
	}
	d := style.GetDisplay()
	if d == css.DisplayInlineBlock || d == css.DisplayInlineFlex {
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
	minBlock := ResolveMinBlockSize(style, wdm, space, geom)
	maxBlock, hasMax := ResolveMaxBlockSize(style, wdm, space, geom)
	isScroller := style.GetOverflow() != css.OverflowVisible

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
	// If parent has min-height, use it as the available size (the parent
	// is guaranteed to be at least this tall).
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
	isScroller := style.GetOverflow() != css.OverflowVisible

	if isScroller {
		// This element is a scroller. Compute its effective block-size
		// constraint and propagate it to descendants.
		minBlock := ResolveMinBlockSize(style, wdm, space, geom)
		maxBlock, hasMax := ResolveMaxBlockSize(style, wdm, space, geom)
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
