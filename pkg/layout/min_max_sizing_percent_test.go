package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// A definite ZERO percentage-resolution block-size must be preserved, not
// collapsed to Indefinite. ConstraintSpace's own convention (constraint_space.go
// IsBlockSizeIndefinite: only < 0 is indefinite; the builder defaults the field
// to Indefinite, never 0) means a 0 arriving here was explicitly provided by
// the parent and is a definite zero. CodeRabbit finding on PR #162 (LOU-364).
func TestResolveNodeBlockSizeForPercent_DefiniteZeroPreserved(t *testing.T) {
	node := makeNode("div")
	styles := map[*html.Node]*css.Style{
		node: makeStyle("display", "block"), // no own height → falls to branch 3
	}
	layoutRoot := buildTestTree(node, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 0}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 0}).
		Build()

	got := resolveNodeBlockSizeForPercent(layoutRoot, space)
	if got != 0 {
		t.Errorf("resolveNodeBlockSizeForPercent: got %v, want 0 (definite zero base must propagate)", got)
	}
}

// Observable consequence: a child with height:100% + aspect-ratio inside a
// zero-height percentage base must contribute 0 to the parent's intrinsic
// inline size (100% of definite 0 → 0 → ratio transfers 0), not fall back to
// its content-based width.
func TestMinMax_PercentHeightAspectRatioAgainstDefiniteZeroBase(t *testing.T) {
	grandchild := makeNode("div")
	child := makeNode("div", grandchild)
	parent := makeNode("div", child)
	styles := map[*html.Node]*css.Style{
		parent:     makeStyle("display", "block"), // height:auto → pct base flows from space
		child:      makeStyle("display", "block", "height", "100%", "aspect-ratio", "1/1"),
		grandchild: makeStyle("display", "block", "width", "50px"),
	}
	ctx := testContext()
	layoutRoot := buildTestTree(parent, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 0}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 0}).
		Build()

	mm := ComputeMinMaxSizes(ctx, layoutRoot, space)
	if mm.MinContent != 0 || mm.MaxContent != 0 {
		t.Errorf("ComputeMinMaxSizes: got {%v %v}, want {0 0} (height:100%% of definite 0 × aspect-ratio → 0, not content width 50)",
			mm.MinContent, mm.MaxContent)
	}
}
