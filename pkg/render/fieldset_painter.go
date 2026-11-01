package render

import (
	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// fieldsetPaintInfo is the fieldset's border-box contraction and the legend
// cutout, both relative to the fieldset's border-box origin. Mirrors Blink's
// FieldsetPaintInfo (core/paint/fieldset_paint_info.{h,cc} @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type fieldsetPaintInfo struct {
	// borderOutsets contract the border box so the block-start border runs
	// through the center of a legend taller than it.
	borderOutsets css.BoxEdge
	// legendCutout is the rectangle the border is not painted behind;
	// zero-sized when there is no rendered legend.
	legendCutout layout.PhysicalRect
}

// newFieldsetPaintInfo mirrors FieldsetPaintInfo's constructor
// (fieldset_paint_info.cc:11-51 @ a9f50e522efa). legend is the legend's
// border box relative to the fieldset's border-box origin.
func newFieldsetPaintInfo(style *css.Style, width float64, borders css.BoxEdge, legend layout.PhysicalRect) fieldsetPaintInfo {
	var info fieldsetPaintInfo
	wdm := layout.NewWritingDirectionMode(style)
	if wdm.IsVertical() {
		legendSize := legend.Size.Width
		flipped := wdm.IsFlippedBlocks()
		borderSize := borders.Left
		if flipped {
			borderSize = borders.Right
		}
		if excess := legendSize - borderSize; excess > 0 {
			if flipped {
				info.borderOutsets.Right = excess / 2
			} else {
				info.borderOutsets.Left = excess / 2
			}
		}
		total := max(legendSize, borderSize)
		info.legendCutout = rectXYWH(0, legend.Offset.Y, total, legend.Size.Height)
		if flipped {
			info.legendCutout.Offset.X += width - total
		}
		return info
	}
	legendSize := legend.Size.Height
	if excess := legendSize - borders.Top; excess > 0 {
		info.borderOutsets.Top = excess / 2
	}
	info.legendCutout = rectXYWH(legend.Offset.X, 0, legend.Size.Width, max(legendSize, borders.Top))
	return info
}

// createFieldsetPaintInfo finds the rendered legend (always the fieldset's
// first child) and builds the paint info. Mirrors FieldsetPainter::
// CreateFieldsetPaintInfo (fieldset_painter.cc:25-71 @ a9f50e522efa). Not
// ported: unapplying the legend's relative offset (:39-67) — the cutout
// follows the legend's painted position.
func createFieldsetPaintInfo(box *layout.Box) fieldsetPaintInfo {
	var legend layout.PhysicalRect
	if len(box.Children) > 0 && box.Children[0].IsRenderedLegend {
		l := box.Children[0]
		legend = rectXYWH(l.X-box.X, l.Y-box.Y, l.Width, l.Height)
	}
	return newFieldsetPaintInfo(box.Style, box.Width, box.Border, legend)
}

// paintFieldsetBoxDecorationBackground paints a fieldset container's
// shadows, background and border, with the border box contracted by the
// border outsets and the border clipped out behind the legend. Mirrors
// FieldsetPainter::PaintBoxDecorationBackground (fieldset_painter.cc:75-151
// @ a9f50e522efa).
func (r *Renderer) paintFieldsetBoxDecorationBackground(layer *PaintLayer) {
	box := layer.Box
	info := createFieldsetPaintInfo(box)
	// The border box contracted by the border outsets, when the border is
	// pushed down by a tall legend (fieldset_painter.cc:83-84).
	o := info.borderOutsets
	rect := rectXYWH(box.X+o.Left, box.Y+o.Top, box.Width-o.Left-o.Right, box.Height-o.Top-o.Bottom)

	if len(layer.BoxShadows) > 0 {
		r.drawOutsetBoxShadows(layer, rect)
	}
	r.drawBackground(layer, rect)
	if len(layer.BoxShadows) > 0 {
		r.drawInsetBoxShadows(layer, rect)
	}

	// Clip out the legend cutout (GraphicsContext::ClipOut, :130-134): an
	// outer rectangle plus the cutout wound the other way leaves a hole
	// under the nonzero rule the clip rasterizer uses.
	cut := info.legendCutout
	if cut.Size.Width > 0 && cut.Size.Height > 0 {
		r.dc.Push()
		defer r.dc.Pop()
		r.dc.DrawRectangle(pixelSnap(box.X, box.Y, box.Width, box.Height))
		cx, cy, cw, ch := pixelSnap(box.X+cut.Offset.X, box.Y+cut.Offset.Y, cut.Size.Width, cut.Size.Height)
		r.buildRoundedRectPathReverse(cx, cy, cw, ch, css.EllipticalRadii{})
		r.dc.Clip()
	}
	r.drawBorders(layer, rect)
}
