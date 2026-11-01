package layout

// EarlyBreakType says what an EarlyBreak breaks before. Mirrors Blink's
// EarlyBreak::BreakType (early_break.h:20-23 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type EarlyBreakType int

const (
	// EarlyBreakLine breaks before a given line number.
	EarlyBreakLine EarlyBreakType = iota
	// EarlyBreakBlock breaks before or inside a given child block.
	EarlyBreakBlock
)

// EarlyBreak is a possible (and good) unforced breakpoint found during
// layout: when an algorithm later runs out of space at a less appealing
// place, it lays out again and breaks here instead. Mirrors Blink's
// EarlyBreak (early_break.h:18-70 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), both variants: kLine (before a
// line of an inline formatting context) and kBlock (before a child block, or
// inside it, following the BreakInside chain).
type EarlyBreak struct {
	breakType EarlyBreakType
	// lineNumber is set for EarlyBreakLine.
	lineNumber int
	// node is set for EarlyBreakBlock: the child block to break before or
	// inside.
	node *LayoutInputNode
	// breakInsideChild is the early break inside node; nil breaks before it.
	breakInsideChild *EarlyBreak
	breakAppeal      BreakAppeal
}

// NewLineEarlyBreak returns an early break before line lineNumber (the
// number of line boxes laid out before it in the fragment). Mirrors
// explicit EarlyBreak(int line_number, BreakAppeal) (early_break.h:32-36).
func NewLineEarlyBreak(lineNumber int, breakAppeal BreakAppeal) *EarlyBreak {
	return &EarlyBreak{breakType: EarlyBreakLine, lineNumber: lineNumber, breakAppeal: breakAppeal}
}

// NewBlockEarlyBreak returns an early break before child block node, or,
// with a non-nil breakInsideChild, inside it at that breakpoint. Mirrors
// explicit EarlyBreak(BlockNode, BreakAppeal, const EarlyBreak*)
// (early_break.h:25-31).
func NewBlockEarlyBreak(node *LayoutInputNode, breakAppeal BreakAppeal, breakInsideChild *EarlyBreak) *EarlyBreak {
	return &EarlyBreak{breakType: EarlyBreakBlock, node: node, breakInsideChild: breakInsideChild, breakAppeal: breakAppeal}
}

// Type mirrors EarlyBreak::Type (early_break.h:38).
func (e *EarlyBreak) Type() EarlyBreakType { return e.breakType }

// IsBreakBefore mirrors EarlyBreak::IsBreakBefore (early_break.h:39): there
// is no breakpoint inside the block, so the break goes before it.
func (e *EarlyBreak) IsBreakBefore() bool { return e.breakInsideChild == nil }

// GetBlockNode is the child block of an EarlyBreakBlock. Mirrors
// EarlyBreak::GetBlockNode (early_break.h:40-43).
func (e *EarlyBreak) GetBlockNode() *LayoutInputNode { return e.node }

// LineNumber is the line to break before. Mirrors EarlyBreak::LineNumber
// (early_break.h:44-47).
func (e *EarlyBreak) LineNumber() int { return e.lineNumber }

// BreakInside is the breakpoint inside the block. Mirrors
// EarlyBreak::BreakInside (early_break.h:48).
func (e *EarlyBreak) BreakInside() *EarlyBreak { return e.breakInsideChild }

// GetBreakAppeal mirrors EarlyBreak::GetBreakAppeal (early_break.h:50-52).
func (e *EarlyBreak) GetBreakAppeal() BreakAppeal { return e.breakAppeal }

// EnterEarlyBreakInChild returns the early break the child is to be laid
// out with: the breakpoint inside child when earlyBreak leads into it, else
// nil. Mirrors Blink's EnterEarlyBreakInChild (fragmentation_utils.cc:
// 1355-1365 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func EnterEarlyBreakInChild(child *LayoutInputNode, earlyBreak *EarlyBreak) *EarlyBreak {
	if earlyBreak == nil || earlyBreak.Type() != EarlyBreakBlock || earlyBreak.GetBlockNode() != child {
		return nil
	}
	// With no break inside, the break went before the child already.
	return earlyBreak.BreakInside()
}

// IsEarlyBreakTarget reports whether the next child is where the early
// break goes: for a line early break, the next line box (laid out after
// builder.LineCount() lines; child is nil); for a block early break, child
// when the break is before it. Mirrors Blink's IsEarlyBreakTarget
// (fragmentation_utils.cc:1367-1375 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func IsEarlyBreakTarget(earlyBreak *EarlyBreak, builder *BoxFragmentBuilder, child *LayoutInputNode) bool {
	if earlyBreak.Type() == EarlyBreakLine {
		return earlyBreak.LineNumber() == builder.LineCount()
	}
	return earlyBreak.IsBreakBefore() && earlyBreak.GetBlockNode() == child
}
