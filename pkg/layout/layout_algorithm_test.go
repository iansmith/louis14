package layout

import (
	"testing"

	"louis14/pkg/html"
)

func TestResolveRelativeURLsInCSS_StripBaseURLsInCSS_RoundTrip(t *testing.T) {
	// Verify stripBaseURLsInCSS is the inverse of ResolveRelativeURLsInCSS
	// for the three quoting variants and a relative URL. Absolute and
	// data:/http: URLs are unaffected in either direction.
	cases := []struct {
		name string
		raw  string
	}{
		{"double-quote", `filter: url("support/foo.svg#x")`},
		{"single-quote", `filter: url('support/foo.svg#x')`},
		{"no-quote", `filter: url(support/foo.svg#x)`},
		{"multiple", `background-image: url(a/x.png); filter: url("a/y.svg#f")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rewritten := ResolveRelativeURLsInCSS(tc.raw, "support")
			roundTrip := stripBaseURLsInCSS(rewritten, "support")
			if roundTrip != tc.raw {
				t.Errorf("roundtrip failed:\n  raw      = %q\n  rewritten= %q\n  back     = %q", tc.raw, rewritten, roundTrip)
			}
		})
	}
}

func TestStripBaseURLsInCSS_AbsoluteUntouched(t *testing.T) {
	// Absolute URLs are not pre-baked, so the strip pass must leave them alone.
	for _, raw := range []string{
		`background-image: url(/abs/path.png)`,
		`filter: url("data:image/svg+xml,...")`,
		`filter: url(http://example.com/x.svg)`,
	} {
		got := stripBaseURLsInCSS(raw, "support")
		if got != raw {
			t.Errorf("stripBaseURLsInCSS modified absolute URL\n  in  = %q\n  out = %q", raw, got)
		}
	}
}

func TestAdoptNodeFromIframe_RestoresInlineStyleURLs(t *testing.T) {
	// Construct the post-pre-bake state: a div whose inline style was
	// rewritten by ResolveRelativeURLsInHTML against iframe base "support".
	// AdoptNodeFromIframe should restore the original relative URL and
	// clear IframeBase so further adopt calls are no-ops.
	div := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"style": `filter: url(support/support/hueRotate.svg#MyFilter); background-image: url(support/bg.png)`,
		},
		IframeBase: "support",
	}
	// Add a child with its own inline style + IframeBase to verify the
	// recursive walk.
	child := &html.Node{
		Type:    html.ElementNode,
		TagName: "img",
		Attributes: map[string]string{
			"style": `filter: url(support/support/blur.svg#b)`,
		},
		IframeBase: "support",
	}
	div.Children = []*html.Node{child}

	AdoptNodeFromIframe(div)

	wantDivStyle := `filter: url(support/hueRotate.svg#MyFilter); background-image: url(bg.png)`
	if got := div.Attributes["style"]; got != wantDivStyle {
		t.Errorf("div.style after adopt:\n  got  = %q\n  want = %q", got, wantDivStyle)
	}
	if div.IframeBase != "" {
		t.Errorf("div.IframeBase not cleared: %q", div.IframeBase)
	}
	wantChildStyle := `filter: url(support/blur.svg#b)`
	if got := child.Attributes["style"]; got != wantChildStyle {
		t.Errorf("child.style after adopt:\n  got  = %q\n  want = %q", got, wantChildStyle)
	}
	if child.IframeBase != "" {
		t.Errorf("child.IframeBase not cleared: %q", child.IframeBase)
	}
}

func TestAdoptNodeFromIframe_NoOpWithoutIframeBase(t *testing.T) {
	// A node with IframeBase = "" is treated as already-adopted; the walk
	// must not modify anything.
	n := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"style": `filter: url(support/foo.svg#x)`,
		},
	}
	orig := n.Attributes["style"]
	AdoptNodeFromIframe(n)
	if n.Attributes["style"] != orig {
		t.Errorf("AdoptNodeFromIframe modified node without IframeBase: %q", n.Attributes["style"])
	}
}
