package visualtest

import (
	"fmt"
	"os"
	"path/filepath"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

// RenderHTMLToFile renders HTML content to a PNG file
func RenderHTMLToFile(htmlContent string, outputPath string, width, height int) error {
	// Parse HTML
	doc, err := html.Parse(htmlContent)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Layout
	engine := layout.NewLayoutEngine(float64(width), float64(height))
	boxes := engine.Layout(doc)

	// Render
	renderer := render.NewRenderer(width, height)
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
