package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
)

// InlineContainingBlockGeometry holds the rectangles that describe the
// containing block for an abspos whose CB resolves to an inline, per CSS 2.1
// §10.1.4 / CSS Position 3 §def-cb.
//
// The inline CB is formed by the union of all fragment rects belonging to
// the inline container on its first line (StartFragmentUnionRect) and on its
// last line (EndFragmentUnionRect). The CB passed to OOF sizing is the
// axis-aligned rectangle that spans from start to end.
//
// Ports Blink's InlineContainingBlockUtils::InlineContainingBlockGeometry
// (inline_containing_block_utils.h). RelativeOffset carries the position:
// relative/sticky offset of the inline itself so OOF descendants anchor at
// the inline's VISUAL position.
type InlineContainingBlockGeometry struct {
	StartFragmentUnionRect PhysicalRect
	EndFragmentUnionRect   PhysicalRect
	RelativeOffset         LogicalOffset
}

// ComputeInlineContainerGeometry walks the given block's accumulated
// children, finds line-box fragments that contain box-fragments produced by
// the target inline element (matched by DOM node), and unions their rects
// into start/end per Blink's GatherInlineContainerFragmentsFromItems.
//
// Returns nil if no matching fragments are found (the inline produced no
// visible line-box content in this block — e.g. split by a block-in-inline
// with all content in sibling continuations).
//
// The returned rects are in PHYSICAL coordinates relative to the block's
// content-box origin. Consumers (OOF resolver) convert to logical via the
// block's writing-mode converter.
//
// Anonymous block descendants are walked transparently: when a block's
// inline content is split by a block-in-inline (CSS 2.1 §9.2.1.1), the
// surviving inline fragments live inside anonymous block siblings under
// the original parent block — both halves of the split must be reachable
// for the union to span first→last line correctly.
func ComputeInlineContainerGeometry(
	blockChildren []logicalChildLink,
	inlineNode *html.Node,
	blockWDM WritingDirectionMode,
	blockContentSize PhysicalSize,
) *InlineContainingBlockGeometry {
	if inlineNode == nil || len(blockChildren) == 0 {
		return nil
	}

	state := &ifcWalkState{inlineNode: inlineNode}
	blockConv := NewConverter(blockWDM, blockContentSize)
	state.walkLogical(blockChildren, blockConv, PhysicalOffset{})

	if !state.startFound || !state.endFound {
		return nil
	}

	return &InlineContainingBlockGeometry{
		StartFragmentUnionRect: state.startRect,
		EndFragmentUnionRect:   state.endRect,
	}
}

// ifcWalkState accumulates the start/end union rects across a recursive
// walk of (possibly anonymous-block-nested) line boxes.
type ifcWalkState struct {
	inlineNode *html.Node
	startRect  PhysicalRect
	endRect    PhysicalRect
	startFound bool
	endFound   bool
}

// walkLogical iterates a block-in-progress builder's children — the
// fragments are present but offsets are still in logical space and are
// converted on the fly via the parent's converter.
func (s *ifcWalkState) walkLogical(
	children []logicalChildLink,
	parentConv WritingModeConverter,
	originInBlock PhysicalOffset,
) {
	for _, childLink := range children {
		frag := childLink.fragment
		if frag == nil {
			continue
		}
		physOff := parentConv.ToPhysicalOffset(childLink.offset, geomSizeToOld(frag.Size))
		absOff := PhysicalOffset{
			X: originInBlock.X + physOff.X,
			Y: originInBlock.Y + physOff.Y,
		}
		s.visit(frag, absOff)
	}
}

// walkPhysical iterates a built fragment's children — used when descending
// into an anonymous block whose Children []ChildLink already carry physical
// offsets relative to the parent's content box.
func (s *ifcWalkState) walkPhysical(parent *PhysicalFragment, originInBlock PhysicalOffset) {
	for _, childLink := range parent.Children {
		frag := childLink.Fragment
		if frag == nil {
			continue
		}
		absOff := PhysicalOffset{
			X: originInBlock.X + childLink.Offset.LeftF64(),
			Y: originInBlock.Y + childLink.Offset.TopF64(),
		}
		s.visit(frag, absOff)
	}
}

// visit dispatches a single fragment: line boxes contribute to the union
// rects; anonymous block boxes recurse; everything else is skipped.
func (s *ifcWalkState) visit(frag *PhysicalFragment, absOff PhysicalOffset) {
	if frag.Type == FragmentLineBox {
		s.processLine(frag, absOff)
		return
	}
	if frag.Type == FragmentBox && frag.LayoutNode != nil && frag.LayoutNode.IsAnonymous() {
		s.walkPhysical(frag, absOff)
	}
}

// processLine scans a line box for box fragments belonging to the target
// inline (matched by DOM node identity — span backgrounds are emitted per
// line in inline_layout.go with Node = span.DOMNode), unions their rects,
// then advances start/end.
func (s *ifcWalkState) processLine(lineBox *PhysicalFragment, lineOriginInBlock PhysicalOffset) {
	var lineMatches []PhysicalRect
	for _, sub := range lineBox.Children {
		if sub.Fragment == nil || sub.Fragment.Node != s.inlineNode {
			continue
		}
		// Skip non-box fragments (text fragments carry the parent element's
		// Node too, but they are FragmentText).
		if sub.Fragment.Type != FragmentBox {
			continue
		}
		rect := PhysicalRect{
			Offset: PhysicalOffset{
				X: lineOriginInBlock.X + sub.Offset.LeftF64(),
				Y: lineOriginInBlock.Y + sub.Offset.TopF64(),
			},
			Size: geomSizeToOld(sub.Fragment.Size),
		}
		lineMatches = append(lineMatches, rect)
	}
	if len(lineMatches) == 0 {
		return
	}
	lineUnion := lineMatches[0]
	for _, r := range lineMatches[1:] {
		lineUnion = unionPhysicalRect(lineUnion, r)
	}
	if !s.startFound {
		s.startRect = lineUnion
		s.startFound = true
	}
	s.endRect = lineUnion
	s.endFound = true
}

// unionPhysicalRect returns the smallest axis-aligned rectangle containing
// both inputs.
func unionPhysicalRect(a, b PhysicalRect) PhysicalRect {
	ax1, ay1 := a.Offset.X, a.Offset.Y
	ax2, ay2 := ax1+a.Size.Width, ay1+a.Size.Height
	bx1, by1 := b.Offset.X, b.Offset.Y
	bx2, by2 := bx1+b.Size.Width, by1+b.Size.Height
	x1 := ax1
	if bx1 < x1 {
		x1 = bx1
	}
	y1 := ay1
	if by1 < y1 {
		y1 = by1
	}
	x2 := ax2
	if bx2 > x2 {
		x2 = bx2
	}
	y2 := ay2
	if by2 > y2 {
		y2 = by2
	}
	return PhysicalRect{
		Offset: PhysicalOffset{X: x1, Y: y1},
		Size:   PhysicalSize{Width: x2 - x1, Height: y2 - y1},
	}
}

// InlineCBLogical converts a physical InlineContainingBlockGeometry into the
// logical-space inputs needed by the OOF resolver: the CB size (derived from
// the start→end vector) and the offset of the CB's inline-start/block-start
// corner within the block's content-box.
//
// Mirrors the Step 1–3 construction in Blink's
// OutOfFlowLayoutPart::LayoutOOFNode (reading start_fragment_union_rect and
// end_fragment_union_rect via the container's writing-mode converter).
func (g *InlineContainingBlockGeometry) InlineCBLogical(
	blockWDM WritingDirectionMode,
	blockContentSize PhysicalSize,
) (size LogicalSize, offset LogicalOffset) {
	conv := NewConverter(blockWDM, blockContentSize)

	startOffsetLog := conv.ToLogicalOffset(g.StartFragmentUnionRect.Offset, g.StartFragmentUnionRect.Size)
	endOffsetLog := conv.ToLogicalOffset(g.EndFragmentUnionRect.Offset, g.EndFragmentUnionRect.Size)

	// Blink: end_offset += ToLogicalSize(end_rect.size) so we point at the
	// end corner of the last-line union rect.
	endLogSize := ToLogicalSize(g.EndFragmentUnionRect.Size, blockWDM.WM)
	endOffsetLog.InlineOffset += endLogSize.InlineSize
	endOffsetLog.BlockOffset += endLogSize.BlockSize

	size = LogicalSize{
		InlineSize: endOffsetLog.InlineOffset - startOffsetLog.InlineOffset,
		BlockSize:  endOffsetLog.BlockOffset - startOffsetLog.BlockOffset,
	}
	if size.InlineSize < 0 {
		size.InlineSize = 0
	}
	if size.BlockSize < 0 {
		size.BlockSize = 0
	}
	offset = startOffsetLog
	return size, offset
}

// BuildPositionedInlineMap walks the inline item stream and returns a map
// from each OOF item to its innermost positioned-inline ancestor's DOM node
// (or absent if no positioned-inline ancestor exists).
//
// A "positioned inline" here is an inline element whose computed `position`
// is non-static (relative or sticky — absolute/fixed inlines are themselves
// emitted as InlineItemOutOfFlow and don't appear as OpenTag/CloseTag).
//
// The map key is the InlineItem pointer for OOF items; line-breaker
// LineItemResult.Item references the same pointer, so call sites in
// inline_layout.go can stamp the inline container at OOF emission.
//
// Position:fixed OOF candidates are intentionally excluded — per CSS 2.1
// §10.1.4, a fixed element's containing block is the viewport (modulo
// transform/contain ancestors), not a positioned inline ancestor. A
// pos:relative inline does NOT establish a CB for fixed descendants.
// Stamping fixed with an inline CB would route it to inline-CB sizing in
// block_layout.go, preventing it from propagating to the root.
//
// Mirrors Blink's stack of positioned inline ancestors maintained during
// inline layout, used to set NGOutOfFlowPositionedNode::inline_container.
func BuildPositionedInlineMap(items []*InlineItem) map[*InlineItem]*html.Node {
	if len(items) == 0 {
		return nil
	}
	// Each stack entry records the inline ancestor node and whether it also
	// contains fixed-position descendants. A non-static inline contains only
	// abs-pos; a filtered inline (filter != none) is a containing block for
	// fixed too — Filter Effects 1 §"The filter property".
	type inlineCB struct {
		node          *html.Node
		containsFixed bool
	}
	var stack []inlineCB
	out := make(map[*InlineItem]*html.Node)
	for _, item := range items {
		switch item.Type {
		case InlineItemOpenTag:
			if item.Style != nil && item.Node != nil {
				if inlineEstablishesContainingBlock(item.Style) {
					stack = append(stack, inlineCB{
						node:          item.Node,
						containsFixed: len(item.Style.GetFilter()) > 0,
					})
				}
			}
		case InlineItemCloseTag:
			if item.Style != nil && item.Node != nil &&
				inlineEstablishesContainingBlock(item.Style) {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		case InlineItemOutOfFlow:
			if len(stack) == 0 {
				continue
			}
			// Fixed descendants are only contained by a filtered inline, not by
			// a merely position:relative one. Walk the stack from the top to
			// the innermost entry that satisfies the requirement — an inner
			// non-CB-for-fixed inline (e.g. position:relative) can sit above an
			// outer filtered inline that IS a valid CB for fixed.
			isFixed := item.Style != nil && item.Style.GetPosition() == css.PositionFixed
			idx := len(stack) - 1
			if isFixed {
				for idx >= 0 && !stack[idx].containsFixed {
					idx--
				}
				if idx < 0 {
					continue
				}
			}
			out[item] = stack[idx].node
		}
	}
	return out
}

// inlineEstablishesContainingBlock reports whether an inline-level element
// establishes a containing block for positioned descendants. A non-static
// position does (CSS 2.1 §10.1.4); so does a filter (Filter Effects 1 §"The
// filter property": "A value other than none ... results in the creation of
// a containing block for absolute and fixed positioned descendants").
func inlineEstablishesContainingBlock(style *css.Style) bool {
	if style.GetPosition() != css.PositionStatic {
		return true
	}
	if len(style.GetFilter()) > 0 {
		return true
	}
	return false
}
