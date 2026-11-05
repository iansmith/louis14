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

	frag := layoutBlockAt200(t, root, styles)

	got := countBoxFragmentsForNode(frag, absSpan)
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
	// rbc/rb use display:inline (not ruby-base-container/ruby-base): in a
	// hand-built layout tree the ruby-internal display values are treated as
	// atomic inlines and their subtree is never recursed into, so the abs OOF
	// would not even be collected. display:inline takes the recursing open-tag
	// path, placing the abs OOF inside the ruby column's base sub-line — which
	// is the exact configuration that reproduces the drop. The rel ancestor is
	// the position:relative <rb>.
	leadX := makeTextNode("X")
	innerX := makeTextNode("X")
	absSpan := makeNode("span", innerX)
	rb := makeNode("rb", absSpan)
	rbc := makeNode("rbc", rb)
	ruby := makeNode("ruby", rbc)
	root := makeNode("div", leadX, ruby)

	styles := map[*html.Node]*css.Style{
		// root is position:relative so it is the containing block that resolves
		// the surfaced candidate: the rel <rb> has no box fragment yet (LOU-311),
		// so the candidate demotes past it to the nearest positioned ancestor —
		// here the root (the viewport/ICB in the WPT). Without a positioned root
		// the demoted candidate would propagate out of this top-level layout.
		root:    makeStyle("display", "block", "position", "relative", "width", "200px", "font-family", "Ahem", "font-size", "50px"),
		ruby:    makeStyle("display", "ruby", "font-family", "Ahem", "font-size", "50px"),
		rbc:     makeStyle("display", "inline", "font-family", "Ahem", "font-size", "50px"),
		rb:      makeStyle("display", "inline", "position", "relative", "font-family", "Ahem", "font-size", "50px"),
		absSpan: makeStyle("position", "absolute", "display", "block", "left", "0", "top", "-50px", "width", "50px", "height", "50px", "font-family", "Ahem", "font-size", "50px"),
	}

	frag := layoutBlockAt200(t, root, styles)

	got := countBoxFragmentsForNode(frag, absSpan)
	if got != 1 {
		t.Errorf("abs-pos box fragments for abs in ruby base: got %d, want 1 "+
			"(0 means the buried ruby-sub-line OOF was dropped — LOU-312)", got)
	}
}

// layoutBlockAt200 lays out root as a block in a 200x600 horizontal-tb LTR
// space and returns its fragment.
func layoutBlockAt200(t *testing.T, root *html.Node, styles map[*html.Node]*css.Style) *PhysicalFragment {
	t.Helper()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: 600}).
		Build()
	result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(root, styles), space).Layout()
	if result == nil || result.Fragment == nil {
		t.Fatal("layout returned nil result")
	}
	return result.Fragment
}

// ahem50Style is makeStyle plus Ahem at 50px.
func ahem50Style(kv ...string) *css.Style {
	return makeStyle(append(kv, "font-family", "Ahem", "font-size", "50px")...)
}

// absBox50 is the style of a 50x50 abs-pos box at the containing block's
// top-left.
func absBox50() *css.Style {
	return ahem50Style("position", "absolute", "display", "block", "left", "0", "top", "0", "width", "50px", "height", "50px")
}

// TestBlockLayout_AbsposInEmptyRuby_Surfaced: in Blink a ruby with no
// ruby-text gets no column result; its base items break as ordinary items of
// the outer line (LineBreaker::HandleRuby returns false, then AddItem +
// MoveToNextOf, inline/line_breaker.cc:3328-3331, :1115-1131 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An abs-pos element in the base
// is therefore an ordinary out-of-flow item of that line and is surfaced.
func TestBlockLayout_AbsposInEmptyRuby_Surfaced(t *testing.T) {
	absSpan := makeNode("span", makeTextNode("X"))
	rb := makeNode("rb", absSpan)
	ruby := makeNode("ruby", rb)
	root := makeNode("div", ruby)
	frag := layoutBlockAt200(t, root, map[*html.Node]*css.Style{
		root:    ahem50Style("display", "block", "position", "relative", "width", "200px"),
		ruby:    ahem50Style("display", "ruby"),
		rb:      ahem50Style("display", "inline"),
		absSpan: absBox50(),
	})
	if got := countBoxFragmentsForNode(frag, absSpan); got != 1 {
		t.Errorf("abs-pos box fragments for abs in empty ruby base: got %d, want 1 "+
			"(0 means the OOF buried in the empty ruby was dropped)", got)
	}
}

// TestBlockLayout_AbsposInEmptyRubyText_Surfaced: a ruby column with ruby-text
// always creates the line box, empty or not (LineBreaker::HandleRuby sets
// column_result->should_create_line_box, inline/line_breaker.cc:3608 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the block gets a line and an
// abs-pos element in the empty annotation is surfaced. Mirrors
// css-contain/contain-layout-005.
func TestBlockLayout_AbsposInEmptyRubyText_Surfaced(t *testing.T) {
	absSpan := makeNode("span", makeTextNode("X"))
	rt := makeNode("rt", absSpan)
	ruby := makeNode("ruby", rt)
	root := makeNode("div", ruby)
	frag := layoutBlockAt200(t, root, map[*html.Node]*css.Style{
		root:    ahem50Style("display", "block", "position", "relative", "width", "200px"),
		ruby:    ahem50Style("display", "ruby"),
		rt:      ahem50Style("display", "ruby-text"),
		absSpan: absBox50(),
	})
	if got := frag.Size.HeightF64(); got <= 0 {
		t.Errorf("block holding an empty ruby with ruby-text: height %v, want > 0 (the column creates the line box)", got)
	}
	if got := countBoxFragmentsForNode(frag, absSpan); got != 1 {
		t.Errorf("abs-pos box fragments for abs in empty ruby-text: got %d, want 1 "+
			"(0 means the empty column's line was treated as empty and the OOF dropped)", got)
	}
}

// TestBlockLayout_EmptyRubyWithoutTextCreatesNoLineBox: with no ruby-text
// Blink makes no column result (line_breaker.cc:1115-1131, :3328-3331 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so an empty ruby alone in a block
// leaves the line empty and the block zero-height (css-inline
// empty-span-size-002).
func TestBlockLayout_EmptyRubyWithoutTextCreatesNoLineBox(t *testing.T) {
	ruby := makeNode("ruby")
	root := makeNode("div", ruby)
	frag := layoutBlockAt200(t, root, map[*html.Node]*css.Style{
		root: ahem50Style("display", "block", "width", "200px"),
		ruby: ahem50Style("display", "ruby"),
	})
	if got := frag.Size.HeightF64(); got != 0 {
		t.Errorf("block holding only an empty ruby: height %v, want 0", got)
	}
}

// TestBlockLayout_RubyBaseContentCreatesLineBox: the base items of a ruby
// without ruby-text are ordinary items of the line (line_breaker.cc:
// 3328-3331, :1115-1131 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so any of
// them that creates a line box on its own creates one here too and the block
// must not collapse to zero height:
//   - a nested ruby with ruby-text (should_create_line_box, :3608);
//   - an atomic inline, even at zero width (HandleAtomicInline);
//   - a <br>, which inside ruby becomes a space text item
//     (inline_items_builder.cc:830-838, css-ruby #anon-gen-unbreak);
//   - an empty inline box with inline-axis padding (InlineItem::IsEmptyItem,
//     inline/inline_item.cc:32-67, :128-139).
func TestBlockLayout_RubyBaseContentCreatesLineBox(t *testing.T) {
	cases := []struct {
		name    string
		content func(styles map[*html.Node]*css.Style) *html.Node
	}{
		{"nested ruby with ruby-text", func(styles map[*html.Node]*css.Style) *html.Node {
			rt := makeNode("rt")
			inner := makeNode("ruby", rt)
			styles[rt] = ahem50Style("display", "ruby-text")
			styles[inner] = ahem50Style("display", "ruby")
			return inner
		}},
		{"zero-width inline-block", func(styles map[*html.Node]*css.Style) *html.Node {
			ib := makeNode("span")
			styles[ib] = ahem50Style("display", "inline-block", "width", "0", "height", "0")
			return ib
		}},
		{"br", func(styles map[*html.Node]*css.Style) *html.Node {
			br := makeNode("br")
			styles[br] = ahem50Style("display", "inline")
			return br
		}},
		{"padded empty span", func(styles map[*html.Node]*css.Style) *html.Node {
			sp := makeNode("span")
			styles[sp] = ahem50Style("display", "inline", "padding-left", "10px")
			return sp
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			rb := makeNode("rb", tc.content(styles))
			ruby := makeNode("ruby", rb)
			root := makeNode("div", ruby)
			styles[root] = ahem50Style("display", "block", "width", "200px")
			styles[ruby] = ahem50Style("display", "ruby")
			styles[rb] = ahem50Style("display", "inline")
			if got := layoutBlockAt200(t, root, styles).Size.HeightF64(); got <= 0 {
				t.Errorf("block holding a ruby whose base has a %s: height %v, want > 0", tc.name, got)
			}
		})
	}
}

// TestBlockLayout_FloatInEmptyRubyBase_Placed: a float in the base of a ruby
// without ruby-text is an ordinary float of the line (line_breaker.cc:
// 3328-3331, :1115-1131 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a line
// holding only that ruby still places it (LineBreaker::HandleFloat).
func TestBlockLayout_FloatInEmptyRubyBase_Placed(t *testing.T) {
	float := makeNode("span")
	rb := makeNode("rb", float)
	ruby := makeNode("ruby", rb)
	root := makeNode("div", ruby)
	frag := layoutBlockAt200(t, root, map[*html.Node]*css.Style{
		root:  ahem50Style("display", "block", "width", "200px"),
		ruby:  ahem50Style("display", "ruby"),
		rb:    ahem50Style("display", "inline"),
		float: ahem50Style("display", "block", "float", "left", "width", "50px", "height", "50px"),
	})
	if got := countBoxFragmentsForNode(frag, float); got != 1 {
		t.Errorf("box fragments for a float in an empty ruby base: got %d, want 1 (0 means the float was dropped)", got)
	}
}
