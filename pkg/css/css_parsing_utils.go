package css

import (
	"strconv"
	"strings"
)

// Ports of the background-related consumers in Blink's
// core/css/properties/css_parsing_utils.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b: ParseBackgroundOrMask (:5369),
// ConsumeBackgroundSize (:5182), ConsumePosition (:2649) with
// ConsumePositionComponent (:2528) and PositionFromOne/Two/ThreeOrFourValues
// (:2575-:2647), ConsumeRepeatStyleValue (:5541), ConsumeBackgroundAttachment
// (:5108), ConsumeBackgroundBox (:5122), ConsumeBackgroundClip (:5128).
//
// louis14 stores each background longhand as a comma-joined string (one entry
// per layer) rather than as CSSValues, so the consumers here validate and
// return the consumed component text; the typed values are built when the
// longhands are read (GetBackgroundLayers / parseBackgroundPosition).

// componentStream is a cursor over the component values of a declaration
// value — the analog of CSSParserTokenStream for louis14's string-valued
// parser. Each token is one whitespace-separated component value (a function
// with its arguments is one token); top-level ',' and '/' are their own
// tokens. save/restore mirror CSSParserTokenStream::Save/Restore.
type componentStream struct {
	toks []string
	pos  int
}

func newComponentStream(value string) *componentStream {
	return &componentStream{toks: splitComponentValues(value)}
}

func (s *componentStream) atEnd() bool { return s.pos >= len(s.toks) }

// peek returns the next token, or "" at the end of the stream.
func (s *componentStream) peek() string {
	if s.atEnd() {
		return ""
	}
	return s.toks[s.pos]
}

func (s *componentStream) save() int { return s.pos }

// since returns the tokens consumed after save point p, space-joined.
func (s *componentStream) since(p int) string { return strings.Join(s.toks[p:s.pos], " ") }
func (s *componentStream) restore(p int)      { s.pos = p }

// consume returns the next token and advances.
func (s *componentStream) consume() string {
	t := s.peek()
	s.pos++
	return t
}

// peekIdent returns the next token lower-cased when it is an <ident-token>,
// else "".
func (s *componentStream) peekIdent() string {
	if t := s.peek(); isIdentToken(t) {
		return strings.ToLower(t)
	}
	return ""
}

// consumeIdent consumes the next token when it is one of the given
// (lower-case) identifiers — Blink's ConsumeIdent<...>.
func (s *componentStream) consumeIdent(ids ...string) string {
	id := s.peekIdent()
	for _, want := range ids {
		if id == want {
			s.pos++
			return id
		}
	}
	return ""
}

// consumeComma / consumeSlash mirror ConsumeCommaIncludingWhitespace and
// ConsumeSlashIncludingWhitespace (css_parsing_utils.cc:1038).
func (s *componentStream) consumeComma() bool { return s.consumeDelim(",") }
func (s *componentStream) consumeSlash() bool { return s.consumeDelim("/") }

func (s *componentStream) consumeDelim(d string) bool {
	if s.peek() == d {
		s.pos++
		return true
	}
	return false
}

// splitComponentValues splits a declaration value into component values:
// whitespace separates tokens outside parentheses and quotes, and top-level
// ',' and '/' are emitted as standalone tokens.
func splitComponentValues(value string) []string {
	var toks []string
	depth := 0
	var quote byte
	start := -1
	flush := func(end int) {
		if start >= 0 {
			toks = append(toks, value[start:end])
			start = -1
		}
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case depth == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'):
			flush(i)
			continue
		case depth == 0 && (c == ',' || c == '/'):
			flush(i)
			toks = append(toks, string(c))
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(value))
	return toks
}

// isIdentToken reports whether tok is an <ident-token> (not a function, number
// or dimension).
func isIdentToken(tok string) bool {
	if tok == "" || strings.ContainsAny(tok, "()'\",/") {
		return false
	}
	c := tok[0]
	if c == '-' && len(tok) > 1 {
		c = tok[1]
	}
	return c == '-' || c == '_' || c >= 0x80 || (c|0x20 >= 'a' && c|0x20 <= 'z')
}

// consumeLengthOrPercent reports whether tok is a <length-percentage> —
// Blink's ConsumeLengthOrPercent. nonNegative mirrors
// CSSPrimitiveValue::ValueRange::kNonNegative; allowUnitless mirrors
// UnitlessQuirk::kAllow (a bare non-zero number as px). Math functions are
// accepted without a range check: Blink clamps a calc() to the range at use
// time instead of rejecting it at parse time.
func isLengthPercentToken(tok string, nonNegative, allowUnitless bool) bool {
	if tok == "" || isIdentToken(tok) {
		return false
	}
	if IsMathFunction(tok) {
		return true
	}
	if pct, ok := ParsePercentage(tok); ok {
		return !nonNegative || pct >= 0
	}
	if n, err := strconv.ParseFloat(tok, 64); err == nil {
		// A unitless zero is a valid <length>; other numbers only under the
		// unitless quirk.
		return (n == 0 || allowUnitless) && (!nonNegative || n >= 0)
	}
	if strings.Contains(tok, "(") {
		return false
	}
	px, ok := ParseLength(tok)
	return ok && (!nonNegative || px >= 0)
}

// positionValue is one consumed <position> component: an edge or center
// keyword (ident set, lower-case) or a <length-percentage> (lp set).
type positionValue struct {
	ident string
	lp    string
}

func (v positionValue) isIdent() bool { return v.ident != "" }

// positionAxis is one resolved axis of a <position>: an edge/center keyword,
// a <length-percentage>, or an edge keyword followed by an offset (the
// CSSValuePair built by PositionFromThreeOrFourValues).
type positionAxis struct {
	edge   string // left/right/top/bottom/center; "" when offset-only
	offset string // <length-percentage>; "" when keyword-only
}

func axisOf(v positionValue) positionAxis {
	if v.isIdent() {
		return positionAxis{edge: v.ident}
	}
	return positionAxis{offset: v.lp}
}

// consumePositionComponent mirrors ConsumePositionComponent
// (css_parsing_utils.cc:2528): a <length-percentage>, or one of
// left/right/top/bottom/center, rejecting a second keyword on an axis that
// already has an edge.
func consumePositionComponent(s *componentStream, allowUnitless bool, horizontalEdge, verticalEdge *bool) (positionValue, bool) {
	id := s.peekIdent()
	if id == "" {
		if tok := s.peek(); isLengthPercentToken(tok, false, allowUnitless) {
			s.consume()
			return positionValue{lp: tok}, true
		}
		return positionValue{}, false
	}
	switch id {
	case "left", "right":
		if *horizontalEdge {
			return positionValue{}, false
		}
		*horizontalEdge = true
	case "top", "bottom":
		if *verticalEdge {
			return positionValue{}, false
		}
		*verticalEdge = true
	case "center":
	default:
		return positionValue{}, false
	}
	s.consume()
	return positionValue{ident: id}, true
}

func isHorizontalPositionKeywordOnly(v positionValue) bool { return isHorizontalKeyword(v.ident) }

func isVerticalPositionKeywordOnly(v positionValue) bool { return isVerticalKeyword(v.ident) }

// positionFromOneValue mirrors PositionFromOneValue (:2575).
func positionFromOneValue(v positionValue) (x, y positionAxis) {
	x, y = axisOf(v), positionAxis{edge: "center"}
	if isVerticalPositionKeywordOnly(v) {
		x, y = y, x
	}
	return x, y
}

// positionFromTwoValues mirrors PositionFromTwoValues (:2586).
func positionFromTwoValues(v1, v2 positionValue) (x, y positionAxis) {
	x, y = axisOf(v1), axisOf(v2)
	if isVerticalPositionKeywordOnly(v1) || isHorizontalPositionKeywordOnly(v2) {
		x, y = y, x
	}
	return x, y
}

// positionFromThreeOrFourValues mirrors PositionFromThreeOrFourValues
// (:2604): each edge keyword takes the following <length-percentage> as its
// offset; a lone center fills whichever axis is left.
func positionFromThreeOrFourValues(values []positionValue) (x, y positionAxis) {
	var hasX, hasY, hasCenter bool
	for i := 0; i < len(values); i++ {
		id := values[i].ident
		if id == "center" {
			hasCenter = true
			continue
		}
		axis := positionAxis{edge: id}
		if i+1 < len(values) && !values[i+1].isIdent() {
			i++
			axis.offset = values[i].lp
		}
		if id == "left" || id == "right" {
			x, hasX = axis, true
		} else {
			y, hasY = axis, true
		}
	}
	if hasCenter {
		if !hasX {
			x = positionAxis{edge: "center"}
		} else if !hasY {
			y = positionAxis{edge: "center"}
		}
	}
	return x, y
}

// consumePosition mirrors ConsumePosition (css_parsing_utils.cc:2649) for the
// background case, where three-value positions are permitted
// (WebFeature::kThreeValuedPositionBackground).
func consumePosition(s *componentStream, allowUnitless bool) (x, y positionAxis, ok bool) {
	var horizontalEdge, verticalEdge bool
	v1, ok := consumePositionComponent(s, allowUnitless, &horizontalEdge, &verticalEdge)
	if !ok {
		return x, y, false
	}
	if !v1.isIdent() {
		horizontalEdge = true
	}

	afterFirst := s.save()
	v2, ok := consumePositionComponent(s, allowUnitless, &horizontalEdge, &verticalEdge)
	if !ok {
		x, y = positionFromOneValue(v1)
		return x, y, true
	}

	var v3 positionValue
	have3 := false
	nextIsIdent := s.peekIdent() != ""
	if v1.isIdent() && v2.isIdent() != nextIsIdent {
		which := v1.ident
		if v2.isIdent() {
			which = v2.ident
		}
		if which != "center" {
			v3, have3 = consumePositionComponent(s, allowUnitless, &horizontalEdge, &verticalEdge)
		}
	}
	if !have3 {
		if verticalEdge && !v2.isIdent() {
			s.restore(afterFirst)
			x, y = positionFromOneValue(v1)
			return x, y, true
		}
		x, y = positionFromTwoValues(v1, v2)
		return x, y, true
	}

	values := []positionValue{v1, v2, v3}
	if v3.isIdent() && v3.ident != "center" && s.peekIdent() == "" {
		if v4, ok := consumePositionComponent(s, allowUnitless, &horizontalEdge, &verticalEdge); ok {
			values = append(values, v4)
		}
	}
	x, y = positionFromThreeOrFourValues(values)
	return x, y, true
}

// consumeBackgroundSize mirrors ConsumeBackgroundSize (css_parsing_utils.cc:
// 5182) with ParsingStyle::kNotLegacy: contain | cover | [ auto |
// <length-percentage [0,∞]> ]{1,2}. Returns the consumed text.
func consumeBackgroundSize(s *componentStream) (string, bool) {
	if id := s.consumeIdent("contain", "cover"); id != "" {
		return id, true
	}
	horizontal := s.consumeIdent("auto")
	if horizontal == "" {
		if tok := s.peek(); isLengthPercentToken(tok, true, false) {
			horizontal = s.consume()
		}
	}
	if horizontal == "" {
		return "", false
	}
	if s.consumeIdent("auto") != "" { // `auto' is the default
		return horizontal, true
	}
	if tok := s.peek(); isLengthPercentToken(tok, true, false) {
		return horizontal + " " + s.consume(), true
	}
	return horizontal, true
}

// consumeRepeatStyleValue mirrors ConsumeRepeatStyleValue (:5541).
func consumeRepeatStyleValue(s *componentStream) string {
	if id := s.consumeIdent("repeat-x", "repeat-y"); id != "" {
		return id
	}
	repeatStyle := []string{"repeat", "no-repeat", "round", "space"}
	if id1 := s.consumeIdent(repeatStyle...); id1 != "" {
		if id2 := s.consumeIdent(repeatStyle...); id2 != "" {
			return id1 + " " + id2
		}
		return id1
	}
	return ""
}

// consumeImageOrNone mirrors ConsumeImageOrNone (:3417): none or an <image>.
func consumeImageOrNone(s *componentStream) string {
	if id := s.consumeIdent("none"); id != "" {
		return id
	}
	if tok := s.peek(); !isIdentToken(tok) && isValidSingleImageLayer(tok) {
		return s.consume()
	}
	return ""
}

// consumeBackgroundColor mirrors ConsumeColor for the background shorthand's
// final-layer <color>.
func consumeBackgroundColor(s *componentStream) string {
	tok := s.peek()
	if tok == "" || tok == "," || tok == "/" {
		return ""
	}
	// CSS-wide keywords (and `default`) are never a <color> component;
	// isValidColorValue admits them as whole-declaration values.
	if _, wide := cssWideKeyword(tok); wide || strings.EqualFold(tok, "default") {
		return ""
	}
	if isValidColorValue(tok) {
		return s.consume()
	}
	return ""
}

// backgroundShorthand lists the background longhands in Blink's
// backgroundShorthand() order (css_properties.json5:9059 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with background-position-x/-y
// folded into louis14's single background-position longhand. Each entry's
// initial is the value ParseBackgroundOrMask's CSSInitialValue resolves to.
var backgroundShorthand = [...]struct {
	property string
	initial  string
}{
	{"background-image", "none"},
	{"background-position", "0% 0%"},
	{"background-size", "auto"},
	{"background-repeat", "repeat"},
	{"background-attachment", "scroll"},
	{"background-origin", "padding-box"},
	{"background-clip", "border-box"},
	{"background-color", "transparent"},
}

// Indices into backgroundShorthand.
const (
	bgImage = iota
	bgPosition
	bgSize
	bgRepeat
	bgAttachment
	bgOrigin
	bgClip
	bgColor
)

// parseBackgroundOrMask mirrors ParseBackgroundOrMask
// (css_parsing_utils.cc:5369 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for
// the background shorthand. Per layer, the components are consumed in any
// order, each longhand at most once; `/ <bg-size>` must directly follow a
// <bg-position>; a longhand the layer does not mention is reset to its initial
// value (background-clip defaults to the layer's background-origin); a
// <color> is only allowed in the final layer. It returns the per-longhand
// values (comma-joined across layers, background-color the final layer's
// only), or ok=false when the declaration is invalid.
func parseBackgroundOrMask(value string) (longhands [len(backgroundShorthand)]string, ok bool) {
	s := newComponentStream(value)
	var layers [len(backgroundShorthand)][]string
	previousLayerHadBackgroundColor := false
	for {
		var parsed [len(backgroundShorthand)]bool
		var layer [len(backgroundShorthand)]string
		foundAny := false
		for {
			foundProperty := false
			bgPositionParsedInCurrentLayer := false
			for i := range backgroundShorthand {
				if parsed[i] {
					continue
				}
				var v string
				switch i {
				case bgPosition:
					start := s.save()
					if _, _, ok := consumePosition(s, false); !ok {
						continue
					}
					v = s.since(start)
					bgPositionParsedInCurrentLayer = true
				case bgSize:
					if !s.consumeSlash() {
						continue
					}
					size, ok := consumeBackgroundSize(s)
					if !ok || !bgPositionParsedInCurrentLayer {
						return longhands, false
					}
					v = size
				case bgImage:
					v = consumeImageOrNone(s)
				case bgRepeat:
					v = consumeRepeatStyleValue(s)
				case bgAttachment:
					v = s.consumeIdent("scroll", "fixed", "local")
				case bgOrigin:
					v = s.consumeIdent("border-box", "padding-box", "content-box")
				case bgClip:
					// ConsumeBackgroundClip: <visual-box> | text.
					v = s.consumeIdent("border-box", "padding-box", "content-box", "text")
				case bgColor:
					v = consumeBackgroundColor(s)
				}
				if v != "" {
					parsed[i], layer[i] = true, v
					foundProperty, foundAny = true, true
				}
			}
			if !foundProperty || s.atEnd() || s.peek() == "," {
				break
			}
		}
		if !foundAny || previousLayerHadBackgroundColor {
			// No component, or a color in a layer that turned out not to be
			// the last one.
			return longhands, false
		}
		previousLayerHadBackgroundColor = parsed[bgColor]
		for i, lh := range backgroundShorthand {
			if !parsed[i] {
				layer[i] = lh.initial
				if i == bgClip && parsed[bgOrigin] {
					layer[i] = layer[bgOrigin]
				}
			}
			layers[i] = append(layers[i], layer[i])
		}
		if !s.consumeComma() {
			break
		}
	}
	if !s.atEnd() {
		return longhands, false
	}
	for i := range backgroundShorthand {
		if i == bgColor {
			longhands[i] = layers[i][len(layers[i])-1]
		} else {
			longhands[i] = strings.Join(layers[i], ", ")
		}
	}
	return longhands, true
}

// expandBackgroundProperty sets the background longhands from the background
// shorthand via parseBackgroundOrMask. An invalid declaration sets nothing,
// so an earlier valid declaration survives.
func expandBackgroundProperty(style *Style, value string) {
	// var() / attr() defer expansion (resolved at cascade time; CSS Syntax
	// §4.3.10, CSS Values 5 §11.5).
	if containsVarFunction(value) || containsAttrFunction(value) {
		style.Set("background-color", value)
		return
	}
	longhands, ok := parseBackgroundOrMask(value)
	if !ok {
		return
	}
	for i, lh := range backgroundShorthand {
		style.Set(lh.property, longhands[i])
	}
}

// isValidBackgroundSizeValue validates a background-size longhand —
// ParseBackgroundSize (:5290, a comma list of ConsumeBackgroundSize).
// CSS-wide keywords and var()/attr() are accepted as-is.
func isValidBackgroundSizeValue(value string) bool {
	if _, wide := cssWideKeyword(value); wide {
		return true
	}
	if containsVarFunction(value) || containsAttrFunction(value) {
		return true
	}
	s := newComponentStream(value)
	for {
		if _, ok := consumeBackgroundSize(s); !ok {
			return false
		}
		if !s.consumeComma() {
			return s.atEnd()
		}
	}
}
