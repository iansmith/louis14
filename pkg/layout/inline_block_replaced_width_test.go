package layout

// LOU-337: an inline-block with explicit width:0 + direction:rtl whose sole
// child is a REPLACED image (e.g. css-pseudo/marker-content-012's
// `.outside.image::before { display:inline-block; direction:rtl; width:0;
// content:url(<32x16 svg>) }`) must honor BOTH width:0 (content inline-size 0)
// and direction:rtl, so the over-wide replaced child overflows to the LEFT
// (negative inline offset) instead of being laid out at its 32px intrinsic
// width in LTR.
//
// Blink: an explicit definite inline-size wins over shrink-to-fit intrinsic
// content (LayoutNG ComputeInlineSizeForFragment / used-width resolution);
// direction is a standard CSS inherited property carried to children.

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// offsetOfNodeWithin returns the cumulative physical inline (x) offset of the
// first fragment wrapping target, measured relative to the fragment wrapping
// origin. Returns (offset, true) when both are found in ancestor/descendant
// relation. Used to read where a replaced child lands inside its inline-block.
func offsetOfNodeWithin(root *PhysicalFragment, origin, target *html.Node) (float64, bool) {
	originFrag := findFragmentByNode(root, origin)
	if originFrag == nil {
		return 0, false
	}
	var walk func(frag *PhysicalFragment, acc float64) (float64, bool)
	walk = func(frag *PhysicalFragment, acc float64) (float64, bool) {
		if frag.Node == target || (frag.LayoutNode != nil && frag.LayoutNode.DOMNode == target) {
			return acc, true
		}
		for _, ch := range frag.Children {
			if x, ok := walk(ch.Fragment, acc+ch.Offset.Left.Float64()); ok {
				return x, true
			}
		}
		return 0, false
	}
	return walk(originFrag, 0)
}

// TestInlineBlockWidth0ReplacedChildOverflowsLeft builds
//
//	block(div) > inline-block(width:0, direction:rtl) > img(intrinsic 32x16)
//
// and asserts the inline-block reports content inline-size 0 (width:0 honored)
// and lays its over-wide replaced child out overflowing to the left (negative
// inline offset ≈ -32, the rtl+start over-constrained idiom), matching Chrome.
func TestInlineBlockWidth0ReplacedChildOverflowsLeft(t *testing.T) {
	img := makeImgNode("green.svg")
	ib := makeNode("div", img)
	block := makeNode("div", ib)

	styles := map[*html.Node]*css.Style{
		block: makeStyle("display", "block", "width", "200px"),
		// inline-block with explicit width:0 + direction:rtl, mirroring the
		// marker-content-012-ref ::before. direction is set on both the
		// inline-block and the replaced child so this test isolates the
		// width:0-honoring facet from the direction-inheritance facet (the
		// latter is covered by the synthetic-image builder test).
		ib: makeStyle("display", "inline-block", "width", "0", "direction", "rtl"),
		// display:inline matches the UA stylesheet for <img> (and what
		// createSyntheticImage stamps on a content:url() pseudo image), so the
		// replaced child is an atomic inline of the inline-block's IFC.
		img: makeStyle("display", "inline", "direction", "rtl"),
	}

	ctx := imgContextWithIntrinsic(32, 16)
	layoutRoot := buildTestTree(block, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	ibFrag := findFragmentByNode(result.Fragment, ib)
	if ibFrag == nil {
		t.Fatal("inline-block fragment not found")
	}
	if got := ibFrag.Size.WidthF64(); got != 0 {
		t.Errorf("inline-block width: got %.1f, want 0 (explicit width:0 must win over the replaced child's 32px intrinsic)", got)
	}

	imgFrag := findFragmentByNode(result.Fragment, img)
	if imgFrag == nil {
		t.Fatal("img fragment not found")
	}
	// The replaced child must keep its 32px intrinsic width even though the
	// available inline space inside the width:0 inline-block is ~0. CSS 2.1
	// §10.3.2: an auto-width replaced element uses its intrinsic width as the
	// USED width and overflows; it is not clamped to fit. The LOU-337 bug sized
	// it to the ~1px available sliver.
	if got := imgFrag.Size.WidthF64(); got != 32 {
		t.Errorf("img width: got %.1f, want 32 (replaced child keeps intrinsic width and overflows; must not clamp to the ~0 available inline space)", got)
	}

	// And inside a 0-wide rtl inline-block the over-wide image must overflow to
	// the left: its inline offset relative to the inline-block content box is
	// negative. A 0 or positive offset means width:0 or direction:rtl was
	// dropped and the img sits at the left edge in LTR — the LOU-337 bug.
	imgX, ok := offsetOfNodeWithin(result.Fragment, ib, img)
	if !ok {
		t.Fatal("img fragment not found within inline-block")
	}
	if imgX >= 0 {
		t.Errorf("img inline offset within inline-block: got %.1f, want negative (replaced child overflows left in a width:0 rtl inline-block)", imgX)
	}
}

// TestSyntheticImageInheritsDirection covers the second LOU-337 facet: the
// synthetic <img> built by createSyntheticImage for content:url() must inherit
// `direction` (a CSS inherited property) from its originating pseudo/marker.
// Real DOM children get this from the cascade's ApplyInheritedProperties pass;
// the synthetic node is built post-cascade and would otherwise default to ltr,
// so a `direction:rtl` ::before image lays out in LTR and never overflows left.
func TestSyntheticImageInheritsDirection(t *testing.T) {
	parent := &html.Node{Type: html.ElementNode, TagName: "::before"}
	parentStyle := makeStyle("display", "inline-block", "width", "0", "direction", "rtl")
	b := &LayoutTreeBuilder{styles: map[*html.Node]*css.Style{parent: parentStyle}}

	imgLin := b.createSyntheticImage("green.svg", parent)
	if imgLin == nil || imgLin.style == nil {
		t.Fatal("createSyntheticImage returned no styled node")
	}
	if got, _ := imgLin.style.Get("direction"); got != "rtl" {
		t.Errorf("synthetic img direction = %q, want \"rtl\" (must inherit from the rtl pseudo parent)", got)
	}
	// Its own display must be preserved, not overwritten by inheritance
	// (display is not an inherited property, so this is a guard against the
	// copy clobbering the inline display the image needs to be an atomic inline).
	if got := imgLin.style.GetDisplay(); got != css.DisplayInline {
		t.Errorf("synthetic img display = %q, want \"inline\"", got)
	}
}
