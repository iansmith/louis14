package layout

// MulticolPartWalker mirrors Blink's MulticolPartWalker
// (third_party/blink/renderer/core/layout/column_layout_algorithm.cc:41-223
// in current Blink main; the cited multicol_part_walker.cc/h files no
// longer exist as separate files — Blink moved the class definition
// inline at the top of column_layout_algorithm.cc).
//
// Iterates the parts of a multicol container that need separate layout:
// regular column content, column spanners, and (eventually) fragmented
// out-of-flow positioned descendants whose containing block is the
// multicol container.
//
// Iteration order:
//  1. Walk parentBreakToken.ChildBreakTokens by childTokenIdx. Each
//     entry's Node discriminates: Node == multicolContainer is column
//     content; Node.IsColumnSpanAll() is a spanner; OOF is gated on a
//     runtime feature in Blink and not yet handled here.
//  2. After incoming child tokens exhausted: optional descendantNode (a
//     spanner moved-to via MoveToSpanner after a fresh discovery via
//     result.ColumnSpannerPath).
//  3. After that: optional nextColumnToken (the trailing column-content
//     resume token from the most recent LayoutLine result).
//  4. isFinished when all three are exhausted.
//
// SCAFFOLD: this type is defined but not yet wired into multicol_layout.go.
// Step 2 of the bundled phase will switch the read site to walker
// dispatch; step 3 will switch the write site.
type MulticolPartWalker struct {
	// multicolContainer is the anonymous content node that wraps multicol
	// children. ChildBreakToken entries with Node == multicolContainer are
	// column-content resume points.
	multicolContainer *LayoutInputNode

	// parentBreakToken is the incoming break token (may be nil at start
	// of a fresh layout).
	parentBreakToken *BlockBreakToken

	// nextColumnToken is the trailing column-content resume token,
	// installed by AddNextColumnBreakToken after a column-content
	// LayoutLine produced a remaining-token. Mirrors Blink's
	// next_column_token_.
	nextColumnToken *BlockBreakToken

	// descendantNode is a spanner discovered in the current outer
	// fragmentainer (via MoveToSpanner after the column-content layout
	// returned a ColumnSpannerPath). Mirrors Blink's descendant_node_.
	descendantNode *LayoutInputNode

	// childTokenIdx is the index into parentBreakToken.ChildBreakTokens
	// for the next entry to inspect. Mirrors Blink's child_token_idx_.
	childTokenIdx int

	current    MulticolPartWalkerEntry
	isFinished bool
}

// MulticolPartWalkerEntry mirrors Blink's MulticolPartWalker::Entry
// (column_layout_algorithm.cc:46-62).
type MulticolPartWalkerEntry struct {
	// BreakToken is the incoming break token for the content to process,
	// or nil if we are at the very start.
	BreakToken *BlockBreakToken

	// DescendantNode is the node to process if this entry represents a
	// column spanner or fragmented OOF. Nil for regular column content.
	DescendantNode *LayoutInputNode
}

// NewMulticolPartWalker constructs a walker for the given multicol
// container content node, seeded by an incoming parent break token (may
// be nil at start of fresh layout).
func NewMulticolPartWalker(container *LayoutInputNode, parentToken *BlockBreakToken) *MulticolPartWalker {
	w := &MulticolPartWalker{
		multicolContainer: container,
		parentBreakToken:  parentToken,
	}
	w.updateCurrent()
	// The first entry in the first multicol fragment may be empty (we
	// just haven't started yet), but if this happens after a parent
	// break token with HasSeenAllChildren, it means we're finished —
	// nothing left to process.
	if parentToken != nil && parentToken.HasSeenAllChildren &&
		w.current.BreakToken == nil && w.current.DescendantNode == nil {
		w.isFinished = true
	}
	return w
}

// Current returns the current walker entry. Caller must check IsFinished
// first.
func (w *MulticolPartWalker) Current() MulticolPartWalkerEntry {
	return w.current
}

// IsFinished reports whether the walker has exhausted all parts.
func (w *MulticolPartWalker) IsFinished() bool {
	return w.isFinished
}

// Next advances the walker to the next part.
func (w *MulticolPartWalker) Next() {
	if w.isFinished {
		return
	}
	w.moveToNext()
	if !w.isFinished {
		w.updateCurrent()
	}
}

// MoveToSpanner moves the walker to the specified spanner and records
// the trailing column-content resume token to walk after the spanner
// (and any sibling spanners). Mirrors Blink's
// MulticolPartWalker::MoveToSpanner.
func (w *MulticolPartWalker) MoveToSpanner(spanner *LayoutInputNode, nextColumnToken *BlockBreakToken) {
	*w = MulticolPartWalker{multicolContainer: w.multicolContainer}
	w.descendantNode = spanner
	w.nextColumnToken = nextColumnToken
	w.updateCurrent()
}

// AddNextColumnBreakToken installs the trailing column-content resume
// token, used when a non-spanner LayoutLine result produced a remaining
// break token. Mirrors Blink's
// MulticolPartWalker::AddNextColumnBreakToken.
func (w *MulticolPartWalker) AddNextColumnBreakToken(token *BlockBreakToken) {
	*w = MulticolPartWalker{multicolContainer: w.multicolContainer}
	w.nextColumnToken = token
	w.updateCurrent()
}

func (w *MulticolPartWalker) updateCurrent() {
	if w.parentBreakToken != nil &&
		w.childTokenIdx < len(w.parentBreakToken.ChildBreakTokens) {
		child := w.parentBreakToken.ChildBreakTokens[w.childTokenIdx]
		// Node == multicolContainer → column content. Otherwise the
		// entry's Node is a spanner (or fragmented OOF, not yet
		// handled here).
		if child != nil && child.Node == w.multicolContainer {
			w.current = MulticolPartWalkerEntry{BreakToken: child}
		} else if child != nil {
			w.current = MulticolPartWalkerEntry{
				BreakToken:     child,
				DescendantNode: child.Node,
			}
		} else {
			// Defensive: a nil child token shouldn't appear in flat
			// document-order encoding, but if it does, treat it as
			// column-content with no resume info.
			w.current = MulticolPartWalkerEntry{}
		}
		return
	}
	if w.descendantNode != nil {
		w.current = MulticolPartWalkerEntry{DescendantNode: w.descendantNode}
		return
	}
	if w.nextColumnToken != nil {
		w.current = MulticolPartWalkerEntry{BreakToken: w.nextColumnToken}
		return
	}
	// Empty entry — at the very start of a fresh multicol layout, or
	// past all children.
	w.current = MulticolPartWalkerEntry{}
}

func (w *MulticolPartWalker) moveToNext() {
	if w.parentBreakToken != nil &&
		w.childTokenIdx < len(w.parentBreakToken.ChildBreakTokens) {
		w.childTokenIdx++
		if w.childTokenIdx < len(w.parentBreakToken.ChildBreakTokens) {
			return
		}
		// Just ran out of incoming child tokens. Fall through to
		// descendantNode / nextColumnToken.
	}
	if w.descendantNode != nil {
		// Blink walks DOM siblings for chained spanner discovery
		// (descendant_node_.NextSibling()). Louis14's spanner discovery
		// happens inside MulticolLayoutAlgorithm via ColumnSpannerPath
		// returns from per-column LayoutLine; chained spanners surface
		// through subsequent LayoutLine calls, not via DOM traversal.
		// So we clear descendantNode and fall through.
		w.descendantNode = nil
		if w.nextColumnToken != nil {
			return
		}
	}
	w.isFinished = true
}

// MulticolBreakTokenBuilder accumulates outgoing child break tokens in
// document order for a multicol container's outgoing BlockBreakToken.
// Mirrors Blink's container_builder_.AddBreakToken /
// AddBreakBeforeChild calls in column_layout_algorithm.cc.
//
// SCAFFOLD: defined but not yet wired into buildOuterBreakResult.
type MulticolBreakTokenBuilder struct {
	children []*BlockBreakToken
}

// AddBreakToken appends an existing break token to the outgoing
// document-order list. Mirrors Blink's
// container_builder_.AddBreakToken(entry.break_token) at cla.cc:729.
func (b *MulticolBreakTokenBuilder) AddBreakToken(t *BlockBreakToken) {
	if t == nil {
		return
	}
	b.children = append(b.children, t)
}

// AddBreakBeforeChild appends a synthetic break-before token for a node
// that needs to start fresh in the next fragmentainer. Mirrors Blink's
// container_builder_.AddBreakBeforeChild at cla.cc:736.
func (b *MulticolBreakTokenBuilder) AddBreakBeforeChild(node *LayoutInputNode, isForcedBreak bool) {
	if node == nil {
		return
	}
	b.children = append(b.children, &BlockBreakToken{
		Node:          node,
		IsBreakBefore: true,
		IsForcedBreak: isForcedBreak,
	})
}

// Children returns the accumulated child break tokens in document order.
func (b *MulticolBreakTokenBuilder) Children() []*BlockBreakToken {
	return b.children
}

// IsEmpty reports whether any child break tokens have been accumulated.
func (b *MulticolBreakTokenBuilder) IsEmpty() bool {
	return len(b.children) == 0
}
