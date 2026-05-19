package layout

import (
	"strings"
	"testing"
)

func TestResolveRelativeURLsInCSS_BasicQuotingVariants(t *testing.T) {
	// ResolveRelativeURLsInCSS rewrites relative url() references against
	// the supplied base across the three quoting variants. Absolute URLs
	// (`/`, `data:`, `http(s):`) are left untouched.
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"double-quote", `filter: url("foo.svg#x")`, `filter: url("support/foo.svg#x")`},
		{"single-quote", `filter: url('foo.svg#x')`, `filter: url('support/foo.svg#x')`},
		{"no-quote", `filter: url(foo.svg#x)`, `filter: url(support/foo.svg#x)`},
		{"absolute-untouched", `background-image: url(/abs/path.png)`, `background-image: url(/abs/path.png)`},
		{"data-untouched", `filter: url("data:image/svg+xml,...")`, `filter: url("data:image/svg+xml,...")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveRelativeURLsInCSS(tc.raw, "support")
			if got != tc.want {
				t.Errorf("got  = %q\nwant = %q", got, tc.want)
			}
		})
	}
}

func TestResolveRelativeURLsInHTML_StyleAttributePassthrough(t *testing.T) {
	// Per LOU-137 v2: inline `style=""` attribute URLs are NOT rewritten
	// — they flow through parse-time resolution via Style.BaseDir.
	// Only <style>...</style> blocks are still pre-baked.
	in := `<div style="filter: url(foo.svg#x)"></div>` +
		`<style>div { background-image: url(bar.png); }</style>`
	got := ResolveRelativeURLsInHTML(in, "support")
	wantStyleAttrUntouched := `<div style="filter: url(foo.svg#x)"></div>`
	if !strings.Contains(got, wantStyleAttrUntouched) {
		t.Errorf("inline style attribute was rewritten (must be left for parse-time resolution):\n  got = %q", got)
	}
	wantStyleBlockRewritten := `background-image: url(support/bar.png)`
	if !strings.Contains(got, wantStyleBlockRewritten) {
		t.Errorf("<style>-block URL was NOT rewritten:\n  got = %q\n  want substring %q", got, wantStyleBlockRewritten)
	}
}
