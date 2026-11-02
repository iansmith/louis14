package render

// <video poster> (LOU-606, css-images worklist C4).
//
// A video with no frame and a poster is in LayoutVideo kPoster display mode
// (layout_video.cc:121-135 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): its
// natural dimensions are the poster's (GetNaturalDimensions, :74-112) and
// VideoPainter::PaintReplaced (video_painter.cc:20-117) paints the poster
// through ImagePainter::PaintIntoRect into the replaced content rect.

import (
	"image/color"
	"testing"
)

// The poster is placed by object-fit / object-position in the content box.
func TestVideoPoster_PaintsPosterWithObjectFit(t *testing.T) {
	c := color.RGBA{217, 41, 42, 255}
	uri := splitPNGDataURI(t, 4, 2, c, c)
	src := `<!DOCTYPE html><style>body{margin:0} video{display:block; width:40px; height:40px; object-fit:none; object-position:right bottom}</style>
<video poster="` + uri + `"></video>`
	img := renderColumnRuleHTML(t, src, 100, 100)
	wantRGB(t, img, 36, 38, c.R, c.G, c.B, "poster at the bottom-right corner")
	wantRGB(t, img, 39, 39, c.R, c.G, c.B, "poster at the bottom-right corner")
	wantRGB(t, img, 35, 38, 255, 255, 255, "left of the poster")
}

// With no size set, the video takes the poster's natural size (16×8), not the
// 300×150 default: a following sibling starts right after it.
func TestVideoPoster_NaturalSizeFromPoster(t *testing.T) {
	c := color.RGBA{43, 218, 44, 255}
	uri := splitPNGDataURI(t, 16, 8, c, c)
	src := `<!DOCTYPE html><style>body{margin:0} video{display:block} div{width:10px; height:10px; background:blue}</style>
<video poster="` + uri + `"></video><div></div>`
	img := renderColumnRuleHTML(t, src, 400, 300)
	wantRGB(t, img, 8, 4, c.R, c.G, c.B, "poster at its natural size")
	wantRGB(t, img, 5, 9, 0, 0, 255, "sibling right below the 8px-tall video")
}
