package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestLineBreaker_StartsAfterBreakOpportunity constructs items equivalent to
// <span>p</span><span>ppp</span><span>p</span> — three adjacent, unspaced
// inline elements — and confirms startsAfterBreakOpportunity correctly
// reports NO break opportunity at either item boundary (the whole run
// "ppppp" is one unbreakable word per UAX#14/CSS Text, since there is no
// whitespace or hyphen anywhere in the run).
//
// A control case with a real space between two spans confirms the positive
// case: startsAfterBreakOpportunity must return true there.
//
// LOU-346: louis14's TextContent is already ONE global concatenated string
// spanning every InlineItem (mirroring Blink's LineBreaker::text_content_,
// core/layout/inline/line_breaker.cc ctor ~line 487-497 @
// b5c08e57c55fe62f7403812c91f4467a19c4f205), but line_breaker.go had no
// query analogous to Blink's break_iterator_.IsBreakable — this test proves
// that gap before the fix lands.
func TestLineBreaker_StartsAfterBreakOpportunity(t *testing.T) {
	// <div><span>p</span><span>ppp</span><span>p</span></div>
	span1 := makeNode("span", makeTextNode("p"))
	span2 := makeNode("span", makeTextNode("ppp"))
	span3 := makeNode("span", makeTextNode("p"))
	parent := makeNode("div", span1, span2, span3)

	spanStyle := makeStyle("display", "inline", "font-size", "16px")
	styles := map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "font-size", "16px"),
		span1:  spanStyle,
		span2:  spanStyle,
		span3:  spanStyle,
	}

	layoutParent := buildTestTree(parent, styles)
	itemsData := CollectInlines(layoutParent)

	if itemsData.TextContent != "ppppp" {
		t.Fatalf("TextContent: got %q, want %q", itemsData.TextContent, "ppppp")
	}

	lb := &LineBreaker{itemsData: itemsData}

	// Every text item's StartOffset in this unspaced run must report NO
	// break opportunity, except the very first (start of the whole run,
	// which is always a valid line start).
	var textStarts []int
	for _, item := range itemsData.Items {
		if item.Type == InlineItemText {
			textStarts = append(textStarts, item.StartOffset)
		}
	}
	if len(textStarts) != 3 {
		t.Fatalf("expected 3 text items, got %d (%v)", len(textStarts), textStarts)
	}

	if got := lb.startsAfterBreakOpportunity(textStarts[0]); got != true {
		t.Errorf("first text item (start of run) StartOffset=%d: got %v, want true (start of run is always a valid line start)", textStarts[0], got)
	}
	if got := lb.startsAfterBreakOpportunity(textStarts[1]); got != false {
		t.Errorf("\"ppp\" item StartOffset=%d: got %v, want false (no break opportunity — prior char is non-whitespace)", textStarts[1], got)
	}
	if got := lb.startsAfterBreakOpportunity(textStarts[2]); got != false {
		t.Errorf("trailing \"p\" item StartOffset=%d: got %v, want false (no break opportunity — prior char is non-whitespace)", textStarts[2], got)
	}
}

// TestLineBreaker_StartsAfterBreakOpportunity_ControlSpace confirms the
// positive control case: <span>p</span> <span>ppp</span> (a real space
// between the spans) DOES have a break opportunity at the second span's
// text start.
func TestLineBreaker_StartsAfterBreakOpportunity_ControlSpace(t *testing.T) {
	// <div><span>p</span> <span>ppp</span></div>
	span1 := makeNode("span", makeTextNode("p"))
	space := makeTextNode(" ")
	span2 := makeNode("span", makeTextNode("ppp"))
	parent := makeNode("div", span1, space, span2)

	spanStyle := makeStyle("display", "inline", "font-size", "16px")
	styles := map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "font-size", "16px"),
		span1:  spanStyle,
		span2:  spanStyle,
	}

	layoutParent := buildTestTree(parent, styles)
	itemsData := CollectInlines(layoutParent)

	if itemsData.TextContent != "p ppp" {
		t.Fatalf("TextContent: got %q, want %q", itemsData.TextContent, "p ppp")
	}

	lb := &LineBreaker{itemsData: itemsData}

	var textStarts []int
	for _, item := range itemsData.Items {
		if item.Type == InlineItemText {
			textStarts = append(textStarts, item.StartOffset)
		}
	}
	if len(textStarts) != 3 {
		t.Fatalf("expected 3 text items (\"p\", \" \", \"ppp\"), got %d (%v)", len(textStarts), textStarts)
	}

	if got := lb.startsAfterBreakOpportunity(textStarts[0]); got != true {
		t.Errorf("first text item (start of run) StartOffset=%d: got %v, want true (start of run is always a valid line start)", textStarts[0], got)
	}
	if got := lb.startsAfterBreakOpportunity(textStarts[2]); got != true {
		t.Errorf("\"ppp\" item after real space, StartOffset=%d: got %v, want true (break opportunity exists)", textStarts[2], got)
	}
}
