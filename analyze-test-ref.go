package main

import (
	"fmt"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"os"
)

func main() {
	// Analyze test HTML
	fmt.Println("=== TEST HTML (box-generation-001.xht) ===")
	testContent, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht")
	testDoc, _ := html.Parse(string(testContent))
	testEngine := layout.NewLayoutEngine(400, 400)
	testBoxes := testEngine.Layout(testDoc)
	findAndPrintDiv1(testBoxes[0], "TEST")

	fmt.Println("\n=== REFERENCE HTML (box-generation-001-ref.xht) ===")
	refContent, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001-ref.xht")
	refDoc, _ := html.Parse(string(refContent))
	refEngine := layout.NewLayoutEngine(400, 400)
	refBoxes := refEngine.Layout(refDoc)
	printAllBoxes(refBoxes[0], 0, "REF")
}

func findAndPrintDiv1(box *layout.Box, label string) {
	if box.Node != nil && box.Node.TagName == "div" {
		if id, ok := box.Node.Attributes["id"]; ok && id == "div1" {
			fmt.Printf("%s div#div1: (%.0f,%.0f) size %.0fx%.0f\n", label, box.X, box.Y, box.Width, box.Height)
			for _, child := range box.Children {
				printBox(child, 1, label)
			}
			return
		}
	}
	for _, child := range box.Children {
		findAndPrintDiv1(child, label)
	}
}

func printAllBoxes(box *layout.Box, depth int, label string) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	name := "?"
	bg := "none"
	if box.Node != nil {
		if box.Node.TagName != "" {
			name = box.Node.TagName
			if id, ok := box.Node.Attributes["id"]; ok {
				name += "#" + id
			}
		} else {
			name = "text"
		}
	}
	// Skip background color for simplicity
	_ = bg

	fmt.Printf("%s%s %s: (%.0f,%.0f) size %.0fx%.0f bg=%s\n",
		indent, label, name, box.X, box.Y, box.Width, box.Height, bg)

	for _, child := range box.Children {
		printAllBoxes(child, depth+1, label)
	}
}

func printBox(box *layout.Box, depth int, label string) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	name := "?"
	bg := "none"
	if box.Node != nil {
		if box.Node.TagName != "" {
			name = box.Node.TagName
			if id, ok := box.Node.Attributes["id"]; ok {
				name += "#" + id
			}
		} else {
			text := box.Node.Text
			if len(text) > 15 {
				text = text[:15] + "..."
			}
			name = fmt.Sprintf("text(%q)", text)
		}
	}
	// Skip background color for simplicity
	_ = bg

	fmt.Printf("%s%s %s: (%.0f,%.0f) size %.0fx%.0f bg=%s\n",
		indent, label, name, box.X, box.Y, box.Width, box.Height, bg)

	for _, child := range box.Children {
		printBox(child, depth+1, label)
	}
}
