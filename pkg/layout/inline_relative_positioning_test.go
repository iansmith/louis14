package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-496 item 2: relative offsets of nested inline boxes accumulate. Every
// line item inside an inline box moves by that box's relative offset, so
// content inside nested boxes moves by the sum of their offsets; an inline
// box's own fragment moves by its ancestors' offsets plus its own; an atomic
// inline moves by its own offset plus its enclosing boxes'. Mirrors Blink's
// InlineLayoutStateStack::ApplyRelativePositioning (inline/
// inline_box_state.cc:940-984) after PlaceRelativePositionedItems (inline/
// inline_layout_algorithm.cc:225-235, :540-551) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.

// layoutInlineRelativeTest lays out root as a 200px wide block and returns
// its first line box.
func layoutInlineRelativeTest(t *testing.T, root *html.Node, styles map[*html.Node]*css.Style) *PhysicalFragment {
	t.Helper()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
		Build()
	result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(root, styles), space).Layout()
	t.Cleanup(func() {
		if t.Failed() {
			dumpFragmentTree(t, "", result.Fragment)
		}
	})
	for _, ch := range result.Fragment.Children {
		if ch.Fragment.Type == FragmentLineBox {
			return ch.Fragment
		}
	}
	t.Fatal("no line box")
	return nil
}

// lineChild returns the line box child matching match, with its offset.
func lineChild(t *testing.T, line *PhysicalFragment, what string, match func(*PhysicalFragment) bool) ChildLink {
	t.Helper()
	for _, ch := range line.Children {
		if match(ch.Fragment) {
			return ch
		}
	}
	t.Fatalf("no %s on the line", what)
	return ChildLink{}
}

func ahemInline(props ...string) *css.Style {
	return makeStyle(append([]string{"display", "inline", "font-family", "Ahem", "font-size", "10px", "line-height", "10px"}, props...)...)
}

func TestApplyRelativePositioning_NestedInlineOffsetsAccumulate(t *testing.T) {
	inner := makeNode("span", makeTextNode("x"))
	outer := makeNode("span", inner)
	root := makeNode("div", outer)
	styles := map[*html.Node]*css.Style{
		root:  makeStyle("display", "block", "font-family", "Ahem", "font-size", "10px", "line-height", "10px"),
		outer: ahemInline("position", "relative", "left", "10px", "top", "5px"),
		inner: ahemInline(),
	}
	line := layoutInlineRelativeTest(t, root, styles)
	text := lineChild(t, line, "text x", func(f *PhysicalFragment) bool {
		return f.Type == FragmentText && f.TextContent == "x"
	})
	if l, top := text.Offset.Left.Float64(), text.Offset.Top.Float64(); l != 10 || top != 5 {
		t.Errorf("text in a static span inside a relative span at (%v, %v), want (10, 5)", l, top)
	}

	// Both relative: the text moves by the sum.
	styles[inner] = ahemInline("position", "relative", "left", "3px", "top", "2px")
	line = layoutInlineRelativeTest(t, root, styles)
	text = lineChild(t, line, "text x", func(f *PhysicalFragment) bool {
		return f.Type == FragmentText && f.TextContent == "x"
	})
	if l, top := text.Offset.Left.Float64(), text.Offset.Top.Float64(); l != 13 || top != 7 {
		t.Errorf("text in nested relative spans at (%v, %v), want (13, 7)", l, top)
	}
	// The inner span's own box fragment moves by the outer offset too.
	innerBox := lineChild(t, line, "inner span box", func(f *PhysicalFragment) bool {
		return f.Type != FragmentText && f.Node == inner
	})
	if l := innerBox.Offset.Left.Float64(); l != 13 {
		t.Errorf("inner span box at left %v, want 13", l)
	}
}

func TestApplyRelativePositioning_AtomicInlineInsideRelativeSpan(t *testing.T) {
	ib := makeNode("div")
	outer := makeNode("span", ib)
	root := makeNode("div", outer)
	styles := map[*html.Node]*css.Style{
		root:  makeStyle("display", "block", "font-family", "Ahem", "font-size", "10px", "line-height", "10px"),
		outer: ahemInline("position", "relative", "left", "10px"),
		ib:    makeStyle("display", "inline-block", "width", "20px", "height", "10px", "position", "relative", "left", "4px"),
	}
	line := layoutInlineRelativeTest(t, root, styles)
	atomic := lineChild(t, line, "inline-block", func(f *PhysicalFragment) bool { return f.Node == ib })
	if l := atomic.Offset.Left.Float64(); l != 14 {
		t.Errorf("relative inline-block inside a relative span at left %v, want 14", l)
	}
}
