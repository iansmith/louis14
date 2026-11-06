package html

import "strconv"

// HTMLDimensionType is the kind of an HTML dimension value: Blink's
// HTMLDimension::HTMLDimensionType (html/html_dimension.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type HTMLDimensionType int

const (
	HTMLDimensionAbsolute HTMLDimensionType = iota
	HTMLDimensionPercentage
	HTMLDimensionRelative
)

// HTMLDimension is a parsed dimension value (Blink's HTMLDimension).
type HTMLDimension struct {
	Value float64
	Type  HTMLDimensionType
}

// IsAbsolute reports whether d is a plain length, not a percentage or a
// relative ('*') value.
func (d HTMLDimension) IsAbsolute() bool { return d.Type == HTMLDimensionAbsolute }

// ParseDimensionValue implements the rules for parsing dimension values as
// Blink does (ParseDimensionValue, html/html_dimension.cc:146-192 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): skip leading HTML spaces; read
// digits and an optional '.' with more digits (a sign is not accepted); a
// following '%' makes a percentage and '*' a relative value; anything else
// trails as garbage.
func ParseDimensionValue(input string) (HTMLDimension, bool) {
	i := 0
	for i < len(input) && isHTMLSpace(input[i]) {
		i++
	}
	start := i
	for i < len(input) && input[i] >= '0' && input[i] <= '9' {
		i++
	}
	if i == start {
		return HTMLDimension{}, false
	}
	if i < len(input) && input[i] == '.' {
		i++
		for i < len(input) && input[i] >= '0' && input[i] <= '9' {
			i++
		}
	}
	v, err := strconv.ParseFloat(input[start:i], 64)
	if err != nil {
		return HTMLDimension{}, false
	}
	d := HTMLDimension{Value: v, Type: HTMLDimensionAbsolute}
	if i < len(input) {
		switch input[i] {
		case '%':
			d.Type = HTMLDimensionPercentage
		case '*':
			d.Type = HTMLDimensionRelative
		}
	}
	return d, true
}

// isHTMLSpace reports whether c is HTML whitespace: space, tab, LF, FF or CR
// (Blink's IsHTMLSpace, html/parser/html_parser_idioms.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}
