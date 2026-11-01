package layout

// The per-box layout results of Blink's LayoutBox (layout_box.h:475-512,
// :605-635; layout_box.cc:2658-2820 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), kept on louis14's
// LayoutInputNode, which stands in for the LayoutBox (see
// layout_input_node.go): one result per fragment the box generated, indexed
// by the sequence number of the break token it was laid out from.
//
// A layout pass overwrites the slot it lays out; the final layout of a box
// runs after any measure layout of it, so once layout is complete the
// results are those of the fragments in the tree. Layouts that must leave
// no trace (Blink's DisableLayoutSideEffectsScope) store nothing.
//
// Not ported: the measure cache (LayoutBox::measure_cache_,
// measure_cache.h), which Blink keeps alongside for layout-result caching;
// louis14 has no layout-result cache to read it, and Blink stores a measure
// pass's result as the layout result as well (SetCachedLayoutResult,
// layout_box.cc:2620-2656).

// GetLayoutResults returns the box's layout results, one per fragment, in
// fragment order. Mirrors LayoutBox::GetLayoutResults (layout_box.h:619-622).
func (n *LayoutInputNode) GetLayoutResults() []*LayoutResult {
	return n.layoutResults
}

// SetLayoutResult stores result as the box's index'th layout result. A
// result past the end is appended. One that replaces an earlier result and
// ends the box (no outgoing break token), or broke because of a column
// spanner, or is a fragmentation context root, drops the results after it,
// left over from a previous layout. Mirrors LayoutBox::SetLayoutResult
// (layout_box.cc:2658-2703 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (n *LayoutInputNode) SetLayoutResult(result *LayoutResult, index int) {
	if index < len(n.layoutResults) {
		if len(n.layoutResults) > index+1 {
			if bt := result.BreakToken; bt == nil || bt.IsCausedByColumnSpanner ||
				(result.Fragment != nil && result.Fragment.IsFragmentationContextRoot) {
				n.ShrinkLayoutResults(index + 1)
			}
		}
		n.layoutResults[index] = result
		return
	}
	n.layoutResults = append(n.layoutResults, result)
}

// ShrinkLayoutResults keeps the first resultsToKeep layout results. Mirrors
// LayoutBox::ShrinkLayoutResults (layout_box.cc:2804-2820).
func (n *LayoutInputNode) ShrinkLayoutResults(resultsToKeep int) {
	if resultsToKeep < len(n.layoutResults) {
		n.layoutResults = n.layoutResults[:resultsToKeep]
	}
}

// StoreResultInLayoutBox records the result of laying node out from
// breakToken on the node, unless layout side effects are disabled. A result
// that is the node's only fragment replaces every earlier one. Mirrors
// BlockNode::StoreResultInLayoutBox (block_node.cc:916-937) with
// BlockNode::FinishLayout's early return (:837-840) and FragmentIndex
// (sequenceNumberAfter) @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func StoreResultInLayoutBox(ctx *LayoutContext, node *LayoutInputNode, result *LayoutResult, breakToken *BlockBreakToken) {
	if ctx.LayoutSideEffectsDisabled() || node == nil || result == nil ||
		result.Status != LayoutResultSuccess || result.Fragment == nil {
		return
	}
	node.SetLayoutResult(result, sequenceNumberAfter(breakToken))
}

// DisableLayoutSideEffects starts a scope in which layout stores no results
// on the boxes it lays out, and returns the function that ends it. Mirrors
// Blink's DisableLayoutSideEffectsScope (layout/disable_layout_side_effects_scope.h
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), used where a box is laid out
// only to measure it.
func (ctx *LayoutContext) DisableLayoutSideEffects() func() {
	ctx.layoutSideEffectsDisabled++
	return func() { ctx.layoutSideEffectsDisabled-- }
}

// LayoutSideEffectsDisabled reports whether a DisableLayoutSideEffects scope
// is open (DisableLayoutSideEffectsScope::IsDisabled).
func (ctx *LayoutContext) LayoutSideEffectsDisabled() bool {
	return ctx.layoutSideEffectsDisabled > 0
}

// PhysicalFragments returns the fragments of the box's layout results, in
// fragment order. Mirrors LayoutBox::PhysicalFragments (layout_box.h:613-616
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (n *LayoutInputNode) PhysicalFragments() []*PhysicalFragment {
	fragments := make([]*PhysicalFragment, len(n.layoutResults))
	for i, r := range n.layoutResults {
		fragments[i] = r.Fragment
	}
	return fragments
}
