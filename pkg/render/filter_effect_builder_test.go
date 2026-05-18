package render

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"louis14/pkg/css"
)

// TestBuildReferenceFilter_ExternalSVG_DataURL feeds a data: URL
// pointing at a one-effect inline SVG; asserts the resulting
// *filters.Filter is non-nil and has both Source and LastEffect set
// (the SVGFilterBuilder always populates them for a successful
// build).
func TestBuildReferenceFilter_ExternalSVG_DataURL(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f"><feFlood flood-color="green"/></filter></svg>`
	dataURL := "data:image/svg+xml;utf8," + svg + "#f"

	b := &FilterEffectBuilder{
		ReferenceBox: image.Rect(0, 0, 100, 100),
		ExternalSVGFetcher: func(uri string) ([]byte, error) {
			// The fetcher receives docURL (no fragment).
			return []byte(svg), nil
		},
	}
	f := b.BuildReferenceFilter(dataURL)
	if f == nil {
		t.Fatal("BuildReferenceFilter returned nil for valid data URL")
	}
	if f.Source == nil || f.LastEffect == nil {
		t.Errorf("Filter is missing builtins: Source=%v LastEffect=%v", f.Source, f.LastEffect)
	}
}

// TestBuildReferenceFilter_ExternalSVG_FileURL writes a synthetic SVG
// to a temp dir, references it via file://, and verifies the filter
// builds.
func TestBuildReferenceFilter_ExternalSVG_FileURL(t *testing.T) {
	dir := t.TempDir()
	const svg = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f"><feFlood flood-color="red"/></filter></svg>`
	p := filepath.Join(dir, "x.svg")
	if err := os.WriteFile(p, []byte(svg), 0644); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + p + "#f"

	b := &FilterEffectBuilder{
		ReferenceBox: image.Rect(0, 0, 50, 50),
		ExternalSVGFetcher: func(uri string) ([]byte, error) {
			// In the file:// case the docURL is file://path (no
			// fragment), and the test harness reads it via os.ReadFile.
			return os.ReadFile(p)
		},
	}
	f := b.BuildReferenceFilter(uri)
	if f == nil {
		t.Fatal("BuildReferenceFilter returned nil for valid file URL")
	}
}

// TestBuildReferenceFilter_ExternalSVG_FetcherError returns an error
// from the fetcher; expect a nil filter (caller falls back to
// unfiltered paint).
func TestBuildReferenceFilter_ExternalSVG_FetcherError(t *testing.T) {
	b := &FilterEffectBuilder{
		ReferenceBox: image.Rect(0, 0, 50, 50),
		ExternalSVGFetcher: func(uri string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}
	f := b.BuildReferenceFilter("foo.svg#missing")
	if f != nil {
		t.Error("expected nil filter when external fetch fails")
	}
}

// TestBuildReferenceFilter_ExternalSVG_BadID feeds valid SVG but
// requests an id that doesn't exist; expect a nil filter.
func TestBuildReferenceFilter_ExternalSVG_BadID(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="real"><feFlood/></filter></svg>`
	b := &FilterEffectBuilder{
		ReferenceBox: image.Rect(0, 0, 50, 50),
		ExternalSVGFetcher: func(uri string) ([]byte, error) {
			return []byte(svg), nil
		},
	}
	f := b.BuildReferenceFilter("doc.svg#nope")
	if f != nil {
		t.Error("expected nil filter for missing fragment id")
	}
}

// TestBuildReferenceFilter_ExternalSVG_NoFetcher: external URL with
// no fetcher returns nil (null-filter fallback).
func TestBuildReferenceFilter_ExternalSVG_NoFetcher(t *testing.T) {
	b := &FilterEffectBuilder{
		ReferenceBox: image.Rect(0, 0, 50, 50),
	}
	if got := b.BuildReferenceFilter("doc.svg#f"); got != nil {
		t.Error("expected nil filter when no ExternalSVGFetcher is configured")
	}
}

// TestBuildFilterEffect_MixedUrlAndShorthandChain mirrors the WPT
// svg-multiple-filter-functions.html structure: a CSS filter-value-list
// with two external-url() entries followed by a shorthand. Asserts that
// BuildFilterEffect builds a single chain Filter (not nil, not the
// shorthand-only fallback), with LastEffect anchored at the shorthand's
// output and Source still pointing at the chain's original SourceGraphic
// (so the renderer's SetSourceImage feeds the right buffer).
func TestBuildFilterEffect_MixedUrlAndShorthandChain(t *testing.T) {
	const fLeft = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f_left"><feFlood flood-color="red"/></filter></svg>`
	const fRight = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f_right"><feFlood flood-color="blue"/></filter></svg>`
	b := &FilterEffectBuilder{
		ReferenceBox: image.Rect(0, 0, 100, 100),
		ExternalSVGFetcher: func(uri string) ([]byte, error) {
			switch uri {
			case "left.svg":
				return []byte(fLeft), nil
			case "right.svg":
				return []byte(fRight), nil
			}
			return nil, os.ErrNotExist
		},
	}
	ops := []css.FilterFunction{
		{Name: "url", URL: "left.svg#f_left"},
		{Name: "url", URL: "right.svg#f_right"},
		{Name: "hue-rotate", Value: 90},
	}
	f := b.BuildFilterEffect(ops)
	if f == nil {
		t.Fatal("BuildFilterEffect returned nil for mixed chain")
	}
	if f.Source == nil {
		t.Fatal("chain Filter.Source is nil — SetSourceImage would no-op")
	}
	if f.LastEffect == nil {
		t.Fatal("chain Filter.LastEffect is nil")
	}
	// The chain's last effect must NOT be the chain's SourceGraphic —
	// that would mean every chain entry produced nil and we fell through
	// to a pass-through, which is the pre-LOU-135 wrong behavior.
	if f.LastEffect == f.Source {
		t.Errorf("LastEffect == Source: chain composition produced no effects")
	}
}

// TestBuildFilterEffect_UnresolvableUrlInChainIsSkipped covers the
// graceful-degradation rule: a url() that doesn't resolve is dropped
// per Filter Effects 1 §3.1, but the rest of the chain still runs.
func TestBuildFilterEffect_UnresolvableUrlInChainIsSkipped(t *testing.T) {
	b := &FilterEffectBuilder{
		ReferenceBox: image.Rect(0, 0, 50, 50),
		ExternalSVGFetcher: func(uri string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}
	ops := []css.FilterFunction{
		{Name: "url", URL: "missing.svg#x"},
		{Name: "hue-rotate", Value: 90},
	}
	f := b.BuildFilterEffect(ops)
	if f == nil {
		t.Fatal("expected non-nil filter from chain with one usable shorthand entry")
	}
	if f.LastEffect == f.Source {
		t.Errorf("LastEffect == Source: shorthand entry was skipped along with the failed url()")
	}
}

// TestSplitFilterRef covers the small parser inside
// filter_effect_builder.go.
func TestSplitFilterRef(t *testing.T) {
	cases := []struct {
		in   string
		doc  string
		frag string
	}{
		{"#foo", "", "foo"},
		{`"#foo"`, "", "foo"},
		{"'#foo'", "", "foo"},
		{"foo.svg#bar", "foo.svg", "bar"},
		{"data:img/svg,...#bar", "data:img/svg,...", "bar"},
		{"http://x/y#z", "http://x/y", "z"},
		{"  #foo  ", "", "foo"},
		{"", "", ""},
		{"foo.svg", "foo.svg", ""},
	}
	for _, c := range cases {
		d, f := splitFilterRef(c.in)
		if d != c.doc || f != c.frag {
			t.Errorf("splitFilterRef(%q) = (%q, %q), want (%q, %q)", c.in, d, f, c.doc, c.frag)
		}
	}
}

// TestParseSVGFilterURL covers the SVG-attribute parser in
// svg_shape_painter.go.
func TestParseSVGFilterURL(t *testing.T) {
	cases := []struct {
		in   string
		doc  string
		frag string
		ok   bool
	}{
		{"url(#foo)", "", "foo", true},
		{"url(foo.svg#bar)", "foo.svg", "bar", true},
		{`url("foo.svg#bar")`, "foo.svg", "bar", true},
		{"url( 'foo.svg#bar' )", "foo.svg", "bar", true},
		{"url(foo.svg)", "foo.svg", "", true},
		{"none", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		d, f, ok := parseSVGFilterURL(c.in)
		if d != c.doc || f != c.frag || ok != c.ok {
			t.Errorf("parseSVGFilterURL(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, d, f, ok, c.doc, c.frag, c.ok)
		}
	}
}
