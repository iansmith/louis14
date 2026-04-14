package render

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"louis14/pkg/css"
)

// isGradientValue returns true if the CSS value is a gradient function.
func isGradientValue(val string) bool {
	lower := strings.ToLower(strings.TrimSpace(val))
	for _, prefix := range []string{"linear-gradient(", "radial-gradient(", "repeating-linear-gradient(", "repeating-radial-gradient(", "conic-gradient("} {
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
	isPercent bool    // true if pos is a percentage (0-100) needing lineLen resolution
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
		stop, stopOk := parseColorStop(arg)
		if stopOk {
			stops = append(stops, stop)
		}
	}

	if len(stops) < 2 {
		return 0, nil, false, false
	}
	return angleDeg, stops, true, repeating
}

// parseColorStop parses a color stop like "green 25px", "red 50%", "#ff0000".
func parseColorStop(s string) (gradientStop, bool) {
	s = strings.TrimSpace(s)

	colorStr, posStr := splitColorAndPosition(s)
	colorStr = strings.TrimSpace(colorStr)
	posStr = strings.TrimSpace(posStr)

	c, colorOk := css.ParseColor(colorStr)
	if !colorOk {
		return gradientStop{}, false
	}

	stop := gradientStop{
		r: float64(c.R) / 255.0,
		g: float64(c.G) / 255.0,
		b: float64(c.B) / 255.0,
		a: c.A,
	}
	if posStr != "" {
		if pos, posOk, isPct := parseStopPosition(posStr); posOk {
			stop.pos = pos
			stop.posIsSet = true
			stop.isPercent = isPct
		}
	}
	return stop, true
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
}

// interpolateGradientColor returns the color at a given pixel position along the gradient line.
func interpolateGradientColor(stops []gradientStop, pos float64) color.RGBA {
	if len(stops) == 0 {
		return color.RGBA{A: 255}
	}
	if pos <= stops[0].pos {
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
			// Degenerate: all stops at same position → use last stop color.
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
				r.target.SetRGBA(px, py, c)
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
				r.target.SetRGBA(px, py, c)
			}
		}
	} else {
		// Diagonal gradient: per-pixel projection.
		cx := x + w/2
		cy := y + h/2
		for py := y0; py < y1; py++ {
			for px := x0; px < x1; px++ {
				projX := float64(px)+0.5 - cx
				projY := float64(py)+0.5 - cy
				pos := projX*dx + projY*dy + lineLen/2
				c := interpolateGradientColor(stops, wrapPos(pos))
				if c.A == 0 {
					continue
				}
				r.target.SetRGBA(px, py, c)
			}
		}
	}
}
