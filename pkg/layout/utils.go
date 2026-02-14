package layout

import (
	"fmt"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// getNodeName returns a debug string for a node
func getNodeName(node *html.Node) string {
	if node == nil {
		return "<nil>"
	}
	if node.Type == html.TextNode {
		return fmt.Sprintf("TEXT(%q)", truncateString(node.Text, 20))
	}
	if node.Type == html.ElementNode {
		if node.TagName != "" {
			return "<" + node.TagName + ">"
		}
		return "<element>"
	}
	return fmt.Sprintf("<%v>", node.Type)
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// AllBorders returns BorderEdgeFlags with all edges enabled
func AllBorders() BorderEdgeFlags {
	return BorderEdgeFlags{Left: true, Right: true, Top: true, Bottom: true}
}

// Box methods for fragments

// AddFragment adds a fragment to the box
func (b *Box) AddFragment(x, y, width, height float64, borders BorderEdgeFlags) {
	b.Fragments = append(b.Fragments, BoxFragment{
		X:       x,
		Y:       y,
		Width:   width,
		Height:  height,
		Borders: borders,
	})
}

// HasFragments returns true if this box should render as fragments
func (b *Box) HasFragments() bool {
	return len(b.Fragments) > 0
}

// GetBorderFlags returns which borders should be drawn based on fragment state
func (b *Box) GetBorderFlags() BorderEdgeFlags {
	// If we have explicit fragments, use the main box position only for the first
	if b.HasFragments() {
		return BorderEdgeFlags{} // Don't draw borders for main box, use fragments
	}

	// Legacy fragment flags for backward compatibility
	flags := AllBorders()
	if b.IsFirstFragment {
		flags.Right = false
	}
	if b.IsLastFragment {
		flags.Left = false
	}
	return flags
}

// LayoutEngine utility methods

// repositionFloatRightChildren repositions float:right children to align with the right edge
func (le *LayoutEngine) repositionFloatRightChildren(box *Box) {
	contentLeft := box.X + box.Border.Left + box.Padding.Left
	contentRight := contentLeft + box.Width
	for _, child := range box.Children {
		if child.Style != nil && child.Style.GetFloat() == css.FloatRight {
			childTotalWidth := le.getTotalWidth(child)
			// Float:right: right edge of child aligns with right edge of parent content
			newX := contentRight - childTotalWidth
			dx := newX - child.X
			if dx != 0 {
				child.X = newX
				le.shiftChildren(child, dx, 0)
				// After shifting, recursively fix any float:right grandchildren
				le.repositionFloatRightChildren(child)
			}
		}
	}
}

// getStyle returns the computed style for a node
func (le *LayoutEngine) getStyle(node *html.Node) *css.Style {
	if styleAttr, ok := node.GetAttribute("style"); ok {
		return css.ParseInlineStyle(styleAttr)
	}
	return css.NewStyle()
}

// getTotalHeight returns the total height including margin, border, padding
func (le *LayoutEngine) getTotalHeight(box *Box) float64 {
	return box.Margin.Top + box.Border.Top + box.Padding.Top +
		box.Height +
		box.Padding.Bottom + box.Border.Bottom + box.Margin.Bottom
}

// getTotalWidth returns the total width including margin, border, padding
func (le *LayoutEngine) getTotalWidth(box *Box) float64 {
	return box.Margin.Left + box.Border.Left + box.Padding.Left +
		box.Width +
		box.Padding.Right + box.Border.Right + box.Margin.Right
}

// computeShrinkToFitChildWidth computes the intrinsic margin-box width of a child box
// for use in shrink-to-fit calculations. For children with explicit width, uses their
// border-box width + margins. For auto-width block children, recursively computes
// the intrinsic width based on their content rather than their expanded width.
func (le *LayoutEngine) computeShrinkToFitChildWidth(box *Box) float64 {
	// Children with explicit width: use actual margin-box (box.Width is border-box)
	if box.Style != nil {
		if _, hasW := box.Style.GetLength("width"); hasW {
			return box.Margin.Left + box.Width + box.Margin.Right
		}
		if _, hasPct := box.Style.GetPercentage("width"); hasPct {
			return box.Margin.Left + box.Width + box.Margin.Right
		}
	}
	// Floats and inline-blocks have their own shrink-to-fit: use actual width
	if box.Style != nil && box.Style.GetFloat() != css.FloatNone {
		return box.Margin.Left + box.Width + box.Margin.Right
	}
	// Auto-width block child: compute intrinsic width from children
	if len(box.Children) == 0 {
		return box.Margin.Left + box.Width + box.Margin.Right
	}
	maxChild := 0.0
	for _, child := range box.Children {
		childWidth := le.computeShrinkToFitChildWidth(child)
		if childWidth > maxChild {
			maxChild = childWidth
		}
	}
	intrinsicWidth := maxChild + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right
	return box.Margin.Left + intrinsicWidth + box.Margin.Right
}

// adjustChildrenY recursively adjusts Y positions of all children by delta
func (le *LayoutEngine) adjustChildrenY(box *Box, delta float64) {
	for _, child := range box.Children {
		child.Y += delta
		le.adjustChildrenY(child, delta)
	}
}

// shiftChildren recursively shifts all children by dx, dy
func (le *LayoutEngine) shiftChildren(box *Box, dx, dy float64) {
	for _, child := range box.Children {
		child.X += dx
		child.Y += dy
		le.shiftChildren(child, dx, dy)
	}
}

// isFirstTextInBlock checks if this text node is the first text content
// in its block-level ancestor (for ::first-letter styling)
func (le *LayoutEngine) isFirstTextInBlock(node *html.Node, parent *Box) bool {
	if parent == nil || parent.Node == nil {
		return false
	}
	// Check if any text or element children came before this node
	for _, sibling := range parent.Node.Children {
		if sibling == node {
			return true // We reached ourselves first
		}
		if sibling.Type == html.TextNode && strings.TrimSpace(sibling.Text) != "" {
			return false // Another text node with content came first
		}
		if sibling.Type == html.ElementNode {
			return false // An element came first
		}
	}
	return false
}

// extractFirstLetter extracts the first letter from text (handling punctuation per CSS spec)
func extractFirstLetter(text string) (string, string) {
	text = strings.TrimLeft(text, " \t\n\r")
	if len(text) == 0 {
		return "", ""
	}
	// Get first rune
	runes := []rune(text)
	return string(runes[0]), string(runes[1:])
}

// parseURLValue parses url() values from CSS content property
func parseURLValue(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "url(") || !strings.HasSuffix(trimmed, ")") {
		return "", false
	}
	inner := trimmed[4 : len(trimmed)-1]
	inner = strings.TrimSpace(inner)
	// Remove optional quotes
	if len(inner) >= 2 && ((inner[0] == '\'' && inner[len(inner)-1] == '\'') || (inner[0] == '"' && inner[len(inner)-1] == '"')) {
		inner = inner[1 : len(inner)-1]
	}
	return inner, true
}

// parseQuotes parses the quotes property value
func parseQuotes(q string) []string {
	var quotes []string
	q = strings.TrimSpace(q)

	for len(q) > 0 {
		q = strings.TrimSpace(q)
		if len(q) == 0 {
			break
		}

		// Handle quoted strings
		if q[0] == '"' || q[0] == '\'' {
			quote := q[0]
			end := 1
			for end < len(q) && q[end] != quote {
				if q[end] == '\\' && end+1 < len(q) {
					end += 2
				} else {
					end++
				}
			}
			if end < len(q) {
				val := q[1:end]
				// Unescape Unicode escapes like \0022
				val = unescapeUnicode(val)
				quotes = append(quotes, val)
				q = q[end+1:]
			} else {
				break
			}
		} else {
			// Skip non-quoted content
			if idx := strings.IndexAny(q, " \t\"'"); idx > 0 {
				q = q[idx:]
			} else {
				break
			}
		}
	}

	return quotes
}

// unescapeUnicode converts CSS Unicode escapes like \0022 to actual characters
func unescapeUnicode(s string) string {
	result := s
	// Handle common escapes
	result = strings.ReplaceAll(result, "\\0022", "\"")
	result = strings.ReplaceAll(result, "\\0027", "'")
	result = strings.ReplaceAll(result, "\\00AB", "«")
	result = strings.ReplaceAll(result, "\\00BB", "»")
	return result
}

// getColspan returns the colspan attribute value (default 1)
func getColspan(node *html.Node) int {
	if colspan, ok := node.GetAttribute("colspan"); ok {
		if c, ok := css.ParseLength(colspan); ok {
			col := int(c)
			if col > 0 {
				return col
			}
		}
	}
	return 1
}

// getRowspan returns the rowspan attribute value (default 1)
func getRowspan(node *html.Node) int {
	if rowspan, ok := node.GetAttribute("rowspan"); ok {
		if r, ok := css.ParseLength(rowspan); ok {
			row := int(r)
			if row > 0 {
				return row
			}
		}
	}
	return 1
}
