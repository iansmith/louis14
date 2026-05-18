package css

import (
	"math"
	"strconv"
	"strings"
)

// GradientType represents the type of CSS gradient
type GradientType int

const (
	GradientLinear GradientType = iota
	GradientRadial
	GradientRepeatingLinear
	GradientRepeatingRadial
	GradientConic
)

// ColorStop represents a color and its position in a gradient
type ColorStop struct {
	Color  Color
	Offset float64 // 0.0 to 1.0 (percentage as decimal)
}

// Gradient represents a CSS gradient
type Gradient struct {
	Type       GradientType
	Direction  string // "to right", "to bottom", "45deg", etc.
	ColorStops []ColorStop
	Repeating  bool    // true for repeating-linear/radial-gradient
	FromAngle  float64 // conic-gradient: starting angle in degrees
	// Radial-specific fields:
	Shape     string  // "circle" or "ellipse"
	Size      string  // "farthest-corner", "closest-side", etc. or ""
	RadiusX   float64 // explicit radius in px, 0 if using size keyword
	RadiusY   float64 // explicit Y radius for ellipse, 0 if circle
	CenterX   float64 // center X as fraction (0-1), default 0.5
	CenterY   float64 // center Y as fraction (0-1), default 0.5
	CenterXPx float64 // center X in px (if specified as px), -1 if not
	CenterYPx float64 // center Y in px (if specified as px), -1 if not
}

// ParseLinearGradient parses a linear-gradient() CSS value
// Example: "linear-gradient(to right, blue 0, blue 150px, red 150px, red 300px)"
func ParseLinearGradient(value string) (*Gradient, bool) {
	value = strings.TrimSpace(value)

	// Check if it's a linear-gradient
	if !strings.HasPrefix(value, "linear-gradient(") {
		return nil, false
	}
	if !strings.HasSuffix(value, ")") {
		return nil, false
	}

	// Extract the content
	content := value[len("linear-gradient(") : len(value)-1]

	// Split by commas (being careful about commas inside functions like rgb())
	parts := splitGradientParts(content)
	if len(parts) < 2 {
		return nil, false
	}

	grad := &Gradient{
		Type:       GradientLinear,
		ColorStops: make([]ColorStop, 0),
	}

	startIdx := 0

	// Check if first part is a direction
	firstPart := strings.TrimSpace(parts[0])
	if strings.HasPrefix(firstPart, "to ") || strings.HasSuffix(firstPart, "deg") {
		grad.Direction = firstPart
		startIdx = 1
	} else {
		// Default direction is "to bottom"
		grad.Direction = "to bottom"
	}

	// Parse color stops
	// Format can be: "color" or "color position"
	// Position can be: "50px", "50%", or absolute pixel values
	for i := startIdx; i < len(parts); i++ {
		stop, ok := parseColorStop(strings.TrimSpace(parts[i]))
		if !ok {
			return nil, false
		}
		grad.ColorStops = append(grad.ColorStops, stop)
	}

	if len(grad.ColorStops) < 2 {
		return nil, false
	}

	return grad, true
}

// parseColorStop parses a color stop like "blue 150px" or "red 50%"
func parseColorStop(stop string) (ColorStop, bool) {
	parts := strings.Fields(stop)
	if len(parts) == 0 {
		return ColorStop{}, false
	}

	// Parse the color (first part)
	color, ok := ParseColor(parts[0])
	if !ok {
		return ColorStop{}, false
	}

	cs := ColorStop{
		Color:  color,
		Offset: -1, // -1 means position not specified
	}

	// Parse the position if present
	if len(parts) >= 2 {
		pos := parts[1]
		if strings.HasSuffix(pos, "px") {
			// Absolute pixel position - we'll convert this to percentage later
			// based on the container size
			pxStr := strings.TrimSuffix(pos, "px")
			px, err := strconv.ParseFloat(pxStr, 64)
			if err == nil {
				cs.Offset = px // Store as negative to distinguish from percentage
			}
		} else if strings.HasSuffix(pos, "%") {
			pctStr := strings.TrimSuffix(pos, "%")
			pct, err := strconv.ParseFloat(pctStr, 64)
			if err == nil {
				cs.Offset = pct / 100.0 // Convert to 0-1 range
			}
		} else if num, err := strconv.ParseFloat(pos, 64); err == nil && num == 0 {
			// CSS: bare 0 is a valid length (= 0px = 0%) in all contexts
			cs.Offset = 0.0
		}
	}

	return cs, true
}

// splitGradientParts splits gradient content by commas, respecting parentheses
func splitGradientParts(content string) []string {
	var parts []string
	var current strings.Builder
	parenDepth := 0

	for _, ch := range content {
		if ch == '(' {
			parenDepth++
			current.WriteRune(ch)
		} else if ch == ')' {
			parenDepth--
			current.WriteRune(ch)
		} else if ch == ',' && parenDepth == 0 {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// ConvertPixelOffsetsToPercentages converts pixel-based offsets to percentages
// based on the gradient size in the specified direction
func (g *Gradient) ConvertPixelOffsetsToPercentages(width, height float64) {
	if g == nil || (g.Type != GradientLinear && g.Type != GradientRepeatingLinear) {
		return
	}

	// Determine gradient size based on direction
	var size float64
	switch g.Direction {
	case "to right", "to left":
		size = width
	case "to bottom", "to top":
		size = height
	default:
		// For angles or other directions, use the diagonal
		size = width // Simplified for now
	}

	if size == 0 {
		return
	}

	// Convert pixel offsets to percentages
	// Percentages are stored as 0-1 values, pixels are stored as actual pixel values (> 1)
	// -1 means the offset wasn't specified
	for i := range g.ColorStops {
		offset := g.ColorStops[i].Offset
		if offset < 0 {
			// Not specified, will be filled in later
			continue
		}
		if offset <= 1.0 {
			// Already a percentage (0-1 range)
			continue
		}
		// It's a pixel value (> 1) - convert to percentage
		g.ColorStops[i].Offset = offset / size
	}

	// Fill in missing offsets by distributing evenly
	g.fillMissingOffsets()
}

// fillMissingOffsets fills in any color stops that don't have explicit offsets
func (g *Gradient) fillMissingOffsets() {
	if len(g.ColorStops) == 0 {
		return
	}

	// If first stop has no offset, set it to 0
	if g.ColorStops[0].Offset < 0 {
		g.ColorStops[0].Offset = 0
	}

	// If last stop has no offset, set it to 1
	lastIdx := len(g.ColorStops) - 1
	if g.ColorStops[lastIdx].Offset < 0 {
		g.ColorStops[lastIdx].Offset = 1.0
	}

	// Fill in any missing offsets between defined ones
	for i := 0; i < len(g.ColorStops); i++ {
		if g.ColorStops[i].Offset < 0 {
			// Find the next defined offset
			nextIdx := i + 1
			for nextIdx < len(g.ColorStops) && g.ColorStops[nextIdx].Offset < 0 {
				nextIdx++
			}

			// Find the previous defined offset
			prevIdx := i - 1
			for prevIdx >= 0 && g.ColorStops[prevIdx].Offset < 0 {
				prevIdx--
			}

			// Interpolate
			if prevIdx >= 0 && nextIdx < len(g.ColorStops) {
				prevOffset := g.ColorStops[prevIdx].Offset
				nextOffset := g.ColorStops[nextIdx].Offset
				count := nextIdx - prevIdx
				step := (nextOffset - prevOffset) / float64(count)
				g.ColorStops[i].Offset = prevOffset + step*float64(i-prevIdx)
			}
		}
	}

	// CSS Images §3.4: if a color stop has a position less than any previous
	// stop's position, clamp it up to the previous stop's position.
	for i := 1; i < len(g.ColorStops); i++ {
		if g.ColorStops[i].Offset < g.ColorStops[i-1].Offset {
			g.ColorStops[i].Offset = g.ColorStops[i-1].Offset
		}
	}
}

// ParseRadialGradient parses a radial-gradient() CSS value
func ParseRadialGradient(value string) (*Gradient, bool) {
	value = strings.TrimSpace(value)

	if !strings.HasPrefix(value, "radial-gradient(") || !strings.HasSuffix(value, ")") {
		return nil, false
	}

	content := value[len("radial-gradient(") : len(value)-1]
	parts := splitGradientParts(content)
	if len(parts) < 2 {
		return nil, false
	}

	grad := &Gradient{
		Type:      GradientRadial,
		Shape:     "ellipse",
		Size:      "farthest-corner",
		CenterX:   0.5,
		CenterY:   0.5,
		CenterXPx: -1,
		CenterYPx: -1,
	}

	// The first part may contain shape/size/position info, or it may be a color stop.
	// Try to parse it as shape/size configuration.
	firstPart := strings.TrimSpace(parts[0])
	startIdx := 0

	// Split on " at " to separate shape/size from position
	configPart := firstPart
	positionPart := ""
	if idx := strings.Index(firstPart, " at "); idx >= 0 {
		configPart = strings.TrimSpace(firstPart[:idx])
		positionPart = strings.TrimSpace(firstPart[idx+4:])
	} else if strings.HasPrefix(firstPart, "at ") {
		positionPart = strings.TrimSpace(firstPart[3:])
		configPart = ""
	}

	// Try parsing the config part (shape/size keywords or explicit radii)
	parsedConfig := false
	if configPart != "" {
		parsedConfig = parseRadialConfig(configPart, grad)
	}
	if positionPart != "" {
		parseRadialPosition(positionPart, grad)
		parsedConfig = true
	}

	if parsedConfig {
		startIdx = 1
	}

	// Parse color stops
	for i := startIdx; i < len(parts); i++ {
		stop, ok := parseColorStop(strings.TrimSpace(parts[i]))
		if !ok {
			// If startIdx==1 and first color stop fails, the first part wasn't config
			if i == startIdx && startIdx == 1 && !parsedConfig {
				// Reset and try treating first part as a color stop
				startIdx = 0
				grad.Shape = "ellipse"
				grad.Size = "farthest-corner"
				grad.RadiusX = 0
				grad.RadiusY = 0
				stop2, ok2 := parseColorStop(strings.TrimSpace(parts[0]))
				if !ok2 {
					return nil, false
				}
				grad.ColorStops = append(grad.ColorStops, stop2)
				continue
			}
			return nil, false
		}
		grad.ColorStops = append(grad.ColorStops, stop)
	}

	if len(grad.ColorStops) < 2 {
		return nil, false
	}

	return grad, true
}

// parseRadialConfig parses the shape/size part of a radial gradient (before "at")
func parseRadialConfig(config string, grad *Gradient) bool {
	tokens := strings.Fields(config)
	if len(tokens) == 0 {
		return false
	}

	sizeKeywords := map[string]bool{
		"closest-side": true, "farthest-side": true,
		"closest-corner": true, "farthest-corner": true,
	}

	hasShapeOrSize := false
	var lengthTokens []string

	for _, tok := range tokens {
		switch {
		case tok == "circle":
			grad.Shape = "circle"
			hasShapeOrSize = true
		case tok == "ellipse":
			grad.Shape = "ellipse"
			hasShapeOrSize = true
		case sizeKeywords[tok]:
			grad.Size = tok
			hasShapeOrSize = true
		case strings.HasSuffix(tok, "px") || strings.HasSuffix(tok, "%"):
			lengthTokens = append(lengthTokens, tok)
			hasShapeOrSize = true
		default:
			// Try parsing as a bare number (px assumed)
			if _, err := strconv.ParseFloat(tok, 64); err == nil {
				lengthTokens = append(lengthTokens, tok+"px")
				hasShapeOrSize = true
			} else {
				// Unknown token — might be a color, so this isn't a config part
				if !hasShapeOrSize {
					return false
				}
			}
		}
	}

	// Process explicit length values
	if len(lengthTokens) == 1 {
		grad.Shape = "circle"
		grad.Size = ""
		grad.RadiusX = parseLengthValue(lengthTokens[0])
		grad.RadiusY = grad.RadiusX
	} else if len(lengthTokens) == 2 {
		grad.Shape = "ellipse"
		grad.Size = ""
		grad.RadiusX = parseLengthValue(lengthTokens[0])
		grad.RadiusY = parseLengthValue(lengthTokens[1])
	}

	return hasShapeOrSize
}

// parseLengthValue parses "50px" or "50%" returning the numeric value.
// Percentage values are returned as negative (e.g., "50%" -> -50).
func parseLengthValue(s string) float64 {
	if strings.HasSuffix(s, "px") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64)
		return v
	}
	if strings.HasSuffix(s, "%") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return -v // negative signals percentage
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// parseRadialPosition parses the position after "at" in a radial gradient
func parseRadialPosition(pos string, grad *Gradient) {
	tokens := strings.Fields(pos)
	if len(tokens) == 0 {
		return
	}

	parseOnePos := func(tok string, isX bool) {
		switch tok {
		case "center":
			if isX {
				grad.CenterX = 0.5
			} else {
				grad.CenterY = 0.5
			}
		case "left":
			grad.CenterX = 0
		case "right":
			grad.CenterX = 1
		case "top":
			grad.CenterY = 0
		case "bottom":
			grad.CenterY = 1
		default:
			if strings.HasSuffix(tok, "%") {
				v, err := strconv.ParseFloat(strings.TrimSuffix(tok, "%"), 64)
				if err == nil {
					if isX {
						grad.CenterX = v / 100.0
					} else {
						grad.CenterY = v / 100.0
					}
				}
			} else if strings.HasSuffix(tok, "px") {
				v, err := strconv.ParseFloat(strings.TrimSuffix(tok, "px"), 64)
				if err == nil {
					if isX {
						grad.CenterXPx = v
					} else {
						grad.CenterYPx = v
					}
				}
			}
		}
	}

	if len(tokens) == 1 {
		// Single value: applies to both axes if keyword, or X with Y=center
		tok := tokens[0]
		if tok == "center" {
			grad.CenterX = 0.5
			grad.CenterY = 0.5
		} else if tok == "top" || tok == "bottom" {
			parseOnePos(tok, false)
			grad.CenterX = 0.5
		} else {
			parseOnePos(tok, true)
			grad.CenterY = 0.5
		}
	} else {
		parseOnePos(tokens[0], true)
		parseOnePos(tokens[1], false)
	}
}

// ConvertRadialPixelOffsets converts pixel-based color stop offsets to percentages
// for radial gradients, using the gradient radius as the reference size.
func (g *Gradient) ConvertRadialPixelOffsets(radius float64) {
	if radius == 0 {
		return
	}
	for i := range g.ColorStops {
		offset := g.ColorStops[i].Offset
		if offset < 0 || offset <= 1.0 {
			continue
		}
		g.ColorStops[i].Offset = offset / radius
	}
	g.fillMissingOffsets()
}

// ComputeRadialRadii computes the actual rx, ry radii in pixels for a radial gradient
func (g *Gradient) ComputeRadialRadii(bgWidth, bgHeight float64) (float64, float64) {
	// Resolve center position
	cx := g.CenterX * bgWidth
	if g.CenterXPx >= 0 {
		cx = g.CenterXPx
	}
	cy := g.CenterY * bgHeight
	if g.CenterYPx >= 0 {
		cy = g.CenterYPx
	}

	// Explicit radii
	if g.Size == "" && g.RadiusX > 0 {
		rx := g.RadiusX
		ry := g.RadiusY
		if ry == 0 {
			ry = rx
		}
		// Handle percentage radii (stored as negative)
		if rx < 0 {
			rx = -rx / 100.0 * bgWidth
		}
		if ry < 0 {
			ry = -ry / 100.0 * bgHeight
		}
		return rx, ry
	}

	// Distance from center to each side
	dLeft := cx
	dRight := bgWidth - cx
	dTop := cy
	dBottom := bgHeight - cy

	switch g.Size {
	case "closest-side":
		if g.Shape == "circle" {
			r := math.Min(math.Min(dLeft, dRight), math.Min(dTop, dBottom))
			return r, r
		}
		// Ellipse: closest side in each axis
		rx := math.Min(dLeft, dRight)
		ry := math.Min(dTop, dBottom)
		return rx, ry

	case "farthest-side":
		if g.Shape == "circle" {
			r := math.Max(math.Max(dLeft, dRight), math.Max(dTop, dBottom))
			return r, r
		}
		rx := math.Max(dLeft, dRight)
		ry := math.Max(dTop, dBottom)
		return rx, ry

	case "closest-corner":
		// Distance to closest corner
		nearX := math.Min(dLeft, dRight)
		nearY := math.Min(dTop, dBottom)
		if g.Shape == "circle" {
			r := math.Hypot(nearX, nearY)
			return r, r
		}
		// Ellipse: scale so that the ellipse passes through the closest corner
		// with the same aspect ratio as the box
		dist := math.Hypot(nearX, nearY)
		ratio := nearY / nearX
		rx := dist / math.Sqrt(1+ratio*ratio)
		ry := rx * ratio
		return rx, ry

	default: // "farthest-corner"
		farX := math.Max(dLeft, dRight)
		farY := math.Max(dTop, dBottom)
		if g.Shape == "circle" {
			r := math.Hypot(farX, farY)
			return r, r
		}
		// Ellipse: scale so that the ellipse passes through the farthest corner
		dist := math.Hypot(farX, farY)
		if farX == 0 {
			return 0, dist
		}
		ratio := farY / farX
		rx := dist / math.Sqrt(1+ratio*ratio)
		ry := rx * ratio
		return rx, ry
	}
}

// ParseConicGradient parses a conic-gradient() CSS value
func ParseConicGradient(value string) (*Gradient, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "conic-gradient(") || !strings.HasSuffix(value, ")") {
		return nil, false
	}
	content := value[len("conic-gradient(") : len(value)-1]
	parts := splitGradientParts(content)
	if len(parts) < 2 {
		return nil, false
	}

	grad := &Gradient{
		Type:      GradientConic,
		CenterX:   0.5,
		CenterY:   0.5,
		CenterXPx: -1,
		CenterYPx: -1,
		FromAngle: 0,
	}

	startIdx := 0
	firstPart := strings.TrimSpace(parts[0])

	// Parse optional "from <angle>" and "at <position>"
	if strings.HasPrefix(firstPart, "from ") || strings.Contains(firstPart, " at ") || strings.HasPrefix(firstPart, "at ") {
		remaining := firstPart
		if strings.HasPrefix(remaining, "from ") {
			remaining = remaining[5:]
			// Extract angle
			if atIdx := strings.Index(remaining, " at "); atIdx >= 0 {
				angleStr := strings.TrimSpace(remaining[:atIdx])
				grad.FromAngle = parseAngleDeg(angleStr)
				remaining = remaining[atIdx+4:]
			} else {
				angleStr := strings.TrimSpace(remaining)
				grad.FromAngle = parseAngleDeg(angleStr)
				remaining = ""
			}
		}
		if strings.HasPrefix(remaining, "at ") {
			remaining = remaining[3:]
			parseRadialPosition(strings.TrimSpace(remaining), grad)
		} else if remaining != "" {
			parseRadialPosition(strings.TrimSpace(remaining), grad)
		}
		startIdx = 1
	}

	// Parse color stops with angle positions
	for i := startIdx; i < len(parts); i++ {
		stop, ok := parseConicColorStop(strings.TrimSpace(parts[i]))
		if !ok {
			return nil, false
		}
		grad.ColorStops = append(grad.ColorStops, stop)
	}

	if len(grad.ColorStops) < 2 {
		return nil, false
	}

	// Fill missing offsets
	grad.fillMissingOffsets()

	return grad, true
}

func parseAngleDeg(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "deg") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "deg"), 64)
		if err == nil {
			return v
		}
	}
	if strings.HasSuffix(s, "turn") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "turn"), 64)
		if err == nil {
			return v * 360
		}
	}
	if strings.HasSuffix(s, "rad") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "rad"), 64)
		if err == nil {
			return v * 180 / math.Pi
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err == nil {
		return v
	}
	return 0
}

func parseConicColorStop(stop string) (ColorStop, bool) {
	fields := strings.Fields(stop)
	if len(fields) == 0 {
		return ColorStop{}, false
	}
	color, ok := ParseColor(fields[0])
	if !ok {
		return ColorStop{}, false
	}
	cs := ColorStop{Color: color, Offset: -1}
	if len(fields) >= 2 {
		pos := fields[1]
		if strings.HasSuffix(pos, "deg") {
			v, err := strconv.ParseFloat(strings.TrimSuffix(pos, "deg"), 64)
			if err == nil {
				cs.Offset = v / 360.0
			}
		} else if strings.HasSuffix(pos, "%") {
			v, err := strconv.ParseFloat(strings.TrimSuffix(pos, "%"), 64)
			if err == nil {
				cs.Offset = v / 100.0
			}
		} else if strings.HasSuffix(pos, "turn") {
			v, err := strconv.ParseFloat(strings.TrimSuffix(pos, "turn"), 64)
			if err == nil {
				cs.Offset = v
			}
		}
	}
	return cs, true
}

// ExtendRepeatingStops extends color stops for repeating gradients to fill 0-1 range
func (g *Gradient) ExtendRepeatingStops(gradientLength float64) {
	if len(g.ColorStops) < 2 || gradientLength <= 0 {
		return
	}

	// Get the first and last stop positions in pixels
	firstOffset := g.ColorStops[0].Offset
	lastOffset := g.ColorStops[len(g.ColorStops)-1].Offset

	// If offsets are already in 0-1 range (percentage-based), compute pattern length
	patternLength := lastOffset - firstOffset
	if patternLength <= 0 {
		return
	}

	// Build repeated stops.
	// Backward extension (shift < 0) is only needed when the gradient starts after 0
	// (i.e., firstOffset > 0). When firstOffset == 0, backward extension would add
	// the previous period's END color at position 0, conflicting with the current
	// period's START color. Only extend backward if it genuinely fills a gap before 0.
	startShift := 0.0
	if firstOffset > 1e-9 {
		startShift = -patternLength * 10
	}
	var newStops []ColorStop
	for shift := startShift; shift <= 1.0+patternLength; shift += patternLength {
		for _, stop := range g.ColorStops {
			newOffset := stop.Offset - firstOffset + shift
			if newOffset >= -0.01 && newOffset <= 1.01 {
				ns := ColorStop{Color: stop.Color, Offset: newOffset}
				if ns.Offset < 0 {
					ns.Offset = 0
				}
				if ns.Offset > 1 {
					ns.Offset = 1
				}
				newStops = append(newStops, ns)
			}
		}
	}

	if len(newStops) >= 2 {
		g.ColorStops = newStops
	}
}

// GetGradient attempts to parse a gradient from a background value
func GetGradient(backgroundValue string) (*Gradient, bool) {
	val := strings.TrimSpace(backgroundValue)
	if strings.HasPrefix(val, "repeating-linear-gradient(") {
		// Strip "repeating-" and parse as linear
		inner := "linear-gradient(" + val[len("repeating-linear-gradient("):]
		g, ok := ParseLinearGradient(inner)
		if ok {
			g.Repeating = true
			g.Type = GradientRepeatingLinear
		}
		return g, ok
	}
	if strings.HasPrefix(val, "repeating-radial-gradient(") {
		inner := "radial-gradient(" + val[len("repeating-radial-gradient("):]
		g, ok := ParseRadialGradient(inner)
		if ok {
			g.Repeating = true
			g.Type = GradientRepeatingRadial
		}
		return g, ok
	}
	if strings.Contains(val, "conic-gradient(") {
		return ParseConicGradient(val)
	}
	if strings.Contains(val, "linear-gradient(") {
		return ParseLinearGradient(val)
	}
	if strings.Contains(val, "radial-gradient(") {
		return ParseRadialGradient(val)
	}
	return nil, false
}
