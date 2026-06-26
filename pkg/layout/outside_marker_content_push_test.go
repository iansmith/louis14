package layout

// LOU-337 (residual): an OUTSIDE list marker that is TALLER than its list
// item's first line — e.g. a list-style-image / content:url() image marker on a
// single line of text — must push the item's inline content DOWN so the first
// line's baseline aligns with the marker's baseline. This matches Chrome and
// the css-pseudo/marker-content-012 reference, which models the same outside
// image marker as an inline-block `::before` that grows the line box.
//
// Root cause this guards: UnpositionedListMarker.AddToBox already returns the
// content-push delta when the marker's ascent exceeds the content's first-line
// ascent, and the block-CONTENT marker path (positionOrPropagateListMarker)
// applies it. The INLINE-content path discarded the return value, so a tall
// outside image marker left the text ~4px too high (the residual 1.6% diff on
// marker-content-012; the image rows match but every text baseline from the
// outside-image row down was 4px high).
//
// Blink: BlockLayoutAlgorithm::PositionOrPropagateListMarker adds
// UnpositionedListMarker::AddToBox's content-adjust to the content block offset
// (block_layout_algorithm.cc / unpositioned_list_marker.cc:81-123
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// firstLineBoxChild returns the first FragmentLineBox child link of a box
// fragment (the list item's first line), or nil.
func firstLineBoxChild(frag *PhysicalFragment) *ChildLink {
	for i := range frag.Children {
		if frag.Children[i].Fragment != nil && frag.Children[i].Fragment.Type == FragmentLineBox {
			return &frag.Children[i]
		}
	}
	return nil
}

// markerBoxChild returns the marker's box child link (the only FragmentBox child
// of a list item whose own content is inline), or nil.
func markerBoxChild(frag *PhysicalFragment) *ChildLink {
	for i := range frag.Children {
		if frag.Children[i].Fragment != nil && frag.Children[i].Fragment.Type == FragmentBox {
			return &frag.Children[i]
		}
	}
	return nil
}

func TestOutsideTallImageMarkerPushesInlineContentDown(t *testing.T) {
	ol := &html.Node{Type: html.ElementNode, TagName: "ol"}
	li := &html.Node{Type: html.ElementNode, TagName: "li", Parent: ol}
	txt := &html.Node{Type: html.TextNode, Text: "alpha", Parent: li}
	li.Children = []*html.Node{txt}
	ol.Children = []*html.Node{li}

	styles := map[*html.Node]*css.Style{
		ol: makeStyle("display", "block", "counter-reset", "list-item 0"),
		// Outside image marker, 32x40 — deliberately TALLER than the single
		// "alpha" text line so the marker's baseline sits below the text's
		// natural baseline and the content must be pushed down.
		li: makeStyle(
			"display", "list-item",
			"list-style-position", "outside",
			"list-style-image", "url(green.png)",
		),
	}

	ctx := imgContextWithIntrinsic(32, 40)
	layoutRoot := buildTestTree(ol, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: 300}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: 300}).
		Build()

	result := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()

	liFrag := findFragmentByNode(result.Fragment, li)
	if liFrag == nil {
		t.Fatal("list item fragment not found")
	}

	line := firstLineBoxChild(liFrag)
	if line == nil {
		t.Fatal("list item has no line box child (inline content)")
	}
	marker := markerBoxChild(liFrag)
	if marker == nil {
		t.Fatal("list item has no marker box child (outside marker not placed)")
	}

	// The marker (40px tall image) is taller than the "alpha" line, so the
	// content's first line must be pushed DOWN. Before the fix the inline-content
	// path discarded AddToBox's content-push and the line sat at block offset 0.
	lineOff := line.Offset.TopF64()
	if lineOff <= 0 {
		t.Errorf("first line block offset = %.1f, want > 0 (tall outside marker must push inline content down)", lineOff)
	}

	// The marker itself stays anchored at the content-box top (block offset ~0);
	// it is the content that moves, not the marker.
	markerOff := marker.Offset.TopF64()
	if markerOff < -0.5 || markerOff > 0.5 {
		t.Errorf("marker block offset = %.1f, want ~0 (marker anchored at content-box top)", markerOff)
	}

	// The list item must grow to contain the taller marker: its block size is at
	// least the 40px marker height. A line-height-only box (~18px) means the
	// marker was ignored.
	if got := liFrag.Size.HeightF64(); got < 40 {
		t.Errorf("list item block size = %.1f, want >= 40 (must contain the 40px marker)", got)
	}

	// Baselines must coincide: marker baseline (its block size, since an image
	// marker's baseline is its bottom edge) measured from the marker's top must
	// equal the first line's baseline from the line box top, in document
	// coordinates. We assert the weaker, robust invariant that the line was
	// pushed down by roughly (markerHeight - lineAscent): the line offset should
	// be within the marker height.
	if lineOff > marker.Fragment.Size.Height.Float64() {
		t.Errorf("first line block offset = %.1f exceeds marker height %.1f; over-pushed",
			lineOff, marker.Fragment.Size.Height.Float64())
	}
}
