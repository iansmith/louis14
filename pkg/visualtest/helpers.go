package visualtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

// RenderHTMLToFile renders HTML content to a PNG file
func RenderHTMLToFile(htmlContent string, outputPath string, width, height int) error {
	return RenderHTMLToFileWithBase(htmlContent, outputPath, width, height, "")
}

// RenderHTMLToFileWithBase renders HTML content to a PNG file with a base path for resolving relative image URLs
func RenderHTMLToFileWithBase(htmlContent string, outputPath string, width, height int, basePath string) error {
	// Parse HTML
	doc, err := html.Parse(htmlContent)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Layout
	engine := layout.NewLayoutEngine(float64(width), float64(height))

	// Enable multi-pass layout for ALL tests
	// This uses the new clean three-phase pipeline (LayoutInlineContentToBoxes)
	// Phase A: Inline box wrapper creation implemented
	// Phase B: Block-in-inline fragment splitting implemented
	engine.SetUseMultiPass(true)

	// Set up image fetcher if base path is provided
	var fetcher images.ImageFetcher
	if basePath != "" {
		fetcher = createFileImageFetcher(basePath)
		engine.SetImageFetcher(fetcher)
	}

	boxes := engine.Layout(doc)

	// Render
	renderer := render.NewRenderer(width, height)
	if fetcher != nil {
		renderer.SetImageFetcher(fetcher)
	}
	renderer.Render(boxes)

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save
	if err := renderer.SavePNG(outputPath); err != nil {
		return fmt.Errorf("save error: %w", err)
	}

	return nil
}

// createFileImageFetcher creates an ImageFetcher that loads images from the filesystem
// relative to the given base path
func createFileImageFetcher(basePath string) images.ImageFetcher {
	return func(uri string) ([]byte, error) {
		// Skip data URIs and absolute URLs
		if strings.HasPrefix(uri, "data:") || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
			return nil, fmt.Errorf("unsupported URI scheme: %s", uri)
		}

		// Resolve relative path against base path
		imagePath := filepath.Join(basePath, uri)
		return os.ReadFile(imagePath)
	}
}

// RenderHTMLFile renders an HTML file to a PNG file
func RenderHTMLFile(htmlPath, outputPath string, width, height int) error {
	htmlContent, err := os.ReadFile(htmlPath)
	if err != nil {
		return fmt.Errorf("failed to read HTML file: %w", err)
	}

	return RenderHTMLToFile(string(htmlContent), outputPath, width, height)
}

// UpdateReferenceImage generates a new reference image
// Use this when you've intentionally changed rendering behavior
func UpdateReferenceImage(htmlPath, referencePath string, width, height int) error {
	fmt.Printf("⚠️  Updating reference image: %s\n", referencePath)
	return RenderHTMLFile(htmlPath, referencePath, width, height)
}
