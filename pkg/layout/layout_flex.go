package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/text"
	"math"
	"sort"
	"strconv"
	"strings"
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
	// vertical-lr, sideways-rl, sideways-lr), the inline direction is vertical,
	// so "row" maps to the block axis (column-like) and "column" maps to the
	// inline axis (row-like).
	isVerticalWM := false
	isVerticalRL := false // vertical-rl/sideways-rl: block axis goes right-to-left
	isSidewaysLR := false // sideways-lr: inline axis goes bottom-to-top (reversed)
	wm, _ := flexBox.Style.Get("writing-mode")
	if wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr" {
		isVerticalWM = true
		isVerticalRL = (wm == "vertical-rl" || wm == "sideways-rl")
		isSidewaysLR = (wm == "sideways-lr")
		isRow = !isRow
	}

	// Track the original flex-direction before any axis swaps for RTL cross handling.
	originalIsRow := (direction == css.FlexDirectionRow || direction == css.FlexDirectionRowReverse)

	// CSS Writing Modes §7.1: In vertical-rl, the block axis (which "column" direction
	// follows) runs right-to-left. After the isRow flip, original "column" becomes
	// isRow=true. The default (isReverse=false) flows left-to-right, but it should
	// flow right-to-left. So we flip isReverse for column+vertical-rl.
	// vertical-lr block axis goes left-to-right (same as default), so no flip needed.
	if isVerticalRL && isRow {
		isReverse = !isReverse
	}

	// CSS Writing Modes §6.4: sideways-lr reverses the inline direction compared
	// to vertical-lr. In vertical-lr, inline axis = top-to-bottom; in sideways-lr,
	// inline axis = bottom-to-top. For flex-direction:row (maps to physical column
	// after isRow flip), the main axis direction must be reversed.
	if isSidewaysLR && !isRow {
		// !isRow means original flex-direction was "row" (after the isRow flip).
		// The physical main axis is vertical. sideways-lr runs bottom-to-top.
		isReverse = !isReverse
	}

	// CSS Flexbox §4.5 + CSS Writing Modes: direction:rtl affects the inline axis.
	// - flex-direction:row follows the inline axis → RTL flips main direction
	// - flex-direction:column follows the block axis → RTL flips cross direction
	// The originalIsRow flag tracks whether the original flex-direction was row/row-reverse
	// (before the vertical WM axis swap), so RTL correctly targets the inline axis.
	isRTL := flexBox.Style.GetDirection() == css.DirectionRTL
	// RTL reverses the cross axis when the original direction is column
	// (the cross axis = inline axis, which is affected by direction:rtl).
	rtlReverseCross := false
	if isRTL && originalIsRow {
		// Original flex-direction:row → inline = main axis → flip main direction.
		isReverse = !isReverse
	} else if isRTL && !originalIsRow {
		// Original flex-direction:column → inline = cross axis → flip cross later.
		rtlReverseCross = true
	}

	// CSS Box Alignment §6.1: left/right in justify-content.
	// When the main axis is NOT parallel to the inline axis (column direction in any WM),
	// both left and right fall back to "start" (= flex-start for non-reverse, flex-end for reverse).
	// When the main axis IS the inline axis (row direction), left/right have physical meaning.
	if justifyContent == css.JustifyContentLeft || justifyContent == css.JustifyContentRight {
		if !originalIsRow {
			// Column direction: main axis ≠ inline axis → both left and right → "start"
			if isReverse {
				justifyContent = css.JustifyContentFlexEnd
			} else {
				justifyContent = css.JustifyContentFlexStart
			}
		} else if isVerticalWM {
			// Row direction in vertical WM: main axis = vertical inline axis.
			if justifyContent == css.JustifyContentLeft {
				if isReverse {
					justifyContent = css.JustifyContentFlexEnd
				} else {
					justifyContent = css.JustifyContentFlexStart
				}
			} else {
				if isReverse {
					justifyContent = css.JustifyContentFlexStart
				} else {
					justifyContent = css.JustifyContentFlexEnd
				}
			}
		} else {
			// Row direction in horizontal-tb: left/right are physical.
			if justifyContent == css.JustifyContentLeft {
				if isReverse {
					justifyContent = css.JustifyContentFlexEnd
				} else {
					justifyContent = css.JustifyContentFlexStart
				}
			} else {
				if isReverse {
					justifyContent = css.JustifyContentFlexStart
				} else {
					justifyContent = css.JustifyContentFlexEnd
				}
			}
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
	mainSizeIsDefinite := false // true when main size comes from explicit height/width (not just min-height)
	if isRow {
		mainSize = contentBoxWidth
		mainSizeIsDefinite = true // row direction: width is always definite from containing block
		if contentBoxHeight > 0 {
			crossSize = contentBoxHeight
			hasDefiniteCross = true
		}
	} else {
		// CSS Flexbox §9.2: column direction main size. Use contentBoxHeight when positive,
		// or when the CSS property explicitly says height: 0 (or resolves to 0 from percentage).
		// When contentBoxHeight==0 but CSS says e.g. height:500px, the box was flex-resolved
		// to 0 by an outer flex container — treat as indefinite so items size naturally.
		cssH, hasExplicitH := flexBox.Style.GetLength("height")
		cssPctH, hasExplicitPctH := flexBox.Style.GetPercentage("height")
		explicitZero := (hasExplicitH && cssH == 0) || (hasExplicitPctH && cssPctH == 0)
		if contentBoxHeight > 0 || explicitZero {
			mainSize = contentBoxHeight
			if mainSize < 0 {
				mainSize = 0
			}
		} else {
			mainSize = math.MaxFloat64 // indefinite
			// CSS Flexbox §9.2: For wrapping, use max-height as the available main size
			// when no explicit height is set.
			if mh, ok := flexBox.Style.GetLength("max-height"); ok {
				pb := flexBox.Padding.Top + flexBox.Padding.Bottom + flexBox.Border.Top + flexBox.Border.Bottom
				mainSize = mh - pb
				if mainSize < 0 {
					mainSize = 0
				}
			} else if mhPct, ok := flexBox.Style.GetPercentage("max-height"); ok {
				if flexBox.Parent != nil {
					cbH := flexBox.Parent.Height - flexBox.Parent.Padding.Top - flexBox.Parent.Padding.Bottom - flexBox.Parent.Border.Top - flexBox.Parent.Border.Bottom
					if cbH > 0 {
						pb := flexBox.Padding.Top + flexBox.Padding.Bottom + flexBox.Border.Top + flexBox.Border.Bottom
						mainSize = cbH*mhPct/100 - pb
						if mainSize < 0 {
							mainSize = 0
						}
					}
				}
			}
		}
		// CSS Flexbox §9.2: the main size is definite for percentage resolution only
		// when there's an explicit height (or percentage height). min-height alone
		// provides a floor but does NOT make the main size definite.
		if _, hasH := flexBox.Style.GetLength("height"); hasH {
			mainSizeIsDefinite = true
		} else if _, hasPctH := flexBox.Style.GetPercentage("height"); hasPctH {
			mainSizeIsDefinite = true
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

	// Detect shrink-to-fit context early for Step 1b and Step 3b.
	isShrinkToFit := false
	containerDisplay := flexBox.Style.GetDisplay()
	if containerDisplay == css.DisplayInlineFlex {
		isShrinkToFit = true
	} else if flexBox.Style.GetFloat() != css.FloatNone {
		isShrinkToFit = true
	} else if flexBox.Style.GetPosition() == css.PositionAbsolute || flexBox.Style.GetPosition() == css.PositionFixed {
		isShrinkToFit = true
	}

	// Step 1: Create flex items by laying out children to get intrinsic sizes
	contentStartX := flexBox.X + flexBox.Border.Left + flexBox.Padding.Left
	contentStartY := flexBox.Y + flexBox.Border.Top + flexBox.Padding.Top
	items := le.createFlexItemsProper(flexBox, contentStartX, contentStartY, contentBoxWidth, computedStyles, isRow)

	// Step 1b: For column-direction shrink-to-fit flex containers (float, abs pos
	// without explicit width), compute the cross-axis (width) from items' intrinsic
	// widths. When contentBoxWidth=0, items were laid out with 0 available width,
	// so their box.Width only reflects padding+border. Compute the actual content
	// width and re-create items so the full Step 3 runs with correct cross sizes.
	// Only applies when contentBoxWidth<=0 (float/abs-pos case); inline-flex
	// containers already have a valid contentBoxWidth from their parent.
	if !isRow && isShrinkToFit && contentBoxWidth <= 0 {
		if _, hasExplicitWidth := flexBox.Style.GetLength("width"); !hasExplicitWidth {
			if _, hasExplicitPctWidth := flexBox.Style.GetPercentage("width"); !hasExplicitPctWidth {
				maxCrossWidth := 0.0
				for _, item := range items {
					itemCross := le.computeShrinkToFitChildWidth(item.Box)
					if itemCross > maxCrossWidth {
						maxCrossWidth = itemCross
					}
				}
				if maxW, ok := flexBox.Style.GetLength("max-width"); ok && maxCrossWidth > maxW {
					maxCrossWidth = maxW
				}
				if minW, ok := flexBox.Style.GetLength("min-width"); ok && maxCrossWidth < minW {
					maxCrossWidth = minW
				}
				if maxCrossWidth > 0 {
					contentBoxWidth = maxCrossWidth
					flexBox.Width = maxCrossWidth + flexBox.Padding.Left + flexBox.Padding.Right + flexBox.Border.Left + flexBox.Border.Right
					crossSize = contentBoxWidth
					// Re-create items with correct available width; Step 3 below
					// will compute their flex base sizes correctly.
					items = le.createFlexItemsProper(flexBox, contentStartX, contentStartY, contentBoxWidth, computedStyles, isRow)
				}
			}
		}
	}

	// Step 2: Sort by order property
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	// Step 3: Determine flex base size and hypothetical main size for each item
	for _, item := range items {
		// CSS Flexbox §9.7: visibility:collapse items have 0 main-axis size.
		// They still contribute their cross-size (set in Step 6) for line sizing.
		if item.Collapsed {
			item.FlexBasis = 0
			item.HypotheticalMainSize = 0
			item.AutoMinMain = 0
			continue
		}
		basisVal := item.Box.Style.GetFlexBasisValue()
		if basisVal.IsContent {
			// flex-basis: content → always use max-content intrinsic size.
			// Unlike flex-basis:auto, this ignores any explicit CSS width/height on the item.
			// CSS Flexbox §9.2: the flex base size is the item's max-content size.
			if isRow {
				if item.Box.Node != nil && item.Box.Node.Type == html.TextNode {
					item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
				} else if item.Box.Node != nil && isReplacedElement(item.Box.Node.TagName) {
					// For replaced elements (canvas, img, etc.), use the element's intrinsic
					// width from HTML attributes (width="N"), ignoring any CSS width override.
					// The initial layout may have applied CSS width, so we read the attribute directly.
					intrinsicW := replacedElementIntrinsicWidth(item.Box.Node)
					if intrinsicW > 0 {
						item.FlexBasis = intrinsicW
					} else {
						// No HTML attribute width: fall back to laid-out width
						item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
						if item.FlexBasis < 0 {
							item.FlexBasis = 0
						}
					}
				} else {
					// Non-replaced block items: compute max-content intrinsic size.
					// flex-basis:content must ignore any explicit CSS width on the item.
					// Clone the style and remove width before calling ComputeIntrinsicSizes
					// so that computeBlockIntrinsicSizes measures the actual content, not
					// the overriding CSS width.
					styleForIntrinsic := item.Box.Style.Clone()
					delete(styleForIntrinsic.Properties, "width")
					intrinsicSizes := le.ComputeIntrinsicSizes(item.Box.Node, styleForIntrinsic, computedStyles)
					item.FlexBasis = intrinsicSizes.MaxContent - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
					if item.FlexBasis < 0 {
						item.FlexBasis = 0
					}
					item.BlockFillBasis = true
				}
			} else {
				// Column direction: flex-basis:content uses max-content height.
				// Must ignore any explicit CSS height on the item.
				if item.Box.Node != nil && isReplacedElement(item.Box.Node.TagName) {
					// For replaced elements, use the element's intrinsic height from HTML attributes.
					intrinsicH := replacedElementIntrinsicHeight(item.Box.Node)
					if intrinsicH > 0 {
						item.FlexBasis = intrinsicH
					} else {
						item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
						if item.FlexBasis < 0 {
							item.FlexBasis = 0
						}
					}
				} else if _, hasExplicitH := item.Box.Style.GetLength("height"); hasExplicitH {
					// There's an explicit CSS height that needs to be ignored for flex-basis:content.
					// Re-layout the item with height removed to get natural content height.
					styleForLayout := item.Box.Style.Clone()
					delete(styleForLayout.Properties, "height")
					savedStyle := computedStyles[item.Box.Node]
					computedStyles[item.Box.Node] = styleForLayout
					naturalBox := le.layoutNodeHTB(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, computedStyles, item.Box.Parent)
					computedStyles[item.Box.Node] = savedStyle
					item.FlexBasis = naturalBox.Height - naturalBox.Padding.Top - naturalBox.Padding.Bottom - naturalBox.Border.Top - naturalBox.Border.Bottom
					if item.FlexBasis < 0 {
						item.FlexBasis = 0
					}
				} else {
					// Non-replaced with no explicit CSS height: use laid-out content height.
					item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
					if item.FlexBasis < 0 {
						item.FlexBasis = 0
					}
				}
			}
		} else if basisVal.IsAuto {
			// flex-basis: auto → use the item's main size property
			if isRow {
				if w, ok := item.Box.Style.GetLength("width"); ok {
					// box-sizing: border-box means the CSS width includes padding+border.
					// FlexBasis represents content width (Step 8 re-adds padding+border),
					// so subtract them here to avoid double-counting.
					if item.Box.Style.GetBoxSizing() == "border-box" {
						w -= item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
						if w < 0 {
							w = 0
						}
					}
					item.FlexBasis = w
				} else if pct, ok := item.Box.Style.GetPercentage("width"); ok {
					// Resolve percentage width against flex container main size.
					// GetLength does not resolve percentages; handle them here.
					item.FlexBasis = pct / 100.0 * mainSize
				} else if wval, wok := item.Box.Style.Get("width"); wok && strings.HasPrefix(wval, "calc(") && strings.Contains(wval, "%") {
					// calc() with percentage: resolve % against flex container main size.
					// GetLength does not evaluate calc() expressions with %; handle here.
					expr := wval[5 : len(wval)-1]
					if result, calcOk := css.EvalCalcWithPercent(expr, item.Box.Style.GetFontSize(), mainSize); calcOk {
						w := result
						if item.Box.Style.GetBoxSizing() == "border-box" {
							w -= item.Box.Padding.Left + item.Box.Padding.Right + item.Box.Border.Left + item.Box.Border.Right
							if w < 0 {
								w = 0
							}
						}
						item.FlexBasis = w
					}
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
						//
						// CSS Flexbox §9.2 Part E: For items with aspect ratio, the flex base
						// size is NOT influenced by main-axis min/max constraints. Use intrinsic
						// width when the main-axis (width) has min/max constraints that would
						// change the dimension from intrinsic. Cross-axis constraints (min-height)
						// that transfer through AR are correctly reflected in box.Width.
						useIntrinsic := false
						if item.Box.Node.TagName == "img" {
							_, hasExplW := item.Box.Style.GetLength("width")
							_, hasExplWPct := item.Box.Style.GetPercentage("width")
							_, hasExplH := item.Box.Style.GetLength("height")
							_, hasExplHPct := item.Box.Style.GetPercentage("height")
							if !hasExplW && !hasExplWPct && !hasExplH && !hasExplHPct {
								// Check if main-axis (width) has non-trivial min/max constraints
								// that would inflate the flex-basis beyond intrinsic size.
								// min-width:0 is trivial (doesn't constrain anything).
								if minW, ok := item.Box.Style.GetLength("min-width"); ok && minW > 0 {
									useIntrinsic = true
								}
								if maxW, ok := item.Box.Style.GetLength("max-width"); ok && maxW > 0 {
									useIntrinsic = true
								}
								if _, ok := item.Box.Style.GetPercentage("max-width"); ok {
									useIntrinsic = true
								}
							}
						}
						if useIntrinsic {
							if src, ok := item.Box.Node.GetAttribute("src"); ok {
								if iw, _, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil && iw > 0 {
									item.FlexBasis = float64(iw)
								} else {
									item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
								}
							} else {
								item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
							}
						} else {
							item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
						}
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
						if itemWM == "vertical-rl" || itemWM == "vertical-lr" ||
							itemWM == "sideways-rl" || itemWM == "sideways-lr" {
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
					// box-sizing: border-box means the CSS height includes padding+border.
					if item.Box.Style.GetBoxSizing() == "border-box" {
						h -= item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
						if h < 0 {
							h = 0
						}
					}
					item.FlexBasis = h
				} else if hPct, hPctOK := item.Box.Style.GetPercentage("height"); hPctOK {
					// CSS Flexbox 9.2: percentage height resolves against the flex
					// container's definite main size. If the container main size is
					// indefinite (no explicit height, only min-height), the percentage
					// is treated as auto and we fall through to use auto sizing.
					if mainSizeIsDefinite {
						h := mainSize * hPct / 100
						if item.Box.Style.GetBoxSizing() == "border-box" {
							h -= item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
							if h < 0 {
								h = 0
							}
						}
						item.FlexBasis = h
					} else {
						// Indefinite main size: percentage resolves to auto.
						// For replaced elements, re-layout with auto height to get AR-derived height.
						if item.Box.Node != nil && isReplacedElement(item.Box.Node.TagName) {
							styleForLayout := item.Box.Style.Clone()
							delete(styleForLayout.Properties, "height")
							savedStyle := computedStyles[item.Box.Node]
							computedStyles[item.Box.Node] = styleForLayout
							naturalBox := le.layoutNodeHTB(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, computedStyles, item.Box.Parent)
							computedStyles[item.Box.Node] = savedStyle
							item.FlexBasis = naturalBox.Height - naturalBox.Padding.Top - naturalBox.Padding.Bottom - naturalBox.Border.Top - naturalBox.Border.Bottom
							// Update item box to use the natural (auto) layout
							item.Box.Height = naturalBox.Height
							item.Box.Width = naturalBox.Width
						} else {
							item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
						}
						if item.FlexBasis < 0 {
							item.FlexBasis = 0
						}
					}
				} else if hval, hok := item.Box.Style.Get("height"); hok && strings.HasPrefix(hval, "calc(") && strings.Contains(hval, "%") {
					// calc() with percentage: resolve % against flex container main size.
					expr := hval[5 : len(hval)-1]
					if result, calcOk := css.EvalCalcWithPercent(expr, item.Box.Style.GetFontSize(), mainSize); calcOk {
						h := result
						if item.Box.Style.GetBoxSizing() == "border-box" {
							h -= item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
							if h < 0 {
								h = 0
							}
						}
						item.FlexBasis = h
					}
				} else {
					// CSS Flexbox §9.2 Part E: For replaced elements with AR and
					// no explicit CSS width or height in column direction, AND the
					// main-axis (height) was clamped by min/max-height, use intrinsic
					// height instead. Cross-axis constraints (min-width) that transfer
					// through AR should still be reflected in the flex-basis.
					useIntrinsic := false
					if item.Box.Node != nil && item.Box.Node.TagName == "img" {
						_, hasExplW := item.Box.Style.GetLength("width")
						_, hasExplWPct := item.Box.Style.GetPercentage("width")
						_, hasExplH := item.Box.Style.GetLength("height")
						_, hasExplHPct := item.Box.Style.GetPercentage("height")
						if !hasExplW && !hasExplWPct && !hasExplH && !hasExplHPct {
							// Check if main-axis (height) has non-trivial min/max constraints.
							// min-height:0 is trivial (doesn't constrain anything).
							if minH, ok := item.Box.Style.GetLength("min-height"); ok && minH > 0 {
								useIntrinsic = true
							}
							if maxH, ok := item.Box.Style.GetLength("max-height"); ok && maxH > 0 {
								useIntrinsic = true
							}
						}
					}
					if useIntrinsic {
						if src, ok := item.Box.Node.GetAttribute("src"); ok {
							if _, ih, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil && ih > 0 {
								item.FlexBasis = float64(ih)
							} else {
								item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
							}
						} else {
							item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
						}
					} else if item.Box.Node != nil && isReplacedElement(item.Box.Node.TagName) && hasDefiniteCross && crossSize > 0 {
						// CSS Flexbox §9.2 Part E: For replaced elements with intrinsic
						// aspect ratio and a definite cross size (from stretch or explicit),
						// compute the flex base size by transferring the cross size through
						// the aspect ratio.
						partEDone := false
						if src, ok := item.Box.Node.GetAttribute("src"); ok {
							if iw, ih, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil && iw > 0 && ih > 0 {
								effectiveAlign := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())
								_, hasExplW := item.Box.Style.GetLength("width")
								_, hasExplWPct := item.Box.Style.GetPercentage("width")
								if effectiveAlign == css.AlignItemsStretch && !hasExplW && !hasExplWPct {
									// Transferred size: crossSize * (intrinsicHeight / intrinsicWidth)
									item.FlexBasis = crossSize * float64(ih) / float64(iw)
									partEDone = true
								} else if hasExplW || hasExplWPct {
									// Explicit cross size: use AR with the laid-out width
									usedW := item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
									item.FlexBasis = usedW * float64(ih) / float64(iw)
									partEDone = true
								}
							}
						}
						if !partEDone {
							item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
						}
					} else {
						item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
					}
				}
			}
		} else if basisVal.IsPercent {
			// CSS Flexbox §9.2: percentage flex-basis resolves against the flex container's
			// main size. If the container's main size is indefinite (column direction with
			// only min-height, no explicit height), percentage resolves to "content"
			// (the item's max-content size, ignoring the main size property).
			if mainSizeIsDefinite {
				item.FlexBasis = mainSize * basisVal.Percentage / 100
			} else {
				// Indefinite main size: percentage flex-basis -> content.
				// CSS Flexbox §9.2: use the item's max-content size, ignoring the
				// main-size property (width/height). Re-layout without the
				// main-size property to get the natural content size.
				if !isRow && item.Box.Node != nil {
					// Column direction: re-layout without CSS height
					styleForLayout := item.Box.Style.Clone()
					delete(styleForLayout.Properties, "height")
					savedStyle := computedStyles[item.Box.Node]
					computedStyles[item.Box.Node] = styleForLayout
					naturalBox := le.layoutNodeHTB(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, computedStyles, item.Box.Parent)
					computedStyles[item.Box.Node] = savedStyle
					item.FlexBasis = naturalBox.Height - naturalBox.Padding.Top - naturalBox.Padding.Bottom - naturalBox.Border.Top - naturalBox.Border.Bottom
				} else if isRow && item.Box.Node != nil {
					// Row direction: re-layout without CSS width
					styleForLayout := item.Box.Style.Clone()
					delete(styleForLayout.Properties, "width")
					savedStyle := computedStyles[item.Box.Node]
					computedStyles[item.Box.Node] = styleForLayout
					naturalBox := le.layoutNodeHTB(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, computedStyles, item.Box.Parent)
					computedStyles[item.Box.Node] = savedStyle
					item.FlexBasis = naturalBox.Width - naturalBox.Padding.Left - naturalBox.Padding.Right - naturalBox.Border.Left - naturalBox.Border.Right
				} else {
					if isRow {
						item.FlexBasis = item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
					} else {
						item.FlexBasis = item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
					}
				}
				if item.FlexBasis < 0 {
					item.FlexBasis = 0
				}
			}
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
		// CSS Flexbox §9.2 step 3: "clamped according to its used min and max main sizes"
		item.HypotheticalMainSize = item.FlexBasis

		// Clamp by max-width/max-height (explicit length values)
		if isRow {
			if maxW, ok := item.Box.Style.GetLength("max-width"); ok && item.HypotheticalMainSize > maxW {
				item.HypotheticalMainSize = maxW
			}
		} else {
			if maxH, ok := item.Box.Style.GetLength("max-height"); ok && item.HypotheticalMainSize > maxH {
				item.HypotheticalMainSize = maxH
			}
		}
		// Clamp by resolved max constraint (intrinsic size keyword).
		if item.HasMaxMain && item.HypotheticalMainSize > item.MaxMainSize {
			item.HypotheticalMainSize = item.MaxMainSize
		}

		// Clamp by min-width/min-height (explicit length values)
		if isRow {
			if minW, ok := item.Box.Style.GetLength("min-width"); ok && item.HypotheticalMainSize < minW {
				item.HypotheticalMainSize = minW
			}
		} else {
			if minH, ok := item.Box.Style.GetLength("min-height"); ok && item.HypotheticalMainSize < minH {
				item.HypotheticalMainSize = minH
			}
		}
		// Clamp by min-width: auto (content-based minimum)
		if item.HypotheticalMainSize < item.AutoMinMain {
			item.HypotheticalMainSize = item.AutoMinMain
		}
		// Clamp by resolved min constraint (intrinsic size keyword)
		if item.HasMinMain && item.HypotheticalMainSize < item.MinMainSize {
			item.HypotheticalMainSize = item.MinMainSize
		}
		if item.HypotheticalMainSize < 0 {
			item.HypotheticalMainSize = 0
		}
	}

	// Step 3.5: Fix AutoMinMain for column-direction replaced elements when the
	// container's effective cross size is established by max-width (not contentBoxWidth,
	// which may be 0 for floated containers with no explicit width).
	// CSS Flexbox §4.5: the transferred size suggestion uses the "definite cross size"
	// of the item — which for column direction is the container's cross size after
	// min/max-width clamping. For floated column containers, contentBoxWidth=0 so
	// createFlexItemsProper used availableWidth=0 → AutoMinMain used natural image width.
	// Fix: recompute AutoMinMain with the effective cross size from container max-width.
	if !isRow && crossSize == 0 {
		effectiveCross := 0.0
		if maxW, ok := flexBox.Style.GetLength("max-width"); ok {
			effectiveCross = maxW
		}
		if effectiveCross > 0 {
			for _, item := range items {
				if item.Box.Node == nil || item.Box.Width <= effectiveCross {
					continue
				}
				// Item's initial layout width exceeds the effective container cross size.
				// Recompute AutoMinMain using the effective cross.
				// CSS Overflow Level 3: use computed overflow-y for column direction main axis.
				columnMainOverflow := item.Box.Style.GetOverflowY()
				hasExplicitMin := false
				if _, ok := item.Box.Style.GetLength("min-height"); ok {
					hasExplicitMin = true
				}
				if !hasExplicitMin && (columnMainOverflow == css.OverflowVisible || columnMainOverflow == css.OverflowClip) {
					newAutoMin := le.computeFlexItemAutoMinMain(item.Box.Node, item.Box.Style, item.Box, isRow, effectiveCross, computedStyles)
					if newAutoMin != item.AutoMinMain {
						item.AutoMinMain = newAutoMin
						// Recompute HypotheticalMainSize with updated AutoMinMain.
						item.HypotheticalMainSize = item.FlexBasis
						if item.HypotheticalMainSize < item.AutoMinMain {
							item.HypotheticalMainSize = item.AutoMinMain
						}
						if item.HypotheticalMainSize < 0 {
							item.HypotheticalMainSize = 0
						}
					}
				}
			}
		}
	}

	// Step 3b: For shrink-to-fit row-direction flex containers, compute the ideal
	// main size from items. CSS Flexbox §9.2: The flex container's main size is its
	// max-content size if the main size property would be auto.
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
				// Apply min/max-width constraints (max first, then min — min wins per CSS2.1 §10.4)
				if maxW, ok := flexBox.Style.GetLength("max-width"); ok && maxContentWidth > maxW {
					maxContentWidth = maxW
				}
				if minW, ok := flexBox.Style.GetLength("min-width"); ok && maxContentWidth < minW {
					maxContentWidth = minW
				}
				// Update container and main size
				contentBoxWidth = maxContentWidth
				flexBox.Width = maxContentWidth + flexBox.Padding.Left + flexBox.Padding.Right + flexBox.Border.Left + flexBox.Border.Right
				mainSize = contentBoxWidth
			}
		}
	}

	// Step 4: Collect items into flex lines
	// For column wrapping containers, the line-breaking main size may differ from
	// the flex resolution main size. When the container has no explicit height and
	// only min-height, the min-height drives flex resolution but should NOT cause
	// wrapping (items stay on one line). When max-height is set, use it for wrapping.
	wrapMainSize := mainSize
	if !isRow && wrap != css.FlexWrapNowrap {
		_, hasExplicitH := flexBox.Style.GetLength("height")
		_, hasExplicitHPct := flexBox.Style.GetPercentage("height")
		if !hasExplicitH && !hasExplicitHPct {
			// No explicit height — check max-height for wrapping constraint
			padBorder := flexBox.Padding.Top + flexBox.Padding.Bottom + flexBox.Border.Top + flexBox.Border.Bottom
			if maxH, ok := flexBox.Style.GetLength("max-height"); ok {
				wrapMainSize = maxH - padBorder
			} else {
				// Only min-height set — don't wrap
				wrapMainSize = math.MaxFloat64
			}
		}
	}
	lines := collectFlexLines(items, wrapMainSize, mainGap, wrap, isRow)

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
				newBox := le.layoutNodeHTB(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, computedStyles, flexBox)
				le.floats = le.floats[:savedFloatsLen]
				le.absoluteBoxes = le.absoluteBoxes[:savedAbsLen]
				if newBox != nil {
					item.Box.Children = newBox.Children
					// For items where the re-layout width is LARGER than the initial layout width
					// (e.g., flex-basis:content with a tiny container), the height may change
					// significantly (e.g., floats that stacked at narrow width now fit on one line).
					// Update the cross-axis height so Step 6 gets the correct line cross size.
					// For items where the re-layout width is smaller (flex-basis:auto normal case),
					// the height is already correct from the initial layout at container width.
					initW := initialSizes[item].w
					if item.Box.Width > initW+0.5 {
						// Re-layout width is larger: height may have changed
						_, hasExplicitH := item.Box.Style.GetLength("height")
						if !hasExplicitH {
							item.Box.Height = newBox.Height
						}
					}
				}
			}
		}
	}

	// Step 5d: For row direction replaced elements (img) whose flex width changed from
	// the initial layout value, recompute height from the aspect ratio using the new width.
	// CSS Flexbox §9.8: a replaced element's cross size follows from the intrinsic aspect
	// ratio when the cross size is not explicitly specified.
	if isRow {
		for _, line := range lines {
			for _, item := range line.Items {
				if item.Box.Node == nil {
					continue
				}
				if item.Box.Node.TagName != "img" {
					continue
				}
				// Only for items with no explicit CSS height (cross size auto).
				if _, hasH := item.Box.Style.GetLength("height"); hasH {
					continue
				}
				// Only if width actually changed from initial layout.
				prev := initialSizes[item]
				if math.Abs(item.Box.Width-prev.w) < 0.5 {
					continue
				}
				// Recompute height from aspect ratio using the new width.
				if src, ok := item.Box.Node.GetAttribute("src"); ok {
					if iw, ih, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil && iw > 0 && ih > 0 {
						contentW := item.Box.Width - item.Box.Padding.Left - item.Box.Padding.Right - item.Box.Border.Left - item.Box.Border.Right
						if contentW > 0 {
							contentH := contentW * float64(ih) / float64(iw)
							newH := contentH + item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
							if minH, ok := item.Box.Style.GetLength("min-height"); ok {
								minBB := minH + item.Box.Padding.Top + item.Box.Padding.Bottom + item.Box.Border.Top + item.Box.Border.Bottom
								if newH < minBB {
									newH = minBB
								}
							}
							item.Box.Height = newH
						}
					}
				}
			}
		}
	}

	// Step 5e: For column-direction flex containers, re-layout items whose height
	// changed after flex resolution. When an item has an explicit CSS height (e.g. 200px)
	// but flex resolution changes it (e.g. to 100px via flex-grow from flex-basis:0),
	// the item's children were laid out at the old height and need re-layout.
	if !isRow {
		for _, line := range lines {
			for _, item := range line.Items {
				prev := initialSizes[item]
				if math.Abs(item.Box.Height-prev.h) < 0.5 {
					continue
				}
				if item.Box.Node == nil {
					continue
				}
				childDisplay := item.Box.Style.GetDisplay()
				if childDisplay == css.DisplayFlex || childDisplay == css.DisplayInlineFlex {
					// Re-layout this flex container with its new height.
					item.Box.Children = item.Box.Children[:0]
					le.layoutFlex(item.Box, item.Box.X, item.Box.Y, item.Box.Width, computedStyles)
				} else if childDisplay == css.DisplayGrid || childDisplay == css.DisplayInlineGrid {
					item.Box.Children = item.Box.Children[:0]
					contentH := item.Box.Height - item.Box.Padding.Top - item.Box.Padding.Bottom - item.Box.Border.Top - item.Box.Border.Bottom
					newBox := le.layoutGridContainer(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, contentH, item.Box.Style, computedStyles, flexBox)
					item.Box.Children = newBox.Children
				} else {
					// Block containers: re-layout with established height
					savedFloats := le.floats
					savedAbsPos := le.absoluteBoxes
					le.floats = le.floats[:le.floatBase]
					le.absoluteBoxes = nil
					newBox := le.layoutNodeHTB(item.Box.Node, item.Box.X, item.Box.Y, item.Box.Width, computedStyles, flexBox)
					le.floats = savedFloats
					le.absoluteBoxes = savedAbsPos
					item.Box.Children = newBox.Children
					// Preserve the flex-resolved height (don't take newBox.Height)
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
		// Line cross size = max of:
		// 1. Each non-baseline item's outer cross size
		// 2. The baseline group's collective cross size (maxAbove + maxBelow)
		maxCross := 0.0
		hasFirstBL := false
		hasLastBL := false
		maxAboveFirst := math.Inf(-1)
		maxBelowFirst := math.Inf(-1)
		maxAboveLast := math.Inf(-1)
		maxBelowLast := math.Inf(-1)
		for _, item := range line.Items {
			alignment := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())
			if alignment == css.AlignItemsBaseline || alignment == css.AlignItemsLastBaseline {
				// Compute baseline for line cross size calculation
				baseline := computeItemFirstBaseline(item, isRow)
				if alignment == css.AlignItemsLastBaseline {
					baseline = computeItemLastBaseline(item, isRow)
				}
				if baseline < 0 {
					// CSS Align §4.2: No baseline found, fall back to start alignment.
					if item.CrossSize > maxCross {
						maxCross = item.CrossSize
					}
				} else {
					crossMarginStart := 0.0
					crossMarginEnd := 0.0
					if isRow {
						crossMarginStart = item.Box.Margin.Top
						crossMarginEnd = item.Box.Margin.Bottom
					} else {
						crossMarginStart = item.Box.Margin.Left
						crossMarginEnd = item.Box.Margin.Right
					}
					var borderBoxCross float64
					if isRow {
						borderBoxCross = item.Box.Height
					} else {
						borderBoxCross = item.Box.Width
					}
					above := crossMarginStart + baseline
					below := (borderBoxCross - baseline) + crossMarginEnd
					if alignment == css.AlignItemsBaseline {
						hasFirstBL = true
						if above > maxAboveFirst {
							maxAboveFirst = above
						}
						if below > maxBelowFirst {
							maxBelowFirst = below
						}
					} else {
						hasLastBL = true
						if above > maxAboveLast {
							maxAboveLast = above
						}
						if below > maxBelowLast {
							maxBelowLast = below
						}
					}
				}
			} else {
				if item.CrossSize > maxCross {
					maxCross = item.CrossSize
				}
			}
		}
		if hasFirstBL {
			baselineGroupFirst := maxAboveFirst + maxBelowFirst
			if baselineGroupFirst > maxCross {
				maxCross = baselineGroupFirst
			}
		}
		if hasLastBL {
			baselineGroupLast := maxAboveLast + maxBelowLast
			if baselineGroupLast > maxCross {
				maxCross = baselineGroupLast
			}
		}
		line.CrossSize = maxCross
	}

	// Single-line container with definite cross size: use container's cross size
	if wrap == css.FlexWrapNowrap && hasDefiniteCross && len(lines) == 1 {
		lines[0].CrossSize = crossSize
	}

	// CSS Flexbox §9.4.4: Single-line containers clamp their line's cross-size to the
	// container's computed min and max cross-size, even when the cross size is indefinite.
	// This ensures max-height (row) or max-width (column) constrains the line height.
	if wrap == css.FlexWrapNowrap && len(lines) == 1 {
		if isRow {
			if maxH, ok := flexBox.Style.GetLength("max-height"); ok && lines[0].CrossSize > maxH {
				lines[0].CrossSize = maxH
			}
			if minH, ok := flexBox.Style.GetLength("min-height"); ok && lines[0].CrossSize < minH {
				lines[0].CrossSize = minH
			}
		} else {
			if maxW, ok := flexBox.Style.GetLength("max-width"); ok && lines[0].CrossSize > maxW {
				lines[0].CrossSize = maxW
			}
			if minW, ok := flexBox.Style.GetLength("min-width"); ok && lines[0].CrossSize < minW {
				lines[0].CrossSize = minW
			}
		}
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
			// CSS Flexbox §9.7: collapsed items are not stretched.
			if item.Collapsed {
				continue
			}
			alignment := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())
			if alignment == css.AlignItemsStretch {
				// Check if item has explicit cross-size (stretch only applies to auto)
				hasExplicitCrossSize := false
				if isRow {
					if _, ok := item.Box.Style.GetLength("height"); ok {
						hasExplicitCrossSize = true
					} else if hval, hok := item.Box.Style.Get("height"); hok && strings.HasPrefix(hval, "calc(") {
						// calc() height is an explicit cross size — do not stretch
						hasExplicitCrossSize = true
					} else if _, ok := item.Box.Style.GetPercentage("height"); ok {
						// percentage height is an explicit cross size — do not stretch
						hasExplicitCrossSize = true
					}
				} else {
					if _, ok := item.Box.Style.GetLength("width"); ok {
						hasExplicitCrossSize = true
					} else if wval, wok := item.Box.Style.Get("width"); wok && strings.HasPrefix(wval, "calc(") {
						// calc() width is an explicit cross size — do not stretch
						hasExplicitCrossSize = true
					} else if _, ok := item.Box.Style.GetPercentage("width"); ok {
						// percentage width is an explicit cross size — do not stretch
						hasExplicitCrossSize = true
					}
				}
				if hasExplicitCrossSize {
					continue
				}
				// CSS Flexbox §8.1: Auto margins override align-self/align-items alignment.
				// If the item has any auto cross-axis margin, skip stretch so the auto margin
				// can absorb the free space instead (this is handled in positionItemsCrossAxis).
				hasAutoCrossMargin := false
				if isRow {
					margin := item.Box.Style.GetMargin()
					hasAutoCrossMargin = margin.AutoTop || margin.AutoBottom
				} else {
					margin := item.Box.Style.GetMargin()
					hasAutoCrossMargin = margin.AutoLeft || margin.AutoRight
				}
				if hasAutoCrossMargin {
					continue
				}
				outerCross := item.outerCrossSize(isRow)
				// CSS Flexbox: for items with aspect-ratio and no explicit CSS cross size,
				// also stretch when the item is TALLER than the line (initial layout
				// applied aspect-ratio to auto-width before cross size was established).
				arItem := item.Box.Style.GetAspectRatio()
				// Also clamp items that exceed the line cross-size (e.g. when line is clamped
				// by container max-height/max-width per CSS Flexbox §9.4.4).
				shouldStretch := outerCross != line.CrossSize || (arItem.IsSet && outerCross != line.CrossSize)
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
						// CSS Intrinsic Sizing: apply max-width with keyword value (min-content/max-content)
						// on column-direction flex items. The resolved value was pre-computed in
						// createFlexItemsProper and stored in MaxCrossSize.
						if item.HasMaxCross && newWidth > item.MaxCrossSize {
							newWidth = item.MaxCrossSize
						}
						// Also enforce numeric max-width on the item (not just container-level clamping).
						if maxW, ok := item.Box.Style.GetLength("max-width"); ok && newWidth > maxW {
							newWidth = maxW
						}
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
			// Apply min/max-width to the recomputed total (max first, then min — min wins per CSS2.1 §10.4)
			if maxW, ok := flexBox.Style.GetLength("max-width"); ok && newTotalMain > maxW {
				newTotalMain = maxW
			}
			if minW, ok := flexBox.Style.GetLength("min-width"); ok && newTotalMain < minW {
				newTotalMain = minW
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
		// CSS Box Alignment §5.1: "safe" keyword — when overflow occurs, fall back to
		// physical start alignment to prevent content from overflowing the start edge.
		// Physical start = 0 offset (non-reverse) or originalFreeSpace (reverse).
		if flexBox.Style.IsSafeJustifyContent() && originalFreeSpace < 0 {
			if isReverse {
				initialOffset = originalFreeSpace // → item at physical start (left/top)
			} else {
				initialOffset = 0 // already physical start, keep as-is
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
		rawFreeSpace := crossSize - totalLinesCross
		freeSpace := rawFreeSpace
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
			// CSS Flexbox §9.4: negative free space → overflow symmetrically
			pos := rawFreeSpace / 2
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
		safeAI := flexBox.Style.IsSafeAlignItems()
		for lineIdx, line := range lines {
			crossPos := 0.0
			if lineIdx < len(lineOffsets) {
				crossPos = lineOffsets[lineIdx]
			}
			positionItemsCrossAxis(line, crossPos, alignItems, isRow, safeAI, isRTL)
		}
	} else {
		// Single-line or no definite cross size
		safeAI := flexBox.Style.IsSafeAlignItems()
		for i, line := range lines {
			positionItemsCrossAxis(line, currentCrossPos, alignItems, isRow, safeAI, isRTL)
			currentCrossPos += line.CrossSize
			if i < len(lines)-1 {
				currentCrossPos += crossGap
			}
		}
	}

	// CSS Writing Modes §7.1 + Flexbox §9.2: The cross axis direction depends on:
	// 1. flex-wrap: wrap-reverse flips the cross direction
	// 2. Writing mode: vertical-rl has right-to-left block axis (cross for original row)
	// 3. direction: rtl on original column flex → cross axis (inline) goes right-to-left
	crossNaturallyReversed := false
	if isVerticalRL && !isRow {
		// Physical column (original row in v-rl/sideways-rl): cross = horizontal block axis = right-to-left.
		crossNaturallyReversed = true
	}
	if isSidewaysLR && isRow {
		// Physical row (original column in sideways-lr): cross = vertical inline axis = bottom-to-top.
		crossNaturallyReversed = true
	}
	if rtlReverseCross {
		// Original column direction + RTL: cross axis = inline direction reversed.
		crossNaturallyReversed = !crossNaturallyReversed
	}
	shouldReverseCross := isWrapReverse != crossNaturallyReversed
	if shouldReverseCross && len(lines) > 1 {
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

	// Save absolutely-positioned children before resetting (they were added in createFlexItemsProper)
	var absposChildren []*Box
	for _, child := range flexBox.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			absposChildren = append(absposChildren, child)
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

			// CSS Flexbox §4.1 / CSS Positioned Layout §3:
			// Apply position:relative offsets (top/left/bottom/right) to flex items.
			// These offsets shift the item visually but don't affect flex layout.
			if item.Box.Position == css.PositionRelative && item.Box.Style != nil {
				offset := item.Box.Style.GetPositionOffset()
				relDx, relDy := 0.0, 0.0
				if offset.HasLeft {
					relDx = offset.Left
				} else if offset.HasRight {
					relDx = -offset.Right
				}
				if offset.HasTop {
					relDy = offset.Top
				} else if offset.HasBottom {
					relDy = -offset.Bottom
				}
				if relDx != 0 || relDy != 0 {
					item.Box.X += relDx
					item.Box.Y += relDy
					le.repositionFlexItemChildren(item.Box, relDx, relDy)
				}
			}

			flexBox.Children = append(flexBox.Children, item.Box)
		}
	}
	// Re-add absolutely-positioned children (they don't participate in flex layout
	// but must be in the tree for rendering)
	flexBox.Children = append(flexBox.Children, absposChildren...)

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

// replacedElementIntrinsicWidth returns the intrinsic width of a replaced element
// from its HTML attributes (width="N"), ignoring any CSS width override.
// Returns 0 if no intrinsic width can be determined.
func replacedElementIntrinsicWidth(node *html.Node) float64 {
	if widthAttr, ok := node.GetAttribute("width"); ok {
		if w, err := strconv.ParseFloat(widthAttr, 64); err == nil {
			return w
		}
	}
	return 0
}

// replacedElementIntrinsicHeight returns the intrinsic height of a replaced element
// from its HTML attributes (height="N"), ignoring any CSS height override.
// Returns 0 if no intrinsic height can be determined.
func replacedElementIntrinsicHeight(node *html.Node) float64 {
	if heightAttr, ok := node.GetAttribute("height"); ok {
		if h, err := strconv.ParseFloat(heightAttr, 64); err == nil {
			return h
		}
	}
	return 0
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

	// CSS Flexbox §4: ::before and ::after pseudo-elements on the flex container
	// are treated as flex items (first and last, respectively).
	// We prepend/append their synthetic nodes and store their styles so the
	// main loop below processes them uniformly (blockification, layout, etc.).
	beforeNode, beforeStyle := le.createPseudoElementNode(flexBox.Node, "before", computedStyles)
	if beforeNode != nil {
		computedStyles[beforeNode] = beforeStyle
		children = append([]*html.Node{beforeNode}, children...)
	}
	afterNode, afterStyle := le.createPseudoElementNode(flexBox.Node, "after", computedStyles)
	if afterNode != nil {
		computedStyles[afterNode] = afterStyle
		children = append(children, afterNode)
	}

	// Check if the flex container preserves whitespace
	containerWS := flexBox.Style.GetWhiteSpace()
	preservesWS := containerWS == css.WhiteSpacePre || containerWS == css.WhiteSpacePreWrap || containerWS == css.WhiteSpacePreLine

	for _, child := range children {
		if child.Type == html.TextNode {
			// CSS Flexbox §4: "an anonymous flex item that contains only white space
			// (i.e. characters that can be affected by the white-space property)
			// is not rendered (just as if it were display:none)."
			// This applies regardless of the white-space property value.
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

			// For non-whitespace-only text, use properly whitespace-collapsed text.
			// CSS Text §4.1.1: collapse runs of CSS whitespace (space, tab, newline)
			// to a single space, strip leading/trailing. Preserve non-breaking spaces
			// (U+00A0) which are NOT collapsible whitespace in CSS.
			var displayText string
			if preservesWS && child.RawText != "" {
				displayText = child.RawText
			} else {
				// CSS-aware whitespace collapsing: only collapse ASCII whitespace
				var buf strings.Builder
				inWS := true // Start true to trim leading whitespace
				for _, c := range textContent {
					if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' {
						if !inWS {
							buf.WriteByte(' ')
							inWS = true
						}
					} else {
						buf.WriteRune(c)
						inWS = false
					}
				}
				displayText = buf.String()
				// Trim trailing space from collapsed whitespace
				displayText = strings.TrimRight(displayText, " ")
			}

			// Create anonymous flex item for text
			containerStyle := flexBox.Style
			fontSize := containerStyle.GetFontSize()
			bold := containerStyle.GetFontWeight() == css.FontWeightBold
			italic := containerStyle.GetFontStyle() == css.FontStyleItalic
			mono := containerStyle.IsMonospaceFamily()
			ahem := containerStyle.IsAhemFamily()

			textWidth, textHeight := text.MeasureTextWithStyle(displayText, fontSize, bold, italic, mono, ahem)

			anonStyle := css.NewStyle()
			anonStyle.Set("display", "block")
			// Inherit font/color/white-space from container
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
			if v, ok := containerStyle.Get("white-space"); ok {
				anonStyle.Set("white-space", v)
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
				PseudoContent: displayText,
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
			// Layout the child and add it to the flex container's children list.
			// Note: flexBox.Children is reset in layoutFlex Step 12, so these boxes
			// are collected there and re-added after Step 12.
			absBox := le.layoutNodeHTB(child, flexBox.X, flexBox.Y, flexBox.Width, computedStyles, flexBox)
			if absBox != nil {
				flexBox.Children = append(flexBox.Children, absBox)
			}
			continue
		}

		// CSS Flexbox §4: Blockification of flex items
		// Children of a flex container have their display value blockified:
		// inline → block, inline-block → block, inline-flex → flex, inline-grid → grid
		// display:table → block (CSS Flexbox §4.4: table outer display becomes block)
		// float and clear have no effect on flex items.
		display := childStyle.GetDisplay()
		if display == css.DisplayInline || display == css.DisplayInlineBlock {
			childStyle.Set("display", "block")
		} else if display == css.DisplayInlineFlex {
			childStyle.Set("display", "flex")
		} else if display == css.DisplayInlineGrid {
			childStyle.Set("display", "grid")
		} else if display == css.DisplayTable {
			// CSS Flexbox §4.4: table items are blockified to display:block.
			// The table formatting context is replaced by block layout inside the flex item.
			childStyle.Set("display", "block")
		}
		childStyle.Set("float", "none")
		childStyle.Set("clear", "none")

		// CSS Intrinsic Sizing: For column flex items with max-width: min-content (or
		// max-content), pre-compute the constrained width BEFORE the initial layout
		// so that block layout (floats, wrapping inline content) uses the correct width.
		// This ensures FlexBasis (height) reflects the actual stacking at constrained width.
		initialLayoutWidth := availableWidth
		if !isRow {
			if val, ok := childStyle.Get("max-width"); ok {
				if kw, kwOK := resolveIntrinsicKeyword(val); kwOK {
					// Compute the intrinsic width constraint using ComputeMinMaxSizes.
					// Build a temporary dummy item to call resolveItemIntrinsicWidthConstraint.
					tempBox := &Box{Node: child, Style: childStyle}
					dummyItem := &FlexItem{Box: tempBox}
					if constrainedW, constrainedOK := le.resolveItemIntrinsicWidthConstraint(dummyItem, kw); constrainedOK && constrainedW > 0 && constrainedW < availableWidth-0.5 {
						initialLayoutWidth = constrainedW
					}
				}
			}
		}

		// Layout the child to get its intrinsic dimensions
		childBox := le.layoutNodeHTB(child, startX, startY, initialLayoutWidth, computedStyles, flexBox)

		// CSS Flexbox §9.2: For column flex items with non-stretch cross alignment,
		// the initial block layout fills the container width (wrong). The item's
		// cross size (width) should be fit-content (shrink-to-fit). Re-layout
		// with the correct width so that percentage-based paddings/margins
		// (e.g. padding-bottom: 50%) resolve against the actual containing-block width.
		// Skip if we already laid out at a constrained width (max-width: min-content).
		if !isRow && initialLayoutWidth == availableWidth {
			flexAlignItems := flexBox.Style.GetAlignItems()
			effectiveAlign := resolveAlignment(flexAlignItems, childStyle.GetAlignSelf())
			if effectiveAlign != css.AlignItemsStretch {
				// Check if child has no explicit CSS width (block-fill case)
				_, hasExplicitW := childStyle.GetLength("width")
				_, hasExplicitPctW := childStyle.GetPercentage("width")
				if !hasExplicitW && !hasExplicitPctW {
					// Compute fit-content width: min(max-content, available-cross)
					constraint := NewConstraintSpace(availableWidth, -1)
					intrinsicW := le.ComputeMinMaxSizes(child, constraint, childStyle).MaxContentSize
					if intrinsicW > availableWidth {
						intrinsicW = availableWidth
					}
					// Re-layout with fit-content width if it's smaller than block-fill width.
					// layoutNode treats availableWidth as the outer available space from which
					// margins are subtracted to get the border-box width. intrinsicW is already
					// border-box (content+padding+border), so we add margins back so layoutNode
					// computes: (intrinsicW+margins) - margins - padding - border = content.
					if intrinsicW > 0 && intrinsicW < childBox.Width-0.5 {
						layoutAvail := intrinsicW + childBox.Margin.Left + childBox.Margin.Right
						childBox = le.layoutNodeHTB(child, startX, startY, layoutAvail, computedStyles, flexBox)
					}
				}
			}
		}

		// CSS Writing Modes §6.4: In vertical writing mode, transform inline layout
		// from horizontal lines to vertical-rl columns. Each horizontal line becomes
		// a column stacking right-to-left, with content flowing top-to-bottom.
		// CSS Writing Modes §6.4: In vertical writing mode, transform inline layout
		// from horizontal lines to vertical-rl columns for auto-sized items.
		// Items with explicit width/height keep their specified dimensions.
		if wm, ok := flexBox.Style.Get("writing-mode"); ok {
			if wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr" {
				_, hasExplicitW := childStyle.GetLength("width")
				_, hasExplicitH := childStyle.GetLength("height")
				if !hasExplicitW && !hasExplicitH {
					transformToVerticalRL(childBox, wm)
				}
			}
		}

		item := &FlexItem{
			Box:        childBox,
			FlexGrow:   childStyle.GetFlexGrow(),
			FlexShrink: childStyle.GetFlexShrink(),
			Order:      childStyle.GetOrder(),
		}

		// CSS Flexbox §9.7: visibility:collapse items act as visibility:hidden for rendering
		// but have 0 main-axis size for flex layout. Mark them so Step 3 zeros their main size.
		if childStyle.GetVisibility() == "collapse" {
			item.Collapsed = true
		}

		// Compute min-width: auto (CSS Flexbox §4.5)
		// For flex items with overflow: visible, min-width/min-height: auto
		// computes to the content-based minimum size.
		// CSS Overflow Level 3: use the computed overflow on the main axis, which accounts
		// for interdependency (e.g., overflow-y:hidden forces overflow-x to auto).
		var mainOverflow css.OverflowType
		if isRow {
			mainOverflow = childStyle.GetOverflowX()
		} else {
			mainOverflow = childStyle.GetOverflowY()
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
		if !hasExplicitMin && (mainOverflow == css.OverflowVisible || mainOverflow == css.OverflowClip) {
			item.AutoMinMain = le.computeFlexItemAutoMinMain(child, childStyle, childBox, isRow, availableWidth, computedStyles)
		}

		// Pre-resolve intrinsic size keyword constraints (min-content, max-content).
		// GetLength() returns false for these keywords, so they must be resolved here
		// using the laid-out box and intrinsic size computation. Values are stored in
		// MaxMainSize, MinMainSize, MaxCrossSize (HasMax/HasMin flags indicate if set).

		// Main-axis max constraint.
		{
			var propName string
			if isRow {
				propName = "max-width"
			} else {
				propName = "max-height"
			}
			if val, ok := childStyle.Get(propName); ok {
				if kw, kwOK := resolveIntrinsicKeyword(val); kwOK {
					var resolved float64
					var resolvedOK bool
					if isRow {
						resolved, resolvedOK = le.resolveItemIntrinsicWidthConstraint(item, kw)
					} else {
						resolved, resolvedOK = resolveItemIntrinsicHeightConstraint(item, kw)
					}
					if resolvedOK {
						item.MaxMainSize = resolved
						item.HasMaxMain = true
					}
				}
			}
		}

		// Main-axis min constraint.
		{
			var propName string
			if isRow {
				propName = "min-width"
			} else {
				propName = "min-height"
			}
			if val, ok := childStyle.Get(propName); ok {
				if kw, kwOK := resolveIntrinsicKeyword(val); kwOK {
					// CSS Sizing 3 §5.1: In the block axis, min-content and max-content
					// are equivalent to "auto". For column flex (min-height: min-content),
					// don't resolve to a specific value — let AutoMinMain handle it.
					if isRow {
						resolved, resolvedOK := le.resolveItemIntrinsicWidthConstraint(item, kw)
						if resolvedOK {
							item.MinMainSize = resolved
							item.HasMinMain = true
						}
					}
					// else: column direction — min-height: min-content/max-content
					// behaves as auto (uses AutoMinMain from computeFlexItemAutoMinMain)
				}
			}
		}

		// Cross-axis max constraint (only relevant for column flex: max-width on item).
		if !isRow {
			if val, ok := childStyle.Get("max-width"); ok {
				if kw, kwOK := resolveIntrinsicKeyword(val); kwOK {
					if resolved, resolvedOK := le.resolveItemIntrinsicWidthConstraint(item, kw); resolvedOK {
						item.MaxCrossSize = resolved
						item.HasMaxCross = true
					}
				}
			}
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

	// Calculate initial free space (before any items are frozen by min/max clamping).
	// Per CSS Flexbox §9.7 step 3: used for fractional flex factor capping.
	initialUsedSpace := 0.0
	for i, item := range line.Items {
		if states[i].frozen {
			initialUsedSpace += states[i].targetMain + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
		} else {
			initialUsedSpace += item.FlexBasis + item.mainMargins(isRow) + item.mainPaddingBorder(isRow)
		}
	}
	initialFreeSpace := effectiveAvailable - initialUsedSpace

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
				// Per CSS Flexbox §9.7.1: If the sum of the unfrozen flex items' flex
				// factors is less than one, multiply the *initial* free space by this sum.
				// If the magnitude of this value is less than the magnitude of the
				// remaining free space, use this as the remaining free space.
				effectiveFree := freeSpace
				if totalGrowFactor < 1 {
					capped := initialFreeSpace * totalGrowFactor
					if capped >= 0 && capped < freeSpace {
						effectiveFree = capped
					}
				}
				for i, item := range line.Items {
					if !states[i].frozen {
						states[i].targetMain = item.FlexBasis + effectiveFree*(item.FlexGrow/totalGrowFactor)
					}
				}
			}
		} else {
			// Shrink: weighted by flex-shrink * flex-basis
			totalShrinkFactor := 0.0
			totalScaledShrink := 0.0
			for i, item := range line.Items {
				if !states[i].frozen {
					totalShrinkFactor += item.FlexShrink
					totalScaledShrink += item.FlexShrink * item.FlexBasis
				}
			}
			if totalScaledShrink > 0 {
				// Per CSS Flexbox §9.7.1: If the sum of the unfrozen flex items' flex
				// shrink factors is less than one, multiply the initial free space by
				// this sum. If the magnitude is less than the remaining free space,
				// use this as the remaining free space.
				effectiveFree := freeSpace
				if totalShrinkFactor < 1 {
					capped := initialFreeSpace * totalShrinkFactor
					// freeSpace is negative when shrinking; capped magnitude should be less
					if capped <= 0 && capped > freeSpace {
						effectiveFree = capped
					}
				}
				for i, item := range line.Items {
					if !states[i].frozen {
						scaledFactor := item.FlexShrink * item.FlexBasis / totalScaledShrink
						states[i].targetMain = item.FlexBasis + effectiveFree*scaledFactor
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
			// Clamp by explicit min-width/min-height (numeric values)
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
			// Clamp by intrinsic keyword min constraint (min-content/max-content keyword)
			if item.HasMinMain && clamped < item.MinMainSize {
				clamped = item.MinMainSize
			}
			if clamped < 0 {
				clamped = 0
			}
			// Clamp by explicit max-width/max-height (numeric values)
			if isRow {
				if maxW, ok := item.Box.Style.GetLength("max-width"); ok && clamped > maxW {
					clamped = maxW
				}
			} else {
				if maxH, ok := item.Box.Style.GetLength("max-height"); ok && clamped > maxH {
					clamped = maxH
				}
			}
			// Clamp by intrinsic keyword max constraint (min-content/max-content keyword)
			if item.HasMaxMain && clamped > item.MaxMainSize {
				clamped = item.MaxMainSize
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
					// Also check intrinsic keyword min constraint.
					if !hitMin && item.HasMinMain && states[i].targetMain <= item.MinMainSize {
						hitMin = true
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
					// Also check intrinsic keyword max constraint.
					if !hitMax && item.HasMaxMain && states[i].targetMain <= item.MaxMainSize {
						hitMax = true
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
// safeAI indicates whether the container's align-items uses the "safe" overflow keyword.
// containerIsRTL indicates that the flex container has direction: rtl (affects column flex cross-axis).
func positionItemsCrossAxis(line *FlexLine, crossStart float64, alignItems css.AlignItems, isRow bool, safeAI bool, containerIsRTL bool) {
	// CSS Flexbox §8.3: First pass — compute baselines for all baseline-aligned items
	// and find the maximum "above baseline" and "below baseline" distances.
	maxAboveFirst := math.Inf(-1)
	maxBelowFirst := math.Inf(-1)
	maxAboveLast := math.Inf(-1)
	maxBelowLast := math.Inf(-1)
	hasFirstBaseline := false
	hasLastBaseline := false

	for _, item := range line.Items {
		// Skip items with cross-axis auto margins
		margin := item.Box.Style.GetMargin()
		if isRow {
			if margin.AutoTop || margin.AutoBottom {
				continue
			}
		} else {
			if margin.AutoLeft || margin.AutoRight {
				continue
			}
		}

		alignment := resolveAlignment(alignItems, item.Box.Style.GetAlignSelf())

		if alignment == css.AlignItemsBaseline {
			baseline := computeItemFirstBaseline(item, isRow)
			if baseline < 0 {
				item.FirstBaseline = -1
			} else {
				item.FirstBaseline = baseline
				crossMarginStart := 0.0
				crossMarginEnd := 0.0
				if isRow {
					crossMarginStart = item.Box.Margin.Top
					crossMarginEnd = item.Box.Margin.Bottom
				} else {
					crossMarginStart = item.Box.Margin.Left
					crossMarginEnd = item.Box.Margin.Right
				}
				var borderBoxCross float64
				if isRow {
					borderBoxCross = item.Box.Height
				} else {
					borderBoxCross = item.Box.Width
				}
				aboveBaseline := crossMarginStart + baseline
				belowBaseline := (borderBoxCross - baseline) + crossMarginEnd
				if aboveBaseline > maxAboveFirst {
					maxAboveFirst = aboveBaseline
				}
				if belowBaseline > maxBelowFirst {
					maxBelowFirst = belowBaseline
				}
				hasFirstBaseline = true
			}
		} else if alignment == css.AlignItemsLastBaseline {
			baseline := computeItemLastBaseline(item, isRow)
			if baseline < 0 {
				item.LastBaseline = -1
			} else {
				item.LastBaseline = baseline
				crossMarginStart := 0.0
				crossMarginEnd := 0.0
				if isRow {
					crossMarginStart = item.Box.Margin.Top
					crossMarginEnd = item.Box.Margin.Bottom
				} else {
					crossMarginStart = item.Box.Margin.Left
					crossMarginEnd = item.Box.Margin.Right
				}
				var borderBoxCross float64
				if isRow {
					borderBoxCross = item.Box.Height
				} else {
					borderBoxCross = item.Box.Width
				}
				belowBaseline := crossMarginEnd + (borderBoxCross - baseline)
				aboveBaseline := crossMarginStart + baseline
				if belowBaseline > maxBelowLast {
					maxBelowLast = belowBaseline
				}
				if aboveBaseline > maxAboveLast {
					maxAboveLast = aboveBaseline
				}
				hasLastBaseline = true
			}
		}
	}

	// Second pass: position all items
	for _, item := range line.Items {
		// CSS Flexbox §8.1: Cross-axis auto margins override align-self
		margin := item.Box.Style.GetMargin()
		hasAutoCrossStart := false
		hasAutoCrossEnd := false
		if isRow {
			hasAutoCrossStart = margin.AutoTop
			hasAutoCrossEnd = margin.AutoBottom
		} else {
			// In RTL column flex, the cross-axis start is the right side (physical end).
			// Auto margins: cross-start auto = right margin auto, cross-end auto = left margin auto.
			if containerIsRTL {
				hasAutoCrossStart = margin.AutoRight
				hasAutoCrossEnd = margin.AutoLeft
			} else {
				hasAutoCrossStart = margin.AutoLeft
				hasAutoCrossEnd = margin.AutoRight
			}
		}

		if hasAutoCrossStart || hasAutoCrossEnd {
			outerCross := item.outerCrossSize(isRow)
			freeSpace := line.CrossSize - outerCross
			if freeSpace < 0 {
				freeSpace = 0
			}

			// crossMarginStart is the physical-start-side margin (the margin between the
			// line's physical-start edge and the item's border-box).
			// For row flex: always top margin.
			// For column LTR: left margin. For column RTL: right margin (physical start = right).
			crossMarginStart := 0.0
			if isRow {
				crossMarginStart = item.Box.Margin.Top
			} else if containerIsRTL {
				crossMarginStart = item.Box.Margin.Right
			} else {
				crossMarginStart = item.Box.Margin.Left
			}

			if hasAutoCrossStart && hasAutoCrossEnd {
				// Both auto: center the item
				autoMargin := freeSpace / 2
				if isRow {
					item.Box.Margin.Top = autoMargin
					item.Box.Margin.Bottom = autoMargin
				} else if containerIsRTL {
					item.Box.Margin.Right = autoMargin
					item.Box.Margin.Left = autoMargin
				} else {
					item.Box.Margin.Left = autoMargin
					item.Box.Margin.Right = autoMargin
				}
				// RTL column: physical start = right, so crossPos from left = line.CrossSize - outerCross + margin.left
				if !isRow && containerIsRTL {
					item.CrossPos = crossStart + line.CrossSize - outerCross + autoMargin
				} else {
					item.CrossPos = crossStart + autoMargin
				}
			} else if hasAutoCrossStart {
				// Only cross-start auto: push to cross-end
				if isRow {
					item.Box.Margin.Top = freeSpace
					item.CrossPos = crossStart + freeSpace
				} else if containerIsRTL {
					// RTL column: cross-start is right, pushing to cross-end = left
					item.Box.Margin.Right = freeSpace
					// crossPos from left = margin.left (at the left edge)
					item.CrossPos = crossStart + item.Box.Margin.Left
				} else {
					item.Box.Margin.Left = freeSpace
					item.CrossPos = crossStart + freeSpace
				}
			} else {
				// Only cross-end auto: stay at cross-start
				if isRow {
					item.Box.Margin.Bottom = freeSpace
					item.CrossPos = crossStart + crossMarginStart
				} else if containerIsRTL {
					// RTL column: cross-start is right, so item should be on the right
					item.Box.Margin.Left = freeSpace
					// crossPos from left = line.CrossSize - outerCross + margin.left
					item.CrossPos = crossStart + line.CrossSize - outerCross + item.Box.Margin.Left
				} else {
					item.Box.Margin.Right = freeSpace
					item.CrossPos = crossStart + crossMarginStart
				}
			}
			continue
		}

		alignSelf := item.Box.Style.GetAlignSelf()
		alignment := resolveAlignment(alignItems, alignSelf)
		outerCross := item.outerCrossSize(isRow)

		// crossMarginPhysStart is the physical-left (column) or physical-top (row) margin.
		// This is always the offset from the content area edge to the border box edge
		// (since Box.X/Y is always the physical-left/top border-box edge).
		crossMarginPhysStart := 0.0
		if isRow {
			crossMarginPhysStart = item.Box.Margin.Top
		} else {
			crossMarginPhysStart = item.Box.Margin.Left
		}

		// CSS Box Alignment §5.1: "safe" — when item overflows the line's cross size,
		// fall back to flex-start (physical cross-start) to avoid unreachable overflow.
		// flex-start in RTL column = physical right side.
		itemSafe := safeAI || item.Box.Style.IsSafeAlignSelf()
		if itemSafe && outerCross > line.CrossSize {
			if !isRow && containerIsRTL {
				// RTL column: safe fallback = flex-start = right side
				item.CrossPos = crossStart + line.CrossSize - outerCross + crossMarginPhysStart
			} else {
				item.CrossPos = crossStart + crossMarginPhysStart
			}
			continue
		}

		// effectiveStart: for column RTL, flex-start is physically at the right, so
		// its formula is equivalent to flex-end in LTR (place at right side).
		// effectiveEnd: for column RTL, flex-end is physically at the left.
		// For row flex or LTR column, effectiveStart = left/top, effectiveEnd = right/bottom.
		wantPhysStart := true // Does the desired alignment put item at physical start?

		switch alignment {
		case css.AlignItemsFlexStart:
			// In RTL column, flex-start = physical right = cross-start.
			// In LTR column or row, flex-start = physical left/top.
			wantPhysStart = !(!isRow && containerIsRTL) // false when RTL column
		case css.AlignItemsFlexEnd:
			// In RTL column, flex-end = physical left = cross-end.
			// In LTR column or row, flex-end = physical right/bottom.
			wantPhysStart = !isRow && containerIsRTL // true when RTL column
		case css.AlignItemsCenter:
			// Center is always centered, RTL doesn't change that.
			item.CrossPos = crossStart + (line.CrossSize-outerCross)/2 + crossMarginPhysStart
			continue
		case css.AlignItemsStretch:
			// Stretch fills the cross size; position at physical cross-start margin.
			// For RTL column, physical cross-start margin is still at the left edge
			// (the item fills the full width, so RTL doesn't change position).
			item.CrossPos = crossStart + crossMarginPhysStart
			continue
		case css.AlignItemsBaseline:
			if hasFirstBaseline && item.FirstBaseline >= 0 {
				item.CrossPos = crossStart + maxAboveFirst - item.FirstBaseline
			} else {
				// No baseline found, fall back to flex-start
				if !isRow && containerIsRTL {
					item.CrossPos = crossStart + line.CrossSize - outerCross + crossMarginPhysStart
				} else {
					item.CrossPos = crossStart + crossMarginPhysStart
				}
			}
			continue
		case css.AlignItemsLastBaseline:
			if hasLastBaseline && item.LastBaseline >= 0 {
				item.CrossPos = crossStart + line.CrossSize - maxBelowLast - item.LastBaseline
			} else {
				// No baseline found, fall back to flex-start
				if !isRow && containerIsRTL {
					item.CrossPos = crossStart + line.CrossSize - outerCross + crossMarginPhysStart
				} else {
					item.CrossPos = crossStart + crossMarginPhysStart
				}
			}
			continue
		}

		// Handle self-start and self-end: use item's own direction/writing-mode.
		if alignSelf == css.AlignSelfSelfStart || alignSelf == css.AlignSelfSelfEnd {
			selfStartIsPhysStart := isSelfStartAtPhysicalCrossStart(item, isRow)
			if alignSelf == css.AlignSelfSelfStart {
				wantPhysStart = selfStartIsPhysStart
			} else {
				wantPhysStart = !selfStartIsPhysStart
			}
		}

		if wantPhysStart {
			// Physical start: left (column) or top (row)
			item.CrossPos = crossStart + crossMarginPhysStart
		} else {
			// Physical end: right (column) or bottom (row)
			// crossPos from physical left/top such that the margin-box ends at line.CrossSize
			item.CrossPos = crossStart + line.CrossSize - outerCross + crossMarginPhysStart
		}
	}
}

// isSelfStartAtPhysicalCrossStart returns true if align-self: self-start should place
// the item at the physical cross-axis start (top for row flex, left for column flex).
// This is determined by the item's own writing-mode and direction.
func isSelfStartAtPhysicalCrossStart(item *FlexItem, isRow bool) bool {
	wm, _ := item.Box.Style.Get("writing-mode")
	itemDir := item.Box.Style.GetDirection()
	isItemRTL := itemDir == css.DirectionRTL

	if isRow {
		// Cross axis = vertical (physical start = top).
		// self-start = item's inline-start projected onto the vertical axis.
		switch wm {
		case "vertical-lr", "vertical-rl":
			// Inline direction is vertical. With RTL reversal, inline-start = bottom.
			// self-start = top only when LTR (inline flows top→bottom).
			return !isItemRTL
		default: // horizontal-tb or unspecified
			// Block-start = top for all horizontal writing modes.
			return true
		}
	} else {
		// Cross axis = horizontal (physical start = left).
		// self-start = item's inline-start projected onto the horizontal axis.
		switch wm {
		case "vertical-rl":
			// Block direction goes right-to-left: block-start = right = physical end.
			return false
		case "vertical-lr":
			// Block direction goes left-to-right: block-start = left = physical start.
			return true
		default: // horizontal-tb or unspecified
			// Inline direction is horizontal: inline-start = left (LTR) or right (RTL).
			return !isItemRTL
		}
	}
}

// resolveAlignment resolves align-self: auto to the container's align-items.
// For self-start and self-end, returns AlignItemsFlexStart as a placeholder;
// positionItemsCrossAxis handles these values separately via isSelfStartAtPhysicalCrossStart.
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
	case css.AlignSelfLastBaseline:
		return css.AlignItemsLastBaseline
	case css.AlignSelfSelfStart, css.AlignSelfSelfEnd:
		// Handled specially in positionItemsCrossAxis based on item's own writing-mode/direction.
		return css.AlignItemsFlexStart // placeholder, overridden below
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

// isNonEmptyTextNode returns true if the box is a text node with non-whitespace content
// and non-zero width (visible text).
func isNonEmptyTextNode(box *Box) bool {
	if box.Node == nil || box.Node.Type != html.TextNode {
		return false
	}
	if strings.TrimSpace(box.Node.Text) == "" {
		return false
	}
	if box.Width == 0 {
		return false
	}
	return true
}

// fontAscentFromStyle computes the font ascent for the given CSS style.
func fontAscentFromStyle(style *css.Style) float64 {
	fontSize := style.GetFontSize()
	bold := style.GetFontWeight() == css.FontWeightBold
	italic := style.GetFontStyle() == css.FontStyleItalic
	mono := style.IsMonospaceFamily()
	ahem := style.IsAhemFamily()
	return text.FontAscent(fontSize, bold, italic, mono, ahem)
}

// computeItemFirstBaseline computes the first baseline of a flex item,
// measured from the item's border-box top edge (for row flex) or left edge (for column flex).
// Returns -1 if no baseline can be determined (should synthesize at cross-end).
func computeItemFirstBaseline(item *FlexItem, isRow bool) float64 {
	box := item.Box

	// Vertical writing mode containers: no horizontal baseline, synthesize.
	if box.Parent != nil && box.Parent.Style != nil {
		if wm, ok := box.Parent.Style.Get("writing-mode"); ok {
			if wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr" {
				return -1
			}
		}
	}

	if !isRow {
		return computeBoxFirstBaselineVertical(box)
	}
	return computeBoxFirstBaselineHorizontal(box)
}

// computeBoxFirstBaselineHorizontal returns the first baseline of a box measured
// from its top border-box edge. Returns -1 if no baseline can be found.
func computeBoxFirstBaselineHorizontal(box *Box) float64 {
	if box == nil || box.Style == nil {
		return -1
	}

	// Replaced elements synthesize baseline at margin-box bottom.
	if box.Node != nil {
		tag := box.Node.TagName
		if tag == "img" || tag == "canvas" || tag == "svg" || tag == "video" || tag == "object" || tag == "embed" {
			return -1
		}
	}

	if len(box.Children) == 0 {
		return -1
	}

	hasInlineContent := false
	for _, child := range box.Children {
		if isNonEmptyTextNode(child) {
			hasInlineContent = true
			break
		}
		if child.Node != nil && child.Node.Type == html.ElementNode && child.Style != nil {
			d := child.Style.GetDisplay()
			if d == css.DisplayInline || d == css.DisplayInlineBlock || d == css.DisplayInlineFlex {
				hasInlineContent = true
				break
			}
			break
		}
	}

	if hasInlineContent {
		fontSize := box.Style.GetFontSize()
		lineHeight := box.Style.GetLineHeight()
		ascent := fontAscentFromStyle(box.Style)
		baselineInLine := (lineHeight-fontSize)/2 + ascent
		return box.Border.Top + box.Padding.Top + baselineInLine
	}

	// Only block children: recurse into the first in-flow block child
	for _, child := range box.Children {
		if child.Node != nil && child.Node.Type == html.ElementNode {
			childBaseline := computeBoxFirstBaselineHorizontal(child)
			if childBaseline >= 0 {
				return (child.Y - box.Y) + childBaseline
			}
		}
	}

	return -1
}

// computeBoxLastBaselineHorizontal returns the last baseline of a box measured
// from its top border-box edge. Returns -1 if no baseline can be found.
func computeBoxLastBaselineHorizontal(box *Box) float64 {
	if box == nil || box.Style == nil {
		return -1
	}

	if box.Node != nil {
		tag := box.Node.TagName
		if tag == "img" || tag == "canvas" || tag == "svg" || tag == "video" || tag == "object" || tag == "embed" {
			return -1
		}
	}

	if len(box.Children) == 0 {
		return -1
	}

	hasInlineContent := false
	for i := len(box.Children) - 1; i >= 0; i-- {
		child := box.Children[i]
		if isNonEmptyTextNode(child) {
			hasInlineContent = true
			break
		}
		if child.Node != nil && child.Node.Type == html.ElementNode && child.Style != nil {
			d := child.Style.GetDisplay()
			if d == css.DisplayInline || d == css.DisplayInlineBlock || d == css.DisplayInlineFlex {
				hasInlineContent = true
				break
			}
			break
		}
	}

	if hasInlineContent {
		fontSize := box.Style.GetFontSize()
		lineHeight := box.Style.GetLineHeight()
		ascent := fontAscentFromStyle(box.Style)
		baselineInLine := (lineHeight-fontSize)/2 + ascent

		lastLineY := -1.0
		for i := len(box.Children) - 1; i >= 0; i-- {
			child := box.Children[i]
			if isNonEmptyTextNode(child) {
				lastLineY = child.Y - box.Y
				break
			}
			if child.Node != nil && child.Node.Type == html.ElementNode {
				lastLineY = child.Y - box.Y
				break
			}
		}
		if lastLineY >= 0 {
			return lastLineY + baselineInLine
		}
		return box.Border.Top + box.Padding.Top + baselineInLine
	}

	for i := len(box.Children) - 1; i >= 0; i-- {
		child := box.Children[i]
		if child.Node != nil && child.Node.Type == html.ElementNode {
			childBaseline := computeBoxLastBaselineHorizontal(child)
			if childBaseline >= 0 {
				return (child.Y - box.Y) + childBaseline
			}
		}
	}

	return -1
}

// computeBoxFirstBaselineVertical returns the first baseline of a box measured
// from its top border-box edge (for column-direction cross-axis alignment).
func computeBoxFirstBaselineVertical(box *Box) float64 {
	return computeBoxFirstBaselineHorizontal(box)
}

// computeItemLastBaseline computes the last baseline of a flex item.
// Returns -1 if no baseline can be determined.
func computeItemLastBaseline(item *FlexItem, isRow bool) float64 {
	box := item.Box

	if box.Parent != nil && box.Parent.Style != nil {
		if wm, ok := box.Parent.Style.Get("writing-mode"); ok {
			if wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr" {
				return -1
			}
		}
	}

	if !isRow {
		return computeBoxLastBaselineHorizontal(box)
	}
	return computeBoxLastBaselineHorizontal(box)
}

// computeFlexItemAutoMinMain computes the content-based minimum main size for a flex item.
// Per CSS Flexbox §4.5, this is the smaller of the content size suggestion and specified size suggestion.
// availableCross is the container's content cross-axis size (used to clamp replaced-element cross widths
// for the transferred size suggestion in column direction).
// computedStyles provides already-cascaded styles for the item's children (with inheritance applied);
// if nil, a fresh cascade (without inheritance) is computed for each child.
func (le *LayoutEngine) computeFlexItemAutoMinMain(node *html.Node, style *css.Style, box *Box, isRow bool, availableCross float64, computedStyles map[*html.Node]*css.Style) float64 {
	if isRow {
		// Row direction: min-width: auto → content-based minimum WIDTH

		// CSS Flexbox §4.5: For replaced elements (img, canvas, etc.) with an intrinsic
		// aspect ratio in row direction, the auto minimum WIDTH is the "transferred size
		// suggestion": cross-axis content height × (intrinsicWidth / intrinsicHeight).
		// The cross-axis size is determined by explicit height, min-height, or the initial
		// layout height (in priority order).
		if node != nil && node.TagName == "img" {
			if src, ok := node.GetAttribute("src"); ok {
				if iw, ih, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil && iw > 0 && ih > 0 {
					// Determine effective cross-axis (height) size from CSS constraints.
					// Prefer: explicit height > min-height > natural layout height.
					// Also apply max-height clamping (CSS §4.5: "constraints in the other dimension").
					var crossH float64
					if h, ok := style.GetLength("height"); ok {
						// Explicit CSS height: definite cross size
						crossH = h
					} else if minH, ok := style.GetLength("min-height"); ok {
						// min-height establishes a minimum cross constraint
						crossH = minH
					} else {
						// Fall back to the item's initial layout height (content box)
						crossH = box.Height - box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
					}
					// Apply max-height as an upper bound on the effective cross size.
					if maxH, ok := style.GetLength("max-height"); ok && maxH < crossH {
						crossH = maxH
					}
					if crossH > 0 {
						contentMinSize := crossH * float64(iw) / float64(ih)
						// Specified size suggestion: the item's computed width, if definite.
						if w, ok := style.GetLength("width"); ok {
							if contentMinSize > w {
								return w
							}
						}
						return contentMinSize
					}
				}
			}
		}

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
				childStyle := computedStyles[child]
				if childStyle == nil {
					childStyle = css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
				}
				if childStyle == nil {
					continue
				}
				if wm, ok := childStyle.Get("writing-mode"); ok {
					if wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr" {
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

		// Build a styles map for inheritance: store the flex item's style so children can inherit.
		inheritStyles := map[*html.Node]*css.Style{node: style}

		for _, child := range node.Children {
			var childStyle *css.Style
			if child.Type == html.TextNode {
				// Text nodes inherit all properties from their parent element
				childStyle = style
			} else {
				childStyle = computedStyles[child]
				if childStyle == nil {
					childStyle = css.ComputeStyle(child, le.stylesheets, le.viewport.width, le.viewport.height)
				}
				if childStyle == nil {
					childStyle = style
				} else {
					// Apply CSS inheritance from the flex item's style
					css.ApplyInheritedProperties(child, childStyle, inheritStyles)
				}
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
		// CSS Flexbox §4.5: for replaced elements with intrinsic aspect ratio,
		// the automatic minimum height is the "transferred size suggestion":
		// cross-axis content width × (intrinsicHeight / intrinsicWidth).
		// This ensures e.g. a 100×100 image with width:30px gets min-height=30px,
		// not 100px (the explicit height).
		transferred := false
		if node.TagName == "img" {
			if src, ok := node.GetAttribute("src"); ok {
				if iw, ih, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil && iw > 0 && ih > 0 {
					// Cross-axis content width: use the item's initial layout width, but clamp
					// to the container's available cross size when the img's natural width
					// exceeds it. Without clamping, an img with natural width 100px in a
					// 34px-wide container would use crossW=96 → min-height=96, preventing
					// the flex algorithm from shrinking it to the correct transferred size
					// (e.g. 30px, derived from min-width:30px or container max-width:34px).
					// CSS Flexbox §4.5: use the definite cross size from constraints.
					effectiveCrossBox := box.Width
					if availableCross > 0 && availableCross < effectiveCrossBox {
						effectiveCrossBox = availableCross
					}
					// Also apply item's own min-width constraint from below.
					if minW, ok := style.GetLength("min-width"); ok {
						minBB := minW + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right
						if effectiveCrossBox < minBB {
							effectiveCrossBox = minBB
						}
					}
					crossW := effectiveCrossBox - box.Padding.Left - box.Padding.Right - box.Border.Left - box.Border.Right
					if crossW > 0 {
						contentMinHeight = crossW * float64(ih) / float64(iw)
						transferred = true
					}
				}
			}
		}
		if !transferred {
			// Fallback: use the actual content height
			contentMinHeight = box.Height - box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
		}
	} else {
		// Block containers: compute content height based on whether the item has
		// an explicit CSS height property or not.
		//
		// Case 1: No explicit CSS height — box.Height reflects the natural content
		// height from the initial layoutNode call (text wraps at the item's specified
		// width, block children stack normally). Use box.Height directly.
		// This correctly handles inline content: e.g. "a b c d e f" at 20px in a
		// 50px-wide item layouts to 2 lines (H=48), not 10 fragment boxes (H=150).
		//
		// Case 2: Explicit CSS height — box.Height equals the CSS height, not the
		// natural content height. Use the sum of children heights to approximate
		// the content-based size. This lets the item shrink below its explicit CSS
		// height when the flex container forces shrinking (AutoMinMain should be
		// the content height, which may be much less than the CSS height).
		_, hasExplicitH := style.GetLength("height")
		_, hasPctH := style.GetPercentage("height")
		if !hasExplicitH && !hasPctH {
			contentMinHeight = box.Height - box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
		} else {
			// CSS Flexbox §4.5: For flex container items, the content-size suggestion
			// is the min-content height, which uses flex-basis values (not laid-out heights
			// that were inflated by flex-grow within the explicit height).
			itemDisplay := style.GetDisplay()
			isFlexColumn := (itemDisplay == css.DisplayFlex || itemDisplay == css.DisplayInlineFlex) &&
				(style.GetFlexDirection() == css.FlexDirectionColumn || style.GetFlexDirection() == css.FlexDirectionColumnReverse)
			if isFlexColumn {
				// Sum children's flex-basis contributions as min-content height.
				for _, child := range box.Children {
					if child == nil || child.Style == nil {
						continue
					}
					childBasis := child.Style.GetFlexBasisValue()
					var childContribution float64
					if childBasis.IsAuto || childBasis.IsContent {
						// Auto/content basis: use the child's intrinsic content height
						childContribution = child.Height + child.Margin.Top + child.Margin.Bottom
					} else if childBasis.IsPercent {
						// Percentage basis in indefinite context: treat as content
						childContribution = child.Height + child.Margin.Top + child.Margin.Bottom
					} else {
						// Explicit length basis (e.g. flex-basis:0px)
						// CSS Flexbox §4.5: The content-based minimum uses the
						// hypothetical main size = max(flex-basis, auto-min-main).
						// Compute the child's content-based min height from its children.
						childContentMin := 0.0
						for _, gc := range child.Children {
							if gc == nil {
								continue
							}
							childContentMin += gc.Height + gc.Margin.Top + gc.Margin.Bottom
						}
						childContribution = math.Max(childBasis.Length, childContentMin) + child.Margin.Top + child.Margin.Bottom
					}
					contentMinHeight += childContribution
				}
			} else {
				for _, child := range box.Children {
					if child == nil {
						continue
					}
					contentMinHeight += child.Height + child.Margin.Top + child.Margin.Bottom
				}
			}
		}
		// A block container with only float children has a strut height (from the
		// default line-height) rather than the float-encompassing height (which
		// requires overflow: hidden / clearfix). To get the content-based minimum
		// height, check if any child extends below the container's content area.
		// This handles cases like two 50px floats stacking vertically in 100px width.
		// Skip for flex containers: their content-size was already computed from
		// flex-basis values above, and laid-out positions reflect flex-grow inflation.
		itemDisplay := style.GetDisplay()
		isFlex := (itemDisplay == css.DisplayFlex || itemDisplay == css.DisplayInlineFlex)
		if !isFlex {
			contentStartY := box.Y + box.Padding.Top + box.Border.Top
			for _, child := range box.Children {
				if child == nil {
					continue
				}
				childBottom := child.Y + child.Height + child.Margin.Top + child.Margin.Bottom - contentStartY
				if childBottom > contentMinHeight {
					contentMinHeight = childBottom
				}
			}
		}
	}
	if contentMinHeight < 0 {
		contentMinHeight = 0
	}

	// Specified size suggestion: the item's computed height, if definite
	// CSS Flexbox §4.5: min(content-size suggestion, specified-size suggestion)
	if h, ok := style.GetLength("height"); ok {
		if contentMinHeight > h {
			return h
		}
	} else if pctH, ok := style.GetPercentage("height"); ok {
		// Percentage height resolves against the parent's content height (box.Height
		// was already resolved during layout). Use the resolved value as specified size.
		resolvedH := box.Height - box.Padding.Top - box.Padding.Bottom - box.Border.Top - box.Border.Bottom
		if pctH > 0 && resolvedH > 0 && contentMinHeight > resolvedH {
			return resolvedH
		}
	}
	return contentMinHeight
}

// Helper methods on FlexItem

// HypotheticalOuterMain returns the outer hypothetical main size (main size + margins + padding + border).
// For visibility:collapse items, returns 0 — collapsed items have no main-axis contribution.
func (item *FlexItem) HypotheticalOuterMain(isRow bool) float64 {
	if item.Collapsed {
		return 0
	}
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

// resolveIntrinsicKeyword checks if a CSS property value is "min-content" or "max-content"
// and returns (keyword, true) if so. Returns ("", false) otherwise.
func resolveIntrinsicKeyword(val string) (keyword string, ok bool) {
	switch val {
	case "min-content":
		return "min-content", true
	case "max-content":
		return "max-content", true
	}
	return "", false
}

// resolveItemIntrinsicWidthConstraint resolves a "min-content" or "max-content" keyword
// for a width-dimension constraint on a flex item. Returns (resolved, true) if the CSS
// property has an intrinsic keyword value, or (0, false) otherwise.
// Accounts for writing-mode: vertical-rl/lr items whose box.Width reflects the transform.
func (le *LayoutEngine) resolveItemIntrinsicWidthConstraint(item *FlexItem, keyword string) (float64, bool) {
	if item.Box.Node == nil {
		return 0, false
	}
	// For items with writing-mode: vertical-rl/lr, block layout already applied
	// transformToVerticalRL so item.Box.Width reflects the correct natural width
	// (sum of column widths). ComputeMinMaxSizes ignores writing-mode transforms,
	// so use the laid-out width directly.
	if wm, ok := item.Box.Style.Get("writing-mode"); ok {
		if wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr" {
			natural := item.Box.Width
			switch keyword {
			case "min-content":
				return natural, true
			case "max-content":
				return natural, true
			}
		}
	}
	constraint := NewConstraintSpace(le.viewport.width, -1)
	sizes := le.ComputeMinMaxSizes(item.Box.Node, constraint, item.Box.Style)
	switch keyword {
	case "min-content":
		return sizes.MinContentSize, true
	case "max-content":
		return sizes.MaxContentSize, true
	}
	return 0, false
}

// resolveItemIntrinsicHeightConstraint resolves a "min-content" or "max-content" keyword
// for a height-dimension constraint on a flex item. Returns (resolved, true) if the CSS
// property has an intrinsic keyword value, or (0, false) otherwise.
// Uses the laid-out box's natural height (from initial block layout before flex resolution).
func resolveItemIntrinsicHeightConstraint(item *FlexItem, keyword string) (float64, bool) {
	if item.Box.Node == nil {
		return 0, false
	}
	// The item.Box.Height from initial block layout (before flex overwrites it) is the
	// natural height of the content. For items with no explicit CSS height this equals
	// the min-content height; for items with explicit CSS height we sum children.
	style := item.Box.Style
	var naturalH float64
	if _, hasExplicitH := style.GetLength("height"); !hasExplicitH {
		// No explicit height: box.Height from block layout IS the natural content height.
		naturalH = item.Box.Height
	} else {
		// Explicit CSS height: box.Height = CSS height. Use sum of children as min-content.
		for _, child := range item.Box.Children {
			if child == nil {
				continue
			}
			naturalH += child.Height + child.Margin.Top + child.Margin.Bottom
		}
		naturalH += item.Box.Padding.Top + item.Box.Padding.Bottom +
			item.Box.Border.Top + item.Box.Border.Bottom
	}
	if naturalH < 0 {
		naturalH = 0
	}
	// For both "min-content" and "max-content" height: return naturalH.
	// (A block element's min-content and max-content heights are typically equal
	// since height doesn't involve wrapping.)
	switch keyword {
	case "min-content", "max-content":
		return naturalH, true
	}
	return 0, false
}

// transformToVerticalRL transforms a horizontally laid-out box to vertical layout.
// Each horizontal line becomes a vertical column. For vertical-rl/sideways-rl,
// columns stack right-to-left; for vertical-lr/sideways-lr, left-to-right.
// Content within each column flows top-to-bottom (original X offset → Y offset).
// Lines are detected by grouping children with similar Y positions.
// If the box has explicit CSS width/height, those dimensions are preserved.
func transformToVerticalRL(box *Box, wm string) {
	if len(box.Children) == 0 {
		return
	}

	isLR := wm == "vertical-lr" || wm == "sideways-lr"

	// Create Dir for block-direction margin access
	var dir Dir
	if isLR {
		dir = NewDir(VerticalLR)
	} else {
		dir = NewDir(VerticalRL)
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
	// Column height = max extent after repositioning (origRelX + child.Height),
	// not just max(child.Height), because children at different X offsets
	// end up at different Y positions within the column.
	// Block-direction margins are subtracted from origRelX since they're handled
	// separately by column spacing (they shouldn't become inline offsets).
	type colInfo struct {
		width          float64 // max child width → column width
		height         float64 // max (origRelX + child.Height) → column content extent
		blockStartMarg float64 // max block-start margin among children in this column
		blockEndMarg   float64 // max block-end margin among children in this column
		hasExplicitW   bool    // true if any child has explicit CSS width
	}

	// Compute the parent's content width for detecting block-fill children
	parentContentW := box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right

	cols := make([]colInfo, len(lines))
	for i, line := range lines {
		for _, child := range line.children {
			if child.Width > cols[i].width {
				cols[i].width = child.Width
			}
			// Check if child has explicit CSS width (not block-fill)
			if child.Style != nil {
				if _, ok := child.Style.GetLength("width"); ok {
					cols[i].hasExplicitW = true
				} else if _, ok := child.Style.GetPercentage("width"); ok {
					cols[i].hasExplicitW = true
				}
			}
			// Subtract both block-start and block-end margins from origRelX:
			// block-direction margins were applied to X by the h-tb layout
			// but should not become Y offsets in the vertical column.
			// Column spacing handles block-direction margins instead.
			blockStartM := dir.BlockStartEdge(child.Margin)
			blockEndM := dir.BlockEndEdge(child.Margin)
			origRelX := child.X - contentStartX - blockStartM - blockEndM
			if origRelX < 0 {
				origRelX = 0
			}
			extent := origRelX + child.Height
			if extent > cols[i].height {
				cols[i].height = extent
			}
			// Collect block-direction margins for column spacing when child has
			// explicit width. Block-fill children (no explicit width) have their
			// block-direction margins already consumed by h-tb block-fill calculation,
			// so adding them again as column spacing would double-count.
			if cols[i].hasExplicitW {
				bs := blockStartM
				be := dir.BlockEndEdge(child.Margin)
				if bs > cols[i].blockStartMarg {
					cols[i].blockStartMarg = bs
				}
				if be > cols[i].blockEndMarg {
					cols[i].blockEndMarg = be
				}
			}
		}
	}
	_ = parentContentW // used for future block-fill detection

	// Compute collapsed margins between adjacent columns.
	// colMargins[i] is the margin gap before column i (between column i-1 and column i).
	// First column gets its block-start margin. Between adjacent columns, margins are
	// collapsed. Last column's block-end margin is NOT added to totalWidth since it
	// participates in parent-child margin collapsing or extends beyond the container.
	colMargins := make([]float64, len(cols))
	for i := range cols {
		if i == 0 {
			colMargins[i] = cols[i].blockStartMarg
		} else {
			// Collapse adjacent block-end and block-start margins
			colMargins[i] = collapseMargins(cols[i-1].blockEndMarg, cols[i].blockStartMarg)
		}
	}

	// Total new dimensions including column margins (except last block-end)
	totalWidth := 0.0
	for i, c := range cols {
		totalWidth += colMargins[i] + c.width
	}
	maxContentHeight := 0.0
	for _, c := range cols {
		if c.height > maxContentHeight {
			maxContentHeight = c.height
		}
	}

	// Determine if box has explicit CSS dimensions
	hasExplicitWidth := false
	hasExplicitHeight := false
	if box.Style != nil {
		if _, ok := box.Style.GetLength("width"); ok {
			hasExplicitWidth = true
		} else if _, ok := box.Style.GetPercentage("width"); ok {
			hasExplicitWidth = true
		}
		if _, ok := box.Style.GetLength("height"); ok {
			hasExplicitHeight = true
		} else if _, ok := box.Style.GetPercentage("height"); ok {
			hasExplicitHeight = true
		}
	}

	// Content area width for column positioning
	contentWidth := totalWidth
	if hasExplicitWidth {
		contentWidth = box.Width - box.Padding.Left - box.Padding.Right - box.Border.Left - box.Border.Right
	}

	// Reposition children into columns, shifting descendants by the same delta.
	// Block-direction margins are subtracted from origRelX since column spacing handles them.
	if isLR {
		// vertical-lr: columns stack left-to-right
		colX := 0.0
		for i, line := range lines {
			colX += colMargins[i] // Add margin before this column
			for _, child := range line.children {
				blockStartM := dir.BlockStartEdge(child.Margin)
				blockEndM := dir.BlockEndEdge(child.Margin)
				origRelX := child.X - contentStartX - blockStartM - blockEndM
				if origRelX < 0 {
					origRelX = 0
				}
				newX := contentStartX + colX
				newY := contentStartY + origRelX
				dx := newX - child.X
				dy := newY - child.Y
				child.X = newX
				child.Y = newY
				shiftAllDescendants(child, dx, dy)
			}
			colX += cols[i].width
		}
	} else {
		// vertical-rl: columns stack right-to-left
		colX := contentWidth
		for i, line := range lines {
			colX -= cols[i].width
			// Subtract margin before this column
			if i > 0 {
				colX -= colMargins[i]
			}
			for _, child := range line.children {
				blockStartM := dir.BlockStartEdge(child.Margin)
				blockEndM := dir.BlockEndEdge(child.Margin)
				origRelX := child.X - contentStartX - blockStartM - blockEndM
				if origRelX < 0 {
					origRelX = 0
				}
				newX := contentStartX + colX
				newY := contentStartY + origRelX
				dx := newX - child.X
				dy := newY - child.Y
				child.X = newX
				child.Y = newY
				shiftAllDescendants(child, dx, dy)
			}
		}
	}

	// Update box dimensions (border-box), preserving explicit CSS values
	if !hasExplicitWidth {
		box.Width = totalWidth + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right
	}
	if !hasExplicitHeight {
		box.Height = maxContentHeight + box.Padding.Top + box.Padding.Bottom + box.Border.Top + box.Border.Bottom
	}

}

// transformBoxToVerticalRecursive recursively transforms a box and its descendants
// so that inline text content is oriented vertically. For leaf text boxes (no children,
// has text content), this splits text into per-character boxes and stacks them vertically.
// For non-leaf boxes, recursively transforms children first, then applies transformToVerticalRL.
func transformBoxToVerticalRecursive(box *Box, wm string) {
	if box == nil {
		return
	}

	// Leaf text box: split text into individual character boxes, stacked vertically.
	// Each character occupies one horizontal "line" so transformToVerticalRL at
	// the parent level can convert them to a vertical column.
	if len(box.Children) == 0 && box.Node != nil && box.Node.Type == html.TextNode && strings.TrimSpace(box.Node.Text) != "" {
		textContent := box.Node.Text
		// Apply text-transform if needed
		if box.Style != nil {
			textContent = ApplyTextTransform(textContent, box.Style.GetTextTransform())
		}
		// Collapse whitespace per CSS rules
		if box.Style != nil {
			ws, _ := box.Style.Get("white-space")
			if ws != "pre" && ws != "pre-wrap" && ws != "pre-line" {
				textContent = strings.Join(strings.Fields(textContent), " ")
			}
		}
		if textContent == "" {
			return
		}

		fontSize := 16.0
		isBold := false
		isItalic := false
		isMono := false
		isAhem := false
		if box.Style != nil {
			fontSize = box.Style.GetFontSize()
			isBold = box.Style.GetFontWeight() == css.FontWeightBold
			isItalic = box.Style.GetFontStyle() == css.FontStyleItalic
			isMono = box.Style.IsMonospaceFamily()
			isAhem = box.Style.IsAhemFamily()
		}

		// Use the line-height for character box heights in vertical text.
		// Each character in vertical writing occupies one line, so the character
		// slot height = line-height (not font metrics height).
		lineHeight := fontSize // default: normal ≈ 1.2 * fontSize
		if box.Style != nil {
			lineHeight = box.Style.GetLineHeight()
		}
		charHeight := lineHeight
		if charHeight <= 0 {
			charHeight = fontSize
		}

		// Split into characters and create child boxes
		runes := []rune(textContent)
		charBoxes := make([]*Box, 0, len(runes))
		curX := box.X
		for _, ch := range runes {
			charStr := string(ch)
			charW, _ := text.MeasureTextWithStyle(charStr, fontSize, isBold, isItalic, isMono, isAhem)
			if charW == 0 {
				continue
			}
			// Create a synthetic text node for this character
			charNode := &html.Node{
				Type: html.TextNode,
				Text: charStr,
			}
			charBox := &Box{
				Node:   charNode,
				Style:  box.Style,
				X:      curX,
				Y:      box.Y,
				Width:  charW,
				Height: charHeight,
				Parent: box,
			}
			charBoxes = append(charBoxes, charBox)
			curX += charW
		}

		if len(charBoxes) > 0 {
			box.Children = charBoxes
			// Clear the text on the original box so it doesn't draw text AND children
			box.Node = nil
			box.PseudoContent = ""
		}
		return
	}

	// Non-leaf box: recursively transform children
	for _, child := range box.Children {
		transformBoxToVerticalRecursive(child, wm)
	}

	// After children are transformed, apply the column transform to this box
	if len(box.Children) > 0 {
		transformToVerticalRL(box, wm)
	}
}
