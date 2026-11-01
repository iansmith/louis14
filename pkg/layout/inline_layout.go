package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
	"louis14/pkg/text"
	"maps"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// countLinesForWidth runs a dry-run line break at the given available width and
// returns the number of lines produced. This is used by text-wrap: balance to
// binary-search for the narrowest width that still yields the same line count.
func countLinesForWidth(
	itemsData *InlineItemsData,
	ctx *LayoutContext,
	wdm WritingDirectionMode,
	width float64,
	fonts text.FontConfig,
) int {
	space := ConstraintSpace{
		AvailableSize:    oldLogicalToGeom(LogicalSize{InlineSize: width, BlockSize: Indefinite}),
		WritingDirection: wdm,
	}
	lb := NewLineBreaker(itemsData, ctx, space, fonts, LineBreakerContent)
	lb.availableWidth = width
	var line LineInfo
	count := 0
	for lb.NextLine(&line) {
		count++
		// Safety: stop at a reasonable limit to avoid infinite loops.
		if count > 100 {
			break
		}
	}
	return count
}

// hasOnlyInlineChildren returns true if the block container's children are
// all inline-level (text nodes, display:inline, display:inline-block, etc.).
// When true, the container should use an inline formatting context.
//
// CSS 2.1 §9.2.1.1: block containers have either all block-level or all
// inline-level children. After anonymous block box generation by the layout
// tree builder, this is always a clean split.
// isCSSCollapsibleRune returns true for whitespace characters that CSS
// considers collapsible in normal white-space mode. U+00A0 (non-breaking
// space) is explicitly excluded — it never collapses (CSS 2.1 §16.6.1).
func isCSSCollapsibleRune(r rune) bool {
	return r != '\u00A0' && unicode.IsSpace(r)
}

// isInlineLevelDisplay reports whether a specified display value is
// inline-level per CSS Display §3. Used to capture the static position of
// an out-of-flow box in an inline formatting context: an inline-level
// abspos captures beside the current inline cursor; a block-level abspos
// captures at the block-end of the current line box (as if it started a
// new block-flow line). Mirrors Blink's
// ComputedStyle::IsOriginalDisplayInlineType.
func isInlineLevelDisplay(d css.DisplayType) bool {
	switch d {
	case css.DisplayInline,
		css.DisplayInlineBlock,
		css.DisplayInlineFlex,
		css.DisplayInlineGrid,
		css.DisplayInlineTable,
		css.DisplayInlineListItem,
		css.DisplayRuby,
		css.DisplayRubyText:
		// DisplayBlockRuby is block-level (its principal box is a
		// LayoutBlockFlow); DisplayRubyBase no longer exists.
		return true
	}
	return false
}

// isAtomicBaselineDisplay returns true iff the display type participates in baseline
// alignment as an atomic inline. These are the displays that export a baseline (either
// from contents or synthesized from the margin box) to the parent line's baseline
// calculation. Mirrors Blink's set of atomic-inline displays that reach
// UseLogicalBottomMarginEdgeForInlineBlockBaseline.
func isAtomicBaselineDisplay(d css.DisplayType) bool {
	switch d {
	case css.DisplayInlineBlock, css.DisplayInlineFlex, css.DisplayFlex,
		css.DisplayTable, css.DisplayInlineTable, css.DisplayInlineGrid, css.DisplayGrid,
		// `inline flow-root list-item` is an atomic inline block container
		// and baseline-aligns like inline-block (CSS 2.1 §10.8.1, last
		// line box). Pure `inline list-item` never reaches the atomic
		// path — it is a true inline box (Blink LayoutInlineListItem).
		css.DisplayInlineListItem:
		return true
	}
	return false
}

// useLogicalBottomMarginEdgeForInlineBlockBaseline returns true iff the element should
// synthesize its inline-level baseline from the logical bottom margin edge (border-box
// block-end in the parent's writing direction), rather than deriving it from content.
// Per CSS Align baseline-export (https://drafts.csswg.org/css-align/#baseline-export):
// - inline-block, inline-table, table: synthesize from margin box when scrollable.
// - inline-flex, inline-grid, flex, grid: always derive from content baseline.
// This mirrors Blink LayoutBox::UseLogicalBottomMarginEdgeForInlineBlockBaseline
// (third_party/blink/renderer/core/layout/layout_box.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func useLogicalBottomMarginEdgeForInlineBlockBaseline(d css.DisplayType, st *css.Style) bool {
	if st == nil {
		return false
	}
	switch d {
	case css.DisplayInlineBlock, css.DisplayTable, css.DisplayInlineTable,
		// `inline flow-root list-item` is an inline-block container and
		// follows the same rule (pure inline list items never reach the
		// atomic baseline path).
		css.DisplayInlineListItem:
		// These displays synthesize from margin box when they are scroll containers.
		return st.GetOverflowX() != css.OverflowVisible || st.GetOverflowY() != css.OverflowVisible
	}
	// Flex, grid, and other displays derive from content baseline regardless of overflow.
	return false
}

func hasOnlyInlineChildren(node *LayoutInputNode) bool {
	hasContent := false
	for _, child := range node.Children() {
		if child.IsText() {
			// Use CSS-aware trimming: U+00A0 (non-breaking space) is not
			// collapsible and counts as visible content (CSS 2.1 §16.6.1).
			// Under white-space: pre/pre-wrap/break-spaces, space/tab
			// characters are NEVER collapsed (CSS Text 3 §16.6.1
			// white-space-collapse: preserve) — a text node consisting
			// SOLELY of tabs/spaces still counts as content there, even
			// though the same text would collapse away under `normal`
			// (LOU-344's active-selection-063.html: a `white-space: pre`
			// div containing only `\t` characters was wrongly classified
			// as having no inline content, routing it into the
			// block-children path where it produced no fragment at all —
			// not just a sizing quirk, a missing box).
			text := child.TextContent()
			ws := css.WhiteSpaceNormal
			if childStyle := child.Style(); childStyle != nil {
				ws = childStyle.GetWhiteSpace()
			}
			if whiteSpacePreservesWhitespace(ws) {
				if text != "" {
					hasContent = true
				}
			} else if whiteSpacePreservesBreaks(ws) && strings.Contains(text, "\n") {
				// pre-line preserves line BREAKS (but collapses other
				// whitespace) — a text node consisting solely of "\n"
				// still produces a real forced line break and must count
				// as content, even though whiteSpacePreservesWhitespace
				// is false for pre-line.
				hasContent = true
			} else if strings.IndexFunc(text, func(r rune) bool {
				return !isCSSCollapsibleRune(r)
			}) >= 0 {
				hasContent = true
			}
			continue
		}
		if !child.IsElement() && !child.IsAnonymous() {
			continue
		}
		style := child.Style()
		if style == nil {
			continue
		}
		// Out-of-flow elements (abs-pos, fixed) don't participate in normal flow.
		// They must not affect the formatting context determination.
		pos := style.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			continue
		}
		// Floats are allowed in both inline and block formatting contexts.
		if style.GetFloat() != css.FloatNone {
			continue
		}
		display := style.GetDisplay()
		if display != css.DisplayInline && display != css.DisplayInlineBlock &&
			display != css.DisplayInlineFlex && display != css.DisplayInlineGrid &&
			display != css.DisplayInlineTable &&
			display != css.DisplayInlineListItem &&
			// css-ruby Phase 2 (LOU-155): `display: ruby` and
			// `display: ruby-text` are inline-level — they're the
			// inline ruby column root and annotation element
			// respectively. Without these here, a block container
			// whose only child is a `<ruby>` falls into the
			// block-children layout path and each ruby internal
			// (rb/rt) gets its own per-element block layout, which
			// is wrong: ruby is a single inline atom that owns its
			// own inline formatting context (mirrors Blink
			// `isInlineLevelDisplay` + the inline-IFC entry in
			// LayoutBlockFlow). Vetted against Chromium main @
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
			display != css.DisplayRuby && display != css.DisplayRubyText {
			return false // Block-level child found.
		}
		hasContent = true
	}
	return hasContent
}

// inlineOOFCandidate builds an out-of-flow candidate for an inline-collected
// OOF item at the given static offset within the containing block's content
// box. The static edges are always start/start — Blink's InlineLayoutAlgorithm
// records inline OOF static positions at the line-relative start corner.
func inlineOOFCandidate(node *LayoutInputNode, inlineOffset, blockOffset float64, isFixed bool, inlineContainer *html.Node) OutOfFlowCandidate {
	return OutOfFlowCandidate{
		Node: node,
		StaticPosition: LogicalStaticPosition{
			Offset:     LogicalOffset{InlineOffset: inlineOffset, BlockOffset: blockOffset},
			InlineEdge: StaticEdgeStart,
			BlockEdge:  StaticEdgeStart,
		},
		IsFixedPosition: isFixed,
		InlineContainer: inlineContainer,
	}
}

// computeLineBoundsBFC returns the line box's start/end BFC inline coordinates
// given the block's content bounds and float exclusion offsets.
// floatStart is the BFC inline coordinate of the right edge of the leftmost
// active float; floatEnd is the distance consumed from the BFC container's
// inline-end edge.
func computeLineBoundsBFC(bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize, floatStart, floatEnd float64) (lineStartBFC, lineEndBFC float64) {
	lineStartBFC = bfcInlineOrigin
	lineEndBFC = bfcInlineOrigin + contentInlineSize
	if floatStart > 0 && floatStart > lineStartBFC {
		lineStartBFC = floatStart
	}
	if floatEnd > 0 {
		if rightEdge := bfcContainerInlineSize - floatEnd; rightEdge < lineEndBFC {
			lineEndBFC = rightEdge
		}
	}
	return
}

// layoutInlineChildren handles inline formatting context for a block container.
// It collects inline content, breaks it into lines, and adds line box fragments
// to the builder. Break tokens of floats that broke before or inside go to
// context (parallel flows the block resumes in the next fragmentainer).
//
// Ported from Blink's InlineLayoutAlgorithm (inline_layout_algorithm.h).
func (bla *BlockLayoutAlgorithm) layoutInlineChildren(
	wdm WritingDirectionMode,
	contentInlineSize float64,
	exclusionSpace *ExclusionSpace,
	bfcBlockOrigin float64,
	bfcInlineOrigin float64,
	bfcContainerInlineSize float64,
	startItemIndex int,
	startTextOffset int,
	segment inlineSegment,
	context *InlineChildLayoutContext,
) (blockSizeUsed float64, updatedES *ExclusionSpace, firstLineAscent float64, lastBaselineOffset float64, inlineBreakToken *BlockBreakToken) {
	itemsData, baseDir, isPlaintext := bla.prepareInlineItems(wdm)
	if len(itemsData.Items) == 0 {
		return segment.blockStart, exclusionSpace, 0, 0, nil
	}

	// Phase 1d: Map each OOF item to its innermost positioned-inline
	// ancestor (DOM node), so the OOF candidate can carry the inline CB
	// reference (CSS 2.1 §10.1.4 / CSS Position 3 §def-cb). Built once
	// up front because the line-breaker emits OpenTag/CloseTag once per
	// inline item — not per line a span spans across — so a per-line
	// stack would miss spans that wrap across multiple lines.
	positionedInlineMap, atomicInlineContainers := bla.inlineItemsCache.positionedInlineMap, bla.inlineItemsCache.atomicInlineContainers

	// Phase 1c: Lay out inline floats (compute their size only). Positioning
	// is deferred until line breaking so that floats declared after a forced
	// break (e.g. after <br>) are placed at the block-start of the post-break
	// line rather than at the container's top. CSS 2.1 §9.5.1 rule 5: "The
	// outer top of a floating box may not be higher than the top of any line
	// box containing a box generated by an element earlier in the source
	// document." Mirrors Blink's LineBreaker::HandleFloat + InlineLayoutAlgorithm::
	// PlaceFloatingObjects which stamps the float with the current line's
	// BFC block-offset (inline_layout_algorithm.cc:835-917).
	if exclusionSpace == nil {
		exclusionSpace = &ExclusionSpace{}
	}
	// The floats not yet positioned, measured once per layout (cached on
	// the prepared inline items, LOU-510).
	floats, initialLetterAdjust := bla.inlineFloats(contentInlineSize, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize)
	pendingFloats := maps.Clone(floats)

	// placeFloat positions a pending float at the top of the current line
	// (floatOriginBFC). Returns the new exclusion-space pointer and whether
	// the float was placed here (it was not when it broke before).
	placeFloat := func(pf *pendingFloat, floatOriginBFC float64, es *ExclusionSpace) (*ExclusionSpace, bool) {
		// The initial-letter float is exempt from the block-start shift: the
		// surrounding text is pushed down by initialLetterAdjust (blockOffset
		// starts there) but the letter stays at the unshifted visual top.
		// (No-op for a drop initial letter, where initialLetterAdjust is 0.)
		if pf.isInitialLetter {
			floatOriginBFC -= initialLetterAdjust
		}
		pf.unpositioned.OriginBfcBlockOffset = floatOriginBFC
		return bla.placeInlineFloat(pf.item, pf.unpositioned, es, bfcBlockOrigin, bfcInlineOrigin, context)
	}

	// Phase 2: Create line breaker.
	fonts := bla.ctx.FontConfig

	// CSS 2.1 §16.6: white-space: nowrap / pre prevent soft wrapping.
	// CSS Text 4 §3.4: text-wrap: nowrap is equivalent in line-break behavior.
	// Use unlimited available width so the line breaker produces a single line
	// that may overflow the container (overflow:hidden will clip it).
	lineAvailableWidth := contentInlineSize
	noWrap := false
	if bla.style != nil {
		ws := bla.style.GetWhiteSpace()
		if ws == css.WhiteSpaceNowrap || ws == css.WhiteSpacePre {
			noWrap = true
		}
		if !noWrap && bla.style.GetTextWrap() == "nowrap" {
			noWrap = true
		}
	}
	// Also check inline items: if any text item's style has nowrap, apply.
	if !noWrap {
		for _, item := range itemsData.Items {
			if item.Type == InlineItemText && item.Style != nil {
				ws := item.Style.GetWhiteSpace()
				if ws == css.WhiteSpaceNowrap || ws == css.WhiteSpacePre {
					noWrap = true
				}
				if !noWrap && item.Style.GetTextWrap() == "nowrap" {
					noWrap = true
				}
				break
			}
		}
	}
	if noWrap {
		lineAvailableWidth = 1e9
	}

	// CSS Text 4 §3.4: text-wrap: balance — make all lines approximately
	// equal width by binary-searching for the narrowest available width
	// that still produces the same number of lines. Per spec, only apply
	// when the line count is ≤ 4. This mirrors Blink's text balancing in
	// InlineLayoutAlgorithm.
	if !noWrap && bla.style != nil && bla.style.GetTextWrap() == "balance" {
		normalLineCount := countLinesForWidth(itemsData, bla.ctx, wdm, lineAvailableWidth, fonts)
		if normalLineCount >= 2 && normalLineCount <= 4 {
			// Binary search: find the narrowest width that still yields
			// normalLineCount lines. The lower bound is 0 (would produce
			// more lines), and the upper bound is the current available width.
			lo := 0.0
			hi := lineAvailableWidth
			for hi-lo > 0.5 { // 0.5px precision
				mid := (lo + hi) / 2
				if countLinesForWidth(itemsData, bla.ctx, wdm, mid, fonts) <= normalLineCount {
					hi = mid
				} else {
					lo = mid
				}
			}
			lineAvailableWidth = hi
		}
	}

	// Compute the percentage resolution block-size for inline children.
	// CSS 2.1 §10.5: percentage heights resolve against the containing block's
	// content height. The containing block is THIS element, so if it has a
	// definite (explicit) block-size, use that — not the parent's percentage
	// resolution size. This mirrors what block_layout.go does for block children
	// at lines 40-48 + 374-376.
	geomForPct := CalculateInitialFragmentGeometry(bla.ctx, bla.node, bla.style, wdm, bla.space)
	pctBlockSize := bla.space.PercentageResolutionSize.BlockSize.Float64()
	if geomForPct.BorderBoxSize.BlockSize != Indefinite {
		ownContentBlock := geomForPct.BorderBoxSize.BlockSize - geomForPct.BlockBorderPadding()
		if ownContentBlock < 0 {
			ownContentBlock = 0
		}
		pctBlockSize = ownContentBlock
	}

	lineAvailBlock := Indefinite
	if pctBlockSize > 0 {
		lineAvailBlock = pctBlockSize
	} else if bla.space.AvailableSize.BlockSize.Float64() >= 0 {
		lineAvailBlock = bla.space.AvailableSize.BlockSize.Float64()
	}

	// §10.3.2: Pre-resolve the available block-size that orthogonal
	// atomic-inline children (inline-block with perpendicular writing mode)
	// should use. Mirrors block_layout.go:94-96 so the atomic-inline path in
	// line_breaker.handleAtomicInline gets the same ancestor-walk + ICB cap
	// as the block-child path at block_layout.go:368-370.
	hasExplicitBlock := geomForPct.BorderBoxSize.BlockSize != Indefinite
	var explicitBlockSize float64
	if hasExplicitBlock {
		explicitBlockSize = geomForPct.BorderBoxSize.BlockSize - geomForPct.BlockBorderPadding()
		if explicitBlockSize < 0 {
			explicitBlockSize = 0
		}
	}
	orthogonalAvailableBlock := computeOrthogonalAvailableBlock(
		bla.style, wdm, bla.space, geomForPct, bla.ctx,
		lineAvailBlock, hasExplicitBlock, explicitBlockSize)

	lineSpace := ConstraintSpace{
		AvailableSize:                  oldLogicalToGeom(LogicalSize{InlineSize: lineAvailableWidth, BlockSize: lineAvailBlock}),
		PercentageResolutionSize:       oldLogicalToGeom(LogicalSize{InlineSize: contentInlineSize, BlockSize: pctBlockSize}),
		PercentageResolutionInlineSize: contentInlineSize,
		WritingDirection:               wdm,
		// An inline child in a parallel flow stretches (Blink's
		// CreateConstraintSpaceForChild, block_layout_algorithm.cc:
		// 3422-3425 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		InlineAutoBehavior: AutoSizeBehaviorStretchImplicit,
		ExclusionSpace:     exclusionSpace,
		// The line breaker splits a line before a <br> that needs
		// clearance past the fragmentainer (LineBreaker::
		// HandleForcedLineBreak reads the line's space,
		// inline/line_breaker.cc:2895 @ a9f50e522efa).
		HasBlockFragmentation:        bla.space.HasBlockFragmentation,
		OrthogonalFallbackInlineSize: bla.space.OrthogonalFallbackInlineSize,
		OrthogonalFallbackBlockSize: computeOrthogonalFallbackBlockForChildren(
			bla.style, wdm, bla.space, geomForPct, bla.ctx,
			hasExplicitBlock, explicitBlockSize),
		OrthogonalAvailableBlock: orthogonalAvailableBlock,
	}
	lb := NewLineBreaker(itemsData, bla.ctx, lineSpace, fonts, LineBreakerContent)
	lb.availableWidth = lineAvailableWidth
	// Resume cursor from incoming break token. Mirrors Blink's
	// `InlineBreakToken::start_` which carries both item-index AND
	// text-offset. Restore when EITHER advances past 0 — a single-text-
	// item IFC (e.g. `<div>xx xx<br/>xx xx<br/>xx xx</div>`) keeps
	// startItemIndex=0 while startTextOffset advances line-by-line, so
	// gating on `startItemIndex > 0` alone loses the resume point and
	// makes each subsequent column re-start at the beginning.
	// See findings §F4.
	after := segment.after()
	if after != nil && startItemIndex == 0 && startTextOffset == 0 {
		// The run starts right after its block-in-inline item.
		bii, ok := bla.inlineItemsCache.blockInInline[after]
		startItemIndex = bii.index + 1
		if !ok || startItemIndex >= len(itemsData.Items) {
			return segment.blockStart, exclusionSpace, 0, 0, nil
		}
	}
	if (startItemIndex > 0 || startTextOffset > 0) && startItemIndex < len(itemsData.Items) {
		lb.currentItemIndex = startItemIndex
		if startTextOffset > itemsData.Items[startItemIndex].StartOffset {
			lb.currentTextOffset = startTextOffset
		} else {
			lb.currentTextOffset = itemsData.Items[startItemIndex].StartOffset
		}
	}

	// Get text-align from the container's style.
	textAlign := "start"
	textAlignLast := "auto"
	if bla.style != nil {
		if ta, ok := bla.style.Get("text-align"); ok {
			textAlign = ta
		}
		textAlignLast = bla.style.GetTextAlignLast()
		// Flex containers: emulate justify-content as text-align for inline content.
		display := bla.style.GetDisplay()
		if display == css.DisplayFlex || display == css.DisplayInlineFlex {
			jc := bla.style.GetJustifyContent()
			switch jc {
			case css.JustifyContentCenter:
				textAlign = "center"
			case css.JustifyContentFlexEnd, css.JustifyContentRight:
				textAlign = "right"
			}
		}
	}

	// CSS 2.1 §16.1: text-indent offsets the first line of a block container.
	// CSS Values §10.2: calc() with percent is resolved against the containing
	// block's inline-size, same as bare-percent text-indent.
	textIndent := 0.0
	if bla.style != nil {
		textIndent = bla.style.ResolveTextIndent(contentInlineSize)
	}

	// Phase 3: Break into lines and create line box fragments.
	// blockOffset starts at initialLetterAdjust so the paragraph text is shifted
	// down past a raised initial letter (LOU-289 part 3); 0 in the common case.
	// A raised initial letter shifts only the inline formatting context's
	// first run of lines.
	blockOffset := segment.blockStart
	if after == nil {
		blockOffset += initialLetterAdjust
	}
	var line LineInfo
	// The first formatted line is the first line of the inline formatting
	// context, never one after a block-in-inline.
	isFirstLine := after == nil
	firstLineAscent = -1.0 // -1 means not yet set
	// Whether an in-flow child placed so far left a breakpoint before the
	// next line, giving it container separation (Blink
	// BlockLayoutAlgorithm::has_break_opportunity_before_next_child_,
	// block_layout_algorithm.cc:311, :2767-2772 @ a9f50e522efa).
	hasBreakOpportunityBeforeNextChild := segment.hasBreakOpportunityBefore
	// The inline child the lines are, as the block's child for break
	// decisions (Blink's InlineNode).
	inlineChild := segment.node
	if inlineChild == nil {
		inlineChild = &LayoutInputNode{isInlineSegment: true}
	}

	// openInlineStack tracks inline spans opened on prior lines that are
	// still open at the start of the current line. Passed into createLineBoxEx
	// so spans that wrap multiple lines emit a fragment per line, and updated
	// from the residual returned after each line. Mirrors Blink's persistent
	// InlineLayoutStateStack across NGInlineLayoutAlgorithm iterations.
	openInlineStack := rebuildOpenInlineStack(itemsData.Items[:lb.currentItemIndex])

	// lineStartBlockOffset is where the line being laid out started, before
	// any float pushed it down (Blink's child BFC block-offset, vs the line
	// box BFC block-offset; block_layout_algorithm.cc:2597-2617 @
	// a9f50e522efa). It survives the restart that re-breaks a line below a
	// float (the `continue` after placing pending floats).
	lineStartBlockOffset := blockOffset

	for {
		// Save item index and text offset at the start of this line iteration,
		// before NextLine advances the line breaker. Both are needed for inline
		// break tokens so the next column resumes at exactly this line's start
		// (item index alone is insufficient when a text item spans multiple lines).
		lineStartIdx := lb.currentItemIndex
		lineStartTextOffset := lb.currentTextOffset
		// A line that breaks before takes the floats placed with it along:
		// they are re-encountered when the line is laid out again in the
		// next fragmentainer. In Blink they are children of the discarded
		// line box, whose exclusion space is not committed
		// (block_layout_algorithm.cc:2646-2678 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		lineStartChildCount := len(bla.containerBuilder.children)
		lineStartExclusionSpace := exclusionSpace
		lineStartParallelFlowTokenCount := len(context.parallelFlowBreakTokens)
		breakBeforeLine := func(token *BlockBreakToken) {
			bla.containerBuilder.children = bla.containerBuilder.children[:lineStartChildCount]
			exclusionSpace = lineStartExclusionSpace
			context.parallelFlowBreakTokens = context.parallelFlowBreakTokens[:lineStartParallelFlowTokenCount]
			inlineBreakToken = token
		}

		// CSS 2.1 §9.5: account for floats when computing available inline size.
		// FindAvailableInlineSize returns the space consumed by left/right floats
		// at the current block position. The exclusion space uses BFC-relative
		// coordinates, so we add bfcBlockOrigin to translate local offsets.
		//
		// For non-BFC blocks whose content area is at a non-zero BFC inline
		// position (e.g., blocks with negative margins or explicit width that
		// differs from the BFC container), we compute the intersection of the
		// block's content area and the float-free region in BFC coordinates,
		// then convert to local coordinates.
		floatStart, floatEnd := 0.0, 0.0
		bfcBlock := bfcBlockOrigin + blockOffset
		if exclusionSpace != nil {
			floatStart, floatEnd = exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, bfcContainerInlineSize)
		}

		lineStartBFC, lineEndBFC := computeLineBoundsBFC(bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize, floatStart, floatEnd)
		lineInlineOffsetFromFloat := lineStartBFC - bfcInlineOrigin // local offset
		lineAvailableInline := lineEndBFC - lineStartBFC
		if lineAvailableInline < 0 {
			lineAvailableInline = 0
		}

		// CSS 2.1 §9.5: if floats consume all available inline space,
		// clear past them before generating the line. This avoids
		// force-fitting content into zero-width space and then clearing,
		// which produces incorrect line breaks.
		if lineAvailableInline < 1 && exclusionSpace != nil && (floatStart > 0 || floatEnd > 0) {
			clearedBlock := exclusionSpace.ClearanceOffset(css.ClearBoth, layoutunit.FromFloat64Round(bfcBlock), wdm).Float64()
			if clearedBlock > bfcBlock {
				blockOffset = clearedBlock - bfcBlockOrigin
				bfcBlock = clearedBlock
				floatStart, floatEnd = exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, bfcContainerInlineSize)
				lineStartBFC, lineEndBFC = computeLineBoundsBFC(bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize, floatStart, floatEnd)
				lineInlineOffsetFromFloat = lineStartBFC - bfcInlineOrigin
				lineAvailableInline = lineEndBFC - lineStartBFC
				if lineAvailableInline < 0 {
					lineAvailableInline = 0
				}
			}
		}
		if lineAvailableInline < 1 {
			lineAvailableInline = 1
		}

		// lineVisualInline is the actual container width used for text alignment
		// and line box sizing. It is always the physical container width, not
		// the inflated value used for noWrap line breaking.
		lineVisualInline := lineAvailableInline

		// CSS 2.1 §16.6: white-space: nowrap / pre — override available width
		// to prevent soft wrapping, allowing text to overflow the container.
		if noWrap {
			lineAvailableInline = 1e9
		}

		// Set available width for the line breaker, including text-indent on first line.
		if isFirstLine && textIndent != 0 {
			lb.availableWidth = lineAvailableInline - textIndent
		} else {
			lb.availableWidth = lineAvailableInline
		}

		// CSS Pseudo 4 §3.2: measure the first formatted line with the
		// ::first-line font so it breaks at the correct character count when
		// ::first-line changes font-size/letter-spacing/word-spacing. Mirrors
		// Blink's `LineBreaker::use_first_line_style_` lifecycle: set for the
		// first formatted line, cleared for every subsequent line
		// (`line_breaker.cc:467` set / `:800` clear @ 4883d11fef). The
		// post-break `applyFirstLineStyles` below only changes paint metrics;
		// the breaker needs the font BEFORE breaking, which this provides.
		lb.useFirstLineStyle = isFirstLine && bla.node.FirstLineStyle != nil
		lb.firstLineStyle = bla.node.FirstLineStyle
		// Each line breaks against the exclusion space current at it, as
		// Blink constructs each line's LineBreaker with it
		// (inline/line_breaker.cc:492 @ a9f50e522efa).
		lb.space.ExclusionSpace = exclusionSpace

		if !lb.NextLine(&line) {
			break
		}
		// A block-in-inline line ends this run of lines: the block is the
		// containing block's next in-flow child (see blockInInlineChildren).
		if line.IsBlockInInline {
			break
		}

		// CSS 2.1 §9.4.2: a line box that contains only out-of-flow
		// candidates (and possibly collapsible whitespace that contributes
		// nothing to the line) should not generate a line box. Record the
		// OOF candidates at the current block offset and continue without
		// advancing. This matches Blink's behavior where a trailing line
		// after a <br> containing only positioned content collapses away.
		if lineHasOnlyOutOfFlow(&line, itemsData, wdm) {
			for _, r := range line.Results {
				if r.Item.Type == InlineItemOutOfFlow && r.Item.LayoutNode != nil {
					bla.containerBuilder.AddOutOfFlowCandidate(inlineOOFCandidate(
						r.Item.LayoutNode, 0, blockOffset,
						r.Item.Style != nil && r.Item.Style.GetPosition() == css.PositionFixed,
						positionedInlineMap[r.Item],
					))
				} else if r.Item.Type == InlineItemFloat {
					if pf, ok := pendingFloats[r.Item]; ok {
						exclusionSpace, _ = placeFloat(pf, bfcBlockOrigin+blockOffset, exclusionSpace)
						delete(pendingFloats, r.Item)
					}
				}
			}
			continue
		}

		// CSS 2.1 §9.5.2: shortened line box push-down.
		// If this line contains a pending float (not yet placed) followed by
		// non-float content that won't fit beside the float in the shortened
		// line box, place the float first at the current block offset, then
		// push the remaining content down past the float and re-break the
		// line. Mirrors Blink's LineBreaker::HandleFloat which commits the
		// float to the exclusion space before testing line-fit for subsequent
		// content. Only applies when no committed content precedes the float
		// on the line — retroactively moving already-positioned content is
		// handled by a separate path.
		if exclusionSpace != nil && len(pendingFloats) > 0 {
			lastPendingFloatIdx := -1
			contentBeforeFloat := false
			for i, r := range line.Results {
				switch r.Item.Type {
				case InlineItemFloat:
					if _, ok := pendingFloats[r.Item]; ok && !contentBeforeFloat {
						lastPendingFloatIdx = i
					}
				case InlineItemText:
					if lastPendingFloatIdx < 0 {
						content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
						if strings.TrimFunc(content, isCSSCollapsibleSpace) != "" {
							contentBeforeFloat = true
						}
					}
				case InlineItemAtomicInline:
					if lastPendingFloatIdx < 0 {
						contentBeforeFloat = true
					}
				}
			}

			if lastPendingFloatIdx >= 0 {
				pendingFloatInline := 0.0
				for i := 0; i <= lastPendingFloatIdx; i++ {
					r := &line.Results[i]
					if r.Item.Type != InlineItemFloat {
						continue
					}
					if pf, ok := pendingFloats[r.Item]; ok {
						pendingFloatInline += pf.marginBoxInlineSize
					}
				}

				contentAfterFloatWidth := 0.0
				hasContentAfterFloat := false
				for i := lastPendingFloatIdx + 1; i < len(line.Results); i++ {
					r := line.Results[i]
					if r.Item.Type == InlineItemFloat || r.Item.Type == InlineItemOutOfFlow {
						continue
					}
					contentAfterFloatWidth += r.InlineSize
					switch r.Item.Type {
					case InlineItemText:
						content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
						if strings.TrimFunc(content, isCSSCollapsibleSpace) != "" {
							hasContentAfterFloat = true
						}
					case InlineItemAtomicInline:
						hasContentAfterFloat = true
					}
				}

				shortenedAvail := lineAvailableInline - pendingFloatInline
				if shortenedAvail < 0 {
					shortenedAvail = 0
				}

				if hasContentAfterFloat && contentAfterFloatWidth > shortenedAvail {
					placedAny := false
					for i := 0; i <= lastPendingFloatIdx; i++ {
						r := &line.Results[i]
						if r.Item.Type != InlineItemFloat {
							continue
						}
						if pf, ok := pendingFloats[r.Item]; ok {
							var placed bool
							exclusionSpace, placed = placeFloat(pf, bfcBlockOrigin+blockOffset, exclusionSpace)
							placedAny = placedAny || placed
							delete(pendingFloats, r.Item)
						}
					}

					// Past the floats placed here; a float that broke
					// before takes no space in this fragmentainer, so the
					// line stays where it is (LineBreaker::HandleFloat
					// returns before UpdateLineOpportunity, inline/
					// line_breaker.cc:3834-3836 @ a9f50e522efa).
					if placedAny {
						clearedBfc := exclusionSpace.ClearanceOffset(css.ClearBoth, layoutunit.FromFloat64Round(bfcBlockOrigin+blockOffset), wdm).Float64()
						if clearedBfc > bfcBlockOrigin+blockOffset {
							blockOffset = clearedBfc - bfcBlockOrigin
						}
					}

					lastFloatItemIdx := line.Results[lastPendingFloatIdx].ItemIndex
					lb.currentItemIndex = lastFloatItemIdx + 1
					lb.done = false
					if lb.currentItemIndex < len(itemsData.Items) {
						lb.currentTextOffset = itemsData.Items[lb.currentItemIndex].StartOffset
					}
					continue
				}
			}
		}

		line.TextAlign = textAlign

		// CSS Text §9.7: text-align-last controls alignment of the last line
		// of a block, or any line immediately before a forced break.
		if line.IsLastLine || line.HasForcedBreak {
			switch textAlignLast {
			case "auto":
				// "auto" means use text-align, except justify falls back to start.
				// The justify fallback is already handled in computeTextAlignOffset,
				// so nothing extra needed here.
			case "justify":
				line.TextAlign = "justify"
				// text-align-last: justify expands the last line (unlike text-align:
				// justify which falls back to start on the last line). Set the flag
				// so createLineBoxEx and computeTextAlignOffset honour the expansion.
				// CSS Text 3 §9.7; mirrors Blink's kJustify vs text-align-last path.
				line.TextAlignLastJustify = true
			case "start", "end", "left", "right", "center":
				line.TextAlign = textAlignLast
			}
		}

		// CSS Pseudo-Elements §3: Apply ::first-line styles to items on the
		// first formatted line. We override item styles after line breaking so
		// that color, text-decoration, background, etc. take effect at paint
		// time. Font properties that affect WHERE the line breaks (font-size,
		// letter-spacing, word-spacing) were already fed into the breaker above
		// via lb.useFirstLineStyle (WI-1), so the break position and the paint
		// metrics agree on the first-line font.
		isFirstLineForBox := isFirstLine && bla.node.FirstLineStyle != nil
		if isFirstLineForBox {
			applyFirstLineStyles(&line, bla.node.FirstLineStyle, bla.style, openInlineStack, fonts)
		}

		// Apply text-indent to the first line only.
		appliedTextIndent := 0.0
		lineInlineOffset := lineInlineOffsetFromFloat
		// Save isFirstLine before mutation so we can forward it to
		// createLineBoxEx (which uses it to stamp multi-line block
		// decorating-box metadata per CSS Text Decor 4 §3.6).
		isFirstLineForCreate := isFirstLine
		if isFirstLine && textIndent != 0 {
			lineInlineOffset += textIndent
			lineAvailableInline -= textIndent
			appliedTextIndent = textIndent
			lineVisualInline -= textIndent
			isFirstLine = false
		} else {
			isFirstLine = false
		}

		// CSS 2.1 §9.5: if the line content doesn't fit beside the float,
		// shift the block offset below the float and use the full width.
		if (floatStart > 0 || floatEnd > 0) && line.Width > lineAvailableInline && exclusionSpace != nil {
			clearedBfc := exclusionSpace.ClearanceOffset(css.ClearBoth, layoutunit.FromFloat64Round(bfcBlockOrigin+blockOffset), wdm).Float64()
			blockOffset = clearedBfc - bfcBlockOrigin
			lineInlineOffset = 0
			lineAvailableInline = contentInlineSize
			lineVisualInline = contentInlineSize
		}

		// Collect out-of-flow candidates from inline items on this line.
		// Their static position is (current block offset, inline position
		// computed from the items preceding them on the line).
		// Also position floats encountered on this line: place them at the
		// current line's BFC block offset. Mirrors Blink's
		// LineBreaker::HandleFloat / InlineLayoutAlgorithm::PlaceFloatingObjects
		// (inline_layout_algorithm.cc:835-917).
		inlinePos := 0.0
		floatsPlacedOnLine := false
		// committedBefore tracks the inline size of non-float content committed to
		// this line before the current float, used to decide whether a float should
		// be deferred to the next line.
		committedBeforeFloat := 0.0
		// trackAvail mirrors lineAvailableInline and is decremented per placed float
		// so that subsequent float-deferral checks use the updated available space.
		trackAvail := lineAvailableInline
		lineResultsTruncateAt := -1
		// Block-level abspos children that appear AFTER in-flow content on this
		// line capture static position at the block-end of the line box (as if
		// they started a new block-flow line below this one). Their emission is
		// deferred until the final line height is known via createLineBoxEx.
		// Block-level abspos children that appear BEFORE any in-flow content
		// emit immediately at (0, blockOffset) — their block-flow line would
		// begin at the current cursor since no preceding content needs to be
		// terminated. Mirrors Blink's InlineLayoutAlgorithm::HandleOutOfFlowPositioned
		// reading line_box_.LineBoxBlockEnd() at the time of encounter.
		var blockLevelOOFOnLine []struct {
			node            *LayoutInputNode
			isFixed         bool
			inlineContainer *html.Node
		}
		hasInflowOnLine := false
		for i, r := range line.Results {
			// CSS Ruby: an OOF descendant of a ruby element is collected into the
			// column's base/annotation sub-LineInfo, not the outer line.Results, so
			// surface it here (with its inline CB from positionedInlineMap) before
			// the column advances the inline cursor below — otherwise it is dropped.
			if r.RubyColumn != nil {
				surfaceRubyColumnOOFCandidates(r.RubyColumn, inlinePos, blockOffset, positionedInlineMap, bla.containerBuilder)
			}
			if r.Item.Type == InlineItemOutOfFlow && r.Item.LayoutNode != nil {
				oofStyle := r.Item.Style
				inlineLevel := oofStyle != nil && isInlineLevelDisplay(oofStyle.GetDisplay())
				isFixed := oofStyle != nil && oofStyle.GetPosition() == css.PositionFixed
				if inlineLevel {
					bla.containerBuilder.AddOutOfFlowCandidate(inlineOOFCandidate(
						r.Item.LayoutNode, inlinePos, blockOffset, isFixed,
						positionedInlineMap[r.Item],
					))
				} else if !hasInflowOnLine {
					// No in-flow content precedes this block-level OOF on the line:
					// its block-flow position starts at the current block cursor.
					bla.containerBuilder.AddOutOfFlowCandidate(inlineOOFCandidate(
						r.Item.LayoutNode, 0, blockOffset, isFixed,
						positionedInlineMap[r.Item],
					))
				} else {
					blockLevelOOFOnLine = append(blockLevelOOFOnLine, struct {
						node            *LayoutInputNode
						isFixed         bool
						inlineContainer *html.Node
					}{node: r.Item.LayoutNode, isFixed: isFixed, inlineContainer: positionedInlineMap[r.Item]})
				}
			} else if r.Item.Type == InlineItemFloat {
				if pf, ok := pendingFloats[r.Item]; ok {
					floatInlineSize := pf.marginBoxInlineSize
					// Push the float after this line when it does not fit
					// beside the content committed before it, or clearance
					// would move it down. Mirrors Blink's LineBreaker::
					// ShouldPushFloatAfterLine (inline/line_breaker.cc:
					// 3673-3712 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
					// Not ported: the retry without the trailing collapsible
					// space, and the block-start edge alignment test
					// (louis14's exclusion space keeps no LastFloatBlockStart).
					// With nothing before it the float is at the line start,
					// where pushing it after the line would not move it.
					if committedBeforeFloat > 0 {
						lineTopBFC := bfcBlockOrigin + blockOffset
						canFitFloat := committedBeforeFloat+floatInlineSize <= trackAvail
						clearType := pf.unpositioned.ClearType()
						isMovedByClearance := clearType != css.ClearNone &&
							exclusionSpace.ClearanceOffset(clearType, layoutunit.FromFloat64Round(lineTopBFC), wdm).Float64() > lineTopBFC
						if !canFitFloat || isMovedByClearance {
							// Defer this float and everything after it to the next line.
							lineResultsTruncateAt = i
							lb.currentItemIndex = r.ItemIndex
							lb.done = false
							if lb.currentItemIndex < len(itemsData.Items) {
								lb.currentTextOffset = itemsData.Items[lb.currentItemIndex].StartOffset
							}
							break
						}
					}
					exclusionSpace, _ = placeFloat(pf, bfcBlockOrigin+blockOffset, exclusionSpace)
					delete(pendingFloats, r.Item)
					floatsPlacedOnLine = true
					trackAvail -= floatInlineSize
					if trackAvail < 0 {
						trackAvail = 0
					}
				}
			} else {
				committedBeforeFloat += r.InlineSize
				// Any non-OOF, non-float item placed on the line causes the
				// line box to grow — a block-level OOF encountered AFTER this
				// point must capture its static position at the line's block-end.
				hasInflowOnLine = true
			}
			inlinePos += r.InlineSize
		}
		// If a float was deferred, truncate the line results and update line width
		// to reflect only the content that will be placed on this line.
		if lineResultsTruncateAt >= 0 {
			newWidth := 0.0
			for j := 0; j < lineResultsTruncateAt; j++ {
				rj := line.Results[j]
				if rj.Item.Type != InlineItemFloat && rj.Item.Type != InlineItemOutOfFlow {
					newWidth += rj.InlineSize
				}
			}
			line.Results = line.Results[:lineResultsTruncateAt]
			line.Width = newWidth
		}

		// After placing inline floats, re-query the exclusion space so text on
		// the same line is positioned beside the float rather than at inline=0.
		// Mirrors Blink's InlineLayoutAlgorithm::PlaceFloatingObjects which
		// updates ConstraintSpaceForLine after committing floats.
		if floatsPlacedOnLine && exclusionSpace != nil {
			bfcBlockNow := bfcBlockOrigin + blockOffset
			newFloatStart, newFloatEnd := exclusionSpace.FindAvailableInlineSize(bfcBlockNow, 0, bfcContainerInlineSize)
			newStartBFC, newEndBFC := computeLineBoundsBFC(bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize, newFloatStart, newFloatEnd)
			lineInlineOffset = newStartBFC - bfcInlineOrigin + appliedTextIndent
			lineAvailableInline = newEndBFC - newStartBFC - appliedTextIndent
			if lineAvailableInline < 0 {
				lineAvailableInline = 0
			}
		}

		// Determine the paragraph level for this line. For plaintext mode,
		// each paragraph between forced breaks may have its own direction.
		lineParagraphLevel := 0
		if baseDir == DirectionRTL {
			lineParagraphLevel = 1
		}
		if isPlaintext {
			for _, r := range line.Results {
				if r.Item.Type == InlineItemText || r.Item.Type == InlineItemAtomicInline {
					lineParagraphLevel = r.Item.ParagraphLevel
					break
				}
			}
			// Fallback: use any item's paragraph level.
			if lineParagraphLevel == 0 {
				for _, r := range line.Results {
					if r.Item.ParagraphLevel != 0 {
						lineParagraphLevel = r.Item.ParagraphLevel
						break
					}
				}
			}
		}

		// The line's last item in logical order: a <br> ending the line
		// adds any clearance after it (addAnyClearanceAfterLine).
		var lastLogicalItem *InlineItem
		if n := len(line.Results); n > 0 {
			lastLogicalItem = line.Results[n-1].Item
		}

		// Reorder line results from logical to visual order (UAX#9 L2)
		// before positioning. Mirrors Blink's BidiReorder step in
		// InlineLayoutAlgorithm::CreateLine.
		ReorderLineVisual(line.Results, lineParagraphLevel)

		// Use the effective direction for this line's box construction.
		// For plaintext mode, this may vary per line.
		effectiveWDM := wdm
		if lineParagraphLevel%2 == 1 {
			effectiveWDM.Dir = DirectionRTL
		} else {
			effectiveWDM.Dir = DirectionLTR
		}
		// Determine if the line uses central baseline. This depends on the
		// container's writing mode and text-orientation. text-orientation:
		// sideways causes vertical modes to use alphabetic baseline.
		centralBaseline := wdm.UsesCentralBaselineWithStyle(bla.style)
		// When every text run on the line has been resolved to
		// text-orientation: sideways (e.g. because its content is all
		// vertical-script — see collectTextNode), the line must use the
		// alphabetic baseline even if the container's computed value is
		// mixed/upright. This mirrors Blink's per-run font-metrics-driven
		// baseline selection when the font's vertical metrics coincide with
		// its horizontal metrics.
		if centralBaseline && lineIsSidewaysResolved(line.Results) {
			centralBaseline = false
		}
		// The available size inline-level relative offsets resolve against:
		// the inline formatting context's, the block's child available size
		// (ComputeRelativeOffsetForInline reads space.AvailableSize,
		// relative_utils.cc:100-124 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
		// as for a block-in-inline (adoptBlockInInlineResult).
		relativeAvailableSize := bla.containerBuilder.ChildAvailableSize()
		// CSS Pseudo 4 §3.2.1: the ::first-line generated box "behaves similar to
		// that of an inline-level element" (the WPT first-line-line-height
		// reftests model it as a `<span class="fl">` inside an unchanged block).
		// So the first line's STRUT — the anonymous root inline box, sized by
		// computeLineMetricsEx's parentStyle — keeps the BLOCK's font and
		// line-height; the ::first-line styles ride on the line's text items via
		// their per-result EffectiveStyle (set by applyFirstLineStyles), which
		// grow the line when the first-line font/line-height is TALLER than the
		// block (test 001) but never shrink it below the block strut when it is
		// SHORTER (test 002). Merging ::first-line line-height into the strut
		// here would wrongly collapse the first line to the smaller height.
		// Mirrors Blink keeping the block-flow strut (bla.style) while the
		// first-line ComputedStyle applies to the inline content.
		var firstLineBgStyle *css.Style
		if isFirstLineForBox {
			firstLineBgStyle = bla.node.FirstLineStyle
		}
		lineFragment, lineHeight, lineAscent, residualStack, lineOOFCandidates := createLineBoxEx(
			itemsData, &line, effectiveWDM, lineVisualInline, fonts, centralBaseline, relativeAvailableSize, bla.style, openInlineStack, firstLineBgStyle, isFirstLineForCreate, atomicInlineContainers,
		)
		openInlineStack = residualStack
		// no Blink analog for the line box type: a zero-height line stands
		// in for PhysicalLineBoxFragment::IsEmptyLineBox.
		isEmptyLineBox := lineHeight == 0
		clearanceAfterLine, lineStatus := 0.0, LayoutResultSuccess
		if !isEmptyLineBox {
			clearanceAfterLine, lineStatus = addAnyClearanceAfterLine(
				lb.space, lastLogicalItem, bfcBlockOrigin+blockOffset+lineHeight)
		}
		// The break-between value and container separation of a break
		// before this line (BreakBeforeChildIfNeeded's inputs, below). A
		// line pushed down by floats is separated from the container
		// (Blink HandleInflow, block_layout_algorithm.cc:2616-2626 @
		// a9f50e522efa). Not ported: the container's own IsPushedByFloats
		// exception (louis14 has no builder flag).
		breakBetween := CalculateBreakBetweenValue(inlineChild, nil, bla.containerBuilder)
		hasContainerSeparation := hasBreakOpportunityBeforeNextChild || blockOffset > lineStartBlockOffset

		// A forced break between the previous in-flow child (a
		// block-in-inline, whose break-after the builder holds) and this
		// line breaks before the line, given container separation. Mirrors
		// BreakBeforeChildIfNeeded's forced-break branch for an inline child
		// (block_layout_algorithm.cc:3144-3170 with
		// CalculateBreakBetweenValue, fragmentation_utils.cc:133-136 @
		// a9f50e522efa).
		if bla.space.HasBlockFragmentation && !isEmptyLineBox && hasBreakOpportunityBeforeNextChild &&
			IsForcedBreakValue(bla.space, breakBetween) {
			breakBeforeLine(bla.createInlineBreakToken(blockOffset, lineStartIdx, lineStartTextOffset))
			inlineBreakToken.IsForcedBreak = true
			break
		}

		// Block fragmentation: check if this line fits in the current fragmentainer.
		// Mirrors Blink's BlockLayoutAlgorithm::BreakBeforeChildIfNeeded for a
		// line box child (block_layout_algorithm.cc:3144-3289 @ a9f50e522efa).
		// Only check when we have a real (non-indefinite) fragmentainer block-size and
		// this is NOT the initial balancing pass (which runs unconstrained to measure
		// total content height).
		// A relayout breaks before the predetermined line. Mirrors the
		// early-break branch of BlockLayoutAlgorithm::Layout
		// (block_layout_algorithm.cc:1074-1089 @ a9f50e522efa):
		// AddBreakBeforeChild with perfect appeal, then
		// ConsumeRemainingFragmentainerSpace (the caller's
		// setBreakingSize). No space shortage is propagated.
		if bla.space.HasKnownFragmentainerBlockSize() &&
			bla.earlyBreak != nil && IsEarlyBreakTarget(bla.earlyBreak, bla.containerBuilder, nil) {
			bla.containerBuilder.ClampBreakAppeal(bla.earlyBreak.GetBreakAppeal())
			breakBeforeLine(bla.createInlineBreakToken(blockOffset, lineStartIdx, lineStartTextOffset))
			break
		}

		// The line aborted (a <br> needing clearance past the
		// fragmentainer): there is no fragment to move past, so break
		// before it, without considering orphans and widows, and retry it
		// in the next fragmentainer. Mirrors BlockLayoutAlgorithm::
		// BreakBeforeChildIfNeeded for a non-success line result
		// (block_layout_algorithm.cc:3176-3181, :3187-3204, :3279-3289) with
		// MovePastBreakpoint's abort branch (fragmentation_utils.cc:
		// 1077-1084) and CalculateSpaceShortage's indefinite shortage for
		// an aborted result (:1037-1040) @ a9f50e522efa; AttemptSoftBreak
		// always succeeds here (see breakBeforeLine).
		if lineStatus != LayoutResultSuccess {
			bla.containerBuilder.SetBreakAppeal(CalculateBreakAppealBeforeWithStatus(
				bla.space, lineStatus, breakBetween, hasContainerSeparation,
				IsBreakableAtStartOfResumedContainerWithFirstForNode(bla.space, lineFragment.IsFirstForNode())))
			breakBeforeLine(bla.createInlineBreakToken(blockOffset, lineStartIdx, lineStartTextOffset))
			break
		}

		if bla.space.HasKnownFragmentainerBlockSize() {
			// blockOffset is content-relative: measure the boundary and the
			// "no progress yet" guard from the content edge's fragmentainer
			// offset, so the container's own block-start border/padding both
			// consumes fragmentainer space and counts as progress on a first
			// fragment (and, being cleared on a continuation, does not). Blink
			// reaches BreakBeforeChildIfNeeded for line boxes through
			// FinishInflow (block_layout_algorithm.cc:2634 @ a9f50e522efa)
			// with the same content-edge origin as block children
			// (FragmentainerOffsetAtBfc + bfc_block_offset, :3156-3158); the
			// guard is MovePastBreakpoint's refuse_break_before, space_left >=
			// fragmentainer_block_size (fragmentation_utils.cc:1169-1171).
			fragEnd := bla.fragmentainerCapacityForChildren() - bla.fragmentainerOffsetForChildren
			// shortage is how much more space would let this line fit here.
			shortage := blockOffset + lineHeight - fragEnd
			if shortage > 0 && bla.fragmentainerOffsetForChildren+blockOffset > 0 {
				// This line overflows: MovePastBreakpoint failed. Before
				// breaking, honor orphans and widows (or detect violations);
				// breakBeforeLine reports whether to break before this line
				// or to keep laying out lines past the fragmentainer end.
				// Per Blink: shortage is the minimum additional space needed
				// to fit the overflowing line (per-break-point), not the
				// cumulative overflow across all fragmentainers.
				// The appeal of breaking before this line: last resort
				// without container separation, so the parent prefers a
				// break before this block when it has one. Blink
				// BreakBeforeChildIfNeeded → CalculateBreakAppealBefore,
				// whose break-between for an inline child is the joined
				// value (fragmentation_utils.cc:133-136, :235-266), recorded
				// by BreakBeforeChild (:944-1014) @ a9f50e522efa. A line
				// carries no break-before of its own; a block-in-inline's
				// arrives through the builder's previous break-after.
				appealBefore := CalculateBreakAppealBeforeWithStatus(
					bla.space, LayoutResultSuccess, breakBetween, hasContainerSeparation,
					IsBreakableAtStartOfResumedContainerWithFirstForNode(bla.space, lineFragment.IsFirstForNode()))
				if bla.breakBeforeLine(&appealBefore, shortage, isEmptyLineBox) {
					bla.containerBuilder.SetBreakAppeal(appealBefore)
					// Breaking before the line reports how much more
					// space it needed (BreakBeforeChild →
					// PropagateSpaceShortage, fragmentation_utils.cc:
					// 944-968 @ a9f50e522efa).
					if bla.space.BlockFragmentationType == FragmentColumn {
						bla.containerBuilder.PropagateSpaceShortage(shortage)
					}
					breakBeforeLine(bla.createInlineBreakToken(blockOffset, lineStartIdx, lineStartTextOffset))
					break
				}
			} else if shortage > 0 && bla.space.BlockFragmentationType == FragmentColumn {
				// A line that does not fit is moved past anyway when breaking
				// before it would make no progress (the first line in the
				// fragmentainer). Report the space shortage so column balancing
				// can stretch to fit the unbreakable line. Mirrors
				// MovePastBreakpoint's move-past branch → PropagateSpaceShortage
				// (fragmentation_utils.cc:1232-1243, :995-1014 @ a9f50e522efa),
				// reached for line boxes through BlockLayoutAlgorithm::
				// BreakBeforeChildIfNeeded (block_layout_algorithm.cc:3178-3181).
				bla.containerBuilder.PropagateSpaceShortage(shortage)
			}
		}

		// The first line box claims a pending outside list marker; a marker
		// taller than the line pushes the line, and so every later line,
		// down. Placed after the line's break check and before it is added,
		// as Blink's PositionOrPropagateListMarker adjusts the line's
		// offset after BreakBeforeChildIfNeeded (block_layout_algorithm.cc:
		// 2634, :2720-2724 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		if firstLineAscent < 0 {
			var contentPush float64
			if lineAscent > 0 {
				contentPush = bla.placeListMarkerAtFirstLine(lineAscent, blockOffset)
				blockOffset += contentPush
			}
			firstLineAscent = lineAscent + contentPush
		}
		// Track the last line's baseline offset from the content area start.
		// This is the block offset of the line + the line's ascent.
		lastBaselineOffset = blockOffset + lineAscent

		lineOffset := LogicalOffset{
			InlineOffset: lineInlineOffset,
			BlockOffset:  blockOffset,
		}
		bla.containerBuilder.AddChild(lineFragment, lineOffset)
		if bla.space.HasBlockFragmentation && !isEmptyLineBox {
			// Count the line for orphans / widows unless it is empty.
			// Mirrors FragmentBuilder::PropagateFromFragment
			// (fragment_builder.cc:487-492 @ a9f50e522efa).
			bla.containerBuilder.SetLineCount(bla.containerBuilder.LineCount() + 1)
		}
		// Blink AddResult → PropagateBreakInfo (box_fragment_builder.cc:
		// 227-229, :479-489 @ a9f50e522efa): a line box records no
		// BlockSizeForFragmentation, so it contributes its own block-size
		// (fragmentation_utils.cc:1545-1583), not its glyphs' overhang.
		bla.containerBuilder.PropagateChildBlockSizeForFragmentation(&LayoutResult{Fragment: lineFragment}, lineOffset)
		if bla.space.HasBlockFragmentation && !hasBreakOpportunityBeforeNextChild {
			hasBreakOpportunityBeforeNextChild = HasBreakOpportunityBeforeNextChild(wdm, lineFragment, nil)
		}

		// Propagate OOF candidates collected from atomic inline children
		// (e.g. abs-pos descendants of inline-blocks) to the parent block.
		// Adjust static positions by the line's offset within the block.
		for _, cand := range lineOOFCandidates {
			adj := cand
			adj.StaticPosition.Offset.InlineOffset += lineInlineOffset
			adj.StaticPosition.Offset.BlockOffset += blockOffset
			bla.containerBuilder.AddOutOfFlowCandidate(adj)
		}

		// Emit deferred block-level OOF candidates at the block-end of the
		// line box. Per Blink's InlineLayoutAlgorithm::HandleOutOfFlowPositioned,
		// a block-level abspos encountered AFTER in-flow content on the line
		// receives static position (inline-start, line_block_end) — if it had
		// been in flow it would have started a new block-flow line below the
		// current line box.
		for _, d := range blockLevelOOFOnLine {
			bla.containerBuilder.AddOutOfFlowCandidate(inlineOOFCandidate(
				d.node, 0, blockOffset+lineHeight, d.isFixed, d.inlineContainer,
			))
		}

		// During the initial column-balancing pass, a line box is an
		// unbreakable piece of content: line boxes are monolithic
		// (PhysicalFragment::IsMonolithic, physical_fragment.cc:586-596 @
		// a9f50e522efa; a block-in-inline is the block's own in-flow child
		// here, never inside a line), so
		// ShouldAvoidBreakInside holds and BreakBeforeChildIfNeeded floors
		// the column block-size at the line's block-size
		// (fragmentation_utils.cc:1110-1118, CalculateUnbreakableBlockSize
		// :979-992). During that pass the fragmentainer block-offset is
		// never negative, so the unbreakable size is the line's own.
		if bla.space.IsInitialColumnBalancingPass {
			bla.containerBuilder.PropagateTallestUnbreakableBlockSize(lineHeight)
		}

		// A <br> clear's clearance goes after the line, pushing what follows
		// (and the container's block-end) past the floats. Blink
		// ComputeInflowPosition (block_layout_algorithm.cc:2955-2980 @
		// a9f50e522efa); louis14 folds annotation overflow into lineHeight,
		// so there is no AnnotationOverflow to take the max with.
		blockOffset += lineHeight + clearanceAfterLine
		lineStartBlockOffset = blockOffset
	}

	if firstLineAscent < 0 {
		firstLineAscent = 0
	}
	return blockOffset, exclusionSpace, firstLineAscent, lastBaselineOffset, inlineBreakToken
}

// addAnyClearanceAfterLine returns the clearance to add after a line laid
// out in lineSpace (whose exclusion space is the one at the line), whose
// last item (in logical order) is lastItem and whose block-end is at BFC
// block offset lineBfcBlockEnd: a <br> with clear pushes what follows past
// the floats it clears. The status is LayoutResultOutOfFragmentainerSpace
// when, under block fragmentation, that clearance has to go past a float
// continuing in the next fragmentainer; the line then breaks before.
// Mirrors Blink's InlineLayoutAlgorithm::AddAnyClearanceAfterLine
// (inline/inline_layout_algorithm.cc:1021-1065 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose false return aborts the
// line with kOutOfFragmentainerSpace (:1501-1503). Not ported: the
// text-box-trim TrimBlockEndBy adjustment of the line's content size.
func addAnyClearanceAfterLine(
	lineSpace ConstraintSpace, lastItem *InlineItem, lineBfcBlockEnd float64,
) (clearanceAfterLine float64, status EStatus) {
	if !isBRItem(lastItem) {
		return 0, LayoutResultSuccess
	}
	// Clearance only ever moves the offset down (Blink AdjustToClearance).
	cleared := lineSpace.ExclusionSpace.ClearanceOffset(lastItem.Style.GetClear(),
		layoutunit.FromFloat64Round(lineBfcBlockEnd), lineSpace.WritingDirection).Float64()
	clearanceAfterLine = max(cleared-lineBfcBlockEnd, 0)
	if brNeedsClearancePastFragmentainer(lineSpace, lastItem) {
		return clearanceAfterLine, LayoutResultOutOfFragmentainerSpace
	}
	return clearanceAfterLine, LayoutResultSuccess
}

// newInlineUnpositionedFloat returns the UnpositionedFloat for a float of
// this block's inline formatting context, with its origin at the line's
// BFC block offset originBfcBlockOffset: the content box inline-size is its
// available and percentage resolution inline-size, with an indefinite
// available block-size (LineBreaker::HandleFloat, inline/
// line_breaker.cc:3787-3796 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (bla *BlockLayoutAlgorithm) newInlineUnpositionedFloat(
	node *LayoutInputNode,
	token *BlockBreakToken,
	contentInlineSize, originBfcBlockOffset, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize float64,
) *UnpositionedFloat {
	return bla.newUnpositionedFloat(node, token,
		LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
		LogicalSize{InlineSize: contentInlineSize, BlockSize: 0},
		bfcInlineOrigin, originBfcBlockOffset, bfcBlockOrigin, bfcContainerInlineSize)
}

// placeInlineFloat positions a float of this block's inline formatting
// context through PositionFloat and adds it unless it breaks before; a
// float that breaks before or inside leaves a parallel-flow break token in
// context. Returns the new exclusion-space pointer and whether the float
// was placed (it was not when it broke before). Mirrors the
// positioning half of LineBreaker::HandleFloat and InlineLayoutAlgorithm::
// PlaceFloatingObjects (inline/line_breaker.cc:3815-3839, inline/
// inline_layout_algorithm.cc:875-903 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (bla *BlockLayoutAlgorithm) placeInlineFloat(
	item *InlineItem,
	uf *UnpositionedFloat,
	es *ExclusionSpace,
	bfcBlockOrigin, bfcInlineOrigin float64,
	context *InlineChildLayoutContext,
) (*ExclusionSpace, bool) {
	es, positioned, breakToken := bla.addPositionedFloat(uf, es, bfcBlockOrigin, bfcInlineOrigin)
	if positioned.BreakBeforeToken == nil {
		// A float paints inside the opacity group of its nearest enclosing
		// group inline (Blink: LayoutObject::PaintingLayer returns the
		// enclosing self-painting layer, layout_object.cc:1218 @ 4883d11f).
		positioned.LayoutResult.Fragment.PaintGroup = item.EnclosingPaintGroup
	}
	if breakToken != nil {
		context.PropagateParallelFlowBreakToken(breakToken)
	}
	return es, positioned.BreakBeforeToken == nil
}

// resumeInlineFloats resumes, first in this fragment, each float of this
// block's inline formatting context that broke before or inside in the
// previous fragmentainer, in its parallel flow at bfcBlockOffset. Mirrors
// LineBreaker::NextLine on an IsInParallelBlockFlow break token, which
// handles the float with its break token on an empty line (inline/
// line_breaker.cc:893-912 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (bla *BlockLayoutAlgorithm) resumeInlineFloats(
	wdm WritingDirectionMode,
	contentInlineSize float64,
	es *ExclusionSpace,
	bfcBlockOffset, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize float64,
	context *InlineChildLayoutContext,
) *ExclusionSpace {
	incoming := bla.space.BreakToken
	if incoming == nil || len(incoming.ChildBreakTokens) == 0 {
		return es
	}
	itemsData, _, _ := bla.prepareInlineItems(wdm)
	for _, tok := range incoming.ChildBreakTokens {
		idx := inlineItemIndex(itemsData.Items, InlineItemFloat, tok.Node)
		if idx < 0 {
			continue
		}
		if es == nil {
			es = &ExclusionSpace{}
		}
		uf := bla.newInlineUnpositionedFloat(tok.Node, tok, contentInlineSize, bfcBlockOffset,
			bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize)
		es, _ = bla.placeInlineFloat(itemsData.Items[idx], uf, es, bfcBlockOrigin, bfcInlineOrigin, context)
	}
	return es
}

// inlineItems is the prepared inline formatting context of a
// BlockLayoutAlgorithm: collected once per layout and shared by every run of
// lines and block-in-inline child (Blink keeps it on the InlineNode,
// InlineNode::ItemsData), with what the runs and wrappers derive from the
// whole item list, so none of them rescans it (LOU-510).
type inlineItems struct {
	data        *InlineItemsData
	baseDir     Direction
	isPlaintext bool

	// positionedInlineMap and atomicInlineContainers: BuildPositionedInlineMap
	// over all items (an item's entry depends only on the items before it).
	positionedInlineMap    map[*InlineItem]*html.Node
	atomicInlineContainers map[*InlineItem]AtomicInlineContainers
	// blockInInline maps each block-in-inline wrapper to its item.
	blockInInline map[*LayoutInputNode]blockInInlineItem

	// floats are the measured floats (inlineFloats, built on first use).
	floats              map[*InlineItem]*pendingFloat
	initialLetterAdjust float64
}

// blockInInlineItem is a block-in-inline wrapper's item index and the
// inline boxes open around it, outermost first (rebuildOpenInlineStack of
// the items before it).
type blockInInlineItem struct {
	index           int
	openInlineStack []*InlineItem
}

// pendingFloat is a float of the inline formatting context, measured (laid
// out without fragmentation) for its inline-size; positioning lays it out
// again with the fragmentainer when fragmented (LineBreaker::HandleFloat's
// UnpositionedFloat and ComputeMarginBoxInlineSizeForUnpositionedFloat,
// inline/line_breaker.cc:3787-3800, :3673-3684 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type pendingFloat struct {
	item                *InlineItem
	unpositioned        *UnpositionedFloat
	marginBoxInlineSize float64
	isInitialLetter     bool
}

// prepareInlineItems collects this block's inline items and resolves their
// bidi levels, once per layout.
func (bla *BlockLayoutAlgorithm) prepareInlineItems(wdm WritingDirectionMode) (*InlineItemsData, Direction, bool) {
	if c := bla.inlineItemsCache; c != nil {
		return c.data, c.baseDir, c.isPlaintext
	}
	itemsData, baseDir, isPlaintext := bla.collectInlineItems(wdm)
	c := &inlineItems{data: itemsData, baseDir: baseDir, isPlaintext: isPlaintext}
	c.positionedInlineMap, c.atomicInlineContainers = BuildPositionedInlineMap(itemsData.Items)
	var open []*InlineItem
	for i, item := range itemsData.Items {
		switch item.Type {
		case InlineItemOpenTag:
			open = append(open, item)
		case InlineItemCloseTag:
			if n := len(open); n > 0 {
				open = open[:n-1]
			}
		case InlineItemBlockInInline:
			if c.blockInInline == nil {
				c.blockInInline = map[*LayoutInputNode]blockInInlineItem{}
			}
			c.blockInInline[item.LayoutNode] = blockInInlineItem{index: i, openInlineStack: slices.Clone(open)}
		}
	}
	bla.inlineItemsCache = c
	return itemsData, baseDir, isPlaintext
}

// inlineFloats returns the floats of this block's inline formatting
// context, measured on first use, and the block-start shift of the text
// for a raised initial letter. LOU-289 part 3: an initial-letter
// ::first-letter float (CSS Inline 3 §7.3) with `raise` / explicit-sink
// shifts the surrounding paragraph text DOWN by block_start_adjust =
// lineHeight*(size-sink), while the letter box itself stays at the block's
// visual top. Mirrors Blink LineInfo::SetInitialLetterBlockStartAdjustment
// (initial_letter_utils.cc:270-300 @ 4883d11fef). The drop case
// (sink==size) yields 0 → no shift. prepareInlineItems must have run.
func (bla *BlockLayoutAlgorithm) inlineFloats(contentInlineSize, bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize float64) (map[*InlineItem]*pendingFloat, float64) {
	c := bla.inlineItemsCache
	if c.floats != nil {
		return c.floats, c.initialLetterAdjust
	}
	c.floats = map[*InlineItem]*pendingFloat{}
	paraLineHeight := 0.0
	if bla.style != nil {
		paraLineHeight = bla.style.GetLineHeight()
	}
	for _, item := range c.data.Items {
		if item.Type != InlineItemFloat || item.LayoutNode == nil || item.Style == nil {
			continue
		}
		il := item.Style.GetInitialLetter()
		c.initialLetterAdjust = max(c.initialLetterAdjust, initialLetterTextShift(il, paraLineHeight))
		uf := bla.newInlineUnpositionedFloat(item.LayoutNode, nil, contentInlineSize, 0,
			bfcBlockOrigin, bfcInlineOrigin, bfcContainerInlineSize)
		c.floats[item] = &pendingFloat{
			item:                item,
			unpositioned:        uf,
			marginBoxInlineSize: ComputeMarginBoxInlineSizeForUnpositionedFloat(uf),
			isInitialLetter:     il.Set,
		}
	}
	return c.floats, c.initialLetterAdjust
}

func (bla *BlockLayoutAlgorithm) collectInlineItems(wdm WritingDirectionMode) (*InlineItemsData, Direction, bool) {
	// Phase 1: Collect inline items from the layout subtree.
	itemsData := CollectInlines(bla.node)

	if len(itemsData.Items) == 0 {
		return itemsData, wdm.Dir, false
	}

	// Phase 1a: (removed) ::marker pseudo-element for inside markers is now
	// handled as a proper layout node child of the list item — the marker
	// text flows through the normal inline pipeline via the ::marker inline
	// element child created in LayoutTreeBuilder.createMarkerPseudoElement.

	// Phase 1b: Block-level bidi control injection.
	// CSS Writing Modes §2.2: When a block container has unicode-bidi set
	// to embed, isolate, bidi-override, isolate-override, or plaintext,
	// the corresponding Unicode bidi control characters must be injected
	// around its inline content so the UAX#9 algorithm resolves levels
	// correctly. This mirrors Blink's InlineItemsBuilder which checks the
	// block container's own unicode-bidi before processing children.
	//
	// The inline-level injection (in collectInlinesRecursive via
	// injectBidiControlChars) only handles inline elements. Block
	// containers need their own injection here.
	injectBlockBidiControls(bla.style, itemsData)

	// Phase 1b: Bidi pipeline (mirrors Blink's BidiParagraph + SegmentText).
	// Uses a pure-Go UAX#9 resolver (the Go bidi package has a neutral
	// resolution bug), then strips control chars and splits at level
	// boundaries for correct L2 reordering.
	//
	// Per CSS Writing Modes §2.2, when the block container has
	// unicode-bidi: plaintext, each bidi paragraph (separated by forced
	// breaks) independently determines its base direction from the first
	// strong character (UAX#9 P2/P3). This mirrors Blink's NGBidiParagraph
	// which calls ICU with UBIDI_DEFAULT_LTR per paragraph for plaintext.
	baseDir := wdm.Dir
	isPlaintext := false
	if bla.style != nil {
		if bidi, ok := bla.style.Get("unicode-bidi"); ok && bidi == "plaintext" {
			isPlaintext = true
		}
	}
	if isPlaintext {
		ResolveBidiLevelsPlaintext(itemsData)
		// Compute overall baseDir for fallback (first paragraph's direction).
		runes := []rune(itemsData.TextContent)
		if determineFSIDirection(runes) == 1 {
			baseDir = DirectionRTL
		} else {
			baseDir = DirectionLTR
		}
	} else {
		ResolveBidiLevels(itemsData, baseDir)
	}
	StripBidiControls(itemsData)
	SplitItemsAtLevelBoundaries(itemsData)
	return itemsData, baseDir, isPlaintext
}

// inlineSegment selects the run of lines layoutInlineChildren lays out: in
// an inline formatting context with block-in-inline items, the lines
// between two of them. no Blink analog: Blink's BlockLayoutAlgorithm lays
// out an inline formatting context one line at a time as in-flow children
// (HandleInflow on the InlineNode); louis14 lays out the lines up to the
// next block-in-inline in one call.
type inlineSegment struct {
	// blockStart is the content-box block offset of the first line.
	blockStart float64
	// hasBreakOpportunityBefore: an in-flow child before the run left a
	// breakpoint before the first line.
	hasBreakOpportunityBefore bool
	// node is the run's pseudo-child (blockInInlineChildren); nil when the
	// inline formatting context has no block-in-inline.
	node *LayoutInputNode
}

// after returns the block-in-inline wrapper the run follows; nil for the
// run that starts the inline formatting context.
func (s inlineSegment) after() *LayoutInputNode {
	if s.node == nil {
		return nil
	}
	return s.node.inlineSegmentAfter
}

// inlineItemIndex returns the index of the item of type typ generated by
// node, or -1.
func inlineItemIndex(items []*InlineItem, typ InlineItemType, node *LayoutInputNode) int {
	return slices.IndexFunc(items, func(item *InlineItem) bool {
		return item.Type == typ && item.LayoutNode == node
	})
}

// rebuildOpenInlineStack returns the inline boxes still open after items,
// outermost first: the boxes a line starting after items continues. Mirrors
// InlineLayoutStateStack rebuilding its box states from the items before
// the line (InlineLayoutStateStack::RebuildBoxStates called from
// InlineLayoutAlgorithm, inline/inline_layout_algorithm.cc:300 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func rebuildOpenInlineStack(items []*InlineItem) []*InlineItem {
	var stack []*InlineItem
	for _, item := range items {
		switch item.Type {
		case InlineItemOpenTag:
			stack = append(stack, item)
		case InlineItemCloseTag:
			if n := len(stack); n > 0 {
				stack = stack[:n-1]
			}
		}
	}
	return stack
}

// createInlineBreakToken returns the break token for breaking before the
// line that starts at item lineStartIdx / text offset lineStartTextOffset,
// blockOffset into this block's content.
func (bla *BlockLayoutAlgorithm) createInlineBreakToken(blockOffset float64, lineStartIdx, lineStartTextOffset int) *BlockBreakToken {
	token := &BlockBreakToken{
		Node: bla.node,
		// Consumed is what this block laid out (its content up to the
		// break), like the block-child token; the container's fragmentainer
		// offset is not part of it.
		ConsumedBlockSize:    layoutunit.FromFloat64Round(blockOffset),
		InlineItemStartIndex: lineStartIdx,
		InlineTextOffset:     lineStartTextOffset,
		SequenceNumber:       sequenceNumberAfter(bla.space.BreakToken),
	}
	if bla.space.BreakToken != nil {
		token.ConsumedBlockSize = token.ConsumedBlockSize.Add(bla.space.BreakToken.ConsumedBlockSize)
	}
	return token
}

// breakBeforeLine decides, for a line box that does not fit and may be
// broken before, whether to break before it (true) or to keep laying out
// lines past the fragmentainer end to find out where widows let the break
// go (false). It lowers *appealBefore when breaking here leaves too few
// lines before the break. Mirrors the line-box part of Blink's
// BlockLayoutAlgorithm::BreakBeforeChildIfNeeded (block_layout_algorithm.cc:
// 3187-3279 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); AttemptSoftBreak
// (:3281-3285) always succeeds: an inline formatting context records no
// earlier breakpoint while laying out its lines (UpdateEarlyBreakBetweenLines
// runs once they are all laid out; a block-in-inline is laid out by the
// block's child walk, not among the lines). Not
// ported: the BR clear=all status check and text-box-trim's
// fragmentainer-end exception.
func (bla *BlockLayoutAlgorithm) breakBeforeLine(appealBefore *BreakAppeal, shortage float64, isEmptyLineBox bool) bool {
	if bla.firstOverflowingLine == 0 && bla.space.BlockFragmentationType == FragmentColumn {
		// The first overflowing line reports its shortage before knowing
		// whether breaking here violates orphans / widows; the line finally
		// broken before reports its own as well (the caller).
		// PropagateSpaceShortage (fragmentation_utils.cc:995-1014).
		bla.containerBuilder.PropagateSpaceShortage(shortage)
	}
	lineCount := bla.containerBuilder.LineCount()
	if lineCount == 0 {
		return true
	}
	if bla.firstOverflowingLine == 0 {
		bla.firstOverflowingLine = lineCount
	}
	// Lines needed before the break: orphans. A resumed fragment has a
	// break before it too, so its leading lines are also the previous
	// fragment's widows.
	minimumLineCount := bla.style.GetOrphans()
	if isFirstFragment := bla.space.BreakToken == nil; !isFirstFragment {
		minimumLineCount = max(minimumLineCount, bla.style.GetWidows())
	}
	if lineCount < minimumLineCount {
		// Not enough orphans. Breaking before this block might do better,
		// which only the parent can tell: permit the break, but mark it.
		*appealBefore = min(*appealBefore, BreakAppealViolatingOrphansAndWidows)
	} else {
		// Enough lines before the break. Keep laying out lines until there
		// are enough after it for widows (empty lines do not count).
		widowsFound := lineCount - bla.firstOverflowingLine + 1
		if widowsFound < bla.style.GetWidows() || isEmptyLineBox {
			return false
		}
	}
	bla.fitAllLines = true
	return true
}

// lineEarlyBreak returns where to break instead, when the inline content
// overflowed at firstOverflowingLine but more lines were laid out (to
// honor widows): at the first overflowing line when there are enough widows
// after it, or else at the line that best balances orphans and widows.
// Mirrors the inline formatting context part of Blink's
// BlockLayoutAlgorithm::FinalizeForFragmentation (block_layout_algorithm.cc:
// 3106-3127 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (bla *BlockLayoutAlgorithm) lineEarlyBreak() *EarlyBreak {
	if bla.earlyBreak != nil || !bla.space.HasBlockFragmentation ||
		bla.firstOverflowingLine == 0 || bla.firstOverflowingLine >= bla.containerBuilder.LineCount() {
		return nil
	}
	lineNumber := bla.firstOverflowingLine
	if !bla.fitAllLines {
		// All the lines were laid out, so there will not be enough widows
		// unless the break goes earlier than where space ran out.
		lineCount := bla.containerBuilder.LineCount()
		lineNumber = max(lineCount-bla.style.GetWidows(), min(lineCount, bla.style.GetOrphans()))
	}
	return NewLineEarlyBreak(lineNumber, BreakAppealPerfect)
}

// UpdateEarlyBreakBetweenLines records the best place to break between two
// of this block's lines, when they all fit: the line that best honors
// orphans and widows, or, if they cannot both be honored, before the last
// line with ViolatingOrphansAndWidows appeal. A parent that later runs out
// of space at a worse breakpoint comes back to it. Mirrors Blink's
// BlockLayoutAlgorithm::UpdateEarlyBreakBetweenLines
// (block_layout_algorithm.cc:3291-3325 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (bla *BlockLayoutAlgorithm) UpdateEarlyBreakBetweenLines() {
	lineCount := bla.containerBuilder.LineCount()
	if lineCount < 2 {
		return
	}
	orphans, widows := bla.style.GetOrphans(), bla.style.GetWidows()
	lineNumber := max(lineCount-widows, min(lineCount-1, orphans))
	appeal := BreakAppealPerfect
	if lineNumber < orphans || lineCount-lineNumber < widows {
		// Too few lines for orphans and / or widows: breaking before the
		// last line fits as much as possible.
		lineNumber = lineCount - 1
		appeal = BreakAppealViolatingOrphansAndWidows
	}
	if bla.containerBuilder.HasEarlyBreak() && bla.containerBuilder.GetEarlyBreak().GetBreakAppeal() > appeal {
		return
	}
	bla.containerBuilder.SetEarlyBreak(NewLineEarlyBreak(lineNumber, appeal))
}

// relayoutAndBreakEarlier lays this block out again, breaking at
// earlyBreak. Mirrors Blink's LayoutAlgorithm::RelayoutAndBreakEarlier
// (layout_algorithm.h:272-281 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// a fresh algorithm with the same input, box type and the early break, which
// repeats the space shortage this pass's builder found (SetupRelayoutData,
// :200-224).
func (bla *BlockLayoutAlgorithm) relayoutAndBreakEarlier(earlyBreak *EarlyBreak) *LayoutResult {
	space := bla.space
	space.EarlyBreak = earlyBreak
	relayout := NewBlockLayoutAlgorithm(bla.ctx, bla.node, space)
	relayout.SetBoxType(bla.boxType)
	relayout.relayoutSpaceShortage = bla.containerBuilder.minimalSpaceShortage
	return relayout.Layout()
}

// relayoutWithoutFragmentation lays this block out again with block
// fragmentation disabled, letting clipped child content overflow past the
// fragmentation line instead of breaking: a block-axis clipping box reached
// its end with content still breaking inside it. Mirrors Blink's
// LayoutAlgorithm::RelayoutWithoutFragmentation (layout_algorithm.h:283-291
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) → Relayout (:227-265): a fresh
// algorithm over ConstraintSpace::CloneWithoutFragmentation, with the same
// break token, box type and early break (which rides on the space).
func (bla *BlockLayoutAlgorithm) relayoutWithoutFragmentation() *LayoutResult {
	relayout := NewBlockLayoutAlgorithm(bla.ctx, bla.node, bla.space.CloneWithoutFragmentation())
	relayout.SetBoxType(bla.boxType)
	return relayout.Layout()
}

// firstLineAllowedProperties lists the CSS properties that ::first-line is
// allowed to override. Per CSS Pseudo-Elements Level 4 §3.2.1 the spec
// allow-list covers font/color/background/decoration/spacing/transform/
// vertical-align. Blink (and Firefox) additionally honor `opacity` on
// ::first-line — see `core/style/computed_style.cc::ApplyFirstLineStyle`
// @ 4883d11fef and the WPT test css-pseudo/first-line-opacity-001.html
// which depends on it.
var firstLineAllowedProperties = []string{
	// Font properties (CSS Pseudo-Elements 4 §7.1.1: all font-* properties
	// apply to ::first-line, including the four font-synthesis-* longhands
	// from CSS Fonts 4 §6.6).
	"font-family", "font-size", "font-style", "font-weight",
	"font-variant", "font-stretch", "font",
	"font-variant-caps", "font-variant-ligatures", "font-variant-numeric",
	"font-feature-settings", "font-optical-sizing", "font-size-adjust",
	"font-synthesis-weight", "font-synthesis-style",
	"font-synthesis-small-caps", "font-synthesis-position",
	// Color and background
	"color", "background", "background-color", "background-image",
	"background-repeat", "background-position", "background-attachment",
	"background-size", "background-origin", "background-clip",
	// Text decoration
	"text-decoration", "text-decoration-color", "text-decoration-line",
	"text-decoration-style",
	// Spacing and line
	"letter-spacing", "word-spacing", "line-height",
	// Text transform
	"text-transform",
	// Vertical align (for inline)
	"vertical-align",
	// Blink/Firefox extension: opacity on ::first-line. CSS Pseudo 4 omits
	// it from the allow-list, but every shipping engine honors it.
	"opacity",
}

// mergeFirstLineStyle returns base with the allowed ::first-line properties
// from firstLine merged in. Returns base unchanged when firstLine is nil or
// declares no allowed properties. Mirrors Blink's
// ComputedStyle::ApplyFirstLineStyle but only for properties in the spec
// allow-list — anything outside (margin, padding, etc.) stays at base.
//
// fonts drives re-resolution of the merged style's cached font-relative
// metrics. base.Clone() copies base's UsedFontSize / ChWidth / XHeight /
// CapHeight / IcWidth / LhSize, which GetUsedFontSize and the ch/ex/ic/cap/lh
// unit resolvers read in preference to the raw Properties. When the merge
// overrides font-size or line-height, those caches go stale (the base font's
// size wins over the overridden Properties), so we re-build the merged font
// and lh size — the same Style.UpdateFont / UpdateLineHeight step the cascade
// runs for every element (StyleResolverState::UpdateFont / UpdateLineHeight,
// core/css/resolver/style_resolver_state.cc:553/565 @
// 5992657f556bcd9d207a4aca187488462f027cab). Blink's ::first-line
// ComputedStyle is itself a fully resolved style; this re-resolution is the
// louis14 analog.
func mergeFirstLineStyle(base, firstLine *css.Style, fonts text.FontConfig) *css.Style {
	return mergeFirstLineStyleExcept(base, firstLine, nil, fonts)
}

// mergeFirstLineStyleExcept is mergeFirstLineStyle with a per-property skip set.
// A property listed in `skip` is left at its `base` value rather than taking the
// ::first-line value. The skip set carries the allowed properties that the
// originating element specified directly — CSS Pseudo-4 §3.2.1: the ::first-line
// box "behaves similar to that of an inline-level element", so its inherited
// properties reach descendants only via inheritance and a descendant's own
// specified value wins. Mirrors Blink's StyleResolver::StyleForFirstLineStyle,
// which re-resolves each descendant with the ::first-line rules folded into the
// cascade so a directly-specified declaration outranks the inherited
// ::first-line value (core/css/resolver/style_resolver.cc +
// core/style/computed_style.cc::ApplyFirstLineStyle @ 4883d11fef).
func mergeFirstLineStyleExcept(base, firstLine *css.Style, skip map[string]bool, fonts text.FontConfig) *css.Style {
	if base == nil || firstLine == nil {
		return base
	}
	hasOverride := false
	for _, prop := range firstLineAllowedProperties {
		if skip[prop] {
			continue
		}
		if val, ok := firstLine.Properties[prop]; ok && val != "" {
			hasOverride = true
			break
		}
	}
	if !hasOverride {
		return base
	}
	merged := base.Clone()
	for _, prop := range firstLineAllowedProperties {
		if skip[prop] {
			continue
		}
		if val, ok := firstLine.Properties[prop]; ok && val != "" {
			merged.Properties[prop] = val
		}
	}
	merged.UpdateFont(text.NewCSSFontSelector(fonts))
	merged.UpdateLineHeight()
	return merged
}

// firstLineDeclaredProps returns the ::first-line-allowed properties the
// element declares itself and that the ::first-line style would otherwise
// override. Primary source is Style.CascadedProps — the cascade's record of
// the element's own declarations — which distinguishes a declared value from
// an inherited one even when the two are textually identical
// (first-line-change-inline-color.html: span declares the same green it
// would inherit). Mirrors Blink re-resolving each first-line inline with
// kPseudoIdFirstLineInherited so its own matched declarations re-apply over
// the inherited ::first-line values (LayoutObject::
// FirstLineStyleWithoutFallback, layout_object.cc:4386-4404 @ Chromium
// 906a32d8634edf17db094507908f439bd9b52de5).
//
// Styles built outside the element cascade (anonymous boxes, pseudo-element
// styles, hand-assembled styles) have nil CascadedProps; for those the
// LOU-179 value-comparison fallback below (firstLineDirectlySpecified)
// still applies.
func firstLineDeclaredProps(elem, parent, firstLine *css.Style) map[string]bool {
	if elem == nil || firstLine == nil {
		return nil
	}
	if elem.CascadedProps == nil {
		return firstLineDirectlySpecified(elem, parent, firstLine)
	}
	var declared map[string]bool
	for _, prop := range firstLineAllowedProperties {
		flVal, ok := firstLine.Properties[prop]
		if !ok || flVal == "" {
			continue // ::first-line does not override this property
		}
		if !elem.CascadedProps[prop] {
			continue
		}
		if declared == nil {
			declared = make(map[string]bool)
		}
		declared[prop] = true
	}
	return declared
}

// firstLineDirectlySpecified is the value-comparison fallback used when
// Style.CascadedProps is unavailable (see firstLineDeclaredProps): a property
// counts as directly specified when the element's computed value differs from
// the value it inherits from its nearest ancestor element (`parent`). Only
// properties the ::first-line style would actually override are reported, so
// the caller's skip set stays minimal.
func firstLineDirectlySpecified(elem, parent, firstLine *css.Style) map[string]bool {
	if elem == nil || parent == nil || firstLine == nil {
		return nil
	}
	var specified map[string]bool
	for _, prop := range firstLineAllowedProperties {
		flVal, ok := firstLine.Properties[prop]
		if !ok || flVal == "" {
			continue // ::first-line does not override this property
		}
		if elem.Properties[prop] == parent.Properties[prop] {
			continue // inherited, not directly specified — ::first-line may apply
		}
		if specified == nil {
			specified = make(map[string]bool)
		}
		specified[prop] = true
	}
	return specified
}

// applyFirstLineStyles stores ::first-line pseudo-element overrides as a
// per-result Style on each item on the given line. Only the allowed subset of
// properties is applied. The underlying InlineItem is shared across lines and
// MUST NOT be mutated — the same InlineItem appearing on a later line keeps
// its original style. Mirrors Blink's FirstLineStyleIterator
// (`core/css/first_line_style_iterator.cc`), which yields a per-fragment
// first-line style without mutating the LayoutObject's stored style.
func applyFirstLineStyles(line *LineInfo, firstLineStyle, containerStyle *css.Style, openInlineStack []*InlineItem, fonts text.FontConfig) {
	if firstLineStyle == nil {
		return
	}

	// Apply overrides to each item on the line that has a style. Write into
	// the per-result Style override field; never mutate r.Item.Style itself.
	// mergeFirstLineStyleExcept clones the base, applies the allowed ::first-line
	// properties (minus the per-item skip set), and re-resolves the merged
	// font's metric caches — so the per-result style reports the first-line
	// font-size for both metrics and glyph rasterization (GetUsedFontSize). It
	// returns the base unchanged when no allowed property is overridden, so the
	// per-result Style stays nil in that case (EffectiveStyle falls back to
	// Item.Style).
	//
	// CSS Pseudo-4 §3.2.1: the ::first-line box behaves like an inline-level
	// element, so its properties reach descendants only via INHERITANCE.
	// Blink models this by re-resolving every inline on the first line with
	// its parent's first-line style as the inheritance parent
	// (StyleRequest(kPseudoIdFirstLineInherited, parent_first_line_style),
	// LayoutObject::FirstLineStyleWithoutFallback, layout_object.cc:4386-4404
	// @ Chromium 906a32d8634edf17db094507908f439bd9b52de5). Two consequences
	// the frame stack below mirrors:
	//
	//   - an element's own cascaded declaration outranks the inherited
	//     ::first-line value (firstLineDeclaredProps / Style.CascadedProps);
	//   - once an element declares a property, its whole inline subtree
	//     inherits THAT value, not the ::first-line value — the property is
	//     "blocked" for every nested descendant
	//     (first-line-change-inline-color-nested.html).
	//
	// Each frame carries the blocked set accumulated from the container down
	// to that element; an item's skip set is its element's blocked set. The
	// stack is seeded from openInlineStack (inline elements still open from
	// prior content) over the container style, then tracks open/close tags.
	type frame struct {
		style   *css.Style
		blocked map[string]bool
		// merged memoizes mergeFirstLineStyleExcept per base style at this
		// frame: items of one element commonly repeat (open/text/close), and
		// the merge clones + re-resolves font metrics on each call.
		merged map[*css.Style]*css.Style
	}
	pushFrame := func(stack []frame, style *css.Style) []frame {
		top := stack[len(stack)-1]
		blocked := top.blocked
		if declared := firstLineDeclaredProps(style, top.style, firstLineStyle); len(declared) > 0 {
			// Copy-on-extend: frames share the parent's blocked map unless
			// this element adds to it (the map doubles as a merge-cache key).
			blocked = make(map[string]bool, len(top.blocked)+len(declared))
			for p := range top.blocked {
				blocked[p] = true
			}
			for p := range declared {
				blocked[p] = true
			}
		}
		return append(stack, frame{style: style, blocked: blocked})
	}
	stack := make([]frame, 1, len(openInlineStack)+4)
	stack[0] = frame{style: containerStyle}
	for _, open := range openInlineStack {
		if open != nil && open.Style != nil {
			stack = pushFrame(stack, open.Style)
		}
	}

	for i := range line.Results {
		r := &line.Results[i]
		if r.Item == nil || r.Item.Style == nil {
			continue
		}
		// An open tag pushes its frame FIRST so the element's own item (and
		// its text/close items) all see the same blocked set — the element's
		// own declarations keep their base values through the skip.
		if r.Item.Type == InlineItemOpenTag {
			stack = pushFrame(stack, r.Item.Style)
		}
		// Only apply to text items and open/close tags (inline spans).
		switch r.Item.Type {
		case InlineItemText, InlineItemOpenTag, InlineItemCloseTag:
			top := &stack[len(stack)-1]
			merged, ok := top.merged[r.Item.Style]
			if !ok {
				merged = mergeFirstLineStyleExcept(r.Item.Style, firstLineStyle, top.blocked, fonts)
				if top.merged == nil {
					top.merged = make(map[*css.Style]*css.Style, 1)
				}
				top.merged[r.Item.Style] = merged
			}
			if merged != r.Item.Style {
				r.Style = merged
			}
		}
		if r.Item.Type == InlineItemCloseTag && len(stack) > 1 {
			stack = stack[:len(stack)-1]
		}
	}
}

// hasVisibleInlinePaint returns true if an inline element's style has
// visible paint properties: non-transparent background, visible border,
// or a non-none outline.
//
// Outline is included because CSS outline does not affect layout but IS painted
// by the renderer from the PaintLayer built for the span's box fragment. Without
// including outline here, a span with only `outline:` and no background/border
// never emits a box fragment from createLineBoxEx — so no PaintLayer is built
// and the outline is silently dropped. Mirrors Blink's decision to generate
// inline box fragments for any element with visible paint (including outline).
func hasVisibleInlinePaint(style *css.Style) bool {
	if style == nil {
		return false
	}
	if bg, ok := style.Get("background-color"); ok && bg != "" && bg != "transparent" {
		if c, ok := css.ParseColor(bg); ok && c.A > 0 {
			return true
		}
	}
	if _, ok := style.GetBackgroundImage(); ok {
		return true
	}
	bw := style.GetBorderWidth()
	if bw.Top > 0 || bw.Right > 0 || bw.Bottom > 0 || bw.Left > 0 {
		return true
	}
	// Outline: GetOutlineWidth returns 0 when outline-style is "none", so a
	// positive width implies a non-none style. The span must emit a fragment
	// so the renderer can build a PaintLayer and call drawOutline.
	return style.GetOutlineWidth() > 0
}

// lineIsSidewaysResolved reports whether every text/atomic run on the line has
// an effective text-orientation of "sideways". collectTextNode rewrites
// text-orientation for all-vertical-script runs so their downstream metrics
// converge with a sideways layout; this helper lets the caller pick the
// matching alphabetic baseline when that rewrite covered the entire line.
func lineIsSidewaysResolved(results []InlineItemResult) bool {
	sawText := false
	for _, r := range results {
		if r.Item == nil || r.Item.Style == nil {
			continue
		}
		if r.Item.Type != InlineItemText {
			continue
		}
		sawText = true
		to, _ := r.Item.Style.Get("text-orientation")
		if to != "sideways" {
			return false
		}
	}
	return sawText
}

// createLineBox positions items within a line and produces a line box fragment.
// This is the backward-compatible wrapper; uses wdm.UsesCentralBaseline().
//
// Ported from Blink's InlineLayoutAlgorithm::CreateLine and PlaceItems.
func createLineBox(
	itemsData *InlineItemsData,
	line *LineInfo,
	wdm WritingDirectionMode,
	availableInline float64,
	fonts text.FontConfig,
) (*PhysicalFragment, float64, float64) {
	frag, h, a, _, _ := createLineBoxEx(itemsData, line, wdm, availableInline, fonts, wdm.UsesCentralBaseline(), LogicalSize{}, nil, nil, nil, true, nil)
	return frag, h, a
}

// createLineBoxEx positions items within a line and produces a line box fragment.
// The centralBaseline flag determines whether to use central (vertical) or
// alphabetic (horizontal/sideways) baseline alignment.
//
// enteringSpanStack carries inline spans that were opened on prior lines and
// remain open at the start of this line — mirrors Blink's
// InlineLayoutStateStack persistence across NGInlineLayoutAlgorithm iterations.
// The returned residualSpanStack is the subset still open at end-of-line, to
// be passed as enteringSpanStack for the next line. CSS 2.1 §9.2.1.1 / §10.8.1:
// each line a span appears on produces its own fragment (first-line gets
// inline-start border/padding, last-line gets inline-end, middle lines get
// neither), and positioned inlines need a fragment per line so descendant
// abspos children can derive their inline containing block from the union.
func createLineBoxEx(
	itemsData *InlineItemsData,
	line *LineInfo,
	wdm WritingDirectionMode,
	availableInline float64,
	fonts text.FontConfig,
	centralBaseline bool,
	relativeAvailableSize LogicalSize,
	parentStyle *css.Style,
	enteringSpanStack []*InlineItem,
	firstLineStyle *css.Style,
	isFirstLine bool,
	atomicInlineContainers map[*InlineItem]AtomicInlineContainers,
) (*PhysicalFragment, float64, float64, []*InlineItem, []OutOfFlowCandidate) { // returns (fragment, lineHeight, maxAscent, residualSpanStack, oofCandidates)
	// Step 1: Compute line height from font metrics of all items.
	maxAscent, maxDescent := computeLineMetricsEx(line, wdm, fonts, centralBaseline, parentStyle, itemsData.TextContent)
	var lineOOFCandidates []OutOfFlowCandidate

	// CSS Ruby Phase 2: grow the line's ascent to contain ruby
	// annotations (default `ruby-position: over` stacks them above
	// the base baseline). Mirrors Blink
	// `inline_layout_algorithm.cc:396-418` SetAnnotationBlockStartAdjustment
	// @ 4883d11fef.
	//
	// Recompute the surviving columns from line.Results rather than
	// reading line.RubyColumns directly: float deferral can truncate
	// line.Results (see `lineResultsTruncateAt` block earlier in
	// layoutInlineChildren) without trimming line.RubyColumns, so
	// any column that's been moved to the next line would otherwise
	// still inflate maxAscent/maxDescent on this one.
	var rbpc RubyBlockPositionCalculator
	if len(line.RubyColumns) > 0 {
		var activeRubyColumns []*InlineItemResultRubyColumn
		for i := range line.Results {
			if line.Results[i].RubyColumn != nil {
				activeRubyColumns = append(activeRubyColumns, line.Results[i].RubyColumn)
			}
		}
		if len(activeRubyColumns) > 0 {
			rbpc.PlaceLines(activeRubyColumns, wdm, fonts, centralBaseline)
			// AnnotationMetrics returns (ascent, 0): the descent half is
			// a Phase 11 stub (`ruby-position: under` not yet supported)
			// and intentionally ignored here.
			annoAsc, _ := rbpc.AnnotationMetrics()

			// Mirror Blink's `ComputeAnnotationOverflow` at
			// `core/layout/inline/ruby_utils.cc:705-826 @
			// 574216cbb0c2b86a39c1d41ad85b2891a050b44c`: the annotation
			// occupies existing half-leading first; the line grows only
			// by the amount the annotation overflows the line-box ascent.
			// Under tall line-heights, the annotation fits inside the
			// natural leading and the line does not grow at all.
			//
			// Concretely: needed = max base-font-ascent across columns +
			// annotation height; if needed > current maxAscent, grow by
			// the deficit. The pre-LOU-161 pattern was unconditional
			// `maxAscent += annoAsc`, which over-inflated under tall
			// line-heights — the Bug A half of the ruby-annotation-Y
			// diagnosis. Per-line max (not per-column) is intentional
			// here: maxAscent is a line-wide quantity. The per-column
			// equivalent at the paint site below is distinct and must
			// stay distinct — see the note there.
			var baseFontAsc float64
			for _, col := range activeRubyColumns {
				if a := baseFontAscentFromSubLine(col.BaseLine, wdm, fonts, centralBaseline); a > baseFontAsc {
					baseFontAsc = a
				}
			}
			if needed := baseFontAsc + annoAsc; needed > maxAscent {
				maxAscent = needed
			}
		}
	}

	// CSS Text Decor 3 §3.4: text-emphasis marks occupy block-axis space
	// like ruby annotations — the line grows only by the amount the mark
	// overflows the existing half-leading, the same rule as the ruby block
	// above. Mirrors Blink `InlineBoxState::ComputeEmphasisMarkOutsets`
	// (`core/layout/inline/inline_box_state.cc:216-229`) feeding
	// `ComputeAnnotationOverflow` (`core/layout/inline/ruby_utils.cc:748-810`),
	// both @ 028f13cd82591cc213686dbcd075eebe562e5615. Ascent side only,
	// regardless of emphasis position — see emphasisMarkAscentFromLine.
	if needed := emphasisMarkAscentFromLine(line, wdm, fonts, centralBaseline); needed > maxAscent {
		maxAscent = needed
	}

	lineHeight := maxAscent + maxDescent
	if lineHeight <= 0 && parentStyle == nil {
		// No style context at all — use UA-default font metrics so the line box
		// has a visible height. When parentStyle is non-nil the strut height was
		// computed from the element's own font-size; a zero result is correct
		// (e.g. font-size:0 produces zero-height line boxes per spec). Mirrors
		// Blink's CSSToLengthConversionData / FontSizes contract: em-based
		// lengths resolve against the element's own computed font-size, not the
		// inherited value. Blink SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		maxAscent = 16 * 0.8
		maxDescent = 16 * 0.2
		lineHeight = maxAscent + maxDescent
	}

	// Step 2: Compute text-align offset.
	alignOffset := computeTextAlignOffset(line, availableInline, wdm)

	// Step 2a: text-align: justify — compute per-gap expansion to distribute
	// the slack between content end and the line's inline-end edge across
	// inter-word boundaries (space characters) on this line. CSS Text 3 §6.2:
	// the last line is not justified (computeTextAlignOffset already returns
	// the start offset for line.IsLastLine / line.HasForcedBreak above, so
	// we mirror the same gate here).
	//
	// Mirrors Blink's "auto" justification model in
	// third_party/blink/renderer/core/layout/inline/justification_utils.cc
	// (ApplyJustifyToLine @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f),
	// which distributes slack across word-boundary expansion opportunities.
	// We count ASCII spaces inside InlineItemText results on this line as
	// the opportunity set; intra-word (grapheme-cluster) expansion is not
	// needed for the WPT auto-justify-001 test which is purely inter-word.
	justifyExpansion := 0.0
	// Expand inter-word gaps for text-align: justify (not on the last line,
	// which falls back to start) and for text-align-last: justify (which DOES
	// expand the last line — CSS Text 3 §9.7 distinguishes the two cases).
	justifyActive := line.TextAlign == "justify" &&
		(!line.IsLastLine || line.TextAlignLastJustify) &&
		!line.HasForcedBreak
	if justifyActive {
		slack := availableInline - line.Width
		if slack > 0 {
			gaps := countJustifyOpportunities(line, itemsData)
			if gaps > 0 {
				justifyExpansion = slack / float64(gaps)
			}
		}
	}

	// Step 3: Build line box fragment with positioned children.
	// Use LTR direction for the line box's internal coordinate system.
	// Items are already in visual order (after bidi reordering in
	// ReorderLineVisual), so they should be placed in increasing inline
	// offset order. The RTL line box builder would flip all child positions,
	// reversing the visual order. Instead, text-align handles RTL alignment
	// by offsetting the starting inline position (computeTextAlignOffset
	// returns 'slack' for RTL start alignment, pushing items toward the
	// inline end).
	//
	// This applies to all writing modes — both horizontal and vertical.
	// In vertical modes with direction:rtl, the inline axis is physical Y.
	// text-align:start pushes items to the bottom (inline-start for RTL),
	// which maps correctly via the parent block's ToPhysicalOffset.
	lineWDM := wdm
	lineWDM.Dir = DirectionLTR
	lineBuilder := NewLineBoxFragmentBuilder(lineWDM)
	lineBuilder.SetSize(LogicalSize{
		InlineSize: availableInline,
		BlockSize:  lineHeight,
	})

	// LOU-149 Phase 4: pre-compute per-text-fragment decorating-box metadata
	// for any decoration on this line. Returns nil when no fragment carries
	// AppliedTextDecorations (the common case). Consumed by the text-fragment
	// construction below; paint_layer reads Box.AppliedTextDecorations in
	// preference to Style.GetAppliedTextDecorations() when non-nil.
	decoratingBoxMetadata := computeDecoratingBoxMetadataPerLine(line, alignOffset, enteringSpanStack, isFirstLine)

	// CSS Pseudo 4 §3.2: the ::first-line background paints behind the first
	// formatted line. Emit BEFORE inline span backgrounds and text fragments so
	// it lands at the bottom of the line's paint stack. Per the spec, the
	// generated ::first-line box "behaves similar to that of an inline-level
	// element" (the assertion the first-line-line-height WPT reftests check by
	// modelling ::first-line as a `<span>`): so its background covers the
	// line's CONTENT inline extent (line.Width starting at the text-align
	// offset) at the first-line font's em box (CSS 2.1 §10.6.1: inline content
	// area is font-derived, not line-height), anchored at the baseline like the
	// inline span-background pre-pass below — the genuine WPT references model
	// ::first-line as an inline `<span>`, so this band must match that span's
	// geometry exactly. Mirrors Blink's
	// `LineBoxFragmentPainter::PaintBackgroundBorderShadow`
	// (`core/paint/inline_box_fragment_painter.cc` @ 4883d11fef), whose
	// `line_style_` is the ::first-line-aware style.
	// LOU-305: opacity on ::first-line is GROUP opacity over the whole first
	// line (CSS Color 3 §3.2), exactly like element opacity. ::first-line has no
	// DOM node, so mirror Blink's anonymous inline wrapper for ::first-line
	// (LayoutBlockFlow::NeedsAnonymousInlineWrapper, computed_style.cc:369 @
	// 4883d11f) with a synthetic node-less paint group: the band and every
	// first-line text/atomic fragment route into it so the alpha is composited
	// ONCE around them all, instead of each fragment double-blending its own
	// copy.
	var flGroup *InlineItem
	if firstLineStyle != nil && firstLineStyle.GetOpacity() < 1 {
		flGroup = &InlineItem{Style: firstLineStyle}
	}
	// flPaintGroup prefers a real enclosing opacity group; first-line fragments
	// with none fall back to the synthetic group (nil when no ::first-line
	// opacity applies). The band always routes straight to flGroup.
	//
	// KNOWN LIMITATION (LOU-310): when enc != nil it is returned unchanged, so a
	// first-line fragment already inside a real inline opacity group (e.g. a
	// nested opacity span on the first line) routes to that group and ESCAPES
	// flGroup — the ::first-line alpha is not applied to that subtree. A correct
	// fix must nest enc inside flGroup per first line without polluting the
	// shared (cross-line) enclosing group; deferred as a heavy lift. Not a
	// regression: pre-LOU-305 that case double-blended too.
	flPaintGroup := func(enc *InlineItem) *InlineItem {
		if enc != nil {
			return enc
		}
		return flGroup
	}
	if firstLineStyle != nil && hasVisibleInlinePaint(firstLineStyle) {
		// Same band geometry as an inline span background (see the Step 3a
		// pre-pass below): max(em box, line box), em box baseline-anchored.
		flFontSize, flAscent := inlineAlignmentAscent(firstLineStyle, fonts, wdm, centralBaseline)
		flBlockSize, flBlockOffset := lineHeight, 0.0
		if flFontSize >= lineHeight {
			flBlockSize, flBlockOffset = flFontSize, maxAscent-flAscent
		}
		bgFrag := &PhysicalFragment{
			Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
				InlineSize: line.Width,
				BlockSize:  flBlockSize,
			}, wdm.WM)),
			Type:             FragmentBox,
			Style:            firstLineStyle,
			WritingDirection: wdm,
			PaintGroup:       flGroup,
		}
		lineBuilder.AddChild(bgFrag, LogicalOffset{
			InlineOffset: alignOffset,
			BlockOffset:  flBlockOffset,
		})
	}

	// Step 3a: Pre-pass — generate background/border fragments for inline spans.
	// These are added FIRST so they paint behind content (CSS 2.1 Appendix E).
	// Inline backgrounds may extend outside the line box (border/padding bleed).
	//
	// The inline-start border/padding appears only on the line with the
	// inline's OpenTag and the inline-end border/padding only on the line
	// with its CloseTag (Blink BoxData has_line_left_edge /
	// has_line_right_edge, inline/inline_box_state.cc:560-585, :1057-1058 @
	// a9f50e522efa).
	//
	// A span that wraps multiple lines produces one fragment per line: the
	// first line carries IS border/padding, the last line carries IE border/
	// padding, and middle lines carry neither. Span state entering this line
	// comes from enteringSpanStack (spans opened on prior lines, still open).
	// Any span still in the stack at end-of-line becomes the residual returned
	// to the caller — its fragment on this line is emitted with
	// isLastFragment=false (no IE edges). Mirrors Blink's InlineLayoutStateStack
	// persistence across NGInlineLayoutAlgorithm iterations.
	var residualSpanStack []*InlineItem
	{
		type spanEntry struct {
			item             *InlineItem
			style            *css.Style
			node             *html.Node
			borderStart      float64
			isFirstFragment  bool
			isLastFragment   bool
			cumulativeVAOffs float64 // sum of vertical-align: <length> from this span + all open ancestor spans
			openSeq          int     // monotonically increasing push order: ancestor-before-descendant, earlier-sibling-first
			// cumulativeRelativeOffset is this span's relative offset plus
			// all open ancestor spans' (ApplyRelativePositioning).
			cumulativeRelativeOffset LogicalOffset
		}
		var spanStack []spanEntry
		// openSeqCounter assigns a unique, monotonically increasing index to each
		// span as it is pushed. Spans entering from prior lines (outermost) are
		// pushed first, then OpenTags in source order — so a lower openSeq is
		// always an ancestor or earlier sibling of a higher one.
		openSeqCounter := 0
		// pushSpan opens span inside the innermost open span, accumulating
		// the vertical-align length and relative offsets.
		pushSpan := func(span spanEntry) {
			var parent spanEntry
			if n := len(spanStack); n > 0 {
				parent = spanStack[n-1]
			}
			span.cumulativeVAOffs = parent.cumulativeVAOffs + span.style.GetVerticalAlignOffset()
			span.cumulativeRelativeOffset = parent.cumulativeRelativeOffset.Add(
				ComputeRelativeOffsetForInline(wdm, relativeAvailableSize, span.style))
			span.openSeq = openSeqCounter
			openSeqCounter++
			spanStack = append(spanStack, span)
		}
		// pendingBg buffers the inline span-background fragments produced by emit
		// so they can be added to the line box in ancestor-first order. CloseTag
		// fires innermost-first, which would otherwise place descendant
		// backgrounds before their ancestors; the line box paints in insertion
		// order, so an opaque ancestor background would overpaint a descendant's.
		// Blink's InlineBoxFragmentPainter::Paint paints an inline box's own
		// background FIRST, then recurses into descendants, so ancestor inline
		// backgrounds paint behind descendant ones
		// (Chromium @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
		type pendingBgFrag struct {
			frag           *PhysicalFragment
			offset         LogicalOffset
			relativeOffset LogicalOffset
			seq            int
		}
		var pendingBg []pendingBgFrag
		emit := func(span spanEntry, endPos float64) {
			// Emit a fragment when there is something to paint OR when the span
			// establishes a containing block for positioned descendants.
			// Positioned inlines (relative/sticky), filtered inlines, and
			// inlines with will-change of a CB-establishing property all need
			// a fragment per line so descendant abspos children can derive
			// their inline containing block from the union of these rects
			// (CSS 2.1 §10.1.4 / CSS Position 3 §def-cb). The fragment is
			// also what the span's relative offset shifts.
			if !(hasVisibleInlinePaint(span.style) || inlineEstablishesContainingBlock(span.style)) {
				return
			}
			geom := ComputeFragmentGeometry(span.style, wdm)
			// Save original inline-end edges before suppression.
			origIEBorder := geom.Border.InlineEnd
			origIEPadding := geom.Padding.InlineEnd
			if !span.isFirstFragment {
				geom.Border.InlineStart = 0
				geom.Padding.InlineStart = 0
			}
			if !span.isLastFragment {
				geom.Border.InlineEnd = 0
				geom.Padding.InlineEnd = 0
			}
			// Fragment inline extent:
			// - First-fragment: border-box starts at margin end (border IS included)
			// - Non-first: no IS border/padding, border-box start = content start
			// - Last-fragment: border-box right = content_end + IE border + padding
			// - Non-last: border-box right = content_end
			fragStart := span.borderStart
			fragEnd := endPos
			if span.isLastFragment {
				fragEnd += origIEBorder + origIEPadding
			}
			spanInlineSize := fragEnd - fragStart
			if spanInlineSize < 0 {
				spanInlineSize = 0
			}
			blockOverhang := geom.Border.BlockStart + geom.Padding.BlockStart
			// An inline background covers at least the font's em box (CSS 2.1
			// §10.6.1: the content area is font-derived, not line-height) and
			// grows to the line box when line-height exceeds it — max(em box,
			// line box). When line-height is SMALLER than the font (e.g.
			// line-height:0) the em box still paints: the t32-opacity-offscreen
			// WPT tests need a full-em band even though the line box collapsed.
			// When line-height is normal/larger, the band fills the line box,
			// matching the block's own line height and how a ::first-line or
			// sibling-span background covered it. The em box is baseline-
			// anchored at maxAscent − spanAscent (Blink
			// InlineBoxState::ComputeTextMetrics, inline_box_state.cc:105-132 @
			// 4883d11fef); the line box is anchored at line top.
			spanFontSize, spanAscent := inlineAlignmentAscent(span.style, fonts, wdm, centralBaseline)
			spanEmBox := spanFontSize >= lineHeight
			spanContentBlock := lineHeight
			if spanEmBox {
				spanContentBlock = spanFontSize
			}
			spanBlockSize := blockOverhang + spanContentBlock + geom.Padding.BlockEnd + geom.Border.BlockEnd
			// Inline span background fragments must paint in flow order (behind
			// text), not via the z-index paint step which would paint ON TOP of
			// text. Reset the copy's position to static for paint purposes.
			bgStyle := span.style
			if span.style.GetPosition() != css.PositionStatic {
				bgStyle = span.style.Clone()
				bgStyle.Set("position", "static")
			}
			bgFrag := &PhysicalFragment{
				Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
					InlineSize: spanInlineSize,
					BlockSize:  spanBlockSize,
				}, wdm.WM)),
				Type:             FragmentBox,
				Style:            bgStyle,
				Node:             span.node,
				WritingDirection: wdm,
				BoxData: &PhysicalBoxData{
					Border:    ToPhysicalEdges(geom.Border, wdm),
					Padding:   ToPhysicalEdges(geom.Padding, wdm),
					Scrollbar: ToPhysicalEdges(geom.Scrollbar, wdm),
				},
			}
			// The group span's own background fragments belong to the group
			// they DEFINE; non-group spans composite inside their nearest
			// enclosing group (nil outside any group). Keyed on the
			// InlineItem so paint groups nest via EnclosingPaintGroup.
			if span.item != nil {
				if IsInlinePaintGroup(span.item.Style, span.node) {
					bgFrag.PaintGroup = span.item
				} else {
					bgFrag.PaintGroup = span.item.EnclosingPaintGroup
				}
			}
			// Anchor the band's content top at maxAscent − spanAscent — the
			// same place the text pass puts this span's text fragments
			// (blockPos = maxAscent − ascent), so band and glyph em boxes
			// coincide.
			//
			// CSS 2.1 §10.8.1: vertical-align: <length> on an inline element
			// shifts the inline box (including its background) up by the
			// offset, matching the shift applied to its text children. Use
			// the cumulative offset so nested spans stack correctly.
			//
			// Anchor: an em box sits on the baseline (maxAscent − spanAscent);
			// a line box fills the line from its top.
			spanBlockOffset := -blockOverhang - span.cumulativeVAOffs
			if spanEmBox {
				spanBlockOffset += maxAscent - spanAscent
			}
			pendingBg = append(pendingBg, pendingBgFrag{
				frag: bgFrag,
				offset: LogicalOffset{
					InlineOffset: fragStart,
					BlockOffset:  spanBlockOffset,
				},
				// CSS 2.1 §9.4.3: an inline box moves by its own and its
				// ancestor boxes' relative offsets (ApplyRelativePositioning,
				// inline/inline_box_state.cc:975-977 @ a9f50e522efa).
				// Sticky is scroll-time, not layout-time — leave zero.
				relativeOffset: span.cumulativeRelativeOffset,
				seq:            span.openSeq,
			})
		}

		// Pre-populate with spans opened on prior lines. These enter the line
		// at the content-area start (alignOffset) with no inline-start
		// border/padding (they are non-first-line fragments).
		for _, item := range enteringSpanStack {
			if item == nil || item.Style == nil {
				continue
			}
			pushSpan(spanEntry{
				item:            item,
				style:           item.Style,
				node:            item.Node,
				borderStart:     alignOffset,
				isFirstFragment: false,
				isLastFragment:  false,
			})
		}

		trackPos := alignOffset
		for i := range line.Results {
			r := &line.Results[i]
			// CSS Pseudo 4 §3.2: ::first-line style overrides apply to
			// inline spans (and their backgrounds) on the first line; use
			// the per-result effective style so background-color/border on
			// open/close-tag fragments reflect the first-line override.
			effStyle := r.EffectiveStyle()
			switch r.Item.Type {
			case InlineItemOpenTag:
				if effStyle != nil {
					pushSpan(spanEntry{
						item:            r.Item,
						style:           effStyle,
						node:            r.Item.Node,
						borderStart:     trackPos + r.Margins.InlineStart,
						isFirstFragment: true,
						isLastFragment:  false, // set by its CloseTag
					})
				}
			case InlineItemCloseTag:
				if len(spanStack) > 0 && effStyle != nil {
					span := spanStack[len(spanStack)-1]
					spanStack = spanStack[:len(spanStack)-1]
					// The closing fragment carries the inline-end
					// border/padding, also for a span entering from a prior
					// line.
					span.isLastFragment = true
					emit(span, trackPos)
				}
			case InlineItemAtomicInline:
				// Atomic inlines advance by margin+size+margin (no default advance).
				trackPos += r.Margins.InlineStart + r.InlineSize + r.Margins.InlineEnd
				continue
			}
			trackPos += r.InlineSize
		}

		// Emit synthetic fragments for spans still open at end-of-line and
		// record them as the residual for the next line's enteringSpanStack.
		for _, span := range spanStack {
			emit(span, trackPos)
			residualSpanStack = append(residualSpanStack, span.item)
		}

		// Add the buffered inline-background fragments in ancestor-first order
		// (lowest openSeq first), so descendant inline backgrounds paint on top
		// of their ancestors' (matching Blink's paint-own-background-then-recurse
		// order). openSeq is unique per push; SliceStable is belt-and-suspenders.
		sort.SliceStable(pendingBg, func(a, b int) bool {
			return pendingBg[a].seq < pendingBg[b].seq
		})
		for _, p := range pendingBg {
			lineBuilder.AddChildWithRelativeOffset(p.frag, p.offset, p.relativeOffset)
		}
	}

	// Position each item within the line.
	//
	// CSS 2.1 §10.8.1 aligned-subtree semantics: each inline box with
	// vertical-align:top/bottom roots an independent "aligned subtree" that is
	// baseline-aligned internally, then shifted so the subtree's top (bottom)
	// aligns with the line-top (line-bottom). For text children inside such a
	// subtree, the effective block-offset is not the line-level baseline-align
	// offset — it is the subtree-root's ascent minus the text's ascent (for
	// top-aligned; symmetric for bottom). This mirrors Blink's
	// InlineBoxState::ApplyBaselineShift (inline_box_state.cc).
	type vaFrame struct {
		vAlign   css.VerticalAlign
		rootAsc  float64 // subtree root inline-box ascent (font-asc + half-leading)
		rootDesc float64
	}
	var vaStack []vaFrame
	// CSS 2.1 §10.8.1: vertical-align: <length> on an inline element raises
	// its inline box (background + descendant text) by the offset within the
	// parent's line. Only applies when the element is encountered as an
	// OpenTag in the inline flow — when the element IS the inline formatting
	// context root (e.g. a flex item span carrying its own text), its
	// vertical-align does not affect its own content (CSS Flexbox §4 — flex
	// items do not honor vertical-align). Tracked as a sum so nested spans
	// with length offsets accumulate.
	var vaLengthOffset float64
	var vaLengthStack []float64
	// relativeOffset is the sum of the relative offsets of the inline boxes
	// open at the current item: every line item inside an inline box moves
	// by that box's relative offset, so items in nested boxes move by the
	// sum. Mirrors Blink's InlineLayoutStateStack::ApplyRelativePositioning
	// (inline/inline_box_state.cc:961-973 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). relativeOffsetStack holds
	// the offset outside each open box, restored at its CloseTag.
	var relativeOffset LogicalOffset
	var relativeOffsetStack []LogicalOffset
	pushInlineBox := func(style *css.Style) {
		off := style.GetVerticalAlignOffset()
		vaLengthStack = append(vaLengthStack, off)
		vaLengthOffset += off
		relativeOffsetStack = append(relativeOffsetStack, relativeOffset)
		relativeOffset = relativeOffset.Add(ComputeRelativeOffsetForInline(wdm, relativeAvailableSize, style))
	}
	// Pre-populate with spans opened on prior lines so their length and
	// relative offsets continue to affect this line's descendant fragments.
	for _, item := range enteringSpanStack {
		if item == nil || item.Style == nil {
			vaLengthStack = append(vaLengthStack, 0)
			relativeOffsetStack = append(relativeOffsetStack, relativeOffset)
			continue
		}
		pushInlineBox(item.Style)
	}
	// inlineBoxAsDesc computes an inline box's ascent/descent contribution the
	// same way computeLineMetricsEx does for an OpenTag: font ascent/descent
	// plus half-leading from line-height. Used to derive subtree-root metrics.
	sidewaysVLR := needsSidewaysVLRBaselineSwap(wdm, centralBaseline)
	inlineBoxAsDesc := func(style *css.Style) (asc, desc float64) {
		if style == nil {
			return 0, 0
		}
		fs, _, _, _, _ := fontPropsFromStyle(style)
		if centralBaseline {
			asc, desc = fs/2, fs/2
		} else {
			fontPath := resolveFontPath(style, fonts)
			asc = alignmentAscentFromFont(sidewaysVLR, fs, fontPath, fonts.Registry)
			desc = alignmentDescentFromFont(sidewaysVLR, fs, fontPath, fonts.Registry)
		}
		lineHt := style.GetLineHeight()
		if style.IsLineHeightNormal() && !centralBaseline {
			fontPath := resolveFontPath(style, fonts)
			lineHt = text.FontHeightFromFont(fs, fontPath, fonts.Registry)
		}
		halfLeading := (lineHt - (asc + desc)) / 2
		asc += halfLeading
		desc += halfLeading
		return
	}
	inlinePos := alignOffset
	for i := range line.Results {
		r := &line.Results[i]
		// CSS Pseudo 4 §3.2: ::first-line style overrides apply to font/
		// color/vertical-align on this line; consult EffectiveStyle() so a
		// shared InlineItem also appearing on later lines still uses its
		// original style there.
		rStyle := r.EffectiveStyle()
		switch r.Item.Type {
		case InlineItemOpenTag:
			if rStyle != nil {
				va := rStyle.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					a, d := inlineBoxAsDesc(rStyle)
					vaStack = append(vaStack, vaFrame{vAlign: va, rootAsc: a, rootDesc: d})
				}
				// Push the span's length and relative offsets onto the
				// cumulative stacks so descendant text/atomic-inline
				// fragments inherit the shift.
				pushInlineBox(rStyle)
			}
		case InlineItemCloseTag:
			if rStyle != nil {
				va := rStyle.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					if n := len(vaStack); n > 0 {
						vaStack = vaStack[:n-1]
					}
				}
				if n := len(vaLengthStack); n > 0 {
					vaLengthOffset -= vaLengthStack[n-1]
					vaLengthStack = vaLengthStack[:n-1]
				}
				if n := len(relativeOffsetStack); n > 0 {
					relativeOffset = relativeOffsetStack[n-1]
					relativeOffsetStack = relativeOffsetStack[:n-1]
				}
			}
		case InlineItemText:
			content := itemsData.TextContent[r.TextStart:r.TextEnd]
			if len(content) == 0 {
				inlinePos += r.InlineSize
				continue
			}

			// CSS Text 3 §5.2: soft hyphens (U+00AD) are invisible when not
			// used as a break point. Strip them from the visible text.
			// If HasHyphen is set, a visible "-" is appended.
			content = strings.ReplaceAll(content, "\u00AD", "")
			if r.HasHyphen {
				content += "-"
			}

			// CSS Fonts 4 §6.1-§6.3: ascent-override affects the strut (line-box
			// height) but NOT the per-item placement within the line. The renderer
			// draws the glyph at box.Y + nativeAscent; if we used the overridden
			// ascent here, blockPos = maxAscent - overriddenAscent would shift the
			// fragment up, making the glyph land at a different visual position
			// than the reference. inlineAlignmentAscent passes a nil registry so
			// the native font ascent is used for positioning, matching Blink's
			// text_fragment_painter.cc paint path which reads metrics.Ascent from
			// the underlying font data directly.
			fontSize, ascent := inlineAlignmentAscent(rStyle, fonts, wdm, centralBaseline)

			// Default: baseline-align the text fragment so its baseline sits
			// at the line's maxAscent.
			blockPos := maxAscent - ascent

			// CSS 2.1 §10.8.1: if this text is inside a vertical-align:top or
			// bottom aligned subtree, shift the fragment so the subtree's top
			// (bottom) edge lines up with the line-top (line-bottom), while
			// preserving baseline-alignment internally to the subtree root.
			// Innermost enclosing subtree wins. For text whose direct parent
			// carries vertical-align:top/bottom but has no enclosing OpenTag
			// (degenerate collection), fall back to the parent's own metrics.
			effectiveVA := css.VerticalAlignBaseline
			var rootAsc, rootDesc float64
			if n := len(vaStack); n > 0 {
				f := vaStack[n-1]
				effectiveVA = f.vAlign
				rootAsc, rootDesc = f.rootAsc, f.rootDesc
			} else if rStyle != nil {
				va := rStyle.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					effectiveVA = va
					rootAsc, rootDesc = inlineBoxAsDesc(rStyle)
				}
			}
			switch effectiveVA {
			case css.VerticalAlignTop:
				blockPos = rootAsc - ascent
			case css.VerticalAlignBottom:
				blockPos = lineHeight - rootDesc - ascent
			}

			// CSS 2.1 §10.8.1: vertical-align: <length> on an enclosing inline
			// span raises descendant text by the (cumulative) length offset.
			// Sourced from vaLengthOffset (only spans opened in the inline flow
			// contribute), so flex items / block containers whose own
			// vertical-align is on the formatting-context root do not shift
			// their own content. Mirrors Blink's InlineBoxState::ApplyBaselineShift.
			if vaLengthOffset != 0 {
				blockPos -= vaLengthOffset
			}

			// Use parent element as Node so the renderer can access styles.
			parentNode := r.Item.Node
			if parentNode != nil && parentNode.Parent != nil {
				parentNode = parentNode.Parent
			}

			// CSS Text 3 §6.2: text-align: justify distributes slack across
			// space characters on the line. When justifyExpansion > 0 we
			// split this text result at space boundaries into per-piece
			// fragments so each space's effective advance is widened by the
			// per-gap expansion. Mirrors Blink's per-glyph offset model in
			// justification_utils.cc (ApplyJustifyToLine, @ SHA
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f) — louis14 splits
			// fragments since the rasterizer draws each text fragment at a
			// fixed inline offset, while Blink mutates per-glyph offsets in
			// ShapeResult.
			fontPath := resolveFontPath(rStyle, fonts)
			// CSS 2.1 §9.4.3: text moves by the relative offsets of the
			// inline boxes enclosing it (relativeOffset). The block container
			// is not one of them: its own parent shifts it.
			textRelativeOffset := relativeOffset
			emitTextFragment := func(piece string, pieceInline float64, pieceOffset float64) {
				if len(piece) == 0 {
					return
				}
				frag := &PhysicalFragment{
					Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
						InlineSize: pieceInline,
						BlockSize:  fontSize,
					}, wdm.WM)),
					Type:             FragmentText,
					TextContent:      piece,
					BidiLevel:        r.Item.BidiLevel,
					Node:             parentNode,
					Style:            rStyle,
					WritingDirection: wdm,
					PaintGroup:       flPaintGroup(r.Item.EnclosingPaintGroup),
				}
				// LOU-149 Phase 4: stamp per-fragment decorating-box metadata so
				// the painter draws a continuous line across bidi-split, line-
				// wrapped, and nested-inline fragments instead of restarting at
				// each fragment boundary.
				if decoratingBoxMetadata != nil {
					if stamped, ok := decoratingBoxMetadata[r.Item]; ok {
						frag.AppliedTextDecorations = stamped
					}
				}

				lineBuilder.AddChildWithRelativeOffset(frag, LogicalOffset{
					InlineOffset: pieceOffset,
					BlockOffset:  blockPos,
				}, textRelativeOffset)
			}

			if justifyExpansion > 0 && strings.Contains(content, " ") {
				// Measure each "word" piece (segment between spaces) and
				// emit one fragment per piece, shifting each subsequent
				// piece's inline offset by spaceWidth + justifyExpansion.
				// Empty pieces (consecutive spaces) collapse naturally
				// because measureTextContent of "" is 0 — we still advance
				// by the space's expanded width to keep glyph positions in
				// sync with the line's measured Width.
				letterSpacing := 0.0
				wordSpacing := 0.0
				if rStyle != nil {
					letterSpacing = rStyle.GetLetterSpacing()
					wordSpacing = rStyle.GetWordSpacing()
				}
				// Measure once for the space character; Ahem at 16px is 16px.
				spaceWidth := measureTextContent(" ", fontSize, fontPath, letterSpacing, 0, false)
				piecePos := inlinePos
				pieces := strings.Split(content, " ")
				for i, piece := range pieces {
					pieceWidth := measureTextContent(piece, fontSize, fontPath, letterSpacing, 0, false)
					if i > 0 {
						// Emit a "space" fragment for inter-word painting
						// continuity (carries any decoration). Empty content
						// is fine — emitTextFragment short-circuits.
						emitTextFragment(" ", spaceWidth+justifyExpansion+wordSpacing, piecePos)
						piecePos += spaceWidth + justifyExpansion + wordSpacing
					}
					emitTextFragment(piece, pieceWidth, piecePos)
					piecePos += pieceWidth
				}
				// Skip the default `inlinePos += r.InlineSize` advance below;
				// the loop above has already advanced piecePos past the
				// expanded content. Set inlinePos = piecePos and continue
				// so we don't double-advance.
				inlinePos = piecePos
				continue
			}
			emitTextFragment(content, r.InlineSize, inlinePos)

		case InlineItemOpenRubyColumn:
			// CSS Ruby Phase 2: paint the base + annotation sub-line
			// glyphs at the column's current inline position. The
			// outer line's `maxAscent` already includes the
			// annotation contribution (RubyBlockPositionCalculator
			// adjusted it above in Step 1). See inline_layout_ruby.go.
			if r.RubyColumn != nil {
				// Mirror Blink at `core/layout/inline/ruby_utils.cc:981 @
				// 574216cbb0c2b86a39c1d41ad85b2891a050b44c` —
				// annotation Y is measured from the base **font** ascent
				// (no line-height leading), not the base sub-line ascent.
				// Using sub-line ascent (the Bug B half of the LOU-161
				// diagnosis) pins annotations at line-box top under tall
				// line-heights because the half-leading double-counts
				// with maxAscent's growth.
				//
				// Per-column (not per-line) base-font ascent: distinct
				// from the max-across-columns computation in Step 1
				// above. Step 1 wants the line-wide max because
				// `maxAscent` is line-wide; this step wants the
				// individual column's font ascent because each column
				// places its own annotation. Don't dedupe.
				baseFontAsc := baseFontAscentFromSubLine(r.RubyColumn.BaseLine, wdm, fonts, centralBaseline)
				annotationBlockTop := maxAscent - baseFontAsc - rbpc.annotationAscent
				if annotationBlockTop < 0 {
					annotationBlockTop = 0
				}
				emitRubyColumnFragments(
					r.RubyColumn,
					inlinePos,
					maxAscent, // base baseline
					annotationBlockTop,
					lineHeight,
					maxAscent,
					itemsData.TextContent,
					r.RubyColumn.RubyAlign,
					wdm, fonts, centralBaseline, sidewaysVLR,
					lineBuilder,
				)
			}
			inlinePos += r.InlineSize
			continue
		case InlineItemAtomicInline:
			// Apply inline-start margin before the child. For RTL items
			// (odd BidiLevel) that have been visually reversed by BIDI
			// reordering, InlineStart is the physical-right side of the item.
			// In visual (left-to-right) placement order, the physical-right is
			// the trailing side, so InlineEnd comes first (leading gap) and
			// InlineStart comes last (trailing gap).
			itemIsRTL := r.Item.BidiLevel%2 == 1
			if itemIsRTL {
				inlinePos += r.Margins.InlineEnd
			} else {
				inlinePos += r.Margins.InlineStart
			}
			if r.LayoutResult != nil {
				childLogical := NewLogicalFragment(wdm, r.LayoutResult.Fragment)
				blockSize := childLogical.BlockSize()
				var blockPos float64

				// CSS 2.1 §10.8.1: vertical-align determines block-direction
				// positioning within the line box.
				va := css.VerticalAlignBaseline
				if r.Item.Style != nil {
					va = r.Item.Style.GetVerticalAlign()
				}

				switch va {
				case css.VerticalAlignTop:
					// CSS 2.1 §10.8.1: Align the top of the margin-box with the
					// top of the line box. The border-box starts below the block-start margin.
					blockPos = r.Margins.BlockStart
				case css.VerticalAlignBottom:
					// CSS 2.1 §10.8.1: Align the bottom of the margin-box with the
					// bottom of the line box. Subtract margin-box height from line height.
					blockPos = lineHeight - blockSize - r.Margins.BlockEnd
					if blockPos < 0 {
						blockPos = 0
					}
				case css.VerticalAlignMiddle:
					// CSS 2.1 §10.8.1: Center the margin-box vertically within the
					// line box. Mirrors the lineHeight-relative pattern used by
					// VerticalAlignBottom above (same WPT line-construction).
					marginBoxSize := blockSize + r.Margins.BlockStart + r.Margins.BlockEnd
					blockPos = (lineHeight-marginBoxSize)/2 + r.Margins.BlockStart
				default:
					// CSS 2.1 §10.8.1: For display:inline-block with overflow:visible,
					// align inline-block so its baseline sits at the line's maxAscent.
					// Also handle inline-tables, replaced elements, and other atomic
					// inlines with baselines.
					display := css.DisplayBlock
					if r.Item.Style != nil {
						display = r.Item.Style.GetDisplay()
					}
					isReplaced := r.Item.Node != nil && IsReplacedElement(r.Item.Node)
					isAtomicForBaseline := isAtomicBaselineDisplay(display) || isReplaced
					// For inline-flex/inline-grid, use first baseline (CSS Flexbox §4.2, CSS Grid).
					// For inline-block/inline-table, use last baseline (CSS 2.1 §10.8.1).
					// Replaced elements don't propagate baselines from line boxes.
					// For orthogonal inline-blocks, synthesize baseline at the
					// block-end edge in the outer writing mode (per Blink, matches
					// CSS Writing Modes §4.3 "no baseline from orthogonal content").
					// For scrollable inline-block/inline-table, synthesize from margin edge.
					atomicBaseline := float64(0)
					childIsOrthogonal := false
					if r.Item.Style != nil {
						childIsOrthogonal = NewWritingDirectionMode(r.Item.Style).IsOrthogonalTo(wdm)
					}
					if !isReplaced {
						if childIsOrthogonal || useLogicalBottomMarginEdgeForInlineBlockBaseline(display, r.Item.Style) {
							atomicBaseline = blockSize + r.Margins.BlockEnd
						} else {
							atomicBaseline = r.LayoutResult.LastBaseline
							if (display == css.DisplayInlineFlex || display == css.DisplayFlex || display == css.DisplayInlineGrid || display == css.DisplayGrid) && r.LayoutResult.Baseline > 0 {
								atomicBaseline = r.LayoutResult.Baseline
							}
						}
					}
					if isAtomicForBaseline && (atomicBaseline > 0 || !centralBaseline) {
						var ibAscent float64
						if atomicBaseline > 0 {
							ibAscent = atomicBaseline
						} else if centralBaseline {
							ibAscent = blockSize / 2
						} else {
							// CSS 2.1 §10.8.1: For replaced elements and inline-blocks
							// with no line boxes, baseline = bottom margin edge.
							ibAscent = blockSize + r.Margins.BlockEnd
						}
						blockPos = maxAscent - ibAscent
					} else if centralBaseline {
						// CSS Writing Modes 3 §4.3: In vertical modes with central
						// baseline, replaced elements and atomic inlines without
						// explicit baselines are centered on the central baseline.
						// For tables/inline-tables, center on the content area
						// (excluding padding), not the padded box.
						blockPos = maxAscent - blockSize/2
					} else {
						// Default: bottom-align to baseline.
						blockPos = maxAscent - blockSize
					}
				}
				// CSS 2.1 §10.8.1: vertical-align: <length> shifts the atomic
				// inline up (positive) or down (negative) by its own offset
				// plus any cumulative offset from enclosing inline spans.
				// Mirrors Blink's InlineBoxState::ApplyBaselineShift.
				totalLengthOffset := vaLengthOffset
				if r.Item.Style != nil {
					totalLengthOffset += r.Item.Style.GetVerticalAlignOffset()
				}
				if totalLengthOffset != 0 {
					blockPos -= totalLengthOffset
				}
				if blockPos < 0 {
					blockPos = 0
				}
				// Atomic inlines composite inside the opacity group of their
				// nearest enclosing group inline, like text runs and floats.
				// On a ::first-line with group opacity, the synthetic first-line
				// group is that enclosing group (LOU-305).
				r.LayoutResult.Fragment.PaintGroup = flPaintGroup(r.Item.EnclosingPaintGroup)
				// CSS 2.1 §9.4.3: an atomic inline moves by its own relative
				// offset (PlaceRelativePositionedItems, inline/
				// inline_layout_algorithm.cc:225-235 @ a9f50e522efa) and its
				// enclosing inline boxes'. Sticky is scroll-time, not
				// layout-time.
				atomicRelativeOffset := relativeOffset.Add(
					ComputeRelativeOffsetForInline(wdm, relativeAvailableSize, r.Item.Style))
				lineBuilder.AddChildWithRelativeOffset(r.LayoutResult.Fragment, LogicalOffset{
					InlineOffset: inlinePos,
					BlockOffset:  blockPos,
				}, atomicRelativeOffset)
				// Propagate OOF candidates from atomic inline's subtree.
				// These are abs/fixed children of the inline-block that need to
				// reach the nearest positioned ancestor (or the root) for layout,
				// shifted by the atomic inline's offset, border+padding and
				// relative offset. Mirrors Blink's line box builder propagating
				// the atomic inline's result (PropagateOOFPositionedInfo,
				// fragment_builder.cc:762 @
				// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 keeps the
				// line builder's forced-LTR coordinates out of it.
				if len(r.LayoutResult.PropagatedOOFCandidates) > 0 && r.Item.Style != nil {
					childGeom := ComputeFragmentGeometry(r.Item.Style, wdm)
					inlineBP := childGeom.Border.InlineStart + childGeom.Padding.InlineStart
					blockBP := childGeom.Border.BlockStart + childGeom.Padding.BlockStart
					for _, cand := range r.LayoutResult.PropagatedOOFCandidates {
						adj := cand
						if adj.InlineContainer == nil {
							adj.InlineContainer = atomicInlineContainers[r.Item].For(adj)
						}
						adj.StaticPosition.Offset.InlineOffset += inlinePos + inlineBP + atomicRelativeOffset.InlineOffset
						adj.StaticPosition.Offset.BlockOffset += blockPos + blockBP + atomicRelativeOffset.BlockOffset
						lineOOFCandidates = append(lineOOFCandidates, adj)
					}
				}
			}
			// Advance past content + trailing margin, skip default advance.
			// For RTL items, InlineStart is the trailing (physical-right) gap.
			if itemIsRTL {
				inlinePos += r.InlineSize + r.Margins.InlineStart
			} else {
				inlinePos += r.InlineSize + r.Margins.InlineEnd
			}
			continue

		case InlineItemFloat:
			// Floats are positioned by the parent block formatting context.
			continue

		case InlineItemControl:
			// Forced break — no content to position.
			continue
		}

		inlinePos += r.InlineSize
	}

	result := lineBuilder.Build()
	return result.Fragment, lineHeight, maxAscent, residualSpanStack, lineOOFCandidates
}

// needsSidewaysVLRBaselineSwap reports whether alphabetic ascent/descent must
// be swapped for line-metric and text-placement purposes. This applies only to
// writing-mode: vertical-lr combined with text-orientation: sideways: the
// glyphs are rotated 90° CW and the block-start side is on the LEFT, so the
// alphabetic baseline lands at the typographic descent from block-start (not
// ascent). sideways-lr uses CCW rotation and sideways-rl / vertical-rl +
// sideways place block-start on the RIGHT, so those keep the normal mapping.
// See CSS Writing Modes 3 §4.3 and §5.1.
func needsSidewaysVLRBaselineSwap(wdm WritingDirectionMode, centralBaseline bool) bool {
	return wdm.WM == WritingModeVerticalLR && !centralBaseline
}

// alignmentAscentFromFont returns the distance from the line's block-start to
// the alphabetic baseline for a font. When swap is true (VLR+sideways per
// needsSidewaysVLRBaselineSwap), this returns the typographic descent.
// reg is passed through to FontAscentFromFont / FontDescentFromFont for
// CSS Fonts 4 metric-override support.
func alignmentAscentFromFont(swap bool, fontSize float64, fontPath string, reg *text.FontRegistry) float64 {
	if swap {
		return text.FontDescentFromFont(fontSize, fontPath, reg)
	}
	return text.FontAscentFromFont(fontSize, fontPath, reg)
}

// inlineAlignmentAscent returns the font size (the inline em-box block size)
// and the baseline-alignment ascent for an inline style — the shared metric
// source for text-fragment block positioning and inline background bands
// (both anchor at line maxAscent − ascent). Passes a nil registry to
// alignmentAscentFromFont so CSS Fonts 4 ascent-override affects only the
// strut, not per-item placement (see the text-pass comment in
// createLineBoxEx; mirrors Blink's text_fragment_painter.cc reading native
// font metrics).
func inlineAlignmentAscent(style *css.Style, fonts text.FontConfig, wdm WritingDirectionMode, centralBaseline bool) (fontSize, ascent float64) {
	fontSize, _, _, _, _ = fontPropsFromStyle(style)
	if centralBaseline {
		// CSS Writing Modes 3 §4.3: in vertical modes with central
		// baseline, use fontSize / 2.
		return fontSize, fontSize / 2
	}
	swap := needsSidewaysVLRBaselineSwap(wdm, centralBaseline)
	return fontSize, alignmentAscentFromFont(swap, fontSize, resolveFontPath(style, fonts), nil)
}

// alignmentDescentFromFont returns the distance from the alphabetic baseline
// to the line's block-end for a font. When swap is true, this returns the
// typographic ascent.
// reg is passed through to FontAscentFromFont / FontDescentFromFont for
// CSS Fonts 4 metric-override support.
func alignmentDescentFromFont(swap bool, fontSize float64, fontPath string, reg *text.FontRegistry) float64 {
	if swap {
		return text.FontAscentFromFont(fontSize, fontPath, reg)
	}
	return text.FontDescentFromFont(fontSize, fontPath, reg)
}

// computeLineMetrics is the backward-compatible wrapper that uses wdm.UsesCentralBaseline().
func computeLineMetrics(line *LineInfo, wdm WritingDirectionMode, fonts text.FontConfig) (maxAscent, maxDescent float64) {
	return computeLineMetricsEx(line, wdm, fonts, wdm.UsesCentralBaseline(), nil, "")
}

// computeLineMetricsEx computes the maximum ascent and descent across all
// items in a line. The line box height = maxAscent + maxDescent, and all
// text is baseline-aligned at maxAscent from the line box top.
//
// CSS 2.1 §10.8: line box height is determined by the tallest inline box.
// Even empty inline elements (open/close tag with no text) still contribute
// their font's line metrics (CSS 2.1 §9.4.2).
//
// parentStyle is the block container's style, used to establish the root
// inline box ("strut") per CSS 2.1 §10.8.1.
//
// textContent is InlineItemsData.TextContent; it is used to identify the first
// codepoint of each InlineItemText run so that @font-face unicode-range
// coverage is respected when selecting the font for line-height computation.
// An empty string disables coverage-aware resolution (safe fallback).
func computeLineMetricsEx(line *LineInfo, wdm WritingDirectionMode, fonts text.FontConfig, centralBaseline bool, parentStyle *css.Style, textContent string) (maxAscent, maxDescent float64) {
	var maxTopBottom float64 // tallest vertical-align:top/bottom element
	sidewaysVLR := needsSidewaysVLRBaselineSwap(wdm, centralBaseline)

	// CSS 2.1 §10.8.1: "the minimum height consists of a minimum height
	// above the baseline and a minimum height below it, exactly as if each
	// line box starts with a zero-width inline box with the element's font
	// and line height properties." This is the "strut".
	if parentStyle != nil {
		fontSize, _, _, _, _ := fontPropsFromStyle(parentStyle)
		var strutAscent, strutDescent float64
		if centralBaseline {
			strutAscent = fontSize / 2
			strutDescent = fontSize / 2
		} else {
			fontPath := resolveStrutFontPath(parentStyle, fonts)
			strutAscent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
			strutDescent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
		}
		// CSS 2.1 §10.8.1: line-height: normal uses the font's recommended
		// line height rather than a fixed 1.2× multiplier. This ensures the
		// strut height matches the font's built-in metrics.
		lineHt := parentStyle.GetLineHeight()
		if parentStyle.IsLineHeightNormal() && !centralBaseline {
			fontPath := resolveStrutFontPath(parentStyle, fonts)
			lineHt = text.FontHeightFromFont(fontSize, fontPath, fonts.Registry)
		}
		// CSS 2.1 §10.8.1: half-leading is allowed to be negative when
		// line-height < ascent+descent. Mirror Blink (font_height.cc::AddLeading
		// has no clamp): let strut ascent/descent go negative so a taller atomic
		// inline (canvas, replaced) wins cleanly via the per-side max() below
		// instead of leaking residual sub-pixel descent into the line box.
		halfLeading := (lineHt - (strutAscent + strutDescent)) / 2
		strutAscent += halfLeading
		strutDescent += halfLeading
		maxAscent = strutAscent
		maxDescent = strutDescent
	}
	// CSS 2.1 §10.8.1: Elements with vertical-align: top/bottom "do not
	// affect the calculation of the baseline." Track nesting depth so that
	// children of top/bottom-aligned inline boxes are also excluded.
	var topBottomDepth int

	for i := range line.Results {
		r := &line.Results[i]
		// CSS Pseudo 4 §3.2.1: font-size and line-height are first-line
		// allowed; use the per-result effective style so first-line items
		// contribute the overridden metrics (e.g. larger font_size grows
		// the line's ascent/descent), and shared InlineItems on later lines
		// stay at their original metrics.
		rStyle := r.EffectiveStyle()
		switch r.Item.Type {
		case InlineItemCloseTag:
			if rStyle != nil {
				va := rStyle.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					topBottomDepth--
				}
			}

		case InlineItemOpenTag:
			// Empty inline boxes (e.g. <span></span>) have no InlineItemText but
			// still establish a strut: their font's ascent/descent determine the
			// minimum line box height per CSS 2.1 §10.8.
			if rStyle == nil {
				continue
			}
			va := rStyle.GetVerticalAlign()
			if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
				topBottomDepth++
			}
			fontSize, _, _, _, _ := fontPropsFromStyle(rStyle)
			var ascent, descent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: central baseline = fontSize / 2.
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				fontPath := resolveFontPath(rStyle, fonts)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
				descent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
			}
			// CSS 2.1 §10.8.1: distribute half-leading from line-height.
			// Negative half-leading (when line-height < font-size) is valid
			// and reduces the inline box's ascent/descent contribution.
			lineHt := rStyle.GetLineHeight()
			if rStyle.IsLineHeightNormal() && !centralBaseline {
				fontPath := resolveFontPath(rStyle, fonts)
				lineHt = text.FontHeightFromFont(fontSize, fontPath, fonts.Registry)
			}
			halfLeading := (lineHt - (ascent + descent)) / 2
			ascent += halfLeading
			descent += halfLeading
			// CSS 2.1 §10.8.1: Inside a vertical-align: top/bottom subtree,
			// contribute to maxTopBottom instead of baseline calculation.
			if topBottomDepth > 0 || va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
				if h := ascent + descent; h > maxTopBottom {
					maxTopBottom = h
				}
				continue
			}
			if ascent > maxAscent {
				maxAscent = ascent
			}
			if descent > maxDescent {
				maxDescent = descent
			}

		case InlineItemText:
			if r.TextEnd <= r.TextStart {
				continue
			}
			// Check if this text is inside a vertical-align: top/bottom subtree,
			// or if its own style specifies top/bottom alignment.
			isInTopBottom := topBottomDepth > 0
			if !isInTopBottom && rStyle != nil {
				va := rStyle.GetVerticalAlign()
				isInTopBottom = va == css.VerticalAlignTop || va == css.VerticalAlignBottom
			}
			fontSize, _, _, _, _ := fontPropsFromStyle(rStyle)
			var ascent, descent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: central baseline = fontSize / 2.
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				// Use coverage-aware resolution: a face whose unicode-range
				// excludes the run's first character is skipped, mirroring
				// Blink's per-character font selection.
				fontPath := resolveFontPathForRun(rStyle, fonts, textContent, r.TextStart, r.TextEnd)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
				descent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
			}
			// CSS 2.1 §10.8.1: distribute half-leading from line-height.
			// Negative half-leading (when line-height < font-size) is valid
			// and reduces the inline box's ascent/descent contribution.
			if rStyle != nil {
				lineHt := rStyle.GetLineHeight()
				if rStyle.IsLineHeightNormal() && !centralBaseline {
					fontPath := resolveFontPathForRun(rStyle, fonts, textContent, r.TextStart, r.TextEnd)
					lineHt = text.FontHeightFromFont(fontSize, fontPath, fonts.Registry)
				}
				halfLeading := (lineHt - (ascent + descent)) / 2
				ascent += halfLeading
				descent += halfLeading
			}
			if isInTopBottom {
				if h := ascent + descent; h > maxTopBottom {
					maxTopBottom = h
				}
				continue
			}
			if ascent > maxAscent {
				maxAscent = ascent
			}
			if descent > maxDescent {
				maxDescent = descent
			}

		case InlineItemControl:
			// A control item (forced line break) contributes a strut using its
			// parent element's font metrics. This ensures blank lines in
			// white-space: pre content have the correct height (CSS 2.1 §10.8).
			// Must mirror the InlineItemText path exactly so that a control-only
			// line (blank line between two \n in <pre>) has the same ascent/descent
			// that a text-bearing line would have with the same font.
			if rStyle == nil {
				continue
			}
			fontSize, _, _, _, _ := fontPropsFromStyle(rStyle)
			var ascent, descent float64
			if centralBaseline {
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				fontPath := resolveFontPath(rStyle, fonts)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
				descent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
			}
			lineHt := rStyle.GetLineHeight()
			if rStyle.IsLineHeightNormal() && !centralBaseline {
				fontPath := resolveFontPath(rStyle, fonts)
				lineHt = text.FontHeightFromFont(fontSize, fontPath, fonts.Registry)
			}
			halfLeading := (lineHt - (ascent + descent)) / 2
			ascent += halfLeading
			descent += halfLeading
			if ascent > maxAscent {
				maxAscent = ascent
			}
			if descent > maxDescent {
				maxDescent = descent
			}

		case InlineItemAtomicInline:
			if r.LayoutResult != nil {
				childLogical := NewLogicalFragment(wdm, r.LayoutResult.Fragment)
				blockSize := childLogical.BlockSize()

				// CSS 2.1 §10.8.1: vertical-align:top/bottom elements don't
				// participate in baseline alignment. They contribute to line
				// height but not to maxAscent/maxDescent directly.
				va := css.VerticalAlignBaseline
				if r.Item.Style != nil {
					va = r.Item.Style.GetVerticalAlign()
				}
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					// Track the tallest top/bottom-aligned element separately.
					// CSS 2.1 §10.8.1: The margin-box height of top/bottom-aligned
					// inline-blocks determines the minimum line-box height.
					outerBlockSize := blockSize + r.Margins.BlockStart + r.Margins.BlockEnd
					if outerBlockSize > maxTopBottom {
						maxTopBottom = outerBlockSize
					}
					continue
				}

				// CSS 2.1 §10.8.1: For display:inline-block with overflow:visible,
				// the baseline is the baseline of the last line box.
				// Also handle inline-tables and other atomic inlines with baselines.
				// Inline replaced elements (img, canvas, video, etc.) are also
				// atomic inlines whose baseline is their bottom margin edge.
				display := css.DisplayBlock
				if r.Item.Style != nil {
					display = r.Item.Style.GetDisplay()
				}
				isReplaced := r.Item.Node != nil && IsReplacedElement(r.Item.Node)
				isAtomicForBaseline := isAtomicBaselineDisplay(display) || isReplaced
				// For inline-flex/inline-grid, use first baseline (CSS Flexbox §4.2, CSS Grid).
				// For inline-block/inline-table, use last baseline (CSS 2.1 §10.8.1).
				// Replaced elements don't propagate baselines from line boxes.
				// For orthogonal inline-blocks, synthesize baseline at block-end
				// (matches Blink for orthogonal writing-mode roots).
				// For scrollable inline-block/inline-table, synthesize from margin edge.
				atomicBaseline := float64(0)
				childIsOrthogonal := false
				if r.Item.Style != nil {
					childIsOrthogonal = NewWritingDirectionMode(r.Item.Style).IsOrthogonalTo(wdm)
				}
				if !isReplaced {
					if childIsOrthogonal || useLogicalBottomMarginEdgeForInlineBlockBaseline(display, r.Item.Style) {
						atomicBaseline = blockSize + r.Margins.BlockEnd
					} else if (display == css.DisplayInlineFlex || display == css.DisplayFlex || display == css.DisplayInlineGrid || display == css.DisplayGrid) && r.LayoutResult.Baseline > 0 {
						atomicBaseline = r.LayoutResult.Baseline
					} else if (display == css.DisplayInlineTable || display == css.DisplayTable) && r.LayoutResult.Baseline > 0 {
						atomicBaseline = r.LayoutResult.Baseline
					} else {
						atomicBaseline = r.LayoutResult.LastBaseline
					}
				}
				if isAtomicForBaseline && (atomicBaseline > 0 || !centralBaseline) {
					var ibAscent float64
					if atomicBaseline > 0 {
						// Use the propagated baseline from the atomic inline's
						// layout result. This is the distance from the border-box
						// block-start to the baseline.
						ibAscent = atomicBaseline
					} else if centralBaseline {
						// CSS Writing Modes 3 §4.3: fallback for empty inline-blocks
						// in vertical modes with central baseline: blockSize / 2.
						ibAscent = blockSize / 2
					} else {
						// CSS 2.1 §10.8.1: For replaced elements and inline-blocks
						// with no line boxes, baseline = bottom margin edge.
						ibAscent = blockSize + r.Margins.BlockEnd
					}
					// CSS 2.1 §10.8.1: block-direction margins contribute to
					// the line box height. margin-block-start adds to the ascent
					// (above the baseline) and margin-block-end adds to the descent.
					totalAscent := r.Margins.BlockStart + ibAscent
					ibDescent := blockSize - ibAscent + r.Margins.BlockEnd
					if ibDescent < 0 {
						ibDescent = 0
					}
					if totalAscent > maxAscent {
						maxAscent = totalAscent
					}
					if ibDescent > maxDescent {
						maxDescent = ibDescent
					}
				} else if centralBaseline {
					// CSS Writing Modes 3 §4.3: In vertical modes with central
					// baseline, replaced elements are centered on the central baseline.
					ascent := blockSize / 2
					descent := blockSize - ascent
					if ascent > maxAscent {
						maxAscent = ascent
					}
					if descent > maxDescent {
						maxDescent = descent
					}
				} else {
					// Default: treat full height as above baseline (bottom-aligned).
					if blockSize > maxAscent {
						maxAscent = blockSize
					}
				}
			}
		}
	}

	// CSS 2.1 §10.8.1: After computing baseline-based line height, ensure
	// the line is tall enough to contain top/bottom-aligned elements.
	baselineHeight := maxAscent + maxDescent
	if maxTopBottom > baselineHeight {
		// Expand the line box symmetrically by increasing maxDescent.
		maxDescent += maxTopBottom - baselineHeight
	}

	return
}

// countJustifyOpportunities counts the inter-word boundaries on a line that
// are eligible for text-align: justify expansion. The "auto" justification
// model (CSS Text 3 §6.2; Blink's default) distributes slack between ASCII
// space characters within text content; trailing whitespace is already
// stripped by LineBreaker.finishLine, so every space we count produces visible
// expansion. Mirrors Blink's CountJustificationOpportunities helper in
// third_party/blink/renderer/core/layout/inline/justification_utils.cc
// (@ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func countJustifyOpportunities(line *LineInfo, itemsData *InlineItemsData) int {
	n := 0
	for i := range line.Results {
		r := &line.Results[i]
		if r.Item.Type != InlineItemText {
			continue
		}
		if r.TextEnd <= r.TextStart || r.TextEnd > len(itemsData.TextContent) {
			continue
		}
		content := itemsData.TextContent[r.TextStart:r.TextEnd]
		n += strings.Count(content, " ")
	}
	return n
}

// computeTextAlignOffset computes the starting inline offset for text-align.
// CSS Text §7.1: "start" and "end" are direction-relative; "left" and "right"
// are physical and independent of direction.
func computeTextAlignOffset(line *LineInfo, availableInline float64, wdm WritingDirectionMode) float64 {
	slack := availableInline - line.Width

	// The offset is the inline-start position where the (already visually
	// ordered) line content begins; it then extends toward the inline end.
	// Alignments that anchor content against the inline END — physical-right
	// in LTR: text-align:right, LTR end, RTL start, plus center — must return
	// `slack` (or slack/2) EVEN WHEN NEGATIVE, so content wider than the line
	// box hangs off the start side rather than clamping to the start edge. This
	// is what makes a `direction:rtl; width:0` inline-block (the WPT marker-
	// content outside-marker idiom) hang its overflowing content to the left,
	// matching Blink. Physical-left/start alignments keep content at offset 0 —
	// overflow simply extends toward the end (no negative offset needed).
	switch line.TextAlign {
	case "center", "-webkit-center":
		return slack / 2
	case "right":
		return slack // physical right
	case "end":
		if !wdm.IsRTL() {
			return slack // LTR end = physical right
		}
		return 0 // RTL end = physical left
	case "start":
		if wdm.IsRTL() {
			return slack // RTL start = physical right
		}
		return 0 // LTR start = physical left
	case "justify":
		// text-align: justify falls back to start on the last line.
		// text-align-last: justify expands the last line (CSS Text 3 §9.7).
		// Inter-word widening (justifyExpansion) handles non-last lines, so
		// their start offset is 0; only the RTL last-line fallback anchors right.
		if (line.IsLastLine || line.HasForcedBreak) && !line.TextAlignLastJustify && wdm.IsRTL() {
			return slack // RTL start = physical right
		}
		return 0
	default: // "left", ""
		return 0
	}
}

// lineHasOnlyOutOfFlow returns true if the given line contains no visible
// in-flow content — i.e., no atomic inlines, no forced breaks, and no text
// with non-whitespace characters. Out-of-flow candidates (abs/fixed), floats
// (which are positioned separately in Phase 1c, not by line layout), and
// collapsible whitespace are ignored for this determination.
//
// Such lines should not generate a line box: they would otherwise add a
// line-height's worth of empty space, and — for inline-block baseline
// propagation (CSS 2.1 §10.8.1) — would incorrectly shift the "last in-flow
// line box" to a trailing empty line. Mirrors Blink's suppression of trailing
// line boxes that contain only positioned content after a forced break.
func lineHasOnlyOutOfFlow(line *LineInfo, itemsData *InlineItemsData, wdm WritingDirectionMode) bool {
	// A forced break by itself does not create a line box: a <br> or
	// preserved newline does (its InlineItemControl below), the forced break
	// before a block-in-inline does not (LineBreaker::HandleBlockInInline →
	// HandleForcedLineBreak(nullptr), inline/line_breaker.cc:3226-3231 @
	// a9f50e522efa).
	if line.Width > 0 {
		return false
	}
	for _, r := range line.Results {
		switch r.Item.Type {
		case InlineItemAtomicInline, InlineItemControl:
			return false
		case InlineItemText:
			if r.TextEnd > r.TextStart && r.TextEnd <= len(itemsData.TextContent) {
				content := itemsData.TextContent[r.TextStart:r.TextEnd]
				if strings.TrimFunc(content, isCSSCollapsibleSpace) != "" {
					return false
				}
			}
		case InlineItemOpenTag, InlineItemCloseTag:
			// An inline box edge with inline-axis border, padding or margin
			// creates the line box; one without does not, whatever its
			// background (LineBreaker::HandleOpenTag / HandleCloseTag
			// should_create_line_box from InlineItem::IsEmptyItem,
			// inline/line_breaker.cc:3947-4030, inline/inline_item.cc:
			// 32-67, :128-139 @ a9f50e522efa). Empty span with padding/
			// border is the canonical case (wpt-css2/linebox/empty-inline-002).
			if r.Item.Style != nil && !isInlineBoxEdgeEmpty(r.Item.Style, wdm, line.AvailableWidth, r.Item.Type == InlineItemOpenTag) {
				return false
			}
		}
	}
	return true
}

// isInlineBoxEdgeEmpty reports whether an inline box's start (or end) edge
// has no inline-axis border, padding or margin. Mirrors Blink's
// IsInlineBoxStartEmpty / IsInlineBoxEndEmpty (inline/inline_item.cc:32-67
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 has no line-height
// quirks mode, so a margin always counts.
func isInlineBoxEdgeEmpty(style *css.Style, wdm WritingDirectionMode, percentageBase float64, isStart bool) bool {
	geom := ComputeFragmentGeometry(style, wdm)
	margins := ResolveMargins(style, wdm, percentageBase)
	if isStart {
		return geom.Border.InlineStart == 0 && geom.Padding.InlineStart == 0 && margins.InlineStart == 0
	}
	return geom.Border.InlineEnd == 0 && geom.Padding.InlineEnd == 0 && margins.InlineEnd == 0
}
