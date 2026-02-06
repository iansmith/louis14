package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type TestResult struct {
	Name          string
	DifferingPixels int
	TotalPixels   int
	ErrorPercent  float64
}

func main() {
	results := []TestResult{}

	files, _ := filepath.Glob("output/reftests/*_diff.png")

	for _, diffPath := range files {
		// Extract test name
		base := filepath.Base(diffPath)
		name := strings.TrimSuffix(base, "_diff.png")

		// Count differing pixels (non-zero pixels in diff image)
		f, err := os.Open(diffPath)
		if err != nil {
			continue
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			continue
		}

		bounds := img.Bounds()
		total := (bounds.Max.X - bounds.Min.X) * (bounds.Max.Y - bounds.Min.Y)
		differing := 0

		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				// Any non-black pixel is a difference
				if r > 0 || g > 0 || b > 0 {
					differing++
				}
			}
		}

		errorPercent := float64(differing) / float64(total) * 100

		results = append(results, TestResult{
			Name:          name,
			DifferingPixels: differing,
			TotalPixels:   total,
			ErrorPercent:  errorPercent,
		})
	}

	// Sort by error percentage descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].ErrorPercent > results[j].ErrorPercent
	})

	fmt.Printf("Test Results (sorted by error %%):\n\n")
	for i, r := range results {
		fmt.Printf("%2d. %-50s %6d / %6d pixels (%.1f%%)\n",
			i+1, r.Name, r.DifferingPixels, r.TotalPixels, r.ErrorPercent)
	}

	passCount := 0
	for _, r := range results {
		if r.ErrorPercent == 0 {
			passCount++
		}
	}

	fmt.Printf("\nSummary: %d/%d tests passing (%.1f%%)\n",
		51-len(results), 51, float64(51-len(results))/51*100)
}
