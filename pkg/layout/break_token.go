package layout

import "louis14/pkg/geometry/layoutunit"

// MulticolBreakTokenData mirrors Blink's MulticolBreakTokenData
// (third_party/blink/renderer/core/layout/multicol_break_token_data.h).
// Carries algorithm-specific resume state for tokens emitted by
// MulticolLayoutAlgorithm; nil for tokens emitted by other algorithms.
//
// Currently a typed nullable pointer rather than a polymorphic
// BreakTokenAlgorithmData interface — louis14 has only one algorithm
// carrying break-token data. Generalize when grid/flex/table need it.
type MulticolBreakTokenData struct {
	// ConsumedRowBlockSize is the portion of the current column-row block
	// size that was painted in earlier outer fragmentainers. Mirrors
	// Blink's MulticolBreakTokenData::consumed_row_block_size. Used by
	// MulticolLayoutAlgorithm.offsetInCurrentRow to add row progress
	// before the modulo against row_stride, so a row that overflows an
	// outer fragmentainer can resume mid-stride in the next outer
	// fragmentainer.
	ConsumedRowBlockSize float64
}

// BlockBreakToken captures the state needed to resume layout in the next
// fragmentainer. Mirrors Blink's BlockBreakToken
// (third_party/blink/renderer/core/layout/block_break_token.h).
type BlockBreakToken struct {
	// Node is the layout node this token belongs to.
	Node *LayoutInputNode

	// ConsumedBlockSize is how much of this node's block-size has been
	// consumed by previous fragments. The next fragment starts here.
	ConsumedBlockSize layoutunit.LayoutUnit

	// SequenceNumber is which fragment this is (0-indexed).
	SequenceNumber int

	// ChildBreakTokens contains break tokens for children that need to
	// resume. Children completed before the break are omitted.
	//
	// For tokens emitted by MulticolLayoutAlgorithm this list is flat
	// document-order; each entry's Node identifies its role to the
	// MulticolPartWalker (column-content vs spanner vs OOF). Other
	// callers continue treating it as a list of direct-child resume
	// tokens dispatched by Node identity.
	ChildBreakTokens []*BlockBreakToken

	// IsBreakBefore means the node hasn't started yet — it should
	// begin fresh in the next fragmentainer.
	IsBreakBefore bool

	// IsForcedBreak means the break was caused by a forced break value
	// (break-before:column, break-after:column, etc.) rather than running
	// out of fragmentainer space. Mirrors Blink's BlockBreakToken::IsForcedBreak.
	IsForcedBreak bool

	// IsCausedByColumnSpanner means a column-span:all element caused the break.
	IsCausedByColumnSpanner bool

	// HasSeenAllChildren means all in-flow children have been encountered
	// (some may still be breaking).
	HasSeenAllChildren bool

	// IsAtBlockEnd means layout was past the block-end border edge of the
	// node when it fragmented — i.e. the node's own box finished, but
	// something is overflowing it, establishing a parallel flow.
	// Subsequent content may be placed into the same fragmentainer as a
	// fragment whose break-token carries this flag, as long as it fits.
	// Mirrors Blink's BlockBreakToken::IsAtBlockEnd
	// (block_break_token.h:~287). Set by FinishFragmentation when the
	// parent's box is satisfied but a child still has content to lay out
	// (fragmentation_utils.cc:875, 891).
	//
	// See https://www.w3.org/TR/css-break-3/#parallel-flows.
	IsAtBlockEnd bool

	// IsInParallelFlow means this break-token belongs to a child whose
	// parent's break-token has IsAtBlockEnd=true. A parallel-flow child
	// resumes in the same fragmentainer as the parent's final fragment
	// rather than at child-local block-offset 0 in a fresh parent
	// continuation. Mirrors Blink's BlockBreakToken parallel-flow
	// signaling (block_break_token.h:~287).
	IsInParallelFlow bool

	// MonolithicOverflow tracks how much a monolithic (unbreakable) element
	// overflowed the fragmentainer.
	MonolithicOverflow float64

	// InlineItemStartIndex is the index into InlineItemsData.Items at which
	// inline layout should resume in the next column. Non-zero only for break
	// tokens produced by inline-content fragmentation. Mirrors Blink's
	// approach of recording the line-box boundary as a resume point.
	InlineItemStartIndex int

	// InlineTextOffset is the byte offset within the text buffer at which to
	// resume inline layout in the next column. Paired with InlineItemStartIndex
	// to correctly resume when a break occurred mid-text-item (a single text
	// item may span multiple lines if it contains multiple words).
	InlineTextOffset int

	// InlineShortage is the minimum additional block-size this fragmentainer
	// would need to fit the overflowing inline line. Set only when
	// InlineItemStartIndex is non-trivial. Computed as (blockOffset +
	// overflowingLineHeight) - fragEnd, matching Blink's per-break-point
	// MinSpaceShortage.
	InlineShortage float64

	// HasUnpositionedListMarker signals that the list-item's ::marker was not
	// placed during layout and should be seeded again when this break token is
	// resumed in the next fragment. Mirrors Blink's
	// BlockBreakToken::has_unpositioned_list_marker_.
	HasUnpositionedListMarker bool

	// MulticolData carries MulticolLayoutAlgorithm-specific resume state.
	// Nil for tokens emitted by other algorithms. Mirrors Blink's
	// BlockBreakToken::data_ + MulticolBreakTokenData
	// (multicol_break_token_data.h).
	MulticolData *MulticolBreakTokenData

	// OofStartOffset is the resume position of an OOF child that spans
	// multiple fragmentainers (e.g. an abspos taller than a column).
	// Carried on the OOF child's OWN BlockBreakToken (not the CB's). Zero
	// value (0,0) is the "no resume offset set" state; for non-OOF tokens
	// the field is unused. Phase 25 — mirrors Blink's
	// `BlockBreakToken::oof_start_offset_` (block_break_token.h:259-268)
	// with accessors `OofBlockStartOffset()` / `OofInlineStartOffset()`.
	// Blink mutates this via a `MutableForOofFragmentation` friend; in Go
	// we just write the field directly since BlockBreakToken is not
	// interned.
	OofStartOffset LogicalOffset
}

// HasBreakToken returns true if there is more content to lay out.
func (t *BlockBreakToken) HasBreakToken() bool {
	return t != nil
}

// IsBreakInside reports whether this token represents a break INSIDE a node —
// i.e. the node already started laying out in a previous fragmentainer and is
// resuming — as opposed to a break *before* the node (which never started).
// Mirrors Blink's IsBreakInside (fragmentation_utils.h:63-67 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// `token && !token->IsBreakBefore() && !token->IsRepeated()`. Louis14 has no
// repeated-content fragmentation, so the IsRepeated clause is absent.
//
// Resumed-fragment sizing (consumed-size subtraction / intrinsic-content
// substitution) applies only under a break-inside token: a break-before token
// carries nothing consumed, so its fragment must size fresh.
func (t *BlockBreakToken) IsBreakInside() bool {
	return t != nil && !t.IsBreakBefore
}

// IsFirstForNode reports whether a child resumed with this token (nil when
// not resumed) is producing its first fragment, so a break before it is
// possible. A break-before token — forced or not — means the child has not
// started. Mirrors Blink's is_first_for_node / PhysicalBoxFragment::
// IsFirstForNode as consumed by MovePastBreakpoint (fragmentation_utils.cc:
// 1119-1131 @ a9f50e522efa); a forced break-before cannot fire again at the
// start of a resumed container because there is no container separation
// there (block_layout_algorithm.cc:3160-3169).
func (t *BlockBreakToken) IsFirstForNode() bool {
	return !t.IsBreakInside()
}

// InputBreakToken returns the first child break token for resumption, or nil.
func (t *BlockBreakToken) InputBreakToken() *BlockBreakToken {
	if t == nil || len(t.ChildBreakTokens) == 0 {
		return nil
	}
	return t.ChildBreakTokens[0]
}
