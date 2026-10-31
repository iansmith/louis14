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

// RenderHTMLToFileWithBase renders HTML content to a PNG file. basePath is
// the directory of the document; when set, every subresource the document
// references (stylesheets, scripts, @font-face sources, images, nested
// documents, external SVG) is loaded from the WPT corpus through one
// wptResourceResolver for that directory.
func RenderHTMLToFileWithBase(htmlContent string, outputPath string, width, height int, basePath string) error {
	var doc *html.Document
	var err error

	var resolver wptResourceResolver
	if basePath != "" {
		resolver = newWPTResourceResolver(basePath)
		// Scripts resolve exactly like stylesheets, so <script src> reuses
		// the CSS fetcher.
		cssFetcher := createFileCSSFetcher(resolver)
		doc, err = html.ParseWithScriptFetcher(htmlContent, cssFetcher, html.ScriptFetcher(cssFetcher))
	} else {
		doc, err = html.Parse(htmlContent)
	}
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Build font config with any @font-face rules from the document.
	fontConfig := text.DefaultFontConfig()
	if basePath != "" {
		if fc, ok := buildFontConfig(doc, resolver); ok {
			fontConfig = fc
		}
	}

	// Layout
	engine := layout.NewLayoutEngine(float64(width), float64(height))
	engine.SetFontConfig(fontConfig)

	// Set up image fetcher if base path is provided
	var fetcher images.ImageFetcher
	if basePath != "" {
		fetcher = resolver.readFile
		engine.SetImageFetcher(fetcher)
		engine.SetDocumentFetcher(createFileDocumentFetcher(resolver))
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
		// CSS Transitions: the post-script pass compares against the first
		// pass's computed styles (before-change styles).
		engine2.SetPreviousStyles(engine.ComputedStyles())
		engine2.SetFontConfig(fontConfig)
		if fetcher != nil {
			engine2.SetImageFetcher(fetcher)
		}
		if basePath != "" {
			engine2.SetDocumentFetcher(createFileDocumentFetcher(resolver))
		}
		boxes = engine2.Layout(doc)
	}

	// Collect @counter-style rules from document stylesheets.
	parsedStylesheets := css.ParseDocumentStylesheets(doc)
	var counterStyles []css.CounterStyleRule
	for _, stylesheet := range parsedStylesheets {
		counterStyles = append(counterStyles, stylesheet.CounterStyles...)
	}

	// Render
	renderer := render.NewRenderer(width, height)
	renderer.SetFonts(fontConfig)
	if fetcher != nil {
		renderer.SetImageFetcher(fetcher)
	}
	if basePath != "" {
		renderer.SetExternalSVGFetcher(createExternalSVGFetcher(resolver))
	}
	if len(counterStyles) > 0 {
		renderer.SetCounterStyles(counterStyles)
	}
	// LOU-344: ::selection highlight painting. doc.Selection is populated
	// by the JS execution above (document.createRange/getSelection/
	// addRange); nil when the test's script never touched it, in which
	// case drawText's selection check no-ops immediately.
	renderer.SetSelectionContext(doc.Selection, parsedStylesheets, float64(width), float64(height))
	// LOU-349: ::target-text highlight painting. doc.TargetTextRanges is
	// populated by the JS execution above (window.location.hash/href
	// assigning a `:~:text=` directive, see pkg/js/dom_location.go); nil
	// when the test's script never touched it, in which case drawText's
	// target-text check no-ops immediately (same fail-open shape as
	// Selection above). Stylesheets/viewport are NOT passed again here —
	// see SetTargetTextRanges's doc comment for why it reuses the triple
	// SetSelectionContext just set.
	renderer.SetTargetTextRanges(doc.TargetTextRanges)
	// LOU-354: ::highlight(name) highlight painting. doc.CustomHighlights/
	// CustomHighlightOrder are populated by the JS execution above
	// (CSS.highlights.set, see pkg/js/dom_highlight.go); nil/empty when the
	// test's script never touched CSS.highlights, in which case drawText's
	// custom-highlight check no-ops immediately (same fail-open shape as
	// TargetTextRanges above).
	renderer.SetCustomHighlights(doc.CustomHighlights, doc.CustomHighlightOrder)
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

// createFileCSSFetcher creates the CSSFetcher (also used for <script src>)
// that loads stylesheets from the WPT corpus through resolver.
func createFileCSSFetcher(resolver wptResourceResolver) html.CSSFetcher {
	return func(uri string) (string, error) {
		data, err := resolver.readFile(uri)
		return string(data), err
	}
}

// buildFontConfig processes @font-face rules from the document and returns an
// updated FontConfig with a registry, or false if there are no web fonts.
func buildFontConfig(doc *html.Document, resolver wptResourceResolver) (text.FontConfig, bool) {
	var allFaces []css.FontFaceRule
	for _, stylesheet := range css.ParseDocumentStylesheets(doc) {
		allFaces = append(allFaces, stylesheet.FontFaces...)
	}
	if len(allFaces) == 0 {
		return text.FontConfig{}, false
	}

	registry := text.NewFontRegistry()

	for _, face := range allFaces {
		if face.Src == nil {
			continue
		}
		hasOverride := face.AscentOverride != nil || face.DescentOverride != nil || face.LineGapOverride != nil
		mo := text.MetricsOverride{
			Ascent:  face.AscentOverride,
			Descent: face.DescentOverride,
			LineGap: face.LineGapOverride,
		}
		path, err := registry.RegisterFontFace(face.Family, face.Src.Data.Absolute, face.Format, face.Weight, face.Style, resolver.readFile)
		if err != nil {
			log.Printf("font-face: %v", err)
			// The URL fetch failed, but a local/system font may still serve
			// this family (e.g. local(Ahem) resolved to fc.Ahem). Store the
			// override keyed by family so MetricsOverrideForPath can apply it
			// when the system font path is returned by FontPathForFamily.
			if hasOverride {
				registry.SetFontFamilyOverride(face.Family, mo)
			}
			continue
		}
		if len(face.UnicodeRanges) > 0 {
			registry.SetUnicodeRanges(path, face.UnicodeRanges)
		}
		if hasOverride {
			registry.SetFontFaceOverride(path, mo)
		}
	}

	fc := text.DefaultFontConfig()
	fc.Registry = registry
	return fc, true
}

// createFileDocumentFetcher creates a DocumentFetcher that loads HTML
// documents from the WPT corpus through resolver, after decoding inline
// data:text/html, URIs. A fetched file with a `.sub.` stem is run through
// ApplyWPTSubstitutions.
func createFileDocumentFetcher(resolver wptResourceResolver) layout.DocumentFetcher {
	return func(uri string) (string, error) {
		if content, ok := strings.CutPrefix(uri, "data:text/html,"); ok {
			return content, nil
		}
		docPath, err := resolver.filesystemPath(uri)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(docPath)
		if err != nil {
			return "", err
		}
		content := string(data)
		if strings.Contains(filepath.Base(docPath), ".sub.") {
			content = ApplyWPTSubstitutions(content, docPath, resolver.docRoot)
		}
		return content, nil
	}
}

// createExternalSVGFetcher creates an ExternalSVGFetcher closure for
// `filter: url(doc#id)` references. data: URLs decode inline via
// stdnet.DecodeDataURL and file:// URLs read the named file; everything
// else loads from the WPT corpus through resolver.
//
// LOU-138 phase 5 eliminated the previous cssDirs fallback: external
// stylesheets now carry their own ParserContext.BaseDir, so url() refs
// inside them get pre-resolved at CSS parse time against the sheet's own
// location. The fetcher receives the already-rooted URI on first try.
// LOU-130 Phase 2 wiring.
func createExternalSVGFetcher(resolver wptResourceResolver) func(uri string) ([]byte, error) {
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
		default:
			return resolver.readFile(uri)
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
