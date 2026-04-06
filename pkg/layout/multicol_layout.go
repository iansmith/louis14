package layout

import (
	"math"

	"louis14/pkg/css"
)

// MulticolLayoutAlgorithm implements CSS Multi-column Layout Module Level 1.
// It resolves column count/width per §3.4, lays out content into columns,
// and positions columns side by side with gaps.
//
// This is a simplified implementation that distributes whole block-level
// children and line boxes across columns. Full mid-block fragmentation
// (break tokens) is a future enhancement.
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
	// §3.4: If both column-width and column-count are auto, not multicol.
	// (Caller already checked isMulticolContainer.)

	var N int
	if colWidth == 0 {
		// column-width: auto — use column-count directly
		N = colCount
	} else if colCount == 0 {
		// column-count: auto — compute from available width
		N = int(math.Max(1, math.Floor((availableInline+gap)/(colWidth+gap))))
	} else {
		// Both specified — column-count is max, column-width is min floor
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

	// §3.4: W = max(0, ((U + gap) / N - gap))
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

	// Step 1: Lay out all children as a single tall block to get their sizes.
	// Use a constraint space with the column width as the inline-size.
	childFragments := mla.layoutChildrenAsBlock(wdm, usedColWidth, geom)

	// Step 2: Compute total content height.
	// Use the physical block-size dimension based on writing mode.
	fragBlockSize := func(f *PhysicalFragment) float64 {
		if wdm.IsHorizontal() {
			return f.Size.Height
		}
		return f.Size.Width
	}
	totalHeight := 0.0
	for _, cf := range childFragments {
		totalHeight += fragBlockSize(cf)
	}

	// Step 3: Determine column height.
	var colHeight float64
	if hasExplicitBlock {
		colHeight = explicitBlockSize
	} else {
		// Balance: divide content evenly across columns.
		colHeight = math.Ceil(totalHeight / float64(numCols))
		if colHeight < 1 {
			colHeight = 1
		}
	}

	// Step 4: Distribute child fragments into columns.
	// We track the current block offset within the current column.
	type columnData struct {
		fragments []childWithOffset
		height    float64
	}
	columns := make([]columnData, 0, numCols)
	columns = append(columns, columnData{})

	currentCol := 0
	colOffset := 0.0

	for _, cf := range childFragments {
		fragHeight := fragBlockSize(cf)

		// If adding this fragment would overflow and we have room for more columns,
		// start a new column — but always place at least one fragment per column.
		if colOffset > 0 && colOffset+fragHeight > colHeight && currentCol+1 < numCols {
			if colOffset > columns[currentCol].height {
				columns[currentCol].height = colOffset
			}
			currentCol++
			columns = append(columns, columnData{})
			colOffset = 0
		}

		columns[currentCol].fragments = append(columns[currentCol].fragments, childWithOffset{
			fragment:    cf,
			blockOffset: colOffset,
		})
		colOffset += fragHeight
		if colOffset > columns[currentCol].height {
			columns[currentCol].height = colOffset
		}
	}

	// Step 5: Position columns side by side and add fragments to builder.
	// Each column is an anonymous fragment positioned at an inline offset.
	maxColHeight := 0.0
	for _, col := range columns {
		if col.height > maxColHeight {
			maxColHeight = col.height
		}
	}

	inlineOffset := 0.0
	for _, col := range columns {
		for _, cwf := range col.fragments {
			// Position: inline offset for the column + fragment's own inline position.
			offset := LogicalOffset{
				InlineOffset: inlineOffset,
				BlockOffset:  cwf.blockOffset,
			}
			builder.AddChild(cwf.fragment, offset)
		}
		inlineOffset += usedColWidth + gap
	}

	// Final block size: the tallest column (or explicit height).
	finalBlockSize := maxColHeight
	if hasExplicitBlock {
		finalBlockSize = explicitBlockSize
	}

	// Handle out-of-flow candidates (absolute/fixed positioned children).
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

	result := builder.Build()
	result.PropagatedOOFCandidates = propagatedOOF
	return result
}

// childWithOffset pairs a fragment with its block offset within a column.
type childWithOffset struct {
	fragment    *PhysicalFragment
	blockOffset float64
}

// layoutChildrenAsBlock lays out all children in a single block flow using
// the column width as the available inline-size. Returns the child fragments
// in order.
func (mla *MulticolLayoutAlgorithm) layoutChildrenAsBlock(
	wdm WritingDirectionMode,
	columnWidth float64,
	geom FragmentGeometry,
) []*PhysicalFragment {
	var fragments []*PhysicalFragment

	for _, child := range mla.node.Children() {
		childStyle := child.Style()

		// Skip display:none children.
		if childStyle != nil && childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		// Build constraint space for the child using column width.
		childSpace := ConstraintSpace{
			AvailableSize: LogicalSize{
				InlineSize: columnWidth,
				BlockSize:  Indefinite,
			},
			PercentageResolutionSize: LogicalSize{
				InlineSize: columnWidth,
				BlockSize:  Indefinite,
			},
			PercentageResolutionInlineSize: columnWidth,
			WritingDirection:               wdm,
		}

		// Handle out-of-flow children.
		if childStyle != nil {
			pos := childStyle.GetPosition()
			if pos == css.PositionAbsolute || pos == css.PositionFixed {
				continue // will be handled by OOF layout
			}
		}

		result := layoutElement(mla.ctx, child, childSpace)
		if result != nil && result.Fragment != nil {
			fragments = append(fragments, result.Fragment)
		}
	}

	return fragments
}
