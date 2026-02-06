package main

import (
	"fmt"
	"image"
	_ "image/png"
	"os"
)

func main() {
	// Load test, ref, and diff images
	testImg := loadImage("output/reftests/box-generation-001_test.png")
	refImg := loadImage("output/reftests/box-generation-001_ref.png")
	
	bounds := testImg.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	
	// Analyze differences by region
	regions := []struct{
		name string
		x, y, w, h int
	}{
		{"Top text area", 0, 0, 400, 50},
		{"Blue block", 0, 50, 100, 30},
		{"Yellow+Orange area", 0, 80, 400, 30},
		{"Bottom area", 0, 110, 400, 190},
	}
	
	fmt.Printf("Image size: %dx%d\n\n", width, height)
	
	for _, region := range regions {
		diffCount := 0
		totalPixels := 0
		
		for y := region.y; y < region.y+region.h && y < height; y++ {
			for x := region.x; x < region.x+region.w && x < width; x++ {
				totalPixels++
				tr, tg, tb, _ := testImg.At(x, y).RGBA()
				rr, rg, rb, _ := refImg.At(x, y).RGBA()
				
				// Convert to 8-bit
				tr8, tg8, tb8 := uint8(tr>>8), uint8(tg>>8), uint8(tb>>8)
				rr8, rg8, rb8 := uint8(rr>>8), uint8(rg>>8), uint8(rb>>8)
				
				if abs(int(tr8)-int(rr8)) > 5 || abs(int(tg8)-int(rg8)) > 5 || abs(int(tb8)-int(rb8)) > 5 {
					diffCount++
				}
			}
		}
		
		pct := float64(diffCount) / float64(totalPixels) * 100
		fmt.Printf("%s: %d/%d pixels differ (%.1f%%)\n", 
			region.name, diffCount, totalPixels, pct)
	}
	
	// Find bounding box of differences
	minX, minY := width, height
	maxX, maxY := 0, 0
	
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			tr, tg, tb, _ := testImg.At(x, y).RGBA()
			rr, rg, rb, _ := refImg.At(x, y).RGBA()
			
			tr8, tg8, tb8 := uint8(tr>>8), uint8(tg>>8), uint8(tb>>8)
			rr8, rg8, rb8 := uint8(rr>>8), uint8(rg>>8), uint8(rb>>8)
			
			if abs(int(tr8)-int(rr8)) > 5 || abs(int(tg8)-int(rg8)) > 5 || abs(int(tb8)-int(rb8)) > 5 {
				if x < minX { minX = x }
				if y < minY { minY = y }
				if x > maxX { maxX = x }
				if y > maxY { maxY = y }
			}
		}
	}
	
	fmt.Printf("\nDifference bounding box: (%d,%d) to (%d,%d)\n", minX, minY, maxX, maxY)
	fmt.Printf("Difference area: %dx%d pixels\n", maxX-minX, maxY-minY)
}

func loadImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	
	img, _, err := image.Decode(f)
	if err != nil {
		panic(err)
	}
	return img
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
