package layout

import "louis14/pkg/images"

// IntrinsicSizingInfo holds the natural dimensions and aspect ratio for a
// replaced element. Uses PHYSICAL dimensions (not logical).
// Mirrors Blink's IntrinsicSizingInfo (layout_replaced.h).
type IntrinsicSizingInfo struct {
	IntrinsicWidth  float64 // natural physical width, 0 if unknown
	IntrinsicHeight float64 // natural physical height, 0 if unknown
	AspectRatio     float64 // width/height, 0 if none
	HasAspectRatio  bool
}

// GetIntrinsicSizingInfo returns the natural dimensions for a replaced element.
func GetIntrinsicSizingInfo(ctx *LayoutContext, node *LayoutInputNode) IntrinsicSizingInfo {
	if node.DOMNode == nil {
		return IntrinsicSizingInfo{}
	}
	switch node.DOMNode.TagName {
	case "img":
		return getImgIntrinsicInfo(ctx, node)
	case "canvas":
		// HTML spec: canvas intrinsic size is 300x150
		return IntrinsicSizingInfo{300, 150, 2.0, true}
	default:
		// video, iframe, embed, object, input, textarea, select, button
		return IntrinsicSizingInfo{300, 150, 2.0, true}
	}
}

func getImgIntrinsicInfo(ctx *LayoutContext, node *LayoutInputNode) IntrinsicSizingInfo {
	if ctx == nil || ctx.ImageFetcher == nil {
		return IntrinsicSizingInfo{}
	}
	src, ok := node.DOMNode.GetAttribute("src")
	if !ok || src == "" {
		return IntrinsicSizingInfo{}
	}
	natW, natH, err := images.GetImageDimensionsWithFetcher(src, ctx.ImageFetcher)
	if err != nil || natW <= 0 || natH <= 0 {
		return IntrinsicSizingInfo{}
	}
	return IntrinsicSizingInfo{
		IntrinsicWidth:  float64(natW),
		IntrinsicHeight: float64(natH),
		AspectRatio:     float64(natW) / float64(natH),
		HasAspectRatio:  true,
	}
}
