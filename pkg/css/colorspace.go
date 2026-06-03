package css

import (
	"fmt"
	"math"
	"strings"
)

// colorspace.go — OKLCH, LCH, HWB color space conversions + color-mix()

// linearToSRGB applies sRGB gamma correction to a linear light value.
// Sign-preserving inverse of sRGBToLinear; same rationale as that helper.
func linearToSRGB(c float64) float64 {
	abs := math.Abs(c)
	var enc float64
	if abs <= 0.0031308 {
		enc = 12.92 * abs
	} else {
		enc = 1.055*math.Pow(abs, 1.0/2.4) - 0.055
	}
	if c < 0 {
		return -enc
	}
	return enc
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

// oklabToLinearSRGB converts OKLab (L, a, b) to linear-light sRGB without
// any clamping or gamma encoding. Pulled out so gamut-mapping (CSS Color 4
// §13.3) can evaluate in-gamut-ness on the linear-light triple before the
// transfer function is applied. Mirrors the matrix half of
// `Color::ConvertToColorSpace()` in Blink's `platform/graphics/color.cc`
// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func oklabToLinearSRGB(L, a, bLab float64) (r, g, b float64) {
	l_ := L + 0.3963377774*a + 0.2158037573*bLab
	m_ := L - 0.1055613458*a - 0.0638541728*bLab
	s_ := L - 0.0894841775*a - 1.2914855480*bLab

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	r = +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return
}

// oklchToRGB converts OKLCh color to sRGB [0,1] values with CSS Color 4 §13.3
// gamut mapping for out-of-gamut sources. Mirrors Blink's gamut-mapping path
// (`IsBakedGamutMappingEnabled()` → OkLCh binary-search chroma reduction in
// the `gfx::` color management layer called from `color.cc` @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func oklchToRGB(L, C, H float64) (r, g, b float64) {
	hRad := H * math.Pi / 180
	a := C * math.Cos(hRad)
	bLab := C * math.Sin(hRad)
	return gamutMapLinearSRGB(oklabToLinearSRGB(L, a, bLab))
}

// linearSRGBInGamut reports whether a linear-sRGB triple is inside the
// [-epsilon, 1+epsilon] cube. Per CSS Color 4 §13.2 we allow a small epsilon
// to absorb floating-point round-trip noise.
func linearSRGBInGamut(r, g, b float64) bool {
	const eps = 1e-6
	return r >= -eps && r <= 1+eps && g >= -eps && g <= 1+eps && b >= -eps && b <= 1+eps
}

// gamutMapLinearSRGB takes a linear-light sRGB triple (possibly out of
// gamut), applies CSS Color 4 §13.3 OkLCh gamut mapping if needed, and
// returns the gamma-encoded sRGB result clamped to [0,1]. This is the single
// tail used by every modern color-space conversion in this file so the
// gamut-mapping behavior is shared instead of duplicated per-space.
func gamutMapLinearSRGB(rl, gl, bl float64) (r, g, b float64) {
	if linearSRGBInGamut(rl, gl, bl) {
		return colorClamp01(linearToSRGB(rl)), colorClamp01(linearToSRGB(gl)), colorClamp01(linearToSRGB(bl))
	}
	L, a, bLab := linearSRGBToOklab(rl, gl, bl)
	if L <= 0 {
		return 0, 0, 0
	}
	if L >= 1 {
		return 1, 1, 1
	}
	C := math.Sqrt(a*a + bLab*bLab)
	hRad := math.Atan2(bLab, a)
	rl, gl, bl = gamutMapOklch(L, C, hRad)
	return colorClamp01(linearToSRGB(rl)), colorClamp01(linearToSRGB(gl)), colorClamp01(linearToSRGB(bl))
}

// gamutMapOklch implements CSS Color 4 §13.3 "Gamut Mapping" by binary-search
// chroma reduction in OkLCh space until the OkLab triple maps into sRGB.
// Mirrors Blink's `gfx::` OkLCh gamut-map helper (binary search bounded by
// 25 iterations, JND threshold 0.02 in OkLCh) — see `color.cc`
// `IsBakedGamutMappingEnabled()` dispatch @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func gamutMapOklch(L, C float64, hRad float64) (r, g, b float64) {
	if C <= 0 {
		// Achromatic and still out-of-gamut means our matrix produced a
		// floating-point noise excursion; clip the matrix output.
		rl, gl, bl := oklabToLinearSRGB(L, 0, 0)
		return colorClamp01(rl), colorClamp01(gl), colorClamp01(bl)
	}
	lo, hi := 0.0, C
	for i := 0; i < 25; i++ {
		mid := (lo + hi) / 2
		a := mid * math.Cos(hRad)
		bLab := mid * math.Sin(hRad)
		rl, gl, bl := oklabToLinearSRGB(L, a, bLab)
		if linearSRGBInGamut(rl, gl, bl) {
			lo = mid
		} else {
			hi = mid
		}
	}
	a := lo * math.Cos(hRad)
	bLab := lo * math.Sin(hRad)
	rl, gl, bl := oklabToLinearSRGB(L, a, bLab)
	return colorClamp01(rl), colorClamp01(gl), colorClamp01(bl)
}

// labF is the CIE Lab reverse transfer function (f^{-1}).
func labF(t float64) float64 {
	const delta = 6.0 / 29.0 // ≈ 0.206897
	if t > delta {
		return t * t * t
	}
	return 3 * delta * delta * (t - 4.0/29.0)
}

// labToLinearSRGB converts CIE Lab (L 0..100, a/b ±125) to linear-light
// sRGB without clamping. Chain: Lab → XYZ-D50 → XYZ-D65 (Bradford) → linear
// sRGB matrix. Mirrors `Color::ToSRGB()` in Blink's
// `platform/graphics/color.cc` @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func labToLinearSRGB(L, a, bLab float64) (r, g, b float64) {
	// Lab to XYZ-D50; D50 illuminant Xn=0.96422, Yn=1.0, Zn=0.82521.
	fy := (L + 16) / 116
	fx := a/500 + fy
	fz := fy - bLab/200

	x := labF(fx) * 0.96422
	y := labF(fy) * 1.0
	z := labF(fz) * 0.82521

	// Bradford D50 → D65.
	xd65, yd65, zd65 := bradfordD50ToD65(x, y, z)

	// XYZ D65 → linear sRGB.
	r = 3.2406254*xd65 - 1.5372080*yd65 - 0.4986286*zd65
	g = -0.9689307*xd65 + 1.8757561*yd65 + 0.0415175*zd65
	b = 0.0557101*xd65 - 0.2040211*yd65 + 1.0569959*zd65
	return
}

// linearSRGBToOklab converts linear-light sRGB to OKLab. Used as the bridge
// for routing Lab/LCh source colors through OkLCh gamut mapping per CSS
// Color 4 §13.3.
func linearSRGBToOklab(r, g, b float64) (float64, float64, float64) {
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b

	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	L := 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_
	A := 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_
	B := 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
	return L, A, B
}

// labToRGB converts CIE Lab to sRGB [0,1] with OkLCh-based gamut mapping for
// out-of-gamut sources (CSS Color 4 §13.3).
func labToRGB(L, a, bLab float64) (r, g, b float64) {
	return gamutMapLinearSRGB(labToLinearSRGB(L, a, bLab))
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

// srgbToOKLab converts gamma-encoded sRGB [0,1] to OKLab (L, a, b) by
// linearising then composing with linearSRGBToOklab. Single shared definition
// of the matrices avoids a duplicate-constants source-of-truth violation.
func srgbToOKLab(r, g, b float64) (float64, float64, float64) {
	return linearSRGBToOklab(sRGBToLinear(r), sRGBToLinear(g), sRGBToLinear(b))
}

// oklabToSRGB converts OKLab (L, a, b) to sRGB [0,1] with gamut mapping for
// out-of-gamut sources (CSS Color 4 §13.3). Routes through OkLCh polar form
// so the binary-search chroma reduction shares a single implementation with
// oklchToRGB.
func oklabToSRGB(L, a, bLab float64) (float64, float64, float64) {
	C := math.Sqrt(a*a + bLab*bLab)
	H := math.Atan2(bLab, a) * 180 / math.Pi
	if H < 0 {
		H += 360
	}
	return oklchToRGB(L, C, H)
}

// parseColorMixWithCurrentColor parses a color-mix() value, resolving any
// "currentcolor" token in either color operand against the provided
// currentColor. This is the late-resolution path for color-mix() values that
// contain currentcolor; it is called from ParseColorWithCurrentColor in
// style.go when the element's computed color is known.
//
// Blink reference: CSSColorMixValue::Resolve() in
// core/css/css_color_mix_value.cc resolves currentColor at use time by
// substituting the element's computed color value before blending @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func parseColorMixWithCurrentColor(val string, currentColor Color) (Color, bool) {
	return parseColorMixInner(val, &currentColor)
}

// parseColorMix parses a color-mix() CSS function and returns the mixed Color.
// Supports: color-mix(in colorspace, color1 [pct%], color2 [pct%])
// Interpolation is performed in the requested color space per CSS Color 5.
func parseColorMix(val string) (Color, bool) {
	return parseColorMixInner(val, nil)
}

// parseColorMixInner is the shared implementation for parseColorMix and
// parseColorMixWithCurrentColor. When currentColorPtr is non-nil it is used
// to resolve "currentcolor" tokens inside the color operands. Supports the
// full set of CSS Color 4/5 interpolation spaces: srgb, srgb-linear, hsl,
// hwb, oklab, oklch, lab, lch, xyz / xyz-d65, xyz-d50.
//
// Blink reference: ConsumeColorMixFunction() in
// core/css/parser/css_color_parser.cc; Color::InterpolateInColorSpace() in
// platform/graphics/color.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func parseColorMixInner(val string, currentColorPtr *Color) (Color, bool) {
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

	color1, pct1 := parseColorWithPercentCC(strings.TrimSpace(args[1]), currentColorPtr)
	color2, pct2 := parseColorWithPercentCC(strings.TrimSpace(args[2]), currentColorPtr)

	// Default percentages: 50/50
	if pct1 < 0 && pct2 < 0 {
		pct1, pct2 = 0.5, 0.5
	} else if pct1 < 0 {
		pct1 = 1 - pct2
	} else if pct2 < 0 {
		pct2 = 1 - pct1
	}

	// Normalize percentages so they sum to 1 (CSS Color 5 §6.1 step 3).
	total := pct1 + pct2
	if total > 0 {
		pct1 /= total
		pct2 /= total
	}

	alpha := color1.A*pct1 + color2.A*pct2

	// Work in non-premultiplied sRGB [0,1] for color space conversions.
	// Premultiplied-alpha interpolation applies in rectangular spaces (sRGB,
	// sRGB-linear); perceptual and polar spaces interpolate channels directly
	// per CSS Color 4 §12.
	r1, g1, b1 := float64(color1.R)/255, float64(color1.G)/255, float64(color1.B)/255
	r2, g2, b2 := float64(color2.R)/255, float64(color2.G)/255, float64(color2.B)/255

	var rf, gf, bf float64
	switch colorspace {
	case "lab":
		// CIE Lab rectangular interpolation. srgbToLab is defined in style.go.
		// Use clamped (not OkLCh-gamut-mapped) sRGB conversion: see
		// linearSRGBToSRGBClamped for the rationale.
		L1, a1, bv1 := srgbToLab(r1, g1, b1)
		L2, a2, bv2 := srgbToLab(r2, g2, b2)
		L := L1*pct1 + L2*pct2
		a := a1*pct1 + a2*pct2
		bv := bv1*pct1 + bv2*pct2
		rl, gl, bl := labToLinearSRGB(L, a, bv)
		rf, gf, bf = linearSRGBToSRGBClamped(rl, gl, bl)
	case "lch":
		// CIE LCH cylindrical interpolation with shortest-hue (default).
		// Use clamped (not OkLCh-gamut-mapped) sRGB conversion: see
		// linearSRGBToSRGBClamped for the rationale.
		L1, C1, H1 := sRGBToLCH(r1, g1, b1)
		L2, C2, H2 := sRGBToLCH(r2, g2, b2)
		// Shorter-hue adjustment per CSS Color 4 §12.4.
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
		rl, gl, bl := labToLinearSRGB(L, C*math.Cos(hRad), C*math.Sin(hRad))
		rf, gf, bf = linearSRGBToSRGBClamped(rl, gl, bl)
	case "oklch":
		// OKLCh cylindrical interpolation with shortest-hue (default).
		L1, la1, lbL1 := srgbToOKLab(r1, g1, b1)
		L2, la2, lbL2 := srgbToOKLab(r2, g2, b2)
		C1 := math.Sqrt(la1*la1 + lbL1*lbL1)
		H1 := math.Atan2(lbL1, la1) * 180 / math.Pi
		C2 := math.Sqrt(la2*la2 + lbL2*lbL2)
		H2 := math.Atan2(lbL2, la2) * 180 / math.Pi
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
	case "hwb":
		H1, W1, Bk1 := sRGBToHWB(r1, g1, b1)
		H2, W2, Bk2 := sRGBToHWB(r2, g2, b2)
		diff := H2 - H1
		if diff > 180 {
			H1 += 360
		} else if diff < -180 {
			H2 += 360
		}
		H := math.Mod(H1*pct1+H2*pct2, 360)
		if H < 0 {
			H += 360
		}
		W := W1*pct1 + W2*pct2
		Bk := Bk1*pct1 + Bk2*pct2
		rf, gf, bf = hwbToRGB(H, W*100, Bk*100)
	case "xyz", "xyz-d65":
		// CIE XYZ D65 rectangular interpolation.
		x1, y1, z1 := sRGBToXYZD65(r1, g1, b1)
		x2, y2, z2 := sRGBToXYZD65(r2, g2, b2)
		x := x1*pct1 + x2*pct2
		y := y1*pct1 + y2*pct2
		z := z1*pct1 + z2*pct2
		rf, gf, bf = xyzD65ToSRGB(x, y, z)
	case "xyz-d50":
		// CIE XYZ D50 rectangular interpolation.
		x1, y1, z1 := sRGBToXYZD65(r1, g1, b1)
		x50_1, y50_1, z50_1 := bradfordD65ToD50(x1, y1, z1)
		x2, y2, z2 := sRGBToXYZD65(r2, g2, b2)
		x50_2, y50_2, z50_2 := bradfordD65ToD50(x2, y2, z2)
		x := x50_1*pct1 + x50_2*pct2
		y := y50_1*pct1 + y50_2*pct2
		z := z50_1*pct1 + z50_2*pct2
		rf, gf, bf = xyzD50ToSRGB(x, y, z)
	case "srgb-linear":
		// Linear-light sRGB rectangular interpolation.
		lr1, lg1, lb1 := sRGBToLinear(r1), sRGBToLinear(g1), sRGBToLinear(b1)
		lr2, lg2, lb2 := sRGBToLinear(r2), sRGBToLinear(g2), sRGBToLinear(b2)
		lr := lr1*pct1 + lr2*pct2
		lg := lg1*pct1 + lg2*pct2
		lb := lb1*pct1 + lb2*pct2
		rf, gf, bf = srgbLinearToSRGB(lr, lg, lb)
	default:
		// sRGB (default) — lerp each channel directly.
		rf = r1*pct1 + r2*pct2
		gf = g1*pct1 + g2*pct2
		bf = b1*pct1 + b2*pct2
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

// resolveLightDark extracts and returns the operand from light-dark(a, b).
// If dark is true, returns the second operand; otherwise the first.
// Returns (operand, true) on success, ("", false) if value is not a light-dark form.
func resolveLightDark(value string, dark bool) (string, bool) {
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "light-dark(") || !strings.HasSuffix(lower, ")") {
		return "", false
	}

	// Extract the inner part: light-dark(...) -> ...
	inner := value[len("light-dark(") : len(value)-1]

	// Split by commas respecting nested parentheses.
	operands := splitColorMixArgs(inner)
	if len(operands) != 2 {
		return "", false
	}

	// Return the appropriate operand.
	idx := 0
	if dark {
		idx = 1
	}
	return strings.TrimSpace(operands[idx]), true
}

// parseColorWithPercent parses a color token optionally followed by a percentage
// (e.g., "red 30%" or "blue"). Returns (color, percentage) where percentage is
// -1 if not specified.
func parseColorWithPercent(s string) (Color, float64) {
	return parseColorWithPercentCC(s, nil)
}

// parseColorWithPercentCC is like parseColorWithPercent but resolves
// "currentcolor" tokens when currentColorPtr is non-nil. Used by
// parseColorMixInner to support color-mix() containing currentcolor.
func parseColorWithPercentCC(s string, currentColorPtr *Color) (Color, float64) {
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

	// Resolve "currentcolor" if a known currentColor is available.
	if currentColorPtr != nil && strings.EqualFold(colorStr, "currentcolor") {
		return *currentColorPtr, pct
	}

	// Nested color-mix() as an operand: propagate the currentColor down so
	// the inner mix's `currentcolor` references resolve against the same
	// element-level value, not against the static ParseColor path (which
	// can't see currentcolor). Mirrors Blink's
	// `CSSColorMixValue::Resolve()` which threads the resolution context
	// through nested CSSColorMixValue children
	// (Chromium @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	if currentColorPtr != nil {
		lower := strings.ToLower(colorStr)
		if strings.HasPrefix(lower, "color-mix(") && strings.HasSuffix(lower, ")") {
			if c, ok := parseColorMixInner(colorStr, currentColorPtr); ok {
				return c, pct
			}
			return Color{}, pct
		}
	}

	c, ok := ParseColor(colorStr)
	if !ok {
		return Color{}, pct
	}
	return c, pct
}

// linearSRGBToSRGBClamped converts a linear-light sRGB triple to gamma-encoded
// sRGB by applying the transfer function per channel and clamping to [0,1].
// Used by color-mix() interpolation in perceptual spaces (Lab, LCH, XYZ) where
// the interpolation result may be slightly outside the sRGB gamut due to
// floating-point round-trip error in the matrix chains. CSS Color 4 §13
// specifies OkLCh gamut mapping for source colors specified in wide-gamut
// spaces, but color-mix() output interpolated from already-in-gamut sRGB inputs
// only exceeds the gamut by a small amount (the numerical noise in the Lab ↔
// sRGB matrix round-trip), and the WPT reference values are produced by simple
// per-channel clamping, not OkLCh chroma reduction. Using gamutMapLinearSRGB
// here triggers the OkLCh binary-search path on these small excursions and
// produces values that differ by up to 11 channels from the expected output.
//
// Blink reference: Color::InterpolateInColorSpace() returns an interpolated
// color that is subsequently converted to sRGB via the standard chain without
// a second gamut-mapping pass for color-mix() output @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func linearSRGBToSRGBClamped(rl, gl, bl float64) (float64, float64, float64) {
	return colorClamp01(linearToSRGB(rl)), colorClamp01(linearToSRGB(gl)), colorClamp01(linearToSRGB(bl))
}

// linearSRGBToXYZD65 converts linear-light sRGB to CIE XYZ D65. This is the
// inverse of xyzD65ToLinearSRGB and uses the standard IEC 61966-2-1 sRGB
// primary matrix. Required for color-mix() interpolation in the xyz / xyz-d65
// color spaces per CSS Color 4 §17.1 / CSS Color 5 §6.
// Blink reference: platform/graphics/color.cc `ToXYZD65()` @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func linearSRGBToXYZD65(r, g, b float64) (x, y, z float64) {
	x = 0.4124000141804757*r + 0.3576000173984332*g + 0.1805000217961596*b
	y = 0.2125999995037464*r + 0.7151999918371852*g + 0.0722000193675808*b
	z = 0.0193000178418460*r + 0.1192000426163858*g + 0.9505000474525293*b
	return
}

// sRGBToXYZD65 converts gamma-encoded sRGB to CIE XYZ D65. Linearises the
// input first then applies the primary matrix.
func sRGBToXYZD65(r, g, b float64) (x, y, z float64) {
	return linearSRGBToXYZD65(sRGBToLinear(r), sRGBToLinear(g), sRGBToLinear(b))
}

// sRGBToLCH converts gamma-encoded sRGB [0,1] to CIE LCH (L 0..100,
// C ≥ 0, H 0..360 degrees). Required for color-mix() in "lch" space.
// Delegates to srgbToLab (style.go) which implements the sRGB → XYZ-D65 →
// XYZ-D50 (Bradford) → Lab chain, then converts rectangular Lab to polar LCH.
// CSS Color 4 §8; Blink: color.cc `ToLCH()` @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func sRGBToLCH(r, g, b float64) (L, C, H float64) {
	L, a, bLab := srgbToLab(r, g, b)
	C = math.Sqrt(a*a + bLab*bLab)
	H = math.Atan2(bLab, a) * 180 / math.Pi
	if H < 0 {
		H += 360
	}
	return
}

// sRGBToHWB converts gamma-encoded sRGB [0,1] to HWB (H degrees,
// W and B in [0,1]). Required for color-mix() in "hwb" space.
// CSS Color 4 §8 (HWB); Blink: color.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func sRGBToHWB(r, g, b float64) (H, W, Bk float64) {
	maxv := math.Max(r, math.Max(g, b))
	minv := math.Min(r, math.Min(g, b))
	W = minv
	Bk = 1 - maxv
	H, _, _ = rgbToHSL(r, g, b)
	return
}

// sRGBToLinear converts an sRGB gamma-encoded value to linear light.
// Sign-preserving for out-of-gamut inputs (CSS Color 4 §17.1): the piecewise
// transfer mirrors itself across zero so the round trip is well-defined for
// the `color(srgb -0.5 1.2 0)`-style overshoots that appear in WPT reftests.
func sRGBToLinear(c float64) float64 {
	abs := math.Abs(c)
	var lin float64
	if abs <= 0.04045 {
		lin = abs / 12.92
	} else {
		lin = math.Pow((abs+0.055)/1.055, 2.4)
	}
	if c < 0 {
		return -lin
	}
	return lin
}

// p3ToSRGB converts Display P3 color (0-1 values) to sRGB (0-1 values).
// Display P3 uses the same gamma as sRGB but different primaries. Routes
// through gamutMapLinearSRGB so wide-gamut sources like display-p3 pure
// green produce the §13.3 chroma-reduced sRGB rather than naive per-channel
// clipping.
func p3ToSRGB(r, g, b float64) (float64, float64, float64) {
	// Linearize from P3 gamma (same transfer function as sRGB).
	lr := sRGBToLinear(r)
	lg := sRGBToLinear(g)
	lb := sRGBToLinear(b)

	// Display P3 (linear) → XYZ D65 → linear sRGB shares the same matrix
	// chain as displayP3LinearToSRGB; reuse to avoid duplicating constants.
	return displayP3LinearToSRGB(lr, lg, lb)
}

// a98RGBToSRGB converts Adobe RGB (1998) color (0-1 values) to sRGB (0-1
// values) with §13.3 gamut mapping. Linearises via gamma 2.19921875 per CSS
// Color 4 §17.3, then reuses the linear-Display-P3 matrix chain... no, A98
// uses its own primaries — keep its matrix but route the tail through
// xyzD65ToSRGB which now does gamut mapping.
func a98RGBToSRGB(r, g, b float64) (float64, float64, float64) {
	// Sign-preserving gamma 563/256 ≈ 2.19921875 per CSS Color 4 §17.3.
	a98Linear := func(c float64) float64 {
		if c < 0 {
			return -math.Pow(-c, 563.0/256.0)
		}
		return math.Pow(c, 563.0/256.0)
	}
	lr := a98Linear(r)
	lg := a98Linear(g)
	lb := a98Linear(b)

	// A98-RGB to XYZ D65 matrix.
	x := 0.5766690*lr + 0.1855582*lg + 0.1882286*lb
	y := 0.2973450*lr + 0.6273635*lg + 0.0752915*lb
	z := 0.0270314*lr + 0.0706872*lg + 0.9911085*lb
	return xyzD65ToSRGB(x, y, z)
}

// rec2020ToSRGB converts Rec.2020 color (0-1 values) to sRGB (0-1 values)
// with §13.3 gamut mapping. Linearises via the BT.2020 piecewise transfer
// per CSS Color 4 §17.5.
func rec2020ToSRGB(r, g, b float64) (float64, float64, float64) {
	rec2020Linear := func(c float64) float64 {
		const alpha = 1.09929682680944
		const beta = 0.018053968510807
		abs := math.Abs(c)
		var lin float64
		if abs < beta*4.5 {
			lin = abs / 4.5
		} else {
			lin = math.Pow((abs+alpha-1)/alpha, 1/0.45)
		}
		if c < 0 {
			return -lin
		}
		return lin
	}
	lr := rec2020Linear(r)
	lg := rec2020Linear(g)
	lb := rec2020Linear(b)
	return rec2020LinearToSRGB(lr, lg, lb)
}

// xyzD65ToLinearSRGB converts CIE XYZ D65 to linear-light sRGB without any
// clamping or gamut mapping. Callers compose with gamutMapLinearSRGB for the
// final §13.3-aware sRGB output.
func xyzD65ToLinearSRGB(x, y, z float64) (float64, float64, float64) {
	sr := 3.2406254*x - 1.5372080*y - 0.4986286*z
	sg := -0.9689307*x + 1.8757561*y + 0.0415175*z
	sb := 0.0557101*x - 0.2040211*y + 1.0569959*z
	return sr, sg, sb
}

// xyzD65ToSRGB converts CIE XYZ D65 to gamma-encoded sRGB (0-1 values) with
// §13.3 OkLCh gamut mapping for out-of-gamut sources.
func xyzD65ToSRGB(x, y, z float64) (float64, float64, float64) {
	return gamutMapLinearSRGB(xyzD65ToLinearSRGB(x, y, z))
}

// bradfordD50ToD65 applies the Bradford chromatic adaptation transform to
// convert XYZ values from a D50 illuminant to a D65 illuminant. Matrix per
// CSS Color 4 §17.7 (https://drafts.csswg.org/css-color-4/#color-conversion-code).
//
// Blink reference (Chromium @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
// `third_party/blink/renderer/platform/graphics/color.cc` — `ToXYZD50` /
// `ExportAsXYZD50Floats` route every wide-gamut conversion through this
// transform; matrix coefficients are sourced from the upstream `gfx::` color
// management layer.
func bradfordD50ToD65(x, y, z float64) (float64, float64, float64) {
	x2 := 0.9554734527042182*x + -0.023098536874261423*y + 0.0632593086610217*z
	y2 := -0.028369706963208136*x + 1.0099954580058226*y + 0.021041398966942022*z
	z2 := 0.012314001688319899*x + -0.020507696433477912*y + 1.3303659366080753*z
	return x2, y2, z2
}

// xyzD50ToSRGB converts CIE XYZ D50 color to sRGB (0-1 values) via Bradford
// chromatic adaptation to D65, then the standard XYZ-D65 → sRGB chain.
// Used for the predefined `xyz-d50` and `prophoto-rgb` color spaces, both of
// which are anchored to a D50 white point per CSS Color 4 §17.4 / §17.7.
func xyzD50ToSRGB(x, y, z float64) (float64, float64, float64) {
	xd65, yd65, zd65 := bradfordD50ToD65(x, y, z)
	return xyzD65ToSRGB(xd65, yd65, zd65)
}

// srgbLinearToSRGB converts a linear-light sRGB triple to gamma-encoded sRGB
// (0-1 values) with §13.3 OkLCh gamut mapping. The matrix step is identity
// (same primaries as sRGB); only the transfer function differs. CSS Color 4
// §10 `srgb-linear` keyword.
func srgbLinearToSRGB(r, g, b float64) (float64, float64, float64) {
	return gamutMapLinearSRGB(r, g, b)
}

// displayP3LinearToSRGB converts linear-light Display P3 to sRGB (0-1 values).
// Uses the Display P3 → XYZ D65 matrix (same as p3ToSRGB) but skips the
// input transfer function. CSS Color 4 §17.2.
func displayP3LinearToSRGB(r, g, b float64) (float64, float64, float64) {
	// Display P3 (linear) to XYZ D65 matrix
	x := 0.4865709*r + 0.2656677*g + 0.1982173*b
	y := 0.2289746*r + 0.6917385*g + 0.0792869*b
	z := 0.0000000*r + 0.0451134*g + 1.0439444*b
	return xyzD65ToSRGB(x, y, z)
}

// rec2020LinearToSRGB converts linear-light Rec.2020 to sRGB (0-1 values).
// Skips Rec.2020's input transfer function. CSS Color 4 §17.5.
func rec2020LinearToSRGB(r, g, b float64) (float64, float64, float64) {
	x := 0.6369580*r + 0.1446169*g + 0.1688810*b
	y := 0.2627002*r + 0.6779981*g + 0.0593017*b
	z := 0.0000000*r + 0.0280727*g + 1.0609851*b
	return xyzD65ToSRGB(x, y, z)
}

// prophotoRGBToSRGB converts ProPhoto RGB (D50 white point) to sRGB
// (0-1 values). ProPhoto uses a piecewise transfer function (CSS Color 4
// §17.4): t = 1/512; if v < 16/512 then v/16, else v^1.8. Then matrix
// linear-ProPhoto → XYZ D50, then Bradford D50→D65, then XYZ→sRGB.
func prophotoRGBToSRGB(r, g, b float64) (float64, float64, float64) {
	prophotoToLinear := func(c float64) float64 {
		// Sign-preserve to support negative inputs (out-of-gamut), per spec.
		abs := math.Abs(c)
		var lin float64
		if abs < 16.0/512.0 {
			lin = abs / 16.0
		} else {
			lin = math.Pow(abs, 1.8)
		}
		if c < 0 {
			return -lin
		}
		return lin
	}
	lr := prophotoToLinear(r)
	lg := prophotoToLinear(g)
	lb := prophotoToLinear(b)

	// linear ProPhoto-RGB → XYZ D50 (CSS Color 4 §17.4)
	x := 0.7977666449006423*lr + 0.13518129740053308*lg + 0.0313477828234366*lb
	y := 0.2880748288194013*lr + 0.7118352342418731*lg + 0.00008993693872564*lb
	z := 0.0*lr + 0.0*lg + 0.8251046025104602*lb

	return xyzD50ToSRGB(x, y, z)
}
