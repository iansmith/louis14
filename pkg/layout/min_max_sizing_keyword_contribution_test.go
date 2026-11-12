package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// LOU-822. A box's min/max-content contribution is its main length clamped
// by its min and max lengths, intrinsic keywords included, whether or not
// the main length is fixed (ComputeMinAndMaxContentContributionInternal,
// length_utils.cc:357-455 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A
// fit-content min or max length clamps the min-content contribution as
// min-content and the max-content contribution as max-content (:436-449).

// keywordChildContribution returns the min/max-content sizes of a block
// whose only child carries childProps and holds two 50px inline-blocks, so
// the child's content min-content is 50 and max-content 100.
func keywordChildContribution(childProps ...string) MinMaxSizes {
	a, b := makeNode("span"), makeNode("span")
	child := makeNode("div", a, b)
	parent := makeNode("div", child)
	inlineBlock := func() *css.Style { return makeStyle("display", "inline-block", "width", "50px", "height", "10px") }
	styles := map[*html.Node]*css.Style{
		parent: makeStyle("display", "block"),
		child:  makeStyle(append([]string{"display", "block"}, childProps...)...),
		a:      inlineBlock(),
		b:      inlineBlock(),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: Indefinite}).
		Build()
	return ComputeMinMaxSizes(testContext(), buildTestTree(parent, styles), space)
}

func TestMinMax_ChildIntrinsicKeywordMinMax(t *testing.T) {
	cases := []struct {
		name  string
		props []string
		want  MinMaxSizes
	}{
		{"control: content sizes", nil, MinMaxSizes{MinContent: 50, MaxContent: 100}},
		// fit-content-min-inline-size
		{"width 10px, min-width fit-content", []string{"width", "10px", "min-width", "fit-content"}, MinMaxSizes{MinContent: 50, MaxContent: 100}},
		// fit-content-max-inline-size.tentative
		{"width 200px, max-width fit-content", []string{"width", "200px", "max-width", "fit-content"}, MinMaxSizes{MinContent: 50, MaxContent: 100}},
		// shrink-to-fit-sizing-max-width-min-content
		{"width 600px, max-width min-content", []string{"width", "600px", "max-width", "min-content"}, MinMaxSizes{MinContent: 50, MaxContent: 50}},
		{"width 10px, min-width max-content", []string{"width", "10px", "min-width", "max-content"}, MinMaxSizes{MinContent: 100, MaxContent: 100}},
		{"auto width, max-width min-content", []string{"max-width", "min-content"}, MinMaxSizes{MinContent: 50, MaxContent: 50}},
		{"auto width, min-width fit-content", []string{"min-width", "fit-content"}, MinMaxSizes{MinContent: 50, MaxContent: 100}},
		// A min length wins over a smaller max length
		// (MinMaxSizes::Constrain then Encompass, length_utils.cc:451-452).
		{"width 80px, min-width 60px, max-width 40px", []string{"width", "80px", "min-width", "60px", "max-width", "40px"}, MinMaxSizes{MinContent: 60, MaxContent: 60}},
	}
	for _, c := range cases {
		if got := keywordChildContribution(c.props...); got != c.want {
			t.Errorf("%s: parent min/max = %+v, want %+v", c.name, got, c.want)
		}
	}
}
