package net

import (
	"errors"
	"testing"
)

// TestRequestResource_StaticData pins the fetcher-layer data: branch: a data:
// URL is decoded in place and never reaches the underlying fetch, every other
// URL does. Mirrors ResourceFetcher::RequestResource handing a data: URL to
// CreateResourceForStaticData (platform/loader/fetch/resource_fetcher.cc:1463-1474,
// 1061-1063 @ 5992657f556bcd9d207a4aca187488462f027cab).
func TestRequestResource_StaticData(t *testing.T) {
	var fetched []string
	fetch := RequestResource(func(url string) ([]byte, error) {
		fetched = append(fetched, url)
		return []byte("net:" + url), nil
	})

	body, err := fetch("data:text/plain;base64,SGVsbG8=")
	if err != nil || string(body) != "Hello" {
		t.Errorf("data: URL = %q, %v; want %q", body, err, "Hello")
	}
	body, err = fetch("img.png")
	if err != nil || string(body) != "net:img.png" {
		t.Errorf("img.png = %q, %v; want the underlying fetch's body", body, err)
	}
	if len(fetched) != 1 || fetched[0] != "img.png" {
		t.Errorf("underlying fetch saw %q, want only img.png", fetched)
	}

	// No underlying fetch: static data still loads, anything else errors.
	none := RequestResource(nil)
	if body, err := none("data:,abc"); err != nil || string(body) != "abc" {
		t.Errorf("nil fetch, data: URL = %q, %v; want abc", body, err)
	}
	if _, err := none("img.png"); err == nil {
		t.Error("nil fetch, img.png: want an error")
	}
	bad := RequestResource(func(string) ([]byte, error) { return nil, errors.New("unreachable") })
	if _, err := bad("data:;base64,!!!"); err == nil {
		t.Error("malformed data: URL: want a decode error")
	}
}

// TestDecodeDataURL_PercentDecoding pins the percent-decoding the image
// loader's former private decoder applied, now that std/net owns the one
// data: URL decoder: '+' is not a space (the fetch spec's data: URL
// processor percent-decodes only), an invalid escape such as SVG's
// width='100%' passes through, and a percent-escaped base64 payload is
// unescaped before decoding.
func TestDecodeDataURL_PercentDecoding(t *testing.T) {
	cases := []struct{ in, want string }{
		{"data:text/plain,a+b", "a+b"},
		{"data:image/svg+xml,<svg width='100%'/>", "<svg width='100%'/>"},
		{"data:image/svg+xml,%3Csvg%2F%3E", "<svg/>"},
		{"data:text/plain;base64,SGk%2B", "Hi>"},
	}
	for _, c := range cases {
		body, _, err := DecodeDataURL(c.in)
		if err != nil || string(body) != c.want {
			t.Errorf("DecodeDataURL(%q) = %q, %v; want %q", c.in, body, err, c.want)
		}
	}
}

// TestDataURLForDisplay: a data: URL keeps only its header in a log or
// error message, never its payload; any other URL is unchanged.
func TestDataURLForDisplay(t *testing.T) {
	if got, want := DataURLForDisplay("data:font/woff2;base64,AAAA"), "data:font/woff2;base64,<4 bytes>"; got != want {
		t.Errorf("DataURLForDisplay = %q, want %q", got, want)
	}
	if got := DataURLForDisplay("a.woff2"); got != "a.woff2" {
		t.Errorf("DataURLForDisplay(a.woff2) = %q", got)
	}
}
