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

	// Positioned elements with z-index != auto create a stacking context
	if box.Position == css.PositionAbsolute || box.Position == css.PositionFixed || box.Position == css.PositionRelative {
		if zStr, ok := box.Style.Get("z-index"); ok && zStr != "auto" && zStr != "" {
			return true
		}
	}

	// Elements with opacity < 1 create a stacking context
	if opacity, ok := box.Style.Get("opacity"); ok && opacity != "1" && opacity != "" {
		return true
	}

	// Elements with transform != none create a stacking context
	if transform, ok := box.Style.Get("transform"); ok && transform != "none" && transform != "" {
		return true
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
		box.Position == css.PositionRelative
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
	return display == "inline" || display == "inline-block"
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
