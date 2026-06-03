package render

import (
	"math"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// Tests capturing the rect-top contract for underline and overline paint
// geometry. Mirrors Blink's `TextDecorationInfo::ComputeLineData`
// (text_decoration_info.cc @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f),
// which builds the line as `gfx::RectF(start_point, SizeF(width, thickness))`
// — i.e. rect TOP at `start_point.y` and rect grows DOWN by `thickness`.
//
// Verifies the regression in css-text-decor/text-decoration-thickness-* where
// a centered-stroke implementation paints OUTSIDE the visible decorating box
// when the thickness greatly exceeds the box height (see
// text-decoration-thickness-underline-001: thickness=4em on a 1em box,
// `bottom: 3em` lifts the underline center above the box entirely).

func TestUnderlineRectTop_GrowsDownFromLineY(t *testing.T) {
	// Ahem-like metrics at font-size 20px: ascent=16, descent=4.
	box := &layout.Box{Y: 0}
	info := newTextDecorationInfo(box, 80, 20, 16, 4, 0, 0)
	td := css.AppliedTextDecoration{
		Lines: css.TextDecorationLineUnderline,
		Style: "solid",
		Thickness: css.TextDecorationThickness{
			Kind: css.TextDecorationThicknessLength, Value: 80,
		},
	}

	rectTop := info.computeUnderlineLineY(td)
	thickness := info.computeThickness(td)

	// Blink-faithful: rect-top is the SAME value as computeUnderlineLineY
	// (which is Blink's `paint_underline_offset`); the rect extends DOWN
	// by `thickness` to cover [rectTop, rectTop + thickness].
	rectBottom := rectTop + thickness
	// LOU-237: auto underline anchors at the alphabetic baseline (ascent) with no
	// descent gap. box.Y + ascent + offset(=0) = 0 + 16 + 0 = 16.
	if rectTop != box.Y+16 { // ascent + offset(=0)
		t.Errorf("underline rect-top = %v; want %v", rectTop, box.Y+16.0)
	}
	if rectBottom != rectTop+80 {
		t.Errorf("underline rect-bottom = %v; want %v", rectBottom, rectTop+80)
	}
}

func TestOverlineRectTop_GrowsUpFromBoxTop(t *testing.T) {
	// Overline must cover the top edge of the text fragment. Blink computes
	// `paint_overline_offset = TextTop - floorf(thickness)`, then rect =
	// [paint_overline_offset, paint_overline_offset + thickness], which
	// places the rect's BOTTOM at TextTop. louis14's computeOverlineLineY
	// returns the TextTop position (box.Y); the rect TOP for the SOLID paint
	// is therefore `box.Y - thickness`.
	box := &layout.Box{Y: 60} // text fragment shifted by `top: 3em` on a 20px font
	info := newTextDecorationInfo(box, 80, 20, 16, 4, 0, 0)
	td := css.AppliedTextDecoration{
		Lines: css.TextDecorationLineOverline,
		Style: "solid",
		Thickness: css.TextDecorationThickness{
			Kind: css.TextDecorationThicknessLength, Value: 80,
		},
	}

	textTop := info.computeOverlineLineY()
	thickness := info.computeThickness(td)
	rectTop := textTop - thickness
	rectBottom := rectTop + thickness

	if textTop != box.Y {
		t.Errorf("computeOverlineLineY = %v; want %v (box.Y)", textTop, box.Y)
	}
	if rectTop != box.Y-thickness {
		t.Errorf("overline rect-top = %v; want %v", rectTop, box.Y-thickness)
	}
	if rectBottom != box.Y {
		t.Errorf("overline rect-bottom = %v; want %v (text-top edge)", rectBottom, box.Y)
	}
}

func TestUnderlineRectCoversBoxBelowAfterRelativeBottomShift(t *testing.T) {
	// Regression case from text-decoration-thickness-underline-001:
	//   #box   { font: 20px/1 Ahem; height: 1em (=20px); }  // visible at y=0..20
	//   #text  { position: relative; bottom: 3em; text-decoration: underline;
	//            text-decoration-thickness: 4em; }
	// The text fragment is shifted UP by 3em (=60px) from its natural top,
	// so its box.Y = -60. With a 4em (=80px) thick underline, the underline
	// anchors at the alphabetic baseline: lineY = box.Y + ascent = -60 + 16 = -44.
	// Rect-top semantics paint [-44, +36], covering 0..20 as required.
	// (Pre-LOU-237: a spurious descent*0.25 gap placed lineY at -43, +37 — same pass.)
	box := &layout.Box{Y: -60}
	info := newTextDecorationInfo(box, 80, 20, 16, 4, 0, 0)
	td := css.AppliedTextDecoration{
		Lines: css.TextDecorationLineUnderline,
		Style: "solid",
		Thickness: css.TextDecorationThickness{
			Kind: css.TextDecorationThicknessLength, Value: 80,
		},
	}
	rectTop := info.computeUnderlineLineY(td)
	thickness := info.computeThickness(td)
	rectBottom := rectTop + thickness

	const visibleTop, visibleBottom = 0.0, 20.0
	overlap := math.Min(rectBottom, visibleBottom) - math.Max(rectTop, visibleTop)
	if overlap <= 0 {
		t.Fatalf("underline rect [%v, %v] does NOT overlap visible box [%v, %v]",
			rectTop, rectBottom, visibleTop, visibleBottom)
	}
	if overlap != visibleBottom-visibleTop {
		t.Errorf("underline rect [%v, %v] overlap = %v; want full coverage = %v",
			rectTop, rectBottom, overlap, visibleBottom-visibleTop)
	}
}

func TestOverlineRectCoversBoxAboveAfterRelativeTopShift(t *testing.T) {
	// Mirror of the underline regression for text-decoration-thickness-overline-001:
	//   #box has visible region 0..20; #text shifted by `top: 3em` (+60).
	// Overline grows UP from text-top to cover the box.
	box := &layout.Box{Y: 60}
	info := newTextDecorationInfo(box, 80, 20, 16, 4, 0, 0)
	td := css.AppliedTextDecoration{
		Lines: css.TextDecorationLineOverline,
		Style: "solid",
		Thickness: css.TextDecorationThickness{
			Kind: css.TextDecorationThicknessLength, Value: 80,
		},
	}
	thickness := info.computeThickness(td)
	rectTop := info.computeOverlineLineY() - thickness
	rectBottom := rectTop + thickness

	const visibleTop, visibleBottom = 0.0, 20.0
	overlap := math.Min(rectBottom, visibleBottom) - math.Max(rectTop, visibleTop)
	if overlap <= 0 {
		t.Fatalf("overline rect [%v, %v] does NOT overlap visible box [%v, %v]",
			rectTop, rectBottom, visibleTop, visibleBottom)
	}
	if overlap != visibleBottom-visibleTop {
		t.Errorf("overline rect [%v, %v] overlap = %v; want full coverage = %v",
			rectTop, rectBottom, overlap, visibleBottom-visibleTop)
	}
}

// TestBaselineRelativeOffset_FromFont verifies that when the font supplies a
// post/MVAR underline-position metric and text-underline-position is from-font,
// baselineRelativeOffset uses the font metric rather than falling back to the
// alphabetic baseline. Mirrors Blink's kNearAlphabeticBaselineFromFont case in
// TextDecorationOffset::ComputeUnderlineOffset @
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Sign convention: underlinePositionFromFont is Y-up negative (below baseline),
// so the downward Y-down gap = -underlinePositionFromFont (positive).
func TestBaselineRelativeOffset_FromFont(t *testing.T) {
	box := &layout.Box{Y: 0}
	cases := []struct {
		name       string
		pos        css.TextUnderlinePosition
		fontPos    float64 // Y-up: negative = below baseline
		wantOffset float64 // distance from box edge (box.Y) to underline zero
	}{
		// from-font + metric present: gap = ascent + |fontPos|
		{"from-font: metric present uses font gap", css.TextUnderlinePositionFromFont, -3.0, 16 + 3.0},
		// from-font + metric absent (0): falls back to alphabetic baseline
		{"from-font: zero metric falls back to baseline", css.TextUnderlinePositionFromFont, 0, 16},
		// auto ignores a non-zero font metric (different input dimension from above)
		{"auto: non-zero metric ignored, stays at baseline", css.TextUnderlinePositionAuto, -3.0, 16},
		// under is unaffected by underlinePositionFromFont
		{"under: always block-end regardless of metric", css.TextUnderlinePositionUnder, -3.0, 16 + 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := newTextDecorationInfo(box, 80, 20, 16, 4, 0, c.fontPos)
			got := info.baselineRelativeOffset(c.pos)
			if got != c.wantOffset {
				t.Errorf("baselineRelativeOffset(%v, fontPos=%v) = %v; want %v",
					c.pos, c.fontPos, got, c.wantOffset)
			}
		})
	}
}

// isCJKLocale mirrors Blink's CJK detection in ResolveUnderlinePosition() at
// text_decoration_info.cc:42 @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f,
// promoted to a locale-prefix check (louis14 doesn't yet derive script from
// font Unicode coverage). Locks in case-insensitivity and BCP47-subtag
// tolerance behavior.
func TestIsCJKLocale(t *testing.T) {
	cases := []struct {
		lang string
		want bool
	}{
		{"", false},
		{"en", false},
		{"en-US", false},
		{"ja", true},
		{"JA", true},
		{"ja-JP", true},
		{"zh", true},
		{"zh-Hant", true},
		{"ko", true},
		{"mn", true},
		{"mn-Mong", true},
		{"de", false},
		{"jp", false}, // ISO-639 for Japanese is "ja", not "jp"
	}
	for _, c := range cases {
		if got := isCJKLocale(c.lang); got != c.want {
			t.Errorf("isCJKLocale(%q) = %v, want %v", c.lang, got, c.want)
		}
	}
}
