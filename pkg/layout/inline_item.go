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
	Node *html.Node
	// Style is the computed style for this item's content.
	Style *css.Style

	// BidiLevel is the resolved bidirectional embedding level (0 = LTR).
	BidiLevel int
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
}

// CollectInlines performs a depth-first scan of the DOM subtree rooted at
// the given block container, flattening inline content into an InlineItemsData.
//
// This is Phase 1a of Blink's inline pre-layout pipeline.
func CollectInlines(node *html.Node, styles map[*html.Node]*css.Style) *InlineItemsData {
	data := &InlineItemsData{}
	var b strings.Builder
	collectInlinesRecursive(node, styles, data, &b, true)
	data.TextContent = b.String()
	return data
}

// collectInlinesRecursive walks the DOM tree depth-first, appending items.
func collectInlinesRecursive(
	node *html.Node,
	styles map[*html.Node]*css.Style,
	data *InlineItemsData,
	text *strings.Builder,
	isRoot bool,
) {
	for _, child := range node.Children {
		if child.Type == html.TextNode {
			collectTextNode(child, styles[node], data, text)
			continue
		}

		if child.Type != html.ElementNode {
			continue
		}

		childStyle := styles[child]
		if childStyle == nil {
			continue
		}

		display := childStyle.GetDisplay()

		// Skip display:none.
		if display == css.DisplayNone {
			continue
		}

		// Floats within inline content.
		if childStyle.GetFloat() != css.FloatNone {
			offset := text.Len()
			data.Items = append(data.Items, &InlineItem{
				Type:        InlineItemFloat,
				StartOffset: offset,
				EndOffset:   offset,
				Node:        child,
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
				Node:        child,
				Style:       childStyle,
			})
			continue
		}

		// Replaced elements (img, canvas, etc.) are atomic.
		if isReplacedElement(child) {
			offset := text.Len()
			text.WriteRune('\uFFFC')
			data.Items = append(data.Items, &InlineItem{
				Type:        InlineItemAtomicInline,
				StartOffset: offset,
				EndOffset:   text.Len(),
				Node:        child,
				Style:       childStyle,
			})
			continue
		}

		// Inline element (span, em, a, etc.) — emit open/close tags.
		openOffset := text.Len()
		data.Items = append(data.Items, &InlineItem{
			Type:        InlineItemOpenTag,
			StartOffset: openOffset,
			EndOffset:   openOffset,
			Node:        child,
			Style:       childStyle,
		})

		// Recurse into children.
		collectInlinesRecursive(child, styles, data, text, false)

		closeOffset := text.Len()
		data.Items = append(data.Items, &InlineItem{
			Type:        InlineItemCloseTag,
			StartOffset: closeOffset,
			EndOffset:   closeOffset,
			Node:        child,
			Style:       childStyle,
		})

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

			if unicode.IsSpace(r) {
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
