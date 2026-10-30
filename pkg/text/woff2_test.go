package text

import (
	"os"
	"testing"
)

// fetchCSSValuesResource is a FontFetcher over the committed WPT
// css-values/resources fonts (IcTest*.woff2 and friends).
func fetchCSSValuesResource(url string) ([]byte, error) {
	return os.ReadFile("../visualtest/testdata/wpt-css3/css-values/resources/" + url)
}

// TestRegisterFontFace_WOFF2 (LOU-415): a WOFF2 @font-face source decodes to
// a usable SFNT, so the face's own glyph advances are measured. The WPT
// IcTest fonts give 水 (U+6C34) a full-width (1em), half-width (0.5em) or zero
// advance. Mirrors Blink's DecodeFontWithIft kWoff2Tag branch
// (platform/fonts/web_font_decoder.cc:189 @
// 5992657f556bcd9d207a4aca187488462f027cab) → woff2::ConvertWOFF2ToTTF.
func TestRegisterFontFace_WOFF2(t *testing.T) {
	for _, tc := range []struct {
		file string
		want float64 // advance of 水 at 20px
	}{
		{"IcTestFullWidth.woff2", 20},
		{"IcTestHalfWidth.woff2", 10},
		{"IcTestZeroWidth.woff2", 0},
	} {
		fr := NewFontRegistry()
		path, err := fr.RegisterFontFace("LOU415-"+tc.file, tc.file, "woff2", "normal", "normal", fetchCSSValuesResource)
		if err != nil {
			t.Errorf("%s: RegisterFontFace: %v", tc.file, err)
			continue
		}
		if got, _ := MeasureText("水", 20, path); got.Float64() != tc.want {
			t.Errorf("%s: advance of 水 at 20px = %v, want %v", tc.file, got.Float64(), tc.want)
		}
	}
}

// TestWOFF2VariableLengthInts covers the WOFF2 §4.1 UIntBase128 and
// 255UInt16 encodings (woff2 src/variable_length.cc ReadBase128 /
// Read255UShort @ 7b67e12355fe7da148db5c19d4261f0a498a9537).
func TestWOFF2VariableLengthInts(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		want int
		ok   bool
	}{
		{[]byte{5}, 5, true},
		{[]byte{252}, 252, true},
		{[]byte{255, 0}, 253, true},
		{[]byte{254, 0}, 506, true},
		{[]byte{253, 0x01, 0x02}, 258, true},
		{[]byte{253, 0x01}, 0, false},
	} {
		got, ok := read255UShort(&woff2Buffer{data: tc.in})
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("read255UShort(% x) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	for _, tc := range []struct {
		in   []byte
		want uint32
		ok   bool
	}{
		{[]byte{0x3f}, 63, true},
		{[]byte{0x81, 0x00}, 128, true},
		{[]byte{0x8f, 0xff, 0xff, 0xff, 0x7f}, 0xffffffff, true},
		{[]byte{0x80, 0x01}, 0, false},                   // leading zero
		{[]byte{0x90, 0x80, 0x80, 0x80, 0x00}, 0, false}, // overflows 32 bits
		{[]byte{0x81, 0x81, 0x81, 0x81, 0x81, 0x01}, 0, false},
	} {
		got, ok := readBase128(&woff2Buffer{data: tc.in})
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("readBase128(% x) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestWOFF2TripletDecode covers one flag from each WOFF2 §5.2 triplet
// range; coordinates accumulate across points and bit 7 marks off-curve.
func TestWOFF2TripletDecode(t *testing.T) {
	flags := []byte{
		1,         // dx 0, dy +(0+5)
		10,        // dx -(0+3), dy 0
		20 | 0x80, // off-curve; b0=0, byte 0x12: dx -(1+0+1), dy -(1+0+2)
		84 + 1,    // b0=1: dx +(1+0+7), dy -(1+0+9)
		120 + 3,   // dx +(0x01<<4 | 0x2), dy +(0x3<<8 | 0x04)
		124 + 2,   // dx -(0x0102), dy +(0x0304)
	}
	in := []byte{5, 3, 0x12, 7, 9, 0x01, 0x23, 0x04, 0x01, 0x02, 0x03, 0x04}
	points := make([]woff2Point, len(flags))
	n, ok := tripletDecode(flags, in, points)
	if !ok || n != len(in) {
		t.Fatalf("tripletDecode consumed %d, ok=%v; want %d, true", n, ok, len(in))
	}
	want := []woff2Point{
		{0, 5, true},
		{-3, 5, true},
		{-5, 2, false},
		{3, -8, true},
		{3 + 0x12, -8 + 0x304, true},
		{3 + 0x12 - 0x102, -8 + 0x304 + 0x304, true},
	}
	for i := range want {
		if points[i] != want[i] {
			t.Errorf("point %d = %+v, want %+v", i, points[i], want[i])
		}
	}
}
