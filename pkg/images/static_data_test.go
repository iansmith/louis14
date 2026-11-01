package images

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

// TestNewFilesystemFetcher_DataURL: the filesystem fetcher serves a data:
// URL itself, through the shared fetcher-layer static-data branch, as Blink's
// ResourceFetcher does for every resource type
// (platform/loader/fetch/resource_fetcher.cc:1061-1063 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestNewFilesystemFetcher_DataURL(t *testing.T) {
	body, err := NewFilesystemFetcher("/nonexistent/doc.html")("data:text/plain,abc")
	if err != nil || string(body) != "abc" {
		t.Errorf("fetch(data:) = %q, %v; want abc", body, err)
	}
}

// TestLoadImageWithFetcher_DataURLGoesThroughFetcher: with a fetcher, a
// data: image is requested through it like any other URL — the image loader
// no longer decodes data: URLs on its own.
func TestLoadImageWithFetcher_DataURLGoesThroughFetcher(t *testing.T) {
	// A 3x1 image no other test loads, so the decoded-image cache is cold.
	img3 := image.NewRGBA(image.Rect(0, 0, 3, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img3); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	var requested []string
	fetcher := func(u string) ([]byte, error) {
		requested = append(requested, u)
		return NewFilesystemFetcher("")(u)
	}
	img, err := LoadImageWithFetcher(uri, fetcher)
	if err != nil {
		t.Fatalf("LoadImageWithFetcher(data:): %v", err)
	}
	if b := img.Bounds(); b.Dx() != 3 || b.Dy() != 1 {
		t.Errorf("image is %v, want 3x1", b)
	}
	if len(requested) != 1 || requested[0] != uri {
		t.Errorf("fetcher saw %d requests, want the one data: URL", len(requested))
	}
}
