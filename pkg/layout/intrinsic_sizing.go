package layout

import (
	"strconv"
	"strings"

	"louis14/pkg/geometry"
	"louis14/pkg/graphics"
	"louis14/pkg/images"
	urlpkg "louis14/pkg/url"
)

// GetNaturalSizingInfo returns the natural dimensions a replaced element
// is laid out with: its natural dimensions with any object-view-box applied
// (LayoutReplaced::ComputeNaturalSizingInfo, layout_replaced.cc:505-516 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An inline <svg> ignores
// object-view-box (LayoutSVGRoot::ShouldApplyObjectViewBox,
// layout_svg_root.h:166-169).
func GetNaturalSizingInfo(ctx *LayoutContext, node *LayoutInputNode) NaturalSizingInfo {
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
	applied := ComputeNaturalSizingInfo(viewBox, PhysicalNaturalSizingInfoFromSizingInfo(info))
	size := geometry.SizeF{Width: applied.Size.WidthF64(), Height: applied.Size.HeightF64()}
	var ratio geometry.SizeF
	if !info.AspectRatio.IsEmpty() && !applied.Size.IsEmpty() {
		ratio = size
	}
	return naturalSizingInfo(size.Width, size.Height, ratio)
}

// naturalSizingInfo returns sizing info of natural size w×h — each axis
// present when positive — and natural ratio ratio (empty for none). Used by
// the producers that still spell an absent axis as 0 (SVG sizing and the
// object-view-box result).
func naturalSizingInfo(w, h float64, ratio geometry.SizeF) NaturalSizingInfo {
	return NaturalSizingInfo{Size: geometry.SizeF{Width: w, Height: h}, AspectRatio: ratio, HasWidth: w > 0, HasHeight: h > 0}
}

// getNaturalDimensions returns a replaced element's natural dimensions
// (LayoutReplaced::GetNaturalDimensions and its overrides).
func getNaturalDimensions(ctx *LayoutContext, node *LayoutInputNode) NaturalSizingInfo {
	if node.DOMNode == nil {
		return NaturalSizingInfoNone()
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
		return MakeSizeNaturalSizingInfo(geometry.SizeF{Width: 300, Height: 150})
	case "textarea":
		// Form controls have intrinsic dimensions but no aspect ratio.
		// Textarea default: ~173×54 border-box per UA stylesheet; content-box
		// is 167×48 (minus 2×1px border + 2×2px padding).
		return MakeSizeNaturalSizingInfo(geometry.SizeF{Width: 167, Height: 48})
	case "input":
		inputType, _ := node.DOMNode.GetAttribute("type")
		if inputType == "checkbox" || inputType == "radio" {
			return MakeFixedNaturalSizingInfo(geometry.SizeF{Width: 13, Height: 13})
		}
		// Text input default: ~173×19 border-box per UA stylesheet; content-box
		// is 165×11 (minus 2×2px border + 2×(1+2)px padding).
		return MakeSizeNaturalSizingInfo(geometry.SizeF{Width: 165, Height: 11})
	case "select":
		// Select default: ~173×19 border-box; content-box is 167×13.
		return MakeSizeNaturalSizingInfo(geometry.SizeF{Width: 167, Height: 13})
	default:
		return MakeSizeNaturalSizingInfo(geometry.SizeF{Width: 300, Height: 150})
	}
}

// getCanvasIntrinsicInfo returns the intrinsic dimensions for a canvas element.
// Per HTML spec, the intrinsic size comes from the width/height HTML attributes,
// defaulting to 300x150 if not specified.
func getCanvasIntrinsicInfo(node *LayoutInputNode) NaturalSizingInfo {
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
	return MakeFixedNaturalSizingInfo(geometry.SizeF{Width: w, Height: h})
}

// getInlineSVGIntrinsicInfo returns intrinsic sizing info for an inline <svg>
// element from its width/height/viewBox attributes. Unlike an SVG image
// (svgIntrinsicInfo → SVGImage::GetNaturalDimensions), one explicit
// dimension plus a viewBox derives the other natural dimension from the
// ratio — louis14's pre-existing inline behaviour, not yet Blink's
// LayoutSVGRoot::UnscaledNaturalSizingInfo (layout_svg_root.cc:74-103 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func getInlineSVGIntrinsicInfo(node *LayoutInputNode) NaturalSizingInfo {
	if node == nil || node.DOMNode == nil {
		return NaturalSizingInfoNone()
	}
	var svg images.SVGSizingInfo
	if val, ok := node.DOMNode.GetAttribute("width"); ok {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			svg.ExplicitWidth, svg.HasExplicitWidth = parsed, true
		}
	}
	if val, ok := node.DOMNode.GetAttribute("height"); ok {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			svg.ExplicitHeight, svg.HasExplicitHeight = parsed, true
		}
	}

	// Parse viewBox "min-x min-y width height" for aspect ratio.
	// Our HTML tokenizer lowercases attribute names, so SVG's camelCase
	// viewBox is stored as "viewbox". Try both to be robust.
	vb, ok := node.DOMNode.GetAttribute("viewBox")
	if !ok {
		vb, ok = node.DOMNode.GetAttribute("viewbox")
	}
	if ok {
		parts := strings.Fields(vb)
		if len(parts) == 4 {
			if w, err := strconv.ParseFloat(parts[2], 64); err == nil && w > 0 {
				if h, err2 := strconv.ParseFloat(parts[3], 64); err2 == nil && h > 0 {
					svg.ViewBoxWidth, svg.ViewBoxHeight, svg.HasViewBox = w, h, true
				}
			}
		}
	}

	var w, h float64
	var ratio geometry.SizeF
	if svg.HasViewBox {
		ratio = geometry.SizeF{Width: svg.ViewBoxWidth, Height: svg.ViewBoxHeight}
	}
	switch {
	case svg.HasExplicitWidth && svg.HasExplicitHeight:
		w, h = svg.ExplicitWidth, svg.ExplicitHeight
		if ratio.IsEmpty() {
			ratio = geometry.SizeF{Width: w, Height: h}
		}
	case svg.HasExplicitWidth && svg.HasViewBox:
		w = svg.ExplicitWidth
		h = svg.ExplicitWidth / (ratio.Width / ratio.Height)
	case svg.HasExplicitHeight && svg.HasViewBox:
		h = svg.ExplicitHeight
		w = svg.ExplicitHeight * (ratio.Width / ratio.Height)
	case svg.HasExplicitWidth:
		// Width only, no viewBox: the natural height stays absent so the
		// CSS 2.1 §10.3.2 default (150) supplies it.
		w = svg.ExplicitWidth
	case svg.HasExplicitHeight:
		// Height only, no viewBox: the CSS 2.1 default width (300) applies.
		h = svg.ExplicitHeight
	}
	// No explicit dimensions: only the viewBox ratio; ComputeReplacedSize
	// uses the 300×150 default subject to it.
	return naturalSizingInfo(w, h, ratio)
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
func getImgIntrinsicInfo(ctx *LayoutContext, node *LayoutInputNode) NaturalSizingInfo {
	if node.DOMNode.IsFallbackImage {
		return MakeFixedNaturalSizingInfo(geometry.SizeF{Width: brokenImageWidth, Height: brokenImageHeight})
	}
	if ctx == nil {
		return NaturalSizingInfoNone()
	}
	src := node.DOMNode.ImageSourceURL()
	if src == "" {
		return NaturalSizingInfoNone()
	}
	if s := node.Style(); s != nil {
		src = urlpkg.CompleteURL(src, s.BaseDir)
	}
	if !images.ImageLoadIsAttempted(src, ctx.ImageFetcher) {
		return NaturalSizingInfoNone()
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
		return NaturalSizingInfoNone()
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
	return MakeFixedNaturalSizingInfo(geometry.SizeF{Width: float64(natW), Height: float64(natH)})
}

// svgIntrinsicInfo converts an SVG image's natural dimensions
// (images.SVGSizingInfo.NaturalSize / NaturalRatio, SVGImage::
// GetNaturalDimensions) to NaturalSizingInfo; an absent natural width or
// height is 0.
func svgIntrinsicInfo(svg images.SVGSizingInfo) NaturalSizingInfo {
	var w, h float64
	nw, nh, hasW, hasH := svg.NaturalSize()
	if hasW {
		w = nw
	}
	if hasH {
		h = nh
	}
	var ratio geometry.SizeF
	if rw, rh := svg.NaturalRatio(); rw > 0 && rh > 0 {
		ratio = geometry.SizeF{Width: rw, Height: rh}
	}
	return naturalSizingInfo(w, h, ratio)
}
