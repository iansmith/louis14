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
	first := layout.NewLayoutEngine(float64(w), float64(h))
	first.Layout(doc)
	eng := js.New()
	eng.SetLayoutSnapshot(first.ComputedStyles(), first.NodeBoxes())
	if err := eng.Execute(doc); err != nil {
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
	canvasWhite  = color.RGBA{255, 255, 255, 255}
	canvasGreen  = color.RGBA{0, 128, 0, 255}
	canvasRed    = color.RGBA{255, 0, 0, 255}
	canvasYellow = color.RGBA{255, 255, 0, 255}
	canvasOrange = color.RGBA{255, 165, 0, 255}
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

// drawImage of an <img> whose request has not completed draws nothing
// (HTMLImageElement::GetSourceImageForCanvas: not "fully decodable"); the
// image completes when its load task runs (#357 review).
func TestCanvas_DrawImageBeforeLoadDrawsNothing(t *testing.T) {
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
			image.src = "`+uri+`";
			document.getElementById("c").getContext("2d").drawImage(image, 0, 0);
		</script>`, 20, 20)
	wantPixel(t, img, 1, 1, canvasWhite, "the image had not loaded when it was drawn")
}

// drawImage of a canvas onto itself reads a snapshot of the source, not the
// pixels it is writing (#357 review).
func TestCanvas_DrawImageOntoItself(t *testing.T) {
	img, _ := renderScripted(t, `<!DOCTYPE html><body style="margin:0">
		<canvas id="c" width="4" height="1" style="display:block; width:40px; height:10px; image-rendering:pixelated"></canvas>
		<script>
			var c = document.getElementById("c"), ctx = c.getContext("2d");
			ctx.fillStyle = "red";
			ctx.fillRect(0, 0, 1, 1);
			ctx.drawImage(c, 1, 0);
		</script>`, 50, 20)
	wantPixel(t, img, 5, 5, canvasRed, "the original pixel")
	wantPixel(t, img, 15, 5, canvasRed, "the copy one pixel right")
	wantPixel(t, img, 25, 5, canvasWhite, "no smear from reading written pixels")
}

// drawImage with an argument count no overload takes throws a TypeError
// (WebIDL overload resolution) (#357 review).
func TestCanvas_DrawImageBadArityThrows(t *testing.T) {
	renderScripted(t, `<!DOCTYPE html><body>
		<canvas id="c" width="4" height="4"></canvas>
		<script>
			var c = document.getElementById("c"), ctx = c.getContext("2d");
			[[c], [c, 0], [c, 0, 0, 1], [c, 0, 0, 1, 1, 0], [c, 0, 0, 1, 1, 0, 0], [c, 0, 0, 1, 1, 0, 0, 1]].forEach(function(args) {
				var threw = false;
				try { ctx.drawImage.apply(ctx, args); } catch (e) { threw = e instanceof TypeError; }
				if (!threw) throw new Error("no TypeError for " + args.length + " arguments");
			});
		</script>`, 20, 20)
}

// fillStyle = "currentcolor" resolves against the canvas element's computed
// color (CanvasRenderingContext2D's ParseColorOrCurrentColor) (#357 review).
func TestCanvas_FillStyleCurrentColor(t *testing.T) {
	img, _ := renderScripted(t, `<!DOCTYPE html><body style="margin:0">
		<canvas id="c" width="4" height="4" style="display:block; color: rgb(0, 128, 0)"></canvas>
		<script>
			var ctx = document.getElementById("c").getContext("2d");
			ctx.fillStyle = "currentColor";
			if (ctx.fillStyle !== "#008000") throw new Error("fillStyle = " + ctx.fillStyle);
			ctx.fillRect(0, 0, 4, 4);
		</script>`, 20, 20)
	wantPixel(t, img, 1, 1, canvasGreen, "filled with the canvas's color")
}
