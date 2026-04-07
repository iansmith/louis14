package layout

import (
	"louis14/pkg/images"
	"strconv"
)

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
		return getCanvasIntrinsicInfo(node)
	default:
		// video, iframe, embed, object, input, textarea, select, button
		return IntrinsicSizingInfo{300, 150, 2.0, true}
	}
}

// getCanvasIntrinsicInfo returns the intrinsic dimensions for a canvas element.
// Per HTML spec, the intrinsic size comes from the width/height HTML attributes,
// defaulting to 300x150 if not specified.
func getCanvasIntrinsicInfo(node *LayoutInputNode) IntrinsicSizingInfo {
	w, h := 300.0, 150.0
	if node.DOMNode != nil {
		if val, ok := node.DOMNode.GetAttribute("width"); ok {
			if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
				w = parsed
			}
		}
		if val, ok := node.DOMNode.GetAttribute("height"); ok {
			if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
				h = parsed
			}
		}
	}
	ar := 2.0
	if h > 0 {
		ar = w / h
	}
	return IntrinsicSizingInfo{w, h, ar, true}
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
