package layout

// RED stubs (LOU-492).
func (b *BoxFragmentBuilder) AddBreakBeforeChild(child *LayoutInputNode, appeal *BreakAppeal, isForcedBreak bool) {
}
func (b *BoxFragmentBuilder) AddBreakToken(token *BlockBreakToken, isInParallelFlow bool) {}
func (b *BoxFragmentBuilder) HasInflowChildBreakInside() bool                             { return false }
func (b *BoxFragmentBuilder) HasInsertedChildBreak() bool                                 { return false }
func (b *BoxFragmentBuilder) ShouldBreakInside() bool                                     { return false }
func (b *BoxFragmentBuilder) HasForcedBreak() bool                                        { return false }
func (b *BoxFragmentBuilder) ChildBreakTokens() []*BlockBreakToken                        { return nil }
func (b *BoxFragmentBuilder) LastChildBreakToken() *BlockBreakToken                       { return nil }
func (b *BoxFragmentBuilder) SetHasSeenAllChildren()                                      {}
func (b *BoxFragmentBuilder) HasSeenAllChildren() bool                                    { return false }
func (b *BoxFragmentBuilder) AddResult(child *LayoutInputNode, childResult *LayoutResult, offset, relativeOffset LogicalOffset) {
}
