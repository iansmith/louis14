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

	// If the node has an explicit inline-size, min = max = that size (content-box).
	if explicitInline, ok := ResolveInlineSize(style, wdm, space, geom); ok {
		return MinMaxSizes{MinContent: explicitInline, MaxContent: explicitInline}
	}

	// Replaced elements (img, canvas, etc.) use ComputeReplacedSize for intrinsic sizing.
	// CSS 2.1 §10.3.2: replaced elements have a single intrinsic inline-size.
	if node.DOMNode != nil && isReplacedElement(node.DOMNode) {
		inlineSize, _ := ComputeReplacedSize(ctx, node, style, space)
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

// measureInlineMinMax computes min/max content sizes for a node with
// only inline-level children by running the line breaker in both modes.
func measureInlineMinMax(node *LayoutInputNode, ctx *LayoutContext, space ConstraintSpace) MinMaxSizes {
	itemsData := CollectInlines(node)
	if len(itemsData.Items) == 0 {
		return MinMaxSizes{}
	}

	fonts := ctx.FontConfig
	wdm := space.WritingDirection

	// Min-content: break at every opportunity.
	minSpace := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 0, BlockSize: Indefinite}).
		SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
		Build()
	minLB := NewLineBreaker(itemsData, ctx, minSpace, fonts, LineBreakerMinContent)
	var minContent float64
	var line LineInfo
	for minLB.NextLine(&line) {
		if line.Width > minContent {
			minContent = line.Width
		}
	}

	// Max-content: never wrap.
	maxSpace := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 1e9, BlockSize: Indefinite}).
		SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
		Build()
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
	if hasOnlyInlineChildren(node) {
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

	var sumMin, sumMax float64
	var maxMin, maxMax float64

	for _, child := range node.Children() {
		if child.IsText() {
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}
		if childStyle.GetDisplay() == css.DisplayNone {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)
		childSpace := NewConstraintSpaceBuilder(wdm, childWDM, false).
			SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, ctx)).
			SetOrthogonalFallbackBlockSize(space.OrthogonalFallbackBlockSize).
			SetAvailableSize(space.AvailableSize).
			SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
			Build()

		childMM := ComputeMinMaxSizes(ctx, child, childSpace)
		childGeom := ComputeFragmentGeometry(childStyle, childWDM)
		childBP := childGeom.InlineBorderPadding()
		childMargins := ResolveMargins(childStyle, childWDM, 0)
		childMin := childMM.MinContent + childBP + childMargins.InlineSum()
		childMax := childMM.MaxContent + childBP + childMargins.InlineSum()

		sumMin += childMin
		sumMax += childMax
		if childMin > maxMin {
			maxMin = childMin
		}
		if childMax > maxMax {
			maxMax = childMax
		}
	}

	if isRow {
		if canWrap {
			// With wrapping, min-content = largest single item; max-content = sum.
			return MinMaxSizes{MinContent: maxMin, MaxContent: sumMax}
		}
		return MinMaxSizes{MinContent: sumMin, MaxContent: sumMax}
	}
	// Column: inline = cross direction → max of items' inline sizes.
	return MinMaxSizes{MinContent: maxMin, MaxContent: maxMax}
}

// measureBlockMinMax computes min/max content sizes for a node with
// block-level children by taking the maximum of each child's sizes.
func measureBlockMinMax(node *LayoutInputNode, ctx *LayoutContext, space ConstraintSpace) MinMaxSizes {
	var result MinMaxSizes

	parentWDM := space.WritingDirection

	for _, child := range node.Children() {
		if child.IsText() {
			continue
		}
		childStyle := child.Style()
		if childStyle == nil {
			continue
		}

		childWDM := NewWritingDirectionMode(childStyle)
		isOrthogonal := parentWDM.IsOrthogonalTo(childWDM)

		if isOrthogonal {
			// Orthogonal child: the parent's inline direction aligns with the
			// child's block direction. We need the child's block-size, which
			// requires actually laying out the child.
			// Mirrors Blink's NGOrthogonalWritingModeRootInlineSize().
			childMin, childMax := measureOrthogonalChild(child, childStyle, childWDM, parentWDM, ctx, space)
			if childMin > result.MinContent {
				result.MinContent = childMin
			}
			if childMax > result.MaxContent {
				result.MaxContent = childMax
			}
		} else {
			// Parallel child: use standard min/max inline-size computation.
			childSpace := NewConstraintSpaceBuilder(parentWDM, childWDM, false).
				SetOrthogonalFallbackInlineSize(
					orthogonalFallbackSize(childWDM, ctx)).
				SetOrthogonalFallbackBlockSize(space.OrthogonalFallbackBlockSize).
				SetAvailableSize(space.AvailableSize).
				SetPercentageResolutionInlineSize(space.PercentageResolutionInlineSize).
				Build()

			childMM := ComputeMinMaxSizes(ctx, child, childSpace)

			childGeom := ComputeFragmentGeometry(childStyle, childWDM)
			childBP := childGeom.InlineBorderPadding()
			childMargins := ResolveMargins(childStyle, childWDM, 0)
			childMin := childMM.MinContent + childBP + childMargins.InlineSum()
			childMax := childMM.MaxContent + childBP + childMargins.InlineSum()

			if childMin > result.MinContent {
				result.MinContent = childMin
			}
			if childMax > result.MaxContent {
				result.MaxContent = childMax
			}
		}
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
