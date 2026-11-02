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

// Without a poster (and no media player) a video has no natural dimensions
// (LayoutVideo::GetNaturalDimensions → PhysicalNaturalSizingInfo::None(),
// layout_video.cc:74-112): only the 300×150 default object size applies, with
// no aspect ratio. Chrome 152: width:100px → 100×150; height:60px → 300×60
// (LOU-610).
func TestVideo_NoPosterHasNoNaturalRatio(t *testing.T) {
	src := `<!DOCTYPE html><style>body{margin:0} video{display:block; background:rgb(0,0,255)}</style>
<video style="width:100px"></video><video style="height:60px"></video>`
	img := renderColumnRuleHTML(t, src, 400, 300)
	// First video: x 0..99, y 0..149. Second: x 0..299, y 150..209.
	wantRGB(t, img, 50, 140, 0, 0, 255, "100px-wide video is 150px tall")
	wantRGB(t, img, 150, 100, 255, 255, 255, "right of the 100px-wide video")
	wantRGB(t, img, 250, 180, 0, 0, 255, "60px-tall video is 300px wide")
	wantRGB(t, img, 301, 180, 255, 255, 255, "right of the 300px-wide video")
	wantRGB(t, img, 50, 211, 255, 255, 255, "below the 60px-tall video")
}
