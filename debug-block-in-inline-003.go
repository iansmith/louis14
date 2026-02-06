package main

import (
	"fmt"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"os"
)

func main() {
	// Test file
	content, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/box-display/block-in-inline-003.xht")
	doc, _ := html.Parse(string(content))
	engine := layout.NewLayoutEngine(400, 400)
	boxes := engine.Layout(doc)

	fmt.Println("=== TEST layout ===")
	printTree(boxes[0], 0)

	// Reference file
	content, _ = os.ReadFile("pkg/visualtest/testdata/wpt-css2/box-display/block-in-inline-003-ref.xht")
	doc, _ = html.Parse(string(content))
	engine = layout.NewLayoutEngine(400, 400)
	boxes = engine.Layout(doc)

	fmt.Println("\n=== REFERENCE layout ===")
	printTree(boxes[0], 0)
}

func printTree(box *layout.Box, depth int) {
	tag := "?"
	class := ""
	if box.Node != nil {
		tag = box.Node.TagName
		if c, ok := box.Node.Attributes["class"]; ok {
			class = "." + c
		}
	}

	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	fmt.Printf("%s<%s%s> pos=(%.0f,%.0f) size=%.0fx%.0f margin=[T%.0f]\n",
		indent, tag, class, box.X, box.Y, box.Width, box.Height, box.Margin.Top)

	for _, child := range box.Children {
		printTree(child, depth+1)
	}
}
