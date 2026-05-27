package render

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"louis14/pkg/css"
)

// isGradientValue returns true if the CSS value is a gradient function.
func isGradientValue(val string) bool {
	lower := strings.ToLower(strings.TrimSpace(val))
	for _, prefix := range []string{"linear-gradient(", "radial-gradient(", "repeating-linear-gradient(", "repeating-radial-gradient(", "conic-gradient(", "repeating-conic-gradient("} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// gradientStop holds a single color stop.
type gradientStop struct {
	r, g, b   float64 // 0-1
	a         float64 // 0-1
	pos       float64 // position along gradient line in pixels (after resolution)
	posIsSet  bool
	isPercent bool // true if pos is a percentage (0-100) needing lineLen resolution
}

// parseLinearGradient parses a linear-gradient(...) value.
// Returns angle in degrees (0=to top, 90=to right, 180=to bottom, 270=to left),
// and the list of color stops.
func parseLinearGradient(val string) (angleDeg float64, stops []gradientStop, ok bool, repeating bool) {
	lower := strings.ToLower(strings.TrimSpace(val))
	var prefixLen int
	if strings.HasPrefix(lower, "repeating-linear-gradient(") {
		prefixLen = len("repeating-linear-gradient(")
		repeating = true
	} else if strings.HasPrefix(lower, "linear-gradient(") {
		prefixLen = len("linear-gradient(")
	} else {
		return 0, nil, false, false
	}
	inner := val[prefixLen:]
	if idx := strings.LastIndex(inner, ")"); idx >= 0 {
		inner = inner[:idx]
	}
	inner = strings.TrimSpace(inner)

	args := splitGradientArgs(inner)
	if len(args) == 0 {
		return 0, nil, false, false
	}

	// Default direction: to bottom (180 degrees).
	angleDeg = 180.0
	startIdx := 0

	first := strings.TrimSpace(args[0])
	firstLower := strings.ToLower(first)
	if strings.HasPrefix(firstLower, "to ") {
		direction := strings.TrimSpace(firstLower[3:])
		switch direction {
		case "bottom":
			angleDeg = 180
		case "top":
			angleDeg = 0
		case "right":
			angleDeg = 90
		case "left":
			angleDeg = 270
		case "bottom right", "right bottom":
			angleDeg = 135
		case "bottom left", "left bottom":
			angleDeg = 225
		case "top right", "right top":
			angleDeg = 45
		case "top left", "left top":
			angleDeg = 315
		}
		startIdx = 1
	} else if strings.HasSuffix(firstLower, "deg") {
		numStr := strings.TrimSuffix(firstLower, "deg")
		if f, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
			angleDeg = f
			startIdx = 1
		}
	} else if strings.HasSuffix(firstLower, "turn") {
		numStr := strings.TrimSuffix(firstLower, "turn")
		if f, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64); err == nil {
			angleDeg = f * 360
			startIdx = 1
		}
	}

	stops = make([]gradientStop, 0, len(args)-startIdx)
	for _, arg := range args[startIdx:] {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		expanded := parseColorStops(arg)
		stops = append(stops, expanded...)
	}

	if len(stops) < 2 {
		return 0, nil, false, false
	}
	return angleDeg, stops, true, repeating
}

// parseColorStops parses a color stop token, returning one or two
// gradientStop entries. CSS Images 4 §3.4.2 allows a single color stop to
// carry two positions ("blue 0% 50%"), which is shorthand for two adjacent
// stops with the same color at the two positions.
//
// Blink reference: third_party/blink/renderer/core/css/css_gradient_value.cc
// (Chromium @ 4883d11fef) — `CSSGradientValue::AddStops` emits a second
// CSSGradientColorStop when the parser supplies a second position.
func parseColorStops(s string) []gradientStop {
	s = strings.TrimSpace(s)
	colorStr, posStr := splitColorAndPosition(s)
	colorStr = strings.TrimSpace(colorStr)
	posStr = strings.TrimSpace(posStr)

	c, colorOk := css.ParseColor(colorStr)
	if !colorOk {
		return nil
	}
	base := gradientStop{
		r: float64(c.R) / 255.0,
		g: float64(c.G) / 255.0,
		b: float64(c.B) / 255.0,
		a: c.A,
	}
	if posStr == "" {
		return []gradientStop{base}
	}

	// Try to split posStr into two position tokens. We split on whitespace
	// and accept either one or two tokens; anything else falls back to
	// treating the whole token as a single position (existing behavior).
	posTokens := strings.Fields(posStr)
	if len(posTokens) == 2 {
		p1, ok1, pct1 := parseStopPosition(posTokens[0])
		p2, ok2, pct2 := parseStopPosition(posTokens[1])
		if ok1 && ok2 {
			s1 := base
			s1.pos = p1
			s1.posIsSet = true
			s1.isPercent = pct1
			s2 := base
			s2.pos = p2
			s2.posIsSet = true
			s2.isPercent = pct2
			return []gradientStop{s1, s2}
		}
	}

	if pos, ok, isPct := parseStopPosition(posStr); ok {
		base.pos = pos
		base.posIsSet = true
		base.isPercent = isPct
	}
	return []gradientStop{base}
}

// parseColorStop parses a single color stop like "green 25px", "red 50%",
// "#ff0000". Multi-position stops ("blue 0% 50%") are handled by the plural
// parseColorStops; this single-stop helper is retained for backwards
// compatibility with call sites that don't yet handle stop expansion.
func parseColorStop(s string) (gradientStop, bool) {
	stops := parseColorStops(s)
	if len(stops) == 0 {
		return gradientStop{}, false
	}
	return stops[0], true
}

// splitColorAndPosition splits "green 25px" into ("green", "25px").
func splitColorAndPosition(s string) (colorStr, posStr string) {
	s = strings.TrimSpace(s)

	// Color function like rgb(...), hsl(...), etc.
	if parenIdx := strings.Index(s, "("); parenIdx >= 0 {
		depth := 0
		closeIdx := -1
		for i := parenIdx; i < len(s); i++ {
			if s[i] == '(' {
				depth++
			} else if s[i] == ')' {
				depth--
				if depth == 0 {
					closeIdx = i
					break
				}
			}
		}
		if closeIdx >= 0 {
			return s[:closeIdx+1], strings.TrimSpace(s[closeIdx+1:])
		}
	}

	// Named color or hex: split on last space.
	// A color stop "green 25px" or just "green" with no position.
	// The color part has no spaces for named colors / hex.
	if idx := strings.Index(s, " "); idx >= 0 {
		return s[:idx], strings.TrimSpace(s[idx+1:])
	}
	return s, ""
}

// parseStopPosition parses "25px", "50%", "0" as a stop position.
// For percentage values, returns the percentage (0-100) and isPercent=true.
// For pixel values, returns the pixel value and isPercent=false.
func parseStopPosition(s string) (pos float64, ok bool, isPercent bool) {
	s = strings.TrimSpace(s)
	if s == "0" {
		return 0, true, false
	}
	if strings.HasSuffix(s, "px") {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64); err == nil {
			return f, true, false
		}
	}
	if strings.HasSuffix(s, "%") {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64); err == nil {
			return f, true, true
		}
	}
	return 0, false, false
}

// splitGradientArgs splits a gradient argument list respecting nested parens.
func splitGradientArgs(s string) []string {
	var parts []string
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
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// resolveStopPositions fills in unspecified stop positions by interpolation.
func resolveStopPositions(stops []gradientStop, lineLen float64) {
	n := len(stops)
	if n == 0 {
		return
	}
	// First resolve percentage stops to pixel positions.
	for i := range stops {
		if stops[i].posIsSet && stops[i].isPercent {
			stops[i].pos = stops[i].pos / 100.0 * lineLen
			stops[i].isPercent = false
		}
	}
	if !stops[0].posIsSet {
		stops[0].pos = 0
		stops[0].posIsSet = true
	}
	if !stops[n-1].posIsSet {
		stops[n-1].pos = lineLen
		stops[n-1].posIsSet = true
	}
	// Fill intermediate stops by interpolation.
	i := 1
	for i < n-1 {
		if stops[i].posIsSet {
			i++
			continue
		}
		// Find next set stop.
		j := i + 1
		for j < n && !stops[j].posIsSet {
			j++
		}
		// Interpolate between stops[i-1].pos and stops[j].pos.
		start := stops[i-1].pos
		end := stops[j].pos
		count := j - (i - 1)
		for k := i; k < j; k++ {
			stops[k].pos = start + (end-start)*float64(k-(i-1))/float64(count)
			stops[k].posIsSet = true
		}
		i = j
	}

	// CSS Images 3 §3.4: enforce monotonic non-decreasing positions. If a
	// stop's position is less than the largest position seen so far, clamp
	// it up to that position. This handles e.g.
	// `linear-gradient(yellow, blue 70%, green 0)` where the bare `0` must
	// be clamped to blue's 70%.
	//
	// Blink reference: third_party/blink/renderer/core/css/css_gradient_value.cc
	// `CSSGradientValue::AddStops` performs this same clamp after position
	// resolution (Chromium @ 4883d11fef).
	for k := 1; k < n; k++ {
		if stops[k].pos < stops[k-1].pos {
			stops[k].pos = stops[k-1].pos
		}
	}
}

// interpolateGradientColor returns the color at a given pixel position along the gradient line.
func interpolateGradientColor(stops []gradientStop, pos float64) color.RGBA {
	if len(stops) == 0 {
		return color.RGBA{A: 255}
	}
	if pos < stops[0].pos {
		s := &stops[0]
		return color.RGBA{R: uint8(s.r * 255), G: uint8(s.g * 255), B: uint8(s.b * 255), A: uint8(s.a * 255)}
	}
	last := &stops[len(stops)-1]
	if pos >= last.pos {
		return color.RGBA{R: uint8(last.r * 255), G: uint8(last.g * 255), B: uint8(last.b * 255), A: uint8(last.a * 255)}
	}
	for i := 1; i < len(stops); i++ {
		if pos <= stops[i].pos {
			// When pos equals a stop position exactly, advance past any later
			// stops at the same position per CSS Images §3.4.3: at a coincident
			// boundary the last color stop's color is used.
			if pos == stops[i].pos {
				for i+1 < len(stops) && stops[i+1].pos == pos {
					i++
				}
				s := &stops[i]
				return color.RGBA{R: uint8(s.r * 255), G: uint8(s.g * 255), B: uint8(s.b * 255), A: uint8(s.a * 255)}
			}
			a := &stops[i-1]
			b := &stops[i]
			span := b.pos - a.pos
			if span <= 0 {
				return color.RGBA{R: uint8(b.r * 255), G: uint8(b.g * 255), B: uint8(b.b * 255), A: uint8(b.a * 255)}
			}
			t := (pos - a.pos) / span
			rv := a.r + (b.r-a.r)*t
			gv := a.g + (b.g-a.g)*t
			bv := a.b + (b.b-a.b)*t
			av := a.a + (b.a-a.a)*t
			return color.RGBA{
				R: uint8(clamp01(rv) * 255),
				G: uint8(clamp01(gv) * 255),
				B: uint8(clamp01(bv) * 255),
				A: uint8(clamp01(av) * 255),
			}
		}
	}
	return color.RGBA{R: uint8(last.r * 255), G: uint8(last.g * 255), B: uint8(last.b * 255), A: uint8(last.a * 255)}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// drawLinearGradient renders a linear-gradient onto the target RGBA image
// within the box bounds [x, y, w, h].
func (r *Renderer) drawLinearGradient(gradVal string, x, y, w, h float64) {
	angleDeg, stops, ok, repeating := parseLinearGradient(gradVal)
	if !ok {
		return
	}

	// Compute gradient line length.
	angleRad := angleDeg * math.Pi / 180.0
	dx := math.Sin(angleRad)
	dy := -math.Cos(angleRad)

	var lineLen float64
	if math.Abs(dx) < 1e-6 {
		lineLen = h
	} else if math.Abs(dy) < 1e-6 {
		lineLen = w
	} else {
		lineLen = math.Abs(w*math.Sin(angleRad)) + math.Abs(h*math.Cos(angleRad))
	}
	if lineLen <= 0 {
		return
	}

	// For repeating gradients, the line length is determined by the stop
	// positions, not the box size. The pattern repeats to fill the box.
	// Per CSS Images 3 §3.4: the repeating gradient tiles the gradient line
	// between the first and last color stop positions.
	var repeatLen float64
	if repeating {
		// Resolve percentage stops against the full lineLen first.
		resolveStopPositions(stops, lineLen)
		repeatLen = stops[len(stops)-1].pos - stops[0].pos
		if repeatLen <= 0 {
			// Degenerate: all stops collapsed to a single position.
			// CSS Images 3 §3.4: when the gradient line has zero length,
			// the gradient renders as a solid color equal to the last
			// color stop's color (Blink css_gradient_value.cc handling
			// of degenerate repeating gradients @ 4883d11fef).
			last := stops[len(stops)-1]
			r.fillSolidGradient(last, x, y, w, h)
			return
		}
	}

	if !repeating {
		resolveStopPositions(stops, lineLen)
	}

	x0 := int(math.Round(x))
	y0 := int(math.Round(y))
	x1 := int(math.Round(x + w))
	y1 := int(math.Round(y + h))
	if x0 >= x1 || y0 >= y1 {
		return
	}

	// Clip drawing bounds to the active clip region (overflow:hidden, CSS clip).
	// The gg context's clip doesn't affect direct pixel writes, so we enforce
	// clip bounds ourselves.
	clipMinX, clipMinY, clipMaxX, clipMaxY := r.activeClipBounds()
	if x0 < clipMinX {
		x0 = clipMinX
	}
	if y0 < clipMinY {
		y0 = clipMinY
	}
	if x1 > clipMaxX {
		x1 = clipMaxX
	}
	if y1 > clipMaxY {
		y1 = clipMaxY
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}

	// Original (unclipped) gradient extent for position calculation.
	origX0 := int(math.Round(x))
	origY0 := int(math.Round(y))
	origX1 := int(math.Round(x + w))
	origY1 := int(math.Round(y + h))
	gradW := origX1 - origX0
	gradH := origY1 - origY0
	if gradW <= 0 || gradH <= 0 {
		return
	}

	// wrapPos wraps a gradient position for repeating gradients.
	// For repeating-linear-gradient, the pattern tiles between the first
	// and last stop positions.
	startPos := 0.0
	if repeating && len(stops) > 0 {
		startPos = stops[0].pos
	}
	wrapPos := func(pos float64) float64 {
		if !repeating {
			return pos
		}
		pos = pos - startPos
		pos = math.Mod(pos, repeatLen)
		if pos < 0 {
			pos += repeatLen
		}
		return pos + startPos
	}

	if math.Abs(dx) < 1e-6 {
		// Vertical gradient.
		for py := y0; py < y1; py++ {
			var pos float64
			if dy > 0 {
				pos = float64(py-origY0) / float64(gradH) * lineLen
			} else {
				pos = float64(origY1-1-py) / float64(gradH) * lineLen
			}
			c := interpolateGradientColor(stops, wrapPos(pos))
			if c.A == 0 {
				continue
			}
			for px := x0; px < x1; px++ {
				blendGradientPixel(r.target, px, py, c)
			}
		}
	} else if math.Abs(dy) < 1e-6 {
		// Horizontal gradient.
		for px := x0; px < x1; px++ {
			var pos float64
			if dx > 0 {
				pos = float64(px-origX0) / float64(gradW) * lineLen
			} else {
				pos = float64(origX1-1-px) / float64(gradW) * lineLen
			}
			c := interpolateGradientColor(stops, wrapPos(pos))
			if c.A == 0 {
				continue
			}
			for py := y0; py < y1; py++ {
				blendGradientPixel(r.target, px, py, c)
			}
		}
	} else {
		// Diagonal gradient: per-pixel projection.
		cx := x + w/2
		cy := y + h/2
		for py := y0; py < y1; py++ {
			for px := x0; px < x1; px++ {
				projX := float64(px) + 0.5 - cx
				projY := float64(py) + 0.5 - cy
				pos := projX*dx + projY*dy + lineLen/2
				c := interpolateGradientColor(stops, wrapPos(pos))
				if c.A == 0 {
					continue
				}
				blendGradientPixel(r.target, px, py, c)
			}
		}
	}
}

// blendGradientPixel composites a gradient color stop (which carries
// straight, non-premultiplied alpha as produced by interpolateGradientColor)
// onto an image.RGBA pixel using Porter-Duff source-over.
//
// interpolateGradientColor interpolates and returns straight-alpha colors,
// but image.RGBA stores premultiplied alpha — so a semi-transparent stop
// must be premultiplied and composited over whatever was painted behind
// the gradient layer (e.g. the element's background-color), not written
// raw. For a fully opaque stop (A == 255) this is exactly a replace, so
// opaque gradients are unaffected.
func blendGradientPixel(img *image.RGBA, px, py int, c color.RGBA) {
	if c.A == 255 {
		img.SetRGBA(px, py, c)
		return
	}
	sa := uint32(c.A)
	// Premultiply the source.
	sr := uint32(c.R) * sa / 255
	sg := uint32(c.G) * sa / 255
	sb := uint32(c.B) * sa / 255
	inv := uint32(255) - sa
	d := img.RGBAAt(px, py)
	img.SetRGBA(px, py, color.RGBA{
		R: uint8(sr + uint32(d.R)*inv/255),
		G: uint8(sg + uint32(d.G)*inv/255),
		B: uint8(sb + uint32(d.B)*inv/255),
		A: uint8(sa + uint32(d.A)*inv/255),
	})
}

// fillSolidGradient fills the box [x,y,w,h] with a single color derived from
// a gradient stop. Used for degenerate gradients (e.g. all stops at the same
// position) per CSS Images 3 §3.4. Respects active clip bounds and uses the
// same straight-alpha → premultiplied compositing path as the normal
// gradient painter.
func (r *Renderer) fillSolidGradient(s gradientStop, x, y, w, h float64) {
	x0 := int(math.Round(x))
	y0 := int(math.Round(y))
	x1 := int(math.Round(x + w))
	y1 := int(math.Round(y + h))
	if x0 >= x1 || y0 >= y1 {
		return
	}
	clipMinX, clipMinY, clipMaxX, clipMaxY := r.activeClipBounds()
	if x0 < clipMinX {
		x0 = clipMinX
	}
	if y0 < clipMinY {
		y0 = clipMinY
	}
	if x1 > clipMaxX {
		x1 = clipMaxX
	}
	if y1 > clipMaxY {
		y1 = clipMaxY
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}
	c := color.RGBA{
		R: uint8(clamp01(s.r) * 255),
		G: uint8(clamp01(s.g) * 255),
		B: uint8(clamp01(s.b) * 255),
		A: uint8(clamp01(s.a) * 255),
	}
	if c.A == 0 {
		return
	}
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			blendGradientPixel(r.target, px, py, c)
		}
	}
}

// resolveLhRlhInGradient substitutes `<n>lh` and `<n>rlh` dimension tokens in
// the gradient value string with their resolved px equivalents based on the
// supplied element style. Other `lh`/`rlh` occurrences (e.g. within identifier
// names) are not touched — we only substitute when the suffix is preceded by
// a number, matching CSS dimension-token syntax.
//
// Blink reference: third_party/blink/renderer/core/css/css_to_length_conversion_data.cc
// (Chromium @ 4883d11fef) — `LineHeightSize::Lh()` / `LineHeightSize::Rlh()`
// resolve `lh`/`rlh` against the element's font + line-height; in louis14 we
// resolve at parse time by textual substitution.
func resolveLhRlhInGradient(val string, style *css.Style) string {
	if style == nil {
		return val
	}
	if !strings.Contains(val, "lh") {
		return val
	}
	lh := style.GetLineHeight()
	// For rlh we want the root element's line-height. In louis14 we don't
	// currently have a cheap root-style accessor at the renderer; fall back
	// to the same line-height (which matches the spec for the root element
	// itself, and is the common case in our line-height tests).
	rlh := lh
	var out strings.Builder
	i := 0
	for i < len(val) {
		// Find the start of a numeric token.
		c := val[i]
		isNumStart := (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-'
		if !isNumStart {
			out.WriteByte(c)
			i++
			continue
		}
		// Scan numeric prefix.
		end := numericPrefixEnd(val[i:])
		if end == 0 {
			out.WriteByte(c)
			i++
			continue
		}
		numStr := val[i : i+end]
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			out.WriteString(numStr)
			i += end
			continue
		}
		// Check unit suffix: rlh has priority over lh because it includes lh
		// as a substring.
		rest := val[i+end:]
		switch {
		case strings.HasPrefix(rest, "rlh") && !isUnitContinuation(rest, 3):
			out.WriteString(strconv.FormatFloat(num*rlh, 'f', -1, 64))
			out.WriteString("px")
			i += end + 3
		case strings.HasPrefix(rest, "lh") && !isUnitContinuation(rest, 2):
			out.WriteString(strconv.FormatFloat(num*lh, 'f', -1, 64))
			out.WriteString("px")
			i += end + 2
		default:
			out.WriteString(numStr)
			i += end
		}
	}
	return out.String()
}

// isUnitContinuation reports whether the character after a unit suffix at
// position `unitLen` is itself an identifier character, which would mean
// the suffix is part of a longer identifier (and not a real unit token).
func isUnitContinuation(s string, unitLen int) bool {
	if unitLen >= len(s) {
		return false
	}
	c := s[unitLen]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-'
}

// numericPrefixEnd returns the index of the first character in s that is
// not part of a CSS numeric token (optional sign, digits, decimal point,
// optional scientific exponent). Mirrors css.numericPrefixEnd; duplicated
// here to keep gradient.go self-contained at the render layer.
func numericPrefixEnd(s string) int {
	end := 0
	if end < len(s) && (s[end] == '+' || s[end] == '-') {
		end++
	}
	sawDigit := false
	for end < len(s) {
		c := s[end]
		if c >= '0' && c <= '9' {
			sawDigit = true
			end++
		} else if c == '.' {
			end++
		} else {
			break
		}
	}
	if end < len(s) && (s[end] == 'e' || s[end] == 'E') {
		expStart := end
		end++
		if end < len(s) && (s[end] == '+' || s[end] == '-') {
			end++
		}
		expDigits := false
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			expDigits = true
			end++
		}
		if !expDigits {
			end = expStart
		}
	}
	if !sawDigit {
		return 0
	}
	return end
}

// drawGradient dispatches to the appropriate per-type gradient renderer
// based on the value's function prefix.
//
// Blink reference: third_party/blink/renderer/platform/graphics/gradient_generated_image.cc
// (Chromium @ 4883d11fef) — `GradientGeneratedImage::Draw` dispatches on
// `GetType()` internally. In louis14 we keep separate per-type renderers
// because each type's geometry (line, radii, angle) doesn't share enough
// structure to warrant a single Draw().
func (r *Renderer) drawGradient(gradVal string, x, y, w, h float64, style *css.Style) {
	resolved := resolveLhRlhInGradient(gradVal, style)
	lower := strings.ToLower(strings.TrimSpace(resolved))
	switch {
	case strings.HasPrefix(lower, "linear-gradient(") || strings.HasPrefix(lower, "repeating-linear-gradient("):
		r.drawLinearGradient(resolved, x, y, w, h)
	case strings.HasPrefix(lower, "radial-gradient(") || strings.HasPrefix(lower, "repeating-radial-gradient("):
		r.drawRadialGradient(resolved, x, y, w, h)
	case strings.HasPrefix(lower, "conic-gradient(") || strings.HasPrefix(lower, "repeating-conic-gradient("):
		r.drawConicGradient(resolved, x, y, w, h)
	}
}

// normalizeStops applies CSS Images 3 §3.5 + §3.6 color-stop normalization
// to a list of stops: monotonic-clamp, distribute-unspecified, fill-end-stops.
// Positions are interpreted as gradient-line offsets in pixels (already
// resolved against any axis length). Out-of-range stops (pos < 0 or
// pos > lineLen) are LEFT in the normalized list so the painter can
// extrapolate; only the visible region is clipped at draw time.
//
// Blink reference: third_party/blink/renderer/core/css/css_gradient_value.cc
// (Chromium @ 4883d11fef) — `CSSGradientValue::AddStops` calls
// `NormalizeAndAddStops` which performs the same monotonic clamp + span
// resolution.
func normalizeStops(stops []gradientStop, lineLen float64) {
	if len(stops) == 0 {
		return
	}
	// Resolve percentages first.
	for i := range stops {
		if stops[i].posIsSet && stops[i].isPercent {
			stops[i].pos = stops[i].pos / 100.0 * lineLen
			stops[i].isPercent = false
		}
	}
	if !stops[0].posIsSet {
		stops[0].pos = 0
		stops[0].posIsSet = true
	}
	n := len(stops)
	if !stops[n-1].posIsSet {
		stops[n-1].pos = lineLen
		stops[n-1].posIsSet = true
	}
	// Fill intermediate stops by linear interpolation between surrounding
	// positioned stops. CSS Images 3 §3.5.2.
	i := 1
	for i < n-1 {
		if stops[i].posIsSet {
			i++
			continue
		}
		j := i + 1
		for j < n && !stops[j].posIsSet {
			j++
		}
		start := stops[i-1].pos
		end := stops[j].pos
		count := j - (i - 1)
		for k := i; k < j; k++ {
			stops[k].pos = start + (end-start)*float64(k-(i-1))/float64(count)
			stops[k].posIsSet = true
		}
		i = j
	}
	// Monotonic clamp: enforce non-decreasing positions.
	for k := 1; k < n; k++ {
		if stops[k].pos < stops[k-1].pos {
			stops[k].pos = stops[k-1].pos
		}
	}
}

// parseRadialGradient parses radial-gradient(...) / repeating-radial-gradient(...).
// Returns center (cx, cy), radii (rx, ry), the stop list (raw, pre-normalization),
// the repeating flag, and ok.
//
// Blink reference: third_party/blink/renderer/core/css/css_gradient_value.cc
// (Chromium @ 4883d11fef) — `CSSGradientValue` for radial parses shape, size,
// position keyword, and stop list separately; we mirror that split.
func parseRadialGradient(val string, boxW, boxH float64) (cx, cy, rx, ry float64, stops []gradientStop, ok bool, repeating bool) {
	lower := strings.ToLower(strings.TrimSpace(val))
	var prefixLen int
	if strings.HasPrefix(lower, "repeating-radial-gradient(") {
		prefixLen = len("repeating-radial-gradient(")
		repeating = true
	} else if strings.HasPrefix(lower, "radial-gradient(") {
		prefixLen = len("radial-gradient(")
	} else {
		return 0, 0, 0, 0, nil, false, false
	}
	inner := val[prefixLen:]
	if idx := strings.LastIndex(inner, ")"); idx >= 0 {
		inner = inner[:idx]
	}
	inner = strings.TrimSpace(inner)
	args := splitGradientArgs(inner)
	if len(args) < 2 {
		return 0, 0, 0, 0, nil, false, false
	}

	// Defaults: ellipse, farthest-corner, center.
	shape := "ellipse"
	size := "farthest-corner"
	cxFrac := 0.5
	cyFrac := 0.5
	cxPx := math.NaN()
	cyPx := math.NaN()
	rxExplicit := 0.0
	ryExplicit := 0.0

	first := strings.TrimSpace(args[0])
	stopsStart := 0
	// The first arg may contain shape/size/position info, or it may be a
	// color stop. Try to detect by looking for shape/size keywords or
	// length-only tokens and "at <position>".
	configPart, posPart := splitOnAtKeyword(first)
	parsedConfig := false
	if configPart != "" {
		if parseRadialConfigPart(configPart, &shape, &size, &rxExplicit, &ryExplicit) {
			parsedConfig = true
		}
	}
	if posPart != "" {
		parseRadialPositionPart(posPart, boxW, boxH, &cxFrac, &cyFrac, &cxPx, &cyPx)
		parsedConfig = true
	}
	if parsedConfig {
		stopsStart = 1
	}

	// Parse color stops.
	stops = make([]gradientStop, 0, len(args)-stopsStart)
	for _, a := range args[stopsStart:] {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		expanded := parseColorStops(a)
		stops = append(stops, expanded...)
	}
	if len(stops) < 2 {
		return 0, 0, 0, 0, nil, false, false
	}

	// Resolve center to px.
	if !math.IsNaN(cxPx) {
		cx = cxPx
	} else {
		cx = cxFrac * boxW
	}
	if !math.IsNaN(cyPx) {
		cy = cyPx
	} else {
		cy = cyFrac * boxH
	}

	// Resolve radii.
	if rxExplicit > 0 {
		rx = rxExplicit
		ry = ryExplicit
		if ry == 0 {
			ry = rx
		}
	} else {
		rx, ry = computeRadialRadii(shape, size, cx, cy, boxW, boxH)
	}

	return cx, cy, rx, ry, stops, true, repeating
}

// splitOnAtKeyword splits a string on the first " at " keyword. The "at"
// keyword separates shape/size config from the position in
// radial-gradient/conic-gradient first-argument syntax.
func splitOnAtKeyword(s string) (config, pos string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "at ") {
		return "", strings.TrimSpace(s[3:])
	}
	if idx := strings.Index(s, " at "); idx >= 0 {
		return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+4:])
	}
	return s, ""
}

// parseRadialConfigPart parses the shape/size/explicit-radii portion of a
// radial-gradient first argument (i.e. the part before " at ").
// Returns true if any shape/size/length was consumed.
func parseRadialConfigPart(config string, shape, size *string, rx, ry *float64) bool {
	tokens := strings.Fields(config)
	if len(tokens) == 0 {
		return false
	}
	sizeKw := map[string]bool{
		"closest-side":   true,
		"farthest-side":  true,
		"closest-corner": true,
		"farthest-corner": true,
	}
	var lengthTokens []string
	hasShapeOrSize := false
	// We accumulate any unrecognized dimension tokens (e.g. `50cqi`, `50em`
	// at a render that doesn't resolve those) as "unknown length" so the
	// caller can fall back to a size keyword rather than misinterpreting
	// the syntax. This mirrors Blink's behavior of dropping unresolvable
	// units to the size-keyword default rather than rejecting the gradient.
	unknownLengths := 0
	for _, tok := range tokens {
		switch {
		case tok == "circle":
			*shape = "circle"
			hasShapeOrSize = true
		case tok == "ellipse":
			*shape = "ellipse"
			hasShapeOrSize = true
		case sizeKw[tok]:
			*size = tok
			hasShapeOrSize = true
		case strings.HasSuffix(tok, "px") || strings.HasSuffix(tok, "%"):
			lengthTokens = append(lengthTokens, tok)
			hasShapeOrSize = true
		default:
			if _, err := strconv.ParseFloat(tok, 64); err == nil {
				lengthTokens = append(lengthTokens, tok+"px")
				hasShapeOrSize = true
			} else if isDimensionLikeToken(tok) {
				// Looks like a dimension we don't resolve (cqi, cqh, em,
				// etc.). Count it but don't append; we'll fall back to a
				// keyword shape if no resolved length is present.
				unknownLengths++
				hasShapeOrSize = true
			} else {
				if !hasShapeOrSize {
					return false
				}
			}
		}
	}
	if len(lengthTokens) == 1 && unknownLengths == 0 {
		*shape = "circle"
		*size = ""
		*rx = parseRadialLength(lengthTokens[0])
		*ry = *rx
	} else if len(lengthTokens) == 1 && unknownLengths >= 1 {
		// One resolved + one unresolved length: fall back to using the
		// resolved length on both axes so the gradient still has a
		// reasonable shape. Best-effort; the true intent would require
		// resolving the unresolvable unit (container query, etc.).
		*size = ""
		*rx = parseRadialLength(lengthTokens[0])
		*ry = *rx
	} else if len(lengthTokens) == 2 {
		*shape = "ellipse"
		*size = ""
		*rx = parseRadialLength(lengthTokens[0])
		*ry = parseRadialLength(lengthTokens[1])
	}
	return hasShapeOrSize
}

// isDimensionLikeToken returns true if s syntactically looks like a CSS
// numeric dimension token (number followed by a letter-only unit).
// Examples: "50cqi" → true, "1lh" → true, "circle" → false. Used by the
// radial-gradient config parser to recognize and skip unresolvable
// length units rather than treating them as unrecognized non-length
// tokens.
func isDimensionLikeToken(s string) bool {
	end := numericPrefixEnd(s)
	if end == 0 || end == len(s) {
		return false
	}
	for i := end; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// parseRadialLength parses "50px" or "50%". Percentages are returned as
// negative numbers to mark them; caller resolves against box dimensions.
// Used only when an explicit radius is present.
func parseRadialLength(s string) float64 {
	if strings.HasSuffix(s, "px") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64)
		return v
	}
	if strings.HasSuffix(s, "%") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return -v
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// parseRadialPositionPart parses the "<position>" token after the "at"
// keyword. Sets the px-form (cxPx/cyPx) when an explicit px is given; sets
// the fraction-form (cxFrac/cyFrac) for percentages or keywords. The
// disambiguation between "px-form" and "fraction-form" is the same one used
// by Blink's GradientPosition resolution.
func parseRadialPositionPart(pos string, boxW, boxH float64, cxFrac, cyFrac *float64, cxPx, cyPx *float64) {
	tokens := strings.Fields(pos)
	if len(tokens) == 0 {
		return
	}
	set := func(tok string, isX bool) {
		switch tok {
		case "center":
			if isX {
				*cxFrac = 0.5
			} else {
				*cyFrac = 0.5
			}
		case "left":
			*cxFrac = 0
		case "right":
			*cxFrac = 1
		case "top":
			*cyFrac = 0
		case "bottom":
			*cyFrac = 1
		default:
			if strings.HasSuffix(tok, "%") {
				v, err := strconv.ParseFloat(strings.TrimSuffix(tok, "%"), 64)
				if err == nil {
					if isX {
						*cxFrac = v / 100.0
					} else {
						*cyFrac = v / 100.0
					}
				}
			} else if strings.HasSuffix(tok, "px") {
				v, err := strconv.ParseFloat(strings.TrimSuffix(tok, "px"), 64)
				if err == nil {
					if isX {
						*cxPx = v
					} else {
						*cyPx = v
					}
				}
			}
		}
	}
	if len(tokens) == 1 {
		t := tokens[0]
		if t == "center" {
			*cxFrac = 0.5
			*cyFrac = 0.5
		} else if t == "top" || t == "bottom" {
			set(t, false)
			*cxFrac = 0.5
		} else {
			set(t, true)
			*cyFrac = 0.5
		}
		return
	}
	set(tokens[0], true)
	set(tokens[1], false)
}

// computeRadialRadii returns rx, ry for the given size keyword. Cx/cy are
// the resolved center in box-local px coordinates; boxW/boxH are the
// rendering area dimensions.
//
// Blink reference: third_party/blink/renderer/core/css/css_gradient_value.cc
// (Chromium @ 4883d11fef) — `CSSGradientValue::ResolveRadius` (radial branch)
// computes the corner-shape radii via the EllipseRadius helper, which keeps
// the ellipse's aspect ratio equal to that of the corresponding
// closest-side/farthest-side rectangle and passes through the chosen corner:
//
//	a² = x² + y²·aspect²
//	rx = a, ry = a / aspect
//
// where (x, y) is the offset from center to the chosen corner and
// `aspect = sideX / sideY` (using the closest-side rectangle for
// closest-corner, farthest-side rectangle for farthest-corner).
func computeRadialRadii(shape, size string, cx, cy, boxW, boxH float64) (rx, ry float64) {
	dLeft := cx
	dRight := boxW - cx
	dTop := cy
	dBottom := boxH - cy
	switch size {
	case "closest-side":
		if shape == "circle" {
			r := math.Min(math.Min(dLeft, dRight), math.Min(dTop, dBottom))
			return r, r
		}
		return math.Min(dLeft, dRight), math.Min(dTop, dBottom)
	case "farthest-side":
		if shape == "circle" {
			r := math.Max(math.Max(dLeft, dRight), math.Max(dTop, dBottom))
			return r, r
		}
		return math.Max(dLeft, dRight), math.Max(dTop, dBottom)
	case "closest-corner":
		nearX := math.Min(dLeft, dRight)
		nearY := math.Min(dTop, dBottom)
		if shape == "circle" {
			r := math.Hypot(nearX, nearY)
			return r, r
		}
		// Ellipse with closest-side rectangle's aspect ratio.
		sideX := math.Min(dLeft, dRight)
		sideY := math.Min(dTop, dBottom)
		return ellipseRadius(nearX, nearY, sideX, sideY)
	default: // farthest-corner
		farX := math.Max(dLeft, dRight)
		farY := math.Max(dTop, dBottom)
		if shape == "circle" {
			r := math.Hypot(farX, farY)
			return r, r
		}
		// Ellipse with farthest-side rectangle's aspect ratio.
		sideX := math.Max(dLeft, dRight)
		sideY := math.Max(dTop, dBottom)
		return ellipseRadius(farX, farY, sideX, sideY)
	}
}

// ellipseRadius computes (rx, ry) for an ellipse that passes through
// offset (x, y) from center and whose aspect ratio matches sideX/sideY.
// Formula: a² = x² + y²·(sideX/sideY)², rx = a, ry = a · (sideY/sideX).
// Mirrors Blink's `EllipseRadius` in core/css/css_gradient_value.cc @
// 4883d11fef.
func ellipseRadius(x, y, sideX, sideY float64) (rx, ry float64) {
	if sideY <= 1e-9 {
		return 0, 0
	}
	aspect := sideX / sideY
	rx = math.Sqrt(x*x + y*y*aspect*aspect)
	if aspect <= 1e-9 {
		return rx, 0
	}
	ry = rx / aspect
	return rx, ry
}

// drawRadialGradient renders a radial-gradient (or repeating-radial-gradient)
// at the box (x, y, w, h). Stop positions resolve against the gradient line
// length, which is the principal radius rx (Blink's choice for ellipse
// gradients @ 4883d11fef).
func (r *Renderer) drawRadialGradient(gradVal string, x, y, w, h float64) {
	cx, cy, rx, ry, stops, ok, repeating := parseRadialGradient(gradVal, w, h)
	if !ok {
		return
	}
	// Translate center into screen coords.
	cx += x
	cy += y

	// Resolve stops against rx (the principal radius). For repeating,
	// percentages still resolve against rx, then the pattern cycles.
	normalizeStops(stops, rx)

	var repeatLen float64
	if repeating {
		repeatLen = stops[len(stops)-1].pos - stops[0].pos
		if repeatLen <= 1e-9 {
			// All stops collapsed: solid color = last stop.
			r.fillSolidGradient(stops[len(stops)-1], x, y, w, h)
			return
		}
	}

	x0 := int(math.Round(x))
	y0 := int(math.Round(y))
	x1 := int(math.Round(x + w))
	y1 := int(math.Round(y + h))
	if x0 >= x1 || y0 >= y1 {
		return
	}
	clipMinX, clipMinY, clipMaxX, clipMaxY := r.activeClipBounds()
	if x0 < clipMinX {
		x0 = clipMinX
	}
	if y0 < clipMinY {
		y0 = clipMinY
	}
	if x1 > clipMaxX {
		x1 = clipMaxX
	}
	if y1 > clipMaxY {
		y1 = clipMaxY
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}

	// rxRy ratio: the gradient is iso-distance ellipses (rx, ry) scaled with
	// the principal rx. For a point (px, py), the gradient parameter is
	// sqrt((dx/(rx/rx))^2 + (dy/(ry/rx*rx))^2) * rx
	// Equivalently: normalize (dx, dy) by (rx, ry), then the gradient
	// parameter is the magnitude scaled back by rx.
	if rx <= 1e-9 || ry <= 1e-9 {
		// Degenerate radii — paint as solid last stop.
		r.fillSolidGradient(stops[len(stops)-1], x, y, w, h)
		return
	}
	rxRy := rx / ry

	startPos := 0.0
	if repeating {
		startPos = stops[0].pos
	}
	wrapPos := func(pos float64) float64 {
		if !repeating {
			return pos
		}
		pos = pos - startPos
		pos = math.Mod(pos, repeatLen)
		if pos < 0 {
			pos += repeatLen
		}
		return pos + startPos
	}

	for py := y0; py < y1; py++ {
		dy := float64(py) + 0.5 - cy
		for px := x0; px < x1; px++ {
			dx := float64(px) + 0.5 - cx
			// Iso-distance ellipse: scale dy by rxRy so distance is in
			// units of rx along both axes.
			ed := math.Hypot(dx, dy*rxRy)
			c := interpolateGradientColor(stops, wrapPos(ed))
			if c.A == 0 {
				continue
			}
			blendGradientPixel(r.target, px, py, c)
		}
	}
}

// parseConicGradient parses conic-gradient(...) / repeating-conic-gradient(...).
// Returns center (cx, cy) in box-local px, from-angle in degrees, the stop
// list (with positions stored as fractions of one full turn), repeating
// flag, and ok.
//
// Blink reference: third_party/blink/renderer/core/css/css_gradient_value.cc
// (Chromium @ 4883d11fef) — `CSSGradientValue` conic parsing handles
// "[from <angle>]? [at <position>]?, <color-stop-list>".
func parseConicGradient(val string, boxW, boxH float64) (cx, cy, fromDeg float64, stops []gradientStop, ok bool, repeating bool) {
	lower := strings.ToLower(strings.TrimSpace(val))
	var prefixLen int
	if strings.HasPrefix(lower, "repeating-conic-gradient(") {
		prefixLen = len("repeating-conic-gradient(")
		repeating = true
	} else if strings.HasPrefix(lower, "conic-gradient(") {
		prefixLen = len("conic-gradient(")
	} else {
		return 0, 0, 0, nil, false, false
	}
	inner := val[prefixLen:]
	if idx := strings.LastIndex(inner, ")"); idx >= 0 {
		inner = inner[:idx]
	}
	inner = strings.TrimSpace(inner)
	args := splitGradientArgs(inner)
	if len(args) < 2 {
		return 0, 0, 0, nil, false, false
	}

	cxFrac := 0.5
	cyFrac := 0.5
	cxPx := math.NaN()
	cyPx := math.NaN()
	fromDeg = 0

	first := strings.TrimSpace(args[0])
	stopsStart := 0

	if strings.HasPrefix(first, "from ") || strings.Contains(first, " at ") || strings.HasPrefix(first, "at ") {
		remaining := first
		if strings.HasPrefix(remaining, "from ") {
			remaining = strings.TrimSpace(remaining[5:])
			if atIdx := strings.Index(remaining, " at "); atIdx >= 0 {
				angleStr := strings.TrimSpace(remaining[:atIdx])
				if deg, ok2 := parseAngleDegRender(angleStr); ok2 {
					fromDeg = deg
				}
				remaining = strings.TrimSpace(remaining[atIdx+4:])
			} else {
				if deg, ok2 := parseAngleDegRender(strings.TrimSpace(remaining)); ok2 {
					fromDeg = deg
				}
				remaining = ""
			}
		}
		if strings.HasPrefix(remaining, "at ") {
			remaining = strings.TrimSpace(remaining[3:])
			parseRadialPositionPart(remaining, boxW, boxH, &cxFrac, &cyFrac, &cxPx, &cyPx)
		} else if remaining != "" {
			parseRadialPositionPart(remaining, boxW, boxH, &cxFrac, &cyFrac, &cxPx, &cyPx)
		}
		stopsStart = 1
	}

	// Parse color stops with angle/percent positions. We re-use
	// parseConicColorStops which interprets "%" as fraction-of-turn and
	// angles in deg/grad/rad/turn.
	stops = make([]gradientStop, 0, len(args)-stopsStart)
	for _, a := range args[stopsStart:] {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		expanded := parseConicColorStops(a)
		stops = append(stops, expanded...)
	}
	if len(stops) < 2 {
		return 0, 0, 0, nil, false, false
	}

	if !math.IsNaN(cxPx) {
		cx = cxPx
	} else {
		cx = cxFrac * boxW
	}
	if !math.IsNaN(cyPx) {
		cy = cyPx
	} else {
		cy = cyFrac * boxH
	}
	return cx, cy, fromDeg, stops, true, repeating
}

// parseConicColorStops parses a single conic color-stop token, allowing one
// or two positions per stop (e.g. "black 0 25%" → two stops). Positions
// may be percentages (treated as fraction-of-turn) or angles in
// deg/grad/rad/turn. The returned stops carry their position in
// fraction-of-turn (0..1 nominal, can go outside for extrapolation), with
// isPercent=false because we've already converted to a uniform unit.
func parseConicColorStops(s string) []gradientStop {
	s = strings.TrimSpace(s)
	colorStr, posStr := splitColorAndPosition(s)
	colorStr = strings.TrimSpace(colorStr)
	posStr = strings.TrimSpace(posStr)
	c, colorOk := css.ParseColor(colorStr)
	if !colorOk {
		return nil
	}
	base := gradientStop{
		r: float64(c.R) / 255.0,
		g: float64(c.G) / 255.0,
		b: float64(c.B) / 255.0,
		a: c.A,
	}
	if posStr == "" {
		return []gradientStop{base}
	}
	tokens := strings.Fields(posStr)
	parsePos := func(tok string) (float64, bool) {
		// Bare "0" = 0% per CSS Images: a zero with no unit is permitted in
		// stop-position context.
		if tok == "0" {
			return 0, true
		}
		if strings.HasSuffix(tok, "%") {
			v, err := strconv.ParseFloat(strings.TrimSuffix(tok, "%"), 64)
			if err == nil {
				return v / 100.0, true
			}
			return 0, false
		}
		if deg, ok := parseAngleDegRender(tok); ok {
			return deg / 360.0, true
		}
		return 0, false
	}
	if len(tokens) == 2 {
		p1, ok1 := parsePos(tokens[0])
		p2, ok2 := parsePos(tokens[1])
		if ok1 && ok2 {
			s1 := base
			s1.pos = p1
			s1.posIsSet = true
			s2 := base
			s2.pos = p2
			s2.posIsSet = true
			return []gradientStop{s1, s2}
		}
	}
	if pos, ok := parsePos(posStr); ok {
		base.pos = pos
		base.posIsSet = true
	}
	return []gradientStop{base}
}

// parseAngleDegRender parses a CSS <angle> token returning degrees.
// Accepts deg, grad, rad, turn (CSS Values 3 §6.2).
func parseAngleDegRender(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	end := numericPrefixEnd(s)
	if end == 0 || end == len(s) {
		// Bare 0 isn't an angle, but accepted as zero in some contexts.
		if num, err := strconv.ParseFloat(s, 64); err == nil && num == 0 {
			return 0, true
		}
		return 0, false
	}
	num, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(s[end:]) {
	case "deg":
		return num, true
	case "grad":
		return num * 360 / 400, true
	case "rad":
		return num * 180 / math.Pi, true
	case "turn":
		return num * 360, true
	}
	return 0, false
}

// drawConicGradient renders a conic-gradient (or repeating-conic-gradient).
// Stop positions are in fraction-of-turn (0..1). For each pixel, the angle
// from center (clockwise from the from-angle, mod 1.0 for repeating) is
// computed and the color interpolated.
//
// Blink reference: third_party/blink/renderer/platform/graphics/gradient_generated_image.cc
// (Chromium @ 4883d11fef) — `GradientGeneratedImage::Draw` dispatches the
// conic-case inside the base class; the painter walks each pixel computing
// atan2 and interpolating.
func (r *Renderer) drawConicGradient(gradVal string, x, y, w, h float64) {
	cx, cy, fromDeg, stops, ok, repeating := parseConicGradient(gradVal, w, h)
	if !ok {
		return
	}
	cx += x
	cy += y

	// Stop positions are already in fraction-of-turn units; "lineLen" = 1.
	normalizeStops(stops, 1.0)

	var repeatLen float64
	if repeating {
		repeatLen = stops[len(stops)-1].pos - stops[0].pos
		if repeatLen <= 1e-9 {
			r.fillSolidGradient(stops[len(stops)-1], x, y, w, h)
			return
		}
	}

	x0 := int(math.Round(x))
	y0 := int(math.Round(y))
	x1 := int(math.Round(x + w))
	y1 := int(math.Round(y + h))
	if x0 >= x1 || y0 >= y1 {
		return
	}
	clipMinX, clipMinY, clipMaxX, clipMaxY := r.activeClipBounds()
	if x0 < clipMinX {
		x0 = clipMinX
	}
	if y0 < clipMinY {
		y0 = clipMinY
	}
	if x1 > clipMaxX {
		x1 = clipMaxX
	}
	if y1 > clipMaxY {
		y1 = clipMaxY
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}

	startPos := 0.0
	if repeating {
		startPos = stops[0].pos
	}
	wrapPos := func(pos float64) float64 {
		if !repeating {
			return pos
		}
		pos = pos - startPos
		pos = math.Mod(pos, repeatLen)
		if pos < 0 {
			pos += repeatLen
		}
		return pos + startPos
	}

	// from-angle in radians, measuring CCW from positive-x in math terms but
	// we want CW-from-top in CSS terms. The CSS-zero is "from <angle>"
	// pointing in the from direction; default from=0deg = up (12 o'clock).
	// For a point (px,py), the CSS angle is atan2(dx, -dy) - fromRad,
	// measured CW from the from direction, normalized to [0, 1) by
	// dividing by 2π. Then for non-repeating, positions outside [0,1) are
	// clamped via interpolateGradientColor's end-stop behavior; for
	// repeating, we cycle via wrapPos.
	fromRad := fromDeg * math.Pi / 180.0
	for py := y0; py < y1; py++ {
		dy := float64(py) + 0.5 - cy
		for px := x0; px < x1; px++ {
			dx := float64(px) + 0.5 - cx
			// CSS conic: 0 is at the top (-y), positive is clockwise.
			// In screen coords (y grows down), this is atan2(dx, -dy).
			ang := math.Atan2(dx, -dy) - fromRad
			// Normalize to [0, 2π).
			for ang < 0 {
				ang += 2 * math.Pi
			}
			for ang >= 2*math.Pi {
				ang -= 2 * math.Pi
			}
			pos := ang / (2 * math.Pi)
			c := interpolateGradientColor(stops, wrapPos(pos))
			if c.A == 0 {
				continue
			}
			blendGradientPixel(r.target, px, py, c)
		}
	}
}

