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
// inline-level children. Mixed content generates anonymous block boxes,
// which is not yet implemented.
func hasOnlyInlineChildren(node *html.Node, styles map[*html.Node]*css.Style) bool {
	hasContent := false
	for _, child := range node.Children {
		if child.Type == html.TextNode {
			if strings.TrimSpace(child.Text) != "" {
				hasContent = true
			}
			continue
		}
		if child.Type != html.ElementNode {
			continue
		}
		style := styles[child]
		if style == nil || style.GetDisplay() == css.DisplayNone {
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
) (blockSizeUsed float64, updatedES *ExclusionSpace) {
	// Phase 1: Collect inline items from the DOM subtree.
	itemsData := CollectInlines(bla.node, bla.ctx.ComputedStyles)
	if len(itemsData.Items) == 0 {
		return 0, exclusionSpace
	}

	// Phase 2: Create line breaker.
	fonts := text.DefaultFontConfig()
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
	}

	// Phase 3: Break into lines and create line box fragments.
	blockOffset := 0.0
	var line LineInfo

	for lb.NextLine(&line) {
		line.TextAlign = textAlign

		lineFragment, lineHeight := createLineBox(
			itemsData, &line, wdm, contentInlineSize,
		)

		builder.AddChild(lineFragment, LogicalOffset{
			InlineOffset: 0,
			BlockOffset:  blockOffset,
		})

		blockOffset += lineHeight
	}

	return blockOffset, exclusionSpace
}

// createLineBox positions items within a line and produces a line box fragment.
//
// Ported from Blink's InlineLayoutAlgorithm::CreateLine and PlaceItems.
func createLineBox(
	itemsData *InlineItemsData,
	line *LineInfo,
	wdm WritingDirectionMode,
	availableInline float64,
) (*PhysicalFragment, float64) {
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
	alignOffset := computeTextAlignOffset(line, availableInline)

	// Step 3: Build line box fragment with positioned children.
	lineBuilder := NewBoxFragmentBuilder(wdm)
	lineBuilder.SetSize(LogicalSize{
		InlineSize: availableInline,
		BlockSize:  lineHeight,
	})

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

			// Use parent element as Node so styles[node] works in fragmentToBox.
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
				Node:             parentNode,
				WritingDirection: wdm,
			}

			lineBuilder.AddChild(textFrag, LogicalOffset{
				InlineOffset: inlinePos,
				BlockOffset:  blockPos,
			})

		case InlineItemAtomicInline:
			if r.LayoutResult != nil {
				childLogical := NewLogicalFragment(wdm, r.LayoutResult.Fragment)
				// Bottom-align atomic inline to baseline (simplified).
				blockPos := maxAscent - childLogical.BlockSize()
				if blockPos < 0 {
					blockPos = 0
				}
				lineBuilder.AddChild(r.LayoutResult.Fragment, LogicalOffset{
					InlineOffset: inlinePos,
					BlockOffset:  blockPos,
				})
			}

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
	return result.Fragment, lineHeight
}

// computeLineMetrics computes the maximum ascent and descent across all
// items in a line. The line box height = maxAscent + maxDescent, and all
// text is baseline-aligned at maxAscent from the line box top.
//
// CSS 2.1 §10.8: line box height is determined by the tallest inline box.
func computeLineMetrics(line *LineInfo, wdm WritingDirectionMode) (maxAscent, maxDescent float64) {
	for _, r := range line.Results {
		switch r.Item.Type {
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
				// Simplified: treat atomic inline's full height as above baseline.
				if blockSize > maxAscent {
					maxAscent = blockSize
				}
			}
		}
	}
	return
}

// computeTextAlignOffset computes the starting inline offset for text-align.
// CSS 2.1 §16.2.
func computeTextAlignOffset(line *LineInfo, availableInline float64) float64 {
	slack := availableInline - line.Width
	if slack <= 0 {
		return 0
	}

	switch line.TextAlign {
	case "center", "-webkit-center":
		return slack / 2
	case "right", "end":
		return slack
	case "justify":
		if line.IsLastLine || line.HasForcedBreak {
			return 0 // Last line of a paragraph is not justified.
		}
		// TODO: distribute inter-word spacing for justify.
		return 0
	default: // "left", "start", ""
		return 0
	}
}
