package layout

import (
	"fmt"
	"testing"
)

// TestInlineBlockBaselineSynthesis checks when an inline-block takes its
// baseline from its block-end margin edge instead of its last line: only
// when it is a scroll container whose block-axis overflow is neither visible
// nor clip (PhysicalBoxFragment::ForceInlineBaselineSynthesis,
// core/layout/physical_box_fragment.h:169-173 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). overflow:clip makes no scroll
// container, so it keeps its content baseline. A scroll container that
// scrolls only inline is possible only in a vertical writing mode, which is
// not tested here.
//
// "A" and the inline-block's "B" are 20px Ahem (ascent 16). With the
// content baseline the two tops line up; with the synthesized one the
// 50px-tall inline-block sits on the baseline, its top 34px above A's.
func TestInlineBlockBaselineSynthesis(t *testing.T) {
	cases := []struct {
		overflow    string
		synthesized bool
	}{
		{"overflow:visible", false},
		{"overflow:clip", false},
		{"overflow-x:clip", false},
		{"overflow:hidden", true},
		{"overflow:auto", true},
		// overflow-y computes to auto beside a scrolling x axis.
		{"overflow-x:scroll", true},
	}
	for _, tc := range cases {
		t.Run(tc.overflow, func(t *testing.T) {
			root := layoutRootTestHTML(t, fmt.Sprintf(`<!doctype html><style>body{margin:0}</style>`+
				`<div style="font:20px/1 Ahem"><span id=a>A</span><span id=ib style="display:inline-block;height:50px;%s">B</span></div>`,
				tc.overflow))
			a, ib := findBoxByID(root, "a"), findBoxByID(root, "ib")
			if a == nil || ib == nil {
				t.Fatal("missing #a or #ib")
			}
			const ascent, height = 16, 50
			want := a.Y
			if tc.synthesized {
				want = a.Y + ascent - height
			}
			if ib.Y != want {
				t.Errorf("inline-block top = %g, want %g (A's top %g; synthesized baseline: %v)", ib.Y, want, a.Y, tc.synthesized)
			}
		})
	}
}
