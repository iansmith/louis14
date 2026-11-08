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
// string (ConsumeUrl then takes nothing).
func ConsumeURL(s string) (value, rest string, ok bool) {
	s = strings.TrimLeft(s, cssWhitespace)
	if len(s) < 4 || !strings.EqualFold(s[:4], "url(") {
		return "", "", false
	}
	s = strings.TrimLeft(s[4:], cssWhitespace)
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		v, after, ok := consumeStringToken(s[1:], s[0])
		if !ok {
			return "", "", false
		}
		after = strings.TrimLeft(after, cssWhitespace)
		if after == "" || after[0] != ')' {
			return "", "", false
		}
		return v, strings.TrimSpace(after[1:]), true
	}
	return consumeURLToken(s)
}

// cssWhitespace is the CSS whitespace bytes (isCSSWhitespace).
const cssWhitespace = " \t\n\r\f"

// consumeURLToken mirrors CSSTokenizer::ConsumeUrlToken (:478-529): s is
// after url( and its whitespace. Whitespace may only precede the ')'; a
// quote, '(' or a non-printable code point, or an invalid escape, makes a
// bad-url token (ok false).
func consumeURLToken(s string) (value, rest string, ok bool) {
	var out strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ')':
			return out.String(), strings.TrimSpace(s[i+1:]), true
		case isCSSWhitespace(c):
			after := strings.TrimLeft(s[i:], cssWhitespace)
			if after == "" {
				return out.String(), "", true
			}
			if after[0] == ')' {
				return out.String(), strings.TrimSpace(after[1:]), true
			}
			return "", "", false
		case c == '"' || c == '\'' || c == '(' || isNonPrintable(c):
			return "", "", false
		case c == '\\':
			if i+1 >= len(s) || s[i+1] == '\n' || s[i+1] == '\r' || s[i+1] == '\f' {
				return "", "", false
			}
			r, n := consumeEscape(s[i+1:])
			out.WriteRune(r)
			i += 1 + n
			continue
		default:
			out.WriteByte(c)
		}
		i++
	}
	return out.String(), "", true // end of input closes the token
}

// consumeStringToken mirrors CSSTokenizer::ConsumeStringTokenUntil
// (:393-434): s is after the opening quote. A newline makes a bad string
// (ok false); an escaped newline is dropped; end of input closes it.
func consumeStringToken(s string, quote byte) (value, rest string, ok bool) {
	var out strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == quote:
			return out.String(), s[i+1:], true
		case c == '\n' || c == '\r' || c == '\f':
			return "", "", false
		case c == '\\':
			if i+1 >= len(s) {
				i++
				continue
			}
			if n := s[i+1]; n == '\n' || n == '\r' || n == '\f' {
				i += 2
				if n == '\r' && i < len(s) && s[i] == '\n' {
					i++
				}
				continue
			}
			r, n := consumeEscape(s[i+1:])
			out.WriteRune(r)
			i += 1 + n
			continue
		default:
			out.WriteByte(c)
		}
		i++
	}
	return out.String(), "", true
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
