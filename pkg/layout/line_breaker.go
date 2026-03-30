package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/text"
	"strings"
	"unicode"
)

// LineBreakerMode controls line breaking behavior.
// Ported from Blink's LineBreakerMode.
type LineBreakerMode int

const (
	// LineBreakerContent performs normal line breaking for layout.
	LineBreakerContent LineBreakerMode = iota
	// LineBreakerMinContent breaks at every opportunity (for min-content sizing).
	LineBreakerMinContent
	// LineBreakerMaxContent never wraps (for max-content sizing).
	LineBreakerMaxContent
)

// InlineItemResult is the measured output of line breaking for one item
// within a line. Transient — produced by LineBreaker, consumed by
// InlineLayoutAlgorithm.
//
// Ported from Blink's InlineItemResult (inline_item_result.h).
type InlineItemResult struct {
	// Item is the source InlineItem.
	Item *InlineItem
	// ItemIndex is the index into InlineItemsData.Items.
	ItemIndex int
	// TextStart and TextEnd are the portion of text used (may be a
	// sub-range of the InlineItem if broken mid-item).
	TextStart int
	TextEnd   int
	// InlineSize is the measured inline size of this result.
	InlineSize float64
	// LayoutResult is set for atomic inlines (inline-block, replaced).
	LayoutResult *LayoutResult
	// Margins holds the inline margins for open/close tags.
	Margins LogicalEdges
	// CanBreakAfter indicates a valid break opportunity after this item.
	CanBreakAfter bool
}

// LineInfo represents a complete line produced by the LineBreaker.
// Ported from Blink's LineInfo (line_info.h).
type LineInfo struct {
	// Results are the item results making up this line.
	Results []InlineItemResult
	// Width is the actual used width of the line content.
	Width float64
	// AvailableWidth is the space available for this line.
	AvailableWidth float64
	// TextAlign is the computed text-align for this line.
	TextAlign string
	// BaseDirection is the paragraph's base direction.
	BaseDirection Direction
	// IsLastLine is true if this is the final line.
	IsLastLine bool
	// HasForcedBreak is true if the line ends with a forced break (BR, newline).
	HasForcedBreak bool
}

// LineBreaker consumes an InlineItemsData and produces lines one at a time.
// Each call to NextLine() fills a LineInfo with the items for one line.
//
// Ported from Blink's LineBreaker (line_breaker.h).
type LineBreaker struct {
	itemsData      *InlineItemsData
	ctx            *LayoutContext
	space          ConstraintSpace
	fonts          text.FontConfig
	mode           LineBreakerMode
	availableWidth float64

	// Current position in the item list.
	currentItemIndex int
	// Current byte offset in TextContent.
	currentTextOffset int
	// Current inline position on the line being built.
	position float64
	// Whether we've finished all items.
	done bool
}

// NewLineBreaker creates a line breaker for the given inline content.
func NewLineBreaker(
	itemsData *InlineItemsData,
	ctx *LayoutContext,
	space ConstraintSpace,
	fonts text.FontConfig,
	mode LineBreakerMode,
) *LineBreaker {
	availableWidth := space.AvailableSize.InlineSize
	if mode == LineBreakerMaxContent {
		availableWidth = 1e9 // effectively unlimited
	}

	return &LineBreaker{
		itemsData:      itemsData,
		ctx:            ctx,
		space:          space,
		fonts:          fonts,
		mode:           mode,
		availableWidth: availableWidth,
	}
}

// NextLine produces the next line of content. Returns false when all
// content has been consumed.
func (lb *LineBreaker) NextLine(line *LineInfo) bool {
	if lb.done {
		return false
	}

	line.Results = line.Results[:0]
	line.Width = 0
	line.AvailableWidth = lb.availableWidth
	line.HasForcedBreak = false
	line.IsLastLine = false
	lb.position = 0

	// Process items until the line is full or we run out.
	for lb.currentItemIndex < len(lb.itemsData.Items) {
		item := lb.itemsData.Items[lb.currentItemIndex]

		switch item.Type {
		case InlineItemText:
			if lb.handleText(item, line) {
				// Line is complete (overflow handled).
				lb.finishLine(line)
				return true
			}
		case InlineItemControl:
			lb.handleControl(item, line)
			lb.finishLine(line)
			return true
		case InlineItemOpenTag:
			lb.handleOpenTag(item, line)
		case InlineItemCloseTag:
			lb.handleCloseTag(item, line)
		case InlineItemAtomicInline:
			if lb.handleAtomicInline(item, line) {
				lb.finishLine(line)
				return true
			}
		case InlineItemFloat:
			lb.handleFloat(item, line)
		}

		lb.currentItemIndex++
	}

	// All items consumed.
	lb.done = true
	if len(line.Results) > 0 || line.Width > 0 {
		line.IsLastLine = true
		lb.finishLine(line)
		return true
	}
	return false
}

// handleText measures text and handles line breaking within text items.
// Returns true if the line should end after this item.
func (lb *LineBreaker) handleText(item *InlineItem, line *LineInfo) bool {
	textStart := item.StartOffset
	if lb.currentTextOffset > textStart {
		textStart = lb.currentTextOffset
	}
	textEnd := item.EndOffset
	content := lb.itemsData.TextContent[textStart:textEnd]

	if len(content) == 0 {
		return false
	}

	// Get font properties from style.
	fontSize, bold, italic, mono, ahem := fontPropsFromStyle(item.Style)

	// Measure the full text segment.
	fullWidth, _ := text.MeasureTextWithStyle(content, fontSize, bold, italic, mono, ahem)

	// Check if it fits.
	remaining := lb.availableWidth - lb.position
	if fullWidth <= remaining || lb.mode == LineBreakerMaxContent {
		// Fits — add the full text.
		line.Results = append(line.Results, InlineItemResult{
			Item:       item,
			ItemIndex:  lb.currentItemIndex,
			TextStart:  textStart,
			TextEnd:    textEnd,
			InlineSize: fullWidth,
		})
		lb.position += fullWidth
		line.Width = lb.position
		lb.currentTextOffset = textEnd
		return false
	}

	// Doesn't fit — find a break point.
	if lb.mode == LineBreakerMinContent {
		// Break at every word boundary.
		return lb.breakTextAtWord(item, content, textStart, textEnd, fontSize, bold, italic, mono, ahem, line, 0)
	}

	// Normal mode: find where to break.
	return lb.breakTextAtWord(item, content, textStart, textEnd, fontSize, bold, italic, mono, ahem, line, remaining)
}

// breakTextAtWord finds a break point within a text item.
// Returns true if the line should end.
func (lb *LineBreaker) breakTextAtWord(
	item *InlineItem,
	content string,
	textStart, textEnd int,
	fontSize float64,
	bold, italic, mono, ahem bool,
	line *LineInfo,
	remaining float64,
) bool {
	// Find word boundaries.
	words := splitIntoWords(content)
	if len(words) == 0 {
		return false
	}

	// Try to fit as many words as possible.
	fitted := 0
	usedWidth := 0.0

	for i, word := range words {
		wordWidth, _ := text.MeasureTextWithStyle(word, fontSize, bold, italic, mono, ahem)

		if lb.mode == LineBreakerMinContent && i > 0 {
			// Min-content: break after every word.
			break
		}

		if usedWidth+wordWidth > remaining && fitted > 0 {
			break
		}

		usedWidth += wordWidth
		fitted++

		if lb.mode == LineBreakerMinContent {
			break
		}
	}

	if fitted == 0 {
		// Can't fit even the first word. If the line is empty, force it on.
		if len(line.Results) == 0 {
			fitted = 1
			usedWidth, _ = text.MeasureTextWithStyle(words[0], fontSize, bold, italic, mono, ahem)
		} else {
			// End the line, retry this item on the next line.
			return true
		}
	}

	// Compute the byte offset for the break point.
	fittedText := strings.Join(words[:fitted], "")
	breakOffset := textStart + len(fittedText)

	line.Results = append(line.Results, InlineItemResult{
		Item:       item,
		ItemIndex:  lb.currentItemIndex,
		TextStart:  textStart,
		TextEnd:    breakOffset,
		InlineSize: usedWidth,
	})
	lb.position += usedWidth
	line.Width = lb.position

	if fitted < len(words) {
		// More text remains — line is complete.
		lb.currentTextOffset = breakOffset
		return true
	}

	// All words fit.
	lb.currentTextOffset = textEnd
	return false
}

// handleControl handles control characters (forced line breaks).
func (lb *LineBreaker) handleControl(item *InlineItem, line *LineInfo) {
	line.HasForcedBreak = true
	lb.currentItemIndex++
	lb.currentTextOffset = item.EndOffset
}

// handleOpenTag processes the start of an inline element.
func (lb *LineBreaker) handleOpenTag(item *InlineItem, line *LineInfo) {
	if item.Style == nil {
		return
	}

	wdm := lb.space.WritingDirection
	margins := ResolveMargins(item.Style, wdm, lb.availableWidth)
	geom := ComputeFragmentGeometry(item.Style, wdm)

	// Add inline-start margin + border + padding.
	startEdge := margins.InlineStart + geom.Border.InlineStart + geom.Padding.InlineStart
	lb.position += startEdge

	line.Results = append(line.Results, InlineItemResult{
		Item:       item,
		ItemIndex:  lb.currentItemIndex,
		InlineSize: startEdge,
		Margins:    margins,
	})
}

// handleCloseTag processes the end of an inline element.
func (lb *LineBreaker) handleCloseTag(item *InlineItem, line *LineInfo) {
	if item.Style == nil {
		return
	}

	wdm := lb.space.WritingDirection
	margins := ResolveMargins(item.Style, wdm, lb.availableWidth)
	geom := ComputeFragmentGeometry(item.Style, wdm)

	// Add inline-end border + padding + margin.
	endEdge := geom.Border.InlineEnd + geom.Padding.InlineEnd + margins.InlineEnd
	lb.position += endEdge

	line.Results = append(line.Results, InlineItemResult{
		Item:       item,
		ItemIndex:  lb.currentItemIndex,
		InlineSize: endEdge,
		Margins:    margins,
	})
}

// handleAtomicInline lays out an atomic inline element (inline-block, replaced).
// Returns true if the line should end.
func (lb *LineBreaker) handleAtomicInline(item *InlineItem, line *LineInfo) bool {
	// Layout the atomic inline.
	childWDM := NewWritingDirectionMode(item.Style)
	childSpace := NewConstraintSpaceBuilder(lb.space.WritingDirection, childWDM, true).
		SetAvailableSize(LogicalSize{
			InlineSize: lb.availableWidth,
			BlockSize:  Indefinite,
		}).
		Build()

	result := layoutElement(lb.ctx, item.Node, childSpace)
	childLogical := NewLogicalFragment(lb.space.WritingDirection, result.Fragment)
	inlineSize := childLogical.InlineSize()

	// Check if it fits.
	remaining := lb.availableWidth - lb.position
	if inlineSize > remaining && len(line.Results) > 0 && lb.mode == LineBreakerContent {
		// Doesn't fit and line has content — end the line.
		return true
	}

	line.Results = append(line.Results, InlineItemResult{
		Item:         item,
		ItemIndex:    lb.currentItemIndex,
		TextStart:    item.StartOffset,
		TextEnd:      item.EndOffset,
		InlineSize:   inlineSize,
		LayoutResult: result,
	})
	lb.position += inlineSize
	line.Width = lb.position
	lb.currentTextOffset = item.EndOffset
	return false
}

// handleFloat records a float item. Float positioning is handled by the
// InlineLayoutAlgorithm, not the line breaker.
func (lb *LineBreaker) handleFloat(item *InlineItem, line *LineInfo) {
	line.Results = append(line.Results, InlineItemResult{
		Item:      item,
		ItemIndex: lb.currentItemIndex,
	})
}

// finishLine applies trailing whitespace trimming and sets final line properties.
func (lb *LineBreaker) finishLine(line *LineInfo) {
	// Trim trailing whitespace from the last text result.
	for i := len(line.Results) - 1; i >= 0; i-- {
		r := &line.Results[i]
		if r.Item.Type == InlineItemText {
			content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
			trimmed := strings.TrimRightFunc(content, unicode.IsSpace)
			if len(trimmed) < len(content) && r.Item.Style != nil {
				fontSize, bold, italic, mono, ahem := fontPropsFromStyle(r.Item.Style)
				newWidth, _ := text.MeasureTextWithStyle(trimmed, fontSize, bold, italic, mono, ahem)
				line.Width -= (r.InlineSize - newWidth)
				r.InlineSize = newWidth
				r.TextEnd = r.TextStart + len(trimmed)
			}
			break
		}
		if r.Item.Type != InlineItemCloseTag && r.Item.Type != InlineItemOpenTag {
			break
		}
	}

	// Mark as last line if we've consumed all items.
	if lb.currentItemIndex >= len(lb.itemsData.Items) {
		line.IsLastLine = true
	}
}

// splitIntoWords splits text into words, preserving spaces attached to
// the following word (for correct break-before-word behavior).
func splitIntoWords(s string) []string {
	var words []string
	start := 0
	inSpace := false

	for i, r := range s {
		if unicode.IsSpace(r) {
			if !inSpace && i > start {
				words = append(words, s[start:i])
			}
			if !inSpace {
				start = i
			}
			inSpace = true
		} else {
			if inSpace {
				// Space run ends — attach spaces to the next word.
				// Actually, keep space with preceding word for correct measurement.
				if start < i {
					if len(words) > 0 {
						words[len(words)-1] += s[start:i]
					} else {
						// Leading spaces — make a separate entry.
						words = append(words, s[start:i])
					}
				}
				start = i
			}
			inSpace = false
		}
	}

	// Trailing content.
	if start < len(s) {
		if inSpace {
			if len(words) > 0 {
				words[len(words)-1] += s[start:]
			} else {
				words = append(words, s[start:])
			}
		} else {
			words = append(words, s[start:])
		}
	}

	return words
}

// fontPropsFromStyle extracts font rendering properties from a CSS style.
func fontPropsFromStyle(style *css.Style) (fontSize float64, bold, italic, mono, ahem bool) {
	if style == nil {
		return 16, false, false, false, false
	}
	fontSize = style.GetFontSize()
	if fontSize <= 0 {
		fontSize = 16
	}
	bold = style.GetFontWeight() == css.FontWeightBold
	italic = style.GetFontStyle() == css.FontStyleItalic
	mono = style.IsMonospaceFamily()
	ahem = style.IsAhemFamily()
	return
}
