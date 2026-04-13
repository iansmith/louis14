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

	// For SVG images, determine intrinsic dimensions from the SVG metadata.
	// Per the SVG/CSS specs and Blink's behavior:
	// - If the SVG has explicit width/height attributes → those are the intrinsic dimensions
	// - If only viewBox → aspect ratio from viewBox, no intrinsic dimensions
	//   (CSS default 300x150 applies, with aspect ratio constraint)
	// - If explicit width + viewBox → derive height from aspect ratio (and vice versa)
	if images.IsSVGPath(src) {
		svgInfo, err := images.GetSVGSizingInfoWithFetcher(src, ctx.ImageFetcher)
		if err == nil {
			return svgIntrinsicInfo(svgInfo)
		}
	}

	natW, natH, err := images.GetImageDimensionsWithFetcher(src, ctx.ImageFetcher)
	if err != nil || natW <= 0 || natH <= 0 {
		// Image failed to load. Per Blink's behavior, fall back to the HTML
		// width/height attributes to determine intrinsic dimensions. This
		// ensures layout is consistent regardless of whether the image loaded.
		return getImgAttrFallbackInfo(node)
	}
	return IntrinsicSizingInfo{
		IntrinsicWidth:  float64(natW),
		IntrinsicHeight: float64(natH),
		AspectRatio:     float64(natW) / float64(natH),
		HasAspectRatio:  true,
	}
}

// getImgAttrFallbackInfo returns intrinsic dimensions from the HTML width/height
// attributes when the image file can't be loaded. Matches Blink's behavior:
// broken images still use the element's width/height attributes for layout.
func getImgAttrFallbackInfo(node *LayoutInputNode) IntrinsicSizingInfo {
	var w, h float64
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
	if w <= 0 && h <= 0 {
		return IntrinsicSizingInfo{}
	}
	info := IntrinsicSizingInfo{
		IntrinsicWidth:  w,
		IntrinsicHeight: h,
	}
	if w > 0 && h > 0 {
		info.AspectRatio = w / h
		info.HasAspectRatio = true
	}
	return info
}

// svgIntrinsicInfo computes intrinsic sizing info for an SVG based on its
// root element attributes. Follows CSS/SVG spec rules and matches Blink:
// - Explicit width/height → intrinsic dimensions
// - viewBox only → aspect ratio, no intrinsic dimensions (use CSS default 300x150)
// - One explicit dimension + viewBox → derive the other from aspect ratio
func svgIntrinsicInfo(svg images.SVGSizingInfo) IntrinsicSizingInfo {
	var info IntrinsicSizingInfo

	// Compute aspect ratio from viewBox if available.
	if svg.HasViewBox && svg.ViewBoxWidth > 0 && svg.ViewBoxHeight > 0 {
		info.HasAspectRatio = true
		info.AspectRatio = svg.ViewBoxWidth / svg.ViewBoxHeight
	}

	switch {
	case svg.HasExplicitWidth && svg.HasExplicitHeight:
		info.IntrinsicWidth = svg.ExplicitWidth
		info.IntrinsicHeight = svg.ExplicitHeight
		if !info.HasAspectRatio && info.IntrinsicHeight > 0 {
			info.HasAspectRatio = true
			info.AspectRatio = info.IntrinsicWidth / info.IntrinsicHeight
		}

	case svg.HasExplicitWidth && info.HasAspectRatio:
		info.IntrinsicWidth = svg.ExplicitWidth
		info.IntrinsicHeight = svg.ExplicitWidth / info.AspectRatio

	case svg.HasExplicitHeight && info.HasAspectRatio:
		info.IntrinsicHeight = svg.ExplicitHeight
		info.IntrinsicWidth = svg.ExplicitHeight * info.AspectRatio

	default:
		// No explicit dimensions. Per CSS 2.1 §10.3.2, the replaced element
		// uses the CSS default 300x150, but the viewBox provides the aspect ratio.
		// We set IntrinsicWidth/Height to 0 to indicate no intrinsic dimensions.
		// ComputeReplacedSize will use the 300/150 defaults.
	}

	return info
}
