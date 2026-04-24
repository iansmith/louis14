package text

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"mazarin/textshape"
)

// FontFetcher fetches font data from a URL.
type FontFetcher func(url string) ([]byte, error)

// fontEntry stores a cached font file path for a specific family/weight/style combo.
type fontEntry struct {
	Family   string
	Weight   string
	Style    string
	FilePath string // Cached file path on disk
}

// FontRegistry manages fetched web fonts, caching them to disk for reuse.
type FontRegistry struct {
	mu      sync.Mutex
	entries []fontEntry
	cacheDir string
}

// NewFontRegistry creates a new FontRegistry that caches fonts in a temp directory.
func NewFontRegistry() *FontRegistry {
	dir, _ := os.MkdirTemp("", "louis14-fonts-*")
	return &FontRegistry{cacheDir: dir}
}

// RegisterFontFace fetches a font from srcURL and registers it for the given family/weight/style.
// The fetcher function is used to download the font data.
// Returns the cached file path on success.
func (fr *FontRegistry) RegisterFontFace(family, srcURL, format, weight, style string, fetcher FontFetcher) (string, error) {
	if fetcher == nil {
		return "", fmt.Errorf("no font fetcher available")
	}

	data, err := fetcher(srcURL)
	if err != nil {
		return "", fmt.Errorf("fetching font %s: %w", srcURL, err)
	}

	// Decompress WOFF1 if needed
	if format == "woff" || (format == "" && isWOFF1(data)) {
		decompressed, err := decompressWOFF1(data)
		if err != nil {
			return "", fmt.Errorf("decompressing WOFF1: %w", err)
		}
		data = decompressed
	}

	// Cache to disk using a family+variant filename so FontPathToFamilyVariant
	// can recover the logical (family, variant) pair from the path. A hashed
	// basename would strand the file outside DirectGlyphProvider's resolver
	// (it can't reverse-parse the hash back to a family), which silently drops
	// all text drawn in the @font-face family.
	variant := textshape.BoolsToVariant(normalizeWeight(weight) == "bold", normalizeStyle(style) == "italic")
	fileName := fmt.Sprintf("%s-%s.ttf", sanitizeFamily(family), textshape.VariantToStyle(variant))
	filePath := filepath.Join(fr.cacheDir, fileName)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("caching font: %w", err)
	}

	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.entries = append(fr.entries, fontEntry{
		Family:   strings.ToLower(family),
		Weight:   normalizeWeight(weight),
		Style:    normalizeStyle(style),
		FilePath: filePath,
	})

	return filePath, nil
}

// Lookup returns the cached file path for a font matching the given family, weight, and style.
// Returns "" if no match is found.
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

	// Exact match first
	for _, e := range fr.entries {
		if e.Family == lowerFamily && e.Weight == targetWeight && e.Style == targetStyle {
			return e.FilePath
		}
	}

	// Fallback: match family only (ignore weight/style)
	for _, e := range fr.entries {
		if e.Family == lowerFamily {
			return e.FilePath
		}
	}

	return ""
}

// sanitizeFamily returns a filesystem-safe form of a CSS font-family name.
// Any rune outside [A-Za-z0-9] is replaced with '_' so that FontPathToFamilyVariant
// can still split the resulting "<family>-<variant>.ttf" basename on the final
// '-'. An empty or fully-stripped family collapses to "font".
func sanitizeFamily(family string) string {
	var b strings.Builder
	b.Grow(len(family))
	for _, r := range family {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" || strings.Trim(s, "_") == "" {
		return "font"
	}
	return s
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
