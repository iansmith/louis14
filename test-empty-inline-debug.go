package main

import (
	"fmt"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"os"
)

func main() {
	content, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/linebox/empty-inline-002.xht")
	doc, _ := html.Parse(string(content))

	computedStyles := css.ComputeStyles(doc.Root, 400, 400)
	engine := layout.NewLayoutEngine()
	box := engine.LayoutDocument(doc, 400, 400, computedStyles)

	printBox(box, 0)
}

func printBox(box *layout.Box, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	name := "box"
	if box.Node != nil {
		if box.Node.TagName != "" {
			name = "<" + box.Node.TagName + ">"
			if id, ok := box.Node.Attributes["id"]; ok {
				name += "#" + id
			}
		} else if box.Node.Type == html.TextNode {
			name = "TEXT"
		}
	}

	fmt.Printf("%s%s: pos=(%.1f,%.1f) size=(%.1fx%.1f) padding=(%.1f,%.1f,%.1f,%.1f) border=(%.1f,%.1f,%.1f,%.1f) margin=(%.1f,%.1f,%.1f,%.1f)\n",
		indent, name,
		box.X, box.Y, box.Width, box.Height,
		box.Padding.Top, box.Padding.Right, box.Padding.Bottom, box.Padding.Left,
		box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left,
		box.Margin.Top, box.Margin.Right, box.Margin.Bottom, box.Margin.Left)

	for _, child := range box.Children {
		printBox(child, depth+1)
	}
}
