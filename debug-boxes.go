package main

import (
	"fmt"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
	"os"
)

func main() {
	content, _ := os.ReadFile("pkg/visualtest/testdata/wpt-css2/box-display/box-generation-001.xht")
	doc, _ := html.Parse(string(content))

	engine := layout.NewLayoutEngine(400, 400)
	computedStyles := css.NewStyleEngine().ComputeStyles(doc.Root, 400, 400)

	rootBox := engine.Layout(doc.Root, 0, 0, 400, computedStyles)

	printAllBoxes(rootBox, 0)
}

func printAllBoxes(box *layout.Box, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	name := "?"
	if box.Node != nil {
		if box.Node.TagName != "" {
			name = "<" + box.Node.TagName + ">"
		} else if box.Node.Type == html.TextNode && box.Node.Text != "" {
			name = fmt.Sprintf("TEXT(%q)", box.Node.Text)
			if len(box.Node.Text) > 20 {
				name = fmt.Sprintf("TEXT(%q...)", box.Node.Text[:20])
			}
		}
	}

	bg := "none"
	if box.Style != nil {
		if bgColor := box.Style.GetBackgroundColor(); bgColor != nil {
			bg = bgColor.String()
		}
	}

	fmt.Printf("%s%s: pos=(%.1f,%.1f) size=(%.1fx%.1f) bg=%s children=%d\n",
		indent, name, box.X, box.Y, box.Width, box.Height, bg, len(box.Children))

	for _, child := range box.Children {
		printAllBoxes(child, depth+1)
	}
}
