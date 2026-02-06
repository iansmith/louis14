package main

import (
	"fmt"
	"image/png"
	"os"
)

func main() {
	analyzeImage("output/reftests/border-005_test.png", "TEST")
	fmt.Println()
	analyzeImage("output/reftests/border-005_ref.png", "REFERENCE")
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
	blue, red, white, other := 0, 0, 0, 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)

			if b8 > 200 && r8 < 50 && g8 < 50 {
				blue++
			} else if r8 > 200 && g8 < 50 && b8 < 50 {
				red++
			} else if r8 > 250 && g8 > 250 && b8 > 250 {
				white++
			} else {
				other++
			}
		}
	}

	total := blue + red + white + other
	fmt.Printf("%s: %d pixels total\n", label, total)
	fmt.Printf("  Blue: %d (%.1f%%)\n", blue, float64(blue)/float64(total)*100)
	fmt.Printf("  Red: %d (%.1f%%)\n", red, float64(red)/float64(total)*100)
	fmt.Printf("  White: %d (%.1f%%)\n", white, float64(white)/float64(total)*100)
	fmt.Printf("  Other: %d (%.1f%%)\n", other, float64(other)/float64(total)*100)
}
