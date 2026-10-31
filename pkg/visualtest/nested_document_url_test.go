package visualtest

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestRenderHTMLToFileWithBase_NestedDocumentImgResolvesAgainstOwnBase pins
// that an <img> inside a nested document in a subdirectory paints the image
// from that document's own directory, while the outer page's same-named
// image still paints from the outer directory. Expected values follow Blink:
// ImageLoader::ImageSourceToKURL completes src against the element's own
// document (third_party/blink/renderer/core/loader/image_loader.cc:746 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
//
// The outer <img> comes first and uses the same relative name, so it primes
// the process-global image cache with the red decoy; an uncompleted nested
// lookup then paints red. Names carry a per-run nonce so runs and tests
// cannot share cache entries.
func TestRenderHTMLToFileWithBase_NestedDocumentImgResolvesAgainstOwnBase(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 128, 0, 255}
	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)

	writePNG := func(t *testing.T, p string, c color.RGBA) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 40, 40))
		draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
		saveTestImage(t, img, p)
	}
	writeFile := func(t *testing.T, p, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// render renders outer from dir and returns the output image.
	render := func(t *testing.T, dir, outer string) image.Image {
		t.Helper()
		out := filepath.Join(dir, "out.png")
		if err := RenderHTMLToFileWithBase(outer, out, 200, 200, dir); err != nil {
			t.Fatalf("render: %v", err)
		}
		f, err := os.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	assertPixel := func(t *testing.T, img image.Image, x, y int, want color.RGBA, what string) {
		t.Helper()
		r, g, b, a := img.At(x, y).RGBA()
		got := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
		if got != want {
			t.Errorf("%s: pixel (%d,%d) = %v, want %v", what, x, y, got, want)
		}
	}
	const frameStyle = `<!doctype html><style>html,body{margin:0}img,iframe{display:block}iframe{border:0;width:100px;height:100px}</style>`

	t.Run("depth1", func(t *testing.T) {
		dir := t.TempDir()
		name := "lou441-p1-" + nonce + ".png"
		writePNG(t, filepath.Join(dir, name), red)
		writePNG(t, filepath.Join(dir, "support", name), green)
		writeFile(t, filepath.Join(dir, "support", "frame.html"), frameStyle+`<img src="`+name+`">`)

		img := render(t, dir, frameStyle+`<img src="`+name+`"><iframe src="support/frame.html"></iframe>`)
		// Outer img at y 0..40; iframe at y 40..140, its img at y 40..80.
		assertPixel(t, img, 20, 20, red, "outer img (decoy reachable)")
		assertPixel(t, img, 20, 60, green, "nested img")
	})

	t.Run("depth2", func(t *testing.T) {
		dir := t.TempDir()
		name := "lou441-p2-" + nonce + ".png"
		writePNG(t, filepath.Join(dir, name), red)
		writePNG(t, filepath.Join(dir, "a", name), red)
		writePNG(t, filepath.Join(dir, "a", "b", name), green)
		writeFile(t, filepath.Join(dir, "a", "frame.html"), frameStyle+`<iframe src="b/inner.html"></iframe>`)
		writeFile(t, filepath.Join(dir, "a", "b", "inner.html"), frameStyle+`<img src="`+name+`">`)

		img := render(t, dir, frameStyle+`<img src="`+name+`"><iframe src="a/frame.html"></iframe>`)
		// Outer img at y 0..40; a/frame.html at y 40..140; its iframe at
		// y 40..140, whose img is at y 40..80.
		assertPixel(t, img, 20, 20, red, "outer img (decoy reachable)")
		assertPixel(t, img, 20, 60, green, "depth-2 nested img")
	})
}
