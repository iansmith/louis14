package images

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"bytes"
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
