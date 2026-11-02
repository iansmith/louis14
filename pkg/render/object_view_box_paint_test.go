package render

// object-view-box (LOU-619, css-images worklist C5).
//
// The view box (an inset() basic shape, or rect()/xywh() computed to one,
// StyleBuilderConverter::ConvertObjectViewBox, style_builder_converter.cc:
// 3489-3496 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) replaces the
// natural size object-fit/object-position work with
// (LayoutReplaced::ComputeReplacedContentRect, layout_replaced.cc:356-420)
// and is the element's natural size for layout (ComputeNaturalSizingInfo,
// :505-516).

import (
	"image/color"
	"testing"
)

// inset(0 50% 0 0) keeps the image's left half (red), which object-fit:fill
// stretches over the whole 40×20 content box.
func TestObjectViewBox_InsetSelectsSubsetForFill(t *testing.T) {
	red, blue := color.RGBA{221, 61, 62, 255}, color.RGBA{63, 64, 222, 255}
	uri := splitPNGDataURI(t, 4, 2, red, blue)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; width:40px; height:20px; object-fit:fill; object-view-box:inset(0 50% 0 0)}</style>
<img src="` + uri + `">`
	img := renderColumnRuleHTML(t, src, 100, 100)
	wantRGB(t, img, 5, 10, red.R, red.G, red.B, "left of the box shows the view box")
	wantRGB(t, img, 35, 10, red.R, red.G, red.B, "right of the box shows the view box too")
}

// The view box is the natural size: an unsized img with xywh(0 0 2px 2px)
// on a 4×2 image is 2×2, so a following sibling starts 2px down.
func TestObjectViewBox_IsNaturalSize(t *testing.T) {
	red, blue := color.RGBA{223, 65, 66, 255}, color.RGBA{67, 68, 224, 255}
	uri := splitPNGDataURI(t, 4, 2, red, blue)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; object-view-box:xywh(0 0 2px 2px)} div{width:10px;height:10px;background:rgb(0,255,0)}</style>
<img src="` + uri + `"><div></div>`
	img := renderColumnRuleHTML(t, src, 100, 100)
	wantRGB(t, img, 0, 0, red.R, red.G, red.B, "view box at the origin")
	wantRGB(t, img, 3, 0, 255, 255, 255, "right of the 2px-wide img")
	wantRGB(t, img, 1, 3, 0, 255, 0, "sibling right below the 2px-tall img")
}
