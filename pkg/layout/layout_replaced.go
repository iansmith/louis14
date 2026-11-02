package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

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
