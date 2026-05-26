package text

import (
	"fmt"
	"strings"
	"sync"

	"mazarin/textshape"
)

// FontFetcher fetches font data from a URL.
type FontFetcher func(url string) ([]byte, error)

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

	// Register the buffer with the shared GlyphProvider. Both layout-time
	// measurement (via getLayout) and paint-time rendering (via the
	// renderer's DrawContext, which reuses CurrentProvider) see the same
	// `registered` map, so a single registration is enough.
	if err := CurrentProvider().RegisterBuffer(family, variant, data); err != nil {
		return "", fmt.Errorf("RegisterBuffer(%s): %w", family, err)
	}

	return syntheticFontPath(family, variant), nil
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
