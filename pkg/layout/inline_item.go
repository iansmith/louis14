package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
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
	collectInlinesRecursive(node, data, &b, true)
	data.TextContent = b.String()
	return data
}

// collectInlinesRecursive walks the layout tree depth-first, appending items.
func collectInlinesRecursive(
	node *LayoutInputNode,
	data *InlineItemsData,
	text *strings.Builder,
	isRoot bool,
) {
	for _, child := range node.Children() {
		if child.IsText() {
			collectTextNode(child.DOMNode, node.Style(), data, text)
			continue
		}

		if !child.IsElement() {
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

		// Block-level or atomic inline elements (inline-block, replaced).
		if display == css.DisplayBlock || display == css.DisplayFlex ||
			display == css.DisplayTable || display == css.DisplayGrid ||
			display == css.DisplayInlineBlock || display == css.DisplayInlineFlex {
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

		// <br> elements produce a forced line break.
		if child.DOMNode != nil && child.DOMNode.TagName == "br" {
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

		// Replaced elements (img, canvas, etc.) are atomic.
		if child.DOMNode != nil && isReplacedElement(child.DOMNode) {
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

		// Inline element (span, em, a, etc.) — emit open/close tags.

		// CSS Writing Modes §2.2: Inject Unicode bidi control characters
		// for elements with unicode-bidi set. This follows Blink's approach
		// in InlineItemsBuilder::InsertBidiOverride/InsertBidiIsolate:
		// control chars are inserted into TextContent so the UAX#9 algorithm
		// (run by ResolveBidiLevels) handles embedding/isolation correctly.
		injectBidiControlChars(childStyle, text, true /* isOpen */)

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

		// Recurse into children.
		collectInlinesRecursive(child, data, text, false)

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

		// Inject closing bidi control characters.
		injectBidiControlChars(childStyle, text, false /* isOpen */)

		continue
	}
}

// collectTextNode adds a text node's content to the inline items,
// performing CSS white-space collapsing.
func collectTextNode(
	node *html.Node,
	parentStyle *css.Style,
	data *InlineItemsData,
	text *strings.Builder,
) {
	content := node.Text
	if len(content) == 0 {
		return
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
		// Preserve whitespace as-is.
		text.WriteString(content)
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

// isReplacedElement returns true for elements that are replaced
// (have intrinsic dimensions, laid out as atomic inlines).
func isReplacedElement(node *html.Node) bool {
	switch node.TagName {
	case "img", "video", "canvas", "svg", "iframe", "embed", "object",
		"input", "textarea", "select", "button":
		return true
	}
	return false
}
