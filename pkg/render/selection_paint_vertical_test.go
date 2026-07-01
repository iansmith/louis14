package render

// Tests for LOU-347: ::selection highlight-segment painting under vertical
// writing modes. drawText's segment-splitting wrapper (unlike drawTextSegment,
// its non-highlight-segment sibling — see that function's ~550 lines of
// IsVerticalText/IsSidewaysRL/IsSidewaysLR dispatch) was unconditionally
// horizontal-axis prior to this fix: it always advanced later segments along
// physical X and always sized the highlight background rect's WIDTH from the
// selected span's measured extent. Under a vertical writing mode the advance
// axis is physical Y (box.Height, not box.Width, carries the text's advance
// extent — see pkg/layout/writing_mode_converter.go's ToPhysicalSize, which
// swaps logical inline-size into physical Height for every non-horizontal
// writing mode), so both assumptions are wrong there.

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// TestDrawText_VerticalWritingMode_SegmentAdvancesAlongY reproduces LOU-347:
// a 3-character text run "a b" (a, space, b) with the middle SPACE character
// selected, under IsVerticalText (upright vertical, e.g. writing-mode:
// vertical-rl; text-orientation: upright). Ahem (the WPT test font used by
// active-selection-031.html, this fix's simplest target per the ticket plan)
// paints a solid opaque glyph box for every visible character, so a selected
// visible glyph would occlude its own highlight band from a pixel scan; a
// selected SPACE has no ink, letting the yellow ::selection background show
// through cleanly. The band must appear at physical Y in
// [box.Y+fontSize, box.Y+2*fontSize), spanning the box's full width — i.e.
// offset along Y, not X. Pre-fix, drawText computed
// segBox.X = box.X + prefixAdvance unconditionally, which (a) misplaced the
// second segment's paint position (it never moved along Y) and (b) sized the
// highlight rect's WIDTH from the selected span's advance-axis extent
// instead of its HEIGHT, so the highlighted region landed at the wrong
// physical location entirely for vertical text.
func TestDrawText_VerticalWritingMode_SegmentAdvancesAlongY(t *testing.T) {
	const fontSize = 16.0
	const W, H = 100, 100

	target := image.NewRGBA(image.Rect(0, 0, W, H))
	r := NewRendererForImage(target)

	stylesheet, err := css.ParseStylesheet(`p::selection { background-color: yellow; }`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	r.selectionStylesheets = []*css.Stylesheet{stylesheet}
	r.selectionViewportW = W
	r.selectionViewportH = H

	p := &html.Node{Type: html.ElementNode, TagName: "p"}
	tn := textNode("a b")
	p.Children = []*html.Node{tn}
	tn.Parent = p

	// The space (node-local [1,2)) is selected.
	r.selectionRange = &html.Range{StartContainer: tn, StartOffset: 1, EndContainer: tn, EndOffset: 2}

	box := &layout.Box{
		Node: p, Text: "a b",
		X: 10, Y: 10,
		Width:          fontSize,     // block/cross axis (line thickness)
		Height:         3 * fontSize, // inline/advance axis (grows with text length)
		IsVerticalText: true,
	}
	r.nodeBoxIndex = map[*html.Node]*layout.Box{p: box}

	layer := &PaintLayer{
		Box: box, TextColor: css.Color{A: 1},
		FontSize: fontSize, FontAhem: true, IsVerticalText: true,
	}

	r.drawText(layer)

	yellow := color.RGBA{R: 255, G: 255, B: 0, A: 255}
	// Expected highlighted band: physical Y in [box.Y+fontSize, box.Y+2*fontSize),
	// spanning the full box.Width in X — i.e. offset along Y from the box origin,
	// not along X. Sampled 2px inset from every edge to tolerate pixel-snap
	// rounding at the band boundary.
	inset := 2
	bandYStart, bandYEnd := int(box.Y+fontSize)+inset, int(box.Y+2*fontSize)-inset
	bandXStart, bandXEnd := int(box.X)+inset, int(box.X+box.Width)-inset

	if !bandIsAllColor(target, bandXStart, bandXEnd, bandYStart, bandYEnd, yellow) {
		t.Errorf("expected yellow ::selection background band at physical Y=[%d,%d) X=[%d,%d) (the space character's advance-axis position under vertical writing mode); band not fully painted there", bandYStart, bandYEnd, bandXStart, bandXEnd)
	}

	// The first ("a") character's band must NOT be highlighted.
	aYStart, aYEnd := int(box.Y)+inset, int(box.Y+fontSize)-inset
	if bandHasAnyColor(target, bandXStart, bandXEnd, aYStart, aYEnd, yellow) {
		t.Errorf("unexpected yellow ::selection background in the 'a' character's band Y=[%d,%d) — selection should only cover the space, not the whole run collapsed to an X-axis segment split", aYStart, aYEnd)
	}
}

// bandIsAllColor reports whether every pixel in target's [x0,x1)x[y0,y1)
// region is within colorsClose tolerance of want.
func bandIsAllColor(target *image.RGBA, x0, x1, y0, y1 int, want color.RGBA) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !colorsClose(target.RGBAAt(x, y), want) {
				return false
			}
		}
	}
	return true
}

// bandHasAnyColor reports whether any pixel in target's [x0,x1)x[y0,y1)
// region is within colorsClose tolerance of want.
func bandHasAnyColor(target *image.RGBA, x0, x1, y0, y1 int, want color.RGBA) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if colorsClose(target.RGBAAt(x, y), want) {
				return true
			}
		}
	}
	return false
}

// colorsClose compares two colors with a small per-channel tolerance to
// absorb pixel-snapping / anti-aliasing at band edges.
func colorsClose(a, b color.RGBA) bool {
	const tol = 10
	diff := func(x, y uint8) bool {
		d := int(x) - int(y)
		if d < 0 {
			d = -d
		}
		return d <= tol
	}
	return diff(a.R, b.R) && diff(a.G, b.G) && diff(a.B, b.B) && diff(a.A, b.A)
}
