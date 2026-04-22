package layout

import (
	"math"

	"louis14/pkg/css"
)

// MulticolLayoutAlgorithm implements CSS Multi-column Layout Module Level 1.
// Mirrors Blink's ColumnLayoutAlgorithm (column_layout_algorithm.{h,cc}).
//
// Phase 12a: fragmentation infrastructure — outer stretch / inner column loop,
// MinimalSpaceShortage feedback, inline content fragmentation.
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

// ResolveColumnCountForPaint is a public wrapper around resolveColumnCount
// for use by the paint layer builder.
func ResolveColumnCountForPaint(availableInline float64, colCount int, colWidth float64, gap float64) (int, float64) {
	return resolveColumnCount(availableInline, colCount, colWidth, gap)
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
// Mirrors Blink's ColumnLayoutAlgorithm::Layout().
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

	// Container explicit block-size (from CSS height/max-height).
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
	}

	// column-fill: balance forces balanced column distribution.
	// Also forced when nested inside an outer initial-balancing pass.
	columnFill := mla.style.GetColumnFill()
	balanceColumns := columnFill == "balance" || columnFill == "" ||
		(mla.space.HasBlockFragmentation && !mla.space.IsInitialColumnBalancingPass &&
			mla.space.FragmentainerBlockSize == Indefinite)

	// Split children into segments separated by column-span:all elements.
	type segment struct {
		isSpanner bool
		children  []*LayoutInputNode
		spanner   *LayoutInputNode
	}
	var segments []segment
	var currentChildren []*LayoutInputNode
	for _, child := range mla.node.Children() {
		childStyle := child.Style()
		if childStyle != nil && childStyle.GetColumnSpan() == "all" {
			if len(currentChildren) > 0 {
				segments = append(segments, segment{children: currentChildren})
				currentChildren = nil
			}
			segments = append(segments, segment{isSpanner: true, spanner: child})
		} else {
			currentChildren = append(currentChildren, child)
		}
	}
	if len(currentChildren) > 0 {
		segments = append(segments, segment{children: currentChildren})
	}

	blockCursor := 0.0

	for _, seg := range segments {
		if seg.isSpanner {
			// Column-span:all: lay out at full container width.
			spannerSpace := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{
					InlineSize: contentInlineSize,
					BlockSize:  Indefinite,
				}).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: contentInlineSize,
					BlockSize:  0,
				}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()

			spanResult := layoutElement(mla.ctx, seg.spanner, spannerSpace)
			if spanResult != nil && spanResult.Fragment != nil {
				builder.AddChild(spanResult.Fragment, LogicalOffset{
					InlineOffset: 0,
					BlockOffset:  blockCursor,
				})
				spanBlockSize := NewLogicalFragment(wdm, spanResult.Fragment).BlockSize()
				blockCursor += spanBlockSize
			}
			continue
		}

		// Column row: lay out via LayoutLine.
		contentNode := &LayoutInputNode{
			style:       mla.style,
			children:    seg.children,
			isAnonymous: true,
		}

		rowBlockAdvance := mla.layoutLine(
			contentNode, wdm, usedColWidth, numCols, gap,
			balanceColumns, hasExplicitBlock, explicitBlockSize,
			blockCursor, builder,
		)
		blockCursor += rowBlockAdvance
	}

	// Final block size.
	finalBlockSize := blockCursor
	if hasExplicitBlock && finalBlockSize < explicitBlockSize {
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
				if extra := oofPart.LayoutCandidates(absoluteCandidates, builder); len(extra) > 0 {
					fixedCandidates = append(fixedCandidates, extra...)
				}
			}
			propagatedOOF = fixedCandidates
		} else {
			propagatedOOF = builder.outOfFlowCandidates
		}
	}

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
	builder.SetIntrinsicBlockSize(blockCursor)

	result := builder.Build()
	result.PropagatedOOFCandidates = propagatedOOF
	return result
}

// layoutLine implements the outer stretch / inner per-column loop for one
// "column row" (all columns between two spanners, or the whole container if
// no spanners). Returns the block-size consumed by this row.
//
// Mirrors Blink's ColumnLayoutAlgorithm::LayoutLine() (cla.cc:858).
func (mla *MulticolLayoutAlgorithm) layoutLine(
	contentNode *LayoutInputNode,
	wdm WritingDirectionMode,
	usedColWidth float64,
	numCols int,
	gap float64,
	balanceColumns bool,
	hasExplicitBlock bool,
	explicitBlockSize float64,
	lineOffset float64,
	builder *BoxFragmentBuilder,
) float64 {
	// Determine initial column block-size.
	var colBlockSize float64
	if balanceColumns {
		// Balanced: estimate from content, then iterate via shortage.
		colBlockSize = mla.resolveColumnAutoBlockSize(contentNode, wdm, usedColWidth, numCols, gap)
		colBlockSize = mla.constrainColumnBlockSize(colBlockSize, hasExplicitBlock, explicitBlockSize)
	} else if hasExplicitBlock {
		// Sequential fill with known container height.
		colBlockSize = explicitBlockSize
	} else {
		// Sequential fill, auto height: columns grow to content.
		colBlockSize = Indefinite
	}

	var maxColHeight float64
	var finalColumns []struct {
		fragment *PhysicalFragment
		offset   LogicalOffset
	}

	// Outer stretch loop: mirrors Blink's do { ... } while (true) at cla.cc:967.
	for {
		var columns []struct {
			fragment *PhysicalFragment
			offset   LogicalOffset
		}
		minSpaceShortage := math.MaxFloat64
		hasShortage := false
		actualColumnCount := 0
		forcedBreakCount := 0
		hasViolatingBreak := false

		var colBreakToken *BlockBreakToken
		inlineOffset := 0.0

		// Inner per-column loop.
		for col := 0; col < numCols || colBlockSize == Indefinite; col++ {
			colSpace := mla.createConstraintSpaceForColumn(wdm, usedColWidth, colBlockSize, lineOffset, colBreakToken, balanceColumns)
			result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, colSpace).Layout()
			if result == nil || result.Fragment == nil {
				break
			}

			colFrag := result.Fragment
			colHeight := NewLogicalFragment(wdm, colFrag).BlockSize()

			columns = append(columns, struct {
				fragment *PhysicalFragment
				offset   LogicalOffset
			}{
				fragment: colFrag,
				offset:   LogicalOffset{InlineOffset: inlineOffset, BlockOffset: lineOffset},
			})

			// Track shortage for balancing stretch.
			if result.MinSpaceShortage > 0 {
				if result.MinSpaceShortage < minSpaceShortage {
					minSpaceShortage = result.MinSpaceShortage
					hasShortage = true
				}
			}

			actualColumnCount++
			if result.HasForcedBreak {
				forcedBreakCount++
			}
			if colBlockSize != Indefinite && colHeight > colBlockSize {
				hasViolatingBreak = true
			}

			colBreakToken = result.BreakToken
			if colBreakToken == nil {
				// All content fit in this column — done.
				break
			}
			inlineOffset += usedColWidth + gap

			// Stop inner loop when we've filled all columns (but content
			// continues — will be handled by a spanner or caller).
			if col+1 >= numCols {
				break
			}
		}

		finalColumns = columns

		// For sequential fill (non-balanced), accept first layout.
		if !balanceColumns {
			break
		}

		// Acceptance condition: content fits with no violations and used
		// at most numCols columns, with no remaining break token.
		if !hasViolatingBreak &&
			actualColumnCount <= numCols &&
			(colBreakToken == nil) {
			break
		}

		// Cannot make progress: all breaks are forced.
		if numCols <= forcedBreakCount+1 {
			break
		}

		if !hasShortage {
			break
		}

		// Stretch column height by the minimum shortage and retry.
		newColBlockSize := colBlockSize + minSpaceShortage
		newColBlockSize = mla.constrainColumnBlockSize(newColBlockSize, hasExplicitBlock, explicitBlockSize)
		if newColBlockSize <= colBlockSize {
			// Propagate shortage outward for nested balanced columns.
			if mla.space.IsInsideBalancedColumns && !mla.space.IsInitialColumnBalancingPass {
				// (shortage propagation to outer builder deferred to Phase 12c)
			}
			break
		}
		colBlockSize = newColBlockSize
	}

	// Commit final columns to the parent builder.
	maxColHeight = 0.0
	for _, col := range finalColumns {
		builder.AddChild(col.fragment, col.offset)
		h := NewLogicalFragment(wdm, col.fragment).BlockSize()
		if h > maxColHeight {
			maxColHeight = h
		}
	}

	return maxColHeight
}

// resolveColumnAutoBlockSize estimates the balanced column height by doing an
// unconstrained layout of the content to get total height, then dividing by
// the number of columns. Mirrors Blink's ResolveColumnAutoBlockSizeInternal().
func (mla *MulticolLayoutAlgorithm) resolveColumnAutoBlockSize(
	contentNode *LayoutInputNode,
	wdm WritingDirectionMode,
	colWidth float64,
	numCols int,
	gap float64,
) float64 {
	// Unconstrained layout: ignore the container's CSS height, let content flow
	// freely to measure its intrinsic block-size.
	// IsContentSuggestionLayout suppresses the element's own CSS block-size
	// (height: 3em etc.) so we see the true content height.
	// IsInitialColumnBalancingPass disables inline fragmentation breaks during
	// this measurement pass.
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
		SetIsContentSuggestionLayout(true).
		SetIsInitialColumnBalancingPass(true).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		Build()

	result := NewBlockLayoutAlgorithm(mla.ctx, contentNode, unconstrainedSpace).Layout()
	if result == nil {
		return 1
	}
	totalHeight := result.IntrinsicBlockSize
	if totalHeight <= 0 {
		return 1
	}

	// Initial estimate: ceil-divide total height across columns.
	estimate := math.Ceil(totalHeight / float64(numCols))
	if estimate < 1 {
		estimate = 1
	}
	return estimate
}

// constrainColumnBlockSize clamps the candidate column height to the
// container's explicit height (if any) and ensures it is positive.
// Mirrors Blink's ConstrainColumnBlockSize() (cla.cc:1974).
func (mla *MulticolLayoutAlgorithm) constrainColumnBlockSize(
	size float64,
	hasExplicitBlock bool,
	explicitBlockSize float64,
) float64 {
	if size < 1 {
		size = 1
	}
	if hasExplicitBlock && size > explicitBlockSize {
		size = explicitBlockSize
	}
	return size
}

// createConstraintSpaceForColumn builds the constraint space for a single
// column fragmentainer. Mirrors Blink's CreateConstraintSpaceForFragmentainer()
// (fragmentation_utils.cc).
func (mla *MulticolLayoutAlgorithm) createConstraintSpaceForColumn(
	wdm WritingDirectionMode,
	colWidth float64,
	colBlockSize float64,
	lineOffset float64,
	breakToken *BlockBreakToken,
	balanceColumns bool,
) ConstraintSpace {
	availBlock := colBlockSize
	isFixed := colBlockSize != Indefinite

	b := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  availBlock,
		}).
		SetPercentageResolutionSize(LogicalSize{
			InlineSize: colWidth,
			BlockSize:  availBlock,
		}).
		SetPercentageResolutionInlineSize(colWidth).
		SetHasBlockFragmentation(true).
		SetBlockFragmentationType(FragmentColumn).
		SetIsInsideBalancedColumns(balanceColumns)

	if isFixed {
		// IsBlockSizeOverride + IsFixedBlockSize: the column height overrides any
		// CSS block-size on the content node (e.g. the multicol container's own
		// height:3em). This ensures the column fragment is exactly colBlockSize tall
		// and fragmentation breaks at the right boundary.
		// Mirrors Blink: column constraint space always has HasKnownFragmentainerBlockSize.
		b = b.SetIsFixedBlockSize(true).
			SetIsBlockSizeOverride(true).
			SetFragmentainerBlockSize(colBlockSize).
			SetFragmentainerOffset(lineOffset)
	}

	if breakToken != nil {
		b = b.SetBreakToken(breakToken)
	}

	return b.Build()
}
