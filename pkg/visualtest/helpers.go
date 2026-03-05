package visualtest

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/js"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

// RenderHTMLToFile renders HTML content to a PNG file
func RenderHTMLToFile(htmlContent string, outputPath string, width, height int) error {
	return RenderHTMLToFileWithBase(htmlContent, outputPath, width, height, "")
}

// RenderHTMLToFileWithBase renders HTML content to a PNG file with a base path for resolving relative image URLs
func RenderHTMLToFileWithBase(htmlContent string, outputPath string, width, height int, basePath string) error {
	// Parse HTML — use fetcher for external CSS if basePath is set
	var doc *html.Document
	var err error
	if basePath != "" {
		cssFetcher := func(uri string) (string, error) {
			if strings.HasPrefix(uri, "data:") || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
				return "", fmt.Errorf("unsupported URI scheme: %s", uri)
			}
			cssPath := filepath.Join(basePath, uri)
			data, err := os.ReadFile(cssPath)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		doc, err = html.ParseWithFetcher(htmlContent, cssFetcher)
	} else {
		doc, err = html.Parse(htmlContent)
	}
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Layout
	engine := layout.NewLayoutEngine(float64(width), float64(height))

	// Multi-pass layout is now the default (no need to enable it explicitly)

	// Set up image fetcher if base path is provided
	var fetcher images.ImageFetcher
	if basePath != "" {
		fetcher = createFileImageFetcher(basePath)
		engine.SetImageFetcher(fetcher)
	}

	boxes := engine.Layout(doc)

	// Execute JavaScript if the document has scripts, then re-layout.
	if len(doc.Scripts) > 0 {
		jsEng := js.New()
		if err := jsEng.Execute(doc); err != nil {
			log.Printf("js: %v", err)
		}
		// Second layout pass to pick up DOM mutations made by JS.
		engine2 := layout.NewLayoutEngine(float64(width), float64(height))
		if fetcher != nil {
			engine2.SetImageFetcher(fetcher)
		}
		boxes = engine2.Layout(doc)
	}

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
