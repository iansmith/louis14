package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	stdnet "louis14/std/net"

	"louis14/pkg/resource"
)

func main() {
	width := flag.Int("w", 800, "viewport width in pixels")
	height := flag.Int("h", 600, "viewport height in pixels")
	output := flag.String("o", "output.png", "output PNG file path")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: l14show [flags] <url>\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}
	url := flag.Arg(0)

	// Fetch HTML
	fmt.Fprintf(os.Stderr, "Fetching %s...\n", url)
	body, _, err := stdnet.Fetch(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching URL: %v\n", err)
		os.Exit(1)
	}

	// Create render target
	target := image.NewRGBA(image.Rect(0, 0, *width, *height))

	// Create fetcher and renderer
	fetcher := resource.NewFetcher(url)
	renderer := resource.NewLouis14Renderer(fetcher)

	// Render
	fmt.Fprintf(os.Stderr, "Rendering %dx%d...\n", *width, *height)
	if err := renderer.Render(string(body), target); err != nil {
		fmt.Fprintf(os.Stderr, "Error rendering: %v\n", err)
		os.Exit(1)
	}

	// Save PNG
	f, err := os.Create(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if err := png.Encode(f, target); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding PNG: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Saved to %s\n", *output)
}
