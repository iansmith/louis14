package images

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	stdnet "louis14/std/net"
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

// LoadImageFromDataURI decodes a data URI and returns the embedded image.
// Format: data:[<mediatype>][;base64],<data>
func LoadImageFromDataURI(uri string) (image.Image, error) {
	data, _, err := stdnet.DecodeDataURL(uri)
	if err != nil {
		return nil, err
	}
	return DecodeImageBytes(data)
}

// LoadImage loads an image from the filesystem or a data URI.
func LoadImage(path string) (image.Image, error) {
	// Handle data URIs
	if stdnet.IsDataURL(path) {
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

	// CSS 2.1 §10.3.2 default replaced-element size when intrinsic dimensions
	// are missing: 300×150. Use those defaults so single-dimension SVGs (e.g.
	// `<svg width="100">`) can still resolve percentage attributes on children.
	if baseW <= 0 {
		baseW = 300
	}
	if baseH <= 0 {
		baseH = 150
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

// svgRootStyleRE captures the value of a `style` attribute on the root <svg>.
var svgRootStyleRE = regexp.MustCompile(`(?s)\bstyle\s*=\s*["']([^"']*)["']`)

// svgBackgroundDeclRE matches a `background` or `background-color` declaration
// in a style attribute, capturing the color value (everything up to ; or end).
var svgBackgroundDeclRE = regexp.MustCompile(`(?:^|;)\s*background(?:-color)?\s*:\s*([^;]+)`)

// preprocessSVGBackground inserts a full-bleed <rect> at the start of the SVG
// body when the root <svg> has a CSS `background`/`background-color`
// declaration in its style attribute. oksvg honors fill on <rect>, but ignores
// CSS background on the root, which leaves WPT fixtures like
//
//	<svg style="background: green" width="100" height="100">…</svg>
//
// rasterizing as transparent. Inserting a coloured rect inside the SVG matches
// the visual effect browsers produce for the same markup.
func preprocessSVGBackground(data []byte) []byte {
	s := string(data)

	rootMatch := svgOpenTagRE.FindStringSubmatch(s)
	if rootMatch == nil {
		return data
	}
	rootAttrs := rootMatch[1]

	styleMatch := svgRootStyleRE.FindStringSubmatch(rootAttrs)
	if styleMatch == nil {
		return data
	}
	bgMatch := svgBackgroundDeclRE.FindStringSubmatch(styleMatch[1])
	if bgMatch == nil {
		return data
	}
	color := strings.TrimSpace(bgMatch[1])
	if color == "" || color == "none" || color == "transparent" {
		return data
	}

	rootTagEnd := strings.Index(s, ">") + 1
	prefix := s[:rootTagEnd]
	body := s[rootTagEnd:]

	rect := `<rect x="0" y="0" width="100%" height="100%" fill="` + color + `"/>`
	return []byte(prefix + rect + body)
}

// svgViewBoxAttrRE detects the presence of a viewBox attribute on the root.
var svgViewBoxAttrRE = regexp.MustCompile(`\bviewBox\s*=`)

// preprocessSVGViewBox synthesizes a viewBox on the root <svg> when one is
// absent. oksvg needs a non-zero viewBox to render geometry — without it,
// `<svg width="100" height="100">` rasterizes empty even with explicit shapes.
// The synthesized viewBox uses the root width/height (or CSS defaults 300×150)
// so the SVG coordinate space matches its intrinsic dimensions.
func preprocessSVGViewBox(data []byte) []byte {
	s := string(data)
	rootMatch := svgOpenTagRE.FindStringSubmatch(s)
	if rootMatch == nil {
		return data
	}
	rootAttrs := rootMatch[1]
	if svgViewBoxAttrRE.MatchString(rootAttrs) {
		return data
	}

	var w, h float64
	for _, m := range svgAttrDimRE.FindAllStringSubmatch(rootAttrs, -1) {
		val, _ := strconv.ParseFloat(m[2], 64)
		if m[1] == "width" {
			w = val
		} else if m[1] == "height" {
			h = val
		}
	}
	if w <= 0 {
		w = 300
	}
	if h <= 0 {
		h = 150
	}

	rootTagEnd := strings.Index(s, ">")
	if rootTagEnd <= 0 {
		return data
	}
	return []byte(s[:rootTagEnd] + fmt.Sprintf(` viewBox="0 0 %g %g"`, w, h) + s[rootTagEnd:])
}

// rasterizeSVG renders SVG data to an image.RGBA using oksvg.
// If w/h are 0, the SVG's explicit width/height attributes are used when present;
// otherwise the viewBox dimensions are used.
func rasterizeSVG(data []byte, w, h int) (image.Image, error) {
	// oksvg doesn't paint the root <svg> element's CSS `background` declaration
	// (e.g. <svg style="background: green">). Per SVG 1.1 §2.5, the SVG
	// background acts like the canvas color. To honor it, inject a full-bleed
	// <rect> filling the SVG with the background color as the first child.
	// Inject BEFORE percentage resolution so the rect's width="100%"/height="100%"
	// get resolved alongside any other percentage shape attributes.
	data = preprocessSVGBackground(data)

	// Without a viewBox, oksvg's coordinate space is undefined and rendered
	// shapes vanish. Synthesize one from the root width/height (or CSS defaults
	// 300×150) so percentage-based and absolute shapes both rasterize.
	data = preprocessSVGViewBox(data)

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
			} else if explW > 0 {
				// Width only, no viewBox/height: pair with CSS 2.1 §10.3.2's
				// default intrinsic height (150) so the buffer matches what
				// the layout engine produces for the same case.
				w, h = int(explW), 150
			} else if explH > 0 {
				w, h = 300, int(explH)
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
// The fetcher serves every URI, data: URLs included (stdnet.RequestResource).
// Falls back to LoadImage when no fetcher is provided.
func LoadImageWithFetcher(path string, fetcher ImageFetcher) (image.Image, error) {
	// If no fetcher, use regular loading (absolute paths and data: URLs)
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
		return nil, fmt.Errorf("fetching image %s: %w", stdnet.DataURLForDisplay(path), err)
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

// ImageIsPotentiallyAvailable reports whether the image at src can be shown:
// it loaded with usable dimensions, or no load was attempted yet. Mirrors
// ImageLoader::ImageIsPotentiallyAvailable (image_loader.cc:284-310 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): an empty source URL is never
// available, and an image that is still loading is. louis14 loads
// synchronously, so "still loading" is the case with no fetcher (no load is
// attempted; data: URIs decode without one). Otherwise the image must
// fetch and decode (SVG included: DecodeImageBytes rasterizes it) to a
// positive size — Blink's "loaded without error and has an image".
func ImageIsPotentiallyAvailable(src string, fetcher ImageFetcher) bool {
	if src == "" {
		return false
	}
	if fetcher == nil && !stdnet.IsDataURL(src) {
		return true
	}
	w, h, err := GetImageDimensionsWithFetcher(src, fetcher)
	return err == nil && w > 0 && h > 0
}

// isNetworkURI returns true if the string looks like an HTTP/HTTPS URL.
func isNetworkURI(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// NewFilesystemFetcher creates an ImageFetcher that resolves relative paths
// against a base URL (typically the document's file path); data: URLs are
// served through stdnet.RequestResource.
func NewFilesystemFetcher(baseURL string) ImageFetcher {
	return stdnet.RequestResource(func(uri string) ([]byte, error) {
		// Don't resolve absolute network URLs
		if isNetworkURI(uri) {
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
	})
}
