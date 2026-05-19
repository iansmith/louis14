package url

import "testing"

// TestCompleteURL covers the four resolution regimes:
//   - empty / "." base: passthrough
//   - relative base + relative ref: path.Join (filesystem-WPT fixtures)
//   - absolute base + relative ref: net/url.ResolveReference (RFC 3986)
//   - absolute ref + any base: passthrough
func TestCompleteURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		base string
		want string
	}{
		// Passthrough cases.
		{"empty-raw", "", "support", ""},
		{"empty-base", "foo.svg", "", "foo.svg"},
		{"dot-base", "foo.svg", ".", "foo.svg"},

		// Filesystem-relative base + relative ref (today's WPT semantics).
		{"rel-base-rel-ref", "foo.svg", "support", "support/foo.svg"},
		{"rel-base-parent-ref", "../foo.svg", "support/sub", "support/foo.svg"},
		{"rel-base-deep-ref", "a/b.svg", "support", "support/a/b.svg"},

		// Absolute http base + relative ref (the case that broke under path.Join).
		{
			"https-base-rel-ref",
			"foo.svg",
			"https://example.com/styles/",
			"https://example.com/styles/foo.svg",
		},
		{
			"https-base-parent-ref",
			"../foo.svg",
			"https://example.com/styles/sub/",
			"https://example.com/styles/foo.svg",
		},
		{
			"http-base-deep-ref",
			"a/b.svg",
			"http://example.com/styles/",
			"http://example.com/styles/a/b.svg",
		},

		// Absolute ref short-circuits regardless of base.
		{
			"scheme-ref-passthrough",
			"https://other.example.com/x.svg",
			"https://example.com/styles/",
			"https://other.example.com/x.svg",
		},
		{
			"data-ref-passthrough",
			"data:image/svg+xml,<svg/>",
			"support",
			"data:image/svg+xml,<svg/>",
		},
		{
			"root-relative-ref-passthrough",
			"/abs/foo.svg",
			"support",
			"/abs/foo.svg",
		},

		// Scheme-relative ref + absolute base inherits scheme.
		{
			"scheme-relative-ref",
			"//cdn.example.com/x.svg",
			"https://example.com/styles/",
			"//cdn.example.com/x.svg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CompleteURL(tc.raw, tc.base)
			if got != tc.want {
				t.Errorf("CompleteURL(%q, %q):\n  got  = %q\n  want = %q",
					tc.raw, tc.base, got, tc.want)
			}
		})
	}
}

// TestCompleteURL_PathJoinMangleAvoided pins the central correctness target:
// an absolute scheme-prefixed base must NOT collapse to a single slash.
// path.Join("https://example.com/styles", "foo.svg") returns
// "https:/example.com/styles/foo.svg" (mangled); CompleteURL must not.
func TestCompleteURL_PathJoinMangleAvoided(t *testing.T) {
	got := CompleteURL("foo.svg", "https://example.com/styles/")
	mangled := "https:/example.com/styles/foo.svg"
	if got == mangled {
		t.Errorf("CompleteURL collapsed scheme '//' to '/': got %q", got)
	}
	want := "https://example.com/styles/foo.svg"
	if got != want {
		t.Errorf("CompleteURL: got %q, want %q", got, want)
	}
}

// TestURLDir pins the directory-portion semantics for both relative and
// absolute URLs. Trailing-slash convention differs: relative paths follow
// path.Dir (no trailing slash); absolute URLs keep the trailing slash so
// the result feeds back into CompleteURL correctly.
func TestURLDir(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		{"", ""},
		{"foo.css", "."},
		{"support/main.css", "support"},
		{"support/sub/main.css", "support/sub"},
		{"https://example.com/styles/main.css", "https://example.com/styles/"},
		{"https://example.com/main.css", "https://example.com/"},
		{"http://example.com/x/y/z.css", "http://example.com/x/y/"},
		{"/styles/main.css", "/styles/"},
		{"/main.css", "/"},
		{"//cdn.example.com/x/main.css", "//cdn.example.com/x/"},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			got := URLDir(tc.uri)
			if got != tc.want {
				t.Errorf("URLDir(%q): got %q, want %q", tc.uri, got, tc.want)
			}
		})
	}
}

// TestURLDirRoundTrip verifies that URLDir followed by CompleteURL with a
// relative ref reconstructs the original directory + new filename. This is
// the central composition contract: fetcher code does
// `CompleteURL(uri, URLDir(parentURI))`.
func TestURLDirRoundTrip(t *testing.T) {
	cases := []struct {
		parentURI string
		newFile   string
		want      string
	}{
		{"support/inner.html", "hueRotate.svg", "support/hueRotate.svg"},
		{
			"https://example.com/styles/main.css",
			"hueRotate.svg",
			"https://example.com/styles/hueRotate.svg",
		},
		{
			"/styles/main.css",
			"hueRotate.svg",
			"/styles/hueRotate.svg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.parentURI, func(t *testing.T) {
			dir := URLDir(tc.parentURI)
			got := CompleteURL(tc.newFile, dir)
			if got != tc.want {
				t.Errorf("CompleteURL(%q, URLDir(%q)=%q):\n  got  = %q\n  want = %q",
					tc.newFile, tc.parentURI, dir, got, tc.want)
			}
		})
	}
}
