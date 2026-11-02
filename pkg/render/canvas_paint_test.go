package render_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/js"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

// renderScripted lays out, runs the document's scripts, lays out again and
// paints, as the reftest harness does.
func renderScripted(t *testing.T, src string, w, h int) (*image.RGBA, []*layout.Box) {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	layout.NewLayoutEngine(float64(w), float64(h)).Layout(doc)
	if err := js.New().Execute(doc); err != nil {
		t.Fatal(err)
	}
	boxes := layout.NewLayoutEngine(float64(w), float64(h)).Layout(doc)
	target := image.NewRGBA(image.Rect(0, 0, w, h))
	render.NewRendererForImage(target).Render(boxes)
	return target, boxes
}

func wantPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, why string) {
	t.Helper()
	if got := img.RGBAAt(x, y); got != want {
		t.Errorf("pixel (%d,%d) = %v, want %v: %s", x, y, got, want, why)
	}
}

var (
	canvasWhite = color.RGBA{255, 255, 255, 255}
	canvasGreen = color.RGBA{0, 128, 0, 255}
	canvasRed   = color.RGBA{255, 0, 0, 255}
)

// fillRect fills the canvas bitmap with fillStyle
// (Canvas2DRecorderContext::fillRect @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and HTMLCanvasPainter paints
// the bitmap into the replaced content rect (object-fit: fill scales the
// 20×10 bitmap to the 40×20 box).
func TestCanvas_FillRectPainted(t *testing.T) {
	img, _ := renderScripted(t, `<!DOCTYPE html><body style="margin:0">
		<canvas id="c" width="20" height="10" style="display:block; width:40px; height:20px; image-rendering:pixelated"></canvas>
		<script>
			var ctx = document.getElementById("c").getContext("2d");
			ctx.fillStyle = "rgb(0, 128, 0)";
			ctx.fillRect(0, 0, 10, 10);
		</script>`, 60, 30)
	wantPixel(t, img, 5, 10, canvasGreen, "left half of the canvas is filled")
	wantPixel(t, img, 30, 10, canvasWhite, "right half of the canvas is clear")
}

// Assigning width resets the canvas: the bitmap is cleared
// (HTMLCanvasElement::OnWidthOrHeightAssigned).
func TestCanvas_WidthAssignmentClears(t *testing.T) {
	img, _ := renderScripted(t, `<!DOCTYPE html><body style="margin:0">
		<canvas id="c" width="20" height="10" style="display:block"></canvas>
		<script>
			var c = document.getElementById("c");
			var ctx = c.getContext("2d");
			ctx.fillStyle = "red";
			ctx.fillRect(0, 0, 20, 10);
			c.width = 20;
			if (ctx.fillStyle !== "#000000") throw new Error("fillStyle not reset: " + ctx.fillStyle);
		</script>`, 40, 20)
	wantPixel(t, img, 5, 5, canvasWhite, "the bitmap was cleared")
}

// drawImage(image, dx, dy) draws a loaded <img> at its natural size, here
// from a new Image()'s load handler as the css-images *c tests do.
func TestCanvas_DrawImageFromImageLoad(t *testing.T) {
	var buf bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range src.Pix {
		src.Pix[i] = []uint8{255, 0, 0, 255}[i%4]
	}
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	img, _ := renderScripted(t, `<!DOCTYPE html><body style="margin:0">
		<canvas id="c" width="8" height="8" style="display:block"></canvas>
		<script>
			var image = new Image();
			image.onload = function() {
				document.getElementById("c").getContext("2d").drawImage(image, 2, 2);
			};
			image.src = "`+uri+`";
		</script>`, 20, 20)
	wantPixel(t, img, 3, 3, canvasRed, "the image is drawn at (2,2)")
	wantPixel(t, img, 1, 1, canvasWhite, "outside the image is clear")
	wantPixel(t, img, 6, 6, canvasWhite, "the image is 4×4")
}

// A canvas's width / height map to `aspect-ratio: auto w / h`
// (ApplyIntegerAspectRatioToStyle), so the natural ratio object-view-box
// gives (50×100 cropped to 50×50) still sizes it.
func TestCanvas_ObjectViewBoxSizesCanvas(t *testing.T) {
	_, boxes := renderScripted(t, `<!DOCTYPE html><body style="margin:0">
		<canvas width="50" height="100" style="object-view-box: inset(50px 0px 0px 0px)"></canvas>`, 200, 200)
	var found *layout.Box
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b.Node != nil && b.Node.TagName == "canvas" {
			found = b
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range boxes {
		walk(b)
	}
	if found == nil || found.Width != 50 || found.Height != 50 {
		t.Fatalf("canvas box = %+v, want 50×50", found)
	}
}
