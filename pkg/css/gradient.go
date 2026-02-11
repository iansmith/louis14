package css

import (
	"strconv"
	"strings"
)

// GradientType represents the type of CSS gradient
type GradientType int

const (
	GradientLinear GradientType = iota
	GradientRadial
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
	if g == nil || g.Type != GradientLinear {
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
}

// GetGradient attempts to parse a gradient from a background value
func GetGradient(backgroundValue string) (*Gradient, bool) {
	if strings.Contains(backgroundValue, "linear-gradient(") {
		return ParseLinearGradient(backgroundValue)
	}
	// Could add radial-gradient support here in the future
	return nil, false
}
