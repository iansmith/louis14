package layout

import "testing"

// TestBuild_LayoutContainmentSuppressesBaseline verifies that a box with
// layout containment exposes no first/last baseline in its LayoutResult,
// so ancestors (flex baseline alignment, inline-block vertical-align)
// synthesize from the border-box edge instead.
//
// Mirrors Blink physical_box_fragment.cc:442 @
// ea697c8865fc20dc39c19d49845a184f6d0ab24c:
//
//	const bool allow_baseline = !layout_object_->ShouldApplyLayoutContainment()
//	                            || layout_object_->IsTableCell();
func TestBuild_LayoutContainmentSuppressesBaseline(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}

	cases := []struct {
		name      string
		props     map[string]string // nil => no style set on the builder
		wantHas   bool
		wantFirst float64
		wantLast  float64
	}{
		{"contain layout suppresses baselines", map[string]string{"contain": "layout"}, false, 0, 0},
		{"contain strict suppresses baselines", map[string]string{"contain": "strict"}, false, 0, 0},
		{"table-cell exemption keeps baselines", map[string]string{"contain": "layout", "display": "table-cell"}, true, 10, 40},
		{"no containment keeps baselines", map[string]string{}, true, 10, 40},
		{"nil style keeps baselines", nil, true, 10, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoxFragmentBuilder(wdm)
			b.SetSize(LogicalSize{InlineSize: 100, BlockSize: 50})
			if tc.props != nil {
				b.SetStyle(styleWith(tc.props))
			}
			b.SetBaseline(10)
			b.SetLastBaseline(40)
			r := b.Build()
			if r.HasBaseline != tc.wantHas || r.Baseline != tc.wantFirst || r.LastBaseline != tc.wantLast {
				t.Errorf("got HasBaseline=%v Baseline=%v LastBaseline=%v, want %v/%v/%v",
					r.HasBaseline, r.Baseline, r.LastBaseline, tc.wantHas, tc.wantFirst, tc.wantLast)
			}
		})
	}
}
