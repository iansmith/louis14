package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// blockChildIteratorFixture returns the children of a block with five block
// children (a..e) and a whitespace text node between b and c.
func blockChildIteratorFixture(t *testing.T) []*LayoutInputNode {
	t.Helper()
	var els []*html.Node
	styles := map[*html.Node]*css.Style{}
	for range 5 {
		el := makeNode("div")
		styles[el] = makeStyle("display", "block")
		els = append(els, el)
	}
	text := makeTextNode(" ")
	root := makeNode("div", els[0], els[1], text, els[2], els[3], els[4])
	styles[root] = makeStyle("display", "block")
	children := buildTestTree(root, styles).Children()
	if len(children) != 6 || !children[2].IsText() {
		t.Fatalf("fixture: %d children (text at 2: %v), want 6 with a text node at 2", len(children), len(children) > 2 && children[2].IsText())
	}
	return children
}

type iteratorStep struct {
	node  *LayoutInputNode
	token *BlockBreakToken
	index int
}

func drainBlockChildIterator(it *BlockChildIterator) []iteratorStep {
	var steps []iteratorStep
	for e := it.NextChild(); !e.AtEnd(); e = it.NextChild() {
		steps = append(steps, iteratorStep{e.Node, e.Token, e.Index})
		if len(steps) > 10 { // more than the fixture's children: not terminating
			break
		}
	}
	return steps
}

func checkIteratorSteps(t *testing.T, got, want []iteratorStep) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("visited %d children, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestBlockChildIterator_Fresh: without a break token every block child is
// visited once, in order, then the iterator stays at its end. Mirrors
// Blink's BlockChildIterator::NextChild (block_child_iterator.cc:72-142 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which walks
// NextBlockSibling — text is not a block child.
func TestBlockChildIterator_Fresh(t *testing.T) {
	c := blockChildIteratorFixture(t)
	it := NewBlockChildIterator(c, nil)
	checkIteratorSteps(t, drainBlockChildIterator(it), []iteratorStep{
		{c[0], nil, 0}, {c[1], nil, 1}, {c[3], nil, 3}, {c[4], nil, 4}, {c[5], nil, 5},
	})
	if !it.NextChild().AtEnd() {
		t.Error("NextChild past the end is not AtEnd")
	}
}

// TestBlockChildIterator_ResumesChildBreakTokens: resuming, the children
// with a break token are visited first, in token order, each with its
// token; the children between them completed earlier; after the last
// token the next unstarted sibling follows (block_child_iterator.cc:16-43,
// :78-95, :101-118 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBlockChildIterator_ResumesChildBreakTokens(t *testing.T) {
	c := blockChildIteratorFixture(t)
	tb := &BlockBreakToken{Node: c[1]}
	td := &BlockBreakToken{Node: c[4], IsBreakBefore: true}
	it := NewBlockChildIterator(c, &BlockBreakToken{ChildBreakTokens: []*BlockBreakToken{tb, td}})
	checkIteratorSteps(t, drainBlockChildIterator(it), []iteratorStep{
		{c[1], tb, 1}, {c[4], td, 4}, {c[5], nil, 5},
	})
}

// TestBlockChildIterator_HasSeenAllChildren: a container that has seen all
// its children resumes only its child break tokens
// (block_child_iterator.cc:33-37, :81-89 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and with none, visits nothing.
func TestBlockChildIterator_HasSeenAllChildren(t *testing.T) {
	c := blockChildIteratorFixture(t)
	tb := &BlockBreakToken{Node: c[1]}
	it := NewBlockChildIterator(c, &BlockBreakToken{ChildBreakTokens: []*BlockBreakToken{tb}, HasSeenAllChildren: true})
	checkIteratorSteps(t, drainBlockChildIterator(it), []iteratorStep{{c[1], tb, 1}})

	it = NewBlockChildIterator(c, &BlockBreakToken{HasSeenAllChildren: true})
	checkIteratorSteps(t, drainBlockChildIterator(it), nil)
}

// TestBlockChildIterator_TokenWithoutChildTokensRestarts: a container break
// token with no child break tokens and not all children seen (a box that
// broke only itself before laying out any child) is forgotten, and the
// children are visited from the first (block_child_iterator.cc:38-41 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBlockChildIterator_TokenWithoutChildTokensRestarts(t *testing.T) {
	c := blockChildIteratorFixture(t)
	it := NewBlockChildIterator(c, &BlockBreakToken{})
	checkIteratorSteps(t, drainBlockChildIterator(it), []iteratorStep{
		{c[0], nil, 0}, {c[1], nil, 1}, {c[3], nil, 3}, {c[4], nil, 4}, {c[5], nil, 5},
	})
}

// TestBlockChildIterator_AtEndAfterStop: stopping at a child and asking for
// the next one tells whether every child has been seen — Blink's
// SetHasSeenAllChildren test after the child loop
// (block_layout_algorithm.cc:1150-1161 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBlockChildIterator_AtEndAfterStop(t *testing.T) {
	c := blockChildIteratorFixture(t)
	it := NewBlockChildIterator(c, nil)
	for e := it.NextChild(); !e.AtEnd(); e = it.NextChild() {
		if e.Node == c[4] {
			break
		}
	}
	if e := it.NextChild(); e.AtEnd() || e.Node != c[5] {
		t.Errorf("after stopping at d: NextChild = %+v, want e", e)
	}
	if !it.NextChild().AtEnd() {
		t.Error("after e: NextChild is not AtEnd")
	}
}
