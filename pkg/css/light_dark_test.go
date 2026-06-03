package css

import "testing"

// TestResolveLightDark verifies that resolveLightDark() selects the correct
// operand of a light-dark(<light>, <dark>) value based on the used color
// scheme. Mirrors Blink's CSSLightDarkValuePair::Resolve(), which returns the
// second value when used_color_scheme == kDark and the first otherwise
// (third_party/blink/renderer/core/css/css_light_dark_value_pair.cc
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func TestResolveLightDark(t *testing.T) {
	tests := []struct {
		name  string
		value string
		dark  bool
		want  string
		ok    bool
	}{
		{"light scheme picks first", "light-dark(green, red)", false, "green", true},
		{"dark scheme picks second", "light-dark(green, red)", true, "red", true},
		{"whitespace operands trimmed", "light-dark( green , red )", false, "green", true},
		{"currentcolor operand preserved", "light-dark(currentColor, currentColor)", false, "currentColor", true},
		{"nested comma in operand respected", "light-dark(rgb(0, 128, 0), red)", false, "rgb(0, 128, 0)", true},
		{"non light-dark value passes through", "green", false, "", false},
		{"uppercase function name", "LIGHT-DARK(green, red)", true, "red", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveLightDark(tt.value, tt.dark)
			if ok != tt.ok {
				t.Fatalf("resolveLightDark(%q, %v) ok=%v, want %v", tt.value, tt.dark, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("resolveLightDark(%q, %v) = %q, want %q", tt.value, tt.dark, got, tt.want)
			}
		})
	}
}

// TestUsedColorSchemeDark verifies the element's used color scheme accessor.
// Mirrors Blink's ComputedStyle::UsedColorScheme() — kDark only when the `dark`
// keyword is in the used scheme; absent/normal/light resolves to light
// (third_party/blink/renderer/core/style/computed_style.cc
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func TestUsedColorSchemeDark(t *testing.T) {
	tests := []struct {
		name   string
		scheme string // value of the color-scheme property ("" = unset)
		want   bool
	}{
		{"unset defaults light", "", false},
		{"normal is light", "normal", false},
		{"light keyword", "light", false},
		{"dark keyword", "dark", true},
		{"light dark list resolves light by default pref", "light dark", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStyle()
			if tt.scheme != "" {
				s.Set("color-scheme", tt.scheme)
			}
			if got := s.UsedColorSchemeDark(); got != tt.want {
				t.Errorf("color-scheme=%q UsedColorSchemeDark()=%v, want %v", tt.scheme, got, tt.want)
			}
		})
	}
}

// TestParseColorWithCurrentColorLightDark verifies that light-dark() resolves
// through ParseColorWithCurrentColor: the selected operand may itself be
// currentcolor, which then resolves against the supplied current color. This is
// the end-to-end path exercised by background-color: light-dark(currentColor,
// currentColor) (css-color/light-dark-currentcolor.html).
func TestParseColorWithCurrentColorLightDark(t *testing.T) {
	green := Color{R: 0, G: 128, B: 0, A: 1.0}
	got, ok := ParseColorWithCurrentColor("light-dark(currentColor, currentColor)", green)
	if !ok {
		t.Fatalf("ParseColorWithCurrentColor light-dark returned not ok")
	}
	if !colorEq(got, green, 1) {
		t.Errorf("light-dark(currentColor, currentColor) = rgb(%d,%d,%d), want green", got.R, got.G, got.B)
	}
}
