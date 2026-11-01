package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// LOU-473: fragmentation_utils.go parity with Blink's CalculateBreakBetweenValue
// and CalculateSpaceShortage (fragmentation_utils.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// TestCalculateBreakBetweenValue_ResumedChildIsAuto_LOU473: a child whose
// fragment resumes its node after a break is no longer *before* the node, so
// its break-before (and the propagated InitialBreakBefore) is ignored and the
// result is auto (fragmentation_utils.cc:144-157). A first-for-node child
// keeps its break-before.
func TestCalculateBreakBetweenValue_ResumedChildIsAuto_LOU473(t *testing.T) {
	for _, tc := range []struct {
		name    string
		resumed bool
		want    string
	}{
		{"first fragment for the node keeps break-before", false, "column"},
		{"resumed fragment ignores break-before", true, "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := makeNode("div")
			child := buildTestTree(n, map[*html.Node]*css.Style{
				n: makeStyle("display", "block", "break-before", "column"),
			})
			res := &LayoutResult{
				Fragment:           &PhysicalFragment{Type: FragmentBox, notFirstForNode: tc.resumed},
				InitialBreakBefore: "column",
			}
			builder := NewBoxFragmentBuilder(WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR})
			if got := CalculateBreakBetweenValue(child, res, builder); got != tc.want {
				t.Errorf("CalculateBreakBetweenValue = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCalculateSpaceShortage_AbortedLayoutIsIndefinite_LOU473: when layout
// aborted (Status != success, no fragment) and the child reported no space
// shortage, nothing can be calculated: kIndefiniteSize
// (fragmentation_utils.cc:1037-1040). A reported shortage is still trusted
// (:1046-1051).
func TestCalculateSpaceShortage_AbortedLayoutIsIndefinite_LOU473(t *testing.T) {
	space, _, _ := outOfFragmentainerSpaceFixture()
	aborted := &LayoutResult{Status: LayoutResultOutOfFragmentainerSpace}
	if got := CalculateSpaceShortage(space, aborted, 90, 100); got != Indefinite {
		t.Errorf("aborted, no reported shortage: CalculateSpaceShortage = %v, want Indefinite", got)
	}
	reported := &LayoutResult{Status: LayoutResultOutOfFragmentainerSpace, MinSpaceShortage: 7}
	if got := CalculateSpaceShortage(space, reported, 90, 100); got != 7 {
		t.Errorf("aborted, reported shortage 7: CalculateSpaceShortage = %v, want 7", got)
	}
}
