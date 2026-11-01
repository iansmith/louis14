package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// LOU-470: a <br> with clear adds its clearance *after* the line it ends
// (InlineLayoutAlgorithm::AddAnyClearanceAfterLine, inline/
// inline_layout_algorithm.cc:1021-1065, consumed by BlockLayoutAlgorithm::
// ComputeInflowPosition :2955-2980 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). When the clearance would have
// to go past a float that continues in the next fragmentainer, the line is
// aborted with LayoutResultOutOfFragmentainerSpace and the block breaks
// before it (:1501-1503); the line breaker first ends the line before such
// a BR when anything precedes it on the line (LineBreaker::
// HandleForcedLineBreak, inline/line_breaker.cc:2867-2903).

// brClearTestStyles styles p as 10px Ahem lines and br as a BR clearing
// clearType.
func brClearTestStyles(p, br *html.Node, clearType string) map[*html.Node]*css.Style {
	text := []string{"font-family", "Ahem", "font-size", "10px", "line-height", "10px"}
	return map[*html.Node]*css.Style{
		p:  makeStyle(append([]string{"display", "block"}, text...)...),
		br: makeStyle(append([]string{"display", "inline", "clear", clearType}, text...)...),
	}
}

// nonEmptyLineTops returns the block offsets of frag's non-empty line boxes.
func nonEmptyLineTops(frag *PhysicalFragment) []float64 {
	var tops []float64
	for _, ch := range frag.Children {
		if ch.Fragment.Type == FragmentLineBox && ch.Fragment.Size.HeightF64() > 0 {
			tops = append(tops, ch.Offset.Top.Float64())
		}
	}
	return tops
}

func equalTops(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Without fragmentation, the line after a <br clear=left> starts below the
// 50px float beside it, and the paragraph stretches past the float.
func TestAddAnyClearanceAfterLine_PushesNextLinePastFloat(t *testing.T) {
	float := makeNode("div")
	br := makeNode("br")
	p := makeNode("p", makeTextNode("x"), br, makeTextNode("y"))
	root := makeNode("div", float, p)
	styles := brClearTestStyles(p, br, "left")
	styles[root] = makeStyle("display", "block", "width", "100px")
	styles[float] = makeStyle("display", "block", "float", "left", "width", "20px", "height", "50px")

	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		Build()
	result := NewBlockLayoutAlgorithm(testContext(), buildTestTree(root, styles), space).Layout()
	t.Cleanup(func() {
		if t.Failed() {
			dumpFragmentTree(t, "", result.Fragment)
		}
	})
	ps := fragmentsOfNode(result.Fragment, p)
	if len(ps) != 1 {
		t.Fatalf("p fragments = %d, want 1", len(ps))
	}
	if tops := nonEmptyLineTops(ps[0].Fragment); !equalTops(tops, []float64{0, 50}) {
		t.Errorf("line tops = %v, want [0 50]", tops)
	}
	if h := ps[0].Fragment.Size.HeightF64(); h != 60 {
		t.Errorf("p height = %v, want 60", h)
	}
}

// A 150px float breaks inside the first 100px column, so a <br clear=left>
// cannot clear it there: the text before the BR keeps its own line in the
// first column, and the BR's line breaks before, to the second column,
// where the float ends at 50px and the line after the BR goes below it.
func TestAddAnyClearanceAfterLine_BreaksBeforeLineClearingPastFragmentainer(t *testing.T) {
	float := makeNode("div")
	br := makeNode("br")
	br0 := makeNode("br")
	p := makeNode("p", makeTextNode("a"), br0, makeTextNode("x"), br, makeTextNode("y"))
	mc := makeNode("div", float, p)
	styles := brClearTestStyles(p, br, "left")
	styles[br0] = makeStyle("display", "inline", "font-family", "Ahem", "font-size", "10px", "line-height", "10px")
	styles[mc] = makeStyle("display", "block", "column-count", "2", "column-gap", "0",
		"column-fill", "auto", "height", "100px", "orphans", "1", "widows", "1")
	styles[float] = makeStyle("display", "block", "float", "left", "width", "20px", "height", "150px")

	result := layoutMulticolForFloatTest(t, mc, styles)
	cols := columnChildren(result)
	if len(cols) != 2 {
		t.Fatalf("columns = %d, want 2", len(cols))
	}
	want := [][]float64{{0, 10}, {0, 50}}
	for i, col := range cols {
		ps := fragmentsOfNode(col.Fragment, p)
		if len(ps) != 1 {
			t.Fatalf("column %d: p fragments = %d, want 1", i, len(ps))
		}
		if tops := nonEmptyLineTops(ps[0].Fragment); !equalTops(tops, want[i]) {
			t.Errorf("column %d: line tops = %v, want %v", i, tops, want[i])
		}
	}
}
