package main

import (
	"fmt"
	"os"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input.html> <output.png>\n", os.Args[0])
		os.Exit(1)
	}
	inputFile := os.Args[1]
	outputFile := os.Args[2]
	htmlContent, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}
	doc, err := html.Parse(string(htmlContent))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing HTML: %v\n", err)
		os.Exit(1)
	}
	viewportWidth := 800.0
	viewportHeight := 600.0
	layoutEngine := layout.NewLayoutEngine(viewportWidth, viewportHeight)
	boxes := layoutEngine.Layout(doc)
	renderer := render.NewRenderer(int(viewportWidth), int(viewportHeight))
	renderer.Render(boxes)
	if err := renderer.SavePNG(outputFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving PNG: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Successfully rendered %s to %s\n", inputFile, outputFile)
	fmt.Printf("Rendered %d boxes\n", len(boxes))
}
