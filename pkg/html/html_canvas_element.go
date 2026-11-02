package html

// This file ports the slice of Blink's HTMLCanvasElement
// (core/html/canvas/html_canvas_element.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) that sizes a <canvas> and
// owns its bitmap; the 2D context that draws into it is pkg/js.

// IsHTMLCanvasElement reports whether n is a <canvas> element
// (IsA<HTMLCanvasElement>).
func (n *Node) IsHTMLCanvasElement() bool {
	return n != nil && n.Type == ElementNode && n.TagName == "canvas"
}

// DefaultCanvasWidth / DefaultCanvasHeight are kDefaultCanvasWidth /
// kDefaultCanvasHeight (html_canvas_element.cc:161-162).
const (
	DefaultCanvasWidth  = 300
	DefaultCanvasHeight = 150
)

// CanvasSize is a <canvas>'s bitmap size from its width / height content
// attributes: each a non-negative integer no larger than 0x7fffffff, else
// the default. Mirrors HTMLCanvasElement::OnWidthOrHeightAssigned
// (html_canvas_element.cc:805-830).
func (n *Node) CanvasSize() (width, height int) {
	return canvasDimension(n, "width", DefaultCanvasWidth), canvasDimension(n, "height", DefaultCanvasHeight)
}

func canvasDimension(n *Node, attr string, def int) int {
	value, _ := n.GetAttribute(attr)
	if v, ok := ParseHTMLNonNegativeInteger(value); value != "" && ok && v <= 0x7fffffff {
		return int(v)
	}
	return def
}
