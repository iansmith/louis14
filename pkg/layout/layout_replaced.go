package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// ComputeObjectViewBoxRect returns the object-view-box rect in the natural
// image space, and whether one applies: a view box (objectViewBox non-nil),
// natural width and height, a non-empty natural size, a non-empty view box
// that differs from the natural rect. Mirrors
// LayoutReplaced::ComputeObjectViewBoxRect (core/layout/layout_replaced.cc:
// 317-354 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the caller applies
// ShouldApplyObjectViewBox (false for an inline <svg>, LayoutSVGRoot,
// layout_svg_root.h:166-169).
func ComputeObjectViewBoxRect(objectViewBox *css.BasicShapeInset, sizingInfo PhysicalNaturalSizingInfo) (geometry.PhysicalRect, bool) {
	if objectViewBox == nil || !sizingInfo.HasWidth || !sizingInfo.HasHeight || sizingInfo.Size.IsEmpty() {
		return geometry.PhysicalRect{}, false
	}
	boundingBox := geometry.NewRectF(0, 0, sizingInfo.Size.Width.Float64(), sizingInfo.Size.Height.Float64())
	viewBoxRect := geometry.PhysicalRectEnclosingRect(objectViewBox.PathBoundingRect(boundingBox))
	if viewBoxRect.IsEmpty() {
		return geometry.PhysicalRect{}, false
	}
	if viewBoxRect == (geometry.PhysicalRect{Size: sizingInfo.Size}) {
		return geometry.PhysicalRect{}, false
	}
	return viewBoxRect, true
}

// ComputeReplacedContentRect returns the rect the replaced content paints
// into. Without a view box it is ComputeObjectFitAndPositionRect over the
// natural size; with one, object-fit/object-position place the view box and
// the whole image is scaled and offset to match, so regions of the image
// outside the view box become ink overflow. Mirrors
// LayoutReplaced::ComputeReplacedContentRect (layout_replaced.cc:356-420 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeReplacedContentRect(objectFit css.ObjectFit, objectPosition css.LengthPoint, objectViewBox *css.BasicShapeInset, baseContentRect geometry.PhysicalRect, sizingInfo PhysicalNaturalSizingInfo) geometry.PhysicalRect {
	viewBox, ok := ComputeObjectViewBoxRect(objectViewBox, sizingInfo)
	if !ok {
		return ComputeObjectFitAndPositionRect(objectFit, objectPosition, baseContentRect, sizingInfo)
	}
	viewBoxPaintRect := ComputeObjectFitAndPositionRect(objectFit, objectPosition, baseContentRect,
		MakeFixedPhysicalNaturalSizingInfo(viewBox.Size))
	if viewBoxPaintRect.IsEmpty() {
		return viewBoxPaintRect
	}
	naturalSize := sizingInfo.Size
	scaledImageSize := geometry.PhysicalSize{
		Width:  naturalSize.Width.MulDiv(viewBoxPaintRect.Size.Width, viewBox.Size.Width),
		Height: naturalSize.Height.MulDiv(viewBoxPaintRect.Size.Height, viewBox.Size.Height),
	}
	scaledOffset := geometry.PhysicalOffset{
		Left: viewBox.Offset.Left.MulDiv(viewBoxPaintRect.Size.Width, viewBox.Size.Width),
		Top:  viewBox.Offset.Top.MulDiv(viewBoxPaintRect.Size.Height, viewBox.Size.Height),
	}
	return geometry.PhysicalRect{Offset: viewBoxPaintRect.Offset.Sub(scaledOffset), Size: scaledImageSize}
}

// ComputeNaturalSizingInfo applies an object-view-box to natural sizing
// info: the view box's size becomes the natural size (and, when there was
// a natural ratio, the ratio). Mirrors LayoutReplaced::
// ComputeNaturalSizingInfo (layout_replaced.cc:505-516 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeNaturalSizingInfo(objectViewBox *css.BasicShapeInset, sizingInfo PhysicalNaturalSizingInfo) PhysicalNaturalSizingInfo {
	if viewBox, ok := ComputeObjectViewBoxRect(objectViewBox, sizingInfo); ok {
		sizingInfo.Size = viewBox.Size
		if !sizingInfo.AspectRatio.IsEmpty() {
			sizingInfo.AspectRatio = sizingInfo.Size
		}
	}
	return sizingInfo
}

// ComputeObjectFitAndPositionRect returns the rect the replaced content
// paints into: the object size chosen by object-fit, placed in
// baseContentRect by object-position. Mirrors
// LayoutReplaced::ComputeObjectFitAndPositionRect (core/layout/
// layout_replaced.cc:422-484 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Blink reads object-fit and object-position from StyleRef(); louis14's paint
// layers carry those computed values, so the caller passes them.
//
// The scale-down branch's ImageDevicePixelRatio rescale (srcset density) is a
// no-op here: louis14 has no srcset, so every image's device pixel ratio is 1.
func ComputeObjectFitAndPositionRect(objectFit css.ObjectFit, objectPosition css.LengthPoint, baseContentRect geometry.PhysicalRect, sizingInfo PhysicalNaturalSizingInfo) geometry.PhysicalRect {
	if objectFit == css.ObjectFitFill && objectPosition == css.InitialObjectPosition {
		return baseContentRect
	}

	intrinsicSize := sizingInfo.Size
	aspectRatio := sizingInfo.AspectRatio
	if intrinsicSize.IsEmpty() && aspectRatio.IsEmpty() {
		return baseContentRect
	}

	scaledIntrinsicSize := intrinsicSize
	objectSize := baseContentRect.Size
	switch objectFit {
	case css.ObjectFitContain, css.ObjectFitCover, css.ObjectFitScaleDown:
		if !aspectRatio.IsEmpty() {
			fit := geometry.AspectRatioFitShrink
			if objectFit == css.ObjectFitCover {
				fit = geometry.AspectRatioFitGrow
			}
			objectSize = objectSize.FitToAspectRatio(aspectRatio, fit)
		}
		if objectFit != css.ObjectFitScaleDown || objectSize.Width.LessEqual(scaledIntrinsicSize.Width) {
			break
		}
		fallthrough
	case css.ObjectFitNone:
		if intrinsicSize.IsEmpty() {
			objectSize = ConcreteObjectSize(sizingInfo, baseContentRect.Size)
		} else {
			objectSize = scaledIntrinsicSize
		}
	case css.ObjectFitFill:
	}

	objectPositionOffset := geometry.PhysicalOffset{
		Left: objectPosition.X.MinimumValueForLength(baseContentRect.Size.Width.Sub(objectSize.Width)),
		Top:  objectPosition.Y.MinimumValueForLength(baseContentRect.Size.Height.Sub(objectSize.Height)),
	}
	return geometry.PhysicalRect{Offset: baseContentRect.Offset.Add(objectPositionOffset), Size: objectSize}
}

// ClipsToContentBox reports whether a replaced element that respects CSS
// overflow clips its content to its content box: it clips overflow on both
// axes (GetOverflowClipAxes() == kOverflowClipBothAxis) with an
// overflow-clip-margin of content-box and no margin. Mirrors
// LayoutReplaced::ClipsToContentBox (core/layout/layout_replaced.cc:621-640
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for its RespectsCSSOverflow()
// branch. The other branch is not reachable from louis14: every element that
// paints through ImagePainter (an <img>; Blink's video, canvas and frame
// owners likewise) respects CSS overflow
// (Element::IsReplacedElementRespectingCSSOverflow, core/dom/
// element.cc:13749-13763 @ the same SHA), and an inline <svg> paints through
// paintSVGRoot. An unset or invalid overflow-clip-margin reads as
// padding-box (css.Style.GetOverflowClipMargin), which does not match.
func ClipsToContentBox(clipsBothAxes bool, clipMargin css.OverflowClipMargin) bool {
	return clipsBothAxes && clipMargin.Box == css.OverflowClipMarginContentBox && clipMargin.Length == 0
}
