package layout

import (
	"louis14/pkg/geometry"
)

// NaturalSizingInfo is an image's or replaced element's natural size and
// natural aspect ratio in float CSS pixels, with whether each natural
// dimension exists. Mirrors Blink's NaturalSizingInfo (core/layout/
// natural_sizing_info.h:15-42 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// An empty AspectRatio means no natural ratio.
type NaturalSizingInfo struct {
	Size        geometry.SizeF
	AspectRatio geometry.SizeF
	HasWidth    bool
	HasHeight   bool
}

// NaturalSizingInfoNone mirrors NaturalSizingInfo::None: no natural size or
// ratio.
func NaturalSizingInfoNone() NaturalSizingInfo { return NaturalSizingInfo{} }

// MakeFixedNaturalSizingInfo mirrors NaturalSizingInfo::MakeFixed: the
// natural size, with the same ratio.
func MakeFixedNaturalSizingInfo(naturalSize geometry.SizeF) NaturalSizingInfo {
	return NaturalSizingInfo{Size: naturalSize, AspectRatio: naturalSize, HasWidth: true, HasHeight: true}
}

// MakeSizeNaturalSizingInfo mirrors NaturalSizingInfo::MakeSize
// (natural_sizing_info.h:26-28 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the natural size, without a natural ratio.
func MakeSizeNaturalSizingInfo(naturalSize geometry.SizeF) NaturalSizingInfo {
	return NaturalSizingInfo{Size: naturalSize, HasWidth: true, HasHeight: true}
}

// ratio returns the natural aspect ratio as width / height (louis14 helper
// for the consumers that compare scalar ratios).
func (n NaturalSizingInfo) ratio() float64 { return n.AspectRatio.Width / n.AspectRatio.Height }

// ResolveWidthForRatioF / ResolveHeightForRatioF mirror the float overloads
// in natural_sizing_info.h:61-64, :70-73 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func ResolveWidthForRatioF(height float64, naturalRatio geometry.SizeF) float64 {
	return height * naturalRatio.Width / naturalRatio.Height
}

func ResolveHeightForRatioF(width float64, naturalRatio geometry.SizeF) float64 {
	return width * naturalRatio.Height / naturalRatio.Width
}

// PhysicalNaturalSizingInfoFromSizingInfo rounds n to LayoutUnits. Mirrors
// PhysicalNaturalSizingInfo::FromSizingInfo (natural_sizing_info.cc:
// 104-109 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func PhysicalNaturalSizingInfoFromSizingInfo(n NaturalSizingInfo) PhysicalNaturalSizingInfo {
	size := geometry.PhysicalSizeFromF64Round(n.Size.Width, n.Size.Height)
	out := PhysicalNaturalSizingInfo{Size: size, HasWidth: n.HasWidth, HasHeight: n.HasHeight}
	if !n.AspectRatio.IsEmpty() {
		out.AspectRatio = geometry.PhysicalSizeFromF64Round(n.AspectRatio.Width, n.AspectRatio.Height)
	}
	return out
}

// ConcreteObjectSizeF is ConcreteObjectSize's float overload (core/layout/
// natural_sizing_info.cc:54-101, :111-114 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the CSS Images 3 default
// sizing algorithm with a contain constraint of defaultObjectSize.
func ConcreteObjectSizeF(info NaturalSizingInfo, defaultObjectSize geometry.SizeF) geometry.SizeF {
	switch {
	case info.HasWidth && info.HasHeight:
		return info.Size
	case info.HasWidth:
		if info.AspectRatio.IsEmpty() {
			return geometry.SizeF{Width: info.Size.Width, Height: defaultObjectSize.Height}
		}
		return geometry.SizeF{Width: info.Size.Width, Height: ResolveHeightForRatioF(info.Size.Width, info.AspectRatio)}
	case info.HasHeight:
		if info.AspectRatio.IsEmpty() {
			return geometry.SizeF{Width: defaultObjectSize.Width, Height: info.Size.Height}
		}
		return geometry.SizeF{Width: ResolveWidthForRatioF(info.Size.Height, info.AspectRatio), Height: info.Size.Height}
	case !info.AspectRatio.IsEmpty():
		if solutionWidth := ResolveWidthForRatioF(defaultObjectSize.Height, info.AspectRatio); solutionWidth <= defaultObjectSize.Width {
			return geometry.SizeF{Width: solutionWidth, Height: defaultObjectSize.Height}
		}
		return geometry.SizeF{Width: defaultObjectSize.Width, Height: ResolveHeightForRatioF(defaultObjectSize.Width, info.AspectRatio)}
	}
	return defaultObjectSize
}

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
