package filters

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestLOU129_DumpDropShadowChain reproduces the exact filter chain used by
// tainting-fedropshadow-001.html and dumps each primitive's output to
// /tmp/lou129_bug1_dump/out so we can visually compare to expected per-stage
// behaviour from the Blink spec / brief.
//
// This test only runs when LOU129_DEBUG=1 in env (it's a manual instrumentation
// tool, not a real assertion).
func TestLOU129_DumpDropShadowChain(t *testing.T) {
	if os.Getenv("LOU129_DEBUG") == "" {
		t.Skip("set LOU129_DEBUG=1 to enable")
	}
	outDir := "/tmp/lou129_bug1_dump/out"
	os.MkdirAll(outDir, 0755)

	refBox := image.Rect(0, 0, 100, 100)
	// Current code yields filterRegion = (-10,-10)-(110,110) for a 100x100
	// reference box (default x=-10%,y=-10%,w=120%,h=120% in userSpaceOnUse
	// using filter region's own base in louis14).
	region := image.Rect(-10, -10, 110, 110)

	fmt.Printf("reference box: %v\n", refBox)
	fmt.Printf("filter region: %v size=%dx%d\n", region, region.Dx(), region.Dy())

	space := InterpolationSpaceSRGB

	src := NewSourceGraphic(space)
	srcAlpha := NewSourceAlpha(src, space)

	// Build SourceGraphic image: red→green gradient rect 100x100 at (0,0)
	// (in absolute coords) → buffer pixels (10..110, 10..110).
	srcImg := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	for y := 10; y < 110; y++ {
		for x := 10; x < 110; x++ {
			rectX := x - 10
			var r, g, b uint8
			if rectX < 50 {
				r, g, b = 255, 0, 0
			} else {
				// CSS "green" = rgb(0,128,0)
				r, g, b = 0, 128, 0
			}
			i := y*srcImg.Stride + x*4
			srcImg.Pix[i+0] = r
			srcImg.Pix[i+1] = g
			srcImg.Pix[i+2] = b
			srcImg.Pix[i+3] = 255
		}
	}
	src.image = srcImg
	dumpImage(srcImg, filepath.Join(outDir, "stage0_SourceGraphic.png"), region)
	summarize("stage0_SourceGraphic", srcImg, region)

	// 1. feFlood x=0 y=0 w=100 h=100 (default black, opaque)
	flood := NewFEFlood(space, 0, 0, 0, 1.0)
	flood.Subregion = image.Rect(0, 0, 100, 100)

	// 2. feDropShadow width=100% dx=100 dy=0 stdDev=0 flood-color=(0,255,128)
	// CRITICAL: This is constructed by louis14 with srcAlpha = filter's global
	// SourceAlpha (the rect's alpha = full opaque 100x100). Per Blink it should
	// be the alpha-of-in (alpha of feFlood = the same 100x100 here).
	// width="100%" → subregion clip wrapper applied at builder; with region
	// width=120, 100% of regionW=120 → primitive subregion = (-10,-10,110,-10+120=110).
	// But the brief expects shadow at (100..200, 0..100) — let's see what we get.
	dropShadow := NewFEDropShadow(flood, srcAlpha, space, 100, 0, 0, 0, 255, 128, 1.0)
	// Apply primitive subregion clip with current resolution semantics:
	// regionW = 120 (the filter region width), so 100% = 120.
	// xDefault → region.Min.X = -10
	// yDefault → region.Min.Y = -10
	// hDefault → regionH = 120
	dropShadowClipped := NewSubregionClippedEffect(dropShadow,
		image.Rect(-10, -10, -10+120, -10+120))

	// 3. feOffset dx=-100 dy=0
	offset := NewFEOffset(dropShadowClipped, space, -100, 0)

	// 4. feDisplacementMap in=SourceGraphic in2=offset, xChan=G, yChan=B,
	//    scale=100, subregion (0,0,100,100)
	dispMap := NewFEDisplacementMap(src, offset, space, ChannelG, ChannelB, 100)
	dispMapClipped := NewSubregionClippedEffect(dispMap, image.Rect(0, 0, 100, 100))

	// Helper to evaluate any effect and dump its output.
	dumpStage := func(label string, eff FilterEffect) {
		memo := map[FilterEffect]*image.RGBA{}
		out := evalRecursive(eff, memo, region)
		path := filepath.Join(outDir, label+".png")
		dumpImage(out, path, region)
		summarize(label, out, region)
	}

	dumpStage("stage1_feFlood", flood)
	dumpStage("stage2_feDropShadow_raw", dropShadow)
	dumpStage("stage2b_feDropShadow_clipped", dropShadowClipped)
	dumpStage("stage3_feOffset", offset)
	dumpStage("stage4_feDisplacementMap_unclipped", dispMap)
	dumpStage("stage4b_feDisplacementMap_clipped", dispMapClipped)
}

func evalRecursive(e FilterEffect, memo map[FilterEffect]*image.RGBA, region image.Rectangle) *image.RGBA {
	if cached, ok := memo[e]; ok {
		return cached
	}
	deps := e.InputEffects()
	inputs := make([]*image.RGBA, len(deps))
	for i, dep := range deps {
		inputs[i] = evalRecursive(dep, memo, region)
	}
	return e.ApplyEffect(inputs, region)
}

func dumpImage(img *image.RGBA, path string, _ image.Rectangle) {
	// Un-premultiply for visual clarity in PNGs.
	out := image.NewRGBA(img.Bounds())
	for i := 0; i+3 < len(img.Pix); i += 4 {
		a := img.Pix[i+3]
		if a == 0 {
			continue
		}
		if a == 255 {
			out.Pix[i+0] = img.Pix[i+0]
			out.Pix[i+1] = img.Pix[i+1]
			out.Pix[i+2] = img.Pix[i+2]
			out.Pix[i+3] = 255
		} else {
			af := float64(a)
			cv := func(c uint8) uint8 {
				v := float64(c) * 255.0 / af
				if v > 255 {
					v = 255
				}
				return uint8(v + 0.5)
			}
			out.Pix[i+0] = cv(img.Pix[i+0])
			out.Pix[i+1] = cv(img.Pix[i+1])
			out.Pix[i+2] = cv(img.Pix[i+2])
			out.Pix[i+3] = a
		}
	}
	f, _ := os.Create(path)
	defer f.Close()
	png.Encode(f, out)
}

func summarize(label string, img *image.RGBA, region image.Rectangle) {
	w, h := region.Dx(), region.Dy()
	minX, minY, maxX, maxY := w, h, -1, -1
	uniq := map[color.RGBA]int{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*img.Stride + x*4
			a := img.Pix[i+3]
			if a == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
			c := color.RGBA{img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
			uniq[c]++
		}
	}
	if maxX < 0 {
		fmt.Printf("[%s] EMPTY\n", label)
		return
	}
	absMin := image.Point{X: minX + region.Min.X, Y: minY + region.Min.Y}
	absMax := image.Point{X: maxX + region.Min.X, Y: maxY + region.Min.Y}
	fmt.Printf("[%s] bbox abs=(%d,%d)..(%d,%d) buf=(%d,%d)..(%d,%d) unique_colors=%d\n",
		label, absMin.X, absMin.Y, absMax.X, absMax.Y, minX, minY, maxX, maxY, len(uniq))
	type cc struct {
		c color.RGBA
		n int
	}
	var top []cc
	for k, n := range uniq {
		top = append(top, cc{k, n})
	}
	for i := 0; i < len(top); i++ {
		for j := i + 1; j < len(top); j++ {
			if top[j].n > top[i].n {
				top[i], top[j] = top[j], top[i]
			}
		}
	}
	if len(top) > 5 {
		top = top[:5]
	}
	for _, t := range top {
		fmt.Printf("    %d × {R=%d G=%d B=%d A=%d}\n", t.n, t.c.R, t.c.G, t.c.B, t.c.A)
	}
}
