package render

import (
	"image"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
)

// NinePiece names one of the nine border-image pieces. Order mirrors Blink's
// NinePiece enum (core/paint/nine_piece_image_grid.h:22-34 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type NinePiece int

const (
	TopLeftPiece NinePiece = iota
	BottomLeftPiece
	LeftPiece
	TopRightPiece
	BottomRightPiece
	RightPiece
	TopPiece
	BottomPiece
	MiddlePiece
	maxPiece
)

// NinePieceTileRule is the per-axis tile rule of a draw info
// (NinePieceDrawInfo::tile_rule).
type NinePieceTileRule struct {
	Horizontal, Vertical css.ENinePieceImageRule
}

// NinePieceDrawInfo is the drawing information for one piece. Mirrors
// NinePieceImageGrid::NinePieceDrawInfo (nine_piece_image_grid.h:74-90 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). TileScale is {x, y}.
type NinePieceDrawInfo struct {
	IsDrawable    bool
	IsCornerPiece bool
	Destination   geometry.RectF
	Source        geometry.RectF
	TileScale     geometry.PointF
	TileRule      NinePieceTileRule
}

// NinePieceEdge is one side's slice (image-local units) and snapped width.
// Mirrors NinePieceImageGrid::Edge (nine_piece_image_grid.h:93-99 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type NinePieceEdge struct {
	Slice float64
	Width int
}

// IsDrawable mirrors Edge::IsDrawable (nine_piece_image_grid.h:95).
func (e NinePieceEdge) IsDrawable() bool { return e.Slice > 0 && e.Width > 0 }

// Scale mirrors Edge::Scale (nine_piece_image_grid.h:96).
func (e NinePieceEdge) Scale() float64 {
	if e.IsDrawable() {
		return float64(e.Width) / e.Slice
	}
	return 1
}

// NinePieceImageGrid computes drawing information for the nine border-image
// pieces. Mirrors Blink's NinePieceImageGrid (core/paint/nine_piece_image_grid.h
// :62-117, nine_piece_image_grid.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type NinePieceImageGrid struct {
	borderImageArea    image.Rectangle
	imageSize          geometry.SizeF
	horizontalTileRule css.ENinePieceImageRule
	verticalTileRule   css.ENinePieceImageRule
	zoom               float64
	fill               bool

	Top, Right, Bottom, Left NinePieceEdge
}

// computeEdgeWidth mirrors ComputeEdgeWidth (nine_piece_image_grid.cc:15-24).
func computeEdgeWidth(borderSlice css.BorderImageLength, borderSide int, imageSide float64, boxExtent int) layoutunit.LayoutUnit {
	if borderSlice.IsNumber() {
		return layoutunit.FromFloat64Trunc(borderSlice.Number() * float64(borderSide))
	}
	if borderSlice.IsAuto() {
		return layoutunit.FromFloat64Trunc(imageSide)
	}
	return layoutunit.FromFloat64Trunc(borderSlice.Length().ValueForLength(float64(boxExtent)))
}

// computeEdgeSlice mirrors ComputeEdgeSlice (nine_piece_image_grid.cc:26-39):
// a <number> slice is scaled into image space, a percentage resolves against
// the image side, and the result is clamped to that side (no halving of
// overlapping slices).
func computeEdgeSlice(slice css.Length, sliceScale, maximum float64) float64 {
	var resolved float64
	if !slice.IsPercent() {
		resolved = slice.Fixed * sliceScale
	} else {
		resolved = slice.ValueForLength(maximum)
	}
	resolved = math.Min(maximum, resolved)
	return layoutunit.FromFloat64Round(resolved).Float64()
}

// snapEdgeWidths mirrors SnapEdgeWidths (nine_piece_image_grid.cc:50-75).
// Widths are [top, right, bottom, left]; returns the same order.
func snapEdgeWidths(w [4]layoutunit.LayoutUnit, boxW, boxH int) [4]int {
	var out [4]int
	epsilon := layoutunit.FromRaw(1)
	if !epsilon.Less(layoutunit.New(boxW).Sub(w[3].Add(w[1]))) {
		out[3] = int(w[3].Round())
		out[1] = boxW - out[3]
	} else {
		out[3] = int(w[3].Floor())
		out[1] = int(w[1].Floor())
	}
	if !epsilon.Less(layoutunit.New(boxH).Sub(w[0].Add(w[2]))) {
		out[0] = int(w[0].Round())
		out[2] = boxH - out[0]
	} else {
		out[0] = int(w[0].Floor())
		out[2] = int(w[2].Floor())
	}
	return out
}

// NewNinePieceImageGrid mirrors the NinePieceImageGrid constructor
// (nine_piece_image_grid.cc:79-157 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// borderWidths is [top, right, bottom, left]. All sides are included
// (louis14 does not fragment border images across lines/columns yet).
func NewNinePieceImageGrid(n css.NinePieceImage, imageSize geometry.SizeF, sliceScale geometry.PointF,
	zoom float64, borderImageArea image.Rectangle, borderWidths [4]int) *NinePieceImageGrid {
	g := &NinePieceImageGrid{
		borderImageArea:    borderImageArea,
		imageSize:          imageSize,
		horizontalTileRule: n.HorizontalRule,
		verticalTileRule:   n.VerticalRule,
		zoom:               zoom,
		fill:               n.Fill,
	}
	g.Top.Slice = computeEdgeSlice(n.ImageSlices.Top, sliceScale.Y, imageSize.Height)
	g.Right.Slice = computeEdgeSlice(n.ImageSlices.Right, sliceScale.X, imageSize.Width)
	g.Bottom.Slice = computeEdgeSlice(n.ImageSlices.Bottom, sliceScale.Y, imageSize.Height)
	g.Left.Slice = computeEdgeSlice(n.ImageSlices.Left, sliceScale.X, imageSize.Width)

	// Slices are image-local; 'auto' widths want zoomed CSS px (:102-106).
	adjX, adjY := zoom/sliceScale.X, zoom/sliceScale.Y
	areaW, areaH := borderImageArea.Dx(), borderImageArea.Dy()
	w := [4]layoutunit.LayoutUnit{
		computeEdgeWidth(n.BorderSlices.Top, borderWidths[0], g.Top.Slice*adjY, areaH),
		computeEdgeWidth(n.BorderSlices.Right, borderWidths[1], g.Right.Slice*adjX, areaW),
		computeEdgeWidth(n.BorderSlices.Bottom, borderWidths[2], g.Bottom.Slice*adjY, areaH),
		computeEdgeWidth(n.BorderSlices.Left, borderWidths[3], g.Left.Slice*adjX, areaW),
	}

	// f = min(Lwidth/(Wleft+Wright), Lheight/(Wtop+Wbottom)); if f < 1 scale
	// all widths by f (:130-148).
	f := math.Min(float64(areaW)/w[3].Add(w[1]).Float64(), float64(areaH)/w[0].Add(w[2]).Float64())
	if f < 1 {
		for i := range w {
			w[i] = layoutunit.FromFloat64Trunc(w[i].Float64() * f)
		}
	}

	s := snapEdgeWidths(w, areaW, areaH)
	g.Top.Width, g.Right.Width, g.Bottom.Width, g.Left.Width = s[0], s[1], s[2], s[3]
	return g
}

// subrect mirrors the Subrect helpers (nine_piece_image_grid.cc:161-191):
// negative offsets are relative to the far edge of the base rect.
func subrect(base geometry.RectF, offsetX, offsetY, w, h float64) geometry.RectF {
	bx, by := base.X(), base.Y()
	if offsetX < 0 {
		bx = base.Right()
	}
	if offsetY < 0 {
		by = base.Bottom()
	}
	return geometry.NewRectF(bx+offsetX, by+offsetY, w, h)
}

func (g *NinePieceImageGrid) imageRect() geometry.RectF {
	return geometry.NewRectF(0, 0, g.imageSize.Width, g.imageSize.Height)
}

func (g *NinePieceImageGrid) areaRect() geometry.RectF {
	a := g.borderImageArea
	return geometry.NewRectF(float64(a.Min.X), float64(a.Min.Y), float64(a.Dx()), float64(a.Dy()))
}

// setDrawInfoCorner mirrors SetDrawInfoCorner (nine_piece_image_grid.cc:205-238).
func (g *NinePieceImageGrid) setDrawInfoCorner(d *NinePieceDrawInfo, piece NinePiece) {
	img, area := g.imageRect(), g.areaRect()
	var v, h NinePieceEdge // vertical-position edge (top/bottom), horizontal (left/right)
	var sx, sy, dx, dy float64
	switch piece {
	case TopLeftPiece:
		v, h = g.Top, g.Left
	case BottomLeftPiece:
		v, h = g.Bottom, g.Left
		sy, dy = -v.Slice, -float64(v.Width)
	case TopRightPiece:
		v, h = g.Top, g.Right
		sx, dx = -h.Slice, -float64(h.Width)
	case BottomRightPiece:
		v, h = g.Bottom, g.Right
		sx, dx = -h.Slice, -float64(h.Width)
		sy, dy = -v.Slice, -float64(v.Width)
	}
	d.IsDrawable = v.IsDrawable() && h.IsDrawable()
	if d.IsDrawable {
		d.Source = subrect(img, sx, sy, h.Slice, v.Slice)
		d.Destination = subrect(area, dx, dy, float64(h.Width), float64(v.Width))
	}
}

// setDrawInfoEdge mirrors SetDrawInfoEdge + SetHorizontalEdge /
// SetVerticalEdge (nine_piece_image_grid.cc:240-317).
func (g *NinePieceImageGrid) setDrawInfoEdge(d *NinePieceDrawInfo, piece NinePiece) {
	img, area := g.imageRect(), g.areaRect()
	srcW := g.imageSize.Width - (g.Left.Slice + g.Right.Slice)
	srcH := g.imageSize.Height - (g.Top.Slice + g.Bottom.Slice)
	dstW := float64(g.borderImageArea.Dx() - (g.Left.Width + g.Right.Width))
	dstH := float64(g.borderImageArea.Dy() - (g.Top.Width + g.Bottom.Width))

	var edge NinePieceEdge
	var src, dst geometry.RectF
	horizontal := piece == TopPiece || piece == BottomPiece
	switch piece {
	case LeftPiece:
		edge = g.Left
		src = subrect(img, 0, g.Top.Slice, g.Left.Slice, srcH)
		dst = subrect(area, 0, float64(g.Top.Width), float64(g.Left.Width), dstH)
	case RightPiece:
		edge = g.Right
		src = subrect(img, -g.Right.Slice, g.Top.Slice, g.Right.Slice, srcH)
		dst = subrect(area, -float64(g.Right.Width), float64(g.Top.Width), float64(g.Right.Width), dstH)
	case TopPiece:
		edge = g.Top
		src = subrect(img, g.Left.Slice, 0, srcW, g.Top.Slice)
		dst = subrect(area, float64(g.Left.Width), 0, dstW, float64(g.Top.Width))
	case BottomPiece:
		edge = g.Bottom
		src = subrect(img, g.Left.Slice, -g.Bottom.Slice, srcW, g.Bottom.Slice)
		dst = subrect(area, float64(g.Left.Width), -float64(g.Bottom.Width), dstW, float64(g.Bottom.Width))
	}
	if horizontal {
		d.IsDrawable = edge.IsDrawable() && src.Width() > 0 && dst.Width() > 0
		d.TileRule = NinePieceTileRule{g.horizontalTileRule, css.StretchImageRule}
	} else {
		d.IsDrawable = edge.IsDrawable() && src.Height() > 0 && dst.Height() > 0
		d.TileRule = NinePieceTileRule{css.StretchImageRule, g.verticalTileRule}
	}
	if d.IsDrawable {
		d.Source, d.Destination = src, dst
		d.TileScale = geometry.PointF{X: edge.Scale(), Y: edge.Scale()}
	}
}

// setDrawInfoMiddle mirrors SetDrawInfoMiddle (nine_piece_image_grid.cc:319-366).
func (g *NinePieceImageGrid) setDrawInfoMiddle(d *NinePieceDrawInfo) {
	srcW := g.imageSize.Width - (g.Left.Slice + g.Right.Slice)
	srcH := g.imageSize.Height - (g.Top.Slice + g.Bottom.Slice)
	dstW := float64(g.borderImageArea.Dx() - (g.Left.Width + g.Right.Width))
	dstH := float64(g.borderImageArea.Dy() - (g.Top.Width + g.Bottom.Width))

	d.IsDrawable = g.fill && srcW > 0 && srcH > 0 && dstW > 0 && dstH > 0
	if !d.IsDrawable {
		return
	}
	d.Source = subrect(g.imageRect(), g.Left.Slice, g.Top.Slice, srcW, srcH)
	d.Destination = subrect(g.areaRect(), float64(g.Left.Width), float64(g.Top.Width), dstW, dstH)

	scale := geometry.PointF{X: g.zoom, Y: g.zoom}
	if g.Top.IsDrawable() {
		scale.X = g.Top.Scale()
	} else if g.Bottom.IsDrawable() {
		scale.X = g.Bottom.Scale()
	}
	if g.Left.IsDrawable() {
		scale.Y = g.Left.Scale()
	} else if g.Right.IsDrawable() {
		scale.Y = g.Right.Scale()
	}
	if g.horizontalTileRule == css.StretchImageRule {
		scale.X = dstW / srcW
	}
	if g.verticalTileRule == css.StretchImageRule {
		scale.Y = dstH / srcH
	}
	d.TileScale = scale
	d.TileRule = NinePieceTileRule{g.horizontalTileRule, g.verticalTileRule}
}

// GetNinePieceDrawInfo mirrors NinePieceImageGrid::GetNinePieceDrawInfo
// (nine_piece_image_grid.cc:368-383 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (g *NinePieceImageGrid) GetNinePieceDrawInfo(piece NinePiece) NinePieceDrawInfo {
	var d NinePieceDrawInfo
	d.IsCornerPiece = piece == TopLeftPiece || piece == TopRightPiece ||
		piece == BottomLeftPiece || piece == BottomRightPiece
	switch {
	case d.IsCornerPiece:
		g.setDrawInfoCorner(&d, piece)
	case piece != MiddlePiece:
		g.setDrawInfoEdge(&d, piece)
	default:
		g.setDrawInfoMiddle(&d)
	}
	return d
}
