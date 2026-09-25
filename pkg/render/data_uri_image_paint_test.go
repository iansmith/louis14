package render

// Fetcher-less data: URI image painting (PR #180).
//
// data: URIs decode inline (images.LoadImageFromDataURI), so a Renderer with
// no ImageFetcher — NewRendererForImage, resource.NewWebEngine — must still
// paint them. These tests pin each of the three painters that used to bail
// on a nil fetcher: backgrounds, <img>, and border-image.

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// solidRedPNGDataURI returns a data: URI for a 4x4 opaque red PNG.
func solidRedPNGDataURI(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func assertRedAt(t *testing.T, img *image.RGBA, x, y int) {
	t.Helper()
	if r, g, b := rgbAt(img, x, y); r != 255 || g != 0 || b != 0 {
		t.Errorf("pixel (%d,%d) = rgb(%d,%d,%d), want rgb(255,0,0): data: image not painted without a fetcher", x, y, r, g, b)
	}
}

func TestDataURIImage_NoFetcher_Background(t *testing.T) {
	uri := solidRedPNGDataURI(t)
	src := `<!DOCTYPE html><body style="margin:0">
<div style="width:40px; height:40px; background-image:url(` + uri + `)"></div>`
	img := renderColumnRuleHTML(t, src, 100, 100)
	assertRedAt(t, img, 20, 20)
}

func TestDataURIImage_NoFetcher_Img(t *testing.T) {
	uri := solidRedPNGDataURI(t)
	src := `<!DOCTYPE html><body style="margin:0">
<img src="` + uri + `" style="display:block; width:40px; height:40px">`
	img := renderColumnRuleHTML(t, src, 100, 100)
	assertRedAt(t, img, 20, 20)
}

func TestDataURIImage_NoFetcher_BorderImage(t *testing.T) {
	uri := solidRedPNGDataURI(t)
	src := `<!DOCTYPE html><body style="margin:0">
<div style="width:20px; height:20px; border:10px solid blue; border-image:url(` + uri + `) 1"></div>`
	img := renderColumnRuleHTML(t, src, 100, 100)
	// Middle of the top border: border-image replaces the blue border.
	assertRedAt(t, img, 20, 5)
}
