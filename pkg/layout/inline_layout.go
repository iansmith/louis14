package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
	"strings"
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
// isTextContentEmpty returns true if text consists entirely of collapsible
// whitespace (ASCII space, tab, newline, CR, form-feed). Non-breaking spaces
// (U+00A0) are NOT collapsible and are treated as content per CSS §16.6.1,
// matching the behaviour of collectTextNode which preserves U+00A0.
func isTextContentEmpty(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != '\f' {
			return false
		}
	}
	return true
}

func hasOnlyInlineChildren(node *LayoutInputNode) bool {
	hasContent := false
	for _, child := range node.Children() {
		if child.IsText() {
			if !isTextContentEmpty(child.TextContent()) {
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
) (blockSizeUsed float64, updatedES *ExclusionSpace, firstLineAscent float64, lastBaselineOffset float64) {
	// Phase 1: Collect inline items from the layout subtree.
	itemsData := CollectInlines(bla.node)

	if len(itemsData.Items) == 0 {
		return 0, exclusionSpace, 0, 0
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

	// Phase 1c: Lay out inline floats and register them in the exclusion space.
	// CSS 2.1 §9.5.1: floats are placed as high as possible.
	// Floats in an IFC must be positioned before line breaking so that
	// FindAvailableInlineSize returns the correct narrowed width.
	if exclusionSpace == nil {
		exclusionSpace = &ExclusionSpace{}
	}
	for _, item := range itemsData.Items {
		if item.Type != InlineItemFloat || item.LayoutNode == nil {
			continue
		}
		// Lay out all floats (including those with content children)
		// so they participate in the exclusion space and are rendered.
		childStyle := item.Style
		if childStyle == nil {
			continue
		}
		childWDM := NewWritingDirectionMode(childStyle)
		// Resolve float margins in the parent's coordinates for positioning.
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
		floatInlineSize := childMargins.InlineSum() + childLogical.InlineSize()
		floatBlockSize := childMargins.BlockSum() + childLogical.BlockSize()
		floatSide := childStyle.GetFloat()
		// CSS float:left/right are physical. Convert to logical for positioning.
		logicalSide := floatSide
		if wdm.Dir == DirectionRTL {
			if floatSide == css.FloatLeft {
				logicalSide = css.FloatRight
			} else {
				logicalSide = css.FloatLeft
			}
		}
		floatBlockOffset := exclusionSpace.FindFloatPosition(logicalSide, floatInlineSize, floatBlockSize, contentInlineSize, bfcBlockOrigin)
		var floatInlineOffset float64
		if logicalSide == css.FloatLeft {
			startOff, _ := exclusionSpace.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
			floatInlineOffset = startOff + childMargins.InlineStart
		} else {
			_, endOff := exclusionSpace.FindAvailableInlineSize(floatBlockOffset, floatBlockSize, contentInlineSize)
			floatInlineOffset = contentInlineSize - endOff - childMargins.InlineEnd - childLogical.InlineSize()
		}
		// The float's position and exclusion are in BFC coordinates. Convert
		// the block offset to local coordinates for the fragment offset.
		localFloatBlock := floatBlockOffset - bfcBlockOrigin
		builder.AddChild(childResult.Fragment, LogicalOffset{
			InlineOffset: floatInlineOffset,
			BlockOffset:  localFloatBlock + childMargins.BlockStart,
		})
		exclusionSpace = exclusionSpace.Add(Exclusion{
			InlineOffset: floatInlineOffset - childMargins.InlineStart,
			BlockOffset:  floatBlockOffset, // BFC-relative
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

	// Propagate the percentage resolution block-size from the parent constraint
	// space so that percentage-height children (e.g., img { height: 100% })
	// can resolve against their containing block's definite height.
	lineAvailBlock := Indefinite
	if bla.space.PercentageResolutionSize.BlockSize > 0 {
		lineAvailBlock = bla.space.PercentageResolutionSize.BlockSize
	} else if bla.space.AvailableSize.BlockSize >= 0 {
		lineAvailBlock = bla.space.AvailableSize.BlockSize
	}
	lineSpace := ConstraintSpace{
		AvailableSize:            LogicalSize{InlineSize: lineAvailableWidth, BlockSize: lineAvailBlock},
		PercentageResolutionSize: bla.space.PercentageResolutionSize,
		WritingDirection:         wdm,
		ExclusionSpace:           exclusionSpace,
	}
	lb := NewLineBreaker(itemsData, bla.ctx, lineSpace, fonts, LineBreakerContent)
	lb.availableWidth = lineAvailableWidth

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

	for {
		// CSS 2.1 §9.5: account for floats when computing available inline size.
		// FindAvailableInlineSize returns the space consumed by left/right floats
		// at the current block position. The exclusion space uses BFC-relative
		// coordinates, so we add bfcBlockOrigin to translate local offsets.
		floatStart, floatEnd := 0.0, 0.0
		bfcBlock := bfcBlockOrigin + blockOffset
		if exclusionSpace != nil {
			floatStart, floatEnd = exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, contentInlineSize)
		}
		lineAvailableInline := contentInlineSize - floatStart - floatEnd

		// CSS 2.1 §9.5: if floats consume all available inline space,
		// clear past them before generating the line. This avoids
		// force-fitting content into zero-width space and then clearing,
		// which produces incorrect line breaks.
		if lineAvailableInline < 1 && exclusionSpace != nil && (floatStart > 0 || floatEnd > 0) {
			clearedBlock := exclusionSpace.ClearanceOffset(css.ClearBoth, bfcBlock, wdm)
			if clearedBlock > bfcBlock {
				blockOffset = clearedBlock - bfcBlockOrigin
				bfcBlock = clearedBlock
				floatStart, floatEnd = exclusionSpace.FindAvailableInlineSize(bfcBlock, 0, contentInlineSize)
				lineAvailableInline = contentInlineSize - floatStart - floatEnd
			}
		}
		if lineAvailableInline < 1 {
			lineAvailableInline = 1
		}

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
		lineInlineOffset := floatStart
		if isFirstLine && textIndent != 0 {
			lineInlineOffset += textIndent
			lineAvailableInline -= textIndent
			isFirstLine = false
		} else {
			isFirstLine = false
		}

		// CSS 2.1 §9.5: if the line content doesn't fit beside the float,
		// shift the block offset below the float and use the full width.
		floatReducedWidth := contentInlineSize - floatStart - floatEnd
		if (floatStart > 0 || floatEnd > 0) && line.Width > floatReducedWidth && exclusionSpace != nil {
			clearedBfc := exclusionSpace.ClearanceOffset(css.ClearBoth, bfcBlockOrigin+blockOffset, wdm)
			blockOffset = clearedBfc - bfcBlockOrigin
			lineInlineOffset = 0
			lineAvailableInline = contentInlineSize
		}

		// Collect out-of-flow candidates from inline items on this line.
		// Their static position is (current block offset, inline position
		// computed from the items preceding them on the line).
		inlinePos := 0.0
		for _, r := range line.Results {
			if r.Item.Type == InlineItemOutOfFlow && r.Item.LayoutNode != nil {
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
					IsFixedPosition: r.Item.Style != nil && r.Item.Style.GetPosition() == css.PositionFixed,
				})
			}
			inlinePos += r.InlineSize
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
		lineFragment, lineHeight, lineAscent := createLineBoxEx(
			itemsData, &line, effectiveWDM, lineAvailableInline, fonts, centralBaseline, cbPhys, bla.style,
		)
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

		blockOffset += lineHeight
	}

	if firstLineAscent < 0 {
		firstLineAscent = 0
	}
	return blockOffset, exclusionSpace, firstLineAscent, lastBaselineOffset
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
	return createLineBoxEx(itemsData, line, wdm, availableInline, fonts, wdm.UsesCentralBaseline(), PhysicalSize{}, nil)
}

// createLineBoxEx positions items within a line and produces a line box fragment.
// The centralBaseline flag determines whether to use central (vertical) or
// alphabetic (horizontal/sideways) baseline alignment.
func createLineBoxEx(
	itemsData *InlineItemsData,
	line *LineInfo,
	wdm WritingDirectionMode,
	availableInline float64,
	fonts text.FontConfig,
	centralBaseline bool,
	cbPhysicalSize PhysicalSize,
	parentStyle *css.Style,
) (*PhysicalFragment, float64, float64) { // returns (fragment, lineHeight, maxAscent)
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
	{
		type spanEntry struct {
			style           *css.Style
			node            *html.Node
			borderStart     float64
			isFirstFragment bool
			isLastFragment  bool
		}
		var spanStack []spanEntry
		trackPos := alignOffset
		for _, r := range line.Results {
			switch r.Item.Type {
			case InlineItemOpenTag:
				if r.Item.Style != nil {
					spanStack = append(spanStack, spanEntry{
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
					if hasVisibleInlinePaint(span.style) {
						geom := ComputeFragmentGeometry(span.style, wdm)

						// Save original inline-end edges before suppression.
						// CSS 2.1 §9.2.1.1: inline-start border/padding appears only
						// on the first fragment; inline-end only on the last fragment.
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

						// Fragment inline extent.
						// span.borderStart = trackPos_at_openTag + margins.InlineStart.
						// At CloseTag time, trackPos = content_end (before CloseTag advance).
						//
						// The border-box left edge is span.borderStart for ALL cases:
						// - First fragment: border-box starts at margin end (border IS included in size)
						// - Non-first: no IS border/padding, so border-box start = content start = span.borderStart
						//
						// The border-box right edge:
						// - Last fragment: content_end + IE border + IE padding
						// - Non-last: content_end (no IE border/padding)
						fragStart := span.borderStart
						fragEnd := trackPos
						if span.isLastFragment {
							fragEnd += origIEBorder + origIEPadding
						}

						spanInlineSize := fragEnd - fragStart
						if spanInlineSize < 0 {
							spanInlineSize = 0
						}
						blockOverhang := geom.Border.BlockStart + geom.Padding.BlockStart
						spanBlockSize := blockOverhang + lineHeight + geom.Padding.BlockEnd + geom.Border.BlockEnd
						// Use a copy of the span style with position reset
						// to static. Inline span background fragments are
						// decorations that must paint in flow order (behind
						// text), not as positioned elements in the z-index
						// paint step which would paint ON TOP of text.
						bgStyle := span.style
						if span.style.GetPosition() != css.PositionStatic {
							bgStyle = span.style.Clone()
							bgStyle.Set("position", "static")
						}
						bgFrag := &PhysicalFragment{
							Size: ToPhysicalSize(LogicalSize{
								InlineSize: spanInlineSize,
								BlockSize:  spanBlockSize,
							}, wdm.WM),
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
						if span.style.GetPosition() == css.PositionRelative || span.style.GetPosition() == css.PositionSticky {
							offset := span.style.GetPositionOffsetResolved(cbPhysicalSize.Width, cbPhysicalSize.Height)
							bgFrag.RelativeOffset = computeRelativeOffset(offset, wdm)
						}
						lineBuilder.AddChild(bgFrag, LogicalOffset{
							InlineOffset: fragStart,
							BlockOffset:  -blockOverhang,
						})
					}
				}
			case InlineItemAtomicInline:
				// Atomic inlines advance by margin+size+margin (no default advance).
				trackPos += r.Margins.InlineStart + r.InlineSize + r.Margins.InlineEnd
				continue
			}
			trackPos += r.InlineSize
		}
	}

	// Position each item within the line.
	inlinePos := alignOffset
	for _, r := range line.Results {
		switch r.Item.Type {
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

			fontSize, bold, italic, mono, ahem := fontPropsFromStyle(r.Item.Style)
			var ascent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: in vertical modes with central
				// baseline, use fontSize / 2.
				ascent = fontSize / 2
			} else {
				ascent = text.FontAscent(fontSize, bold, italic, mono, ahem)
			}

			// Baseline-align: position top of text at (maxAscent - textAscent).
			blockPos := maxAscent - ascent

			// Use parent element as Node so the renderer can access styles.
			parentNode := r.Item.Node
			if parentNode != nil && parentNode.Parent != nil {
				parentNode = parentNode.Parent
			}

			textFrag := &PhysicalFragment{
				Size: ToPhysicalSize(LogicalSize{
					InlineSize: r.InlineSize,
					BlockSize:  fontSize,
				}, wdm.WM),
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
				if pos == css.PositionRelative || pos == css.PositionSticky {
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
							display == css.DisplayTable || display == css.DisplayInlineTable) &&
						r.Item.Style.GetOverflowX() == css.OverflowVisible && r.Item.Style.GetOverflowY() == css.OverflowVisible
					isAtomicForBaseline := isInlineBlockLike || isReplaced
					// For inline-flex, use first baseline (CSS Flexbox §4.2).
					// For inline-block/inline-table, use last baseline (CSS 2.1 §10.8.1).
					// Replaced elements don't propagate baselines from line boxes.
					atomicBaseline := float64(0)
					if !isReplaced {
						atomicBaseline = r.LayoutResult.LastBaseline
						if display == css.DisplayInlineFlex && r.LayoutResult.Baseline > 0 {
							atomicBaseline = r.LayoutResult.Baseline
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
				if r.Item.Style != nil {
					pos := r.Item.Style.GetPosition()
					if pos == css.PositionRelative || pos == css.PositionSticky {
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

		case InlineItemOpenTag, InlineItemCloseTag:
			// Margins/borders/padding contribution to InlineSize is already
			// accounted for by the line breaker.

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
	return result.Fragment, lineHeight, maxAscent
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
			strutAscent = text.FontAscentFromFont(fontSize, fontPath)
			strutDescent = fontSize - strutAscent
		}
		lineHt := parentStyle.GetLineHeight()
		halfLeading := (lineHt - (strutAscent + strutDescent)) / 2
		strutAscent += halfLeading
		strutDescent += halfLeading
		if strutAscent < 0 {
			strutAscent = 0
		}
		if strutDescent < 0 {
			strutDescent = 0
		}
		maxAscent = strutAscent
		maxDescent = strutDescent
	}
	for _, r := range line.Results {
		switch r.Item.Type {
		case InlineItemOpenTag:
			// Empty inline boxes (e.g. <span></span>) have no InlineItemText but
			// still establish a strut: their font's ascent/descent determine the
			// minimum line box height per CSS 2.1 §10.8.
			if r.Item.Style == nil {
				continue
			}
			fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
			var ascent, descent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: central baseline = fontSize / 2.
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				ascent = text.FontAscentFromFont(fontSize, fontPath)
				descent = fontSize - ascent
			}
			// CSS 2.1 §10.8.1: distribute half-leading from line-height.
			// Negative half-leading (when line-height < font-size) is valid
			// and reduces the inline box's ascent/descent contribution.
			lineHt := r.Item.Style.GetLineHeight()
			halfLeading := (lineHt - (ascent + descent)) / 2
			ascent += halfLeading
			descent += halfLeading
			if ascent < 0 {
				ascent = 0
			}
			if descent < 0 {
				descent = 0
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
			fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
			var ascent, descent float64
			if centralBaseline {
				// CSS Writing Modes 3 §4.3: central baseline = fontSize / 2.
				ascent = fontSize / 2
				descent = fontSize / 2
			} else {
				fontPath := resolveFontPath(r.Item.Style, fonts)
				ascent = text.FontAscentFromFont(fontSize, fontPath)
				descent = fontSize - ascent
			}
			// CSS 2.1 §10.8.1: distribute half-leading from line-height.
			// Negative half-leading (when line-height < font-size) is valid
			// and reduces the inline box's ascent/descent contribution.
			if r.Item.Style != nil {
				lineHt := r.Item.Style.GetLineHeight()
				halfLeading := (lineHt - (ascent + descent)) / 2
				ascent += halfLeading
				descent += halfLeading
				if ascent < 0 {
					ascent = 0
				}
				if descent < 0 {
					descent = 0
				}
			}
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
						display == css.DisplayTable || display == css.DisplayInlineTable) &&
					r.Item.Style.GetOverflowX() == css.OverflowVisible && r.Item.Style.GetOverflowY() == css.OverflowVisible
				isAtomicForBaseline := isInlineBlockLike || isReplaced
				// For inline-flex, use first baseline (CSS Flexbox §4.2).
				// For inline-block/inline-table, use last baseline (CSS 2.1 §10.8.1).
				// Replaced elements don't propagate baselines from line boxes.
				atomicBaseline := float64(0)
				if !isReplaced {
					atomicBaseline = r.LayoutResult.LastBaseline
					if display == css.DisplayInlineFlex && r.LayoutResult.Baseline > 0 {
						atomicBaseline = r.LayoutResult.Baseline
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
	if slack <= 0 {
		return 0
	}

	switch line.TextAlign {
	case "center", "-webkit-center":
		return slack / 2
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
