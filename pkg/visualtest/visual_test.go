package visualtest

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareImages_Identical(t *testing.T) {
	// Create a simple test image
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255}) // Red
		}
	}

	// Save it twice
	tmpDir := t.TempDir()
	path1 := filepath.Join(tmpDir, "img1.png")
	path2 := filepath.Join(tmpDir, "img2.png")

	saveTestImage(t, img, path1)
	saveTestImage(t, img, path2)

	// Compare
	result, err := CompareImages(path1, path2, DefaultOptions())
	if err != nil {
		t.Fatalf("comparison failed: %v", err)
	}

	if !result.Match {
		t.Errorf("expected images to match")
	}
	if result.DifferentPixels != 0 {
		t.Errorf("expected 0 different pixels, got %d", result.DifferentPixels)
	}
}

func TestCompareImages_Different(t *testing.T) {
	tmpDir := t.TempDir()

	// Create image 1 (red)
	img1 := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img1.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	path1 := filepath.Join(tmpDir, "img1.png")
	saveTestImage(t, img1, path1)

	// Create image 2 (blue)
	img2 := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img2.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	path2 := filepath.Join(tmpDir, "img2.png")
	saveTestImage(t, img2, path2)

	// Compare
	opts := DefaultOptions()
	opts.SaveDiffImage = true
	opts.DiffImagePath = filepath.Join(tmpDir, "diff.png")

	result, err := CompareImages(path1, path2, opts)
	if err != nil {
		t.Fatalf("comparison failed: %v", err)
	}

	if result.Match {
		t.Errorf("expected images to not match")
	}
	if result.DifferentPixels != 100 {
		t.Errorf("expected 100 different pixels, got %d", result.DifferentPixels)
	}

	// Verify diff image was created
	if _, err := os.Stat(opts.DiffImagePath); os.IsNotExist(err) {
		t.Errorf("diff image was not created")
	}
}

func TestCompareImages_WithTolerance(t *testing.T) {
	tmpDir := t.TempDir()

	// Create image 1
	img1 := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img1.Set(x, y, color.RGBA{100, 100, 100, 255})
		}
	}
	path1 := filepath.Join(tmpDir, "img1.png")
	saveTestImage(t, img1, path1)

	// Create image 2 (slightly different - 2 points off)
	img2 := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img2.Set(x, y, color.RGBA{102, 102, 102, 255})
		}
	}
	path2 := filepath.Join(tmpDir, "img2.png")
	saveTestImage(t, img2, path2)

	// Compare with tolerance=2 (should match)
	opts := DefaultOptions()
	opts.Tolerance = 2
	result, err := CompareImages(path1, path2, opts)
	if err != nil {
		t.Fatalf("comparison failed: %v", err)
	}
	if !result.Match {
		t.Errorf("expected images to match with tolerance=2")
	}

	// Compare with tolerance=0 (should not match)
	opts.Tolerance = 0
	result, err = CompareImages(path1, path2, opts)
	if err != nil {
		t.Fatalf("comparison failed: %v", err)
	}
	if result.Match {
		t.Errorf("expected images to not match with tolerance=0")
	}
}

func TestCompareImages_DifferentDimensions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 10x10 image
	img1 := image.NewRGBA(image.Rect(0, 0, 10, 10))
	path1 := filepath.Join(tmpDir, "img1.png")
	saveTestImage(t, img1, path1)

	// Create 20x20 image
	img2 := image.NewRGBA(image.Rect(0, 0, 20, 20))
	path2 := filepath.Join(tmpDir, "img2.png")
	saveTestImage(t, img2, path2)

	// Compare - should error or return mismatch
	result, err := CompareImages(path1, path2, DefaultOptions())
	if err == nil {
		t.Errorf("expected error for different dimensions")
	}
	if result != nil && result.Match {
		t.Errorf("expected images with different dimensions to not match")
	}
}

// Helper function to save test images
func saveTestImage(t *testing.T, img image.Image, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create image file: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatalf("failed to encode image: %v", err)
	}
}
