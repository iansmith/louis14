package main

import (
	"fmt"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"os"
)

func main() {
	htmlPath := "pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht"
	htmlContent, _ := os.ReadFile(htmlPath)
	doc, _ := html.Parse(string(htmlContent))

	width, height := 400, 300

	// Single-pass
	fmt.Println("=== SINGLE-PASS ===")
	engine1 := layout.NewLayoutEngine(float64(width), float64(height))
	engine1.SetUseMultiPass(false)
	boxes1 := engine1.Layout(doc)
	fmt.Printf("Total boxes: %d\n", countBoxes(boxes1))
	printBoxes(boxes1, 0, "")

	fmt.Println("\n=== MULTI-PASS ===")
	engine2 := layout.NewLayoutEngine(float64(width), float64(height))
	engine2.SetUseMultiPass(true)
	boxes2 := engine2.Layout(doc)
	fmt.Printf("Total boxes: %d\n", countBoxes(boxes2))
	printBoxes(boxes2, 0, "")
}

func countBoxes(boxes []*layout.Box) int {
	count := len(boxes)
	for _, box := range boxes {
		count += countBoxes(box.Children)
	}
	return count
}

func printBoxes(boxes []*layout.Box, depth int, prefix string) {
	for i, box := range boxes {
		indent := ""
		for j := 0; j < depth; j++ {
			indent += "  "
		}
		
		name := "?"
		if box.Node != nil {
			if box.Node.Type == 1 { // TextNode
				text := box.Node.Text
				if len(text) > 20 {
					text = text[:20] + "..."
				}
				name = fmt.Sprintf("TEXT(%q)", text)
			} else if box.Node.Type == 3 { // ElementNode
				name = "<" + box.Node.TagName + ">"
			}
		}
		
		bg := ""
		if box.Style != nil {
			// Check for background
			if val, ok := box.Style.Properties["background-color"]; ok {
				if val != "transparent" && val != "" {
					bg = fmt.Sprintf(" bg=%s", val)
				}
			}
			if val, ok := box.Style.Properties["background"]; ok {
				if val != "transparent" && val != "" && val != "none" {
					bg = fmt.Sprintf(" bg=%s", val)
				}
			}
		}
		
		fmt.Printf("%s[%d] %s: (%.0f,%.0f) %.0fx%.0f%s\n", 
			indent, i, name, box.X, box.Y, box.Width, box.Height, bg)
		
		if len(box.Children) > 0 {
			printBoxes(box.Children, depth+1, fmt.Sprintf("%s[%d]", prefix, i))
		}
	}
}
