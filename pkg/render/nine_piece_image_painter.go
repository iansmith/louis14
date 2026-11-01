package render

import (
	"image"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	stdnet "louis14/std/net"
)

// calculateSpaceNeeded mirrors CalculateSpaceNeeded
// (core/paint/nine_piece_image_painter.cc:23-36 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the n whole tiles that fit are
// separated, and preceded and followed, by n+1 equal gaps.
func calculateSpaceNeeded(destination, source float64) (float64, bool) {
	repeatTilesCount := math.Floor(destination / source)
	if repeatTilesCount == 0 {
		return 0, false
	}
	space := destination - source*repeatTilesCount
	space /= repeatTilesCount + 1
	return space, true
}

// tileParameters mirrors TileParameters (nine_piece_image_painter.cc:38-43).
type tileParameters struct {
	scaleFactor float64
	phase       float64
	spacing     float64
}

// computeTileParameters mirrors ComputeTileParameters
// (nine_piece_image_painter.cc:45-73 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// ok is false when nothing should be painted (space with no whole tile).
func computeTileParameters(tileRule css.ENinePieceImageRule, dstExtent, srcExtent float64) (tileParameters, bool) {
	switch tileRule {
	case css.RoundImageRule:
		repetitions := math.Max(1, math.Round(dstExtent/srcExtent))
		return tileParameters{scaleFactor: dstExtent / (srcExtent * repetitions)}, true
	case css.RepeatImageRule:
		// Centre one tile: the pattern is symmetric about the extent's middle.
		return tileParameters{scaleFactor: 1, phase: (dstExtent - srcExtent) / 2}, true
	case css.SpaceImageRule:
		spacing, ok := calculateSpaceNeeded(dstExtent, srcExtent)
		if !ok {
			return tileParameters{}, false
		}
		return tileParameters{scaleFactor: 1, phase: spacing, spacing: spacing}, true
	}
	return tileParameters{scaleFactor: 1}, true
}

// shouldTile mirrors ShouldTile (nine_piece_image_painter.cc:75-85).
func shouldTile(d NinePieceDrawInfo) bool {
	if d.IsCornerPiece {
		return false
	}
	return !(d.TileRule.Horizontal == css.StretchImageRule && d.TileRule.Vertical == css.StretchImageRule)
}

// ninePieceImageSource is a decoded border-image source: a raster image or a
// generated (gradient) image rasterised at the border image area size.
type ninePieceImageSource struct {
	img image.Image
	// unzoomedSize is the image's size in CSS px (louis14 has no zoom, so it
	// equals the image's pixel size; Blink's slice_scale is then 1).
	unzoomedSize geometry.SizeF
}

// paintNinePieceImage paints layer's border-image into rect (the border box)
// and reports whether it did. Mirrors NinePieceImagePainter::Paint
// (core/paint/nine_piece_image_painter.cc:180-244 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), called from
// BoxPainterBase::PaintBorder (core/paint/box_painter_base.cc:1745-1775):
// border-image ignores border-radius and replaces the box border.
func (r *Renderer) paintNinePieceImage(layer *PaintLayer, rect layout.PhysicalRect) bool {
	x, y, w, h := pixelSnap(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height)
	box := layer.Box
	borderWidths := [4]float64{box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left}
	o := layer.BorderImage.ImageOutsets(borderWidths)
	// ToPixelSnappedRect(rect_with_outsets).
	area := image.Rect(int(math.Round(x-o[3])), int(math.Round(y-o[0])),
		int(math.Round(x+w+o[1])), int(math.Round(y+h+o[2])))
	if area.Empty() {
		return true
	}

	src, ok := r.acquireNinePieceImageSource(layer, area.Dx(), area.Dy())
	if !ok {
		return false
	}
	r.paintPieces(layer, area, src, [4]int{
		int(borderWidths[0]), int(borderWidths[1]), int(borderWidths[2]), int(borderWidths[3]),
	})
	return true
}

// acquireNinePieceImageSource resolves the StyleImage: a url() is fetched; a
// generated image is rasterised at the default object size, which for a
// border image is the border image area (nine_piece_image_painter.cc:216-224).
func (r *Renderer) acquireNinePieceImageSource(layer *PaintLayer, areaW, areaH int) (ninePieceImageSource, bool) {
	if layer.BorderImageGenerated != "" {
		img := r.renderGeneratedImage(layer.BorderImageGenerated, areaW, areaH, layer.Box.Style)
		return ninePieceImageSource{img: img, unzoomedSize: geometry.SizeF{Width: float64(areaW), Height: float64(areaH)}}, true
	}
	if layer.BorderImageSource == nil {
		return ninePieceImageSource{}, false
	}
	url := layer.BorderImageSource.Data.Absolute
	if r.imageFetcher == nil && !stdnet.IsDataURL(url) {
		return ninePieceImageSource{}, false
	}
	img, err := images.LoadImageWithFetcher(url, r.imageFetcher)
	if err != nil || img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		return ninePieceImageSource{}, false
	}
	b := img.Bounds()
	return ninePieceImageSource{img: img, unzoomedSize: geometry.SizeF{Width: float64(b.Dx()), Height: float64(b.Dy())}}, true
}

// renderGeneratedImage rasterises a gradient (StyleGeneratedImage →
// GradientGeneratedImage) into a w×h offscreen image.
// NOTE for coordinator: a generic gradient-to-image helper; lane LOU-531 may
// grow an equivalent for background tiling — reconcile into one.
func (r *Renderer) renderGeneratedImage(gradVal string, w, h int, style *css.Style) *image.RGBA {
	buf := image.NewRGBA(image.Rect(0, 0, w, h))
	origDC, origTarget := r.dc, r.target
	r.dc = origDC.NewChildContext(buf)
	r.target = buf
	defer func() { r.dc, r.target = origDC, origTarget }()
	r.drawGradient(gradVal, 0, 0, float64(w), float64(h), style)
	return buf
}

// paintPieces mirrors PaintPieces (nine_piece_image_painter.cc:87-176 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) paintPieces(layer *PaintLayer, area image.Rectangle, src ninePieceImageSource, borderWidths [4]int) {
	b := src.img.Bounds()
	imageSize := geometry.SizeF{Width: float64(b.Dx()), Height: float64(b.Dy())}
	sliceScale := geometry.PointF{
		X: imageSize.Width / src.unzoomedSize.Width,
		Y: imageSize.Height / src.unzoomedSize.Height,
	}
	grid := NewNinePieceImageGrid(layer.BorderImage, imageSize, sliceScale, 1, area, borderWidths)
	for piece := TopLeftPiece; piece < maxPiece; piece++ {
		d := grid.GetNinePieceDrawInfo(piece)
		if !d.IsDrawable {
			continue
		}
		if !shouldTile(d) {
			r.drawImageRect(src.img, d.Source, d.Destination, layer.ImageRendering)
			continue
		}
		hTile, hOK := computeTileParameters(d.TileRule.Horizontal, d.Destination.Width(), d.Source.Width()*d.TileScale.X)
		vTile, vOK := computeTileParameters(d.TileRule.Vertical, d.Destination.Height(), d.Source.Height()*d.TileScale.Y)
		if !hOK || !vOK {
			continue
		}
		r.drawImageTiled(src.img, d.Source, d.Destination,
			geometry.PointF{X: d.TileScale.X * hTile.scaleFactor, Y: d.TileScale.Y * vTile.scaleFactor},
			geometry.PointF{X: hTile.phase, Y: vTile.phase},
			geometry.PointF{X: hTile.spacing, Y: vTile.spacing},
			layer.ImageRendering)
	}
}

// subImageF crops img to the source rect (image-local, rounded to pixels).
func subImageF(img image.Image, s geometry.RectF) image.Image {
	b := img.Bounds()
	rr := image.Rect(b.Min.X+int(math.Round(s.X())), b.Min.Y+int(math.Round(s.Y())),
		b.Min.X+int(math.Round(s.Right())), b.Min.Y+int(math.Round(s.Bottom()))).Intersect(b)
	if rr.Empty() {
		return nil
	}
	return img.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(rr)
}

// snapRectF rounds a float rect's edges to device pixels.
func snapRectF(d geometry.RectF) image.Rectangle {
	return image.Rect(int(math.Round(d.X())), int(math.Round(d.Y())),
		int(math.Round(d.Right())), int(math.Round(d.Bottom())))
}

// drawImageRect draws the src rect of img scaled into dst
// (GraphicsContext::DrawImage with a src rect).
func (r *Renderer) drawImageRect(img image.Image, src, dst geometry.RectF, imageRendering string) {
	piece := subImageF(img, src)
	d := snapRectF(dst)
	if piece == nil || d.Empty() {
		return
	}
	pb := piece.Bounds()
	r.dc.DrawImage(scaleImage(piece, pb.Dx(), pb.Dy(), d.Dx(), d.Dy(), imageRendering), d.Min.X, d.Min.Y)
}

// drawImageTiled tiles the src rect of img over dst (GraphicsContext::
// DrawImageTiled with an ImageTilingInfo): each tile is src scaled by scale;
// the first tile's origin sits at dst.origin + phase, tiles step by the
// tile size plus spacing in both directions, and everything is clipped to dst.
func (r *Renderer) drawImageTiled(img image.Image, src, dst geometry.RectF, scale, phase, spacing geometry.PointF, imageRendering string) {
	piece := subImageF(img, src)
	clip := snapRectF(dst)
	if piece == nil || clip.Empty() {
		return
	}
	tileW, tileH := src.Width()*scale.X, src.Height()*scale.Y
	if tileW <= 0 || tileH <= 0 {
		return
	}
	stepX, stepY := tileW+spacing.X, tileH+spacing.Y
	x0 := dst.X() + phase.X
	x0 -= math.Ceil((x0-dst.X())/stepX) * stepX
	y0 := dst.Y() + phase.Y
	y0 -= math.Ceil((y0-dst.Y())/stepY) * stepY

	pb := piece.Bounds()
	cache := map[image.Point]image.Image{}
	for ty := y0; ty < dst.Bottom(); ty += stepY {
		for tx := x0; tx < dst.Right(); tx += stepX {
			t := image.Rect(int(math.Round(tx)), int(math.Round(ty)),
				int(math.Round(tx+tileW)), int(math.Round(ty+tileH)))
			vis := t.Intersect(clip)
			if vis.Empty() {
				continue
			}
			sz := t.Size()
			scaled, ok := cache[sz]
			if !ok {
				scaled = scaleImage(piece, pb.Dx(), pb.Dy(), sz.X, sz.Y, imageRendering)
				cache[sz] = scaled
			}
			sb := scaled.Bounds()
			off := vis.Min.Sub(t.Min)
			sub := scaled.(interface {
				SubImage(image.Rectangle) image.Image
			}).SubImage(image.Rect(sb.Min.X+off.X, sb.Min.Y+off.Y, sb.Min.X+off.X+vis.Dx(), sb.Min.Y+off.Y+vis.Dy()))
			r.dc.DrawImage(sub, vis.Min.X, vis.Min.Y)
		}
	}
}
