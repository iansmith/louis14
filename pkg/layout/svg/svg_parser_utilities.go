package svg

import (
	"math"
	"strconv"
)

// This file ports the number parsing of Blink's core/svg/svg_parser_utilities
// (svg_parser_utilities.cc:42-207 and svg_parser_utilities.h:47-95 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// isSVGSpace mirrors IsHTMLSpace, the whitespace SkipOptionalSVGSpaces skips.
func isSVGSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// skipOptionalSVGSpaces mirrors SkipOptionalSVGSpaces.
func skipOptionalSVGSpaces(s string) string {
	for len(s) > 0 && isSVGSpace(s[0]) {
		s = s[1:]
	}
	return s
}

// skipOptionalSVGSpacesOrDelimiter mirrors SkipOptionalSVGSpacesOrDelimiter
// with the ',' delimiter: spaces, then at most one comma, then spaces.
func skipOptionalSVGSpacesOrDelimiter(s string) string {
	if len(s) > 0 && !isSVGSpace(s[0]) && s[0] != ',' {
		return s
	}
	s = skipOptionalSVGSpaces(s)
	if len(s) > 0 && s[0] == ',' {
		s = skipOptionalSVGSpaces(s[1:])
	}
	return s
}

// ParseNumber parses an SVG number at the start of s, skipping leading
// spaces and, after it, spaces and one optional comma; it returns the number
// and the rest of s. Mirrors GenericParseNumber with
// kAllowLeadingAndTrailingWhitespace: a sign, digits with an optional
// fraction (at least one digit in all), and an exponent unless the 'e'
// starts an "em" or "ex" unit. Infinities, NaN and hex floats are not
// numbers, nor is a value outside float's range.
func ParseNumber(s string) (float64, string, bool) {
	s = skipOptionalSVGSpaces(s)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if i >= len(s) || (!isDigit(s[i]) && s[i] != '.') {
		return 0, "", false
	}
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		if i >= len(s) || !isDigit(s[i]) {
			return 0, "", false
		}
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	}
	if i+1 < len(s) && (s[i] == 'e' || s[i] == 'E') && s[i+1] != 'x' && s[i+1] != 'm' {
		j := i + 1
		if s[j] == '+' || s[j] == '-' {
			j++
		}
		if j >= len(s) || !isDigit(s[j]) {
			return 0, "", false
		}
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		i = j
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || math.Abs(v) > math.MaxFloat32 {
		return 0, "", false
	}
	return v, skipOptionalSVGSpacesOrDelimiter(s[i:]), true
}

// ParseNumberOptionalNumber parses one number, or two, as the whole of s; a
// single number is both. Mirrors ParseNumberOptionalNumber
// (svg_parser_utilities.cc:189-207).
func ParseNumberOptionalNumber(s string) (x, y float64, ok bool) {
	if s == "" {
		return 0, 0, false
	}
	x, rest, ok := ParseNumber(s)
	if !ok {
		return 0, 0, false
	}
	if rest == "" {
		return x, x, true
	}
	y, rest, ok = ParseNumber(rest)
	if !ok || rest != "" {
		return 0, 0, false
	}
	return x, y, true
}
