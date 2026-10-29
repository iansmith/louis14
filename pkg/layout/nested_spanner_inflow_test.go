package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// nonEmptyRects drops zero-block-size fragments: Chrome reports a 0px-tall
// fragment of a spanner parent after its spanner that louis14 does not
// always emit, and the difference is invisible (ledger L51).
func nonEmptyRects(rects map[string][]fragRect) map[string][]fragRect {
	out := map[string][]fragRect{}
	for id, rs := range rects {
		for _, r := range rs {
			if r.h != 0 {
				out[id] = append(out[id], r)
			}
		}
	}
	return out
}

// TestNestedSpannerParent_ChromeGeometry_LOU410 pins the geometry of boxes
// whose column spanner sits inside an in-flow child (a nested spanner) against
// Chrome 155.0.8059.41 (headless, getClientRects relative to the multicol,
// zero-height rects dropped).
//
// Blink places such a child like any in-flow child (FinishInflow → AddResult,
// block_layout_algorithm.cc:2733-2738 @ a9f50e522efa): the parent advances by
// the child's fragment block-size (ComputeInflowPosition, :2953), which
// FinishFragmentation's FoundColumnSpanner branch clamped to
// min(desired, intrinsic) - trailing border/padding (fragmentation_utils.cc:
// 601-628); the builder wraps the child's spanner path (PropagateBreakInfo,
// box_fragment_builder.cc:594-608) and the child loop stops
// (block_layout_algorithm.cc:1108-1115).
func TestNestedSpannerParent_ChromeGeometry_LOU410(t *testing.T) {
	h := func(v string, more ...string) []string { return append([]string{"height", v}, more...) }
	span := func(v string) []string { return []string{"column-span", "all", "height", v} }
	wide2 := []string{"width", "200px", "column-count", "2", "column-gap", "0", "column-fill", "balance"}
	box100 := []string{"width", "100px", "height", "100px", "column-count", "2", "column-gap", "0", "column-fill", "balance"}
	for _, tc := range []struct {
		name   string
		mc     []string
		kids   []marginSpec
		want   map[string][]fragRect
		height float64
	}{
		{
			// Chrome probe f1.html. The definite-height container's
			// pre-spanner fragment is min(100, 150) = 100; outer advances
			// by that, not by the container's 150px of content.
			name: "F1_definite_height_child_over_budget",
			mc:   wide2,
			kids: []marginSpec{
				ms("outer", nil, ms("container", h("100px"), ms("a", h("150px")), ms("sp", span("20px")))),
				ms("tail", h("40px")),
			},
			want: map[string][]fragRect{
				"outer":     {{0, 0, 100, 75}, {100, 0, 100, 25}},
				"container": {{0, 0, 100, 75}, {100, 0, 100, 25}},
				"a":         {{0, 0, 100, 75}, {100, 0, 100, 75}},
				"sp":        {{0, 75, 200, 20}},
				"tail":      {{0, 95, 100, 20}, {100, 95, 100, 20}},
			},
			height: 115,
		},
		{
			// Chrome probe f8.html. The nested-site box's own max-height
			// clamps its pre-spanner fragment (FragmentsTotalBlockSize via
			// ComputeBlockSizeForFragment, block_layout_algorithm.cc:
			// 1401-1407, then the FoundColumnSpanner branch).
			name: "F8_max_height_parent",
			mc:   []string{"width", "200px", "column-count", "2", "column-gap", "0"},
			kids: []marginSpec{
				ms("outer", props("max-height", "100px"), ms("container", nil, ms("a", h("150px")), ms("sp", span("20px")))),
				ms("tail", h("40px")),
			},
			want: map[string][]fragRect{
				"outer":     {{0, 0, 100, 75}, {100, 0, 100, 25}},
				"container": {{0, 0, 100, 75}, {100, 0, 100, 75}},
				"a":         {{0, 0, 100, 75}, {100, 0, 100, 75}},
				"sp":        {{0, 75, 200, 20}},
				"tail":      {{0, 95, 100, 20}, {100, 95, 100, 20}},
			},
			height: 115,
		},
		{
			// Chrome probe r3.html. The parent advances by the child's
			// border-box fragment, block-start padding included.
			name: "R3_padded_spanner_parent",
			mc:   []string{"width", "200px", "column-count", "1", "column-gap", "0"},
			kids: []marginSpec{
				ms("outer", nil, ms("p", props("padding-top", "10px"), ms("b", h("40px")), ms("sp", span("10px")))),
				ms("after", h("20px")),
			},
			want: map[string][]fragRect{
				"outer": {{0, 0, 200, 50}},
				"p":     {{0, 0, 200, 50}},
				"b":     {{0, 10, 200, 40}},
				"sp":    {{0, 50, 200, 10}},
				"after": {{0, 60, 200, 20}},
			},
			height: 80,
		},
		{
			// Chrome probe r2.html. The pre-spanner fragments of a
			// definite-height box hold none of its trailing border: the
			// second is min(60-40, 40+10) - 10 = 10 (fragmentation_utils.cc:
			// 606-628); the border comes after the spanner.
			name: "r2_trailing_border_not_in_budget",
			mc:   box100,
			kids: []marginSpec{
				ms("cont", h("50px", "border-bottom-width", "10px", "border-bottom-style", "solid"),
					ms("child", nil, ms("b", h("80px")), ms("s", span("10px")))),
				ms("c", h("20px")),
			},
			want: map[string][]fragRect{
				"cont":  {{0, 0, 50, 40}, {50, 0, 50, 10}, {0, 50, 50, 10}},
				"child": {{0, 0, 50, 40}, {50, 0, 50, 40}},
				"b":     {{0, 0, 50, 40}, {50, 0, 50, 40}},
				"s":     {{0, 40, 100, 10}},
				"c":     {{0, 60, 50, 5}, {50, 50, 50, 15}},
			},
			height: 100,
		},
		{
			// Chrome probe r4.html. An empty pre-spanner child is placed by
			// the normal path at its collapsed margin: Blink resolves the
			// BFC block-offset of a box interrupted by a spanner
			// (block_layout_algorithm.cc:1413-1416), so g's 20px margin
			// is in the pre-spanner row (x 40 + 20 balanced to 30).
			name: "r4_empty_pre_spanner_child_margin",
			mc:   box100,
			kids: []marginSpec{
				ms("x", h("40px")),
				ms("g", props("margin-top", "20px"), ms("s", span("10px"))),
				ms("c", h("20px")),
			},
			want: map[string][]fragRect{
				"x": {{0, 0, 50, 30}, {50, 0, 50, 10}},
				"s": {{0, 30, 100, 10}},
				"c": {{0, 40, 50, 10}, {50, 40, 50, 10}},
			},
			height: 100,
		},
		{
			// Chrome probe f5.html (guard). The loop stops at the
			// spanner-carrying child: sib is laid out after the spanner.
			name: "F5_loop_stops",
			mc:   wide2,
			kids: []marginSpec{
				ms("w", nil, ms("a", h("40px")), ms("sp", span("20px"))),
				ms("sib", h("200px")),
			},
			want: map[string][]fragRect{
				"w":   {{0, 0, 100, 20}, {100, 0, 100, 20}},
				"a":   {{0, 0, 100, 20}, {100, 0, 100, 20}},
				"sp":  {{0, 20, 200, 20}},
				"sib": {{0, 40, 100, 100}, {100, 40, 100, 100}},
			},
			height: 140,
		},
		{
			// Chrome probe f6.html (guard). The interrupted child's
			// margin-bottom is not in the pre-spanner row (Blink drops the
			// trailing strut of a child broken inside, ComputeInflowPosition,
			// block_layout_algorithm.cc:3014-3031); it applies after the
			// spanner.
			name: "F6_parent_margin_bottom_after_spanner",
			mc:   wide2,
			kids: []marginSpec{
				ms("g", props("margin-bottom", "20px"), ms("a", h("40px")), ms("sp", span("20px"))),
				ms("c", h("40px")),
			},
			want: map[string][]fragRect{
				"g":  {{0, 0, 100, 20}, {100, 0, 100, 20}},
				"a":  {{0, 0, 100, 20}, {100, 0, 100, 20}},
				"sp": {{0, 20, 200, 20}},
				"c":  {{0, 60, 100, 10}, {100, 40, 100, 30}},
			},
			height: 70,
		},
		{
			// Chrome probe f7.html (guard). A spanner-carrying child's own
			// break-before:column is honoured.
			name: "F7_break_before_on_spanner_parent",
			mc:   wide2,
			kids: []marginSpec{
				ms("x", h("20px")),
				ms("g", props("break-before", "column"), ms("a", h("20px")), ms("sp", span("10px"))),
				ms("c", h("40px")),
			},
			want: map[string][]fragRect{
				"x":  {{0, 0, 100, 20}},
				"g":  {{100, 0, 100, 20}},
				"a":  {{100, 0, 100, 20}},
				"sp": {{0, 20, 200, 10}},
				"c":  {{0, 30, 100, 20}, {100, 30, 100, 20}},
			},
			height: 50,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutColumnsSpec(t, tc.mc, tc.kids...)
			checkRects(t, nonEmptyRects(borderBoxRectsByID(frag, ids)), tc.want)
			if got := frag.Size.HeightF64(); got != tc.height {
				t.Errorf("multicol height = %v, want %v (Chrome 155)", got, tc.height)
			}
		})
	}
}

// TestNestedSpannerParentStopsChildLoop_LOU410 lays a nested spanner's
// parents out directly in a column fragmentation space and checks the
// LayoutResult against Blink's arithmetic @ a9f50e522efa.
func TestNestedSpannerParentStopsChildLoop_LOU410(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	columnSpace := func(isNewFC, balancing bool, fragmentainerBlockSize float64) ConstraintSpace {
		return NewConstraintSpaceBuilder(wdm, wdm, isNewFC).
			SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
			SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(fragmentainerBlockSize).
			SetIsInitialColumnBalancingPass(balancing).
			SetBlockFragmentationType(FragmentColumn).
			Build()
	}

	// Row a: outer > container{height:100px} > [a{height:150px}, sp].
	// container's fragment is min(100, 150) = 100 (FinishFragmentation's
	// FoundColumnSpanner branch, fragmentation_utils.cc:601-628); outer
	// advances by it (ComputeInflowPosition, block_layout_algorithm.cc:2953),
	// so its own fragment is min(100, 100) = 100 and it consumes 0 + 100
	// (fragmentation_utils.cc:682-683).
	t.Run("a_consumes_clamped_child_fragment", func(t *testing.T) {
		a := makeNode("div")
		sp := makeNode("div")
		container := makeNode("div", a, sp)
		outer := makeNode("div", container)
		styles := map[*html.Node]*css.Style{
			outer:     makeStyle("display", "block"),
			container: makeStyle("display", "block", "height", "100px"),
			a:         makeStyle("display", "block", "height", "150px"),
			sp:        makeStyle("display", "block", "column-span", "all", "height", "20px"),
		}
		outerNode := buildTestTree(outer, styles)
		containerNode := outerNode.Children()[0]
		spNode := containerNode.Children()[1]
		res := NewBlockLayoutAlgorithm(testContext(), outerNode, columnSpace(false, false, 200)).Layout()
		if got := NewLogicalFragment(wdm, res.Fragment).BlockSize(); got != 100 {
			t.Errorf("outer fragment block-size = %v, want 100", got)
		}
		if res.IntrinsicBlockSize != 100 {
			t.Errorf("outer IntrinsicBlockSize = %v, want 100", res.IntrinsicBlockSize)
		}
		tok := res.BreakToken
		if tok == nil {
			t.Fatal("outer has no break token")
		}
		if got := tok.ConsumedBlockSize.Float64(); got != 100 {
			t.Errorf("outer ConsumedBlockSize = %v, want 100", got)
		}
		if !tok.IsCausedByColumnSpanner {
			t.Error("outer token is not caused by a column spanner")
		}
		if len(tok.ChildBreakTokens) != 1 || tok.ChildBreakTokens[0].Node != containerNode ||
			!tok.ChildBreakTokens[0].IsBreakInside() {
			t.Errorf("outer child tokens = %+v, want one break inside container", tok.ChildBreakTokens)
		}
		path := res.ColumnSpannerPath
		if path == nil || path.Box != containerNode || path.Child == nil || path.Child.Box != spNode {
			t.Errorf("spanner path = %+v, want container → sp", path)
		}
	})

	// Rows b, c: a column box in the initial balancing pass (indefinite
	// fragmentainer), root > [w{...} > [a{height:40px}, sp], sib{height:200px}].
	// The loop stops after w (block_layout_algorithm.cc:1108-1115), so sib is
	// not laid out; w's trailing margin is dropped because w broke inside
	// (:3014-3031), so the content stays 40.
	for _, tc := range []struct {
		name    string
		wStyles []string
	}{
		{"b_balancing_pass_stops_after_spanner_parent", nil},
		{"c_interrupted_child_margin_dropped", []string{"margin-bottom", "20px"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := makeNode("div")
			sp := makeNode("div")
			w := makeNode("div", a, sp)
			sib := makeNode("div")
			root := makeNode("div", w, sib)
			styles := map[*html.Node]*css.Style{
				root: makeStyle("display", "block"),
				w:    makeStyle(append([]string{"display", "block"}, tc.wStyles...)...),
				a:    makeStyle("display", "block", "height", "40px"),
				sp:   makeStyle("display", "block", "column-span", "all", "height", "20px"),
				sib:  makeStyle("display", "block", "height", "200px"),
			}
			rootNode := buildTestTree(root, styles)
			wNode := rootNode.Children()[0]
			spNode := wNode.Children()[1]
			bla := NewBlockLayoutAlgorithm(testContext(), rootNode, columnSpace(true, true, Indefinite))
			bla.SetBoxType(BoxTypeColumn)
			res := bla.Layout()
			if n := len(res.Fragment.Children); n != 1 {
				t.Errorf("column box has %d child fragments, want 1 (w only)", n)
			}
			if res.IntrinsicBlockSize != 40 {
				t.Errorf("column IntrinsicBlockSize = %v, want 40", res.IntrinsicBlockSize)
			}
			tok := res.BreakToken
			if tok == nil {
				t.Fatal("column box has no break token")
			}
			if !tok.IsCausedByColumnSpanner {
				t.Error("column token is not caused by a column spanner")
			}
			if len(tok.ChildBreakTokens) != 1 || tok.ChildBreakTokens[0].Node != wNode ||
				tok.ChildBreakTokens[0].IsBreakBefore {
				t.Errorf("column child tokens = %+v, want w's own token", tok.ChildBreakTokens)
			}
			path := res.ColumnSpannerPath
			if path == nil || path.Box != wNode || path.Child == nil || path.Child.Box != spNode {
				t.Errorf("spanner path = %+v, want w → sp", path)
			}
		})
	}
}
