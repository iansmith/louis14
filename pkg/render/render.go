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

	// view paints the canvas background of the document being rendered
	// (ViewPainter, view_painter.go); nil when no root box. Set per
	// paintBoxes call.
	view *ViewPainter

	// nodeBoxIndex maps each html.Node to its primary layout box. Built once
	// per Render call in paintBoxes. Used by parentPerspectiveContext to
	// locate the perspective-establishing box for an abspos child whose
	// layout parent (containing block) differs from its DOM parent.
	nodeBoxIndex map[*html.Node]*layout.Box

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
	selectionRange       *html.Range
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
// Consumed by FilterEffectBuilder.BuildReferenceFilter's external
// branch. LOU-130 Phase 2 wiring.
func (r *Renderer) SetExternalSVGFetcher(fetcher func(uri string) ([]byte, error)) {
	r.externalSVGFetcher = fetcher
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
	r.embeddedViewport.W = 0
	r.embeddedViewport.H = 0

	r.tempFontIDs = r.tempFontIDs[:0]
	defer r.closeTempFonts()

	// The base background color behind the view background (ViewPainter::
	// PaintRootGroup, view_painter.cc:36-74 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
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
// The viewport is also recorded so the view background
// (ViewPainter.PaintRootElementGroup) covers only that rectangle.
func (r *Renderer) RenderEmbedded(boxes []*layout.Box, viewportW, viewportH float64) {
	r.embeddedViewport.W = viewportW
	r.embeddedViewport.H = viewportH

	r.tempFontIDs = r.tempFontIDs[:0]
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
// [RenderEmbedded]. The base white fill is the caller's responsibility; the
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
	// Build the renderer-wide SVG resource registry by walking every
	// box subtree for inline `<svg>` SVGRoots and merging their
	// per-root registries. `url(#id)` references on any element on
	// the page can then resolve against this single registry. See
	// svgResources comment on the Renderer type.
	r.svgResources = collectSVGResources(boxes)
	// `url(#id)` references resolve in the DOCUMENT tree scope, not the
	// local `<svg>`: a `<rect>` in one inline SVG can reference a
	// `<filter>` (or gradient/clip/mask) declared in a different inline
	// SVG on the same page. The SVG paint context uses each SVGRoot's
	// own per-root registry (svg_root_painter.go), so redistribute the
	// merged document-scope registry back into every SVGRoot's registry.
	// Mirrors Blink's SVGTreeScopeResources being a per-Document (tree
	// scope) namespace, not per outermost-svg.
	redistributeSVGResources(boxes, r.svgResources)
	// Phase 6: detect reference cycles. Cycle-flagged resources
	// (a self-referential `<mask>`, two masks pointing at each other,
	// etc.) are treated as `none` at paint time so the painters don't
	// infinite-loop. Mirrors Blink's SVGResourcesCycleSolver pass that
	// runs after the registry is built and before any paint dispatch.
	svg.SolveResourceCycles(r.svgResources)
	// Paint via PaintLayer tree (CSS 2.1 Appendix E stacking order). The
	// view paints the canvas background from inside the root layer's paint
	// (paintViewBackground); the root and a propagating body skip their own
	// (backgroundTransfersToView).
	for i, box := range boxes {
		layer := BuildPaintTree(box)
		if i == 0 {
			r.view = newViewPainter(r, box, layer)
		}
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

// redistributeSVGResources copies the merged document-scope registry
// back into every inline `<svg>` SVGRoot's per-root registry. Because
// mergeSVGRegistries uses the first-wins RegisterXxx methods, ids a root
// already owns are preserved; ids from other roots are added. This makes
// cross-SVG `url(#id)` references (e.g. a `<rect>` in one svg referencing
// a `<filter>` in another) resolve via the SVG paint context's local
// `root.Resources` lookup. Mirrors Blink's per-Document tree-scope
// resource namespace.
func redistributeSVGResources(boxes []*layout.Box, merged *svg.SVGResourceRegistry) {
	if merged == nil {
		return
	}
	for _, b := range boxes {
		redistributeSVGResourcesToBox(b, merged)
	}
}

// redistributeSVGResourcesToBox is the recursive worker for
// redistributeSVGResources: pre-order walk that back-merges the merged
// document-scope registry into each SVGRoot's per-root registry.
func redistributeSVGResourcesToBox(box *layout.Box, merged *svg.SVGResourceRegistry) {
	if box == nil {
		return
	}
	if box.LayoutNode != nil {
		if root, ok := box.LayoutNode.SVGRoot.(*svg.SVGRoot); ok && root != nil && root.Resources != nil {
			mergeSVGRegistries(root.Resources, merged)
		}
	}
	for _, child := range box.Children {
		redistributeSVGResourcesToBox(child, merged)
	}
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
		// Filter Effects 1 §3.3: when a filtered element has
		// `visibility: hidden`, the element itself does NOT paint, but its
		// filter STILL evaluates. The graph sees a transparent input — which
		// for generator primitives like feFlood / feImage still produces
		// non-empty output. Mirrors Blink ScopedSVGPaintState::Apply, which
		// installs the filter effect node unconditionally and lets the
		// element's content draw call become a no-op under
		// visibility:hidden.
		if layer.HasFilter {
			r.paintLayerWithFilter(layer)
			return
		}
		// Same spec rule on the SVG side: an inline <svg> with
		// `visibility: hidden` must still walk its subtree because
		// filtered descendants still apply their filters. The SVG paint
		// walk (paintSVGRoot → paintSVGNode → svgShapePainter.paint)
		// dispatches each element's filter BEFORE its own visibility check
		// (svg_shape_painter.go:50-63, svg_container_painter.go:124-126),
		// so the rect/text/g descendants of a hidden <svg> still emit
		// filter output. Without this entry, a hidden SVG root drops the
		// entire subtree before any filter can run — what
		// svg-visibility-hidden-element-with-filter-002/003 actually
		// require.
		// The view's style is not the root's: a hidden root still has its
		// background painted by the view (ViewPainter::
		// PaintBoxDecorationBackground checks the view's visibility,
		// view_painter.cc:77 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		r.paintViewBackground(layer)
		if isInlineSVGLayer(layer) {
			r.paintSVGRoot(layer)
		}
		return
	}
	if layer.Opacity <= 0 {
		return
	}
	// CSS Transforms L2 §11: `backface-visibility: hidden` skips the layer
	// (and its subtree) when the back face is presented to the viewer.
	// BackfaceHidden was computed at layer-build time from the element's
	// own transform composed with any preserve-3d ancestor transforms.
	// Mirrors Blink's PaintLayerPainter::ShouldPaintForBackfaceHidden gate at
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if layer.BackfaceHidden {
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
	// We still balance the Push/Pop and transformDepth so the paint walk
	// stays consistent.
	transformSingular := r.enterLayerTransform(layer)

	if !transformSingular {
		// Apply inherited ancestor overflow clips. Populated only on
		// layers that escaped a parent's overflow via z-list escalation
		// (positioned stacking-context children of an overflow-clipping
		// parent). See PaintLayer.AncestorOverflowClips for rationale.
		// Pushed innermost→outermost so the clip stack matches Blink's
		// property-tree-inherited clip nodes.
		for _, clip := range layer.AncestorOverflowClips {
			r.dc.Push()
			ox, oy, ow, oh := pixelSnap(clip[0], clip[1], clip[2], clip[3])
			r.dc.DrawRectangle(ox, oy, ow, oh)
			r.dc.Clip()
		}

		if layer.Opacity < 1.0 {
			// CSS3 Color §4.2: render subtree to offscreen buffer and composite.
			r.dc.PushGroup()
			r.paintLayerContent(layer)
			r.dc.PopGroupWithAlpha(layer.Opacity)
		} else {
			r.paintLayerContent(layer)
		}

		for range layer.AncestorOverflowClips {
			r.dc.Pop()
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
// whether the transform is singular (see applyTransforms). Pair with
// exitLayerTransform.
func (r *Renderer) enterLayerTransform(layer *PaintLayer) (singular bool) {
	r.paintViewBackground(layer)
	if layer.HasTransform {
		r.dc.Push()
		singular = r.applyTransforms(layer)
	}
	if layer.HasTransformPaint {
		r.transformDepth++
	}
	return singular
}

// exitLayerTransform undoes enterLayerTransform.
func (r *Renderer) exitLayerTransform(layer *PaintLayer) {
	if layer.HasTransformPaint {
		r.transformDepth--
	}
	if layer.HasTransform {
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
	// Cull edge-on flattened transforms before setting up the offscreen buffer.
	if isLayerTransformSingular(layer) {
		return
	}
	box := layer.Box
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.X+box.Width)) - bx + 1
	bh := int(math.Ceil(box.Y+box.Height)) - by + 1

	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
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
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
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
				r.enterLayerTransform(layer)
				r.paintLayerContent(layer)
				r.exitLayerTransform(layer)
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

// paintLayerWithFilter renders the entire layer subtree into an offscreen
// buffer sized to the filter region, runs the FilterEffect graph over it, and
// composites the result back. Mirrors Blink's FilterPainter: the element
// subtree is recorded into a source buffer, fed to the filter graph as
// SourceGraphic, and the graph output replayed.
func (r *Renderer) paintLayerWithFilter(layer *PaintLayer) {
	// Cull edge-on flattened transforms before setting up the offscreen buffer.
	if isLayerTransformSingular(layer) {
		return
	}
	box := layer.Box

	// Device transform carried by the DC at this paint scope. Two matrices
	// matter:
	//
	//   - dm: the ANCESTOR device matrix already on the DC (e.g. a
	//     `transform: scale(10)` on the parent). This is the matrix the
	//     source-render child DC must be seeded with, because
	//     paintLayerContentWithEffects re-applies the layer's OWN transform.
	//   - dmFull: ancestor ∘ own — the matrix the element's content actually
	//     lands at in device pixels. The filter region and the composite
	//     origin are computed from dmFull so a `transform` on the filtered
	//     element itself (and the filter result's final position) is honored.
	//
	// Blink rasterizes a filter's SourceGraphic at the device-pixel scale
	// (the "filter scale" = transform scale × device pixel ratio) so the
	// input — and therefore the crisp box edges — are sampled at the
	// resolution they are finally displayed at, not rasterized small and
	// bilinearly upscaled. When both matrices are identity (the common
	// no-transform case) this collapses to the original behaviour.
	dm := deviceMatrixFromDC(r.dc)
	dmFull := dm
	if layer.HasTransform {
		r.dc.Push()
		r.applyTransforms(layer)
		dmFull = deviceMatrixFromDC(r.dc)
		r.dc.Pop()
	}
	// The device-space path rasterizes the filter SourceGraphic at the
	// device-pixel scale (so a scaled box has crisp edges instead of being
	// rasterized small and bilinearly upscaled) and positions the result at
	// its transformed device rect. It engages only for an axis-aligned
	// (no rotation/skew) scale/translate, and only when:
	//
	//   - the ANCESTOR carries a scale (dm has scale), e.g.
	//     `transform: scale(10)` on the parent of a filtered element — the
	//     filter-scale-001 / filter-scaling-001 case; or
	//   - the element's OWN transform is a pure translate (no scale), e.g.
	//     `transform: translate(10px,10px)` on the filtered box — the
	//     svg-filter-{filter,primitive}-units-user-space container-3 case.
	//
	// A self-SCALED filtered element (own transform has a scale component but
	// no ancestor scale) deliberately keeps the legacy untransformed path:
	// louis14 also re-applies the ancestor filter to a transformed/composited
	// child layer, and engaging the device path there double-renders the
	// scaled region over the parent's region. Rotation/skew and projective 3D
	// transforms (deviceMatrixFromDC drops the perspective component) keep the
	// legacy path too — the 3D-transform filter tests rely on it.
	ancestorHasScale := !nearlyOne(dm.XX) || !nearlyOne(dm.YY)
	ownHasScale := !nearlyOne(dmFull.XX) || !nearlyOne(dmFull.YY)
	deviceScaled := isAxisAlignedScaleTranslate(dmFull) &&
		!isIdentityMatrix(dmFull) &&
		(ancestorHasScale || !ownHasScale)

	var referenceBox image.Rectangle
	if deviceScaled {
		// Reference box: the border box mapped into device pixels via the
		// full device matrix, corners rounded to align with the pixel-snapped
		// content rasterization.
		referenceBox = mapBoxToDeviceRect(dmFull, box.X, box.Y, box.Width, box.Height)
	} else {
		// Original untransformed path: border box at the layout position.
		rbx := int(math.Floor(box.X))
		rby := int(math.Floor(box.Y))
		rbw := int(math.Ceil(box.X+box.Width)) - rbx
		rbh := int(math.Ceil(box.Y+box.Height)) - rby
		referenceBox = image.Rect(rbx, rby, rbx+rbw, rby+rbh)
	}

	// SourceGraphic extent: the element subtree's paint bounds, which can
	// extend beyond the border box (positioned/overflowing descendants).
	// Blink's filter SourceGraphic is the recorded layer contents including
	// overflow; for drop-shadow especially the shadow is cast from the whole
	// painted shape, so the source buffer must cover it. drop-shadow-clipped
	// proves this: a descendant offset out of the border box still casts a
	// shadow. In the device-scaled path it is mapped through the full device
	// matrix so it lives in the same device-pixel space as referenceBox.
	subtreeBounds := subtreeBorderBoxBounds(box)
	if deviceScaled {
		subtreeBounds = mapRectToDeviceRect(dmFull, subtreeBounds)
	}
	sourceExtent := referenceBox.Union(subtreeBounds)

	// Build the filter graph. The builder computes the filter region by
	// walking the graph's MapRect chain (blur/drop-shadow inflate it).
	// Phase 7: SVG `<filter>` resource registry is supplied so
	// `filter: url(#id)` references resolve via BuildReferenceFilter.
	builder := &FilterEffectBuilder{
		ReferenceBox:       referenceBox,
		CurrentColor:       layer.TextColor,
		Resources:          r.lookupSVGResources(),
		ExternalSVGFetcher: r.externalSVGFetcher,
		ImageFetcher:       r.imageFetcher,
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
	//
	// When the element has zero painted extent (a 0x0 box with no
	// descendants) the MapRect-of-source-extent yields empty — losing any
	// SVG-resolved filter region from the referenced `<filter>` element
	// (Filter Effects 1 §6.4: `<filter>` x/y/width/height define the
	// result buffer for generators like feFlood whose output does not
	// depend on the source extent). Fall back to the SVG-resolved region
	// in that case so generator-only filters on empty elements still emit
	// pixels — mirrors Blink's FilterPainter which treats the
	// `<filter>` element's region as authoritative when MapRect(SourceGraphic)
	// would otherwise collapse to zero.
	mapped := filter.MapRect(sourceExtent)
	if filter.FilterRegion.Empty() {
		// No SVG-specified region: size to the mapped content extent.
		filter.FilterRegion = mapped
	}
	// When the SVG <filter> element provided an explicit x/y/width/height,
	// filter.FilterRegion is already set and is authoritative (Filter Effects 1
	// §10.4). Do not override it with the source-extent mapping — doing so
	// collapses the filter canvas to the element's border box, dropping the
	// outer filter region that generators (feFlood, feConvolveMatrix bias) fill.
	region := filter.FilterRegion
	bx, by := region.Min.X, region.Min.Y
	bw, bh := region.Dx(), region.Dy()
	if bw <= 0 || bh <= 0 || bw > 8000 || bh > 8000 {
		// Fallback: render without filters to avoid OOM.
		r.paintLayerContentWithEffects(layer)
		return
	}

	// Render the layer subtree into the source buffer (filter-region sized).
	// Filter Effects 1 §3.3: a `visibility: hidden` element does not paint its
	// own content into the source — the source stays transparent — but the
	// filter graph still evaluates. Skip the content render in that case so
	// the SourceGraphic is the zero-initialized transparent buf. (Descendants
	// with explicit `visibility: visible` would still paint if we let
	// paintLayerContentWithEffects run, mirroring CSS visibility inheritance;
	// the current bucket of visibility-hidden-with-filter tests has no such
	// descendants, so the simple "skip the whole subtree" matches Blink's
	// ApplyFilterIfNecessary path where the element's content draw is gated by
	// the same Visible bit before the filter runs.)
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	if layer.Visible {
		origDC := r.dc
		origTarget := r.target
		childDC := origDC.NewChildContext(buf)
		r.dc = childDC
		r.target = buf
		// Offset paint by -region.Min so it lands at the right buffer pixel.
		r.dc.Translate(float64(-bx), float64(-by))
		if deviceScaled {
			// Seed the child DC with the ANCESTOR device matrix.
			// paintLayerContentWithEffects then composes the layer's OWN
			// transform on top, so the effective content matrix is
			// translate(-region.Min) ∘ dmFull — exactly the device space the
			// region was computed in. The box therefore rasterizes at device
			// scale (e.g. 150×150 under scale(10)) with crisp vector edges,
			// instead of at layout scale (15×15) then bilinearly upscaled.
			r.dc.MultiplyMatrix(dm.XX, dm.YX, dm.XY, dm.YY, dm.X0, dm.Y0)
		}
		r.paintLayerContentWithEffects(layer)
		r.dc = origDC
		r.target = origTarget
	}

	// Run the FilterEffect graph and composite the output back at the
	// filter region origin.
	filter.SetSourceImage(buf)
	out := filter.ApplyToSRGB()
	if out == nil {
		// Match the two earlier failure paths (filter == nil, oversize buffer):
		// fall back to unfiltered paint so the element still renders rather
		// than vanishing into an invisible hole. r.dc/r.target were already
		// restored above.
		r.paintLayerContentWithEffects(layer)
		return
	}
	if deviceScaled {
		// The output is already in device pixels at region.Min — composite it
		// 1:1, bypassing the DC's transform stack (which still carries the
		// ancestor scale already baked into the device-space rasterization).
		r.blitImageToTarget(out, bx, by)
	} else {
		// Identity device matrix: composite through the DC so the active clip
		// stack (overflow:scroll/hidden, will-change layers) is honored.
		r.dc.DrawImage(out, bx, by)
	}
}

// deviceMatrixFromDC reconstructs the DrawContext's current affine matrix by
// probing the transforms of three points. The DrawContext interface exposes
// TransformPoint but not the raw matrix, so we recover it: with M(p) =
// (XX·x + XY·y + X0, YX·x + YY·y + Y0), M(0,0) gives (X0,Y0) and the unit-axis
// images give the columns.
func deviceMatrixFromDC(dc textshape.DrawContext) textshape.Matrix {
	ox, oy := dc.TransformPoint(0, 0)
	xx, yx := dc.TransformPoint(1, 0)
	xy, yy := dc.TransformPoint(0, 1)
	return textshape.Matrix{
		XX: xx - ox, YX: yx - oy,
		XY: xy - ox, YY: yy - oy,
		X0: ox, Y0: oy,
	}
}

// isIdentityMatrix reports whether m is the identity affine within a small
// epsilon (the matrix is reconstructed from TransformPoint probes, so it
// carries float rounding noise).
func isIdentityMatrix(m textshape.Matrix) bool {
	const eps = 1e-6
	return nearlyOne(m.XX) && nearlyOne(m.YY) &&
		math.Abs(m.XY) < eps && math.Abs(m.YX) < eps &&
		math.Abs(m.X0) < eps && math.Abs(m.Y0) < eps
}

// nearlyOne reports whether v is within a small epsilon of 1 (used to detect
// the no-scale case in a reconstructed device matrix).
func nearlyOne(v float64) bool { return math.Abs(v-1) < 1e-6 }

// isAxisAlignedScaleTranslate reports whether m is a pure scale + translate
// with no rotation or skew (off-diagonal terms ~0). The device-scaled filter
// path only handles this case exactly; rotation/skew and projective 3D
// transforms fall back to the untransformed path.
func isAxisAlignedScaleTranslate(m textshape.Matrix) bool {
	const eps = 1e-6
	return math.Abs(m.XY) < eps && math.Abs(m.YX) < eps
}

// mapBoxToDeviceRect maps a CSS box (layout-space x/y/w/h) through the device
// matrix and returns the integer reference rect in device pixels. The corners
// are ROUNDED (not floor/ceil) so the reference box aligns with where the box
// content is actually rasterized — the box painter pixel-snaps its border box
// via pixelSnap (math.Round), and the filter SourceGraphic / feFlood
// userSpaceOrigin anchor must match that snapped origin to avoid a 1px seam.
// For a pure scale/translate this is exact; for a rotation it is the
// axis-aligned bounding box (Blink's filter region is likewise axis-aligned).
func mapBoxToDeviceRect(m textshape.Matrix, x, y, w, h float64) image.Rectangle {
	x0, y0 := m.TransformPoint(x, y)
	x1, y1 := m.TransformPoint(x+w, y+h)
	x2, y2 := m.TransformPoint(x, y+h)
	x3, y3 := m.TransformPoint(x+w, y)
	minX := math.Min(math.Min(x0, x1), math.Min(x2, x3))
	minY := math.Min(math.Min(y0, y1), math.Min(y2, y3))
	maxX := math.Max(math.Max(x0, x1), math.Max(x2, x3))
	maxY := math.Max(math.Max(y0, y1), math.Max(y2, y3))
	return image.Rect(
		int(math.Round(minX)), int(math.Round(minY)),
		int(math.Round(maxX)), int(math.Round(maxY)),
	)
}

// mapRectToDeviceRect maps an already-device-pixel integer rect (computed
// in untransformed layout space) through the device matrix into device pixels.
func mapRectToDeviceRect(m textshape.Matrix, r image.Rectangle) image.Rectangle {
	if r.Empty() {
		return r
	}
	return mapCornersToDeviceRect(m, float64(r.Min.X), float64(r.Min.Y), float64(r.Max.X), float64(r.Max.Y))
}

// mapCornersToDeviceRect transforms the four corners of (x0,y0)-(x1,y1) and
// returns the axis-aligned integer bounding rect (floor min, ceil max).
func mapCornersToDeviceRect(m textshape.Matrix, x0, y0, x1, y1 float64) image.Rectangle {
	cx := [4]float64{x0, x1, x0, x1}
	cy := [4]float64{y0, y0, y1, y1}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for i := 0; i < 4; i++ {
		px, py := m.TransformPoint(cx[i], cy[i])
		minX, maxX = math.Min(minX, px), math.Max(maxX, px)
		minY, maxY = math.Min(minY, py), math.Max(maxY, py)
	}
	return image.Rect(
		int(math.Floor(minX)), int(math.Floor(minY)),
		int(math.Ceil(maxX)), int(math.Ceil(maxY)),
	)
}

// blitImageToTarget composites a straight-alpha RGBA image onto the renderer's
// target at absolute device pixel (dx, dy) using Porter-Duff source-over,
// bypassing the DC transform/clip stack. Used to composite a filter output
// that is already positioned and scaled in device pixels.
func (r *Renderer) blitImageToTarget(im *image.RGBA, dx, dy int) {
	if r == nil || r.target == nil || im == nil {
		return
	}
	tb := r.target.Bounds()
	sb := im.Bounds()
	for sy := sb.Min.Y; sy < sb.Max.Y; sy++ {
		ty := sy - sb.Min.Y + dy
		if ty < tb.Min.Y || ty >= tb.Max.Y {
			continue
		}
		// Hoist the row-base pixel offsets and advance by 4 (RGBA stride) per
		// column, avoiding a PixOffset multiply on every pixel.
		si := im.PixOffset(sb.Min.X, sy)
		for sx := sb.Min.X; sx < sb.Max.X; sx, si = sx+1, si+4 {
			tx := sx - sb.Min.X + dx
			if tx < tb.Min.X || tx >= tb.Max.X {
				continue
			}
			sa := im.Pix[si+3]
			if sa == 0 {
				continue
			}
			ti := r.target.PixOffset(tx, ty)
			if sa == 255 {
				r.target.Pix[ti+0] = im.Pix[si+0]
				r.target.Pix[ti+1] = im.Pix[si+1]
				r.target.Pix[ti+2] = im.Pix[si+2]
				r.target.Pix[ti+3] = 255
				continue
			}
			inv := uint32(255 - sa)
			r.target.Pix[ti+0] = uint8(uint32(im.Pix[si+0]) + uint32(r.target.Pix[ti+0])*inv/255)
			r.target.Pix[ti+1] = uint8(uint32(im.Pix[si+1]) + uint32(r.target.Pix[ti+1])*inv/255)
			r.target.Pix[ti+2] = uint8(uint32(im.Pix[si+2]) + uint32(r.target.Pix[ti+2])*inv/255)
			r.target.Pix[ti+3] = uint8(uint32(sa) + uint32(r.target.Pix[ti+3])*inv/255)
		}
	}
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
	r.enterLayerTransform(layer)
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}
	r.exitLayerTransform(layer)
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

	// A logically zero-area border box paints no backdrop. Skip before
	// snapping to device pixels — otherwise floor/ceil on fractional Y
	// inflates 0 logical height to 1 device row and lets the filter
	// (e.g. invert) stamp a 1px-tall strip onto the canvas.
	if box.Width <= 0 || box.Height <= 0 {
		return
	}

	// Element's border-box region in absolute device pixels.
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.X+box.Width)) - bx
	bh := int(math.Ceil(box.Y+box.Height)) - by

	if bw <= 0 || bh <= 0 || bw > 8000 || bh > 8000 {
		return
	}
	borderBox := image.Rect(bx, by, bx+bw, by+bh)

	// CSS Filter Effects 2 §3.4: the filter input is the backdrop content
	// within the element's filter region (border box). Samples the blur
	// kernel would pull from outside the border box are NOT canvas pixels
	// — that would import content the spec excludes from the backdrop
	// (e.g. green boxes painted before but outside the filter element).
	// Mirrors Blink's `core/paint/filter_painter.cc` at
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which clips the source to
	// the filter region and edge-extends pixels for the blur kernel.
	//
	// Our model: snapshot the canvas pixels for the padded region but
	// clamp (sx,sy) into the border-box rect before reading from r.target.
	// Pixels in the pad area thus replicate the nearest border-box pixel
	// — equivalent to SVG `feGaussianBlur edgeMode="duplicate"`, which is
	// the behaviour WPT's backdrop-filter-edge-clipping reference asserts.
	pad := 0
	for _, f := range layer.BackdropFilters {
		if f.Name == "blur" {
			pad = max(pad, int(math.Ceil(f.Value*3))+2)
		}
	}
	region := image.Rect(bx-pad, by-pad, bx+bw+pad, by+bh+pad)
	rw, rh := region.Dx(), region.Dy()

	// Snapshot the backdrop image, edge-clamped to the border box so blur
	// padding does not import canvas content from outside the filter region.
	backdrop := image.NewRGBA(image.Rect(0, 0, rw, rh))
	targetBounds := r.target.Bounds()
	for y := 0; y < rh; y++ {
		sy := region.Min.Y + y
		if sy < by {
			sy = by
		} else if sy >= by+bh {
			sy = by + bh - 1
		}
		if sy < targetBounds.Min.Y || sy >= targetBounds.Max.Y {
			continue
		}
		for x := 0; x < rw; x++ {
			sx := region.Min.X + x
			if sx < bx {
				sx = bx
			} else if sx >= bx+bw {
				sx = bx + bw - 1
			}
			if sx < targetBounds.Min.X || sx >= targetBounds.Max.X {
				continue
			}
			si := r.target.PixOffset(sx, sy)
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
		ImageFetcher:       r.imageFetcher,
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
	// Cull edge-on flattened transforms before setting up the offscreen buffer.
	if isLayerTransformSingular(layer) {
		return
	}
	box := layer.Box
	if box == nil {
		r.paintLayerContent(layer)
		return
	}

	// Buffer extent: the layer's own border box unioned with the
	// subtree's border boxes so an overflowing blended descendant is
	// fully captured. Matches paintLayerWithFilter's sourceExtent
	// pattern.
	selfBounds := image.Rect(
		int(math.Floor(box.X)),
		int(math.Floor(box.Y)),
		int(math.Ceil(box.X+box.Width)),
		int(math.Ceil(box.Y+box.Height)),
	)
	extent := selfBounds.Union(subtreeBorderBoxBounds(box))
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
	// against the outer canvas. paintLayerWithBlend and other pixel-
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
	if layer.Opacity < 1.0 {
		compositeBufferWithOpacityOnto(r.target, buf, 0, 0, layer.Opacity)
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

	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}

	r.exitLayerTransform(layer)
}

// paintLayerWithBlend renders the entire layer subtree into an offscreen
// buffer, then blend-composites the result onto the destination using the
// specified CSS mix-blend-mode.
func (r *Renderer) paintLayerWithBlend(layer *PaintLayer) {
	// Cull edge-on flattened transforms before setting up the offscreen buffer.
	if isLayerTransformSingular(layer) {
		return
	}
	box := layer.Box
	bx := int(math.Floor(box.X))
	by := int(math.Floor(box.Y))
	bw := int(math.Ceil(box.Width+(box.X-float64(bx)))) + 1
	bh := int(math.Ceil(box.Height+(box.Y-float64(by)))) + 1

	if bw <= 0 || bh <= 0 || bw > 4000 || bh > 4000 {
		r.paintViewBackground(layer)
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
	r.enterLayerTransform(layer)
	if layer.Opacity < 1.0 {
		r.dc.PushGroup()
		r.paintLayerContent(layer)
		r.dc.PopGroupWithAlpha(layer.Opacity)
	} else {
		r.paintLayerContent(layer)
	}
	r.exitLayerTransform(layer)

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
//
// The transform-origin's absolute position is computed against the *snapped*
// border-box, not the raw layout box. Blink does the same in
// `TransformPaintPropertyNode::Build` (paint_property_tree_builder.cc
// @ 4883d11fef): the origin is added to `EnclosingLayoutRect(border_box)`
// rather than the unsnapped origin so that the paint-time transform pivots
// against the same pixel grid the box itself is rasterized to. Without this,
// a 0.0625-px sub-pixel offset in `box.Y` is compounded by the rotation
// matrix and leaks one full pixel of color across an axis-aligned edge after
// 180° / 90° rotations.
//
// Returns whether the effective 2D affine it applied is singular — i.e. the
// flattened transform maps the box to a zero-area quad (edge-on, e.g. after
// rotateX(90deg)). The HasTransform paint scope uses this to cull the
// subtree, mirroring Blink's GeometryMapper invertibility check.
func (r *Renderer) applyTransforms(layer *PaintLayer) bool {
	box := layer.Box
	sx, sy, _, _ := pixelSnap(box.X, box.Y, box.Width, box.Height)
	ox := sx + layer.TransformOrigin[0]
	oy := sy + layer.TransformOrigin[1]

	// A layer that establishes a 3D rendering context (transform-style:
	// preserve-3d) does NOT flatten: its children compose in 4×4 within the
	// shared context, so the layer's own 2D projection being edge-on does
	// not make the subtree empty — a child's transform can compose back to a
	// visible (non-degenerate) quad (e.g. rotateX(90deg) preserve-3d wrapping
	// rotateX(90deg) scale(2) → scale(2,-2)). The singular-projection cull is
	// therefore only valid for flattening layers; preserve-3d layers are
	// never culled by their own projection. Mirrors Blink, where the
	// invertibility/empty-visual-rect cull applies to flattened
	// TransformPaintPropertyNodes, not to the 3D-rendering-context root
	// (paint_property_tree_builder.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	flattens := box.Style == nil || box.Style.GetTransformStyle() != css.TransformStylePreserve3D

	// 3D rendering context composition. Per CSS Transforms L2 §6.2,
	// a child of a `transform-style: preserve-3d` parent shares the
	// parent's 3D rendering context: its transform composes with the
	// parent's transform in 4×4 (cumulative 3D rotation/scale/perspective)
	// BEFORE 2D projection — not as the cumulative 2D projection that
	// each ancestor independently applied.
	//
	// Mirrors Blink's TransformPaintPropertyNode tree, where a child
	// inside a preserve-3d context inherits the parent's 4×4 transform
	// node directly rather than re-flattening at each level
	// (paint_property_tree_builder.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	//
	// Implementation: walk up while parent has preserve-3d, prepending
	// each ancestor's transforms (and transform-origin shift) to a
	// cumulative transform list. Compute the 2D projection of (ancestors)
	// vs (ancestors+self), and apply the delta:
	//     M_new = proj(ancestors+self) * proj(ancestors)^-1
	// which when multiplied onto the DC matrix (already carrying
	// proj(ancestors)) yields proj(ancestors+self).
	//
	// Falls back to plain self-only projection (per-layer flat
	// composition) when there is no preserve-3d ancestor OR when the
	// ancestors' projection is singular (determinant 0, e.g. an
	// ancestor's transform is scaleY(0)). The singular fallback is a
	// graceful degradation rather than correctness — the singular case
	// cannot be expressed as a 2D affine delta.
	// Build the transform-origin-Z-wrapped self transforms once; reused
	// by both the preserve-3d branch and the flat branch below. Per CSS
	// Transforms L2 §6 the transform-origin's z-component shifts the
	// transform's 3D origin along the Z axis: the effective composition
	// is `translateZ(oz) * Mtransforms * translateZ(-oz)`. (X/Y origin
	// shifts are handled by the renderer's enclosing Translate(ox,oy) /
	// Translate(-ox,-oy) pair.) For an oz of 0 this collapses to the
	// raw transform list.
	selfTransforms := wrapTransformsWithOriginZ(layer.Transforms, layer.TransformOriginZ)

	// Parent-established perspective (CSS Transforms L2 §6): when the box's
	// parent carries a `perspective` property, the child's transform composes
	// with the parent's perspective matrix (about perspective-origin) in 4×4
	// BEFORE projecting to 2D — so the child's out-of-plane Z (from
	// rotateX/rotateY) is foreshortened. This is the property-sourced
	// counterpart of the `perspective()` transform function and is what the
	// transform3d-perspective-* reference files exercise.
	if d, opx, opy, ok := r.parentPerspectiveContext(box); ok {
		h := css.ComposePerspectiveChildProjection(selfTransforms, ox, oy, d, opx, opy)
		r.dc.MultiplyProjectiveMatrix(h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7], h[8])
		return false
	}

	if ancestorTransforms := preserve3DAncestorTransforms(box); len(ancestorTransforms) > 0 {
		// Move origin to transform-origin point.
		r.dc.Translate(ox, oy)
		ancXX, ancYX, ancXY, ancYY, ancX0, ancY0 := css.ComposeAffineProjection(ancestorTransforms)
		combined := append(append([]css.Transform(nil), ancestorTransforms...), selfTransforms...)
		newXX, newYX, newXY, newYY, newX0, newY0 := css.ComposeAffineProjection(combined)
		// 2D affine inverse of M with coefficients (xx, yx, xy, yy, x0, y0):
		// the point transform is x' = xx*x + xy*y + x0, y' = yx*x + yy*y + y0.
		// Determinant det = xx*yy - xy*yx; inverse exists when det != 0.
		det := ancXX*ancYY - ancXY*ancYX
		if det != 0 {
			invXX := ancYY / det
			invYX := -ancYX / det
			invXY := -ancXY / det
			invYY := ancXX / det
			invX0 := (ancXY*ancY0 - ancYY*ancX0) / det
			invY0 := (ancYX*ancX0 - ancXX*ancY0) / det
			// Undo ancestor's accumulated 2D projection.
			r.dc.MultiplyMatrix(invXX, invYX, invXY, invYY, invX0, invY0)
			// Apply the full combined (ancestors+self) projection.
			r.dc.MultiplyMatrix(newXX, newYX, newXY, newYY, newX0, newY0)
			// Move back from transform-origin.
			r.dc.Translate(-ox, -oy)
			// The effective applied projection is the combined ancestors+self
			// affine; if it is edge-on the subtree maps to zero area (only
			// when this layer flattens — preserve-3d layers are never culled).
			return flattens && isSingularProjection(newXX, newYX, newXY, newYY)
		}
		// Singular ancestor projection: fall through to flat composition.
		r.dc.Translate(-ox, -oy)
	}

	// Move origin to transform-origin point.
	r.dc.Translate(ox, oy)

	// Compose all transforms into a single 4×4 matrix, then project to a
	// 2D affine. This is the "flatten" step Blink's
	// TransformPaintPropertyNode performs for every layer that inherits
	// a flat parent context (paint_property_tree_builder.cc @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	//
	// The composed projection is correct for cumulative 3D transforms
	// — e.g. rotateX(90deg) * scale3d(2,1,2) * rotateX(-90deg) composes
	// to scale(2,2), which a per-function 2D projection cannot produce.
	//
	// Perspective division (m30/m31 != 0 or m33 != 1) is approximated by
	// dropping row 3 — this matches the no-perspective case exactly and
	// degrades gracefully for tests where the perspective component is
	// small. Boxes with non-trivial perspective render as the affine
	// portion of the projection rather than the full perspective image.
	// When the composed projection of the z=0 plane carries a perspective
	// component (homography bottom row ≠ [0 0 1]), apply the full 2D homography
	// (with perspective divide) so perspective()/`perspective` foreshortening is
	// exact. Perspective preserves straight lines, so a transformed rectangle
	// remains a (non-affine) quad — correct for the box's border/background fill,
	// which the renderer emits as a 4-corner path. Otherwise use the affine
	// projection (the common, cheaper path).
	//
	// ComposeProjective is called once; affine coefficients are extracted from
	// the same h matrix rather than calling ComposeAffineProjection again.
	h := css.ComposeProjective(selfTransforms)
	if h[6] != 0 || h[7] != 0 || h[8] != 1 {
		r.dc.MultiplyProjectiveMatrix(h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7], h[8])
		r.dc.Translate(-ox, -oy)
		return false
	}
	// h = [xx xy x0 / yx yy y0 / 0 0 1]; MultiplyMatrix expects (xx,yx,xy,yy,x0,y0).
	r.dc.MultiplyMatrix(h[0], h[3], h[1], h[4], h[2], h[5])

	// Move back from transform-origin.
	r.dc.Translate(-ox, -oy)
	return flattens && isSingularProjection(h[0], h[3], h[1], h[4])
}

// isLayerTransformSingular returns true if layer.HasTransform is set and the
// layer's flattened 2D projection is singular (edge-on / zero-area). Used to
// cull the layer before any paint path — including the special-effect paths
// (HasMaskImage, HasFilter, blend, isolated) that would otherwise bypass the
// transformSingular check in the main paintLayer body.
//
// Conservative in the preserve-3d-ancestor case: if a preserve-3d ancestor
// exists the combined projection is not computed here (returns false, no cull).
// This is correct for the common case and avoids duplicating the full
// preserve-3d delta-projection maths from applyTransforms.
func isLayerTransformSingular(layer *PaintLayer) bool {
	if !layer.HasTransform || layer.Box == nil {
		return false
	}
	box := layer.Box
	// preserve-3d layers are never culled by their own projection (children
	// compose in 4×4 and can produce a visible quad even when the layer's
	// own transform is edge-on). Same guard as applyTransforms.
	if box.Style != nil && box.Style.GetTransformStyle() == css.TransformStylePreserve3D {
		return false
	}
	// If there is a preserve-3d ancestor, the combined projection may differ
	// from the self-only projection. Conservatively skip culling (don't cull
	// visible layers). The main-path applyTransforms handles this correctly.
	if len(preserve3DAncestorTransforms(box)) > 0 {
		return false
	}
	selfTransforms := wrapTransformsWithOriginZ(layer.Transforms, layer.TransformOriginZ)
	xx, yx, xy, yy, _, _ := css.ComposeAffineProjection(selfTransforms)
	return isSingularProjection(xx, yx, xy, yy)
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

// wrapTransformsWithOriginZ returns `transforms` bracketed by
// translateZ(oz) and translateZ(-oz) so that ComposeAffineProjection's
// 4×4 composition reflects the full 3D transform-origin per CSS
// Transforms L2 §6 (`T(0,0,oz) * M * T(0,0,-oz)`). For oz == 0 the
// original slice is returned unchanged.
func wrapTransformsWithOriginZ(transforms []css.Transform, oz float64) []css.Transform {
	if oz == 0 {
		return transforms
	}
	wrapped := make([]css.Transform, 0, len(transforms)+2)
	wrapped = append(wrapped, css.Transform{Type: "translateZ", Values: []float64{oz}})
	wrapped = append(wrapped, transforms...)
	wrapped = append(wrapped, css.Transform{Type: "translateZ", Values: []float64{-oz}})
	return wrapped
}

// preserve3DAncestorTransforms returns the flat list of ancestor
// transforms (in document/composition order: outermost first) that share
// `box`'s 3D rendering context. The walk stops at the first ancestor
// whose parent is NOT `transform-style: preserve-3d` — that ancestor's
// transform is included (it's the context root from the descendant's
// perspective) only if its parent is preserve-3d; otherwise the walk
// stops with nothing included.
//
// Concretely: walk parent → grandparent → ... while
// `ancestor.Style.GetTransformStyle() == preserve-3d`. The walk
// terminates as soon as that condition fails (the ancestor sits in a
// flat parent's context, so its own transform was already 2D-projected
// at its own paint).
//
// An anonymous ancestor without a transform of its own (e.g. the anonymous
// table, row-group and row generated around a display:table-cell) is
// skipped: the 3D rendering context follows the DOM ancestor chain, and
// Blink resets the flattening state only for non-anonymous objects
// (FragmentPaintPropertyTreeBuilder::UpdateTransform,
// core/paint/paint_property_tree_builder.cc:1606-1620 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). Such a box has no DOM node.
// A block-in-inline wrapper ends the walk: its layout parent is an inline
// box, which is not transformable, so its used transform-style is flat and
// the 3D rendering context does not reach past it (the context follows the
// layout tree, where the LayoutInline is the wrapper's parent).
//
// Mirrors Blink's TransformPaintPropertyNode 3D-rendering-context walk
// in paint_property_tree_builder.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func preserve3DAncestorTransforms(box *layout.Box) []css.Transform {
	if box == nil || box.Parent == nil {
		return nil
	}
	var ancestors []css.Transform
	for ancestor := box.Parent; ancestor != nil && ancestor.Style != nil && !ancestor.IsBlockInInline; ancestor = ancestor.Parent {
		if ancestor.Node == nil && len(ancestor.Style.GetTransforms()) == 0 {
			continue
		}
		if ancestor.Style.GetTransformStyle() != css.TransformStylePreserve3D {
			break
		}
		// Prepend this ancestor's transforms (outermost first, applied
		// before descendant transforms in the composition chain).
		ancTransforms := ancestor.Style.GetTransforms()
		if len(ancTransforms) > 0 {
			ancestors = append(append([]css.Transform(nil), ancTransforms...), ancestors...)
		}
		// Individual longhands (translate/rotate/scale) apply in
		// canonical CSS order: translate → rotate → scale → transform.
		// Mirrors css.IsBackFaceVisible / computeBackfaceHidden.
		if deg, ok := ancestor.Style.GetIndividualRotate(); ok {
			ancestors = append([]css.Transform{{Type: "rotate", Values: []float64{deg}}}, ancestors...)
		}
		if sx, sy, ok := ancestor.Style.GetIndividualScale(); ok {
			ancestors = append([]css.Transform{{Type: "scale", Values: []float64{sx, sy}}}, ancestors...)
		}
	}
	return ancestors
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
	}

	// Step 2: Negative z-index stacking contexts.
	for _, child := range layer.NegativeZ {
		r.paintLayer(child)
	}

	// Step 3: Descendant block backgrounds (non-self-painting subtree).
	r.paintDescendantsPhase(layer, PhaseBackground)

	// A collapsed table paints its borders after its descendants'
	// backgrounds (box_fragment_painter.cc:941-947 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). paintLayerContent reaches
	// a self-painting table; paintDescendantPhase reaches the others.
	r.PaintCollapsedBorders(layer)

	// Step 4: Floats (each runs its own full phase loop).
	r.paintDescendantsPhase(layer, PhaseFloat)

	// Step 5a: Self foreground — text + image content.
	r.paintSelfForeground(layer)

	// Step 5b: Descendant foreground (text/image + non-positioned SCs).
	r.paintDescendantsPhase(layer, PhaseForeground)

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
		r.paintLayer(child)
	}

	// Step 7: Positive z-index stacking contexts.
	for _, child := range layer.PositiveZ {
		r.paintLayer(child)
	}

	if clipping {
		r.dc.Pop()
	}

	// Classic scrollbar widget (overflow controls). Painted after the
	// box's content and child layers and OUTSIDE the overflow content
	// clip — the gutter sits between the inner border edge and the
	// padding edge, which the content clip excludes. Mirrors Blink's
	// ScrollableAreaPainter::PaintOverflowControls running after the
	// foreground phase (scrollable_area_painter.cc @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	r.paintScrollbars(layer)

	// Self outline paints OUTSIDE the element's own overflow clip — the
	// clip applies to the box's contents, never to its own outline ring
	// (Blink paints kSelfOutlineOnly without the contents clip) — but
	// still inside clip-path / CSS clip, which do clip outlines.
	r.paintSelfOutline(layer)

	if clipPathActive {
		r.dc.Pop()
	}

	if cssClipping {
		r.dc.Pop()
	}
}

// paintSelfOutline paints the layer's own outline. CSS 2.1 Appendix E
// step 10: outlines paint after all in-flow content of the stacking
// context, so an outline overlapping its own element's text or background
// (e.g. with a negative outline-offset) draws on top. Mirrors Blink's
// dedicated PaintPhase::kSelfOutlineOnly phase (paint_phase.h @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func (r *Renderer) paintSelfOutline(layer *PaintLayer) {
	if layer.OutlineStyle != "none" && layer.OutlineWidth > 0 {
		r.drawOutline(layer)
	}
}

// paintSelfDecorations paints a layer's own background, borders,
// shadows, column rules, and list marker — the subset of
// painting that precedes descendant painting in CSS 2.1 Appendix E.
// The outline is NOT painted here; it has its own post-content phase
// (see paintSelfOutline). Called once per layer (not once per phase).
func (r *Renderer) paintSelfDecorations(layer *PaintLayer) {
	// empty-cells: hide — skip background, borders, and box shadows for
	// empty table cells in the separate border model (CSS 2.1 §17.6.1.1).
	// The cell still occupies space; only painting is suppressed.
	if layer.Box != nil && layer.Box.IsFieldsetContainer {
		r.paintFieldsetBoxDecorationBackground(layer)
	} else if !layer.EmptyCellHide {
		rect := borderBoxRect(layer.Box)
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
	if !child.Visible {
		// Filter Effects 1 §3.3: a `visibility: hidden` element with a filter
		// still evaluates its filter graph (against a transparent source).
		// Forward to paintLayer so the filter-dispatch in paintLayer can
		// route through paintLayerWithFilter. paintLayer's own visibility
		// gate already handles non-filtered hidden layers; this keeps the
		// behaviour identical for those, but lets a filtered hidden flow
		// child reach the filter path. Mirrors Blink
		// ScopedSVGPaintState::Apply (filter installs unconditionally) +
		// ScopedPaintChunkProperties::Visibility (hidden suppresses content
		// only).
		if child.HasFilter && phase == PhaseForeground {
			r.paintLayer(child)
		}
		return
	}
	if child.Opacity <= 0 {
		return
	}
	// CSS Transforms L2 §11: `backface-visibility: hidden` skips the layer
	// (and its non-self-painting subtree) when the back face is presented
	// to the viewer. Same gate as the paintLayer entry — applied here so
	// non-stacking-context flow children also honour the skip.
	if child.BackfaceHidden {
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
	case PhaseOutline:
		r.paintSelfOutline(child)
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
	}

	r.paintDescendantsPhase(child, phase)

	// A collapsed table's borders, after its descendants' backgrounds
	// (see paintLayerContent).
	if phase == PhaseBackground {
		r.PaintCollapsedBorders(child)
	}

	if clipping {
		r.dc.Pop()
	}

	// Classic scrollbar widget for a non-stacking-context scroll container.
	// Painted in the foreground phase (on top of the box's content) and
	// OUTSIDE the box's overflow clip, mirroring the self-painting-layer
	// path in paintLayerContent. A box reaches exactly one of the two
	// paths, so the scrollbar paints once.
	if phase == PhaseForeground {
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
// When no clip-margin is active the border-box radii are returned
// unchanged.
func overflowClipRadii(layer *PaintLayer) css.EllipticalRadii {
	if !layer.HasClipMargin {
		return layer.BorderRadius
	}
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

// buildRoundedRectPathReverse traces a rounded rectangle path in reverse (CCW)
// for use with even-odd fill rule to cut out inner regions.
func (r *Renderer) buildRoundedRectPathReverse(x, y, w, h float64, radii css.EllipticalRadii) {
	buildRoundedRectPathReverseDC(r.dc, x, y, w, h, radii)
}

// buildRoundedRectPathOnDC traces an elliptical rounded rectangle path on any DrawContext.
// This is the single source of truth for rounded-rect path construction.
func buildRoundedRectPathOnDC(dc pathBuilder, x, y, w, h float64, radii css.EllipticalRadii) {
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
func buildRoundedRectPathReverseDC(dc pathBuilder, x, y, w, h float64, radii css.EllipticalRadii) {
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
		// Background-color only on the bottom layer.
		if bg.IsBottomLayer() {
			r.paintFillLayerColor(layer, rect)
		}
		img := r.loadFillLayerImage(bg)
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

// loadFillLayerImage fetches a fill layer's url() image, keeping an SVG
// document as an SVGImage (StyleFetchedImage over an SVGImage) so it is
// sized by its natural sizing info and drawn for its container. None when
// the layer has no url() image or there is no fetcher (data: URIs decode
// inline and need none).
func (r *Renderer) loadFillLayerImage(bg *css.FillLayer) fillLayerImage {
	if bg.Image == nil || (r.imageFetcher == nil && !stdnet.IsDataURL(bg.Image.Data.Absolute)) {
		return fillLayerImage{}
	}
	url := bg.Image.Data.Absolute
	if svg := images.LoadSVGImageWithFetcher(url, r.imageFetcher); svg != nil {
		return fillLayerImage{svg: svg}
	}
	img, _ := images.LoadImageWithFetcher(url, r.imageFetcher)
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
	scrolledPaintRect := r.fillLayerClipPath(layer, rect, contouredBorderClip(bg.Clip, local), local)
	r.dc.Clip()
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
func (r *Renderer) fillLayerClipPath(layer *PaintLayer, rect layout.PhysicalRect, clip css.BackgroundClipType, local bool) layout.PhysicalRect {
	scrolledPaintRect := rect
	if local {
		scrolledPaintRect = r.adjustRectForScrolledContent(layer.Box, rect)
	}
	if radii := backgroundClipRadiiForClip(layer, rect, clip); !radii.IsZero() {
		x, y, w, h := r.backgroundPaintRectForLayer(layer, rect, clip)
		rr := FloatRoundedRect{Rect: geometry.NewRectF(x, y, w, h), Radii: radii}
		if rr.IsRenderable() {
			rr.addPath(r.dc)
		} else {
			// Non-renderable rrect: corner-wise clip, then the plain rect
			// (PaintFastBottomLayer / PaintFillLayer's
			// RoundedInnerRectClipper, box_painter_base.cc:1101-1108,
			// :1503-1505 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
			r.clipRoundedInnerRect(geometry.NewRectF(rect.Offset.X, rect.Offset.Y, rect.Size.Width, rect.Size.Height), rr)
			r.dc.DrawRectangle(x, y, w, h)
		}
	} else {
		r.dc.DrawRectangle(r.backgroundPaintRectForLayer(layer, scrolledPaintRect, clip))
	}
	return scrolledPaintRect
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
	var info NaturalSizingInfo
	var tileImage func(w, h int) image.Image
	switch {
	case img.svg != nil:
		info = naturalSizingInfoForSVG(img.svg.Sizing)
		tileImage = func(w, h int) image.Image {
			tile, err := img.svg.DrawForContainer(w, h)
			if err != nil {
				return image.Transparent
			}
			return tile
		}
	case img.raster != nil:
		src := img.raster
		srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
		if srcW == 0 || srcH == 0 {
			return
		}
		info = naturalSizingInfoForRaster(srcW, srcH)
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

// drawImage paints an <img> element with object-fit and object-position support.
func (r *Renderer) drawImage(layer *PaintLayer) {
	if layer.ImageSrc == "" || (r.imageFetcher == nil && !stdnet.IsDataURL(layer.ImageSrc)) {
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

// drawBorders paints the layer's borders in rect, its border box: the
// border image when one is set and loads, else BoxBorderPainter
// (BoxPainterBase::PaintBorder, box_painter_base.cc:1745-1780 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) drawBorders(layer *PaintLayer, rect layout.PhysicalRect) {
	if layer.BorderImageSource != nil || layer.BorderImageGenerated != "" {
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

// outlinePaintRect returns the pixel-snapped rect the outline ring is drawn
// around. For multi-line inline elements, OutlineBox holds the union bounding
// box of all line fragments for the element; use it when set (w > 0) so the
// ring encloses the full inline element, not just one line's box. Mirrors
// Blink's OutlinePainter::PaintOutline collecting all line boxes for a
// LayoutInline (outline_painter.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func outlinePaintRect(layer *PaintLayer) (x, y, w, h float64) {
	if layer.OutlineBox[2] > 0 {
		return pixelSnap(layer.OutlineBox[0], layer.OutlineBox[1], layer.OutlineBox[2], layer.OutlineBox[3])
	}
	box := layer.Box
	return pixelSnap(box.X, box.Y, box.Width, box.Height)
}

func (r *Renderer) drawOutline(layer *PaintLayer) {
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

	// Treat `auto` as `solid` — louis14 has no focus-ring concept (CSS UI 4
	// §4 explicitly permits this fallback).
	style := layer.OutlineStyle
	if style == "auto" {
		style = "solid"
	}

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
		// Paint non-solid outline styles per side: each side's run spans the
		// full outer-rect edge, with its centerline half a band-width inside
		// the outer edge. Stand-in for Blink painting single-rect non-auto
		// outlines through the border painter
		// (BoxBorderPainter::PaintSingleRectOutline, outline_painter.cc:1022
		// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A double outline goes
		// through that port (paintSingleRectOutline) unless the CSS UI
		// minimum-size clamp moved its rect; dashed/dotted keep this
		// per-side geometry, which matches DrawDashedOrDottedBoxSide's.
		if style == "double" && outerW > minDim && outerH > minDim {
			ro, wi := layoutunit.FromFloat64Round(offset), layoutunit.FromFloat64Round(width)
			r.paintSingleRectOutline(layer.BorderRadius, toLayoutRect(x, y, w, h), int(wi.Round()),
				geometry.PhysicalBoxStrut{Top: ro, Right: ro, Bottom: ro, Left: ro}, layer.OutlineColor, css.BorderStyleDouble)
			return
		}
		draw := r.drawDashedLine
		switch style {
		case "dotted":
			draw = r.drawDottedLine
		case "double":
			draw = r.drawDoubleLine
		}
		outerR := outerX + outerW
		outerB := outerY + outerH
		midT := outerY + width/2
		midB := outerB - width/2
		midL := outerX + width/2
		midR := outerR - width/2
		draw(outerX, midT, outerR, midT, width) // top
		draw(midR, outerY, midR, outerB, width) // right
		draw(outerX, midB, outerR, midB, width) // bottom
		draw(midL, outerY, midL, outerB, width) // left
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

// shouldFlipUnderlineAndOverline reports whether the underline/overline
// bits on this run's AppliedTextDecorations must be swapped before painting.
// Mirrors Blink's `flip_underline_and_overline_` logic at
// third_party/blink/renderer/core/paint/text_decoration_info.cc:223-237
// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which keys on:
//
//	original_underline_position_ == ResolvedUnderlinePosition::kOver
//
// Per CSS Text Decor 3 §3.5: `text-underline-position: left|right` names a
// physical side, but the underline must always stay on the LINE-RELATIVE
// under-side of the text content. If the named physical side IS the over-
// side (relative to the inline-axis), the underline/overline lines swap
// sides so the underline still ends up on the under-side.
//
// For central-baseline vertical runs (IsUprightVertical), non-CJK:
//   - `position:right` triggers a flip in vertical-rl (RIGHT = over-side,
//     because block-start direction is RIGHT in vertical-rl) but NOT in
//     vertical-lr (RIGHT = under-side, since block-end direction is RIGHT).
//   - `position:left` triggers a flip in vertical-lr (LEFT = over-side)
//     but NOT in vertical-rl (LEFT = under-side).
//
// For central-baseline vertical runs (IsUprightVertical), CJK (TextLangIsCJK):
// Per CSS Text Decor 3 §3.5 the default underline placement flips to the
// over-side. Mirrors Blink's ResolveUnderlinePosition() CJK branch which
// returns kOver for any position other than explicit `kLeft`
// (text_decoration_info.cc:42-47 @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
//   - position: auto / from-font / under / right → kOver → flip
//   - position: left → kUnder → no flip (explicit override of CJK default)
func shouldFlipUnderlineAndOverline(layer *PaintLayer) bool {
	if !layer.IsUprightVertical {
		return false
	}
	pos := layer.TextUnderlinePosition
	// `position:right` is the over-side in vertical-rl (and only there).
	if strings.Contains(pos, "right") && !layer.IsWritingModeVerticalLR {
		return true
	}
	// `position:left` is the over-side in vertical-lr (and only there).
	if strings.Contains(pos, "left") && layer.IsWritingModeVerticalLR {
		return true
	}
	// CJK script default: position resolves to kOver unless the author
	// explicitly opted out with `left`. Independent of writing-mode (rl/lr)
	// — Blink's CJK branch doesn't condition on the inline direction.
	if layer.TextLangIsCJK && !strings.Contains(pos, "left") {
		return true
	}
	return false
}

// isCJKLocale reports whether the given BCP47 language tag has a primary
// language subtag that triggers the CSS Text Decor 3 §3.5 vertical-default
// flip (Chinese, Japanese, Korean). Mirrors the script check in Blink's
// ResolveUnderlinePosition() at third_party/blink/renderer/core/paint/
// text_decoration_info.cc:42 @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
// (USCRIPT_KATAKANA_OR_HIRAGANA / USCRIPT_HANGUL), promoted here to a locale
// prefix check because louis14 doesn't yet derive script from the font's
// Unicode coverage. Mongolian (`mn`) is also included per CSS Text Decor 3
// §3.5's explicit enumeration; Chinese (`zh`) covers both Han variants.
//
// Matching is case-insensitive and tolerates region/script subtags
// (`ja-JP`, `zh-Hant`, etc.) by checking only the primary subtag prefix
// up to the first hyphen.
func isCJKLocale(lang string) bool {
	if lang == "" {
		return false
	}
	if i := strings.IndexByte(lang, '-'); i >= 0 {
		lang = lang[:i]
	}
	switch strings.ToLower(lang) {
	case "zh", "ja", "ko", "mn":
		return true
	}
	return false
}

// flipUnderlineOverlineBits returns a copy of decorations with the underline
// and overline bits swapped on each entry's Lines bitfield. Used when
// shouldFlipUnderlineAndOverline() reports true to mirror Blink's
// `std::swap(decoration.has_underline, decoration.has_overline)` at
// text_decoration_info.cc:233 @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func flipUnderlineOverlineBits(decorations []css.AppliedTextDecoration) []css.AppliedTextDecoration {
	if len(decorations) == 0 {
		return decorations
	}
	out := make([]css.AppliedTextDecoration, len(decorations))
	for i, td := range decorations {
		out[i] = td
		hasUnder := td.Lines.Has(css.TextDecorationLineUnderline)
		hasOver := td.Lines.Has(css.TextDecorationLineOverline)
		// Clear both bits, then set them according to the swapped values.
		lines := td.Lines &^ (css.TextDecorationLineUnderline | css.TextDecorationLineOverline)
		if hasOver {
			lines |= css.TextDecorationLineUnderline
		}
		if hasUnder {
			lines |= css.TextDecorationLineOverline
		}
		out[i].Lines = lines
	}
	return out
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
// The IsUprightVertical guard distinguishes "true sideways" (sideways-rl /
// sideways-lr / vertical-*+text-orientation:sideways) from "central-baseline
// vertical" (vertical-rl / vertical-lr with mixed or upright text-orientation),
// because louis14 collapses the latter into IsSidewaysRL=true at engine.go:374.
func sidewaysUnderlineGoesRight(layer *PaintLayer) bool {
	pos := layer.TextUnderlinePosition
	if layer.IsUprightVertical {
		// Central-baseline vertical: left/right keywords ARE applied to
		// select the physical side of the inline-axis run. Mirrors Blink's
		// ResolveUnderlinePosition() for non-CJK + central-baseline:
		//   - kLeft (or auto) → kUnder (block-end side; LEFT in vertical-rl,
		//     RIGHT in vertical-lr because IsSidewaysRL is set with
		//     IsWritingModeVerticalLR=true). For our caller (which only
		//     cares about LEFT vs RIGHT physically), kUnder in vertical-rl
		//     is LEFT, in vertical-lr is RIGHT.
		//   - kRight → kOver, then flip_underline_and_overline_=true
		//     swaps the lines so underline_position becomes kUnder.
		//     After the swap, the underline still ends up on the kUnder
		//     side. So even with `right`, the final underline placement
		//     after the swap is the kUnder side (LEFT in vertical-rl).
		//
		// The flip is applied in shouldFlipUnderlineAndOverline + the
		// caller's flipUnderlineOverlineBits call; sidewaysUnderlineGoesRight
		// returns the post-flip placement.
		if strings.Contains(pos, "left") || strings.Contains(pos, "right") {
			// Both resolve (post-flip if needed) to the kUnder side.
			// kUnder in vertical-rl = LEFT, in vertical-lr = RIGHT.
			return layer.IsWritingModeVerticalLR
		}
	}
	// `under` keyword: follows block-end direction.
	//   vertical-rl / sideways-rl: block-end = LEFT (block progresses to left).
	//   vertical-lr / sideways-lr: block-end = RIGHT.
	if strings.Contains(pos, "under") {
		return layer.IsWritingModeVerticalLR || layer.IsSidewaysLR
	}
	// auto / from-font / empty (and left|right when ignored): follow rotation
	// direction. CCW (IsSidewaysLR) → RIGHT; CW (everything else) → LEFT.
	//
	// Note: the CSS Text Decor 3 §3.5 CJK-default flip (underline auto-resolves
	// to kOver for ja/ko/zh in upright vertical) is realized through
	// shouldFlipUnderlineAndOverline + flipUnderlineOverlineBits — the
	// decoration bits swap upstream, then sidewaysUnderlineGoesRight reports
	// the post-flip placement (still kUnder side). The "RIGHT = over-side"
	// physical placement of the originally-overline (now-underline) flows
	// through the overline-painting branch in drawText.
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
	for i, seg := range segs {
		segBox := *box
		segBox.Text = box.Text[seg.start:seg.end]
		prefix := box.Text[:seg.start]
		var prefixAdvance float64
		if strings.Contains(prefix, "\t") {
			prefixAdvance = r.measureSegmentAdvanceWithTabs(prefix, box, fontID, layer)
		} else {
			prefixAdvance = r.measureSegmentAdvance(prefix, fontID, layer)
		}
		// The prefix advance offsets this segment along the ADVANCE axis:
		// physical X for horizontal text, physical Y for vertical text
		// (drawTextSegment's own IsVerticalText branch advances box.Y by
		// layer.FontSize per character, render.go:6918-6932; its sideways
		// branches likewise treat box.Y as the advance origin — see
		// CHECKPOINT_STATUS.md/LOU-347's Blink-divergence note for why
		// drawTextSegment, not Blink's HighlightPainter, is this fix's
		// reference pattern). box.Height (not box.Width) carries the
		// advance-axis extent under any vertical writing mode, per
		// pkg/layout/writing_mode_converter.go's ToPhysicalSize.
		if layer.IsVerticalText {
			segBox.Y = box.Y + prefixAdvance
		} else {
			segBox.X = box.X + prefixAdvance
		}

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
		//
		// Computed for EVERY segment (not just selected ones) since it also
		// bounds the per-segment paint clip below (LOU-348), not just the
		// selected-segment highlight background rect.
		var segWidth float64
		if strings.Contains(segBox.Text, "\t") {
			segWidth = r.measureSegmentVisualWidthWithTabs(&segBox, fontID, layer)
		} else {
			segWidth = r.measureSegmentVisualWidth(segBox.Text, fontID, layer)
		}
		// selectionBackgroundRectCrossAxis: use the originating element's
		// own inline background-fragment box's CROSS-axis origin/extent
		// when one exists, not box's own (always em-box-sized) cross-axis
		// origin/extent — see that function's doc comment for why these can
		// differ (selection-contenteditable-011.html's red sliver,
		// LOU-344). Cross axis = Y/Height for horizontal text, X/Width for
		// vertical text (box.Width, not box.Height, carries the
		// cross/block-progression extent under any vertical writing mode —
		// see this loop's segBox.Y/segBox.X dispatch above).
		crossOrigin, crossExtent := selectionBackgroundRectCrossAxis(box, layer.IsVerticalText)
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
		// see the sideways branch's "physical box already has
		// Width=lineHeight, Height=textAdvance" comment a few hundred lines
		// below).
		var sx, sy, sw, sh float64
		if layer.IsVerticalText {
			sx, sy, sw, sh = pixelSnap(crossOrigin, segBox.Y, crossExtent, segWidth)
		} else {
			sx, sy, sw, sh = pixelSnap(segBox.X, crossOrigin, segWidth, crossExtent)
		}

		segLayer := *layer
		segLayer.Box = &segBox
		if seg.selected {
			// Background rect behind the selected glyphs, painted BEFORE
			// the recursive drawTextSegment call so glyphs/decorations
			// land on top — same paint order as a normal element
			// background-then-foreground (drawBackground runs in
			// PhaseBackground, text in PhaseForeground).
			if seg.backgroundColor != nil && seg.backgroundColor.A > 0 {
				r.setColor(*seg.backgroundColor)
				r.dc.DrawRectangle(sx, sy, sw, sh)
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
			segBox, segWidth, crossOrigin, crossExtent, segLayer.TextShadows,
			layer.IsVerticalText, layer.FontSize, i == 0, i == len(segs)-1)
		r.dc.Push()
		r.dc.DrawRectangle(clipMinX, clipMinY, clipMaxX-clipMinX, clipMaxY-clipMinY)
		r.dc.Clip()
		r.drawTextSegment(&segLayer)
		r.dc.Pop()
	}
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
// segBox/segWidth are this segment's own advance-axis box/measured extent;
// crossOrigin/crossExtent are the cross-axis origin/extent
// (selectionBackgroundRectCrossAxis's result, shared with the highlight
// fill rect); shadows is the segment's own resolved text-shadow list (nil
// if none); isFirst/isLast mark the first/last segment of the fragment.
//
// The CROSS axis is deliberately left unclipped (padded a full fontSize
// past crossOrigin/crossExtent) rather than clipping tightly to it: Blink's
// own clip derives its cross-axis bound from the fragment's InkOverflowRect
// (actual rasterized ink bounds, which for an italic ascender/descender
// legitimately exceeds the CSS em-box), not from the em-box/line-box
// geometry selectionBackgroundRectCrossAxis computes for sizing the
// highlight FILL rect. This engine has no per-fragment ink-overflow
// tracking to mirror that exactly; reusing the tight em-box rect here
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
// segWidth's right edge, the last segment) was clipped away. INTERIOR
// boundaries stay tight at shadowPad (0 when this segment has no shadow) —
// that boundary is exactly where this bug's cross-segment glyph-ink bleed
// happens and must stay clipped absent a shadow's own wider requirement.
//
// The advance-axis origin/extent used below is segBox.X/segWidth — the
// SAME raw (non-pixel-snapped) values drawTextStr positions this segment's
// glyphs from (drawTextStr's own doc comment: "X is passed through
// fractionally so the rasterizer's subpixel pen carry... produces
// consistent glyph positioning") — NOT a pixelSnap'd rect (as used for the
// highlight FILL rect). Using a pixel-snapped rect here misaligned the clip
// boundary from the glyph rasterizer's own sub-pixel grid by a fraction of
// a pixel, clipping one extra antialiased fringe pixel at every INTERIOR
// segment boundary vs. an unclipped reference (highlight-painting-
// currentcolor-003a's 13-pixel regression, confirmed empirically:
// switching this from a pixel-snapped rect to segBox.X/segWidth took it to
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
func segmentClipRect(segBox layout.Box, segWidth, crossOrigin, crossExtent float64, shadows []css.TextShadow, isVerticalText bool, fontSize float64, isFirst, isLast bool) (minX, minY, maxX, maxY float64) {
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
		clipX, clipY, clipW, clipH = crossOrigin-pad, segBox.Y-advStart, crossExtent+2*pad, segWidth+advStart+advEnd
	} else {
		clipX, clipY, clipW, clipH = segBox.X-advStart, crossOrigin-pad, segWidth+advStart+advEnd, crossExtent+2*pad
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
		textWidth := r.measureTextStr(text, fontID, layer.featureInputs())

		// CSS Text Decor 3 §3.5: for non-CJK upright/mixed vertical text,
		// `text-underline-position: right` resolves to kOver, which in Blink
		// triggers flip_underline_and_overline_ — the underline/overline bits
		// on each AppliedTextDecoration are swapped, then the underline is
		// painted at the kUnder side. Mirrors text_decoration_info.cc:223-237
		// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f. We apply the swap to
		// a local copy and substitute it on the layer for the duration of the
		// sideways paint so the downstream painters see the flipped lines.
		if shouldFlipUnderlineAndOverline(layer) && len(layer.AppliedTextDecorations) > 0 {
			origDecorations := layer.AppliedTextDecorations
			layer.AppliedTextDecorations = flipUnderlineOverlineBits(origDecorations)
			defer func() { layer.AppliedTextDecorations = origDecorations }()
		}

		hasDecor := len(layer.AppliedTextDecorations) > 0 || (layer.TextDecoration != css.TextDecorationNone && layer.TextDecoration != "")

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
			virtualBox := &layout.Box{X: 0, Y: 0, Width: float64(ta), Height: float64(lh)}
			tmpInfo := newVerticalTextDecorationInfo(virtualBox, textWidth, layer.FontSize, ascent, descent, 0, layer.blockUnderDirection())
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
			//   logicalEnd   = box.X + textWidth - Inset.InlineEnd
			// lands the painted rect inside the extended buffer
			// [0, taExt) — and HasDecoratingBox=true branches anchor
			// at box.X + DecoratingBoxOffsetX, which Stays valid too.
			virtualBox := &layout.Box{X: float64(inlineStartExt), Y: float64(textOff), Width: float64(ta), Height: float64(lhExt)}
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
		// This matches the non-CJK default. For CJK upright vertical
		// (TextLangIsCJK), shouldFlipUnderlineAndOverline swaps the
		// underline/overline bits upstream, so a `text-decoration: overline`
		// reaches this branch as underline (painted on physical LEFT in
		// vertical-rl, matching the lang=en underline reference), and a
		// `text-decoration: underline` reaches the overline-painting path
		// (physical RIGHT in vertical-rl, the CJK over-side default).
		if hasDecor {
			origDC := r.dc
			r.dc = childDC
			if underBelow {
				// Phase 1 (underline/overline) already ran before glyph drawing.
				// Phase 2: line-through only, painted after glyphs so it renders over them.
				virtualBox := &layout.Box{X: float64(inlineStartExt), Y: float64(textOff), Width: float64(ta), Height: float64(lhExt)}
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
				info := newVerticalTextDecorationInfo(&layout.Box{X: 0, Width: float64(ta)}, textWidth, layer.FontSize, ascent, descent, 0, layer.blockUnderDirection())
				for _, td := range layer.AppliedTextDecorations {
					if !td.Lines.Has(css.TextDecorationLineUnderline) {
						continue
					}
					th := info.computeThickness(td)
					gap := info.computeUnderlineZeroOffset(td.UnderlinePosition)
					rectTopSrcY := float64(topExt) - gap - th - td.UnderlineOffset
					xStart := float64(inlineStartExt)
					xEnd := float64(inlineStartExt + ta)
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
		r.dc.DrawImage(rot, blitX, blitY)

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
	// advance in the block-progression (vertical) direction. Each character
	// cell is its vertical advance tall (uprightVerticalAdvances) and the
	// glyph is centered horizontally.
	if layer.IsVerticalText {
		y := box.Y
		advances := uprightVerticalAdvances(layer, text, fontPath)
		for i, ch := range []rune(text) {
			charStr := string(ch)
			charW := r.measureTextStr(charStr, fontID, layer.featureInputs())
			xOffset := (box.Width - charW) / 2
			if xOffset < 0 {
				xOffset = 0
			}
			r.drawTextStr(charStr, fontID, box.X+xOffset, y+ascent, layer.featureInputs())
			y += advances[i]
		}
		// Paint text decorations for upright vertical text (Strategy B).
		// Mirrors Blink's writing-mode dispatch at
		// third_party/blink/renderer/core/paint/text_fragment_painter.cc:609-618
		// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
		r.drawVerticalTextDecoration(layer, text, advances, box, fontID, ascent)
		return
	}

	// Text overflow: ellipsis or custom <string> — truncate text if it
	// overflows the nearest ancestor block container that has
	// overflow-x in {hidden, scroll, auto, clip}. CSS UI 4 §6.2 has the
	// replacement string rendered in the block container's font, so
	// applyTextOverflowEllipsis returns the truncated text plus an
	// `ellipsisPaint` describing where/what to draw separately.
	var ellipsisPaintInfo *ellipsisPaint
	text, ellipsisPaintInfo = r.applyTextOverflowEllipsis(layer, text, box, fontID)

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

	// Paint the text-overflow replacement (CSS UI 4 §6.2). Drawn LAST so
	// it sits on top of the (truncated) inline text. The replacement uses
	// the block container's font and color, not the inline run's, and
	// must NOT receive the run's text-decoration or text-emphasis marks
	// — string-004 explicitly checks that text-emphasis on the container
	// does not decorate the ellipsis.
	if ellipsisPaintInfo != nil {
		r.paintTextOverflowEllipsis(layer, box, ellipsisPaintInfo)
	}
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

	emphMetrics := r.dc.GetFontMetrics(emphFontID)
	emphAscent := float64(emphMetrics.Ascent) / 64.0
	emphDescent := float64(emphMetrics.Descent) / 64.0
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
	emphY := box.Y - emphDescent - emphAscent

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
// ta and lh are the parent's off-screen horizontal-frame dimensions
// (text-advance and line-height); used to compute the rotation mapping.
func (r *Renderer) drawSidewaysEmphasisMarks(layer *PaintLayer, text string, box *layout.Box, fontID int32, ascent float64, ta, lh int) {
	mark := layer.TextEmphasisMark
	emphFontSize := layout.EmphasisMarkFontSize(layer.FontSize)
	fontPath := r.fontPathForLayer(layer)
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

	// Compute the loop-invariant screen X for the annotation.
	//
	// All sideways emphasis annotations paint on the right (kOver) side of the
	// base text column. CSS Text Decor §3.4 allows kUnder (left in vertical-rl)
	// but louis14 delegates emphasis placement to ruby positioning; because
	// ruby-position:under is not yet implemented it falls back to over (right),
	// so every reference rendering that uses ruby-position:under to express the
	// left-side position also ends up on the right. Both kOver and kUnder
	// positions are therefore the same screen X.
	//
	// Two baseline modes apply, differing only in the X offset:
	//
	// Central baseline (vertical-rl/lr + text-orientation: mixed, the default):
	//   annotationAscent = fontSize/2 = annotH, so X = box.X + lh.
	//   Mirrors ruby annotation layout where lh = lineHeight = 2×annotH for a
	//   0.5-em mark: lh = annotH + baseColWidth = annotH + annotH.
	//
	// Alphabetic baseline (text-orientation: sideways, or writing-mode:sideways-rl):
	//   annotationAscent = emphTypoAscent + emphTypoDescent ≠ annotH
	//   (5.547 + 1.719 = 7.266 vs annotH = 8 for Liberation Mono 8pt).
	//   X = box.X + emphAscent + emphDescent + annotH.
	//   Derivation: X_annotation − X_base = annotationAscent + annotation.W_phys
	//               = (emphAscent + emphDescent) + annotH.
	//
	// Blink ref: TextEmphasisPainter::Paint → kOver X offset in
	// third_party/blink/renderer/core/paint/text_emphasis_painter.cc
	// (Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	isCentralBaseline := false
	isVLR := false
	if box.Style != nil {
		wm, _ := box.Style.Get("writing-mode")
		to, _ := box.Style.Get("text-orientation")
		// vertical-rl/vertical-lr with text-orientation != "sideways" uses the
		// central baseline; horizontal-tb (wm == "") is not sideways.
		// writing-mode: sideways-rl/sideways-lr always uses alphabetic baseline.
		if wm == "vertical-rl" || wm == "vertical-lr" || wm == "" {
			isCentralBaseline = to != "sideways"
		}
		isVLR = wm == "vertical-lr"
	}
	// For vertical-lr, recover the vertical-rl-equivalent fragment X before
	// either baseline branch. The WPT text-emphasis reftests share a single
	// vertical-rl reference for both rl and lr variants, so the mark X must
	// match the vertical-rl geometry. When the line box is symmetric
	// (maxAscent == maxDescent, the pre-LOU-299 situation) the formula
	// reduces to box.X and is a no-op; once emphasis marks inflate the
	// line's ascent side (emphasisMarkAscentFromLine, LOU-299) the run sits
	// asymmetrically within the line and vertical-lr's blockPos anchor
	// diverges from vertical-rl's flipped anchor. (For the alphabetic
	// branch the divergence pre-dates LOU-299 — see the rounding-asymmetry
	// derivation below.)
	baseX := box.X
	if isVLR && box.Parent != nil {
		baseX = 2*box.Parent.X + box.Parent.Width - box.X - float64(lh)
	}
	var screenX float64
	if isCentralBaseline {
		// annotationAscent = annotH for central baseline → X = baseX + lh.
		screenX = baseX + float64(lh)
	} else {
		// Alphabetic baseline (text-orientation: sideways, or sideways-rl/lr).
		//
		// annotationAscent = emphAscent + emphDescent for alphabetic baseline.
		// The base formula X = box.X + emphAscent + emphDescent + annotH is
		// correct for vertical-rl and sideways-rl.
		//
		// For vertical-lr + sideways: CSS Writing Modes §7.3 specifies the same
		// 90°-CW glyph rotation as vertical-rl + sideways; the visual position
		// must match the vertical-rl reference (WPT reftests share a single
		// vertical-rl reference for both rl and lr variants). However the layout
		// engine places the lr text fragment 2 px further right than rl due to
		// rounding asymmetry:
		//   vertical-rl: X = round(lineHeight − blockPos − fontSize)
		//   vertical-lr: X = round(blockPos)
		// When blockPos is non-integer (e.g. 32.735 from asymmetric
		// font metrics + half-leading), these round to different integers.
		// The VLR baseline swap is correct for inline-block alignment
		// (inline-block-alignment-007.xht) so we cannot change layout.
		// Instead, the hoisted baseX above computes the VRL-equivalent
		// annotation X from the parent block container:
		//   X_rl_equiv = 2·parent.X + parent.Width − box.X − fontSize
		// which inverts X_lr = parent.X + blockPos_lr to recover the rl formula
		// X_rl = parent.X + (lineHeight − blockPos_rl − fontSize), using
		// the fact that blockPos_lr = blockPos_rl (same halfLeading).
		screenX = baseX + emphAscent + emphDescent + float64(annotH)
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

// ellipsisPaint carries the information drawText needs to paint a
// text-overflow replacement (the ellipsis character "…" or a custom
// <string>) in the block container's font rather than the inline run's
// font. CSS UI 4 §6.2 stipulates that the replacement string is
// rendered using the styles of the block container, so the truncated
// run's font is irrelevant for the ellipsis itself.
//
// Mirrors Blink's logic where the text-overflow algorithm builds a
// separate fragment item for the ellipsis tied to the block container's
// ComputedStyle (see `text_overflow.cc::PlaceEllipsis` @ SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
type ellipsisPaint struct {
	str            string     // replacement string, e.g. "…" or "123"
	x              float64    // physical x of the LEFT edge of the replacement
	containerStyle *css.Style // block container whose font/color to use
}

// ellipsisChar is the U+2026 HORIZONTAL ELLIPSIS character used as the
// default replacement when text-overflow: ellipsis applies.
const ellipsisChar = "…"

// isBlockContainerForTextOverflow reports whether `style` describes a
// box that is a block container for the purpose of CSS UI 4 §6.2.
// `display: inline` and similar inline-level values do NOT make a
// block container — text-overflow on the parent of an inline element
// still applies to text inside that inline. But `display: block`,
// `inline-block`, table cells, list items, flex/grid items, etc.
// establish their own block context and shadow a more distant
// ancestor's text-overflow.
func isBlockContainerForTextOverflow(style *css.Style) bool {
	switch style.GetDisplay() {
	case css.DisplayInline:
		// True inline elements (spans) are NOT block containers; the
		// text inside them belongs to the enclosing block.
		return false
	}
	return true
}

// textOverflowReplacement returns the replacement string the inline
// run's own text-overflow value implies, or "" if it implies none.
func textOverflowReplacement(layer *PaintLayer) string {
	switch layer.TextOverflow {
	case css.TextOverflowEllipsis:
		return ellipsisChar
	case css.TextOverflowString:
		return layer.TextOverflowString
	}
	return ""
}

// applyTextOverflowEllipsis decides whether text-overflow truncation
// applies to this text run. If so, it returns the truncated text plus
// an *ellipsisPaint describing where and what to draw for the
// replacement. The replacement is drawn separately by the caller in
// the block container's font (per CSS UI 4 §6.2), NOT in the inline
// run's font.
func (r *Renderer) applyTextOverflowEllipsis(layer *PaintLayer, text string, box *layout.Box, fontID int32) (string, *ellipsisPaint) {
	// Determine if text-overflow applies on this run.
	hasOverflow := layer.TextOverflow == css.TextOverflowEllipsis ||
		layer.TextOverflow == css.TextOverflowString
	replacement := textOverflowReplacement(layer)

	// Walk up the box tree to the FIRST block-container ancestor — that
	// is, the block container the text fragment belongs to. CSS UI 4
	// §6.2 says text-overflow "applies to block container elements",
	// meaning the block container of the text is the one that decides
	// whether and how to truncate. A more distant ancestor's
	// text-overflow does NOT reach through an intervening block (which
	// establishes its own block formatting context). text-overflow-020
	// is the canonical "nested paragraph won't ellipse" case.
	var clipAncestor *layout.Box
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Style == nil {
			continue
		}
		if !isBlockContainerForTextOverflow(p.Style) {
			continue
		}
		overflowX := p.Style.GetOverflowX()
		if overflowX == css.OverflowHidden || overflowX == css.OverflowScroll ||
			overflowX == css.OverflowAuto || overflowX == css.OverflowClip {
			if !hasOverflow {
				switch p.Style.GetTextOverflow() {
				case css.TextOverflowEllipsis:
					hasOverflow = true
					if replacement == "" {
						replacement = ellipsisChar
					}
				case css.TextOverflowString:
					hasOverflow = true
					replacement = p.Style.GetTextOverflowString()
				}
			}
			if hasOverflow {
				clipAncestor = p
			}
		}
		// First block container reached: stop the walk regardless of
		// whether it has overflow:hidden or text-overflow:ellipsis.
		// A more distant ancestor's text-overflow is not visible to
		// this run.
		break
	}

	if !hasOverflow || clipAncestor == nil || replacement == "" {
		return text, nil
	}

	// Compute available width: right edge of ancestor's padding box minus
	// the text run's starting X position. The padding box is the overflow
	// clip boundary (CSS Overflow §3), so the replacement must fit within
	// it.
	paddingRight := clipAncestor.X + clipAncestor.Width - clipAncestor.Border.Right
	availW := paddingRight - box.X
	if availW <= 0 {
		return text, nil
	}

	// Account for letter-spacing and word-spacing in run-width measurements.
	ls := layer.LetterSpacing
	ws := layer.WordSpacing
	measureRunWidth := func(s string) float64 {
		if ls == 0 && ws == 0 {
			return r.measureTextStr(s, fontID, layer.featureInputs())
		}
		total := 0.0
		runes := []rune(s)
		for i, ch := range runes {
			total += r.measureTextStr(string(ch), fontID, layer.featureInputs())
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
		return text, nil
	}

	// Measure the replacement in the *block container's* font (CSS UI 4
	// §6.2), not the inline run's font. Open a fontID for the container
	// so the inline run's Ahem (or other large) font doesn't inflate the
	// replacement width and chew up too many inline glyphs.
	containerFontID := r.openContainerFont(clipAncestor)
	var replacementW float64
	if containerFontID >= 0 {
		replacementW = r.measureTextStr(replacement, containerFontID, fontFeatureInputs{})
	} else {
		// Fallback: measure with inline font.
		replacementW = r.measureTextStr(replacement, fontID, layer.featureInputs())
	}
	truncW := availW - replacementW

	// CSS UI 4 §6.2 / Blink `text_overflow.cc::PlaceEllipsis`: the FIRST
	// glyph of the line is always preserved — it is clipped by
	// overflow:hidden rather than replaced by the ellipsis. This handles
	// the degenerate case where availW is smaller than the first glyph's
	// advance: we keep the first glyph and suppress the ellipsis entirely
	// (its insertion would shift the first glyph or hide it, neither of
	// which the spec permits). See text-overflow-008 / -014 / -020.
	//
	// Note that this only applies when the text run is anchored at the
	// content edge (i.e., box.X is at or before the padding box's left
	// edge); a non-leading run that exceeds available width still gets a
	// normal ellipsis.
	runes := []rune(text)
	leadingX := clipAncestor.X + clipAncestor.Border.Left
	isLeadingRun := box.X <= leadingX+0.5
	if len(runes) >= 1 {
		firstAdvance := r.measureTextStr(string(runes[0]), fontID, layer.featureInputs())
		if isLeadingRun && firstAdvance > availW {
			// First glyph doesn't fit; preserve it and skip the ellipsis.
			return string(runes[0]), nil
		}
	}

	if truncW <= 0 {
		// Replacement (in container font) is wider than the available
		// space.
		//
		// CSS UI 4 §6.2: when the replacement is a <string> "and there
		// is insufficient space for the entire string, then the string
		// can be clipped to make it fit". Accept as many inline glyphs
		// as fit in availW (in inline font), then emit a clipped
		// replacement filling the remaining space. text-overflow-string-002.
		//
		// For ellipsis (the U+2026 character), the leading-run rule from
		// Blink's text_overflow.cc::PlaceEllipsis applies instead: keep
		// the first glyph clipped by overflow and suppress the ellipsis.
		// text-overflow-008 / -010 / -020.
		if layer.TextOverflow == css.TextOverflowString ||
			(layer.TextOverflow != css.TextOverflowEllipsis && replacement != ellipsisChar) {
			// Custom <string>: accept text in inline font, clip replacement.
			textTaken, textW2 := acceptInlineGlyphs(r, layer, fontID, runes, availW, ls, ws)
			remaining := availW - textW2
			clipped := clipReplacement(r, replacement, containerFontID, fontID, layer.featureInputs(), remaining)
			if clipped == "" {
				return textTaken, nil
			}
			return textTaken, &ellipsisPaint{
				str:            clipped,
				x:              box.X + textW2,
				containerStyle: clipAncestor.Style,
			}
		}
		// Ellipsis case.
		if isLeadingRun && len(runes) >= 1 {
			return string(runes[0]), nil
		}
		return "", &ellipsisPaint{
			str:            replacement,
			x:              box.X,
			containerStyle: clipAncestor.Style,
		}
	}

	// Find the truncation point: walk the run accepting glyphs up to truncW,
	// using Blink's spacing model (trailing spacing of last retained glyph excluded).
	truncText, glyphEnd := acceptInlineGlyphs(r, layer, fontID, runes, truncW, ls, ws)

	return truncText, &ellipsisPaint{
		str:            replacement,
		x:              box.X + glyphEnd,
		containerStyle: clipAncestor.Style,
	}
}

// acceptInlineGlyphs walks `runes` accepting cumulative inline glyphs
// up to `limitW` in the inline font, returning the byte index just
// past the accepted prefix and the cumulative advance (inline-end of
// the last accepted glyph, without trailing spacing).
//
// Used by the CSS UI 4 §6.2 clipped-<string> path where the
// replacement is wider than availW: as many text glyphs are accepted
// as fit, then the replacement is clipped to fill the rest.
func acceptInlineGlyphs(r *Renderer, layer *PaintLayer, fontID int32, runes []rune, limitW, ls, ws float64) (string, float64) {
	glyphEnd := 0.0 // Inline-end of the last accepted glyph, without trailing spacing.
	idx := 0
	var prevCh rune
	for i, ch := range runes {
		cw := r.measureTextStr(string(ch), fontID, layer.featureInputs())
		// Compute the inline-end if this glyph is accepted: glyphEnd + inter-glyph
		// spacing from the previous glyph + this glyph's content.
		nextGlyphEnd := glyphEnd + cw
		// The spacing that precedes this glyph (from the previous glyph).
		if i > 0 {
			nextGlyphEnd += ls
			if prevCh == ' ' {
				nextGlyphEnd += ws
			}
		}
		// Fit test: does this glyph (with its preceding spacing) fit within limitW?
		if nextGlyphEnd > limitW {
			break
		}
		// Accept this glyph: update the inline-end position.
		glyphEnd = nextGlyphEnd
		idx = len(string(runes[:i+1]))
		prevCh = ch
	}
	return string(runes)[:idx], glyphEnd
}

// clipReplacement returns the longest prefix of `replacement` whose
// width in the container font is <= `limitW`. The result may be empty
// when even the first character does not fit, per CSS UI 4 §6.2's
// "the string can be clipped" allowance.
func clipReplacement(r *Renderer, replacement string, containerFontID, inlineFontID int32, inlineFeatures fontFeatureInputs, limitW float64) string {
	if limitW <= 0 {
		return ""
	}
	fid := containerFontID
	var features fontFeatureInputs
	if fid < 0 {
		fid = inlineFontID
		features = inlineFeatures
	}
	runes := []rune(replacement)
	w := 0.0
	idx := 0
	for i, ch := range runes {
		cw := r.measureTextStr(string(ch), fid, features)
		if w+cw > limitW {
			break
		}
		w += cw
		idx = len(string(runes[:i+1]))
	}
	return replacement[:idx]
}

// openContainerFont opens a fontID for the given clip ancestor's
// computed font (family / size / weight / style). Returns -1 when the
// ancestor has no style.
func (r *Renderer) openContainerFont(ancestor *layout.Box) int32 {
	if ancestor == nil || ancestor.Style == nil {
		return -1
	}
	st := ancestor.Style
	fontSize := st.GetUsedFontSize()
	if fontSize <= 0 {
		fontSize = 16
	}
	bold := st.GetFontWeight() == css.FontWeightBold
	italic := st.GetFontStyle() == css.FontStyleItalic
	mono := st.IsMonospaceFamily()
	ahem := st.IsAhemFamily()
	family, _ := st.Get("font-family")
	synth := st.GetFontSynthesis()
	fontPath := r.fonts.FontPathForFamilyWithSynthesis(
		family, bold, italic, mono, ahem,
		synth.Weight, synth.Style)
	return r.openFont(fontPath, fontSize)
}

// paintTextOverflowEllipsis draws the ellipsis (or custom replacement
// string) in the block container's font at the previously-computed x
// position, aligned to the inline run's baseline (so the replacement
// sits on the same baseline as the truncated text).
//
// Per CSS UI 4 §6.2 the replacement is rendered using the styles of
// the block container; when the inline run is on the container's first
// line, the container's ::first-line pseudo styles (color/font subset)
// apply to the ellipsis as well — matching Blink's behavior where the
// ellipsis fragment is built into the first line's fragments.
func (r *Renderer) paintTextOverflowEllipsis(layer *PaintLayer, box *layout.Box, ep *ellipsisPaint) {
	if ep == nil || ep.containerStyle == nil {
		return
	}
	st := ep.containerStyle

	// If the container has a ::first-line pseudo and the inline run sits
	// on its first line, layer first-line property overrides on top of
	// the container's style for color/font resolution. Detection:
	// box.Y matches the container's content-top within line-height (i.e.,
	// this text fragment is on the first formatted line of the
	// container). The first-line allowed-properties subset (CSS Pseudo
	// §3) is the same one that `applyFirstLineStyles` in inline layout
	// uses; we only need the color/font subset here.
	colorStyle := st
	fontStyle := st
	if firstLineSt := containerFirstLineStyle(box, ep.containerStyle); firstLineSt != nil {
		colorStyle = firstLineSt
		fontStyle = firstLineSt
	}

	fontSize := fontStyle.GetUsedFontSize()
	if fontSize <= 0 {
		fontSize = 16
	}
	bold := fontStyle.GetFontWeight() == css.FontWeightBold
	italic := fontStyle.GetFontStyle() == css.FontStyleItalic
	mono := fontStyle.IsMonospaceFamily()
	ahem := fontStyle.IsAhemFamily()
	family, _ := fontStyle.Get("font-family")
	synth := fontStyle.GetFontSynthesis()
	fontPath := r.fonts.FontPathForFamilyWithSynthesis(
		family, bold, italic, mono, ahem,
		synth.Weight, synth.Style)
	containerFontID := r.openFont(fontPath, fontSize)
	if containerFontID < 0 {
		return
	}

	// Baseline alignment: inline run's baseline = box.Y + inlineAscent.
	// Use the container font's ascent so the replacement's baseline
	// matches the inline run's baseline.
	inlineFontPath := r.fontPathForLayer(layer)
	inlineFontID := r.openFont(inlineFontPath, layer.FontSize)
	if inlineFontID < 0 {
		return
	}
	inlineAscent := float64(r.dc.GetFontMetrics(inlineFontID).Ascent) / 64.0
	baselineY := box.Y + inlineAscent

	// Draw the replacement in the container's text color (CSS UI 4 §6.2:
	// the replacement uses the styles of the block container, optionally
	// overridden by ::first-line on the first line).
	containerColor := colorStyle.GetColor()
	r.setColor(containerColor)
	r.drawTextStr(ep.str, containerFontID, ep.x, baselineY, fontFeatureInputs{})
	// Restore the layer's text color so subsequent paint operations
	// (decorations, emphasis) continue with the inline run's color.
	r.setColor(layer.TextColor)
}

// containerFirstLineStyle returns a *css.Style with the container's
// ::first-line overrides layered on top of its base style, if and only
// if `box` is positioned on the container's first formatted line.
// Returns nil otherwise (the caller falls back to the base container
// style).
//
// Detection: louis14's inline layout (`applyFirstLineStyles` at
// `pkg/layout/inline_layout.go:1196`) sets each first-line result's
// Style to a cloned style with the first-line property overrides
// applied. We detect that override by checking whether the fragment's
// own style has the same color value as the ::first-line color
// override — if so, the fragment belongs to the first line.
//
// Mirrors Blink's `LayoutObject::FirstLineStyle` for the purpose of
// styling the text-overflow ellipsis (`text_overflow.cc::PaintLine` @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func containerFirstLineStyle(box *layout.Box, containerStyle *css.Style) *css.Style {
	if box == nil || containerStyle == nil {
		return nil
	}
	// Walk to the clip ancestor (the box whose Style == containerStyle).
	var ancestor *layout.Box
	for p := box.Parent; p != nil; p = p.Parent {
		if p.Style == containerStyle {
			ancestor = p
			break
		}
	}
	if ancestor == nil || ancestor.LayoutNode == nil || ancestor.LayoutNode.FirstLineStyle == nil {
		return nil
	}
	firstLineStyle := ancestor.LayoutNode.FirstLineStyle
	// Sentinel comparison: a property that the first-line pseudo
	// changes from the container's value indicates the fragment is on
	// the first line.
	if !boxOnFirstLine(box, containerStyle, firstLineStyle) {
		return nil
	}
	// Build a merged style: clone the container's base style, then
	// overlay first-line properties.
	merged := containerStyle.Clone()
	for prop, val := range firstLineStyle.Properties {
		if val != "" {
			merged.Properties[prop] = val
		}
	}
	return merged
}

// boxOnFirstLine reports whether `box` (a text fragment) lives on the
// container's first formatted line. It walks the first-line property
// set looking for a property whose value differs between the container
// and the ::first-line pseudo, then compares the fragment's resolved
// value to the ::first-line value.
func boxOnFirstLine(box *layout.Box, containerStyle, firstLineStyle *css.Style) bool {
	if box == nil || box.Style == nil {
		return false
	}
	for prop, flVal := range firstLineStyle.Properties {
		if flVal == "" {
			continue
		}
		containerVal, _ := containerStyle.Get(prop)
		if containerVal == flVal {
			continue
		}
		fragVal, _ := box.Style.Get(prop)
		if fragVal == flVal {
			return true
		}
	}
	return false
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
	if len(layer.AppliedTextDecorations) == 0 &&
		(layer.TextDecoration == css.TextDecorationNone || layer.TextDecoration == "") {
		return
	}

	metrics := r.dc.GetFontMetrics(fontID)
	descent := float64(metrics.Descent) / 64.0
	textWidth := r.measureTextStr(text, fontID, layer.featureInputs())

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
				tailWidth := r.measureTextStr(tail, fontID, layer.featureInputs())
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
				headWidth := r.measureTextStr(head, fontID, layer.featureInputs())
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
		// For upright vertical text in the sideways paint path, use newVerticalTextDecorationInfo
		// so that text-underline-position: under applies only to alphabetic-baseline runs.
		var info textDecorationInfo
		if layer.IsUprightVertical && (layer.IsSidewaysRL || layer.IsSidewaysLR) {
			info = newVerticalTextDecorationInfo(box, textWidth, layer.FontSize, ascent, descent, 0, layer.blockUnderDirection())
		} else {
			info = newTextDecorationInfo(box, textWidth, layer.FontSize, ascent, descent, 0)
		}
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
		var glyphs []glyphExtent
		if layer.TextDecorationSkipInk == css.TextDecorationSkipInkAuto && layer.FontAhem {
			glyphs = r.buildGlyphExtentsHorizontal(text, fontID, layer.featureInputs(), box.X, layer.LetterSpacing, layer.WordSpacing)
		}
		for _, td := range layer.AppliedTextDecorations {
			r.drawOneAppliedTextDecoration(td, info, box, textWidth, skipStartWidth, skipEndWidth, glyphs, onlyLineThrough)
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
	case css.TextDecorationOverline:
		if onlyLineThrough {
			return
		}
		centerY = box.Y
		rectTop = centerY - thickness // overline rect grows UPWARD from TextTop
	case css.TextDecorationLineThrough:
		if !onlyLineThrough {
			return
		}
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
//   - Bare (single-fragment fallback): the extent spans [boxX, boxX+textWidth].
//
// Inset adjustment is delegated to applyDecorationInset.
func computeDecorationExtent(td css.AppliedTextDecoration, isRTL bool, boxX, textWidth float64) (logicalStart, logicalEnd float64) {
	if td.HasDecoratingBox {
		logicalStart = boxX + td.DecoratingBoxOffsetX
		logicalEnd = logicalStart + td.DecoratingBoxWidth
	} else {
		logicalStart = boxX
		logicalEnd = boxX + textWidth
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
// sub-pixel double-coverage at adjacent fragment joints.
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
func clampDecorationBounds(logicalStart, logicalEnd float64, td css.AppliedTextDecoration, box *layout.Box) (xStart, xEnd float64, skip bool) {
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
		if !last && box.X+box.Width < xEnd {
			xEnd = box.X + box.Width
		}
		if xEnd <= xStart {
			return xStart, xEnd, true
		}
	}
	return xStart, xEnd, false
}

// drawLineThroughStroke paints a line-through in the style given by styleVal
// ("solid", "dashed", "dotted", "double", "wavy"). lineY is the stroke
// center, thickness the stroke width. phaseFromLogicalStart keeps dashed/
// dotted/wavy patterns phase-continuous across adjacent fragments.
func (r *Renderer) drawLineThroughStroke(xStart, xEnd, lineY, thickness, phaseFromLogicalStart float64, styleVal string) {
	switch styleVal {
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
func (r *Renderer) drawOneAppliedTextDecoration(td css.AppliedTextDecoration, info textDecorationInfo, box *layout.Box, textWidth, skipStartWidth, skipEndWidth float64, glyphs []glyphExtent, onlyLineThrough bool) {
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
	logicalStart, logicalEnd := computeDecorationExtent(td, isRTL, box.X, textWidth)
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
	// overflows the fragment. Clamp uses box.Width (layout-time) to match
	// the layout-time DecoratingBoxOffsetX / Width values; clamping by paint-
	// time textWidth would leak sub-pixel drift between adjacent fragments.
	//
	// Interior left edge: ceil to the next integer pixel boundary. When
	// adjacent fragments share a sub-pixel boundary (e.g. xEnd=467.5 for
	// fragment N, xStart=467.5 for fragment N+1), the sharp-pixel rasterizer
	// gives non-zero coverage to pixel 467 for BOTH rects. Ceiling the
	// interior left edge prevents the later-painted fragment from overwriting
	// a child element's decoration that was painted on top of the earlier
	// fragment's decoration at that same pixel. The previous fragment's right
	// edge already covers the pixel (its coverage is clamped at box.X +
	// box.Width ≤ xEnd_ceil), so no visual gap is introduced. Blink avoids
	// this entirely by painting each decorating box's line in one rect rather
	// than per fragment; ceil is the louis14-specific equivalent.
	xStart, xEnd, skip := clampDecorationBounds(logicalStart, logicalEnd, td, box)
	if skip {
		return
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
	// to line-through per spec.
	paintLine := func(rectTop, centerY float64, skipInk bool) {
		segments := []inkSegment{{Start: xStart, End: xEnd}}
		// Apply skip-ink only when the line's perpendicular position
		// (rect [rectTop, rectTop+thickness]) overlaps the glyph ink.
		lineTop := rectTop
		lineBottom := rectTop + thickness
		linePerpOverlapsGlyph := lineBottom > glyphYTop && lineTop < glyphYBottom
		if skipInk && len(glyphs) > 0 && linePerpOverlapsGlyph {
			segments = skipInkSegments(xStart, xEnd, thickness, glyphs)
		}
		for _, seg := range segments {
			segPhase := phaseFromLogicalStart + (seg.Start - xStart)
			switch td.Style {
			case "dashed":
				r.drawDashedLinePhased(seg.Start, centerY, seg.End, centerY, thickness, segPhase)
			case "dotted":
				r.drawDottedLinePhased(seg.Start, centerY, seg.End, centerY, thickness, segPhase)
			case "double":
				r.drawDoubleLine(seg.Start, centerY, seg.End, centerY, thickness)
			case "wavy":
				r.drawWavyLinePhased(seg.Start, centerY, seg.End-seg.Start, thickness, segPhase)
			default: // "solid"
				// Path-based fill respects the layer's overflow:auto/hidden clip
				// (the rasterizer's fillWithPattern applies dc.gs.clipMask/
				// clipRect). FillRectangle's fast path bypasses that clip and
				// leaks underlines outside the scroll box (regresses
				// text-underline-offset-scroll-001).
				r.dc.DrawRectangle(seg.Start, rectTop, seg.End-seg.Start, thickness)
				r.dc.Fill()
			}
		}
	}

	if !onlyLineThrough && td.Lines.Has(css.TextDecorationLineUnderline) {
		// computeUnderlineLineY returns Blink's `paint_underline_offset`
		// (= rect TOP for solid). Round to integer pixels so the underline
		// aligns with the rounded baseline used by drawTextStr (math.Round),
		// preventing a sub-pixel gap that exposes the underline below the glyph.
		lineY := math.Round(info.computeUnderlineLineY(td))
		paintLine(lineY, lineY, true)
	}
	if !onlyLineThrough && td.Lines.Has(css.TextDecorationLineOverline) {
		// computeOverlineLineY returns the TextTop position (box.Y), which
		// is the stroke center for the non-solid path (matches the
		// pre-port baseline). Blink's overline rect spans
		// [TextTop - thickness, TextTop], so for the SOLID path the rect
		// TOP is `TextTop - thickness` (overline grows UPWARD from
		// text-top — see text-decoration-thickness-overline-001).
		centerY := info.computeOverlineLineY()
		paintLine(centerY-thickness, centerY, true)
	}
	if onlyLineThrough && td.Lines.Has(css.TextDecorationLineLineThrough) {
		// Line-through stays stroke-centered for all styles. Blink centers
		// at `2*ascent/3` (it adds the `-thickness/2` offset to keep the
		// stroke around that center point). Switching to rect-top would
		// regress text-decoration-thickness-linethrough-001 which uses
		// 1.1em thickness on a 1em-tall box.
		lineY := info.computeLineThroughLineY(thickness)
		r.drawLineThroughStroke(xStart, xEnd, lineY, thickness, phaseFromLogicalStart, td.Style)
	}
}

// drawVerticalTextDecoration paints underline, overline, or line-through for
// upright vertical text (writing-mode: vertical-rl or vertical-lr with
// text-orientation: upright). This is Strategy B from the C54 plan.
//
// For upright vertical text, the inline axis is physical-Y and the block axis
// is physical-X. The decoration is a VERTICAL rect (full height of the text
// column, some perpendicular X coordinate). Mirrors the writing-mode dispatch at
// third_party/blink/renderer/core/paint/text_fragment_painter.cc:609-618
// @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// The box passed in is the physical bounding box of the upright vertical run.
// box.Width is the line height (physical block extent). box.Height is the
// text column height (physical inline extent). advances is the run's
// uprightVerticalAdvances, computed once by the caller's paint loop.
func (r *Renderer) drawVerticalTextDecoration(layer *PaintLayer, text string, advances []float64, box *layout.Box, fontID int32, ascent float64) {
	if len(layer.AppliedTextDecorations) == 0 &&
		(layer.TextDecoration == css.TextDecorationNone || layer.TextDecoration == "") {
		return
	}

	metrics := r.dc.GetFontMetrics(fontID)
	descent := float64(metrics.Descent) / 64.0

	// The inline extent: the glyphs' upright advances plus letter-spacing,
	// as the paint loop steps them. This is the physical HEIGHT of the
	// vertical text column.
	inlineLen := uprightAdvanceExtent(advances, layer, true)

	// Determine which physical X direction is "under" (block-end) based on
	// the writing mode. For vertical-rl, under = left; for vertical-lr, under = right.
	// Since louis14 represents vertical-lr with IsSidewaysRL=true for mixed text
	// orientation (engine.go:362-368), pure upright vertical (IsVerticalText &&
	// !IsSidewaysRL && !IsSidewaysLR) does not distinguish rl vs lr at paint time.
	// Default: blockUnderLeft (vertical-rl convention). This is the conventional
	// "under" for upright CJK text which is the primary use case for IsVerticalText.
	dir := blockUnderLeft

	info := newVerticalTextDecorationInfo(box, inlineLen, layer.FontSize, ascent, descent, 0, dir)

	if len(layer.AppliedTextDecorations) > 0 {
		// CSS Text Decor 4 §1.1.5 text-decoration-skip-ink for the upright
		// vertical paint path. Same Ahem-only gate as the horizontal path
		// (see drawTextDecoration for the rationale: louis14 lacks per-
		// glyph ink-bbox data, so advance-as-ink is only safe for Ahem).
		var glyphs []glyphExtent
		if layer.TextDecorationSkipInk == css.TextDecorationSkipInkAuto && layer.FontAhem {
			glyphs = buildGlyphExtentsVertical(text, layer.FontSize, box.Y, layer.LetterSpacing)
		}
		for _, td := range layer.AppliedTextDecorations {
			r.drawOneAppliedVerticalDecoration(td, info, glyphs)
		}
		return
	}

	// Legacy single-decoration fallback.
	thickness := math.Max(1.0, math.Round(layer.TextDecorationThickness))
	r.setColor(layer.TextDecorationColor)

	var rectLeft float64
	switch layer.TextDecoration {
	case css.TextDecorationUnderline:
		perpX := info.computeUnderlinePerpX(css.AppliedTextDecoration{
			UnderlineOffset: layer.TextUnderlineOffset,
		})
		rectLeft = vertDecorationRectLeft(perpX, thickness, dir)
	case css.TextDecorationOverline:
		perpX := info.computeOverlinePerpX()
		// Overline is on block-start = opposite side from underDir.
		overDir := blockUnderRight
		if dir == blockUnderRight {
			overDir = blockUnderLeft
		}
		rectLeft = vertDecorationRectLeft(perpX, thickness, overDir)
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
// derived from textDecorationInfo with axis=decorationAxisVertical.
//
// `glyphs` carries the per-glyph Y-extents along the inline axis used by CSS
// Text Decor 4 §1.1.5 skip-ink. Nil/empty disables skip-ink. Skip-ink does
// NOT apply to line-through per spec.
func (r *Renderer) drawOneAppliedVerticalDecoration(td css.AppliedTextDecoration, info textDecorationInfo, glyphs []glyphExtent) {
	if td.Lines.IsNone() {
		return
	}
	thickness := info.computeThickness(td)
	r.setColor(td.Color)

	lineStart := info.box.Y
	inlineLen := info.width
	lineEnd := lineStart + inlineLen

	// Glyph perpendicular (X) extent: the upright vertical glyph cell
	// spans the block-axis width (box.Width). The skip-ink gate requires
	// the decoration rect [rectLeft, rectLeft+thickness] to overlap
	// [box.X, box.X + box.Width] before any glyph extents are removed —
	// mirroring the horizontal path's lineY-vs-em-square gate so an
	// out-of-glyph underline (large positive offset) still paints
	// uninterrupted.
	glyphPerpL := info.box.X
	glyphPerpR := info.box.X + info.box.Width

	paintVertSegment := func(rectLeft float64, skipInk bool) {
		segments := []inkSegment{{Start: lineStart, End: lineEnd}}
		rectRight := rectLeft + thickness
		linePerpOverlapsGlyph := rectRight > glyphPerpL && rectLeft < glyphPerpR
		if skipInk && len(glyphs) > 0 && linePerpOverlapsGlyph {
			segments = skipInkSegments(lineStart, lineEnd, thickness, glyphs)
		}
		for _, seg := range segments {
			r.dc.DrawRectangle(rectLeft, seg.Start, thickness, seg.End-seg.Start)
			r.dc.Fill()
		}
	}

	if td.Lines.Has(css.TextDecorationLineUnderline) {
		perpX := info.computeUnderlinePerpX(td)
		rectLeft := vertDecorationRectLeft(perpX, thickness, info.underDir)
		paintVertSegment(rectLeft, true)
	}
	if td.Lines.Has(css.TextDecorationLineOverline) {
		perpX := info.computeOverlinePerpX()
		// Overline is on block-start = opposite side from underDir.
		overDir := blockUnderRight
		if info.underDir == blockUnderRight {
			overDir = blockUnderLeft
		}
		rectLeft := vertDecorationRectLeft(perpX, thickness, overDir)
		paintVertSegment(rectLeft, true)
	}
	if td.Lines.Has(css.TextDecorationLineLineThrough) {
		perpX := info.computeLineThroughPerpX()
		rectLeft := perpX - thickness/2
		// Line-through is NOT subject to skip-ink (CSS Text Decor 4 §1.1.5).
		paintVertSegment(rectLeft, false)
	}
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

// hasOverflowClipping reports whether the box clips its overflow along
// either axis (overflow or paint containment), as layout decided it.
// Mirrors LayoutObject::ShouldClipOverflowAlongEitherAxis
// (layout_object.h:2062-2065 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func hasOverflowClipping(box *layout.Box) bool {
	return box.OverflowClipAxes != layout.NoOverflowClip
}

// isContainedByOverflow returns true if the child is clipped by the parent's
// overflow/containment and should NOT escape to the ancestor stacking context.
func isContainedByOverflow(child, parent *layout.Box) bool {
	if parent.Style == nil {
		return false
	}
	// CSS Containment: layout and paint containment contain all positioned
	// descendants (absolute and fixed), acting as a containing block.
	if parent.Style.ShouldApplyLayoutContainment() || parent.Style.ShouldApplyPaintContainment() {
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
	childDC.SetColor(premultipliedRGBA(shadow.Color))

	lsx, lsy := sx-minX, sy-minY
	lbx, lby := bx-minX, by-minY

	// Fill the shadow shape, then clip out the border box
	// (GraphicsContext::ClipOut(border rrect) in BoxPainterBase::
	// PaintNormalBoxShadow, box_painter_base.cc @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Clearing every pixel the
	// border box touches gives the same pixel partition as the border
	// painter's clip-out (paintClippedOut): the box's own background and
	// borders own their rounded edge pixels.
	if !shadowRadii.IsZero() {
		buildRoundedRectPathOnDC(childDC, lsx, lsy, sw, sh, shadowRadii)
	} else {
		childDC.DrawRectangle(lsx, lsy, sw, sh)
	}
	childDC.Fill()
	r.clearHoleFromBuffer(buf, lbx, lby, bw, bh, borderRadii)

	// Apply box blur if needed.
	if shadow.Blur > 0 {
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))
		boxBlur(buf, int(math.Round(sigma)))

		// Re-clip the border box out of the *blurred* buffer. The blur spreads
		// shadow back across the cleared boundary into the border box by a few
		// pixels; without this an outset shadow paints inside the border box,
		// visible whenever the element background is transparent (e.g. a
		// backdrop-filter element). Mirrors Blink's
		// GraphicsContext::ClipOut(border_box) in
		// BoxPainterBase::PaintNormalBoxShadow (CSS Backgrounds 3 §7.1).
		r.clearHoleFromBuffer(buf, lbx, lby, bw, bh, borderRadii)
	}

	r.dc.DrawImage(buf, int(math.Round(minX)), int(math.Round(minY)))
}

// clearHoleFromBuffer clears (toward transparent) the pixels of buf that fall
// inside the given rectangle, optionally with rounded corners. It rasterizes
// the shape to an alpha mask using the draw context's rasterizer — so rounded
// corners match other path rendering exactly — then multiplies each covered
// buffer pixel toward zero by the mask's inverse coverage. Shared by the outset
// shadow's hole clear, its post-blur re-clip, and the inset shadow's inner-hole
// clear. Coordinates are buffer-local.
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
	// sides, and this helper runs twice per outset shadow (pre- and post-blur),
	// so walking the transparent margin each time is pure waste.
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
	// below, which is correct for inset shadows: their inner boundary is soft,
	// so — unlike the outset case — no post-blur re-clip is applied here.
	r.clearHoleFromBuffer(buf, ix-bx, iy-by, iw, ih, innerRadii)

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
