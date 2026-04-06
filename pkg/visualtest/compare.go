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
	Match           bool
	DifferentPixels int
	TotalPixels     int
	MaxDifference   int // Max color channel difference found
}

// CompareOptions configures the image comparison
type CompareOptions struct {
	// Tolerance: maximum allowed difference per color channel (0-255)
	// Recommended: 2-5 for rendering differences, 0 for exact match
	Tolerance int

	// FuzzyRadius: if > 0, a pixel matches if it matches any pixel within this radius
	// Useful for tests with small positional differences (e.g., 1-2px text shifts)
	// Recommended: 1-2 for text positioning tolerance, 0 for exact positioning
	FuzzyRadius int

	// MaxDifferentPercent: if > 0, pass if the percentage of different pixels is <= this value
	// Useful for tests with minor anti-aliasing or positioning differences
	// Recommended: 0.1-0.5 for small differences, 0 for exact match
	MaxDifferentPercent float64

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
				// If fuzzy matching is enabled, check nearby pixels
				matched := false
				if opts.FuzzyRadius > 0 {
					matched = fuzzyMatch(actualImg, expectedImg, x, y, opts.FuzzyRadius, opts.Tolerance, actualBounds)
				}

				if !matched {
					result.Match = false
					result.DifferentPixels++

					if diffImg != nil {
						// Highlight difference in red
						diffImg.Set(x, y, color.RGBA{255, 0, 0, 255})
					}
				} else if diffImg != nil {
					// Fuzzy matched - show in grayscale
					gray := uint8(ar)
					diffImg.Set(x, y, color.RGBA{gray, gray, gray, 255})
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

	// Check if percentage of different pixels is acceptable
	if !result.Match && opts.MaxDifferentPercent > 0 {
		pct := float64(result.DifferentPixels) / float64(result.TotalPixels) * 100
		if pct <= opts.MaxDifferentPercent {
			result.Match = true
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

// fuzzyMatch checks if the actual pixel at (x, y) matches any expected pixel within radius
func fuzzyMatch(actual, expected image.Image, x, y, radius, tolerance int, bounds image.Rectangle) bool {
	ar, ag, ab, aa := actual.At(x, y).RGBA()
	ar, ag, ab, aa = ar>>8, ag>>8, ab>>8, aa>>8

	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			nx, ny := x+dx, y+dy
			if nx < bounds.Min.X || nx >= bounds.Max.X || ny < bounds.Min.Y || ny >= bounds.Max.Y {
				continue
			}

			er, eg, eb, ea := expected.At(nx, ny).RGBA()
			er, eg, eb, ea = er>>8, eg>>8, eb>>8, ea>>8

			diff := maxInt(
				absInt(int(ar)-int(er)),
				absInt(int(ag)-int(eg)),
				absInt(int(ab)-int(eb)),
				absInt(int(aa)-int(ea)),
			)

			if diff <= tolerance {
				return true
			}
		}
	}
	return false
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

// EdgeLocalityResult contains results of edge-locality validation.
type EdgeLocalityResult struct {
	HighDiffPixels int // pixels exceeding baseTolerance
	OnEdgePixels   int // of those, on color-transition boundaries
	InteriorPixels int // of those, NOT on boundaries (potential bugs)
}

// ValidateEdgeLocality checks whether pixels that differ between two images
// are located on color-transition boundaries. Anti-aliasing differences only
// appear at edges between distinct color regions; diffs in flat interior regions
// indicate rendering bugs rather than AA variation.
//
// baseTolerance: channel diff above which a pixel is considered "different" (e.g. 2).
// edgeThreshold: min neighbor gradient to qualify as an edge pixel (e.g. 10).
func ValidateEdgeLocality(actualPath, expectedPath string, baseTolerance, edgeThreshold int) (*EdgeLocalityResult, error) {
	actualFile, err := os.Open(actualPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open actual image: %w", err)
	}
	defer actualFile.Close()
	actualImg, err := png.Decode(actualFile)
	if err != nil {
		return nil, fmt.Errorf("failed to decode actual image: %w", err)
	}

	expectedFile, err := os.Open(expectedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open expected image: %w", err)
	}
	defer expectedFile.Close()
	expectedImg, err := png.Decode(expectedFile)
	if err != nil {
		return nil, fmt.Errorf("failed to decode expected image: %w", err)
	}

	bounds := actualImg.Bounds()
	result := &EdgeLocalityResult{}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ar, ag, ab, aa := actualImg.At(x, y).RGBA()
			er, eg, eb, ea := expectedImg.At(x, y).RGBA()
			ar, ag, ab, aa = ar>>8, ag>>8, ab>>8, aa>>8
			er, eg, eb, ea = er>>8, eg>>8, eb>>8, ea>>8

			diff := maxInt(
				absInt(int(ar)-int(er)),
				absInt(int(ag)-int(eg)),
				absInt(int(ab)-int(eb)),
				absInt(int(aa)-int(ea)),
			)
			if diff <= baseTolerance {
				continue
			}

			result.HighDiffPixels++

			// Check if this pixel is on a color boundary in EITHER image.
			// AA pixels sit where colors transition, so at least one neighbor
			// should differ significantly from the center.
			if isOnEdge(actualImg, x, y, bounds, edgeThreshold) ||
				isOnEdge(expectedImg, x, y, bounds, edgeThreshold) {
				result.OnEdgePixels++
			} else {
				result.InteriorPixels++
			}
		}
	}

	return result, nil
}

// isOnEdge returns true if pixel (x,y) has at least one 8-connected neighbor
// whose max channel difference from (x,y) is >= threshold.
func isOnEdge(img image.Image, x, y int, bounds image.Rectangle, threshold int) bool {
	cr, cg, cb, ca := img.At(x, y).RGBA()
	cr, cg, cb, ca = cr>>8, cg>>8, cb>>8, ca>>8

	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx < bounds.Min.X || nx >= bounds.Max.X || ny < bounds.Min.Y || ny >= bounds.Max.Y {
				continue
			}
			nr, ng, nb, na := img.At(nx, ny).RGBA()
			nr, ng, nb, na = nr>>8, ng>>8, nb>>8, na>>8

			grad := maxInt(
				absInt(int(cr)-int(nr)),
				absInt(int(cg)-int(ng)),
				absInt(int(cb)-int(nb)),
				absInt(int(ca)-int(na)),
			)
			if grad >= threshold {
				return true
			}
		}
	}
	return false
}
