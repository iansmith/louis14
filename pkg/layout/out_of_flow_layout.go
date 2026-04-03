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

	// StaticPosition is the position the element would have in normal flow,
	// with edge annotations for alignment. Used when insets are 'auto'.
	// Expressed in the containing block's logical coordinates.
	//
	// Mirrors Blink's LogicalStaticPosition.
	StaticPosition LogicalStaticPosition

	// IsFixedPosition distinguishes position:fixed from position:absolute.
	// Fixed elements propagate past positioned ancestors to the viewport (ICB),
	// unless an ancestor with transform/filter/will-change creates a containing
	// block. Absolute elements stop at the nearest positioned ancestor.
	//
	// Mirrors Blink's NGOutOfFlowPositionedNode::is_for_fragmentation / inline_container
	// type tracking in the candidate.
	IsFixedPosition bool
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
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)

		// Blink's cross-WM static position conversion:
		// The static position is in the containing block's logical coordinates.
		// If the child has a different writing mode, convert:
		//   container-logical → physical → child-logical
		// Then back to container-logical for the constraint equation.
		// For now, we keep the static position in the CB's logical coordinates
		// since the constraint equation runs in CB coordinates.
		staticInline := candidate.StaticPosition.Offset.InlineOffset
		staticBlock := candidate.StaticPosition.Offset.BlockOffset

		// Pre-compute all values needed for both sizing and positioning.
		// Resolve physical insets, then convert to logical in CB's writing mode.
		physOffset := childStyle.GetPositionOffsetResolved(cbPhys.Width, cbPhys.Height)
		insets := PhysicalInsetsToLogical(physOffset, wdm)

		// Resolve margins and auto-margin flags in CB's logical coordinates.
		childMargins := ResolveMargins(childStyle, wdm, cbInline)
		rawMargin := childStyle.GetMargin()
		autoInlineStart, autoInlineEnd, autoBlockStart, autoBlockEnd :=
			PhysicalAutoMarginsToLogical(rawMargin, wdm)

		// Compute child's fragment geometry (border/padding) in the child's WDM.
		childGeom := ComputeFragmentGeometry(childStyle, childWDM)

		// Determine if child and CB share the same inline axis.
		parallel := wdm.IsVertical() == childWDM.IsVertical()

		// --- Two-pass sizing (Blink: ComputeOutOfFlowInlineDimensions) ---
		// When both insets are set and the corresponding dimension is auto,
		// CSS §10.3.7 / §10.6.4 require computing the size from the
		// constraint equation BEFORE layout.

		availInline := cbInline
		availBlock := cbBlock
		useFixedInline := false
		useFixedBlock := false

		if insets.HasInlineStart && insets.HasInlineEnd {
			if isAutoSizeInDirection(childStyle, wdm, true) {
				// Child's border+padding in CB's inline direction.
				var childBPInline float64
				if parallel {
					childBPInline = childGeom.InlineBorderPadding()
				} else {
					childBPInline = childGeom.BlockBorderPadding()
				}
				// Auto margins are treated as 0 for constraint equation.
				mStart := childMargins.InlineStart
				mEnd := childMargins.InlineEnd
				if autoInlineStart {
					mStart = 0
				}
				if autoInlineEnd {
					mEnd = 0
				}
				contentInline := cbInline - insets.InlineStart - insets.InlineEnd -
					mStart - mEnd - childBPInline
				if contentInline < 0 {
					contentInline = 0
				}
				availInline = contentInline + childBPInline // border-box
				useFixedInline = true
			}
		}

		if insets.HasBlockStart && insets.HasBlockEnd && cbBlock != Indefinite {
			if isAutoSizeInDirection(childStyle, wdm, false) {
				var childBPBlock float64
				if parallel {
					childBPBlock = childGeom.BlockBorderPadding()
				} else {
					childBPBlock = childGeom.InlineBorderPadding()
				}
				mStart := childMargins.BlockStart
				mEnd := childMargins.BlockEnd
				if autoBlockStart {
					mStart = 0
				}
				if autoBlockEnd {
					mEnd = 0
				}
				contentBlock := cbBlock - insets.BlockStart - insets.BlockEnd -
					mStart - mEnd - childBPBlock
				if contentBlock < 0 {
					contentBlock = 0
				}
				availBlock = contentBlock + childBPBlock
				useFixedBlock = true
			}
		}

		// Build constraint space for the absolute child.
		csb := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetAvailableSize(LogicalSize{
				InlineSize: availInline,
				BlockSize:  availBlock,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: cbInline,
				BlockSize:  cbBlock,
			})
		if useFixedInline {
			csb.SetIsFixedInlineSize(true)
		}
		if useFixedBlock {
			csb.SetIsFixedBlockSize(true)
		}
		childSpace := csb.Build()

		childResult := layoutElement(p.ctx, child, childSpace)
		childLogical := NewLogicalFragment(wdm, childResult.Fragment)

		// CSS 2.1 §10.3.7 / §10.6.4: Solve inline and block constraint
		// equations entirely in logical coordinates.

		// --- Inline axis ---
		// CSS 2.1 §10.3.7: The constraint equation is:
		//   inset-start + margin-start + border-box-width + margin-end + inset-end = CB width
		// childLogical.InlineSize() returns the border-box size, so "remaining"
		// is the space available for auto margins.
		var inlineOffset float64
		if insets.HasInlineStart && insets.HasInlineEnd {
			usedInlineSize := childLogical.InlineSize()
			remaining := cbInline - insets.InlineStart - insets.InlineEnd - usedInlineSize

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
					inlineOffset = cbInline - insets.InlineEnd - childMargins.InlineEnd - usedInlineSize
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
		// Same pattern: border-box size already includes border+padding.
		var blockOffset float64
		if insets.HasBlockStart && insets.HasBlockEnd {
			usedBlockSize := childLogical.BlockSize()
			remaining := cbBlock - insets.BlockStart - insets.BlockEnd - usedBlockSize

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
