package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/js"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input.html> <output.png> [width] [height]\n", os.Args[0])
		os.Exit(1)
	}
	inputFile := os.Args[1]
	outputFile := os.Args[2]

	// Default viewport size
	viewportWidth := 800.0
	viewportHeight := 2400.0 // Much taller default for typical web pages

	// Parse optional width and height arguments
	if len(os.Args) >= 4 {
		fmt.Sscanf(os.Args[3], "%f", &viewportWidth)
	}
	if len(os.Args) >= 5 {
		fmt.Sscanf(os.Args[4], "%f", &viewportHeight)
	}

	htmlContent, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}
	// Create a filesystem CSS fetcher that resolves relative paths against the input file
	baseDir := filepath.Dir(inputFile)
	cssFetcher := func(uri string) (string, error) {
		resolvedPath := uri
		if !filepath.IsAbs(uri) {
			resolvedPath = filepath.Join(baseDir, uri)
		}
		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	doc, err := html.ParseWithFetcher(string(htmlContent), cssFetcher)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing HTML: %v\n", err)
		os.Exit(1)
	}

	// Create a filesystem fetcher that resolves relative paths against the input file
	fetcher := images.NewFilesystemFetcher(inputFile)

	layoutEngine := layout.NewLayoutEngine(viewportWidth, viewportHeight)
	layoutEngine.SetImageFetcher(fetcher)
	boxes := layoutEngine.Layout(doc)

	renderer := render.NewRenderer(int(viewportWidth), int(viewportHeight))
	renderer.SetImageFetcher(fetcher)
	renderer.Render(boxes)

	// Execute JavaScript if there are scripts
	if len(doc.Scripts) > 0 {
		engine := js.New()
		if err := engine.Execute(doc); err != nil {
			log.Printf("js: %v", err)
		}
		// Re-layout and re-render with JS modifications
		layoutEngine2 := layout.NewLayoutEngine(viewportWidth, viewportHeight)
		layoutEngine2.SetImageFetcher(fetcher)
		boxes2 := layoutEngine2.Layout(doc)
		renderer = render.NewRenderer(int(viewportWidth), int(viewportHeight))
		renderer.SetImageFetcher(fetcher)
		renderer.Render(boxes2)
	}

	if err := renderer.SavePNG(outputFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving PNG: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Successfully rendered %s to %s\n", inputFile, outputFile)
	fmt.Printf("Viewport: %.0fx%.0f, Rendered %d boxes\n", viewportWidth, viewportHeight, len(boxes))

	// Try to open the output file; ignore errors (e.g. if "open" is not available)
	exec.Command("open", outputFile).Start()
}
