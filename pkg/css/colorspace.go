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

// labF is the CIE Lab reverse transfer function (f^{-1}).
func labF(t float64) float64 {
	const delta = 6.0 / 29.0 // ≈ 0.206897
	if t > delta {
		return t * t * t
	}
	return 3 * delta * delta * (t - 4.0/29.0)
}

// labToRGB converts CIE Lab color to sRGB [0,1] values.
// L = lightness [0,100], a = green-red [-125,125], b = blue-yellow [-125,125]
// Conversion chain: Lab → XYZ-D50 → XYZ-D65 → linear-sRGB → sRGB
func labToRGB(L, a, bLab float64) (r, g, b float64) {
	// Lab to XYZ-D50
	// D50 illuminant: Xn=0.96422, Yn=1.0, Zn=0.82521
	fy := (L + 16) / 116
	fx := a/500 + fy
	fz := fy - bLab/200

	x := labF(fx) * 0.96422
	y := labF(fy) * 1.0
	z := labF(fz) * 0.82521

	// Chromatic adaptation: D50 → D65 (Bradford method)
	xd65 := 0.9555766*x + -0.0230393*y + 0.0631636*z
	yd65 := -0.0282895*x + 1.0099416*y + 0.0210077*z
	zd65 := 0.0122982*x + -0.0204830*y + 1.3299098*z

	// XYZ D65 to linear sRGB
	rl := 3.2406254*xd65 - 1.5372080*yd65 - 0.4986286*zd65
	gl := -0.9689307*xd65 + 1.8757561*yd65 + 0.0415175*zd65
	bl := 0.0557101*xd65 - 0.2040211*yd65 + 1.0569959*zd65

	return colorClamp01(linearToSRGB(rl)), colorClamp01(linearToSRGB(gl)), colorClamp01(linearToSRGB(bl))
}

// lchToRGB converts CIELCh color to sRGB [0,1] values.
// L = lightness [0,100], C = chroma [0,~230], H = hue [0,360 degrees]
// CSS Color 4: LCH is the polar form of CIE Lab, which uses D50.
// Chain: LCH → Lab → XYZ-D50 → XYZ-D65 (Bradford) → linear-sRGB → sRGB
func lchToRGB(L, C, H float64) (r, g, b float64) {
	// LCH to Lab (polar to rectangular)
	hRad := H * math.Pi / 180
	a := C * math.Cos(hRad)
	bLab := C * math.Sin(hRad)

	// Reuse labToRGB which does Lab → XYZ-D50 → D65 → sRGB correctly
	return labToRGB(L, a, bLab)
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

// srgbToOKLab converts sRGB [0,1] to OKLab (L, a, b).
func srgbToOKLab(r, g, b float64) (float64, float64, float64) {
	lr := sRGBToLinear(r)
	lg := sRGBToLinear(g)
	lb := sRGBToLinear(b)

	// linear sRGB to LMS
	l := 0.4122214708*lr + 0.5363325363*lg + 0.0514459929*lb
	m := 0.2119034982*lr + 0.6806995451*lg + 0.1073969566*lb
	s := 0.0883024619*lr + 0.2817188376*lg + 0.6299787005*lb

	// cube root
	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	L := 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_
	A := 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_
	B := 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
	return L, A, B
}

// oklabToSRGB converts OKLab (L, a, b) to sRGB [0,1].
func oklabToSRGB(L, a, bLab float64) (float64, float64, float64) {
	l_ := L + 0.3963377774*a + 0.2158037573*bLab
	m_ := L - 0.1055613458*a - 0.0638541728*bLab
	s_ := L - 0.0894841775*a - 1.2914855480*bLab

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	r := +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

	return colorClamp01(linearToSRGB(r)), colorClamp01(linearToSRGB(g)), colorClamp01(linearToSRGB(b))
}

// parseColorMix parses a color-mix() CSS function and returns the mixed Color.
// Supports: color-mix(in colorspace, color1 [pct%], color2 [pct%])
// Interpolation is performed in the requested color space per CSS Color 5.
func parseColorMix(val string) (Color, bool) {
	start := len("color-mix(")
	end := strings.LastIndex(val, ")")
	if end <= start {
		return Color{}, false
	}
	inner := val[start:end]

	args := splitColorMixArgs(inner)
	if len(args) < 3 {
		return Color{}, false
	}

	// Parse colorspace: "in srgb", "in oklch", "in hsl", etc.
	csArg := strings.TrimSpace(args[0])
	colorspace := "srgb"
	if strings.HasPrefix(csArg, "in ") {
		colorspace = strings.TrimSpace(csArg[3:])
		// Strip hue interpolation method if present (e.g., "oklch shorter hue")
		if idx := strings.Index(colorspace, " "); idx >= 0 {
			colorspace = colorspace[:idx]
		}
	}

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

	alpha := color1.A*pct1 + color2.A*pct2

	r1, g1, b1 := float64(color1.R)/255, float64(color1.G)/255, float64(color1.B)/255
	r2, g2, b2 := float64(color2.R)/255, float64(color2.G)/255, float64(color2.B)/255

	// CSS Color 5: premultiplied alpha interpolation.
	// Before interpolating, multiply color channels by alpha.
	a1, a2 := color1.A, color2.A
	r1 *= a1
	g1 *= a1
	b1 *= a1
	r2 *= a2
	g2 *= a2
	b2 *= a2

	var rf, gf, bf float64
	switch colorspace {
	case "oklch":
		// Convert premultiplied sRGB to OKLab, then polar (OKLCH), interpolate
		L1, la1, lbL1 := srgbToOKLab(r1, g1, b1)
		L2, la2, lbL2 := srgbToOKLab(r2, g2, b2)
		C1 := math.Sqrt(la1*la1 + lbL1*lbL1)
		H1 := math.Atan2(lbL1, la1) * 180 / math.Pi
		C2 := math.Sqrt(la2*la2 + lbL2*lbL2)
		H2 := math.Atan2(lbL2, la2) * 180 / math.Pi
		// Shorter hue interpolation (default)
		diff := H2 - H1
		if diff > 180 {
			H1 += 360
		} else if diff < -180 {
			H2 += 360
		}
		L := L1*pct1 + L2*pct2
		C := C1*pct1 + C2*pct2
		H := H1*pct1 + H2*pct2
		hRad := H * math.Pi / 180
		rf, gf, bf = oklabToSRGB(L, C*math.Cos(hRad), C*math.Sin(hRad))
	case "oklab":
		L1, la1, lbL1 := srgbToOKLab(r1, g1, b1)
		L2, la2, lbL2 := srgbToOKLab(r2, g2, b2)
		L := L1*pct1 + L2*pct2
		la := la1*pct1 + la2*pct2
		lbL := lbL1*pct1 + lbL2*pct2
		rf, gf, bf = oklabToSRGB(L, la, lbL)
	case "hsl":
		h1, s1, l1 := rgbToHSL(r1, g1, b1)
		h2, s2, l2 := rgbToHSL(r2, g2, b2)
		// Shorter hue interpolation
		diff := h2 - h1
		if diff > 180 {
			h1 += 360
		} else if diff < -180 {
			h2 += 360
		}
		h := math.Mod(h1*pct1+h2*pct2, 360)
		if h < 0 {
			h += 360
		}
		s := s1*pct1 + s2*pct2
		l := l1*pct1 + l2*pct2
		rf, gf, bf = hslToRGB(h, s, l)
	default:
		// sRGB (default) — lerp each premultiplied channel
		rf = r1*pct1 + r2*pct2
		gf = g1*pct1 + g2*pct2
		bf = b1*pct1 + b2*pct2
	}

	// Un-premultiply alpha from the result.
	if alpha > 0 {
		rf /= alpha
		gf /= alpha
		bf /= alpha
	}

	return Color{
		R: uint8(math.Round(colorClamp01(rf) * 255)),
		G: uint8(math.Round(colorClamp01(gf) * 255)),
		B: uint8(math.Round(colorClamp01(bf) * 255)),
		A: alpha,
	}, true
}

// rgbToHSL converts sRGB [0,1] to HSL (H in degrees, S and L in [0,1]).
func rgbToHSL(r, g, b float64) (float64, float64, float64) {
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l := (max + min) / 2
	if max == min {
		return 0, 0, l
	}
	d := max - min
	s := d / (1 - math.Abs(2*l-1))
	var h float64
	switch max {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	case b:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l
}

// hslToRGB converts HSL (H in degrees, S and L in [0,1]) to sRGB [0,1].
func hslToRGB(h, s, l float64) (float64, float64, float64) {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}

// splitColorMixArgs splits a color-mix() inner string by commas, respecting
// nested parentheses so that rgb(), hsl(), etc. inside are not split.
func splitColorMixArgs(s string) []string {
	var args []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
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

// sRGBToLinear converts an sRGB gamma-encoded value to linear light.
func sRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// p3ToSRGB converts Display P3 color (0-1 values) to sRGB (0-1 values).
// Display P3 uses the same gamma as sRGB but different primaries.
func p3ToSRGB(r, g, b float64) (float64, float64, float64) {
	// Linearize from P3 gamma (same encoding as sRGB)
	lr := sRGBToLinear(colorClamp01(r))
	lg := sRGBToLinear(colorClamp01(g))
	lb := sRGBToLinear(colorClamp01(b))

	// Display P3 to XYZ D65 matrix
	x := 0.4865709*lr + 0.2656677*lg + 0.1982173*lb
	y := 0.2289746*lr + 0.6917385*lg + 0.0792869*lb
	z := 0.0000000*lr + 0.0451134*lg + 1.0439444*lb

	// XYZ D65 to linear sRGB
	sr := 3.2406254*x - 1.5372080*y - 0.4986286*z
	sg := -0.9689307*x + 1.8757561*y + 0.0415175*z
	sb := 0.0557101*x - 0.2040211*y + 1.0569959*z

	// Apply sRGB gamma
	return colorClamp01(linearToSRGB(sr)), colorClamp01(linearToSRGB(sg)), colorClamp01(linearToSRGB(sb))
}

// a98RGBToSRGB converts Adobe RGB (1998) color (0-1 values) to sRGB (0-1 values).
func a98RGBToSRGB(r, g, b float64) (float64, float64, float64) {
	// Linearize from A98-RGB gamma (2.2)
	lr := math.Pow(colorClamp01(r), 2.2)
	lg := math.Pow(colorClamp01(g), 2.2)
	lb := math.Pow(colorClamp01(b), 2.2)

	// A98-RGB to XYZ D65 matrix
	x := 0.5766690*lr + 0.1855582*lg + 0.1882286*lb
	y := 0.2973450*lr + 0.6273635*lg + 0.0752915*lb
	z := 0.0270314*lr + 0.0706872*lg + 0.9911085*lb

	// XYZ D65 to linear sRGB
	sr := 3.2406254*x - 1.5372080*y - 0.4986286*z
	sg := -0.9689307*x + 1.8757561*y + 0.0415175*z
	sb := 0.0557101*x - 0.2040211*y + 1.0569959*z

	return colorClamp01(linearToSRGB(sr)), colorClamp01(linearToSRGB(sg)), colorClamp01(linearToSRGB(sb))
}

// rec2020ToSRGB converts Rec.2020 color (0-1 values) to sRGB (0-1 values).
func rec2020ToSRGB(r, g, b float64) (float64, float64, float64) {
	// Linearize from Rec.2020 gamma (approximately 2.2 for simplified version)
	rec2020Linear := func(c float64) float64 {
		c = colorClamp01(c)
		alpha := 1.09929682680944
		beta := 0.018053968510807
		if c < beta*4.5 {
			return c / 4.5
		}
		return math.Pow((c+alpha-1)/alpha, 1/0.45)
	}
	lr := rec2020Linear(r)
	lg := rec2020Linear(g)
	lb := rec2020Linear(b)

	// Rec.2020 to XYZ D65 matrix
	x := 0.6369580*lr + 0.1446169*lg + 0.1688810*lb
	y := 0.2627002*lr + 0.6779981*lg + 0.0593017*lb
	z := 0.0000000*lr + 0.0280727*lg + 1.0609851*lb

	// XYZ D65 to linear sRGB
	sr := 3.2406254*x - 1.5372080*y - 0.4986286*z
	sg := -0.9689307*x + 1.8757561*y + 0.0415175*z
	sb := 0.0557101*x - 0.2040211*y + 1.0569959*z

	return colorClamp01(linearToSRGB(sr)), colorClamp01(linearToSRGB(sg)), colorClamp01(linearToSRGB(sb))
}

// xyzD65ToSRGB converts CIE XYZ D65 color to sRGB (0-1 values).
func xyzD65ToSRGB(x, y, z float64) (float64, float64, float64) {
	// XYZ D65 to linear sRGB
	sr := 3.2406254*x - 1.5372080*y - 0.4986286*z
	sg := -0.9689307*x + 1.8757561*y + 0.0415175*z
	sb := 0.0557101*x - 0.2040211*y + 1.0569959*z

	return colorClamp01(linearToSRGB(sr)), colorClamp01(linearToSRGB(sg)), colorClamp01(linearToSRGB(sb))
}
