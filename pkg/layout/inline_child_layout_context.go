package layout

// InlineChildLayoutContext carries what the layout of an inline formatting
// context hands back to its block container besides the line boxes: the
// break tokens of floats that broke before or inside, to be resumed in
// parallel flows in the next fragmentainer. A forced break inside a float
// goes to the block's builder directly (PropagateBreakInfo). Mirrors Blink's
// InlineChildLayoutContext parallel-flow token list
// (inline/inline_child_layout_context.h:64-68, .cc:86-93 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the block adds them as
// parallel-flow child break tokens (block_layout_algorithm.cc:2666-2672).
//
// Blink wraps each float token in an InlineBreakToken
// (CreateForParallelBlockFlow, keyed by the float's item index); louis14
// keeps the float's own BlockBreakToken, keyed by its node, which resumes
// the same float.
type InlineChildLayoutContext struct {
	parallelFlowBreakTokens []*BlockBreakToken
}

// ParallelFlowBreakTokens returns the float break tokens propagated so far.
func (c *InlineChildLayoutContext) ParallelFlowBreakTokens() []*BlockBreakToken {
	return c.parallelFlowBreakTokens
}

// PropagateParallelFlowBreakToken records the break token of a float that
// broke before or inside.
func (c *InlineChildLayoutContext) PropagateParallelFlowBreakToken(token *BlockBreakToken) {
	c.parallelFlowBreakTokens = append(c.parallelFlowBreakTokens, token)
}

// propagateTo adds the parallel-flow break tokens to the block's builder
// (block_layout_algorithm.cc:2666-2672 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (c *InlineChildLayoutContext) propagateTo(builder *BoxFragmentBuilder) {
	for _, token := range c.parallelFlowBreakTokens {
		builder.AddBreakToken(token, true)
	}
}
