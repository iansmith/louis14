package layout

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"louis14/pkg/css"
)

const flexDebug = false

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

// computeMainIsItemInline determines whether the flex main axis corresponds to the
// item's logical inline axis.
//
// The flex main axis direction (physical) depends on the container's writing mode
// and the flex direction:
//   - HTB container, row flex  → main = physical horizontal
//   - HTB container, col flex  → main = physical vertical
//   - VRL container, row flex  → main = physical vertical
//   - VRL container, col flex  → main = physical horizontal
//
// The item's inline axis is horizontal for HTB items and vertical for VRL/VLR items.
// mainIsItemInline=true means the flex main axis == the item's inline direction
// (normal/non-orthogonal case). mainIsItemInline=false means the main axis == the
// item's block direction (orthogonal case — axes are swapped in the item's frame).
//
// All flexItem dimension helpers use this flag directly to choose inline vs block.
//
// Examples:
//   HTB container, row, HTB item → mainIsHoriz=true, itemInlineIsHoriz=true  → mainIsItemInline=true
//   HTB container, row, VRL item → mainIsHoriz=true, itemInlineIsHoriz=false → mainIsItemInline=false
//   HTB container, col, HTB item → mainIsHoriz=false, itemInlineIsHoriz=true → mainIsItemInline=false
//   VRL container, row, HTB item → mainIsHoriz=false, itemInlineIsHoriz=true → mainIsItemInline=false
//   VRL container, row, VRL item → mainIsHoriz=false, itemInlineIsHoriz=false→ mainIsItemInline=true
func computeMainIsItemInline(containerWDM WritingDirectionMode, itemWDM WritingDirectionMode, isRow bool) bool {
	mainIsHorizontal := !containerWDM.IsVertical() == isRow
	itemInlineIsHorizontal := !itemWDM.IsVertical()
	return mainIsHorizontal == itemInlineIsHorizontal
}

// selfStartIsCrossStart returns true when align-self: self-start should
// resolve to the same side as flex-start (cross-start). Per CSS Alignment §4.1,
// self-start/self-end use the *item's own* writing direction rather than the
// container's.
//
// The algorithm compares two physical sides:
//  1. The container's cross-start (block-start for row, inline-start for column)
//  2. The item's "start" on the same physical axis. If the cross axis is
//     the item's block axis, this is the item's block-start; if it is the
//     item's inline axis, this is the item's inline-start.
//
// When these physical sides match, self-start ≡ cross-start → return true.
func selfStartIsCrossStart(containerWDM, itemWDM WritingDirectionMode, isRow bool) bool {
	// Determine the container's physical cross-start side.
	var containerCrossStart physicalSide
	if isRow {
		// Row flex: cross = block axis.
		containerCrossStart = physicalBlockStart(containerWDM)
	} else {
		// Column flex: cross = inline axis.
		containerCrossStart = physicalInlineStart(containerWDM)
	}

	// Determine whether the cross axis (physical) aligns with the item's
	// block axis or inline axis.
	crossIsVertical := containerCrossStart == sideTop || containerCrossStart == sideBottom
	itemBlockIsVertical := itemWDM.IsHorizontal() // HTB: block is vertical

	var itemSelfStart physicalSide
	if crossIsVertical == itemBlockIsVertical {
		// Cross axis corresponds to item's block axis.
		itemSelfStart = physicalBlockStart(itemWDM)
	} else {
		// Cross axis corresponds to item's inline axis.
		itemSelfStart = physicalInlineStart(itemWDM)
	}

	return containerCrossStart == itemSelfStart
}

// physicalSide is a simple enum for the four physical sides.
type physicalSide int

const (
	sideTop physicalSide = iota
	sideRight
	sideBottom
	sideLeft
)

func physicalBlockStart(wdm WritingDirectionMode) physicalSide {
	switch wdm.WM {
	case WritingModeVerticalRL, WritingModeSidewaysRL:
		return sideRight
	case WritingModeVerticalLR, WritingModeSidewaysLR:
		return sideLeft
	default: // horizontal-tb
		return sideTop
	}
}

func physicalInlineStart(wdm WritingDirectionMode) physicalSide {
	if wdm.IsHorizontal() {
		if wdm.Dir == DirectionRTL {
			return sideRight
		}
		return sideLeft
	}
	// Vertical/sideways: inline axis is vertical.
	// sideways-lr has inline direction bottom-to-top, so inline-start = bottom.
	if wdm.WM == WritingModeSidewaysLR {
		if wdm.Dir == DirectionRTL {
			return sideTop
		}
		return sideBottom
	}
	// vertical-rl, vertical-lr, sideways-rl: inline direction top-to-bottom.
	if wdm.Dir == DirectionRTL {
		return sideBottom
	}
	return sideTop
}

// flexItem holds layout information for a single flex item.
type flexItem struct {
	node             *LayoutInputNode
	style            *css.Style
	wdm              WritingDirectionMode
	geom             FragmentGeometry
	margins          LogicalEdges
	mainIsItemInline bool    // true when flex main axis == item's inline axis (false for orthogonal items)
	flexBasis        float64 // resolved flex-basis (content-box in main axis)
	hypothetical     float64 // hypothetical main size (clamped flex-basis)
	resolvedMain     float64 // final main size after flex grow/shrink
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
	// Per CSS Align §4.4, items with no natural baseline get a synthesized first
	// baseline at the block-start edge (0) and last baseline at block-end (crossSize).
	baseline     float64
	hasBaseline  bool // true if the layout produced a real baseline
	lastBaseline float64

	// propagatedOOF holds OOF candidates that the item's layout couldn't resolve
	// because the item is not a positioned container.
	propagatedOOF []OutOfFlowCandidate

	// collapsed is true when the item has visibility:collapse (§12).
	// Collapsed items contribute to the line's cross-size but occupy 0 main-size
	// and are not rendered.
	collapsed bool
}

// mainMarginSum returns the total margin in the flex main axis.
func (fi *flexItem) mainMarginSum() float64 {
	if fi.isRow {
		return fi.margins.InlineSum()
	}
	return fi.margins.BlockSum()
}

// mainMarginStart returns the margin-start in the flex main axis.
// Margins are resolved in the container's WDM, so InlineStart/End align with
// the container's inline direction (correct for row flex) and BlockStart/End
// align with the container's block direction (correct for column flex).
func (fi *flexItem) mainMarginStart() float64 {
	if fi.isRow {
		return fi.margins.InlineStart
	}
	return fi.margins.BlockStart
}

// crossMarginStart returns the margin-start in the flex cross axis.
func (fi *flexItem) crossMarginStart() float64 {
	if fi.isRow {
		return fi.margins.BlockStart
	}
	return fi.margins.InlineStart
}

// crossMarginSum returns the total margin in the flex cross axis.
func (fi *flexItem) crossMarginSum() float64 {
	if fi.isRow {
		return fi.margins.BlockSum()
	}
	return fi.margins.InlineSum()
}

// crossMarginEnd returns the margin-end in the flex cross axis.
func (fi *flexItem) crossMarginEnd() float64 {
	if fi.isRow {
		return fi.margins.BlockEnd
	}
	return fi.margins.InlineEnd
}

// mainMarginEnd returns the margin-end in the flex main axis.
func (fi *flexItem) mainMarginEnd() float64 {
	if fi.isRow {
		return fi.margins.InlineEnd
	}
	return fi.margins.BlockEnd
}

// mainBorderPadding returns the border+padding sum in the flex main axis.
// mainBorderPadding returns the border+padding sum in the flex main axis.
// Expressed in parent logical coordinates (inline for row flex, block for column flex).
// The ConstraintSpaceBuilder swaps axes automatically for orthogonal children,
// so these values should always be in the PARENT's coordinate frame.
func (fi *flexItem) mainBorderPadding() float64 {
	if fi.mainIsItemInline {
		return fi.geom.InlineBorderPadding()
	}
	return fi.geom.BlockBorderPadding()
}

// crossBorderPadding returns the border+padding sum in the flex cross axis.
// Expressed in parent logical coordinates (block for row flex, inline for column flex).
func (fi *flexItem) crossBorderPadding() float64 {
	if fi.mainIsItemInline {
		return fi.geom.BlockBorderPadding()
	}
	return fi.geom.InlineBorderPadding()
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
	geom := CalculateInitialFragmentGeometry(fla.ctx, fla.node, fla.style, wdm, fla.space)
	builder := NewBoxFragmentBuilder(wdm)
	builder.SetLayoutNode(fla.node)

	// §9.1 — Determine flex direction and whether main axis == inline axis.
	flexDir := fla.getFlexDirection()
	isRow := flexDir == "row" || flexDir == "row-reverse"
	if flexDebug {
		fmt.Fprintf(os.Stderr, "FLEX-DBG === container wdm=%v flexDir=%s ===\n", wdm, flexDir)
	}
	reverseMain := flexDir == "row-reverse" || flexDir == "column-reverse"
	// Note: RTL direction is handled automatically by the fragment builder's
	// logical→physical coordinate conversion (ToPhysicalOffset). We do NOT
	// need to flip reverseMain for RTL here; InlineOffset=0 already maps to
	// the physical right side (main-start) in RTL containers.
	wrapMode := fla.getFlexWrap()
	reverseCross := wrapMode == "wrap-reverse"

	// §9.2 — Resolve container inline-size.
	contentInlineSize := geom.BorderBoxSize.InlineSize - geom.InlineBorderPadding()
	if contentInlineSize < 0 {
		contentInlineSize = 0
	}

	// Resolve container block-size.
	hasExplicitBlock := geom.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geom.BorderBoxSize.BlockSize - geom.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
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
	allItems := fla.collectItems(builder, wdm, contentInlineSize, containerMainSize, hasDefiniteMain, containerCrossSize, hasDefiniteCross, isRow)

	// §9.3 — Sort by order property.
	sortFlexItems(allItems)

	// §9.3 — Build flex lines.
	// For column flex with no explicit height but max-height set, use max-height
	// as the wrap boundary (items that exceed it wrap to the next column).
	wrapMainSize := containerMainSize
	hasWrapBoundary := hasDefiniteMain
	if !isRow && !hasDefiniteMain && wrapMode != "nowrap" {
		if maxBlock, hasMax := ResolveMaxBlockSize(fla.style, wdm, fla.space, geom); hasMax {
			wrapMainSize = maxBlock
			hasWrapBoundary = true
		}
	}
	lines := fla.buildFlexLines(allItems, wrapMode, wrapMainSize, hasWrapBoundary, mainGap)

	// §9.7 — Resolve flexible lengths for each line.
	// For wrapping column flex with max-height (no explicit height), use the
	// max-height-derived wrap boundary for flex resolution — items should
	// grow to fill the line within that boundary.
	resolveMainSize := containerMainSize
	resolveDefinite := hasDefiniteMain
	if hasWrapBoundary && !hasDefiniteMain && wrapMode != "nowrap" {
		resolveMainSize = wrapMainSize
		resolveDefinite = true
	}
	for _, line := range lines {
		fla.resolveFlexibleLengths(line, resolveMainSize, resolveDefinite, mainGap)
	}

	// §9.4 — Determine cross-size of items and lines.
	// Do a layout pass for each item at its resolved main size to get intrinsic cross size.
	// alignItems is needed here to determine crossIsFixed per item for column flex.
	alignItems := fla.getAlignItems()
	for _, line := range lines {
		lineCrossMax := 0.0
		maxAscent := 0.0
		maxDescent := 0.0
		hasBaselineItem := false
		maxLastAscent := 0.0
		maxLastDescent := 0.0
		hasLastBaselineItem := false
		for _, item := range line.items {
			// For column flex: stretch items (without auto cross margins) lay out at the container
			// inline-size (crossIsFixed=true). Non-stretch items and stretch items with auto
			// cross margins shrink-to-fit their content (crossIsFixed=false).
			// Exception: in wrapping column flex, the first pass must NOT fix the cross-size
			// because we don't know the line cross-size yet. Items should use fit-content
			// width. The stretch pass (stretchFlexItems) will later stretch them to the
			// line cross-size.
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			isStretch := selfAlign == "stretch" && !item.crossAutoStart && !item.crossAutoEnd &&
				!fla.hasExplicitCrossSize(item.style, wdm, isRow)
			crossIsFixed := !isRow && isStretch && wrapMode == "nowrap"
			cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
				item.resolvedMain, Indefinite, crossIsFixed)
			result := layoutElement(fla.ctx, item.node, cs)
			item.fragment = result.Fragment
			item.baseline = result.Baseline
			item.hasBaseline = result.HasBaseline
			item.lastBaseline = result.LastBaseline
			item.propagatedOOF = result.PropagatedOOFCandidates
			lf := NewLogicalFragment(wdm, item.fragment)

			// For replaced elements with aspect ratios, the layout may have
			// enlarged the main-size due to cross min/max constraints transferring
			// through the aspect ratio (e.g., min-height on a row flex img).
			// Update resolvedMain to match the actual fragment.
			if item.node.DOMNode != nil && isReplacedElement(item.node.DOMNode) {
				var actualMain float64
				if isRow {
					actualMain = lf.InlineSize() - item.mainBorderPadding()
				} else {
					actualMain = lf.BlockSize() - item.mainBorderPadding()
				}
				if actualMain > item.resolvedMain {
					item.resolvedMain = actualMain
				}
			}

			var itemCross float64
			if isRow {
				itemCross = lf.BlockSize()
			} else {
				itemCross = lf.InlineSize()
			}
			item.crossSize = itemCross

			// Replaced elements: derive cross size from main size via aspect ratio,
			// but ONLY when the item does not have an explicit cross-size set in CSS.
			// When both width and height are explicit, the layout pass already uses
			// the explicit height; overriding with the aspect ratio would be wrong
			// (e.g., a 100x100 image with CSS width:10px height:20px should be 20px
			// tall, not 10px from the 1:1 intrinsic ratio).
			if item.node.DOMNode != nil && isReplacedElement(item.node.DOMNode) &&
				!fla.hasExplicitCrossSize(item.style, item.wdm, isRow) {
				info := GetIntrinsicSizingInfo(fla.ctx, item.node)
				if info.HasAspectRatio && info.AspectRatio > 0 {
					// Convert physical ratio (width/height) to logical (inline/block).
					logicalRatio := info.AspectRatio
					if item.wdm.IsVertical() {
						// Vertical WM: inline=height, block=width → logical = 1/physical.
						logicalRatio = 1.0 / info.AspectRatio
					}
					mainContent := item.resolvedMain
					var crossContent float64
					if item.mainIsItemInline {
						// Main = inline axis → cross = block axis.
						// logicalRatio = inline/block → block = inline/ratio.
						crossContent = mainContent / logicalRatio
						// Clamp by min/max block (cross) size.
						minCross := ResolveMinBlockSize(item.style, item.wdm, cs, item.geom)
						if crossContent < minCross {
							crossContent = minCross
						}
						if maxCross, hasMax := ResolveMaxBlockSize(item.style, item.wdm, cs, item.geom); hasMax && crossContent > maxCross {
							crossContent = maxCross
						}
					} else {
						// Main = block axis → cross = inline axis.
						// logicalRatio = inline/block → inline = block * ratio.
						crossContent = mainContent * logicalRatio
						// Clamp by min/max inline (cross) size.
						minCross := ResolveMinInlineSize(item.style, item.wdm, cs, item.geom)
						if crossContent < minCross {
							crossContent = minCross
						}
						if maxCross, hasMax := ResolveMaxInlineSize(item.style, item.wdm, cs, item.geom); hasMax && crossContent > maxCross {
							crossContent = maxCross
						}
					}
					item.crossSize = crossContent + item.crossBorderPadding()
				}
			}

			// §9.4: line cross-size computation.
			outerCross := item.crossSize + item.crossMarginSum()
			// Baseline participation requires the item's block axis to be parallel
			// to the flex container's cross axis (CSS Flexbox §9.4 step 8).
			// Row flex: both same orientation. Column flex: orthogonal writing modes only.
			baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
				(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
			if selfAlign == "baseline" && item.baseline > 0 && baselineParallel {
				// First baseline items: track ascent and descent separately.
				// Clamp baseline within the item's border-box to avoid
				// overflowing baselines expanding the flex line.
				bl := item.baseline
				if bl > item.crossSize {
					bl = item.crossSize
				}
				ascent := item.crossMarginStart() + bl
				descent := outerCross - ascent
				if ascent > maxAscent {
					maxAscent = ascent
				}
				if descent > maxDescent {
					maxDescent = descent
				}
				hasBaselineItem = true
			} else if selfAlign == "last baseline" && baselineParallel {
				// Last baseline items: track ascent (from top) and descent (from last baseline to bottom).
				lb := item.lastBaseline
				if lb <= 0 {
					lb = item.crossSize // fallback: bottom of border-box
				}
				lastAscent := item.crossMarginStart() + lb
				lastDescent := outerCross - lastAscent
				if lastAscent > maxLastAscent {
					maxLastAscent = lastAscent
				}
				if lastDescent > maxLastDescent {
					maxLastDescent = lastDescent
				}
				hasLastBaselineItem = true
			} else {
				if outerCross > lineCrossMax {
					lineCrossMax = outerCross
				}
			}
		}
		if hasBaselineItem {
			baselineCross := maxAscent + maxDescent
			if baselineCross > lineCrossMax {
				lineCrossMax = baselineCross
			}
		}
		if hasLastBaselineItem {
			lastBaselineCross := maxLastAscent + maxLastDescent
			if lastBaselineCross > lineCrossMax {
				lineCrossMax = lastBaselineCross
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
	// ALL non-stretch items with the definite line cross-size. This matches
	// Blink's GiveItemsFinalPositionAndSize which relayouts every item.
	// crossIsFixed=false ensures items size to content (not forced to line
	// cross-size), while avail.BlockSize and pctBlockSize are set to the
	// line cross-size for percentage resolution in descendants.
	// Stretch items are handled separately in stretchFlexItems below.
	for _, line := range lines {
		lineCrossMax := 0.0
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			if selfAlign == "stretch" && !item.crossAutoStart && !item.crossAutoEnd &&
				!fla.hasExplicitCrossSize(item.style, wdm, isRow) {
				// Stretch items without auto margins are handled in the stretch pass below.
				if item.crossSize > lineCrossMax {
					lineCrossMax = item.crossSize
				}
				continue
			}
			// Re-layout with the definite line cross-size for percentage resolution.
			var crossBP2 float64
			if item.mainIsItemInline {
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
			item.baseline = result.Baseline
			item.hasBaseline = result.HasBaseline
			item.lastBaseline = result.LastBaseline
			item.propagatedOOF = result.PropagatedOOFCandidates
			lf := NewLogicalFragment(wdm, item.fragment)
			if isRow {
				item.crossSize = lf.BlockSize()
			} else {
				item.crossSize = lf.InlineSize()
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

	// §9.4 step 8 (revisited): After min/max cross constraints, for single-line
	// containers the line cross-size must equal the container cross-size.
	// This handles both min-height (expanding the line) and max-height (clamping
	// the line down). Per Blink and the spec, the single flex line's cross-size
	// always tracks the container's resolved cross-size.
	if wrapMode == "nowrap" && len(lines) == 1 {
		lines[0].crossSize = containerCrossSize
	}

	// §9.6 — align-content: distribute lines within container cross-size.
	// Check for "safe" keyword before getAlignContent strips it.
	rawAlignContent := ""
	if v, ok := fla.style.Get("align-content"); ok {
		rawAlignContent = strings.TrimSpace(v)
	}
	alignContentSafe := strings.Contains(rawAlignContent, "safe")
	alignContent := fla.getAlignContent()
	var lineOffsets []float64
	if wrapMode == "nowrap" && len(lines) == 1 {
		// Single-line (nowrap) container: no multi-line alignment needed.
		// But still respect reverseCross for single line.
		if reverseCross && hasDefiniteCross {
			lineOffsets = []float64{containerCrossSize - lines[0].crossSize}
		} else {
			lineOffsets = []float64{0}
		}
		// §5.3 Safe alignment: if the line overflows past the start edge,
		// clamp to 0 so overflow goes toward the end edge.
		if alignContentSafe && lineOffsets[0] < 0 {
			lineOffsets[0] = 0
		}
	} else {
		lineOffsets = computeAlignContent(lines, containerCrossSize, totalLinesCross, alignContent, reverseCross, crossGap)
		// §5.3 Safe alignment: if any line overflows past the start edge,
		// clamp to 0 so overflow goes toward the end edge.
		if alignContentSafe {
			for i := range lineOffsets {
				if lineOffsets[i] < 0 {
					lineOffsets[i] = 0
				}
			}
		}
	}
	// §9.4 — Stretch items to line cross-size (align-self: stretch).
	// Must happen AFTER align-content so multi-line containers use the final
	// (possibly grown by align-content:stretch) line cross-sizes.
	// For single-line nowrap containers, lines[0].crossSize was already set to
	// containerCrossSize in §9.4 step 8.
	// For single-line wrapping containers with align-content:stretch, grow
	// the line cross-size now so that stretch items fill the container cross-size.
	// This matches Blink's behavior: single-line wrapping + stretch → line fills cross.
	if wrapMode != "nowrap" && len(lines) == 1 && hasDefiniteCross &&
		alignContent == "stretch" && containerCrossSize > lines[0].crossSize {
		lines[0].crossSize = containerCrossSize
	}
	fla.stretchFlexItems(lines, alignItems, wdm, contentInlineSize, isRow)

	// When the main axis is indefinite (auto-sized), compute the actual
	// container main size from item outer sizes, then apply min/max constraints.
	// This ensures justify-content sees the correct free space (e.g., min-height
	// on a column flex gives extra space for justify-content to distribute).
	if !hasDefiniteMain {
		for _, line := range lines {
			lineTotal := mainGap * float64(len(line.items)-1)
			for _, item := range line.items {
				lineTotal += item.outerMainSize()
			}
			if lineTotal > containerMainSize {
				containerMainSize = lineTotal
			}
		}
		// Apply min/max block constraints (only relevant for column flex where
		// main axis = block axis and the container has min-height/max-height).
		if !isRow {
			minBlock := ResolveMinBlockSize(fla.style, wdm, fla.space, geom)
			if containerMainSize < minBlock {
				containerMainSize = minBlock
			}
			if maxBlock, hasMax := ResolveMaxBlockSize(fla.style, wdm, fla.space, geom); hasMax {
				if containerMainSize > maxBlock {
					containerMainSize = maxBlock
				}
			}
		}
	}

	// §9.8 — Main axis alignment (justify-content) and item positioning.
	justifyContent := fla.getJustifyContent()
	justifyContentSafe := fla.isJustifyContentSafe()
	// Resolve physical/logical keywords (left/right/start/end) to flex-relative
	// equivalents (flex-start/flex-end).
	//
	// Step 1: Resolve left/right to start/end.
	// Per CSS Box Alignment §4.1:
	//   - When the main axis is the inline axis (isRow): left/right resolve
	//     based on the CSS direction property (LTR/RTL).
	//   - When the main axis is NOT the inline axis (!isRow): both left and
	//     right fall back to "start".
	if isRow {
		isLTR := wdm.Dir != DirectionRTL
		if justifyContent == "left" {
			if isLTR {
				justifyContent = "start"
			} else {
				justifyContent = "end"
			}
		} else if justifyContent == "right" {
			if isLTR {
				justifyContent = "end"
			} else {
				justifyContent = "start"
			}
		}
	} else {
		if justifyContent == "left" || justifyContent == "right" {
			justifyContent = "start"
		}
	}
	// Step 2: Resolve start/end to flex-start/flex-end.
	// "start" = main-axis start in the writing direction (inline-start for row,
	// block-start for column). In a reversed flex container, the writing-direction
	// start is the flex-end.
	if justifyContent == "start" {
		if reverseMain {
			justifyContent = "flex-end"
		} else {
			justifyContent = "flex-start"
		}
	} else if justifyContent == "end" {
		if reverseMain {
			justifyContent = "flex-start"
		} else {
			justifyContent = "flex-end"
		}
	}

	// Save the fully resolved justify-content so we can restore it after auto
	// margin overrides on individual lines.
	resolvedJustifyContent := justifyContent

	// §9.9 — Cross axis alignment per item (align-self).
	for lineIdx, line := range lines {
		// §8.1: Auto margins in the main axis absorb free space before justify-content.
		// Count auto margin slots and available free space.
		// §12: Collapsed items don't participate in auto margin distribution.
		mainAutoCount := 0
		for _, item := range line.items {
			if item.collapsed {
				continue
			}
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
		computeItemMainOffsets(line.items, containerMainSize, justifyContent, justifyContentSafe, reverseMain, mainGap)

		crossStart := lineOffsets[lineIdx]

		// §9.9 baseline alignment: find the shared baseline for this line.
		// First baseline: sharedBaseline = max(crossMarginStart + baseline) over baseline items.
		sharedBaseline := 0.0
		hasBaselineItem := false
		// Last baseline: sharedLastDescend = max(crossMarginEnd + (crossSize - lastBaseline)).
		sharedLastDescend := 0.0
		hasLastBaselineItem := false
		// Baseline synthesis at block-end of border-box is only correct for
		// horizontal writing modes. Vertical writing modes use the central
		// baseline (text-orientation dependent) which is not yet implemented.
		canSynthesize := isRow && !wdm.IsVertical()
		for _, item := range line.items {
			if item.collapsed {
				continue // §12: collapsed items don't participate in baseline positioning
			}
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			// Baseline participation requires parallel axes.
			baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
				(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
			if selfAlign == "baseline" && baselineParallel {
				bl := item.baseline
				if !item.hasBaseline && canSynthesize {
					// CSS Align §4.4: synthesize first baseline at block-start
					// of border-box for items with no natural baseline.
					bl = 0
				}
				if bl > 0 || item.hasBaseline {
					b := item.crossMarginStart() + bl
					if b > sharedBaseline {
						sharedBaseline = b
					}
					hasBaselineItem = true
				}
			}
			if selfAlign == "last baseline" && baselineParallel {
				lb := item.lastBaseline
				if lb <= 0 {
					lb = item.baseline
				}
				if !item.hasBaseline && lb <= 0 && canSynthesize {
					lb = item.crossSize // CSS Align §4.4: synthesize last baseline at block-end.
				}
				d := item.crossMarginEnd() + (item.crossSize - lb)
				if d > sharedLastDescend {
					sharedLastDescend = d
				}
				hasLastBaselineItem = true
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
			case "flex-end":
				itemCrossOffset = crossStart + crossFreeForAlign
			case "end":
				if reverseCross {
					itemCrossOffset = crossStart // acts like flex-start under wrap-reverse
				} else {
					itemCrossOffset = crossStart + crossFreeForAlign // acts like flex-end
				}
			case "start":
				if reverseCross {
					itemCrossOffset = crossStart + crossFreeForAlign // acts like flex-end under wrap-reverse
				} else {
					itemCrossOffset = crossStart // acts like flex-start
				}
			case "self-end":
				if selfStartIsCrossStart(wdm, item.wdm, isRow) {
					// Item's end maps to container's cross-end.
					itemCrossOffset = crossStart + crossFreeForAlign
				} else {
					// Item's end maps to container's cross-start.
					itemCrossOffset = crossStart
				}
			case "self-start":
				if selfStartIsCrossStart(wdm, item.wdm, isRow) {
					// Item's start maps to container's cross-start.
					itemCrossOffset = crossStart
				} else {
					// Item's start maps to container's cross-end.
					itemCrossOffset = crossStart + crossFreeForAlign
				}
			case "center":
				itemCrossOffset = crossStart + crossFreeForAlign/2
			case "baseline":
				if hasBaselineItem {
					bl := item.baseline
					if !item.hasBaseline && canSynthesize {
						bl = 0 // CSS Align §4.4: synthesize first baseline at block-start
					}
					if bl > 0 || item.hasBaseline || canSynthesize {
						itemCrossOffset = crossStart + sharedBaseline - item.crossMarginStart() - bl
					} else {
						itemCrossOffset = crossStart
					}
				} else {
					itemCrossOffset = crossStart
				}
			case "last baseline":
				if hasLastBaselineItem {
					bl := item.lastBaseline
					if bl <= 0 {
						bl = item.baseline
					}
					if !item.hasBaseline && bl <= 0 && canSynthesize {
						bl = item.crossSize // CSS Align §4.4: synthesize last baseline at block-end
					}
					belowBL := item.crossSize - bl
					itemCrossOffset = crossStart + line.crossSize - sharedLastDescend - belowBL - item.crossMarginEnd()
				} else {
					itemCrossOffset = crossStart + crossFreeForAlign
				}
			default: // flex-start, stretch
				itemCrossOffset = crossStart
			}
			item.crossOffset = itemCrossOffset
		}
		_ = hasBaselineItem
		_ = hasLastBaselineItem

		// Reset justify-content for next line (may have been overridden by auto margins).
		justifyContent = resolvedJustifyContent
	}

	// §9.9 — Add children to builder.
	// mainOffset and crossOffset are already the content-box positions
	// (margins accounted for by mainMarginStart/crossMarginStart).
	//
	// Items are added in physical left-to-right (top-to-bottom for column) order
	// so the painter renders correctly at sub-pixel boundaries. When two adjacent
	// items meet at a fractional-pixel edge, the right item's border (painted last)
	// correctly wins over the left item's background. This matches the reference
	// behaviour where items are in DOM≡visual order.
	//
	// Physical inline position depends on writing direction:
	//   LTR HTB: physX = inlineOffset
	//   RTL HTB: physX = containerInlineSize - inlineOffset - itemInlineSize
	isRTL := wdm.Dir == DirectionRTL
	for _, line := range lines {
		sorted := make([]*flexItem, len(line.items))
		copy(sorted, line.items)
		sort.SliceStable(sorted, func(i, j int) bool {
			if isRow {
				// Compute physical left edge for each item.
				physI := sorted[i].mainOffset
				physJ := sorted[j].mainOffset
				if isRTL {
					physI = contentInlineSize - sorted[i].mainOffset - sorted[i].fragment.Size.Width
					physJ = contentInlineSize - sorted[j].mainOffset - sorted[j].fragment.Size.Width
				}
				return physI < physJ
			}
			// Column: sort by physical top (blockOffset, unaffected by RTL).
			return sorted[i].mainOffset < sorted[j].mainOffset
		})
		for _, item := range sorted {
			var inlineOff, blockOff float64
			if isRow {
				inlineOff = item.mainOffset
				blockOff = item.crossOffset + item.crossMarginStart()
			} else {
				inlineOff = item.crossOffset + item.crossMarginStart()
				blockOff = item.mainOffset
			}
			if flexDebug {
				fmt.Fprintf(os.Stderr, "FLEX-DBG item wdm=%v mainOff=%.1f crossOff=%.1f crossMarginStart=%.1f crossSize=%.1f mainIsItemInline=%v fragW=%.1f fragH=%.1f inlineOff=%.1f blockOff=%.1f margins={IS=%.1f IE=%.1f BS=%.1f BE=%.1f}\n",
					item.wdm, item.mainOffset, item.crossOffset, item.crossMarginStart(),
					item.crossSize, item.mainIsItemInline,
					item.fragment.Size.Width, item.fragment.Size.Height,
					inlineOff, blockOff,
					item.margins.InlineStart, item.margins.InlineEnd, item.margins.BlockStart, item.margins.BlockEnd)
			}
			builder.AddChild(item.fragment, LogicalOffset{
				InlineOffset: inlineOff,
				BlockOffset:  blockOff,
			})
			// Inherit OOF candidates propagated from non-positioned flex items.
			if len(item.propagatedOOF) > 0 {
				itemBP := ComputeFragmentGeometry(item.style, wdm)
				blockAdj := blockOff + itemBP.Border.BlockStart + itemBP.Padding.BlockStart
				inlineAdj := inlineOff + itemBP.Border.InlineStart + itemBP.Padding.InlineStart
				for _, cand := range item.propagatedOOF {
					adj := cand
					adj.StaticPosition.Offset.BlockOffset += blockAdj
					adj.StaticPosition.Offset.InlineOffset += inlineAdj
					builder.AddOutOfFlowCandidate(adj)
				}
			}
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

	// -webkit-line-clamp: limit block-size to N * line-height.
	// Requires display:-webkit-box (mapped to flex) with -webkit-box-orient:vertical
	// (mapped to column direction). The clamp acts as a max-block-size.
	if lineClamp := fla.style.GetLineClamp(); lineClamp > 0 && !isRow {
		clampHeight := float64(lineClamp) * fla.style.GetLineHeight()
		if finalBlockSize > clampHeight {
			finalBlockSize = clampHeight
		}
	}

	builder.SetSize(LogicalSize{
		InlineSize: contentInlineSize + geom.InlineBorderPadding(),
		BlockSize:  finalBlockSize + geom.BlockBorderPadding(),
	})
	builder.SetIntrinsicBlockSize(intrinsicBlockSize)

	// §4.2 — Flex container baseline.
	// First baseline: from the first non-collapsed item in the first line that
	// participates in baseline alignment (or the first non-collapsed item).
	// Last baseline: from the last non-collapsed item in the last line.
	if len(lines) > 0 {
		crossBPStart := geom.Border.BlockStart + geom.Padding.BlockStart

		// First baseline: search first line.
		if len(lines[0].items) > 0 {
			var firstBaselineItem *flexItem
			for _, item := range lines[0].items {
				if item.collapsed {
					continue
				}
				selfAlign := fla.getAlignSelf(item.style, alignItems)
				baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
					(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
				if selfAlign == "baseline" && baselineParallel {
					firstBaselineItem = item
					break
				}
				if firstBaselineItem == nil {
					firstBaselineItem = item
				}
			}
			if firstBaselineItem == nil {
				firstBaselineItem = lines[0].items[0]
			}
			bl := firstBaselineItem.baseline
			if !firstBaselineItem.hasBaseline && isRow && !wdm.IsVertical() {
				bl = 0 // CSS Align §4.4: synthesize first baseline at block-start
			}
			var itemBlockOffset float64
			if isRow {
				itemBlockOffset = firstBaselineItem.crossOffset + firstBaselineItem.crossMarginStart()
			} else {
				itemBlockOffset = firstBaselineItem.mainOffset
			}
			builder.SetBaseline(crossBPStart + itemBlockOffset + bl)
		}

		// Last baseline: search last line.
		lastLine := lines[len(lines)-1]
		if len(lastLine.items) > 0 {
			var lastBaselineItem *flexItem
			for i := len(lastLine.items) - 1; i >= 0; i-- {
				item := lastLine.items[i]
				if item.collapsed {
					continue
				}
				selfAlign := fla.getAlignSelf(item.style, alignItems)
				baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
					(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
				if selfAlign == "last baseline" && baselineParallel {
					lastBaselineItem = item
					break
				}
				if lastBaselineItem == nil {
					lastBaselineItem = item
				}
			}
			if lastBaselineItem == nil {
				lastBaselineItem = lastLine.items[len(lastLine.items)-1]
			}
			lb := lastBaselineItem.lastBaseline
			if lb <= 0 {
				lb = lastBaselineItem.baseline
			}
			if !lastBaselineItem.hasBaseline && isRow && !wdm.IsVertical() {
				lb = lastBaselineItem.crossSize // CSS Align §4.4: synthesize last baseline at block-end
			}
			var itemBlockOffset float64
			if isRow {
				itemBlockOffset = lastBaselineItem.crossOffset + lastBaselineItem.crossMarginStart()
			} else {
				itemBlockOffset = lastBaselineItem.mainOffset
			}
			builder.SetLastBaseline(crossBPStart + itemBlockOffset + lb)
		}
	}

	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(fla.style, wdm, fla.space.AvailableSize.InlineSize), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:  physMargin,
		Border:  physBorder,
		Padding: physPadding,
	})

	// Layout OOF children. Same fixed/absolute split as block layout:
	// positioned flex containers resolve absolute but propagate fixed.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		isPositioned := fla.style != nil && fla.style.GetPosition() != css.PositionStatic
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
					ctx:                 fla.ctx,
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

	result := builder.Build()
	if len(propagatedOOF) > 0 {
		result.PropagatedOOFCandidates = propagatedOOF
	}

	// CSS position:relative — "start wins over end" in logical coordinates.
	if fla.style != nil && (fla.style.GetPosition() == css.PositionRelative || fla.style.GetPosition() == css.PositionSticky) {
		cbWidth := fla.space.AvailableSize.InlineSize
		cbHeight := fla.space.AvailableSize.BlockSize
		if cbHeight == Indefinite {
			cbHeight = 0
		}
		physCB := ToPhysicalSize(LogicalSize{
			InlineSize: cbWidth,
			BlockSize:  cbHeight,
		}, wdm.WM)
		offset := fla.style.GetPositionOffsetResolved(physCB.Width, physCB.Height)
		result.Fragment.RelativeOffset = computeRelativeOffset(offset, wdm)
	}

	return result
}

// buildFlexChildList pre-processes the flex container's children to implement
// CSS Flexbox §4 anonymous flex item wrapping. Each contiguous run of text
// nodes (with display:none elements being transparent) is grouped into a
// single anonymous block flex item so that inter-word spaces are preserved.
// OOF children (position:absolute/fixed) interrupt text runs.
func (fla *FlexLayoutAlgorithm) buildFlexChildList() []*LayoutInputNode {
	var result []*LayoutInputNode
	var textRun []*LayoutInputNode // accumulates text nodes for current run

	flushTextRun := func() {
		if len(textRun) == 0 {
			return
		}
		// Drop whitespace-only runs.
		hasContent := false
		for _, n := range textRun {
			if strings.TrimSpace(n.TextContent()) != "" {
				hasContent = true
				break
			}
		}
		if hasContent {
			anonStyle := css.NewAnonymousBlockStyle(fla.style)
			result = append(result, &LayoutInputNode{
				style:       anonStyle,
				children:    textRun,
				isAnonymous: true,
			})
		}
		textRun = nil
	}

	for _, child := range fla.node.Children() {
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
		// OOF children interrupt text runs (matches Blink behaviour).
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			flushTextRun()
			result = append(result, child)
			continue
		}
		// Visible in-flow element: flush any pending text run, then add element.
		flushTextRun()
		result = append(result, child)
	}
	flushTextRun()
	return result
}

// collectItems walks node.Children() and returns flex items (skipping OOF and display:none).
func (fla *FlexLayoutAlgorithm) collectItems(
	builder *BoxFragmentBuilder,
	wdm WritingDirectionMode,
	contentInlineSize float64,
	containerMainSize float64,
	hasDefiniteMain bool,
	containerCrossSize float64,
	hasDefiniteCross bool,
	isRow bool,
) []*flexItem {
	var items []*flexItem

	// Pre-process children: group contiguous text runs into anonymous flex items.
	// CSS Flexbox §4: "each contiguous run of text that is directly contained
	// inside a flex container is wrapped in an anonymous block container flex item."
	// display:none elements are transparent (don't interrupt a run).
	// OOF (position:absolute/fixed) elements interrupt runs (Blink behaviour).
	flexChildren := fla.buildFlexChildList()

	for _, child := range flexChildren {
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
				Node: child,
				StaticPosition: LogicalStaticPosition{
					Offset:     LogicalOffset{InlineOffset: 0, BlockOffset: 0},
					InlineEdge: StaticEdgeStart,
					BlockEdge:  StaticEdgeStart,
				},
				IsFixedPosition: pos == css.PositionFixed,
			})
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)
		childGeom := ComputeFragmentGeometry(childStyle, childWDM)

		// Resolve margins for the item.
		// In flex layout, margin auto is used for alignment — resolve to 0 for now,
		// we handle auto margins later.
		// Margins are resolved in the CONTAINER's writing mode so that main/cross
		// margin accessors align with the container's axis directions. Per CSS
		// Flexbox §4.1, flex item margins participate in the container's formatting
		// context. Using the item's WDM would swap inline-start/end for items whose
		// direction differs from the container's.
		childMargins := fla.resolveItemMargins(childStyle, wdm, contentInlineSize, isRow)

		// Compute flex properties (negative values are invalid per spec).
		flexGrow := fla.parseFloat(childStyle, "flex-grow", 0)
		if flexGrow < 0 {
			flexGrow = 0
		}
		flexShrink := fla.parseFloat(childStyle, "flex-shrink", 1)
		if flexShrink < 0 {
			flexShrink = 1
		}
		order := fla.parseInt(childStyle, "order", 0)

		// Constraint space for computing intrinsic sizes (§4.5, min/max).
		itemSizingSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			SetIsInsideFlexibleBox(true).
			Build()

		// Compute flex-basis and hypothetical main size.
		flexBasis := fla.resolveFlexBasis(child, childStyle, childWDM, childGeom, wdm,
			contentInlineSize, containerMainSize, hasDefiniteMain, containerCrossSize, hasDefiniteCross, isRow)

		// Compute §4.5 effective minimum main size (content-based; used for final clamp).
		minMainSize := fla.flexItemMinMain(child, childStyle, childWDM, childGeom,
			itemSizingSpace, flexBasis, isRow, containerCrossSize, hasDefiniteCross, contentInlineSize)

		// For the hypothetical main size (used for wrap decisions and free-space computation),
		// only clamp by the explicit CSS min-width/min-height. The content-based automatic
		// minimum must NOT clamp the hypothetical (§4.5: "not clamped by the item's flex-basis",
		// and items with flex-basis:0 must start at 0, not min-content).
		explicitMin := fla.flexItemExplicitMin(childStyle, childWDM, childGeom, itemSizingSpace, isRow)

		// Clamp flex-basis by explicit min/max main size only.
		hyp := fla.clampMainSizeWithMin(flexBasis, explicitMin, childStyle, childWDM, childGeom,
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
		itemMainIsInline := computeMainIsItemInline(wdm, childWDM, isRow)
		// Auto margins are resolved in the container's WDM (same as margins above).
		mainAS, mainAE, crossAS, crossAE := getItemAutoMargins(childStyle, wdm, isRow)

		item := &flexItem{
			node:           child,
			style:          childStyle,
			wdm:            childWDM,
			geom:           childGeom,
			margins:        childMargins,
			mainIsItemInline: itemMainIsInline,
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
			collapsed:      childStyle.GetVisibility() == "collapse",
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

	if basisVal == "auto" || basisVal == "content" {
		mainIsItemInline := computeMainIsItemInline(parentWDM, childWDM, isRow)

		// flex-basis: auto → use the specified main-size property if set.
		// flex-basis: content → always use content sizing, ignoring specified main-size.
		if basisVal == "auto" {
			// For column flex with a definite main-size, pass it as the percentage
			// resolution block-size so that height:100% on items resolves correctly.
			// §9.8: If a flex container has a definite main size, percentage main
			// sizes on flex items resolve against it.
			pctBlockSize := 0.0
			availBlockSize := Indefinite
			if !isRow && hasDefiniteMain {
				pctBlockSize = containerMainSize
				availBlockSize = containerMainSize
			}
			itemSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
				SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: availBlockSize}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: pctBlockSize}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			if mainIsItemInline {
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					return explicit
				}
			} else {
				if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
					return explicit
				}
			}
			// §9.2 aspect-ratio fallback: when the item has a definite cross-size
			// and a CSS aspect-ratio, derive the flex basis (main-size) from the
			// cross-size × ratio. Only applies to flex-basis: auto — content sizing
			// uses max-content which is independent of cross-size.
			//
			// The item's definite cross-size is (in priority order):
			//  1. Its explicit CSS cross-size property (e.g. width in column flex).
			//  2. Container cross-size, only if the item will stretch to fill it.
			if ar := style.GetAspectRatio(); ar.IsSet {
				var itemCrossContent float64
				var hasItemCross bool
				crossItemSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
					SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
					SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
					SetPercentageResolutionInlineSize(contentInlineSize).
					Build()
				if mainIsItemInline {
					// Cross = block axis. Check for explicit CSS block-size.
					if explicitCross, ok := ResolveBlockSize(style, childWDM, crossItemSpace, childGeom); ok {
						itemCrossContent = explicitCross
						hasItemCross = true
					}
				} else {
					// Cross = inline axis. Check for explicit CSS inline-size.
					if explicitCross, ok := ResolveInlineSize(style, childWDM, crossItemSpace, childGeom); ok {
						itemCrossContent = explicitCross
						hasItemCross = true
					}
				}
				// If no explicit cross-size, check if the item stretches to container.
				if !hasItemCross && hasDefiniteCross {
					alignItems := "stretch"
					if v, ok := fla.style.Get("align-items"); ok {
						alignItems = strings.TrimSpace(v)
					}
					selfAlign := fla.getAlignSelf(style, alignItems)
					_, _, crossAS, crossAE := getItemAutoMargins(style, childWDM, isRow)
					hasExplCross := fla.hasExplicitCrossSize(style, parentWDM, isRow)
					if selfAlign == "stretch" && !crossAS && !crossAE && !hasExplCross {
						if mainIsItemInline {
							itemCrossContent = containerCrossSize - childGeom.BlockBorderPadding() - resolveItemCrossMargins(style, childWDM, contentInlineSize, mainIsItemInline)
						} else {
							itemCrossContent = containerCrossSize - childGeom.InlineBorderPadding() - resolveItemCrossMargins(style, childWDM, contentInlineSize, mainIsItemInline)
						}
						if itemCrossContent < 0 {
							itemCrossContent = 0
						}
						hasItemCross = true
					}
				}
				if hasItemCross {
					if mainIsItemInline && ar.Height > 0 {
						return itemCrossContent * ar.Width / ar.Height
					} else if !mainIsItemInline && ar.Width > 0 {
						return itemCrossContent * ar.Height / ar.Width
					}
				}
			}
			return fla.itemMaxContentMainSize(child, style, childWDM, childGeom, parentWDM,
				contentInlineSize, isRow)
		}
		// flex-basis: content → use content-based max-content sizing.
		// Per CSS Flexbox §9.2, flex-basis:content sizes the item based on its
		// content, not its CSS main-size property or cross-size aspect-ratio.
		return fla.itemContentMaxMainSize(child, style, childWDM, childGeom, parentWDM,
			contentInlineSize, isRow)
	}

	// CSS Flexbox §7.3.3: flex-basis does not accept negative lengths.
	// If a negative value was set, treat as auto (fall back to width/height).
	if v, ok := style.GetLength("flex-basis"); ok && v < 0 {
		basisVal = "auto"
		// Re-run auto logic.
		mainIsItemInline := computeMainIsItemInline(parentWDM, childWDM, isRow)
		itemSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		if mainIsItemInline {
			if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
				return explicit
			}
		} else {
			if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
				return explicit
			}
		}
		// §9.2 Part B: aspect-ratio fallback when the item's cross-size is definite.
		// The item's cross-size is definite if it has an explicit CSS cross-size,
		// OR if it will be stretched (align-self:stretch, no auto cross margins,
		// no explicit cross-size) and the container cross-size is definite.
		// Check both the CSS aspect-ratio property AND intrinsic aspect ratio
		// for replaced elements (images, canvas, video, etc.).
		var arW, arH float64
		var hasAR bool
		if ar := style.GetAspectRatio(); ar.IsSet {
			arW, arH, hasAR = ar.Width, ar.Height, true
		} else if child.DOMNode != nil && isReplacedElement(child.DOMNode) {
			info := GetIntrinsicSizingInfo(fla.ctx, child)
			if info.HasAspectRatio && info.AspectRatio > 0 {
				// Intrinsic aspect ratio is width/height (physical).
				// Convert to the item's logical frame: arW=inline, arH=block.
				if childWDM.IsVertical() {
					arW, arH, hasAR = info.IntrinsicHeight, info.IntrinsicWidth, true
				} else {
					arW, arH, hasAR = info.IntrinsicWidth, info.IntrinsicHeight, true
				}
			}
		}
		if hasAR {
			// Determine the item's definite cross-size content value.
			// Priority: 1) explicit CSS cross-size (clamped by min/max), 2) stretched container cross-size.
			var itemCrossContent float64
			var hasItemCross bool
			if mainIsItemInline {
				// Cross = block axis.
				if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
					itemCrossContent = explicit
					// Clamp by min/max block size.
					minBlock := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom)
					if itemCrossContent < minBlock {
						itemCrossContent = minBlock
					}
					if maxBlock, hasMax := ResolveMaxBlockSize(style, childWDM, itemSpace, childGeom); hasMax && itemCrossContent > maxBlock {
						itemCrossContent = maxBlock
					}
					hasItemCross = true
				}
			} else {
				// Cross = inline axis.
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					itemCrossContent = explicit
					// Clamp by min/max inline size.
					minInline := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom)
					if itemCrossContent < minInline {
						itemCrossContent = minInline
					}
					if maxInline, hasMax := ResolveMaxInlineSize(style, childWDM, itemSpace, childGeom); hasMax && itemCrossContent > maxInline {
						itemCrossContent = maxInline
					}
					hasItemCross = true
				}
			}
			// If no explicit cross-size, check stretch alignment.
			if !hasItemCross && hasDefiniteCross {
				alignItems := "stretch"
				if v, ok := fla.style.Get("align-items"); ok {
					alignItems = strings.TrimSpace(v)
				}
				selfAlign := fla.getAlignSelf(style, alignItems)
				// Check for auto margins in the cross axis.
				_, _, crossAS, crossAE := getItemAutoMargins(style, childWDM, isRow)
				hasExplCross := fla.hasExplicitCrossSize(style, parentWDM, isRow)
				if selfAlign == "stretch" && !crossAS && !crossAE && !hasExplCross {
					crossMargins := resolveItemCrossMargins(style, childWDM, contentInlineSize, isRow)
					if mainIsItemInline {
						itemCrossContent = containerCrossSize - childGeom.BlockBorderPadding() - crossMargins
					} else {
						itemCrossContent = containerCrossSize - childGeom.InlineBorderPadding() - crossMargins
					}
					if itemCrossContent < 0 {
						itemCrossContent = 0
					}
					hasItemCross = true
				}
			}
			if hasItemCross {
				if mainIsItemInline && arH > 0 {
					return itemCrossContent * arW / arH
				} else if !mainIsItemInline && arW > 0 {
					return itemCrossContent * arH / arW
				}
			}
		}
		return fla.itemMaxContentMainSize(child, style, childWDM, childGeom, parentWDM,
			contentInlineSize, isRow)
	}

	// Numeric flex-basis (non-negative lengths/percentages only).
	parentSpace := NewConstraintSpaceBuilder(parentWDM, parentWDM, false).
		SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		Build()
	if isRow {
		// Resolve as inline-size against the container.
		if v, ok := style.GetLength("flex-basis"); ok && v >= 0 {
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
		// Handle calc() with percentages, e.g. calc(50% - 10px).
		if rawBasis, ok := style.Get("flex-basis"); ok && css.IsCalcWithPercent(rawBasis) {
			if v, ok := css.EvalCalcWithPercent(rawBasis[5:len(rawBasis)-1], style.GetFontSize(), contentInlineSize); ok && v >= 0 {
				result := v
				if style.GetBoxSizing() == "border-box" {
					result -= childGeom.InlineBorderPadding()
					if result < 0 {
						result = 0
					}
				}
				return result
			}
		}
	} else {
		// Resolve as block-size.
		if v, ok := style.GetLength("flex-basis"); ok && v >= 0 {
			result := v
			if style.GetBoxSizing() == "border-box" {
				result -= childGeom.BlockBorderPadding()
				if result < 0 {
					result = 0
				}
			}
			return result
		}
		if pct, ok := style.GetPercentage("flex-basis"); ok && (hasDefiniteMain || pct == 0) {
			result := containerMainSize * pct / 100
			if style.GetBoxSizing() == "border-box" {
				result -= childGeom.BlockBorderPadding()
				if result < 0 {
					result = 0
				}
			}
			return result
		}
		// Handle calc() with percentages for column flex.
		if rawBasis, ok := style.Get("flex-basis"); ok && css.IsCalcWithPercent(rawBasis) && hasDefiniteMain {
			if v, ok := css.EvalCalcWithPercent(rawBasis[5:len(rawBasis)-1], style.GetFontSize(), containerMainSize); ok && v >= 0 {
				result := v
				if style.GetBoxSizing() == "border-box" {
					result -= childGeom.BlockBorderPadding()
					if result < 0 {
						result = 0
					}
				}
				return result
			}
		}
	}
	_ = parentSpace

	// Fallback: use max-content.
	return fla.itemMaxContentMainSize(child, style, childWDM, childGeom, parentWDM,
		contentInlineSize, isRow)
}

// itemContentMaxMainSize returns the max-content size in the main axis,
// ignoring any explicit CSS main-size. Used for flex-basis: content.
// Mirrors Blink's ComputeMinAndMaxContentContributionForSelf() which uses
// content-based sizing regardless of flex direction.
func (fla *FlexLayoutAlgorithm) itemContentMaxMainSize(
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	isRow bool,
) float64 {
	// For replaced elements, use intrinsic sizes directly. flex-basis:content
	// must ignore the item's CSS main-size, but ComputeReplacedSize (used by
	// computeContentMinMaxSizes) consults CSS width/height. Use intrinsic
	// dimensions instead, matching Blink's IntrinsicSize() path.
	if child.DOMNode != nil && isReplacedElement(child.DOMNode) {
		info := GetIntrinsicSizingInfo(fla.ctx, child)
		mainIsItemInline := computeMainIsItemInline(parentWDM, childWDM, isRow)
		if mainIsItemInline {
			if childWDM.IsVertical() {
				return info.IntrinsicHeight
			}
			return info.IntrinsicWidth
		}
		if childWDM.IsVertical() {
			return info.IntrinsicWidth
		}
		return info.IntrinsicHeight
	}
	if isRow {
		// Row flex: main axis = inline. Use computeContentMinMaxSizes which
		// ignores the item's explicit CSS inline-size (width).
		space := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		mm := computeContentMinMaxSizes(fla.ctx, child, space)
		return mm.MaxContent
	}
	// Column flex: main axis = block. Lay out the item with indefinite
	// block-size and IsContentSuggestionLayout to suppress its explicit CSS
	// block-size (height). The resulting block-size is content-driven.
	availInline := contentInlineSize
	margins := ResolveMargins(style, childWDM, contentInlineSize)
	availInline -= margins.InlineStart + margins.InlineEnd
	if availInline < 0 {
		availInline = 0
	}
	space := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
		SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		SetIsContentSuggestionLayout(true).
		Build()
	result := layoutElement(fla.ctx, child, space)
	lf := NewLogicalFragment(parentWDM, result.Fragment)
	return lf.BlockSize() - childGeom.BlockBorderPadding()
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
	// For column flex, the item's cross-axis margins reduce the available
	// inline space for layout (affects wrapping behavior and thus block-size).
	availInline := contentInlineSize
	if !isRow {
		margins := ResolveMargins(style, childWDM, contentInlineSize)
		availInline -= margins.InlineStart + margins.InlineEnd
		if availInline < 0 {
			availInline = 0
		}
	}
	space := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
		SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		Build()
	if isRow {
		mm := ComputeMinMaxSizes(fla.ctx, child, space)
		return mm.MaxContent
	}
	// Column: lay out item at full width to get intrinsic block-size.
	result := layoutElement(fla.ctx, child, space)
	lf := NewLogicalFragment(parentWDM, result.Fragment)
	return lf.BlockSize() - childGeom.BlockBorderPadding()
}

// clampMainSizeWithMin clamps the flex-basis to the CSS max constraint only.
// The minimum (§4.5 content-based) is stored in item.minMain and enforced
// by the final clamp in resolveFlexibleLengths — NOT here. This ensures
// growing items (flex-grow>0) start at their flex-basis (e.g. 0), not at
// their content-based minimum, so free space distributes correctly.
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
	// Apply explicit minimum (e.g. min-width/min-height). Content-based automatic
	// minimums are NOT applied here — they're enforced in resolveFlexibleLengths.
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
// mainIsItemInline=true: cross = item's block axis. mainIsItemInline=false: cross = item's inline axis.
func resolveItemCrossMargins(style *css.Style, wdm WritingDirectionMode, containingInlineSize float64, mainIsItemInline bool) float64 {
	margins := ResolveMargins(style, wdm, containingInlineSize)
	if mainIsItemInline {
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
	parentSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
		SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		Build()
	minMain := fla.flexItemMinMain(child, style, childWDM, childGeom, parentSpace, basis, isRow, 0, false, contentInlineSize)
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
		// §12: Collapsed items contribute 0 to line main-size for wrapping.
		itemSize := 0.0
		if !item.collapsed {
			itemSize = item.outerHypotheticalMainSize()
		}

		// CSS Flexbox §10: forced line breaks.
		// break-before / page-break-before: always/left/right/page → new line.
		forcedBreakBefore := false
		if i > 0 {
			forcedBreakBefore = hasForcedBreakBefore(item.style)
		}

		if i == 0 {
			currentLine = append(currentLine, item)
			currentSize = itemSize
			continue
		}
		gap := 0.0
		if len(currentLine) > 0 && !item.collapsed {
			gap = mainGap
		}
		if forcedBreakBefore || (currentSize+gap+itemSize > containerMainSize && len(currentLine) > 0) {
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

// hasForcedBreakBefore returns true if the style requests a forced line break
// before this item (CSS Flexbox §10). Only the modern break-before property
// is checked, and only values that apply to flex line wrapping:
//   - "always": forces a break in any fragmentation context
//   - "column": forces a column break (flex lines in column flex ARE columns)
//
// The legacy page-break-before property does NOT force flex line breaks because
// page-break-before:always maps to break-before:page (CSS Fragmentation §4.4),
// which only applies to paged fragmentation contexts. Similarly, break-before
// values "page", "left", "right", and "region" are context-specific and do not
// apply to flex line wrapping.
func hasForcedBreakBefore(style *css.Style) bool {
	if style == nil {
		return false
	}
	if v, ok := style.Get("break-before"); ok {
		switch v {
		case "always", "column":
			return true
		}
	}
	return false
}

// resolveFlexibleLengths implements §9.7: the flex algorithm.
func (fla *FlexLayoutAlgorithm) resolveFlexibleLengths(
	line *flexLine,
	containerMainSize float64,
	hasDefiniteMain bool,
	mainGap float64,
) {
	items := line.items

	// If no definite container main size, use hypothetical sizes clamped to minimum.
	if !hasDefiniteMain {
		for _, item := range items {
			if item.collapsed {
				item.resolvedMain = 0
				continue
			}
			item.resolvedMain = item.hypothetical
			if item.resolvedMain < item.minMain {
				item.resolvedMain = item.minMain
			}
			if item.resolvedMain < 0 {
				item.resolvedMain = 0
			}
		}
		return
	}

	// Compute initial free space.
	// Free space = container content-box minus the outer hypothetical sizes of all items.
	// Outer size = content-box + border-padding + margins.
	// §12: Collapsed items contribute 0 to used space.
	nonCollapsedCount := 0
	for _, item := range items {
		if !item.collapsed {
			nonCollapsedCount++
		}
	}
	usedSpace := mainGap * float64(nonCollapsedCount-1)
	if nonCollapsedCount == 0 {
		usedSpace = 0
	}
	for _, item := range items {
		if item.collapsed {
			continue
		}
		usedSpace += item.outerHypotheticalMainSize()
	}
	freeSpace := containerMainSize - usedSpace

	// §9.7 step 1: Set each item's target main size to its flex base size.
	for _, item := range items {
		item.resolvedMain = item.flexBasis
		item.frozen = false
	}

	if math.Abs(freeSpace) < 0.001 {
		// No free space: all items stay at their hypothetical main size.
		for _, item := range items {
			item.resolvedMain = item.hypothetical
		}
		return
	}

	growing := freeSpace > 0

	// §9.7 step 2: Size inflexible items. Freeze, setting its target main
	// size to its hypothetical main size:
	//   - any item with a flex factor of zero
	//   - if growing: any item with flex base size > hypothetical (max-clamped)
	//   - if shrinking: any item with flex base size < hypothetical (min-clamped)
	for _, item := range items {
		// §12: Collapsed items don't participate in flex grow/shrink.
		if item.collapsed {
			item.frozen = true
			item.resolvedMain = 0
			continue
		}
		freeze := false
		if growing && item.flexGrow == 0 {
			freeze = true
		} else if !growing && item.flexShrink == 0 {
			freeze = true
		}
		if !freeze {
			if growing && item.flexBasis > item.hypothetical {
				freeze = true
			} else if !growing && item.flexBasis < item.hypothetical {
				freeze = true
			}
		}
		if freeze {
			item.resolvedMain = item.hypothetical
			item.frozen = true
		}
	}

	// §9.7 step 3: Calculate initial free space.
	// Already computed above from outer hypothetical sizes.
	initialFreeSpace := freeSpace

	// §9.7 step 4: Loop.
	for iter := 0; iter < 100; iter++ {
		// 4a: Compute total flex factor and scaled flex shrink factor of unfrozen items.
		var totalFactor float64       // raw flex factor sum (for < 1 check)
		var totalScaledShrink float64 // scaled shrink factor sum (for proportional distribution)
		var unfrozenCount int
		for _, item := range items {
			if !item.frozen {
				if growing {
					totalFactor += item.flexGrow
				} else {
					totalFactor += item.flexShrink
					totalScaledShrink += item.flexShrink * item.flexBasis
				}
				unfrozenCount++
			}
		}
		if unfrozenCount == 0 || totalFactor < 0.001 {
			break
		}

		// 4b: Calculate remaining free space.
		// Frozen items use their target (resolvedMain), unfrozen use their flex base size.
		freeSpace = containerMainSize - mainGap*float64(nonCollapsedCount-1)
		if nonCollapsedCount == 0 {
			freeSpace = containerMainSize
		}
		for _, item := range items {
			if item.collapsed {
				continue
			}
			if item.frozen {
				freeSpace -= item.resolvedMain + item.mainBorderPadding() + item.mainMarginSum()
			} else {
				freeSpace -= item.flexBasis + item.mainBorderPadding() + item.mainMarginSum()
			}
		}

		// §9.7: If the sum of the unfrozen flex items' flex factors is less
		// than one, multiply the initial free space by this sum. If the magnitude
		// of this value is less than the magnitude of the remaining free space,
		// use this as the remaining free space.
		if totalFactor < 1 {
			scaled := initialFreeSpace * totalFactor
			if math.Abs(scaled) < math.Abs(freeSpace) {
				freeSpace = scaled
			}
		}

		if math.Abs(freeSpace) < 0.001 {
			break
		}

		// 4c: Distribute free space. Set each unfrozen item's target main size
		// to its flex base size plus a fraction of the remaining free space.
		for _, item := range items {
			if item.frozen {
				continue
			}
			var delta float64
			if growing {
				if totalFactor > 0 {
					delta = freeSpace * item.flexGrow / totalFactor
				}
			} else {
				if totalScaledShrink > 0 {
					// §9.7 step 4c: distribute negative free space proportionally
					// using scaled flex shrink factors. The < 1 factor cap was
					// already applied to freeSpace above (lines 1922-1927).
					delta = freeSpace * (item.flexShrink * item.flexBasis) / totalScaledShrink
				}
			}
			item.resolvedMain = item.flexBasis + delta
		}

		// 4d: Fix min/max violations. Clamp each non-frozen item's target main
		// size by its used min and max main sizes.
		totalViolation := 0.0
		type violation struct {
			item *flexItem
			adj  float64
		}
		var violations []violation
		for _, item := range items {
			if item.frozen {
				continue
			}
			clamped := item.resolvedMain
			if clamped < item.minMain {
				clamped = item.minMain
			}
			if item.maxMain != Indefinite && clamped > item.maxMain {
				clamped = item.maxMain
			}
			if clamped < 0 {
				clamped = 0
			}
			adj := clamped - item.resolvedMain
			if math.Abs(adj) > 0.001 {
				totalViolation += adj
				violations = append(violations, violation{item, adj})
			}
			item.resolvedMain = clamped
		}

		// 4e: Freeze over-flexed items based on total violation.
		if math.Abs(totalViolation) < 0.001 {
			// Total violation is zero: freeze all items.
			break
		}
		frozenAny := false
		if totalViolation > 0 {
			// Positive: freeze items with min violations.
			for _, v := range violations {
				if v.adj > 0 {
					v.item.frozen = true
					frozenAny = true
				}
			}
		} else {
			// Negative: freeze items with max violations.
			for _, v := range violations {
				if v.adj < 0 {
					v.item.frozen = true
					frozenAny = true
				}
			}
		}
		if !frozenAny {
			break
		}

		// Reset unfrozen items' targets to flex base size for next iteration.
		for _, item := range items {
			if !item.frozen {
				item.resolvedMain = item.flexBasis
			}
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

	// Set orthogonal fallback before SetAvailableSize so the swap can apply it.
	b.SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx))
	b.SetOrthogonalFallbackBlockSize(fla.space.OrthogonalFallbackBlockSize)

	// NOTE: ConstraintSpaceBuilder.SetAvailableSize and SetIsFixedInlineSize/BlockSize
	// automatically swap inline↔block axes for orthogonal children (!b.parallel).
	// We always pass sizes in the PARENT's logical coordinates; the builder handles
	// the conversion to the child's coordinate frame.
	if isRow {
		// Main axis = parent inline. Cross axis = parent block.
		// For orthogonal items (e.g. VRL item in HTB row flex), the builder swaps:
		//   parent's inline (mainSize + mainBP) → child's block (fixed width for VRL)
		//   parent's block (crossSize + crossBP) → child's inline (fixed height for VRL)
		avail := LogicalSize{
			InlineSize: mainSize + item.mainBorderPadding(),
			BlockSize:  Indefinite,
		}
		if crossSize != Indefinite {
			avail.BlockSize = crossSize + item.crossBorderPadding()
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
		b.SetPercentageResolutionInlineSize(contentInlineSize)
		b.SetIsFixedInlineSize(true)
		if crossIsFixed && crossSize != Indefinite {
			b.SetIsFixedBlockSize(true)
		}
	} else {
		// Main axis = parent block. Cross axis = parent inline.
		crossInlineContent := contentInlineSize
		if crossSize != Indefinite {
			crossInlineContent = crossSize
		}
		// For non-fixed cross items (wrapping column flex), subtract cross margins
		// from the available inline-size so fit-content sizing respects the margin box.
		// For column flex, available inline-size is the cross content size.
		// Border-padding is already handled by the child layout geometry.
		availInline := crossInlineContent
		if !crossIsFixed {
			availInline -= item.crossMarginSum()
			if availInline < 0 {
				availInline = 0
			}
		}
		avail := LogicalSize{
			InlineSize: availInline,
			BlockSize:  Indefinite,
		}
		if mainSize > 0 {
			avail.BlockSize = mainSize + item.mainBorderPadding()
		}
		b.SetAvailableSize(avail)
		b.SetPercentageResolutionSize(LogicalSize{
			InlineSize: crossInlineContent,
			BlockSize:  mainSize,
		})
		b.SetPercentageResolutionInlineSize(contentInlineSize)
		b.SetIsFixedInlineSize(crossIsFixed)
		if mainSize > 0 {
			b.SetIsFixedBlockSize(true)
			// Per §9.5: the flex-resolved main size IS the used main size.
			// Override the item's CSS height so the layout uses the flex size.
			b.SetIsBlockSizeOverride(true)
		}
	}

	return b.Build()
}

// computeItemMainOffsets assigns main-axis offsets using justify-content.
func computeItemMainOffsets(
	items []*flexItem,
	containerMainSize float64,
	justifyContent string,
	justifyContentSafe bool,
	reverseMain bool,
	mainGap float64,
) {
	if len(items) == 0 {
		return
	}

	// §12: Filter out collapsed items for spacing calculations.
	// Collapsed items are positioned at the same offset as their predecessor
	// but occupy 0 main-axis space.
	var visibleItems []*flexItem
	for _, item := range items {
		if !item.collapsed {
			visibleItems = append(visibleItems, item)
		}
	}

	// Compute total outer item sizes (content + border-padding + margins).
	totalItemSize := mainGap * float64(len(visibleItems)-1)
	if len(visibleItems) == 0 {
		totalItemSize = 0
	}
	for _, item := range visibleItems {
		totalItemSize += item.outerMainSize()
	}
	freeSpace := containerMainSize - totalItemSize

	// Per CSS Box Alignment §5.3: when free space is negative:
	// - Distributing values (space-between, space-around, space-evenly) fall
	//   back to flex-start per Flexbox Level 1 §8.2.
	// - With "safe" overflow alignment, center and flex-end also fall back to
	//   flex-start to prevent start-edge overflow (data loss prevention).
	if freeSpace < 0 {
		switch justifyContent {
		case "space-between", "space-around", "space-evenly":
			justifyContent = "flex-start"
		case "center", "flex-end":
			if justifyContentSafe {
				justifyContent = "flex-start"
			}
		}
	}

	var initialOffset, gap float64
	switch justifyContent {
	case "flex-end":
		initialOffset = freeSpace
		gap = mainGap
	case "center":
		initialOffset = freeSpace / 2
		gap = mainGap
	case "space-between":
		initialOffset = 0
		if len(visibleItems) > 1 {
			gap = (freeSpace + mainGap*float64(len(visibleItems)-1)) / float64(len(visibleItems)-1)
		} else {
			gap = 0
		}
	case "space-around":
		perItem := 0.0
		if len(visibleItems) > 0 {
			perItem = freeSpace / float64(len(visibleItems))
		}
		initialOffset = perItem / 2
		gap = perItem + mainGap
	case "space-evenly":
		spacing := 0.0
		if len(visibleItems)+1 > 0 {
			spacing = freeSpace / float64(len(visibleItems)+1)
		}
		initialOffset = spacing
		gap = spacing + mainGap
	default: // flex-start
		initialOffset = 0
		gap = mainGap
	}

	if reverseMain {
		// Place items right-to-left from the main-end.
		effectiveSize := containerMainSize
		if effectiveSize < totalItemSize {
			effectiveSize = totalItemSize
		}
		cursor := effectiveSize - initialOffset
		visIdx := 0
		for _, item := range items {
			if item.collapsed {
				// Collapsed items are placed at the current cursor but take no space.
				item.mainOffset = cursor + item.mainMarginStart()
				continue
			}
			cursor -= item.outerMainSize()
			item.mainOffset = cursor + item.mainMarginStart()
			visIdx++
			if visIdx < len(visibleItems) {
				cursor -= gap
			}
		}
	} else {
		cursor := initialOffset
		visIdx := 0
		for _, item := range items {
			if item.collapsed {
				// Collapsed items are placed at the current cursor but take no space.
				item.mainOffset = cursor + item.mainMarginStart()
				continue
			}
			if visIdx > 0 {
				cursor += gap
			}
			item.mainOffset = cursor + item.mainMarginStart()
			cursor += item.outerMainSize()
			visIdx++
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

	var initialOffset, gap float64
	switch alignContent {
	case "flex-end":
		if freeSpace < 0 {
			initialOffset = 0
		} else {
			initialOffset = freeSpace
		}
		gap = crossGap
	case "end":
		if reverseCross {
			initialOffset = 0 // acts like flex-start under wrap-reverse
		} else {
			initialOffset = freeSpace // acts like flex-end
		}
		gap = crossGap
	case "start":
		if reverseCross {
			initialOffset = freeSpace // acts like flex-end under wrap-reverse
		} else {
			initialOffset = 0 // acts like flex-start
		}
		gap = crossGap
	case "center":
		initialOffset = freeSpace / 2
		gap = crossGap
	case "space-between":
		if freeSpace < 0 {
			// Fallback to flex-start.
			initialOffset = 0
			gap = crossGap
		} else {
			initialOffset = 0
			if len(lines) > 1 {
				gap = (freeSpace + crossGap*float64(len(lines)-1)) / float64(len(lines)-1)
			} else {
				gap = 0
			}
		}
	case "space-around":
		if freeSpace < 0 {
			// Fallback to center.
			initialOffset = freeSpace / 2
			gap = crossGap
		} else {
			perLine := 0.0
			if len(lines) > 0 {
				perLine = freeSpace / float64(len(lines))
			}
			initialOffset = perLine / 2
			gap = perLine + crossGap
		}
	case "space-evenly":
		if freeSpace < 0 {
			// Fallback to center.
			initialOffset = freeSpace / 2
			gap = crossGap
		} else {
			spacing := 0.0
			if len(lines)+1 > 0 {
				spacing = freeSpace / float64(len(lines)+1)
			}
			initialOffset = spacing
			gap = spacing + crossGap
		}
	case "stretch":
		if freeSpace > 0 && len(lines) > 1 {
			// Distribute positive free space to lines (multi-line only).
			// For single-line wrapping containers, the stretch pass
			// (stretchFlexItems) handles item stretching separately.
			extra := freeSpace / float64(len(lines))
			for i := range lines {
				lines[i].crossSize += extra
			}
		}
		// Fallback alignment for stretch is flex-start.
		initialOffset = 0
		gap = crossGap
	default: // flex-start, start
		initialOffset = 0
		gap = crossGap
	}

	// Compute offsets in normal (non-reversed) order first.
	cursor := initialOffset
	for i, line := range lines {
		offsets[i] = cursor
		cursor += line.crossSize + gap
	}

	// For wrap-reverse, flip all offsets: each line's offset becomes
	// containerCrossSize - offset - lineCrossSize, per Blink's approach.
	if reverseCross {
		for i, line := range lines {
			offsets[i] = containerCrossSize - offsets[i] - line.crossSize
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

// hasOverflowSafe returns true if the value has the "safe" overflow keyword.
// Per CSS Box Alignment §5.3, "safe" means if the aligned subject overflows
// the alignment container, it is aligned as if the alignment mode were "start".
func hasOverflowSafe(v string) bool {
	return strings.HasPrefix(strings.TrimSpace(v), "safe ")
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

// isJustifyContentSafe returns true if justify-content has the "safe" keyword.
func (fla *FlexLayoutAlgorithm) isJustifyContentSafe() bool {
	if v, ok := fla.style.Get("justify-content"); ok {
		return hasOverflowSafe(v)
	}
	return false
}

// getAlignItems returns the align-items value (default: "stretch").
func (fla *FlexLayoutAlgorithm) getAlignItems() string {
	if v, ok := fla.style.Get("align-items"); ok {
		v = stripOverflowKeyword(v)
		switch v {
		case "stretch", "flex-start", "flex-end", "center", "baseline",
			"start", "end", "self-start", "self-end", "last baseline":
			return v
		case "first baseline":
			return "baseline"
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
				"start", "end", "self-start", "self-end", "last baseline":
				return v
			case "first baseline":
				return "baseline"
			}
		}
	}
	return alignItems
}

// getAlignContent returns the align-content value (default: "stretch").
func (fla *FlexLayoutAlgorithm) getAlignContent() string {
	if v, ok := fla.style.Get("align-content"); ok {
		v = stripOverflowKeyword(v)
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

	// Step 3: Map logical edges to main/cross axis.
	// isRow here acts as mainIsItemInline (caller must pass the correct value
	// for orthogonal items).
	if isRow {
		// main axis = item's inline, cross axis = item's block
		return iAS, iAE, bAS, bAE
	}
	// main axis = item's block, cross axis = item's inline
	return bAS, bAE, iAS, iAE
}

// flexItemMinMain returns the effective minimum main size for a flex item.
// §4.5: For min-width/min-height:auto (the initial value for flex items), returns
// min(min-content-size, flex-basis) — the "automatic minimum size".
// Returns the explicit CSS min value if explicitly set to a non-auto value.
// flexItemExplicitMin returns the explicit CSS minimum main size (min-width / min-height).
// Returns 0 when the minimum is "auto" (i.e. not explicitly set).
// This is used to clamp the hypothetical main size so that wrapping decisions respect
// explicit minimum sizes, while NOT applying the content-based automatic minimum
// (which is only enforced at the final clamp in resolveFlexibleLengths).
func (fla *FlexLayoutAlgorithm) flexItemExplicitMin(
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	space ConstraintSpace,
	isRow bool,
) float64 {
	mainIsItemInline := computeMainIsItemInline(fla.space.WritingDirection, childWDM, isRow)
	if mainIsItemInline {
		minProp := "min-width"
		if childWDM.IsVertical() {
			minProp = "min-height"
		}
		if v, ok := style.Get(minProp); ok && v != "" && v != "auto" {
			return ResolveMinInlineSize(style, childWDM, space, childGeom)
		}
	} else {
		minProp := "min-height"
		if childWDM.IsVertical() {
			minProp = "min-width"
		}
		if v, ok := style.Get(minProp); ok && v != "" && v != "auto" {
			return ResolveMinBlockSize(style, childWDM, space, childGeom)
		}
	}
	return 0
}

func (fla *FlexLayoutAlgorithm) flexItemMinMain(
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	space ConstraintSpace,
	flexBasis float64,
	isRow bool,
	containerCrossSize float64,
	hasDefiniteCross bool,
	contentInlineSize float64,
) float64 {
	// Check if min-size is explicitly set (non-auto).
	// Use mainIsItemInline to determine which axis of the ITEM corresponds to
	// the flex main axis — this is correct for orthogonal items too.
	mainIsItemInline := computeMainIsItemInline(fla.space.WritingDirection, childWDM, isRow)
	if mainIsItemInline {
		// Main axis = item's inline axis → min-inline-size property.
		minProp := "min-width"
		if childWDM.IsVertical() {
			minProp = "min-height"
		}
		if v, ok := style.Get(minProp); ok && v != "" && v != "auto" {
			return ResolveMinInlineSize(style, childWDM, space, childGeom)
		}
	} else {
		// Main axis = item's block axis → min-block-size property.
		minProp := "min-height"
		if childWDM.IsVertical() {
			minProp = "min-width"
		}
		if v, ok := style.Get(minProp); ok && v != "" && v != "auto" {
			return ResolveMinBlockSize(style, childWDM, space, childGeom)
		}
	}

	// §4.5: min-size is auto (default). The automatic minimum size is the
	// content-based minimum size. Only applies when overflow is not scrollable.
	// Per CSSWG resolution (issue #7714) and Blink's IsOverflowValueScrollable(),
	// only scroll containers disable auto-min. overflow:clip is NOT a scroll container.
	isScrollable := func(v css.OverflowType) bool {
		return v != css.OverflowVisible && v != css.OverflowClip
	}
	if isScrollable(style.GetOverflowX()) || isScrollable(style.GetOverflowY()) {
		return 0
	}

	// §4.5 Content size suggestion: min-content size in the main axis.
	contentSuggestion := -1.0
	if mainIsItemInline {
		// Main axis = item's inline axis: inline min-content size at zero available width.
		// PercentageResolutionSize is set so percentage widths on flex items
		// resolve against the flex container's content-box inline-size.
		minContentSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
			SetAvailableSize(LogicalSize{InlineSize: 0, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
			SetPercentageResolutionInlineSize(fla.space.PercentageResolutionInlineSize).
			Build()
		mm := computeContentMinMaxSizes(fla.ctx, child, minContentSpace)
		contentSuggestion = mm.MinContent
	} else {
		// Main axis = item's block axis: block-direction minimum via layout.
		// Use IsContentSuggestionLayout to suppress the item's own CSS block-size
		// so the layout produces the content-based block-size (§4.5).
		containerInlineSize := space.AvailableSize.InlineSize
		colMinSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
			SetAvailableSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: containerInlineSize}).
			SetPercentageResolutionInlineSize(containerInlineSize).
			SetIsContentSuggestionLayout(true).
			Build()
		result := layoutElement(fla.ctx, child, colMinSpace)
		lf := NewLogicalFragment(childWDM, result.Fragment)
		isReplaced := child.DOMNode != nil && isReplacedElement(child.DOMNode)
		if isReplaced {
			// Replaced elements (img, canvas, etc.): use the fragment's resolved
			// block-size, since IntrinsicBlockSize is 0 for childless elements
			// in block layout (the image's computed size is in the fragment).
			contentSuggestion = lf.BlockSize() - childGeom.BlockBorderPadding()
		} else {
			// Non-replaced elements: use IntrinsicBlockSize (the content's natural
			// block-size before min/max/explicit constraints). The fragment's
			// BlockSize includes explicit CSS height (e.g. height:50px), but the
			// content size suggestion per §4.5 is the min-content block-size.
			contentSuggestion = result.IntrinsicBlockSize
		}
	}
	if contentSuggestion < 0 {
		contentSuggestion = 0
	}

	// §4.5 Specified size suggestion: if the item has a definite preferred main size,
	// that size (content-box). Only applies when the preferred size is definite.
	specifiedSuggestion := -1.0
	{
		itemSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		if mainIsItemInline {
			if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
				specifiedSuggestion = explicit
			}
		} else {
			if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
				specifiedSuggestion = explicit
			}
		}
	}

	// §4.5 Transferred size suggestion: if the item has an intrinsic aspect ratio
	// and a definite size in the cross axis, compute the main size from:
	// cross-content-size * aspect-ratio.
	transferredSuggestion := -1.0
	if child.DOMNode != nil && isReplacedElement(child.DOMNode) {
		info := GetIntrinsicSizingInfo(fla.ctx, child)
		if info.HasAspectRatio && info.AspectRatio > 0 {
			// Try to get a definite cross-size: first from explicit CSS, then from
			// the container cross-size (for stretched items).
			crossContentSize := -1.0

			// Check for explicit cross-size on the item.
			itemSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
				SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			if mainIsItemInline {
				// Main = inline, cross = block.
				if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
					crossContentSize = explicit
				}
			} else {
				// Main = block, cross = inline.
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					crossContentSize = explicit
				}
			}

			// §4.5: Also check min-cross-size as a constraint for the transferred
			// size suggestion. Per the spec, if the item has an intrinsic aspect
			// ratio, the transferred size is derived from "constraints in the
			// other dimension", which includes min-height/min-width.
			if crossContentSize < 0 {
				if mainIsItemInline {
					// Cross = block; check min-block-size (min-height).
					minCross := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom)
					if minCross > 0 {
						crossContentSize = minCross
					}
				} else {
					// Cross = inline; check min-inline-size (min-width).
					minCross := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom)
					if minCross > 0 {
						crossContentSize = minCross
					}
				}
			}

			// If no explicit cross-size but container cross is definite (item will be stretched),
			// use the container cross-size minus item's cross border/padding/margins.
			if crossContentSize < 0 && hasDefiniteCross {
				crossMargins := resolveItemCrossMargins(style, childWDM, contentInlineSize, mainIsItemInline)
				if mainIsItemInline {
					crossContentSize = containerCrossSize - childGeom.BlockBorderPadding() - crossMargins
				} else {
					crossContentSize = containerCrossSize - childGeom.InlineBorderPadding() - crossMargins
				}
				if crossContentSize < 0 {
					crossContentSize = 0
				}
			}

			if crossContentSize >= 0 {
				// Convert physical aspect ratio to logical.
				logicalRatio := info.AspectRatio // width/height = inline/block for horizontal WM
				if childWDM.IsVertical() {
					logicalRatio = 1.0 / info.AspectRatio
				}
				if mainIsItemInline {
					// Main = inline: transferred = cross * logicalRatio
					transferredSuggestion = crossContentSize * logicalRatio
				} else {
					// Main = block: transferred = cross / logicalRatio
					if logicalRatio > 0 {
						transferredSuggestion = crossContentSize / logicalRatio
					}
				}
			}
		}
	}
	// Also check CSS aspect-ratio property for non-replaced elements.
	if transferredSuggestion < 0 {
		if ar := style.GetAspectRatio(); ar.IsSet && ar.Width > 0 && ar.Height > 0 {
			crossContentSize := -1.0
			itemSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
				SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			if mainIsItemInline {
				if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
					crossContentSize = explicit
				}
			} else {
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					crossContentSize = explicit
				}
			}
			// Also check min-cross-size as a constraint for non-replaced elements.
			if crossContentSize < 0 {
				if mainIsItemInline {
					minCross := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom)
					if minCross > 0 {
						crossContentSize = minCross
					}
				} else {
					minCross := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom)
					if minCross > 0 {
						crossContentSize = minCross
					}
				}
			}
			if crossContentSize < 0 && hasDefiniteCross {
				crossMargins := resolveItemCrossMargins(style, childWDM, contentInlineSize, mainIsItemInline)
				if mainIsItemInline {
					crossContentSize = containerCrossSize - childGeom.BlockBorderPadding() - crossMargins
				} else {
					crossContentSize = containerCrossSize - childGeom.InlineBorderPadding() - crossMargins
				}
				if crossContentSize < 0 {
					crossContentSize = 0
				}
			}
			if crossContentSize >= 0 {
				if mainIsItemInline {
					transferredSuggestion = crossContentSize * ar.Width / ar.Height
				} else {
					transferredSuggestion = crossContentSize * ar.Height / ar.Width
				}
			}
		}
	}

	// §4.5: Combine the suggestions into the automatic minimum size.
	// The formula differs for replaced vs non-replaced elements:
	//   - Replaced:     min(content, transferred), then cap by specified
	//   - Non-replaced: max(content, transferred), then cap by specified
	// Per Blink's approach and the spec, "transferred" only applies when an
	// aspect ratio is present and a definite cross-size is available.
	isReplaced := child.DOMNode != nil && isReplacedElement(child.DOMNode)
	autoMin := contentSuggestion
	if transferredSuggestion >= 0 {
		if isReplaced {
			// Replaced: use the smaller of content and transferred.
			if transferredSuggestion < autoMin {
				autoMin = transferredSuggestion
			}
		} else {
			// Non-replaced: use the larger of content and transferred.
			if transferredSuggestion > autoMin {
				autoMin = transferredSuggestion
			}
		}
	}
	// Cap by the specified size suggestion (if definite).
	if specifiedSuggestion >= 0 && specifiedSuggestion < autoMin {
		autoMin = specifiedSuggestion
	}

	// §4.5: "In all cases, the size is capped by the item's max main size property, if definite."
	{
		maxSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		if mainIsItemInline {
			if maxMain, hasMax := ResolveMaxInlineSize(style, childWDM, maxSpace, childGeom); hasMax {
				if autoMin > maxMain {
					autoMin = maxMain
				}
			}
		} else {
			if maxMain, hasMax := ResolveMaxBlockSize(style, childWDM, maxSpace, childGeom); hasMax {
				if autoMin > maxMain {
					autoMin = maxMain
				}
			}
		}
	}

	if autoMin < 0 {
		autoMin = 0
	}
	return autoMin
}

// hasExplicitCrossSize returns true if the item has an explicit CSS cross-size property set.
// Per CSS Flexbox §9.4: align-self:stretch only stretches items whose cross-size is auto.
// If the item has an explicit cross-size (width for column flex, height for row flex),
// stretch does not override it.
func (fla *FlexLayoutAlgorithm) hasExplicitCrossSize(style *css.Style, wdm WritingDirectionMode, isRow bool) bool {
	if style == nil {
		return false
	}
	var prop string
	if isRow {
		// cross axis = block = height
		if wdm.IsVertical() {
			prop = "width"
		} else {
			prop = "height"
		}
	} else {
		// cross axis = inline = width
		if wdm.IsVertical() {
			prop = "height"
		} else {
			prop = "width"
		}
	}
	if _, ok := style.GetLength(prop); ok {
		return true
	}
	if _, ok := style.GetPercentage(prop); ok {
		return true
	}
	return false
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
			// CSS Flexbox §9.4: stretch only applies when the item's cross-size is auto.
			// Items with an explicit CSS cross-size keep it even with align-self:stretch.
			if fla.hasExplicitCrossSize(item.style, wdm, isRow) {
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
			// Cross border+padding: swapped for orthogonal items.
			crossIsItemInline := item.mainIsItemInline
			var crossBP float64
			if crossIsItemInline {
				crossBP = item.geom.BlockBorderPadding()
			} else {
				crossBP = item.geom.InlineBorderPadding()
			}
			stretchContent := stretchBorderBox - crossBP
			if stretchContent < 0 {
				stretchContent = 0
			}
			// Clamp to item's own min/max cross size (content-box).
			if crossIsItemInline {
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
			// Always relayout: even if the border-box size is unchanged,
			// the percentage resolution block-size changed from 0 (first pass
			// with Indefinite cross) to stretchContent (now definite).
			// This ensures descendants with percentage heights resolve correctly.
			cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
				item.resolvedMain, stretchContent, true)
			result := layoutElement(fla.ctx, item.node, cs)
			item.fragment = result.Fragment
			item.baseline = result.Baseline
			item.hasBaseline = result.HasBaseline
			item.lastBaseline = result.LastBaseline
			item.propagatedOOF = result.PropagatedOOFCandidates
			lf := NewLogicalFragment(wdm, item.fragment)
			if isRow {
				item.crossSize = lf.BlockSize()
			} else {
				item.crossSize = lf.InlineSize()
			}
			_ = newBorderBox // computed for potential future caching
		}
	}
}
