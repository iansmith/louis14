package layout

// EarlyBreak is a breakpoint determined ahead of layout: when a first layout
// pass finds a more appealing place to break than where it ran out of space,
// the algorithm lays out again and breaks there. Mirrors Blink's EarlyBreak
// (early_break.h:18-70 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), kLine
// variant only — break before a given line of an inline formatting context
// (explicit EarlyBreak(int line_number, BreakAppeal), :32-36). Not ported:
// the kBlock variant (break before or inside a block child, with its
// BreakInside() chain) and EnterEarlyBreakInChild, which need block-level
// early-break propagation louis14 does not have yet.
type EarlyBreak struct {
	lineNumber  int
	breakAppeal BreakAppeal
}

// NewLineEarlyBreak returns an early break before line lineNumber (the
// number of line boxes laid out before it in the fragment).
func NewLineEarlyBreak(lineNumber int, breakAppeal BreakAppeal) *EarlyBreak {
	return &EarlyBreak{lineNumber: lineNumber, breakAppeal: breakAppeal}
}

// LineNumber is the line to break before. Mirrors EarlyBreak::LineNumber
// (early_break.h:43-46).
func (e *EarlyBreak) LineNumber() int { return e.lineNumber }

// GetBreakAppeal mirrors EarlyBreak::GetBreakAppeal (early_break.h:49).
func (e *EarlyBreak) GetBreakAppeal() BreakAppeal { return e.breakAppeal }

// IsEarlyBreakTarget reports whether the next line box (laid out after
// builder.LineCount() lines) is where the early break goes. Mirrors the kLine
// branch of Blink's IsEarlyBreakTarget (fragmentation_utils.cc:1367-1375 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func IsEarlyBreakTarget(earlyBreak *EarlyBreak, builder *BoxFragmentBuilder) bool {
	return earlyBreak.LineNumber() == builder.LineCount()
}
