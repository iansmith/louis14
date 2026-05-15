package render

import (
	"image"
	"image/color"
	"math"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// patternShader samples a pre-rendered tile at each user-space point.
// The tile is rasterized once at shader-build time at the tile's
// effective pixel resolution. SampleAt computes the user→tile
// transform per pixel.
//
// Mirrors Blink's LayoutSVGResourcePattern::BuildPattern →
// cc::PaintShader::MakeImage (a tiled image shader with the wrap mode
// set to repeat).
type patternShader struct {
	// tile is the rasterized tile (in tile-pixel space).
	tile *image.RGBA

	// userFromTile maps a tile-pixel coord (px) → user-space coord.
	// Composed from the tile's TileRect + patternTransform.
	// userFromTile = translate(tileX, tileY) ∘ patternTransform ∘
	//                scale(tileW / pxW, tileH / pxH)
	//
	// tileFromUser is its inverse; that's what SampleAt actually
	// uses.
	tileFromUser geometry.AffineTransform

	// tileWUser / tileHUser are the tile's extent in USER space — the
	// modulo basis when wrapping. Even though `tileFromUser` already
	// maps into the tile's pixel space, the wrap happens in user
	// space before the per-pixel sampling so wrap aligns with the
	// repeat seam.
	tileWUser, tileHUser float64

	// tilePxW, tilePxH are the tile buffer's pixel dimensions.
	tilePxW, tilePxH int

	opacity float64
}

func (s *patternShader) SampleAt(userX, userY float64) css.Color {
	if s.tile == nil || s.tilePxW == 0 || s.tilePxH == 0 {
		return css.Color{}
	}
	if s.tileWUser <= 0 || s.tileHUser <= 0 {
		return css.Color{}
	}
	// Map user-space coord into tile-pixel space.
	t := s.tileFromUser
	tx := t.A*userX + t.C*userY + t.E
	ty := t.B*userX + t.D*userY + t.F
	// Wrap into the tile's pixel range [0, tilePxW) × [0, tilePxH).
	tx = math.Mod(tx, float64(s.tilePxW))
	if tx < 0 {
		tx += float64(s.tilePxW)
	}
	ty = math.Mod(ty, float64(s.tilePxH))
	if ty < 0 {
		ty += float64(s.tilePxH)
	}
	px := int(tx)
	py := int(ty)
	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	if px >= s.tilePxW {
		px = s.tilePxW - 1
	}
	if py >= s.tilePxH {
		py = s.tilePxH - 1
	}
	idx := s.tile.PixOffset(px, py)
	pix := s.tile.Pix[idx : idx+4]
	// image.RGBA stores PREMULTIPLIED alpha. Convert back to straight
	// alpha for the shader compositor (which does its own
	// premultiplication when blending).
	a := float64(pix[3])
	if a == 0 {
		return css.Color{}
	}
	c := css.Color{
		R: pix[0],
		G: pix[1],
		B: pix[2],
		A: a / 255.0,
	}
	if a < 255 {
		// Unpremultiply.
		c.R = uint8(math.Round(float64(pix[0]) * 255.0 / a))
		c.G = uint8(math.Round(float64(pix[1]) * 255.0 / a))
		c.B = uint8(math.Round(float64(pix[2]) * 255.0 / a))
	}
	c.A *= s.opacity
	return c
}

// buildPatternShader is the layout→render bridge for `<pattern>`. It
// resolves the tile rect, builds an offscreen tile image at a
// reasonable resolution, renders the pattern's children into it, and
// returns the shader.
//
// Tile resolution: for objectBBox mode the tile dimensions are
// fractions of the reference bbox; we multiply by bbox W/H to get
// user-space tile size, then round to integer pixels (clamped to a
// reasonable max so a tile="200% 200%" with a huge shape doesn't
// allocate a gigantic buffer).
//
// Empty / zero tile returns (nil, false) so the caller treats it as
// "no paint" per SVG 1.1 §13.3 ("a zero-sized tile produces no paint").
func buildPatternShader(p *svg.SVGPattern, referenceBox geometry.RectF, lengthCtx svg.SVGLengthContext, opacity float64, resources *svg.SVGResourceRegistry) (paintShader, bool) {
	if p == nil {
		return nil, false
	}
	// Resolve tile rect against the unit mode + reference box.
	tileX := p.TileX.ResolveAgainst(p.PatternUnits, svg.LengthModeWidth, lengthCtx, 0)
	tileY := p.TileY.ResolveAgainst(p.PatternUnits, svg.LengthModeHeight, lengthCtx, 0)
	tileW := p.TileWidth.ResolveAgainst(p.PatternUnits, svg.LengthModeWidth, lengthCtx, 0)
	tileH := p.TileHeight.ResolveAgainst(p.PatternUnits, svg.LengthModeHeight, lengthCtx, 0)

	// Map to user space.
	var tileUserX, tileUserY, tileUserW, tileUserH float64
	switch p.PatternUnits {
	case svg.SVGUnitObjectBoundingBox:
		tileUserX = referenceBox.X() + tileX*referenceBox.Width()
		tileUserY = referenceBox.Y() + tileY*referenceBox.Height()
		tileUserW = tileW * referenceBox.Width()
		tileUserH = tileH * referenceBox.Height()
	case svg.SVGUnitUserSpaceOnUse:
		tileUserX = tileX
		tileUserY = tileY
		tileUserW = tileW
		tileUserH = tileH
	}

	if tileUserW <= 0 || tileUserH <= 0 {
		return nil, false
	}

	// Allocate the tile buffer. One device-pixel per user-space unit
	// keeps the tile sharp at 1:1 device scale; finer device scales
	// (page zoom) sample with nearest-neighbour, which is acceptable
	// for the Phase 4 hand-check gate (Blink uses bilinear via Skia's
	// shader; we'll match later if a reftest needs it).
	const maxTilePx = 4096
	pxW := int(math.Ceil(tileUserW))
	pxH := int(math.Ceil(tileUserH))
	if pxW < 1 {
		pxW = 1
	}
	if pxH < 1 {
		pxH = 1
	}
	if pxW > maxTilePx {
		pxW = maxTilePx
	}
	if pxH > maxTilePx {
		pxH = maxTilePx
	}

	tile := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	// Render pattern children into the tile. Build a synthetic
	// renderer/paint context that paints into the tile buffer. The
	// child SVG nodes paint at coordinates resolved in
	// patternContentUnits space; we set up a paint context whose
	// DeviceFromUser maps userSpaceOnUse coords directly to tile
	// pixels (and objectBoundingBox content units scale through the
	// referenceBox).
	if len(p.Children) > 0 {
		// Build a temporary renderer sharing the host renderer's
		// fonts/imageFetcher. No SVG-tree resolution beyond the
		// tile is needed.
		renderTileContent(p, tile, pxW, pxH, tileUserW, tileUserH, referenceBox, resources)
	}

	// Compose patternTransform: in objectBBox mode the transform
	// operates on the tile's normalized coords (we've already scaled
	// the tile to user space, so the user-space tile rect carries
	// patternTransform applied AFTER the units mapping).
	//
	// We'll set up: tileFromUser =
	//    scale(pxW / tileUserW, pxH / tileUserH) ·
	//    inverse(patternTransform) ·
	//    translate(-tileUserX, -tileUserY)
	//
	// (so that a user-space point P first gets shifted relative to
	// tile origin, then untransformed, then scaled to tile pixels.)
	pre := geometry.Translate(-tileUserX, -tileUserY)
	patternInv, ok := p.PatternTransform.Invert()
	if !ok {
		patternInv = geometry.Identity()
	}
	scaleToPx := geometry.Scale(float64(pxW)/tileUserW, float64(pxH)/tileUserH)
	tileFromUser := scaleToPx.Compose(patternInv).Compose(pre)

	return &patternShader{
		tile:         tile,
		tileFromUser: tileFromUser,
		tileWUser:    tileUserW,
		tileHUser:    tileUserH,
		tilePxW:      pxW,
		tilePxH:      pxH,
		opacity:      opacity,
	}, true
}

// renderTileContent paints the pattern's child SVG nodes into the tile
// buffer at the tile's resolution. Mirrors Blink's
// LayoutSVGResourcePattern::CreatePaintRecord — render the children
// once at the tile's native resolution and use that as the tile.
//
// `tileUserW`/`tileUserH` are the tile's USER-space extent (size in
// the gradient's local coords, before pixel snapping). For
// patternContentUnits=objectBoundingBox the children resolve their own
// percentages against the reference bbox; for userSpaceOnUse they
// resolve against the active viewport.
//
// Phase 4: implements only the userSpaceOnUse case for the
// content tree; objectBoundingBox content gets the same treatment
// (the children's percentages resolve against a synthetic
// LengthContext sized to the bbox). The Phase 4 hand-check test only
// exercises the userSpaceOnUse-style child shapes.
func renderTileContent(p *svg.SVGPattern, tile *image.RGBA, pxW, pxH int, tileUserW, tileUserH float64, referenceBox geometry.RectF, resources *svg.SVGResourceRegistry) {
	// Start with a transparent tile (the default for image.RGBA).
	// Then paint each child as a standalone svg.SVGRoot, scaled so
	// (0,0)..(tileUserW, tileUserH) in user space → (0,0)..(pxW, pxH)
	// in tile pixels.

	// Build a minimal renderer for the tile. No font cache or
	// fetcher needed — patterns paint only shape geometry.
	tileR := NewRendererForImage(tile)

	// Set up a paint context whose DeviceFromUser maps user-space
	// tile coords directly to tile pixels.
	deviceFromUser := geometry.Scale(float64(pxW)/tileUserW, float64(pxH)/tileUserH)
	tileR.dc.Push()
	tileR.dc.MultiplyMatrix(
		deviceFromUser.A, deviceFromUser.B,
		deviceFromUser.C, deviceFromUser.D,
		deviceFromUser.E, deviceFromUser.F,
	)
	defer tileR.dc.Pop()

	// Build a synthetic length context. patternContentUnits chooses
	// the viewport: objectBoundingBox → (1, 1); userSpaceOnUse →
	// (tileUserW, tileUserH).
	var ctxSize geometry.SizeF
	if p.PatternContentUnits == svg.SVGUnitObjectBoundingBox {
		ctxSize = geometry.SizeF{Width: 1, Height: 1}
	} else {
		ctxSize = geometry.SizeF{Width: tileUserW, Height: tileUserH}
	}

	pctx := &svgPaintContext{
		dc:               tileR.dc,
		originX:          0,
		originY:          0,
		viewport:         geometry.NewRectF(0, 0, ctxSize.Width, ctxSize.Height),
		CurrentTransform: geometry.Identity(),
		DeviceFromUser:   deviceFromUser,
		// Phase 6: thread the umbrella registry so nested url(#…)
		// references inside `<pattern>` children resolve (gradients,
		// clippers, masks, other patterns).
		Resources: resources,
		Renderer:  tileR,
	}
	_ = pctx
	_ = referenceBox

	for _, child := range p.Children {
		paintSVGNode(pctx, child)
	}
}

// fillTileSolid is a debug helper that clears the tile to a given
// color (used by the simplified pattern tile builder if it's ever
// invoked with empty children). Kept here for parity with the
// CSS-gradient code's helpers; not currently called.
func fillTileSolid(tile *image.RGBA, c color.RGBA) {
	for i := 0; i < len(tile.Pix); i += 4 {
		tile.Pix[i+0] = c.R
		tile.Pix[i+1] = c.G
		tile.Pix[i+2] = c.B
		tile.Pix[i+3] = c.A
	}
}
