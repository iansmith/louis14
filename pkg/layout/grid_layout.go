package layout

import (
	"math"
	"strings"

	"louis14/pkg/css"
)

// GridLayoutAlgorithm implements the CSS Grid Layout Module Level 1.
// It resolves grid tracks, places items, sizes tracks, and positions items.
type GridLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace
}

// NewGridLayoutAlgorithm creates a grid layout algorithm for the given node.
func NewGridLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *GridLayoutAlgorithm {
	return &GridLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: node.Style(),
		space: space,
	}
}

// gridItem represents a grid item with its placement info and layout result.
type gridItem struct {
	node     *LayoutInputNode
	style    *css.Style
	colStart int // 0-indexed column start (-1 if auto)
	colEnd   int // 0-indexed column end (exclusive, -1 if auto)
	rowStart int // 0-indexed row start (-1 if auto)
	rowEnd   int // 0-indexed row end (exclusive, -1 if auto)
	// Span counts requested by the item via `grid-column/row: span N` when the
	// start is auto. Auto-placement (CSS Grid 2 §8.5) consumes these to size
	// the item as it slots into the grid. Default 1 when the item declared no
	// span (or when the span resolved into explicit start/end already).
	colSpan int
	rowSpan int
	result  *LayoutResult
	wdm     WritingDirectionMode
	margins LogicalEdges
}

// Layout performs CSS Grid layout.
func (gla *GridLayoutAlgorithm) Layout() *LayoutResult {
	wdm := gla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(gla.ctx, gla.node, gla.style, wdm, gla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(gla.node)

	// Resolve container sizes.
	contentInlineSize := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if contentInlineSize < 0 {
		contentInlineSize = 0
	}
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
	}

	// Parse grid template definitions with named lines.
	colTracks, colLineNames := gla.style.GetGridTemplateColumnsWithNames()
	rowTracks, rowLineNames := gla.style.GetGridTemplateRowsWithNames()
	rowGap, colGap := gla.style.GetGridGap()
	autoFlow := gla.style.GetGridAutoFlow()

	// Parse grid-template-areas for named area placement.
	namedAreas := gla.style.GetGridTemplateAreas()

	// Build list of grid children (skip OOF and whitespace-only text nodes).
	// CSS Grid §6.1: contiguous runs of non-whitespace text directly inside
	// the grid container are wrapped into anonymous block-level grid items so
	// that they can be placed by the auto-placement algorithm. Mirrors the
	// flex container's buildFlexChildren behaviour (CSS Flexbox §4).
	gridChildren := buildGridChildren(gla.node, gla.style)

	var items []*gridItem
	for _, child := range gridChildren {
		// Anonymous wrappers created by buildGridChildren have a style,
		// but original children that lost theirs (e.g. broken DOM) are
		// skipped defensively.
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			// Register OOF candidate.
			builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
				Node:            child,
				IsFixedPosition: pos == css.PositionFixed,
			})
			continue
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}
		items = append(items, &gridItem{
			node:     child,
			style:    childStyle,
			wdm:      wdm,
			colStart: -1,
			colEnd:   -1,
			rowStart: -1,
			rowEnd:   -1,
			colSpan:  1,
			rowSpan:  1,
		})
	}

	// Handle auto-fill/auto-fit BEFORE placement: repeat(auto-fill, …) /
	// repeat(auto-fit, …) is resolved against the container's free space
	// at layout time (CSS Grid 2 §7.2.2.1). Auto-placement and explicit
	// line resolution both depend on the final row and column counts, so we expand
	// the sentinels in both axes. Both axes expand auto-fill; when the available
	// size is indefinite, the repetition count resolves to 1 per CSS Grid 2 §7.2.2.1.
	colTracks = gla.expandAutoFillFit(colTracks, contentInlineSize, colGap)
	rowTracks = gla.expandAutoFillFit(rowTracks, explicitBlockSize, rowGap)

	// Resolve explicit placements from grid-column/grid-row/grid-area.
	for _, item := range items {
		gla.resolveItemPlacement(item, namedAreas, colLineNames, rowLineNames, len(colTracks), len(rowTracks))
	}

	// Auto-place items that don't have explicit positions.
	numCols := len(colTracks)
	numRows := len(rowTracks)
	// First, compute grid dimensions from explicit placements.
	for _, item := range items {
		if item.colEnd >= 0 && item.colEnd > numCols {
			numCols = item.colEnd
		}
		if item.rowEnd >= 0 && item.rowEnd > numRows {
			numRows = item.rowEnd
		}
	}
	// Ensure at least 1 column.
	if numCols == 0 {
		numCols = 1
	}

	// Auto-placement: fill unplaced items into the grid.
	gla.autoPlace(items, &numCols, &numRows, autoFlow)

	// Extend track definitions to cover all rows/columns.
	autoColTrack := css.GridTrack{Auto: true}
	if t := gla.style.GetGridAutoColumns(); t != nil {
		autoColTrack = *t
	}
	autoRowTrack := css.GridTrack{Auto: true}
	if t := gla.style.GetGridAutoRows(); t != nil {
		autoRowTrack = *t
	}
	colTracks = gla.extendTracks(colTracks, numCols, autoColTrack)
	rowTracks = gla.extendTracks(rowTracks, numRows, autoRowTrack)

	// --- Track sizing: columns ---
	colSizes := gla.resolveTrackSizes(colTracks, contentInlineSize, colGap, true, items, numCols, numRows, wdm, geom)

	// --- Layout items to determine row intrinsic sizes ---
	// First pass: layout items with resolved column sizes to get natural heights.
	itemLayouts := make([]*LayoutResult, len(items))
	for i, item := range items {
		// Compute the inline size available to this item (sum of spanned columns + gaps).
		itemInline := gla.spannedSize(colSizes, item.colStart, item.colEnd, colGap)

		// CSS Box Alignment 3 §6.1: stretch only applies when the box has
		// no explicit size in the relevant axis. A grid item with an
		// explicit inline-size (e.g. width:100px) must NOT be stretched to
		// the track size — Blink mirrors this by only setting
		// kFixedInlineSize when justify-self resolves to stretch AND the
		// item's inline-size is auto. Without this guard, a flex container
		// child of a grid with `width:100px` gets force-resized to the
		// track width, breaking the grid-container-as-flex-item tests.
		stretchInline := gla.shouldStretchInline(item.style, wdm)
		childWDM := NewWritingDirectionMode(item.style)
		// Use NewConstraintSpaceBuilder so orthogonal items (e.g. writing-mode:
		// vertical-rl inside a horizontal grid) get axis-swapped spaces.
		// For orthogonal items: the grid's inline (column width) becomes the child's
		// block; the grid's block (row height) becomes the child's inline.
		// Pass explicitBlockSize as the available block so orthogonal children know
		// the grid height when sizing their content (enabling correct line-wrapping).
		// Mirrors Blink's GridLayoutAlgorithm::CreateConstraintSpaceForLayout.
		firstPassBlock := float64(Indefinite)
		if hasExplicitBlock {
			firstPassBlock = explicitBlockSize
		}
		childSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, gla.ctx)).
			SetAvailableSize(LogicalSize{InlineSize: itemInline, BlockSize: firstPassBlock}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: itemInline, BlockSize: firstPassBlock}).
			SetPercentageResolutionInlineSize(itemInline).
			SetIsFixedInlineSize(stretchInline).
			Build()
		itemLayouts[i] = layoutElement(gla.ctx, item.node, childSpace)
		item.result = itemLayouts[i]
		item.margins = ResolveMargins(item.style, wdm, itemInline)
	}

	// --- Track sizing: rows ---
	// For row auto-sizing, use the maximum content height of items in each row.
	rowSizes := gla.resolveTrackSizes(rowTracks, explicitBlockSize, rowGap, false, items, numCols, numRows, wdm, geom)

	// For auto rows, size them to the maximum content block-size of their items.
	// In vertical writing modes, block-size is the physical width; in HTB it is height.
	for r := 0; r < numRows; r++ {
		if r < len(rowTracks) && !rowTracks[r].Auto && rowTracks[r].Fr == 0 {
			continue // fixed row
		}
		maxHeight := 0.0
		for _, item := range items {
			if item.rowStart <= r && item.rowEnd > r {
				span := item.rowEnd - item.rowStart
				if span <= 0 {
					span = 1
				}
				var itemH float64
				if wdm.IsVertical() {
					itemH = item.result.Fragment.Size.WidthF64()
				} else {
					itemH = item.result.Fragment.Size.HeightF64()
				}
				itemH += item.margins.BlockStart + item.margins.BlockEnd
				// Distribute item block-size evenly across spanned rows.
				contribution := itemH / float64(span)
				if contribution > maxHeight {
					maxHeight = contribution
				}
			}
		}
		if maxHeight > rowSizes[r] {
			rowSizes[r] = maxHeight
		}
	}

	// Distribute fr units for rows if container has explicit block size.
	if hasExplicitBlock {
		totalRowGaps := rowGap * float64(numRows-1)
		if numRows <= 1 {
			totalRowGaps = 0
		}
		gla.distributeFr(rowTracks, rowSizes, explicitBlockSize-totalRowGaps)
	}

	// Re-layout items that have percentage heights or need stretching.
	for i, item := range items {
		itemBlock := gla.spannedSize(rowSizes, item.rowStart, item.rowEnd, rowGap)
		itemInline := gla.spannedSize(colSizes, item.colStart, item.colEnd, colGap)

		// Check if item needs full block-size (stretch or percentage block-size).
		// The logical block-size CSS property is "height" in HTB and "width" in vertical modes.
		blockSizeProp := "height"
		if wdm.IsVertical() {
			blockSizeProp = "width"
		}
		needsRelayout := false
		if _, ok := item.style.GetPercentage(blockSizeProp); ok {
			needsRelayout = true
		}
		// Default alignment for grid items is stretch.
		selfAlign := gla.getAlignSelf(item.style)
		if selfAlign == "stretch" || selfAlign == "normal" {
			// Only stretch if item has auto block-size.
			if _, ok := item.style.Get(blockSizeProp); !ok {
				needsRelayout = true
			}
		}

		if needsRelayout && itemBlock > 0 {
			margins := item.margins
			availBlock := itemBlock - margins.BlockStart - margins.BlockEnd
			if availBlock < 0 {
				availBlock = 0
			}
			// CSS Box Alignment 3 §6.1: stretch only applies to auto sizes.
			// Match the first-pass guard so an item with explicit width keeps
			// its width on the re-layout pass too.
			stretchInline := gla.shouldStretchInline(item.style, wdm)
			childWDM2 := NewWritingDirectionMode(item.style)
			// For orthogonal items, availBlock becomes the child's available InlineSize
			// (after axis swap in NewConstraintSpaceBuilder). Use the grid container's
			// explicit block-size when available so the child's inline is capped to the
			// visible row height. Without this, an overflowing auto row (row > container)
			// gives the child too much inline space, causing incorrect line-wrapping.
			reLayoutBlock := availBlock
			if wdm.IsOrthogonalTo(childWDM2) && hasExplicitBlock && explicitBlockSize < availBlock {
				reLayoutBlock = explicitBlockSize
			}
			childSpace2 := NewConstraintSpaceBuilder(wdm, childWDM2, true).
				SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM2, gla.ctx)).
				SetAvailableSize(LogicalSize{InlineSize: itemInline, BlockSize: reLayoutBlock}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: itemInline, BlockSize: availBlock}).
				SetPercentageResolutionInlineSize(itemInline).
				SetIsFixedInlineSize(stretchInline).
				SetIsFixedBlockSize(selfAlign == "stretch" || selfAlign == "normal").
				Build()
			itemLayouts[i] = layoutElement(gla.ctx, item.node, childSpace2)
			item.result = itemLayouts[i]
		}
	}

	// --- Position items ---
	// Compute content-distribution offsets (justify-content, align-content).
	totalRowSize := gla.totalTrackSize(rowSizes, rowGap)

	justifyContent := "start"
	if v, ok := gla.style.Get("justify-content"); ok && v != "" {
		justifyContent = v
	}
	alignContent := "start"
	if v, ok := gla.style.Get("align-content"); ok && v != "" {
		alignContent = v
	}

	colOffsets := gla.computeTrackOffsets(colSizes, colGap, contentInlineSize, justifyContent, numCols)
	rowOffsets := gla.computeTrackOffsets(rowSizes, rowGap, totalRowSize, alignContent, numRows)
	if hasExplicitBlock {
		rowOffsets = gla.computeTrackOffsets(rowSizes, rowGap, explicitBlockSize, alignContent, numRows)
	}

	for _, item := range items {
		itemInline := gla.spannedSize(colSizes, item.colStart, item.colEnd, colGap)
		itemBlock := gla.spannedSize(rowSizes, item.rowStart, item.rowEnd, rowGap)

		// Item position = track offset + margin.
		margins := item.margins
		inlineOffset := colOffsets[item.colStart] + margins.InlineStart
		blockOffset := rowOffsets[item.rowStart] + margins.BlockStart

		// Align item within its grid area.
		fragSize := ToLogicalSize(geomSizeToOld(item.result.Fragment.Size), wdm.WM)
		fragInline := fragSize.InlineSize
		fragBlock := fragSize.BlockSize
		availInline := itemInline - margins.InlineStart - margins.InlineEnd
		availBlock := itemBlock - margins.BlockStart - margins.BlockEnd

		// justify-self alignment.
		justifySelf := gla.getJustifySelf(item.style)
		inlineOffset += gla.alignOffset(fragInline, availInline, justifySelf)

		// align-self alignment.
		selfAlign := gla.getAlignSelf(item.style)
		blockOffset += gla.alignOffset(fragBlock, availBlock, selfAlign)

		offset := LogicalOffset{InlineOffset: inlineOffset, BlockOffset: blockOffset}
		builder.AddChild(item.result.Fragment, offset)

		// Propagate OOF candidates from children.
		for _, cand := range item.result.PropagatedOOFCandidates {
			builder.AddOutOfFlowCandidate(cand)
		}
	}

	// Set first/last baseline from baseline-participating items in first/last rows.
	// Mirrors Blink GridLayoutAlgorithm::SetBaselineFromItems @ 4883d11f.
	// CSS Grid §7.1: first baseline = first row; last baseline = last row.
	lastRow := numRows - 1
	var gridFirstBL, gridLastBL float64
	for _, item := range items {
		if item.result.Baseline > 0 && item.rowStart == 0 {
			bl := geom.Border.BlockStart + geom.Padding.BlockStart +
				rowOffsets[0] + item.margins.BlockStart + item.result.Baseline
			if gridFirstBL == 0 || bl < gridFirstBL {
				gridFirstBL = bl
			}
		}
		if item.result.Baseline > 0 && item.rowStart == lastRow {
			bl := geom.Border.BlockStart + geom.Padding.BlockStart +
				rowOffsets[lastRow] + item.margins.BlockStart + item.result.Baseline
			if gridLastBL == 0 || bl < gridLastBL {
				gridLastBL = bl
			}
		}
	}
	if gridFirstBL > 0 {
		builder.SetBaseline(gridFirstBL)
	}
	if gridLastBL > 0 {
		builder.SetLastBaseline(gridLastBL)
	} else if gridFirstBL > 0 {
		builder.SetLastBaseline(gridFirstBL)
	}

	// Compute final block-size.
	intrinsicBlockSize := totalRowSize

	// CSS Contain 1 §4.2: size containment — collapse intrinsic block-size to 0
	// when no explicit block-size is set (see contain_utils.go).
	intrinsicBlockSize = sizeContainedIntrinsicBlockSize(gla.style, hasExplicitBlock, intrinsicBlockSize)

	finalBlockSize := intrinsicBlockSize
	if hasExplicitBlock {
		finalBlockSize = explicitBlockSize
	}

	// Apply min/max block constraints.
	// Intrinsic keywords (min-content / max-content / fit-content) resolve
	// against intrinsicBlockSize — the natural content height (total row size).
	minBlock := ResolveMinBlockSizeWithIntrinsic(gla.style, wdm, gla.space, geom, intrinsicBlockSize)
	if finalBlockSize < minBlock {
		finalBlockSize = minBlock
	}
	if maxBlock, hasMax := ResolveMaxBlockSizeWithIntrinsic(gla.style, wdm, gla.space, geom, intrinsicBlockSize); hasMax {
		if finalBlockSize > maxBlock {
			finalBlockSize = maxBlock
		}
	}

	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})
	builder.SetIntrinsicBlockSize(intrinsicBlockSize)

	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(gla.style, wdm, gla.space.AvailableSize.InlineSize.Float64()), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:    physMargin,
		Border:    physBorder,
		Padding:   physPadding,
		Scrollbar: physScrollbar,
	})

	// Layout OOF children.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		isPositioned := gla.style != nil && gla.style.GetPosition() != css.PositionStatic
		isContainmentCB := gla.style != nil && (gla.style.ShouldApplyLayoutContainment() || gla.style.ShouldApplyPaintContainment())
		// CSS Will Change §2.2: `will-change: position` (and other abspos-only
		// CB triggers) makes this grid container a CB for abs-pos descendants
		// but not for fixed. Mirrors block_layout's isWillChangeAbsposCB.
		isWillChangeAbsposCB := false
		if gla.style != nil && !isContainmentCB && !isPositioned {
			if gla.style.WillChangeEstablishesContainingBlock(false) {
				isWillChangeAbsposCB = true
			}
		}
		if isContainmentCB {
			oofPart := &OutOfFlowLayoutPart{
				ctx:                 gla.ctx,
				containingBlockWDM:  wdm,
				containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
				geom:                geom,
				resolvesFixed:       true,
			}
			oofPart.LayoutCandidates(builder.outOfFlowCandidates, builder)
		} else if isPositioned || isWillChangeAbsposCB {
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
					ctx:                 gla.ctx,
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

	result := builder.Build()
	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	// CSS position:relative offset. Sticky is scroll-time, not layout-time.
	if gla.style != nil && gla.style.GetPosition() == css.PositionRelative {
		cbWidth := gla.space.AvailableSize.InlineSize.Float64()
		cbHeight := gla.space.AvailableSize.BlockSize.Float64()
		if cbHeight == Indefinite {
			cbHeight = 0
		}
		physCB := ToPhysicalSize(LogicalSize{InlineSize: cbWidth, BlockSize: cbHeight}, wdm.WM)
		offset := gla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
		result.Fragment.RelativeOffset = computeRelativeOffset(offset, wdm)
	}

	return result
}

// resolveItemPlacement resolves grid-column, grid-row, and grid-area for an item.
func (gla *GridLayoutAlgorithm) resolveItemPlacement(item *gridItem, namedAreas map[string]css.GridAreaInfo, colLineNames, rowLineNames map[string]int, numCols, numRows int) {
	s := item.style

	// Check grid-area first (it sets both row and column).
	if area := s.GetGridArea(); area != "" {
		if info, ok := namedAreas[area]; ok {
			item.colStart = info.ColStart - 1
			item.colEnd = info.ColEnd - 1
			item.rowStart = info.RowStart - 1
			item.rowEnd = info.RowEnd - 1
			return
		}
	}

	// grid-column placement (handles grid-column, grid-column-start/end, named lines).
	// Per CSS Grid 2 §8.3, `span N` with no start defers placement to the
	// auto-placement algorithm; we capture SpanCount on the item so autoPlace
	// can size the slot correctly. With an explicit start, we resolve the
	// span immediately into an explicit end.
	if col := s.GetGridColumnWithNames(colLineNames); col != nil {
		if col.Start > 0 || col.End > 0 || col.IsSpan {
			if col.Start > 0 {
				item.colStart = col.Start - 1
			}
			if col.End > 0 {
				item.colEnd = col.End - 1
			} else if col.IsSpan && col.SpanCount > 0 && col.Start > 0 {
				item.colEnd = item.colStart + col.SpanCount
			} else if col.Start > 0 {
				item.colEnd = item.colStart + 1
			}
			if col.SpanCount > 0 {
				item.colSpan = col.SpanCount
			}
		}
	}

	// grid-row placement (handles grid-row, grid-row-start/end, named lines).
	if row := s.GetGridRowWithNames(rowLineNames); row != nil {
		if row.Start > 0 || row.End > 0 || row.IsSpan {
			if row.Start > 0 {
				item.rowStart = row.Start - 1
			}
			if row.End > 0 {
				item.rowEnd = row.End - 1
			} else if row.IsSpan && row.SpanCount > 0 && row.Start > 0 {
				item.rowEnd = item.rowStart + row.SpanCount
			} else if row.Start > 0 {
				item.rowEnd = item.rowStart + 1
			}
			if row.SpanCount > 0 {
				item.rowSpan = row.SpanCount
			}
		}
	}
}

// autoPlace fills unplaced items into the grid using auto-placement.
func (gla *GridLayoutAlgorithm) autoPlace(items []*gridItem, numCols, numRows *int, autoFlow string) {
	// Build occupancy grid.
	occupied := make(map[[2]int]bool)
	for _, item := range items {
		if item.colStart >= 0 && item.colEnd >= 0 && item.rowStart >= 0 && item.rowEnd >= 0 {
			for r := item.rowStart; r < item.rowEnd; r++ {
				for c := item.colStart; c < item.colEnd; c++ {
					occupied[[2]int{r, c}] = true
				}
			}
		}
	}

	cursor := [2]int{0, 0} // {row, col}
	isColumnFlow := autoFlow == "column"

	for _, item := range items {
		if item.colStart >= 0 && item.colEnd >= 0 && item.rowStart >= 0 && item.rowEnd >= 0 {
			continue // already placed
		}

		// Determine span sizes. When the item has an explicit range, that
		// dominates; otherwise honour the requested span from
		// `grid-{column,row}: span N` recorded at placement-resolution time.
		colSpan := item.colSpan
		rowSpan := item.rowSpan
		if item.colEnd > item.colStart && item.colStart >= 0 {
			colSpan = item.colEnd - item.colStart
		}
		if item.rowEnd > item.rowStart && item.rowStart >= 0 {
			rowSpan = item.rowEnd - item.rowStart
		}
		if colSpan < 1 {
			colSpan = 1
		}
		if rowSpan < 1 {
			rowSpan = 1
		}

		// If item has a fixed row but no column (or vice versa), handle partial placement.
		if item.rowStart >= 0 && item.rowEnd >= 0 && item.colStart < 0 {
			// Row is fixed, find first available column.
			for c := 0; c < *numCols+1; c++ {
				fits := true
				for dr := 0; dr < rowSpan; dr++ {
					for dc := 0; dc < colSpan; dc++ {
						if occupied[[2]int{item.rowStart + dr, c + dc}] {
							fits = false
							break
						}
					}
					if !fits {
						break
					}
				}
				if fits {
					item.colStart = c
					item.colEnd = c + colSpan
					if item.colEnd > *numCols {
						*numCols = item.colEnd
					}
					break
				}
			}
			// Mark as occupied.
			for r := item.rowStart; r < item.rowEnd; r++ {
				for c := item.colStart; c < item.colEnd; c++ {
					occupied[[2]int{r, c}] = true
				}
			}
			continue
		}

		if item.colStart >= 0 && item.colEnd >= 0 && item.rowStart < 0 {
			// Column is fixed, find first available row.
			for r := 0; ; r++ {
				fits := true
				for dr := 0; dr < rowSpan; dr++ {
					for dc := 0; dc < colSpan; dc++ {
						if occupied[[2]int{r + dr, item.colStart + dc}] {
							fits = false
							break
						}
					}
					if !fits {
						break
					}
				}
				if fits {
					item.rowStart = r
					item.rowEnd = r + rowSpan
					if item.rowEnd > *numRows {
						*numRows = item.rowEnd
					}
					break
				}
			}
			for r := item.rowStart; r < item.rowEnd; r++ {
				for c := item.colStart; c < item.colEnd; c++ {
					occupied[[2]int{r, c}] = true
				}
			}
			continue
		}

		// Fully auto-placed item.
		if isColumnFlow {
			gla.placeItemColumnFlow(item, colSpan, rowSpan, &cursor, numCols, numRows, occupied)
		} else {
			gla.placeItemRowFlow(item, colSpan, rowSpan, &cursor, numCols, numRows, occupied)
		}
	}
}

func (gla *GridLayoutAlgorithm) placeItemRowFlow(item *gridItem, colSpan, rowSpan int, cursor *[2]int, numCols, numRows *int, occupied map[[2]int]bool) {
	// If no explicit columns, expand to fit items (prevent infinite loop).
	if *numCols < colSpan {
		*numCols = colSpan
	}
	for {
		r, c := cursor[0], cursor[1]
		if c+colSpan > *numCols {
			// Wrap to next row.
			cursor[0]++
			cursor[1] = 0
			continue
		}
		// Check if space is available.
		fits := true
		for dr := 0; dr < rowSpan; dr++ {
			for dc := 0; dc < colSpan; dc++ {
				if occupied[[2]int{r + dr, c + dc}] {
					fits = false
					break
				}
			}
			if !fits {
				break
			}
		}
		if fits {
			item.colStart = c
			item.colEnd = c + colSpan
			item.rowStart = r
			item.rowEnd = r + rowSpan
			if item.rowEnd > *numRows {
				*numRows = item.rowEnd
			}
			for dr := 0; dr < rowSpan; dr++ {
				for dc := 0; dc < colSpan; dc++ {
					occupied[[2]int{r + dr, c + dc}] = true
				}
			}
			cursor[1] = c + colSpan
			return
		}
		cursor[1]++
	}
}

func (gla *GridLayoutAlgorithm) placeItemColumnFlow(item *gridItem, colSpan, rowSpan int, cursor *[2]int, numCols, numRows *int, occupied map[[2]int]bool) {
	// If no explicit rows, expand to fit items (prevent infinite loop).
	if *numRows < rowSpan {
		*numRows = rowSpan
	}
	for {
		r, c := cursor[0], cursor[1]
		if r+rowSpan > *numRows {
			// Wrap to next column.
			cursor[1]++
			cursor[0] = 0
			continue
		}
		fits := true
		for dr := 0; dr < rowSpan; dr++ {
			for dc := 0; dc < colSpan; dc++ {
				if occupied[[2]int{r + dr, c + dc}] {
					fits = false
					break
				}
			}
			if !fits {
				break
			}
		}
		if fits {
			item.colStart = c
			item.colEnd = c + colSpan
			item.rowStart = r
			item.rowEnd = r + rowSpan
			if item.rowEnd > *numRows {
				*numRows = item.rowEnd
			}
			if item.colEnd > *numCols {
				*numCols = item.colEnd
			}
			for dr := 0; dr < rowSpan; dr++ {
				for dc := 0; dc < colSpan; dc++ {
					occupied[[2]int{r + dr, c + dc}] = true
				}
			}
			cursor[0] = r + rowSpan
			return
		}
		cursor[0]++
	}
}

// extendTracks extends the track list to cover at least n tracks.
func (gla *GridLayoutAlgorithm) extendTracks(tracks []css.GridTrack, n int, autoSize css.GridTrack) []css.GridTrack {
	for len(tracks) < n {
		if autoSize.Size > 0 || autoSize.Fr > 0 || autoSize.MinContent || autoSize.MaxContent {
			tracks = append(tracks, autoSize)
		} else {
			tracks = append(tracks, css.GridTrack{Auto: true})
		}
	}
	return tracks
}

// expandAutoFillFit expands auto-fill/auto-fit tracks based on available space.
func (gla *GridLayoutAlgorithm) expandAutoFillFit(tracks []css.GridTrack, availableSize, gap float64) []css.GridTrack {
	if len(tracks) == 0 {
		return tracks
	}

	var result []css.GridTrack
	for _, t := range tracks {
		if (t.AutoFill || t.AutoFit) && len(t.AutoTemplate) > 0 {
			// Calculate template total size.
			templateSize := 0.0
			for _, at := range t.AutoTemplate {
				if at.Size > 0 {
					templateSize += at.Size
				} else if at.MinSize > 0 {
					templateSize += at.MinSize
				}
			}
			if templateSize <= 0 {
				templateSize = 1 // prevent division by zero
			}

			// Calculate how many repetitions fit.
			templateWithGap := templateSize + gap*float64(len(t.AutoTemplate)-1)
			reps := int(math.Floor((availableSize + gap) / (templateWithGap + gap)))
			if reps < 1 {
				reps = 1
			}

			for i := 0; i < reps; i++ {
				result = append(result, t.AutoTemplate...)
			}
		} else {
			result = append(result, t)
		}
	}
	return result
}

// resolveTrackSizes resolves track sizes from track definitions.
func (gla *GridLayoutAlgorithm) resolveTrackSizes(tracks []css.GridTrack, available, gap float64, isInline bool, items []*gridItem, numCols, numRows int, wdm WritingDirectionMode, geom FragmentGeometry) []float64 {
	n := len(tracks)
	sizes := make([]float64, n)

	// First pass: resolve fixed sizes and percentages.
	for i, t := range tracks {
		if t.Size > 0 {
			sizes[i] = t.Size
		} else if t.Percent > 0 && available > 0 {
			sizes[i] = available * t.Percent / 100
		} else if t.IsMinMax {
			sizes[i] = t.MinSize
		} else if t.IsFitContent {
			sizes[i] = 0 // will be resolved later
		} else if t.MinContent || t.MaxContent {
			sizes[i] = 0 // will be resolved from content
		}
	}

	// Resolve min-content/max-content/auto tracks from item content sizes.
	for i, t := range tracks {
		if !t.Auto && !t.MinContent && !t.MaxContent && t.Fr == 0 && !t.IsFitContent && !(t.IsMinMax && t.MaxAuto) {
			continue
		}
		maxSize := 0.0
		for _, item := range items {
			var start, end int
			if isInline {
				start, end = item.colStart, item.colEnd
			} else {
				start, end = item.rowStart, item.rowEnd
			}
			if start <= i && end > i {
				span := end - start
				if span <= 0 {
					span = 1
				}
				var itemSize float64
				if item.result != nil {
					// Use the physical dimension that corresponds to the logical direction.
					// For inline tracks: inline = Width in HTB, Height in VLR.
					// For block tracks:  block  = Height in HTB, Width in VLR.
					// Formula: useWidth when (isInline != wdm.IsVertical()).
					if isInline != wdm.IsVertical() {
						itemSize = item.result.Fragment.Size.WidthF64()
					} else {
						itemSize = item.result.Fragment.Size.HeightF64()
					}
				}
				contribution := itemSize / float64(span)
				if contribution > maxSize {
					maxSize = contribution
				}
			}
		}
		if t.IsFitContent && maxSize > t.FitContentMax {
			maxSize = t.FitContentMax
		}
		if t.IsMinMax {
			if maxSize < t.MinSize {
				maxSize = t.MinSize
			}
			if t.MaxSize > 0 && maxSize > t.MaxSize {
				maxSize = t.MaxSize
			}
		}
		if maxSize > sizes[i] {
			sizes[i] = maxSize
		}
	}

	// Distribute fr units.
	gla.distributeFr(tracks, sizes, available-gap*float64(n-1))

	// CSS Grid §12.5 step 5: stretch auto tracks to fill remaining free space
	// when the container has a definite size (justify/align-content: normal).
	if available >= 0 {
		totalUsed := 0.0
		autoCount := 0
		for i, t := range tracks {
			if t.Auto && t.Fr == 0 {
				autoCount++
			} else {
				totalUsed += sizes[i]
			}
		}
		if autoCount > 0 {
			totalGap := gap * float64(n-1)
			freeSpace := available - totalUsed - totalGap
			if freeSpace > 0 {
				share := freeSpace / float64(autoCount)
				for i, t := range tracks {
					if t.Auto && t.Fr == 0 && sizes[i] < share {
						sizes[i] = share
					}
				}
			}
		}
	}

	return sizes
}

// distributeFr distributes remaining space among fr tracks.
func (gla *GridLayoutAlgorithm) distributeFr(tracks []css.GridTrack, sizes []float64, totalAvailable float64) {
	n := len(tracks)
	if n == 0 {
		return
	}

	// Calculate used space by non-fr tracks.
	usedSpace := 0.0
	totalFr := 0.0
	for i, t := range tracks {
		if t.Fr > 0 {
			totalFr += t.Fr
		} else if t.IsMinMax && t.MaxFr > 0 {
			totalFr += t.MaxFr
		} else {
			usedSpace += sizes[i]
		}
	}

	if totalFr <= 0 {
		return
	}

	freeSpace := totalAvailable - usedSpace
	if freeSpace < 0 {
		freeSpace = 0
	}

	frSize := freeSpace / totalFr
	for i, t := range tracks {
		if t.Fr > 0 {
			sizes[i] = t.Fr * frSize
			if t.IsMinMax && sizes[i] < t.MinSize {
				sizes[i] = t.MinSize
			}
		} else if t.IsMinMax && t.MaxFr > 0 {
			s := t.MaxFr * frSize
			if s < t.MinSize {
				s = t.MinSize
			}
			sizes[i] = s
		}
	}
}

// spannedSize returns the total size of tracks from start to end (exclusive), including gaps.
func (gla *GridLayoutAlgorithm) spannedSize(sizes []float64, start, end int, gap float64) float64 {
	total := 0.0
	for i := start; i < end && i < len(sizes); i++ {
		total += sizes[i]
		if i > start {
			total += gap
		}
	}
	return total
}

// buildGridChildren implements CSS Grid §6.1 anonymous grid-item wrapping.
//
// Each contiguous run of text nodes (with display:none being transparent and
// position:absolute/fixed interrupting the run) is grouped into a single
// anonymous block-level grid item so that the run participates in
// auto-placement. Mirrors the flex container's buildFlexChildren (CSS
// Flexbox §4) so the two layout modes follow the same wrapping rules.
func buildGridChildren(node *LayoutInputNode, parentStyle *css.Style) []*LayoutInputNode {
	var result []*LayoutInputNode
	var textRun []*LayoutInputNode

	flushTextRun := func() {
		if len(textRun) == 0 {
			return
		}
		// Per CSS Grid §6.1 (which defers to Flexbox §4): a sequence of
		// child text content that is only collapsible white space is not
		// rendered, so we don't wrap whitespace-only runs.
		hasContent := false
		for _, n := range textRun {
			if n.IsText() {
				if !isCSSWhitespaceOnly(n.TextContent()) {
					hasContent = true
					break
				}
			} else {
				hasContent = true
				break
			}
		}
		if hasContent {
			anonStyle := css.NewAnonymousBlockStyle(parentStyle)
			result = append(result, &LayoutInputNode{
				style:       anonStyle,
				children:    textRun,
				isAnonymous: true,
			})
		}
		textRun = nil
	}

	for _, child := range node.Children() {
		if child.IsText() {
			textRun = append(textRun, child)
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			flushTextRun()
			continue
		}
		// display:none is transparent — don't interrupt the text run.
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}
		// OOF children interrupt text runs (Blink behaviour).
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			flushTextRun()
			result = append(result, child)
			continue
		}
		// <br> joins surrounding text (LayoutBR is a LayoutText subclass in
		// Blink), so the contiguous run wrapping picks it up.
		if child.DOMNode != nil && child.DOMNode.TagName == "br" {
			textRun = append(textRun, child)
			continue
		}
		flushTextRun()
		result = append(result, child)
	}
	flushTextRun()
	return result
}

// isAnonymousWhitespaceOnly checks if an anonymous node wraps only whitespace.
func isAnonymousWhitespaceOnly(n *LayoutInputNode) bool {
	for _, c := range n.Children() {
		if c.IsText() {
			if strings.TrimSpace(c.TextContent()) != "" {
				return false
			}
		} else {
			return false
		}
	}
	return true
}

// totalTrackSize returns the total size of all tracks including gaps.
func (gla *GridLayoutAlgorithm) totalTrackSize(sizes []float64, gap float64) float64 {
	total := 0.0
	for i, s := range sizes {
		total += s
		if i > 0 {
			total += gap
		}
	}
	return total
}

// computeTrackOffsets computes the starting offset for each track.
func (gla *GridLayoutAlgorithm) computeTrackOffsets(sizes []float64, gap, containerSize float64, contentAlign string, n int) []float64 {
	offsets := make([]float64, n)
	totalSize := gla.totalTrackSize(sizes, gap)
	freeSpace := containerSize - totalSize

	startOffset := 0.0
	adjustedGap := gap

	switch contentAlign {
	case "center":
		startOffset = freeSpace / 2
	case "end", "flex-end":
		startOffset = freeSpace
	case "space-between":
		if n > 1 {
			adjustedGap = gap + freeSpace/float64(n-1)
		}
	case "space-around":
		if n > 0 {
			extra := freeSpace / float64(n)
			startOffset = extra / 2
			adjustedGap = gap + extra
		}
	case "space-evenly":
		if n > 0 {
			extra := freeSpace / float64(n+1)
			startOffset = extra
			adjustedGap = gap + extra
		}
	}

	offset := startOffset
	for i := 0; i < n; i++ {
		offsets[i] = offset
		if i < len(sizes) {
			offset += sizes[i]
		}
		offset += adjustedGap
	}
	return offsets
}

// getAlignSelf returns the resolved align-self for a grid item.
func (gla *GridLayoutAlgorithm) getAlignSelf(itemStyle *css.Style) string {
	if v, ok := itemStyle.Get("align-self"); ok && v != "" && v != "auto" {
		return v
	}
	if v, ok := gla.style.Get("align-items"); ok && v != "" {
		return v
	}
	return "stretch"
}

// getJustifySelf returns the resolved justify-self for a grid item.
func (gla *GridLayoutAlgorithm) getJustifySelf(itemStyle *css.Style) string {
	if v, ok := itemStyle.Get("justify-self"); ok && v != "" && v != "auto" {
		return v
	}
	if v, ok := gla.style.Get("justify-items"); ok && v != "" {
		return v
	}
	return "stretch"
}

// shouldStretchInline returns true when the grid item should be stretched
// to its track's inline-size — i.e. when justify-self resolves to stretch
// AND the item has no explicit inline-size. Per CSS Box Alignment 3 §6.1,
// stretch only applies when the box has no explicit size in the relevant
// axis. Blink mirrors this in
// third_party/blink/renderer/core/layout/grid/grid_layout_algorithm.cc
// where the per-item constraint space only sets kFixedInlineSize under
// the same conditions (justify-self stretch on an auto-sized item).
func (gla *GridLayoutAlgorithm) shouldStretchInline(itemStyle *css.Style, wdm WritingDirectionMode) bool {
	if itemStyle == nil {
		return true
	}
	justify := gla.getJustifySelf(itemStyle)
	if justify != "stretch" && justify != "normal" {
		return false
	}
	inlineProp := "width"
	if wdm.IsVertical() {
		inlineProp = "height"
	}
	if v, ok := itemStyle.Get(inlineProp); ok && v != "" && v != "auto" {
		return false
	}
	return true
}

// alignOffset computes offset for alignment within available space.
func (gla *GridLayoutAlgorithm) alignOffset(itemSize, available float64, align string) float64 {
	if itemSize >= available {
		return 0
	}
	switch align {
	case "center":
		return (available - itemSize) / 2
	case "end", "flex-end":
		return available - itemSize
	case "stretch", "normal", "start", "flex-start":
		return 0
	default:
		return 0
	}
}
