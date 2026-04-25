package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
	"louis14/pkg/text"
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
		AvailableSize:    LogicalSize{InlineSize: width, BlockSize: Indefinite},
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
		css.DisplayRuby,
		css.DisplayRubyText,
		css.DisplayRubyBase:
		return true
	}
	return false
}

func hasOnlyInlineChildren(node *LayoutInputNode) bool {
	hasContent := false
	for _, child := range node.Children() {
		if child.IsText() {
			// Use CSS-aware trimming: U+00A0 (non-breaking space) is not
			// collapsible and counts as visible content (CSS 2.1 §16.6.1).
			if strings.IndexFunc(child.TextContent(), func(r rune) bool {
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
			display != css.DisplayInlineFlex && display != css.DisplayInlineTable {
			return false // Block-level child found.
		}
		hasContent = true
	}
	return hasContent
}

// layoutInlineChildren handles inline formatting context for a block container.
// It collects inline content, breaks it into lines, and adds line box fragments
// to the builder.
//
// Ported from Blink's InlineLayoutAlgorithm (inline_layout_algorithm.h).
func (bla *BlockLayoutAlgorithm) layoutInlineChildren(
	wdm WritingDirectionMode,
	contentInlineSize float64,
	exclusionSpace *ExclusionSpace,
	builder *BoxFragmentBuilder,
	bfcBlockOrigin float64,
	bfcInlineOrigin float64,
	bfcContainerInlineSize float64,
	startItemIndex int,
	startTextOffset int,
) (blockSizeUsed float64, updatedES *ExclusionSpace, firstLineAscent float64, lastBaselineOffset float64, inlineBreakToken *BlockBreakToken) {
	// Phase 1: Collect inline items from the layout subtree.
	itemsData := CollectInlines(bla.node)

	if len(itemsData.Items) == 0 {
		return 0, exclusionSpace, 0, 0, nil
	}

	// Phase 1a: Block-level bidi control injection.
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

	// Phase 1d: Map each OOF item to its innermost positioned-inline
	// ancestor (DOM node), so the OOF candidate can carry the inline CB
	// reference (CSS 2.1 §10.1.4 / CSS Position 3 §def-cb). Built once
	// up front because the line-breaker emits OpenTag/CloseTag once per
	// inline item — not per line a span spans across — so a per-line
	// stack would miss spans that wrap across multiple lines.
	positionedInlineMap := BuildPositionedInlineMap(itemsData.Items)

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
	type pendingFloat struct {
		item         *InlineItem
		margins      LogicalEdges
		childLogical LogicalFragment
		fragment     *PhysicalFragment
	}
	pendingFloats := map[*InlineItem]*pendingFloat{}
	for _, item := range itemsData.Items {
		if item.Type != InlineItemFloat || item.LayoutNode == nil {
			continue
		}
		childStyle := item.Style
		if childStyle == nil {
			continue
		}
		childWDM := NewWritingDirectionMode(childStyle)
		childMargins := ResolveMargins(childStyle, wdm, contentInlineSize)
		childSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetOrthogonalFallbackInlineSize(
				orthogonalFallbackSize(childWDM, bla.ctx)).
			SetOrthogonalFallbackBlockSize(
				bla.space.OrthogonalFallbackBlockSize).
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
		childResult := layoutElement(bla.ctx, item.LayoutNode, childSpace)
		childLogical := NewLogicalFragment(wdm, childResult.Fragment)
		pendingFloats[item] = &pendingFloat{
			item:         item,
			margins:      childMargins,
			childLogical: childLogical,
			fragment:     childResult.Fragment,
		}
	}

	// placeFloat positions a single pending float at the given BFC block
	// origin (the top of the current line), updates the exclusion space, and
	// adds the float fragment as a child of the containing block. Returns the
	// new exclusion-space pointer.
	placeFloat := func(pf *pendingFloat, floatOriginBFC float64, es *ExclusionSpace) *ExclusionSpace {
		childStyle := pf.item.Style
		floatInlineSize := pf.margins.InlineSum() + pf.childLogical.InlineSize()
		floatBlockSize := pf.margins.BlockSum() + pf.childLogical.BlockSize()
		floatSide := childStyle.GetFloat()
		logicalSide := floatSide
		if wdm.Dir == DirectionRTL {
			if floatSide == css.FloatLeft {
				logicalSide = css.FloatRight
			} else {
				logicalSide = css.FloatLeft
			}
		}
		floatBlockOffset := es.FindFloatPosition(logicalSide, floatInlineSize, floatBlockSize, contentInlineSize, floatOriginBFC)
		var floatInlineOffset float64
		if logicalSide == css.FloatLeft {
			startOff, _ := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
			floatInlineOffset = startOff + pf.margins.InlineStart
		} else {
			_, endOff := es.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
			floatInlineOffset = contentInlineSize - endOff - pf.margins.InlineEnd - pf.childLogical.InlineSize()
		}
		localFloatBlock := floatBlockOffset - bfcBlockOrigin
		builder.AddChild(pf.fragment, LogicalOffset{
			InlineOffset: floatInlineOffset,
			BlockOffset:  localFloatBlock + pf.margins.BlockStart,
		})
		return es.Add(Exclusion{
			InlineOffset: floatInlineOffset - pf.margins.InlineStart,
			BlockOffset:  floatBlockOffset,
			InlineSize:   floatInlineSize,
			BlockSize:    floatBlockSize,
			Side:         PhysicalFloatToExclusionSide(floatSide, wdm),
		})
	}

	// Phase 2: Create line breaker.
	fonts := bla.ctx.FontConfig

	// CSS 2.1 §16.6: white-space: nowrap / pre prevent soft wrapping.
	// Use unlimited available width so the line breaker produces a single line
	// that may overflow the container (overflow:hidden will clip it).
	lineAvailableWidth := contentInlineSize
	noWrap := false
	if bla.style != nil {
		ws := bla.style.GetWhiteSpace()
		if ws == css.WhiteSpaceNowrap || ws == css.WhiteSpacePre {
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
	pctBlockSize := bla.space.PercentageResolutionSize.BlockSize
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
	} else if bla.space.AvailableSize.BlockSize >= 0 {
		lineAvailBlock = bla.space.AvailableSize.BlockSize
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
		AvailableSize:                LogicalSize{InlineSize: lineAvailableWidth, BlockSize: lineAvailBlock},
		PercentageResolutionSize:     LogicalSize{InlineSize: contentInlineSize, BlockSize: pctBlockSize},
		WritingDirection:             wdm,
		ExclusionSpace:               exclusionSpace,
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
	textIndent := 0.0
	if bla.style != nil {
		if v, ok := bla.style.GetLength("text-indent"); ok {
			textIndent = v
		} else if pct, ok := bla.style.GetPercentage("text-indent"); ok {
			textIndent = contentInlineSize * pct / 100
		}
	}

	// Phase 3: Break into lines and create line box fragments.
	blockOffset := 0.0
	var line LineInfo
	isFirstLine := true
	firstLineAscent = -1.0 // -1 means not yet set

	// openInlineStack tracks inline spans opened on prior lines that are
	// still open at the start of the current line. Passed into createLineBoxEx
	// so spans that wrap multiple lines emit a fragment per line, and updated
	// from the residual returned after each line. Mirrors Blink's persistent
	// InlineLayoutStateStack across NGInlineLayoutAlgorithm iterations.
	var openInlineStack []*InlineItem

	for {
		// Save item index and text offset at the start of this line iteration,
		// before NextLine advances the line breaker. Both are needed for inline
		// break tokens so the next column resumes at exactly this line's start
		// (item index alone is insufficient when a text item spans multiple lines).
		lineStartIdx := lb.currentItemIndex
		lineStartTextOffset := lb.currentTextOffset

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

		// Compute line box bounds in BFC coordinates.
		// floatStart/floatEnd are in local (container-relative) coords; convert to BFC-absolute.
		lineStartBFC := bfcInlineOrigin
		lineEndBFC := bfcInlineOrigin + contentInlineSize
		if floatStart > 0 && bfcInlineOrigin+floatStart > lineStartBFC {
			lineStartBFC = bfcInlineOrigin + floatStart
		}
		if floatEnd > 0 {
			rightEdge := bfcInlineOrigin + contentInlineSize - floatEnd
			if rightEdge < lineEndBFC {
				lineEndBFC = rightEdge
			}
		}
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
				lineStartBFC = bfcInlineOrigin
				lineEndBFC = bfcInlineOrigin + contentInlineSize
				if floatStart > 0 && bfcInlineOrigin+floatStart > lineStartBFC {
					lineStartBFC = bfcInlineOrigin + floatStart
				}
				if floatEnd > 0 {
					rightEdge := bfcInlineOrigin + contentInlineSize - floatEnd
					if rightEdge < lineEndBFC {
						lineEndBFC = rightEdge
					}
				}
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

		if !lb.NextLine(&line) {
			break
		}

		// CSS 2.1 §9.4.2: a line box that contains only out-of-flow
		// candidates (and possibly collapsible whitespace that contributes
		// nothing to the line) should not generate a line box. Record the
		// OOF candidates at the current block offset and continue without
		// advancing. This matches Blink's behavior where a trailing line
		// after a <br> containing only positioned content collapses away.
		if lineHasOnlyOutOfFlow(&line, itemsData) {
			for _, r := range line.Results {
				if r.Item.Type == InlineItemOutOfFlow && r.Item.LayoutNode != nil {
					builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
						Node: r.Item.LayoutNode,
						StaticPosition: LogicalStaticPosition{
							Offset: LogicalOffset{
								InlineOffset: 0,
								BlockOffset:  blockOffset,
							},
							InlineEdge: StaticEdgeStart,
							BlockEdge:  StaticEdgeStart,
						},
						IsFixedPosition: r.Item.Style != nil && r.Item.Style.GetPosition() == css.PositionFixed,
						InlineContainer: positionedInlineMap[r.Item],
					})
				} else if r.Item.Type == InlineItemFloat {
					if pf, ok := pendingFloats[r.Item]; ok {
						exclusionSpace = placeFloat(pf, bfcBlockOrigin+blockOffset, exclusionSpace)
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
						pendingFloatInline += pf.margins.InlineSum() + pf.childLogical.InlineSize()
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
					for i := 0; i <= lastPendingFloatIdx; i++ {
						r := &line.Results[i]
						if r.Item.Type != InlineItemFloat {
							continue
						}
						if pf, ok := pendingFloats[r.Item]; ok {
							exclusionSpace = placeFloat(pf, bfcBlockOrigin+blockOffset, exclusionSpace)
							delete(pendingFloats, r.Item)
						}
					}

					clearedBfc := exclusionSpace.ClearanceOffset(css.ClearBoth, layoutunit.FromFloat64Round(bfcBlockOrigin+blockOffset), wdm).Float64()
					if clearedBfc > bfcBlockOrigin+blockOffset {
						blockOffset = clearedBfc - bfcBlockOrigin
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
			case "start", "end", "left", "right", "center", "justify":
				line.TextAlign = textAlignLast
			}
		}

		// CSS Pseudo-Elements §3: Apply ::first-line styles to items on the
		// first formatted line. We override item styles after line breaking so
		// that color, text-decoration, background, etc. take effect. Font
		// properties that affect line breaking are not yet handled (would need
		// two-pass layout).
		if isFirstLine && bla.node.FirstLineStyle != nil {
			applyFirstLineStyles(&line, bla.node.FirstLineStyle)
		}

		// Apply text-indent to the first line only.
		appliedTextIndent := 0.0
		lineInlineOffset := lineInlineOffsetFromFloat
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
			if r.Item.Type == InlineItemOutOfFlow && r.Item.LayoutNode != nil {
				oofStyle := r.Item.Style
				inlineLevel := oofStyle != nil && isInlineLevelDisplay(oofStyle.GetDisplay())
				isFixed := oofStyle != nil && oofStyle.GetPosition() == css.PositionFixed
				if inlineLevel {
					builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
						Node: r.Item.LayoutNode,
						StaticPosition: LogicalStaticPosition{
							Offset: LogicalOffset{
								InlineOffset: inlinePos,
								BlockOffset:  blockOffset,
							},
							InlineEdge: StaticEdgeStart,
							BlockEdge:  StaticEdgeStart,
						},
						IsFixedPosition: isFixed,
						InlineContainer: positionedInlineMap[r.Item],
					})
				} else if !hasInflowOnLine {
					// No in-flow content precedes this block-level OOF on the line:
					// its block-flow position starts at the current block cursor.
					builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
						Node: r.Item.LayoutNode,
						StaticPosition: LogicalStaticPosition{
							Offset: LogicalOffset{
								InlineOffset: 0,
								BlockOffset:  blockOffset,
							},
							InlineEdge: StaticEdgeStart,
							BlockEdge:  StaticEdgeStart,
						},
						IsFixedPosition: isFixed,
						InlineContainer: positionedInlineMap[r.Item],
					})
				} else {
					blockLevelOOFOnLine = append(blockLevelOOFOnLine, struct {
						node            *LayoutInputNode
						isFixed         bool
						inlineContainer *html.Node
					}{node: r.Item.LayoutNode, isFixed: isFixed, inlineContainer: positionedInlineMap[r.Item]})
				}
			} else if r.Item.Type == InlineItemFloat {
				if pf, ok := pendingFloats[r.Item]; ok {
					floatInlineSize := pf.margins.InlineSum() + pf.childLogical.InlineSize()
					// Defer this float if placing it would displace committed inline
					// content that fits beside the previously-placed floats. Condition:
					//   (a) committed content fits beside this float alone (fitsAlone), and
					//   (b) available after placing this float < committed content.
					// This mirrors Blink's InlineLayoutAlgorithm float placement which
					// detects that a second float would evict already-committed text.
					// fitsAlone ensures we don't defer a float when the text doesn't
					// fit beside it even in isolation (in that case the existing
					// push-down code moves the text past the float, not the float).
					if committedBeforeFloat > 0 {
						availableAfterFloat := trackAvail - floatInlineSize
						if availableAfterFloat < 0 {
							availableAfterFloat = 0
						}
						fitsAlone := committedBeforeFloat <= contentInlineSize-floatInlineSize
						if fitsAlone && availableAfterFloat < committedBeforeFloat {
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
					exclusionSpace = placeFloat(pf, bfcBlockOrigin+blockOffset, exclusionSpace)
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
			newStartBFC := bfcInlineOrigin
			newEndBFC := bfcInlineOrigin + contentInlineSize
			if newFloatStart > 0 && bfcInlineOrigin+newFloatStart > newStartBFC {
				newStartBFC = bfcInlineOrigin + newFloatStart
			}
			if newFloatEnd > 0 {
				rightEdge := bfcInlineOrigin + contentInlineSize - newFloatEnd
				if rightEdge < newEndBFC {
					newEndBFC = rightEdge
				}
			}
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
		// Compute containing block physical size for inline relative positioning.
		// Percentages for top/bottom resolve against CB height, left/right against width.
		cbBlockSize := bla.space.AvailableSize.BlockSize
		if cbBlockSize == Indefinite {
			cbBlockSize = 0
		}
		cbPhys := ToPhysicalSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  cbBlockSize,
		}, wdm.WM)
		lineFragment, lineHeight, lineAscent, residualStack := createLineBoxEx(
			itemsData, &line, effectiveWDM, lineVisualInline, fonts, centralBaseline, cbPhys, bla.style, openInlineStack,
		)
		openInlineStack = residualStack

		// Block fragmentation: check if this line fits in the current fragmentainer.
		// Mirrors Blink's InlineLayoutAlgorithm fragmentation at the line-box level.
		// Only check when we have a real (non-indefinite) fragmentainer block-size and
		// this is NOT the initial balancing pass (which runs unconstrained to measure
		// total content height).
		if bla.space.HasBlockFragmentation &&
			bla.space.FragmentainerBlockSize != Indefinite &&
			!bla.space.IsInitialColumnBalancingPass {
			fragEnd := bla.space.FragmentainerBlockSize - bla.space.FragmentainerOffset
			if blockOffset+lineHeight > fragEnd && blockOffset > 0 {
				// This line overflows. Stop here and signal the parent to create a
				// break token so the next column resumes from this line.
				// blockOffset > 0 guard: never emit an empty column (at least one
				// line must have been placed, mirroring Blink's RequiresContent guard).
				inlineBreakToken = &BlockBreakToken{
					Node:                 bla.node,
					ConsumedBlockSize:    layoutunit.FromFloat64Round(blockOffset + bla.space.FragmentainerOffset),
					InlineItemStartIndex: lineStartIdx,
					InlineTextOffset:     lineStartTextOffset,
					SequenceNumber:       0,
				}
				if bla.space.BreakToken != nil {
					inlineBreakToken.ConsumedBlockSize = inlineBreakToken.ConsumedBlockSize.Add(bla.space.BreakToken.ConsumedBlockSize)
					inlineBreakToken.SequenceNumber = bla.space.BreakToken.SequenceNumber + 1
				}
				break
			}
		}

		if firstLineAscent < 0 {
			firstLineAscent = lineAscent
		}
		// Track the last line's baseline offset from the content area start.
		// This is the block offset of the line + the line's ascent.
		lastBaselineOffset = blockOffset + lineAscent

		builder.AddChild(lineFragment, LogicalOffset{
			InlineOffset: lineInlineOffset,
			BlockOffset:  blockOffset,
		})

		// Emit deferred block-level OOF candidates at the block-end of the
		// line box. Per Blink's InlineLayoutAlgorithm::HandleOutOfFlowPositioned,
		// a block-level abspos encountered AFTER in-flow content on the line
		// receives static position (inline-start, line_block_end) — if it had
		// been in flow it would have started a new block-flow line below the
		// current line box.
		for _, d := range blockLevelOOFOnLine {
			builder.AddOutOfFlowCandidate(OutOfFlowCandidate{
				Node: d.node,
				StaticPosition: LogicalStaticPosition{
					Offset: LogicalOffset{
						InlineOffset: 0,
						BlockOffset:  blockOffset + lineHeight,
					},
					InlineEdge: StaticEdgeStart,
					BlockEdge:  StaticEdgeStart,
				},
				IsFixedPosition: d.isFixed,
				InlineContainer: d.inlineContainer,
			})
		}

		blockOffset += lineHeight
	}

	if firstLineAscent < 0 {
		firstLineAscent = 0
	}
	return blockOffset, exclusionSpace, firstLineAscent, lastBaselineOffset, inlineBreakToken
}

// firstLineAllowedProperties lists the CSS properties that ::first-line is
// allowed to override (CSS Pseudo-Elements Level 4 §3).
var firstLineAllowedProperties = []string{
	// Font properties
	"font-family", "font-size", "font-style", "font-weight",
	"font-variant", "font-stretch", "font",
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
}

// applyFirstLineStyles merges ::first-line pseudo-element styles into the
// items on the given line. Only the allowed subset of properties is applied.
// Each item's Style is cloned before modification to avoid mutating shared styles.
func applyFirstLineStyles(line *LineInfo, firstLineStyle *css.Style) {
	if firstLineStyle == nil {
		return
	}

	// Collect the allowed property overrides from the first-line style.
	overrides := make(map[string]string)
	for _, prop := range firstLineAllowedProperties {
		if val, ok := firstLineStyle.Properties[prop]; ok && val != "" {
			overrides[prop] = val
		}
	}
	if len(overrides) == 0 {
		return
	}

	// Apply overrides to each item on the line that has a style.
	for i := range line.Results {
		r := &line.Results[i]
		if r.Item.Style == nil {
			continue
		}
		// Only apply to text items and open/close tags (inline spans).
		switch r.Item.Type {
		case InlineItemText, InlineItemOpenTag, InlineItemCloseTag:
			// Clone the style to avoid mutating shared state.
			cloned := r.Item.Style.Clone()
			for prop, val := range overrides {
				cloned.Properties[prop] = val
			}
			r.Item.Style = cloned
		}
	}
}

// hasVisibleInlinePaint returns true if an inline element's style has
// visible paint properties: non-transparent background or visible border.
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
	return bw.Top > 0 || bw.Right > 0 || bw.Bottom > 0 || bw.Left > 0
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
	frag, h, a, _ := createLineBoxEx(itemsData, line, wdm, availableInline, fonts, wdm.UsesCentralBaseline(), PhysicalSize{}, nil, nil)
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
	cbPhysicalSize PhysicalSize,
	parentStyle *css.Style,
	enteringSpanStack []*InlineItem,
) (*PhysicalFragment, float64, float64, []*InlineItem) { // returns (fragment, lineHeight, maxAscent, residualSpanStack)
	// Step 1: Compute line height from font metrics of all items.
	maxAscent, maxDescent := computeLineMetricsEx(line, wdm, fonts, centralBaseline, parentStyle)
	lineHeight := maxAscent + maxDescent
	if lineHeight <= 0 {
		// Empty line (forced break) — use default font metrics.
		maxAscent = 16 * 0.8
		maxDescent = 16 * 0.2
		lineHeight = maxAscent + maxDescent
	}

	// Step 2: Compute text-align offset.
	alignOffset := computeTextAlignOffset(line, availableInline, wdm)

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
	lineBuilder := NewBoxFragmentBuilder(lineWDM)
	lineBuilder.SetSize(LogicalSize{
		InlineSize: availableInline,
		BlockSize:  lineHeight,
	})

	// Step 3a: Pre-pass — generate background/border fragments for inline spans.
	// These are added FIRST so they paint behind content (CSS 2.1 Appendix E).
	// Inline backgrounds may extend outside the line box (border/padding bleed).
	//
	// CSS 2.1 §9.2.1.1: for a split inline element (block-in-inline), the
	// inline-start border/padding appears only on the first fragment and the
	// inline-end border/padding appears only on the last fragment.
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
			item            *InlineItem
			style           *css.Style
			node            *html.Node
			borderStart     float64
			isFirstFragment bool
			isLastFragment  bool
		}
		var spanStack []spanEntry
		emit := func(span spanEntry, endPos float64) {
			// Emit a fragment when there is something to paint OR when the span
			// is positioned. Positioned inlines (relative/sticky) need a
			// fragment per line so descendant abspos children can derive their
			// inline containing block from the union of these rects (CSS 2.1
			// §10.1.4 / CSS Position 3 §def-cb). The fragment is also where
			// RelativeOffset is anchored for paint-time sticky/relative shifts.
			if !(hasVisibleInlinePaint(span.style) || span.style.GetPosition() != css.PositionStatic) {
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
			spanBlockSize := blockOverhang + lineHeight + geom.Padding.BlockEnd + geom.Border.BlockEnd
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
					Border:  ToPhysicalEdges(geom.Border, wdm),
					Padding: ToPhysicalEdges(geom.Padding, wdm),
				},
			}
			// CSS 2.1 §9.4.3: inline span backgrounds also shift with
			// position:relative. Use the original (non-reset) style.
			// Sticky is scroll-time, not layout-time — leave zero.
			if span.style.GetPosition() == css.PositionRelative {
				offset := span.style.GetPositionOffsetResolved(cbPhysicalSize.Width, cbPhysicalSize.Height)
				bgFrag.RelativeOffset = computeRelativeOffset(offset, wdm)
			}
			lineBuilder.AddChild(bgFrag, LogicalOffset{
				InlineOffset: fragStart,
				BlockOffset:  -blockOverhang,
			})
		}

		// Pre-populate with spans opened on prior lines. These enter the line
		// at the content-area start (alignOffset) with no inline-start
		// border/padding (they are non-first-line fragments).
		for _, item := range enteringSpanStack {
			if item == nil || item.Style == nil {
				continue
			}
			spanStack = append(spanStack, spanEntry{
				item:            item,
				style:           item.Style,
				node:            item.Node,
				borderStart:     alignOffset,
				isFirstFragment: false,
				isLastFragment:  false,
			})
		}

		trackPos := alignOffset
		for _, r := range line.Results {
			switch r.Item.Type {
			case InlineItemOpenTag:
				if r.Item.Style != nil {
					spanStack = append(spanStack, spanEntry{
						item:            r.Item,
						style:           r.Item.Style,
						node:            r.Item.Node,
						borderStart:     trackPos + r.Margins.InlineStart,
						isFirstFragment: r.Item.IsFirstFragment,
						isLastFragment:  r.Item.IsLastFragment,
					})
				}
			case InlineItemCloseTag:
				if len(spanStack) > 0 && r.Item.Style != nil {
					span := spanStack[len(spanStack)-1]
					spanStack = spanStack[:len(spanStack)-1]
					// The closing fragment carries inline-end border/padding
					// only if the InlineItem is the last split fragment. A
					// prior-line pre-populated entry inherits this from the
					// InlineItem being closed now.
					span.isLastFragment = r.Item.IsLastFragment
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
			asc = alignmentAscentFromFont(sidewaysVLR, fs, fontPath)
			desc = alignmentDescentFromFont(sidewaysVLR, fs, fontPath)
		}
		lineHt := style.GetLineHeight()
		if style.IsLineHeightNormal() && !centralBaseline {
			fontPath := resolveFontPath(style, fonts)
			lineHt = text.FontHeightFromFont(fs, fontPath)
		}
		halfLeading := (lineHt - (asc + desc)) / 2
		asc += halfLeading
		desc += halfLeading
		return
	}
	inlinePos := alignOffset
	for _, r := range line.Results {
		switch r.Item.Type {
		case InlineItemOpenTag:
			if r.Item.Style != nil {
				va := r.Item.Style.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					a, d := inlineBoxAsDesc(r.Item.Style)
					vaStack = append(vaStack, vaFrame{vAlign: va, rootAsc: a, rootDesc: d})
				}
			}
		case InlineItemCloseTag:
			if r.Item.Style != nil {
				va := r.Item.Style.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					if n := len(vaStack); n > 0 {
						vaStack = vaStack[:n-1]
					}
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

			fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
			var ascent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: in vertical modes with central
				// baseline, use fontSize / 2.
				ascent = fontSize / 2
			} else {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath)
			}

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
			} else if r.Item.Style != nil {
				va := r.Item.Style.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					effectiveVA = va
					rootAsc, rootDesc = inlineBoxAsDesc(r.Item.Style)
				}
			}
			switch effectiveVA {
			case css.VerticalAlignTop:
				blockPos = rootAsc - ascent
			case css.VerticalAlignBottom:
				blockPos = lineHeight - rootDesc - ascent
			}

			// Use parent element as Node so the renderer can access styles.
			parentNode := r.Item.Node
			if parentNode != nil && parentNode.Parent != nil {
				parentNode = parentNode.Parent
			}

			textFrag := &PhysicalFragment{
				Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
					InlineSize: r.InlineSize,
					BlockSize:  fontSize,
				}, wdm.WM)),
				Type:             FragmentText,
				TextContent:      content,
				BidiLevel:        r.Item.BidiLevel,
				Node:             parentNode,
				Style:            r.Item.Style,
				WritingDirection: wdm,
			}

			// CSS 2.1 §9.4.3: Apply position:relative offset to inline-level
			// text fragments. Only applies when the parent is a true inline element
			// (display:inline), not a block container. Block containers handle their
			// own position:relative offset in block layout — applying it here would
			// double-offset the text.
			if r.Item.Style != nil && r.Item.Style.GetDisplay() == css.DisplayInline {
				pos := r.Item.Style.GetPosition()
				if pos == css.PositionRelative {
					offset := r.Item.Style.GetPositionOffsetResolved(cbPhysicalSize.Width, cbPhysicalSize.Height)
					textFrag.RelativeOffset = computeRelativeOffset(offset, wdm)
				}
			}

			lineBuilder.AddChild(textFrag, LogicalOffset{
				InlineOffset: inlinePos,
				BlockOffset:  blockPos,
			})

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
				default:
					// CSS 2.1 §10.8.1: For display:inline-block with overflow:visible,
					// align inline-block so its baseline sits at the line's maxAscent.
					// Also handle inline-tables, replaced elements, and other atomic
					// inlines with baselines.
					display := css.DisplayBlock
					if r.Item.Style != nil {
						display = r.Item.Style.GetDisplay()
					}
					isReplaced := r.Item.Node != nil && isReplacedElement(r.Item.Node)
					isInlineBlockLike := r.Item.Style != nil &&
						(display == css.DisplayInlineBlock || display == css.DisplayInlineFlex ||
							display == css.DisplayFlex || display == css.DisplayTable || display == css.DisplayInlineTable) &&
						r.Item.Style.GetOverflowX() == css.OverflowVisible && r.Item.Style.GetOverflowY() == css.OverflowVisible
					isAtomicForBaseline := isInlineBlockLike || isReplaced
					// For inline-flex, use first baseline (CSS Flexbox §4.2).
					// For inline-block/inline-table, use last baseline (CSS 2.1 §10.8.1).
					// Replaced elements don't propagate baselines from line boxes.
					// For orthogonal inline-blocks, synthesize baseline at the
					// block-end edge in the outer writing mode (per Blink, matches
					// CSS Writing Modes §4.3 "no baseline from orthogonal content").
					atomicBaseline := float64(0)
					childIsOrthogonal := false
					if r.Item.Style != nil {
						childIsOrthogonal = NewWritingDirectionMode(r.Item.Style).IsOrthogonalTo(wdm)
					}
					if !isReplaced {
						if childIsOrthogonal {
							atomicBaseline = blockSize
						} else {
							atomicBaseline = r.LayoutResult.LastBaseline
							if display == css.DisplayInlineFlex && r.LayoutResult.Baseline > 0 {
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
							// CSS 2.1 §10.8.1: For replaced elements, baseline
							// is at the bottom. For inline-blocks with no line
							// boxes, baseline is the bottom margin edge.
							ibAscent = blockSize
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
				if blockPos < 0 {
					blockPos = 0
				}
				// CSS 2.1 §9.4.3: Apply position:relative offset to atomic inlines.
				// Sticky is scroll-time, not layout-time.
				if r.Item.Style != nil {
					pos := r.Item.Style.GetPosition()
					if pos == css.PositionRelative {
						offset := r.Item.Style.GetPositionOffsetResolved(cbPhysicalSize.Width, cbPhysicalSize.Height)
						r.LayoutResult.Fragment.RelativeOffset = computeRelativeOffset(offset, wdm)
					}
				}
				lineBuilder.AddChild(r.LayoutResult.Fragment, LogicalOffset{
					InlineOffset: inlinePos,
					BlockOffset:  blockPos,
				})
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
	result.Fragment.Type = FragmentLineBox
	return result.Fragment, lineHeight, maxAscent, residualSpanStack
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
func alignmentAscentFromFont(swap bool, fontSize float64, fontPath string) float64 {
	if swap {
		return text.FontDescentFromFont(fontSize, fontPath)
	}
	return text.FontAscentFromFont(fontSize, fontPath)
}

// alignmentDescentFromFont returns the distance from the alphabetic baseline
// to the line's block-end for a font. When swap is true, this returns the
// typographic ascent.
func alignmentDescentFromFont(swap bool, fontSize float64, fontPath string) float64 {
	if swap {
		return text.FontAscentFromFont(fontSize, fontPath)
	}
	return text.FontDescentFromFont(fontSize, fontPath)
}

// computeLineMetrics is the backward-compatible wrapper that uses wdm.UsesCentralBaseline().
func computeLineMetrics(line *LineInfo, wdm WritingDirectionMode, fonts text.FontConfig) (maxAscent, maxDescent float64) {
	return computeLineMetricsEx(line, wdm, fonts, wdm.UsesCentralBaseline(), nil)
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
func computeLineMetricsEx(line *LineInfo, wdm WritingDirectionMode, fonts text.FontConfig, centralBaseline bool, parentStyle *css.Style) (maxAscent, maxDescent float64) {
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
			fontPath := resolveFontPath(parentStyle, fonts)
			strutAscent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath)
			strutDescent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath)
		}
		// CSS 2.1 §10.8.1: line-height: normal uses the font's recommended
		// line height rather than a fixed 1.2× multiplier. This ensures the
		// strut height matches the font's built-in metrics.
		lineHt := parentStyle.GetLineHeight()
		if parentStyle.IsLineHeightNormal() && !centralBaseline {
			fontPath := resolveFontPath(parentStyle, fonts)
			lineHt = text.FontHeightFromFont(fontSize, fontPath)
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

	for _, r := range line.Results {
		switch r.Item.Type {
		case InlineItemCloseTag:
			if r.Item.Style != nil {
				va := r.Item.Style.GetVerticalAlign()
				if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
					topBottomDepth--
				}
			}

		case InlineItemOpenTag:
			// Empty inline boxes (e.g. <span></span>) have no InlineItemText but
			// still establish a strut: their font's ascent/descent determine the
			// minimum line box height per CSS 2.1 §10.8.
			if r.Item.Style == nil {
				continue
			}
			va := r.Item.Style.GetVerticalAlign()
			if va == css.VerticalAlignTop || va == css.VerticalAlignBottom {
				topBottomDepth++
			}
			fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
			var ascent, descent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: central baseline = fontSize / 2.
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath)
				descent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath)
			}
			// CSS 2.1 §10.8.1: distribute half-leading from line-height.
			// Negative half-leading (when line-height < font-size) is valid
			// and reduces the inline box's ascent/descent contribution.
			lineHt := r.Item.Style.GetLineHeight()
			if r.Item.Style.IsLineHeightNormal() && !centralBaseline {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				lineHt = text.FontHeightFromFont(fontSize, fontPath)
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
			if !isInTopBottom && r.Item.Style != nil {
				va := r.Item.Style.GetVerticalAlign()
				isInTopBottom = va == css.VerticalAlignTop || va == css.VerticalAlignBottom
			}
			fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
			var ascent, descent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: central baseline = fontSize / 2.
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath)
				descent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath)
			}
			// CSS 2.1 §10.8.1: distribute half-leading from line-height.
			// Negative half-leading (when line-height < font-size) is valid
			// and reduces the inline box's ascent/descent contribution.
			if r.Item.Style != nil {
				lineHt := r.Item.Style.GetLineHeight()
				if r.Item.Style.IsLineHeightNormal() && !centralBaseline {
					fontPath := resolveFontPath(r.Item.Style, fonts)
					lineHt = text.FontHeightFromFont(fontSize, fontPath)
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
			if r.Item.Style == nil {
				continue
			}
			fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
			var ascent, descent float64
			if centralBaseline {
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath)
				descent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath)
			}
			lineHt := r.Item.Style.GetLineHeight()
			if r.Item.Style.IsLineHeightNormal() && !centralBaseline {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				lineHt = text.FontHeightFromFont(fontSize, fontPath)
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
				isReplaced := r.Item.Node != nil && isReplacedElement(r.Item.Node)
				isInlineBlockLike := r.Item.Style != nil &&
					(display == css.DisplayInlineBlock || display == css.DisplayInlineFlex ||
						display == css.DisplayFlex || display == css.DisplayTable || display == css.DisplayInlineTable) &&
					r.Item.Style.GetOverflowX() == css.OverflowVisible && r.Item.Style.GetOverflowY() == css.OverflowVisible
				isAtomicForBaseline := isInlineBlockLike || isReplaced
				// For inline-flex, use first baseline (CSS Flexbox §4.2).
				// For inline-block/inline-table, use last baseline (CSS 2.1 §10.8.1).
				// Replaced elements don't propagate baselines from line boxes.
				// For orthogonal inline-blocks, synthesize baseline at block-end
				// (matches Blink for orthogonal writing-mode roots).
				atomicBaseline := float64(0)
				childIsOrthogonal := false
				if r.Item.Style != nil {
					childIsOrthogonal = NewWritingDirectionMode(r.Item.Style).IsOrthogonalTo(wdm)
				}
				if !isReplaced {
					if childIsOrthogonal {
						atomicBaseline = blockSize
					} else if (display == css.DisplayInlineFlex || display == css.DisplayFlex) && r.LayoutResult.Baseline > 0 {
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
						// CSS 2.1 §10.8.1: For replaced elements, baseline is at
						// the bottom. For inline-blocks with no line boxes, baseline
						// is the bottom margin edge.
						ibAscent = blockSize
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

// computeTextAlignOffset computes the starting inline offset for text-align.
// CSS Text §7.1: "start" and "end" are direction-relative; "left" and "right"
// are physical and independent of direction.
func computeTextAlignOffset(line *LineInfo, availableInline float64, wdm WritingDirectionMode) float64 {
	slack := availableInline - line.Width

	// Center alignment returns slack/2 even when negative (content overflows),
	// matching Blink's behavior. This ensures text is centered symmetrically
	// regardless of whether it fits in the container.
	switch line.TextAlign {
	case "center", "-webkit-center":
		return slack / 2
	}

	if slack <= 0 {
		return 0
	}

	switch line.TextAlign {
	case "right":
		return slack
	case "end":
		if wdm.IsRTL() {
			return 0 // RTL end = physical left
		}
		return slack // LTR end = physical right
	case "start":
		if wdm.IsRTL() {
			return slack // RTL start = physical right
		}
		return 0 // LTR start = physical left
	case "justify":
		if line.IsLastLine || line.HasForcedBreak {
			// Last line falls back to start alignment.
			if wdm.IsRTL() {
				return slack // RTL start = physical right
			}
			return 0 // LTR start = physical left
		}
		// TODO: distribute inter-word spacing for justify.
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
func lineHasOnlyOutOfFlow(line *LineInfo, itemsData *InlineItemsData) bool {
	if line.HasForcedBreak || line.Width > 0 {
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
			// CSS 2.1 §9.2.1.1 + §9.4.2: an inline element with visible paint
			// (background, border, or non-zero padding/margin) still generates
			// a line box even when empty — its box decorations must render.
			// Empty span with padding/border is the canonical case
			// (wpt-css2/linebox/empty-inline-002).
			if r.Item.Style != nil && hasVisibleInlineBoxDecoration(r.Item.Style) {
				return false
			}
		}
	}
	return true
}

// hasVisibleInlineBoxDecoration returns true if the inline element has
// any box decoration that must paint: visible background/border, or any
// non-zero padding/margin that affects the inline-box extent. Ported
// from Blink's ComputedStyle::HasBoxDecorationBackground semantics for
// inline boxes.
func hasVisibleInlineBoxDecoration(style *css.Style) bool {
	if hasVisibleInlinePaint(style) {
		return true
	}
	pad := style.GetPadding()
	if pad.Top > 0 || pad.Right > 0 || pad.Bottom > 0 || pad.Left > 0 {
		return true
	}
	m := style.GetMargin()
	if m.Top > 0 || m.Right > 0 || m.Bottom > 0 || m.Left > 0 {
		return true
	}
	return false
}
