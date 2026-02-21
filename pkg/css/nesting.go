package css

import (
	"fmt"
	"math"
	"strings"
)

// colorspace.go — OKLCH, LCH, HWB color space conversions + color-mix()

// linearToSRGB applies sRGB gamma correction to a linear light value.
func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// colorClamp01 clamps a float64 to [0, 1].
func colorClamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// oklchToRGB converts OKLCH color to sRGB [0,1] values.
// L = lightness [0,1], C = chroma [0,~0.4], H = hue [0,360 degrees]
func oklchToRGB(L, C, H float64) (r, g, b float64) {
	// Step 1: OKLCH to OKLab
	hRad := H * math.Pi / 180
	a := C * math.Cos(hRad)
	bLab := C * math.Sin(hRad)

	// Step 2: OKLab to linear sRGB via OKLab matrices
	// OKLab to LMS (cube root space)
	l_ := L + 0.3963377774*a + 0.2158037573*bLab
	m_ := L - 0.1055613458*a - 0.0638541728*bLab
	s_ := L - 0.0894841775*a - 1.2914855480*bLab

	// Cube to get LMS
	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	// Step 3: LMS to linear sRGB
	r = +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

	// Step 4: Gamma correction (linear to sRGB)
	r = linearToSRGB(r)
	g = linearToSRGB(g)
	b = linearToSRGB(b)

	return colorClamp01(r), colorClamp01(g), colorClamp01(b)
}

// labF is the CIE Lab forward transfer function.
func labF(t float64) float64 {
	if t > 0.206897 {
		return t * t * t
	}
	return (t - 16.0/116) / 7.787
}

// lchToRGB converts CIELCh color to sRGB [0,1] values.
// L = lightness [0,100], C = chroma [0,~230], H = hue [0,360 degrees]
func lchToRGB(L, C, H float64) (r, g, b float64) {
	// LCH to Lab
	hRad := H * math.Pi / 180
	a := C * math.Cos(hRad)
	bLab := C * math.Sin(hRad)

	// Lab to XYZ (D65)
	fy := (L + 16) / 116
	fx := a/500 + fy
	fz := fy - bLab/200
	x := labF(fx) * 0.95047
	y := labF(fy) * 1.00000
	z := labF(fz) * 1.08883

	// XYZ to linear sRGB
	rl := 3.2406*x - 1.5372*y - 0.4986*z
	gl := -0.9689*x + 1.8758*y + 0.0415*z
	bl := 0.0557*x - 0.2040*y + 1.0570*z

	return colorClamp01(linearToSRGB(rl)), colorClamp01(linearToSRGB(gl)), colorClamp01(linearToSRGB(bl))
}

// hwbToRGB converts HWB color to sRGB [0,1] values.
// H = hue [0,360 degrees], W = whiteness [0,100 percent], B = blackness [0,100 percent]
func hwbToRGB(H, W, B float64) (r, g, b float64) {
	W /= 100
	B /= 100
	if W+B >= 1 {
		gray := W / (W + B)
		return gray, gray, gray
	}
	// Convert full-saturation hue to RGB, then apply whiteness/blackness
	r, g, b = hslHueToRGB(H)
	r = r*(1-W-B) + W
	g = g*(1-W-B) + W
	b = b*(1-W-B) + W
	return
}

// hslHueToRGB converts a hue angle (0-360) at full saturation (S=1, L=0.5) to RGB [0,1].
func hslHueToRGB(H float64) (r, g, b float64) {
	H = math.Mod(H, 360)
	if H < 0 {
		H += 360
	}
	// At S=1, L=0.5: c=1, m=0
	x := 1 - math.Abs(math.Mod(H/60, 2)-1)
	switch {
	case H < 60:
		r, g, b = 1, x, 0
	case H < 120:
		r, g, b = x, 1, 0
	case H < 180:
		r, g, b = 0, 1, x
	case H < 240:
		r, g, b = 0, x, 1
	case H < 300:
		r, g, b = x, 0, 1
	default:
		r, g, b = 1, 0, x
	}
	return
}

// parseColorMix parses a color-mix() CSS function and returns the mixed Color.
// Supports: color-mix(in colorspace, color1 [pct%], color2 [pct%])
// Mixing is always performed in sRGB regardless of the specified colorspace.
func parseColorMix(val string) (Color, bool) {
	// Find the outer parens: color-mix(...)
	start := len("color-mix(")
	end := strings.LastIndex(val, ")")
	if end <= start {
		return Color{}, false
	}
	inner := val[start:end]

	// Split on commas, depth-aware (to handle rgb(), hsl(), etc. inside)
	args := splitColorMixArgs(inner)
	if len(args) < 3 {
		return Color{}, false
	}

	// args[0] = "in oklch" or "in srgb" (colorspace, ignored for mixing)
	// args[1] = "red 30%" or just "red"
	// args[2] = "blue 70%" or just "blue"
	color1, pct1 := parseColorWithPercent(strings.TrimSpace(args[1]))
	color2, pct2 := parseColorWithPercent(strings.TrimSpace(args[2]))

	// Default percentages: 50/50
	if pct1 < 0 && pct2 < 0 {
		pct1, pct2 = 0.5, 0.5
	} else if pct1 < 0 {
		pct1 = 1 - pct2
	} else if pct2 < 0 {
		pct2 = 1 - pct1
	}

	// Normalize percentages so they sum to 1
	total := pct1 + pct2
	if total > 0 {
		pct1 /= total
		pct2 /= total
	}

	// Mix in sRGB
	r := uint8(math.Round(float64(color1.R)*pct1 + float64(color2.R)*pct2))
	g := uint8(math.Round(float64(color1.G)*pct1 + float64(color2.G)*pct2))
	b := uint8(math.Round(float64(color1.B)*pct1 + float64(color2.B)*pct2))
	a := color1.A*pct1 + color2.A*pct2

	return Color{r, g, b, a}, true
}

// splitColorMixArgs splits a color-mix() inner string by commas, respecting
// nested parentheses so that rgb(), hsl(), etc. inside are not split.
func splitColorMixArgs(s string) []string {
	var args []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '('  :
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, s[start:i])
				start = i + 1
			}
		}
	}
	args = append(args, s[start:])
	return args
}

// parseColorWithPercent parses a color token optionally followed by a percentage
// (e.g., "red 30%" or "blue"). Returns (color, percentage) where percentage is
// -1 if not specified.
func parseColorWithPercent(s string) (Color, float64) {
	pct := -1.0
	colorStr := s

	// Check if the string ends with a percentage like "30%"
	if idx := strings.LastIndex(s, " "); idx >= 0 {
		maybePct := strings.TrimSpace(s[idx+1:])
		if strings.HasSuffix(maybePct, "%") {
			var p float64
			n, _ := fmt.Sscanf(maybePct, "%f%%", &p)
			if n == 1 {
				pct = p / 100.0
				colorStr = strings.TrimSpace(s[:idx])
			}
		}
	}

	c, ok := ParseColor(colorStr)
	if !ok {
		return Color{}, pct
	}
	return c, pct
}
