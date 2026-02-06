package main

import (
	"fmt"
	"image/png"
	"os"
)

func main() {
	analyzeImage("output/reftests/block-in-inline-003_test.png", "TEST")
	fmt.Println()
	analyzeImage("output/reftests/block-in-inline-003_ref.png", "REFERENCE")
}

func analyzeImage(path, label string) {
	f, _ := os.Open(path)
	if f == nil {
		fmt.Printf("%s: Could not open\n", label)
		return
	}
	defer f.Close()

	img, _ := png.Decode(f)
	if img == nil {
		return
	}

	bounds := img.Bounds()

	// Count colors
	green, white, other := 0, 0, 0
	firstGreenX, firstGreenY := -1, -1

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)

			if g8 > 100 && r8 < 50 && b8 < 50 {
				green++
				if firstGreenX == -1 {
					firstGreenX, firstGreenY = x, y
				}
			} else if r8 > 250 && g8 > 250 && b8 > 250 {
				white++
			} else {
				other++
				if other <= 5 {
					fmt.Printf("  Non-white/green at (%d,%d): RGB(%d,%d,%d)\n", x, y, r8, g8, b8)
				}
			}
		}
	}

	total := green + white + other
	fmt.Printf("%s: %d pixels total\n", label, total)
	fmt.Printf("  Green: %d (%.1f%%)  First at (%d, %d)\n", green, float64(green)/float64(total)*100, firstGreenX, firstGreenY)
	fmt.Printf("  White: %d (%.1f%%)\n", white, float64(white)/float64(total)*100)
	fmt.Printf("  Other: %d (%.1f%%)\n", other, float64(other)/float64(total)*100)
}
