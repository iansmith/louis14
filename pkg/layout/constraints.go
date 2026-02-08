package layout

import (
	"louis14/pkg/css"
)

// NewExclusionSpace creates an empty exclusion space.
func NewExclusionSpace() *ExclusionSpace {
	return &ExclusionSpace{
		exclusions: []Exclusion{},
	}
}

// IsEmpty returns true if there are no exclusions.
func (es *ExclusionSpace) IsEmpty() bool {
	return len(es.exclusions) == 0
}

// AvailableInlineSize returns the horizontal offsets from left and right edges
// caused by floats at the given Y position and height.
//
// For a container with width W:
// - leftOffset: distance from left edge (sum of left float widths)
// - rightOffset: distance from right edge (sum of right float widths)
// - Available width = W - leftOffset - rightOffset
func (es *ExclusionSpace) AvailableInlineSize(y, height float64) (leftOffset, rightOffset float64) {
	if es == nil {
		return 0, 0
	}

	// Check each exclusion to see if it intersects the given Y range
	for _, excl := range es.exclusions {
		// Check if exclusion overlaps vertically with [y, y+height]
		exclTop := excl.Rect.Y
		exclBottom := excl.Rect.Y + excl.Rect.Height
		rangeTop := y
		rangeBottom := y + height

		// No overlap if exclusion ends before range starts or starts after range ends
		if exclBottom <= rangeTop || exclTop >= rangeBottom {
			continue
		}

		// Overlaps - add to appropriate offset
		if excl.Side == css.FloatLeft {
			// Left float: extends from left edge
			floatRight := excl.Rect.X + excl.Rect.Width
			if floatRight > leftOffset {
				leftOffset = floatRight
			}
		} else if excl.Side == css.FloatRight {
			// Right float: extends from right edge (excl.Rect.X is already the right edge offset)
			if excl.Rect.Width > rightOffset {
				rightOffset = excl.Rect.Width
			}
		}
	}

	return leftOffset, rightOffset
}

// Add returns a NEW ExclusionSpace with the given exclusion added.
// The original ExclusionSpace is NOT modified (immutability).
//
// This is the key to preventing float accumulation bugs during retry:
// each retry iteration gets a clean copy of the constraint space.
func (es *ExclusionSpace) Add(exclusion Exclusion) *ExclusionSpace {
	// Create new slice with existing exclusions + new one
	newExclusions := make([]Exclusion, len(es.exclusions)+1)
	copy(newExclusions, es.exclusions)
	newExclusions[len(es.exclusions)] = exclusion

	return &ExclusionSpace{
		exclusions: newExclusions,
	}
}

// NewConstraintSpace creates a constraint space with the given available size.
func NewConstraintSpace(width, height float64) *ConstraintSpace {
	return &ConstraintSpace{
		AvailableSize: Size{
			Width:  width,
			Height: height,
		},
		ExclusionSpace: NewExclusionSpace(),
		TextAlign:      css.TextAlignLeft, // Default
	}
}

// WithExclusion returns a NEW ConstraintSpace with the given exclusion added.
// The original ConstraintSpace is NOT modified (immutability).
//
// This is used during line construction when a float is positioned:
// - Position the float
// - Create new constraint with the float added
// - Use new constraint for subsequent content on the line
func (cs *ConstraintSpace) WithExclusion(exclusion Exclusion) *ConstraintSpace {
	return &ConstraintSpace{
		AvailableSize:  cs.AvailableSize,
		ExclusionSpace: cs.ExclusionSpace.Add(exclusion),
		TextAlign:      cs.TextAlign,
	}
}

// WithAvailableWidth returns a NEW ConstraintSpace with modified available width.
func (cs *ConstraintSpace) WithAvailableWidth(width float64) *ConstraintSpace {
	return &ConstraintSpace{
		AvailableSize: Size{
			Width:  width,
			Height: cs.AvailableSize.Height,
		},
		ExclusionSpace: cs.ExclusionSpace,
		TextAlign:      cs.TextAlign,
	}
}

// WithTextAlign returns a NEW ConstraintSpace with modified text alignment.
func (cs *ConstraintSpace) WithTextAlign(align css.TextAlign) *ConstraintSpace {
	return &ConstraintSpace{
		AvailableSize:  cs.AvailableSize,
		ExclusionSpace: cs.ExclusionSpace,
		TextAlign:      align,
	}
}

// AvailableInlineSize returns the available inline size at the given Y position and height,
// accounting for exclusions (floats).
func (cs *ConstraintSpace) AvailableInlineSize(y, height float64) float64 {
	leftOffset, rightOffset := cs.ExclusionSpace.AvailableInlineSize(y, height)
	return cs.AvailableSize.Width - leftOffset - rightOffset
}

// constraintsChanged checks if the constraint space changed during fragment construction.
// This is used to determine if we need to retry line breaking.
//
// Returns true if:
// - Floats were added (exclusion space changed)
// - Any other constraints changed (future extensions)
//
// This is the key to the retry logic: if Phase 3 added floats that affect
// line breaking, we need to re-run Phase 2 with the updated constraints.
func constraintsChanged(original, final *ConstraintSpace, lines []*LineInfo) bool {
	// Check if exclusion space changed (floats were added)
	originalEmpty := original.ExclusionSpace.IsEmpty()
	finalEmpty := final.ExclusionSpace.IsEmpty()

	// If original was empty and final is not, definitely changed
	if originalEmpty && !finalEmpty {
		return true
	}

	// If both non-empty, check if they're different
	// For now, we use a simple heuristic: check available width at first line
	if !originalEmpty && !finalEmpty && len(lines) > 0 {
		firstLineY := lines[0].Y
		firstLineHeight := lines[0].Height

		originalWidth := original.AvailableInlineSize(firstLineY, firstLineHeight)
		finalWidth := final.AvailableInlineSize(firstLineY, firstLineHeight)

		// If available width changed, constraints changed
		if originalWidth != finalWidth {
			return true
		}
	}

	// TODO: In the future, check other constraint changes:
	// - Available size changes
	// - Text alignment changes
	// - etc.

	return false
}
