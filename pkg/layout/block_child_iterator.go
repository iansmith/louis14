package layout

import "slices"

// BlockChildIterator walks the block children of a block container in
// layout order: when resuming after a fragmentation break, first the
// children the incoming break token carries a child break token for (in
// token order), then — unless the container has seen all its children —
// the unstarted siblings after the last of them. Mirrors Blink's
// BlockChildIterator (block_child_iterator.h:18-50,
// block_child_iterator.cc:14-142 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// with calculate_child_idx always on: every entry carries its index.
//
// louis14 hands the iterator the container's child list instead of its
// first child: text children are skipped, as Blink's NextBlockSibling walks
// only block children. Blink's inline-formatting-context mode (is_ifc_) is
// not ported: a louis14 inline formatting context lays out through
// layoutInlineChildren, and its runs of lines in a block-in-inline
// container are block-walk children here. A child break token whose node is
// not among the children (a float of the inline formatting context, which
// resumeInlineFloats resumes) is skipped.
type BlockChildIterator struct {
	children []*LayoutInputNode
	// containerBreakToken is the incoming break token while its child break
	// tokens are being resumed; nil once they are done (or there are none).
	containerBreakToken *BlockBreakToken
	// childTokenIdx indexes the next child break token to resume.
	childTokenIdx int
	// nextUnstartedChild is the index of the next child to lay out from
	// scratch; -1 when there is none (yet).
	nextUnstartedChild int
	// lastResumedChild is the index of the child of the last resumed token.
	lastResumedChild    int
	didHandleFirstChild bool
}

// BlockChildIteratorEntry is one child to lay out and the break token to
// resume it with (nil when laid out from scratch). Mirrors Blink's
// BlockChildIterator::Entry (block_child_iterator.h:52-77 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BlockChildIteratorEntry struct {
	Node  *LayoutInputNode
	Token *BlockBreakToken
	Index int
}

// AtEnd reports whether the iteration is over (Entry::AtEnd).
func (e BlockChildIteratorEntry) AtEnd() bool { return e.Node == nil }

// NewBlockChildIterator returns an iterator over children resuming
// containerBreakToken, if any. Mirrors the BlockChildIterator constructor
// (block_child_iterator.cc:14-44 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func NewBlockChildIterator(children []*LayoutInputNode, containerBreakToken *BlockBreakToken) *BlockChildIterator {
	it := &BlockChildIterator{
		children:            children,
		containerBreakToken: containerBreakToken,
		nextUnstartedChild:  nextBlockChild(children, 0),
		lastResumedChild:    -1,
	}
	if containerBreakToken != nil {
		// With child break tokens, the next unstarted child is known only
		// past them; with all children seen, there is none (:31-37).
		if len(containerBreakToken.ChildBreakTokens) > 0 || containerBreakToken.HasSeenAllChildren {
			it.nextUnstartedChild = -1
		}
		// Done with the container token right away when it has no child
		// break tokens (:38-41).
		if len(containerBreakToken.ChildBreakTokens) == 0 {
			it.containerBreakToken = nil
		}
	}
	return it
}

// nextBlockChild returns the index of the first non-text child at or after
// from, or -1. Stands in for Blink's NextBlockSibling.
func nextBlockChild(children []*LayoutInputNode, from int) int {
	for i := max(from, 0); i < len(children); i++ {
		if !children[i].IsText() {
			return i
		}
	}
	return -1
}

// NextChild returns the next child to lay out, or an entry that is AtEnd.
// Mirrors BlockChildIterator::NextChild for a block formatting context
// (block_child_iterator.cc:72-134 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (it *BlockChildIterator) NextChild() BlockChildIteratorEntry {
	if it.didHandleFirstChild {
		if it.containerBreakToken != nil {
			if it.childTokenIdx == len(it.containerBreakToken.ChildBreakTokens) {
				// Past the last child break token: the next unstarted
				// sibling follows the last resumed child, unless all
				// children were seen (:78-95).
				if !it.containerBreakToken.HasSeenAllChildren && it.lastResumedChild >= 0 {
					it.nextUnstartedChild = nextBlockChild(it.children, it.lastResumedChild+1)
				}
				it.containerBreakToken = nil
			}
		} else if it.nextUnstartedChild >= 0 {
			it.nextUnstartedChild = nextBlockChild(it.children, it.nextUnstartedChild+1)
		}
	} else {
		it.didHandleFirstChild = true
	}

	if it.containerBreakToken != nil {
		// Resume the children that broke earlier, one token each
		// (:101-118).
		tokens := it.containerBreakToken.ChildBreakTokens
		for it.childTokenIdx < len(tokens) {
			token := tokens[it.childTokenIdx]
			it.childTokenIdx++
			if idx := slices.Index(it.children, token.Node); idx >= 0 {
				it.lastResumedChild = idx
				return BlockChildIteratorEntry{Node: token.Node, Token: token, Index: idx}
			}
		}
		// Only tokens for nodes that are not children remained.
		return it.NextChild()
	}
	if it.nextUnstartedChild < 0 {
		return BlockChildIteratorEntry{}
	}
	return BlockChildIteratorEntry{Node: it.children[it.nextUnstartedChild], Index: it.nextUnstartedChild}
}
