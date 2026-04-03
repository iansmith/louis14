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
	r, g, b  float64 // 0-1
	a        float64 // 0-1
	pos      float64 // position along gradient line in pixels
	posIsSet bool
}

// parseLinearGradient parses a linear-gradient(...) value.
// Returns angle in degrees (0=to top, 90=to right, 180=to bottom, 270=to left),
// and the list of color stops.
func parseLinearGradient(val string) (angleDeg float64, stops []gradientStop, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(val))
	if !strings.HasPrefix(lower, "linear-gradient(") {
		return 0, nil, false
	}
	inner := val[len("linear-gradient("):]
	if idx := strings.LastIndex(inner, ")"); idx >= 0 {
		inner = inner[:idx]
	}
	inner = strings.TrimSpace(inner)

	args := splitGradientArgs(inner)
	if len(args) == 0 {
		return 0, nil, false
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
		return 0, nil, false
	}
	return angleDeg, stops, true
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
		if pos, posOk := parseStopPosition(posStr); posOk {
			stop.pos = pos
			stop.posIsSet = true
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

// parseStopPosition parses "25px", "0" as a pixel position.
func parseStopPosition(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "0" {
		return 0, true
	}
	if strings.HasSuffix(s, "px") {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64); err == nil {
			return f, true
		}
	}
	// % positions not supported yet — skip.
	return 0, false
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
	angleDeg, stops, ok := parseLinearGradient(gradVal)
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

	resolveStopPositions(stops, lineLen)

	x0 := int(math.Round(x))
	y0 := int(math.Round(y))
	x1 := int(math.Round(x + w))
	y1 := int(math.Round(y + h))
	if x0 >= x1 || y0 >= y1 {
		return
	}

	bounds := r.target.Bounds()
	imgX1 := bounds.Max.X
	imgY1 := bounds.Max.Y

	if math.Abs(dx) < 1e-6 {
		// Vertical gradient.
		for py := y0; py < y1 && py < imgY1; py++ {
			var pos float64
			if dy > 0 {
				pos = float64(py-y0) / float64(y1-y0) * lineLen
			} else {
				pos = float64(y1-1-py) / float64(y1-y0) * lineLen
			}
			c := interpolateGradientColor(stops, pos)
			if c.A == 0 {
				continue
			}
			for px := x0; px < x1 && px < imgX1; px++ {
				r.target.SetRGBA(px, py, c)
			}
		}
	} else if math.Abs(dy) < 1e-6 {
		// Horizontal gradient.
		for px := x0; px < x1 && px < imgX1; px++ {
			var pos float64
			if dx > 0 {
				pos = float64(px-x0) / float64(x1-x0) * lineLen
			} else {
				pos = float64(x1-1-px) / float64(x1-x0) * lineLen
			}
			c := interpolateGradientColor(stops, pos)
			if c.A == 0 {
				continue
			}
			for py := y0; py < y1 && py < imgY1; py++ {
				r.target.SetRGBA(px, py, c)
			}
		}
	} else {
		// Diagonal gradient: per-pixel projection.
		cx := x + w/2
		cy := y + h/2
		for py := y0; py < y1 && py < imgY1; py++ {
			for px := x0; px < x1 && px < imgX1; px++ {
				projX := float64(px)+0.5 - cx
				projY := float64(py)+0.5 - cy
				pos := projX*dx + projY*dy + lineLen/2
				c := interpolateGradientColor(stops, pos)
				if c.A == 0 {
					continue
				}
				r.target.SetRGBA(px, py, c)
			}
		}
	}
}
