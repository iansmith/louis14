package layout

// OutOfFlowCandidate records an absolutely or fixed positioned child
// discovered during normal layout. Collected by BoxFragmentBuilder,
// resolved by OutOfFlowLayoutPart after the containing block's size
// is known.
//
// Mirrors Blink's OutOfFlowPositionedCandidate.
type OutOfFlowCandidate struct {
	// Node is the layout tree node for the out-of-flow child.
	Node *LayoutInputNode

	// StaticOffset is the position the element would have in normal flow.
	// Used when top/left/bottom/right are all 'auto'.
	StaticOffset LogicalOffset
}

// OutOfFlowLayoutPart handles layout of absolutely and fixed positioned
// elements. It runs after the containing block's layout is complete,
// using the containing block's resolved dimensions.
//
// Ported from Blink's OutOfFlowLayoutPart.
type OutOfFlowLayoutPart struct {
	ctx                 *LayoutContext
	containingBlockWDM  WritingDirectionMode
	containingBlockSize LogicalSize // content-box of the containing block
	geom                FragmentGeometry
}

// LayoutCandidates positions all out-of-flow candidates and adds their
// fragments to the builder.
func (p *OutOfFlowLayoutPart) LayoutCandidates(
	candidates []OutOfFlowCandidate,
	builder *BoxFragmentBuilder,
) {
	contentInlineSize := p.containingBlockSize.InlineSize
	contentBlockSize := p.containingBlockSize.BlockSize
	wdm := p.containingBlockWDM

	for _, candidate := range candidates {
		child := candidate.Node
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)

		// Build constraint space for the absolute child.
		// Percentages resolve against the containing block's padding box.
		childSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetAvailableSize(LogicalSize{
				InlineSize: contentInlineSize,
				BlockSize:  contentBlockSize,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: contentInlineSize,
				BlockSize:  contentBlockSize,
			}).
			Build()

		childResult := layoutElement(p.ctx, child, childSpace)
		childLogical := NewLogicalFragment(wdm, childResult.Fragment)

		// Get raw margins (preserving auto flags).
		rawMargin := childStyle.GetMargin()
		childMargins := ResolveMargins(childStyle, childWDM, contentInlineSize)
		offset := childStyle.GetPositionOffsetResolved(contentInlineSize, contentBlockSize)

		// CSS 2.1 §10.3.7: Inline position for abs-pos non-replaced.
		var inlineOffset float64
		if offset.HasLeft {
			inlineOffset = offset.Left + childMargins.InlineStart
		} else if offset.HasRight {
			inlineOffset = contentInlineSize - offset.Right - childMargins.InlineEnd - childLogical.InlineSize()
		} else {
			inlineOffset = childMargins.InlineStart
		}

		// CSS 2.1 §10.6.4: Block position for abs-pos non-replaced.
		var blockOffset float64
		if offset.HasTop && offset.HasBottom {
			// Both top and bottom set — solve for auto margins.
			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			usedTop := offset.Top
			usedBottom := offset.Bottom
			usedHeight := childLogical.BlockSize()
			usedBPBlock := childGeom.BlockBorderPadding()
			remaining := contentBlockSize - usedTop - usedBottom - usedBPBlock - usedHeight

			autoTop := rawMargin.AutoTop
			autoBottom := rawMargin.AutoBottom
			if wdm.IsVertical() {
				autoTop = rawMargin.AutoLeft
				autoBottom = rawMargin.AutoRight
			}

			if autoTop && autoBottom {
				// Both auto: split evenly.
				halfMargin := remaining / 2
				if halfMargin < 0 {
					halfMargin = 0
				}
				blockOffset = usedTop + halfMargin
			} else if autoTop {
				blockOffset = usedTop + remaining - childMargins.BlockEnd
			} else if autoBottom {
				blockOffset = usedTop + childMargins.BlockStart
			} else {
				blockOffset = usedTop + childMargins.BlockStart
			}
		} else if offset.HasTop {
			blockOffset = offset.Top + childMargins.BlockStart
		} else if offset.HasBottom {
			blockOffset = contentBlockSize - offset.Bottom - childMargins.BlockEnd - childLogical.BlockSize()
		} else {
			blockOffset = childMargins.BlockStart
		}

		builder.AddChild(childResult.Fragment, LogicalOffset{
			InlineOffset: inlineOffset,
			BlockOffset:  blockOffset,
		})
	}
}
