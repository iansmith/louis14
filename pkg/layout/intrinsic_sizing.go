package layout

import (
	"louis14/pkg/images"
	"strconv"
	"strings"
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
	case "svg":
		return getInlineSVGIntrinsicInfo(node)
	case "canvas":
		return getCanvasIntrinsicInfo(node)
	case "video":
		// Per HTML spec, video has intrinsic dimensions (default 300×150 if no
		// source) and an intrinsic aspect ratio.
		return IntrinsicSizingInfo{300, 150, 2.0, true}
	case "iframe", "embed", "object":
		// Per CSS 2.1 §10.3.2, these have a default 300×150 size but NO
		// intrinsic aspect ratio. Unlike images/video, their content doesn't
		// define a natural ratio between width and height.
		return IntrinsicSizingInfo{300, 150, 0, false}
	case "textarea":
		// Form controls have intrinsic dimensions but no aspect ratio.
		// Textarea default: ~173×54 border-box per UA stylesheet; content-box
		// is 167×48 (minus 2×1px border + 2×2px padding).
		return IntrinsicSizingInfo{167, 48, 0, false}
	case "input":
		inputType, _ := node.DOMNode.GetAttribute("type")
		if inputType == "checkbox" || inputType == "radio" {
			return IntrinsicSizingInfo{13, 13, 1.0, true}
		}
		// Text input default: ~173×19 border-box per UA stylesheet; content-box
		// is 165×11 (minus 2×2px border + 2×(1+2)px padding).
		return IntrinsicSizingInfo{165, 11, 0, false}
	case "select":
		// Select default: ~173×19 border-box; content-box is 167×13.
		return IntrinsicSizingInfo{167, 13, 0, false}
	case "button":
		// Button sizes to content; provide reasonable fallback.
		return IntrinsicSizingInfo{0, 0, 0, false}
	default:
		return IntrinsicSizingInfo{300, 150, 0, false}
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

// getInlineSVGIntrinsicInfo returns intrinsic sizing info for an inline <svg>
// element based on its width/height/viewBox attributes. Matches Blink's
// behavior and svgIntrinsicInfo for SVG images:
// - Explicit width + height → intrinsic dimensions (aspect ratio derived).
// - viewBox only → aspect ratio from viewBox; no intrinsic dimensions
//   (ComputeReplacedSize applies the CSS default 300x150 subject to ratio).
// - One explicit dimension + viewBox → derive the other via aspect ratio.
func getInlineSVGIntrinsicInfo(node *LayoutInputNode) IntrinsicSizingInfo {
	if node == nil || node.DOMNode == nil {
		return IntrinsicSizingInfo{}
	}
	var info IntrinsicSizingInfo

	var explW, explH float64
	var hasExplW, hasExplH bool
	if val, ok := node.DOMNode.GetAttribute("width"); ok {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			explW = parsed
			hasExplW = true
		}
	}
	if val, ok := node.DOMNode.GetAttribute("height"); ok {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			explH = parsed
			hasExplH = true
		}
	}

	// Parse viewBox "min-x min-y width height" for aspect ratio.
	// Our HTML tokenizer lowercases attribute names, so SVG's camelCase
	// viewBox is stored as "viewbox". Try both to be robust.
	var vbW, vbH float64
	hasViewBox := false
	vb, ok := node.DOMNode.GetAttribute("viewBox")
	if !ok {
		vb, ok = node.DOMNode.GetAttribute("viewbox")
	}
	if ok {
		parts := strings.Fields(vb)
		if len(parts) == 4 {
			if w, err := strconv.ParseFloat(parts[2], 64); err == nil && w > 0 {
				if h, err2 := strconv.ParseFloat(parts[3], 64); err2 == nil && h > 0 {
					vbW, vbH = w, h
					hasViewBox = true
					info.HasAspectRatio = true
					info.AspectRatio = vbW / vbH
				}
			}
		}
	}

	switch {
	case hasExplW && hasExplH:
		info.IntrinsicWidth = explW
		info.IntrinsicHeight = explH
		if !info.HasAspectRatio && explH > 0 {
			info.HasAspectRatio = true
			info.AspectRatio = explW / explH
		}
	case hasExplW && hasViewBox:
		info.IntrinsicWidth = explW
		info.IntrinsicHeight = explW / info.AspectRatio
	case hasExplH && hasViewBox:
		info.IntrinsicHeight = explH
		info.IntrinsicWidth = explH * info.AspectRatio
	default:
		// No explicit dimensions. Aspect ratio (if any) from viewBox is
		// preserved; intrinsic dimensions remain 0 so ComputeReplacedSize
		// applies the CSS 2.1 §10.3.2 default (300x150) subject to ratio.
	}

	return info
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
