package css

import "testing"

// TestSVG_ParseURLReference verifies the Phase 6 shared url(#id)
// parser covers every accepted CSS form and rejects the malformed
// variants per the CSS Image Values L4 §3.1 grammar.
func TestSVG_ParseURLReference(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		// Accepted forms.
		{"bare hash id", "url(#id)", "id", true},
		{"double-quoted", `url("#id")`, "id", true},
		{"single-quoted", "url('#id')", "id", true},
		{"whitespace around inner", "url(  #id  )", "id", true},
		{"id with hyphens/underscores/dots", "url(#id-with-hyphens-and_underscores.dots)", "id-with-hyphens-and_underscores.dots", true},
		{"path then fragment (path dropped)", "url(foo.svg#id)", "id", true},
		{"comma-separated takes first", "url(#first), url(#second)", "first", true},
		{"leading whitespace", "  url(#id)", "id", true},
		// Rejected forms.
		{"empty", "", "", false},
		{"none keyword", "none", "", false},
		{"empty url()", "url()", "", false},
		{"http URL no fragment", "url(http://example.com/x.svg)", "", false},
		{"id-only (no hash)", "url(without-hash)", "", false},
		{"hash with empty id", "url(#)", "", false},
		{"not a url() at all", "linear-gradient(red, blue)", "", false},
		{"missing closing paren", "url(#id", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseURLReference(c.input)
			if ok != c.ok {
				t.Fatalf("ParseURLReference(%q): ok=%v, want %v", c.input, ok, c.ok)
			}
			if got != c.want {
				t.Errorf("ParseURLReference(%q): got=%q, want=%q", c.input, got, c.want)
			}
		})
	}
}
