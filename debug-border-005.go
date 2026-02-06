package main

import (
	"fmt"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"os"
)

func main() {
	content, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/borders/border-005.xht")
	doc, _ := html.Parse(string(content))
	engine := layout.NewLayoutEngine(400, 400)
	boxes := engine.Layout(doc)
	
	findTestDivs(boxes[0], 0)
}

func findTestDivs(box *layout.Box, depth int) {
	if box.Node != nil && box.Node.TagName == "div" {
		id := ""
		if idVal, ok := box.Node.Attributes["id"]; ok {
			id = "#" + idVal
		}
		
		if id == "#test" || id == "#reference" {
			fmt.Printf("div%s:\n", id)
			fmt.Printf("  Position: (%.0f, %.0f)\n", box.X, box.Y)
			fmt.Printf("  Size: %.0fx%.0f (border box)\n", box.Width, box.Height)
			fmt.Printf("  Border: T=%.0f R=%.0f B=%.0f L=%.0f\n", 
				box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left)
			
			if box.Style != nil {
				if border, ok := box.Style.Get("border"); ok {
					fmt.Printf("  CSS border: %s\n", border)
				}
				if borderStyle, ok := box.Style.Get("border-style"); ok {
					fmt.Printf("  CSS border-style: %s\n", borderStyle)
				}
				if borderWidth, ok := box.Style.Get("border-width"); ok {
					fmt.Printf("  CSS border-width: %s\n", borderWidth)
				}
				if borderColor, ok := box.Style.Get("border-color"); ok {
					fmt.Printf("  CSS border-color: %s\n", borderColor)
				}
			}
			
			fmt.Printf("  Expected size with 1in borders: 192x192\n\n")
		}
	}
	
	for _, child := range box.Children {
		findTestDivs(child, depth+1)
	}
}
