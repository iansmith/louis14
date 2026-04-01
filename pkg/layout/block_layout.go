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

	// §10.3.2: For orthogonal children, when the parent's block-size is
	// indefinite but has a max-block-size, use that as the available block.
	// This prevents the ICB fallback from overriding the max constraint.
	orthogonalAvailableBlock := childAvailableBlock
	if childAvailableBlock == Indefinite {
		if maxBlock, hasMax := ResolveMaxBlockSize(bla.style, wdm, bla.space, geom); hasMax {
			orthogonalAvailableBlock = maxBlock
		}
	}

	// Float exclusion tracking.
	// Inherit exclusion space from parent, or start fresh for new BFCs.
	exclusionSpace := bla.space.ExclusionSpace
	if bla.space.IsNewFormattingContext || exclusionSpace == nil {
		exclusionSpace = &ExclusionSpace{}
	}

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
	if hasOnlyInlineChildren(bla.node) {
		// Inline formatting context: text nodes and inline-level children.
		var inlineAscent float64
		blockCursor, exclusionSpace, inlineAscent = bla.layoutInlineChildren(wdm, contentInlineSize, exclusionSpace, builder)
		firstLineAscent = inlineAscent
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
				staticBlockOffset := blockCursor + prevMarginStrut.Resolve()
				builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
					Node:         child,
					StaticOffset: LogicalOffset{InlineOffset: 0, BlockOffset: staticBlockOffset},
				})
				continue
			}

			// Handle floats.
			if childStyle.GetFloat() != css.FloatNone {
				bla.layoutFloat(child, childStyle, wdm, contentInlineSize, childAvailableBlock,
					blockCursor, &prevMarginStrut, exclusionSpace, builder, &exclusionSpace)
				continue
			}

			// Handle clear property.
			clearType := childStyle.GetClear()
			if clearType != css.ClearNone {
				clearedBlock := exclusionSpace.ClearanceOffset(clearType, blockCursor)
				if clearedBlock > blockCursor {
					blockCursor = clearedBlock
					prevMarginStrut = MarginStrut{} // Clear resets margin collapsing.
				}
			}

			// Determine child's writing direction.
			childWDM := NewWritingDirectionMode(childStyle)

			// Resolve child's margins.
			childMargins := ResolveMargins(childStyle, childWDM, childAvailableInline)

			// Compute available inline for this child, accounting for floats.
			floatStartOff, floatEndOff := exclusionSpace.FindAvailableInlineSize(blockCursor, 0)
			childInlineForSpace := childAvailableInline - childMargins.InlineSum() - floatStartOff - floatEndOff
			if childInlineForSpace < 0 {
				childInlineForSpace = 0
			}

			// Build constraint space for this child.
			isChildNewFC := createsFormattingContext(childStyle)
			blockForChild := childAvailableBlock
			if wdm.IsOrthogonalTo(childWDM) {
				blockForChild = orthogonalAvailableBlock
			}
			childSpace := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
				SetOrthogonalFallbackInlineSize(
					orthogonalFallbackSize(childWDM, bla.ctx)).
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
				prevMarginStrut.Append(childMargins.BlockEnd)
				continue
			}

			// Step 4: Position child in the inline direction.
			childInlineOffset := childMargins.InlineStart + floatStartOff

			// Step 5: Handle parent-child top margin collapsing.
			if firstNonEmptyChild && canPropagateTop {
				// Propagate the accumulated margin strut upward.
				propagatedTopMargin = prevMarginStrut
				// Position child at offset 0 (margin moves outside parent).
				builder.AddChild(childResult.Fragment, LogicalOffset{
					InlineOffset: childInlineOffset,
					BlockOffset:  0,
				})
				blockCursor = childBlockSize
			} else {
				// Step 6: Normal margin resolution.
				collapsedMargin := prevMarginStrut.Resolve()
				childBlockOffset := blockCursor + collapsedMargin
				builder.AddChild(childResult.Fragment, LogicalOffset{
					InlineOffset: childInlineOffset,
					BlockOffset:  childBlockOffset,
				})
				blockCursor = childBlockOffset + childBlockSize
			}

			firstNonEmptyChild = false

			// Reset margin strut to the child's block-end margin.
			prevMarginStrut = childResult.EndMarginStrut
			prevMarginStrut.Append(childMargins.BlockEnd)
		}
	}

	// CSS 2.1 §10.6.7: For BFC roots with auto block-size, the auto height
	// extends to the last child's margin-bottom edge. For non-BFC containers,
	// the trailing margin propagates outward via EndMarginStrut.
	if bla.space.IsNewFormattingContext && !prevMarginStrut.IsEmpty() {
		blockCursor += prevMarginStrut.Resolve()
		prevMarginStrut = MarginStrut{} // consumed
	}

	// Ensure content clears all floats for auto block-size.
	if !hasExplicitBlock {
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

	// Set baseline: distance from border-box block-start to first line baseline.
	// Used by flex layout for align-items: baseline.
	if firstLineAscent > 0 {
		builder.SetBaseline(geom.Border.BlockStart + geom.Padding.BlockStart + firstLineAscent)
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

	// CSS 2.1 §10.6.4: Lay out absolutely positioned children.
	// They are positioned relative to this containing block's padding box.
	if len(builder.outOfFlowCandidates) > 0 {
		oofPart := &OutOfFlowLayoutPart{
			ctx:                 bla.ctx,
			containingBlockWDM:  wdm,
			containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
			geom:                geom,
		}
		oofPart.LayoutCandidates(builder.outOfFlowCandidates, builder)
	}

	result := builder.Build()

	// CSS 2.1 §8.3.1: Propagate first child's margin for parent-child collapsing.
	if !propagatedTopMargin.IsEmpty() {
		result.PropagatedTopMargin = propagatedTopMargin
	}

	// CSS 2.1 §9.4.3: Compute position:relative offset during layout.
	// Stored on the fragment for paint-time application (not baked into positions).
	// Percentages resolve against the containing block's dimensions.
	if bla.style != nil && bla.style.GetPosition() == css.PositionRelative {
		cbWidth := bla.space.AvailableSize.InlineSize
		cbHeight := bla.space.AvailableSize.BlockSize
		if cbHeight == Indefinite {
			cbHeight = 0 // auto height → percentages compute to 0
		}
		offset := bla.style.GetPositionOffsetResolved(cbWidth, cbHeight)
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
	childMargins := ResolveMargins(childStyle, childWDM, contentInlineSize)

	// Floats establish a new BFC.
	childSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
		SetOrthogonalFallbackInlineSize(
			orthogonalFallbackSize(childWDM, bla.ctx)).
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
		startOff, _ := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize)
		floatInlineOffset = startOff + childMargins.InlineStart
	} else {
		_, endOff := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize)
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
