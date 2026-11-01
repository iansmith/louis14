package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
)

// soChild is a child fragment and its logical offset in a test builder.
type soChild struct {
	fragment *PhysicalFragment
	offset   LogicalOffset
}

// soAt places frag at the given logical offset.
func soAt(inline, block float64, frag *PhysicalFragment) soChild {
	return soChild{fragment: frag, offset: LogicalOffset{InlineOffset: inline, BlockOffset: block}}
}

// soBuilder returns a builder for a w x h box styled with kv, wired to a
// LayoutInputNode as BlockLayoutAlgorithm wires its builder (SetLayoutNode),
// with children's relative offsets resolved against its own size.
func soBuilder(w, h float64, kv ...string) *BoxFragmentBuilder {
	style := makeStyle(append([]string{"display", "block"}, kv...)...)
	node := makeNode("div")
	lin := buildTestTree(node, map[*html.Node]*css.Style{node: style})
	b := NewBoxFragmentBuilder(NewWritingDirectionMode(style))
	b.SetLayoutNode(lin)
	b.SetSize(LogicalSize{InlineSize: w, BlockSize: h})
	b.SetChildAvailableSize(LogicalSize{InlineSize: w, BlockSize: h})
	return b
}

// soBox builds a w x h box styled with kv holding children.
func soBox(w, h float64, kv []string, children ...soChild) *PhysicalFragment {
	b := soBuilder(w, h, kv...)
	for _, c := range children {
		b.AddChild(c.fragment, c.offset)
	}
	return b.Build().Fragment
}

// soColumn builds a column fragmentainer (BoxTypeColumn) of the given size
// for a multicol styled with kv, holding children.
func soColumn(w, h float64, kv []string, children ...soChild) *PhysicalFragment {
	b := soBuilder(w, h, append([]string{"column-count", "2"}, kv...)...)
	b.SetBoxType(BoxTypeColumn)
	for _, c := range children {
		b.AddChild(c.fragment, c.offset)
	}
	return b.Build().Fragment
}

// TestColumnBlockEndScrollableOverflow (LOU-469) replaces
// TestFragmentainerBlockSizeForFragmentation_ClipAndOutOfFlow: a column's
// BlockEndScrollableOverflow (logical_box_fragment.cc:86-91 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) is the block-end of the
// scrollable overflow ScrollableOverflowCalculator gathers from its
// children (scrollable_overflow_calculator.cc:240-336 @ a9f50e522efa):
// each child contributes its border box, plus its own scrollable overflow
// unless it clips both axes or has layout containment, clipped per axis,
// expanded by overflow-clip-margin, moved by its relative offset (Blink
// adds it to the child offset, box_fragment_builder.cc:262-267 @
// a9f50e522efa) and mapped through its transform. The 5px column sits
// under every child, so only the children decide.
func TestColumnBlockEndScrollableOverflow(t *testing.T) {
	tall := func() *PhysicalFragment { return soBox(50, 80, nil) }
	// child is a 50x20 box styled with kv whose 80px child overflows it.
	child := func(kv ...string) *PhysicalFragment {
		return soBox(50, 20, kv, soAt(0, 0, tall()))
	}
	for _, tc := range []struct {
		name  string
		child *PhysicalFragment
		want  float64
	}{
		{"visible overflow extends", child(), 90},
		{"overflow:hidden clips", child("overflow", "hidden"), 30},
		{"overflow-y:clip clips the block axis", child("overflow-y", "clip"), 30},
		{"overflow-x:clip leaves the block axis", child("overflow-x", "clip"), 90},
		{"overflow-clip-margin extends the clip", child("overflow", "clip", "overflow-clip-margin", "5px"), 35},
		{"layout containment ignores overflow", child("contain", "layout"), 30},
		{"abspos contributes", child("position", "absolute"), 90},
		{"relative offset moves it", soBox(50, 20, []string{"position", "relative", "top", "15px"}), 45},
		{"translate moves it", soBox(50, 20, []string{"transform", "translateY(30px)"}), 60},
		{"scale grows it about its origin", soBox(50, 20, []string{"transform", "scale(2)"}), 40},
	} {
		col := soColumn(50, 5, nil, soAt(0, 10, tc.child))
		if got := NewLogicalFragment(col.WritingDirection, col).BlockEndScrollableOverflow(); got != tc.want {
			t.Errorf("%s: BlockEndScrollableOverflow = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestColumnBlockEndScrollableOverflow_VerticalLR (LOU-469): the block end
// is read in the column's writing mode (logical_box_fragment.cc:86-91 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); in vertical-lr the block axis
// is physical x.
func TestColumnBlockEndScrollableOverflow_VerticalLR(t *testing.T) {
	vlr := []string{"writing-mode", "vertical-lr"}
	// Logical sizes (inline, block): the 80px block-size of inner is
	// physical width, overflowing box's 20px.
	inner := soBox(50, 80, vlr)
	box := soBox(50, 20, vlr, soAt(0, 0, inner))
	col := soColumn(50, 5, vlr, soAt(0, 10, box))
	if got := NewLogicalFragment(col.WritingDirection, col).BlockEndScrollableOverflow(); got != 90 {
		t.Errorf("BlockEndScrollableOverflow = %v, want 90", got)
	}
}

// TestScrollableOverflow_LineBoxItems (LOU-469): a line box contributes its
// own rect and its items — text and atomic inlines — their rects in the
// container (ScrollableOverflowCalculator::AddItemsInternal,
// scrollable_overflow_calculator.cc:126-182 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 16px text run overhanging
// its 10px line reaches past the line box.
func TestScrollableOverflow_LineBoxItems(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	text := &PhysicalFragment{Type: FragmentText, Size: geometry.PhysicalSizeFromF64Round(30, 16), WritingDirection: wdm}
	line := NewBoxFragmentBuilder(wdm)
	line.SetSize(LogicalSize{InlineSize: 50, BlockSize: 10})
	line.AddChild(text, LogicalOffset{BlockOffset: -2})
	lineFrag := line.Build().Fragment
	lineFrag.Type = FragmentLineBox
	col := soColumn(50, 5, nil, soAt(0, 10, lineFrag))
	if got := NewLogicalFragment(wdm, col).BlockEndScrollableOverflow(); got != 24 {
		t.Errorf("BlockEndScrollableOverflow = %v, want 24 (text run 8..24)", got)
	}
}

// TestScrollableOverflow_ScrollContainerOrigin (LOU-469): a scroll
// container's overflow starts at its padding box and cannot extend before
// the scroll origin: left of it in LTR, right of it in RTL, where it
// scrolls leftward (AdjustOverflowForScrollOrigin,
// scrollable_overflow_calculator.cc:214-238; HasLeftOverflow,
// layout_box.cc:3240-3256 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestScrollableOverflow_ScrollContainerOrigin(t *testing.T) {
	// A 30x30 child at physical (-20, -20) and one at (40, 0).
	kids := func() []soChild {
		return []soChild{
			soAt(0, 0, soBox(30, 30, []string{"position", "relative", "left", "-20px", "top", "-20px"})),
			soAt(40, 0, soBox(30, 30, nil)),
		}
	}
	for _, tc := range []struct {
		name string
		kv   []string
		want geometry.PhysicalRect
	}{
		{"ltr", []string{"overflow", "auto"}, geometry.NewPhysicalRect(0, 0, 70, 50)},
		{"rtl", []string{"overflow", "auto", "direction", "rtl"}, geometry.NewPhysicalRect(-20, 0, 70, 50)},
		{"visible", nil, geometry.NewPhysicalRect(-20, -20, 90, 70)},
	} {
		b := soBuilder(50, 50, tc.kv...)
		for _, c := range kids() {
			// Physical offsets: pin the inline offsets to LTR positions.
			off := c.offset
			if b.wdm.IsRTL() {
				off.InlineOffset = 50 - off.InlineOffset - c.fragment.Size.WidthF64()
			}
			b.AddChild(c.fragment, off)
		}
		if got := b.Build().Fragment.ScrollableOverflow(); got != tc.want {
			t.Errorf("%s: ScrollableOverflow = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestScrollableOverflow_PaddingRectAndDefault (LOU-469): a scroll
// container's overflow starts as its padding rect, inside the borders
// (scrollable_overflow_calculator.cc:91-100 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a fragment no calculator ran
// for reports its border box (PhysicalBoxFragment::ScrollableOverflow,
// physical_box_fragment.h:234-240 @ a9f50e522efa).
func TestScrollableOverflow_PaddingRectAndDefault(t *testing.T) {
	b := soBuilder(50, 50, "overflow", "auto")
	b.SetBoxData(&PhysicalBoxData{Border: PhysicalEdges{Top: 5, Right: 5, Bottom: 5, Left: 5}})
	if got, want := b.Build().Fragment.ScrollableOverflow(), geometry.NewPhysicalRect(5, 5, 40, 40); got != want {
		t.Errorf("bordered scroll container: ScrollableOverflow = %+v, want %+v", got, want)
	}
	plain := &PhysicalFragment{Size: geometry.PhysicalSizeFromF64Round(30, 20)}
	if got, want := plain.ScrollableOverflow(), geometry.NewPhysicalRect(0, 0, 30, 20); got != want {
		t.Errorf("uncomputed fragment: ScrollableOverflow = %+v, want %+v", got, want)
	}
}

// TestScrollableOverflow_ChildOffsetIsBorderBoxRelative (LOU-469): louis14
// child offsets are content-box relative (Build's converter, fragmentToBox),
// Blink's are border-box relative; the overflow is in the border-box space
// (physical_box_fragment.cc:214-240 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The child sits at the content
// origin (23, 12); the overflow starts at the padding rect (3, 2).
func TestScrollableOverflow_ChildOffsetIsBorderBoxRelative(t *testing.T) {
	b := soBuilder(50, 50)
	b.SetBoxData(&PhysicalBoxData{
		Border:  PhysicalEdges{Top: 2, Left: 3},
		Padding: PhysicalEdges{Top: 10, Left: 20},
	})
	b.AddChild(soBox(60, 70, nil), LogicalOffset{})
	if got, want := b.Build().Fragment.ScrollableOverflow(), geometry.NewPhysicalRect(3, 2, 80, 80); got != want {
		t.Errorf("ScrollableOverflow = %+v, want %+v", got, want)
	}
}
