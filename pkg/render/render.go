package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/graphics"
	"louis14/pkg/graphics/filters"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/layout/svg"
	"louis14/pkg/text"
	stdnet "louis14/std/net"
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
	// externalSVG caches external SVG documents and the filters built from
	// them for the current paint pass (beginPaintPass resets it).
	externalSVG *externalSVGCache
	scrollY     float64

	// paintingLayer is the layer being painted: url() references resolve
	// in its document (lookupSVGResources). Set on every path into a
	// layer's paint (paintLayer, paintLayerContents), restored on leaving.
	paintingLayer *PaintLayer

	// opacityLayer is the layer whose opacity paintLayerWithBackdrop's
	// layer applies: its effects paint without it (effectOpacity).
	opacityLayer *PaintLayer

	// svgImagesForContainer keeps each SVG image drawn at a container size
	// for this renderer's paint pass (svgImageForContainer).
	svgImagesForContainer map[svgImageForContainerKey]image.Image

	// Font ID cache: (path, size) → fontID
	fontCache   map[fontCacheKey]int32
	fontCacheMu sync.Mutex
	// faceFeatureSettings holds, per font ID openFont returned for a
	// registered @font-face, that face's font-feature-settings descriptor —
	// the face-level input shapeFeatures resolves against each run's
	// description. Guarded by fontCacheMu.
	faceFeatureSettings map[int32][]textshape.FontFeature
	// hasFaceFeatureSettings is len(faceFeatureSettings) > 0, read without
	// the lock so shapeFeatures' common case (no descriptor face open)
	// skips the mutex.
	hasFaceFeatureSettings atomic.Bool
	// shapeFeaturesMemo caches shapeFeatures results for the most recent
	// (font ID, description) pairs, so the per-glyph measure/draw loops
	// resolve a face's features once rather than per call — Blink resolves
	// them once into the face's FontPlatformData. Copy-on-write, read
	// without a lock.
	shapeFeaturesMemo atomic.Pointer[[]shapeFeaturesMemoEntry]

	// tempFontIDs collects fontIDs returned by openFont during the
	// current Render call. Each public Render entry point resets this
	// to empty on entry and defers a closeTempFonts that releases each
	// ID via dc.CloseTemporaryFont. Permanent-pool fontIDs (returned
	// by the OpenTemporaryFont permanent-first dedupe) close to a no-op,
	// so this list can include any fontID openFont produced.
	tempFontIDs []int32

	// Custom @counter-style rules, keyed by name.
	counterStyles map[string]css.CounterStyleRule

	// embeddedViewport, when non-zero, signals that the renderer is painting
	// inside a host DrawContext that owns more area than just the HTML viewport
	// (e.g. mancini's WebInteractor inside a larger app). In that mode the
	// base white fill and the view background (ViewPainter.backgroundRect)
	// are scoped to (0,0)-(viewport.W, viewport.H) instead of calling
	// dc.Clear(), which blanks the entire host DC.
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

	// clipTree is the clip property tree of the box tree being painted;
	// clipStack holds the clip nodes applied to r.dc, outermost first (nil
	// for a bracket whose layer has no node). Set per paintBoxes root.
	clipTree  *clipTreeBuilder
	clipStack []*ClipPaintPropertyNode

	// view paints the canvas background of the document being rendered
	// (ViewPainter, view_painter.go); nil when no root box. Set per
	// paintBoxes call.
	view *ViewPainter

	// nodeBoxIndex maps each html.Node to its primary layout box. Built once
	// per Render call in paintBoxes. Used by parentPerspective and the
	// transform tree's DOM-parent walk to
	// locate the perspective-establishing box for an abspos child whose
	// layout parent (containing block) differs from its DOM parent.
	nodeBoxIndex map[*html.Node]*layout.Box

	// transformTree builds each box's TransformPaintPropertyNode for this
	// paint; context3D is the 3D rendering context being drawn
	// (paintRenderingContext3D), nil otherwise.
	transformTree *transformTreeBuilder
	context3D     *renderingContext3D

	// unculledBelow memoizes hasUnculledDescendant per layer for one paint.
	unculledBelow map[*PaintLayer]bool

	// LOU-344: ::selection highlight painting. selectionRange carries the
	// document's current DOM Selection/Range (html.Document.Selection,
	// populated by pkg/js's Selection/Range API bindings);
	// selectionStylesheets + selectionViewportW/H are what
	// css.ComputePseudoElementStyle(node, "selection", ...) needs to
	// resolve a ::selection rule at paint time. Mirrors how pkg/layout's
	// LayoutTreeBuilder already holds b.stylesheets to resolve
	// ::marker/::first-letter/::first-line — pkg/render previously had NO
	// stylesheet/document access at all (it only consumed the pre-built
	// Box tree), so this is new plumbing rather than reuse of an existing
	// field. Set via SetSelectionContext; nil when the caller doesn't
	// supply a selection, in which case selection painting is skipped
	// entirely (same fail-open shape as r.imageFetcher == nil).
	selectionRange *html.Range
	// focusedElement and caret: the document's focused element, and the
	// caret this paint draws at a collapsed selection (frame_caret.go).
	focusedElement       *html.Node
	caret                *frameCaret
	selectionStylesheets []*css.Stylesheet
	selectionViewportW   float64
	selectionViewportH   float64

	// selectionStyleCache memoizes ComputePseudoElementStyle(node,
	// "selection", ...) per originating element for the duration of one
	// Render call — multiple text fragments commonly share the same
	// originating element (e.g. several line-wrapped runs of one <p>).
	selectionStyleCache map[*html.Node]*css.Style

	// LOU-349: ::target-text highlight painting. targetTextRanges carries
	// the document's current Text Fragment matches
	// (html.Document.TargetTextRanges, populated by window.location's
	// hash/href setters — see pkg/js/dom_location.go). Unlike
	// selectionRange above this is a SLICE: one logical `:~:text=` match
	// can already expand to multiple *html.Range (one per spanned text
	// node, see html.FindTextFragmentMatches's doc comment), and a fragment
	// can carry multiple independent `&text=` directives
	// (target-text-003.html). Set via SetTargetTextRanges; nil when the
	// caller doesn't supply any matches, in which case target-text painting
	// is skipped entirely (same fail-open shape as selectionRange == nil).
	// Reuses selectionStylesheets/selectionViewportW/H rather than adding
	// parallel fields — both are just "the document's stylesheets/viewport
	// dimensions", not selection-specific, and every caller that sets one
	// sets the other from the same document in the same call (see
	// SetTargetTextRanges's doc comment).
	targetTextRanges []*html.Range

	// targetTextStyleCache mirrors selectionStyleCache, but for
	// ComputePseudoElementStyle(node, "target-text", ...). Kept as a
	// separate map (not reused) since a single originating element can
	// have BOTH a ::selection and a ::target-text rule simultaneously (not
	// exercised by any of the 14 LOU-349 target tests, but the two pseudo
	// styles are independent values and must not collide on one cache key).
	targetTextStyleCache map[*html.Node]*css.Style

	// LOU-354: ::highlight(name) highlight painting (CSS Custom Highlight
	// API). customHighlights carries the document's registered highlights
	// (html.Document.CustomHighlights, populated by CSS.highlights.set —
	// see pkg/js/dom_highlight.go), keyed by name; customHighlightOrder is
	// the same names in first-registration order (html.Document.
	// CustomHighlightOrder), used as the bottom-to-top paint order within
	// the custom-highlight portion of the overlay stack (CSS Pseudo-4
	// §highlight-overlay-stack) when no test spreads priority values. Set
	// via SetCustomHighlights; nil/empty when the caller supplies none, in
	// which case custom-highlight painting is skipped entirely (same
	// fail-open shape as targetTextRanges == nil).
	customHighlights     map[string][]*html.Range
	customHighlightOrder []string

	// customHighlightStyleCaches mirrors targetTextStyleCache, but one
	// per-name map rather than one map total — each ::highlight(name) rule
	// is an INDEPENDENT resolved style (highlight-painting-currentcolor-
	// 001.html's ::highlight(a) and ::highlight(b) have different color/
	// background-color), so a single shared cache keyed only by originating
	// element would collide across names the way selectionStyleCache/
	// targetTextStyleCache never need to worry about (those each cover
	// exactly one fixed pseudo-element name).
	customHighlightStyleCaches map[string]map[*html.Node]*css.Style

	// highlightChainStyleCaches memoizes selection_paint.go's
	// resolveHighlightChainStyle per pseudo-element name ("selection",
	// "target-text", "highlight(<name>)") and per originating element —
	// the louis14 stand-in for Blink's per-element StyleHighlightData
	// storage (LOU-352, CSS Pseudo-4 §highlight-cascade; see
	// resolveHighlightChainStyle's doc comment). Values may be nil: an
	// explicit nil entry means "no rule for this highlight matches the
	// node or any ancestor", distinct from a missing entry (not yet
	// resolved). Kept separate from the final-style caches above because a
	// node's chain style is shared by pointer down the subtree, while the
	// final style may be a per-node UA-default bake.
	highlightChainStyleCaches map[string]map[*html.Node]*css.Style

	// selectionTextConsumed tracks, per DOM text node, how many UTF-16
	// code units of that node's text content have already been painted by
	// prior text fragments in this Render call. A single DOM text node can
	// produce multiple Box/PhysicalFragment text runs (line wraps, bidi
	// splits, justify-expansion splits — see inline_layout.go's
	// emitTextFragment) but pkg/render only sees the flattened Box tree
	// (Box.Node is the PARENT ELEMENT for text fragments, not the text
	// node itself — see fragmentToBox/emitTextFragment), so this is how
	// drawText recovers "which node-local character range does THIS
	// fragment cover" without new plumbing through pkg/layout: walk
	// box.Node's children in order, and for each text-node child consume
	// len(fragment text) code units starting at the running counter.
	selectionTextConsumed map[*html.Node]int
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
	return newRendererForContext(textshape.NewDrawContext(width, height, newProvider()))
}

// newRendererForContext wraps a fresh root draw context. The context
// anti-aliases fills, strokes and images, as a GraphicsContext does by
// default (GraphicsContextState::should_antialias_,
// platform/graphics/graphics_context_state.cc:22-30 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); child contexts inherit it.
func newRendererForContext(dc *textshape.DrawContextImpl) *Renderer {
	dc.SetAntiAlias(true)
	return NewRendererForDrawContext(dc)
}

// NewRendererForImage creates a renderer that paints onto an existing image.
func NewRendererForImage(target *image.RGBA) *Renderer {
	return newRendererForContext(textshape.NewDrawContextForImage(target, newProvider()))
}

// NewRendererForImageWithProvider creates a renderer that paints onto an
// existing image using the supplied GlyphProvider for font rasterization.
func NewRendererForImageWithProvider(target *image.RGBA, provider textshape.GlyphProvider) *Renderer {
	return newRendererForContext(textshape.NewDrawContextForImage(target, provider))
}

// NewRendererForDrawContext creates a renderer that paints using an
// existing DrawContext. The DrawContext's image, translation, and
// clipping state are used directly — no intermediate buffer is created.
// Use this when the caller already owns the drawing surface (e.g.,
// mancini interactor framework). The renderer tracks the draw context's
// matrix from here on (trackDrawContext), reading its starting matrix once
// with an affine probe: dc must not already be projective, and it panics if
// it is.
func NewRendererForDrawContext(dc textshape.DrawContext) *Renderer {
	// dc is validated before any global state changes.
	tracked := trackDrawContext(dc)
	target, ok := dc.Image().(*image.RGBA)
	if !ok {
		panic("render: the host draw context's image is not an *image.RGBA")
	}
	text.SetTextLayout(dc.TextLayout())
	return &Renderer{
		dc:        tracked,
		target:    target,
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

// SetSelectionContext supplies what drawText needs to paint the ::selection
// highlight (LOU-344): the document's current Selection/Range (nil if
// nothing is selected — selection painting is then skipped entirely) and
// the stylesheets + viewport dimensions needed to resolve a ::selection
// rule via css.ComputePseudoElementStyle, mirroring how pkg/layout's
// LayoutTreeBuilder resolves ::marker/::first-letter against the same
// b.stylesheets/b.viewportWidth/b.viewportHeight triple.
func (r *Renderer) SetSelectionContext(selection *html.Range, stylesheets []*css.Stylesheet, viewportWidth, viewportHeight float64) {
	r.selectionRange = selection
	r.selectionStylesheets = stylesheets
	r.selectionViewportW = viewportWidth
	r.selectionViewportH = viewportHeight
}

// SetTargetTextRanges supplies what drawText needs to paint the
// ::target-text highlight (LOU-349): the document's current Text Fragment
// matches (nil/empty if none — target-text painting is then skipped
// entirely). Stylesheets/viewport dimensions are NOT separate parameters
// here — callers are expected to have already called SetSelectionContext
// (or to call it alongside this, even with a nil selection) on the same
// Renderer for the same document/Render call, since both pseudo-elements
// resolve against the identical stylesheet set and viewport; duplicating
// those two parameters on this method would invite them drifting out of
// sync with selectionStylesheets/selectionViewportW/H for no benefit.
func (r *Renderer) SetTargetTextRanges(ranges []*html.Range) {
	r.targetTextRanges = ranges
}

// SetCustomHighlights supplies what drawText needs to paint ::highlight
// (name) highlights (LOU-354, CSS Custom Highlight API): the document's
// registered highlights (nil/empty if none — custom-highlight painting is
// then skipped entirely) plus their first-registration order. Stylesheets/
// viewport dimensions are NOT separate parameters here, for the same reason
// SetTargetTextRanges doesn't take them — see that method's doc comment.
func (r *Renderer) SetCustomHighlights(highlights map[string][]*html.Range, order []string) {
	r.customHighlights = highlights
	r.customHighlightOrder = order
}

// SetExternalSVGFetcher installs a closure that resolves
// `filter: url(doc#id)` references to external SVG document bytes
// (data:, file://, http(s)://, or harness-resolved relative URLs).
// Consumed by FilterEffectBuilder.lookupReferenceFilter's external
// branch. LOU-130 Phase 2 wiring.
func (r *Renderer) SetExternalSVGFetcher(fetcher func(uri string) ([]byte, error)) {
	r.externalSVGFetcher = fetcher
	r.externalSVG = nil // the next use builds a cache over the new fetcher
}

// SetScrollY sets the vertical scroll offset.
func (r *Renderer) SetScrollY(scrollY float64) {
	r.scrollY = scrollY
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
	if settings := r.fonts.Registry.FontFeatureSettingsForPath(fontPath); len(settings) > 0 {
		if r.faceFeatureSettings == nil {
			r.faceFeatureSettings = make(map[int32][]textshape.FontFeature)
		}
		r.faceFeatureSettings[metrics.FontID] = settings
		r.hasFaceFeatureSettings.Store(true)
	}
	return metrics.FontID
}

// fontFeatureInputs is the feature state of a text run's font description
// that shaping needs, each input kept apart: features is Blink's
// Font::GetFontFeatures() with the font-variant-caps overlay (PaintLayer.
// FontFeatures), settings is FontDescription::FeatureSettings() (the
// font-feature-settings property) and alternates is the
// font-variant-alternates features.
type fontFeatureInputs struct {
	features   []textshape.FontFeature
	settings   []textshape.FontFeature
	alternates []textshape.FontFeature
}

// featureInputs returns the layer's fontFeatureInputs.
func (layer *PaintLayer) featureInputs() fontFeatureInputs {
	return fontFeatureInputs{
		features:   layer.FontFeatures,
		settings:   layer.FontFeatureSettings,
		alternates: layer.FontVariantAlternates,
	}
}

// shapeFeatures is the final feature list a run shaped with fontID uses:
// the face's ResolvedFeatures, then the run's own features. Mirrors
// ShapeRange inserting current_font->PlatformData().ResolvedFeatures()
// before the font's features (platform/fonts/shaping/harfbuzz_shaper.cc:
// 327-340 @ 5992657f556bcd9d207a4aca187488462f027cab). The resolved
// features are keyed to the face that shapes the run (fontID), so a
// fallback face gets its own.
func (r *Renderer) shapeFeatures(fontID int32, in fontFeatureInputs) []textshape.FontFeature {
	hasDescriptors := r.hasFaceFeatureSettings.Load()
	if !hasDescriptors && len(in.alternates) == 0 {
		return in.features
	}
	var memo []shapeFeaturesMemoEntry
	if p := r.shapeFeaturesMemo.Load(); p != nil {
		memo = *p
	}
	for i := range memo {
		if memo[i].fontID == fontID && memo[i].in.sameAs(in) {
			return memo[i].result
		}
	}
	var descriptor []textshape.FontFeature
	if hasDescriptors {
		r.fontCacheMu.Lock()
		descriptor = r.faceFeatureSettings[fontID]
		r.fontCacheMu.Unlock()
	}
	result := in.features
	if resolved := resolveFontFeatures(descriptor, in.settings, in.alternates); len(resolved) > 0 {
		result = append(resolved, in.features...) // resolved never aliases an input
	}
	next := make([]shapeFeaturesMemoEntry, 0, shapeFeaturesMemoSize)
	next = append(next, shapeFeaturesMemoEntry{fontID: fontID, in: in, result: result})
	next = append(next, memo[:min(len(memo), shapeFeaturesMemoSize-1)]...)
	r.shapeFeaturesMemo.Store(&next)
	return result
}

// shapeFeaturesMemoSize bounds shapeFeaturesMemo: enough for a run's font
// IDs (primary, small-caps, emphasis) across the layers being painted.
const shapeFeaturesMemoSize = 8

// shapeFeaturesMemoEntry is one memoized shapeFeatures result.
type shapeFeaturesMemoEntry struct {
	fontID int32
	in     fontFeatureInputs
	result []textshape.FontFeature
}

// sameAs reports whether in and o are the same description inputs: the
// same slices (a PaintLayer's feature lists are built once and never
// modified), compared by identity.
func (in fontFeatureInputs) sameAs(o fontFeatureInputs) bool {
	return sameFeatureSlice(in.features, o.features) &&
		sameFeatureSlice(in.settings, o.settings) &&
		sameFeatureSlice(in.alternates, o.alternates)
}

func sameFeatureSlice(a, b []textshape.FontFeature) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// resolveFontFeatures is a face's ResolvedFeatures: the
// font-variant-alternates features, then the face's @font-face
// font-feature-settings descriptor features that neither the
// font-feature-settings property nor the alternates name. Mirrors
// CSSFontFace::GetFontData merging the descriptor into the face's
// FontDescription (core/css/css_font_face.cc:149-151), then
// FontDescription::MergeFontFeatureSettingsWithDescriptor and
// ResolveFontFeatures (platform/fonts/font_description.cc:549-576), whose
// result becomes the face's FontPlatformData::ResolvedFeatures
// (core/css/binary_data_font_face_source.cc:58), all @
// 5992657f556bcd9d207a4aca187488462f027cab. The result never aliases its
// inputs.
func resolveFontFeatures(descriptor, settings, alternates []textshape.FontFeature) []textshape.FontFeature {
	resolved := resolveFontFeatureSettingsDescriptor(settings, descriptor)
	if len(alternates) == 0 {
		return resolved
	}
	return append(slices.Clip(alternates), resolveFontFeatureSettingsDescriptor(alternates, resolved)...)
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
	r.faceFeatureSettings = nil
	r.hasFaceFeatureSettings.Store(false)
	r.shapeFeaturesMemo.Store(nil)
}

// Render paints the box tree onto the image. Clears the entire underlying
// DrawContext to white first; intended for the standalone case where the DC
// owns the whole image. Embedded callers (e.g. WebInteractor inside a larger
// mancini scene) should use [RenderEmbedded] instead so the surrounding DC
// content isn't blanked.
//
// Implements CSS 2.1 Appendix E paint order (simplified).
func (r *Renderer) Render(boxes []*layout.Box) {
	// The base background color behind the view background (ViewPainter::
	// PaintRootGroup, view_painter.cc:36-74 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	r.renderOnBase(boxes, color.RGBA{R: 255, G: 255, B: 255, A: 255})
}

// paintPassDoneHook, when set, runs at the end of each paint pass
// (endPaintPass). Tests set it to assert the renderer's passes left no
// Push/Pop imbalance.
var paintPassDoneHook func(*Renderer)

// endPaintPass ends a paint pass (paintPassDoneHook).
func (r *Renderer) endPaintPass() {
	if paintPassDoneHook != nil {
		paintPassDoneHook(r)
	}
}

// beginPaintPass starts a paint pass over an embedder viewport of
// viewportW×viewportH (0×0: the target is the viewport), dropping what the
// previous pass kept. Callers defer closeTempFonts.
func (r *Renderer) beginPaintPass(viewportW, viewportH float64) {
	r.embeddedViewport.W = viewportW
	r.embeddedViewport.H = viewportH
	r.svgImagesForContainer = nil
	r.tempFontIDs = r.tempFontIDs[:0]
	r.externalSVG = newExternalSVGCache(r.externalSVGFetcher)
}

// newChildRenderer returns a renderer painting into buf for an offscreen
// effect or resource buffer (a filter source, a mask, a clip), sharing the
// paint pass's external SVG cache, so a filter nested there fetches and
// builds its documents once with the rest of the paint.
func (r *Renderer) newChildRenderer(buf *image.RGBA) *Renderer {
	c := NewRendererForImage(buf)
	c.externalSVG = r.externalSVGs()
	return c
}

// externalSVGs is the paint pass's external SVG cache, made on first use
// outside a paint pass (a filter built directly).
func (r *Renderer) externalSVGs() *externalSVGCache {
	if r == nil {
		return nil
	}
	if r.externalSVG == nil {
		r.externalSVG = newExternalSVGCache(r.externalSVGFetcher)
	}
	return r.externalSVG
}

// renderOnBase paints a whole document over the frame view's base
// background color (LocalFrameView::BaseBackgroundColor): white for a
// page, transparent for an SVG image (IsolatedSVGDocumentHost,
// core/svg/graphics/isolated_svg_document_host.cc:148 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so only the document's own
// backgrounds paint there. Each call is a new paint pass.
func (r *Renderer) renderOnBase(boxes []*layout.Box, base color.RGBA) {
	defer r.endPaintPass()
	r.beginPaintPass(0, 0)
	defer r.closeTempFonts()

	if base.A != 0 {
		r.dc.SetColor(base)
		r.dc.Clear()
	}

	r.paintBoxes(boxes)
}

// RenderEmbedded paints the box tree onto the DrawContext without calling
// dc.Clear(). The white canvas background is painted with FillRectangle
// scoped to (0,0)-(viewportW,viewportH), so any clip the caller pushed is
// respected and pixels outside the viewport are left untouched. Use this
// when the renderer's DrawContext is shared with other UI components
// (e.g. mancini's WebInteractor).
//
// The viewport is also recorded so the view background
// (ViewPainter.PaintRootElementGroup) covers only that rectangle.
func (r *Renderer) RenderEmbedded(boxes []*layout.Box, viewportW, viewportH float64) {
	defer r.endPaintPass()
	r.beginPaintPass(viewportW, viewportH)
	defer r.closeTempFonts()

	r.dc.SetColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	r.dc.FillRectangle(0, 0, viewportW, viewportH)

	r.paintBoxes(boxes)
}

// buildNodeBoxIndex populates r.nodeBoxIndex with a node→first-box mapping
// by walking every box tree in boxes. For nodes that map to multiple boxes
// (e.g. inline split across lines), only the first box encountered is stored;
// for perspective lookup this is sufficient since we only need the style and
// position of the container.
func (r *Renderer) buildNodeBoxIndex(boxes []*layout.Box) {
	r.nodeBoxIndex = make(map[*html.Node]*layout.Box)
	var walk func(*layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		if b.Node != nil {
			if _, exists := r.nodeBoxIndex[b.Node]; !exists {
				r.nodeBoxIndex[b.Node] = b
			}
		}
		for _, child := range b.Children {
			walk(child)
		}
	}
	for _, b := range boxes {
		walk(b)
	}
}

// paintBoxes performs the per-box paint pass shared by [Render] and
// [RenderEmbedded]. The base background fill (white, or none for an SVG image) is the caller's responsibility; the
// view background is painted from the root layer (paintViewBackground).
func (r *Renderer) paintBoxes(boxes []*layout.Box) {
	// LOU-344: reset the per-render ::selection bookkeeping. Both maps are
	// populated lazily by drawText, so a fresh nil map here is enough —
	// avoids stale per-node consumed-offset counts or cached pseudo-styles
	// leaking from a prior Render call on the same *Renderer (the test
	// harness's two-pass JS-mutation re-layout in RenderHTMLToFileWithBase
	// constructs a fresh Renderer per pass, so this mainly guards reuse by
	// other callers, e.g. mancini's WebInteractor).
	r.selectionStyleCache = nil
	r.selectionTextConsumed = nil
	// LOU-349: same reset for ::target-text's parallel per-render
	// bookkeeping. targetTextStyleCache is its own map (see that field's
	// doc comment), but findOriginatingTextNode's consumed-offset tracking
	// (selectionTextConsumed) is intentionally SHARED with ::selection —
	// both pseudo-elements resolve the same fragment's originating text
	// node via the same per-box call in drawText, so a second independent
	// counter would double-consume and desync node-local offsets.
	r.targetTextStyleCache = nil
	// LOU-354: same reset for ::highlight(name)'s per-render style cache.
	r.customHighlightStyleCaches = nil
	// LOU-352: same reset for the highlight-cascade chain caches (see that
	// field's doc comment).
	r.highlightChainStyleCaches = nil

	// Build the node→box index used by parentPerspectiveContext to resolve
	// the perspective-establishing DOM parent for out-of-flow (abspos) children.
	r.buildNodeBoxIndex(boxes)
	r.updateFrameCaret()
	r.transformTree = newTransformTreeBuilder(r.parentPerspective, r.domParentBox)
	r.context3D = nil
	r.unculledBelow = make(map[*PaintLayer]bool)
	// Phase 6: detect reference cycles in each document's id map.
	// Cycle-flagged resources (a self-referential `<mask>`, two masks
	// pointing at each other, etc.) are treated as `none` at paint time so
	// the painters don't infinite-loop. Mirrors Blink's
	// SVGResourcesCycleSolver pass that runs before any paint dispatch.
	solveSVGResourceCycles(boxes)
	// Paint via PaintLayer tree (CSS 2.1 Appendix E stacking order). The
	// view paints the canvas background from inside the root layer's paint
	// (paintViewBackground); the root and a propagating body skip their own
	// (backgroundTransfersToView).
	for i, box := range boxes {
		layer := BuildPaintTree(box)
		if i == 0 {
			r.view = newViewPainter(r, box, layer)
		}
		r.clipTree = newClipTreeBuilder(layer)
		r.paintLayer(layer)
	}
}

// IsViewportDefiningBody reports whether the given box is the BODY element
// whose `overflow` property propagates to the viewport per CSS Overflow 3 §3.3
// ("Overflow viewport propagation").
//
// Mirrors Blink's `Document::ViewportDefiningElement()` at chromium
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
// (third_party/blink/renderer/core/dom/document.h). Blink consults this lazily
// at consumer sites (`LayoutView::CalculateScrollbarModes`,
// `ViewPainter::PaintRootElementGroup`); louis14 mirrors that pattern by
// querying it at paint-layer construction.
//
// The body's overflow propagates iff:
//  1. The box's parent is the root element (i.e. <html>).
//  2. The root element's overflow is `visible` on both axes (else root keeps
//     its own overflow on the viewport per spec rule 1).
//  3. Neither root nor body has any form of CSS containment (per CSS Contain
//     1/2 §3 — containment disables canvas-machinery propagation).
//  4. The box's DOM node is the FIRST body element in the root's DOM-children
//     (per HTML spec's `Document.body`). The first DOM body is the ONLY
//     candidate — if that body has `display: none` and therefore generates no
//     box, propagation is disabled entirely (the second body does NOT inherit
//     candidacy). Mirrors Blink's `Document::body()` → `ViewportDefiningElement`
//     fallback path.
//
// When this returns true, callers should treat the body's own `overflow` as
// `visible` (no body-local clip) — the actual overflow is owned by the
// viewport instead.
func IsViewportDefiningBody(box *layout.Box) bool {
	if box == nil || box.Node == nil || box.Node.TagName != "body" {
		return false
	}
	parent := box.Parent
	if parent == nil || parent.Node == nil || parent.Node.TagName != "html" {
		return false
	}
	// Spec rule 1: root must have overflow:visible on both axes for body to
	// be the viewport-defining element. If root has non-visible overflow, the
	// root is itself the viewport-defining element.
	if parent.Style != nil {
		if parent.Style.GetOverflowX() != css.OverflowVisible ||
			parent.Style.GetOverflowY() != css.OverflowVisible {
			return false
		}
	}
	// CSS Contain 1/2 §3: containment on root or body suppresses propagation.
	if layout.ShouldStopBodyPropagation(parent.LayoutNode) || layout.ShouldStopBodyPropagation(box.LayoutNode) {
		return false
	}
	// Per HTML spec, `Document.body` is the FIRST body element in DOM order
	// among the root's children — regardless of display, so a display:none
	// first body (no layout object) disables propagation entirely.
	return box.LayoutNode != nil && box.LayoutNode == box.LayoutNode.FirstBodyElement()
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

// lookupSVGResources returns the id map of the painting layer's document
// (LayoutInputNode.TreeScopeSVGResources on its document element, the
// nearest one up its box's ancestors): url() references resolve per
// document, as in Blink's TreeScope. nil outside a layer's paint or in a
// document without SVG. A layer without a box takes the view's document.
func (r *Renderer) lookupSVGResources() *svg.SVGResourceRegistry {
	if r == nil {
		return nil
	}
	var box *layout.Box
	switch {
	case r.paintingLayer != nil && r.paintingLayer.Box != nil:
		box = r.paintingLayer.Box
	case r.view != nil && r.view.rootLayer != nil:
		box = r.view.rootLayer.Box
	}
	for ; box != nil; box = box.Parent {
		if box.LayoutNode != nil && box.LayoutNode.IsDocumentElement() {
			return box.LayoutNode.TreeScopeSVGResources
		}
	}
	return nil
}

// solveSVGResourceCycles flags the reference cycles in the id map of every
// document under boxes, nested ones included.
func solveSVGResourceCycles(boxes []*layout.Box) {
	for _, b := range boxes {
		if b.LayoutNode != nil && b.LayoutNode.TreeScopeSVGResources != nil {
			svg.SolveResourceCycles(b.LayoutNode.TreeScopeSVGResources)
		}
		solveSVGResourceCycles(b.Children)
	}
}

// paintLayer paints a PaintLayer and its descendants using the pre-built
// stacking order.
//
// 3D rendering contexts. The root of a context draws the context's layers
// itself, depth-sorted (paintRenderingContext3D); such a layer reached in
// paint order is skipped, except while the root draws it.
func (r *Renderer) paintLayer(layer *PaintLayer) {
	if layer == nil {
		return
	}
	outerLayer := r.paintingLayer
	r.paintingLayer = layer
	defer func() { r.paintingLayer = outerLayer }()
	if c := r.context3D; c != nil && c.units[layer] {
		if layer == c.drawing.layer {
			r.paintLayerContents(layer)
		}
		return
	}
	if r.transformTree != nil && layer.Box != nil {
		if node := r.transformTree.node(layer.Box); node != nil && node.RenderingContextRoot == layer.Box {
			r.paintRenderingContext3D(layer, node)
			return
		}
	}
	r.paintLayerContents(layer)
}

// paintLayerContents paints one layer at its place in the stacking order.
// All paint decisions use pre-computed fields — no Style access.
func (r *Renderer) paintLayerContents(layer *PaintLayer) {
	if layer.HiddenFrameDocument {
		return
	}
	outerLayer := r.paintingLayer
	r.paintingLayer = layer
	defer func() { r.paintingLayer = outerLayer }()
	if outer, ok := r.enterFrameView(layer); ok {
		defer func() { r.view = outer }()
	}
	defer r.exitClipNodes(r.enterLocalBorderBoxClip(layer))
	// visibility:hidden suppresses only the layer's own content
	// (paintLayerContent); its effects, child layers and visible descendants
	// still paint (LOU-725). A hidden filtered element's filter thus still
	// evaluates against its visible descendants, or a transparent input
	// (Filter Effects 1 §3.3).
	if layer.Opacity <= 0 {
		return
	}
	// CSS Transforms L2 §11: `backface-visibility: hidden`.
	if r.skipsBackfaceCulledSubtree(layer) {
		return
	}
	if r.paintsBackdrop(layer) {
		r.paintLayerWithBackdrop(layer)
		return
	}
	r.paintLayerEffects(layer, r.paintLayerWithFilter)
}

// paintLayerEffects paints layer through its own effects: mask, filter
// (paintFilter), blending, isolation, transform and opacity.
func (r *Renderer) paintLayerEffects(layer *PaintLayer, paintFilter func(*PaintLayer)) {
	// CSS mask-image: render subtree to offscreen buffer, apply mask, composite.
	// A hidden box paints no mask image, so nothing masks its visible
	// descendants: PaintLayerPainter paints the kMask phase only for a layer
	// with visible content (paint_layer_painter.cc:525-531) and
	// BoxFragmentPainter::PaintObject paints the mask only for a visible box
	// (box_fragment_painter.cc:876 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if layer.HasMaskImage && layer.Visible {
		r.paintLayerWithMask(layer)
		return
	}

	// CSS Filters, inside the layer's effect node (opacity, mix-blend-mode).
	if layer.HasFilter {
		r.paintLayerWithEffect(layer, paintFilter)
		return
	}

	// CSS mix-blend-mode: the effect node over the layer's transformed content.
	if hasBlendMode(layer) {
		r.paintLayerWithEffect(layer, r.paintLayerContentDirectNoOpacityGroup)
		return
	}

	// CSS Compositing 1 §8: a stacking context that contains a blended
	// descendant must be rendered as an isolated group — into an offscreen
	// buffer initialised to transparent black — before being alpha-
	// composited onto the canvas. Inside the isolation buffer the blended
	// descendant sees the parent group's own painted content (and only
	// that) as its backdrop, so overflow past the parent's painted area
	// blends against transparent (which per Compositing 1 §10.3 reduces to
	// the source colour) rather than against the canvas underneath.
	// Mirrors Blink's PaintLayer isolation promotion at paint_layer.cc @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if layer.HasBlendingDescendant {
		r.paintLayerIsolated(layer)
		return
	}

	// CSS Filter Effects 2 §3.5: a Backdrop Root that contains a descendant
	// with backdrop-filter must render its subtree into an isolated buffer
	// so the descendant's backdrop sample is bounded by the backdrop-root
	// rather than reaching out to the canvas underneath. Without this,
	// applyBackdropFilter samples r.target — which holds the full canvas
	// including ancestor content painted before this layer — and the
	// descendant filter is computed against the wrong backdrop. The
	// isolation buffer pre-fills to transparent black, so a descendant's
	// backdrop-filter that samples outside the layer's own painted area
	// sees transparent (not the ancestor canvas). Mirrors Blink's backdrop-
	// root paint-property promotion at effect_paint_property_node.cc @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if layer.IsBackdropRoot && layer.HasBackdropFilterDescendant {
		r.paintLayerIsolated(layer)
		return
	}

	// Apply CSS transform if present (wraps around opacity handling).
	// A flattened transform that maps the box to a zero-area quad (edge-on,
	// e.g. rotateX(90deg) under the default flat transform-style) makes the
	// layer and its subtree paint nothing — Blink culls such a layer because
	// its concatenated transform is non-invertible and its visual rect is
	// empty (GeometryMapper / TransformPaintPropertyNode invertibility,
	// paint_property_tree_builder.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	// So does a layer whose own 4×4 is not invertible (scale3d(2, 2, 0); cc
	// node_and_ancestors_are_animated_or_invertible, see
	// isLayerTransformSingular). We still balance the Push/Pop and
	// transformDepth so the paint walk stays consistent.
	transformSingular := r.enterLayerTransform(layer)

	if !transformSingular {
		if r.effectOpacity(layer) < 1.0 {
			// CSS3 Color §4.2: render subtree to offscreen buffer and composite.
			r.dc.PushGroup()
			r.paintLayerContent(layer)
			r.dc.PopGroupWithAlpha(r.effectOpacity(layer))
		} else {
			r.paintLayerContent(layer)
		}
	}

	r.exitLayerTransform(layer)
}

// enterLayerTransform starts painting layer's content in its own
// transform: it first paints the view background when layer is the
// document element's (the view paints it in the root's PreTransform state,
// ViewPainter::PaintBoxDecorationBackground, view_painter.cc:183-201 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), then pushes and applies the
// layer's transform and enters its transform paint context. Reports
// whether the layer must not paint: its projection is edge-on (see
// applyTransforms) or its 4×4 is not invertible (isLayerTransformSingular).
// Pair with exitLayerTransform.
func (r *Renderer) enterLayerTransform(layer *PaintLayer) (singular bool) {
	r.paintViewBackground(layer)
	if r.appliesTransform(layer) {
		r.dc.Push()
		singular = r.applyTransforms(layer) || !r.transformTree.node(layer.Box).Matrix.IsInvertible()
	}
	if layer.HasTransformPaint {
		r.transformDepth++
	}
	return singular
}

// appliesTransform reports whether layer paints under a transform of its
// own: a transform, or — for a 3D-rendering-context participant painted
// from the context base — its projection to that base.
func (r *Renderer) appliesTransform(layer *PaintLayer) bool {
	return layer.HasTransform || r.paintsFromContextBase(layer)
}

// exitLayerTransform undoes enterLayerTransform.
func (r *Renderer) exitLayerTransform(layer *PaintLayer) {
	if layer.HasTransformPaint {
		r.transformDepth--
	}
	if r.appliesTransform(layer) {
		r.dc.Pop()
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
	// Cull edge-on or non-invertible transforms before setting up the offscreen buffer.
	if r.isLayerTransformSingular(layer) {
		return
	}
	box := layer.linesOrBorderBox()
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.X+box.Width)) - bx + 1
	bh := int(math.Ceil(box.Y+box.Height)) - by + 1

	if !effectBufferUsable(bw, bh) {
		// Fallback: render without mask to avoid OOM.
		r.paintViewBackground(layer)
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
	r.enterLayerTransform(layer)
	if r.effectOpacity(layer) < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(r.effectOpacity(layer))
	} else {
		r.paintLayerContent(layer)
	}
	r.exitLayerTransform(layer)

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
	// an SVG `<mask>` element in the document's SVG resource
	// registry, rasterize that mask's children into an alpha buffer and
	// apply with kDstIn directly. Mirrors Blink's
	// SVGMaskPainter::PaintSVGMaskForCSS dispatch (when the mask
	// reference resolves to an SVGMaskElement, take the SVG path; else
	// fall through to the CSS Images-3 image-or-gradient path). This
	// also implements the css-masking Phase 3 promise the foundation
	// claims (SVG `<mask>` resolution: mask-opacity-1d gate).
	//
	// Note: per CSS Masking 1 §3.7 the rendering order is filter →
	// mask → opacity → composite. The buffer above already had opacity
	// applied through PushGroup/PopGroupWithAlpha, so this branch repaints
	// the layer without it, masks that, then applies opacity in the
	// composite.
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
				r.enterLayerTransform(layer)
				r.paintLayerContent(layer)
				r.exitLayerTransform(layer)
				r.dc = saveDC
				r.target = saveTarget

				refBox := boxRectF(box)
				maskBuf := r.rasterizeSVGMask(masker, refBox, bw, bh, bx, by)
				if maskBuf != nil {
					applySVGMaskToBuffer(freshBuf, maskBuf, masker.MaskType)
					// CSS Masking 1 §3.7 rendering order: filter → mask →
					// opacity → composite.
					compositeBufferWithOpacityOnto(r.target, freshBuf, bx, by, r.effectOpacity(layer))
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
			box.X-float64(bx), box.Y-float64(by), box.Width, box.Height, box.Style)
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

// layerFilter is a filtered layer's filter graph, its region set to the
// buffer the layer's content paints into, and the CTM split it was built
// for (prepareLayerFilter).
type layerFilter struct {
	filter  *filters.Filter
	mapping filters.Mapping
	// own is the layer's own projection, dm the draw context's device
	// matrix (dmAffine false when it is projective).
	own, dm  geometry.Transform
	dmAffine bool
}

// prepareLayerFilter builds layer's filter graph and sizes its buffer. paints
// is false when the layer paints nothing (its transform is singular or its
// CTM does not decompose); a nil layerFilter that paints falls back to
// unfiltered paint: no supported filter functions, or a buffer too large.
func (r *Renderer) prepareLayerFilter(layer *PaintLayer) (lf *layerFilter, paints bool) {
	// Cull edge-on or non-invertible transforms before setting up the offscreen buffer.
	if r.isLayerTransformSingular(layer) {
		return nil, false
	}
	box := layer.linesOrBorderBox()

	// The SaveLayer's CTM: the draw context's matrix (the ancestors') ∘ the
	// layer's own projection. Under a projective draw context the CTM is
	// taken relative to it, and the remainder is still drawn through it.
	own := r.ownLayerProjection(layer)
	dm, dmAffine := drawContextAffine(r.dc)
	ctm := own
	if dmAffine {
		ctm = dm.Mul(own)
	}

	// Build the filter graph; SVG `filter: url(#id)` references resolve
	// through the renderer's resource registry (lookupReferenceFilter).
	builder := r.newFilterEffectBuilder(layer)
	// The reference box: the border box, or, for a filter that moves pixels,
	// the bounding box of the layer and its self-painting descendants
	// (PaintLayer::UpdateFilterReferenceBox, core/paint/paint_layer.cc:
	// 2021-2044 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	paintBounds := r.localBoundingBoxIncludingSelfPaintingDescendants(layer).ToRectF()
	borderBox := boxRectF(box)
	referenceBox := borderBox
	if layer.hasFilterThatMovesPixels() {
		referenceBox = paintBounds
	}
	builder.ReferenceBox = referenceBox
	builder.FilterViewport = rectFFromRect(filters.SnapRectOut(borderBox))
	// SourceGraphic extent: the layer's paint bounds, which can extend beyond
	// the border box (positioned or overflowing descendants). For drop-shadow
	// the shadow is cast from the whole painted shape, so the source buffer
	// must cover it (drop-shadow-clipped).
	paintExtent := filters.SnapRectOut(paintBounds)
	// The graph runs in layer pixels: the builder maps its parameter-space
	// boxes and parameters into the layer; the source extent is mapped here.
	stages := builder.resolveStages(layer.Filters)
	filter, mapping, ok := r.layerSpaceFilter(ctm, builder, stages, func(m filters.Mapping) bool {
		sub, ok := m.ParamToLayerPixels(paintBounds)
		paintExtent = sub
		return ok
	})
	if !ok {
		return nil, false
	}
	if filter == nil {
		// No supported CSS filter functions (e.g. only url() references,
		// not yet handled) — paint without filtering rather than dropping
		// the element.
		return nil, true
	}

	// The buffer. A reference filter keeps its SVG region and a chain with a
	// url() stage the region its builder gave it; an empty SVG region falls
	// back to the source extent mapped through the filter, as before (exact
	// sizing for these is LOU-704). A shorthand-only chain is not cropped
	// (filters.Filter.EnsureRegion): its buffer holds the content and what
	// the chain draws from it, clipped to the device clip as Skia clips a
	// layer's bounds (get_layer_mapping_and_bounds, src/core/SkCanvas.cpp:
	// 570-600 @ skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e) and grown by
	// the filter's reach (filterLayerClip). Without a clip (a projective ancestor, or the
	// layer's own perspective) the chain keeps the region it had before.
	refPixels := builder.LayerReferenceBox()
	switch {
	case hasReferenceStage(stages):
		filter.EnsureRegion(refPixels.Union(paintExtent), false)
	case dmAffine:
		if clip, ok := r.filterLayerClip(filter, mapping); ok {
			filter.EnsureRegion(paintExtent, true)
			filter.FilterRegion = filter.FilterRegion.Intersect(clip)
		}
	}
	filter.EnsureRegion(refPixels, false)
	if region := filter.FilterRegion; region.Empty() || region.Dx() > 8000 || region.Dy() > 8000 {
		// Fallback: render without filters to avoid OOM.
		return nil, true
	}
	return &layerFilter{filter: filter, mapping: mapping, own: own, dm: dm, dmAffine: dmAffine}, true
}

// paintLayerWithFilter paints layer through its filter node: the subtree is
// recorded into a layer as the graph's SourceGraphic, the graph runs over it,
// and the output is drawn into the canvas.
//
// The filter node's local transform space is the layer's own transform
// (FragmentPaintPropertyTreeBuilder::UpdateFilter sets local_transform_space
// to context_.current.transform, paint_property_tree_builder.cc:2453 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and ConversionContext::
// StartEffect switches to it before pushing the filter's SaveLayerOp
// (paint_chunks_to_cc_layer.cc:690-731 @ a9f50e52). Skia then splits that
// CTM into the layer the graph runs in and the remainder the layer is drawn
// with (get_layer_mapping_and_bounds, src/core/SkCanvas.cpp:570-600; skif::
// Mapping::decomposeCTM, src/core/SkImageFilterTypes.cpp:259-298 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e — see filters.Mapping). Opacity
// and blending belong to the parent effect node (paintLayerWithEffect).
func (r *Renderer) paintLayerWithFilter(layer *PaintLayer) {
	lf, paints := r.prepareLayerFilter(layer)
	r.paintPreparedFilter(layer, lf, paints)
}

// paintPreparedFilter is paintLayerWithFilter with the filter prepared
// (prepareLayerFilter).
func (r *Renderer) paintPreparedFilter(layer *PaintLayer, lf *layerFilter, paints bool) {
	switch {
	case !paints:
		return
	case lf == nil:
		// No filter to run, or a buffer too large: the content paints
		// unfiltered, and so does its backdrop (paintLayerWithBackdrop).
		r.paintLayerContentDirectNoOpacityGroup(layer)
		return
	}
	filter, mapping, own, dm, dmAffine := lf.filter, lf.mapping, lf.own, lf.dm, lf.dmAffine
	paramToLayer := mapping.ParamToLayer
	region := filter.FilterRegion
	bx, by := region.Min.X, region.Min.Y
	bw, bh := region.Dx(), region.Dy()

	// Render the layer subtree into the source buffer (filter-region sized).
	// Filter Effects 1 §3.3: a `visibility: hidden` element paints none of
	// its own content into the source, but its visible descendants do
	// (paintLayerContent), and the filter graph still evaluates.
	// The layer's own clips are its filter's output clip when the filter
	// moves pixels: the descendants then paint under the
	// PixelMovingFilterClipExpander, not under those clips. A filter that does
	// not move pixels has no expander, so its source is clipped as usual, as
	// is a layer whose own homography is singular, which has no local space
	// to clip the output in.
	ownH := homographyTransform(own)
	clipsOnOutput := layer.needsPixelMovingFilterClipExpander() &&
		(layer.HasCSSClip || layer.ClipPath != nil) && ownH.IsInvertible()
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	origDC := r.dc
	origTarget := r.target
	r.dc = origDC.NewChildContext(buf)
	r.target = buf
	// Layer pixel (bx, by) is buffer pixel (0, 0); the content paints in
	// its local space mapped into the layer.
	r.dc.Translate(float64(-bx), float64(-by))
	multiplyDCHomography(r.dc, projectiveHomography(paramToLayer))
	// Ancestors' clips are not on the offscreen draw context, but they stay
	// marked applied on the shared clip stack (LOU-700 ports per-chunk
	// clip states).
	r.paintFilterSourceGraphic(layer, clipsOnOutput)
	r.dc = origDC
	r.target = origTarget

	// Run the FilterEffect graph and composite the output back at the
	// filter region origin.
	filter.SetSourceImage(buf)
	out := filter.ApplyToSRGB()
	if out == nil {
		// The graph and its region are set (prepareLayerFilter), so only an
		// effect producing nothing gets here: the filtered output is empty.
		return
	}

	// Draw the layer through the remainder (fLayerToDevMatrix) on top of the
	// draw context's matrix: own ∘ paramToLayer⁻¹, which is dm⁻¹ when the
	// layer is device space. With the layer's own clips, the filter's output
	// clip, it is split as own ∘ fromLocal so they apply in between, in its
	// local space.
	var onDC geometry.Transform
	switch {
	case !mapping.LayerToDev.IsIdentity():
		onDC = own.Mul(paramToLayer.InverseOrIdentity())
	case dmAffine:
		onDC = dm.InverseOrIdentity()
	}
	if !clipsOnOutput {
		// The own clips, if any, clipped the source.
		r.dc.Push()
		multiplyDCHomography(r.dc, projectiveHomography(onDC))
		r.dc.DrawImage(out, bx, by)
		r.dc.Pop()
		return
	}
	// The split is taken on the z=0 homographies the draw context uses, so
	// own ∘ fromLocal is exactly onDC there even when own is 3D.
	fromLocal := ownH.InverseOrIdentity().Mul(homographyTransform(onDC))
	r.inOwnSpaceAndClips(layer, own, func() {
		multiplyDCHomography(r.dc, projectiveHomography(fromLocal))
		r.dc.DrawImage(out, bx, by)
	})
}

// inOwnSpaceAndClips runs paint with the layer's own projection on the draw
// context and its own clips (CSS clip, clip-path) applied.
func (r *Renderer) inOwnSpaceAndClips(layer *PaintLayer, own geometry.Transform, paint func()) {
	r.dc.Push()
	multiplyDCHomography(r.dc, projectiveHomography(own))
	r.pushOwnClips(layer)
	paint()
	r.popOwnClips(layer)
	r.dc.Pop()
}

// layerSpaceFilter builds builder's graph of stages in the layer space ctm
// decomposes into for it (skif::Mapping::decomposeCTM, src/core/
// SkImageFilterTypes.cpp:259-298 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e), once: the capability is read
// from the stages (filterStagesCapability), the CTM decomposed, and the
// graph built with builder.Mapping set to the result. An integer
// translation is folded into LayerToDev (FoldIntegerTranslation). inLayer,
// when non-nil, maps the caller's own boxes into the layer before the build
// and reports false when it cannot. ok is false when the CTM does not
// decompose or inLayer fails; filter is nil, with ok, when no stage builds
// anything.
func (r *Renderer) layerSpaceFilter(ctm geometry.Transform, builder *FilterEffectBuilder, stages []filterStage, inLayer func(filters.Mapping) bool) (filter *filters.Filter, mapping filters.Mapping, ok bool) {
	capability, builds := filterStagesCapability(stages)
	if !builds {
		return nil, mapping, true
	}
	if !mapping.DecomposeCTM(ctm, capability, r.filterDecompositionCenter(ctm)) {
		return nil, mapping, false
	}
	mapping.FoldIntegerTranslation()
	builder.Mapping = mapping
	if inLayer != nil && !inLayer(mapping) {
		return nil, mapping, false
	}
	return builder.buildStages(stages), mapping, true
}

// filterLayerClip returns the draw context's clip in the filter layer's pixels,
// grown by the filter's reach so pixels just outside it still feed the
// filtered pixels inside: the layer bounds Skia keeps, the device clip mapped
// into the layer (fLayerToDevMatrix inverse) and expanded by the filter's
// required input (get_layer_mapping_and_bounds, src/core/SkCanvas.cpp:570-600
// @ skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). louis14 has no reverse
// MapRect (LOU-704), so the reach is the forward MapRect's largest outset per
// axis; it serves shorthand-only chains, whose primitives all report their
// reach that way.
func (r *Renderer) filterLayerClip(filter *filters.Filter, mapping filters.Mapping) (image.Rectangle, bool) {
	c := r.dc.ClipBounds()
	var inv geometry.Transform
	if !mapping.LayerToDev.GetInverse(&inv) {
		return image.Rectangle{}, false
	}
	layerClip, ok := filters.MapRectAsM33(inv, rectFFromRect(c))
	if !ok {
		return image.Rectangle{}, false
	}
	// No reach to measure leaves the region unclipped.
	return outsetByReach(filter, filters.SnapRectOut(layerClip))
}

// outsetByReach outsets rect by the filter's reach: on each axis, the larger
// distance its forward bounds mapping (Filter.MapRect) moves either edge, so
// the result covers both what the filter draws from rect and what it reads
// for rect. ok is false when the mapping is empty.
func outsetByReach(filter *filters.Filter, rect image.Rectangle) (image.Rectangle, bool) {
	m := filter.MapRect(rect)
	if m.Empty() {
		return image.Rectangle{}, false
	}
	padX := max(rect.Min.X-m.Min.X, m.Max.X-rect.Max.X, 0)
	padY := max(rect.Min.Y-m.Min.Y, m.Max.Y-rect.Max.Y, 0)
	return image.Rect(rect.Min.X-padX, rect.Min.Y-padY, rect.Max.X+padX, rect.Max.Y+padY), true
}

// maxEffectBufferDimension caps an offscreen effect buffer's width and
// height, in device pixels; a larger one paints without the effect.
const maxEffectBufferDimension = 4000

// effectBufferUsable reports whether an offscreen effect buffer w×h is
// non-empty and within maxEffectBufferDimension.
func effectBufferUsable(w, h int) bool {
	return w > 0 && h > 0 && w <= maxEffectBufferDimension && h <= maxEffectBufferDimension
}

// homographyTransform returns m's z=0 homography (projectiveHomography) as a
// transform with no z terms, so products and inverses of it match those of the
// homographies the draw context composes.
func homographyTransform(m geometry.Transform) geometry.Transform {
	return transformFromHomography(projectiveHomography(m))
}

// transformFromHomography is a 3×3 row-major z=0 homography as a transform
// with no z terms (projectiveHomography's inverse).
func transformFromHomography(h [9]float64) geometry.Transform {
	return geometry.TransformRowMajor(
		h[0], h[1], 0, h[2],
		h[3], h[4], 0, h[5],
		0, 0, 1, 0,
		h[6], h[7], 0, h[8])
}

// newFilterEffectBuilder returns a FilterEffectBuilder for layer's filters,
// with the renderer's resources and fetchers; the caller sets the boxes.
func (r *Renderer) newFilterEffectBuilder(layer *PaintLayer) *FilterEffectBuilder {
	return &FilterEffectBuilder{
		CurrentColor: layer.TextColor,
		Resources:    r.lookupSVGResources(),
		ExternalSVG:  r.externalSVGs(),
		ImageFetcher: r.imageFetcher,
	}
}

// ownLayerProjection is the transform applyTransforms paints layer's content
// with on top of the draw context: its projection (layerProjection), or the
// identity when it paints under no transform of its own.
func (r *Renderer) ownLayerProjection(layer *PaintLayer) geometry.Transform {
	if !r.appliesTransform(layer) || r.transformTree == nil {
		return geometry.Transform{}
	}
	node := r.transformTree.node(layer.Box)
	if node == nil {
		return geometry.Transform{}
	}
	return r.layerProjection(layer, node)
}

// paintFilterSourceGraphic paints layer's content as its filter's
// SourceGraphic: in the layer's local space, which the caller has already
// mapped into the filter layer. With clipsOnOutput the layer's own clips are
// left to the filter output (UpdateCssClip and UpdateClipPathClip run before
// UpdateFilter, whose output_clip is the clip they leave,
// core/paint/paint_property_tree_builder.cc:4079-4082, 2485 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and the descendants paint under
// the PixelMovingFilterClipExpander (:2512-2532).
func (r *Renderer) paintFilterSourceGraphic(layer *PaintLayer, clipsOnOutput bool) {
	// The canvas background is in the root's effect: the view paints it in
	// the root's LocalBorderBoxProperties with its PreTransform
	// (ViewPainter::PaintBoxDecorationBackground, view_painter.cc:183-188 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the root's filter
	// applies to it (compositing/root-element-filter).
	r.paintViewBackground(layer)
	if layer.HasTransformPaint {
		r.transformDepth++
	}
	if clipsOnOutput {
		expander := r.pushPixelMovingFilterClipExpander(layer)
		r.paintLayerContentWithoutOwnClips(layer)
		if expander {
			r.popClipNode()
		}
	} else {
		r.paintLayerContent(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth--
	}
}

// filterDecompositionCenter is the local point a perspective CTM's layer
// scale is measured at: the centre of the device clip mapped back through the
// CTM. Mirrors compute_decomposition_center without content bounds
// (src/core/SkCanvas.cpp:531-546 @ skia d19e557a).
func (r *Renderer) filterDecompositionCenter(ctm geometry.Transform) geometry.PointF {
	c := r.dc.ClipBounds()
	v := [4]float64{float64(c.Min.X+c.Max.X) / 2, float64(c.Min.Y+c.Max.Y) / 2, 0, 1}
	ctm.InverseOrIdentity().TransformVector4(&v)
	return geometry.PointF{X: v[0] / v[3], Y: v[1] / v[3]}
}

// backdropFilterOperations returns the operations the layer's backdrop is
// filtered with: its backdrop-filter followed by its filter, which Blink
// appends because the backdrop's effect node comes before the filter's
// (PaintLayer::UpdateBackdropFilters, paint_layer.cc:2601-2614 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (layer *PaintLayer) backdropFilterOperations() []css.FilterFunction {
	return append(slices.Clip(layer.BackdropFilters), layer.Filters...)
}

// applyBackdropFilter draws layer's filtered backdrop (CSS Filter Effects 2
// backdrop-filter): it reads the backdrop under the element's border box
// from r.target, runs ops over it, clips the result to the border box,
// honouring border-radius, and draws it through r.dc. ops is the
// backdrop-filter's, followed by the element's filter's when its content is
// filtered (backdropFilterOperations).
//
// paintLayerWithBackdrop calls it in the layer's own space, under its own
// clips, before the layer's effects paint: r.target is then the canvas the
// layer composites onto, and r.dc draws into it or into the layer's effect
// layer.
func (r *Renderer) applyBackdropFilter(layer *PaintLayer, ops []css.FilterFunction) {
	box := layer.linesOrBorderBox()

	// A logically zero-area border box paints no backdrop. Skip before
	// snapping to device pixels — otherwise floor/ceil on fractional Y
	// inflates 0 logical height to 1 device row and lets the filter
	// (e.g. invert) stamp a 1px-tall strip onto the canvas.
	if box.Width <= 0 || box.Height <= 0 {
		return
	}

	// The backdrop filter lives on an effect node in the element's local
	// transform space (PropertyTreeManager::PopulateCcEffectNode,
	// property_tree_manager.cc:1346-1360 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the backdrop under the
	// transformed bounds is read from the target, filtered in that space and
	// drawn back through the transform. The CTM splits as for a filter
	// (filters.Mapping): the graph runs in layer space (ParamToLayer, the
	// CTM's scale), which LayerToDev maps onto the target. Under a projective
	// draw context the backdrop is sampled untransformed.
	ctm, _ := drawContextAffine(r.dc) // the identity when projective

	// Element's border-box region, pixel-snapped: Blink's backdrop-filter
	// bounds are the reference box's PixelSnappedContouredBorder
	// (PaintLayer::BackdropFilterBounds, paint_layer.cc:2072-2078 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), the same snapping as the
	// box's background.
	sx, sy, sw, sh := pixelSnap(box.X, box.Y, box.Width, box.Height)
	localBox := image.Rect(int(sx), int(sy), int(sx+sw), int(sy+sh))

	builder := r.newFilterEffectBuilder(layer)
	var layerBox image.Rectangle
	builder.ReferenceBox = rectFFromRect(localBox)
	builder.FilterViewport = rectFFromRect(localBox)
	filter, mapping, ok := r.layerSpaceFilter(ctm, builder, builder.resolveStages(ops), func(m filters.Mapping) bool {
		lb, ok := filters.MapRectAsM33(m.ParamToLayer, geometry.NewRectF(sx, sy, sw, sh))
		layerBox = snapRectF(lb)
		return ok
	})
	if !ok || filter == nil {
		return
	}

	// CSS Filter Effects 2 §3.4: the filter input is the backdrop content
	// within the element's filter region (border box). Samples the blur
	// kernel would pull from outside the border box are NOT canvas pixels
	// — that would import content the spec excludes from the backdrop
	// (e.g. green boxes painted before but outside the filter element).
	// Blink's compositor crops a backdrop filter's input to the layer
	// bounds with a mirror edge mode (paint_layer.cc:2612-2614 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	//
	// Our model: snapshot the backdrop for the region the chain reads in
	// layer space — the box outset by the chain's reach (outsetByReach) —
	// reflecting each sample into the border box, in local space, before
	// reading the target (sampleBackdrop). Pixels in the pad area thus
	// mirror the border box — as WPT's simulate-backdrop-blur.js reference
	// builds it.
	region, ok := outsetByReach(filter, layerBox)
	if !ok {
		region = layerBox
	}
	// Only the part that can reach the device clip is read and filtered, as
	// for a filter (filterLayerClip): a scaled-up box is not refused for its
	// layer-space size.
	if clip, ok := r.filterLayerClip(filter, mapping); ok {
		region = region.Intersect(clip)
	}
	if region.Empty() || region.Dx() > 8000 || region.Dy() > 8000 {
		return
	}
	backdrop := r.sampleBackdrop(region, localBox, mapping, ctm)

	filter.FilterRegion = region
	filter.SetSourceImage(backdrop)
	out := filter.ApplyToSRGB()
	if out == nil {
		return
	}

	// Clip the filtered backdrop to the element's border box, honouring
	// border-radius, then draw it back. The clip is the reason a
	// backdrop-filter element with rounded corners does not tint outside
	// its rounded shape. The clip is built in local space, under the CTM;
	// the backdrop is drawn in layer space.
	r.dc.Push()
	if hasBorderRadius(layer) {
		r.buildRoundedRectPath(sx, sy, sw, sh, layer.BorderRadius)
	} else {
		r.dc.DrawRectangle(sx, sy, sw, sh)
	}
	// Blink hands this shape to cc as BackdropFilterInfo::bounds
	// (PopulateBackdropFilterIfNeeded, paint_property_tree_builder.cc:
	// 4186-4212 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); viz clips the
	// filtered backdrop to it anti-aliased (viz-side, not in Blink's tree).
	clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
	if !mapping.ParamToLayer.IsIdentity() {
		multiplyDCHomography(r.dc, projectiveHomography(mapping.ParamToLayer.InverseOrIdentity()))
	}
	r.dc.DrawImage(out, region.Min.X, region.Min.Y)
	r.dc.Pop()
}

// mirrorPixelCentre mirrors the pixel-centre coordinate v into the pixel
// centres of [lo, hi): mirrorCoord, kept off the edge line by half a pixel,
// so a sample never sits on the edge. An empty range returns lo.
func mirrorPixelCentre(v, lo, hi float64) float64 {
	if hi <= lo {
		return lo
	}
	return min(max(mirrorCoord(v, lo, hi), lo+0.5), hi-0.5)
}

// mirrorPixel is the pixel of [lo, hi) the mirrored centre of pixel v lands
// on: the edge pixel repeats once (Skia's SkTileMode::kMirror).
func mirrorPixel(v, lo, hi int) int {
	return int(math.Floor(mirrorPixelCentre(float64(v)+0.5, float64(lo), float64(hi))))
}

// sampleBackdrop returns the backdrop for region, in layer space. Each layer
// pixel's centre is mapped back to local space and mirrored onto the box's
// pixel centres (mirrorPixelCentre, in local space, so a rotated box reflects
// only its own pixels), then read from the target through ctm bilinearly,
// with the taps clamped to the box's device bounds. An integer translation
// reads target pixels directly, so nothing outside the box is read; under a
// rotation or skew a tap can still reach a pixel within half a pixel of the
// box's edge, as any bilinear read of the edge does.
func (r *Renderer) sampleBackdrop(region, box image.Rectangle, mapping filters.Mapping, ctm geometry.Transform) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	tb := r.target.Bounds()
	a, b, c := ctm.Rc(0, 0), ctm.Rc(0, 1), ctm.Rc(0, 3)
	d, e, f := ctm.Rc(1, 0), ctm.Rc(1, 1), ctm.Rc(1, 3)
	if mapping.ParamToLayer.IsIdentity() && ctm.IsIdentityOrIntegerTranslation() {
		// Exact pixel centres: copy. Unlike the general path, which clamps
		// its taps to the target, a pixel off the target stays transparent.
		tx, ty := int(c), int(f)
		cols := make([]int, region.Dx())
		for x := range cols {
			cols[x] = mirrorPixel(region.Min.X+x, box.Min.X, box.Max.X) + tx
		}
		for y := 0; y < region.Dy(); y++ {
			sy := mirrorPixel(region.Min.Y+y, box.Min.Y, box.Max.Y) + ty
			if sy < tb.Min.Y || sy >= tb.Max.Y {
				continue
			}
			for x, sx := range cols {
				if sx < tb.Min.X || sx >= tb.Max.X {
					continue
				}
				copy(out.Pix[out.PixOffset(x, y):][:4], r.target.Pix[r.target.PixOffset(sx, sy):][:4])
			}
		}
		return out
	}
	toLocal := mapping.ParamToLayer.InverseOrIdentity()
	la, lb, lc := toLocal.Rc(0, 0), toLocal.Rc(0, 1), toLocal.Rc(0, 3)
	ld, le, lf := toLocal.Rc(1, 0), toLocal.Rc(1, 1), toLocal.Rc(1, 3)
	devBox, ok := filters.MapRectAsM33(ctm, rectFFromRect(box))
	if !ok {
		return out
	}
	// The pixels whose centres lie inside the box's device bounds.
	tap := snapRectF(devBox).Intersect(tb)
	if tap.Empty() {
		return out
	}
	minX, maxX := float64(box.Min.X), float64(box.Max.X)
	minY, maxY := float64(box.Min.Y), float64(box.Max.Y)
	for y := 0; y < region.Dy(); y++ {
		py := float64(region.Min.Y+y) + 0.5
		for x := 0; x < region.Dx(); x++ {
			px := float64(region.Min.X+x) + 0.5
			qx := mirrorPixelCentre(la*px+lb*py+lc, minX, maxX)
			qy := mirrorPixelCentre(ld*px+le*py+lf, minY, maxY)
			out.SetRGBA(x, y, graphics.SampleBilinearPremultiplied(r.target, a*qx+b*qy+c, d*qx+e*qy+f, tap.Min.X, tap.Min.Y, tap.Max.X-1, tap.Max.Y-1))
		}
	}
	return out
}

// paintLayerIsolated renders a stacking-context layer's full subtree into an
// offscreen buffer initialised to transparent black, then alpha-composites
// the buffer onto r.target with normal (source-over) compositing. The buffer
// is sized to cover both the layer's own box and the union of all descendant
// border boxes, so an overflowing blended descendant still paints its full
// extent inside the isolation group.
//
// This is the implementation of CSS Compositing 1 §8's "parent group" rule:
// when a layer contains a descendant with mix-blend-mode != normal, that
// descendant's blend operates against the parent group's painted content
// (the contents of this isolation buffer), not against whatever the canvas
// already holds underneath the parent group. Where the blender overflows
// past the parent's painted area, the backdrop within the buffer is
// transparent black, and Compositing 1 §10.3 reduces the blend operation to
// the source colour — exactly what reftests like
// mix-blend-mode-overflowing-child and mix-blend-mode-parent-with-border-
// radius assert.
//
// Mirrors Blink's PaintLayer isolation step (paint_layer_painter.cc /
// paint_layer.cc) at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func (r *Renderer) paintLayerIsolated(layer *PaintLayer) {
	// Cull edge-on or non-invertible transforms before setting up the offscreen buffer.
	if r.isLayerTransformSingular(layer) {
		return
	}
	box := layer.Box
	if box == nil {
		r.paintLayerContent(layer)
		return
	}

	// Buffer extent: the layer's paint bounds, so an overflowing blended
	// descendant is fully captured.
	extent := filters.SnapRectOut(r.localBoundingBoxIncludingSelfPaintingDescendants(layer).ToRectF())
	bx, by := extent.Min.X, extent.Min.Y
	bw, bh := extent.Dx(), extent.Dy()
	if bw <= 0 || bh <= 0 || bw > 8000 || bh > 8000 {
		// Buffer would be empty or unreasonably large — fall back to a
		// non-isolated paint. Better to mispaint the blend than to OOM.
		r.paintLayerContentDirect(layer)
		return
	}

	// Render the layer subtree into the buffer (transparent black init).
	// The buffer is created with world-coordinate bounds so r.target
	// pixel addressing inside the isolation matches the addressing used
	// against the outer canvas. blendComposite and other pixel-
	// level writers compute dst offsets via (dy - dstBounds.Min.Y) and
	// (dx - dstBounds.Min.X), so a zero-origin buffer would mis-locate
	// the blender's output by (-bx, -by). Mazarin's NewChildContext
	// honours arbitrary bounds; no additional Translate is required.
	buf := image.NewRGBA(image.Rect(bx, by, bx+bw, by+bh))
	origDC := r.dc
	origTarget := r.target
	childDC := origDC.NewChildContext(buf)
	r.dc = childDC
	r.target = buf

	// Inside the isolation, paint WITHOUT the PushGroup/PopGroupWithAlpha
	// opacity wrap: PushGroup swaps dc.im to a fresh buffer for the
	// group, but r.target stays pointed at `buf` — so a descendant's
	// backdrop-filter sees the empty buf instead of the layer's painted
	// content. Apply opacity by post-modulating the alpha channel of buf
	// at composite-out time, which preserves the same composited result
	// (per CSS Color §15: applying alpha to premultiplied colour values
	// before source-over composite is equivalent to PushGroup with the
	// same alpha). Mirrors Blink's PaintLayer using SkBlendMode::kSrcOver
	// with a constant-alpha layer rather than a separate group surface
	// for opacity-only stacking contexts (paint_layer_painter.cc @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	r.paintLayerContentDirectNoOpacityGroup(layer)

	// Restore original DC and alpha-composite the isolated buffer onto
	// r.target with source-over (DrawImage), modulating by the layer's
	// opacity since the inner paint skipped the PushGroup wrap.
	r.dc = origDC
	r.target = origTarget
	if r.effectOpacity(layer) < 1.0 {
		compositeBufferWithOpacityOnto(r.target, buf, 0, 0, r.effectOpacity(layer))
	} else {
		r.dc.DrawImage(buf, bx, by)
	}
}

// paintLayerContentDirectNoOpacityGroup mirrors paintLayerContentDirect but
// skips the PushGroup/PopGroupWithAlpha opacity wrap. The caller (typically
// paintLayerIsolated) applies the layer's opacity at composite-out time so
// that r.target stays pointed at the surface the layer is actually painting
// into — necessary for descendants whose backdrop-filter samples r.target.
func (r *Renderer) paintLayerContentDirectNoOpacityGroup(layer *PaintLayer) {
	r.enterLayerTransform(layer)

	r.paintLayerContent(layer)

	r.exitLayerTransform(layer)
}

// paintLayerContentDirect runs the view-background + transform + opacity
// wrapping that paintLayer normally applies before reaching
// paintLayerContent, but without the early-return branches for
// mask/filter/blend-mode/isolation. It is used by paintLayerIsolated to
// drive a layer's full paint into an offscreen buffer; the mask / filter /
// blend-mode / isolation branches do not apply when the layer itself does
// not own those effects.
func (r *Renderer) paintLayerContentDirect(layer *PaintLayer) {
	r.enterLayerTransform(layer)

	if r.effectOpacity(layer) < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(r.effectOpacity(layer))
	} else {
		r.paintLayerContent(layer)
	}

	r.exitLayerTransform(layer)
}

// hasBlendMode reports whether layer has a mix-blend-mode other than normal.
func hasBlendMode(layer *PaintLayer) bool {
	return layer.BlendMode != css.MixBlendModeNormal && layer.BlendMode != ""
}

// paintLayerWithEffect paints layer's contents (paintContents) inside its
// effect node: a layer of its own that is composited with the layer's opacity
// and mix-blend-mode. Blink's effect node for opacity and blending is the
// parent of the layer's filter node (UpdateEffect runs before UpdateFilter,
// paint_property_tree_builder.cc:4080-4082 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and ConversionContext::
// StartEffect pushes its SaveLayer[Alpha]Op with the blend mode and opacity
// separately from the filter's SaveLayerOp (paint_chunks_to_cc_layer.cc:
// 700-735 @ a9f50e52) — so both apply to the filter's output.
//
// The layer is a buffer covering the current clip, addressed like the current
// canvas and drawn under the same device matrix (Skia's saveLayer keeps the
// CTM). With neither
// effect, paintContents paints directly.
func (r *Renderer) paintLayerWithEffect(layer *PaintLayer, paintContents func(*PaintLayer)) {
	blend := hasBlendMode(layer)
	if r.effectOpacity(layer) >= 1 && !blend {
		paintContents(layer)
		return
	}
	if r.isLayerTransformSingular(layer) {
		return
	}
	buf, parent, ok := r.paintIntoEffectLayer(func() { paintContents(layer) })
	switch {
	case !ok:
		paintContents(layer)
		return
	case buf == nil:
		return
	}

	// The layer covers the clip in effect here (its bounds; a rounded clip
	// is not reapplied).
	if !blend {
		compositeBufferWithOpacityOnto(parent, buf, 0, 0, r.effectOpacity(layer))
		return
	}
	applyConstantAlphaToBuffer(buf, r.effectOpacity(layer))
	blendComposite(parent, buf, buf.Rect.Min.X, buf.Rect.Min.Y, layer.BlendMode)
}

// paintIntoEffectLayer runs paint into a layer of its own: a buffer covering
// the current clip, addressed like the current canvas and drawn under the
// same device matrix (Skia's saveLayer keeps the CTM). It returns the layer
// and the canvas to composite it onto. ok is false, and nothing paints, when
// the canvas is not a bitmap; an empty clip paints nothing and returns no
// layer.
func (r *Renderer) paintIntoEffectLayer(paint func()) (buf, parent *image.RGBA, ok bool) {
	parent, ok = r.dc.Image().(*image.RGBA)
	if !ok {
		return nil, nil, false
	}
	clip := parent.Bounds().Intersect(r.dc.ClipBounds())
	if clip.Empty() {
		return nil, parent, true
	}
	// The buffer is painted under the draw context's whole matrix, a
	// projective one included; the content's near-plane clipping happens as
	// it paints.
	dm := drawContextTransform(r.dc)
	buf = image.NewRGBA(clip)
	origDC, origTarget := r.dc, r.target
	r.dc = origDC.NewChildContext(buf)
	r.target = buf
	multiplyDCHomography(r.dc, projectiveHomography(dm))
	paint()
	r.dc, r.target = origDC, origTarget
	return buf, parent, true
}

// paintsBackdrop reports whether layer paints a backdrop-filter: its own
// content paints, and it is not a 3D-context member's content polygon.
func (r *Renderer) paintsBackdrop(layer *PaintLayer) bool {
	return layer.HasBackdropFilter && r.paintsOwnContent(layer) && r.context3D.partOf(layer) != memberContent
}

// paintLayerWithBackdrop paints a layer with a backdrop-filter. viz draws the
// element into a layer initialized with its filtered backdrop: the content
// draws into it with the element's mix-blend-mode, and the layer restores
// source-over with the element's opacity (SkiaRenderer::PrepareCanvasForRPDQ,
// components/viz/service/display/skia_renderer.cc:1736-1773 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So the backdrop is read from the
// canvas the element composites onto, before its mask, filter and blending
// paint, and is inside its opacity. With neither opacity nor blending, that
// layer is the canvas itself.
func (r *Renderer) paintLayerWithBackdrop(layer *PaintLayer) {
	if r.isLayerTransformSingular(layer) {
		return
	}
	// The element's filter ops are appended only when its content is
	// filtered (backdropFilterOperations), so a filter that falls back to
	// unfiltered paint leaves the backdrop unfiltered by it too. The filter
	// is prepared once, for both: the effect layer keeps the device matrix.
	ops := layer.BackdropFilters
	paintFilter := r.paintLayerWithFilter
	if layer.HasFilter {
		lf, paints := r.prepareLayerFilter(layer)
		if lf != nil {
			ops = layer.backdropFilterOperations()
		}
		paintFilter = func(l *PaintLayer) { r.paintPreparedFilter(l, lf, paints) }
	}
	applyBackdrop := func() {
		r.inOwnSpaceAndClips(layer, r.ownLayerProjection(layer), func() { r.applyBackdropFilter(layer, ops) })
	}
	if r.effectOpacity(layer) < 1 || hasBlendMode(layer) {
		canvas := r.target
		buf, parent, ok := r.paintIntoEffectLayer(func() {
			layerBuf := r.target
			r.target = canvas
			applyBackdrop()
			r.target = layerBuf
			// The layer takes the opacity; a blend mode blends the content
			// against the backdrop in the layer.
			outer := r.opacityLayer
			r.opacityLayer = layer
			r.paintLayerEffects(layer, paintFilter)
			r.opacityLayer = outer
		})
		if ok {
			if buf != nil {
				compositeBufferWithOpacityOnto(parent, buf, 0, 0, layer.Opacity)
			}
			return
		}
	}
	applyBackdrop()
	r.paintLayerEffects(layer, paintFilter)
}

// effectOpacity is the opacity layer's effects paint with: none when
// paintLayerWithBackdrop's layer applies it.
func (r *Renderer) effectOpacity(layer *PaintLayer) float64 {
	if layer == r.opacityLayer {
		return 1
	}
	return layer.Opacity
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

// applyTransforms applies the layer's TransformPaintPropertyNode to the draw
// context. The draw context holds the flattened projection of the space the
// node's parent paints into, so a node that flattens what it inherits
// composes its own matrix onto it (projecting the z=0 plane of a flattened
// parent commutes with the product). A node that participates in a 3D
// rendering context is painted from the context base instead (its root
// draws it, paintRenderingContext3D) and applies its whole
// projection to that base, GeometryMapper::SourceToDestinationProjection
// (projectionToRenderingContextBase), so its preserve-3d ancestors' 4×4s
// compose before anything is projected.
//
// The node matrix carries the transform-origin on the pixel-snapped border
// box (TransformPaintPropertyNode::Build adds the origin to the snapped
// border box), so the paint-time transform pivots on the pixel grid the box
// is rasterized to.
//
// Returns whether the applied projection maps the box to a zero-area quad
// (edge-on, e.g. rotateX(90deg)) while the layer flattens its children: the
// HasTransform paint scope then culls the subtree (an empty visual rect in
// Blink). A preserve-3d layer is never culled by its own projection: its
// participating descendants are painted from the context base, not under it.
func (r *Renderer) applyTransforms(layer *PaintLayer) bool {
	node := r.transformTree.node(layer.Box)
	if node == nil {
		return false
	}
	h := projectiveHomography(r.layerProjection(layer, node))
	singular := !layer.Box.Preserves3D() && isSingularProjection(h[0], h[3], h[1], h[4])
	if !multiplyDCHomography(r.dc, h) {
		return false
	}
	return singular
}

// multiplyDCHomography composes a 3×3 row-major homography onto dc: as an
// affine matrix when its bottom row is [0 0 1] (reported true), else in
// projective mode with the perspective divide.
func multiplyDCHomography(dc textshape.DrawContext, h [9]float64) (affine bool) {
	if h[6] != 0 || h[7] != 0 || h[8] != 1 {
		dc.MultiplyProjectiveMatrix(h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7], h[8])
		return false
	}
	// h = [xx xy x0 / yx yy y0 / 0 0 1]; MultiplyMatrix expects (xx,yx,xy,yy,x0,y0).
	dc.MultiplyMatrix(h[0], h[3], h[1], h[4], h[2], h[5])
	return true
}

// layerProjection is the transform applyTransforms paints layer with: its
// node matrix onto its parent's flattened projection, or — painted from its
// 3D rendering context base — its projection to that base.
func (r *Renderer) layerProjection(layer *PaintLayer, node *TransformPaintPropertyNode) geometry.Transform {
	if r.paintsFromContextBase(layer) {
		return projectionToRenderingContextBase(node)
	}
	return node.Matrix
}

// isLayerTransformSingular returns true if layer.HasTransform is set and the
// layer's 4×4 transform is not invertible, or it flattens its children while
// the projection it paints with is singular (edge-on / zero-area). Used to
// cull the layer before any paint path — including the special-effect paths
// (HasMaskImage, HasFilter, blend, isolated) that would otherwise bypass the
// transformSingular check in the main paintLayer body. A participant of a 3D
// rendering context is judged by its projection to the context base, as
// applyTransforms paints it.
func (r *Renderer) isLayerTransformSingular(layer *PaintLayer) bool {
	if !r.appliesTransform(layer) || layer.Box == nil {
		return false
	}
	node := r.transformTree.node(layer.Box)
	if node == nil {
		return false
	}
	// A layer whose own 4×4 transform is not invertible is skipped with its
	// subtree, preserve-3d or not, even when its 2D projection is invertible
	// (scale3d(2, 2, 0)). Mirrors cc's
	// TransformNode::node_and_ancestors_are_animated_or_invertible
	// (cc/trees/property_tree.cc:939-955: is_invertible is the node's
	// to_parent.IsInvertible()) and the layer skip in
	// cc/trees/draw_property_utils.cc:544-549 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b. louis14 bakes animations
	// into the style, so the has_potential_animation exemption has no
	// counterpart; descendants of a skipped layer are not painted, which
	// covers the ancestor half for layers painted inside it.
	if !node.Matrix.IsInvertible() {
		return true
	}
	if layer.Box.Preserves3D() {
		return false
	}
	h := projectiveHomography(r.layerProjection(layer, node))
	return isSingularProjection(h[0], h[3], h[1], h[4])
}

// singularProjectionEpsilon bounds the determinant magnitude below which a
// flattened 2D transform projection is treated as singular (degenerate to a
// zero-area quad). It must catch the floating-point residue a 90° axis
// rotation leaves in the projection (e.g. rotateX(90deg) yields
// yy = cos(90°) ≈ 6.12e-17, so det ≈ 6.12e-17) while sitting far below any
// determinant a legitimately-visible transform produces — the smallest
// plausible legit determinant is from an aggressive uniform scale-down whose
// det is the square of the scale factor, orders of magnitude above this bound.
const singularProjectionEpsilon = 1e-12

// isSingularProjection reports whether a flattened 2D affine projection with
// linear coefficients (xx, yx, xy, yy) maps its input to a zero-area image —
// i.e. the layer is edge-on after flattening and must paint nothing. Mirrors
// Blink's GeometryMapper / TransformPaintPropertyNode invertibility check: a
// layer whose concatenated transform produces a degenerate (non-invertible)
// mapping has an empty visual rect and is culled rather than painted
// (paint_property_tree_builder.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func isSingularProjection(xx, yx, xy, yy float64) bool {
	det := xx*yy - xy*yx
	return math.Abs(det) < singularProjectionEpsilon
}

// parentPerspective returns the perspective node matrix box's DOM parent
// applies to box: perspective(d) about the parent's perspective-origin in
// absolute coordinates (FragmentPaintPropertyTreeBuilder::UpdatePerspective,
// paint_property_tree_builder.cc:3153-3216 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; ComputedStyle::ApplyPerspective).
func (r *Renderer) parentPerspective(box *layout.Box) (geometry.Transform, bool) {
	var m geometry.Transform
	d, opx, opy, ok := r.parentPerspectiveContext(box)
	if !ok {
		return m, false
	}
	m.ApplyPerspectiveDepth(d)
	m.ApplyTransformOrigin(opx, opy, 0)
	return m, true
}

// domParentBox returns the layout box of box's DOM parent, nil when unknown.
func (r *Renderer) domParentBox(box *layout.Box) *layout.Box {
	if box == nil || box.Node == nil || box.Node.Parent == nil {
		return nil
	}
	return r.nodeBoxIndex[box.Node.Parent]
}

// parentPerspectiveContext reports whether the box's DOM parent establishes a
// `perspective` property and, if so, returns the perspective distance plus the
// screen-space perspective-origin (parent box position + resolved
// perspective-origin offset). Used to apply parent-sourced perspective to a
// child's transform in applyTransforms.
//
// Perspective applies only to the perspective element's direct DOM children
// (CSS Transforms L2 §6; csswg-drafts #918). The box tree reparents
// absolutely/fixed-positioned boxes under their containing block, so box.Parent
// may not be the DOM parent. We use r.nodeBoxIndex (built once per render) to
// look up the box for box.Node.Parent.
func (r *Renderer) parentPerspectiveContext(box *layout.Box) (d, opx, opy float64, ok bool) {
	if box == nil || box.Node == nil || box.Node.Parent == nil {
		return 0, 0, 0, false
	}
	p := r.nodeBoxIndex[box.Node.Parent]
	if p == nil || p.Style == nil {
		return 0, 0, 0, false
	}
	pd, has := p.Style.GetPerspective()
	if !has {
		return 0, 0, 0, false
	}
	psx, psy, _, _ := pixelSnap(p.X, p.Y, p.Width, p.Height)
	oox, ooy := p.Style.ResolvePerspectiveOriginPx(p.Width, p.Height)
	return pd, psx + oox, psy + ooy, true
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
	r.pushOwnClips(layer)
	expander := r.pushPixelMovingFilterClipExpander(layer)
	r.paintLayerContentWithoutOwnClips(layer)
	if expander {
		r.popClipNode()
	}
	r.popOwnClips(layer)
}

// paintLayerContentWithoutOwnClips is paintLayerContent without the layer's
// own CSS clip and clip-path: a filter's source, whose output they clip.
func (r *Renderer) paintLayerContentWithoutOwnClips(layer *PaintLayer) {
	// A backface-culled layer reaching here has unculled descendants to
	// paint; its own content is skipped (skipsBackfaceCulledSubtree). So is
	// a hidden layer's: visibility is per box, and its descendants still
	// paint (PaintLayerPainter::PaintLayerContents gates only the layer's
	// own content, paint_layer_painter.cc:362-366 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	own := r.paintsOwnContent(layer)
	// A layer that preserves 3D in a context being drawn paints its
	// background and its other content as separate polygons, around its
	// negative z-index children (paintRenderingContext3D).
	part := r.context3D.partOf(layer)

	if own && part != memberContent {
		// Self decorations: bg, borders, shadows, outline, list marker.
		r.paintSelfDecorations(layer)
	}
	if part == memberBackground {
		return
	}

	// Overflow clip using pre-computed rectangle.
	var overflowClip *ClipPaintPropertyNode
	if layer.HasClip {
		overflowClip = r.clipTree.ownClipNode(layer, clipNodeOverflowClip)
		r.pushClip(layer, clipNodeOverflowClip, overflowClip)
	}

	// Step 2: Negative z-index stacking contexts.
	for _, child := range layer.NegativeZ {
		r.paintZOrderChild(child, overflowClip)
	}

	// Step 3: Descendant block backgrounds (non-self-painting subtree).
	r.paintDescendantsPhase(layer, PhaseBackground)

	// A collapsed table paints its borders after its descendants'
	// backgrounds (box_fragment_painter.cc:941-947 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). paintLayerContent reaches
	// a self-painting table; paintDescendantPhase reaches the others.
	if own {
		r.PaintCollapsedBorders(layer)
	}

	// Step 4: Floats (each runs its own full phase loop).
	r.paintDescendantsPhase(layer, PhaseFloat)

	// Step 5a: Self foreground — text + image content. A block caret paints
	// first, under the text it covers (paintFrameCaret).
	if !r.isBackfaceCulled(layer) {
		r.paintFrameCaret(layer, true)
		r.paintSelfForeground(layer)
	}

	// Step 5b: Descendant foreground (text/image + non-positioned SCs), then
	// a bar or underscore caret over it.
	r.paintDescendantsPhase(layer, PhaseForeground)
	if !r.isBackfaceCulled(layer) {
		r.paintFrameCaret(layer, false)
	}

	// Step 5c: Descendant outlines (CSS 2.1 Appendix E step 10). Painted
	// after all in-flow foreground content so an outline overlapping its
	// element's text or background (e.g. negative outline-offset) draws on
	// top; before the z-ordered child layers, mirroring Blink's
	// PaintLayerPainter phase order (kDescendantOutlinesOnly before child
	// layers @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Descendant
	// outlines stay inside the overflow clip — a scroller clips its
	// contents' outlines.
	r.paintDescendantsPhase(layer, PhaseOutline)

	// Step 6: z-index:auto positioned + z-index:0 SCs.
	for _, child := range layer.AutoZero {
		r.paintZOrderChild(child, overflowClip)
	}

	// Step 7: Positive z-index stacking contexts.
	for _, child := range layer.PositiveZ {
		r.paintZOrderChild(child, overflowClip)
	}

	if layer.HasClip {
		r.popClipNode()
	}

	// Classic scrollbar widget (overflow controls). Painted after the
	// box's content and child layers and OUTSIDE the overflow content
	// clip — the gutter sits between the inner border edge and the
	// padding edge, which the content clip excludes. Mirrors Blink's
	// ScrollableAreaPainter::PaintOverflowControls running after the
	// foreground phase (scrollable_area_painter.cc @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	// Self outline paints OUTSIDE the element's own overflow clip — the
	// clip applies to the box's contents, never to its own outline ring
	// (Blink paints kSelfOutlineOnly without the contents clip) — but
	// still inside clip-path / CSS clip, which do clip outlines.
	if own {
		r.paintScrollbars(layer)
		r.paintSelfOutline(layer)
	}

}

// paintsOwnContent reports whether the layer paints its own decorations,
// foreground and outline: not when backface-culled, and not when hidden
// (visibility is per box; its descendants still paint).
func (r *Renderer) paintsOwnContent(layer *PaintLayer) bool {
	return layer.Visible && !r.isBackfaceCulled(layer)
}

// paintSelfOutline paints the layer's own outline. CSS 2.1 Appendix E
// step 10: outlines paint after all in-flow content of the stacking
// context, so an outline overlapping its own element's text or background
// (e.g. with a negative outline-offset) draws on top. Mirrors Blink's
// dedicated PaintPhase::kSelfOutlineOnly phase (paint_phase.h @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func (r *Renderer) paintSelfOutline(layer *PaintLayer) {
	if layer.inlinePaintGroup != nil {
		return // its fragments paint it (see paintSelfDecorations)
	}
	if layer.hasOutline() {
		r.drawOutline(layer)
	}
}

// paintSelfDecorations paints a layer's own background, borders,
// shadows, column rules, and list marker — the subset of
// painting that precedes descendant painting in CSS 2.1 Appendix E.
// The outline is NOT painted here; it has its own post-content phase
// (see paintSelfOutline). Called once per layer (not once per phase).
//
// An inline paint group's layer paints no box of its own: its box fragments
// paint the inline's background, borders, shadows and outline, as Blink's
// InlineBoxFragmentPainter::Paint does per fragment
// (inline_box_fragment_painter.cc:61-77 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); BoxFragmentPainter never paints
// an inline box (box_fragment_painter.cc:594).
func (r *Renderer) paintSelfDecorations(layer *PaintLayer) {
	if layer.inlinePaintGroup != nil {
		return
	}
	// empty-cells: hide — skip background, borders, and box shadows for
	// empty table cells in the separate border model (CSS 2.1 §17.6.1.1).
	// The cell still occupies space; only painting is suppressed.
	if layer.Box != nil && layer.Box.IsFieldsetContainer {
		r.paintFieldsetBoxDecorationBackground(layer)
	} else if !layer.EmptyCellHide {
		rect := borderBoxRect(layer.Box)
		// A table paints its background and border on its grid rect: the
		// table box without its captions (TablePainter::
		// PaintBoxDecorationBackground, table_painters.cc:421-435 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		if g := layer.Box.TableGridRect; g != nil {
			rect = rectXYWH(layer.Box.X+g.Offset.X, layer.Box.Y+g.Offset.Y, g.Size.Width, g.Size.Height)
		}
		clipped := r.clipTableCellBackground(layer, rect)
		if len(layer.BoxShadows) > 0 {
			r.drawOutsetBoxShadows(layer, rect)
		}
		r.drawBackground(layer, rect)
		// A collapsed table and its cells paint no border of their own;
		// the table paints the collapsed borders (PaintCollapsedBorders).
		if !layer.HasCollapsedBorders {
			r.drawBorders(layer, rect)
		}
		if len(layer.BoxShadows) > 0 {
			r.drawInsetBoxShadows(layer, rect)
		}
		if clipped {
			r.dc.Pop()
		}
	}

	// Gap decorations paint after the background and borders
	// (box_fragment_painter.cc:1428-1436 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	r.paintGapDecorations(layer)

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
//
// An inline <svg> walks its content even when hidden: ReplacedPainter::
// ShouldPaint lets an SVG root through (replaced_painter.cc:270-273 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), as SVG checks visibility per
// element and a filtered descendant still applies its filter. Text, image
// and canvas paint only for a visible box.
func (r *Renderer) paintSelfForeground(layer *PaintLayer) {
	if isInlineSVGLayer(layer) {
		r.paintSVGRoot(layer)
		return
	}
	if !layer.Visible {
		return
	}
	if layer.Box != nil && layer.Box.Text != "" {
		r.drawText(layer)
	}
	// An <img> / image-typed <object>/<embed> paints through
	// ImagePainter::PaintReplaced; a <video> through VideoPainter::
	// PaintReplaced (video_painter.cc:20-117 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which — with no media
	// player, so only in kPoster mode (ImageSrc set) — takes the same
	// replaced content rect and ClipsToContentBox visual rect and paints the
	// poster through ImagePainter::PaintIntoRect, i.e. drawImage.
	if layer.ImageSrc != "" {
		r.drawImage(layer)
	} else if layer.Canvas != nil {
		r.paintCanvas(layer)
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
	if child.Opacity <= 0 {
		return
	}
	// A stacking context, an atomic inline or an absolutely positioned flow
	// child (kept in DOM-order paint because an overflow clip contains it)
	// paints as a unit, like the self-painting PaintLayer Blink gives every
	// stacked (positioned) box (PaintLayer::ShouldBeSelfPaintingLayer,
	// core/paint/paint_layer.cc:2274-2276 → LayoutBox::LayerTypeRequired,
	// layout/layout_box.cc:556-563 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
	// its background must not drop to the parent's step-3 background phase
	// and paint under in-flow text that precedes it in DOM order.
	if isSelfPaintingFlowChild(child) {
		if phase == PhaseForeground {
			r.paintLayer(child)
		}
		return
	}

	// CSS Transforms L2 §11: the paintLayerContents gate, for flow children
	// that paint in their parent's phases (the branches above reach it
	// through paintLayer).
	if r.skipsBackfaceCulledSubtree(child) {
		return
	}
	// A hidden child paints its descendants but not its own decorations or
	// outline (paintSelfForeground checks visibility itself).
	culled := r.isBackfaceCulled(child)
	pureInline := isPureInlineForPaint(child)

	switch {
	case culled:
		// Backface culled: only descendants paint.
	case phase == PhaseBackground:
		if child.Visible && !pureInline {
			r.paintSelfDecorations(child)
		}
		// Pure inline defers self decorations to PhaseForeground.
	case phase == PhaseForeground:
		if child.Visible && pureInline {
			r.paintSelfDecorations(child)
		}
		r.paintSelfForeground(child)
	case phase == PhaseOutline && child.Visible:
		r.paintSelfOutline(child)
		// PhaseFloat: skip self — floats paint their own bg during their
		// paintLayer.
	}

	// Overflow clip on a non-SC descendant still applies to its own
	// descendants' phase work. Wrap the recursion in the same clip
	// setup used at the SC level, but scoped per phase so each phase
	// pass is independently clipped.
	clipping := false
	if child.HasClip {
		clipping = true
		r.pushClipNode(child, clipNodeOverflowClip)
	}

	// The caret paints inside the block's overflow clip: a block caret
	// before its contents, a bar or underscore caret after them
	// (paintFrameCaret).
	caretSlot := !culled && phase == PhaseForeground
	if caretSlot {
		r.paintFrameCaret(child, true)
	}
	r.paintDescendantsPhase(child, phase)
	if caretSlot {
		r.paintFrameCaret(child, false)
	}

	// A collapsed table's borders, after its descendants' backgrounds
	// (see paintLayerContent).
	if !culled && child.Visible && phase == PhaseBackground {
		r.PaintCollapsedBorders(child)
	}

	if clipping {
		r.popClipNode()
	}

	// Classic scrollbar widget for a non-stacking-context scroll container.
	// Painted in the foreground phase (on top of the box's content) and
	// OUTSIDE the box's overflow clip, mirroring the self-painting-layer
	// path in paintLayerContent. A box reaches exactly one of the two
	// paths, so the scrollbar paints once.
	if !culled && child.Visible && phase == PhaseForeground {
		r.paintScrollbars(child)
	}
}

// isAtomicInlineForPaint returns true for boxes that paint as a
// single atomic unit at CSS 2.1 Appendix E step 5. These include:
//   - Atomic inline-level boxes: inline-block, inline-flex,
//     inline-grid, inline-table (CSS 2.1 §10.3.7).
//   - Flex items (CSS Flexbox §13: "flex items paint exactly the
//     same as inline blocks").
//   - Grid items (CSS Grid §17 painting, same treatment).
//   - Replaced elements with non-visible overflow (CSS Overflow 4 §3.7
//     applies a UA `overflow: clip` rule to embed/iframe/img/video;
//     routing them through paintLayer is the only path that honors
//     the established clip rectangle around the replaced content,
//     mirroring Blink's LayoutReplaced::PaintReplaced @
//     4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
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
	// Replaced inline elements with non-visible overflow (e.g. UA
	// `overflow: clip` on <img>/<video>/<embed>/<iframe>) must paint
	// atomically through paintLayer so the established clip rectangle
	// constrains the replaced content paint. Without this routing the
	// content paints through the parent's inline phase with no clip,
	// and a sub-pixel content size that rounds UP can spill past the
	// content-box into the border ring — diverging from a sibling flex
	// or inline-block path that DOES route through paintLayer.
	if layer.Box.Node != nil && html.IsReplacedElement(layer.Box.Node) {
		s := layer.Box.Style
		if s.GetOverflowX() != css.OverflowVisible || s.GetOverflowY() != css.OverflowVisible {
			return true
		}
	}
	return false
}

// isOutOfFlowForPaint reports whether layer's box is absolutely or fixed
// positioned.
func isOutOfFlowForPaint(layer *PaintLayer) bool {
	return layer != nil && layer.Box != nil && isOutOfFlow(layer.Box)
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

// pushOwnClips applies layer's CSS clip (rect(), physical coordinates per CSS
// Writing Modes §7.6) and clip-path, which clip everything the layer paints.
// Pair with popOwnClips.
func (r *Renderer) pushOwnClips(layer *PaintLayer) {
	if layer.HasCSSClip {
		r.pushClipNode(layer, clipNodeCssClip)
	}
	if layer.ClipPath != nil {
		r.dc.Push()
		r.applyClipPath(layer)
	}
}

// popOwnClips undoes pushOwnClips.
func (r *Renderer) popOwnClips(layer *PaintLayer) {
	if layer.ClipPath != nil {
		r.dc.Pop()
	}
	if layer.HasCSSClip {
		r.popClipNode()
	}
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
	// Pixel-snapped clip-path reference box.
	ref := layer.clipPathReferenceBox()
	bx, by, bw, bh := pixelSnap(ref.X(), ref.Y(), ref.Width(), ref.Height())

	// Reference variant: resolve `url(#id)` against the document's
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

	case css.ClipPathInset:
		// BasicShapeInset::GetPath (core/style/basic_shapes.cc:349-379 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the reference box
		// inset by the four offsets (size clamped at 0), corner radii
		// constrained (FloatRoundedRect::ConstrainRadii) to that rect.
		// Path::MakeRoundedRect of a radius-free rect is a plain rect
		// (SkPath::addRRect of a kRect_Type SkRRect), which takes the
		// draw context's axis-aligned clip fast path.
		top, right, bottom, left := cp.Insets[0], cp.Insets[1], cp.Insets[2], cp.Insets[3]
		w := math.Max(bw-left-right, 0)
		h := math.Max(bh-top-bottom, 0)
		if radii := cp.InsetRadii.ConstrainRadii(w, h); !radii.IsZero() {
			buildRoundedRectPathOnDC(r.dc, bx+left, by+top, w, h, radii)
		} else {
			r.dc.DrawRectangle(bx+left, by+top, w, h)
		}

	default:
		return // Unknown type, no clipping
	}

	// A path-based clip-path is a clip node whose ClipPathOp anti-aliases
	// (paint_chunks_to_cc_layer.cc:581-583 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
}

// hasBorderRadius returns true if any corner radius is non-zero.
func hasBorderRadius(layer *PaintLayer) bool {
	return !layer.BorderRadius.IsZero()
}

// applyOverflowClip installs layer's overflow clip on the draw context
// (the caller has Push()ed):
//   - overflow-clip-margin: the margin-adjusted rect rounded by
//     overflowClipRadii (AdjustRoundedClipForOverflowClipMargin).
//   - a table cell forced to clip to its border box: that rect rounded by
//     the border-box radii.
//   - otherwise Blink's two clip nodes: the rectangular OverflowClip
//     (ClipRect, excluding the scrollbar gutter) and, when the box clips
//     along both axes, the InnerBorderRadiusClip — the padding box rounded
//     by the inner border radii, ContouredBorderGeometry::
//     PixelSnappedContouredInnerBorder (NeedsInnerBorderRadiusClip /
//     UpdateInnerBorderRadiusClip, paint_property_tree_builder.cc:2816-2976
//     @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The rect goes first so
//     the rounded mask folds it in without cloning.
func (r *Renderer) applyOverflowClip(layer *PaintLayer) {
	ox, oy, ow, oh := pixelSnap(layer.ClipRect[0], layer.ClipRect[1],
		layer.ClipRect[2], layer.ClipRect[3])
	box := layer.Box
	rounded := hasBorderRadius(layer)
	switch {
	case rounded && layer.HasClipMargin:
		r.buildRoundedRectPath(ox, oy, ow, oh, overflowClipRadii(layer))
	case rounded && box.ClipContentToBorderBox:
		r.buildRoundedRectPath(ox, oy, ow, oh, layer.BorderRadius)
	default:
		r.dc.DrawRectangle(ox, oy, ow, oh)
		if rounded && layer.ClipX && layer.ClipY {
			clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
			r.clipContouredRect(PixelSnappedContouredInnerBorder(layer.BorderRadius,
				toLayoutRect(box.X, box.Y, box.Width, box.Height),
				toLayoutStrut(box.Border)), textshape.ClipOpIntersect, graphics.AntiAliased)
			return
		}
	}
	// Overflow clip nodes become ClipRectOp / ClipRRectOp with
	// antialias=true (paint_chunks_to_cc_layer.cc:573-579 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
}

// overflowClipRadii returns the elliptical radii to use when drawing the
// overflow clip path for `layer`. CSS Overflow 4 §3.2 specifies that the
// overflow clip edge "is shaped in the corners exactly the same way as an
// outer box-shadow with a spread radius of the same cumulative offset
// from the box's border edge" — so this must stay formula-identical to
// the box-shadow spread path (WPT overflow-clip-margin-border-radius
// asserts the parity directly).
//
// Mirrors Blink's `AdjustRoundedClipForOverflowClipMargin`
// (core/paint/paint_property_tree_builder.cc:2869 @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): start from the inner
// (padding-box) radii — border-box radii inset by the border widths —
// then apply ONE corner-corrected outset combining the reference-box
// delta (border widths for border-box, zero for padding-box, minus
// padding for content-box) and the margin length, using the same
// coverage-factor formula as box-shadow spread. The coverage edge is
// the pre-outset padding-box size.
//
// Only the overflow-clip-margin path calls it (see applyOverflowClip).
func overflowClipRadii(layer *PaintLayer) css.EllipticalRadii {
	box := layer.Box
	radii := layer.BorderRadius.Inset(
		box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left)
	// Per-side outset from the padding edge to the clip edge:
	// borderWidth − visualBoxInset + marginLength.
	s := layer.ClipMarginLength
	top := box.Border.Top - layer.ClipMarginVisualBoxInset[0] + s
	right := box.Border.Right - layer.ClipMarginVisualBoxInset[1] + s
	bottom := box.Border.Bottom - layer.ClipMarginVisualBoxInset[2] + s
	left := box.Border.Left - layer.ClipMarginVisualBoxInset[3] + s
	pw := box.Width - box.Border.Left - box.Border.Right
	ph := box.Height - box.Border.Top - box.Border.Bottom
	return radii.OutsetWithCornerCorrectionUsingCoverageFactor(
		top, right, bottom, left, pw, ph)
}

// buildRoundedRectPath traces a rounded rectangle path using CubicTo for
// elliptical corner arcs. Uses kappa constant for quarter-ellipse approximation.
func (r *Renderer) buildRoundedRectPath(x, y, w, h float64, radii css.EllipticalRadii) {
	buildRoundedRectPathOnDC(r.dc, x, y, w, h, radii)
}

// roundedCornerKappa places a cubic Bézier's control points for a quarter
// ellipse: 4*(sqrt(2)-1)/3 of the radius from each end along its tangent.
const roundedCornerKappa = 0.5522847498

// buildRoundedRectPathOnDC traces an elliptical rounded rectangle path on any DrawContext.
// This is the single source of truth for rounded-rect path construction.
func buildRoundedRectPathOnDC(dc pathBuilder, x, y, w, h float64, radii css.EllipticalRadii) {
	const k = roundedCornerKappa
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
func buildRoundedRectPathReverseDC(dc pathBuilder, x, y, w, h float64, radii css.EllipticalRadii) {
	const k = roundedCornerKappa
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

// borderBoxRect returns box's border-box rect: the rect Blink's box
// decoration painters take (BoxPainterBase::PaintFillLayers,
// BoxBorderPainter::PaintBorder, PaintNormalBoxShadow), which a fieldset
// contracts for its legend (paint/fieldset_painter.cc:75-84 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func borderBoxRect(box *layout.Box) layout.PhysicalRect {
	return rectXYWH(box.X, box.Y, box.Width, box.Height)
}

// rectXYWH builds a PhysicalRect from its origin and size.
func rectXYWH(x, y, w, h float64) layout.PhysicalRect {
	return layout.PhysicalRect{Offset: layout.PhysicalOffset{X: x, Y: y}, Size: layout.PhysicalSize{Width: w, Height: h}}
}

// backgroundClipRectForClip computes the background painting area based on
// a background-clip value (border-box, padding-box, content-box).
func backgroundClipRectForClip(box *layout.Box, rect layout.PhysicalRect, clip css.BackgroundClipType) (float64, float64, float64, float64) {
	switch clip {
	case css.BackgroundClipPaddingBox:
		return pixelSnap(
			rect.Offset.X+box.Border.Left, rect.Offset.Y+box.Border.Top,
			rect.Size.Width-box.Border.Left-box.Border.Right,
			rect.Size.Height-box.Border.Top-box.Border.Bottom)
	case css.BackgroundClipContentBox:
		return pixelSnap(
			rect.Offset.X+box.Border.Left+box.Padding.Left,
			rect.Offset.Y+box.Border.Top+box.Padding.Top,
			rect.Size.Width-box.Border.Left-box.Border.Right-box.Padding.Left-box.Padding.Right,
			rect.Size.Height-box.Border.Top-box.Border.Bottom-box.Padding.Top-box.Padding.Bottom)
	default: // border-box
		return pixelSnap(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height)
	}
}

// backgroundPaintRectForLayer returns the background painting area for a
// box's fill layer: its background-clip rect. (The canvas background is
// painted by the view, view_painter.go.)
func (r *Renderer) backgroundPaintRectForLayer(layer *PaintLayer, rect layout.PhysicalRect, clip css.BackgroundClipType) (float64, float64, float64, float64) {
	return backgroundClipRectForClip(layer.Box, rect, clip)
}

// isClippedWithLocalScrolling mirrors FillLayerInfo::
// is_clipped_with_local_scrolling (box_painter_base.cc:639-641 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), fed PhysicalBoxFragment::
// IsScrollContainer by BoxFragmentPainter::GetFillLayerInfo
// (box_fragment_painter.cc:2345-2358): a fill layer with
// background-attachment: local on a scroll container paints in the
// scrolled contents' coordinate space. A canvas background is painted by
// the view (ViewPainter), never through this box path.
func isClippedWithLocalScrolling(layer *PaintLayer, attachment css.BackgroundAttachmentType) bool {
	return layer.Box != nil && layer.Box.IsScrollContainer() &&
		attachment == css.BackgroundAttachmentLocal
}

// contouredBorderClip returns the box ContouredBorderRectForClip
// (box_painter_base.cc:1236-1246 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) clips a rounded fill to:
// content-box first, then the padding box for padding-box or a
// local-scrolling layer (its inner border), else the declared clip.
func contouredBorderClip(clip css.BackgroundClipType, local bool) css.BackgroundClipType {
	switch {
	case clip == css.BackgroundClipContentBox:
		return clip
	case clip == css.BackgroundClipPaddingBox || local:
		return css.BackgroundClipPaddingBox
	}
	return clip
}

// bottomLayerAttachment returns the background-attachment of the bottom
// surviving fill layer, or the element's style when it has no fill layers
// (only background-color + background-attachment are declared).
func bottomLayerAttachment(layer *PaintLayer) css.BackgroundAttachmentType {
	var bottom *css.FillLayer
	for cur := layer.BackgroundLayers; cur != nil; cur = cur.Next {
		bottom = cur
	}
	if bottom != nil {
		return bottom.Attachment
	}
	if layer.Box != nil && layer.Box.Style != nil {
		return layer.Box.Style.GetBackgroundAttachment()
	}
	return ""
}

// effectiveBackgroundClip returns the box the background color's rounded
// clip uses: the bottom-most CSS layer's declared clip resolved through
// ContouredBorderRectForClip (contouredBorderClip) with the bottom layer's
// local scrolling.
//
// The declared clip is the pre-computed background-color clip
// (Style.GetBackgroundColorClip), which already selects the bottom-most
// CSS layer's clip with the proper layer-count cycling. The
// BackgroundLayers list must NOT be used to re-derive it: CullEmptyLayers
// drops empty trailing layers and always keeps the head, so its tail is
// the topmost surviving layer rather than the CSS bottom-most layer (e.g.
// `background-image: none, none` collapses to the head's clip).
func effectiveBackgroundClip(layer *PaintLayer) css.BackgroundClipType {
	return contouredBorderClip(layer.BackgroundClip,
		isClippedWithLocalScrolling(layer, bottomLayerAttachment(layer)))
}

// backgroundClipRadiiForClip returns the border radii inset to a
// background-clip box: Blink's Radii inset (shrink Rx by the horizontal
// inset, Ry by the vertical), NOT re-constrained to the inset rect — only
// the outer border radii are constrained (PixelSnappedContouredBorderWithOutsets
// / PixelSnappedContouredInnerBorder, contoured_border_geometry.cc:207-255 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Radii that overlap in the inset
// rect are clipped corner by corner (clipRoundedInnerRect).
func backgroundClipRadiiForClip(layer *PaintLayer, rect layout.PhysicalRect, clip css.BackgroundClipType) css.EllipticalRadii {
	radii := layer.BorderRadius
	box := layer.Box
	var top, right, bottom, left float64
	switch clip {
	case css.BackgroundClipPaddingBox:
		top, right, bottom, left = box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left
	case css.BackgroundClipContentBox:
		top = box.Border.Top + box.Padding.Top
		right = box.Border.Right + box.Padding.Right
		bottom = box.Border.Bottom + box.Padding.Bottom
		left = box.Border.Left + box.Padding.Left
	default: // border-box
		return radii
	}
	return radii.Inset(top, right, bottom, left)
}

// bodyOverflowPropagatesToViewport reports whether a <body> element's overflow
// propagates to the viewport (CSS Overflow §3.3). Propagation happens only when
// the root <html> has visible overflow and establishes no paint/layout
// containment; root containment (e.g. contain: paint) stops propagation, so the
// body retains its own scrollbar. When the root itself has non-visible overflow,
// the root — not the body — is the viewport-propagating element, and the body
// keeps its own scrollbar.
// paintScrollbars paints the classic scrollbar widget (track + thumb) into
// the reserved gutter of a scroll container. Layout reserves the gutter as
// box.Scrollbar insets (ComputeScrollbarLogicalEdges, CSS Overflow §3); the
// vertical bar is on the physical right by default or the physical left for
// horizontal RTL (placeVerticalScrollbarOnLeft), the horizontal bar on the
// physical bottom. The vertical bar spans the full padding-box height and owns
// the corner; the horizontal bar fills the remaining width beside it.
//
// scrollbar-color maps the thumb and track colours (CSS Scrollbars 1); auto
// uses the engine's default classic colours. The thumb is half the track,
// matching the louis14 classic-scrollbar reference geometry, placed along it
// by the scroll position (thumbPosition).
//
// Blink analog: ScrollableAreaPainter::PaintScrollbar /
// ScrollbarThemeAura::PaintThumb+PaintTrackBackground
// (scrollable_area_painter.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func (r *Renderer) paintScrollbars(layer *PaintLayer) {
	box := layer.Box
	if box == nil || box.Style == nil {
		return
	}
	sb := box.Scrollbar
	if sb.Right <= 0 && sb.Left <= 0 && sb.Bottom <= 0 && sb.Top <= 0 {
		return // no reserved classic-scrollbar gutter (the common case)
	}
	// Skip boxes that do not own their scrollbar widget:
	//   - The root <html> and a viewport-defining <body> propagate their
	//     overflow to the viewport (CSS Overflow §3.3); the viewport — not the
	//     element box — owns that scrollbar, and louis14 does not paint
	//     viewport scrollbars. IsViewportDefiningBody encodes the propagation
	//     rule (root overflow visible + no containment + first body in DOM),
	//     so a body kept non-propagating by root containment still paints its
	//     own scrollbar (overflow-body-propagation-011).
	//   - Replaced elements (img, iframe, video, canvas, …) get the UA
	//     `overflow: clip` treatment and never generate scrollbars even with
	//     overflow: scroll (CSS Overflow §3.7).
	if box.Node != nil {
		switch {
		case box.Node.TagName == "html":
			return
		case IsViewportDefiningBody(box):
			return
		case html.IsReplacedElement(box.Node):
			return
		}
	}

	vWidth := sb.Right
	vOnLeft := false
	if sb.Left > 0 {
		vWidth = sb.Left
		vOnLeft = true
	}
	hHeight := sb.Bottom

	// Resolve track/thumb colours. scrollbar-color: <thumb> <track>.
	scv := box.Style.GetScrollbarColor()
	trackColor := css.Color{R: 200, G: 200, B: 200, A: 1}
	thumbColor := css.Color{R: 160, G: 160, B: 160, A: 1}
	if !scv.IsAuto {
		thumbColor = scv.Thumb
		trackColor = scv.Track
	}

	// Padding-box geometry: the gutter sits between the inner border edge
	// and the padding edge.
	px := box.X + box.Border.Left
	py := box.Y + box.Border.Top
	pw := box.Width - box.Border.Left - box.Border.Right
	ph := box.Height - box.Border.Top - box.Border.Bottom

	fill := func(x, y, w, h float64, c css.Color) {
		if w <= 0 || h <= 0 {
			return
		}
		sx, sy, sw, sh := pixelSnap(x, y, w, h)
		r.setColor(c)
		r.dc.DrawRectangle(sx, sy, sw, sh)
		r.dc.Fill()
	}

	// Vertical bar: full padding-box height, owns the corner.
	if vWidth > 0 {
		vx := px + pw - vWidth
		if vOnLeft {
			vx = px
		}
		fill(vx, py, vWidth, ph, trackColor)
		fill(vx, py+thumbPosition(ph, box.ScrollOffset.Y, box.MinimumScrollOffset.Y, box.MaximumScrollOffset.Y), vWidth, ph*0.5, thumbColor)
	}
	// Horizontal bar: remaining width beside the vertical bar.
	if hHeight > 0 {
		hx := px
		if vOnLeft {
			hx = px + vWidth
		}
		hw := pw - vWidth
		hy := py + ph - hHeight
		fill(hx, hy, hw, hHeight, trackColor)
		fill(hx+thumbPosition(hw, box.ScrollOffset.X, box.MinimumScrollOffset.X, box.MaximumScrollOffset.X), hy, hw*0.5, hHeight, thumbColor)
	}
}

// thumbPosition returns the thumb's offset along a track of trackLength
// whose thumb is half the track: the scroll position (offset relative to
// the minimum, Scrollbar::CurrentPos) scaled from the scroll range
// (Scrollbar::Maximum, scroll/scrollbar.cc:147-154) onto the free track.
// Mirrors ScrollbarTheme::ThumbPosition (scroll/scrollbar_theme.cc:195-208
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) with louis14's fixed
// half-track thumb length.
func thumbPosition(trackLength, offset, minimum, maximum float64) float64 {
	size := maximum - minimum
	if size <= 0 {
		return 0
	}
	return max(0, offset-minimum) * (trackLength - trackLength*0.5) / size
}

// drawBackground paints the layer's background color and image layers (pre-computed).
// Layers are painted bottom-to-top (Blink's IterateFillLayersInReverseOrder).
// Background-color is painted only with the bottommost layer.
//
// A background that transfers to the view is painted by the view
// (paintViewBackground), not here (BoxFragmentPainter::PaintBackground,
// box_fragment_painter.cc:1923 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) drawBackground(layer *PaintLayer, rect layout.PhysicalRect) {
	if r.backgroundTransfersToView(layer) {
		return
	}
	fl := layer.BackgroundLayers
	if fl == nil {
		// No fill layers: only the background color.
		r.paintFillLayerColor(layer, rect)
		return
	}

	// The paint context is per box, shared by all its fill layers
	// (BoxBackgroundPaintContext, box_background_paint_context.h @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	paintRect := backgroundPaintRect(rect)
	ctx := r.newBoxBackgroundPaintContext(layer)

	// Paint from bottom to top.
	fl.IterateReverse(func(bg *css.FillLayer) {
		// FillLayerInfo takes the respect-orientation from the painted box's
		// style (box_painter_base.cc:633 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		img := r.loadFillLayerImage(bg, layer.ImageOrientation)
		// Background-color only on the bottom layer, and not under an
		// image that occludes it (FillLayerInfo::should_paint_color,
		// box_painter_base.cc:696-699).
		if bg.IsBottomLayer() && !imageOccludesNextLayers(bg, img) {
			r.paintFillLayerColor(layer, rect)
		}
		if bg.Gradient != "" || !img.isNone() {
			r.paintFillLayer(layer, ctx, rect, paintRect, bg, img)
		}
	})
}

// paintFillLayerColor paints the background color under the bottom-most
// CSS layer's clip (effectiveBackgroundClip) — the color branch of
// BoxPainterBase::PaintFillLayer for the bottom layer
// (PaintFillLayerBackground, box_painter_base.cc:1264-1275 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): it fills the layer's clip
// area, within the scrolled contents for a local-scrolling bottom layer.
func (r *Renderer) paintFillLayerColor(layer *PaintLayer, rect layout.PhysicalRect) {
	if layer.BackgroundColor.A == 0 {
		return
	}
	local := isClippedWithLocalScrolling(layer, bottomLayerAttachment(layer))
	r.dc.Push()
	defer r.dc.Pop()
	r.fillLayerClipPath(layer, rect, effectiveBackgroundClip(layer), local)
	r.setColor(layer.BackgroundColor)
	r.dc.Fill()
}

// fillLayerImage is one fill layer's fetched url() image (StyleFetchedImage):
// a raster image, or an SVG image drawn at the tile size. Neither set means
// the layer paints its gradient.
type fillLayerImage struct {
	raster image.Image
	svg    *images.SVGImage
}

func (f fillLayerImage) isNone() bool { return f.raster == nil && f.svg == nil }

// imageOccludesNextLayers reports whether bg's image covers everything
// beneath it: FillLayer::ImageOccludesNextLayers for the source-over,
// normal-blend composite louis14 paints with — an image that tiles the
// layer (ImageTilesLayer: repeat or round on both axes) and is known to be
// opaque with a non-empty size (ImageIsOpaque) (core/style/fill_layer.cc:
// 419-464 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). It takes the decoded
// image because louis14's css.FillLayer holds only the url. Only a raster
// image is known to be opaque here (BitmapImage::
// CurrentFrameKnownToBeOpaque); an SVG image never is, and a gradient's
// opacity (CSSGradientValue::KnownToBeOpaque) is not ported.
func imageOccludesNextLayers(bg *css.FillLayer, img fillLayerImage) bool {
	tiles := func(t css.BackgroundRepeatType) bool {
		return t == css.BackgroundRepeatRepeat || t == css.BackgroundRepeatRound
	}
	if img.raster == nil || !tiles(bg.Repeat.X) || !tiles(bg.Repeat.Y) || img.raster.Bounds().Empty() {
		return false
	}
	o, ok := img.raster.(interface{ Opaque() bool })
	return ok && o.Opaque()
}

// loadImageForPaint loads the bitmap at url as it is drawn with
// respectOrientation: oriented under kRespectImageOrientation
// (BitmapImage::Draw, bitmap_image.cc:343-363 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), as stored otherwise. Its size
// is then Image::SizeAsFloat(respect_orientation), so the natural sizing
// info every painter derives from it is the oriented one
// (StyleFetchedImage::GetNaturalSizingInfo, style_fetched_image.cc:163-176).
// louis14 has no origin model, so StyleImage::ForceOrientationIfNecessary
// (style_image.cc:11-19) — forcing orientation for a cross-origin image —
// is not applied.
func (r *Renderer) loadImageForPaint(url string, respectOrientation graphics.RespectImageOrientationEnum) (image.Image, error) {
	if respectOrientation == graphics.RespectImageOrientation {
		return images.OrientedImageWithFetcher(url, r.imageFetcher)
	}
	return images.LoadImageWithFetcher(url, r.imageFetcher)
}

// loadFillLayerImage fetches a fill layer's url() image, keeping an SVG
// document as an SVGImage (StyleFetchedImage over an SVGImage) so it is
// sized by its natural sizing info and drawn for its container. None when
// the layer has no url() image or there is no fetcher (data: URIs decode
// inline and need none).
func (r *Renderer) loadFillLayerImage(bg *css.FillLayer, respectOrientation graphics.RespectImageOrientationEnum) fillLayerImage {
	if bg.Image == nil || (r.imageFetcher == nil && !stdnet.IsDataURL(bg.Image.Data.Absolute)) {
		return fillLayerImage{}
	}
	url := bg.Image.Data.Absolute
	if svg := images.LoadSVGImageWithFetcher(url, r.imageFetcher); svg != nil {
		return fillLayerImage{svg: svg}
	}
	img, _ := r.loadImageForPaint(url, respectOrientation)
	return fillLayerImage{raster: img}
}

// paintFillLayer paints one fill layer's image content (url() image img,
// or the layer's gradient when img is none) clipped to its background-clip
// area. Mirrors the image branch of BoxPainterBase::PaintFillLayer
// (box_painter_base.cc:1349-1570 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a local-scrolling layer is
// positioned in the scrolled paint rect (:1378-1386, :1424-1425).
func (r *Renderer) paintFillLayer(layer *PaintLayer, ctx *BoxBackgroundPaintContext, rect layout.PhysicalRect, paintRect geometry.PhysicalRect, bg *css.FillLayer, img fillLayerImage) {
	local := isClippedWithLocalScrolling(layer, bg.Attachment)
	r.dc.Push()
	defer r.dc.Pop()
	scrolledPaintRect, aa := r.fillLayerClipPath(layer, rect, contouredBorderClip(bg.Clip, local), local)
	clipPath(r.dc, aa, textshape.ClipOpIntersect)
	if local {
		paintRect = backgroundPaintRect(scrolledPaintRect)
	}
	r.paintFillLayerImage(ctx, paintRect, bg, img, layer.ImageRendering, layer.Box.Style)
}

// fillLayerClipPath builds the path of a fill layer's clip area and
// returns its scrolled paint rect. clip is already resolved by
// contouredBorderClip. A local-scrolling layer first clips the context to
// the overflow clip rect (AdjustRectForScrolledContent; the caller holds
// the state save), so a padding-box clip of the scrolled rect equals
// Blink's unclipped border-box one there. The rounded fill clips against
// the unscrolled rect (ContouredBorderRectForClip, box_painter_base.cc:
// 1474-1477 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the non-rounded
// padding/content clip contracts the scrolled one (:1533-1551).
//
// It also returns how a clip to that path anti-aliases: a renderable
// rounded rect is RoundedInnerRectClipper's ClipContouredRect
// (anti-aliased); a plain rect is GraphicsContext::Clip
// (ToPixelSnappedRect(clip_rect), not anti-aliased, :1540-1551).
func (r *Renderer) fillLayerClipPath(layer *PaintLayer, rect layout.PhysicalRect, clip css.BackgroundClipType, local bool) (layout.PhysicalRect, graphics.AntiAliasingMode) {
	scrolledPaintRect := rect
	if local {
		scrolledPaintRect = r.adjustRectForScrolledContent(layer.Box, rect)
	}
	if radii := backgroundClipRadiiForClip(layer, rect, clip); !radii.IsZero() {
		x, y, w, h := r.backgroundPaintRectForLayer(layer, rect, clip)
		rr := FloatRoundedRect{Rect: geometry.NewRectF(x, y, w, h), Radii: radii}
		// A border-box fill under borders that obscure its edge shrinks
		// away from the anti-aliased border edge (ContouredBorderRectForClip,
		// box_painter_base.cc:1226-1231).
		if clip == css.BackgroundClipBorderBox && !local {
			if edges := borderEdgesForLayer(layer, backgroundPaintRect(rect)); computeBleedAvoidance(layer, edges) == layout.BackgroundBleedShrinkBackground {
				rr = backgroundRoundedRectAdjustedForBleedAvoidance(edges, rr)
			}
		}
		if rr.IsRenderable() {
			rr.addPath(r.dc)
			return scrolledPaintRect, graphics.AntiAliased
		}
		// Non-renderable rrect: corner-wise clip, then the plain rect
		// (PaintFastBottomLayer / PaintFillLayer's
		// RoundedInnerRectClipper, box_painter_base.cc:1101-1108,
		// :1503-1505 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		r.clipRoundedInnerRect(geometry.NewRectF(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height), rr)
		r.dc.DrawRectangle(x, y, w, h)
	} else {
		r.dc.DrawRectangle(r.backgroundPaintRectForLayer(layer, scrolledPaintRect, clip))
	}
	return scrolledPaintRect, graphics.NotAntiAliased
}

// adjustRectForScrolledContent mirrors BoxFragmentPainter::
// AdjustRectForScrolledContent (box_fragment_painter.cc:2321-2343 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): it clips the context to the
// overflow clip rect (LayoutBox::OverflowClipRect, layout_box.cc:2364-2416:
// the padding box minus the scrollbars) and returns the paint rect of the
// scrolled contents with the borders at its ends — offset by the floored
// −ScrollPosition and sized to ScrollSize plus the borders snapped to
// whole pixels (ComputeSnappedBorders, box_painter_base.cc:1340-1345).
func (r *Renderer) adjustRectForScrolledContent(box *layout.Box, rect layout.PhysicalRect) layout.PhysicalRect {
	b, sb := box.Border, box.Scrollbar
	x, y, w, h := backgroundClipRectForClip(box, rect, css.BackgroundClipPaddingBox)
	r.dc.DrawRectangle(x+sb.Left, y+sb.Top, w-sb.Left-sb.Right, h-sb.Top-sb.Bottom)
	r.dc.Clip()
	pos := box.ScrollPosition()
	return rectXYWH(
		rect.Offset.X-math.Floor(pos.X), rect.Offset.Y-math.Floor(pos.Y),
		box.ScrollSize.Width+math.Trunc(b.Left)+math.Trunc(b.Right),
		box.ScrollSize.Height+math.Trunc(b.Top)+math.Trunc(b.Bottom))
}

// toLayoutRect converts a float rect to LayoutUnits (round-to-nearest
// 1/64 px).
func toLayoutRect(x, y, w, h float64) geometry.PhysicalRect {
	return geometry.PhysicalRect{
		Offset: geometry.PhysicalOffsetFromF64Round(x, y),
		Size:   geometry.PhysicalSizeFromF64Round(w, h),
	}
}

// toLayoutStrut converts a css.BoxEdge to a LayoutUnit PhysicalBoxStrut.
func toLayoutStrut(e css.BoxEdge) geometry.PhysicalBoxStrut {
	return geometry.PhysicalBoxStrut{
		Top:    layoutunit.FromFloat64Round(e.Top),
		Right:  layoutunit.FromFloat64Round(e.Right),
		Bottom: layoutunit.FromFloat64Round(e.Bottom),
		Left:   layoutunit.FromFloat64Round(e.Left),
	}
}

// backgroundPaintRect returns Blink's paint_rect for a box's fill layer:
// its border-box paint rect.
func backgroundPaintRect(rect layout.PhysicalRect) geometry.PhysicalRect {
	return toLayoutRect(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height)
}

// newBoxBackgroundPaintContext builds the BoxBackgroundPaintContext for a
// box's fill layers: the LayoutBoxModelObject constructor
// (box_background_paint_context.cc:117-119, :176-187 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), positioning_box_ = the box
// itself. (The view's constructor is newViewBoxBackgroundPaintContext,
// view_painter.go.)
//
// louis14's has_background_fixed_to_viewport_ (HasBackgroundFixedToViewport,
// :342-364: no transformed ancestor) is the transform-depth rule CSS
// Transforms 1 §6 states. The fixed positioning area is the target viewport
// (FixedAttachmentPositioningArea, :366-389).
func (r *Renderer) newBoxBackgroundPaintContext(layer *PaintLayer) *BoxBackgroundPaintContext {
	ctx := &BoxBackgroundPaintContext{
		fixedAttachmentPositioningArea: geometry.PhysicalRectFromRect(r.target.Bounds()),
	}
	box := layer.Box
	ctx.hasBackgroundFixedToViewport = r.transformDepth == 0
	ctx.borderOutsets = toLayoutStrut(box.Border)
	ctx.paddingOutsets = toLayoutStrut(box.Padding)
	widths := [4]float64{box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left}
	for side := range 4 {
		// BorderEdge::ObscuresBackground (core/style/border_edge.cc:56-67 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a double border
		// narrower than 3px paints as solid (BorderEdge::EffectiveStyle,
		// :23-30).
		style := layer.BorderStyles[side]
		if style == css.BorderStyleDouble && widths[side] < 3 {
			style = css.BorderStyleSolid
		}
		present := widths[side] > 0 && style != css.BorderStyleNone && style != "hidden"
		ctx.obscuringEdges[side] = present && layer.BorderColors[side].A >= 1 &&
			style != css.BorderStyleDotted && style != css.BorderStyleDashed && style != css.BorderStyleDouble
	}
	ctx.hasBorderImageOrCollapse = layer.BorderImageSource != nil ||
		(box.Style != nil && box.Style.GetBorderCollapse() == css.BorderCollapseCollapse)
	return ctx
}

// paintFillLayerImage paints one fill layer's image through the shared
// BackgroundImageGeometry: img is the fetched url() image, or none to paint
// the layer's gradient — a generated image with no natural size, painted
// through exactly the same geometry and tiling (StyleGeneratedImage →
// GradientGeneratedImage sized to TileSize()). An SVG image reports its
// natural sizing info (StyleFetchedImage::GetNaturalSizingInfo,
// style_fetched_image.cc:163-183) and is drawn laid out in a TileSize()
// container (StyleFetchedImage::GetImage → SVGImageForContainer,
// :231-247; box_painter_base.cc:1441-1443). Mirrors the image branch of
// BoxPainterBase::PaintFillLayer (box_painter_base.cc:1349-1570 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) → BackgroundImageGeometry::
// Calculate → PaintFillLayerBackground (:1250-1298) → DrawTiledBackground.
// imageStyle is BoxBackgroundPaintContext::ImageStyle
// (box_background_paint_context.cc:399-405).
func (r *Renderer) paintFillLayerImage(ctx *BoxBackgroundPaintContext, paintRect geometry.PhysicalRect, bg *css.FillLayer, img fillLayerImage, imageRendering string, imageStyle *css.Style) {
	var info layout.NaturalSizingInfo
	var tileImage func(w, h int) image.Image
	switch {
	case img.svg != nil:
		info = naturalSizingInfoForSVG(img.svg.Sizing)
		tileImage = func(w, h int) image.Image {
			if tile := r.svgImageForContainer(img.svg, geometry.SizeF{Width: float64(w), Height: float64(h)}, w, h); tile != nil {
				return tile
			}
			return image.Transparent
		}
	case img.raster != nil:
		src := img.raster
		srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
		if srcW == 0 || srcH == 0 {
			return
		}
		info = layout.MakeFixedNaturalSizingInfo(geometry.SizeF{Width: float64(srcW), Height: float64(srcH)})
		tileImage = func(w, h int) image.Image {
			if w == srcW && h == srcH {
				return src
			}
			return scaleImage(src, srcW, srcH, w, h, imageRendering)
		}
	default:
		tileImage = func(w, h int) image.Image {
			return r.renderGeneratedImage(bg.Gradient, w, h, imageStyle)
		}
	}
	var g BackgroundImageGeometry
	g.Calculate(bg, ctx, paintRect, info)
	// PaintFillLayerBackground skips an empty dest rect or tile
	// (box_painter_base.cc:1277-1278).
	if g.SnappedDestRect().IsEmpty() || g.TileSize().Width.LessEqual(layoutunit.Zero) ||
		g.TileSize().Height.LessEqual(layoutunit.Zero) {
		return
	}
	r.drawTiledBackground(&g, tileImage)
}

// drawTiledBackground paints the tile grid the geometry describes into its
// snapped dest rect: tiles of TileSize() separated by SpaceSize(), the first
// at SnappedDestRect().offset + ComputePhase(). Each tile is drawn at its
// pixel-snapped extent through the draw context, so the active clip
// (including a rounded background clip mask) and transform apply to every
// pixel. Mirrors DrawTiledBackground (box_painter_base.cc:814-890 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), including its rule that a tile
// within 0.5px of the dest size takes the dest size (no lone repeated row
// or column, :862-874). tileImage returns the image scaled to a w×h tile.
func (r *Renderer) drawTiledBackground(g *BackgroundImageGeometry, tileImage func(w, h int) image.Image) {
	dest := g.SnappedDestRect()
	destPx := geometry.ToPixelSnappedRect(dest)
	tileW, tileH := g.TileSize().Width.Float64(), g.TileSize().Height.Float64()
	if math.Abs(tileW-dest.Size.Width.Float64()) <= 0.5 {
		tileW = dest.Size.Width.Float64()
	}
	if math.Abs(tileH-dest.Size.Height.Float64()) <= 0.5 {
		tileH = dest.Size.Height.Float64()
	}
	stepW := tileW + g.SpaceSize().Width.Float64()
	stepH := tileH + g.SpaceSize().Height.Float64()
	if stepW <= 0 || stepH <= 0 {
		return
	}
	phase := g.ComputePhase()
	originX := dest.Offset.Left.Float64() + phase.Left.Float64()
	originY := dest.Offset.Top.Float64() + phase.Top.Float64()
	// The pattern extends in both directions. ComputePhase keeps the phase
	// within one step, so at most one step back reaches the near edge.
	if originX > float64(destPx.Min.X) {
		originX -= stepW
	}
	if originY > float64(destPx.Min.Y) {
		originY -= stepH
	}

	// Assemble the visible tiles into one dest-sized buffer, then draw it
	// once through the context so its clip (a rounded background clip
	// mask) and transform are applied once per layer, not once per tile.
	// Zero-origin buffer: the context's transformed DrawImage path maps
	// source coordinates from (0,0), so a buffer whose bounds start at
	// destPx.Min would be offset twice under a transform.
	buf := image.NewRGBA(image.Rect(0, 0, destPx.Dx(), destPx.Dy()))
	var tiles [4]struct {
		size image.Point
		img  image.Image
	}
	nTiles := 0
	tileFor := func(size image.Point) image.Image {
		for i := range nTiles {
			if tiles[i].size == size {
				return tiles[i].img
			}
		}
		img := tileImage(size.X, size.Y)
		if nTiles < len(tiles) {
			tiles[nTiles].size, tiles[nTiles].img = size, img
			nTiles++
		}
		return img
	}
	for j := 0; ; j++ {
		ty := originY + float64(j)*stepH
		if ty >= float64(destPx.Max.Y) {
			break
		}
		y0, y1 := int(math.Round(ty)), int(math.Round(ty+tileH))
		if y1 <= destPx.Min.Y || y1 <= y0 {
			continue
		}
		for i := 0; ; i++ {
			tx := originX + float64(i)*stepW
			if tx >= float64(destPx.Max.X) {
				break
			}
			x0, x1 := int(math.Round(tx)), int(math.Round(tx+tileW))
			if x1 <= destPx.Min.X || x1 <= x0 {
				continue
			}
			tileRect := image.Rect(x0, y0, x1, y1)
			tile := tileFor(tileRect.Size())
			vis := tileRect.Intersect(destPx)
			draw.Draw(buf, vis.Sub(destPx.Min), tile, tile.Bounds().Min.Add(vis.Min.Sub(tileRect.Min)), draw.Src)
		}
	}
	r.dc.DrawImage(buf, destPx.Min.X, destPx.Min.Y)
}

// drawImage paints an <img> element's image. Mirrors
// ImagePainter::PaintReplaced (core/paint/image_painter.cc:118-203 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the paint rect is the replaced
// content rect (LayoutReplaced::ReplacedContentRect →
// ComputeReplacedContentRect, layout_replaced.cc:356-420, which without an
// object-view-box is ComputeObjectFitAndPositionRect over the image's natural
// size), and the visual rect is the content box when the element
// ClipsToContentBox, else the paint rect.
func (r *Renderer) drawImage(layer *PaintLayer) {
	if layer.ImageSrc == "" || (r.imageFetcher == nil && !stdnet.IsDataURL(layer.ImageSrc)) {
		return
	}
	contentRect := replacedContentBox(layer.Box)
	if contentRect.Size.IsEmpty() {
		return
	}

	// An SVG image is fitted by its SVG natural dimensions
	// (LayoutImageResource::GetNaturalDimensions → SVGImageForContainer::
	// GetNaturalDimensions, layout_image_resource.cc:153-158 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and drawn laid out at the
	// destination size: PaintIntoRect paints image_resource.GetImage(
	// dest_rect.size), an SVGImageForContainer (image_painter.cc:214-217).
	if svg := images.LoadSVGImageWithFetcher(layer.ImageSrc, r.imageFetcher); svg != nil {
		paintRect := layout.ComputeReplacedContentRect(layer.ObjectFit, layer.ObjectPosition, layer.ObjectViewBox, contentRect,
			layout.PhysicalNaturalSizingInfoFromSizingInfo(naturalSizingInfoForSVG(svg.Sizing)))
		dest := geometry.ToPixelSnappedRect(paintRect)
		if dest.Empty() {
			return
		}
		img := r.svgImageForContainer(svg, geometry.SizeF{Width: paintRect.Size.Width.Float64(), Height: paintRect.Size.Height.Float64()}, dest.Dx(), dest.Dy())
		if img == nil {
			return
		}
		r.paintImageIntoRect(layer, img, paintRect, imageVisualRect(layer, contentRect, paintRect))
		return
	}

	// ImagePainter::PaintIntoRect draws with the style's respect-orientation
	// (image_painter.cc:224-247), and the natural size the replaced content
	// rect is fitted to is the oriented size.
	img, err := r.loadImageForPaint(layer.ImageSrc, layer.ImageOrientation)
	if err != nil {
		return
	}
	srcW, srcH := img.Bounds().Dx(), img.Bounds().Dy()
	if srcW == 0 || srcH == 0 {
		return
	}
	paintRect := layout.ComputeReplacedContentRect(layer.ObjectFit, layer.ObjectPosition, layer.ObjectViewBox, contentRect,
		layout.MakeFixedPhysicalNaturalSizingInfo(geometry.NewPhysicalSize(srcW, srcH)))
	r.paintImageIntoRect(layer, img, paintRect, imageVisualRect(layer, contentRect, paintRect))
}

// replacedContentBox is a replaced box's content box in page coordinates
// (PhysicalContentBoxRect moved to the paint offset).
func replacedContentBox(box *layout.Box) geometry.PhysicalRect {
	return geometry.PhysicalRect{
		Offset: geometry.PhysicalOffsetFromF64Round(box.X+box.Border.Left+box.Padding.Left, box.Y+box.Border.Top+box.Padding.Top),
		Size: geometry.PhysicalSizeFromF64Round(
			box.Width-box.Border.Left-box.Border.Right-box.Padding.Left-box.Padding.Right,
			box.Height-box.Border.Top-box.Border.Bottom-box.Padding.Top-box.Padding.Bottom),
	}
}

// paintCanvas paints a <canvas>'s bitmap. Mirrors HTMLCanvasPainter::
// PaintReplaced (core/paint/html_canvas_painter.cc:36-88 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the paint rect is the replaced
// content rect over the canvas's natural size (its bitmap size), drawn with
// the image-rendering interpolation (InterpolationQualityForCanvas, :22-33)
// and, like an image, clipped to the content box when the element
// ClipsToContentBox. A clear canvas paints nothing.
func (r *Renderer) paintCanvas(layer *PaintLayer) {
	bitmap := layer.Canvas.CanvasBitmap
	if bitmap == nil {
		return
	}
	contentRect := replacedContentBox(layer.Box)
	if contentRect.Size.IsEmpty() {
		return
	}
	w, h := layer.Canvas.CanvasSize()
	paintRect := layout.ComputeReplacedContentRect(layer.ObjectFit, layer.ObjectPosition, layer.ObjectViewBox, contentRect,
		layout.MakeFixedPhysicalNaturalSizingInfo(geometry.NewPhysicalSize(w, h)))
	r.paintImageIntoRect(layer, bitmap, paintRect, imageVisualRect(layer, contentRect, paintRect))
}

// imageVisualRect is ImagePainter::PaintReplaced's visual rect
// (image_painter.cc:141-146 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the
// content box when the element ClipsToContentBox, else the paint rect.
func imageVisualRect(layer *PaintLayer, contentRect, paintRect geometry.PhysicalRect) geometry.PhysicalRect {
	if layer.ClipsToContentBox {
		return contentRect
	}
	return paintRect
}

// paintImageIntoRect draws img scaled into destRect, restricted to
// contentRect. Mirrors ImagePainter::PaintIntoRect (core/paint/
// image_painter.cc:205-302 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): when
// contentRect does not contain destRect, the pixel-snapped destination is
// intersected with the pixel-snapped content rect and the source rect mapped
// to match, and the draw clamps sampling to that source rect
// (Image::ImageClampingMode::kClampImageToSourceRect, :297-301). louis14
// scales the whole image to the pixel-snapped destination and crops that,
// which maps destination pixels to the same source points as Blink's
// mapped source rect; the bilinear filter is clamped to the source pixels
// the mapped rect covers so pixels outside it do not bleed in.
func (r *Renderer) paintImageIntoRect(layer *PaintLayer, img image.Image, destRect, contentRect geometry.PhysicalRect) {
	pixelSnappedDestRect := geometry.ToPixelSnappedRect(destRect)
	if pixelSnappedDestRect.Empty() {
		return
	}
	srcW, srcH := img.Bounds().Dx(), img.Bounds().Dy()
	drawRect := pixelSnappedDestRect
	clamp := image.Rect(0, 0, srcW, srcH)
	if !contentRect.Contains(destRect) {
		drawRect = geometry.ToPixelSnappedRect(contentRect).Intersect(pixelSnappedDestRect)
		if drawRect.Empty() {
			return
		}
		// src_rect = MapRect(pixel-snapped content ∩ dest, pixel-snapped
		// dest, full source) (image_painter.cc:230-240).
		d := pixelSnappedDestRect
		sx := float64(srcW) / float64(d.Dx())
		sy := float64(srcH) / float64(d.Dy())
		clamp = image.Rect(
			int(math.Floor(float64(drawRect.Min.X-d.Min.X)*sx)),
			int(math.Floor(float64(drawRect.Min.Y-d.Min.Y)*sy)),
			int(math.Ceil(float64(drawRect.Max.X-d.Min.X)*sx)),
			int(math.Ceil(float64(drawRect.Max.Y-d.Min.Y)*sy))).Intersect(clamp)
	}
	scaled := scaleImageClamped(img, srcW, srcH, pixelSnappedDestRect.Dx(), pixelSnappedDestRect.Dy(), layer.ImageRendering, clamp)
	finalImg := scaled.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(drawRect.Sub(pixelSnappedDestRect.Min))
	finalX, finalY := drawRect.Min.X, drawRect.Min.Y

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
// The filter's source taps are clamped to clamp (source pixel coordinates).
func scaleImageBilinear(src image.Image, srcW, srcH, dstW, dstH int, clamp image.Rectangle) image.Image {
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
		sy0, sy1 = clampInt(sy0, clamp.Min.Y, clamp.Max.Y-1), clampInt(sy1, clamp.Min.Y, clamp.Max.Y-1)

		for dx := 0; dx < dstW; dx++ {
			sx := (float64(dx)+0.5)*float64(srcW)/float64(dstW) - 0.5
			sx0 := int(math.Floor(sx))
			sx1 := sx0 + 1
			fx := sx - float64(sx0)
			sx0, sx1 = clampInt(sx0, clamp.Min.X, clamp.Max.X-1), clampInt(sx1, clamp.Min.X, clamp.Max.X-1)

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
	return scaleImageClamped(src, srcW, srcH, dstW, dstH, imageRendering, image.Rect(0, 0, srcW, srcH))
}

// scaleImageClamped is scaleImage with bilinear taps clamped to clamp
// (source pixel coordinates); nearest-neighbor sampling of a destination
// pixel never leaves the source pixels it maps onto.
func scaleImageClamped(src image.Image, srcW, srcH, dstW, dstH int, imageRendering string, clamp image.Rectangle) image.Image {
	if isNearestNeighborRendering(imageRendering) {
		return scaleImageNearest(src, srcW, srcH, dstW, dstH)
	}
	return scaleImageBilinear(src, srcW, srcH, dstW, dstH, clamp)
}

// clampInt returns v limited to [lo, hi].
func clampInt(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// drawBorders paints the layer's borders in rect, its border box: the
// border image when one is set and loads, else BoxBorderPainter
// (BoxPainterBase::PaintBorder, box_painter_base.cc:1745-1780 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) drawBorders(layer *PaintLayer, rect layout.PhysicalRect) {
	if layer.hasBorderImage() {
		if r.paintNinePieceImage(layer, rect) {
			return
		}
		// Fall through to regular borders if the image failed to load.
	}
	r.PaintBorder(layer, toLayoutRect(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height))
}

// drawColumnRuleRect paints one pixel-snapped column-rule segment through
// BoxBorderPainter::DrawBoxSide (box_border_painter.cc:2497-2507 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), as Blink's gap decoration
// painter does. horizontalRule is true when the multicol's writing mode is
// vertical, so the rule runs along the physical horizontal axis (Blink
// picks the equivalent BoxSide at bfp.cc:1897-1902: kLeft for horizontal
// writing modes, kTop otherwise). Styles css.BorderStyle cannot hold
// (groove, ridge, inset, outset) paint solid.
func (r *Renderer) drawColumnRuleRect(style string, c css.Color, x, y, w, h float64, horizontalRule bool) {
	side := BoxSideLeft
	if horizontalRule {
		side = BoxSideTop
	}
	r.DrawBoxSide(intRect{int(x), int(y), int(x+w) - int(x), int(y+h) - int(y)}, side, c, css.BorderStyle(style))
}

// drawOutline draws the CSS outline around the border-box, offset by outline-offset.
//
// Geometry (CSS UI 4 §4.3): The outline is a band whose INNER edge sits at
// `border-box ± outline-offset`, and whose OUTER edge sits another
// `outline-width` further out.
//
//	Inner-edge  = border-box + outline-offset   (the gap)
//	Outer-edge  = Inner-edge + outline-width    (the visible band)
//
// For positive offsets the outline draws outside the border-box; for negative
// offsets the inner edge moves inward into the border-box (and the band can
// extend over content/background).
//
// CSS UI §4.3 negative-offset clamp: the outside of the outline shape must
// never become smaller than `2 × outline-width` in either dimension. Without
// this clamp, a sufficiently negative offset collapses the outline; the spec
// mandates clamping the outer rectangle to a minimum of 2*outline-width.
// Mirrors Blink's OutlinePainter::ComputeOutlineRect adjustment at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Painting strategy: rectangular (non-rounded) outlines are filled as a
// single even-odd ring (outer rect minus inner rect) via fillOutlineSides,
// which avoids diagonal seam artifacts from per-side painting.

// outlinePaintRect returns the pixel-snapped rect a single-rect non-auto
// outline is drawn around: the union of the layer's outline rects, which
// drawOutline reaches only when that union is the first rect (several
// rects otherwise go through complexOutline).
func outlinePaintRect(layer *PaintLayer) (x, y, w, h float64) {
	rects := layer.outlineRects()
	u := rects[0]
	for _, r := range rects[1:] {
		u = u.Union(r)
	}
	return pixelSnap(u.Origin.X, u.Origin.Y, u.Size.Width, u.Size.Height)
}

// unionEvenIfEmpty is the smallest rect containing every rect, empty ones
// included (gfx::Rect::UnionEvenIfEmpty, as PaintOutlineRects unites a
// non-auto outline's rects, outline_painter.cc:956-967).
func unionEvenIfEmpty(rects []image.Rectangle) image.Rectangle {
	u := rects[0]
	for _, rc := range rects[1:] {
		u.Min.X, u.Min.Y = min(u.Min.X, rc.Min.X), min(u.Min.Y, rc.Min.Y)
		u.Max.X, u.Max.Y = max(u.Max.X, rc.Max.X), max(u.Max.Y, rc.Max.Y)
	}
	return u
}

// pixelSnappedOutlineRects returns outline rects pixel-snapped (Blink's
// per-rect ToPixelSnappedRect in OutlinePainter::PaintOutline).
func pixelSnappedOutlineRects(rects []geometry.RectF) []image.Rectangle {
	out := make([]image.Rectangle, 0, len(rects))
	for _, r := range rects {
		x, y, w, h := pixelSnap(r.Origin.X, r.Origin.Y, r.Size.Width, r.Size.Height)
		out = append(out, image.Rect(int(x), int(y), int(x+w), int(y+h)))
	}
	return out
}

// Blink's focus ring geometry (core/paint/outline_painter.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a 3px ring at zoom 1
// (FocusRingStrokeWidth, :38-47) drawn as two strokes of twice the inner
// width, so 2px each (:50-57, :921-929); the default corner radius is the
// ring width (DefaultFocusRingCornerRadius, :758-761).
const (
	focusRingStrokeWidth      = 3.0
	focusRingInnerStrokeWidth = focusRingStrokeWidth / 3
	focusRingOuterStrokeWidth = focusRingStrokeWidth / 3 * 2
)

// focusRingOuterColor is the outer ring's color in a light color scheme
// (PaintFocusRing, outline_painter.cc:879-880 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; #101010 in a dark one).
var focusRingOuterColor = css.Color{R: 255, G: 255, B: 255, A: 1}

// drawFocusRing paints outline-style: auto around the layer's outline rects
// as Blink's PaintFocusRing / PaintSingleFocusRing do
// (outline_painter.cc:813-930 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// for each ring, the rects are outset by its offset and united
// (rightAnglePath); an outer white ring one inner-ring width further out is
// stroked first, then the inner ring in outline-color over it, both 2px
// wide and centred on the contour (DrawPlatformFocusRing,
// platform/graphics/platform_focus_ring.cc, same SHA). A rectangular union
// is stroked as a rounded rect; otherwise a uniform corner radius rounds
// the contour's corners with the corner path effect (cornerPathEffect) and
// non-uniform radii are baked in (addCornerRadiiToPath). Not ported: dark
// color schemes (outer #101010, inner white), zoom scaling,
// native-appearance radii, and corner-shape.
func (r *Renderer) drawFocusRing(layer *PaintLayer) {
	// Blink drops empty pixel-snapped rects from a focus ring and paints
	// nothing when none is left (OutlinePainter::PaintOutlineRects,
	// outline_painter.cc:956-967).
	rects := slices.DeleteFunc(pixelSnappedOutlineRects(r.focusRingOutlineRects(layer)), image.Rectangle.Empty)
	if len(rects) == 0 {
		return
	}
	offset := int(focusRingOffset(layer))
	radii := focusRingCornerRadii(layer)
	for _, ring := range []struct {
		outset int
		color  css.Color
	}{
		{offset + int(math.Ceil(focusRingInnerStrokeWidth)), focusRingOuterColor},
		{offset, layer.OutlineColor},
	} {
		contours := rightAnglePath(rects, ring.outset, 0)
		if len(contours) == 0 {
			continue
		}
		switch radius, uniform := uniformRadius(radii); {
		case len(contours) == 1 && len(contours[0]) == 4:
			c := contours[0]
			r.buildRoundedRectPath(float64(c[0].X), float64(c[0].Y), float64(c[2].X-c[0].X), float64(c[2].Y-c[0].Y), radii)
		case uniform:
			for _, c := range contours {
				cornerPathEffect(r.dc, c, radius)
			}
		default:
			for _, c := range contours {
				addCornerRadiiToPath(r.dc, c, radii, radii)
			}
		}
		r.setColor(ring.color)
		r.dc.SetLineWidth(focusRingOuterStrokeWidth)
		r.dc.Stroke()
	}
}

// uniformRadius is FloatRoundedRect::Radii::UniformRadius: the radius when
// all four corners are the same circle.
func uniformRadius(radii css.EllipticalRadii) (float64, bool) {
	c := radii[0]
	if c.Rx != c.Ry {
		return 0, false
	}
	for _, o := range radii[1:] {
		if o != c {
			return 0, false
		}
	}
	return c.Rx, true
}

// focusRingOffset is Blink's FocusRingOffset (outline_painter.cc:59-74 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): outline-offset, pulled one
// pixel in when every border edge is at least a pixel wide.
func focusRingOffset(layer *PaintLayer) float64 {
	offset := math.Trunc(layer.OutlineOffset)
	if b := layer.Box; b != nil && min(b.Border.Top, b.Border.Right, b.Border.Bottom, b.Border.Left) >= 1 {
		offset--
	}
	return offset
}

// outlineOutsetExtent is Blink's OutlinePainter::OutlineOutsetExtent
// (outline_painter.cc:1050-1061 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// how far an outline reaches outside its outline rects. A focus ring draws
// only part of its width outside its offset; another outline its whole
// width.
func outlineOutsetExtent(layer *PaintLayer) int {
	if layer.OutlineStyle == "auto" {
		return int(focusRingOffset(layer)) + int(math.Ceil(focusRingStrokeWidth/3))*2
	}
	return max(int(layer.OutlineWidth)+int(layer.OutlineOffset), 0)
}

// focusRingCornerRadii is Blink's GetFocusRingCornerRadii for an element
// without native appearance (outline_painter.cc:763-811 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an author border-radius
// outset by outline-offset with every radius at least the ring width, else
// the ring width on every corner.
func focusRingCornerRadii(layer *PaintLayer) css.EllipticalRadii {
	ringRadius := css.EllipticalRadius{Rx: focusRingStrokeWidth, Ry: focusRingStrokeWidth}
	if !hasBorderRadius(layer) {
		return css.EllipticalRadii{ringRadius, ringRadius, ringRadius, ringRadius}
	}
	o := layer.OutlineOffset
	radii := layer.BorderRadius.Outset(o, o, o, o)
	for i := range radii {
		radii[i].Rx = math.Max(radii[i].Rx, ringRadius.Rx)
		radii[i].Ry = math.Max(radii[i].Ry, ringRadius.Ry)
	}
	return radii
}

func (r *Renderer) drawOutline(layer *PaintLayer) {
	if layer.OutlineStyle == "auto" {
		r.drawFocusRing(layer)
		return
	}
	// Several rects whose union is not the first go through
	// ComplexOutlinePainter (OutlinePainter::PaintOutlineRects,
	// outline_painter.cc:1018-1031 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the rest paint around one
	// rect below.
	if rects := pixelSnappedOutlineRects(layer.outlineRects()); unionEvenIfEmpty(rects) != rects[0] {
		r.complexOutline(layer, rects)
		return
	}
	x, y, w, h := outlinePaintRect(layer)

	width := layer.OutlineWidth
	offset := layer.OutlineOffset

	// Inner edge (the gap-edge facing the border-box).
	innerX := x - offset
	innerY := y - offset
	innerW := w + 2*offset
	innerH := h + 2*offset

	// Outer edge: extend outward by outline-width.
	outerX := innerX - width
	outerY := innerY - width
	outerW := innerW + 2*width
	outerH := innerH + 2*width

	// CSS UI §4.3: clamp the outer rectangle so that neither dimension shrinks
	// below 2*outline-width. Apply per-dimension (long boxes with extreme
	// negative offset clamp width and height independently — see outline-014/-015).
	minDim := 2 * width
	if outerW < minDim {
		// Re-center on the original box and grow to minDim.
		outerX = x + w/2 - minDim/2
		outerW = minDim
	}
	if outerH < minDim {
		outerY = y + h/2 - minDim/2
		outerH = minDim
	}

	if outerW <= 0 || outerH <= 0 || width <= 0 {
		return
	}

	r.setColor(layer.OutlineColor)

	style := layer.OutlineStyle
	switch style {
	case "solid":
		if hasBorderRadius(layer) {
			// For rounded corners, draw the outline as a filled ring between
			// the outer rounded rect and an inner rounded rect inset by
			// outline-width. Use even-odd fill so the inner shape punches
			// the outline interior cleanly.
			outerRadii := layer.BorderRadius.Outset(offset+width, offset+width, offset+width, offset+width)
			innerRadii := layer.BorderRadius.Outset(offset, offset, offset, offset)
			r.buildRoundedRectPath(outerX, outerY, outerW, outerH, outerRadii)
			iX := outerX + width
			iY := outerY + width
			iW := outerW - 2*width
			iH := outerH - 2*width
			if iW > 0 && iH > 0 {
				r.buildRoundedRectPath(iX, iY, iW, iH, innerRadii)
			}
			r.dc.SetFillRule(textshape.FillRuleEvenOdd)
			r.dc.Fill()
			r.dc.SetFillRule(textshape.FillRuleWinding)
		} else {
			// Rectangular outline as a single even-odd ring.
			r.fillOutlineSides(outerX, outerY, outerW, outerH, width)
		}
	case "dashed", "dotted", "double":
		// Non-solid single-rect outlines paint as a border through
		// BoxBorderPainter::PaintSingleRectOutline (outline_painter.cc:
		// 1018-1026 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), unless the
		// CSS UI minimum-size clamp moved the rect; then each side's run
		// spans the full outer edge with its centerline half a band inside.
		if outerW > minDim && outerH > minDim {
			ro, wi := layoutunit.FromFloat64Round(offset), layoutunit.FromFloat64Round(width)
			r.paintSingleRectOutline(layer.BorderRadius, toLayoutRect(x, y, w, h), int(wi.Round()),
				geometry.PhysicalBoxStrut{Top: ro, Right: ro, Bottom: ro, Left: ro}, layer.OutlineColor, css.BorderStyle(style))
			return
		}
		if style == "double" {
			outerR, outerB := outerX+outerW, outerY+outerH
			midT, midB := outerY+width/2, outerB-width/2
			midL, midR := outerX+width/2, outerR-width/2
			r.drawDoubleLine(outerX, midT, outerR, midT, width) // top
			r.drawDoubleLine(midR, outerY, midR, outerB, width) // right
			r.drawDoubleLine(outerX, midB, outerR, midB, width) // bottom
			r.drawDoubleLine(midL, outerY, midL, outerB, width) // left
			return
		}
		bs := css.BorderStyleDashed
		if style == "dotted" {
			bs = css.BorderStyleDotted
		}
		l, t, wi := int(outerX), int(outerY), int(width)
		rt, b := int(outerX+outerW), int(outerY+outerH)
		r.drawDashedOrDottedBoxSide(l, t, rt, t+wi, BoxSideTop, layer.OutlineColor, wi, bs)
		r.drawDashedOrDottedBoxSide(rt-wi, t, rt, b, BoxSideRight, layer.OutlineColor, wi, bs)
		r.drawDashedOrDottedBoxSide(l, b-wi, rt, b, BoxSideBottom, layer.OutlineColor, wi, bs)
		r.drawDashedOrDottedBoxSide(l, t, l+wi, b, BoxSideLeft, layer.OutlineColor, wi, bs)
	default:
		// Treat unknown / not-yet-implemented styles (groove/ridge/inset/outset)
		// as solid for visibility — matches Blink's fallback behavior for
		// outline-style values that don't have a distinct platform rendering.
		r.fillOutlineSides(outerX, outerY, outerW, outerH, width)
	}
}

// fillOutlineSides paints a rectangular outline as a single even-odd filled ring,
// with the outer rectangle and inner hole (inset by width on all sides).
// This mirrors the rounded-corner branch and eliminates the diagonal seams
// that resulted from painting 4 separate trapezoid sides.
// Blink analog: OutlinePainter::PaintOutline @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
func (r *Renderer) fillOutlineSides(outerX, outerY, outerW, outerH, width float64) {
	outerL := outerX
	outerT := outerY
	outerR := outerX + outerW
	outerB := outerY + outerH
	innerL := outerL + width
	innerT := outerT + width
	innerR := outerR - width
	innerB := outerB - width
	if innerL > innerR {
		innerL, innerR = (outerL+outerR)/2, (outerL+outerR)/2
	}
	if innerT > innerB {
		innerT, innerB = (outerT+outerB)/2, (outerT+outerB)/2
	}

	// Build outer rectangle path (CW).
	r.dc.MoveTo(outerL, outerT)
	r.dc.LineTo(outerR, outerT)
	r.dc.LineTo(outerR, outerB)
	r.dc.LineTo(outerL, outerB)
	r.dc.ClosePath()

	// Build inner rectangle path (CCW) for even-odd hole punch.
	if innerR-innerL > 0 && innerB-innerT > 0 {
		r.dc.MoveTo(innerL, innerT)
		r.dc.LineTo(innerL, innerB)
		r.dc.LineTo(innerR, innerB)
		r.dc.LineTo(innerR, innerT)
		r.dc.ClosePath()
	}

	// Fill as a single even-odd ring.
	r.dc.SetFillRule(textshape.FillRuleEvenOdd)
	r.dc.Fill()
	r.dc.SetFillRule(textshape.FillRuleWinding)
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

// premultipliedRGBA converts a straight-alpha css.Color to an alpha-
// premultiplied color.RGBA.
//
// mazarin's DrawContext rasterizer composites fill colours through Go's
// color.RGBA, whose RGBA() contract is alpha-PREMULTIPLIED: it scales the
// stored channels to 16-bit WITHOUT re-applying alpha, and the source-over
// blend (textshape draw_impl.go fillWithPattern) computes src + dst*(1-a)
// directly. Handing it a straight-alpha colour (full R,G,B with A<255)
// therefore omits the src*a term and overflows uint8 when src+dst*(1-a) > 255
// — e.g. a translucent background painted over already-opaque content (the
// inverted backdrop of a backdrop-filter element) wraps to garbage. Storing
// the premultiplied value makes the rasterizer's arithmetic the correct
// Porter-Duff over. Opaque colours (A==255) are unchanged since c*255/255==c.
func premultipliedRGBA(c css.Color) color.RGBA {
	a := uint8(c.A * 255)
	return color.RGBA{
		R: uint8(uint32(c.R) * uint32(a) / 255),
		G: uint8(uint32(c.G) * uint32(a) / 255),
		B: uint8(uint32(c.B) * uint32(a) / 255),
		A: a,
	}
}

// setColor sets the draw context color from a css.Color.
func (r *Renderer) setColor(c css.Color) {
	r.dc.SetColor(premultipliedRGBA(c))
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
		return fmt.Sprintf("%d", index) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeDecimalLeadingZero:
		return fmt.Sprintf("%02d", index) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeLowerAlpha, css.ListStyleTypeLowerLatin:
		return css.ToAlpha(index) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeUpperAlpha, css.ListStyleTypeUpperLatin:
		return strings.ToUpper(css.ToAlpha(index)) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeLowerRoman:
		return strings.ToLower(css.ToRoman(index)) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeUpperRoman:
		return css.ToRoman(index) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeLowerGreek:
		return css.ToGreek(index) + css.DefaultCounterStyleSuffix
	case css.ListStyleTypeDisclosureOpen:
		return "\u25BE" // ▾ downward-pointing triangle
	case css.ListStyleTypeDisclosureClosed:
		return "\u25B8" // ▸ right-pointing triangle
	default:
		// Check custom @counter-style rules.
		if r != nil && r.counterStyles != nil {
			if cs, ok := r.counterStyles[string(lst)]; ok {
				return css.ApplyCounterStyle(index, cs, r.counterStyles)
			}
		}
		// Return custom string list-style-type values as-is.
		// These are CSS <string> values from e.g. list-style-type: "\2022".
		// Per CSS Lists §3, a <string> value is used as the marker text.
		return string(lst)
	}
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
//
// LOU-367: this baseline round chooses the integer device row the glyph mask
// lands on. It pairs with mazzy's RenderGlyph grid-fit (rasterize.go): the
// rasterizer now bakes the mask with the exact fractional top edge as the y
// origin (DrMinY rounded), so a full-em Ahem glyph lands on whole rows with no
// fractional α≈0.804/0.2 seam (previously the floored outline bounds baked the
// seam into the mask; e.g. multicol-rule-px-001). See
// TestAhemGlyphNoFractionalSeam for the executable guard.
func (r *Renderer) drawTextStr(text string, fontID int32, x, y float64, in fontFeatureInputs) {
	y = math.Round(y)
	features := r.shapeFeatures(fontID, in)
	if len(features) > 0 {
		r.dc.DrawTextWithFeatures(text, fontID, x, y, features)
	} else {
		r.dc.DrawText(text, fontID, x, y)
	}
}

// measureTextStr measures a text string, applying OpenType features if present.
func (r *Renderer) measureTextStr(text string, fontID int32, in fontFeatureInputs) float64 {
	features := r.shapeFeatures(fontID, in)
	if len(features) > 0 {
		return r.dc.MeasureTextWithFeatures(text, fontID, features)
	}
	return r.dc.MeasureText(text, fontID)
}

// familyDeclaredViaFontFace reports whether any family name in the CSS
// `font-family` list was declared via @font-face — i.e. resolves through
// the FontRegistry rather than a UA built-in or system fallback. Used as
// the louis14 heuristic for "the active face may carry native OpenType
// features" (small-caps GSUB lookups) when deciding whether to also run
// the synthesized small-caps fallback.
func (r *Renderer) familyDeclaredViaFontFace(family string) bool {
	if r.fonts.Registry == nil {
		return false
	}
	for _, fam := range text.ParseFontFamilyList(family) {
		if r.fonts.Registry.IsDeclared(fam) {
			return true
		}
	}
	return false
}

// shouldFlipUnderlineAndOverline reports whether a central-baseline
// (upright or mixed vertical) run's underline resolves to the over side,
// which swaps its underline and overline: Blink's ResolveUnderlinePosition
// central branch (core/paint/text_decoration_info.cc:23-53 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and flip_underline_and_overline_
// (:224-235). Japanese (Katakana/Hiragana) or Korean script resolves over
// unless text-underline-position has left; any other script only when it has
// right. left and right name the same sides in vertical-rl and vertical-lr:
// Blink rotates both clockwise, so line-over is physical right in both.
func shouldFlipUnderlineAndOverline(layer *PaintLayer) bool {
	if !layer.IsUprightVertical {
		return false
	}
	pos := layer.TextUnderlinePosition
	if layer.TextScriptIsKanaOrHangul {
		return !strings.Contains(pos, "left")
	}
	return strings.Contains(pos, "right")
}

// isKanaOrHangulLocale reports whether a BCP47 language tag's primary
// subtag maps to USCRIPT_KATAKANA_OR_HIRAGANA (ja) or USCRIPT_HANGUL (ko),
// the scripts ResolveUnderlinePosition flips for
// (core/paint/text_decoration_info.cc:41-47 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); Blink takes the script from
// the content language (FontDescription::GetScript). Han (zh) and
// Mongolian (mn) don't flip: Chrome keeps a lang=zh underline on the under
// side. Case-insensitive; region and script subtags are ignored.
func isKanaOrHangulLocale(lang string) bool {
	if i := strings.IndexByte(lang, '-'); i >= 0 {
		lang = lang[:i]
	}
	switch strings.ToLower(lang) {
	case "ja", "ko":
		return true
	}
	return false
}

// sidewaysUnderlineGoesRight reports whether the underline for a rotated
// (IsSidewaysRL or IsSidewaysLR) text run should paint on the physical RIGHT
// side of the line column. Per CSS Text Decor 4 §3.3:
//
//   - Default (auto): follows the rotated alphabetic baseline's under
//     direction. CW rotation (vertical-rl/-lr mixed, sideways-rl) puts the
//     under-side on physical LEFT (rotated baseline runs vertically with
//     "below baseline" mapped to physical -X). CCW rotation (sideways-lr)
//     puts it on physical RIGHT.
//
//   - `text-underline-position: right` / `left`: per CSS Text Decor 3 §3.5
//     these keywords apply ONLY in upright/mixed vertical writing modes
//     (central-baseline). In sideways modes the line is laid out horizontally
//     and the entire line column is then rotated by the canvas, so left/right
//     are spec'd to have no effect — the underline stays on the rotated-
//     baseline's natural under-side. Mirrors Blink's gating on
//     GetFontBaseline() == kCentralBaseline in ResolveUnderlinePosition() at
//     third_party/blink/renderer/core/paint/text_decoration_info.cc:13-39
//     @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// louis14 doesn't yet type the property; the raw string lives on
// PaintLayer.TextUnderlinePosition and we do substring-contains checks here.
// Only true sideways runs (sideways-rl / sideways-lr /
// vertical-*+text-orientation:sideways) reach it: central-baseline vertical
// runs, which louis14 also marks IsSidewaysRL (engine.go:374), decorate
// line-relative (drawLineRelativeTextDecoration).
func sidewaysUnderlineGoesRight(layer *PaintLayer) bool {
	pos := layer.TextUnderlinePosition
	// `under` keyword: follows block-end direction.
	//   vertical-rl / sideways-rl: block-end = LEFT (block progresses to left).
	//   vertical-lr / sideways-lr: block-end = RIGHT.
	if strings.Contains(pos, "under") {
		return layer.IsWritingModeVerticalLR || layer.IsSidewaysLR
	}
	// auto / from-font / empty (and left|right, ignored here): follow
	// rotation direction. CCW (IsSidewaysLR) → RIGHT; CW → LEFT.
	return layer.IsSidewaysLR
}

// drawText paints a text fragment, splitting it into pre-selection /
// selected / post-selection segments when the LOU-344 selection context
// (Renderer.SetSelectionContext) has an active Range overlapping this
// fragment's originating text node. Each segment recurses into
// drawTextSegment with its own (possibly color-overridden) shallow-copied
// Box+PaintLayer — see selection_paint.go's computeSelectionSegments for
// the split computation and pkg/render/selection_paint.go's file doc
// comment for why this happens at paint time rather than via pkg/layout
// plumbing.
func (r *Renderer) drawText(layer *PaintLayer) {
	box := layer.Box
	if box == nil {
		return
	}

	segs := r.computeSelectionSegments(box, box.Text)
	if len(segs) == 1 && !segs[0].selected {
		r.drawTextSegment(layer)
		return
	}

	fontID := r.fontIDForLayer(layer)
	if fontID < 0 {
		// Mirrors drawTextSegment's own openFont-failure guard (further
		// below in this file) — without it, the measurement calls below
		// run with an invalid font ID before that guard would ever fire.
		return
	}
	// starts[i] is segment i's advance from the fragment start, each the
	// last one's plus that segment's own advance; the last entry is the
	// fragment's own inline size, where the last segment ends. A tab's
	// advance depends on where it sits, so a segment holding one is
	// measured as the whole prefix up to its end.
	starts := make([]float64, len(segs)+1)
	for i, seg := range segs[:len(segs)-1] {
		if text := box.Text[seg.start:seg.end]; strings.Contains(text, "\t") {
			starts[i+1] = r.measureSegmentAdvanceWithTabs(box.Text[:seg.end], box, fontID, layer)
		} else {
			starts[i+1] = starts[i] + r.measureSegmentAdvance(text, fontID, layer)
		}
	}
	starts[len(segs)] = layer.decorationInlineSize()
	for i, seg := range segs {
		segBox := *box
		segBox.Text = box.Text[seg.start:seg.end]
		prefixAdvance := starts[i]
		// stretch is this segment's own stretch of the fragment's inline
		// size, the last one reaching the fragment's end: what paints
		// across the inline size (decorations) paints each stretch once.
		stretch := starts[i+1] - prefixAdvance
		// The prefix advance offsets this segment along the ADVANCE axis:
		// physical X for horizontal text, physical Y for vertical text
		// (drawTextSegment's own IsVerticalText branch advances box.Y by
		// layer.FontSize per character, render.go:6918-6932; its sideways
		// branches likewise treat box.Y as the advance origin — see
		// CHECKPOINT_STATUS.md/LOU-347's Blink-divergence note for why
		// drawTextSegment, not Blink's HighlightPainter, is this fix's
		// reference pattern). box.Height (not box.Width) carries the
		// advance-axis extent under any vertical writing mode, per
		// pkg/layout/writing_mode_converter.go's ToPhysicalSize. The box
		// keeps its size, so the glyph buffers it sizes still hold the
		// glyphs' tails.
		if layer.IsVerticalText {
			segBox.Y = box.Y + prefixAdvance
		} else {
			segBox.X = box.X + prefixAdvance
		}

		// selectionRectCrossAxis: Blink's selection rect, the text
		// fragment united with its line box in the block direction. Cross
		// axis = Y/Height for horizontal text, X/Width for vertical text
		// (box.Width, not box.Height, carries the cross/block-progression
		// extent under any vertical writing mode — see this loop's
		// segBox.Y/segBox.X dispatch above).
		crossOrigin, crossExtent := selectionRectCrossAxis(box, layer.IsVerticalText)
		segLayer := *layer
		segLayer.Box = &segBox
		segLayer.segmentInlineSize, segLayer.hasSegmentInlineSize = stretch, true
		segLayer.AppliedTextDecorations = rebaseDecoratingBoxes(layer.AppliedTextDecorations, prefixAdvance)
		if seg.selected {
			// Background rect behind the selected glyphs, painted BEFORE
			// the recursive drawTextSegment call so glyphs/decorations
			// land on top — same paint order as a normal element
			// background-then-foreground (drawBackground runs in
			// PhaseBackground, text in PhaseForeground).
			if seg.backgroundColor != nil && seg.backgroundColor.A > 0 {
				r.setColor(*seg.backgroundColor)
				r.dc.DrawRectangle(r.selectionBackgroundRect(box, &segBox, seg, fontID, layer, crossOrigin, crossExtent))
				r.dc.Fill()
			}
			if seg.textColor != nil || seg.decorationColor != nil {
				// ::selection { text-decoration-color: ... } alone (no
				// `color`) must still recolor the decoration — fall back
				// to the segment's own current TextColor so
				// setLayerColorOverride applies decorationColor without
				// also overriding the (unauthored) text color.
				textColor := segLayer.TextColor
				if seg.textColor != nil {
					textColor = *seg.textColor
				}
				r.setLayerColorOverride(&segLayer, textColor, seg.decorationColor)
			}
			if seg.hasOwnDecoration {
				applySelectionOwnDecoration(&segLayer, seg.decorationLine)
				// The newly-introduced decoration must paint in
				// ::selection's own color (set above when textColor !=
				// nil; when ::selection set decoration but not color,
				// TextDecorationColor already defaults to currentColor
				// via paint_layer.go's normal resolution, which is
				// correct since GetTextDecorationColor's own currentColor
				// fallback resolves against the (possibly UA-default)
				// ::selection color computed by ComputePseudoElementStyle
				// — see resolveSelectionPseudoStyle).
			}
			if seg.hasOwnTextShadow {
				// LOU-349: a highlight pseudo-element's OWN text-shadow
				// (target-text-shadow-horizontal/-vertical.html) PAINTS
				// ALONGSIDE the originating element's own shadows, rather
				// than replacing them — confirmed against
				// target-text-shadow-horizontal-ref.html, whose reference
				// <span> declares text-shadow as a TWO-shadow comma-list
				// where the second shadow is byte-identical to the
				// originating <p>'s own text-shadow value, and the test's
				// own meta[name=assert] states "::target-text with a
				// shadow is painted, INCLUDING originating element
				// shadows". Put the highlight's own shadow(s) at the FRONT
				// of the merged slice (drawTextShadows iterates len()-1
				// down to 0, so index 0 paints LAST/on-top) so it ends up
				// topmost, matching CSS's "first-specified shadow is on
				// top" layering rule (https://drafts.csswg.org/css-text-decor-4/#text-shadow-property)
				// applied to the reference's declared order (highlight
				// shadow first in the comma-list, origin shadow second —
				// i.e. highlight on top). Verified empirically: swapping
				// this order does NOT change target-text-shadow-horizontal's
				// reftest result (its fuzzy tolerance masks the visual
				// difference), so this comment — not a reftest — is the
				// source of truth for why this order was chosen; don't
				// "fix" it based on a plausible-sounding but unverified
				// re-read (a CodeRabbit review on this PR proposed exactly
				// that swap; verified against the CSS spec and rejected).
				// seg.textShadows may be nil/empty
				// (`text-shadow: none` on the highlight) — appending
				// nothing to segLayer.TextShadows in that case correctly
				// leaves only the originating shadows, which IS the
				// "highlight explicitly clears its own shadow but doesn't
				// suppress the origin's" interpretation consistent with
				// this additive model (no LOU-349 target test exercises
				// `text-shadow: none` on a highlight pseudo, so this
				// specific sub-case is inferred from the additive model
				// rather than directly verified).
				segLayer.TextShadows = append(append([]css.TextShadow{}, seg.textShadows...), segLayer.TextShadows...)
			}
		}
		// Clip this segment's glyph/decoration paint to its own ADVANCE-axis
		// span (LOU-348) — see segmentClipRect's doc comment for the full
		// Blink-mirroring rationale.
		clipMinX, clipMinY, clipMaxX, clipMaxY := segmentClipRect(
			segBox, stretch, crossOrigin, crossExtent, segLayer.TextShadows,
			layer.IsVerticalText, layer.FontSize, i == 0, i == len(segs)-1)
		r.dc.Push()
		r.dc.DrawRectangle(clipMinX, clipMinY, clipMaxX-clipMinX, clipMaxY-clipMinY)
		r.dc.Clip()
		r.drawTextSegment(&segLayer)
		r.dc.Pop()
	}
}

// rebaseDecoratingBoxes returns tds with each decorating box's offset
// measured from a fragment start advance further along, as a selection
// segment's box starts: the boxes stay where the fragment put them.
func rebaseDecoratingBoxes(tds []css.AppliedTextDecoration, advance float64) []css.AppliedTextDecoration {
	if advance == 0 || !slices.ContainsFunc(tds, func(td css.AppliedTextDecoration) bool { return td.HasDecoratingBox }) {
		return tds
	}
	rebased := slices.Clone(tds)
	for i := range rebased {
		if rebased[i].HasDecoratingBox {
			rebased[i].DecoratingBoxOffsetX -= advance
		}
	}
	return rebased
}

// selectionBackgroundRect is the pixel-snapped ::selection background rect
// of a selected segment whose box is segBox, cut from the fragment box.
func (r *Renderer) selectionBackgroundRect(box, segBox *layout.Box, seg selectionSegment, fontID int32, layer *PaintLayer, crossOrigin, crossExtent float64) (sx, sy, sw, sh float64) {
	// measureSegmentAdvance (not the plain measureTextStr) mirrors
	// drawTextSegment's own per-character advance loop under
	// letter-spacing/word-spacing — plain string measurement ignores
	// the extra per-character gap drawTextSegment's letter-spacing path
	// inserts, which under-measured the highlight rect down to isolated
	// per-glyph boxes instead of one continuous band spanning the
	// intercharacter spacing too (selection-intercharacter-011/-012's
	// whole point: the spacing itself must be highlighted, per CSS
	// Pseudo-4 §highlight-bounds). CSS Text 3 §4.2: a tab character's
	// highlighted width is its EXPANDED tab-stop span, not its raw glyph
	// advance — see measureSegmentVisualWidthWithTabs's doc comment
	// (active-selection-063.html).
	var segWidth float64
	if strings.Contains(segBox.Text, "\t") {
		segWidth = r.measureSegmentVisualWidthWithTabs(segBox, fontID, layer)
	} else {
		segWidth = r.measureSegmentVisualWidth(segBox.Text, fontID, layer)
	}
	// Pixel-snap exactly like drawBackground's element-background rect
	// (pixelSnap, used throughout render.go for backgrounds/borders) so
	// the per-segment rect lands on the same pixel grid as an
	// equivalent real background-color box — without this, sub-pixel
	// rounding differences between this rect and the WPT reference's
	// actual `background-color` <div> box left a 1px red fringe on two
	// edges (active-selection-012).
	//
	// Under a vertical writing mode the segment's measured advance-axis
	// extent (segWidth, despite the horizontal-sounding name — it's the
	// segment's length along whichever axis is "inline" here) becomes
	// the rect's HEIGHT, and the cross-axis extent becomes its WIDTH,
	// mirroring drawTextSegment's own physical-axis swap for vertical
	// text (box.Width=lineHeight/cross, box.Height=textAdvance/inline —
	// see drawTextSegment's sideways branch).
	advanceOrigin, advanceExtent := segBox.X, segWidth
	if layer.IsVerticalText {
		advanceOrigin = segBox.Y
	}
	if seg.selectsSoftLineBreak {
		advanceOrigin, advanceExtent = r.widenForSoftLineBreak(box, fontID, advanceOrigin, advanceExtent)
	}
	if layer.IsVerticalText {
		sx, sy, sw, sh = pixelSnap(crossOrigin, advanceOrigin, crossExtent, advanceExtent)
	} else {
		sx, sy, sw, sh = pixelSnap(advanceOrigin, crossOrigin, advanceExtent, crossExtent)
	}
	return sx, sy, sw, sh
}

// widenForSoftLineBreak widens a selected segment's background extent
// along the advance axis by the font's space width at its inline end, so a
// selection running past a soft line break visibly reaches it
// (ExpandedSelectionRectForSoftLineBreakIfNeeded,
// core/layout/inline/inline_cursor.cc:38-54 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b, which grows the logical inline
// size before the rect is snapped; SpaceWidth is the space glyph's advance,
// without letter- or word-spacing). The inline end is the advance-axis end
// of LTR text and its start for RTL text.
func (r *Renderer) widenForSoftLineBreak(box *layout.Box, fontID int32, origin, extent float64) (float64, float64) {
	space := r.dc.MeasureText(" ", fontID)
	if layout.DirectionFromLevel(box.BidiLevel) == layout.DirectionRTL {
		origin -= space
	}
	return origin, extent + space
}

// segmentClipRect computes the integer-pixel clip bounds (minX, minY, maxX,
// maxY) for one drawText segment's glyph/decoration paint (LOU-348),
// mirroring Blink's HighlightPainter::ClipToPartRect
// (third_party/blink/renderer/core/paint/highlight_painter.cc @ Chromium
// main, 2026-06-30): each highlight "part" (a character range with one
// resolved paint style, from HighlightOverlay::ComputeParts) is painted
// from the SAME shaped line run but clipped to its own part rect before its
// text is drawn — without that clip, a glyph whose rasterized ink overhangs
// past its own part's advance box (e.g. an italic integral's hooked
// terminal) bleeds into the ADJACENT part's paint, which here painted a
// different color moments later and silently overwrote pixels the earlier
// part's color (here, ::selection's `color: transparent`) should have left
// alone. Confirmed via glyph-run dump: selection-background-painting-
// order.html's unselected "∫∫" run positions its first glyph at a NEGATIVE
// relative X (HarfBuzz left-side-bearing overhang for this italic glyph),
// landing squarely inside the PRECEDING selected "ii∫" segment's highlight
// rect and painting solid opaque orange there instead of leaving the
// transparent segment's (correctly invisible) pixels alone.
//
// segBox is this segment's box, its advance-axis origin at the segment's
// start; stretch is the segment's stretch of the fragment's inline size
// (drawText's starts), so neighbouring segments' clips meet with no gap
// between them where one's trailing letter- or word-spacing lies;
// crossOrigin/crossExtent are the cross-axis origin/extent
// (selectionRectCrossAxis's result, shared with the highlight
// fill rect); shadows is the segment's own resolved text-shadow list (nil
// if none); isFirst/isLast mark the first/last segment of the fragment.
//
// The CROSS axis is deliberately left unclipped (padded a full fontSize
// past crossOrigin/crossExtent) rather than clipping tightly to it: Blink's
// own clip derives its cross-axis bound from the fragment's InkOverflowRect
// (actual rasterized ink bounds, which for an italic ascender/descender
// legitimately exceeds the CSS em-box), not from the fragment/line-box
// geometry selectionRectCrossAxis computes for sizing the
// highlight FILL rect. This engine has no per-fragment ink-overflow
// tracking to mirror that exactly; reusing the tight fragment rect here (as
// observed before LOU-959, when the fill rect was the em box)
// clipped off genuine ascender antialiasing at the very top of this test's
// italic glyphs (confirmed empirically: y=74-76 antialiasing pixels
// disappeared when the cross axis was clipped to crossOrigin/crossExtent).
// Since the bug being fixed is purely an ADVANCE-axis bleed between
// segments, padding the cross axis by a full font-size in each direction
// comfortably covers any realistic glyph overshoot without the precision a
// real ink-overflow rect would give.
//
// text-shadow (CSS Text Decor 4 §text-shadow-property) has no bound
// relative to the glyph box on EITHER axis — an author can offset a shadow
// arbitrarily far in any direction (highlight-painting-currentcolor-004's
// `0 4em` vertical offset, target-text-shadow-horizontal's `0.25em 0.25em`
// diagonal offset are both real target tests). shadowPad is this segment's
// own required margin so its shadows are never clipped, REGARDLESS of
// whether this segment sits at the fragment's outer edge or between two
// other segments — a shadow is its own explicit paint geometry the author
// specified, not glyph ink that might bleed into a neighbor, so unlike the
// glyph-overhang case below it always gets full room on every side.
// Confirmed empirically: without this, highlight-painting-currentcolor-
// 004/-004a/-004b and target-text-shadow-horizontal regressed from PASS to
// FAIL (their shadows landed outside a smaller, outer-edge-only pad and
// were clipped away).
//
// The ADVANCE axis is ALSO padded outward at the fragment's own outer
// edges (isFirst's leading edge, isLast's trailing edge) even with no
// text-shadow — Blink's ClipToPartRect does the same ("expanded slightly
// at the far left or right end of a fragment"), since a glyph's natural
// overhang at the very start/end of the WHOLE fragment has no adjacent
// segment to bleed into, so there is nothing to protect against by
// clipping it. Confirmed empirically: without this widening, the
// unselected "∫∫" run's own trailing glyph's top-hook overhang (past
// the last segment's end) was clipped away. INTERIOR
// boundaries stay tight at shadowPad (0 when this segment has no shadow) —
// that boundary is exactly where this bug's cross-segment glyph-ink bleed
// happens and must stay clipped absent a shadow's own wider requirement.
//
// The advance-axis origin/extent used below is segBox.X/stretch — the
// SAME raw (non-pixel-snapped) values drawTextStr positions this segment's
// glyphs from (drawTextStr's own doc comment: "X is passed through
// fractionally so the rasterizer's subpixel pen carry... produces
// consistent glyph positioning") — NOT a pixelSnap'd rect (as used for the
// highlight FILL rect). Using a pixel-snapped rect here misaligned the clip
// boundary from the glyph rasterizer's own sub-pixel grid by a fraction of
// a pixel, clipping one extra antialiased fringe pixel at every INTERIOR
// segment boundary vs. an unclipped reference (highlight-painting-
// currentcolor-003a's 13-pixel regression, confirmed empirically:
// switching this from a pixel-snapped rect to segBox.X and the segment's
// extent took it to
// 0 diff pixels with no change to the main LOU-348 target's result).
//
// Finally, mazzy's DrawContextImpl.Clip fast-paths an axis-aligned
// rectangle via int(rect[...]), which TRUNCATES toward zero rather than
// rounding — a fractional-pixel clip max (e.g. 45.8125) silently became
// 45, excluding a pixel this clip was meant to include (highlight-
// painting-currentcolor-003a's last remaining diff pixel: the trailing
// edge of an interior segment's own underline landed at x=45.81, truncated
// to clipRect.Max.X=45, dropping pixel 45 itself). Rounding the min corner
// down and the max corner up guarantees the integer clip rect always
// CONTAINS the intended fractional span, never truncates into it.
func segmentClipRect(segBox layout.Box, stretch, crossOrigin, crossExtent float64, shadows []css.TextShadow, isVerticalText bool, fontSize float64, isFirst, isLast bool) (minX, minY, maxX, maxY float64) {
	shadowPad := maxTextShadowExtent(shadows)
	pad := max(fontSize, shadowPad)
	advStart, advEnd := shadowPad, shadowPad
	if isFirst {
		advStart = pad * 4
	}
	if isLast {
		advEnd = pad * 4
	}
	var clipX, clipY, clipW, clipH float64
	if isVerticalText {
		clipX, clipY, clipW, clipH = crossOrigin-pad, segBox.Y-advStart, crossExtent+2*pad, stretch+advStart+advEnd
	} else {
		clipX, clipY, clipW, clipH = segBox.X-advStart, crossOrigin-pad, stretch+advStart+advEnd, crossExtent+2*pad
	}
	return math.Floor(clipX), math.Floor(clipY), math.Ceil(clipX + clipW), math.Ceil(clipY + clipH)
}

// maxTextShadowExtent returns the largest absolute reach (offset magnitude
// plus blur) of any shadow in shadows, along either axis — the minimum
// symmetric padding a clip rect needs on every side to avoid cutting off a
// text-shadow painted alongside this segment's glyphs (LOU-348's
// drawText segment-clip; see that call site's doc comment). Returns 0 for
// an empty/nil slice, so callers fall back to their own non-shadow default
// padding unchanged.
func maxTextShadowExtent(shadows []css.TextShadow) float64 {
	var extent float64
	for _, s := range shadows {
		extent = max(extent, math.Abs(s.OffsetX)+s.Blur, math.Abs(s.OffsetY)+s.Blur)
	}
	return extent
}

// fontIDForLayer opens (or reuses, via the font cache) the font this
// layer's text would be drawn with. Factored out of drawTextSegment so
// drawText's segment-splitting path can measure segment widths with the
// exact same font selection logic before delegating the actual glyph
// paint — avoids re-deriving the font path/ID twice with any risk of
// drift between the two call sites.
func (r *Renderer) fontIDForLayer(layer *PaintLayer) int32 {
	return r.openFont(r.fontPathForLayer(layer), layer.FontSize)
}

// resolveFontFeatureSettingsDescriptor keeps the descriptor features whose
// tag the font-feature-settings property does not name. Mirrors Blink's
// ResolveFontFeatureSettingsDescriptor (platform/fonts/
// resolved_font_features.cc:14-43 @ 5992657f556bcd9d207a4aca187488462f027cab).
func resolveFontFeatureSettingsDescriptor(existing, descriptor []textshape.FontFeature) []textshape.FontFeature {
	var resolved []textshape.FontFeature
	for _, f := range descriptor {
		if !slices.ContainsFunc(existing, func(e textshape.FontFeature) bool { return e.Tag == f.Tag }) {
			resolved = append(resolved, f)
		}
	}
	return resolved
}

// fontPathForLayer is the font file this layer's text is drawn with — the
// one font-selection rule every layer-based paint path shares.
func (r *Renderer) fontPathForLayer(layer *PaintLayer) string {
	return r.fonts.FontPathForFamilyWithSynthesis(
		layer.FontFamily, layer.FontBold, layer.FontItalic,
		layer.FontMono, layer.FontAhem,
		layer.FontSynthesisWeight, layer.FontSynthesisStyle)
}

// measureSegmentAdvance measures the inline-axis advance of text exactly
// as drawTextSegment's own letter-spacing/word-spacing branch (further
// below in this file) would draw it — i.e. INCLUDING the per-character
// layer.LetterSpacing gap after every character (and layer.WordSpacing
// after every space). Plain measureTextStr only measures glyph advances,
// which under letter-spacing under-counts the true on-screen width drawn
// by the character-by-character loop; drawText's segment-splitting (used
// to position later segments and to size the ::selection background rect)
// needs the same total advance the draw loop actually produces, or the
// background rect and the next segment's X both land short — see
// selection-intercharacter-011/-012's comment at this function's call
// site. Falls back to plain measureTextStr when letter-spacing and
// word-spacing are both zero (the overwhelmingly common case), since the
// per-character loop and the whole-string measurement agree exactly there
// — using the cheaper path keeps every non-letter-spacing target test's
// hot path unaffected by this addition.
func (r *Renderer) measureSegmentAdvance(text string, fontID int32, layer *PaintLayer) float64 {
	return r.measureSegmentAdvanceTrailing(text, fontID, layer, true)
}

// measureSegmentVisualWidth is measureSegmentAdvance without the FINAL
// character's trailing letter-spacing/word-spacing gap — the highlight
// rect for a selected segment must not bleed into the gap between it and
// the next (unselected) character, even though that same gap WOULD be
// included when this segment is merely a prefix used to position a LATER
// segment's X (see measureSegmentAdvance's doc comment). Confirmed against
// selection-intercharacter-011's WPT reference: the yellow ::selection
// background ends exactly at the last selected glyph's trailing edge, not
// one letter-spacing gap past it.
func (r *Renderer) measureSegmentVisualWidth(text string, fontID int32, layer *PaintLayer) float64 {
	return r.measureSegmentAdvanceTrailing(text, fontID, layer, false)
}

// measureSegmentAdvanceTrailing is the shared implementation behind
// measureSegmentAdvance (includeTrailing=true) and
// measureSegmentVisualWidth (includeTrailing=false, drops the final
// character's own trailing spacing contribution). Plain measureTextStr
// only measures glyph advances, which under letter-spacing under-counts
// the true on-screen width drawn by drawTextSegment's character-by-
// character loop; falls back to it directly when letter-spacing and
// word-spacing are both zero (the overwhelmingly common case, where the
// per-character loop and the whole-string measurement agree exactly) to
// keep every non-letter-spacing target test's hot path unaffected.
func (r *Renderer) measureSegmentAdvanceTrailing(text string, fontID int32, layer *PaintLayer, includeTrailing bool) float64 {
	if text == "" {
		return 0
	}
	// Upright vertical text (IsVerticalText, NOT the sideways-collapsed
	// case — see drawTextSegment's IsVerticalText branch, render.go:
	// ~6953-6967) advances each character by its upright advance along the
	// advance axis, NOT by its horizontal glyph width — the per-character
	// draw loop only uses measureTextStr(charStr) to CENTER the glyph
	// within the cross-axis box.Width, never as an advance. WordSpacing has
	// no effect in that loop (no space-character special case), matching
	// the dispatch below. Sideways text (IsSidewaysRL/IsSidewaysLR, which
	// also sets IsVerticalText per engine.go:373-391) is excluded: it
	// renders the run as ordinary horizontal glyphs into an off-screen
	// buffer before rotating the whole buffer, so measureTextStr's glyph
	// width IS the correct per-character advance there, same as horizontal
	// text.
	if layer.IsVerticalText && !layer.IsSidewaysRL && !layer.IsSidewaysLR {
		return measureUprightVerticalSegmentAdvance(text, layer, r.fontPathForLayer(layer), includeTrailing)
	}
	if layer.LetterSpacing == 0 && layer.WordSpacing == 0 {
		return r.measureTextStr(text, fontID, layer.featureInputs())
	}
	if layer.LetterSpacing != 0 {
		runes := []rune(text)
		var total float64
		for i, ch := range runes {
			total += r.measureTextStr(string(ch), fontID, layer.featureInputs())
			if includeTrailing || i < len(runes)-1 {
				total += layer.LetterSpacing
				if ch == ' ' {
					total += layer.WordSpacing
				}
			}
		}
		return total
	}
	// Word-spacing only: mirrors the word-by-word branch.
	words := strings.Split(text, " ")
	spaceW := r.measureTextStr(" ", fontID, layer.featureInputs())
	var total float64
	for i, word := range words {
		if word != "" {
			total += r.measureTextStr(word, fontID, layer.featureInputs())
		}
		if i < len(words)-1 {
			total += spaceW + layer.WordSpacing
		}
	}
	return total
}

// measureUprightVerticalSegmentAdvance measures the advance-axis extent of
// text exactly as drawTextSegment's upright-vertical per-character loop
// (render.go's IsVerticalText branch) advances y: the sum of
// uprightVerticalAdvances (that loop has no WordSpacing effect at all — see
// measureSegmentAdvanceTrailing's dispatch comment for why sideways text does
// not take this path). includeTrailing mirrors measureSegmentAdvance vs.
// measureSegmentVisualWidth's existing contract: true keeps the final rune's
// own trailing LetterSpacing gap (positioning a LATER segment), false drops
// it (sizing the highlighted rect itself, which must not bleed into the gap
// before the next, unselected rune).
func measureUprightVerticalSegmentAdvance(text string, layer *PaintLayer, fontPath string, includeTrailing bool) float64 {
	return uprightAdvanceExtent(uprightVerticalAdvances(layer, text, fontPath), layer, includeTrailing)
}

// uprightAdvanceExtent sums advances, the segment's uprightVerticalAdvances,
// dropping the final rune's trailing LetterSpacing unless includeTrailing
// (see measureUprightVerticalSegmentAdvance). An empty segment is 0.
func uprightAdvanceExtent(advances []float64, layer *PaintLayer, includeTrailing bool) float64 {
	if len(advances) == 0 {
		return 0
	}
	var total float64
	for _, a := range advances {
		total += a
	}
	if !includeTrailing {
		total -= layer.LetterSpacing
	}
	return total
}

// uprightVerticalAdvances is the physical Y distance drawTextSegment's own
// IsVerticalText per-character paint loop (render.go, a few hundred lines
// below) advances for each rune of str: layout's unsnapped upright advances,
// text.UprightVerticalAdvances — the per-glyph vertical advances of the
// shape result Blink paints (HarfBuzzGetGlyphVerticalAdvance,
// platform/fonts/shaping/harfbuzz_face.cc:359-372 @
// 5992657f556bcd9d207a4aca187488462f027cab) — plus layer.LetterSpacing per
// rune (that loop has no WordSpacing effect at all). Shared by the paint
// loop, measureUprightVerticalSegmentAdvance and the upright decoration
// length, so the highlight rect's measured extent never drifts from what the
// glyphs actually draw at — the exact failure mode LOU-347 exists to fix —
// and, as layout ceils only the sum of the same advances
// (text.MeasureTextVerticalFromFont), the glyphs never paint past the
// layout box.
func uprightVerticalAdvances(layer *PaintLayer, str string, fontPath string) []float64 {
	advances := text.UprightVerticalAdvances(str, layer.FontSize, fontPath)
	for i := range advances {
		advances[i] += layer.LetterSpacing
	}
	return advances
}

// setLayerColorOverride applies a ::selection color override to a segment
// layer: TextColor always overrides to textColor; TextDecorationColor
// overrides to decorationColor (falling back to textColor, i.e.
// currentColor semantics, when decorationColor is nil); and EVERY
// AppliedTextDecorations entry is recolored to that resolved decoration
// color — not just entries with HasColor == false (currentColor at the
// originating element). Per CSS Pseudo-4 §highlight-painting: "The
// element's own text decorations... are thus drawn in the pseudo-element's
// own color when that is not currentColor, regardless of their ORIGINAL
// COLOR OR FILL SPECIFICATIONS" (selection-originating-decoration-color.html
// quotes this verbatim — its <main> has an EXPLICIT
// `text-decoration: 0.125em black solid line-through` (HasColor == true,
// not currentColor), and the test still asserts the selected portion's
// strikethrough recolors to ::selection's green). decorationColor here is
// always a concrete resolved RGB by the time this function runs (never
// literally "currentColor"), so the spec's "when that is not currentColor"
// carve-out is already satisfied — always recoloring is correct.
func (r *Renderer) setLayerColorOverride(layer *PaintLayer, textColor css.Color, decorationColor *css.Color) {
	layer.TextColor = textColor
	if decorationColor != nil {
		layer.TextDecorationColor = *decorationColor
	} else {
		layer.TextDecorationColor = textColor
	}
	if len(layer.AppliedTextDecorations) > 0 {
		decorations := make([]css.AppliedTextDecoration, len(layer.AppliedTextDecorations))
		copy(decorations, layer.AppliedTextDecorations)
		for i := range decorations {
			decorations[i].Color = layer.TextDecorationColor
			decorations[i].HasColor = true
		}
		layer.AppliedTextDecorations = decorations
	}
}

// applySelectionOwnDecoration sets layer.TextDecoration to ::selection's
// own text-decoration-line (active-selection-014.html: `div::selection {
// text-decoration: underline }` on a div with no decoration of its own).
// Only takes effect via the legacy single-decoration field — the
// AppliedTextDecorations vector is reserved for decorations the
// ORIGINATING element's cascade produced (recolored, not introduced, by
// setLayerColorOverride above); a ::selection-introduced decoration has no
// originating-element source to carry decorating-box continuity metadata
// for, so the simpler legacy field is the correct mechanism here.
func applySelectionOwnDecoration(layer *PaintLayer, line css.TextDecoration) {
	layer.TextDecoration = line
}

// drawTextSegment is drawText's original single-color paint body —
// renamed so drawText (above) can wrap it with ::selection segment
// splitting without duplicating the ~550 lines of sideways/vertical/
// decoration/shadow/small-caps/tab/letter-spacing dispatch logic below.
// Every pre-LOU-344 call path is unchanged; the only difference is the
// caller now always goes through drawText's segment loop (a no-split
// fragment is just a single unselected segment, byte-identical to the
// original whole-fragment paint).
func (r *Renderer) drawTextSegment(layer *PaintLayer) {
	box := layer.Box

	// Apply CSS text-transform.
	text := box.Text
	if layer.TextTransform != css.TextTransformNone {
		text = applyTextTransform(text, layer.TextTransform)
	}

	fontPath := r.fontPathForLayer(layer)
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

		// Strategy A: paint decorations into the off-screen buffer before
		// rotation. The buffer is in horizontal coords; decoration geometry is
		// computed against a virtual box offset to the text position within the
		// buffer. After rotation, buffer srcY maps to physical X according to
		// the rotation direction (CW for IsSidewaysRL, CCW for IsSidewaysLR).
		//
		// Two orthogonal axes determine where the underline lands physically:
		//
		//   1. Rotation direction (set by IsSidewaysRL vs IsSidewaysLR).
		//      Both vertical-rl mixed and vertical-lr mixed map to CW
		//      (IsSidewaysRL=true; IsWritingModeVerticalLR disambiguates the
		//      original writing mode). sideways-lr is the only CCW case.
		//
		//   2. Underline side (LEFT or RIGHT physical side of the line column).
		//      Per CSS Text Decor 4 §3.3, the default (auto position) follows
		//      the rotated alphabetic baseline's under direction:
		//        CW rotation → physical LEFT
		//        CCW rotation → physical RIGHT
		//      `text-underline-position: left|right` overrides to a specific
		//      physical side regardless of rotation.
		//
		// The buffer is extended on one side to make room for the decoration:
		//
		//   underline → LEFT, rotation CW (vertical-rl/-lr mixed, sideways-rl):
		//     Paint decoration BELOW text in buffer (decorExt rows). After CW
		//     rotation, low destX = LEFT. blitX = box.X - decorExt to keep text
		//     glyphs aligned at box.X.
		//
		//   underline → RIGHT, rotation CW (e.g. vertical-lr + text-underline-
		//   position: right):
		//     Paint decoration ABOVE text (topExt rows). After CW rotation,
		//     high destX = RIGHT. blitX = box.X (no shift; text occupies the
		//     low destX rows after CW maps srcY=topExt..lhExt-1 to destX =
		//     decorExt..lhExt-1-topExt = 0..lh-1, which is the leftmost
		//     range of dest with blitX = box.X).
		//
		//   underline → RIGHT, rotation CCW (sideways-lr default):
		//     Paint decoration BELOW text (decorExt rows). After CCW rotation,
		//     high destX = RIGHT. blitX = box.X (text at low destX maps to
		//     box.X..box.X+lh; decoration past lh maps to box.X+lh..box.X+
		//     lh+decorExt-1).
		//
		//   underline → LEFT, rotation CCW (e.g. sideways-lr + text-underline-
		//   position: left):
		//     Paint decoration ABOVE text (topExt rows). After CCW rotation,
		//     low destX = LEFT. blitX = box.X - topExt to keep text aligned.
		//
		// Mirrors Blink's ConcatCTM(*rotation) at
		// third_party/blink/renderer/core/paint/text_fragment_painter.cc:1002-1009
		// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		metrics := r.dc.GetFontMetrics(fontID)
		descent := float64(metrics.Descent) / 64.0
		// The decorations span the fragment's inline size, not the ceiled
		// buffer width ta.
		decorationInlineSize := layer.decorationInlineSize()

		// A central-baseline (vertical-*, mixed) run's decorations paint
		// line-relative after the blit instead (drawLineRelativeTextDecoration).
		centralBaseline := layer.IsUprightVertical
		hasDecor := !centralBaseline && layer.hasTextDecoration()

		// underToRight: true when the underline paints on the physical RIGHT
		// side of the line column. See the block comment above for the rotation
		// × position truth table.
		//
		// For RTL sideways text the underline flips to the opposite physical
		// side from LTR: sideways-rl RTL → RIGHT (not LEFT), sideways-lr RTL →
		// LEFT (not RIGHT). Mirrors Blink's WritingMode × direction × position
		// truth table in text_decoration_info.cc:ComputeUnderlinePosition.
		underToRight := sidewaysUnderlineGoesRight(layer)
		rotCW := layer.IsSidewaysRL // vs CCW for sideways-lr
		if len(layer.AppliedTextDecorations) > 0 {
			if td0 := layer.AppliedTextDecorations[0]; td0.SourceStyle != nil &&
				td0.SourceStyle.GetDirection() == css.DirectionRTL {
				underToRight = !underToRight
			}
		}

		// decorExt: rows BELOW text in buffer. topExt: rows ABOVE text.
		// Exactly one side is non-zero (determined by underToRight × rotation).
		decorExt := 0
		topExt := 0

		// Inline-axis buffer extensions for text-decoration-inset with
		// negative values (which extend the decoration past the fragment's
		// inline-start / inline-end edges). The buffer X axis is the inline
		// axis pre-rotation; without extending the buffer, a negative inset
		// would clip at the buffer edge and produce no extension after
		// rotation. CSS Text Decor 4 §3.6 (text-decoration-inset).
		//
		// Gated on HasDecoratingBox + IsFirstFragment/IsLastFragment so the
		// extension only fires at the actual outer edges of the decorating
		// box (per LOU-149 Phase 4 metadata + the C68 multi-line block
		// extension). For a single-fragment inline decoration (e.g.
		// `<u>brown</u>` in a vertical writing mode) the layout-side
		// stamping marks HasDecoratingBox=true with IsFirstFragment=true
		// AND IsLastFragment=true so both edges extend. For multi-line
		// block decorations, only the first line's first-frag and last
		// line's last-frag extend.
		inlineStartExt := 0
		inlineEndExt := 0

		// underBelow: which side of the text the underline grows toward in the
		// pre-rotation buffer. Overline support in this branch is currently
		// limited — see the paint block below for the rationale.
		underBelow := (rotCW && !underToRight) || (!rotCW && underToRight)
		if hasDecor && len(layer.AppliedTextDecorations) > 0 {
			virtualBox := &layout.Box{X: 0, Y: 0, Width: decorationInlineSize, Height: float64(lh)}
			tmpInfo := newVerticalTextDecorationInfo(virtualBox, decorationInlineSize, layer.FontSize, ascent, descent, 0, layer.blockUnderDirection())
			for _, td := range layer.AppliedTextDecorations {
				if !td.Lines.Has(css.TextDecorationLineUnderline) {
					continue
				}
				th := int(math.Ceil(tmpInfo.computeThickness(td)))
				gap := int(math.Ceil(tmpInfo.computeUnderlineZeroOffset(td.UnderlinePosition)))
				offsetExt := int(math.Ceil(td.UnderlineOffset))
				if offsetExt < 0 {
					offsetExt = 0
				}
				needed := gap + th + offsetExt
				if underBelow {
					if needed > decorExt {
						decorExt = needed
					}
				} else {
					if needed > topExt {
						topExt = needed
					}
				}
				// Inline-axis extension for negative insets.
				//
				// Two stamp paths for "this fragment IS at the decorating-
				// box edge":
				//
				//  (a) HasDecoratingBox=true: LOU-149 inline-multi-fragment
				//      or C68 multi-line-block layout-stamped metadata.
				//      Trust td.IsFirstFragment / td.IsLastFragment.
				//
				//  (b) HasDecoratingBox=false: layout didn't stamp because
				//      no fragmentation is happening. Cascade default has
				//      IsFirstFragment=true AND IsLastFragment=true. Treat
				//      this fragment as the complete decorating box and
				//      extend at both edges — this is the single-fragment
				//      inline path (e.g. `<u>brown</u>` in vertical-rl).
				//
				// Branch (b) requires BOTH flags true so we don't over-
				// extend in a non-LOU-149-tracked path where they might
				// differ (defensive).
				appliesAtStart := false
				appliesAtEnd := false
				if td.HasDecoratingBox {
					appliesAtStart = td.IsFirstFragment
					appliesAtEnd = td.IsLastFragment
				} else if td.IsFirstFragment && td.IsLastFragment {
					appliesAtStart = true
					appliesAtEnd = true
				}
				// For RTL sideways text (both sideways-rl and sideways-lr)
				// the logical InlineStart and InlineEnd map to the OPPOSITE
				// physical sides compared to LTR, so the buffer-extension
				// bucket assignments are swapped.
				//
				// sideways-rl RTL: InlineStart=physical BOTTOM=high buffer X,
				//                  InlineEnd=physical TOP=low buffer X.
				// sideways-lr RTL: InlineStart=physical TOP=high buffer X,
				//                  InlineEnd=physical BOTTOM=low buffer X.
				//
				// In both cases a negative InlineEnd (extend past inline-end)
				// extends toward low buffer X → inlineStartExt; LTR would
				// extend inlineEndExt instead.
				sidewaysRTL := td.SourceStyle != nil && td.SourceStyle.GetDirection() == css.DirectionRTL
				if sidewaysRTL {
					if appliesAtStart && td.Inset.InlineStart < 0 {
						ext := int(math.Ceil(-td.Inset.InlineStart))
						if ext > inlineEndExt {
							inlineEndExt = ext
						}
					}
					if appliesAtEnd && td.Inset.InlineEnd < 0 {
						ext := int(math.Ceil(-td.Inset.InlineEnd))
						if ext > inlineStartExt {
							inlineStartExt = ext
						}
					}
				} else {
					if appliesAtStart && td.Inset.InlineStart < 0 {
						ext := int(math.Ceil(-td.Inset.InlineStart))
						if ext > inlineStartExt {
							inlineStartExt = ext
						}
					}
					if appliesAtEnd && td.Inset.InlineEnd < 0 {
						ext := int(math.Ceil(-td.Inset.InlineEnd))
						if ext > inlineEndExt {
							inlineEndExt = ext
						}
					}
				}
			}
		}

		lhExt := lh + decorExt + topExt             // enlarged buffer height
		taExt := ta + inlineStartExt + inlineEndExt // enlarged buffer width (inline axis)
		textOff := topExt                           // srcY where text glyph top lands

		// Draw horizontal text into an off-screen buffer. Text is painted
		// at buffer X = inlineStartExt so the inlineStartExt columns to
		// its left remain available for negative-inset overflow.
		src := image.NewRGBA(image.Rect(0, 0, taExt, lhExt))
		childDC := r.dc.NewChildContext(src)
		childDC.SetColor(premultipliedRGBA(layer.TextColor))

		// Phase 1 (underline/overline) must paint BEFORE text glyphs so glyphs
		// render on top. Mirrors Blink's PaintDecorationsExceptLineThrough called
		// before text in TextFragmentPainter::Paint @ 4883d11f. Only the
		// below-text path (underBelow) uses drawTextDecoration here; the
		// above-text path (else branch below) paints directly and is unaffected.
		if hasDecor && underBelow {
			origDC := r.dc
			r.dc = childDC
			// virtualBox X = inlineStartExt anchors the inset math so
			// drawOneAppliedTextDecoration's
			//   logicalStart = box.X + Inset.InlineStart
			//   logicalEnd   = box.X + inline size - Inset.InlineEnd
			// lands the painted rect inside the extended buffer
			// [0, taExt) — and HasDecoratingBox=true branches anchor
			// at box.X + DecoratingBoxOffsetX, which stays valid too.
			virtualBox := &layout.Box{X: float64(inlineStartExt), Y: float64(textOff), Width: decorationInlineSize, Height: float64(lhExt)}
			r.drawTextDecoration(layer, text, virtualBox, fontID, ascent, false)
			r.dc = origDC
			// drawTextDecoration mutates r.dc's color (via r.setColor). Since
			// r.dc was childDC during the call, childDC's color is now the
			// decoration color. Restore the text color so the glyph draw below
			// uses the correct color. Mirrors the r.setColor(layer.TextColor)
			// call in the non-rotation drawText path.
			childDC.SetColor(premultipliedRGBA(layer.TextColor))
		}

		if features := r.shapeFeatures(fontID, layer.featureInputs()); len(features) > 0 {
			childDC.DrawTextWithFeatures(text, fontID, float64(inlineStartExt), ascent+float64(textOff), features)
		} else {
			childDC.DrawText(text, fontID, float64(inlineStartExt), ascent+float64(textOff))
		}

		// Paint decorations into the buffer (Strategy A). Temporarily swap r.dc
		// for childDC so the painter writes into src.
		//
		// Underline painting is split by `underBelow` (the buffer side where
		// the underline sits before rotation). The below-text path's phase 1
		// (underline/overline) already ran above, before glyph drawing. Only
		// phase 2 (line-through, onlyLineThrough=true) runs here.
		//
		// The above-text path computes the underline rect manually because
		// drawOneAppliedTextDecoration assumes srcY-down, which would
		// re-introduce the offset-cancellation bug.
		//
		// Overline painting is delegated to drawTextDecoration's standard
		// horizontal geometry (rect [textTop - thickness, textTop] grows
		// upward from text-top, so srcY < textOff). After rotation:
		//   CW (vertical-rl/-lr mixed, sideways-rl): above-text → physical RIGHT
		//   CCW (sideways-lr): above-text → physical LEFT
		// Only true sideways runs reach here; central-baseline (mixed) runs
		// decorate line-relative instead (drawLineRelativeTextDecoration).
		if hasDecor {
			origDC := r.dc
			r.dc = childDC
			if underBelow {
				// Phase 1 (underline/overline) already ran before glyph drawing.
				// Phase 2: line-through only, painted after glyphs so it renders over them.
				virtualBox := &layout.Box{X: float64(inlineStartExt), Y: float64(textOff), Width: decorationInlineSize, Height: float64(lhExt)}
				r.drawTextDecoration(layer, text, virtualBox, fontID, ascent, true)
			} else if len(layer.AppliedTextDecorations) > 0 {
				// Above-text underline path: paint each rect manually with the
				// offset moving srcY UP (away from text), so after rotation
				// the offset moves the decoration FURTHER from the text in
				// the under-direction. Inset trims/extends the rect on the
				// inline axis (buffer X); HasDecoratingBox/first/last
				// fragment flags gate inset application at the decorating-
				// box edges so interior line breaks don't extend at the line
				// wrap. Mirrors drawOneAppliedTextDecoration's logic.
				info := newVerticalTextDecorationInfo(&layout.Box{X: 0, Width: decorationInlineSize}, decorationInlineSize, layer.FontSize, ascent, descent, 0, layer.blockUnderDirection())
				for _, td := range layer.AppliedTextDecorations {
					if !td.Lines.Has(css.TextDecorationLineUnderline) {
						continue
					}
					th := info.computeThickness(td)
					gap := info.computeUnderlineZeroOffset(td.UnderlinePosition)
					rectTopSrcY := float64(topExt) - gap - th - td.UnderlineOffset
					xStart := float64(inlineStartExt)
					xEnd := float64(inlineStartExt) + decorationInlineSize
					// Apply RTL inset swap for all sideways RTL text.
					// Both sideways-rl and sideways-lr RTL have InlineStart
					// at high buffer X and InlineEnd at low buffer X,
					// opposite to LTR — so the RTL applyDecorationInset
					// swap is correct for both rotation directions.
					sidewaysRTL := td.SourceStyle != nil && td.SourceStyle.GetDirection() == css.DirectionRTL
					xStart, xEnd = applyDecorationInset(xStart, xEnd, td, sidewaysRTL)
					if xEnd <= xStart {
						continue
					}
					childDC.SetColor(premultipliedRGBA(td.Color))
					childDC.DrawRectangle(xStart, rectTopSrcY, xEnd-xStart, th)
					childDC.Fill()
				}
			}
			r.dc = origDC
		}

		// Rotate pixels 90° into destination (lhExt × taExt) buffer. The
		// extended buffer width (taExt = ta + inlineStartExt + inlineEndExt)
		// carries negative-inset decoration overflow on the inline axis.
		rot := image.NewRGBA(image.Rect(0, 0, lhExt, taExt))
		for y := 0; y < lhExt; y++ {
			for x := 0; x < taExt; x++ {
				c := src.RGBAAt(x, y)
				if c.A == 0 {
					continue
				}
				if rotCW {
					// 90° CW: (x,y) → (lhExt-1-y, x)
					rot.SetRGBA(lhExt-1-y, x, c)
				} else {
					// 90° CCW: (x,y) → (y, taExt-1-x)
					rot.SetRGBA(y, taExt-1-x, c)
				}
			}
		}

		// Blit position depends on which side of the buffer carries the text:
		//   below-text (decorExt > 0):
		//     CW rotation: text glyphs occupy high srcY → low destX; the
		//       decorExt rows (high srcY past lh) map to even lower destX,
		//       which must end up LEFT of box.X. So blitX = box.X - decorExt.
		//     CCW rotation: text glyphs occupy low srcY → low destX; the
		//       decorExt rows (high srcY past lh) map to high destX, which
		//       lands RIGHT of box.X + lh naturally. blitX = box.X.
		//   above-text (topExt > 0):
		//     CW rotation: topExt rows (low srcY) map to high destX, landing
		//       RIGHT of box.X + lh naturally. blitX = box.X.
		//     CCW rotation: topExt rows map to low destX, which must end up
		//       LEFT of box.X. blitX = box.X - topExt.
		blitShift := 0
		if rotCW {
			blitShift = decorExt
		} else {
			blitShift = topExt
		}
		blitX := int(math.Round(box.X)) - blitShift
		// Inline-axis blit shift accounts for the inlineStartExt /
		// inlineEndExt columns that carry negative-inset overflow.
		//   CW rotation: low buffer X → low dest Y. Text glyphs sit at
		//     buffer X = inlineStartExt → dest Y = inlineStartExt; shift
		//     blitY up by inlineStartExt to land them at dest Y = box.Y.
		//   CCW rotation: low buffer X → high dest Y. Text glyphs at
		//     buffer X = inlineStartExt → dest Y = taExt-1-inlineStartExt;
		//     the inlineEndExt rows extend off the original destination
		//     bottom, so shift blitY up by inlineEndExt to keep the text
		//     rows where they were.
		blitY := int(math.Round(box.Y))
		if rotCW {
			blitY -= inlineStartExt
		} else {
			blitY -= inlineEndExt
		}
		// A central-baseline run decorates line-relative across its advance,
		// letter- and word-spacing included as Blink's text fragment's inline
		// size is (measureSegmentAdvance): under- and overlines
		// beneath the glyphs, line-through over them. Rotated glyphs skip
		// ink, unlike upright ones (InterceptsFromBlobs,
		// platform/fonts/font.cc:161-162 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		var glyphs []glyphExtent
		var inlineLen float64
		lineRelativeDecor := centralBaseline && layer.hasTextDecoration()
		if lineRelativeDecor {
			glyphs = r.skipInkGlyphs(layer, text, fontID, box.Y)
			inlineLen = r.measureSegmentAdvance(text, fontID, layer)
			r.drawLineRelativeTextDecoration(layer, box, inlineLen, fontID, ascent, glyphs, false)
		}
		r.dc.DrawImage(rot, blitX, blitY)
		if lineRelativeDecor {
			r.drawLineRelativeTextDecoration(layer, box, inlineLen, fontID, ascent, glyphs, true)
		}

		// Per-character emphasis marks: each mark renders as its own small
		// rotated off-screen buffer at the annotation-equivalent screen
		// position. Mirrors louis14's ruby annotation paint structure
		// (`pkg/layout/inline_layout_ruby.go:60-83`) so emphasis pixels snap
		// to the same per-fragment math.Round anchors as ruby annotation
		// pixels — the structural fix for the sub-pixel drift identified in
		// LOU-162's first attempt (see ticket-active/LOU-162/findings.md).
		if layer.TextEmphasisMark != "" {
			r.drawSidewaysEmphasisMarks(layer, text, box, fontID, ta)
		}
		return
	}

	// Vertical text: draw each character stacked vertically.
	// CSS Writing Modes §5.1: in vertical writing modes, upright glyphs
	// advance in the block-progression (vertical) direction. Each character
	// cell is its vertical advance tall (uprightVerticalAdvances) and the
	// glyph is centered horizontally.
	if layer.IsVerticalText {
		y := box.Y
		advances := uprightVerticalAdvances(layer, text, fontPath)
		// Decorations paint line-relative along the glyphs' upright advances
		// plus letter-spacing: under- and overlines beneath the glyphs,
		// line-through over them (TextFragmentPainter::Paint,
		// core/paint/text_fragment_painter.cc @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Upright glyphs skip no
		// ink: Blink computes no intercepts for upright blobs
		// (InterceptsFromBlobs, platform/fonts/font.cc:161-162).
		inlineLen := uprightAdvanceExtent(advances, layer, true)
		if layer.hasTextDecoration() {
			r.drawLineRelativeTextDecoration(layer, box, inlineLen, fontID, ascent, nil, false)
		}
		for i, ch := range []rune(text) {
			charStr := string(ch)
			charW := r.measureTextStr(charStr, fontID, layer.featureInputs())
			// Center the glyph on the central baseline.
			xOffset := layout.CentralBaselineUnderSide(box.Width) - charW/2
			if xOffset < 0 {
				xOffset = 0
			}
			r.drawTextStr(charStr, fontID, box.X+xOffset, y+ascent, layer.featureInputs())
			y += advances[i]
		}
		if layer.hasTextDecoration() {
			r.drawLineRelativeTextDecoration(layer, box, inlineLen, fontID, ascent, nil, true)
		}
		return
	}

	// Draw underline and overline before text glyphs so text renders on top.
	// Mirrors Blink TextPainter::PaintDecorationsExceptLineThrough called
	// before the text in TextFragmentPainter::Paint @ 4883d11f.
	r.drawTextDecoration(layer, text, box, fontID, ascent, false)
	// Restore the text color after drawTextDecoration mutates the color state.
	r.setColor(layer.TextColor)

	// Draw text shadows (behind actual text).
	if len(layer.TextShadows) > 0 {
		r.drawTextShadows(layer, text, box, fontID, ascent)
	}

	// CSS font-variant-caps: small-caps / all-small-caps. When the active
	// face carries native `smcp`/`c2sc` glyphs (emitted as feature tags in
	// paint_layer.go), HarfBuzz substitutes them during shaping and the
	// regular drawText path renders correctly. We only need the synthesized
	// scale-lowercase-to-uppercase fallback (drawTextSmallCaps) when:
	//   (a) the synthesis is permitted (font-synthesis-small-caps != none), AND
	//   (b) the active face has no native small-caps support.
	//
	// Heuristic for (b): a face that resolved through the @font-face
	// FontRegistry is treated as "may have native smcp" (defer to feature
	// emission); a face from the UA built-in defaults is treated as "no
	// native smcp" (synthesize). This handles the WPT
	// font-synthesis-small-caps-not-applied fixture (Exo-DemiBold via
	// @font-face) without needing a per-fontID GSUB probe. CSS Fonts 4 §6.6
	// mirrors Blink's gate at
	// platform/fonts/opentype/open_type_caps_support.cc::SyntheticSmallCapsAllowed
	// (SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f), which checks only the
	// synthesis flag — Blink's NeedsSyntheticFont() then consults a real
	// per-font GSUB lookup. The louis14 heuristic is a conservative stand-in
	// that avoids double-application of native + synthesized small-caps.
	wantsCaps := layer.FontVariantCaps == "small-caps" || layer.FontVariantCaps == "all-small-caps"
	familyFromRegistry := r.familyDeclaredViaFontFace(layer.FontFamily)
	if wantsCaps && layer.FontSynthesisSmallCaps && !familyFromRegistry {
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
				r.drawTextStr(string(ch), fontID, x, baselineY, layer.featureInputs())
				charW := r.measureTextStr(string(ch), fontID, layer.featureInputs())
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
			spaceW := r.measureTextStr(" ", fontID, layer.featureInputs())
			for i, word := range words {
				if word != "" {
					r.drawTextStr(word, fontID, x, baselineY, layer.featureInputs())
					x += r.measureTextStr(word, fontID, layer.featureInputs())
				}
				if i < len(words)-1 {
					x += spaceW + layer.WordSpacing
				}
			}
		}
	} else {
		r.drawTextStr(text, fontID, box.X, box.Y+ascent, layer.featureInputs())
	}

	// Draw text decoration lines: line-through (after text so it paints over glyphs).
	// Underline and overline were drawn before text glyphs (see below).
	r.drawTextDecoration(layer, text, box, fontID, ascent, true)

	// Draw text emphasis marks (small marks above/below each character).
	if layer.TextEmphasisMark != "" {
		r.drawTextEmphasis(layer, text, box, fontID, ascent)
	}
}

// emphasisMarkAscentDescent is the emphasis-mark font's ascent and descent
// at size: its normalized OS/2 typo metrics (Font::EmphasisMarkAscent /
// EmphasisMarkDescent, platform/fonts/font.cc:259-281 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) emphasisMarkAscentDescent(size float64, fontPath string) (ascent, descent float64) {
	return text.NormalizedTypoAscentAndDescent(size, fontPath)
}

// emHeightOverAdjustment is text.EmHeightOverAdjustment for an emphasized
// item on its dominant baseline, which Blink reads from the item's style
// (style->GetFontBaseline(), ruby_utils.cc:920-929 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) emHeightOverAdjustment(size float64, fontPath string, centralBaseline bool) float64 {
	return text.EmHeightOverAdjustment(size, fontPath, centralBaseline)
}

// drawTextEmphasis renders text emphasis marks (e.g., dots, circles, sesame)
// above or below each character. The mark is drawn at ~50% of the text font
// size, centered horizontally over each glyph.
func (r *Renderer) drawTextEmphasis(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	mark := layer.TextEmphasisMark
	emphFontSize := layout.EmphasisMarkFontSize(layer.FontSize)

	// Open a font for the emphasis mark at half size.
	fontPath := r.fontPathForLayer(layer)
	emphFontID := r.openFont(fontPath, emphFontSize)
	if emphFontID < 0 {
		return
	}

	r.setColor(layer.TextEmphasisColor)

	// The mark font's ascent and descent are its normalized OS/2 typo
	// metrics (Font::EmphasisMarkAscent / EmphasisMarkDescent →
	// SimpleFontData::NormalizedTypoAscent / Descent, platform/fonts/
	// font.cc:259-281 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b, with
	// TextEmphasisAsRuby stable), the ones layout sizes the mark with
	// (emphasisMarkAscentFromLine).
	emphAscent, emphDescent := r.emphasisMarkAscentDescent(emphFontSize, fontPath)
	markW := r.dc.MeasureText(mark, emphFontID)

	// Compute vertical position of the emphasis mark.
	// Blink's formula at `third_party/blink/renderer/core/paint/text_painter.cc`
	// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f`:
	//
	//   over:  mark baseline = baseline - baseAscent - emphDescent = box.Y - emphDescent
	//   under: mark baseline = baseline + baseDescent + emphAscent
	//
	// box.Y is the inline-box top, box.Y + ascent is the baseline, and
	// DrawText draws with the baseline at the y argument — so we set
	// emphY = (mark baseline) - emphAscent.
	//
	// WPT tests compare emphasis-position: under against ruby-position: under.
	// Louis14's ruby-position: under is stubbed as over (Phase 11 future work);
	// its annotations appear ABOVE the base text, same as ruby-position: over.
	// To match, emphasis-position: under must also use the over formula until
	// ruby-position: under is properly implemented (Phase 11).
	// TODO(Phase11): switch the under branch to:
	//   emphY = box.Y + ascent + baseDescent
	// once ruby-position:under paints annotations below the base text.
	// Both over and under use the over formula; see comment above.
	// TextPainter::SetEmphasisMark (core/paint/text_painter.cc:510-550 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) floors the over offset from
	// the baseline: floor(-baseAscent - emphDescent). box.Y is the baseline
	// less the rounded base ascent, so the mark baseline is
	// box.Y + floor(-emphDescent), lowered by the item's em-height
	// adjustment (emHeightOverAdjustment), which SetEmphasisMark adds
	// back through the item's annotation ascent (text_painter.cc:529).
	emphY := box.Y + r.emHeightOverAdjustment(layer.FontSize, fontPath, false) + math.Floor(-emphDescent) - emphAscent

	// Iterate over each character and draw the mark centered above/below it.
	x := box.X
	for _, ch := range text {
		charStr := string(ch)
		charW := r.measureTextStr(charStr, fontID, layer.featureInputs())

		// Skip characters that cannot receive emphasis marks.
		if canReceiveTextEmphasis(ch) {
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
// ta is the parent's off-screen horizontal-frame text advance, used to
// place counter-clockwise marks along the inline axis.
func (r *Renderer) drawSidewaysEmphasisMarks(layer *PaintLayer, text string, box *layout.Box, fontID int32, ta int) {
	mark := layer.TextEmphasisMark
	emphFontSize := layout.EmphasisMarkFontSize(layer.FontSize)
	fontPath := r.fontPathForLayer(layer)
	emphFontID := r.openFont(fontPath, emphFontSize)
	if emphFontID < 0 {
		return
	}
	// Normalized OS/2 typo metrics, as in drawTextEmphasis.
	emphAscent, emphDescent := r.emphasisMarkAscentDescent(emphFontSize, fontPath)
	markW := r.dc.MeasureText(mark, emphFontID)

	// The buffer's on-screen position comes from Blink's line-relative
	// geometry (sidewaysEmphasisGeometry.screenX) below.

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
	markDC.SetColor(premultipliedRGBA(emphColor))
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

	// The loop-invariant screen X of the mark buffer, from Blink's
	// line-relative geometry (sidewaysEmphasisGeometry.screenX). Every mark
	// goes on the over side: louis14 paints ruby-position: under as over, and
	// the WPT references express under emphasis with ruby-position: under, so
	// the two stay aligned until under ruby lands.
	screenX := sidewaysEmphasisGeometry{
		boxLeft: box.X, boxWidth: box.Width,
		overAdjust: r.emHeightOverAdjustment(layer.FontSize, fontPath, sidewaysEmphasisCentralBaseline(box)),
		markAscent: emphAscent, markDescent: emphDescent,
		annotH: annotH,
	}.screenX(layer.IsSidewaysRL)
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
		charW := r.measureTextStr(charStr, fontID, layer.featureInputs())

		if canReceiveTextEmphasis(ch) {
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
			r.drawTextStr(drawStr, smallFontID, x, smallBaselineY, layer.featureInputs())
			charW := r.measureTextStr(drawStr, smallFontID, layer.featureInputs())
			x += charW
		} else {
			// Draw at normal size.
			r.drawTextStr(drawStr, fontID, x, baselineY, layer.featureInputs())
			charW := r.measureTextStr(drawStr, fontID, layer.featureInputs())
			x += charW
		}

		// Apply letter-spacing after each character.
		x += layer.LetterSpacing
		if ch == ' ' {
			x += layer.WordSpacing
		}
	}
}

// tabStopIntervalPx computes the tab stop interval in pixels for layer:
// tab-size as an explicit length uses that length directly; tab-size as a
// bare number is that many space-character widths (CSS Text 3 §4.2).
// Factored out of drawTextWithTabs so measureSegmentVisualWidthWithTabs
// (the ::selection highlight-rect sizing path) computes the SAME interval
// rather than re-deriving it and risking drift (CLAUDE.md §4 dedup).
func (r *Renderer) tabStopIntervalPx(layer *PaintLayer, fontID int32) float64 {
	tabSizeVal := layer.TabSize
	var tabStopPx float64
	if layer.TabSizeIsLength {
		tabStopPx = tabSizeVal
	} else {
		spaceW := r.measureTextStr(" ", fontID, layer.featureInputs())
		if spaceW <= 0 {
			spaceW = layer.FontSize
		}
		tabStopPx = tabSizeVal * spaceW
	}
	if tabStopPx <= 0 {
		tabStopPx = layer.FontSize * 8
	}
	return tabStopPx
}

// tabAdvance returns the width a tab character occupies: the distance from
// posFromLineStart forward to the next tab stop, or a full tabStopPx when
// already exactly on one. Shared by drawTextWithTabs and
// measureSegmentVisualWidthWithTabs so the painted glyph advance and the
// ::selection highlight-rect width stay identical (CLAUDE.md §4 dedup).
func tabAdvance(posFromLineStart, tabStopPx float64) float64 {
	rem := math.Mod(posFromLineStart, tabStopPx)
	if rem < 1e-9 {
		return tabStopPx
	}
	return tabStopPx - rem
}

// lineStartX finds the line start (containing block's content-area left
// edge) for box: the nearest ancestor with a Style, per CSS Text 3 §4.2's
// "tab stops are measured from the line's start" rule. Factored out of
// drawTextWithTabs for the same dedup reason as tabStopIntervalPx.
func lineStartX(box *layout.Box) float64 {
	lineStart := box.X
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Style != nil {
			return p.X + p.Padding.Left + p.Border.Left
		}
	}
	return lineStart
}

// measureSegmentVisualWidthWithTabs is measureSegmentVisualWidth's tab-
// aware counterpart: text containing a tab character expands each tab to
// its tab stop (CSS Text 3 §4.2), exactly mirroring drawTextWithTabs'
// advance math, so the ::selection highlight rect covers the SAME width
// the glyphs (or, for a bare tab, the expanded blank run) actually occupy
// — plain measureSegmentVisualWidth measures "\t" as an ordinary glyph
// advance (a few px), which left the highlight rect far narrower than the
// real tab-stop-wide gap (active-selection-063.html: a lone tab's
// highlight covered ~1 character cell instead of the full 4-character
// tab-size span). segBox supplies X/Parent for the line-start-relative
// tab-stop phase calculation; only the FINAL trailing tab-stop expansion
// after the last non-tab segment is included (matching
// measureSegmentVisualWidth's no-trailing-gap contract — there is no
// "next character" to leave a gap before when this is the last/only
// segment, which is the only shape LOU-344's target tests exercise).
func (r *Renderer) measureSegmentVisualWidthWithTabs(segBox *layout.Box, fontID int32, layer *PaintLayer) float64 {
	return r.measureTabAwareAdvance(segBox.Text, segBox, fontID, layer)
}

// measureSegmentAdvanceWithTabs is measureSegmentVisualWidthWithTabs'
// counterpart for prefix positioning: measures the tab-aware advance of
// text (typically box.Text[:n], everything BEFORE a later segment) so a
// segment positioned after a tab lands at the tab-stop-expanded X, not the
// tab's raw (much narrower) glyph advance — drawText's prefix-positioning
// call previously used the plain (non-tab-aware) measureSegmentAdvance
// unconditionally, which mispositioned any segment following a tab even
// though the SELECTED segment's own width was already tab-stop-correct
// (CodeRabbit review, LOU-344 PR #148).
func (r *Renderer) measureSegmentAdvanceWithTabs(text string, refBox *layout.Box, fontID int32, layer *PaintLayer) float64 {
	return r.measureTabAwareAdvance(text, refBox, fontID, layer)
}

// measureTabAwareAdvance is the shared implementation behind
// measureSegmentVisualWidthWithTabs (text == refBox.Text, the selected
// segment's own already-sliced box, refBox.X is where THAT segment starts)
// and measureSegmentAdvanceWithTabs (text is a PREFIX of the original,
// un-sliced box.Text, so refBox must be the ORIGINAL box — refBox.X is
// where the prefix starts, i.e. the line's text-run start). Either way
// refBox.X anchors the tab-stop phase calculation relative to the line
// start; only Parent/X are read, never refBox.Text itself.
func (r *Renderer) measureTabAwareAdvance(text string, refBox *layout.Box, fontID int32, layer *PaintLayer) float64 {
	tabStopPx := r.tabStopIntervalPx(layer, fontID)
	startX := lineStartX(refBox)
	posFromLineStart := refBox.X - startX
	total := 0.0
	segments := strings.Split(text, "\t")
	for i, seg := range segments {
		if seg != "" {
			segW := r.measureTextStr(seg, fontID, layer.featureInputs())
			if layer.WordSpacing != 0 {
				segW += layer.WordSpacing * float64(strings.Count(seg, " "))
			}
			total += segW
			posFromLineStart += segW
		}
		if i < len(segments)-1 {
			tabW := tabAdvance(posFromLineStart, tabStopPx)
			total += tabW
			posFromLineStart += tabW
		}
	}
	return total
}

// drawTextWithTabs renders text containing tab characters. Tab characters
// advance to the next tab stop without drawing a glyph. Tab stops are at
// multiples of (tab-size × space-width) or (tab-size in px) from the line
// start (containing block's content edge).
func (r *Renderer) drawTextWithTabs(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64) {
	tabStopPx := r.tabStopIntervalPx(layer, fontID)

	// Position within the line, measured from line start.
	posFromLineStart := box.X - lineStartX(box)
	x := box.X
	baselineY := box.Y + ascent

	segments := strings.Split(text, "\t")
	for i, seg := range segments {
		if seg != "" {
			if layer.LetterSpacing != 0 {
				for _, ch := range seg {
					r.drawTextStr(string(ch), fontID, x, baselineY, layer.featureInputs())
					charW := r.measureTextStr(string(ch), fontID, layer.featureInputs())
					x += charW + layer.LetterSpacing
					posFromLineStart += charW + layer.LetterSpacing
					if ch == ' ' {
						x += layer.WordSpacing
						posFromLineStart += layer.WordSpacing
					}
				}
			} else {
				r.drawTextStr(seg, fontID, x, baselineY, layer.featureInputs())
				segW := r.measureTextStr(seg, fontID, layer.featureInputs())
				if layer.WordSpacing != 0 {
					segW += layer.WordSpacing * float64(strings.Count(seg, " "))
				}
				x += segW
				posFromLineStart += segW
			}
		}
		// After each segment except the last, advance to next tab stop.
		if i < len(segments)-1 {
			tabW := tabAdvance(posFromLineStart, tabStopPx)
			x += tabW
			posFromLineStart += tabW
		}
	}
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
					r.drawTextStr(string(ch), fontID, x, baselineY, layer.featureInputs())
					charW := r.measureTextStr(string(ch), fontID, layer.featureInputs())
					x += charW + layer.LetterSpacing
					if ch == ' ' {
						x += layer.WordSpacing
					}
				}
			} else {
				r.drawTextStr(text, fontID, box.X+shadow.OffsetX, box.Y+ascent+shadow.OffsetY, layer.featureInputs())
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
	textWidth := r.measureTextStr(text, fontID, layer.featureInputs())
	metrics := r.dc.GetFontMetrics(fontID)
	textHeight := float64(metrics.Ascent-metrics.Descent) / 64.0

	sigma := shadow.Blur / 2
	extend := math.Ceil(sigma * 3)

	bw := int(math.Ceil(textWidth + 2*extend))
	bh := int(math.Ceil(textHeight + 2*extend))
	if bw <= 0 || bh <= 0 || bw > 2000 || bh > 2000 {
		// Fallback: draw without blur.
		r.setColor(shadow.Color)
		r.drawTextStr(text, fontID, box.X+shadow.OffsetX, box.Y+ascent+shadow.OffsetY, layer.featureInputs())
		return
	}

	// Render text into offscreen buffer.
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	childDC := r.dc.NewChildContext(buf)
	childDC.SetColor(premultipliedRGBA(shadow.Color))
	if features := r.shapeFeatures(fontID, layer.featureInputs()); len(features) > 0 {
		childDC.DrawTextWithFeatures(text, fontID, extend, extend+ascent, features)
	} else {
		childDC.DrawText(text, fontID, extend, extend+ascent)
	}

	// Apply 3-pass box blur (approximates Gaussian blur).
	blurRadius := int(math.Round(sigma))
	gaussianBlur3Box(buf, blurRadius)

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

// emphasisPunctuationException returns true if the rune is in the explicit
// allow-list of "symbol-like" punctuation that may receive text-emphasis marks.
// This list mirrors Blink's Character::CanReceiveTextEmphasis exception switch,
// verified against Chromium @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
// The 23 code points are: # % & @ § ¶ ‰ ‱ ⁊ ⁋ ⁓ 〽 and their fullwidth/small
// variants (NFKD-equivalent sets); hardcoding avoids a runtime normalization
// dependency and matches Blink's structure.
func emphasisPunctuationException(r rune) bool {
	switch r {
	case 0x0023, // #, NUMBER SIGN
		0x0025, // %, PERCENT SIGN
		0x0026, // &, AMPERSAND
		0x0040, // @, COMMERCIAL AT
		0x00A7, // §, SECTION SIGN
		0x00B6, // ¶, PILCROW
		0x0609, // ARABIC-INDIC PER MILLE SIGN
		0x060A, // ARABIC-INDIC PER TEN THOUSAND SIGN
		0x066A, // ARABIC PERCENT SIGN
		0x2030, // ‰, PER MILLE SIGN
		0x2031, // ‱, PER TEN THOUSAND SIGN
		0x204A, // ⁊, TIRONIAN SIGN ET
		0x204B, // ⁋, REVERSED PILCROW
		0x2053, // ⁓, SWUNG DASH
		0x303D, // 〽, PART ALTERNATION MARK
		0xFE5F, // ﹟, SMALL NUMBER SIGN
		0xFE60, // ﹠, SMALL AMPERSAND
		0xFE6A, // ﹪, SMALL PERCENT SIGN
		0xFE6B, // ﹫, SMALL COMMERCIAL AT
		0xFF03, // ＃, FULLWIDTH NUMBER SIGN
		0xFF05, // ％, FULLWIDTH PERCENT SIGN
		0xFF06, // ＆, FULLWIDTH AMPERSAND
		0xFF20: // ＠, FULLWIDTH COMMERCIAL AT
		return true
	}
	return false
}

// canReceiveTextEmphasis reports whether a character may receive text-emphasis
// marks. Mirrors Blink's Character::CanReceiveTextEmphasis @ character.cc
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, implementing CSS Text Decor L3/L4
// emphasis skip rules: suppress marks over whitespace, control, format,
// NotAssigned, and punctuation — except the 23-codepoint symbol-like exception list.
func canReceiveTextEmphasis(r rune) bool {
	// Skip whitespace (covers Zs, Zl U+2028, Zp U+2029, and ASCII controls
	// via Go's unicode.IsSpace), control (Cc), format (Cf), and unassigned (Cn).
	if unicode.IsSpace(r) ||
		unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cn, r) {
		return false
	}
	// Skip punctuation, unless it's a symbol-like exception.
	if unicode.IsPunct(r) {
		return emphasisPunctuationException(r)
	}
	return true
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
//
// onlyLineThrough controls the paint phase (see drawOneAppliedTextDecoration).
func (r *Renderer) drawTextDecoration(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64, onlyLineThrough bool) {
	// Fast path when there are no accumulated decorations and no legacy entry.
	if !layer.hasTextDecoration() {
		return
	}

	metrics := r.dc.GetFontMetrics(fontID)
	descent := float64(metrics.Descent) / 64.0
	// The lines span the fragment's laid-out inline size
	// (PaintLayer.decorationInlineSize).
	inlineSize := layer.decorationInlineSize()

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
	// The trims are measured the way the inline size is laid out, letter-
	// and word-spacing included (measureSegmentAdvance), and each is the
	// whole text's advance less the rest's, so the whitespace's contribution
	// matches its in-context advance.
	var skipStartWidth, skipEndWidth float64
	if box != nil && box.Style != nil && text != "" {
		skip := box.Style.GetTextDecorationSkipSpaces()
		// trimmable reports whether part is whitespace to trim at an end
		// of text, neither empty nor all of it.
		trimmable := func(part string) bool { return part != "" && part != text }
		var leading, trailing string
		if skip.SkipStart {
			leading = leadingWhitespacePrefix(text)
		}
		if skip.SkipEnd {
			trailing = trailingWhitespaceSuffix(text)
		}
		if trimmable(leading) || trimmable(trailing) {
			textAdvance := r.measureSegmentAdvance(text, fontID, layer)
			// trimOf is the advance of text beyond rest, a part of it.
			trimOf := func(rest string) float64 {
				return max(textAdvance-r.measureSegmentAdvance(rest, fontID, layer), 0)
			}
			if trimmable(leading) {
				skipStartWidth = trimOf(text[len(leading):])
			}
			if trimmable(trailing) {
				skipEndWidth = trimOf(text[:len(text)-len(trailing)])
			}
		}
	}

	if len(layer.AppliedTextDecorations) > 0 {
		// TODO: pass the font's underline-thickness metric here once it's
		// plumbed through textshape.FontMetrics; 0 makes from-font fall back
		// to auto (which is correct, just lossy).
		info := newTextDecorationInfo(box, inlineSize, layer.FontSize, ascent, descent, 0)
		// CSS Text Decor 4 §1.1.5 text-decoration-skip-ink: when `auto`
		// (default), the under/overline is interrupted where it crosses a
		// glyph's ink bounding box.
		//
		// IMPLEMENTATION SCOPE — louis14 today does not have per-glyph ink
		// bounding boxes; the closest stand-in is the per-character advance,
		// which is correct ONLY for Ahem (1em-square solid glyphs where
		// advance ≡ ink). For real fonts (Liberation, Atkinson, …) the
		// advance is a broad superset of the ink — applying skip-ink on
		// advance over-skips and regresses text-decoration-inset-*, text-
		// decoration-skip-spaces-*, and text-underline-position-* tests whose
		// Blink-generated references show uninterrupted underlines (Blink's
		// skip-ink uses real ink bboxes and doesn't fire there). Until a
		// glyph-ink-bbox API ships through textshape, gate skip-ink on
		// `FontAhem` so it stays a no-op for real fonts and matches the
		// Blink references on those.
		glyphs := r.skipInkGlyphs(layer, text, fontID, box.X)
		for _, td := range layer.AppliedTextDecorations {
			r.drawOneAppliedTextDecoration(td, info, box, inlineSize, skipStartWidth, skipEndWidth, glyphs, onlyLineThrough)
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

	var rectTop, centerY, wavyOffset float64
	switch layer.TextDecoration {
	case css.TextDecorationUnderline:
		if onlyLineThrough {
			return
		}
		// text-underline-offset:0 / position:auto sits at the alphabetic baseline
		// (box.Y+ascent), no descent gap — matching computeUnderlineZeroPosition
		// and the spec (CSS Text Decor 4 §line-offset-zero). The pre-port
		// descent*0.25 term here was a stale relic; it leaked the underline below
		// the glyph and broke text-underline-offset-zero-position.html (LOU-333).
		//
		// Rounded to the integer pixel grid like drawOneAppliedTextDecoration's
		// lineY (its "aligns with the rounded baseline" comment): an
		// unrounded fractional ascent (e.g. font-size: 300%) lands the
		// 1px-thick rect straddling a pixel boundary, which the
		// antialiased rasterizer spreads across two rows instead of one —
		// a 1px-too-tall underline vs. the WPT reference
		// (active-selection-014.html, LOU-344: ::selection's own
		// text-decoration takes this legacy path).
		centerY = math.Round(box.Y + ascent + layer.TextUnderlineOffset)
		rectTop = centerY // already Blink's paint_underline_offset (rect TOP)
		wavyOffset = doubleOffsetFromThickness(thickness)
	case css.TextDecorationOverline:
		if onlyLineThrough {
			return
		}
		centerY = box.Y
		rectTop = centerY - thickness // overline rect grows UPWARD from TextTop
		wavyOffset = -doubleOffsetFromThickness(thickness)
	case css.TextDecorationLineThrough:
		if !onlyLineThrough {
			return
		}
		centerY = box.Y + ascent*0.65
		rectTop = centerY - thickness/2 // the solid line is centered on centerY
	default:
		return
	}

	legacyXStart := box.X + skipStartWidth
	legacyXEnd := box.X + inlineSize - skipEndWidth
	if legacyXEnd <= legacyXStart {
		return
	}
	if st, ok := decorationStrokeStyle(layer.TextDecorationStyle); ok {
		r.drawLineAsStroke(legacyXStart, rectTop, legacyXEnd-legacyXStart, thickness, st, nil)
		return
	}
	switch layer.TextDecorationStyle {
	case "double":
		r.drawDoubleLine(legacyXStart, centerY, legacyXEnd, centerY, thickness)
	case "wavy":
		r.drawWavyLine(legacyXStart, rectTop, legacyXEnd-legacyXStart, thickness, wavyOffset, nil)
	default: // "solid"
		r.drawLineAsRect(legacyXStart, rectTop, legacyXEnd-legacyXStart, thickness)
	}
}

// drawOneAppliedTextDecoration paints a single AppliedTextDecoration entry on a
// text run. Geometry (resolved thickness + per-line Y) comes from the shared
// textDecorationInfo built by drawTextDecoration. Mirrors one iteration of
// Blink's `TextDecorationInfo` paint loop (core/paint/text_decoration_info.cc
// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// applyDecorationInset applies text-decoration-inset to an already-positioned
// decoration extent [start, end], returning the adjusted [start, end].
//
// It handles the HasDecoratingBox × RTL × IsFirst/IsLastFragment matrix:
//   - HasDecoratingBox: inset is applied only at the actual box edges (first/
//     last fragment flags) so interior line-break boundaries are untouched.
//   - Bare path: inset is applied unconditionally to both edges.
//   - RTL: InlineStart trims the physical END; InlineEnd trims (or extends,
//     when negative) the physical START — opposite of the LTR convention.
func applyDecorationInset(start, end float64, td css.AppliedTextDecoration, isRTL bool) (float64, float64) {
	if td.HasDecoratingBox {
		if isRTL {
			if td.IsFirstFragment {
				end -= td.Inset.InlineStart // trim right
			}
			if td.IsLastFragment {
				start += td.Inset.InlineEnd // extend left
			}
		} else {
			if td.IsFirstFragment {
				start += td.Inset.InlineStart // trim left
			}
			if td.IsLastFragment {
				end -= td.Inset.InlineEnd // extend right
			}
		}
	} else {
		if isRTL {
			start += td.Inset.InlineEnd // negative InlineEnd → extends left
			end -= td.Inset.InlineStart
		} else {
			start += td.Inset.InlineStart
			end -= td.Inset.InlineEnd
		}
	}
	return start, end
}

// computeDecorationExtent returns the logical (physical-axis) start and end of
// the decoration line, with text-decoration-inset applied.
//   - HasDecoratingBox: the extent spans the layout-stamped decorating-box width.
//   - Bare (single-fragment fallback): the extent spans [boxX, boxX+inlineSize].
//
// Inset adjustment is delegated to applyDecorationInset.
func computeDecorationExtent(td css.AppliedTextDecoration, isRTL bool, boxX, inlineSize float64) (logicalStart, logicalEnd float64) {
	if td.HasDecoratingBox {
		logicalStart = boxX + td.DecoratingBoxOffsetX
		logicalEnd = logicalStart + td.DecoratingBoxWidth
	} else {
		logicalStart = boxX
		logicalEnd = boxX + inlineSize
	}
	return applyDecorationInset(logicalStart, logicalEnd, td, isRTL)
}

// applySkipSpacesTrim trims leading/trailing whitespace from the decoration
// extent (text-decoration-skip-spaces). Applied after inset so a negative
// inset (extension) does not extend through skipped whitespace.
// For the HasDecoratingBox case, trimming only applies at the actual box
// edge (first/last fragment) so interior line-break boundaries are untouched.
func applySkipSpacesTrim(start, end, skipStart, skipEnd float64, hasDecoratingBox, isFirst, isLast bool) (float64, float64) {
	if skipStart > 0 && (!hasDecoratingBox || isFirst) {
		start += skipStart
	}
	if skipEnd > 0 && (!hasDecoratingBox || isLast) {
		end -= skipEnd
	}
	return start, end
}

// clampDecorationBounds clamps the painting extent to the fragment's physical
// boundary for the HasDecoratingBox case. Interior edges are ceil'd to prevent
// sub-pixel double-coverage at adjacent fragment joints. The ceil is a
// louis14 stand-in (no Blink analog): Blink paints each fragment's stretch
// from its unsnapped origin (TextDecorationInfo local_origin_ / width_,
// text_decoration_info.cc:125-145 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), whose joints land on whole
// pixels when advances are rounded (SkiaTextMetrics without subpixel
// positioning, platform/fonts/skia/skia_text_metrics.cc:32-33, 57-71).
// Dropping it waits on that text-metrics port (LOU-654 finding).
//
// The inline-start edge (physical left for LTR, physical right for RTL) is NOT
// clamped on the first fragment, and the inline-end edge (physical right for
// LTR, physical left for RTL) is NOT clamped on the last fragment, because a
// negative inset legitimately extends the decoration past those edges.
//
// RTL flips which physical edge is "inline-start" vs "inline-end":
//
//	LTR: inline-start = physical LEFT  (IsFirstFragment), inline-end = RIGHT (IsLastFragment)
//	RTL: inline-start = physical RIGHT (IsFirstFragment), inline-end = LEFT  (IsLastFragment)
//
// Returns (xStart, xEnd, skip) where skip=true means the extent collapsed.
func clampDecorationBounds(logicalStart, logicalEnd float64, td css.AppliedTextDecoration, box *layout.Box, inlineSize float64) (xStart, xEnd float64, skip bool) {
	xStart, xEnd = logicalStart, logicalEnd
	if td.HasDecoratingBox {
		// first/last track which fragment edge is free from clamping.
		// RTL swaps inline-start (physical right) and inline-end (physical left),
		// so the free-left and free-right fragment flags are exchanged.
		first, last := td.IsFirstFragment, td.IsLastFragment
		if td.SourceStyle != nil && td.SourceStyle.GetDirection() == css.DirectionRTL {
			first, last = last, first
		}
		if !first && box.X > xStart {
			xStart = math.Ceil(box.X)
		}
		if !last && box.X+inlineSize < xEnd {
			xEnd = box.X + inlineSize
		}
		if xEnd <= xStart {
			return xStart, xEnd, true
		}
	}
	return xStart, xEnd, false
}

// drawLineThroughStroke paints a line-through in the style given by styleVal
// ("solid", "dashed", "dotted", "double", "wavy"). lineY is the stroke
// center, thickness the stroke width.
func (r *Renderer) drawLineThroughStroke(xStart, xEnd, lineY, thickness float64, styleVal string) {
	if st, ok := decorationStrokeStyle(styleVal); ok {
		r.drawLineAsStroke(xStart, lineY-thickness/2, xEnd-xStart, thickness, st, nil)
		return
	}
	switch styleVal {
	case "double":
		r.drawDoubleLine(xStart, lineY, xEnd, lineY, thickness)
	case "wavy":
		r.drawWavyLine(xStart, lineY-thickness/2, xEnd-xStart, thickness, 0, nil)
	default: // "solid"
		r.drawLineAsRect(xStart, lineY-thickness/2, xEnd-xStart, thickness)
	}
}

// drawLineAsRect paints a solid horizontal text-decoration line, the
// non-SVG branch of Blink's DrawLineAsRect (core/paint/
// decoration_line_painter.cc:104-124 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the rect is snapped on its
// y axis (SnapYAxis, :40-45 — top rounded half up, thickness floored to
// at least 1px, RoundDownThickness :21-23) so the line does not
// anti-alias vertically, then filled with the fill flags (anti-aliased).
// A path fill (not FillRectangle's fast path) keeps it inside the
// layer's overflow clip mask (text-underline-offset-scroll-001).
func (r *Renderer) drawLineAsRect(x, y, w, thickness float64) {
	top := math.Floor(y + 0.5)
	h := math.Max(math.Floor(thickness), 1)
	r.dc.DrawRectangle(x, top, w, h)
	r.dc.Fill()
}

// appliedDecorationIsRTL reports whether the decoration's inline axis runs
// right-to-left. It prefers box.Style (normal horizontal path) and falls back
// to td.SourceStyle for the sideways path where the synthetic virtualBox has
// no Style attached.
func appliedDecorationIsRTL(box *layout.Box, td css.AppliedTextDecoration) bool {
	if box.Style != nil {
		return box.Style.GetDirection() == css.DirectionRTL
	}
	return td.SourceStyle != nil && td.SourceStyle.GetDirection() == css.DirectionRTL
}

// td.Color is already resolved at cascade time per CSS Text Decor 3 §2 — no
// currentcolor substitution needed here.
// onlyLineThrough=false: draw underline+overline (called before text glyphs).
// onlyLineThrough=true:  draw line-through only (called after text glyphs).
// Mirrors Blink TextPainter::PaintDecorationsExceptLineThrough /
// PaintDecorationLineThrough split in text_fragment_painter.cc @ 4883d11f.
func (r *Renderer) drawOneAppliedTextDecoration(td css.AppliedTextDecoration, info textDecorationInfo, box *layout.Box, inlineSize, skipStartWidth, skipEndWidth float64, glyphs []glyphExtent, onlyLineThrough bool) {
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
	isRTL := appliedDecorationIsRTL(box, td)
	logicalStart, logicalEnd := computeDecorationExtent(td, isRTL, box.X, inlineSize)
	logicalStart, logicalEnd = applySkipSpacesTrim(
		logicalStart, logicalEnd, skipStartWidth, skipEndWidth,
		td.HasDecoratingBox, td.IsFirstFragment, td.IsLastFragment,
	)
	if logicalEnd <= logicalStart {
		return
	}

	// Clamp at INTERIOR fragment boundaries only. The first fragment's left
	// edge and the last fragment's right edge are the decorating box's OUTER
	// edges where a negative text-decoration-inset (= extension) legitimately
	// overflows the fragment. Clamp uses the inline size (layout-time) to match
	// the layout-time DecoratingBoxOffsetX / Width values; clamping by a
	// paint-time measurement would leak sub-pixel drift between adjacent
	// fragments.
	//
	// Interior left edge: ceil to the next integer pixel boundary. When
	// adjacent fragments share a sub-pixel boundary (e.g. xEnd=467.5 for
	// fragment N, xStart=467.5 for fragment N+1), the sharp-pixel rasterizer
	// gives non-zero coverage to pixel 467 for BOTH rects. Ceiling the
	// interior left edge prevents the later-painted fragment from overwriting
	// a child element's decoration that was painted on top of the earlier
	// fragment's decoration at that same pixel. The previous fragment's right
	// edge already covers the pixel (its coverage is clamped at box.X +
	// inlineSize ≤ xEnd_ceil), so no visual gap is introduced. Blink avoids
	// this entirely by painting each decorating box's line in one rect rather
	// than per fragment; ceil is the louis14-specific equivalent.
	xStart, xEnd, skip := clampDecorationBounds(logicalStart, logicalEnd, td, box, inlineSize)
	if skip {
		return
	}

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
	// glyphYTop and glyphYBottom approximate the vertical (perpendicular)
	// extent of the glyph ink for horizontal text. For Ahem (1em-square
	// solid glyphs) this is exact — the em-square IS the ink. For real
	// fonts it's a conservative superset (the box can overestimate ink),
	// matching the spec's intent of skipping where the line "would cross"
	// the glyph; non-Ahem cases are not covered by the C65 reftests but
	// the gate prevents over-eager skipping when the underline is offset
	// well below the text (text-decoration-skip-ink-003 — `text-underline-
	// offset: .5em` puts the line BELOW the em-square, and skip-ink must
	// NOT trigger).
	glyphYTop := box.Y
	glyphYBottom := box.Y + info.ascent + info.descent

	// rectTop = the y-coordinate of the rect's TOP edge for the SOLID path.
	// centerY = the y-coordinate of the stroke's CENTER for non-solid styles.
	// skipInk: when true and the decoration's perpendicular position overlaps
	// the glyph ink extent, the paint is split into sub-segments that avoid
	// the glyph extents (CSS Text Decor 4 §1.1.5). Skip-ink does NOT apply
	// to line-through per spec. wavyOffset places a wavy line (drawWavyLine).
	paintLine := func(rectTop, centerY, wavyOffset float64, skipInk bool) {
		segments := []inkSegment{{Start: xStart, End: xEnd}}
		// Apply skip-ink only when the line's perpendicular extent overlaps
		// the glyph ink: its rect [rectTop, rectTop+thickness], or a wavy
		// line's paint rect (DecorationLinePainter::Bounds).
		lineTop, lineBottom := rectTop, rectTop+thickness
		if td.Style == "wavy" {
			top, height := wavyPaintRect(rectTop, thickness, wavyOffset)
			lineTop, lineBottom = top, top+height
		}
		skipped := skipInk && len(glyphs) > 0 && decorationBandCrossesGlyphs(lineTop, lineBottom, glyphYTop, glyphYBottom)
		if skipped {
			segments = skipInkSegments(xStart, xEnd, thickness, glyphs)
		}
		// Dashed, dotted and wavy lines paint the fragment's whole line, their
		// pattern starting at its own start; skip-ink clips.
		var keep []inkSegment
		if skipped {
			keep = segments
		}
		if st, ok := decorationStrokeStyle(td.Style); ok {
			r.drawLineAsStroke(xStart, rectTop, xEnd-xStart, thickness, st, keep)
			return
		}
		if td.Style == "wavy" {
			r.drawWavyLine(xStart, rectTop, xEnd-xStart, thickness, wavyOffset, keep)
			return
		}
		for _, seg := range segments {
			switch td.Style {
			case "double":
				r.drawDoubleLine(seg.Start, centerY, seg.End, centerY, thickness)
			default: // "solid"
				r.drawLineAsRect(seg.Start, rectTop, seg.End-seg.Start, thickness)
			}
		}
	}

	if !onlyLineThrough && td.Lines.Has(css.TextDecorationLineUnderline) {
		// computeUnderlineLineY returns Blink's `paint_underline_offset`
		// (= rect TOP for solid). Round to integer pixels so the underline
		// aligns with the rounded baseline used by drawTextStr (math.Round),
		// preventing a sub-pixel gap that exposes the underline below the glyph.
		lineY := info.underlineRectTop(td)
		paintLine(lineY, lineY, doubleOffsetFromThickness(thickness), true)
	}
	if !onlyLineThrough && td.Lines.Has(css.TextDecorationLineOverline) {
		// computeOverlineLineY returns the TextTop position (box.Y), which
		// is the stroke center for the non-solid path (matches the
		// pre-port baseline). Blink's overline rect spans
		// [TextTop - thickness, TextTop], so for the SOLID path the rect
		// TOP is `TextTop - thickness` (overline grows UPWARD from
		// text-top — see text-decoration-thickness-overline-001).
		top := info.overlineRectTop(thickness)
		paintLine(top, top+thickness, -doubleOffsetFromThickness(thickness), true)
	}
	if onlyLineThrough && td.Lines.Has(css.TextDecorationLineLineThrough) {
		// Line-through stays stroke-centered for all styles. Blink centers
		// at `2*ascent/3` (it adds the `-thickness/2` offset to keep the
		// stroke around that center point). Switching to rect-top would
		// regress text-decoration-thickness-linethrough-001 which uses
		// 1.1em thickness on a 1em-tall box.
		lineY := info.computeLineThroughLineY(thickness)
		r.drawLineThroughStroke(xStart, xEnd, lineY, thickness, td.Style)
	}
}

// hasTextDecoration reports whether the layer carries any text decoration.
func (layer *PaintLayer) hasTextDecoration() bool {
	return len(layer.AppliedTextDecorations) > 0 ||
		(layer.TextDecoration != css.TextDecorationNone && layer.TextDecoration != "")
}

// skipInkGlyphs returns a run's glyph extents along its inline axis from
// inlineStart, for skip-ink, or nil when the run skips none. Gated on Ahem,
// whose glyphs fill their advance: louis14 has no glyph ink bounds yet, and
// on other fonts the advance over-skips (see drawTextDecoration).
func (r *Renderer) skipInkGlyphs(layer *PaintLayer, text string, fontID int32, inlineStart float64) []glyphExtent {
	if layer.TextDecorationSkipInk != css.TextDecorationSkipInkAuto || !layer.FontAhem {
		return nil
	}
	return r.buildGlyphExtentsHorizontal(text, fontID, layer.featureInputs(), inlineStart, layer.LetterSpacing, layer.WordSpacing)
}

// drawLineRelativeTextDecoration paints a central-baseline vertical run's
// decorations line-relative, inlineLen long down from box.Y: upright runs,
// and mixed-orientation runs whose glyphs rotate, which Blink decorates
// alike (TextFragmentPainter::Paint rotates the context by the writing-mode
// rotation either way, core/paint/text_fragment_painter.cc:369-379 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). glyphs are the run's glyph
// extents along the inline axis for skip-ink, nil when it skips none.
// onlyLineThrough picks the line-through, painted after the glyphs, over
// the under- and overlines, painted before them, as drawTextDecoration's
// does.
func (r *Renderer) drawLineRelativeTextDecoration(layer *PaintLayer, box *layout.Box, inlineLen float64, fontID int32, ascent float64, glyphs []glyphExtent, onlyLineThrough bool) {
	r.dc.Push() // the decorations' colours stay off the glyphs
	defer r.dc.Pop()

	metrics := r.dc.GetFontMetrics(fontID)
	descent := float64(metrics.Descent) / 64.0

	// Upright text's line-under side is physical left in vertical-rl and
	// vertical-lr alike: Blink rotates both clockwise (TextFragmentPainter::
	// Paint, core/paint/text_fragment_painter.cc:369-379 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), as Chrome draws them. flip
	// swaps the underline to the over side and the overline to the under
	// side (shouldFlipUnderlineAndOverline).
	dir := blockUnderLeft

	info := newVerticalTextDecorationInfo(box, inlineLen, layer.FontSize, ascent, descent, 0, dir)
	info.flipUnderlineAndOverline = shouldFlipUnderlineAndOverline(layer)

	if len(layer.AppliedTextDecorations) > 0 {
		for _, td := range layer.AppliedTextDecorations {
			r.drawOneAppliedVerticalDecoration(td, info, glyphs, onlyLineThrough)
		}
		return
	}

	// Legacy single-decoration fallback.
	thickness := math.Max(1.0, math.Round(layer.TextDecorationThickness))
	r.setColor(layer.TextDecorationColor)

	if (layer.TextDecoration == css.TextDecorationLineThrough) != onlyLineThrough {
		return // painted in the other pass
	}
	var rectLeft float64
	switch layer.TextDecoration {
	case css.TextDecorationUnderline:
		rectLeft, _ = info.underlineRectLeft(layer.TextUnderlineOffset, thickness)
	case css.TextDecorationOverline:
		rectLeft, _ = info.overlineRectLeft(thickness)
	case css.TextDecorationLineThrough:
		perpX := info.computeLineThroughPerpX()
		rectLeft = perpX - thickness/2
	default:
		return
	}
	r.dc.DrawRectangle(rectLeft, box.Y, thickness, inlineLen)
	r.dc.Fill()
}

// drawOneAppliedVerticalDecoration paints a single AppliedTextDecoration entry
// for an upright vertical text run. Perpendicular coordinate (physical X) is
// derived from textDecorationInfo with axis=decorationAxisVertical; the line
// itself paints line-relative (see paintLine), each line on the side
// underlineRectLeft / overlineRectLeft resolve.
func (r *Renderer) drawOneAppliedVerticalDecoration(td css.AppliedTextDecoration, info textDecorationInfo, glyphs []glyphExtent, onlyLineThrough bool) {
	if td.Lines.IsNone() {
		return
	}
	thickness := info.computeThickness(td)
	r.setColor(td.Color)

	// paintLine paints one decoration line-relative: Blink rotates the
	// context by the writing-mode rotation and paints the decoration as in
	// horizontal text (TextFragmentPainter::Paint, core/paint/
	// text_fragment_painter.cc:369-379, :686-692 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; DecorationLinePainter::Paint,
	// decoration_line_painter.cc:437-476). Rotating 90° clockwise about
	// (rectLeft + thickness, 0) maps the line-relative rect at (lineStart, 0),
	// inlineLen x thickness, onto the physical column [rectLeft,
	// rectLeft + thickness] running down from lineStart, trimmed or extended
	// by text-decoration-inset. skipInk lets an under- or overline that
	// crosses the glyphs' column skip their ink, as horizontal text does.
	//
	// doubleOffset is where a double decoration's second line sits,
	// line-relative along y: thickness + 1 away from the text for under and
	// over, floor(thickness + 1) toward line-under for line-through;
	// wavyOffset where a wavy line's centreline sits, the same but 0 for
	// line-through (TextDecorationInfo::
	// ComputeLineData, text_decoration_info.cc:255-282;
	// DecorationLinePainter::Paint's kDoubleStroke,
	// decoration_line_painter.cc:464-473). Under the rotation, line-relative
	// +y is physical -x.
	rtl := td.SourceStyle != nil && td.SourceStyle.GetDirection() == css.DirectionRTL
	lineStart, lineEnd := applyDecorationInset(info.box.Y, info.box.Y+info.width, td, rtl)
	if lineEnd <= lineStart {
		return
	}
	paintLine := func(rectLeft, doubleOffset, wavyOffset float64, skipInk bool) {
		// The line's bounds across it, physical x: its rect, or a wavy
		// line's paint rect mapped through the rotation (line-relative y
		// runs from rectLeft + thickness toward physical -x).
		lo, hi := rectLeft, rectLeft+thickness
		if td.Style == "wavy" {
			top, height := wavyPaintRect(0, thickness, wavyOffset)
			lo, hi = rectLeft+thickness-(top+height), rectLeft+thickness-top
		}
		var keep []inkSegment
		if skipInk && len(glyphs) > 0 && decorationBandCrossesGlyphs(lo, hi, info.box.X, info.box.X+info.box.Width) {
			keep = skipInkSegments(lineStart, lineEnd, thickness, glyphs)
		}
		r.dc.Push()
		defer r.dc.Pop()
		r.dc.Translate(rectLeft+thickness, 0)
		r.dc.Rotate(math.Pi / 2)
		if st, ok := decorationStrokeStyle(td.Style); ok {
			r.drawLineAsStroke(lineStart, 0, lineEnd-lineStart, thickness, st, keep)
			return
		}
		if td.Style == "wavy" {
			r.drawWavyLine(lineStart, 0, lineEnd-lineStart, thickness, wavyOffset, keep)
			return
		}
		segments := keep
		if segments == nil {
			segments = []inkSegment{{Start: lineStart, End: lineEnd}}
		}
		for _, seg := range segments {
			r.drawLineAsRect(seg.Start, 0, seg.End-seg.Start, thickness)
			if td.Style == "double" {
				r.drawLineAsRect(seg.Start, doubleOffset, seg.End-seg.Start, thickness)
			}
		}
	}

	// A double or wavy line's offset leads away from the text on the side
	// the line lands on.
	paintOnSide := func(rectLeft float64, side blockUnderDir) {
		offset := awayFromText(side) * doubleOffsetFromThickness(thickness)
		paintLine(rectLeft, offset, offset, true)
	}
	if !onlyLineThrough && td.Lines.Has(css.TextDecorationLineUnderline) {
		paintOnSide(info.underlineRectLeft(td.UnderlineOffset, thickness))
	}
	if !onlyLineThrough && td.Lines.Has(css.TextDecorationLineOverline) {
		paintOnSide(info.overlineRectLeft(thickness))
	}
	if onlyLineThrough && td.Lines.Has(css.TextDecorationLineLineThrough) {
		paintLine(info.computeLineThroughPerpX()-thickness/2, awayFromText(info.underDir)*math.Floor(doubleOffsetFromThickness(thickness)), 0, false)
	}
}

// awayFromText is the line-relative y direction (see
// drawOneAppliedVerticalDecoration's rotation) that leads away from the text
// for a decoration on the dir side: physical left (blockUnderLeft) is
// line-relative +y, physical right -y.
func awayFromText(dir blockUnderDir) float64 {
	if dir == blockUnderLeft {
		return 1
	}
	return -1
}

// vertDecorationRectLeft computes the left X edge of a vertical decoration rect.
//
// perpX is the "inner edge" coordinate — the edge of the decoration nearest to
// the text glyph column:
//   - blockUnderLeft (vertical-rl, under=left): inner edge at the LEFT of text,
//     decoration extends further LEFT → rectLeft = perpX - thickness
//   - blockUnderRight (vertical-lr, under=right): inner edge at the RIGHT of text,
//     decoration extends further RIGHT → rectLeft = perpX
func vertDecorationRectLeft(perpX, thickness float64, dir blockUnderDir) float64 {
	if dir == blockUnderLeft {
		return perpX - thickness
	}
	return perpX
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

// mirrorCoord reflects v into [lo, hi] about the edges, repeating with
// period 2(hi - lo) (a triangle wave). It is SVG spreadMethod="reflect" and
// Skia's SkTileMode::kMirror.
func mirrorCoord(v, lo, hi float64) float64 {
	if lo <= v && v <= hi {
		return v
	}
	w := hi - lo
	if w <= 0 {
		return lo
	}
	t := wrapIntoPeriod(v-lo, 2*w)
	if t > w {
		t = 2*w - t
	}
	return lo + t
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

// applySpreadToShadowShape expands (or shrinks, for negative spread) shadow
// radii by the spread distance, mirroring Blink's ApplySpreadToShadowShape
// (box_painter_base.cc:91 @ 4883d11fef4a): coverage-factor corner correction
// against the pre-spread box (w, h), then radius constraining against the
// spread-adjusted box (outW, outH). No-op when spread is zero, matching
// Blink's early return.
func applySpreadToShadowShape(radii css.EllipticalRadii, spread, w, h, outW, outH float64) css.EllipticalRadii {
	if spread == 0 {
		return radii
	}
	return radii.
		OutsetWithCornerCorrectionUsingCoverageFactor(spread, spread, spread, spread, w, h).
		ConstrainRadii(outW, outH)
}

// drawOutsetBoxShadows paints outset box shadows behind the element.
// Shadows are painted in reverse declaration order (last = behind).
func (r *Renderer) drawOutsetBoxShadows(layer *PaintLayer, rect layout.PhysicalRect) {
	x, y, w, h := pixelSnap(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height)

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

		// Expand border radii by spread for the shadow shape.
		shadowRadii := applySpreadToShadowShape(layer.BorderRadius, shadow.Spread, w, h, sw, sh)

		// Use offscreen buffer to clip out the border box from the shadow.
		// Per CSS spec: outset shadows are only visible outside the border edge.
		r.drawOutsetShadowBuffer(sx, sy, sw, sh, shadowRadii,
			x, y, w, h, layer.BorderRadius, shadow)
	}
}

// drawOutsetShadowBuffer renders an outset shadow using an offscreen buffer.
// Fills the shadow shape, optionally blurs it, clears the border box area,
// then composites.
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
	if !effectBufferUsable(bufW, bufH) {
		return
	}

	buf := image.NewRGBA(image.Rect(0, 0, bufW, bufH))

	childDC := r.dc.NewChildContext(buf)
	childDC.SetColor(premultipliedRGBA(shadow.Color))

	lsx, lsy := sx-minX, sy-minY
	lbx, lby := bx-minX, by-minY

	// Fill the shadow shape and blur it whole: the blur is a draw looper on
	// the fill (BoxPainterBase::PaintNormalBoxShadow, box_painter_base.cc:
	// 348-353, :383 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	if !shadowRadii.IsZero() {
		buildRoundedRectPathOnDC(childDC, lsx, lsy, sw, sh, shadowRadii)
	} else {
		childDC.DrawRectangle(lsx, lsy, sw, sh)
	}
	childDC.Fill()
	if shadow.Blur > 0 {
		gaussianBlur3Box(buf, int(math.Round(sigma)))
	}
	// Then clip out the border box (ClipOutContouredRect / ClipOut,
	// box_painter_base.cc:175-197). Cutting it before the blur would spread
	// the hole outward, leaving the shadow see-through next to the box. The
	// hole is cleared by its anti-aliased coverage, as the border painter's
	// clip-out (paintClippedOut) cuts the same rrect.
	r.clearHoleFromBuffer(buf, lbx, lby, bw, bh, borderRadii)

	r.dc.DrawImage(buf, int(math.Round(minX)), int(math.Round(minY)))
}

// clearHoleFromBuffer clears (toward transparent) the pixels of buf that fall
// inside the given rectangle, optionally with rounded corners. It rasterizes
// the shape to an alpha mask using the draw context's rasterizer — so rounded
// corners match other path rendering exactly — then multiplies each covered
// buffer pixel toward zero by the mask's inverse coverage. Shared by the outset
// shadow's hole clear and the inset shadow's inner-hole clear. Coordinates are buffer-local.
func (r *Renderer) clearHoleFromBuffer(buf *image.RGBA, x, y, w, h float64, radii css.EllipticalRadii) {
	if w <= 0 || h <= 0 {
		return
	}
	bounds := buf.Bounds()
	bw, bh := bounds.Dx(), bounds.Dy()

	holeMask := image.NewRGBA(image.Rect(0, 0, bw, bh))
	holeDC := r.dc.NewChildContext(holeMask)
	holeDC.SetColor(color.White)
	if !radii.IsZero() {
		buildRoundedRectPathOnDC(holeDC, x, y, w, h, radii)
	} else {
		holeDC.DrawRectangle(x, y, w, h)
	}
	holeDC.Fill()

	// The mask is non-zero only within the hole's bounding box, so clamp the
	// scan to it: the buffer is deliberately expanded by the blur radius on all
	// sides, so walking its transparent margin is pure waste.
	x0, y0 := max(0, int(math.Floor(x))), max(0, int(math.Floor(y)))
	x1, y1 := min(bw, int(math.Ceil(x+w))), min(bh, int(math.Ceil(y+h)))
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			moff := py*holeMask.Stride + px*4
			a := holeMask.Pix[moff+3]
			if a == 0 {
				continue
			}
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

// drawInsetBoxShadows paints inset box shadows inside the element,
// after background and borders. Per CSS spec, inset shadows are drawn
// inside the padding box, creating a "donut" between the box edge and
// the shadow inner rect.
func (r *Renderer) drawInsetBoxShadows(layer *PaintLayer, rect layout.PhysicalRect) {
	box := layer.Box
	// Inset shadows paint inside the padding box.
	px := rect.Offset.X + box.Border.Left
	py := rect.Offset.Y + box.Border.Top
	pw := rect.Size.Width - box.Border.Left - box.Border.Right
	ph := rect.Size.Height - box.Border.Top - box.Border.Bottom
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

		// Adjust inner radii by the negated spread (Blink passes
		// -shadow.Spread() for inset shadows, box_painter_base.cc:547
		// @ 4883d11fef4a): positive spread degenerates to a linear
		// per-dimension shrink, negative spread grows radii via the
		// coverage-factor correction.
		shadowInnerRadii := applySpreadToShadowShape(innerRadii, -shadow.Spread, pw, ph, iw, ih)

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

	// Fill the entire buffer with shadow color (premultiplied so the
	// later DrawImage composite matches Go's premultiplied color.RGBA).
	sc := premultipliedRGBA(shadow.Color)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			off := y*buf.Stride + x*4
			buf.Pix[off+0] = sc.R
			buf.Pix[off+1] = sc.G
			buf.Pix[off+2] = sc.B
			buf.Pix[off+3] = sc.A
		}
	}

	// Clear the inner hole (set to transparent). The inner edge is then blurred
	// below, which is correct for inset shadows: their inner boundary is soft.
	// An outset shadow, by contrast, cuts its border-box hole after its blur.
	r.clearHoleFromBuffer(buf, ix-bx, iy-by, iw, ih, innerRadii)

	// Apply box blur if needed.
	if shadow.Blur > 0 {
		gaussianBlur3Box(buf, int(math.Round(sigma)))
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

// gaussianBlur3Box approximates a Gaussian blur of standard deviation
// about radius with three box-blur passes. no Blink analog: Blink blurs
// with Skia's Gaussian image filter; louis14 rasterizes in Go.
func gaussianBlur3Box(img *image.RGBA, radius int) {
	boxBlur(img, radius)
	boxBlur(img, radius)
	boxBlur(img, radius)
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
