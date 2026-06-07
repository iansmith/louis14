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

// cssPreservesWhitespace returns true when the style's white-space value
// mandates that spaces at line start and end must not be stripped.
// CSS 2.1 §16.6.1: 'pre' and 'pre-wrap' preserve all whitespace verbatim.
// CSS Text 4: 'break-spaces' also preserves all spaces (including at line
// edges) — spaces always occupy width and are never discarded.
func cssPreservesWhitespace(style *css.Style) bool {
	if style == nil {
		return false
	}
	ws := style.GetWhiteSpace()
	return ws == css.WhiteSpacePre || ws == css.WhiteSpacePreWrap || ws == css.WhiteSpaceBreakSpaces
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
	// RubyColumn is non-nil when this result is a ruby column produced
	// by LineBreaker.handleRuby. Mirrors Blink's
	// InlineItemResult::RubyColumn
	// (`core/layout/inline/inline_item_result_ruby_column.h`).
	RubyColumn *InlineItemResultRubyColumn
	// Style is a per-line style override. When non-nil, it replaces
	// Item.Style for THIS line only (the underlying InlineItem is shared
	// across lines and must not be mutated). Set by applyFirstLineStyles
	// for items that land on the first formatted line, so that the same
	// InlineItem appearing on a later line still uses its original style.
	// Mirrors Blink's FirstLineStyleIterator
	// (`core/css/first_line_style_iterator.cc`) which yields a per-fragment
	// style without mutating the source LayoutObject style.
	Style *css.Style
}

// EffectiveStyle returns the style that applies to this result. The per-line
// Style override (set by ::first-line application) wins over Item.Style; this
// preserves the shared InlineItem's original style for use on other lines.
func (r *InlineItemResult) EffectiveStyle() *css.Style {
	if r.Style != nil {
		return r.Style
	}
	if r.Item != nil {
		return r.Item.Style
	}
	return nil
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
	// HasContent is true when at least one visible item (text or atomic inline)
	// has been committed to this line. Maintained incrementally by handleText,
	// breakTextAtWord, and handleAtomicInline so breakTextAtWord can test for
	// prior visible content in O(1) instead of scanning Results.
	// Mirrors Blink's LineInfo::HasContent().
	HasContent bool
	// TextAlignLastJustify is true when text-align was overridden to "justify"
	// by the text-align-last property. Unlike text-align: justify, which falls
	// back to start on the last line, text-align-last: justify expands even the
	// last line. This flag lets createLineBoxEx and computeTextAlignOffset
	// distinguish the two cases.
	// CSS Text 3 §9.7; mirrors Blink's distinction between kJustify (from
	// text-align) and text-align-last: justify at computedStyle level.
	TextAlignLastJustify bool

	// IsRubyBase is true if this LineInfo is the sub-line of a ruby
	// column's base. Set by LineBreaker.CreateSubLineInfo when building
	// the column payload in handleRuby. Mirrors Blink's
	// LineInfo::SetIsRubyBase (line_info.h).
	IsRubyBase bool
	// IsRubyText is true if this LineInfo is the sub-line of a ruby
	// column's annotation (`<rt>`). Set by LineBreaker.CreateSubLineInfo.
	// Mirrors Blink's LineInfo::SetIsRubyText.
	IsRubyText bool
	// RubyColumns is the set of ruby columns on this (top-level) line —
	// the InlineItemResultRubyColumn payloads emitted by handleRuby for
	// this line, in order. Consumed by UpdateRubyColumnInlinePositions
	// + RubyBlockPositionCalculator during inline layout.
	RubyColumns []*InlineItemResultRubyColumn
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

	// useFirstLineStyle, when true, makes the breaker measure text with the
	// ::first-line-merged style (firstLineStyle merged over each item's base
	// style) instead of the base style alone — so the first formatted line
	// breaks at the correct character count when ::first-line changes
	// font-size/letter-spacing/word-spacing. The caller sets this true before
	// breaking the first formatted line and false afterward, mirroring Blink's
	// `LineBreaker::use_first_line_style_` lifecycle (set in the ctor
	// `core/layout/inline/line_breaker.cc:467` when
	// `is_first_formatted_line_ && node.UseFirstLineStyleItemsData()`, cleared
	// in `PrepareNextLine` `:800` @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	// louis14 measures width on the fly, so the flag + on-the-fly font swap is
	// the faithful analog of Blink's pre-shaped first_line_items_.
	useFirstLineStyle bool
	// firstLineStyle is the ::first-line pseudo-element style to merge over the
	// base item style when useFirstLineStyle is set. Nil when the block has no
	// ::first-line rule.
	firstLineStyle *css.Style
	// firstLineMergeCache memoizes mergeFirstLineStyle results keyed by the
	// base item style, so the many measuring sites that call measuringStyle for
	// the same item during one line's breaking each reuse one clone+metric
	// resolution instead of repeating it. Only populated while
	// useFirstLineStyle is set (the first formatted line).
	firstLineMergeCache map[*css.Style]*css.Style
}

// measuringStyle returns the style the breaker should measure text with for an
// item carrying itemStyle. On the first formatted line (useFirstLineStyle set)
// it returns the ::first-line-merged style — the same merge that
// inline_layout.go applies to the line's metrics and paint, so breaking,
// metrics, and paint all agree on the first-line font. On every other line it
// returns the base item style unchanged. Mirrors Blink routing all first-line
// measurement through `items_data_(&node.ItemsData(use_first_line_style_))`.
func (lb *LineBreaker) measuringStyle(itemStyle *css.Style) *css.Style {
	if !lb.useFirstLineStyle || lb.firstLineStyle == nil {
		return itemStyle
	}
	if cached, ok := lb.firstLineMergeCache[itemStyle]; ok {
		return cached
	}
	merged := mergeFirstLineStyle(itemStyle, lb.firstLineStyle, lb.fonts)
	if lb.firstLineMergeCache == nil {
		lb.firstLineMergeCache = make(map[*css.Style]*css.Style)
	}
	lb.firstLineMergeCache[itemStyle] = merged
	return merged
}

// isVerticalMeasurement returns true when text in the given style should be
// measured using vertical font metrics (em advance per glyph). This applies
// only when text-orientation is "upright" — which renders all glyphs upright
// with each one advancing by one em in the inline direction.
//
// For writing-mode: vertical-rl/lr with the default text-orientation "mixed",
// Latin characters are rendered sideways and advance by their horizontal width.
// For "sideways" or "sideways-rl/lr" modes the same applies. Horizontal
// measurement (MeasureText) is correct in all non-upright cases.
//
// Mirrors CSS Writing Modes §7.5 / Blink's IsHorizontalTypographicMode().
func (lb *LineBreaker) isVerticalMeasurement(style *css.Style) bool {
	if !lb.space.WritingDirection.IsVertical() || lb.space.WritingDirection.IsSideways() {
		return false
	}
	// VRL/VLR: use em advance only for text-orientation: upright.
	if style == nil {
		return false
	}
	to, _ := style.Get("text-orientation")
	return to == "upright"
}

// NewLineBreaker creates a line breaker for the given inline content.
func NewLineBreaker(
	itemsData *InlineItemsData,
	ctx *LayoutContext,
	space ConstraintSpace,
	fonts text.FontConfig,
	mode LineBreakerMode,
) *LineBreaker {
	availableWidth := space.AvailableSize.InlineSize.Float64()
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
	line.HasContent = false
	line.IsLastLine = false
	line.TextAlignLastJustify = false
	line.BaseDirection = lb.space.WritingDirection.Dir
	// CSS Ruby Phase 2: reset the ruby-specific fields too so a
	// LineInfo reused across NextLine calls doesn't carry stale ruby
	// columns or sub-line role flags from the previous line.
	line.IsRubyBase = false
	line.IsRubyText = false
	line.RubyColumns = line.RubyColumns[:0]
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
		case InlineItemOpenRubyColumn:
			// CSS Ruby Phase 2: a ruby column is an atomic inline
			// unit. handleRuby builds the base + annotation
			// sub-LineInfos and emits a single InlineItemResult for
			// the column; advances currentItemIndex to the matching
			// CloseRubyColumn. Mirrors Blink
			// `line_breaker.cc:1082-1093` dispatch + `:3278-3449`
			// HandleRuby (@ 4883d11fef). See line_breaker_ruby.go.
			if lb.handleRuby(item, line) {
				lb.finishLine(line)
				return true
			}
		case InlineItemCloseRubyColumn, InlineItemRubyLinePlaceholder:
			// Zero-width markers. handleRuby consumes everything
			// between open/close in the outer pass; in a
			// sub-LineBreaker over a column sub-range they're
			// end-of-range no-ops. Mirrors
			// `line_breaker.cc:1059-1060`.
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
		mtLU, _ := text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
		w = mtLU.Float64()
	} else {
		mtLU, _ := text.MeasureText(visible, fontSize, fontPath)
		w = mtLU.Float64()
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
	// Skipped for white-space: pre / pre-wrap which preserve all whitespace.
	if lb.position == 0 && len(line.Results) == 0 && !cssPreservesWhitespace(item.Style) {
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

	// Get font properties from style. On the first formatted line this is the
	// ::first-line-merged style so the line breaks at the first-line font's
	// glyph advances (WI-1).
	measureStyle := lb.measuringStyle(item.Style)
	fontSize, _, _, _, _ := fontPropsFromStyle(measureStyle)
	fontPath := resolveFontPath(measureStyle, lb.fonts)

	// CSS 2.1 §16.4: letter-spacing adds inter-character space.
	letterSpacing := 0.0
	if measureStyle != nil {
		letterSpacing = measureStyle.GetLetterSpacing()
	}

	// CSS 2.1 §16.4: word-spacing adds extra space between words.
	wordSpacing := 0.0
	if measureStyle != nil {
		wordSpacing = measureStyle.GetWordSpacing()
	}

	// Measure the full text segment. In vertical writing modes, use vertical
	// measurement where each upright glyph advances by fontSize.
	isVertical := lb.isVerticalMeasurement(item.Style)
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
			mtLU, _ := text.MeasureTextVerticalFromFont(content, fontSize, fontPath)
			fullWidth = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(content, fontSize, fontPath)
			fullWidth = mtLU.Float64()
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
		line.HasContent = true
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
					mtLU, _ := text.MeasureTextVerticalFromFont(stripped, fontSize, fontPath)
					strippedWidth = mtLU.Float64()
				} else {
					mtLU, _ := text.MeasureText(stripped, fontSize, fontPath)
					strippedWidth = mtLU.Float64()
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
				line.HasContent = true
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

	// CSS 2.1 §16.4: letter-spacing. Use the ::first-line-merged style on the
	// first formatted line so spacing matches the breaking font (WI-1).
	measureStyle := lb.measuringStyle(item.Style)
	letterSpacing := 0.0
	if measureStyle != nil {
		letterSpacing = measureStyle.GetLetterSpacing()
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
	if measureStyle != nil {
		wordSpacing = measureStyle.GetWordSpacing()
	}

	isVertical := lb.isVerticalMeasurement(item.Style)

	// measureWord measures a word, stripping soft hyphens (zero-width).
	measureWord := func(word string) float64 {
		visible := strings.ReplaceAll(word, "\u00AD", "")
		if len(visible) == 0 {
			return 0
		}
		var w float64
		if isVertical {
			mtLU, _ := text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
			w = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(visible, fontSize, fontPath)
			w = mtLU.Float64()
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
			mtLU, _ := text.MeasureTextVerticalFromFont("-", fontSize, fontPath)
			hyphenWidth = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText("-", fontSize, fontPath)
			hyphenWidth = mtLU.Float64()
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

		// If this is the first word on an empty line and it overflows,
		// and overflow-wrap:break-word is set, break it at character boundaries.
		if fitted == 0 && len(line.Results) == 0 && usedWidth+wordWidth > remaining &&
			(overflowWrap == "break-word" || overflowWrap == "anywhere") {
			return lb.breakTextAtCharacter(item, content, textStart, textEnd, fontSize, fontPath, line, remaining)
		}

		if usedWidth+wordWidth > remaining && (fitted > 0 || line.HasContent) {
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
			// The break-word/anywhere first-word-overflow case is handled by the
			// early dispatch above (before the word is force-fitted), so only the
			// non-breaking overflow-wrap:normal case reaches here: force the whole
			// word onto the empty line (it overflows, per CSS Text 3 §4.1).
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
	line.HasContent = true
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
	isVertical := lb.isVerticalMeasurement(item.Style)

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
			mtLU, _ := text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
			segWidth = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(visible, fontSize, fontPath)
			segWidth = mtLU.Float64()
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
					mtLU, _ := text.MeasureTextVerticalFromFont(visible, fontSize, fontPath)
					segWidth = mtLU.Float64()
				} else {
					mtLU, _ := text.MeasureText(visible, fontSize, fontPath)
					segWidth = mtLU.Float64()
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
	isVertical := lb.isVerticalMeasurement(item.Style)

	// Measure the visible hyphen.
	var hyphenWidth float64
	if isVertical {
		mtLU, _ := text.MeasureTextVerticalFromFont("-", fontSize, fontPath)
		hyphenWidth = mtLU.Float64()
	} else {
		mtLU, _ := text.MeasureText("-", fontSize, fontPath)
		hyphenWidth = mtLU.Float64()
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
			mtLU, _ := text.MeasureTextVerticalFromFont(word, fontSize, fontPath)
			w = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(word, fontSize, fontPath)
			w = mtLU.Float64()
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
			mtLU, _ := text.MeasureTextVerticalFromFont(prefix, fontSize, fontPath)
			prefixWidth = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(prefix, fontSize, fontPath)
			prefixWidth = mtLU.Float64()
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

	// Use the ::first-line-merged style on the first formatted line so spacing
	// matches the breaking font (WI-1).
	letterSpacing := 0.0
	if measureStyle := lb.measuringStyle(item.Style); measureStyle != nil {
		letterSpacing = measureStyle.GetLetterSpacing()
	}

	isVertical := lb.isVerticalMeasurement(item.Style)

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
			mtLU, _ := text.MeasureTextVerticalFromFont(charStr, fontSize, fontPath)
			charWidth = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(charStr, fontSize, fontPath)
			charWidth = mtLU.Float64()
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
				mtLU, _ := text.MeasureTextVerticalFromFont(charStr, fontSize, fontPath)
				usedWidth = mtLU.Float64()
			} else {
				mtLU, _ := text.MeasureText(charStr, fontSize, fontPath)
				usedWidth = mtLU.Float64()
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
	line.HasContent = true
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
	// Include the control item in Results so that computeLineMetricsEx can
	// use its style (parent element's font) to compute the strut for blank
	// lines. CSS 2.1 §10.8: a line box starts with a zero-width strut whose
	// metrics match the block container's font and line-height.
	line.Results = append(line.Results, InlineItemResult{
		Item:      item,
		ItemIndex: lb.currentItemIndex,
		TextStart: item.StartOffset,
		TextEnd:   item.EndOffset,
	})
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

	// Update line.Width to reflect the cursor position after the close tag.
	// This ensures negative margin-end (e.g. margin-inline-end:-1px) on a
	// wrapper span reduces the reported line width, so max-content sizing
	// accounts for it correctly.  Without this, max-content stays at the
	// position of the last placed content item and ignores the close-tag
	// cursor adjustment — causing ShrinkToFit to overcount the item's
	// inline-size by the absolute value of the negative margin.
	//
	// Only update when lb.position > 0: a negative close-tag margin that
	// brings position below 0 (degenerate case) should not produce a
	// negative line.Width.
	if lb.position > 0 {
		line.Width = lb.position
	}

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
			SetAvailableSize(geomLogicalToOld(lb.space.AvailableSize)).
			SetPercentageResolutionInlineSize(lb.space.PercentageResolutionInlineSize)
		// Propagate percentage resolution block-size from parent so that
		// percentage-height replaced elements (e.g., img { height: 100% })
		// can resolve against their containing block's definite height.
		if lb.space.PercentageResolutionSize.BlockSize.Float64() > 0 {
			csBuilder.SetPercentageResolutionSize(geomLogicalToOld(lb.space.PercentageResolutionSize))
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
		if lb.space.PercentageResolutionSize.BlockSize.Float64() > 0 {
			availBlock = lb.space.PercentageResolutionSize.BlockSize.Float64()
		} else if lb.space.AvailableSize.BlockSize.Float64() >= 0 {
			availBlock = lb.space.AvailableSize.BlockSize.Float64()
		}
		// §10.3.2: For an atomic-inline orthogonal root, the available block-size
		// that becomes the child's (axis-swapped) inline-size must come from the
		// parent's own constraints — walked up to the nearest definite ancestor
		// or ICB — not from whatever block-size happened to flow down. Mirrors
		// the block-child path at block_layout.go:368-370 using the value
		// pre-computed in layoutInlineChildren.
		if lb.space.WritingDirection.IsOrthogonalTo(childWDM) && lb.space.OrthogonalAvailableBlock > 0 {
			availBlock = lb.space.OrthogonalAvailableBlock
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
		if lb.space.PercentageResolutionSize.BlockSize.Float64() > 0 {
			csb.SetPercentageResolutionSize(geomLogicalToOld(lb.space.PercentageResolutionSize))
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
	if totalInlineSize > remaining && line.HasContent && lb.mode != LineBreakerMaxContent {
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
	line.HasContent = true
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

// isNonVisibleInlineItem reports whether an item type represents a non-visible
// inline boundary (structural tag, float, OOF, or control) that should be
// skipped when scanning for text to strip leading or trailing collapsible whitespace.
func isNonVisibleInlineItem(t InlineItemType) bool {
	return t == InlineItemOpenTag || t == InlineItemCloseTag ||
		t == InlineItemFloat || t == InlineItemOutOfFlow ||
		t == InlineItemControl
}

// finishLine applies trailing whitespace trimming and sets final line properties.
func (lb *LineBreaker) finishLine(line *LineInfo) {
	// Determine measurement mode from the first text item's style (text-orientation
	// is inherited so all items on this line share the same value).
	var firstTextStyle *css.Style
	for i := range line.Results {
		if line.Results[i].Item.Type == InlineItemText && line.Results[i].Item.Style != nil {
			firstTextStyle = line.Results[i].Item.Style
			break
		}
	}
	isVertical := lb.isVerticalMeasurement(firstTextStyle)
	// CSS 2.1 §16.6.1: strip leading collapsible whitespace at the start of the line.
	// Skip past floats, CloseTags, OpenTags, and OOF items to find the first actual
	// text — a line that begins with a float still strips leading whitespace from
	// the text that follows it (e.g., " B" after a float:right on a new column).
	// Skipped for white-space: pre / pre-wrap which preserve all whitespace.
	for i := 0; i < len(line.Results); i++ {
		r := &line.Results[i]
		if r.Item.Type == InlineItemText {
			if cssPreservesWhitespace(r.Item.Style) {
				break
			}
			content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
			trimmed := strings.TrimLeftFunc(content, isCSSCollapsibleSpace)
			if len(trimmed) < len(content) && r.Item.Style != nil {
				// Re-measure with the same font handleText used for the
				// initial InlineSize so the width delta stays consistent —
				// the ::first-line-merged font on the first line (WI-1).
				measureStyle := lb.measuringStyle(r.Item.Style)
				fontSize, _, _, _, _ := fontPropsFromStyle(measureStyle)
				fontPath := resolveFontPath(measureStyle, lb.fonts)
				var newWidth float64
				if isVertical {
					mtLU, _ := text.MeasureTextVerticalFromFont(trimmed, fontSize, fontPath)
					newWidth = mtLU.Float64()
				} else {
					mtLU, _ := text.MeasureText(trimmed, fontSize, fontPath)
					newWidth = mtLU.Float64()
				}
				line.Width -= (r.InlineSize - newWidth)
				r.InlineSize = newWidth
				r.TextStart = r.TextStart + (len(content) - len(trimmed))
			}
			break
		}
		if isNonVisibleInlineItem(r.Item.Type) {
			continue
		}
		break
	}

	// Trim trailing whitespace from the last text result.
	// Skip over floats, OOF, open/close tags — they don't produce visible
	// inline content that would prevent trailing-whitespace trimming.
	// If a text item is trimmed to empty, continue scanning backwards to also
	// trim trailing whitespace from the preceding text item — this handles the
	// case where a space-only text node sits between two floats (e.g., "A " …
	// [float:left] … " " … [float:right]) and the inter-float space was trimmed
	// to zero but the preceding "A " still carries a trailing space.
	for i := len(line.Results) - 1; i >= 0; i-- {
		r := &line.Results[i]
		if r.Item.Type == InlineItemText {
			if cssPreservesWhitespace(r.Item.Style) {
				break
			}
			content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
			trimmed := strings.TrimRightFunc(content, isCSSCollapsibleSpace)
			if len(trimmed) < len(content) && r.Item.Style != nil {
				// Re-measure with the same font handleText used for the
				// initial InlineSize so the width delta stays consistent —
				// the ::first-line-merged font on the first line (WI-1).
				measureStyle := lb.measuringStyle(r.Item.Style)
				fontSize, _, _, _, _ := fontPropsFromStyle(measureStyle)
				fontPath := resolveFontPath(measureStyle, lb.fonts)
				var newWidth float64
				if isVertical {
					mtLU, _ := text.MeasureTextVerticalFromFont(trimmed, fontSize, fontPath)
					newWidth = mtLU.Float64()
				} else {
					mtLU, _ := text.MeasureText(trimmed, fontSize, fontPath)
					newWidth = mtLU.Float64()
				}
				line.Width -= (r.InlineSize - newWidth)
				r.InlineSize = newWidth
				r.TextEnd = r.TextStart + len(trimmed)
				if len(trimmed) == 0 {
					continue // trimmed to empty — keep scanning for more trailing space
				}
			}
			break
		}
		// Continue past items that don't produce visible inline content.
		if isNonVisibleInlineItem(r.Item.Type) {
			continue
		}
		break // Atomic inline or other content — stop searching.
	}

	// Cross-span kerning re-measures adjacent same-context text items as a
	// single shaped string and overrides per-item widths from the merged
	// shape's slices. Paint, however, reshapes each item separately — so
	// when measure-merged and paint-per-item produce different widths, the
	// items render squished or gapped.
	//
	// For pure-LTR text at paragraph base level 0 the difference is sub-
	// pixel kerning that's worth keeping (otherwise span-broken Latin
	// drifts ~1 px per boundary, e.g. `flexbox-order-only-flexitems`). For
	// any item above the paragraph base (level >= 1, i.e. bidi reordering
	// is in play) the per-item paint reshape produces structurally different
	// glyph positions from the merged-shape slice, and using the merged
	// widths makes items overlap or gap by many pixels (`bidi-normal-005`,
	// `bidi-unset-005/006` etc.).
	//
	// Blink solves this in general via `ShapeResult::CopyRange` so paint
	// reads from the merged shape (the F1c work in findings.md). Until that
	// lands, gate cross-span kerning on the LTR-paragraph-no-bidi case where
	// it's safe.
	lb.applyCrossSpanKerning(line, isVertical)

	// LB14: Do not break after opening punctuation. If the last placed text
	// item on this line consists entirely of Unicode opening punctuation (Ps
	// category, e.g. "[", "(", "{"), retract it to the next line so the break
	// falls BEFORE the bracket, not after it. Without this, a lone "[" text
	// node that fits in the remaining space is stranded on line N even though
	// its following content is on line N+1 — violating LBA Rule LB14.
	lb.retractTrailingOpenPunct(line)

	// Mark as last line if we've consumed all items.
	if lb.currentItemIndex >= len(lb.itemsData.Items) {
		line.IsLastLine = true
	}
}

// retractTrailingOpenPunct implements Unicode LBA Rule LB14 ("do not break
// after opening punctuation"). When the last word of the last text item on
// the current line consists entirely of Unicode Ps (Open Punctuation)
// characters (e.g. "[", "(", "{"), this method retracts that word to the
// next line so the break falls BEFORE the bracket rather than after it.
//
// The common case is a text node like "! [" where "! " fills the remaining
// space and "[" is the last word — "[" should begin the next line so the
// following content (e.g. "Qrst") is not orphaned from its opening bracket.
//
// Blink handles this naturally via its single-pass per-character iterator
// (BreakingContext in line/breaking_context_inline_headers.h) which applies
// LB14 inline without look-ahead. Louis14's item-at-a-time loop needs this
// explicit post-finalize correction.
func (lb *LineBreaker) retractTrailingOpenPunct(line *LineInfo) {
	// Only applies to content-mode breaking (not min/max-content sizing).
	if lb.mode != LineBreakerContent {
		return
	}
	// Only retract when the line ended because a following item didn't fit
	// (i.e. there are still items left to process). Last line = no following
	// content to violate LB14.
	if lb.currentItemIndex >= len(lb.itemsData.Items) {
		return
	}
	// Walk backwards through line.Results to find the last text item,
	// skipping zero-width non-visible items (open/close tags, OOF, floats).
	lastTextIdx := -1
	for i := len(line.Results) - 1; i >= 0; i-- {
		r := &line.Results[i]
		if r.Item.Type == InlineItemText {
			lastTextIdx = i
			break
		}
		if !isNonVisibleInlineItem(r.Item.Type) {
			return // Atomic inline — no OP tail to retract.
		}
	}
	if lastTextIdx < 0 {
		return
	}
	r := &line.Results[lastTextIdx]
	content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]

	// Find the last word in the item's content. splitIntoWords attaches
	// trailing spaces to the preceding word, so the last word here has no
	// trailing space if the item ends with non-space characters.
	words := splitIntoWords(content)
	if len(words) == 0 {
		return
	}
	lastWord := words[len(words)-1]

	// Only retract if the last word is entirely opening punctuation.
	if !isAllOpenPunct(lastWord) {
		return
	}

	// Retract the trailing OP word: trim the item's text range so it
	// excludes the OP word, and set currentTextOffset so the next
	// NextLine call re-processes from the OP word onwards.
	// The OP word starts at offset (len(content) - len(lastWord)) into
	// the item's text range.
	opWordStart := r.TextStart + len(content) - len(lastWord)

	// If the OP word IS the entire item (item consists only of OP chars),
	// remove the item from line.Results entirely and restore currentItemIndex.
	if opWordStart == r.TextStart {
		lb.currentItemIndex = r.ItemIndex
		lb.currentTextOffset = r.TextStart
		line.Results = line.Results[:lastTextIdx]
	} else {
		// Trim the item: keep the content before the OP word.
		trimmed := content[:len(content)-len(lastWord)]
		// Re-measure the trimmed content.
		measureStyle := lb.measuringStyle(r.Item.Style)
		isVertical := lb.isVerticalMeasurement(r.Item.Style)
		fontSize, _, _, _, _ := fontPropsFromStyle(measureStyle)
		fontPath := resolveFontPath(measureStyle, lb.fonts)
		var newWidth float64
		if isVertical {
			mtLU, _ := text.MeasureTextVerticalFromFont(trimmed, fontSize, fontPath)
			newWidth = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(trimmed, fontSize, fontPath)
			newWidth = mtLU.Float64()
		}
		r.TextEnd = opWordStart
		r.InlineSize = newWidth
		// Reset item cursor to r's item so the next NextLine call re-reads
		// the OP word (from opWordStart) out of the same text item.
		lb.currentItemIndex = r.ItemIndex
		lb.currentTextOffset = opWordStart
		// Trim line.Results: keep r (now trimmed) but remove anything after it.
		line.Results = line.Results[:lastTextIdx+1]
	}

	// Recompute line.Width from the (possibly trimmed) results.
	var w float64
	for i := range line.Results {
		w += line.Results[i].InlineSize
	}
	line.Width = w
}

// isAllOpenPunct reports whether every rune in s belongs to Unicode category
// Ps (Open Punctuation, i.e. opening brackets). An empty string returns false.
func isAllOpenPunct(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.Ps, r) {
			return false
		}
	}
	return true
}

// applyCrossSpanKerning re-measures runs of adjacent text items that share
// the same shaping context as a single concatenated string, then assigns
// each item its context-aware advance. Items are considered compatible when
// they use the same font/weight/style/size, letter-spacing, and word-spacing,
// and are separated only by zero-edge open/close tags.
//
// Skipped in vertical writing modes, where measurement is rune-count-based
// and not affected by horizontal kerning.
func (lb *LineBreaker) applyCrossSpanKerning(line *LineInfo, isVertical bool) {
	if isVertical {
		return
	}

	i := 0
	for i < len(line.Results) {
		if line.Results[i].Item.Type != InlineItemText {
			i++
			continue
		}

		// Extend the run across compatible text items, tolerating
		// intervening zero-edge open/close tags.
		runStart := i
		runEnd := i + 1
		for runEnd < len(line.Results) {
			r := &line.Results[runEnd]
			switch r.Item.Type {
			case InlineItemOpenTag, InlineItemCloseTag:
				if r.InlineSize != 0 {
					goto runDone
				}
				// Spans that carry a non-normal unicode-bidi value
				// (embed/override/isolate/plaintext/isolate-override)
				// inject a Unicode bidi control character into the text
				// content at the span boundary. Blink models these as
				// `kBidiControl` items of length 1 between adjacent text
				// items, which break shape merging via the `else { break }`
				// path in `inline_node.cc:1660-1702`. We don't materialize
				// kBidiControl as an InlineItem, so the equivalent break
				// must be enforced here on the OpenTag/CloseTag for any
				// span whose `unicode-bidi` is non-normal.
				if isBidiBoundary(r.Item.Style) {
					goto runDone
				}
				runEnd++
				continue
			case InlineItemText:
				if !canMergeShapingContext(line.Results[runStart].Item, r.Item) {
					goto runDone
				}
				runEnd++
				continue
			}
			goto runDone
		}
	runDone:

		// Collect indices of the text items in this run.
		var textIndices []int
		for j := runStart; j < runEnd; j++ {
			if line.Results[j].Item.Type == InlineItemText {
				textIndices = append(textIndices, j)
			}
		}

		if len(textIndices) < 2 {
			i = runEnd
			continue
		}

		// Cross-span merging is only safe when every item in the run is at
		// the paragraph base level (level 0, LTR). At level >= 1 the per-item
		// paint reshape produces glyph widths that don't match the merged-
		// shape slice, so applying merged widths to layout makes items render
		// at different positions than where their glyphs actually paint.
		// See the comment at the call site for the full rationale (F1c).
		allLevelZero := true
		for _, idx := range textIndices {
			if line.Results[idx].Item.BidiLevel != 0 {
				allLevelZero = false
				break
			}
		}
		if !allLevelZero {
			i = runEnd
			continue
		}

		// Concatenate text items in the run and shape once. Per-byte
		// cumulative advances let us slice each item's context-aware width,
		// including the advance adjustment applied to the LAST glyph of an
		// item by a kern pair that spans into the NEXT item.
		first := &line.Results[textIndices[0]]
		// Re-shape with the same font handleText measured with so the merged-
		// shape slices match the per-item widths — the ::first-line-merged
		// font on the first formatted line (WI-1).
		firstMeasureStyle := lb.measuringStyle(first.Item.Style)
		fontSize, _, _, _, _ := fontPropsFromStyle(firstMeasureStyle)
		fontPath := resolveFontPath(firstMeasureStyle, lb.fonts)
		letterSpacing, wordSpacing := 0.0, 0.0
		if firstMeasureStyle != nil {
			letterSpacing = firstMeasureStyle.GetLetterSpacing()
			wordSpacing = firstMeasureStyle.GetWordSpacing()
		}

		var combined strings.Builder
		itemRanges := make([][2]int, len(textIndices))
		itemRTL := make([]bool, len(textIndices))
		for k, idx := range textIndices {
			r := &line.Results[idx]
			content := lb.itemsData.TextContent[r.TextStart:r.TextEnd]
			start := combined.Len()
			combined.WriteString(content)
			itemRanges[k] = [2]int{start, combined.Len()}
			itemRTL[k] = r.Item.BidiLevel%2 == 1
		}

		// Build direction runs: collapse consecutive same-direction items into
		// one run so HarfBuzz can apply intra-run kerning across item boundaries.
		// Mirrors Blink's HarfBuzzShaper which segments by direction internally
		// and shapes each segment as a single buffer.
		var runs []text.DirectionRun
		for k := 0; k < len(itemRanges); k++ {
			runStartK := k
			for k+1 < len(itemRanges) && itemRTL[k+1] == itemRTL[runStartK] {
				k++
			}
			runs = append(runs, text.DirectionRun{
				Start: itemRanges[runStartK][0],
				End:   itemRanges[k][1],
				RTL:   itemRTL[runStartK],
			})
		}

		cum, ok := text.ShapeAdvancesMixed(combined.String(), runs, fontSize, fontPath)
		if !ok {
			i = runEnd
			continue
		}

		for k, idx := range textIndices {
			r := &line.Results[idx]
			rng := itemRanges[k]
			content := combined.String()[rng[0]:rng[1]]
			// Pair-of-positions snapped width: End[e] - Start[s], the
			// Blink ShapeResult::CachedWidth(start, end) analog.
			segmentWidth := cum.SnappedWidth(rng[0], rng[1]).Float64()

			if letterSpacing != 0 {
				rc := runeLen(content)
				if rc > 1 {
					segmentWidth += letterSpacing * float64(rc-1)
				}
			}
			if wordSpacing != 0 {
				segmentWidth += wordSpacing * float64(strings.Count(content, " "))
			}

			line.Width += segmentWidth - r.InlineSize
			r.InlineSize = segmentWidth
		}

		i = runEnd
	}
}

// isBidiBoundary reports whether an InlineItem's style introduces a Unicode
// bidi control character at this span boundary. CSS Writing Modes §2.2:
// `unicode-bidi: embed/override/isolate/plaintext/isolate-override` inject
// LRE/RLE/LRO/RLO/RLI/LRI/FSI and matching PDF/PDI characters around the
// element's content. These act like Blink's `kBidiControl` items between
// adjacent text items: shape merging must not cross them.
func isBidiBoundary(style *css.Style) bool {
	if style == nil {
		return false
	}
	bidi, ok := style.Get("unicode-bidi")
	if !ok || bidi == "" || bidi == "normal" {
		return false
	}
	return true
}

// canMergeShapingContext reports whether two inline items share the same
// HarfBuzz shaping context and can therefore be measured together as one
// concatenated run. Items must have:
//
//   - matching font (size/weight/style/family/mono/ahem),
//   - matching letter-spacing and word-spacing,
//   - matching CSS direction,
//   - and **the same resolved bidi level** (not just same parity).
//
// The bidi-level requirement mirrors Blink: after bidi resolution, every
// InlineItem has exactly one level (split by `InlineItem::SetBidiLevel` and
// by injected `kBidiControl` items at unicode-bidi:embed/override/isolate
// boundaries — see Blink `inline_node.cc:1660-1702`). Two items at levels
// 1 and 3 are both RTL parity but live in different embedding contexts and
// must shape as separate HarfBuzz buffers, exactly like ICU's
// `ubidi_getLogicalRun` returns runs of equal level.
//
// Pre-fix this predicate compared only direction parity, which let the
// embed boundary's level transition (1→3→1 in `<div dir=rtl>a <span style=
// "unicode-bidi:embed;direction:rtl">…</span> b</div>`) merge into a single
// shaping context — masking glyph positions that should differ from a single
// LRO-pre-ordered reference run.
func canMergeShapingContext(a, b *InlineItem) bool {
	if a.Style == nil || b.Style == nil {
		return a.Style == b.Style
	}
	if a.BidiLevel != b.BidiLevel {
		return false
	}
	aSize, aBold, aItalic, aMono, aAhem := fontPropsFromStyle(a.Style)
	bSize, bBold, bItalic, bMono, bAhem := fontPropsFromStyle(b.Style)
	if aSize != bSize || aBold != bBold || aItalic != bItalic || aMono != bMono || aAhem != bAhem {
		return false
	}
	aFamily, _ := a.Style.Get("font-family")
	bFamily, _ := b.Style.Get("font-family")
	if aFamily != bFamily {
		return false
	}
	if a.Style.GetLetterSpacing() != b.Style.GetLetterSpacing() {
		return false
	}
	if a.Style.GetWordSpacing() != b.Style.GetWordSpacing() {
		return false
	}
	if a.Style.GetDirection() != b.Style.GetDirection() {
		return false
	}
	return true
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
// Returns the *used* font-size (post-`font-size-adjust`) so the text shaper
// rasterizes glyphs at the visually-adjusted size per CSS Fonts 5 §1.7.
// Box geometry (em margins, line-height) stays on the specified size — only
// the glyph rendering scales.
func fontPropsFromStyle(style *css.Style) (fontSize float64, bold, italic, mono, ahem bool) {
	if style == nil {
		return 16, false, false, false, false
	}
	fontSize = style.GetUsedFontSize()
	// GetUsedFontSize already returns 16.0 when font-size is not set.
	// Only clamp NEGATIVE values (invalid) — do NOT clamp 0, which means the
	// author explicitly set font-size:0 (e.g. to collapse whitespace between
	// inline-block items) or font-size-adjust:0 (CSS Fonts 5 §1.7).
	// Clamping 0→16 would make space characters visible even when the author
	// intended zero-width spaces.
	if fontSize < 0 {
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
	synth := style.GetFontSynthesis()
	return fonts.FontPathForFamilyWithSynthesis(family, bold, italic, mono, ahem, synth.Weight, synth.Style)
}

// resolveStrutFontPath returns the font path for the CSS 2.1 §10.8.1 strut
// ("first available font" per CSS Fonts 3 §5.4). Unlike resolveFontPath, it
// uses FontPathForStrut which skips @font-face faces whose unicode-range
// excludes U+0020 (SPACE) — the reference character for primary-font
// determination. This matches Blink's FontFallbackList::DeterminePrimaryFont
// (third_party/blink/renderer/platform/fonts/font_fallback_list.cc @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func resolveStrutFontPath(style *css.Style, fonts text.FontConfig) string {
	if style == nil {
		return fonts.Regular
	}
	bold := style.GetFontWeight() == css.FontWeightBold
	italic := style.GetFontStyle() == css.FontStyleItalic
	mono := style.IsMonospaceFamily()
	ahem := style.IsAhemFamily()
	family, _ := style.Get("font-family")
	synth := style.GetFontSynthesis()
	return fonts.FontPathForStrut(family, bold, italic, mono, ahem, synth.Weight, synth.Style)
}

// resolveFontPathForRun returns the font path for a text run, accounting for
// @font-face unicode-range coverage. When textContent is non-empty and
// startOff/endOff describe a valid sub-slice, the first rune of the run is
// used as the coverage character — a face whose unicode-range excludes that
// rune is skipped in favour of the next family in the list. This mirrors
// Blink's per-character font selection in CSSSegmentedFontFace::FontDataForCharacter
// (core/css/css_segmented_font_face.cc @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
// When textContent is empty or the offsets are invalid, falls back to
// resolveFontPath (no coverage check — preserves behaviour for callers that
// don't have text content available).
func resolveFontPathForRun(style *css.Style, fonts text.FontConfig, textContent string, startOff, endOff int) string {
	if textContent == "" || startOff < 0 || endOff <= startOff || startOff >= len(textContent) {
		return resolveFontPath(style, fonts)
	}
	if style == nil {
		return fonts.Regular
	}
	bold := style.GetFontWeight() == css.FontWeightBold
	italic := style.GetFontStyle() == css.FontStyleItalic
	mono := style.IsMonospaceFamily()
	ahem := style.IsAhemFamily()
	family, _ := style.Get("font-family")
	synth := style.GetFontSynthesis()

	// Extract the first rune of the run for coverage checking.
	sub := textContent[startOff:]
	cp, _ := utf8.DecodeRuneInString(sub)
	if cp == utf8.RuneError {
		return resolveFontPath(style, fonts)
	}
	return fonts.FontPathForCovering(family, bold, italic, mono, ahem, synth.Weight, synth.Style, cp)
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
			mtLU, _ := text.MeasureTextVerticalFromFont(" ", fontSize, fontPath)
			spaceW = mtLU.Float64()
		} else {
			mtLU, _ := text.MeasureText(" ", fontSize, fontPath)
			spaceW = mtLU.Float64()
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
				mtLU, _ := text.MeasureTextVerticalFromFont(seg, fontSize, fontPath)
				segW = mtLU.Float64()
			} else {
				mtLU, _ := text.MeasureText(seg, fontSize, fontPath)
				segW = mtLU.Float64()
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
