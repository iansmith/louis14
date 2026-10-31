package layout

import (
	"slices"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-400 (WI-6, plan G14): break-between values propagate through
// LayoutResult.InitialBreakBefore / FinalBreakAfter, and a nested multicol
// whose first child is a break-before:column spanner is broken before by its
// parent instead of emitting a zero-height fragment.
//
// Blink @ a9f50e522efa:
//   - BoxFragmentBuilder::AddResult → PropagateChildBreakValues
//     (box_fragment_builder.cc:226-232, :618-657): the first in-flow child's
//     joined break-before becomes the container's initial break-before
//     (SetInitialBreakBeforeIfNeeded, box_fragment_builder.h:518-521), every
//     child's joined break-after overwrites previous_break_after_.
//   - LayoutResult copies both unless layout containment applies
//     (layout_result.cc:89-95).
//   - CalculateBreakBetweenValue joins the child result's InitialBreakBefore
//     (fragmentation_utils.cc:131-160); BreakBeforeChildIfNeeded forces the
//     break only with container separation (:908-916).
//   - A non-empty column line resets previous_break_after_
//     (column_layout_algorithm.cc:1233-1235).

// layoutBlockInFragmentainer lays node out as a 50px-wide block. With a
// definite fragBlockSize it is inside a column fragmentainer of that size at
// fragOffset; Indefinite leaves fragmentation off.
func layoutBlockInFragmentainer(t *testing.T, node *html.Node, styles map[*html.Node]*css.Style, fragBlockSize, fragOffset float64) *LayoutResult {
	t.Helper()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	b := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 50, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 50, BlockSize: Indefinite})
	if fragBlockSize != Indefinite {
		b = b.SetHasBlockFragmentation(true).
			SetFragmentainerBlockSize(fragBlockSize).
			SetFragmentainerOffset(fragOffset).
			SetIsAtFragmentainerStart(fragOffset <= 0).
			SetBlockFragmentationType(FragmentColumn)
	}
	res := layoutElement(testContext(), buildTestTree(node, styles), b.Build())
	if res == nil || res.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	return res
}

// layoutMulticolInBalancingPass lays mc out directly in an outer initial
// column-balancing pass space: block fragmentation with an indefinite
// fragmentainer, so the multicol is not constrained by outer fragmentation.
func layoutMulticolInBalancingPass(t *testing.T, mc *html.Node, styles map[*html.Node]*css.Style) *LayoutResult {
	t.Helper()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetHasBlockFragmentation(true).
		SetFragmentainerBlockSize(Indefinite).
		SetIsInitialColumnBalancingPass(true).
		SetBlockFragmentationType(FragmentColumn).
		Build()
	res := NewMulticolLayoutAlgorithm(testContext(), buildTestTree(mc, styles), space).Layout()
	if res == nil || res.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	return res
}

// layoutColumnsSpec lays out a multicol with outerProps (display:block
// implied) whose children are kids, and returns its fragment and the node ids.
func layoutColumnsSpec(t *testing.T, outerProps []string, kids ...marginSpec) (*PhysicalFragment, map[*html.Node]string) {
	t.Helper()
	styles := map[*html.Node]*css.Style{}
	ids := map[*html.Node]string{}
	var kidNodes []*html.Node
	for _, k := range kids {
		kidNodes = append(kidNodes, k.build(styles, ids))
	}
	mc := makeNode("div", kidNodes...)
	styles[mc] = makeStyle(append([]string{"display", "block"}, outerProps...)...)
	return layoutMulticolForTest(t, mc, styles).Fragment, ids
}

// checkTops reports every id whose fragment origins differ from the
// Chrome-measured want.
func checkTops(t *testing.T, got map[string][]fragRect, want map[string][][2]float64) {
	t.Helper()
	for id, w := range want {
		var tops [][2]float64
		for _, r := range got[id] {
			tops = append(tops, [2]float64{r.x, r.y})
		}
		if !slices.Equal(tops, w) {
			t.Errorf("%s fragment origins = %v, want %v (Chrome 155)", id, tops, w)
		}
	}
}

// TestPropagateChildBreakValues_ResultValues (DoD D1): the values a block's
// LayoutResult carries, by Blink's arithmetic (box_fragment_builder.h:518-521,
// box_fragment_builder.cc:627-653, layout_result.cc:89-95 @ a9f50e522efa).
func TestPropagateChildBreakValues_ResultValues(t *testing.T) {
	h := func(v string, more ...string) []string { return append([]string{"height", v}, more...) }
	for _, tc := range []struct {
		name           string
		tree           marginSpec
		fragBlockSize  float64
		initial, final string
	}{
		{"first break-before and last break-after propagate",
			ms("W", nil, ms("B1", h("10px", "break-before", "column")), ms("B2", h("10px", "break-after", "column"))),
			100, "column", "column"},
		{"layout containment stops propagation",
			ms("W", props("contain", "layout"), ms("B1", h("10px", "break-before", "column")), ms("B2", h("10px", "break-after", "column"))),
			100, "auto", "auto"},
		{"no propagation outside fragmentation",
			ms("W", nil, ms("B1", h("10px", "break-before", "column")), ms("B2", h("10px", "break-after", "column"))),
			Indefinite, "auto", "auto"},
		{"the first child's auto wins over a later break-before",
			ms("W", nil, ms("X", h("10px")), ms("B", h("10px", "break-before", "column"))),
			100, "auto", "auto"},
		{"the first child's auto wins over a later break-before:avoid",
			ms("W", nil, ms("X", h("10px")), ms("B", h("10px", "break-before", "avoid"))),
			100, "auto", "auto"},
		{"a later auto break-after overwrites an earlier avoid",
			ms("W", nil, ms("A", h("10px", "break-after", "avoid")), ms("B", h("10px"))),
			100, "auto", "auto"},
		{"break-before propagates through two levels",
			ms("W", nil, ms("V", nil, ms("B", h("10px", "break-before", "column")))),
			100, "column", "auto"},
		{"a float does not propagate",
			ms("W", nil, ms("F", h("10px", "float", "left", "width", "10px", "break-before", "column")), ms("B", h("10px"))),
			100, "auto", "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			ids := map[*html.Node]string{}
			res := layoutBlockInFragmentainer(t, tc.tree.build(styles, ids), styles, tc.fragBlockSize, 0)
			if res.InitialBreakBefore != tc.initial {
				t.Errorf("InitialBreakBefore = %q, want %q", res.InitialBreakBefore, tc.initial)
			}
			if res.FinalBreakAfter != tc.final {
				t.Errorf("FinalBreakAfter = %q, want %q", res.FinalBreakAfter, tc.final)
			}
		})
	}
}

// TestPropagateChildBreakValues_Exclusions: column boxes, floats and
// out-of-flow boxes do not propagate (box_fragment_builder.cc:627-630 @
// a9f50e522efa), while a normal block child does.
func TestPropagateChildBreakValues_Exclusions(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	child := func(boxType BoxType, style ...string) *LayoutResult {
		return &LayoutResult{
			Fragment:           &PhysicalFragment{Type: FragmentBox, BoxType: boxType, Style: makeStyle(style...)},
			InitialBreakBefore: "column",
			FinalBreakAfter:    "column",
		}
	}
	for _, tc := range []struct {
		name  string
		child *LayoutResult
		want  string
	}{
		{"block", child(BoxTypeNormal, "display", "block"), "column"},
		{"column box", child(BoxTypeColumn, "display", "block"), "auto"},
		{"float", child(BoxTypeNormal, "display", "block", "float", "left"), "auto"},
		{"out-of-flow", child(BoxTypeNormal, "display", "block", "position", "absolute"), "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoxFragmentBuilder(wdm)
			b.PropagateChildBreakValues(tc.child)
			res := b.Build()
			if res.InitialBreakBefore != tc.want || res.FinalBreakAfter != tc.want {
				t.Errorf("InitialBreakBefore, FinalBreakAfter = %q, %q, want %q", res.InitialBreakBefore, res.FinalBreakAfter, tc.want)
			}
		})
	}
}

// TestBreakPropagation_ColumnGeometry (DoD D2, D3): a wrapper is broken
// before when its first descendant has break-before:column, and its next
// sibling is broken before when its last descendant has break-after:column.
// Every fixture is the 100x100 two-column multicol of layoutTwoColumnSpec
// (columns 50 wide). Expected rects are Chrome 155 getClientRects() relative
// to the multicol (probes lou400design p1, p2, p3, p3c, d2b, d7).
func TestBreakPropagation_ColumnGeometry(t *testing.T) {
	h := func(v string, more ...string) []string { return append([]string{"height", v}, more...) }
	for _, tc := range []struct {
		name    string
		balance bool
		tree    marginSpec
		rects   map[string][]fragRect
		tops    map[string][][2]float64
	}{
		{name: "p1 first descendant's break-before breaks before the wrapper",
			tree:  ms("R", nil, ms("A", h("40px")), ms("W", nil, ms("B", h("20px", "break-before", "column")))),
			rects: map[string][]fragRect{"A": {{0, 0, 50, 40}}, "W": {{50, 0, 50, 20}}, "B": {{50, 0, 50, 20}}},
			tops:  map[string][][2]float64{"R": {{0, 0}, {50, 0}}}},
		{name: "p2 last descendant's break-after breaks before the next sibling",
			tree:  ms("R", nil, ms("W", nil, ms("A", h("20px", "break-after", "column"))), ms("D", h("20px"))),
			rects: map[string][]fragRect{"W": {{0, 0, 50, 20}}, "D": {{50, 0, 50, 20}}}},
		{name: "d7 a break-after that is not last does not leak out of the wrapper",
			tree:  ms("R", nil, ms("W", nil, ms("A", h("20px", "break-after", "column")), ms("B", h("20px"))), ms("D", h("20px"))),
			rects: map[string][]fragRect{"A": {{0, 0, 50, 20}}, "B": {{50, 0, 50, 20}}, "D": {{50, 20, 50, 20}}}},
		{name: "p3 only the first child's break-before propagates",
			tree:  ms("R", nil, ms("A", h("40px")), ms("W", nil, ms("X", h("10px")), ms("B", h("20px", "break-before", "column")))),
			rects: map[string][]fragRect{"X": {{0, 40, 50, 10}}, "B": {{50, 0, 50, 20}}},
			tops:  map[string][][2]float64{"W": {{0, 40}, {50, 0}}}},
		{name: "p3c layout containment stops propagation",
			tree:  ms("R", nil, ms("A", h("40px")), ms("W", props("contain", "layout"), ms("B", h("20px", "break-before", "column")))),
			rects: map[string][]fragRect{"W": {{0, 40, 50, 20}}, "B": {{0, 40, 50, 20}}}},
		{name: "d2b balanced columns honour the propagated break", balance: true,
			tree:  ms("R", nil, ms("A", h("40px")), ms("W", nil, ms("B", h("20px", "break-before", "column")))),
			rects: map[string][]fragRect{"W": {{50, 0, 50, 20}}, "B": {{50, 0, 50, 20}}},
			tops:  map[string][][2]float64{"R": {{0, 0}, {50, 0}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutTwoColumnSpec(t, tc.tree, tc.balance)
			got := borderBoxRectsByID(frag, ids)
			checkRects(t, got, tc.rects)
			checkTops(t, got, tc.tops)
		})
	}
}

// TestLeadingBreakBeforeSpanner (DoD D4, D5, D6): a nested multicol whose
// first child is a break-before:column spanner. Blink places the spanner
// without a break (has_processed_first_child_ is false,
// column_layout_algorithm.cc:1397-1411), propagates the value to the
// multicol's InitialBreakBefore (AddResult :1427 → box_fragment_builder.cc:
// 640-642), and the parent breaks before the multicol only when it has
// container separation (fragmentation_utils.cc:908-916 @ a9f50e522efa).
// Expected rects are Chrome 155 (probes lou400design p4, p5, p6, p7;
// lou400d probe.html l1).
func TestLeadingBreakBeforeSpanner(t *testing.T) {
	h := func(v string, more ...string) []string { return append([]string{"height", v}, more...) }
	border20 := []string{
		"border-top-width", "20px", "border-top-style", "solid",
		"border-right-width", "20px", "border-right-style", "solid",
		"border-bottom-width", "20px", "border-bottom-style", "solid",
		"border-left-width", "20px", "border-left-style", "solid",
	}
	spanBefore := func(height string) []string {
		return h(height, "column-span", "all", "break-before", "column")
	}
	autoCols := func(height string) []string {
		return []string{"width", "100px", "height", height, "column-count", "2", "column-fill", "auto", "column-gap", "0"}
	}
	for _, tc := range []struct {
		name  string
		outer []string
		kids  []marginSpec
		rects map[string][]fragRect
		tops  map[string][][2]float64
	}{
		{name: "009 shape: broken before, one fragment with its top border",
			outer: autoCols("300px"),
			kids: []marginSpec{ms("A", h("100px")),
				ms("I", append([]string{"column-count", "2"}, border20...), ms("S", spanBefore("60px")))},
			rects: map[string][]fragRect{"A": {{0, 0, 50, 100}}, "I": {{50, 0, 50, 100}}, "S": {{70, 20, 10, 60}}}},
		{name: "no container separation: no break",
			outer: autoCols("100px"),
			kids:  []marginSpec{ms("I", props("column-count", "2"), ms("S", spanBefore("60px")))},
			rects: map[string][]fragRect{"I": {{0, 0, 50, 60}}, "S": {{0, 0, 50, 60}}}},
		{name: "spanner after column content breaks before the spanner",
			outer: autoCols("100px"),
			kids: []marginSpec{ms("A", h("40px")),
				ms("I", props("column-count", "2"), ms("C", h("20px")), ms("S", spanBefore("20px")))},
			rects: map[string][]fragRect{"A": {{0, 0, 50, 40}}, "S": {{50, 0, 50, 20}}},
			tops:  map[string][][2]float64{"I": {{0, 40}, {50, 0}}}},
		{name: "balanced outer: broken before in the balancing pass too",
			outer: []string{"width", "100px", "column-count", "2", "column-gap", "0"},
			kids: []marginSpec{ms("A", h("40px")),
				ms("I", props("column-count", "2"), ms("S", spanBefore("60px")))},
			rects: map[string][]fragRect{"A": {{0, 0, 50, 40}}, "I": {{50, 0, 50, 60}}, "S": {{50, 0, 50, 60}}}},
		{name: "a column box does not propagate its first child's break-before",
			outer: autoCols("100px"),
			kids: []marginSpec{ms("a", h("50px")),
				ms("m", props("column-count", "2", "column-gap", "0"),
					ms("b", h("20px", "break-before", "column")), ms("c", h("20px")))},
			rects: map[string][]fragRect{"m": {{0, 50, 50, 20}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutColumnsSpec(t, tc.outer, tc.kids...)
			got := borderBoxRectsByID(frag, ids)
			checkRects(t, got, tc.rects)
			checkTops(t, got, tc.tops)
		})
	}
}

// TestMulticol_LeadingBreakBeforeSpannerSetsInitialBreakBefore (DoD D4,
// multicol level): inside an outer fragmentainer, with content before it,
// the multicol lays its leading break-before:column spanner out without
// breaking and reports the value as its InitialBreakBefore for the parent to
// act on (column_layout_algorithm.cc:1404-1407, :1427;
// box_fragment_builder.cc:640-642 @ a9f50e522efa). Its block-size is Chrome
// p4's 100 (20 border + 60 spanner + 20 border).
func TestMulticol_LeadingBreakBeforeSpannerSetsInitialBreakBefore(t *testing.T) {
	styles := map[*html.Node]*css.Style{}
	s := makeNode("div")
	styles[s] = makeStyle("display", "block", "column-span", "all", "break-before", "column", "height", "60px")
	mc := makeNode("div", s)
	styles[mc] = makeStyle("display", "block", "column-count", "2",
		"border-top-width", "20px", "border-top-style", "solid",
		"border-bottom-width", "20px", "border-bottom-style", "solid")
	res := layoutMulticolInFragmentainer(t, mc, styles, 300, 100)
	if res.InitialBreakBefore != "column" {
		t.Errorf("InitialBreakBefore = %q, want %q", res.InitialBreakBefore, "column")
	}
	if res.BreakToken != nil {
		t.Errorf("BreakToken = %+v, want nil (the parent breaks before the multicol)", res.BreakToken)
	}
	if res.HasForcedBreak {
		t.Error("HasForcedBreak = true, want false")
	}
	if got := res.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("block-size = %v, want 100", got)
	}
}

// TestMulticol_NonEmptyLineResetsPreviousBreakAfter: a spanner's
// break-after:column becomes the multicol's FinalBreakAfter only when no
// column content follows it, since a non-empty column line resets
// previous_break_after_ (column_layout_algorithm.cc:1233-1235;
// box_fragment_builder.cc:651-653 @ a9f50e522efa). Laid out in an outer
// balancing pass, where the multicol is not constrained by outer
// fragmentation.
func TestMulticol_NonEmptyLineResetsPreviousBreakAfter(t *testing.T) {
	spanner := func(styles map[*html.Node]*css.Style) *html.Node {
		n := makeNode("div")
		styles[n] = makeStyle("display", "block", "column-span", "all", "break-after", "column", "height", "10px")
		return n
	}
	content := func(styles map[*html.Node]*css.Style) *html.Node {
		n := makeNode("div")
		styles[n] = makeStyle("display", "block", "height", "20px")
		return n
	}
	for _, tc := range []struct {
		name         string
		spannerFirst bool
		want         string
	}{
		{"column content after the spanner resets it", true, "auto"},
		{"a last spanner's break-after propagates", false, "column"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			kids := []*html.Node{spanner(styles), content(styles)}
			if !tc.spannerFirst {
				kids = []*html.Node{content(styles), spanner(styles)}
			}
			mc := makeNode("div", kids...)
			styles[mc] = makeStyle("display", "block", "column-count", "2", "column-gap", "0")
			res := layoutMulticolInBalancingPass(t, mc, styles)
			if res.FinalBreakAfter != tc.want {
				t.Errorf("FinalBreakAfter = %q, want %q", res.FinalBreakAfter, tc.want)
			}
		})
	}
}

// TestBreakBeforeLeaf_AppealJoinsPreviousSiblingBreakAfter (LOU-400 review 1,
// ledger L39): breaking before a leaf child that starts exactly at the
// fragmentainer end joins the PREVIOUS sibling's break-after with the leaf's
// break-before, not the leaf's own break-after, which AddResult has not yet
// propagated. Blink: BreakBeforeChildIfNeeded computes CalculateBreakAppealBefore
// → CalculateBreakBetweenValue → builder.JoinedBreakBetweenValue before the
// child's AddResult (fragmentation_utils.cc:130-160, :221-235, :895-920 @
// a9f50e522efa); the appeal is kBreakAppealViolatingBreakAvoid only when that
// join is an avoid value. Expected values are that arithmetic.
//
// The fixture uses a zero-size fragmentainer (the balance-estimate fragSize==0
// path): with a positive fragmentainer, louis14's BreakBeforeChildIfNeeded
// breaks before such an X itself (BrokeBefore), so the post-layout
// isBreakBeforeCurrent check in BlockLayoutAlgorithm does not see it. With
// size 0, MovePastBreakpoint refuses every break-before (no progress), X
// overflows from offset 0 = the fragmentainer end, and the post-layout check
// scores the break before X.
// X is monolithic (contain:size) so it overflows whole instead of being
// sliced, as in TestZeroHeightUnbreakableNoContinuation. A is a flow-root so
// it does not collapse through (a collapsed-through child never reaches
// PropagateChildBreakValues in louis14).
func TestBreakBeforeLeaf_AppealJoinsPreviousSiblingBreakAfter(t *testing.T) {
	h := func(v string, more ...string) []string { return append([]string{"height", v}, more...) }
	for _, tc := range []struct {
		name string
		tree marginSpec
		want BreakAppeal
	}{
		{"the leaf's own break-after:avoid-column does not taint the break before it",
			ms("W", nil, ms("A", h("0px", "display", "flow-root")), ms("X", h("10px", "contain", "size", "break-after", "avoid-column"))),
			BreakAppealPerfect},
		{"the previous sibling's break-after:avoid-column does",
			ms("W", nil, ms("A", h("0px", "display", "flow-root", "break-after", "avoid-column")), ms("X", h("10px", "contain", "size"))),
			BreakAppealViolatingBreakAvoid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			ids := map[*html.Node]string{}
			res := layoutBlockInFragmentainer(t, tc.tree.build(styles, ids), styles, 0, 0)
			if res.BreakToken == nil {
				t.Fatalf("W did not break in a 0px fragmentainer (X overflows it)")
			}
			if res.BreakAppeal != tc.want {
				t.Errorf("BreakAppeal = %v, want %v", res.BreakAppeal, tc.want)
			}
		})
	}
}

// TestMulticol_SpannerBreakAfterPropagatesAsFinalBreakAfter_LOU471: inside an
// outer fragmentainer, a spanner's break-after:column never breaks the outer
// fragmentainer by itself. Blink records it through AddResult →
// PropagateChildBreakValues (column_layout_algorithm.cc:1427;
// box_fragment_builder.cc:651-653 @ a9f50e522efa): following column content
// resets it (column_layout_algorithm.cc:1233-1235), and a last spanner's value
// becomes the multicol's FinalBreakAfter, which the parent joins with the next
// sibling's break-before (CalculateBreakBetweenValue,
// fragmentation_utils.cc:131-160). Neither case sets HasForcedBreak or emits a
// break token. The content fits the 300px fragmentainer, so any break is the
// break-after's.
func TestMulticol_SpannerBreakAfterPropagatesAsFinalBreakAfter_LOU471(t *testing.T) {
	for _, tc := range []struct {
		name         string
		spannerFirst bool
		want         string
	}{
		{"column content after the spanner: no break", true, "auto"},
		{"last spanner: FinalBreakAfter, no break", false, "column"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			s := makeNode("div")
			styles[s] = makeStyle("display", "block", "column-span", "all", "break-after", "column", "height", "50px")
			c := makeNode("div")
			styles[c] = makeStyle("display", "block", "height", "100px")
			kids := []*html.Node{s, c}
			if !tc.spannerFirst {
				kids = []*html.Node{c, s}
			}
			mc := makeNode("div", kids...)
			styles[mc] = makeStyle("display", "block", "column-count", "2", "column-gap", "0")
			res := layoutMulticolInFragmentainer(t, mc, styles, 300, 50)
			if res.BreakToken != nil {
				t.Errorf("BreakToken = %+v, want nil", res.BreakToken)
			}
			if res.HasForcedBreak {
				t.Error("HasForcedBreak = true, want false")
			}
			if res.FinalBreakAfter != tc.want {
				t.Errorf("FinalBreakAfter = %q, want %q", res.FinalBreakAfter, tc.want)
			}
		})
	}
}

// TestBreakPropagation_LastSpannerBreakAfterBreaksBeforeNextSibling_LOU471:
// a nested multicol ending in a break-after:column spanner makes its parent
// break before the multicol's next sibling, through FinalBreakAfter and
// CalculateBreakBetweenValue (fragmentation_utils.cc:131-160, :908-916 @
// a9f50e522efa) rather than through HasForcedBreak.
func TestBreakPropagation_LastSpannerBreakAfterBreaksBeforeNextSibling_LOU471(t *testing.T) {
	h := func(v string, more ...string) []string { return append([]string{"height", v}, more...) }
	tree := ms("R", nil,
		ms("I", props("column-count", "2", "column-gap", "0"),
			ms("A", h("10px")), ms("S", h("20px", "column-span", "all", "break-after", "column"))),
		ms("D", h("20px")))
	frag, ids := layoutTwoColumnSpec(t, tree, false)
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"S": {{0, 5, 50, 20}},
		"D": {{50, 0, 50, 20}},
	})
}
