package images

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// createTestPNGDataURI creates a small 2x2 red PNG as a data URI.
func createTestPNGDataURI() string {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	red := color.RGBA{255, 0, 0, 255}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	return "data:image/png;base64," + encoded
}

func TestIsDataURI(t *testing.T) {
	if !IsDataURI("data:image/png;base64,abc") {
		t.Error("expected true for data URI")
	}
	if IsDataURI("/path/to/file.png") {
		t.Error("expected false for file path")
	}
	if IsDataURI("") {
		t.Error("expected false for empty string")
	}
}

func TestLoadImageFromDataURI(t *testing.T) {
	uri := createTestPNGDataURI()
	img, err := LoadImageFromDataURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Errorf("expected 2x2 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestLoadImageFromDataURI_Invalid(t *testing.T) {
	tests := []string{
		"not-a-data-uri",
		"data:image/png;base64", // no comma
		"data:image/png;base64,!!!invalid-base64!!!",
		"data:image/png;base64,aGVsbG8=", // valid base64 but not an image
	}
	for _, uri := range tests {
		_, err := LoadImageFromDataURI(uri)
		if err == nil {
			t.Errorf("expected error for %q", uri)
		}
	}
}

func TestLoadImage_DataURI(t *testing.T) {
	uri := createTestPNGDataURI()
	img, err := LoadImage(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Errorf("expected 2x2 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Second call should hit cache
	img2, err := LoadImage(uri)
	if err != nil {
		t.Fatalf("unexpected error on cached load: %v", err)
	}
	if img != img2 {
		t.Error("expected cached image to be the same pointer")
	}
}

func TestGetImageDimensions_DataURI(t *testing.T) {
	uri := createTestPNGDataURI()
	w, h, err := GetImageDimensions(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 2 || h != 2 {
		t.Errorf("expected 2x2, got %dx%d", w, h)
	}
}

// TestImageIsPotentiallyAvailable mirrors ImageLoader::ImageIsPotentiallyAvailable
// (image_loader.cc:284-310 @ d4ecdfed88f962439247c2ad36b8fe47805b1520): an
// image that loaded with dimensions is available; one that failed is not; an
// empty source URL never is. With no fetcher, louis14 makes no load attempt,
// so a non-data: source is treated like Blink's "still loading" state.
func TestImageIsPotentiallyAvailable(t *testing.T) {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	pngData := pngBuf.Bytes()
	const svgData = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="5"></svg>`
	// No width/height: no natural size, only a ratio, but the image loaded.
	const viewBoxOnlySVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 5"></svg>`
	fetcher := func(uri string) ([]byte, error) {
		switch uri {
		case "ok.png":
			return pngData, nil
		case "ok.svg":
			return []byte(svgData), nil
		case "viewbox.svg":
			return []byte(viewBoxOnlySVG), nil
		}
		return nil, fmt.Errorf("404: %s", uri)
	}
	validDataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData)
	cases := []struct {
		name    string
		src     string
		fetcher ImageFetcher
		want    bool
	}{
		{"empty src", "", fetcher, false},
		{"empty src, no fetcher", "", nil, false},
		{"no fetcher, still loading", "a.png", nil, true},
		{"valid data: URI", validDataURI, nil, true},
		{"corrupt data: URI", "data:image/png;base64,AAAA", nil, false},
		{"fetch error", "missing.png", fetcher, false},
		{"valid png", "ok.png", fetcher, true},
		{"valid svg", "ok.svg", fetcher, true},
		{"viewBox-only svg", "viewbox.svg", fetcher, true},
		{"svg fetch error", "missing.svg", fetcher, false},
	}
	for _, c := range cases {
		if got := ImageIsPotentiallyAvailable(c.src, c.fetcher); got != c.want {
			t.Errorf("%s: ImageIsPotentiallyAvailable(%q) = %v, want %v", c.name, c.src, got, c.want)
		}
	}
}
