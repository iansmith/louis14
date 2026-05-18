// Command l14imgdiff compares two PNG images of identical dimensions and
// reports pixel-difference metrics as JSON. It is the shared diff step for
// LOU-114 three-image comparisons, so every per-site analysis reports the
// same numbers.
//
// Usage:
//
//	l14imgdiff [-tol N] [-diff out.png] <a.png> <b.png>
//
// Output (stdout, JSON):
//
//	{"width":1000,"height":980,"total_pixels":980000,
//	 "different_pixels":362600,"different_percent":37.0,
//	 "max_channel_diff":255,"mean_channel_diff":27.2}
//
// different_pixels counts pixels whose max channel difference exceeds -tol.
// mean_channel_diff averages the max-channel difference over every pixel.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	tol := flag.Int("tol", 2, "per-channel tolerance; a pixel counts as different when its max channel diff exceeds this")
	diffPath := flag.String("diff", "", "optional path to write a diff image (different pixels in red)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: l14imgdiff [-tol N] [-diff out.png] <a.png> <b.png>\n\nCompares two equally-sized PNGs and prints JSON difference metrics.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(1)
	}

	a, err := loadPNG(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", flag.Arg(0), err)
		os.Exit(1)
	}
	b, err := loadPNG(flag.Arg(1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", flag.Arg(1), err)
		os.Exit(1)
	}
	if a.Bounds() != b.Bounds() {
		fmt.Fprintf(os.Stderr, "dimensions differ: %v vs %v\n", a.Bounds(), b.Bounds())
		os.Exit(1)
	}

	bounds := a.Bounds()
	var diffImg *image.RGBA
	if *diffPath != "" {
		diffImg = image.NewRGBA(bounds)
	}

	var different int
	var maxDiff int
	var sumDiff int64
	total := bounds.Dx() * bounds.Dy()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			d := max4(
				absDiff(ar>>8, br>>8),
				absDiff(ag>>8, bg>>8),
				absDiff(ab>>8, bb>>8),
				absDiff(aa>>8, ba>>8),
			)
			sumDiff += int64(d)
			if d > maxDiff {
				maxDiff = d
			}
			if d > *tol {
				different++
				if diffImg != nil {
					diffImg.Set(x, y, color.RGBA{255, 0, 0, 255})
				}
			} else if diffImg != nil {
				diffImg.Set(x, y, a.At(x, y))
			}
		}
	}

	if diffImg != nil {
		f, err := os.Create(*diffPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "write diff image: %v\n", err)
			os.Exit(1)
		}
		if err := png.Encode(f, diffImg); err != nil {
			f.Close()
			fmt.Fprintf(os.Stderr, "encode diff image: %v\n", err)
			os.Exit(1)
		}
		f.Close()
	}

	out := map[string]any{
		"width":             bounds.Dx(),
		"height":            bounds.Dy(),
		"total_pixels":      total,
		"different_pixels":  different,
		"different_percent": round1(100 * float64(different) / float64(total)),
		"max_channel_diff":  maxDiff,
		"mean_channel_diff": round1(float64(sumDiff) / float64(total)),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func absDiff(a, b uint32) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func max4(a, b, c, d int) int {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	if d > m {
		m = d
	}
	return m
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
