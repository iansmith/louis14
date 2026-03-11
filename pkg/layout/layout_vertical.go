package layout

import (
	"strings"
	"unicode"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

// layoutVerticalInline lays out inline children for a writing-mode:vertical-rl/lr container.
// Each text character becomes an individual box positioned in a column (top-to-bottom).
// Columns stack right-to-left for vertical-rl, or left-to-right for vertical-lr.
//
// The columnLength is the available inline extent (= container physical height for vertical-rl/lr).
// If columnLength <= 0, a very large value is used (no wrapping — one infinite column).
//
// Returns positioned in-flow child boxes. Abs-pos children are skipped (caller handles them).
// Also returns the number of columns used and the max column height, so the caller can
// update the container's auto dimensions.
func (le *LayoutEngine) layoutVerticalInline(
	container *Box,
	children []*html.Node,
	contentX, contentY, contentWidth float64,
	columnLength float64,
	isRTL bool, // true for vertical-rl (columns go right-to-left)
	computedStyles map[*html.Node]*css.Style,
) (boxes []*Box, totalCols int, maxColHeight float64) {

	if columnLength <= 0 {
		columnLength = 1e9 // effectively infinite
	}

	currentColIdx := 0
	currentY := contentY

	// processNodes recursively processes inline children, emitting character boxes.
	var processNodes func(nodes []*html.Node, inheritedStyle *css.Style)
	processNodes = func(nodes []*html.Node, inheritedStyle *css.Style) {
		for _, node := range nodes {
			switch node.Type {
			case html.TextNode:
				if inheritedStyle == nil {
					continue
				}
				fontSize := inheritedStyle.GetFontSize()
				if fontSize <= 0 {
					fontSize = 16
				}
				bold := inheritedStyle.GetFontWeight() == css.FontWeightBold
				italic := inheritedStyle.GetFontStyle() == css.FontStyleItalic
				mono := inheritedStyle.IsMonospaceFamily()
				ahem := inheritedStyle.IsAhemFamily()
				lineHeight := inheritedStyle.GetLineHeight()
				if lineHeight <= 0 {
					lineHeight = fontSize
				}

				// Collect and normalize text content.
				rawText := node.Text
				ws := inheritedStyle.GetWhiteSpace()
				if ws != "pre" && ws != "pre-wrap" && ws != "pre-line" {
					// CSS white-space:normal: collapse runs of whitespace to single space.
					rawText = strings.Join(strings.Fields(rawText), " ")
				}

				for _, ch := range rawText {
					// Each character occupies a slot of lineHeight in the inline (Y) direction.
					slotSize := lineHeight

					// Wrap to next column when the current slot would exceed the column length.
					if currentY+slotSize > contentY+columnLength {
						currentColIdx++
						currentY = contentY
					}

					// Compute the character's slot width (column width = em-square).
					// For simplicity use fontSize as column width for all chars.
					// For rotated Latin in non-Ahem fonts the visual slot is charW wide,
					// but we still allocate fontSize for the column to keep columns uniform.
					colWidth := fontSize
					_ = bold
					_ = italic
					_ = mono
					_ = ahem
					_ = ch // used below for box creation

					// Compute physical X of this column.
					var charX float64
					if isRTL {
						// vertical-rl: column 0 is rightmost, columns advance leftward.
						charX = contentX + contentWidth - float64(currentColIdx+1)*colWidth
					} else {
						// vertical-lr: column 0 is leftmost, columns advance rightward.
						charX = contentX + float64(currentColIdx)*colWidth
					}

					b := &Box{
						Node:          node,
						Style:         inheritedStyle,
						X:             charX,
						Y:             currentY,
						Width:         colWidth,
						Height:        slotSize,
						PseudoContent: string(ch),
					}
					boxes = append(boxes, b)

					currentY += slotSize

					// Track total columns and max column height.
					if currentColIdx+1 > totalCols {
						totalCols = currentColIdx + 1
					}
					if currentY-contentY > maxColHeight {
						maxColHeight = currentY - contentY
					}
				}

			case html.ElementNode:
				childStyle := computedStyles[node]
				if childStyle == nil {
					childStyle = inheritedStyle
				}
				if childStyle == nil {
					continue
				}
				// Skip absolutely/fixed positioned elements — they're handled by the caller.
				childPos := childStyle.GetPosition()
				if childPos == css.PositionAbsolute || childPos == css.PositionFixed {
					continue
				}
				// Skip display:none elements.
				if childStyle.GetDisplay() == css.DisplayNone {
					continue
				}
				// Skip floated elements for now.
				if childStyle.GetFloat() != css.FloatNone {
					continue
				}
				// Recurse into inline children (spans, etc.).
				processNodes(node.Children, childStyle)
			}
		}
	}

	processNodes(children, container.Style)

	if totalCols == 0 && len(boxes) > 0 {
		totalCols = 1
	}

	return boxes, totalCols, maxColHeight
}

// isVerticalWritingMode returns true if the style has a vertical writing mode.
func isVerticalWritingMode(style *css.Style) bool {
	if style == nil {
		return false
	}
	wm, _ := style.Get("writing-mode")
	return wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr"
}

// isVerticalRL returns true if the style has writing-mode: vertical-rl.
func isVerticalRL(style *css.Style) bool {
	if style == nil {
		return false
	}
	wm, _ := style.Get("writing-mode")
	return wm == "vertical-rl" || wm == "sideways-rl"
}

// measureVerticalCharSlot returns the inline-direction slot size for a character
// in vertical writing mode. For Ahem (square glyphs) this is always fontSize.
// For other fonts, rotated Latin chars use their horizontal width as the slot;
// upright CJK chars use fontSize.
func measureVerticalCharSlot(ch rune, style *css.Style) float64 {
	if style == nil {
		return 16
	}
	fontSize := style.GetFontSize()
	if fontSize <= 0 {
		fontSize = 16
	}
	if style.IsAhemFamily() {
		return fontSize
	}

	// Check text-orientation.
	textOrientation, _ := style.Get("text-orientation")
	if textOrientation == "upright" {
		return fontSize
	}
	// For "sideways", all characters rotate (slot = horizontal char width).
	bold := style.GetFontWeight() == css.FontWeightBold
	italic := style.GetFontStyle() == css.FontStyleItalic
	mono := style.IsMonospaceFamily()
	if textOrientation == "sideways" {
		charW, _ := text.MeasureTextWithStyle(string(ch), fontSize, bold, italic, mono, false)
		return charW
	}

	// "mixed" (default): CJK/full-width chars are upright (slot = fontSize),
	// Latin/ASCII chars are rotated sideways (slot = charWidth).
	if IsCJKOrFullWidth(ch) {
		return fontSize
	}
	charW, _ := text.MeasureTextWithStyle(string(ch), fontSize, bold, italic, mono, false)
	return charW
}

// IsCJKOrFullWidth returns true if the rune is a CJK ideograph or full-width
// character that stays upright in vertical writing modes with text-orientation:mixed.
func IsCJKOrFullWidth(r rune) bool {
	// Unicode ranges for characters that stay upright in vertical-rl:
	// CJK Unified Ideographs (basic block)
	if r >= 0x4E00 && r <= 0x9FFF {
		return true
	}
	// Hiragana
	if r >= 0x3040 && r <= 0x309F {
		return true
	}
	// Katakana
	if r >= 0x30A0 && r <= 0x30FF {
		return true
	}
	// Hangul syllables
	if r >= 0xAC00 && r <= 0xD7AF {
		return true
	}
	// CJK Extensions, Compatibility, etc.
	if r >= 0x3000 && r <= 0x303F {
		return true // CJK Symbols and Punctuation
	}
	// ASCII and Latin characters always rotate sideways in "mixed" mode.
	if r < 0x0300 && !unicode.IsSpace(r) {
		// Check if it's a combining character or spacing modifier
		return false
	}
	return false
}
