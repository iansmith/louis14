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

// TestInlineLayout_UnspacedSpans_NormalWrap_Overflows is the Checkpoint-3
// normal-mode (non-min-content) regression case: a fixed-width container
// just narrower than an unspaced multi-span run's full width. Per CSS
// Text/UAX#14 there is no break opportunity between <span>p</span>,
// <span>ppp</span>, <span>p</span> (no whitespace anywhere in the run), so
// the whole "ppppp" run must overflow the container as ONE line rather
// than wrap at a span boundary. LOU-346's breakTextAtWord fix (Checkpoint
// 2) applies in both LineBreakerContent and LineBreakerMinContent modes
// (same code path, line_breaker.go's overflow-detection branch runs in
// both) — this test confirms normal-mode wrapping specifically, not just
// min-content sizing.
func TestInlineLayout_UnspacedSpans_NormalWrap_Overflows(t *testing.T) {
	// <div><span>p</span><span>ppp</span><span>p</span></div>, Ahem 80px:
	// "ppppp" = 5 * 80px = 400px. Container width 300px — narrower than the
	// full run but wider than any single span alone, so a per-item-
	// independent breaker would (bug) wrap at a span boundary; the correct
	// behavior is one 400px-wide overflowing line.
	span1 := makeNode("span", makeTextNode("p"))
	span2 := makeNode("span", makeTextNode("ppp"))
	span3 := makeNode("span", makeTextNode("p"))
	parent := makeNode("div", span1, span2, span3)

	spanStyle := makeStyle("display", "inline", "font-family", "Ahem", "font-size", "80px")
	styles := map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "font-family", "Ahem", "font-size", "80px", "width", "300px"),
		span1:  spanStyle,
		span2:  spanStyle,
		span3:  spanStyle,
	}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 300)

	if len(lineBoxes) != 1 {
		t.Fatalf("expected the unbreakable \"ppppp\" run to stay on ONE overflowing line, got %d lines", len(lineBoxes))
	}

	var allText string
	for _, child := range lineBoxes[0].Children {
		if child.Fragment.Type == FragmentText {
			allText += child.Fragment.TextContent
		}
	}
	if allText != "ppppp" {
		t.Errorf("line 0 text: got %q, want %q (all 5 characters on the single overflowing line)", allText, "ppppp")
	}
}

// TestInlineLayout_UnspacedSpans_BreakAnywhere_BreaksAtItemBoundary is the
// Checkpoint-3 breakTextAtCharacter check. Unlike breakTextAtWord,
// word-break:break-all / overflow-wrap:anywhere makes EVERY character
// position a valid break opportunity per CSS Text 3 §4.1 — including an
// item/element boundary, since that's just another character position.
// breakTextAtCharacter's existing per-item independence is therefore
// correct here (no startsAfterBreakOpportunity gating needed): breaking at
// <span>p</span>|<span>ppp</span> is legitimate under break-all, unlike
// the plain-word case in breakTextAtWord. This test confirms the run still
// glues into full, uninterrupted text across the fragmented line boxes
// (no dropped/duplicated characters at the item boundary) rather than
// asserting "must not break" — the correctness bar is different for this
// mode.
func TestInlineLayout_UnspacedSpans_BreakAnywhere_BreaksAtItemBoundary(t *testing.T) {
	span1 := makeNode("span", makeTextNode("p"))
	span2 := makeNode("span", makeTextNode("ppp"))
	span3 := makeNode("span", makeTextNode("p"))
	parent := makeNode("div", span1, span2, span3)

	spanStyle := makeStyle("display", "inline", "font-family", "Ahem", "font-size", "80px")
	styles := map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "font-family", "Ahem", "font-size", "80px",
			"width", "300px", "overflow-wrap", "anywhere"),
		span1: spanStyle,
		span2: spanStyle,
		span3: spanStyle,
	}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 300)

	if len(lineBoxes) == 0 {
		t.Fatal("expected at least 1 line")
	}
	for i, lb := range lineBoxes {
		if lb.Size.WidthF64() > 300.0+0.01 {
			t.Errorf("line %d width %.1f exceeds available width 300px", i, lb.Size.WidthF64())
		}
	}

	var allText string
	for _, lb := range lineBoxes {
		for _, child := range lb.Children {
			if child.Fragment.Type == FragmentText {
				allText += child.Fragment.TextContent
			}
		}
	}
	if allText != "ppppp" {
		t.Errorf("concatenated text across all lines: got %q, want %q (no dropped/duplicated characters at item boundaries)", allText, "ppppp")
	}
}
