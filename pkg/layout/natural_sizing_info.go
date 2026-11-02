package layout

import (
	"louis14/pkg/geometry"
)

// PhysicalNaturalSizingInfo is a replaced element's natural size and natural
// aspect ratio in LayoutUnits, with whether each natural dimension exists.
// Mirrors Blink's PhysicalNaturalSizingInfo (core/layout/
// natural_sizing_info.h:44-59 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// An empty AspectRatio means no natural ratio.
type PhysicalNaturalSizingInfo struct {
	Size        geometry.PhysicalSize
	AspectRatio geometry.PhysicalSize
	HasWidth    bool
	HasHeight   bool
}

// MakeFixedPhysicalNaturalSizingInfo returns sizing info whose natural size
// and ratio are both naturalSize. Mirrors PhysicalNaturalSizingInfo::MakeFixed
// (natural_sizing_info.h:48-50 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func MakeFixedPhysicalNaturalSizingInfo(naturalSize geometry.PhysicalSize) PhysicalNaturalSizingInfo {
	return PhysicalNaturalSizingInfo{Size: naturalSize, AspectRatio: naturalSize, HasWidth: true, HasHeight: true}
}

// ResolveWidthForRatio / ResolveHeightForRatio mirror the LayoutUnit
// overloads in natural_sizing_info.h:65-77 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func ResolveWidthForRatio(height geometry.LayoutUnit, naturalRatio geometry.PhysicalSize) geometry.LayoutUnit {
	return height.MulDiv(naturalRatio.Width, naturalRatio.Height)
}

func ResolveHeightForRatio(width geometry.LayoutUnit, naturalRatio geometry.PhysicalSize) geometry.LayoutUnit {
	return width.MulDiv(naturalRatio.Height, naturalRatio.Width)
}

// ConcreteObjectSize resolves the CSS Images 3 default sizing algorithm
// (https://www.w3.org/TR/css3-images/#default-sizing) with no specified size
// and a contain constraint of defaultObjectSize. Mirrors ConcreteObjectSize
// (PhysicalNaturalSizingInfo overload) → ConcreteObjectSizeImpl
// (core/layout/natural_sizing_info.cc:54-101, :116-118 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ConcreteObjectSize(info PhysicalNaturalSizingInfo, defaultObjectSize geometry.PhysicalSize) geometry.PhysicalSize {
	if info.HasWidth && info.HasHeight {
		return info.Size
	}
	if info.HasWidth {
		if info.AspectRatio.IsEmpty() {
			return geometry.PhysicalSize{Width: info.Size.Width, Height: defaultObjectSize.Height}
		}
		return geometry.PhysicalSize{Width: info.Size.Width, Height: ResolveHeightForRatio(info.Size.Width, info.AspectRatio)}
	}
	if info.HasHeight {
		if info.AspectRatio.IsEmpty() {
			return geometry.PhysicalSize{Width: defaultObjectSize.Width, Height: info.Size.Height}
		}
		return geometry.PhysicalSize{Width: ResolveWidthForRatio(info.Size.Height, info.AspectRatio), Height: info.Size.Height}
	}
	if !info.AspectRatio.IsEmpty() {
		solutionWidth := ResolveWidthForRatio(defaultObjectSize.Height, info.AspectRatio)
		if solutionWidth.LessEqual(defaultObjectSize.Width) {
			return geometry.PhysicalSize{Width: solutionWidth, Height: defaultObjectSize.Height}
		}
		return geometry.PhysicalSize{Width: defaultObjectSize.Width, Height: ResolveHeightForRatio(defaultObjectSize.Width, info.AspectRatio)}
	}
	return defaultObjectSize
}
