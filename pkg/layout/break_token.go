package layout

// BlockBreakToken captures the state needed to resume layout in the next
// fragmentainer. Mirrors Blink's BlockBreakToken
// (third_party/blink/renderer/core/layout/block_break_token.h).
type BlockBreakToken struct {
	// Node is the layout node this token belongs to.
	Node *LayoutInputNode

	// ConsumedBlockSize is how much of this node's block-size has been
	// consumed by previous fragments. The next fragment starts here.
	ConsumedBlockSize float64

	// SequenceNumber is which fragment this is (0-indexed).
	SequenceNumber int

	// ChildBreakTokens contains break tokens for children that need
	// to resume. Children completed before the break are omitted.
	ChildBreakTokens []*BlockBreakToken

	// IsBreakBefore means the node hasn't started yet — it should
	// begin fresh in the next fragmentainer.
	IsBreakBefore bool

	// IsCausedByColumnSpanner means a column-span:all element caused the break.
	IsCausedByColumnSpanner bool

	// HasSeenAllChildren means all in-flow children have been encountered
	// (some may still be breaking).
	HasSeenAllChildren bool

	// MonolithicOverflow tracks how much a monolithic (unbreakable) element
	// overflowed the fragmentainer.
	MonolithicOverflow float64
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
