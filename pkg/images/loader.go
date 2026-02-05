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
	"strings"
	"sync"
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

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image decode error: %w", err)
	}

	return img, nil
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
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image decode error: %w", err)
	}
	return img, nil
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
