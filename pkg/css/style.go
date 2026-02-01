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
	return ParseLength(val)
}

// ParseLength parses a length value (e.g., "100px" or "100")
func ParseLength(val string) (float64, bool) {
	val = strings.TrimSpace(val)
	val = strings.TrimSuffix(val, "px")
	num, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

// Phase 2: Box model helpers

// BoxEdge represents the four sides of a box (top, right, bottom, left)
type BoxEdge struct {
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
}

// GetMargin returns the margin values for all four sides
func (s *Style) GetMargin() BoxEdge {
	return BoxEdge{
		Top:    s.getLengthOrZero("margin-top"),
		Right:  s.getLengthOrZero("margin-right"),
		Bottom: s.getLengthOrZero("margin-bottom"),
		Left:   s.getLengthOrZero("margin-left"),
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
	return BoxEdge{
		Top:    s.getLengthOrZero("border-top-width"),
		Right:  s.getLengthOrZero("border-right-width"),
		Bottom: s.getLengthOrZero("border-bottom-width"),
		Left:   s.getLengthOrZero("border-left-width"),
	}
}

// getLengthOrZero returns the length value or 0 if not found
func (s *Style) getLengthOrZero(property string) float64 {
	val, ok := s.GetLength(property)
	if !ok {
		return 0
	}
	return val
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
	if pos, ok := s.Get("position"); ok {
		switch pos {
		case "relative":
			return PositionRelative
		case "absolute":
			return PositionAbsolute
		case "fixed":
			return PositionFixed
		}
	}
	return PositionStatic
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

// expandBorderProperty expands border shorthand
// Format: "1px solid black" or "2px dotted #FF0000"
func expandBorderProperty(style *Style, value string) {
	parts := strings.Fields(value)

	for _, part := range parts {
		if strings.HasSuffix(part, "px") {
			// Width
			style.Set("border-width", part)
			style.Set("border-top-width", part)
			style.Set("border-right-width", part)
			style.Set("border-bottom-width", part)
			style.Set("border-left-width", part)
		} else if part == "solid" || part == "dotted" || part == "dashed" || part == "double" {
			// Style
			style.Set("border-style", part)
		} else {
			// Color
			style.Set("border-color", part)
		}
	}
}

type Color struct {
	R, G, B uint8
}

func ParseColor(colorStr string) (Color, bool) {
	colorStr = strings.ToLower(strings.TrimSpace(colorStr))
	namedColors := map[string]Color{
		"red":     {255, 0, 0},
		"green":   {0, 128, 0},
		"blue":    {0, 0, 255},
		"yellow":  {255, 255, 0},
		"cyan":    {0, 255, 255},
		"magenta": {255, 0, 255},
		"white":   {255, 255, 255},
		"black":   {0, 0, 0},
		"gray":    {128, 128, 128},
		"orange":  {255, 165, 0},
		"purple":  {128, 0, 128},
		"pink":    {255, 192, 203},
		"brown":   {165, 42, 42},
		"lime":    {0, 255, 0},
		"navy":    {0, 0, 128},
		"teal":    {0, 128, 128},
		"silver":  {192, 192, 192},
	}
	color, ok := namedColors[colorStr]
	return color, ok
}

// Phase 6: Text rendering helpers

// GetFontSize returns the font-size in pixels (default: 16px)
func (s *Style) GetFontSize() float64 {
	if size, ok := s.GetLength("font-size"); ok {
		return size
	}
	return 16.0 // Default font size
}

// GetColor returns the text color (default: black)
func (s *Style) GetColor() Color {
	if colorStr, ok := s.Get("color"); ok {
		if color, ok := ParseColor(colorStr); ok {
			return color
		}
	}
	return Color{0, 0, 0} // Default to black
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

// Phase 7: Display modes

// DisplayType represents the display property value
type DisplayType string

const (
	DisplayBlock       DisplayType = "block"
	DisplayInline      DisplayType = "inline"
	DisplayInlineBlock DisplayType = "inline-block"
	DisplayNone        DisplayType = "none"
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

// GetLineHeight returns the line-height in pixels (default: 1.2 * font-size)
func (s *Style) GetLineHeight() float64 {
	if lh, ok := s.GetLength("line-height"); ok {
		return lh
	}
	// Default to 1.2x font size
	return s.GetFontSize() * 1.2
}
