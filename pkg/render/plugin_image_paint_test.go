package render

// <embed>/<object> showing an image (LOU-600, css-images worklist C3).
//
// HTMLPlugInElement::CreateLayoutObject makes a LayoutImage when
// IsImageType() (core/html/html_plugin_element.cc:349-364, :643-677 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the element paints its
// image through ImagePainter with object-fit / object-position, clipped to
// the content box by the UA sheet's `iframe, embed, object { overflow: clip
// !important; overflow-clip-margin: content-box !important }`
// (core/html/resources/html.css:180-183).

import (
	"image"
	"image/color"
	"testing"
)

// renderPluginImage paints a 40×40 (+5px padding) element of the given tag
// whose image (a w×h solid PNG of colour c, typed image/png) is placed with
// object-fit:none. attr is the URL attribute (data for object, src for embed).
func renderPluginImage(t *testing.T, tag, attr string, w, h int, c color.RGBA, extra string) *image.RGBA {
	t.Helper()
	uri := splitPNGDataURI(t, w, h, c, c)
	src := `<!DOCTYPE html><style>body{margin:0} ` + tag + `{display:block; width:40px; height:40px; padding:5px; object-fit:none; ` + extra + `}</style>
<` + tag + ` type="image/png" ` + attr + `="` + uri + `"></` + tag + `>`
	return RenderPage(t, src, 100, 100)
}

func TestPluginImage_ObjectPaintsImage(t *testing.T) {
	c := color.RGBA{214, 31, 32, 255}
	img := renderPluginImage(t, "object", "data", 4, 2, c, "")
	// Content box 5..45; a 4×2 image centred at x 23..26, y 24..25.
	wantRGB(t, img, 23, 24, c.R, c.G, c.B, "object image")
	wantRGB(t, img, 26, 25, c.R, c.G, c.B, "object image")
	wantRGB(t, img, 22, 24, 255, 255, 255, "left of the object image")
}

func TestPluginImage_EmbedPaintsImage(t *testing.T) {
	c := color.RGBA{33, 215, 34, 255}
	img := renderPluginImage(t, "embed", "src", 4, 2, c, "")
	wantRGB(t, img, 23, 24, c.R, c.G, c.B, "embed image")
	wantRGB(t, img, 27, 24, 255, 255, 255, "right of the embed image")
}

// The UA `overflow: clip !important` beats an author `overflow: visible`, so
// a 60px-wide image is clipped to the 40px content box, not painted into the
// padding.
func TestPluginImage_ObjectClipsToContentBox(t *testing.T) {
	c := color.RGBA{35, 36, 216, 255}
	img := renderPluginImage(t, "object", "data", 60, 4, c, "overflow:visible")
	wantRGB(t, img, 5, 24, c.R, c.G, c.B, "image at the content-box left edge")
	wantRGB(t, img, 44, 24, c.R, c.G, c.B, "image at the content-box right edge")
	wantRGB(t, img, 2, 24, 255, 255, 255, "padding left of the content box")
	wantRGB(t, img, 47, 24, 255, 255, 255, "padding right of the content box")
}
