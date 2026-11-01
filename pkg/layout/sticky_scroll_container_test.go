package layout

import "testing"

// TestStickyScrollportIsNearestScrollContainer pins that a sticky box
// sticks to its nearest scroll container ancestor, which overflow:hidden
// establishes (ComputedStyle::IsScrollContainer, core/style/
// computed_style.h:1984-1987 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b;
// the sticky constraints are computed against that container's scrollport,
// PaintLayerScrollableArea / StickyPositionScrollingConstraints).
// overflow:clip is not a scroll container, so the viewport stays the
// scrollport there and top:20px at y=200 does not move.
func TestStickyScrollportIsNearestScrollContainer(t *testing.T) {
	for _, tc := range []struct {
		overflow string
		wantY    float64
	}{
		{"hidden", 220},
		{"auto", 220},
		{"clip", 200},
	} {
		got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
			`<div style="height:200px"></div>`+
			`<div style="overflow:`+tc.overflow+`;height:300px">`+
			`<div id=st style="position:sticky;top:20px;height:10px"></div></div>`)
		if st := got["st"]; len(st) != 1 || st[0].y != tc.wantY {
			t.Errorf("overflow:%s: sticky rects = %+v, want y=%v", tc.overflow, st, tc.wantY)
		}
	}
}
