package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-499 (worklist C11): the behaviour of an auto inline-size comes from the
// parent's constraint space (Blink's AutoSizeBehavior, constraint_space.h:
// 70-81 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A block container
// stretches an auto-width child only when
// ShouldBlockContainerChildStretchAutoInlineSize holds (space_utils.cc:82-104
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) — never for a replaced element,
// a table or a form control — both for in-flow children
// (BlockLayoutAlgorithm::CreateConstraintSpaceForChild,
// block_layout_algorithm.cc:3428-3449) and for column spanners
// (ColumnLayoutAlgorithm::CreateConstraintSpaceForSpanner,
// column_layout_algorithm.cc:1871-1876). Otherwise the auto inline-size is
// fit-content (length_utils.cc:556-569).

// autoWidthCanvas returns a <canvas width=100 height=100> with display:block
// and the aspect-ratio its width/height attributes map to (HTML's
// presentational hint), plus extra style properties.
func autoWidthCanvas(styles map[*html.Node]*css.Style, props ...string) *html.Node {
	cv := makeNode("canvas")
	cv.Attributes = map[string]string{"width": "100", "height": "100"}
	styles[cv] = makeStyle(append([]string{"display", "block", "aspect-ratio", "100/100"}, props...)...)
	return cv
}

func layoutInAvailable(t *testing.T, root *html.Node, styles map[*html.Node]*css.Style, inline float64) *PhysicalFragment {
	t.Helper()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: inline, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: inline, BlockSize: Indefinite}).
		Build()
	res := layoutElement(testContext(), buildTestTree(root, styles), space)
	if res == nil || res.Fragment == nil {
		t.Fatal("layout returned nil")
	}
	return res.Fragment
}

func assertFragmentSize(t *testing.T, what string, f *PhysicalFragment, w, h float64) {
	t.Helper()
	if f == nil {
		t.Fatalf("%s: no fragment", what)
	}
	if gw, gh := f.Size.WidthF64(), f.Size.HeightF64(); gw != w || gh != h {
		t.Errorf("%s: size %vx%v, want %vx%v", what, gw, gh, w, h)
	}
}

// An auto-width block-level replaced element in a narrower block is not
// stretched: its fit-content inline-size is its natural 100px, and the
// aspect-ratio transfers that to a 100px block-size (not the 50px a
// stretched inline-size would give).
func TestInlineAutoBehavior_BlockChildReplacedIsFitContent_LOU499(t *testing.T) {
	styles := map[*html.Node]*css.Style{}
	cv := autoWidthCanvas(styles)
	div := makeNode("div", cv)
	styles[div] = makeStyle("display", "block", "width", "50px")
	frag := layoutInAvailable(t, div, styles, 200)
	assertFragmentSize(t, "canvas", findFragmentByNode(frag, cv), 100, 100)
}

// The same canvas as a column spanner (css-multicol
// replaced-content-spanner-auto-width): the spanner space does not stretch
// a replaced element.
func TestInlineAutoBehavior_ReplacedSpannerIsFitContent_LOU499(t *testing.T) {
	styles := map[*html.Node]*css.Style{}
	cv := autoWidthCanvas(styles, "column-span", "all")
	mc := makeNode("div", cv)
	styles[mc] = makeStyle("display", "block", "width", "50px", "column-count", "2", "column-gap", "0")
	frag := layoutInAvailable(t, mc, styles, 200)
	assertFragmentSize(t, "canvas spanner", findFragmentByNode(frag, cv), 100, 100)
}

// Regression guard: a non-replaced auto-width spanner and block child still
// stretch to the content box.
func TestInlineAutoBehavior_NonReplacedStretches_LOU499(t *testing.T) {
	styles := map[*html.Node]*css.Style{}
	sp := makeNode("div")
	styles[sp] = makeStyle("display", "block", "column-span", "all", "height", "10px")
	kid := makeNode("div")
	styles[kid] = makeStyle("display", "block", "height", "10px")
	mc := makeNode("div", sp, kid)
	styles[mc] = makeStyle("display", "block", "width", "50px", "column-count", "2", "column-gap", "0")
	frag := layoutInAvailable(t, mc, styles, 200)
	assertFragmentSize(t, "div spanner", findFragmentByNode(frag, sp), 50, 10)
	// The 10px child balances into both columns; only its width matters.
	if f := findFragmentByNode(frag, kid); f == nil || f.Size.WidthF64() != 25 {
		t.Errorf("column child: want width 25, got %v", f)
	}
}

// The spanner space's inline auto behaviour and orthogonal fallback, by
// CreateConstraintSpaceForSpanner (column_layout_algorithm.cc:1871-1876 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An orthogonal spanner sets no
// parallel behaviour (louis14 keeps the builder default) but gets the ICB
// fallback inline size.
func TestCreateConstraintSpaceForSpanner_InlineAutoBehavior_LOU499(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	for _, tc := range []struct {
		name         string
		tag          string
		props        []string
		wantBehavior AutoSizeBehavior
		wantFallback float64
	}{
		{name: "block", tag: "div", wantBehavior: AutoSizeBehaviorStretchImplicit},
		{name: "replaced", tag: "canvas", wantBehavior: AutoSizeBehaviorFitContent},
		{name: "table", tag: "div", props: []string{"display", "table"}, wantBehavior: AutoSizeBehaviorFitContent},
		{name: "form control", tag: "button", wantBehavior: AutoSizeBehaviorFitContent},
		{name: "orthogonal", tag: "div", props: []string{"writing-mode", "vertical-rl"},
			wantBehavior: AutoSizeBehaviorStretchImplicit, wantFallback: 600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sp := makeNode(tc.tag)
			mc := makeNode("div", sp)
			styles := map[*html.Node]*css.Style{
				mc: makeStyle("display", "block", "column-count", "2", "column-gap", "0"),
				sp: makeStyle(append([]string{"display", "block", "column-span", "all"}, tc.props...)...),
			}
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
				Build()
			mcNode := buildTestTree(mc, styles)
			mla := NewMulticolLayoutAlgorithm(testContext(), mcNode, space)
			mla.Layout()
			got := mla.CreateConstraintSpaceForSpanner(mcNode.Children()[0], 0, nil)
			if got.InlineAutoBehavior != tc.wantBehavior {
				t.Errorf("InlineAutoBehavior = %v, want %v", got.InlineAutoBehavior, tc.wantBehavior)
			}
			if got.OrthogonalFallbackInlineSize != tc.wantFallback {
				t.Errorf("OrthogonalFallbackInlineSize = %v, want %v", got.OrthogonalFallbackInlineSize, tc.wantFallback)
			}
		})
	}
}
