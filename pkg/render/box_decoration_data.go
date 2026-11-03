package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout"
)

// computeBleedAvoidance mirrors BoxDecorationData::ComputeBleedAvoidance
// and BorderObscuresBackgroundEdge (core/paint/box_decoration_data.cc:11-70
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for a box that paints a
// background: a document element, or a box without a painted border,
// border radius, or with a border image, gets none (the background-image
// clip-layer case, :46-62, is not ported); a rounded border whose edges all
// obscure the background edge shrinks the background; any other rounded
// border clips it.
//
// louis14 implements only kBackgroundBleedShrinkBackground: the clipping
// strategies (kBackgroundBleedClipOnly / ClipLayer, which clip the whole
// decoration to the border rrect and paint the border with
// DrawBleedAdjustedDRRect) paint as kBackgroundBleedNone.
func computeBleedAvoidance(layer *PaintLayer, edges [4]BorderEdge) layout.BackgroundBleedAvoidance {
	box := layer.Box
	if box == nil || (box.LayoutNode != nil && box.LayoutNode.IsDocumentElement()) ||
		!hasBorderRadius(layer) || layer.BorderImageSource != nil {
		return layout.BackgroundBleedNone
	}
	paintsBorder := false
	for _, e := range edges {
		paintsBorder = paintsBorder || e.ShouldRender()
	}
	if !paintsBorder {
		return layout.BackgroundBleedNone
	}
	for _, e := range edges {
		if !e.ObscuresBackgroundEdge() {
			return layout.BackgroundBleedClipLayer
		}
	}
	return layout.BackgroundBleedShrinkBackground
}

// backgroundRoundedRectAdjustedForBleedAvoidance insets the background
// rrect by a "safe" amount: half the border width, a sixth for double
// borders (BackgroundContouredRectAdjustedForBleedAvoidance,
// core/paint/box_painter_base.cc:1165-1197 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func backgroundRoundedRectAdjustedForBleedAvoidance(edges [4]BorderEdge, rr FloatRoundedRect) FloatRoundedRect {
	fractionalInset := 1.0 / 2
	for _, e := range edges {
		if e.style == css.BorderStyleDouble {
			fractionalInset = 1.0 / 6
			break
		}
	}
	top := float64(edges[BoxSideTop].UsedWidth()) * fractionalInset
	right := float64(edges[BoxSideRight].UsedWidth()) * fractionalInset
	bottom := float64(edges[BoxSideBottom].UsedWidth()) * fractionalInset
	left := float64(edges[BoxSideLeft].UsedWidth()) * fractionalInset
	return FloatRoundedRect{
		Rect: geometry.NewRectF(rr.Rect.X()+left, rr.Rect.Y()+top,
			rr.Rect.Width()-left-right, rr.Rect.Height()-top-bottom),
		Radii: rr.Radii.Inset(top, right, bottom, left),
	}
}
