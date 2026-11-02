package html

import "testing"

// An SVG document's document element is its root <svg>: no implied
// html/head/body, and any element may self-close, as Blink's XML document
// parser builds an SVGImage's document (SVGImage::DataChanged, svg_image.cc
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestParseSVGDocument_RootIsSVG(t *testing.T) {
	doc, err := ParseSVGDocument(`<?xml version="1.0"?>
<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">
  <rect width="5" height="5"/>
  <g><circle r="2"/></g>
</svg>`)
	if err != nil {
		t.Fatal(err)
	}
	var elems []*Node
	for _, c := range doc.Root.Children {
		if c.Type == ElementNode {
			elems = append(elems, c)
		}
	}
	if len(elems) != 1 || elems[0].TagName != "svg" || elems[0].NamespaceURI() != SVGNamespaceURI {
		t.Fatalf("document children = %v, want exactly the SVG-namespace <svg>", elems)
	}
	svg := elems[0]
	var kids []string
	for _, c := range svg.Children {
		if c.Type == ElementNode {
			kids = append(kids, c.TagName)
		}
	}
	if len(kids) != 2 || kids[0] != "rect" || kids[1] != "g" {
		t.Errorf("<svg> element children = %v, want [rect g] (self-closing <rect/> has no children)", kids)
	}
}

// An XML document has no HTML foreign-content breakout: an HTML-named
// element inside the SVG stays there, in the SVG namespace (#354 review).
func TestParseSVGDocument_NoForeignContentBreakout(t *testing.T) {
	doc, err := ParseSVGDocument(`<svg xmlns="http://www.w3.org/2000/svg"><g><b>x</b><p/><font>y</font><rect/></g></svg>`)
	if err != nil {
		t.Fatal(err)
	}
	var g *Node
	for _, c := range doc.Root.Children {
		if c.Type == ElementNode && c.TagName == "svg" && len(c.Children) > 0 {
			g = c.Children[0]
		}
	}
	if g == nil || g.TagName != "g" {
		t.Fatalf("no <g> under the root <svg>")
	}
	var kids []string
	for _, c := range g.Children {
		if c.Type == ElementNode {
			kids = append(kids, c.TagName)
			if c.NamespaceURI() != SVGNamespaceURI {
				t.Errorf("<%s> namespace = %q, want SVG", c.TagName, c.NamespaceURI())
			}
		}
	}
	if len(kids) != 4 {
		t.Errorf("<g> children = %v, want [b p font rect]", kids)
	}
}
