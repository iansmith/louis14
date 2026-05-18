package net

import (
	"strings"
	"testing"
)

func TestIsNetworkURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://example.com/", true},
		{"https://example.com/", true},
		{"file:///tmp/a.svg", false},
		{"data:text/plain,abc", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsNetworkURL(c.in); got != c.want {
			t.Errorf("IsNetworkURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsFileURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"file:///tmp/a.svg", true},
		{"file://localhost/tmp/a.svg", true},
		{"http://example.com/", false},
		{"data:text/plain,abc", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsFileURL(c.in); got != c.want {
			t.Errorf("IsFileURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsDataURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"data:text/plain,abc", true},
		{"data:image/svg+xml;base64,PHN2Zy8+", true},
		{"data:,", true},
		{"http://example.com/", false},
		{"file:///tmp/a.svg", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsDataURL(c.in); got != c.want {
			t.Errorf("IsDataURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDecodeDataURL(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantBody    string
		wantMediaTy string
		wantErr     bool
	}{
		{
			name:        "base64 svg+xml",
			in:          "data:image/svg+xml;base64,PHN2Zy8+",
			wantBody:    "<svg/>",
			wantMediaTy: "image/svg+xml",
		},
		{
			name:        "urlencoded plain",
			in:          "data:text/plain,Hello%20World",
			wantBody:    "Hello World",
			wantMediaTy: "text/plain",
		},
		{
			name:        "no media type",
			in:          "data:,abc",
			wantBody:    "abc",
			wantMediaTy: "",
		},
		{
			name:        "urlencoded svg with hash",
			in:          "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg'/>",
			wantBody:    "<svg xmlns='http://www.w3.org/2000/svg'/>",
			wantMediaTy: "image/svg+xml;utf8",
		},
		{
			name:    "missing comma",
			in:      "data:text/plain;base64",
			wantErr: true,
		},
		{
			name:    "not a data URL",
			in:      "http://example.com/",
			wantErr: true,
		},
		{
			name:    "bad base64",
			in:      "data:image/svg+xml;base64,!!!",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, mt, err := DecodeDataURL(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got body=%q mt=%q", body, mt)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(body) != c.wantBody {
				t.Errorf("body = %q, want %q", string(body), c.wantBody)
			}
			if mt != c.wantMediaTy {
				t.Errorf("mediaType = %q, want %q", mt, c.wantMediaTy)
			}
		})
	}
}

func TestDecodeDataURL_Base64Whitespace(t *testing.T) {
	// RFC 2397 allows whitespace within base64 payload.
	body, _, err := DecodeDataURL("data:text/plain;base64,SGVs\nbG8g\tV29ybGQ=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "Hello World" {
		t.Errorf("body = %q, want %q", string(body), "Hello World")
	}
}

func TestFileURLToPath(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "basic",
			in:   "file:///tmp/a.svg",
			want: "/tmp/a.svg",
		},
		{
			name: "localhost host",
			in:   "file://localhost/tmp/a.svg",
			want: "/tmp/a.svg",
		},
		{
			name: "with fragment",
			in:   "file:///tmp/a.svg",
			want: "/tmp/a.svg",
		},
		{
			name: "nested path",
			in:   "file:///Users/foo/bar/baz.svg",
			want: "/Users/foo/bar/baz.svg",
		},
		{
			name:    "non-local host",
			in:      "file://example.com/foo",
			wantErr: true,
		},
		{
			name:    "not a file URL",
			in:      "http://example.com/",
			wantErr: true,
		},
		{
			name:    "missing path",
			in:      "file://",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FileURLToPath(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("FileURLToPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestResolveURL(t *testing.T) {
	// Sanity: existing behavior still works.
	got := ResolveURL("file:///Users/foo/page.html", "support/x.svg")
	if !strings.HasSuffix(got, "/Users/foo/support/x.svg") {
		t.Errorf("ResolveURL relative file: got %q", got)
	}
}
