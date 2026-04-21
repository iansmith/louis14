package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// Phase 4: Containing block logic

// isNonReplacedInline returns true if box is a non-replaced inline element
// (e.g. <span>, <em>, <a>). CSS Transforms Level 1 §1 states that the
// transform property does NOT apply to non-replaced inline boxes, so
// transform-related properties cannot create a containing block on them.
func isNonReplacedInline(box *Box) bool {
	if box == nil || box.Style == nil || box.Node == nil {
		return false
	}
	if box.Node.Type == html.TextNode {
		return false
	}
	d := box.Style.GetDisplay()
	if d != css.DisplayInline {
		return false
	}
	// Replaced elements (transforms apply normally to these even when inline)
	switch box.Node.TagName {
	case "img", "input", "video", "audio", "canvas", "object",
		"embed", "iframe", "svg", "math", "picture":
		return false
	}
	return true
}

// FindContainingBlock finds the containing block for a positioned element.
// For absolute: nearest ancestor that is positioned OR has transform/filter/perspective/will-change.
// For fixed: nearest ancestor with transform/filter/perspective/backdrop-filter/will-change (or viewport).
// For relative/static/sticky: parent box.
func (b *Box) FindContainingBlock() *Box {
	position := b.Style.GetPosition()

	switch position {
	case css.PositionAbsolute:
		return b.findContainingBlockForAbsPos()

	case css.PositionFixed:
		// Fixed elements are positioned relative to the viewport UNLESS an ancestor
		// has transform/filter/perspective/backdrop-filter or will-change with those values,
		// which creates a local containing block (per CSS Transforms/Filters specs).
		return b.findContainingBlockForFixedPos()

	case css.PositionRelative, css.PositionStatic, css.PositionSticky:
		return b.Parent

	default:
		return b.Parent
	}
}

// findContainingBlockForAbsPos finds the nearest ancestor that creates a containing
// block for absolutely positioned elements.
func (b *Box) findContainingBlockForAbsPos() *Box {
	current := b.Parent
	for current != nil {
		if boxIsContainingBlockForAbsPos(current) {
			return current
		}
		current = current.Parent
	}
	return nil // viewport (initial containing block)
}

// findContainingBlockForFixedPos finds the nearest ancestor that traps fixed
// positioning (turning it into absolute-like within that ancestor).
func (b *Box) findContainingBlockForFixedPos() *Box {
	current := b.Parent
	for current != nil {
		if boxIsContainingBlockForFixedPos(current) {
			return current
		}
		current = current.Parent
	}
	return nil // viewport
}

// boxIsContainingBlockForAbsPos returns true if box creates a containing block
// for absolutely positioned descendants. This is true when:
//   - position != static (the classic case), OR
//   - transform/filter/perspective is non-none, OR
//   - will-change lists transform, filter, perspective, position, contain, etc.
func boxIsContainingBlockForAbsPos(box *Box) bool {
	if box == nil || box.Style == nil {
		return false
	}
	// Classic: any non-static position
	if box.Position != css.PositionStatic {
		return true
	}
	// CSS Transforms Level 1 §1: transform does not apply to non-replaced inline boxes.
	// Skip all transform-related checks for such elements.
	nonReplacedInline := isNonReplacedInline(box)
	if !nonReplacedInline {
		// transform != none (CSS Transforms spec)
		if t, ok := box.Style.Get("transform"); ok && t != "none" && t != "" {
			return true
		}
		// Individual transform properties
		if _, _, _, _, ok := box.Style.GetIndividualTranslate(); ok {
			return true
		}
		if _, ok := box.Style.GetIndividualRotate(); ok {
			return true
		}
		if _, _, ok := box.Style.GetIndividualScale(); ok {
			return true
		}
	}
	// filter != none (CSS Filter Effects spec — applies to all elements)
	if f, ok := box.Style.Get("filter"); ok && f != "none" && f != "" {
		return true
	}
	// perspective != none (CSS Transforms spec)
	if !nonReplacedInline {
		if p, ok := box.Style.Get("perspective"); ok && p != "none" && p != "" {
			return true
		}
	}
	// backdrop-filter != none (CSS Filter Effects Level 2)
	if bf, ok := box.Style.Get("backdrop-filter"); ok && bf != "none" && bf != "" {
		return true
	}
	// contain: layout/paint/strict/content (CSS Contain spec)
	if c, ok := box.Style.Get("contain"); ok {
		switch c {
		case "layout", "paint", "strict", "content":
			return true
		}
	}
	// will-change creates a containing block when specifying a property that would
	// establish one (CSS Will-Change spec §2)
	for _, prop := range box.Style.GetWillChange() {
		switch prop {
		case "transform", "translate", "rotate", "scale", "offset", "offset-path",
			"transform-style", "-webkit-perspective":
			// Transform-related: skip for non-replaced inlines
			if !nonReplacedInline {
				return true
			}
		case "filter", "perspective", "backdrop-filter", "position", "contain":
			return true
		}
	}
	return false
}

// boxIsContainingBlockForFixedPos returns true if box creates a containing block
// that traps fixed positioned descendants (making them behave like absolute).
// Per spec: transform, filter, perspective, backdrop-filter, and will-change
// with those values create a containing block for fixed positioned elements.
func boxIsContainingBlockForFixedPos(box *Box) bool {
	if box == nil || box.Style == nil {
		return false
	}
	// CSS Transforms Level 1 §1: transform does not apply to non-replaced inline boxes.
	// Skip all transform-related checks for such elements.
	nonReplacedInline := isNonReplacedInline(box)
	if !nonReplacedInline {
		// transform != none
		if t, ok := box.Style.Get("transform"); ok && t != "none" && t != "" {
			return true
		}
		// Individual transform properties
		if _, _, _, _, ok := box.Style.GetIndividualTranslate(); ok {
			return true
		}
		if _, ok := box.Style.GetIndividualRotate(); ok {
			return true
		}
		if _, _, ok := box.Style.GetIndividualScale(); ok {
			return true
		}
	}
	// filter != none (applies to all elements)
	if f, ok := box.Style.Get("filter"); ok && f != "none" && f != "" {
		return true
	}
	// perspective != none
	if !nonReplacedInline {
		if p, ok := box.Style.Get("perspective"); ok && p != "none" && p != "" {
			return true
		}
	}
	// backdrop-filter != none
	if bf, ok := box.Style.Get("backdrop-filter"); ok && bf != "none" && bf != "" {
		return true
	}
	// contain: layout/paint/strict/content
	if c, ok := box.Style.Get("contain"); ok {
		switch c {
		case "layout", "paint", "strict", "content":
			return true
		}
	}
	// will-change with relevant properties (position does NOT trap fixed-pos elements)
	for _, prop := range box.Style.GetWillChange() {
		switch prop {
		case "transform", "translate", "rotate", "scale", "offset", "offset-path",
			"transform-style", "-webkit-perspective":
			// Transform-related: skip for non-replaced inlines
			if !nonReplacedInline {
				return true
			}
		case "filter", "perspective", "backdrop-filter", "contain":
			return true
		}
	}
	return false
}

// IsPositioned returns true if the box has position != static
func (b *Box) IsPositioned() bool {
	return b.Position != css.PositionStatic
}
