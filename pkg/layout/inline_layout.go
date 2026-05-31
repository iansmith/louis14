package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
	"louis14/pkg/text"
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
		// Phase 20 P20.6 (float extension): floats with break-inside:avoid
		// (or otherwise monolithic) within an IFC contribute their block-size
		// as TallestUnbreakable during the multicol initial column-balancing
		// pass. Mirrors Blink fragmentation_utils.cc:1105-1113 — any
		// ShouldAvoidBreakInside child propagates its block-extent so the
		// multicol's column auto-block-size grows to fit. Without this,
		// a float with break-inside:avoid larger than the natural column
		// estimate gets cropped by P20.5's container OverflowClip.
		if bla.space.IsInitialColumnBalancingPass &&
			childResult != nil &&
			ShouldAvoidBreakInside(bla.space, childResult) {
			builder.PropagateTallestUnbreakableBlockSize(childLogical.BlockSize() + childMargins.BlockSum())
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
		ExclusionSpace:                 exclusionSpace,
		OrthogonalFallbackInlineSize:   bla.space.OrthogonalFallbackInlineSize,
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
	// CSS Values §10.2: calc() with percent is resolved against the containing
	// block's inline-size, same as bare-percent text-indent.
	textIndent := 0.0
	if bla.style != nil {
		textIndent = bla.style.ResolveTextIndent(contentInlineSize)
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
		cbBlockSize := bla.space.AvailableSize.BlockSize.Float64()
		if cbBlockSize == Indefinite {
			cbBlockSize = 0
		}
		cbPhys := ToPhysicalSize(LogicalSize{
			InlineSize: contentInlineSize,
			BlockSize:  cbBlockSize,
		}, wdm.WM)
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
		lineFragment, lineHeight, lineAscent, residualStack := createLineBoxEx(
			itemsData, &line, effectiveWDM, lineVisualInline, fonts, centralBaseline, cbPhys, bla.style, openInlineStack, firstLineBgStyle, isFirstLineForCreate,
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
			if blockOffset+lineHeight > fragEnd && bla.space.FragmentainerOffset+blockOffset > 0 {
				// This line overflows. Stop here and signal the parent to create a
				// break token so the next column resumes from this line.
				// blockOffset > 0 guard: never emit an empty column (at least one
				// line must have been placed, mirroring Blink's RequiresContent guard).
				// Per Blink: shortage is the minimum additional space needed
				// to fit the overflowing line (per-break-point), not the
				// cumulative overflow across all fragmentainers.
				shortage := (blockOffset + lineHeight) - fragEnd
				if shortage < 0 {
					shortage = 0
				}
				inlineBreakToken = &BlockBreakToken{
					Node:                 bla.node,
					ConsumedBlockSize:    layoutunit.FromFloat64Round(blockOffset + bla.space.FragmentainerOffset),
					InlineItemStartIndex: lineStartIdx,
					InlineTextOffset:     lineStartTextOffset,
					InlineShortage:       shortage,
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

		// Phase 20 P20.6: during the multicol initial column-balancing
		// pass, propagate each atomic inline's block-extent as a
		// TallestUnbreakable contribution. Atomic inlines (display:
		// inline-block, inline-flex, inline-grid, inline-table, replaced
		// inline elements) cannot break across fragmentainer boundaries —
		// they are implicitly monolithic for column-balancing. Without
		// this propagation, the multicol's column auto-block-size doesn't
		// account for the atomic inline's height; combined with the
		// multicol container OverflowClip (P20.5), this leaves columns
		// too short to contain the atomic inline and clips legitimate
		// content (e.g. inline-block-and-column-span-all). Mirrors Blink
		// fragmentation_utils.cc:1105-1113 PropagateTallestUnbreakableBlockSize
		// for ShouldAvoidBreakInside children — atomic inlines hit this
		// path in Blink via line-box inclusion of monolithic content.
		if bla.space.IsInitialColumnBalancingPass {
			lineHasAtomicInline := false
			for _, r := range line.Results {
				if r.Item != nil && r.Item.Type == InlineItemAtomicInline &&
					r.LayoutResult != nil && r.LayoutResult.Fragment != nil {
					lineHasAtomicInline = true
					break
				}
			}
			if lineHasAtomicInline {
				// Propagate just the line's block-extent (without
				// blockOffset). During the initial column-balancing pass
				// the BLA isn't placed inside a fragmentainer yet — the
				// fragmentainer block-offset is 0 for these candidates.
				// What matters as a TallestUnbreakable floor is the
				// individual line's height (the unbreakable unit), not
				// the cumulative offset of the line within the IFC.
				// Mirrors Blink's per-child PropagateTallestUnbreakableBlockSize
				// in the initial balancing pass, where each child contributes
				// its own block-extent independently.
				builder.PropagateTallestUnbreakableBlockSize(lineHeight)
			}
		}

		blockOffset += lineHeight
	}

	if firstLineAscent < 0 {
		firstLineAscent = 0
	}
	return blockOffset, exclusionSpace, firstLineAscent, lastBaselineOffset, inlineBreakToken
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
// CapHeight / LhSize, which GetUsedFontSize and the ch/ex/cap/lh unit
// resolvers read in preference to the raw Properties. When the merge
// overrides font-size or line-height, those caches go stale (the base font's
// size wins over the overridden Properties), so we re-resolve them against
// the merged font — the same metric pass computeChWidths runs over every
// node style. Blink's ::first-line ComputedStyle is itself a fully resolved
// style; this re-resolution is the louis14 analog.
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
	resolveFontMetricsForStyle(merged, fonts, newFontMetricsMeasurer(fonts))
	return merged
}

// firstLineDirectlySpecified returns the set of ::first-line-allowed properties
// that the originating element (with computed style `elem`) set directly,
// detected as a value that differs from the value the element inherits from its
// nearest ancestor element (`parent`). Only properties the ::first-line style
// would actually override are reported, so the caller's skip set stays minimal.
// These are the properties whose ::first-line value must NOT clobber the
// element's own — the louis14 analog of a descendant declaration outranking the
// inherited ::first-line value (see mergeFirstLineStyleExcept).
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
	// element, so its inherited properties reach descendants only via
	// inheritance — an element on the first line that specifies a property
	// directly keeps its own value. We detect a direct specification by
	// comparing each element's computed value against the value it inherits
	// from its nearest ancestor element. The enclosing-element style stack is
	// seeded from openInlineStack (inline elements still open from prior lines)
	// over the container style, then tracks open/close tags as we walk the
	// line. Mirrors Blink resolving a per-descendant first-line ComputedStyle
	// (StyleResolver::StyleForFirstLineStyle @ 4883d11fef).
	stack := make([]*css.Style, 0, len(openInlineStack)+4)
	stack = append(stack, containerStyle)
	for _, open := range openInlineStack {
		if open != nil && open.Style != nil {
			stack = append(stack, open.Style)
		}
	}
	enclosing := func() *css.Style { return stack[len(stack)-1] }

	// Items on the first line commonly share a (base style, enclosing style)
	// pair; the merge clones + re-resolves font metrics on each call, so
	// memoize by that pair to collapse O(items) merges to O(distinct pairs).
	type mergeKey struct{ base, parent *css.Style }
	mergeCache := make(map[mergeKey]*css.Style)
	for i := range line.Results {
		r := &line.Results[i]
		if r.Item == nil || r.Item.Style == nil {
			continue
		}
		// Only apply to text items and open/close tags (inline spans).
		switch r.Item.Type {
		case InlineItemText, InlineItemOpenTag, InlineItemCloseTag:
			// The element this item belongs to inherits from `parent`: for an
			// open tag, the currently-enclosing element; for text/close tags,
			// the element below the one being closed (text shares its element's
			// style, so comparing against that same element would never detect
			// a direct specification — compare against the grandparent instead).
			parent := enclosing()
			if r.Item.Type != InlineItemOpenTag && len(stack) >= 2 {
				parent = stack[len(stack)-2]
			}
			key := mergeKey{r.Item.Style, parent}
			merged, ok := mergeCache[key]
			if !ok {
				skip := firstLineDirectlySpecified(r.Item.Style, parent, firstLineStyle)
				merged = mergeFirstLineStyleExcept(r.Item.Style, firstLineStyle, skip, fonts)
				mergeCache[key] = merged
			}
			if merged != r.Item.Style {
				r.Style = merged
			}
		}
		// Maintain the enclosing-element stack after deciding this item.
		switch r.Item.Type {
		case InlineItemOpenTag:
			stack = append(stack, r.Item.Style)
		case InlineItemCloseTag:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
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
	frag, h, a, _ := createLineBoxEx(itemsData, line, wdm, availableInline, fonts, wdm.UsesCentralBaseline(), PhysicalSize{}, nil, nil, nil, true)
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
	firstLineStyle *css.Style,
	isFirstLine bool,
) (*PhysicalFragment, float64, float64, []*InlineItem) { // returns (fragment, lineHeight, maxAscent, residualSpanStack)
	// Step 1: Compute line height from font metrics of all items.
	maxAscent, maxDescent := computeLineMetricsEx(line, wdm, fonts, centralBaseline, parentStyle)

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

	lineHeight := maxAscent + maxDescent
	if lineHeight <= 0 {
		// Empty line (forced break) — use default font metrics.
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
	if line.TextAlign == "justify" && !line.IsLastLine && !line.HasForcedBreak {
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
	lineBuilder := NewBoxFragmentBuilder(lineWDM)
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
	// modelling ::first-line as a `<span>`): so its background covers the same
	// box an inline span on this line would — the line's CONTENT inline extent
	// (line.Width starting at the text-align offset), at the full line-box
	// HEIGHT (lineHeight). This mirrors the inline span-background pre-pass
	// below, which sizes each span fragment as `lineHeight` tall and
	// content-wide (`fragEnd − fragStart`); the ::first-line band is the
	// degenerate single-span case. Mirrors Blink's
	// `LineBoxFragmentPainter::PaintBackgroundBorderShadow`
	// (`core/paint/inline_box_fragment_painter.cc` @ 4883d11fef), whose
	// `line_style_` is the ::first-line-aware style.
	if firstLineStyle != nil && hasVisibleInlinePaint(firstLineStyle) {
		bgFrag := &PhysicalFragment{
			Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
				InlineSize: line.Width,
				BlockSize:  lineHeight,
			}, wdm.WM)),
			Type:             FragmentBox,
			Style:            firstLineStyle,
			WritingDirection: wdm,
		}
		lineBuilder.AddChild(bgFrag, LogicalOffset{
			InlineOffset: alignOffset,
			BlockOffset:  0,
		})
	}

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
			item             *InlineItem
			style            *css.Style
			node             *html.Node
			borderStart      float64
			isFirstFragment  bool
			isLastFragment   bool
			cumulativeVAOffs float64 // sum of vertical-align: <length> from this span + all open ancestor spans
			openSeq          int     // monotonically increasing push order: ancestor-before-descendant, earlier-sibling-first
		}
		var spanStack []spanEntry
		// openSeqCounter assigns a unique, monotonically increasing index to each
		// span as it is pushed. Spans entering from prior lines (outermost) are
		// pushed first, then OpenTags in source order — so a lower openSeq is
		// always an ancestor or earlier sibling of a higher one.
		openSeqCounter := 0
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
			frag   *PhysicalFragment
			offset LogicalOffset
			seq    int
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
			// also where RelativeOffset is anchored for paint-time sticky/
			// relative shifts.
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
					Border:    ToPhysicalEdges(geom.Border, wdm),
					Padding:   ToPhysicalEdges(geom.Padding, wdm),
					Scrollbar: ToPhysicalEdges(geom.Scrollbar, wdm),
				},
			}
			// CSS 2.1 §9.4.3: inline span backgrounds also shift with
			// position:relative. Use the original (non-reset) style.
			// Sticky is scroll-time, not layout-time — leave zero.
			if span.style.GetPosition() == css.PositionRelative {
				offset := span.style.GetPositionOffsetResolved(cbPhysicalSize.Width, cbPhysicalSize.Height)
				bgFrag.RelativeOffset = computeRelativeOffset(offset, wdm)
			}
			// CSS 2.1 §10.8.1: vertical-align: <length> on an inline element
			// shifts the inline box (including its background) up by the
			// offset, matching the shift applied to its text children. Use
			// the cumulative offset so nested spans stack correctly.
			spanBlockOffset := -blockOverhang - span.cumulativeVAOffs
			pendingBg = append(pendingBg, pendingBgFrag{
				frag: bgFrag,
				offset: LogicalOffset{
					InlineOffset: fragStart,
					BlockOffset:  spanBlockOffset,
				},
				seq: span.openSeq,
			})
		}

		// Pre-populate with spans opened on prior lines. These enter the line
		// at the content-area start (alignOffset) with no inline-start
		// border/padding (they are non-first-line fragments).
		for _, item := range enteringSpanStack {
			if item == nil || item.Style == nil {
				continue
			}
			parentCum := 0.0
			if n := len(spanStack); n > 0 {
				parentCum = spanStack[n-1].cumulativeVAOffs
			}
			spanStack = append(spanStack, spanEntry{
				item:             item,
				style:            item.Style,
				node:             item.Node,
				borderStart:      alignOffset,
				isFirstFragment:  false,
				isLastFragment:   false,
				cumulativeVAOffs: parentCum + item.Style.GetVerticalAlignOffset(),
				openSeq:          openSeqCounter,
			})
			openSeqCounter++
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
					parentCum := 0.0
					if n := len(spanStack); n > 0 {
						parentCum = spanStack[n-1].cumulativeVAOffs
					}
					spanStack = append(spanStack, spanEntry{
						item:             r.Item,
						style:            effStyle,
						node:             r.Item.Node,
						borderStart:      trackPos + r.Margins.InlineStart,
						isFirstFragment:  r.Item.IsFirstFragment,
						isLastFragment:   r.Item.IsLastFragment,
						cumulativeVAOffs: parentCum + effStyle.GetVerticalAlignOffset(),
						openSeq:          openSeqCounter,
					})
					openSeqCounter++
				}
			case InlineItemCloseTag:
				if len(spanStack) > 0 && effStyle != nil {
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

		// Add the buffered inline-background fragments in ancestor-first order
		// (lowest openSeq first), so descendant inline backgrounds paint on top
		// of their ancestors' (matching Blink's paint-own-background-then-recurse
		// order). openSeq is unique per push; SliceStable is belt-and-suspenders.
		sort.SliceStable(pendingBg, func(a, b int) bool {
			return pendingBg[a].seq < pendingBg[b].seq
		})
		for _, p := range pendingBg {
			lineBuilder.AddChild(p.frag, p.offset)
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
	// Pre-populate vaLengthStack with spans opened on prior lines so their
	// length offsets continue to affect this line's descendant fragments.
	for _, item := range enteringSpanStack {
		if item == nil || item.Style == nil {
			vaLengthStack = append(vaLengthStack, 0)
			continue
		}
		off := item.Style.GetVerticalAlignOffset()
		vaLengthStack = append(vaLengthStack, off)
		vaLengthOffset += off
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
				// Push the span's length offset onto the cumulative stack so
				// descendant text/atomic-inline fragments inherit the shift.
				off := rStyle.GetVerticalAlignOffset()
				vaLengthStack = append(vaLengthStack, off)
				vaLengthOffset += off
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

			fontSize, _, _, _, _ := fontPropsFromStyle(rStyle)
			var ascent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: in vertical modes with central
				// baseline, use fontSize / 2.
				ascent = fontSize / 2
			} else {
				fontPath := resolveFontPath(rStyle, fonts)
				// CSS Fonts 4 §6.1-§6.3: ascent-override affects the strut (line-box
				// height) but NOT the per-item placement within the line. The renderer
				// draws the glyph at box.Y + nativeAscent; if we used the overridden
				// ascent here, blockPos = maxAscent - overriddenAscent would shift the
				// fragment up, making the glyph land at a different visual position
				// than the reference. Pass nil so the native font ascent is used for
				// positioning, matching Blink's text_fragment_painter.cc paint path
				// which reads metrics.Ascent from the underlying font data directly.
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, nil)
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

				// CSS 2.1 §9.4.3: Apply position:relative offset to inline-level
				// text fragments. Only applies when the parent is a true inline element
				// (display:inline), not a block container. Block containers handle their
				// own position:relative offset in block layout — applying it here would
				// double-offset the text.
				if rStyle != nil && rStyle.GetDisplay() == css.DisplayInline {
					pos := rStyle.GetPosition()
					if pos == css.PositionRelative {
						offset := rStyle.GetPositionOffsetResolved(cbPhysicalSize.Width, cbPhysicalSize.Height)
						frag.RelativeOffset = computeRelativeOffset(offset, wdm)
					}
				}
				lineBuilder.AddChild(frag, LogicalOffset{
					InlineOffset: pieceOffset,
					BlockOffset:  blockPos,
				})
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
					itemsData.TextContent,
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
// reg is passed through to FontAscentFromFont / FontDescentFromFont for
// CSS Fonts 4 metric-override support.
func alignmentAscentFromFont(swap bool, fontSize float64, fontPath string, reg *text.FontRegistry) float64 {
	if swap {
		return text.FontDescentFromFont(fontSize, fontPath, reg)
	}
	return text.FontAscentFromFont(fontSize, fontPath, reg)
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
			strutAscent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
			strutDescent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
		}
		// CSS 2.1 §10.8.1: line-height: normal uses the font's recommended
		// line height rather than a fixed 1.2× multiplier. This ensures the
		// strut height matches the font's built-in metrics.
		lineHt := parentStyle.GetLineHeight()
		if parentStyle.IsLineHeightNormal() && !centralBaseline {
			fontPath := resolveFontPath(parentStyle, fonts)
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
				fontPath := resolveFontPath(rStyle, fonts)
				ascent = alignmentAscentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
				descent = alignmentDescentFromFont(sidewaysVLR, fontSize, fontPath, fonts.Registry)
			}
			// CSS 2.1 §10.8.1: distribute half-leading from line-height.
			// Negative half-leading (when line-height < font-size) is valid
			// and reduces the inline box's ascent/descent contribution.
			if rStyle != nil {
				lineHt := rStyle.GetLineHeight()
				if rStyle.IsLineHeightNormal() && !centralBaseline {
					fontPath := resolveFontPath(rStyle, fonts)
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
