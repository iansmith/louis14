package layout

import "louis14/pkg/css"

// Phase 4: Containing block logic

// FindContainingBlock finds the containing block for a positioned element
// For absolute positioned elements: nearest positioned ancestor
// For relative/static: parent box
// For fixed: viewport (nil)
func (b *Box) FindContainingBlock() *Box {
	position := b.Style.GetPosition()

	switch position {
	case css.PositionAbsolute:
		// Find nearest positioned ancestor
		return b.findNearestPositionedAncestor()

	case css.PositionFixed:
		// Fixed elements are positioned relative to viewport (return nil)
		return nil

	case css.PositionRelative, css.PositionStatic:
		// Relative and static use parent as containing block
		return b.Parent

	default:
		return b.Parent
	}
}

// findNearestPositionedAncestor finds the nearest ancestor with position != static
func (b *Box) findNearestPositionedAncestor() *Box {
	current := b.Parent

	for current != nil {
		if current.Position != css.PositionStatic {
			return current
		}
		current = current.Parent
	}

	// If no positioned ancestor found, use initial containing block (viewport)
	return nil
}

// IsPositioned returns true if the box has position != static
func (b *Box) IsPositioned() bool {
	return b.Position != css.PositionStatic
}
