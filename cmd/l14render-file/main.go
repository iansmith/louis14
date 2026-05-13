package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"louis14/pkg/js"
	"louis14/pkg/resource"
)

func main() {
	width := flag.Int("w", 1000, "viewport width in CSS px")
	height := flag.Int("h", 980, "viewport height in CSS px")
	output := flag.String("o", "out.png", "output PNG file")
	base := flag.String("base", "https://x.com/", "base URL for resolving relative asset references")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: l14render-file [flags] <input.html>\n\nReads HTML from a file; fetches CSS/images/fonts over HTTP\nrelative to -base. Outputs a PNG at the given viewport.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	htmlBytes, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read html: %v\n", err)
		os.Exit(1)
	}

	fetcher := resource.NewFetcher(*base)
	renderer := resource.NewLouis14Renderer(fetcher)
	renderer.SetJSEngine(js.New())

	target := image.NewRGBA(image.Rect(0, 0, *width, *height))
	fmt.Fprintf(os.Stderr, "Rendering %dx%d (base=%s)...\n", *width, *height, *base)
	if err := renderer.Render(string(htmlBytes), target); err != nil {
		fmt.Fprintf(os.Stderr, "render: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create out: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, target); err != nil {
		fmt.Fprintf(os.Stderr, "encode png: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Saved %s\n", *output)
}
