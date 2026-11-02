package layout

import (
	"encoding/base64"
	"testing"

	"louis14/pkg/html"
)

// A size-contained replaced element takes its contain-intrinsic-size in each
// contained axis instead of 0 (LayoutInputNode::GetOverrideIntrinsicSize,
// layout_input_node.cc:236-253; LayoutBox::OverrideIntrinsicContentInlineSize
// / BlockSize, layout_box.cc:1271-1340 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestReplacedContainIntrinsicSize(t *testing.T) {
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(fakeImagePNG(40, 20))
	for _, tc := range []struct {
		name          string
		html          string
		width, height float64
	}{
		{"size contained, no override", `<img id="t" src="` + png + `" style="contain:size">`, 0, 0},
		{"longhands", `<img id="t" src="` + png + `" style="contain:size; contain-intrinsic-width:32px; contain-intrinsic-height:48px">`, 32, 48},
		{"shorthand two values", `<img id="t" src="` + png + `" style="contain:size; contain-intrinsic-size:50px 100px">`, 50, 100},
		{"shorthand one value", `<img id="t" src="` + png + `" style="contain:size; contain-intrinsic-size:8px">`, 8, 8},
		{"auto length", `<img id="t" src="` + png + `" style="contain:size; contain-intrinsic-size:auto 10px auto 12px">`, 10, 12},
		{"not contained ignores it", `<img id="t" src="` + png + `" style="contain-intrinsic-size:50px 100px">`, 40, 20},
		{"canvas", `<canvas id="t" style="contain:size; contain-intrinsic-size:30px 20px"></canvas>`, 30, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse(`<!DOCTYPE html><body style="margin:0">` + tc.html)
			if err != nil {
				t.Fatal(err)
			}
			var target *Box
			var walk func(b *Box)
			walk = func(b *Box) {
				if b.Node != nil {
					if id, _ := b.Node.GetAttribute("id"); id == "t" && target == nil {
						target = b
					}
				}
				for _, c := range b.Children {
					walk(c)
				}
			}
			for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
				walk(b)
			}
			if target == nil {
				t.Fatal("no #t box")
			}
			if target.Width != tc.width || target.Height != tc.height {
				t.Errorf("border-box = %vx%v, want %vx%v", target.Width, target.Height, tc.width, tc.height)
			}
		})
	}
}
