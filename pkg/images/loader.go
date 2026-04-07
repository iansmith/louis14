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
		// Lenient URL-decode: handles valid percent-encoding (%3C → <, %25 → %)
		// while passing through invalid sequences (e.g. 100%' in SVG attributes).
		// url.PathUnescape rejects the entire string if any % is followed by
		// non-hex characters, which breaks SVGs like width='100%'.
		data = []byte(lenientPercentDecode(encoded))
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

// svgAttrDimRE matches a width or height attribute with an absolute value (single or double quotes).
var svgAttrDimRE = regexp.MustCompile(`\b(width|height)=["'](\d+(?:\.\d+)?)(?:px)?["']`)

// svgPctAttrRE matches width/height percentage attributes on any element (single or double quotes).
var svgPctAttrRE = regexp.MustCompile(`\b(width|height)=["'](\d+(?:\.\d+)?)%["']`)

// preprocessSVGPercentages resolves percentage width/height attributes in SVG data.
// oksvg doesn't handle width="100%" on shapes like <rect>. This replaces them
// with absolute values in SVG coordinate space.
func preprocessSVGPercentages(data []byte) []byte {
	s := string(data)

	// Find the root <svg> opening tag.
	rootMatch := svgOpenTagRE.FindStringSubmatch(s)
	if rootMatch == nil {
		return data
	}
	rootAttrs := rootMatch[1]

	// SVG child element percentages are relative to the viewBox coordinate system,
	// not to the CSS pixel dimensions. Extract viewBox dimensions first.
	var baseW, baseH float64
	vbRE := regexp.MustCompile(`viewBox=["'][\d.]+\s+[\d.]+\s+([\d.]+)\s+([\d.]+)["']`)
	if m := vbRE.FindStringSubmatch(rootAttrs); m != nil {
		baseW, _ = strconv.ParseFloat(m[1], 64)
		baseH, _ = strconv.ParseFloat(m[2], 64)
	}

	// Fall back to explicit CSS width/height when no viewBox is present.
	if baseW <= 0 || baseH <= 0 {
		for _, m := range svgAttrDimRE.FindAllStringSubmatch(rootAttrs, -1) {
			val, _ := strconv.ParseFloat(m[2], 64)
			if m[1] == "width" && baseW <= 0 {
				baseW = val
			} else if m[1] == "height" && baseH <= 0 {
				baseH = val
			}
		}
	}

	if baseW <= 0 || baseH <= 0 {
		return data
	}

	// Replace percentage width/height with resolved coordinate-space values.
	// Only replace occurrences outside the root <svg> tag.
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
			return fmt.Sprintf(`width="%.4g"`, pct/100.0*baseW)
		}
		return fmt.Sprintf(`height="%.4g"`, pct/100.0*baseH)
	})
	return []byte(prefix + body)
}

// rasterizeSVG renders SVG data to an image.RGBA using oksvg.
// If w/h are 0, the SVG's explicit width/height attributes are used when present;
// otherwise the viewBox dimensions are used.
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

	// When no target size is specified, prefer explicit pixel width/height from
	// the root <svg> element over the viewBox coordinate dimensions.
	// E.g. <svg viewBox="0 0 200 400" width="50px"> has intrinsic size 50×100,
	// not 200×400. The viewBox only defines the coordinate space.
	if w <= 0 && h <= 0 {
		s := string(data)
		if rootMatch := svgOpenTagRE.FindStringSubmatch(s); rootMatch != nil {
			var explW, explH float64
			for _, m := range svgAttrDimRE.FindAllStringSubmatch(rootMatch[1], -1) {
				val, _ := strconv.ParseFloat(m[2], 64)
				if m[1] == "width" {
					explW = val
				} else {
					explH = val
				}
			}
			if explW > 0 && explH > 0 {
				w, h = int(explW), int(explH)
			} else if explW > 0 && icon.ViewBox.W > 0 && icon.ViewBox.H > 0 {
				w = int(explW)
				h = int(explW*icon.ViewBox.H/icon.ViewBox.W + 0.5)
			} else if explH > 0 && icon.ViewBox.W > 0 && icon.ViewBox.H > 0 {
				h = int(explH)
				w = int(explH*icon.ViewBox.W/icon.ViewBox.H + 0.5)
			}
		}
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

// SVGSizingInfo holds the parsed intrinsic sizing metadata from an SVG file.
// Per the SVG and CSS specs, an SVG's intrinsic dimensions come from its explicit
// width/height attributes (not percentage-based). The viewBox provides the intrinsic
// aspect ratio. When width/height are absent, the SVG has no intrinsic dimensions
// but may have an intrinsic ratio from the viewBox.
type SVGSizingInfo struct {
	HasExplicitWidth  bool
	HasExplicitHeight bool
	ExplicitWidth     float64 // only valid when HasExplicitWidth is true
	ExplicitHeight    float64 // only valid when HasExplicitHeight is true
	HasViewBox        bool
	ViewBoxWidth      float64
	ViewBoxHeight     float64
}

// svgViewBoxRE matches a viewBox attribute on the root <svg> element.
var svgViewBoxRE = regexp.MustCompile(`viewBox=["']([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)["']`)

// GetSVGSizingInfo parses SVG data to extract explicit width/height and viewBox.
func GetSVGSizingInfo(data []byte) SVGSizingInfo {
	var info SVGSizingInfo
	rootMatch := svgOpenTagRE.FindStringSubmatch(string(data))
	if rootMatch == nil {
		return info
	}
	rootAttrs := rootMatch[1]

	// Extract explicit width/height (absolute values only, not percentages).
	for _, m := range svgAttrDimRE.FindAllStringSubmatch(rootAttrs, -1) {
		val, _ := strconv.ParseFloat(m[2], 64)
		if m[1] == "width" {
			info.HasExplicitWidth = true
			info.ExplicitWidth = val
		} else if m[1] == "height" {
			info.HasExplicitHeight = true
			info.ExplicitHeight = val
		}
	}

	// Extract viewBox.
	if m := svgViewBoxRE.FindStringSubmatch(rootAttrs); m != nil {
		info.HasViewBox = true
		info.ViewBoxWidth, _ = strconv.ParseFloat(m[3], 64)
		info.ViewBoxHeight, _ = strconv.ParseFloat(m[4], 64)
	}

	return info
}

// GetSVGSizingInfoWithFetcher fetches SVG data and returns sizing info.
func GetSVGSizingInfoWithFetcher(path string, fetcher ImageFetcher) (SVGSizingInfo, error) {
	if fetcher == nil {
		return SVGSizingInfo{}, fmt.Errorf("no fetcher")
	}
	data, err := fetcher(path)
	if err != nil {
		return SVGSizingInfo{}, err
	}
	if !isSVGData(data) {
		return SVGSizingInfo{}, fmt.Errorf("not SVG data")
	}
	return GetSVGSizingInfo(data), nil
}

// IsSVGPath returns true if the path likely refers to an SVG file.
func IsSVGPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".svg") || strings.HasSuffix(lower, ".svgz")
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

// lenientPercentDecode decodes percent-encoded sequences (%XX) while passing
// through invalid sequences (where XX are not valid hex digits). This is needed
// for SVG data URIs that contain literal % characters (e.g. width='100%').
func lenientPercentDecode(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi := unhex(s[i+1])
			lo := unhex(s[i+2])
			if hi >= 0 && lo >= 0 {
				buf.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		buf.WriteByte(s[i])
	}
	return buf.String()
}

func unhex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	}
	return -1
}
