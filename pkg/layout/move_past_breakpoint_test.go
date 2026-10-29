package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
)

// TestMovePastBreakpoint_InnerMulticolAtFragmentainerEndBreaksBefore
// (LOU-383): a child whose border edge starts exactly at the fragmentainer
// end and that broke inside must be broken before, not placed as an empty
// fragment and dropped (MovePastBreakpoint must_break_before,
// fragmentation_utils.cc:1172-1189 @ a9f50e522efa). Mirrors
// multicol-rule-nested-balancing-001 with column-gap:0; Chrome 155 (probe
// prnb0) pushes the 200px inner multicol whole to outer column 2, where its
// two columns are 150 tall.
func TestMovePastBreakpoint_InnerMulticolAtFragmentainerEndBreaksBefore(t *testing.T) {
	innerBlock := makeNode("div")
	innerMC := makeNode("div", innerBlock)
	outerBlock := makeNode("div")
	outerMC := makeNode("div", outerBlock, innerMC)
	styles := map[*html.Node]*css.Style{
		outerMC:    makeStyle("display", "block", "width", "400px", "height", "250px", "column-count", "2", "column-gap", "0"),
		outerBlock: makeStyle("display", "block", "height", "200px"),
		innerMC:    makeStyle("display", "block", "height", "200px", "column-count", "2", "column-gap", "0"),
		innerBlock: makeStyle("display", "block", "height", "300px"),
	}
	ctx := testContext()
	outerNode := buildTestTree(outerMC, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
		Build()
	result := layoutElement(ctx, outerNode, space)
	if result == nil || result.Fragment == nil {
		t.Fatal("outer mc layout returned nil")
	}
	dumpFragmentTree(t, "", result.Fragment)
	if len(result.Fragment.Children) != 2 {
		t.Fatalf("outer mc has %d columns, want 2", len(result.Fragment.Children))
	}
	if l := childLinkFor(result.Fragment.Children[0].Fragment, innerMC); l != nil {
		t.Errorf("outer col 1 holds an inner mc fragment (h=%v); want it broken before",
			NewLogicalFragment(wdm, l.Fragment).BlockSize())
	}
	inner := childLinkFor(result.Fragment.Children[1].Fragment, innerMC)
	if inner == nil {
		t.Fatal("outer col 2 has no inner mc fragment")
	}
	if got := NewLogicalFragment(wdm, inner.Fragment).BlockSize(); got != 200 {
		t.Errorf("inner mc in outer col 2: block-size = %v, want 200", got)
	}
	if got := len(inner.Fragment.Children); got != 2 {
		t.Fatalf("inner mc in outer col 2 has %d columns, want 2", got)
	}
	for i, col := range inner.Fragment.Children {
		if got := NewLogicalFragment(wdm, col.Fragment).BlockSize(); got != 150 {
			t.Errorf("inner column %d block-size = %v, want 150", i, got)
		}
	}
}

// TestMovePastBreakpoint_ChildPushedPastEndByMarginBreaksBefore (LOU-383): a
// child whose block-start margin pushes its border edge past the
// fragmentainer end must be broken before (must_break_before for
// space_left < 0, fragmentation_utils.cc:1174-1176 @ a9f50e522efa); it starts
// the next column with its margin truncated.
func TestMovePastBreakpoint_ChildPushedPastEndByMarginBreaksBefore(t *testing.T) {
	first := makeNode("div")
	pushed := makeNode("div")
	container := makeNode("div", first, pushed)
	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block"),
		first:     makeStyle("display", "block", "height", "90px"),
		pushed:    makeStyle("display", "block", "height", "10px", "margin-top", "20px"),
	}
	mc := layoutTwoColumn(t, container, styles, "auto")
	if l := childLinkFor(mc.Children[0].Fragment.Children[0].Fragment, pushed); l != nil {
		t.Errorf("pushed child placed in column 1 at %v; want it broken before", l.Offset.Top.Float64())
	}
	col2 := mc.Children[1].Fragment
	if len(col2.Children) == 0 {
		t.Fatal("column 2 is empty")
	}
	l := childLinkFor(col2.Children[0].Fragment, pushed)
	if l == nil {
		t.Fatal("column 2 has no fragment for the pushed child")
	}
	if got := l.Offset.Top.Float64(); got != 0 {
		t.Errorf("pushed child in column 2 at %v, want 0 (margin truncated after the break)", got)
	}
}

// TestMovePastBreakpoint_ChildThatDoesNotFitBreaksBefore (LOU-392): Blink's
// fit rule (fragmentation_utils.cc:1191-1226 @ a9f50e522efa) breaks before
// any child whose block-size for fragmentation exceeds the space left — not
// only break-inside:avoid or monolithic ones — unless breaking there is
// refused (no progress yet) or content is required before breaking.
func TestMovePastBreakpoint_ChildThatDoesNotFitBreaksBefore(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := ConstraintSpace{
		WritingDirection:       wdm,
		HasBlockFragmentation:  true,
		FragmentainerBlockSize: 100,
		BlockFragmentationType: FragmentColumn,
		MinBreakAppeal:         BreakAppealLastResort,
	}
	tall := &LayoutResult{BreakAppeal: BreakAppealPerfect, Fragment: &PhysicalFragment{Size: geometry.PhysicalSizeFromF64Round(50, 80)}}
	for _, tc := range []struct {
		name   string
		offset float64
		space  ConstraintSpace
		want   bool
	}{
		{"mid-fragmentainer, does not fit: break before", 50, space, false},
		{"at fragmentainer start: refuse, move past", 0, space, true},
		{"fits: move past", 20, space, true},
	} {
		builder := NewBoxFragmentBuilder(wdm)
		if got := MovePastBreakpoint(tc.space, tall, tc.offset, 100, BreakAppealPerfect, builder); got != tc.want {
			t.Errorf("%s: MovePastBreakpoint = %v, want %v", tc.name, got, tc.want)
		}
	}
}
