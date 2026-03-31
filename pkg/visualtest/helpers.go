package visualtest

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/js"
	"louis14/pkg/layout"
	"louis14/pkg/render"
	"louis14/pkg/text"
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

	// wptRoot is one level above basePath (e.g. wpt-css2/ for tests in wpt-css2/linebox/).
	// Absolute URL paths like /fonts/ahem.css are resolved relative to this root.
	wptRoot := ""
	if basePath != "" {
		wptRoot = filepath.Dir(basePath)
	}

	if basePath != "" {
		cssFetcher := func(uri string) (string, error) {
			if strings.HasPrefix(uri, "data:") || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
				return "", fmt.Errorf("unsupported URI scheme: %s", uri)
			}
			var cssPath string
			if strings.HasPrefix(uri, "/") {
				// Absolute path — resolve relative to the WPT root.
				cssPath = filepath.Join(wptRoot, uri)
			} else {
				cssPath = filepath.Join(basePath, uri)
			}
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

	// Build font config with any @font-face rules from the document.
	fontConfig := text.DefaultFontConfig()
	if basePath != "" {
		if fc, ok := buildFontConfig(doc, basePath, wptRoot); ok {
			fontConfig = fc
		}
	}

	// Layout
	engine := layout.NewLayoutEngine(float64(width), float64(height))
	engine.SetFontConfig(fontConfig)

	// Set up image fetcher if base path is provided
	var fetcher images.ImageFetcher
	if basePath != "" {
		fetcher = createFileImageFetcher(basePath, wptRoot)
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
		engine2.SetFontConfig(fontConfig)
		if fetcher != nil {
			engine2.SetImageFetcher(fetcher)
		}
		boxes = engine2.Layout(doc)
	}

	// Render
	renderer := render.NewRenderer(width, height)
	renderer.SetFonts(fontConfig)
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

// buildFontConfig processes @font-face rules from the document and returns an
// updated FontConfig with a registry, or false if there are no web fonts.
func buildFontConfig(doc *html.Document, basePath, wptRoot string) (text.FontConfig, bool) {
	var allFaces []css.FontFaceRule
	for _, cssText := range doc.Stylesheets {
		if stylesheet, err := css.ParseStylesheet(cssText); err == nil {
			allFaces = append(allFaces, stylesheet.FontFaces...)
		}
	}
	if len(allFaces) == 0 {
		return text.FontConfig{}, false
	}

	registry := text.NewFontRegistry()
	fontFetcher := func(uri string) ([]byte, error) {
		var fontPath string
		if strings.HasPrefix(uri, "/") {
			fontPath = filepath.Join(wptRoot, uri)
		} else {
			fontPath = filepath.Join(basePath, uri)
		}
		return os.ReadFile(fontPath)
	}

	for _, face := range allFaces {
		if _, err := registry.RegisterFontFace(face.Family, face.Src, face.Format, face.Weight, face.Style, fontFetcher); err != nil {
			log.Printf("font-face: %v", err)
		}
	}

	fc := text.DefaultFontConfig()
	fc.Registry = registry
	return fc, true
}

// createFileImageFetcher creates an ImageFetcher that loads images from the filesystem.
// Absolute paths are resolved relative to wptRoot; relative paths relative to basePath.
func createFileImageFetcher(basePath, wptRoot string) images.ImageFetcher {
	return func(uri string) ([]byte, error) {
		if strings.HasPrefix(uri, "data:") || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
			return nil, fmt.Errorf("unsupported URI scheme: %s", uri)
		}
		var imagePath string
		if strings.HasPrefix(uri, "/") {
			imagePath = filepath.Join(wptRoot, uri)
		} else {
			imagePath = filepath.Join(basePath, uri)
		}
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
