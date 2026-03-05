package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// StackingContext represents a CSS stacking context.
// A stacking context is created by certain CSS properties (z-index, opacity, transform, etc.)
// and establishes a new local coordinate system for z-ordering.
type StackingContext struct {
	Box    *Box // The box that creates this context (nil for root)
	ZIndex int  // Z-index value (0 for root and auto)

	// Child stacking contexts organized by z-index
	NegativeZContexts []*StackingContext // z-index < 0, sorted ascending
	ZeroZContexts     []*StackingContext // z-index == 0, document order
	PositiveZContexts []*StackingContext // z-index > 0, sorted ascending
}

// NewStackingContext creates a new stacking context for the given box.
func NewStackingContext(box *Box, zIndex int) *StackingContext {
	return &StackingContext{
		Box:    box,
		ZIndex: zIndex,
	}
}

// AddChildContext adds a child stacking context to the appropriate z-index category.
func (sc *StackingContext) AddChildContext(child *StackingContext) {
	if child.ZIndex < 0 {
		sc.NegativeZContexts = append(sc.NegativeZContexts, child)
	} else if child.ZIndex > 0 {
		sc.PositiveZContexts = append(sc.PositiveZContexts, child)
	} else {
		sc.ZeroZContexts = append(sc.ZeroZContexts, child)
	}
}

// BoxCreatesStackingContext returns true if the box creates a new stacking context.
func BoxCreatesStackingContext(box *Box) bool {
	if box == nil || box.Style == nil {
		return false
	}

	// Text node boxes are not CSS elements and cannot create stacking contexts.
	// Per CSS spec, only elements can create stacking contexts. Text boxes may
	// inherit opacity/other values from their parent element's style, but the
	// stacking context belongs to the element, not the text node.
	if box.Node != nil && box.Node.Type == html.TextNode {
		return false
	}

	// Positioned elements with z-index != auto create a stacking context
	if box.Position == css.PositionAbsolute || box.Position == css.PositionFixed || box.Position == css.PositionRelative || box.Position == css.PositionSticky {
		if zStr, ok := box.Style.Get("z-index"); ok && zStr != "auto" && zStr != "" {
			return true
		}
	}

	// CSS Flexbox §5.4: flex items with z-index != auto create a stacking context,
	// even if position is static.
	if box.Parent != nil && box.Parent.Style != nil {
		if parentDisplay, ok := box.Parent.Style.Get("display"); ok {
			if parentDisplay == "flex" || parentDisplay == "inline-flex" ||
				parentDisplay == "grid" || parentDisplay == "inline-grid" {
				if zStr, ok := box.Style.Get("z-index"); ok && zStr != "auto" && zStr != "" {
					return true
				}
			}
		}
	}

	// Elements with opacity < 1 create a stacking context
	if opacity, ok := box.Style.Get("opacity"); ok && opacity != "1" && opacity != "" {
		return true
	}

	// Elements with transform != none create a stacking context.
	// CSS Transforms Level 1 §1: transform does not apply to non-replaced inline boxes.
	if !isNonReplacedInline(box) {
		if transform, ok := box.Style.Get("transform"); ok && transform != "none" && transform != "" {
			return true
		}
		// Individual transform properties also create a stacking context (CSS Transforms Level 2)
		if _, _, ok := box.Style.GetIndividualScale(); ok {
			return true
		}
		if _, ok := box.Style.GetIndividualRotate(); ok {
			return true
		}
		if _, _, ok := box.Style.GetIndividualTranslate(); ok {
			return true
		}
	}

	// Elements with clip-path != none create a stacking context
	if clipPath, ok := box.Style.Get("clip-path"); ok && clipPath != "none" && clipPath != "" {
		return true
	}

	// Elements with filter != none create a stacking context
	if filter, ok := box.Style.Get("filter"); ok && filter != "none" && filter != "" {
		return true
	}

	// Elements with mix-blend-mode != normal create a stacking context
	if blend, ok := box.Style.Get("mix-blend-mode"); ok && blend != "normal" && blend != "" {
		return true
	}

	// Elements with mask-image != none create a stacking context
	if mask, ok := box.Style.Get("mask-image"); ok && mask != "none" && mask != "" {
		return true
	}
	if mask, ok := box.Style.Get("-webkit-mask-image"); ok && mask != "none" && mask != "" {
		return true
	}

	// Elements with backdrop-filter != none create a stacking context
	if val, ok := box.Style.Get("backdrop-filter"); ok && val != "none" && val != "" {
		return true
	}

	// Elements with isolation: isolate create a stacking context
	if box.Style.GetIsolation() == "isolate" {
		return true
	}

	// will-change creates a stacking context for any property that would create
	// one when its value is non-initial (CSS Will-Change spec §2).
	nonReplacedInline := isNonReplacedInline(box)
	for _, prop := range box.Style.GetWillChange() {
		switch prop {
		case "transform", "perspective", "offset", "offset-path", "transform-style",
			"translate", "rotate", "scale":
			// Transform-related: only create SC for elements that support transforms
			if !nonReplacedInline {
				return true
			}
		case "opacity", "filter", "clip-path", "mix-blend-mode",
			"isolation", "backdrop-filter", "mask", "mask-image",
			"view-transition-name": // view-transition-name always creates a stacking context
			return true
		case "z-index":
			// z-index in will-change creates SC only when z-index would actually apply:
			// positioned elements, flex items, and grid items.
			// Non-positioned non-flex/grid blocks are unaffected (z-index ignored).
			if box.Position != css.PositionStatic {
				return true
			}
			// Check if parent is a flex or grid container (z-index applies to flex/grid items)
			if box.Parent != nil && box.Parent.Style != nil {
				if display, ok := box.Parent.Style.Get("display"); ok {
					if display == "flex" || display == "inline-flex" ||
						display == "grid" || display == "inline-grid" {
						return true
					}
				}
			}
		case "position":
			// will-change: position anticipates a non-static position value.
			// Since sticky and fixed both create stacking contexts, this creates a SC.
			return true
		}
	}

	return false
}

// IsPositioned returns true if the box has position other than static.
func IsPositioned(box *Box) bool {
	if box == nil {
		return false
	}
	return box.Position == css.PositionAbsolute ||
		box.Position == css.PositionFixed ||
		box.Position == css.PositionRelative ||
		box.Position == css.PositionSticky
}

// IsFloat returns true if the box is floated.
func IsFloat(box *Box) bool {
	if box == nil || box.Style == nil {
		return false
	}
	return box.Style.GetFloat() != css.FloatNone
}

// IsInline returns true if the box is inline-level.
func IsInline(box *Box) bool {
	if box == nil {
		return false
	}
	// Text nodes are always inline content
	if box.Node != nil && box.Node.Type == html.TextNode {
		return true
	}
	// Pseudo-element content without explicit display is inline
	if box.PseudoContent != "" && box.Style != nil {
		if _, ok := box.Style.Get("display"); !ok {
			return true
		}
	}
	if box.Style == nil {
		return false
	}
	display, ok := box.Style.Get("display")
	if !ok {
		return false
	}
	return display == "inline" || display == "inline-block" || display == "inline-flex"
}

// BuildStackingContextTree builds the stacking context tree from root boxes.
// This only builds the tree of stacking contexts, not a flat list of all boxes.
// The renderer will traverse the tree and render boxes in the correct order.
func BuildStackingContextTree(roots []*Box) *StackingContext {
	rootCtx := NewStackingContext(nil, 0)

	for _, root := range roots {
		if root == nil {
			continue
		}
		collectChildContexts(root, rootCtx)
	}

	// Sort child contexts by z-index
	sortContexts(rootCtx.NegativeZContexts)
	sortContexts(rootCtx.PositiveZContexts)

	return rootCtx
}

// collectChildContexts finds all stacking contexts in the subtree and adds them to the parent context.
func collectChildContexts(box *Box, parentCtx *StackingContext) {
	if box == nil {
		return
	}

	// If this box creates a new stacking context, add it as a child
	if BoxCreatesStackingContext(box) {
		childCtx := NewStackingContext(box, box.ZIndex)
		parentCtx.AddChildContext(childCtx)

		// Recursively find stacking contexts in this box's children
		for _, child := range box.Children {
			collectChildContexts(child, childCtx)
		}

		// Sort the child context's children
		sortContexts(childCtx.NegativeZContexts)
		sortContexts(childCtx.PositiveZContexts)
		return
	}

	// This box doesn't create a stacking context, so its children
	// belong to the same parent context
	for _, child := range box.Children {
		collectChildContexts(child, parentCtx)
	}
}

// sortContexts sorts stacking contexts by z-index (ascending).
func sortContexts(contexts []*StackingContext) {
	for i := 0; i < len(contexts); i++ {
		for j := i + 1; j < len(contexts); j++ {
			if contexts[j].ZIndex < contexts[i].ZIndex {
				contexts[i], contexts[j] = contexts[j], contexts[i]
			}
		}
	}
}

// GetContextForBox returns the stacking context that should be used when rendering
// the given box. This walks up the parent chain to find the nearest ancestor that
// creates a stacking context.
func GetContextForBox(box *Box, rootCtx *StackingContext) *StackingContext {
	if box == nil {
		return rootCtx
	}

	// Walk up parents to find the stacking context
	current := box.Parent
	for current != nil {
		if BoxCreatesStackingContext(current) {
			// Find the corresponding StackingContext
			return findContext(current, rootCtx)
		}
		current = current.Parent
	}

	return rootCtx
}

// findContext finds the StackingContext for a given box.
func findContext(box *Box, ctx *StackingContext) *StackingContext {
	if ctx.Box == box {
		return ctx
	}

	for _, child := range ctx.NegativeZContexts {
		if found := findContext(box, child); found != nil {
			return found
		}
	}
	for _, child := range ctx.ZeroZContexts {
		if found := findContext(box, child); found != nil {
			return found
		}
	}
	for _, child := range ctx.PositiveZContexts {
		if found := findContext(box, child); found != nil {
			return found
		}
	}

	return nil
}
