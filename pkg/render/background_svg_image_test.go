package render

// LOU-556: SVG background images report Blink's natural sizing info
// (SVGImage::GetNaturalDimensions → LayoutSVGRoot::UnscaledNaturalSizingInfo)
// and are drawn at the concrete object size (SVGImageForContainer created
// with the geometry's TileSize(), box_painter_base.cc:1441-1443 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

import (
	"encoding/base64"
	"testing"

	"louis14/pkg/images"
)

func svgDataURI(svg string) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

// An SVG with no width, height or viewBox has no natural size or ratio, so
// `background-size: 200px auto` resolves the auto height to the positioning
// area height (400) and the image is laid out in a 200×400 container: the
// 100%×100% rect's 32px stroke is 16px wide on every inner edge, not
// stretched from a fixed-size raster (background-size-043 shape).
func TestBackgroundSVG_NoNaturalSize_DrawnAtTileSize(t *testing.T) {
	uri := svgDataURI(`<svg xmlns="http://www.w3.org/2000/svg" version="1.1" preserveAspectRatio="none">
  <rect width="100%" height="100%" fill="lime" stroke="blue" stroke-width="32" />
</svg><!-- LOU-556 no-natural-size -->`)
	src := `<!DOCTYPE html><body style="margin:0">
<div style="background-color:red; background-image:url(` + uri + `);
 background-origin:content-box; background-position:center; background-repeat:no-repeat;
 background-size:200px auto; border:solid 40px transparent; width:400px; height:400px"></div>`
	img := renderColumnRuleHTML(t, src, 500, 500)
	// Tile spans x 140..340, y 40..440.
	wantRGB(t, img, 150, 240, 0, 0, 255, "left stroke is 16px wide (x 140..156)")
	wantRGB(t, img, 330, 240, 0, 0, 255, "right stroke is 16px wide (x 324..340)")
	wantRGB(t, img, 240, 50, 0, 0, 255, "top stroke is 16px tall (y 40..56)")
	wantRGB(t, img, 240, 70, 0, 255, 0, "fill starts at y 56, not at a stretched stroke")
	wantRGB(t, img, 240, 430, 0, 0, 255, "bottom stroke is 16px tall (y 424..440)")
	wantRGB(t, img, 240, 240, 0, 255, 0, "fill inside the strokes")
	wantRGB(t, img, 100, 240, 255, 0, 0, "left of the tile: background-color only")
}

// naturalSizingInfoForSVG mirrors LayoutSVGRoot::UnscaledNaturalSizingInfo
// (layout_svg_root.cc:74-103) plus the preserveAspectRatio="none" rule of
// SVGImage::GetNaturalDimensions (svg_image.cc:238-264).
func TestNaturalSizingInfoForSVG(t *testing.T) {
	cases := []struct {
		name string
		svg  string
		want NaturalSizingInfo
	}{
		{"none", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`, NaturalSizingInfo{}},
		{"percent size is not natural", `<svg width="100%" height="50%"></svg>`, NaturalSizingInfo{}},
		{"width and height", `<svg width="40" height="20" viewBox="0 0 10 10"></svg>`,
			NaturalSizingInfo{Width: 40, Height: 20, RatioW: 40, RatioH: 20, HasWidth: true, HasHeight: true}},
		{"width and viewBox", `<svg width="40" viewBox="0 0 10 20"></svg>`,
			NaturalSizingInfo{Width: 40, RatioW: 10, RatioH: 20, HasWidth: true}},
		{"viewBox only", `<svg viewBox="0 0 16 8"></svg>`, NaturalSizingInfo{RatioW: 16, RatioH: 8}},
		{"viewBox only, preserveAspectRatio none", `<svg viewBox="0 0 16 8" preserveAspectRatio="none"></svg>`,
			NaturalSizingInfo{}},
		{"fixed size, preserveAspectRatio none", `<svg width="16" height="8" viewBox="0 0 1 1" preserveAspectRatio="none"></svg>`,
			NaturalSizingInfo{Width: 16, Height: 8, RatioW: 16, RatioH: 8, HasWidth: true, HasHeight: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := naturalSizingInfoForSVG(images.GetSVGSizingInfo([]byte(c.svg)))
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
