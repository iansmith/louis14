package layout

import (
	"testing"

	"louis14/pkg/html"
)

// LOU-504: an inline <svg> sized only by its width attribute is laid out in
// its line's fit-content space (Blink's builder default,
// constraint_space.h:1619-1621 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// It has no intrinsic size for fit-content to shrink to, so its used width
// is still the replaced size, 300px, overflowing its 100px container
// (css-transforms skew-test1).
func TestInlineSVGInFitContentSpaceKeepsReplacedWidth_LOU504(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0"><div style="width:100px;height:100px">` +
		`<svg width="300px" height="100px"></svg></div>`
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	var svg *Box
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil && b.Node.TagName == "svg" {
			svg = b
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
		walk(b)
	}
	if svg == nil {
		t.Fatal("no svg box")
	}
	if svg.Width != 300 {
		t.Errorf("svg width = %v, want 300", svg.Width)
	}
}
