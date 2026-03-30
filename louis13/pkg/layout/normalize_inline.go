package layout

// normalizeBlocksInInline implements CSS 2.1 §9.2.1.1 tree normalization.
//
// When an inline element contains block-level children, the CSS spec requires
// splitting the inline element around each block child and wrapping the pieces
// in anonymous block boxes. This pre-pass transforms the DOM so that the
// standard inline layout pipeline never encounters inline-contains-block.
//
// Example (no position):
//
//	<span>before<div>block</div>after</span>
//
// becomes:
//
//	[_anon block]<span data-block-fragment="first">before</span>[/_anon block]
//	<div>block</div>
//	[_anon block]<span data-block-fragment="last">after</span>[/_anon block]
//
// Example (with position:relative on the inline):
//
//	<span style="position:relative;top:2in">before<div>block</div>after</span>
//
// becomes:
//
//	[_anon block position:relative;top:2in]
//	  [_anon block]<span data-block-fragment="first">before</span>[/_anon block]
//	  <div>block</div>
//	  [_anon block]<span data-block-fragment="last">after</span>[/_anon block]
//	[/_anon block]
//
// The data-block-fragment attribute drives box.IsFirstFragment / box.IsLastFragment
// for correct border suppression in the renderer.
//
// Synthetic node styles live in le.syntheticStyles and are consulted by
// collectInlineItemsClean and layoutNode before calling css.ComputeStyle.

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// isBlockLevelDisplay returns true for CSS display values that produce block-level boxes.
func isBlockLevelDisplay(d css.DisplayType) bool {
	switch d {
	case css.DisplayBlock, css.DisplayFlowRoot, css.DisplayListItem, css.DisplayTable,
		css.DisplayFlex, css.DisplayGrid, css.DisplayTableRow,
		css.DisplayTableCell, css.DisplayTableRowGroup,
		css.DisplayTableHeaderGroup, css.DisplayTableFooterGroup,
		css.DisplayTableCaption:
		return true
	}
	return false
}

// getSyntheticOrComputeStyle returns the style for node, consulting syntheticStyles
// first, then computedStyles, then the CSS cascade.
func (le *LayoutEngine) getSyntheticOrComputeStyle(node *html.Node, computedStyles map[*html.Node]*css.Style) *css.Style {
	if node.Type == html.TextNode {
		return nil
	}
	if style, ok := le.syntheticStyles[node]; ok {
		return style
	}
	if style, ok := computedStyles[node]; ok {
		return style
	}
	style := css.ComputeStyleWithLogical(node, le.stylesheets, le.viewport.width, le.viewport.height)
	return style
}

// inlineHasBlockDescendant returns true if node (display:inline) has any block-level
// descendant among its direct or deeper children.
func (le *LayoutEngine) inlineHasBlockDescendant(node *html.Node, computedStyles map[*html.Node]*css.Style) bool {
	for _, child := range node.Children {
		if child.Type == html.TextNode {
			continue
		}
		childStyle := le.getSyntheticOrComputeStyle(child, computedStyles)
		if childStyle != nil && isBlockLevelDisplay(childStyle.GetDisplay()) {
			return true
		}
		if le.inlineHasBlockDescendant(child, computedStyles) {
			return true
		}
	}
	return false
}

// makeAnonBlock creates an anonymous block wrapper node with the given children
// and records display:block in le.syntheticStyles.
func (le *LayoutEngine) makeAnonBlock(children []*html.Node) *html.Node {
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "_anon",
	}
	node.Children = make([]*html.Node, len(children))
	for i, child := range children {
		node.Children[i] = child
		child.Parent = node
	}
	style := css.NewStyle()
	style.Set("display", "block")
	le.syntheticStyles[node] = style
	return node
}

// makeAnonBlockWithStyle creates an anonymous block wrapper with a custom style.
func (le *LayoutEngine) makeAnonBlockWithStyle(children []*html.Node, style *css.Style) *html.Node {
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "_anon",
	}
	node.Children = make([]*html.Node, len(children))
	for i, child := range children {
		node.Children[i] = child
		child.Parent = node
	}
	le.syntheticStyles[node] = style
	return node
}

// cloneStyleWithoutPosition returns a new Style that copies all properties from
// src except position-related ones (position, top, left, right, bottom).
// Used when the outer anonymous wrapper handles the relative positioning, so
// the clone itself should not double-apply the offset.
func cloneStyleWithoutPosition(src *css.Style) *css.Style {
	dst := css.NewStyle()
	skip := map[string]bool{
		"position": true,
		"top":      true,
		"left":     true,
		"right":    true,
		"bottom":   true,
	}
	for k, v := range src.Properties {
		if !skip[k] {
			dst.Set(k, v)
		}
	}
	return dst
}

// splitInlineAroundBlocks splits an inline element around its block-level children.
//
// For a plain inline (no positioning):
//
//	Returns: [anonBlock[clone-before]?, blockChild, anonBlock[clone-after]?, ...]
//
// For an inline with position:relative:
//
//	Returns: [outerAnonBlock(position:relative){anonBlock[clone-before]?, blockChild, anonBlock[clone-after]?, ...}]
//
// Each inline clone gets a "data-block-fragment" attribute ("first", "last", or "middle")
// so that box creation in LayoutInlineContentToBoxes sets IsFirstFragment / IsLastFragment.
func (le *LayoutEngine) splitInlineAroundBlocks(inlineNode *html.Node, computedStyles map[*html.Node]*css.Style) []*html.Node {
	type segment struct {
		children  []*html.Node // inline content; nil blockNode means this is an inline run
		blockNode *html.Node   // non-nil when this segment IS a block child
	}

	var segments []segment
	var currentChildren []*html.Node

	for _, child := range inlineNode.Children {
		if child.Type == html.TextNode {
			currentChildren = append(currentChildren, child)
			continue
		}
		childStyle := le.getSyntheticOrComputeStyle(child, computedStyles)
		if childStyle != nil && isBlockLevelDisplay(childStyle.GetDisplay()) {
			segments = append(segments, segment{children: currentChildren})
			segments = append(segments, segment{blockNode: child})
			currentChildren = nil
		} else {
			currentChildren = append(currentChildren, child)
		}
	}
	segments = append(segments, segment{children: currentChildren})

	// Count non-empty inline segments to assign fragment types.
	nonEmptyInline := 0
	for _, seg := range segments {
		if seg.blockNode == nil && len(seg.children) > 0 {
			nonEmptyInline++
		}
	}

	origStyle := le.getSyntheticOrComputeStyle(inlineNode, computedStyles)

	// If the inline has position:relative, the outer anonymous block will carry
	// the positioning; the clone styles must not re-apply it.
	hasRelPos := origStyle != nil && origStyle.GetPosition() == css.PositionRelative
	var cloneBaseStyle *css.Style
	if hasRelPos {
		cloneBaseStyle = cloneStyleWithoutPosition(origStyle)
	} else {
		cloneBaseStyle = origStyle
	}

	// Build the inner parts (anon wrappers around clones + block children).
	var innerParts []*html.Node
	inlineSegmentIdx := 0

	for _, seg := range segments {
		if seg.blockNode != nil {
			innerParts = append(innerParts, seg.blockNode)
			continue
		}
		if len(seg.children) == 0 {
			continue
		}
		inlineSegmentIdx++

		isFirst := inlineSegmentIdx == 1
		isLast := inlineSegmentIdx == nonEmptyInline
		var fragType string
		switch {
		case isFirst && isLast:
			fragType = "" // sole fragment — keep full borders
		case isFirst:
			fragType = "first" // suppress right border
		case isLast:
			fragType = "last" // suppress left border
		default:
			fragType = "middle" // suppress both borders
		}

		// Clone the inline node with only this segment's children.
		clone := &html.Node{
			Type:    inlineNode.Type,
			TagName: inlineNode.TagName,
		}
		clone.Children = make([]*html.Node, len(seg.children))
		for i, child := range seg.children {
			clone.Children[i] = child
			child.Parent = clone
		}
		clone.Attributes = make(map[string]string)
		for k, v := range inlineNode.Attributes {
			clone.Attributes[k] = v
		}
		if fragType != "" {
			clone.Attributes["data-block-fragment"] = fragType
		}

		// Register clone's style (without position if outer wrapper handles it).
		// For block-in-inline fragments, suppress the borders that should not appear:
		//   "first" fragment → no right border (continuation opens on left, closes on right is suppressed)
		//   "last"  fragment → no left border  (continuation opens without left border)
		//   "middle"         → no left or right border
		// This ensures layout does not advance currentX for suppressed borders,
		// matching the reference rendering where those borders simply don't exist.
		if cloneBaseStyle != nil {
			suppressLeft := fragType == "last" || fragType == "middle"
			suppressRight := fragType == "first" || fragType == "middle"
			if suppressLeft || suppressRight {
				cloneStyle := css.NewStyle()
				for k, v := range cloneBaseStyle.Properties {
					cloneStyle.Set(k, v)
				}
				if suppressLeft {
					cloneStyle.Set("border-left-width", "0")
					cloneStyle.Set("border-left-style", "none")
				}
				if suppressRight {
					cloneStyle.Set("border-right-width", "0")
					cloneStyle.Set("border-right-style", "none")
				}
				le.syntheticStyles[clone] = cloneStyle
			} else {
				le.syntheticStyles[clone] = cloneBaseStyle
			}
		}

		// Create the anonymous block wrapper.
		// When the inline has NO position:relative (hasRelPos=false), propagate the
		// inline element's background-color so it covers the full anonymous block width,
		// matching real browser behavior for block-in-inline splits (CSS 2.1 §9.2.1.1).
		// When hasRelPos=true, the outer anonymous block handles positioning and the
		// inner anon blocks are transparent structural wrappers (matching browser behavior
		// where the inline background only paints at the inline content width).
		anonStyle := css.NewStyle()
		anonStyle.Set("display", "block")
		if !hasRelPos && origStyle != nil {
			if bg, ok := origStyle.Get("background-color"); ok && bg != "" && bg != "transparent" {
				anonStyle.Set("background-color", bg)
			}
		}
		innerParts = append(innerParts, le.makeAnonBlockWithStyle([]*html.Node{clone}, anonStyle))
	}

	// If position:relative, wrap all inner parts in one outer block that carries the offset.
	if hasRelPos {
		outerStyle := css.NewStyle()
		outerStyle.Set("display", "block")
		outerStyle.Set("position", "relative")
		for _, prop := range []string{"top", "left", "right", "bottom", "z-index"} {
			if v, ok := origStyle.Get(prop); ok {
				outerStyle.Set(prop, v)
			}
		}
		outerAnon := le.makeAnonBlockWithStyle(innerParts, outerStyle)
		return []*html.Node{outerAnon}
	}

	return innerParts
}

// normalizeBlocksInInline walks children and replaces any inline element that
// contains block-level descendants with anonymous-block wrappers (CSS 2.1 §9.2.1.1).
// Returns the (possibly modified) children slice; unchanged if no splits were needed.
//
// Styles for new synthetic nodes are stored in le.syntheticStyles.
func (le *LayoutEngine) normalizeBlocksInInline(children []*html.Node, computedStyles map[*html.Node]*css.Style) []*html.Node {
	var result []*html.Node
	changed := false

	for _, child := range children {
		if child.Type == html.TextNode {
			result = append(result, child)
			continue
		}
		childStyle := le.getSyntheticOrComputeStyle(child, computedStyles)
		if childStyle == nil {
			result = append(result, child)
			continue
		}

		if childStyle.GetDisplay() == css.DisplayInline && le.inlineHasBlockDescendant(child, computedStyles) {
			parts := le.splitInlineAroundBlocks(child, computedStyles)
			result = append(result, parts...)
			changed = true
		} else {
			result = append(result, child)
		}
	}

	if !changed {
		return children
	}
	return result
}
