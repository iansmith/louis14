package text

import (
	"encoding/binary"
	"testing"
)

// TestAscentDescentWithHacks_HeightFallback (LOU-424 review): the upright
// advance of a font with no vmtx is HarfBuzzFontData's height_fallback_ =
// lroundf(ascent) + lroundf(descent) from FontMetrics::AscentDescentWithHacks
// over SkFont::getMetrics (platform/fonts/shaping/harfbuzz_font_data.h:62-69 and
// platform/fonts/font_metrics.cc:50-152 @
// 5992657f556bcd9d207a4aca187488462f027cab), whose ascent/descent are
// FreeType's face ascender/descender scaled to the font size. Expected values
// come from an independent sfnt read of each file (head unitsPerEm, hhea
// ascender/descender, OS/2 fsSelection/sTypo*), not from louis14 output:
//
//   - LiberationSerif-Regular: upem 2048, hhea 1825/-443, no USE_TYPO_METRICS:
//     20px → 17.82 + 4.33 → 18 + 4 = 22 (Chrome's 22, where louis14's line
//     ascent+descent gives 18); 13px → 11.58 + 2.81 → 12 + 3 = 15.
//   - ExTest.woff: upem 1024, USE_TYPO_METRICS set, sTypo 768/-256 (hhea
//     128/0): 20px → 15 + 5 = 20 — the typo branch, not hhea's 2.5 + 0.
//   - Ahem.ttf: upem 1000, hhea 800/-200: 20px → 16 + 4 = 20.
func TestAscentDescentWithHacks_HeightFallback(t *testing.T) {
	fr := NewFontRegistry()
	exTest := registerTestFace(t, fr, "LOU424-ADH-ExTest", "ExTest.woff", fetchCSSValuesResource)
	serif := DefaultFontConfig().resolveBuiltinFamily("serif", false, false)
	for _, tc := range []struct {
		name string
		path string
		size float64
		want int
	}{
		{"LiberationSerif 20px", serif, 20, 22},
		{"LiberationSerif 13px", serif, 13, 15},
		{"ExTest 20px (USE_TYPO_METRICS)", exTest, 20, 20},
		{"Ahem 20px", "../../fonts/Ahem.ttf", 20, 20},
	} {
		vd := fontPlatformData{fontPath: tc.path, size: tc.size}.GetHarfBuzzFace().VerticalData()
		if vd.HasVerticalMetrics() {
			t.Fatalf("%s: fixture has vmtx", tc.name)
		}
		if got := vd.AdvanceHeight(1); got != float64(tc.want) {
			t.Errorf("%s: AdvanceHeight (height fallback) = %v, want %d", tc.name, got, tc.want)
		}
	}
}

// TestSfntFaceAscenderDescender ports FreeType's choice of a face's
// ascender/descender (src/sfnt/sfobjs.c sfnt_load_face, FreeType @
// a5877f48fff8dd6cf8d53c505ce055499376aee2, Chromium's DEPS pin at
// 5992657f556bcd9d207a4aca187488462f027cab), which Skia's
// SkScalerContext_FreeType::generateFontMetrics scales into SkFontMetrics
// (src/ports/SkFontHost_FreeType.cpp:1603-1612, Skia @
// c0c46ee9f938fa32c086bea666199686ad7cf26e): OS/2 sTypo* when
// USE_TYPO_METRICS (fsSelection bit 7) is set; otherwise hhea; when hhea's
// are both 0, OS/2 sTypo* if non-zero, else usWin*.
func TestSfntFaceAscenderDescender(t *testing.T) {
	hhea := func(asc, desc int16) []byte {
		b := make([]byte, 36)
		binary.BigEndian.PutUint16(b[4:], uint16(asc))
		binary.BigEndian.PutUint16(b[6:], uint16(desc))
		return b
	}
	os2 := func(fsSelection uint16, typoAsc, typoDesc int16, winAsc, winDesc uint16) []byte {
		b := make([]byte, 78)
		binary.BigEndian.PutUint16(b[62:], fsSelection)
		binary.BigEndian.PutUint16(b[68:], uint16(typoAsc))
		binary.BigEndian.PutUint16(b[70:], uint16(typoDesc))
		binary.BigEndian.PutUint16(b[74:], winAsc)
		binary.BigEndian.PutUint16(b[76:], winDesc)
		return b
	}
	for _, tc := range []struct {
		name                 string
		hhea, os2            []byte
		wantAscent, wantDesc int
	}{
		{"hhea", hhea(900, -200), os2(0, 700, -300, 1000, 250), 900, -200},
		{"USE_TYPO_METRICS", hhea(900, -200), os2(1<<7, 700, -300, 1000, 250), 700, -300},
		{"zero hhea → typo", hhea(0, 0), os2(0, 700, -300, 1000, 250), 700, -300},
		{"zero hhea and typo → win", hhea(0, 0), os2(0, 0, 0, 1000, 250), 1000, -250},
		{"zero hhea, no OS/2", hhea(0, 0), nil, 0, 0},
		{"no OS/2", hhea(900, -200), nil, 900, -200},
	} {
		a, d := sfntFaceAscenderDescender(tc.hhea, tc.os2)
		if a != tc.wantAscent || d != tc.wantDesc {
			t.Errorf("%s: (ascender, descender) = (%d, %d), want (%d, %d)", tc.name, a, d, tc.wantAscent, tc.wantDesc)
		}
	}
}
