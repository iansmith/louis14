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

	// Check pixel at (250, 335) - should be green (0,128,0)
	rgba := img.(*image.RGBA)
	pixelIndex := 335*rgba.Stride + 250*4
	r := rgba.Pix[pixelIndex+0]
	g := rgba.Pix[pixelIndex+1]
	b := rgba.Pix[pixelIndex+2]
	a := rgba.Pix[pixelIndex+3]

	fmt.Printf("Pixel at (250,335) in saved PNG: RGBA(%d,%d,%d,%d)\n", r, g, b, a)

	if r == 0 && g == 128 && b == 0 {
		fmt.Println("✓ GREEN is preserved in PNG file!")
	} else if r == 255 && g == 255 && b == 255 {
		fmt.Println("✗ Pixel is WHITE in PNG file - encoding bug!")
	} else {
		fmt.Printf("? Unexpected color\n")
	}
}
