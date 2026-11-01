package render

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/images"
)

// This file ports Blink's BackgroundImageGeometry
// (core/paint/background_image_geometry.{h,cc} @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) together with the slice of
// BoxBackgroundPaintContext (core/paint/box_background_paint_context.{h,cc}
// @ a9f50e52) the geometry consumes. One geometry path serves every fill
// layer: raster images and gradients (generated images with no natural
// size) alike.

// NaturalSizingInfo describes a fill-layer image's natural dimensions and
// ratio. Mirrors Blink's NaturalSizingInfo (core/style/style_image.h @
// a9f50e52) as returned by StyleImage::GetNaturalSizingInfo. A gradient has
// none of the three; a raster image has all of them.
type NaturalSizingInfo struct {
	Width, Height       float64
	RatioW, RatioH      float64 // natural aspect ratio; zero = none
	HasWidth, HasHeight bool
}

// naturalSizingInfoForRaster returns the sizing info of a raster image with
// pixel dimensions w×h.
func naturalSizingInfoForRaster(w, h int) NaturalSizingInfo {
	return NaturalSizingInfo{
		Width: float64(w), Height: float64(h),
		RatioW: float64(w), RatioH: float64(h),
		HasWidth: true, HasHeight: true,
	}
}

// HasIntrinsicSize mirrors StyleImage::HasIntrinsicSize: true when the
// image has at least one natural dimension (generated images have none).
func (n NaturalSizingInfo) HasIntrinsicSize() bool { return n.HasWidth || n.HasHeight }

// concreteObjectSize resolves the image's size against a default object size
// per the CSS Images 3 default sizing algorithm (§5.3), the computation
// StyleImage::ImageSize performs for an `auto auto` background-size
// (background_image_geometry.cc:474-480 @ a9f50e52 calls
// image->ImageSize(zoom, positioning_area_size)). A generated image returns
// the default object size.
func (n NaturalSizingInfo) concreteObjectSize(def geometry.PhysicalSize) (float64, float64) {
	dw, dh := def.Width.Float64(), def.Height.Float64()
	hasRatio := n.RatioW > 0 && n.RatioH > 0
	switch {
	case n.HasWidth && n.HasHeight:
		return n.Width, n.Height
	case n.HasWidth:
		if hasRatio {
			return n.Width, n.Width * n.RatioH / n.RatioW
		}
		return n.Width, dh
	case n.HasHeight:
		if hasRatio {
			return n.Height * n.RatioW / n.RatioH, n.Height
		}
		return dw, n.Height
	case hasRatio:
		// Contain-fit the ratio into the default object size.
		if dw*n.RatioH > dh*n.RatioW {
			return dh * n.RatioW / n.RatioH, dh
		}
		return dw, dw * n.RatioH / n.RatioW
	}
	return dw, dh
}

// BoxBackgroundPaintContext carries the box-dependent inputs to
// BackgroundImageGeometry. Mirrors Blink's BoxBackgroundPaintContext
// (core/paint/box_background_paint_context.h:34-151 @ a9f50e52). louis14
// keeps the positioning box's border / padding outsets and border-edge
// opacity directly instead of a LayoutBoxModelObject pointer.
type BoxBackgroundPaintContext struct {
	// borderOutsets / paddingOutsets of positioning_box_
	// (BorderOutsets / PaddingOutsets, box_background_paint_context.cc:189-201).
	borderOutsets  geometry.PhysicalBoxStrut
	paddingOutsets geometry.PhysicalBoxStrut
	// obscuringEdges[side] = BorderEdge::ObscuresBackground for the
	// positioning box (top, right, bottom, left), border_edge.cc:56-67.
	obscuringEdges [4]bool

	positioningSizeOverride        geometry.PhysicalSize
	elementPositioningAreaOffset   geometry.PhysicalOffset
	hasBackgroundFixedToViewport   bool
	paintingView                   bool
	hasBorderImageOrCollapse       bool // feeds DisallowBorderDerivedAdjustment
	fixedAttachmentPositioningArea geometry.PhysicalRect
}

// EffectiveClip mirrors BoxBackgroundPaintContext::EffectiveClip
// (box_background_paint_context.h:59-64 @ a9f50e52): the view always clips
// to the border box (it paints the whole canvas).
func (c *BoxBackgroundPaintContext) EffectiveClip(fl *css.FillLayer) css.BackgroundClipType {
	if c.paintingView {
		return css.BackgroundClipBorderBox
	}
	return fl.Clip
}

// NormalPositioningArea mirrors box_background_paint_context.cc:309-316.
func (c *BoxBackgroundPaintContext) NormalPositioningArea(paintRect geometry.PhysicalRect) geometry.PhysicalRect {
	if c.paintingView {
		return geometry.PhysicalRect{Size: c.positioningSizeOverride}
	}
	return paintRect
}

// DisallowBorderDerivedAdjustment mirrors box_background_paint_context.cc:318-325.
func (c *BoxBackgroundPaintContext) DisallowBorderDerivedAdjustment() bool {
	return c.paintingView || c.hasBorderImageOrCollapse
}

// ShouldUseFixedAttachment mirrors box_background_paint_context.cc:332-340.
func (c *BoxBackgroundPaintContext) ShouldUseFixedAttachment(fl *css.FillLayer) bool {
	return c.hasBackgroundFixedToViewport && fl.Attachment == css.BackgroundAttachmentFixed
}

// OffsetInBackground mirrors box_background_paint_context.cc:415-421.
func (c *BoxBackgroundPaintContext) OffsetInBackground(fl *css.FillLayer) geometry.PhysicalOffset {
	if c.ShouldUseFixedAttachment(fl) {
		return geometry.PhysicalOffset{}
	}
	return c.elementPositioningAreaOffset
}

// innerBorderRect returns the pixel-snapped inner border rect of
// positioning_area: the snapped border box inset by the border widths.
// Stand-in for ContouredBorderGeometry::PixelSnappedContouredInnerBorder(
// style, positioning_area).Rect() (contoured_border_geometry.cc @ a9f50e52)
// — only its rect is consumed here.
func (c *BoxBackgroundPaintContext) innerBorderRect(positioningArea geometry.PhysicalRect) geometry.PhysicalRect {
	outer := geometry.PhysicalRectFromRect(geometry.ToPixelSnappedRect(positioningArea))
	return outer.Contract(c.borderOutsets)
}

// InnerBorderOutsets mirrors box_background_paint_context.cc:248-263.
func (c *BoxBackgroundPaintContext) InnerBorderOutsets(destRect, positioningArea geometry.PhysicalRect) geometry.PhysicalBoxStrut {
	inner := c.innerBorderRect(positioningArea)
	return geometry.PhysicalBoxStrut{
		Left:   inner.Offset.Left.Sub(destRect.Offset.Left),
		Top:    inner.Offset.Top.Sub(destRect.Offset.Top),
		Right:  destRect.Right().Sub(inner.Right()),
		Bottom: destRect.Bottom().Sub(inner.Bottom()),
	}
}

// SnappedAndUnsnappedOutsets mirrors box_background_paint_context.h:29-32.
type SnappedAndUnsnappedOutsets struct {
	Snapped, Unsnapped geometry.PhysicalBoxStrut
}

// ObscuredBorderOutsets mirrors box_background_paint_context.cc:265-297.
func (c *BoxBackgroundPaintContext) ObscuredBorderOutsets(destRect, positioningArea geometry.PhysicalRect) SnappedAndUnsnappedOutsets {
	inner := c.innerBorderRect(positioningArea)
	var adjust SnappedAndUnsnappedOutsets
	if c.obscuringEdges[0] {
		adjust.Snapped.Top = inner.Offset.Top.Sub(destRect.Offset.Top)
		adjust.Unsnapped.Top = c.borderOutsets.Top
	}
	if c.obscuringEdges[1] {
		adjust.Snapped.Right = destRect.Right().Sub(inner.Right())
		adjust.Unsnapped.Right = c.borderOutsets.Right
	}
	if c.obscuringEdges[2] {
		adjust.Snapped.Bottom = destRect.Bottom().Sub(inner.Bottom())
		adjust.Unsnapped.Bottom = c.borderOutsets.Bottom
	}
	if c.obscuringEdges[3] {
		adjust.Snapped.Left = inner.Offset.Left.Sub(destRect.Offset.Left)
		adjust.Unsnapped.Left = c.borderOutsets.Left
	}
	return adjust
}

// BackgroundImageGeometry computes where and how a fill layer's image tiles.
// Mirrors Blink's BackgroundImageGeometry (background_image_geometry.h:23-150
// @ a9f50e52).
type BackgroundImageGeometry struct {
	unsnappedDestRect geometry.PhysicalRect
	snappedDestRect   geometry.PhysicalRect
	phase             geometry.PhysicalOffset
	tileSize          geometry.PhysicalSize
	repeatSpacing     geometry.PhysicalSize
}

func (g *BackgroundImageGeometry) SnappedDestRect() geometry.PhysicalRect { return g.snappedDestRect }
func (g *BackgroundImageGeometry) TileSize() geometry.PhysicalSize        { return g.tileSize }
func (g *BackgroundImageGeometry) SpaceSize() geometry.PhysicalSize       { return g.repeatSpacing }

// intMod mirrors Blink's IntMod (platform/geometry/layout_unit.h:714-716 @
// a9f50e52): the truncated remainder of the raw values, sign of a.
func intMod(a, b geometry.LayoutUnit) geometry.LayoutUnit {
	if b.IsZero() {
		return layoutunit.Zero
	}
	return layoutunit.FromRaw(int64(a.Raw()) % int64(b.Raw()))
}

func neg(a geometry.LayoutUnit) geometry.LayoutUnit { return layoutunit.Zero.Sub(a) }

// getSpaceBetweenImageTiles mirrors background_image_geometry.cc:20-31.
func getSpaceBetweenImageTiles(areaSize, tileSize geometry.LayoutUnit) geometry.LayoutUnit {
	numberOfTiles := int(areaSize.Div(tileSize).ToInt())
	space := layoutunit.New(-1)
	if numberOfTiles > 1 {
		space = areaSize.Sub(tileSize.MulInt(numberOfTiles)).DivInt(numberOfTiles - 1)
	}
	return space
}

// computeRoundedTileSize mirrors background_image_geometry.cc:33-36.
func computeRoundedTileSize(areaSize, tileSize geometry.LayoutUnit) geometry.LayoutUnit {
	nrTiles := max(1, int(areaSize.Div(tileSize).Round()))
	return areaSize.DivInt(nrTiles)
}

// computeTilePhase mirrors background_image_geometry.cc:38-43.
func computeTilePhase(position, tileExtent geometry.LayoutUnit) geometry.LayoutUnit {
	if tileExtent.IsZero() {
		return layoutunit.Zero
	}
	return tileExtent.Sub(intMod(position, tileExtent))
}

// resolveWidthForRatio / resolveHeightForRatio mirror Blink's
// ResolveWidthForRatio / ResolveHeightForRatio (a MulDiv by the ratio).
func resolveWidthForRatio(height geometry.LayoutUnit, ratio geometry.PhysicalSize) geometry.LayoutUnit {
	return height.MulDiv(ratio.Width, ratio.Height)
}

func resolveHeightForRatio(width geometry.LayoutUnit, ratio geometry.PhysicalSize) geometry.LayoutUnit {
	return width.MulDiv(ratio.Height, ratio.Width)
}

// resolveClampedWidthForRatio mirrors background_image_geometry.cc:45-52.
func resolveClampedWidthForRatio(height geometry.LayoutUnit, ratio geometry.PhysicalSize) geometry.LayoutUnit {
	w := resolveWidthForRatio(height, ratio)
	if !ratio.Width.Less(layoutunit.New(1)) && w.Less(layoutunit.New(1)) {
		return layoutunit.New(1)
	}
	return w
}

// resolveClampedHeightForRatio mirrors background_image_geometry.cc:54-61.
func resolveClampedHeightForRatio(width geometry.LayoutUnit, ratio geometry.PhysicalSize) geometry.LayoutUnit {
	h := resolveHeightForRatio(width, ratio)
	if !ratio.Height.Less(layoutunit.New(1)) && h.Less(layoutunit.New(1)) {
		return layoutunit.New(1)
	}
	return h
}

// resolveXPosition mirrors background_image_geometry.cc:63-74: the
// background-position x resolved against available_width (= positioning
// area − tile), minus offset. louis14's BackgroundPosition already folds
// right-edge origins into its value, so ResolveX(available, 0) is the
// absolute position.
func resolveXPosition(fl *css.FillLayer, availableWidth, offset geometry.LayoutUnit) geometry.LayoutUnit {
	return layoutunit.FromFloat64Trunc(fl.Position.ResolveX(availableWidth.Float64(), 0)).Sub(offset)
}

// resolveYPosition mirrors background_image_geometry.cc:76-87.
func resolveYPosition(fl *css.FillLayer, availableHeight, offset geometry.LayoutUnit) geometry.LayoutUnit {
	return layoutunit.FromFloat64Trunc(fl.Position.ResolveY(availableHeight.Float64(), 0)).Sub(offset)
}

// setNoRepeatX mirrors background_image_geometry.cc:101-138 (fill layers
// here are backgrounds, so NeedsFullSizeDestination is always false).
func (g *BackgroundImageGeometry) setNoRepeatX(xOffset, snappedXOffset geometry.LayoutUnit) {
	if layoutunit.Zero.Less(xOffset) {
		g.unsnappedDestRect.Offset.Left = g.unsnappedDestRect.Offset.Left.Add(xOffset)
		g.snappedDestRect.Offset.Left = layoutunit.New(int(g.unsnappedDestRect.Offset.Left.Round()))
		g.unsnappedDestRect.Size.Width = g.tileSize.Width
		g.snappedDestRect.Size.Width = g.tileSize.Width
		g.phase.Left = layoutunit.Zero
	} else {
		g.phase.Left = neg(xOffset)
		g.unsnappedDestRect.Size.Width = g.tileSize.Width.Add(xOffset)
		g.snappedDestRect.Size.Width = g.tileSize.Width.Add(snappedXOffset)
	}
	g.repeatSpacing.Width = layoutunit.Zero
}

// setNoRepeatY mirrors background_image_geometry.cc:140-177.
func (g *BackgroundImageGeometry) setNoRepeatY(yOffset, snappedYOffset geometry.LayoutUnit) {
	if layoutunit.Zero.Less(yOffset) {
		g.unsnappedDestRect.Offset.Top = g.unsnappedDestRect.Offset.Top.Add(yOffset)
		g.snappedDestRect.Offset.Top = layoutunit.New(int(g.unsnappedDestRect.Offset.Top.Round()))
		g.unsnappedDestRect.Size.Height = g.tileSize.Height
		g.snappedDestRect.Size.Height = g.tileSize.Height
		g.phase.Top = layoutunit.Zero
	} else {
		g.phase.Top = neg(yOffset)
		g.unsnappedDestRect.Size.Height = g.tileSize.Height.Add(yOffset)
		g.snappedDestRect.Size.Height = g.tileSize.Height.Add(snappedYOffset)
	}
	g.repeatSpacing.Height = layoutunit.Zero
}

// setRepeatX mirrors background_image_geometry.cc:179-185.
func (g *BackgroundImageGeometry) setRepeatX(xOffset geometry.LayoutUnit) {
	g.phase.Left = computeTilePhase(xOffset, g.tileSize.Width)
	g.repeatSpacing.Width = layoutunit.Zero
}

// setRepeatY mirrors background_image_geometry.cc:187-193.
func (g *BackgroundImageGeometry) setRepeatY(yOffset geometry.LayoutUnit) {
	g.phase.Top = computeTilePhase(yOffset, g.tileSize.Height)
	g.repeatSpacing.Height = layoutunit.Zero
}

// setSpaceX mirrors background_image_geometry.cc:195-200.
func (g *BackgroundImageGeometry) setSpaceX(space, extraOffset geometry.LayoutUnit) {
	g.repeatSpacing.Width = space
	g.phase.Left = computeTilePhase(extraOffset, g.tileSize.Width.Add(space))
}

// setSpaceY mirrors background_image_geometry.cc:202-207.
func (g *BackgroundImageGeometry) setSpaceY(space, extraOffset geometry.LayoutUnit) {
	g.repeatSpacing.Height = space
	g.phase.Top = computeTilePhase(extraOffset, g.tileSize.Height.Add(space))
}

// paddingBoxOutsets is the kPadding case shared by
// ComputeDestRectAdjustments (background_image_geometry.cc:235-246) and
// ComputePositioningAreaAdjustments (:305-319): the border outsets, snapped
// to the inner border edge of positioningArea unless border-derived
// adjustment is disallowed. rect is the rect being adjusted.
func paddingBoxOutsets(ctx *BoxBackgroundPaintContext, rect, positioningArea geometry.PhysicalRect, disallow bool) SnappedAndUnsnappedOutsets {
	o := SnappedAndUnsnappedOutsets{Unsnapped: ctx.borderOutsets, Snapped: ctx.borderOutsets}
	if !disallow {
		o.Snapped = ctx.InnerBorderOutsets(rect, positioningArea)
	}
	return o
}

// contentBoxOutsets is the kContent case of both adjustments (:224-234,
// :294-304): padding + border outsets, unsnapped; with zero padding it
// falls through to the kPadding case.
func contentBoxOutsets(ctx *BoxBackgroundPaintContext, rect, positioningArea geometry.PhysicalRect, disallow bool) SnappedAndUnsnappedOutsets {
	if ctx.paddingOutsets.IsZero() {
		return paddingBoxOutsets(ctx, rect, positioningArea, disallow)
	}
	u := ctx.paddingOutsets.Add(ctx.borderOutsets)
	return SnappedAndUnsnappedOutsets{Unsnapped: u, Snapped: u}
}

// computeDestRectAdjustments mirrors background_image_geometry.cc:209-280.
func (g *BackgroundImageGeometry) computeDestRectAdjustments(fl *css.FillLayer, ctx *BoxBackgroundPaintContext, unsnappedPositioningArea geometry.PhysicalRect, disallow bool) SnappedAndUnsnappedOutsets {
	switch ctx.EffectiveClip(fl) {
	case css.BackgroundClipContentBox:
		return contentBoxOutsets(ctx, g.unsnappedDestRect, unsnappedPositioningArea, disallow)
	case css.BackgroundClipPaddingBox:
		return paddingBoxOutsets(ctx, g.unsnappedDestRect, unsnappedPositioningArea, disallow)
	}
	// border-box: obscuring border edges may shrink the dest rect.
	if disallow {
		return SnappedAndUnsnappedOutsets{}
	}
	return ctx.ObscuredBorderOutsets(g.unsnappedDestRect, unsnappedPositioningArea)
}

// computePositioningAreaAdjustments mirrors background_image_geometry.cc:282-336.
func computePositioningAreaAdjustments(fl *css.FillLayer, ctx *BoxBackgroundPaintContext, unsnappedPositioningArea geometry.PhysicalRect, disallow bool) SnappedAndUnsnappedOutsets {
	switch fl.Origin {
	case css.BackgroundOriginContentBox:
		return contentBoxOutsets(ctx, unsnappedPositioningArea, unsnappedPositioningArea, disallow)
	case css.BackgroundOriginBorderBox:
		return SnappedAndUnsnappedOutsets{} // all adjustments remain 0
	}
	return paddingBoxOutsets(ctx, unsnappedPositioningArea, unsnappedPositioningArea, disallow)
}

// adjustPositioningArea mirrors background_image_geometry.cc:338-404.
// louis14 paints fill layers only in the background phase with source-over
// compositing, so disallow_border_derived_adjustment reduces to the paint
// context's DisallowBorderDerivedAdjustment.
func (g *BackgroundImageGeometry) adjustPositioningArea(fl *css.FillLayer, ctx *BoxBackgroundPaintContext, unsnappedPositioningArea, snappedPositioningArea *geometry.PhysicalRect, unsnappedBoxOffset, snappedBoxOffset *geometry.PhysicalOffset) {
	disallow := ctx.DisallowBorderDerivedAdjustment()
	destAdjust := g.computeDestRectAdjustments(fl, ctx, *unsnappedPositioningArea, disallow)
	boxOutset := computePositioningAreaAdjustments(fl, ctx, *unsnappedPositioningArea, disallow)

	*unsnappedBoxOffset = boxOutset.Unsnapped.Offset().Sub(destAdjust.Unsnapped.Offset())
	*snappedBoxOffset = boxOutset.Snapped.Offset().Sub(destAdjust.Snapped.Offset())

	g.snappedDestRect = clampSizeToZero(geometry.PhysicalRectFromRect(
		geometry.ToPixelSnappedRect(g.unsnappedDestRect.Contract(destAdjust.Snapped))))
	g.unsnappedDestRect = clampSizeToZero(g.unsnappedDestRect.Contract(destAdjust.Unsnapped))
	*snappedPositioningArea = clampSizeToZero(geometry.PhysicalRectFromRect(
		geometry.ToPixelSnappedRect(unsnappedPositioningArea.Contract(boxOutset.Snapped))))
	*unsnappedPositioningArea = clampSizeToZero(unsnappedPositioningArea.Contract(boxOutset.Unsnapped))
}

// clampSizeToZero mirrors PhysicalSize::ClampNegativeToZero applied to a
// rect's size (platform/geometry/physical_size.h:98-101 @ a9f50e52).
func clampSizeToZero(r geometry.PhysicalRect) geometry.PhysicalRect {
	r.Size = clampNegativeToZero(r.Size)
	return r
}

func clampNegativeToZero(s geometry.PhysicalSize) geometry.PhysicalSize {
	return geometry.PhysicalSize{
		Width:  layoutunit.Max(s.Width, layoutunit.Zero),
		Height: layoutunit.Max(s.Height, layoutunit.Zero),
	}
}

// fitToAspectRatio mirrors PhysicalSize::FitToAspectRatio
// (platform/geometry/physical_size.cc:12-26 @ a9f50e52).
func fitToAspectRatio(s, ratio geometry.PhysicalSize, grow bool) geometry.PhysicalSize {
	constrainedHeight := s.Width.MulDiv(ratio.Height, ratio.Width)
	if (grow && constrainedHeight.Less(s.Height)) || (!grow && s.Height.Less(constrainedHeight)) {
		return geometry.PhysicalSize{Width: s.Height.MulDiv(ratio.Width, ratio.Height), Height: s.Height}
	}
	return geometry.PhysicalSize{Width: s.Width, Height: constrainedHeight}
}

// resolveFillSizeLength resolves one background-size component: louis14's
// BackgroundSize stores 0 for auto, a negative value for a percentage, a
// positive value for pixels.
func resolveFillSizeLength(v float64, area geometry.LayoutUnit) geometry.LayoutUnit {
	if v < 0 {
		return layoutunit.ResolvePercent(area, -v)
	}
	return layoutunit.FromFloat64Trunc(v)
}

// calculateFillTileSize mirrors background_image_geometry.cc:406-523.
func (g *BackgroundImageGeometry) calculateFillTileSize(fl *css.FillLayer, info NaturalSizingInfo, unsnappedAreaSize, snappedAreaSize geometry.PhysicalSize) {
	var ratio geometry.PhysicalSize
	if info.RatioW > 0 && info.RatioH > 0 {
		ratio = geometry.PhysicalSizeFromF64Floor(info.RatioW, info.RatioH)
	}
	ratioEmpty := !layoutunit.Zero.Less(ratio.Width) || !layoutunit.Zero.Less(ratio.Height)
	areaSize := unsnappedAreaSize
	if !info.HasIntrinsicSize() {
		areaSize = snappedAreaSize
	}
	size := fl.Size
	if size.Cover || size.Contain {
		if ratioEmpty {
			g.tileSize = snappedAreaSize
			return
		}
		g.tileSize = fitToAspectRatio(snappedAreaSize, ratio, size.Cover)
		one := layoutunit.New(1)
		if size.Contain {
			if g.tileSize.Width != snappedAreaSize.Width {
				g.tileSize.Width = layoutunit.New(max(1, int(g.tileSize.Width.Round())))
			}
			if g.tileSize.Height != snappedAreaSize.Height {
				g.tileSize.Height = layoutunit.New(max(1, int(g.tileSize.Height.Round())))
			}
		} else {
			if g.tileSize.Width != snappedAreaSize.Width {
				g.tileSize.Width = layoutunit.Max(one, g.tileSize.Width)
			}
			if g.tileSize.Height != snappedAreaSize.Height {
				g.tileSize.Height = layoutunit.Max(one, g.tileSize.Height)
			}
		}
		return
	}

	// kSizeLength.
	g.tileSize = areaSize
	widthAuto, heightAuto := size.Width == 0, size.Height == 0
	if !widthAuto {
		g.tileSize.Width = resolveFillSizeLength(size.Width, areaSize.Width)
	}
	if !heightAuto {
		g.tileSize.Height = resolveFillSizeLength(size.Height, areaSize.Height)
	}
	switch {
	case widthAuto && !heightAuto:
		if !ratioEmpty {
			g.tileSize.Width = resolveClampedWidthForRatio(g.tileSize.Height, ratio)
		} else if info.HasWidth {
			g.tileSize.Width = layoutunit.FromFloat64Floor(info.Width)
		} else {
			g.tileSize.Width = areaSize.Width
		}
	case !widthAuto && heightAuto:
		if !ratioEmpty {
			g.tileSize.Height = resolveClampedHeightForRatio(g.tileSize.Width, ratio)
		} else if info.HasHeight {
			g.tileSize.Height = layoutunit.FromFloat64Floor(info.Height)
		} else {
			g.tileSize.Height = areaSize.Height
		}
	case widthAuto && heightAuto:
		w, h := info.concreteObjectSize(areaSize)
		g.tileSize = geometry.PhysicalSizeFromF64Floor(w, h)
	}
	g.tileSize = clampNegativeToZero(g.tileSize)
}

// calculateRepeatAndPosition mirrors background_image_geometry.cc:525-644.
func (g *BackgroundImageGeometry) calculateRepeatAndPosition(fl *css.FillLayer, offsetInBackground geometry.PhysicalOffset, unsnappedAreaSize, snappedAreaSize geometry.PhysicalSize, unsnappedBoxOffset, snappedBoxOffset geometry.PhysicalOffset) {
	repeatX, repeatY := fl.Repeat.X, fl.Repeat.Y
	z := layoutunit.Zero

	unsnappedAvailableWidth := unsnappedAreaSize.Width.Sub(g.tileSize.Width)
	unsnappedAvailableHeight := unsnappedAreaSize.Height.Sub(g.tileSize.Height)
	snappedAvailableWidth := snappedAreaSize.Width.Sub(g.tileSize.Width)
	snappedAvailableHeight := snappedAreaSize.Height.Sub(g.tileSize.Height)

	if repeatX == css.BackgroundRepeatRound && z.Less(snappedAreaSize.Width) && z.Less(g.tileSize.Width) {
		roundedWidth := computeRoundedTileSize(snappedAreaSize.Width, g.tileSize.Width)
		// Maintain aspect ratio if background-size: auto is set.
		if fl.Size.Height == 0 && !fl.Size.Cover && !fl.Size.Contain && repeatY != css.BackgroundRepeatRound {
			g.tileSize.Height = resolveClampedHeightForRatio(roundedWidth, g.tileSize)
		}
		g.tileSize.Width = roundedWidth
		xOffset := resolveXPosition(fl, snappedAvailableWidth, offsetInBackground.Left)
		g.phase.Left = computeTilePhase(xOffset.Add(unsnappedBoxOffset.Left), g.tileSize.Width)
		g.repeatSpacing = geometry.PhysicalSize{}
	}
	if repeatY == css.BackgroundRepeatRound && z.Less(snappedAreaSize.Height) && z.Less(g.tileSize.Height) {
		roundedHeight := computeRoundedTileSize(snappedAreaSize.Height, g.tileSize.Height)
		if fl.Size.Width == 0 && !fl.Size.Cover && !fl.Size.Contain && repeatX != css.BackgroundRepeatRound {
			g.tileSize.Width = resolveClampedWidthForRatio(roundedHeight, g.tileSize)
		}
		g.tileSize.Height = roundedHeight
		yOffset := resolveYPosition(fl, snappedAvailableHeight, offsetInBackground.Top)
		g.phase.Top = computeTilePhase(yOffset.Add(unsnappedBoxOffset.Top), g.tileSize.Height)
		g.repeatSpacing = geometry.PhysicalSize{}
	}

	if repeatX == css.BackgroundRepeatRepeat {
		xOffset := resolveXPosition(fl, unsnappedAvailableWidth, offsetInBackground.Left)
		g.setRepeatX(unsnappedBoxOffset.Left.Add(xOffset))
	} else if repeatX == css.BackgroundRepeatSpace && z.Less(g.tileSize.Width) {
		space := getSpaceBetweenImageTiles(snappedAreaSize.Width, g.tileSize.Width)
		if !space.Less(z) {
			g.setSpaceX(space, snappedBoxOffset.Left)
		} else {
			repeatX = css.BackgroundRepeatNoRepeat
		}
	}
	if repeatX == css.BackgroundRepeatNoRepeat {
		xOffset := resolveXPosition(fl, unsnappedAvailableWidth, offsetInBackground.Left)
		snappedXOffset := resolveXPosition(fl, snappedAvailableWidth, offsetInBackground.Left)
		g.setNoRepeatX(unsnappedBoxOffset.Left.Add(xOffset), snappedBoxOffset.Left.Add(snappedXOffset))
	}

	if repeatY == css.BackgroundRepeatRepeat {
		yOffset := resolveYPosition(fl, unsnappedAvailableHeight, offsetInBackground.Top)
		g.setRepeatY(unsnappedBoxOffset.Top.Add(yOffset))
	} else if repeatY == css.BackgroundRepeatSpace && z.Less(g.tileSize.Height) {
		space := getSpaceBetweenImageTiles(snappedAreaSize.Height, g.tileSize.Height)
		if !space.Less(z) {
			g.setSpaceY(space, snappedBoxOffset.Top)
		} else {
			repeatY = css.BackgroundRepeatNoRepeat
		}
	}
	if repeatY == css.BackgroundRepeatNoRepeat {
		yOffset := resolveYPosition(fl, unsnappedAvailableHeight, offsetInBackground.Top)
		snappedYOffset := resolveYPosition(fl, snappedAvailableHeight, offsetInBackground.Top)
		g.setNoRepeatY(unsnappedBoxOffset.Top.Add(yOffset), snappedBoxOffset.Top.Add(snappedYOffset))
	}
}

// Calculate mirrors BackgroundImageGeometry::Calculate
// (background_image_geometry.cc:654-757 @ a9f50e52), minus the border-shape
// state (louis14 has no border-shape).
func (g *BackgroundImageGeometry) Calculate(fl *css.FillLayer, ctx *BoxBackgroundPaintContext, paintRect geometry.PhysicalRect, info NaturalSizingInfo) {
	var unsnappedPositioningArea, snappedPositioningArea geometry.PhysicalRect
	var unsnappedBoxOffset, snappedBoxOffset geometry.PhysicalOffset

	fixed := ctx.ShouldUseFixedAttachment(fl)
	if fixed {
		unsnappedPositioningArea = ctx.fixedAttachmentPositioningArea
		g.unsnappedDestRect = unsnappedPositioningArea
		g.snappedDestRect = unsnappedPositioningArea
		snappedPositioningArea = unsnappedPositioningArea
	} else {
		unsnappedPositioningArea = ctx.NormalPositioningArea(paintRect)
		g.unsnappedDestRect = paintRect
		g.adjustPositioningArea(fl, ctx, &unsnappedPositioningArea, &snappedPositioningArea, &unsnappedBoxOffset, &snappedBoxOffset)
	}

	g.calculateFillTileSize(fl, info, unsnappedPositioningArea.Size, snappedPositioningArea.Size)

	g.calculateRepeatAndPosition(fl, ctx.OffsetInBackground(fl), unsnappedPositioningArea.Size,
		snappedPositioningArea.Size, unsnappedBoxOffset, snappedBoxOffset)

	if fixed {
		adj := paintRect.Offset.Sub(g.unsnappedDestRect.Offset)
		adj.Left = layoutunit.Max(adj.Left, layoutunit.Zero)
		adj.Top = layoutunit.Max(adj.Top, layoutunit.Zero)
		g.phase = g.phase.Add(adj)
	}

	// Clip the final output rect to the paint rect.
	g.unsnappedDestRect = g.unsnappedDestRect.Intersect(paintRect)
	g.snappedDestRect = g.snappedDestRect.Intersect(paintRect)
	// Re-snap the dest rect as it may have been adjusted with unsnapped values.
	g.snappedDestRect = geometry.PhysicalRectFromRect(geometry.ToPixelSnappedRect(g.snappedDestRect))
}

// ComputePhase mirrors background_image_geometry.cc:834-842: the phase
// reduced to at most one tile + spacing in magnitude. The first tile's
// origin is SnappedDestRect().offset + ComputePhase().
func (g *BackgroundImageGeometry) ComputePhase() geometry.PhysicalOffset {
	stepW := g.tileSize.Width.Add(g.repeatSpacing.Width)
	stepH := g.tileSize.Height.Add(g.repeatSpacing.Height)
	return geometry.PhysicalOffset{
		Left: intMod(neg(g.phase.Left), stepW),
		Top:  intMod(neg(g.phase.Top), stepH),
	}
}

// naturalSizingInfoForSVG returns the natural sizing info of an SVG image
// from its root <svg>: width / height when absolute (percentages are not
// natural), the ratio from a non-empty size or else the viewBox. Mirrors
// LayoutSVGRoot::UnscaledNaturalSizingInfo (layout_svg_root.cc:74-103 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) plus SVGImage::
// GetNaturalDimensions (svg_image.cc:238-264), which drops everything for a
// partially sized image with preserveAspectRatio="none" (the
// SvgSizingWithPreserveAspectRatioNone flag that removes that is
// "experimental", runtime_enabled_features.json5:6045-6046).
func naturalSizingInfoForSVG(svg images.SVGSizingInfo) NaturalSizingInfo {
	n := NaturalSizingInfo{
		Width: svg.ExplicitWidth, Height: svg.ExplicitHeight,
		HasWidth: svg.HasExplicitWidth, HasHeight: svg.HasExplicitHeight,
	}
	if (!n.HasWidth || !n.HasHeight) && svg.PreserveAspectRatioNone {
		return NaturalSizingInfo{}
	}
	switch {
	case n.Width > 0 && n.Height > 0:
		n.RatioW, n.RatioH = n.Width, n.Height
	case svg.HasViewBox && svg.ViewBoxWidth > 0 && svg.ViewBoxHeight > 0:
		// The viewBox can only yield a natural ratio, not a natural size.
		n.RatioW, n.RatioH = svg.ViewBoxWidth, svg.ViewBoxHeight
	}
	return n
}
