package layout

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
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

	// Container cross-size (content-box), populated during runLayout.
	// Used by buildItemConstraintSpace to provide a percentage base for
	// flex items' own cross-axis sizes (e.g., height:calc(100%-4em) in row
	// flex), so items can resolve their own percentage heights during the
	// first layout pass before the line cross-size is known.
	containerCrossSize float64
	hasDefiniteCross   bool
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
//
//	HTB container, row, HTB item → mainIsHoriz=true, itemInlineIsHoriz=true  → mainIsItemInline=true
//	HTB container, row, VRL item → mainIsHoriz=true, itemInlineIsHoriz=false → mainIsItemInline=false
//	HTB container, col, HTB item → mainIsHoriz=false, itemInlineIsHoriz=true → mainIsItemInline=false
//	VRL container, row, HTB item → mainIsHoriz=false, itemInlineIsHoriz=true → mainIsItemInline=false
//	VRL container, row, VRL item → mainIsHoriz=false, itemInlineIsHoriz=false→ mainIsItemInline=true
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
	crossSize        float64 // final cross size (border-box)
	mainOffset       float64 // position along main axis (content-box offset within container)
	crossOffset      float64 // position along cross axis (margin-box start within container)
	fragment         *PhysicalFragment
	flexGrow         float64
	flexShrink       float64
	frozen           bool
	order            int
	isRow            bool // true if main axis = inline axis

	// §9.7 freeze bounds (CSS min/max, content-box).
	minMain float64 // effective min main size (§4.5 for auto, or CSS min-width/height)
	maxMain float64 // CSS max main size, or Indefinite if none

	// Auto margins (§8.1): whether each logical edge is margin:auto.
	mainAutoStart  bool
	mainAutoEnd    bool
	crossAutoStart bool
	crossAutoEnd   bool

	// baseline is the first-line baseline position relative to the item's border-box top.
	// Per Blink's SynthesizedBaseline(), items with no natural baseline get a
	// synthesized baseline at the block-end edge (crossSize) for both first and last.
	// Use resolvedFirstBaseline/resolvedLastBaseline helpers for consistent access.
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

	// strutCrossSize is the cross-size "strut" for collapsed items per §12.
	// When an item collapses, it carries the cross-size of the line it was on
	// in the pre-collapse pass. This strut contributes to the cross-size of
	// whatever line the collapsed item ends up on after re-wrapping.
	strutCrossSize float64
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

// resolvedFirstBaseline returns the first baseline position for this item
// relative to its border-box block-start edge.
//
// Orthogonal items participate in baseline alignment with a synthesized
// baseline at the block-end edge (= crossSize) per Blink's
// DetermineBaselineWritingMode (baseline_utils.h:15-44) and
// LogicalBoxFragment::FirstBaselineOrSynthesize. For a row flex with a
// horizontal-tb container and vertical-rl child, the child's baseline is
// read in horizontal-tb and synthesizes to the item's physical bottom.
//
// For non-orthogonal items:
//  1. Use natural baseline if available.
//  2. Otherwise synthesize at block-end of border box (= crossSize) per
//     Blink's SynthesizedBaseline() and CSS Box Alignment §5.4.
func (fi *flexItem) resolvedFirstBaseline(baselineParallel, canSynthesizeRow bool) (bl float64, participates bool) {
	if !baselineParallel {
		// Orthogonal item: per Blink, baseline is synthesized at block-end
		// of the item in the container's block-axis frame (= crossSize).
		return fi.crossSize, true
	}
	if fi.hasBaseline || fi.baseline > 0 {
		return fi.baseline, true
	}
	if canSynthesizeRow {
		return fi.crossSize, true
	}
	return 0, false
}

// resolvedLastBaseline returns the last baseline position for this item
// relative to its border-box block-start edge.
//
// Orthogonal items participate with a synthesized last-baseline at
// block-end (= crossSize), same as first-baseline; the subsequent
// wrap-reverse/is_last_baseline flip (flex_layout_algorithm.cc:382-384)
// transforms this into the appropriate last-baseline position.
//
// For non-orthogonal items:
//  1. Use lastBaseline from layout if available.
//  2. Fall back to first baseline if lastBaseline is not set.
//  3. If no baseline at all, synthesize at block-end (= crossSize).
func (fi *flexItem) resolvedLastBaseline(baselineParallel, canSynthesizeRow bool) (bl float64, participates bool) {
	if !baselineParallel {
		// Orthogonal item: synthesize at block-end (flipped to 0 later for
		// non-wrap-reverse last-baseline per Blink BaselineAscent).
		return fi.crossSize, true
	}
	if lb := fi.lastBaseline; lb > 0 {
		return lb, true
	}
	if fi.baseline > 0 || fi.hasBaseline {
		return fi.baseline, true
	}
	if canSynthesizeRow {
		return fi.crossSize, true
	}
	return 0, false
}

// flexLine holds the items on one flex line.
//
// majorBaseline / minorBaseline / crossAxisOffset mirror Blink's FlexLine
// fields (see third_party/blink/renderer/core/layout/flex/flex_line.h and
// flex_layout_algorithm.cc:1460-1559 for their population). The accumulator
// at :80-153 reads them as:
//
//	first_major_baseline = cross_axis_offset + major_baseline
//	first_minor_baseline = cross_axis_offset + line_cross_size - minor_baseline
//
// majorBaseline = Blink's max_major_ascent: max over kMajor items of
// margins.CrossStart() + (possibly wrap-reverse-flipped) baseline.
// minorBaseline = Blink's max_minor_ascent: max over kMinor items of
// margins.CrossEnd() + (possibly wrap-reverse-flipped) baseline.
// Group membership swaps under wrap-reverse per DetermineBaselineGroup
// (baseline_utils.h:51-89); the flip is applied in the cross-sizing loop
// per BaselineAscent (flex_layout_algorithm.cc:382-384). unsetBaseline marks
// "no item in that group participated on this line" (analogous to Blink's
// LayoutUnit::Min()).
type flexLine struct {
	items           []*flexItem
	crossSize       float64
	majorBaseline   float64 // unsetBaseline if no baseline-aligned item on line
	minorBaseline   float64 // unsetBaseline if no last-baseline-aligned item on line
	crossAxisOffset float64 // physical cross-start offset in container space (post wrap-reverse flip)
}

// unsetBaseline marks "no participant" for flexLine.majorBaseline /
// minorBaseline and baselineAccumulator's optional fields. Mirrors Blink's
// use of LayoutUnit::Min() as a sentinel (flex_layout_algorithm.cc:107-112).
var unsetBaseline = math.Inf(-1)

// baselineAccumulator ports Blink's FlexLayoutAlgorithm::BaselineAccumulator
// (third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc:80-153).
// It aggregates per-line major/minor baselines plus per-item fallbacks to
// produce the container's exported first and last baselines.
//
// Priority (asymmetric — this is from Blink):
//
//	FirstBaseline() = firstMajor → firstMinor → firstFallback
//	LastBaseline()  = lastMinor  → lastMajor  → lastFallback
type baselineAccumulator struct {
	firstMajor, firstMinor, firstFallback float64
	lastMajor, lastMinor, lastFallback    float64
}

func newBaselineAccumulator() *baselineAccumulator {
	return &baselineAccumulator{
		firstMajor: unsetBaseline, firstMinor: unsetBaseline, firstFallback: unsetBaseline,
		lastMajor: unsetBaseline, lastMinor: unsetBaseline, lastFallback: unsetBaseline,
	}
}

// accumulateLine mirrors BaselineAccumulator::AccumulateLine
// (flex_layout_algorithm.cc:104-125). Caller passes line already annotated
// with crossAxisOffset reflecting the post-ApplyReversals physical offset.
// isFirst/isLast are physical-order booleans.
func (a *baselineAccumulator) accumulateLine(line *flexLine, isFirst, isLast bool) {
	if isFirst {
		if line.majorBaseline != unsetBaseline {
			a.firstMajor = line.crossAxisOffset + line.majorBaseline
		}
		if line.minorBaseline != unsetBaseline {
			a.firstMinor = line.crossAxisOffset + line.crossSize - line.minorBaseline
		}
	}
	if isLast {
		if line.majorBaseline != unsetBaseline {
			a.lastMajor = line.crossAxisOffset + line.majorBaseline
		}
		if line.minorBaseline != unsetBaseline {
			a.lastMinor = line.crossAxisOffset + line.crossSize - line.minorBaseline
		}
	}
}

// accumulateFirstFallback mirrors the is_first_line branch of
// BaselineAccumulator::AccumulateItem (flex_layout_algorithm.cc:91-96).
// Only the first-ever call wins (matches Blink's `if (!first_fallback_baseline_)`).
func (a *baselineAccumulator) accumulateFirstFallback(blockOffsetPlusBaseline float64) {
	if a.firstFallback == unsetBaseline {
		a.firstFallback = blockOffsetPlusBaseline
	}
}

// accumulateLastFallback mirrors the is_last_line branch
// (flex_layout_algorithm.cc:98-101). Unconditionally overwrites — Blink
// lets later items on the last line supersede earlier ones.
func (a *baselineAccumulator) accumulateLastFallback(blockOffsetPlusBaseline float64) {
	a.lastFallback = blockOffsetPlusBaseline
}

// firstBaseline returns the container's exported first baseline, using
// the major → minor → fallback priority from flex_layout_algorithm.cc:128-134.
func (a *baselineAccumulator) firstBaseline() (float64, bool) {
	if a.firstMajor != unsetBaseline {
		return a.firstMajor, true
	}
	if a.firstMinor != unsetBaseline {
		return a.firstMinor, true
	}
	if a.firstFallback != unsetBaseline {
		return a.firstFallback, true
	}
	return 0, false
}

// lastBaseline returns the container's exported last baseline, using
// the minor → major → fallback priority from flex_layout_algorithm.cc:135-141.
func (a *baselineAccumulator) lastBaseline() (float64, bool) {
	if a.lastMinor != unsetBaseline {
		return a.lastMinor, true
	}
	if a.lastMajor != unsetBaseline {
		return a.lastMajor, true
	}
	if a.lastFallback != unsetBaseline {
		return a.lastFallback, true
	}
	return 0, false
}

// computeStrutSizes performs a pre-collapse layout pass per CSS Flexbox §12.
// It temporarily treats collapsed items as non-collapsed to form lines with
// their real main sizes, computes line cross-sizes (including align-content
// stretch), then records each collapsed item's strut = its line's cross-size.
func (fla *FlexLayoutAlgorithm) computeStrutSizes(
	items []*flexItem,
	wdm WritingDirectionMode,
	contentInlineSize, containerMainSize float64,
	hasDefiniteMain bool,
	containerCrossSize float64,
	hasDefiniteCross bool,
	isRow bool,
	wrapMode string,
	mainGap, crossGap float64,
) {
	// Temporarily un-collapse items for line formation.
	collapsedSet := make(map[*flexItem]bool)
	for _, item := range items {
		if item.collapsed {
			collapsedSet[item] = true
			item.collapsed = false
		}
	}
	defer func() {
		for item := range collapsedSet {
			item.collapsed = true
		}
	}()

	// Form lines with real sizes.
	preLines := fla.buildFlexLines(items, wrapMode, containerMainSize, hasDefiniteMain, mainGap)

	// Resolve flexible lengths.
	for _, line := range preLines {
		fla.resolveFlexibleLengths(line, containerMainSize, hasDefiniteMain, mainGap)
	}

	// Compute cross-sizes for pre-collapse lines.
	alignItems := fla.getAlignItems()
	for _, line := range preLines {
		lineCrossMax := 0.0
		for _, item := range line.items {
			cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
				item.resolvedMain, Indefinite, false)
			result := layoutElement(fla.ctx, item.node, cs)
			lf := NewLogicalFragment(wdm, result.Fragment)
			var itemCross float64
			if isRow {
				itemCross = lf.BlockSize()
			} else {
				itemCross = lf.InlineSize()
			}

			selfAlign := fla.getAlignSelf(item.style, alignItems)
			outerCross := itemCross + item.crossMarginSum()
			baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
				(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
			if selfAlign == "baseline" && baselineParallel {
				if outerCross > lineCrossMax {
					lineCrossMax = outerCross
				}
			} else {
				if outerCross > lineCrossMax {
					lineCrossMax = outerCross
				}
			}
		}
		line.crossSize = lineCrossMax
	}

	// Single-line definite cross-size override.
	if wrapMode == "nowrap" && len(preLines) == 1 && hasDefiniteCross {
		preLines[0].crossSize = containerCrossSize
	}

	// Apply align-content stretch if applicable (§12: strut captures the
	// STRETCHED line cross-size, not the original).
	alignContent := fla.getAlignContent()
	if alignContent == "stretch" && hasDefiniteCross && len(preLines) > 0 {
		totalCross := 0.0
		for i, line := range preLines {
			totalCross += line.crossSize
			if i < len(preLines)-1 {
				totalCross += crossGap
			}
		}
		if totalCross < containerCrossSize {
			extra := containerCrossSize - totalCross
			if extra > 0 {
				perLine := extra / float64(len(preLines))
				for _, line := range preLines {
					line.crossSize += perLine
				}
			}
		}
	}

	// Record strut sizes for collapsed items.
	for _, line := range preLines {
		for _, item := range line.items {
			if collapsedSet[item] {
				item.strutCrossSize = line.crossSize
			}
		}
	}

	// Reset flex resolution state so the normal pass can re-resolve.
	for _, item := range items {
		item.frozen = false
		item.resolvedMain = item.flexBasis
	}
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
	fla.containerCrossSize = containerCrossSize
	fla.hasDefiniteCross = hasDefiniteCross

	// Resolve gap properties.
	contentBlockSize := 0.0
	hasDefiniteBlock := false
	if hasExplicitBlock {
		contentBlockSize = explicitBlockSize
		hasDefiniteBlock = true
	}
	mainGap, crossGap := fla.resolveGaps(wdm, isRow, contentInlineSize, contentBlockSize, hasDefiniteBlock)

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
			wrapMainSize = maxBlock.Float64()
			hasWrapBoundary = true
		}
	}
	// §12 — Pre-collapse pass: if any items have visibility:collapse in a
	// wrapping container, compute strut cross-sizes before the real layout.
	// The spec requires running the algorithm twice: first with collapsed
	// items at their real sizes to determine line cross-sizes (including
	// stretch), then again with collapsed items at zero main-size.
	hasCollapsedItems := false
	for _, item := range allItems {
		if item.collapsed {
			hasCollapsedItems = true
			break
		}
	}
	if hasCollapsedItems && wrapMode != "nowrap" {
		fla.computeStrutSizes(allItems, wdm, contentInlineSize, wrapMainSize,
			hasWrapBoundary, containerCrossSize, hasDefiniteCross, isRow,
			wrapMode, mainGap, crossGap)
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
		// Initialize baseline accumulators to -MaxFloat64 (like Blink's
		// LayoutUnit::Min()) so that a single item whose baseline is
		// outside its border-box produces ascent+descent = crossSize
		// instead of inflating the line. See baseline-outside-flex-item test.
		maxAscent := -math.MaxFloat64
		maxDescent := -math.MaxFloat64
		hasBaselineItem := false
		maxLastAscent := -math.MaxFloat64
		maxLastDescent := -math.MaxFloat64
		hasLastBaselineItem := false
		for _, item := range line.items {
			// For column flex: the initial layout pass measures intrinsic cross-sizes.
			// Stretch items are NOT given crossIsFixed=true here; instead the stretch
			// pass (stretchFlexItems) re-lays them out at the final line cross-size.
			// This is critical for multi-line column flex where each line may have a
			// different cross-size.
			crossIsFixed := false
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
			if item.node.DOMNode != nil && IsReplacedElement(item.node.DOMNode) {
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
			if item.node.DOMNode != nil && IsReplacedElement(item.node.DOMNode) &&
				!fla.hasExplicitCrossSize(item.style, wdm, isRow) {
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
						minCross := ResolveMinBlockSize(item.style, item.wdm, cs, item.geom).Float64()
						if crossContent < minCross {
							crossContent = minCross
						}
						if maxCrossLU, hasMax := ResolveMaxBlockSize(item.style, item.wdm, cs, item.geom); hasMax && crossContent > maxCrossLU.Float64() {
							crossContent = maxCrossLU.Float64()
						}
					} else {
						// Main = block axis → cross = inline axis.
						// logicalRatio = inline/block → inline = block * ratio.
						crossContent = mainContent * logicalRatio
						// Clamp by min/max inline (cross) size.
						minCross := ResolveMinInlineSize(item.style, item.wdm, cs, item.geom).Float64()
						if crossContent < minCross {
							crossContent = minCross
						}
						if maxCrossLU, hasMax := ResolveMaxInlineSize(item.style, item.wdm, cs, item.geom); hasMax && crossContent > maxCrossLU.Float64() {
							crossContent = maxCrossLU.Float64()
						}
					}
					item.crossSize = crossContent + item.crossBorderPadding()
				}
			}

			// §9.4: line cross-size computation.
			// §12: Collapsed items contribute their strut cross-size (from the
			// pre-collapse pass) rather than their own outer cross-size.
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			outerCross := item.crossSize + item.crossMarginSum()
			if item.collapsed && item.strutCrossSize > 0 {
				outerCross = item.strutCrossSize
			}
			baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
				(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
			canSynthesizeRow := isRow && !wdm.IsVertical()
			if selfAlign == "baseline" || selfAlign == "last baseline" {
				isLastBaseline := selfAlign == "last baseline"
				var baselineVal float64
				var ok bool
				if isLastBaseline {
					baselineVal, ok = item.resolvedLastBaseline(baselineParallel, canSynthesizeRow)
				} else {
					baselineVal, ok = item.resolvedFirstBaseline(baselineParallel, canSynthesizeRow)
				}
				if ok {
					// Mirror Blink's BaselineAscent (flex_layout_algorithm.cc:378-392):
					//   baseline = is_last_baseline ? LastBaseline : FirstBaseline;
					//   if (is_wrap_reverse_ != is_last_baseline)
					//       baseline = BlockSize() - baseline;
					//   return is_major ? CrossStart + baseline : CrossEnd + baseline;
					if reverseCross != isLastBaseline {
						baselineVal = item.crossSize - baselineVal
					}
					// baseline_group per DetermineBaselineGroup (baseline_utils.h:51-89):
					//   Normal wrap: align:baseline→kMajor, align:last-baseline→kMinor.
					//   Wrap-reverse swaps both → align:baseline→kMinor,
					//   align:last-baseline→kMajor. Equivalently:
					//   isMajor = (isLastBaseline == reverseCross).
					isMajor := isLastBaseline == reverseCross
					if isMajor {
						ascent := item.crossMarginStart() + baselineVal
						descent := outerCross - ascent
						if ascent > maxAscent {
							maxAscent = ascent
						}
						if descent > maxDescent {
							maxDescent = descent
						}
						hasBaselineItem = true
					} else {
						lastAscent := item.crossMarginEnd() + baselineVal
						lastDescent := outerCross - lastAscent
						if lastAscent > maxLastAscent {
							maxLastAscent = lastAscent
						}
						if lastDescent > maxLastDescent {
							maxLastDescent = lastDescent
						}
						hasLastBaselineItem = true
					}
				} else {
					if outerCross > lineCrossMax {
						lineCrossMax = outerCross
					}
				}
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

		// Record per-line major/minor baselines for the container's baseline
		// export (see baselineAccumulator). Mirrors Blink's
		// FlexLayoutAlgorithm::PlaceFlexItems at flex_layout_algorithm.cc:1557-1559
		// which stores max_major_ascent and max_minor_ascent onto FlexLine.
		//   majorBaseline = max_major_ascent = max over kMajor items of
		//                   margins.CrossStart() + (possibly flipped) baseline.
		//   minorBaseline = max_minor_ascent = max over kMinor items of
		//                   margins.CrossEnd()   + (possibly flipped) baseline.
		// Membership of items in kMajor/kMinor swaps under wrap-reverse per
		// DetermineBaselineGroup (baseline_utils.h:51-89).
		if hasBaselineItem {
			line.majorBaseline = maxAscent
		} else {
			line.majorBaseline = unsetBaseline
		}
		if hasLastBaselineItem {
			line.minorBaseline = maxLastAscent
		} else {
			line.minorBaseline = unsetBaseline
		}
	}

	// §9.4 step 8: If single-line and container has definite cross size,
	// the flex line cross-size equals the container cross-size.
	if wrapMode == "nowrap" && len(lines) == 1 && hasDefiniteCross {
		lines[0].crossSize = containerCrossSize
	}

	// For single-line wrapping containers with align-content:stretch (the default),
	// grow the line cross-size to the container cross-size. This must happen before
	// lineOffsets computation so that wrap-reverse offset flipping uses the correct
	// (stretched) line size. Matches Blink's behavior.
	alignContent := fla.getAlignContent()
	if wrapMode != "nowrap" && len(lines) == 1 && hasDefiniteCross &&
		alignContent == "stretch" && containerCrossSize > lines[0].crossSize {
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

	// Apply min/max cross constraints BEFORE the §9.8 re-layout and stretch
	// passes, so items see the clamped (definite) line cross-size when
	// resolving descendant percentages. Mirrors Blink: total_block_size_ is
	// computed via ComputeBlockSizeForFragment (min/max applied) before
	// GiveItemsFinalPositionAndSize sets the single line's cross size to it
	// and gives items their final layout.
	if isRow {
		// cross = block
		// CSS 2.1 §10.7: apply max first, then min, so min wins when min > max.
		if maxBlockLU, hasMax := ResolveMaxBlockSize(fla.style, wdm, fla.space, geom); hasMax {
			maxBlock := maxBlockLU.Float64()
			if containerCrossSize > maxBlock {
				containerCrossSize = maxBlock
			}
		}
		minBlock := ResolveMinBlockSize(fla.style, wdm, fla.space, geom).Float64()
		if containerCrossSize < minBlock {
			containerCrossSize = minBlock
		}
	}
	// else: cross = inline, already constrained via contentInlineSize.

	// §9.4 step 8: for single-line containers the flex line's cross-size is
	// clamped to the container's min/max cross sizes — i.e. it always tracks
	// the container's resolved cross-size.
	if wrapMode == "nowrap" && len(lines) == 1 {
		lines[0].crossSize = containerCrossSize
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
		// Don't shrink the line below its first-pass size — UNLESS this is
		// a single-line container with a definite cross-size, in which case
		// the line cross-size was already fixed to the container's cross-size
		// in §9.4 step 8 and must not grow.
		isSingleLineDefinite := wrapMode == "nowrap" && len(lines) == 1 && hasDefiniteCross
		if !isSingleLineDefinite && lineCrossMax > line.crossSize {
			line.crossSize = lineCrossMax
		}
	}

	// §9.6 — align-content: distribute lines within container cross-size.
	// Check for "safe" keyword before getAlignContent strips it.
	rawAlignContent := ""
	if v, ok := fla.style.Get("align-content"); ok {
		rawAlignContent = strings.TrimSpace(v)
	}
	alignContentSafe := strings.Contains(rawAlignContent, "safe")
	alignContent = fla.getAlignContent()
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
	fla.stretchFlexItems(lines, alignItems, wdm, contentInlineSize, isRow)

	// When the main axis is indefinite (auto-sized), compute the actual
	// container main size from item outer sizes, then apply min/max constraints.
	// This ensures justify-content sees the correct free space (e.g., min-height
	// on a column flex gives extra space for justify-content to distribute).
	if !hasDefiniteMain {
		hypotheticalMainSize := 0.0
		for _, line := range lines {
			lineTotal := mainGap * float64(len(line.items)-1)
			for _, item := range line.items {
				lineTotal += item.outerMainSize()
			}
			if lineTotal > hypotheticalMainSize {
				hypotheticalMainSize = lineTotal
			}
		}
		containerMainSize = hypotheticalMainSize
		// Apply min/max block constraints (only relevant for column flex where
		// main axis = block axis and the container has min-height/max-height).
		// CSS 2.1 §10.7: Apply max first, then min. This ensures min wins when min > max.
		if !isRow {
			if maxBlockLU, hasMax := ResolveMaxBlockSize(fla.style, wdm, fla.space, geom); hasMax {
				maxBlock := maxBlockLU.Float64()
				if containerMainSize > maxBlock {
					containerMainSize = maxBlock
				}
			}
			minBlock := ResolveMinBlockSize(fla.style, wdm, fla.space, geom).Float64()
			if containerMainSize < minBlock {
				containerMainSize = minBlock
			}
		}

		// If min/max constraints changed the container main size, the items need
		// to be re-resolved with the new definite size. Per CSS Flexbox §9.7,
		// when a column flex container's auto block-size is constrained by
		// min-height/max-height, the flex algorithm must run again with the
		// constrained size to properly distribute space (grow/shrink).
		if math.Abs(containerMainSize-hypotheticalMainSize) > 0.001 {
			for _, line := range lines {
				// Reset frozen/resolved state.
				for _, item := range line.items {
					item.frozen = false
					item.resolvedMain = item.flexBasis
				}
				fla.resolveFlexibleLengths(line, containerMainSize, true, mainGap)
			}
			// Re-layout items at the new resolved main sizes.
			for _, line := range lines {
				for _, item := range line.items {
					crossIsFixed := false
					cs := fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
						item.resolvedMain, Indefinite, crossIsFixed)
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
				}
			}
			// Re-apply stretch after re-layout: the re-layout above uses
			// crossIsFixed=false and Indefinite cross, which shrinks stretch
			// items back to their hypothetical cross-size. Stretch them back
			// to the (now finalized) line cross-size.
			fla.stretchFlexItems(lines, alignItems, wdm, contentInlineSize, isRow)
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
		sharedBaseline := -math.MaxFloat64
		hasBaselineItem := false
		// Last baseline: sharedLastDescend = max(crossMarginEnd + (crossSize - lastBaseline)).
		sharedLastDescend := -math.MaxFloat64
		hasLastBaselineItem := false
		// Baseline synthesis at block-end of border-box is only correct for
		// horizontal writing modes. Vertical writing modes use the central
		// baseline (text-orientation dependent) which is not yet implemented.
		canSynthesizeRow := isRow && !wdm.IsVertical()
		for _, item := range line.items {
			if item.collapsed {
				continue // §12: collapsed items don't participate in baseline positioning
			}
			selfAlign := fla.getAlignSelf(item.style, alignItems)
			// Baseline participation requires parallel axes (row with same
			// vertical-ness, or column with orthogonal vertical-ness).
			baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
				(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
			// Column containers with same writing mode: items participate
			// using a synthesized inline-axis baseline at the item's
			// inline-start edge (CSS Box Alignment §7 baseline synthesis).
			// For LTR this is bl=0; for RTL this is bl=crossSize. The
			// baseline framework below handles the reverseCross flip.
			columnSameWM := !isRow && item.wdm.IsVertical() == wdm.IsVertical()
			if selfAlign == "baseline" {
				var bl float64
				var ok bool
				if columnSameWM {
					bl = 0
					if wdm.Dir == DirectionRTL {
						bl = item.crossSize
					}
					ok = true
				} else {
					bl, ok = item.resolvedFirstBaseline(baselineParallel, canSynthesizeRow)
				}
				if ok {
					var b float64
					if reverseCross {
						// wrap-reverse: first-baseline items align from cross-end.
						// sharedBaseline = max(crossMarginEnd + (crossSize - bl)).
						b = item.crossMarginEnd() + (item.crossSize - bl)
					} else {
						b = item.crossMarginStart() + bl
					}
					if b > sharedBaseline {
						sharedBaseline = b
					}
					hasBaselineItem = true
				}
			}
			if selfAlign == "last baseline" {
				if lb, ok := item.resolvedLastBaseline(baselineParallel, canSynthesizeRow); ok {
					var d float64
					if reverseCross {
						// wrap-reverse: last-baseline items align from cross-start.
						// sharedLastDescend = max(crossMarginStart + lb).
						d = item.crossMarginStart() + lb
					} else {
						d = item.crossMarginEnd() + (item.crossSize - lb)
					}
					if d > sharedLastDescend {
						sharedLastDescend = d
					}
					hasLastBaselineItem = true
				}
			}
		}

		// Align items in this line.
		for _, item := range line.items {
			selfAlign := fla.getAlignSelf(item.style, alignItems)

			// §8.1: Auto margins in the cross axis override align-self.
			// crossFreeSpace = line cross-size minus item's outer cross-size (border-box + margins).
			// Resolved auto margins are written back into item.margins so they
			// surface on the fragment's BoxData.Margin (for CSSOM resolved values)
			// and so positioning can re-use item.crossMarginStart() uniformly.
			crossFreeSpace := line.crossSize - item.crossSize - item.crossMarginSum()
			if item.crossAutoStart && item.crossAutoEnd {
				// Both auto margins: each absorbs half of free space, centering
				// the item. Matches Blink: when the item overflows the line
				// (negative free space), the item is centered with equal
				// overflow on both sides — equivalent to align-self:center.
				half := crossFreeSpace / 2
				if item.isRow {
					item.margins.BlockStart += half
					item.margins.BlockEnd += half
				} else {
					item.margins.InlineStart += half
					item.margins.InlineEnd += half
				}
				item.crossOffset = crossStart
				continue
			}
			if crossFreeSpace > 0 && (item.crossAutoStart || item.crossAutoEnd) {
				if item.crossAutoStart {
					// Auto start only: absorbs all free space at the start.
					if item.isRow {
						item.margins.BlockStart += crossFreeSpace
					} else {
						item.margins.InlineStart += crossFreeSpace
					}
				} else {
					// Auto end only: absorbs all free space at the end.
					if item.isRow {
						item.margins.BlockEnd += crossFreeSpace
					} else {
						item.margins.InlineEnd += crossFreeSpace
					}
				}
				item.crossOffset = crossStart
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
				// Per Blink's ResolvedAlignSelf (flex_layout_algorithm.cc:308-310),
				// `end` resolves to FlexEnd with an EARLY RETURN — the wrap-reverse
				// flip (applied later to FlexStart/FlexEnd) does not affect `end`.
				// So `end` always aligns to cross-end regardless of wrap-reverse.
				itemCrossOffset = crossStart + crossFreeForAlign
			case "start":
				// Per Blink's ResolvedAlignSelf (flex_layout_algorithm.cc:305-307),
				// `start` resolves to FlexStart with an EARLY RETURN — wrap-reverse
				// does not flip `start`. Always aligns to cross-start.
				itemCrossOffset = crossStart
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
			case "baseline", "first baseline":
				baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
					(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
				columnSameWM := !isRow && item.wdm.IsVertical() == wdm.IsVertical()
				if hasBaselineItem {
					var bl float64
					var ok bool
					if columnSameWM {
						// Synthesized inline-axis baseline: inline-start edge.
						bl = 0
						if wdm.Dir == DirectionRTL {
							bl = item.crossSize
						}
						ok = true
					} else {
						bl, ok = item.resolvedFirstBaseline(baselineParallel, canSynthesizeRow)
					}
					if ok {
						if reverseCross {
							// wrap-reverse: first-baseline items align from cross-end (bottom).
							// sharedBaseline = max(crossMarginEnd + crossSize - bl).
							itemCrossOffset = crossStart + line.crossSize - sharedBaseline - bl - item.crossMarginStart()
						} else {
							itemCrossOffset = crossStart + sharedBaseline - item.crossMarginStart() - bl
						}
					} else {
						itemCrossOffset = crossStart // fallback to flex-start
					}
				} else {
					itemCrossOffset = crossStart
				}
			case "last baseline":
				if hasLastBaselineItem {
					baselineParallel := (isRow && item.wdm.IsVertical() == wdm.IsVertical()) ||
						(!isRow && item.wdm.IsVertical() != wdm.IsVertical())
					if bl, ok := item.resolvedLastBaseline(baselineParallel, canSynthesizeRow); ok {
						if reverseCross {
							// wrap-reverse: last-baseline items align from cross-start (top).
							itemCrossOffset = crossStart + sharedLastDescend - item.crossMarginStart() - bl
						} else {
							itemCrossOffset = crossStart + line.crossSize - sharedLastDescend - item.crossMarginStart() - bl
						}
					} else {
						itemCrossOffset = crossStart + crossFreeForAlign
					}
				} else {
					itemCrossOffset = crossStart + crossFreeForAlign
				}
			default: // flex-start, stretch
				itemCrossOffset = crossStart
			}
			// Per CSS Box Alignment §5.3: when "safe" overflow alignment is
			// specified and the item overflows the line (negative free space),
			// fall back to start alignment to prevent start-edge overflow.
			if itemCrossOffset < crossStart && fla.isAlignSelfSafe(item.style) {
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
					physI = contentInlineSize - sorted[i].mainOffset - sorted[i].fragment.Size.WidthF64()
					physJ = contentInlineSize - sorted[j].mainOffset - sorted[j].fragment.Size.WidthF64()
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
					item.fragment.Size.WidthF64(), item.fragment.Size.HeightF64(),
					inlineOff, blockOff,
					item.margins.InlineStart, item.margins.InlineEnd, item.margins.BlockStart, item.margins.BlockEnd)
			}
			// Propagate auto-margin resolution (§8.1) onto the item fragment's
			// physical margins. Flex layout owns margin resolution for its
			// items, so the fragment's BoxData.Margin must reflect the
			// container-WDM logical item.margins after auto-margin absorption.
			// CSSOM getComputedStyle reads used margins from this field.
			if item.fragment != nil && item.fragment.BoxData != nil {
				item.fragment.BoxData.Margin = ToPhysicalEdges(item.margins, wdm)
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

	// CSS Contain 1 §4.2: size containment — collapse intrinsic block-size to 0
	// when no explicit block-size is set (see contain_utils.go).
	intrinsicBlockSize = sizeContainedIntrinsicBlockSize(fla.style, hasExplicitBlock, intrinsicBlockSize)

	finalBlockSize := intrinsicBlockSize
	if hasExplicitBlock {
		finalBlockSize = explicitBlockSize
	}

	// Apply min/max block constraints.
	// CSS 2.1 §10.7: Apply max first, then min. This ensures min wins when min > max.
	if maxBlockLU, hasMax := ResolveMaxBlockSize(fla.style, wdm, fla.space, geom); hasMax {
		maxBlock := maxBlockLU.Float64()
		if finalBlockSize > maxBlock {
			finalBlockSize = maxBlock
		}
	}
	minBlock := ResolveMinBlockSize(fla.style, wdm, fla.space, geom).Float64()
	if finalBlockSize < minBlock {
		finalBlockSize = minBlock
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
	//
	// This is a port of Blink's BaselineAccumulator mechanism from
	// third_party/blink/renderer/core/layout/flex/flex_layout_algorithm.cc.
	// The accumulator (:80-153) aggregates each line's major/minor baselines
	// into the container's exported first/last baselines, with per-item
	// fallbacks used when no baseline-aligned item participates.
	//
	// Iteration order is "physical", matching Blink's post-ApplyReversals
	// frame (:1589-1599): under flex-wrap: wrap-reverse, lines[] remains in
	// source order in louis14, so we walk the index sequence in reverse.
	// Each line's crossAxisOffset is populated from the (already-flipped)
	// lineOffsets so that the accumulator's
	//
	//	first_major_baseline = cross_axis_offset + major_baseline
	//
	// invariant holds verbatim against the container's physical frame.
	//
	// AccumulateLine is row-only in Blink (see the !is_column_ guard at
	// :1784); columns rely exclusively on the per-item fallback path.
	if len(lines) > 0 {
		crossBPStart := geom.Border.BlockStart + geom.Padding.BlockStart

		// Populate each line's crossAxisOffset with its physical cross-start
		// offset from the container's border-box start. lineOffsets already
		// reflects the wrap-reverse flip (applied inside computeAlignContent
		// and in the nowrap single-line path above).
		for i, line := range lines {
			line.crossAxisOffset = crossBPStart + lineOffsets[i]
		}

		canSynthesize := !wdm.IsVertical()

		// baselineAxisParallel gates the fallback path to items whose
		// baseline axis is parallel to the container's (same writing-mode
		// verticality). Differs from per-line baseline ALIGNMENT (which is
		// main-axis parallel) — the container baseline export cares about
		// the item's block-axis orientation per CSS Flexbox §8.5.
		baselineAxisParallel := func(item *flexItem) bool {
			return item.wdm.IsVertical() == wdm.IsVertical()
		}

		// itemBlockOffset returns the block-axis offset of the item's
		// border-box block-start edge, relative to the container's
		// content-box block-start. Matches Blink's offset.block_offset that
		// is passed to AccumulateItem at :1980-1981.
		itemBlockOffset := func(item *flexItem) float64 {
			if isRow {
				return item.crossOffset + item.crossMarginStart()
			}
			return item.mainOffset
		}

		// fallbackFirstBaseline mirrors LogicalBoxFragment::FirstBaselineOrSynthesize
		// combined with Blink's baseline-axis gate. Returns the distance from
		// the item's border-box block-start to its first baseline.
		fallbackFirstBaseline := func(item *flexItem) (float64, bool) {
			if baselineAxisParallel(item) && (item.hasBaseline || item.baseline > 0) {
				return item.baseline, true
			}
			if canSynthesize {
				return item.crossSize, true
			}
			return 0, false
		}

		// fallbackLastBaseline mirrors LastBaselineOrSynthesize, falling back
		// to first baseline when no last-baseline is present.
		fallbackLastBaseline := func(item *flexItem) (float64, bool) {
			if baselineAxisParallel(item) {
				if item.lastBaseline > 0 {
					return item.lastBaseline, true
				}
				if item.hasBaseline || item.baseline > 0 {
					return item.baseline, true
				}
			}
			if canSynthesize {
				return item.crossSize, true
			}
			return 0, false
		}

		// Under row-reverse / column-reverse, Blink's ApplyReversals
		// (flex_layout_algorithm.cc:1594-1598) reverses each line's
		// item_indices. AccumulateItem then iterates post-reversal order:
		// "first item iterated" = source-last, "last item iterated" (whose
		// last_fallback value wins, overwriting earlier ones) = source-first.
		// We emulate that by flipping firstNonCollapsed/lastNonCollapsed under
		// reverseMain.
		firstNonCollapsed := func(items []*flexItem) *flexItem {
			if reverseMain {
				for i := len(items) - 1; i >= 0; i-- {
					if !items[i].collapsed {
						return items[i]
					}
				}
				return nil
			}
			for _, it := range items {
				if !it.collapsed {
					return it
				}
			}
			return nil
		}
		lastNonCollapsed := func(items []*flexItem) *flexItem {
			if reverseMain {
				for _, it := range items {
					if !it.collapsed {
						return it
					}
				}
				return nil
			}
			for i := len(items) - 1; i >= 0; i-- {
				if !items[i].collapsed {
					return items[i]
				}
			}
			return nil
		}

		// Physical iteration: under wrap-reverse source-last is physical-first.
		order := make([]int, len(lines))
		for i := range order {
			order[i] = i
		}
		if reverseCross {
			for l, r := 0, len(order)-1; l < r; l, r = l+1, r-1 {
				order[l], order[r] = order[r], order[l]
			}
		}

		accum := newBaselineAccumulator()
		for i, idx := range order {
			line := lines[idx]
			isFirst := i == 0
			isLast := i == len(order)-1

			// AccumulateLine is row-only in Blink (flex_layout_algorithm.cc:1784).
			if isRow {
				accum.accumulateLine(line, isFirst, isLast)
			}

			// Per-item fallback — mirrors AccumulateItem at :1980-1981.
			// We only need to feed the first item on the first physical line
			// and the last item on the last physical line (the intervening
			// AccumulateItem calls in Blink are no-ops because of the
			// is_first_line / is_last_line guards at :91-101).
			if isFirst {
				if it := firstNonCollapsed(line.items); it != nil {
					if bl, ok := fallbackFirstBaseline(it); ok {
						accum.accumulateFirstFallback(crossBPStart + itemBlockOffset(it) + bl)
					}
				}
			}
			if isLast {
				if it := lastNonCollapsed(line.items); it != nil {
					if bl, ok := fallbackLastBaseline(it); ok {
						accum.accumulateLastFallback(crossBPStart + itemBlockOffset(it) + bl)
					}
				}
			}
		}

		if bl, ok := accum.firstBaseline(); ok {
			builder.SetBaseline(bl)
		}
		if bl, ok := accum.lastBaseline(); ok {
			builder.SetLastBaseline(bl)
		}
	}

	physBorder := ToPhysicalEdges(geom.Border, wdm)
	physPadding := ToPhysicalEdges(geom.Padding, wdm)
	physScrollbar := ToPhysicalEdges(geom.Scrollbar, wdm)
	physMargin := ToPhysicalEdges(ResolveMargins(fla.style, wdm, fla.space.AvailableSize.InlineSize.Float64()), wdm)
	builder.SetBoxData(&PhysicalBoxData{
		Margin:    physMargin,
		Border:    physBorder,
		Padding:   physPadding,
		Scrollbar: physScrollbar,
	})

	// Layout OOF children. Same fixed/absolute split as block layout:
	// positioned flex containers resolve absolute but propagate fixed.
	var propagatedOOF []OutOfFlowCandidate
	if len(builder.outOfFlowCandidates) > 0 {
		isPositioned := fla.style != nil && fla.style.GetPosition() != css.PositionStatic
		// CSS Will Change §2.2: `will-change: position` (and other abspos-only
		// CB triggers) makes this flex container a CB for abs-pos descendants
		// but not for fixed. Mirrors block_layout's isWillChangeAbsposCB.
		isWillChangeAbsposCB := false
		if fla.style != nil && !isPositioned {
			if fla.style.WillChangeEstablishesContainingBlock(false) {
				isWillChangeAbsposCB = true
			}
		}
		if isPositioned || isWillChangeAbsposCB {
			var absoluteCandidates, fixedCandidates []OutOfFlowCandidate
			for _, cand := range builder.outOfFlowCandidates {
				if cand.IsFixedPosition {
					fixedCandidates = append(fixedCandidates, cand)
				} else {
					absoluteCandidates = append(absoluteCandidates, cand)
				}
			}
			if len(absoluteCandidates) > 0 {
				// Per CSS 2.1 §10.3.7 / Blink's GetContainingBlockInfo():
				// CB size = padding-box = content + padding (borders excluded).
				oofPart := &OutOfFlowLayoutPart{
					ctx:                fla.ctx,
					containingBlockWDM: wdm,
					containingBlockSize: LogicalSize{
						InlineSize: contentInlineSize + geom.Padding.InlineStart + geom.Padding.InlineEnd,
						BlockSize:  finalBlockSize + geom.Padding.BlockStart + geom.Padding.BlockEnd,
					},
					containingBlockPadding: geom.Padding,
					geom:                   geom,
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

	// CSS position:relative — "start wins over end" in logical coordinates.
	// Sticky is scroll-time, not layout-time; leave RelativeOffset zero here.
	if fla.style != nil && fla.style.GetPosition() == css.PositionRelative {
		cbWidth := fla.space.AvailableSize.InlineSize.Float64()
		cbHeight := fla.space.AvailableSize.BlockSize.Float64()
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
// buildFlexChildren implements CSS Flexbox §4 anonymous flex item wrapping.
// Each contiguous run of text nodes (with display:none elements being transparent)
// is grouped into a single anonymous block flex item so that inter-word spaces
// are preserved. OOF children (position:absolute/fixed) interrupt text runs.
// This is a standalone function so both the full layout path and intrinsic
// sizing (measureFlexMinMax) can share the same wrapping logic.
func buildFlexChildren(node *LayoutInputNode, parentStyle *css.Style) []*LayoutInputNode {
	var result []*LayoutInputNode
	var textRun []*LayoutInputNode // accumulates text nodes for current run

	flushTextRun := func() {
		if len(textRun) == 0 {
			return
		}
		// Drop whitespace-only runs per CSS Flexbox §4: "A sequence of child
		// text content that contains only white space (i.e., characters that
		// can be affected by the white-space property) is not rendered."
		// Only CSS-collapsible whitespace (U+0020 space, U+0009 tab,
		// U+000A LF, U+000D CR) is dropped. Non-breaking space (U+00A0)
		// is NOT collapsible and must create an anonymous flex item.
		hasContent := false
		for _, n := range textRun {
			if n.IsText() {
				if !isCSSWhitespaceOnly(n.TextContent()) {
					hasContent = true
					break
				}
			} else {
				// Inline element (e.g. <br>) is always renderable content.
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
		// OOF children interrupt text runs (matches Blink behaviour).
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			flushTextRun()
			result = append(result, child)
			continue
		}
		// <br> joins the surrounding text run. In Blink, LayoutBR is a
		// LayoutText subclass, so the "contiguous run of text" wrapping
		// rule (CSS Flexbox 1 §4) groups it with adjacent text into a
		// single anonymous block-level flex item. Other inline-level
		// elements (e.g. <i>, <span>) are blockified per CSS Display 3
		// §2.4 and become their own flex items.
		if child.DOMNode != nil && child.DOMNode.TagName == "br" {
			textRun = append(textRun, child)
			continue
		}
		// Visible in-flow element: flush any pending text run, then add element.
		flushTextRun()
		result = append(result, child)
	}
	flushTextRun()
	return result
}

// buildFlexChildList wraps the standalone buildFlexChildren for the algorithm.
func (fla *FlexLayoutAlgorithm) buildFlexChildList() []*LayoutInputNode {
	return buildFlexChildren(fla.node, fla.style)
}

// isCSSWhitespaceOnly returns true if s contains only CSS-collapsible whitespace
// characters (space U+0020, tab U+0009, LF U+000A, CR U+000D). Non-breaking
// space (U+00A0) and other Unicode spaces are NOT CSS-collapsible.
func isCSSWhitespaceOnly(s string) bool {
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return false
		}
	}
	return true
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
			// Mirrors Blink's LayoutFlexibleBox::PrepareChildForPositionedLayout:
			// the static position of an OOF flex-child is where it would have
			// been placed if in-flow, derived from the container's
			// justify-content (main) and align-items (cross) values. The OOF
			// resolver reads the edge annotation and drives IMCB center-
			// clipping / start-end pinning off of it.
			mainEdge, mainOff := flexOOFStaticMain(
				fla.getJustifyContent(), containerMainSize, hasDefiniteMain)
			crossEdge, crossOff := flexOOFStaticCross(
				fla.getAlignItems(), containerCrossSize, hasDefiniteCross)
			var inlineEdge, blockEdge StaticPositionEdge
			var inlineOff, blockOff float64
			if isRow {
				inlineEdge, inlineOff = mainEdge, mainOff
				blockEdge, blockOff = crossEdge, crossOff
			} else {
				blockEdge, blockOff = mainEdge, mainOff
				inlineEdge, inlineOff = crossEdge, crossOff
			}
			builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
				Node: child,
				StaticPosition: LogicalStaticPosition{
					Offset:     LogicalOffset{InlineOffset: inlineOff, BlockOffset: blockOff},
					InlineEdge: inlineEdge,
					BlockEdge:  blockEdge,
				},
				IsFixedPosition: pos == css.PositionFixed,
			})
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)
		childGeom := ComputeFragmentGeometry(childStyle, childWDM, contentInlineSize)

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
			itemSizingSpace, flexBasis, isRow, containerCrossSize, hasDefiniteCross, contentInlineSize,
			containerMainSize, hasDefiniteMain)

		// Per CSS Flexbox §9.2 step 4: "The hypothetical main size is the item's
		// flex base size clamped according to its used min and max main sizes
		// (and flooring the content box size at zero)." The "used min" includes the
		// §4.5 content-based automatic minimum, so clamp by the full minMainSize.
		// This makes inflexible items (flex-grow/shrink == 0) freeze at the right
		// size in §9.7 step 3, and makes wrap decisions respect auto-min properly.
		// Growing items (flex-basis:0, flex-grow>0) are unaffected because §9.7
		// step 1 starts target main size at flex-base, not hypothetical.
		hyp := fla.clampMainSizeWithMin(flexBasis, minMainSize, childStyle, childWDM, childGeom,
			itemSizingSpace, isRow)

		// Compute CSS max main size for §9.7 freeze loop.
		// Must dispatch to the correct axis function based on mainIsItemInline,
		// since for orthogonal items the flex main axis may be the item's block axis.
		mainIsItemInlineForMax := computeMainIsItemInline(wdm, childWDM, isRow)
		maxMainSize := Indefinite
		if mainIsItemInlineForMax {
			if max, ok := ResolveMaxInlineSize(childStyle, childWDM, itemSizingSpace, childGeom); ok {
				maxMainSize = max.Float64()
			}
		} else {
			if max, ok := ResolveMaxBlockSize(childStyle, childWDM, itemSizingSpace, childGeom); ok {
				maxMainSize = max.Float64()
			}
		}
		// Handle intrinsic keywords for max main size (e.g., max-height: min-content).
		if maxMainSize == Indefinite {
			maxMainSize = fla.resolveIntrinsicMaxMain(child, childStyle, childWDM, childGeom,
				itemSizingSpace, isRow, mainIsItemInlineForMax, contentInlineSize)
		}

		// Clamp the hypothetical main size by the intrinsic max main size
		// (which may have been resolved from min-content, max-content keywords).
		// clampMainSizeWithMin only handles length/percentage max values, so
		// intrinsic keywords need this additional clamp.
		if maxMainSize != Indefinite && hyp > maxMainSize {
			hyp = maxMainSize
		}

		// Detect auto margins for §8.1 alignment.
		itemMainIsInline := computeMainIsItemInline(wdm, childWDM, isRow)
		// Auto margins are resolved in the container's WDM (same as margins above).
		mainAS, mainAE, crossAS, crossAE := getItemAutoMargins(childStyle, wdm, isRow)

		item := &flexItem{
			node:             child,
			style:            childStyle,
			wdm:              childWDM,
			geom:             childGeom,
			margins:          childMargins,
			mainIsItemInline: itemMainIsInline,
			flexBasis:        flexBasis,
			hypothetical:     hyp,
			flexGrow:         flexGrow,
			flexShrink:       flexShrink,
			order:            order,
			isRow:            isRow,
			minMain:          minMainSize,
			maxMain:          maxMainSize,
			mainAutoStart:    mainAS,
			mainAutoEnd:      mainAE,
			crossAutoStart:   crossAS,
			crossAutoEnd:     crossAE,
			collapsed:        childStyle.GetVisibility() == "collapse",
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

// crossConstraintSpace builds the constraint space used to resolve an item's
// cross-axis CSS sizes (and their percentages) during flex-basis / max-content
// main-size computation. When the main axis is the item's inline axis the cross
// axis is its block axis, so a definite container cross-size becomes the
// percentage-resolution AND available block-size (CSS Flexbox §9.8); otherwise
// the block axis stays Indefinite (the -1 sentinel) so a percentage cross-size
// like `height: 100%` resolves to auto rather than to the float64 zero value.
func (fla *FlexLayoutAlgorithm) crossConstraintSpace(
	parentWDM, childWDM WritingDirectionMode,
	contentInlineSize, containerCrossSize float64,
	mainIsItemInline, hasDefiniteCross bool,
) ConstraintSpace {
	crossBlock := float64(Indefinite)
	if mainIsItemInline && hasDefiniteCross {
		crossBlock = containerCrossSize
	}
	return NewConstraintSpaceBuilder(parentWDM, childWDM, false).
		SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: crossBlock}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: crossBlock}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		Build()
}

// itemEffectiveAspectRatio returns the item's logical aspect ratio (inline /
// block) from its CSS `aspect-ratio` property, or, when that is unset and the
// item is a replaced element, from its intrinsic dimensions. The returned
// ratio is already adjusted for the item's writing mode. IsSet is false when
// the item has no usable ratio in either source. This is the single predicate
// the §9.2 transferred-size fallback keys on, so its callers stay in agreement
// about which items participate in aspect-ratio main-size transfer.
func (fla *FlexLayoutAlgorithm) itemEffectiveAspectRatio(
	child *LayoutInputNode, style *css.Style, childWDM WritingDirectionMode,
) css.AspectRatio {
	ar := style.GetAspectRatio()
	if !ar.IsSet && child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
		info := GetIntrinsicSizingInfo(fla.ctx, child)
		if info.HasAspectRatio && info.AspectRatio > 0 {
			ar = css.AspectRatio{IsSet: true, Width: info.AspectRatio, Height: 1}
			if childWDM.IsVertical() {
				ar = css.AspectRatio{IsSet: true, Width: 1, Height: info.AspectRatio}
			}
		}
	}
	return ar
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
			} else if fla.itemEffectiveAspectRatio(child, style, childWDM).IsSet {
				// Aspect-ratio item, indefinite container main: keep the percentage
				// base Indefinite (the -1 sentinel), NOT a definite 0.
				// IsBlockSizeIndefinite() checks PercentageResolutionSize.BlockSize < 0,
				// so a definite 0 would make a main-axis percentage like `height: 100%`
				// resolve to 0 (ok=true) and short-circuit the §9.2 transferred-size
				// fallback below, collapsing the item to 0 in the main axis. Per CSS
				// Flexbox §9.2 / Blink's resolve_main_length() ("we weren't able to
				// resolve the length … fallback to the max-content size";
				// flex_layout_algorithm.cc @ Chromium main
				// 3da458a33174a6422f35b4e603f4090f028670ae), an unresolvable percentage
				// main-size must fall through to the content/transferred size — the
				// item's definite cross-size carried through its ratio. Gating on the
				// same effective-aspect-ratio predicate the §9.2 fallback keys on keeps
				// ratio-free items (e.g. plain column `height:100%` divs) on the legacy
				// 0 base, where the percentage correctly resolves to 0.
				pctBlockSize = float64(Indefinite)
			}
			itemSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
				SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: availBlockSize}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: pctBlockSize}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			if mainIsItemInline {
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					return explicit.Float64()
				}
			} else {
				if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
					return explicit.Float64()
				}
			}
			// §9.2 aspect-ratio fallback: when the item has an aspect ratio
			// (CSS or intrinsic) and a definite cross-size, derive the flex basis
			// (main-size) from the cross-size × ratio.
			//
			// For replaced elements, only use EXPLICIT cross-size (CSS property),
			// not stretch-predicted cross-size. Replaced elements' natural content
			// dimensions should be used when no explicit cross-size forces a
			// different proportion. Stretch prediction gives wrong results when the
			// stretch cross-size differs from the intrinsic cross-size.
			ar := fla.itemEffectiveAspectRatio(child, style, childWDM)
			isReplaced := child.DOMNode != nil && IsReplacedElement(child.DOMNode)
			if ar.IsSet {
				var itemCrossContent float64
				var hasItemCross bool
				// Per CSS Flexbox §9.8 + CSS Sizing 3 §5.1: when the container's
				// cross-size is definite, the item's percentage cross-size
				// resolves against the container's content cross-size. Without
				// this basis, `height: 50%` would resolve to 0, suppressing the
				// aspect-ratio transfer that derives the main-size.
				crossItemSpace := fla.crossConstraintSpace(parentWDM, childWDM,
					contentInlineSize, containerCrossSize, mainIsItemInline, hasDefiniteCross)
				if mainIsItemInline {
					// Cross = block axis. Check for explicit CSS block-size.
					if explicitCross, ok := ResolveBlockSize(style, childWDM, crossItemSpace, childGeom); ok {
						itemCrossContent = explicitCross.Float64()
						hasItemCross = true
					}
				} else {
					// Cross = inline axis. Check for explicit CSS inline-size.
					if explicitCross, ok := ResolveInlineSize(style, childWDM, crossItemSpace, childGeom); ok {
						itemCrossContent = explicitCross.Float64()
						hasItemCross = true
					}
				}
				// If no explicit cross-size, check if the item stretches to container.
				// Skip stretch prediction for replaced elements — their intrinsic
				// content dimensions should be used when no explicit cross-size
				// forces a different proportion via aspect ratio.
				if !hasItemCross && hasDefiniteCross && !isReplaced {
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
				// Clamp cross-size by min/max constraints before transferring.
				if hasItemCross {
					if mainIsItemInline {
						minCross := ResolveMinBlockSize(style, childWDM, crossItemSpace, childGeom).Float64()
						if itemCrossContent < minCross {
							itemCrossContent = minCross
						}
						if maxCrossLU, ok := ResolveMaxBlockSize(style, childWDM, crossItemSpace, childGeom); ok {
							maxCross := maxCrossLU.Float64()
							if itemCrossContent > maxCross {
								itemCrossContent = maxCross
							}
						}
					} else {
						minCross := ResolveMinInlineSize(style, childWDM, crossItemSpace, childGeom).Float64()
						if itemCrossContent < minCross {
							itemCrossContent = minCross
						}
						if maxCrossLU, ok := ResolveMaxInlineSize(style, childWDM, crossItemSpace, childGeom); ok {
							maxCross := maxCrossLU.Float64()
							if itemCrossContent > maxCross {
								itemCrossContent = maxCross
							}
						}
					}
					if mainIsItemInline && ar.Height > 0 {
						return itemCrossContent * ar.Width / ar.Height
					} else if !mainIsItemInline && ar.Width > 0 {
						return itemCrossContent * ar.Height / ar.Width
					}
				}
			}
			return fla.itemMaxContentMainSize(child, style, childWDM, childGeom, parentWDM,
				contentInlineSize, isRow, containerCrossSize, hasDefiniteCross)
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
				return explicit.Float64()
			}
		} else {
			if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
				return explicit.Float64()
			}
		}
		// §9.2 Part B: aspect-ratio fallback when the item has an aspect ratio
		// (CSS or intrinsic) and a definite cross-size. For replaced elements,
		// only use explicit cross-size, not stretch-predicted cross-size.
		var arW, arH float64
		var hasAR bool
		if ar := style.GetAspectRatio(); ar.IsSet {
			arW, arH, hasAR = ar.Width, ar.Height, true
		} else if child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
			info := GetIntrinsicSizingInfo(fla.ctx, child)
			if info.HasAspectRatio && info.AspectRatio > 0 {
				if childWDM.IsVertical() {
					arW, arH, hasAR = info.IntrinsicHeight, info.IntrinsicWidth, true
				} else {
					arW, arH, hasAR = info.IntrinsicWidth, info.IntrinsicHeight, true
				}
			}
		}
		isReplacedB := child.DOMNode != nil && IsReplacedElement(child.DOMNode)
		if hasAR {
			// Determine the item's definite cross-size content value.
			// Priority: 1) explicit CSS cross-size (clamped by min/max), 2) stretched container cross-size.
			var itemCrossContent float64
			var hasItemCross bool
			if mainIsItemInline {
				// Cross = block axis.
				if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
					itemCrossContent = explicit.Float64()
					// Clamp by min/max block size.
					minBlock := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom).Float64()
					if itemCrossContent < minBlock {
						itemCrossContent = minBlock
					}
					if maxBlockLU, hasMax := ResolveMaxBlockSize(style, childWDM, itemSpace, childGeom); hasMax && itemCrossContent > maxBlockLU.Float64() {
						itemCrossContent = maxBlockLU.Float64()
					}
					hasItemCross = true
				}
			} else {
				// Cross = inline axis.
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					itemCrossContent = explicit.Float64()
					// Clamp by min/max inline size.
					minInline := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom).Float64()
					if itemCrossContent < minInline {
						itemCrossContent = minInline
					}
					if maxInlineLU, hasMax := ResolveMaxInlineSize(style, childWDM, itemSpace, childGeom); hasMax && itemCrossContent > maxInlineLU.Float64() {
						itemCrossContent = maxInlineLU.Float64()
					}
					hasItemCross = true
				}
			}
			// If no explicit cross-size, check stretch alignment.
			// Skip stretch prediction for replaced elements.
			if !hasItemCross && hasDefiniteCross && !isReplacedB {
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
			contentInlineSize, isRow, containerCrossSize, hasDefiniteCross)
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
			result := layoutunit.ResolvePercent(
				layoutunit.FromFloat64Round(contentInlineSize), pct).Float64()
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
		if pct, ok := style.GetPercentage("flex-basis"); ok {
			if hasDefiniteMain {
				result := layoutunit.ResolvePercent(
					layoutunit.FromFloat64Round(containerMainSize), pct).Float64()
				if style.GetBoxSizing() == "border-box" {
					result -= childGeom.BlockBorderPadding()
					if result < 0 {
						result = 0
					}
				}
				return result
			}
			// Per CSS Flexbox §7.2: a percentage main-size on a flex item
			// whose container's main size is indefinite is treated as
			// 'content'. Fall through to content-based sizing (which applies
			// aspect-ratio cross→main transfer for replaced elements).
			return fla.itemContentMaxMainSize(child, style, childWDM, childGeom, parentWDM,
				contentInlineSize, isRow)
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
		contentInlineSize, isRow, containerCrossSize, hasDefiniteCross)
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
	// For replaced elements in the inline axis, return intrinsic inline-size
	// directly. For the block axis, use ComputeReplacedSize with the CSS
	// block-size suppressed (flex-basis:content ignores the main-size) —
	// this correctly derives block-size from cross-size × AR when the item
	// has an explicit CSS cross-size.
	mainIsItemInline := computeMainIsItemInline(parentWDM, childWDM, isRow)
	if child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
		info := GetIntrinsicSizingInfo(fla.ctx, child)
		// Logical aspect ratio: inline/block.
		var logicalRatio float64
		if info.HasAspectRatio && info.AspectRatio > 0 {
			if childWDM.IsVertical() {
				logicalRatio = 1.0 / info.AspectRatio
			} else {
				logicalRatio = info.AspectRatio
			}
		}
		if mainIsItemInline {
			// Inline axis: check for explicit CSS cross-size (block-size).
			// If the item has an explicit cross-size and aspect ratio,
			// the content inline-size is derived from cross × ratio
			// (CSS 2.1 §10.3.2: auto width with definite height and ratio).
			// flex-basis:content suppresses the main-size (width) but the
			// cross-size (height) still applies.
			crossSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
				SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			if crossSize, hasCross := ResolveBlockSize(style, childWDM, crossSpace, childGeom); hasCross && logicalRatio > 0 {
				return crossSize.Float64() * logicalRatio
			}
			// No explicit cross-size or no AR: use intrinsic inline-size.
			if childWDM.IsVertical() {
				return info.IntrinsicHeight
			}
			return info.IntrinsicWidth
		}
		// Block axis: resolve CSS cross-size (inline-size) and derive
		// block-size via aspect ratio, suppressing any CSS block-size.
		// This mirrors ComputeReplacedSize §10.6.2 with height:auto.
		availInline := contentInlineSize
		if !parentWDM.IsOrthogonalTo(childWDM) {
			margins := ResolveMargins(style, childWDM, contentInlineSize)
			availInline -= margins.InlineStart + margins.InlineEnd
			if availInline < 0 {
				availInline = 0
			}
		}
		crossSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
			SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		// Check for explicit CSS cross-size (inline-size).
		crossSizeLU, hasCross := ResolveInlineSize(style, childWDM, crossSpace, childGeom)
		if !hasCross {
			// No explicit CSS inline-size; use intrinsic block dimension.
			if childWDM.IsVertical() {
				return info.IntrinsicWidth
			}
			return info.IntrinsicHeight
		}
		// CSS cross-size is set. Derive block-size via intrinsic AR.
		if logicalRatio > 0 {
			return crossSizeLU.Float64() / logicalRatio
		}
		// No AR — use intrinsic block dimension.
		if childWDM.IsVertical() {
			return info.IntrinsicWidth
		}
		return info.IntrinsicHeight
	}
	if mainIsItemInline {
		// Main axis = item's inline axis. Use computeContentMinMaxSizes which
		// ignores the item's explicit CSS inline-size.
		parentBlockSize := Indefinite
		if parentWDM.IsOrthogonalTo(childWDM) {
			parentBlockSize = fla.space.AvailableSize.BlockSize.Float64()
		}
		space := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
			SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: parentBlockSize}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		mm := computeContentMinMaxSizes(fla.ctx, child, space)
		return mm.MaxContent
	}
	// Main axis = item's block axis. Lay out with indefinite block-size and
	// IsContentSuggestionLayout to suppress its explicit CSS block-size.
	//
	// CSS Sizing 3 §5.1: The max-content block size is the block size when
	// the box is laid out at its max-content inline size. For column flex
	// items without explicit cross-size, this means using the item's
	// shrink-to-fit (fit-content) width, NOT the container's cross-axis size.
	// This ensures percentage padding on descendants resolves against the
	// item's actual width, not the container width.
	availInline := contentInlineSize
	if !parentWDM.IsOrthogonalTo(childWDM) {
		margins := ResolveMargins(style, childWDM, contentInlineSize)
		availInline -= margins.InlineStart + margins.InlineEnd
		if availInline < 0 {
			availInline = 0
		}
	}
	// If the item has no explicit inline-size (cross-size in column flex)
	// and won't stretch, compute its max-content inline-size and use that
	// for the layout. This gives the correct shrink-to-fit width for
	// percentage resolution.
	alignItems := "stretch"
	if v, ok := fla.style.Get("align-items"); ok {
		alignItems = strings.TrimSpace(v)
	}
	selfAlign := fla.getAlignSelf(style, alignItems)
	_, _, crossAS, crossAE := getItemAutoMargins(style, childWDM, isRow)
	willStretch := selfAlign == "stretch" && !crossAS && !crossAE
	if !willStretch {
		if _, hasExplicitInline := ResolveInlineSize(style, childWDM,
			NewConstraintSpaceBuilder(parentWDM, childWDM, false).
				SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build(), childGeom); !hasExplicitInline {
			mmSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
				SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
				SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			mm := computeContentMinMaxSizes(fla.ctx, child, mmSpace)
			// computeContentMinMaxSizes returns content-box sizes.
			// AvailableSize.InlineSize is border-box, so add border+padding.
			fitContentInline := mm.MaxContent + childGeom.InlineBorderPadding()
			if fitContentInline > availInline {
				fitContentInline = availInline
			}
			availInline = fitContentInline
		}
	}
	parentBlockSize := Indefinite
	if parentWDM.IsOrthogonalTo(childWDM) {
		parentBlockSize = fla.space.AvailableSize.BlockSize.Float64()
	}
	space := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
		SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
		SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: parentBlockSize}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
		SetPercentageResolutionInlineSize(contentInlineSize).
		SetIsContentSuggestionLayout(true).
		Build()
	result := layoutElement(fla.ctx, child, space)
	lf := NewLogicalFragment(childWDM, result.Fragment)
	return lf.BlockSize() - childGeom.BlockBorderPadding()
}

// itemMaxContentMainSize returns the max-content size in the main axis.
// For replaced elements, returns intrinsic dimensions directly to avoid
// CSS min/max clamping — the flex algorithm applies min/max separately
// when computing the hypothetical main size (§9.2 step 3).
func (fla *FlexLayoutAlgorithm) itemMaxContentMainSize(
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	parentWDM WritingDirectionMode,
	contentInlineSize float64,
	isRow bool,
	containerCrossSize float64,
	hasDefiniteCross bool,
) float64 {
	// Replaced elements: compute max-content main size with CROSS-axis
	// min/max constraints (which affect sizing via aspect ratio) but
	// WITHOUT main-axis min/max constraints. The flex algorithm applies
	// main-axis min/max separately in the hypothetical/resolve steps.
	// A full layout (below for non-replaced) applies ALL min/max
	// constraints, which is incorrect for the flex base size (§9.2).
	if child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
		info := GetIntrinsicSizingInfo(fla.ctx, child)
		mainIsItemInline := computeMainIsItemInline(parentWDM, childWDM, isRow)

		// Convert physical intrinsic dimensions to logical.
		var intrinsicInline, intrinsicBlock float64
		if childWDM.IsVertical() {
			intrinsicInline = info.IntrinsicHeight
			intrinsicBlock = info.IntrinsicWidth
		} else {
			intrinsicInline = info.IntrinsicWidth
			intrinsicBlock = info.IntrinsicHeight
		}
		logicalRatio := 0.0
		if info.HasAspectRatio && info.AspectRatio > 0 {
			if childWDM.IsVertical() {
				logicalRatio = 1.0 / info.AspectRatio
			} else {
				logicalRatio = info.AspectRatio
			}
		}

		// Determine base inline and block sizes (same as ComputeReplacedSize default case).
		inlineSize := intrinsicInline
		if inlineSize <= 0 {
			if logicalRatio > 0 && intrinsicBlock > 0 {
				inlineSize = intrinsicBlock * logicalRatio
			} else {
				inlineSize = 300
			}
		}
		blockSize := intrinsicBlock
		if blockSize <= 0 {
			if logicalRatio > 0 && inlineSize > 0 {
				blockSize = inlineSize / logicalRatio
			} else {
				blockSize = 150
			}
		}

		// Apply ONLY cross-axis min/max constraints, re-deriving via aspect ratio.
		// A definite container cross-size becomes the percentage-resolution block
		// base so a percentage cross constraint (e.g. max-height:100%) resolves to
		// it rather than to 0 — the block-axis max then transfers through the
		// aspect ratio to bound the main (inline) size, per Blink's
		// ComputeTransferredMinMaxBlockSizes (flex_layout_algorithm.cc @ Chromium
		// main 3da458a33174a6422f35b4e603f4090f028670ae). See crossConstraintSpace.
		crossItemSpace := fla.crossConstraintSpace(parentWDM, childWDM,
			contentInlineSize, containerCrossSize, mainIsItemInline, hasDefiniteCross)
		if mainIsItemInline {
			// Main=inline, cross=block. Apply block min/max only.
			minBlock := ResolveMinBlockSize(style, childWDM, crossItemSpace, childGeom).Float64()
			if blockSize < minBlock {
				blockSize = minBlock
				if logicalRatio > 0 {
					inlineSize = blockSize * logicalRatio
				}
			}
			if maxBlockLU, ok := ResolveMaxBlockSize(style, childWDM, crossItemSpace, childGeom); ok {
				maxBlock := maxBlockLU.Float64()
				if blockSize > maxBlock {
					blockSize = maxBlock
					if logicalRatio > 0 {
						inlineSize = blockSize * logicalRatio
					}
				}
			}
			return inlineSize
		}
		// Main=block, cross=inline. Apply inline min/max only.
		minInline := ResolveMinInlineSize(style, childWDM, crossItemSpace, childGeom).Float64()
		if inlineSize < minInline {
			inlineSize = minInline
			if logicalRatio > 0 {
				blockSize = inlineSize / logicalRatio
			}
		}
		if maxInlineLU, ok := ResolveMaxInlineSize(style, childWDM, crossItemSpace, childGeom); ok {
			maxInline := maxInlineLU.Float64()
			if inlineSize > maxInline {
				inlineSize = maxInline
				if logicalRatio > 0 {
					blockSize = inlineSize / logicalRatio
				}
			}
		}
		return blockSize
	}
	// When main = item's block axis, cross-axis margins (inline margins)
	// reduce available inline space for layout.
	mainIsItemInline := computeMainIsItemInline(parentWDM, childWDM, isRow)
	availInline := contentInlineSize
	if !mainIsItemInline && !parentWDM.IsOrthogonalTo(childWDM) {
		margins := ResolveMargins(style, childWDM, contentInlineSize)
		availInline -= margins.InlineStart + margins.InlineEnd
		if availInline < 0 {
			availInline = 0
		}
	}
	// For orthogonal items, pass the container's block-size so the builder can
	// set the child's available inline-size via axis swapping.
	parentBlockSize := Indefinite
	if parentWDM.IsOrthogonalTo(childWDM) {
		parentBlockSize = fla.space.AvailableSize.BlockSize.Float64()
	}
	// CSS Flexbox §9.8: If a single-line flex container has a definite cross
	// size, the automatic preferred outer cross size of any stretched flex items
	// is the flex container's inner cross size and is considered definite.
	// For orthogonal items, the container's cross-size maps to the item's
	// inline-size (via axis swap). Use the definite cross-size so percentage
	// padding/sizing in descendants resolves against the stretched size during
	// flex basis computation. This matches the spec assertion: "Item's stretched
	// size is used for laying out descendants when determining flex base size."
	stretchedCrossForBasis := Indefinite
	if hasDefiniteCross {
		alignItems := "stretch"
		if v, ok := fla.style.Get("align-items"); ok {
			alignItems = strings.TrimSpace(v)
		}
		selfAlign := fla.getAlignSelf(style, alignItems)
		_, _, crossAS, crossAE := getItemAutoMargins(style, childWDM, isRow)
		hasExplCross := fla.hasExplicitCrossSize(style, parentWDM, isRow)
		if selfAlign == "stretch" && !crossAS && !crossAE && !hasExplCross {
			stretchedCrossForBasis = containerCrossSize
			if parentWDM.IsOrthogonalTo(childWDM) {
				parentBlockSize = containerCrossSize
			}
		}
	}
	if !mainIsItemInline {
		// CSS Sizing 3 §5.1: The max-content block size is the block size
		// when the box is laid out at its max-content inline size. For column
		// flex items without explicit inline-size that won't stretch, compute
		// shrink-to-fit width so that percentage padding on descendants
		// resolves against the item's actual width rather than the container's
		// cross-axis size.
		alignItems := "stretch"
		if v, ok := fla.style.Get("align-items"); ok {
			alignItems = strings.TrimSpace(v)
		}
		selfAlign := fla.getAlignSelf(style, alignItems)
		_, _, crossAS, crossAE := getItemAutoMargins(style, childWDM, isRow)
		willStretch := selfAlign == "stretch" && !crossAS && !crossAE
		if !willStretch {
			if _, hasExplicitInline := ResolveInlineSize(style, childWDM,
				NewConstraintSpaceBuilder(parentWDM, childWDM, false).
					SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: Indefinite}).
					SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
					SetPercentageResolutionInlineSize(contentInlineSize).
					Build(), childGeom); !hasExplicitInline {
				mmSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
					SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
					SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: Indefinite}).
					SetPercentageResolutionSize(LogicalSize{InlineSize: availInline}).
					SetPercentageResolutionInlineSize(contentInlineSize).
					Build()
				mm := computeContentMinMaxSizes(fla.ctx, child, mmSpace)
				// computeContentMinMaxSizes returns content-box sizes.
				// AvailableSize.InlineSize is border-box, so add border+padding.
				fitContentInline := mm.MaxContent + childGeom.InlineBorderPadding()
				if fitContentInline > availInline {
					fitContentInline = availInline
				}
				availInline = fitContentInline
			}
		}
	}
	// CSS Flexbox §9.8: When the item will be stretched, its cross-size is
	// treated as definite. Pass it as PercentageResolutionSize.BlockSize so
	// descendants with percentage heights can resolve against it.
	//
	// Per CSS Sizing 3 §5.1 + Blink's BuildSpaceForFlexBasis at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f: when the container's cross
	// size is definite, the container's cross-size also serves as the
	// percentage resolution basis for any item that has an explicit
	// percentage cross-size (so the item's own `height: 50%` can resolve to
	// a definite value, and `measureBlockMinMax` then propagates that
	// resolved cross-size to descendants). Mirror Blink's
	// FlexLayoutAlgorithm::BuildSpaceForFlexBasis.
	pctBlock := float64(0)
	if stretchedCrossForBasis != Indefinite {
		pctBlock = stretchedCrossForBasis
	} else if hasDefiniteCross && fla.hasExplicitPercentCrossSize(style, parentWDM, isRow) {
		pctBlock = containerCrossSize
	}
	csb := NewConstraintSpaceBuilder(parentWDM, childWDM, true).
		SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
		SetAvailableSize(LogicalSize{InlineSize: availInline, BlockSize: parentBlockSize}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: availInline, BlockSize: pctBlock}).
		SetPercentageResolutionInlineSize(contentInlineSize)
	// CSS Flexbox §9.8: When the stretched cross-size is definite for flex
	// basis computation, fix the item's cross dimension in the constraint
	// space. For orthogonal items this swaps to IsFixedInlineSize, ensuring
	// the item uses the definite stretched size instead of shrink-to-fit.
	if stretchedCrossForBasis != Indefinite {
		csb.SetIsFixedBlockSize(true)
	}
	space := csb.Build()
	if mainIsItemInline {
		mm := ComputeMinMaxSizes(fla.ctx, child, space)
		return mm.MaxContent
	}
	// Main = item's block axis: lay out to get intrinsic block-size.
	result := layoutElement(fla.ctx, child, space)
	lf := NewLogicalFragment(childWDM, result.Fragment)
	return lf.BlockSize() - childGeom.BlockBorderPadding()
}

// resolveIntrinsicMaxMain resolves intrinsic keywords (min-content, max-content,
// fit-content) for the max main size property (max-width / max-height).
// Returns Indefinite if the property is not an intrinsic keyword.
func (fla *FlexLayoutAlgorithm) resolveIntrinsicMaxMain(
	child *LayoutInputNode,
	style *css.Style,
	childWDM WritingDirectionMode,
	childGeom FragmentGeometry,
	space ConstraintSpace,
	isRow bool,
	mainIsItemInline bool,
	contentInlineSize float64,
) float64 {
	containerIsVertical := fla.space.WritingDirection.IsVertical()
	var maxProp string
	if isRow {
		if containerIsVertical {
			maxProp = "max-height"
		} else {
			maxProp = "max-width"
		}
	} else {
		if containerIsVertical {
			maxProp = "max-width"
		} else {
			maxProp = "max-height"
		}
	}
	val, ok := style.Get(maxProp)
	if !ok {
		return Indefinite
	}
	val = strings.TrimSpace(val)
	if val != "min-content" && val != "max-content" && val != "fit-content" {
		return Indefinite
	}

	if mainIsItemInline {
		childSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
			SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
			SetAvailableSize(geomLogicalToOld(space.AvailableSize)).
			SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
			Build()
		mm := ComputeMinMaxSizes(fla.ctx, child, childSpace)
		switch val {
		case "min-content":
			return mm.MinContent
		case "max-content", "fit-content":
			return mm.MaxContent
		}
	} else {
		// Block-axis intrinsic keyword: lay out the item to determine its content block-size.
		containerInlineSize := space.AvailableSize.InlineSize.Float64()
		minBlockSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
			SetAvailableSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: containerInlineSize}).
			SetPercentageResolutionInlineSize(containerInlineSize).
			SetIsContentSuggestionLayout(true).
			Build()
		result := layoutElement(fla.ctx, child, minBlockSpace)
		blockContent := result.IntrinsicBlockSize
		if blockContent < 0 {
			blockContent = 0
		}
		return blockContent
	}
	return Indefinite
}

// clampMainSizeWithMin clamps a size by a given minimum and the CSS max main
// size constraint. Used to compute the hypothetical main size per CSS Flexbox
// §9.2 step 4. The caller passes the full §4.5 effective minimum (including the
// content-based automatic minimum when min-width/min-height is auto).
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
		if maxLU, ok := ResolveMaxInlineSize(style, childWDM, space, childGeom); ok {
			max := maxLU.Float64()
			if result > max {
				result = max
			}
		}
	} else {
		if maxLU, ok := ResolveMaxBlockSize(style, childWDM, space, childGeom); ok {
			max := maxLU.Float64()
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
	minMain := fla.flexItemMinMain(child, style, childWDM, childGeom, parentSpace, basis, isRow, 0, false, contentInlineSize, 0, false)
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
		// No free space: all items stay at their hypothetical main size,
		// but still clamp to their effective minimum (§4.5 automatic minimum).
		for _, item := range items {
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
		// §9.8: Use the actual cross-size for percentage block-size resolution when definite.
		// When crossSize is Indefinite (first pass), check if the item has an explicit
		// CSS cross-size (e.g., height: 100px). Per CSS 2.1 §10.5, percentage heights
		// resolve against the containing block's content height, which is the item's
		// explicit CSS height if set.
		// Determine the percentage-resolution block-size for descendants.
		// Per CSS Flexbox §4.5, percentage cross-sizes on a flex item resolve
		// against the container's content-box cross-size when definite. Use
		// fla.containerCrossSize as the available + percentage-resolution
		// block-size for both the item's own % height resolution (itemSpace)
		// and for the cs passed to the child's layout.
		// When no definite base exists, the percentage base stays Indefinite
		// (NOT a definite 0) so percent (max-)heights are ignored in the
		// measure pass, per Blink's BuildSpaceForLayout which only sets the
		// percentage block-size from a known line_cross_size or a definite
		// container cross-size.
		pctBlockSize := float64(Indefinite)
		itemSpaceBlock := float64(Indefinite)
		if fla.hasDefiniteCross {
			pctBlockSize = fla.containerCrossSize
			itemSpaceBlock = fla.containerCrossSize
		}
		itemSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: itemSpaceBlock}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: pctBlockSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		if explicit, ok := ResolveBlockSize(item.style, childWDM, itemSpace, item.geom); ok && crossSize == Indefinite {
			// First pass: set available block-size so IsBlockSizeIndefinite()
			// returns false and inner percentage heights can resolve.
			avail.BlockSize = explicit.Float64() + item.crossBorderPadding()
		} else if crossSize != Indefinite && !fla.hasDefiniteCross {
			// Fallback: use the line's cross-size as the percentage base when
			// the container's cross isn't definite.
			pctBlockSize = crossSize
		} else if crossSize == Indefinite && fla.hasDefiniteCross {
			// No explicit height and no line cross yet: make avail definite so
			// calc(%) in the child's own geometry computation can resolve.
			avail.BlockSize = fla.containerCrossSize + item.crossBorderPadding()
		}
		b.SetAvailableSize(avail)
		b.SetPercentageResolutionSize(LogicalSize{
			InlineSize: mainSize,
			BlockSize:  pctBlockSize,
		})
		b.SetPercentageResolutionInlineSize(contentInlineSize)
		b.SetIsFixedInlineSize(true)
		if crossIsFixed && crossSize != Indefinite {
			b.SetIsFixedBlockSize(true)
		}
		// Per §9.5: the flex-resolved main size IS the used main size. For
		// orthogonal items in row flex, the parent's inline (main) axis maps
		// to the child's block axis; without marking the block-size as an
		// override, CalculateInitialFragmentGeometry would let the child's
		// CSS block-size (e.g. `width:33px` on a VLR canvas) take precedence
		// over the flex-resolved main. The override only fires when
		// IsFixedBlockSize is true, which is the case only for orthogonal
		// children here (IsFixedInlineSize → IsFixedBlockSize after swap).
		b.SetIsBlockSizeOverride(true)
	} else {
		// Main axis = parent block. Cross axis = parent inline.
		crossInlineContent := contentInlineSize
		if crossSize != Indefinite {
			crossInlineContent = crossSize
		}
		// For non-fixed cross items (wrapping column flex), subtract cross margins
		// from the available inline-size so fit-content sizing respects the margin box.
		// For column flex, available inline-size is the cross content size.
		// AvailableSize is always border-box per ConstraintSpace convention.
		// When IsFixedInlineSize is set, CalculateInitialFragmentGeometry uses
		// AvailableSize.InlineSize directly as the border-box inline-size.
		// crossInlineContent is content-box when coming from crossSize (stretch/line),
		// or the container's content-box inline-size otherwise. For fixed cross items,
		// convert content-box to border-box by adding the item's cross border+padding.
		availInline := crossInlineContent
		if crossIsFixed && crossSize != Indefinite {
			// crossInlineContent = crossSize (content-box). Convert to border-box.
			var crossBPInline float64
			if item.mainIsItemInline {
				crossBPInline = item.geom.BlockBorderPadding()
			} else {
				crossBPInline = item.geom.InlineBorderPadding()
			}
			availInline += crossBPInline
		}
		if !crossIsFixed {
			availInline -= item.crossMarginSum()
			if availInline < 0 {
				availInline = 0
			}
		}
		avail := LogicalSize{
			InlineSize: availInline,
			BlockSize:  mainSize + item.mainBorderPadding(),
		}
		b.SetAvailableSize(avail)
		// Percentage resolution: per CSS2.1 §10.2 and Flexbox §4, a flex item's
		// containing block is the flex container, so percentage inline-sizes
		// resolve against the container's content inline-size — NOT the flex
		// line's cross-size. For multi-line column flex, using the line cross
		// would make width:50% resolve against half the container.
		b.SetPercentageResolutionSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  mainSize,
		})
		b.SetPercentageResolutionInlineSize(contentInlineSize)
		b.SetIsFixedInlineSize(crossIsFixed)
		b.SetIsFixedBlockSize(true)
		// Per §9.5: the flex-resolved main size IS the used main size.
		// Override the item's CSS height so the layout uses the flex size.
		b.SetIsBlockSizeOverride(true)
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
	// - With "safe" overflow alignment, alignment falls back to the writing-mode
	//   start edge to prevent start-edge overflow (data loss prevention). For a
	//   reversed main axis (row-reverse/column-reverse), the writing-mode start
	//   is the flex-end, so the safe fallback is flex-end there.
	if freeSpace < 0 {
		switch justifyContent {
		case "space-between", "space-around", "space-evenly":
			justifyContent = "flex-start"
		case "center", "flex-end", "flex-start":
			if justifyContentSafe {
				if reverseMain {
					justifyContent = "flex-end"
				} else {
					justifyContent = "flex-start"
				}
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
		// Use containerMainSize directly — items that overflow will get negative
		// offsets, which is correct (overflow:hidden clips them).
		cursor := containerMainSize - initialOffset
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

// flexOOFStaticMain maps the container's resolved justify-content to a
// (StaticPositionEdge, offset-in-content-box) pair along the main axis.
// Only the "packing" values collapse to a single point; the distributed
// values (space-between / space-around / space-evenly) behave as start
// for a single item, matching Blink.
//
// If the main size is indefinite (column flex with auto block-size in an
// indefinite parent), the static offset falls back to (start, 0) — there is
// no meaningful center/end coordinate.
func flexOOFStaticMain(jc string, mainSize float64, hasDefiniteMain bool) (StaticPositionEdge, float64) {
	if !hasDefiniteMain {
		return StaticEdgeStart, 0
	}
	switch jc {
	case "center":
		return StaticEdgeCenter, mainSize / 2
	case "flex-end", "end", "right":
		return StaticEdgeEnd, mainSize
	default:
		return StaticEdgeStart, 0
	}
}

// flexOOFStaticCross maps the container's align-items to (edge, offset) on
// the cross axis. Mirrors the same rules as flexOOFStaticMain but reads
// align-items values. "stretch" (default) and "baseline" fall through to
// start — the OOF cannot stretch its own cross-size, so there's no coherent
// center/end point for those keywords.
func flexOOFStaticCross(ai string, crossSize float64, hasDefiniteCross bool) (StaticPositionEdge, float64) {
	if !hasDefiniteCross {
		return StaticEdgeStart, 0
	}
	switch ai {
	case "center":
		return StaticEdgeCenter, crossSize / 2
	case "flex-end", "end", "self-end", "last baseline":
		return StaticEdgeEnd, crossSize
	default:
		return StaticEdgeStart, 0
	}
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
		case "first baseline", "first-baseline":
			return "baseline"
		case "last-baseline":
			return "last baseline"
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
		// Per WPT flexbox-safe-overflow-position-006: "safe" overflow keyword
		// has no effect on legacy -webkit-box containers. When "safe" is present,
		// ignore the entire align-self value and fall back to the container's
		// align-items (from -webkit-box-align).
		if hasOverflowSafe(v) {
			if d, ok2 := fla.style.Get("display"); ok2 {
				d = strings.TrimSpace(d)
				if d == "-webkit-box" || d == "-webkit-inline-box" {
					return alignItems
				}
			}
		}
		v = stripOverflowKeyword(v)
		if v != "auto" && v != "" {
			switch v {
			case "stretch", "flex-start", "flex-end", "center", "baseline",
				"start", "end", "self-start", "self-end", "last baseline":
				return v
			case "first baseline", "first-baseline":
				return "baseline"
			case "last-baseline":
				return "last baseline"
			}
		}
	}
	return alignItems
}

// isAlignSelfSafe returns true if the effective align-self for an item has the
// "safe" overflow keyword. Per CSS Box Alignment §5.3, "safe" means if the
// aligned subject overflows the alignment container, it is aligned as if the
// alignment mode were "start" (i.e., offset clamped to 0).
// Per WPT flexbox-safe-overflow-position-006, "safe" has no effect on
// legacy -webkit-box containers.
func (fla *FlexLayoutAlgorithm) isAlignSelfSafe(style *css.Style) bool {
	// "safe" is not supported in legacy -webkit-box mode.
	if v, ok := fla.style.Get("display"); ok {
		v = strings.TrimSpace(v)
		if v == "-webkit-box" || v == "-webkit-inline-box" {
			return false
		}
	}
	if style != nil {
		if v, ok := style.Get("align-self"); ok {
			v = strings.TrimSpace(v)
			stripped := stripOverflowKeyword(v)
			if stripped != "auto" && stripped != "" {
				return hasOverflowSafe(v)
			}
		}
	}
	// Falls back to align-items.
	if v, ok := fla.style.Get("align-items"); ok {
		return hasOverflowSafe(v)
	}
	return false
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
func (fla *FlexLayoutAlgorithm) resolveGaps(wdm WritingDirectionMode, isRow bool, contentInlineSize float64, contentBlockSize float64, hasDefiniteBlock bool) (mainGap, crossGap float64) {
	// row-gap and column-gap.
	// Per CSS Align Level 3 §8.1 and CSSWG issue #5081:
	// - column-gap percentages resolve against the inline-size (always definite)
	// - row-gap percentages resolve against the block-size; if indefinite, resolve to 0
	var rowGap, colGap float64
	if v, ok := fla.style.GetLength("row-gap"); ok {
		rowGap = v
	} else if pct, ok := fla.style.GetPercentage("row-gap"); ok {
		if hasDefiniteBlock {
			rowGap = layoutunit.ResolvePercent(
				layoutunit.FromFloat64Round(contentBlockSize), pct).Float64()
		}
		// else: indefinite block-size → percentage row-gap resolves to 0
	}
	if v, ok := fla.style.GetLength("column-gap"); ok {
		colGap = v
	} else if pct, ok := fla.style.GetPercentage("column-gap"); ok {
		colGap = layoutunit.ResolvePercent(
			layoutunit.FromFloat64Round(contentInlineSize), pct).Float64()
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
	containerMainSize float64,
	hasDefiniteMain bool,
) float64 {
	// Check if min-size is explicitly set (non-auto).
	// The CSS property controlling the flex main axis depends on the CONTAINER's
	// writing mode (same logic as flexItemExplicitMin), while the resolve function
	// depends on the ITEM's writing mode via mainIsItemInline.
	mainIsItemInline := computeMainIsItemInline(fla.space.WritingDirection, childWDM, isRow)
	containerIsVertical := fla.space.WritingDirection.IsVertical()

	var minProp string
	if isRow {
		if containerIsVertical {
			minProp = "min-height" // VRL row: main = physical height
		} else {
			minProp = "min-width" // HTB row: main = physical width
		}
	} else {
		if containerIsVertical {
			minProp = "min-width" // VRL column: main = physical width
		} else {
			minProp = "min-height" // HTB column: main = physical height
		}
	}

	if v, ok := style.Get(minProp); ok && v != "" && v != "auto" {
		v = strings.TrimSpace(v)
		// Handle intrinsic keywords (min-content, max-content, fit-content).
		if v == "min-content" || v == "max-content" || v == "fit-content" {
			// Per CSS Sizing 3 §5.1, min-content in the block axis is equivalent
			// to auto. For flex items, auto triggers the §4.5 automatic minimum
			// size algorithm, so we fall through to that code below.
			if v == "min-content" && !mainIsItemInline {
				// Fall through to §4.5 automatic minimum size below.
			} else if mainIsItemInline {
				childSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
					SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
					SetAvailableSize(geomLogicalToOld(space.AvailableSize)).
					SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
					Build()
				mm := ComputeMinMaxSizes(fla.ctx, child, childSpace)
				switch v {
				case "min-content":
					return mm.MinContent
				case "max-content", "fit-content":
					return mm.MaxContent
				}
			} else {
				containerInlineSize := space.AvailableSize.InlineSize.Float64()
				minBlockSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
					SetAvailableSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
					SetPercentageResolutionSize(LogicalSize{InlineSize: containerInlineSize}).
					SetPercentageResolutionInlineSize(containerInlineSize).
					SetIsContentSuggestionLayout(true).
					Build()
				result := layoutElement(fla.ctx, child, minBlockSpace)
				lf := NewLogicalFragment(childWDM, result.Fragment)
				blockContent := lf.BlockSize() - childGeom.BlockBorderPadding()
				if blockContent < 0 {
					blockContent = 0
				}
				return blockContent
			}
		} else {
			if mainIsItemInline {
				return ResolveMinInlineSize(style, childWDM, space, childGeom).Float64()
			}
			return ResolveMinBlockSize(style, childWDM, space, childGeom).Float64()
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
		// Per Blink: if the item has a definite cross-size (block-size), pass it
		// as the percentage resolution block-size so that percentage-height
		// descendants can resolve (e.g., img { height: 100% } inside an item
		// with explicit height).
		pctBlockSize := Indefinite
		{
			itemSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
				SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
				pctBlockSize = explicit.Float64()
			} else if hasDefiniteCross {
				// Item will be stretched to the container cross-size.
				crossMargins := resolveItemCrossMargins(style, childWDM, contentInlineSize, mainIsItemInline)
				pctBlockSize = containerCrossSize - childGeom.BlockBorderPadding() - crossMargins
				if pctBlockSize < 0 {
					pctBlockSize = 0
				}
			}
		}
		pctResSize := LogicalSize{InlineSize: contentInlineSize}
		if pctBlockSize != Indefinite {
			pctResSize.BlockSize = pctBlockSize
		}
		minContentSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
			SetAvailableSize(LogicalSize{InlineSize: 0, BlockSize: Indefinite}).
			SetPercentageResolutionSize(pctResSize).
			SetPercentageResolutionInlineSize(fla.space.PercentageResolutionInlineSize).
			Build()
		mm := computeContentMinMaxSizes(fla.ctx, child, minContentSpace)
		contentSuggestion = mm.MinContent
	} else {
		// Main axis = item's block axis: block-direction minimum via layout.
		// Use IsContentSuggestionLayout to suppress the item's own CSS block-size
		// so the layout produces the content-based block-size (§4.5).
		//
		// CSS Sizing 3 §5.1: The min-content block size equals the max-content
		// block size, which is the block size when laid out at the item's
		// max-content inline size (shrink-to-fit width). For column flex items
		// without explicit inline-size, compute the shrink-to-fit width first
		// so that percentage padding on descendants resolves correctly.
		containerInlineSize := space.AvailableSize.InlineSize.Float64()
		// Only use shrink-to-fit for items that won't stretch to the container
		// cross-size. Stretch items use the full container width, so their
		// content suggestion should be computed at that width.
		alignItems := "stretch"
		if v, ok := fla.style.Get("align-items"); ok {
			alignItems = strings.TrimSpace(v)
		}
		selfAlign := fla.getAlignSelf(style, alignItems)
		_, _, crossAS, crossAE := getItemAutoMargins(style, childWDM, isRow)
		willStretch := selfAlign == "stretch" && !crossAS && !crossAE
		if !willStretch {
			if _, hasExplicitInline := ResolveInlineSize(style, childWDM,
				NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
					SetAvailableSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
					SetPercentageResolutionSize(LogicalSize{InlineSize: containerInlineSize}).
					SetPercentageResolutionInlineSize(contentInlineSize).
					Build(), childGeom); !hasExplicitInline {
				mmSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
					SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx)).
					SetAvailableSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
					SetPercentageResolutionSize(LogicalSize{InlineSize: containerInlineSize}).
					SetPercentageResolutionInlineSize(contentInlineSize).
					Build()
				mm := computeContentMinMaxSizes(fla.ctx, child, mmSpace)
				// computeContentMinMaxSizes returns content-box sizes.
				// AvailableSize.InlineSize is border-box, so add border+padding.
				fitContentInline := mm.MaxContent + childGeom.InlineBorderPadding()
				if fitContentInline > containerInlineSize {
					fitContentInline = containerInlineSize
				}
				containerInlineSize = fitContentInline
			}
		}
		colMinSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, true).
			SetAvailableSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: containerInlineSize, BlockSize: Indefinite}).
			SetPercentageResolutionInlineSize(containerInlineSize).
			SetIsContentSuggestionLayout(true).
			Build()
		result := layoutElement(fla.ctx, child, colMinSpace)
		lf := NewLogicalFragment(childWDM, result.Fragment)
		isReplaced := child.DOMNode != nil && IsReplacedElement(child.DOMNode)
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
	// Per §9.8, percentage main sizes on flex items resolve against the container's
	// definite main size, so we pass it for percentage resolution.
	specifiedSuggestion := -1.0
	{
		// Default to the Indefinite sentinel (not the float64 zero value):
		// IsBlockSizeIndefinite() checks BlockSize < 0, so a 0.0 would be
		// treated as DEFINITE and resolve a percentage main-size (e.g.
		// `height:1%`) against base 0 to a definite 0 instead of leaving it
		// auto. §4.5: the specified size suggestion only applies when the
		// preferred main size is definite; a percentage main-size against an
		// indefinite column container is NOT definite, so it must not feed a
		// definite-zero suggestion that would clamp the automatic minimum size
		// to 0. The `!mainIsItemInline && hasDefiniteMain` override below still
		// resolves percentages against a definite column main size. Mirrors
		// Blink core/layout/flex/flex_layout_algorithm.cc, where the flex-basis
		// ConstraintSpace's percentage-resolution block-size defaults to
		// kIndefiniteSize unless the container main size is definite (Chromium
		// pin 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
		pctBlockSize := float64(Indefinite)
		availBlockSize := Indefinite
		if !mainIsItemInline && hasDefiniteMain {
			pctBlockSize = containerMainSize
			availBlockSize = containerMainSize
		}
		itemSpace := NewConstraintSpaceBuilder(fla.space.WritingDirection, childWDM, false).
			SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: availBlockSize}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: pctBlockSize}).
			SetPercentageResolutionInlineSize(contentInlineSize).
			Build()
		if mainIsItemInline {
			if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
				specifiedSuggestion = explicit.Float64()
			}
		} else {
			if explicit, ok := ResolveBlockSize(style, childWDM, itemSpace, childGeom); ok {
				specifiedSuggestion = explicit.Float64()
			}
		}
	}

	// §4.5 Transferred size suggestion: if the item has an intrinsic aspect ratio
	// and a definite size in the cross axis, compute the main size from:
	// cross-content-size * aspect-ratio.
	transferredSuggestion := -1.0
	if child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
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
					crossContentSize = explicit.Float64()
				}
			} else {
				// Main = block, cross = inline.
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					crossContentSize = explicit.Float64()
				}
			}

			// §4.5: Also check min-cross-size as a fallback for the transferred
			// size suggestion. Per the spec, if the item has an intrinsic aspect
			// ratio, the transferred size is derived from "constraints in the
			// other dimension", which includes min-height/min-width.
			if crossContentSize < 0 {
				if mainIsItemInline {
					// Cross = block; check min-block-size (min-height).
					minCross := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom).Float64()
					if minCross > 0 {
						crossContentSize = minCross
					}
				} else {
					// Cross = inline; check min-inline-size (min-width).
					minCross := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom).Float64()
					if minCross > 0 {
						crossContentSize = minCross
					}
				}
			}

			// Clamp crossContentSize by cross-axis min/max constraints.
			// Per §4.5, the transferred size uses "cross size constraints"
			// which includes min/max.
			if crossContentSize >= 0 {
				if mainIsItemInline {
					// Cross = block.
					minCross := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom).Float64()
					if crossContentSize < minCross {
						crossContentSize = minCross
					}
					if maxCrossLU, ok := ResolveMaxBlockSize(style, childWDM, itemSpace, childGeom); ok {
						maxCross := maxCrossLU.Float64()
						if crossContentSize > maxCross {
							crossContentSize = maxCross
						}
					}
				} else {
					// Cross = inline.
					minCross := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom).Float64()
					if crossContentSize < minCross {
						crossContentSize = minCross
					}
					if maxCrossLU, ok := ResolveMaxInlineSize(style, childWDM, itemSpace, childGeom); ok {
						maxCross := maxCrossLU.Float64()
						if crossContentSize > maxCross {
							crossContentSize = maxCross
						}
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
					crossContentSize = explicit.Float64()
				}
			} else {
				if explicit, ok := ResolveInlineSize(style, childWDM, itemSpace, childGeom); ok {
					crossContentSize = explicit.Float64()
				}
			}
			// Also check min-cross-size as a fallback for non-replaced elements.
			if crossContentSize < 0 {
				if mainIsItemInline {
					minCross := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom).Float64()
					if minCross > 0 {
						crossContentSize = minCross
					}
				} else {
					minCross := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom).Float64()
					if minCross > 0 {
						crossContentSize = minCross
					}
				}
			}
			// Clamp crossContentSize by cross-axis min/max constraints.
			if crossContentSize >= 0 {
				if mainIsItemInline {
					minCross := ResolveMinBlockSize(style, childWDM, itemSpace, childGeom).Float64()
					if crossContentSize < minCross {
						crossContentSize = minCross
					}
					if maxCrossLU, ok := ResolveMaxBlockSize(style, childWDM, itemSpace, childGeom); ok {
						maxCross := maxCrossLU.Float64()
						if crossContentSize > maxCross {
							crossContentSize = maxCross
						}
					}
				} else {
					minCross := ResolveMinInlineSize(style, childWDM, itemSpace, childGeom).Float64()
					if crossContentSize < minCross {
						crossContentSize = minCross
					}
					if maxCrossLU, ok := ResolveMaxInlineSize(style, childWDM, itemSpace, childGeom); ok {
						maxCross := maxCrossLU.Float64()
						if crossContentSize > maxCross {
							crossContentSize = maxCross
						}
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
	isReplaced := child.DOMNode != nil && IsReplacedElement(child.DOMNode)
	if flexDebug {
		tag := ""
		if child.DOMNode != nil {
			tag = child.DOMNode.TagName
		}
		fmt.Printf("  AUTO-MIN <%s>: content=%.2f specified=%.2f transferred=%.2f isReplaced=%v mainIsInline=%v\n",
			tag, contentSuggestion, specifiedSuggestion, transferredSuggestion, isReplaced, mainIsItemInline)
	}
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
			if maxMainLU, hasMax := ResolveMaxInlineSize(style, childWDM, maxSpace, childGeom); hasMax {
				maxMain := maxMainLU.Float64()
				if autoMin > maxMain {
					autoMin = maxMain
				}
			}
		} else {
			if maxMainLU, hasMax := ResolveMaxBlockSize(style, childWDM, maxSpace, childGeom); hasMax {
				maxMain := maxMainLU.Float64()
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

// hasExplicitPercentCrossSize reports whether the item has a percentage
// (as opposed to length) value on its cross-axis size property. Used to
// decide whether to propagate the container's cross-size as the percentage
// resolution basis (CSS Flexbox §9.8 + CSS Sizing 3 §5.1).
func (fla *FlexLayoutAlgorithm) hasExplicitPercentCrossSize(style *css.Style, wdm WritingDirectionMode, isRow bool) bool {
	if style == nil {
		return false
	}
	var prop string
	if isRow {
		if wdm.IsVertical() {
			prop = "width"
		} else {
			prop = "height"
		}
	} else {
		if wdm.IsVertical() {
			prop = "height"
		} else {
			prop = "width"
		}
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
			// CSS Flexbox §9.4 + CSS Sizing 3: replaced elements with only an
			// intrinsic aspect ratio (no intrinsic dimensions) are not stretched.
			// Their cross-size was determined by the replaced element sizing
			// algorithm using the CSS default (300×150), and the transferred size
			// suggestion used that definite size. Stretching would override both
			// the cross-size and (via aspect ratio) the flex-resolved main-size.
			if item.node.DOMNode != nil && IsReplacedElement(item.node.DOMNode) {
				info := GetIntrinsicSizingInfo(fla.ctx, item.node)
				if info.HasAspectRatio && info.IntrinsicWidth == 0 && info.IntrinsicHeight == 0 {
					continue
				}
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
				minBlock := ResolveMinBlockSize(item.style, item.wdm, fla.space, item.geom).Float64()
				if stretchContent < minBlock {
					stretchContent = minBlock
				}
				if maxBlockLU, hasMax := ResolveMaxBlockSize(item.style, item.wdm, fla.space, item.geom); hasMax {
					maxBlock := maxBlockLU.Float64()
					if stretchContent > maxBlock {
						stretchContent = maxBlock
					}
				}
			} else {
				minInlineItem := ResolveMinInlineSize(item.style, item.wdm, fla.space, item.geom).Float64()
				if stretchContent < minInlineItem {
					stretchContent = minInlineItem
				}
				if maxInlineItemLU, hasMax := ResolveMaxInlineSize(item.style, item.wdm, fla.space, item.geom); hasMax {
					maxInlineItem := maxInlineItemLU.Float64()
					if stretchContent > maxInlineItem {
						stretchContent = maxInlineItem
					}
				}
			}
			newBorderBox := stretchContent + crossBP

			// CSS Flexbox §9.2 step B / §9.4: For elements with an aspect ratio
			// (intrinsic for <img>, or CSS aspect-ratio), no explicit CSS main-size,
			// and a stretch cross-size, build the constraint space without fixing
			// the main-size so it can be derived from the cross-size via the ratio.
			//
			// For <img>: uses intrinsic aspect ratio from the image.
			// For other elements: uses CSS aspect-ratio property.
			//
			// Only applies when the item's main-size was NOT resolved by
			// flex-grow/flex-shrink (i.e., flex-grow is 0 and no explicit main-size).
			// Otherwise, flex:1 items would have their grown main-size overridden.
			useAspectRatioStretch := false
			hasExplicitMainSize := false
			tmpSpace := NewConstraintSpaceBuilder(wdm, item.wdm, false).
				SetAvailableSize(LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: contentInlineSize}).
				SetPercentageResolutionInlineSize(contentInlineSize).
				Build()
			if item.mainIsItemInline {
				_, hasExplicitMainSize = ResolveInlineSize(item.style, item.wdm, tmpSpace, item.geom)
			} else {
				_, hasExplicitMainSize = ResolveBlockSize(item.style, item.wdm, tmpSpace, item.geom)
			}
			if !hasExplicitMainSize {
				if item.node.DOMNode != nil && item.node.DOMNode.TagName == "img" {
					info := GetIntrinsicSizingInfo(fla.ctx, item.node)
					if info.HasAspectRatio && info.AspectRatio > 0 {
						useAspectRatioStretch = true
					}
				} else if ar := item.style.GetAspectRatio(); ar.IsSet && ar.Width > 0 && ar.Height > 0 {
					// CSS aspect-ratio property on non-replaced elements.
					// Only apply when flex-grow is 0 — if the item grew via flex,
					// its main-size is already determined and shouldn't be overridden.
					flexGrow := item.style.GetFlexGrow()
					if flexGrow == 0 {
						useAspectRatioStretch = true
					}
				}
			}

			var cs ConstraintSpace
			if useAspectRatioStretch {
				// Build constraint space with cross-size fixed but main-size NOT
				// fixed. ComputeReplacedSize will derive the main-size from the
				// fixed cross-size and the image's intrinsic aspect ratio.
				childWDM := item.wdm
				b := NewConstraintSpaceBuilder(wdm, childWDM, true)
				b.SetIsInsideFlexibleBox(true)
				b.SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, fla.ctx))
				b.SetOrthogonalFallbackBlockSize(fla.space.OrthogonalFallbackBlockSize)
				if isRow {
					avail := LogicalSize{
						InlineSize: item.resolvedMain + item.mainBorderPadding(),
						BlockSize:  stretchContent + item.crossBorderPadding(),
					}
					b.SetAvailableSize(avail)
					b.SetPercentageResolutionSize(LogicalSize{
						InlineSize: item.resolvedMain,
						BlockSize:  stretchContent,
					})
					b.SetPercentageResolutionInlineSize(contentInlineSize)
					// Do NOT fix inline-size: let aspect ratio derive it from cross.
					b.SetIsFixedBlockSize(true)
				} else {
					avail := LogicalSize{
						InlineSize: stretchContent + item.crossBorderPadding(),
						BlockSize:  item.resolvedMain + item.mainBorderPadding(),
					}
					b.SetAvailableSize(avail)
					b.SetPercentageResolutionSize(LogicalSize{
						InlineSize: stretchContent,
						BlockSize:  item.resolvedMain,
					})
					b.SetPercentageResolutionInlineSize(contentInlineSize)
					b.SetIsFixedInlineSize(true)
					// Do NOT fix block-size: let aspect ratio derive it from cross.
				}
				cs = b.Build()
			} else {
				// Always relayout: even if the border-box size is unchanged,
				// the percentage resolution block-size changed from 0 (first pass
				// with Indefinite cross) to stretchContent (now definite).
				// This ensures descendants with percentage heights resolve correctly.
				cs = fla.buildItemConstraintSpace(item, wdm, contentInlineSize, isRow,
					item.resolvedMain, stretchContent, true)
			}
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
