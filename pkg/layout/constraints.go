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

// NextBandBelowY returns the nearest Y position below the given Y where
// the available width changes (i.e., below the bottom of a float that
// overlaps the given Y). Returns -1 if no floats overlap at this Y.
// This implements CSS 2.1 §9.5: "the line box is shifted downward"
func (es *ExclusionSpace) NextBandBelowY(y, height float64) float64 {
	if es == nil {
		return -1
	}

	nextY := -1.0
	for _, excl := range es.exclusions {
		exclBottom := excl.Rect.Y + excl.Rect.Height
		// Only consider floats that overlap with [y, y+height]
		if exclBottom <= y || excl.Rect.Y >= y+height {
			continue
		}
		// Find the nearest float bottom above
		if nextY < 0 || exclBottom < nextY {
			nextY = exclBottom
		}
	}
	return nextY
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

// NewConstraintSpaceDir creates a constraint space with Dir-awareness.
// For horizontal-tb, inlineSize maps to Width and blockSize to Height.
// For vertical modes, inlineSize maps to Height and blockSize to Width.
func NewConstraintSpaceDir(inlineSize, blockSize float64, dir Dir) *ConstraintSpace {
	var w, h float64
	if dir.IsVertical() {
		w = blockSize
		h = inlineSize
	} else {
		w = inlineSize
		h = blockSize
	}
	return &ConstraintSpace{
		AvailableSize: Size{
			Width:  w,
			Height: h,
		},
		ExclusionSpace: NewExclusionSpace(),
		TextAlign:      css.TextAlignLeft,
		Dir:            dir,
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
		NoWrap:         cs.NoWrap,
		TextIndent:     cs.TextIndent,
		TextOverflow:   cs.TextOverflow,
		WordBreak:      cs.WordBreak,
		OverflowWrap:   cs.OverflowWrap,
		LineClampN:     cs.LineClampN,
		TextWrap:       cs.TextWrap,
		Dir:            cs.Dir,
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
		NoWrap:         cs.NoWrap,
		TextIndent:     cs.TextIndent,
		TextOverflow:   cs.TextOverflow,
		WordBreak:      cs.WordBreak,
		OverflowWrap:   cs.OverflowWrap,
		LineClampN:     cs.LineClampN,
		TextWrap:       cs.TextWrap,
		Dir:            cs.Dir,
	}
}

// WithTextAlign returns a NEW ConstraintSpace with modified text alignment.
func (cs *ConstraintSpace) WithTextAlign(align css.TextAlign) *ConstraintSpace {
	return &ConstraintSpace{
		AvailableSize:  cs.AvailableSize,
		ExclusionSpace: cs.ExclusionSpace,
		TextAlign:      align,
		NoWrap:         cs.NoWrap,
		TextIndent:     cs.TextIndent,
		TextOverflow:   cs.TextOverflow,
		WordBreak:      cs.WordBreak,
		OverflowWrap:   cs.OverflowWrap,
		LineClampN:     cs.LineClampN,
		TextWrap:       cs.TextWrap,
		Dir:            cs.Dir,
	}
}

// AvailableInlineSize returns the available inline size at the given Y position and height,
// accounting for exclusions (floats).
//
// NOTE: This method always operates in horizontal-TB space (block=Y, inline=X)
// regardless of the Dir setting. This is because the inline layout pipeline currently
// operates in h-tb logical space, with vertical transformation applied post-layout.
// Use AvailableInlineSizeDir() for direction-aware queries.
func (cs *ConstraintSpace) AvailableInlineSize(y, height float64) float64 {
	leftOffset, rightOffset := cs.ExclusionSpace.AvailableInlineSize(y, height)
	return cs.AvailableSize.Width - leftOffset - rightOffset
}

// InlineAvailableSize returns the available inline size based on the embedded Dir,
// at the given block-axis position and inline extent.
//
// This is the Dir-aware version of AvailableInlineSize:
// - For horizontal-tb: uses Width, block=Y, inline=X
// - For vertical modes: uses Height, block=X, inline=Y
func (cs *ConstraintSpace) InlineAvailableSize(blockPos, inlineExtent float64) float64 {
	return cs.AvailableInlineSizeDir(blockPos, inlineExtent, cs.Dir)
}

// BaseInlineSize returns the base available inline size from the embedded Dir
// without accounting for exclusions.
func (cs *ConstraintSpace) BaseInlineSize() float64 {
	if cs.Dir.IsVertical() {
		return cs.AvailableSize.Height
	}
	return cs.AvailableSize.Width
}

// AvailableInlineSizeDir returns the available inline size at the given block-axis position,
// accounting for exclusions (floats).
//
// For horizontal (dir=DirHorizontal): same as AvailableInlineSize — returns available width.
// For vertical (dir=DirVerticalRL/LR): returns available height at the given X position.
func (cs *ConstraintSpace) AvailableInlineSizeDir(blockPos, inlineExtent float64, dir Dir) float64 {
	if !dir.IsVertical() {
		return cs.AvailableInlineSize(blockPos, inlineExtent)
	}
	// In vertical mode, the available inline size is the available height
	// minus any start/end offsets from floats at this block position (X).
	startOffset, endOffset := cs.ExclusionSpace.AvailableInlineSizeDir(blockPos, inlineExtent, dir)
	return cs.AvailableSize.Height - startOffset - endOffset
}

// AvailableInlineSizeDir returns start and end offsets along the inline axis
// caused by float exclusions at the given block-axis position.
//
// For horizontal: same as AvailableInlineSize (block=Y, inline=X).
// For vertical: block=X, inline=Y. Returns (topOffset, bottomOffset).
func (es *ExclusionSpace) AvailableInlineSizeDir(blockPos, inlineExtent float64, dir Dir) (startOffset, endOffset float64) {
	if !dir.IsVertical() {
		return es.AvailableInlineSize(blockPos, inlineExtent)
	}
	if es == nil {
		return 0, 0
	}

	// In vertical mode, exclusions' Rect.X is the block-start position and
	// Rect.Width is the block extent. We check for overlap along the block axis (X).
	// Rect.Y is the inline position and Rect.Height is the inline extent.
	for _, excl := range es.exclusions {
		exclBlockStart := excl.Rect.X
		exclBlockEnd := excl.Rect.X + excl.Rect.Width
		rangeBlockStart := blockPos
		rangeBlockEnd := blockPos + inlineExtent // this is the block-extent for the query

		if exclBlockEnd <= rangeBlockStart || exclBlockStart >= rangeBlockEnd {
			continue
		}

		if excl.Side == css.FloatLeft {
			// float:left = inline-start (top)
			floatInlineEnd := excl.Rect.Y + excl.Rect.Height
			if floatInlineEnd > startOffset {
				startOffset = floatInlineEnd
			}
		} else if excl.Side == css.FloatRight {
			// float:right = inline-end (bottom)
			if excl.Rect.Height > endOffset {
				endOffset = excl.Rect.Height
			}
		}
	}

	return startOffset, endOffset
}

// NextBandBelowDir returns the nearest block-axis position beyond the given position where
// the available inline size changes.
//
// For horizontal: same as NextBandBelowY.
// For vertical: returns the next X position where a float exclusion ends.
func (es *ExclusionSpace) NextBandBelowDir(blockPos, inlineExtent float64, dir Dir) float64 {
	if !dir.IsVertical() {
		return es.NextBandBelowY(blockPos, inlineExtent)
	}
	if es == nil {
		return -1
	}

	nextBlockPos := -1.0
	for _, excl := range es.exclusions {
		exclBlockEnd := excl.Rect.X + excl.Rect.Width
		if exclBlockEnd <= blockPos || excl.Rect.X >= blockPos+inlineExtent {
			continue
		}
		if nextBlockPos < 0 || exclBlockEnd < nextBlockPos {
			nextBlockPos = exclBlockEnd
		}
	}
	return nextBlockPos
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
