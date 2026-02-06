package main

import (
	"fmt"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
	"os"
)

func main() {
	content, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/borders/border-005.xht")
	doc, _ := html.Parse(string(content))
	engine := layout.NewLayoutEngine(400, 400)
	boxes := engine.Layout(doc)

	// Render the page
	renderer := render.NewRenderer(400, 400)
	renderer.Render(boxes)

	// Save test output
	renderer.SavePNG("test-border-005-debug.png")

	// Find and print all divs
	fmt.Println("\n=== All divs in layout tree ===")
	findAllDivs(boxes[0], 0)

	// Count how many times boxes appear in tree
	fmt.Println("\n=== Box tree structure ===")
	printBoxTree(boxes[0], 0)
}

func findAllDivs(box *layout.Box, depth int) {
	if box.Node != nil && box.Node.TagName == "div" {
		id := ""
		if idVal, ok := box.Node.Attributes["id"]; ok {
			id = " id=" + idVal
		}

		fmt.Printf("%sdiv%s: pos=(%.0f,%.0f) size=%.0fx%.0f border=[T%.0f R%.0f B%.0f L%.0f]\n",
			indent(depth), id, box.X, box.Y, box.Width, box.Height,
			box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left)

		if box.Style != nil {
			if bg, ok := box.Style.Get("background-color"); ok {
				fmt.Printf("%s  background-color: %s\n", indent(depth), bg)
			}
			if bc, ok := box.Style.Get("border-color"); ok {
				fmt.Printf("%s  border-color: %s\n", indent(depth), bc)
			}
		}
	}

	for _, child := range box.Children {
		findAllDivs(child, depth+1)
	}
}

func printBoxTree(box *layout.Box, depth int) {
	tag := "?"
	id := ""
	if box.Node != nil {
		tag = box.Node.TagName
		if idVal, ok := box.Node.Attributes["id"]; ok {
			id = "#" + idVal
		}
	}

	fmt.Printf("%s<%s%s> pos=(%.0f,%.0f) size=%.0fx%.0f\n",
		indent(depth), tag, id, box.X, box.Y, box.Width, box.Height)

	for _, child := range box.Children {
		printBoxTree(child, depth+1)
	}
}

func indent(depth int) string {
	s := ""
	for i := 0; i < depth; i++ {
		s += "  "
	}
	return s
}
