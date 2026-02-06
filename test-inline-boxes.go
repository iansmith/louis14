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
	htmlContent, err := os.ReadFile("test-span-width.html")
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
	width, height := 400, 200
	engine := layout.NewLayoutEngine(float64(width), float64(height))
	engine.SetUseMultiPass(true) // Enable multi-pass

	fmt.Println("=== RENDERING WITH MULTI-PASS ===")
	boxes := engine.Layout(doc)

	fmt.Printf("\n=== BOX TREE (total %d boxes) ===\n", len(boxes))
	printBoxTree(boxes, 0)

	// Render
	renderer := render.NewRenderer(width, height)
	renderer.Render(boxes)

	// Save
	outputPath := "output/test-span-width.png"
	if err := renderer.SavePNG(outputPath); err != nil {
		fmt.Printf("Save error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Saved to %s\n", outputPath)
	fmt.Println("\nOpen with: open output/test-span-width.png")
}

func printBoxTree(boxes []*layout.Box, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	for _, box := range boxes {
		nodeName := getNodeName(box.Node)
		fmt.Printf("%s- %s: (%.1f, %.1f) %.1fx%.1f\n", indent, nodeName, box.X, box.Y, box.Width, box.Height)

		if len(box.Children) > 0 {
			printBoxTree(box.Children, depth+1)
		}
	}
}

func getNodeName(node *html.Node) string {
	if node == nil {
		return "<nil>"
	}
	if node.Type == html.TextNode {
		text := node.Text
		if len(text) > 20 {
			text = text[:20] + "..."
		}
		return fmt.Sprintf("TEXT(%q)", text)
	}
	if node.Type == html.ElementNode {
		if node.TagName != "" {
			return "<" + node.TagName + ">"
		}
		return "<element>"
	}
	return fmt.Sprintf("<%v>", node.Type)
}
