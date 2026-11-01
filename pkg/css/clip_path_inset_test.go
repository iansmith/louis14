package css

import "testing"

// TestClipPathInset_ParseAndResolve covers the inset() basic shape: 1-4
// <length-percentage> insets (TRBL expansion) plus an optional `round
// <border-radius>`, with percentages resolving against the reference box
// (insets: top/bottom against height, left/right against width; radii: Rx
// against width, Ry against height), as BasicShapeInset::GetPath does
// (core/style/basic_shapes.cc:349-379 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestClipPathInset_ParseAndResolve(t *testing.T) {
	cases := []struct {
		val    string
		w, h   float64
		insets [4]float64
		radii  EllipticalRadii
	}{
		{"inset(60px 10px 190px 320px)", 360, 260, [4]float64{60, 10, 190, 320}, EllipticalRadii{}},
		{"inset(10px)", 100, 100, [4]float64{10, 10, 10, 10}, EllipticalRadii{}},
		{"inset(0 0 385px 0)", 100, 500, [4]float64{0, 0, 385, 0}, EllipticalRadii{}},
		{"inset(10% 20px)", 200, 100, [4]float64{10, 20, 10, 20}, EllipticalRadii{}},
		{"inset(80% 0 0 round 8%)", 100, 100, [4]float64{80, 0, 0, 0},
			EllipticalRadii{{Rx: 8, Ry: 8}, {Rx: 8, Ry: 8}, {Rx: 8, Ry: 8}, {Rx: 8, Ry: 8}}},
		{"inset(0 round 10px 20px / 5px)", 100, 100, [4]float64{0, 0, 0, 0},
			EllipticalRadii{{Rx: 10, Ry: 5}, {Rx: 20, Ry: 5}, {Rx: 10, Ry: 5}, {Rx: 20, Ry: 5}}},
	}
	for _, c := range cases {
		st := NewStyle()
		st.Set("clip-path", c.val)
		cp := st.GetClipPath()
		if cp == nil || cp.Type != ClipPathInset {
			t.Errorf("%s: GetClipPath = %+v; want Type inset", c.val, cp)
			continue
		}
		r := cp.ResolveClipPath(c.w, c.h)
		if r.Insets != c.insets {
			t.Errorf("%s: resolved insets = %v; want %v", c.val, r.Insets, c.insets)
		}
		if r.InsetRadii != c.radii {
			t.Errorf("%s: resolved radii = %v; want %v", c.val, r.InsetRadii, c.radii)
		}
	}
}
