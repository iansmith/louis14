package css

import "testing"

func TestStripCSSComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic comment between rules",
			input:    "body { color: red; } /* comment */ p { color: blue; }",
			expected: "body { color: red; }  p { color: blue; }",
		},
		{
			name:     "comment inside declaration block",
			input:    "body { /* comment */ color: red; }",
			expected: "body {  color: red; }",
		},
		{
			name:     "comment inside selector area",
			input:    "body /* comment */ { color: red; }",
			expected: "body  { color: red; }",
		},
		{
			name:     "unterminated comment fully stripped",
			input:    "body { color: red; } /* unterminated",
			expected: "body { color: red; } ",
		},
		{
			name:     "nested-looking comment ends at first close",
			input:    "/* outer /* inner */ still-outside */",
			expected: " still-outside */",
		},
		{
			name:     "comment containing CSS-like content",
			input:    "/* body { color: red; } */",
			expected: "",
		},
		{
			name:     "multiple comments",
			input:    "/* c1 */ body { color: red; } /* c2 */ p { color: blue; }",
			expected: " body { color: red; }  p { color: blue; }",
		},
		{
			name:     "empty comment",
			input:    "/**/",
			expected: "",
		},
		{
			name:     "comment with stars",
			input:    "/*** comment ***/",
			expected: "",
		},
		{
			name:     "no comments",
			input:    "body { color: red; }",
			expected: "body { color: red; }",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCSSComments(tt.input)
			if got != tt.expected {
				t.Errorf("stripCSSComments(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expected)
			}
		})
	}
}
