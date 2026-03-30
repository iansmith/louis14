package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// BlockLayoutAlgorithm implements the block formatting context layout.
// It positions block-level children sequentially in the block direction,
// handling margin collapsing, auto sizing, floats, and clear.
//
// Ported from Blink's BlockLayoutAlgorithm.
type BlockLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *html.Node
	style *css.Style
	space ConstraintSpace
}

// NewBlockLayoutAlgorithm creates a block layout algorithm for the given
// element with the given constraint space.
func NewBlockLayoutAlgorithm(ctx *LayoutContext, node *html.Node, space ConstraintSpace) *BlockLayoutAlgorithm {
	style := ctx.ComputedStyles[node]
	return &BlockLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: style,
		space: space,
	}
}

// Layout performs block layout and returns the result.
func (bla *BlockLayoutAlgorithm) Layout() *LayoutResult {
	wdm := bla.space.WritingDirection
	geom := ComputeFragmentGeometry(bla.style, wdm)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetNode(bla.node)

	// Resolve inline-size.
	var contentInlineSize float64
	if explicitInline, ok := ResolveInlineSize(bla.style, wdm, bla.space, geom); ok {
		contentInlineSize = explicitInline
	} else {
		// Auto inline-size: fill available space minus border/padding.
		contentInlineSize = bla.space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if contentInlineSize < 0 {
			contentInlineSize = 0
		}
	}

	// Resolve block-size (may be auto).
	explicitBlockSize, hasExplicitBlock := ResolveBlockSize(bla.style, wdm, bla.space, geom)

	// Build child constraint space.
	childAvailableInline := contentInlineSize
	childAvailableBlock := Indefinite
	if hasExplicitBlock {
		childAvailableBlock = explicitBlockSize
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

	// TODO: Enable inline formatting context dispatch once the inline layout
	// pipeline has bidi support, vertical line boxes, and correct Ahem metrics.
	// The infrastructure is ready (see inline_layout.go, line_breaker.go,
	// inline_item.go) but enabling it in the production path causes regressions
	// because text rendering exposes unimplemented features.
	//
	// When ready, replace this block with:
	//   if hasOnlyInlineChildren(bla.node, bla.ctx.ComputedStyles) {
	//       blockSize, exclusionSpace = bla.layoutInlineChildren(...)
	//   } else { ... }

	{
		// Block formatting context: block-level children.
		for _, child := range bla.node.Children {
			// Skip non-element nodes (text nodes handled by inline layout later).
			if child.Type != html.ElementNode {
				continue
			}

			childStyle := bla.ctx.ComputedStyles[child]
			if childStyle == nil {
				continue
			}

			// Skip display:none elements.
			if childStyle.GetDisplay() == css.DisplayNone {
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
			childSpace := NewConstraintSpaceBuilder(wdm, childWDM, isChildNewFC).
				SetAvailableSize(LogicalSize{
					InlineSize: childInlineForSpace,
					BlockSize:  childAvailableBlock,
				}).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: contentInlineSize,
					BlockSize:  explicitBlockSize, // 0 if auto
				}).
				SetExclusionSpace(exclusionSpace).
				Build()

			// Recursively lay out the child.
			childResult := layoutElement(bla.ctx, child, childSpace)

			// Margin collapsing: collapse current margin strut with child's
			// block-start margin.
			prevMarginStrut.Append(childMargins.BlockStart)
			collapsedMargin := prevMarginStrut.Resolve()

			// Position child in the block direction.
			childBlockOffset := blockCursor + collapsedMargin

			// Position child in the inline direction (accounting for floats).
			childInlineOffset := childMargins.InlineStart + floatStartOff

			// Add child to the builder.
			builder.AddChild(childResult.Fragment, LogicalOffset{
				InlineOffset: childInlineOffset,
				BlockOffset:  childBlockOffset,
			})

			// Advance block cursor past this child.
			childLogical := NewLogicalFragment(wdm, childResult.Fragment)
			blockCursor = childBlockOffset + childLogical.BlockSize()

			// Reset margin strut to the child's block-end margin.
			prevMarginStrut = childResult.EndMarginStrut
			prevMarginStrut.Append(childMargins.BlockEnd)
		}
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

	// Set the fragment size.
	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})

	builder.SetIntrinsicBlockSize(intrinsicBlockSize)

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

	return builder.Build()
}

// layoutFloat handles layout and positioning of a float child within the
// block formatting context. CSS 2.1 §9.5.
func (bla *BlockLayoutAlgorithm) layoutFloat(
	child *html.Node,
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
		SetAvailableSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  availableBlock,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  0,
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
func layoutElement(ctx *LayoutContext, node *html.Node, space ConstraintSpace) *LayoutResult {
	style := ctx.ComputedStyles[node]
	if style == nil {
		return emptyResult(space.WritingDirection)
	}

	display := style.GetDisplay()

	switch display {
	case css.DisplayNone:
		return emptyResult(space.WritingDirection)
	case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayListItem:
		return NewBlockLayoutAlgorithm(ctx, node, space).Layout()
	// TODO: DisplayFlex, DisplayGrid, DisplayTable, DisplayInlineBlock
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

	// Flex/grid items create a BFC.
	d := style.GetDisplay()
	if d == css.DisplayFlex || d == css.DisplayInlineFlex ||
		d == css.DisplayGrid || d == css.DisplayInlineGrid {
		return true
	}

	return false
}
