package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
	"strings"
)

// hasOnlyInlineChildren returns true if the block container's children are
// all inline-level (text nodes, display:inline, display:inline-block, etc.).
// When true, the container should use an inline formatting context.
//
// CSS 2.1 §9.2.1.1: block containers have either all block-level or all
// inline-level children. After anonymous block box generation by the layout
// tree builder, this is always a clean split.
func hasOnlyInlineChildren(node *LayoutInputNode) bool {
	hasContent := false
	for _, child := range node.Children() {
		if child.IsText() {
			if strings.TrimSpace(child.TextContent()) != "" {
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
			display != css.DisplayInlineFlex {
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
) (blockSizeUsed float64, updatedES *ExclusionSpace, firstLineAscent float64) {
	// Phase 1: Collect inline items from the layout subtree.
	itemsData := CollectInlines(bla.node)
	if len(itemsData.Items) == 0 {
		return 0, exclusionSpace, 0
	}

	// Phase 1b: Resolve UAX#9 bidi levels for all items.
	// This sets BidiLevel on each InlineItem (0=LTR, 1=RTL).
	// Mirrors Blink's BidiParagraph pass in InlineNode::PrepareLayout.
	ResolveBidiLevels(itemsData, wdm.Dir)

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
		childMargins := ResolveMargins(childStyle, childWDM, contentInlineSize)
		childSpace := NewConstraintSpaceBuilder(wdm, childWDM, true).
			SetOrthogonalFallbackInlineSize(
				orthogonalFallbackSize(childWDM, bla.ctx)).
			SetAvailableSize(LogicalSize{
				InlineSize: contentInlineSize,
				BlockSize:  Indefinite,
			}).
			SetPercentageResolutionSize(LogicalSize{
				InlineSize: contentInlineSize,
				BlockSize:  0,
			}).
			Build()
		childResult := layoutElement(bla.ctx, item.LayoutNode, childSpace)
		childLogical := NewLogicalFragment(wdm, childResult.Fragment)
		floatInlineSize := childMargins.InlineSum() + childLogical.InlineSize()
		floatBlockSize := childMargins.BlockSum() + childLogical.BlockSize()
		floatSide := childStyle.GetFloat()
		floatBlockOffset := exclusionSpace.FindFloatPosition(floatSide, floatInlineSize, floatBlockSize, contentInlineSize, 0)
		var floatInlineOffset float64
		if floatSide == css.FloatLeft {
			startOff, _ := exclusionSpace.FindAvailableInlineSize(floatBlockOffset, floatBlockSize)
			floatInlineOffset = startOff + childMargins.InlineStart
		} else {
			_, endOff := exclusionSpace.FindAvailableInlineSize(floatBlockOffset, floatBlockSize)
			floatInlineOffset = contentInlineSize - endOff - childMargins.InlineEnd - childLogical.InlineSize()
		}
		builder.AddChild(childResult.Fragment, LogicalOffset{
			InlineOffset: floatInlineOffset,
			BlockOffset:  floatBlockOffset + childMargins.BlockStart,
		})
		exclusionSpace = exclusionSpace.Add(Exclusion{
			InlineOffset: floatInlineOffset - childMargins.InlineStart,
			BlockOffset:  floatBlockOffset,
			InlineSize:   floatInlineSize,
			BlockSize:    floatBlockSize,
			Side:         floatSide,
		})
	}

	// Phase 2: Create line breaker.
	fonts := bla.ctx.FontConfig
	lineSpace := ConstraintSpace{
		AvailableSize:    LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
		WritingDirection: wdm,
		ExclusionSpace:   exclusionSpace,
	}
	lb := NewLineBreaker(itemsData, bla.ctx, lineSpace, fonts, LineBreakerContent)
	lb.availableWidth = contentInlineSize

	// Get text-align from the container's style.
	textAlign := "start"
	if bla.style != nil {
		if ta, ok := bla.style.Get("text-align"); ok {
			textAlign = ta
		}
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
		// at the current block position.
		floatStart, floatEnd := 0.0, 0.0
		if exclusionSpace != nil {
			floatStart, floatEnd = exclusionSpace.FindAvailableInlineSize(blockOffset, 0)
		}
		lineAvailableInline := contentInlineSize - floatStart - floatEnd
		if lineAvailableInline < 1 {
			lineAvailableInline = 1
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
			blockOffset = exclusionSpace.ClearanceOffset(css.ClearBoth, blockOffset)
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

		// Reorder line results from logical to visual order (UAX#9 L2)
		// before positioning. Mirrors Blink's BidiReorder step in
		// InlineLayoutAlgorithm::CreateLine.
		ReorderLineVisual(line.Results)

		lineFragment, lineHeight, lineAscent := createLineBox(
			itemsData, &line, wdm, lineAvailableInline,
		)
		if firstLineAscent < 0 {
			firstLineAscent = lineAscent
		}

		builder.AddChild(lineFragment, LogicalOffset{
			InlineOffset: lineInlineOffset,
			BlockOffset:  blockOffset,
		})

		blockOffset += lineHeight
	}

	if firstLineAscent < 0 {
		firstLineAscent = 0
	}
	return blockOffset, exclusionSpace, firstLineAscent
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
//
// Ported from Blink's InlineLayoutAlgorithm::CreateLine and PlaceItems.
func createLineBox(
	itemsData *InlineItemsData,
	line *LineInfo,
	wdm WritingDirectionMode,
	availableInline float64,
) (*PhysicalFragment, float64, float64) { // returns (fragment, lineHeight, maxAscent)
	// Step 1: Compute line height from font metrics of all items.
	maxAscent, maxDescent := computeLineMetrics(line, wdm)
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
	// For horizontal writing modes, use LTR direction for the line box's
	// internal coordinate system because items are already in visual LTR order
	// (after bidi reordering in ReorderLineVisual). The RTL line box builder
	// would flip all child positions via physX = outerW - inlineOffset -
	// childWidth, reversing the visual order. The line box itself is positioned
	// within the parent block using the parent's WDM.
	lineWDM := wdm
	if wdm.IsHorizontal() {
		lineWDM.Dir = DirectionLTR
	}
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
						bgFrag := &PhysicalFragment{
							Size: ToPhysicalSize(LogicalSize{
								InlineSize: spanInlineSize,
								BlockSize:  spanBlockSize,
							}, wdm.WM),
							Type:             FragmentBox,
							Style:            span.style,
							Node:             span.node,
							WritingDirection: wdm,
							BoxData: &PhysicalBoxData{
								Border:  ToPhysicalEdges(geom.Border, wdm),
								Padding: ToPhysicalEdges(geom.Padding, wdm),
							},
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

			fontSize, bold, italic, mono, ahem := fontPropsFromStyle(r.Item.Style)
			ascent := text.FontAscent(fontSize, bold, italic, mono, ahem)

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

			lineBuilder.AddChild(textFrag, LogicalOffset{
				InlineOffset: inlinePos,
				BlockOffset:  blockPos,
			})

		case InlineItemAtomicInline:
			// Apply inline-start margin before the child.
			inlinePos += r.Margins.InlineStart
			if r.LayoutResult != nil {
				childLogical := NewLogicalFragment(wdm, r.LayoutResult.Fragment)
				blockSize := childLogical.BlockSize()
				var blockPos float64
				// CSS 2.1 §10.8.1: For display:inline-block with overflow:visible,
				// align inline-block so its baseline (= font ascent from top) sits
				// at the line's maxAscent.
				if r.Item.Style != nil &&
					r.Item.Style.GetDisplay() == css.DisplayInlineBlock &&
					r.Item.Style.GetOverflow() == css.OverflowVisible {
					fontSize, bold, italic, mono, ahem := fontPropsFromStyle(r.Item.Style)
					ibAscent := text.FontAscent(fontSize, bold, italic, mono, ahem)
					// CSS 2.1 §10.8.1: maxAscent now includes margin-block-start,
					// so blockPos correctly places the border-box after the top margin.
					blockPos = maxAscent - ibAscent
				} else {
					// Default: bottom-align to baseline.
					blockPos = maxAscent - blockSize
				}
				if blockPos < 0 {
					blockPos = 0
				}
				lineBuilder.AddChild(r.LayoutResult.Fragment, LogicalOffset{
					InlineOffset: inlinePos,
					BlockOffset:  blockPos,
				})
			}
			// Advance past content + inline-end margin, skip default advance.
			inlinePos += r.InlineSize + r.Margins.InlineEnd
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

// computeLineMetrics computes the maximum ascent and descent across all
// items in a line. The line box height = maxAscent + maxDescent, and all
// text is baseline-aligned at maxAscent from the line box top.
//
// CSS 2.1 §10.8: line box height is determined by the tallest inline box.
// Even empty inline elements (open/close tag with no text) still contribute
// their font's line metrics (CSS 2.1 §9.4.2).
func computeLineMetrics(line *LineInfo, wdm WritingDirectionMode) (maxAscent, maxDescent float64) {
	for _, r := range line.Results {
		switch r.Item.Type {
		case InlineItemOpenTag:
			// Empty inline boxes (e.g. <span></span>) have no InlineItemText but
			// still establish a strut: their font's ascent/descent determine the
			// minimum line box height per CSS 2.1 §10.8.
			if r.Item.Style == nil {
				continue
			}
			fontSize, bold, italic, mono, ahem := fontPropsFromStyle(r.Item.Style)
			ascent := text.FontAscent(fontSize, bold, italic, mono, ahem)
			descent := fontSize - ascent
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
			fontSize, bold, italic, mono, ahem := fontPropsFromStyle(r.Item.Style)
			ascent := text.FontAscent(fontSize, bold, italic, mono, ahem)
			descent := fontSize - ascent
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
				// CSS 2.1 §10.8.1: For display:inline-block with overflow:visible,
				// the baseline is the baseline of the last line box. Use the
				// inline-block's font ascent as an approximation.
				if r.Item.Style != nil &&
					r.Item.Style.GetDisplay() == css.DisplayInlineBlock &&
					r.Item.Style.GetOverflow() == css.OverflowVisible {
					fontSize, bold, italic, mono, ahem := fontPropsFromStyle(r.Item.Style)
					ibAscent := text.FontAscent(fontSize, bold, italic, mono, ahem)
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
				} else {
					// Default: treat full height as above baseline (bottom-aligned).
					if blockSize > maxAscent {
						maxAscent = blockSize
					}
				}
			}
		}
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
