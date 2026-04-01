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

	// IsInsideFlexibleBox is true when this child is a direct flex item.
	// Used by §4.5 to compute the automatic minimum size (min-content instead of 0
	// for min-width:auto), and by ResolveBlockSize to suppress percentage resolution
	// on the first layout pass before the cross-size is determined.
	IsInsideFlexibleBox bool

	// IsFixedBlockSizeIndefinite is true when IsFixedBlockSize is true but the
	// block-size comes from the first-pass flex layout rather than a real definite size.
	// Children should treat percentage block-sizes as auto (resolve to 0) on this pass.
	IsFixedBlockSizeIndefinite bool

	// OrthogonalFallbackInlineSize is the ICB size used when an orthogonal
	// child would otherwise get Indefinite as its available inline-size.
	// Per CSS Writing Modes §10.3.2, when the parent's block-size is indefinite,
	// an orthogonal child falls back to the ICB size.
	OrthogonalFallbackInlineSize float64

	// ForcedMinBlockSize is a minimum block-size forced by the parent.
	// Used for the root element to ensure it fills at least the ICB block-size.
	ForcedMinBlockSize float64
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
		// If parent's block-size was indefinite, the child's inline-size
		// becomes indefinite. Fall back to the ICB size per §10.3.2.
		if b.space.AvailableSize.InlineSize == Indefinite &&
			b.space.OrthogonalFallbackInlineSize > 0 {
			b.space.AvailableSize.InlineSize = b.space.OrthogonalFallbackInlineSize
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
// For orthogonal children, "inline" from the parent's perspective becomes
// the child's block axis, so this sets IsFixedBlockSize in the child's space.
func (b *ConstraintSpaceBuilder) SetIsFixedInlineSize(fixed bool) *ConstraintSpaceBuilder {
	if b.parallel {
		b.space.IsFixedInlineSize = fixed
	} else {
		// Orthogonal: parent's inline axis = child's block axis.
		b.space.IsFixedBlockSize = fixed
	}
	return b
}

// SetIsFixedBlockSize marks the block-size as predetermined.
// For orthogonal children, "block" from the parent's perspective becomes
// the child's inline axis, so this sets IsFixedInlineSize in the child's space.
func (b *ConstraintSpaceBuilder) SetIsFixedBlockSize(fixed bool) *ConstraintSpaceBuilder {
	if b.parallel {
		b.space.IsFixedBlockSize = fixed
	} else {
		// Orthogonal: parent's block axis = child's inline axis.
		b.space.IsFixedInlineSize = fixed
	}
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

// SetIsInsideFlexibleBox marks the child as a direct flex item.
func (b *ConstraintSpaceBuilder) SetIsInsideFlexibleBox(v bool) *ConstraintSpaceBuilder {
	b.space.IsInsideFlexibleBox = v
	return b
}

// SetIsFixedBlockSizeIndefinite marks this as a first-pass flex layout where
// the block-size is nominally fixed but children should treat % block-sizes as auto.
func (b *ConstraintSpaceBuilder) SetIsFixedBlockSizeIndefinite(v bool) *ConstraintSpaceBuilder {
	b.space.IsFixedBlockSizeIndefinite = v
	return b
}

// SetOrthogonalFallbackInlineSize sets the ICB fallback for orthogonal children.
// This must be called BEFORE SetAvailableSize, as the fallback is applied during
// the available-size swap.
func (b *ConstraintSpaceBuilder) SetOrthogonalFallbackInlineSize(v float64) *ConstraintSpaceBuilder {
	b.space.OrthogonalFallbackInlineSize = v
	return b
}

// SetForcedMinBlockSize sets a minimum block-size for the child.
// Used for the root element to ensure it fills at least the ICB.
func (b *ConstraintSpaceBuilder) SetForcedMinBlockSize(v float64) *ConstraintSpaceBuilder {
	b.space.ForcedMinBlockSize = v
	return b
}

// Build returns the finished ConstraintSpace.
func (b *ConstraintSpaceBuilder) Build() ConstraintSpace {
	return b.space
}
