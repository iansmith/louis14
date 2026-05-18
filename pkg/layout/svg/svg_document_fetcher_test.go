package svg

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fakeNetworkFetcher counts calls and serves a fixed response.
type fakeNetworkFetcher struct {
	body  []byte
	ct    string
	err   error
	calls int32
}

func (f *fakeNetworkFetcher) Fetch(uri string) ([]byte, string, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.body, f.ct, f.err
}

const svgWithFlood = `<svg xmlns="http://www.w3.org/2000/svg">` +
	`<filter id="f"><feFlood flood-color="green"/></filter></svg>`

func TestFetchSVGForFilter_DataURL_Base64(t *testing.T) {
	// data:image/svg+xml;base64,<base64>
	body := svgWithFlood
	// pre-encode by hand to avoid an extra import in the test
	dataURL := "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPjxmaWx0ZXIgaWQ9ImYiPjxmZUZsb29kIGZsb29kLWNvbG9yPSJncmVlbiIvPjwvZmlsdGVyPjwvc3ZnPg=="
	f := NewSVGDocumentFetcher(nil, "https://host.example/")
	doc, err := f.FetchSVGForFilter(dataURL)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if string(doc.Data) != body {
		t.Errorf("Data = %q, want %q", string(doc.Data), body)
	}
	if doc.Tainted {
		t.Error("data URLs are not tainted")
	}
}

func TestFetchSVGForFilter_DataURL_URLEncoded(t *testing.T) {
	dataURL := "data:image/svg+xml;utf8," + svgWithFlood
	f := NewSVGDocumentFetcher(nil, "https://host.example/")
	doc, err := f.FetchSVGForFilter(dataURL)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if string(doc.Data) != svgWithFlood {
		t.Errorf("Data = %q, want %q", string(doc.Data), svgWithFlood)
	}
	if doc.Tainted {
		t.Error("data URLs are not tainted")
	}
}

func TestFetchSVGForFilter_FileURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.svg")
	if err := os.WriteFile(path, []byte(svgWithFlood), 0644); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + path

	f := NewSVGDocumentFetcher(nil, "https://host.example/")
	doc, err := f.FetchSVGForFilter(uri)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if string(doc.Data) != svgWithFlood {
		t.Errorf("Data mismatch")
	}
	if doc.Tainted {
		t.Error("file:// is treated as same-origin")
	}
	wantOrigin := "file://" + dir
	if doc.Origin != wantOrigin {
		t.Errorf("Origin = %q, want %q", doc.Origin, wantOrigin)
	}
}

func TestFetchSVGForFilter_FileURL_NotFound(t *testing.T) {
	f := NewSVGDocumentFetcher(nil, "https://host.example/")
	_, err := f.FetchSVGForFilter("file:///nonexistent/x.svg")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchSVGForFilter_NetworkSameOrigin(t *testing.T) {
	fake := &fakeNetworkFetcher{body: []byte(svgWithFlood), ct: "image/svg+xml"}
	f := NewSVGDocumentFetcher(fake, "https://a.example")
	doc, err := f.FetchSVGForFilter("https://a.example/foo.svg")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if doc.Tainted {
		t.Error("same-origin should not taint")
	}
	if doc.Origin != "https://a.example" {
		t.Errorf("Origin = %q", doc.Origin)
	}
}

func TestFetchSVGForFilter_NetworkCrossOriginTaints(t *testing.T) {
	fake := &fakeNetworkFetcher{body: []byte(svgWithFlood), ct: "image/svg+xml"}
	f := NewSVGDocumentFetcher(fake, "https://a.example")
	doc, err := f.FetchSVGForFilter("https://b.example/foo.svg")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !doc.Tainted {
		t.Error("cross-origin should taint")
	}
	if doc.Origin != "https://b.example" {
		t.Errorf("Origin = %q", doc.Origin)
	}
}

func TestFetchSVGForFilter_NetworkNoBase(t *testing.T) {
	f := NewSVGDocumentFetcher(nil, "https://a.example")
	_, err := f.FetchSVGForFilter("https://b.example/foo.svg")
	if err == nil {
		t.Fatal("expected error when base fetcher is nil for http URL")
	}
}

func TestFetchSVGForFilter_NetworkError(t *testing.T) {
	fake := &fakeNetworkFetcher{err: fmt.Errorf("boom")}
	f := NewSVGDocumentFetcher(fake, "https://a.example")
	_, err := f.FetchSVGForFilter("https://a.example/foo.svg")
	if err == nil {
		t.Fatal("expected error propagation")
	}
}

func TestFetchSVGForFilter_Cache(t *testing.T) {
	fake := &fakeNetworkFetcher{body: []byte(svgWithFlood), ct: "image/svg+xml"}
	f := NewSVGDocumentFetcher(fake, "https://a.example")
	for i := 0; i < 5; i++ {
		if _, err := f.FetchSVGForFilter("https://a.example/foo.svg"); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&fake.calls); got != 1 {
		t.Errorf("base.Fetch called %d times, want 1", got)
	}
}

func TestFetchSVGForFilter_UnsupportedScheme(t *testing.T) {
	f := NewSVGDocumentFetcher(nil, "")
	_, err := f.FetchSVGForFilter("ftp://example.com/x.svg")
	if err == nil {
		t.Fatal("expected error for ftp scheme")
	}
}

func TestSplitFilterURL(t *testing.T) {
	cases := []struct {
		in   string
		doc  string
		frag string
	}{
		{"#foo", "", "foo"},
		{"foo.svg#bar", "foo.svg", "bar"},
		{"http://x/foo.svg#bar", "http://x/foo.svg", "bar"},
		{"foo.svg", "foo.svg", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		doc, frag := SplitFilterURL(c.in)
		if doc != c.doc || frag != c.frag {
			t.Errorf("SplitFilterURL(%q) = (%q, %q), want (%q, %q)", c.in, doc, frag, c.doc, c.frag)
		}
	}
}
