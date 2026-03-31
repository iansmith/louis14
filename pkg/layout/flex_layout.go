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
	isRow         bool // true if main axis = inline axis

	// §9.7 freeze bounds (CSS min/max, content-box).
	minMain float64 // effective min main size (§4.5 for auto, or CSS min-width/height)
	maxMain float64 // CSS max main size, or Indefinite if none

	// Auto margins (§8.1): whether each logical edge is margin:auto.
	mainAutoStart  bool
	mainAutoEnd    bool
	crossAutoStart bool
	crossAutoEnd   bool

	// baseline is the first-line baseline position relative to the item's border-box top.
	// 0 means no baseline available (fall back to flex-start).
	baseline float64
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

// crossMarginEnd returns the margin-end in the cross axis.
func (fi *flexItem) crossMarginEnd() float64 {
	if fi.isRow {
		return fi.margins.BlockEnd
	}
	return fi.margins.InlineEnd
}

// mainMarginEnd returns the margin-end in the main axis.
func (fi *flexItem) mainMarginEnd() float64 {
	if fi.isRow {
		return fi.margins.InlineEnd
	}
	return fi.margins.BlockEnd
}

// mainBorderPadding returns the border+padding sum in the main axis.
// resolvedMain is content-box only; adding this gives the border-box size.
func (fi *flexItem) mainBorderPadding() float64 {
	if fi.isRow {
		return fi.geom.InlineBorderPadding()
	}
	return fi.geom.BlockBorderPadding()
}

// outerMainSize returns the margin-box size in the main axis (content + border + padding + margins).
// This is used for free-space calculations and item positioning.
func (fi *flexItem) outerMainSize() float64 {
	return fi.resolvedMain + fi.mainBorderPadding() + fi.mainMarginSum()
}

// outerHypotheticalMainSize returns the margin-box hypothetical size.
func (fi *flexItem) outerHypotheticalMainSize() float64 {
	return fi.hypothetical + fi.mainBorderPadding() + fi.mainMarginSum()
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
	// Note: RTL direction is handled automatically by the fragment builder's
	// logical→physical coordinate conversion (ToPhysicalOffset). We do NOT
	// need to flip reverseMain for RTL here; InlineOffset=0 already maps to
	// the physical right side (main-start) in RTL containers.
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

	// If the parent has fixed the block-size via constraint space (e.g. nested flex stretch),
	// use it as the definite height.
	if !hasExplicitBlock && fla.space.IsFixedBlockSize && !fla.space.IsFixedBlockSizeIndefinite {
		fixedBS := fla.space.AvailableSize.BlockSize - geom.BlockBorderPadding()
		if fixedBS < 0 {
			fixedBS = 0
		}
		explicitBlockSize = fixedBS
		hasExplicitBlock = true
	}

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
	allItems := fla.collectItems(wdm, contentInlineSize, containerMainSize, hasDefiniteMain, containerCrossSize, hasDefiniteCross, isRow)

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
	// alignItems is needed here to determine crossIsFixed per item for column flex.
	alignItems := fla.getAlignItems()
	for _, line := range lines {
		lineCrossMax := 0.0
		for _, item := range line.items {
			// For column flex: stretch items (without auto cross margins) lay out at the container
			// inline-size (crossIsFixed=true). Non-stretch items and stretch items with auto
			// cross margins shrink-to-fit their content (crossIsFixed=false).
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			isStretch := selfAlign == "stretch" && !item.crossAutoStart && !item.crossAutoEnd
			crossIsFixed := !isRow && isStretch
			cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
				item.resolvedMain, Indefinite, crossIsFixed)
			result := layoutElement(fla.ctx, item.node, cs)
			item.fragment = result.Fragment
			item.baseline = result.Baseline
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

	// §9.8 — Two-pass layout: now that line cross-sizes are known, re-layout
	// non-stretch items that have percentage cross-sizes or aspect-ratio.
	// This gives them access to the definite cross-size for % resolution.
	for _, line := range lines {
		lineCrossMax := 0.0
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			if selfAlign == "stretch" {
				// Stretch items are handled in the stretch pass below.
				if item.crossSize > lineCrossMax {
					lineCrossMax = item.crossSize
				}
				continue
			}
			// Check if item has percentage cross-size or aspect-ratio that can now be resolved.
			needsRelayout := false
			if isRow {
				if _, ok := item.style.GetPercentage("height"); ok {
					needsRelayout = true
				}
			} else {
				if _, ok := item.style.GetPercentage("width"); ok {
					needsRelayout = true
				}
			}
			// Aspect-ratio: if main-size was determined by aspect-ratio in first pass,
			// re-layout with definite cross-size so aspect-ratio items get correct cross-size.
			if ar := item.style.GetAspectRatio(); ar.IsSet {
				needsRelayout = true
			}
			if needsRelayout {
				var crossBP2 float64
				if isRow {
					crossBP2 = item.geom.BlockBorderPadding()
				} else {
					crossBP2 = item.geom.InlineBorderPadding()
				}
				crossContent2 := line.crossSize - item.crossMarginSum() - crossBP2
				if crossContent2 < 0 {
					crossContent2 = 0
				}
				cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
					item.resolvedMain, crossContent2, false)
				result := layoutElement(fla.ctx, item.node, cs)
				item.fragment = result.Fragment
				lf := NewLogicalFragment(wdm, item.fragment)
				if isRow {
					item.crossSize = lf.BlockSize()
				} else {
					item.crossSize = lf.InlineSize()
				}
			}
			if item.crossSize > lineCrossMax {
				lineCrossMax = item.crossSize
			}
		}
		// Don't shrink the line below its first-pass size.
		if lineCrossMax > line.crossSize {
			line.crossSize = lineCrossMax
		}
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

	// §9.4 — Stretch items to line cross-size (align-self: stretch).
	// Must happen AFTER align-content so multi-line containers use the final
	// (possibly grown by align-content:stretch) line cross-sizes.
	// For single-line containers, lines[0].crossSize was set to containerCrossSize
	// in §9.4 step 8, which is unchanged by align-content for single-line.
	fla.stretchFlexItems(lines, alignItems, wdm, contentInlineSize, isRow)

	// §9.8 — Main axis alignment (justify-content) and item positioning.
	justifyContent := fla.getJustifyContent()

	// §9.9 — Cross axis alignment per item (align-self).
	for lineIdx, line := range lines {
		// §8.1: Auto margins in the main axis absorb free space before justify-content.
		// Count auto margin slots and available free space.
		mainAutoCount := 0
		for _, item := range line.items {
			if item.mainAutoStart {
				mainAutoCount++
			}
			if item.mainAutoEnd {
				mainAutoCount++
			}
		}
		if mainAutoCount > 0 {
			// Compute free space for this line (outer sizes include border-padding + margins).
			usedMain := mainGap * float64(len(line.items)-1)
			for _, item := range line.items {
				usedMain += item.resolvedMain + item.mainBorderPadding() + item.mainMarginSum()
			}
			lineFreeSpace := containerMainSize - usedMain
			if lineFreeSpace > 0 {
				autoMarginVal := lineFreeSpace / float64(mainAutoCount)
				// Add auto margin values to the appropriate logical margin edges.
				for _, item := range line.items {
					if item.mainAutoStart {
						if item.isRow {
							item.margins.InlineStart += autoMarginVal
						} else {
							item.margins.BlockStart += autoMarginVal
						}
					}
					if item.mainAutoEnd {
						if item.isRow {
							item.margins.InlineEnd += autoMarginVal
						} else {
							item.margins.BlockEnd += autoMarginVal
						}
					}
				}
				// With auto margins consuming all free space, justify-content has no effect.
				justifyContent = "flex-start"
			}
		}

		// Compute main offsets using justify-content.
		computeItemMainOffsets(line.items, containerMainSize, justifyContent, reverseMain, mainGap)

		crossStart := lineOffsets[lineIdx]

		// §9.9 baseline alignment: find the shared baseline for this line.
		// The shared baseline = max(item.crossMarginStart + item.baseline) over all baseline items.
		sharedBaseline := 0.0
		hasBaselineItem := false
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			if selfAlign == "baseline" && item.baseline > 0 {
				b := item.crossMarginStart() + item.baseline
				if b > sharedBaseline {
					sharedBaseline = b
				}
				hasBaselineItem = true
			}
		}

		// Align items in this line.
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)

			// §8.1: Auto margins in the cross axis override align-self.
			// crossFreeSpace = line cross-size minus item's outer cross-size (border-box + margins).
			crossFreeSpace := line.crossSize - item.crossSize - item.crossMarginSum()
			if crossFreeSpace > 0 && (item.crossAutoStart || item.crossAutoEnd) {
				if item.crossAutoStart && item.crossAutoEnd {
					// Both auto: center the item.
					// crossOffset is relative to container, builder adds crossMarginStart.
					item.crossOffset = crossStart + crossFreeSpace/2
				} else if item.crossAutoStart {
					// Auto start only: push to end.
					item.crossOffset = crossStart + crossFreeSpace
				} else {
					// Auto end only: stays at start.
					item.crossOffset = crossStart
				}
				continue
			}

			// crossFreeSpace = remaining space after item's outer cross-size.
			// This may be negative when the item is larger than the line (e.g. due to
			// overflow). Do NOT clamp to 0: flex-end and center use the raw value to
			// position items partially outside the line (overflow:hidden clips them).
			crossFreeForAlign := line.crossSize - item.crossSize - item.crossMarginSum()
			// crossOffset stores the position BEFORE crossMarginStart is added by the builder.
			var itemCrossOffset float64
			switch selfAlign {
			case "flex-end", "end":
				itemCrossOffset = crossStart + crossFreeForAlign
			case "center":
				itemCrossOffset = crossStart + crossFreeForAlign/2
			case "baseline":
				if hasBaselineItem && item.baseline > 0 {
					// Align so that item.baseline aligns with sharedBaseline.
					// crossOffset is position of item's margin-box start.
					// item.crossMarginStart() + item.baseline should equal crossStart + sharedBaseline.
					itemCrossOffset = crossStart + sharedBaseline - item.crossMarginStart() - item.baseline
				} else {
					itemCrossOffset = crossStart
				}
			default: // flex-start, stretch
				itemCrossOffset = crossStart
			}
			item.crossOffset = itemCrossOffset
		}
		_ = hasBaselineItem

		// Reset justify-content for next line (may have been overridden by auto margins).
		justifyContent = fla.getJustifyContent()
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
					lineTot += item.outerMainSize()
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
	containerCrossSize float64,
	hasDefiniteCross bool,
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

		// Constraint space for computing intrinsic sizes (§4.5, min/max).
		itemSizingSpace := ConstraintSpace{
			AvailableSize:            LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
			PercentageResolutionSize: LogicalSize{InlineSize: contentInlineSize},
			WritingDirection:         childWDM,
			IsNewFormattingContext:   true,
			IsInsideFlexibleBox:      true,
		}

		// Compute flex-basis and hypothetical main size.
		flexBasis := fla.resolveFlexBasis(child, childStyle, childWDM, childGeom, wdm,
			contentInlineSize, containerMainSize, hasDefiniteMain, containerCrossSize, hasDefiniteCross, isRow)

		// Compute §4.5 effective minimum main size.
		minMainSize := fla.flexItemMinMain(child, childStyle, childWDM, childGeom,
			itemSizingSpace, flexBasis, isRow)

		// Clamp flex-basis by min/max main size (using §4.5 min).
		hyp := fla.clampMainSizeWithMin(flexBasis, minMainSize, childStyle, childWDM, childGeom,
			itemSizingSpace, isRow)

		// Compute CSS max main size for §9.7 freeze loop.
		maxMainSize := Indefinite
		if isRow {
			if max, ok := ResolveMaxInlineSize(childStyle, childWDM, itemSizingSpace, childGeom); ok {
				maxMainSize = max
			}
		} else {
			if max, ok := ResolveMaxBlockSize(childStyle, childWDM, itemSizingSpace, childGeom); ok {
				maxMainSize = max
			}
		}

		// Detect auto margins for §8.1 alignment.
		mainAS, mainAE, crossAS, crossAE := getItemAutoMargins(childStyle, childWDM, isRow)

		item := &flexItem{
			node:           child,
			style:          childStyle,
			wdm:            childWDM,
			geom:           childGeom,
			margins:        childMargins,
			flexBasis:      flexBasis,
			hypothetical:   hyp,
			flexGrow:       flexGrow,
			flexShrink:     flexShrink,
			order:          order,
			isRow:          isRow,
			minMain:        minMainSize,
			maxMain:        maxMainSize,
			mainAutoStart:  mainAS,
			mainAutoEnd:    mainAE,
			crossAutoStart: crossAS,
			crossAutoEnd:   crossAE,
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
	containerCrossSize float64,
	hasDefiniteCross bool,
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
			// §9.2: If no explicit main size but aspect-ratio is set and cross-size is definite,
			// transfer cross-size through the ratio to get the flex-basis.
			if ar := style.GetAspectRatio(); ar.IsSet && hasDefiniteCross && ar.Height > 0 {
				crossContent := containerCrossSize - childGeom.BlockBorderPadding() - resolveItemCrossMargins(style, childWDM, contentInlineSize, true)
				if crossContent < 0 {
					crossContent = 0
				}
				return crossContent * ar.Width / ar.Height
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
			// Column: if no explicit block-size but aspect-ratio is set and cross-size (inline) is definite,
			// derive main-size (block) from cross-size / ratio.
			if ar := style.GetAspectRatio(); ar.IsSet && hasDefiniteCross && ar.Width > 0 {
				crossContent := containerCrossSize - childGeom.InlineBorderPadding() - resolveItemCrossMargins(style, childWDM, contentInlineSize, false)
				if crossContent < 0 {
					crossContent = 0
				}
				return crossContent * ar.Height / ar.Width
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

// clampMainSizeWithMin clamps the flex-basis using a precomputed minimum (§4.5)
// and the CSS max constraint.
func (fla *FlexLayoutAlgorithm) clampMainSizeWithMin(
	basis float64,
	minMain float64,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	space ConstraintSpace,
	isRow bool,
) float64 {
	result := basis
	if result < minMain {
		result = minMain
	}
	if isRow {
		if max, ok := ResolveMaxInlineSize(style, childWDM, space, childGeom); ok {
			if result > max {
				result = max
			}
		}
	} else {
		if max, ok := ResolveMaxBlockSize(style, childWDM, space, childGeom); ok {
			if result > max {
				result = max
			}
		}
	}
	return result
}

// resolveItemCrossMargins returns the total cross-axis margin sum for a flex item.
// isRow=true: cross axis is block, sum = top+bottom margins.
func resolveItemCrossMargins(style *css.Style, wdm WritingDirectionMode, containingInlineSize float64, isRow bool) float64 {
	margins := ResolveMargins(style, wdm, containingInlineSize)
	if isRow {
		return margins.BlockSum()
	}
	return margins.InlineSum()
}

// clampMainSize clamps the flex-basis by min/max main size constraints.
// Kept for backward compatibility; new code uses clampMainSizeWithMin.
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
	parentSpace := ConstraintSpace{
		AvailableSize:            LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
		PercentageResolutionSize: LogicalSize{InlineSize: contentInlineSize},
		WritingDirection:         childWDM,
	}
	minMain := fla.flexItemMinMain(child, style, childWDM, childGeom, parentSpace, basis, isRow)
	return fla.clampMainSizeWithMin(basis, minMain, style, childWDM, childGeom, parentSpace, isRow)
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
		itemSize := item.outerHypotheticalMainSize()
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
	// Free space = container content-box minus the outer hypothetical sizes of all items.
	// Outer size = content-box + border-padding + margins.
	usedSpace := mainGap * float64(len(items)-1)
	if len(items) == 0 {
		usedSpace = 0
	}
	for _, item := range items {
		usedSpace += item.outerHypotheticalMainSize()
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
			freeSpace -= item.resolvedMain + item.mainBorderPadding() + item.mainMarginSum()
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

		// §9.7: Freeze items that hit their CSS min or max main size.
		frozenAny := false
		for _, item := range items {
			if item.frozen {
				continue
			}
			if growing {
				// Freeze at max if we've grown past it.
				if item.maxMain != Indefinite && item.resolvedMain > item.maxMain {
					item.resolvedMain = item.maxMain
					item.frozen = true
					frozenAny = true
				}
			} else {
				// Freeze at min if we've shrunk past it.
				if item.resolvedMain < item.minMain {
					item.resolvedMain = item.minMain
					item.frozen = true
					frozenAny = true
				}
			}
		}
		if !frozenAny {
			break
		}
	}

	// Final clamp: no item can go below its effective minimum.
	for _, item := range items {
		if item.resolvedMain < item.minMain {
			item.resolvedMain = item.minMain
		}
		if item.resolvedMain < 0 {
			item.resolvedMain = 0
		}
	}
}

// buildItemConstraintSpace builds the constraint space for laying out a flex item
// at a given main size and cross size.
// crossIsFixed: for column flex (isRow=false), whether the inline-size is fixed to crossSize.
// Pass false for the initial/non-stretch passes, true for the stretch relayout pass.
func (fla *FlexLayoutAlgorithm) buildItemConstraintSpace(
	item *flexItem,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	isRow bool,
	mainSize float64,
	crossSize float64, // Indefinite if not known
	crossIsFixed bool, // only used when isRow=false
) ConstraintSpace {
	childWDM := item.wdm

	// Flex items always establish a new formatting context.
	b := NewConstraintSpaceBuilder(parentWDM, childWDM, true)

	// Mark this child as a flex item so inner layout can use flex-specific sizing rules.
	b.SetIsInsideFlexibleBox(true)

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
		// §9.8: Use the actual cross-size for percentage block-size resolution when definite.
		// When crossSize is Indefinite (first pass), use 0 so children treat % block-sizes as auto.
		pctBlockSize := 0.0
		if crossSize != Indefinite {
			pctBlockSize = crossSize
		}
		b.SetPercentageResolutionSize(LogicalSize{
			InlineSize: mainSize,
			BlockSize:  pctBlockSize,
		})
		b.SetIsFixedInlineSize(true)
		if crossSize != Indefinite {
			b.SetIsFixedBlockSize(true)
		}
	} else {
		// main = block, cross = inline.
		// When crossIsFixed (stretch relayout), the item must fill exactly crossSize.
		// Otherwise (initial pass, non-stretch), the item sizes to its content.
		crossInlineContent := contentInlineSize
		if crossSize != Indefinite {
			crossInlineContent = crossSize
		}
		avail := LogicalSize{
			InlineSize: crossInlineContent + item.geom.InlineBorderPadding(),
			BlockSize:  Indefinite,
		}
		// Only constrain the block-size when mainSize is a meaningful positive value.
		// When mainSize=0, use intrinsic sizing so content (e.g. fixed-height children)
		// can paint their full height via overflow:visible. This matches Blink's behavior
		// for column flex items with flex-basis:0 and fixed-height children.
		if mainSize > 0 {
			avail.BlockSize = mainSize + item.geom.BlockBorderPadding()
		}
		b.SetAvailableSize(avail)
		b.SetPercentageResolutionSize(LogicalSize{
			InlineSize: crossInlineContent,
			BlockSize:  mainSize,
		})
		b.SetIsFixedInlineSize(crossIsFixed)
		if mainSize > 0 {
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

	// Compute total outer item sizes (content + border-padding + margins).
	totalItemSize := mainGap * float64(len(items)-1)
	for _, item := range items {
		totalItemSize += item.outerMainSize()
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
			cursor -= item.outerMainSize()
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
			cursor += item.outerMainSize()
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

// stripOverflowKeyword removes the safe/unsafe overflow alignment keywords.
// CSS Flexbox Level 1 §8: "safe" and "unsafe" precede the alignment keyword.
func stripOverflowKeyword(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "safe ") {
		v = strings.TrimPrefix(v, "safe ")
	} else if strings.HasPrefix(v, "unsafe ") {
		v = strings.TrimPrefix(v, "unsafe ")
	}
	return strings.TrimSpace(v)
}

// getJustifyContent returns the justify-content value (default: "flex-start").
func (fla *FlexLayoutAlgorithm) getJustifyContent() string {
	if v, ok := fla.style.Get("justify-content"); ok {
		v = stripOverflowKeyword(v)
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
		v = stripOverflowKeyword(v)
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
		v = stripOverflowKeyword(v)
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

// getItemAutoMargins returns which logical margin edges of a flex item are "auto",
// mapped to main-axis (start/end) and cross-axis (start/end).
//
// Uses the same physical→logical mapping as ToLogicalEdges in writing_mode_converter.go,
// applied to the boolean Auto flags of each physical margin property.
func getItemAutoMargins(style *css.Style, wdm WritingDirectionMode, isRow bool) (mainAutoStart, mainAutoEnd, crossAutoStart, crossAutoEnd bool) {
	m := style.GetMargin()

	// Step 1: Map physical auto flags to logical (inline-start, inline-end, block-start, block-end).
	// This mirrors ToLogicalEdges exactly.
	var iAS, iAE, bAS, bAE bool
	switch wdm.WM {
	case WritingModeHorizontalTB:
		iAS, iAE = m.AutoLeft, m.AutoRight
		bAS, bAE = m.AutoTop, m.AutoBottom
	case WritingModeVerticalRL, WritingModeSidewaysRL:
		// inline axis: top→bottom; block axis: right→left
		iAS, iAE = m.AutoTop, m.AutoBottom
		bAS, bAE = m.AutoRight, m.AutoLeft
	case WritingModeVerticalLR:
		// inline axis: top→bottom; block axis: left→right
		iAS, iAE = m.AutoTop, m.AutoBottom
		bAS, bAE = m.AutoLeft, m.AutoRight
	case WritingModeSidewaysLR:
		// inline axis: bottom→top (reversed); block axis: left→right
		iAS, iAE = m.AutoBottom, m.AutoTop
		bAS, bAE = m.AutoLeft, m.AutoRight
	}

	// Step 2: Apply RTL direction — swaps inline-start and inline-end.
	if wdm.Dir == DirectionRTL {
		iAS, iAE = iAE, iAS
	}

	// Step 3: Map logical edges to main/cross axis based on flex direction.
	if isRow {
		// main axis = inline, cross axis = block
		return iAS, iAE, bAS, bAE
	}
	// main axis = block, cross axis = inline
	return bAS, bAE, iAS, iAE
}

// flexItemMinMain returns the effective minimum main size for a flex item.
// §4.5: For min-width/min-height:auto (the initial value for flex items), returns
// min(min-content-size, flex-basis) — the "automatic minimum size".
// Returns the explicit CSS min value if explicitly set to a non-auto value.
func (fla *FlexLayoutAlgorithm) flexItemMinMain(
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	space ConstraintSpace,
	flexBasis float64,
	isRow bool,
) float64 {
	// Check if min-size is explicitly set (non-auto).
	if isRow {
		if v, ok := style.Get("min-width"); ok && v != "" && v != "auto" {
			return ResolveMinInlineSize(style, childWDM, space, childGeom)
		}
		if v, ok := style.Get("min-height"); ok && v != "" && v != "auto" && childWDM.IsVertical() {
			return ResolveMinInlineSize(style, childWDM, space, childGeom)
		}
	} else {
		if v, ok := style.Get("min-height"); ok && v != "" && v != "auto" {
			return ResolveMinBlockSize(style, childWDM, space, childGeom)
		}
		if v, ok := style.Get("min-width"); ok && v != "" && v != "auto" && childWDM.IsVertical() {
			return ResolveMinBlockSize(style, childWDM, space, childGeom)
		}
	}

	// §4.5: min-size is auto (default). The automatic minimum size is
	// min(min-content-size, flex-basis). Only applies when overflow is visible.
	overflow := "visible"
	if v, ok := style.Get("overflow"); ok {
		overflow = strings.TrimSpace(v)
	}
	if overflow != "visible" {
		return 0
	}

	// Compute min-content size in the main axis.
	minContentSpace := ConstraintSpace{
		AvailableSize:    LogicalSize{InlineSize: 0, BlockSize: Indefinite},
		WritingDirection: childWDM,
		IsNewFormattingContext: true,
	}
	var minContentMain float64
	if isRow {
		mm := ComputeMinMaxSizes(fla.ctx, child, minContentSpace)
		minContentMain = mm.MinContent
	} else {
		// Column: min-content block-size = lay out at zero inline size, take block result.
		result := layoutElement(fla.ctx, child, minContentSpace)
		lf := NewLogicalFragment(childWDM, result.Fragment)
		minContentMain = lf.BlockSize() - childGeom.BlockBorderPadding()
	}

	// Automatic minimum = min(min-content, flex-basis).
	autoMin := minContentMain
	if flexBasis >= 0 && flexBasis < autoMin {
		autoMin = flexBasis
	}
	if autoMin < 0 {
		autoMin = 0
	}
	return autoMin
}

// stretchFlexItems performs align-self: stretch for all items across all lines.
// Must be called AFTER align-content has finalized line cross-sizes, so that
// multi-line containers stretched by align-content:stretch get the correct target.
func (fla *FlexLayoutAlgorithm) stretchFlexItems(
	lines []*flexLine,
	alignItems string,
	wdm WritingDirectionMode,
	contentInlineSize float64,
	isRow bool,
) {
	for _, line := range lines {
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			if selfAlign != "stretch" {
				continue
			}
			// CSS Flexbox §9.5.1: If the item has any auto margins in the cross axis,
			// the auto margins absorb the free space and the item is NOT stretched.
			if item.crossAutoStart || item.crossAutoEnd {
				continue
			}
			// Stretch item's border-box to the line cross-size minus cross margins.
			stretchBorderBox := line.crossSize - item.crossMarginSum()
			if stretchBorderBox < 0 {
				stretchBorderBox = 0
			}
			var crossBP float64
			if isRow {
				crossBP = item.geom.BlockBorderPadding()
			} else {
				crossBP = item.geom.InlineBorderPadding()
			}
			stretchContent := stretchBorderBox - crossBP
			if stretchContent < 0 {
				stretchContent = 0
			}
			// Clamp to item's own min/max cross size (content-box).
			if isRow {
				minBlock := ResolveMinBlockSize(item.style, item.wdm, fla.space, item.geom)
				if stretchContent < minBlock {
					stretchContent = minBlock
				}
				if maxBlock, hasMax := ResolveMaxBlockSize(item.style, item.wdm, fla.space, item.geom); hasMax {
					if stretchContent > maxBlock {
						stretchContent = maxBlock
					}
				}
			} else {
				minInlineItem := ResolveMinInlineSize(item.style, item.wdm, fla.space, item.geom)
				if stretchContent < minInlineItem {
					stretchContent = minInlineItem
				}
				if maxInlineItem, hasMax := ResolveMaxInlineSize(item.style, item.wdm, fla.space, item.geom); hasMax {
					if stretchContent > maxInlineItem {
						stretchContent = maxInlineItem
					}
				}
			}
			newBorderBox := stretchContent + crossBP
			if newBorderBox != item.crossSize {
				cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
					item.resolvedMain, stretchContent, true)
				result := layoutElement(fla.ctx, item.node, cs)
				item.fragment = result.Fragment
				lf := NewLogicalFragment(wdm, item.fragment)
				if isRow {
					item.crossSize = lf.BlockSize()
				} else {
					item.crossSize = lf.InlineSize()
				}
			}
		}
	}
}
