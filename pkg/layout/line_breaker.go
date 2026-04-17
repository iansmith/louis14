package layout

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"louis14/pkg/css"
	"louis14/pkg/text"
)

// isCSSCollapsibleSpace returns true for whitespace characters that CSS
// considers collapsible (CSS 2.1 §16.6.1). U+00A0 (non-breaking space) is
// explicitly excluded — it is never collapsed or stripped.
func isCSSCollapsibleSpace(r rune) bool {
	return r != '\u00A0' && unicode.IsSpace(r)
}

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
	// HasHyphen indicates the text was broken at a soft hyphen (U+00AD) or
	// auto-hyphenation point and a visible "-" should be appended at the end.
	HasHyphen bool
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
	line.BaseDirection = lb.space.WritingDirection.Dir
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
		case InlineItemOutOfFlow:
			// OOF items don't participate in line layout. Record them with
			// zero advance so their static inline position is captured.
			line.Results = append(line.Results, InlineItemResult{
				Item:      item,
				TextStart: item.StartOffset,
				TextEnd:   item.StartOffset,
			})
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

// measureTextContent measures text width, stripping soft hyphens (U+00AD)
// which are zero-width when not at a break point.
func measureTextContent(content string, fontSize float64, fontPath string, letterSpacing, wordSpacing float64, isVertical bool) float64 {
	// Strip soft hyphens for measurement — they are invisible when not breaking.
	visible := strings.ReplaceAll(content, "\u00AD", "")
	if len(visible) == 0 {
		return 0
	}
	var w float64
	if isVertical {
		w, _ = text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
	} else {
		w, _ = text.MeasureText(visible, fontSize, fontPath)
	}
	if letterSpacing != 0 {
		rc := runeLen(visible)
		if rc > 1 {
			w += letterSpacing * float64(rc-1)
		}
	}
	if wordSpacing != 0 {
		w += wordSpacing * float64(strings.Count(visible, " "))
	}
	return w
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

	// CSS 2.1 §16.6.1: strip leading collapsible whitespace at the start of a line.
	if lb.position == 0 && len(line.Results) == 0 {
		trimmed := strings.TrimLeftFunc(content, isCSSCollapsibleSpace)
		if len(trimmed) < len(content) {
			textStart += len(content) - len(trimmed)
			content = trimmed
			if len(content) == 0 {
				lb.currentTextOffset = textEnd
				return false
			}
		}
	}

	// Get font properties from style.
	fontSize, _, _, _, _ := fontPropsFromStyle(item.Style)
	fontPath := resolveFontPath(item.Style, lb.fonts)

	// CSS 2.1 §16.4: letter-spacing adds inter-character space.
	letterSpacing := 0.0
	if item.Style != nil {
		letterSpacing = item.Style.GetLetterSpacing()
	}

	// CSS 2.1 §16.4: word-spacing adds extra space between words.
	wordSpacing := 0.0
	if item.Style != nil {
		wordSpacing = item.Style.GetWordSpacing()
	}

	// Measure the full text segment. In vertical writing modes, use vertical
	// measurement where each upright glyph advances by fontSize.
	isVertical := lb.space.WritingDirection.IsVertical() && !lb.space.WritingDirection.IsSideways()
	var fullWidth float64

	// CSS Text 3 §4.2: tab characters advance to the next tab stop.
	// Tab stops are at multiples of (tab-size × space-width) or (tab-size px).
	hasSoftHyphen := strings.Contains(content, "\u00AD")
	if strings.Contains(content, "\t") && item.Style != nil {
		fullWidth = lb.measureTextWithTabs(content, fontSize, fontPath, letterSpacing, wordSpacing, isVertical)
	} else if hasSoftHyphen {
		// Soft hyphens are zero-width — measure without them.
		fullWidth = measureTextContent(content, fontSize, fontPath, letterSpacing, wordSpacing, isVertical)
	} else {
		if isVertical {
			fullWidth, _ = text.MeasureTextVerticalFromFont(content, fontSize, fontPath)
		} else {
			fullWidth, _ = text.MeasureText(content, fontSize, fontPath)
		}
		if letterSpacing != 0 {
			runeCount := runeLen(content)
			if runeCount > 1 {
				fullWidth += letterSpacing * float64(runeCount-1)
			}
		}
		if wordSpacing != 0 {
			fullWidth += wordSpacing * float64(strings.Count(content, " "))
		}
	}

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

	// CSS 2.1 §16.6.1: trailing whitespace is always stripped at end-of-line.
	// If the text with trailing whitespace doesn't fit but the text without does,
	// treat it as fitting. finishLine will strip the trailing whitespace anyway.
	if lb.mode == LineBreakerContent {
		stripped := strings.TrimRightFunc(content, isCSSCollapsibleSpace)
		if len(stripped) < len(content) {
			var strippedWidth float64
			if hasSoftHyphen {
				strippedWidth = measureTextContent(stripped, fontSize, fontPath, letterSpacing, wordSpacing, isVertical)
			} else {
				if isVertical {
					strippedWidth, _ = text.MeasureTextVerticalFromFont(stripped, fontSize, fontPath)
				} else {
					strippedWidth, _ = text.MeasureText(stripped, fontSize, fontPath)
				}
				if letterSpacing != 0 {
					rc := runeLen(stripped)
					if rc > 1 {
						strippedWidth += letterSpacing * float64(rc-1)
					}
				}
			}
			if strippedWidth <= remaining {
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
		}
	}

	// Read word-break and overflow-wrap properties.
	wordBreak := "normal"
	overflowWrap := "normal"
	hyphens := "manual"
	if item.Style != nil {
		wordBreak = item.Style.GetWordBreak()
		overflowWrap = item.Style.GetOverflowWrap()
		hyphens = item.Style.GetHyphens()
	}

	// Doesn't fit — find a break point.
	if lb.mode == LineBreakerMinContent {
		// overflow-wrap:anywhere affects min-content sizing: treat every
		// character as a potential break point (CSS Text 3 §4.1).
		if overflowWrap == "anywhere" || wordBreak == "break-all" {
			return lb.breakTextAtCharacter(item, content, textStart, textEnd, fontSize, fontPath, line, 0)
		}
		// Break at every word boundary.
		return lb.breakTextAtWord(item, content, textStart, textEnd, fontSize, fontPath, line, 0, wordBreak, overflowWrap, hyphens)
	}

	// word-break:break-all — break between any two characters.
	if wordBreak == "break-all" {
		return lb.breakTextAtCharacter(item, content, textStart, textEnd, fontSize, fontPath, line, remaining)
	}

	// Normal mode: find where to break.
	return lb.breakTextAtWord(item, content, textStart, textEnd, fontSize, fontPath, line, remaining, wordBreak, overflowWrap, hyphens)
}

// breakTextAtWord finds a break point within a text item.
// Returns true if the line should end.
func (lb *LineBreaker) breakTextAtWord(
	item *InlineItem,
	content string,
	textStart, textEnd int,
	fontSize float64,
	fontPath string,
	line *LineInfo,
	remaining float64,
	wordBreak, overflowWrap, hyphens string,
) bool {
	// Find word boundaries. Pass hyphens mode so soft hyphens are treated
	// as break opportunities (manual/auto) or ignored (none).
	words := splitIntoWords(content)
	if len(words) == 0 {
		return false
	}

	// CSS 2.1 §16.4: letter-spacing.
	letterSpacing := 0.0
	if item.Style != nil {
		letterSpacing = item.Style.GetLetterSpacing()
	}

	// Try to fit as many words as possible.
	fitted := 0
	usedWidth := 0.0

	// If remaining space is effectively zero and the line already has content,
	// don't even try to fit the first word — end the line immediately.
	// This prevents a single-word text item from being force-added to an
	// already-full line (the fitted>0 guard only applies to subsequent words).
	if remaining <= 0 && len(line.Results) > 0 {
		return true
	}

	// CSS 2.1 §16.4: word-spacing adds extra space between words.
	wordSpacing := 0.0
	if item.Style != nil {
		wordSpacing = item.Style.GetWordSpacing()
	}

	isVertical := lb.space.WritingDirection.IsVertical() && !lb.space.WritingDirection.IsSideways()

	// measureWord measures a word, stripping soft hyphens (zero-width).
	measureWord := func(word string) float64 {
		visible := strings.ReplaceAll(word, "\u00AD", "")
		if len(visible) == 0 {
			return 0
		}
		var w float64
		if isVertical {
			w, _ = text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
		} else {
			w, _ = text.MeasureText(visible, fontSize, fontPath)
		}
		if letterSpacing != 0 {
			rc := runeLen(visible)
			if rc > 1 {
				w += letterSpacing * float64(rc-1)
			}
		}
		if wordSpacing != 0 {
			w += wordSpacing * float64(strings.Count(visible, " "))
		}
		return w
	}

	// Measure the visible hyphen character width (for soft-hyphen breaks).
	var hyphenWidth float64
	hasSoftHyphen := strings.Contains(content, "\u00AD")
	if hasSoftHyphen && hyphens != "none" {
		if isVertical {
			hyphenWidth, _ = text.MeasureTextVerticalFromFont("-", fontSize, fontPath)
		} else {
			hyphenWidth, _ = text.MeasureText("-", fontSize, fontPath)
		}
	}

	// CSS Text 3 §5.2: hyphens:auto — find auto-hyphenation break points.
	// Only attempt when hyphens is "auto" and no soft hyphens are present.
	if hyphens == "auto" && !hasSoftHyphen && remaining > 0 {
		if result := lb.tryAutoHyphenation(item, content, textStart, textEnd, fontSize, fontPath, line, remaining, letterSpacing, wordSpacing); result != nil {
			return *result
		}
	}

	// If hyphens:manual or hyphens:auto with soft hyphens present,
	// split at soft-hyphen positions as break opportunities.
	if hasSoftHyphen && hyphens != "none" {
		return lb.breakTextAtSoftHyphen(item, content, textStart, textEnd, fontSize, fontPath, line, remaining, hyphenWidth, letterSpacing, wordSpacing, overflowWrap)
	}

	for i, word := range words {
		wordWidth := measureWord(word)
		if letterSpacing != 0 && i > 0 {
			wordWidth += letterSpacing
		}

		if lb.mode == LineBreakerMinContent && i > 0 {
			// Min-content: break after every word.
			break
		}

		if usedWidth+wordWidth > remaining && fitted > 0 {
			// CSS 2.1 §16.6.1: trailing collapsible whitespace hangs and does
			// not contribute to the line's width. If stripping trailing spaces
			// from this word makes it fit, include it — it would be the last
			// word on the line, so its trailing space will be stripped.
			trimmed := strings.TrimRightFunc(word, isCSSCollapsibleSpace)
			if trimmed != word && trimmed != "" {
				trimmedWidth := measureWord(trimmed)
				if letterSpacing != 0 && i > 0 {
					trimmedWidth += letterSpacing
				}
				if usedWidth+trimmedWidth <= remaining {
					// Fits without trailing whitespace — include it.
					usedWidth += wordWidth
					fitted++
					break // but line is full, stop fitting more words
				}
			}
			break
		}

		usedWidth += wordWidth
		fitted++

		if lb.mode == LineBreakerMinContent {
			break
		}
	}

	if fitted == 0 {
		// Can't fit even the first word.
		if len(line.Results) == 0 {
			// overflow-wrap:break-word or anywhere — break the word at a
			// character boundary so it fits the available width (CSS Text 3 §4.1).
			if overflowWrap == "break-word" || overflowWrap == "anywhere" {
				return lb.breakTextAtCharacter(item, content, textStart, textEnd, fontSize, fontPath, line, remaining)
			}
			// Force the whole word onto the empty line.
			fitted = 1
			usedWidth = measureWord(words[0])
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

// breakTextAtSoftHyphen handles line breaking at soft hyphen (U+00AD) positions.
// CSS Text 3 §5.2: soft hyphens are break opportunities that display a visible
// hyphen when used as a break point.
func (lb *LineBreaker) breakTextAtSoftHyphen(
	item *InlineItem,
	content string,
	textStart, textEnd int,
	fontSize float64,
	fontPath string,
	line *LineInfo,
	remaining float64,
	hyphenWidth float64,
	letterSpacing, wordSpacing float64,
	overflowWrap string,
) bool {
	isVertical := lb.space.WritingDirection.IsVertical() && !lb.space.WritingDirection.IsSideways()

	// Find all soft-hyphen positions and regular word boundaries.
	// We need to try breaking at each soft-hyphen position (and at
	// regular word boundaries) to find the best fit.
	var breaks []breakPoint

	// Collect break points: regular word boundaries and soft-hyphen positions.
	// Regular word boundaries from splitIntoWords:
	wordsNoShy := splitIntoWords(strings.ReplaceAll(content, "\u00AD", ""))
	_ = wordsNoShy // we handle regular word breaks implicitly

	// Find all soft-hyphen byte positions.
	for i := 0; i < len(content); {
		r, size := utf8.DecodeRuneInString(content[i:])
		if r == '\u00AD' {
			breaks = append(breaks, breakPoint{byteOffset: i + size, isSoftHyph: true})
		}
		i += size
	}

	// Also add regular word boundaries (spaces).
	inSpace := false
	wordEnd := 0
	for i, r := range content {
		if r == '\u00AD' {
			continue
		}
		if isCSSCollapsibleSpace(r) {
			if !inSpace && i > 0 {
				// End of a word — this is a break opportunity.
				breaks = append(breaks, breakPoint{byteOffset: i, isSoftHyph: false})
			}
			inSpace = true
		} else {
			if inSpace {
				wordEnd = i
				_ = wordEnd
			}
			inSpace = false
		}
	}

	// Sort breaks by byte offset.
	sortBreakPoints(breaks)

	// Try each break point, finding the last one that fits.
	bestBreak := -1
	bestIsShy := false
	bestWidth := 0.0

	for _, bp := range breaks {
		segment := content[:bp.byteOffset]
		// Strip soft hyphens from measurement.
		visible := strings.ReplaceAll(segment, "\u00AD", "")
		var segWidth float64
		if isVertical {
			segWidth, _ = text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
		} else {
			segWidth, _ = text.MeasureText(visible, fontSize, fontPath)
		}
		if letterSpacing != 0 {
			rc := runeLen(visible)
			if rc > 1 {
				segWidth += letterSpacing * float64(rc-1)
			}
		}
		if wordSpacing != 0 {
			segWidth += wordSpacing * float64(strings.Count(visible, " "))
		}

		totalWidth := segWidth
		if bp.isSoftHyph {
			totalWidth += hyphenWidth
		}

		if totalWidth <= remaining {
			bestBreak = bp.byteOffset
			bestIsShy = bp.isSoftHyph
			bestWidth = segWidth
		} else {
			// Past remaining — stop searching.
			break
		}
	}

	if bestBreak <= 0 {
		// No soft-hyphen break fits. Fall back to normal word breaking
		// with soft hyphens stripped, or overflow-wrap.
		if len(line.Results) == 0 {
			if overflowWrap == "break-word" || overflowWrap == "anywhere" {
				return lb.breakTextAtCharacter(item, content, textStart, textEnd, fontSize, fontPath, line, remaining)
			}
			// Force content onto the line — use the first break point.
			if len(breaks) > 0 {
				bp := breaks[0]
				segment := content[:bp.byteOffset]
				visible := strings.ReplaceAll(segment, "\u00AD", "")
				var segWidth float64
				if isVertical {
					segWidth, _ = text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
				} else {
					segWidth, _ = text.MeasureText(visible, fontSize, fontPath)
				}
				if letterSpacing != 0 {
					rc := runeLen(visible)
					if rc > 1 {
						segWidth += letterSpacing * float64(rc-1)
					}
				}
				totalWidth := segWidth
				if bp.isSoftHyph {
					totalWidth += hyphenWidth
				}
				line.Results = append(line.Results, InlineItemResult{
					Item:       item,
					ItemIndex:  lb.currentItemIndex,
					TextStart:  textStart,
					TextEnd:    textStart + bp.byteOffset,
					InlineSize: totalWidth,
					HasHyphen:  bp.isSoftHyph,
				})
				lb.position += totalWidth
				line.Width = lb.position
				lb.currentTextOffset = textStart + bp.byteOffset
				return true
			}
			// No break points at all — force entire content.
			fullWidth := measureTextContent(content, fontSize, fontPath, letterSpacing, wordSpacing, isVertical)
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
		return true
	}

	// Break at bestBreak.
	totalWidth := bestWidth
	if bestIsShy {
		totalWidth += hyphenWidth
	}

	line.Results = append(line.Results, InlineItemResult{
		Item:       item,
		ItemIndex:  lb.currentItemIndex,
		TextStart:  textStart,
		TextEnd:    textStart + bestBreak,
		InlineSize: totalWidth,
		HasHyphen:  bestIsShy,
	})
	lb.position += totalWidth
	line.Width = lb.position
	lb.currentTextOffset = textStart + bestBreak

	if bestBreak < len(content) {
		return true
	}
	return false
}

// sortBreakPoints sorts break points by byte offset (insertion sort, small slices).
func sortBreakPoints(bps []breakPoint) {
	for i := 1; i < len(bps); i++ {
		key := bps[i]
		j := i - 1
		for j >= 0 && bps[j].byteOffset > key.byteOffset {
			bps[j+1] = bps[j]
			j--
		}
		bps[j+1] = key
	}
}

// breakPoint is a candidate line break position within text.
type breakPoint struct {
	byteOffset int  // offset in content where to break
	isSoftHyph bool // true if this break is at a soft hyphen
}

// tryAutoHyphenation attempts heuristic hyphenation for English text.
// CSS Text 3 §5.2: hyphens:auto — the UA may hyphenate words automatically.
// Returns nil if no auto-hyphenation was applied (caller should continue
// with normal word breaking), or a pointer to a bool result.
func (lb *LineBreaker) tryAutoHyphenation(
	item *InlineItem,
	content string,
	textStart, textEnd int,
	fontSize float64,
	fontPath string,
	line *LineInfo,
	remaining float64,
	letterSpacing, wordSpacing float64,
) *bool {
	isVertical := lb.space.WritingDirection.IsVertical() && !lb.space.WritingDirection.IsSideways()

	// Measure the visible hyphen.
	var hyphenWidth float64
	if isVertical {
		hyphenWidth, _ = text.MeasureTextVerticalFromFont("-", fontSize, fontPath)
	} else {
		hyphenWidth, _ = text.MeasureText("-", fontSize, fontPath)
	}

	// Split into words and try to hyphenate the word that doesn't fit.
	words := splitIntoWords(content)
	if len(words) == 0 {
		return nil
	}

	// Measure words to find which one overflows.
	usedWidth := 0.0
	fittedWords := 0
	overflowIdx := -1
	for i, word := range words {
		var w float64
		if isVertical {
			w, _ = text.MeasureTextVerticalFromFont(word, fontSize, fontPath)
		} else {
			w, _ = text.MeasureText(word, fontSize, fontPath)
		}
		if letterSpacing != 0 {
			rc := runeLen(word)
			if rc > 1 {
				w += letterSpacing * float64(rc-1)
			}
			if i > 0 {
				w += letterSpacing
			}
		}
		if wordSpacing != 0 {
			w += wordSpacing * float64(strings.Count(word, " "))
		}
		if usedWidth+w > remaining {
			overflowIdx = i
			break
		}
		usedWidth += w
		fittedWords++
	}

	if overflowIdx < 0 {
		return nil // everything fits, no hyphenation needed
	}

	// Try to hyphenate the overflowing word.
	word := words[overflowIdx]
	// Strip trailing whitespace for hyphenation analysis.
	cleanWord := strings.TrimRightFunc(word, isCSSCollapsibleSpace)
	hyphPoints := findAutoHyphenPoints(cleanWord)
	if len(hyphPoints) == 0 {
		return nil // no hyphenation points found
	}

	// Remaining space for the overflowing word.
	wordRemaining := remaining - usedWidth

	// Try each hyphenation point (they are rune offsets into cleanWord).
	bestRuneOffset := -1
	bestWidth := 0.0
	runes := []rune(cleanWord)
	for _, runeOff := range hyphPoints {
		prefix := string(runes[:runeOff])
		var prefixWidth float64
		if isVertical {
			prefixWidth, _ = text.MeasureTextVerticalFromFont(prefix, fontSize, fontPath)
		} else {
			prefixWidth, _ = text.MeasureText(prefix, fontSize, fontPath)
		}
		if letterSpacing != 0 {
			rc := utf8.RuneCountInString(prefix)
			if rc > 1 {
				prefixWidth += letterSpacing * float64(rc-1)
			}
			if overflowIdx > 0 {
				prefixWidth += letterSpacing
			}
		}
		totalWidth := prefixWidth + hyphenWidth
		if totalWidth <= wordRemaining {
			bestRuneOffset = runeOff
			bestWidth = prefixWidth
		} else {
			break
		}
	}

	if bestRuneOffset < 0 {
		return nil // no hyphenation point fits
	}

	// Build the break: all fitted words + prefix of the overflowing word.
	prefix := string(runes[:bestRuneOffset])
	prefixBytes := len(prefix)

	// Compute byte offset of the break in content.
	var bytesBefore int
	for i := 0; i < overflowIdx; i++ {
		bytesBefore += len(words[i])
	}
	breakOffset := textStart + bytesBefore + prefixBytes
	totalInlineSize := usedWidth + bestWidth + hyphenWidth

	result := true
	line.Results = append(line.Results, InlineItemResult{
		Item:       item,
		ItemIndex:  lb.currentItemIndex,
		TextStart:  textStart,
		TextEnd:    breakOffset,
		InlineSize: totalInlineSize,
		HasHyphen:  true,
	})
	lb.position += totalInlineSize
	line.Width = lb.position
	lb.currentTextOffset = breakOffset
	return &result
}

// findAutoHyphenPoints returns rune offsets where a word may be hyphenated.
// Uses simple English heuristics: common suffixes and double-consonant splits.
// Minimum 2 chars before break, 3 chars after.
func findAutoHyphenPoints(word string) []int {
	runes := []rune(strings.ToLower(word))
	n := len(runes)
	if n < 5 { // need at least 2+3
		return nil
	}

	isConsonant := func(r rune) bool {
		switch r {
		case 'b', 'c', 'd', 'f', 'g', 'h', 'j', 'k', 'l', 'm',
			'n', 'p', 'q', 'r', 's', 't', 'v', 'w', 'x', 'y', 'z':
			return true
		}
		return false
	}

	var points []int
	seen := make(map[int]bool)

	addPoint := func(pos int) {
		if pos >= 2 && pos <= n-3 && !seen[pos] {
			points = append(points, pos)
			seen[pos] = true
		}
	}

	// Common suffix patterns: break before the suffix.
	suffixes := []string{"tion", "sion", "ment", "ness", "ing", "ble", "ful", "less", "ous", "ive", "ize", "ise", "ent", "ant", "ance", "ence"}
	wordLower := string(runes)
	for _, suf := range suffixes {
		sufRunes := []rune(suf)
		pos := n - len(sufRunes)
		if pos >= 2 && strings.HasSuffix(wordLower, suf) {
			addPoint(pos)
		}
	}

	// Double consonants: break between them.
	for i := 2; i < n-3; i++ {
		if isConsonant(runes[i]) && runes[i] == runes[i+1] {
			addPoint(i + 1)
		}
	}

	// Sort points.
	for i := 1; i < len(points); i++ {
		key := points[i]
		j := i - 1
		for j >= 0 && points[j] > key {
			points[j+1] = points[j]
			j--
		}
		points[j+1] = key
	}

	return points
}

// breakTextAtCharacter breaks text at character boundaries.
// Used for word-break:break-all and overflow-wrap:break-word/anywhere.
// Returns true if the line should end.
func (lb *LineBreaker) breakTextAtCharacter(
	item *InlineItem,
	content string,
	textStart, textEnd int,
	fontSize float64,
	fontPath string,
	line *LineInfo,
	remaining float64,
) bool {
	if len(content) == 0 {
		return false
	}

	letterSpacing := 0.0
	if item.Style != nil {
		letterSpacing = item.Style.GetLetterSpacing()
	}

	isVertical := lb.space.WritingDirection.IsVertical() && !lb.space.WritingDirection.IsSideways()

	// Fit as many characters as possible within the remaining width.
	fitted := 0
	usedWidth := 0.0

	// If remaining space is effectively zero and the line already has content,
	// end the line immediately.
	if remaining <= 0 && len(line.Results) > 0 {
		return true
	}

	byteOffset := 0
	for i, r := range content {
		charStr := string(r)
		var charWidth float64
		if isVertical {
			charWidth, _ = text.MeasureTextVerticalFromFont(charStr, fontSize, fontPath)
		} else {
			charWidth, _ = text.MeasureText(charStr, fontSize, fontPath)
		}
		// Add letter-spacing between characters (not before the first).
		if letterSpacing != 0 && fitted > 0 {
			charWidth += letterSpacing
		}

		if usedWidth+charWidth > remaining && fitted > 0 {
			break
		}

		usedWidth += charWidth
		fitted++
		byteOffset = i + utf8.RuneLen(r)

		// In min-content mode, break after every character.
		if lb.mode == LineBreakerMinContent && fitted > 0 {
			break
		}
	}

	if fitted == 0 {
		if len(line.Results) == 0 {
			// Force at least one character on an empty line.
			r, size := utf8.DecodeRuneInString(content)
			charStr := string(r)
			if isVertical {
				usedWidth, _ = text.MeasureTextVerticalFromFont(charStr, fontSize, fontPath)
			} else {
				usedWidth, _ = text.MeasureText(charStr, fontSize, fontPath)
			}
			fitted = 1
			byteOffset = size
		} else {
			return true
		}
	}

	breakOffset := textStart + byteOffset

	line.Results = append(line.Results, InlineItemResult{
		Item:       item,
		ItemIndex:  lb.currentItemIndex,
		TextStart:  textStart,
		TextEnd:    breakOffset,
		InlineSize: usedWidth,
	})
	lb.position += usedWidth
	line.Width = lb.position

	if breakOffset < textEnd {
		// More text remains — line is complete.
		lb.currentTextOffset = breakOffset
		return true
	}

	// All characters fit.
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

	// CSS 2.1 §9.2.1.1: inline-start border/padding appears only on the first
	// fragment of a split inline. For non-first continuations, omit it.
	borderInlineStart := geom.Border.InlineStart
	paddingInlineStart := geom.Padding.InlineStart
	if !item.IsFirstFragment {
		borderInlineStart = 0
		paddingInlineStart = 0
	}

	// Add inline-start margin + border + padding.
	startEdge := margins.InlineStart + borderInlineStart + paddingInlineStart
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

	// CSS 2.1 §9.2.1.1: inline-end border/padding appears only on the last
	// fragment of a split inline. For non-last continuations, omit it.
	borderInlineEnd := geom.Border.InlineEnd
	paddingInlineEnd := geom.Padding.InlineEnd
	if !item.IsLastFragment {
		borderInlineEnd = 0
		paddingInlineEnd = 0
	}

	// Add inline-end border + padding + margin.
	endEdge := borderInlineEnd + paddingInlineEnd + margins.InlineEnd
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
	// Resolve margins for the atomic inline.
	margins := LogicalEdges{}
	if item.Style != nil {
		margins = ResolveMargins(item.Style, lb.space.WritingDirection, lb.availableWidth)
	}

	var inlineSize float64
	var result *LayoutResult

	childWDM := NewWritingDirectionMode(item.Style)

	if lb.mode == LineBreakerMinContent || lb.mode == LineBreakerMaxContent {
		// In sizing mode: compute the child's intrinsic size instead of
		// doing full layout. Mirrors Blink's recursive min/max sizing.
		csBuilder := NewConstraintSpaceBuilder(lb.space.WritingDirection, childWDM, true).
			SetOrthogonalFallbackInlineSize(
				orthogonalFallbackSize(childWDM, lb.ctx)).
			SetOrthogonalFallbackBlockSize(
				lb.space.OrthogonalFallbackBlockSize).
			SetAvailableSize(lb.space.AvailableSize).
			SetPercentageResolutionInlineSize(lb.space.PercentageResolutionInlineSize)
		// Propagate percentage resolution block-size from parent so that
		// percentage-height replaced elements (e.g., img { height: 100% })
		// can resolve against their containing block's definite height.
		if lb.space.PercentageResolutionSize.BlockSize > 0 {
			csBuilder.SetPercentageResolutionSize(lb.space.PercentageResolutionSize)
		}
		childSpace := csBuilder.Build()
		childMM := ComputeMinMaxSizes(lb.ctx, item.LayoutNode, childSpace)
		// ComputeMinMaxSizes returns content-box; convert to border-box
		// for the atomic inline's contribution to the line.
		childGeom := ComputeFragmentGeometry(item.Style, childWDM)
		childBP := childGeom.InlineBorderPadding()
		if lb.mode == LineBreakerMinContent {
			inlineSize = childMM.MinContent + childBP
		} else {
			inlineSize = childMM.MaxContent + childBP
		}
	} else {
		// Normal layout: lay out the atomic inline with shrink-to-fit.
		// Propagate the percentage resolution block-size from the parent's space
		// so that percentage-height children can resolve.
		availBlock := Indefinite
		if lb.space.PercentageResolutionSize.BlockSize > 0 {
			availBlock = lb.space.PercentageResolutionSize.BlockSize
		} else if lb.space.AvailableSize.BlockSize >= 0 {
			availBlock = lb.space.AvailableSize.BlockSize
		}
		csb := NewConstraintSpaceBuilder(lb.space.WritingDirection, childWDM, true).
			SetOrthogonalFallbackInlineSize(
				orthogonalFallbackSize(childWDM, lb.ctx)).
			SetOrthogonalFallbackBlockSize(
				lb.space.OrthogonalFallbackBlockSize).
			SetAvailableSize(LogicalSize{
				InlineSize: lb.availableWidth,
				BlockSize:  availBlock,
			}).
			SetPercentageResolutionInlineSize(lb.space.PercentageResolutionInlineSize)
		if lb.space.PercentageResolutionSize.BlockSize > 0 {
			csb.SetPercentageResolutionSize(lb.space.PercentageResolutionSize)
		}
		childSpace := csb.
			Build()

		result = layoutElement(lb.ctx, item.LayoutNode, childSpace)
		childLogical := NewLogicalFragment(lb.space.WritingDirection, result.Fragment)
		inlineSize = childLogical.InlineSize()
	}

	// Total inline size including margins.
	totalInlineSize := margins.InlineStart + inlineSize + margins.InlineEnd

	// Check if it fits.
	remaining := lb.availableWidth - lb.position
	// Break before the atom if it doesn't fit AND the line already has content.
	// In LineBreakerContent mode: normal wrap.
	// In LineBreakerMinContent mode: break at every opportunity so each atom
	// contributes independently to the min-content width.
	// In LineBreakerMaxContent mode: never break (place everything on one line).
	if totalInlineSize > remaining && len(line.Results) > 0 && lb.mode != LineBreakerMaxContent {
		// Doesn't fit and line has content — end the line.
		return true
	}

	// Add inline-start margin.
	lb.position += margins.InlineStart

	line.Results = append(line.Results, InlineItemResult{
		Item:         item,
		ItemIndex:    lb.currentItemIndex,
		TextStart:    item.StartOffset,
		TextEnd:      item.EndOffset,
		InlineSize:   inlineSize,
		LayoutResult: result,
		Margins:      margins,
	})
	lb.position += inlineSize

	// Add inline-end margin.
	lb.position += margins.InlineEnd
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
	isVertical := lb.space.WritingDirection.IsVertical() && !lb.space.WritingDirection.IsSideways()
	// CSS 2.1 §16.6.1: strip leading collapsible whitespace at the start of the line.
	for i := 0; i < len(line.Results); i++ {
		r := &line.Results[i]
		if r.Item.Type == InlineItemText {
			content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
			trimmed := strings.TrimLeftFunc(content, isCSSCollapsibleSpace)
			if len(trimmed) < len(content) && r.Item.Style != nil {
				fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
				fontPath := resolveFontPath(r.Item.Style, lb.fonts)
				var newWidth float64
				if isVertical {
					newWidth, _ = text.MeasureTextVerticalFromFont(trimmed, fontSize, fontPath)
				} else {
					newWidth, _ = text.MeasureText(trimmed, fontSize, fontPath)
				}
				line.Width -= (r.InlineSize - newWidth)
				r.InlineSize = newWidth
				r.TextStart = r.TextStart + (len(content) - len(trimmed))
			}
			break
		}
		if r.Item.Type != InlineItemOpenTag {
			break
		}
	}

	// Trim trailing whitespace from the last text result.
	// Skip over floats, OOF, open/close tags — they don't produce visible
	// inline content that would prevent trailing-whitespace trimming.
	for i := len(line.Results) - 1; i >= 0; i-- {
		r := &line.Results[i]
		if r.Item.Type == InlineItemText {
			content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
			trimmed := strings.TrimRightFunc(content, isCSSCollapsibleSpace)
			if len(trimmed) < len(content) && r.Item.Style != nil {
				fontSize, _, _, _, _ := fontPropsFromStyle(r.Item.Style)
				fontPath := resolveFontPath(r.Item.Style, lb.fonts)
				var newWidth float64
				if isVertical {
					newWidth, _ = text.MeasureTextVerticalFromFont(trimmed, fontSize, fontPath)
				} else {
					newWidth, _ = text.MeasureText(trimmed, fontSize, fontPath)
				}
				line.Width -= (r.InlineSize - newWidth)
				r.InlineSize = newWidth
				r.TextEnd = r.TextStart + len(trimmed)
			}
			break
		}
		// Continue past items that don't produce visible inline content.
		if r.Item.Type == InlineItemCloseTag || r.Item.Type == InlineItemOpenTag ||
			r.Item.Type == InlineItemFloat || r.Item.Type == InlineItemOutOfFlow {
			continue
		}
		break // Atomic inline or other content — stop searching.
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
		if isCSSCollapsibleSpace(r) {
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

// runeLen returns the number of Unicode code points in a string.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
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

// resolveFontPath returns the font file path for a given CSS style,
// using the font config to resolve the CSS font-family to a system font.
func resolveFontPath(style *css.Style, fonts text.FontConfig) string {
	if style == nil {
		return fonts.Regular
	}
	bold := style.GetFontWeight() == css.FontWeightBold
	italic := style.GetFontStyle() == css.FontStyleItalic
	mono := style.IsMonospaceFamily()
	ahem := style.IsAhemFamily()
	family, _ := style.Get("font-family")
	return fonts.FontPathForFamily(family, bold, italic, mono, ahem)
}

// measureTextWithTabs computes the inline size of text containing tab characters.
// CSS Text 3 §4.2: a tab character advances to the next tab stop, where tab
// stops are at multiples of (tab-size × space-width) or (tab-size in px) from
// the start of the line.
func (lb *LineBreaker) measureTextWithTabs(
	content string,
	fontSize float64,
	fontPath string,
	letterSpacing, wordSpacing float64,
	isVertical bool,
) float64 {
	// Compute tab stop interval in pixels.
	tabSizeVal := 8.0
	tabSizeIsLength := false
	if lb.itemsData.Items[lb.currentItemIndex].Style != nil {
		tabSizeVal, tabSizeIsLength = lb.itemsData.Items[lb.currentItemIndex].Style.GetTabSize()
	}

	var tabStopPx float64
	if tabSizeIsLength {
		tabStopPx = tabSizeVal
	} else {
		// tab-size is a character count: interval = N × advance-of-space.
		var spaceW float64
		if isVertical {
			spaceW, _ = text.MeasureTextVerticalFromFont(" ", fontSize, fontPath)
		} else {
			spaceW, _ = text.MeasureText(" ", fontSize, fontPath)
		}
		if spaceW <= 0 {
			spaceW = fontSize
		}
		tabStopPx = tabSizeVal * spaceW
	}
	if tabStopPx <= 0 {
		tabStopPx = fontSize * 8
	}

	// Track position from the start of the line (lb.position is the current
	// inline offset on this line).
	pos := lb.position
	segments := strings.Split(content, "\t")
	for i, seg := range segments {
		if len(seg) > 0 {
			var segW float64
			if isVertical {
				segW, _ = text.MeasureTextVerticalFromFont(seg, fontSize, fontPath)
			} else {
				segW, _ = text.MeasureText(seg, fontSize, fontPath)
			}
			if letterSpacing != 0 {
				rc := runeLen(seg)
				if rc > 1 {
					segW += letterSpacing * float64(rc-1)
				}
			}
			if wordSpacing != 0 {
				segW += wordSpacing * float64(strings.Count(seg, " "))
			}
			pos += segW
		}
		// After each segment except the last, advance to next tab stop.
		if i < len(segments)-1 {
			rem := math.Mod(pos, tabStopPx)
			if rem < 1e-9 {
				pos += tabStopPx
			} else {
				pos += tabStopPx - rem
			}
		}
	}
	// Return the width consumed by this text (difference from where we started).
	return pos - lb.position
}
