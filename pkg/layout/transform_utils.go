package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// computeReferenceBoxInternal returns the transform reference box of a box
// with border box borderBox: the border box, or for content-box the box
// inside borders and padding, its size clamped to zero. Mirrors
// ComputeReferenceBoxInternal (core/layout/transform_utils.cc:17-34 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func computeReferenceBoxInternal(style *css.Style, borderBox geometry.RectF, borders, padding PhysicalEdges) geometry.RectF {
	if style == nil || style.UsedTransformBox(css.TransformBoxContextLayoutBox) != css.TransformBoxContentBox {
		return borderBox
	}
	left, top := borders.Left+padding.Left, borders.Top+padding.Top
	return geometry.NewRectF(borderBox.X()+left, borderBox.Y()+top,
		max(0, borderBox.Width()-left-borders.Right-padding.Right),
		max(0, borderBox.Height()-top-borders.Bottom-padding.Bottom))
}

// ComputeReferenceBox returns fragment's transform reference box in its
// local coordinates. Mirrors ComputeReferenceBox(const PhysicalBoxFragment&)
// (core/layout/transform_utils.cc:38-40 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeReferenceBox(fragment *PhysicalFragment) geometry.RectF {
	var borders, padding PhysicalEdges
	if fragment.BoxData != nil {
		borders, padding = fragment.BoxData.Border, fragment.BoxData.Padding
	}
	return computeReferenceBoxInternal(fragment.Style,
		geometry.NewRectF(0, 0, fragment.Size.WidthF64(), fragment.Size.HeightF64()), borders, padding)
}

// ComputeReferenceBoxForBox returns box's transform reference box relative
// to its border-box origin, for the paint tree's Box. Mirrors
// ComputeReferenceBox(const LayoutBox&) (core/layout/transform_utils.cc:42-48
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeReferenceBoxForBox(box *Box) geometry.RectF {
	return computeReferenceBoxInternal(box.Style, geometry.NewRectF(0, 0, box.Width, box.Height),
		physicalEdges(box.Border), physicalEdges(box.Padding))
}

func physicalEdges(e css.BoxEdge) PhysicalEdges {
	return PhysicalEdges{Top: e.Top, Right: e.Right, Bottom: e.Bottom, Left: e.Left}
}
