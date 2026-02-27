package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
	"math"
	"sort"
)

func (le *LayoutEngine) layoutFlex(flexBox *Box, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style) {
	direction := flexBox.Style.GetFlexDirection()
	wrap := flexBox.Style.GetFlexWrap()
	justifyContent := flexBox.Style.GetJustifyContent()
	alignItems := flexBox.Style.GetAlignItems()
	alignContent := flexBox.Style.GetAlignContent()


	isRow := direction == css.FlexDirectionRow || direction == css.FlexDirectionRowReverse
	isReverse := direction == css.FlexDirectionRowReverse || direction == css.FlexDirectionColumnReverse
	isWrapReverse := wrap == css.FlexWrapWrapReverse

	// CSS Flexbox §9.1 + CSS Writing Modes §6.4: flex-direction: row follows the
	// inline direction of the writing mode. In vertical writing modes (vertical-rl,
	// vertical-lr), the inline direction is vertical, so "row" maps to the block
	// axis (column-like) and "column" maps to the inline axis (row-like).
	isVerticalWM := false
	if wm, ok := flexBox.Style.Get("writing-mode"); ok {
		if wm == "vertical-rl" || wm == "vertical-lr" {
			isVerticalWM = true
			isRow = !isRow
		}
	}

	// CSS Flexbox §4.5: direction:rtl flips the main axis for row direction.
	// row + rtl = items flow right-to-left (same as row-reverse + ltr)
	// row-reverse + rtl = items flow left-to-right (same as row + ltr)
	isRTL := flexBox.Style.GetDirection() == css.DirectionRTL
	if isRow && isRTL {
		isReverse = !isReverse
	}

	// CSS Box Alignment §6.1: left/right only apply to the inline axis.
	// For row direction (inline axis = main), left→flex-start, right→flex-end.
	// For column direction (inline axis ≠ main), left/right fall back to "start"
	// (physical start of the main axis). "start" = flex-start for non-reverse,
	// but flex-end for reverse (since reverse flips where flex-start is).
	// When the effective direction is reversed (row-reverse LTR or row RTL),
	// physical left/right swap relative to flex-start/flex-end.
	if justifyContent == css.JustifyContentLeft {
		if isRow && isReverse {
			justifyContent = css.JustifyContentFlexEnd
		} else if !isRow && isReverse {
			// column-reverse: "start" = physical top = flex-end
			justifyContent = css.JustifyContentFlexEnd
		} else {
			justifyContent = css.JustifyContentFlexStart
		}
	} else if justifyContent == css.JustifyContentRight {
		if isRow && isReverse {
			justifyContent = css.JustifyContentFlexStart
		} else if isRow {
			justifyContent = css.JustifyContentFlexEnd
		} else if isReverse {
			// column-reverse: "start" = physical top = flex-end
			justifyContent = css.JustifyContentFlexEnd
		} else {
			justifyContent = css.JustifyContentFlexStart
		}
	}

	// Main-axis margin helpers for direction-agnostic positioning.
	// In reverse directions, main-start is the physical end (right for row, bottom for column).
	mainStartMargin := func(box *Box) float64 {
		if isRow {
			if isReverse {
				return box.Margin.Right
			}
			return box.Margin.Left
		}
		if isReverse {
			return box.Margin.Bottom
		}
		return box.Margin.Top
	}
	mainEndMargin := func(box *Box) float64 {
		if isRow {
			if isReverse {
				return box.Margin.Left
			}
			return box.Margin.Right
		}
		if isReverse {
			return box.Margin.Top
		}
		return box.Margin.Bottom
	}
	mainBoxSize := func(box *Box) float64 {
		if isRow {
			return box.Width
		}
		return box.Height
	}

	// Container content box dimensions (inside padding+border)
	contentBoxWidth := flexBox.Width - flexBox.Padding.Left - flexBox.Padding.Right - flexBox.Border.Left - flexBox.Border.Right
	contentBoxHeight := flexBox.Height - flexBox.Padding.Top - flexBox.Padding.Bottom - flexBox.Border.Top - flexBox.Border.Bottom

	// Determine main axis available size
	var mainSize, crossSize float64
	hasDefiniteCross := false
	if isRow {
		mainSize = contentBoxWidth
		if contentBoxHeight > 0 {
			crossSize = contentBoxHeight
			hasDefiniteCross = true
		}
	} else {
		if contentBoxHeight > 0 {
			mainSize = contentBoxHeight
		} else {
			mainSize = math.MaxFloat64 // indefinite
		}
		// Only treat cross size as definite if there's an explicit width.
		// In vertical writing mode (where isRow was swapped), the physical width
		// is the block dimension and should be auto-sized from content when not
		// explicitly set — don't treat parent-inherited width as definite.
		if _, hasExplicitWidth := flexBox.Style.GetLength("width"); hasExplicitWidth {
			crossSize = contentBoxWidth
			hasDefiniteCross = true
		} else if _, hasExplicitPctWidth := flexBox.Style.GetPercentage("width"); hasExplicitPctWidth {
			crossSize = contentBoxWidth
			hasDefiniteCross = true
		} else if contentBoxWidth > 0 && !isVerticalWM {
			crossSize = contentBoxWidth
			hasDefiniteCross = true
		}
	}

	// Get gap values
	rowGap := 0.0
	colGap := 0.0
	if val, ok := flexBox.Style.Get("row-gap"); ok {
		if g, ok := css.ParseLengthWithFontSize(val, flexBox.Style.GetFontSize()); ok {
			rowGap = g
		}
	}
	if val, ok := flexBox.Style.Get("column-gap"); ok {
		if pct, ok := css.ParsePercentage(val); ok {
			// column-gap percentages always resolve against the inline size (width)
			colGap = contentBoxWidth * pct / 100
		} else if g, ok := css.ParseLengthWithFontSize(val, flexBox.Style.GetFontSize()); ok {
			colGap = g
		}
	}
	// For flex, column-gap is the main-axis gap (row direction), row-gap is cross-axis gap
	var mainGap, crossGap float64
	if isRow {
		mainGap = colGap
		crossGap = rowGap
	} else {
		mainGap = rowGap
		crossGap = colGap
	}

	// Step 1: Create flex items by laying out children to get intrinsic sizes
	contentStartX := flexBox.X + flexBox.Border.Left + flexBox.Padding.Left
	contentStartY := flexBox.Y + flexBox.Border.Top + flexBox.Padding.Top
	items := le.createFlexItemsProper(flexBox, contentStartX, contentStartY, contentBoxWidth, computedStyles, isRow)

	// Step 2: Sort by order property
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	// Step 3: Determine flex base size and hypothetical main size for each item
	for _, item := range items {
		basisVal := item.Box.Style.GetFlexBasisValue()
		if basisVal.IsAuto {
			// flex-basis: auto → use the item's main size property
			if isRow {
				if w, ok := item.Box.Style.GetLength("width"); ok {
					item.FlexBasis = w
				} else if pct, ok := item.Box.Style.GetPercentage("width"); ok {
					// Resolve percentage width against flex container main size.
					// GetLength does not resolve percentages; handle them here.
					item.FlexBasis = pct / 100.0 * mainSize
				} else if item.Box.Node != nil && item.Box.Node.Type == html.TextNode {
					// Anonymous text flex item: Box.Width is already the measured
					// width of the trimmed (non-whitespace) text content.
					item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
				} else {
					// CSS Flexbox §9.2: for flex-basis:auto with no definite main size,
					// use the item's max-content main size. For block-level grid containers
					// the layout width fills available, but the actual content width (track
					// sum) is the correct flex-basis. Derive it from the rightmost child edge.
					childDisplay := item.Box.Style.GetDisplay()
					if childDisplay == css.DisplayGrid || childDisplay == css.DisplayInlineGrid {
						maxRight := 0.0
						contentLeft := item.Box.X + item.Box.Border.Left + item.Box.Padding.Left
						for _, child := range item.Box.Children {
							right := child.X - contentLeft + child.Width +
								child.Margin.Left + child.Margin.Right +
								child.Border.Left + child.Border.Right +
								child.Padding.Left + child.Padding.Right
							if right > maxRight {
								maxRight = right
							}
						}
						item.FlexBasis = maxRight
					} else if item.Box.Node != nil && isReplacedElement(item.Box.Node.TagName) {
						// Replaced elements (img, canvas, video, iframe, svg): block layout
						// already applied aspect-ratio transfer and CSS height/max-width constraints
						// to item.Box.Width. ComputeIntrinsicSizes returns raw file dimensions,
						// which is wrong when height + aspect-ratio determine the displayed width.
						item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
						if item.FlexBasis < 0 {
							item.FlexBasis = 0
						}
					} else {
						// CSS Flexbox §9.2: flex-basis:auto with no explicit width = max-content
						// intrinsic size. For block-level items, item.Box.Width == container width
						// (block layout fills available space), which is wrong.
						//
						// Exception: items with vertical writing-mode (vertical-rl, vertical-lr).
						// createFlexItemsProper already applied transformToVerticalRL to item.Box,
						// so item.Box.Width reflects the transform-aware width (not block-fill).
						// ComputeIntrinsicSizes ignores writing-mode transforms and returns the
						// wrong (pre-transform) width. Use item.Box.Width directly instead.
						itemWM, _ := item.Box.Style.Get("writing-mode")
						if itemWM == "vertical-rl" || itemWM == "vertical-lr" {
							// Use the transform-aware laid-out width; children are already correct.
							item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
							if item.FlexBasis < 0 {
								item.FlexBasis = 0
							}
						} else {
							intrinsicSizes := le.ComputeIntrinsicSizes(item.Box.Node, item.Box.Style, computedStyles)
							item.FlexBasis = intrinsicSizes.MaxContent - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
							if item.FlexBasis < 0 {
								item.FlexBasis = 0
							}
							// Mark: initial layout used block-fill (container width), not intrinsic size.
							// Step 5c will re-layout children after flex resolution.
							item.BlockFillBasis = true
						}
					}
				}
			} else {
				if h, ok := item.Box.Style.GetLength("height"); ok {
					item.FlexBasis = h
				} else {
					item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
				}
			}
		} else if basisVal.IsPercent {
			item.FlexBasis = mainSize * basisVal.Percentage / 100
		} else if basisVal.IsCalc {
			// Resolve calc() expression with mainSize as the percentage base
			if resolved, ok := css.EvalCalcWithPercent(basisVal.CalcExpr, basisVal.FontSize, mainSize); ok {
				item.FlexBasis = resolved
			} else {
				// Fallback: treat as auto
				if isRow {
					item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
				} else {
					item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
				}
			}
		} else {
			item.FlexBasis = basisVal.Length
		}

		// Hypothetical main size = flex base size clamped by min/max
		item.HypotheticalMainSize = item.FlexBasis
		// Clamp by min-width: auto
		if item.HypotheticalMainSize < item.AutoMinMain {
			item.HypotheticalMainSize = item.AutoMinMain
		}
		if item.HypotheticalMainSize < 0 {
			item.HypotheticalMainSize = 0
		}
	}

	// Step 3b: For shrink-to-fit flex containers (float, inline-flex, abs pos without
	// explicit width/height), compute the ideal main size from items.
	// CSS Flexbox §9.2: The flex container's main size is its max-content size if
	// the main size property would be auto.
	// Only applies to intrinsic-sizing contexts: inline-flex, floated, or abs/fixed positioned.
	// Block-level display:flex containers use the available width from their parent.
	isShrinkToFit := false
	containerDisplay := flexBox.Style.GetDisplay()
	if containerDisplay == css.DisplayInlineFlex {
		isShrinkToFit = true
	} else if flexBox.Style.GetFloat() != css.FloatNone {
		isShrinkToFit = true
	} else if flexBox.Style.GetPosition() == css.PositionAbsolute || flexBox.Style.GetPosition() == css.PositionFixed {
		isShrinkToFit = true
	}
	if isRow && isShrinkToFit {
		if _, hasExplicitWidth := flexBox.Style.GetLength("width"); !hasExplicitWidth {
			if _, hasExplicitPctWidth := flexBox.Style.GetPercentage("width"); !hasExplicitPctWidth {
				// Compute max-content width: sum of all item outer hypothetical main sizes + gaps
				maxContentWidth := 0.0
				for i, item := range items {
					maxContentWidth += item.HypotheticalMainSize + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
					if i > 0 {
						maxContentWidth += mainGap
					}
				}
				// Apply min/max-width constraints
				if minW, ok := flexBox.Style.GetLength("min-width"); ok && maxContentWidth < minW {
					maxContentWidth = minW
				}
				if maxW, ok := flexBox.Style.GetLength("max-width"); ok && maxContentWidth > maxW {
					maxContentWidth = maxW
				}
				// Update container and main size
				contentBoxWidth = maxContentWidth
				flexBox.Width = maxContentWidth + flexBox.Padding.Left + flexBox.Padding.Right + flexBox.Border.Left + flexBox.Border.Right
				mainSize = contentBoxWidth
			}
		}
	}

	// Step 4: Collect items into flex lines
	lines := collectFlexLines(items, mainSize, mainGap, wrap, isRow)

	// Record each item's main-axis border-box size BEFORE flex resolution.
	// After resolution, block items whose size changed need children re-layout.
	type itemInitialSize struct{ w, h float64 }
	initialSizes := make(map[*FlexItem]itemInitialSize, len(items))
	for _, item := range items {
		initialSizes[item] = itemInitialSize{item.Box.Width, item.Box.Height}
	}

	// Step 5: Resolve flexible lengths for each line
	for _, line := range lines {
		resolveFlexibleLengths(line, mainSize, mainGap, isRow)
	}

	// Step 5b: Re-layout grid items whose main-axis size was established by flex resolution.
	// CSS Flexbox §9.4: after flex lengths resolve, grid containers need a second layout pass
	// with their now-definite main size so that auto tracks fill the container correctly.
	for _, line := range lines {
		for _, item := range line.Items {
			childDisplay := item.Box.Style.GetDisplay()
			if childDisplay != css.DisplayGrid && childDisplay != css.DisplayInlineGrid {
				continue
			}
			if item.Box.Node == nil {
				continue
			}
			// Compute the content-area main size (height for column, width for row)
			var contentMain float64
			if isRow {
				contentMain = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
			} else {
				contentMain = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
			}
			if contentMain <= 0 {
				continue
			}
			// Re-layout the grid with the established main size.
			// Pass item.Box.Width (border-box) as availableWidth so layoutGridContainer
			// correctly subtracts the grid's own padding+border to get content width.
			item.Box.Children = item.Box.Children[:0]
			if !isRow {
				// Column direction: main axis is height
				newBox := le.layoutGridContainer(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, contentMain, item.Box.Style, computedStyles, flexBox)
				item.Box.Children = newBox.Children
			} else {
				// Row direction: main axis is width — re-layout with established width
				newBox := le.layoutGridContainer(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, 0, item.Box.Style, computedStyles, flexBox)
				item.Box.Children = newBox.Children
			}
		}
	}

	// Step 5c: Re-layout block items that used block-fill (container width) for initial
	// layout, but got a different (smaller) main size after flex resolution.
	// Only fires for items where flex-basis:auto with no explicit CSS width triggered
	// ComputeIntrinsicSizes (BlockFillBasis=true). These items's children were laid out
	// at the container width and need re-layout at the correct intrinsic-based flex size.
	// Items with explicit CSS widths or explicit flex-basis are correct as-is.
	if isRow {
		for _, line := range lines {
			for _, item := range line.Items {
				// Only items where block-fill initial layout was wrong.
				if !item.BlockFillBasis {
					continue
				}
				if item.Box.Node == nil || len(item.Box.Children) == 0 {
					continue
				}
				// Width changed: re-layout children with the established width.
				// Save and restore float/abspos state to prevent double-registration.
				savedFloatsLen := len(le.floats)
				savedAbsLen := len(le.absoluteBoxes)
				newBox := le.layoutNode(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, computedStyles, flexBox)
				le.floats = le.floats[:savedFloatsLen]
				le.absoluteBoxes = le.absoluteBoxes[:savedAbsLen]
				if newBox != nil {
					item.Box.Children = newBox.Children
					// Do not update item.Box.Height; cross-axis is determined by Step 6/8.
				}
			}
		}
	}

	// Step 6: Determine cross sizes
	for _, line := range lines {
		for _, item := range line.Items {
			// Apply explicit min cross-size constraints (CSS Flexbox §9.4).
			// min-height (row) or min-width (column) on flex items must be enforced
			// regardless of align-items — stretch handles its own clamping in Step 8.
			if isRow {
				if minH, ok := item.Box.Style.GetLength("min-height"); ok {
					contentH := item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
					if contentH < minH {
						item.Box.Height = minH + item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
						// Mark as definite so percentage-height children can resolve against
						// item.Box.Height in layout_block.go (CSS Flexbox §9.8).
						item.Box.HeightIsDefinite = true
						// Re-layout children that depend on the newly-established height.
						childDisplay := item.Box.Style.GetDisplay()
						if (childDisplay == css.DisplayGrid || childDisplay == css.DisplayInlineGrid) && item.Box.Node != nil {
							item.Box.Children = item.Box.Children[:0]
							newBox := le.layoutGridContainer(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, minH, item.Box.Style, computedStyles, flexBox)
							item.Box.Children = newBox.Children
						} else if childDisplay == css.DisplayFlex || childDisplay == css.DisplayInlineFlex {
							item.Box.Children = item.Box.Children[:0]
							le.layoutFlex(item.Box, item.Box.X, item.Box.Y, item.Box.Width, computedStyles)
						}
					}
				}
			} else {
				if minW, ok := item.Box.Style.GetLength("min-width"); ok {
					contentW := item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
					if contentW < minW {
						item.Box.Width = minW + item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
						childDisplay := item.Box.Style.GetDisplay()
						if (childDisplay == css.DisplayGrid || childDisplay == css.DisplayInlineGrid) && item.Box.Node != nil {
							item.Box.Children = item.Box.Children[:0]
							newBox := le.layoutGridContainer(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, 0, item.Box.Style, computedStyles, flexBox)
							item.Box.Children = newBox.Children
						} else if childDisplay == css.DisplayFlex || childDisplay == css.DisplayInlineFlex {
							item.Box.Children = item.Box.Children[:0]
							le.layoutFlex(item.Box, item.Box.X, item.Box.Y, item.Box.Width, computedStyles)
						}
					}
				}
			}
			item.CrossSize = item.outerCrossSize(isRow)
		}
		// Line cross size = max item cross size
		maxCross := 0.0
		for _, item := range line.Items {
			if item.CrossSize > maxCross {
				maxCross = item.CrossSize
			}
		}
		line.CrossSize = maxCross
	}

	// Single-line container with definite cross size: use container's cross size
	if wrap == css.FlexWrapNowrap && hasDefiniteCross && len(lines) == 1 {
		lines[0].CrossSize = crossSize
	}

	// Step 7: Handle align-content: stretch for multi-line
	totalLinesCross := 0.0
	for i, line := range lines {
		totalLinesCross += line.CrossSize
		if i > 0 {
			totalLinesCross += crossGap
		}
	}
	if hasDefiniteCross && alignContent == css.AlignContentStretch && wrap != css.FlexWrapNowrap {
		freeSpace := crossSize - totalLinesCross
		if freeSpace > 0 {
			extra := freeSpace / float64(len(lines))
			for _, line := range lines {
				line.CrossSize += extra
			}
			totalLinesCross = crossSize
		}
	}

	// Step 8: Handle align-items: stretch for each item
	// CSS Flexbox §8.2: Only stretch if the item's cross-size property is auto
	for _, line := range lines {
		for _, item := range line.Items {
			alignment := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())
			if alignment == css.AlignItemsStretch {
				// Check if item has explicit cross-size (stretch only applies to auto)
				hasExplicitCrossSize := false
				if isRow {
					if _, ok := item.Box.Style.GetLength("height"); ok {
						hasExplicitCrossSize = true
					}
				} else {
					if _, ok := item.Box.Style.GetLength("width"); ok {
						hasExplicitCrossSize = true
					}
				}
				if hasExplicitCrossSize {
					continue
				}
				outerCross := item.outerCrossSize(isRow)
				// CSS Flexbox: for items with aspect-ratio and no explicit CSS cross size,
				// also stretch when the item is TALLER than the line (initial layout
				// applied aspect-ratio to auto-width before cross size was established).
				arItem := item.Box.Style.GetAspectRatio()
				shouldStretch := outerCross < line.CrossSize ||
					(arItem.IsSet && outerCross != line.CrossSize)
				if shouldStretch {
					// Stretch item to fill line's cross size
					crossMargin := 0.0
					if isRow {
						crossMargin = item.Box.Margin.Top + item.Box.Margin.Bottom
						oldHeight := outerCross - crossMargin
						newHeight := line.CrossSize - crossMargin
						item.Box.Height = newHeight
						// Mark as definite so percentage-height children can resolve against
						// item.Box.Height in layout_block.go (CSS Flexbox §9.8 / §6.2).
						item.Box.HeightIsDefinite = true
						// Aspect ratio transfer: for replaced elements (img, canvas, video) without
						// explicit CSS width, update width to maintain the natural aspect ratio when
						// height is established by stretch alignment (CSS Flexbox / CSS Sizing §4.2).
						if item.Box.Node != nil {
							tag := item.Box.Node.TagName
							isReplaced := tag == "img" || tag == "canvas" || tag == "video"
							if isReplaced {
								_, hasExplicitW := item.Box.Style.GetLength("width")
								if !hasExplicitW {
									oldContentH := oldHeight - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
									oldContentW := item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
									if oldContentH > 0 && oldContentW > 0 {
										newContentH := newHeight - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
										ratio := oldContentW / oldContentH
										newContentW := newContentH * ratio
										item.Box.Width = newContentW + item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
									}
								}
							}
						}
					} else {
						crossMargin = item.Box.Margin.Left + item.Box.Margin.Right
						newWidth := line.CrossSize - crossMargin
						item.Box.Width = newWidth
					}
					// CSS aspect-ratio: for elements with CSS aspect-ratio property and no explicit
					// width, transfer the stretched height to width via the ratio.
					// CSS Sizing Level 4: aspect-ratio maps preferred width from resolved height.
					if isRow {
						arProp := item.Box.Style.GetAspectRatio()
						if arProp.IsSet && arProp.Width > 0 && arProp.Height > 0 {
							_, hasExplicitW := item.Box.Style.GetLength("width")
							if !hasExplicitW {
								paddingH := item.Box.Padding.Top + item.Box.Padding.Bottom
								borderH := item.Box.Border.Top + item.Box.Border.Bottom
								paddingW := item.Box.Padding.Left + item.Box.Padding.Right
								borderW := item.Box.Border.Left + item.Box.Border.Right
								var newBorderBoxW float64
								if item.Box.Style.GetBoxSizing() == "border-box" {
									newBorderBoxW = item.Box.Height * arProp.Width / arProp.Height
								} else {
									contentH := item.Box.Height - paddingH - borderH
									if contentH < 0 {
										contentH = 0
									}
									contentW := contentH * arProp.Width / arProp.Height
									newBorderBoxW = contentW + paddingW + borderW
								}
								item.Box.Width = newBorderBoxW
							}
						}
					}

					item.CrossSize = line.CrossSize

					// CSS Flexbox §9.4: After stretching, re-layout the item's
					// contents with the new definite cross size so that children
					// that depend on it (e.g. nested flex with flex:1) resolve correctly.
					childDisplay := item.Box.Style.GetDisplay()
					if childDisplay == css.DisplayFlex || childDisplay == css.DisplayInlineFlex {
						item.Box.Children = item.Box.Children[:0]
						le.layoutFlex(item.Box, item.Box.X, item.Box.Y, item.Box.Width, computedStyles)
					} else if (childDisplay == css.DisplayGrid || childDisplay == css.DisplayInlineGrid) && item.Box.Node != nil {
						item.Box.Children = item.Box.Children[:0]
						// Pass item.Box.Width (border-box) as availableWidth; layoutGridContainer
						// subtracts the grid's padding+border to get content width.
						var establishedH float64
						if isRow {
							// Row direction: cross axis is height — newly stretched
							// establishedH is content height (border-box - padding - border)
							establishedH = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
						}
						newBox := le.layoutGridContainer(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, establishedH, item.Box.Style, computedStyles, flexBox)
						item.Box.Children = newBox.Children
					}
				}
			}
		}
	}

	// Step 8c: For shrink-to-fit containers (inline-flex, floated, abs-pos), recompute the
	// container's main-axis size after Step 8 aspect ratio transfers may have changed item widths.
	// Items may grow (replaced element stretched height → larger width) or shrink (non-replaced
	// element with aspect-ratio had initial width = available width, now corrected by ratio).
	if isRow && isShrinkToFit {
		if _, hasExplicitWidth := flexBox.Style.GetLength("width"); !hasExplicitWidth {
			newTotalMain := 0.0
			for i, item := range items {
				newTotalMain += item.outerMainSize(isRow)
				if i > 0 {
					newTotalMain += mainGap
				}
			}
			// Apply min/max-width to the recomputed total
			if minW, ok := flexBox.Style.GetLength("min-width"); ok && newTotalMain < minW {
				newTotalMain = minW
			}
			if maxW, ok := flexBox.Style.GetLength("max-width"); ok && newTotalMain > maxW {
				newTotalMain = maxW
			}
			// Always update the container size to reflect the post-stretch item widths.
			contentBoxWidth = newTotalMain
			mainSize = contentBoxWidth
			flexBox.Width = newTotalMain + flexBox.Padding.Left + flexBox.Padding.Right + flexBox.Border.Left + flexBox.Border.Right
		}
	}

	// Step 8b: Resolve auto margins on the main axis (CSS Flexbox §8.1)
	// Auto margins absorb remaining free space BEFORE justify-content.
	for _, line := range lines {
		totalItemsMain := 0.0
		for i, item := range line.Items {
			totalItemsMain += item.outerMainSize(isRow)
			if i > 0 {
				totalItemsMain += mainGap
			}
		}
		freeSpace := mainSize - totalItemsMain
		// For indefinite main size (auto-height column containers), the container
		// shrink-wraps to content, so there's no free space to distribute.
		if mainSize == math.MaxFloat64 {
			freeSpace = 0
		}

		// Preserve original free space for justify-content fallback detection
		originalFreeSpace := freeSpace
		if freeSpace < 0 {
			freeSpace = 0
		}

		// Count auto margins on main axis
		autoMarginCount := 0
		for _, item := range line.Items {
			margin := item.Box.Style.GetMargin()
			if isRow {
				if margin.AutoLeft {
					autoMarginCount++
				}
				if margin.AutoRight {
					autoMarginCount++
				}
			} else {
				if margin.AutoTop {
					autoMarginCount++
				}
				if margin.AutoBottom {
					autoMarginCount++
				}
			}
		}

		if autoMarginCount > 0 && freeSpace > 0 {
			// Distribute free space to auto margins
			autoMarginSize := freeSpace / float64(autoMarginCount)
			for _, item := range line.Items {
				margin := item.Box.Style.GetMargin()
				if isRow {
					if margin.AutoLeft {
						item.Box.Margin.Left = autoMarginSize
					}
					if margin.AutoRight {
						item.Box.Margin.Right = autoMarginSize
					}
				} else {
					if margin.AutoTop {
						item.Box.Margin.Top = autoMarginSize
					}
					if margin.AutoBottom {
						item.Box.Margin.Bottom = autoMarginSize
					}
				}
			}
			// Recalculate freeSpace (should be 0 now)
			freeSpace = 0
		}

		// Step 9: Main-axis alignment (justify-content)
		// CSS Flexbox §8.2: flex-end and center use the actual free space (even if negative).
		// space-between/space-around/space-evenly fall back to flex-start when
		// free space is negative (overflow) or there's only one item.
		var initialOffset, spacing float64
		switch justifyContent {
		case css.JustifyContentFlexStart:
			initialOffset = 0
		case css.JustifyContentFlexEnd:
			initialOffset = originalFreeSpace
		case css.JustifyContentCenter:
			initialOffset = originalFreeSpace / 2
		case css.JustifyContentSpaceBetween:
			// Fall back to flex-start if overflow or single item
			if originalFreeSpace < 0 || len(line.Items) == 1 {
				initialOffset = 0 // flex-start
			} else if len(line.Items) > 1 {
				spacing = freeSpace / float64(len(line.Items)-1)
			}
		case css.JustifyContentSpaceAround:
			// Fall back to flex-start if overflow, center if single item
			if originalFreeSpace < 0 {
				initialOffset = 0 // flex-start
			} else if len(line.Items) == 1 {
				initialOffset = freeSpace / 2 // center
			} else if len(line.Items) > 0 {
				spacing = freeSpace / float64(len(line.Items))
				initialOffset = spacing / 2
			}
		case css.JustifyContentSpaceEvenly:
			// Fall back to flex-start if overflow, center if single item
			if originalFreeSpace < 0 {
				initialOffset = 0 // flex-start
			} else if len(line.Items) == 1 {
				initialOffset = freeSpace / 2 // center
			} else if len(line.Items) > 0 {
				spacing = freeSpace / float64(len(line.Items)+1)
				initialOffset = spacing
			}
		}

		currentPos := initialOffset
		for i, item := range line.Items {
			item.MainPos = currentPos + mainStartMargin(item.Box)
			currentPos += item.outerMainSize(isRow) + spacing
			if i < len(line.Items)-1 {
				currentPos += mainGap
			}
		}
	}

	// Step 10: Cross-axis alignment
	currentCrossPos := 0.0

	// Align content (distribute lines along cross axis)
	if hasDefiniteCross && (len(lines) > 1 || wrap != css.FlexWrapNowrap) {
		freeSpace := crossSize - totalLinesCross
		if freeSpace < 0 {
			freeSpace = 0
		}
		var lineOffsets []float64
		switch alignContent {
		case css.AlignContentFlexStart, css.AlignContentStretch:
			pos := 0.0
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentFlexEnd:
			pos := freeSpace
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentCenter:
			pos := freeSpace / 2
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentSpaceBetween:
			lineSpacing := 0.0
			if len(lines) > 1 {
				lineSpacing = freeSpace / float64(len(lines)-1)
			}
			pos := 0.0
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize + lineSpacing
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		case css.AlignContentSpaceAround:
			lineSpacing := 0.0
			if len(lines) > 0 {
				lineSpacing = freeSpace / float64(len(lines))
			}
			pos := lineSpacing / 2
			for i, line := range lines {
				lineOffsets = append(lineOffsets, pos)
				pos += line.CrossSize + lineSpacing
				if i < len(lines)-1 {
					pos += crossGap
				}
			}
		}

		// Position items within lines using computed offsets
		for lineIdx, line := range lines {
			crossPos := 0.0
			if lineIdx < len(lineOffsets) {
				crossPos = lineOffsets[lineIdx]
			}
			positionItemsCrossAxis(line, crossPos, alignItems, isRow)
		}
	} else {
		// Single-line or no definite cross size
		for i, line := range lines {
			positionItemsCrossAxis(line, currentCrossPos, alignItems, isRow)
			currentCrossPos += line.CrossSize
			if i < len(lines)-1 {
				currentCrossPos += crossGap
			}
		}
	}

	if isWrapReverse && len(lines) > 1 {
		// Reverse line order along cross axis
		totalCross := 0.0
		for i, line := range lines {
			totalCross += line.CrossSize
			if i > 0 {
				totalCross += crossGap
			}
		}
		for _, line := range lines {
			for _, item := range line.Items {
				item.CrossPos = totalCross - item.CrossPos - item.CrossSize
			}
		}
	}

	// Step 11b: Adjust for overflow with justify-content: space-between in reverse directions.
	// CSS WG issue #11937 (tentative): When justify-content falls back to flex-start
	// due to overflow in a reverse direction, shift items to show their content
	// at main-start rather than empty space at the item's trailing edge.
	if isReverse && justifyContent == css.JustifyContentSpaceBetween {
		for _, line := range lines {
			totalItemsMain := 0.0
			for i, item := range line.Items {
				totalItemsMain += item.outerMainSize(isRow)
				if i > 0 {
					totalItemsMain += mainGap
				}
			}
			if totalItemsMain <= mainSize {
				continue
			}
			for _, item := range line.Items {
				if len(item.Box.Children) == 0 {
					continue
				}
				var itemContentSize, childrenSize float64
				if isRow {
					itemContentSize = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
					for _, child := range item.Box.Children {
						childrenSize += child.Width + child.Margin.Left + child.Margin.Right
					}
				} else {
					itemContentSize = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
					for _, child := range item.Box.Children {
						childrenSize += child.Height + child.Margin.Top + child.Margin.Bottom
					}
				}
				emptySpace := itemContentSize - childrenSize
				if emptySpace > 0 {
					item.MainPos -= emptySpace
				}
			}
		}
	}

	// Step 12: Set final box positions (direction-aware coordinate mapping).
	// For reverse directions, convert abstract main-start positions to physical coordinates.
	effectiveMainSize := mainSize
	if isReverse && mainSize == math.MaxFloat64 {
		effectiveMainSize = 0
		for _, line := range lines {
			for _, item := range line.Items {
				itemEnd := item.MainPos + mainBoxSize(item.Box) + mainEndMargin(item.Box)
				if itemEnd > effectiveMainSize {
					effectiveMainSize = itemEnd
				}
			}
		}
	}

	flexBox.Children = flexBox.Children[:0]
	for _, line := range lines {
		for _, item := range line.Items {
			oldX := item.Box.X
			oldY := item.Box.Y
			if isRow {
				if isReverse {
					item.Box.X = contentStartX + effectiveMainSize - item.MainPos - item.Box.Width
				} else {
					item.Box.X = contentStartX + item.MainPos
				}
				item.Box.Y = contentStartY + item.CrossPos
	
			} else {
				item.Box.X = contentStartX + item.CrossPos
				if isReverse {
					item.Box.Y = contentStartY + effectiveMainSize - item.MainPos - item.Box.Height
				} else {
					item.Box.Y = contentStartY + item.MainPos
				}
			}
			// Re-position children relative to new box position
			deltaX := item.Box.X - oldX
			deltaY := item.Box.Y - oldY
			le.repositionFlexItemChildren(item.Box, deltaX, deltaY)
			flexBox.Children = append(flexBox.Children, item.Box)
		}
	}

	// Step 13: Update container auto width for column direction
	if !isRow && !hasDefiniteCross {
		totalCrossSize := 0.0
		for i, line := range lines {
			totalCrossSize += line.CrossSize
			if i > 0 {
				totalCrossSize += crossGap
			}
		}
		flexBox.Width = totalCrossSize + flexBox.Padding.Left + flexBox.Padding.Right + flexBox.Border.Left + flexBox.Border.Right
	}

	// Step 14: Update container auto height
	if !hasDefiniteCross || (isRow && contentBoxHeight == 0) {
		maxBottom := 0.0
		for _, child := range flexBox.Children {
			childBottom := child.Y + child.Height + child.Margin.Bottom - contentStartY
			if childBottom > maxBottom {
				maxBottom = childBottom
			}
		}
		if isRow {
			flexBox.Height = maxBottom + flexBox.Padding.Top + flexBox.Padding.Bottom + flexBox.Border.Top + flexBox.Border.Bottom
		}
	}
	if !isRow && mainSize == math.MaxFloat64 {
		maxBottom := 0.0
		for _, child := range flexBox.Children {
			childBottom := child.Y + child.Height + child.Margin.Bottom - contentStartY
			if childBottom > maxBottom {
				maxBottom = childBottom
			}
		}
		flexBox.Height = maxBottom + flexBox.Padding.Top + flexBox.Padding.Bottom + flexBox.Border.Top + flexBox.Border.Bottom
	}
}

// displayContentsIsSupressed returns true for elements where CSS Display Level 3 §B
// specifies that display:contents should be treated as display:none (suppressed).
// These are replaced elements, form controls, and other "unusual" HTML elements.
func displayContentsIsSuppressed(tagName string) bool {
	switch tagName {
	case "br", "wbr", "meter", "progress", "canvas", "embed", "object",
		"audio", "iframe", "img", "video", "input", "textarea", "select",
		"frame", "frameset", "col", "colgroup", "summary":
		return true
	}
	return false
}

// isReplacedElement returns true for elements whose dimensions come from their content
// (image file, video stream, etc.) rather than from CSS layout of their children.
// Used in flex-basis computation: replaced elements use block-layout width (which applies
// CSS height + aspect-ratio transfer), not ComputeIntrinsicSizes (which returns raw file dims).
func isReplacedElement(tagName string) bool {
	switch tagName {
	case "img", "canvas", "video", "iframe", "embed", "object", "picture", "svg":
		return true
	}
	return false
}

// flattenContentsChildren returns the children of node, recursively expanding any
// child with display:contents (those children participate directly in the parent layout).
// Per CSS Display Level 3 §B, display:contents is suppressed (treated as display:none)
// for replaced elements, form controls, and other unusual HTML elements.
func (le *LayoutEngine) flattenContentsChildren(node *html.Node, computedStyles map[*html.Node]*css.Style) []*html.Node {
	var result []*html.Node
	for _, child := range node.Children {
		if child.Type == html.ElementNode {
			childStyle := computedStyles[child]
			if childStyle == nil {
				childStyle = le.syntheticStyles[child]
			}
			if childStyle == nil {
				childStyle = css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
				computedStyles[child] = childStyle
			}
			if childStyle.GetDisplay() == css.DisplayContents {
				// Per CSS Display Level 3 §B: display:contents is suppressed to
				// display:none for replaced elements and certain other elements.
				// Skip these entirely (do not expand or include).
				if displayContentsIsSuppressed(child.TagName) {
					continue
				}
				result = append(result, le.flattenContentsChildren(child, computedStyles)...)
				continue
			}
		}
		result = append(result, child)
	}
	return result
}

// createFlexItemsProper creates flex items by laying out each child to get proper dimensions.
func (le *LayoutEngine) createFlexItemsProper(flexBox *Box, startX, startY, availableWidth float64, computedStyles map[*html.Node]*css.Style, isRow bool) []*FlexItem {
	items := make([]*FlexItem, 0)

	// Expand display:contents children so their children participate as direct flex items
	children := le.flattenContentsChildren(flexBox.Node, computedStyles)

	for _, child := range children {
		if child.Type == html.TextNode {
			// CSS Flexbox §4: Skip whitespace-only text runs (ASCII whitespace)
			textContent := child.Text
			trimmed := ""
			for _, c := range textContent {
				if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
					trimmed += string(c)
				}
			}
			if trimmed == "" {
				continue
			}

			// Create anonymous flex item for non-whitespace text (e.g., &nbsp;)
			containerStyle := flexBox.Style
			fontSize := containerStyle.GetFontSize()
			bold := containerStyle.GetFontWeight() == css.FontWeightBold
			italic := containerStyle.GetFontStyle() == css.FontStyleItalic
			mono := containerStyle.IsMonospaceFamily()
			ahem := containerStyle.IsAhemFamily()

			textWidth, textHeight := text.MeasureTextWithStyle(trimmed, fontSize, bold, italic, mono, ahem)

			anonStyle := css.NewStyle()
			anonStyle.Set("display", "block")
			// Inherit font/color from container
			if v, ok := containerStyle.Get("font-size"); ok {
				anonStyle.Set("font-size", v)
			}
			if v, ok := containerStyle.Get("font-weight"); ok {
				anonStyle.Set("font-weight", v)
			}
			if v, ok := containerStyle.Get("font-family"); ok {
				anonStyle.Set("font-family", v)
			}
			if v, ok := containerStyle.Get("color"); ok {
				anonStyle.Set("color", v)
			}

			childBox := &Box{
				Node:          child,
				Style:         anonStyle,
				X:             startX,
				Y:             startY,
				Width:         textWidth,
				Height:        textHeight,
				Children:      make([]*Box, 0),
				Parent:        flexBox,
				PseudoContent: trimmed,
			}

			item := &FlexItem{
				Box:        childBox,
				FlexGrow:   0,
				FlexShrink: 1,
				Order:      0,
			}
			items = append(items, item)
			continue
		}
		if child.Type != html.ElementNode {
			continue
		}

		childStyle := computedStyles[child]
		if childStyle == nil {
			childStyle = css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
			computedStyles[child] = childStyle
		}

		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		// CSS Flexbox §4: Absolutely-positioned flex items do not participate in
		// flex layout — they are laid out as absolute/fixed-positioned elements
		// and do not contribute to intrinsic sizes or gap spacing.
		childPos := childStyle.GetPosition()
		if childPos == css.PositionAbsolute || childPos == css.PositionFixed {
			// Still layout the child (adds it to absoluteBoxes for later positioning)
			le.layoutNode(child, flexBox.X, flexBox.Y, flexBox.Width, computedStyles, flexBox)
			continue
		}

		// CSS Flexbox §4: Blockification of flex items
		// Children of a flex container have their display value blockified:
		// inline → block, inline-block → block, inline-flex → flex
		// float and clear have no effect on flex items.
		display := childStyle.GetDisplay()
		if display == css.DisplayInline || display == css.DisplayInlineBlock {
			childStyle.Set("display", "block")
		} else if display == css.DisplayInlineFlex {
			childStyle.Set("display", "flex")
		}
		childStyle.Set("float", "none")
		childStyle.Set("clear", "none")

		// Layout the child to get its intrinsic dimensions
		childBox := le.layoutNode(child, startX, startY, availableWidth, computedStyles, flexBox)

		// CSS Writing Modes §6.4: In vertical writing mode, transform inline layout
		// from horizontal lines to vertical-rl columns. Each horizontal line becomes
		// a column stacking right-to-left, with content flowing top-to-bottom.
		// CSS Writing Modes §6.4: In vertical writing mode, transform inline layout
		// from horizontal lines to vertical-rl columns for auto-sized items.
		// Items with explicit width/height keep their specified dimensions.
		if wm, ok := flexBox.Style.Get("writing-mode"); ok {
			if wm == "vertical-rl" || wm == "vertical-lr" {
				_, hasExplicitW := childStyle.GetLength("width")
				_, hasExplicitH := childStyle.GetLength("height")
				if !hasExplicitW && !hasExplicitH {
					transformToVerticalRL(childBox)
				}
			}
		}

		item := &FlexItem{
			Box:        childBox,
			FlexGrow:   childStyle.GetFlexGrow(),
			FlexShrink: childStyle.GetFlexShrink(),
			Order:      childStyle.GetOrder(),
		}

		// Compute min-width: auto (CSS Flexbox §4.5)
		// For flex items with overflow: visible, min-width/min-height: auto
		// computes to the content-based minimum size
		overflow := "visible"
		if v, ok := childStyle.Get("overflow"); ok {
			overflow = v
		}
		hasExplicitMin := false
		if isRow {
			if _, ok := childStyle.GetLength("min-width"); ok {
				hasExplicitMin = true
			}
		} else {
			if _, ok := childStyle.GetLength("min-height"); ok {
				hasExplicitMin = true
			}
		}
		// CSS Flexbox §4.5: automatic minimum size applies when overflow is visible or clip.
		// Both treat the automatic minimum as the content-based minimum size.
		if !hasExplicitMin && (overflow == "visible" || overflow == "clip") {
			item.AutoMinMain = le.computeFlexItemAutoMinMain(child, childStyle, childBox, isRow)
		}

		items = append(items, item)
	}

	return items
}

// collectFlexLines collects flex items into lines based on wrapping rules.
func collectFlexLines(items []*FlexItem, mainSize, mainGap float64, wrap css.FlexWrap, isRow bool) []*FlexLine {
	if wrap == css.FlexWrapNowrap || len(items) == 0 {
		return []*FlexLine{{Items: items}}
	}

	var lines []*FlexLine
	currentLine := &FlexLine{Items: make([]*FlexItem, 0)}
	lineMainSize := 0.0

	for _, item := range items {
		itemMain := item.HypotheticalOuterMain(isRow)
		gapSize := 0.0
		if len(currentLine.Items) > 0 {
			gapSize = mainGap
		}
		if lineMainSize+gapSize+itemMain > mainSize && len(currentLine.Items) > 0 {
			lines = append(lines, currentLine)
			currentLine = &FlexLine{Items: make([]*FlexItem, 0)}
			lineMainSize = 0
			gapSize = 0
		}
		currentLine.Items = append(currentLine.Items, item)
		lineMainSize += gapSize + itemMain
	}
	if len(currentLine.Items) > 0 {
		lines = append(lines, currentLine)
	}
	return lines
}

// resolveFlexibleLengths implements CSS Flexbox spec Section 9.7.
func resolveFlexibleLengths(line *FlexLine, availableMain, mainGap float64, isRow bool) {
	if len(line.Items) == 0 {
		return
	}

	// CSS Flexbox §9.7: If the main size is indefinite, flex items use their
	// intrinsic sizes — there is no definite free space to distribute.
	if availableMain == math.MaxFloat64 {
		for _, item := range line.Items {
			// Items stay at their hypothetical main size (intrinsic)
			if isRow {
				item.Box.Width = item.HypotheticalMainSize + item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
			} else {
				item.Box.Height = item.HypotheticalMainSize + item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
			}
		}
		return
	}

	// Account for gaps between items
	totalGaps := mainGap * float64(len(line.Items)-1)
	effectiveAvailable := availableMain - totalGaps

	// Calculate sum of outer hypothetical main sizes
	sumHypothetical := 0.0
	for _, item := range line.Items {
		sumHypothetical += item.HypotheticalMainSize + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
	}

	// Determine whether we're growing or shrinking
	growing := sumHypothetical < effectiveAvailable

	// Freeze inflexible items
	type flexState struct {
		frozen    bool
		targetMain float64
	}
	states := make([]flexState, len(line.Items))
	for i, item := range line.Items {
		if growing && item.FlexGrow == 0 {
			states[i].frozen = true
			states[i].targetMain = item.HypotheticalMainSize
		} else if !growing && item.FlexShrink == 0 {
			states[i].frozen = true
			states[i].targetMain = item.HypotheticalMainSize
		} else {
			states[i].targetMain = item.HypotheticalMainSize
		}
	}

	// Iterative resolution loop
	for iteration := 0; iteration < 10; iteration++ {
		// Check if all frozen
		allFrozen := true
		for _, s := range states {
			if !s.frozen {
				allFrozen = false
				break
			}
		}
		if allFrozen {
			break
		}

		// Calculate remaining free space
		usedSpace := 0.0
		for i, item := range line.Items {
			if states[i].frozen {
				usedSpace += states[i].targetMain + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
			} else {
				usedSpace += item.FlexBasis + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
			}
		}
		freeSpace := effectiveAvailable - usedSpace

		// Distribute space
		if growing {
			totalGrowFactor := 0.0
			for i, item := range line.Items {
				if !states[i].frozen {
					totalGrowFactor += item.FlexGrow
				}
			}
			if totalGrowFactor > 0 {
				for i, item := range line.Items {
					if !states[i].frozen {
						states[i].targetMain = item.FlexBasis + freeSpace*(item.FlexGrow/totalGrowFactor)
					}
				}
			}
		} else {
			// Shrink: weighted by flex-shrink * flex-basis
			totalScaledShrink := 0.0
			for i, item := range line.Items {
				if !states[i].frozen {
					totalScaledShrink += item.FlexShrink * item.FlexBasis
				}
			}
			if totalScaledShrink > 0 {
				for i, item := range line.Items {
					if !states[i].frozen {
						scaledFactor := item.FlexShrink * item.FlexBasis / totalScaledShrink
						states[i].targetMain = item.FlexBasis + freeSpace*scaledFactor
					}
				}
			}
		}

		// Clamp by min/max (explicit and auto) and detect violations.
		// CSS Flexbox §9.7: after distributing space, clamp each item's size by
		// its min/max constraints, then freeze violating items.
		totalViolation := 0.0
		for i, item := range line.Items {
			if states[i].frozen {
				continue
			}
			clamped := states[i].targetMain
			// Clamp by explicit min-width/min-height
			if isRow {
				if minW, ok := item.Box.Style.GetLength("min-width"); ok && clamped < minW {
					clamped = minW
				}
			} else {
				if minH, ok := item.Box.Style.GetLength("min-height"); ok && clamped < minH {
					clamped = minH
				}
			}
			// Clamp by min-width: auto (content-based minimum)
			if clamped < item.AutoMinMain {
				clamped = item.AutoMinMain
			}
			if clamped < 0 {
				clamped = 0
			}
			// Clamp by explicit max-width/max-height
			if isRow {
				if maxW, ok := item.Box.Style.GetLength("max-width"); ok && clamped > maxW {
					clamped = maxW
				}
			} else {
				if maxH, ok := item.Box.Style.GetLength("max-height"); ok && clamped > maxH {
					clamped = maxH
				}
			}
			totalViolation += clamped - states[i].targetMain
			states[i].targetMain = clamped
		}

		// Freeze violating items (CSS Flexbox §9.7 step 4).
		if totalViolation == 0 {
			// No violations: freeze all remaining unfrozen items.
			for i := range states {
				states[i].frozen = true
			}
		} else if totalViolation > 0 {
			// Positive violation: items hit their minimum → freeze those items.
			for i, item := range line.Items {
				if !states[i].frozen {
					hitMin := states[i].targetMain <= item.AutoMinMain
					if !hitMin && isRow {
						if minW, ok := item.Box.Style.GetLength("min-width"); ok && states[i].targetMain <= minW {
							hitMin = true
						}
					} else if !hitMin {
						if minH, ok := item.Box.Style.GetLength("min-height"); ok && states[i].targetMain <= minH {
							hitMin = true
						}
					}
					if hitMin {
						states[i].frozen = true
					}
				}
			}
		} else {
			// Negative violation: items hit their maximum → freeze only those items.
			for i, item := range line.Items {
				if !states[i].frozen {
					hitMax := false
					if isRow {
						if maxW, ok := item.Box.Style.GetLength("max-width"); ok && states[i].targetMain <= maxW {
							hitMax = true
						}
					} else {
						if maxH, ok := item.Box.Style.GetLength("max-height"); ok && states[i].targetMain <= maxH {
							hitMax = true
						}
					}
					if hitMax {
						states[i].frozen = true
					}
				}
			}
		}
	}

	// Apply resolved main sizes to items
	for i, item := range line.Items {
		item.MainSize = states[i].targetMain
		// Update the box's main dimension
		if isRow {
			item.Box.Width = item.MainSize + item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
		} else {
			item.Box.Height = item.MainSize + item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
		}
	}
}

// positionItemsCrossAxis positions items within a line along the cross axis.
func positionItemsCrossAxis(line *FlexLine, crossStart float64, alignItems css.AlignItems, isRow bool) {
	for _, item := range line.Items {
		// CSS Flexbox §8.1: Cross-axis auto margins override align-self
		margin := item.Box.Style.GetMargin()
		hasAutoCrossStart := false
		hasAutoCrossEnd := false
		if isRow {
			hasAutoCrossStart = margin.AutoTop
			hasAutoCrossEnd = margin.AutoBottom
		} else {
			hasAutoCrossStart = margin.AutoLeft
			hasAutoCrossEnd = margin.AutoRight
		}

		if hasAutoCrossStart || hasAutoCrossEnd {
			outerCross := item.outerCrossSize(isRow)
			freeSpace := line.CrossSize - outerCross
			if freeSpace < 0 {
				freeSpace = 0
			}

			crossMarginStart := 0.0
			if isRow {
				crossMarginStart = item.Box.Margin.Top
			} else {
				crossMarginStart = item.Box.Margin.Left
			}

			if hasAutoCrossStart && hasAutoCrossEnd {
				// Both auto: center the item
				autoMargin := freeSpace / 2
				if isRow {
					item.Box.Margin.Top = autoMargin
					item.Box.Margin.Bottom = autoMargin
				} else {
					item.Box.Margin.Left = autoMargin
					item.Box.Margin.Right = autoMargin
				}
				item.CrossPos = crossStart + autoMargin
			} else if hasAutoCrossStart {
				// Only start auto: push to end
				if isRow {
					item.Box.Margin.Top = freeSpace
				} else {
					item.Box.Margin.Left = freeSpace
				}
				item.CrossPos = crossStart + freeSpace
			} else {
				// Only end auto: stay at start
				if isRow {
					item.Box.Margin.Bottom = freeSpace
				} else {
					item.Box.Margin.Right = freeSpace
				}
				item.CrossPos = crossStart + crossMarginStart
			}
			continue
		}

		alignment := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())
		outerCross := item.outerCrossSize(isRow)
		crossMarginStart := 0.0
		if isRow {
			crossMarginStart = item.Box.Margin.Top
		} else {
			crossMarginStart = item.Box.Margin.Left
		}

		switch alignment {
		case css.AlignItemsFlexStart:
			item.CrossPos = crossStart + crossMarginStart
		case css.AlignItemsFlexEnd:
			item.CrossPos = crossStart + line.CrossSize - outerCross + crossMarginStart
		case css.AlignItemsCenter:
			item.CrossPos = crossStart + (line.CrossSize-outerCross)/2 + crossMarginStart

		case css.AlignItemsStretch:
			item.CrossPos = crossStart + crossMarginStart
		case css.AlignItemsBaseline:
			item.CrossPos = crossStart + crossMarginStart
		}
	}
}

// resolveAlignment resolves align-self: auto to the container's align-items.
func resolveAlignment(alignItems css.AlignItems, alignSelf css.AlignSelf) css.AlignItems {
	switch alignSelf {
	case css.AlignSelfFlexStart:
		return css.AlignItemsFlexStart
	case css.AlignSelfFlexEnd:
		return css.AlignItemsFlexEnd
	case css.AlignSelfCenter:
		return css.AlignItemsCenter
	case css.AlignSelfStretch:
		return css.AlignItemsStretch
	case css.AlignSelfBaseline:
		return css.AlignItemsBaseline
	default: // auto
		return alignItems
	}
}

// repositionFlexItemChildren adjusts children positions after a flex item is moved.
// deltaX and deltaY are the difference between the new and original box position.
func (le *LayoutEngine) repositionFlexItemChildren(box *Box, deltaX, deltaY float64) {
	if deltaX == 0 && deltaY == 0 {
		return
	}
	for _, child := range box.Children {
		child.X += deltaX
		child.Y += deltaY
		// Recursively shift grandchildren
		le.repositionFlexItemChildren(child, deltaX, deltaY)
	}
	// Also shift line boxes if any
	for _, lb := range box.LineBoxes {
		lb.Y += deltaY
		for _, lbBox := range lb.Boxes {
			lbBox.X += deltaX
			lbBox.Y += deltaY
		}
	}
}

// computeFlexItemAutoMinMain computes the content-based minimum main size for a flex item.
// Per CSS Flexbox §4.5, this is the smaller of the content size suggestion and specified size suggestion.
func (le *LayoutEngine) computeFlexItemAutoMinMain(node *html.Node, style *css.Style, box *Box, isRow bool) float64 {
	if isRow {
		// Row direction: min-width: auto → content-based minimum WIDTH
		contentMinSize := 0.0

		// CSS Flexbox §9.9.1: For a flex container in row direction with nowrap,
		// the min-content main size is the SUM of flex items' min-content contributions.
		// For block containers, it's the MAX of children's min-content widths.
		itemDisplay := style.GetDisplay()

		// Special case: grid containers with orthogonal (writing-mode: vertical-rl/lr)
		// direct children. Standard min-content underestimates because it measures each
		// child's horizontal width independently, ignoring that vertical-rl flow stacks
		// children into columns. The correct minimum = sum of all columns = initial layout
		// width (computed when the grid was laid out at the flex container's full width).
		if itemDisplay == css.DisplayGrid || itemDisplay == css.DisplayInlineGrid {
			for _, child := range node.Children {
				if child.Type != html.ElementNode {
					continue
				}
				childStyle := css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
				if childStyle == nil {
					continue
				}
				if wm, ok := childStyle.Get("writing-mode"); ok {
					if wm == "vertical-rl" || wm == "vertical-lr" {
						contentW := box.Width - box.Padding.Left - box.Padding.Right - box.Border.Left - box.Border.Right
						if contentW > 0 {
							return contentW
						}
						break
					}
				}
			}
		}

		isFlexRow := (itemDisplay == css.DisplayFlex || itemDisplay == css.DisplayInlineFlex) &&
			(style.GetFlexDirection() == css.FlexDirectionRow || style.GetFlexDirection() == css.FlexDirectionRowReverse)

		for _, child := range node.Children {
			childStyle := css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
			if childStyle == nil {
				childStyle = style
			}
			constraint := &ConstraintSpace{AvailableSize: Size{Width: le.viewport.width}}
			childMinMax := le.ComputeMinMaxSizes(child, constraint, childStyle)
			if isFlexRow {
				// Sum for row-direction flex containers
				contentMinSize += childMinMax.MinContentSize
			} else {
				// Max for block containers
				if childMinMax.MinContentSize > contentMinSize {
					contentMinSize = childMinMax.MinContentSize
				}
			}
		}

		// Specified size suggestion: the item's computed width, if definite
		if w, ok := style.GetLength("width"); ok {
			if contentMinSize > w {
				return w
			}
		}
		return contentMinSize
	}

	// Column direction: min-height: auto → content-based minimum HEIGHT
	// Per CSS Flexbox §4.5, the content-size suggestion is the item's min-content height.
	// For replaced elements, use the intrinsic laid-out height.
	// For block containers, use the sum of children outer heights (this approximates the
	// item's natural height without any explicit CSS height constraint).
	isReplacedElement := node != nil && (node.TagName == "img" || node.TagName == "canvas" ||
		node.TagName == "video" || node.TagName == "iframe")
	var contentMinHeight float64
	if isReplacedElement {
		// Replaced elements: use intrinsic height (border-box minus padding+border)
		contentMinHeight = box.Height - box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
	} else {
		// Block containers: sum children outer heights as the content-size suggestion.
		// An empty container has content size = 0 (no children to sum).
		for _, child := range box.Children {
			if child == nil {
				continue
			}
			contentMinHeight += child.Height + child.Margin.Top + child.Margin.Bottom
		}
	}
	if contentMinHeight < 0 {
		contentMinHeight = 0
	}

	// Specified size suggestion: the item's computed height, if definite
	if h, ok := style.GetLength("height"); ok {
		if contentMinHeight > h {
			return h
		}
	}
	return contentMinHeight
}

// Helper methods on FlexItem

// HypotheticalOuterMain returns the outer hypothetical main size (main size + margins + padding + border).
func (item *FlexItem) HypotheticalOuterMain(isRow bool) float64 {
	return item.HypotheticalMainSize + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
}

func (item *FlexItem) mainMargins(isRow bool) float64 {
	if isRow {
		return item.Box.Margin.Left + item.Box.Margin.Right
	}
	return item.Box.Margin.Top + item.Box.Margin.Bottom
}

func (item *FlexItem) mainPaddingBorder(isRow bool) float64 {
	if isRow {
		return item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
	}
	return item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
}

func (item *FlexItem) outerMainSize(isRow bool) float64 {
	if isRow {
		return item.Box.Width + item.Box.Margin.Left + item.Box.Margin.Right
	}
	return item.Box.Height + item.Box.Margin.Top + item.Box.Margin.Bottom
}

func (item *FlexItem) outerCrossSize(isRow bool) float64 {
	if isRow {
		return item.Box.Height + item.Box.Margin.Top + item.Box.Margin.Bottom
	}
	return item.Box.Width + item.Box.Margin.Left + item.Box.Margin.Right
}

// transformToVerticalRL transforms a horizontally laid-out box to vertical-rl layout.
// Each horizontal line becomes a vertical column, stacking right-to-left.
// Content within each column flows top-to-bottom (original X offset → Y offset).
// Lines are detected by grouping children with similar Y positions.
func transformToVerticalRL(box *Box) {
	if len(box.Children) == 0 {
		return
	}

	contentStartX := box.X + box.Border.Left + box.Padding.Left
	contentStartY := box.Y + box.Border.Top + box.Padding.Top

	// Group children into lines by their Y position (within 1px tolerance)
	type lineGroup struct {
		y        float64
		children []*Box
	}
	var lines []lineGroup
	for _, child := range box.Children {
		found := false
		for i := range lines {
			if math.Abs(child.Y-lines[i].y) < 1.0 {
				lines[i].children = append(lines[i].children, child)
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, lineGroup{y: child.Y, children: []*Box{child}})
		}
	}

	// Sort lines by Y (top to bottom)
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].y < lines[j].y
	})

	// Each line becomes a column. Compute column dimensions.
	type colInfo struct {
		width  float64 // max child width → column width
		height float64 // max child height → column height
	}
	cols := make([]colInfo, len(lines))
	for i, line := range lines {
		for _, child := range line.children {
			if child.Width > cols[i].width {
				cols[i].width = child.Width
			}
			if child.Height > cols[i].height {
				cols[i].height = child.Height
			}
		}
	}

	// Total new dimensions
	totalWidth := 0.0
	for _, c := range cols {
		totalWidth += c.width
	}
	totalHeight := 0.0
	for _, c := range cols {
		if c.height > totalHeight {
			totalHeight = c.height
		}
	}

	// Reposition children: columns stack right-to-left (vertical-rl)
	colX := totalWidth
	for i, line := range lines {
		colX -= cols[i].width
		for _, child := range line.children {
			// Original X offset within line → Y offset within column
			origRelX := child.X - contentStartX
			child.X = contentStartX + colX
			child.Y = contentStartY + origRelX
		}
	}

	// Update box dimensions (border-box)
	box.Width = totalWidth + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right
	box.Height = totalHeight + box.Padding.Top + box.Padding.Bottom + box.Border.Top + box.Border.Bottom
}
