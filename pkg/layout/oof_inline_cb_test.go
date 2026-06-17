package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// countBoxFragmentsForNode walks the fragment tree and returns how many
// element-level box fragments wrap the given DOM node. Unlike
// findFragmentByNode (which returns the first match), this counts ALL
// occurrences — the signal for a duplicate/phantom out-of-flow candidate.
func countBoxFragmentsForNode(frag *PhysicalFragment, target *html.Node) int {
	if frag == nil {
		return 0
	}
	n := 0
	if frag.Type == FragmentBox && frag.LayoutNode != nil && frag.LayoutNode.DOMNode == target {
		n++
	}
	for _, ch := range frag.Children {
		n += countBoxFragmentsForNode(ch.Fragment, target)
	}
	return n
}

// TestBlockLayout_AbsposInRelativeInline_SingleCandidate is the LOU-312
// Phase 0 contract for the GENERAL (non-ruby) case.
//
// DOM mirrors css-ruby/abs-in-ruby-base-ref.html with ruby stripped:
//
//	<div><span class="rel">a<span class="abs">X</span></span></div>
//	.rel { position: relative }
//	.abs { position: absolute; left: 0; top: -1em }   (-1em == -50px @ 50px Ahem)
//
// An abs-pos element that descends from a position:relative INLINE must be
// surfaced as EXACTLY ONE out-of-flow candidate, resolved against that rel
// inline (its nearest positioned ancestor). The LOU-311 probes observed a
// phantom SECOND candidate resolving against the abs element's own box
// (block-origin 0), making the abs render twice (one correct, one off-area).
//
// Blink reference: an OOF is collected once at its static position and
// propagated to its containing block (OutOfFlowLayoutPart::LayoutOOFNodes,
// NGOutOfFlowPositionedNode::inline_container) — it is NOT re-collected during
// its own layout. So exactly one box fragment for the abs node must appear.
//
// RED EXPECTATION: if the bug is general, count == 2. If this passes (count
// == 1) on current code, the duplicate is ruby-sub-line-specific and LOU-312
// re-scopes to the ruby surfacing path (the css-ruby/abs-in-ruby reftests
// remain the hard contract).
func TestBlockLayout_AbsposInRelativeInline_SingleCandidate(t *testing.T) {
	aText := makeTextNode("a")
	xText := makeTextNode("X")
	absSpan := makeNode("span", xText)
	relSpan := makeNode("span", aText, absSpan)
	root := makeNode("div", relSpan)

	styles := map[*html.Node]*css.Style{
		root:    makeStyle("display", "block", "width", "200px", "font-family", "Ahem", "font-size", "50px"),
		relSpan: makeStyle("position", "relative", "display", "inline", "font-family", "Ahem", "font-size", "50px"),
		absSpan: makeStyle("position", "absolute", "display", "block", "left", "0", "top", "-50px", "width", "50px", "height", "50px", "font-family", "Ahem", "font-size", "50px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil result")
	}

	got := countBoxFragmentsForNode(result.Fragment, absSpan)
	if got != 1 {
		t.Errorf("abs-pos box fragments for the abs node: got %d, want 1 "+
			"(>1 means the duplicate/phantom OOF candidate — LOU-312)", got)
	}
}

// TestBlockLayout_AbsposInRubyBase_Surfaced is the LOU-312 Phase 0 RED test
// for the RE-SCOPED bug.
//
// DOM mirrors css-ruby/abs-in-ruby-base.html:
//
//	X<ruby><rbc><rb class="rel"><span class="abs">X</span></rb></rbc></ruby>
//	.rel { position: relative }
//	.abs { position: absolute; left: 0; top: -1em }   (-1em == -50px @ 50px Ahem)
//
// An abs-pos element buried in a ruby base sub-line MUST still be surfaced as
// an out-of-flow candidate (resolved against its nearest positioned ancestor,
// the rel <rb>). Step-2 instrumentation showed it is currently DROPPED: the
// outer line loop only walks the outer line.Results (which holds the single
// RubyColumn result), so the OOF buried in column.BaseLine.Results is never
// surfaced — count == 0 on current code.
//
// Blink reference: InlineLayoutAlgorithm propagates positioned descendants
// found inside ruby columns to the algorithm's OOF candidate list (ruby code
// vetted @ Chromium 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// RED EXPECTATION: count == 0 (dropped) on current code; the fix makes it 1.
func TestBlockLayout_AbsposInRubyBase_Surfaced(t *testing.T) {
	leadX := makeTextNode("X")
	innerX := makeTextNode("X")
	absSpan := makeNode("span", innerX)
	rb := makeNode("rb", absSpan)
	rbc := makeNode("rbc", rb)
	ruby := makeNode("ruby", rbc)
	root := makeNode("div", leadX, ruby)

	styles := map[*html.Node]*css.Style{
		root:    makeStyle("display", "block", "width", "200px", "font-family", "Ahem", "font-size", "50px"),
		ruby:    makeStyle("display", "ruby", "font-family", "Ahem", "font-size", "50px"),
		rbc:     makeStyle("display", "ruby-base-container", "font-family", "Ahem", "font-size", "50px"),
		rb:      makeStyle("display", "ruby-base", "position", "relative", "font-family", "Ahem", "font-size", "50px"),
		absSpan: makeStyle("position", "absolute", "display", "block", "left", "0", "top", "-50px", "width", "50px", "height", "50px", "font-family", "Ahem", "font-size", "50px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil result")
	}

	got := countBoxFragmentsForNode(result.Fragment, absSpan)
	if got != 1 {
		t.Errorf("abs-pos box fragments for abs in ruby base: got %d, want 1 "+
			"(0 means the buried ruby-sub-line OOF was dropped — LOU-312)", got)
	}
}
