package css

import "testing"

// ParseGridLine follows ConsumeGridLine (core/css/properties/
// css_parsing_utils.cc:7282-7374) and StyleBuilderConverter::
// ConvertGridPosition's clamps (core/css/resolver/
// style_builder_converter.cc:1586-1591) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b: span comes first or last, and
// integers clamp to kGridMaxTracks.
func TestParseGridLineGrammarAndClamp(t *testing.T) {
	for _, tc := range []struct {
		v    string
		ok   bool
		want GridLine
	}{
		{"1 span foo", false, GridLine{}},
		{"foo span 2", false, GridLine{}},
		{"1 foo span", true, GridLine{Span: true, Integer: 1, Name: "foo"}},
		{"span 2 foo", true, GridLine{Span: true, Integer: 2, Name: "foo"}},
		{"span 20000000", true, GridLine{Span: true, Integer: GridMaxTracks}},
		{"-20000000", true, GridLine{Integer: -GridMaxTracks}},
		{"20000000 foo", true, GridLine{Integer: GridMaxTracks, Name: "foo"}},
		// An integer too large for an int still parses, and clamps.
		{"99999999999999999999", true, GridLine{Integer: GridMaxTracks}},
		{"span 99999999999999999999", true, GridLine{Span: true, Integer: GridMaxTracks}},
		{"-99999999999999999999 foo", true, GridLine{Integer: -GridMaxTracks, Name: "foo"}},
	} {
		got, ok := ParseGridLine(tc.v)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("ParseGridLine(%q) = %+v, %v; want %+v, %v", tc.v, got, ok, tc.want, tc.ok)
		}
	}
}
