package main

import (
	"fmt"
	"os"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

func main() {
	// Read test HTML
	htmlPath := "pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht"
	htmlContent, err := os.ReadFile(htmlPath)
	if err != nil {
		fmt.Printf("Error reading HTML: %v\n", err)
		os.Exit(1)
	}

	// Parse HTML
	doc, err := html.Parse(string(htmlContent))
	if err != nil {
		fmt.Printf("Parse error: %v\n", err)
		os.Exit(1)
	}

	// Layout with multi-pass enabled
	width, height := 400, 300
	engine := layout.NewLayoutEngine(float64(width), float64(height))
	engine.SetUseMultiPass(true) // Enable multi-pass

	fmt.Println("=== RENDERING box-generation-001.xht WITH MULTI-PASS ===\n")
	boxes := engine.Layout(doc)

	// Render
	renderer := render.NewRenderer(width, height)
	renderer.Render(boxes)

	// Save
	outputPath := "output/test-box-gen-debug.png"
	os.MkdirAll("output", 0755)
	if err := renderer.SavePNG(outputPath); err != nil {
		fmt.Printf("Save error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Saved to %s\n", outputPath)
}
