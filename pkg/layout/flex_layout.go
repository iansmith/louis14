package layout

import (
	"math"
	"strconv"
	"strings"

	"louis14/pkg/css"
)

// FlexLayoutAlgorithm implements the CSS Flexible Box Layout Module Level 1 §9.
// It distributes flex items along the main axis and aligns them on the cross axis.
//
// Ported from Blink's FlexLayoutAlgorithm pattern.
type FlexLayoutAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	style *css.Style
	space ConstraintSpace
}

// NewFlexLayoutAlgorithm creates a flex layout algorithm for the given node.
func NewFlexLayoutAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *FlexLayoutAlgorithm {
	return &FlexLayoutAlgorithm{
		ctx:   ctx,
		node:  node,
		style: node.Style(),
		space: space,
	}
}

// flexItem holds layout information for a single flex item.
type flexItem struct {
	node          *LayoutInputNode
	style         *css.Style
	wdm           WritingDirectionMode
	geom          FragmentGeometry
	margins       LogicalEdges
	flexBasis     float64 // resolved flex-basis (content-box in main axis)
	hypothetical  float64 // hypothetical main size (clamped flex-basis)
	resolvedMain  float64 // final main size after flex grow/shrink
	crossSize     float64 // final cross size (border-box)
	mainOffset    float64 // position along main axis (content-box offset within container)
	crossOffset   float64 // position along cross axis (margin-box start within container)
	fragment      *PhysicalFragment
	flexGrow      float64
	flexShrink    float64
	frozen        bool
	order         int
	isRow         bool   // true if main axis = inline axis
}

// mainMarginSum returns the total margin in the main axis.
func (fi *flexItem) mainMarginSum() float64 {
	if fi.isRow {
		return fi.margins.InlineSum()
	}
	return fi.margins.BlockSum()
}

// mainMarginStart returns the margin-start in the main axis.
func (fi *flexItem) mainMarginStart() float64 {
	if fi.isRow {
		return fi.margins.InlineStart
	}
	return fi.margins.BlockStart
}

// crossMarginStart returns the margin-start in the cross axis.
func (fi *flexItem) crossMarginStart() float64 {
	if fi.isRow {
		return fi.margins.BlockStart
	}
	return fi.margins.InlineStart
}

// crossMarginSum returns the total margin in the cross axis.
func (fi *flexItem) crossMarginSum() float64 {
	if fi.isRow {
		return fi.margins.BlockSum()
	}
	return fi.margins.InlineSum()
}

// flexLine holds the items on one flex line.
type flexLine struct {
	items     []*flexItem
	crossSize float64
}

// Layout performs flex layout and returns the LayoutResult.
func (fla *FlexLayoutAlgorithm) Layout() *LayoutResult {
	wdm := fla.space.WritingDirection
	geom := ComputeFragmentGeometry(fla.style, wdm)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(fla.node)

	// §9.1 — Determine flex direction and whether main axis == inline axis.
	flexDir := fla.getFlexDirection()
	isRow := flexDir == "row" || flexDir == "row-reverse"
	reverseMain := flexDir == "row-reverse" || flexDir == "column-reverse"
	wrapMode := fla.getFlexWrap()
	reverseCross := wrapMode == "wrap-reverse"

	// §9.2 — Resolve container inline-size.
	var contentInlineSize float64
	if explicitInline, ok := ResolveInlineSize(fla.style, wdm, fla.space, geom); ok {
		contentInlineSize = explicitInline
	} else if needsShrinkToFit(fla.style) {
		minMax := ComputeMinMaxSizes(fla.ctx, fla.node, fla.space)
		available := fla.space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if available < 0 {
			available = 0
		}
		contentInlineSize = minMax.ShrinkToFit(available)
	} else {
		contentInlineSize = fla.space.AvailableSize.InlineSize - geom.InlineBorderPadding()
		if contentInlineSize < 0 {
			contentInlineSize = 0
		}
	}
	// Apply min/max inline constraints.
	minInline := ResolveMinInlineSize(fla.style, wdm, fla.space, geom)
	if contentInlineSize < minInline {
		contentInlineSize = minInline
	}
	if maxInline, hasMax := ResolveMaxInlineSize(fla.style, wdm, fla.space, geom); hasMax {
		if contentInlineSize > maxInline {
			contentInlineSize = maxInline
		}
	}

	// Resolve container block-size.
	explicitBlockSize, hasExplicitBlock := ResolveBlockSize(fla.style, wdm, fla.space, geom)

	// Determine container main/cross sizes (content-box).
	var containerMainSize float64
	var containerCrossSize float64
	var hasDefiniteMain, hasDefiniteCross bool

	if isRow {
		containerMainSize = contentInlineSize
		hasDefiniteMain = true // inline-size is always definite
		if hasExplicitBlock {
			containerCrossSize = explicitBlockSize
			hasDefiniteCross = true
		}
	} else {
		// column direction
		if hasExplicitBlock {
			containerMainSize = explicitBlockSize
			hasDefiniteMain = true
		}
		containerCrossSize = contentInlineSize
		hasDefiniteCross = true
	}

	// Resolve gap properties.
	mainGap, crossGap := fla.resolveGaps(wdm, isRow, contentInlineSize)

	// §9.3 — Collect flex items.
	allItems := fla.collectItems(wdm, contentInlineSize, containerMainSize, hasDefiniteMain, isRow)

	// §9.3 — Sort by order property.
	sortFlexItems(allItems)

	// §9.3 — Build flex lines.
	lines := fla.buildFlexLines(allItems, wrapMode, containerMainSize, hasDefiniteMain, mainGap)

	// §9.7 — Resolve flexible lengths for each line.
	for _, line := range lines {
		fla.resolveFlexibleLengths(line, containerMainSize, hasDefiniteMain, mainGap)
	}

	// §9.4 — Determine cross-size of items and lines.
	// Do a layout pass for each item at its resolved main size to get intrinsic cross size.
	for _, line := range lines {
		lineCrossMax := 0.0
		for _, item := range line.items {
			cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
				item.resolvedMain, Indefinite)
			result := layoutElement(fla.ctx, item.node, cs)
			item.fragment = result.Fragment
			lf := NewLogicalFragment(wdm, item.fragment)
			var itemCross float64
			if isRow {
				itemCross = lf.BlockSize()
			} else {
				itemCross = lf.InlineSize()
			}
			item.crossSize = itemCross
			if itemCross > lineCrossMax {
				lineCrossMax = itemCross
			}
		}
		line.crossSize = lineCrossMax
	}

	// §9.4 step 8: If single-line and container has definite cross size,
	// the flex line cross-size equals the container cross-size.
	if wrapMode == "nowrap" && len(lines) == 1 && hasDefiniteCross {
		lines[0].crossSize = containerCrossSize
	}

	// §9.4 — Stretch items to line cross-size (align-self: stretch).
	alignItems := fla.getAlignItems()
	for _, line := range lines {
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			if selfAlign == "stretch" {
				// Clamp to item's own min/max cross size.
				stretchCross := line.crossSize
				if isRow {
					// cross = block
					minBlock := ResolveMinBlockSize(item.style, item.wdm, fla.space, item.geom)
					if stretchCross < minBlock {
						stretchCross = minBlock
					}
					if maxBlock, hasMax := ResolveMaxBlockSize(item.style, item.wdm, fla.space, item.geom); hasMax {
						if stretchCross > maxBlock {
							stretchCross = maxBlock
						}
					}
				} else {
					// cross = inline
					minInlineItem := ResolveMinInlineSize(item.style, item.wdm, fla.space, item.geom)
					if stretchCross < minInlineItem {
						stretchCross = minInlineItem
					}
					if maxInlineItem, hasMax := ResolveMaxInlineSize(item.style, item.wdm, fla.space, item.geom); hasMax {
						if stretchCross > maxInlineItem {
							stretchCross = maxInlineItem
						}
					}
				}
				if stretchCross != item.crossSize {
					cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
						item.resolvedMain, stretchCross)
					result := layoutElement(fla.ctx, item.node, cs)
					item.fragment = result.Fragment
					item.crossSize = stretchCross
				}
			}
		}
	}

	// §9.5 — Compute total cross-size of all lines.
	totalLinesCross := 0.0
	for i, line := range lines {
		totalLinesCross += line.crossSize
		if i < len(lines)-1 {
			totalLinesCross += crossGap
		}
	}

	// §9.6 — Determine container cross-size.
	if !hasDefiniteCross {
		containerCrossSize = totalLinesCross
		hasDefiniteCross = true
	}

	// Apply min/max cross constraints.
	if isRow {
		// cross = block
		minBlock := ResolveMinBlockSize(fla.style, wdm, fla.space, geom)
		if containerCrossSize < minBlock {
			containerCrossSize = minBlock
		}
		if maxBlock, hasMax := ResolveMaxBlockSize(fla.style, wdm, fla.space, geom); hasMax {
			if containerCrossSize > maxBlock {
				containerCrossSize = maxBlock
			}
		}
	} else {
		// cross = inline (already constrained above via contentInlineSize)
	}

	// §9.6 — align-content: distribute lines within container cross-size.
	alignContent := fla.getAlignContent()
	var lineOffsets []float64
	if len(lines) == 1 {
		// Single line: no multi-line alignment needed.
		// But still respect reverseCross for single line.
		if reverseCross && hasDefiniteCross {
			lineOffsets = []float64{containerCrossSize - lines[0].crossSize}
		} else {
			lineOffsets = []float64{0}
		}
	} else {
		lineOffsets = computeAlignContent(lines, containerCrossSize, totalLinesCross, alignContent, reverseCross, crossGap)
	}

	// §9.8 — Main axis alignment (justify-content) and item positioning.
	justifyContent := fla.getJustifyContent()

	// §9.9 — Cross axis alignment per item (align-self).
	for lineIdx, line := range lines {
		// Compute main offsets using justify-content.
		computeItemMainOffsets(line.items, containerMainSize, justifyContent, reverseMain, mainGap)

		crossStart := lineOffsets[lineIdx]
		// Align items in this line.
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			var itemCrossOffset float64
			switch selfAlign {
			case "flex-end", "end":
				itemCrossOffset = crossStart + line.crossSize - item.crossSize
			case "center":
				itemCrossOffset = crossStart + (line.crossSize-item.crossSize)/2
			case "baseline":
				// Approximate with flex-start for now.
				itemCrossOffset = crossStart
			default: // flex-start, stretch
				itemCrossOffset = crossStart
			}
			item.crossOffset = itemCrossOffset
		}
	}

	// §9.9 — Add children to builder.
	// mainOffset and crossOffset are already the content-box positions
	// (margins accounted for by mainMarginStart/crossMarginStart).
	for _, line := range lines {
		for _, item := range line.items {
			var inlineOff, blockOff float64
			if isRow {
				inlineOff = item.mainOffset
				blockOff = item.crossOffset + item.crossMarginStart()
			} else {
				inlineOff = item.crossOffset + item.crossMarginStart()
				blockOff = item.mainOffset
			}
			builder.AddChild(item.fragment, LogicalOffset{
				InlineOffset: inlineOff,
				BlockOffset:  blockOff,
			})
		}
	}

	// Compute container block-size.
	var intrinsicBlockSize float64
	if isRow {
		// Block-size = sum of line cross sizes + gaps.
		intrinsicBlockSize = totalLinesCross
	} else {
		if hasDefiniteMain {
			intrinsicBlockSize = containerMainSize
		} else {
			// Auto block-size for column flex: the main axis is block.
			// For single-column flex, sum all item main sizes + margins + gaps.
			// For multi-column (wrap), each line is a column.
			total := 0.0
			for _, line := range lines {
				lineTot := mainGap * float64(len(line.items)-1)
				for _, item := range line.items {
					lineTot += item.resolvedMain + item.mainMarginSum()
				}
				if lineTot > total {
					total = lineTot
				}
			}
			intrinsicBlockSize = total
		}
	}

	finalBlockSize := intrinsicBlockSize
	if hasExplicitBlock {
		finalBlockSize = explicitBlockSize
	}

	// Apply min/max block constraints.
	minBlock := ResolveMinBlockSize(fla.style, wdm, fla.space, geom)
	if finalBlockSize < minBlock {
		finalBlockSize = minBlock
	}
	if maxBlock, hasMax := ResolveMaxBlockSize(fla.style, wdm, fla.space, geom); hasMax {
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
	physMargin := ToPhysicalEdges(ResolveMargins(fla.style, wdm, fla.space.AvailableSize.InlineSize), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})

	// Layout OOF children.
	if len(builder.outOfFlowCandidates) > 0 {
		oofPart := &OutOfFlowLayoutPart{
			ctx:                 fla.ctx,
			containingBlockWDM:  wdm,
			containingBlockSize: LogicalSize{InlineSize: contentInlineSize, BlockSize: finalBlockSize},
			geom:                geom,
		}
		oofPart.LayoutCandidates(builder.outOfFlowCandidates, builder)
	}

	result := builder.Build()

	// CSS position:relative.
	if fla.style != nil && fla.style.GetPosition() == css.PositionRelative {
		cbWidth := fla.space.AvailableSize.InlineSize
		cbHeight := fla.space.AvailableSize.BlockSize
		if cbHeight == Indefinite {
			cbHeight = 0
		}
		offset := fla.style.GetPositionOffsetResolved(cbWidth, cbHeight)
		var dx, dy float64
		if offset.HasLeft {
			dx = offset.Left
		} else if offset.HasRight {
			dx = -offset.Right
		}
		if offset.HasTop {
			dy = offset.Top
		} else if offset.HasBottom {
			dy = -offset.Bottom
		}
		result.Fragment.RelativeOffset = PhysicalOffset{X: dx, Y: dy}
	}

	return result
}

// collectItems walks node.Children() and returns flex items (skipping OOF and display:none).
func (fla *FlexLayoutAlgorithm) collectItems(
	wdm WritingDirectionMode,
	contentInlineSize float64,
	containerMainSize float64,
	hasDefiniteMain bool,
	isRow bool,
) []*flexItem {
	var items []*flexItem

	for _, child := range fla.node.Children() {
		if child.IsText() {
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			// TODO: Add as OOF candidate.
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)
		childGeom := ComputeFragmentGeometry(childStyle, childWDM)

		// Resolve margins for the item.
		// In flex layout, margin auto is used for alignment — resolve to 0 for now,
		// we handle auto margins later.
		childMargins := fla.resolveItemMargins(childStyle, childWDM, contentInlineSize, isRow)

		// Compute flex properties.
		flexGrow := fla.parseFloat(childStyle, "flex-grow", 0)
		flexShrink := fla.parseFloat(childStyle, "flex-shrink", 1)
		order := fla.parseInt(childStyle, "order", 0)

		// Compute flex-basis and hypothetical main size.
		flexBasis := fla.resolveFlexBasis(child, childStyle, childWDM, childGeom, wdm,
			contentInlineSize, containerMainSize, hasDefiniteMain, isRow)

		// Clamp flex-basis by min/max main size.
		hyp := fla.clampMainSize(flexBasis, child, childStyle, childWDM, childGeom, wdm,
			contentInlineSize, containerMainSize, isRow)

		item := &flexItem{
			node:         child,
			style:        childStyle,
			wdm:          childWDM,
			geom:         childGeom,
			margins:      childMargins,
			flexBasis:    flexBasis,
			hypothetical: hyp,
			flexGrow:     flexGrow,
			flexShrink:   flexShrink,
			order:        order,
			isRow:        isRow,
		}
		items = append(items, item)
	}
	return items
}

// resolveItemMargins resolves margins for a flex item.
// Auto margins in the main axis are treated as 0 initially; they're handled in alignment.
func (fla *FlexLayoutAlgorithm) resolveItemMargins(
	style *css.Style,
	wdm WritingDirectionMode,
	containingInlineSize float64,
	isRow bool,
) LogicalEdges {
	// Use the standard margin resolution. Auto margins → 0 in GetAllMarginsForWidth.
	return ResolveMargins(style, wdm, containingInlineSize)
}

// resolveFlexBasis computes the flex-basis content size for an item.
func (fla *FlexLayoutAlgorithm) resolveFlexBasis(
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	containerMainSize float64,
	hasDefiniteMain bool,
	isRow bool,
) float64 {
	basisVal := ""
	if v, ok := style.Get("flex-basis"); ok {
		basisVal = strings.TrimSpace(v)
	}
	if basisVal == "" {
		basisVal = "auto"
	}

	// "content" is equivalent to auto for our purposes.
	if basisVal == "auto" || basisVal == "content" {
		// Use main-size property if explicitly set, else use max-content.
		if isRow {
			// main axis = inline
			if explicit, ok := ResolveInlineSize(style, childWDM, ConstraintSpace{
				AvailableSize:            LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
				PercentageResolutionSize: LogicalSize{InlineSize: contentInlineSize},
				WritingDirection:         childWDM,
			}, childGeom); ok {
				return explicit
			}
		} else {
			// main axis = block
			if explicit, ok := ResolveBlockSize(style, childWDM, ConstraintSpace{
				AvailableSize:            LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
				PercentageResolutionSize: LogicalSize{InlineSize: contentInlineSize},
				WritingDirection:         childWDM,
			}, childGeom); ok {
				return explicit
			}
		}
		// No explicit size → use max-content.
		return fla.itemMaxContentMainSize(child, style, childWDM, childGeom, parentWDM,
			contentInlineSize, isRow)
	}

	// Numeric flex-basis.
	// Parse as length (includes px, em, %, etc.)
	parentSpace := ConstraintSpace{
		AvailableSize: LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  Indefinite,
		},
		PercentageResolutionSize: LogicalSize{
			InlineSize: contentInlineSize,
		},
		WritingDirection: parentWDM,
	}
	if isRow {
		// Resolve as inline-size against the container.
		if v, ok := style.GetLength("flex-basis"); ok {
			result := v
			if style.GetBoxSizing() == "border-box" {
				result -= childGeom.InlineBorderPadding()
				if result < 0 {
					result = 0
				}
			}
			return result
		}
		if pct, ok := style.GetPercentage("flex-basis"); ok {
			result := contentInlineSize * pct / 100
			if style.GetBoxSizing() == "border-box" {
				result -= childGeom.InlineBorderPadding()
				if result < 0 {
					result = 0
				}
			}
			return result
		}
	} else {
		// Resolve as block-size.
		if v, ok := style.GetLength("flex-basis"); ok {
			result := v
			if style.GetBoxSizing() == "border-box" {
				result -= childGeom.BlockBorderPadding()
				if result < 0 {
					result = 0
				}
			}
			return result
		}
		if pct, ok := style.GetPercentage("flex-basis"); ok && hasDefiniteMain {
			result := containerMainSize * pct / 100
			if style.GetBoxSizing() == "border-box" {
				result -= childGeom.BlockBorderPadding()
				if result < 0 {
					result = 0
				}
			}
			return result
		}
	}
	_ = parentSpace

	// Fallback: use max-content.
	return fla.itemMaxContentMainSize(child, style, childWDM, childGeom, parentWDM,
		contentInlineSize, isRow)
}

// itemMaxContentMainSize returns the max-content size in the main axis.
func (fla *FlexLayoutAlgorithm) itemMaxContentMainSize(
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	isRow bool,
) float64 {
	space := ConstraintSpace{
		AvailableSize: LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  Indefinite,
		},
		PercentageResolutionSize: LogicalSize{
			InlineSize: contentInlineSize,
		},
		WritingDirection:   childWDM,
		IsNewFormattingContext: true,
	}
	if isRow {
		mm := ComputeMinMaxSizes(fla.ctx, child, space)
		return mm.MaxContent
	}
	// Column: lay out item at full width to get intrinsic block-size.
	result := layoutElement(fla.ctx, child, space)
	lf := NewLogicalFragment(parentWDM, result.Fragment)
	return lf.BlockSize() - childGeom.BlockBorderPadding()
}

// clampMainSize clamps the flex-basis by min/max main size constraints.
func (fla *FlexLayoutAlgorithm) clampMainSize(
	basis float64,
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	containerMainSize float64,
	isRow bool,
) float64 {
	result := basis
	parentSpace := ConstraintSpace{
		AvailableSize:            LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
		PercentageResolutionSize: LogicalSize{InlineSize: contentInlineSize},
		WritingDirection:         childWDM,
	}
	if isRow {
		min := ResolveMinInlineSize(style, childWDM, parentSpace, childGeom)
		if result < min {
			result = min
		}
		if max, ok := ResolveMaxInlineSize(style, childWDM, parentSpace, childGeom); ok {
			if result > max {
				result = max
			}
		}
	} else {
		min := ResolveMinBlockSize(style, childWDM, parentSpace, childGeom)
		if result < min {
			result = min
		}
		if max, ok := ResolveMaxBlockSize(style, childWDM, parentSpace, childGeom); ok {
			if result > max {
				result = max
			}
		}
	}
	return result
}

// buildFlexLines distributes items into lines based on flex-wrap.
func (fla *FlexLayoutAlgorithm) buildFlexLines(
	items []*flexItem,
	wrapMode string,
	containerMainSize float64,
	hasDefiniteMain bool,
	mainGap float64,
) []*flexLine {
	if wrapMode == "nowrap" || !hasDefiniteMain {
		return []*flexLine{{items: items}}
	}

	var lines []*flexLine
	var currentLine []*flexItem
	currentSize := 0.0

	for i, item := range items {
		itemSize := item.hypothetical + item.mainMarginSum()
		if i == 0 {
			currentLine = append(currentLine, item)
			currentSize = itemSize
			continue
		}
		gap := 0.0
		if len(currentLine) > 0 {
			gap = mainGap
		}
		if currentSize+gap+itemSize > containerMainSize && len(currentLine) > 0 {
			lines = append(lines, &flexLine{items: currentLine})
			currentLine = []*flexItem{item}
			currentSize = itemSize
		} else {
			currentLine = append(currentLine, item)
			currentSize += gap + itemSize
		}
	}
	if len(currentLine) > 0 {
		lines = append(lines, &flexLine{items: currentLine})
	}
	if len(lines) == 0 {
		lines = []*flexLine{{}}
	}
	return lines
}

// resolveFlexibleLengths implements §9.7: the flex algorithm.
func (fla *FlexLayoutAlgorithm) resolveFlexibleLengths(
	line *flexLine,
	containerMainSize float64,
	hasDefiniteMain bool,
	mainGap float64,
) {
	items := line.items

	// If no definite container main size, just use hypothetical sizes.
	if !hasDefiniteMain {
		for _, item := range items {
			item.resolvedMain = item.hypothetical
		}
		return
	}

	// Compute initial free space.
	usedSpace := mainGap * float64(len(items)-1)
	if len(items) == 0 {
		usedSpace = 0
	}
	for _, item := range items {
		usedSpace += item.hypothetical + item.mainMarginSum()
	}
	freeSpace := containerMainSize - usedSpace

	// Initialize.
	for _, item := range items {
		item.resolvedMain = item.hypothetical
		item.frozen = false
	}

	if math.Abs(freeSpace) < 0.001 {
		return
	}

	growing := freeSpace > 0

	// Freeze items that won't participate.
	for _, item := range items {
		if growing && item.flexGrow == 0 {
			item.frozen = true
		} else if !growing && item.flexShrink == 0 {
			item.frozen = true
		}
	}

	// Iterative flex algorithm.
	for iter := 0; iter < 100; iter++ {
		// Compute total flex factor of unfrozen items.
		var totalFactor float64
		var unfrozenCount int
		for _, item := range items {
			if !item.frozen {
				if growing {
					totalFactor += item.flexGrow
				} else {
					totalFactor += item.flexShrink * item.flexBasis
				}
				unfrozenCount++
			}
		}
		if unfrozenCount == 0 || totalFactor < 0.001 {
			break
		}

		// Recompute free space from frozen items.
		freeSpace = containerMainSize - mainGap*float64(len(items)-1)
		for _, item := range items {
			freeSpace -= item.resolvedMain + item.mainMarginSum()
		}

		if math.Abs(freeSpace) < 0.001 {
			break
		}

		// Distribute free space.
		anyUnfrozen := false
		for _, item := range items {
			if item.frozen {
				continue
			}
			anyUnfrozen = true
			var delta float64
			if growing {
				if totalFactor > 0 {
					delta = freeSpace * item.flexGrow / totalFactor
				}
			} else {
				if totalFactor > 0 {
					delta = freeSpace * (item.flexShrink * item.flexBasis) / totalFactor
				}
			}
			item.resolvedMain += delta
		}

		if !anyUnfrozen {
			break
		}

		// Freeze items that hit min/max constraints.
		frozenAny := false
		for _, item := range items {
			if item.frozen {
				continue
			}
			// Min constraint.
			minMain := item.hypothetical // hypothetical already incorporates CSS min
			// Actually use 0 as min if no CSS min.
			_ = minMain
			// Just clamp to 0 for simplicity.
			if item.resolvedMain < 0 {
				item.resolvedMain = 0
				item.frozen = true
				frozenAny = true
			}
		}
		if !frozenAny {
			break
		}
	}

	// Final clamp: no item can be negative.
	for _, item := range items {
		if item.resolvedMain < 0 {
			item.resolvedMain = 0
		}
	}
}

// buildItemConstraintSpace builds the constraint space for laying out a flex item
// at a given main size and cross size.
func (fla *FlexLayoutAlgorithm) buildItemConstraintSpace(
	item *flexItem,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	isRow bool,
	mainSize float64,
	crossSize float64, // Indefinite if not known
) ConstraintSpace {
	childWDM := item.wdm

	// Flex items always establish a new formatting context.
	b := NewConstraintSpaceBuilder(parentWDM, childWDM, true)

	if isRow {
		// main = inline, cross = block
		avail := LogicalSize{
			InlineSize: mainSize + item.geom.InlineBorderPadding(),
			BlockSize:  Indefinite,
		}
		if crossSize != Indefinite {
			avail.BlockSize = crossSize + item.geom.BlockBorderPadding()
		}
		b.SetAvailableSize(avail)
		b.SetPercentageResolutionSize(LogicalSize{
			InlineSize: mainSize,
			BlockSize:  0,
		})
		b.SetIsFixedInlineSize(true)
		if crossSize != Indefinite {
			b.SetIsFixedBlockSize(true)
		}
	} else {
		// main = block, cross = inline
		// The cross size (inline) is always the container's content inline size.
		avail := LogicalSize{
			InlineSize: contentInlineSize + item.geom.InlineBorderPadding(),
			BlockSize:  Indefinite,
		}
		if mainSize != Indefinite {
			avail.BlockSize = mainSize + item.geom.BlockBorderPadding()
		}
		b.SetAvailableSize(avail)
		b.SetPercentageResolutionSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  mainSize,
		})
		b.SetIsFixedInlineSize(true)
		if mainSize != Indefinite {
			b.SetIsFixedBlockSize(true)
		}
	}

	return b.Build()
}

// computeItemMainOffsets assigns main-axis offsets using justify-content.
func computeItemMainOffsets(
	items []*flexItem,
	containerMainSize float64,
	justifyContent string,
	reverseMain bool,
	mainGap float64,
) {
	if len(items) == 0 {
		return
	}

	// Compute total item sizes (including margins).
	totalItemSize := mainGap * float64(len(items)-1)
	for _, item := range items {
		totalItemSize += item.resolvedMain + item.mainMarginSum()
	}
	freeSpace := containerMainSize - totalItemSize
	if freeSpace < 0 {
		freeSpace = 0
	}

	var initialOffset, gap float64
	switch justifyContent {
	case "flex-end", "end":
		initialOffset = freeSpace
		gap = mainGap
	case "center":
		initialOffset = freeSpace / 2
		gap = mainGap
	case "space-between":
		initialOffset = 0
		if len(items) > 1 {
			gap = (freeSpace + mainGap*float64(len(items)-1)) / float64(len(items)-1)
		} else {
			gap = 0
		}
	case "space-around":
		perItem := 0.0
		if len(items) > 0 {
			perItem = freeSpace / float64(len(items))
		}
		initialOffset = perItem / 2
		gap = perItem + mainGap
	case "space-evenly":
		spacing := 0.0
		if len(items)+1 > 0 {
			spacing = freeSpace / float64(len(items)+1)
		}
		initialOffset = spacing
		gap = spacing + mainGap
	default: // flex-start, start
		initialOffset = 0
		gap = mainGap
	}

	if reverseMain {
		// Place items right-to-left from the main-end.
		cursor := containerMainSize - initialOffset
		for i, item := range items {
			_ = i
			cursor -= item.resolvedMain + item.mainMarginSum()
			item.mainOffset = cursor + item.mainMarginStart()
			if i < len(items)-1 {
				cursor -= gap
			}
		}
	} else {
		cursor := initialOffset
		for i, item := range items {
			if i > 0 {
				cursor += gap
			}
			item.mainOffset = cursor + item.mainMarginStart()
			cursor += item.resolvedMain + item.mainMarginSum()
		}
	}
}

// computeAlignContent distributes flex lines for multi-line containers.
func computeAlignContent(
	lines []*flexLine,
	containerCrossSize float64,
	totalLinesCross float64,
	alignContent string,
	reverseCross bool,
	crossGap float64,
) []float64 {
	offsets := make([]float64, len(lines))
	freeSpace := containerCrossSize - totalLinesCross
	if freeSpace < 0 {
		freeSpace = 0
	}

	var initialOffset, gap float64
	switch alignContent {
	case "flex-end", "end":
		initialOffset = freeSpace
		gap = crossGap
	case "center":
		initialOffset = freeSpace / 2
		gap = crossGap
	case "space-between":
		initialOffset = 0
		if len(lines) > 1 {
			gap = (freeSpace + crossGap*float64(len(lines)-1)) / float64(len(lines)-1)
		} else {
			gap = 0
		}
	case "space-around":
		perLine := 0.0
		if len(lines) > 0 {
			perLine = freeSpace / float64(len(lines))
		}
		initialOffset = perLine / 2
		gap = perLine + crossGap
	case "stretch":
		// Distribute free space to lines.
		extra := 0.0
		if len(lines) > 0 {
			extra = freeSpace / float64(len(lines))
		}
		for i := range lines {
			lines[i].crossSize += extra
		}
		initialOffset = 0
		gap = crossGap
	default: // flex-start
		initialOffset = 0
		gap = crossGap
	}

	cursor := initialOffset
	if reverseCross {
		for i := len(lines) - 1; i >= 0; i-- {
			offsets[i] = cursor
			cursor += lines[i].crossSize + gap
		}
	} else {
		for i, line := range lines {
			offsets[i] = cursor
			cursor += line.crossSize + gap
		}
	}

	return offsets
}

// getFlexDirection returns the flex-direction value (default: "row").
func (fla *FlexLayoutAlgorithm) getFlexDirection() string {
	if v, ok := fla.style.Get("flex-direction"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "row", "row-reverse", "column", "column-reverse":
			return v
		}
	}
	// Check -webkit-box-orient / -webkit-box-direction.
	if v, ok := fla.style.Get("-webkit-box-orient"); ok {
		orient := strings.TrimSpace(v)
		dir := ""
		if d, ok2 := fla.style.Get("-webkit-box-direction"); ok2 {
			dir = strings.TrimSpace(d)
		}
		if orient == "vertical" {
			if dir == "reverse" {
				return "column-reverse"
			}
			return "column"
		}
		if orient == "horizontal" || orient == "inline-axis" {
			if dir == "reverse" {
				return "row-reverse"
			}
			return "row"
		}
	}
	return "row"
}

// getFlexWrap returns the flex-wrap value (default: "nowrap").
func (fla *FlexLayoutAlgorithm) getFlexWrap() string {
	if v, ok := fla.style.Get("flex-wrap"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "nowrap", "wrap", "wrap-reverse":
			return v
		}
	}
	return "nowrap"
}

// getJustifyContent returns the justify-content value (default: "flex-start").
func (fla *FlexLayoutAlgorithm) getJustifyContent() string {
	if v, ok := fla.style.Get("justify-content"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "flex-start", "flex-end", "center",
			"space-between", "space-around", "space-evenly",
			"start", "end", "left", "right":
			return v
		}
	}
	return "flex-start"
}

// getAlignItems returns the align-items value (default: "stretch").
func (fla *FlexLayoutAlgorithm) getAlignItems() string {
	if v, ok := fla.style.Get("align-items"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "stretch", "flex-start", "flex-end", "center", "baseline",
			"start", "end", "self-start", "self-end":
			return v
		}
	}
	// -webkit-box-align
	if v, ok := fla.style.Get("-webkit-box-align"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "start":
			return "flex-start"
		case "end":
			return "flex-end"
		case "center":
			return "center"
		case "baseline":
			return "baseline"
		case "stretch":
			return "stretch"
		}
	}
	return "stretch"
}

// getAlignSelf returns the effective align-self for an item.
func (fla *FlexLayoutAlgorithm) getAlignSelf(style *css.Style, alignItems string) string {
	if style == nil {
		return alignItems
	}
	if v, ok := style.Get("align-self"); ok {
		v = strings.TrimSpace(v)
		if v != "auto" && v != "" {
			switch v {
			case "stretch", "flex-start", "flex-end", "center", "baseline",
				"start", "end", "self-start", "self-end":
				return v
			}
		}
	}
	return alignItems
}

// getAlignContent returns the align-content value (default: "stretch").
func (fla *FlexLayoutAlgorithm) getAlignContent() string {
	if v, ok := fla.style.Get("align-content"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "stretch", "flex-start", "flex-end", "center",
			"space-between", "space-around", "space-evenly",
			"start", "end":
			return v
		}
	}
	return "stretch"
}

// resolveGaps returns the main and cross gap values.
func (fla *FlexLayoutAlgorithm) resolveGaps(wdm WritingDirectionMode, isRow bool, contentInlineSize float64) (mainGap, crossGap float64) {
	// row-gap and column-gap.
	var rowGap, colGap float64
	if v, ok := fla.style.GetLength("row-gap"); ok {
		rowGap = v
	} else if pct, ok := fla.style.GetPercentage("row-gap"); ok {
		rowGap = contentInlineSize * pct / 100
	}
	if v, ok := fla.style.GetLength("column-gap"); ok {
		colGap = v
	} else if pct, ok := fla.style.GetPercentage("column-gap"); ok {
		colGap = contentInlineSize * pct / 100
	}
	// gap shorthand may have been resolved to row-gap/column-gap already.
	if isRow {
		// main = column direction, cross = row direction (in terms of gap).
		// Actually: flex-direction:row means items flow in inline direction.
		// column-gap = gap between items (main axis).
		// row-gap = gap between lines (cross axis).
		mainGap = colGap
		crossGap = rowGap
	} else {
		mainGap = rowGap
		crossGap = colGap
	}
	_ = wdm
	return
}

// parseFloat parses a CSS property as float64, returning defaultVal if absent/invalid.
func (fla *FlexLayoutAlgorithm) parseFloat(style *css.Style, prop string, defaultVal float64) float64 {
	if v, ok := style.Get(prop); ok {
		v = strings.TrimSpace(v)
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

// parseInt parses a CSS property as int, returning defaultVal if absent/invalid.
func (fla *FlexLayoutAlgorithm) parseInt(style *css.Style, prop string, defaultVal int) int {
	if v, ok := style.Get(prop); ok {
		v = strings.TrimSpace(v)
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

// sortFlexItems sorts items by their order property (stable sort by DOM order within same order).
func sortFlexItems(items []*flexItem) {
	// Simple insertion sort (stable).
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].order < items[j-1].order; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
