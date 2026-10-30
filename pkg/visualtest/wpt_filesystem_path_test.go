package visualtest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// LOU-416: expected paths are literal corpus paths (relative to the
// pkg/visualtest test cwd), never values computed by the resolver itself.
// A row with mustExist also os.Stats the expected file, so a row cannot pass
// by mapping onto a path that serves nothing.

type wptPathRow struct {
	name      string
	basePath  string // directory of the referencing document
	uri       string
	want      string // slash-separated
	mustExist bool
}

func runWPTPathRows(t *testing.T, rows []wptPathRow) {
	t.Helper()
	for _, row := range rows {
		got, err := newWPTResourceResolver(row.basePath).filesystemPath(row.uri)
		if err != nil {
			t.Errorf("%s: filesystemPath(%q) from %q: unexpected error %v", row.name, row.uri, row.basePath, err)
			continue
		}
		if filepath.ToSlash(got) != row.want {
			t.Errorf("%s: filesystemPath(%q) from %q = %q, want %q", row.name, row.uri, row.basePath, filepath.ToSlash(got), row.want)
			continue
		}
		if row.mustExist {
			if _, err := os.Stat(filepath.FromSlash(row.want)); err != nil {
				t.Errorf("%s: expected corpus file %q is missing: %v", row.name, row.want, err)
			}
		}
	}
}

func TestWPTResourceResolver_FilesystemPath(t *testing.T) {
	t.Run("css_section_from_nested_doc", func(t *testing.T) {
		// ic-unit-008-ref.html (css-values/reference/) loads
		// url(/css/css-values/resources/IcTestFullWidth.woff2); upstream
		// /css/<section>/ lives at wpt-css3/<section>/.
		runWPTPathRows(t, []wptPathRow{{
			name:      "ic font from css-values/reference",
			basePath:  "testdata/wpt-css3/css-values/reference",
			uri:       "/css/css-values/resources/IcTestFullWidth.woff2",
			want:      "testdata/wpt-css3/css-values/resources/IcTestFullWidth.woff2",
			mustExist: true,
		}})
	})

	t.Run("fonts_nested_and_top_level", func(t *testing.T) {
		runWPTPathRows(t, []wptPathRow{
			{
				name:      "ahem.css from nested reference dir",
				basePath:  "testdata/wpt-css3/css-values/reference",
				uri:       "/fonts/ahem.css",
				want:      "testdata/wpt-css3/fonts/ahem.css",
				mustExist: true,
			},
			{
				name:      "ahem.css from section dir",
				basePath:  "testdata/wpt-css3/css-pseudo",
				uri:       "/fonts/ahem.css",
				want:      "testdata/wpt-css3/fonts/ahem.css",
				mustExist: true,
			},
			{
				name:      "ahem.css from wpt-css2 section dir",
				basePath:  "testdata/wpt-css2/linebox",
				uri:       "/fonts/ahem.css",
				want:      "testdata/wpt-css2/fonts/ahem.css",
				mustExist: true,
			},
		})
	})

	t.Run("css_support_vendored", func(t *testing.T) {
		// 60x60-green.png is vendored only at wpt-css3/css/support/ (LOU-177).
		runWPTPathRows(t, []wptPathRow{{
			name:      "60x60-green from css-sizing",
			basePath:  "testdata/wpt-css3/css-sizing",
			uri:       "/css/support/60x60-green.png",
			want:      "testdata/wpt-css3/css/support/60x60-green.png",
			mustExist: true,
		}})
	})

	t.Run("css_reference", func(t *testing.T) {
		// rel=match href="/css/reference/blank.html" (css-overflow
		// scrollbar-empty-001 and others).
		runWPTPathRows(t, []wptPathRow{{
			name:      "blank.html from css-overflow",
			basePath:  "testdata/wpt-css3/css-overflow",
			uri:       "/css/reference/blank.html",
			want:      "testdata/wpt-css3/reference/blank.html",
			mustExist: true,
		}})
	})

	t.Run("relative_and_section_escape", func(t *testing.T) {
		runWPTPathRows(t, []wptPathRow{
			{
				name:      "relative from section dir",
				basePath:  "testdata/wpt-css3/css-values",
				uri:       "resources/IcTestZeroWidth.woff2",
				want:      "testdata/wpt-css3/css-values/resources/IcTestZeroWidth.woff2",
				mustExist: true,
			},
			{
				// css-grid/grid-item-percentage-quirk-001's rel=match.
				name:      "relative escaping the corpus",
				basePath:  "testdata/wpt-css3/css-grid",
				uri:       "../../quirks/reference/green-100px-square-no-red.html",
				want:      "testdata/quirks/reference/green-100px-square-no-red.html",
				mustExist: true,
			},
		})
	})

	t.Run("wpt_host_dot_segments_escape", func(t *testing.T) {
		runWPTPathRows(t, []wptPathRow{
			{
				name:      "WPT host URL from nested dir",
				basePath:  "testdata/wpt-css3/css-values/reference",
				uri:       "http://web-platform.test:8000/fonts/ahem.css",
				want:      "testdata/wpt-css3/fonts/ahem.css",
				mustExist: true,
			},
			{
				name:      "dot segments under a mount",
				basePath:  "testdata/wpt-css3/css-pseudo",
				uri:       "/css/../fonts/ahem.css",
				want:      "testdata/wpt-css3/fonts/ahem.css",
				mustExist: true,
			},
			{
				// URL dot-segment removal cannot climb above "/", so the
				// result stays under the doc root.
				name:     "dot segments above the root",
				basePath: "testdata/wpt-css3/css-pseudo",
				uri:      "/../../etc/x",
				want:     "testdata/wpt-css3/etc/x",
			},
		})
	})

	t.Run("unsupported_schemes", func(t *testing.T) {
		r := newWPTResourceResolver("testdata/wpt-css3/css-pseudo")
		for _, uri := range []string{"https://example.com/x", "//example.com/x", "data:,x", "file:///etc/hosts"} {
			if got, err := r.filesystemPath(uri); err == nil {
				t.Errorf("filesystemPath(%q) = %q, want an error", uri, got)
			}
		}
	})
}

func TestNewWPTResourceResolver_DocRoot(t *testing.T) {
	for _, tc := range []struct{ basePath, want string }{
		{"testdata/wpt-css3/css-values/reference", "testdata/wpt-css3"},
		{"testdata/wpt-css3/css-values", "testdata/wpt-css3"},
	} {
		if got := filepath.ToSlash(newWPTResourceResolver(tc.basePath).docRoot); got != tc.want {
			t.Errorf("newWPTResourceResolver(%q).docRoot = %q, want %q", tc.basePath, got, tc.want)
		}
	}
}

func TestFindWPTRoot(t *testing.T) {
	for _, tc := range []struct{ dir, want string }{
		{"testdata/wpt-css3/css-values/reference", "testdata/wpt-css3"},
		{"testdata/wpt-css3", "testdata/wpt-css3"},
		{"testdata/wpt-css2/linebox", "testdata/wpt-css2"},
		{"foo/bar", "foo"}, // not in a corpus: falls back to the parent dir
	} {
		if got := filepath.ToSlash(findWPTRoot(filepath.FromSlash(tc.dir))); got != tc.want {
			t.Errorf("findWPTRoot(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}

// TestFileFetchers_ShareFilesystemPath pins that the harness fetcher kinds
// (the image fetcher is r.readFile itself) load through the shared resolver: from a nested reference dir, each one
// must return the bytes of the literal corpus file wptserve would serve.
func TestFileFetchers_ShareFilesystemPath(t *testing.T) {
	r := newWPTResourceResolver("testdata/wpt-css3/css-values/reference")
	for _, tc := range []struct {
		name  string
		fetch func(string) ([]byte, error)
		uri   string
		want  string
	}{
		{"image", r.readFile, "/css/support/60x60-green.png", "testdata/wpt-css3/css/support/60x60-green.png"},
		{"document", func(uri string) ([]byte, error) {
			s, err := createFileDocumentFetcher(r)(uri)
			return []byte(s), err
		}, "/css/reference/blank.html", "testdata/wpt-css3/reference/blank.html"},
		{"external SVG", createExternalSVGFetcher(r), "/css/filter-effects/support/hueRotate.svg", "testdata/wpt-css3/filter-effects/support/hueRotate.svg"},
	} {
		want, err := os.ReadFile(filepath.FromSlash(tc.want))
		if err != nil {
			t.Fatalf("%s: reading expected corpus file: %v", tc.name, err)
		}
		got, err := tc.fetch(tc.uri)
		if err != nil {
			t.Errorf("%s fetcher(%q): %v", tc.name, tc.uri, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s fetcher(%q) returned %d bytes, want the %d bytes of %s", tc.name, tc.uri, len(got), len(want), tc.want)
		}
	}
}
