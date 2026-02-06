package main

import (
	"fmt"
	"github.com/fogleman/gg"
)

func main() {
	// Create a 200x100 canvas
	dc := gg.NewContext(200, 100)

	// Fill with white background
	dc.SetRGB(1, 1, 1)
	dc.Clear()

	// Test different green values
	greenValues := []float64{0.50, 0.502, 0.55, 0.59, 0.595, 0.60}

	x := 10.0
	for _, gVal := range greenValues {
		// Draw a rectangle with this green value
		dc.SetRGBA(0, gVal, 0, 1.0)
		dc.DrawRectangle(x, 25, 25, 50)
		dc.Fill()

		fmt.Printf("Drew rectangle at x=%.0f with green=%.3f (G_uint8=%d)\n",
			x, gVal, uint8(gVal*255))
		x += 30
	}

	// Save the result
	err := dc.SavePNG("test_green_values.png")
	if err != nil {
		fmt.Printf("Error saving PNG: %v\n", err)
		return
	}

	fmt.Println("Saved test_green_values.png")
	fmt.Println("Expected: Six green rectangles with increasing brightness")
	fmt.Println("If gg bug exists: First few rectangles will be white/invisible")
}
