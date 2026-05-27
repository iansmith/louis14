package text

import (
	"fmt"
	"strings"
	"sync"

	"mazarin/textshape"
)

// FontFetcher fetches font data from a URL.
type FontFetcher func(url string) ([]byte, error)

// MetricsOverride holds CSS Fonts 4 §6.1-§6.3 metric overrides for a
// @font-face rule. nil fields mean "normal" (use the font's native metric).
// Non-nil fields store the override ratio (percent / 100): 100% → 1.0.
// Mirrors Blink's FontMetricsOverride struct in
// platform/fonts/font_metrics_override.h @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
type MetricsOverride struct {
	Ascent  *float64 // ascent-override ratio, or nil for native
	Descent *float64 // descent-override ratio, or nil for native
	LineGap *float64 // line-gap-override ratio, or nil for native
}

// bufferEntry stores a fetched @font-face buffer keyed by (family, weight, style).
// Lookup matches against family/weight/style; the buffer is registered with the
// underlying [textshape.GlyphProvider] via [FontRegistry.ApplyTo] so layout-time
// and render-time text shaping can resolve the font without filesystem I/O.
type bufferEntry struct {
	Family  string
	Weight  string
	Style   string
	Variant int32
	Data    []byte
}

// FontRegistry holds fetched @font-face font buffers and applies them to a
// [textshape.TextLayout] (which forwards to its underlying GlyphProvider via
// [textshape.GlyphProvider.RegisterBuffer]).
//
// No filesystem state. Replaces the prior temp-file caching scheme: the parsed
// buffer is the source of truth, and [Lookup] returns a synthetic path (just
// `<family>-<Variant>.ttf`) so existing path-based call sites (FontPathToFamilyVariant,
// openFont) round-trip cleanly. The synthetic path is never opened from disk —
// the GlyphProvider's registered map intercepts the family+variant lookup first.
//
// `declaredFamilies` records families that were named in @font-face rules even
// when the actual fetch failed. This lets the font matching algorithm
// distinguish "family was declared but unloaded" (a real face that's
// unavailable — synthesis is forbidden when font-synthesis-* is none) from
// "family was never declared" (system fallback — natural face selection).
type FontRegistry struct {
	mu               sync.Mutex
	entries          []bufferEntry
	declaredFamilies map[string]struct{}
	// overridesByPath maps a synthetic font path (returned by RegisterFontFace)
	// to its metric overrides. Keyed by path so the existing fontIDCache —
	// which is also keyed by (path, size) — is completely unaffected; the
	// override layer sits above openFont, not inside it.
	overridesByPath map[string]MetricsOverride
	// overridesByFamily maps a lowercased family name to metric overrides for
	// the fallback case where the font URL fetch failed but a local/system font
	// is used. MetricsOverrideForPath falls back to this map when no path match
	// is found, after extracting the family from the font path.
	overridesByFamily map[string]MetricsOverride
}

// NewFontRegistry creates a new FontRegistry.
func NewFontRegistry() *FontRegistry {
	return &FontRegistry{}
}

// RegisterFontFace fetches a font from srcURL, decompresses WOFF1 if needed,
// and stores the resulting TTF/OTF buffer under (family, weight, style).
// Returns a synthetic path that round-trips through FontPathToFamilyVariant
// back to (family, variant).
func (fr *FontRegistry) RegisterFontFace(family, srcURL, format, weight, style string, fetcher FontFetcher) (string, error) {
	if fetcher == nil {
		return "", fmt.Errorf("no font fetcher available")
	}

	// Record the declared family before attempting the fetch. Even if the
	// fetch fails, the family was named by a @font-face rule and the font
	// matching algorithm must treat lookups for it as a webfont request, not
	// a system fallback. This is what gates font-synthesis-none correctly
	// when the declared face fails to load.
	fr.mu.Lock()
	if fr.declaredFamilies == nil {
		fr.declaredFamilies = make(map[string]struct{})
	}
	fr.declaredFamilies[strings.ToLower(family)] = struct{}{}
	fr.mu.Unlock()

	data, err := fetcher(srcURL)
	if err != nil {
		return "", fmt.Errorf("fetching font %s: %w", srcURL, err)
	}

	if format == "woff" || (format == "" && isWOFF1(data)) {
		decompressed, err := decompressWOFF1(data)
		if err != nil {
			return "", fmt.Errorf("decompressing WOFF1: %w", err)
		}
		data = decompressed
	}

	variant := textshape.BoolsToVariant(normalizeWeight(weight) == "bold", normalizeStyle(style) == "italic")

	fr.mu.Lock()
	fr.entries = append(fr.entries, bufferEntry{
		Family:  strings.ToLower(family),
		Weight:  normalizeWeight(weight),
		Style:   normalizeStyle(style),
		Variant: variant,
		Data:    data,
	})
	fr.mu.Unlock()

	// Use lowercase family throughout: bufferEntry.Family is already lowercased,
	// syntheticFontPath must match what Lookup() returns (which calls
	// syntheticFontPath(e.Family, ...) with the lowercased entry family), and
	// RegisterBuffer must use the same case so FontPathToFamilyVariant can
	// round-trip the path back to the provider's registered family. Consistent
	// lowercase prevents MetricsOverrideForPath misses and OpenFont lookup
	// failures when the caller's family name has a capital letter (e.g. "Ahem").
	lowerFamily := strings.ToLower(family)

	// Register the buffer with the shared GlyphProvider. Both layout-time
	// measurement (via getLayout) and paint-time rendering (via the
	// renderer's DrawContext, which reuses CurrentProvider) see the same
	// `registered` map, so a single registration is enough.
	if err := CurrentProvider().RegisterBuffer(lowerFamily, variant, data); err != nil {
		return "", fmt.Errorf("RegisterBuffer(%s): %w", lowerFamily, err)
	}

	return syntheticFontPath(lowerFamily, variant), nil
}

// SetFontFaceOverride stores metric overrides for a synthetic font path.
// Must be called after RegisterFontFace with the path it returned.
// A zero MetricsOverride (all-nil fields) is a no-op. Mirrors the
// MetricsOverriddenFontData factory path in Blink's SimpleFontData —
// applying overrides at font-data creation time rather than at metric-
// query time (simple_font_data.cc:253-257 @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func (fr *FontRegistry) SetFontFaceOverride(path string, mo MetricsOverride) {
	if mo.Ascent == nil && mo.Descent == nil && mo.LineGap == nil {
		return
	}
	fr.mu.Lock()
	if fr.overridesByPath == nil {
		fr.overridesByPath = make(map[string]MetricsOverride)
	}
	fr.overridesByPath[path] = mo
	fr.mu.Unlock()
}

// SetFontFamilyOverride stores metric overrides keyed by family name (lowercase).
// Used when RegisterFontFace fails (URL unavailable) but a local/system font
// serves the family — ensures the override is applied even when the fontPath
// is the system path rather than the synthetic web-font path.
func (fr *FontRegistry) SetFontFamilyOverride(family string, mo MetricsOverride) {
	if mo.Ascent == nil && mo.Descent == nil && mo.LineGap == nil {
		return
	}
	lf := strings.ToLower(family)
	fr.mu.Lock()
	if fr.overridesByFamily == nil {
		fr.overridesByFamily = make(map[string]MetricsOverride)
	}
	fr.overridesByFamily[lf] = mo
	fr.mu.Unlock()
}

// MetricsOverrideForPath returns the MetricsOverride stored for path, or a
// zero MetricsOverride (all-nil fields) if none was registered. Used by the
// Font*FromFont family in measure.go to intercept metric values above openFont.
//
// When no path-keyed override is found, falls back to a family-keyed lookup
// using the family name extracted from the path (e.g. "ahem" from
// "/.../fonts/Ahem.ttf"). This covers the case where the @font-face URL fetch
// failed and the system font is used instead of the synthetic web-font path.
func (fr *FontRegistry) MetricsOverrideForPath(path string) MetricsOverride {
	if fr == nil {
		return MetricsOverride{}
	}
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if mo, ok := fr.overridesByPath[path]; ok {
		return mo
	}
	// Fallback: look up by family extracted from the path. Supports the case
	// where the @font-face fetch failed and the system font path is used.
	if fr.overridesByFamily != nil {
		family, _ := FontPathToFamilyVariant(path)
		if mo, ok := fr.overridesByFamily[strings.ToLower(family)]; ok {
			return mo
		}
	}
	return MetricsOverride{}
}

// IsDeclared returns true if any @font-face rule named this family — even if
// the underlying fetch failed. Used by font matching to distinguish a missing
// webfont (synthesis-eligible) from a system family (synthesis-irrelevant).
func (fr *FontRegistry) IsDeclared(family string) bool {
	if fr == nil {
		return false
	}
	fr.mu.Lock()
	defer fr.mu.Unlock()
	_, ok := fr.declaredFamilies[strings.ToLower(family)]
	return ok
}

// Lookup returns the synthetic path for a font matching the given family,
// weight, and style. Returns "" if no match is found.
func (fr *FontRegistry) Lookup(family string, bold, italic bool) string {
	if fr == nil {
		return ""
	}
	fr.mu.Lock()
	defer fr.mu.Unlock()

	lowerFamily := strings.ToLower(family)
	targetWeight := "normal"
	if bold {
		targetWeight = "bold"
	}
	targetStyle := "normal"
	if italic {
		targetStyle = "italic"
	}

	for _, e := range fr.entries {
		if e.Family == lowerFamily && e.Weight == targetWeight && e.Style == targetStyle {
			return syntheticFontPath(e.Family, e.Variant)
		}
	}
	for _, e := range fr.entries {
		if e.Family == lowerFamily {
			return syntheticFontPath(e.Family, e.Variant)
		}
	}
	return ""
}

// ApplyTo registers every buffer in this registry with the given TextLayout.
// Idempotent — re-applying replaces any prior registration on the layout's
// underlying provider with the same (family, variant) key.
func (fr *FontRegistry) ApplyTo(tl textshape.TextLayout) error {
	if fr == nil || tl == nil {
		return nil
	}
	fr.mu.Lock()
	defer fr.mu.Unlock()
	for _, e := range fr.entries {
		if err := tl.RegisterBuffer(e.Family, e.Variant, e.Data); err != nil {
			return fmt.Errorf("RegisterBuffer(%s, %d): %w", e.Family, e.Variant, err)
		}
	}
	return nil
}

// syntheticFontPath returns a basename-style path that FontPathToFamilyVariant
// can round-trip back to (family, variant). The path is not a real filesystem
// location; the GlyphProvider's registered map resolves the family+variant
// without ever opening the file.
func syntheticFontPath(family string, variant int32) string {
	return family + "-" + textshape.VariantToStyle(variant) + ".ttf"
}

// normalizeWeight converts CSS font-weight values to "normal" or "bold".
func normalizeWeight(w string) string {
	w = strings.TrimSpace(w)
	switch w {
	case "bold", "700", "800", "900":
		return "bold"
	default:
		return "normal"
	}
}

// normalizeStyle converts CSS font-style values to "normal" or "italic".
func normalizeStyle(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "italic", "oblique":
		return "italic"
	default:
		return "normal"
	}
}
