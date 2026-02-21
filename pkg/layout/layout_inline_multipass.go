package layout

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
)

// ApplyTextTransform applies CSS text-transform to a string.
func ApplyTextTransform(s string, transform css.TextTransform) string {
	switch transform {
	case css.TextTransformUppercase:
		return strings.ToUpper(s)
	case css.TextTransformLowercase:
		return strings.ToLower(s)
	case css.TextTransformCapitalize:
		prevIsSpace := true
		runes := []rune(s)
		for i, r := range runes {
			if unicode.IsSpace(r) {
				prevIsSpace = true
			} else if prevIsSpace {
				runes[i] = unicode.ToTitle(r)
				prevIsSpace = false
			}
		}
		return string(runes)
	}
	return s
}

func NewTextFragment(text string, style *css.Style, x, y, width, height float64, node *html.Node) *Fragment {
	return &Fragment{
		Type: FragmentText,
		Text: text,
		Node: node, // CRITICAL: Must set Node for rendering
		Style: style,
		Position: Position{X: x, Y: y},
		Size:     Size{Width: width, Height: height},
	}
}

// NewBoxFragment creates a fragment from a box (for existing layout code).
func NewBoxFragment(box *Box, fragType FragmentType) *Fragment {
	return &Fragment{
		Type:     fragType,
		Node:     box.Node,
		Style:    box.Style,
		Position: Position{X: box.X, Y: box.Y},
		Size:     Size{Width: box.Width, Height: box.Height},
		Box:      box,
	}
}

// MinMaxSizes represents the intrinsic sizing information for an element.
// These are the "content-based" sizes (CSS Sizing Level 3):
// - MinContentSize: minimum size without overflow (narrowest the content can be)
// - MaxContentSize: preferred size without wrapping (widest the content wants to be)
//
// For text: min = longest word, max = full text width
// For inline boxes with inline children: min = max child min, max = sum of child max
// For inline boxes with block children: min = max child min, max = max child max

// ComputeMinMaxSizes calculates the intrinsic sizing information for a node
// WITHOUT laying it out (pure function, no side effects).
//
// This is CRITICAL for the new architecture:
// - OLD way: Call layoutNode() to get dimensions → causes side effects (pollutes le.floats)
// - NEW way: Call ComputeMinMaxSizes() → pure query, no state changes
//
// This function is used during Phase 1 (CollectInlineItems) to get float dimensions
// without actually laying them out.

// computeTextMinMax calculates min/max sizes for text content.
// Min size: width of longest word (won't wrap within words)
// Max size: width of full text (preferred width without wrapping)

// computeInlineMinMax calculates min/max sizes for inline elements.
// Inline elements with inline children: sum child widths (horizontal flow)
// Inline elements with block children: max child widths (stacking)

// computeBlockMinMax calculates min/max sizes for block elements.
// For blocks: min/max based on children (blocks stack vertically)

// computeInlineBlockMinMax calculates min/max sizes for inline-block elements.
// Inline-blocks are sized like blocks but participate in inline layout.

// computeStylesForTree computes styles for a node and all its descendants.
// This is a helper to avoid recomputing styles multiple times.

// BreakLines performs line breaking on a list of inline items.
// This is Phase 2 of the multi-pass inline layout pipeline.
//
// PURE FUNCTION - no side effects! Only decides what items go on which lines.
// The actual fragment construction happens in Phase 3.
//
// Algorithm:
// 1. Iterate through items sequentially
// 2. For each item, check if it fits on current line
// 3. Account for floats via constraint.ExclusionSpace
// 4. If doesn't fit, start new line
// 5. Return list of LineInfo (one per line)
func (le *LayoutEngine) BreakLines(
	items []*InlineItem,
	constraint *ConstraintSpace,
	startY float64,
) []*LineInfo {
	if len(items) == 0 {
		return []*LineInfo{}
	}

	// text-wrap: balance — re-run with reduced width to equalize line lengths
	if constraint.TextWrap == "balance" {
		return le.breakLinesBalanced(items, constraint, startY)
	}

	lines := []*LineInfo{}
	currentY := startY
	currentLine := &LineInfo{
		Y:          currentY,
		Items:      []*InlineItem{},
		Constraint: constraint,
		Height:     0,
	}
	textIndent := constraint.TextIndent // CSS 2.1 §16.1: text-indent on first line only
	currentX := 0.0                    // X position on current line
	hasSeenContentOnLine := false      // Track if we've seen content on this line (for whitespace stripping)
	lastTextEndedWithSpace := false // Track trailing whitespace for cross-item collapsing
	lineFloatWidth := 0.0 // Width consumed by floats on current line
	var lineFloats []*InlineItem // Floats on current line (for shifting down)

	for i := 0; i < len(items); i++ {
		item := items[i]

		// Get available width at current Y position
		// This accounts for floats via the exclusion space AND local float items
		// Note: text-indent is NOT subtracted here — it's already reflected in currentX
		// (and thus usedWidth), so subtracting it from availableWidth would double-count.
		availableWidth := constraint.AvailableInlineSize(currentY, item.Height) - lineFloatWidth

		// Check if we need to start at a different X due to floats
		leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(currentY, item.Height)

		// If this is a new line, start at the left offset (plus text-indent for first line)
		if currentX == 0 {
			currentX = leftOffset + textIndent
		}

		// Calculate how much space we've used on this line
		usedWidth := currentX - leftOffset

		switch item.Type {
		case InlineItemText:
			// Check if this item preserves whitespace (white-space: pre/pre-wrap/pre-line)
			isPreserveWS := false
			if item.Style != nil {
				ws := item.Style.GetWhiteSpace()
				isPreserveWS = ws == "pre" || ws == "pre-wrap" || ws == "pre-line"
			}

			// CSS 2.1 §16.6.1: Strip leading whitespace at start of line
			// Include \n and \r since white-space:normal collapses them to spaces
			// But NOT for white-space: pre/pre-wrap/pre-line which preserve whitespace
			if !isPreserveWS && !hasSeenContentOnLine && item.Node != nil {
				trimmedText := strings.TrimLeft(item.Text, " \t\n\r")
				if trimmedText != item.Text {
					item.Node.Text = trimmedText
					item.Text = trimmedText
					// Recalculate width for trimmed text
					if item.Style != nil {
						fontSize := item.Style.GetFontSize()
						bold := item.Style.GetFontWeight() == css.FontWeightBold
						italic := item.Style.GetFontStyle() == css.FontStyleItalic
						mono := item.Style.IsMonospaceFamily()
						ahem := item.Style.IsAhemFamily()
						newWidth, _ := text.MeasureTextWithStyle(trimmedText, fontSize, bold, italic, mono, ahem)
						ls := item.Style.GetLetterSpacing()
						if ls != 0 && len([]rune(trimmedText)) > 1 {
							newWidth += ls * float64(len([]rune(trimmedText))-1)
						}
						item.Width = newWidth
					}
					// If text was trimmed to empty, zero height too so it doesn't
					// inflate line box height (e.g., in containers with line-height:0)
					if trimmedText == "" {
						item.Height = 0
					}
				}
			}

			// CSS whitespace collapsing across element boundaries:
			// When previous text ended with space and this text starts with space,
			// collapse to a single space by trimming leading space from this item.
			// Skip for white-space: pre/pre-wrap which preserves spaces.
			if !isPreserveWS && lastTextEndedWithSpace && item.Node != nil && len(item.Text) > 0 && item.Text[0] == ' ' {
				trimmedText := strings.TrimLeft(item.Text, " ")
				if trimmedText != item.Text {
					item.Node.Text = trimmedText
					item.Text = trimmedText
					if item.Style != nil {
						fontSize := item.Style.GetFontSize()
						bold := item.Style.GetFontWeight() == css.FontWeightBold
						italic := item.Style.GetFontStyle() == css.FontStyleItalic
						mono := item.Style.IsMonospaceFamily()
						ahem := item.Style.IsAhemFamily()
						newWidth, _ := text.MeasureTextWithStyle(trimmedText, fontSize, bold, italic, mono, ahem)
						ls := item.Style.GetLetterSpacing()
						if ls != 0 && len([]rune(trimmedText)) > 1 {
							newWidth += ls * float64(len([]rune(trimmedText))-1)
						}
						item.Width = newWidth
					}
					// If text was trimmed to empty, zero height too
					if trimmedText == "" {
						item.Height = 0
					}
				}
			}

			// Only mark content seen if text is non-empty after trimming
			if item.Text != "" {
				hasSeenContentOnLine = true
			}
			// Track trailing whitespace for cross-item collapsing
			lastTextEndedWithSpace = len(item.Text) > 0 && item.Text[len(item.Text)-1] == ' '

			// Text item - may need to wrap
			textWidth := item.Width

			// CSS 2.1 §10.8.1: line-height determines the inline box height
			// that contributes to line box height calculation. Font metrics height
			// (item.Height) is used for rendering/alignment, not line box sizing.
			textLineHeight := item.Height
			if item.Style != nil {
				textLineHeight = item.Style.GetLineHeight()
			}

			// CSS 2.1 §16.6.1: Whitespace at the end of a line "hangs" — it does not
			// contribute to the line width for wrapping purposes. Whitespace-only text
			// should always be added to the current line, never cause a line break.
			isHangingWhitespace := strings.TrimSpace(item.Text) == ""

			if usedWidth+textWidth <= availableWidth || constraint.NoWrap || isHangingWhitespace {
				// Fits on current line, white-space: nowrap, or hanging whitespace
				currentLine.Items = append(currentLine.Items, item)
				currentX += textWidth

				// Update line height using line-height
				if textLineHeight > currentLine.Height {
					currentLine.Height = textLineHeight
				}
			} else if !constraint.NoWrap && tryBreakAtSoftHyphen(
				currentLine, item, &lines, &currentY, &currentX, &hasSeenContentOnLine,
				&lastTextEndedWithSpace, &lineFloatWidth, &lineFloats, &textIndent, constraint,
			) {
				// Broke at a soft hyphen — line was committed, continue with next item
			} else if textWidth <= availableWidth {
				// Doesn't fit, but would fit on new line
				// Finish current line
				if len(currentLine.Items) > 0 {
					lines = append(lines, currentLine)
					currentY += currentLine.Height
				}

				// Start new line - reset whitespace and float tracking
				hasSeenContentOnLine = true // This item is the first content
				textIndent = 0             // text-indent only applies to first line
				lineFloatWidth = 0
				lineFloats = nil
				leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(currentY, item.Height)
				currentLine = &LineInfo{
					Y:          currentY,
					Items:      []*InlineItem{item},
					Constraint: constraint,
					Height:     textLineHeight,
				}
				currentX = leftOffset + textWidth
			} else {
				// Text is wider than available width
				// CSS 2.1 §9.5: "If a shortened line box is too small to contain any
				// content, then the line box is shifted downward until either some
				// content fits or there are no more floats present."

				// Check for floats narrowing the line (both from exclusion space and local floats)
				shifted := false
				if lineFloatWidth > 0 || !constraint.ExclusionSpace.IsEmpty() {
					// Find the nearest float bottom to shift past
					// First check local floats (on current line, not yet in exclusion space)
					nextY := -1.0
					for _, f := range lineFloats {
						floatBottom := currentY + f.Height
						if nextY < 0 || floatBottom < nextY {
							nextY = floatBottom
						}
					}
					// Also check exclusion space floats
					esNextY := constraint.ExclusionSpace.NextBandBelowY(currentY, textLineHeight)
					if esNextY > 0 && (nextY < 0 || esNextY < nextY) {
						nextY = esNextY
					}
					if nextY > currentY {
						// Shift down past the float - keep float items, retry text
						// Emit current line with just the float items
						if len(currentLine.Items) > 0 {
							lines = append(lines, currentLine)
						}
						currentY = nextY
						currentLine = &LineInfo{
							Y:          currentY,
							Items:      []*InlineItem{},
							Constraint: constraint,
							Height:     0,
						}
						currentX = 0
						lineFloatWidth = 0
						lineFloats = nil
						hasSeenContentOnLine = false
						lastTextEndedWithSpace = false
						i-- // Retry this item at the new Y position
						shifted = true
					}
				}
				if !shifted {
					// Check for character-level breaking
					breakAll := constraint.WordBreak == "break-all"
					breakWord := constraint.OverflowWrap == "break-word" || constraint.OverflowWrap == "anywhere"
					charBroke := false
					if (breakAll || breakWord) && item.Style != nil {
						remainingWidth := availableWidth - usedWidth
						if remainingWidth <= 0 {
							remainingWidth = availableWidth
						}
						cFontSize := item.Style.GetFontSize()
						cBold := item.Style.GetFontWeight() == css.FontWeightBold
						cItalic := item.Style.GetFontStyle() == css.FontStyleItalic
						cMono := item.Style.IsMonospaceFamily()
						cAhem := item.Style.IsAhemFamily()
						shouldBreak := breakAll
						if !shouldBreak && breakWord {
							ww, _ := text.MeasureTextWithStyle(item.Text, cFontSize, cBold, cItalic, cMono, cAhem)
							shouldBreak = ww > availableWidth
						}
						if shouldBreak {
							pfx, rem := text.BreakTextAtCharacterBoundary(item.Text, cFontSize, cBold, cItalic, cMono, cAhem, remainingWidth)
							if pfx != "" {
								pfxW, _ := text.MeasureTextWithStyle(pfx, cFontSize, cBold, cItalic, cMono, cAhem)
								currentLine.Items = append(currentLine.Items, &InlineItem{Type: InlineItemText, Text: pfx, Style: item.Style, Node: item.Node, Width: pfxW, Height: item.Height})
								if textLineHeight > currentLine.Height {
									currentLine.Height = textLineHeight
								}
								if rem != "" {
									remW, remH := text.MeasureTextWithStyle(rem, cFontSize, cBold, cItalic, cMono, cAhem)
									remItem := &InlineItem{Type: InlineItemText, Text: rem, Style: item.Style, Node: item.Node, Width: remW, Height: remH}
									newItems := make([]*InlineItem, 0, len(items)+1)
									newItems = append(newItems, items[:i+1]...)
									newItems = append(newItems, remItem)
									if i+1 < len(items) {
										newItems = append(newItems, items[i+1:]...)
									}
									items = newItems
								}
								lines = append(lines, currentLine)
								currentY += currentLine.Height
								textIndent = 0
								lineFloatWidth = 0
								lineFloats = nil
								currentLine = &LineInfo{Y: currentY, Items: []*InlineItem{}, Constraint: constraint, Height: 0}
								currentX = 0
								hasSeenContentOnLine = false
								lastTextEndedWithSpace = false
								charBroke = true
							}
						}
					}
					if !charBroke {
						// CSS 2.1 §16.6: For word-break: normal, text can break at soft wrap
						// opportunities (spaces). If the text item contains spaces and is wider
						// than available width, try to split at a word boundary.
						wordBroke := false
						if !constraint.NoWrap && item.Style != nil && strings.Contains(item.Text, " ") {
							cFontSize := item.Style.GetFontSize()
							cBold := item.Style.GetFontWeight() == css.FontWeightBold
							cItalic := item.Style.GetFontStyle() == css.FontStyleItalic
							cMono := item.Style.IsMonospaceFamily()
							cAhem := item.Style.IsAhemFamily()
							remainingWidth := availableWidth - usedWidth
							if remainingWidth <= 0 {
								remainingWidth = availableWidth
							}
							pfxText, remText := breakTextAtWordBoundary(item.Text, cFontSize, cBold, cItalic, cMono, cAhem, remainingWidth)
							if pfxText != "" && remText != "" {
								pfxW, _ := text.MeasureTextWithStyle(pfxText, cFontSize, cBold, cItalic, cMono, cAhem)
								currentLine.Items = append(currentLine.Items, &InlineItem{Type: InlineItemText, Text: pfxText, Style: item.Style, Node: item.Node, Width: pfxW, Height: item.Height})
								if textLineHeight > currentLine.Height {
									currentLine.Height = textLineHeight
								}
								remW, remH := text.MeasureTextWithStyle(remText, cFontSize, cBold, cItalic, cMono, cAhem)
								remItem := &InlineItem{Type: InlineItemText, Text: remText, Style: item.Style, Node: item.Node, Width: remW, Height: remH}
								newItems := make([]*InlineItem, 0, len(items)+1)
								newItems = append(newItems, items[:i+1]...)
								newItems = append(newItems, remItem)
								if i+1 < len(items) {
									newItems = append(newItems, items[i+1:]...)
								}
								items = newItems
								lines = append(lines, currentLine)
								currentY += currentLine.Height
								textIndent = 0
								lineFloatWidth = 0
								lineFloats = nil
								currentLine = &LineInfo{Y: currentY, Items: []*InlineItem{}, Constraint: constraint, Height: 0}
								currentX = 0
								hasSeenContentOnLine = false
								lastTextEndedWithSpace = false
								wordBroke = true
							} else if pfxText == "" {
								// Nothing fits on current line - start a new line and retry
								if len(currentLine.Items) > 0 {
									lines = append(lines, currentLine)
									currentY += currentLine.Height
								}
								lineFloatWidth = 0
								lineFloats = nil
								currentLine = &LineInfo{Y: currentY, Items: []*InlineItem{}, Constraint: constraint, Height: 0}
								currentX = 0
								hasSeenContentOnLine = false
								lastTextEndedWithSpace = false
								i-- // Retry this item on the new line
								wordBroke = true
							}
						}
						if !wordBroke {
							// No floats to clear - force onto current line (true overflow)
							currentLine.Items = append(currentLine.Items, item)
							currentX += textWidth
							if textLineHeight > currentLine.Height {
								currentLine.Height = textLineHeight
							}
						}
					}
				}
			}

		case InlineItemFloat:
			// Float item - reduces available width for subsequent content on this line
			// Phase 3 will position it and update the constraint
			currentLine.Items = append(currentLine.Items, item)
			lineFloatWidth += item.Width
			lineFloats = append(lineFloats, item)
			// CSS 2.1: floats are out-of-flow. Do NOT reset lastTextEndedWithSpace —
			// a space before the float and a space after the float form an adjacent
			// whitespace sequence that should collapse to one space. (float-nowrap-3)
			hasSeenContentOnLine = true

			// Update line height
			if item.Height > currentLine.Height {
				currentLine.Height = item.Height
			}

		case InlineItemAtomic:
			// Atomic item (inline-block, replaced element) - cannot break
			// Include horizontal margins in width calculation (CSS 2.1 §10.3.9)
			atomicMarginLeft := 0.0
			atomicMarginRight := 0.0
			if item.Style != nil {
				margin := item.Style.GetMargin()
				atomicMarginLeft = margin.Left
				atomicMarginRight = margin.Right
			}
			atomicWidth := atomicMarginLeft + item.Width + atomicMarginRight

			if usedWidth+atomicWidth <= availableWidth || constraint.NoWrap {
				// Fits on current line, or white-space: nowrap forces it on same line
				currentLine.Items = append(currentLine.Items, item)
				currentX += atomicWidth

				// Update line height
				if item.Height > currentLine.Height {
					currentLine.Height = item.Height
				}
			} else {
				// Doesn't fit - start new line
				if len(currentLine.Items) > 0 {
					lines = append(lines, currentLine)
					currentY += currentLine.Height
				}

				// Start new line with this item
				textIndent = 0 // text-indent only applies to first line
				lineFloatWidth = 0
				lineFloats = nil
				leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(currentY, item.Height)
				currentLine = &LineInfo{
					Y:          currentY,
					Items:      []*InlineItem{item},
					Constraint: constraint,
					Height:     item.Height,
				}
				currentX = leftOffset + atomicWidth
			}
			lastTextEndedWithSpace = false // Real content interrupts whitespace sequence
			hasSeenContentOnLine = true

		case InlineItemSoftHyphen:
			// Soft hyphen (U+00AD) — zero-width break opportunity marker.
			// Invisible on the current line; only becomes visible (as '-') when a line break
			// occurs at this position. Add as a zero-width marker to track break opportunities.
			currentLine.Items = append(currentLine.Items, item)
			// Does not affect currentX, line height, or whitespace tracking.

		case InlineItemOpenTag, InlineItemCloseTag:
			// Tag markers - add to current line but don't affect layout
			currentLine.Items = append(currentLine.Items, item)

		case InlineItemControl:
			// Control item (br, etc.) - forces line break
			currentLine.Items = append(currentLine.Items, item)

			// Finish current line
			if len(currentLine.Items) > 0 {
				lines = append(lines, currentLine)
				currentY += currentLine.Height
			}

			// Start new line - reset whitespace and float tracking
			textIndent = 0 // text-indent only applies to first line
			currentLine = &LineInfo{
				Y:          currentY,
				Items:      []*InlineItem{},
				Constraint: constraint,
				Height:     0,
			}
			currentX = 0
			hasSeenContentOnLine = false
			lastTextEndedWithSpace = false
			lineFloatWidth = 0
			lineFloats = nil

		case InlineItemBlockChild:
			// Block child - MUST be on its own line
			// Finish current line if it has any content
			if len(currentLine.Items) > 0 {
				lines = append(lines, currentLine)
				currentY += currentLine.Height
			}

			// Create a line containing ONLY the block child
			// Height will be determined during recursive layout in Phase 3
			currentLine = &LineInfo{
				Y:          currentY,
				Items:      []*InlineItem{item},
				Constraint: constraint,
				Height:     0, // Will be set after recursive layout
			}
			lines = append(lines, currentLine)

			// Y advance will happen in Phase 3 after we know the block's height
			// For now, just reset for next line
			textIndent = 0 // text-indent only applies to first line
			currentLine = &LineInfo{
				Y:          currentY, // Will be updated in Phase 3
				Items:      []*InlineItem{},
				Constraint: constraint,
				Height:     0,
			}
			currentX = 0
			hasSeenContentOnLine = false
			lastTextEndedWithSpace = false
			lineFloatWidth = 0
			lineFloats = nil

		default:
			// Unknown item type - treat as atomic
			currentLine.Items = append(currentLine.Items, item)
			currentX += item.Width

			if item.Height > currentLine.Height {
				currentLine.Height = item.Height
			}
		}
	}

	// Add final line if it has items
	if len(currentLine.Items) > 0 {
		lines = append(lines, currentLine)
	}

	// CSS 2.1 §16.6.1: Strip trailing whitespace at end of each line
	// But NOT for white-space: pre/pre-wrap which preserves whitespace
	for _, line := range lines {
		// Find last text item on the line and strip trailing whitespace
		for j := len(line.Items) - 1; j >= 0; j-- {
			item := line.Items[j]
			if item.Type == InlineItemText {
				// Skip empty text items (width=0, no visible content) when scanning
				// backward — they shouldn't prevent trailing strip on the real text.
				// These can appear after block-in-inline splits where whitespace
				// normalization produced empty text nodes.
				if item.Text == "" {
					continue
				}
				// Don't strip trailing whitespace for preformatted content
				if item.Style != nil {
					ws := item.Style.GetWhiteSpace()
					if ws == "pre" || ws == "pre-wrap" {
						break
					}
				}
				trimmedText := strings.TrimRight(item.Text, " \t\n\r")
				if trimmedText != item.Text {
					if item.Node != nil {
						item.Node.Text = trimmedText
					}
					item.Text = trimmedText
					// Recalculate width for trimmed text
					if item.Style != nil {
						fontSize := item.Style.GetFontSize()
						bold := item.Style.GetFontWeight() == css.FontWeightBold
						italic := item.Style.GetFontStyle() == css.FontStyleItalic
						mono := item.Style.IsMonospaceFamily()
						ahem := item.Style.IsAhemFamily()
						newWidth, _ := text.MeasureTextWithStyle(trimmedText, fontSize, bold, italic, mono, ahem)
						ls := item.Style.GetLetterSpacing()
						if ls != 0 && len([]rune(trimmedText)) > 1 {
							newWidth += ls * float64(len([]rune(trimmedText))-1)
						}
						item.Width = newWidth
					}
					// If text was trimmed to empty, zero height too so it doesn't
					// inflate container auto-height (e.g., in containers with line-height:0)
					if trimmedText == "" {
						item.Height = 0
					}
				}
				break // Only strip last text item
			}
			if item.Type == InlineItemOpenTag || item.Type == InlineItemCloseTag {
				continue // Skip tag markers
			}
			break // Stop at non-text, non-tag items
		}
	}

	// -webkit-line-clamp: truncate to N lines
	if constraint.LineClampN > 0 && len(lines) > constraint.LineClampN {
		lines = lines[:constraint.LineClampN]
	}

	// CSS text-overflow: ellipsis — truncate overflowing nowrap lines
	if constraint.NoWrap && constraint.TextOverflow == css.TextOverflowEllipsis {
		for _, line := range lines {
			le.applyTextOverflowEllipsis(line, constraint.AvailableSize.Width)
		}
	}

	// CSS 2.1 §9.4.2: Line boxes that contain no text, no preserved white space,
	// no inline elements with non-zero margins/padding/borders, and no other in-flow
	// content must be treated as zero-height (not existing).
	// Filter out whitespace-only lines.
	for _, line := range lines {
		if isWhitespaceOnlyLine(line) {
			line.Height = 0
		}
	}

	return lines
}

// breakLinesBalanced implements text-wrap: balance by computing the balanced target
// line width and re-running normal line breaking with that reduced width.
// Algorithm:
//  1. Run normal breaking to count lines (N)
//  2. Sum total text width
//  3. Target width = totalTextWidth / N
//  4. Re-run with min(availableWidth, targetWidth) to balance lines
func (le *LayoutEngine) breakLinesBalanced(
	items []*InlineItem,
	constraint *ConstraintSpace,
	startY float64,
) []*LineInfo {
	// Step 1: Run normal breaking (without balance) to get line count
	normalConstraint := *constraint
	normalConstraint.TextWrap = "normal" // prevent infinite recursion
	normalLines := le.BreakLines(items, &normalConstraint, startY)
	nLines := len(normalLines)
	if nLines <= 1 {
		// Single line or empty: nothing to balance
		return normalLines
	}

	// Step 2: Sum total text + atomic inline widths
	totalWidth := 0.0
	for _, item := range items {
		switch item.Type {
		case InlineItemText:
			totalWidth += item.Width
		case InlineItemAtomic:
			totalWidth += item.Width
		}
	}

	// Step 3: Compute target width
	targetWidth := totalWidth / float64(nLines)

	// Ensure target width is at least enough for the widest single word/item
	// (otherwise we might create more lines than needed)
	maxItemWidth := 0.0
	for _, item := range items {
		if item.Width > maxItemWidth {
			maxItemWidth = item.Width
		}
	}
	if targetWidth < maxItemWidth {
		targetWidth = maxItemWidth
	}

	// Step 4: Re-run with reduced available width
	if targetWidth >= constraint.AvailableSize.Width {
		// No reduction needed; normal breaking already balanced
		return normalLines
	}

	balancedConstraint := *constraint
	balancedConstraint.TextWrap = "normal" // prevent infinite recursion
	balancedConstraint.AvailableSize.Width = targetWidth
	return le.BreakLines(items, &balancedConstraint, startY)
}

// breakTextAtWordBoundary splits text at the last space such that the prefix fits
// within availableWidth. Returns (prefix, remainder) where prefix is the text that
// fits and remainder is the rest (starting after the space).
// If nothing fits (even a single word is too wide), returns ("", fullText).
// If everything fits, returns (fullText, "").
func breakTextAtWordBoundary(txt string, fontSize float64, bold, italic, mono, ahem bool, availableWidth float64) (string, string) {
	// Find all space positions
	runes := []rune(txt)
	bestEnd := -1 // Last rune index (exclusive) of best fitting prefix
	for i, r := range runes {
		if r == ' ' {
			// Try prefix up to (but not including) this space
			prefix := string(runes[:i])
			if prefix == "" {
				continue
			}
			w, _ := text.MeasureTextWithStyle(prefix, fontSize, bold, italic, mono, ahem)
			if w <= availableWidth {
				bestEnd = i
			} else {
				break // Words only get longer
			}
		}
	}
	if bestEnd < 0 {
		// No word boundary found where prefix fits - check if single word fits
		// (for the case where there's no space at all)
		w, _ := text.MeasureTextWithStyle(txt, fontSize, bold, italic, mono, ahem)
		if w <= availableWidth {
			return txt, ""
		}
		return "", txt
	}
	prefix := strings.TrimRight(string(runes[:bestEnd]), " ")
	remainder := strings.TrimLeft(string(runes[bestEnd:]), " ")
	return prefix, remainder
}

// splitTextIntoWordAndSpaceParts splits text into alternating word and space parts.
// For example: "XX XX XX" → ["XX", " ", "XX", " ", "XX"]
// This enables word-level wrapping in BreakLines and inter-word spacing for text-align:justify.
func splitTextIntoWordAndSpaceParts(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	i := 0
	for i < len(s) {
		if s[i] == ' ' {
			j := i
			for j < len(s) && s[j] == ' ' {
				j++
			}
			parts = append(parts, s[i:j])
			i = j
		} else {
			j := i
			for j < len(s) && s[j] != ' ' {
				j++
			}
			parts = append(parts, s[i:j])
			i = j
		}
	}
	return parts
}

// tryBreakAtSoftHyphen looks backwards in currentLine.Items for the last InlineItemSoftHyphen.
// If found, it breaks the line at that position: items before the soft hyphen are committed
// to a new line with a visible '-' appended to the last text item, then the items after the
// soft hyphen (plus the overflow item) become the start of the next line.
// Returns true if a soft hyphen break was performed (line was committed and next line started).
func tryBreakAtSoftHyphen(
	currentLine *LineInfo,
	overflowItem *InlineItem,
	lines *[]*LineInfo,
	currentY *float64,
	currentX *float64,
	hasSeenContentOnLine *bool,
	lastTextEndedWithSpace *bool,
	lineFloatWidth *float64,
	lineFloats *[]*InlineItem,
	textIndent *float64,
	constraint *ConstraintSpace,
) bool {
	// Find last soft hyphen in current line items
	shIdx := -1
	for j := len(currentLine.Items) - 1; j >= 0; j-- {
		if currentLine.Items[j].Type == InlineItemSoftHyphen {
			shIdx = j
			break
		}
	}
	if shIdx < 0 {
		return false // No soft hyphen opportunity found
	}

	// Measure the hyphen character '-' in the style of the item before the soft hyphen.
	hyphenChar := "-"
	hyphenW := 0.0
	hyphenLetterSpacing := 0.0
	for j := shIdx - 1; j >= 0; j-- {
		if currentLine.Items[j].Type == InlineItemText && currentLine.Items[j].Style != nil {
			hyphenStyle := currentLine.Items[j].Style
			hFontSize := hyphenStyle.GetFontSize()
			hBold := hyphenStyle.GetFontWeight() == css.FontWeightBold
			hItalic := hyphenStyle.GetFontStyle() == css.FontStyleItalic
			hMono := hyphenStyle.IsMonospaceFamily()
			hAhem := hyphenStyle.IsAhemFamily()
			hyphenW, _ = text.MeasureTextWithStyle(hyphenChar, hFontSize, hBold, hItalic, hMono, hAhem)
			hyphenLetterSpacing = hyphenStyle.GetLetterSpacing()
			break
		}
	}

	// Build the committed line: items before the soft hyphen, with '-' appended to last text item.
	committedItems := make([]*InlineItem, 0, shIdx)
	for j := 0; j < shIdx; j++ {
		committedItems = append(committedItems, currentLine.Items[j])
	}
	// Append hyphen to the last text item before the soft hyphen position.
	// We create a new item that shows the text + '-' visually.
	for j := len(committedItems) - 1; j >= 0; j-- {
		if committedItems[j].Type == InlineItemText {
			orig := committedItems[j]
			hyphenatedText := orig.Text + hyphenChar
			hW := orig.Width + hyphenW
			if hyphenLetterSpacing != 0 {
				hW += hyphenLetterSpacing // one additional glyph spacing
			}
			hyphenatedNode := &html.Node{
				Type:   html.TextNode,
				Text:   hyphenatedText,
				Parent: orig.Node.Parent,
			}
			committedItems[j] = &InlineItem{
				Type:        InlineItemText,
				Node:        hyphenatedNode,
				Text:        hyphenatedText,
				StartOffset: orig.StartOffset,
				EndOffset:   orig.EndOffset,
				Style:       orig.Style,
				Width:       hW,
				Height:      orig.Height,
			}
			break
		}
	}

	// Compute height of committed line
	committedHeight := currentLine.Height
	if committedHeight == 0 {
		for _, it := range committedItems {
			if it.Height > committedHeight {
				committedHeight = it.Height
			}
		}
	}

	// Commit the line ending at the soft hyphen.
	committedLine := &LineInfo{
		Y:          currentLine.Y,
		Items:      committedItems,
		Constraint: currentLine.Constraint,
		Height:     committedHeight,
	}
	*lines = append(*lines, committedLine)
	*currentY += committedHeight

	// Items after the soft hyphen (but before the overflow item) go to the next line.
	// These are items that were already placed past the soft hyphen on the current line.
	carryItems := currentLine.Items[shIdx+1:]

	// Start the new line with any carry items plus the overflow item.
	newItems := make([]*InlineItem, 0, len(carryItems)+1)
	newItems = append(newItems, carryItems...)
	newItems = append(newItems, overflowItem)

	newHeight := overflowItem.Height
	newWidth := overflowItem.Width
	for _, it := range carryItems {
		if it.Height > newHeight {
			newHeight = it.Height
		}
		newWidth += it.Width
	}

	leftOffset, _ := constraint.ExclusionSpace.AvailableInlineSize(*currentY, newHeight)
	*currentLine = LineInfo{
		Y:          *currentY,
		Items:      newItems,
		Constraint: constraint,
		Height:     newHeight,
	}
	*currentX = leftOffset + newWidth
	*textIndent = 0
	*lineFloatWidth = 0
	*lineFloats = nil
	*hasSeenContentOnLine = true
	*lastTextEndedWithSpace = false
	return true
}

// applyTextOverflowEllipsis truncates a line's text items so they fit within
// availableWidth, replacing the truncation point with "...".
func (le *LayoutEngine) applyTextOverflowEllipsis(line *LineInfo, availableWidth float64) {
	// Calculate total content width
	totalWidth := 0.0
	for _, item := range line.Items {
		if item.Type == InlineItemText || item.Type == InlineItemAtomic {
			totalWidth += item.Width
		}
	}
	if totalWidth <= availableWidth {
		return // Fits, no truncation needed
	}

	// Measure ellipsis "..." in the style of the last text item
	ellipsis := "\u2026" // Unicode ellipsis character
	ellipsisWidth := 0.0
	var ellipsisStyle *css.Style
	for _, item := range line.Items {
		if item.Type == InlineItemText && item.Style != nil {
			ellipsisStyle = item.Style
		}
	}
	if ellipsisStyle != nil {
		fontSize := ellipsisStyle.GetFontSize()
		bold := ellipsisStyle.GetFontWeight() == css.FontWeightBold
		italic := ellipsisStyle.GetFontStyle() == css.FontStyleItalic
		mono := ellipsisStyle.IsMonospaceFamily()
		ahem := ellipsisStyle.IsAhemFamily()
		ellipsisWidth, _ = text.MeasureTextWithStyle(ellipsis, fontSize, bold, italic, mono, ahem)
	}

	// Walk items, find where to truncate
	usedWidth := 0.0
	truncateAt := availableWidth - ellipsisWidth
	for idx, item := range line.Items {
		if item.Type == InlineItemText {
			if usedWidth+item.Width > truncateAt {
				// This text item overflows — truncate it character by character
				runes := []rune(item.Text)
				fontSize := item.Style.GetFontSize()
				bold := item.Style.GetFontWeight() == css.FontWeightBold
				italic := item.Style.GetFontStyle() == css.FontStyleItalic
				mono := item.Style.IsMonospaceFamily()
				ahem := item.Style.IsAhemFamily()

				bestLen := 0
				for i := 1; i <= len(runes); i++ {
					prefix := string(runes[:i])
					w, _ := text.MeasureTextWithStyle(prefix, fontSize, bold, italic, mono, ahem)
					if usedWidth+w > truncateAt {
						break
					}
					bestLen = i
				}

				truncated := string(runes[:bestLen]) + ellipsis
				w, _ := text.MeasureTextWithStyle(truncated, fontSize, bold, italic, mono, ahem)
				item.Text = truncated
				if item.Node != nil {
					item.Node.Text = truncated
				}
				item.Width = w

				// Remove all subsequent items (keep only tags after truncation point)
				newItems := make([]*InlineItem, 0, idx+1)
				newItems = append(newItems, line.Items[:idx+1]...)
				// Preserve close tags that come after
				for _, remaining := range line.Items[idx+1:] {
					if remaining.Type == InlineItemCloseTag || remaining.Type == InlineItemOpenTag {
						newItems = append(newItems, remaining)
					}
				}
				line.Items = newItems
				return
			}
			usedWidth += item.Width
		} else if item.Type == InlineItemAtomic {
			if usedWidth+item.Width > truncateAt {
				// Atomic item overflows — remove it and everything after, add ellipsis
				// Insert an ellipsis text item before this position
				if ellipsisStyle != nil {
					ellipsisItem := &InlineItem{
						Type:  InlineItemText,
						Text:  ellipsis,
						Style: ellipsisStyle,
						Width: ellipsisWidth,
					}
					newItems := make([]*InlineItem, 0, idx+1)
					newItems = append(newItems, line.Items[:idx]...)
					newItems = append(newItems, ellipsisItem)
					line.Items = newItems
				}
				return
			}
			usedWidth += item.Width
		}
	}
}

// isWhitespaceOnlyLine checks if a line contains only whitespace text items
// and tag items (no floats, no atomics, no block children, no non-whitespace text).
func isWhitespaceOnlyLine(line *LineInfo) bool {
	for _, item := range line.Items {
		switch item.Type {
		case InlineItemText:
			if strings.TrimSpace(item.Text) != "" {
				return false // Has non-whitespace text
			}
			// For white-space: pre, even empty/whitespace lines are preserved
			if item.Style != nil {
				ws := item.Style.GetWhiteSpace()
				if ws == "pre" || ws == "pre-wrap" || ws == "pre-line" {
					return false
				}
			}
		case InlineItemFloat, InlineItemAtomic, InlineItemBlockChild:
			return false // Has non-text content
		case InlineItemOpenTag, InlineItemCloseTag, InlineItemControl, InlineItemSoftHyphen:
			// Tags, control items, and soft hyphens don't count as content
			continue
		default:
			return false
		}
	}
	return true
}

// LayoutInlineContent orchestrates the three-phase inline layout with retry support.
// This is the CLEAN implementation following Blink LayoutNG principles.
//
// Three phases:
// 1. CollectInlineItems - flatten DOM to sequential items (PURE - no side effects)
// 2. BreakLines - decide line breaks (PURE - no side effects)
// 3. ConstructFragments - create positioned fragments (HAS side effects)
//
// Retry logic:
// - After Phase 3, check if floats were added that affect line breaking
// - If yes, retry with updated constraint space
// - Max 3 iterations to prevent infinite loops
//
// NOTE: Phase 3 (ConstructFragments) is not yet implemented, so this is a
// simplified version that demonstrates the retry pattern.
func (le *LayoutEngine) LayoutInlineContent(
	children []*html.Node,
	constraint *ConstraintSpace,
	startY float64,
	containerStyle *css.Style,
	overrideStyles map[*html.Node]*css.Style,
) []*Fragment {
	const maxRetries = 3

	// Three-phase pipeline with retry support
	// This is the COMPLETE implementation following Blink LayoutNG principles!

	// CRITICAL: Always start from the ORIGINAL constraint
	// Don't carry over float exclusions from previous retries
	// Phase 3 will rebuild exclusions from scratch each time
	originalConstraint := constraint
	var finalFragments []*Fragment

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Phase 1: Collect inline items (PURE - no side effects!)
		// Use original constraint - don't accumulate floats across retries
		items := le.collectInlineItemsClean(children, originalConstraint, containerStyle, overrideStyles)

		// Phase 2: Break lines (PURE - no side effects!)
		// Use original constraint - floats will be added in Phase 3
		lines := le.BreakLines(items, originalConstraint, startY)

		// Phase 3: Construct fragments (HAS side effects - creates fragments)
		// Start from original constraint and build up float exclusions
		fragments, finalConstraint := le.ConstructFragments(lines, originalConstraint)

		// Check if we need to retry
		if !constraintsChanged(originalConstraint, finalConstraint, lines) {
			// Success! Constraints didn't change, we're done
			finalFragments = fragments
			break
		}

		// Constraints changed - this means Phase 3 added floats
		// For now, we don't actually retry - we just return what we have
		// The retry logic isn't needed for simple float cases
		// TODO: Implement proper retry when float changes affect line breaking
		finalFragments = fragments
		break
	}

	return finalFragments
}

// collectInlineItemsClean is a clean version of CollectInlineItems that works
// with the new architecture (ConstraintSpace instead of InlineLayoutState).
//
// This is a simplified placeholder until we fully refactor CollectInlineItems.
// For now, it creates a minimal InlineLayoutState and delegates to the existing
// CollectInlineItems function.
func (le *LayoutEngine) collectInlineItemsClean(
	children []*html.Node,
	constraint *ConstraintSpace,
	containerStyle *css.Style,
	overrideStyles map[*html.Node]*css.Style,
) []*InlineItem {
	// Create a temporary state for collecting items
	// This is a bridge between old and new architectures
	if containerStyle == nil {
		containerStyle = css.NewStyle()
	}
	state := &InlineLayoutState{
		Items:          []*InlineItem{},
		AvailableWidth: constraint.AvailableSize.Width,
		ContainerStyle: containerStyle,
	}

	// Compute styles for all children with inheritance from container
	computedStyles := make(map[*html.Node]*css.Style)

	// Pre-populate with override styles (e.g., for synthetic pseudo-element nodes)
	if overrideStyles != nil {
		for node, style := range overrideStyles {
			computedStyles[node] = style
		}
	}

	for _, child := range children {
		// Skip style computation if already in overrideStyles (synthetic nodes)
		if _, hasOverride := computedStyles[child]; hasOverride {
			continue
		}
		// Skip style computation for nodes whose styles live in le.syntheticStyles
		// (anonymous blocks and clones created by block-in-inline normalization)
		if synthStyle, ok := le.syntheticStyles[child]; ok {
			computedStyles[child] = synthStyle
			continue
		}
		childStyle := css.ComputeStyle(
			child,
			le.stylesheets,
			le.viewport.width,
			le.viewport.height,
		)
		// Inherit properties from container style if not set
		if containerStyle != nil {
			// Font properties should inherit
			if _, ok := childStyle.Get("font-size"); !ok {
				if fontSize, ok := containerStyle.Get("font-size"); ok {
					childStyle.Set("font-size", fontSize)
				}
			}
			if _, ok := childStyle.Get("font-family"); !ok {
				if fontFamily, ok := containerStyle.Get("font-family"); ok {
					childStyle.Set("font-family", fontFamily)
				}
			}
			if _, ok := childStyle.Get("line-height"); !ok {
				if lineHeight, ok := containerStyle.Get("line-height"); ok {
					childStyle.Set("line-height", lineHeight)
				}
			}
			if _, ok := childStyle.Get("color"); !ok {
				if color, ok := containerStyle.Get("color"); ok {
					childStyle.Set("color", color)
				}
			}
		}
		computedStyles[child] = childStyle
	}

	// Collect items using existing function
	for _, child := range children {
		le.CollectInlineItems(child, state, computedStyles)
	}

	// CSS 2.1 §9.2.2.1: If a block container has block-level children,
	// whitespace-only inline runs between block children don't generate boxes.
	// Group items into "runs" separated by BlockChild items. If a run consists
	// entirely of whitespace text, eliminate it.
	hasBlockChildren := false
	for _, item := range state.Items {
		if item.Type == InlineItemBlockChild {
			hasBlockChildren = true
			break
		}
	}
	if hasBlockChildren {
		filtered := make([]*InlineItem, 0, len(state.Items))
		runStart := 0
		for i := 0; i <= len(state.Items); i++ {
			isBlockOrEnd := (i == len(state.Items)) || state.Items[i].Type == InlineItemBlockChild
			if isBlockOrEnd {
				// Check if run [runStart, i) is whitespace-only
				runIsWhitespaceOnly := true
				for j := runStart; j < i; j++ {
					if state.Items[j].Type != InlineItemText || strings.TrimSpace(state.Items[j].Text) != "" {
						runIsWhitespaceOnly = false
						break
					}
				}
				if !runIsWhitespaceOnly {
					filtered = append(filtered, state.Items[runStart:i]...)
				}
				// Add the block child itself
				if i < len(state.Items) {
					filtered = append(filtered, state.Items[i])
				}
				runStart = i + 1
			}
		}
		state.Items = filtered
	}

	return state.Items
}

// constructLine creates positioned fragments for a single line.
// This is the core of Phase 3 - it creates fragments with CORRECT positions
// from the start (no repositioning needed).
//
// For each item on the line:
// - Text: Create text fragment at current X
// - Float: Position float, create fragment, update constraint
// - Atomic: Create atomic fragment at current X
// - Tags: Skip (markers only)
//
// Returns:
// - fragments: List of positioned fragments for this line
// - newConstraint: Updated constraint with floats added
func (le *LayoutEngine) constructLine(
	line *LineInfo,
	constraint *ConstraintSpace,
) ([]*Fragment, *ConstraintSpace) {
	fragments := []*Fragment{}
	currentConstraint := constraint

	// CRITICAL: Process floats FIRST before inline content
	// Floats affect the positioning of subsequent inline content, even if they
	// appear later in document order. This is per CSS spec: floats are removed
	// from flow and positioned first.
	//
	// Pass 1: Position all floats and update constraint
	for _, item := range line.Items {
		if item.Type == InlineItemFloat {
			floatFrag, newConstraint := le.positionFloat(
				item,
				line.Y,
				currentConstraint,
			)
			fragments = append(fragments, floatFrag)
			currentConstraint = newConstraint
		}
	}

	// Calculate starting X position accounting for floats (now updated) and text-indent
	leftOffset, _ := currentConstraint.ExclusionSpace.AvailableInlineSize(line.Y, line.Height)
	currentX := leftOffset + constraint.TextIndent

	// Pass 2: Process inline content with floats already positioned
	for _, item := range line.Items {
		switch item.Type {
		case InlineItemText:
			txt := item.Text
			width := item.Width
			// Create text fragment with correct position
			frag := NewTextFragment(
				txt,
				item.Style,
				currentX,
				line.Y,
				width,
				item.Height,
				item.Node, // Pass the text node for rendering
			)
			fragments = append(fragments, frag)
			currentX += width

		case InlineItemFloat:
			// Skip - floats are already handled in Pass 1
			continue

		case InlineItemAtomic:
			// Create atomic fragment (inline-block, replaced element)
			// Account for horizontal margins (layoutNode will apply margin.Left to position)
			atomicMarginLeft := 0.0
			atomicMarginRight := 0.0
			if item.Style != nil {
				margin := item.Style.GetMargin()
				atomicMarginLeft = margin.Left
				atomicMarginRight = margin.Right
			}
			frag := &Fragment{
				Type:     FragmentAtomic,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: item.Width, Height: item.Height},
			}
			// For img elements, set the ImagePath for rendering
			if item.Node != nil && item.Node.TagName == "img" {
				if src, ok := item.Node.GetAttribute("src"); ok {
					frag.ImagePath = src
				}
			}
			fragments = append(fragments, frag)
			currentX += atomicMarginLeft + item.Width + atomicMarginRight

		case InlineItemOpenTag:
			// Opening tag marker - create inline fragment marker
			// CSS 2.1 §8.3: Apply left margin/border/padding at inline box start
			frag := &Fragment{
				Type:     FragmentInline,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: 0, Height: 0},
			}
			fragments = append(fragments, frag)
			currentX += item.Width // Advance past left margin+border+padding

		case InlineItemCloseTag:
			// Closing tag marker
			// CSS 2.1 §8.3: Apply right padding+border+margin at inline box end
			frag := &Fragment{
				Type:     FragmentInline,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: 0, Height: 0},
			}
			fragments = append(fragments, frag)
			currentX += item.Width // Advance past right padding+border+margin

		case InlineItemSoftHyphen:
			// Soft hyphen marker — invisible when not at a line break (zero width).
			// When BreakLines breaks at a soft hyphen, it emits the preceding text item
			// with '-' appended, so there is nothing to render here.
			continue

		case InlineItemControl:
			// Control item (br, etc.) - just marker
			frag := &Fragment{
				Type:     FragmentInline,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: currentX, Y: line.Y},
				Size:     Size{Width: 0, Height: 0},
			}
			fragments = append(fragments, frag)

		case InlineItemBlockChild:
			// Block child - create marker fragment
			// Actual layout will happen in LayoutInlineContentToBoxes which has
			// access to all the context needed for layoutNode()
			frag := &Fragment{
				Type:     FragmentBlockChild,
				Node:     item.Node,
				Style:    item.Style,
				Position: Position{X: leftOffset, Y: line.Y}, // Start at left edge
				Size:     Size{Width: 0, Height: 0},          // Will be set after layout
			}
			fragments = append(fragments, frag)
		}
	}

	return fragments, currentConstraint
}

// positionFloat positions a float and creates its fragment.
// This also updates the constraint space with the new float exclusion.
//
// Key principle: Fragment is created with CORRECT position from the start.
// No repositioning or deltas needed.

// ConstructFragments creates positioned fragments from line breaking results.
// This is Phase 3 of the multi-pass inline layout pipeline.
//
// For each line:
// 1. Call constructLine to create fragments
// 2. Propagate constraint updates (floats) to next line
// 3. Accumulate all fragments
//
// Returns:
// - fragments: All positioned fragments (flattened from all lines)
// - finalConstraint: Constraint space after all floats added
//
// This function HAS side effects (creates fragments), but the constraint
// space propagation is clean (immutable updates via WithExclusion).
func (le *LayoutEngine) ConstructFragments(
	lines []*LineInfo,
	constraint *ConstraintSpace,
) ([]*Fragment, *ConstraintSpace) {
	allFragments := []*Fragment{}
	currentConstraint := constraint

	for i, line := range lines {
		// Construct fragments for this line using current constraint
		lineFragments, newConstraint := le.constructLine(line, currentConstraint)

		// Add fragments to result
		allFragments = append(allFragments, lineFragments...)

		// Propagate constraint to next line
		// This ensures floats added on this line affect subsequent lines
		currentConstraint = newConstraint

		// CSS 2.1 §16.1: text-indent only applies to the first line
		// Create a new constraint to avoid mutating the original pointer
		if i == 0 && currentConstraint.TextIndent != 0 {
			cs := *currentConstraint
			cs.TextIndent = 0
			currentConstraint = &cs
		}
	}

	return allFragments, currentConstraint
}

// constraintsChanged checks if the constraint space changed during fragment construction.
// This is used to determine if we need to retry line breaking.
//
// Returns true if:
// - Floats were added (exclusion space changed)
// - Any other constraints changed (future extensions)
//
// This is the key to the retry logic: if Phase 3 added floats that affect
// line breaking, we need to re-run Phase 2 with the updated constraints.

// fragmentsToBoxes converts Fragment tree back to Box tree for existing rendering pipeline.
// This is a TEMPORARY BRIDGE until we migrate the entire pipeline to use fragments.
//
// For now, this allows us to use the new multi-pass architecture while keeping
// the existing rendering code working.
func fragmentsToBoxes(fragments []*Fragment) []*Box {
	boxes := []*Box{}

	for _, frag := range fragments {
		// Skip tag markers (they don't produce visual output)
		if frag.Type == FragmentInline && frag.Size.Width == 0 && frag.Size.Height == 0 {
			continue
		}

		// Create box from fragment
		box := &Box{
			Node:   frag.Node,
			Style:  frag.Style,
			X:      frag.Position.X,
			Y:      frag.Position.Y,
			Width:  frag.Size.Width,
			Height: frag.Size.Height,
		}

		// Convert fragment type to box positioning info
		switch frag.Type {
		case FragmentFloat:
			// Mark as positioned (floats are out of flow)
			box.Position = css.PositionAbsolute // Treated like absolute for rendering
		}

		boxes = append(boxes, box)
	}

	return boxes
}

// fragmentToBoxSingle converts a single fragment to a box.
// Helper for LayoutInlineContentToBoxes when processing fragments individually.
func fragmentToBoxSingle(frag *Fragment) *Box {
	// Skip tag markers (they don't produce visual output)
	if frag.Type == FragmentInline && frag.Size.Width == 0 && frag.Size.Height == 0 {
		return nil
	}

	// Create box from fragment
	box := &Box{
		Node:      frag.Node,
		Style:     frag.Style,
		X:         frag.Position.X,
		Y:         frag.Position.Y,
		Width:     frag.Size.Width,
		Height:    frag.Size.Height,
		ImagePath: frag.ImagePath, // Copy image path for img elements
	}

	// Convert fragment type to box positioning info
	switch frag.Type {
	case FragmentFloat:
		// Mark as positioned (floats are out of flow)
		box.Position = css.PositionAbsolute // Treated like absolute for rendering
	}

	// Apply position:relative offset for inline elements (images, inline-blocks)
	if frag.Style != nil && frag.Style.GetPosition() == css.PositionRelative {
		box.Position = css.PositionRelative
		offset := frag.Style.GetPositionOffset()
		if offset.HasTop {
			box.Y += offset.Top
		} else if offset.HasBottom {
			box.Y -= offset.Bottom
		}
		if offset.HasLeft {
			box.X += offset.Left
		} else if offset.HasRight {
			box.X -= offset.Right
		}
	}

	return box
}

// LayoutInlineContentToBoxes is a convenience wrapper that runs the new multi-pass
// pipeline and converts the result to boxes for the existing rendering pipeline.
//
// This allows gradual migration: call this instead of the old inline layout,
// and the rest of the pipeline keeps working.
func (le *LayoutEngine) LayoutInlineContentToBoxes(
	children []*html.Node,
	containerBox *Box,
	availableWidth float64,
	startY float64,
	computedStyles map[*html.Node]*css.Style,
	overrideStyles map[*html.Node]*css.Style,
) *InlineLayoutResult {

	// Merge override styles into computedStyles so all lookups find them
	if overrideStyles != nil {
		for node, style := range overrideStyles {
			computedStyles[node] = style
		}
	}

	// CSS 2.1 §9.2.1.1: Normalize block-in-inline before the inline layout pipeline.
	// Ensure children have computed styles so normalization can check display types.
	for _, child := range children {
		if child.Type == html.TextNode {
			continue
		}
		if _, ok := computedStyles[child]; ok {
			continue
		}
		if _, ok := le.syntheticStyles[child]; ok {
			continue
		}
		computedStyles[child] = css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
	}
	children = le.normalizeBlocksInInline(children, computedStyles)
	// Populate computedStyles with any synthetic nodes created by normalization
	// (_anon block wrappers). Without this, CollectInlineItems falls back to
	// css.ComputeStyle which returns display:inline for unknown "_anon" tags,
	// causing the _anon block wrappers to be treated as inline (narrow) instead
	// of display:block (full-width).
	for _, child := range children {
		if _, ok := computedStyles[child]; !ok {
			if synthStyle, ok := le.syntheticStyles[child]; ok {
				computedStyles[child] = synthStyle
			}
		}
	}

	// Create constraint space
	constraint := NewConstraintSpace(availableWidth, 0)

	// Check if container has white-space: nowrap
	if containerBox.Style != nil {
		if ws, ok := containerBox.Style.Get("white-space"); ok && (ws == "nowrap" || ws == "pre") {
			constraint.NoWrap = true
		}
		// CSS 2.1 §16.1: text-indent applies to the first line of a block container
		// GetTextIndent returns (fraction, true) for percentages or (pixels, false) for lengths.
		tiVal, tiPct := containerBox.Style.GetTextIndent()
		if tiPct {
			constraint.TextIndent = tiVal * availableWidth
		} else {
			constraint.TextIndent = tiVal
		}

		// text-overflow applies when container has overflow:hidden + nowrap
		if containerBox.Style.GetOverflow() != css.OverflowVisible {
			constraint.TextOverflow = containerBox.Style.GetTextOverflow()
		}

		// word-break and overflow-wrap
		constraint.WordBreak = containerBox.Style.GetWordBreak()
		constraint.OverflowWrap = containerBox.Style.GetOverflowWrap()

		// -webkit-line-clamp: clamp inline content to N lines
		if containerBox.Style.GetBoxOrient() == "vertical" {
			if n := containerBox.Style.GetLineClamp(); n > 0 {
				constraint.LineClampN = n
			}
		}

		// text-wrap: balance/pretty/nowrap
		tw := containerBox.Style.GetTextWrap()
		constraint.TextWrap = tw
		if tw == "nowrap" {
			constraint.NoWrap = true
		}
	}

	// Run new multi-pass pipeline
	fragments := le.LayoutInlineContent(children, constraint, startY, containerBox.Style, overrideStyles)

	// LineMetrics tracks line box height separately from content height
	// This matches CSS 2.1 §10.8.1: line box height is independent of content height
	type LineMetrics struct {
		// Maximum height of content on this line (text, images, atomic inlines)
		// This is the "natural" height of the tallest box
		contentHeight float64

		// Minimum height from inline element line-heights
		// This ensures line boxes have sufficient height even for small text
		lineBoxHeight float64

		// Track if line has any actual content (not just OpenTag markers)
		// Used to determine if we should advance Y for this line
		hasContent bool
	}

	// EffectiveHeight returns the height to use for Y advancement
	// Per CSS spec: line box height is the max of content height and line-height
	lineMetricsEffectiveHeight := func(lm *LineMetrics) float64 {
		if lm.contentHeight > lm.lineBoxHeight {
			return lm.contentHeight
		}
		return lm.lineBoxHeight
	}

	// Reset clears metrics for a new line
	// preserveLineBoxHeight: if true, keeps line-box height from open inline elements
	lineMetricsReset := func(lm *LineMetrics, preserveLineBoxHeight bool) {
		lm.contentHeight = 0
		lm.hasContent = false
		if !preserveLineBoxHeight {
			lm.lineBoxHeight = 0
		}
	}

	// Track inline element spans for creating wrapper boxes
	type inlineSpan struct {
		node             *html.Node
		style            *css.Style
		startX           float64
		startY           float64
		startIdx         int // Fragment index where span started
		startBoxCount    int // len(boxes) at OpenTag time (for wrapper insertion ordering)
		hasChildWrappers bool // true if any child inline wrapper boxes were created during this span
	}

	// Process fragments, handling block children with recursive layout
	boxes := []*Box{}
	currentY := startY
	currentLineY := startY        // Track which line we're on
	lastFinalizedLineHeight := 0.0 // Track the last finalized line height for return
	currentX := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left // Track rightmost X position
	lineMetrics := &LineMetrics{}  // Track line box metrics (content height + line-box height)
	inlineStack := []*inlineSpan{}

	// Track which nodes we've seen to distinguish OpenTag from CloseTag
	// First FragmentInline for a node = OpenTag, second = CloseTag
	seenNodes := make(map[*html.Node]bool)

	// Helper: compute accumulated relative positioning offsets from open inline elements
	// CSS 2.1 §9.4.3: "Once a box has been laid out according to the normal flow...
	// it is shifted according to the offset values."
	// Block children inside relative-positioned inline elements inherit the offset.
	getRelativeOffset := func() (float64, float64) {
		var offsetX, offsetY float64
		for _, span := range inlineStack {
			if span.style != nil {
				if span.style.GetPosition() == css.PositionRelative {
					posOffset := span.style.GetPositionOffset()
					if posOffset.HasTop {
						offsetY += posOffset.Top
					} else if posOffset.HasBottom {
						offsetY -= posOffset.Bottom
					}
					if posOffset.HasLeft {
						offsetX += posOffset.Left
					} else if posOffset.HasRight {
						offsetX -= posOffset.Right
					}
				}
				// CSS vertical-align: <length> on inline spans raises (+) or lowers (-) the
				// element relative to the line's baseline. Positive value = raise up = negative Y.
				// This is equivalent to position:relative; top:-N for the same visual result.
				if va := span.style.GetVerticalAlignOffset(); va != 0 {
					offsetY -= va
				}
			}
		}
		return offsetX, offsetY
	}

	for i, frag := range fragments {
		if frag.Type == FragmentBlockChild {
			// Block child - first finalize the current line before laying out the block
			// Advance currentY past any content on the current line
			// FIX: Only advance if the line had actual content (not just OpenTag markers)
			effectiveHeight := lineMetricsEffectiveHeight(lineMetrics)

			if lineMetrics.hasContent && lineMetricsEffectiveHeight(lineMetrics) > 0 {
				currentY = currentY + effectiveHeight
				lastFinalizedLineHeight = effectiveHeight // Save before resetting
			}
			lineMetricsReset(lineMetrics, false) // Clear for content after block child

			// Block child - call layoutNode recursively
			childNode := frag.Node
			childStyle := computedStyles[childNode]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}

			// Calculate X position (block children start at left edge)
			// CSS 2.1 §9.4.3: Block children inside relative-positioned inlines
			// inherit the relative positioning offset
			relOffX, relOffY := getRelativeOffset()
			childX := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left + relOffX
			childY := currentY + relOffY

			// Recursively layout the block child
			childBox := le.layoutNode(
				childNode,
				childX,
				childY,
				availableWidth,
				computedStyles,
				containerBox,
			)

			boxes = append(boxes, childBox)

			// Update Y for next content (advance past this block)
			childBox.Parent = containerBox
			// CRITICAL: childBox.Height already includes borders and padding (it's total box height)
			// Only add margins to get the total height including spacing
			totalHeight := childBox.Margin.Top + childBox.Height + childBox.Margin.Bottom
			// CRITICAL: Only advance Y for elements in normal flow
			// Absolutely positioned and fixed positioned elements are removed from flow
			floatType := css.FloatNone
			if childBox.Style != nil {
				floatType = childBox.Style.GetFloat()
			}

			if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && floatType == css.FloatNone {
				// Child is in normal flow - advance Y
				// CSS 2.1 §9.5.2: The 'clear' property may push a child below floats,
				// so childBox.Y can be greater than currentY + margin.Top.
				// Compute flow bottom: childBox.Y minus non-flow offsets (parent inline
				// relative offset + child's own relative offset), plus height + margin.
				// This gives the correct Y advancement for both normal and cleared elements.
				flowY := childBox.Y - relOffY
				// Also subtract child's own relative positioning (visual only, not flow)
				if childBox.Style != nil && childBox.Style.GetPosition() == css.PositionRelative {
					offset := childBox.Style.GetPositionOffset()
					if offset.HasTop {
						flowY -= offset.Top
					} else if offset.HasBottom {
						flowY += offset.Bottom
					}
				}
				flowBottom := flowY + childBox.Height + childBox.Margin.Bottom
				if flowBottom > currentY+totalHeight {
					currentY = flowBottom
				} else {
					currentY += totalHeight
				}
				currentLineY = currentY // Update line Y to match
				lastFinalizedLineHeight = effectiveHeight // Save before resetting
		lineMetricsReset(lineMetrics, false) // Reset for next line

				// Reset currentX - block child takes full width, next content starts at left
				currentX = containerBox.X + containerBox.Border.Left + containerBox.Padding.Left
			}
		} else if frag.Type == FragmentInline && frag.Size.Width == 0 && frag.Size.Height == 0 {
			// Inline element marker (OpenTag or CloseTag)
			// Distinguish by checking if we've seen this node before
			isOpenTag := !seenNodes[frag.Node]

			if isOpenTag {
				// OpenTag - push to stack and record fragment index
				// CRITICAL: Use frag.Position.X not currentX - fragments are pre-positioned
				// accounting for floats by line breaking phase
				// CRITICAL: If the OpenTag is on a new line (e.g., after <br>),
				// finalize the previous line before recording startY.
				// Without this, span.startY captures the previous line's Y.
				if frag.Position.Y != currentLineY {
					effectiveHeight := lineMetricsEffectiveHeight(lineMetrics)
					if lineMetrics.hasContent && effectiveHeight > 0 {
						currentY = currentLineY + effectiveHeight
						lastFinalizedLineHeight = effectiveHeight
						lineMetricsReset(lineMetrics, false)
					} else if effectiveHeight > 0 {
						lineMetricsReset(lineMetrics, true)
					}
					currentLineY = frag.Position.Y
				}

				// Record box count at OpenTag time for correct CSS painting order.
				// If child inline wrappers are created during this span, we'll
				// insert this span's wrapper BEFORE them at CloseTag time.
				span := &inlineSpan{
					node:          frag.Node,
					style:         frag.Style,
					startX:        frag.Position.X, // Use fragment position, not currentX
					startY:        currentY,
					startIdx:      i,
					startBoxCount: len(boxes),
				}
				inlineStack = append(inlineStack, span)
				seenNodes[frag.Node] = true

				// Track inline element's line-height contribution to line box
				// When an inline element opens, its line-height should contribute to the line box height
				// This ensures correct Y advancement when block children or line breaks are encountered
				if frag.Style != nil {
					lineHeight := frag.Style.GetLineHeight()
					if lineHeight > lineMetrics.lineBoxHeight {
						lineMetrics.lineBoxHeight = lineHeight
					}
				}
			} else {
				// CloseTag - pop from stack and create wrapper box
				if len(inlineStack) > 0 {
					// Find matching span on stack (should be top for well-formed HTML)
					var span *inlineSpan
					spanIdx := -1
					for idx := len(inlineStack) - 1; idx >= 0; idx-- {
						if inlineStack[idx].node == frag.Node {
							span = inlineStack[idx]
							spanIdx = idx
							break
						}
					}

					if span != nil {
						// Compute relative positioning offset for this inline element
						// This includes the element's own offset + ancestor offsets
						wrapRelX, wrapRelY := getRelativeOffset()

						// Normal inline box (not split)
						endX := frag.Position.X
						wrapperWidth := endX - span.startX

						// Compute border, padding, margin from style
						border := span.style.GetBorderWidth()
						padding := span.style.GetPadding()
						margin := span.style.GetMargin()
						// Inline elements ignore vertical margins (CSS 2.1 §8.3)
						margin.Top = 0
						margin.Bottom = 0

						// CRITICAL FIX: Empty inline elements (no content between OpenTag and CloseTag)
						// must still have dimensions from border and padding (CSS 2.1 §10.3.1)
						// Example: <span style="border:25px; padding:100px"></span>
						// Should render as 250px wide (25+100+0+100+25) even with no content

						// Check if inline is truly empty (no text/atomic content between OpenTag and CloseTag)
						isEmpty := true
						for j := span.startIdx + 1; j < i; j++ {
							if fragments[j].Type == FragmentText || fragments[j].Type == FragmentAtomic {
								isEmpty = false
								break
							}
						}

						if isEmpty {
							// Empty inline: width = full horizontal border + padding (no content)
							wrapperWidth = border.Left + padding.Left + padding.Right + border.Right
						}

						// Calculate height from line-height or font-size
						// Empty inline elements establish line box height per CSS 2.1 §10.8.1
						wrapperHeight := lineMetricsEffectiveHeight(lineMetrics)
						if wrapperHeight == 0 {
							// Use font-size as minimum height for empty inline elements
							fontSize := span.style.GetFontSize()
							if lineHeightValue, ok := span.style.Get("line-height"); ok && lineHeightValue != "normal" && lineHeightValue != "" {
								// Handle relative units (em, %) relative to font-size
								if strings.HasSuffix(lineHeightValue, "em") {
									// Parse the number before "em"
									numStr := strings.TrimSuffix(lineHeightValue, "em")
									if multiplier, err := strconv.ParseFloat(numStr, 64); err == nil {
										wrapperHeight = fontSize * multiplier
									} else {
										wrapperHeight = fontSize // Fallback
									}
								} else if strings.HasSuffix(lineHeightValue, "%") {
									// Parse percentage
									numStr := strings.TrimSuffix(lineHeightValue, "%")
									if pct, err := strconv.ParseFloat(numStr, 64); err == nil {
										wrapperHeight = fontSize * (pct / 100.0)
									} else {
										wrapperHeight = fontSize // Fallback
									}
								} else if parsedValue, parseOk := css.ParseLength(lineHeightValue); parseOk {
									// Absolute units (px, pt, etc.)
									wrapperHeight = parsedValue
								} else {
									wrapperHeight = fontSize // Fallback to font-size
								}
							} else {
								wrapperHeight = fontSize // Default: font-size
							}
						}

						// Box height is the line box height (CSS 2.1 §10.8.1)
						// Borders/padding "bleed" outside this and are drawn separately by the render phase
						// wrapperHeight already equals effective height (line box height)
						// Convert from content-relative to absolute coordinates
						// Fragment positions are relative to container's content area
						// (after border+padding), so add container's offset
						baseX := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left
						// baseY :=  // Y coordinates are already absolute, not needed containerBox.Y + containerBox.Border.Top + containerBox.Padding.Top

						wrapperBox := &Box{
							Node:    span.node,
							Style:   span.style,
							X:       baseX + span.startX + margin.Left + wrapRelX,  // Apply left margin + relative offset
							Y:       span.startY + margin.Top + wrapRelY,   // Apply top margin + relative offset
							Width:   wrapperWidth,
							Height:  wrapperHeight,
							Border:  border,
							Padding: padding,
							Margin:  margin,
							Parent:  containerBox,
						}
						// Insert wrapper at correct position for CSS painting order
						// Block-in-inline normalization: set fragment flags from data-block-fragment attribute
						if span.node != nil {
							if fragType, ok := span.node.GetAttribute("data-block-fragment"); ok {
								switch fragType {
								case "first":
									wrapperBox.IsFirstFragment = true
								case "last":
									wrapperBox.IsLastFragment = true
								case "middle":
									wrapperBox.IsFirstFragment = true
									wrapperBox.IsLastFragment = true
								}
							}
						}
						// Insert wrapper at correct position for CSS painting order.
						// Always insert BEFORE span text/child boxes so the wrapper
						// background renders behind text (CSS §10.1: backgrounds first).
						if span.startBoxCount <= len(boxes) {
							newBoxes := make([]*Box, 0, len(boxes)+1)
							newBoxes = append(newBoxes, boxes[:span.startBoxCount]...)
							newBoxes = append(newBoxes, wrapperBox)
							newBoxes = append(newBoxes, boxes[span.startBoxCount:]...)
							boxes = newBoxes
						} else {
							boxes = append(boxes, wrapperBox)
						}

						// Track wrapper box height for line height calculation
						// CSS 2.1 §10.8.1: Use line box height, NOT visual extent
						// The borders/padding "bleed" outside the line box and don't affect
						// parent container height. The render phase handles drawing the bleeding
						// extent by extending the background/borders (see render.go lines 388-393)
						if wrapperHeight > lineMetricsEffectiveHeight(lineMetrics) {
							lineMetrics.lineBoxHeight = wrapperHeight
						}

						// Mark parent spans as having child wrappers (for CSS painting order)
						for _, parentSpan := range inlineStack {
							if parentSpan != span {
								parentSpan.hasChildWrappers = true
							}
						}

						// Remove span from stack
						inlineStack = append(inlineStack[:spanIdx], inlineStack[spanIdx+1:]...)
					}
				}
			}
		} else if frag.Type == FragmentFloat {
			// Float - recursively layout its contents, then position as a float
			floatNode := frag.Node
			floatStyle := computedStyles[floatNode]
			if floatStyle == nil {
				// Fall back to the style stored on the fragment (e.g. synthetic drop-cap
				// nodes created by initial-letter processing whose style is not in the
				// main computedStyles map).
				floatStyle = frag.Style
			}
			if floatStyle == nil {
				floatStyle = css.NewStyle()
			}

			containerContentLeft := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left
			containerAvailWidth := containerBox.Width - containerBox.Border.Left - containerBox.Padding.Left -
				containerBox.Padding.Right - containerBox.Border.Right

			// Track floats before layoutNode (it may add floats as side effect)
			floatCountBefore := len(le.floats)

			// Layout the float to get actual dimensions (estimated sizes from Phase 1 may be wrong)
			floatBox := le.layoutNode(
				floatNode,
				containerContentLeft,
				currentY,
				containerAvailWidth,
				computedStyles,
				containerBox,
			)

			// Remove any floats added during layoutNode (to avoid double-counting)
			if len(le.floats) > floatCountBefore {
				le.floats = le.floats[:floatCountBefore]
			}

			// Now position the float properly using actual dimensions
			floatType := floatStyle.GetFloat()
			floatY := currentY

			// Apply clear property
			clearType := floatStyle.GetClear()
			if clearType != css.ClearNone {
				floatY = le.getClearY(clearType, floatY)
			}

			// CSS 2.1 §9.5.1: Float drop - if the float doesn't fit beside
			// existing floats at the current Y, drop it below them.
			// Use availableWidth (the constraint passed to this function) rather than
			// containerBox.Width because:
			// - For shrink-to-fit containers (no max-width), availableWidth is the parent's
			//   full width, so floats won't drop unnecessarily (container will grow to fit).
			// - For max-width constrained containers, availableWidth is clamped to max-width,
			//   so floats correctly drop when they exceed the maximum container width.
			floatTotalWidth := floatBox.Margin.Left + floatBox.Width + floatBox.Margin.Right
			if availableWidth > 0 {
				floatY = le.getFloatDropY(floatType, floatTotalWidth, floatY, availableWidth)
			}

			// Get float offsets at the target Y
			leftOffset, rightOffset := le.getFloatOffsets(floatY)

			// Calculate correct X position
			var newX float64
			if floatType == css.FloatLeft {
				newX = containerContentLeft + leftOffset + floatBox.Margin.Left
			} else {
				floatWidth := floatBox.Margin.Left + floatBox.Width + floatBox.Margin.Right
				newX = containerContentLeft + containerAvailWidth - rightOffset - floatWidth + floatBox.Margin.Left
			}
			newY := floatY + floatBox.Margin.Top

			// Reposition box and children
			deltaX := newX - floatBox.X
			deltaY := newY - floatBox.Y
			if deltaX != 0 || deltaY != 0 {
				floatBox.X = newX
				floatBox.Y = newY
				le.shiftChildren(floatBox, deltaX, deltaY)
			}

			// Add float to engine's float list
			le.addFloat(floatBox, floatType, floatY)

			// Mark as floated for rendering
			floatBox.Position = css.PositionAbsolute
			floatBox.Parent = containerBox
			boxes = append(boxes, floatBox)
		} else if frag.Type == FragmentAtomic && frag.Node != nil && frag.Node.TagName != "img" {
			// Non-replaced atomic inline (inline-block) - recursively layout its content
			// Images and other replaced elements use fragmentToBoxSingle instead

			// Check if we've moved to a new line (Y changed)
			if frag.Position.Y != currentLineY {
				effectiveHeight := lineMetricsEffectiveHeight(lineMetrics)
				if lineMetrics.hasContent && effectiveHeight > 0 {
					currentY = currentLineY + effectiveHeight
					lastFinalizedLineHeight = effectiveHeight
					lineMetricsReset(lineMetrics, false)
				} else if effectiveHeight > 0 {
					lineMetricsReset(lineMetrics, true)
				}
				currentLineY = frag.Position.Y
			}

			atomicNode := frag.Node
			absX := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left + frag.Position.X

			atomicBox := le.layoutNode(
				atomicNode,
				absX,
				currentY,
				frag.Size.Width,
				computedStyles,
				containerBox,
			)
			if atomicBox != nil {
				atomicBox.Parent = containerBox
				boxes = append(boxes, atomicBox)

				// Track as content for line metrics
				lineMetrics.hasContent = true
				if atomicBox.Height > lineMetrics.contentHeight {
					lineMetrics.contentHeight = atomicBox.Height
				}

				// Update currentX
				boxRight := atomicBox.X + atomicBox.Width
				if boxRight > currentX {
					currentX = boxRight
				}

			}
		} else {
			// Regular fragment - convert to box
			box := fragmentToBoxSingle(frag)
			if box != nil {
				// Fragment X is relative to line start (content area);
				// add container's content area offset for absolute position
				box.X += containerBox.X + containerBox.Border.Left + containerBox.Padding.Left

				// Check if we've moved to a new line (Y changed)
				if frag.Position.Y != currentLineY {
					// Advance currentY past the previous line
				effectiveHeight := lineMetricsEffectiveHeight(lineMetrics)

					// FIX: Only advance if the previous line had actual content (not just OpenTag markers)
					// This prevents double-advancement when OpenTag sets line-height before content appears
					if lineMetrics.hasContent && lineMetricsEffectiveHeight(lineMetrics) > 0 {
					currentY = currentLineY + effectiveHeight
						lastFinalizedLineHeight = effectiveHeight // Save before resetting
						lineMetricsReset(lineMetrics, false) // Clear both content and line-box height
					} else if effectiveHeight > 0 {
						lineMetricsReset(lineMetrics, true) // Preserve line-box height from open inlines
					}
					currentLineY = frag.Position.Y
				}

				// CRITICAL FIX: Use currentY instead of frag.Position.Y
				// After block children, frag.Position.Y is wrong because BreakLines
				// doesn't know block heights. We track actual Y in currentY.
				// Also apply relative positioning offset from open inline ancestors
				relOffX, relOffY := getRelativeOffset()
				targetY := currentY + relOffY
				if box.Y != targetY {
					box.Y = targetY
				}
				if relOffX != 0 {
					box.X += relOffX
				}

				// Track content height and mark that line has content
				// CSS 2.1 §9.4.2: Whitespace-only text doesn't count as content
				isContent := false
				if frag.Type == FragmentText {
					if strings.TrimSpace(frag.Text) != "" {
						isContent = true
					}
				} else if frag.Type == FragmentAtomic || frag.Type == FragmentBlockChild {
					isContent = true
				}
				if isContent {
					lineMetrics.hasContent = true
					if box.Height > lineMetrics.contentHeight {
						lineMetrics.contentHeight = box.Height
					}
					// CSS 2.1 §10.8.1: Text line-height contributes to line box height
					if frag.Type == FragmentText && frag.Style != nil {
						lh := frag.Style.GetLineHeight()
						if lh > lineMetrics.lineBoxHeight {
							lineMetrics.lineBoxHeight = lh
						}
					}
				}

				// Update currentX to track rightmost position
				boxRight := box.X + box.Width
				if boxRight > currentX {
					currentX = boxRight
				}

				box.Parent = containerBox
				boxes = append(boxes, box)
			}
		}
	}

	// Apply ::first-line styles to boxes on the first line.
	// CSS §12.1: ::first-line applies color, font, background, etc. to the first formatted line.
	// We identify first-line boxes by their Y position matching startY.
	if containerBox.Node != nil && len(le.stylesheets) > 0 &&
		css.HasFirstLineRules(containerBox.Node, le.stylesheets, le.viewport.width, le.viewport.height) {
		firstLineStyle := css.ComputePseudoElementStyle(
			containerBox.Node, "first-line", le.stylesheets,
			le.viewport.width, le.viewport.height, containerBox.Style,
		)
		// Allowed ::first-line properties per CSS spec §12.1:
		// background-color is handled separately (applied to container for full-width coverage)
		firstLineProps := []string{"color", "font-size", "font-family", "font-weight", "font-style",
			"text-decoration", "text-transform", "letter-spacing",
			"word-spacing", "line-height", "vertical-align", "clear"}
		for _, b := range boxes {
			if b == nil || b.Style == nil {
				continue
			}
			// First-line boxes have Y == startY (the initial line Y position)
			if b.Y >= startY && b.Y < startY+1.0 {
				// Create a copy of the style with first-line overrides applied
				newStyle := css.NewStyle()
				newStyle.ViewportWidth = b.Style.ViewportWidth
				newStyle.ViewportHeight = b.Style.ViewportHeight
				// Copy all existing properties
				for k, v := range b.Style.Properties {
					newStyle.Properties[k] = v
				}
				// Apply first-line property overrides
				for _, prop := range firstLineProps {
					if v, ok := firstLineStyle.Properties[prop]; ok {
						containerVal, _ := containerBox.Style.Properties[prop]
						if v != containerVal {
							newStyle.Properties[prop] = v
						}
					}
				}
				b.Style = newStyle
			}
		}

		// Apply ::first-line background-color to the container box for full line-width coverage.
		// CSS §12.1: background applies to the line box (full container width), not just text.
		// We apply to the container and rely on the container's background rendering to fill the line.
		if bgColor, ok := firstLineStyle.Properties["background-color"]; ok {
			containerVal, _ := containerBox.Style.Properties["background-color"]
			if bgColor != containerVal {
				newContainerStyle := css.NewStyle()
				newContainerStyle.ViewportWidth = containerBox.Style.ViewportWidth
				newContainerStyle.ViewportHeight = containerBox.Style.ViewportHeight
				for k, v := range containerBox.Style.Properties {
					newContainerStyle.Properties[k] = v
				}
				newContainerStyle.Properties["background-color"] = bgColor
				containerBox.Style = newContainerStyle
			}
		}
	}

	// Apply direction:rtl mirroring and text-align to inline children
	if containerBox.Style != nil {
		display := containerBox.Style.GetDisplay()
		if display != css.DisplayInline {
			contentWidth := containerBox.Width - containerBox.Padding.Left - containerBox.Padding.Right - containerBox.Border.Left - containerBox.Border.Right
			contentLeft := containerBox.X + containerBox.Border.Left + containerBox.Padding.Left
			contentRight := contentLeft + contentWidth

			isRTL := containerBox.Style.GetDirection() == css.DirectionRTL

			// For RTL, mirror all inline box positions within the container
			if isRTL {
				le.mirrorInlineBoxesRTL(boxes, contentLeft, contentRight)
			}

			// Determine effective text-align
			textAlign := ""
			if ta, ok := containerBox.Style.Get("text-align"); ok {
				textAlign = ta
			}
			if isRTL && textAlign == "" {
				// RTL default alignment is "right" — boxes are already right-aligned after mirror
				textAlign = "right"
			}

			// Determine text-align-last (controls last line alignment)
			textAlignLast := containerBox.Style.GetTextAlignLast()

			if textAlign != "" && textAlign != "left" {
				le.applyTextAlignToBoxes(boxes, containerBox, textAlign, contentWidth, textAlignLast)
			} else if isRTL && textAlign == "left" {
				// RTL + text-align:left: shift mirrored boxes to left edge
				le.applyTextAlignToBoxes(boxes, containerBox, "left", contentWidth, textAlignLast)
			} else if textAlignLast != "auto" && textAlignLast != "left" {
				// text-align is left (default) but text-align-last overrides the last line
				le.applyTextAlignToBoxes(boxes, containerBox, "left", contentWidth, textAlignLast)
			}
		}
	}

	// Determine final line height: use current line if active, otherwise last finalized
	finalLineHeight := lineMetricsEffectiveHeight(lineMetrics)
	if finalLineHeight == 0 {
		finalLineHeight = lastFinalizedLineHeight
	}

	// Create inline context for auto-height calculation
	// Track the final line Y and line height so parent can calculate its height
	// LineBoxes should only include actual inline boxes (text, inline elements),
	// NOT block element children, because the strut height check in auto-height should
	// only apply to containers with actual inline content.
	// Note: Text boxes inherit their parent's display:block style but are NOT block children.
	// Only exclude actual element nodes with block display.
	inlineBoxes := []*Box{}
	for _, b := range boxes {
		if b.Node != nil && b.Node.Type == html.ElementNode &&
			b.Style != nil && (b.Style.GetDisplay() == css.DisplayBlock || b.Style.GetDisplay() == css.DisplayFlowRoot || b.Style.GetDisplay() == css.DisplayTable || b.Style.GetDisplay() == css.DisplayListItem) {
			continue // Skip actual block element children
		}
		// Skip whitespace-only text boxes that were stripped during line breaking
		// (they have Width=0 and shouldn't trigger strut height)
		if b.Node != nil && b.Node.Type == html.TextNode && b.Width == 0 {
			continue
		}
		inlineBoxes = append(inlineBoxes, b)
	}
	finalInlineCtx := &InlineContext{
		LineX:      0,               // Not needed for height calculation
		LineY:      currentY,        // Final Y position after all content
		LineHeight: finalLineHeight, // Height of the current or last finalized line
		LineBoxes:  inlineBoxes,     // Only inline boxes (not block children)
	}

	return &InlineLayoutResult{
		ChildBoxes:     boxes,
		FinalInlineCtx: finalInlineCtx,
		UsedMultiPass:  true,
	}
}
func (le *LayoutEngine) layoutInlineContentWIP(
	node *html.Node,
	box *Box,
	availableWidth float64,
	startY float64,
	border, padding css.BoxEdge,
	computedStyles map[*html.Node]*css.Style,
) []*Box {
	// Initialize state
	state := &InlineLayoutState{
		Items:          []*InlineItem{},
		Lines:          []*LineBreakResult{},
		ContainerBox:   box,
		ContainerStyle: box.Style,
		AvailableWidth: availableWidth,
		StartY:         startY,
		Border:         border,
		Padding:        padding,
		FloatList:      []FloatInfo{},
		FloatBaseIndex: le.floatBase,
	}

	// Phase 1: Collect inline items
	for _, child := range node.Children {
		le.CollectInlineItems(child, state, computedStyles)
	}

	// Phase 2 & 3: Line breaking with retry when floats change available width
	// This implements the Gecko-style retry mechanism (RedoMoreFloats)
	const maxRetries = 3 // Prevent infinite loops
	var boxes []*Box

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Phase 2: Break into lines with current float state
		success := le.breakLinesWIP(state)
		if !success {
			return []*Box{} // Line breaking failed
		}

		// Phase 3: Construct line boxes and layout floats
		// Returns boxes and whether retry is needed
		boxes, retryNeeded := le.constructLineBoxesWithRetry(state, box, computedStyles)

		if !retryNeeded {
			// Success - no floats changed available width
			return boxes
		}

		// Retry needed - a float changed available width
		// Phase 2 will be re-run with updated float list on next iteration
	}

	// Max retries exceeded - return what we have
	return boxes
}

// Phase 1: CollectInlineItems flattens the DOM tree into a sequential list of inline items.
// This converts the hierarchical structure into a flat array that's easier to process for line breaking.
//
// Example:
//
//	<p>Hello <em>world</em>!</p>
//
// Becomes:
//
//	[Text("Hello "), OpenTag(<em>), Text("world"), CloseTag(</em>), Text("!")]
func (le *LayoutEngine) CollectInlineItems(node *html.Node, state *InlineLayoutState, computedStyles map[*html.Node]*css.Style) {
	if node == nil {
		return
	}

	// Handle text nodes
	if node.Type == html.TextNode {
		if node.Text == "" {
			return
		}

		// Get parent style for text measurements
		parentStyle := state.ContainerStyle
		if node.Parent != nil {
			if style := computedStyles[node.Parent]; style != nil {
				parentStyle = style
			}
		}

		// Check for initial-letter (drop cap) styling on the container.
		// initial-letter creates a floated box for the first letter, sized to span
		// multiple lines. It applies only to the very first text in the container.
		if len(state.Items) == 0 && state.ContainerStyle != nil {
			il := state.ContainerStyle.GetInitialLetter()
			if il.Set {
				firstLetter, remaining := extractFirstLetter(node.Text)
				if firstLetter != "" {
					// Compute the drop-cap font size.
					// The drop cap should have a cap-height equal to (size * line-height).
					// Using cap-height ≈ 0.7 × font-size: fontSize = size * lineHeight / 0.7
					baseLineHeight := state.ContainerStyle.GetLineHeight()
					dropCapFontSize := il.Size * baseLineHeight / 0.7

					// Build a synthetic style for the drop-cap span
					dropCapStyle := css.NewStyle()
					dropCapStyle.ViewportWidth = le.viewport.width
					dropCapStyle.ViewportHeight = le.viewport.height
					dropCapStyle.Set("float", "left")
					dropCapStyle.Set("font-size", fmt.Sprintf("%.4fpx", dropCapFontSize))
					// Inherit font-family and font-weight from container
					if ff, ok := state.ContainerStyle.Get("font-family"); ok {
						dropCapStyle.Set("font-family", ff)
					}
					if fw, ok := state.ContainerStyle.Get("font-weight"); ok {
						dropCapStyle.Set("font-weight", fw)
					}
					if color, ok := state.ContainerStyle.Get("color"); ok {
						dropCapStyle.Set("color", color)
					}
					// Small right margin so text doesn't hug the drop cap
					dropCapStyle.Set("margin-right", "4px")

					// Compute dimensions of the drop cap
					dcBold := dropCapStyle.GetFontWeight() == css.FontWeightBold
					dcItalic := dropCapStyle.GetFontStyle() == css.FontStyleItalic
					dcMono := dropCapStyle.IsMonospaceFamily()
					dcAhem := dropCapStyle.IsAhemFamily()
					dcWidth, _ := text.MeasureTextWithStyle(firstLetter, dropCapFontSize, dcBold, dcItalic, dcMono, dcAhem)
					// Use fontSize * 1.2 for float exclusion height, matching the engine's
					// default float height estimate (style.GetFontSize() * 1.2) so that
					// references using plain floats with the same font-size produce the same
					// text wrapping behavior.
					dcHeight := dropCapFontSize * 1.2

					// Create a synthetic DOM node for the drop-cap letter
					textChild := &html.Node{
						Type: html.TextNode,
						Text: firstLetter,
					}
					dropCapNode := &html.Node{
						Type:       html.ElementNode,
						TagName:    "span",
						Attributes: map[string]string{},
						Children:   []*html.Node{textChild},
						Parent:     node.Parent,
					}
					textChild.Parent = dropCapNode

					// Register the style so layoutNode and LayoutInlineContentToBoxes can find it.
					// Both le.syntheticStyles (used by layoutNode) and computedStyles (used by
					// FragmentFloat handling for floatType lookup) must be populated.
					le.syntheticStyles[dropCapNode] = dropCapStyle
					le.syntheticStyles[textChild] = dropCapStyle
					computedStyles[dropCapNode] = dropCapStyle
					computedStyles[textChild] = dropCapStyle

					// Create the float item for the drop cap
					floatItem := &InlineItem{
						Type:   InlineItemFloat,
						Node:   dropCapNode,
						Style:  dropCapStyle,
						Width:  dcWidth,
						Height: dcHeight,
					}
					state.Items = append(state.Items, floatItem)

					// Create a text item for the remaining text
					if remaining != "" {
						remFontSize := parentStyle.GetFontSize()
						remBold := parentStyle.GetFontWeight() == css.FontWeightBold
						remItalic := parentStyle.GetFontStyle() == css.FontStyleItalic
						remMono := parentStyle.IsMonospaceFamily()
						remAhem := parentStyle.IsAhemFamily()
						remWidth, remHeight := text.MeasureTextWithStyle(remaining, remFontSize, remBold, remItalic, remMono, remAhem)
						remItem := &InlineItem{
							Type:        InlineItemText,
							Node:        node,
							Text:        remaining,
							StartOffset: len(firstLetter),
							EndOffset:   len(node.Text),
							Style:       parentStyle,
							Width:       remWidth,
							Height:      remHeight,
						}
						state.Items = append(state.Items, remItem)
					}
					return
				}
			}
		}

		// Check for ::first-letter pseudo-element styling
		// This applies to the first letter of the first text in a block container
		shouldApplyFirstLetter := false
		if node.Parent != nil && len(state.Items) == 0 {
			// This is the first text in the inline batch
			// Check if there are any :first-letter rules for the parent
			for _, stylesheet := range le.stylesheets {
				for _, rule := range stylesheet.Rules {
					if rule.Selector.PseudoElement == "first-letter" {
						if css.MatchesSelector(node.Parent, rule.Selector) {
							shouldApplyFirstLetter = true
							break
						}
					}
				}
				if shouldApplyFirstLetter {
					break
				}
			}
		}

		if shouldApplyFirstLetter {
			// Get the computed first-letter style
			firstLetterStyle := css.ComputePseudoElementStyle(node.Parent, "first-letter", le.stylesheets, le.viewport.width, le.viewport.height, parentStyle)
			firstLetter, remaining := extractFirstLetter(node.Text)

			if firstLetter != "" {
				// Create item for the first letter with special styling
				flFontSize := firstLetterStyle.GetFontSize()
				flBold := firstLetterStyle.GetFontWeight() == css.FontWeightBold
				flItalic := firstLetterStyle.GetFontStyle() == css.FontStyleItalic
				flMono := firstLetterStyle.IsMonospaceFamily()
				flAhem := firstLetterStyle.IsAhemFamily()
				flWidth, flHeight := text.MeasureTextWithStyle(firstLetter, flFontSize, flBold, flItalic, flMono, flAhem)

				firstLetterItem := &InlineItem{
					Type:        InlineItemText,
					Node:        node,
					Text:        firstLetter,
					StartOffset: 0,
					EndOffset:   len(firstLetter),
					Style:       firstLetterStyle,
					Width:       flWidth,
					Height:      flHeight,
				}
				state.Items = append(state.Items, firstLetterItem)

				// If there's remaining text, create an item for it
				if remaining != "" {
					fontSize := parentStyle.GetFontSize()
					bold := parentStyle.GetFontWeight() == css.FontWeightBold
					italic := parentStyle.GetFontStyle() == css.FontStyleItalic
					mono := parentStyle.IsMonospaceFamily()
					ahem := parentStyle.IsAhemFamily()
					width, height := text.MeasureTextWithStyle(remaining, fontSize, bold, italic, mono, ahem)

					remainingItem := &InlineItem{
						Type:        InlineItemText,
						Node:        node,
						Text:        remaining,
						StartOffset: len(firstLetter),
						EndOffset:   len(node.Text),
						Style:       parentStyle,
						Width:       width,
						Height:      height,
					}
					state.Items = append(state.Items, remainingItem)
				}
				return
			}
		}

		// Normal text without first-letter styling
		// CSS 2.1 §16.6.1: For white-space: normal, collapse newlines and tabs to spaces,
		// then collapse consecutive spaces to a single space
		textContent := node.Text
		whiteSpace := parentStyle.GetWhiteSpace()
		// For pre-wrap/pre/pre-line: restore original whitespace from RawText if available.
		// This is needed when white-space is set via a stylesheet rule (not inline style),
		// because the HTML parser normalizes whitespace before CSS is applied.
		if (whiteSpace == "pre" || whiteSpace == "pre-wrap" || whiteSpace == "pre-line") && node.RawText != "" {
			textContent = node.RawText
		}
		if whiteSpace == "" || whiteSpace == "normal" || whiteSpace == "nowrap" {
			// Replace newlines and tabs with spaces
			textContent = strings.Map(func(r rune) rune {
				if r == '\n' || r == '\r' || r == '\t' {
					return ' '
				}
				return r
			}, textContent)
			// Collapse consecutive spaces to a single space
			for strings.Contains(textContent, "  ") {
				textContent = strings.ReplaceAll(textContent, "  ", " ")
			}
			node.Text = textContent
		}
		// Apply text-transform before measurement
		textTransform := parentStyle.GetTextTransform()
		if textTransform != css.TextTransformNone {
			textContent = ApplyTextTransform(textContent, textTransform)
			node.Text = textContent
		}

		// Strip soft hyphens (U+00AD) when hyphens: none — they must be invisible.
		// When hyphens != "none", soft hyphens are handled below (split into sub-items
		// with InlineItemSoftHyphen markers between them).
		if strings.ContainsRune(textContent, '\u00AD') {
			hyphensVal := parentStyle.GetHyphens()
			if hyphensVal == "none" {
				textContent = strings.ReplaceAll(textContent, "\u00AD", "")
				node.Text = textContent
			}
		}

		// CSS 2.1 §16.6.1: For white-space: pre, newlines force line breaks.
		// Split text at newline characters and insert forced break items between segments.
		if whiteSpace == "pre" || whiteSpace == "pre-wrap" || whiteSpace == "pre-line" {
			lines := strings.Split(textContent, "\n")
			fontSize := parentStyle.GetFontSize()
			bold := parentStyle.GetFontWeight() == css.FontWeightBold
			italic := parentStyle.GetFontStyle() == css.FontStyleItalic
			mono := parentStyle.IsMonospaceFamily()
			ahem := parentStyle.IsAhemFamily()
			letterSpacing := parentStyle.GetLetterSpacing()
			for j, line := range lines {
				if j > 0 {
					// Insert forced line break for \n
					state.Items = append(state.Items, &InlineItem{
						Type:  InlineItemControl,
						Node:  node,
						Style: parentStyle,
						Width: 0, Height: 0,
					})
				}
				// For pre, handle tab characters with proper tab-stop logic per CSS Text Level 3.
				if (whiteSpace == "pre" || whiteSpace == "pre-wrap") && strings.Contains(line, "\t") {
					// Tab-stop expansion: compute tab stop size in pixels.
					tabSizeVal, tabSizeIsLength := parentStyle.GetTabSize()
					var tabStopPx float64
					if tabSizeIsLength {
						tabStopPx = tabSizeVal
					} else {
						// tab-size is a character count: stop = N * advance-of-space
						spaceW, _ := text.MeasureTextWithStyle(" ", fontSize, bold, italic, mono, ahem)
						if spaceW <= 0 {
							spaceW = fontSize // fallback: 1em per character
						}
						tabStopPx = tabSizeVal * spaceW
					}
					if tabStopPx <= 0 {
						tabStopPx = fontSize * 8
					}

					// Split at tabs and create items, tracking accumulated X.
					_, segH := text.MeasureTextWithStyle("X", fontSize, bold, italic, mono, ahem)
					segments := strings.Split(line, "\t")
					currentLinePx := 0.0
					for si, seg := range segments {
						if len(seg) > 0 {
							segW, _ := text.MeasureTextWithStyle(seg, fontSize, bold, italic, mono, ahem)
							if letterSpacing != 0 && len([]rune(seg)) > 1 {
								segW += letterSpacing * float64(len([]rune(seg))-1)
							}
							segNode := &html.Node{
								Type:   html.TextNode,
								Text:   seg,
								Parent: node.Parent,
							}
							state.Items = append(state.Items, &InlineItem{
								Type:        InlineItemText,
								Node:        segNode,
								Text:        seg,
								StartOffset: 0,
								EndOffset:   len(seg),
								Style:       parentStyle,
								Width:       segW,
								Height:      segH,
							})
							currentLinePx += segW
						}
						// After each segment except the last, insert a tab-width item.
						if si < len(segments)-1 {
							rem := math.Mod(currentLinePx, tabStopPx)
							var tabW float64
							if rem < 1e-9 {
								tabW = tabStopPx
							} else {
								tabW = tabStopPx - rem
							}
							tabNode := &html.Node{
								Type:   html.TextNode,
								Text:   "",
								Parent: node.Parent,
							}
							state.Items = append(state.Items, &InlineItem{
								Type:        InlineItemText,
								Node:        tabNode,
								Text:        "",
								StartOffset: 0,
								EndOffset:   0,
								Style:       parentStyle,
								Width:       tabW,
								Height:      segH,
							})
							currentLinePx += tabW
						}
					}
				} else {
					// No tabs (or pre-line): simple path
					if whiteSpace != "pre" && whiteSpace != "pre-wrap" {
						// pre-line: no tab preservation needed
					}
					w, h := text.MeasureTextWithStyle(line, fontSize, bold, italic, mono, ahem)
					if letterSpacing != 0 && len([]rune(line)) > 1 {
						w += letterSpacing * float64(len([]rune(line))-1)
					}
					lineNode := &html.Node{
						Type:   html.TextNode,
						Text:   line,
						Parent: node.Parent,
					}
					state.Items = append(state.Items, &InlineItem{
						Type:        InlineItemText,
						Node:        lineNode,
						Text:        line,
						StartOffset: 0,
						EndOffset:   len(line),
						Style:       parentStyle,
						Width:       w,
						Height:      h,
					})
				}
			}
			return
		}

		fontSize := parentStyle.GetFontSize()
		bold := parentStyle.GetFontWeight() == css.FontWeightBold
		italic := parentStyle.GetFontStyle() == css.FontStyleItalic
		mono := parentStyle.IsMonospaceFamily()
		ahem := parentStyle.IsAhemFamily()
		letterSpacing := parentStyle.GetLetterSpacing()
		wordSpacing := parentStyle.GetWordSpacing()

		// Split text into word-level items for proper word wrapping and text-align:justify.
		// Each word and each space run becomes its own InlineItem so BreakLines can wrap
		// at word boundaries and applyTextAlignToBoxes can distribute space between word boxes.
		hyphens := parentStyle.GetHyphens()
		wordParts := splitTextIntoWordAndSpaceParts(textContent)
		for _, part := range wordParts {
			// Handle soft hyphens (U+00AD) in word parts when hyphens != "none".
			// Split each word at soft hyphen positions, inserting InlineItemSoftHyphen
			// break opportunity markers between the sub-pieces.
			if hyphens != "none" && strings.ContainsRune(part, '\u00AD') {
				subParts := strings.Split(part, "\u00AD")
				for si, subPart := range subParts {
					if si > 0 {
						// Insert soft hyphen break opportunity item (zero width)
						_, shH := text.MeasureTextWithStyle("X", fontSize, bold, italic, mono, ahem)
						state.Items = append(state.Items, &InlineItem{
							Type:   InlineItemSoftHyphen,
							Node:   node,
							Style:  parentStyle,
							Width:  0,
							Height: shH,
						})
					}
					if subPart == "" {
						continue
					}
					subW, subH := text.MeasureTextWithStyle(subPart, fontSize, bold, italic, mono, ahem)
					if letterSpacing != 0 && len([]rune(subPart)) > 1 {
						subW += letterSpacing * float64(len([]rune(subPart))-1)
					}
					subNode := &html.Node{
						Type:   html.TextNode,
						Text:   subPart,
						Parent: node.Parent,
					}
					state.Items = append(state.Items, &InlineItem{
						Type:        InlineItemText,
						Node:        subNode,
						Text:        subPart,
						StartOffset: 0,
						EndOffset:   len(subPart),
						Style:       parentStyle,
						Width:       subW,
						Height:      subH,
					})
				}
				continue
			}
			partW, partH := text.MeasureTextWithStyle(part, fontSize, bold, italic, mono, ahem)
			if letterSpacing != 0 && len([]rune(part)) > 1 {
				partW += letterSpacing * float64(len([]rune(part))-1)
			}
			// Apply word-spacing to space items (CSS 2.1 §16.4)
			if wordSpacing != 0 && strings.TrimSpace(part) == "" && len(part) > 0 {
				partW += wordSpacing
			}
			partNode := &html.Node{
				Type:   html.TextNode,
				Text:   part,
				Parent: node.Parent,
			}
			state.Items = append(state.Items, &InlineItem{
				Type:        InlineItemText,
				Node:        partNode,
				Text:        part,
				StartOffset: 0,
				EndOffset:   len(part),
				Style:       parentStyle,
				Width:       partW,
				Height:      partH,
			})
		}
		return
	}

	// Handle element nodes
	if node.Type == html.ElementNode {
		style := computedStyles[node]
		if style == nil {
			// Compute style on-the-fly for nested elements not in the map
			// (collectInlineItemsClean only pre-computes direct children)
			style = css.ComputeStyle(node, le.stylesheets, le.viewport.width, le.viewport.height)
			// Inherit from parent if available
			if node.Parent != nil {
				if parentStyle := computedStyles[node.Parent]; parentStyle != nil {
					css.ApplyInheritedProperties(node, style, computedStyles)
				}
			}
			computedStyles[node] = style
		}

		display := style.GetDisplay()

		// Skip display:none elements
		if display == css.DisplayNone {
			return
		}

		// Images and SVG default to inline-block display (replaced elements)
		if (node.TagName == "img" || node.TagName == "svg") && display != css.DisplayNone && display != css.DisplayBlock {
			display = css.DisplayInlineBlock
		}

		// Skip absolutely positioned elements — they're out of flow and will be
		// laid out separately by the parent container's absolute positioning pass.
		pos := style.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			return
		}

		// Check for floats BEFORE display switch - floated elements compute to
		// display:block per CSS spec, but should be treated as float items regardless
		floatVal := style.GetFloat()
		if floatVal != css.FloatNone {
			// Floated elements become atomic items
			// NEW ARCHITECTURE: Use ComputeMinMaxSizes instead of layoutNode!
			// This is PURE - no side effects, no float pollution

			// Create a constraint space for sizing the float
			constraint := NewConstraintSpace(state.AvailableWidth, 0)

			// Compute dimensions WITHOUT laying out (no side effects!)
			sizes := le.ComputeMinMaxSizes(node, constraint, style)

			// For floats, use max content size (preferred width)
			// Height will be computed during actual layout in Phase 3
			width := sizes.MaxContentSize

			// Use explicit CSS height if available, otherwise estimate from font size
			height := style.GetFontSize() * 1.2 // Default estimate
			if h, ok := style.GetLength("height"); ok {
				// Explicit height: compute border-box height
				padding := style.GetPadding()
				border := style.GetBorderWidth()
				height = h + padding.Top + padding.Bottom + border.Top + border.Bottom
			}

			item := &InlineItem{
				Type:   InlineItemFloat,
				Node:   node,
				Style:  style,
				Width:  width,
				Height: height,
			}
			state.Items = append(state.Items, item)
			// Don't process children - they're part of the float box
			return
		}

		// Handle different display types
		switch display {
		case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayTable, css.DisplayListItem, css.DisplayFlex:
			// Block elements in inline contexts are handled as BlockChild items
			// They force line breaks before and after, and require recursive layout
			item := &InlineItem{
				Type:   InlineItemBlockChild,
				Node:   node,
				Style:  style,
				Width:  0, // Will be determined during recursive layout
				Height: 0, // Will be determined during recursive layout
			}
			state.Items = append(state.Items, item)
			return

		case css.DisplayInline:
			// Special case: <br/> creates a line break (Control item)
			if node.TagName == "br" {
				item := &InlineItem{
					Type:   InlineItemControl,
					Node:   node,
					Style:  style,
					Width:  0,
					Height: 0,
				}
				state.Items = append(state.Items, item)
				return
			}

			// Check if this inline element contains ONLY block-level children
			// Per CSS 2.1 §9.2.1.1: When an inline box contains a block box, the inline
			// is broken around the block. If the resulting anonymous inline boxes are empty
			// (no text, no inline content), they shouldn't create visible space.
			hasOnlyBlockChildren := true
			hasAnyChildren := false
			for _, child := range node.Children {
				hasAnyChildren = true
				// Text nodes with non-whitespace content count as inline
				if child.Type == html.TextNode && strings.TrimSpace(child.Text) != "" {
					hasOnlyBlockChildren = false
					break
				}
				// Element nodes need style check
				if child.Type == html.ElementNode {
					childStyle := computedStyles[child]
					if childStyle == nil {
						// Compute on the fly for nested elements not in the map
						childStyle = css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
						computedStyles[child] = childStyle
					}
					childDisplay := childStyle.GetDisplay()
					// Block-level displays don't break the pattern
					if childDisplay != css.DisplayBlock && childDisplay != css.DisplayFlowRoot && childDisplay != css.DisplayTable && childDisplay != css.DisplayListItem {
						hasOnlyBlockChildren = false
						break
					}
				}
			}

			// If inline contains only block children, skip OpenTag/CloseTag to avoid empty inline boxes
			if hasAnyChildren && hasOnlyBlockChildren {
				// Just process children directly without creating inline box fragments
				for _, child := range node.Children {
					le.CollectInlineItems(child, state, computedStyles)
				}
				return
			}

			// Regular inline element - add open tag
			// CSS 2.1 §8.3: Inline element's left margin/border/padding appear at start
			margin := style.GetMargin()
			padding := style.GetPadding()
			border := style.GetBorderWidth()
			openWidth := margin.Left + border.Left + padding.Left
			closeWidth := padding.Right + border.Right + margin.Right

			openItem := &InlineItem{
				Type:  InlineItemOpenTag,
				Node:  node,
				Style: style,
				Width: openWidth,
			}
			state.Items = append(state.Items, openItem)

			// Process children recursively
			for _, child := range node.Children {
				le.CollectInlineItems(child, state, computedStyles)
			}

			// Add close tag
			// CSS 2.1 §8.3: Right margin/border/padding appear at end
			closeItem := &InlineItem{
				Type:  InlineItemCloseTag,
				Node:  node,
				Style: style,
				Width: closeWidth,
			}
			state.Items = append(state.Items, closeItem)

		case css.DisplayInlineBlock:
			// Atomic inline element
			// NEW ARCHITECTURE: Use ComputeMinMaxSizes instead of layoutNode!
			// This is PURE - no side effects

			var width, height float64

			// Special case for SVG elements: use width/height attributes (unitless = pixels)
			if node.TagName == "svg" {
				if widthAttr, ok := node.GetAttribute("width"); ok {
					if w, err := strconv.ParseFloat(widthAttr, 64); err == nil {
						width = w
					}
				}
				if heightAttr, ok := node.GetAttribute("height"); ok {
					if h, err := strconv.ParseFloat(heightAttr, 64); err == nil {
						height = h
					}
				}
			}

			// Special case for img elements: load actual image dimensions
			if node.TagName == "img" {
				if src, ok := node.GetAttribute("src"); ok {
					// Try to load image to get natural dimensions
					if w, h, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil {
						width = float64(w)
						height = float64(h)

						// Check for explicit CSS width/height, then HTML attributes
						hasWidth := false
						hasHeight := false

						if cssWidth, ok := style.GetLength("width"); ok {
							width = cssWidth
							hasWidth = true
						} else if widthPct, ok := style.GetPercentage("width"); ok {
							// Percentage width resolved against available width
							width = state.AvailableWidth * widthPct / 100
							hasWidth = true
						} else if widthAttr, ok := node.GetAttribute("width"); ok {
							// HTML attributes use unitless numbers (pixels)
							if attrW, err := strconv.ParseFloat(widthAttr, 64); err == nil {
								width = attrW
								hasWidth = true
							}
						}

						if cssHeight, ok := style.GetLength("height"); ok {
							height = cssHeight
							hasHeight = true
						} else if heightPct, ok := style.GetPercentage("height"); ok {
							// Percentage height - for now, use natural dimensions
							// (proper handling requires containing block height)
							_ = heightPct // unused for now
							height = float64(h)
							hasHeight = true
						} else if heightAttr, ok := node.GetAttribute("height"); ok {
							// HTML attributes use unitless numbers (pixels)
							if attrH, err := strconv.ParseFloat(heightAttr, 64); err == nil {
								height = attrH
								hasHeight = true
							}
						}

						// If only one dimension specified, scale the other to maintain aspect ratio
						if hasWidth && !hasHeight && w > 0 {
							height = width * float64(h) / float64(w)
						} else if hasHeight && !hasWidth && h > 0 {
							width = height * float64(w) / float64(h)
						}
					} else {
						// Image loading failed - use fallback dimensions
						width = 0
						height = 0
					}
				}
			}

			// For non-img elements, check CSS width/height first
			if node.TagName != "img" {
				if cssWidth, ok := style.GetLength("width"); ok {
					width = cssWidth
					// Add padding/border for border-box calculation
					padding := style.GetPadding()
					border := style.GetBorderWidth()
					width += padding.Left + padding.Right + border.Left + border.Right
				}
				if cssHeight, ok := style.GetLength("height"); ok {
					height = cssHeight
					padding := style.GetPadding()
					border := style.GetBorderWidth()
					height += padding.Top + padding.Bottom + border.Top + border.Bottom
				}
			}

			// If no explicit width, compute from children's text content
			// using the inline-block element's inherited style for correct font properties.
			// ComputeMinMaxSizes has font inheritance issues for text nodes.
			if width == 0 {
				fontSize := style.GetFontSize()
				bold := style.GetFontWeight() == css.FontWeightBold
				italic := style.GetFontStyle() == css.FontStyleItalic
				mono := style.IsMonospaceFamily()
				ahem := style.IsAhemFamily()

				// Measure children text content with parent's font properties
				for _, child := range node.Children {
					if child.Type == html.TextNode && child.Text != "" {
						tw, th := text.MeasureTextWithStyle(child.Text, fontSize, bold, italic, mono, ahem)
						width += tw
						if th > height {
							height = th
						}
					} else if child.Type == html.ElementNode {
						// For element children, fall back to ComputeMinMaxSizes
						childStyle := css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
						if childStyle != nil {
							constraint := NewConstraintSpace(state.AvailableWidth, 0)
							sizes := le.ComputeMinMaxSizes(child, constraint, childStyle)
							childWidth := sizes.MaxContentSize
							// Block children's margins consume space within the
							// inline-block container's content area
							childMargin := childStyle.GetMargin()
							childWidth += childMargin.Left + childMargin.Right
							width += childWidth
						}
					}
				}

				// Add padding/border from the inline-block element itself
				padding := style.GetPadding()
				border := style.GetBorderWidth()
				width += padding.Left + padding.Right + border.Left + border.Right

				// Estimate height if not set from children
				if height == 0 {
					height = fontSize * 1.2
				}
			}

			item := &InlineItem{
				Type:   InlineItemAtomic,
				Node:   node,
				Style:  style,
				Width:  width,
				Height: height,
			}
			state.Items = append(state.Items, item)
			// Don't process children - they're part of the atomic box

		default:
			// Other display types - treat as atomic for now
			// NEW ARCHITECTURE: Use ComputeMinMaxSizes instead of layoutNode!
			// This is PURE - no side effects

			// Create a constraint space for sizing
			constraint := NewConstraintSpace(state.AvailableWidth, 0)

			// Compute dimensions WITHOUT laying out (no side effects!)
			sizes := le.ComputeMinMaxSizes(node, constraint, style)

			// Use max content size (preferred width)
			width := sizes.MaxContentSize

			// Estimate height (will be accurate in Phase 3)
			height := style.GetFontSize() * 1.2 // Rough estimate

			item := &InlineItem{
				Type:   InlineItemAtomic,
				Node:   node,
				Style:  style,
				Width:  width,
				Height: height,
			}
			state.Items = append(state.Items, item)
		}
	}
}

// Phase 2: BreakLines determines what items go on each line, accounting for floats.
// This is where retry happens - if floats change available width, we re-break affected lines.
//
// Returns true if line breaking succeeded, false if retry is needed.
// NOTE: This is the OLD WIP implementation. New code should use BreakLines() instead.
func (le *LayoutEngine) breakLinesWIP(state *InlineLayoutState) bool {
	if len(state.Items) == 0 {
		return true // Nothing to break
	}

	state.Lines = nil // Clear any previous line breaking results
	currentY := state.StartY
	itemIndex := 0

	for itemIndex < len(state.Items) {
		// Start a new line
		line := &LineBreakResult{
			Y:          currentY,
			Items:      []*InlineItem{},
			StartIndex: itemIndex,
			TextBreaks: make(map[*InlineItem]struct {
				StartOffset int
				EndOffset   int
			}),
		}

		// Calculate available width for this line (accounting for floats)
		leftOffset, rightOffset := le.getFloatOffsets(currentY)
		line.AvailableWidth = state.AvailableWidth - leftOffset - rightOffset

		// Accumulate items on this line
		lineX := 0.0
		lineHeight := 0.0

		for itemIndex < len(state.Items) {
			item := state.Items[itemIndex]

			// Calculate item width
			itemWidth := 0.0
			itemHeight := 0.0

			switch item.Type {
			case InlineItemText:
				// For text, we might need to break it
				itemWidth = item.Width
				itemHeight = item.Height

				// Check if text fits on current line
				if lineX+itemWidth > line.AvailableWidth && len(line.Items) > 0 {
					// Text doesn't fit - need to break
					// For now, simple algorithm: break entire text to next line
					// TODO: Implement proper word breaking within text
					goto finishLine
				}

			case InlineItemOpenTag:
				// Opening tag contributes to line height even if element is empty
				// This is per CSS 2.1: empty inline elements still influence line height
				// CSS 2.1 §8.3: Left margin/border/padding at inline box start
				itemWidth = item.Width

				// CSS 2.1 §10.8.1: For inline boxes, line box height is determined by 'line-height'
				// Padding and borders render visually but DON'T affect line box height calculation
				lineHeightValue := item.Style.GetLineHeight()
				itemHeight = lineHeightValue

			case InlineItemCloseTag:
				// Closing tag doesn't add height (already accounted for in opening tag)
				// CSS 2.1 §8.3: Right margin/border/padding at inline box end
				itemWidth = item.Width
				itemHeight = 0

			case InlineItemAtomic, InlineItemFloat:
				// Atomic items have their own width/height
				itemWidth = item.Width
				itemHeight = item.Height

				if lineX+itemWidth > line.AvailableWidth && len(line.Items) > 0 {
					// Atomic item doesn't fit
					goto finishLine
				}

			case InlineItemBlockChild:
				// Block children force line breaks before and after
				// If we have items on current line, finish it first
				if len(line.Items) > 0 {
					goto finishLine
				}
				// Add block child as sole item on its own line
				line.Items = append(line.Items, item)
				itemIndex++
				goto finishLine

			case InlineItemControl:
				// Control items (like <br>) force a line break
				itemIndex++
				goto finishLine
			}

			// Add item to line
			line.Items = append(line.Items, item)
			lineX += itemWidth
			if itemHeight > lineHeight {
				lineHeight = itemHeight
			}

			itemIndex++
		}

	finishLine:
		// Finalize this line
		line.EndIndex = itemIndex
		line.LineHeight = lineHeight
		if line.LineHeight == 0 {
			// Use container's line-height as minimum
			line.LineHeight = state.ContainerStyle.GetLineHeight()
		}

		state.Lines = append(state.Lines, line)

		// Move to next line
		currentY += line.LineHeight

		// If we didn't make progress, either shift down past floats or force item
		if itemIndex == line.StartIndex && itemIndex < len(state.Items) {
			// CSS 2.1 §9.5: "If a shortened line box is too small to contain any
			// content, then the line box is shifted downward until either some
			// content fits or there are no more floats present."
			leftOff, rightOff := le.getFloatOffsets(currentY)
			if leftOff > 0 || rightOff > 0 {
				// Line is narrowed by floats - find next Y below the nearest float
				nextY := currentY
				for i := le.floatBase; i < len(le.floats); i++ {
					floatInfo := le.floats[i]
					floatBottom := floatInfo.Y + le.getTotalHeight(floatInfo.Box)
					if floatBottom > currentY && (nextY == currentY || floatBottom < nextY) {
						nextY = floatBottom
					}
				}
				if nextY > currentY {
					// Shift line down and retry
					currentY = nextY
					// Remove the empty line we just added and create a new one
					state.Lines = state.Lines[:len(state.Lines)-1]
					continue
				}
			}
			// No floats to clear - force include at least one item to avoid infinite loop
			item := state.Items[itemIndex]
			line.Items = append(line.Items, item)
			line.EndIndex = itemIndex + 1
			itemIndex++
		}
	}

	return true // Line breaking succeeded
}

// Phase 3: ConstructLineBoxes creates actual positioned Box fragments from line breaking results.
// This is the final phase that produces the output fragment tree.
func (le *LayoutEngine) ConstructLineBoxes(state *InlineLayoutState, parent *Box) []*Box {
	boxes := []*Box{}

	for _, line := range state.Lines {
		// Calculate starting X for this line (accounting for floats)
		leftOffset, _ := le.getFloatOffsets(line.Y)
		currentX := state.ContainerBox.X + state.Border.Left + state.Padding.Left + leftOffset

		// Track open inline elements (for nested inline styling)
		type inlineContext struct {
			node               *html.Node
			style              *css.Style
			box                *Box
			fragmentStartX     float64  // Where current fragment starts
			fragmentStartY     float64
			fragmentMaxX       float64 // Bounding box of current fragment
			fragmentMaxY       float64
			completedFragments []*Box // Completed fragments (before blocks)
		}
		openInlines := []inlineContext{}

		// Reorder items: floats first, then everything else (CSS-correct)
		reorderedItems := make([]*InlineItem, 0, len(line.Items))
		nonFloats := make([]*InlineItem, 0, len(line.Items))

		for _, item := range line.Items {
			if item.Type == InlineItemFloat {
				reorderedItems = append(reorderedItems, item)
			} else {
				nonFloats = append(nonFloats, item)
			}
		}
		reorderedItems = append(reorderedItems, nonFloats...)

		// Process each item on this line
		for _, item := range reorderedItems {
			switch item.Type {
			case InlineItemText:
				// Create a text box
				textBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, textBox)
				currentX += item.Width

				// Update fragment bounds for all open inline elements
				for i := range openInlines {
					if currentX > openInlines[i].fragmentMaxX {
						openInlines[i].fragmentMaxX = currentX
					}
					if line.Y+line.LineHeight > openInlines[i].fragmentMaxY {
						openInlines[i].fragmentMaxY = line.Y + line.LineHeight
					}
				}

			case InlineItemOpenTag:
				// Start tracking this inline element
				// Create a box for it (will be sized after seeing all children)
				padding := item.Style.GetPadding()
				border := item.Style.GetBorderWidth()
				margin := item.Style.GetMargin()

				// CSS 2.1 §10.8.1: Inline element vertical margins/padding don't affect line box height
				// but padding/borders DO render visually extending beyond the line box

				// Inline elements ignore vertical margins (CSS 2.1 §10.6.1)
				margin.Top = 0
				margin.Bottom = 0

				// Box height is the line box height (CSS 2.1 §10.8.1)
				// Borders/padding "bleed" outside this and are drawn separately by the render phase
				inlineBoxHeight := line.LineHeight

			// Apply left margin BEFORE positioning the box
			currentX += margin.Left

				inlineBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    0, // Will be computed from children
					Height:   inlineBoxHeight,
					Margin:   margin, // Inline elements have horizontal margins
					Padding:  padding,
					Border:   border,
					Position: css.PositionStatic,
					Parent:   parent,
				}
				// Initialize fragment tracking
				fragStartX := currentX + border.Left + padding.Left
				openInlines = append(openInlines, inlineContext{
					node:           item.Node,
					style:          item.Style,
					box:            inlineBox,
					fragmentStartX: fragStartX,
					fragmentStartY: line.Y,
					fragmentMaxX:   fragStartX,
					fragmentMaxY:   line.Y + inlineBoxHeight,
				})

				// Advance currentX by left border + padding (margin already applied above)
				// This ensures empty inline elements have proper width
				currentX += border.Left + padding.Left

			case InlineItemCloseTag:
				// Close the most recent inline element
				if len(openInlines) > 0 {
					ctx := openInlines[len(openInlines)-1]
					openInlines = openInlines[:len(openInlines)-1]

					// Add right padding + border (NOT margin) before computing width
					currentX += ctx.box.Padding.Right + ctx.box.Border.Right

					// Compute width from current X - start X
					ctx.box.Width = currentX - ctx.box.X
					// If this inline was split by block children, ctx.box is the final fragment
					if len(ctx.completedFragments) > 0 {
						ctx.box.IsLastFragment = true
					}
					boxes = append(boxes, ctx.box)

				// Now add right margin for positioning next element
				currentX += ctx.box.Margin.Right
				}

			case InlineItemAtomic:
				// Atomic inline element - it has its own dimensions
				atomicBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, atomicBox)
				currentX += item.Width

			case InlineItemFloat:
				// Floats are positioned separately by float logic
				// We don't position them here
				// TODO: Integrate with existing float positioning
			}
		}
	}

	return boxes
}

// constructLineBoxesWithRetry is like ConstructLineBoxes but also detects when floats
// change available width and signals that retry is needed.
// Returns (boxes, retryNeeded)
func (le *LayoutEngine) constructLineBoxesWithRetry(
	state *InlineLayoutState,
	parent *Box,
	computedStyles map[*html.Node]*css.Style,
) ([]*Box, bool) {
	boxes := []*Box{}
	retryNeeded := false

	for _, line := range state.Lines {
		// Calculate starting X for this line (accounting for floats)
		leftOffsetBefore, _ := le.getFloatOffsets(line.Y)
		currentX := state.ContainerBox.X + state.Border.Left + state.Padding.Left + leftOffsetBefore

		// Track open inline elements
		type inlineContext struct {
			node               *html.Node
			style              *css.Style
			box                *Box
			fragmentStartX     float64  // Where current fragment starts
			fragmentStartY     float64
			fragmentMaxX       float64 // Bounding box of current fragment
			fragmentMaxY       float64
			completedFragments []*Box // Completed fragments (before blocks)
		}
		openInlines := []inlineContext{}

		// Reorder items: floats first, then everything else (CSS-correct)
		reorderedItems := make([]*InlineItem, 0, len(line.Items))
		nonFloats := make([]*InlineItem, 0, len(line.Items))

		for _, item := range line.Items {
			if item.Type == InlineItemFloat {
				reorderedItems = append(reorderedItems, item)
			} else {
				nonFloats = append(nonFloats, item)
			}
		}
		reorderedItems = append(reorderedItems, nonFloats...)

		// Process each item on this line
		// Track if we've seen content (non-float) on this line yet
		hasSeenContentOnLine := false
		for _, item := range reorderedItems {
			switch item.Type {
			case InlineItemText:
				// CSS whitespace collapsing: trim leading whitespace at start of line
				// (after line breaks, leading spaces should be trimmed)
				trimmedText := item.Text
				if !hasSeenContentOnLine && item.Node != nil {
					trimmedText = strings.TrimLeft(item.Text, " \t\n\r")
					// Update the node's text for rendering
					if trimmedText != item.Text {
						item.Node.Text = trimmedText
						// Recalculate width for trimmed text
						if item.Style != nil {
							fontSize := item.Style.GetFontSize()
							bold := item.Style.GetFontWeight() == css.FontWeightBold
							italic := item.Style.GetFontStyle() == css.FontStyleItalic
							mono := item.Style.IsMonospaceFamily()
							ahem := item.Style.IsAhemFamily()
							trimmedWidth, _ := text.MeasureTextWithStyle(trimmedText, fontSize, bold, italic, mono, ahem)
							ls := item.Style.GetLetterSpacing()
							if ls != 0 && len([]rune(trimmedText)) > 1 {
								trimmedWidth += ls * float64(len([]rune(trimmedText))-1)
							}
							item.Width = trimmedWidth
						}
					}
				}
				hasSeenContentOnLine = true

				textBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    item.Width,
					Height:   item.Height,
					Margin:   css.BoxEdge{},
					Padding:  css.BoxEdge{},
					Border:   css.BoxEdge{},
					Position: css.PositionStatic,
					Parent:   parent,
				}
				boxes = append(boxes, textBox)
				currentX += item.Width

				// Update fragment bounds for all open inline elements
				for i := range openInlines {
					if currentX > openInlines[i].fragmentMaxX {
						openInlines[i].fragmentMaxX = currentX
					}
					if line.Y+line.LineHeight > openInlines[i].fragmentMaxY {
						openInlines[i].fragmentMaxY = line.Y + line.LineHeight
					}
				}

			case InlineItemOpenTag:
				padding := item.Style.GetPadding()
				border := item.Style.GetBorderWidth()
				margin := item.Style.GetMargin()

				// CSS 2.1 §10.8.1: Inline element vertical margins/padding don't affect line box height
				// but padding/borders DO render visually extending beyond the line box

				// Inline elements ignore vertical margins (CSS 2.1 §10.6.1)
				margin.Top = 0
				margin.Bottom = 0

				// Box height is the line box height (CSS 2.1 §10.8.1)
				// Borders/padding "bleed" outside this and are drawn separately by the render phase
				inlineBoxHeight := line.LineHeight

			// Apply left margin BEFORE positioning the box
			currentX += margin.Left

				inlineBox := &Box{
					Node:     item.Node,
					Style:    item.Style,
					X:        currentX,
					Y:        line.Y,
					Width:    0,
					Height:   inlineBoxHeight,
					Margin:   margin, // Inline elements have horizontal margins
					Padding:  padding,
					Border:   border,
					Position: css.PositionStatic,
					Parent:   parent,
				}
				// Initialize fragment tracking
				fragStartX := currentX + border.Left + padding.Left
				openInlines = append(openInlines, inlineContext{
					node:           item.Node,
					style:          item.Style,
					box:            inlineBox,
					fragmentStartX: fragStartX,
					fragmentStartY: line.Y,
					fragmentMaxX:   fragStartX,
					fragmentMaxY:   line.Y + inlineBoxHeight,
				})

				// Advance currentX by left border + padding (margin already applied above)
				// This ensures empty inline elements have proper width
				currentX += border.Left + padding.Left

			case InlineItemCloseTag:
				if len(openInlines) > 0 {
					ctx := openInlines[len(openInlines)-1]
					openInlines = openInlines[:len(openInlines)-1]

					// Add right padding + border (NOT margin) before computing width
					currentX += ctx.box.Padding.Right + ctx.box.Border.Right

					ctx.box.Width = currentX - ctx.box.X
					// If this inline was split by block children, ctx.box is the final fragment
					if len(ctx.completedFragments) > 0 {
						ctx.box.IsLastFragment = true
					}
					boxes = append(boxes, ctx.box)

				// Now add right margin for positioning next element
				currentX += ctx.box.Margin.Right
				}

			case InlineItemAtomic:
				// Atomic inline (inline-block) - recursively layout its content
				// Use the pre-computed width as the available width for its children
				atomicBox := le.layoutNode(
					item.Node,
					currentX,
					line.Y,
					item.Width, // Use computed width as constraint
					computedStyles,
					parent,
				)
				if atomicBox != nil {
					// Apply vertical alignment to inline-block
					// For baseline alignment, the inline-block's baseline (last line box's baseline)
					// should align with the parent line's baseline
					le.applyVerticalAlign(atomicBox, line.Y, line.LineHeight)

					boxes = append(boxes, atomicBox)
					// Use actual width (might include margins/padding/borders)
					actualWidth := le.getTotalWidth(atomicBox)
					currentX += actualWidth
				}

			case InlineItemBlockChild:
				// Block-in-inline: Block children split inline elements into fragments (CSS 2.1 §9.2.1.1)
				// STEP 1: Complete current fragments for ALL open inline elements
				for i := range openInlines {
					ctx := &openInlines[i]

					// Create fragment box for content before the block
					if ctx.fragmentMaxX > ctx.fragmentStartX {
						fragmentBox := &Box{
							Node:            ctx.node,
							Style:           ctx.style,
							X:               ctx.fragmentStartX - ctx.box.Border.Left - ctx.box.Padding.Left,
							Y:               ctx.fragmentStartY,
							Width:           ctx.fragmentMaxX - ctx.fragmentStartX + ctx.box.Border.Left + ctx.box.Border.Right + ctx.box.Padding.Left + ctx.box.Padding.Right,
							Height:          ctx.fragmentMaxY - ctx.fragmentStartY,
							Margin:          css.BoxEdge{}, // Fragments don't have margins
							Padding:         ctx.box.Padding,
							Border:          ctx.box.Border,
							Position:        css.PositionStatic,
							Parent:          parent,
							IsFirstFragment: len(ctx.completedFragments) == 0, // First fragment if no previous fragments
							IsLastFragment:  false,                            // Not last - more content after block
						}
						ctx.completedFragments = append(ctx.completedFragments, fragmentBox)
					}
				}

				// STEP 2: Layout the block child
				blockBox := le.layoutNode(
					item.Node,
					state.ContainerBox.X+state.Border.Left+state.Padding.Left,
					line.Y,
					state.AvailableWidth,
					computedStyles,
					parent,
				)
				if blockBox != nil {
					boxes = append(boxes, blockBox)
				}

				// STEP 3: Restart fragments for open inline elements (content after block)
				// Note: New fragments will start on the next line, which will be processed in next iteration
				for i := range openInlines {
					ctx := &openInlines[i]
					// Fragment bounds will be set when we process the next line's content
					ctx.fragmentStartX = 0
					ctx.fragmentStartY = 0
					ctx.fragmentMaxX = 0
					ctx.fragmentMaxY = 0
				}

			case InlineItemFloat:
				// Check if this float has already been laid out (to avoid duplicate layouts on retry)
				var existingFloatBox *Box
				for i := state.FloatBaseIndex; i < len(le.floats); i++ {
					if le.floats[i].Box != nil && le.floats[i].Box.Node == item.Node {
						existingFloatBox = le.floats[i].Box
						break
					}
				}

				// If float already exists, skip re-layout and continue
				if existingFloatBox != nil {
					boxes = append(boxes, existingFloatBox)
					continue
				}

				// Layout the float to get its dimensions
				// Track float count before layoutNode (layoutNode may add float as side effect)
				floatCountBefore := len(le.floats)

				floatBox := le.layoutNode(
					item.Node,
					state.ContainerBox.X+state.Border.Left+state.Padding.Left,
					line.Y,
					state.AvailableWidth,
					computedStyles,
					parent,
				)

				if floatBox != nil {
					// Remove any floats added during layoutNode (float seeing itself bug)
					if len(le.floats) > floatCountBefore {
						le.floats = le.floats[:floatCountBefore]
					}

					// Get float type and reposition the box correctly
					floatType := item.Style.GetFloat()
					floatWidth := le.getTotalWidth(floatBox)
					floatY := line.Y

					// CSS 2.1 §9.5.2: Apply clear property — move float below previous floats
					if item.Style != nil {
						clearType := item.Style.GetClear()
						if clearType != css.ClearNone {
							floatY = le.getClearY(clearType, floatY)
						}
					}

					// IMPORTANT: Get fresh float offsets BEFORE positioning this float
					// Don't use leftOffsetBefore which was captured at start of line
					leftOffset, rightOffset := le.getFloatOffsets(floatY)

					// Calculate correct position based on float type
					var newX float64
					if floatType == css.FloatLeft {
						// For left floats, position must clear both other floats (leftOffset) AND inline content (currentX)
						baseX := state.ContainerBox.X + state.Border.Left + state.Padding.Left
						floatClearX := baseX + leftOffset + floatBox.Margin.Left
						inlineEndX := currentX + floatBox.Margin.Left
						if inlineEndX > floatClearX {
							newX = inlineEndX
						} else {
							newX = floatClearX
						}
					} else {
						// Right float
						baseX := state.ContainerBox.X + state.Border.Left + state.Padding.Left
						newX = baseX + state.AvailableWidth - rightOffset - floatWidth + floatBox.Margin.Left
					}
					newY := floatY + floatBox.Margin.Top

					// Calculate position delta to reposition the float and its children
					deltaX := newX - floatBox.X
					deltaY := newY - floatBox.Y

					// Reposition child boxes
					for _, child := range floatBox.Children {
						child.X += deltaX
						child.Y += deltaY
					}

					floatBox.X = newX
					floatBox.Y = newY

					boxes = append(boxes, floatBox)

					// Add float to engine's float list
					le.addFloat(floatBox, floatType, floatY)

					// Update currentX to account for the float we just added
					// (subsequent inline content must clear the float)
					if floatType == css.FloatLeft {
						leftOffsetNew, _ := le.getFloatOffsets(line.Y)
						baseX := state.ContainerBox.X + state.Border.Left + state.Padding.Left
						newCurX := baseX + leftOffsetNew
						if newCurX > currentX {
							currentX = newCurX
						}
					}
					// Check if this float changes available width for this line
					leftOffsetAfter, _ := le.getFloatOffsets(line.Y)
					if leftOffsetAfter != leftOffsetBefore {
						// Float changed available width - retry needed
						retryNeeded = true
					}
				}
			}
		}
	}

	return boxes, retryNeeded
}
