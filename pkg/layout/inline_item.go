package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	textpkg "louis14/pkg/text"
	"strings"
	"unicode"
)

// InlineItemType identifies the kind of inline content.
// Ported from Blink's InlineItem::Type enum.
type InlineItemType int

const (
	// InlineItemText is a text run (shaped, measurable).
	InlineItemText InlineItemType = iota
	// InlineItemOpenTag marks the start of an inline element (span, em, etc.).
	InlineItemOpenTag
	// InlineItemCloseTag marks the end of an inline element.
	InlineItemCloseTag
	// InlineItemAtomicInline is a replaced element or inline-block.
	InlineItemAtomicInline
	// InlineItemFloat is a float within inline content.
	InlineItemFloat
	// InlineItemControl is a control character (forced line break, tab).
	InlineItemControl
	// InlineItemOutOfFlow is an absolutely or fixed positioned element
	// within inline content. It is not part of the normal flow but its
	// static position is determined by its position in the inline sequence.
	// Ported from Blink's InlineItem::kOutOfFlowPositioned.
	InlineItemOutOfFlow

	// InlineItemOpenRubyColumn marks the start of a ruby column. Emitted
	// at item-collection time when entering a `display: ruby` element and
	// (per CSS Ruby §2 box generation) reopened after every `</rt>` so
	// successive base/annotation pairs ("ruby columns") fall out of a flat
	// `<rb><rb><rt><rt>` child list. Ported from Blink's
	// InlineItem::kOpenRubyColumn (`core/layout/inline/inline_items_builder.cc:1550-1595,1682-1697`,
	// Chromium main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	InlineItemOpenRubyColumn
	// InlineItemCloseRubyColumn closes a ruby column. Emitted on `</rt>`
	// (inside a ruby) and on `</ruby>`. Almost-empty trailing columns
	// produced by the "reopen after </rt>" rule are stripped at `</ruby>`
	// (`inline_items_builder.cc:1617-1628`). Ported from Blink's
	// InlineItem::kCloseRubyColumn.
	InlineItemCloseRubyColumn
	// InlineItemRubyLinePlaceholder is a zero-width placeholder emitted
	// inside a ruby column to mark the start of a base or annotation
	// sub-line. The line breaker uses these to anchor sub-LineInfo
	// construction. Ported from Blink's InlineItem::kRubyLinePlaceholder
	// (`core/layout/inline/inline_items_builder.cc:1550-1595`).
	InlineItemRubyLinePlaceholder
)

// InlineItem is a segment of inline content within a formatting context.
// Items reference ranges within InlineItemsData.TextContent.
//
// Ported from Blink's InlineItem (inline_item.h).
type InlineItem struct {
	Type InlineItemType

	// StartOffset and EndOffset are byte offsets into TextContent.
	StartOffset int
	EndOffset   int

	// Node is the DOM node that produced this item.
	// Used by the renderer (via PhysicalFragment.Node) for style/tag lookup.
	Node *html.Node
	// LayoutNode is the layout tree node for atomic inlines that need
	// recursive layout. Nil for text items.
	LayoutNode *LayoutInputNode
	// Style is the computed style for this item's content.
	Style *css.Style

	// BidiLevel is the resolved bidirectional embedding level (0 = LTR).
	BidiLevel int

	// ParagraphLevel is the bidi paragraph base embedding level (0 = LTR,
	// 1 = RTL). For unicode-bidi: plaintext, paragraphs separated by
	// forced breaks may have different base levels (UAX#9 P2/P3).
	ParagraphLevel int

	// IsFirstFragment / IsLastFragment track which visual fragment of an inline
	// element this item represents. Used to suppress inline-start/inline-end
	// borders for split inline elements (CSS 2.1 §9.2.1.1 block-in-inline).
	// Both are true for regular (non-split) inlines.
	IsFirstFragment bool
	IsLastFragment  bool
}

// InlineItemsData is the pre-layout representation of all inline content
// in a block container. The DOM tree is flattened into a single text string
// and a sequence of items referencing ranges within it.
//
// Ported from Blink's InlineItemsData (inline_items_data.h).
type InlineItemsData struct {
	// TextContent is the concatenated text of all inline content.
	// Text nodes are whitespace-collapsed per CSS white-space.
	// Atomic inlines are represented as U+FFFC (object replacement).
	TextContent string

	// Items is the ordered sequence of inline items.
	Items []*InlineItem

	// RuneLevels holds the resolved bidi level for each rune in TextContent.
	// Populated by ResolveBidiLevels, stripped by StripBidiControls.
	RuneLevels []int

	// ParagraphLevels holds the paragraph base level for each rune.
	// For plaintext mode, different paragraphs separated by forced breaks
	// may have different base levels.
	ParagraphLevels []int
}

// CollectInlines performs a depth-first scan of the layout subtree rooted at
// the given block container, flattening inline content into an InlineItemsData.
//
// This is Phase 1a of Blink's inline pre-layout pipeline.
func CollectInlines(node *LayoutInputNode) *InlineItemsData {
	data := &InlineItemsData{}
	var b strings.Builder
	collectInlinesRecursive(node, data, &b, true, nil)
	data.TextContent = b.String()
	return data
}

// collectInlinesRecursive walks the layout tree depth-first, appending items.
// rubyState is non-nil when this call is recursing inside a `<ruby>`
// element; collectInlinesRecursive consults it to suppress forced
// breaks inside `<rt>` and to handle the per-`<rt>` column close/reopen.
// See ruby_inline_items.go.
func collectInlinesRecursive(
	node *LayoutInputNode,
	data *InlineItemsData,
	text *strings.Builder,
	isRoot bool,
	rubyState *rubyCollectState,
) {
	for _, child := range node.Children() {
		if child.IsText() {
			collectTextNode(child.DOMNode, node.Style(), data, text, rubyState)
			continue
		}

		// Anonymous inline boxes (no DOMNode) are accepted alongside real
		// element nodes. The only anonymous inline currently produced is the
		// `display:ruby` wrapper that `wrapBlockRubyAsTwoBox` generates as
		// the sole child of a `display:block ruby` principal box (mirrors
		// Blink's `LayoutRubyAsBlock` which creates an anonymous LayoutInline
		// with `display:ruby` as the IFC child — see
		// `core/layout/layout_ruby_as_block.cc` at SHA
		// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). That anonymous box must
		// still emit the open/close ruby column items so the line breaker
		// runs handleRuby on it just as it would on a real `<ruby>` element.
		if !child.IsElement() && !child.IsAnonymous() {
			continue
		}

		childStyle := child.Style()
		if childStyle == nil {
			continue
		}

		// Out-of-flow elements (abs-pos, fixed) are not part of the inline
		// flow, but their static position is recorded during line layout.
		// Ported from Blink's InlineItem::kOutOfFlowPositioned.
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			offset := text.Len()
			data.Items = append(data.Items, &InlineItem{
				Type:        InlineItemOutOfFlow,
				StartOffset: offset,
				EndOffset:   offset,
				Node:        child.DOMNode,
				LayoutNode:  child,
				Style:       childStyle,
			})
			continue
		}

		display := childStyle.GetDisplay()

		// <br> elements always produce a forced line break, regardless of
		// CSS display. In Blink, HTMLBRElement always creates a LayoutBR
		// (a LayoutText subclass), so the `display` property cannot turn
		// <br> into an atomic inline-block. Mirror that here by classifying
		// <br> as a control break before the float/atomic branches.
		if child.DOMNode != nil && child.DOMNode.TagName == "br" {
			// CSS Ruby §"forced breaks": `<br>` inside a `<rt>` (or
			// any descendant) is rewritten to a space. Mirrors Blink's
			// `kDisableForcedBreakInRubyColumn` gate at
			// `core/layout/inline/inline_items_builder.cc:74,801`.
			if rubyForcedBreakSuppressed(rubyState) {
				segStart := text.Len()
				text.WriteRune(' ')
				data.Items = append(data.Items, &InlineItem{
					Type:        InlineItemText,
					StartOffset: segStart,
					EndOffset:   text.Len(),
					Node:        child.DOMNode,
					Style:       childStyle,
				})
				continue
			}
			brOffset := text.Len()
			text.WriteRune('\n')
			data.Items = append(data.Items, &InlineItem{
				Type:        InlineItemControl,
				StartOffset: brOffset,
				EndOffset:   text.Len(),
				Node:        child.DOMNode,
				Style:       childStyle,
			})
			continue
		}

		// Floats within inline content.
		if childStyle.GetFloat() != css.FloatNone {
			offset := text.Len()
			data.Items = append(data.Items, &InlineItem{
				Type:        InlineItemFloat,
				StartOffset: offset,
				EndOffset:   offset,
				Node:        child.DOMNode,
				LayoutNode:  child,
				Style:       childStyle,
			})
			continue
		}

		// Block-level or atomic inline elements (inline-block, replaced,
		// inline-table, inline list-item). `display: inline list-item` is an
		// atomic inline that internally is a list-item block-flow (Blink
		// LayoutInlineListItem).
		if display == css.DisplayBlock || display == css.DisplayFlex ||
			display == css.DisplayTable || display == css.DisplayGrid ||
			display == css.DisplayInlineBlock || display == css.DisplayInlineFlex ||
			display == css.DisplayInlineTable || display == css.DisplayInlineListItem {
			// Atomic inline — represented as U+FFFC.
			offset := text.Len()
			text.WriteRune('\uFFFC')
			data.Items = append(data.Items, &InlineItem{
				Type:        InlineItemAtomicInline,
				StartOffset: offset,
				EndOffset:   text.Len(),
				Node:        child.DOMNode,
				LayoutNode:  child,
				Style:       childStyle,
			})
			continue
		}

		// Replaced elements (img, canvas, etc.) are atomic.
		if child.DOMNode != nil && IsReplacedElement(child.DOMNode) {
			offset := text.Len()
			text.WriteRune('\uFFFC')
			data.Items = append(data.Items, &InlineItem{
				Type:        InlineItemAtomicInline,
				StartOffset: offset,
				EndOffset:   text.Len(),
				Node:        child.DOMNode,
				LayoutNode:  child,
				Style:       childStyle,
			})
			continue
		}

		// CSS Writing Modes §7.3: An inline element whose writing mode is
		// orthogonal to its parent's (one is vertical, the other horizontal)
		// is promoted to an atomic inline — laid out in its own writing mode
		// and treated like display:inline-block in the parent's line.
		// Mirrors Blink's LayoutObject::IsAtomicInlineLevel() for orthogonal roots.
		if node.Style() != nil {
			parentWDM := NewWritingDirectionMode(node.Style())
			childWDM := NewWritingDirectionMode(childStyle)
			if childWDM.IsOrthogonalTo(parentWDM) {
				offset := text.Len()
				text.WriteRune('\uFFFC')
				data.Items = append(data.Items, &InlineItem{
					Type:        InlineItemAtomicInline,
					StartOffset: offset,
					EndOffset:   text.Len(),
					Node:        child.DOMNode,
					LayoutNode:  child,
					Style:       childStyle,
				})
				continue
			}
		}

		// Inline element (span, em, a, etc.) — emit open/close tags.

		// CSS Writing Modes §2.2: Inject Unicode bidi control characters
		// for elements with unicode-bidi set. This follows Blink's approach
		// in InlineItemsBuilder::InsertBidiOverride/InsertBidiIsolate:
		// control chars are inserted into TextContent so the UAX#9 algorithm
		// (run by ResolveBidiLevels) handles embedding/isolation correctly.
		injectBidiControlChars(childStyle, text, true /* isOpen */)

		// CSS Ruby — when entering a `<rt>` inside an enclosing
		// `<ruby>`, emit an annotation sub-line placeholder BEFORE the
		// `<rt>`'s OpenTag so ParseRubyInInlineItems can use it as the
		// boundary between the column's base content and the
		// annotation. Mirrors Blink
		// `core/layout/inline/inline_items_builder.cc:1550-1595`
		// (`IsInlineRubyText()` branch, @ 4883d11fef).
		if rubyState != nil && childStyle.IsInlineRubyText() {
			emitRubyAnnotationPlaceholder(data, text, childStyle, child.DOMNode)
		}

		openOffset := text.Len()
		data.Items = append(data.Items, &InlineItem{
			Type:            InlineItemOpenTag,
			StartOffset:     openOffset,
			EndOffset:       openOffset,
			Node:            child.DOMNode,
			Style:           childStyle,
			IsFirstFragment: child.IsFirstFragment(),
			IsLastFragment:  child.IsLastFragment(),
		})

		// CSS Ruby — `<rb>`/`<rbc>`/`<rtc>` are plain inlines per
		// Phase 1's UA stylesheet and stay transparent here;
		// ParseRubyInInlineItems treats their OpenTag/CloseTag pairs
		// as non-column-boundary items.
		var childRubyState *rubyCollectState
		switch {
		case childStyle.IsInlineRuby():
			childRubyState = &rubyCollectState{
				rubyStyle: childStyle,
				rubyNode:  child.DOMNode,
			}
			// Carry forced-break suppression depth from any enclosing
			// `<rt>` into the nested ruby — descendants of `<rt>`
			// continue to suppress `<br>`/`\n` regardless of how
			// many ruby boundaries they cross. Without this carry, a
			// `<ruby>` nested inside an outer `<rt>` resets the
			// counter to 0 and forced breaks in its subtree fire
			// when they shouldn't.
			if rubyState != nil {
				childRubyState.textNestingLevel = rubyState.textNestingLevel
			}
			childRubyState.currentColumnCheckpoint = openRubyColumn(
				data, text, childStyle, child.DOMNode,
			)
		case rubyState != nil && childStyle.IsInlineRubyText():
			rubyState.textNestingLevel++
			childRubyState = rubyState
		default:
			childRubyState = rubyState
		}

		collectInlinesRecursive(child, data, text, false, childRubyState)

		if rubyState != nil && childStyle.IsInlineRubyText() {
			rubyState.textNestingLevel--
		}

		closeOffset := text.Len()
		data.Items = append(data.Items, &InlineItem{
			Type:            InlineItemCloseTag,
			StartOffset:     closeOffset,
			EndOffset:       closeOffset,
			Node:            child.DOMNode,
			Style:           childStyle,
			IsFirstFragment: child.IsFirstFragment(),
			IsLastFragment:  child.IsLastFragment(),
		})

		// CloseTag emits before the column close so the `<rt>`/`<ruby>`
		// CloseTag belongs to the closing column (mirrors Blink
		// `inline_items_builder.cc:1617-1628,1682-1697`, @ 4883d11fef).
		switch {
		case childStyle.IsInlineRuby():
			closeOrStripRubyColumn(
				data, text,
				childRubyState.currentColumnCheckpoint,
				childStyle, child.DOMNode,
			)
		case rubyState != nil && childStyle.IsInlineRubyText():
			closeOrStripRubyColumn(
				data, text,
				rubyState.currentColumnCheckpoint,
				rubyState.rubyStyle, rubyState.rubyNode,
			)
			rubyState.currentColumnCheckpoint = openRubyColumn(
				data, text, rubyState.rubyStyle, rubyState.rubyNode,
			)
		}

		// Inject closing bidi control characters.
		injectBidiControlChars(childStyle, text, false /* isOpen */)

		continue
	}
}

// isAllVerticalScript reports whether content consists entirely of runes from
// scripts natively written vertically (Mongolian, Phags-Pa) plus ASCII
// whitespace. Used to decide whether to collapse text-orientation values to
// `sideways` for this run — see collectTextNode.
func isAllVerticalScript(content string) bool {
	sawVerticalScript := false
	for _, r := range content {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if !textpkg.IsVerticalScriptCharacter(r) {
			return false
		}
		sawVerticalScript = true
	}
	return sawVerticalScript
}

// collectTextNode adds a text node's content to the inline items,
// performing CSS white-space collapsing.
// rubyState is non-nil when this text node is inside a `<ruby>`;
// preserved `\n` forced breaks are rewritten to spaces while inside a
// `<rt>` (rubyForcedBreakSuppressed).
func collectTextNode(
	node *html.Node,
	parentStyle *css.Style,
	data *InlineItemsData,
	text *strings.Builder,
	rubyState *rubyCollectState,
) {
	content := node.Text
	if len(content) == 0 {
		return
	}

	// CSS Ruby — forced breaks inside `<rt>` (or any descendant) are
	// rewritten to spaces at item-collection time so the existing
	// whitespace-handling branches below never emit InlineItemControl
	// for these newlines. Mirrors Blink
	// `core/layout/inline/inline_items_builder.cc:74,1068`
	// (`kDisableForcedBreakInRubyColumn` gate, @ 4883d11fef). The
	// RawText override (for `white-space: pre`/`pre-wrap`) is applied
	// at its use site below.
	suppressRubyBreaks := rubyForcedBreakSuppressed(rubyState) &&
		strings.ContainsAny(content, "\n\r")
	if suppressRubyBreaks {
		content = strings.ReplaceAll(content, "\n", " ")
		content = strings.ReplaceAll(content, "\r", " ")
	}

	// CSS Writing Modes §5.1 interop: for scripts natively written vertically
	// (Mongolian, Phags-Pa) the font's vertical metrics equal its horizontal
	// metrics, so `mixed`, `upright`, and `sideways` produce the same visual.
	// We mirror Blink's font-driven convergence at the style level by treating
	// all-vertical-script text runs as text-orientation: sideways — downstream
	// baseline selection, measurement, and painting then take the unified path.
	if parentStyle != nil && isAllVerticalScript(content) {
		if to, _ := parentStyle.Get("text-orientation"); to != "sideways" {
			clone := parentStyle.Clone()
			clone.Set("text-orientation", "sideways")
			parentStyle = clone
		}
	}

	// Determine white-space handling.
	whiteSpace := "normal"
	if parentStyle != nil {
		if ws, ok := parentStyle.Get("white-space"); ok {
			whiteSpace = ws
		}
	}

	preserveNewlines := whiteSpace == "pre" || whiteSpace == "pre-wrap" || whiteSpace == "pre-line"
	collapseSpaces := whiteSpace == "normal" || whiteSpace == "nowrap" || whiteSpace == "pre-line"

	startOffset := text.Len()

	if !collapseSpaces {
		// Preserve whitespace as-is (white-space: pre / pre-wrap).
		// Use RawText which preserves the original whitespace from the HTML
		// source (node.Text may have been collapsed during HTML parsing).
		preservedContent := content
		if node.RawText != "" {
			preservedContent = node.RawText
			if rubyForcedBreakSuppressed(rubyState) &&
				strings.ContainsAny(preservedContent, "\n\r") {
				preservedContent = strings.ReplaceAll(preservedContent, "\n", " ")
				preservedContent = strings.ReplaceAll(preservedContent, "\r", " ")
			}
		}
		// CSS 2.1 §16.6: newlines in preserved-whitespace content cause forced
		// line breaks. Split on '\n' and emit InlineItemControl for each break,
		// mirroring Blink's inline_items_builder.cc::AppendText and the
		// collapseSpaces path's newline handling.
		for _, seg := range strings.SplitAfter(preservedContent, "\n") {
			if strings.HasSuffix(seg, "\n") {
				// Emit any text before the newline.
				before := seg[:len(seg)-1]
				if len(before) > 0 {
					segStart := text.Len()
					text.WriteString(before)
					data.Items = append(data.Items, &InlineItem{
						Type:        InlineItemText,
						StartOffset: segStart,
						EndOffset:   text.Len(),
						Node:        node,
						Style:       parentStyle,
					})
				}
				// Emit control item for the forced break.
				brOffset := text.Len()
				text.WriteRune('\n')
				data.Items = append(data.Items, &InlineItem{
					Type:        InlineItemControl,
					StartOffset: brOffset,
					EndOffset:   text.Len(),
					Node:        node,
					Style:       parentStyle,
				})
				startOffset = text.Len()
			} else if len(seg) > 0 {
				// Segment after the last newline (or entire content if no newline).
				text.WriteString(seg)
			}
		}
	} else {
		// Collapse whitespace per CSS 2.1 §16.6.1.
		// - Sequences of spaces/tabs collapse to a single space.
		// - Newlines collapse to a space (unless preserve-newlines).
		prevSpace := false
		// Check if the text so far ends with a space.
		if text.Len() > 0 {
			s := text.String()
			prevSpace = s[len(s)-1] == ' '
		}

		for _, r := range content {
			if r == '\n' && preserveNewlines {
				// Forced line break.
				endOffset := text.Len()
				if endOffset > startOffset {
					data.Items = append(data.Items, &InlineItem{
						Type:        InlineItemText,
						StartOffset: startOffset,
						EndOffset:   endOffset,
						Node:        node,
						Style:       parentStyle,
					})
				}
				// Emit control item for forced break.
				brOffset := text.Len()
				text.WriteRune('\n')
				data.Items = append(data.Items, &InlineItem{
					Type:        InlineItemControl,
					StartOffset: brOffset,
					EndOffset:   text.Len(),
					Node:        node,
					Style:       parentStyle,
				})
				startOffset = text.Len()
				prevSpace = false
				continue
			}

			if r != '\u00A0' && unicode.IsSpace(r) {
				if !prevSpace {
					text.WriteRune(' ')
					prevSpace = true
				}
				continue
			}

			text.WriteRune(r)
			prevSpace = false
		}
	}

	endOffset := text.Len()
	if endOffset > startOffset {
		data.Items = append(data.Items, &InlineItem{
			Type:        InlineItemText,
			StartOffset: startOffset,
			EndOffset:   endOffset,
			Node:        node,
			Style:       parentStyle,
		})
	}
}

// injectBidiControlChars writes Unicode bidi control characters into the text
// buffer for an element with CSS unicode-bidi set. This implements the same
// logic as Blink's InlineItemsBuilder::InsertBidiOverride and InsertBidiIsolate.
//
// At the opening tag, directional embedding/override/isolate characters are
// written. At the closing tag, the corresponding terminator (PDF or PDI) is
// written. The existing ResolveBidiLevels (UAX#9 algorithm) then processes
// these characters to compute correct bidi levels for all inline items.
//
// Control character mapping per CSS Writing Modes §2.2:
//
//	unicode-bidi    direction  open            close
//	embed           ltr        LRE (U+202A)    PDF (U+202C)
//	embed           rtl        RLE (U+202B)    PDF (U+202C)
//	isolate         ltr        LRI (U+2066)    PDI (U+2069)
//	isolate         rtl        RLI (U+2067)    PDI (U+2069)
//	bidi-override   ltr        LRO (U+202D)    PDF (U+202C)
//	bidi-override   rtl        RLO (U+202E)    PDF (U+202C)
//	isolate-override ltr       LRI + LRO       PDF + PDI
//	isolate-override rtl       RLI + RLO       PDF + PDI
//	plaintext        —         FSI (U+2068)    PDI (U+2069)
func injectBidiControlChars(style *css.Style, text *strings.Builder, isOpen bool) {
	if style == nil {
		return
	}
	bidiVal, hasBidi := style.Get("unicode-bidi")
	if !hasBidi || bidiVal == "" || bidiVal == "normal" {
		return
	}

	dir := style.GetDirection()
	isRTL := dir == css.DirectionRTL

	if isOpen {
		switch bidiVal {
		case "embed":
			if isRTL {
				text.WriteRune('\u202B') // RLE
			} else {
				text.WriteRune('\u202A') // LRE
			}
		case "isolate":
			if isRTL {
				text.WriteRune('\u2067') // RLI
			} else {
				text.WriteRune('\u2066') // LRI
			}
		case "bidi-override":
			if isRTL {
				text.WriteRune('\u202E') // RLO
			} else {
				text.WriteRune('\u202D') // LRO
			}
		case "isolate-override":
			if isRTL {
				text.WriteRune('\u2067') // RLI
				text.WriteRune('\u202E') // RLO
			} else {
				text.WriteRune('\u2066') // LRI
				text.WriteRune('\u202D') // LRO
			}
		case "plaintext":
			text.WriteRune('\u2068') // FSI
		}
	} else {
		// Close tag: emit terminator(s).
		switch bidiVal {
		case "embed", "bidi-override":
			text.WriteRune('\u202C') // PDF
		case "isolate":
			text.WriteRune('\u2069') // PDI
		case "isolate-override":
			text.WriteRune('\u202C') // PDF
			text.WriteRune('\u2069') // PDI
		case "plaintext":
			text.WriteRune('\u2069') // PDI
		}
	}
}

// injectBlockBidiControls prepends/appends Unicode bidi control characters
// for a block container's own unicode-bidi property. This implements the
// block-level counterpart to the inline-level injectBidiControlChars.
//
// CSS Writing Modes §2.2 specifies that when a block container has
// unicode-bidi set to bidi-override, isolate-override, or plaintext,
// it should affect the bidi resolution of its inline content by injecting
// the corresponding control characters around the block's text content.
//
// For block containers, embed and isolate do NOT inject control characters
// into the block's own inline content. These values only affect how the
// block interacts with surrounding inline content (which is not applicable
// for block-level elements). Only bidi-override, isolate-override, and
// plaintext cause injection at the block level.
//
// This mirrors Blink's InlineItemsBuilder which checks the block container's
// own unicode-bidi/direction before processing child inline items.
func injectBlockBidiControls(style *css.Style, data *InlineItemsData) {
	if style == nil {
		return
	}
	bidiVal, hasBidi := style.Get("unicode-bidi")
	if !hasBidi || bidiVal == "" || bidiVal == "normal" {
		return
	}

	dir := style.GetDirection()
	isRTL := dir == css.DirectionRTL

	// Determine opening and closing control characters.
	// Only bidi-override, isolate-override, and plaintext are relevant
	// for block containers. embed and isolate on blocks don't inject
	// controls into the block's own inline content.
	var openChars, closeChars []rune
	switch bidiVal {
	case "bidi-override":
		if isRTL {
			openChars = []rune{'\u202E'} // RLO
		} else {
			openChars = []rune{'\u202D'} // LRO
		}
		closeChars = []rune{'\u202C'} // PDF
	case "isolate-override":
		if isRTL {
			openChars = []rune{'\u2067', '\u202E'} // RLI + RLO
		} else {
			openChars = []rune{'\u2066', '\u202D'} // LRI + LRO
		}
		closeChars = []rune{'\u202C', '\u2069'} // PDF + PDI
	default:
		// embed, isolate, normal, and plaintext do not inject control
		// characters at block level. For plaintext specifically, the
		// block itself is the paragraph — wrapping its content in FSI/PDI
		// would defeat UAX#9 P2/P3 first-strong-character detection
		// (the isolate would hide the strong character from P2/P3).
		// ResolveBidiLevelsPlaintext handles auto-direction per paragraph
		// directly on the unwrapped content, matching Blink's call of ICU
		// with UBIDI_DEFAULT_LTR on each paragraph.
		return
	}

	if len(openChars) == 0 {
		return
	}

	// Build the prefix string and compute its byte length.
	var prefix string
	for _, r := range openChars {
		prefix += string(r)
	}
	var suffix string
	for _, r := range closeChars {
		suffix += string(r)
	}
	prefixLen := len(prefix)

	// Shift all existing item offsets by the prefix length.
	for _, item := range data.Items {
		item.StartOffset += prefixLen
		item.EndOffset += prefixLen
	}

	// Prepend prefix and append suffix to TextContent.
	data.TextContent = prefix + data.TextContent + suffix
}

// IsReplacedElement returns true for elements that are replaced
// (have intrinsic dimensions, laid out as atomic inlines).
//
// Callers MUST guard with `node != nil` before invoking. This function
// dereferences `node.TagName` without a nil check by design: it encodes
// the louis14 analog of Blink's "LayoutObject is never null" invariant.
// Blink dispatches `LayoutObject::IsReplaced()` on a real LayoutObject
// so the nil case is unreachable in its type system; louis14 uses
// *html.Node, which IS nilable for anonymous boxes (no DOM element),
// so callers carry the same guarantee explicitly. Every production
// caller follows the pattern `node != nil && IsReplacedElement(node)`
// — see e.g. pkg/layout/inline_layout.go, flex_layout.go,
// block_layout.go, out_of_flow_layout.go, inline_item.go itself
// (including IsTransformableBox below), and
// pkg/render/paint_layer.go's transform-collection branch.
//
// Do NOT "fix" the lack of a nil case by adding `if node == nil {
// return false }` here — that would diverge from both Blink's shape
// (which has no such guard because it doesn't need one) and the
// established louis14 caller-side convention.
func IsReplacedElement(node *html.Node) bool {
	switch node.TagName {
	case "img", "video", "canvas", "svg", "iframe", "embed", "object",
		"input", "textarea", "select", "button":
		return true
	}
	return false
}

// IsTransformableBox reports whether the given box accepts CSS
// transform / translate / rotate / scale per CSS Transforms Level 1
// §3 "transformable element"
// (https://www.w3.org/TR/css-transforms-1/#transformable-element):
//
//	"...all elements whose layout is governed by the CSS box model
//	 except for non-replaced inline boxes, table-column boxes, and
//	 table-column-group boxes..."
//
// Returns false for non-replaced inline-level boxes (`display: inline`,
// `display: ruby`, `display: ruby-text`). Returns true for everything
// else, including atomic inline-level boxes (`inline-block`,
// `inline-flex`, `inline-grid`, `inline-table`, `inline-list-item`) and
// replaced inline elements (img, video, etc. — atomic inlines per
// CSS Display 3 §2.2 even though their computed `display` may still
// resolve to `inline`). Ruby and ruby-text fall under "non-replaced
// inline boxes" because they generate inline-level boxes (Blink models
// them via LayoutInline).
//
// louis14's GetDisplay() doesn't currently recognize `display:
// table-column` / `display: table-column-group` as distinct display
// values — they fall through to the default. Those would also need
// to be gated here per the spec when louis14 grows native column
// support.
//
// Mirrors the `!object.IsBox()` short-circuit of Blink's `NeedsTransform`
// at
// third_party/blink/renderer/core/paint/paint_property_tree_builder.cc:1310
// (function spans :1299-:1319) at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
// This is ONE of NeedsTransform's five branches — backface-visibility:hidden,
// transform animations, and preserve-3d also create paint-property
// transform nodes in Blink, and louis14 doesn't yet model those
// pathways into stacking-context creation (separate gap, not in
// LOU-156's scope). An earlier version of louis14 cited
// `LayoutObject::HasTransformRelatedProperty()` as the gate; that
// was incorrect at the pinned SHA — that method is a style-flag
// query, not a transformability gate. SVG elements are handled by
// their own paint paths (svg_*painter.go) and don't reach this
// predicate.
//
// The `node != nil` guard mirrors louis14's caller-side analog to
// Blink's "LayoutObject is never null" type-system invariant — see
// IsReplacedElement above. Anonymous inline boxes (Node==nil, no DOM
// element) fall through to `return false`, matching Blink's outcome
// for LayoutInline.
func IsTransformableBox(s *css.Style, node *html.Node) bool {
	if s == nil {
		return true
	}
	switch s.GetDisplay() {
	case css.DisplayInline, css.DisplayRuby, css.DisplayRubyText:
		if node != nil && IsReplacedElement(node) {
			return true
		}
		return false
	}
	return true
}
