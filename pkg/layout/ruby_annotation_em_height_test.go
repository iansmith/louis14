package layout

import "testing"

// An annotation's em height is ComputeEmHeight's: the typo ascent and
// descent of the fonts the annotation run is drawn in, each rounded up and
// capped at the primary font's (core/layout/inline/ruby_utils.cc:82-107 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). SpaceOnly is Ahem limited to
// U+0020, so it is the annotation's primary font but its "XX" is drawn in
// Liberation Serif: ceil 38.6 = 39 above the baseline, ceil 11.4 = 12
// capped at Ahem's 10 below, 49 in all. Measured in Chrome 2026-11-13 over
// a 100px Ahem base: the base sits at y 49, the annotation at y -1, and
// the div is 149px tall.
func TestRubyAnnotationEmHeight_RunFontsCappedByPrimary(t *testing.T) {
	tb, _ := layoutWithFontFace(t, `@font-face { font-family: SpaceOnly; src: url(Ahem.ttf); unicode-range: U+20 }`, "SpaceOnly", `<!doctype html><style>
body { margin: 0 }
#t { font: 100px Ahem; line-height: 1 }
#t rt { font: 50px SpaceOnly, "Liberation Serif" }
</style><div id=t><ruby>XX<rt>XX</rt></ruby></div><div id=r></div>`)
	boxes := collectTextBoxes(tb, nil)
	if len(boxes) != 2 {
		t.Fatalf("got %d text boxes, want the base and the annotation", len(boxes))
	}
	// The annotation is the 50px box, the base the 100px one.
	base, anno := boxes[0], boxes[1]
	if anno.Height > base.Height {
		base, anno = anno, base
	}
	if base.Y != 49 || anno.Y != -1 || tb.Height != 149 {
		t.Errorf("base y %v, annotation y %v, div height %v; want Chrome's 49, -1, 149", base.Y, anno.Y, tb.Height)
	}
}
