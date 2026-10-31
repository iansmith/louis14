package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-411 (plan WI-6, G13): one spanner space. Blink lays every spanner out
// in the space ColumnLayoutAlgorithm::CreateConstraintSpaceForSpanner builds
// (column_layout_algorithm.cc:1865-1893 @ a9f50e522efa): available and
// percentage-resolution size are both ChildAvailableSize() (:1877-1878), and
// the fragmentation fields come from SetupSpaceBuilderForFragmentation
// (fragmentation_utils.cc:321-397) only when the multicol's own space has
// block fragmentation (:1888-1891) — FragmentainerCapacity /
// FragmentainerOffset for children (fragmentation_utils.h:187-226), the
// parent's balancing flags and MinBreakAppeal, nothing for a monolithic
// spanner (:336-338).

// layoutOuterMulticolSpec lays out a 400px-wide-available top-level block
// with outerProps (display:block implied) whose children are kids, and
// returns its fragment and the node ids. Rects read from it are relative to
// that block's border box.
func layoutOuterMulticolSpec(t *testing.T, outerProps []string, kids ...marginSpec) (*PhysicalFragment, map[*html.Node]string) {
	t.Helper()
	styles := map[*html.Node]*css.Style{}
	ids := map[*html.Node]string{}
	var kidNodes []*html.Node
	for _, k := range kids {
		kidNodes = append(kidNodes, k.build(styles, ids))
	}
	outer := makeNode("div", kidNodes...)
	styles[outer] = makeStyle(append([]string{"display", "block"}, outerProps...)...)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		Build()
	res := layoutElement(testContext(), buildTestTree(outer, styles), space)
	if res == nil || res.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	return res.Fragment, ids
}

// TestCreateConstraintSpaceForSpanner_LOU411 (DoD D1): the shape of the one
// spanner space, by Blink's arithmetic (column_layout_algorithm.cc:1865-1893,
// fragmentation_utils.cc:321-372, fragmentation_utils.h:187-226 @
// a9f50e522efa).
func TestCreateConstraintSpaceForSpanner_LOU411(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	type outerSpace struct {
		fragBlockSize, fragOffset float64
		balancingPass             bool
		insideBalanced            bool
		minBreakAppeal            BreakAppeal
	}
	type want struct {
		frag           bool
		avail, percent float64
		fragBlockSize  float64
		fragOffset     float64
		atStart        bool
		balancingPass  bool
		insideBalanced bool
		minBreakAppeal BreakAppeal
	}
	for _, tc := range []struct {
		name        string
		mcProps     []string
		spannerProp []string
		outer       *outerSpace
		blockOffset float64
		want        want
	}{
		{
			name:    "unfragmented definite height",
			mcProps: []string{"height", "200px"},
			want:    want{avail: 200, percent: 200},
		},
		{
			name: "unfragmented auto height",
			want: want{avail: Indefinite, percent: Indefinite},
		},
		{
			// Capacity 100-10-10 (cloned decorations, fragmentation_utils.h:
			// 187-203); offset 30-10 (FragmentainerOffset for children,
			// :212-226) + block_offset 25.
			name:        "nested constrained, cloned decorations",
			mcProps:     []string{"height", "200px", "padding-top", "10px", "padding-bottom", "10px", "box-decoration-break", "clone"},
			outer:       &outerSpace{fragBlockSize: 100, fragOffset: 30, insideBalanced: true, minBreakAppeal: BreakAppealViolatingBreakAvoid},
			blockOffset: 25,
			want: want{frag: true, avail: 200, percent: 200, fragBlockSize: 80, fragOffset: 45,
				insideBalanced: true, minBreakAppeal: BreakAppealViolatingBreakAvoid},
		},
		{
			name:    "nested constrained at fragmentainer start",
			mcProps: []string{"height", "200px"},
			outer:   &outerSpace{fragBlockSize: 100},
			want:    want{frag: true, avail: 200, percent: 200, fragBlockSize: 100, atStart: true},
		},
		{
			// An outer initial balancing pass gives the spanner fragmentation
			// with an unknown fragmentainer size (FragmentainerCapacity →
			// kIndefiniteSize), which is what makes its space an initial
			// balancing pass (constraint_space.h:320-323).
			name:  "outer initial balancing pass",
			outer: &outerSpace{fragBlockSize: Indefinite, balancingPass: true, insideBalanced: true},
			want: want{frag: true, avail: Indefinite, percent: Indefinite, fragBlockSize: Indefinite,
				atStart: true, balancingPass: true, insideBalanced: true},
		},
		{
			// A monolithic spanner takes no part in block fragmentation
			// (fragmentation_utils.cc:336-338).
			name:        "monolithic spanner",
			mcProps:     []string{"height", "200px"},
			spannerProp: []string{"contain", "size"},
			outer:       &outerSpace{fragBlockSize: 100, fragOffset: 30},
			want:        want{avail: 200, percent: 200},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spannerEl := makeNode("div")
			mc := makeNode("div", spannerEl)
			styles := map[*html.Node]*css.Style{
				mc:        makeStyle(append([]string{"display", "block", "column-count", "2", "column-gap", "0"}, tc.mcProps...)...),
				spannerEl: makeStyle(append([]string{"display", "block", "column-span", "all", "height", "10px"}, tc.spannerProp...)...),
			}
			b := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite})
			if o := tc.outer; o != nil {
				b = b.SetHasBlockFragmentation(true).
					SetFragmentainerBlockSize(o.fragBlockSize).
					SetFragmentainerOffset(o.fragOffset).
					SetIsAtFragmentainerStart(o.fragOffset <= 0).
					SetBlockFragmentationType(FragmentColumn).
					SetIsInitialColumnBalancingPass(o.balancingPass).
					SetIsInsideBalancedColumns(o.insideBalanced).
					SetMinBreakAppeal(o.minBreakAppeal)
			}
			mcNode := buildTestTree(mc, styles)
			mla := NewMulticolLayoutAlgorithm(testContext(), mcNode, b.Build())
			mla.Layout()
			got := mla.CreateConstraintSpaceForSpanner(mcNode.Children()[0], tc.blockOffset, nil)
			w := tc.want
			if got.HasBlockFragmentation != w.frag {
				t.Fatalf("HasBlockFragmentation = %v, want %v", got.HasBlockFragmentation, w.frag)
			}
			if a := got.AvailableSize.BlockSize.Float64(); a != w.avail {
				t.Errorf("AvailableSize.BlockSize = %v, want %v (ChildAvailableSize)", a, w.avail)
			}
			if p := got.PercentageResolutionSize.BlockSize.Float64(); p != w.percent {
				t.Errorf("PercentageResolutionSize.BlockSize = %v, want %v (ChildAvailableSize)", p, w.percent)
			}
			if !w.frag {
				return
			}
			if got.FragmentainerBlockSize != w.fragBlockSize {
				t.Errorf("FragmentainerBlockSize = %v, want %v (FragmentainerCapacity for children)", got.FragmentainerBlockSize, w.fragBlockSize)
			}
			if got.FragmentainerOffset != w.fragOffset {
				t.Errorf("FragmentainerOffset = %v, want %v", got.FragmentainerOffset, w.fragOffset)
			}
			if got.BlockFragmentationType != FragmentColumn {
				t.Errorf("BlockFragmentationType = %v, want column", got.BlockFragmentationType)
			}
			if got.IsAtFragmentainerStart != w.atStart {
				t.Errorf("IsAtFragmentainerStart = %v, want %v", got.IsAtFragmentainerStart, w.atStart)
			}
			if got.IsInitialColumnBalancingPass != w.balancingPass {
				t.Errorf("IsInitialColumnBalancingPass = %v, want %v", got.IsInitialColumnBalancingPass, w.balancingPass)
			}
			if got.IsInsideBalancedColumns != w.insideBalanced {
				t.Errorf("IsInsideBalancedColumns = %v, want %v", got.IsInsideBalancedColumns, w.insideBalanced)
			}
			if got.MinBreakAppeal != w.minBreakAppeal {
				t.Errorf("MinBreakAppeal = %v, want %v", got.MinBreakAppeal, w.minBreakAppeal)
			}
		})
	}
}

// nestedSpannerFixture is probe3's inner multicol: [a 20px, spanner holding
// a 150px block, b 20px] in a two-column multicol with 10px block padding,
// nested in a 400x100 two-column column-fill:auto multicol.
func nestedSpannerFixture(innerExtra ...string) []marginSpec {
	inner := append([]string{"column-count", "2", "column-gap", "0", "padding-top", "10px", "padding-bottom", "10px"}, innerExtra...)
	return []marginSpec{ms("mc", inner,
		ms("a", props("height", "20px")),
		ms("s", props("column-span", "all"), ms("c", props("height", "150px"))),
		ms("b", props("height", "20px")),
	)}
}

var outerFillAuto400x100 = []string{"column-count", "2", "column-gap", "0", "column-fill", "auto", "width", "400px", "height", "100px"}

// TestNestedSpannerFragmentainerCapacityExcludesClonedDecorations_LOU411: the
// spanner of a box-decoration-break:clone nested multicol fragments at the
// outer capacity less the cloned padding (FragmentainerCapacity
// is_for_children, fragmentation_utils.h:187-203 @ a9f50e522efa). Chrome
// 155 headless, probe3.html case Io.
func TestNestedSpannerFragmentainerCapacityExcludesClonedDecorations_LOU411(t *testing.T) {
	frag, ids := layoutOuterMulticolSpec(t, outerFillAuto400x100, nestedSpannerFixture("box-decoration-break", "clone")...)
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"s": {{0, 20, 200, 70}, {200, 10, 200, 80}},
		"a": {{0, 10, 100, 10}, {100, 10, 100, 10}},
		"b": {{400, 10, 100, 10}, {500, 10, 100, 10}},
	})
}

// TestNestedSpannerSliceDecorationsGuard_LOU411: guard for the capacity
// change — with slice decorations the spanner gets the full outer
// fragmentainer. Chrome 155 headless, probe3.html case Jo.
func TestNestedSpannerSliceDecorationsGuard_LOU411(t *testing.T) {
	frag, ids := layoutOuterMulticolSpec(t, outerFillAuto400x100, nestedSpannerFixture()...)
	checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
		"s": {{0, 20, 200, 80}, {200, 0, 200, 70}},
		"b": {{200, 70, 100, 10}, {300, 70, 100, 10}},
	})
}

// TestSpannerPercentageHeightWithAndWithoutOuterFragmentation_LOU411 (DoD
// D1): a height:50% spanner resolves against the multicol's content-box
// block-size (ChildAvailableSize, column_layout_algorithm.cc:1877-1878 @
// a9f50e522efa) whether or not the multicol is nested in an outer
// fragmentation context. Chrome 155 headless, probe.html cases A and B/Bo.
func TestSpannerPercentageHeightWithAndWithoutOuterFragmentation_LOU411(t *testing.T) {
	inner := func() marginSpec {
		return ms("mc", props("column-count", "2", "column-gap", "0", "width", "200px", "height", "200px"),
			ms("a", props("height", "20px")),
			ms("s", props("column-span", "all", "height", "50%")),
			ms("b", props("height", "20px")),
		)
	}
	want := map[string][]fragRect{
		"s": {{0, 10, 200, 100}},
		"b": {{0, 110, 100, 10}, {100, 110, 100, 10}},
	}
	t.Run("top-level", func(t *testing.T) {
		frag, ids := layoutOuterMulticolSpec(t, props("width", "200px"), inner())
		checkRects(t, borderBoxRectsByID(frag, ids), want)
	})
	t.Run("nested", func(t *testing.T) {
		frag, ids := layoutOuterMulticolSpec(t, props("column-count", "2", "column-gap", "0", "column-fill", "auto", "width", "400px", "height", "300px"), inner())
		checkRects(t, borderBoxRectsByID(frag, ids), want)
	})
}

// spannerOverflowFixture is probe.html's C/C0 multicol: [a, a 10px spanner
// holding s1 10px + 100px + s2 10px, b].
func spannerOverflowFixture(mcProps []string, blockHeight string) marginSpec {
	return ms("mc", mcProps,
		ms("a", props("height", blockHeight)),
		ms("s", props("column-span", "all", "height", "10px"),
			ms("s1", props("height", "10px")),
			ms("", props("height", "100px")),
			ms("s2", props("height", "10px")),
		),
		ms("b", props("height", blockHeight)),
	)
}

// TestSpannerOverflowingDeclaredHeight_LOU411 (DoD D2): a spanner whose
// content overflows its declared height is not monolithic. Blink never
// marks it so; its overflow reaches the parent through its
// BlockSizeForFragmentation (box_fragment_builder.cc:479-489, :758-761 @
// a9f50e522efa), the balanced lines around it ignore it, and nested in an
// outer fragmentation context it breaks inside like any block. Chrome 155
// headless, LOU-411 probe.html C0 (top level) and Co/C (nested).
func TestSpannerOverflowingDeclaredHeight_LOU411(t *testing.T) {
	inner := props("column-count", "2", "column-gap", "0")
	t.Run("top-level", func(t *testing.T) {
		frag, ids := layoutOuterMulticolSpec(t, nil, spannerOverflowFixture(append(inner, "width", "200px"), "40px"))
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"mc": {{0, 0, 200, 50}},
			"a":  {{0, 0, 100, 20}, {100, 0, 100, 20}},
			"s":  {{0, 20, 200, 10}},
			"s2": {{0, 130, 200, 10}},
			"b":  {{0, 30, 100, 20}, {100, 30, 100, 20}},
		})
	})
	t.Run("nested", func(t *testing.T) {
		frag, ids := layoutOuterMulticolSpec(t,
			props("column-count", "2", "column-gap", "0", "column-fill", "auto", "width", "200px", "height", "100px"),
			spannerOverflowFixture(inner, "20px"))
		checkRects(t, borderBoxRectsByID(frag, ids), map[string][]fragRect{
			"a":  {{0, 0, 50, 10}, {50, 0, 50, 10}},
			"s":  {{0, 10, 100, 10}, {100, 0, 100, 0}},
			"s1": {{0, 10, 100, 10}},
			"s2": {{100, 20, 100, 10}},
			"b":  {{0, 20, 50, 10}, {50, 20, 50, 10}},
		})
	})
}
