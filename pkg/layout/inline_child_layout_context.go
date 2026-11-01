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
// Each float token is wrapped in an InlineBreakToken in a parallel block
// flow, keyed by the float's item index (CreateForParallelBlockFlow).
type InlineChildLayoutContext struct {
	parallelFlowBreakTokens []*InlineBreakToken
	// lineFloats are the floats positioned for the line being laid out,
	// added to the block (commitLineFloats) once the line is kept, or
	// dropped with it when it is broken before: Blink adds a line's floats
	// with its line box, after BreakBeforeChildIfNeeded
	// (block_layout_algorithm.cc:2634-2736 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	lineFloats []lineFloat
}

// lineFloat is a float positioned for the line being laid out, not yet
// added to the block.
type lineFloat struct {
	node *LayoutInputNode
	// start is where the float resumes when it breaks: its item in the
	// block's inline formatting context.
	start      InlineItemTextIndex
	positioned PositionedFloat
}

// ParallelFlowBreakTokens returns the float break tokens propagated so far.
func (c *InlineChildLayoutContext) ParallelFlowBreakTokens() []*InlineBreakToken {
	return c.parallelFlowBreakTokens
}

// PropagateParallelFlowBreakToken records the inline break token of a float
// that broke before or inside.
func (c *InlineChildLayoutContext) PropagateParallelFlowBreakToken(token *InlineBreakToken) {
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
