package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
)

// LOU-411 (LOU-399's BSFF half): a balanced column line is as tall as its
// columns' BlockSizeForFragmentation — the content, overflow included
// (ResolveColumnAutoBlockSizeInternal measures each run by it,
// column_layout_algorithm.cc:1652-1672 @ a9f50e522efa). A column's value
// holds its children only (box_fragment_builder.cc:743-745); a box that
// takes part in fragmentation extends it by its children's overflow
// (:758-761); line boxes and floats contribute like any child (AddResult →
// PropagateBreakInfo, :227-229, :479-489).
// Expected geometry is Chrome 155 headless (LOU-411 probe.html F1/F2,
// probe6.html F3-F5). probe5.html L (a monolithic box with overflowing
// content) is TestBalancingFloorMonolithicContent_LOU411: it also needs the
// monolithic floor to be the border box.
func TestColumnBSFFSetsBalanceHeight_LOU411(t *testing.T) {
	mc := props("column-count", "2", "column-gap", "0", "width", "200px")
	for _, tc := range []struct {
		name   string
		kids   func(styles map[*html.Node]*css.Style, ids map[*html.Node]string) []*html.Node
		height float64
		want   map[string][]fragRect
	}{
		{
			// A breakable 100px block overflowing a height:0 parent
			// balances to 50px columns.
			name:   "F1 breakable overflow",
			kids:   specKids(ms("z", props("height", "0"), ms("c", props("height", "100px")))),
			height: 50,
			want:   map[string][]fragRect{"c": {{0, 0, 100, 50}, {100, 0, 100, 50}}},
		},
		{
			name: "F2 monolithic overflow",
			kids: specKids(ms("z", props("height", "0"),
				ms("c", props("height", "100px", "contain", "size", "width", "50px")))),
			height: 100,
			want:   map[string][]fragRect{"c": {{0, 0, 50, 100}}},
		},
		{
			// An inline-block in a zero-height line overflows the line box;
			// the line box itself contributes only its own size, but the
			// atomic inline is unbreakable, so the columns grow to 100.
			name: "F3 inline-block overflowing its line",
			kids: func(styles map[*html.Node]*css.Style, ids map[*html.Node]string) []*html.Node {
				ib := makeNode("span")
				z := makeNode("div", ib)
				styles[z] = makeStyle("display", "block", "height", "0", "line-height", "0")
				styles[ib] = makeStyle("display", "inline-block", "width", "50px", "height", "100px", "vertical-align", "top")
				ids[ib] = "ib"
				return []*html.Node{z}
			},
			height: 100,
			want:   map[string][]fragRect{"ib": {{0, 0, 50, 100}}},
		},
		{
			// Chrome breaks the float into (0,0,50,50) and (100,0,50,50);
			// louis14 does not fragment floats yet (plan T9), so only the
			// line height is asserted.
			name: "F4 breakable float",
			kids: specKids(ms("z", props("height", "0"),
				ms("f", props("float", "left", "width", "50px", "height", "100px")))),
			height: 50,
		},
		{
			name: "F5 monolithic float",
			kids: specKids(ms("z", props("height", "0"),
				ms("f", props("float", "left", "width", "50px", "height", "100px", "contain", "size")))),
			height: 100,
			want:   map[string][]fragRect{"f": {{0, 0, 50, 100}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			ids := map[*html.Node]string{}
			root := makeNode("div", tc.kids(styles, ids)...)
			styles[root] = makeStyle(append([]string{"display", "block"}, mc...)...)
			frag := layoutMulticolForTest(t, root, styles).Fragment
			checkRects(t, borderBoxRectsByID(frag, ids), tc.want)
			checkMulticolHeight(t, frag, tc.height)
		})
	}
}

// specKids adapts marginSpecs to the node-builder form the table uses.
func specKids(kids ...marginSpec) func(map[*html.Node]*css.Style, map[*html.Node]string) []*html.Node {
	return func(styles map[*html.Node]*css.Style, ids map[*html.Node]string) []*html.Node {
		var nodes []*html.Node
		for _, k := range kids {
			nodes = append(nodes, k.build(styles, ids))
		}
		return nodes
	}
}

// TestCalculateUnbreakableBlockSize_MonolithicUsesFragmentSize_LOU411: a
// monolithic box's unbreakable block-size is its block-size for
// fragmentation — the border box, as it records none — never its content's
// intrinsic block-size (CalculateUnbreakableBlockSize,
// fragmentation_utils.cc:979-993, BlockSizeForFragmentation :1545-1583 @
// a9f50e522efa).
func TestCalculateUnbreakableBlockSize_MonolithicUsesFragmentSize_LOU411(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := ConstraintSpace{WritingDirection: wdm}
	result := &LayoutResult{
		Fragment:           &PhysicalFragment{Size: geometry.NewPhysicalSize(50, 30), IsMonolithic: true},
		IntrinsicBlockSize: 100,
	}
	if got := CalculateUnbreakableBlockSize(space, result, 0); got != 30 {
		t.Errorf("CalculateUnbreakableBlockSize = %v, want 30", got)
	}
}

// TestBalancingFloorMonolithicContent_LOU411: a size-contained box is
// monolithic; its 100px of visible content does not raise the balancing
// floor or the column BlockSizeForFragmentation past its 30px border box
// (layout_result.cc:75-79: no block fragmentation, nothing recorded).
// Chrome 155 headless, LOU-411 probe5.html L.
func TestBalancingFloorMonolithicContent_LOU411(t *testing.T) {
	checkMonolithicFloor(t, props("height", "30px", "contain", "size"))
}

// TestBalancingFloorScrollContainerOverflow_LOU411: likewise for a scroll
// container. Chrome 155 headless, LOU-411 probe5.html L2.
func TestBalancingFloorScrollContainerOverflow_LOU411(t *testing.T) {
	checkMonolithicFloor(t, props("height", "30px", "overflow", "auto"))
}

func checkMonolithicFloor(t *testing.T, boxProps []string) {
	t.Helper()
	frag, ids := layoutColumnsSpec(t, props("column-count", "2", "column-gap", "0", "width", "200px"),
		ms("a", props("height", "20px")),
		ms("sc", boxProps, ms("c", props("height", "100px"))),
		ms("b", props("height", "20px")),
	)
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"a":  {{0, 0, 100, 20}},
		"sc": {{0, 20, 100, 30}},
		"b":  {{100, 0, 100, 20}},
	})
	checkMulticolHeight(t, frag, 50)
}
