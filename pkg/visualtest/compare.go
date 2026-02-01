package visualtest

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

// CompareResult contains the results of an image comparison
type CompareResult struct {
	Match          bool
	DifferentPixels int
	TotalPixels    int
	MaxDifference  int // Max color channel difference found
}

// CompareOptions configures the image comparison
type CompareOptions struct {
	// Tolerance: maximum allowed difference per color channel (0-255)
	// Recommended: 2-5 for rendering differences, 0 for exact match
	Tolerance int

	// SaveDiffImage: if true, saves a diff image highlighting differences
	SaveDiffImage bool
	DiffImagePath string
}

// DefaultOptions returns sensible defaults for image comparison
func DefaultOptions() CompareOptions {
	return CompareOptions{
		Tolerance:     2, // Allow small rendering differences
		SaveDiffImage: false,
	}
}

// CompareImages compares two PNG images pixel-by-pixel
func CompareImages(actualPath, expectedPath string, opts CompareOptions) (*CompareResult, error) {
	// Load actual image
	actualFile, err := os.Open(actualPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open actual image: %w", err)
	}
	defer actualFile.Close()

	actualImg, err := png.Decode(actualFile)
	if err != nil {
		return nil, fmt.Errorf("failed to decode actual image: %w", err)
	}

	// Load expected image
	expectedFile, err := os.Open(expectedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open expected image: %w", err)
	}
	defer expectedFile.Close()

	expectedImg, err := png.Decode(expectedFile)
	if err != nil {
		return nil, fmt.Errorf("failed to decode expected image: %w", err)
	}

	// Compare dimensions
	actualBounds := actualImg.Bounds()
	expectedBounds := expectedImg.Bounds()
	if actualBounds != expectedBounds {
		return &CompareResult{
			Match: false,
		}, fmt.Errorf("image dimensions differ: actual=%v, expected=%v", actualBounds, expectedBounds)
	}

	// Compare pixels
	result := &CompareResult{
		Match:       true,
		TotalPixels: actualBounds.Dx() * actualBounds.Dy(),
	}

	var diffImg *image.RGBA
	if opts.SaveDiffImage {
		diffImg = image.NewRGBA(actualBounds)
	}

	for y := actualBounds.Min.Y; y < actualBounds.Max.Y; y++ {
		for x := actualBounds.Min.X; x < actualBounds.Max.X; x++ {
			actualColor := actualImg.At(x, y)
			expectedColor := expectedImg.At(x, y)

			ar, ag, ab, aa := actualColor.RGBA()
			er, eg, eb, ea := expectedColor.RGBA()

			// Convert from 16-bit to 8-bit
			ar, ag, ab, aa = ar>>8, ag>>8, ab>>8, aa>>8
			er, eg, eb, ea = er>>8, eg>>8, eb>>8, ea>>8

			// Calculate maximum difference across all channels
			diff := maxInt(
				absInt(int(ar)-int(er)),
				absInt(int(ag)-int(eg)),
				absInt(int(ab)-int(eb)),
				absInt(int(aa)-int(ea)),
			)

			if diff > result.MaxDifference {
				result.MaxDifference = diff
			}

			if diff > opts.Tolerance {
				result.Match = false
				result.DifferentPixels++

				if diffImg != nil {
					// Highlight difference in red
					diffImg.Set(x, y, color.RGBA{255, 0, 0, 255})
				}
			} else {
				if diffImg != nil {
					// Same pixel - show in grayscale
					gray := uint8(ar) // Use actual image as base
					diffImg.Set(x, y, color.RGBA{gray, gray, gray, 255})
				}
			}
		}
	}

	// Save diff image if requested
	if opts.SaveDiffImage && !result.Match && opts.DiffImagePath != "" {
		if err := savePNG(diffImg, opts.DiffImagePath); err != nil {
			return result, fmt.Errorf("failed to save diff image: %w", err)
		}
	}

	return result, nil
}

// savePNG saves an image as PNG
func savePNG(img image.Image, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return png.Encode(file, img)
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func maxInt(vals ...int) int {
	if len(vals) == 0 {
		return 0
	}
	max := vals[0]
	for _, v := range vals[1:] {
		if v > max {
			max = v
		}
	}
	return max
}
