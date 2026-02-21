package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/fogleman/gg"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
)

type Renderer struct {
	context      *gg.Context
	scrollY      float64              // Viewport scroll offset - non-fixed content is shifted by -scrollY
	imageFetcher images.ImageFetcher  // Optional fetcher for network images
	fonts        text.FontConfig      // Font configuration for text rendering
	lastFontKey  string               // Tracks loaded font to avoid redundant loads
}

func NewRenderer(width, height int) *Renderer {
	return &Renderer{
		context: gg.NewContext(width, height),
		fonts:   text.DefaultFontConfig(),
	}
}

// NewRendererForImage creates a renderer that draws onto the provided RGBA image.
// The viewport dimensions are derived from the image bounds.
func NewRendererForImage(target *image.RGBA) *Renderer {
	return &Renderer{
		context: gg.NewContextForRGBA(target),
		fonts:   text.DefaultFontConfig(),
	}
}

// SetFonts sets the font configuration used for text rendering.
func (r *Renderer) SetFonts(fonts text.FontConfig) {
	r.fonts = fonts
}

// SetImageFetcher sets the image fetcher used to load network images during rendering.
func (r *Renderer) SetImageFetcher(fetcher images.ImageFetcher) {
	r.imageFetcher = fetcher
}

// loadFont loads a font face on the gg context for the given size and style.
// Skips reloading if the same font+size is already active.
func (r *Renderer) loadFont(fontSize float64, bold, italic, mono, ahem bool) {
	r.loadFontForFamily("", fontSize, bold, italic, mono, ahem)
}

// loadFontForFamily loads a font face, checking the web font registry for the given family first.
func (r *Renderer) loadFontForFamily(family string, fontSize float64, bold, italic, mono, ahem bool) {
	fontPath := r.fonts.FontPathForFamily(family, bold, italic, mono, ahem)
	key := fmt.Sprintf("%s@%.1f", fontPath, fontSize)
	if key == r.lastFontKey {
		return
	}
	if err := r.context.LoadFontFace(fontPath, fontSize); err == nil {
		r.lastFontKey = key
	}
}

// popContext restores the gg context state and invalidates the font cache.
// gg's Push/Pop saves and restores the entire context struct, including the
// font face. But the Renderer's lastFontKey is not part of the gg state,
// so after Pop the cached key may reference a font that was loaded inside
// the Push/Pop pair and is no longer the active face. Clearing the key
// forces the next loadFont call to actually reload the font.
func (r *Renderer) popContext() {
	r.context.Pop()
	r.lastFontKey = ""
}

// SetScrollY sets the viewport scroll offset for rendering.
// Non-fixed content will be shifted up by this amount.
// Fixed-positioned content remains at its absolute position.
func (r *Renderer) SetScrollY(scrollY float64) {
	r.scrollY = scrollY
}

// Render renders boxes using tree-based paint order (CSS 2.1 Appendix E).
// This maintains proper parent-child relationships while respecting z-index stacking.
// Fixed elements are painted in their natural tree order (not extracted and painted last).
// This matches modern browser behavior where position:fixed creates a stacking context.
func (r *Renderer) Render(boxes []*layout.Box) {
	r.context.SetRGB(1, 1, 1)
	r.context.Clear()

	// CSS 2.1 §14.2: Background propagation to canvas
	// If html has no background, propagate body's background to fill viewport
	r.drawCanvasBackground(boxes)

	// Render each root box as a stacking context (the root always forms one)
	// This ensures proper CSS 2.1 Appendix E paint order for the entire document
	for _, box := range boxes {
		r.paintStackingContext(box)
	}
}

// drawCanvasBackground implements CSS 2.1 §14.2 background propagation.
// If html has no background, body's background propagates to fill the viewport canvas.
func (r *Renderer) drawCanvasBackground(boxes []*layout.Box) {
	if len(boxes) == 0 {
		return
	}

	// Find the html box (root element)
	var htmlBox *layout.Box
	for _, box := range boxes {
		if box.Node != nil && box.Node.TagName == "html" {
			htmlBox = box
			break
		}
	}
	if htmlBox == nil {
		return
	}

	// Check if html has a background color
	htmlHasBg := false
	if htmlBox.Style != nil {
		if bgColor, ok := htmlBox.Style.Get("background-color"); ok {
			if color, ok := css.ParseColor(bgColor); ok && color.A > 0 {
				// Html has background - use it for canvas
				htmlHasBg = true
				width := float64(r.context.Width())
				height := float64(r.context.Height())
				r.context.SetRGBA(
					float64(color.R)/255.0,
					float64(color.G)/255.0,
					float64(color.B)/255.0,
					color.A)
				r.context.DrawRectangle(0, 0, width, height)
				r.context.Fill()
			}
		}
	}

	// If html has no background, propagate body's background to canvas
	if !htmlHasBg {
		// Find body element (first child of html with TagName == "body")
		var bodyBox *layout.Box
		for _, child := range htmlBox.Children {
			if child.Node != nil && child.Node.TagName == "body" {
				bodyBox = child
				break
			}
		}
		if bodyBox != nil && bodyBox.Style != nil {
			if bgColor, ok := bodyBox.Style.Get("background-color"); ok {
				if color, ok := css.ParseColor(bgColor); ok && color.A > 0 {
					// Body has background - propagate to canvas (fill viewport)
					width := float64(r.context.Width())
					height := float64(r.context.Height())
					r.context.SetRGBA(
						float64(color.R)/255.0,
						float64(color.G)/255.0,
						float64(color.B)/255.0,
						color.A)
					r.context.DrawRectangle(0, 0, width, height)
					r.context.Fill()
				}
			}
		}
	}
}

// paintStackingContext paints a box that creates a stacking context,
// following CSS 2.1 Appendix E paint order for ALL descendants.
func (r *Renderer) paintStackingContext(box *layout.Box) {
	if box == nil {
		return
	}

	// CSS Color Module Level 3: opacity creates a stacking context and composites
	// the element and all its descendants as a single offscreen buffer.
	opacity := 1.0
	if box.Style != nil {
		opacity = box.Style.GetOpacity()
	}

	if opacity <= 0 {
		return // Fully transparent — skip all rendering
	}

	if opacity < 1.0 {
		// Render to an offscreen buffer, then composite with reduced alpha
		r.paintWithOpacity(box, opacity)
		return
	}

	// CSS Filter Effects: render to offscreen buffer, apply filter, composite
	if box.Style != nil {
		if filters := box.Style.GetFilter(); len(filters) > 0 {
			r.paintWithFilter(box, filters)
			return
		}
	}

	// CSS Compositing: mix-blend-mode renders to offscreen buffer, blends with backdrop
	if box.Style != nil {
		blendMode := box.Style.GetMixBlendMode()
		if blendMode != css.MixBlendModeNormal {
			r.paintWithBlendMode(box, blendMode)
			return
		}
	}

	// CSS Masking: mask-image renders to offscreen buffer, applies mask alpha
	if box.Style != nil {
		maskValue := box.Style.GetMaskImage()
		if maskValue != "" && maskValue != "none" {
			r.paintWithMask(box, maskValue)
			return
		}
	}

	// CSS Compositing §4.2: Stacking contexts create isolated groups.
	// This ensures mix-blend-mode children blend against parent content,
	// not the full canvas below. Only isolate when blend-mode descendants exist.
	needsIsolation := layout.BoxCreatesStackingContext(box) && hasBlendModeDescendant(box)
	if needsIsolation {
		r.context.PushGroup()
	}

	// Apply clip-path if set (clips everything including backgrounds/borders)
	hasClipPath := false
	if box.Style != nil {
		if cp := box.Style.GetClipPath(); cp != nil {
			resolved := cp.ResolveClipPath(box.Width, box.Height)
			r.context.Push()
			hasClipPath = true
			r.applyClipPath(box, resolved)
		}
	}

	// Apply CSS transforms at the stacking context level so they wrap
	// all painting (backgrounds, borders, AND children)
	hasTransform := false
	if box.Style != nil {
		// Build combined transform list: individual properties (translate → rotate → scale)
		// applied before the transform shorthand, per CSS Transforms Level 2.
		var allTransforms []css.Transform
		if tx, ty, ok := box.Style.GetIndividualTranslate(); ok {
			allTransforms = append(allTransforms, css.Transform{Type: "translate", Values: []float64{tx, ty}})
		}
		if deg, ok := box.Style.GetIndividualRotate(); ok {
			allTransforms = append(allTransforms, css.Transform{Type: "rotate", Values: []float64{deg}})
		}
		if sx, sy, ok := box.Style.GetIndividualScale(); ok {
			allTransforms = append(allTransforms, css.Transform{Type: "scale", Values: []float64{sx, sy}})
		}
		allTransforms = append(allTransforms, box.Style.GetTransforms()...)
		if len(allTransforms) > 0 {
			r.context.Push()
			hasTransform = true
			r.applyTransforms(box, allTransforms)
		}
	}

	// CSS Backdrop Filter: apply filter to pixels already on canvas (behind the element)
	// This must happen BEFORE drawing the element's own background/content
	if box.Style != nil {
		if bdFilters := box.Style.GetBackdropFilter(); len(bdFilters) > 0 {
			r.applyBackdropFilter(box, bdFilters)
		}
	}

	// Step 1: Background and borders of this element
	r.drawBoxBackgroundAndBorders(box)

	// Check if we need to clip overflow (per-axis)
	needsClipX := false
	needsClipY := false
	if box.Style != nil {
		oxType := box.Style.GetOverflowX()
		oyType := box.Style.GetOverflowY()
		needsClipX = (oxType != css.OverflowVisible)
		needsClipY = (oyType != css.OverflowVisible)
	}
	needsClip := needsClipX || needsClipY

	// Apply clipping if any axis has non-visible overflow
	if needsClip {
		r.context.Push()

		// Per-axis clipping: clip only on axes with non-visible overflow.
		// Unclipped axes extend to the full canvas so overflow remains visible.
		canvasW := float64(r.context.Width())
		canvasH := float64(r.context.Height())

		clipX := 0.0
		clipY := 0.0
		clipW := canvasW
		clipH := canvasH

		// Clip to padding box on each axis that needs clipping
		if needsClipX {
			clipX = box.X + box.Border.Left
			clipW = box.Width - box.Border.Left - box.Border.Right
		}
		if needsClipY {
			clipY = box.Y + box.Border.Top
			clipH = box.Height - box.Border.Top - box.Border.Bottom
		}

		// Use rounded clip path when border-radius is set
		var corners css.BorderRadiusCorners
		if box.Style != nil {
			corners = box.Style.GetBorderRadiusCorners()
		}
		if corners.MaxRadius() > 0 {
			// Reduce each corner radius by border width for inner (padding box) clipping
			clampZero := func(v float64) float64 {
				if v < 0 {
					return 0
				}
				return v
			}
			r.context.DrawRoundedRectangleCorners(clipX, clipY, clipW, clipH,
				clampZero(corners.TopLeft-box.Border.Left),
				clampZero(corners.TopRight-box.Border.Right),
				clampZero(corners.BottomRight-box.Border.Right),
				clampZero(corners.BottomLeft-box.Border.Left))
		} else {
			r.context.DrawRectangle(clipX, clipY, clipW, clipH)
		}
		r.context.Clip()
	}

	// Collect ALL descendants, categorized by paint order
	var negativeZ, zeroAutoZ, positiveZ []*layout.Box
	var blocks, floats, inlines []*layout.Box

	r.collectDescendantsForPaintOrder(box, &negativeZ, &blocks, &floats, &inlines, &zeroAutoZ, &positiveZ)

	// CSS 2.1 Appendix E: If this box is a positioned element with z-index:auto
	// (does NOT create a real stacking context), all positioned and stacking
	// context descendants have been promoted to the parent stacking context.
	// Clear all z-index lists to prevent double-painting.
	// Exception: if overflow != visible, descendants were NOT promoted (they
	// must stay within the overflow clip), so keep the lists.
	isZAutoPositioned := layout.IsPositioned(box) && !layout.BoxCreatesStackingContext(box)
	hasOverflowClip := hasNonVisibleOverflow(box)
	// Only clear descendants if they were actually promoted to a parent stacking
	// context. When box.Parent is nil, this is a root box and there's no parent
	// to have promoted to — descendants must be painted here.
	if isZAutoPositioned && !hasOverflowClip && box.Parent != nil {
		negativeZ = nil
		positiveZ = nil
		zeroAutoZ = nil
	}

	// Sort z-index groups
	sort.SliceStable(negativeZ, func(i, j int) bool {
		return negativeZ[i].ZIndex < negativeZ[j].ZIndex
	})
	sort.SliceStable(positiveZ, func(i, j int) bool {
		return positiveZ[i].ZIndex < positiveZ[j].ZIndex
	})

	// Step 2: Child stacking contexts with negative z-index
	for _, child := range negativeZ {
		r.paintStackingContext(child)
	}

	// Step 3: In-flow, non-positioned, block-level descendants (backgrounds/borders)
	for _, child := range blocks {
		if hasNonVisibleOverflow(child) {
			r.paintStackingContext(child) // Paint atomically with clipping
		} else {
			r.drawBoxBackgroundAndBorders(child)
		}
	}

	// Step 4: Non-positioned floats
	// Floats are painted with their own internal paint order (like a mini stacking context)
	for _, child := range floats {
		r.paintStackingContext(child)
	}

	// Step 5: In-flow, inline-level descendants (content paints here)
	// This includes inline elements AND content of block elements
	for _, child := range inlines {
		r.drawBoxBackgroundAndBorders(child)
		r.drawBoxContent(child)
	}

	// Also paint content of blocks at step 5 (text/images inside blocks)
	for _, child := range blocks {
		if hasNonVisibleOverflow(child) {
			continue // Already painted atomically in step 3
		}
		r.drawBoxContent(child)
	}

	// Paint this box's own content
	r.drawBoxContent(box)

	// Step 6: Positioned descendants with z-index: auto or 0
	// These are painted "as if they generated a new stacking context" (CSS 2.1 Appendix E)
	for _, child := range zeroAutoZ {
		r.paintStackingContext(child)
	}

	// Step 7: Child stacking contexts with positive z-index
	for _, child := range positiveZ {
		r.paintStackingContext(child)
	}

	// Restore clipping state if we applied clipping
	if needsClip {
		r.popContext()
	}

	// Restore transform state
	if hasTransform {
		r.popContext()
	}

	// Composite isolated stacking context group back onto parent surface
	if needsIsolation {
		r.context.PopGroupWithAlpha(1.0)
	}

	// Restore clip-path state
	if hasClipPath {
		r.popContext()
	}
}

// applyClipPath applies a CSS clip-path shape as a gg clipping region.
// Coordinates in the resolved ClipPath are relative to the element's border box.
func (r *Renderer) applyClipPath(box *layout.Box, cp *css.ClipPath) {
	ox, oy := box.X, box.Y
	switch cp.Type {
	case css.ClipPathCircle:
		r.context.DrawCircle(ox+cp.Cx, oy+cp.Cy, cp.Radius)
	case css.ClipPathEllipse:
		r.context.DrawEllipse(ox+cp.Cx, oy+cp.Cy, cp.Rx, cp.Ry)
	case css.ClipPathPolygon:
		if len(cp.Points) >= 4 {
			r.context.NewSubPath()
			r.context.MoveTo(ox+cp.Points[0], oy+cp.Points[1])
			for i := 2; i < len(cp.Points)-1; i += 2 {
				r.context.LineTo(ox+cp.Points[i], oy+cp.Points[i+1])
			}
			r.context.ClosePath()
		}
	}
	r.context.Clip()
}

// paintWithOpacity renders a stacking context to an offscreen buffer, then
// composites it onto the main canvas with the specified opacity.
func (r *Renderer) paintWithOpacity(box *layout.Box, opacity float64) {
	// Use gg's group compositing: redirect drawing to offscreen buffer,
	// render the stacking context, then composite back with opacity.
	r.context.PushGroup()

	// Temporarily set opacity to 1 so the recursive call doesn't re-enter this path.
	origOpacity := box.Style.GetOpacity()
	box.Style.Set("opacity", "1")
	r.paintStackingContext(box)
	box.Style.Set("opacity", fmt.Sprintf("%g", origOpacity))

	r.context.PopGroupWithAlpha(opacity)
}

// paintWithFilter renders to offscreen buffer, applies CSS filter functions, composites back.
func (r *Renderer) paintWithFilter(box *layout.Box, filters []css.FilterFunction) {
	// Temporarily remove the filter to prevent re-entry
	origFilter, _ := box.Style.Get("filter")
	box.Style.Set("filter", "none")

	r.context.PushGroup()
	r.paintStackingContext(box)

	// Build composite filter function
	// Note: gg uses image.RGBA (premultiplied alpha). We must un-premultiply
	// before applying filters, then re-premultiply.
	r.context.PopGroupWithPixelFilter(func(red, green, blue, alpha uint8) (uint8, uint8, uint8, uint8) {
		if alpha == 0 {
			return 0, 0, 0, 0
		}
		// Un-premultiply
		a := float64(alpha) / 255
		fr, fg, fb := float64(red)/a/255, float64(green)/a/255, float64(blue)/a/255
		fa := a

		for _, f := range filters {
			switch f.Name {
			case "opacity":
				fa *= f.Value
			case "contrast":
				fr = (fr-0.5)*f.Value + 0.5
				fg = (fg-0.5)*f.Value + 0.5
				fb = (fb-0.5)*f.Value + 0.5
			case "grayscale":
				amt := f.Value
				if amt > 1 {
					amt = 1
				}
				gray := 0.2126*fr + 0.7152*fg + 0.0722*fb
				fr = fr*(1-amt) + gray*amt
				fg = fg*(1-amt) + gray*amt
				fb = fb*(1-amt) + gray*amt
			case "brightness":
				fr *= f.Value
				fg *= f.Value
				fb *= f.Value
			case "saturate":
				gray := 0.2126*fr + 0.7152*fg + 0.0722*fb
				fr = gray + (fr-gray)*f.Value
				fg = gray + (fg-gray)*f.Value
				fb = gray + (fb-gray)*f.Value
			case "sepia":
				amt := f.Value
				if amt > 1 {
					amt = 1
				}
				sr := 0.393*fr + 0.769*fg + 0.189*fb
				sg := 0.349*fr + 0.686*fg + 0.168*fb
				sb := 0.272*fr + 0.534*fg + 0.131*fb
				fr = fr*(1-amt) + sr*amt
				fg = fg*(1-amt) + sg*amt
				fb = fb*(1-amt) + sb*amt
			case "invert":
				amt := f.Value
				if amt > 1 {
					amt = 1
				}
				fr = fr*(1-amt) + (1-fr)*amt
				fg = fg*(1-amt) + (1-fg)*amt
				fb = fb*(1-amt) + (1-fb)*amt
			}
		}

		clamp := func(v float64) uint8 {
			if v < 0 { return 0 }
			if v > 255 { return 255 }
			return uint8(v)
		}
		// Re-premultiply
		outA := fa
		return clamp(fr * outA * 255), clamp(fg * outA * 255), clamp(fb * outA * 255), clamp(outA * 255)
	})

	box.Style.Set("filter", origFilter)
}

// paintWithBlendMode renders to offscreen buffer, composites with custom blend mode.
func (r *Renderer) paintWithBlendMode(box *layout.Box, blendMode css.MixBlendMode) {
	// Temporarily remove blend mode to prevent re-entry
	origBlend, _ := box.Style.Get("mix-blend-mode")
	box.Style.Set("mix-blend-mode", "normal")

	r.context.PushGroup()
	r.paintStackingContext(box)

	r.context.PopGroupWithBlend(func(sr, sg, sb, sa, dr, dg, db, da uint8) (uint8, uint8, uint8, uint8) {
		// Convert to float 0-1 range
		sfr, sfg, sfb := float64(sr)/255, float64(sg)/255, float64(sb)/255
		dfr, dfg, dfb := float64(dr)/255, float64(dg)/255, float64(db)/255
		sfa, dfa := float64(sa)/255, float64(da)/255

		var br, bg, bb float64

		switch blendMode {
		case css.MixBlendModeDifference:
			br = abs(dfr - sfr)
			bg = abs(dfg - sfg)
			bb = abs(dfb - sfb)
		case css.MixBlendModeMultiply:
			br = sfr * dfr
			bg = sfg * dfg
			bb = sfb * dfb
		case css.MixBlendModeScreen:
			br = sfr + dfr - sfr*dfr
			bg = sfg + dfg - sfg*dfg
			bb = sfb + dfb - sfb*dfb
		case css.MixBlendModeDarken:
			br = math.Min(sfr, dfr)
			bg = math.Min(sfg, dfg)
			bb = math.Min(sfb, dfb)
		case css.MixBlendModeLighten:
			br = math.Max(sfr, dfr)
			bg = math.Max(sfg, dfg)
			bb = math.Max(sfb, dfb)
		default:
			// Normal blend
			br, bg, bb = sfr, sfg, sfb
		}

		// Composite: Cs x αs x αb + Cb x αb x (1 – αs) (simplified Porter-Duff)
		oa := sfa + dfa*(1-sfa)
		if oa == 0 {
			return 0, 0, 0, 0
		}
		or := (br*sfa*dfa + dfr*dfa*(1-sfa)) / oa
		og := (bg*sfa*dfa + dfg*dfa*(1-sfa)) / oa
		ob := (bb*sfa*dfa + dfb*dfa*(1-sfa)) / oa

		// Also add source contribution outside backdrop
		or += sfr * sfa * (1 - dfa) / oa
		og += sfg * sfa * (1 - dfa) / oa
		ob += sfb * sfa * (1 - dfa) / oa

		clamp := func(v float64) uint8 {
			if v < 0 { return 0 }
			if v > 255 { return 255 }
			return uint8(v)
		}
		return clamp(or * 255), clamp(og * 255), clamp(ob * 255), clamp(oa * 255)
	})

	box.Style.Set("mix-blend-mode", origBlend)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// paintWithMask renders the element to an offscreen buffer and applies a CSS mask-image.
// For gradient masks, the alpha channel of the gradient controls element visibility.
// For url(#id) masks, references an inline SVG <mask> element.
func (r *Renderer) paintWithMask(box *layout.Box, maskValue string) {
	// Handle multiple mask layers (comma-separated) — use first layer
	firstMask := maskValue
	if idx := strings.Index(maskValue, ","); idx >= 0 {
		firstMask = strings.TrimSpace(maskValue[:idx])
	}

	// Check for url(#id) reference to SVG mask element
	if strings.HasPrefix(firstMask, "url(#") {
		maskID := strings.TrimPrefix(firstMask, "url(#")
		maskID = strings.TrimSuffix(maskID, ")")
		r.paintWithSVGMask(box, maskID)
		return
	}

	// Parse gradient from mask-image value
	grad, ok := css.ParseLinearGradient(maskValue)
	if !ok {
		// Unresolvable mask (e.g., url(#nonexistent)) → treat as no mask per CSS Masking spec
		origMask, _ := box.Style.Get("mask-image")
		box.Style.Set("mask-image", "none")
		r.paintStackingContext(box)
		box.Style.Set("mask-image", origMask)
		return
	}

	// Temporarily remove mask-image to prevent re-entry
	origMask, _ := box.Style.Get("mask-image")
	box.Style.Set("mask-image", "none")

	// Prepare gradient for interpolation
	gradCopy := *grad
	gradCopy.ConvertPixelOffsetsToPercentages(box.Width, box.Height)

	// Compute gradient line endpoints relative to box
	var x0, y0, x1, y1 float64
	switch gradCopy.Direction {
	case "to right":
		x0, y0 = box.X, box.Y
		x1, y1 = box.X+box.Width, box.Y
	case "to left":
		x0, y0 = box.X+box.Width, box.Y
		x1, y1 = box.X, box.Y
	case "to bottom", "":
		x0, y0 = box.X, box.Y
		x1, y1 = box.X, box.Y+box.Height
	case "to top":
		x0, y0 = box.X, box.Y+box.Height
		x1, y1 = box.X, box.Y
	default:
		x0, y0 = box.X, box.Y
		x1, y1 = box.X, box.Y+box.Height
	}

	// Gradient length
	dx, dy := x1-x0, y1-y0
	gradLen := dx*dx + dy*dy

	r.context.PushGroup()
	r.paintStackingContext(box)

	// Apply mask: modulate alpha by gradient alpha at each pixel position
	r.context.PopGroupWithPositionFilter(func(px, py int, cr, cg, cb, ca uint8) (uint8, uint8, uint8, uint8) {
		if ca == 0 {
			return 0, 0, 0, 0
		}

		// Project pixel position onto gradient line to get t parameter
		var t float64
		if gradLen > 0 {
			t = (float64(px)-x0)*dx/gradLen + (float64(py)-y0)*dy/gradLen
		}
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}

		// Interpolate gradient alpha at position t
		maskAlpha := interpolateGradientAlpha(gradCopy.ColorStops, t)

		// Multiply pixel alpha by mask alpha (premultiplied alpha)
		scale := maskAlpha
		return uint8(float64(cr) * scale), uint8(float64(cg) * scale),
			uint8(float64(cb) * scale), uint8(float64(ca) * scale)
	})

	box.Style.Set("mask-image", origMask)
}

// paintWithSVGMask renders the element with an SVG <mask> element applied.
// The mask's children are rendered to a luminance buffer, and the luminance
// at each pixel controls the element's visibility.
func (r *Renderer) paintWithSVGMask(box *layout.Box, maskID string) {
	// Find the mask element by traversing the DOM from the document root
	docRoot := findDocumentRoot(box.Node)
	maskNode := findElementByID(docRoot, maskID)
	if maskNode == nil {
		// Unresolvable mask → render normally (no mask)
		origMask, _ := box.Style.Get("mask-image")
		box.Style.Set("mask-image", "none")
		r.paintStackingContext(box)
		box.Style.Set("mask-image", origMask)
		return
	}

	// Temporarily remove mask to prevent re-entry
	origMask, _ := box.Style.Get("mask-image")
	box.Style.Set("mask-image", "none")

	// Determine mask dimensions (from mask element or element dimensions)
	maskW := box.Width
	maskH := box.Height
	if wAttr, ok := maskNode.GetAttribute("width"); ok {
		if w, ok := parseSVGLength(wAttr); ok {
			maskW = w
		}
	}
	if hAttr, ok := maskNode.GetAttribute("height"); ok {
		if h, ok := parseSVGLength(hAttr); ok {
			maskH = h
		}
	}

	// Render mask children to a luminance buffer
	maskImg := image.NewRGBA(image.Rect(0, 0, int(maskW), int(maskH)))
	maskCtx := gg.NewContextForRGBA(maskImg)

	for _, child := range maskNode.Children {
		if child.Type != html.ElementNode {
			continue
		}
		if child.TagName == "rect" {
			// Parse rect attributes
			rx, ry, rw, rh := 0.0, 0.0, 0.0, 0.0
			if val, ok := child.GetAttribute("x"); ok {
				if v, ok := parseSVGLength(val); ok {
					rx = v
				}
			}
			if val, ok := child.GetAttribute("y"); ok {
				if v, ok := parseSVGLength(val); ok {
					ry = v
				}
			}
			if val, ok := child.GetAttribute("width"); ok {
				if v, ok := parseSVGLength(val); ok {
					rw = v
				}
			}
			if val, ok := child.GetAttribute("height"); ok {
				if v, ok := parseSVGLength(val); ok {
					rh = v
				}
			}
			fc := getSVGFillColor(child)
			// Premultiplied alpha: scale RGB by A
			a := fc.A
			maskCtx.SetRGBA(float64(fc.R)/255.0*a, float64(fc.G)/255.0*a, float64(fc.B)/255.0*a, a)
			maskCtx.DrawRectangle(rx, ry, rw, rh)
			maskCtx.Fill()
		}
	}

	// Render the element to an offscreen buffer
	r.context.PushGroup()
	r.paintStackingContext(box)

	// Apply mask: use luminance of mask pixels as alpha multiplier
	r.context.PopGroupWithPositionFilter(func(px, py int, cr, cg, cb, ca uint8) (uint8, uint8, uint8, uint8) {
		if ca == 0 {
			return 0, 0, 0, 0
		}

		// Map pixel position to mask coordinates (relative to element's border box)
		mx := px - int(box.X)
		my := py - int(box.Y)

		maskAlpha := 0.0
		if mx >= 0 && mx < int(maskW) && my >= 0 && my < int(maskH) {
			mc := maskImg.RGBAAt(mx, my)
			if mc.A > 0 {
				// CSS Masking: luminance mask — luminance = 0.2126*R + 0.7152*G + 0.0722*B
				// Un-premultiply first
				a := float64(mc.A) / 255
				fr := float64(mc.R) / a / 255
				fg := float64(mc.G) / a / 255
				fb := float64(mc.B) / a / 255
				maskAlpha = (0.2126*fr + 0.7152*fg + 0.0722*fb) * a
			}
		}

		// Multiply all channels by mask alpha (premultiplied alpha)
		return uint8(float64(cr) * maskAlpha), uint8(float64(cg) * maskAlpha),
			uint8(float64(cb) * maskAlpha), uint8(float64(ca) * maskAlpha)
	})

	box.Style.Set("mask-image", origMask)
}

// interpolateGradientAlpha computes the alpha value at position t (0-1) along a gradient.
func interpolateGradientAlpha(stops []css.ColorStop, t float64) float64 {
	if len(stops) == 0 {
		return 1.0
	}
	if t <= stops[0].Offset {
		return stops[0].Color.A
	}
	if t >= stops[len(stops)-1].Offset {
		return stops[len(stops)-1].Color.A
	}

	// Find the two stops that bracket t
	for i := 0; i < len(stops)-1; i++ {
		if t >= stops[i].Offset && t <= stops[i+1].Offset {
			// Linear interpolation between stops
			range_ := stops[i+1].Offset - stops[i].Offset
			if range_ <= 0 {
				return stops[i].Color.A
			}
			frac := (t - stops[i].Offset) / range_
			return stops[i].Color.A*(1-frac) + stops[i+1].Color.A*frac
		}
	}

	return stops[len(stops)-1].Color.A
}

// applyBackdropFilter applies CSS backdrop-filter effects to the pixels already on the canvas
// within the element's border-box region. This must be called BEFORE drawing the element's
// own background and content.
func (r *Renderer) applyBackdropFilter(box *layout.Box, filters []css.FilterFunction) {
	effectiveY := r.getEffectiveY(box)

	// Get the element's border-box bounds
	bx := int(math.Floor(box.X))
	by := int(math.Floor(effectiveY))
	bw := int(math.Ceil(box.Width + box.Padding.Left + box.Padding.Right + box.Border.Left + box.Border.Right))
	bh := int(math.Ceil(box.Height + box.Padding.Top + box.Padding.Bottom + box.Border.Top + box.Border.Bottom))

	// Get the current canvas image
	img, ok := r.context.Image().(*image.RGBA)
	if !ok {
		return
	}

	bounds := img.Bounds()
	maxX := bounds.Max.X
	maxY := bounds.Max.Y

	// Apply filters to each pixel in the backdrop region
	for py := by; py < by+bh && py < maxY; py++ {
		if py < 0 {
			continue
		}
		for px := bx; px < bx+bw && px < maxX; px++ {
			if px < 0 {
				continue
			}
			offset := img.PixOffset(px, py)
			// image.RGBA uses premultiplied alpha
			pr, pg, pb, pa := img.Pix[offset], img.Pix[offset+1], img.Pix[offset+2], img.Pix[offset+3]
			if pa == 0 {
				continue
			}

			// Un-premultiply
			a := float64(pa) / 255.0
			fr := float64(pr) / a / 255.0
			fg := float64(pg) / a / 255.0
			fb := float64(pb) / a / 255.0
			fa := a

			for _, f := range filters {
				switch f.Name {
				case "grayscale":
					amt := f.Value
					if amt > 1 {
						amt = 1
					}
					gray := 0.2126*fr + 0.7152*fg + 0.0722*fb
					fr = fr*(1-amt) + gray*amt
					fg = fg*(1-amt) + gray*amt
					fb = fb*(1-amt) + gray*amt
				case "brightness":
					fr *= f.Value
					fg *= f.Value
					fb *= f.Value
				case "contrast":
					fr = (fr-0.5)*f.Value + 0.5
					fg = (fg-0.5)*f.Value + 0.5
					fb = (fb-0.5)*f.Value + 0.5
				case "saturate":
					gray := 0.2126*fr + 0.7152*fg + 0.0722*fb
					fr = gray + (fr-gray)*f.Value
					fg = gray + (fg-gray)*f.Value
					fb = gray + (fb-gray)*f.Value
				case "opacity":
					fa *= f.Value
				case "invert":
					amt := f.Value
					if amt > 1 {
						amt = 1
					}
					fr = fr*(1-amt) + (1-fr)*amt
					fg = fg*(1-amt) + (1-fg)*amt
					fb = fb*(1-amt) + (1-fb)*amt
				case "sepia":
					amt := f.Value
					if amt > 1 {
						amt = 1
					}
					sr := 0.393*fr + 0.769*fg + 0.189*fb
					sg := 0.349*fr + 0.686*fg + 0.168*fb
					sb := 0.272*fr + 0.534*fg + 0.131*fb
					fr = fr*(1-amt) + sr*amt
					fg = fg*(1-amt) + sg*amt
					fb = fb*(1-amt) + sb*amt
				}
			}

			// Clamp values
			clamp01 := func(v float64) float64 {
				if v < 0 {
					return 0
				}
				if v > 1 {
					return 1
				}
				return v
			}
			fr = clamp01(fr)
			fg = clamp01(fg)
			fb = clamp01(fb)
			fa = clamp01(fa)

			// Re-premultiply
			img.Pix[offset] = uint8(fr * fa * 255.0)
			img.Pix[offset+1] = uint8(fg * fa * 255.0)
			img.Pix[offset+2] = uint8(fb * fa * 255.0)
			img.Pix[offset+3] = uint8(fa * 255.0)
		}
	}
}

// hasBlendModeDescendant checks if any descendant of box has mix-blend-mode set.
func hasBlendModeDescendant(box *layout.Box) bool {
	for _, child := range box.Children {
		if child.Style != nil {
			if child.Style.GetMixBlendMode() != css.MixBlendModeNormal {
				return true
			}
		}
		if hasBlendModeDescendant(child) {
			return true
		}
	}
	return false
}

// hasNonVisibleOverflow returns true if a box has any non-visible overflow
// on either axis. This handles per-axis overflow (e.g. overflow-x:clip with
// overflow-y:visible), not just the shorthand overflow property.
func hasNonVisibleOverflow(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	return box.Style.GetOverflowX() != css.OverflowVisible ||
		box.Style.GetOverflowY() != css.OverflowVisible
}

// collectDescendantsForPaintOrder recursively collects all descendants,
// categorizing them by paint order. Stops at child stacking contexts.
func (r *Renderer) collectDescendantsForPaintOrder(box *layout.Box,
	negativeZ, blocks, floats, inlines, zeroAutoZ, positiveZ *[]*layout.Box) {

	for _, child := range box.Children {
		if child.Position == css.PositionFixed {
			// Fixed elements create stacking contexts in modern browsers
			*zeroAutoZ = append(*zeroAutoZ, child)
		} else if layout.BoxCreatesStackingContext(child) {
			// Child creates stacking context - categorize by z-index
			if child.ZIndex < 0 {
				*negativeZ = append(*negativeZ, child)
			} else if child.ZIndex > 0 {
				*positiveZ = append(*positiveZ, child)
			} else {
				*zeroAutoZ = append(*zeroAutoZ, child)
			}
			// Don't recurse into stacking contexts - they paint atomically
		} else if layout.IsFloat(child) {
			// CSS 2.1 Appendix E Step 4: Non-positioned floats paint before inline content.
			// Note: the layout engine may set box.Position = PositionAbsolute on floats
			// as an internal marker (out-of-flow), but for paint order we must check
			// the CSS style position to determine if the float is truly positioned.
			cssPos := css.PositionStatic
			if child.Style != nil {
				cssPos = child.Style.GetPosition()
			}
			if cssPos == css.PositionRelative || cssPos == css.PositionAbsolute || cssPos == css.PositionFixed || cssPos == css.PositionSticky {
				// Truly positioned float → paint at Step 6 (positioned descendants)
				*zeroAutoZ = append(*zeroAutoZ, child)
			} else {
				// Non-positioned float → paint at Step 4
				*floats = append(*floats, child)
			}
			// Don't recurse into float children - floats paint atomically
		} else if layout.IsPositioned(child) {
			// Positioned but no stacking context (z-index: auto) - paint at step 6
			// "as if it generated a new stacking context" per CSS 2.1 Appendix E
			*zeroAutoZ = append(*zeroAutoZ, child)
			// CSS 2.1 Appendix E: positioned elements with z-index:auto don't
			// create stacking contexts. Their positioned descendants and
			// stacking context descendants participate in the PARENT stacking
			// context, not this element's.
			// Exception: don't promote if overflow != visible — the overflow clip
			// must contain all descendants.
			hasOverflowClipChild := hasNonVisibleOverflow(child)
			if !hasOverflowClipChild {
				r.promoteDescendantsToParentSC(child, negativeZ, zeroAutoZ, positiveZ)
			}
		} else if layout.IsInline(child) {
			*inlines = append(*inlines, child)
			// Recurse into inline's descendants (inline content is part of step 5)
			r.collectDescendantsForPaintOrder(child, negativeZ, blocks, floats, inlines, zeroAutoZ, positiveZ)
		} else if hasNonVisibleOverflow(child) {
			// Block with overflow clipping — paint atomically (don't flatten children)
			*blocks = append(*blocks, child)
		} else {
			// Block element
			*blocks = append(*blocks, child)
			// Recurse into block's descendants to find inline content for step 5
			r.collectDescendantsForPaintOrder(child, negativeZ, blocks, floats, inlines, zeroAutoZ, positiveZ)
		}
	}
}

// promoteDescendantsToParentSC finds positioned and stacking context descendants
// within a positioned element that has z-index:auto (no stacking context).
// Per CSS 2.1 Appendix E: "any positioned descendants and descendants which
// actually create a new stacking context should be considered part of the parent
// stacking context, not this new one."
func (r *Renderer) promoteDescendantsToParentSC(box *layout.Box,
	negativeZ, zeroAutoZ, positiveZ *[]*layout.Box) {
	for _, child := range box.Children {
		if child.Position == css.PositionFixed {
			*zeroAutoZ = append(*zeroAutoZ, child)
		} else if layout.BoxCreatesStackingContext(child) {
			if child.ZIndex < 0 {
				*negativeZ = append(*negativeZ, child)
			} else if child.ZIndex > 0 {
				*positiveZ = append(*positiveZ, child)
			} else {
				*zeroAutoZ = append(*zeroAutoZ, child)
			}
		} else if layout.IsPositioned(child) {
			// Positioned but no stacking context — also promoted
			*zeroAutoZ = append(*zeroAutoZ, child)
			// Recurse: this child also doesn't create a stacking context,
			// so its own positioned/SC descendants are also promoted
			r.promoteDescendantsToParentSC(child, negativeZ, zeroAutoZ, positiveZ)
		} else {
			// Non-positioned, non-stacking-context — recurse to find deeper descendants
			r.promoteDescendantsToParentSC(child, negativeZ, zeroAutoZ, positiveZ)
		}
	}
}

// RenderLegacy uses the old flat-list rendering approach (kept for comparison)
func (r *Renderer) RenderLegacy(boxes []*layout.Box) {
	r.context.SetRGB(1, 1, 1)
	r.context.Clear()

	allBoxes := r.collectAllBoxes(boxes)
	r.sortByZIndex(allBoxes)

	for _, box := range allBoxes {
		r.drawBox(box)
	}
}

// collectAllBoxes flattens the box tree into a single list
func (r *Renderer) collectAllBoxes(boxes []*layout.Box) []*layout.Box {
	result := make([]*layout.Box, 0)
	for _, box := range boxes {
		result = append(result, box)
		result = append(result, r.collectAllBoxes(box.Children)...)
	}
	return result
}

// paintLevel returns the CSS painting level for stacking order within the same z-index
func paintLevel(box *layout.Box) int {
	if box.Style == nil {
		return 0
	}
	if box.Style.GetFloat() != css.FloatNone {
		return 1
	}
	if disp, ok := box.Style.Get("display"); ok && disp == "inline" {
		return 2
	}
	return 0
}

// sortByZIndex sorts boxes by z-index and CSS painting order
func (r *Renderer) sortByZIndex(boxes []*layout.Box) {
	sort.SliceStable(boxes, func(i, j int) bool {
		if boxes[i].ZIndex != boxes[j].ZIndex {
			return boxes[i].ZIndex < boxes[j].ZIndex
		}
		return paintLevel(boxes[i]) < paintLevel(boxes[j])
	})
}

// getEffectiveY returns the Y coordinate adjusted for scroll offset.
// Fixed-positioned elements are not affected by scroll.
func (r *Renderer) getEffectiveY(box *layout.Box) float64 {
	if box.Position == css.PositionFixed {
		return box.Y // Fixed elements stay at their absolute position
	}
	return box.Y - r.scrollY // Non-fixed content is shifted up by scrollY
}

// backgroundClipCoords returns x, y, width, height for drawing a background
// based on the background-clip property value.
func backgroundClipCoords(box *layout.Box, effectiveY float64) (float64, float64, float64, float64) {
	clip := box.Style.GetBackgroundClip()
	switch clip {
	case css.BackgroundClipPaddingBox:
		return box.X + box.Border.Left,
			effectiveY + box.Border.Top,
			box.Width - box.Border.Left - box.Border.Right,
			box.Height - box.Border.Top - box.Border.Bottom
	case css.BackgroundClipContentBox:
		return box.X + box.Border.Left + box.Padding.Left,
			effectiveY + box.Border.Top + box.Padding.Top,
			box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right,
			box.Height - box.Border.Top - box.Border.Bottom - box.Padding.Top - box.Padding.Bottom
	default: // border-box
		return box.X, effectiveY, box.Width, box.Height
	}
}

// drawBoxBackgroundAndBorders draws only the background and borders of a box.
func (r *Renderer) drawBoxBackgroundAndBorders(box *layout.Box) {
	if box == nil || box.Style == nil {
		return
	}

	// CSS 2.1 §11.2: visibility:hidden elements are invisible but still occupy space
	if v := box.Style.GetVisibility(); v == "hidden" || v == "collapse" {
		return
	}

	// empty-cells: hide — skip background and border for empty table cells
	if box.HideBackground {
		return
	}

	// Note: transforms are applied at the stacking context level in paintStackingContext,
	// wrapping all painting (backgrounds, borders, AND children).

	// Draw box-shadow (underneath the box)
	r.drawBoxShadow(box)

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Check for gradient background first (shorthand "background" or longhand "background-image")
	hasGradient := false
	if bgValue, ok := box.Style.Get("background"); ok {
		if grad, ok := css.GetGradient(bgValue); ok {
			r.drawGradientBackground(box, grad, effectiveY)
			hasGradient = true
		}
	}
	if !hasGradient {
		if bgValue, ok := box.Style.Get("background-image"); ok {
			if grad, ok := css.GetGradient(bgValue); ok {
				r.drawGradientBackground(box, grad, effectiveY)
				hasGradient = true
			}
		}
	}

	// Draw background color (only if no gradient was drawn)
	if !hasGradient {
		if bgColor, ok := box.Style.Get("background-color"); ok {
			if color, ok := css.ParseColor(bgColor); ok && color.A > 0 {
				r.context.SetRGBA(
					float64(color.R)/255.0,
					float64(color.G)/255.0,
					float64(color.B)/255.0,
					color.A)

				bgX, bgY, bgWidth, bgHeight := backgroundClipCoords(box, effectiveY)

				// CRITICAL FIX: For inline elements, box.Height is the line box height
				// but borders/padding "bleed" outside the line box (CSS 2.1 §10.8.1)
				// We must extend the background to cover the full bleeding area
				if box.Style.GetDisplay() == css.DisplayInline && box.Style.GetBackgroundClip() == css.BackgroundClipBorderBox {
					// Add vertical borders and padding to line box height for rendering
					bgHeight = box.Height + box.Border.Top + box.Padding.Top + box.Padding.Bottom + box.Border.Bottom
					// Adjust Y position to account for top border/padding
					bgY -= box.Border.Top + box.Padding.Top
				}

				if bgWidth > 0 && bgHeight > 0 {
					if box.Style.HasEllipticalBorderRadius() {
						ec := box.Style.GetBorderRadiusCornersElliptical()
						halfW, halfH := bgWidth/2, bgHeight/2
						if ec[0].Rx == halfW && ec[0].Ry == halfH &&
							ec[1].Rx == halfW && ec[1].Ry == halfH &&
							ec[2].Rx == halfW && ec[2].Ry == halfH &&
							ec[3].Rx == halfW && ec[3].Ry == halfH {
							r.context.DrawEllipse(bgX+halfW, bgY+halfH, halfW, halfH)
						} else {
							r.context.DrawRoundedRectangleCornersElliptical(bgX, bgY, bgWidth, bgHeight,
								ec[0].Rx, ec[0].Ry, ec[1].Rx, ec[1].Ry,
								ec[2].Rx, ec[2].Ry, ec[3].Rx, ec[3].Ry)
						}
					} else {
						corners := box.Style.GetBorderRadiusCorners()
						if corners.MaxRadius() > 0 {
							r.context.DrawRoundedRectangleCorners(bgX, bgY, bgWidth, bgHeight,
								corners.TopLeft, corners.TopRight, corners.BottomRight, corners.BottomLeft)
						} else {
							r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
						}
					}
					r.context.Fill()
				}
			}
		}
	}

	// Draw background image
	r.drawBackgroundImage(box)

	// CSS spec: inset box-shadows are drawn above background, below border/content
	r.drawInsetBoxShadow(box)

	// Draw border
	r.drawBorder(box)

	// Draw outline (outside the border box, does not affect layout)
	r.drawOutline(box)

	// Draw column rules (for multi-column containers)
	r.drawColumnRules(box)
}

// drawColumnRules draws vertical rules between columns for multi-column containers.
func (r *Renderer) drawColumnRules(box *layout.Box) {
	if box == nil || box.Style == nil {
		return
	}
	ruleStyle := box.Style.GetColumnRuleStyle()
	if ruleStyle == "none" {
		return
	}
	ruleWidth := box.Style.GetColumnRuleWidth()
	if ruleWidth <= 0 {
		return
	}
	columnCount := box.Style.GetColumnCount()
	if columnCount <= 1 {
		return
	}
	ruleColor := box.Style.GetColumnRuleColor()
	if ruleColor.A <= 0 {
		return
	}

	effectiveY := r.getEffectiveY(box)
	contentX := box.X + box.Border.Left + box.Padding.Left
	contentW := box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right
	contentY := effectiveY + box.Border.Top + box.Padding.Top
	contentH := box.Height - box.Border.Top - box.Border.Bottom - box.Padding.Top - box.Padding.Bottom

	gapVal := box.Style.GetColumnGapMulticol()
	numCols := float64(columnCount)
	colWidth := (contentW - gapVal*float64(columnCount-1)) / numCols

	r.context.SetRGBA(
		float64(ruleColor.R)/255.0,
		float64(ruleColor.G)/255.0,
		float64(ruleColor.B)/255.0,
		ruleColor.A,
	)
	for i := 1; i < columnCount; i++ {
		// The gap starts at: contentX + i*colWidth + (i-1)*gap
		gapStart := contentX + float64(i)*colWidth + float64(i-1)*gapVal
		gapCenter := gapStart + gapVal/2
		ruleX := math.Floor(gapCenter - ruleWidth/2)
		r.context.DrawRectangle(ruleX, contentY, ruleWidth, contentH)
		r.context.Fill()
	}
}

// drawGradientBackground renders a CSS gradient as the box background
func (r *Renderer) drawGradientBackground(box *layout.Box, grad *css.Gradient, effectiveY float64) {
	if grad == nil {
		return
	}

	bgX := box.X
	bgY := effectiveY
	bgWidth := box.Width
	bgHeight := box.Height

	// Handle inline element bleeding (same as solid color backgrounds)
	if box.Style.GetDisplay() == css.DisplayInline {
		bgHeight = box.Height + box.Border.Top + box.Padding.Top + box.Padding.Bottom + box.Border.Bottom
		bgY -= box.Border.Top + box.Padding.Top
	}

	if bgWidth <= 0 || bgHeight <= 0 {
		return
	}

	// Apply background-size to constrain gradient dimensions
	bgSize := box.Style.GetBackgroundSize()
	gradWidth := bgWidth
	gradHeight := bgHeight
	if bgSize.Width > 0 {
		gradWidth = bgSize.Width
	}
	if bgSize.Height > 0 {
		gradHeight = bgSize.Height
	}

	// Apply background-position to offset gradient within the box
	pos := box.Style.GetBackgroundPosition()
	gradX := bgX + pos.ResolveX(bgWidth, gradWidth)
	gradY := bgY + pos.ResolveY(bgHeight, gradHeight)

	var ggGrad gg.Gradient

	if grad.Type == css.GradientConic {
		// Conic gradient: pixel-level rendering
		r.drawConicGradient(grad, bgX, bgY, bgWidth, bgHeight, box)
		return
	}

	if grad.Type == css.GradientLinear || grad.Type == css.GradientRepeatingLinear {
		// Convert pixel offsets to percentages based on gradient direction
		gradCopy := *grad
		gradCopy.ColorStops = make([]css.ColorStop, len(grad.ColorStops))
		copy(gradCopy.ColorStops, grad.ColorStops)
		gradCopy.ConvertPixelOffsetsToPercentages(gradWidth, gradHeight)

		if gradCopy.Repeating {
			gradCopy.ExtendRepeatingStops(gradWidth)
		}

		// Determine gradient start and end points based on direction
		var x0, y0, x1, y1 float64
		switch gradCopy.Direction {
		case "to right":
			x0, y0 = gradX, gradY
			x1, y1 = gradX+gradWidth, gradY
		case "to left":
			x0, y0 = gradX+gradWidth, gradY
			x1, y1 = gradX, gradY
		case "to bottom", "":
			x0, y0 = gradX, gradY
			x1, y1 = gradX, gradY+gradHeight
		case "to top":
			x0, y0 = gradX, gradY+gradHeight
			x1, y1 = gradX, gradY
		default:
			x0, y0 = gradX, gradY
			x1, y1 = gradX, gradY+gradHeight
		}

		ggGrad = gg.NewLinearGradient(x0, y0, x1, y1)

		for _, stop := range gradCopy.ColorStops {
			ggGrad.AddColorStop(stop.Offset, r.premultiplyColor(stop.Color))
		}
	} else if grad.Type == css.GradientRadial || grad.Type == css.GradientRepeatingRadial {
		gradCopy := *grad
		gradCopy.ColorStops = make([]css.ColorStop, len(grad.ColorStops))
		copy(gradCopy.ColorStops, grad.ColorStops)

		// Compute radii
		rx, ry := gradCopy.ComputeRadialRadii(gradWidth, gradHeight)
		if rx <= 0 && ry <= 0 {
			return
		}

		// Compute center in pixel coordinates
		cx := gradCopy.CenterX * gradWidth
		if gradCopy.CenterXPx >= 0 {
			cx = gradCopy.CenterXPx
		}
		cy := gradCopy.CenterY * gradHeight
		if gradCopy.CenterYPx >= 0 {
			cy = gradCopy.CenterYPx
		}

		// Convert pixel color stop offsets using the primary radius
		radius := rx
		if radius == 0 {
			radius = ry
		}
		gradCopy.ConvertRadialPixelOffsets(radius)

		if gradCopy.Repeating {
			gradCopy.ExtendRepeatingStops(radius)
		}

		// Translate center to absolute coordinates
		absCx := gradX + cx
		absCy := gradY + cy

		if rx == ry || ry == 0 {
			// Circular gradient
			ggGrad = gg.NewRadialGradient(absCx, absCy, 0, absCx, absCy, rx)
		} else {
			// Elliptical gradient
			ggGrad = gg.NewEllipticalRadialGradient(absCx, absCy, rx, ry)
		}

		for _, stop := range gradCopy.ColorStops {
			ggGrad.AddColorStop(stop.Offset, r.premultiplyColor(stop.Color))
		}
	} else {
		return
	}

	// Set the gradient as the fill pattern
	r.context.SetFillStyle(ggGrad)

	// Clip to box bounds, then draw gradient at positioned offset
	r.context.Push()
	corners := box.Style.GetBorderRadiusCorners()
	if corners.MaxRadius() > 0 {
		r.context.DrawRoundedRectangleCorners(bgX, bgY, bgWidth, bgHeight,
			corners.TopLeft, corners.TopRight, corners.BottomRight, corners.BottomLeft)
	} else {
		r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
	}
	r.context.Clip()
	r.context.DrawRectangle(gradX, gradY, gradWidth, gradHeight)
	r.context.Fill()
	r.popContext()
}

// drawConicGradient renders a conic-gradient using pixel-level rendering
func (r *Renderer) drawConicGradient(grad *css.Gradient, bgX, bgY, bgWidth, bgHeight float64, box *layout.Box) {
	centerX := bgX + grad.CenterX*bgWidth
	centerY := bgY + grad.CenterY*bgHeight
	if grad.CenterXPx >= 0 {
		centerX = bgX + grad.CenterXPx
	}
	if grad.CenterYPx >= 0 {
		centerY = bgY + grad.CenterYPx
	}

	fromAngleRad := grad.FromAngle * math.Pi / 180.0

	for py := int(bgY); py < int(bgY+bgHeight); py++ {
		for px := int(bgX); px < int(bgX+bgWidth); px++ {
			dx := float64(px) + 0.5 - centerX
			dy := float64(py) + 0.5 - centerY
			// Angle from top, clockwise
			angle := math.Atan2(dx, -dy)
			if angle < 0 {
				angle += 2 * math.Pi
			}
			// Apply from angle offset
			angle -= fromAngleRad
			if angle < 0 {
				angle += 2 * math.Pi
			}
			normalized := angle / (2 * math.Pi)
			normalized = math.Mod(normalized, 1.0)
			if normalized < 0 {
				normalized += 1.0
			}
			c := interpolateConicColor(grad.ColorStops, normalized)
			r.context.SetColor(color.RGBA{R: c.R, G: c.G, B: c.B, A: uint8(c.A * 255)})
			r.context.SetPixel(px, py)
		}
	}
}

func interpolateConicColor(stops []css.ColorStop, t float64) css.Color {
	if len(stops) == 0 {
		return css.Color{}
	}
	if t <= stops[0].Offset {
		return stops[0].Color
	}
	if t >= stops[len(stops)-1].Offset {
		return stops[len(stops)-1].Color
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].Offset {
			prev := stops[i-1]
			curr := stops[i]
			span := curr.Offset - prev.Offset
			if span <= 0 {
				return curr.Color
			}
			frac := (t - prev.Offset) / span
			return css.Color{
				R: uint8(float64(prev.Color.R)*(1-frac) + float64(curr.Color.R)*frac),
				G: uint8(float64(prev.Color.G)*(1-frac) + float64(curr.Color.G)*frac),
				B: uint8(float64(prev.Color.B)*(1-frac) + float64(curr.Color.B)*frac),
				A: prev.Color.A*(1-frac) + curr.Color.A*frac,
			}
		}
	}
	return stops[len(stops)-1].Color
}

// premultiplyColor converts a CSS color to a premultiplied alpha color.RGBA
func (r *Renderer) premultiplyColor(c css.Color) color.RGBA {
	a := c.A
	return color.RGBA{
		R: uint8(float64(c.R) * a),
		G: uint8(float64(c.G) * a),
		B: uint8(float64(c.B) * a),
		A: uint8(a * 255),
	}
}

// drawBoxContent draws the content of a box (text, images, scrollbars).
func (r *Renderer) drawBoxContent(box *layout.Box) {
	if box == nil || box.Style == nil {
		return
	}

	// CSS 2.1 §11.2: visibility:hidden elements are invisible but still occupy space
	if v := box.Style.GetVisibility(); v == "hidden" || v == "collapse" {
		return
	}

	// Note: transforms are applied at the stacking context level in paintStackingContext.

	// Draw SVG content
	if box.Node != nil && box.Node.TagName == "svg" {
		r.drawSVGContent(box)
	}

	// Draw image
	r.drawImage(box)

	// Draw text
	r.drawText(box)

	// Draw scrollbar indicators (only for overflow:scroll which always shows scrollbars;
	// overflow:auto only shows when content overflows, which we don't detect yet)
	overflow := box.Style.GetOverflow()
	if overflow == css.OverflowScroll {
		r.drawScrollbarIndicators(box)
	}
}

// drawBox draws a complete box (used by legacy renderer)
func (r *Renderer) drawBox(box *layout.Box) {
	// Note: transforms are applied at the stacking context level in paintStackingContext.

	// Phase 19: Apply opacity (wraps all drawing for this box)
	opacity := box.Style.GetOpacity()
	if opacity < 1.0 {
		r.context.Push()
		defer r.popContext()
	}

	// Phase 19: Draw box-shadow (drawn first, underneath the box)
	r.drawBoxShadow(box)

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Phase 2: Draw background (content + padding area, not including margin)
	if bgColor, ok := box.Style.Get("background-color"); ok {
		if color, ok := css.ParseColor(bgColor); ok && color.A > 0 {
			r.context.SetRGBA(
				float64(color.R)/255.0,
				float64(color.G)/255.0,
				float64(color.B)/255.0,
				color.A,
			)

			// CSS 2.1 §14.2.1: Background covers content + padding + border area
			// box.X/Y is the border-box edge (outside of border)
			bgX := box.X
			bgY := effectiveY
			bgWidth := box.Width // Border-box dimensions
			bgHeight := box.Height // Border-box dimensions

			if bgWidth > 0 && bgHeight > 0 {
				// Phase 12: Check for border-radius (including elliptical)
				if box.Style.HasEllipticalBorderRadius() {
					ec := box.Style.GetBorderRadiusCornersElliptical()
					// Optimize: if all corners form a complete ellipse, use DrawEllipse
					halfW, halfH := bgWidth/2, bgHeight/2
					if ec[0].Rx == halfW && ec[0].Ry == halfH &&
						ec[1].Rx == halfW && ec[1].Ry == halfH &&
						ec[2].Rx == halfW && ec[2].Ry == halfH &&
						ec[3].Rx == halfW && ec[3].Ry == halfH {
						r.context.DrawEllipse(bgX+halfW, bgY+halfH, halfW, halfH)
					} else {
						r.context.DrawRoundedRectangleCornersElliptical(bgX, bgY, bgWidth, bgHeight,
							ec[0].Rx, ec[0].Ry, ec[1].Rx, ec[1].Ry,
							ec[2].Rx, ec[2].Ry, ec[3].Rx, ec[3].Ry)
					}
				} else {
					borderRadius := box.Style.GetBorderRadius()
					if borderRadius > 0 {
						r.context.DrawRoundedRectangle(bgX, bgY, bgWidth, bgHeight, borderRadius)
					} else {
						r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
					}
				}
				r.context.Fill()
			}
		}
	}

	// Phase 24: Draw background image
	r.drawBackgroundImage(box)

	// CSS spec: inset box-shadows are drawn above background, below border/content
	r.drawInsetBoxShadow(box)

	// Phase 2: Draw border
	r.drawBorder(box)

	// Draw outline (outside the border box, does not affect layout)
	r.drawOutline(box)

	// Phase 21: overflow clipping
	overflow := box.Style.GetOverflow()

	// Phase 8: Draw image
	r.drawImage(box)

	// Draw text
	r.drawText(box)

	// Phase 21: Draw scrollbar indicators (only for overflow:scroll;
	// overflow:auto only shows when content overflows)
	if overflow == css.OverflowScroll {
		r.drawScrollbarIndicators(box)
	}
}

// getBorderSideColor returns the color for a specific border side
func (r *Renderer) getBorderSideColor(box *layout.Box, side string) (css.Color, bool) {
	// resolveCurrentColor resolves "currentcolor" to the element's color property
	resolveCurrentColor := func(colorStr string) (css.Color, bool) {
		if strings.EqualFold(colorStr, "currentcolor") {
			if c, ok := box.Style.Get("color"); ok {
				if color, ok := css.ParseColor(c); ok {
					return color, true
				}
			}
			// Default currentcolor is black
			return css.Color{R: 0, G: 0, B: 0, A: 1.0}, true
		}
		return css.ParseColor(colorStr)
	}

	// Check per-side color first
	if colorStr, ok := box.Style.Get("border-" + side + "-color"); ok {
		if color, ok := resolveCurrentColor(colorStr); ok {
			return color, true
		}
	}
	// Fall back to global border-color
	if colorStr, ok := box.Style.Get("border-color"); ok {
		if color, ok := resolveCurrentColor(colorStr); ok {
			return color, true
		}
	}
	// Fall back to element's color property (CSS spec: border-color defaults to currentColor)
	if colorStr, ok := box.Style.Get("color"); ok {
		if color, ok := css.ParseColor(colorStr); ok {
			return color, true
		}
	}
	return css.Color{R: 0, G: 0, B: 0, A: 1.0}, false
}

func (r *Renderer) drawBorder(box *layout.Box) {
	if box.Border.Top == 0 && box.Border.Right == 0 && box.Border.Bottom == 0 && box.Border.Left == 0 {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// CRITICAL FIX: For inline elements, adjust dimensions to include bleeding borders/padding
	renderHeight := box.Height
	renderY := effectiveY
	if box.Style != nil && box.Style.GetDisplay() == css.DisplayInline {
		// Inline elements: box.Height is line box height, but borders "bleed" outside.
		// All inline fragments (including IsFirstFragment) get full top+bottom bleed.
		// The intervening block's background is painted after Fragment1 in document order,
		// so it covers any bleeding bottom border of Fragment1 (as in browser behavior).
		topBleed := box.Border.Top + box.Padding.Top
		bottomBleed := box.Padding.Bottom + box.Border.Bottom
		renderHeight = box.Height + topBleed + bottomBleed
		renderY = effectiveY - topBleed
	}

	// Phase 12: Get border styles for each side
	borderStyles := box.Style.GetBorderStyle()

	// Phase 12: Check for uniform rounded borders (with per-corner support)
	corners := box.Style.GetBorderRadiusCorners()
	if corners.MaxRadius() > 0 && box.Border.Top == box.Border.Right &&
		box.Border.Right == box.Border.Bottom && box.Border.Bottom == box.Border.Left {
		// Draw uniform-width rounded border with per-corner radii
		if color, ok := r.getBorderSideColor(box, "top"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.SetLineWidth(box.Border.Top)
			borderX := box.X + box.Border.Left/2
			borderY := renderY + box.Border.Top/2
			borderWidth := box.Width - box.Border.Left // Border-box dimensions
			borderHeight := renderHeight - box.Border.Top // Border-box dimensions
			r.context.DrawRoundedRectangleCorners(borderX, borderY, borderWidth, borderHeight,
				corners.TopLeft, corners.TopRight, corners.BottomRight, corners.BottomLeft)
			r.context.Stroke()
		}
		return
	}

	// Calculate border box coordinates using effective Y.
	// Snap inner coordinates to pixel boundaries so the gg rasterizer treats
	// them as hard edges. At fractional positions the rasterizer fills any pixel
	// the path crosses, causing border color to bleed into the content area
	// (visible when clip-path clips at the same boundary). Floor for top/left
	// inner edges (bottom/right side of top/left trapezoid) ensures the pixel
	// just past the edge is outside; ceil for bottom/right inner edges (top/left
	// side of bottom/right trapezoid) ensures the same.
	outerLeft := box.X
	outerTop := renderY
	outerRight := box.X + box.Width       // Border-box dimensions
	outerBottom := renderY + renderHeight  // Border-box dimensions
	innerLeft := math.Floor(box.X + box.Border.Left)
	innerTop := math.Floor(renderY + box.Border.Top)
	innerRight := math.Ceil(box.X + box.Width - box.Border.Right - 1e-9)      // Border-box dimensions; epsilon absorbs float imprecision from unit conversions (e.g. cm→px)
	innerBottom := math.Ceil(renderY + renderHeight - box.Border.Bottom - 1e-9) // Border-box dimensions

	// Draw each side as a trapezoid (CSS mitered border rendering).
	// Drawing order: bottom → left → right → top. Later-drawn sides
	// overwrite boundary pixels at diagonal miters, so this order gives
	// CSS priority: top > right > left > bottom at shared corners.

	// Special case: if all 4 sides are dotted with uniform width and same color,
	// use drawDottedRect for consistent rendering with dotted outlines.
	if borderStyles.Top == css.BorderStyleDotted && borderStyles.Right == css.BorderStyleDotted &&
		borderStyles.Bottom == css.BorderStyleDotted && borderStyles.Left == css.BorderStyleDotted &&
		box.Border.Top == box.Border.Right && box.Border.Right == box.Border.Bottom &&
		box.Border.Bottom == box.Border.Left && box.Border.Top > 0 {
		if c, ok := r.getBorderSideColor(box, "top"); ok {
			r.drawDottedRect(outerLeft, outerTop, box.Width, renderHeight, box.Border.Top, c.R, c.G, c.B, c.A)
			return
		}
	}

	// Bottom border
	if box.Border.Bottom > 0 && borderStyles.Bottom != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "bottom"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerBottom)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.LineTo(innerRight, innerBottom)
			r.context.LineTo(outerRight, outerBottom)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Left border
	// Skip left border for LastFragment of split inline (CSS 2.1 §9.2.1.1)
	if box.Border.Left > 0 && borderStyles.Left != css.BorderStyleNone && !box.IsLastFragment {
		if color, ok := r.getBorderSideColor(box, "left"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerTop)
			r.context.LineTo(innerLeft, innerTop)
			r.context.LineTo(innerLeft, innerBottom)
			r.context.LineTo(outerLeft, outerBottom)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Right border
	// Skip right border for FirstFragment of split inline (CSS 2.1 §9.2.1.1)
	if box.Border.Right > 0 && borderStyles.Right != css.BorderStyleNone && !box.IsFirstFragment {
		if color, ok := r.getBorderSideColor(box, "right"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerRight, outerTop)
			r.context.LineTo(outerRight, outerBottom)
			r.context.LineTo(innerRight, innerBottom)
			r.context.LineTo(innerRight, innerTop)
			r.context.ClosePath()
			r.context.Fill()
		}
	}

	// Top border
	if box.Border.Top > 0 && borderStyles.Top != css.BorderStyleNone {
		if color, ok := r.getBorderSideColor(box, "top"); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
			r.context.MoveTo(outerLeft, outerTop)
			r.context.LineTo(outerRight, outerTop)
			r.context.LineTo(innerRight, innerTop)
			r.context.LineTo(innerLeft, innerTop)
			r.context.ClosePath()
			r.context.Fill()
		}
	}
}

// drawOutline renders the CSS outline outside the border box.
// The outline is drawn OUTSIDE the border box and does not affect layout.
// outline-offset shifts the outline inward (negative) or outward (positive).
func (r *Renderer) drawOutline(box *layout.Box) {
	if box == nil || box.Style == nil {
		return
	}

	outlineWidth := box.Style.GetOutlineWidth()
	if outlineWidth <= 0 {
		return
	}

	outlineOffset := box.Style.GetOutlineOffset()
	oR, oG, oB, oA := box.Style.GetOutlineColor()

	if oA <= 0 {
		return
	}

	// Get effective Y (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Border-box dimensions: box.X/Y is the border-box top-left corner,
	// box.Width/Height are border-box dimensions.
	bbX := box.X
	bbY := effectiveY
	bbW := box.Width
	bbH := box.Height

	// Snap all outline coordinates to pixel boundaries to avoid sub-pixel
	// rendering artifacts (same technique as drawBorder). Fractional box
	// positions (from font metrics) would otherwise cause strip heights
	// to be 1px too large due to the gg rasterizer's "fill any crossed pixel"
	// behavior at fractional boundaries.
	outX := math.Round(bbX - outlineOffset - outlineWidth)
	outY := math.Round(bbY - outlineOffset - outlineWidth)
	inX := math.Round(bbX - outlineOffset)
	inY := math.Round(bbY - outlineOffset)
	inW := math.Round(bbW + 2*outlineOffset)
	inH := math.Round(bbH + 2*outlineOffset)
	outW := math.Round(bbW + 2*(outlineOffset + outlineWidth))
	outH := math.Round(bbH + 2*(outlineOffset + outlineWidth))

	// If the inner box collapses (negative offset bigger than half dimensions), skip
	if inW <= 0 || inH <= 0 {
		return
	}

	r.context.SetColor(color.NRGBA{R: oR, G: oG, B: oB, A: uint8(oA * 255)})

	outlineStyle := box.Style.GetOutlineStyle()

	if outlineStyle == "dotted" {
		// Dotted outline: draw square dots along each edge.
		r.drawDottedRect(outX, outY, outW, outH, outlineWidth, oR, oG, oB, oA)
		return
	}

	// Draw 4 outline strips using snapped coordinates so strip sizes are
	// derived from integer positions (avoids 1px rounding errors).
	topH := inY - outY
	bottomH := outH - inH - topH
	leftW := inX - outX
	rightW := outW - inW - leftW
	// Top strip
	r.context.DrawRectangle(outX, outY, outW, topH)
	r.context.Fill()
	// Bottom strip
	r.context.DrawRectangle(outX, inY+inH, outW, bottomH)
	r.context.Fill()
	// Left strip (between top and bottom strips)
	r.context.DrawRectangle(outX, inY, leftW, inH)
	r.context.Fill()
	// Right strip (between top and bottom strips)
	r.context.DrawRectangle(inX+inW, inY, rightW, inH)
	r.context.Fill()
}


// drawDottedRect draws a dotted rectangular outline along the perimeter of the
// rectangle (x, y, x+w, y+h). dotSize is the border/outline width; dots are
// square (dotSize×dotSize) with equal-size gaps between them. Corners get a dot,
// intermediate dots are evenly spaced along each edge.
func (r *Renderer) drawDottedRect(x, y, w, h, dotSize float64, cr, cg, cb uint8, ca float64) {
	r.context.SetRGBA(float64(cr)/255.0, float64(cg)/255.0, float64(cb)/255.0, ca)
	step := dotSize * 2.0 // dot size + gap size

	// Corner dot centers (in from outer edge by dotSize/2)
	x1 := x + dotSize/2 // left corner center x
	x2 := x + w - dotSize/2 // right corner center x
	y1 := y + dotSize*1.5 // first intermediate dot center y on left/right edges
	y2 := y + h - dotSize*1.5 // last intermediate dot center y on left/right edges

	// Top edge: draw dots from left corner to right corner
	for cx := x1; ; cx += step {
		if cx > x2 {
			cx = x2
		}
		r.context.DrawRectangle(cx-dotSize/2, y, dotSize, dotSize)
		r.context.Fill()
		if cx >= x2 {
			break
		}
	}

	// Bottom edge: draw dots from left corner to right corner
	by := y + h - dotSize
	for cx := x1; ; cx += step {
		if cx > x2 {
			cx = x2
		}
		r.context.DrawRectangle(cx-dotSize/2, by, dotSize, dotSize)
		r.context.Fill()
		if cx >= x2 {
			break
		}
	}

	// Left edge: intermediate dots (between corner dots)
	if y1 <= y2 {
		for cy := y1; ; cy += step {
			if cy > y2 {
				cy = y2
			}
			r.context.DrawRectangle(x, cy-dotSize/2, dotSize, dotSize)
			r.context.Fill()
			if cy >= y2 {
				break
			}
		}
	}

	// Right edge: intermediate dots (between corner dots)
	rx := x + w - dotSize
	if y1 <= y2 {
		for cy := y1; ; cy += step {
			if cy > y2 {
				cy = y2
			}
			r.context.DrawRectangle(rx, cy-dotSize/2, dotSize, dotSize)
			r.context.Fill()
			if cy >= y2 {
				break
			}
		}
	}
}

func (r *Renderer) drawBoxShadow(box *layout.Box) {
	shadows := box.Style.GetBoxShadow()
	if len(shadows) == 0 {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Box dimensions (border-box edge — CSS spec: shadow is cast from border edge)
	boxX := box.X
	boxY := effectiveY
	boxWidth := box.Width
	boxHeight := box.Height
	borderRadius := box.Style.GetBorderRadius()

	// Draw outer shadows in reverse order (first declared = topmost)
	for i := len(shadows) - 1; i >= 0; i-- {
		shadow := shadows[i]

		if shadow.Inset {
			continue // Inset shadows drawn separately after background
		}

		// Outer shadow: drawn outside the border edge
		shadowX := boxX + shadow.OffsetX - shadow.Spread
		shadowY := boxY + shadow.OffsetY - shadow.Spread
		shadowWidth := boxWidth + 2*shadow.Spread
		shadowHeight := boxHeight + 2*shadow.Spread

		if shadow.Blur > 0 {
			// Approximate Gaussian blur with multiple layers
			layers := int(shadow.Blur / 2)
			if layers < 3 {
				layers = 3
			}
			if layers > 10 {
				layers = 10
			}

			for layer := layers; layer >= 0; layer-- {
				expand := float64(layer) * (shadow.Blur / float64(layers))
				layerX := shadowX - expand
				layerY := shadowY - expand
				layerWidth := shadowWidth + 2*expand
				layerHeight := shadowHeight + 2*expand

				layerAlpha := shadow.Color.A * (1.0 - float64(layer)/float64(layers+1)) * 0.3

				r.context.SetRGBA(
					float64(shadow.Color.R)/255.0,
					float64(shadow.Color.G)/255.0,
					float64(shadow.Color.B)/255.0,
					layerAlpha,
				)

				if borderRadius > 0 {
					r.context.DrawRoundedRectangle(layerX, layerY, layerWidth, layerHeight, borderRadius+expand)
				} else {
					r.context.DrawRectangle(layerX, layerY, layerWidth, layerHeight)
				}
				r.context.Fill()
			}
		} else {
			// No blur — solid shadow
			r.context.SetRGBA(
				float64(shadow.Color.R)/255.0,
				float64(shadow.Color.G)/255.0,
				float64(shadow.Color.B)/255.0,
				shadow.Color.A,
			)

			if borderRadius > 0 {
				r.context.DrawRoundedRectangle(shadowX, shadowY, shadowWidth, shadowHeight, borderRadius)
			} else {
				r.context.DrawRectangle(shadowX, shadowY, shadowWidth, shadowHeight)
			}
			r.context.Fill()
		}
	}
}

// drawInsetBoxShadow draws inset box-shadows (above background, below border/content)
func (r *Renderer) drawInsetBoxShadow(box *layout.Box) {
	shadows := box.Style.GetBoxShadow()
	if len(shadows) == 0 {
		return
	}

	effectiveY := r.getEffectiveY(box)
	boxX := box.X
	boxY := effectiveY
	boxWidth := box.Width
	boxHeight := box.Height

	for i := len(shadows) - 1; i >= 0; i-- {
		shadow := shadows[i]
		if !shadow.Inset {
			continue
		}

		// Inset shadow is drawn inside the padding edge
		padX := boxX + box.Border.Left
		padY := boxY + box.Border.Top
		padW := boxWidth - box.Border.Left - box.Border.Right
		padH := boxHeight - box.Border.Top - box.Border.Bottom

		innerX := padX + shadow.OffsetX + shadow.Spread
		innerY := padY + shadow.OffsetY + shadow.Spread
		innerW := padW - 2*shadow.Spread
		innerH := padH - 2*shadow.Spread

		r.context.SetRGBA(
			float64(shadow.Color.R)/255.0,
			float64(shadow.Color.G)/255.0,
			float64(shadow.Color.B)/255.0,
			shadow.Color.A,
		)

		if innerW <= 0 || innerH <= 0 {
			r.context.DrawRectangle(padX, padY, padW, padH)
			r.context.Fill()
			continue
		}

		// Draw 4 rectangles forming the inset frame
		r.context.DrawRectangle(padX, padY, padW, innerY-padY)
		r.context.Fill()
		r.context.DrawRectangle(padX, innerY+innerH, padW, (padY+padH)-(innerY+innerH))
		r.context.Fill()
		r.context.DrawRectangle(padX, innerY, innerX-padX, innerH)
		r.context.Fill()
		r.context.DrawRectangle(innerX+innerW, innerY, (padX+padW)-(innerX+innerW), innerH)
		r.context.Fill()
	}
}

func (r *Renderer) drawText(box *layout.Box) {
	// Multi-line text containers have children (one per line) that draw the
	// actual text. Drawing the container's full text would duplicate it.
	if len(box.Children) > 0 && box.Node != nil && box.Node.Type == html.TextNode {
		return
	}

	// Determine text content: from DOM text node or pseudo-element content
	textContent := ""
	if box.PseudoContent != "" {
		textContent = box.PseudoContent
	} else if box.Node != nil && box.Node.Type == html.TextNode {
		textContent = box.Node.Text
	}
	if textContent == "" {
		return
	}

	// Skip drawing parent's PseudoContent if it has children (child boxes draw the actual content)
	// This prevents the parent from drawing the full text which would be covered by child boxes
	if box.PseudoContent != "" && len(box.Children) > 0 {
		return
	}

	// Apply text-transform (safety net — layout phase usually handles this via CollectInlineItems)
	if box.Style != nil {
		textContent = layout.ApplyTextTransform(textContent, box.Style.GetTextTransform())
		// Apply font-variant-caps: small-caps renders as uppercase (simple approximation)
		if box.Style.GetFontVariantCaps() == "small-caps" {
			textContent = strings.ToUpper(textContent)
		}
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Calculate X position based on text-align
	textX := box.X
	textAlign := box.Style.GetTextAlign()
	fontSize := box.Style.GetFontSize()
	bold := box.Style.GetFontWeight() == css.FontWeightBold
	italic := box.Style.GetFontStyle() == css.FontStyleItalic
	mono := box.Style.IsMonospaceFamily()
	ahem := box.Style.IsAhemFamily()
	fontFamily, _ := box.Style.Get("font-family")

	// Load the appropriate font face (checks web font registry first)
	r.loadFontForFamily(fontFamily, fontSize, bold, italic, mono, ahem)

	r.context.SetRGB(0, 0, 0)
	if colorStr, ok := box.Style.Get("color"); ok {
		if color, ok := css.ParseColor(colorStr); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
		}
	}

	// Draw text at calculated position
	// Use actual font ascent for baseline placement (not fontSize).
	// For Ahem at 40px: ascent=32, descent=8. Using fontSize (40) would
	// place the baseline 8px too low, causing glyphs to overflow the line box.
	ascent := r.context.FontAscent()
	textY := effectiveY + ascent

	// Draw text-shadow layers first (behind main text)
	shadows := box.Style.GetTextShadow()
	for _, shadow := range shadows {
		r.context.SetRGBA(
			float64(shadow.Color.R)/255.0,
			float64(shadow.Color.G)/255.0,
			float64(shadow.Color.B)/255.0,
			shadow.Color.A,
		)
		shadowX := textX + shadow.OffsetX
		shadowY := textY + shadow.OffsetY
		letterSpacing := box.Style.GetLetterSpacing()
		if letterSpacing != 0 {
			drawX := shadowX
			for _, ch := range textContent {
				charStr := string(ch)
				r.context.DrawString(charStr, drawX, shadowY)
				charWidth, _ := text.MeasureTextWithStyle(charStr, fontSize, bold, italic, mono, ahem)
				drawX += charWidth + letterSpacing
			}
		} else {
			r.context.DrawString(textContent, shadowX, shadowY)
		}
	}
	// Restore main text color
	r.context.SetRGB(0, 0, 0)
	if colorStr, ok := box.Style.Get("color"); ok {
		if color, ok := css.ParseColor(colorStr); ok {
			r.context.SetRGBA(float64(color.R)/255.0, float64(color.G)/255.0, float64(color.B)/255.0, color.A)
		}
	}

	// CSS 2.1 §16.4: Apply letter-spacing between characters
	letterSpacing := box.Style.GetLetterSpacing()
	if letterSpacing != 0 {
		// Draw characters individually with letter-spacing
		drawX := textX
		for _, ch := range textContent {
			charStr := string(ch)
			r.context.DrawString(charStr, drawX, textY)
			charWidth, _ := text.MeasureTextWithStyle(charStr, fontSize, bold, italic, mono, ahem)
			drawX += charWidth + letterSpacing
		}
	} else {
		r.context.DrawString(textContent, textX, textY)
	}

	// Phase 17: Draw text decorations
	decoration := box.Style.GetTextDecoration()
	if decoration != css.TextDecorationNone {
		// Check text-decoration-color
		if decoColor, hasDecoColor := box.Style.GetTextDecorationColor(); hasDecoColor {
			if decoColor.A == 0 || (decoColor.R == 0 && decoColor.G == 0 && decoColor.B == 0 && decoColor.A == 0) {
				// transparent decoration — skip drawing
				goto afterDecoration
			}
			r.context.SetRGBA(float64(decoColor.R)/255, float64(decoColor.G)/255, float64(decoColor.B)/255, decoColor.A)
		}

		{
			textWidth, _ := text.MeasureTextWithStyle(textContent, fontSize, bold, italic, mono, ahem)
			underlineOffset := box.Style.GetTextUnderlineOffset()
			thickness := box.Style.GetTextDecorationThickness()
			decoStyle := box.Style.GetTextDecorationStyle()

			// Compute x start position for decoration line
			lineX := textX
			if textAlign == css.TextAlignCenter {
				lineX = textX - textWidth/2
			}

			// Compute half-leading: (lineHeight - fontSize) / 2
			// When line-height > font-size, text is vertically centered in the line box.
			// Underline should be offset from the bottom of the em-square (fontSize), not
			// from the top of the line box. Half-leading shifts the underline down correctly.
			halfLeading := 0.0
			if box.Style != nil {
				lineHeight := box.Style.GetLineHeight()
				if lineHeight > fontSize {
					halfLeading = (lineHeight - fontSize) / 2
				}
			}

			switch decoration {
			case css.TextDecorationUnderline:
				underlineY := effectiveY + halfLeading + fontSize + 2 + underlineOffset
				switch decoStyle {
				case "double":
					// Two lines with a 2px gap between them
					r.context.DrawRectangle(lineX, underlineY, textWidth, thickness)
					r.context.Fill()
					r.context.DrawRectangle(lineX, underlineY+thickness+2, textWidth, thickness)
					r.context.Fill()
				case "wavy":
					// Sine wave underline
					amplitude := thickness * 1.5
					if amplitude < 2 {
						amplitude = 2
					}
					period := fontSize * 0.4
					if period < 6 {
						period = 6
					}
					waveY := underlineY + amplitude
					r.context.SetLineWidth(thickness)
					r.context.NewSubPath()
					r.context.MoveTo(lineX, waveY)
					endX := lineX + textWidth
					for wx := lineX; wx <= endX; wx += 1.0 {
						wy := waveY + amplitude*math.Sin((wx-lineX)*2*math.Pi/period)
						r.context.LineTo(wx, wy)
					}
					r.context.Stroke()
				default:
					// solid (and other styles fall back to solid)
					r.context.DrawRectangle(lineX, underlineY, textWidth, thickness)
					r.context.Fill()
				}

			case css.TextDecorationOverline:
				overlineY := effectiveY
				r.context.DrawRectangle(lineX, overlineY, textWidth, thickness)
				r.context.Fill()

			case css.TextDecorationLineThrough:
				lineThroughY := effectiveY + fontSize*0.5
				r.context.DrawRectangle(lineX, lineThroughY, textWidth, thickness)
				r.context.Fill()
			}
		}
	}
afterDecoration:
}

func (r *Renderer) drawImage(box *layout.Box) {
	if box.ImagePath == "" {
		return
	}

	// Get effective Y position (adjusted for scroll offset)
	effectiveY := r.getEffectiveY(box)

	// Load the image (use fetcher if available)
	img, err := images.LoadImageWithFetcher(box.ImagePath, r.imageFetcher)
	if err != nil {
		// Image failed to load, draw placeholder
		r.context.SetRGB(0.9, 0.9, 0.9)
		r.context.DrawRectangle(box.X, effectiveY, box.Width, box.Height)
		r.context.Fill()

		r.context.SetRGB(0.5, 0.5, 0.5)
		r.context.SetLineWidth(2)
		r.context.DrawLine(box.X, effectiveY, box.X+box.Width, effectiveY+box.Height)
		r.context.DrawLine(box.X+box.Width, effectiveY, box.X, effectiveY+box.Height)
		r.context.Stroke()
		return
	}

	contentX := box.X + box.Border.Left + box.Padding.Left
	contentY := effectiveY + box.Border.Top + box.Padding.Top
	contentW := box.Width
	contentH := box.Height

	bounds := img.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())

	// Guard against zero dimensions
	if imgW <= 0 || imgH <= 0 || contentW <= 0 || contentH <= 0 {
		return
	}

	// Determine object-fit scaling
	fit := css.ObjectFitFill
	if box.Style != nil {
		fit = box.Style.GetObjectFit()
	}

	scaleX := contentW / imgW
	scaleY := contentH / imgH

	switch fit {
	case css.ObjectFitContain:
		s := math.Min(scaleX, scaleY)
		scaleX, scaleY = s, s
	case css.ObjectFitCover:
		s := math.Max(scaleX, scaleY)
		scaleX, scaleY = s, s
	case css.ObjectFitNone:
		scaleX, scaleY = 1, 1
	case css.ObjectFitScaleDown:
		s := math.Min(scaleX, scaleY)
		if s > 1 {
			s = 1
		}
		scaleX, scaleY = s, s
	// ObjectFitFill: keep independent scaleX, scaleY (stretch)
	}

	// Compute rendered image size after scaling
	renderW := imgW * scaleX
	renderH := imgH * scaleY

	// Apply object-position (default 50% 50%)
	posX, posY := 0.5, 0.5
	if box.Style != nil {
		posX, posY = box.Style.GetObjectPosition()
	}
	offsetX := (contentW - renderW) * posX
	offsetY := (contentH - renderH) * posY

	// Guard against extreme scale values
	if math.IsNaN(scaleX) || math.IsNaN(scaleY) || math.IsInf(scaleX, 0) || math.IsInf(scaleY, 0) ||
		scaleX <= 0 || scaleY <= 0 || scaleX > 1e6 || scaleY > 1e6 {
		return
	}

	r.context.Push()
	// Clip to content box so cover/none don't overflow
	r.context.DrawRectangle(contentX, contentY, contentW, contentH)
	r.context.Clip()
	r.context.Translate(contentX+offsetX, contentY+offsetY)
	r.context.Scale(scaleX, scaleY)
	r.context.DrawImage(img, 0, 0)
	r.popContext()
}

// drawBackgroundImage renders a CSS background-image on a box
func (r *Renderer) drawBackgroundImage(box *layout.Box) {
	imgURL, ok := box.Style.GetBackgroundImage()
	if !ok {
		return
	}

	img, err := images.LoadImageWithFetcher(imgURL, r.imageFetcher)
	if err != nil {
		return
	}

	effectiveY := r.getEffectiveY(box)

	bgX := box.X
	bgY := effectiveY
	bgWidth := box.Width // Border-box dimensions
	bgHeight := box.Height // Border-box dimensions

	bounds := img.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())

	// Apply background-size
	bgSize := box.Style.GetBackgroundSize()
	scaleX, scaleY := 1.0, 1.0
	if bgSize.Cover {
		// Scale to cover the entire background area (may crop)
		sx := bgWidth / imgW
		sy := bgHeight / imgH
		scale := sx
		if sy > sx {
			scale = sy
		}
		scaleX, scaleY = scale, scale
		imgW *= scale
		imgH *= scale
	} else if bgSize.Contain {
		// Scale to fit within the background area (may leave gaps)
		sx := bgWidth / imgW
		sy := bgHeight / imgH
		scale := sx
		if sy < sx {
			scale = sy
		}
		scaleX, scaleY = scale, scale
		imgW *= scale
		imgH *= scale
	} else {
		// Explicit dimensions
		targetW, targetH := bgSize.Width, bgSize.Height
		// Resolve percentage values (stored as negative)
		if targetW < 0 {
			targetW = bgWidth * (-targetW) / 100
		}
		if targetH < 0 {
			targetH = bgHeight * (-targetH) / 100
		}
		if targetW > 0 && targetH > 0 {
			scaleX = targetW / float64(bounds.Dx())
			scaleY = targetH / float64(bounds.Dy())
			imgW = targetW
			imgH = targetH
		} else if targetW > 0 {
			scaleX = targetW / float64(bounds.Dx())
			scaleY = scaleX // Maintain aspect ratio
			imgW = targetW
			imgH = float64(bounds.Dy()) * scaleY
		} else if targetH > 0 {
			scaleY = targetH / float64(bounds.Dy())
			scaleX = scaleY // Maintain aspect ratio
			imgW = float64(bounds.Dx()) * scaleX
			imgH = targetH
		}
	}

	repeat := box.Style.GetBackgroundRepeat()
	pos := box.Style.GetBackgroundPosition()
	attachment := box.Style.GetBackgroundAttachment()

	originX := bgX
	originY := bgY
	if attachment == "fixed" {
		originX = 0
		originY = 0
	}

	r.context.Push()
	r.context.DrawRectangle(bgX, bgY, bgWidth, bgHeight)
	r.context.Clip()

	needsScale := scaleX != 1.0 || scaleY != 1.0
	drawClipped := func(drawX, drawY int) {
		if needsScale {
			r.context.Push()
			r.context.Translate(float64(drawX), float64(drawY))
			r.context.Scale(scaleX, scaleY)
			r.context.DrawImage(img, 0, 0)
			r.popContext()
		} else {
			r.context.DrawImage(img, drawX, drawY)
		}
	}

	// Resolve percentage-based positions to pixels
	posX := pos.ResolveX(bgWidth, imgW)
	posY := pos.ResolveY(bgHeight, imgH)

	switch repeat {
	case css.BackgroundRepeatNoRepeat:
		drawClipped(int(originX+posX), int(originY+posY))

	case css.BackgroundRepeatRepeatX:
		startX := posX
		for startX > 0 {
			startX -= imgW
		}
		tileEndX := bgX + bgWidth - originX
		for x := startX; x < tileEndX; x += imgW {
			drawClipped(int(originX+x), int(originY+posY))
		}

	case css.BackgroundRepeatRepeatY:
		startY := posY
		for startY > 0 {
			startY -= imgH
		}
		tileEndY := bgY + bgHeight - originY
		for y := startY; y < tileEndY; y += imgH {
			drawClipped(int(originX+posX), int(originY+y))
		}

	default: // repeat
		startX := posX
		for startX > 0 {
			startX -= imgW
		}
		startY := posY
		for startY > 0 {
			startY -= imgH
		}
		tileEndX := bgX + bgWidth - originX
		tileEndY := bgY + bgHeight - originY
		for y := startY; y < tileEndY; y += imgH {
			for x := startX; x < tileEndX; x += imgW {
				drawClipped(int(originX+x), int(originY+y))
			}
		}
	}

	r.popContext()
}

func (r *Renderer) SavePNG(filename string) error {
	return r.context.SavePNG(filename)
}

func (r *Renderer) applyTransforms(box *layout.Box, transforms []css.Transform) {
	origin := box.Style.GetTransformOrigin()
	effectiveY := r.getEffectiveY(box)

	originX := box.X + box.Padding.Left + origin.X*box.Width
	originY := effectiveY + box.Padding.Top + origin.Y*box.Height

	r.context.Translate(originX, originY)

	for _, t := range transforms {
		switch t.Type {
		case "translate":
			if len(t.Values) >= 2 {
				r.context.Translate(t.Values[0], t.Values[1])
			} else if len(t.Values) >= 1 {
				r.context.Translate(t.Values[0], 0)
			}
		case "rotate":
			if len(t.Values) >= 1 {
				// Transform values are stored in degrees; gg.Rotate expects radians
				r.context.Rotate(t.Values[0] * math.Pi / 180)
			}
		case "scale":
			if len(t.Values) >= 2 {
				r.context.Scale(t.Values[0], t.Values[1])
			} else if len(t.Values) >= 1 {
				r.context.Scale(t.Values[0], t.Values[0])
			}
		case "skew":
			// CSS skewX(a) with Y-down screen coords: positive angle tilts top rightward.
			// gg.Shear(sx, sy) maps: x' = x + sx*y, y' = sy*x + y.
			// To get top-shifts-right behavior for positive a, negate the shear values.
			if len(t.Values) >= 2 {
				tanX := math.Tan(t.Values[0] * math.Pi / 180)
				tanY := math.Tan(t.Values[1] * math.Pi / 180)
				r.context.Shear(-tanX, -tanY)
			} else if len(t.Values) >= 1 {
				tanX := math.Tan(t.Values[0] * math.Pi / 180)
				r.context.Shear(-tanX, 0)
			}
		}
	}

	r.context.Translate(-originX, -originY)
}

func (r *Renderer) drawScrollbarIndicators(box *layout.Box) {
	// Check scrollbar-width: none — no scrollbars drawn at all
	scrollbarWidthKeyword := box.Style.GetScrollbarWidth()
	if scrollbarWidthKeyword == "none" {
		return
	}

	// Determine scrollbar track width based on scrollbar-width keyword
	barSize := 12.0
	if scrollbarWidthKeyword == "thin" {
		barSize = 8.0
	}

	// Determine scrollbar colors (track and thumb)
	scrollbarColorVal := box.Style.GetScrollbarColor()
	var trackColor, thumbColor css.Color
	if scrollbarColorVal.IsAuto {
		// Default platform-style colors
		trackColor = css.Color{R: 200, G: 200, B: 200, A: 1.0}
		thumbColor = css.Color{R: 160, G: 160, B: 160, A: 1.0}
	} else {
		thumbColor = scrollbarColorVal.Thumb
		trackColor = scrollbarColorVal.Track
	}

	effectiveY := r.getEffectiveY(box)

	contentX := box.X + box.Padding.Left
	contentY := effectiveY + box.Padding.Top
	contentWidth := box.Width
	contentHeight := box.Height

	// Draw vertical scrollbar track
	r.context.SetRGBA(
		float64(trackColor.R)/255.0,
		float64(trackColor.G)/255.0,
		float64(trackColor.B)/255.0,
		trackColor.A,
	)
	r.context.DrawRectangle(
		contentX+contentWidth-barSize,
		contentY,
		barSize,
		contentHeight,
	)
	r.context.Fill()

	// Draw vertical scrollbar thumb (top 50% of track height, centered)
	thumbHeight := contentHeight * 0.5
	r.context.SetRGBA(
		float64(thumbColor.R)/255.0,
		float64(thumbColor.G)/255.0,
		float64(thumbColor.B)/255.0,
		thumbColor.A,
	)
	r.context.DrawRectangle(
		contentX+contentWidth-barSize,
		contentY,
		barSize,
		thumbHeight,
	)
	r.context.Fill()

	// Draw horizontal scrollbar track
	r.context.SetRGBA(
		float64(trackColor.R)/255.0,
		float64(trackColor.G)/255.0,
		float64(trackColor.B)/255.0,
		trackColor.A,
	)
	r.context.DrawRectangle(
		contentX,
		contentY+contentHeight-barSize,
		contentWidth-barSize,
		barSize,
	)
	r.context.Fill()

	// Draw horizontal scrollbar thumb (left 50% of track width)
	thumbWidth := (contentWidth - barSize) * 0.5
	r.context.SetRGBA(
		float64(thumbColor.R)/255.0,
		float64(thumbColor.G)/255.0,
		float64(thumbColor.B)/255.0,
		thumbColor.A,
	)
	r.context.DrawRectangle(
		contentX,
		contentY+contentHeight-barSize,
		thumbWidth,
		barSize,
	)
	r.context.Fill()
}

// drawSVGContent renders inline SVG using oksvg for full path/shape support.
func (r *Renderer) drawSVGContent(box *layout.Box) {
	if box.Node == nil {
		return
	}

	effectiveY := r.getEffectiveY(box)
	svgX := box.X + box.Border.Left + box.Padding.Left
	svgY := effectiveY + box.Border.Top + box.Padding.Top
	w := int(math.Ceil(box.Width))
	h := int(math.Ceil(box.Height))
	if w <= 0 || h <= 0 {
		return
	}

	// Serialize our DOM subtree to SVG XML for oksvg
	var buf bytes.Buffer
	serializeSVGNode(box.Node, &buf)

	icon, err := oksvg.ReadIconStream(&buf, oksvg.IgnoreErrorMode)
	if err != nil {
		// Fallback: try old per-element rendering
		r.drawSVGContentFallback(box, svgX, svgY)
		return
	}

	// If viewBox is zero, use the box dimensions
	if icon.ViewBox.W == 0 {
		icon.ViewBox.W = float64(w)
	}
	if icon.ViewBox.H == 0 {
		icon.ViewBox.H = float64(h)
	}

	icon.SetTarget(0, 0, float64(w), float64(h))

	// Render to an offscreen RGBA image
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	dasher := rasterx.NewDasher(w, h, scanner)
	icon.Draw(dasher, 1.0)

	// Composite onto our main context
	r.context.DrawImage(rgba, int(svgX), int(svgY))
}

// serializeSVGNode writes an html.Node tree as XML suitable for oksvg parsing.
func serializeSVGNode(node *html.Node, buf *bytes.Buffer) {
	if node.Type == html.TextNode {
		buf.WriteString(node.Text)
		return
	}
	if node.Type != html.ElementNode {
		return
	}

	// Write opening tag
	buf.WriteByte('<')
	buf.WriteString(node.TagName)
	for k, v := range node.Attributes {
		buf.WriteByte(' ')
		buf.WriteString(k)
		buf.WriteString(`="`)
		// Escape XML attribute value
		for _, c := range v {
			switch c {
			case '"':
				buf.WriteString("&quot;")
			case '&':
				buf.WriteString("&amp;")
			case '<':
				buf.WriteString("&lt;")
			case '>':
				buf.WriteString("&gt;")
			default:
				buf.WriteRune(c)
			}
		}
		buf.WriteByte('"')
	}

	if len(node.Children) == 0 {
		buf.WriteString("/>")
		return
	}

	buf.WriteByte('>')
	for _, child := range node.Children {
		serializeSVGNode(child, buf)
	}
	buf.WriteString("</")
	buf.WriteString(node.TagName)
	buf.WriteByte('>')
}

// drawSVGContentFallback is the old per-element SVG renderer, used when oksvg fails.
func (r *Renderer) drawSVGContentFallback(box *layout.Box, svgX, svgY float64) {
	for _, child := range box.Node.Children {
		if child.Type != html.ElementNode {
			continue
		}
		if child.TagName == "rect" {
			r.drawSVGRect(child, svgX, svgY)
		}
	}
}

// parseSVGLength parses an SVG length attribute value (unitless number = pixels).
func parseSVGLength(val string) (float64, bool) {
	if v, ok := css.ParseLength(val); ok {
		return v, true
	}
	v, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// drawSVGRect draws an SVG <rect> element (fallback renderer).
func (r *Renderer) drawSVGRect(node *html.Node, svgX, svgY float64) {
	x, y, w, h := 0.0, 0.0, 0.0, 0.0
	if val, ok := node.GetAttribute("x"); ok {
		if v, ok := parseSVGLength(val); ok {
			x = v
		}
	}
	if val, ok := node.GetAttribute("y"); ok {
		if v, ok := parseSVGLength(val); ok {
			y = v
		}
	}
	if val, ok := node.GetAttribute("width"); ok {
		if v, ok := parseSVGLength(val); ok {
			w = v
		}
	}
	if val, ok := node.GetAttribute("height"); ok {
		if v, ok := parseSVGLength(val); ok {
			h = v
		}
	}
	fillColor := getSVGFillColor(node)
	r.context.SetRGBA(float64(fillColor.R)/255.0, float64(fillColor.G)/255.0, float64(fillColor.B)/255.0, fillColor.A)
	r.context.DrawRectangle(svgX+x, svgY+y, w, h)
	r.context.Fill()
}

// getSVGFillColor extracts the fill color from an SVG element's fill attribute or inline style.
func getSVGFillColor(node *html.Node) css.Color {
	defaultColor := css.Color{R: 0, G: 0, B: 0, A: 1.0}
	if fill, ok := node.GetAttribute("fill"); ok {
		if c, ok := css.ParseColor(fill); ok {
			return c
		}
	}
	if styleAttr, ok := node.GetAttribute("style"); ok {
		for _, decl := range strings.Split(styleAttr, ";") {
			parts := strings.SplitN(strings.TrimSpace(decl), ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "fill" {
				if c, ok := css.ParseColor(strings.TrimSpace(parts[1])); ok {
					return c
				}
			}
		}
	}
	return defaultColor
}

// findDocumentRoot walks up the node tree to find the document root.
func findDocumentRoot(node *html.Node) *html.Node {
	root := node
	for root.Parent != nil {
		root = root.Parent
	}
	return root
}

// findElementByID searches the DOM tree for an element with the given id attribute.
func findElementByID(root *html.Node, id string) *html.Node {
	if root.Type == html.ElementNode {
		if attrID, ok := root.GetAttribute("id"); ok && attrID == id {
			return root
		}
	}
	for _, child := range root.Children {
		if found := findElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}
