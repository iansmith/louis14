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

	// Check several pixels in the white rectangle area
	testPixels := [][2]int{
		{75, 200},   // Center of white area
		{50, 175},   // Top-left of white area
		{100, 300},  // Middle of white area
		{250, 335},  // Previous test point
		{200, 200},  // Should be green border
	}

	for _, coords := range testPixels {
		x, y := coords[0], coords[1]
		pixelIndex := y*rgba.Stride + x*4
		r := rgba.Pix[pixelIndex+0]
		g := rgba.Pix[pixelIndex+1]
		b := rgba.Pix[pixelIndex+2]
		a := rgba.Pix[pixelIndex+3]

		color := "?"
		if r == 0 && g == 128 && b == 0 {
			color = "CSS GREEN"
		} else if r == 255 && g == 255 && b == 255 {
			color = "WHITE"
		} else if r > 200 && g > 200 && b > 200 {
			color = "light/white"
		} else if g > r && g > b {
			color = "greenish"
		}

		fmt.Printf("Pixel at (%3d,%3d): RGBA(%3d,%3d,%3d,%3d) = %s\n",
			x, y, r, g, b, a, color)
	}
}
