package css

import (
	"slices"
	"strings"
)

// serializeShorthand serializes property, a shorthand, from its longhands'
// declarations in the block, as Blink's StylePropertySerializer::
// SerializeShorthand does (core/css/style_property_serializer.cc:591 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A longhand set to its initial
// value by the shorthand (stored as `initial`, implicit in the shorthand) is
// left out of the serialization.
func (d *DeclarationResult) serializeShorthand(property string) string {
	values, serialized, ok := d.commonShorthandChecks(property)
	switch {
	case !ok:
		return ""
	case serialized != "":
		return serialized
	case property == "border":
		return d.borderValue()
	case property == "font":
		return d.fontValue()
	case property == "grid":
		return gridValue(values)
	case property == "grid-template":
		return gridTemplateValue(values[0], values[1], values[2])
	case isBorderSideShorthand[property]:
		return serializeBorderValues(values)
	case isFourValueShorthand[property]:
		return get4Values(values)
	case isTwoValueShorthand[property]:
		return get2Values(values)
	}
	return getShorthandValue(values, " ")
}

// commonShorthandChecks is CommonShorthandChecks (:522-588): every longhand
// declared, with one importance, and no CSS-wide keyword other than an
// implicit initial unless they are all the same keyword, and no pending
// substitution unless they are all pending from one value of property. It
// returns the longhands' values in longhand order, and serialized, the
// shorthand's text when the checks decide it: the common keyword, or the
// pending shorthand value.
func (d *DeclarationResult) commonShorthandChecks(property string) (values []string, serialized string, ok bool) {
	longhands := shorthandLonghands[property]
	values = make([]string, len(longhands))
	important := 0
	for i, lh := range longhands {
		v, ok := d.Declarations[storageKey(lh)]
		if !ok {
			return nil, "", false
		}
		if d.Important[lh] {
			important++
		}
		values[i] = v
	}
	if important != 0 && important != len(longhands) {
		return nil, "", false
	}
	if kw, ok := allSameCSSWideKeyword(values); ok {
		return values, kw, true
	}
	// Longhands all pending from one value of this shorthand serialize as
	// that value (:550-570).
	if shorthand, value, pending := parsePendingSubstitution(values[0]); pending && shorthand == property &&
		!slices.ContainsFunc(values, func(v string) bool { return v != values[0] }) {
		return values, value, true
	}
	for _, v := range values {
		_, _, pending := parsePendingSubstitution(v)
		if kw, wide := cssWideKeyword(v); (wide && kw != "initial") || pending || containsVarFunction(v) {
			return nil, "", false
		}
	}
	return values, "", true
}

// allSameCSSWideKeyword reports the CSS-wide keyword every value is, when
// they are all the same one.
func allSameCSSWideKeyword(values []string) (string, bool) {
	kw, wide := cssWideKeyword(values[0])
	if !wide {
		return "", false
	}
	for _, v := range values[1:] {
		if other, _ := cssWideKeyword(v); other != kw {
			return "", false
		}
	}
	return kw, true
}

// isImplicitInitial reports whether a longhand value is the initial value a
// shorthand set implicitly (`initial` only; isInitialValueKeyword also takes
// `unset`).
func isImplicitInitial(v string) bool {
	kw, _ := cssWideKeyword(v)
	return kw == "initial"
}

// The shorthands SerializeShorthand serializes with Get4Values, Get2Values
// and GetShorthandValueForBorder (:600-760).
var (
	isFourValueShorthand = map[string]bool{
		"margin": true, "padding": true, "inset": true,
		"border-width": true, "border-style": true, "border-color": true,
	}
	isTwoValueShorthand = map[string]bool{
		"gap": true, "inset-block": true, "inset-inline": true,
		"margin-block": true, "margin-inline": true, "padding-block": true,
		"padding-inline": true, "place-content": true, "place-items": true,
		"place-self": true,
	}
	isBorderSideShorthand = map[string]bool{
		"border-top": true, "border-right": true, "border-bottom": true, "border-left": true,
	}
)

// get2Values is Get2Values (:1778-1793): start, then end when it differs.
func get2Values(v []string) string {
	if v[0] == v[1] {
		return v[0]
	}
	return v[0] + " " + v[1]
}

// get4Values is Get4Values (:1795-1825): top, then right, bottom and left
// as far as they are needed.
func get4Values(v []string) string {
	showLeft := v[1] != v[3]
	showBottom := v[0] != v[2] || showLeft
	showRight := v[0] != v[1] || showBottom
	out := []string{v[0]}
	if showRight {
		out = append(out, v[1])
	}
	if showBottom {
		out = append(out, v[2])
	}
	if showLeft {
		out = append(out, v[3])
	}
	return strings.Join(out, " ")
}

// getShorthandValue is GetShorthandValue (:2192-2208): the longhands' values
// that are not initial, joined by sep.
func getShorthandValue(values []string, sep string) string {
	var out []string
	for _, v := range values {
		if !isImplicitInitial(v) {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}

// serializeBorderValues is SerializeBorderValues (:97-115): width, style
// and colour, each left out when initial.
func serializeBorderValues(values []string) string {
	initial := [3]string{"medium", "none", "currentcolor"}
	var out []string
	for i, v := range values {
		if !isImplicitInitial(v) && !strings.EqualFold(v, initial[i]) {
			out = append(out, v)
		}
	}
	return strings.Join(out, " ")
}

// borderValue is BorderPropertyValue (:3095-3130): the border-image
// longhands initial, and border-width, border-style and border-color each
// one value on every side, serialized as one side.
func (d *DeclarationResult) borderValue() string {
	for _, lh := range shorthandLonghands["border-image"] {
		if !isImplicitInitial(d.Declarations[lh]) {
			return ""
		}
	}
	var side [3]string
	for i, part := range []string{"border-width", "border-style", "border-color"} {
		longhands := shorthandLonghands[part]
		side[i] = d.Declarations[longhands[0]]
		for _, lh := range longhands[1:] {
			if d.Declarations[lh] != side[i] {
				return ""
			}
		}
	}
	return serializeBorderValues(side[:])
}

// fontSerialized are the font longhands louis14's font shorthand sets, in
// FontValue's order (:1337-1560); fontSizeIndex is font-size's position.
var fontSerialized = []string{"font-style", "font-variant-caps", "font-weight", "font-stretch", "font-size", "line-height", "font-family"}

const fontSizeIndex = 4

// fontValue is FontValue: style, variant-caps, weight and stretch when not
// normal, then size, a line-height when not normal, and family. Another font
// longhand not at its initial value cannot be expressed: "".
func (d *DeclarationResult) fontValue() string {
	normal := func(v string) bool { return isImplicitInitial(v) || strings.EqualFold(v, "normal") }
	for _, lh := range shorthandLonghands["font"] {
		if !normal(d.Declarations[lh]) && !slices.Contains(fontSerialized, lh) {
			return ""
		}
	}
	var out []string
	for _, lh := range fontSerialized[:fontSizeIndex] {
		if v := d.Declarations[lh]; !normal(v) {
			out = append(out, v)
		}
	}
	size := d.Declarations["font-size"]
	if lh := d.Declarations["line-height"]; !normal(lh) {
		size += "/" + lh
	}
	return strings.Join(append(out, size, d.Declarations["font-family"]), " ")
}

// isNoneValue is CSSOMUtils::IsNoneValue (cssom_utils.cc:50): none, which a
// template longhand also is when a shorthand left it implicit.
func isNoneValue(v string) bool { return isImplicitInitial(v) || strings.EqualFold(v, "none") }

// gridTemplateValue is CSSOMUtils::ComputedValueForGridTemplateShorthand
// (cssom_utils.cc:139-226), which GetShorthandValueForGridTemplate
// (style_property_serializer.cc:3063) serializes: none; <rows> / <columns>;
// or the area strings, each followed by its row's size unless auto, with
// the line names between them, then / <columns> unless none. "" when the
// longhands match none of these.
func gridTemplateValue(rows, columns, areas string) string {
	rowsNone, columnsNone, areasNone := isNoneValue(rows), isNoneValue(columns), isNoneValue(areas)
	switch {
	case rowsNone && columnsNone && areasNone:
		return "none"
	case rowsNone && !areasNone:
		// grid-template-areas needs grid-template-rows.
		return ""
	case areasNone:
		return rows + " / " + columns
	}
	rowToks, columnToks := splitComponentValues(rows), splitComponentValues(columns)
	isRepeat := func(t string) bool { return isFunction(t, "repeat") }
	if slices.ContainsFunc(rowToks, isRepeat) || slices.ContainsFunc(columnToks, isRepeat) {
		return ""
	}
	areaRows := splitComponentValues(areas)
	var out []string
	row := 0
	for _, t := range rowToks {
		if isGridLineNames(t) {
			out = append(out, t)
			continue
		}
		if row == len(areaRows) {
			return ""
		}
		// SerializeGridAreaText (cssom_utils.cc:104): the row's cells
		// joined by single spaces.
		cells := parseGridTemplateAreasColumnNames(unquoteCSSString(areaRows[row]))
		out = append(out, `"`+strings.Join(cells, " ")+`"`)
		row++
		if !strings.EqualFold(t, "auto") {
			out = append(out, t)
		}
	}
	if row != len(areaRows) {
		return ""
	}
	if !columnsNone {
		out = append(out, "/", columns)
	}
	return strings.Join(out, " ")
}

// gridValue is StylePropertySerializer::GetShorthandValueForGrid
// (style_property_serializer.cc:2822-2956), values in shorthandLonghands
// order. An auto longhand stored as initial is one the grid shorthand set
// implicitly; Blink tells it apart from a specified value by its being an
// identifier rather than a value list.
func gridValue(values []string) string {
	rows, columns, areas, flow, autoRows, autoColumns := values[0], values[1], values[2], values[3], values[4], values[5]
	flowSpecified, autoRowsSpecified, autoColumnsSpecified := !isImplicitInitial(flow), !isImplicitInitial(autoRows), !isImplicitInitial(autoColumns)
	nonInitialAutoRows := autoRowsSpecified && !strings.EqualFold(autoRows, "auto")
	nonInitialAutoColumns := autoColumnsSpecified && !strings.EqualFold(autoColumns, "auto")
	flowInitial := strings.EqualFold(flow, "row")

	// grid-auto-* with named areas is not valid per the grammar.
	if (flowSpecified || autoRowsSpecified || autoColumnsSpecified) && !isNoneValue(areas) {
		return ""
	}
	nonInitialRows, nonInitialColumns := !isNoneValue(rows), !isNoneValue(columns)
	// grid-template-* and grid-auto-* are mutually exclusive per direction.
	if (nonInitialRows && nonInitialAutoRows) || (nonInitialColumns && nonInitialAutoColumns) ||
		(nonInitialAutoRows && nonInitialAutoColumns) {
		return ""
	}
	if !flowSpecified || (flowInitial && !nonInitialAutoColumns && !nonInitialAutoRows) {
		return gridTemplateValue(rows, columns, areas)
	}
	if nonInitialRows && nonInitialColumns {
		return ""
	}
	flowWords := strings.Fields(strings.ToLower(flow))
	autoFlow := "auto-flow"
	if slices.Contains(flowWords, "dense") {
		autoFlow += " dense"
	}
	if slices.Contains(flowWords, "column") {
		out := rows + " / " + autoFlow
		if nonInitialAutoColumns {
			out += " " + autoColumns
		}
		return out
	}
	out := autoFlow
	if nonInitialAutoRows {
		out += " " + autoRows
	}
	return out + " / " + columns
}
