package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

func (le *LayoutEngine) Layout(doc *html.Document) []*Box {
	// Phase 3: Compute styles from stylesheets
	// Phase 22: Pass viewport dimensions for media query evaluation
	computedStyles := css.ApplyStylesToDocument(doc, le.viewport.width, le.viewport.height)

	// Apply HTML dir attribute → CSS direction/unicode-bidi mapping.
	// This handles the UA stylesheet rule [dir="rtl"] { direction: rtl; unicode-bidi: isolate; }
	// which is not in the CSS cascade but required by the HTML spec.
	// css.ApplyHTMLDirToTree(doc.Root, computedStyles)

	// Resolve CSS logical properties (border-inline-start, margin-block-end, etc.)
	// based on computed writing-mode and direction. The expandShorthand function in
	// the cascade assumes horizontal-tb + ltr; this fixes up for other modes.
	css.ResolveLogicalPropertiesInTree(doc.Root, computedStyles)

	// Phase 11: Parse and store stylesheets for pseudo-element styling
	le.stylesheets = make([]*css.Stylesheet, 0)
	ctx := css.NewParserContextFromDocument(doc)
	for _, source := range doc.Stylesheets {
		if stylesheet, err := css.ParseStylesheet(source.Text, ctx); err == nil {
			le.stylesheets = append(le.stylesheets, stylesheet)
		}
	}

	// Register @counter-style rules from all stylesheets for use during layout
	RegisterCounterStyles(le.stylesheets)

	// Phase 2: Recursively layout the tree starting from root's children
	boxes := make([]*Box, 0)
	y := 0.0

	// Phase 4: Track absolutely positioned boxes separately
	le.absoluteBoxes = make([]*Box, 0)

	// Phase 5: Initialize floats tracking
	le.floats = make([]FloatInfo, 0)

	// Initialize synthetic styles map for tree normalization
	le.syntheticStyles = make(map[*html.Node]*css.Style)

	// Detect root element's writing-mode and use the correct Dir for layout.
	rootDir := NewDir(HorizontalTB)
	for _, node := range doc.Root.Children {
		if node.Type == html.ElementNode {
			if style := computedStyles[node]; style != nil {
				rootDir = NewDir(WritingModeFromStyle(style))
			}
			break
		}
	}

	var prevBox *Box // Track previous sibling for margin collapsing
	for _, node := range doc.Root.Children {
		if node.Type == html.ElementNode {
			// For vertical writing modes, the available inline-size comes from the
			// viewport height, not width. Use Dir.ViewportInlineSize to get the
			// correct value. For HTB this returns viewport.width (unchanged).
			rootAvailInline := rootDir.ViewportInlineSize(le)
			box := le.layoutNode(node, 0, y, rootAvailInline, rootDir, computedStyles, nil)
			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if box == nil {
				continue
			}
			boxes = append(boxes, box)

			// CSS Writing Modes §7.1: The root element's box fills the
			// initial containing block (viewport). For vertical writing modes,
			// the block-size is the physical width. For VRL, block-start is
			// the right edge, so all content must be shifted rightward so that
			// the first column starts at the viewport's right edge.
			if rootDir.IsVertical() {
				viewportBlock := rootDir.ViewportBlockSize(le)
				if box.Width < viewportBlock {
					if rootDir.WM == VerticalRL {
						shift := viewportBlock - box.Width
						for _, child := range box.Children {
							child.X += shift
							shiftAllDescendants(child, shift, 0)
						}
					}
					box.Width = viewportBlock
				}

				// sideways-lr: inline-start is at the physical bottom.
				// Shift content to the bottom of the viewport so the first
				// inline content starts at the bottom edge.
				if box.Style != nil {
					wm, _ := box.Style.Get("writing-mode")
					if wm == "sideways-lr" {
						viewportInline := rootDir.ViewportInlineSize(le)
						// Position children so content bottom aligns with
						// viewport bottom minus margin (inline-start = bottom).
						desiredContentBottom := viewportInline - box.Margin.Bottom
						currentContentBottom := box.Y + box.Height
						shift := desiredContentBottom - currentContentBottom
						if shift > 0 {
							for _, child := range box.Children {
								child.Y += shift
								shiftAllDescendants(child, 0, shift)
							}
						}
						// Expand box to fill viewport (minus margins) for background painting.
						availH := viewportInline - box.Margin.Top - box.Margin.Bottom
						if box.Height < availH {
							box.Height = availH
						}
					}
				}
			}

			// Phase 4 & 5: Only advance Y if element is in normal flow (not absolutely positioned or floated)
			floatType := box.Style.GetFloat()
			if box.Position != css.PositionAbsolute && box.Position != css.PositionFixed && floatType == css.FloatNone {
				// Margin collapsing between adjacent siblings
				if prevBox != nil && shouldCollapseMargins(prevBox) && shouldCollapseMargins(box) {
					collapsed := collapseMargins(prevBox.Margin.Bottom, box.Margin.Top)
					// We already advanced by prevBox's full total height (including prevBox.Margin.Bottom)
					// and layoutNode already added box.Margin.Top to box.Y.
					// We need to pull back by the non-collapsed portion.
					adjustment := prevBox.Margin.Bottom + box.Margin.Top - collapsed
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

