package css

import "testing"

// TestKeywordPropertyValues checks keyword properties drop a value outside
// their keyword list, as CSSParserFastPaths::IsValidKeywordPropertyAndValue
// does (core/css/parser/css_parser_fast_paths.cc:1142; caret-shape at
// :1191-1194 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so an earlier
// valid declaration stays (LOU-906).
func TestKeywordPropertyValues(t *testing.T) {
	for _, tt := range []struct{ decl, prop, want string }{
		{"caret-shape: block", "caret-shape", "block"},
		{"caret-shape: BAR", "caret-shape", "bar"},
		{"caret-shape: underscore", "caret-shape", "underscore"},
		{"caret-shape: auto", "caret-shape", "auto"},
		{"caret-shape: block; caret-shape: foo", "caret-shape", "block"},
		{"caret-shape: block; caret-shape: 10px", "caret-shape", "block"},
		{"caret-shape: block; caret-shape: bar block", "caret-shape", "block"},
		{"caret-shape: block; caret-shape: inherit", "caret-shape", "inherit"},
		{"caret-shape: block; caret-shape: var(--s)", "caret-shape", "var(--s)"},
	} {
		if got := parseDeclarations(tt.decl).Declarations[tt.prop]; got != tt.want {
			t.Errorf("%s: %s = %q, want %q", tt.decl, tt.prop, got, tt.want)
		}
	}
}
