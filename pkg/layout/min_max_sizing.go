package layout

import (
	"louis14/pkg/css"
	"strings"
)

// ComputeMinMaxSizes computes the intrinsic min-content and max-content
// inline sizes for a layout node. Returns content-box values (excludes the
// node's own border+padding). Callers convert to border-box when needed.
//
// Mirrors Blink's ComputeMinMaxSizes (layout_box.h).
func ComputeMinMaxSizes(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) MinMaxSizes {
	style := node.Style()
	if style == nil {
		return MinMaxSizes{}
	}

	wdm := space.WritingDirection
	geom := ComputeFragmentGeometry(style, wdm)

	// Table with table-layout:fixed + percentage inline-size: max-content is effectively
	// infinite. Mirrors Blink's TableLayoutAlgorithm::ComputeMinMaxSizes
	// (table_layout_algorithm.cc:727): `if (is_fixed_layout && Style().LogicalWidth().HasPercent())
	// min_max.max_size = TableTypes::kTableMaxInlineSize`. This lets ancestors doing
	// max-content sizing (flex basis:auto, shrink-to-fit) see the table as "wants as
	// much space as available", rather than summing potentially-zero column max-contents.
	// Skip the fast path so we don't collapse to a 0-resolved percent width.
	disp := style.GetDisplay()
	if (disp == css.DisplayTable || disp == css.DisplayInlineTable) &&
		style.GetTableLayout() == css.TableLayoutFixed &&
		hasPercentLogicalWidth(style, wdm) {
		minResult := measureBlockMinMax(node, ctx, space)
		result := MinMaxSizes{
			MinContent: minResult.MinContent,
			MaxContent: kTableMaxInlineSize,
		}
		minInline := ResolveMinInlineSize(style, wdm, space, geom)
		if result.MinContent < minInline {
			result.MinContent = minInline
		}
		if result.MaxContent < minInline {
			result.MaxContent = minInline
		}
		if maxInline, hasMax := ResolveMaxInlineSize(style, wdm, space, geom); hasMax {
			if result.MinContent > maxInline {
				result.MinContent = maxInline
			}
			if result.MaxContent > maxInline {
				result.MaxContent = maxInline
			}
		}
		return result
	}

	// If the node has an explicit inline-size, min = max = that size (content-box).
	// Skip this fast-path for replaced elements: their inline-size may be
	// overridden by the CSS 2.1 §10.3.2 constraint resolution when a
	// percentage block-size resolves to a definite value and an aspect ratio
	// transfers it to the inline dimension. ComputeReplacedSize handles
	// this correctly, so replaced elements always go through that path below.
	isReplaced := node.DOMNode != nil && isReplacedElement(node.DOMNode)
	if !isReplaced {
		if explicitInline, ok := ResolveInlineSize(style, wdm, space, geom); ok {
			result := MinMaxSizes{MinContent: explicitInline, MaxContent: explicitInline}
			// Apply min/max inline-size constraints (all content-box).
			minInline := ResolveMinInlineSize(style, wdm, space, geom)
			if result.MinContent < minInline {
				result.MinContent = minInline
			}
			if result.MaxContent < minInline {
				result.MaxContent = minInline
			}
			if maxInline, hasMax := ResolveMaxInlineSize(style, wdm, space, geom); hasMax {
				if result.MinContent > maxInline {
					result.MinContent = maxInline
				}
				if result.MaxContent > maxInline {
					result.MaxContent = maxInline
				}
			}
			return result
		}
	}

	// CSS Containment: size containment — intrinsic sizes are 0 (element sized as empty).
	// Inline-size containment also zeroes intrinsic inline sizes.
	if style.HasSizeContainment() || style.HasInlineSizeContainment() {
		return MinMaxSizes{}
	}

	// Replaced elements (img, canvas, etc.): compute intrinsic inline-size without
	// block-axis min/max constraints transferring through aspect ratio.
	// CSS Sizing 3 §5.1: intrinsic sizes are based on content, not block constraints.
	if node.DOMNode != nil && isReplacedElement(node.DOMNode) {
		inlineSize := ComputeReplacedIntrinsicInlineSize(ctx, node, style, space)
		return MinMaxSizes{MinContent: inlineSize, MaxContent: inlineSize}
	}

	// Compute intrinsic sizes based on children (content-box).
	var result MinMaxSizes

	display := style.GetDisplay()
	if display == css.DisplayFlex || display == css.DisplayInlineFlex {
		// Flex containers: use flex-specific min/max computation.
		result = measureFlexMinMax(node, ctx, space)
	} else if hasOnlyInlineChildren(node) {
		// Inline formatting context: measure via line breaker.
		result = measureInlineMinMax(node, ctx, space)
	} else {
		// Block formatting context: take max of children's sizes.
		result = measureBlockMinMax(node, ctx, space)
	}

	// CSS Sizing 4: aspect-ratio on non-replaced elements.
	// When an element has a preferred aspect ratio and definite block-size
	// constraints, those transfer through the ratio to affect intrinsic
	// inline sizes. This mirrors Blink's NGBlockNode::ComputeMinMaxSizes
	// which accounts for aspect-ratio before applying min/max inline constraints.
	if ar := style.GetAspectRatio(); ar.IsSet && ar.Width > 0 && ar.Height > 0 {
		isBorderBox := style.GetBoxSizing() == "border-box"
		bp := geom.InlineBorderPadding()

		// Helper to convert an aspect-ratio transfer from block to inline.
		// The ratio applies to the content-box unless box-sizing: border-box,
		// in which case it applies to the border-box.
		transferBlockToInline := func(blockVal float64, blockBP float64) float64 {
			if isBorderBox {
				// border-box: ratio applies to the border-box
				return blockVal * ar.Width / ar.Height
			}
			// content-box: ratio applies to content, blockVal is content-box
			return blockVal * ar.Width / ar.Height
		}

		// Check for definite block-size (height).
		if blockSize, ok := ResolveBlockSize(style, wdm, space, geom); ok {
			inlineFromRatio := transferBlockToInline(blockSize, geom.BlockBorderPadding())
			if isBorderBox {
				// blockSize from ResolveBlockSize with border-box already had BP subtracted.
				// Add it back for the ratio, then subtract inline BP.
				inlineFromRatio = (blockSize + geom.BlockBorderPadding()) * ar.Width / ar.Height - bp
				if inlineFromRatio < 0 {
					inlineFromRatio = 0
				}
			}
			result.MinContent = inlineFromRatio
			result.MaxContent = inlineFromRatio
		}

		// Check for min-block-size (min-height) transferring to min-inline-size.
		minBlock := ResolveMinBlockSize(style, wdm, space, geom)
		if minBlock > 0 {
			minInlineFromRatio := transferBlockToInline(minBlock, geom.BlockBorderPadding())
			if isBorderBox {
				// minBlock from ResolveMinBlockSize with border-box already had BP subtracted.
				minInlineFromRatio = (minBlock + geom.BlockBorderPadding()) * ar.Width / ar.Height - bp
				if minInlineFromRatio < 0 {
					minInlineFromRatio = 0
				}
			}
			if minInlineFromRatio > result.MinContent {
				result.MinContent = minInlineFromRatio
			}
			if minInlineFromRatio > result.MaxContent {
				result.MaxContent = minInlineFromRatio
			}
		}

		// Check for max-block-size (max-height) transferring to max-inline-size.
		if maxBlock, hasMax := ResolveMaxBlockSize(style, wdm, space, geom); hasMax {
			maxInlineFromRatio := transferBlockToInline(maxBlock, geom.BlockBorderPadding())
			if isBorderBox {
				maxInlineFromRatio = (maxBlock + geom.BlockBorderPadding()) * ar.Width / ar.Height - bp
				if maxInlineFromRatio < 0 {
					maxInlineFromRatio = 0
				}
			}
			if maxInlineFromRatio < result.MinContent {
				result.MinContent = maxInlineFromRatio
			}
			if maxInlineFromRatio < result.MaxContent {
				result.MaxContent = maxInlineFromRatio
			}
		}
	}

	// Apply min/max inline-size constraints (all content-box).
	minInline := ResolveMinInlineSize(style, wdm, space, geom)
	if result.MinContent < minInline {
		result.MinContent = minInline
	}
	if result.MaxContent < minInline {
		result.MaxContent = minInline
	}
	if maxInline, hasMax := ResolveMaxInlineSize(style, wdm, space, geom); hasMax {
		if result.MinContent > maxInline {
			result.MinContent = maxInline
		}
		if result.MaxContent > maxInline {
			result.MaxContent = maxInline
		}
	}

	// Apply intrinsic keyword min/max constraints (min-content, max-content, etc.)
	// that ResolveMinInlineSize/ResolveMaxInlineSize can't handle.
	applyIntrinsicKeywordMinMax(style, wdm, &result)

	return result
}

// resolveIntrinsicInlineKeyword checks if a CSS property (min-width, max-width, etc.)
// is an intrinsic sizing keyword and returns the resolved value.
// For min-content → result.MinContent, max-content → result.MaxContent,
// fit-content → result.MaxContent (unconstrained equivalent).
func resolveIntrinsicInlineKeyword(style *css.Style, wdm WritingDirectionMode, prop string, result MinMaxSizes) (float64, bool) {
	v, ok := style.Get(prop)
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(v)
	switch v {
	case "min-content":
		return result.MinContent, true
	case "max-content":
		return result.MaxContent, true
	case "fit-content":
		return result.MaxContent, true
	}
	return 0, false
}

// applyIntrinsicKeywordMinMax applies intrinsic keyword min/max inline-size
// constraints (min-content, max-content, fit-content) to computed min/max sizes.
// This handles the cases that ResolveMinInlineSize/ResolveMaxInlineSize miss.
func applyIntrinsicKeywordMinMax(style *css.Style, wdm WritingDirectionMode, result *MinMaxSizes) {
	minProp := "min-width"
	maxProp := "max-width"
	if wdm.IsVertical() {
		minProp = "min-height"
		maxProp = "max-height"
	}
	// min-width: <intrinsic> acts as a floor.
	if minVal, ok := resolveIntrinsicInlineKeyword(style, wdm, minProp, *result); ok {
		if result.MinContent < minVal {
			result.MinContent = minVal
		}
		if result.MaxContent < minVal {
			result.MaxContent = minVal
		}
	}
	// max-width: <intrinsic> acts as a cap.
	if maxVal, ok := resolveIntrinsicInlineKeyword(style, wdm, maxProp, *result); ok {
		if result.MinContent > maxVal {
			result.MinContent = maxVal
		}
		if result.MaxContent > maxVal {
			result.MaxContent = maxVal
		}
	}
}

// measureInlineMinMax computes min/max content sizes for a node with
// only inline-level children by running the line breaker in both modes.
func measureInlineMinMax(node *LayoutInputNode, ctx *LayoutContext, space ConstraintSpace) MinMaxSizes {
	itemsData := CollectInlines(node)
	if len(itemsData.Items) == 0 {
		return MinMaxSizes{}
	}

	fonts := ctx.FontConfig
	wdm := space.WritingDirection

	// Resolve the node's own definite block-size for percentage resolution.
	// This allows children with percentage heights (e.g., img { height: 100% })
	// to resolve against the containing block's height.
	blockForPct := Indefinite
	if nodeStyle := node.Style(); nodeStyle != nil {
		nodeGeom := ComputeFragmentGeometry(nodeStyle, wdm)
		if bs, ok := ResolveBlockSize(nodeStyle, wdm, space, nodeGeom); ok {
			blockForPct = bs
		} else if space.PercentageResolutionSize.BlockSize > 0 {
			blockForPct = space.PercentageResolutionSize.BlockSize
		}
	}

	// Min-content: break at every opportunity.
	minAvailBlock := Indefinite
	if blockForPct != Indefinite {
		minAvailBlock = blockForPct
	}
	minBuilder := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 0, BlockSize: minAvailBlock}).
		SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize)
	if blockForPct != Indefinite {
		minBuilder.SetPercentageResolutionSize(LogicalSize{
			InlineSize: space.PercentageResolutionInlineSize,
			BlockSize:  blockForPct,
		})
	}
	minSpace := minBuilder.Build()
	minLB := NewLineBreaker(itemsData, ctx, minSpace, fonts, LineBreakerMinContent)
	var minContent float64
	var line LineInfo
	for minLB.NextLine(&line) {
		if line.Width > minContent {
			minContent = line.Width
		}
	}

	// Max-content: never wrap.
	maxBuilder := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 1e9, BlockSize: minAvailBlock}).
		SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize)
	if blockForPct != Indefinite {
		maxBuilder.SetPercentageResolutionSize(LogicalSize{
			InlineSize: space.PercentageResolutionInlineSize,
			BlockSize:  blockForPct,
		})
	}
	maxSpace := maxBuilder.Build()
	maxLB := NewLineBreaker(itemsData, ctx, maxSpace, fonts, LineBreakerMaxContent)
	var maxContent float64
	for maxLB.NextLine(&line) {
		if line.Width > maxContent {
			maxContent = line.Width
		}
	}

	return MinMaxSizes{MinContent: minContent, MaxContent: maxContent}
}

// computeContentMinMaxSizes is like ComputeMinMaxSizes but does NOT
// short-circuit on an explicit inline-size. Used by flex §4.5 to compute
// the content-based minimum size independent of the CSS width property.
func computeContentMinMaxSizes(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) MinMaxSizes {
	style := node.Style()
	if style == nil {
		return MinMaxSizes{}
	}
	wdm := space.WritingDirection
	geom := ComputeFragmentGeometry(style, wdm)

	// Replaced elements use ComputeReplacedSize for content-based sizing.
	if node.DOMNode != nil && isReplacedElement(node.DOMNode) {
		inlineSize, _ := ComputeReplacedSize(ctx, node, style, space)
		return MinMaxSizes{MinContent: inlineSize, MaxContent: inlineSize}
	}

	var result MinMaxSizes
	display := style.GetDisplay()
	if display == css.DisplayFlex || display == css.DisplayInlineFlex {
		result = measureFlexMinMax(node, ctx, space)
	} else if hasOnlyInlineChildren(node) {
		result = measureInlineMinMax(node, ctx, space)
	} else {
		result = measureBlockMinMax(node, ctx, space)
	}

	// Apply min/max inline-size constraints (but NOT explicit inline-size).
	minInline := ResolveMinInlineSize(style, wdm, space, geom)
	if result.MinContent < minInline {
		result.MinContent = minInline
	}
	if result.MaxContent < minInline {
		result.MaxContent = minInline
	}
	if maxInline, hasMax := ResolveMaxInlineSize(style, wdm, space, geom); hasMax {
		if result.MinContent > maxInline {
			result.MinContent = maxInline
		}
		if result.MaxContent > maxInline {
			result.MaxContent = maxInline
		}
	}

	// Apply intrinsic keyword min/max constraints.
	applyIntrinsicKeywordMinMax(style, wdm, &result)

	return result
}

// measureFlexMinMax computes min/max content inline sizes for a flex container.
//
// For flex-direction: row (main = inline):
//   - max-content = sum of items' max-content inline sizes + margins + border/padding
//   - min-content = same (nowrap); max of items' min-content (wrap)
//
// For flex-direction: column (main = block):
//   - max-content = max of items' max-content inline sizes (cross = inline)
//   - min-content = max of items' min-content inline sizes
func measureFlexMinMax(node *LayoutInputNode, ctx *LayoutContext, space ConstraintSpace) MinMaxSizes {
	style := node.Style()
	wdm := space.WritingDirection

	flexDir := "row"
	if v, ok := style.Get("flex-direction"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "row", "row-reverse", "column", "column-reverse":
			flexDir = v
		}
	}
	isRow := flexDir == "row" || flexDir == "row-reverse"
	wrapMode := "nowrap"
	if v, ok := style.Get("flex-wrap"); ok {
		wrapMode = strings.TrimSpace(v)
	}
	canWrap := wrapMode == "wrap" || wrapMode == "wrap-reverse"

	// Resolve the container's definite cross-size for aspect-ratio transfer.
	// Only needed for row flex (cross = block-size). For column flex, the cross
	// IS the inline-size we're computing, so it's never definite here.
	containerGeom := ComputeFragmentGeometry(style, wdm)
	var containerCrossContent float64
	var hasDefiniteCross bool
	if isRow {
		if bs, ok := ResolveBlockSize(style, wdm, space, containerGeom); ok {
			containerCrossContent = bs
			hasDefiniteCross = true
		}
	}

	// Resolve container align-items for stretch detection.
	alignItems := "stretch"
	if v, ok := style.Get("align-items"); ok {
		alignItems = strings.TrimSpace(v)
	}

	// Resolve main-axis gap for intrinsic sizing.
	var mainGap float64
	if isRow {
		if v, ok := style.GetLength("column-gap"); ok {
			mainGap = v
		}
	} else {
		if v, ok := style.GetLength("row-gap"); ok {
			mainGap = v
		}
	}

	var sumMin, sumMax float64
	var maxMin, maxMax float64
	var itemCount int

	// Use the same anonymous flex item wrapping as the full layout path.
	// CSS Flexbox §4: bare text nodes in a flex container are wrapped in
	// anonymous block flex items. Without this, text-only flex containers
	// (e.g., <div style="display:inline-flex">Success!</div>) would compute
	// zero intrinsic size because text nodes are skipped.
	flexChildren := buildFlexChildren(node, style)

	for _, child := range flexChildren {
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}
		// Skip OOF children — they are not flex items and do not
		// contribute to the container's intrinsic size or gap count.
		pos := childStyle.GetPosition()
		if pos == css.PositionAbsolute || pos == css.PositionFixed {
			continue
		}
		// CSS Flexbox §4.4: visibility:collapse items are not rendered and
		// do not contribute to the container's main-axis intrinsic size.
		if childStyle.GetVisibility() == "collapse" {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)
		csb := NewConstraintSpaceBuilder(wdm, childWDM, false).
			SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, ctx)).
			SetOrthogonalFallbackBlockSize(space.OrthogonalFallbackBlockSize).
			SetAvailableSize(space.AvailableSize).
			SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize)
		childSpace := csb.Build()

		childGeom := ComputeFragmentGeometry(childStyle, childWDM, space.PercentageResolutionInlineSize)

		childBP := childGeom.InlineBorderPadding()
		childMargins := ResolveMargins(childStyle, childWDM, space.PercentageResolutionInlineSize)

		// Determine if this item will stretch in the cross axis.
		willStretch := false
		if isRow && hasDefiniteCross {
			alignSelf := alignItems
			if v, ok := childStyle.Get("align-self"); ok {
				v = strings.TrimSpace(v)
				if v != "" && v != "auto" {
					alignSelf = v
				}
			}
			if alignSelf == "stretch" {
				if _, ok := ResolveBlockSize(childStyle, childWDM, childSpace, childGeom); !ok {
					willStretch = true
				}
			}
		}

		// CSS Flexbox §9.8: When a stretched flex item's cross-size is definite,
		// descendants with percentage heights should resolve against it.
		// Rebuild the constraint space with the stretched cross-size as the
		// percentage resolution block-size.
		if willStretch {
			crossContent := containerCrossContent - childGeom.BlockBorderPadding() - childMargins.BlockSum()
			if crossContent < 0 {
				crossContent = 0
			}
			childSpace = NewConstraintSpaceBuilder(wdm, childWDM, false).
				SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, ctx)).
				SetOrthogonalFallbackBlockSize(space.OrthogonalFallbackBlockSize).
				SetAvailableSize(LogicalSize{
					InlineSize: space.AvailableSize.InlineSize,
					BlockSize:  crossContent,
				}).
				SetPercentageResolutionSize(LogicalSize{
					InlineSize: space.PercentageResolutionInlineSize,
					BlockSize:  crossContent,
				}).
				SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
				Build()
		}

		// Check for aspect-ratio transfer from definite cross-size.
		// For row flex with definite height, items with aspect-ratio that
		// will stretch get their inline size from cross × ratio.
		transferred := false
		var childMin, childMax float64
		if willStretch && isRow && hasDefiniteCross {
			ar := childStyle.GetAspectRatio()
			hasAR := ar.IsSet && ar.Width > 0 && ar.Height > 0

			// Also check replaced elements for intrinsic aspect ratio.
			if !hasAR && child.DOMNode != nil && isReplacedElement(child.DOMNode) {
				info := GetIntrinsicSizingInfo(ctx, child)
				if info.HasAspectRatio && info.AspectRatio > 0 {
					hasAR = true
					ar = css.AspectRatio{IsSet: true, Width: info.AspectRatio, Height: 1}
				}
			}

			if hasAR {
				// Cross content size = container cross - item cross border/padding/margins.
				crossContent := containerCrossContent - childGeom.BlockBorderPadding() - childMargins.BlockSum()
				if crossContent < 0 {
					crossContent = 0
				}
				// Transfer: inline = cross × (width/height).
				inlineContent := crossContent * ar.Width / ar.Height
				childMin = inlineContent + childBP + childMargins.InlineSum()
				childMax = childMin
				transferred = true
			}
		}

		if !transferred {
			// CSS Flexbox §9.9.3 + Blink's conservative algorithm (csswg-drafts #8884):
			// For row flex, compute CSS Sizing-3 intrinsic contributions, then
			// adjust based on whether the item can actually flex to reach them.
			if isRow && wdm.IsOrthogonalTo(childWDM) {
				// Orthogonal row-flex child: the parent's main-axis = child's
				// block-axis. Use measureOrthogonalChild, which lays out the
				// child and returns its block-size (+ parent-WDM inline margins)
				// as the contribution to the parent's inline/main axis.
				oMin, oMax := measureOrthogonalChild(child, childStyle, childWDM, wdm, ctx, space)
				childMin = oMin
				childMax = oMax
			} else if isRow {
				outerExtra := childBP + childMargins.InlineSum()
				childGeom2 := ComputeFragmentGeometry(childStyle, childWDM)

				// Pure content intrinsic sizes (content-box, ignores explicit width).
				contentMM := computeContentMinMaxSizes(ctx, child, childSpace)

				// Explicit width (content-box), if any.
				explicit, hasExplicit := ResolveInlineSize(childStyle, childWDM, childSpace, childGeom2)

				// CSS Sizing-3 contributions = max(content intrinsic, explicit width).
				minContrib := contentMM.MinContent
				maxContrib := contentMM.MaxContent
				if hasExplicit {
					if explicit > minContrib {
						minContrib = explicit
					}
					if explicit > maxContrib {
						maxContrib = explicit
					}
				}

				flexBasis, basisIsContent := resolveFlexBasisForIntrinsic(
					childStyle, childWDM, childSpace,
					childGeom2, ctx, child)

				if basisIsContent {
					// Basis is content-derived: use CSS Sizing-3 contributions directly.
					childMin = minContrib + outerExtra
					childMax = maxContrib + outerExtra
				} else {
					flexGrow := childStyle.GetFlexGrow()
					flexShrink := childStyle.GetFlexShrink()

					// Hypothetical main size = flex base size clamped by min/max main.
					minMain := ResolveMinInlineSize(childStyle, childWDM, childSpace, childGeom2)
					hyp := flexBasis
					if hyp < minMain {
						hyp = minMain
					}
					if maxMain, ok := ResolveMaxInlineSize(childStyle, childWDM, childSpace, childGeom2); ok {
						if hyp > maxMain {
							hyp = maxMain
						}
					}

					// Conservative algorithm: if the item can't flex to reach the
					// CSS Sizing-3 contribution, use its hypothetical main size instead.
					// Min-content contribution:
					cantGrowMin := flexGrow == 0 && flexBasis < minContrib
					cantShrinkMin := flexShrink == 0 && flexBasis > minContrib
					if cantGrowMin || cantShrinkMin {
						childMin = hyp + outerExtra
					} else {
						childMin = minContrib + outerExtra
					}

					// Max-content contribution:
					cantGrowMax := flexGrow == 0 && flexBasis < maxContrib
					cantShrinkMax := flexShrink == 0 && flexBasis > maxContrib
					if cantGrowMax || cantShrinkMax {
						childMax = hyp + outerExtra
					} else {
						childMax = maxContrib + outerExtra
					}
				}
			} else {
				// Column flex: inline axis = cross axis. Item contributions
				// are just their intrinsic inline (cross) sizes.
				// For orthogonal children, the child's block-size maps to the
				// parent's inline-size, so we need measureOrthogonalChild.
				isOrthogonal := wdm.IsOrthogonalTo(childWDM)
				if isOrthogonal {
					oMin, oMax := measureOrthogonalChild(child, childStyle, childWDM, wdm, ctx, space)
					childMin = oMin
					childMax = oMax
				} else {
					childMM := ComputeMinMaxSizes(ctx, child, childSpace)
					childMin = childMM.MinContent + childBP + childMargins.InlineSum()
					childMax = childMM.MaxContent + childBP + childMargins.InlineSum()
				}
			}
		}

		sumMin += childMin
		sumMax += childMax
		if childMin > maxMin {
			maxMin = childMin
		}
		if childMax > maxMax {
			maxMax = childMax
		}
		itemCount++
	}

	// Add gaps between items: (N-1) * mainGap.
	totalGap := 0.0
	if itemCount > 1 && mainGap > 0 {
		totalGap = float64(itemCount-1) * mainGap
	}

	if isRow {
		if canWrap {
			// With wrapping, min-content = largest single item; max-content = sum + gaps.
			return MinMaxSizes{MinContent: maxMin, MaxContent: sumMax + totalGap}
		}
		return MinMaxSizes{MinContent: sumMin + totalGap, MaxContent: sumMax + totalGap}
	}
	// Column: inline = cross direction → max of items' inline sizes.
	return MinMaxSizes{MinContent: maxMin, MaxContent: maxMax}
}

// resolveFlexBasisForIntrinsic resolves a flex item's flex-basis to a definite
// content-box value for use in the container's intrinsic sizing algorithm
// (CSS Flexbox §9.9.1). Returns the resolved basis and whether the basis is
// content-dependent (i.e., "content" keyword or "auto" with no explicit main size).
func resolveFlexBasisForIntrinsic(
	childStyle *css.Style,
	childWDM WritingDirectionMode,
	childSpace ConstraintSpace,
	childGeom FragmentGeometry,
	ctx *LayoutContext,
	child *LayoutInputNode,
) (float64, bool) {
	fbv := childStyle.GetFlexBasisValue()

	if fbv.IsContent {
		return 0, true
	}

	if fbv.IsAuto {
		if explicit, ok := ResolveInlineSize(childStyle, childWDM, childSpace, childGeom); ok {
			return explicit, false
		}
		return 0, true
	}

	if fbv.IsPercent {
		return 0, true
	}
	if fbv.IsCalc {
		if resolved, ok := css.EvalCalcWithPercent(fbv.CalcExpr, fbv.FontSize, 0); ok {
			if resolved < 0 {
				resolved = 0
			}
			return resolved, false
		}
		return 0, true
	}

	basis := fbv.Length
	if basis < 0 {
		basis = 0
	}
	return basis, false
}

// measureBlockMinMax computes min/max content sizes for a node with
// block-level children by taking the maximum of each child's sizes.
//
// hasPercentLogicalWidth reports whether the node's logical inline-size
// is a percentage (including calc() expressions containing a % term).
// Mirrors Blink's Style().LogicalWidth().HasPercent() check.
func hasPercentLogicalWidth(style *css.Style, wdm WritingDirectionMode) bool {
	prop := "width"
	if wdm.IsVertical() {
		prop = "height"
	}
	val, ok := style.Get(prop)
	if !ok {
		return false
	}
	val = strings.TrimSpace(val)
	if strings.HasSuffix(val, "%") {
		if _, ok := css.ParsePercentage(val); ok {
			return true
		}
	}
	if css.IsCalcWithPercent(val) {
		return true
	}
	return false
}

// Floats are handled specially: multiple same-side floats placed side-by-side
// contribute their SUMMED inline sizes to max-content (since at max-content
// width, all floats fit beside each other). Min-content = max single float
// inline size (floats can always stack when width is insufficient).
func measureBlockMinMax(node *LayoutInputNode, ctx *LayoutContext, space ConstraintSpace) MinMaxSizes {
	var result MinMaxSizes

	parentWDM := space.WritingDirection

	// Resolve the node's own definite block-size (CSS height for HTB).
	// This is used as the percentage resolution block-size for children,
	// so that percentage-height descendants can resolve (e.g., img { height: 100% }
	// inside a div with explicit height).
	nodeBlockSize := Indefinite
	if nodeStyle := node.Style(); nodeStyle != nil {
		nodeGeom := ComputeFragmentGeometry(nodeStyle, parentWDM)
		if bs, ok := ResolveBlockSize(nodeStyle, parentWDM, space, nodeGeom); ok {
			nodeBlockSize = bs
		} else if space.PercentageResolutionSize.BlockSize > 0 {
			// Parent provided a definite block percentage resolution size
			// (e.g., from a flex item's explicit cross-size). Propagate it.
			nodeBlockSize = space.PercentageResolutionSize.BlockSize
		}
	}

	// Accumulate float inline sizes by side for max-content computation.
	// Mirrors Blink's NGBlockLayoutAlgorithm::ComputeMinMaxSizes: at max-content
	// width, same-side floats are placed side-by-side, so their total inline
	// size = sum of individual sizes.
	var floatStartMaxSum float64 // sum of inline-start (left) float max-content sizes
	var floatEndMaxSum float64   // sum of inline-end (right) float max-content sizes

	for _, child := range node.Children() {
		if child.IsText() {
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}

		// Out-of-flow children (position:absolute or position:fixed) are
		// removed from the normal flow and must NOT contribute to min/max
		// intrinsic sizing. CSS 2.1 §9.3: positioned elements don't
		// participate in the flow. Mirrors Blink's LayoutBox::ComputeIntrinsicLogicalWidths
		// which skips out-of-flow children.
		childPos := childStyle.GetPosition()
		if childPos == css.PositionAbsolute || childPos == css.PositionFixed {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)
		isOrthogonal := parentWDM.IsOrthogonalTo(childWDM)

		var childMin, childMax float64
		if isOrthogonal {
			// Orthogonal child: the parent's inline direction aligns with the
			// child's block direction. We need the child's block-size, which
			// requires actually laying out the child.
			// Mirrors Blink's NGOrthogonalWritingModeRootInlineSize().
			childMin, childMax = measureOrthogonalChild(child, childStyle, childWDM, parentWDM, ctx, space)
		} else {
			// Parallel child: use standard min/max inline-size computation.
			// Pass the node's definite block-size so children can resolve
			// percentage heights against it (e.g., img { height: 100% }
			// inside a div with height: 100px).
			csBuilder := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
				SetOrthogonalFallbackInlineSize(
					orthogonalFallbackSize(childWDM, ctx)).
				SetOrthogonalFallbackBlockSize(space.OrthogonalFallbackBlockSize).
				SetAvailableSize(space.AvailableSize).
				SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize)
			if nodeBlockSize != Indefinite {
				csBuilder.SetPercentageResolutionSize(LogicalSize{
					InlineSize: space.PercentageResolutionInlineSize,
					BlockSize:  nodeBlockSize,
				})
				// Set available block-size to make IsBlockSizeIndefinite() return false
				// so percentage heights resolve.
				csBuilder.SetAvailableSize(LogicalSize{
					InlineSize: space.AvailableSize.InlineSize,
					BlockSize:  nodeBlockSize,
				})
			}
			childSpace := csBuilder.Build()

			childMM := ComputeMinMaxSizes(ctx, child, childSpace)

			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			childBP := childGeom.InlineBorderPadding()
			childMargins := ResolveMargins(childStyle, childWDM, 0)
			childMin = childMM.MinContent + childBP + childMargins.InlineSum()
			childMax = childMM.MaxContent + childBP + childMargins.InlineSum()

			floatType := childStyle.GetFloat()
			if floatType == css.FloatLeft || floatType == css.FloatRight {
				// CSS 2.1 §9.5.1: clear forces a float below previous floats
				// on the cleared side(s), so cleared floats cannot be placed
				// side-by-side with earlier same-side floats. Flush the
				// current float row before starting a new one.
				clearType := childStyle.GetClear()
				if clearType != css.ClearNone {
					floatMaxRow := floatStartMaxSum + floatEndMaxSum
					if floatMaxRow > result.MaxContent {
						result.MaxContent = floatMaxRow
					}
					if clearType == css.ClearLeft || clearType == css.ClearBoth {
						floatStartMaxSum = 0
					}
					if clearType == css.ClearRight || clearType == css.ClearBoth {
						floatEndMaxSum = 0
					}
				}

				if floatType == css.FloatLeft {
					floatStartMaxSum += childMax
				} else {
					floatEndMaxSum += childMax
				}
				if childMin > result.MinContent {
					result.MinContent = childMin
				}
				continue
			}
		}

		// Non-float child (or orthogonal child — floats handled above for parallel).
		if childMin > result.MinContent {
			result.MinContent = childMin
		}

		isFloat := childStyle.GetFloat() != css.FloatNone
		if isFloat {
			// Orthogonal float: check clear before accumulating.
			clearType := childStyle.GetClear()
			if clearType != css.ClearNone {
				floatMaxRow := floatStartMaxSum + floatEndMaxSum
				if floatMaxRow > result.MaxContent {
					result.MaxContent = floatMaxRow
				}
				if clearType == css.ClearLeft || clearType == css.ClearBoth {
					floatStartMaxSum = 0
				}
				if clearType == css.ClearRight || clearType == css.ClearBoth {
					floatEndMaxSum = 0
				}
			}
			// Orthogonal float: accumulate as start-side (conservative).
			floatStartMaxSum += childMax
		} else {
			// Non-float block child: flush any pending float row, since
			// floats preceding this child occupy their own block positions
			// and don't extend into this child's inline space at max-content.
			floatMaxRow := floatStartMaxSum + floatEndMaxSum
			if floatMaxRow > result.MaxContent {
				result.MaxContent = floatMaxRow
			}
			floatStartMaxSum = 0
			floatEndMaxSum = 0
			if childMax > result.MaxContent {
				result.MaxContent = childMax
			}
		}
	}

	// Incorporate accumulated float sizes into max-content.
	// Start-side and end-side floats are on opposite sides, so they add.
	// Multiple same-side floats are placed side-by-side → sum.
	floatMaxSum := floatStartMaxSum + floatEndMaxSum
	if floatMaxSum > result.MaxContent {
		result.MaxContent = floatMaxSum
	}

	return result
}

// measureOrthogonalChild lays out an orthogonal child and returns its
// contribution to the parent's min/max inline-size. The child's block-size
// (after layout) becomes the parent's inline contribution.
//
// Uses a cache on LayoutContext to avoid redundant layouts and detect cycles.
// On cycle detection, falls back to the orthogonal fallback size (ICB cross-size).
func measureOrthogonalChild(
	child *LayoutInputNode,
	childStyle *css.Style,
	childWDM, parentWDM WritingDirectionMode,
	ctx *LayoutContext,
	space ConstraintSpace,
) (minContrib, maxContrib float64) {
	// Check cache first.
	if cached, isCycle := ctx.GetOrthogonalLayout(child); isCycle {
		// Cycle detected: fall back to ICB cross-size.
		fallback := orthogonalFallbackSize(childWDM, ctx)
		childMargins := ResolveMargins(childStyle, parentWDM, 0)
		contrib := fallback + childMargins.InlineSum()
		return contrib, contrib
	} else if cached != nil {
		// Use cached result's block-size as the contribution.
		childLogical := NewLogicalFragment(parentWDM, cached.Fragment)
		childMargins := ResolveMargins(childStyle, parentWDM, 0)
		contrib := childLogical.InlineSize() + childMargins.InlineSum()
		return contrib, contrib
	}

	// Mark as computing (cycle detection sentinel).
	ctx.SetOrthogonalComputing(child)

	// Build constraint space for the orthogonal child.
	// The child's inline-size will use the orthogonal fallback (ICB cross-size)
	// when the parent's block-size is indefinite.
	// CSS Writing Modes §4.3: an orthogonal flow always establishes a new BFC,
	// so isChildNewFC is true by default for orthogonal children. Also check
	// the element's own properties (overflow, float, etc.) for completeness.
	isChildNewFC := createsFormattingContext(childStyle) ||
		parentWDM.WM != childWDM.WM
	childSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, isChildNewFC).
		SetOrthogonalFallbackInlineSize(
			orthogonalFallbackSize(childWDM, ctx)).
		SetOrthogonalFallbackBlockSize(space.OrthogonalFallbackBlockSize).
		SetAvailableSize(space.AvailableSize).
		SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
		Build()

	// Lay out the child to get its block-size.
	childResult := layoutElement(ctx, child, childSpace)

	// Cache the result.
	ctx.SetOrthogonalResult(child, childResult)

	// The child's physical size viewed in the parent's logical coordinates:
	// InlineSize() gives the extent in the parent's inline direction,
	// which is the child's block-size (since they're orthogonal).
	childLogical := NewLogicalFragment(parentWDM, childResult.Fragment)
	childMargins := ResolveMargins(childStyle, parentWDM, 0)
	contrib := childLogical.InlineSize() + childMargins.InlineSum()

	return contrib, contrib
}
