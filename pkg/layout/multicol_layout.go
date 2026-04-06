package layout

import (
	"math"

	"louis14/pkg/css"
)

// MulticolLayoutAlgorithm implements CSS Multi-column Layout Module Level 1.
// It resolves column count/width per §3.4, creates a fragmentation context,
// and delegates content layout to the fragmentation-aware BlockLayoutAlgorithm.
//
// Mirrors Blink's ColumnLayoutAlgorithm.
type MulticolLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace
}

// NewMulticolLayoutAlgorithm creates a multicol layout algorithm.
func NewMulticolLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *MulticolLayoutAlgorithm {
	return &MulticolLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: node.Style(),
		space: space,
	}
}

// isMulticolContainer returns true if the style triggers multi-column layout.
func isMulticolContainer(style *css.Style) bool {
	if style == nil {
		return false
	}
	return style.GetColumnCount() > 0 || style.GetColumnWidth() > 0
}

// resolveColumnCount implements CSS Multicol §3.4 — the pseudo-algorithm
// for resolving column-count and column-width into used values.
// Returns (usedColumnCount, usedColumnWidth).
func resolveColumnCount(availableInline float64, colCount int, colWidth float64, gap float64) (int, float64) {
	var N int
	if colWidth == 0 {
		N = colCount
	} else if colCount == 0 {
		N = int(math.Max(1, math.Floor((availableInline+gap)/(colWidth+gap))))
	} else {
		fromWidth := int(math.Floor((availableInline + gap) / (colWidth + gap)))
		if fromWidth < 1 {
			fromWidth = 1
		}
		N = colCount
		if fromWidth < N {
			N = fromWidth
		}
	}
	if N < 1 {
		N = 1
	}
	W := math.Max(0, (availableInline+gap)/float64(N)-gap)
	return N, W
}

// Layout performs multi-column layout and returns the result.
func (mla *MulticolLayoutAlgorithm) Layout() *LayoutResult {
	wdm := mla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(mla.ctx, mla.node, mla.style, wdm, mla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(mla.node)

	contentInlineSize := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if contentInlineSize < 0 {
		contentInlineSize = 0
	}

	// Resolve column parameters.
	colCount := mla.style.GetColumnCount()
	colWidth := mla.style.GetColumnWidth()
	gap := mla.style.GetColumnGapMulticol()
	numCols, usedColWidth := resolveColumnCount(contentInlineSize, colCount, colWidth, gap)

	// Block-size: use explicit height if set, else auto (balance).
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
	}

	// Create the anonymous content node wrapping multicol children.
	contentNode := mla.contentNode()

	// Determine column height.
	var colHeight float64
	if hasExplicitBlock {
		colHeight = explicitBlockSize
	} else {
		// Column balancing: lay out unconstrained to get total content height,
		// then balance across columns with iterative refinement.
		colHeight = mla.balanceColumnHeight(contentNode, wdm, usedColWidth, numCols, gap)
	}

	// Layout columns using fragmentation.
	var breakToken *BlockBreakToken
	inlineOffset := 0.0
	maxColHeight := 0.0
	actualCols := 0

	for col := 0; col < numCols; col++ {
		// Create constraint space for this column with fragmentation.
		colSpace := NewConstraintSpaceBuilder(wdm, wdm, true). // New BFC per column
			SetAvailableSize(LogicalSize{
				InlineSize: usedColWidth,
				BlockSize:  colHeight,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: usedColWidth,
				BlockSize:  colHeight,
			}).
			SetPercentageResolutionInlineSize(usedColWidth).
			SetIsFixedBlockSize(true).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(colHeight).
			SetFragmentainerOffset(0).
			SetBlockFragmentationType(FragmentColumn).
			SetBreakToken(breakToken).
			Build()

		// Delegate to BlockLayoutAlgorithm.
		result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, colSpace).Layout()
		if result == nil || result.Fragment == nil {
			break
		}

		// Add this column's fragment at the correct inline offset.
		builder.AddChild(result.Fragment, LogicalOffset{
			InlineOffset: inlineOffset,
			BlockOffset:  0,
		})

		// Track actual column height for sizing.
		colBlockSize := NewLogicalFragment(wdm, result.Fragment).BlockSize()
		if colBlockSize > maxColHeight {
			maxColHeight = colBlockSize
		}
		actualCols++

		// Check if all content is exhausted.
		breakToken = result.BreakToken
		if breakToken == nil {
			break
		}

		inlineOffset += usedColWidth + gap
	}

	// If break token still exists after max columns, content overflows.
	// (CSS spec says content overflows the last column.)

	// Final block size.
	finalBlockSize := maxColHeight
	if hasExplicitBlock {
		finalBlockSize = explicitBlockSize
	}

	// Handle out-of-flow candidates.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		isPositioned := mla.style != nil && mla.style.GetPosition() != css.PositionStatic
		if isPositioned {
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
					ctx:                 mla.ctx,
					containingBlockWDM:  wdm,
					containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
					geom:                geom,
				}
				oofPart.LayoutCandidates(absoluteCandidates, builder)
			}
			propagatedOOF = fixedCandidates
		} else {
			propagatedOOF = builder.outOfFlowCandidates
		}
	}

	// Set final size and box data.
	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})
	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(mla.style, wdm, mla.space.AvailableSize.InlineSize), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})
	builder.SetIntrinsicBlockSize(maxColHeight)

	result := builder.Build()
	result.PropagatedOOFCandidates = propagatedOOF
	return result
}

// contentNode creates an anonymous LayoutInputNode wrapping the multicol
// container's children. This mirrors Blink's "flow thread" concept — a
// single block node containing all multicol content that gets fragmented
// across columns.
func (mla *MulticolLayoutAlgorithm) contentNode() *LayoutInputNode {
	return &LayoutInputNode{
		style:       mla.style,
		children:    mla.node.Children(),
		isAnonymous: true,
	}
}

// balanceColumnHeight computes a balanced column height by doing an
// unconstrained layout first, then iteratively refining.
func (mla *MulticolLayoutAlgorithm) balanceColumnHeight(
	contentNode *LayoutInputNode,
	wdm WritingDirectionMode,
	colWidth float64,
	numCols int,
	gap float64,
) float64 {
	// Step 1: Unconstrained layout to get total content height.
	unconstrainedSpace := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  Indefinite,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  0,
		}).
		SetPercentageResolutionInlineSize(colWidth).
		Build()

	unconstrainedResult := NewBlockLayoutAlgorithm(mla.ctx, contentNode, unconstrainedSpace).Layout()
	if unconstrainedResult == nil {
		return 1
	}
	totalHeight := unconstrainedResult.IntrinsicBlockSize
	if totalHeight <= 0 {
		return 1
	}

	// Step 2: Initial estimate.
	estimate := math.Ceil(totalHeight / float64(numCols))
	if estimate < 1 {
		estimate = 1
	}

	// Step 3: Iterative balancing (max numCols+1 iterations).
	for iter := 0; iter < numCols+1; iter++ {
		actualCols, minShortage := mla.trialLayout(contentNode, wdm, colWidth, estimate, numCols)

		if actualCols <= numCols && minShortage == 0 {
			break // Balanced
		}

		if minShortage > 0 {
			estimate += minShortage
		} else {
			break // No progress possible
		}
	}

	return estimate
}

// trialLayout performs a trial fragmented layout to check how many columns
// are used and what the minimum space shortage is.
func (mla *MulticolLayoutAlgorithm) trialLayout(
	contentNode *LayoutInputNode,
	wdm WritingDirectionMode,
	colWidth float64,
	colHeight float64,
	maxCols int,
) (actualCols int, minShortage float64) {
	var breakToken *BlockBreakToken
	actualCols = 0

	for col := 0; col < maxCols; col++ {
		colSpace := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{
				InlineSize: colWidth,
				BlockSize:  colHeight,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: colWidth,
				BlockSize:  colHeight,
			}).
			SetPercentageResolutionInlineSize(colWidth).
			SetIsFixedBlockSize(true).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(colHeight).
			SetFragmentainerOffset(0).
			SetBlockFragmentationType(FragmentColumn).
			SetBreakToken(breakToken).
			Build()

		result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, colSpace).Layout()
		if result == nil {
			break
		}
		actualCols++

		if result.MinSpaceShortage > 0 && (minShortage == 0 || result.MinSpaceShortage < minShortage) {
			minShortage = result.MinSpaceShortage
		}

		breakToken = result.BreakToken
		if breakToken == nil {
			break
		}
	}

	return actualCols, minShortage
}
