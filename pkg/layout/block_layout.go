package layout

import "louis14/pkg/css"

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

	// Replaced elements (img, etc.) with auto inline/block size: derive from
	// intrinsic dimensions and aspect ratio.
	// CSS 2.1 §10.3.2 (replaced inline): inline-size = intrinsic width.
	// CSS 2.1 §10.6.2: if height is auto and there is an intrinsic ratio, use it.
	// CSS Containment: size containment overrides intrinsic sizing — treat as 0.
	hasSizeContain := bla.style != nil && bla.style.HasSizeContainment()
	if !hasSizeContain && bla.node.DOMNode != nil && isReplacedElement(bla.node.DOMNode) {
		// Check if inline-size is explicitly set. ResolveInlineSize returns false
		// for auto/unset, which is when we should use the intrinsic inline-size.
		_, explicitInlineOK := ResolveInlineSize(bla.style, wdm, bla.space, geom)
		if !explicitInlineOK && !bla.space.IsFixedInlineSize {
			// CSS 2.1 §10.3.2: replaced elements with auto width use intrinsic width.
			inlineSize, _ := ComputeReplacedSize(bla.ctx, bla.node, bla.style, bla.space)
			if inlineSize > 0 && inlineSize < contentInlineSize {
				contentInlineSize = inlineSize
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
		bfcBlockOrigin = bla.space.BfcBlockOffset + geom.Border.BlockStart + geom.Padding.BlockStart
	}

	// Lay out children in the block direction.
	// CSS 2.1 §9.2.1.1: a block container has either all block-level or
	// all inline-level children.
	blockCursor := 0.0 // current block position within content box
	var prevMarginStrut MarginStrut

	// Fragmentation state: track incoming break token for resume.
	incomingBreakToken := bla.space.BreakToken
	resumeChildIdx := -1 // index in Children() to resume from (-1 = start from beginning)
	var resumeChildBreakToken *BlockBreakToken
	if incomingBreakToken != nil {
		blockCursor = 0 // We start at 0 in the new fragmentainer; consumed is tracked by the token.
		if len(incomingBreakToken.ChildBreakTokens) > 0 {
			resumeChildBreakToken = incomingBreakToken.ChildBreakTokens[0]
		}
	}

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
	var lastChildBaseline float64     // Baseline of the last in-flow block child.
	var lastChildBlockOffset float64  // Block offset of the last in-flow block child.
	hasLastChildBaseline := false

	// Iframe/object with a document source: lay out the nested document
	// instead of this element's DOM children.
	if nested := bla.tryLayoutNestedDocument(contentInlineSize, wdm, geom); nested != nil {
		// For vertical-rl/sideways-rl nested roots, the root is anchored to the
		// right edge of the iframe viewport. Apply the X offset as an inline offset.
		offset := LogicalOffset{InlineOffset: nested.rootOffsetX}
		builder.AddChild(nested.fragment, offset)
	} else if hasOnlyInlineChildren(bla.node) {
		// Inline formatting context: text nodes and inline-level children.
		prevES := exclusionSpace
		var inlineAscent, lastBaselineOff float64
		blockCursor, exclusionSpace, inlineAscent, lastBaselineOff = bla.layoutInlineChildren(wdm, contentInlineSize, exclusionSpace, builder, bfcBlockOrigin)
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
		children := bla.node.Children()

		// When resuming from a break token, find the child to resume at.
		if incomingBreakToken != nil && resumeChildBreakToken != nil {
			for ci, ch := range children {
				if ch == resumeChildBreakToken.Node {
					resumeChildIdx = ci
					break
				}
			}
		} else if incomingBreakToken != nil && incomingBreakToken.HasSeenAllChildren {
			// All children were seen in a previous fragment; nothing to lay out.
			resumeChildIdx = len(children)
		}

		for childIdx, child := range children {
			// When resuming, skip children completed in previous fragments.
			if resumeChildIdx >= 0 && childIdx < resumeChildIdx {
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
					blockCursor, &prevMarginStrut, exclusionSpace, builder, &exclusionSpace, bfcBlockOrigin)
				// Only BFC roots extend auto block-size to clear floats
				// (CSS 2.1 §10.6.7). Non-BFC parents let floats overflow;
				// hasOwnFloats is already true for BFC roots (initialized
				// from IsNewFormattingContext).

				continue
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
				clearedBlockBfc := exclusionSpace.ClearanceOffset(clearType, bfcCursor, wdm)
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
			isChildNewFC := createsFormattingContext(childStyle) ||
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
			childInlineForSpace := childAvailableInline - childMargins.InlineSum() - floatStartOff - floatEndOff
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
					neededInline := resolvedInline + childGeomForBFC.InlineBorderPadding() + childMargins.InlineSum()
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
						}
						// Recompute float offsets at new position.
						floatStartOff, floatEndOff = exclusionSpace.FindAvailableInlineSize(bfcBlockOrigin+blockCursor, 0, childAvailableInline)
						childInlineForSpace = childAvailableInline - childMargins.InlineSum() - floatStartOff - floatEndOff
						if childInlineForSpace < 0 {
							childInlineForSpace = 0
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
					BlockSize:  explicitBlockSize, // 0 if auto
				}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				SetExclusionSpace(exclusionSpace)

			// For non-BFC children, propagate the BFC block offset so they
			// can correctly query the exclusion space in their own layout.
			if !isChildNewFC {
				// The child's tentative BFC offset = parent's BFC origin +
				// child's local position (blockCursor + pending margin).
				tentativeChildBfcOff := bfcBlockOrigin + blockCursor + prevMarginStrut.Resolve() + childMargins.BlockStart
				csBuilder.SetBfcBlockOffset(tentativeChildBfcOff)
			}

			// Propagate fragmentation context to children.
			if bla.space.HasBlockFragmentation {
				childFragOffset := bla.space.FragmentainerOffset + blockCursor + prevMarginStrut.Resolve()
				csBuilder.
					SetHasBlockFragmentation(true).
					SetFragmentainerBlockSize(bla.space.FragmentainerBlockSize).
					SetFragmentainerOffset(childFragOffset).
					SetBlockFragmentationType(bla.space.BlockFragmentationType)

				// Pass child break token if resuming this specific child.
				if childIdx == resumeChildIdx && resumeChildBreakToken != nil {
					csBuilder.SetBreakToken(resumeChildBreakToken)
				}
			}

			childSpace := csBuilder.Build()

			// Recursively lay out the child.
			childResult := layoutElement(bla.ctx, child, childSpace)

			// CSS 2.1 §9.5 Rule 5 / §10.3.3: A new BFC must not overlap
			// float margin boxes. If the child doesn't fit alongside
			// floats at the current block position, push it below them.
			// This check uses NewLogicalFragment(parentWDM, ...) which
			// correctly maps the child's dimensions to the parent's frame,
			// so it works for both same-mode and orthogonal children.
			if isChildNewFC && (floatStartOff > 0 || floatEndOff > 0) {
				childLogicalTmp := NewLogicalFragment(wdm, childResult.Fragment)
				neededInline := childLogicalTmp.InlineSize() + childMargins.InlineSum()
				availableInline := childAvailableInline - floatStartOff - floatEndOff
				if neededInline > availableInline {
					// Child doesn't fit — find the earliest block position
					// where the BFC fits alongside remaining floats.
					bfcBlockSize := childLogicalTmp.BlockSize() + childMargins.BlockSum()
					newBlockBfc := exclusionSpace.FindFloatPosition(
						css.FloatLeft, neededInline, bfcBlockSize,
						childAvailableInline, floatCheckBlock)
					newBlock := newBlockBfc - bfcBlockOrigin
					if newBlock > blockCursor {
						blockCursor = newBlock
						prevMarginStrut = MarginStrut{}
						// Recompute float offsets at the new position.
						floatStartOff, floatEndOff = exclusionSpace.FindAvailableInlineSize(bfcBlockOrigin+blockCursor, 0, childAvailableInline)
						childInlineForSpace = childAvailableInline - childMargins.InlineSum() - floatStartOff - floatEndOff
						if childInlineForSpace < 0 {
							childInlineForSpace = 0
						}
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
								BlockSize:  explicitBlockSize,
							}).
							SetPercentageResolutionInlineSize(contentInlineSize).
							SetExclusionSpace(exclusionSpace)
						childSpace = csBuilder2.Build()
						childResult = layoutElement(bla.ctx, child, childSpace)
					}
				}
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
			childLogical := NewLogicalFragment(wdm, childResult.Fragment)
			childBlockSize := childLogical.BlockSize()
			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			collapseThrough := childBlockSize == 0 &&
				len(childResult.Fragment.Children) == 0 &&
				childGeom.Border.BlockStart == 0 && childGeom.Border.BlockEnd == 0 &&
				childGeom.Padding.BlockStart == 0 && childGeom.Padding.BlockEnd == 0 &&
				!isChildNewFC

			// Step 4: Position child in the inline direction.
			// CSS 2.1 §10.3.3: If both margin-inline-start and margin-inline-end are auto,
			// and the element has a definite inline-size, center it.
			// NOTE: computed before collapse-through check so it can be used for OOF
			// propagation even when the element collapses through.
			childInlineOffset := childMargins.InlineStart + floatStartOff

			if collapseThrough {
				// Margins collapse through: append block-end margin and continue
				// without resolving or advancing the cursor.
				// Pick up any propagated OOF candidates before continuing.
				if len(childResult.PropagatedOOFCandidates) > 0 {
					approxBlock := blockCursor + prevMarginStrut.Resolve()
					bla.inheritPropagatedOOF(childResult, childStyle, wdm,
						childInlineOffset, approxBlock, builder)
				}
				prevMarginStrut.Append(childMargins.BlockEnd)
				continue
			}

			rawMargin := childStyle.GetMargin()
			autoInlineStart, autoInlineEnd, _, _ := PhysicalAutoMarginsToLogical(rawMargin, wdm)
			if autoInlineStart || autoInlineEnd {
				childInlineSize := NewLogicalFragment(wdm, childResult.Fragment).InlineSize()
				remaining := childAvailableInline - childInlineSize - floatStartOff - floatEndOff
				if remaining > 0 {
					if autoInlineStart && autoInlineEnd {
						childInlineOffset = floatStartOff + remaining/2
					} else if autoInlineStart {
						childInlineOffset = floatStartOff + remaining - childMargins.InlineEnd
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

			// CSS Inline §4.2: propagate the first in-flow block child's
			// first baseline as this container's first baseline.
			if !hasFirstChildBaseline && childResult.HasBaseline {
				firstChildBaseline = childResult.Baseline
				firstChildBlockOffset = actualChildBlockOff
				hasFirstChildBaseline = true
			}

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

			// Fragmentation: check if we've overflowed the fragmentainer.
			if bla.space.HasBlockFragmentation {
				fragSize := bla.space.FragmentainerBlockSize
				if fragSize != Indefinite && blockCursor > fragSize-bla.space.FragmentainerOffset {
					// Content overflowed. Create outgoing break token.
					shortage := blockCursor - (fragSize - bla.space.FragmentainerOffset)

					outToken := &BlockBreakToken{
						Node:              bla.node,
						ConsumedBlockSize: blockCursor,
						SequenceNumber:    0,
					}
					if incomingBreakToken != nil {
						outToken.ConsumedBlockSize += incomingBreakToken.ConsumedBlockSize
						outToken.SequenceNumber = incomingBreakToken.SequenceNumber + 1
					}

					// If the child itself broke, include its break token.
					if childResult.BreakToken != nil {
						outToken.ChildBreakTokens = append(outToken.ChildBreakTokens, childResult.BreakToken)
					} else {
						// Child completed, but there are more siblings.
						// Create a marker break token for the next sibling.
						if childIdx+1 < len(children) {
							nextChild := children[childIdx+1]
							outToken.ChildBreakTokens = append(outToken.ChildBreakTokens, &BlockBreakToken{
								Node:          nextChild,
								IsBreakBefore: true,
							})
						} else {
							outToken.HasSeenAllChildren = true
						}
					}

					// Build the partial fragment.
					intrinsicBlock := blockCursor
					if !hasExplicitBlock {
						borderBoxBlock := intrinsicBlock + geom.BlockBorderPadding()
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
						Border:  ToPhysicalEdges(geom.Border, wdm),
						Padding: ToPhysicalEdges(geom.Padding, wdm),
					})
					builder.SetEndMarginStrut(prevMarginStrut)
					builder.SetExclusionSpace(exclusionSpace)
					result := builder.Build()
					result.BreakToken = outToken
					result.MinSpaceShortage = shortage
					result.PropagatedTopMargin = propagatedTopMargin
					return result
				}
			}
		}
	}

	// CSS 2.1 §8.3.1: The bottom margin of an in-flow block box with a
	// 'height' of 'auto' collapses with its last child's bottom margin if
	// the box has no bottom border and no bottom padding. When block-size
	// is explicit (not auto), OR there is border/padding at block-end,
	// the trailing margin does NOT propagate out as EndMarginStrut.
	canPropagateBottom := !bla.space.IsNewFormattingContext &&
		!hasExplicitBlock &&
		geom.Border.BlockEnd == 0 && geom.Padding.BlockEnd == 0

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

	// CSS 2.1 §10.6.7: For elements that own floats (BFC roots or elements
	// that contain their own floats), auto block-size extends to clear them.
	// Elements that only inherit floats from a parent BFC do not extend.
	if !hasExplicitBlock && hasOwnFloats {
		bfcCursor := bfcBlockOrigin + blockCursor
		clearedBlockBfc := exclusionSpace.ClearanceOffset(css.ClearBoth, bfcCursor, wdm)
		clearedBlock := clearedBlockBfc - bfcBlockOrigin
		if clearedBlock > blockCursor {
			blockCursor = clearedBlock
		}
	}
	// Compute final block-size.
	intrinsicBlockSize := blockCursor
	// CSS Containment: size containment — element is sized as if empty.
	// If block-size is auto (not explicit), intrinsic size is 0.
	if bla.style != nil && bla.style.HasSizeContainment() && !hasExplicitBlock {
		intrinsicBlockSize = 0
	}
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
		// CSS Containment: layout and paint containment establish a containing
		// block for absolutely positioned descendants (same as positioned elements).
		isContainmentCB := bla.style != nil && (bla.style.HasLayoutContainment() || bla.style.HasPaintContainment())
		// CSS Transforms §2.1 / Will Change §2.2: transform, perspective, or
		// will-change naming them creates a containing block for all positioned
		// descendants (including fixed).
		isTransformCB := false
		if bla.style != nil && !isContainmentCB {
			if transforms := bla.style.GetTransforms(); len(transforms) > 0 {
				isTransformCB = true
			} else if filters := bla.style.GetFilter(); len(filters) > 0 {
				isTransformCB = true
			} else {
				for _, prop := range bla.style.GetWillChange() {
					if prop == "transform" || prop == "perspective" || prop == "filter" {
						isTransformCB = true
						break
					}
				}
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
				ctx:                    bla.ctx,
				containingBlockWDM:     wdm,
				// ICB is the viewport: no border or padding, so padding-box == content-box.
				containingBlockSize:    LogicalSize{InlineSize: icbInline, BlockSize: icbBlock},
				containingBlockPadding: LogicalEdges{},
				geom:                   geom,
			}
			oofPart.LayoutCandidates(builder.outOfFlowCandidates, builder)
		} else if isContainmentCB || isTransformCB {
			// CSS Containment / CSS Transforms: containment, transforms, filters,
			// and will-change:transform/perspective/filter make this element a
			// containing block for ALL positioned descendants, including fixed.
			//
			// Per CSS 2.1 §10.3.7 / Blink's GetContainingBlockInfo():
			// CB size = padding-box = content + padding (borders excluded).
			oofPart := &OutOfFlowLayoutPart{
				ctx:                 bla.ctx,
				containingBlockWDM:  wdm,
				containingBlockSize: LogicalSize{
					InlineSize: contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd,
					BlockSize:  finalBlockSize + geom.Padding.BlockStart + geom.Padding.BlockEnd,
				},
				containingBlockPadding: geom.Padding,
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
				// Per CSS 2.1 §10.3.7 / Blink's GetContainingBlockInfo():
				// CB size = padding-box = content + padding (borders excluded).
				oofPart := &OutOfFlowLayoutPart{
					ctx:                 bla.ctx,
					containingBlockWDM:  wdm,
					containingBlockSize: LogicalSize{
						InlineSize: contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd,
						BlockSize:  finalBlockSize + geom.Padding.BlockStart + geom.Padding.BlockEnd,
					},
					containingBlockPadding: geom.Padding,
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
	//
	// The overconstrained resolution rule is "start wins over end" in logical
	// coordinates, following Blink's ComputeRelativeOffset (relative_utils.cc).
	if bla.style != nil && (bla.style.GetPosition() == css.PositionRelative || bla.style.GetPosition() == css.PositionSticky) {
		logicalBlock := bla.space.AvailableSize.BlockSize
		if logicalBlock == Indefinite {
			logicalBlock = 0 // auto height → percentages compute to 0
		}
		physCB := ToPhysicalSize(LogicalSize{
			InlineSize: bla.space.AvailableSize.InlineSize,
			BlockSize:  logicalBlock,
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
func computeRelativeOffset(offset css.PositionOffset, wdm WritingDirectionMode) PhysicalOffset {
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

	return PhysicalOffset{X: dx, Y: dy}
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
	// The bug with using parentWDM here: when child and parent have orthogonal
	// writing modes, interpreting the child's physical borders using the parent's
	// WDM gives wrong logical edge assignments. For example, a VRL child inside
	// an HTB parent has its physical top mapped to block-start in VRL, but the
	// parent's HTB WDM maps physical top to block-start (which is the same physical
	// edge but correct). However, the child's inline-start in VRL = physical top,
	// while parentWDM would assign physical top to parent's block-start.
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
	blockAdj := childBlockOff + parentLogicalBP.BlockStart
	inlineAdj := childInlineOff + parentLogicalBP.InlineStart

	// Detect when the child's writing direction differs from the parent's.
	// This includes both orthogonal writing modes (e.g. HTB inside VRL) and
	// same-axis writing modes with different directions (e.g. VLR-LTR inside
	// VLR-RTL). In all these cases the static position must be re-expressed in
	// the parent's logical coordinate system via a physical round-trip.
	needsConversion := childWDM.WM != parentWDM.WM || childWDM.Dir != parentWDM.Dir

	// Pre-compute the child's content-box physical size for coordinate
	// conversion. The static position is measured within this box.
	var childContentPhys PhysicalSize
	if needsConversion {
		childContentPhys = PhysicalSize{
			Width:  childResult.Fragment.Size.Width - physBPEdges.Left - physBPEdges.Right,
			Height: childResult.Fragment.Size.Height - physBPEdges.Top - physBPEdges.Bottom,
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
	bfcBlockOrigin float64,
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
		SetPercentageResolutionInlineSize(contentInlineSize).
		Build()

	// Layout the float's contents.
	childResult := layoutElement(bla.ctx, child, childSpace)
	childLogical := NewLogicalFragment(parentWDM, childResult.Fragment)

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
		clearedBlock := es.ClearanceOffset(clearType, floatBlockStart, parentWDM)
		if clearedBlock > floatBlockStart {
			floatBlockStart = clearedBlock
		}
	}

	// Find where the float fits (CSS 2.1 §9.5.1 Rule 6: as high as possible).
	floatSide := childStyle.GetFloat()
	floatBlockOffset := es.FindFloatPosition(floatSide, floatInlineSize, floatBlockSize,
		contentInlineSize, floatBlockStart)

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

// tryLayoutNestedDocument checks if this element is an iframe/object with a
// document source. If so, fetches + lays out the nested document and returns
// the root fragment and its X offset within the iframe viewport. Returns nil if not applicable.
func (bla *BlockLayoutAlgorithm) tryLayoutNestedDocument(contentInlineSize float64, wdm WritingDirectionMode, geom FragmentGeometry) *nestedDocFragment {
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

	res := layoutNestedDocument(bla.ctx, htmlContent, physSize.Width, physSize.Height, uri)
	if res == nil {
		return nil
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

	display := style.GetDisplay()

	switch display {
	case css.DisplayNone:
		return emptyResult(space.WritingDirection)
	case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayListItem, css.DisplayInlineBlock:
		// TODO: CSS Multicol — requires true fragmentation (break tokens) to
		// avoid regressing tests that currently pass via block fallback.
		// Skeleton in multicol_layout.go; disabled until fragmentation is done.
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

	// overflow != visible on either axis creates a BFC.
	if style.GetOverflowX() != css.OverflowVisible || style.GetOverflowY() != css.OverflowVisible {
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

	// CSS Containment: layout and paint containment establish a BFC.
	if style.HasLayoutContainment() || style.HasPaintContainment() {
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
	if d == css.DisplayInlineBlock || d == css.DisplayInlineFlex || d == css.DisplayInlineTable {
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
	isScroller := style.GetOverflowX() != css.OverflowVisible || style.GetOverflowY() != css.OverflowVisible

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

