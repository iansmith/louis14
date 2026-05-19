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
	stdnet "louis14/std/net"
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
		engine.SetDocumentFetcher(createFileDocumentFetcher(basePath, wptRoot))
	}

	boxes := engine.Layout(doc)

	// Execute JavaScript if the document has scripts, then re-layout.
	if len(doc.Scripts) > 0 {
		jsEng := js.New()
		jsEng.SetLayoutSnapshot(engine.ComputedStyles(), engine.NodeBoxes())
		if err := jsEng.Execute(doc); err != nil {
			log.Printf("js: %v", err)
		}
		// Second layout pass to pick up DOM mutations made by JS.
		engine2 := layout.NewLayoutEngine(float64(width), float64(height))
		engine2.SetFontConfig(fontConfig)
		if fetcher != nil {
			engine2.SetImageFetcher(fetcher)
		}
		if basePath != "" {
			engine2.SetDocumentFetcher(createFileDocumentFetcher(basePath, wptRoot))
		}
		boxes = engine2.Layout(doc)
	}

	// Collect @counter-style rules from document stylesheets.
	var counterStyles []css.CounterStyleRule
	for _, stylesheet := range css.ParseDocumentStylesheets(doc) {
		counterStyles = append(counterStyles, stylesheet.CounterStyles...)
	}

	// Render
	renderer := render.NewRenderer(width, height)
	renderer.SetFonts(fontConfig)
	if fetcher != nil {
		renderer.SetImageFetcher(fetcher)
	}
	if basePath != "" {
		renderer.SetExternalSVGFetcher(createExternalSVGFetcher(basePath, wptRoot))
	}
	if len(counterStyles) > 0 {
		renderer.SetCounterStyles(counterStyles)
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
	for _, stylesheet := range css.ParseDocumentStylesheets(doc) {
		allFaces = append(allFaces, stylesheet.FontFaces...)
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
// URLs pointing at a configured WPT host (e.g. `http://web-platform.test:8000/...`)
// are rewritten to wptRoot-rooted filesystem paths.
func createFileImageFetcher(basePath, wptRoot string) images.ImageFetcher {
	cfg := DefaultWPTServerConfig()
	return func(uri string) ([]byte, error) {
		if strings.HasPrefix(uri, "data:") {
			return nil, fmt.Errorf("unsupported URI scheme: %s", uri)
		}
		if rewritten, ok := stripWPTHost(uri, cfg); ok {
			uri = rewritten
		} else if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "//") {
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

// createFileDocumentFetcher creates a DocumentFetcher that loads HTML documents
// from the filesystem. Handles relative file paths, data:text/html, URIs, and
// WPT-host URLs (e.g. `//www.not-web-platform.test:8000/...`). If the fetched
// file has a `.sub.` stem its contents are run through ApplyWPTSubstitutions.
func createFileDocumentFetcher(basePath, wptRoot string) layout.DocumentFetcher {
	cfg := DefaultWPTServerConfig()
	return func(uri string) (string, error) {
		// Handle data: URIs (e.g. data:text/html,<html>...</html>)
		if strings.HasPrefix(uri, "data:text/html,") {
			content := strings.TrimPrefix(uri, "data:text/html,")
			return content, nil
		}
		if strings.HasPrefix(uri, "data:") {
			return "", fmt.Errorf("unsupported URI scheme: %s", uri)
		}
		if rewritten, ok := stripWPTHost(uri, cfg); ok {
			uri = rewritten
		} else if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "//") {
			return "", fmt.Errorf("unsupported URI scheme: %s", uri)
		}
		var docPath string
		if strings.HasPrefix(uri, "/") {
			docPath = filepath.Join(wptRoot, uri)
		} else {
			docPath = filepath.Join(basePath, uri)
		}
		data, err := os.ReadFile(docPath)
		if err != nil {
			return "", err
		}
		content := string(data)
		if strings.Contains(filepath.Base(docPath), ".sub.") {
			content = ApplyWPTSubstitutions(content, docPath, wptRoot)
		}
		return content, nil
	}
}

// createExternalSVGFetcher creates an ExternalSVGFetcher closure for
// `filter: url(doc#id)` references. Handles three URL forms:
//
//   - data: URLs decode inline via stdnet.DecodeDataURL.
//   - Absolute paths (`/foo/x.svg`) resolve relative to wptRoot.
//   - Relative paths (`support/x.svg`) resolve against basePath.
//
// LOU-138 phase 5 eliminated the previous cssDirs fallback: external
// stylesheets now carry their own ParserContext.BaseDir, so url() refs
// inside them get pre-resolved at CSS parse time against the sheet's own
// location. The fetcher receives the already-rooted URI on first try.
//
// file:// URLs are accepted too (read via os.ReadFile after path
// extraction). Network (http(s)://) URIs are rejected — tests don't
// hit the network. LOU-130 Phase 2 wiring.
func createExternalSVGFetcher(basePath, wptRoot string) func(uri string) ([]byte, error) {
	return func(uri string) ([]byte, error) {
		switch {
		case stdnet.IsDataURL(uri):
			body, _, err := stdnet.DecodeDataURL(uri)
			return body, err
		case stdnet.IsFileURL(uri):
			p, err := stdnet.FileURLToPath(uri)
			if err != nil {
				return nil, err
			}
			return os.ReadFile(p)
		case strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://"):
			return nil, fmt.Errorf("unsupported URI scheme: %s", uri)
		default:
			if strings.HasPrefix(uri, "/") {
				return os.ReadFile(filepath.Join(wptRoot, uri))
			}
			return os.ReadFile(filepath.Join(basePath, uri))
		}
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
