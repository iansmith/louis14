package css

import (
	"math"
	"testing"
)

// colorEq returns true if two Color values are within `tol` per channel (0-255).
func colorEq(a, b Color, tol int) bool {
	diff := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return diff(a.R, b.R) <= tol &&
		diff(a.G, b.G) <= tol &&
		diff(a.B, b.B) <= tol &&
		math.Abs(a.A-b.A) <= float64(tol)/255
}

// TestParseColorMix_LCH verifies color-mix(in lch, ...) interpolation.
// Derived from color-mix-percents-01.html: expected rgb(68.4898%, 36.015%, 68.3102%)
// purple = #800080, plum = #DDA0DD.
func TestParseColorMix_LCH(t *testing.T) {
	tests := []struct {
		name  string
		input string
		wantR uint8
		wantG uint8
		wantB uint8
		tol   int
	}{
		{
			name:  "lch 50/50 purple/plum",
			input: "color-mix(in lch, purple 50%, plum 50%)",
			// css spec says rgb(68.4898%, 36.015%, 68.3102%) = rgb(175, 92, 174)
			wantR: 175, wantG: 92, wantB: 174,
			tol: 1,
		},
		{
			name:  "lch default weights purple/plum",
			input: "color-mix(in lch, purple, plum)",
			wantR: 175, wantG: 92, wantB: 174,
			tol: 1,
		},
		{
			name:  "lab 50/50 red/green",
			input: "color-mix(in lab, red, green)",
			// from color-mix-non-srgb-001: red+green in lab → rgb(161, 108, 0)
			wantR: 161, wantG: 108, wantB: 0,
			tol: 1,
		},
		{
			name:  "xyz 50/50 red/green",
			input: "color-mix(in xyz, red, green)",
			// from color-mix-non-srgb-001: rgb(188, 92, 0)
			wantR: 188, wantG: 92, wantB: 0,
			tol: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseColor(tt.input)
			if !ok {
				t.Fatalf("ParseColor(%q) returned not ok", tt.input)
			}
			want := Color{R: tt.wantR, G: tt.wantG, B: tt.wantB, A: 1.0}
			if !colorEq(got, want, tt.tol) {
				t.Errorf("ParseColor(%q) = rgb(%d,%d,%d), want rgb(%d,%d,%d) (tol %d)",
					tt.input, got.R, got.G, got.B, tt.wantR, tt.wantG, tt.wantB, tt.tol)
			}
		})
	}
}

// TestParseColorMixWithCurrentColor verifies that color-mix() resolves
// currentcolor tokens when called via ParseColorWithCurrentColor.
func TestParseColorMixWithCurrentColor(t *testing.T) {
	red := Color{R: 255, G: 0, B: 0, A: 1.0}
	green := Color{R: 0, G: 128, B: 0, A: 1.0}

	tests := []struct {
		name         string
		input        string
		currentColor Color
		wantR        uint8
		wantG        uint8
		wantB        uint8
		tol          int
	}{
		{
			// color-mix(in srgb, currentColor 50%, green) with currentColor=red
			// → mix(red 50%, green 50%) in srgb
			// = rgb(128, 64, 0)
			name:         "srgb currentColor 50% with red",
			input:        "color-mix(in srgb, currentColor 50%, green)",
			currentColor: red,
			wantR:        128, wantG: 64, wantB: 0,
			tol: 1,
		},
		{
			// color-mix(in srgb, currentcolor, white 75%) with currentColor=green
			// white 75% means pct(white)=0.75, pct(currentcolor)=0.25
			// green=rgb(0,128,0), white=rgb(255,255,255)
			// r=0*0.25+255*0.75=191, g=128*0.25+255*0.75=223, b=191
			name:         "srgb green+white75 with currentColor=green",
			input:        "color-mix(in srgb, currentcolor, white 75%)",
			currentColor: green,
			wantR:        191, wantG: 223, wantB: 191,
			tol: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseColorWithCurrentColor(tt.input, tt.currentColor)
			if !ok {
				t.Fatalf("ParseColorWithCurrentColor(%q) returned not ok", tt.input)
			}
			want := Color{R: tt.wantR, G: tt.wantG, B: tt.wantB, A: 1.0}
			if !colorEq(got, want, tt.tol) {
				t.Errorf("ParseColorWithCurrentColor(%q) = rgb(%d,%d,%d), want rgb(%d,%d,%d) (tol %d)",
					tt.input, got.R, got.G, got.B, tt.wantR, tt.wantG, tt.wantB, tt.tol)
			}
		})
	}
}
