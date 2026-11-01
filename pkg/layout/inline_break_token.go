package layout

// BreakToken is a break token of either kind, as a parent's child break
// tokens hold both: a BlockBreakToken for a block child, an
// InlineBreakToken for its inline formatting context. Mirrors Blink's
// BreakToken base (break_token.h:33-42 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BreakToken interface {
	// InputNode is the node the token resumes.
	InputNode() *LayoutInputNode
	// IsInlineType reports an InlineBreakToken.
	IsInlineType() bool
}

// InputNode mirrors BreakToken::InputNode for a block break token.
func (t *BlockBreakToken) InputNode() *LayoutInputNode { return t.Node }

// IsInlineType mirrors BreakToken::IsInlineType: false for a block token.
func (t *BlockBreakToken) IsInlineType() bool { return false }

// InlineItemTextIndex is a position in an inline formatting context: an
// item index and an offset in the text content. Mirrors Blink's
// InlineItemTextIndex (inline/inline_item_text_index.h:15-39 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type InlineItemTextIndex struct {
	ItemIndex  int
	TextOffset int
}

// IsZero reports the start of the inline formatting context.
func (i InlineItemTextIndex) IsZero() bool { return i.ItemIndex == 0 && i.TextOffset == 0 }

// InlineBreakToken is where layout of an inline formatting context resumes
// in the next fragment: the start of the next line (Start), or — in a
// parallel block flow — a float of the inline formatting context that broke
// before or inside, at its item, wrapping the float's block break token.
// Mirrors Blink's InlineBreakToken (inline/inline_break_token.h:46-160,
// inline_break_token.cc:24-67 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// louis14 has no InlineNode: the node of a line's token is the run of lines
// (an inline segment) of a block-in-inline container, or the block whose
// children are all inline; that of a float's is the block whose inline
// items hold the float (its item index counts in them), which is never one
// of the block walk's children. Not ported: the style at the break (louis14 rebuilds the open
// inline boxes from the items before Start), the first-line, cloned box
// decoration, line-clamp and past-first-formatted-line flags, and ruby
// break data (louis14 breaks no ruby column across fragmentainers).
type InlineBreakToken struct {
	Node  *LayoutInputNode
	Start InlineItemTextIndex
	// IsForcedBreak: the line was broken before by a forced break
	// (kIsForcedBreak).
	IsForcedBreak bool
	// IsInParallelBlockFlow: the token resumes a float in a parallel flow
	// (kIsInParallelBlockFlow); blockBreakToken is the float's.
	IsInParallelBlockFlow bool
	blockBreakToken       *BlockBreakToken
}

// CreateInlineBreakTokenForParallelBlockFlow wraps the break token of a
// float at item start of node's inline formatting context, so the float
// resumes at its item. Mirrors InlineBreakToken::CreateForParallelBlockFlow
// (inline_break_token.cc:61-67 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func CreateInlineBreakTokenForParallelBlockFlow(node *LayoutInputNode, start InlineItemTextIndex, childBreakToken *BlockBreakToken) *InlineBreakToken {
	return &InlineBreakToken{Node: node, Start: start, IsInParallelBlockFlow: true, blockBreakToken: childBreakToken}
}

// InputNode mirrors BreakToken::InputNode.
func (t *InlineBreakToken) InputNode() *LayoutInputNode { return t.Node }

// IsInlineType mirrors BreakToken::IsInlineType.
func (t *InlineBreakToken) IsInlineType() bool { return true }

// GetBlockBreakToken is the break token of the float a parallel-flow token
// resumes, or nil. Mirrors InlineBreakToken::GetBlockBreakToken
// (inline_break_token.cc:24-29 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (t *InlineBreakToken) GetBlockBreakToken() *BlockBreakToken {
	if t == nil {
		return nil
	}
	return t.blockBreakToken
}

// inlineLineBreakToken returns the line resume point among tokens: the
// inline break token that is not in a parallel flow (in Blink always the
// last child token of the block, box_fragment_builder.cc:156-160 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), or nil.
func inlineLineBreakToken(tokens []BreakToken) *InlineBreakToken {
	for i := len(tokens) - 1; i >= 0; i-- {
		if t, ok := tokens[i].(*InlineBreakToken); ok && !t.IsInParallelBlockFlow {
			return t
		}
	}
	return nil
}
