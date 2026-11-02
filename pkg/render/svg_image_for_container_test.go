package render

import (
	"image"
	"testing"

	"louis14/pkg/geometry"
	"louis14/pkg/images"
)

func loadTestSVGImage(t *testing.T, svg string) *images.SVGImage {
	t.Helper()
	img := images.LoadSVGImageWithFetcher(svgDataURI(svg), nil)
	if img == nil {
		t.Fatal("SVG image did not load")
	}
	return img
}

// The root is laid out at the container size floored to LayoutUnits
// (PhysicalSize::FromSizeFFloor, svg_image.cc:463-464 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and the paint is mapped from
// that laid-out size onto the destination raster, so a fractional container
// still fills the raster to its last pixel (#354 review).
func TestSVGImageForContainer_FractionalContainerFillsRaster(t *testing.T) {
	svg := loadTestSVGImage(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1" preserveAspectRatio="none"><rect width="1" height="1" fill="rgb(255,0,0)"/></svg>`)
	r := NewRenderer(1, 1)
	img := r.svgImageForContainer(svg, geometry.SizeF{Width: 10.3, Height: 10.3}, 10, 10).(*image.RGBA)
	if c := img.RGBAAt(9, 9); c.R != 255 || c.A != 255 {
		t.Errorf("last pixel = %v, want opaque red (the root maps onto the whole raster)", c)
	}
}

// svgImagesForContainer lives for one paint pass: a renderer reused for
// another Render starts with none (#354 review).
func TestSVGImageForContainer_CacheIsPerPaintPass(t *testing.T) {
	svg := loadTestSVGImage(t, `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"/>`)
	r := NewRenderer(10, 10)
	r.svgImageForContainer(svg, geometry.SizeF{Width: 4, Height: 4}, 4, 4)
	if len(r.svgImagesForContainer) != 1 {
		t.Fatalf("drawings kept = %d, want 1", len(r.svgImagesForContainer))
	}
	r.Render(nil)
	if len(r.svgImagesForContainer) != 0 {
		t.Errorf("drawings kept after a new Render = %d, want 0", len(r.svgImagesForContainer))
	}
}
