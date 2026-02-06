package main

import (
	"fmt"
	"os"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

func printBoxDetails(box *layout.Box, indent string) {
	if box == nil {
		return
	}
	fmt.Printf("%sBox at (%.0f,%.0f): ", indent, box.X, box.Y)
	if box.Node != nil && box.Node.Type == 3 { // TextNode
		fmt.Printf("TEXT node=%q ", box.Node.Text[:min(20, len(box.Node.Text))])
	} else if box.Node != nil && box.Node.TagName != "" {
		fmt.Printf("<%s> ", box.Node.TagName)
	}
	fmt.Printf("size %.0fx%.0f", box.Width, box.Height)
	// Check if box itself stores text (for rendering)
	if box.Node != nil && box.Node.Type == 3 {
		fmt.Printf(" [Node.Text set]")
	}
	fmt.Printf("\n")
	for _, child := range box.Children {
		printBoxDetails(child, indent+"  ")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	htmlContent, _ := os.ReadFile("test-simple-block.html")
	doc, _ := html.Parse(string(htmlContent))

	fmt.Println("=== MULTI-PASS ===")
	engine := layout.NewLayoutEngine(400, 400)
	engine.SetUseMultiPass(true)
	boxes := engine.Layout(doc)
	for _, box := range boxes {
		printBoxDetails(box, "")
	}
}
