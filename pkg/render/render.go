package render

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/layout/svg"
	"louis14/pkg/text"
	"mazarin/textshape"
)

// fontCacheKey identifies a font by path and size.
type fontCacheKey struct {
	path string
	size int32
}

// Renderer paints a tree of layout boxes onto an image.
type Renderer struct {
	dc                 textshape.DrawContext
	target             *image.RGBA
	fonts              text.FontConfig
	imageFetcher       images.ImageFetcher
	externalSVGFetcher func(uri string) ([]byte, error)
	scrollY            float64

	// svgResources aggregates the SVG resource registries from every
	// inline `<svg>` in the current paint frame. Populated by Render
	// (and RenderEmbedded) just before the paint walk by
	// collectSVGResources, which walks the layout-box tree gathering
	// every SVGRoot.Resources. Read by paint-time `url(#id)` resolvers
	// for `mask-image: url(#id)` and `clip-path: url(#id)` on non-SVG
	// elements — the document fragment id space is shared across the
	// page per SVG 2 §3.1, so an inline `<svg height="0">` declaring a
	// `<mask id="m">` is reachable from any CSS box's `mask-image:
	// url(#m)`. Mirrors Blink's per-Document SVGTreeScopeResources
	// (core/svg/svg_resource.h) — louis14 aggregates per-render-frame
	// because we don't have a Document-lifetime renderer.
	svgResources *svg.SVGResourceRegistry

	// Font ID cache: (path, size) → fontID
	fontCache   map[fontCacheKey]int32
	fontCacheMu sync.Mutex

	// tempFontIDs collects fontIDs returned by openFont during the
	// current Render call. Each public Render entry point resets this
	// to empty on entry and defers a closeTempFonts that releases each
	// ID via dc.CloseTemporaryFont. Permanent-pool fontIDs (returned
	// by the OpenTemporaryFont permanent-first dedupe) close to a no-op,
	// so this list can include any fontID openFont produced.
	tempFontIDs []int32

	// Custom @counter-style rules, keyed by name.
	counterStyles map[string]css.CounterStyleRule

	// clipStack tracks active overflow:hidden clip rectangles. Gradient rendering
	// writes directly to the pixel buffer (bypassing the gg context's clip), so
	// we maintain our own clip bounds to intersect with during direct pixel writes.
	clipStack []clipRect

	// embeddedViewport, when non-zero, signals that the renderer is painting
	// inside a host DrawContext that owns more area than just the HTML viewport
	// (e.g. mancini's WebInteractor inside a larger app). In that mode the
	// canvas-background fills must be scoped to (0,0)-(viewport.W, viewport.H)
	// instead of calling dc.Clear() which blanks the entire host DC.
	// Set by RenderEmbedded; cleared on entry to Render.
	embeddedViewport struct{ W, H float64 }

	// transformDepth counts the active stack of layer transforms (including
	// `will-change: transform`) wrapping the current paint operation. Per
	// CSS Transforms 1 §6, when this counter is > 0, `background-attachment:
	// fixed` on a non-canvas layer degrades to `scroll` (the element is no
	// longer fixed to the viewport). Mirrors Blink's
	// FixedBackgroundPaintsInLocalCoordinates check in
	// background_image_geometry.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	transformDepth int

	// canvasBgPaintedLayer, when non-nil, marks the PaintLayer whose canvas
	// background was painted out-of-transform by paintLayerCanvasBackground.
	// drawBackground compares against this pointer to skip the in-transform
	// repaint that would otherwise rotate/scale the canvas bg with the
	// layer's own transform. Reset to nil after the layer's paint completes.
	canvasBgPaintedLayer *PaintLayer

	// canvasBgPaintedDescendants tracks canvas-bg layers whose bg was
	// painted out-of-transform by an ancestor's paintLayerCanvasBackground
	// (typically body promoted to root's canvas, where root has the
	// transform and body is a non-SC FlowChild visited by
	// paintDescendantPhase). drawBackground consults this set to skip the
	// in-transform repaint on the body layer during the standard paint
	// walk. Cleared when the owning paintLayer call finishes.
	canvasBgPaintedDescendants map[*PaintLayer]struct{}
}

// clipRect represents an active clip rectangle.
type clipRect struct {
	x, y, w, h float64
}

// newProvider returns the shared GlyphProvider used by both layout-time
// measurement and paint-time rendering. Reusing the same provider — instead
// of constructing a fresh DirectGlyphProvider per renderer — keeps
// @font-face buffers (registered via [text.FontRegistry.RegisterFontFace])
// visible across the layout↔render handoff. See `text.CurrentProvider`.
func newProvider() textshape.GlyphProvider {
	return text.CurrentProvider()
}

// NewRenderer creates a new renderer with a fresh image of the given dimensions.
func NewRenderer(width, height int) *Renderer {
	dc := textshape.NewDrawContext(width, height, newProvider())
	text.SetTextLayout(dc.TextLayout())
	return &Renderer{
		dc:        dc,
		target:    dc.Image().(*image.RGBA),
		fonts:     text.DefaultFontConfig(),
		fontCache: make(map[fontCacheKey]int32),
	}
}

// NewRendererForImage creates a renderer that paints onto an existing image.
func NewRendererForImage(target *image.RGBA) *Renderer {
	dc := textshape.NewDrawContextForImage(target, newProvider())
	text.SetTextLayout(dc.TextLayout())
	return &Renderer{
		dc:        dc,
		target:    target,
		fonts:     text.DefaultFontConfig(),
		fontCache: make(map[fontCacheKey]int32),
	}
}

// NewRendererForImageWithProvider creates a renderer that paints onto an
// existing image using the supplied GlyphProvider for font rasterization.
func NewRendererForImageWithProvider(target *image.RGBA, provider textshape.GlyphProvider) *Renderer {
	dc := textshape.NewDrawContextForImage(target, provider)
	text.SetTextLayout(dc.TextLayout())
	return &Renderer{
		dc:        dc,
		target:    target,
		fonts:     text.DefaultFontConfig(),
		fontCache: make(map[fontCacheKey]int32),
	}
}

// NewRendererForDrawContext creates a renderer that paints using an
// existing DrawContext. The DrawContext's image, translation, and
// clipping state are used directly — no intermediate buffer is created.
// Use this when the caller already owns the drawing surface (e.g.,
// mancini interactor framework).
func NewRendererForDrawContext(dc textshape.DrawContext) *Renderer {
	text.SetTextLayout(dc.TextLayout())
	return &Renderer{
		dc:        dc,
		target:    dc.Image().(*image.RGBA),
		fonts:     text.DefaultFontConfig(),
		fontCache: make(map[fontCacheKey]int32),
	}
}

// SetFonts configures font paths for text rendering.
func (r *Renderer) SetFonts(fonts text.FontConfig) {
	r.fonts = fonts
}

// SetImageFetcher sets the image fetcher for loading network images during painting.
func (r *Renderer) SetImageFetcher(fetcher images.ImageFetcher) {
	r.imageFetcher = fetcher
}

// SetExternalSVGFetcher installs a closure that resolves
// `filter: url(doc#id)` references to external SVG document bytes
// (data:, file://, http(s)://, or harness-resolved relative URLs).
// Consumed by FilterEffectBuilder.BuildReferenceFilter's external
// branch. LOU-130 Phase 2 wiring.
func (r *Renderer) SetExternalSVGFetcher(fetcher func(uri string) ([]byte, error)) {
	r.externalSVGFetcher = fetcher
}

// SetScrollY sets the vertical scroll offset.
func (r *Renderer) SetScrollY(scrollY float64) {
	r.scrollY = scrollY
}

// pushClipRect pushes a clip rectangle onto the stack. Direct pixel-writing
// operations (like gradient rendering) must respect these bounds.
func (r *Renderer) pushClipRect(x, y, w, h float64) {
	r.clipStack = append(r.clipStack, clipRect{x, y, w, h})
}

// popClipRect removes the most recent clip rectangle from the stack.
func (r *Renderer) popClipRect() {
	if len(r.clipStack) > 0 {
		r.clipStack = r.clipStack[:len(r.clipStack)-1]
	}
}

// activeClipBounds returns the intersection of all active clip rectangles.
// Returns the image bounds if no clips are active.
func (r *Renderer) activeClipBounds() (x0, y0, x1, y1 int) {
	bounds := r.target.Bounds()
	fx0 := float64(bounds.Min.X)
	fy0 := float64(bounds.Min.Y)
	fx1 := float64(bounds.Max.X)
	fy1 := float64(bounds.Max.Y)
	for _, c := range r.clipStack {
		cx0 := c.x
		cy0 := c.y
		cx1 := c.x + c.w
		cy1 := c.y + c.h
		if cx0 > fx0 {
			fx0 = cx0
		}
		if cy0 > fy0 {
			fy0 = cy0
		}
		if cx1 < fx1 {
			fx1 = cx1
		}
		if cy1 < fy1 {
			fy1 = cy1
		}
	}
	return int(math.Round(fx0)), int(math.Round(fy0)), int(math.Round(fx1)), int(math.Round(fy1))
}

// SetCounterStyles registers custom @counter-style rules for list marker rendering.
func (r *Renderer) SetCounterStyles(rules []css.CounterStyleRule) {
	r.counterStyles = make(map[string]css.CounterStyleRule, len(rules))
	for _, rule := range rules {
		r.counterStyles[rule.Name] = rule
	}
}

// openFont returns the fontID for the given path+size, opening it if needed.
// The path is parsed into a logical (family, variant) pair before calling
// dc.OpenTemporaryFont so that IPC-only providers (e.g. mazzy fontsvc) receive
// a resolvable family name rather than the raw filesystem path.
//
// Uses OpenTemporaryFont so that fontIDs returned during a single Render
// call can be released en masse at the end via closeTempFonts. Fonts that
// dedupe against the provider's permanent pool (system fonts, repeats
// across renders) come back as permanent-range fontIDs whose
// CloseTemporaryFont is a no-op — so the open/close discipline is
// uniform regardless of which pool actually services the request.
func (r *Renderer) openFont(fontPath string, fontSize float64) int32 {
	size := int32(math.Round(fontSize))
	key := fontCacheKey{path: fontPath, size: size}
	r.fontCacheMu.Lock()
	defer r.fontCacheMu.Unlock()
	if id, ok := r.fontCache[key]; ok {
		return id
	}
	family, variant := text.FontPathToFamilyVariant(fontPath)
	metrics, err := r.dc.OpenTemporaryFont(family, variant, size)
	if err != nil {
		return -1
	}
	r.fontCache[key] = metrics.FontID
	r.tempFontIDs = append(r.tempFontIDs, metrics.FontID)
	return metrics.FontID
}

// closeTempFonts releases every fontID openFont produced during the
// current Render call, then drops the per-render fontCache. Permanent-
// range fontIDs close to no-op at the provider level. The cache is
// cleared so the next Render re-opens (and re-defers-close) cleanly —
// without this, a stale fontID could survive into the next render
// where the provider may have recycled the underlying slot.
//
// Called via defer from each public Render entry point.
func (r *Renderer) closeTempFonts() {
	r.fontCacheMu.Lock()
	defer r.fontCacheMu.Unlock()
	for _, id := range r.tempFontIDs {
		_ = r.dc.CloseTemporaryFont(id)
	}
	r.tempFontIDs = r.tempFontIDs[:0]
	if len(r.fontCache) > 0 {
		r.fontCache = make(map[fontCacheKey]int32)
	}
}

// Render paints the box tree onto the image. Clears the entire underlying
// DrawContext to white first; intended for the standalone case where the DC
// owns the whole image. Embedded callers (e.g. WebInteractor inside a larger
// mancini scene) should use [RenderEmbedded] instead so the surrounding DC
// content isn't blanked.
//
// Implements CSS 2.1 Appendix E paint order (simplified).
func (r *Renderer) Render(boxes []*layout.Box) {
	r.embeddedViewport.W = 0
	r.embeddedViewport.H = 0

	r.tempFontIDs = r.tempFontIDs[:0]
	defer r.closeTempFonts()

	// CSS 2.1 §14.2: Canvas background.
	// The root element's background becomes the canvas background.
	// If the root element (html) has no background, use the body's background.
	r.dc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	r.dc.Clear()

	r.paintBoxes(boxes)
}

// RenderEmbedded paints the box tree onto the DrawContext without calling
// dc.Clear(). The white canvas background is painted with FillRectangle
// scoped to (0,0)-(viewportW,viewportH), so any clip the caller pushed is
// respected and pixels outside the viewport are left untouched. Use this
// when the renderer's DrawContext is shared with other UI components
// (e.g. mancini's WebInteractor).
//
// The viewport is also recorded so subsequent canvas-background fills
// triggered by paintCanvasBackground / fillCanvasWithBackground use a
// scoped FillRectangle instead of dc.Clear().
func (r *Renderer) RenderEmbedded(boxes []*layout.Box, viewportW, viewportH float64) {
	r.embeddedViewport.W = viewportW
	r.embeddedViewport.H = viewportH

	r.tempFontIDs = r.tempFontIDs[:0]
	defer r.closeTempFonts()

	r.dc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	r.dc.FillRectangle(0, 0, viewportW, viewportH)

	r.paintBoxes(boxes)
}

// fillCanvasScoped paints the canvas background with the current fill colour.
// In embedded mode it fills only the recorded viewport rectangle so the host
// DrawContext's surrounding pixels are untouched; in standalone mode it
// delegates to dc.Clear() which fills the entire underlying image.
func (r *Renderer) fillCanvasScoped() {
	if r.embeddedViewport.W > 0 && r.embeddedViewport.H > 0 {
		r.dc.FillRectangle(0, 0, r.embeddedViewport.W, r.embeddedViewport.H)
		return
	}
	r.dc.Clear()
}

// paintBoxes performs the per-box paint pass shared by [Render] and
// [RenderEmbedded]. The canvas-background fill is the caller's responsibility.
func (r *Renderer) paintBoxes(boxes []*layout.Box) {
	// Build the renderer-wide SVG resource registry by walking every
	// box subtree for inline `<svg>` SVGRoots and merging their
	// per-root registries. `url(#id)` references on any element on
	// the page can then resolve against this single registry. See
	// svgResources comment on the Renderer type.
	r.svgResources = collectSVGResources(boxes)
	// Phase 6: detect reference cycles. Cycle-flagged resources
	// (a self-referential `<mask>`, two masks pointing at each other,
	// etc.) are treated as `none` at paint time so the painters don't
	// infinite-loop. Mirrors Blink's SVGResourcesCycleSolver pass that
	// runs after the registry is built and before any paint dispatch.
	svg.SolveResourceCycles(r.svgResources)
	r.paintCanvasBackground(boxes)

	// Paint via PaintLayer tree (CSS 2.1 Appendix E stacking order).
	for _, box := range boxes {
		layer := BuildPaintTree(box)
		// CSS 2.1 §14.2: root element's background paints the entire canvas.
		// If the root has a background, it paints the canvas. If not, body's
		// background is promoted to the canvas and body's layer also gets
		// PaintsCanvasBackground=true so its background-image uses the ICB
		// as the positioning area (CSS Backgrounds §7.2).
		//
		// CSS Containment 1/2 §3: containment on the root suppresses all
		// canvas-background propagation (see paintCanvasBackground for the
		// matching guard on the direct fill path).
		if canPropagateBackground(box) {
			if r.hasBackground(box) {
				layer.PaintsCanvasBackground = true
			} else {
				// Body box may be absent from the box tree when an empty
				// body collapses through margins (CSS 2.1 §8.3.1). Per
				// CSS Backgrounds 3 §2.11.2 / Blink's
				// BackgroundTransfersToView, the body's bg still
				// propagates to the canvas regardless of whether body
				// produced a fragment. Synthesize a minimal body
				// PaintLayer attached to the root layer so the standard
				// promotion path can paint body's bg on the canvas.
				ensureBodyPaintLayerForPropagation(layer)
				// Root has no background — promote body's layer.
				// Body is a flow child of the root layer in most cases.
				r.promoteBodyCanvasBackground(layer)
			}
		}
		r.paintLayer(layer)
	}
}

// ensureBodyPaintLayerForPropagation synthesizes a body PaintLayer + Box
// when body has no fragment in the box tree but its style has a
// propagating background. Per CSS Backgrounds 3 §2.11.2, body's bg
// propagates to the canvas regardless of whether body produced a layout
// fragment; mirrors Blink's BackgroundTransfersToView contract
// (layout_box_model_object.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f),
// which paints the propagated bg from the View, not from body's box.
//
// The synthetic body box is sized to the root's padding box (the
// background positioning area per §2.11.2) and inserted as a FlowChild
// so promoteBodyCanvasBackground can mark it PaintsCanvasBackground=true.
// We leave body's style intact; the standard drawBackground path consumes
// it and paints the canvas-extent bg via the PaintsCanvasBackground flag.
func ensureBodyPaintLayerForPropagation(rootLayer *PaintLayer) {
	if rootLayer == nil || rootLayer.Box == nil {
		return
	}
	rootBox := rootLayer.Box

	// Find body's style via the LayoutInputNode children. This walks the
	// layout-input tree, which retains body's style regardless of whether
	// body produced a fragment (collapse-through drops the fragment but
	// not the layout-input node).
	if rootBox.LayoutNode == nil {
		return
	}
	for _, kid := range rootBox.LayoutNode.Children() {
		if kid == nil {
			continue
		}
		dom := kid.DOMNode
		if dom == nil || dom.TagName != "body" {
			continue
		}
		bodyStyle := kid.Style()
		if bodyStyle == nil {
			return
		}

		// Skip if body's bg doesn't propagate. canPropagateBackground (style
		// containment) and bg-presence are both checked here so we don't
		// allocate a synthetic box for a body that wouldn't paint anyway.
		if bodyStyle.HasAnyContainment() {
			return
		}
		if !hasStyleBackground(bodyStyle) {
			return
		}

		// Check whether the root layer already has body in FlowChildren
		// (or any z-list). If so, the standard path handles it.
		if findBodyLayer(rootLayer) != nil {
			return
		}

		// Synthesize a body Box that aligns with the root's padding box
		// (CSS Backgrounds 3 §2.11.2: the positioning area for the
		// promoted body background is the root element's padding box).
		bodyBox := &layout.Box{
			Node:       dom,
			Style:      bodyStyle,
			X:          rootBox.X + rootBox.Border.Left,
			Y:          rootBox.Y + rootBox.Border.Top,
			Width:      rootBox.Width - rootBox.Border.Left - rootBox.Border.Right,
			Height:     rootBox.Height - rootBox.Border.Top - rootBox.Border.Bottom,
			Parent:     rootBox,
			LayoutNode: kid,
		}
		bodyLayer := newPaintLayer(bodyBox)
		rootLayer.FlowChildren = append(rootLayer.FlowChildren, bodyLayer)
		return
	}
}

// findBodyLayer returns the PaintLayer for the body element if one
// already exists among rootLayer's children. Returns nil otherwise.
func findBodyLayer(rootLayer *PaintLayer) *PaintLayer {
	isBody := func(l *PaintLayer) bool {
		return l != nil && l.Box != nil && l.Box.Node != nil && l.Box.Node.TagName == "body"
	}
	for _, child := range rootLayer.FlowChildren {
		if isBody(child) {
			return child
		}
	}
	for _, child := range rootLayer.AutoZero {
		if isBody(child) {
			return child
		}
	}
	for _, child := range rootLayer.NegativeZ {
		if isBody(child) {
			return child
		}
	}
	for _, child := range rootLayer.PositiveZ {
		if isBody(child) {
			return child
		}
	}
	return nil
}

// hasStyleBackground returns true if a style has a visible background
// color or image. Counterpart of (*Renderer).hasBackground that operates
// directly on a *css.Style without requiring a Box.
func hasStyleBackground(s *css.Style) bool {
	if s == nil {
		return false
	}
	if bgStr, ok := s.Get("background-color"); ok && bgStr != "" && bgStr != "transparent" {
		if bgColor, ok := css.ParseColor(bgStr); ok && bgColor.A > 0 {
			return true
		}
	}
	if _, ok := s.GetBackgroundImage(); ok {
		return true
	}
	if val, ok := s.Get("background-image"); ok && isGradientValue(val) {
		return true
	}
	return false
}

// promoteBodyCanvasBackground searches the root layer's children for body
// and sets PaintsCanvasBackground=true on body's layer, per CSS 2.1 §14.2.
// CSS Backgrounds Level 3 §2.11.2: the positioning area for the promoted
// body background is the root element's padding box.
//
// CSS Containment 1/2 §3: if the body has any `contain` value other than
// `none`, its background does not propagate — `canPropagateBackground` short
// circuits the layer promotion.
func (r *Renderer) promoteBodyCanvasBackground(rootLayer *PaintLayer) {
	rootBox := rootLayer.Box
	tryPromote := func(childLayer *PaintLayer) bool {
		if childLayer.Box == nil || childLayer.Box.Node == nil ||
			childLayer.Box.Node.TagName != "body" {
			return false
		}
		if canPropagateBackground(childLayer.Box) {
			childLayer.PaintsCanvasBackground = true
			childLayer.CanvasBackgroundRootBox = rootBox
		}
		return true
	}
	for _, childLayer := range rootLayer.FlowChildren {
		if tryPromote(childLayer) {
			return
		}
	}
	// Body could be in AutoZero if it establishes a stacking context.
	for _, childLayer := range rootLayer.AutoZero {
		if tryPromote(childLayer) {
			return
		}
	}
	for _, childLayer := range rootLayer.NegativeZ {
		if tryPromote(childLayer) {
			return
		}
	}
	for _, childLayer := range rootLayer.PositiveZ {
		if tryPromote(childLayer) {
			return
		}
	}
}

// paintCanvasBackground implements CSS 2.1 §14.2 canvas background propagation.
// If the root element has a background, use it. Otherwise, propagate from body.
//
// CSS Containment Level 1/2 §3 (https://drafts.csswg.org/css-contain-1/#contain-property):
// when an element has any form of containment (layout, paint, size, or style),
// it establishes an independent containment context and its background MUST NOT
// propagate to the canvas. If the root (html) is contained, body propagation is
// also suppressed — containment on the root disables the canvas-propagation
// machinery entirely for its subtree. Mirrors Blink's `ViewPainter::PaintRootGroup`
// guard at chromium @ 4883d11fef.
func (r *Renderer) paintCanvasBackground(boxes []*layout.Box) {
	if len(boxes) == 0 {
		return
	}
	root := boxes[0]

	// Containment on the root suppresses all canvas-background propagation
	// for the document (both root's own background and body's).
	if canPropagateBackground(root) {
		// Check if the root element has a background.
		if r.hasBackground(root) {
			r.fillCanvasWithBackground(root)
			return
		}

		// Root has no background — look for body element among its children.
		for _, child := range root.Children {
			if child.Node != nil && child.Node.TagName == "body" {
				if canPropagateBackground(child) && r.hasBackground(child) {
					r.fillCanvasWithBackground(child)
				}
				return
			}
		}
	}
}

// canPropagateBackground reports whether the given box's background is
// eligible for promotion to the canvas. Per CSS Containment 1/2 §3, any
// `contain` value other than `none` (layout, paint, size, style, or any
// combination) suppresses background propagation.
func canPropagateBackground(box *layout.Box) bool {
	if box == nil || box.Style == nil {
		return true
	}
	return !box.Style.HasAnyContainment()
}

// hasBackground returns true if a box has a visible background
// (non-transparent background-color or a background-image/gradient).
func (r *Renderer) hasBackground(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	// Check background-color.
	if bgStr, ok := box.Style.Get("background-color"); ok && bgStr != "" && bgStr != "transparent" {
		if bgColor, ok := css.ParseColor(bgStr); ok && bgColor.A > 0 {
			return true
		}
	}
	// Check background-image (url or gradient).
	if _, ok := box.Style.GetBackgroundImage(); ok {
		return true
	}
	if val, ok := box.Style.Get("background-image"); ok && isGradientValue(val) {
		return true
	}
	return false
}

// fillCanvasWithBackground fills the entire canvas with the box's background color.
func (r *Renderer) fillCanvasWithBackground(box *layout.Box) {
	if box.Style == nil {
		return
	}
	bgStr, ok := box.Style.Get("background-color")
	if !ok {
		return
	}
	bgColor, ok := css.ParseColor(bgStr)
	if !ok || bgColor.A == 0 {
		return
	}
	r.dc.SetColor(color.RGBA{
		R: bgColor.R,
		G: bgColor.G,
		B: bgColor.B,
		A: uint8(bgColor.A * 255),
	})
	r.fillCanvasScoped()
}

// clampColor clamps RGBA values to [0,255] and returns a color.RGBA.
func clampColor(rv, g, b, a float64) color.RGBA {
	clamp := func(v float64) uint8 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	return color.RGBA{R: clamp(rv), G: clamp(g), B: clamp(b), A: clamp(a)}
}

// lookupSVGResources returns the renderer-wide aggregated SVG
// resource registry. Built by collectSVGResources at the top of every
// Render/RenderEmbedded call. Returns nil when there are no inline
// SVGs on the page — callers must guard against nil before Lookup*.
func (r *Renderer) lookupSVGResources() *svg.SVGResourceRegistry {
	if r == nil {
		return nil
	}
	return r.svgResources
}

// collectSVGResources walks `boxes` and every descendant box,
// gathering each inline `<svg>` element's SVGRoot.Resources into a
// single merged registry that the renderer exposes to `url(#id)`
// resolvers. Mirrors how Blink's SVGTreeScopeResources is a per-
// Document namespace — louis14 builds it per render frame because we
// don't yet have a Document-lifetime renderer (the layout pass owns
// the SVGRoot lifetime; the renderer is recreated per paint).
//
// Conflicts (same id registered by two different SVGs) follow the
// first-wins rule from each per-root RegisterXxx; the renderer
// preserves that ordering by walking boxes in pre-order, which
// matches document order for HTML.
func collectSVGResources(boxes []*layout.Box) *svg.SVGResourceRegistry {
	merged := svg.NewSVGResourceRegistry()
	for _, b := range boxes {
		mergeSVGResourcesFromBox(b, merged)
	}
	return merged
}

// mergeSVGResourcesFromBox is the recursive worker for
// collectSVGResources: pre-order walk that copies each SVGRoot's
// per-root registry contents into `dst`.
func mergeSVGResourcesFromBox(box *layout.Box, dst *svg.SVGResourceRegistry) {
	if box == nil {
		return
	}
	if box.LayoutNode != nil {
		if root, ok := box.LayoutNode.SVGRoot.(*svg.SVGRoot); ok && root != nil && root.Resources != nil {
			mergeSVGRegistries(dst, root.Resources)
		}
	}
	for _, child := range box.Children {
		mergeSVGResourcesFromBox(child, dst)
	}
}

// mergeSVGRegistries copies `src`'s contents into `dst` using the
// public RegisterXxx methods so the first-wins rule is preserved.
// Phase 5 needs this because each SVGRoot has its own per-root
// registry — gradient/pattern paint servers and Phase 5's clippers/
// maskers — and the renderer needs a single namespace across all
// SVGs on the page.
//
// Implementation detail: the per-root maps are unexported, so this
// helper relies on the per-root registry exposing iteration via the
// kind-specific Lookup paths. To avoid widening the API surface
// beyond what Phase 5 needs, we add a per-kind drain helper on the
// registry. See svg_resource_registry.go.
func mergeSVGRegistries(dst, src *svg.SVGResourceRegistry) {
	if dst == nil || src == nil {
		return
	}
	src.ForEachPaintServer(func(s svg.SVGResourcePaintServer) {
		dst.RegisterPaintServer(s)
	})
	src.ForEachClipper(func(c *svg.SVGResourceClipper) {
		dst.RegisterClipper(c)
	})
	src.ForEachMasker(func(m *svg.SVGResourceMasker) {
		dst.RegisterMasker(m)
	})
	src.ForEachFilter(func(f *svg.SVGResourceFilter) {
		dst.RegisterFilter(f)
	})
}

// paintLayer paints a PaintLayer and its descendants using the pre-built
// stacking order. All paint decisions use pre-computed fields — no Style access.
func (r *Renderer) paintLayer(layer *PaintLayer) {
	if layer == nil {
		return
	}
	// Pre-computed visibility and opacity.
	if !layer.Visible {
		return
	}
	if layer.Opacity <= 0 {
		return
	}

	// CSS mask-image: render subtree to offscreen buffer, apply mask, composite.
	if layer.HasMaskImage {
		r.paintLayerWithMask(layer)
		return
	}

	// CSS Filters: render subtree to offscreen buffer, apply filters, composite.
	if layer.HasFilter {
		r.paintLayerWithFilter(layer)
		return
	}

	// CSS mix-blend-mode: render to offscreen buffer and blend-composite.
	if layer.BlendMode != css.MixBlendModeNormal && layer.BlendMode != "" {
		r.paintLayerWithBlend(layer)
		return
	}

	// CSS Transforms 1 §6 / CSS Backgrounds 3 §3.13: the canvas background
	// (root or propagated body bg) is owned by the root canvas, NOT the
	// layer that supplies it. A `transform` on the body or root element must
	// NOT transform the canvas bg. Paint the canvas bg BEFORE pushing the
	// layer transform; drawBackground will short-circuit on the layer it
	// already painted so we don't double-paint inside the transformed dc.
	// Mirrors Blink's ViewPainter::PaintRootGroup which paints the canvas
	// bg before the root element's transform-property scope at
	// view_painter.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	r.paintLayerCanvasBackground(layer)

	// Apply CSS transform if present (wraps around opacity handling).
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth++
	}

	if layer.Opacity < 1.0 {
		// CSS3 Color §4.2: render subtree to offscreen buffer and composite.
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}

	if layer.HasTransformPaint {
		r.transformDepth--
	}
	if layer.HasTransform {
		r.dc.Pop()
	}
	if r.canvasBgPaintedLayer == layer {
		r.canvasBgPaintedLayer = nil
	}
}

// paintLayerCanvasBackground paints the canvas-propagated background (color
// + image + gradient) outside the layer's transform context. Per CSS
// Transforms 1 §6 and Backgrounds 3 §3.13, the canvas background belongs to
// the root canvas in viewport coordinates and must not be transformed with
// the body/root element. After painting, drawBackground will skip this
// layer's bg so it isn't redrawn (and transformed) inside paintLayerContent.
//
// When `layer` is the root and the propagated body is one of its
// descendant layers (the common case — body PaintsCanvasBackground=true
// after promotion), we also paint body's bg here, before the root's
// transform is applied. This is required because in louis14's paint walk
// non-SC descendants of root are visited via paintDescendantPhase
// INSIDE the root's transform scope, so a body that doesn't establish
// its own transform/SC would otherwise have its propagated bg rotated
// with the root.
func (r *Renderer) paintLayerCanvasBackground(layer *PaintLayer) {
	if layer == nil {
		return
	}
	if layer.PaintsCanvasBackground {
		// Mark before painting so drawBackground (called from paintSelfDecorations
		// nested below paintLayerContent) skips the in-transform repaint.
		r.drawBackground(layer)
		r.canvasBgPaintedLayer = layer
	}
	// Hoist any descendant canvas-bg layer's paint out of this layer's
	// transform — but only when this layer (or an ancestor) actually has
	// a transform paint context. Without a transform, the descendant's
	// canvas-bg paint happens in the normal stacking order inside
	// paintLayerContent (CSS 2.1 Appendix E step 1: canvas-bg first, then
	// NegativeZ, etc.), and the hoist would only move the bg paint
	// EARLIER — covering NegativeZ children that would otherwise be
	// painted ON TOP of the canvas bg. The transform-gated path is what
	// fixes tests 002/006/007 (body has propagated bg, ancestor has
	// transform); the no-transform path stays out of stacking order.
	if layer.HasTransformPaint || r.transformDepth > 0 {
		r.paintDescendantCanvasBackground(layer)
	}
}

// paintDescendantCanvasBackground walks layer's direct children for a
// canvas-bg-propagating descendant (typically body when html has no own
// bg) and paints it out-of-transform. Idempotent — already-painted
// layers are skipped via canvasBgPaintedDescendants tracking.
//
// Only called when the ancestor has a transform paint context, since
// without a transform the descendant's bg should paint in the standard
// stacking order (where NegativeZ paints over canvas-bg).
func (r *Renderer) paintDescendantCanvasBackground(layer *PaintLayer) {
	if layer == nil {
		return
	}
	visit := func(child *PaintLayer) {
		if child == nil || !child.PaintsCanvasBackground {
			return
		}
		if r.canvasBgPaintedLayer == child {
			return
		}
		// Skip layers whose bg paint would be a no-op — avoids inserting
		// the layer into canvasBgPaintedDescendants for a non-visible bg
		// (which would silently suppress the descendant's no-op redraw
		// without any benefit).
		if child.Box == nil || child.Box.Style == nil {
			return
		}
		if !hasStyleBackground(child.Box.Style) {
			return
		}
		r.drawBackground(child)
		// Don't set r.canvasBgPaintedLayer here — we want drawBackground
		// to also skip the in-transform repaint when paintDescendantPhase
		// eventually reaches this child. Use the descendant-tracking flag
		// instead so the suppression covers both the SC paintLayer path
		// (which clears canvasBgPaintedLayer on exit) and the paintSelf
		// Decorations path reached through paintDescendantPhase.
		r.canvasBgPaintedDescendants[child] = struct{}{}
	}
	if r.canvasBgPaintedDescendants == nil {
		r.canvasBgPaintedDescendants = make(map[*PaintLayer]struct{})
	}
	for _, child := range layer.FlowChildren {
		visit(child)
	}
	for _, child := range layer.AutoZero {
		visit(child)
	}
	for _, child := range layer.NegativeZ {
		visit(child)
	}
	for _, child := range layer.PositiveZ {
		visit(child)
	}
}

// paintLayerWithMask renders the entire layer subtree into an offscreen buffer,
// generates or loads the mask image, and applies per-pixel alpha masking.
//
// Per CSS Masking Level 1:
//   - url() images use the mask's alpha channel (mask-mode: alpha)
//   - Gradients use the mask's luminance (mask-mode: luminance)
//   - The default mask-mode is "match-source" which selects between the two
//
// For each pixel: result.A = element.A * maskValue
// where maskValue is either mask.A (alpha mode) or luminance(mask) (luminance mode).
func (r *Renderer) paintLayerWithMask(layer *PaintLayer) {
	box := layer.Box
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.X+box.Width)) - bx + 1
	bh := int(math.Ceil(box.Y+box.Height)) - by + 1

	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		// Fallback: render without mask to avoid OOM.
		r.paintLayerContent(layer)
		return
	}

	// Step 1: Render the element to an offscreen buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	origDC := r.dc
	origTarget := r.target
	childDC := origDC.NewChildContext(buf)
	r.dc = childDC
	r.target = buf
	r.dc.Translate(float64(-bx), float64(-by))

	// Paint with transforms and opacity in the offscreen buffer.
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth++
	}
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth--
	}
	if layer.HasTransform {
		r.dc.Pop()
	}

	// Restore original context.
	r.dc = origDC
	r.target = origTarget

	// Step 2: Generate the mask image.
	maskVal := layer.MaskImage
	// Default mask-mode is match-source: for a mask-image of type <image>
	// (a CSS gradient or a raster image) the alpha values of the mask
	// layer image are used as the mask values — NOT luminance. Luminance
	// mode is the default only for an SVG <mask> element reference.
	// (CSS Masking Level 1 §6.4.)
	useLuminance := false
	var maskImg *image.RGBA

	// SVG fast path: if mask-image is `url(#id)` and the id resolves to
	// an SVG `<mask>` element in the renderer-wide SVG resource
	// registry, rasterize that mask's children into an alpha buffer and
	// apply with kDstIn directly. Mirrors Blink's
	// SVGMaskPainter::PaintSVGMaskForCSS dispatch (when the mask
	// reference resolves to an SVGMaskElement, take the SVG path; else
	// fall through to the CSS Images-3 image-or-gradient path). This
	// also implements the css-masking Phase 3 promise the foundation
	// claims (SVG `<mask>` resolution: mask-opacity-1d gate).
	//
	// Note: per CSS Masking 1 §3.7 the rendering order is filter →
	// mask → opacity → composite. The buffer already had opacity
	// applied above (via PushGroup/PopGroupWithAlpha). We undo that
	// by repainting without opacity below if an SVG mask is in play,
	// because mazarin's PushGroup/PopGroupWithAlpha applies opacity
	// using PRE-multiplied alpha math that produces a slightly
	// different pixel value than the per-channel-times-alpha math the
	// plain CSS-color rendering uses. To match the reftest's
	// reference page (which uses rgba(c, a) and goes through plain
	// CSS-color rendering), the SVG-mask code path must apply
	// opacity post-mask via the SAME per-channel multiplication.
	if id, ok := css.ParseURLReference(maskVal); ok {
		if registry := r.lookupSVGResources(); registry != nil {
			if masker, ok := registry.LookupMasker(id); ok && masker != nil && masker.CycleState() != svg.SVGCycleHasCycle {
				// Repaint the layer WITHOUT opacity, into a fresh
				// buffer. We re-create `buf` because the existing
				// one above had opacity baked in via PushGroup.
				freshBuf := image.NewRGBA(image.Rect(0, 0, bw, bh))
				freshDC := origDC.NewChildContext(freshBuf)
				saveDC := r.dc
				saveTarget := r.target
				r.dc = freshDC
				r.target = freshBuf
				r.dc.Translate(float64(-bx), float64(-by))
				if layer.HasTransform {
					r.dc.Push()
					r.applyTransforms(layer)
				}
				if layer.HasTransformPaint {
					r.transformDepth++
				}
				r.paintLayerContent(layer)
				if layer.HasTransformPaint {
					r.transformDepth--
				}
				if layer.HasTransform {
					r.dc.Pop()
				}
				r.dc = saveDC
				r.target = saveTarget

				refBox := geometry.NewRectF(box.X, box.Y, box.Width, box.Height)
				maskBuf := r.rasterizeSVGMask(masker, refBox, bw, bh, bx, by)
				if maskBuf != nil {
					applySVGMaskToBuffer(freshBuf, maskBuf, masker.MaskType)
					// Composite freshBuf onto the page canvas using
					// the same source-over math the existing CSS
					// color-fill path produces, then apply opacity
					// using the SAME compositor as a constant alpha
					// modulation. This is what makes the test render
					// pixel-identical to the rgba() ref render that
					// the WPT reftest matches against (CSS Masking 1
					// §3.7 rendering order: filter → mask → opacity →
					// composite, with the louis14 canvas composite
					// math matched to mazarin's gg backend).
					compositeBufferWithOpacityOnto(r.target, freshBuf, bx, by, layer.Opacity)
					return
				}
			}
		}
	}

	if isGradientValue(maskVal) {
		// Gradient mask: <image> type → match-source resolves to alpha mode.
		maskImg = image.NewRGBA(image.Rect(0, 0, bw, bh))

		// Create a temporary renderer to draw the gradient into the mask buffer.
		maskR := &Renderer{
			dc:           origDC.NewChildContext(maskImg),
			target:       maskImg,
			fonts:        r.fonts,
			imageFetcher: r.imageFetcher,
		}
		// The mask layer image is positioned over the element's border box
		// (default mask-origin), so the gradient must be drawn at the
		// element's box geometry within the offscreen buffer — the element
		// origin maps to (box.X-bx, box.Y-by) and the gradient spans the
		// border box's exact width/height. Drawing it over the padded
		// buffer extent instead would shift the gradient line and the
		// per-row colour banding off the element's own paint geometry.
		maskR.drawLinearGradient(maskVal,
			box.X-float64(bx), box.Y-float64(by), box.Width, box.Height)
	} else if strings.HasPrefix(maskVal, "url(") {
		// Image mask: load the image and use its alpha channel.
		src := maskVal
		src = strings.TrimPrefix(src, "url(")
		src = strings.TrimSuffix(src, ")")
		src = strings.Trim(src, `"'`)
		if loadedImg, err := images.LoadImageWithFetcher(src, r.imageFetcher); err == nil {
			// Scale/stretch mask image to element bounds (default mask-size: auto → cover).
			maskImg = image.NewRGBA(image.Rect(0, 0, bw, bh))
			imgBounds := loadedImg.Bounds()
			imgW := imgBounds.Dx()
			imgH := imgBounds.Dy()
			if imgW > 0 && imgH > 0 {
				for py := 0; py < bh; py++ {
					srcY := py * imgH / bh
					if srcY >= imgH {
						srcY = imgH - 1
					}
					for px := 0; px < bw; px++ {
						srcX := px * imgW / bw
						if srcX >= imgW {
							srcX = imgW - 1
						}
						r, g, b, a := loadedImg.At(imgBounds.Min.X+srcX, imgBounds.Min.Y+srcY).RGBA()
						maskImg.SetRGBA(px, py, color.RGBA{
							R: uint8(r >> 8),
							G: uint8(g >> 8),
							B: uint8(b >> 8),
							A: uint8(a >> 8),
						})
					}
				}
			}
		}
	}

	if maskImg == nil {
		// No valid mask → just composite the element buffer as-is.
		r.dc.DrawImage(buf, bx, by)
		return
	}

	// Step 3: Apply the mask — modulate element alpha by mask value.
	for py := 0; py < bh; py++ {
		for px := 0; px < bw; px++ {
			ei := buf.PixOffset(px, py)
			mi := maskImg.PixOffset(px, py)

			var maskFactor float64
			if useLuminance {
				// Luminance = 0.2126*R + 0.7152*G + 0.0722*B (sRGB coefficients).
				// Premultiplied alpha: un-premultiply first.
				ma := maskImg.Pix[mi+3]
				if ma == 0 {
					maskFactor = 0
				} else {
					maf := float64(ma) / 255.0
					mr := float64(maskImg.Pix[mi+0]) / 255.0 / maf
					mg := float64(maskImg.Pix[mi+1]) / 255.0 / maf
					mb := float64(maskImg.Pix[mi+2]) / 255.0 / maf
					lum := 0.2126*mr + 0.7152*mg + 0.0722*mb
					maskFactor = lum * maf
				}
			} else {
				// Alpha mode: use mask's alpha channel directly.
				maskFactor = float64(maskImg.Pix[mi+3]) / 255.0
			}

			// Multiply all RGBA channels by maskFactor (premultiplied alpha).
			buf.Pix[ei+0] = uint8(float64(buf.Pix[ei+0]) * maskFactor)
			buf.Pix[ei+1] = uint8(float64(buf.Pix[ei+1]) * maskFactor)
			buf.Pix[ei+2] = uint8(float64(buf.Pix[ei+2]) * maskFactor)
			buf.Pix[ei+3] = uint8(float64(buf.Pix[ei+3]) * maskFactor)
		}
	}

	// Step 4: Composite the masked buffer onto the canvas.
	r.dc.DrawImage(buf, bx, by)
}

// paintLayerWithFilter renders the entire layer subtree into an offscreen
// buffer sized to the filter region, runs the FilterEffect graph over it, and
// composites the result back. Mirrors Blink's FilterPainter: the element
// subtree is recorded into a source buffer, fed to the filter graph as
// SourceGraphic, and the graph output replayed.
func (r *Renderer) paintLayerWithFilter(layer *PaintLayer) {
	box := layer.Box

	// Reference box: the element's border box in absolute device pixels.
	rbx := int(math.Floor(box.X))
	rby := int(math.Floor(box.Y))
	rbw := int(math.Ceil(box.X+box.Width)) - rbx
	rbh := int(math.Ceil(box.Y+box.Height)) - rby
	referenceBox := image.Rect(rbx, rby, rbx+rbw, rby+rbh)

	// SourceGraphic extent: the element subtree's paint bounds, which can
	// extend beyond the border box (positioned/overflowing descendants).
	// Blink's filter SourceGraphic is the recorded layer contents including
	// overflow; for drop-shadow especially the shadow is cast from the whole
	// painted shape, so the source buffer must cover it. drop-shadow-clipped
	// proves this: a descendant offset out of the border box still casts a
	// shadow.
	sourceExtent := referenceBox.Union(subtreeBorderBoxBounds(box))

	// Build the filter graph. The builder computes the filter region by
	// walking the graph's MapRect chain (blur/drop-shadow inflate it).
	// Phase 7: SVG `<filter>` resource registry is supplied so
	// `filter: url(#id)` references resolve via BuildReferenceFilter.
	builder := &FilterEffectBuilder{
		ReferenceBox:       referenceBox,
		CurrentColor:       layer.TextColor,
		Resources:          r.lookupSVGResources(),
		ExternalSVGFetcher: r.externalSVGFetcher,
	}
	filter := builder.BuildFilterEffect(layer.Filters)
	if filter == nil {
		// No supported CSS filter functions (e.g. only url() references,
		// not yet handled) — paint without filtering rather than dropping
		// the element.
		r.paintLayerContentWithEffects(layer)
		return
	}

	// The filter region is the graph's forward-mapped bounds over the actual
	// source extent (not just the border box) so the offscreen buffer covers
	// every pixel the filter draws.
	filter.FilterRegion = filter.MapRect(sourceExtent)
	region := filter.FilterRegion
	bx, by := region.Min.X, region.Min.Y
	bw, bh := region.Dx(), region.Dy()
	if bw <= 0 || bh <= 0 || bw > 8000 || bh > 8000 {
		// Fallback: render without filters to avoid OOM.
		r.paintLayerContentWithEffects(layer)
		return
	}

	// Render the layer subtree into the source buffer (filter-region sized).
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	origDC := r.dc
	origTarget := r.target
	childDC := origDC.NewChildContext(buf)
	r.dc = childDC
	r.target = buf
	r.dc.Translate(float64(-bx), float64(-by))
	r.paintLayerContentWithEffects(layer)
	r.dc = origDC
	r.target = origTarget

	// Run the FilterEffect graph and composite the output back at the
	// filter region origin.
	filter.SetSourceImage(buf)
	out := filter.Apply()
	if out == nil {
		// Match the two earlier failure paths (filter == nil, oversize buffer):
		// fall back to unfiltered paint so the element still renders rather
		// than vanishing into an invisible hole. r.dc/r.target were already
		// restored above.
		r.paintLayerContentWithEffects(layer)
		return
	}
	r.dc.DrawImage(out, bx, by)
}

// subtreeBorderBoxBounds returns the union of the border boxes of box and all
// its descendants, in absolute device pixels. It is the conservative paint
// extent the filter SourceGraphic must cover so that descendants painted
// outside the element's own border box (relative/absolute positioned, or
// simply overflowing) still feed the filter graph.
func subtreeBorderBoxBounds(box *layout.Box) image.Rectangle {
	if box == nil {
		return image.Rectangle{}
	}
	x0 := int(math.Floor(box.X))
	y0 := int(math.Floor(box.Y))
	x1 := int(math.Ceil(box.X + box.Width))
	y1 := int(math.Ceil(box.Y + box.Height))
	bounds := image.Rect(x0, y0, x1, y1)
	for _, child := range box.Children {
		cb := subtreeBorderBoxBounds(child)
		if !cb.Empty() {
			bounds = bounds.Union(cb)
		}
	}
	return bounds
}

// paintLayerContentWithEffects paints a layer's content applying its transform
// and opacity, used inside an offscreen buffer (the filter source). It mirrors
// the transform/opacity handling in paintLayer so a filtered element still
// honours its own transform and opacity.
func (r *Renderer) paintLayerContentWithEffects(layer *PaintLayer) {
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth++
	}
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth--
	}
	if layer.HasTransform {
		r.dc.Pop()
	}
}

// applyBackdropFilter implements CSS Filter Effects 2 backdrop-filter: it
// snapshots the backdrop image (everything painted behind the element's
// border box), runs the filter graph over it, clips the result to the
// element's border box including border-radius, and draws it back. The
// element then paints its own decorations and content on top.
//
// applyBackdropFilter is invoked from paintLayerContent before
// paintSelfDecorations, so r.target already holds the backdrop content. This
// matches the Filter Effects 2 algorithm's "backdrop root image" for the
// common case; isolation: isolate does not form a backdrop root, which is
// consistent here because r.target is the shared canvas.
func (r *Renderer) applyBackdropFilter(layer *PaintLayer) {
	box := layer.Box

	// Element's border-box region in absolute device pixels.
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.X+box.Width)) - bx
	bh := int(math.Ceil(box.Y+box.Height)) - by

	if bw <= 0 || bh <= 0 || bw > 8000 || bh > 8000 {
		return
	}
	borderBox := image.Rect(bx, by, bx+bw, by+bh)

	// A blur on the backdrop samples pixels outside the element's border
	// box, so the captured region is inflated by the blur extent — otherwise
	// the blur sees transparent black off the edges and darkens the result.
	// The filtered output is clipped back to the border box afterwards.
	// Filter Effects 2 treats the backdrop root image as effectively
	// unbounded; capturing a margin around the box approximates that.
	pad := 0
	for _, f := range layer.BackdropFilters {
		if f.Name == "blur" {
			pad = max(pad, int(math.Ceil(f.Value*3))+2)
		}
	}
	region := image.Rect(bx-pad, by-pad, bx+bw+pad, by+bh+pad)
	rw, rh := region.Dx(), region.Dy()

	// Snapshot the backdrop image: the canvas content under the region.
	backdrop := image.NewRGBA(image.Rect(0, 0, rw, rh))
	targetBounds := r.target.Bounds()
	for y := 0; y < rh; y++ {
		dy := region.Min.Y + y
		if dy < targetBounds.Min.Y || dy >= targetBounds.Max.Y {
			continue
		}
		for x := 0; x < rw; x++ {
			dx := region.Min.X + x
			if dx < targetBounds.Min.X || dx >= targetBounds.Max.X {
				continue
			}
			si := r.target.PixOffset(dx, dy)
			di := backdrop.PixOffset(x, y)
			backdrop.Pix[di+0] = r.target.Pix[si+0]
			backdrop.Pix[di+1] = r.target.Pix[si+1]
			backdrop.Pix[di+2] = r.target.Pix[si+2]
			backdrop.Pix[di+3] = r.target.Pix[si+3]
		}
	}

	// Run the filter graph over the backdrop image. backdrop-filter uses the
	// same filter-function list as filter.
	builder := &FilterEffectBuilder{
		ReferenceBox:       borderBox,
		CurrentColor:       layer.TextColor,
		Resources:          r.lookupSVGResources(),
		ExternalSVGFetcher: r.externalSVGFetcher,
	}
	filter := builder.BuildFilterEffect(layer.BackdropFilters)
	if filter == nil {
		return
	}
	filter.FilterRegion = region
	filter.SetSourceImage(backdrop)
	out := filter.Apply()
	if out == nil {
		return
	}

	// Clip the filtered backdrop to the element's border box, honouring
	// border-radius, then draw it back. The clip is the reason a
	// backdrop-filter element with rounded corners does not tint outside
	// its rounded shape.
	r.dc.Push()
	bxf, byf, bwf, bhf := float64(bx), float64(by), float64(bw), float64(bh)
	if hasBorderRadius(layer) {
		r.buildRoundedRectPath(bxf, byf, bwf, bhf, layer.BorderRadius)
	} else {
		r.dc.DrawRectangle(bxf, byf, bwf, bhf)
	}
	r.dc.Clip()
	r.dc.DrawImage(out, region.Min.X, region.Min.Y)
	r.dc.Pop()
}

// paintLayerWithBlend renders the entire layer subtree into an offscreen
// buffer, then blend-composites the result onto the destination using the
// specified CSS mix-blend-mode.
func (r *Renderer) paintLayerWithBlend(layer *PaintLayer) {
	box := layer.Box
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.Width+(box.X-float64(bx)))) + 1
	bh := int(math.Ceil(box.Height+(box.Y-float64(by)))) + 1

	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		r.paintLayerContent(layer)
		return
	}

	// Render to offscreen buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	origDC := r.dc
	origTarget := r.target
	childDC := origDC.NewChildContext(buf)
	r.dc = childDC
	r.target = buf
	r.dc.Translate(float64(-bx), float64(-by))

	// Apply transforms and opacity within the offscreen buffer.
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth++
	}
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth--
	}
	if layer.HasTransform {
		r.dc.Pop()
	}

	// Restore original DC.
	r.dc = origDC
	r.target = origTarget

	// Blend-composite the buffer onto the destination.
	blendComposite(r.target, buf, bx, by, layer.BlendMode)
}

// blendComposite performs pixel-level blend compositing of src onto dst
// at offset (ox, oy) using the specified CSS mix-blend-mode.
func blendComposite(dst *image.RGBA, src *image.RGBA, ox, oy int, mode css.MixBlendMode) {
	srcBounds := src.Bounds()
	dstBounds := dst.Bounds()

	for sy := 0; sy < srcBounds.Dy(); sy++ {
		dy := sy + oy
		if dy < dstBounds.Min.Y || dy >= dstBounds.Max.Y {
			continue
		}
		for sx := 0; sx < srcBounds.Dx(); sx++ {
			dx := sx + ox
			if dx < dstBounds.Min.X || dx >= dstBounds.Max.X {
				continue
			}

			srcOff := sy*src.Stride + sx*4
			dstOff := (dy-dstBounds.Min.Y)*dst.Stride + (dx-dstBounds.Min.X)*4

			sa := float64(src.Pix[srcOff+3]) / 255
			if sa == 0 {
				continue
			}

			sr := float64(src.Pix[srcOff]) / 255
			sg := float64(src.Pix[srcOff+1]) / 255
			sb := float64(src.Pix[srcOff+2]) / 255
			dr := float64(dst.Pix[dstOff]) / 255
			dg := float64(dst.Pix[dstOff+1]) / 255
			db := float64(dst.Pix[dstOff+2]) / 255

			var rr, rg, rb float64
			switch mode {
			case css.MixBlendModeMultiply:
				rr, rg, rb = sr*dr, sg*dg, sb*db
			case css.MixBlendModeScreen:
				rr = sr + dr - sr*dr
				rg = sg + dg - sg*dg
				rb = sb + db - sb*db
			case css.MixBlendModeOverlay:
				rr = overlayChannel(dr, sr)
				rg = overlayChannel(dg, sg)
				rb = overlayChannel(db, sb)
			case css.MixBlendModeDarken:
				rr = math.Min(sr, dr)
				rg = math.Min(sg, dg)
				rb = math.Min(sb, db)
			case css.MixBlendModeLighten:
				rr = math.Max(sr, dr)
				rg = math.Max(sg, dg)
				rb = math.Max(sb, db)
			case css.MixBlendModeDifference:
				rr = math.Abs(sr - dr)
				rg = math.Abs(sg - dg)
				rb = math.Abs(sb - db)
			default:
				rr, rg, rb = sr, sg, sb
			}

			// Source-over compositing with blended color.
			da := float64(dst.Pix[dstOff+3]) / 255
			outA := sa + da*(1-sa)
			if outA > 0 {
				dst.Pix[dstOff] = clampByte((rr*sa + dr*da*(1-sa)) / outA * 255)
				dst.Pix[dstOff+1] = clampByte((rg*sa + dg*da*(1-sa)) / outA * 255)
				dst.Pix[dstOff+2] = clampByte((rb*sa + db*da*(1-sa)) / outA * 255)
				dst.Pix[dstOff+3] = clampByte(outA * 255)
			}
		}
	}
}

// overlayChannel implements the CSS overlay blend formula for a single channel.
func overlayChannel(backdrop, source float64) float64 {
	if backdrop < 0.5 {
		return 2 * backdrop * source
	}
	return 1 - 2*(1-backdrop)*(1-source)
}

// clampFilter01 clamps a value to the [0, 1] range.
func clampFilter01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clampByte clamps a float64 to [0, 255] and returns a uint8.
func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// applyTransforms applies the CSS transform list to the draw context.
// Transforms are applied relative to the transform-origin point.
func (r *Renderer) applyTransforms(layer *PaintLayer) {
	box := layer.Box
	ox := box.X + layer.TransformOrigin[0]
	oy := box.Y + layer.TransformOrigin[1]

	// Move origin to transform-origin point.
	r.dc.Translate(ox, oy)

	// Apply transforms in order.
	for _, t := range layer.Transforms {
		switch t.Type {
		case "translate":
			tx := t.Values[0]
			ty := 0.0
			if len(t.Values) > 1 {
				ty = t.Values[1]
			}
			r.dc.Translate(tx, ty)
		case "rotate":
			// parseAngle() returns degrees; DrawContext.Rotate() takes radians.
			r.dc.Rotate(t.Values[0] * math.Pi / 180)
		case "scale":
			sx := t.Values[0]
			sy := sx
			if len(t.Values) > 1 {
				sy = t.Values[1]
			}
			r.dc.Scale(sx, sy)
		case "skew":
			// skew(ax, ay) = matrix(1, tan(ay), tan(ax), 1, 0, 0)
			ax := t.Values[0] * math.Pi / 180
			ay := 0.0
			if len(t.Values) > 1 {
				ay = t.Values[1] * math.Pi / 180
			}
			r.dc.MultiplyMatrix(1, math.Tan(ay), math.Tan(ax), 1, 0, 0)
		case "matrix":
			// matrix(a, b, c, d, e, f) — general 2D affine transform
			if len(t.Values) >= 6 {
				r.dc.MultiplyMatrix(t.Values[0], t.Values[1], t.Values[2], t.Values[3], t.Values[4], t.Values[5])
			}
		}
	}

	// Move back from transform-origin.
	r.dc.Translate(-ox, -oy)
}

// paintLayerContent paints the layer's own box and children in
// CSS 2.1 Appendix E order using the pre-sorted PaintLayer lists.
//
// The walk is split into phases (mirrors Blink's PaintLayerPainter):
//  1. Self decorations — bg, borders, shadows, outline, list marker.
//  2. NegativeZ z-children (step 2).
//  3. PhaseBackground across non-self-painting descendants (step 3).
//  4. PhaseFloat across non-self-painting descendants (step 4).
//  5. Self foreground — text + image.
//  6. PhaseForeground across non-self-painting descendants (step 5).
//  7. AutoZero + PositiveZ z-children (steps 6-7).
//
// Descendant painting is split into phases so floats (step 4) can paint
// above earlier siblings' block backgrounds but below later siblings'
// inline content — a constraint that a single DOM-order walk cannot
// satisfy for block-in-inline structures.
func (r *Renderer) paintLayerContent(layer *PaintLayer) {
	// CSS clip: rect() — clips everything including backgrounds and borders.
	// Purely physical coordinates per CSS Writing Modes §7.6.
	cssClipping := false
	if layer.HasCSSClip {
		r.dc.Push()
		cssClipping = true
		cx, cy, cw, ch := pixelSnap(layer.CSSClipRect[0], layer.CSSClipRect[1],
			layer.CSSClipRect[2], layer.CSSClipRect[3])
		r.dc.DrawRectangle(cx, cy, cw, ch)
		r.dc.Clip()
		r.pushClipRect(cx, cy, cw, ch)
	}

	// CSS clip-path: clips all content to a shape.
	clipPathActive := false
	if layer.ClipPath != nil {
		r.dc.Push()
		clipPathActive = true
		r.applyClipPath(layer)
	}

	// CSS backdrop-filter: filter the region behind the element before painting.
	if layer.HasBackdropFilter {
		r.applyBackdropFilter(layer)
	}

	// Self decorations: bg, borders, shadows, outline, list marker.
	r.paintSelfDecorations(layer)

	// Overflow clip using pre-computed rectangle.
	clipping := false
	if layer.HasClip {
		r.dc.Push()
		clipping = true
		ox, oy, ow, oh := pixelSnap(layer.ClipRect[0], layer.ClipRect[1],
			layer.ClipRect[2], layer.ClipRect[3])
		if hasBorderRadius(layer) {
			r.buildRoundedRectPath(ox, oy, ow, oh, overflowClipRadii(layer))
		} else {
			r.dc.DrawRectangle(ox, oy, ow, oh)
		}
		r.dc.Clip()
		// Push clip bounds for direct pixel writers (gradient renderer).
		r.pushClipRect(ox, oy, ow, oh)
	}

	// Step 2: Negative z-index stacking contexts.
	for _, child := range layer.NegativeZ {
		r.paintLayer(child)
	}

	// Step 3: Descendant block backgrounds (non-self-painting subtree).
	r.paintDescendantsPhase(layer, PhaseBackground)

	// Collapsed table-cell border paints inside the background phase,
	// after descendant backgrounds. paintLayerContent reaches the cell
	// when it is its own stacking context (e.g. position: relative);
	// paintDescendantPhase reaches non-SC cells. Both delegate to the
	// same helper so the spec ordering stays in one place.
	r.paintCollapsedTableCellBorder(layer)

	// Step 4: Floats (each runs its own full phase loop).
	r.paintDescendantsPhase(layer, PhaseFloat)

	// Step 5a: Self foreground — text + image content.
	r.paintSelfForeground(layer)

	// Step 5b: Descendant foreground (text/image + non-positioned SCs).
	r.paintDescendantsPhase(layer, PhaseForeground)

	// Step 6: z-index:auto positioned + z-index:0 SCs.
	for _, child := range layer.AutoZero {
		r.paintLayer(child)
	}

	// Step 7: Positive z-index stacking contexts.
	for _, child := range layer.PositiveZ {
		r.paintLayer(child)
	}

	if clipping {
		r.popClipRect()
		r.dc.Pop()
	}

	if clipPathActive {
		r.dc.Pop()
	}

	if cssClipping {
		r.popClipRect()
		r.dc.Pop()
	}
}

// paintSelfDecorations paints a layer's own background, borders,
// shadows, column rules, outline, and list marker — the subset of
// painting that precedes descendant painting in CSS 2.1 Appendix E.
// Called once per layer (not once per phase).
func (r *Renderer) paintSelfDecorations(layer *PaintLayer) {
	// empty-cells: hide — skip background, borders, and box shadows for
	// empty table cells in the separate border model (CSS 2.1 §17.6.1.1).
	// The cell still occupies space; only painting is suppressed.
	if !layer.EmptyCellHide {
		if len(layer.BoxShadows) > 0 {
			r.drawOutsetBoxShadows(layer)
		}
		r.drawBackground(layer)
		// Collapsed table cell borders paint in their own phase AFTER
		// descendants (see paintCollapsedBordersAfterDescendants). The
		// background and box shadows still belong here so descendants
		// paint above them in the normal way.
		if !layer.IsCollapsedBorderCell {
			r.drawBorders(layer)
		}
		if len(layer.BoxShadows) > 0 {
			r.drawInsetBoxShadows(layer)
		}
	}

	if layer.IsMulticol && layer.ColumnRuleStyle != "none" && layer.ColumnRuleWidth > 0 && layer.ColumnCount > 1 {
		r.drawColumnRules(layer)
	}

	if layer.OutlineStyle != "none" && layer.OutlineWidth > 0 {
		r.drawOutline(layer)
	}

	// List markers are real laid-out ::marker boxes in the fragment tree
	// (marker-foundation Phases 3-4): inside markers flow through the inline
	// pipeline, outside markers are placed by the UnpositionedListMarker
	// carry/claim protocol. They paint as ordinary box fragments — there is no
	// paint-time marker hack any more.
}

// paintSelfForeground paints a layer's own text and replaced-element
// image — the content that paints at CSS 2.1 Appendix E step 5
// (inline foreground), after step-4 floats. Inline <svg> is a
// replaced element whose content is the SVG subtree; it routes
// through paintSVGRoot in the same slot as <img>'s drawImage.
// Mirrors Blink's ReplacedPainter::PaintReplacedContent dispatch on
// LayoutSVGRoot.
func (r *Renderer) paintSelfForeground(layer *PaintLayer) {
	if layer.Box != nil && layer.Box.Text != "" {
		r.drawText(layer)
	}
	if isInlineSVGLayer(layer) {
		r.paintSVGRoot(layer)
		return
	}
	if layer.ImageSrc != "" {
		r.drawImage(layer)
	}
}

// paintDescendantsPhase walks the non-self-painting descendants of
// layer, painting only the work associated with phase. Self-painting
// descendants (positioned + z-index, opacity<1, transform, etc.) are
// not visited here — they are reached via the enclosing stacking
// context's z-lists instead.
//
// Non-positioned stacking contexts live in FlowChildren for DOM-order
// placement (buildPaintSubtree) but paint atomically: they are
// visited only during PhaseForeground, where paintLayer drives their
// own full phase loop.
func (r *Renderer) paintDescendantsPhase(layer *PaintLayer, phase PaintPhase) {
	for _, child := range layer.FlowChildren {
		r.paintDescendantPhase(child, phase)
	}
	if phase == PhaseFloat {
		for _, child := range layer.FloatChildren {
			// Each float runs its own full phase loop — a non-positioned
			// float still paints bg/border/content in Appendix E order
			// within the step-4 slot.
			r.paintLayer(child)
		}
	}
}

// paintDescendantPhase paints a single non-self-painting descendant
// (a direct or transitive FlowChild) at the given phase, then
// recurses into its own descendants for the same phase.
//
// Stacking-context descendants short-circuit to paintLayer during
// PhaseForeground; they are otherwise skipped (their own phase loop
// runs inside paintLayer).
//
// Atomic inlines (display: inline-block / inline-flex / inline-grid /
// inline-table) paint atomically: parent's PhaseForeground calls
// paintLayer which drives the atomic inline's own full phase loop.
// Their internal floats thus paint between their own bg and their
// own text, not at the outer SC's step 4.
//
// Pure inlines (display: inline) defer self decorations to
// PhaseForeground but recurse through their subtree on every phase so
// any nested atomic inlines, block-in-inline wrappers, or floats get
// their correct phase slot.
func (r *Renderer) paintDescendantPhase(child *PaintLayer, phase PaintPhase) {
	if child == nil {
		return
	}
	if !child.Visible || child.Opacity <= 0 {
		return
	}
	if child.Box != nil && child.Box.CreatesStackingContext() {
		if phase == PhaseForeground {
			r.paintLayer(child)
		}
		return
	}

	if isAtomicInlineForPaint(child) {
		if phase == PhaseForeground {
			r.paintLayer(child)
		}
		return
	}

	pureInline := isPureInlineForPaint(child)

	switch phase {
	case PhaseBackground:
		if !pureInline {
			r.paintSelfDecorations(child)
		}
		// Pure inline defers self decorations to PhaseForeground.
	case PhaseForeground:
		if pureInline {
			r.paintSelfDecorations(child)
		}
		r.paintSelfForeground(child)
	case PhaseFloat:
		// Skip self — floats paint their own bg during their paintLayer.
	}

	// Overflow clip on a non-SC descendant still applies to its own
	// descendants' phase work. Wrap the recursion in the same clip
	// setup used at the SC level, but scoped per phase so each phase
	// pass is independently clipped.
	clipping := false
	if child.HasClip {
		r.dc.Push()
		clipping = true
		ox, oy, ow, oh := pixelSnap(child.ClipRect[0], child.ClipRect[1],
			child.ClipRect[2], child.ClipRect[3])
		if hasBorderRadius(child) {
			r.buildRoundedRectPath(ox, oy, ow, oh, overflowClipRadii(child))
		} else {
			r.dc.DrawRectangle(ox, oy, ow, oh)
		}
		r.dc.Clip()
		r.pushClipRect(ox, oy, ow, oh)
	}

	r.paintDescendantsPhase(child, phase)

	// Collapsed table-cell border. See paintCollapsedTableCellBorder
	// for the spec ordering. Gated on PhaseBackground so we only fire
	// once per cell per layout pass (paintDescendantPhase is called
	// once per phase).
	if phase == PhaseBackground {
		r.paintCollapsedTableCellBorder(child)
	}

	if clipping {
		r.popClipRect()
		r.dc.Pop()
	}
}

// isAtomicInlineForPaint returns true for boxes that paint as a
// single atomic unit at CSS 2.1 Appendix E step 5. These include:
//   - Atomic inline-level boxes: inline-block, inline-flex,
//     inline-grid, inline-table (CSS 2.1 §10.3.7).
//   - Flex items (CSS Flexbox §13: "flex items paint exactly the
//     same as inline blocks").
//   - Grid items (CSS Grid §17 painting, same treatment).
//
// Atomic-paint boxes short-circuit the phase walk: the parent calls
// paintLayer during PhaseForeground, driving the box's own full
// phase loop. This ensures their bg, internal floats, and text all
// paint within their own z-range, not interleaved with siblings'.
func isAtomicInlineForPaint(layer *PaintLayer) bool {
	if layer == nil || layer.Box == nil || layer.Box.Style == nil {
		return false
	}
	switch layer.Box.Style.GetDisplay() {
	case css.DisplayInlineBlock,
		css.DisplayInlineFlex,
		css.DisplayInlineGrid,
		css.DisplayInlineTable:
		return true
	}
	if layer.Box.IsFlexItem() {
		return true
	}
	// Inline <svg> is a CSS replaced element (Blink's
	// LayoutSVGRoot : LayoutReplaced @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	// Replaced elements paint atomically at CSS 2.1 Appendix E step 5 —
	// background/border, then content — and the SVG root's UA viewport
	// clip needs to wrap the SVG content paint. Routing inline <svg>
	// through paintLayer ensures the standard CSS-clip path (which is
	// what `overflow-clip-margin` rides on) is established before
	// paintSVGRoot draws the SVG subtree.
	if isInlineSVGLayer(layer) {
		return true
	}
	return false
}

// isPureInlineForPaint returns true for display: inline — a
// non-atomic inline-level box whose bg / borders paint with its text
// at step 5, but whose subtree is still walked per phase by the
// containing stacking context (for any atomic-inline or block-level
// descendants).
func isPureInlineForPaint(layer *PaintLayer) bool {
	if layer == nil || layer.Box == nil || layer.Box.Style == nil {
		return false
	}
	return layer.Box.Style.GetDisplay() == css.DisplayInline
}

// pixelSnap rounds a box's position and size to integer pixel boundaries.
// Mirrors Blink's PhysicalRect::ToPixelSnappedRect (physical_rect.h:224):
// origin via ToRoundedPoint, size via SnapSizeToPixel(size, unsnapped_origin)
// — each axis snapped independently against its own unsnapped origin.
// SnapSizeToPixel applies the >4-raw thin-line clause, so a sub-quantum size
// at a sub-pixel origin no longer collapses to 0; the 1/64 LayoutUnit quantum
// also pins float64 IEEE-754 noise to the nearest raw step before rounding.
func pixelSnap(x, y, w, h float64) (float64, float64, float64, float64) {
	sx := math.Round(x)
	sy := math.Round(y)
	luX := layoutunit.FromFloat64Round(x)
	luY := layoutunit.FromFloat64Round(y)
	luW := layoutunit.FromFloat64Round(w)
	luH := layoutunit.FromFloat64Round(h)
	sw := float64(layoutunit.SnapSizeToPixel(luW, luX))
	sh := float64(layoutunit.SnapSizeToPixel(luH, luY))
	return sx, sy, sw, sh
}

// applyClipPath builds the clip-path shape and calls Clip().
// Caller must have already called Push(); will Pop() to restore.
//
// The reference box for a <basic-shape> clip-path is the border box
// (CSS Masking L1 default geometry-box). It is pixel-snapped the same
// way backgroundClipRectForClip snaps the border box for background
// painting, so a clip-path shape lands on the same pixel grid as the
// element's background and borders.
func (r *Renderer) applyClipPath(layer *PaintLayer) {
	box := layer.Box
	// Pixel-snapped border box — the clip-path reference box.
	bx, by, bw, bh := pixelSnap(box.X, box.Y, box.Width, box.Height)

	// Reference variant: resolve `url(#id)` against the renderer-wide
	// SVG resource registry and install the resolved clip directly.
	// Mirrors Blink's ClipPathClipper::ApplyReferenceClipPath dispatch
	// (core/paint/clip_path_clipper.cc).
	if layer.ClipPath.Type == css.ClipPathReference {
		target := geometry.NewRectF(bx, by, bw, bh)
		r.applySVGClipPath(target, layer.ClipPath.ReferenceID)
		return
	}

	cp := layer.ClipPath.ResolveClipPath(bw, bh)

	switch cp.Type {
	case css.ClipPathCircle:
		// A circle is an ellipse with rx == ry. Build it as a rounded
		// rectangle whose bounding box is the circle's bounding box and
		// whose four corner radii all equal the circle radius. This is
		// the exact same path construction used for a border-radius
		// circle (buildRoundedRectPathOnDC), so a clip-path: circle()
		// rasterizes pixel-identically to the border-radius circle these
		// tests are reftested against.
		cx, cy := bx+cp.Cx, by+cp.Cy
		rad := cp.Radius
		radii := css.EllipticalRadii{
			{Rx: rad, Ry: rad}, {Rx: rad, Ry: rad},
			{Rx: rad, Ry: rad}, {Rx: rad, Ry: rad},
		}
		buildRoundedRectPathOnDC(r.dc, cx-rad, cy-rad, 2*rad, 2*rad, radii)

	case css.ClipPathEllipse:
		// An ellipse is a rounded rectangle whose bounding box is the
		// ellipse's bounding box and whose four corner radii all equal
		// (rx, ry) — identical construction to a border-*-radius: rx ry
		// ellipse, so it rasterizes pixel-identically to the reference.
		cx, cy := bx+cp.Cx, by+cp.Cy
		rx, ry := cp.Rx, cp.Ry
		radii := css.EllipticalRadii{
			{Rx: rx, Ry: ry}, {Rx: rx, Ry: ry},
			{Rx: rx, Ry: ry}, {Rx: rx, Ry: ry},
		}
		buildRoundedRectPathOnDC(r.dc, cx-rx, cy-ry, 2*rx, 2*ry, radii)

	case css.ClipPathPolygon:
		pts := cp.Points
		if len(pts) < 4 {
			return // Need at least 2 points
		}
		for i := 0; i < len(pts)-1; i += 2 {
			px := bx + pts[i]
			py := by + pts[i+1]
			if i == 0 {
				r.dc.MoveTo(px, py)
			} else {
				r.dc.LineTo(px, py)
			}
		}
		r.dc.ClosePath()

	default:
		return // Unknown type, no clipping
	}

	r.dc.Clip()
}

// hasBorderRadius returns true if any corner radius is non-zero.
func hasBorderRadius(layer *PaintLayer) bool {
	return !layer.BorderRadius.IsZero()
}

// overflowClipRadii returns the elliptical radii to use when drawing the
// overflow clip path for `layer`. CSS Overflow 4 §3.2 specifies that the
// overflow clip edge "is shaped in the corners exactly the same way as an
// outer box-shadow with a spread radius of the same cumulative offset
// from the box's border edge", referring to the CSS Backgrounds 3 §5.4
// shadow shape formula.
//
// Blink's actual implementation (verified at Chromium @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f via
// `PhysicalBoxFragment::OverflowClipMarginOutsets` +
// `BoxPainterBase` rounded-clip construction) builds the radii in two
// steps:
//
//  1. **Inset** the border-box radii by the visual-box's per-side
//     distance from the border-edge (linear shrink — same shrink
//     used by `RoundedInnerRectForLineStyle` for inner border-radius).
//     For border-box the inset is zero on every side; for padding-box
//     the inset equals the border widths; for content-box it equals
//     border + padding.
//
//  2. **Outset** those visual-box radii outward by `ClipMarginLength`
//     using the CSS Backgrounds 3 §5.4 shadow-shape cubic formula
//     (`OutsetForBoxShadow`). The length is a single non-negative
//     scalar in the spec, applied equally on every side.
//
// Order matters: step 1 first reduces each radius to the visual-box's
// radius (which can be zero when the inset >= radius), and step 2 then
// applies the cubic correction `r + s·(1 + (r/s − 1)³)` for r < s.
// This is what produces, e.g., 27.5 for a TR corner of 15 on a
// `padding-box 20px` clip with a 5px border, instead of the naive
// border-edge sum 15 + 15 = 30 the previous implementation produced.
//
// When no clip-margin is active the border-box radii are returned
// unchanged.
func overflowClipRadii(layer *PaintLayer) css.EllipticalRadii {
	if !layer.HasClipMargin {
		return layer.BorderRadius
	}
	// Step 1: linear inset to the chosen visual-box's radii.
	radii := layer.BorderRadius.Inset(
		layer.ClipMarginVisualBoxInset[0],
		layer.ClipMarginVisualBoxInset[1],
		layer.ClipMarginVisualBoxInset[2],
		layer.ClipMarginVisualBoxInset[3],
	)
	// Step 2: cubic outset by the margin length on every side.
	if layer.ClipMarginLength == 0 {
		return radii
	}
	s := layer.ClipMarginLength
	return radii.OutsetForBoxShadow(s, s, s, s)
}

// buildRoundedRectPath traces a rounded rectangle path using CubicTo for
// elliptical corner arcs. Uses kappa constant for quarter-ellipse approximation.
func (r *Renderer) buildRoundedRectPath(x, y, w, h float64, radii css.EllipticalRadii) {
	buildRoundedRectPathOnDC(r.dc, x, y, w, h, radii)
}

// buildRoundedRectPathReverse traces a rounded rectangle path in reverse (CCW)
// for use with even-odd fill rule to cut out inner regions.
func (r *Renderer) buildRoundedRectPathReverse(x, y, w, h float64, radii css.EllipticalRadii) {
	const k = 0.5522847498 // kappa: 4*(sqrt(2)-1)/3
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
	r.dc.MoveTo(x+tl.Rx, y)
	// top-left (reverse: CW to CCW)
	r.dc.CubicTo(x+tl.Rx-tl.Rx*k, y, x, y+tl.Ry-tl.Ry*k, x, y+tl.Ry)
	r.dc.LineTo(x, y+h-bl.Ry)
	// bottom-left (reverse)
	r.dc.CubicTo(x, y+h-bl.Ry+bl.Ry*k, x+bl.Rx-bl.Rx*k, y+h, x+bl.Rx, y+h)
	r.dc.LineTo(x+w-br.Rx, y+h)
	// bottom-right (reverse)
	r.dc.CubicTo(x+w-br.Rx+br.Rx*k, y+h, x+w, y+h-br.Ry+br.Ry*k, x+w, y+h-br.Ry)
	r.dc.LineTo(x+w, y+tr.Ry)
	// top-right (reverse)
	r.dc.CubicTo(x+w, y+tr.Ry-tr.Ry*k, x+w-tr.Rx+tr.Rx*k, y, x+w-tr.Rx, y)
	r.dc.ClosePath()
}

// buildRoundedRectPathOnDC traces an elliptical rounded rectangle path on any DrawContext.
// This is the single source of truth for rounded-rect path construction.
func buildRoundedRectPathOnDC(dc interface {
	MoveTo(x, y float64)
	LineTo(x, y float64)
	CubicTo(x1, y1, x2, y2, x3, y3 float64)
	ClosePath()
}, x, y, w, h float64, radii css.EllipticalRadii) {
	const k = 0.5522847498 // kappa: 4*(sqrt(2)-1)/3
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]

	dc.MoveTo(x+tl.Rx, y)
	// Top edge → top-right corner
	dc.LineTo(x+w-tr.Rx, y)
	dc.CubicTo(x+w-tr.Rx+tr.Rx*k, y, x+w, y+tr.Ry-tr.Ry*k, x+w, y+tr.Ry)
	// Right edge → bottom-right corner
	dc.LineTo(x+w, y+h-br.Ry)
	dc.CubicTo(x+w, y+h-br.Ry+br.Ry*k, x+w-br.Rx+br.Rx*k, y+h, x+w-br.Rx, y+h)
	// Bottom edge → bottom-left corner
	dc.LineTo(x+bl.Rx, y+h)
	dc.CubicTo(x+bl.Rx-bl.Rx*k, y+h, x, y+h-bl.Ry+bl.Ry*k, x, y+h-bl.Ry)
	// Left edge → top-left corner
	dc.LineTo(x, y+tl.Ry)
	dc.CubicTo(x, y+tl.Ry-tl.Ry*k, x+tl.Rx-tl.Rx*k, y, x+tl.Rx, y)
	dc.ClosePath()
}

// buildRoundedRectPathReverseDC traces a rounded rectangle path in reverse (CCW)
// on any DrawContext. Used with even-odd fill rule to cut out inner regions.
func buildRoundedRectPathReverseDC(dc interface {
	MoveTo(x, y float64)
	LineTo(x, y float64)
	CubicTo(x1, y1, x2, y2, x3, y3 float64)
	ClosePath()
}, x, y, w, h float64, radii css.EllipticalRadii) {
	const k = 0.5522847498 // kappa: 4*(sqrt(2)-1)/3
	tl, tr, br, bl := radii[0], radii[1], radii[2], radii[3]
	dc.MoveTo(x+tl.Rx, y)
	// top-left (reverse: CW to CCW)
	dc.CubicTo(x+tl.Rx-tl.Rx*k, y, x, y+tl.Ry-tl.Ry*k, x, y+tl.Ry)
	dc.LineTo(x, y+h-bl.Ry)
	// bottom-left (reverse)
	dc.CubicTo(x, y+h-bl.Ry+bl.Ry*k, x+bl.Rx-bl.Rx*k, y+h, x+bl.Rx, y+h)
	dc.LineTo(x+w-br.Rx, y+h)
	// bottom-right (reverse)
	dc.CubicTo(x+w-br.Rx+br.Rx*k, y+h, x+w, y+h-br.Ry+br.Ry*k, x+w, y+h-br.Ry)
	dc.LineTo(x+w, y+tr.Ry)
	// top-right (reverse)
	dc.CubicTo(x+w, y+tr.Ry-tr.Ry*k, x+w-tr.Rx+tr.Rx*k, y, x+w-tr.Rx, y)
	dc.ClosePath()
}

// backgroundClipRectForClip computes the background painting area based on
// a background-clip value (border-box, padding-box, content-box).
func backgroundClipRectForClip(box *layout.Box, clip css.BackgroundClipType) (float64, float64, float64, float64) {
	switch clip {
	case css.BackgroundClipPaddingBox:
		return pixelSnap(
			box.X+box.Border.Left, box.Y+box.Border.Top,
			box.Width-box.Border.Left-box.Border.Right,
			box.Height-box.Border.Top-box.Border.Bottom)
	case css.BackgroundClipContentBox:
		return pixelSnap(
			box.X+box.Border.Left+box.Padding.Left,
			box.Y+box.Border.Top+box.Padding.Top,
			box.Width-box.Border.Left-box.Border.Right-box.Padding.Left-box.Padding.Right,
			box.Height-box.Border.Top-box.Border.Bottom-box.Padding.Top-box.Padding.Bottom)
	default: // border-box
		return pixelSnap(box.X, box.Y, box.Width, box.Height)
	}
}

// backgroundClipRect returns the clip rect for background-color.
// Per CSS spec, background-color is clipped by the bottom-most layer's clip.
func (r *Renderer) backgroundClipRect(layer *PaintLayer) (float64, float64, float64, float64) {
	clip := layer.BackgroundClip
	if fl := layer.BackgroundLayers; fl != nil {
		// Find the bottom layer's clip for background-color.
		for cur := fl; cur != nil; cur = cur.Next {
			if cur.Next == nil {
				clip = cur.Clip
			}
		}
	}
	return backgroundClipRectForClip(layer.Box, clip)
}

// backgroundClipRadiiForClip adjusts border radii for a background-clip inset.
// Uses Blink's Inset operation: shrink Rx by horizontal inset, Ry by vertical.
func backgroundClipRadiiForClip(layer *PaintLayer, clip css.BackgroundClipType) css.EllipticalRadii {
	radii := layer.BorderRadius
	box := layer.Box
	switch clip {
	case css.BackgroundClipPaddingBox:
		return radii.Inset(box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left)
	case css.BackgroundClipContentBox:
		return radii.Inset(
			box.Border.Top+box.Padding.Top,
			box.Border.Right+box.Padding.Right,
			box.Border.Bottom+box.Padding.Bottom,
			box.Border.Left+box.Padding.Left)
	}
	return radii
}

// backgroundClipRadii returns radii for background-color's clip area.
func backgroundClipRadii(layer *PaintLayer) css.EllipticalRadii {
	clip := layer.BackgroundClip
	if fl := layer.BackgroundLayers; fl != nil {
		for cur := fl; cur != nil; cur = cur.Next {
			if cur.Next == nil {
				clip = cur.Clip
			}
		}
	}
	return backgroundClipRadiiForClip(layer, clip)
}

// drawBackground paints the layer's background color and image layers (pre-computed).
// Layers are painted bottom-to-top (Blink's IterateFillLayersInReverseOrder).
// Background-color is painted only with the bottommost layer.
//
// Canvas-bg short circuit: when paintLayerCanvasBackground has already
// painted this layer's bg outside the layer's transform (per CSS Transforms
// 1 §6), skip the in-transform repaint so the bg isn't rotated/scaled with
// the body/root element.
func (r *Renderer) drawBackground(layer *PaintLayer) {
	if layer.PaintsCanvasBackground {
		if r.canvasBgPaintedLayer == layer {
			// paintLayerCanvasBackground already painted this layer's bg
			// outside the transform. The in-transform repaint would
			// rotate/scale the canvas bg with the body/root element —
			// skip it.
			return
		}
		if _, ok := r.canvasBgPaintedDescendants[layer]; ok {
			// Ancestor's paintLayerCanvasBackground already painted this
			// descendant canvas-bg layer out-of-transform; skip the in-
			// transform repaint reached via paintDescendantPhase. Remove
			// the entry so the set stays bounded.
			delete(r.canvasBgPaintedDescendants, layer)
			return
		}
	}
	sx, sy, sw, sh := r.backgroundClipRect(layer)
	radii := backgroundClipRadii(layer)
	hasRadius := !radii.IsZero()

	fl := layer.BackgroundLayers

	if fl == nil {
		// No layers — just paint background-color.
		if c := layer.BackgroundColor; c.A > 0 {
			r.setColor(c)
			if hasRadius {
				r.buildRoundedRectPath(sx, sy, sw, sh, radii)
				r.dc.Fill()
			} else {
				r.dc.DrawRectangle(sx, sy, sw, sh)
				r.dc.Fill()
			}
		}
		return
	}

	// Paint from bottom to top.
	fl.IterateReverse(func(bg *css.FillLayer) {
		// Background-color only on bottom layer (uses bottom layer's clip, already in sx/sy/sw/sh).
		if bg.IsBottomLayer() {
			if c := layer.BackgroundColor; c.A > 0 {
				r.setColor(c)
				if hasRadius {
					r.buildRoundedRectPath(sx, sy, sw, sh, radii)
					r.dc.Fill()
				} else {
					r.dc.DrawRectangle(sx, sy, sw, sh)
					r.dc.Fill()
				}
			}
		}

		// Per-layer clip rect and radii for gradient/image content.
		lx, ly, lw, lh := backgroundClipRectForClip(layer.Box, bg.Clip)
		lRadii := backgroundClipRadiiForClip(layer, bg.Clip)
		lHasRadius := !lRadii.IsZero()

		// Paint gradient.
		if bg.Gradient != "" {
			// CSS Backgrounds 3 §3.5: gradients with per-axis repeat space
			// or round (or repeat with an explicit background-size) must
			// be tiled at the resolved tile size with per-axis placement.
			// The plain "repeat repeat" + auto background-size case keeps
			// the legacy fill-the-box behavior (which is also spec-correct
			// for a gradient that has no intrinsic size — it stretches to
			// the positioning area).
			//
			// drawTiledGradient handles the case where the per-axis
			// computation degenerates to a single tile (the N == 1
			// fallback for space, no-repeat, or round with no scaling): it
			// delegates back to the regular drawLinearGradient with the
			// layer-clip rect so the result matches the legacy no-repeat
			// path bit-for-bit.
			hasExplicitSize := bg.Size.Width != 0 || bg.Size.Height != 0 || bg.Size.Cover || bg.Size.Contain
			usesAxisTile := bg.Repeat.X == css.BackgroundRepeatSpace ||
				bg.Repeat.X == css.BackgroundRepeatRound ||
				bg.Repeat.Y == css.BackgroundRepeatSpace ||
				bg.Repeat.Y == css.BackgroundRepeatRound ||
				(hasExplicitSize && (bg.Repeat.X == css.BackgroundRepeatRepeat || bg.Repeat.Y == css.BackgroundRepeatRepeat))
			if usesAxisTile {
				if lHasRadius {
					r.dc.Push()
					r.buildRoundedRectPath(lx, ly, lw, lh, lRadii)
					r.dc.Clip()
					r.drawTiledGradient(layer, bg)
					r.dc.Pop()
				} else {
					r.drawTiledGradient(layer, bg)
				}
			} else {
				// For fixed attachment, draw the gradient over the viewport
				// but clip to the element's background-clip area. Per CSS
				// Transforms 1 §6, a non-canvas layer with any ancestor
				// transform paint context degrades fixed to scroll.
				gradX, gradY, gradW, gradH := lx, ly, lw, lh
				gradFixed := bg.Attachment == css.BackgroundAttachmentFixed &&
					(layer.PaintsCanvasBackground || r.transformDepth == 0)
				if gradFixed {
					bounds := r.target.Bounds()
					gradX = float64(bounds.Min.X)
					gradY = float64(bounds.Min.Y)
					gradW = float64(bounds.Dx())
					gradH = float64(bounds.Dy())
					r.dc.Push()
					if lHasRadius {
						r.buildRoundedRectPath(lx, ly, lw, lh, lRadii)
					} else {
						r.dc.DrawRectangle(lx, ly, lw, lh)
					}
					r.dc.Clip()
					r.drawLinearGradient(bg.Gradient, gradX, gradY, gradW, gradH)
					r.dc.Pop()
				} else if lHasRadius {
					r.dc.Push()
					r.buildRoundedRectPath(lx, ly, lw, lh, lRadii)
					r.dc.Clip()
					r.drawLinearGradient(bg.Gradient, gradX, gradY, gradW, gradH)
					r.dc.Pop()
				} else {
					r.drawLinearGradient(bg.Gradient, gradX, gradY, gradW, gradH)
				}
			}
		}

		// Paint image.
		if bg.Image != nil && r.imageFetcher != nil {
			if lHasRadius {
				r.dc.Push()
				r.buildRoundedRectPath(lx, ly, lw, lh, lRadii)
				r.dc.Clip()
				r.drawBackgroundImageLayer(layer, bg)
				r.dc.Pop()
			} else {
				r.drawBackgroundImageLayer(layer, bg)
			}
		}
	})
}

// drawBackgroundImageLayer tiles a single background layer's image onto the layer's box.
// Reads image URL, origin, size, position, repeat from the FillLayer.
// Tiles are manually clipped to the box bounds because DrawImage bypasses
// the DrawContext clip mask (fast-path pixel blit ignores clipMask).
// All arithmetic is integer-based to avoid fractional pixel misalignment.
func (r *Renderer) drawBackgroundImageLayer(layer *PaintLayer, bg *css.FillLayer) {
	box := layer.Box
	img, err := images.LoadImageWithFetcher(bg.Image.Data.Absolute, r.imageFetcher)
	if err != nil {
		return
	}
	imgWI := img.Bounds().Dx()
	imgHI := img.Bounds().Dy()
	if imgWI == 0 || imgHI == 0 {
		return
	}
	imgW := float64(imgWI)
	imgH := float64(imgHI)

	// CSS3 Backgrounds §3.6: background-origin determines the positioning area.
	// For background-attachment:fixed, the positioning area is the viewport
	// (CSS3 Backgrounds §3.5), not the element's box.
	//
	// CSS 2.1 §14.2 / CSS Backgrounds Level 3 §2.11.2:
	// When PaintsCanvasBackground is set, the painting area extends to the
	// canvas, but the positioning area depends on the element:
	// - Root element: positioning area = root's own box (per background-origin)
	// - Body (promoted to canvas): positioning area = root element's padding box
	//   (§2.11.2: "the background positioning area of the root is the
	//   background positioning area that would be used if the background
	//   were specified on the body element" — but since it's promoted to
	//   the root, the positioning area is the root's, not body's)
	// - Any element with attachment:fixed → positioning area = viewport
	//
	// CSS Transforms 1 §6 fallback: for a non-canvas layer, when the layer
	// itself or any of its ancestors has a transform paint context, the
	// `fixed` value is treated as `scroll` (positioning area = element's
	// own box). The transformDepth counter includes the current layer's
	// transform (paintLayer increments BEFORE calling paintLayerContent).
	isFixed := bg.Attachment == css.BackgroundAttachmentFixed
	if isFixed && !layer.PaintsCanvasBackground && r.transformDepth > 0 {
		isFixed = false
	}
	// For promoted body backgrounds, use the root element's box for positioning.
	posBox := box
	if layer.CanvasBackgroundRootBox != nil {
		posBox = layer.CanvasBackgroundRootBox
	}
	var originX, originY, originW, originH float64
	if isFixed {
		bounds := r.target.Bounds()
		originX = float64(bounds.Min.X)
		originY = float64(bounds.Min.Y)
		originW = float64(bounds.Dx())
		originH = float64(bounds.Dy())
	} else {
		switch bg.Origin {
		case css.BackgroundOriginBorderBox:
			originX, originY, originW, originH = pixelSnap(
				posBox.X, posBox.Y, posBox.Width, posBox.Height)
		case css.BackgroundOriginContentBox:
			originX, originY, originW, originH = pixelSnap(
				posBox.X+posBox.Border.Left+posBox.Padding.Left,
				posBox.Y+posBox.Border.Top+posBox.Padding.Top,
				posBox.Width-posBox.Border.Left-posBox.Border.Right-posBox.Padding.Left-posBox.Padding.Right,
				posBox.Height-posBox.Border.Top-posBox.Border.Bottom-posBox.Padding.Top-posBox.Padding.Bottom)
		default: // padding-box
			originX, originY, originW, originH = pixelSnap(
				posBox.X+posBox.Border.Left,
				posBox.Y+posBox.Border.Top,
				posBox.Width-posBox.Border.Left-posBox.Border.Right,
				posBox.Height-posBox.Border.Top-posBox.Border.Bottom)
		}
	}
	if originW < 0 {
		originW = 0
	}
	if originH < 0 {
		originH = 0
	}

	// CSS3 Backgrounds §3.9: Resolve background-size.
	// Track whether each axis was auto-sized — round must rescale auto axes
	// proportionally per §3.5 ("If background-size is auto on the other
	// dimension, that other dimension is also scaled proportionally").
	bgSize := bg.Size
	autoW := !(bgSize.Cover || bgSize.Contain || bgSize.Width != 0)
	autoH := !(bgSize.Cover || bgSize.Contain || bgSize.Height != 0)
	if bgSize.Cover || bgSize.Contain {
		if originW > 0 && originH > 0 && imgW > 0 && imgH > 0 {
			scaleX := originW / imgW
			scaleY := originH / imgH
			var scale float64
			if bgSize.Cover {
				scale = math.Max(scaleX, scaleY)
			} else {
				scale = math.Min(scaleX, scaleY)
			}
			imgW = math.Round(imgW * scale)
			imgH = math.Round(imgH * scale)
		}
	} else if bgSize.Width != 0 || bgSize.Height != 0 {
		newW := imgW
		newH := imgH
		if bgSize.Width != 0 {
			if bgSize.Width < 0 {
				newW = math.Round(originW * (-bgSize.Width) / 100)
			} else {
				newW = math.Round(bgSize.Width)
			}
		}
		if bgSize.Height != 0 {
			if bgSize.Height < 0 {
				newH = math.Round(originH * (-bgSize.Height) / 100)
			} else {
				newH = math.Round(bgSize.Height)
			}
		}
		if bgSize.Width != 0 && bgSize.Height == 0 && imgW > 0 {
			newH = math.Round(imgH * newW / imgW)
		} else if bgSize.Width == 0 && bgSize.Height != 0 && imgH > 0 {
			newW = math.Round(imgW * newH / imgH)
		}
		imgW = newW
		imgH = newH
	}

	// CSS Backgrounds 3 §3.5 round: rescale the tile so an integer number
	// of tiles fits in the positioning area. When the other axis is auto,
	// scale it proportionally by the same factor. Mirrors Blink's handling
	// in BackgroundImageGeometry::UseFixedAttachment for the kRoundFill case
	// (third_party/blink/renderer/core/paint/background_image_geometry.cc
	// @ 4883d11fef).
	if bg.Repeat.X == css.BackgroundRepeatRound && imgW > 0 && originW > 0 {
		n := math.Round(originW / imgW)
		if n < 1 {
			n = 1
		}
		newW := originW / n
		if autoH && imgW > 0 {
			imgH = imgH * newW / imgW
		}
		imgW = newW
	}
	if bg.Repeat.Y == css.BackgroundRepeatRound && imgH > 0 && originH > 0 {
		n := math.Round(originH / imgH)
		if n < 1 {
			n = 1
		}
		newH := originH / n
		if autoW && imgH > 0 {
			imgW = imgW * newH / imgH
		}
		imgH = newH
	}

	// Snap final tile dimensions to integer pixels and rescale the source
	// image once we know the final tile size.
	imgWI = int(math.Round(imgW))
	imgHI = int(math.Round(imgH))
	if imgWI <= 0 || imgHI <= 0 {
		return
	}
	if imgWI != img.Bounds().Dx() || imgHI != img.Bounds().Dy() {
		img = scaleImage(img, img.Bounds().Dx(), img.Bounds().Dy(), imgWI, imgHI, layer.ImageRendering)
	}
	imgW = float64(imgWI)
	imgH = float64(imgHI)

	pos := bg.Position
	startX := originX + pos.ResolveX(originW, imgW)
	startY := originY + pos.ResolveY(originH, imgH)

	// Clip bounds: normally the border box, but for the root element's
	// background CSS 2.1 §14.2 says the background paints the entire canvas.
	var boxX0, boxY0, boxX1, boxY1 int
	if layer.PaintsCanvasBackground {
		bounds := r.target.Bounds()
		boxX0 = bounds.Min.X
		boxY0 = bounds.Min.Y
		boxX1 = bounds.Max.X
		boxY1 = bounds.Max.Y
	} else {
		boxX0 = int(math.Round(box.X))
		boxY0 = int(math.Round(box.Y))
		boxX1 = int(math.Round(box.X + box.Width))
		boxY1 = int(math.Round(box.Y + box.Height))
	}

	// Compute per-axis tile positions. Each entry is the top-left of one
	// tile; tiles always have size imgWI × imgHI after the resolution above.
	// The clip extent (box*) is used only by the regular `repeat` case to
	// extend tiles beyond the positioning area; space/round place tiles
	// strictly within the positioning area.
	xs := tilePositions(bg.Repeat.X, originX, originW, startX, imgW, float64(boxX0), float64(boxX1))
	ys := tilePositions(bg.Repeat.Y, originY, originH, startY, imgH, float64(boxY0), float64(boxY1))

	subImager, _ := img.(interface {
		SubImage(image.Rectangle) image.Image
	})

	for _, ty := range ys {
		for _, tx := range xs {
			dstX0 := tx
			if dstX0 < boxX0 {
				dstX0 = boxX0
			}
			dstX1 := tx + imgWI
			if dstX1 > boxX1 {
				dstX1 = boxX1
			}
			dstY0 := ty
			if dstY0 < boxY0 {
				dstY0 = boxY0
			}
			dstY1 := ty + imgHI
			if dstY1 > boxY1 {
				dstY1 = boxY1
			}
			if dstX1 <= dstX0 || dstY1 <= dstY0 {
				continue
			}

			srcX0 := dstX0 - tx
			srcY0 := dstY0 - ty
			srcX1 := dstX1 - tx
			srcY1 := dstY1 - ty

			if subImager != nil {
				sub := subImager.SubImage(image.Rect(srcX0, srcY0, srcX1, srcY1))
				r.dc.DrawImage(sub, dstX0, dstY0)
			} else {
				r.dc.DrawImage(img, dstX0, dstY0)
			}
		}
	}
}

// tilePositions returns the integer top-left positions for tiles along one
// axis, given the per-axis repeat value, positioning area extent
// (originStart..originStart+originLen), the natural tile start (after
// background-position), tile size, and the clip extent
// (coverStart..coverEnd) — used by the regular `repeat` case to extend
// tiles beyond the positioning area to fill the clip. All positions are
// pixel-snapped integers; the caller clips each tile against the clip rect.
//
// Spec: CSS Backgrounds 3 §3.5. Mirrors Blink's per-axis logic in
// BackgroundImageGeometry::UseFixedAttachment for kRepeatFill / kSpaceFill /
// kRoundFill / kNoRepeatFill cases @ 4883d11fef.
func tilePositions(repeat css.BackgroundRepeatType, originStart, originLen, tileStart, tileSize, coverStart, coverEnd float64) []int {
	if tileSize <= 0 {
		return []int{int(math.Round(tileStart))}
	}
	switch repeat {
	case css.BackgroundRepeatNoRepeat:
		return []int{int(math.Round(tileStart))}

	case css.BackgroundRepeatSpace:
		// §3.5 space: place N = floor(area / tile) tiles, with the first
		// and last flush against the positioning area edges and equal gaps
		// between. When only one tile fits (N == 1) or none (tile larger
		// than area), fall back to no-repeat at the background-position.
		if tileSize > originLen {
			return []int{int(math.Round(tileStart))}
		}
		n := int(math.Floor(originLen / tileSize))
		if n <= 1 {
			return []int{int(math.Round(tileStart))}
		}
		// N tiles: first at originStart, last at originStart+originLen-tile,
		// remaining N-2 evenly spaced in between.
		positions := make([]int, n)
		gap := (originLen - float64(n)*tileSize) / float64(n-1)
		for i := 0; i < n; i++ {
			p := originStart + float64(i)*(tileSize+gap)
			positions[i] = int(math.Round(p))
		}
		return positions

	case css.BackgroundRepeatRound:
		// After the caller's tile-size adjustment, round behaves like
		// repeat: tiles cover the positioning area at the adjusted size,
		// with background-position determining the phase. Mirrors Blink's
		// kRoundFill handling once the size is settled.
		if tileSize > originLen {
			return []int{int(math.Round(originStart))}
		}
		size := int(math.Round(tileSize))
		if size <= 0 {
			return []int{int(math.Round(tileStart))}
		}
		startPx := int(math.Round(tileStart))
		near := int(math.Round(originStart))
		far := int(math.Round(originStart + originLen))
		for startPx > near {
			startPx -= size
		}
		var positions []int
		for p := startPx; p < far; p += size {
			positions = append(positions, p)
		}
		if len(positions) == 0 {
			positions = append(positions, startPx)
		}
		return positions

	default: // BackgroundRepeatRepeat (also: any unexpected value)
		// Walk back from tileStart to cover the clip's near edge, then
		// forward until the clip's far edge.
		size := int(math.Round(tileSize))
		if size <= 0 {
			return []int{int(math.Round(tileStart))}
		}
		startPx := int(math.Round(tileStart))
		near := int(math.Round(coverStart))
		far := int(math.Round(coverEnd))
		for startPx > near {
			startPx -= size
		}
		var positions []int
		for p := startPx; p < far; p += size {
			positions = append(positions, p)
		}
		if len(positions) == 0 {
			positions = append(positions, startPx)
		}
		return positions
	}
}

// drawTiledGradient paints a gradient layer that has per-axis repeat = space
// or round. The gradient is drawn once per tile position at the resolved
// tile size; tile positions are computed by tilePositions().
//
// Gradients have no intrinsic dimensions per CSS Images 3, so an `auto`
// background-size value resolves to the positioning area's dimension on
// that axis. The round adjustment then mirrors the image path's round
// behavior (rescale tile so an integer number fits; with auto on the
// other axis the auto dim scales proportionally to keep one tile a square
// when the gradient is asked to be square in the original auto cases).
func (r *Renderer) drawTiledGradient(layer *PaintLayer, bg *css.FillLayer) {
	box := layer.Box

	// Per CSS Transforms 1 §6, a non-canvas layer with any ancestor
	// transform paint context degrades `fixed` to `scroll`.
	isFixed := bg.Attachment == css.BackgroundAttachmentFixed
	if isFixed && !layer.PaintsCanvasBackground && r.transformDepth > 0 {
		isFixed = false
	}
	posBox := box
	if layer.CanvasBackgroundRootBox != nil {
		posBox = layer.CanvasBackgroundRootBox
	}
	var originX, originY, originW, originH float64
	if isFixed {
		bounds := r.target.Bounds()
		originX = float64(bounds.Min.X)
		originY = float64(bounds.Min.Y)
		originW = float64(bounds.Dx())
		originH = float64(bounds.Dy())
	} else {
		switch bg.Origin {
		case css.BackgroundOriginBorderBox:
			originX, originY, originW, originH = pixelSnap(
				posBox.X, posBox.Y, posBox.Width, posBox.Height)
		case css.BackgroundOriginContentBox:
			originX, originY, originW, originH = pixelSnap(
				posBox.X+posBox.Border.Left+posBox.Padding.Left,
				posBox.Y+posBox.Border.Top+posBox.Padding.Top,
				posBox.Width-posBox.Border.Left-posBox.Border.Right-posBox.Padding.Left-posBox.Padding.Right,
				posBox.Height-posBox.Border.Top-posBox.Border.Bottom-posBox.Padding.Top-posBox.Padding.Bottom)
		default: // padding-box
			originX, originY, originW, originH = pixelSnap(
				posBox.X+posBox.Border.Left,
				posBox.Y+posBox.Border.Top,
				posBox.Width-posBox.Border.Left-posBox.Border.Right,
				posBox.Height-posBox.Border.Top-posBox.Border.Bottom)
		}
	}
	if originW <= 0 || originH <= 0 {
		return
	}

	// Gradient tile size from background-size. Gradients have no intrinsic
	// size, so `auto` resolves to the positioning-area dimension.
	bgSize := bg.Size
	tileW := originW
	tileH := originH
	autoW := true
	autoH := true
	if bgSize.Cover || bgSize.Contain {
		// Cover/contain on a gradient just fills the area; no intrinsic
		// ratio constrains it.
		tileW = originW
		tileH = originH
		autoW = false
		autoH = false
	} else {
		if bgSize.Width != 0 {
			if bgSize.Width < 0 {
				tileW = originW * (-bgSize.Width) / 100
			} else {
				tileW = bgSize.Width
			}
			autoW = false
		}
		if bgSize.Height != 0 {
			if bgSize.Height < 0 {
				tileH = originH * (-bgSize.Height) / 100
			} else {
				tileH = bgSize.Height
			}
			autoH = false
		}
	}

	// Round adjustments: §3.5. When the other axis is auto, scale the auto
	// dim proportionally with the round factor on the explicit axis.
	if bg.Repeat.X == css.BackgroundRepeatRound && tileW > 0 {
		n := math.Round(originW / tileW)
		if n < 1 {
			n = 1
		}
		newW := originW / n
		if autoH && tileW > 0 {
			tileH = tileH * newW / tileW
		}
		tileW = newW
	}
	if bg.Repeat.Y == css.BackgroundRepeatRound && tileH > 0 {
		n := math.Round(originH / tileH)
		if n < 1 {
			n = 1
		}
		newH := originH / n
		if autoW && tileH > 0 {
			tileW = tileW * newH / tileH
		}
		tileH = newH
	}
	if tileW <= 0 || tileH <= 0 {
		return
	}

	pos := bg.Position
	startX := originX + pos.ResolveX(originW, tileW)
	startY := originY + pos.ResolveY(originH, tileH)

	// Clip extent — for gradients we don't extend beyond the positioning
	// area because space/round always stay within it (the only case using
	// drawTiledGradient).
	var boxX0, boxY0, boxX1, boxY1 float64
	if layer.PaintsCanvasBackground {
		bounds := r.target.Bounds()
		boxX0 = float64(bounds.Min.X)
		boxY0 = float64(bounds.Min.Y)
		boxX1 = float64(bounds.Max.X)
		boxY1 = float64(bounds.Max.Y)
	} else {
		boxX0 = box.X
		boxY0 = box.Y
		boxX1 = box.X + box.Width
		boxY1 = box.Y + box.Height
	}

	xs := tilePositions(bg.Repeat.X, originX, originW, startX, tileW, boxX0, boxX1)
	ys := tilePositions(bg.Repeat.Y, originY, originH, startY, tileH, boxY0, boxY1)

	// When both axes degenerate to a single tile (the N == 1 / no-room
	// fallback for space, or round with no rescale needed), delegate to
	// the regular full-box gradient path so the result matches the
	// no-repeat code bit-for-bit. drawLinearGradient receives the layer
	// clip rect rather than the resolved tile rect; that mirrors how
	// no-repeat-with-explicit-background-size already renders gradients
	// elsewhere in the renderer.
	if len(xs) == 1 && len(ys) == 1 &&
		(bg.Repeat.X == css.BackgroundRepeatSpace ||
			bg.Repeat.X == css.BackgroundRepeatNoRepeat) &&
		(bg.Repeat.Y == css.BackgroundRepeatSpace ||
			bg.Repeat.Y == css.BackgroundRepeatNoRepeat) {
		lx, ly, lw, lh := backgroundClipRectForClip(box, bg.Clip)
		r.drawLinearGradient(bg.Gradient, lx, ly, lw, lh)
		return
	}

	// Push a clip rect so tiles overflowing the layer-clip area don't
	// paint past the box edges. Image tiling already clips per-tile via
	// the dst* bounds; gradient tiling lacks that fast-path, so we add an
	// explicit clip-bounds push for the duration of the tile loop.
	lx, ly, lw, lh := backgroundClipRectForClip(box, bg.Clip)
	r.pushClipRect(lx, ly, lw, lh)
	for _, ty := range ys {
		for _, tx := range xs {
			r.drawLinearGradient(bg.Gradient, float64(tx), float64(ty), tileW, tileH)
		}
	}
	r.popClipRect()
}

// drawImage paints an <img> element with object-fit and object-position support.
func (r *Renderer) drawImage(layer *PaintLayer) {
	if layer.ImageSrc == "" || r.imageFetcher == nil {
		return
	}
	box := layer.Box
	img, err := images.LoadImageWithFetcher(layer.ImageSrc, r.imageFetcher)
	if err != nil {
		return
	}

	srcW := float64(img.Bounds().Dx())
	srcH := float64(img.Bounds().Dy())
	contentX := math.Round(box.X + box.Border.Left + box.Padding.Left)
	contentY := math.Round(box.Y + box.Border.Top + box.Padding.Top)
	cw := box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right
	ch := box.Height - box.Border.Top - box.Border.Bottom - box.Padding.Top - box.Padding.Bottom
	dstW := int(math.Round(cw))
	dstH := int(math.Round(ch))
	if dstW <= 0 || dstH <= 0 || srcW == 0 || srcH == 0 {
		return
	}

	// Compute actual draw dimensions based on object-fit.
	var drawW, drawH float64
	switch layer.ObjectFit {
	case css.ObjectFitContain:
		scale := math.Min(cw/srcW, ch/srcH)
		drawW, drawH = srcW*scale, srcH*scale
	case css.ObjectFitCover:
		scale := math.Max(cw/srcW, ch/srcH)
		drawW, drawH = srcW*scale, srcH*scale
	case css.ObjectFitNone:
		drawW, drawH = srcW, srcH
	case css.ObjectFitScaleDown:
		scale := math.Min(1, math.Min(cw/srcW, ch/srcH))
		drawW, drawH = srcW*scale, srcH*scale
	default: // ObjectFitFill
		drawW, drawH = cw, ch
	}

	// Scale image to draw dimensions.
	scaledW, scaledH := int(math.Round(drawW)), int(math.Round(drawH))
	if scaledW <= 0 || scaledH <= 0 {
		return
	}
	scaled := scaleImage(img, int(srcW), int(srcH), scaledW, scaledH, layer.ImageRendering)

	// Position within content box using object-position.
	dx := contentX + (cw-drawW)*layer.ObjectPosition[0]
	dy := contentY + (ch-drawH)*layer.ObjectPosition[1]

	// Determine if clipping to the content box is needed (image may extend beyond).
	needsContentClip := layer.ObjectFit == css.ObjectFitCover || layer.ObjectFit == css.ObjectFitNone ||
		(layer.ObjectFit == css.ObjectFitScaleDown && (srcW > cw || srcH > ch))

	// finalImg and finalX/finalY will hold the image to draw after all clipping.
	var finalImg image.Image
	finalX, finalY := int(math.Round(dx)), int(math.Round(dy))

	if needsContentClip {
		// Crop the scaled image to the content box.
		cropX := int(math.Round(contentX - dx))
		cropY := int(math.Round(contentY - dy))
		cropW := dstW
		cropH := dstH
		if cropX < 0 {
			cropX = 0
		}
		if cropY < 0 {
			cropY = 0
		}
		if cropX+cropW > scaledW {
			cropW = scaledW - cropX
		}
		if cropY+cropH > scaledH {
			cropH = scaledH - cropY
		}
		if cropW <= 0 || cropH <= 0 {
			return
		}
		finalImg = scaled.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(cropX, cropY, cropX+cropW, cropY+cropH))
		finalX = finalX + cropX
		finalY = finalY + cropY
	} else {
		finalImg = scaled
	}

	// CSS clip: rect() — DrawImage bypasses the clip mask, so we must
	// manually crop the image to the CSS clip region.
	if layer.HasCSSClip {
		// CSSClipRect is [x, y, w, h] in absolute coordinates.
		cx := int(math.Round(layer.CSSClipRect[0]))
		cy := int(math.Round(layer.CSSClipRect[1]))
		ccw := int(math.Round(layer.CSSClipRect[2]))
		cch := int(math.Round(layer.CSSClipRect[3]))

		imgBounds := finalImg.Bounds()
		imgW := imgBounds.Dx()
		imgH := imgBounds.Dy()

		// Intersect clip region with image draw area.
		ix0 := finalX
		if cx > ix0 {
			ix0 = cx
		}
		iy0 := finalY
		if cy > iy0 {
			iy0 = cy
		}
		ix1 := finalX + imgW
		if cx+ccw < ix1 {
			ix1 = cx + ccw
		}
		iy1 := finalY + imgH
		if cy+cch < iy1 {
			iy1 = cy + cch
		}
		if ix1 <= ix0 || iy1 <= iy0 {
			return // Entirely clipped
		}
		// Extract the visible sub-image.
		sub := finalImg.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(
			imgBounds.Min.X+(ix0-finalX),
			imgBounds.Min.Y+(iy0-finalY),
			imgBounds.Min.X+(ix1-finalX),
			imgBounds.Min.Y+(iy1-finalY),
		))
		r.dc.DrawImage(sub, ix0, iy0)
		return
	}

	r.dc.DrawImage(finalImg, finalX, finalY)
}

// scaleImageNearest scales src to dstW×dstH using nearest-neighbor.
func scaleImageNearest(src image.Image, srcW, srcH, dstW, dstH int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := 0; dy < dstH; dy++ {
		sy := dy * srcH / dstH
		for dx := 0; dx < dstW; dx++ {
			sx := dx * srcW / dstW
			dst.Set(dx, dy, src.At(src.Bounds().Min.X+sx, src.Bounds().Min.Y+sy))
		}
	}
	return dst
}

// scaleImageBilinear scales src to dstW×dstH using bilinear interpolation.
// This produces smoother results than nearest-neighbor for photographic images.
func scaleImageBilinear(src image.Image, srcW, srcH, dstW, dstH int) image.Image {
	if dstW <= 0 || dstH <= 0 || srcW <= 0 || srcH <= 0 {
		return image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	b := src.Bounds()

	for dy := 0; dy < dstH; dy++ {
		// Map destination pixel center to source coordinate.
		sy := (float64(dy)+0.5)*float64(srcH)/float64(dstH) - 0.5
		sy0 := int(math.Floor(sy))
		sy1 := sy0 + 1
		fy := sy - float64(sy0)
		if sy0 < 0 {
			sy0 = 0
		}
		if sy1 >= srcH {
			sy1 = srcH - 1
		}

		for dx := 0; dx < dstW; dx++ {
			sx := (float64(dx)+0.5)*float64(srcW)/float64(dstW) - 0.5
			sx0 := int(math.Floor(sx))
			sx1 := sx0 + 1
			fx := sx - float64(sx0)
			if sx0 < 0 {
				sx0 = 0
			}
			if sx1 >= srcW {
				sx1 = srcW - 1
			}

			// Sample four source pixels.
			r00, g00, b00, a00 := src.At(b.Min.X+sx0, b.Min.Y+sy0).RGBA()
			r10, g10, b10, a10 := src.At(b.Min.X+sx1, b.Min.Y+sy0).RGBA()
			r01, g01, b01, a01 := src.At(b.Min.X+sx0, b.Min.Y+sy1).RGBA()
			r11, g11, b11, a11 := src.At(b.Min.X+sx1, b.Min.Y+sy1).RGBA()

			// Bilinear interpolation.
			lerp := func(v00, v10, v01, v11 uint32) uint8 {
				top := float64(v00)*(1-fx) + float64(v10)*fx
				bot := float64(v01)*(1-fx) + float64(v11)*fx
				return uint8((top*(1-fy) + bot*fy) / 256)
			}

			dst.SetRGBA(dx, dy, color.RGBA{
				R: lerp(r00, r10, r01, r11),
				G: lerp(g00, g10, g01, g11),
				B: lerp(b00, b10, b01, b11),
				A: lerp(a00, a10, a01, a11),
			})
		}
	}
	return dst
}

// isNearestNeighborRendering returns true if the image-rendering value
// requests nearest-neighbor (pixelated) interpolation.
func isNearestNeighborRendering(imageRendering string) bool {
	switch imageRendering {
	case "pixelated", "crisp-edges", "-webkit-optimize-contrast":
		return true
	}
	return false
}

// scaleImage scales src to dstW×dstH using interpolation determined by imageRendering.
// "pixelated", "crisp-edges", "-webkit-optimize-contrast" use nearest-neighbor;
// everything else (including "auto" and "") uses bilinear.
func scaleImage(src image.Image, srcW, srcH, dstW, dstH int, imageRendering string) image.Image {
	if isNearestNeighborRendering(imageRendering) {
		return scaleImageNearest(src, srcW, srcH, dstW, dstH)
	}
	return scaleImageBilinear(src, srcW, srcH, dstW, dstH)
}

// drawBorderImage implements CSS border-image 9-slice rendering.
// Returns true if the border-image was painted successfully, false if the
// image could not be loaded (caller should fall back to regular borders).
//
// The source image is sliced into 9 regions and each is drawn into the
// corresponding area of the border box:
//
//	+-------+-------------------+-------+
//	| TL    |    Top edge       |  TR   |
//	+-------+-------------------+-------+
//	|       |                   |       |
//	| Left  |   Center (fill)   | Right |
//	|       |                   |       |
//	+-------+-------------------+-------+
//	| BL    |   Bottom edge     |  BR   |
//	+-------+-------------------+-------+
//
// Mirrors Blink's NinePieceImagePainter.
func (r *Renderer) drawBorderImage(layer *PaintLayer) bool {
	if r.imageFetcher == nil {
		return false
	}

	// PaintLayer.BorderImageSource is *CSSImageValue post-LOU-138 phase 7.3;
	// the url() inner was extracted by cascade-time wrapping in paint_layer.go.
	src := layer.BorderImageSource.Data.Absolute

	img, err := images.LoadImageWithFetcher(src, r.imageFetcher)
	if err != nil {
		return false
	}
	imgW := img.Bounds().Dx()
	imgH := img.Bounds().Dy()
	if imgW == 0 || imgH == 0 {
		return false
	}

	box := layer.Box
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	// Border-image-outset extends the painted area outside the border box
	// per CSS Backgrounds 3 §6.2. Layout box is unchanged; only the area
	// the 9-slice image is drawn into grows.
	outset := layer.BorderImageOutset // [top, right, bottom, left]
	x -= outset[3]
	y -= outset[0]
	w += outset[1] + outset[3]
	h += outset[0] + outset[2]

	// Border-image-width determines the area occupied by border slices.
	biW := layer.BorderImageWidth // [top, right, bottom, left]

	// Compute slice offsets in source image pixels.
	slice := layer.BorderImageSlice
	var sliceTop, sliceRight, sliceBottom, sliceLeft int
	if slice.IsPercent {
		sliceTop = int(math.Round(slice.Top * float64(imgH) / 100))
		sliceRight = int(math.Round(slice.Right * float64(imgW) / 100))
		sliceBottom = int(math.Round(slice.Bottom * float64(imgH) / 100))
		sliceLeft = int(math.Round(slice.Left * float64(imgW) / 100))
	} else {
		sliceTop = int(math.Round(slice.Top))
		sliceRight = int(math.Round(slice.Right))
		sliceBottom = int(math.Round(slice.Bottom))
		sliceLeft = int(math.Round(slice.Left))
	}

	// Clamp slices to image dimensions.
	if sliceTop > imgH {
		sliceTop = imgH
	}
	if sliceBottom > imgH {
		sliceBottom = imgH
	}
	if sliceLeft > imgW {
		sliceLeft = imgW
	}
	if sliceRight > imgW {
		sliceRight = imgW
	}
	if sliceTop+sliceBottom > imgH {
		sliceTop = imgH / 2
		sliceBottom = imgH - sliceTop
	}
	if sliceLeft+sliceRight > imgW {
		sliceLeft = imgW / 2
		sliceRight = imgW - sliceLeft
	}

	// Destination areas (border-image-width in px).
	dstTop := biW[0]
	dstRight := biW[1]
	dstBottom := biW[2]
	dstLeft := biW[3]

	// Clamp destination widths to box dimensions.
	if dstTop+dstBottom > h {
		ratio := h / (dstTop + dstBottom)
		dstTop *= ratio
		dstBottom *= ratio
	}
	if dstLeft+dstRight > w {
		ratio := w / (dstLeft + dstRight)
		dstLeft *= ratio
		dstRight *= ratio
	}

	imgBounds := img.Bounds()
	minX := imgBounds.Min.X
	minY := imgBounds.Min.Y

	// Helper: extract a sub-rectangle from the source image.
	subImage := func(sx, sy, sw, sh int) image.Image {
		if sw <= 0 || sh <= 0 {
			return nil
		}
		return img.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(minX+sx, minY+sy, minX+sx+sw, minY+sy+sh))
	}

	// Helper: draw a source region scaled to a destination rectangle.
	drawScaled := func(srcImg image.Image, dx, dy, dw, dh float64) {
		if srcImg == nil || dw <= 0 || dh <= 0 {
			return
		}
		sw := srcImg.Bounds().Dx()
		sh := srcImg.Bounds().Dy()
		if sw == 0 || sh == 0 {
			return
		}
		idw := int(math.Round(dw))
		idh := int(math.Round(dh))
		if idw <= 0 || idh <= 0 {
			return
		}
		scaled := scaleImageNearest(srcImg, sw, sh, idw, idh)
		r.dc.DrawImage(scaled, int(math.Round(dx)), int(math.Round(dy)))
	}

	// Helper: draw a source region repeated/rounded/spaced along an edge.
	drawEdge := func(srcImg image.Image, dx, dy, dw, dh float64, repeatMode string, horizontal bool) {
		if srcImg == nil || dw <= 0 || dh <= 0 {
			return
		}
		sw := srcImg.Bounds().Dx()
		sh := srcImg.Bounds().Dy()
		if sw == 0 || sh == 0 {
			return
		}

		switch repeatMode {
		case "stretch":
			drawScaled(srcImg, dx, dy, dw, dh)

		case "repeat":
			if horizontal {
				// Scale source height to match destination height, keep aspect ratio for width.
				tileH := int(math.Round(dh))
				tileW := int(math.Round(float64(sw) * dh / float64(sh)))
				if tileW <= 0 || tileH <= 0 {
					return
				}
				tile := scaleImageNearest(srcImg, sw, sh, tileW, tileH)
				edgeX0 := int(math.Round(dx))
				edgeX1 := int(math.Round(dx + dw))
				edgeY := int(math.Round(dy))
				// Center the tiling pattern.
				totalW := float64(edgeX1 - edgeX0)
				nTiles := math.Ceil(totalW / float64(tileW))
				tiledW := nTiles * float64(tileW)
				offset := int(math.Round((totalW - tiledW) / 2))
				for tx := edgeX0 + offset; tx < edgeX1; tx += tileW {
					// Clip to edge bounds.
					srcX0 := 0
					dstX0 := tx
					if dstX0 < edgeX0 {
						srcX0 = edgeX0 - dstX0
						dstX0 = edgeX0
					}
					dstX1 := tx + tileW
					if dstX1 > edgeX1 {
						dstX1 = edgeX1
					}
					srcX1 := srcX0 + (dstX1 - dstX0)
					if srcX1 > tileW {
						srcX1 = tileW
					}
					if dstX1 <= dstX0 || srcX1 <= srcX0 {
						continue
					}
					sub := tile.(interface {
						SubImage(image.Rectangle) image.Image
					}).SubImage(image.Rect(srcX0, 0, srcX1, tileH))
					r.dc.DrawImage(sub, dstX0, edgeY)
				}
			} else {
				// Vertical edge: scale source width to match destination width.
				tileW := int(math.Round(dw))
				tileH := int(math.Round(float64(sh) * dw / float64(sw)))
				if tileW <= 0 || tileH <= 0 {
					return
				}
				tile := scaleImageNearest(srcImg, sw, sh, tileW, tileH)
				edgeY0 := int(math.Round(dy))
				edgeY1 := int(math.Round(dy + dh))
				edgeX := int(math.Round(dx))
				totalH := float64(edgeY1 - edgeY0)
				nTiles := math.Ceil(totalH / float64(tileH))
				tiledH := nTiles * float64(tileH)
				offset := int(math.Round((totalH - tiledH) / 2))
				for ty := edgeY0 + offset; ty < edgeY1; ty += tileH {
					srcY0 := 0
					dstY0 := ty
					if dstY0 < edgeY0 {
						srcY0 = edgeY0 - dstY0
						dstY0 = edgeY0
					}
					dstY1 := ty + tileH
					if dstY1 > edgeY1 {
						dstY1 = edgeY1
					}
					srcY1 := srcY0 + (dstY1 - dstY0)
					if srcY1 > tileH {
						srcY1 = tileH
					}
					if dstY1 <= dstY0 || srcY1 <= srcY0 {
						continue
					}
					sub := tile.(interface {
						SubImage(image.Rectangle) image.Image
					}).SubImage(image.Rect(0, srcY0, tileW, srcY1))
					r.dc.DrawImage(sub, edgeX, dstY0)
				}
			}

		case "round":
			if horizontal {
				tileH := int(math.Round(dh))
				naturalTileW := float64(sw) * dh / float64(sh)
				if naturalTileW <= 0 || tileH <= 0 {
					return
				}
				n := math.Max(1, math.Round(dw/naturalTileW))
				tileW := int(math.Round(dw / n))
				if tileW <= 0 {
					return
				}
				tile := scaleImageNearest(srcImg, sw, sh, tileW, tileH)
				edgeX0 := int(math.Round(dx))
				edgeX1 := int(math.Round(dx + dw))
				edgeY := int(math.Round(dy))
				for tx := edgeX0; tx < edgeX1; tx += tileW {
					tw := tileW
					if tx+tw > edgeX1 {
						tw = edgeX1 - tx
					}
					if tw <= 0 {
						break
					}
					sub := tile.(interface {
						SubImage(image.Rectangle) image.Image
					}).SubImage(image.Rect(0, 0, tw, tileH))
					r.dc.DrawImage(sub, tx, edgeY)
				}
			} else {
				tileW := int(math.Round(dw))
				naturalTileH := float64(sh) * dw / float64(sw)
				if naturalTileH <= 0 || tileW <= 0 {
					return
				}
				n := math.Max(1, math.Round(dh/naturalTileH))
				tileH := int(math.Round(dh / n))
				if tileH <= 0 {
					return
				}
				tile := scaleImageNearest(srcImg, sw, sh, tileW, tileH)
				edgeY0 := int(math.Round(dy))
				edgeY1 := int(math.Round(dy + dh))
				edgeX := int(math.Round(dx))
				for ty := edgeY0; ty < edgeY1; ty += tileH {
					th := tileH
					if ty+th > edgeY1 {
						th = edgeY1 - ty
					}
					if th <= 0 {
						break
					}
					sub := tile.(interface {
						SubImage(image.Rectangle) image.Image
					}).SubImage(image.Rect(0, 0, tileW, th))
					r.dc.DrawImage(sub, edgeX, ty)
				}
			}

		case "space":
			if horizontal {
				tileH := int(math.Round(dh))
				tileW := int(math.Round(float64(sw) * dh / float64(sh)))
				if tileW <= 0 || tileH <= 0 {
					return
				}
				tile := scaleImageNearest(srcImg, sw, sh, tileW, tileH)
				edgeX0 := int(math.Round(dx))
				edgeY := int(math.Round(dy))
				n := int(dw) / tileW
				if n <= 0 {
					return
				}
				if n == 1 {
					// Center single tile.
					cx := edgeX0 + (int(math.Round(dw))-tileW)/2
					r.dc.DrawImage(tile, cx, edgeY)
				} else {
					spacing := (dw - float64(n*tileW)) / float64(n-1)
					for i := 0; i < n; i++ {
						tx := edgeX0 + int(math.Round(float64(i)*(float64(tileW)+spacing)))
						r.dc.DrawImage(tile, tx, edgeY)
					}
				}
			} else {
				tileW := int(math.Round(dw))
				tileH := int(math.Round(float64(sh) * dw / float64(sw)))
				if tileW <= 0 || tileH <= 0 {
					return
				}
				tile := scaleImageNearest(srcImg, sw, sh, tileW, tileH)
				edgeY0 := int(math.Round(dy))
				edgeX := int(math.Round(dx))
				n := int(dh) / tileH
				if n <= 0 {
					return
				}
				if n == 1 {
					cy := edgeY0 + (int(math.Round(dh))-tileH)/2
					r.dc.DrawImage(tile, edgeX, cy)
				} else {
					spacing := (dh - float64(n*tileH)) / float64(n-1)
					for i := 0; i < n; i++ {
						ty := edgeY0 + int(math.Round(float64(i)*(float64(tileH)+spacing)))
						r.dc.DrawImage(tile, edgeX, ty)
					}
				}
			}

		default:
			drawScaled(srcImg, dx, dy, dw, dh)
		}
	}

	// Source image regions (9-slice coordinates).
	middleW := imgW - sliceLeft - sliceRight
	middleH := imgH - sliceTop - sliceBottom

	// Destination middle area.
	dstMiddleW := w - dstLeft - dstRight
	dstMiddleH := h - dstTop - dstBottom

	hRepeat := layer.BorderImageRepeat[0]
	vRepeat := layer.BorderImageRepeat[1]

	// 4 corners — always scaled (stretched) to fit.
	if sliceTop > 0 && sliceLeft > 0 && dstTop > 0 && dstLeft > 0 {
		drawScaled(subImage(0, 0, sliceLeft, sliceTop), x, y, dstLeft, dstTop)
	}
	if sliceTop > 0 && sliceRight > 0 && dstTop > 0 && dstRight > 0 {
		drawScaled(subImage(imgW-sliceRight, 0, sliceRight, sliceTop), x+w-dstRight, y, dstRight, dstTop)
	}
	if sliceBottom > 0 && sliceLeft > 0 && dstBottom > 0 && dstLeft > 0 {
		drawScaled(subImage(0, imgH-sliceBottom, sliceLeft, sliceBottom), x, y+h-dstBottom, dstLeft, dstBottom)
	}
	if sliceBottom > 0 && sliceRight > 0 && dstBottom > 0 && dstRight > 0 {
		drawScaled(subImage(imgW-sliceRight, imgH-sliceBottom, sliceRight, sliceBottom), x+w-dstRight, y+h-dstBottom, dstRight, dstBottom)
	}

	// 4 edges — repeat mode controlled by border-image-repeat.
	if middleW > 0 && sliceTop > 0 && dstTop > 0 && dstMiddleW > 0 {
		drawEdge(subImage(sliceLeft, 0, middleW, sliceTop), x+dstLeft, y, dstMiddleW, dstTop, hRepeat, true)
	}
	if middleW > 0 && sliceBottom > 0 && dstBottom > 0 && dstMiddleW > 0 {
		drawEdge(subImage(sliceLeft, imgH-sliceBottom, middleW, sliceBottom), x+dstLeft, y+h-dstBottom, dstMiddleW, dstBottom, hRepeat, true)
	}
	if middleH > 0 && sliceLeft > 0 && dstLeft > 0 && dstMiddleH > 0 {
		drawEdge(subImage(0, sliceTop, sliceLeft, middleH), x, y+dstTop, dstLeft, dstMiddleH, vRepeat, false)
	}
	if middleH > 0 && sliceRight > 0 && dstRight > 0 && dstMiddleH > 0 {
		drawEdge(subImage(imgW-sliceRight, sliceTop, sliceRight, middleH), x+w-dstRight, y+dstTop, dstRight, dstMiddleH, vRepeat, false)
	}

	// Center (fill): only drawn when the fill keyword is present.
	if slice.Fill && middleW > 0 && middleH > 0 && dstMiddleW > 0 && dstMiddleH > 0 {
		drawScaled(subImage(sliceLeft, sliceTop, middleW, middleH), x+dstLeft, y+dstTop, dstMiddleW, dstMiddleH)
	}

	return true
}

// paintCollapsedTableCellBorder paints a collapsed table cell's border
// in its CSS Tables 3 paint phase: inside the background phase, after
// descendant backgrounds (so a child's CSS background can't overpaint
// the shared border) but before the foreground phase (so an inline-block
// descendant's foreground — e.g. a `background: green` painted as part
// of its own atomic-inline phase loop — paints above the collapsed
// border). Mirrors Blink's TablePainter::PaintCollapsedBorders ordering
// inside PaintPhase::kBlockBackground at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. WPT
// collapsed-border-paint-001 asserts the latter; -paint-phase-002
// asserts the former.
func (r *Renderer) paintCollapsedTableCellBorder(layer *PaintLayer) {
	if !layer.IsCollapsedBorderCell || layer.EmptyCellHide {
		return
	}
	r.drawBorders(layer)
}

// drawBorders draws all four borders of the layer's box (pre-computed styles/colors).
func (r *Renderer) drawBorders(layer *PaintLayer) {
	// Border-image replaces regular border drawing when a source is set.
	if layer.BorderImageSource != nil {
		if r.drawBorderImage(layer) {
			return // border-image painted successfully
		}
		// Fall through to regular borders if image failed to load.
	}

	box := layer.Box
	bw := box.Border
	if bw.Top == 0 && bw.Right == 0 && bw.Bottom == 0 && bw.Left == 0 {
		return
	}

	// Rounded borders: delegate to specialized method.
	if hasBorderRadius(layer) {
		r.drawRoundedBorders(layer)
		return
	}

	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)
	outerLeft, outerTop := x, y
	outerRight, outerBottom := x+w, y+h

	// Border thickness is snapped via SnapSizeToPixel against the unsnapped
	// origin of each side: left/top use the unsnapped box origin; right/bottom
	// use (unsnapped outer right/bottom - bw), which is where the right/bottom
	// border thickness begins. The thin-line clause inside SnapSizeToPixel
	// preserves sub-pixel borders (border-width: 0.1px–0.5px) that ad-hoc
	// math.Round of the snapped outer + bw would collapse to 0. Mirrors
	// Blink's PhysicalRect::PixelSnappedWidth/Height pattern from
	// core/layout/geometry/physical_rect.h.
	luBoxX := layoutunit.FromFloat64Round(box.X)
	luBoxY := layoutunit.FromFloat64Round(box.Y)
	luBoxXW := layoutunit.FromFloat64Round(box.X + box.Width)
	luBoxYH := layoutunit.FromFloat64Round(box.Y + box.Height)
	luBwL := layoutunit.FromFloat64Round(bw.Left)
	luBwT := layoutunit.FromFloat64Round(bw.Top)
	luBwR := layoutunit.FromFloat64Round(bw.Right)
	luBwB := layoutunit.FromFloat64Round(bw.Bottom)
	innerLeft := outerLeft + float64(layoutunit.SnapSizeToPixel(luBwL, luBoxX))
	innerTop := outerTop + float64(layoutunit.SnapSizeToPixel(luBwT, luBoxY))
	innerRight := outerRight - float64(layoutunit.SnapSizeToPixel(luBwR, luBoxXW.Sub(luBwR)))
	innerBottom := outerBottom - float64(layoutunit.SnapSizeToPixel(luBwB, luBoxYH.Sub(luBwB)))

	// Top border (index 0).
	if bw.Top > 0 && layer.BorderStyles[0] != css.BorderStyleNone {
		if c := layer.BorderColors[0]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[0] {
			case css.BorderStyleDashed:
				midY := (outerTop + innerTop) / 2
				r.drawDashedLine(outerLeft, midY, outerRight, midY, bw.Top)
			case css.BorderStyleDotted:
				midY := (outerTop + innerTop) / 2
				r.drawDottedLine(outerLeft, midY, outerRight, midY, bw.Top)
			case css.BorderStyleDouble:
				midY := (outerTop + innerTop) / 2
				r.drawDoubleLine(outerLeft, midY, outerRight, midY, bw.Top)
			default: // Solid, Hidden, Groove, Ridge, Inset, Outset — trapezoid
				r.dc.MoveTo(outerLeft, outerTop)
				r.dc.LineTo(outerRight, outerTop)
				r.dc.LineTo(innerRight, innerTop)
				r.dc.LineTo(innerLeft, innerTop)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
	// Right border (index 1).
	if bw.Right > 0 && layer.BorderStyles[1] != css.BorderStyleNone {
		if c := layer.BorderColors[1]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[1] {
			case css.BorderStyleDashed:
				midX := (outerRight + innerRight) / 2
				r.drawDashedLine(midX, outerTop, midX, outerBottom, bw.Right)
			case css.BorderStyleDotted:
				midX := (outerRight + innerRight) / 2
				r.drawDottedLine(midX, outerTop, midX, outerBottom, bw.Right)
			case css.BorderStyleDouble:
				midX := (outerRight + innerRight) / 2
				r.drawDoubleLine(midX, outerTop, midX, outerBottom, bw.Right)
			default:
				r.dc.MoveTo(outerRight, outerTop)
				r.dc.LineTo(outerRight, outerBottom)
				r.dc.LineTo(innerRight, innerBottom)
				r.dc.LineTo(innerRight, innerTop)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
	// Bottom border (index 2).
	if bw.Bottom > 0 && layer.BorderStyles[2] != css.BorderStyleNone {
		if c := layer.BorderColors[2]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[2] {
			case css.BorderStyleDashed:
				midY := (outerBottom + innerBottom) / 2
				r.drawDashedLine(outerLeft, midY, outerRight, midY, bw.Bottom)
			case css.BorderStyleDotted:
				midY := (outerBottom + innerBottom) / 2
				r.drawDottedLine(outerLeft, midY, outerRight, midY, bw.Bottom)
			case css.BorderStyleDouble:
				midY := (outerBottom + innerBottom) / 2
				r.drawDoubleLine(outerLeft, midY, outerRight, midY, bw.Bottom)
			default:
				r.dc.MoveTo(outerLeft, outerBottom)
				r.dc.LineTo(innerLeft, innerBottom)
				r.dc.LineTo(innerRight, innerBottom)
				r.dc.LineTo(outerRight, outerBottom)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
	// Left border (index 3).
	if bw.Left > 0 && layer.BorderStyles[3] != css.BorderStyleNone {
		if c := layer.BorderColors[3]; c.A > 0 {
			r.setColor(c)
			switch layer.BorderStyles[3] {
			case css.BorderStyleDashed:
				midX := (outerLeft + innerLeft) / 2
				r.drawDashedLine(midX, outerTop, midX, outerBottom, bw.Left)
			case css.BorderStyleDotted:
				midX := (outerLeft + innerLeft) / 2
				r.drawDottedLine(midX, outerTop, midX, outerBottom, bw.Left)
			case css.BorderStyleDouble:
				midX := (outerLeft + innerLeft) / 2
				r.drawDoubleLine(midX, outerTop, midX, outerBottom, bw.Left)
			default:
				r.dc.MoveTo(outerLeft, outerTop)
				r.dc.LineTo(innerLeft, innerTop)
				r.dc.LineTo(innerLeft, innerBottom)
				r.dc.LineTo(outerLeft, outerBottom)
				r.dc.ClosePath()
				r.dc.Fill()
			}
		}
	}
}

// drawRoundedBorders draws borders for a box with border-radius.
func (r *Renderer) drawRoundedBorders(layer *PaintLayer) {
	box := layer.Box
	bw := box.Border
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	// Check if all sides have the same width, style, and color (uniform case).
	uniform := bw.Top == bw.Right && bw.Right == bw.Bottom && bw.Bottom == bw.Left &&
		layer.BorderStyles[0] == layer.BorderStyles[1] &&
		layer.BorderStyles[1] == layer.BorderStyles[2] &&
		layer.BorderStyles[2] == layer.BorderStyles[3] &&
		layer.BorderColors[0] == layer.BorderColors[1] &&
		layer.BorderColors[1] == layer.BorderColors[2] &&
		layer.BorderColors[2] == layer.BorderColors[3]

	if uniform && bw.Top > 0 && layer.BorderStyles[0] != css.BorderStyleNone {
		// Simple case: draw a single stroked rounded rect at the midline.
		// However, this only works when the midline radii faithfully represent
		// the outer shape. When border-width/2 exceeds any outer radius, the
		// midline radius goes to 0 (sharp corner) while the outer edge should
		// still be rounded. Fall through to the ring approach in that case.
		hw := bw.Top / 2
		midRadii := layer.BorderRadius.Inset(hw, hw, hw, hw)
		midOk := true
		for i := 0; i < 4; i++ {
			if !layer.BorderRadius[i].IsZero() && midRadii[i].IsZero() {
				midOk = false
				break
			}
		}
		if midOk {
			r.setColor(layer.BorderColors[0])
			r.dc.SetLineWidth(bw.Top)
			r.buildRoundedRectPath(x+hw, y+hw, w-bw.Top, h-bw.Top, midRadii)
			r.dc.Stroke()
			return
		}
	}

	// Non-uniform borders with radius: draw each side using even-odd fill
	// between outer and inner rounded rects, clipped to each side's region.
	outerRadii := layer.BorderRadius

	// Inner rounded rect (border-box inset by border widths) — Blink's Inset.
	ix := x + bw.Left
	iy := y + bw.Top
	iw := w - bw.Left - bw.Right
	ih := h - bw.Top - bw.Bottom
	innerRadii := outerRadii.Inset(bw.Top, bw.Right, bw.Bottom, bw.Left)

	type borderSide struct {
		width                      float64
		style                      css.BorderStyle
		color                      css.Color
		clipX, clipY, clipW, clipH float64
	}

	// For clip region calculations, use the maximum of Rx and Ry per corner.
	or := [4]float64{
		math.Max(outerRadii[0].Rx, outerRadii[0].Ry),
		math.Max(outerRadii[1].Rx, outerRadii[1].Ry),
		math.Max(outerRadii[2].Rx, outerRadii[2].Ry),
		math.Max(outerRadii[3].Rx, outerRadii[3].Ry),
	}

	// Compute clip regions for each side.
	sides := [4]borderSide{
		{ // Top
			width: bw.Top, style: layer.BorderStyles[0], color: layer.BorderColors[0],
			clipX: x, clipY: y,
			clipW: w,
			clipH: math.Max(bw.Top, math.Max(or[0], or[1])),
		},
		{ // Right
			width: bw.Right, style: layer.BorderStyles[1], color: layer.BorderColors[1],
			clipX: x + w - math.Max(bw.Right, math.Max(or[1], or[2])),
			clipY: y,
			clipW: math.Max(bw.Right, math.Max(or[1], or[2])),
			clipH: h,
		},
		{ // Bottom
			width: bw.Bottom, style: layer.BorderStyles[2], color: layer.BorderColors[2],
			clipX: x,
			clipY: y + h - math.Max(bw.Bottom, math.Max(or[2], or[3])),
			clipW: w,
			clipH: math.Max(bw.Bottom, math.Max(or[2], or[3])),
		},
		{ // Left
			width: bw.Left, style: layer.BorderStyles[3], color: layer.BorderColors[3],
			clipX: x, clipY: y,
			clipW: math.Max(bw.Left, math.Max(or[0], or[3])),
			clipH: h,
		},
	}

	for _, side := range sides {
		if side.width <= 0 || side.style == css.BorderStyleNone || side.color.A <= 0 {
			continue
		}
		r.dc.Push()
		r.dc.DrawRectangle(side.clipX, side.clipY, side.clipW, side.clipH)
		r.dc.Clip()
		r.setColor(side.color)
		// Outer path (CW) + inner path (CCW) with even-odd fill.
		r.buildRoundedRectPath(x, y, w, h, outerRadii)
		if iw > 0 && ih > 0 {
			r.buildRoundedRectPathReverse(ix, iy, iw, ih, innerRadii)
		}
		r.dc.SetFillRule(textshape.FillRuleEvenOdd)
		r.dc.Fill()
		r.dc.SetFillRule(textshape.FillRuleWinding)
		r.dc.Pop()
	}
}

// columnExtentRange returns the absolute Y range covered by all
// IsColumnBox children of box. Used by drawColumnRules to bound rule
// block-extent to the column fragmentainers' actual placements rather
// than the multicol container's full content area. Mirrors Blink
// BoxFragmentPainter::PaintColumnRules's per-column rule_block_start /
// rule_block_end derivation (box_fragment_painter.cc ~1876).
func columnExtentRange(box *layout.Box) (top, bottom float64, ok bool) {
	for _, child := range box.Children {
		if child == nil || !child.IsColumnBox {
			continue
		}
		cy := child.Y
		cb := child.Y + child.Height
		if !ok {
			top = cy
			bottom = cb
			ok = true
			continue
		}
		if cy < top {
			top = cy
		}
		if cb > bottom {
			bottom = cb
		}
	}
	return
}

// drawColumnRules draws vertical rules between multicol columns.
// Rules are centered in the gap between adjacent columns.
// When layer.GapGeometry is set (Phase 12h.6), CrossGaps drive the positions
// and spanner-adjacent gaps are skipped. Otherwise falls back to the ad-hoc loop.
// Phase 20 P20.4: rule block-extent is derived from the placed column
// fragmentainers (Box.IsColumnBox children) when available, mirroring Blink.
func (r *Renderer) drawColumnRules(layer *PaintLayer) {
	box := layer.Box
	ruleWidth := layer.ColumnRuleWidth

	if ruleWidth <= 0 {
		return
	}

	r.setColor(layer.ColumnRuleColor)

	// Content area start (inside border and padding) — shared by both paths.
	contentX := math.Round(box.X + box.Border.Left + box.Padding.Left)
	contentY := math.Round(box.Y + box.Border.Top + box.Padding.Top)
	contentH := math.Round(box.Y+box.Height-box.Border.Bottom-box.Padding.Bottom) - contentY

	// Phase 20 P20.4: derive the rule's block extent from the actual column
	// fragmentainers rather than the multicol's full content area. Mirrors
	// Blink BoxFragmentPainter::PaintColumnRules (box_fragment_painter.cc
	// ~line 1876), which computes rule_block_start/end from adjacent column-
	// fragment offsets/sizes, not from the container box. With the multicol
	// container's broad ClipContentToBorderBox workaround (3389efe7) about to
	// be removed in P20.5, deriving rule extent from column boxes prevents
	// rules from drawing past the columns into the multicol's stretched-or-
	// padded area (flex-stretched parent, column-fill:auto with shorter
	// columns, etc.). Box.IsColumnBox is forwarded from
	// PhysicalFragment.BoxType == BoxTypeColumn (P20.1-P20.3).
	//
	// Note: this is the simple "union of column extents" form. Blink's
	// algorithm tracks per-row extents and stretches the last row to the
	// container's content_block_end (a known TODO in Blink to remove). For
	// louis14 the union matches the column box bounds — the typical case is
	// a single row of columns; multi-row cases (with spanners) already skip
	// spanner-adjacent gaps via gg.IsMultiColSpanner.
	colsTopY, colsBottomY, hasCols := columnExtentRange(box)
	if hasCols {
		contentY = math.Round(colsTopY)
		contentH = math.Round(colsBottomY) - contentY
		if contentH < 0 {
			contentH = 0
		}
	}

	// Blink stretches the last row's column rules to the container's
	// content-box block end (box_fragment_painter.cc ~line 1876).
	// Per-row detection mirrors Blink's !items_until_last_row gate:
	// only cross gaps at indices >= LastRowCrossGapStart belong to the
	// last row and get stretched. The content-box bottom accounts for
	// the scrollbar block-end reservation (matches Blink's subtraction
	// of scrollbars.block_end from ContentRect().BlockEndOffset()).
	// Mirrors Blink behavior noted as "a known TODO in Blink to remove".
	stretchedH := contentH
	if hasCols {
		cbBottom := math.Round(box.Y + box.Height - box.Border.Bottom - box.Padding.Bottom)
		if gg := layer.GapGeometry; gg != nil {
			cbBottom -= gg.ScrollbarBlockEnd
			if cbBottom > contentY+contentH {
				stretchedH = cbBottom - contentY
			}
		}
	}

	drawRule := func(ruleX float64, ruleH float64) {
		switch layer.ColumnRuleStyle {
		case "solid":
			r.dc.DrawRectangle(ruleX-ruleWidth/2, contentY, ruleWidth, ruleH)
			r.dc.Fill()
		case "dashed":
			r.drawDashedLine(ruleX, contentY, ruleX, contentY+ruleH, ruleWidth)
		case "dotted":
			r.drawDottedLine(ruleX, contentY, ruleX, contentY+ruleH, ruleWidth)
		case "double":
			thirdW := ruleWidth / 3
			r.dc.DrawRectangle(ruleX-ruleWidth/2, contentY, thirdW, ruleH)
			r.dc.Fill()
			r.dc.DrawRectangle(ruleX+ruleWidth/2-thirdW, contentY, thirdW, ruleH)
			r.dc.Fill()
		default:
			r.dc.DrawRectangle(ruleX-ruleWidth/2, contentY, ruleWidth, ruleH)
			r.dc.Fill()
		}
	}

	if gg := layer.GapGeometry; gg != nil {
		// GapGeometry path: use layout-computed cross gap positions.
		// CrossGap.GapInlineOffset is content-box relative (logical inline);
		// for HTB writing mode that equals physical X offset from content origin.
		for i, cg := range gg.CrossGaps {
			if gg.IsMultiColSpanner(i, layout.GapForColumns) {
				continue // spanner-adjacent gap: no rule
			}
			ruleX := contentX + math.Round(cg.GapInlineOffset)
			ruleH := contentH
			if i >= gg.LastRowCrossGapStart {
				ruleH = stretchedH
			}
			drawRule(ruleX, ruleH)
		}
		return
	}

	// Ad-hoc fallback: only reached when GapGeometry is nil (pre-12h.6
	// code paths). Draw numCols-1 rules evenly spaced.
	colWidth := layer.ColumnWidth
	gap := layer.ColumnGap
	numCols := layer.ColumnCount
	if numCols < 2 || colWidth <= 0 {
		return
	}
	for i := 1; i < numCols; i++ {
		ruleX := contentX + float64(i)*(colWidth+gap) - gap/2
		drawRule(ruleX, contentH)
	}
}

// drawOutline draws the CSS outline around the border-box, offset by outline-offset.
func (r *Renderer) drawOutline(layer *PaintLayer) {
	box := layer.Box
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	// Outline is drawn at: border-box + offset + width/2 (stroke centered on path).
	off := layer.OutlineOffset + layer.OutlineWidth/2
	ox := x - off
	oy := y - off
	ow := w + 2*off
	oh := h + 2*off

	if ow <= 0 || oh <= 0 {
		return
	}

	r.setColor(layer.OutlineColor)

	switch layer.OutlineStyle {
	case "solid":
		r.dc.SetLineWidth(layer.OutlineWidth)
		if hasBorderRadius(layer) {
			expandedRadii := layer.BorderRadius.Outset(off, off, off, off)
			r.buildRoundedRectPath(ox, oy, ow, oh, expandedRadii)
		} else {
			r.dc.DrawRectangle(ox, oy, ow, oh)
		}
		r.dc.Stroke()
	case "dashed":
		midOff := layer.OutlineOffset + layer.OutlineWidth/2
		mx, my := x-midOff, y-midOff
		mw, mh := w+2*midOff, h+2*midOff
		r.drawDashedLine(mx, my, mx+mw, my, layer.OutlineWidth)       // top
		r.drawDashedLine(mx+mw, my, mx+mw, my+mh, layer.OutlineWidth) // right
		r.drawDashedLine(mx, my+mh, mx+mw, my+mh, layer.OutlineWidth) // bottom
		r.drawDashedLine(mx, my, mx, my+mh, layer.OutlineWidth)       // left
	case "dotted":
		midOff := layer.OutlineOffset + layer.OutlineWidth/2
		mx, my := x-midOff, y-midOff
		mw, mh := w+2*midOff, h+2*midOff
		r.drawDottedLine(mx, my, mx+mw, my, layer.OutlineWidth)
		r.drawDottedLine(mx+mw, my, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDottedLine(mx, my+mh, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDottedLine(mx, my, mx, my+mh, layer.OutlineWidth)
	case "double":
		midOff := layer.OutlineOffset + layer.OutlineWidth/2
		mx, my := x-midOff, y-midOff
		mw, mh := w+2*midOff, h+2*midOff
		r.drawDoubleLine(mx, my, mx+mw, my, layer.OutlineWidth)
		r.drawDoubleLine(mx+mw, my, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDoubleLine(mx, my+mh, mx+mw, my+mh, layer.OutlineWidth)
		r.drawDoubleLine(mx, my, mx, my+mh, layer.OutlineWidth)
	default:
		// Treat unknown styles as solid.
		r.dc.SetLineWidth(layer.OutlineWidth)
		r.dc.DrawRectangle(ox, oy, ow, oh)
		r.dc.Stroke()
	}
}

// drawDashedLine draws a dashed line from (x1,y1) to (x2,y2) with the given width.
// Dash length and gap are both 3× the border width per CSS spec.
func (r *Renderer) drawDashedLine(x1, y1, x2, y2, width float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}

	dashLen := width * 3
	gapLen := width * 3
	ux, uy := dx/length, dy/length
	nx, ny := -uy, ux // perpendicular
	hw := width / 2

	along := 0.0
	for along < length {
		segEnd := along + dashLen
		if segEnd > length {
			segEnd = length
		}

		sx, sy := x1+ux*along, y1+uy*along
		ex, ey := x1+ux*segEnd, y1+uy*segEnd

		r.dc.MoveTo(sx+nx*hw, sy+ny*hw)
		r.dc.LineTo(ex+nx*hw, ey+ny*hw)
		r.dc.LineTo(ex-nx*hw, ey-ny*hw)
		r.dc.LineTo(sx-nx*hw, sy-ny*hw)
		r.dc.ClosePath()
		r.dc.Fill()

		along = segEnd + gapLen
	}
}

// drawDottedLine draws a dotted line from (x1,y1) to (x2,y2) with the given width.
// Dot diameter equals the border width; gap equals the border width.
func (r *Renderer) drawDottedLine(x1, y1, x2, y2, width float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}

	dotRadius := width / 2
	spacing := width * 2 // dot diameter + gap
	ux, uy := dx/length, dy/length

	along := dotRadius
	for along < length {
		cx := x1 + ux*along
		cy := y1 + uy*along
		r.dc.DrawCircle(cx, cy, dotRadius)
		r.dc.Fill()
		along += spacing
	}
}

// drawDoubleLine draws two parallel lines from (x1,y1) to (x2,y2).
// Each line is width/3 thick with a width/3 gap between them.
// Falls back to solid for borders thinner than 3px.
func (r *Renderer) drawDoubleLine(x1, y1, x2, y2, width float64) {
	if width < 3 {
		// Too thin for double — draw as solid line
		r.drawSolidLine(x1, y1, x2, y2, width)
		return
	}
	nx, ny := -(y2 - y1), (x2 - x1)
	length := math.Sqrt(nx*nx + ny*ny)
	if length == 0 {
		return
	}
	nx, ny = nx/length, ny/length

	lineW := width / 3
	offset := width/2 - lineW/2
	// Outer line
	r.drawSolidLine(x1+nx*offset, y1+ny*offset, x2+nx*offset, y2+ny*offset, lineW)
	// Inner line
	r.drawSolidLine(x1-nx*offset, y1-ny*offset, x2-nx*offset, y2-ny*offset, lineW)
}

// drawSolidLine draws a filled rectangle along a line from (x1,y1) to (x2,y2) with the given width.
func (r *Renderer) drawSolidLine(x1, y1, x2, y2, width float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}
	nx, ny := -dy/length, dx/length
	hw := width / 2

	r.dc.MoveTo(x1+nx*hw, y1+ny*hw)
	r.dc.LineTo(x2+nx*hw, y2+ny*hw)
	r.dc.LineTo(x2-nx*hw, y2-ny*hw)
	r.dc.LineTo(x1-nx*hw, y1-ny*hw)
	r.dc.ClosePath()
	r.dc.Fill()
}

// setColor sets the draw context color from a css.Color.
func (r *Renderer) setColor(c css.Color) {
	r.dc.SetColor(color.RGBA{
		R: c.R,
		G: c.G,
		B: c.B,
		A: uint8(c.A * 255),
	})
}

// applyTextTransform applies CSS text-transform to a string.
func applyTextTransform(s string, transform css.TextTransform) string {
	switch transform {
	case css.TextTransformUppercase:
		return strings.ToUpper(s)
	case css.TextTransformLowercase:
		return strings.ToLower(s)
	case css.TextTransformCapitalize:
		return capitalizeWords(s)
	}
	return s
}

// capitalizeWords capitalizes the first letter of each word (CSS capitalize).
func capitalizeWords(s string) string {
	inWord := false
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			inWord = false
			b.WriteRune(r)
		} else if !inWord {
			inWord = true
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// formatListMarker returns the marker text for a given list-style-type and index.
// Custom @counter-style rules are checked first when the list-style-type
// doesn't match a built-in type.
func (r *Renderer) formatListMarker(lst css.ListStyleType, index int) string {
	switch lst {
	case css.ListStyleTypeDecimal:
		return fmt.Sprintf("%d.", index)
	case css.ListStyleTypeDecimalLeadingZero:
		return fmt.Sprintf("%02d.", index)
	case css.ListStyleTypeLowerAlpha, css.ListStyleTypeLowerLatin:
		return css.ToAlpha(index) + "."
	case css.ListStyleTypeUpperAlpha, css.ListStyleTypeUpperLatin:
		return strings.ToUpper(css.ToAlpha(index)) + "."
	case css.ListStyleTypeLowerRoman:
		return strings.ToLower(css.ToRoman(index)) + "."
	case css.ListStyleTypeUpperRoman:
		return css.ToRoman(index) + "."
	case css.ListStyleTypeLowerGreek:
		return css.ToGreek(index) + "."
	case css.ListStyleTypeDisclosureOpen:
		return "\u25BE" // ▾ downward-pointing triangle
	case css.ListStyleTypeDisclosureClosed:
		return "\u25B8" // ▸ right-pointing triangle
	default:
		// Check custom @counter-style rules.
		if r != nil && r.counterStyles != nil {
			if cs, ok := r.counterStyles[string(lst)]; ok {
				return applyCounterStyle(index, cs, r.counterStyles)
			}
		}
		// Return custom string list-style-type values as-is.
		// These are CSS <string> values from e.g. list-style-type: "\2022".
		// Per CSS Lists §3, a <string> value is used as the marker text.
		return string(lst)
	}
}

// applyCounterStyle converts a counter value using a custom @counter-style rule.
func applyCounterStyle(value int, cs css.CounterStyleRule, allStyles map[string]css.CounterStyleRule) string {
	switch cs.System {
	case "cyclic":
		if len(cs.Symbols) == 0 {
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		n := len(cs.Symbols)
		idx := ((value-1)%n + n) % n
		return cs.Prefix + cs.Symbols[idx] + cs.Suffix

	case "numeric":
		if len(cs.Symbols) < 2 {
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		base := len(cs.Symbols)
		if value == 0 {
			return cs.Prefix + cs.Symbols[0] + cs.Suffix
		}
		negative := value < 0
		if negative {
			value = -value
		}
		result := ""
		v := value
		for v > 0 {
			result = cs.Symbols[v%base] + result
			v /= base
		}
		if negative {
			result = "-" + result
		}
		return cs.Prefix + result + cs.Suffix

	case "alphabetic":
		if len(cs.Symbols) < 2 {
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		if value <= 0 {
			// Alphabetic system not defined for zero or negative values.
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		base := len(cs.Symbols)
		result := ""
		v := value
		for v > 0 {
			v-- // 1-based: subtract 1 before modding
			result = cs.Symbols[v%base] + result
			v /= base
		}
		return cs.Prefix + result + cs.Suffix

	case "symbolic":
		if len(cs.Symbols) == 0 {
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		if value <= 0 {
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		n := len(cs.Symbols)
		idx := ((value-1)%n + n) % n
		count := ((value - 1) / n) + 1
		sym := cs.Symbols[idx]
		result := strings.Repeat(sym, count)
		return cs.Prefix + result + cs.Suffix

	case "fixed":
		if value >= 1 && value <= len(cs.Symbols) {
			return cs.Prefix + cs.Symbols[value-1] + cs.Suffix
		}
		// Out of range: use fallback
		return fallbackCounter(value, cs.Fallback, allStyles)

	case "additive":
		if len(cs.AdditiveSymbols) == 0 {
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		// Sort additive symbols by value descending
		sorted := make([]css.AdditiveSymbol, len(cs.AdditiveSymbols))
		copy(sorted, cs.AdditiveSymbols)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Value > sorted[j].Value
		})
		if value == 0 {
			// Special case: find symbol with value 0
			for _, as := range sorted {
				if as.Value == 0 {
					return cs.Prefix + as.Symbol + cs.Suffix
				}
			}
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		result := ""
		remaining := value
		for _, as := range sorted {
			if as.Value <= 0 {
				break
			}
			for remaining >= as.Value {
				result += as.Symbol
				remaining -= as.Value
			}
		}
		if remaining > 0 {
			// Could not represent — use fallback
			return fallbackCounter(value, cs.Fallback, allStyles)
		}
		return cs.Prefix + result + cs.Suffix

	default:
		// Unknown system — use fallback
		return fallbackCounter(value, cs.Fallback, allStyles)
	}
}

// fallbackCounter applies the fallback counter style, defaulting to decimal.
func fallbackCounter(value int, fallback string, allStyles map[string]css.CounterStyleRule) string {
	if fallback != "" && allStyles != nil {
		if cs, ok := allStyles[fallback]; ok {
			return applyCounterStyle(value, cs, allStyles)
		}
		// Check if fallback is a built-in style
		switch fallback {
		case "decimal":
			return fmt.Sprintf("%d.", value)
		case "lower-alpha", "lower-latin":
			return css.ToAlpha(value) + "."
		case "upper-alpha", "upper-latin":
			return strings.ToUpper(css.ToAlpha(value)) + "."
		case "lower-roman":
			return strings.ToLower(css.ToRoman(value)) + "."
		case "upper-roman":
			return css.ToRoman(value) + "."
		}
	}
	return fmt.Sprintf("%d.", value)
}

// List markers are real laid-out ::marker boxes (marker-foundation Phases
// 3-4): inside markers flow through the inline pipeline, outside markers are
// placed by the UnpositionedListMarker carry/claim protocol. Both paint as
// ordinary box fragments — the paint-time drawListMarker / drawListMarkerOutside
// hack has been retired. formatListMarker / applyCounterStyle / fallbackCounter
// below remain only as the @counter-style data path (docs/plan-css-lists.md
// B5 replaces them with the CounterStyle subsystem).

// drawTextStr draws a text string, applying OpenType features if present on the layer.
// The baseline Y is pixel-snapped; X is passed through fractionally so the
// rasterizer's subpixel pen carry (StartPenX in textshape.DrawText) produces
// consistent glyph positioning regardless of how a line's text is split across
// inline runs.
func (r *Renderer) drawTextStr(text string, fontID int32, x, y float64, features []textshape.FontFeature) {
	y = math.Round(y)
	if len(features) > 0 {
		r.dc.DrawTextWithFeatures(text, fontID, x, y, features)
	} else {
		r.dc.DrawText(text, fontID, x, y)
	}
}

// measureTextStr measures a text string, applying OpenType features if present.
func (r *Renderer) measureTextStr(text string, fontID int32, features []textshape.FontFeature) float64 {
	if len(features) > 0 {
		return r.dc.MeasureTextWithFeatures(text, fontID, features)
	}
	return r.dc.MeasureText(text, fontID)
}

// drawText paints text content using pre-computed font/color properties.
func (r *Renderer) drawText(layer *PaintLayer) {
	box := layer.Box

	// Apply CSS text-transform.
	text := box.Text
	if layer.TextTransform != css.TextTransformNone {
		text = applyTextTransform(text, layer.TextTransform)
	}

	fontPath := r.fonts.FontPathForFamilyWithSynthesis(
		layer.FontFamily, layer.FontBold, layer.FontItalic,
		layer.FontMono, layer.FontAhem,
		layer.FontSynthesisWeight, layer.FontSynthesisStyle)
	fontID := r.openFont(fontPath, layer.FontSize)
	if fontID < 0 {
		return
	}
	r.setColor(layer.TextColor)

	metrics := r.dc.GetFontMetrics(fontID)
	ascent := float64(metrics.Ascent) / 64.0

	// Sideways text: CSS Writing Modes §7.3.
	// sideways-rl / sideways-lr treat the entire line as horizontal text
	// rotated 90°.  The physical box already has Width=lineHeight,
	// Height=textAdvance.  Strategy: render the string horizontally into an
	// off-screen (textAdvance × lineHeight) buffer, then rotate the pixels
	// into a (lineHeight × textAdvance) destination and blit at box origin.
	if layer.IsSidewaysRL || layer.IsSidewaysLR {
		ta := int(math.Ceil(box.Height)) // text advance → off-screen width
		lh := int(math.Ceil(box.Width))  // line height  → off-screen height
		if ta <= 0 || lh <= 0 {
			return
		}

		// Draw horizontal text into an off-screen buffer.
		src := image.NewRGBA(image.Rect(0, 0, ta, lh))
		childDC := r.dc.NewChildContext(src)
		childDC.SetColor(color.RGBA{
			R: layer.TextColor.R,
			G: layer.TextColor.G,
			B: layer.TextColor.B,
			A: uint8(layer.TextColor.A * 255),
		})
		if len(layer.FontFeatures) > 0 {
			childDC.DrawTextWithFeatures(text, fontID, 0, ascent, layer.FontFeatures)
		} else {
			childDC.DrawText(text, fontID, 0, ascent)
		}

		// Rotate pixels 90° into destination (lh × ta) buffer.
		rot := image.NewRGBA(image.Rect(0, 0, lh, ta))
		for y := 0; y < lh; y++ {
			for x := 0; x < ta; x++ {
				c := src.RGBAAt(x, y)
				if c.A == 0 {
					continue
				}
				if layer.IsSidewaysRL {
					// 90° CW: (x,y) → (lh-1-y, x)
					rot.SetRGBA(lh-1-y, x, c)
				} else {
					// 90° CCW: (x,y) → (y, ta-1-x)
					rot.SetRGBA(y, ta-1-x, c)
				}
			}
		}

		r.dc.DrawImage(rot, int(math.Round(box.X)), int(math.Round(box.Y)))

		// Per-character emphasis marks: each mark renders as its own small
		// rotated off-screen buffer at the annotation-equivalent screen
		// position. Mirrors louis14's ruby annotation paint structure
		// (`pkg/layout/inline_layout_ruby.go:60-83`) so emphasis pixels snap
		// to the same per-fragment math.Round anchors as ruby annotation
		// pixels — the structural fix for the sub-pixel drift identified in
		// LOU-162's first attempt (see ticket-active/LOU-162/findings.md).
		if layer.TextEmphasisMark != "" {
			r.drawSidewaysEmphasisMarks(layer, text, box, fontID, ascent, ta, lh)
		}
		return
	}

	// Vertical text: draw each character stacked vertically.
	// CSS Writing Modes §5.1: in vertical writing modes, upright glyphs
	// advance in the block-progression (vertical) direction. For Ahem
	// (1em × 1em squares) and other upright text, each character cell
	// is fontSize tall and the glyph is centered horizontally.
	if layer.IsVerticalText {
		y := box.Y
		for _, ch := range text {
			charStr := string(ch)
			charW := r.measureTextStr(charStr, fontID, layer.FontFeatures)
			xOffset := (box.Width - charW) / 2
			if xOffset < 0 {
				xOffset = 0
			}
			r.drawTextStr(charStr, fontID, box.X+xOffset, y+ascent, layer.FontFeatures)
			y += layer.FontSize
			if layer.LetterSpacing != 0 {
				y += layer.LetterSpacing
			}
		}
		return
	}

	// Text overflow: ellipsis — truncate text if it overflows the
	// nearest ancestor block container that has overflow:hidden.
	// CSS text-overflow applies to the block container; we check both
	// the text run's own style and ancestor styles for the property.
	text = r.applyTextOverflowEllipsis(layer, text, box, fontID)

	// Draw text shadows (behind actual text).
	if len(layer.TextShadows) > 0 {
		r.drawTextShadows(layer, text, box, fontID, ascent)
	}

	// CSS font-variant-caps: small-caps / all-small-caps — synthesize small
	// capitals by drawing lowercase (or all) letters as uppercase glyphs at a
	// reduced font size (~70% of normal).  The baseline stays aligned.
	// CSS Fonts 4 §6.6: font-synthesis-small-caps: none forbids the UA from
	// synthesizing small-caps glyphs; if the active face doesn't have a real
	// small-caps variant, we must render the lowercase letters as-is.
	// Mirrors Blink's gate at font_description.cc::SynthesisAllowedSmallCaps
	// (SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	if (layer.FontVariantCaps == "small-caps" || layer.FontVariantCaps == "all-small-caps") && layer.FontSynthesisSmallCaps {
		r.drawTextSmallCaps(layer, text, box, fontPath, fontID, ascent)
	} else if strings.Contains(text, "\t") {
		// CSS Text 3 §4.2: tab characters advance to the next tab stop.
		r.drawTextWithTabs(layer, text, box, fontID, ascent)
	} else if layer.LetterSpacing != 0 || layer.WordSpacing != 0 {
		x := box.X
		baselineY := box.Y + ascent
		if layer.LetterSpacing != 0 {
			// Character-by-character rendering for letter-spacing.
			for _, ch := range text {
				r.drawTextStr(string(ch), fontID, x, baselineY, layer.FontFeatures)
				charW := r.measureTextStr(string(ch), fontID, layer.FontFeatures)
				x += charW + layer.LetterSpacing
				if ch == ' ' {
					x += layer.WordSpacing
				}
			}
		} else {
			// Word-by-word rendering for word-spacing only.
			// Drawing words as units avoids sub-pixel accumulation errors
			// that occur with per-character rendering.
			words := strings.Split(text, " ")
			spaceW := r.measureTextStr(" ", fontID, layer.FontFeatures)
			for i, word := range words {
				if word != "" {
					r.drawTextStr(word, fontID, x, baselineY, layer.FontFeatures)
					x += r.measureTextStr(word, fontID, layer.FontFeatures)
				}
				if i < len(words)-1 {
					x += spaceW + layer.WordSpacing
				}
			}
		}
	} else {
		r.drawTextStr(text, fontID, box.X, box.Y+ascent, layer.FontFeatures)
	}

	// Draw text decoration lines (underline, overline, line-through).
	r.drawTextDecoration(layer, text, box, fontID, ascent)

	// Draw text emphasis marks (small marks above/below each character).
	if layer.TextEmphasisMark != "" {
		r.drawTextEmphasis(layer, text, box, fontID, ascent)
	}
}

// drawTextEmphasis renders text emphasis marks (e.g., dots, circles, sesame)
// above or below each character. The mark is drawn at ~50% of the text font
// size, centered horizontally over each glyph.
func (r *Renderer) drawTextEmphasis(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	mark := layer.TextEmphasisMark
	emphFontSize := layer.FontSize * 0.5
	if emphFontSize < 4 {
		emphFontSize = 4
	}

	// Open a font for the emphasis mark at half size.
	fontPath := r.fonts.FontPathForFamilyWithSynthesis(
		layer.FontFamily, layer.FontBold, layer.FontItalic,
		layer.FontMono, layer.FontAhem,
		layer.FontSynthesisWeight, layer.FontSynthesisStyle)
	emphFontID := r.openFont(fontPath, emphFontSize)
	if emphFontID < 0 {
		return
	}

	r.setColor(layer.TextEmphasisColor)

	emphMetrics := r.dc.GetFontMetrics(emphFontID)
	emphAscent := float64(emphMetrics.Ascent) / 64.0
	emphDescent := float64(emphMetrics.Descent) / 64.0
	markW := r.dc.MeasureText(mark, emphFontID)

	// Compute vertical position of the emphasis mark.
	// Mirrors Blink at `third_party/blink/renderer/core/paint/text_painter.cc:519-528
	// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f`:
	//
	//   over:  mark baseline = baseline - baseAscent - emphDescent = box.Y - emphDescent
	//   under: mark baseline = baseline + baseDescent + emphAscent
	//
	// box.Y is the inline-box top, box.Y + ascent is the baseline, and
	// DrawText draws with the baseline at the y argument — so we set
	// emphY = (mark baseline) - emphAscent.
	baseMetrics := r.dc.GetFontMetrics(fontID)
	baseDescent := float64(baseMetrics.Descent) / 64.0
	var emphY float64
	if layer.TextEmphasisOver {
		emphY = box.Y - emphDescent - emphAscent
	} else {
		emphY = box.Y + ascent + baseDescent
	}

	// Iterate over each character and draw the mark centered above/below it.
	x := box.X
	for _, ch := range text {
		charStr := string(ch)
		charW := r.measureTextStr(charStr, fontID, layer.FontFeatures)

		// Skip whitespace characters — no emphasis marks on spaces.
		if !unicode.IsSpace(ch) {
			markX := x + (charW-markW)/2
			r.dc.DrawText(mark, emphFontID, markX, emphY+emphAscent)
		}

		x += charW
		if layer.LetterSpacing != 0 {
			x += layer.LetterSpacing
		}
		if ch == ' ' && layer.WordSpacing != 0 {
			x += layer.WordSpacing
		}
	}

	// Restore original text color.
	r.setColor(layer.TextColor)
}

// drawSidewaysEmphasisMarks renders text-emphasis marks for a sideways
// (vertical-rl + text-orientation: sideways / sideways-rl / sideways-lr)
// text run as per-character mini-fragments. Each mark is drawn into its
// own small off-screen RGBA, rotated CW/CCW to match the parent's writing
// mode, and blitted at the annotation-equivalent screen position with
// `int(math.Round(...))` snapping — mirroring louis14's ruby annotation
// paint path (`pkg/layout/inline_layout_ruby.go:60-83 @ b555e690`).
//
// Why per-fragment rather than drawing into the parent's off-screen
// buffer: ruby annotations apply `math.Round` at each fragment's blit;
// the parent text applies `math.Round` once at its own box position. The
// two snap anchors differ by up to ~1 sub-pixel each, which produces the
// 1-2 px per-glyph drift observed in LOU-162's first attempt. Snapping
// per fragment here aligns emphasis pixels with where ruby pixels would
// snap.
//
// ta and lh are the parent's off-screen horizontal-frame dimensions
// (text-advance and line-height); used to compute the rotation mapping.
func (r *Renderer) drawSidewaysEmphasisMarks(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64, ta, lh int) {
	mark := layer.TextEmphasisMark
	emphFontSize := layer.FontSize * 0.5
	if emphFontSize < 4 {
		emphFontSize = 4
	}
	fontPath := r.fonts.FontPathForFamilyWithSynthesis(
		layer.FontFamily, layer.FontBold, layer.FontItalic,
		layer.FontMono, layer.FontAhem,
		layer.FontSynthesisWeight, layer.FontSynthesisStyle)
	emphFontID := r.openFont(fontPath, emphFontSize)
	if emphFontID < 0 {
		return
	}
	emphMetrics := r.dc.GetFontMetrics(emphFontID)
	emphAscent := float64(emphMetrics.Ascent) / 64.0
	emphDescent := float64(emphMetrics.Descent) / 64.0
	markW := r.dc.MeasureText(mark, emphFontID)

	// Compute the annotation fragment's TOP edge and BASELINE in the parent's
	// off-screen HORIZONTAL frame (the "logical" frame where the base text was
	// rendered as horizontal). Mirrors louis14's ruby annotation positioning at
	// `pkg/layout/inline_layout_ruby.go:77-78 @ b555e690`:
	//
	//   annoBaseline = annotationBlockTop + math.Round(annoEmAscent)
	//
	// The `math.Round` on the ascent is load-bearing: it is the same per-fragment
	// integer-snap that LOU-161's ruby annotation paint produces, so emphasis
	// pixels snap to the SAME positions as ruby annotation pixels.
	//
	// The annotation's TOP edge (annotationBlockTop) follows Blink at
	// `third_party/blink/renderer/core/paint/text_painter.cc:519-528 @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f`:
	//
	//   kOver:  annotationBlockTop = -(emphAscent + emphDescent)
	//           — the strip just above the inline-box top.
	//   kUnder: annotationBlockTop = base_baseline + baseDescent
	//                              = ascent + baseDescent
	//           — the strip just below the inline-box bottom.
	// (baseAscent/baseDescent were used by the Blink-formula path; the
	// final formula matches ruby's annotation layout directly via
	// `screenX = box.X + lh` / `box.X - annotH` and does not need the
	// Blink offset values here. Keep the metrics fetched in case the
	// kUnder under-line allotment formula needs them later.)
	_ = fontID
	_ = ascent

	// The annotation-fragment's WIDTH (advance dimension in horizontal frame,
	// = HEIGHT in screen after sideways rotation) is markW. Its HEIGHT (block
	// dimension in horizontal frame, = WIDTH in screen after rotation) covers
	// the typo ascent + descent of the emphasis font.
	annotW := int(math.Ceil(markW)) + 1 // small safety margin against int rounding
	// Match ruby's annotation line-height = annotation-font-size: ceil(ascent+descent)
	// with no safety margin. Adding margin shifts the rotated glyph 1 px left in
	// screen X (the rotation maps the buffer's bottom edge to dest x=0).
	annotH := int(math.Ceil(emphAscent + emphDescent))

	if annotW <= 0 || annotH <= 0 {
		return
	}

	// Build the mini off-screen buffer ONCE per run. Glyph, font, color, and
	// rotation orientation are loop-invariant — every character gets the same
	// rotated mark, just blitted at a different position. Pre-rotating avoids
	// per-character `image.NewRGBA` + a nested pixel loop in the paint hot path.
	markSrc := image.NewRGBA(image.Rect(0, 0, annotW, annotH))
	markDC := r.dc.NewChildContext(markSrc)
	emphColor := layer.TextEmphasisColor
	markDC.SetColor(color.RGBA{R: emphColor.R, G: emphColor.G, B: emphColor.B, A: uint8(emphColor.A * 255)})
	markDC.DrawText(mark, emphFontID, 0, emphAscent)

	markRot := image.NewRGBA(image.Rect(0, 0, annotH, annotW))
	for yy := 0; yy < annotH; yy++ {
		for xx := 0; xx < annotW; xx++ {
			c := markSrc.RGBAAt(xx, yy)
			if c.A == 0 {
				continue
			}
			if layer.IsSidewaysRL {
				// 90° CW: src (x,y) → dest (annotH-1-y, x)
				markRot.SetRGBA(annotH-1-yy, xx, c)
			} else {
				// 90° CCW: src (x,y) → dest (y, annotW-1-x)
				markRot.SetRGBA(yy, annotW-1-xx, c)
			}
		}
	}

	// Determine which physical side the annotation paints on, and pre-compute
	// the loop-invariant screen X. In vertical-rl/sideways-rl, kOver
	// (TextEmphasisOver=true) = right of column; in sideways-lr the resolver
	// has already flipped so kOver = left of column. Combined predicate:
	// paint on the right edge when XNOR of IsSidewaysRL and Over.
	rightSide := layer.IsSidewaysRL == layer.TextEmphasisOver
	var screenX float64
	if rightSide {
		screenX = box.X + float64(lh) // top-left of annotation = right edge of base column
	} else {
		screenX = box.X - float64(annotH) // top-left of annotation = left of base column by buffer width
	}
	screenXi := int(math.Round(screenX))

	// Iterate characters and blit the (pre-rotated) mark at each non-whitespace
	// character's annotation-equivalent screen position. The sideways base text
	// at `:3605-3608` is drawn as a single `DrawText` call without per-character
	// LetterSpacing / WordSpacing adjustments, so the emphasis advance here is
	// the bare measured char width — applying spacing here would drift the marks
	// off the rendered glyphs (caught by CodeRabbit on the LOU-162 PR).
	x := 0.0
	for _, ch := range text {
		charStr := string(ch)
		charW := r.measureTextStr(charStr, fontID, layer.FontFeatures)

		if !unicode.IsSpace(ch) {
			// Annotation's inline origin (in off-screen horizontal frame),
			// centered over the character — mirrors
			// `pkg/layout/inline_layout_ruby.go:64-65 @ b555e690`
			//   `annoOffset += (column.InlineSize - anno.Width) / 2`.
			annotInline := x + (charW-markW)/2
			var screenY float64
			if layer.IsSidewaysRL {
				screenY = box.Y + annotInline
			} else {
				screenY = box.Y + float64(ta) - annotInline - float64(annotW)
			}
			r.dc.DrawImage(markRot, screenXi, int(math.Round(screenY)))
		}

		x += charW
	}
}

// smallCapsScale is the ratio of the small-caps glyph size to the normal size.
// Blink uses ~0.7; CSS Fonts Level 4 suggests using the font's x-height/cap-height
// ratio when available, but synthesis typically uses 0.7–0.8.
const smallCapsScale = 0.7

// drawTextSmallCaps renders text with font-variant-caps: small-caps or all-small-caps.
// Lowercase letters (and for all-small-caps, uppercase too) are drawn as uppercase
// glyphs at a reduced font size.  The baseline remains aligned with the normal text.
func (r *Renderer) drawTextSmallCaps(layer *PaintLayer, text string, box *layout.Box, fontPath string, fontID int32, ascent float64) {
	allSmall := layer.FontVariantCaps == "all-small-caps"

	// Open a second font at the reduced size for small-cap glyphs.
	smallSize := math.Round(layer.FontSize * smallCapsScale)
	if smallSize < 1 {
		smallSize = 1
	}
	smallFontID := r.openFont(fontPath, smallSize)
	if smallFontID < 0 {
		smallFontID = fontID // fallback: draw at normal size
	}
	smallMetrics := r.dc.GetFontMetrics(smallFontID)
	smallAscent := float64(smallMetrics.Ascent) / 64.0

	baselineY := box.Y + ascent
	x := box.X

	for _, ch := range text {
		s := string(ch)

		// Determine if this character should be drawn as a small cap.
		isSmallCap := false
		drawStr := s
		if allSmall {
			// all-small-caps: both upper and lowercase become small caps.
			if unicode.IsLetter(ch) {
				isSmallCap = true
				drawStr = strings.ToUpper(s)
			}
		} else {
			// small-caps: only lowercase letters become small caps.
			if unicode.IsLower(ch) {
				isSmallCap = true
				drawStr = strings.ToUpper(s)
			}
		}

		if isSmallCap {
			// Draw uppercase glyph at reduced size, baseline-aligned.
			// The small font has a smaller ascent; shift down so baselines match.
			smallBaselineY := baselineY + (ascent - smallAscent)
			r.drawTextStr(drawStr, smallFontID, x, smallBaselineY, layer.FontFeatures)
			charW := r.measureTextStr(drawStr, smallFontID, layer.FontFeatures)
			x += charW
		} else {
			// Draw at normal size.
			r.drawTextStr(drawStr, fontID, x, baselineY, layer.FontFeatures)
			charW := r.measureTextStr(drawStr, fontID, layer.FontFeatures)
			x += charW
		}

		// Apply letter-spacing after each character.
		x += layer.LetterSpacing
		if ch == ' ' {
			x += layer.WordSpacing
		}
	}
}

// drawTextWithTabs renders text containing tab characters. Tab characters
// advance to the next tab stop without drawing a glyph. Tab stops are at
// multiples of (tab-size × space-width) or (tab-size in px) from the line
// start (containing block's content edge).
func (r *Renderer) drawTextWithTabs(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	// Compute tab stop interval in pixels.
	tabSizeVal := layer.TabSize
	var tabStopPx float64
	if layer.TabSizeIsLength {
		tabStopPx = tabSizeVal
	} else {
		spaceW := r.measureTextStr(" ", fontID, layer.FontFeatures)
		if spaceW <= 0 {
			spaceW = layer.FontSize
		}
		tabStopPx = tabSizeVal * spaceW
	}
	if tabStopPx <= 0 {
		tabStopPx = layer.FontSize * 8
	}

	// Find the line start (containing block's content-area left edge).
	// Walk up from the text box to find the nearest block container.
	lineStartX := box.X
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Style != nil {
			lineStartX = p.X + p.Padding.Left + p.Border.Left
			break
		}
	}

	// Position within the line, measured from line start.
	posFromLineStart := box.X - lineStartX
	x := box.X
	baselineY := box.Y + ascent

	segments := strings.Split(text, "\t")
	for i, seg := range segments {
		if seg != "" {
			if layer.LetterSpacing != 0 {
				for _, ch := range seg {
					r.drawTextStr(string(ch), fontID, x, baselineY, layer.FontFeatures)
					charW := r.measureTextStr(string(ch), fontID, layer.FontFeatures)
					x += charW + layer.LetterSpacing
					posFromLineStart += charW + layer.LetterSpacing
					if ch == ' ' {
						x += layer.WordSpacing
						posFromLineStart += layer.WordSpacing
					}
				}
			} else {
				r.drawTextStr(seg, fontID, x, baselineY, layer.FontFeatures)
				segW := r.measureTextStr(seg, fontID, layer.FontFeatures)
				if layer.WordSpacing != 0 {
					segW += layer.WordSpacing * float64(strings.Count(seg, " "))
				}
				x += segW
				posFromLineStart += segW
			}
		}
		// After each segment except the last, advance to next tab stop.
		if i < len(segments)-1 {
			rem := math.Mod(posFromLineStart, tabStopPx)
			var tabW float64
			if rem < 1e-9 {
				tabW = tabStopPx
			} else {
				tabW = tabStopPx - rem
			}
			x += tabW
			posFromLineStart += tabW
		}
	}
}

// applyTextOverflowEllipsis truncates text and appends "…" when
// text-overflow:ellipsis is active on a block container with overflow:hidden.
// Returns the (possibly truncated) text string.
func (r *Renderer) applyTextOverflowEllipsis(layer *PaintLayer, text string, box *layout.Box, fontID int32) string {
	// Determine if text-overflow:ellipsis applies.
	// It may be set on the text run's own style (when the text node's
	// parent element is the block container) or on an ancestor.
	hasEllipsis := layer.TextOverflow == css.TextOverflowEllipsis

	// Walk up the box tree to find the nearest ancestor with overflow-x:hidden
	// and text-overflow:ellipsis.
	var clipAncestor *layout.Box
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Style == nil {
			continue
		}
		overflowX := p.Style.GetOverflowX()
		if overflowX == css.OverflowHidden || overflowX == css.OverflowScroll || overflowX == css.OverflowAuto {
			if !hasEllipsis && p.Style.GetTextOverflow() == css.TextOverflowEllipsis {
				hasEllipsis = true
			}
			if hasEllipsis {
				clipAncestor = p
			}
			break
		}
	}

	if !hasEllipsis || clipAncestor == nil {
		return text
	}

	// Compute available width: right edge of ancestor's padding box minus
	// the text run's starting X position.  The padding box is the overflow
	// clip boundary (CSS Overflow §3), so the ellipsis must fit within it.
	paddingRight := clipAncestor.X + clipAncestor.Width - clipAncestor.Border.Right
	availW := paddingRight - box.X
	if availW <= 0 {
		return text
	}

	// Account for letter-spacing and word-spacing in width measurements.
	ls := layer.LetterSpacing
	ws := layer.WordSpacing
	measureRunWidth := func(s string) float64 {
		if ls == 0 && ws == 0 {
			return r.measureTextStr(s, fontID, layer.FontFeatures)
		}
		total := 0.0
		runes := []rune(s)
		for i, ch := range runes {
			total += r.measureTextStr(string(ch), fontID, layer.FontFeatures)
			if i < len(runes)-1 {
				total += ls
			}
			if ch == ' ' {
				total += ws
			}
		}
		return total
	}

	textW := measureRunWidth(text)
	if textW <= availW {
		return text
	}

	// Text overflows — truncate and append ellipsis.
	const ellipsis = "\u2026" // "…"
	ellipsisW := r.measureTextStr(ellipsis, fontID, layer.FontFeatures)
	truncW := availW - ellipsisW

	if truncW <= 0 {
		// Not enough room even for the ellipsis — just show ellipsis.
		return ellipsis
	}

	// Find the truncation point by measuring characters.
	w := 0.0
	truncIdx := 0
	runes := []rune(text)
	for i, ch := range runes {
		cw := r.measureTextStr(string(ch), fontID, layer.FontFeatures)
		advance := cw
		if i < len(runes)-1 {
			advance += ls
		}
		if ch == ' ' {
			advance += ws
		}
		if w+cw > truncW {
			truncIdx = len(string(runes[:i]))
			break
		}
		w += advance
		truncIdx = len(string(runes[:i+1]))
	}

	return text[:truncIdx] + ellipsis
}

// drawTextShadows paints text shadows behind the actual text glyphs.
// Shadows are painted in reverse declaration order (last declared = behind).
func (r *Renderer) drawTextShadows(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	for i := len(layer.TextShadows) - 1; i >= 0; i-- {
		shadow := layer.TextShadows[i]
		if shadow.Blur > 0 {
			r.drawBlurredTextShadow(layer, text, box, fontID, ascent, shadow)
		} else {
			// No blur: just draw text at offset position with shadow color.
			r.setColor(shadow.Color)
			if layer.LetterSpacing != 0 || layer.WordSpacing != 0 {
				x := box.X + shadow.OffsetX
				baselineY := box.Y + ascent + shadow.OffsetY
				for _, ch := range text {
					r.drawTextStr(string(ch), fontID, x, baselineY, layer.FontFeatures)
					charW := r.measureTextStr(string(ch), fontID, layer.FontFeatures)
					x += charW + layer.LetterSpacing
					if ch == ' ' {
						x += layer.WordSpacing
					}
				}
			} else {
				r.drawTextStr(text, fontID, box.X+shadow.OffsetX, box.Y+ascent+shadow.OffsetY, layer.FontFeatures)
			}
		}
	}
	// Restore text color for actual text drawing.
	r.setColor(layer.TextColor)
}

// drawBlurredTextShadow renders a single text shadow with Gaussian-like blur
// by drawing text into an offscreen buffer, applying box blur, then compositing.
func (r *Renderer) drawBlurredTextShadow(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64, shadow css.TextShadow) {
	// Measure text to determine buffer size.
	textWidth := r.measureTextStr(text, fontID, layer.FontFeatures)
	metrics := r.dc.GetFontMetrics(fontID)
	textHeight := float64(metrics.Ascent-metrics.Descent) / 64.0

	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	bw := int(math.Ceil(textWidth + 2*extend))
	bh := int(math.Ceil(textHeight + 2*extend))
	if bw <= 0 || bh <= 0 || bw > 2000 || bh > 2000 {
		// Fallback: draw without blur.
		r.setColor(shadow.Color)
		r.drawTextStr(text, fontID, box.X+shadow.OffsetX, box.Y+ascent+shadow.OffsetY, layer.FontFeatures)
		return
	}

	// Render text into offscreen buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	childDC := r.dc.NewChildContext(buf)
	childDC.SetColor(color.RGBA{
		R: shadow.Color.R,
		G: shadow.Color.G,
		B: shadow.Color.B,
		A: uint8(shadow.Color.A * 255),
	})
	if len(layer.FontFeatures) > 0 {
		childDC.DrawTextWithFeatures(text, fontID, extend, extend+ascent, layer.FontFeatures)
	} else {
		childDC.DrawText(text, fontID, extend, extend+ascent)
	}

	// Apply 3-pass box blur (approximates Gaussian blur).
	blurRadius := int(math.Round(sigma))
	boxBlur(buf, blurRadius)
	boxBlur(buf, blurRadius)
	boxBlur(buf, blurRadius)

	// Composite back at shadow offset position.
	dx := int(math.Round(box.X + shadow.OffsetX - extend))
	dy := int(math.Round(box.Y + shadow.OffsetY - extend))
	r.dc.DrawImage(buf, dx, dy)
}

// isDecorationSkippableSpace reports whether a rune counts as a "space" for
// the text-decoration-skip-spaces property (CSS Text Decor L4). Includes
// ASCII whitespace plus the Unicode space characters U+00A0, U+1680,
// U+2000-U+200A, U+202F, U+205F, U+3000 — the cohort tested by the WPT
// `text-decoration-skip-spaces-*` reftests.
func isDecorationSkippableSpace(ch rune) bool {
	switch ch {
	case ' ', '\t', '\n', '\r',
		0x00A0, // NBSP
		0x1680, // OGHAM SPACE MARK
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005,
		0x2006, 0x2007, 0x2008, 0x2009, 0x200A,
		0x202F, // NARROW NO-BREAK SPACE
		0x205F, // MEDIUM MATHEMATICAL SPACE
		0x3000: // IDEOGRAPHIC SPACE
		return true
	}
	return false
}

// leadingWhitespacePrefix returns the longest prefix of text consisting of
// runes that isDecorationSkippableSpace accepts.
func leadingWhitespacePrefix(text string) string {
	for i, ch := range text {
		if !isDecorationSkippableSpace(ch) {
			return text[:i]
		}
	}
	return text
}

// trailingWhitespaceSuffix returns the longest suffix of text consisting of
// runes that isDecorationSkippableSpace accepts.
func trailingWhitespaceSuffix(text string) string {
	endByte := len(text)
	for i := len(text); i > 0; {
		ch, size := utf8.DecodeLastRuneInString(text[:i])
		if !isDecorationSkippableSpace(ch) {
			return text[endByte:]
		}
		i -= size
		endByte = i
	}
	return text
}

// drawTextDecoration renders underline, overline, or line-through decoration
// lines for non-vertical, non-sideways text.
//
// CSS Text Decor 3 §2: a text run paints every AppliedTextDecoration on its
// accumulated vector (one per ancestor that established a decoration). When
// the vector is empty, the legacy single-decoration fallback below paints —
// used for anonymous boxes synthesized without a cascade pass.
func (r *Renderer) drawTextDecoration(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	// Fast path when there are no accumulated decorations and no legacy entry.
	if len(layer.AppliedTextDecorations) == 0 &&
		(layer.TextDecoration == css.TextDecorationNone || layer.TextDecoration == "") {
		return
	}

	metrics := r.dc.GetFontMetrics(fontID)
	descent := float64(metrics.Descent) / 64.0
	textWidth := r.measureTextStr(text, fontID, layer.FontFeatures)

	// CSS Text Decor 4 §"text-decoration-skip-spaces-property": when
	// `text-decoration-skip-spaces` enables skipping at the start/end of the
	// line, the decoration line does not paint over leading/trailing
	// whitespace characters. Layout doesn't currently pass line-start /
	// line-end flags to the painter, so we apply a conservative heuristic:
	// suppress the leading/trailing whitespace WIDTH whenever the text run
	// HAS leading/trailing whitespace. This works for the dominant case
	// where each text fragment corresponds to a contiguous shaped run, and
	// the leading/trailing whitespace either is the boundary OR is collapsed
	// at the start/end of an inline context (white-space:pre and friends).
	// Inter-word whitespace remains underlined because it's interior to the
	// run.
	//
	// To avoid sub-pixel drift between measuring the prefix in isolation
	// versus in context (HarfBuzz shaping can produce slightly different
	// advances), the leading-width is derived as
	//   textWidth - measure(text-without-leading-ws)
	// so the prefix's contribution matches the in-context advance exactly.
	// The trailing-width is computed the same way against the head segment.
	var skipStartWidth, skipEndWidth float64
	if box != nil && box.Style != nil && text != "" {
		skip := box.Style.GetTextDecorationSkipSpaces()
		if skip.SkipStart {
			leading := leadingWhitespacePrefix(text)
			if leading != "" && leading != text {
				tail := text[len(leading):]
				tailWidth := r.measureTextStr(tail, fontID, layer.FontFeatures)
				skipStartWidth = textWidth - tailWidth
				if skipStartWidth < 0 {
					skipStartWidth = 0
				}
			}
		}
		if skip.SkipEnd {
			trailing := trailingWhitespaceSuffix(text)
			if trailing != "" && trailing != text {
				head := text[:len(text)-len(trailing)]
				headWidth := r.measureTextStr(head, fontID, layer.FontFeatures)
				skipEndWidth = textWidth - headWidth
				if skipEndWidth < 0 {
					skipEndWidth = 0
				}
			}
		}
	}

	if len(layer.AppliedTextDecorations) > 0 {
		// TODO: pass the font's underline-thickness metric here once it's
		// plumbed through textshape.FontMetrics; 0 makes from-font fall back
		// to auto (which is correct, just lossy).
		info := newTextDecorationInfo(box, textWidth, layer.FontSize, ascent, descent, 0)
		for _, td := range layer.AppliedTextDecorations {
			r.drawOneAppliedTextDecoration(td, info, box, textWidth, skipStartWidth, skipEndWidth)
		}
		return
	}

	// Legacy fallback (anonymous boxes / synthetic styles). Same rect-top
	// semantics as drawOneAppliedTextDecoration above — see the paintLine
	// comments there for the contract: rectTop for solid, centerY for
	// non-solid.
	r.setColor(layer.TextDecorationColor)
	r.dc.SetLineWidth(layer.TextDecorationThickness)

	thickness := layer.TextDecorationThickness

	var rectTop, centerY float64
	var isLineThrough bool
	switch layer.TextDecoration {
	case css.TextDecorationUnderline:
		centerY = box.Y + ascent + math.Abs(descent)*0.25 + layer.TextUnderlineOffset
		rectTop = centerY // pre-port lineY value is already Blink's paint_underline_offset (rect TOP)
	case css.TextDecorationOverline:
		centerY = box.Y
		rectTop = centerY - thickness // overline rect grows UPWARD from TextTop
	case css.TextDecorationLineThrough:
		centerY = box.Y + ascent*0.65
		rectTop = centerY // unused for line-through (stays stroke-centered)
		isLineThrough = true
	default:
		return
	}

	legacyXStart := box.X + skipStartWidth
	legacyXEnd := box.X + textWidth - skipEndWidth
	if legacyXEnd <= legacyXStart {
		return
	}
	switch layer.TextDecorationStyle {
	case "dashed":
		r.drawDashedLine(legacyXStart, centerY, legacyXEnd, centerY, thickness)
	case "dotted":
		r.drawDottedLine(legacyXStart, centerY, legacyXEnd, centerY, thickness)
	case "double":
		r.drawDoubleLine(legacyXStart, centerY, legacyXEnd, centerY, thickness)
	case "wavy":
		r.drawWavyLine(legacyXStart, centerY, legacyXEnd-legacyXStart, thickness)
	default: // "solid"
		if isLineThrough {
			r.dc.MoveTo(legacyXStart, centerY)
			r.dc.LineTo(legacyXEnd, centerY)
			r.dc.Stroke()
		} else {
			// See paintLine above for why path-fill, not FillRectangle.
			r.dc.DrawRectangle(legacyXStart, rectTop, legacyXEnd-legacyXStart, thickness)
			r.dc.Fill()
		}
	}
}

// drawOneAppliedTextDecoration paints a single AppliedTextDecoration entry on a
// text run. Geometry (resolved thickness + per-line Y) comes from the shared
// textDecorationInfo built by drawTextDecoration. Mirrors one iteration of
// Blink's `TextDecorationInfo` paint loop (core/paint/text_decoration_info.cc
// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// td.Color is already resolved at cascade time per CSS Text Decor 3 §2 — no
// currentcolor substitution needed here.
func (r *Renderer) drawOneAppliedTextDecoration(td css.AppliedTextDecoration, info textDecorationInfo, box *layout.Box, textWidth, skipStartWidth, skipEndWidth float64) {
	if td.Lines.IsNone() {
		return
	}

	thickness := info.computeThickness(td)

	// LOU-149 Phase 4: when this fragment is part of a multi-fragment
	// decorating box, layout has stamped the decorating-box extent + edge
	// flags on td (see css.AppliedTextDecoration). The painter paints the
	// SLICE of the logical decoration that falls within this fragment, with
	// the inset trim applied only at the decorating box's actual logical
	// edges. HasDecoratingBox=false → LOU-142 single-fragment fallback.
	var logicalStart, logicalEnd float64
	if td.HasDecoratingBox {
		logicalStart = box.X + td.DecoratingBoxOffsetX
		logicalEnd = logicalStart + td.DecoratingBoxWidth
		if td.IsFirstFragment {
			logicalStart += td.Inset.InlineStart
		}
		if td.IsLastFragment {
			logicalEnd -= td.Inset.InlineEnd
		}
	} else {
		logicalStart = box.X + td.Inset.InlineStart
		logicalEnd = box.X + textWidth - td.Inset.InlineEnd
	}
	// text-decoration-skip-spaces: trim leading/trailing whitespace widths
	// from the decoration extent. Applied after inset so a negative inset
	// (extension) does not extend through skipped whitespace. For the
	// HasDecoratingBox case only the relevant edge is trimmed — interior
	// fragments don't carry leading/trailing space at the boundary.
	if skipStartWidth > 0 {
		if !td.HasDecoratingBox || td.IsFirstFragment {
			logicalStart += skipStartWidth
		}
	}
	if skipEndWidth > 0 {
		if !td.HasDecoratingBox || td.IsLastFragment {
			logicalEnd -= skipEndWidth
		}
	}
	if logicalEnd <= logicalStart {
		return
	}

	// Clamp at INTERIOR fragment boundaries only. The first fragment's left
	// edge and the last fragment's right edge are the decorating box's OUTER
	// edges where a negative text-decoration-inset (= extension) legitimately
	// overflows the fragment. Clamp uses box.Width (layout-time) to match
	// the layout-time DecoratingBoxOffsetX / Width values; clamping by paint-
	// time textWidth would leak sub-pixel drift between adjacent fragments.
	xStart := logicalStart
	xEnd := logicalEnd
	if td.HasDecoratingBox {
		if !td.IsFirstFragment && box.X > xStart {
			xStart = box.X
		}
		if !td.IsLastFragment && box.X+box.Width < xEnd {
			xEnd = box.X + box.Width
		}
		if xEnd <= xStart {
			return
		}
	}

	phaseFromLogicalStart := xStart - logicalStart

	r.setColor(td.Color)
	r.dc.SetLineWidth(thickness)

	// paintLine paints one decoration stroke (under/over/through) on the
	// SOLID path as a top-aligned filled rectangle, or on a non-solid path
	// (dashed/dotted/double/wavy) using stroke-centered geometry around
	// `centerY`. Mirrors Blink's `TextDecorationInfo::ComputeLineData`
	// (text_decoration_info.cc @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f),
	// which builds the line rect as
	//   gfx::RectF(start_point, gfx::SizeF(width, thickness))
	// growing DOWN from `start_point.y`. For under/over the rect-top differs
	// from the stroke center; for line-through Blink's offset already centers
	// the rect on `2*ascent/3` (it subtracts thickness/2 from the rect top),
	// so we can keep the existing stroke-centered geometry there.
	//
	// rectTop = the y-coordinate of the rect's TOP edge for the SOLID path.
	// centerY = the y-coordinate of the stroke's CENTER for non-solid styles.
	paintLine := func(rectTop, centerY float64) {
		switch td.Style {
		case "dashed":
			r.drawDashedLinePhased(xStart, centerY, xEnd, centerY, thickness, phaseFromLogicalStart)
		case "dotted":
			r.drawDottedLinePhased(xStart, centerY, xEnd, centerY, thickness, phaseFromLogicalStart)
		case "double":
			r.drawDoubleLine(xStart, centerY, xEnd, centerY, thickness)
		case "wavy":
			r.drawWavyLinePhased(xStart, centerY, xEnd-xStart, thickness, phaseFromLogicalStart)
		default: // "solid"
			// Path-based fill respects the layer's overflow:auto/hidden clip
			// (the rasterizer's fillWithPattern applies dc.gs.clipMask/
			// clipRect). FillRectangle's fast path bypasses that clip and
			// leaks underlines outside the scroll box (regresses
			// text-underline-offset-scroll-001).
			r.dc.DrawRectangle(xStart, rectTop, xEnd-xStart, thickness)
			r.dc.Fill()
		}
	}

	if td.Lines.Has(css.TextDecorationLineUnderline) {
		// computeUnderlineLineY returns Blink's `paint_underline_offset`
		// (= rect TOP for solid). The same value is the stroke center for
		// non-solid styles in louis14's pre-port renderer — keeping it for
		// dashed/dotted/double/wavy preserves their pixel-exact baseline.
		lineY := info.computeUnderlineLineY(td)
		paintLine(lineY, lineY)
	}
	if td.Lines.Has(css.TextDecorationLineOverline) {
		// computeOverlineLineY returns the TextTop position (box.Y), which
		// is the stroke center for the non-solid path (matches the
		// pre-port baseline). Blink's overline rect spans
		// [TextTop - thickness, TextTop], so for the SOLID path the rect
		// TOP is `TextTop - thickness` (overline grows UPWARD from
		// text-top — see text-decoration-thickness-overline-001).
		centerY := info.computeOverlineLineY()
		paintLine(centerY-thickness, centerY)
	}
	if td.Lines.Has(css.TextDecorationLineLineThrough) {
		// Line-through stays stroke-centered for all styles. Blink centers
		// at `2*ascent/3` (it adds the `-thickness/2` offset to keep the
		// stroke around that center point). Switching to rect-top would
		// regress text-decoration-thickness-linethrough-001 which uses
		// 1.1em thickness on a 1em-tall box.
		lineY := info.computeLineThroughLineY(thickness)
		switch td.Style {
		case "dashed":
			r.drawDashedLinePhased(xStart, lineY, xEnd, lineY, thickness, phaseFromLogicalStart)
		case "dotted":
			r.drawDottedLinePhased(xStart, lineY, xEnd, lineY, thickness, phaseFromLogicalStart)
		case "double":
			r.drawDoubleLine(xStart, lineY, xEnd, lineY, thickness)
		case "wavy":
			r.drawWavyLinePhased(xStart, lineY, xEnd-xStart, thickness, phaseFromLogicalStart)
		default: // "solid"
			r.dc.MoveTo(xStart, lineY)
			r.dc.LineTo(xEnd, lineY)
			r.dc.Stroke()
		}
	}
}

// wrapIntoPeriod returns v's canonical representative in [0, period). math.Mod
// returns same-signed values for negative inputs, so we normalize once here
// rather than scatter sign fixes through the phased draw helpers below.
func wrapIntoPeriod(v, period float64) float64 {
	v = math.Mod(v, period)
	if v < 0 {
		v += period
	}
	return v
}

// drawDottedLinePhased, drawDashedLinePhased, and drawWavyLinePhased are the
// LOU-149 Phase 4 phased variants of the existing line-style draw funcs.
// phaseFromLogicalStart shifts the pattern's logical origin so that adjacent
// fragments of one decorating box continue the same dot/dash/wave cycle
// across the fragment boundary. With phaseFromLogicalStart=0 each one's
// output matches its unphased counterpart byte-for-byte.

func (r *Renderer) drawDottedLinePhased(x1, y1, x2, y2, width, phaseFromLogicalStart float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}

	dotRadius := width / 2
	spacing := width * 2
	ux, uy := dx/length, dy/length

	along := wrapIntoPeriod(dotRadius-phaseFromLogicalStart, spacing)
	for along < length {
		cx := x1 + ux*along
		cy := y1 + uy*along
		r.dc.DrawCircle(cx, cy, dotRadius)
		r.dc.Fill()
		along += spacing
	}
}

func (r *Renderer) drawDashedLinePhased(x1, y1, x2, y2, width, phaseFromLogicalStart float64) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}

	dashLen := width * 3
	gapLen := width * 3
	period := dashLen + gapLen
	ux, uy := dx/length, dy/length
	nx, ny := -uy, ux
	hw := width / 2

	// Start one period to the left of the slice so the loop's first
	// iteration can paint a partial dash that bleeds in from x1.
	along := wrapIntoPeriod(-phaseFromLogicalStart, period) - period
	for along < length {
		segStart := along
		segEnd := along + dashLen
		if segStart < 0 {
			segStart = 0
		}
		if segEnd > length {
			segEnd = length
		}
		if segEnd > segStart {
			sx, sy := x1+ux*segStart, y1+uy*segStart
			ex, ey := x1+ux*segEnd, y1+uy*segEnd
			r.dc.MoveTo(sx+nx*hw, sy+ny*hw)
			r.dc.LineTo(ex+nx*hw, ey+ny*hw)
			r.dc.LineTo(ex-nx*hw, ey-ny*hw)
			r.dc.LineTo(sx-nx*hw, sy-ny*hw)
			r.dc.ClosePath()
			r.dc.Fill()
		}
		along += period
	}
}

func (r *Renderer) drawWavyLinePhased(x, y, width, thickness, phaseFromLogicalStart float64) {
	if width <= 0 {
		return
	}
	amplitude := thickness * 1.5
	wavelength := thickness * 4
	if wavelength < 4 {
		wavelength = 4
	}
	halfWave := wavelength / 2

	r.dc.SetLineWidth(thickness)

	startPhase := wrapIntoPeriod(phaseFromLogicalStart, wavelength)
	up := startPhase < halfWave
	// First flip is at the next half-wave boundary in the cycle.
	var nextFlip float64
	if up {
		nextFlip = halfWave - startPhase
	} else {
		nextFlip = wavelength - startPhase
	}

	r.dc.MoveTo(x, y)
	cx := 0.0
	for cx < width {
		endX := cx + nextFlip
		if endX > width {
			endX = width
		}
		midX := (cx + endX) / 2
		if up {
			r.dc.QuadraticTo(x+midX, y-amplitude, x+endX, y)
		} else {
			r.dc.QuadraticTo(x+midX, y+amplitude, x+endX, y)
		}
		cx = endX
		up = !up
		nextFlip = halfWave
	}
	r.dc.Stroke()
}

// drawWavyLine draws a wavy (sinusoidal) decoration line using quadratic curves.
func (r *Renderer) drawWavyLine(x, y, width, thickness float64) {
	if width <= 0 {
		return
	}
	amplitude := thickness * 1.5
	wavelength := thickness * 4
	if wavelength < 4 {
		wavelength = 4
	}
	halfWave := wavelength / 2

	r.dc.SetLineWidth(thickness)
	r.dc.MoveTo(x, y)

	cx := x
	up := true
	for cx < x+width {
		endX := cx + halfWave
		if endX > x+width {
			endX = x + width
		}
		midX := (cx + endX) / 2
		if up {
			r.dc.QuadraticTo(midX, y-amplitude, endX, y)
		} else {
			r.dc.QuadraticTo(midX, y+amplitude, endX, y)
		}
		cx = endX
		up = !up
	}
	r.dc.Stroke()
}

// SavePNG writes the rendered image to a PNG file.
func (r *Renderer) SavePNG(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, r.target)
}

// hasOverflowClipping returns true if the box has overflow:hidden/scroll/auto/clip
// on either axis, or paint containment (which also clips and contains positioned descendants).
func hasOverflowClipping(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	clipping := func(o css.OverflowType) bool {
		return o == css.OverflowHidden || o == css.OverflowScroll || o == css.OverflowAuto || o == css.OverflowClip
	}
	if clipping(box.Style.GetOverflowX()) || clipping(box.Style.GetOverflowY()) {
		return true
	}
	// CSS Containment: paint containment clips content and contains descendants.
	return box.Style.HasPaintContainment()
}

// isContainedByOverflow returns true if the child is clipped by the parent's
// overflow/containment and should NOT escape to the ancestor stacking context.
func isContainedByOverflow(child, parent *layout.Box) bool {
	if parent.Style == nil {
		return false
	}
	// CSS Containment: layout and paint containment contain all positioned
	// descendants (absolute and fixed), acting as a containing block.
	if parent.Style.HasLayoutContainment() || parent.Style.HasPaintContainment() {
		if child.Position != css.PositionStatic {
			return true
		}
	}
	if !hasOverflowClipping(parent) {
		return false
	}
	// position:relative and position:sticky children are always clipped by parent's overflow.
	if child.Position == css.PositionRelative || child.Position == css.PositionSticky {
		return true
	}
	// position:absolute children are clipped only when the parent is
	// positioned (i.e., is the containing block for the abs-pos child).
	if child.Position == css.PositionAbsolute && parent.Position != css.PositionStatic {
		return true
	}
	return false
}

// drawOutsetBoxShadows paints outset box shadows behind the element.
// Shadows are painted in reverse declaration order (last = behind).
func (r *Renderer) drawOutsetBoxShadows(layer *PaintLayer) {
	box := layer.Box
	x, y, w, h := pixelSnap(box.X, box.Y, box.Width, box.Height)

	for i := len(layer.BoxShadows) - 1; i >= 0; i-- {
		shadow := layer.BoxShadows[i]
		if shadow.Inset {
			continue
		}

		// Shadow rectangle: offset by (offsetX, offsetY), expanded by spread.
		sx := x + shadow.OffsetX - shadow.Spread
		sy := y + shadow.OffsetY - shadow.Spread
		sw := w + 2*shadow.Spread
		sh := h + 2*shadow.Spread

		if sw <= 0 || sh <= 0 {
			continue
		}

		// Expand border radii by spread for the shadow shape using the CSS
		// spec's cubic interpolation formula (§5.4 shadow shape).
		sp := shadow.Spread
		shadowRadii := layer.BorderRadius.OutsetForBoxShadow(sp, sp, sp, sp)

		// Use offscreen buffer to clip out the border box from the shadow.
		// Per CSS spec: outset shadows are only visible outside the border edge.
		r.drawOutsetShadowBuffer(sx, sy, sw, sh, shadowRadii,
			x, y, w, h, layer.BorderRadius, shadow)
	}
}

// drawOutsetShadowBuffer renders an outset shadow using an offscreen buffer.
// Fills the shadow shape, clears the border box area, optionally blurs, then composites.
func (r *Renderer) drawOutsetShadowBuffer(
	sx, sy, sw, sh float64, shadowRadii css.EllipticalRadii,
	bx, by, bw, bh float64, borderRadii css.EllipticalRadii,
	shadow css.BoxShadow,
) {
	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	// Compute the bounding box that covers both shadow and border box + blur.
	minX := math.Min(sx, bx) - extend
	minY := math.Min(sy, by) - extend
	maxX := math.Max(sx+sw, bx+bw) + extend
	maxY := math.Max(sy+sh, by+bh) + extend

	bufW := int(math.Ceil(maxX - minX))
	bufH := int(math.Ceil(maxY - minY))
	if bufW <= 0 || bufH <= 0 || bufW > 4000 || bufH > 4000 {
		return
	}

	buf := image.NewRGBA(image.Rect(0, 0, bufW, bufH))

	childDC := r.dc.NewChildContext(buf)
	childDC.SetColor(color.RGBA{
		R: shadow.Color.R,
		G: shadow.Color.G,
		B: shadow.Color.B,
		A: uint8(shadow.Color.A * 255),
	})

	lsx, lsy := sx-minX, sy-minY
	lbx, lby := bx-minX, by-minY

	if !shadowRadii.IsZero() || !borderRadii.IsZero() {
		// When rounded corners are involved, use even-odd fill in a single
		// rasterization pass. This ensures the outer and inner boundaries
		// share identical sub-pixel coverage decisions, preventing
		// anti-aliasing gaps at rounded corners.
		if !shadowRadii.IsZero() {
			buildRoundedRectPathOnDC(childDC, lsx, lsy, sw, sh, shadowRadii)
		} else {
			childDC.DrawRectangle(lsx, lsy, sw, sh)
		}
		if !borderRadii.IsZero() {
			buildRoundedRectPathReverseDC(childDC, lbx, lby, bw, bh, borderRadii)
		} else {
			childDC.MoveTo(lbx, lby)
			childDC.LineTo(lbx, lby+bh)
			childDC.LineTo(lbx+bw, lby+bh)
			childDC.LineTo(lbx+bw, lby)
			childDC.ClosePath()
		}
		childDC.SetFillRule(textshape.FillRuleEvenOdd)
		childDC.Fill()
		childDC.SetFillRule(textshape.FillRuleWinding)
	} else {
		// For purely rectangular shadows, use the two-pass approach:
		// fill the outer shape, then clear the inner hole via mask.
		childDC.DrawRectangle(lsx, lsy, sw, sh)
		childDC.Fill()

		holeMask := image.NewRGBA(image.Rect(0, 0, bufW, bufH))
		holeDC := r.dc.NewChildContext(holeMask)
		holeDC.SetColor(color.White)
		holeDC.DrawRectangle(lbx, lby, bw, bh)
		holeDC.Fill()

		for py := 0; py < bufH; py++ {
			for px := 0; px < bufW; px++ {
				moff := py*holeMask.Stride + px*4
				a := holeMask.Pix[moff+3]
				if a > 0 {
					off := py*buf.Stride + px*4
					if a == 255 {
						buf.Pix[off+0] = 0
						buf.Pix[off+1] = 0
						buf.Pix[off+2] = 0
						buf.Pix[off+3] = 0
					} else {
						keep := 255 - uint16(a)
						buf.Pix[off+0] = uint8(uint16(buf.Pix[off+0]) * keep / 255)
						buf.Pix[off+1] = uint8(uint16(buf.Pix[off+1]) * keep / 255)
						buf.Pix[off+2] = uint8(uint16(buf.Pix[off+2]) * keep / 255)
						buf.Pix[off+3] = uint8(uint16(buf.Pix[off+3]) * keep / 255)
					}
				}
			}
		}
	}

	// Apply box blur if needed.
	if shadow.Blur > 0 {
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
	}

	r.dc.DrawImage(buf, int(math.Round(minX)), int(math.Round(minY)))
}

// drawInsetBoxShadows paints inset box shadows inside the element,
// after background and borders. Per CSS spec, inset shadows are drawn
// inside the padding box, creating a "donut" between the box edge and
// the shadow inner rect.
func (r *Renderer) drawInsetBoxShadows(layer *PaintLayer) {
	box := layer.Box
	// Inset shadows paint inside the padding box.
	px := box.X + box.Border.Left
	py := box.Y + box.Border.Top
	pw := box.Width - box.Border.Left - box.Border.Right
	ph := box.Height - box.Border.Top - box.Border.Bottom
	px, py, pw, ph = pixelSnap(px, py, pw, ph)

	if pw <= 0 || ph <= 0 {
		return
	}

	// Compute inner border radii (radii shrink by border width) — Blink's Inset.
	innerRadii := layer.BorderRadius.Inset(box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left)

	for i := len(layer.BoxShadows) - 1; i >= 0; i-- {
		shadow := layer.BoxShadows[i]
		if !shadow.Inset {
			continue
		}

		// Inner shadow rect: padding-box shrunk by spread, offset.
		// Positive spread shrinks the hole (makes shadow wider).
		ix := px + shadow.Spread + shadow.OffsetX
		iy := py + shadow.Spread + shadow.OffsetY
		iw := pw - 2*shadow.Spread
		ih := ph - 2*shadow.Spread

		// Adjust inner radii by spread using the CSS spec formula.
		// Positive spread shrinks radii (simple inset).
		// Negative spread grows radii (use cubic interpolation formula).
		sp := shadow.Spread
		var shadowInnerRadii css.EllipticalRadii
		if sp >= 0 {
			shadowInnerRadii = innerRadii.Inset(sp, sp, sp, sp)
		} else {
			shadowInnerRadii = innerRadii.OutsetForBoxShadow(-sp, -sp, -sp, -sp)
		}

		r.setColor(shadow.Color)

		// Use offscreen buffer for all inset shadows: fill the padding box
		// with shadow color, clear the inner hole, optionally blur, then
		// composite clipped to the padding box.
		r.drawInsetShadowBuffer(px, py, pw, ph, innerRadii,
			ix, iy, iw, ih, shadowInnerRadii, shadow)
	}
}

// drawInsetShadowBuffer renders an inset shadow using an offscreen buffer.
// Fills the buffer with shadow color, clears the inner hole, optionally blurs,
// then clips to the padding box and composites.
func (r *Renderer) drawInsetShadowBuffer(
	px, py, pw, ph float64, clipRadii css.EllipticalRadii,
	ix, iy, iw, ih float64, innerRadii css.EllipticalRadii,
	shadow css.BoxShadow,
) {
	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	// Buffer covers the padding box + blur extend.
	bx := px - extend
	by := py - extend
	bw := int(math.Ceil(pw + 2*extend))
	bh := int(math.Ceil(ph + 2*extend))
	if bw <= 0 || bh <= 0 || bw > 2000 || bh > 2000 {
		return
	}

	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))

	// Fill the entire buffer with shadow color.
	sc := color.RGBA{
		R: shadow.Color.R,
		G: shadow.Color.G,
		B: shadow.Color.B,
		A: uint8(shadow.Color.A * 255),
	}
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			off := y*buf.Stride + x*4
			buf.Pix[off+0] = sc.R
			buf.Pix[off+1] = sc.G
			buf.Pix[off+2] = sc.B
			buf.Pix[off+3] = sc.A
		}
	}

	// Clear the inner hole (set to transparent) by drawing the inner shape
	// as a mask and clearing pixels inside it. Uses the draw context's
	// rasterizer for rounded corners to match other path rendering exactly.
	if iw > 0 && ih > 0 {
		lix := ix - bx
		liy := iy - by

		// Rasterize the inner shape to a mask buffer.
		holeMask := image.NewRGBA(image.Rect(0, 0, bw, bh))
		childDC := r.dc.NewChildContext(holeMask)
		childDC.SetColor(color.White)
		if !innerRadii.IsZero() {
			buildRoundedRectPathOnDC(childDC, lix, liy, iw, ih, innerRadii)
			childDC.Fill()
		} else {
			childDC.DrawRectangle(lix, liy, iw, ih)
			childDC.Fill()
		}

		// Clear pixels where the hole mask is opaque.
		for y := 0; y < bh; y++ {
			for x := 0; x < bw; x++ {
				moff := y*holeMask.Stride + x*4
				a := holeMask.Pix[moff+3] // alpha channel
				if a > 0 {
					off := y*buf.Stride + x*4
					if a == 255 {
						buf.Pix[off+0] = 0
						buf.Pix[off+1] = 0
						buf.Pix[off+2] = 0
						buf.Pix[off+3] = 0
					} else {
						// Partial coverage: blend.
						keep := 255 - uint16(a)
						buf.Pix[off+0] = uint8(uint16(buf.Pix[off+0]) * keep / 255)
						buf.Pix[off+1] = uint8(uint16(buf.Pix[off+1]) * keep / 255)
						buf.Pix[off+2] = uint8(uint16(buf.Pix[off+2]) * keep / 255)
						buf.Pix[off+3] = uint8(uint16(buf.Pix[off+3]) * keep / 255)
					}
				}
			}
		}
	}

	// Apply box blur if needed.
	if shadow.Blur > 0 {
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
	}

	// Clip the buffer to the padding box before compositing.
	r.clipInsetShadowBuffer(buf, bx, by, px, py, pw, ph, clipRadii)

	r.dc.DrawImage(buf, int(math.Round(bx)), int(math.Round(by)))
}

// clipInsetShadowBuffer zeroes pixels outside the clip rect in a shadow buffer.
// Uses a rasterized clip mask for rounded corners to match path rendering exactly.
func (r *Renderer) clipInsetShadowBuffer(buf *image.RGBA, bx, by, cx, cy, cw, ch float64, radii css.EllipticalRadii) {
	bounds := buf.Bounds()
	bw, bh := bounds.Dx(), bounds.Dy()
	hasRadius := !radii.IsZero()

	if !hasRadius {
		// Simple rectangular clip.
		for y := 0; y < bh; y++ {
			for x := 0; x < bw; x++ {
				px := bx + float64(x)
				py := by + float64(y)
				if px < cx || px >= cx+cw || py < cy || py >= cy+ch {
					off := y*buf.Stride + x*4
					buf.Pix[off+0] = 0
					buf.Pix[off+1] = 0
					buf.Pix[off+2] = 0
					buf.Pix[off+3] = 0
				}
			}
		}
		return
	}

	// Rasterize the rounded clip rect to a mask.
	clipMask := image.NewRGBA(image.Rect(0, 0, bw, bh))
	childDC := r.dc.NewChildContext(clipMask)
	childDC.SetColor(color.White)
	lcx, lcy := cx-bx, cy-by
	buildRoundedRectPathOnDC(childDC, lcx, lcy, cw, ch, radii)
	childDC.Fill()

	// Zero out pixels outside the clip mask.
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			moff := y*clipMask.Stride + x*4
			a := clipMask.Pix[moff+3]
			if a == 0 {
				off := y*buf.Stride + x*4
				buf.Pix[off+0] = 0
				buf.Pix[off+1] = 0
				buf.Pix[off+2] = 0
				buf.Pix[off+3] = 0
			} else if a < 255 {
				// Partial coverage: reduce.
				off := y*buf.Stride + x*4
				buf.Pix[off+0] = uint8(uint16(buf.Pix[off+0]) * uint16(a) / 255)
				buf.Pix[off+1] = uint8(uint16(buf.Pix[off+1]) * uint16(a) / 255)
				buf.Pix[off+2] = uint8(uint16(buf.Pix[off+2]) * uint16(a) / 255)
				buf.Pix[off+3] = uint8(uint16(buf.Pix[off+3]) * uint16(a) / 255)
			}
		}
	}
}

// boxBlur applies a separable box blur (horizontal then vertical pass)
// to an RGBA image. The kernel size is (2*radius+1).
func boxBlur(img *image.RGBA, radius int) {
	if radius <= 0 {
		return
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return
	}

	stride := img.Stride

	// Temporary buffer for intermediate results.
	tmp := make([]uint8, len(img.Pix))

	// Horizontal pass: read from img.Pix, write to tmp.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rSum, gSum, bSum, aSum uint32
			count := uint32(0)
			for kx := -radius; kx <= radius; kx++ {
				sx := x + kx
				if sx < 0 || sx >= w {
					continue
				}
				off := y*stride + sx*4
				rSum += uint32(img.Pix[off+0])
				gSum += uint32(img.Pix[off+1])
				bSum += uint32(img.Pix[off+2])
				aSum += uint32(img.Pix[off+3])
				count++
			}
			off := y*stride + x*4
			tmp[off+0] = uint8(rSum / count)
			tmp[off+1] = uint8(gSum / count)
			tmp[off+2] = uint8(bSum / count)
			tmp[off+3] = uint8(aSum / count)
		}
	}

	// Vertical pass: read from tmp, write back to img.Pix.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rSum, gSum, bSum, aSum uint32
			count := uint32(0)
			for ky := -radius; ky <= radius; ky++ {
				sy := y + ky
				if sy < 0 || sy >= h {
					continue
				}
				off := sy*stride + x*4
				rSum += uint32(tmp[off+0])
				gSum += uint32(tmp[off+1])
				bSum += uint32(tmp[off+2])
				aSum += uint32(tmp[off+3])
				count++
			}
			off := y*stride + x*4
			img.Pix[off+0] = uint8(rSum / count)
			img.Pix[off+1] = uint8(gSum / count)
			img.Pix[off+2] = uint8(bSum / count)
			img.Pix[off+3] = uint8(aSum / count)
		}
	}
}
