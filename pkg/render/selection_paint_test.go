package render

// Tests for LOU-344 ::selection paint-time bookkeeping: given a DOM Range
// (html.Document.Selection) and a text fragment's originating *html.Node +
// node-local character range, compute the sub-range (if any) of that
// fragment's text that falls inside the selection. Mirrors Blink's
// FrameSelection boundary-point containment check, simplified to the single
// text-node-vs-range case this engine needs (see html.Range's doc comment
// in pkg/html/dom.go).

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"testing"
)

func textNode(text string) *html.Node {
	return &html.Node{Type: html.TextNode, Text: text}
}

func TestSelectionOverlapForTextNode_FullySelected(t *testing.T) {
	// div::selection spans selectNodeContents(div) — start=(div,0), end=(div,1)
	// (div has one text-node child). The text node itself is fully covered.
	div := &html.Node{Type: html.ElementNode, TagName: "div"}
	tn := textNode("Selected Text")
	div.Children = []*html.Node{tn}
	tn.Parent = div

	rng := &html.Range{StartContainer: div, StartOffset: 0, EndContainer: div, EndOffset: 1}

	start, end, ok := selectionOverlapForTextNode(rng, tn, 0, len(tn.Text))
	if !ok {
		t.Fatal("expected overlap for a text node fully inside selectNodeContents(parent)")
	}
	if start != 0 || end != len(tn.Text) {
		t.Errorf("got [%d,%d), want [0,%d)", start, end, len(tn.Text))
	}
}

func TestSelectionOverlapForTextNode_PartialRange(t *testing.T) {
	// selection-originating-decoration-color.html: range.setStart(textNode,1);
	// range.setEnd(textNode,4) on a single "ppppp" text node (len 5).
	tn := textNode("ppppp")
	rng := &html.Range{StartContainer: tn, StartOffset: 1, EndContainer: tn, EndOffset: 4}

	start, end, ok := selectionOverlapForTextNode(rng, tn, 0, len(tn.Text))
	if !ok {
		t.Fatal("expected overlap")
	}
	if start != 1 || end != 4 {
		t.Errorf("got [%d,%d), want [1,4)", start, end)
	}
}

// TestSelectionOverlapForTextNode_UTF16OffsetNonASCII reproduces
// selection-background-painting-order.html's setStart(node,0)/setEnd(node,3)
// on the text node "ii∫∫∫" (∫ = U+222B, 1 UTF-16 code unit but 3 UTF-8
// bytes). Per DOM §5.4, Range offsets into a text node are UTF-16 code-unit
// offsets (html.Range's doc comment) — offset 3 means "ii∫" (3 JS
// characters), NOT byte 3, which lands mid-way through the first ∫'s UTF-8
// encoding ("ii\xe2") and previously corrupted both the selected and
// unselected text fed to the shaper (observed as notdef/tofu glyph boxes
// for the unselected remainder). selectionOverlapForTextNode must return
// byte offsets that fall on rune boundaries.
func TestSelectionOverlapForTextNode_UTF16OffsetNonASCII(t *testing.T) {
	tn := textNode("ii∫∫∫")
	rng := &html.Range{StartContainer: tn, StartOffset: 0, EndContainer: tn, EndOffset: 3}

	start, end, ok := selectionOverlapForTextNode(rng, tn, 0, len(tn.Text))
	if !ok {
		t.Fatal("expected overlap")
	}
	wantEnd := len("ii∫") // byte length of the 3-UTF-16-unit prefix "ii∫"
	if start != 0 || end != wantEnd {
		t.Errorf("got [%d,%d), want [0,%d) (= byte length of \"ii∫\")", start, end, wantEnd)
	}
	// The split must land on UTF-8 rune boundaries, not mid-encoding.
	selected := tn.Text[start:end]
	unselected := tn.Text[end:]
	if selected != "ii∫" {
		t.Errorf("selected segment = %q, want %q", selected, "ii∫")
	}
	if unselected != "∫∫" {
		t.Errorf("unselected segment = %q, want %q", unselected, "∫∫")
	}
}

func TestSelectionOverlapForTextNode_FragmentSubrange(t *testing.T) {
	// A single DOM text node line-wrapped into two fragments: fragment A
	// covers node-local [0,5), fragment B covers [5,11). Selection covers
	// node-local [3,8) (spans both fragments) — each fragment's overlap
	// query must clip to its own [fragStart,fragEnd) and return
	// FRAGMENT-LOCAL offsets (not node-local), since drawText slices
	// box.Text (the fragment's own string), not the whole node's text.
	tn := textNode("Hello world") // len 11; fragment split at index 5
	rng := &html.Range{StartContainer: tn, StartOffset: 3, EndContainer: tn, EndOffset: 8}

	// Fragment A: node-local [0,5) ("Hello")
	startA, endA, okA := selectionOverlapForTextNode(rng, tn, 0, 5)
	if !okA {
		t.Fatal("expected overlap in fragment A")
	}
	if startA != 3 || endA != 5 {
		t.Errorf("fragment A: got [%d,%d), want [3,5) (fragment-local)", startA, endA)
	}

	// Fragment B: node-local [5,11) ("world")
	startB, endB, okB := selectionOverlapForTextNode(rng, tn, 5, 11)
	if !okB {
		t.Fatal("expected overlap in fragment B")
	}
	if startB != 0 || endB != 3 {
		t.Errorf("fragment B: got [%d,%d), want [0,3) (fragment-local: node-local [5,8) minus fragStart 5)", startB, endB)
	}
}

func TestSelectionOverlapForTextNode_NoOverlap(t *testing.T) {
	tn := textNode("Hello world")
	other := textNode("unrelated")
	rng := &html.Range{StartContainer: other, StartOffset: 0, EndContainer: other, EndOffset: len(other.Text)}

	_, _, ok := selectionOverlapForTextNode(rng, tn, 0, len(tn.Text))
	if ok {
		t.Error("expected no overlap for a range targeting a different text node")
	}
}

func TestSelectionOverlapForTextNode_NilRange(t *testing.T) {
	tn := textNode("Hello")
	_, _, ok := selectionOverlapForTextNode(nil, tn, 0, len(tn.Text))
	if ok {
		t.Error("expected no overlap for a nil range")
	}
}

// TestSelectionOverlapForTextNode_NestedAncestorContainer mirrors
// active-selection-018.html: selectNodeContents(div#parent) where
// div#parent contains "Selected Text " (a direct text-node child) followed
// by <span>FAIL</span> (a TWO-levels-deep text node — div > span > #text).
// The selection boundary container is the GRANDPARENT of the span's text
// node, not its direct parent — nodeLocalBoundary must walk up the
// ancestor chain to find that the <span> subtree itself is included.
func TestSelectionOverlapForTextNode_NestedAncestorContainer(t *testing.T) {
	div := &html.Node{Type: html.ElementNode, TagName: "div"}
	leadingText := textNode("Selected Text ")
	leadingText.Parent = div
	span := &html.Node{Type: html.ElementNode, TagName: "span"}
	span.Parent = div
	spanText := textNode("FAIL")
	spanText.Parent = span
	span.Children = []*html.Node{spanText}
	div.Children = []*html.Node{leadingText, span}

	// selectNodeContents(div): start=(div,0), end=(div,2) (2 children).
	rng := &html.Range{StartContainer: div, StartOffset: 0, EndContainer: div, EndOffset: 2}

	start, end, ok := selectionOverlapForTextNode(rng, spanText, 0, len(spanText.Text))
	if !ok {
		t.Fatal("expected the nested span's text node to be included in selectNodeContents(div#parent)")
	}
	if start != 0 || end != len(spanText.Text) {
		t.Errorf("got [%d,%d), want [0,%d) (span text fully selected)", start, end, len(spanText.Text))
	}
}

// TestSelectionBackgroundRectY_UsesSiblingBackgroundFragment reproduces
// selection-contenteditable-011.html's ~3px red sliver: a
// `display: inline` div with `background-color: red` (own background) and
// `font-size: 300%` (so line-height: normal's 1.2x default exceeds the
// font's em box). pkg/layout/inline_layout.go emits that own background
// as a SEPARATE sibling PhysicalFragment from the text fragment, sized
// max(em box, line box) and anchored at the line-box top — taller than,
// and starting higher than, the text fragment's own (always em-box-sized)
// box. Using the text fragment's own (Y, Height) for the ::selection
// background rect left a gap at the top where the originating red
// background showed through. selectionBackgroundRectY must return the
// SIBLING background fragment's (Y, Height), not the text box's own.
func TestSelectionBackgroundRectY_UsesSiblingBackgroundFragment(t *testing.T) {
	divNode := &html.Node{Type: html.ElementNode, TagName: "div"}
	parent := &layout.Box{Node: nil} // anonymous block, like the real tree

	bgBox := &layout.Box{Node: divNode, Parent: parent, Y: 8.00, Height: 50.83} // line-box-anchored
	textBox := &layout.Box{Node: divNode, Parent: parent, Y: 11.92, Height: 48.00, Text: "Selected Text"}
	parent.Children = []*layout.Box{bgBox, textBox}

	y, height := selectionBackgroundRectY(textBox)
	if y != bgBox.Y || height != bgBox.Height {
		t.Errorf("selectionBackgroundRectY = (%v, %v), want the sibling background fragment's (%v, %v) — using the text box's own (%v, %v) leaves the top sliver of the originating background exposed",
			y, height, bgBox.Y, bgBox.Height, textBox.Y, textBox.Height)
	}
}

// TestSelectionBackgroundRectY_NoSiblingBackgroundFallsBackToOwnBox covers
// the common case (no inline background-fragment sibling exists, e.g. the
// originating element has no background-color of its own — most LOU-344
// target tests): selectionBackgroundRectY must fall back to the text
// box's own (Y, Height) rather than panicking or returning zero.
func TestSelectionBackgroundRectY_NoSiblingBackgroundFallsBackToOwnBox(t *testing.T) {
	divNode := &html.Node{Type: html.ElementNode, TagName: "div"}
	parent := &layout.Box{Node: nil}
	textBox := &layout.Box{Node: divNode, Parent: parent, Y: 11.92, Height: 48.00, Text: "Selected Text"}
	parent.Children = []*layout.Box{textBox}

	y, height := selectionBackgroundRectY(textBox)
	if y != textBox.Y || height != textBox.Height {
		t.Errorf("selectionBackgroundRectY = (%v, %v), want the text box's own (%v, %v) when no background-fragment sibling exists",
			y, height, textBox.Y, textBox.Height)
	}
}

// TestSelectionBackgroundRectY_NilBox guards the nil-safety the rest of
// computeSelectionSegments relies on (it's only ever called with a
// non-nil box in practice, but defends against the zero value the same
// way selectionOverlapForTextNode's nil-Range case does).
func TestSelectionBackgroundRectY_NilBox(t *testing.T) {
	y, height := selectionBackgroundRectY(nil)
	if y != 0 || height != 0 {
		t.Errorf("selectionBackgroundRectY(nil) = (%v, %v), want (0, 0)", y, height)
	}
}

// TestLineStartX_FindsNearestAncestorWithStyle mirrors
// drawTextWithTabs's line-start lookup (CSS Text 3 §4.2: tab stops are
// measured from the line's start, i.e. the containing block's content
// edge): lineStartX walks up from a text fragment box to the nearest
// ancestor carrying a Style and returns its content-box left edge
// (X + Padding.Left + Border.Left). measureSegmentVisualWidthWithTabs
// (LOU-344's active-selection-063.html fix) relies on this returning the
// SAME line start drawTextWithTabs would use, or the ::selection
// highlight rect's tab-stop phase would disagree with where the glyphs
// actually land.
func TestLineStartX_FindsNearestAncestorWithStyle(t *testing.T) {
	lineBox := &layout.Box{X: 8, Style: css.NewStyle()}
	anonBlock := &layout.Box{X: 0, Parent: lineBox} // no Style — must be skipped
	textBox := &layout.Box{X: 33, Parent: anonBlock}

	got := lineStartX(textBox)
	if got != 8 {
		t.Errorf("lineStartX = %v, want 8 (the nearest ancestor WITH a Style, skipping the anonymous no-Style box)", got)
	}
}

// TestLineStartX_PaddingAndBorderOffsetContentEdge covers the
// Padding.Left/Border.Left contribution: the line start is the CONTENT
// edge, not the border edge.
func TestLineStartX_PaddingAndBorderOffsetContentEdge(t *testing.T) {
	lineBox := &layout.Box{
		X:       8,
		Style:   css.NewStyle(),
		Padding: css.BoxEdge{Left: 5},
		Border:  css.BoxEdge{Left: 2},
	}
	textBox := &layout.Box{X: 15, Parent: lineBox}

	got := lineStartX(textBox)
	want := 8.0 + 5.0 + 2.0
	if got != want {
		t.Errorf("lineStartX = %v, want %v (X + Padding.Left + Border.Left)", got, want)
	}
}

// TestLineStartX_NoStyledAncestorFallsBackToOwnX covers the case
// drawTextWithTabs's original loop also handled: no ancestor with a
// Style at all (lineStart initialized to box.X, loop never finds a
// match) — falls back to the box's own X rather than 0.
func TestLineStartX_NoStyledAncestorFallsBackToOwnX(t *testing.T) {
	textBox := &layout.Box{X: 42}
	got := lineStartX(textBox)
	if got != 42 {
		t.Errorf("lineStartX = %v, want 42 (own X when no styled ancestor exists)", got)
	}
}

// TestTargetTextOverlapsForTextNode_SingleRange mirrors
// TestSelectionOverlapForTextNode_FullySelected's shape for the LOU-349
// multi-range entry point: one Range, fully covering the fragment, should
// produce exactly one [start,end) span.
func TestTargetTextOverlapsForTextNode_SingleRange(t *testing.T) {
	tn := textNode("match me")
	ranges := []*html.Range{
		{StartContainer: tn, StartOffset: 0, EndContainer: tn, EndOffset: len(tn.Text)},
	}
	spans := targetTextOverlapsForTextNode(ranges, tn, 0, len(tn.Text))
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1: %+v", len(spans), spans)
	}
	if spans[0].start != 0 || spans[0].end != len(tn.Text) {
		t.Errorf("span = [%d,%d), want [0,%d)", spans[0].start, spans[0].end, len(tn.Text))
	}
}

// TestTargetTextOverlapsForTextNode_NonOverlapping covers
// target-text-003.html's shape: two independent directives matching
// disjoint sub-ranges of the SAME text node must produce two separate,
// non-contiguous spans (not merged into one).
func TestTargetTextOverlapsForTextNode_NonOverlapping(t *testing.T) {
	tn := textNode("match me and you") // len 16
	ranges := []*html.Range{
		{StartContainer: tn, StartOffset: 0, EndContainer: tn, EndOffset: 5},  // "match"
		{StartContainer: tn, StartOffset: 9, EndContainer: tn, EndOffset: 16}, // "and you"
	}
	spans := targetTextOverlapsForTextNode(ranges, tn, 0, len(tn.Text))
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	if spans[0].start != 0 || spans[0].end != 5 {
		t.Errorf("span 0 = [%d,%d), want [0,5)", spans[0].start, spans[0].end)
	}
	if spans[1].start != 9 || spans[1].end != 16 {
		t.Errorf("span 1 = [%d,%d), want [9,16)", spans[1].start, spans[1].end)
	}
}

// TestTargetTextOverlapsForTextNode_OverlappingMerged mirrors
// target-text-009.html's shape: "match me and me" with selectors "match me"
// ([0,8)) and "me and me" ([6,16)) overlapping at [6,8) — must merge into
// ONE continuous [0,16) span so the highlight paints as a single
// uninterrupted band (the WPT reference renders the whole phrase as one
// color block, not two abutting/overlapping rects).
func TestTargetTextOverlapsForTextNode_OverlappingMerged(t *testing.T) {
	tn := textNode("match me and me") // len 15
	ranges := []*html.Range{
		{StartContainer: tn, StartOffset: 0, EndContainer: tn, EndOffset: 8},  // "match me"
		{StartContainer: tn, StartOffset: 6, EndContainer: tn, EndOffset: 15}, // "me and me"
	}
	spans := targetTextOverlapsForTextNode(ranges, tn, 0, len(tn.Text))
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1 (merged): %+v", len(spans), spans)
	}
	if spans[0].start != 0 || spans[0].end != 15 {
		t.Errorf("merged span = [%d,%d), want [0,15)", spans[0].start, spans[0].end)
	}
}

// TestTargetTextOverlapsForTextNode_NoRanges covers the "no target-text
// context configured" fast path: nil/empty ranges slice yields no spans.
func TestTargetTextOverlapsForTextNode_NoRanges(t *testing.T) {
	tn := textNode("hello")
	if spans := targetTextOverlapsForTextNode(nil, tn, 0, len(tn.Text)); len(spans) != 0 {
		t.Errorf("got %d spans for nil ranges, want 0", len(spans))
	}
	if spans := targetTextOverlapsForTextNode([]*html.Range{}, tn, 0, len(tn.Text)); len(spans) != 0 {
		t.Errorf("got %d spans for empty ranges, want 0", len(spans))
	}
}
