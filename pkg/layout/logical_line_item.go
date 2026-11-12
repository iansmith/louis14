package layout

// LogicalLineItem is one child of a line box under construction: a text
// fragment, an atomic inline's fragment, or an inline box's background
// fragment, at its logical offset in the line. Mirrors Blink's
// LogicalLineItem (inline/logical_line_item.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), reduced to what the line box
// builder and LineTruncator read. louis14 builds each fragment while
// placing it, so Fragment stands in for Blink's shape_result / text_offset,
// and an inline box's background fragment (no InlineItem) stands in for
// Blink's inline-box placeholder.
type LogicalLineItem struct {
	Fragment *PhysicalFragment
	// InlineItem is the text or atomic inline item the fragment is for; nil
	// for an inline box's (or the ::first-line box's) background fragment.
	InlineItem *InlineItem
	// LayoutResult is the atomic inline's layout result; nil otherwise.
	LayoutResult *LayoutResult
	// Offset is the item's offset in the line, without RelativeOffset.
	// Blink's rect.offset.
	Offset         LogicalOffset
	RelativeOffset LogicalOffset
	InlineSize     float64
}

// HasInFlowFragment reports whether the item is in-flow content: text or
// an atomic inline. Mirrors LogicalLineItem::HasInFlowFragment.
func (c *LogicalLineItem) HasInFlowFragment() bool {
	return c.InlineItem != nil
}

// LogicalLineItems is the list of a line box's children in the order they
// paint. Mirrors Blink's LogicalLineItems.
type LogicalLineItems []LogicalLineItem

// AddChild appends a child. Mirrors LogicalLineItems::AddChild.
func (l *LogicalLineItems) AddChild(c LogicalLineItem) {
	*l = append(*l, c)
}

// InsertChild inserts a child at index. Mirrors LogicalLineItems::InsertChild.
func (l *LogicalLineItems) InsertChild(index int, c LogicalLineItem) {
	*l = append(*l, LogicalLineItem{})
	copy((*l)[index+1:], (*l)[index:])
	(*l)[index] = c
}
