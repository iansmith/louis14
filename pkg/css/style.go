package css

import (
	"fmt"
	"strconv"
	"strings"
)

type Style struct {
	Properties map[string]string
}

func NewStyle() *Style {
	return &Style{Properties: make(map[string]string)}
}

func (s *Style) Get(property string) (string, bool) {
	val, ok := s.Properties[property]
	return val, ok
}

func (s *Style) Set(property, value string) {
	s.Properties[property] = value
}

func (s *Style) GetLength(property string) (float64, bool) {
	val, ok := s.Get(property)
	if !ok {
		return 0, false
	}
	return ParseLengthWithFontSize(val, s.GetFontSize())
}

// ParsePercentage parses a percentage value (e.g., "140%") and returns the number (e.g., 140).
func ParsePercentage(val string) (float64, bool) {
	val = strings.TrimSpace(val)
	if !strings.HasSuffix(val, "%") {
		return 0, false
	}
	numStr := strings.TrimSuffix(val, "%")
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

// GetPercentage returns the percentage value of a property (e.g., "140%" returns 140).
func (s *Style) GetPercentage(property string) (float64, bool) {
	val, ok := s.Get(property)
	if !ok {
		return 0, false
	}
	return ParsePercentage(val)
}

// ParseLength parses a length value (e.g., "100px" or "100")
// Does not handle em units — use ParseLengthWithFontSize for that.
func ParseLength(val string) (float64, bool) {
	return ParseLengthWithFontSize(val, 16.0)
}

// ParseLengthWithFontSize parses a length value with em support.
func ParseLengthWithFontSize(val string, fontSize float64) (float64, bool) {
	val = strings.TrimSpace(val)
	if strings.HasSuffix(val, "em") {
		numStr := strings.TrimSuffix(val, "em")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * fontSize, true
	}
	if strings.HasSuffix(val, "mm") {
		numStr := strings.TrimSuffix(val, "mm")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 3.7795275591, true // 1mm ≈ 3.78px at 96dpi
	}
	if strings.HasSuffix(val, "in") {
		numStr := strings.TrimSuffix(val, "in")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 96.0, true // 1in = 96px at 96dpi
	}
	if strings.HasSuffix(val, "cm") {
		numStr := strings.TrimSuffix(val, "cm")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 37.7952755906, true // 1cm = 96/2.54 px
	}
	if strings.HasSuffix(val, "pc") {
		numStr := strings.TrimSuffix(val, "pc")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * 16.0, true // 1pc = 16px
	}
	if strings.HasSuffix(val, "pt") {
		numStr := strings.TrimSuffix(val, "pt")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, false
		}
		return num * (96.0 / 72.0), true // 1pt = 96/72 px
	}
	if strings.HasSuffix(val, "px") {
		val = strings.TrimSuffix(val, "px")
	} else {
		// CSS 2.1: lengths require units (except 0)
		// Bare numbers without units are invalid
		num, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false
		}
		if num == 0 {
			return 0, true
		}
		return 0, false // non-zero without unit is invalid
	}
	num, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

// Phase 2: Box model helpers

// BoxEdge represents the four sides of a box (top, right, bottom, left)
type BoxEdge struct {
	Top       float64
	Right     float64
	Bottom    float64
	Left      float64
	AutoTop   bool // True if margin-top: auto
	AutoRight bool // True if margin-right: auto
	AutoBottom bool // True if margin-bottom: auto
	AutoLeft  bool // True if margin-left: auto
}

// GetMargin returns the margin values for all four sides
func (s *Style) GetMargin() BoxEdge {
	top, autoTop := s.getLengthOrAuto("margin-top")
	right, autoRight := s.getLengthOrAuto("margin-right")
	bottom, autoBottom := s.getLengthOrAuto("margin-bottom")
	left, autoLeft := s.getLengthOrAuto("margin-left")

	return BoxEdge{
		Top:        top,
		Right:      right,
		Bottom:     bottom,
		Left:       left,
		AutoTop:    autoTop,
		AutoRight:  autoRight,
		AutoBottom: autoBottom,
		AutoLeft:   autoLeft,
	}
}

// GetPadding returns the padding values for all four sides
func (s *Style) GetPadding() BoxEdge {
	return BoxEdge{
		Top:    s.getLengthOrZero("padding-top"),
		Right:  s.getLengthOrZero("padding-right"),
		Bottom: s.getLengthOrZero("padding-bottom"),
		Left:   s.getLengthOrZero("padding-left"),
	}
}

// GetBorderWidth returns the border width for all four sides
func (s *Style) GetBorderWidth() BoxEdge {
	styles := s.GetBorderStyle()
	edge := BoxEdge{
		Top:    s.getLengthOrZero("border-top-width"),
		Right:  s.getLengthOrZero("border-right-width"),
		Bottom: s.getLengthOrZero("border-bottom-width"),
		Left:   s.getLengthOrZero("border-left-width"),
	}
	// CSS 2.1 §8.5.1: border-style:none computes border-width to 0
	if styles.Top == BorderStyleNone {
		edge.Top = 0
	}
	if styles.Right == BorderStyleNone {
		edge.Right = 0
	}
	if styles.Bottom == BorderStyleNone {
		edge.Bottom = 0
	}
	if styles.Left == BorderStyleNone {
		edge.Left = 0
	}
	return edge
}

// getLengthOrZero returns the length value or 0 if not found
func (s *Style) getLengthOrZero(property string) float64 {
	val, ok := s.GetLength(property)
	if !ok {
		return 0
	}
	return val
}

// getLengthOrAuto returns the length value and whether it's "auto"
// Returns (value, isAuto) where value is 0 if auto
func (s *Style) getLengthOrAuto(property string) (float64, bool) {
	if val, ok := s.Get(property); ok {
		if val == "auto" {
			return 0, true
		}
	}
	return s.getLengthOrZero(property), false
}

// Phase 12: Border styling

// BorderStyle represents the border-style property value
type BorderStyle string

const (
	BorderStyleNone   BorderStyle = "none"
	BorderStyleSolid  BorderStyle = "solid"
	BorderStyleDashed BorderStyle = "dashed"
	BorderStyleDotted BorderStyle = "dotted"
	BorderStyleDouble BorderStyle = "double"
)

// BorderStyleEdge represents border styles for all four sides
type BorderStyleEdge struct {
	Top    BorderStyle
	Right  BorderStyle
	Bottom BorderStyle
	Left   BorderStyle
}

// GetBorderStyle returns the border style for all four sides
func (s *Style) GetBorderStyle() BorderStyleEdge {
	return BorderStyleEdge{
		Top:    s.getBorderStyleSide("border-top-style"),
		Right:  s.getBorderStyleSide("border-right-style"),
		Bottom: s.getBorderStyleSide("border-bottom-style"),
		Left:   s.getBorderStyleSide("border-left-style"),
	}
}

// getBorderStyleSide returns the border style for a specific side (default: solid)
func (s *Style) getBorderStyleSide(property string) BorderStyle {
	if style, ok := s.Get(property); ok {
		switch style {
		case "none":
			return BorderStyleNone
		case "dashed":
			return BorderStyleDashed
		case "dotted":
			return BorderStyleDotted
		case "double":
			return BorderStyleDouble
		}
	}
	return BorderStyleSolid // Default to solid
}

// GetBorderRadius returns the border-radius value (simplified - single value for all corners)
func (s *Style) GetBorderRadius() float64 {
	if radius, ok := s.GetLength("border-radius"); ok {
		return radius
	}
	return 0.0 // Default no radius
}

// GetMaxWidth returns the max-width value if set
func (s *Style) GetMaxWidth() (float64, bool) {
	return s.GetLength("max-width")
}

// Phase 4: Positioning helpers

// Position type constants
type PositionType string

const (
	PositionStatic   PositionType = "static"
	PositionRelative PositionType = "relative"
	PositionAbsolute PositionType = "absolute"
	PositionFixed    PositionType = "fixed"
)

// GetPosition returns the position type (default: static)
func (s *Style) GetPosition() PositionType {
	pos, ok := s.Get("position")
	if !ok {
		// DEBUG: Check if we're looking at an element that should have position set
		if tagName, hasTag := s.Get("_debug_tag"); hasTag && tagName == "div" {
			if id, hasID := s.Get("_debug_id"); hasID && (id == "div3" || id == "div2") {
				fmt.Printf("DEBUG CSS: #%s has no 'position' property set!\n", id)
			}
		}
		return PositionStatic
	}
	switch pos {
	case "relative":
		return PositionRelative
	case "absolute":
		return PositionAbsolute
	case "fixed":
		return PositionFixed
	default:
		return PositionStatic
	}
}

// GetPositionOffset returns the offset values for positioned elements
type PositionOffset struct {
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
	HasTop    bool
	HasRight  bool
	HasBottom bool
	HasLeft   bool
}

// GetPositionOffset returns positioning offset values
func (s *Style) GetPositionOffset() PositionOffset {
	offset := PositionOffset{}

	if top, ok := s.GetLength("top"); ok {
		offset.Top = top
		offset.HasTop = true
	}

	if right, ok := s.GetLength("right"); ok {
		offset.Right = right
		offset.HasRight = true
	}

	if bottom, ok := s.GetLength("bottom"); ok {
		offset.Bottom = bottom
		offset.HasBottom = true
	}

	if left, ok := s.GetLength("left"); ok {
		offset.Left = left
		offset.HasLeft = true
	}

	return offset
}

// GetZIndex returns the z-index value (default: 0)
func (s *Style) GetZIndex() int {
	if zindex, ok := s.Get("z-index"); ok {
		// Simple integer parsing
		var z int
		if _, err := fmt.Sscanf(zindex, "%d", &z); err == nil {
			return z
		}
	}
	return 0
}

func ParseInlineStyle(styleAttr string) *Style {
	style := NewStyle()
	declarations := strings.Split(styleAttr, ";")
	for _, decl := range declarations {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		property := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		// Phase 2: Expand shorthand properties
		expandShorthand(style, property, value)
	}
	return style
}

// expandShorthand expands shorthand CSS properties into individual properties
func expandShorthand(style *Style, property, value string) {
	switch property {
	case "margin":
		// margin: 10px -> margin-top/right/bottom/left: 10px
		expandBoxProperty(style, "margin", value)
	case "padding":
		// padding: 10px -> padding-top/right/bottom/left: 10px
		expandBoxProperty(style, "padding", value)
	case "border":
		// border: 1px solid black -> border-width/style/color
		expandBorderProperty(style, value)
	case "border-top", "border-right", "border-bottom", "border-left":
		expandBorderSideProperty(style, property, value)
	case "border-width":
		expandBorderBoxProperty(style, value, "width")
	case "border-style":
		expandBorderBoxProperty(style, value, "style")
	case "border-color":
		expandBorderBoxProperty(style, value, "color")
	case "background":
		expandBackgroundProperty(style, value)
	case "font":
		expandFontProperty(style, value)
	default:
		// Regular property
		style.Set(property, value)
	}
}

// expandBoxProperty expands margin/padding shorthand
// Supports: "10px" (all), "10px 20px" (vertical horizontal),
//           "10px 20px 30px" (top h bottom), "10px 20px 30px 40px" (t r b l)
func expandBoxProperty(style *Style, prefix, value string) {
	parts := strings.Fields(value)

	switch len(parts) {
	case 1:
		// All sides the same
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-right", parts[0])
		style.Set(prefix+"-bottom", parts[0])
		style.Set(prefix+"-left", parts[0])
	case 2:
		// Vertical, horizontal
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-bottom", parts[0])
		style.Set(prefix+"-right", parts[1])
		style.Set(prefix+"-left", parts[1])
	case 3:
		// Top, horizontal, bottom
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-right", parts[1])
		style.Set(prefix+"-left", parts[1])
		style.Set(prefix+"-bottom", parts[2])
	case 4:
		// Top, right, bottom, left
		style.Set(prefix+"-top", parts[0])
		style.Set(prefix+"-right", parts[1])
		style.Set(prefix+"-bottom", parts[2])
		style.Set(prefix+"-left", parts[3])
	}
}

// expandBorderBoxProperty expands border-width/style/color shorthand (1-4 values)
func expandBorderBoxProperty(style *Style, value string, suffix string) {
	parts := strings.Fields(value)
	var top, right, bottom, left string
	switch len(parts) {
	case 1:
		top, right, bottom, left = parts[0], parts[0], parts[0], parts[0]
	case 2:
		top, bottom = parts[0], parts[0]
		right, left = parts[1], parts[1]
	case 3:
		top, right, left, bottom = parts[0], parts[1], parts[1], parts[2]
	case 4:
		top, right, bottom, left = parts[0], parts[1], parts[2], parts[3]
	default:
		return
	}
	style.Set("border-top-"+suffix, top)
	style.Set("border-right-"+suffix, right)
	style.Set("border-bottom-"+suffix, bottom)
	style.Set("border-left-"+suffix, left)
}

// borderWidthKeyword resolves thin/medium/thick to pixel values.
func borderWidthKeyword(val string) (string, bool) {
	switch strings.ToLower(val) {
	case "thin":
		return "1px", true
	case "medium":
		return "3px", true
	case "thick":
		return "5px", true
	}
	return "", false
}

// expandBorderProperty expands border shorthand
// Format: "1px solid black" or "2px dotted #FF0000"
// Per CSS spec, shorthand properties reset ALL sub-properties to their initial values,
// then apply the specified values.
func expandBorderProperty(style *Style, value string) {
	// Reset all sub-properties to their initial values first
	// Initial values: width=medium (3px), style=none, color=currentcolor
	sides := []string{"top", "right", "bottom", "left"}
	for _, side := range sides {
		style.Set("border-"+side+"-width", "3px") // medium = 3px
		style.Set("border-"+side+"-style", "none")
		style.Set("border-"+side+"-color", "currentcolor")
	}

	// Now apply the specified values
	parts := strings.Fields(value)
	for _, part := range parts {
		if bw, ok := borderWidthKeyword(part); ok {
			style.Set("border-width", bw)
			style.Set("border-top-width", bw)
			style.Set("border-right-width", bw)
			style.Set("border-bottom-width", bw)
			style.Set("border-left-width", bw)
		} else if _, ok := ParseLength(part); ok {
			// Width (px, em, mm, or bare number)
			style.Set("border-width", part)
			style.Set("border-top-width", part)
			style.Set("border-right-width", part)
			style.Set("border-bottom-width", part)
			style.Set("border-left-width", part)
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" || part == "none" {
			// Style
			style.Set("border-style", part)
			style.Set("border-top-style", part)
			style.Set("border-right-style", part)
			style.Set("border-bottom-style", part)
			style.Set("border-left-style", part)
		} else {
			// Color
			style.Set("border-color", part)
			style.Set("border-top-color", part)
			style.Set("border-right-color", part)
			style.Set("border-bottom-color", part)
			style.Set("border-left-color", part)
		}
	}
}

// expandBorderSideProperty expands border-top/right/bottom/left shorthands.
// Per CSS spec, shorthand properties reset ALL sub-properties to their initial values,
// then apply the specified values.
func expandBorderSideProperty(style *Style, property, value string) {
	// property is "border-top", "border-right", etc.
	side := strings.TrimPrefix(property, "border-")

	// Reset all sub-properties to their initial values first
	// Initial values: width=medium (3px), style=none, color=currentcolor
	style.Set("border-"+side+"-width", "3px") // medium = 3px
	style.Set("border-"+side+"-style", "none")
	style.Set("border-"+side+"-color", "currentcolor")

	// Now apply the specified values
	parts := strings.Fields(value)
	for _, part := range parts {
		if part == "0" {
			style.Set("border-"+side+"-width", "0")
		} else if bw, ok := borderWidthKeyword(part); ok {
			style.Set("border-"+side+"-width", bw)
		} else if _, ok := ParseLength(part); ok {
			style.Set("border-"+side+"-width", part)
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" || part == "none" {
			style.Set("border-"+side+"-style", part)
		} else {
			style.Set("border-"+side+"-color", part)
		}
	}
}

// expandFontProperty expands the font shorthand.
// Format: [style] [variant] [weight] size[/line-height] family[, family...]
func expandFontProperty(style *Style, value string) {
	parts := strings.Fields(value)
	if len(parts) < 2 {
		return
	}

	i := 0
	// Skip optional font-style
	if i < len(parts) && (parts[i] == "italic" || parts[i] == "oblique" || parts[i] == "normal") {
		style.Set("font-style", parts[i])
		i++
	}
	// Skip optional font-variant
	if i < len(parts) && parts[i] == "small-caps" {
		style.Set("font-variant", parts[i])
		i++
	}
	// Skip optional font-weight
	if i < len(parts) {
		switch parts[i] {
		case "bold", "bolder", "lighter", "100", "200", "300", "400", "500", "600", "700", "800", "900":
			style.Set("font-weight", parts[i])
			i++
		}
	}
	// Next should be size[/line-height]
	if i < len(parts) {
		sizeStr := parts[i]
		if idx := strings.Index(sizeStr, "/"); idx >= 0 {
			style.Set("font-size", sizeStr[:idx])
			style.Set("line-height", sizeStr[idx+1:])
		} else {
			style.Set("font-size", sizeStr)
		}
		i++
	}
	// Remaining is font-family
	if i < len(parts) {
		family := strings.Join(parts[i:], " ")
		style.Set("font-family", family)
	}
}

// expandBackgroundProperty expands the background shorthand.
// It extracts url(...), color, no-repeat, and position components.
func expandBackgroundProperty(style *Style, value string) {
	// Handle "none" - resets background
	trimmed := strings.TrimSpace(value)
	if trimmed == "none" {
		style.Set("background-color", "transparent")
		style.Set("background-image", "none")
		return
	}

	// Extract url(...) first since it may contain spaces (e.g. data URIs)
	urlStart := strings.Index(value, "url(")
	if urlStart >= 0 {
		// Find matching closing paren, accounting for nested parens
		depth := 0
		urlEnd := -1
		for i := urlStart + 4; i < len(value); i++ {
			if value[i] == '(' {
				depth++
			} else if value[i] == ')' {
				if depth == 0 {
					urlEnd = i + 1
					break
				}
				depth--
			}
		}
		if urlEnd > urlStart {
			urlPart := value[urlStart:urlEnd]
			style.Set("background-image", urlPart)
			// Remove url(...) from value to parse remaining parts
			value = value[:urlStart] + value[urlEnd:]
		}
	}

	// Parse remaining tokens for color, repeat, position
	parts := strings.Fields(value)
	positionParts := []string{}
	colorFound := false
	colorValue := ""
	for _, part := range parts {
		if part == "no-repeat" || part == "repeat" || part == "repeat-x" || part == "repeat-y" {
			style.Set("background-repeat", part)
		} else if _, ok := ParseColor(part); ok {
			if colorFound {
				// Two color values = invalid declaration, skip entirely
				return
			}
			colorFound = true
			colorValue = part
		} else if part == "transparent" {
			if colorFound {
				return
			}
			colorFound = true
			colorValue = "transparent"
		} else if _, ok := ParseLength(part); ok {
			positionParts = append(positionParts, part)
		} else if part == "center" || part == "left" || part == "right" || part == "top" || part == "bottom" {
			positionParts = append(positionParts, part)
		} else if part == "fixed" || part == "scroll" || part == "local" {
			style.Set("background-attachment", part)
		}
	}
	if colorFound {
		style.Set("background-color", colorValue)
	}
	if len(positionParts) > 0 {
		style.Set("background-position", strings.Join(positionParts, " "))
	}
}

// Phase 19: Enhanced color with alpha channel
type Color struct {
	R, G, B uint8
	A       float64 // Alpha: 0.0 (transparent) to 1.0 (opaque), default 1.0
}

func ParseColor(colorStr string) (Color, bool) {
	colorStr = strings.TrimSpace(colorStr)

	// Reject quoted values — CSS color values are never strings
	if strings.HasPrefix(colorStr, "'") || strings.HasPrefix(colorStr, "\"") {
		return Color{}, false
	}

	colorStr = strings.ToLower(colorStr)

	// Handle transparent
	if colorStr == "transparent" {
		return Color{0, 0, 0, 0.0}, true
	}

	// Phase 19: Handle rgba() format
	if strings.HasPrefix(colorStr, "rgba(") && strings.HasSuffix(colorStr, ")") {
		values := strings.TrimSuffix(strings.TrimPrefix(colorStr, "rgba("), ")")
		parts := strings.Split(values, ",")
		if len(parts) == 4 {
			var r, g, b int
			var a float64
			fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &r)
			fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &g)
			fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &b)
			fmt.Sscanf(strings.TrimSpace(parts[3]), "%f", &a)
			if r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255 {
				return Color{uint8(r), uint8(g), uint8(b), a}, true
			}
		}
	}

	// Try hex color first (#RGB or #RRGGBB)
	if strings.HasPrefix(colorStr, "#") {
		hex := colorStr[1:]
		var r, g, b uint8

		if len(hex) == 3 {
			// #RGB format - expand to #RRGGBB
			n, _ := fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b)
			if n != 3 {
				return Color{}, false
			}
			r = r*16 + r
			g = g*16 + g
			b = b*16 + b
			return Color{r, g, b, 1.0}, true
		} else if len(hex) == 6 {
			// #RRGGBB format
			n, _ := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
			if n != 3 {
				return Color{}, false
			}
			return Color{r, g, b, 1.0}, true
		}
	}

	// Try named colors
	namedColors := map[string]Color{
		"red":     {255, 0, 0, 1.0},
		"green":   {0, 128, 0, 1.0},
		"blue":    {0, 0, 255, 1.0},
		"yellow":  {255, 255, 0, 1.0},
		"cyan":    {0, 255, 255, 1.0},
		"magenta": {255, 0, 255, 1.0},
		"white":   {255, 255, 255, 1.0},
		"black":   {0, 0, 0, 1.0},
		"gray":    {128, 128, 128, 1.0},
		"orange":  {255, 165, 0, 1.0},
		"purple":  {128, 0, 128, 1.0},
		"pink":    {255, 192, 203, 1.0},
		"brown":   {165, 42, 42, 1.0},
		"lime":    {0, 255, 0, 1.0},
		"navy":    {0, 0, 128, 1.0},
		"teal":    {0, 128, 128, 1.0},
		"silver":  {192, 192, 192, 1.0},
	}
	color, ok := namedColors[colorStr]
	return color, ok
}

// Phase 6: Text rendering helpers

// GetFontSize returns the font-size in pixels (default: 16px)
func (s *Style) GetFontSize() float64 {
	val, ok := s.Get("font-size")
	if !ok {
		return 16.0
	}
	// For font-size, em is relative to parent's font-size (use 16px as default parent)
	if size, ok := ParseLengthWithFontSize(val, 16.0); ok {
		return size
	}
	return 16.0
}

// GetColor returns the text color (default: black)
func (s *Style) GetColor() Color {
	if colorStr, ok := s.Get("color"); ok {
		if color, ok := ParseColor(colorStr); ok {
			return color
		}
	}
	return Color{0, 0, 0, 1.0} // Default to black
}

// Phase 5: Float layout helpers

// FloatType represents the float property value
type FloatType string

const (
	FloatNone  FloatType = "none"
	FloatLeft  FloatType = "left"
	FloatRight FloatType = "right"
)

// GetFloat returns the float value (default: none)
func (s *Style) GetFloat() FloatType {
	if floatVal, ok := s.Get("float"); ok {
		switch floatVal {
		case "left":
			return FloatLeft
		case "right":
			return FloatRight
		}
	}
	return FloatNone
}

// ClearType represents the clear property value
type ClearType string

const (
	ClearNone  ClearType = "none"
	ClearLeft  ClearType = "left"
	ClearRight ClearType = "right"
	ClearBoth  ClearType = "both"
)

// GetClear returns the clear value (default: none)
func (s *Style) GetClear() ClearType {
	if clearVal, ok := s.Get("clear"); ok {
		switch clearVal {
		case "left":
			return ClearLeft
		case "right":
			return ClearRight
		case "both":
			return ClearBoth
		}
	}
	return ClearNone
}

// Phase 6 Enhancements: Text styling

// TextAlign represents the text-align property value
type TextAlign string

const (
	TextAlignLeft   TextAlign = "left"
	TextAlignCenter TextAlign = "center"
	TextAlignRight  TextAlign = "right"
)

// GetTextAlign returns the text-align value (default: left)
func (s *Style) GetTextAlign() TextAlign {
	if align, ok := s.Get("text-align"); ok {
		switch align {
		case "center":
			return TextAlignCenter
		case "right":
			return TextAlignRight
		}
	}
	return TextAlignLeft
}

// FontWeight represents the font-weight property value
type FontWeight string

const (
	FontWeightNormal FontWeight = "normal"
	FontWeightBold   FontWeight = "bold"
)

// GetFontWeight returns the font-weight value (default: normal)
func (s *Style) GetFontWeight() FontWeight {
	if weight, ok := s.Get("font-weight"); ok {
		switch weight {
		case "bold", "700", "800", "900":
			return FontWeightBold
		}
	}
	return FontWeightNormal
}

// FontStyle represents the font-style property value
type FontStyle string

const (
	FontStyleNormal FontStyle = "normal"
	FontStyleItalic FontStyle = "italic"
)

// GetFontStyle returns the font-style value (default: normal)
func (s *Style) GetFontStyle() FontStyle {
	if style, ok := s.Get("font-style"); ok {
		switch style {
		case "italic", "oblique":
			return FontStyleItalic
		}
	}
	return FontStyleNormal
}

// IsMonospaceFamily returns true if the computed font-family is a monospace font.
func (s *Style) IsMonospaceFamily() bool {
	if family, ok := s.Get("font-family"); ok {
		lower := strings.ToLower(family)
		for _, mono := range []string{"monospace", "mono", "courier", "consolas", "menlo", "monaco"} {
			if strings.Contains(lower, mono) {
				return true
			}
		}
	}
	return false
}

// Phase 17: Text decoration

// TextDecoration represents the text-decoration property value
type TextDecoration string

const (
	TextDecorationNone        TextDecoration = "none"
	TextDecorationUnderline   TextDecoration = "underline"
	TextDecorationOverline    TextDecoration = "overline"
	TextDecorationLineThrough TextDecoration = "line-through"
)

// GetTextDecoration returns the text-decoration value (default: none)
func (s *Style) GetTextDecoration() TextDecoration {
	if decoration, ok := s.Get("text-decoration"); ok {
		switch decoration {
		case "underline":
			return TextDecorationUnderline
		case "overline":
			return TextDecorationOverline
		case "line-through":
			return TextDecorationLineThrough
		case "none":
			return TextDecorationNone
		}
	}
	return TextDecorationNone
}

// Phase 20: Additional text properties

// GetLetterSpacing returns the letter-spacing value in pixels (default: 0)
func (s *Style) GetLetterSpacing() float64 {
	if spacing, ok := s.GetLength("letter-spacing"); ok {
		return spacing
	}
	return 0.0
}

// GetWordSpacing returns the word-spacing value in pixels (default: 0)
func (s *Style) GetWordSpacing() float64 {
	if spacing, ok := s.GetLength("word-spacing"); ok {
		return spacing
	}
	return 0.0
}

// TextTransform represents the text-transform property value
type TextTransform string

const (
	TextTransformNone       TextTransform = "none"
	TextTransformUppercase  TextTransform = "uppercase"
	TextTransformLowercase  TextTransform = "lowercase"
	TextTransformCapitalize TextTransform = "capitalize"
)

// GetTextTransform returns the text-transform value (default: none)
func (s *Style) GetTextTransform() TextTransform {
	if transform, ok := s.Get("text-transform"); ok {
		switch transform {
		case "uppercase":
			return TextTransformUppercase
		case "lowercase":
			return TextTransformLowercase
		case "capitalize":
			return TextTransformCapitalize
		case "none":
			return TextTransformNone
		}
	}
	return TextTransformNone
}

// WhiteSpace represents the white-space property value
type WhiteSpace string

const (
	WhiteSpaceNormal  WhiteSpace = "normal"
	WhiteSpaceNowrap  WhiteSpace = "nowrap"
	WhiteSpacePre     WhiteSpace = "pre"
	WhiteSpacePreWrap WhiteSpace = "pre-wrap"
	WhiteSpacePreLine WhiteSpace = "pre-line"
)

// GetWhiteSpace returns the white-space value (default: normal)
func (s *Style) GetWhiteSpace() WhiteSpace {
	if ws, ok := s.Get("white-space"); ok {
		switch ws {
		case "nowrap":
			return WhiteSpaceNowrap
		case "pre":
			return WhiteSpacePre
		case "pre-wrap":
			return WhiteSpacePreWrap
		case "pre-line":
			return WhiteSpacePreLine
		case "normal":
			return WhiteSpaceNormal
		}
	}
	return WhiteSpaceNormal
}

// Phase 21: Overflow properties

// OverflowType represents the overflow property value
type OverflowType string

const (
	OverflowVisible OverflowType = "visible"
	OverflowHidden  OverflowType = "hidden"
	OverflowScroll  OverflowType = "scroll"
	OverflowAuto    OverflowType = "auto"
)

// GetOverflow returns the overflow value (default: visible)
func (s *Style) GetOverflow() OverflowType {
	if overflow, ok := s.Get("overflow"); ok {
		switch overflow {
		case "hidden":
			return OverflowHidden
		case "scroll":
			return OverflowScroll
		case "auto":
			return OverflowAuto
		case "visible":
			return OverflowVisible
		}
	}
	return OverflowVisible
}

// GetOverflowX returns the overflow-x value (default: overflow value)
func (s *Style) GetOverflowX() OverflowType {
	if overflowX, ok := s.Get("overflow-x"); ok {
		switch overflowX {
		case "hidden":
			return OverflowHidden
		case "scroll":
			return OverflowScroll
		case "auto":
			return OverflowAuto
		case "visible":
			return OverflowVisible
		}
	}
	return s.GetOverflow()
}

// GetOverflowY returns the overflow-y value (default: overflow value)
func (s *Style) GetOverflowY() OverflowType {
	if overflowY, ok := s.Get("overflow-y"); ok {
		switch overflowY {
		case "hidden":
			return OverflowHidden
		case "scroll":
			return OverflowScroll
		case "auto":
			return OverflowAuto
		case "visible":
			return OverflowVisible
		}
	}
	return s.GetOverflow()
}

// Phase 19: Visual effects

// GetOpacity returns the opacity value (0.0 to 1.0, default: 1.0)
func (s *Style) GetOpacity() float64 {
	if opacityStr, ok := s.Get("opacity"); ok {
		var opacity float64
		if _, err := fmt.Sscanf(opacityStr, "%f", &opacity); err == nil {
			// Clamp to 0.0 - 1.0
			if opacity < 0.0 {
				opacity = 0.0
			} else if opacity > 1.0 {
				opacity = 1.0
			}
			return opacity
		}
	}
	return 1.0 // Fully opaque by default
}

// BoxShadow represents a box-shadow effect
type BoxShadow struct {
	OffsetX float64
	OffsetY float64
	Blur    float64
	Spread  float64
	Color   Color
	Inset   bool
}

// GetBoxShadow parses and returns box-shadow values
func (s *Style) GetBoxShadow() []BoxShadow {
	shadowStr, ok := s.Get("box-shadow")
	if !ok || shadowStr == "none" {
		return nil
	}

	// Parse box-shadow: offsetX offsetY blur spread color
	// Example: "2px 2px 5px 0px rgba(0,0,0,0.3)"
	shadows := make([]BoxShadow, 0)

	// Split by comma for multiple shadows
	parts := strings.Split(shadowStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		shadow := parseBoxShadowValue(part)
		if shadow != nil {
			shadows = append(shadows, *shadow)
		}
	}

	return shadows
}

// parseBoxShadowValue parses a single box-shadow value
func parseBoxShadowValue(s string) *BoxShadow {
	s = strings.TrimSpace(s)
	tokens := strings.Fields(s)

	if len(tokens) < 2 {
		return nil
	}

	shadow := &BoxShadow{
		Color: Color{0, 0, 0, 0.3}, // Default shadow color
	}

	tokenIndex := 0

	// Check for 'inset'
	if tokens[tokenIndex] == "inset" {
		shadow.Inset = true
		tokenIndex++
	}

	// Parse offset-x
	if tokenIndex < len(tokens) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.OffsetX = val
			tokenIndex++
		}
	}

	// Parse offset-y
	if tokenIndex < len(tokens) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.OffsetY = val
			tokenIndex++
		}
	}

	// Parse blur radius (optional)
	if tokenIndex < len(tokens) && !isColor(tokens[tokenIndex]) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.Blur = val
			tokenIndex++
		}
	}

	// Parse spread radius (optional)
	if tokenIndex < len(tokens) && !isColor(tokens[tokenIndex]) {
		if val, ok := ParseLength(tokens[tokenIndex]); ok {
			shadow.Spread = val
			tokenIndex++
		}
	}

	// Parse color (rest of the string)
	if tokenIndex < len(tokens) {
		colorStr := strings.Join(tokens[tokenIndex:], " ")
		if color, ok := ParseColor(colorStr); ok {
			shadow.Color = color
		}
	}

	return shadow
}

// isColor checks if a token might be a color value
func isColor(s string) bool {
	return strings.HasPrefix(s, "#") ||
		   strings.HasPrefix(s, "rgb") ||
		   strings.HasPrefix(s, "hsl") ||
		   (s != "inset" && !strings.HasSuffix(s, "px") && !strings.HasSuffix(s, "em"))
}

// Phase 7: Display modes

// DisplayType represents the display property value
type DisplayType string

const (
	DisplayBlock           DisplayType = "block"
	DisplayInline          DisplayType = "inline"
	DisplayInlineBlock     DisplayType = "inline-block"
	DisplayNone            DisplayType = "none"
	DisplayTable           DisplayType = "table"
	DisplayTableRow        DisplayType = "table-row"
	DisplayTableCell       DisplayType = "table-cell"
	DisplayTableHeaderGroup DisplayType = "table-header-group"
	DisplayTableRowGroup   DisplayType = "table-row-group"
	DisplayTableFooterGroup DisplayType = "table-footer-group"
	DisplayListItem        DisplayType = "list-item" // Phase 23
	DisplayFlex            DisplayType = "flex"
	DisplayInlineFlex      DisplayType = "inline-flex"
	DisplayGrid            DisplayType = "grid"
	DisplayInlineGrid      DisplayType = "inline-grid"
)

// GetDisplay returns the display value (default: block)
func (s *Style) GetDisplay() DisplayType {
	if display, ok := s.Get("display"); ok {
		switch display {
		case "inline":
			return DisplayInline
		case "inline-block":
			return DisplayInlineBlock
		case "none":
			return DisplayNone
		case "table":
			return DisplayTable
		case "table-row":
			return DisplayTableRow
		case "table-cell":
			return DisplayTableCell
		case "table-header-group":
			return DisplayTableHeaderGroup
		case "table-row-group":
			return DisplayTableRowGroup
		case "table-footer-group":
			return DisplayTableFooterGroup
		case "list-item":
			return DisplayListItem
		case "flex":
			return DisplayFlex
		case "inline-flex":
			return DisplayInlineFlex
		case "grid":
			return DisplayGrid
		case "inline-grid":
			return DisplayInlineGrid
		}
	}
	return DisplayBlock
}

// VerticalAlign represents the vertical-align property value
type VerticalAlign string

const (
	VerticalAlignBaseline VerticalAlign = "baseline"
	VerticalAlignTop      VerticalAlign = "top"
	VerticalAlignMiddle   VerticalAlign = "middle"
	VerticalAlignBottom   VerticalAlign = "bottom"
)

// GetVerticalAlign returns the vertical-align value (default: baseline)
func (s *Style) GetVerticalAlign() VerticalAlign {
	if align, ok := s.Get("vertical-align"); ok {
		switch align {
		case "top":
			return VerticalAlignTop
		case "middle":
			return VerticalAlignMiddle
		case "bottom":
			return VerticalAlignBottom
		}
	}
	return VerticalAlignBaseline
}

// GetLineHeight returns the line-height in pixels (default: 1.2 * font-size).
// CSS line-height accepts unitless numbers (e.g., "1.5") meaning a multiplier
// of the current font-size, unlike other CSS length properties where bare
// numbers are invalid.
func (s *Style) GetLineHeight() float64 {
	val, ok := s.Get("line-height")
	if !ok {
		return s.GetFontSize() * 1.2
	}
	// Try as a standard CSS length first (px, em, etc.)
	if lh, ok := ParseLengthWithFontSize(val, s.GetFontSize()); ok {
		return lh
	}
	// Try as a unitless multiplier (e.g., "1.5" means 1.5 × font-size)
	val = strings.TrimSpace(val)
	if num, err := strconv.ParseFloat(val, 64); err == nil && num > 0 {
		return num * s.GetFontSize()
	}
	// Try as a percentage (e.g., "150%" means 1.5 × font-size)
	if pct, ok := ParsePercentage(val); ok {
		return pct / 100.0 * s.GetFontSize()
	}
	return s.GetFontSize() * 1.2
}

// Phase 9: Table layout

// BorderCollapse represents the border-collapse property value
type BorderCollapse string

const (
	BorderCollapseSeparate BorderCollapse = "separate"
	BorderCollapseCollapse BorderCollapse = "collapse"
)

// GetBorderCollapse returns the border-collapse value (default: separate)
func (s *Style) GetBorderCollapse() BorderCollapse {
	if bc, ok := s.Get("border-collapse"); ok {
		switch bc {
		case "collapse":
			return BorderCollapseCollapse
		}
	}
	return BorderCollapseSeparate
}

// GetBorderSpacing returns the border-spacing value (default: 0 per CSS 2.1)
// If two values are given (horizontal vertical), returns the first value.
func (s *Style) GetBorderSpacing() float64 {
	if val, ok := s.Get("border-spacing"); ok {
		// Handle two-value syntax: "96px 96px"
		parts := strings.Fields(val)
		if len(parts) >= 1 {
			if spacing, ok := ParseLength(parts[0]); ok {
				return spacing
			}
		}
	}
	return 0 // CSS 2.1 initial value
}

// Phase 10: Flexbox layout

// FlexDirection represents the flex-direction property value
type FlexDirection string

const (
	FlexDirectionRow           FlexDirection = "row"
	FlexDirectionRowReverse    FlexDirection = "row-reverse"
	FlexDirectionColumn        FlexDirection = "column"
	FlexDirectionColumnReverse FlexDirection = "column-reverse"
)

// GetFlexDirection returns the flex-direction value (default: row)
func (s *Style) GetFlexDirection() FlexDirection {
	if dir, ok := s.Get("flex-direction"); ok {
		switch dir {
		case "row-reverse":
			return FlexDirectionRowReverse
		case "column":
			return FlexDirectionColumn
		case "column-reverse":
			return FlexDirectionColumnReverse
		}
	}
	return FlexDirectionRow
}

// FlexWrap represents the flex-wrap property value
type FlexWrap string

const (
	FlexWrapNowrap      FlexWrap = "nowrap"
	FlexWrapWrap        FlexWrap = "wrap"
	FlexWrapWrapReverse FlexWrap = "wrap-reverse"
)

// GetFlexWrap returns the flex-wrap value (default: nowrap)
func (s *Style) GetFlexWrap() FlexWrap {
	if wrap, ok := s.Get("flex-wrap"); ok {
		switch wrap {
		case "wrap":
			return FlexWrapWrap
		case "wrap-reverse":
			return FlexWrapWrapReverse
		}
	}
	return FlexWrapNowrap
}

// JustifyContent represents the justify-content property value
type JustifyContent string

const (
	JustifyContentFlexStart    JustifyContent = "flex-start"
	JustifyContentFlexEnd      JustifyContent = "flex-end"
	JustifyContentCenter       JustifyContent = "center"
	JustifyContentSpaceBetween JustifyContent = "space-between"
	JustifyContentSpaceAround  JustifyContent = "space-around"
	JustifyContentSpaceEvenly  JustifyContent = "space-evenly"
)

// GetJustifyContent returns the justify-content value (default: flex-start)
func (s *Style) GetJustifyContent() JustifyContent {
	if jc, ok := s.Get("justify-content"); ok {
		switch jc {
		case "flex-end":
			return JustifyContentFlexEnd
		case "center":
			return JustifyContentCenter
		case "space-between":
			return JustifyContentSpaceBetween
		case "space-around":
			return JustifyContentSpaceAround
		case "space-evenly":
			return JustifyContentSpaceEvenly
		}
	}
	return JustifyContentFlexStart
}

// AlignItems represents the align-items property value
type AlignItems string

const (
	AlignItemsFlexStart AlignItems = "flex-start"
	AlignItemsFlexEnd   AlignItems = "flex-end"
	AlignItemsCenter    AlignItems = "center"
	AlignItemsStretch   AlignItems = "stretch"
	AlignItemsBaseline  AlignItems = "baseline"
)

// GetAlignItems returns the align-items value (default: stretch)
func (s *Style) GetAlignItems() AlignItems {
	if ai, ok := s.Get("align-items"); ok {
		switch ai {
		case "flex-start":
			return AlignItemsFlexStart
		case "flex-end":
			return AlignItemsFlexEnd
		case "center":
			return AlignItemsCenter
		case "baseline":
			return AlignItemsBaseline
		}
	}
	return AlignItemsStretch
}

// AlignContent represents the align-content property value
type AlignContent string

const (
	AlignContentFlexStart    AlignContent = "flex-start"
	AlignContentFlexEnd      AlignContent = "flex-end"
	AlignContentCenter       AlignContent = "center"
	AlignContentStretch      AlignContent = "stretch"
	AlignContentSpaceBetween AlignContent = "space-between"
	AlignContentSpaceAround  AlignContent = "space-around"
)

// GetAlignContent returns the align-content value (default: stretch)
func (s *Style) GetAlignContent() AlignContent {
	if ac, ok := s.Get("align-content"); ok {
		switch ac {
		case "flex-start":
			return AlignContentFlexStart
		case "flex-end":
			return AlignContentFlexEnd
		case "center":
			return AlignContentCenter
		case "space-between":
			return AlignContentSpaceBetween
		case "space-around":
			return AlignContentSpaceAround
		}
	}
	return AlignContentStretch
}

// GetFlexGrow returns the flex-grow value (default: 0)
func (s *Style) GetFlexGrow() float64 {
	if grow, ok := s.GetLength("flex-grow"); ok {
		return grow
	}
	return 0.0
}

// GetFlexShrink returns the flex-shrink value (default: 1)
func (s *Style) GetFlexShrink() float64 {
	if shrink, ok := s.GetLength("flex-shrink"); ok {
		return shrink
	}
	return 1.0
}

// GetFlexBasis returns the flex-basis value (default: auto, returns -1 for auto)
func (s *Style) GetFlexBasis() float64 {
	if basis, ok := s.Get("flex-basis"); ok {
		if basis == "auto" {
			return -1 // Special value for auto
		}
		if length, ok := ParseLength(basis); ok {
			return length
		}
	}
	return -1 // Default to auto
}

// AlignSelf represents the align-self property value
type AlignSelf string

const (
	AlignSelfAuto      AlignSelf = "auto"
	AlignSelfFlexStart AlignSelf = "flex-start"
	AlignSelfFlexEnd   AlignSelf = "flex-end"
	AlignSelfCenter    AlignSelf = "center"
	AlignSelfStretch   AlignSelf = "stretch"
	AlignSelfBaseline  AlignSelf = "baseline"
)

// GetAlignSelf returns the align-self value (default: auto)
func (s *Style) GetAlignSelf() AlignSelf {
	if as, ok := s.Get("align-self"); ok {
		switch as {
		case "flex-start":
			return AlignSelfFlexStart
		case "flex-end":
			return AlignSelfFlexEnd
		case "center":
			return AlignSelfCenter
		case "stretch":
			return AlignSelfStretch
		case "baseline":
			return AlignSelfBaseline
		}
	}
	return AlignSelfAuto
}

// GetOrder returns the order value (default: 0)
func (s *Style) GetOrder() int {
	if order, ok := s.Get("order"); ok {
		var o int
		if _, err := fmt.Sscanf(order, "%d", &o); err == nil {
			return o
		}
	}
	return 0
}

// Phase 11: Pseudo-elements

// ContentValue represents a single value in the content property
type ContentValue struct {
	Type  string // "text", "url", "counter", "attr", "open-quote", "close-quote"
	Value string // The actual value (text content, URL path, counter name, attr name)
}

// GetContent returns the content property value for pseudo-elements
// Returns the content string and true if content is set, or "", false if not
func (s *Style) GetContent() (string, bool) {
	if content, ok := s.Get("content"); ok {
		// Handle "none" and "normal" (no content)
		if content == "none" || content == "normal" {
			return "", false
		}

		// Remove quotes from string content
		content = strings.TrimSpace(content)
		if len(content) >= 2 {
			// Remove single or double quotes
			if (content[0] == '"' && content[len(content)-1] == '"') ||
			   (content[0] == '\'' && content[len(content)-1] == '\'') {
				content = content[1 : len(content)-1]
			}
		}

		return content, true
	}
	return "", false
}

// GetContentValues returns the parsed content property as a list of values
// This handles complex content like: counter(ctr) url(img.png) "text" attr(class)
func (s *Style) GetContentValues() ([]ContentValue, bool) {
	raw, ok := s.Get("content")
	if !ok {
		return nil, false
	}

	// Handle "none" and "normal" (no content)
	raw = strings.TrimSpace(raw)
	if raw == "none" || raw == "normal" {
		return nil, false
	}

	return ParseContentValues(raw), true
}

// ParseContentValues parses a CSS content value into individual parts
func ParseContentValues(raw string) []ContentValue {
	var values []ContentValue
	raw = strings.TrimSpace(raw)

	for len(raw) > 0 {
		raw = strings.TrimSpace(raw)
		if len(raw) == 0 {
			break
		}

		// Check for quoted string
		if raw[0] == '"' || raw[0] == '\'' {
			quote := raw[0]
			end := 1
			for end < len(raw) && raw[end] != quote {
				if raw[end] == '\\' && end+1 < len(raw) {
					end += 2 // Skip escaped character
				} else {
					end++
				}
			}
			if end < len(raw) {
				text := raw[1:end]
				// Unescape common sequences
				text = strings.ReplaceAll(text, "\\0022", "\"")
				text = strings.ReplaceAll(text, "\\\"", "\"")
				values = append(values, ContentValue{Type: "text", Value: text})
				raw = raw[end+1:]
			} else {
				// Unclosed quote - take rest as text
				values = append(values, ContentValue{Type: "text", Value: raw[1:]})
				break
			}
			continue
		}

		// Check for function-style values: counter(), url(), attr()
		// First check if the raw string starts with a known function name followed by (
		funcIdx := -1
		funcName := ""
		for _, fn := range []string{"counter", "url", "attr", "counters"} {
			if strings.HasPrefix(strings.ToLower(raw), fn+"(") {
				funcIdx = len(fn)
				funcName = fn
				break
			}
		}
		if funcIdx > 0 {
			idx := funcIdx
			// Find matching closing paren
			depth := 1
			start := idx + 1
			end := start
			for end < len(raw) && depth > 0 {
				if raw[end] == '(' {
					depth++
				} else if raw[end] == ')' {
					depth--
				}
				end++
			}
			if depth == 0 {
				arg := strings.TrimSpace(raw[start : end-1])
				switch funcName {
				case "url":
					// Strip quotes if present
					arg = strings.Trim(arg, "\"'")
					values = append(values, ContentValue{Type: "url", Value: arg})
				case "counter":
					// counter(name) or counter(name, style)
					values = append(values, ContentValue{Type: "counter", Value: arg})
				case "attr":
					values = append(values, ContentValue{Type: "attr", Value: arg})
				}
				raw = raw[end:]
				continue
			}
		}

		// Check for keywords
		lowerRaw := strings.ToLower(raw)
		if strings.HasPrefix(lowerRaw, "open-quote") {
			values = append(values, ContentValue{Type: "open-quote", Value: ""})
			raw = raw[10:]
			continue
		}
		if strings.HasPrefix(lowerRaw, "close-quote") {
			values = append(values, ContentValue{Type: "close-quote", Value: ""})
			raw = raw[11:]
			continue
		}
		if strings.HasPrefix(lowerRaw, "no-open-quote") {
			raw = raw[13:]
			continue
		}
		if strings.HasPrefix(lowerRaw, "no-close-quote") {
			raw = raw[14:]
			continue
		}

		// Unknown content - skip to next space or take rest
		if idx := strings.IndexAny(raw, " \t"); idx > 0 {
			raw = raw[idx:]
		} else {
			break
		}
	}

	return values
}

// Phase 15: CSS Grid properties

// GridTrack represents a single grid track (column or row)
type GridTrack struct {
	Size float64  // Size in pixels
}

// GetGridTemplateColumns parses grid-template-columns and returns track sizes
func (s *Style) GetGridTemplateColumns() []GridTrack {
	if val, ok := s.Get("grid-template-columns"); ok {
		return parseGridTracks(val)
	}
	return nil
}

// GetGridTemplateRows parses grid-template-rows and returns track sizes
func (s *Style) GetGridTemplateRows() []GridTrack {
	if val, ok := s.Get("grid-template-rows"); ok {
		return parseGridTracks(val)
	}
	return nil
}

// parseGridTracks parses a space-separated list of track sizes (e.g., "100px 200px 150px")
func parseGridTracks(val string) []GridTrack {
	tracks := make([]GridTrack, 0)
	parts := strings.Fields(val)
	
	for _, part := range parts {
		if size, ok := ParseLength(part); ok {
			tracks = append(tracks, GridTrack{Size: size})
		}
	}
	
	return tracks
}

// GetGridGap returns the grid-gap value (shorthand for row-gap and column-gap)
func (s *Style) GetGridGap() (rowGap, columnGap float64) {
	// Try grid-gap first (older syntax)
	if gap, ok := s.GetLength("grid-gap"); ok {
		return gap, gap
	}
	
	// Try gap (newer syntax)
	if gap, ok := s.GetLength("gap"); ok {
		return gap, gap
	}
	
	// Try individual properties
	rowGap, _ = s.GetLength("row-gap")
	columnGap, _ = s.GetLength("column-gap")
	
	return rowGap, columnGap
}

// GridPlacement represents grid-column or grid-row placement
type GridPlacement struct {
	Start int  // Starting line (1-indexed)
	End   int  // Ending line (1-indexed, exclusive)
}

// GetGridColumn parses grid-column property (e.g., "1 / 3" or "1 / span 2")
func (s *Style) GetGridColumn() *GridPlacement {
	if val, ok := s.Get("grid-column"); ok {
		return parseGridPlacement(val)
	}
	return nil
}

// GetGridRow parses grid-row property (e.g., "2 / 4")
func (s *Style) GetGridRow() *GridPlacement {
	if val, ok := s.Get("grid-row"); ok {
		return parseGridPlacement(val)
	}
	return nil
}

// parseGridPlacement parses grid line placement (e.g., "1 / 3")
func parseGridPlacement(val string) *GridPlacement {
	parts := strings.Split(val, "/")
	if len(parts) != 2 {
		return nil
	}
	
	start := strings.TrimSpace(parts[0])
	end := strings.TrimSpace(parts[1])
	
	var startNum, endNum int
	fmt.Sscanf(start, "%d", &startNum)
	fmt.Sscanf(end, "%d", &endNum)
	
	if startNum == 0 || endNum == 0 {
		return nil
	}
	
	return &GridPlacement{
		Start: startNum,
		End:   endNum,
	}
}

// JustifyItems represents the justify-items property value for grid
type JustifyItems string

const (
	JustifyItemsStart   JustifyItems = "start"
	JustifyItemsEnd     JustifyItems = "end"
	JustifyItemsCenter  JustifyItems = "center"
	JustifyItemsStretch JustifyItems = "stretch"
)

// GetJustifyItems returns the justify-items value (default: stretch)
func (s *Style) GetJustifyItems() JustifyItems {
	if val, ok := s.Get("justify-items"); ok {
		switch val {
		case "start":
			return JustifyItemsStart
		case "end":
			return JustifyItemsEnd
		case "center":
			return JustifyItemsCenter
		}
	}
	return JustifyItemsStretch
}

// Note: We can reuse AlignItems from flexbox for align-items in grid

// Phase 16: CSS Transforms

// Transform represents a CSS transform
type Transform struct {
	Type   string    // "translate", "rotate", "scale", "skew"
	Values []float64 // Parameter values
}

// GetTransforms parses the transform property and returns a list of transforms
func (s *Style) GetTransforms() []Transform {
	if val, ok := s.Get("transform"); ok {
		if val == "none" {
			return nil
		}
		return parseTransforms(val)
	}
	return nil
}

// parseTransforms parses transform functions (e.g., "translate(10px, 20px) rotate(45deg)")
func parseTransforms(val string) []Transform {
	transforms := make([]Transform, 0)
	
	// Simple parser for transform functions
	i := 0
	for i < len(val) {
		// Skip whitespace
		for i < len(val) && val[i] == ' ' {
			i++
		}
		if i >= len(val) {
			break
		}
		
		// Find function name
		start := i
		for i < len(val) && val[i] != '(' {
			i++
		}
		if i >= len(val) {
			break
		}
		
		funcName := val[start:i]
		i++ // Skip '('
		
		// Find function arguments
		argStart := i
		depth := 1
		for i < len(val) && depth > 0 {
			if val[i] == '(' {
				depth++
			} else if val[i] == ')' {
				depth--
			}
			i++
		}
		
		args := val[argStart : i-1]
		
		// Parse the transform
		transform := parseTransformFunction(funcName, args)
		if transform != nil {
			transforms = append(transforms, *transform)
		}
	}
	
	return transforms
}

// parseTransformFunction parses a single transform function
func parseTransformFunction(name, args string) *Transform {
	name = strings.TrimSpace(name)
	args = strings.TrimSpace(args)
	
	switch name {
	case "translate":
		// translate(x, y) or translate(x)
		parts := strings.Split(args, ",")
		values := make([]float64, 0)
		for _, part := range parts {
			if val := parseTransformValue(strings.TrimSpace(part)); val != nil {
				values = append(values, *val)
			}
		}
		if len(values) == 1 {
			values = append(values, 0) // y defaults to 0
		}
		if len(values) >= 2 {
			return &Transform{Type: "translate", Values: values[:2]}
		}
		
	case "translateX":
		if val := parseTransformValue(args); val != nil {
			return &Transform{Type: "translate", Values: []float64{*val, 0}}
		}
		
	case "translateY":
		if val := parseTransformValue(args); val != nil {
			return &Transform{Type: "translate", Values: []float64{0, *val}}
		}
		
	case "rotate":
		// rotate(45deg)
		if val := parseAngle(args); val != nil {
			return &Transform{Type: "rotate", Values: []float64{*val}}
		}
		
	case "scale":
		// scale(x, y) or scale(x)
		parts := strings.Split(args, ",")
		values := make([]float64, 0)
		for _, part := range parts {
			if val, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil {
				values = append(values, val)
			}
		}
		if len(values) == 1 {
			values = append(values, values[0]) // y defaults to x
		}
		if len(values) >= 2 {
			return &Transform{Type: "scale", Values: values[:2]}
		}
		
	case "scaleX":
		if val, err := strconv.ParseFloat(args, 64); err == nil {
			return &Transform{Type: "scale", Values: []float64{val, 1}}
		}
		
	case "scaleY":
		if val, err := strconv.ParseFloat(args, 64); err == nil {
			return &Transform{Type: "scale", Values: []float64{1, val}}
		}
	}
	
	return nil
}

// parseTransformValue parses a length value that might be pixels or percentage
func parseTransformValue(val string) *float64 {
	val = strings.TrimSpace(val)
	
	// Check for percentage
	if strings.HasSuffix(val, "%") {
		percentStr := strings.TrimSuffix(val, "%")
		if percent, err := strconv.ParseFloat(percentStr, 64); err == nil {
			// Return negative value to indicate percentage (will be resolved later with element size)
			result := -percent // Negative indicates percentage
			return &result
		}
	}
	
	// Check for px or unitless
	val = strings.TrimSuffix(val, "px")
	if length, err := strconv.ParseFloat(val, 64); err == nil {
		return &length
	}
	
	return nil
}

// parseAngle parses an angle value (deg, rad, turn)
func parseAngle(val string) *float64 {
	val = strings.TrimSpace(val)
	
	// Degrees
	if strings.HasSuffix(val, "deg") {
		degStr := strings.TrimSuffix(val, "deg")
		if deg, err := strconv.ParseFloat(degStr, 64); err == nil {
			return &deg
		}
	}
	
	// Radians
	if strings.HasSuffix(val, "rad") {
		radStr := strings.TrimSuffix(val, "rad")
		if rad, err := strconv.ParseFloat(radStr, 64); err == nil {
			deg := rad * 180 / 3.14159265359
			return &deg
		}
	}
	
	// Turns
	if strings.HasSuffix(val, "turn") {
		turnStr := strings.TrimSuffix(val, "turn")
		if turn, err := strconv.ParseFloat(turnStr, 64); err == nil {
			deg := turn * 360
			return &deg
		}
	}
	
	return nil
}

// TransformOrigin represents the transform-origin property
type TransformOrigin struct {
	X float64 // 0.0 = left, 0.5 = center, 1.0 = right
	Y float64 // 0.0 = top, 0.5 = center, 1.0 = bottom
}

// GetTransformOrigin parses transform-origin (default: center center = 50% 50%)
func (s *Style) GetTransformOrigin() TransformOrigin {
	if val, ok := s.Get("transform-origin"); ok {
		parts := strings.Fields(val)
		origin := TransformOrigin{X: 0.5, Y: 0.5} // Default center center
		
		if len(parts) >= 1 {
			origin.X = parseOriginValue(parts[0])
		}
		if len(parts) >= 2 {
			origin.Y = parseOriginValue(parts[1])
		}
		
		return origin
	}
	return TransformOrigin{X: 0.5, Y: 0.5} // Default center center
}

// parseOriginValue parses a single origin value (left/center/right/top/bottom or percentage)
func parseOriginValue(val string) float64 {
	val = strings.TrimSpace(val)
	
	switch val {
	case "left", "top":
		return 0.0
	case "center":
		return 0.5
	case "right", "bottom":
		return 1.0
	}
	
	// Try percentage
	if strings.HasSuffix(val, "%") {
		percentStr := strings.TrimSuffix(val, "%")
		if percent, err := strconv.ParseFloat(percentStr, 64); err == nil {
			return percent / 100.0
		}
	}
	
	// Try pixels (convert to 0-1 range... but we don't know element size here)
	// For now, just use as-is
	if length, ok := ParseLength(val); ok {
		return length / 100.0 // Rough approximation
	}
	
	return 0.5 // Default to center
}

// Phase 24: Background image support

// ParseURLValue extracts the URL from a CSS url(...) value.
// Handles url(path), url('path'), url("path").
// Returns the URL string and true if valid, or "", false otherwise.
func ParseURLValue(val string) (string, bool) {
	val = strings.TrimSpace(val)
	if !strings.HasPrefix(val, "url(") || !strings.HasSuffix(val, ")") {
		return "", false
	}
	inner := val[4 : len(val)-1]
	inner = strings.TrimSpace(inner)
	// Remove quotes if present
	if len(inner) >= 2 {
		if (inner[0] == '"' && inner[len(inner)-1] == '"') ||
			(inner[0] == '\'' && inner[len(inner)-1] == '\'') {
			inner = inner[1 : len(inner)-1]
		}
	}
	if inner == "" {
		return "", false
	}
	return inner, true
}

// GetBackgroundImage returns the background-image URL if set.
// Checks both background-image and the background shorthand.
func (s *Style) GetBackgroundImage() (string, bool) {
	if val, ok := s.Get("background-image"); ok {
		if url, ok := ParseURLValue(val); ok {
			return url, true
		}
	}
	return "", false
}

// BackgroundRepeatType represents background-repeat values
type BackgroundRepeatType string

const (
	BackgroundRepeatRepeat   BackgroundRepeatType = "repeat"
	BackgroundRepeatNoRepeat BackgroundRepeatType = "no-repeat"
	BackgroundRepeatRepeatX  BackgroundRepeatType = "repeat-x"
	BackgroundRepeatRepeatY  BackgroundRepeatType = "repeat-y"
)

// GetBackgroundAttachment returns the background-attachment value (default: scroll)
func (s *Style) GetBackgroundAttachment() string {
	if val, ok := s.Get("background-attachment"); ok {
		return val
	}
	return "scroll"
}

// GetBackgroundRepeat returns the background-repeat value (default: repeat)
func (s *Style) GetBackgroundRepeat() BackgroundRepeatType {
	if val, ok := s.Get("background-repeat"); ok {
		switch val {
		case "no-repeat":
			return BackgroundRepeatNoRepeat
		case "repeat-x":
			return BackgroundRepeatRepeatX
		case "repeat-y":
			return BackgroundRepeatRepeatY
		}
	}
	return BackgroundRepeatRepeat
}

// BackgroundPosition represents background-position x,y values in pixels
type BackgroundPosition struct {
	X float64
	Y float64
}

// GetBackgroundPosition parses background-position (default: 0 0)
func (s *Style) GetBackgroundPosition() BackgroundPosition {
	val, ok := s.Get("background-position")
	if !ok {
		return BackgroundPosition{0, 0}
	}
	return ParseBackgroundPosition(val)
}

// ParseBackgroundPosition parses a background-position value string
func ParseBackgroundPosition(val string) BackgroundPosition {
	parts := strings.Fields(val)
	pos := BackgroundPosition{}
	if len(parts) >= 1 {
		pos.X = parsePositionComponent(parts[0], true)
	}
	if len(parts) >= 2 {
		pos.Y = parsePositionComponent(parts[1], false)
	} else if len(parts) == 1 {
		// Single value: y defaults to center (but for px values, 0 is fine for Acid2)
		switch parts[0] {
		case "center":
			pos.Y = 0 // will need box dimensions for true center; 0 as fallback
		default:
			pos.Y = 0
		}
	}
	return pos
}

func parsePositionComponent(val string, isX bool) float64 {
	switch val {
	case "left":
		return 0
	case "right":
		return 0 // needs box width; handled at render time
	case "top":
		return 0
	case "bottom":
		return 0 // needs box height; handled at render time
	case "center":
		return 0 // handled at render time
	}
	if length, ok := ParseLength(val); ok {
		return length
	}
	return 0
}

// Phase 23: List styling

// ListStyleType represents the list-style-type property value
type ListStyleType string

const (
	ListStyleTypeDisc    ListStyleType = "disc"
	ListStyleTypeCircle  ListStyleType = "circle"
	ListStyleTypeSquare  ListStyleType = "square"
	ListStyleTypeDecimal ListStyleType = "decimal"
	ListStyleTypeNone    ListStyleType = "none"
)

// GetListStyleType returns the list-style-type value (default: disc)
func (s *Style) GetListStyleType() ListStyleType {
	if val, ok := s.Get("list-style-type"); ok {
		switch val {
		case "disc":
			return ListStyleTypeDisc
		case "circle":
			return ListStyleTypeCircle
		case "square":
			return ListStyleTypeSquare
		case "decimal":
			return ListStyleTypeDecimal
		case "none":
			return ListStyleTypeNone
		default:
			// Handle custom string values (quoted strings like "\2022")
			// Strip quotes if present
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
				return ListStyleType(val[1 : len(val)-1])
			}
			// Return as-is for other values
			return ListStyleType(val)
		}
	}
	return ListStyleTypeDisc
}
