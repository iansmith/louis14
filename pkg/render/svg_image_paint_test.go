package render

// SVG images in a LayoutImage (LOU-621, css-images worklist B4 M1).
//
// An <img> showing an SVG takes SVGImage::GetNaturalDimensions
// (svg_image.cc:238-264 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) as its
// natural sizing, and ImagePainter::PaintIntoRect draws the SVG laid out at
// the destination size (image_resource.GetImage(dest_rect.size) →
// SVGImageForContainer, image_painter.cc:205-302), not a natural-size raster
// scaled up.

import "testing"

// A 2×1 SVG (left red, right blue) drawn into a 40×20 box stays crisp: the
// column just left of the middle is pure red, not a bilinear blend.
func TestSVGImage_DrawnAtDestinationSize(t *testing.T) {
	uri := svgDataURI(`<svg xmlns="http://www.w3.org/2000/svg" width="2" height="1" viewBox="0 0 2 1" preserveAspectRatio="none"><rect width="1" height="1" fill="rgb(255,0,0)"/><rect x="1" width="1" height="1" fill="rgb(0,0,255)"/></svg>`)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; width:40px; height:20px}</style><img src="` + uri + `">`
	img := RenderPage(t, src, 100, 100)
	wantRGB(t, img, 19, 10, 255, 0, 0, "left half is pure red at the edge")
	wantRGB(t, img, 20, 10, 0, 0, 255, "right half is pure blue at the edge")
}

// An SVG with only a viewBox has a natural ratio but no natural size; with
// object-fit:none its concrete object size is the ratio contain-fitted into
// the 40×40 box (ConcreteObjectSize): 40×20, centred at y 10..29.
func TestSVGImage_NoNaturalSizeUsesConcreteObjectSize(t *testing.T) {
	uri := svgDataURI(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 8"><rect width="16" height="8" fill="rgb(0,128,0)"/></svg>`)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; width:40px; height:40px; object-fit:none}</style><img src="` + uri + `">`
	img := RenderPage(t, src, 100, 100)
	wantRGB(t, img, 1, 11, 0, 128, 0, "left edge of the 40×20 concrete object")
	wantRGB(t, img, 38, 28, 0, 128, 0, "right edge of the 40×20 concrete object")
	wantRGB(t, img, 20, 5, 255, 255, 255, "above the concrete object")
}

// svgImageForContainer lays the image's own SVG document out in the
// container (SVGImage::DrawForContainer → PaintRecordForCurrentFrame,
// svg_image.cc:283-296, :451-495 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// so the root's default preserveAspectRatio (xMidYMid meet) centres a
// viewBox-only image's 10×10 viewBox in a 40×20 container at 20×20 (x
// 10..30) instead of stretching it.
const centredSquareSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="rgb(255,0,0)"/></svg>`

func TestSVGImage_ImgLaidOutWithPreserveAspectRatio(t *testing.T) {
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; width:40px; height:20px}</style><img src="` + svgDataURI(centredSquareSVG) + `">`
	img := RenderPage(t, src, 100, 100)
	wantRGB(t, img, 4, 10, 255, 255, 255, "left band outside the centred viewBox stays unpainted")
	wantRGB(t, img, 20, 10, 255, 0, 0, "centre is the viewBox content")
	wantRGB(t, img, 35, 10, 255, 255, 255, "right band outside the centred viewBox stays unpainted")
}

func TestSVGImage_BackgroundLaidOutWithPreserveAspectRatio(t *testing.T) {
	src := `<!DOCTYPE html><style>body{margin:0} div{width:40px; height:20px; background:url("` + svgDataURI(centredSquareSVG) + `") no-repeat; background-size:40px 20px}</style><div></div>`
	img := RenderPage(t, src, 100, 100)
	wantRGB(t, img, 4, 10, 255, 255, 255, "left band outside the centred viewBox stays unpainted")
	wantRGB(t, img, 20, 10, 255, 0, 0, "centre is the viewBox content")
	wantRGB(t, img, 35, 10, 255, 255, 255, "right band outside the centred viewBox stays unpainted")
}

// An SVG image's frame view has a transparent base background
// (isolated_svg_document_host.cc:148 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): what the document leaves
// unpainted shows the <img>'s own background.
func TestSVGImage_TransparentBaseBackground(t *testing.T) {
	uri := svgDataURI(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="10" height="20" fill="rgb(255,0,0)"/></svg>`)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; background:rgb(0,0,255)}</style><img src="` + uri + `">`
	img := RenderPage(t, src, 100, 100)
	wantRGB(t, img, 5, 10, 255, 0, 0, "the document's rect")
	wantRGB(t, img, 30, 10, 0, 0, 255, "unpainted document area shows the img background")
}
