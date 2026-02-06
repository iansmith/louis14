package main

import (
	"fmt"
	"os"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

func printBoxTree(box *layout.Box, indent string) {
	if box == nil {
		return
	}
	fmt.Printf("%sBox: ", indent)
	if box.Node != nil {
		if box.Node.Type == 3 { // TextNode
			text := box.Node.Text
			if len(text) > 20 {
				text = text[:20] + "..."
			}
			fmt.Printf("TEXT(%q) ", text)
		} else if box.Node.TagName != "" {
			fmt.Printf("<%s> ", box.Node.TagName)
		}
	}
	fmt.Printf("at (%.0f,%.0f) size %.0fx%.0f, %d children\n",
		box.X, box.Y, box.Width, box.Height, len(box.Children))
	for _, child := range box.Children {
		printBoxTree(child, indent+"  ")
	}
}

func main() {
	htmlContent, _ := os.ReadFile("test-simple-block.html")
	doc, _ := html.Parse(string(htmlContent))

	// Multi-pass
	fmt.Println("=== MULTI-PASS BOXES ===")
	engine := layout.NewLayoutEngine(400, 400)
	engine.SetUseMultiPass(true)
	boxes := engine.Layout(doc)
	for _, box := range boxes {
		printBoxTree(box, "")
	}

	// Single-pass
	fmt.Println("\n=== SINGLE-PASS BOXES ===")
	engine2 := layout.NewLayoutEngine(400, 400)
	boxes2 := engine2.Layout(doc)
	for _, box := range boxes2 {
		printBoxTree(box, "")
	}
}
