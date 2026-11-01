package text

import (
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
)

// sfntWith builds a minimal sfnt file holding tables (tag → bytes): the
// offset table and table records the OpenType spec lays out, each table
// 4-byte aligned. Checksums are 0; table readers do not verify them.
func sfntWith(tables map[uint32][]byte) []byte {
	tags := make([]uint32, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })
	out := make([]byte, 12+16*len(tags))
	binary.BigEndian.PutUint32(out[0:], 0x00010000)
	binary.BigEndian.PutUint16(out[4:], uint16(len(tags)))
	for i, tag := range tags {
		rec := out[12+16*i:]
		binary.BigEndian.PutUint32(rec[0:], tag)
		binary.BigEndian.PutUint32(rec[8:], uint32(len(out)))
		binary.BigEndian.PutUint32(rec[12:], uint32(len(tables[tag])))
		out = append(out, tables[tag]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	return out
}

// headWithUnitsPerEm is a 54-byte head table with the given unitsPerEm.
func headWithUnitsPerEm(upem uint16) []byte {
	b := make([]byte, 54)
	binary.BigEndian.PutUint32(b[0:], 0x00010000)
	binary.BigEndian.PutUint32(b[12:], 0x5F0F3CF5)
	binary.BigEndian.PutUint16(b[headUnitsPerEmOffset:], upem)
	return b
}

// hheaWith is a 36-byte hhea table with the given ascender, descender and
// one long horizontal metric.
func hheaWith(ascender, descender int16) []byte {
	b := make([]byte, sizeofMetricsHeaderTable)
	binary.BigEndian.PutUint32(b[0:], 0x00010000)
	binary.BigEndian.PutUint16(b[hheaAscenderOffset:], uint16(ascender))
	binary.BigEndian.PutUint16(b[hheaDescenderOffset:], uint16(descender))
	binary.BigEndian.PutUint16(b[metricsHeaderCountOffset:], 1)
	return b
}

// fontMetricsHeight is louis14's FontMetrics height of the font at path —
// the rounded ascent plus the rounded descent, with no @font-face override —
// the FontMetrics Blink always has for a font.
func fontMetricsHeight(path string, size float64) float64 {
	return FontAscentFromFont(size, path, nil) + FontDescentFromFont(size, path, nil)
}

// verticalSummary prints v without its per-glyph tables.
func verticalSummary(v openTypeVerticalData) string {
	return fmt.Sprintf("{%d advance widths, %d advance heights, sizePerUnit %v, heightFallback %d}",
		len(v.advanceWidths), len(v.advanceHeights), v.sizePerUnit, v.heightFallback)
}

// TestHarfBuzzFace_VerticalDataWhenTablesDoNotParse (LOU-466 item 1): every
// font Blink lays out has FontMetrics, so HarfBuzzFontData::
// UpdateFallbackMetricsAndScale always gets a real ascent and descent for the
// height fallback (platform/fonts/shaping/harfbuzz_font_data.h:47-83 @
// 5992657f556bcd9d207a4aca187488462f027cab), and Skia reads the tables of the
// decoded typeface. louis14's typeface must therefore (a) decode a WOFF2 file
// at a builtin path before reading its tables, getting the same vertical data
// as the same file registered (decoded) as an @font-face; and (b) when a
// font's tables cannot be read at all (an unknown synthetic path), fall back
// to the font's FontMetrics height rather than 0, which stacked every upright
// glyph at one spot.
func TestHarfBuzzFace_VerticalDataWhenTablesDoNotParse(t *testing.T) {
	const size = 20.0
	vertical := func(path string) openTypeVerticalData {
		return fontPlatformData{fontPath: path, size: size}.GetHarfBuzzFace().VerticalData()
	}

	t.Run("WOFF2 at a builtin path", func(t *testing.T) {
		const file = "IcTestHalfWidth.woff2"
		registered := vertical(registerTestFace(t, NewFontRegistry(), "LOU466-Woff2", file, fetchCSSValuesResource))
		builtin := vertical("../visualtest/testdata/wpt-css3/css-values/resources/" + file)
		if registered.AdvanceHeight(1) == 0 {
			t.Fatal("fixture: the registered face's height fallback is 0")
		}
		if !reflect.DeepEqual(builtin, registered) {
			t.Errorf("vertical data from the WOFF2 file path = %s, want the decoded face's %s", verticalSummary(builtin), verticalSummary(registered))
		}
	})

	t.Run("unknown synthetic path", func(t *testing.T) {
		const path = "lou466-no-such-family-Regular.ttf"
		want := fontMetricsHeight(path, size)
		if want == 0 {
			t.Fatal("fixture: FontMetrics height is 0")
		}
		if got := vertical(path).AdvanceHeight(1); got != want {
			t.Errorf("AdvanceHeight (height fallback) = %v, want the FontMetrics height %v", got, want)
		}
		if inline, _ := MeasureTextVerticalFromFont("00", size, path); inline.Float64() != 2*want {
			t.Errorf("MeasureTextVerticalFromFont(\"00\") = %v, want %v", inline.Float64(), 2*want)
		}
	})
}

// TestHarfBuzzFace_UnitsPerEmOutsideFreeTypeRange (LOU-466 item 7): FreeType
// refuses a face whose head.unitsPerEm is outside [16, 16384]
// (sfnt_load_face, src/sfnt/sfobjs.c:977-985, FreeType @
// a5877f48fff8dd6cf8d53c505ce055499376aee2, Chromium's DEPS pin at
// 5992657f556bcd9d207a4aca187488462f027cab), so Skia never scales metrics by
// such a value — and never by HarfBuzz's clamped 1000 either. The font's
// tables are unusable, so the height fallback is the font's FontMetrics
// height (as for any unreadable font), not hhea scaled by the clamped upem:
// upem 10 with hhea 8/-2 at 20px is 0.16 + 0.04 → 0 under the clamp.
func TestHarfBuzzFace_UnitsPerEmOutsideFreeTypeRange(t *testing.T) {
	const family, size = "lou466-upem10", 20.0
	registerTypeface(family, 0, sfntWith(map[uint32][]byte{
		tagHead: headWithUnitsPerEm(10),
		tagHhea: hheaWith(8, -2),
	}))
	path := syntheticFontPath(family, 0)
	want := fontMetricsHeight(path, size)
	if got := (fontPlatformData{fontPath: path, size: size}).GetHarfBuzzFace().VerticalData().AdvanceHeight(1); got != want {
		t.Errorf("AdvanceHeight (height fallback) = %v, want the FontMetrics height %v", got, want)
	}
}

// TestRegisterTypeface_OwnsParsedTables (LOU-466 item 4): a registered font's
// typeface owns its parsed tables — as Blink's OpenTypeVerticalData copies the
// tables it reads out of the typeface (CopyOpenTypeTable,
// platform/fonts/opentype/open_type_vertical_data.cc:144-152 @
// 5992657f556bcd9d207a4aca187488462f027cab) — so the registration keeps no
// reference to the caller's font buffer. Clearing the buffer after
// registration must not change the face's vertical data.
func TestRegisterTypeface_OwnsParsedTables(t *testing.T) {
	data, err := os.ReadFile("../../fonts/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	vertical := func(family string) openTypeVerticalData {
		return fontPlatformData{fontPath: syntheticFontPath(family, 0), size: 20}.GetHarfBuzzFace().VerticalData()
	}
	registerTypeface("lou466-owned-intact", 0, append([]byte(nil), data...))
	want := vertical("lou466-owned-intact")

	registerTypeface("lou466-owned-cleared", 0, data)
	clear(data)
	if got := vertical("lou466-owned-cleared"); !reflect.DeepEqual(got, want) {
		t.Errorf("vertical data after clearing the registered buffer = %s, want %s", verticalSummary(got), verticalSummary(want))
	}
}

// BenchmarkMeasureTextVerticalFromFont (LOU-466 item 2) measures the upright
// advance of a run in a vmtx font, the line breaker's hot path.
func BenchmarkMeasureTextVerticalFromFont(b *testing.B) {
	data, err := os.ReadFile("../visualtest/testdata/wpt-css3/css-values/resources/ChTestShortZero.woff")
	if err != nil {
		b.Fatal(err)
	}
	fr := NewFontRegistry()
	path, err := fr.RegisterFontFace("LOU466-Bench", "ChTestShortZero.woff", "", "normal", "normal",
		func(string) ([]byte, error) { return data, nil })
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		MeasureTextVerticalFromFont("0000000000000000", 80.4, path)
	}
}
