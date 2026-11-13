package layout

import (
	"math"
	"slices"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
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
	// columns and rows are the item's alignment in each axis
	// (gridItemAxisData), resolved when the item is collected.
	columns, rows gridItemAxisData
}

// Layout performs CSS Grid layout.
func (gla *GridLayoutAlgorithm) Layout() *LayoutResult {
	wdm := gla.space.WritingDirection
	geom := CalculateInitialFragmentGeometry(gla.ctx, gla.node, gla.style, wdm, gla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(gla.node)
	builder.SetChildAvailableSize(CalculateChildAvailableSize(gla.space, gla.node, geom))

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
	sizeContained := gla.style != nil && gla.style.ShouldApplySizeContainment()

	rowBlockBase := float64(Indefinite)
	if hasExplicitBlock {
		rowBlockBase = explicitBlockSize
	}
	rowGap, _ := gla.style.ResolveGap("row-gap", rowBlockBase)
	colGap, _ := gla.style.ResolveGap("column-gap", contentInlineSize)
	tree := gla.buildGridSizingTree(contentInlineSize, explicitBlockSize, false)
	items, oofChildren := tree.items, tree.oofChildren
	colTracks, rowTracks := tree.colTracks, tree.rowTracks
	numCols, numRows := tree.numCols, tree.numRows
	// Layout keeps one column in a grid with no column at all, as before
	// buildGridSizingTree stopped synthesizing one for its intrinsic size.
	// Blink has none there; dropping it from layout is out of this scope.
	if numCols == 0 {
		numCols = 1
		colTracks = gla.extendTracks(colTracks, 1, gla.autoColumnTrack())
	}

	// --- Track sizing: columns ---
	// Size containment sizes only the container as if empty: its columns
	// still size from its items, as GridLayoutAlgorithm::Layout builds the
	// sizing tree with its children (grid_layout_algorithm.cc:416-422 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	colSets := newGridSets(colTracks, columnAvailable(contentInlineSize))
	colSizing := gla.columnSizingItems(colSets, items)
	colSizes := gla.sizeColumns(slices.Clone(colSets), contentInlineSize, colGap, colSizing, gridSizingLayout)

	// --- Layout items to determine row intrinsic sizes ---
	// First pass: layout items with resolved column sizes to get natural heights.
	gla.layoutItemsAtColumns(items, colSizes, colGap, hasExplicitBlock, explicitBlockSize)

	// --- Track sizing: rows ---
	// Rows run the track sizing algorithm at the grid's available block
	// size, indefinite without one; a definite min block size is what auto
	// rows stretch into then (CompleteTrackSizingAlgorithm(kForRows,
	// kLayout), grid_layout_algorithm.cc:429-430 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Size containment sizes only
	// the container's intrinsic block size (below), not its rows.
	rowAvailable := float64(Indefinite)
	if hasExplicitBlock {
		rowAvailable = explicitBlockSize
	}
	rowMinAvailable := max(0, ResolveMinBlockSize(gla.style, wdm, gla.space, geom).Float64())

	// Under size containment the intrinsic block size is the grid's as if
	// empty: its explicit rows sized with no items, plus gaps; with an
	// indefinite block size, the block size it resolves to is the rows'
	// available size from the start (GridLayoutAlgorithm's constructor,
	// grid_layout_algorithm.cc:41-55; ComputeIntrinsicBlockSizeIgnoringChildren,
	// :508-530 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Ignoring
	// children, every auto-fit row is empty and collapses.
	containedIntrinsicBlockSize := 0.0
	if sizeContained {
		explicitRows := collapseAutoFitEmptyTracks(slices.Clone(rowTracks[:min(len(rowTracks), tree.lines.explicitRows)]), nil, false)
		emptyRows := gla.computeRowSizes(explicitRows, rowAvailable, rowMinAvailable, rowGap, nil, nil)
		containedIntrinsicBlockSize = gridTracksSpanSize(explicitRows, emptyRows, rowGap)
		if !hasExplicitBlock {
			// A percentage row gap resolves against that contained size
			// (grid_available_size_).
			rowAvailable = gla.clampBlockSize(containedIntrinsicBlockSize, wdm, geom)
			rowGap, _ = gla.style.ResolveGap("row-gap", rowAvailable)
		}
	}
	// The first pass (CompleteTrackSizingAlgorithm(kForRows, kLayout),
	// grid_layout_algorithm.cc:429-430 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	rowSizes := gla.computeRowSizes(rowTracks, rowAvailable, rowMinAvailable, rowGap, items,
		gla.layoutRowContribution(colSizes, colGap, hasExplicitBlock))
	// The rows' intrinsic block size is their first pass's span
	// (CalculateIntrinsicBlockSize after the first pass, :432-433), and the
	// block size it resolves to is what the rows are aligned in and sized
	// again in (:443-473).
	rowsIntrinsicBlockSize := gla.totalTrackSize(rowSizes, rowGap)
	if sizeContained {
		rowsIntrinsicBlockSize = containedIntrinsicBlockSize
	}
	resolvedBlockSize := gla.clampBlockSizeAround(rowsIntrinsicBlockSize, rowsIntrinsicBlockSize, wdm, geom)
	if hasExplicitBlock {
		resolvedBlockSize = gla.clampBlockSizeAround(explicitBlockSize, rowsIntrinsicBlockSize, wdm, geom)
	}
	// The additional pass (:443-490), with an indefinite block size, when
	// the rows depend on the available size (NeedsAdditionalLayoutPass,
	// grid_layout_utils.cc), or, whatever the block size, when an
	// orthogonal item's column contribution changes against its first-pass
	// rows (MayChangeBlockSizeDependentGridItemContributions, :1146-1162):
	// the columns are sized again against those rows, then the rows again,
	// at the resolved block size (:478-484).
	needsAdditionalPass := rowAvailable == Indefinite && gla.rowsDependOnAvailableSize(rowTracks)
	if sizing := gla.remeasureBlockSizeDependentItems(colSizing, rowSizes, rowGap); sizing != nil {
		if rerun := gla.sizeColumns(slices.Clone(colSets), contentInlineSize, colGap, sizing, gridSizingLayout); !slices.Equal(rerun, colSizes) {
			colSizes = rerun
			gla.layoutItemsAtColumns(items, colSizes, colGap, hasExplicitBlock, explicitBlockSize)
			needsAdditionalPass = true
		}
	}
	if needsAdditionalPass {
		if rowAvailable == Indefinite {
			rowGap, _ = gla.style.ResolveGap("row-gap", resolvedBlockSize)
		}
		rowSizes = gla.computeRowSizes(rowTracks, resolvedBlockSize, rowMinAvailable, rowGap, items,
			gla.layoutRowContribution(colSizes, colGap, hasExplicitBlock))
	}

	// Re-layout items that have percentage heights or need stretching.
	for _, item := range items {
		itemBlock := gla.spannedSize(rowSizes, item.rowStart, item.rowEnd, rowGap)
		itemInline := gla.spannedSize(colSizes, item.colStart, item.colEnd, colGap)

		// Check if item needs full block-size (stretch or percentage block-size).
		// The logical block-size CSS property is "height" in HTB and "width" in vertical modes.
		blockSizeProp := logicalBlockSizeProperty(wdm)
		needsRelayout := false
		if _, ok := item.style.GetPercentage(blockSizeProp); ok {
			needsRelayout = true
		}
		// An item whose block size stretches (its rows auto behaviour).
		if item.rows.stretches() {
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
			stretchInline := gla.shouldStretchInline(item, wdm)
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
			// CSS Box Alignment §6.1 / CSS Grid §12.5: non-stretch items were
			// ShrinkToFit-sized in the first pass. Preserve that inline size on
			// the re-layout pass (which runs for block-stretch / percentage-height
			// items). Without this guard the re-layout uses the full column width,
			// undoing the ShrinkToFit and causing incorrect line wrapping on the
			// second pass. Mirrors the first-pass ShrinkToFit block above.
			reLayoutInline := itemInline
			isFixedInline := stretchInline
			// Replaced items (img/canvas/…) are excluded: their first-pass
			// fragment uses the natural inline-size, but on the re-layout pass a
			// now-definite block-size (e.g. a fixed row track + percentage height)
			// must transfer through the intrinsic aspect ratio to the inline-size
			// via ComputeReplacedSize in CalculateInitialFragmentGeometry. Reusing the first-pass
			// natural width here (with IsFixedInlineSize=true) would defeat that —
			// e.g. a 200×200 img with height:100% in a 100px grid area would stay
			// 200px wide instead of becoming a 100px square (grid-in-table-cell-with-img).
			if !stretchInline && isAutoOrUnsetInline(item.style, wdm) &&
				!gla.replacedHasNaturalSize(item.node, wdm, true) {
				ar := boxAspectRatio(gla.ctx, item.node, item.style)
				if !ar.IsSet {
					// Reuse the ShrinkToFit inline size from the first-pass fragment.
					// Aspect-ratio items are excluded: CalculateInitialFragmentGeometry
					// derives their inline from the definite block via the reverse-computation
					// path (fragment_geometry.go §5.1), which requires inlineSizeIsAuto=true.
					// Setting IsFixedInlineSize=true here would short-circuit that path.
					reLayoutInline = NewLogicalFragment(wdm, item.result.Fragment).InlineSize()
					isFixedInline = true
				}
			}
			childSpace2 := NewConstraintSpaceBuilder(wdm, childWDM2, true).
				SetInlineAutoBehavior(gridItemInlineAutoBehavior(stretchInline)).
				SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM2, gla.ctx)).
				SetOrthogonalFallbackBlockSize(gla.space.OrthogonalFallbackBlockSize).
				SetAvailableSize(LogicalSize{InlineSize: reLayoutInline, BlockSize: reLayoutBlock}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: reLayoutInline, BlockSize: availBlock}).
				SetPercentageResolutionInlineSize(reLayoutInline).
				SetIsFixedInlineSize(isFixedInline).
				SetIsFixedBlockSize(item.rows.stretches()).
				Build()
			item.result = layoutElement(gla.ctx, item.node, childSpace2)
		}
	}

	// --- Position items ---
	// Compute content-distribution offsets (justify-content, align-content).
	justifyContent := "start"
	if v, ok := gla.style.Get("justify-content"); ok && v != "" {
		justifyContent = v
	}
	alignContent := "start"
	if v, ok := gla.style.Get("align-content"); ok && v != "" {
		alignContent = v
	}

	colOffsets := gla.computeTrackOffsets(colSizes, colGap, contentInlineSize, justifyContent, numCols)
	// Rows align in the resolved block size (:458-473), as the columns in
	// the inline size: negative free space is not clamped (an unsafe
	// alignment by default).
	rowOffsets := gla.computeTrackOffsets(rowSizes, rowGap, resolvedBlockSize, alignContent, numRows)

	// The shared baselines from the items' final layout
	// (CompleteFinalBaselineAlignment, grid_layout_algorithm.cc:1334-1340 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), read in the grid's font
	// baseline (ComputedStyle::GetFontBaseline).
	centralBaseline := wdm.UsesCentralBaselineWithStyle(gla.style)
	colBaselines := computeGridItemBaselines(items, true, numCols, centralBaseline)
	rowBaselines := computeGridItemBaselines(items, false, numRows, centralBaseline)

	for _, item := range items {
		itemInline := gla.spannedSize(colSizes, item.colStart, item.colEnd, colGap)
		itemBlock := gla.spannedSize(rowSizes, item.rowStart, item.rowEnd, rowGap)

		// Item position = track offset + its alignment offset in the grid
		// area, margins included (grid_layout_algorithm.cc:1851-1877 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		margins := item.margins
		fragSize := ToLogicalSize(geomSizeToOld(item.result.Fragment.Size), wdm.WM)
		inlineBaselineOffset := colBaselines.baselineOffset(item, true, itemInline, fragSize.InlineSize)
		blockBaselineOffset := rowBaselines.baselineOffset(item, false, itemBlock, fragSize.BlockSize)
		inlineOffset := colOffsets[item.colStart] + AlignmentOffset(itemInline, fragSize.InlineSize,
			margins.InlineStart, margins.InlineEnd, inlineBaselineOffset, item.columns.alignment, item.columns.isOverflowSafe)
		blockOffset := rowOffsets[item.rowStart] + AlignmentOffset(itemBlock, fragSize.BlockSize,
			margins.BlockStart, margins.BlockEnd, blockBaselineOffset, item.rows.alignment, item.rows.isOverflowSafe)

		offset := LogicalOffset{InlineOffset: inlineOffset, BlockOffset: blockOffset}
		// Grid is special in that %-based offsets resolve against the grid
		// area, so the relative offset is determined here instead of in the
		// builder (grid_layout_algorithm.cc:1879-1899 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		relativeOffset := ComputeRelativeOffsetForBoxFragment(item.result.Fragment, wdm,
			LogicalSize{InlineSize: itemInline, BlockSize: itemBlock})
		builder.AddChildWithRelativeOffset(item.result.Fragment, offset, relativeOffset)
		builder.PropagateOOFPositionedInfo(item.result, item.style, offset, relativeOffset)
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

	// Compute final block-size: the explicit one, else the intrinsic one,
	// clamped by min/max (intrinsic keywords there resolving against the
	// intrinsic block size). The contained intrinsic block size is above;
	// the generic sizeContainedIntrinsicBlockSize would zero even fixed row
	// tracks.
	intrinsicBlockSize := rowsIntrinsicBlockSize
	finalBlockSize := resolvedBlockSize

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

	// The padding box, in content-box coordinates; the containing block of
	// the out-of-flow descendants this grid contains unless they are placed
	// within a grid area.
	paddingBox := gridArea{
		offset: LogicalOffset{InlineOffset: -geom.Padding.InlineStart, BlockOffset: -geom.Padding.BlockStart},
		size: LogicalSize{
			InlineSize: contentInlineSize + geom.Padding.InlineSum(),
			BlockSize:  finalBlockSize + geom.Padding.BlockSum(),
		},
	}
	tracks := gridTrackGeometry{
		colOffsets: colOffsets, colSizes: colSizes,
		rowOffsets: rowOffsets, rowSizes: rowSizes,
		lines: tree.lines,
	}
	gla.PlaceOutOfFlowItems(builder, oofChildren, tracks, paddingBox)

	// Lay out the out-of-flow children. Mirrors the OutOfFlowLayoutPart
	// run of Blink's BoxFragmentBuilder::HandleOofsAndSpecialDescendants
	// (box_fragment_builder.cc:660-671, called at the end of
	// GridLayoutAlgorithm::Layout @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
	// with a candidate placed within a grid area contained by that area
	// (GetContainingBlockInfo, out_of_flow_layout_part.cc:798-834, :902-906).
	var propagatedOOF []OutOfFlowCandidate
	if builder.NeedsOutOfFlowLayoutPart() {
		part := NewOutOfFlowLayoutPart(gla.ctx, builder, gla.space, paddingBox.size, geom.Padding,
			previousConsumedContentBlockSize(gla.space, geom.BorderBoxPadding().BlockStart))
		part.gridAreaContainingBlock = func(candidate OutOfFlowCandidate) (LogicalSize, LogicalOffset, bool) {
			style := candidate.Node.Style()
			if !isPlacedWithinGridArea(style) {
				return LogicalSize{}, LogicalOffset{}, false
			}
			area := tracks.ComputeOutOfFlowItemContainingRect(style, paddingBox)
			return area.size, area.offset, true
		}
		propagatedOOF = part.Run()
	}

	result := builder.Build()
	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	return result
}

// gridSizingTree is a placed grid: its in-flow items with resolved lines,
// its out-of-flow children, and its track lists extended over the implicit
// grid. Layout and the intrinsic inline size computation both size it, so
// implicit tracks take part in each. Mirrors Blink's GridSizingTree, which
// GridLayoutAlgorithm::ComputeMinMaxSizes builds, running placement, before
// sizing columns (layout/grid/grid_layout_algorithm.cc:279-284 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), as Layout does.
type gridSizingTree struct {
	items                []*gridItem
	oofChildren          []*LayoutInputNode
	colTracks, rowTracks []css.GridTrack
	numCols, numRows     int
	// lines resolves placement longhands against the explicit grid.
	lines gridLineResolver
}

// buildGridSizingTree places the grid's items. contentInlineSize and
// explicitBlockSize resolve repeat(auto-fill/auto-fit); a size of 0 or
// Indefinite gives one repetition. ignoreChildren places no items, as
// Blink's BuildGridSizingTreeIgnoringChildren.
func (gla *GridLayoutAlgorithm) buildGridSizingTree(contentInlineSize, explicitBlockSize float64, ignoreChildren bool) gridSizingTree {
	wdm := gla.space.WritingDirection
	// Parse grid template definitions with named lines.
	colTracks, colLineNames := gla.style.GetGridTemplateColumnsWithNames()
	rowTracks, rowLineNames := gla.style.GetGridTemplateRowsWithNames()
	blockBase := float64(Indefinite)
	if explicitBlockSize > 0 {
		blockBase = explicitBlockSize
	}
	rowGap, _ := gla.style.ResolveGap("row-gap", blockBase)
	colGap, _ := gla.style.ResolveGap("column-gap", contentInlineSize)
	autoFlow := gla.style.GetGridAutoFlow()

	// Parse grid-template-areas for named area placement.
	namedAreas := gla.style.GetGridTemplateAreas()

	// Build list of grid children (skip OOF and whitespace-only text nodes).
	// CSS Grid §6.1: contiguous runs of non-whitespace text directly inside
	// the grid container are wrapped into anonymous block-level grid items so
	// that they can be placed by the auto-placement algorithm. Mirrors the
	// flex container's buildFlexChildren behaviour (CSS Flexbox §4).
	var gridChildren []*LayoutInputNode
	if !ignoreChildren {
		gridChildren = buildGridChildren(gla.node, gla.style)
	}

	var items []*gridItem
	// oofChildren are the out-of-flow children, placed once the grid's
	// geometry is known (PlaceOutOfFlowItems).
	var oofChildren []*LayoutInputNode
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
			oofChildren = append(oofChildren, child)
			continue
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}
		childWDM := NewWritingDirectionMode(childStyle)
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
			columns:  newGridItemAxisData(true, gla.style, wdm, childStyle, childWDM, gla.replacedHasNaturalSize(child, wdm, true)),
			rows:     newGridItemAxisData(false, gla.style, wdm, childStyle, childWDM, gla.replacedHasNaturalSize(child, wdm, false)),
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
	// The explicit grid is as large as its template areas too
	// (GridLineResolver::ExplicitGridColumnCount / RowCount,
	// grid_line_resolver.cc:525-555 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	explicitCols, explicitRows := len(colTracks), len(rowTracks)
	for _, area := range namedAreas {
		explicitCols, explicitRows = max(explicitCols, area.ColEnd-1), max(explicitRows, area.RowEnd-1)
	}
	explicitCols, explicitRows = min(explicitCols, css.GridMaxTracks), min(explicitRows, css.GridMaxTracks)
	lines := gridLineResolver{
		explicitCols: explicitCols, explicitRows: explicitRows,
		colLineNames: colLineNames, rowLineNames: rowLineNames, namedAreas: namedAreas,
	}
	for _, item := range items {
		gla.resolveItemPlacement(item, lines)
	}

	// Auto-place items that don't have explicit positions.
	// The explicit grid's tracks always exist, its areas' included.
	numCols := explicitCols
	numRows := explicitRows
	// First, compute grid dimensions from explicit placements.
	for _, item := range items {
		if item.colEnd >= 0 && item.colEnd > numCols {
			numCols = item.colEnd
		}
		if item.rowEnd >= 0 && item.rowEnd > numRows {
			numRows = item.rowEnd
		}
	}
	// Auto-placement needs a column to place into. An itemless grid gets
	// no implicit track, as Blink places no item there.
	if numCols == 0 && len(items) > 0 {
		numCols = 1
	}

	// Auto-placement: fill unplaced items into the grid.
	gla.autoPlace(items, &numCols, &numRows, autoFlow)

	// Extend track definitions to cover all rows/columns.
	autoColTrack := gla.autoColumnTrack()
	autoRowTrack := css.GridTrack{Auto: true}
	if t := gla.style.GetGridAutoRows(); t != nil {
		autoRowTrack = *t
	}
	colTracks = gla.extendTracks(colTracks, numCols, autoColTrack)
	rowTracks = gla.extendTracks(rowTracks, numRows, autoRowTrack)

	// CSS Grid §7.2.2.1: auto-fit collapses empty tracks to 0px.
	colTracks = collapseAutoFitEmptyTracks(colTracks, items, true)
	rowTracks = collapseAutoFitEmptyTracks(rowTracks, items, false)

	return gridSizingTree{
		items: items, oofChildren: oofChildren,
		colTracks: colTracks, rowTracks: rowTracks,
		numCols: numCols, numRows: numRows,
		lines: lines,
	}
}

// resolveItemPlacement resolves an item's lines from its placement
// longhands (gridLineResolver): a definite axis gets its lines, an
// indefinite one its span size for auto-placement.
func (gla *GridLayoutAlgorithm) resolveItemPlacement(item *gridItem, r gridLineResolver) {
	if col := r.resolve(item.style, true); col.definite {
		item.colStart, item.colEnd = col.start, col.end
	} else {
		item.colSpan = col.size
	}
	if row := r.resolve(item.style, false); row.definite {
		item.rowStart, item.rowEnd = row.start, row.end
	} else {
		item.rowSpan = row.size
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
				for _, at := range t.AutoTemplate {
					at.AutoFitExpanded = t.AutoFit
					result = append(result, at)
				}
			}
		} else {
			result = append(result, t)
		}
	}
	return result
}

// collapseAutoFitEmptyTracks zeros out auto-fit-expanded tracks that contain
// no items (CSS Grid §7.2.2.1).
func collapseAutoFitEmptyTracks(tracks []css.GridTrack, items []*gridItem, isColumn bool) []css.GridTrack {
	for i := range tracks {
		if !tracks[i].AutoFitExpanded {
			continue
		}
		occupied := false
		for _, item := range items {
			if start, end := item.span(isColumn); start <= i && i < end {
				occupied = true
				break
			}
		}
		if !occupied {
			tracks[i] = css.GridTrack{Size: 0, AutoFitExpanded: true}
		}
	}
	return tracks
}

// autoColumnTrack is the sizing function of an implicit column:
// grid-auto-columns, or auto.
func (gla *GridLayoutAlgorithm) autoColumnTrack() css.GridTrack {
	if t := gla.style.GetGridAutoColumns(); t != nil {
		return *t
	}
	return css.GridTrack{Auto: true}
}

// gridTrackIsFixed reports whether t is a plain length, a collapsed auto-fit
// track included: no intrinsic, flexible, percentage or minmax() part.
func gridTrackIsFixed(t css.GridTrack) bool {
	return !t.Auto && t.Fr == 0 && t.Percent == 0 && !t.IsMinMax && !t.MinContent && !t.MaxContent && !t.IsFitContent
}

// gridTrackIsCollapsed reports whether t is an auto-fit track that
// collapsed for holding no item (collapseAutoFitEmptyTracks).
func gridTrackIsCollapsed(t css.GridTrack) bool {
	return t.AutoFitExpanded && gridTrackIsFixed(t) && t.Size == 0
}

// gridTracksSpanSize is the size the tracks span: their sizes, and a gap
// between each pair of tracks that are not collapsed auto-fit tracks, whose
// gutters collapse too (CSS Grid 2 §7.2.2.1).
func gridTracksSpanSize(tracks []css.GridTrack, sizes []float64, gap float64) float64 {
	total, separated := 0.0, 0
	for i, t := range tracks {
		total += sizes[i]
		if !gridTrackIsCollapsed(t) {
			separated++
		}
	}
	if separated > 1 {
		total += gap * float64(separated-1)
	}
	return total
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
			anonStyle := css.CreateAnonymousStyleWithDisplay(parentStyle, string(css.DisplayBlock))
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
		// <br> and anonymous inlines join surrounding text (joinsTextRun),
		// so the contiguous run wrapping picks them up.
		if joinsTextRun(child) {
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

// computeTrackOffsets computes the starting offset for each track. A
// position alignment applies negative free space too (unsafe by default);
// space-between, -around and -evenly distribute only non-negative free
// space, and are start-aligned with their gaps otherwise
// (ComputeFirstSetGeometry, grid_track_sizing_algorithm.cc:221-261 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
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
		if n > 1 && freeSpace >= 0 {
			adjustedGap = gap + freeSpace/float64(n-1)
		}
	case "space-around":
		if n > 0 && freeSpace >= 0 {
			extra := freeSpace / float64(n)
			startOffset = extra / 2
			adjustedGap = gap + extra
		}
	case "space-evenly":
		if freeSpace >= 0 {
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

// gridItemInlineAutoBehavior is the behaviour of a grid item's auto
// size along the grid's inline axis: stretch when its justify-self
// stretches it, fit-content otherwise (a replaced item under `normal`
// included). Mirrors the auto_behavior AxisEdgeFromItemPosition sets
// (grid_item.cc:48, :120-141 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func gridItemInlineAutoBehavior(stretchInline bool) AutoSizeBehavior {
	if stretchInline {
		return AutoSizeBehaviorStretchImplicit
	}
	return AutoSizeBehaviorFitContent
}

// shouldStretchInline returns true when the grid item should be stretched
// to its track's inline-size: when its columns auto behaviour stretches
// (gridItemAxisData; a replaced item under `normal`, auto margins and every
// non-stretch alignment are fit-content) AND it has no explicit inline-size,
// as CSS Box Alignment 3 §6.1 has stretch apply only to auto sizes.
func (gla *GridLayoutAlgorithm) shouldStretchInline(item *gridItem, wdm WritingDirectionMode) bool {
	return item.columns.stretches() && isAutoOrUnsetInline(item.style, wdm)
}

// replacedHasNaturalSize reports whether the grid item node is a replaced
// element with a positive natural size in the requested axis (inlineAxis=true
// for the grid's inline axis, false for the block axis). Used to suppress the
// default `normal` stretch for replaced items per CSS Box Alignment §6.1.
func (gla *GridLayoutAlgorithm) replacedHasNaturalSize(node *LayoutInputNode, wdm WritingDirectionMode, inlineAxis bool) bool {
	if node == nil || node.DOMNode == nil || !IsReplacedElement(node.DOMNode) {
		return false
	}
	// Form controls (the semi-replaced elements: input, textarea, select,
	// button) are replaced for rendering but, unlike media/embedded content,
	// behave as NON-replaced for box alignment: Blink's
	// LayoutObject::IsReplaced() returns false for them, so their default
	// `normal` self-alignment resolves to stretch (kStretchImplicit) and they
	// fill the grid area — see WPT stretch-grid-item-{checkbox,radio}-input.
	if html.IsSemiReplacedElementTag(node.DOMNode.TagName) {
		return false
	}
	info := GetNaturalSizingInfo(gla.ctx, node)
	// NaturalSizingInfo holds PHYSICAL dimensions. The inline axis maps to the
	// physical width in horizontal writing modes and the physical height in
	// vertical writing modes (and vice-versa for the block axis).
	if inlineAxis == wdm.IsVertical() {
		return info.HasHeight
	}
	return info.HasWidth
}

// gridArea is a rectangle in the grid container's content-box coordinates.
type gridArea struct {
	offset LogicalOffset
	size   LogicalSize
}

// gridTrackGeometry is the laid-out grid: each track's offset (content-box
// relative) and size, per axis, and the names that resolve grid lines.
// Stands in for the GridLayoutData + GridPlacementData that Blink's
// ComputeOutOfFlowItemContainingRect reads.
type gridTrackGeometry struct {
	colOffsets, colSizes []float64
	rowOffsets, rowSizes []float64
	lines                gridLineResolver
}

// isPlacedWithinGridArea reports whether an out-of-flow box has a
// non-auto grid placement, so its containing block (when it is the grid) is
// the grid area. Mirrors IsPlacedWithinGridOrGridLanesArea
// (out_of_flow_layout_part.cc:798-807 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func isPlacedWithinGridArea(style *css.Style) bool {
	if style == nil {
		return false
	}
	colStart, colEnd := placementLonghands(style, true)
	rowStart, rowEnd := placementLonghands(style, false)
	return !colStart.Auto || !colEnd.Auto || !rowStart.Auto || !rowEnd.Auto
}

// trackStartOffset is the offset of grid line `line` used as a start edge:
// the start of the track after it. trackEndOffset is the line used as an
// end edge: the end of the track before it. Mirror Blink's TrackStartOffset
// / TrackEndOffset (grid_layout_algorithm.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func trackStartOffset(offsets, sizes []float64, line int) float64 {
	if line < len(offsets) {
		return offsets[line]
	}
	return trackEndOffset(offsets, sizes, line)
}

func trackEndOffset(offsets, sizes []float64, line int) float64 {
	if line == 0 {
		return offsets[0]
	}
	size := 0.0
	if line-1 < len(sizes) {
		size = sizes[line-1]
	}
	return offsets[line-1] + size
}

// ComputeOutOfFlowItemContainingRect returns the grid area an out-of-flow
// box is placed in: the padding box with each non-auto grid line's edge
// moved to that line. Mirrors GridLayoutAlgorithm::
// ComputeOutOfFlowItemContainingRect (grid_layout_algorithm.cc:2742-2791 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (g gridTrackGeometry) ComputeOutOfFlowItemContainingRect(style *css.Style, paddingBox gridArea) gridArea {
	colStart, colEnd := g.outOfFlowLines(style, true, len(g.colOffsets))
	rowStart, rowEnd := g.outOfFlowLines(style, false, len(g.rowOffsets))
	inlineStart, inlineEnd := paddingBox.offset.InlineOffset, paddingBox.offset.InlineOffset+paddingBox.size.InlineSize
	blockStart, blockEnd := paddingBox.offset.BlockOffset, paddingBox.offset.BlockOffset+paddingBox.size.BlockSize
	if len(g.colOffsets) > 0 {
		if colStart >= 0 {
			inlineStart = trackStartOffset(g.colOffsets, g.colSizes, colStart)
		}
		if colEnd >= 0 {
			inlineEnd = trackEndOffset(g.colOffsets, g.colSizes, colEnd)
		}
	}
	if len(g.rowOffsets) > 0 {
		if rowStart >= 0 {
			blockStart = trackStartOffset(g.rowOffsets, g.rowSizes, rowStart)
		}
		if rowEnd >= 0 {
			blockEnd = trackEndOffset(g.rowOffsets, g.rowSizes, rowEnd)
		}
	}
	return gridArea{
		offset: LogicalOffset{InlineOffset: inlineStart, BlockOffset: blockStart},
		size:   LogicalSize{InlineSize: max(0, inlineEnd-inlineStart), BlockSize: max(0, blockEnd-blockStart)},
	}
}

// PlaceOutOfFlowItems adds the grid's out-of-flow children as candidates.
// When the grid is their containing block the static position is the start
// of their grid area (the padding box when not placed); otherwise it is the
// content-box start. Mirrors GridLayoutAlgorithm::PlaceOutOfFlowItems
// (grid_layout_algorithm.cc:2602-2681 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), including its use of
// IsAbsoluteContainer for fixed-position children too (:2621). Not ported:
// AlignmentOffsetForOutOfFlow (justify-self / align-self on the static
// position) and the fragmentation deferral of a static position in a later
// fragment (louis14 grids do not fragment).
func (gla *GridLayoutAlgorithm) PlaceOutOfFlowItems(builder *BoxFragmentBuilder, oofChildren []*LayoutInputNode, tracks gridTrackGeometry, paddingBox gridArea) {
	isAbsoluteContainer := gla.node.IsAbsoluteContainer()
	isFixedContainer := gla.node.IsAbsoluteContainer()
	for _, child := range oofChildren {
		isFixed := child.Style().GetPosition() == css.PositionFixed
		var staticPosition LogicalStaticPosition
		if (isAbsoluteContainer && !isFixed) || (isFixedContainer && isFixed) {
			staticPosition.Offset = tracks.ComputeOutOfFlowItemContainingRect(child.Style(), paddingBox).offset
		}
		builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
			Node:            child,
			StaticPosition:  staticPosition,
			IsFixedPosition: isFixed,
		})
	}
}

// outOfFlowLines resolves an out-of-flow box's lines in one axis to 0-based
// indices, -1 for the padding edge: a line is kept when its longhand is not
// auto and it lies within the grid; a line before the grid is the padding
// edge, the other line kept. Mirrors
// GridPlacement::ResolveOutOfFlowItemGridLines (core/layout/grid/
// grid_placement.cc:659-703 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (g gridTrackGeometry) outOfFlowLines(style *css.Style, columns bool, trackCount int) (start, end int) {
	start, end = -1, -1
	initial, final := placementLonghands(style, columns)
	span := g.lines.resolveLines(initial, final, columns)
	if !span.definite {
		return start, end
	}
	if !initial.Auto && span.start >= 0 && span.start <= trackCount {
		start = span.start
	}
	if !final.Auto && span.end >= 0 && span.end <= trackCount {
		end = span.end
	}
	return start, end
}

// clampBlockSizeAround clamps a content block size by the grid's min/max
// block size, intrinsic keywords there resolving against intrinsic.
func (gla *GridLayoutAlgorithm) clampBlockSizeAround(size, intrinsic float64, wdm WritingDirectionMode, geom FragmentGeometry) float64 {
	size = max(size, ResolveMinBlockSizeWithIntrinsic(gla.style, wdm, gla.space, geom, intrinsic))
	if maxBlock, hasMax := ResolveMaxBlockSizeWithIntrinsic(gla.style, wdm, gla.space, geom, intrinsic); hasMax {
		size = min(size, maxBlock)
	}
	return size
}

// clampBlockSize is the content block size an indefinite grid resolves to
// from its intrinsic block size (ComputeBlockSizeForFragment).
func (gla *GridLayoutAlgorithm) clampBlockSize(intrinsic float64, wdm WritingDirectionMode, geom FragmentGeometry) float64 {
	return gla.clampBlockSizeAround(intrinsic, intrinsic, wdm, geom)
}

// rowsDependOnAvailableSize reports whether the rows resolve differently
// once the block size is definite: a flexible or percentage row, or a
// percentage row gap (NeedsAdditionalLayoutPass, grid_layout_utils.cc;
// IsDependentOnAvailableSize, grid_track_collection.cc:1133-1137,
// 1191-1195 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Not ported: the
// flex-item case of an initial block size forced indefinite.
func (gla *GridLayoutAlgorithm) rowsDependOnAvailableSize(tracks []css.GridTrack) bool {
	if propertyHasPercent(gla.style, "row-gap") {
		return true
	}
	for _, t := range tracks {
		if t.Fr > 0 || t.MaxFr > 0 || t.Percent > 0 {
			return true
		}
	}
	return false
}

// layoutItemsAtColumns lays each in-flow item out at its columns' size, its
// first pass (item.result, item.margins), whose block sizes the rows size
// from.
func (gla *GridLayoutAlgorithm) layoutItemsAtColumns(items []*gridItem, colSizes []float64, colGap float64, hasExplicitBlock bool, explicitBlockSize float64) {
	blockSize := float64(Indefinite)
	if hasExplicitBlock {
		blockSize = explicitBlockSize
	}
	for _, item := range items {
		item.result, item.margins = gla.layoutItemAtColumns(item, colSizes, colGap, blockSize)
	}
}

// layoutItemAtColumns lays an in-flow item out at its columns' size, with
// blockSize (the grid's definite content block size, or Indefinite) as its
// available and percentage block size, and returns the result and its
// margins. The first pass and the row contributions' measures share it.
func (gla *GridLayoutAlgorithm) layoutItemAtColumns(item *gridItem, colSizes []float64, colGap, blockSize float64) (*LayoutResult, LogicalEdges) {
	wdm := gla.space.WritingDirection
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
	stretchInline := gla.shouldStretchInline(item, wdm)
	childWDM := NewWritingDirectionMode(item.style)
	// Use NewConstraintSpaceBuilder so orthogonal items (e.g. writing-mode:
	// vertical-rl inside a horizontal grid) get axis-swapped spaces.
	// For orthogonal items: the grid's inline (column width) becomes the child's
	// block; the grid's block (row height) becomes the child's inline.
	// blockSize is the available block so orthogonal children know the grid
	// height when sizing their content (enabling correct line-wrapping).
	// Mirrors Blink's GridLayoutAlgorithm::CreateConstraintSpaceForLayout.
	// CSS Box Alignment §6.1 / CSS Grid §12.5: when justify-self is not
	// "stretch" or "normal" and the item's inline-size is auto, the item
	// must be sized to its max-content clamped to the track (ShrinkToFit),
	// not stretched to the full track.  Only applies to auto-sized items:
	// an explicit width (e.g. "width:50px") is resolved by
	// CalculateInitialFragmentGeometry regardless and must not be replaced
	// by max-content.
	//
	// Mirrors Blink's GridLayoutAlgorithm::ComputeItemSizeForTrackSizing @
	// third_party/blink/renderer/core/layout/grid/grid_layout_algorithm.cc
	// (Chromium 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): non-stretch items
	// with auto inline-size get kShrinkToFit sizing before CreateConstraintSpace.
	//
	// Without this, a grid item with justify-items:start and auto inline-size
	// is given AvailableInlineSize=trackWidth with IsFixedInlineSize=false,
	// causing CalculateInitialFragmentGeometry to fill the full track width.
	// An item whose max-content is 1px narrower than the track then fails to
	// wrap its inline children (bug: firefox-bug-1881495).
	isFixedInline := stretchInline
	if !stretchInline {
		if isAutoOrUnsetInline(item.style, wdm) { // auto inline-size: apply ShrinkToFit
			mmSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
				SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
				SetAvailableSize(LogicalSize{InlineSize: itemInline, BlockSize: blockSize}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: itemInline, BlockSize: blockSize}).
				Build()
			mm := ComputeMinMaxSizes(gla.ctx, item.node, mmSpace)
			itemInline = mm.ShrinkToFit(itemInline)
			isFixedInline = true // item is definitively sized at ShrinkToFit
		}
	}
	childSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
		SetInlineAutoBehavior(gridItemInlineAutoBehavior(stretchInline)).
		SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, gla.ctx)).
		SetOrthogonalFallbackBlockSize(gla.space.OrthogonalFallbackBlockSize).
		SetAvailableSize(LogicalSize{InlineSize: itemInline, BlockSize: blockSize}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: itemInline, BlockSize: blockSize}).
		SetPercentageResolutionInlineSize(itemInline).
		SetIsFixedInlineSize(isFixedInline).
		Build()
	return layoutElement(gla.ctx, item.node, childSpace), ResolveMargins(item.style, wdm, itemInline)
}
