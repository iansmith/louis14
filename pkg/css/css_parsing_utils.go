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
// longhands are read (GetBackgroundLayers / FillLayer.mapFillPosition).

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
// whitespace separates tokens outside parentheses, brackets and quotes, and
// top-level ',' and '/' are emitted as standalone tokens. A bracketed block
// (a grid <line-names>) is one token, as a [-block is in CSS Syntax.
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
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
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

// isDeferredOrCSSWideValue reports whether value is a CSS-wide keyword, or
// holds a var() or attr() reference (or a shorthand's pending substitution)
// resolved at computed-value time, so a parse-time validator accepts it
// as-is.
func isDeferredOrCSSWideValue(value string) bool {
	if _, wide := cssWideKeyword(value); wide {
		return true
	}
	_, _, pending := parsePendingSubstitution(value)
	return pending || containsVarFunction(value) || containsAttrFunction(value)
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

// consumePosition mirrors ConsumePosition (css_parsing_utils.cc:2649 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). threeValuePosition mirrors a
// non-null three_value_position WebFeature: background-position passes it
// (WebFeature::kThreeValuedPositionBackground) and accepts
// "<edge> <length-percentage> <edge>"; object-position does not
// (ObjectPosition::ParseSingleValue, longhands_custom.cc:7317), so a
// three-value form backs up to its one- or two-value prefix and the
// trailing component is left unconsumed.
func consumePosition(s *componentStream, allowUnitless, threeValuePosition bool) (x, y positionAxis, ok bool) {
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

	afterSecond := s.save()
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
	have4 := false
	if v3.isIdent() && v3.ident != "center" && s.peekIdent() == "" {
		var v4 positionValue
		if v4, have4 = consumePositionComponent(s, allowUnitless, &horizontalEdge, &verticalEdge); have4 {
			values = append(values, v4)
		}
	}
	if !have4 && !threeValuePosition {
		// [top | bottom] <length-percentage> is not permitted.
		if verticalEdge && !v2.isIdent() {
			s.restore(afterFirst)
			x, y = positionFromOneValue(v1)
			return x, y, true
		}
		s.restore(afterSecond)
		x, y = positionFromTwoValues(v1, v2)
		return x, y, true
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
					if _, _, ok := consumePosition(s, false, true); !ok {
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
	// attr() defers expansion (resolved at cascade time; CSS Values 5
	// §11.5); expandShorthand makes a var() value pending substitution.
	if containsAttrFunction(value) {
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

// isValidImageOrientationValue validates an image-orientation declaration:
// from-image | none (ImageOrientation::ParseSingleValue,
// longhands_custom.cc:5381-5387 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// CSS-wide keywords and var()/attr() are accepted as-is.
func isValidImageOrientationValue(value string) bool {
	if isDeferredOrCSSWideValue(value) {
		return true
	}
	s := newComponentStream(value)
	return s.consumeIdent("from-image", "none") != "" && s.atEnd()
}

// Display keyword classes of Display::ParseSingleValue
// (longhands_custom.cc:3682-3711 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// grid-lanes / inline-grid-lanes are gated on the experimental
// CSSGridLanesLayout feature and so stay invalid.
var (
	displayOutside  = []string{"inline", "block"}
	displayInside   = []string{"flow-root", "flow", "table", "flex", "grid", "math", "ruby"}
	displayBox      = []string{"none", "contents"}
	displayInternal = []string{
		"table-row-group", "table-header-group", "table-footer-group", "table-row",
		"table-column-group", "table-column", "table-cell", "table-caption", "ruby-text",
	}
	displayLegacy = []string{
		"inline-block", "inline-table", "inline-flex", "inline-grid",
		"-webkit-box", "-webkit-inline-box", "-webkit-flex", "-webkit-inline-flex",
	}
)

// IsValidDisplayValue validates a display declaration — Display::
// ParseSingleValue (longhands_custom.cc:3851-3876): one keyword from the
// list-item / box / internal / legacy / inside / outside classes, or two or
// three keywords that ValidateDisplayKeywords (:3726-3748) splits into at
// most one <display-outside>, one <display-inside> and one `list-item`, with
// a list-item's inside limited to flow or flow-root
// (ParseDisplayMultipleKeywords, :3823-3828). Anything else — the removed
// ruby-base / ruby-base-container / ruby-text-container among them — drops
// the declaration. The layout() functions need CSSLayoutAPI, which louis14
// does not support. CSS-wide keywords and var()/attr() are accepted as-is.
func IsValidDisplayValue(value string) bool {
	if isDeferredOrCSSWideValue(value) {
		return true
	}
	s := newComponentStream(value)
	if s.atEnd() || len(s.toks) > 3 {
		return false
	}
	if len(s.toks) == 1 {
		return s.consumeIdent("list-item") != "" || s.consumeIdent(displayBox...) != "" ||
			s.consumeIdent(displayInternal...) != "" || s.consumeIdent(displayLegacy...) != "" ||
			s.consumeIdent(displayInside...) != "" || s.consumeIdent(displayOutside...) != ""
	}
	var outside, inside, listItem string
	for !s.atEnd() {
		if outside == "" {
			if outside = s.consumeIdent(displayOutside...); outside != "" {
				continue
			}
		}
		if inside == "" {
			if inside = s.consumeIdent(displayInside...); inside != "" {
				continue
			}
		}
		if listItem == "" {
			if listItem = s.consumeIdent("list-item"); listItem != "" {
				continue
			}
		}
		return false
	}
	return listItem == "" || inside == "" || inside == "flow" || inside == "flow-root"
}

// isValidObjectPositionValue validates an object-position declaration —
// ObjectPosition::ParseSingleValue (longhands_custom.cc:7317 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): ConsumePosition with
// UnitlessQuirk::kForbid and no three-value form, consuming the whole value.
// CSS-wide keywords and var()/attr() are accepted as-is.
func isValidObjectPositionValue(value string) bool {
	if isDeferredOrCSSWideValue(value) {
		return true
	}
	s := newComponentStream(value)
	_, _, ok := consumePosition(s, false, false)
	return ok && s.atEnd()
}

// isValidBackgroundSizeValue validates a background-size longhand —
// ParseBackgroundSize (:5290, a comma list of ConsumeBackgroundSize).
// CSS-wide keywords and var()/attr() are accepted as-is.
func isValidBackgroundSizeValue(value string) bool {
	if isDeferredOrCSSWideValue(value) {
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

// Ports of Blink's grid track-list and grid-template consumers in
// core/css/properties/css_parsing_utils.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b: ConsumeGridBreadth (:6894),
// ConsumeFitContent (:6916), IsGridTrackFixedSized (:6960),
// ConsumeGridTrackSize (:6988), ConsumeGridLineNames (:7041),
// ConsumeGridTrackRepeatFunction (:7089),
// ConsumeGridTemplateRowsAndAreasAndColumns (:7198), ConsumeGridTrackList
// (:7379), ParseGridTemplateAreasColumnNames (:6854),
// ParseGridTemplateAreasRow (:7475), ConsumeGridTemplatesRowsOrColumns
// (:7550) and ConsumeGridTemplateShorthand (:7598). As with the background
// consumers, they validate and return the consumed text; the typed values
// are built when the longhands are read.

// gridTrackListType is css_parsing_utils::TrackListType.
type gridTrackListType int

const (
	gridTemplate gridTrackListType = iota
	gridTemplateNoRepeat
	gridTemplateSubgrid
	gridAuto
)

// gridBreadthKind classifies a <track-breadth> for IsGridBreadthFixedSized.
type gridBreadthKind int

const (
	gridBreadthFixed gridBreadthKind = iota
	gridBreadthFlex
	gridBreadthIntrinsic
)

// functionArgs returns the arguments of tok when it is a call of the
// (lower-case) function name, split at top-level commas.
func functionArgs(tok, name string) ([]string, bool) {
	if !isFunction(tok, name) || !strings.HasSuffix(tok, ")") {
		return nil, false
	}
	args := splitCSSFunctionArgs(tok[len(name)+1 : len(tok)-1])
	for i := range args {
		args[i] = strings.TrimSpace(args[i])
	}
	return args, true
}

// consumeGridBreadth is ConsumeGridBreadth: auto, min-content,
// max-content, a non-negative <flex>, or a non-negative
// <length-percentage>.
func consumeGridBreadth(tok string) (gridBreadthKind, bool) {
	lower := strings.ToLower(tok)
	switch lower {
	case "auto", "min-content", "max-content":
		return gridBreadthIntrinsic, true
	}
	if strings.HasSuffix(lower, "fr") {
		if n, err := strconv.ParseFloat(tok[:len(tok)-2], 64); err == nil {
			return gridBreadthFlex, n >= 0
		}
	}
	return gridBreadthFixed, isLengthPercentToken(tok, true, false)
}

// consumeGridTrackSize is ConsumeGridTrackSize: minmax(), fit-content() or
// a <track-breadth>. fixed is IsGridTrackFixedSized.
func consumeGridTrackSize(s *componentStream) (fixed, ok bool) {
	tok := s.peek()
	if args, isMinmax := functionArgs(tok, "minmax"); isMinmax {
		if len(args) != 2 {
			return false, false
		}
		minKind, ok := consumeGridBreadth(args[0])
		if !ok || minKind == gridBreadthFlex {
			return false, false
		}
		maxKind, ok := consumeGridBreadth(args[1])
		if !ok {
			return false, false
		}
		s.consume()
		return minKind == gridBreadthFixed || maxKind == gridBreadthFixed, true
	}
	if args, isFitContent := functionArgs(tok, "fit-content"); isFitContent {
		if len(args) != 1 || !isLengthPercentToken(args[0], true, false) {
			return false, false
		}
		s.consume()
		return false, true
	}
	kind, ok := consumeGridBreadth(tok)
	if !ok {
		return false, false
	}
	s.consume()
	return kind == gridBreadthFixed, true
}

// isFunction reports whether tok is a call of the (lower-case) function
// name.
func isFunction(tok, name string) bool {
	return len(tok) > len(name) && strings.EqualFold(tok[:len(name)+1], name+"(")
}

// isGridLineNames reports whether tok is a bracketed <line-names> token.
func isGridLineNames(tok string) bool { return strings.HasPrefix(tok, "[") }

// consumeGridLineNames is ConsumeGridLineNames (:7041-7076): a bracketed
// list of <custom-ident>s other than auto and span. It reports whether it
// consumed a list of names; an empty list is consumed too, but reported
// only for a subgrid's <line-name-list>.
func consumeGridLineNames(s *componentStream, isSubgrid bool) bool {
	tok := s.peek()
	if !isGridLineNames(tok) || !strings.HasSuffix(tok, "]") {
		return false
	}
	names := strings.Fields(tok[1 : len(tok)-1])
	for _, name := range names {
		if !isIdentToken(name) || strings.EqualFold(name, "auto") || strings.EqualFold(name, "span") {
			return false
		}
	}
	s.consume()
	return isSubgrid || len(names) > 0
}

// consumeGridTrackRepeatFunction is ConsumeGridTrackRepeatFunction.
// allFixed carries ConsumeGridTrackList's
// all_tracks_are_intrinsic_repeat_or_fixed_sized.
func consumeGridTrackRepeatFunction(s *componentStream, isSubgrid bool, allFixed *bool) (isAutoRepeat, ok bool) {
	args, _ := functionArgs(s.peek(), "repeat")
	if len(args) != 2 {
		return false, false
	}
	count := strings.ToLower(args[0])
	if isSubgrid && count == "auto-fit" {
		return false, false
	}
	isAutoRepeat = count == "auto-fill" || count == "auto-fit"
	if !isAutoRepeat {
		if n, err := strconv.Atoi(args[0]); err != nil || n < 1 {
			if !IsMathFunction(args[0]) {
				return false, false
			}
		}
	}
	inner := newComponentStream(args[1])
	lineNameSets, tracks := 0, 0
	if consumeGridLineNames(inner, isSubgrid) {
		lineNameSets++
	}
	for !inner.atEnd() {
		if isSubgrid {
			if lineNameSets == 0 || !consumeGridLineNames(inner, true) {
				return false, false
			}
			lineNameSets++
			continue
		}
		fixed, ok := consumeGridTrackSize(inner)
		if !ok {
			return false, false
		}
		// The CSSGridLanesLayout branch that also allows intrinsic sizes in
		// an auto repeat is behind a runtime flag; louis14 has no grid lanes.
		*allFixed = *allFixed && fixed
		tracks++
		consumeGridLineNames(inner, false)
	}
	if (isSubgrid && lineNameSets == 0) || (!isSubgrid && tracks == 0) {
		return false, false
	}
	s.consume()
	return isAutoRepeat, true
}

// consumeGridTrackList is ConsumeGridTrackList, without the grid-lanes
// shorthand's look-ahead: the tokens up to the end of the stream or a
// delimiter.
func consumeGridTrackList(s *componentStream, listType gridTrackListType) bool {
	allowLineNames := listType != gridAuto
	if !allowLineNames && isGridLineNames(s.peek()) {
		return false
	}
	isSubgrid := listType == gridTemplateSubgrid
	if isSubgrid && s.consumeIdent("subgrid") == "" {
		return false
	}
	consumeGridLineNames(s, isSubgrid)
	allowRepeat := isSubgrid || listType == gridTemplate
	atRangeEnd := func() bool { return s.atEnd() || s.peek() == "/" || s.peek() == "," }
	seenAutoRepeat, allFixed := false, true
	for {
		if isFunction(s.peek(), "repeat") {
			if !allowRepeat {
				return false
			}
			isAutoRepeat, ok := consumeGridTrackRepeatFunction(s, isSubgrid, &allFixed)
			if !ok || (isAutoRepeat && seenAutoRepeat) {
				return false
			}
			seenAutoRepeat = seenAutoRepeat || isAutoRepeat
		} else if fixed, ok := consumeGridTrackSize(s); ok {
			if isSubgrid {
				return false
			}
			allFixed = allFixed && fixed
		} else if !isSubgrid {
			return false
		}
		if seenAutoRepeat && !allFixed {
			return false
		}
		if !allowLineNames && isGridLineNames(s.peek()) {
			return false
		}
		appended := consumeGridLineNames(s, isSubgrid)
		if isSubgrid && !appended {
			if !isFunction(s.peek(), "repeat") {
				return atRangeEnd()
			}
		}
		if atRangeEnd() {
			return true
		}
	}
}

// consumeGridTemplatesRowsOrColumns is ConsumeGridTemplatesRowsOrColumns:
// none, a subgrid <line-name-list>, or a <track-list>.
func consumeGridTemplatesRowsOrColumns(s *componentStream) bool {
	switch s.peekIdent() {
	case "none":
		s.consume()
		return true
	case "subgrid":
		return consumeGridTrackList(s, gridTemplateSubgrid)
	}
	return consumeGridTrackList(s, gridTemplate)
}

// parseGridTemplateAreasColumnNames is ParseGridTemplateAreasColumnNames:
// the cell names of one area string, a run of '.' being one null cell. nil
// when a character is neither a name code point, '.', nor whitespace.
func parseGridTemplateAreasColumnNames(row string) []string {
	var names []string
	start := -1 // start of the name being read, or -1
	flush := func(end int) {
		if start >= 0 {
			if row[start] == '.' {
				names = append(names, ".") // a run of '.' is one null cell
			} else {
				names = append(names, row[start:end])
			}
			start = -1
		}
	}
	inDots := func() bool { return start >= 0 && row[start] == '.' }
	for i := 0; i < len(row); i++ {
		c := row[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			flush(i)
			continue
		case c == '.':
			if inDots() {
				continue
			}
			flush(i)
		case c >= 0x80 || isIdentContinue(c):
			if inDots() {
				flush(i)
			}
		default:
			return nil
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(row))
	return names
}

// gridAreaSpan is a named area's GridArea: rows [rowStart, rowEnd) and
// columns [colStart, colEnd).
type gridAreaSpan struct{ rowStart, rowEnd, colStart, colEnd int }

// parseGridTemplateAreasRow is ParseGridTemplateAreasRow: it adds row (the
// unquoted text of the rowCount'th string) to areas, and reports whether
// every row has columnCount cells and every named area stays one filled-in
// rectangle.
func parseGridTemplateAreasRow(row string, areas map[string]*gridAreaSpan, rowCount int, columnCount *int) bool {
	if strings.TrimSpace(row) == "" {
		return false
	}
	names := parseGridTemplateAreasColumnNames(row)
	if rowCount == 0 {
		*columnCount = len(names)
		if *columnCount == 0 {
			return false
		}
	} else if *columnCount != len(names) {
		return false
	}
	for col := 0; col < len(names); col++ {
		name := names[col]
		if name == "." {
			continue
		}
		end := col + 1
		for end < len(names) && names[end] == name {
			end++
		}
		if area, ok := areas[name]; !ok {
			areas[name] = &gridAreaSpan{rowCount, rowCount + 1, col, end}
		} else {
			if rowCount != area.rowEnd || col != area.colStart || end != area.colEnd {
				return false
			}
			area.rowEnd++
		}
		col = end - 1
	}
	return true
}

// parseGridTemplateAreas reads <string>+ as grid-template-areas: the named
// areas, if every string is a row parseGridTemplateAreasRow accepts.
func parseGridTemplateAreas(rows []string) (map[string]*gridAreaSpan, bool) {
	areas := map[string]*gridAreaSpan{}
	columns := 0
	for i, row := range rows {
		if !isQuotedStringValue(row) || !parseGridTemplateAreasRow(unquoteCSSString(row), areas, i, &columns) {
			return nil, false
		}
	}
	return areas, len(rows) > 0
}

// consumeGridTemplateRowsAndAreasAndColumns is
// ConsumeGridTemplateRowsAndAreasAndColumns: [ <line-names>? <string>
// <track-size>? <line-names>? ]+ [ / <track-list> ]?, giving the rows'
// track list (a row with no size is auto, and a row's leading line names
// merge into the previous row's trailing ones), the area strings and the
// columns.
func consumeGridTemplateRowsAndAreasAndColumns(s *componentStream) (rows, columns, areas string, ok bool) {
	var rowList, areaList []string
	gridAreas := map[string]*gridAreaSpan{}
	columnCount := 0
	for {
		if p := s.save(); consumeGridLineNames(s, false) {
			names := s.since(p)
			if n := len(rowList); n > 0 && isGridLineNames(rowList[n-1]) {
				rowList[n-1] = strings.TrimSuffix(rowList[n-1], "]") + " " + strings.TrimPrefix(names, "[")
			} else {
				rowList = append(rowList, names)
			}
		}
		row := s.peek()
		if !isQuotedStringValue(row) || !parseGridTemplateAreasRow(unquoteCSSString(row), gridAreas, len(areaList), &columnCount) {
			return "", "", "", false
		}
		s.consume()
		areaList = append(areaList, row)
		size := "auto"
		p := s.save()
		if _, ok := consumeGridTrackSize(s); ok {
			size = s.since(p)
		}
		rowList = append(rowList, size)
		if p := s.save(); consumeGridLineNames(s, false) {
			rowList = append(rowList, s.since(p))
		}
		if s.atEnd() || s.peek() == "/" {
			break
		}
	}
	columns = "none"
	if s.consumeSlash() {
		p := s.save()
		if !consumeGridTrackList(s, gridTemplateNoRepeat) || !s.atEnd() {
			return "", "", "", false
		}
		columns = s.since(p)
	}
	return strings.Join(rowList, " "), columns, strings.Join(areaList, " "), true
}

// consumeGridTemplateShorthand is ConsumeGridTemplateShorthand: the
// grid-template-rows, grid-template-columns and grid-template-areas values
// of a <'grid-template'> at the start of s, tried as <rows> / <columns>,
// then as area strings, then as none.
func consumeGridTemplateShorthand(s *componentStream) (rows, columns, areas string, ok bool) {
	start := s.save()
	if consumeGridTemplatesRowsOrColumns(s) {
		rows = s.since(start)
		if s.consumeSlash() {
			p := s.save()
			if consumeGridTemplatesRowsOrColumns(s) {
				return rows, s.since(p), "none", true
			}
		}
	}
	s.restore(start)
	if rows, columns, areas, ok := consumeGridTemplateRowsAndAreasAndColumns(s); ok {
		return rows, columns, areas, true
	}
	s.restore(start)
	if s.consumeIdent("none") != "" {
		return "none", "none", "none", true
	}
	return "", "", "", false
}
