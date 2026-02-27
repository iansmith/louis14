package images

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// ImageCache caches loaded images
type ImageCache struct {
	cache map[string]image.Image
	mu    sync.RWMutex
}

// Global image cache
var globalCache = &ImageCache{
	cache: make(map[string]image.Image),
}

// IsDataURI returns true if the string is a data URI.
func IsDataURI(uri string) bool {
	return strings.HasPrefix(uri, "data:")
}

// LoadImageFromDataURI decodes a data URI and returns the embedded image.
// Format: data:[<mediatype>][;base64],<data>
func LoadImageFromDataURI(uri string) (image.Image, error) {
	if !strings.HasPrefix(uri, "data:") {
		return nil, fmt.Errorf("not a data URI")
	}

	// Split off "data:" prefix
	rest := uri[5:]

	// Find the comma separating metadata from data
	commaIdx := strings.Index(rest, ",")
	if commaIdx < 0 {
		return nil, fmt.Errorf("invalid data URI: no comma found")
	}

	meta := rest[:commaIdx]
	encoded := rest[commaIdx+1:]

	isBase64 := strings.HasSuffix(meta, ";base64")

	var data []byte
	if isBase64 {
		// URL-decode the base64 data first (handles %2F, %2B, etc.)
		if decoded, err := url.PathUnescape(encoded); err == nil {
			encoded = decoded
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("base64 decode error: %w", err)
		}
	} else {
		data = []byte(encoded)
	}

	return DecodeImageBytes(data)
}

// LoadImage loads an image from the filesystem or a data URI.
func LoadImage(path string) (image.Image, error) {
	// Handle data URIs
	if IsDataURI(path) {
		// Check cache first
		globalCache.mu.RLock()
		if img, ok := globalCache.cache[path]; ok {
			globalCache.mu.RUnlock()
			return img, nil
		}
		globalCache.mu.RUnlock()

		img, err := LoadImageFromDataURI(path)
		if err != nil {
			return nil, err
		}

		globalCache.mu.Lock()
		globalCache.cache[path] = img
		globalCache.mu.Unlock()

		return img, nil
	}

	// Check cache first
	globalCache.mu.RLock()
	if img, ok := globalCache.cache[path]; ok {
		globalCache.mu.RUnlock()
		return img, nil
	}
	globalCache.mu.RUnlock()

	// Load image from file
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	// Cache the image
	globalCache.mu.Lock()
	globalCache.cache[path] = img
	globalCache.mu.Unlock()

	return img, nil
}

// GetImageDimensions returns the width and height of an image
func GetImageDimensions(path string) (width, height int, err error) {
	img, err := LoadImage(path)
	if err != nil {
		return 0, 0, err
	}

	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy(), nil
}

// ImageFetcher is a function type that fetches raw bytes for an image URI.
// It is used to support network-based image loading without creating a
// dependency on the resource package.
type ImageFetcher func(uri string) ([]byte, error)

// DecodeImageBytes decodes an image from raw bytes.
func DecodeImageBytes(data []byte) (image.Image, error) {
	// Check for SVG content
	if isSVGData(data) {
		return rasterizeSVG(data, 0, 0)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image decode error: %w", err)
	}
	return img, nil
}

// isSVGData checks if the data looks like SVG content.
func isSVGData(data []byte) bool {
	n := len(data)
	if n > 256 {
		n = 256
	}
	s := strings.TrimSpace(string(data[:n]))
	return strings.HasPrefix(s, "<svg") || strings.HasPrefix(s, "<?xml") ||
		strings.Contains(s, "<svg")
}

// svgOpenTagRE matches the opening <svg ...> tag to extract root dimensions.
var svgOpenTagRE = regexp.MustCompile(`(?s)<svg\b([^>]*)>`)

// svgAttrDimRE matches a width or height attribute with an absolute value.
var svgAttrDimRE = regexp.MustCompile(`\b(width|height)="(\d+(?:\.\d+)?)(?:px)?"`)

// svgPctAttrRE matches width/height percentage attributes on any element.
var svgPctAttrRE = regexp.MustCompile(`\b(width|height)="(\d+(?:\.\d+)?)%"`)

// preprocessSVGPercentages resolves percentage width/height attributes in SVG data.
// oksvg doesn't handle width="100%" on shapes like <rect>. This replaces them
// with absolute pixel values computed from the root <svg> element's dimensions.
func preprocessSVGPercentages(data []byte) []byte {
	s := string(data)

	// Find the root <svg> opening tag and extract its width/height
	rootMatch := svgOpenTagRE.FindStringSubmatch(s)
	if rootMatch == nil {
		return data
	}
	rootAttrs := rootMatch[1]
	var svgW, svgH float64
	for _, m := range svgAttrDimRE.FindAllStringSubmatch(rootAttrs, -1) {
		if m[1] == "width" {
			svgW, _ = strconv.ParseFloat(m[2], 64)
		} else {
			svgH, _ = strconv.ParseFloat(m[2], 64)
		}
	}
	if svgW <= 0 || svgH <= 0 {
		return data
	}

	// Replace percentage width/height with resolved pixel values.
	// Only replace occurrences outside the root <svg> tag (skip index 0 to end of root tag).
	rootTagEnd := strings.Index(s, ">") + 1
	prefix := s[:rootTagEnd]
	body := s[rootTagEnd:]

	body = svgPctAttrRE.ReplaceAllStringFunc(body, func(match string) string {
		m := svgPctAttrRE.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		pct, _ := strconv.ParseFloat(m[2], 64)
		if m[1] == "width" {
			return fmt.Sprintf(`width="%.4g"`, pct/100.0*svgW)
		}
		return fmt.Sprintf(`height="%.4g"`, pct/100.0*svgH)
	})
	return []byte(prefix + body)
}

// rasterizeSVG renders SVG data to an image.RGBA using oksvg.
// If w/h are 0, the SVG's viewBox dimensions are used.
func rasterizeSVG(data []byte, w, h int) (image.Image, error) {
	// oksvg doesn't handle percentage width/height on shapes (e.g. width="100%").
	// Resolve them to absolute values before parsing.
	data = preprocessSVGPercentages(data)

	icon, err := oksvg.ReadIconStream(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("SVG parse error: %w", err)
	}

	svgW := int(icon.ViewBox.W)
	svgH := int(icon.ViewBox.H)
	if svgW <= 0 {
		svgW = 32
	}
	if svgH <= 0 {
		svgH = 32
	}
	if w <= 0 {
		w = svgW
	}
	if h <= 0 {
		h = svgH
	}

	icon.SetTarget(0, 0, float64(w), float64(h))
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	raster := rasterx.NewDasher(w, h, scanner)
	icon.Draw(raster, 1.0)
	return rgba, nil
}

// LoadImageWithFetcher loads an image using the provided fetcher.
// The fetcher is used for both network URIs and relative paths.
// Falls back to LoadImage for data URIs and when no fetcher is provided.
func LoadImageWithFetcher(path string, fetcher ImageFetcher) (image.Image, error) {
	// Data URIs are handled by LoadImage
	if IsDataURI(path) {
		return LoadImage(path)
	}

	// If no fetcher, use regular loading (only works for absolute paths)
	if fetcher == nil {
		return LoadImage(path)
	}

	// For absolute paths that exist on disk, try loading directly first
	if filepath.IsAbs(path) {
		if img, err := LoadImage(path); err == nil {
			return img, nil
		}
	}

	// Check cache first
	globalCache.mu.RLock()
	if img, ok := globalCache.cache[path]; ok {
		globalCache.mu.RUnlock()
		return img, nil
	}
	globalCache.mu.RUnlock()

	// Fetch via network
	data, err := fetcher(path)
	if err != nil {
		return nil, fmt.Errorf("fetching image %s: %w", path, err)
	}

	img, err := DecodeImageBytes(data)
	if err != nil {
		return nil, err
	}

	// Cache the image
	globalCache.mu.Lock()
	globalCache.cache[path] = img
	globalCache.mu.Unlock()

	return img, nil
}

// GetImageDimensionsWithFetcher returns the width and height of an image,
// using the provided fetcher for network URIs.
func GetImageDimensionsWithFetcher(path string, fetcher ImageFetcher) (width, height int, err error) {
	img, err := LoadImageWithFetcher(path, fetcher)
	if err != nil {
		return 0, 0, err
	}

	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy(), nil
}

// isNetworkURI returns true if the string looks like an HTTP/HTTPS URL.
func isNetworkURI(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// NewFilesystemFetcher creates an ImageFetcher that resolves relative paths
// against a base URL (typically the document's file path).
func NewFilesystemFetcher(baseURL string) ImageFetcher {
	return func(uri string) ([]byte, error) {
		// Don't resolve data URIs or absolute network URLs
		if IsDataURI(uri) || isNetworkURI(uri) {
			return nil, fmt.Errorf("filesystem fetcher only handles file paths")
		}

		// Resolve relative paths against base URL
		resolvedPath := uri
		if baseURL != "" && !filepath.IsAbs(uri) {
			baseDir := filepath.Dir(baseURL)
			resolvedPath = filepath.Join(baseDir, uri)
		}

		// Read the file
		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("reading file %s: %w", resolvedPath, err)
		}

		return data, nil
	}
}
