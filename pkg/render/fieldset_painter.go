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
	legendCutout fieldsetRect
}

// fieldsetRect is a rectangle relative to the fieldset's border-box origin.
type fieldsetRect struct{ x, y, w, h float64 }

// newFieldsetPaintInfo mirrors FieldsetPaintInfo's constructor
// (fieldset_paint_info.cc:11-51 @ a9f50e522efa). legend is the legend's
// border box relative to the fieldset's border-box origin.
func newFieldsetPaintInfo(style *css.Style, width float64, borders css.BoxEdge, legend fieldsetRect) fieldsetPaintInfo {
	var info fieldsetPaintInfo
	wdm := layout.NewWritingDirectionMode(style)
	if wdm.IsVertical() {
		legendSize := legend.w
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
		info.legendCutout = fieldsetRect{0, legend.y, total, legend.h}
		if flipped {
			info.legendCutout.x += width - total
		}
		return info
	}
	legendSize := legend.h
	if excess := legendSize - borders.Top; excess > 0 {
		info.borderOutsets.Top = excess / 2
	}
	info.legendCutout = fieldsetRect{legend.x, 0, legend.w, max(legendSize, borders.Top)}
	return info
}

// createFieldsetPaintInfo finds the rendered legend (always the fieldset's
// first child) and builds the paint info. Mirrors FieldsetPainter::
// CreateFieldsetPaintInfo (fieldset_painter.cc:25-71 @ a9f50e522efa). Not
// ported: unapplying the legend's relative offset (:39-67) — the cutout
// follows the legend's painted position.
func createFieldsetPaintInfo(box *layout.Box) fieldsetPaintInfo {
	var legend fieldsetRect
	if len(box.Children) > 0 && box.Children[0].IsRenderedLegend {
		l := box.Children[0]
		legend = fieldsetRect{l.X - box.X, l.Y - box.Y, l.Width, l.Height}
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
	// The draw helpers read the border box off layer.Box; paint them on a
	// contracted copy when the border is pushed down by a tall legend.
	if o := info.borderOutsets; o != (css.BoxEdge{}) {
		contracted := *box
		contracted.X += o.Left
		contracted.Y += o.Top
		contracted.Width -= o.Left + o.Right
		contracted.Height -= o.Top + o.Bottom
		layer.Box = &contracted
		defer func() { layer.Box = box }()
	}

	if len(layer.BoxShadows) > 0 {
		r.drawOutsetBoxShadows(layer)
	}
	r.drawBackground(layer)
	if len(layer.BoxShadows) > 0 {
		r.drawInsetBoxShadows(layer)
	}

	// Clip out the legend cutout (GraphicsContext::ClipOut, :130-134): an
	// outer rectangle plus the cutout wound the other way leaves a hole
	// under the nonzero rule the clip rasterizer uses.
	cut := info.legendCutout
	if cut.w > 0 && cut.h > 0 {
		r.dc.Push()
		defer r.dc.Pop()
		r.dc.DrawRectangle(pixelSnap(box.X, box.Y, box.Width, box.Height))
		cx, cy, cw, ch := pixelSnap(box.X+cut.x, box.Y+cut.y, cut.w, cut.h)
		r.buildRoundedRectPathReverse(cx, cy, cw, ch, css.EllipticalRadii{})
		r.dc.Clip()
	}
	r.drawBorders(layer)
}
