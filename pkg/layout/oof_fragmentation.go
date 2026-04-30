package layout

import (
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
)

// OOF-fragmentation port (Phase 25). Mirrors Blink's mechanism for laying out
// out-of-flow positioned descendants whose containing block is itself
// fragmented across a multicol's outer fragmentainers. See `findings.md`
// § Phase 25 for the full design + verified Blink references.
//
// Source files (third_party/blink/renderer/core/layout/, verified 2026-04-30):
//   - oof_positioned_node.h:30-86, 171-235, 266-350, 366-408
//   - block_break_token.h:124-137, 212-237, 259-268
//   - fragment_builder.h:254-423
//   - column_layout_algorithm.cc:391-414
//   - out_of_flow_layout_part.cc:589-754, 1265-1529, 2390-2630, 3143-3210
//
// Architecture: louis14 collapses Blink's three-tier
// LayoutInputNode/LayoutObject/LayoutBox model into a single
// `*LayoutInputNode` (see pkg/layout/layout_input_node.go). Map keys here use
// `*LayoutInputNode` everywhere Blink would key by `*LayoutBox`. OOF data is
// carried in logical coordinates throughout (Blink converts logical→physical
// at fragment-finalization time; revisit if vertical writing modes need to
// traverse a fragmentation context).

// LogicalOofContainingBlock captures a containing-block fragment plus its
// offset, relative offset, an optional clipped-container offset, and a
// column-spanner flag. Used by deferred OOF candidates to remember which CB
// fragment they belong to when the OOF is finally laid out at the outer
// fragmentation root. Mirrors Blink's `OofContainingBlock<LogicalOffset>`
// (oof_positioned_node.h:30-86).
type LogicalOofContainingBlock struct {
	// Offset is the CB fragment's offset within the multicol container,
	// expressed in the multicol's logical coordinate system.
	Offset LogicalOffset

	// RelativeOffset is the CSS position:relative offset on the CB itself
	// (applied at paint time, not baked into Offset). Mirrors Blink's
	// OofContainingBlock::relative_offset_.
	RelativeOffset LogicalOffset

	// Fragment points at the specific PhysicalFragment of the CB this OOF
	// belongs to. Used at OOF-layout time to extract the CB's
	// ConsumedBlockSize via the CB fragment's BlockBreakToken for the
	// stitched ⇄ local coordinate conversion. Mirrors Blink's
	// OofContainingBlock::fragment_.
	Fragment *PhysicalFragment

	// ClippedContainerBlockOffset is non-nil when the OOF descends through
	// an `overflow:clip` (or similar) ancestor. Consumed by
	// ComputeStartFragmentIndexAndRelativeOffset
	// (out_of_flow_layout_part.cc:3143-3210) to clamp the start fragmentainer
	// for OOFs inside clipped containers during paginated/multicol layout.
	// Blink uses `LayoutUnit::Min()` as the absent-value sentinel; in Go we
	// use a nil pointer to disambiguate.
	ClippedContainerBlockOffset *layoutunit.LayoutUnit

	// IsInsideColumnSpanner marks OOFs whose CB is inside a column-spanner
	// descendant (handled differently because spanners interrupt the column
	// flow). Mirrors Blink's OofContainingBlock::is_inside_column_spanner_.
	IsInsideColumnSpanner bool
}

// LogicalOofInlineContainer captures inline-CB resolution info for OOF
// candidates whose CB walks through an inline element (CSS 2.1 §10.1.4).
// Mirrors Blink's `OofInlineContainer<LogicalOffset>` (oof_positioned_node.h).
//
// Stored by value, not by pointer — the absent state is a zero-valued
// LogicalOofInlineContainer{} (Container == nil), matching Blink's
// `inline_container.container_ == nullptr` convention.
type LogicalOofInlineContainer struct {
	// Container is the inline DOM node. The DOM node (not LayoutInputNode)
	// is stored to match against background fragments at OOF-resolution time.
	Container *html.Node

	// RelativeOffset is the inline's position:relative offset (paint-time).
	RelativeOffset LogicalOffset
}

// LogicalOofNodeForFragmentation is a deferred OOF candidate carrying
// containing-block info so the OOF can be laid out at the outer fragmentation
// root and binned into the right outer fragmentainer. Composes
// OutOfFlowCandidate (which now carries the BreakToken and
// RequiresContentBeforeBreaking fields from Blink's OofPositionedNode base
// class). Mirrors Blink's `LogicalOofNodeForFragmentation`
// (oof_positioned_node.h:266-350).
//
// In Blink this is "transient — should not be stored/persisted" (per the file
// comment at oof_positioned_node.h:298-303). Blink converts to the Physical
// form at fragment-finalization time. louis14 keeps the Logical form
// throughout for now (HTB-only); revisit for vertical writing modes.
type LogicalOofNodeForFragmentation struct {
	// Candidate is the underlying OOF data: Node, StaticPosition,
	// Alignment, IsFixedPosition, InlineContainer, and the new
	// BreakToken / RequiresContentBeforeBreaking fields added in Phase 25.
	Candidate OutOfFlowCandidate

	// ContainingBlock captures the absolute-position CB fragment.
	ContainingBlock LogicalOofContainingBlock

	// FixedposContainingBlock captures the fixed-position CB fragment (the
	// ICB or a transform/filter/will-change ancestor that creates a fixed
	// CB). Distinct from ContainingBlock because abspos and fixed CBs differ.
	FixedposContainingBlock LogicalOofContainingBlock

	// FixedposInlineContainer captures inline-container info for the fixed
	// CB when it resolves through an inline ancestor. Zero value
	// (Container == nil) means absent.
	FixedposInlineContainer LogicalOofInlineContainer
}

// MulticolWithPendingOOFs is the entry recorded by an inner multicol's
// MulticolLayoutAlgorithm.Layout when it finishes fragmentation with OOF
// fragmentainer descendants still pending. Drained by
// OutOfFlowLayoutPart.HandleMulticolsWithPendingOOFs at the outer
// fragmentation root, which then walks the inner multicol's physical
// fragments and lays out the OOFs against the outer's fragmentainer flow.
//
// The entry itself does NOT carry the OOF list — the OOFs remain on the inner
// multicol's outgoing fragments via FragmentedOofData. The entry is just
// "remember to revisit this multicol later" plus the multicol-level
// positioning context for fixedpos descendants. Mirrors Blink's
// `MulticolWithPendingOofs<LogicalOffset>` (oof_positioned_node.h).
type MulticolWithPendingOOFs struct {
	// MulticolOffset is the inner multicol's offset within the outer
	// fragmentation root, expressed in the root's logical coordinate system.
	// (Becomes relative to the fixedpos containing block when one applies —
	// matches Blink's behaviour at oof_positioned_node.h:135.)
	MulticolOffset LogicalOffset

	// FixedposContainingBlock is the full CB info for fixedpos descendants
	// of this inner multicol — fragment pointer, relative offset, clipped
	// offset, spanner flag. NOT a bare offset (the verification pass found
	// the bare-offset shape was insufficient — fixedpos resolution needs the
	// full struct).
	FixedposContainingBlock LogicalOofContainingBlock

	// FixedposInlineContainer captures inline-container info for the fixed
	// CB when it resolves through an inline ancestor. Zero value
	// (Container == nil) means absent.
	FixedposInlineContainer LogicalOofInlineContainer
}

// FragmentedOofData is the OOF-fragmentation payload carried on a
// PhysicalFragment of a multicol container or fragmentation-context root.
// Drained by OutOfFlowLayoutPart at the fragmentation root. Mirrors Blink's
// `FragmentedOofData` (oof_positioned_node.h:366-408 — a
// `PhysicalFragment::OofData` subclass; in louis14 we store it as a nullable
// pointer field on PhysicalFragment).
//
// Note: in Blink the descendants vector is the *Physical* form
// (`HeapVector<PhysicalOofNodeForFragmentation>`); louis14 stores the Logical
// form (we don't currently round-trip through writing-mode conversion at the
// fragment boundary). Revisit when vertical writing modes need to traverse a
// fragmentation context.
type FragmentedOofData struct {
	// OofPositionedFragmentainerDescendants is the list of OOFs deferred
	// from this fragment for layout at the fragmentation root. Each entry
	// carries its CB fragment pointer + stitched-coord static position.
	OofPositionedFragmentainerDescendants []LogicalOofNodeForFragmentation

	// MulticolsWithPendingOOFs records inner-multicol nodes that have their
	// OWN deferred OOF descendants. The outer's OutOfFlowLayoutPart walks
	// the inner's physical fragments to drain those OOFs and lay them out
	// against the outer's fragmentainer flow.
	//
	// Keyed by *LayoutInputNode (louis14's persistent layout-tree identity —
	// see pkg/layout/layout_input_node.go for the deliberate two-tier
	// collapse vs Blink's `*LayoutBox` map key).
	MulticolsWithPendingOOFs map[*LayoutInputNode]*MulticolWithPendingOOFs
}
