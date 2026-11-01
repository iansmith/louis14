package text

import (
	"strconv"
	"strings"

	"mazarin/textshape"
)

// fontFeatureTagLength is the length of an OpenType feature tag.
const fontFeatureTagLength = 4

// cssWhitespace is the CSS whitespace set that separates tokens.
const cssWhitespace = " \t\n\r\f"

// ParseFontFeatureSettings parses a font-feature-settings value (the
// property or the @font-face descriptor): `normal`, or a comma-separated
// list of a 4-character string tag (U+0020-U+007E) optionally followed by an
// <integer>, `on` or `off` (default 1). ok is false when the value is
// malformed; one bad entry invalidates the whole list. Mirrors Blink's
// ConsumeFontSettings / ConsumeFontSettingsTagAndValue
// (core/css/properties/css_parsing_utils.cc:6630-6704 @
// 5992657f556bcd9d207a4aca187488462f027cab), which returns nullptr for the
// list rather than a partial one; the integer is stored as Blink's
// FontFeatureRange does, static_cast<uint32_t>
// (platform/fonts/shaping/font_features.cc:218-219 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func ParseFontFeatureSettings(value string) (features []textshape.FontFeature, ok bool) {
	p := fontSettingsParser{s: value}
	p.skipSpace()
	if p.consumeIdentIf("normal") {
		p.skipSpace()
		return nil, p.atEnd()
	}
	for {
		f, ok := p.consumeTagAndValue()
		if !ok {
			return nil, false
		}
		features = append(features, f)
		p.skipSpace()
		if p.atEnd() {
			return features, true
		}
		if p.s[p.i] != ',' {
			return nil, false
		}
		p.i++
		p.skipSpace()
	}
}

// fontSettingsParser walks a font-feature-settings value token by token.
type fontSettingsParser struct {
	s string
	i int
}

func (p *fontSettingsParser) atEnd() bool { return p.i >= len(p.s) }

func (p *fontSettingsParser) skipSpace() {
	for p.i < len(p.s) && strings.IndexByte(cssWhitespace, p.s[p.i]) >= 0 {
		p.i++
	}
}

// ident returns the identifier starting at p.i without consuming it.
func (p *fontSettingsParser) ident() string {
	j := p.i
	for j < len(p.s) {
		c := p.s[j]
		isLetter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '-' || c == '_'
		if !isLetter && !(j > p.i && '0' <= c && c <= '9') {
			break
		}
		j++
	}
	return p.s[p.i:j]
}

func (p *fontSettingsParser) consumeIdentIf(name string) bool {
	if id := p.ident(); strings.EqualFold(id, name) {
		p.i += len(id)
		return true
	}
	return false
}

// consumeTagAndValue consumes `<string> [<integer> | on | off]?`.
func (p *fontSettingsParser) consumeTagAndValue() (textshape.FontFeature, bool) {
	var f textshape.FontFeature
	if p.atEnd() || (p.s[p.i] != '"' && p.s[p.i] != '\'') {
		return f, false
	}
	quote := p.s[p.i]
	end := strings.IndexByte(p.s[p.i+1:], quote)
	if end != fontFeatureTagLength {
		return f, false
	}
	tag := p.s[p.i+1 : p.i+1+end]
	for i := 0; i < fontFeatureTagLength; i++ {
		if tag[i] < 0x20 || tag[i] > 0x7E {
			return f, false
		}
	}
	copy(f.Tag[:], tag)
	p.i += end + 2

	f.Value = 1
	p.skipSpace()
	if p.atEnd() || p.s[p.i] == ',' {
		return f, true
	}
	switch {
	case p.consumeIdentIf("on"):
	case p.consumeIdentIf("off"):
		f.Value = 0
	default:
		j := p.i
		if j < len(p.s) && (p.s[j] == '+' || p.s[j] == '-') {
			j++
		}
		for j < len(p.s) && '0' <= p.s[j] && p.s[j] <= '9' {
			j++
		}
		n, err := strconv.ParseInt(p.s[p.i:j], 10, 32)
		if err != nil {
			return f, false
		}
		p.i = j
		f.Value = uint32(n)
	}
	// The value must end its entry: `1.5`, `2px`, `onx` are not tokens
	// ConsumeInteger / the on|off idents accept.
	if !p.atEnd() && p.s[p.i] != ',' && strings.IndexByte(cssWhitespace, p.s[p.i]) < 0 {
		return f, false
	}
	return f, true
}
