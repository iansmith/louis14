package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
)

func main() {
	file, err := os.Open("/tmp/test_output5.png")
	if err != nil {
		fmt.Printf("Error opening PNG: %v\n", err)
		return
	}
	defer file.Close()

	img, err := png.Decode(file)
	if err != nil {
		fmt.Printf("Error decoding PNG: %v\n", err)
		return
	}

	rgba := img.(*image.RGBA)

	// Span is at (125, 160.2) with size 250x350
	// So it spans X=125-375, Y=160-510
	// Let's check pixels INSIDE the span
	testPixels := [][2]int{
		{150, 200},  // Inside span, near left edge
		{200, 250},  // Inside span, left-center
		{250, 300},  // Inside span, center
		{300, 350},  // Inside span, right-center
		{350, 400},  // Inside span, near right edge
	}

	fmt.Println("Checking pixels INSIDE span bounds (X=125-375, Y=160-510):")
	for _, coords := range testPixels {
		x, y := coords[0], coords[1]
		pixelIndex := y*rgba.Stride + x*4
		r := rgba.Pix[pixelIndex+0]
		g := rgba.Pix[pixelIndex+1]
		b := rgba.Pix[pixelIndex+2]
		a := rgba.Pix[pixelIndex+3]

		color := "?"
		if r == 0 && g == 128 && b == 0 {
			color = "CSS GREEN ✓"
		} else if r == 255 && g == 255 && b == 255 {
			color = "WHITE ✗"
		}

		fmt.Printf("  (%3d,%3d): RGBA(%3d,%3d,%3d,%3d) = %s\n",
			x, y, r, g, b, a, color)
	}
}
