package text

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/text/csstest"

	"mazarin/textshape"
)

// DirectionRun describes a contiguous byte range within a text string that
// must be shaped with a specific HarfBuzz direction. Used by
// ShapeAdvancesMixed to handle multi-bidi-level inline runs as a single
// shaping pass.
type DirectionRun struct {
	Start int  // inclusive byte offset in the input text
	End   int  // exclusive byte offset
	RTL   bool // false=LTR/auto-detect, true=RTL
}

// measureCache caches text advance widths keyed on "text\x00fontSize\x00fontPath".
var measureCache sync.Map

func measureCacheKey(text string, fontSize float64, fontPath string) string {
	return fmt.Sprintf("%s\x00%.4f\x00%s", text, fontSize, fontPath)
}

// sharedLayout is the package-level TextLayout used for all measurement.
// Set via SetTextLayout (called by the renderer after creating its DrawContext)
// or lazily initialized on first use. Safe for concurrent use.
//
// sharedProvider is the GlyphProvider underneath sharedLayout. Tracked
// separately so the renderer can reuse the same provider when constructing
// its DrawContext — keeping @font-face RegisterBuffer entries visible to
// both layout-time and paint-time text shaping.
var (
	sharedLayout   textshape.TextLayout
	sharedProvider textshape.GlyphProvider
	sharedLayoutMu sync.Mutex
)

func getLayout() textshape.TextLayout {
	sharedLayoutMu.Lock()
	defer sharedLayoutMu.Unlock()
	if sharedLayout == nil {
		sharedProvider = textshape.NewDirectGlyphProvider(defaultFontsDir())
		sharedLayout = textshape.NewTextLayoutWithProvider(sharedProvider)
		registerVendoredFonts(sharedProvider)
	}
	return sharedLayout
}

// CurrentProvider returns the shared GlyphProvider, lazy-initializing it on
// first call. Renderers reuse this provider for their DrawContext so that
// @font-face buffers registered via [FontRegistry.RegisterFontFace] are
// visible at both layout time and paint time.
func CurrentProvider() textshape.GlyphProvider {
	sharedLayoutMu.Lock()
	defer sharedLayoutMu.Unlock()
	if sharedProvider == nil {
		sharedProvider = textshape.NewDirectGlyphProvider(defaultFontsDir())
		sharedLayout = textshape.NewTextLayoutWithProvider(sharedProvider)
		registerVendoredFonts(sharedProvider)
	}
	return sharedProvider
}

// registerVendoredFonts hooks the in-tree vendored WPT test font sets into
// the shared GlyphProvider. Replaces the legacy "install fonts system-wide"
// dance for the WPT CSSTest reftests in pkg/visualtest. Wired into the
// lazy-init paths so the registration happens exactly once, before any
// caller observes the provider — and after the provider exists so
// RegisterBuffer has somewhere to write.
func registerVendoredFonts(p textshape.GlyphProvider) {
	csstest.Register(p)
}

// SetTextLayout sets the shared TextLayout used for all text measurement.
// Must be called by the renderer after creating its DrawContext so that
// layout measurement and paint rendering share the same shape cache.
// Resets per-layout caches; the underlying GlyphProvider (with its
// @font-face registered map) is preserved across the swap.
func SetTextLayout(tl textshape.TextLayout) {
	sharedLayoutMu.Lock()
	sharedLayout = tl
	sharedLayoutMu.Unlock()
	fontIDCacheMu.Lock()
	fontIDCache = make(map[fontIDKey]textshape.FontMetrics)
	fontIDCacheMu.Unlock()
	measureCache = sync.Map{}
}

// fontIDCache caches (fontPath, sizeInt32) → FontMetrics to avoid re-opening fonts.
var (
	fontIDCache   = make(map[fontIDKey]textshape.FontMetrics)
	fontIDCacheMu sync.Mutex
)

type fontIDKey struct {
	path string
	size int32
}

// FontPathToFamilyVariant extracts a logical family name and variant from a
// font file path. For example:
//
//	"/.../AtkinsonHyperlegible-Bold.ttf" → ("AtkinsonHyperlegible", VariantBold)
//	"/.../Ahem.ttf" → ("Ahem", VariantRegular)
//
// Exported so other louis14 packages (e.g. render) translate paths to
// family+variant before calling DrawContext.OpenFont — otherwise IPC-only
// providers like mazzy fontsvc receive an unparseable path string as the
// "family" argument.
func FontPathToFamilyVariant(fontPath string) (string, int32) {
	base := filepath.Base(fontPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	variant := int32(textshape.VariantRegular)
	family := name

	if idx := strings.LastIndex(name, "-"); idx > 0 {
		suffix := strings.ToLower(name[idx+1:])
		family = name[:idx]
		switch suffix {
		case "bold":
			variant = textshape.VariantBold
		case "italic", "oblique":
			// Latin Modern fonts use "oblique" instead of "italic"
			// (e.g. lmsans10-oblique.otf is the italic variant).
			variant = textshape.VariantItalic
		case "bolditalic", "boldoblique":
			variant = textshape.VariantBoldItalic
		case "light":
			variant = textshape.VariantLight
		case "condensed":
			variant = textshape.VariantCondensed
		case "regular":
			variant = textshape.VariantRegular
		default:
			// Unknown suffix — treat entire name as family.
			family = name
		}
	}
	// Note: previously absolute paths were passed through as the family
	// argument so DirectGlyphProvider could resolve them via its
	// filepath.IsAbs() fallback. That coupled louis14 to filesystem-based
	// providers and broke IPC providers (e.g. mazzy fontsvc) which only
	// understand logical family names. Both providers now receive the
	// basename-derived family + variant. @font-face web fonts with
	// hash-style filenames lose the family-name hint here; provider-
	// specific registration (e.g. RegisterFontFace pushing the file into
	// the provider's index by basename) is the right fix for that case.
	return family, variant
}

// openFont returns the FontMetrics for the given path+size, opening it if needed.
// Returns zero FontMetrics with FontID=-1 on error.
func openFont(fontPath string, fontSize float64) textshape.FontMetrics {
	size := int32(math.Round(fontSize))
	key := fontIDKey{path: fontPath, size: size}
	fontIDCacheMu.Lock()
	defer fontIDCacheMu.Unlock()
	if m, ok := fontIDCache[key]; ok {
		return m
	}
	family, variant := FontPathToFamilyVariant(fontPath)
	metrics, err := getLayout().OpenFont(textshape.OpenFontRequest{
		Family:  family,
		Variant: variant,
		Size:    size,
	})
	if err != nil {
		return textshape.FontMetrics{FontID: -1}
	}
	fontIDCache[key] = metrics
	return metrics
}

// FontConfig holds paths to font files used for text measurement and rendering.
type FontConfig struct {
	Regular    string
	Bold       string
	Italic     string
	BoldItalic string
	Monospace  string
	MonoBold   string
	Ahem       string        // Special test font where all glyphs are 1em x 1em squares
	Registry   *FontRegistry // Optional web font registry for @font-face fonts
}

// DefaultFontsDir returns the fonts directory relative to this source file.
func DefaultFontsDir() string {
	return defaultFontsDir()
}

func defaultFontsDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "fonts")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "fonts")
}

// DefaultFontConfig returns a FontConfig using the bundled Atkinson Hyperlegible fonts.
func DefaultFontConfig() FontConfig {
	dir := defaultFontsDir()
	return FontConfig{
		Regular:    filepath.Join(dir, "AtkinsonHyperlegible-Regular.ttf"),
		Bold:       filepath.Join(dir, "AtkinsonHyperlegible-Bold.ttf"),
		Italic:     filepath.Join(dir, "AtkinsonHyperlegible-Italic.ttf"),
		BoldItalic: filepath.Join(dir, "AtkinsonHyperlegible-BoldItalic.ttf"),
		Monospace:  filepath.Join(dir, "AtkinsonHyperlegibleMono-Regular.otf"),
		MonoBold:   filepath.Join(dir, "AtkinsonHyperlegibleMono-Bold.otf"),
		Ahem:       filepath.Join(dir, "Ahem.ttf"),
	}
}

// FontPath returns the font path for the given style combination.
func (fc FontConfig) FontPath(bold, italic, mono, ahem bool) string {
	if ahem && fc.Ahem != "" {
		return fc.Ahem
	}
	if mono {
		if bold && fc.MonoBold != "" {
			return fc.MonoBold
		}
		if fc.Monospace != "" {
			return fc.Monospace
		}
	}
	if bold && italic && fc.BoldItalic != "" {
		return fc.BoldItalic
	}
	if bold {
		return fc.Bold
	}
	if italic && fc.Italic != "" {
		return fc.Italic
	}
	return fc.Regular
}

// FontPathForFamily returns the font path for a CSS font-family string.
//
// CSS Fonts 3 §5.4: when matching the declared family list fails completely
// (no name in the list resolves to a real face), the UA must fall back to the
// *initial value* of font-family — not to whatever font happens to be the
// FontConfig's default. Louis14's UA initial value is "serif" (set on the root
// in pkg/css/cascade.go applyUserAgentStyles), which maps to Liberation Serif.
// Mirroring Blink: third_party/blink/renderer/platform/fonts/font_selector.cc
// final fallback dispatches to the platform default ("Times New Roman"/serif)
// rather than to an arbitrary registered face.
//
// Exceptions:
//   - When the declared family is the Ahem test font (caller signals via
//     ahem=true, set by [css.Style.IsAhemFamily]), the WPT test suite expects
//     Ahem itself — route to fc.Ahem via fc.FontPath.
//   - Same for the "monospace" generic (mono=true via
//     [css.Style.IsMonospaceFamily]) — route to fc.Monospace.
//
// fc.FontPath is also the final safety net for non-CSS callers (bare
// text.MeasureText with no FontConfig serif binding).
func (fc FontConfig) FontPathForFamily(family string, bold, italic, mono, ahem bool) string {
	families := parseFontFamilyList(family)
	for _, fam := range families {
		if fc.Registry != nil {
			if path := fc.Registry.Lookup(fam, bold, italic); path != "" {
				return path
			}
		}
		if path := fc.resolveBuiltinFamily(fam, bold, italic); path != "" {
			return path
		}
	}
	// Matching failed (either nothing in the list resolved, or the list was
	// effectively empty after quote/whitespace stripping). Per spec, fall
	// back to the UA initial value (serif). Skip for ahem/mono so those
	// test-font / generic-family requests stay routed to fc.Ahem /
	// fc.Monospace via fc.FontPath.
	if !ahem && !mono {
		if path := fc.resolveBuiltinFamily("serif", bold, italic); path != "" {
			return path
		}
	}
	return fc.FontPath(bold, italic, mono, ahem)
}

func parseFontFamilyList(raw string) []string {
	var families []string
	for _, part := range strings.Split(raw, ",") {
		fam := strings.TrimSpace(part)
		if len(fam) >= 2 && ((fam[0] == '"' && fam[len(fam)-1] == '"') || (fam[0] == '\'' && fam[len(fam)-1] == '\'')) {
			fam = fam[1 : len(fam)-1]
		}
		fam = strings.TrimSpace(fam)
		if fam != "" {
			families = append(families, fam)
		}
	}
	return families
}

func (fc FontConfig) resolveBuiltinFamily(family string, bold, italic bool) string {
	dir := defaultFontsDir()
	switch strings.ToLower(family) {
	case "helvetica", "helvetica neue", "arial",
		"liberation sans", "nimbus sans", "sans-serif":
		return latinModernSansPath(dir, bold, italic)
	case "latin modern roman", "computer modern":
		return latinModernRomanPath(dir, bold, italic)
	case "times", "times new roman", "liberation serif", "serif":
		return liberationSerifPath(dir, bold, italic)
	case "courier", "courier new", "liberation mono", "monospace":
		return atkinsonMonoPath(dir, bold, italic)
	}
	// CSSTest vendored faces (pkg/text/csstest). Each registered family is
	// matched case-insensitively per CSS Fonts 3 §3.1 ("Font family names are
	// case-insensitive"). The synthetic path's basename, after
	// FontPathToFamilyVariant strips the extension, round-trips back to the
	// registered family string (the name has no `-` so the suffix-based
	// variant parser treats the whole basename as the family — a "Bold"
	// suffix inside a CSS family name must stay part of the family).
	for _, name := range csstest.Names() {
		if strings.EqualFold(family, name) {
			return filepath.Join(dir, name+".ttf")
		}
	}
	return ""
}

func latinModernSansPath(dir string, bold, italic bool) string {
	switch {
	case bold && italic:
		return filepath.Join(dir, "lmsans10-boldoblique.otf")
	case bold:
		return filepath.Join(dir, "lmsans10-bold.otf")
	case italic:
		return filepath.Join(dir, "lmsans10-oblique.otf")
	default:
		return filepath.Join(dir, "lmsans10-regular.otf")
	}
}

func latinModernRomanPath(dir string, bold, italic bool) string {
	switch {
	case bold && italic:
		return filepath.Join(dir, "lmroman10-bolditalic.otf")
	case bold:
		return filepath.Join(dir, "lmroman10-bold.otf")
	case italic:
		return filepath.Join(dir, "lmroman10-italic.otf")
	default:
		return filepath.Join(dir, "lmroman10-regular.otf")
	}
}

func liberationSerifPath(dir string, bold, italic bool) string {
	switch {
	case bold && italic:
		return filepath.Join(dir, "LiberationSerif-BoldItalic.ttf")
	case bold:
		return filepath.Join(dir, "LiberationSerif-Bold.ttf")
	case italic:
		return filepath.Join(dir, "LiberationSerif-Italic.ttf")
	default:
		return filepath.Join(dir, "LiberationSerif-Regular.ttf")
	}
}

func atkinsonMonoPath(dir string, bold, italic bool) string {
	switch {
	case bold && italic:
		return filepath.Join(dir, "AtkinsonHyperlegibleMono-BoldItalic.otf")
	case bold:
		return filepath.Join(dir, "AtkinsonHyperlegibleMono-Bold.otf")
	case italic:
		return filepath.Join(dir, "AtkinsonHyperlegibleMono-RegularItalic.otf")
	default:
		return filepath.Join(dir, "AtkinsonHyperlegibleMono-Regular.otf")
	}
}

var DefaultFontPath = DefaultFontConfig().Regular
var BoldFontPath = DefaultFontConfig().Bold

// measureWidth returns the advance width of text for a given font path+size,
// using the shared TextLayout (HarfBuzz shaping) with a sync.Map cache.
func measureWidth(text string, fontSize float64, fontPath string) float64 {
	// font-size: 0 — all text has zero advance width.  This is an author
	// technique to collapse whitespace between inline-block elements.
	if fontSize == 0 {
		return 0
	}
	key := measureCacheKey(text, fontSize, fontPath)
	if v, ok := measureCache.Load(key); ok {
		return v.(float64)
	}
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		w := math.Round(float64(len(text)) * fontSize * 0.6)
		measureCache.Store(key, w)
		return w
	}
	adv, err := getLayout().MeasureText(textshape.ShapingParams{
		Text:   text,
		FontID: m.FontID,
	})
	var w float64
	if err != nil {
		w = math.Floor(float64(len(text)) * fontSize * 0.6)
	} else {
		// Return the exact sub-pixel advance (no rounding).
		// Layout accumulates these as float64, so adjacent text boxes are
		// positioned at exact fractional-pixel boundaries. DrawText then
		// decomposes each box's X into floor(X) + frac(X)*64 as StartPenX,
		// producing glyph positions identical to a single combined run.
		w = float64(adv) / 64.0
		if w < 1 && len(text) > 0 {
			w = 1
		}
	}
	measureCache.Store(key, w)
	return w
}

// MeasureText measures the width and height of text with the given font and
// returns them as LayoutUnit values, snapped at this boundary using CEIL —
// the Blink ShapeResult::SnappedWidth analog (shape_result.h:
// `LayoutUnit::FromFloatCeil(width_)`). CEIL is the conservative direction
// for line-fit decisions: over-reporting is safe, under-reporting overflows
// the line box.
//
// Internal float64 accumulation is preserved (`measureWidth` keeps the
// HarfBuzz int32 26.6 / 64.0 path); the snap happens once at the public API
// boundary via `shapeWidthSnap` (= `layoutunit.FromFloat64Ceil`). Callers
// holding a float64 accumulator bridge with `.Float64()` (round-trips
// losslessly for exact-1/64-px values, which is what HarfBuzz produces, and
// for the math.Round-produced integer height).
func MeasureText(text string, fontSize float64, fontPath string) (width, height layoutunit.LayoutUnit) {
	w := measureWidth(text, fontSize, fontPath)
	width = shapeWidthSnap(w)
	m := openFont(fontPath, fontSize)
	if m.FontID >= 0 && m.Height > 0 {
		height = layoutunit.FromFloat64Ceil(math.Round(float64(m.Height) / 64.0))
	} else {
		height = layoutunit.FromFloat64Ceil(math.Round(fontSize * 1.2))
	}
	return
}

// MeasureTextDefault measures text using the default font. Float64 wrapper
// over MeasureText for legacy callers (louis13/); the snap discipline is
// preserved internally.
func MeasureTextDefault(text string, fontSize float64) (width, height float64) {
	w, h := MeasureText(text, fontSize, DefaultFontPath)
	return w.Float64(), h.Float64()
}

// MeasureTextWithWeight measures text using the specified font weight.
// Float64 wrapper over MeasureText.
func MeasureTextWithWeight(text string, fontSize float64, bold bool) (width, height float64) {
	fontPath := DefaultFontPath
	if bold {
		fontPath = BoldFontPath
	}
	w, h := MeasureText(text, fontSize, fontPath)
	return w.Float64(), h.Float64()
}

// MeasureTextWithStyle measures text with the full style combination. Float64
// wrapper over MeasureText.
func MeasureTextWithStyle(text string, fontSize float64, bold, italic, mono, ahem bool) (width, height float64) {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	w, h := MeasureText(text, fontSize, fontPath)
	return w.Float64(), h.Float64()
}

// MeasureTextVertical returns the inline advance of text in a vertical writing
// mode. For upright text (the default for Ahem and CJK), each glyph advances
// by fontSize in the inline (vertical) direction. For sideways text, the
// inline advance equals the horizontal advance (rotated glyphs keep their
// horizontal width as the inline advance).
//
// CSS Writing Modes §5.1: text-orientation determines whether glyphs are
// upright or sideways. For now, this treats all characters as upright,
// which is correct for Ahem (1em × 1em squares) and CJK text.
func MeasureTextVertical(text string, fontSize float64, bold, italic, mono, ahem bool) (inlineAdvance, blockAdvance float64) {
	runeCount := utf8.RuneCountInString(text)
	// Upright: each glyph advances by fontSize in the inline direction.
	inlineAdvance = float64(runeCount) * fontSize
	// Block advance = font height (line thickness in the block direction).
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	m := openFont(fontPath, fontSize)
	if m.FontID >= 0 && m.Height > 0 {
		blockAdvance = math.Round(float64(m.Height) / 64.0)
	} else {
		blockAdvance = math.Round(fontSize * 1.2)
	}
	return
}

// FontAscent returns the font ascent in pixels for the given style.
func FontAscent(fontSize float64, bold, italic, mono, ahem bool) float64 {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	return FontAscentFromFont(fontSize, fontPath)
}

// FontAscentFromFont returns the font ascent in pixels for the given font path,
// rounded to the nearest integer. Matches Blink's int_ascent_ (lroundf in
// SimpleFontData::PlatformInit) so the strut/leading math produces integer-aligned
// line-box heights and stacked block containers don't accumulate sub-pixel drift.
func FontAscentFromFont(fontSize float64, fontPath string) float64 {
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return math.Round(fontSize * 0.8)
	}
	return math.Round(float64(m.Ascent) / 64.0)
}

// FontDescentFromFont returns the font descent in pixels for the given font path,
// rounded to the nearest integer (positive value). See FontAscentFromFont for
// the rationale on integer rounding.
func FontDescentFromFont(fontSize float64, fontPath string) float64 {
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return math.Round(fontSize * 0.2)
	}
	return math.Round(float64(m.Descent) / 64.0)
}

// FontTypoAscentFromFont returns the unrounded font typo ascent in pixels.
// Mirrors Blink's float-precision ascent (`FontMetrics::FloatAscent()` /
// `FixedAscent()` at
// `third_party/blink/renderer/platform/fonts/font_metrics.h:117-133 @
// 574216cbb0c2b86a39c1d41ad85b2891a050b44c`) used by sub-pixel-precise
// layout paths such as `ComputeEmHeight` in
// `core/layout/inline/ruby_utils.cc:78-126`. Unlike `FontAscentFromFont`,
// this does NOT apply `math.Round` — for use where downstream layout
// math needs to agree with the renderer's unrounded `GetFontMetrics`
// path (e.g. ruby annotation positioning, where layout-vs-render
// rounding mismatch surfaces as a visible 1px gap; see LOU-161
// findings.md).
func FontTypoAscentFromFont(fontSize float64, fontPath string) float64 {
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return fontSize * 0.8
	}
	return float64(m.Ascent) / 64.0
}

// FontTypoDescentFromFont returns the unrounded font typo descent in pixels.
// See FontTypoAscentFromFont for the rationale.
func FontTypoDescentFromFont(fontSize float64, fontPath string) float64 {
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return fontSize * 0.2
	}
	return float64(m.Descent) / 64.0
}

// FontHeightFromFont returns the font's recommended line height in pixels for
// CSS line-height: normal. Returns the unrounded m.Height so line-height:normal
// matches the font's intrinsic spacing exactly; integer rounding is applied at
// the strut sites (via FontAscent/Descent) where it matters for line-box layout.
func FontHeightFromFont(fontSize float64, fontPath string) float64 {
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return fontSize * 1.2
	}
	return float64(m.Height) / 64.0
}

// ShapeAdvances shapes text with HarfBuzz and returns the cumulative advance
// through each byte offset as a Blink-parity ShapeCumulative pair (Start/End,
// floor/ceil snapped per shape_result.h SnappedStart/EndPositionForOffset).
// The Start and End slices each have length len(text)+1; index i holds the
// snapped position at which text[i:] begins within the shaped run. Index 0
// is always Zero; the final index equals the snapped MeasureText advance.
//
// Use ShapeCumulative.SnappedWidth(s, e) for the pair-of-positions read
// pattern that mirrors Blink's CachedWidth(start, end). Summing per-byte
// widths instead would accumulate up to N raw quanta over N clusters; the
// pair-of-positions form caps the rounding-drift error at 2 raw quanta total.
//
// This is used to recover per-item context-aware widths when multiple inline
// items share a shaping context (same font/style) — measure the concatenation
// once via this function, then slice by item byte-range to get each item's
// advance in context of its neighbors (including any cross-item kerning).
//
// Returns the zero ShapeCumulative and false on any shaping failure, so
// callers can fall back to standalone measurement. Clusters that don't map
// 1:1 to byte offsets (complex shaping, reordered glyphs, ligatures) are
// handled best-effort by attributing each glyph's advance to its cluster
// byte offset; offsets between cluster boundaries receive the most recent
// cluster's cumulative X.
func ShapeAdvances(textStr string, fontSize float64, fontPath string) (ShapeCumulative, bool) {
	cum, ok := shapeAdvancesFloat(textStr, fontSize, fontPath)
	if !ok {
		return ShapeCumulative{}, false
	}
	return shapeAdvancePairSnap(cum), true
}

// shapeAdvancesFloat computes the per-byte cumulative-position float64 slice
// before snapping. Internal helper: the public ShapeAdvances wraps this with
// the FLOOR/CEIL snap pair at the boundary.
func shapeAdvancesFloat(textStr string, fontSize float64, fontPath string) ([]float64, bool) {
	if textStr == "" {
		return []float64{0}, true
	}
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return nil, false
	}
	run, err := getLayout().ShapeText(textshape.ShapingParams{
		Text:   textStr,
		FontID: m.FontID,
	})
	if err != nil {
		return nil, false
	}

	cum := make([]float64, len(textStr)+1)
	// Fill with -1 as sentinel to detect missing cluster samples.
	for i := range cum {
		cum[i] = -1
	}
	cum[0] = 0
	var x int32
	for _, g := range run.Glyphs {
		c := int(g.Cluster)
		if c >= 0 && c <= len(textStr) && cum[c] < 0 {
			cum[c] = float64(x) / 64.0
		}
		x += g.XAdvance
	}
	cum[len(textStr)] = float64(x) / 64.0
	// Forward-fill missing cluster boundaries with the last known cumulative X.
	// Offsets that fall inside a multi-byte cluster or between ligature parts
	// inherit the cluster-start position, which is the correct approximation
	// for byte ranges that don't land on a cluster boundary.
	last := 0.0
	for i, v := range cum {
		if v < 0 {
			cum[i] = last
		} else {
			last = v
		}
	}
	return cum, true
}

// ShapeAdvancesMixed shapes a text string composed of multiple bidi-direction
// runs and returns a per-byte cumulative advance map indexed by LOGICAL byte
// position, packaged as a Blink-parity ShapeCumulative pair (Start/End,
// floor/ceil snapped). For each direction run, the corresponding sub-string is
// shaped with the right HarfBuzz direction (LTR auto-detect or explicit RTL);
// per-byte cumulative advances are derived from glyphs sorted by cluster
// ascending so the returned slices are monotonically non-decreasing in byte
// index, regardless of internal RTL cluster ordering.
//
// Mirrors Blink's HarfBuzzShaper which runs once over the whole inline text
// run (segmenting by direction internally) and lets per-item layout slice
// advances from the unified shape result. Compared to calling ShapeAdvances
// independently per item, this avoids dropping cross-item kerning within a
// same-direction segment AND keeps sub-pixel residue consistent across
// bidi-level boundaries — the F1 root cause the css-writing-modes
// bidi-embed/override-006 tests hit.
//
// Empty `runs` is treated as a single LTR run over the whole text (degenerate
// case equivalent to ShapeAdvances). Returns the zero ShapeCumulative and
// false on any shaping failure.
func ShapeAdvancesMixed(textStr string, runs []DirectionRun, fontSize float64, fontPath string) (ShapeCumulative, bool) {
	cum, ok := shapeAdvancesMixedFloat(textStr, runs, fontSize, fontPath)
	if !ok {
		return ShapeCumulative{}, false
	}
	return shapeAdvancePairSnap(cum), true
}

// shapeAdvancesMixedFloat computes the per-byte cumulative-position float64
// slice for the bidi-mixed case before snapping.
func shapeAdvancesMixedFloat(textStr string, runs []DirectionRun, fontSize float64, fontPath string) ([]float64, bool) {
	if textStr == "" {
		return []float64{0}, true
	}
	if len(runs) == 0 {
		return shapeAdvancesFloat(textStr, fontSize, fontPath)
	}
	m := openFont(fontPath, fontSize)
	if m.FontID < 0 {
		return nil, false
	}

	cum := make([]float64, len(textStr)+1)
	for i := range cum {
		cum[i] = -1
	}
	cum[0] = 0

	base := 0.0
	layout := getLayout()

	for _, r := range runs {
		if r.Start < 0 || r.End > len(textStr) || r.Start >= r.End {
			continue
		}
		subText := textStr[r.Start:r.End]

		params := textshape.ShapingParams{
			Text:   subText,
			FontID: m.FontID,
		}
		if r.RTL {
			params.Direction = textshape.RTL
		}

		result, err := layout.ShapeText(params)
		if err != nil {
			return nil, false
		}

		// Accumulate (cluster, advance) pairs and sort by cluster ascending so
		// we can produce a logical-order prefix sum. For RTL output, glyphs are
		// emitted in descending cluster order; the sort restores logical order.
		type clusterEntry struct {
			cluster int
			advance int32
		}
		entries := make([]clusterEntry, 0, len(result.Glyphs))
		var totalAdvance int32
		for _, g := range result.Glyphs {
			c := int(g.Cluster)
			if c < 0 || c > len(subText) {
				continue
			}
			entries = append(entries, clusterEntry{c, g.XAdvance})
			totalAdvance += g.XAdvance
		}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].cluster < entries[j].cluster })

		var prefixAdv int32
		for _, e := range entries {
			absByte := r.Start + e.cluster
			if absByte >= 0 && absByte <= len(textStr) && cum[absByte] < 0 {
				cum[absByte] = base + float64(prefixAdv)/64.0
			}
			prefixAdv += e.advance
		}
		// Always set cum[r.End] to the run's right edge.
		if r.End <= len(textStr) {
			cum[r.End] = base + float64(totalAdvance)/64.0
		}
		base += float64(totalAdvance) / 64.0
	}

	// Forward-fill any unset entries with the last known cumulative value.
	// Bytes between cluster boundaries (e.g. inside a ligature, or between
	// adjacent direction runs we already covered) inherit the previous
	// cluster-start position, matching ShapeAdvances' approximation.
	last := 0.0
	for i, v := range cum {
		if v < 0 {
			cum[i] = last
		} else {
			last = v
		}
	}
	return cum, true
}

// MeasureTextVerticalFromFont returns the inline advance of text in a vertical
// writing mode using the given font path. Each upright glyph advances by fontSize.
// Returns LayoutUnit values snapped via CEIL at this boundary — same Blink-
// parity ShapeResult::SnappedWidth discipline as MeasureText.
func MeasureTextVerticalFromFont(text string, fontSize float64, fontPath string) (inlineAdvance, blockAdvance layoutunit.LayoutUnit) {
	runeCount := utf8.RuneCountInString(text)
	inlineAdvance = layoutunit.FromFloat64Ceil(float64(runeCount) * fontSize)
	m := openFont(fontPath, fontSize)
	if m.FontID >= 0 && m.Height > 0 {
		blockAdvance = layoutunit.FromFloat64Ceil(math.Round(float64(m.Height) / 64.0))
	} else {
		blockAdvance = layoutunit.FromFloat64Ceil(math.Round(fontSize * 1.2))
	}
	return
}

// BreakTextAtCharacterBoundary splits text so the prefix fits within maxWidth.
func BreakTextAtCharacterBoundary(textStr string, fontSize float64, bold, italic, mono, ahem bool, maxWidth float64) (string, string) {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	runes := []rune(textStr)
	bestLen := 0
	for i := 1; i <= len(runes); i++ {
		prefix := string(runes[:i])
		w := measureWidth(prefix, fontSize, fontPath)
		if w > maxWidth {
			break
		}
		bestLen = i
	}
	if bestLen == 0 {
		return "", textStr
	}
	return string(runes[:bestLen]), string(runes[bestLen:])
}

// BreakTextIntoLines breaks text into lines that fit within maxWidth.
func BreakTextIntoLines(text string, fontSize float64, bold bool, maxWidth float64) []string {
	return BreakTextIntoLinesWithWrap(text, fontSize, bold, maxWidth, maxWidth)
}

// BreakTextIntoLinesWithWrap breaks text into lines with separate first/remaining widths.
func BreakTextIntoLinesWithWrap(text string, fontSize float64, bold bool, firstLineMax, remainingMax float64) []string {
	fontPath := DefaultFontPath
	if bold {
		fontPath = BoldFontPath
	}
	return breakLines(text, fontSize, fontPath, firstLineMax, remainingMax)
}

// BreakTextIntoLinesWithStyle breaks text with the full style combination.
func BreakTextIntoLinesWithStyle(text string, fontSize float64, bold, italic, mono, ahem bool, firstLineMax, remainingMax float64) []string {
	fontPath := DefaultFontConfig().FontPath(bold, italic, mono, ahem)
	return breakLines(text, fontSize, fontPath, firstLineMax, remainingMax)
}

func breakLines(text string, fontSize float64, fontPath string, firstLineMax, remainingMax float64) []string {
	if measureWidth(text, fontSize, fontPath) <= firstLineMax {
		return []string{text}
	}
	leadingSpace := ""
	if len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n') {
		leadingSpace = " "
	}
	words := splitIntoWords(text)
	if len(words) == 0 {
		return []string{text}
	}
	var lines []string
	currentLine := ""
	lineNum := 0
	for i, word := range words {
		if i == 0 && leadingSpace != "" {
			word = leadingSpace + word
		}
		testLine := currentLine
		if testLine != "" {
			testLine += " "
		}
		testLine += word
		maxWidth := remainingMax
		if lineNum == 0 {
			maxWidth = firstLineMax
		}
		if measureWidth(testLine, fontSize, fontPath) <= maxWidth {
			currentLine = testLine
		} else {
			if currentLine != "" {
				lines = append(lines, currentLine)
				lineNum++
			}
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	if len(lines) == 0 {
		return []string{text}
	}
	return lines
}

func splitIntoWords(text string) []string {
	var words []string
	currentWord := ""
	for _, ch := range text {
		if ch == ' ' || ch == '\t' || ch == '\n' {
			if currentWord != "" {
				words = append(words, currentWord)
				currentWord = ""
			}
		} else {
			currentWord += string(ch)
		}
	}
	if currentWord != "" {
		words = append(words, currentWord)
	}
	return words
}

// GetFirstWord returns the first word of the text.
func GetFirstWord(text string) string {
	words := splitIntoWords(text)
	if len(words) > 0 {
		return words[0]
	}
	return ""
}
