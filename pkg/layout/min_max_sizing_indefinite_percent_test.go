package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// LOU-817. During intrinsic sizing Blink gives a child an indefinite inline
// percentage size (MinMaxConstraintSpaceBuilder, constraint_space_builder.h:
// 722-764 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and a length with a
// percentage against an indefinite base resolves the percentage against 0
// for a min length and is indefinite for a main or max length
// (ResolveInlineLengthInternal, length_utils.cc:66-77; ResolveBlockLength
// Internal, :203-215 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// indefinitePercentSpace is a horizontal space whose inline and block
// percentage sizes are both indefinite.
func indefinitePercentSpace() ConstraintSpace {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	return NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: Indefinite, BlockSize: Indefinite}).
		Build()
}

func TestResolveInlineLengths_IndefinitePercentageBase(t *testing.T) {
	space := indefinitePercentSpace()
	wdm := space.WritingDirection
	// Every case leaves the max-inline-size none and the inline-size unset.
	cases := []struct {
		name    string
		style   *css.Style
		wantMin float64
	}{
		{"min calc(160px + 10%) resolves % against 0", makeStyle("min-width", "calc(160px + 10%)"), 160},
		{"min 200% resolves against 0", makeStyle("min-width", "200%"), 0},
		{"max 50% is indefinite", makeStyle("max-width", "50%"), 0},
		{"max calc(40px + 10%) is indefinite", makeStyle("max-width", "calc(40px + 10%)"), 0},
		{"width 50% is indefinite", makeStyle("width", "50%"), 0},
		{"width calc(40px + 10%) is indefinite", makeStyle("width", "calc(40px + 10%)"), 0},
	}
	for _, c := range cases {
		geom := ComputeFragmentGeometry(c.style, wdm)
		if got := ResolveMinInlineSize(c.style, wdm, space, geom).Float64(); got != c.wantMin {
			t.Errorf("%s: ResolveMinInlineSize = %v, want %v", c.name, got, c.wantMin)
		}
		if got, ok := ResolveMaxInlineSize(c.style, wdm, space, geom); ok {
			t.Errorf("%s: ResolveMaxInlineSize = %v, want none", c.name, got.Float64())
		}
		if got, ok := ResolveInlineSize(c.style, wdm, space, geom); ok {
			t.Errorf("%s: ResolveInlineSize = %v, want unset", c.name, got.Float64())
		}
	}

	// Lengths without a percentage still resolve.
	fixed := makeStyle("width", "40px", "max-width", "40px")
	geom := ComputeFragmentGeometry(fixed, wdm)
	if got, ok := ResolveMaxInlineSize(fixed, wdm, space, geom); !ok || got.Float64() != 40 {
		t.Errorf("max-width 40px: ResolveMaxInlineSize = (%v, %v), want 40", got.Float64(), ok)
	}
	if got, ok := ResolveInlineSize(fixed, wdm, space, geom); !ok || got.Float64() != 40 {
		t.Errorf("width 40px: ResolveInlineSize = (%v, %v), want 40", got.Float64(), ok)
	}
}

func TestResolveMinBlockSize_CalcPercentAgainstIndefiniteBase(t *testing.T) {
	space := indefinitePercentSpace()
	wdm := space.WritingDirection
	style := makeStyle("min-height", "calc(160px + 10%)")
	geom := ComputeFragmentGeometry(style, wdm)
	if got := ResolveMinBlockSize(style, wdm, space, geom).Float64(); got != 160 {
		t.Errorf("ResolveMinBlockSize(calc(160px + 10%%)) against an indefinite base = %v, want 160", got)
	}
}

// percentChildContribution returns the min/max-content sizes of a block
// whose only child carries childProps and holds a 50px-wide block. The
// parent's own space has a definite 80px percentage inline size, which the
// child must not inherit while it is sized intrinsically.
func percentChildContribution(t *testing.T, childProps ...string) MinMaxSizes {
	t.Helper()
	content := makeNode("div")
	child := makeNode("div", content)
	parent := makeNode("div", child)
	styles := map[*html.Node]*css.Style{
		parent:  makeStyle("display", "block"),
		child:   makeStyle(append([]string{"display", "block"}, childProps...)...),
		content: makeStyle("display", "block", "width", "50px"),
	}
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 80, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 80, BlockSize: Indefinite}).
		SetPercentageResolutionInlineSize(80).
		Build()
	return ComputeMinMaxSizes(testContext(), buildTestTree(parent, styles), space)
}

func TestMinMax_ChildPercentLengthsIgnoreParentPercentageBase(t *testing.T) {
	cases := []struct {
		name  string
		props []string
		want  float64
	}{
		// intrinsic-percent-non-replaced-001
		{"min-width 200% contributes content", []string{"min-width", "200%"}, 50},
		{"min-width calc(160px + 10%) contributes 160", []string{"min-width", "calc(160px + 10%)"}, 160},
		// intrinsic-percent-non-replaced-002
		{"max-width 50% does not clamp", []string{"max-width", "50%"}, 50},
		{"max-width calc(20px + 10%) does not clamp", []string{"max-width", "calc(20px + 10%)"}, 50},
	}
	for _, c := range cases {
		mm := percentChildContribution(t, c.props...)
		if mm.MinContent != c.want || mm.MaxContent != c.want {
			t.Errorf("%s: parent min/max = {%v %v}, want {%v %v}", c.name, mm.MinContent, mm.MaxContent, c.want, c.want)
		}
	}
}
