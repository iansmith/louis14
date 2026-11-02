package images

import "testing"

// SVGImage::GetNaturalDimensions (svg_image.cc:238-264 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) over
// LayoutSVGRoot::UnscaledNaturalSizingInfo.
func TestSVGSizingInfoNaturalDimensions(t *testing.T) {
	for _, tc := range []struct {
		name         string
		svg          string
		w, h         float64
		hasW, hasH   bool
		ratioW, rati float64
	}{
		{"sized", `<svg width="16" height="8" viewBox="0 0 4 4">`, 16, 8, true, true, 16, 8},
		{"viewBox only", `<svg viewBox="0 0 16 8">`, 0, 0, false, false, 16, 8},
		{"width only + viewBox", `<svg width="10" viewBox="0 0 16 8">`, 10, 0, true, false, 16, 8},
		{"width only, par none", `<svg width="10" viewBox="0 0 16 8" preserveAspectRatio="none">`, 0, 0, false, false, 0, 0},
		{"sized, par none", `<svg width="16" height="8" preserveAspectRatio="none">`, 16, 8, true, true, 16, 8},
		{"nothing", `<svg>`, 0, 0, false, false, 0, 0},
	} {
		s := GetSVGSizingInfo([]byte(tc.svg + "</svg>"))
		w, h, hasW, hasH := s.NaturalSize()
		rw, rh := s.NaturalRatio()
		if w != tc.w || h != tc.h || hasW != tc.hasW || hasH != tc.hasH || rw != tc.ratioW || rh != tc.rati {
			t.Errorf("%s: size (%v,%v,%v,%v) ratio (%v,%v)", tc.name, w, h, hasW, hasH, rw, rh)
		}
	}
}
