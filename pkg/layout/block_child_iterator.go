package layout

// BlockChildIterator: RED stub (LOU-492).
type BlockChildIterator struct{}

// BlockChildIteratorEntry: RED stub.
type BlockChildIteratorEntry struct {
	Node  *LayoutInputNode
	Token *BlockBreakToken
	Index int
}

// AtEnd: RED stub.
func (e BlockChildIteratorEntry) AtEnd() bool { return e.Node == nil }

// NewBlockChildIterator: RED stub.
func NewBlockChildIterator(children []*LayoutInputNode, containerBreakToken *BlockBreakToken) *BlockChildIterator {
	return &BlockChildIterator{}
}

// NextChild: RED stub.
func (it *BlockChildIterator) NextChild() BlockChildIteratorEntry { return BlockChildIteratorEntry{} }
