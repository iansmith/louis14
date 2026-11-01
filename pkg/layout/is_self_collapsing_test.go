package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// layoutSelfCollapsingProbe lays out a block styled with props holding the
// given children (each with its own style), in a 200px-wide block flow.
func layoutSelfCollapsingProbe(t *testing.T, token *BlockBreakToken, props []string, kids ...[]string) *LayoutResult {
	t.Helper()
	styles := map[*html.Node]*css.Style{}
	var kidNodes []*html.Node
	for _, k := range kids {
		n := makeNode("div")
		styles[n] = makeStyle(k...)
		kidNodes = append(kidNodes, n)
	}
	el := makeNode("div", kidNodes...)
	styles[el] = makeStyle(append([]string{"display", "block"}, props...)...)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	b := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite})
	if token != nil {
		b.SetBreakToken(token)
	}
	return NewBlockLayoutAlgorithm(testContext(), buildTestTree(el, styles), b.Build()).Layout()
}

// TestBlockLayout_IsSelfCollapsingDecidedByTheBlock (LOU-508): a block's own
// algorithm reports it self-collapsing when its BFC block-offset is still
// unresolved at the end of layout: no block-start or block-end border or
// padding, nothing that resolves it (a non-self-collapsing child, a line
// box), a zero block-size, not resumed after a break, not a new formatting
// context. A float alone does not resolve it (an adjoining float). Mirrors
// BlockLayoutAlgorithm::FinishLayout → SetIsSelfCollapsing
// (block_layout_algorithm.cc:1406-1420, :1455-1462 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBlockLayout_IsSelfCollapsingDecidedByTheBlock(t *testing.T) {
	block := []string{"display", "block"}
	for _, tc := range []struct {
		name  string
		token *BlockBreakToken
		props []string
		kids  [][]string
		want  bool
	}{
		{"empty", nil, nil, nil, true},
		{"empty child", nil, nil, [][]string{block}, true},
		{"only a float", nil, nil, [][]string{{"display", "block", "float", "left", "width", "10px", "height", "10px"}}, true},
		{"block-size", nil, []string{"height", "10px"}, nil, false},
		{"min-block-size", nil, []string{"min-height", "1px"}, nil, false},
		{"block-start border", nil, []string{"border-top-width", "1px", "border-top-style", "solid"}, nil, false},
		{"block-end padding", nil, []string{"padding-bottom", "1px"}, nil, false},
		{"non-empty child", nil, nil, [][]string{{"display", "block", "height", "1px"}}, false},
		{"new formatting context", nil, []string{"display", "flow-root"}, nil, false},
		{"resumed", &BlockBreakToken{}, nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := layoutSelfCollapsingProbe(t, tc.token, tc.props, tc.kids...).IsSelfCollapsing; got != tc.want {
				t.Errorf("IsSelfCollapsing = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBlockLayout_MarginsCollapseThroughBlockWithOnlyAFloat (LOU-508): an
// empty block whose only child is a float is self-collapsing, so the
// margins around it collapse through it (CSS 2.1 §8.3.1; Blink's adjoining
// floats leave its BFC block-offset unresolved): the 30px margin-top of the
// next block collapses with the 20px margin-bottom of the previous one.
func TestBlockLayout_MarginsCollapseThroughBlockWithOnlyAFloat(t *testing.T) {
	a := makeNode("div")
	f := makeNode("div")
	b := makeNode("div", f)
	c := makeNode("div")
	parent := makeNode("div", a, b, c)
	styles := map[*html.Node]*css.Style{
		parent: makeStyle("display", "block", "width", "200px"),
		a:      makeStyle("display", "block", "height", "10px", "margin-bottom", "20px"),
		b:      makeStyle("display", "block"),
		f:      makeStyle("display", "block", "float", "left", "width", "10px", "height", "10px"),
		c:      makeStyle("display", "block", "height", "10px", "margin-top", "30px"),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: Indefinite}).
		Build()
	result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(parent, styles), space).Layout()
	cLink := childLinkFor(result.Fragment, c)
	if cLink == nil {
		t.Fatal("no fragment for the block after the float's container")
	}
	if got := cLink.Offset.Top.Float64(); got != 40 {
		t.Errorf("next block top = %v, want 10 + max(20, 30) = 40", got)
	}
}
