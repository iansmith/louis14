package main

import (
	"fmt"
	"os"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

func main() {
	// Read HTML
	htmlContent, err := os.ReadFile("test-simple-block.html")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Parse
	doc, err := html.Parse(string(htmlContent))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing HTML: %v\n", err)
		os.Exit(1)
	}

	// Layout with multi-pass ENABLED
	engine := layout.NewLayoutEngine(400, 400)
	engine.SetUseMultiPass(true)
	boxes := engine.Layout(doc)

	// Render
	renderer := render.NewRenderer(400, 400)
	renderer.Render(boxes)
	
	if err := renderer.SavePNG("test-simple-block-multipass.png"); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving PNG: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Rendered to test-simple-block-multipass.png")
}
