package layout

import "louis14/pkg/css"

// UnpositionedFloat is a float waiting to be positioned in its block
// formatting context: the inputs PositionFloat needs. Mirrors Blink's
// UnpositionedFloat (unpositioned_float.h:19-90 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// Coordinates are louis14's: BFC offsets are float64 BFC-relative values,
// and ExpectedBfcBlockOffset / FragmentainerBlockOffset both describe the
// parent's content-box block-start (Blink's describe its border edge; the
// two pairs are consistent, and only their difference,
// FragmentainerOffsetAtBfc, is used). Blink's parent_style (orthogonal
// fallback) and is_hidden_for_paint have no louis14 consumer here;
// BfcContainerInlineSize and Ctx carry what louis14's exclusion-space
// queries and layout entry need.
type UnpositionedFloat struct {
	Node *LayoutInputNode
	// Token is the float's incoming break token when it resumes after a
	// fragmentainer break (or was pushed there by a break before it).
	Token          *BlockBreakToken
	AvailableSize  LogicalSize
	PercentageSize LogicalSize
	// OriginBfcLineOffset / OriginBfcBlockOffset are the float's origin
	// (Blink's origin_bfc_offset): the parent's content-box line-left edge
	// and the next border edge after the preceding in-flow content.
	OriginBfcLineOffset  float64
	OriginBfcBlockOffset float64
	ParentSpace          ConstraintSpace
	// FragmentainerBlockSize and FragmentainerBlockOffset are the parent's
	// FragmentainerCapacityForChildren / FragmentainerOffsetForChildren.
	FragmentainerBlockSize   float64
	FragmentainerBlockOffset float64
	// ExpectedBfcBlockOffset is the BFC block offset at which
	// FragmentainerBlockOffset applies (Blink reads it from the parent
	// space, ConstraintSpace::ExpectedBfcBlockOffset).
	ExpectedBfcBlockOffset float64

	BfcContainerInlineSize float64
	Ctx                    *LayoutContext
}

// FloatSide is the float's physical side (Blink: Style().Floating()).
func (uf *UnpositionedFloat) FloatSide() css.FloatType {
	return uf.Node.Style().GetFloat()
}

// ClearType is the float's clear value. Mirrors Blink's
// UnpositionedFloat::ClearType (unpositioned_float.h:70-72); louis14 keeps
// clear physical and maps it in the exclusion-space queries.
func (uf *UnpositionedFloat) ClearType() css.ClearType {
	return uf.Node.Style().GetClear()
}

// FragmentainerSpaceLeft is the fragmentainer space left at the parent's
// block-start, clamped to zero. Mirrors Blink's
// UnpositionedFloat::FragmentainerSpaceLeft (unpositioned_float.h:74-78).
func (uf *UnpositionedFloat) FragmentainerSpaceLeft() float64 {
	return max(0, uf.FragmentainerBlockSize-uf.FragmentainerBlockOffset)
}

// FragmentainerOffsetAtBfc is the fragmentainer offset of the BFC block
// origin: add a BFC block offset to get a fragmentainer offset. Mirrors
// Blink's UnpositionedFloat::FragmentainerOffsetAtBfc
// (unpositioned_float.h:80-84).
func (uf *UnpositionedFloat) FragmentainerOffsetAtBfc() float64 {
	return uf.FragmentainerBlockOffset - uf.ExpectedBfcBlockOffset
}
