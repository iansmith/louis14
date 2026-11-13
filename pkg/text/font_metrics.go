package text

import (
	"encoding/binary"
	"math"

	"louis14/pkg/geometry/layoutunit"
)

// hhea and OS/2 field offsets (OpenType spec) read by
// sfntFaceAscenderDescender.
const (
	hheaAscenderOffset     = 4
	hheaDescenderOffset    = 6
	hheaMinSize            = 36
	os2FsSelectionOffset   = 62
	os2TypoAscenderOffset  = 68
	os2TypoDescenderOffset = 70
	os2WinAscentOffset     = 74
	os2WinDescentOffset    = 76
	os2Version0Size        = 78 // the shortest OS/2 table FreeType loads
	os2UseTypoMetrics      = 1 << 7
)

// i16 reads the big-endian int16 at b[off:].
func i16(b []byte, off int) int { return int(int16(binary.BigEndian.Uint16(b[off:]))) }

// sfntFaceAscenderDescender returns the face ascender and descender, in font
// units, that FreeType sets on a scalable face: OS/2 sTypoAscender/
// sTypoDescender when fsSelection's USE_TYPO_METRICS bit is set, else hhea's,
// and when hhea's are both 0, OS/2 sTypo* if either is non-zero, else
// usWinAscent/-usWinDescent. Mirrors FreeType's sfnt_load_face
// (src/sfnt/sfobjs.c:1489-1522, FreeType @
// a5877f48fff8dd6cf8d53c505ce055499376aee2, Chromium's DEPS pin at
// 5992657f556bcd9d207a4aca187488462f027cab), whose values Skia's
// SkScalerContext_FreeType::generateFontMetrics scales into SkFontMetrics
// fAscent/fDescent (src/ports/SkFontHost_FreeType.cpp:1603-1612, Skia @
// c0c46ee9f938fa32c086bea666199686ad7cf26e). An OS/2 table shorter than
// version 0 counts as absent, as FreeType fails to load it.
func sfntFaceAscenderDescender(hhea, os2 []byte) (ascender, descender int) {
	u16 := func(b []byte, off int) int { return int(binary.BigEndian.Uint16(b[off:])) }
	hasOS2 := len(os2) >= os2Version0Size
	if hasOS2 && u16(os2, os2FsSelectionOffset)&os2UseTypoMetrics != 0 {
		return i16(os2, os2TypoAscenderOffset), i16(os2, os2TypoDescenderOffset)
	}
	if len(hhea) >= hheaMinSize {
		ascender, descender = i16(hhea, hheaAscenderOffset), i16(hhea, hheaDescenderOffset)
	}
	if ascender != 0 || descender != 0 || !hasOS2 {
		return ascender, descender
	}
	if a, d := i16(os2, os2TypoAscenderOffset), i16(os2, os2TypoDescenderOffset); a != 0 || d != 0 {
		return a, d
	}
	return u16(os2, os2WinAscentOffset), -u16(os2, os2WinDescentOffset)
}

// ascentDescentWithHacks returns the rounded ascent and descent, in pixels,
// of typeface t at size. Mirrors the branch of
// FontMetrics::AscentDescentWithHacks (platform/fonts/font_metrics.cc:50-152
// @ 5992657f556bcd9d207a4aca187488462f027cab) that HarfBuzzFontData::
// UpdateFallbackMetricsAndScale takes (no overrides, no subpixel ascent/
// descent, harfbuzz_font_data.h:62-65 @ the same SHA):
// SkScalarRoundToScalar of the SkFont metrics, the face ascender/descender
// scaled by size / unitsPerEm. Skia scales by FreeType's raw units_per_EM
// (SkTypeface_FreeType::GetUnitsPerEm, src/ports/SkFontHost_FreeType.cpp:
// 855-867, Skia @ c0c46ee9f938fa32c086bea666199686ad7cf26e), not HarfBuzz's
// clamped get_upem; the two agree because t is only read when it loaded,
// and FreeType refuses a unitsPerEm outside the range HarfBuzz clamps
// (typeface.loaded). Not ported: the VDMX table (read only under
// full/normal hinting, which louis14 does not do), the Linux subpixel-
// positioning descent borrow, and the Mac Times/Helvetica/Courier ascent hack.
func ascentDescentWithHacks(t *typeface, size float64) (ascent, descent float64) {
	scale := size / float64(t.upem)
	return math.Floor(float64(t.ascender)*scale + 0.5), math.Floor(-float64(t.descender)*scale + 0.5)
}

// typoAscenderAndDescender returns OS/2 sTypoAscender and -sTypoDescender,
// in font units, or 0, 0 when the table is too short to hold them. Mirrors
// TypoAscenderAndDescender (platform/fonts/simple_font_data.cc:360-373 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which reads the two int16s at
// OS/2 offset 68.
func typoAscenderAndDescender(os2 []byte) (ascender, descender int) {
	if len(os2) < os2TypoDescenderOffset+2 {
		return 0, 0
	}
	return i16(os2, os2TypoAscenderOffset), -i16(os2, os2TypoDescenderOffset)
}

// normalizedTypoAscentAndDescent scales ascent and descent so they sum to
// emHeight, keeping their ratio: the ascent rounded to a LayoutUnit, the
// descent the rest of emHeight rounded to a LayoutUnit. ok is false when
// the pair cannot be normalized (a non-positive sum, or an ascent outside
// [0, sum]). Mirrors SimpleFontData::TrySetNormalizedTypoAscentAndDescent
// (platform/fonts/simple_font_data.cc:395-415 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func normalizedTypoAscentAndDescent(ascent, descent, emHeight float64) (normAscent, normDescent float64, ok bool) {
	height := ascent + descent
	if height <= 0 || ascent < 0 || ascent > height {
		return 0, 0, false
	}
	a := layoutunit.FromFloat64Round(ascent * emHeight / height)
	return a.Float64(), layoutunit.FromFloat64Round(emHeight).Sub(a).Float64(), true
}
