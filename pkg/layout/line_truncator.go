package layout

import (
	"sort"

	"github.com/go-text/typesetting/segmenter"

	"louis14/pkg/css"
	"louis14/pkg/text"
)

// LineTruncator truncates a line that overflows a block with
// 'text-overflow' other than clip: it hides the children past the ellipsis,
// truncates the child the ellipsis follows, and places the ellipsis, in the
// line's style, next to it. Mirrors Blink's LineTruncator
// (inline/line_truncator.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// Not ported: TruncateLineInTheMiddle (<input type=file>), the line-clamp
// ellipsis, and the bidi split of an RTL <string> value in SetupEllipsis.
type LineTruncator struct {
	lineStyle      *css.Style
	availableWidth float64
	lineDirection  Direction
	fonts          text.FontConfig

	ellipsisText  string
	ellipsisWidth float64
}

// NewLineTruncator creates a truncator for a line whose style (the block's,
// or its first-line style on the first line) is lineStyle and whose line
// opportunity, less text-indent, is availableWidth. Mirrors the
// LineTruncator constructor (line_truncator.cc:30-40).
func NewLineTruncator(lineStyle *css.Style, availableWidth float64, lineDirection Direction, fonts text.FontConfig) *LineTruncator {
	return &LineTruncator{
		lineStyle:      lineStyle,
		availableWidth: availableWidth,
		lineDirection:  lineDirection,
		fonts:          fonts,
	}
}

// LineBoxPlacement is what TruncateLine needs from the line box builder to
// place the ellipsis: the text-align offset already folded into the
// children's inline offsets (Blink applies it to the line box after
// truncating, inline_layout_algorithm.cc:470-479), the line's baseline
// position, and the paint group of fragments on the line. no Blink analog:
// Blink reads these from InlineLayoutStateStack and FontHeight.
type LineBoxPlacement struct {
	LineOffsetForTextAlign float64
	MaxAscent              float64
	WritingDirection       WritingDirectionMode
	CentralBaseline        bool
	PaintGroup             *InlineItem
}

func (t *LineTruncator) isLtr() bool { return t.lineDirection != DirectionRTL }

// ComputeEllipsisText returns the <string> value of text-overflow, else
// U+2026, or "..." when the line style's font has no glyph for it. Mirrors
// LineTruncator::ComputeEllipsisText (line_truncator.cc:51-61).
func (t *LineTruncator) ComputeEllipsisText() string {
	if t.lineStyle.GetTextOverflow() == css.TextOverflowString {
		return t.lineStyle.GetTextOverflowString()
	}
	fontSize, _, _, _, _ := fontPropsFromStyle(t.lineStyle)
	if text.GlyphForCharacter(resolveFontPath(t.lineStyle, t.fonts), fontSize, '…') != 0 {
		return "…"
	}
	return "..."
}

// SetupEllipsis computes the ellipsis text and its inline size. Mirrors
// LineTruncator::SetupEllipsis (line_truncator.cc:63-96), without the bidi
// split of the text.
func (t *LineTruncator) SetupEllipsis(wdm WritingDirectionMode) {
	t.ellipsisText = t.ComputeEllipsisText()
	t.ellipsisWidth = t.measure(t.lineStyle, t.ellipsisText, wdm, false)
}

// measure is the inline size of s in style. withSpacing adds letter- and
// word-spacing, as the line breaker measured the line's text.
func (t *LineTruncator) measure(style *css.Style, s string, wdm WritingDirectionMode, withSpacing bool) float64 {
	fontSize, _, _, _, _ := fontPropsFromStyle(style)
	letterSpacing, wordSpacing := 0.0, 0.0
	if withSpacing {
		letterSpacing, wordSpacing = style.GetLetterSpacing(), style.GetWordSpacing()
	}
	return measureTextContent(s, fontSize, resolveFontPath(style, t.fonts), text.GetFontFeatures(style),
		letterSpacing, wordSpacing, isVerticalMeasurement(style, wdm))
}

// TruncateLine truncates lineBox, whose content is lineWidth wide, and
// returns the line's new inline size. Mirrors LineTruncator::TruncateLine
// (line_truncator.cc:173-240).
func (t *LineTruncator) TruncateLine(lineWidth float64, lineBox *LogicalLineItems, p LineBoxPlacement) float64 {
	t.SetupEllipsis(p.WritingDirection)

	// The in-flow children in visual order: Blink's line box holds them in
	// that order, louis14's in paint order (inline box backgrounds first).
	var inFlow []int
	for i := range *lineBox {
		if (*lineBox)[i].HasInFlowFragment() {
			inFlow = append(inFlow, i)
		}
	}
	if len(inFlow) == 0 {
		return lineWidth
	}
	sort.SliceStable(inFlow, func(a, b int) bool {
		return (*lineBox)[inFlow[a]].Offset.InlineOffset < (*lineBox)[inFlow[b]].Offset.InlineOffset
	})

	// Loop children from the logical last to the logical first to determine
	// where to place the ellipsis.
	ellipsized := -1
	var truncated *LogicalLineItem
	if t.isLtr() {
		first := inFlow[0]
		for k := len(inFlow) - 1; k >= 0; k-- {
			i := inFlow[k]
			if ok, tc := t.EllipsizeChild(lineWidth, i == first, &(*lineBox)[i], p); ok {
				ellipsized, truncated = i, tc
				break
			}
		}
	} else {
		first := inFlow[len(inFlow)-1]
		for _, i := range inFlow {
			if ok, tc := t.EllipsizeChild(lineWidth, i == first, &(*lineBox)[i], p); ok {
				ellipsized, truncated = i, tc
				break
			}
		}
	}

	// Abort if ellipsis could not be placed.
	if ellipsized < 0 {
		return lineWidth
	}

	// Truncate the text fragment if needed: hide the original and insert
	// the truncated one after it.
	if truncated != nil {
		toTruncate := (*lineBox)[ellipsized]
		if !t.isLtr() {
			truncated.Offset.InlineOffset += toTruncate.InlineSize - truncated.InlineSize
		}
		lineBox.InsertChild(ellipsized+1, *truncated)
		t.HideChild(&(*lineBox)[ellipsized])
		ellipsized++
	}

	ellipsisOffset := t.PlaceEllipsisNextTo(lineBox, ellipsized, p)
	return max(ellipsisOffset-p.LineOffsetForTextAlign+t.ellipsisWidth, lineWidth)
}

// PlaceEllipsisNextTo adds the ellipsis next to the child at index
// ellipsized and returns its inline offset. Mirrors
// LineTruncator::PlaceEllipsisNextTo (line_truncator.cc:98-140): the
// ellipsis is styled by the line style and sits on the line's baseline.
func (t *LineTruncator) PlaceEllipsisNextTo(lineBox *LogicalLineItems, ellipsized int, p LineBoxPlacement) float64 {
	child := (*lineBox)[ellipsized]
	offset := child.Offset.InlineOffset + child.InlineSize
	if !t.isLtr() {
		offset = child.Offset.InlineOffset - t.ellipsisWidth
	}
	_, ascent, textHeight := textFragmentMetrics(t.lineStyle, t.fonts, p.WritingDirection, p.CentralBaseline)
	node := child.Fragment.Node
	if child.LayoutResult != nil && node != nil {
		// The ellipsis is associated with the atomic inline, but it is text
		// of the block, not a box of the atomic inline's element.
		node = node.Parent
	}
	lineBox.AddChild(LogicalLineItem{
		Fragment: &PhysicalFragment{
			Size: oldSizeToGeom(ToPhysicalSize(LogicalSize{
				InlineSize: t.ellipsisWidth,
				BlockSize:  textHeight,
			}, p.WritingDirection.WM)),
			Type:             FragmentText,
			TextContent:      t.ellipsisText,
			Node:             node,
			Style:            t.lineStyle,
			WritingDirection: p.WritingDirection,
			PaintGroup:       p.PaintGroup,
			IsEllipsis:       true,
		},
		Offset:         LogicalOffset{InlineOffset: offset, BlockOffset: p.MaxAscent - ascent},
		RelativeOffset: child.RelativeOffset,
		InlineSize:     t.ellipsisWidth,
	})
	return offset
}

// HideChild hides child from paint, keeping its geometry. Mirrors
// LineTruncator::HideChild (line_truncator.cc:450-475). An atomic inline's
// fragment may be a cached layout result's, so it is hidden through a copy.
func (t *LineTruncator) HideChild(child *LogicalLineItem) {
	if child.LayoutResult != nil {
		// Need to propagate OOF descendants in this inline-block child.
		if len(child.LayoutResult.PropagatedOOFCandidates) > 0 {
			return
		}
		hidden := *child.Fragment
		child.Fragment = &hidden
	}
	child.Fragment.IsHiddenForPaint = true
}

// EllipsizeChild reports whether the ellipsis goes next to child; it may
// return the truncated child to put in its place, and may hide child.
// Mirrors LineTruncator::EllipsizeChild (line_truncator.cc:480-529).
func (t *LineTruncator) EllipsizeChild(lineWidth float64, isFirstChild bool, child *LogicalLineItem, p LineBoxPlacement) (bool, *LogicalLineItem) {
	// Can't place ellipsis if this child is completely outside of the box.
	inlineOffset := child.Offset.InlineOffset - p.LineOffsetForTextAlign
	childInlineOffset := inlineOffset
	if !t.isLtr() {
		childInlineOffset = lineWidth - (inlineOffset + child.InlineSize)
	}
	spaceForChild := t.availableWidth - childInlineOffset
	if spaceForChild <= 0 {
		// This child is outside of the content box, but we still need to
		// hide it.
		if !isFirstChild {
			t.HideChild(child)
		}
		return false, nil
	}

	// If child can fit in the space, truncate this line at its end.
	spaceForChild -= t.ellipsisWidth
	if spaceForChild >= child.InlineSize {
		return true, nil
	}

	// If not all of this child can fit, try to truncate.
	if ok, truncated := t.TruncateChild(spaceForChild, isFirstChild, child, p); ok {
		return true, truncated
	}

	// This child is partially in the box, but it can't be truncated to
	// fit. It should not be visible because an earlier sibling will be
	// truncated.
	if !isFirstChild {
		t.HideChild(child)
	}
	return false, nil
}

// TruncateChild truncates child to spaceForChild. It reports true, with a
// nil child, for a first child that cannot be truncated: the spec clips
// rather than ellipses the first character or atomic inline of a line.
// Mirrors LineTruncator::TruncateChild (line_truncator.cc:536-570).
func (t *LineTruncator) TruncateChild(spaceForChild float64, isFirstChild bool, child *LogicalLineItem, p LineBoxPlacement) (bool, *LogicalLineItem) {
	// If the space is not enough, try the next child.
	if spaceForChild <= 0 && !isFirstChild {
		return false, nil
	}

	// Only text fragments can be truncated.
	if child.InlineItem.Type != InlineItemText {
		return isFirstChild, nil
	}

	clusters := graphemeClusterEnds(child.Fragment.TextContent)
	offsetToFit := t.OffsetToFit(child, clusters, spaceForChild, p.WritingDirection)
	if offsetToFit == 0 || offsetToFit == len(clusters) {
		if !isFirstChild {
			return false, nil
		}
		if offsetToFit == 0 {
			offsetToFit = 1
		} else {
			offsetToFit--
		}
	}
	truncated := t.TruncateText(child, clusters, offsetToFit, p.WritingDirection)
	return true, &truncated
}

// keepsLogicalStart reports whether truncating child keeps the start of its
// text: the part next to the line's start edge is the text's logical start
// when the text runs in the line's direction, its logical end otherwise.
func (t *LineTruncator) keepsLogicalStart(child *LogicalLineItem) bool {
	return t.lineDirection == DirectionFromLevel(child.Fragment.BidiLevel)
}

// keptText is the text truncating child to n grapheme clusters keeps.
func (t *LineTruncator) keptText(child *LogicalLineItem, clusters []int, n int) string {
	s := child.Fragment.TextContent
	if t.keepsLogicalStart(child) {
		return s[:clusters[n-1]]
	}
	if n == len(clusters) {
		return s
	}
	return s[clusters[len(clusters)-n-1]:]
}

// OffsetToFit returns how many grapheme clusters of child, counted from the
// line's start edge, fit in width. Stands in for ShapeResult::OffsetToFit
// (platform/fonts/shaping/shape_result.cc:683-693), which counts whole
// glyph clusters; louis14 has no shape result, so it measures grapheme
// clusters (UAX #29).
func (t *LineTruncator) OffsetToFit(child *LogicalLineItem, clusters []int, width float64, wdm WritingDirectionMode) int {
	// The kept width grows with the cluster count: binary-search it.
	return sort.Search(len(clusters), func(n int) bool {
		return t.measure(child.Fragment.Style, t.keptText(child, clusters, n+1), wdm, true) > width+offsetToFitEpsilon
	})
}

// TruncateText returns child truncated to n grapheme clusters from the
// line's start edge, at child's offset. Mirrors LineTruncator::TruncateText
// (line_truncator.cc:572-586).
func (t *LineTruncator) TruncateText(child *LogicalLineItem, clusters []int, n int, wdm WritingDirectionMode) LogicalLineItem {
	kept := t.keptText(child, clusters, n)
	inlineSize := t.measure(child.Fragment.Style, kept, wdm, true)
	frag := *child.Fragment
	frag.TextContent = kept
	frag.Size = oldSizeToGeom(ToPhysicalSize(LogicalSize{
		InlineSize: inlineSize,
		BlockSize:  ToLogicalSize(geomSizeToOld(child.Fragment.Size), wdm.WM).BlockSize,
	}, wdm.WM))
	truncated := *child
	truncated.Fragment = &frag
	truncated.InlineSize = inlineSize
	return truncated
}

// offsetToFitEpsilon absorbs float error when comparing a measured width
// with the space for it.
const offsetToFitEpsilon = 1.0 / 128

// graphemeClusterEnds returns the byte offset just past each grapheme
// cluster of s.
func graphemeClusterEnds(s string) []int {
	var seg segmenter.Segmenter
	seg.InitWithString(s)
	var ends []int
	end := 0
	for it := seg.GraphemeIterator(); it.Next(); {
		end += len(string(it.Grapheme().Text))
		ends = append(ends, end)
	}
	return ends
}
