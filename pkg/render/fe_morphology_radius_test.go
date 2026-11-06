package render_test

import "testing"

// LOU-751 (filter-effects worklist B10): an feMorphology radius is a float,
// rounded half up in layer space (SkMorphologyImageFilter::radii,
// SkMorphologyImageFilter.cpp:72-79 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e); both radii zero is the input
// (:113-118), and a zero on one axis skips that axis's pass (:187-220).
// Blink passes the float radii through (FEMorphology, fe_morphology.cc:
// 86-97 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// morphologyPage dilates a 20×20 green square at (40,40) with radius.
func morphologyPage(radius string) string {
	return `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="120" height="120"><filter id="m" color-interpolation-filters="sRGB">
<feMorphology operator="dilate" radius="` + radius + `"/></filter>
<rect x="40" y="40" width="20" height="20" fill="green" filter="url(#m)"/></svg>`
}

func TestFEMorphology_Radius(t *testing.T) {
	for _, tc := range []struct {
		name, radius string
		green        [][2]int // pixels the dilated square covers
		white        [][2]int // pixels it does not
	}{
		// tainting-femorphology-001: 0.5 rounds to 1.
		{"half rounds up", "0.5", [][2]int{{50, 50}, {39, 50}, {50, 39}}, [][2]int{{38, 50}, {50, 38}}},
		{"zero passes through", "0", [][2]int{{50, 50}}, [][2]int{{39, 50}, {50, 39}}},
		{"zero on one axis", "2 0", [][2]int{{50, 50}, {38, 50}, {61, 50}}, [][2]int{{37, 50}, {62, 50}, {50, 39}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderToImage(t, morphologyPage(tc.radius), 120, 120)
			for _, p := range tc.green {
				wantPixel(t, img, p[0], p[1], canvasGreen, "inside the dilated square")
			}
			for _, p := range tc.white {
				wantPixel(t, img, p[0], p[1], canvasWhite, "outside the dilated square")
			}
		})
	}
}

// With primitiveUnits="objectBoundingBox" the radius is a fraction of the
// reference box (Filter::ApplyHorizontalScale / ApplyVerticalScale, filter.cc:
// 65-75 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): radius 0.1 on a 100px
// square dilates it by 10px.
func TestFEMorphology_RadiusObjectBoundingBoxUnits(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="140" height="140"><filter id="m" color-interpolation-filters="sRGB" primitiveUnits="objectBoundingBox">
<feMorphology operator="dilate" radius="0.1"/></filter>
<rect x="20" y="20" width="100" height="100" fill="green" filter="url(#m)"/></svg>`, 140, 140)
	wantPixel(t, img, 11, 70, canvasGreen, "10px left of the square, inside the dilation")
	wantPixel(t, img, 9, 70, canvasWhite, "past the dilation")
}
