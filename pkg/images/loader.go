package images

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
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

// LoadImage loads an image from the filesystem
func LoadImage(path string) (image.Image, error) {
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
