package layout

// ConstraintSpace is the immutable input from parent to child during layout.
// It carries all constraints in the CHILD's writing mode — the parent handles
// the conversion when constructing it.
//
// Ported from Blink's ConstraintSpace (constraint_space.h).
type ConstraintSpace struct {
	// AvailableSize is the space available for the child's margin box,
	// in the child's logical coordinates.
	// InlineSize is always definite. BlockSize may be Indefinite.
	AvailableSize LogicalSize

	// PercentageResolutionSize is the size used for resolving percentage lengths.
	// Typically the containing block's content size in the child's writing mode.
	PercentageResolutionSize LogicalSize

	// WritingDirection is the child's writing direction mode.
	WritingDirection WritingDirectionMode

	// IsNewFormattingContext is true if this child establishes a new BFC.
	// When true, the child gets a fresh ExclusionSpace and margins don't
	// collapse across the boundary.
	IsNewFormattingContext bool

	// IsOrthogonalWritingModeRoot is true when the child has a different
	// inline axis than the parent. This is set automatically by the builder.
	IsOrthogonalWritingModeRoot bool

	// IsFixedInlineSize is true when the inline-size is predetermined
	// (e.g., from an explicit CSS width in HTB). The layout algorithm
	// should use AvailableSize.InlineSize as the exact content inline-size.
	IsFixedInlineSize bool

	// IsFixedBlockSize is true when the block-size is predetermined.
	IsFixedBlockSize bool

	// ExclusionSpace tracks floats that the child must flow around.
	// Nil means no floats to avoid (or new BFC).
	ExclusionSpace *ExclusionSpace
}

// Indefinite is the sentinel value for an unconstrained block-size.
// Matches Blink's kIndefiniteSize.
const Indefinite float64 = -1

// IsBlockSizeIndefinite returns true if the block-size is unconstrained.
func (cs ConstraintSpace) IsBlockSizeIndefinite() bool {
	return cs.AvailableSize.BlockSize < 0
}

// ConstraintSpaceBuilder constructs a ConstraintSpace for a child element.
// It handles the writing-mode conversion automatically: when the child has
// an orthogonal writing mode, inline/block axes are swapped.
//
// Ported from Blink's ConstraintSpaceBuilder (constraint_space_builder.h).
type ConstraintSpaceBuilder struct {
	parentWDM WritingDirectionMode
	childWDM  WritingDirectionMode
	parallel  bool // true if parent and child share the same inline axis

	space ConstraintSpace
}

// NewConstraintSpaceBuilder creates a builder for constructing a child's
// ConstraintSpace. The parent's writing direction and the child's writing
// direction are used to determine if axes need swapping.
func NewConstraintSpaceBuilder(parentWDM, childWDM WritingDirectionMode, isNewFC bool) *ConstraintSpaceBuilder {
	b := &ConstraintSpaceBuilder{
		parentWDM: parentWDM,
		childWDM:  childWDM,
		parallel:  !parentWDM.IsOrthogonalTo(childWDM),
	}
	b.space.WritingDirection = childWDM
	b.space.IsNewFormattingContext = isNewFC
	b.space.IsOrthogonalWritingModeRoot = !b.parallel
	return b
}

// SetAvailableSize sets the available size for the child.
// The size is in the PARENT's logical coordinates — the builder swaps
// inline/block when the child is orthogonal.
func (b *ConstraintSpaceBuilder) SetAvailableSize(size LogicalSize) *ConstraintSpaceBuilder {
	if b.parallel {
		b.space.AvailableSize = size
	} else {
		// Orthogonal: parent's inline becomes child's block, and vice versa.
		b.space.AvailableSize = LogicalSize{
			InlineSize: size.BlockSize,
			BlockSize:  size.InlineSize,
		}
	}
	return b
}

// SetPercentageResolutionSize sets the percentage resolution base.
// The size is in the PARENT's logical coordinates.
func (b *ConstraintSpaceBuilder) SetPercentageResolutionSize(size LogicalSize) *ConstraintSpaceBuilder {
	if b.parallel {
		b.space.PercentageResolutionSize = size
	} else {
		b.space.PercentageResolutionSize = LogicalSize{
			InlineSize: size.BlockSize,
			BlockSize:  size.InlineSize,
		}
	}
	return b
}

// SetIsFixedInlineSize marks the inline-size as predetermined.
func (b *ConstraintSpaceBuilder) SetIsFixedInlineSize(fixed bool) *ConstraintSpaceBuilder {
	b.space.IsFixedInlineSize = fixed
	return b
}

// SetIsFixedBlockSize marks the block-size as predetermined.
func (b *ConstraintSpaceBuilder) SetIsFixedBlockSize(fixed bool) *ConstraintSpaceBuilder {
	b.space.IsFixedBlockSize = fixed
	return b
}

// SetExclusionSpace sets the float exclusion space.
// For new formatting contexts, this should be nil (fresh exclusion space).
func (b *ConstraintSpaceBuilder) SetExclusionSpace(es *ExclusionSpace) *ConstraintSpaceBuilder {
	if !b.space.IsNewFormattingContext {
		b.space.ExclusionSpace = es
	}
	return b
}

// Build returns the finished ConstraintSpace.
func (b *ConstraintSpaceBuilder) Build() ConstraintSpace {
	return b.space
}
