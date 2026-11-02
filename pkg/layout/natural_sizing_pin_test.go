package layout

import (
	"encoding/base64"
	"testing"

	"louis14/pkg/html"
)

// TestNaturalSizingPinned_LOU623 pins the border-box geometry the natural
// sizing info produces through every consumer — block (ComputeReplacedSize),
// flex (aspect-ratio transfer, base size, min-size suggestion, stretch),
// grid (natural-size check) and min/max sizing — before LOU-623 moves
// IntrinsicSizingInfo / render.NaturalSizingInfo onto Blink's
// NaturalSizingInfo (core/layout/natural_sizing_info.h:15-42 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The move is a pure refactor:
// every expectation is the value master produced before it.
func TestNaturalSizingPinned_LOU623(t *testing.T) {
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(fakeImagePNG(40, 20))
	for _, tc := range []struct {
		name          string
		html          string
		width, height float64
	}{
		{"img natural", `<img id="t" src="` + png + `">`, 40, 20},
		{"img width only", `<img id="t" src="` + png + `" style="width:100px">`, 100, 50},
		{"img in flex row, width only", `<div style="display:flex"><img id="t" src="` + png + `" style="width:80px"></div>`, 80, 40},
		{"img in flex column, stretch", `<div style="display:flex;flex-direction:column;width:200px"><img id="t" src="` + png + `"></div>`, 200, 100},
		{"img in grid", `<div style="display:grid;grid-template-columns:120px"><img id="t" src="` + png + `"></div>`, 40, 20},
		{"img in inline-block shrink", `<div style="display:inline-block"><img id="t" src="` + png + `" style="height:60px"></div>`, 120, 60},
		{"canvas", `<canvas id="t" width="30" height="10" style="width:90px"></canvas>`, 90, 30},
		{"canvas in flex", `<div style="display:flex;height:50px"><canvas id="t" width="30" height="10"></canvas></div>`, 150, 50},
		{"video width only", `<video id="t" style="width:100px"></video>`, 100, 150},
		{"video in flex", `<div style="display:flex"><video id="t"></video></div>`, 300, 150},
		{"iframe", `<iframe id="t" style="height:40px"></iframe>`, 300, 40},
		{"svg viewBox only", `<div style="width:200px"><svg id="t" viewBox="0 0 16 8" style="display:block"></svg></div>`, 200, 100},
		{"svg width + viewBox", `<svg id="t" width="40" viewBox="0 0 16 8"></svg>`, 40, 20},
		{"svg height only", `<svg id="t" height="40"></svg>`, 300, 40},
		{"svg in flex row", `<div style="display:flex;width:300px"><svg id="t" viewBox="0 0 2 1" style="width:50px"></svg></div>`, 50, 25},
		{"checkbox", `<input id="t" type="checkbox" style="width:30px">`, 30, 30},
		{"img object-view-box", `<img id="t" src="` + png + `" style="object-view-box:inset(0 50% 0 0)">`, 20, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse(`<!DOCTYPE html><body style="margin:0">` + tc.html)
			if err != nil {
				t.Fatalf("html.Parse: %v", err)
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
