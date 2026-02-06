package main

import (
	"fmt"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"os"
)

func main() {
	content, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/box-display/block-in-inline-003.xht")
	doc, _ := html.Parse(string(content))

	// Get the inline div
	var inlineDiv *html.Node
	var findInlineDiv func(*html.Node)
	findInlineDiv = func(n *html.Node) {
		if n.TagName == "div" {
			if class, ok := n.Attributes["class"]; ok && class == "inline" {
				inlineDiv = n
				return
			}
		}
		for _, child := range n.Children {
			findInlineDiv(child)
		}
	}
	findInlineDiv(doc)

	if inlineDiv == nil {
		fmt.Println("Could not find inline div")
		return
	}

	// Compute styles
	styles := css.NewCascadedStyleMap()
	engine := layout.NewLayoutEngine(400, 400)
	engine.ComputeStyles(doc, styles)

	style := styles.Get(inlineDiv)
	fmt.Printf("div.inline styles:\n")
	fmt.Printf("  display: %v\n", style.GetDisplay())
	fmt.Printf("  background: %v\n", style.GetBackgroundColor())
	fmt.Printf("  color: %v\n", style.GetColor())
	fmt.Printf("  font-size: %.1f\n", style.GetFontSize())
	fmt.Printf("  line-height: %.1f\n", style.GetLineHeight())
	fmt.Printf("  padding: %+v\n", style.GetPadding())
	fmt.Printf("  margin: %+v\n", style.GetMargin())
	fmt.Printf("  border: %+v\n", style.GetBorderWidth())
}
