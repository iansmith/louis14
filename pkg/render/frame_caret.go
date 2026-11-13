package render

import (
	"math"

	"github.com/go-text/typesetting/segmenter"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// caretShape is the shape a caret paints in. Mirrors Blink's CaretShape
// (core/layout/inline/caret_rect.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type caretShape int

const (
	caretShapeBar caretShape = iota
	caretShapeBlock
	caretShapeUnderscore
)

// barCaretWidth is LocalFrameView::BarCaretWidth at device scale 1
// (core/frame/local_frame_view.cc:5024-5027 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
const barCaretWidth = 1.0

// frameCaret is the caret to paint: the box whose foreground paints it, its
// rect in absolute coordinates, its color and its shape. Mirrors what
// Blink's CaretDisplayItemClient keeps (layout_block_, local_rect_, color_,
// core/editing/caret_display_item_client.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type frameCaret struct {
	block      *layout.Box
	x, y, w, h float64
	color      css.Color
	shape      caretShape
}

// SetFocusedElement supplies the document's focused element, which decides
// whether the caret at a collapsed selection paints (FrameCaret::
// ShouldShowCaret, core/editing/frame_caret.cc:308-337 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) SetFocusedElement(focused *html.Node) {
	r.focusedElement = focused
}

// updateFrameCaret computes the caret for this paint, or none. Blink paints
// the caret whenever FrameCaret::ShouldShowCaret holds; a static snapshot is
// the first frame after focus, when FrameCaret::StartBlinkCaret has made it
// visible (frame_caret.cc:162-184), so caret-animation does not matter here.
func (r *Renderer) updateFrameCaret() {
	r.caret = nil
	sel := r.selectionRange
	if sel == nil || sel.StartContainer != sel.EndContainer || sel.StartOffset != sel.EndOffset {
		return
	}
	if !r.shouldShowCaret(sel.StartContainer) {
		return
	}
	node, offset := canonicalCaretPosition(sel.StartContainer, sel.StartOffset)
	if node.Type == html.TextNode {
		r.caret = r.caretAtTextOffset(node, offset)
	} else {
		r.caret = r.caretInEmptyBlock(node)
	}
}

// shouldShowCaret mirrors FrameCaret::ShouldShowCaret (frame_caret.cc:
// 308-337) for louis14's static document: the caret is in editable content
// and the focused element contains it (FrameSelection::SelectionHasFocus).
// Caret browsing is not supported.
func (r *Renderer) shouldShowCaret(node *html.Node) bool {
	return html.IsEditable(node) && r.focusedElement != nil && r.focusedElement.Contains(node)
}

// canonicalCaretPosition resolves a caret position in an element to a text
// position, as VisiblePosition canonicalization does before
// LocalCaretRectOfPosition: the end of the text before the offset, else the
// start of the text after it (core/editing/visible_position.cc,
// MostBackwardCaretPosition / MostForwardCaretPosition @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A position with no text on
// either side stays in its element (an empty block). Stand-in: louis14 has
// no VisiblePosition; this covers a caret beside text and in an empty
// element.
func canonicalCaretPosition(container *html.Node, offset int) (*html.Node, int) {
	if container.Type == html.TextNode {
		return container, html.UTF16OffsetToByteOffset(container.Text, offset)
	}
	if offset > 0 && offset <= len(container.Children) {
		if t := lastTextNode(container.Children[offset-1]); t != nil {
			return t, len(t.Text)
		}
	}
	for _, c := range container.Children[min(offset, len(container.Children)):] {
		if t := firstTextNode(c); t != nil {
			return t, 0
		}
	}
	return container, offset
}

func firstTextNode(n *html.Node) *html.Node {
	if n.Type == html.TextNode {
		return n
	}
	for _, c := range n.Children {
		if t := firstTextNode(c); t != nil {
			return t
		}
	}
	return nil
}

func lastTextNode(n *html.Node) *html.Node {
	if n.Type == html.TextNode {
		return n
	}
	for i := len(n.Children) - 1; i >= 0; i-- {
		if t := lastTextNode(n.Children[i]); t != nil {
			return t
		}
	}
	return nil
}

// caretAtTextOffset returns the caret at byte offset in text node t.
// Mirrors ComputeLocalCaretRectAtTextOffset and
// ComputeLogicalCaretRectAtTextOffset (core/layout/inline/caret_rect.cc:
// 180-290 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The text box holding
// the offset is found by walking t's text boxes in tree order; stand-in for
// OffsetMapping, which louis14 lacks, so collapsed white space shifts the
// offset.
func (r *Renderer) caretAtTextOffset(t *html.Node, offset int) *frameCaret {
	// The root editable element's box holds every text box of its text.
	var boxes []*layout.Box
	if root := r.nodeBoxIndex[html.RootEditableElementOf(t)]; root != nil {
		collectTextBoxesForNode(root, t, &boxes)
	}
	var tb *layout.Box
	local := 0
	consumed := 0
	for i, b := range boxes {
		n := len(b.Text)
		if offset < consumed+n || (offset == consumed+n && i == len(boxes)-1) {
			tb, local = b, offset-consumed
			break
		}
		consumed += n
	}
	if tb == nil {
		return nil
	}
	block := caretBlockOf(tb)
	if block == nil {
		return nil
	}

	layer := newPaintLayer(tb)
	fontID := r.fontIDForLayer(layer)
	if fontID < 0 {
		return nil
	}
	shape := caretShapeFromStyle(tb.Style)
	vertical := tb.IsVerticalText
	logical := tb.Text
	ltr := layout.DirectionFromLevel(tb.BidiLevel) == layout.DirectionLTR
	if !ltr {
		logical = layout.ReverseAndMirrorRunes(logical)
	}

	// The caret's inline position, and its block extent: the text
	// fragment's (cursor_block_size, caret_rect.cc:172-176). caretBlockExtent
	// keeps the extent the caret has used since LOU-344 until LOU-960 moves
	// it to the text fragment's own block size.
	inlineLen := tb.Width
	if vertical {
		inlineLen = tb.Height
	}
	blockStart, blockSize := caretBlockExtent(tb, vertical)
	advance := r.measureSegmentAdvance(logical[:local], fontID, layer)
	caretLeft := advance
	if !ltr {
		caretLeft = inlineLen - advance
	}

	inlineSize := barCaretWidth
	inlineOffset := caretLeft - barCaretWidth/2
	if shape != caretShapeBar {
		// ComputeNextCharacterLogicalRect (caret_rect.cc:68-155): the next
		// character's width; past the box's last character, the next text
		// box's first character, else 1ch.
		inlineSize = 0
		if local < len(logical) {
			inlineSize = r.measureSegmentVisualWidth(firstGrapheme(logical[local:]), fontID, layer)
		} else if next := nextTextBoxInLine(tb); next != nil {
			nextLayer := newPaintLayer(next)
			if nextFont := r.fontIDForLayer(nextLayer); nextFont >= 0 {
				inlineSize = r.measureSegmentVisualWidth(firstGrapheme(next.Text), nextFont, nextLayer)
			}
			blockStart, blockSize = caretBlockExtent(next, vertical)
		}
		if inlineSize == 0 {
			inlineSize = r.measureTextStr("0", fontID, layer.featureInputs())
		}
		inlineOffset = caretLeft
		if !ltr {
			inlineOffset = caretLeft - inlineSize
		}
	}
	caretBlockSize := blockSize
	if shape == caretShapeUnderscore {
		caretBlockSize = barCaretWidth
		if !isFlippedLinesWritingMode(tb) {
			blockStart += blockSize - barCaretWidth
		}
	}

	// ComputeLocalCaretRectAtTextOffset rounds the caret's inline position
	// to a pixel, keeping it no further start than its line box
	// (ClampAndRound, caret_rect.cc:58-65, :255-288).
	lineStart := 0.0
	if line := tb.Parent; line != nil {
		switch {
		case !vertical:
			lineStart = line.X - tb.X
		case tb.IsSidewaysLR:
			// The inline axis runs bottom to top.
			lineStart = (tb.Y + tb.Height) - (line.Y + line.Height)
		default:
			lineStart = line.Y - tb.Y
		}
	}
	inlineOffset = math.Round(math.Max(inlineOffset, math.Ceil(lineStart)))

	c := &frameCaret{block: block, shape: shape, color: caretColor(tb.Style, shape)}
	switch {
	case !vertical:
		c.x, c.y, c.w, c.h = tb.X+inlineOffset, blockStart, inlineSize, caretBlockSize
	case tb.IsSidewaysLR:
		// The inline axis runs bottom to top.
		c.x, c.y, c.w, c.h = blockStart, tb.Y+tb.Height-inlineOffset-inlineSize, caretBlockSize, inlineSize
	default:
		c.x, c.y, c.w, c.h = blockStart, tb.Y+inlineOffset, caretBlockSize, inlineSize
	}
	return c
}

// caretInEmptyBlock returns the caret in an empty editable element.
// Mirrors LayoutBlock::LocalCaretRect → LocalCaretRectForEmptyElement
// (core/layout/layout_block.cc:508-526, layout_box_model_object.cc:842-971
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), for a block without children.
// text-indent and ::first-line are not applied.
func (r *Renderer) caretInEmptyBlock(element *html.Node) *frameCaret {
	eb := r.nodeBoxIndex[element]
	if eb == nil || eb.Style == nil || len(eb.Children) > 0 {
		return nil
	}
	layer := newPaintLayer(eb)
	fontID := r.fontIDForLayer(layer)
	if fontID < 0 {
		return nil
	}
	metrics := r.dc.GetFontMetrics(fontID)
	fontHeight := float64(metrics.Ascent+metrics.Descent) / 64.0
	shape := caretShapeFromStyle(eb.Style)
	inlineSize := barCaretWidth
	if shape == caretShapeBlock || shape == caretShapeUnderscore {
		inlineSize = r.measureTextStr("0", fontID, layer.featureInputs())
	}
	caretBlockSize := fontHeight
	if shape == caretShapeUnderscore {
		caretBlockSize = barCaretWidth
	}

	vertical := css.IsVerticalWritingMode(eb.Style)
	inlineLen := eb.Width
	inlineStartBP := eb.Border.Left + eb.Padding.Left
	inlineEndBP := eb.Border.Right + eb.Padding.Right
	blockStartBP := eb.Border.Top + eb.Padding.Top
	if vertical {
		inlineLen = eb.Height
		inlineStartBP = eb.Border.Top + eb.Padding.Top
		inlineEndBP = eb.Border.Bottom + eb.Padding.Bottom
		blockStartBP = eb.Border.Left + eb.Padding.Left
	}

	maxX := inlineLen - inlineEndBP
	x := inlineStartBP
	ltr := eb.Style.GetDirection() != css.DirectionRTL
	alignment, _ := eb.Style.Get("text-align")
	switch {
	case alignment == "center" || alignment == "-webkit-center":
		x = (x + maxX) / 2
	case alignment == "right" || alignment == "-webkit-right",
		(alignment == "start" || alignment == "justify" || alignment == "") && !ltr,
		alignment == "end" && ltr:
		x = maxX - inlineSize
	}
	x = math.Min(x, math.Max(maxX-inlineSize, 0))

	lineHeight := eb.Style.GetLineHeight()
	blockOffset := blockStartBP + (lineHeight-fontHeight)/2
	if shape == caretShapeUnderscore {
		blockOffset += fontHeight
	}

	c := &frameCaret{block: eb, shape: shape, color: caretColor(eb.Style, shape)}
	if vertical {
		c.x, c.y, c.w, c.h = eb.X+blockOffset, eb.Y+x, caretBlockSize, inlineSize
	} else {
		c.x, c.y, c.w, c.h = eb.X+x, eb.Y+blockOffset, inlineSize, caretBlockSize
	}
	return c
}

// caretShapeFromStyle mirrors GetCaretShapeFromComputedStyle
// (caret_rect.cc:386-404).
func caretShapeFromStyle(style *css.Style) caretShape {
	if style == nil {
		return caretShapeBar
	}
	switch v, _ := style.Get("caret-shape"); v {
	case "block":
		return caretShapeBlock
	case "underscore":
		return caretShapeUnderscore
	}
	return caretShapeBar
}

// caretColor resolves caret-color on the caret's node (auto is
// currentColor); a block caret with an auto caret-color is drawn at alpha
// 0.5 (CaretDisplayItemClient::UpdateStyleAndLayoutIfNeeded,
// caret_display_item_client.cc:218-261).
func caretColor(style *css.Style, shape caretShape) css.Color {
	c := style.VisitedDependentColor("caret-color")
	if v, ok := style.Get("caret-color"); shape == caretShapeBlock && (!ok || v == "auto") {
		c.A = c.A / 2
	}
	return c
}

// paintFrameCaret paints the caret if layer's box is the caret's block and
// the phase slot matches its shape: a block caret before the block's
// contents (it sits under the character it covers), a bar or underscore
// caret after them. Mirrors BoxFragmentPainter::PaintInternal's
// PaintCaretsIfNeeded calls (core/paint/box_fragment_painter.cc:755-783)
// and CaretDisplayItemClient::PaintCaret (caret_display_item_client.cc:
// 377-398), a pixel-snapped FillRect.
func (r *Renderer) paintFrameCaret(layer *PaintLayer, beforeContents bool) {
	c := r.caret
	if c == nil || layer.Box != c.block || (c.shape == caretShapeBlock) != beforeContents {
		return
	}
	x, y, w, h := pixelSnap(c.x, c.y, c.w, c.h)
	r.setColor(c.color)
	r.dc.DrawRectangle(x, y, w, h)
	r.dc.Fill()
}

// caretBlockOf returns the block box whose foreground paints a caret in
// text box tb: the nearest ancestor that is neither a line box nor an
// inline box (FrameSelection::GetCaretLayoutBlock, the caret's containing
// LayoutBlock).
func caretBlockOf(tb *layout.Box) *layout.Box {
	for b := tb.Parent; b != nil; b = b.Parent {
		if b.Style == nil || b.Style.GetDisplay() == css.DisplayInline {
			continue
		}
		return b
	}
	return nil
}

// nextTextBoxInLine returns the text box after tb in its line box, if the
// very next item is text (InlineCursor::MoveToNext in
// ComputeNextCharacterLogicalRect).
func nextTextBoxInLine(tb *layout.Box) *layout.Box {
	line := tb.Parent
	if line == nil {
		return nil
	}
	for i, c := range line.Children {
		if c == tb && i+1 < len(line.Children) {
			if next := line.Children[i+1]; next.Text != "" {
				return next
			}
			return nil
		}
	}
	return nil
}

func collectTextBoxesForNode(b *layout.Box, t *html.Node, out *[]*layout.Box) {
	if b.TextNode == t && b.Text != "" && !b.IsHiddenForPaint {
		*out = append(*out, b)
	}
	for _, c := range b.Children {
		collectTextBoxesForNode(c, t, out)
	}
}

// firstGrapheme returns s's first grapheme cluster (LengthOfGraphemeCluster
// in ComputeCharacterWidthAtOffset, caret_rect.cc:58-66).
func firstGrapheme(s string) string {
	var seg segmenter.Segmenter
	seg.InitWithString(s)
	it := seg.GraphemeIterator()
	if !it.Next() {
		return s
	}
	return string(it.Grapheme().Text)
}

// isFlippedLinesWritingMode mirrors blink::IsFlippedLinesWritingMode
// (platform/text/writing_mode.h): lines stack toward the line-over side in
// vertical-lr and sideways-lr.
func isFlippedLinesWritingMode(tb *layout.Box) bool {
	return tb.IsWritingModeVerticalLR || tb.IsSidewaysLR
}

// caretBlockExtent returns the caret's block-direction (origin, extent):
// the rule ::selection backgrounds used before LOU-959, kept for the caret
// until LOU-960 replaces it with Blink's text-fragment block size
// (cursor_block_size, core/layout/inline/caret_rect.cc:85-110 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). It prefers the originating
// element's own inline background-fragment sibling, else a
// line-height: normal block ancestor sharing the node, else textBox's own
// box (LOU-344 / LOU-347 history; obsolete once LOU-960 lands).
func caretBlockExtent(textBox *layout.Box, vertical bool) (origin, extent float64) {
	if textBox == nil {
		return 0, 0
	}
	if textBox.Parent != nil && textBox.Node != nil {
		for _, sibling := range textBox.Parent.Children {
			if sibling != textBox && sibling.Node == textBox.Node && sibling.Text == "" {
				return crossAxisOriginExtent(sibling, vertical)
			}
		}
		if ancestor := textBox.Parent.Parent; ancestor != nil && ancestor.Node == textBox.Node && ancestor.Text == "" &&
			textBox.Style != nil && textBox.Style.IsLineHeightNormal() {
			return crossAxisOriginExtent(ancestor, vertical)
		}
	}
	return crossAxisOriginExtent(textBox, vertical)
}
