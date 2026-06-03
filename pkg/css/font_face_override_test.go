package css

import (
	"testing"
)

// TestParseFontFaceOverrideDescriptors verifies that ascent-override,
// descent-override, and line-gap-override descriptors in @font-face rules
// are parsed per CSS Fonts 4 §6.1-§6.3: grammar `normal | <percentage [0,∞)>`.
// `normal` or absent → nil pointer. Non-negative percentage → pointer to
// (percent / 100). Negative → rejected (nil). Mirrors Blink's
// ConvertFontMetricOverrideValue in core/css/font_face.cc and
// ConsumeFontMetricOverride in core/css/parser/at_rule_descriptor_parser.cc
// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestParseFontFaceOverrideDescriptors(t *testing.T) {
	ptr := func(f float64) *float64 { return &f }

	tests := []struct {
		name        string
		css         string
		wantAscent  *float64
		wantDescent *float64
		wantLineGap *float64
	}{
		{
			name: "no override descriptors — all nil",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
			}`,
			wantAscent:  nil,
			wantDescent: nil,
			wantLineGap: nil,
		},
		{
			name: "ascent-override: 100%",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				ascent-override: 100%;
			}`,
			wantAscent:  ptr(1.0),
			wantDescent: nil,
			wantLineGap: nil,
		},
		{
			name: "descent-override: 50%",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				descent-override: 50%;
			}`,
			wantAscent:  nil,
			wantDescent: ptr(0.5),
			wantLineGap: nil,
		},
		{
			name: "line-gap-override: 0%",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				line-gap-override: 0%;
			}`,
			wantAscent:  nil,
			wantDescent: nil,
			wantLineGap: ptr(0.0),
		},
		{
			name: "all three overrides",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				ascent-override: 80%;
				descent-override: 20%;
				line-gap-override: 100%;
			}`,
			wantAscent:  ptr(0.8),
			wantDescent: ptr(0.2),
			wantLineGap: ptr(1.0),
		},
		{
			name: "normal keyword resets to nil",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				ascent-override: normal;
				descent-override: normal;
				line-gap-override: normal;
			}`,
			wantAscent:  nil,
			wantDescent: nil,
			wantLineGap: nil,
		},
		{
			name: "later normal resets earlier percentage",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				ascent-override: 50%;
				ascent-override: normal;
			}`,
			wantAscent:  nil,
			wantDescent: nil,
			wantLineGap: nil,
		},
		{
			name: "later percentage overrides earlier normal",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				ascent-override: normal;
				ascent-override: 120%;
			}`,
			wantAscent:  ptr(1.2),
			wantDescent: nil,
			wantLineGap: nil,
		},
		{
			name: "fractional percentage",
			css: `@font-face {
				font-family: Test;
				src: url(/fonts/Ahem.ttf);
				ascent-override: 87.5%;
			}`,
			wantAscent:  ptr(0.875),
			wantDescent: nil,
			wantLineGap: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tc.css, nil)
			if err != nil {
				t.Fatalf("ParseStylesheet: %v", err)
			}
			if len(ss.FontFaces) != 1 {
				t.Fatalf("expected 1 font-face rule, got %d", len(ss.FontFaces))
			}
			ff := ss.FontFaces[0]

			checkOverride := func(label string, got, want *float64) {
				t.Helper()
				if want == nil {
					if got != nil {
						t.Errorf("%s: want nil, got %v", label, *got)
					}
					return
				}
				if got == nil {
					t.Errorf("%s: want %v, got nil", label, *want)
					return
				}
				const eps = 1e-9
				if diff := *got - *want; diff < -eps || diff > eps {
					t.Errorf("%s: want %.6f, got %.6f", label, *want, *got)
				}
			}

			checkOverride("AscentOverride", ff.AscentOverride, tc.wantAscent)
			checkOverride("DescentOverride", ff.DescentOverride, tc.wantDescent)
			checkOverride("LineGapOverride", ff.LineGapOverride, tc.wantLineGap)
		})
	}
}
