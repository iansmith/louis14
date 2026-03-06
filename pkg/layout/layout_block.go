package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"math"
	"sort"
	"strconv"
	"strings"
)

// blockChildEstablishesBFC returns true if the element establishes a new block formatting
// context per CSS 2.1 §9.4.1. Such blocks must not overlap float margin boxes.
func blockChildEstablishesBFC(style *css.Style) bool {
	overflow := style.GetOverflow()
	display := style.GetDisplay()
	return overflow == css.OverflowHidden ||
		overflow == css.OverflowAuto ||
		overflow == css.OverflowScroll ||
		display == css.DisplayFlowRoot ||
		display == css.DisplayFlex ||
		display == css.DisplayInlineFlex ||
		display == css.DisplayGrid ||
		display == css.DisplayInlineGrid
}

// layoutNodeHTB is a convenience wrapper that calls layoutNode with HorizontalTB direction.
// During the Dir-parameterization migration, existing call sites use this wrapper.
// Agents converting subsystems to be Dir-aware replace layoutNodeHTB calls with direct
// layoutNode calls passing the correct Dir.
func (le *LayoutEngine) layoutNodeHTB(node *html.Node, x, y, availableWidth float64, computedStyles map[*html.Node]*css.Style, parent *Box) *Box {
	return le.layoutNode(node, x, y, availableWidth, NewDir(HorizontalTB), computedStyles, parent)
}

func (le *LayoutEngine) layoutNode(node *html.Node, x, y, availableWidth float64, dir Dir, computedStyles map[*html.Node]*css.Style, parent *Box) *Box {
	// Guard against stack overflow on deeply nested pages.
	le.layoutDepth++
	defer func() { le.layoutDepth-- }()
	if le.layoutDepth > 500 {
		return &Box{Node: node, X: x, Y: y}
	}

	// Phase 3: Use computed styles from cascade
	style := computedStyles[node]
	if style == nil {
		// Fallback: check synthetic styles (anonymous blocks / clones from normalization)
		style = le.syntheticStyles[node]
	}
	if style == nil {
		style = css.NewStyle()
	}

	// Compute the Dir for children of this element. If this element has a
	// different writing-mode than the parent's dir, switch to the new Dir.
	elementWM := WritingModeFromStyle(style)
	childDir := dir
	if elementWM != dir.WM {
		childDir = NewDir(elementWM)
	}
	_ = childDir // used by recursive layoutNode calls in subsequent phases

	// Detect orthogonal flow: this element has a different axis than its parent.
	// CSS Writing Modes §7.3.1: orthogonal flow blocks with auto inline-size
	// use shrink-to-fit instead of block-fill.
	elementIsVertical := elementWM == VerticalRL || elementWM == VerticalLR || elementWM == SidewaysLR
	parentIsVertical := false
	if node.Parent != nil {
		parentStyle := computedStyles[node.Parent]
		if parentStyle == nil {
			parentStyle = le.syntheticStyles[node.Parent]
		}
		if parentStyle != nil {
			pWM := WritingModeFromStyle(parentStyle)
			parentIsVertical = pWM == VerticalRL || pWM == VerticalLR || pWM == SidewaysLR
		}
	}
	isOrthogonalFlow := elementIsVertical != parentIsVertical

	// Phase 7: Check display mode early
	display := style.GetDisplay()
	if display == css.DisplayNone {
		return nil
	}

	// CSS Display Level 3: display:contents elements generate no box of their own.
	// Their children participate in the parent's formatting context directly.
	// The element is transparent: no background, border, or float box is created.
	// This must be checked before float handling (CSS 2.1 §9.7 blockification does
	// NOT apply to display:contents per CSS Display Level 3 §2.7).
	//
	// Exception: per CSS Display Level 3 §2.7, if the root element has display:contents,
	// it blockifies to display:block (root element cannot be contents).
	if display == css.DisplayContents {
		if parent == nil && node.TagName == "html" {
			// Root element: blockify contents → block
			display = css.DisplayBlock
		} else {
			return nil
		}
	}

	// Phase 8: Check if this is an img element
	isImage := node.TagName == "img"
	// Phase 24: Check if this is an object element with a loadable image
	isObjectImage := false
	if node.TagName == "object" {
		if data, ok := node.GetAttribute("data"); ok {
			if _, _, err := images.GetImageDimensionsWithFetcher(data, le.imageFetcher); err == nil {
				isObjectImage = true
			}
		}
	}
	var imageWidth, imageHeight int
	var imagePath string
	if isImage {
		// Get image source
		if src, ok := node.GetAttribute("src"); ok {
			imagePath = src
			// Try to load image to get natural dimensions
			if w, h, err := images.GetImageDimensionsWithFetcher(src, le.imageFetcher); err == nil {
				imageWidth = w
				imageHeight = h
			}
		}
		// Replaced elements (img) are inline-block regardless of whether the UA
		// stylesheet sets display:inline or display:block. CSS 2.1 §9.2.2: for
		// replaced elements, vertical padding/margin always apply (unlike
		// non-replaced inlines where they have no layout effect).
		if display == css.DisplayBlock || display == css.DisplayInline {
			display = css.DisplayInlineBlock
		}
	} else if isObjectImage {
		// Object element with loadable image - treat like img
		if data, ok := node.GetAttribute("data"); ok {
			imagePath = data
			if w, h, err := images.GetImageDimensionsWithFetcher(data, le.imageFetcher); err == nil {
				imageWidth = w
				imageHeight = h
			}
		}
		isImage = true
		if display == css.DisplayBlock || display == css.DisplayInline {
			display = css.DisplayInlineBlock
		}
	}

	// Check if this is an SVG element (replaced element with explicit dimensions)
	isSVG := node.TagName == "svg"
	svgWidthFromViewBox := false // true when SVG width was auto-derived from container (viewBox path)

	// Ruby display types: normalize for layout purposes.
	// display:ruby → treat as inline (the ruby element participates in inline flow)
	// display:ruby-text → treat as inline-block (the <rt> annotation box)
	// display:ruby-base → treat as inline
	if display == css.DisplayRuby || display == css.DisplayRubyBase {
		display = css.DisplayInline
	} else if display == css.DisplayRubyText {
		display = css.DisplayInlineBlock
	}

	// Phase 5: Check for float early to determine width calculation
	floatType := style.GetFloat()

	// CSS 2.1 §9.7: Relationships between display, position, and float
	// Floated or absolutely positioned inline elements compute to block display
	if display == css.DisplayInline {
		pos := style.GetPosition()
		if floatType != css.FloatNone || pos == css.PositionAbsolute || pos == css.PositionFixed {
			display = css.DisplayBlock
		}
	}

	// Get box model values
	margin := style.GetMargin()
	padding := style.GetPadding()
	border := style.GetBorderWidth()

	// Resolve calc() expressions in margin against the containing block width.
	// GetMargin() uses GetLength() which handles fixed calc() but not calc()+%.
	resolveCalcMargin := func(prop string) (float64, bool) {
		if val, ok := style.Get(prop); ok {
			trimmed := strings.TrimSpace(val)
			if strings.HasPrefix(trimmed, "calc(") && strings.HasSuffix(trimmed, ")") {
				inner := trimmed[5 : len(trimmed)-1]
				return css.EvalCalcWithPercent(inner, style.GetFontSize(), availableWidth)
			}
		}
		return 0, false
	}
	if v, ok := resolveCalcMargin("margin-top"); ok {
		margin.Top = v
	}
	if v, ok := resolveCalcMargin("margin-right"); ok {
		margin.Right = v
	}
	if v, ok := resolveCalcMargin("margin-bottom"); ok {
		margin.Bottom = v
	}
	if v, ok := resolveCalcMargin("margin-left"); ok {
		margin.Left = v
	}

	// CSS 2.1 §8.3: Percentage margin resolves against the WIDTH of the
	// containing block (even for margin-top and margin-bottom).
	resolveMarginPercent := func(prop string, current float64, isAuto bool) float64 {
		if isAuto || current != 0 {
			return current
		}
		if val, ok := style.Get(prop); ok {
			if pct, ok := css.ParsePercentage(val); ok {
				return availableWidth * pct / 100
			}
		}
		return current
	}
	margin.Top = resolveMarginPercent("margin-top", margin.Top, margin.AutoTop)
	margin.Right = resolveMarginPercent("margin-right", margin.Right, margin.AutoRight)
	margin.Bottom = resolveMarginPercent("margin-bottom", margin.Bottom, margin.AutoBottom)
	margin.Left = resolveMarginPercent("margin-left", margin.Left, margin.AutoLeft)

	// CSS 2.1 §8.4: Percentage padding resolves against the WIDTH of the
	// containing block (even for padding-top and padding-bottom).
	resolvePaddingPercent := func(prop string, current float64) float64 {
		if current == 0 {
			if val, ok := style.Get(prop); ok {
				if pct, ok := css.ParsePercentage(val); ok {
					return availableWidth * pct / 100
				}
			}
		}
		return current
	}
	padding.Top = resolvePaddingPercent("padding-top", padding.Top)
	padding.Right = resolvePaddingPercent("padding-right", padding.Right)
	padding.Bottom = resolvePaddingPercent("padding-bottom", padding.Bottom)
	padding.Left = resolvePaddingPercent("padding-left", padding.Left)

	// Phase 7 Enhancement: Inline elements ignore vertical margins and padding
	if display == css.DisplayInline {
		margin.Top = 0
		margin.Bottom = 0
		padding.Top = 0
		padding.Bottom = 0
	}

	// Apply margin offset
	x += margin.Left
	y += margin.Top

	// Calculate content width
	var contentWidth float64
	hasExplicitWidth := false
	imageHasCSSWidth := false // true only when width comes from CSS/attribute (not natural/fallback)

	// Phase 8: Images use image dimensions or explicit dimensions
	if isImage {
		if w, ok := style.GetLength("width"); ok {
			contentWidth = w
			hasExplicitWidth = true
			imageHasCSSWidth = true
		} else if widthAttr, ok := node.GetAttribute("width"); ok {
			// Parse width attribute
			if w, ok := css.ParseLength(widthAttr); ok {
				contentWidth = w
				hasExplicitWidth = true
				imageHasCSSWidth = true
			}
		} else if imageWidth > 0 {
			// Use natural image width (not CSS-explicit)
			contentWidth = float64(imageWidth)
			hasExplicitWidth = true
		} else {
			// Fallback for missing/broken images (not CSS-explicit)
			contentWidth = 100
			hasExplicitWidth = true
		}
	} else if isSVG {
		// SVG elements are replaced elements — use width/height attributes
		// SVG attributes use unitless numbers (pixels), not CSS lengths
		if w, ok := style.GetLength("width"); ok {
			contentWidth = w
			hasExplicitWidth = true
		} else if widthAttr, ok := node.GetAttribute("width"); ok {
			if w, err := strconv.ParseFloat(widthAttr, 64); err == nil {
				contentWidth = w
				hasExplicitWidth = true
			}
		} else if viewBox, ok := node.GetAttribute("viewbox"); ok {
			// SVG 2 §7.7 / CSS Images Level 3: SVG with viewBox but no explicit
			// width uses the containing block width as its width, then derives
			// height from the viewBox aspect ratio. This handles inline <svg>
			// elements used as flex items with only viewBox dimensions.
			_ = viewBox // viewBox parsing is done below in height section
			contentWidth = availableWidth - margin.Left - margin.Right - padding.Left - padding.Right - border.Left - border.Right
			if contentWidth < 0 {
				contentWidth = 0
			}
			hasExplicitWidth = true
			svgWidthFromViewBox = true
		}
	} else if display == css.DisplayInline {
		// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore width property)
		contentWidth = 0
		hasExplicitWidth = false
	} else if widthVal, ok := style.Get(dir.InlineSizeProp()); ok && (widthVal == "min-content" || widthVal == "max-content" || widthVal == "fit-content") {
		// CSS Intrinsic & Extrinsic Sizing: keyword values
		constraint := NewConstraintSpace(availableWidth, -1)
		sizes := le.ComputeMinMaxSizes(node, constraint, style)
		switch widthVal {
		case "min-content":
			contentWidth = sizes.MinContentSize
		case "max-content":
			contentWidth = sizes.MaxContentSize
		case "fit-content":
			contentWidth = math.Min(sizes.MaxContentSize, math.Max(sizes.MinContentSize, availableWidth-dir.InlineStartEdge(margin)-dir.InlineEndEdge(margin)-dir.InlineBorderBox(padding, border)))
		}
		hasExplicitWidth = true
	} else if w, ok := style.GetLength(dir.InlineSizeProp()); ok {
		contentWidth = w
		hasExplicitWidth = true
	} else if val, hasWidth := style.Get(dir.InlineSizeProp()); hasWidth && strings.HasPrefix(val, "calc(") && strings.HasSuffix(val, ")") && strings.Contains(val, "%") {
		// calc() with percentage: resolve % against containing block inline size
		expr := val[5 : len(val)-1]
		cbWidth := availableWidth
		if style.GetPosition() == css.PositionFixed {
			cbWidth = dir.ViewportInlineSize(le)
		}
		if result, ok := css.EvalCalcWithPercent(expr, style.GetFontSize(), cbWidth); ok {
			contentWidth = result
			hasExplicitWidth = true
		}
	} else if pct, ok := style.GetPercentage(dir.InlineSizeProp()); ok {
		// Percentage inline size resolved against containing block
		cbWidth := availableWidth
		if style.GetPosition() == css.PositionFixed {
			cbWidth = dir.ViewportInlineSize(le)
		}
		contentWidth = cbWidth * pct / 100
		hasExplicitWidth = true
	} else if style.GetPosition() == css.PositionAbsolute || style.GetPosition() == css.PositionFixed {
		// Absolutely positioned elements without explicit width shrink-wrap
		contentWidth = 0
	} else if floatType != css.FloatNone {
		// CSS 2.1 §10.3.5: Floated elements without explicit width use shrink-to-fit
		contentWidth = 0
	} else if display == css.DisplayTable {
		// CSS 2.1 §17.5.2: Tables without explicit width use shrink-to-fit
		contentWidth = 0
	} else if isOrthogonalFlow && !elementIsVertical {
		// CSS Writing Modes §7.3.1: Orthogonal flow blocks (horizontal-tb inside
		// a vertical containing block) with auto inline-size use shrink-to-fit:
		//   used-width = min(max-content, max(min-content, constraint))
		// The constraint is the parent's block-size (availableWidth for the child).
		stfConstraint := availableWidth - dir.InlineStartEdge(margin) - dir.InlineEndEdge(margin) -
			dir.InlineBorderBox(padding, border)
		if stfConstraint < 0 {
			stfConstraint = 0
		}
		constraintSpace := NewConstraintSpace(availableWidth, -1)
		sizes := le.ComputeMinMaxSizes(node, constraintSpace, style)
		// ComputeMinMaxSizes returns border-box sizes; convert to content-box
		paddingBorderInline := dir.InlineBorderBox(padding, border)
		maxContent := sizes.MaxContentSize - paddingBorderInline
		minContent := sizes.MinContentSize - paddingBorderInline
		if maxContent < 0 {
			maxContent = 0
		}
		if minContent < 0 {
			minContent = 0
		}
		contentWidth = math.Min(maxContent, math.Max(minContent, stfConstraint))
	} else {
		// Default to available inline size minus inline margin, padding, border
		contentWidth = availableWidth - dir.InlineStartEdge(margin) - dir.InlineEndEdge(margin) -
			dir.InlineBorderBox(padding, border)
	}

	// Calculate content height
	var contentHeight float64
	hasExplicitHeight := false
	// Phase 8: Images use image dimensions or explicit dimensions
	if isImage {
		if h, ok := style.GetLength("height"); ok {
			contentHeight = h
			hasExplicitHeight = true
		} else if hPct, ok := style.GetPercentage("height"); ok {
			// Percentage height on img: resolve against parent's established height
			// (e.g., grid cell height, explicit parent height). Use parent.Height
			// directly since grid/flex cells provide definite height without CSS height.
			if parent != nil {
				parentContentH := parent.Height - parent.Padding.Top - parent.Padding.Bottom -
					parent.Border.Top - parent.Border.Bottom
				if parentContentH > 0 {
					contentHeight = parentContentH * hPct / 100
					hasExplicitHeight = true
				}
			}
			// When parent height is indeterminate (e.g. auto-height grid row during
			// intrinsic sizing), fall back to natural image dimensions like no height was set.
			if !hasExplicitHeight {
				if imageHeight > 0 {
					if hasExplicitWidth && imageWidth > 0 {
						contentHeight = contentWidth * float64(imageHeight) / float64(imageWidth)
					} else {
						contentHeight = float64(imageHeight)
					}
				} else {
					contentHeight = 100
				}
			}
		} else if heightAttr, ok := node.GetAttribute("height"); ok {
			// Parse height attribute
			if h, ok := css.ParseLength(heightAttr); ok {
				contentHeight = h
				hasExplicitHeight = true
			}
		} else if imageHeight > 0 {
			// Use natural image height, maintaining aspect ratio if width was specified
			if hasExplicitWidth && imageWidth > 0 {
				// Scale height to maintain aspect ratio
				contentHeight = contentWidth * float64(imageHeight) / float64(imageWidth)
			} else {
				contentHeight = float64(imageHeight)
			}
		} else {
			// Fallback for missing/broken images
			contentHeight = 100
		}
	} else if isSVG {
		// SVG elements — use height attribute (unitless numbers = pixels)
		if h, ok := style.GetLength("height"); ok {
			contentHeight = h
			hasExplicitHeight = true
		} else if heightAttr, ok := node.GetAttribute("height"); ok {
			if h, err := strconv.ParseFloat(heightAttr, 64); err == nil {
				contentHeight = h
				hasExplicitHeight = true
			}
		} else if viewBox, ok := node.GetAttribute("viewbox"); ok {
			// SVG 2 §7.7: Derive height from viewBox aspect ratio and computed width.
			parts := strings.Fields(viewBox)
			if len(parts) == 4 {
				vbW, errW := strconv.ParseFloat(parts[2], 64)
				vbH, errH := strconv.ParseFloat(parts[3], 64)
				if errW == nil && errH == nil && vbW > 0 && vbH > 0 {
					contentHeight = contentWidth * vbH / vbW
					hasExplicitHeight = true
				}
			}
		}
	} else if display == css.DisplayInline {
		// Phase 7 Enhancement: Inline elements always shrink-wrap (ignore height property)
		contentHeight = 0
	} else if h, ok := style.GetLength(dir.BlockSizeProp()); ok {
		contentHeight = h
		hasExplicitHeight = true
	} else if hval, hok := style.Get(dir.BlockSizeProp()); hok && strings.HasPrefix(hval, "calc(") && strings.Contains(hval, "%") {
		// calc() with percentage block size: resolve % against containing block block size.
		// Uses same containing-block rules as percentage block size.
		calcCBHeight := 0.0
		if node.TagName == "html" {
			calcCBHeight = dir.ViewportBlockSize(le)
		} else if style.GetPosition() == css.PositionAbsolute || style.GetPosition() == css.PositionFixed {
			cb := findPositionedAncestorBox(parent)
			if cb == nil || style.GetPosition() == css.PositionFixed {
				calcCBHeight = dir.ViewportBlockSize(le)
			} else {
				calcCBHeight = dir.BlockSize(cb) - dir.BlockStartEdge(cb.Border) - dir.BlockEndEdge(cb.Border)
			}
		} else if parent != nil && parent.Style != nil {
			_, hasLen := parent.Style.GetLength(dir.BlockSizeProp())
			_, hasPct := parent.Style.GetPercentage(dir.BlockSizeProp())
			calcHasParentCalcH := false
			if phval, phok := parent.Style.Get(dir.BlockSizeProp()); phok && strings.HasPrefix(phval, "calc(") && strings.Contains(phval, "%") {
				calcHasParentCalcH = true
			}
			if hasLen || hasPct || calcHasParentCalcH || parent.HeightIsDefinite {
				calcCBHeight = dir.BlockSize(parent) - dir.BlockBorderBox(parent.Padding, parent.Border)
			}
		}
		if calcCBHeight > 0 {
			expr := hval[5 : len(hval)-1]
			if result, calcOk := css.EvalCalcWithPercent(expr, style.GetFontSize(), calcCBHeight); calcOk {
				contentHeight = result
				hasExplicitHeight = true
			}
		}
		// else: containing block block size depends on content → treat as auto
	} else if hPct, ok := style.GetPercentage(dir.BlockSizeProp()); ok {
		// CSS 2.1 §10.5: Percentage block sizes resolve against containing block block size
		cbHeight := 0.0
		if node.TagName == "html" {
			// Root element: containing block is initial containing block (viewport)
			cbHeight = dir.ViewportBlockSize(le)
		} else if style.GetPosition() == css.PositionAbsolute || style.GetPosition() == css.PositionFixed {
			// CSS 2.1 §10.1: For absolutely/fixed positioned elements, the containing
			// block is the nearest positioned ancestor's padding box (or viewport)
			cb := findPositionedAncestorBox(parent)
			if cb == nil || style.GetPosition() == css.PositionFixed {
				cbHeight = dir.ViewportBlockSize(le)
			} else {
				cbHeight = dir.BlockSize(cb) - dir.BlockStartEdge(cb.Border) - dir.BlockEndEdge(cb.Border)
			}
		} else if parent != nil && parent.Style != nil {
			// Non-root: resolve against parent's content block size if parent has explicit block size
			// OR if parent is a flex item that was stretched to a definite block size (HeightIsDefinite).
			_, hasLen := parent.Style.GetLength(dir.BlockSizeProp())
			_, hasPct := parent.Style.GetPercentage(dir.BlockSizeProp())
			if hasLen || hasPct || parent.HeightIsDefinite {
				cbHeight = dir.BlockSize(parent) - dir.BlockBorderBox(parent.Padding, parent.Border)
			}
		}
		if cbHeight > 0 {
			contentHeight = cbHeight * hPct / 100
			hasExplicitHeight = true
		}
		// else: containing block block size depends on content → treat as auto
	} else {
		contentHeight = 0 // Auto block size - will be calculated from children
	}

	// CSS aspect-ratio: compute missing dimension from the other + ratio
	ar := style.GetAspectRatio()
	if ar.IsSet && !hasExplicitHeight && contentWidth > 0 {
		contentHeight = contentWidth * ar.Height / ar.Width
		hasExplicitHeight = true
	} else if ar.IsSet && hasExplicitHeight && !hasExplicitWidth {
		contentWidth = contentHeight * ar.Width / ar.Height
		hasExplicitWidth = true
	}
	// For images: CSS height drives width via aspect-ratio (CSS or natural) even when
	// width came from natural/fallback dimensions (not CSS).
	// Handles: block-size/height on img + aspect-ratio, and height:% in grid cells.
	if isImage && hasExplicitHeight && !imageHasCSSWidth {
		if ar.IsSet {
			contentWidth = contentHeight * ar.Width / ar.Height
			hasExplicitWidth = true
		} else if imageWidth > 0 && imageHeight > 0 {
			// Use natural image aspect ratio to compute width from explicit height
			contentWidth = contentHeight * float64(imageWidth) / float64(imageHeight)
			hasExplicitWidth = true
		}
	}

	// CSS3 box-sizing: border-box means specified inline/block size include padding+border
	if style.GetBoxSizing() == "border-box" {
		if hasExplicitWidth {
			contentWidth -= dir.InlineBorderBox(padding, border)
			if contentWidth < 0 {
				contentWidth = 0
			}
		}
		if hasExplicitHeight {
			contentHeight -= dir.BlockBorderBox(padding, border)
			if contentHeight < 0 {
				contentHeight = 0
			}
		}
	}

	// Apply min/max inline size constraints (handles both fixed lengths and calc()+% values).
	resolveCalcWidth := func(prop string) (float64, bool) {
		if v, ok := style.GetLength(prop); ok {
			return v, true
		}
		// Handle plain percentage values (e.g. max-width: 100%).
		// CSS 2.1 §10.2: percentages on min/max inline size resolve against the
		// containing block's (parent's) inline size = availableWidth.
		if pct, ok := style.GetPercentage(prop); ok {
			return pct / 100.0 * availableWidth, true
		}
		if val, ok := style.Get(prop); ok {
			trimmed := strings.TrimSpace(val)
			// CSS Intrinsic Sizing: handle max-content/min-content/fit-content keywords
			// for max-width/min-width. Returns content-box value (sizes from
			// ComputeMinMaxSizes include padding+border, so we subtract them).
			if trimmed == "max-content" || trimmed == "min-content" || trimmed == "fit-content" {
				constraint := NewConstraintSpace(availableWidth, -1)
				sizes := le.ComputeMinMaxSizes(node, constraint, style)
				paddingBorder := dir.InlineBorderBox(padding, border)
				switch trimmed {
				case "min-content":
					return math.Max(0, sizes.MinContentSize-paddingBorder), true
				case "max-content":
					return math.Max(0, sizes.MaxContentSize-paddingBorder), true
				case "fit-content":
					contentArea := availableWidth - dir.InlineStartEdge(margin) - dir.InlineEndEdge(margin) - paddingBorder
					maxC := math.Max(0, sizes.MaxContentSize-paddingBorder)
					minC := math.Max(0, sizes.MinContentSize-paddingBorder)
					return math.Min(maxC, math.Max(minC, contentArea)), true
				}
			}
			if strings.HasPrefix(trimmed, "calc(") && strings.HasSuffix(trimmed, ")") {
				inner := trimmed[5 : len(trimmed)-1]
				// CSS 2.1 §10.2: In a shrink-to-fit context (float parent without explicit
				// inline size), percentage in calc() is undefined — use percentBase=0 so that
				// non-zero % terms fail gracefully (treated as none). Fixed-only calc()
				// expressions still resolve correctly with percentBase=0.
				pctBase := availableWidth
				if strings.Contains(inner, "%") && parent != nil && parent.Style != nil {
					if parent.Style.GetFloat() != css.FloatNone {
						_, parentHasLen := parent.Style.GetLength(dir.InlineSizeProp())
						_, parentHasPct := parent.Style.GetPercentage(dir.InlineSizeProp())
						if !parentHasLen && !parentHasPct {
							pctBase = 0
						}
					}
				}
				return css.EvalCalcWithPercent(inner, style.GetFontSize(), pctBase)
			}
		}
		return 0, false
	}
	resolvedMinWidth := 0.0
	hasMinWidth := false
	if minWidth, ok := resolveCalcWidth(dir.MinInlineSizeProp()); ok {
		hasMinWidth = true
		resolvedMinWidth = minWidth
		if contentWidth < minWidth {
			contentWidth = minWidth
		}
	}
	if maxWidth, ok := resolveCalcWidth(dir.MaxInlineSizeProp()); ok {
		if contentWidth > maxWidth {
			contentWidth = maxWidth
		}
		// CSS 2.1 §10.4: If max inline size < min inline size, min wins.
		// After applying max, re-apply min if we went below it.
		if hasMinWidth && contentWidth < resolvedMinWidth {
			contentWidth = resolvedMinWidth
		}
	}

	// For images: after width clamping, recalculate height from aspect ratio
	// if the height was derived from the (now-clamped) width. CSS replaced element
	// constraint solving: min/max-width affects height when height is auto.
	// Also recalculate when min/max-width changed the width from the natural image width
	// (e.g. min-width:40px on a 20px-wide image → width becomes 40px, height must scale).
	if isImage && !hasExplicitHeight && imageWidth > 0 && imageHeight > 0 &&
		(imageHasCSSWidth || contentWidth != float64(imageWidth)) {
		contentHeight = contentWidth * float64(imageHeight) / float64(imageWidth)
	}

	// For SVG elements with viewBox: after width clamping (max-width/min-width),
	// recalculate height from viewBox aspect ratio if no explicit CSS height.
	if isSVG && !isImage {
		if viewBox, ok := node.GetAttribute("viewbox"); ok {
			parts := strings.Fields(viewBox)
			if len(parts) == 4 {
				vbW, errW := strconv.ParseFloat(parts[2], 64)
				vbH, errH := strconv.ParseFloat(parts[3], 64)
				if errW == nil && errH == nil && vbW > 0 && vbH > 0 {
					expectedH := contentWidth * vbH / vbW
					if _, ok := style.GetLength("height"); !ok {
						contentHeight = expectedH
					}
				}
			}
		}
	}

	// Apply min/max block size constraints (min-block-size overrides max-block-size per CSS 2.1 10.7)
	maxHeightVal := 0.0
	hasMaxHeight := false
	if mh, ok := style.GetLength(dir.MaxBlockSizeProp()); ok {
		maxHeightVal = mh
		hasMaxHeight = true
	} else if mhPct, ok := style.GetPercentage(dir.MaxBlockSizeProp()); ok {
		cbHeight := 0.0
		if node.TagName == "html" {
			cbHeight = dir.ViewportBlockSize(le)
		} else if parent != nil && parent.Style != nil {
			_, hasLen := parent.Style.GetLength(dir.BlockSizeProp())
			_, hasPct := parent.Style.GetPercentage(dir.BlockSizeProp())
			if hasLen || hasPct {
				cbHeight = dir.BlockSize(parent) - dir.BlockBorderBox(parent.Padding, parent.Border)
			}
		}
		if cbHeight > 0 {
			maxHeightVal = cbHeight * mhPct / 100
			hasMaxHeight = true
		}
	}
	if hasMaxHeight && contentHeight > maxHeightVal {
		contentHeight = maxHeightVal
	}
	minHeightVal := 0.0
	hasMinHeight := false
	if mh, ok := style.GetLength(dir.MinBlockSizeProp()); ok {
		minHeightVal = mh
		hasMinHeight = true
	} else if mhPct, ok := style.GetPercentage(dir.MinBlockSizeProp()); ok {
		cbHeight := 0.0
		if node.TagName == "html" {
			cbHeight = dir.ViewportBlockSize(le)
		} else if parent != nil && parent.Style != nil {
			_, hasLen := parent.Style.GetLength(dir.BlockSizeProp())
			_, hasPct := parent.Style.GetPercentage(dir.BlockSizeProp())
			if hasLen || hasPct || parent.HeightIsDefinite {
				cbHeight = dir.BlockSize(parent) - dir.BlockBorderBox(parent.Padding, parent.Border)
			}
		}
		if cbHeight > 0 {
			minHeightVal = cbHeight * mhPct / 100
			hasMinHeight = true
		}
	}
	if hasMinHeight && contentHeight < minHeightVal {
		contentHeight = minHeightVal
	}

	// CSS 2.1 §10.4: For replaced elements with intrinsic aspect ratio, after
	// min/max-height clamping changes the height, recalculate width via AR.
	// This is the height→width analogue of the width→height recalc at line 554.
	// Only applies when width is not explicitly set via CSS (imageHasCSSWidth=false)
	// and height was changed from the natural value by min/max-height constraints.
	if isImage && !imageHasCSSWidth && imageWidth > 0 && imageHeight > 0 {
		expectedH := contentWidth * float64(imageHeight) / float64(imageWidth)
		if (hasMinHeight || hasMaxHeight) && math.Abs(contentHeight-expectedH) > 0.5 {
			newW := contentHeight * float64(imageWidth) / float64(imageHeight)
			// Also respect min/max-width constraints on the transferred width
			if hasMinWidth && newW < resolvedMinWidth {
				newW = resolvedMinWidth
			}
			if maxW, ok := resolveCalcWidth("max-width"); ok && newW > maxW {
				newW = maxW
			}
			contentWidth = newW
		}
	}

	// CSS 2.1 §10.4: For SVG elements with viewBox aspect ratio, after
	// min/max-height clamping changes the height, recalculate width via AR.
	// Only applies when SVG width was auto-derived from container (not explicitly set).
	if isSVG && svgWidthFromViewBox && (hasMinHeight || hasMaxHeight) {
		if viewBox, ok := node.GetAttribute("viewbox"); ok {
			parts := strings.Fields(viewBox)
			if len(parts) == 4 {
				vbW, errW := strconv.ParseFloat(parts[2], 64)
				vbH, errH := strconv.ParseFloat(parts[3], 64)
				if errW == nil && errH == nil && vbW > 0 && vbH > 0 {
					expectedH := contentWidth * vbH / vbW
					if math.Abs(contentHeight-expectedH) > 0.5 {
						newW := contentHeight * vbW / vbH
						// Also respect min/max-width constraints on the transferred width
						if hasMinWidth && newW < resolvedMinWidth {
							newW = resolvedMinWidth
						}
						if maxW, ok := resolveCalcWidth("max-width"); ok && newW > maxW {
							newW = maxW
						}
						contentWidth = newW
					}
				}
			}
		}
	}

	// Phase 13: Handle margin: auto for inline-direction centering
	// Only center if both inline-start and inline-end margins are auto
	if dir.AutoInlineStart(margin) && dir.AutoInlineEnd(margin) {
		// For block-level elements with auto inline margins, center them
		// Calculate total inline size including padding and border
		totalWidth := contentWidth + dir.InlineBorderBox(padding, border)
		// Center within available inline size
		if totalWidth < availableWidth {
			centerOffset := (availableWidth - totalWidth) / 2
			x = x + centerOffset
		}
	}

	// HTML <center> element: center block-level children (per HTML spec UA styles)
	// Browsers use text-align: -webkit-center which centers block children too
	if !dir.AutoInlineStart(margin) && !dir.AutoInlineEnd(margin) && parent != nil && parent.Node != nil &&
		parent.Node.TagName == "center" &&
		(display == css.DisplayTable || display == css.DisplayBlock || display == css.DisplayFlowRoot || display == css.DisplayFlex) {
		totalWidth := contentWidth + dir.InlineBorderBox(padding, border)
		if totalWidth < availableWidth {
			centerOffset := (availableWidth - totalWidth) / 2
			x = x + centerOffset
		}
	}

	// Phase 4: Get positioning information
	position := style.GetPosition()
	zindex := style.GetZIndex()

	// Phase 5: Check for clear property
	clearType := style.GetClear()

	// Phase 5: Handle clear property - move Y down past floats
	if clearType != css.ClearNone {
		y = le.getClearY(clearType, y)
	}

	// Track whether this box has zero initial content height (auto, no min-height).
	// If true, position:relative children with percentage top/bottom were computed
	// against cbHeight=0 and need a post-layout correction pass.
	parentContentHeightIsZero := !hasExplicitHeight && contentHeight == 0

	box := &Box{
		Node:      node,
		Style:     style,
		X:         x,
		Y:         y,
		Margin:    margin,
		Padding:   padding,
		Border:    border,
		Children:  make([]*Box, 0),
		Position:  position,
		ZIndex:    zindex,
		Parent:    parent,
		ImagePath: imagePath, // Phase 8: Store image path for rendering
	}
	// Set inline and block sizes using Dir-aware accessors so that
	// contentWidth (inline) and contentHeight (block) map to the correct
	// physical box dimensions for the current writing-mode.
	dir.SetInlineSize(box, contentWidth+dir.InlineBorderBox(padding, border))
	dir.SetBlockSize(box, contentHeight+dir.BlockBorderBox(padding, border))

	// Block-in-inline normalization: if this node is a clone produced by
	// splitInlineAroundBlocks, set fragment flags for correct border rendering.
	if fragType, ok := node.GetAttribute("data-block-fragment"); ok {
		switch fragType {
		case "first":
			box.IsFirstFragment = true
		case "last":
			box.IsLastFragment = true
		case "middle":
			box.IsFirstFragment = true
			box.IsLastFragment = true
		}
	}

	// Phase 5: Float positioning will be done AFTER children are laid out
	// (to support shrink-wrapping and float drop)

	// Phase 4: Handle positioning (sticky at scroll=0 doesn't apply offsets)
	if position == css.PositionRelative {
		// Relative positioning: offset from normal position
		offset := style.GetPositionOffset()

		// CSS 2.1 §9.4.3: Percentage offsets resolve against containing block dimensions
		if !offset.HasTop {
			if pct, ok := style.GetPercentage("top"); ok {
				cbHeight := 0.0
				if parent != nil {
					cbHeight = parent.Height - parent.Border.Top - parent.Border.Bottom - parent.Padding.Top - parent.Padding.Bottom
				}
				offset.Top = cbHeight * (pct / 100.0)
				offset.HasTop = true
			}
		}
		if !offset.HasBottom {
			if pct, ok := style.GetPercentage("bottom"); ok {
				cbHeight := 0.0
				if parent != nil {
					cbHeight = parent.Height - parent.Border.Top - parent.Border.Bottom - parent.Padding.Top - parent.Padding.Bottom
				}
				offset.Bottom = cbHeight * (pct / 100.0)
				offset.HasBottom = true
			}
		}
		if !offset.HasLeft {
			if pct, ok := style.GetPercentage("left"); ok {
				cbWidth := availableWidth
				offset.Left = cbWidth * (pct / 100.0)
				offset.HasLeft = true
			}
		}
		if !offset.HasRight {
			if pct, ok := style.GetPercentage("right"); ok {
				cbWidth := availableWidth
				offset.Right = cbWidth * (pct / 100.0)
				offset.HasRight = true
			}
		}

		if offset.HasTop {
			box.Y += offset.Top
		} else if offset.HasBottom {
			box.Y -= offset.Bottom
		}
		if offset.HasLeft {
			box.X += offset.Left
		} else if offset.HasRight {
			box.X -= offset.Right
		}
	} else if position == css.PositionAbsolute || position == css.PositionFixed {
		// Absolutely positioned elements - positioning applied after children layout
		le.absoluteBoxes = append(le.absoluteBoxes, box)
	}

	// Multi-column layout: triggered by column-count or column-width on block elements
	if (display == css.DisplayBlock || display == css.DisplayFlowRoot || display == css.DisplayInlineBlock) &&
		(style.GetColumnCount() > 0 || style.GetColumnWidth() > 0) {
		le.layoutMulticolumn(box, x, y, availableWidth, style, computedStyles)
		if position == css.PositionAbsolute || position == css.PositionFixed {
			oldX, oldY := box.X, box.Y
			le.applyAbsolutePositioning(box)
			if dx, dy := box.X-oldX, box.Y-oldY; dx != 0 || dy != 0 {
				le.shiftChildren(box, dx, dy)
			}
		}
		return box
	}

	// Phase 9: Handle table layout specially
	if display == css.DisplayTable {
		le.layoutTable(box, x, y, availableWidth, computedStyles)
		if position == css.PositionAbsolute || position == css.PositionFixed {
			oldX, oldY := box.X, box.Y
			le.applyAbsolutePositioning(box)
			if dx, dy := box.X-oldX, box.Y-oldY; dx != 0 || dy != 0 {
				le.shiftChildren(box, dx, dy)
			}
		}

		// CSS Writing Modes: Apply vertical transform to tables with vertical writing mode.
		// In vertical-rl/lr, caption-side:top/bottom maps to block-start/end (right/left in
		// vertical-rl, left/right in vertical-lr). The horizontal table layout places captions
		// above/below; transformToVerticalRL converts this Y-stacking to column-based X-stacking.
		// Each child (caption/cell) first gets its own vertical transform (converting horizontal
		// inline text to vertical columns), then the table gets its own transform (rearranging
		// caption and cell groups into columns based on their Y positions).
		if style != nil && len(box.Children) > 0 {
			if wm, ok := style.Get("writing-mode"); ok {
				isVertical := wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr"
				if isVertical {
					parentIsVertical := false
					if node.Parent != nil {
						if parentStyle := computedStyles[node.Parent]; parentStyle != nil {
							if parentWM, pok := parentStyle.Get("writing-mode"); pok {
								parentIsVertical = parentWM == "vertical-rl" || parentWM == "vertical-lr" || parentWM == "sideways-rl" || parentWM == "sideways-lr"
							}
						}
					}
					if !parentIsVertical {
						// Transform each child's inline content to vertical, then
						// transform the table box to rearrange children as columns.
						for _, child := range box.Children {
							transformBoxToVerticalRecursive(child, wm)
						}
						preTransformWidth := box.Width
						preTransformHeight := box.Height
						transformToVerticalRL(box, wm)
						repositionAbsPosAfterVerticalTransform(box, wm, preTransformWidth, preTransformHeight)
					}
				}
			}
		}

		return box
	}

	// Phase 10: Handle flexbox layout specially
	if display == css.DisplayFlex || display == css.DisplayInlineFlex {
		// When min-height establishes the flex container's initial height (no explicit CSS height),
		// mark it as having a definite height so that percentage-height children can resolve
		// against box.Height. Per CSS Flexbox §9.8 / definite-sizes spec.
		if hasMinHeight && !hasExplicitHeight {
			box.HeightIsDefinite = true
		}
		le.layoutFlex(box, x, y, availableWidth, computedStyles)
		if position == css.PositionAbsolute || position == css.PositionFixed {
			oldX, oldY := box.X, box.Y
			le.applyAbsolutePositioning(box)
			if dx, dy := box.X-oldX, box.Y-oldY; dx != 0 || dy != 0 {
				le.shiftChildren(box, dx, dy)
			}
		}
		return box
	}

	// Phase 15: Handle grid layout specially
	if display == css.DisplayGrid || display == css.DisplayInlineGrid {
		// Pass the established height (from explicit CSS height, aspect-ratio, etc.)
		// so that the grid can use it for row track sizing even without grid-template-rows.
		gridEstablishedHeight := 0.0
		if hasExplicitHeight {
			gridEstablishedHeight = contentHeight
		}
		box = le.layoutGridContainer(node, x, y, availableWidth, gridEstablishedHeight, style, computedStyles, parent)
		if position == css.PositionAbsolute || position == css.PositionFixed {
			oldX, oldY := box.X, box.Y
			le.applyAbsolutePositioning(box)
			if dx, dy := box.X-oldX, box.Y-oldY; dx != 0 || dy != 0 {
				le.shiftChildren(box, dx, dy)
			}
		}
		return box
	}

	// SVG elements are replaced elements — skip normal child layout.
	// Children (<rect>, <mask>, etc.) are drawn by the renderer, not the HTML layout engine.
	if isSVG {
		return box
	}

	// Check if this element creates a new block formatting context (BFC)
	createsBFC := false
	if style.GetOverflow() != css.OverflowVisible || floatType != css.FloatNone ||
		position == css.PositionAbsolute || position == css.PositionFixed ||
		display == css.DisplayInlineBlock || display == css.DisplayFlowRoot {
		createsBFC = true
	}
	// contain: layout, content, or strict also creates a BFC
	if !createsBFC {
		contain := style.GetContain()
		if strings.Contains(contain, "layout") || contain == "strict" || contain == "content" {
			createsBFC = true
		}
	}
	if createsBFC {
		le.floatBaseStack = append(le.floatBaseStack, le.floatBase)
		le.floatBase = len(le.floats)
	}

	// CSS Counter support: Process counter-reset on this element
	var counterResets map[string]int
	if resetVal, ok := style.Get("counter-reset"); ok {
		counterResets = parseCounterReset(resetVal)
		for name, value := range counterResets {
			le.counterReset(name, value)
		}
	}

	// Phase 2: Recursively layout children
	// Use box.X/Y which include relative positioning offset
	childY := box.Y + border.Top + padding.Top
	childAvailableWidth := contentWidth

	// For shrink-to-fit elements (floats, abs pos without explicit width), pass the parent's
	// available width to children so they can lay out naturally, then we'll shrink-wrap around them.
	// Use !hasExplicitWidth (not contentWidth==0) because min-width may have set a non-zero
	// contentWidth, but the element still needs shrink-to-fit behavior (floats inside can
	// exceed min-width and the container grows to accommodate them).
	if !hasExplicitWidth && (floatType != css.FloatNone || position == css.PositionAbsolute || position == css.PositionFixed) {
		childAvailableWidth = availableWidth - dir.InlineBorderBox(padding, border)
		// CSS 2.1 §10.3.5: max inline size constrains the available width for child layout.
		// Without this, floats inside a shrink-to-fit container with max-width would be
		// positioned as if the container had unlimited width, then the container would
		// shrink to max-width but the float positions would already be set incorrectly.
		if maxW, ok := style.GetLength(dir.MaxInlineSizeProp()); ok && childAvailableWidth > maxW {
			childAvailableWidth = maxW
		}
	}

	// Track previous block child for margin collapsing between siblings
	var prevBlockChild *Box
	var pendingMargins []float64 // margins from collapse-through elements

	// Phase 7: Determine which inline layout algorithm to use
	// Use multi-pass only for pure inline formatting contexts (no block children)
	algorithm := InlineLayoutSinglePass

	// Check if we should use multi-pass (only for containers without pseudo-elements)
	// hasPseudo := le.hasPseudoElements(node, computedStyles) // REMOVED: Allow multi-pass with pseudo-elements
	hasInlineChild := false
	didAnalyzeChildren := false // Track if we analyzed children

	if (display == css.DisplayBlock || display == css.DisplayFlowRoot || display == css.DisplayInline || display == css.DisplayInlineBlock || display == css.DisplayTableCell || display == css.DisplayTableCaption) {
		didAnalyzeChildren = true
		// Two-pass scan: determine if whitespace text counts as inline content.
		// CSS 2.1 §9.2.2.1: In block containers with only block children,
		// whitespace-only text between block elements doesn't generate boxes.
		// But in inline formatting contexts, whitespace creates word-spacing.
		hasBlockChild := false
		hasNonWhitespaceInline := false
		hasWhitespaceText := false

		// Expand display:contents children for accurate inline/block detection
		for _, child := range le.flattenContentsChildren(node, computedStyles) {
			if child.Type == html.ElementNode {
				childStyle := computedStyles[child]
				if childStyle == nil {
					// Check synthetic styles for normalization-created nodes (_anon blocks, clones)
					childStyle = le.syntheticStyles[child]
				}
				if childStyle != nil {
					childDisplay := childStyle.GetDisplay()
					childPos := childStyle.GetPosition()
					// Skip out-of-flow elements for this determination
					if childPos == css.PositionAbsolute || childPos == css.PositionFixed {
						continue
					}
					if childStyle.GetFloat() != css.FloatNone {
						continue
					}
					if childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock {
						hasNonWhitespaceInline = true
					} else {
						hasBlockChild = true
					}
				}
			} else if child.Type == html.TextNode {
				if strings.TrimSpace(child.Text) != "" {
					hasNonWhitespaceInline = true
				} else if child.Text != "" {
					hasWhitespaceText = true
				}
			}
		}

		// Whitespace counts as inline ONLY when no block children present
		hasInlineChild = hasNonWhitespaceInline || (hasWhitespaceText && !hasBlockChild)

		// Use multi-pass for ALL inline formatting contexts (per user request)
		// Requirements:
		// 1. Has some inline content (block children handled as InlineItemBlockChild)
		// 2. Not an object with image
		// 3. Container is a BLOCK (not inline - inline containers have complex fragment splitting)
		// EXPERIMENTAL: Allow mixed block/inline content - block children handled in multi-pass
		if hasInlineChild && !isObjectImage && (display == css.DisplayBlock || display == css.DisplayFlowRoot || display == css.DisplayInlineBlock || display == css.DisplayTableCell || display == css.DisplayTableCaption) {
			algorithm = InlineLayoutMultiPass
		}
	}

	// NEW ARCHITECTURE: If multi-pass is enabled AND we have a pure inline formatting context,
	// use clean LayoutInlineContentToBoxes. Multi-pass can only handle inline content, not mixed block/inline.
	var childBoxes []*Box
	var inlineLayoutResult *InlineLayoutResult

	// Check if we can use multi-pass (we analyzed children)
	// Block children are now supported via recursive layoutNode calls
	canUseMultiPass := le.useMultiPass && didAnalyzeChildren

	if canUseMultiPass {
		// Create synthetic nodes for pseudo-elements so they go through the same
		// multi-pass pipeline as real elements (identical sizing and positioning)
		overrideStyles := make(map[*html.Node]*css.Style)
		// Expand display:contents children before building the extended children list
		flatChildren := le.flattenContentsChildren(node, computedStyles)
		extendedChildren := make([]*html.Node, 0, len(flatChildren)+2)

		// ::before pseudo-element -> synthetic node
		beforeNode, beforeStyle := le.createPseudoElementNode(node, "before", computedStyles)
		if beforeNode != nil {
			overrideStyles[beforeNode] = beforeStyle
			// Also add override styles for synthetic children (img nodes)
			for _, child := range beforeNode.Children {
				if child.Type == html.ElementNode && child.TagName == "img" {
					imgStyle := css.NewStyle()
					imgStyle.Set("display", "inline-block")
					overrideStyles[child] = imgStyle
				}
			}
			extendedChildren = append(extendedChildren, beforeNode)
		}

		// Real children (display:contents already expanded)
		extendedChildren = append(extendedChildren, flatChildren...)

		// ::after pseudo-element -> synthetic node
		afterNode, afterStyle := le.createPseudoElementNode(node, "after", computedStyles)
		if afterNode != nil {
			overrideStyles[afterNode] = afterStyle
			// Also add override styles for synthetic children (img nodes)
			for _, child := range afterNode.Children {
				if child.Type == html.ElementNode && child.TagName == "img" {
					imgStyle := css.NewStyle()
					imgStyle.Set("display", "inline-block")
					overrideStyles[child] = imgStyle
				}
			}
			extendedChildren = append(extendedChildren, afterNode)
		}

		// Use new three-phase multi-pass pipeline with extended children
		inlineLayoutResult = le.LayoutInlineContentToBoxes(
			extendedChildren,
			box,
			childAvailableWidth,
			childY,
			computedStyles,
			overrideStyles,
		)
		childBoxes = inlineLayoutResult.ChildBoxes

		// CRITICAL FIX: Apply margin collapsing between adjacent block siblings
		// LayoutInlineContentToBoxes doesn't handle margin collapsing, so we must do it here.
		// Adjustments are cumulative: when box N is moved up, all subsequent boxes must also
		// be moved up by the same amount (since their positions were computed relative to N's
		// pre-collapsing position).
		var prevBox *Box
		cumulativeAdjustment := 0.0
		var mpPendingMargins []float64
		for _, childBox := range childBoxes {
			if childBox == nil {
				continue
			}

			// Only collapse margins for block-level boxes in normal flow
			floatType := css.FloatNone
			if childBox.Style != nil {
				floatType = childBox.Style.GetFloat()
			}

			if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && floatType == css.FloatNone {
				// Apply cumulative adjustment from previous collapses
				if cumulativeAdjustment != 0 {
					childBox.Y -= cumulativeAdjustment
					le.adjustChildrenY(childBox, -cumulativeAdjustment)
				}

				// CSS 2.1 §8.3.1: Collapse-through — empty elements with no height,
				// border, or padding have their top and bottom margins collapse through.
				// These margins then participate in collapsing with adjacent siblings.
				if isCollapseThrough(childBox) {
					mpPendingMargins = append(mpPendingMargins, childBox.Margin.Top, childBox.Margin.Bottom)
					collectCollapseThroughChildMargins(childBox, &mpPendingMargins)
					// Remove the space this element consumed in layout
					cumulativeAdjustment += childBox.Margin.Top + childBox.Margin.Bottom
					// LayoutInlineContentToBoxes places collapse-through elements at
					// Y = prevBlock.bottom + margin-top. Remove the margin-top from Y so
					// the element sits at prevBlock.bottom. This matches the singlepass
					// behavior and ensures the auto-height calculation sees the correct
					// position: margin-bottom (not margin-top+margin-bottom) as contribution.
					if childBox.Margin.Top != 0 {
						childBox.Y -= childBox.Margin.Top
						le.adjustChildrenY(childBox, -childBox.Margin.Top)
					}
					continue
				}

				// Check if both boxes should collapse margins
				if prevBox != nil && shouldCollapseMargins(prevBox) && shouldCollapseMargins(childBox) {
					// Collect all margins: prev bottom, pending from collapse-through, current top
					allMargins := []float64{prevBox.Margin.Bottom}
					allMargins = append(allMargins, mpPendingMargins...)
					allMargins = append(allMargins, childBox.Margin.Top)
					var maxPos, minNeg float64
					for _, m := range allMargins {
						if m > maxPos {
							maxPos = m
						}
						if m < minNeg {
							minNeg = m
						}
					}
					collapsed := maxPos + minNeg
					totalUsed := prevBox.Margin.Bottom + childBox.Margin.Top
					adjustment := totalUsed - collapsed

					childBox.Y -= adjustment
					le.adjustChildrenY(childBox, -adjustment)
					cumulativeAdjustment += adjustment
				} else if len(mpPendingMargins) > 0 && shouldCollapseMargins(childBox) {
					// No prev sibling but pending margins from collapse-through
					allMargins := append(mpPendingMargins, childBox.Margin.Top)
					var maxPos, minNeg float64
					for _, m := range allMargins {
						if m > maxPos {
							maxPos = m
						}
						if m < minNeg {
							minNeg = m
						}
					}
					collapsed := maxPos + minNeg
					totalUsed := childBox.Margin.Top
					adjustment := totalUsed - collapsed
					childBox.Y -= adjustment
					le.adjustChildrenY(childBox, -adjustment)
					cumulativeAdjustment += adjustment
				}
				mpPendingMargins = nil
				prevBox = childBox
			}
		}

		// Add all child boxes to the container
		box.Children = append(box.Children, childBoxes...)

		// Lay out absolutely positioned children that were skipped by inline layout.
		// The multi-pass pipeline creates new text nodes for word fragments, so
		// we can't match fragment boxes to original DOM children by pointer.
		// Instead, use the fragment boxes' positions directly: the last inline
		// fragment tells us where the inline cursor is.

		// Build a set of element nodes that appear before each abs-pos child.
		// For element children, we can match by node pointer in childBoxes.
		// Multi-pass creates new nodes for text fragments, but block-level
		// element children retain their original html.Node pointers.
		nodeToBox := make(map[*html.Node]*Box)
		for _, cb := range childBoxes {
			if cb != nil && cb.Node != nil {
				nodeToBox[cb.Node] = cb
			}
		}

		hasAbspos := false
		for i, child := range node.Children {
			if child.Type != html.ElementNode {
				continue
			}
			childStyle := computedStyles[child]
			if childStyle == nil {
				continue
			}
			childPos := childStyle.GetPosition()
			if childPos == css.PositionAbsolute || childPos == css.PositionFixed {
				// Compute static position.
				// CSS 2.1 §10.3.7: static position is at the current inline cursor
				// after all preceding inline content, or below the last block sibling.
				staticX := box.X + box.Padding.Left + box.Border.Left
				staticY := childY // default: top of content area

				// Strategy 1: Check preceding element siblings via nodeToBox.
				// This works for block-level element children (multi-pass preserves
				// their node pointers).
				foundPrev := false
				for j := i - 1; j >= 0; j-- {
					prevChild := node.Children[j]
					if prevBox, ok := nodeToBox[prevChild]; ok {
						flowY := prevBox.Y
						if prevBox.Position == css.PositionRelative && prevBox.Style != nil {
							offset := prevBox.Style.GetPositionOffset()
							if offset.HasTop {
								flowY -= offset.Top
							} else if offset.HasBottom {
								flowY += offset.Bottom
							}
						}
						// Check if it's a block-level element
						isBlock := false
						if prevBox.Style != nil {
							d := prevBox.Style.GetDisplay()
							isBlock = d == css.DisplayBlock || d == css.DisplayFlex || d == css.DisplayGrid ||
								d == css.DisplayTable || d == css.DisplayFlowRoot || d == css.DisplayListItem
						}
						if isBlock {
							staticY = flowY + prevBox.Height + prevBox.Margin.Bottom
						} else {
							staticX = prevBox.X + prevBox.Width
							staticY = prevBox.Y
						}
						foundPrev = true
						break
					}
				}

				// Strategy 2: If no preceding element sibling found via nodeToBox
				// (e.g. only text precedes the abs-pos element), scan childBoxes
				// for the last inline fragment. Multi-pass creates new text nodes
				// so pointer matching fails, but the fragments are in document
				// order and all precede the abs-pos element (which was skipped).
				if !foundPrev && len(childBoxes) > 0 {
					for j := len(childBoxes) - 1; j >= 0; j-- {
						cb := childBoxes[j]
						if cb == nil || (cb.Width == 0 && cb.Height == 0) {
							continue
						}
						// Check if this box belongs to a child AFTER the abs-pos
						// element. If so, skip it.
						if cb.Node != nil && cb.Node.Type == html.ElementNode {
							afterAbspos := false
							for k := i + 1; k < len(node.Children); k++ {
								if node.Children[k] == cb.Node {
									afterAbspos = true
									break
								}
							}
							if afterAbspos {
								continue
							}
						}
						// Text fragment or element before abs-pos: use its position
						isBlock := false
						if cb.Node != nil && cb.Node.Type == html.ElementNode && cb.Style != nil {
							d := cb.Style.GetDisplay()
							isBlock = d == css.DisplayBlock || d == css.DisplayFlex || d == css.DisplayGrid ||
								d == css.DisplayTable || d == css.DisplayFlowRoot || d == css.DisplayListItem
						}
						if isBlock {
							flowY := cb.Y
							if cb.Position == css.PositionRelative && cb.Style != nil {
								offset := cb.Style.GetPositionOffset()
								if offset.HasTop {
									flowY -= offset.Top
								} else if offset.HasBottom {
									flowY += offset.Bottom
								}
							}
							staticY = flowY + cb.Height + cb.Margin.Bottom
						} else {
							staticX = cb.X + cb.Width
							staticY = cb.Y
						}
						break
					}
				}

				childBox := le.layoutNodeHTB(child, staticX, staticY, childAvailableWidth, computedStyles, box)
				if childBox != nil {
					box.Children = append(box.Children, childBox)
					hasAbspos = true
				}
			}
		}

		// If we added abspos children, re-sort all children by document order.
		// Abspos children were appended at the end but CSS paint order requires
		// document order within the same z-index level.
		if hasAbspos {
			nodeOrder := make(map[*html.Node]int)
			for i, child := range node.Children {
				nodeOrder[child] = i
			}
			sort.SliceStable(box.Children, func(i, j int) bool {
				ni := box.Children[i].Node
				nj := box.Children[j].Node
				oi, oki := nodeOrder[ni]
				oj, okj := nodeOrder[nj]
				if oki && okj {
					return oi < oj
				}
				return false // preserve existing order for non-mapped children
			})
		}
	} else {
		// Use existing layout code
		// Layout inline children using detected algorithm
		// This handles ::before, child loop, ::after, and text-align
		inlineLayoutResult = le.layoutInlineChildren(
			node, box, display, style, border, padding, x, childY,
			childAvailableWidth, contentWidth, isObjectImage, computedStyles,
			&prevBlockChild, &pendingMargins, algorithm,
		)

		// Add all child boxes to the container
		box.Children = append(box.Children, inlineLayoutResult.ChildBoxes...)
		childBoxes = inlineLayoutResult.ChildBoxes
	}

	// NOTE: The rest of the old inline layout code (lines 700-1212) has been
	// extracted into layoutInlineChildrenSinglePass() and is now called above.
	// The following line preserves inline context for any code that might use it later.
	var inlineCtx *InlineContext
	if inlineLayoutResult != nil {
		inlineCtx = inlineLayoutResult.FinalInlineCtx
	}
	// Note: Both single-pass and multi-pass now provide inline context for height calculation

	// TEMPORARY: Keep the old inline layout code below commented out for reference
	// until we verify the refactor works correctly. Will be deleted once stable.
	/*
	// Phase 11: Generate ::before pseudo-element if it has content
	beforeBox := le.generatePseudoElement(node, "before", inlineCtx.LineX, inlineCtx.LineY, childAvailableWidth, computedStyles, box)
	if beforeBox != nil {
		beforeFloat := beforeBox.Style.GetFloat()
		if beforeFloat != css.FloatNone {
			// Position floated ::before pseudo-element
			floatWidth := le.getTotalWidth(beforeBox)
			// Pseudo-element floats position inline at current LineY, allowing overflow
			// rather than dropping to a new line like block-level floats
			floatY := inlineCtx.LineY
			leftOffset, rightOffset := le.getFloatOffsets(floatY)
			// Calculate new position
			var newX float64
			if beforeFloat == css.FloatLeft {
				// For left floats, position must clear both other floats (leftOffset) AND inline content (LineX)
				baseX := box.X + border.Left + padding.Left
				floatClearX := baseX + leftOffset + beforeBox.Margin.Left
				inlineEndX := inlineCtx.LineX + beforeBox.Margin.Left
				if inlineEndX > floatClearX {
					newX = inlineEndX
				} else {
					newX = floatClearX
				}
			} else {
				newX = box.X + border.Left + padding.Left + childAvailableWidth - rightOffset - floatWidth + beforeBox.Margin.Left
			}
			newY := floatY + beforeBox.Margin.Top

			// Calculate position delta to reposition children
			deltaX := newX - beforeBox.X
			deltaY := newY - beforeBox.Y

			// Reposition child boxes (e.g., images inside the pseudo-element)
			for _, child := range beforeBox.Children {
				child.X += deltaX
				child.Y += deltaY
			}

			beforeBox.X = newX
			beforeBox.Y = newY
			le.addFloat(beforeBox, beforeFloat, floatY)
			box.Children = append(box.Children, beforeBox)
		} else {
			box.Children = append(box.Children, beforeBox)
			// Update inline context for subsequent children
			beforeDisplay := beforeBox.Style.GetDisplay()
			if beforeDisplay == css.DisplayBlock || beforeDisplay == css.DisplayFlowRoot {
				inlineCtx.LineY += le.getTotalHeight(beforeBox)
				inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
			} else {
				inlineCtx.LineX += le.getTotalWidth(beforeBox)
				if beforeBox.Height > inlineCtx.LineHeight {
					inlineCtx.LineHeight = beforeBox.Height
				}
			}
		}
	}

	// Phase 23: Generate list marker for list-item elements
	if display == css.DisplayListItem {
		markerBox := le.generateListMarker(node, style, x, inlineCtx.LineY, box)
		if markerBox != nil {
			box.Children = append(box.Children, markerBox)
		}
	}

	// Phase 24: Skip children for object elements that successfully loaded an image
	skipChildren := isObjectImage

	// Track block-in-inline for fragment splitting (CSS 2.1 §9.2.1.1)
	// When a block element is inside an inline element, the inline's borders are split
	isInlineParent := display == css.DisplayInline
	hasSeenBlockChild := false
	hasInlineContentBeforeBlock := false

	// Fragment tracking for block-in-inline
	// We track the bounding region of inline content to create fragments
	type fragmentRegion struct {
		startX, startY float64
		maxX, maxY     float64
		hasContent     bool
	}
	currentFragment := fragmentRegion{
		startX: box.X + border.Left + padding.Left,
		startY: box.Y + border.Top + padding.Top,
	}
	var completedFragments []fragmentRegion

	for _, child := range node.Children {
		if skipChildren {
			break
		}
		if child.Type == html.ElementNode {
			// Get child's computed style to check display mode
			childStyle := computedStyles[child]
			if childStyle == nil {
				childStyle = css.NewStyle()
			}
			childDisplay := childStyle.GetDisplay()

			// Determine initial X coordinate for child
			// For inline/inline-block elements, use LineX (accumulates horizontally)
			// For block elements and floats, use parent content area left edge
			childX := inlineCtx.LineX
			childFloat := childStyle.GetFloat()
			if childDisplay == css.DisplayBlock || childDisplay == css.DisplayFlowRoot ||
			   childDisplay == css.DisplayTable || childDisplay == css.DisplayListItem ||
			   childDisplay == css.DisplayFlex || childDisplay == css.DisplayGrid ||
			   childFloat != css.FloatNone {
				// Block-level or floated: start from parent's left content edge
				childX = box.X + border.Left + padding.Left
			}

			// CSS 2.1 §9.4.1: A block that establishes a new BFC must not overlap float margin boxes.
			// Adjust X and available width for BFC-establishing block children.
			adjustedChildX := childX
			adjustedChildWidth := childAvailableWidth
			if childFloat == css.FloatNone &&
				childStyle.GetPosition() != css.PositionAbsolute &&
				childStyle.GetPosition() != css.PositionFixed &&
				blockChildEstablishesBFC(childStyle) {
				leftOff, rightOff := le.getFloatOffsets(inlineCtx.LineY)
				if leftOff > 0 || rightOff > 0 {
					// Use the absolute right edge of left floats to avoid double-counting when
					// the querying block's content-left differs from the float's container left.
					absLeftEdge := le.getLeftFloatAbsoluteEdge(inlineCtx.LineY)
					if absLeftEdge > childX {
						adjustedChildX = absLeftEdge
					}
					adjustedChildWidth = childAvailableWidth - leftOff - rightOff
					if adjustedChildWidth < 0 {
						adjustedChildWidth = 0
					}
				}
			}

			// Layout the child
			childBox := le.layoutNodeHTB(
				child,
				adjustedChildX,
				inlineCtx.LineY,
				adjustedChildWidth,
				computedStyles,
				box, // Phase 4: Pass parent
			)

			// Phase 7: Skip elements with display: none (layoutNode returns nil)
			if childBox != nil {
				// Handle <br> elements - force a line break
				if child.TagName == "br" {
					// Move to next line
					if inlineCtx.LineHeight == 0 {
						inlineCtx.LineHeight = style.GetLineHeight()
					}
					inlineCtx.LineY += inlineCtx.LineHeight
					inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
					inlineCtx.LineHeight = 0
					inlineCtx.LineBoxes = make([]*Box, 0)
					// Don't add <br> to children - it's just a control element
					continue
				}

				// Phase 7: Handle inline and inline-block elements
				// Skip inline positioning for floated elements (they are positioned by float logic)
				childIsFloated := childStyle != nil && childStyle.GetFloat() != css.FloatNone
				if (childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock) && childBox.Position == css.PositionStatic && !childIsFloated {
					// Block-in-inline: mark inline content after a block as last fragment
					if isInlineParent && hasSeenBlockChild {
						childBox.IsLastFragment = true
					}
					if isInlineParent && !hasSeenBlockChild {
						hasInlineContentBeforeBlock = true
					}

					// Update fragment region with this inline child's bounds
					if isInlineParent {
						childRight := childBox.X + le.getTotalWidth(childBox)
						childBottom := childBox.Y + le.getTotalHeight(childBox)
						if childRight > currentFragment.maxX {
							currentFragment.maxX = childRight
						}
						if childBottom > currentFragment.maxY {
							currentFragment.maxY = childBottom
						}
						currentFragment.hasContent = true
					}

					childTotalWidth := le.getTotalWidth(childBox)

					// Check if child fits on current line (skip wrapping if white-space: nowrap)
					allowWrap := style.GetWhiteSpace() != css.WhiteSpaceNowrap
					if allowWrap && inlineCtx.LineX+childTotalWidth > box.X+border.Left+padding.Left+childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to next line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
						inlineCtx.LineHeight = 0
						inlineCtx.LineBoxes = make([]*Box, 0)

						// Reposition child at start of new line
						childBox.X = inlineCtx.LineX
						childBox.Y = inlineCtx.LineY
					} else {
						// Fits on current line - position it at the current LineX
						childBox.X = inlineCtx.LineX
						childBox.Y = inlineCtx.LineY
					}

					// Add to current line
					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, childBox)
					childHeight := le.getTotalHeight(childBox)
					if childHeight > inlineCtx.LineHeight {
						inlineCtx.LineHeight = childHeight
					}
					// CSS 2.1 §10.8.1: The "strut" ensures line box height is at least
					// the block container's line-height
					strutHeight := style.GetLineHeight()
					if strutHeight > inlineCtx.LineHeight {
						inlineCtx.LineHeight = strutHeight
					}

					// Advance X for next inline-block element
					inlineCtx.LineX += childTotalWidth

					box.Children = append(box.Children, childBox)

					// Phase 7 Enhancement: Apply vertical-align to inline element
					le.applyVerticalAlign(childBox, inlineCtx.LineY, inlineCtx.LineHeight)
				} else {
					// Block element or other display mode
					// Block-in-inline: when a block is inside an inline parent, mark fragments
					if isInlineParent && hasInlineContentBeforeBlock {
						// Complete the current fragment (content before the block)
						if currentFragment.hasContent {
							completedFragments = append(completedFragments, currentFragment)
						}
						// Start a new fragment for content after the block
						// (will be positioned after block layout is done)
						hasSeenBlockChild = true
						// Mark legacy flags for backward compatibility
						box.IsFirstFragment = true
					}

					// Finish current inline line (apply strut for line box height)
					if len(inlineCtx.LineBoxes) > 0 {
						strutHeight := style.GetLineHeight()
						if strutHeight > inlineCtx.LineHeight {
							inlineCtx.LineHeight = strutHeight
						}
						childY = inlineCtx.LineY + inlineCtx.LineHeight
						inlineCtx.LineBoxes = make([]*Box, 0)
						inlineCtx.LineHeight = 0
					} else {
						childY = inlineCtx.LineY
					}

					// Update child position for block element (skip absolute/fixed - positioned later, skip floats - positioned by float logic)
					childFloatTypePos := css.FloatNone
					if childStyle != nil {
						childFloatTypePos = childStyle.GetFloat()
					}
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatTypePos == css.FloatNone {
						// For position:relative, preserve the offset that was already applied
						relativeOffsetY := 0.0
						if childBox.Position == css.PositionRelative && childStyle != nil {
							offset := childStyle.GetPositionOffset()
							if offset.HasTop {
								relativeOffsetY = offset.Top
							} else if offset.HasBottom {
								relativeOffsetY = -offset.Bottom
							}
						}
						// Calculate new position.
						// Use adjustedChildX (not box.X+border+padding) so that BFC float
						// avoidance offsets (computed above) are preserved after repositioning.
						var newX float64
						if childBox.Margin.AutoLeft && childBox.Margin.AutoRight {
							childTotalW := childBox.Width + childBox.Padding.Left + childBox.Padding.Right + childBox.Border.Left + childBox.Border.Right
							centerOff := (adjustedChildWidth - childTotalW) / 2
							if centerOff < 0 {
								centerOff = 0
							}
							newX = adjustedChildX + centerOff
						} else {
							newX = adjustedChildX + childBox.Margin.Left
						}
						newY := childY + childBox.Margin.Top + relativeOffsetY

						// Shift children by the position delta (important for block-in-inline)
						dx := newX - childBox.X
						dy := newY - childBox.Y
						if dx != 0 || dy != 0 {
							le.shiftChildren(childBox, dx, dy)
						}
						childBox.X = newX
						childBox.Y = newY
					}

					box.Children = append(box.Children, childBox)

					// Advance Y for block elements
					childFloatType := childBox.Style.GetFloat()
					if childBox.Position != css.PositionAbsolute && childBox.Position != css.PositionFixed && childFloatType == css.FloatNone {
						// Margin-collapse-through: collect margins from collapse-through elements
						// and combine them with the next non-collapse-through sibling's margins.
						if isCollapseThrough(childBox) {
							// Add this element's margins (and children's) to pending list
							pendingMargins = append(pendingMargins, childBox.Margin.Top, childBox.Margin.Bottom)
							collectCollapseThroughChildMargins(childBox, &pendingMargins)
							// Position at childY (zero-height, no visual impact)
							childBox.Y = childY
							// Don't advance childY, don't set prevBlockChild
						} else {
							// Normal margin collapsing between adjacent block siblings
							if prevBlockChild != nil && shouldCollapseMargins(prevBlockChild) && shouldCollapseMargins(childBox) {
								// Collect all margins: prev bottom, any pending from collapse-through, current top
								allMargins := []float64{prevBlockChild.Margin.Bottom}
								allMargins = append(allMargins, pendingMargins...)
								allMargins = append(allMargins, childBox.Margin.Top)
								// Collapse all together
								var maxPos, minNeg float64
								for _, m := range allMargins {
									if m > maxPos {
										maxPos = m
									}
									if m < minNeg {
										minNeg = m
									}
								}
								collapsed := maxPos + minNeg
								// Only real margins used space; pending margins were from zero-height elements
								totalUsed := prevBlockChild.Margin.Bottom + childBox.Margin.Top
								adjustment := totalUsed - collapsed
								childBox.Y -= adjustment
								le.adjustChildrenY(childBox, -adjustment)
							} else if len(pendingMargins) > 0 && shouldCollapseMargins(childBox) {
								// No prev sibling but pending margins from collapse-through
								allMargins := append(pendingMargins, childBox.Margin.Top)
								var maxPos, minNeg float64
								for _, m := range allMargins {
									if m > maxPos {
										maxPos = m
									}
									if m < minNeg {
										minNeg = m
									}
								}
								collapsed := maxPos + minNeg
								totalUsed := childBox.Margin.Top
								adjustment := totalUsed - collapsed
								childBox.Y -= adjustment
								le.adjustChildrenY(childBox, -adjustment)
							}
							pendingMargins = nil
							// Apply clear property after margin collapsing
							if childBox.Style != nil {
								childClear := childBox.Style.GetClear()
								if childClear != css.ClearNone {
									clearY := le.getClearY(childClear, childBox.Y)
									if clearY > childBox.Y {
										delta := clearY - childBox.Y
										childBox.Y = clearY
										le.adjustChildrenY(childBox, delta)
									}
								}
							}
							// childBox.Height is border-box (content+padding+border), so just add margin.
							childY = childBox.Y + childBox.Height + childBox.Margin.Bottom
							prevBlockChild = childBox
						}
					}

					// Reset inline context for next line
					inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
					inlineCtx.LineY = childY

					// Reset fragment tracking for next fragment (content after this block)
					if isInlineParent {
						currentFragment = fragmentRegion{
							startX: inlineCtx.LineX,
							startY: inlineCtx.LineY,
						}
					}
				}
			}
		} else if child.Type == html.TextNode {
			// Phase 6: Layout text nodes
			// Always use inline flow so text nodes participate in the inline
			// formatting context together with sibling inline elements (e.g. <em>).
			// layoutTextNode already handles float offsets internally, so pass the
			// original position and let it adjust for floats
			// Ensure LineX accounts for any floats that were added (e.g., floated ::before)
			le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
			textBox := le.layoutTextNode(
				child,
				inlineCtx.LineX,
				inlineCtx.LineY,
				box.X+border.Left+padding.Left+childAvailableWidth-inlineCtx.LineX,
				style, // Text inherits parent's style
				box,
			)
			if textBox != nil {
				// Block-in-inline: track and mark text fragments
				if isInlineParent {
					if hasSeenBlockChild {
						textBox.IsLastFragment = true
					} else {
						hasInlineContentBeforeBlock = true
					}
					// Update fragment region with this text's bounds
					textRight := textBox.X + le.getTotalWidth(textBox)
					textBottom := textBox.Y + le.getTotalHeight(textBox)
					if textRight > currentFragment.maxX {
						currentFragment.maxX = textRight
					}
					if textBottom > currentFragment.maxY {
						currentFragment.maxY = textBottom
					}
					currentFragment.hasContent = true
				}
				box.Children = append(box.Children, textBox)

				// For multi-line text containers, the inline context should
				// continue after the LAST line, not after the full container width.
				if len(textBox.Children) > 0 {
					// Multi-line text: advance to end of last line
					lastLine := textBox.Children[len(textBox.Children)-1]
					inlineCtx.LineY = lastLine.Y
					inlineCtx.LineX = lastLine.X + le.getTotalWidth(lastLine)
					inlineCtx.LineHeight = le.getTotalHeight(lastLine)
					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, textBox)
				} else {
					// Single-line text
					textWidth := le.getTotalWidth(textBox)
					textHeight := le.getTotalHeight(textBox)

					// Check if text fits on current line (skip wrapping if white-space: nowrap)
					allowWrap := style.GetWhiteSpace() != css.WhiteSpaceNowrap
					if allowWrap && inlineCtx.LineX+textWidth > box.X+border.Left+padding.Left+childAvailableWidth && len(inlineCtx.LineBoxes) > 0 {
						// Wrap to new line
						inlineCtx.LineY += inlineCtx.LineHeight
						inlineCtx.LineX = le.initializeLineX(box, border, padding, inlineCtx.LineY)
						inlineCtx.LineHeight = textHeight
						textBox.X = inlineCtx.LineX
						textBox.Y = inlineCtx.LineY
						inlineCtx.LineX += textWidth
						le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
					} else {
						// Fits on current line (or is the first item on the line)
						inlineCtx.LineX += textWidth
						le.ensureLineXClearsFloats(inlineCtx, box, border, padding)
						if textHeight > inlineCtx.LineHeight {
							inlineCtx.LineHeight = textHeight
						}
					}

					inlineCtx.LineBoxes = append(inlineCtx.LineBoxes, textBox)
				}
			}
		}
	}

	// Phase 11: Generate ::after pseudo-element if it has content
	afterBox := le.generatePseudoElement(node, "after", inlineCtx.LineX, inlineCtx.LineY, childAvailableWidth, computedStyles, box)
	if afterBox != nil {
		afterFloat := afterBox.Style.GetFloat()
		if afterFloat != css.FloatNone {
			// Position floated ::after pseudo-element
			floatWidth := le.getTotalWidth(afterBox)
			// Pseudo-element floats position inline at current LineY, allowing overflow
			// rather than dropping to a new line like block-level floats
			floatY := inlineCtx.LineY
			leftOffset, rightOffset := le.getFloatOffsets(floatY)

			// Calculate new position
			var newX float64
			if afterFloat == css.FloatLeft {
				// For left floats, position must clear both other floats (leftOffset) AND inline content (LineX)
				baseX := box.X + border.Left + padding.Left
				floatClearX := baseX + leftOffset + afterBox.Margin.Left
				inlineEndX := inlineCtx.LineX + afterBox.Margin.Left
				if inlineEndX > floatClearX {
					newX = inlineEndX
				} else {
					newX = floatClearX
				}
			} else {
				newX = box.X + border.Left + padding.Left + childAvailableWidth - rightOffset - floatWidth + afterBox.Margin.Left
			}
			newY := floatY + afterBox.Margin.Top

			// Calculate position delta to reposition children
			deltaX := newX - afterBox.X
			deltaY := newY - afterBox.Y

			// Reposition child boxes (e.g., images inside the pseudo-element)
			for _, child := range afterBox.Children {
				child.X += deltaX
				child.Y += deltaY
			}

			afterBox.X = newX
			afterBox.Y = newY
			le.addFloat(afterBox, afterFloat, floatY)
		}
		box.Children = append(box.Children, afterBox)
	}

	// Finalize block-in-inline fragments
	// If we're an inline parent that was split by block children, create the fragment boxes
	if isInlineParent && hasSeenBlockChild {
		// Complete the final fragment (content after the last block)
		if currentFragment.hasContent {
			completedFragments = append(completedFragments, currentFragment)
		}

		// Create BoxFragment objects for rendering
		for i, frag := range completedFragments {
			if !frag.hasContent {
				continue
			}

			// Determine which borders this fragment should have
			borders := AllBorders()
			if i == 0 {
				// First fragment: has left border, no right border
				borders.Right = false
			}
			if i == len(completedFragments)-1 {
				// Last fragment: has right border, no left border
				borders.Left = false
			}

			// Calculate fragment dimensions including padding/border
			fragWidth := frag.maxX - frag.startX + border.Left + border.Right + padding.Left + padding.Right
			fragHeight := frag.maxY - frag.startY + border.Top + border.Bottom + padding.Top + padding.Bottom

			box.AddFragment(
				frag.startX-border.Left-padding.Left,
				frag.startY-border.Top-padding.Top,
				fragWidth,
				fragHeight,
				borders,
			)
		}
	}

	// Apply text-align to inline children (only for block containers, not inline elements)
	if display != css.DisplayInline && display != css.DisplayInlineBlock {
		if textAlign, ok := style.Get("text-align"); ok && textAlign != "left" && textAlign != "" {
			le.applyTextAlign(box, textAlign, contentWidth)
		}
	}
	*/
	// END OF COMMENTED OLD INLINE LAYOUT CODE - will be removed once refactor is verified

	// Parent-child top margin collapsing
	// If parent has no border-top/padding-top, collapse with first block child's top margin
	if parentCanCollapseTopMargin(box) && shouldCollapseMargins(box) {
		// Find first in-flow block child
		var firstBlockChild *Box
		for _, ch := range box.Children {
			if ch.Style != nil && ch.Style.GetFloat() != css.FloatNone {
				continue
			}
			if ch.Position == css.PositionAbsolute || ch.Position == css.PositionFixed {
				continue
			}
			if ch.Style != nil {
				d := ch.Style.GetDisplay()
				if d == css.DisplayInline || d == css.DisplayInlineBlock {
					break // inline content separates margins
				}
			}
			firstBlockChild = ch
			break
		}
		if firstBlockChild != nil && shouldCollapseMargins(firstBlockChild) && firstBlockChild.Margin.Top > 0 {
			childMarginTop := firstBlockChild.Margin.Top
			// Pull all children up by the first child's top margin
			for _, ch := range box.Children {
				ch.Y -= childMarginTop
				le.adjustChildrenY(ch, -childMarginTop)
			}
			// Compute collapsed margin
			collapsed := collapseMargins(margin.Top, childMarginTop)
			marginDiff := collapsed - margin.Top
			box.Margin.Top = collapsed
			if marginDiff != 0 {
				box.Y += marginDiff
				for _, ch := range box.Children {
					ch.Y += marginDiff
					le.adjustChildrenY(ch, marginDiff)
				}
			}
		}
	}

	// If block-size is auto and we have children, adjust block-size to fit content
	if !hasExplicitHeight && len(box.Children) > 0 {
		// Calculate block-size based on maximum block-end edge of children (not sum)
		// This correctly handles overlapping children (like floats with blocks)
		parentContentTop := dir.ContentStartBlockPos(box)
		maxBottom := 0.0

		// CSS 2.1 §8.3.1 / §10.6.3: Parent-child block-end margin collapsing.
		// When parent has no block-end border and no block-end padding (and auto block-size),
		// the last in-flow child's block-end margin collapses with the parent's block-end
		// margin, so it should NOT be included in the auto block-size calculation.
		// Note: Margin collapsing does NOT apply to absolutely positioned elements,
		// which establish a new block formatting context (CSS 2.1 §9.4.1).
		parentChildBottomCollapse := parentCanCollapseBottomMargin(box) &&
			position != css.PositionAbsolute && position != css.PositionFixed
		var lastInFlowChild *Box
		if parentChildBottomCollapse {
			for _, child := range box.Children {
				if child.Position != css.PositionAbsolute && child.Position != css.PositionFixed {
					// CSS 2.1 §8.3.1: Parent-child block-end margin collapse only applies to
					// the last in-flow BLOCK-LEVEL child. Inline-block margins don't collapse.
					childDisplay := css.DisplayBlock
					if child.Style != nil {
						childDisplay = child.Style.GetDisplay()
					}
					if childDisplay == css.DisplayInline || childDisplay == css.DisplayInlineBlock {
						continue
					}
					lastInFlowChild = child
				}
			}
		}

		for _, child := range box.Children {
			if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
				continue
			}
			// Calculate child's block-end edge relative to parent content area
			// For position:relative children, use their normal flow position
			// (CSS 2.1 §10.6.3: relative offset doesn't affect parent block-size)
			childBlockPos := dir.BlockPos(child)
			if child.Position == css.PositionRelative && child.Style != nil {
				offset := child.Style.GetPositionOffset()
				// Undo physical block-direction offset to get normal flow position.
				// For h-tb block=Y (top/bottom), for v-rl/v-lr block=X (left/right).
				if !dir.IsVertical() {
					if offset.HasTop {
						childBlockPos -= offset.Top
					} else if offset.HasBottom {
						childBlockPos += offset.Bottom
					}
				} else {
					if offset.HasLeft {
						childBlockPos -= offset.Left
					} else if offset.HasRight {
						childBlockPos += offset.Right
					}
				}
			}
			childRelativePos := childBlockPos - parentContentTop
			// Use block-size from child's block-start edge downward:
			// border + padding + content + padding + border + block-end margin.
			// Don't include block-start margin since child's block position already accounts for it.
			childMarginBlockEnd := dir.BlockEndEdge(child.Margin)
			if parentChildBottomCollapse && child == lastInFlowChild {
				// Last child's block-end margin collapses through the parent
				childMarginBlockEnd = 0
			}
			// Box block-size is ALWAYS border-box (content + padding + borders).
			var childBlockSize float64
			if child.Style != nil && child.Style.GetDisplay() == css.DisplayInline {
				// IMPORTANT: For inline elements, use LINE BOX height (not wrapper box block-size)
				// CSS 2.1 §10.8.1: Borders/padding "bleed" outside line box, don't affect container block-size
				childBlockSize = 0  // Don't count inline wrapper box block-size twice
			} else {
				// Block: block-size is already border-box, just add block-end margin
				childBlockSize = dir.BlockSize(child) + childMarginBlockEnd
			}
			childBottom := childRelativePos + childBlockSize
			if childBottom > maxBottom {
				maxBottom = childBottom
			}
		}
		// CSS 2.1 §10.8.1: Account for trailing inline line box height (including strut)
		// Only count in-flow boxes — absolutely positioned/fixed elements don't generate line boxes
		hasInFlowLineBoxes := false
		if inlineCtx != nil {
			for _, lb := range inlineCtx.LineBoxes {
				if lb.Position != css.PositionAbsolute && lb.Position != css.PositionFixed {
					hasInFlowLineBoxes = true
					break
				}
			}
		}
		if hasInFlowLineBoxes {
			strutHeight := style.GetLineHeight()
			lineBoxHeight := inlineCtx.LineHeight
			if strutHeight > lineBoxHeight {
				lineBoxHeight = strutHeight
			}
			lineBottom := (inlineCtx.LineY - parentContentTop) + lineBoxHeight

			if lineBottom > maxBottom {
				maxBottom = lineBottom
			}
		}
		// CSS 2.1 §10.6.7: For elements that establish a new BFC, the auto block-size
		// extends to include the block-end margin edge of any floating descendants.
		if createsBFC {
			for _, child := range box.Children {
				if child.Style != nil && child.Style.GetFloat() != css.FloatNone {
					floatBottom := (dir.BlockPos(child) - parentContentTop) + dir.BlockSize(child) + dir.BlockEndEdge(child.Margin)
					if floatBottom > maxBottom {
						maxBottom = floatBottom
					}
				}
			}
		}

		if maxBottom < 0 {
			maxBottom = 0
		}
		// Block-size must be border-box (content + padding + borders)
		// maxBottom is content block-size, so add padding and borders
		dir.SetBlockSize(box, maxBottom+dir.BlockBorderBox(box.Padding, box.Border))

		// CSS 2.1 §8.3.1: When parent-child block-end margin collapsing applies,
		// propagate the last child's block-end margin to the parent's block-end margin.
		// The collapsed margin is the combination of parent's and child's margins.
		if parentChildBottomCollapse && lastInFlowChild != nil && dir.BlockEndEdge(lastInFlowChild.Margin) != 0 {
			parentMB := dir.BlockEndEdge(box.Margin)
			childMB := dir.BlockEndEdge(lastInFlowChild.Margin)
			if parentMB >= 0 && childMB >= 0 {
				if childMB > parentMB {
					dir.SetBlockEndEdge(&box.Margin, childMB)
				}
			} else if parentMB < 0 && childMB < 0 {
				if childMB < parentMB {
					dir.SetBlockEndEdge(&box.Margin, childMB)
				}
			} else {
				dir.SetBlockEndEdge(&box.Margin, parentMB+childMB)
			}
		}
	}

	// Re-apply min/max block size constraints after auto block-size calculation
	if maxHeight, ok := style.GetLength(dir.MaxBlockSizeProp()); ok {
		if dir.BlockSize(box) > maxHeight {
			dir.SetBlockSize(box, maxHeight)
			// When max block-size constrains an auto-block-size element with non-visible overflow,
			// the box now has a definite block size. Direct children with block-size:% were laid out
			// when block size was 0, so they resolved as auto. Re-layout them so
			// they resolve against the now-definite parent block size.
			if !hasExplicitHeight && style.GetOverflow() != css.OverflowVisible {
				childAvailW := dir.InlineSize(box) - dir.InlineBorderBox(box.Padding, box.Border)
				for i, child := range box.Children {
					if child == nil || child.Node == nil || child.Style == nil {
						continue
					}
					if _, hasPct := child.Style.GetPercentage(dir.BlockSizeProp()); hasPct {
						newChild := le.layoutNodeHTB(child.Node, child.X, child.Y, childAvailW, computedStyles, box)
						if newChild != nil {
							box.Children[i] = newChild
						}
					}
				}
			}
		}
	}
	if minHeight, ok := style.GetLength(dir.MinBlockSizeProp()); ok {
		if dir.BlockSize(box) < minHeight {
			dir.SetBlockSize(box, minHeight)
		}
	}

	// Deferred relative-positioning fix: position:relative children with percentage
	// top/bottom were resolved against cbHeight=0 when parent had auto block-size.
	// Now that block-size is final, apply the correct offsets.
	if parentContentHeightIsZero {
		cbH := dir.BlockSize(box) - dir.BlockBorderBox(box.Padding, box.Border)
		if cbH > 0 {
			for _, childBox := range box.Children {
				if childBox.Position != css.PositionRelative || childBox.Style == nil {
					continue
				}
				var dy float64
				if pct, ok := childBox.Style.GetPercentage("top"); ok {
					dy = cbH * pct / 100.0
				} else if pct, ok := childBox.Style.GetPercentage("bottom"); ok {
					dy = -cbH * pct / 100.0
				} else {
					continue
				}
				if dy != 0 {
					childBox.Y += dy
					le.shiftChildren(childBox, 0, dy)
				}
			}
		}
	}

	// Phase 7 Enhancement: Inline elements always shrink-wrap to children
	if display == css.DisplayInline && len(box.Children) > 0 {
		// Calculate width from children
		// For inline formatting context, children flow horizontally so we SUM their widths
		totalChildWidth := 0.0
		maxChildHeight := 0.0
		for _, child := range box.Children {
			childWidth := le.getTotalWidth(child)
			totalChildWidth += childWidth
			childHeight := le.getTotalHeight(child)
			if childHeight > maxChildHeight {
				maxChildHeight = childHeight
			}
		}

		box.Width = totalChildWidth
		box.Height = maxChildHeight
	}

	// Phase 5 Enhancement: Float shrink-wrapping
	// If this is a float without explicit width, shrink-wrap to content
	if floatType != css.FloatNone && !hasExplicitWidth && len(box.Children) > 0 {
		// For inline formatting context (inline children), sum widths horizontally
		// For block formatting context (block children), take max width (vertical stacking)
		allInline := true
		for _, child := range box.Children {
			if child.Style != nil {
				childDisplay := child.Style.GetDisplay()
				if childDisplay != css.DisplayInline && childDisplay != css.DisplayInlineBlock && child.Node != nil && child.Node.Type != html.TextNode {
					allInline = false
					break
				}
			}
		}

		if allInline {
			// Inline formatting context: compute content width from max right edge
			// Children (text boxes, span wrappers) may overlap so summing would double-count.
			// Instead, find the rightmost border-box edge relative to the content area.
			contentAreaLeft := box.X + box.Border.Left + box.Padding.Left
			maxContentRight := 0.0
			for _, child := range box.Children {
				childRight := child.X + child.Width
				if childRight > maxContentRight {
					maxContentRight = childRight
				}
			}
			shrinkContentWidth := maxContentRight - contentAreaLeft
			if shrinkContentWidth > 0 {
				// box.Width is border-box: content + padding + borders
				box.Width = shrinkContentWidth + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right
			}
		} else {
			// Block formatting context: compute max-content width.
			// CSS 2.1 §10.3.5: preferred width = width with infinite available space.
			// Float children would all be on one line → sum their widths.
			// Non-float block children stack vertically → take max width.
			floatWidthSum := 0.0
			maxNonFloatWidth := 0.0
			for _, child := range box.Children {
				// Skip whitespace-only text nodes — they don't contribute to
				// shrink-to-fit width (CSS 2.1 §9.2.2.1: whitespace between
				// block/float children doesn't generate boxes).
				if child.Node != nil && child.Node.Type == html.TextNode {
					if strings.TrimSpace(child.Node.Text) == "" {
						continue
					}
				}
				childWidth := le.computeShrinkToFitChildWidth(child)
				// Text nodes are always inline content, never float children,
				// even if they inherit float from a parent container.
				isFloat := child.Style != nil && child.Style.GetFloat() != css.FloatNone &&
					(child.Node == nil || child.Node.Type != html.TextNode)
				if isFloat {
					floatWidthSum += childWidth
				} else {
					if childWidth > maxNonFloatWidth {
						maxNonFloatWidth = childWidth
					}
				}
			}
			maxChildWidth := floatWidthSum
			if maxNonFloatWidth > maxChildWidth {
				maxChildWidth = maxNonFloatWidth
			}
			if maxChildWidth > 0 {
				// box.Width is border-box: content + own padding + own borders
				box.Width = maxChildWidth + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right
			}
			// Re-layout auto-width block children to use the new container width
			containerContentWidth := box.Width - box.Padding.Left - box.Padding.Right - box.Border.Left - box.Border.Right
			for _, child := range box.Children {
				if child.Style != nil {
					if _, hasW := child.Style.GetLength("width"); !hasW {
						if _, hasPct := child.Style.GetPercentage("width"); !hasPct {
							childDisplay := child.Style.GetDisplay()
							if childDisplay != css.DisplayInline &&
								child.Style.GetFloat() == css.FloatNone &&
								child.Style.GetPosition() != css.PositionAbsolute && child.Style.GetPosition() != css.PositionFixed {
								child.Width = containerContentWidth - child.Border.Left - child.Padding.Left -
									child.Padding.Right - child.Border.Right - child.Margin.Left - child.Margin.Right
								if child.Width < 0 {
									child.Width = 0
								}
							}
						}
					}
				}
			}
		}

		// Re-apply min/max inline size after shrink-wrapping
		// (shrink-to-fit overrides the initial inline size, must re-clamp)
		shrinkContent := dir.InlineSize(box) - dir.InlineBorderBox(box.Padding, box.Border)
		if minW, ok := style.GetLength(dir.MinInlineSizeProp()); ok && shrinkContent < minW {
			dir.SetInlineSize(box, minW+dir.InlineBorderBox(box.Padding, box.Border))
		}
		if maxW, ok := style.GetLength(dir.MaxInlineSizeProp()); ok {
			maxBorderBox := maxW + dir.InlineBorderBox(box.Padding, box.Border)
			if dir.InlineSize(box) > maxBorderBox {
				dir.SetInlineSize(box, maxBorderBox)
			}
		}
	}

	// Shrink-wrap absolutely positioned elements without explicit width
	if (position == css.PositionAbsolute || position == css.PositionFixed) && !hasExplicitWidth && len(box.Children) > 0 {
		maxChildWidth := 0.0
		for _, child := range box.Children {
			// child.Width is border-box for block-level children, so margin-box = margins + child.Width
			childWidth := child.Margin.Left + child.Width + child.Margin.Right
			if childWidth > maxChildWidth {
				maxChildWidth = childWidth
			}
		}
		if maxChildWidth > 0 {
			// box.Width is border-box: content + own padding + own borders
			box.Width = maxChildWidth + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right
		}
		// After shrink-wrap, update block children with auto width to use the new parent width
		containerContentWidth := box.Width - box.Padding.Left - box.Padding.Right - box.Border.Left - box.Border.Right
		for _, child := range box.Children {
			if child.Style != nil {
				childDisplay := child.Style.GetDisplay()
				if _, hasW := child.Style.GetLength("width"); !hasW && childDisplay != css.DisplayInline &&
					child.Style.GetFloat() == css.FloatNone &&
					child.Style.GetPosition() != css.PositionAbsolute && child.Style.GetPosition() != css.PositionFixed {
					child.Width = containerContentWidth - child.Border.Left - child.Padding.Left - child.Padding.Right - child.Border.Right -
						child.Margin.Left - child.Margin.Right
					if child.Width < 0 {
						child.Width = 0
					}
					// Re-apply text-align with the updated width
					if child.Style != nil {
						if ta, ok := child.Style.Get("text-align"); ok && ta != "left" && ta != "" {
							le.applyTextAlign(child, ta, child.Width)
						}
					}
				}
			}
		}
	}

	// Phase 4: Apply absolute positioning AFTER children layout and height finalization
	if position == css.PositionAbsolute || position == css.PositionFixed {
		oldX, oldY := box.X, box.Y
		le.applyAbsolutePositioning(box)
		// Shift all children by the position delta
		dx, dy := box.X-oldX, box.Y-oldY
		if dx != 0 || dy != 0 {
			le.shiftChildren(box, dx, dy)
		}
	}

	// Phase 5: Handle float positioning AFTER children layout and shrink-wrapping
	var floatY float64
	if floatType != css.FloatNone && position == css.PositionStatic {
		oldX, oldY := box.X, box.Y
		// box.Width is border-box (content + padding + borders), so margin-box is just margins + box.Width
		floatTotalWidth := box.Margin.Left + box.Width + box.Margin.Right

		// Phase 5 Enhancement: Check if float fits, apply drop if needed
		// margin.Top was already applied to y at line 276 (y += margin.Top) and is
		// included in box.Y, so don't add it again here
		floatY = le.getFloatDropY(floatType, floatTotalWidth, box.Y, availableWidth)
		box.Y = floatY

		// Position float horizontally
		if floatType == css.FloatLeft {
			// Position at left edge (accounting for existing left floats)
			leftOffset, _ := le.getFloatOffsets(floatY)
			box.X = x + leftOffset
		} else if floatType == css.FloatRight {
			// Position at right edge (accounting for existing right floats)
			_, rightOffset := le.getFloatOffsets(floatY)
			box.X = x + availableWidth - floatTotalWidth - rightOffset
		}

		// Shift children by the position delta
		dx, dy := box.X-oldX, box.Y-oldY
		if dx != 0 || dy != 0 {
			le.shiftChildren(box, dx, dy)
		}
	}

	// Restore BFC float context - remove floats added inside this BFC
	if createsBFC {
		le.floats = le.floats[:le.floatBase]
		le.floatBase = le.floatBaseStack[len(le.floatBaseStack)-1]
		le.floatBaseStack = le.floatBaseStack[:len(le.floatBaseStack)-1]
	}

	// CSS Counter support: Pop counter scopes that were reset on this element
	if counterResets != nil {
		for name := range counterResets {
			le.counterPop(name)
		}
	}

	// Add to float tracking (after BFC pop so float is in parent context)
	if floatType != css.FloatNone && position == css.PositionStatic {
		le.addFloat(box, floatType, floatY)
	}

	// After all positioning is done, fix float:right children that were
	// positioned before the parent width was finalized (shrink-to-fit containers)
	if !hasExplicitWidth && box.Width > 0 {
		le.repositionFloatRightChildren(box)
	}

	// CSS Writing Modes §6.4: For block-level elements with writing-mode: vertical-rl/lr,
	// transform the horizontally-laid-out children to vertical columns.
	// Each horizontal "line" of children (grouped by Y position) becomes a vertical column.
	// This only applies to block/inline-block elements — flex and grid have their own layout.
	// Only the OUTERMOST element that establishes a new vertical writing-mode context
	// runs the transform. If the parent already has vertical WM, the parent's transform
	// already handled this element (repositioned it as part of the parent's column layout).
	if style != nil && !isImage && !isSVG && len(box.Children) > 0 {
		if display == css.DisplayBlock || display == css.DisplayInlineBlock || display == css.DisplayFlowRoot || display == css.DisplayListItem {
			if wm, ok := style.Get("writing-mode"); ok {
				isVertical := wm == "vertical-rl" || wm == "vertical-lr" || wm == "sideways-rl" || wm == "sideways-lr"
				if isVertical {
					parentIsVertical := false
					if node.Parent != nil {
						if parentStyle := computedStyles[node.Parent]; parentStyle != nil {
							if parentWM, pok := parentStyle.Get("writing-mode"); pok {
								parentIsVertical = parentWM == "vertical-rl" || parentWM == "vertical-lr" || parentWM == "sideways-rl" || parentWM == "sideways-lr"
							}
						}
					}
					if !parentIsVertical {
						preTransformWidth := box.Width
						preTransformHeight := box.Height

						transformToVerticalRL(box, wm)

						// Re-position absolutely positioned children using vertical-mode
						// constraint equations (CSS Writing Modes §7.1)
						repositionAbsPosAfterVerticalTransform(box, wm, preTransformWidth, preTransformHeight)
					}
				}
			}
		}
	}

	return box
}


// findPositionedAncestorBox walks up the Box parent chain to find the nearest
// ancestor that creates a containing block for absolutely positioned elements.
// Returns nil if none found (viewport = initial containing block).
func findPositionedAncestorBox(box *Box) *Box {
	current := box
	for current != nil {
		if boxIsContainingBlockForAbsPos(current) {
			return current
		}
		current = current.Parent
	}
	return nil
}

// LayoutChildren for BlockLayoutMode - to be implemented as refactor progresses

// ComputeIntrinsicSizes for InlineLayoutMode
func (m *InlineLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for InlineLayoutMode - to be implemented as refactor progresses
func (m *InlineLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	// This will be filled in as we refactor layoutNode
	return nil
}

// ComputeIntrinsicSizes for FlexLayoutMode
func (m *FlexLayoutMode) ComputeIntrinsicSizes(le *LayoutEngine, node *html.Node, style *css.Style, computedStyles map[*html.Node]*css.Style) IntrinsicSizes {
	// Flex intrinsic sizing follows CSS Flexible Box Layout Module Level 1 §9.9
	// For now, delegate to block computation
	return le.ComputeIntrinsicSizes(node, style, computedStyles)
}

// LayoutChildren for FlexLayoutMode - to be implemented
func (m *FlexLayoutMode) LayoutChildren(le *LayoutEngine, container *Box, children []*html.Node, availableWidth float64, computedStyles map[*html.Node]*css.Style) []*Box {
	// This will implement the full flex layout algorithm
	return nil
}

// ============================================================================
