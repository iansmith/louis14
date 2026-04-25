package layout

import "louis14/pkg/geometry/layoutunit"

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

	// ChildBreakTokens contains break tokens for children that need
	// to resume. Children completed before the break are omitted.
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
}

// HasBreakToken returns true if there is more content to lay out.
func (t *BlockBreakToken) HasBreakToken() bool {
	return t != nil
}

// InputBreakToken returns the first child break token for resumption, or nil.
func (t *BlockBreakToken) InputBreakToken() *BlockBreakToken {
	if t == nil || len(t.ChildBreakTokens) == 0 {
		return nil
	}
	return t.ChildBreakTokens[0]
}
