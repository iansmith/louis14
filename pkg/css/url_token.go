package css

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// This file ports the url() tokenization of Blink's CSSTokenizer
// (core/css/parser/css_tokenizer.cc:374-560) and its escape idioms
// (css_parser_idioms.cc:27-56, css_parser_idioms.h:65-67 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), for values louis14 reads as
// raw strings (an SVG filter attribute).

// ConsumeURL consumes a leading url() from s (leading whitespace allowed):
// a url-token, or the url( function holding exactly one string token,
// surrounded by optional whitespace (CSSTokenizer::ConsumeIdentLikeToken,
// :374-389). It returns the decoded URL and the trimmed rest of s. ok is
// false when s does not start with url( or the url() is malformed: a
// bad-url token, a bad string, or text other than whitespace around the
// string (ConsumeUrl then takes nothing). A NUL reads as U+FFFD, as CSS
// Syntax 3 §3.3 preprocessing makes it.
func ConsumeURL(s string) (value, rest string, ok bool) {
	s = strings.TrimLeft(s, cssWhitespace)
	value, end, ok := consumeURLSpan(s)
	if !ok {
		return "", "", false
	}
	return value, strings.TrimSpace(s[end:]), true
}

// consumeURLSpan is ConsumeURL on s starting at url(, reporting the byte
// offset just past the url() instead of the rest.
func consumeURLSpan(s string) (value string, end int, ok bool) {
	if len(s) < 4 || !strings.EqualFold(s[:4], "url(") {
		return "", 0, false
	}
	i := 4
	for i < len(s) && isCSSWhitespace(s[i]) {
		i++
	}
	if i < len(s) && (s[i] == '"' || s[i] == '\'') {
		v, n, ok := consumeStringToken(s[i+1:], s[i])
		if !ok {
			return "", 0, false
		}
		// The url( function's block closes at ')' or at the end of input
		// (BlockGuard), so an unterminated quoted url() is accepted.
		j := i + 1 + n
		for j < len(s) && isCSSWhitespace(s[j]) {
			j++
		}
		if j == len(s) {
			return v, j, true
		}
		if s[j] != ')' {
			return "", 0, false
		}
		return v, j + 1, true
	}
	v, n, ok := consumeURLToken(s[i:])
	return v, i + n, ok
}

// cssWhitespace is the CSS whitespace bytes (isCSSWhitespace).
const cssWhitespace = " \t\n\r\f"

// consumeURLToken mirrors CSSTokenizer::ConsumeUrlToken (:478-529): s is
// after url( and its whitespace. Whitespace may only precede the ')'; a
// quote, '(' or a non-printable code point, or a backslash before a
// newline, makes a bad-url token (ok false). n is the bytes consumed, the
// ')' included.
func consumeURLToken(s string) (value string, n int, ok bool) {
	var out strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ')':
			return out.String(), i + 1, true
		case isCSSWhitespace(c):
			j := i
			for j < len(s) && isCSSWhitespace(s[j]) {
				j++
			}
			if j == len(s) {
				return out.String(), j, true
			}
			if s[j] == ')' {
				return out.String(), j + 1, true
			}
			return "", 0, false
		case c == 0:
			out.WriteRune('\uFFFD')
		case c == '"' || c == '\'' || c == '(' || isNonPrintable(c):
			return "", 0, false
		case c == '\\':
			// A backslash at the end of input is a valid escape (U+FFFD);
			// before a newline it is not (TwoCharsAreValidEscape).
			if i+1 < len(s) && (s[i+1] == '\n' || s[i+1] == '\r' || s[i+1] == '\f') {
				return "", 0, false
			}
			r, m := consumeEscape(s[i+1:])
			out.WriteRune(r)
			i += 1 + m
			continue
		default:
			out.WriteByte(c)
		}
		i++
	}
	return out.String(), len(s), true // end of input closes the token
}

// stringEnd is how a string token ends (CSSTokenizer::ConsumeStringTokenUntil,
// css_tokenizer.cc:393 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type stringEnd uint8

const (
	stringClosed stringEnd = iota // at its closing quote: a <string-token>
	stringBad                     // before a newline: a <bad-string-token>
	stringEOF                     // at end of input: a <string-token>
)

// stringTokenEnd is the index of the last byte of the string token whose
// opening quote is s[i], and how it ended: its closing quote; the byte
// before a newline, which ends a bad string; or s's last byte when end of
// input closes it. An escaped character, newline included, is skipped. A
// scanner resuming after it sees a bad string's newline.
func stringTokenEnd(s string, i int) (int, stringEnd) {
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case quote:
			return j, stringClosed
		case '\n', '\r', '\f':
			return j - 1, stringBad
		case '\\':
			j++
		}
	}
	return len(s) - 1, stringEOF
}

// consumeStringToken mirrors CSSTokenizer::ConsumeStringTokenUntil
// (:393-434): s is after the opening quote. A newline makes a bad string
// (ok false); an escaped newline, or a backslash at the end of input, is
// dropped; end of input closes it. n is the bytes consumed, the closing
// quote included.
func consumeStringToken(s string, quote byte) (value string, n int, ok bool) {
	var out strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == quote:
			return out.String(), i + 1, true
		case c == '\n' || c == '\r' || c == '\f':
			return "", 0, false
		case c == 0:
			out.WriteRune('\uFFFD')
		case c == '\\':
			if i+1 >= len(s) {
				i++
				continue
			}
			if nc := s[i+1]; nc == '\n' || nc == '\r' || nc == '\f' {
				i += 2
				if nc == '\r' && i < len(s) && s[i] == '\n' {
					i++
				}
				continue
			}
			r, m := consumeEscape(s[i+1:])
			out.WriteRune(r)
			i += 1 + m
			continue
		default:
			out.WriteByte(c)
		}
		i++
	}
	return out.String(), len(s), true
}

// consumeEscape mirrors ConsumeEscape (css_parser_idioms.cc:27-56): s is
// after the backslash. Up to six hex digits and one following whitespace
// are a code point (zero, a surrogate or past U+10FFFF is U+FFFD); any
// other character is itself. It returns the code point and the bytes
// consumed.
func consumeEscape(s string) (rune, int) {
	if s == "" {
		return '�', 0
	}
	n := 0
	for n < len(s) && n < 6 && isHexDigit(s[n]) {
		n++
	}
	if n == 0 {
		if s[0] == 0 {
			return '\uFFFD', 1 // preprocessing (§3.3)
		}
		return utf8.DecodeRuneInString(s) // an invalid byte is one U+FFFD
	}
	cp, _ := strconv.ParseInt(s[:n], 16, 32)
	if n < len(s) && isCSSWhitespace(s[n]) {
		if s[n] == '\r' && n+1 < len(s) && s[n+1] == '\n' {
			n++
		}
		n++
	}
	if cp == 0 || (cp >= 0xD800 && cp <= 0xDFFF) || cp > 0x10FFFF {
		return '�', n
	}
	return rune(cp), n
}

// isNonPrintable mirrors IsNonPrintableCodePoint (css_tokenizer.cc:472-475).
func isNonPrintable(c byte) bool {
	return c <= 0x08 || c == 0x0B || (c >= 0x0E && c <= 0x1F) || c == 0x7F
}
