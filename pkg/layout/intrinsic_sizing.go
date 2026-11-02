package layout

import (
	"strconv"
	"strings"

	"louis14/pkg/geometry"
	"louis14/pkg/graphics"
	"louis14/pkg/images"
	urlpkg "louis14/pkg/url"
	stdnet "louis14/std/net"
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

// GetIntrinsicSizingInfo returns the natural dimensions a replaced element
// is laid out with: its natural dimensions with any object-view-box applied
// (LayoutReplaced::ComputeNaturalSizingInfo, layout_replaced.cc:505-516 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An inline <svg> ignores
// object-view-box (LayoutSVGRoot::ShouldApplyObjectViewBox,
// layout_svg_root.h:166-169).
func GetIntrinsicSizingInfo(ctx *LayoutContext, node *LayoutInputNode) IntrinsicSizingInfo {
	info := getNaturalDimensions(ctx, node)
	if node.DOMNode == nil || node.DOMNode.TagName == "svg" {
		return info
	}
	s := node.Style()
	if s == nil {
		return info
	}
	viewBox := s.ObjectViewBox()
	if viewBox == nil {
		return info
	}
	applied := ComputeNaturalSizingInfo(viewBox, physicalNaturalSizingInfo(info))
	info.IntrinsicWidth, info.IntrinsicHeight = applied.Size.WidthF64(), applied.Size.HeightF64()
	if info.HasAspectRatio && !applied.Size.IsEmpty() {
		info.AspectRatio = info.IntrinsicWidth / info.IntrinsicHeight
	}
	return info
}

// physicalNaturalSizingInfo converts louis14's float IntrinsicSizingInfo
// (an unknown axis is 0) to PhysicalNaturalSizingInfo, rounding as
// PhysicalNaturalSizingInfo::FromSizingInfo does (natural_sizing_info.cc:
// 104-110 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func physicalNaturalSizingInfo(info IntrinsicSizingInfo) PhysicalNaturalSizingInfo {
	size := geometry.PhysicalSizeFromF64Round(info.IntrinsicWidth, info.IntrinsicHeight)
	out := PhysicalNaturalSizingInfo{Size: size, HasWidth: info.IntrinsicWidth > 0, HasHeight: info.IntrinsicHeight > 0}
	if info.HasAspectRatio {
		out.AspectRatio = size
	}
	return out
}

// getNaturalDimensions returns a replaced element's natural dimensions
// (LayoutReplaced::GetNaturalDimensions and its overrides).
func getNaturalDimensions(ctx *LayoutContext, node *LayoutInputNode) IntrinsicSizingInfo {
	if node.DOMNode == nil {
		return IntrinsicSizingInfo{}
	}
	// A LayoutImage (an <img>, or an image-typed <object>/<embed>) takes its
	// natural size from the image (LayoutImage::GetNaturalDimensions). Every
	// <img> is routed here, not only html.CreatesLayoutImage ones: a fallback
	// <img> gets the broken-image size from getImgIntrinsicInfo.
	if node.DOMNode.TagName == "img" || node.DOMNode.IsImageType() {
		return getImgIntrinsicInfo(ctx, node)
	}
	switch node.DOMNode.TagName {
	case "svg":
		return getInlineSVGIntrinsicInfo(node)
	case "canvas":
		return getCanvasIntrinsicInfo(node)
	case "video":
		return getVideoNaturalDimensions(ctx, node)
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
//   - Explicit width + height → intrinsic dimensions (aspect ratio derived).
//   - viewBox only → aspect ratio from viewBox; no intrinsic dimensions
//     (ComputeReplacedSize applies the CSS default 300x150 subject to ratio).
//   - One explicit dimension + viewBox → derive the other via aspect ratio.
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
	case hasExplW:
		// Width only, no viewBox: intrinsic width is set, intrinsic height
		// remains unknown so the CSS 2.1 §10.3.2 default (150) supplies it.
		info.IntrinsicWidth = explW
	case hasExplH:
		// Height only, no viewBox: intrinsic height is set, intrinsic width
		// falls back to the CSS 2.1 default (300) downstream.
		info.IntrinsicHeight = explH
	default:
		// No explicit dimensions. Aspect ratio (if any) from viewBox is
		// preserved; intrinsic dimensions remain 0 so ComputeReplacedSize
		// applies the CSS 2.1 §10.3.2 default (300x150) subject to ratio.
	}

	return info
}

// brokenImageWidth/Height are the natural size of Blink's broken-image icon,
// the IDR_BROKENIMAGE resource (blink/broken_image.png at 1x, 14x16 px,
// loaded by LayoutImageResource::BrokenImage, layout_image_resource.cc:
// 156-169 @ d4ecdfed88f962439247c2ad36b8fe47805b1520). louis14 has no copy of
// the bitmap, so the icon paints nothing; only its size takes part in layout.
const (
	brokenImageWidth  = 14
	brokenImageHeight = 16
)

// getImgIntrinsicInfo returns an <img>'s natural size. Its src is completed
// against the owning document's base (Style.BaseDir) before it is fetched,
// as Blink's ImageLoader::ImageSourceToKURL does with
// resolver_document.CompleteURL(src)
// (third_party/blink/renderer/core/loader/image_loader.cc:746 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func getImgIntrinsicInfo(ctx *LayoutContext, node *LayoutInputNode) IntrinsicSizingInfo {
	if node.DOMNode.IsFallbackImage {
		return IntrinsicSizingInfo{
			IntrinsicWidth:  brokenImageWidth,
			IntrinsicHeight: brokenImageHeight,
			AspectRatio:     float64(brokenImageWidth) / brokenImageHeight,
			HasAspectRatio:  true,
		}
	}
	if ctx == nil {
		return IntrinsicSizingInfo{}
	}
	src := node.DOMNode.ImageSourceURL()
	if src == "" {
		return IntrinsicSizingInfo{}
	}
	if s := node.Style(); s != nil {
		src = urlpkg.CompleteURL(src, s.BaseDir)
	}
	// data: URIs decode inline and need no fetcher.
	if ctx.ImageFetcher == nil && !stdnet.IsDataURL(src) {
		return IntrinsicSizingInfo{}
	}

	// An SVG image's natural dimensions are SVGImage::GetNaturalDimensions
	// (LayoutImageResource::GetNaturalDimensions, layout_image_resource.cc:
	// 153-158 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The resource is
	// recognised by content, not by URL extension.
	if svg := images.LoadSVGImageWithFetcher(src, ctx.ImageFetcher); svg != nil {
		return svgIntrinsicInfo(svg.Sizing)
	}

	natW, natH, err := images.GetImageDimensionsWithFetcher(src, ctx.ImageFetcher)
	if err != nil || natW <= 0 || natH <= 0 {
		// No natural size. A broken <img> normally never gets here: the
		// layout disposition pass gives it fallback content, which is not
		// replaced (html.IsReplacedElement). Blink keeps a broken image's
		// width/height only through TreatImageAsReplaced + ShowAsReplaced on
		// the alt-text container (html_image_fallback_helper.cc:50-62,
		// :103-116 @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
		return IntrinsicSizingInfo{}
	}
	// A bitmap's natural size is its oriented size when the style respects
	// the image orientation (LayoutImageResource::GetNaturalDimensions,
	// image.Size(ImageOrientation()), core/layout/layout_image_resource.cc:
	// 145-170; BitmapImage::SizeWithConfig transposes for orientations 5-8,
	// platform/graphics/bitmap_image.cc:193-201, both @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if s := node.Style(); (s == nil || s.ImageOrientation() == graphics.RespectImageOrientation) &&
		images.ImageOrientationWithFetcher(src, ctx.ImageFetcher).UsesWidthAsHeight() {
		natW, natH = natH, natW
	}
	return IntrinsicSizingInfo{
		IntrinsicWidth:  float64(natW),
		IntrinsicHeight: float64(natH),
		AspectRatio:     float64(natW) / float64(natH),
		HasAspectRatio:  true,
	}
}

// svgIntrinsicInfo converts an SVG image's natural dimensions
// (images.SVGSizingInfo.NaturalSize / NaturalRatio, SVGImage::
// GetNaturalDimensions) to IntrinsicSizingInfo, where an absent natural
// width or height is 0.
func svgIntrinsicInfo(svg images.SVGSizingInfo) IntrinsicSizingInfo {
	var info IntrinsicSizingInfo
	w, h, hasW, hasH := svg.NaturalSize()
	if hasW {
		info.IntrinsicWidth = w
	}
	if hasH {
		info.IntrinsicHeight = h
	}
	if rw, rh := svg.NaturalRatio(); rw > 0 && rh > 0 {
		info.AspectRatio, info.HasAspectRatio = rw/rh, true
	}
	return info
}
