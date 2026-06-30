package render

// Tests for LOU-344 ::selection paint-time bookkeeping: given a DOM Range
// (html.Document.Selection) and a text fragment's originating *html.Node +
// node-local character range, compute the sub-range (if any) of that
// fragment's text that falls inside the selection. Mirrors Blink's
// FrameSelection boundary-point containment check, simplified to the single
// text-node-vs-range case this engine needs (see html.Range's doc comment
// in pkg/html/dom.go).

import (
	"louis14/pkg/html"
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
