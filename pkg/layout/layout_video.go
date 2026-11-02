package layout

import "louis14/pkg/html"

// VideoDisplayMode mirrors LayoutVideo::DisplayMode (core/layout/
// layout_video.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type VideoDisplayMode int

const (
	VideoDisplayModePoster VideoDisplayMode = iota
	VideoDisplayModeVideo
)

// GetVideoDisplayMode mirrors LayoutVideo::GetDisplayMode
// (core/layout/layout_video.cc:121-135 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the poster shows when the
// show-poster flag is set or there is no video frame, and there is a poster.
// louis14 has no media player, so a video never has a frame.
func GetVideoDisplayMode(video *html.Node) VideoDisplayMode {
	if video.PosterImageURL() != "" {
		return VideoDisplayModePoster
	}
	return VideoDisplayModeVideo
}

// getVideoNaturalDimensions mirrors LayoutVideo::GetNaturalDimensions
// (layout_video.cc:74-112 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): in
// poster mode the poster image's natural dimensions
// (LayoutImage::GetNaturalDimensions) unless it failed to load. Otherwise
// Blink asks the media player, and without one returns no natural
// dimensions; louis14 keeps its 300×150, 2:1 stand-in for that case
// (worklist row B7).
func getVideoNaturalDimensions(ctx *LayoutContext, node *LayoutInputNode) IntrinsicSizingInfo {
	if GetVideoDisplayMode(node.DOMNode) == VideoDisplayModePoster {
		if info := getImgIntrinsicInfo(ctx, node); info.IntrinsicWidth > 0 && info.IntrinsicHeight > 0 {
			return info
		}
	}
	return IntrinsicSizingInfo{300, 150, 2.0, true}
}

// LayoutImageSource reports whether n's layout object is a LayoutImage and,
// if so, the image URL its ImageResource shows (uncompleted; "" for none):
// Element::ImageSourceURL for an <img> or an image-typed <object>/<embed>
// (html.CreatesLayoutImage), and for a <video> (LayoutVideo : LayoutMedia :
// LayoutImage) its poster in kPoster display mode.
func LayoutImageSource(n *html.Node) (url string, ok bool) {
	switch {
	case n == nil || n.Type != html.ElementNode:
		return "", false
	case n.TagName == "video":
		if GetVideoDisplayMode(n) == VideoDisplayModePoster {
			return n.PosterImageURL(), true
		}
		return "", true
	case html.CreatesLayoutImage(n):
		return n.ImageSourceURL(), true
	}
	return "", false
}
