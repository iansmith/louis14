package layout

import (
	"fmt"
	"louis14/pkg/css"
	"louis14/pkg/html"
)

func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
	fmt.Println("DEBUG: Layout() called - code is running!")

	// Phase 3: Compute styles from stylesheets
	// Phase 22: Pass viewport dimensions for media query evaluation
	computedStyles := css.ApplyStylesToDocument(doc, le.viewport.width, le.viewport.height)

	// Phase 11: Parse and store stylesheets for pseudo-element styling
	le.stylesheets = make([]*css.Stylesheet, 0)
	for _, cssText := range doc.Stylesheets {
		if stylesheet, err := css.ParseStylesheet(cssText); err == nil {
			le.stylesheets = append(le.stylesheets, stylesheet)
		}
	}

	// Phase 2: Recursively layout the tree starting from root's children
	boxes := make([]*Box, 0)
	y := 0.0

	// Phase 4: Track absolutely positioned boxes separately
	le.absoluteBoxes = make([]*Box, 0)

	// Phase 5: Initialize floats tracking
	le.floats = make([]FloatInfo, 0)

	var prevBox *Box // Track previous sibling for margin collapsing
	for _, node := range doc.Root.Children {
		if node.Type == html.ElementNode {
			fmt.Printf("DEBUG LAYOUT LOOP: Processing <%s>\n", node.TagName)
			box := le.layoutNode(node, 0, y, le.viewport.width, computedStyles, nil)
			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if box == nil {
				continue
			}
			boxes = append(boxes, box)

			// Phase 4 & 5: Only advance Y if element is in normal flow (not absolutely positioned or floated)
			floatType := box.Style.GetFloat()
			if box.Position != css.PositionAbsolute && box.Position != css.PositionFixed && floatType == css.FloatNone {
				// Margin collapsing between adjacent siblings
				if node.TagName == "div" && node.Attributes != nil {
					if id, ok := node.Attributes["id"]; ok && id == "div1" {
						fmt.Printf("DEBUG COLLAPSE CHECK: prevBox=%v, shouldCollapse(prev)=%v, shouldCollapse(box)=%v\n",
							prevBox != nil, prevBox != nil && shouldCollapseMargins(prevBox), shouldCollapseMargins(box))
					}
				}
				if prevBox != nil && shouldCollapseMargins(prevBox) && shouldCollapseMargins(box) {
					collapsed := collapseMargins(prevBox.Margin.Bottom, box.Margin.Top)
					// We already advanced by prevBox's full total height (including prevBox.Margin.Bottom)
					// and layoutNode already added box.Margin.Top to box.Y.
					// We need to pull back by the non-collapsed portion.
					adjustment := prevBox.Margin.Bottom + box.Margin.Top - collapsed
					fmt.Printf("DEBUG COLLAPSE: prevBox.MarginBottom=%.1f, box.MarginTop=%.1f, collapsed=%.1f, adjustment=%.1f\n",
						prevBox.Margin.Bottom, box.Margin.Top, collapsed, adjustment)
					fmt.Printf("DEBUG COLLAPSE: box.Y before=%.1f, after=%.1f\n", box.Y, box.Y-adjustment)
					box.Y -= adjustment
					le.adjustChildrenY(box, -adjustment)
				}
				y = box.Y + box.Border.Top + box.Padding.Top + box.Height + box.Padding.Bottom + box.Border.Bottom + box.Margin.Bottom
				prevBox = box
			}
		}
	}

	// Phase 4: Absolutely positioned boxes are already in the tree as children
	// of their containing blocks, so no need to add them separately.

	return boxes
}

