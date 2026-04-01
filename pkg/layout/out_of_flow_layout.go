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
	cbInline := p.containingBlockSize.InlineSize
	cbBlock := p.containingBlockSize.BlockSize
	wdm := p.containingBlockWDM

	// Convert logical CB size to physical for percentage resolution of
	// inset properties (top/right/bottom/left percentages resolve against
	// physical width/height, not logical axes).
	cbPhys := ToPhysicalSize(p.containingBlockSize, wdm.WM)

	for _, candidate := range candidates {
		child := candidate.Node
		staticBlock := candidate.StaticOffset.BlockOffset
		staticInline := candidate.StaticOffset.InlineOffset
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)

		// Build constraint space for the absolute child.
		childSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetAvailableSize(LogicalSize{
				InlineSize: cbInline,
				BlockSize:  cbBlock,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: cbInline,
				BlockSize:  cbBlock,
			}).
			Build()

		childResult := layoutElement(p.ctx, child, childSpace)
		childLogical := NewLogicalFragment(wdm, childResult.Fragment)

		// Resolve physical insets using physical CB dimensions, then convert
		// to logical insets based on the containing block's writing mode.
		physOffset := childStyle.GetPositionOffsetResolved(cbPhys.Width, cbPhys.Height)
		insets := PhysicalInsetsToLogical(physOffset, wdm)

		// Resolve margins and get auto-margin flags in logical coordinates.
		childMargins := ResolveMargins(childStyle, wdm, cbInline)
		rawMargin := childStyle.GetMargin()
		autoInlineStart, autoInlineEnd, autoBlockStart, autoBlockEnd :=
			PhysicalAutoMarginsToLogical(rawMargin, wdm)

		// CSS 2.1 §10.3.7 / §10.6.4: Solve inline and block constraint
		// equations entirely in logical coordinates.

		// --- Inline axis ---
		var inlineOffset float64
		if insets.HasInlineStart && insets.HasInlineEnd {
			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			usedInlineSize := childLogical.InlineSize()
			usedBPInline := childGeom.InlineBorderPadding()
			remaining := cbInline - insets.InlineStart - insets.InlineEnd - usedBPInline - usedInlineSize

			if autoInlineStart && autoInlineEnd {
				halfMargin := remaining / 2
				if halfMargin < 0 {
					halfMargin = 0
				}
				inlineOffset = insets.InlineStart + halfMargin
			} else if autoInlineStart {
				inlineOffset = insets.InlineStart + remaining - childMargins.InlineEnd
			} else if autoInlineEnd {
				inlineOffset = insets.InlineStart + childMargins.InlineStart
			} else {
				// Overconstrained: LTR ignores inline-end, RTL ignores inline-start.
				if wdm.IsRTL() {
					inlineOffset = cbInline - insets.InlineEnd - childMargins.InlineEnd - childLogical.InlineSize()
				} else {
					inlineOffset = insets.InlineStart + childMargins.InlineStart
				}
			}
		} else if insets.HasInlineStart {
			inlineOffset = insets.InlineStart + childMargins.InlineStart
		} else if insets.HasInlineEnd {
			inlineOffset = cbInline - insets.InlineEnd - childMargins.InlineEnd - childLogical.InlineSize()
		} else {
			// Both auto: use static position.
			inlineOffset = staticInline + childMargins.InlineStart
		}

		// --- Block axis ---
		var blockOffset float64
		if insets.HasBlockStart && insets.HasBlockEnd {
			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			usedBlockSize := childLogical.BlockSize()
			usedBPBlock := childGeom.BlockBorderPadding()
			remaining := cbBlock - insets.BlockStart - insets.BlockEnd - usedBPBlock - usedBlockSize

			if autoBlockStart && autoBlockEnd {
				halfMargin := remaining / 2
				if halfMargin < 0 {
					halfMargin = 0
				}
				blockOffset = insets.BlockStart + halfMargin
			} else if autoBlockStart {
				blockOffset = insets.BlockStart + remaining - childMargins.BlockEnd
			} else if autoBlockEnd {
				blockOffset = insets.BlockStart + childMargins.BlockStart
			} else {
				blockOffset = insets.BlockStart + childMargins.BlockStart
			}
		} else if insets.HasBlockStart {
			blockOffset = insets.BlockStart + childMargins.BlockStart
		} else if insets.HasBlockEnd {
			blockOffset = cbBlock - insets.BlockEnd - childMargins.BlockEnd - childLogical.BlockSize()
		} else {
			// Both auto: use static position.
			blockOffset = staticBlock + childMargins.BlockStart
		}

		builder.AddChild(childResult.Fragment, LogicalOffset{
			InlineOffset: inlineOffset,
			BlockOffset:  blockOffset,
		})
	}
}
