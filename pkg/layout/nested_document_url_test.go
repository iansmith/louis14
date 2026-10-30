package layout

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"louis14/pkg/html"
)

// TestLayoutNestedDocument_RelativeURLsCompleteAgainstOwnerDocument pins
// that every relative URL inside a nested (iframe) document is completed
// against that document's own base before it reaches the document-agnostic
// fetchers, as Blink does: Document::CompleteURL
// (third_party/blink/renderer/core/dom/document.cc:7553), called for <img
// src> by ImageLoader::ImageSourceToKURL
// (third_party/blink/renderer/core/loader/image_loader.cc:746) and for
// iframe src by HTMLFrameElementBase::OpenURL
// (third_party/blink/renderer/core/html/html_frame_element_base.cc:70). A
// srcdoc document's base is its container document's base
// (Document::FallbackBaseURL, document.cc:5046/:5063). All @
// d4ecdfed88f962439247c2ad36b8fe47805b1520.
//
// The fetchers record every URI they are asked for. images.globalCache is
// process-global and keyed by the URI string, so every image name carries
// a per-run nonce.
func TestLayoutNestedDocument_RelativeURLsCompleteAgainstOwnerDocument(t *testing.T) {
	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	name := func(n int) string { return fmt.Sprintf("lou441-l%d-%s.png", n, nonce) }

	tests := []struct {
		name  string
		outer string
		docs  map[string]string
		imgs  map[string][2]int // served URI -> natural size
		want  string            // completed URI the nested <img> must fetch
		// bareMustNotBeFetched: the uncompleted name must never reach the
		// fetcher (false when the outer document itself fetches it).
		bareMustNotBeFetched bool
	}{
		{
			name:                 "depth1",
			outer:                `<iframe src="a/frame.html"></iframe>`,
			docs:                 map[string]string{"a/frame.html": `<img src="` + name(1) + `">`},
			imgs:                 map[string][2]int{"a/" + name(1): {30, 20}},
			want:                 "a/" + name(1),
			bareMustNotBeFetched: true,
		},
		{
			name:  "depth2",
			outer: `<iframe src="a/frame.html"></iframe>`,
			docs: map[string]string{
				"a/frame.html":   `<iframe src="b/inner.html"></iframe>`,
				"a/b/inner.html": `<img src="` + name(2) + `">`,
			},
			imgs:                 map[string][2]int{"a/b/" + name(2): {30, 20}},
			want:                 "a/b/" + name(2),
			bareMustNotBeFetched: true,
		},
		{
			name:  "srcdocInSubdir",
			outer: `<iframe src="a/frame.html"></iframe>`,
			docs: map[string]string{
				"a/frame.html": `<iframe srcdoc="<img src=` + name(3) + `>"></iframe>`,
			},
			imgs:                 map[string][2]int{"a/" + name(3): {30, 20}},
			want:                 "a/" + name(3),
			bareMustNotBeFetched: true,
		},
		{
			name:                 "rootRelativeFrame",
			outer:                `<iframe src="/r/frame.html"></iframe>`,
			docs:                 map[string]string{"/r/frame.html": `<img src="` + name(4) + `">`},
			imgs:                 map[string][2]int{"/r/" + name(4): {30, 20}},
			want:                 "/r/" + name(4),
			bareMustNotBeFetched: true,
		},
		{
			// The outer document fetches the same relative name first, so
			// an uncompleted nested lookup hits the outer's cache entry.
			name:  "cachePrimed",
			outer: `<img src="` + name(5) + `"><iframe src="a/frame.html"></iframe>`,
			docs:  map[string]string{"a/frame.html": `<img src="` + name(5) + `">`},
			imgs: map[string][2]int{
				name(5):        {7, 7},
				"a/" + name(5): {30, 20},
			},
			want: "a/" + name(5),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var fetchedImgs, fetchedDocs []string
			engine := NewLayoutEngine(400, 400)
			engine.SetDocumentFetcher(func(uri string) (string, error) {
				fetchedDocs = append(fetchedDocs, uri)
				if content, ok := tc.docs[uri]; ok {
					return content, nil
				}
				return "", fmt.Errorf("no document %q", uri)
			})
			engine.SetImageFetcher(func(uri string) ([]byte, error) {
				fetchedImgs = append(fetchedImgs, uri)
				if size, ok := tc.imgs[uri]; ok {
					return fakeImagePNG(size[0], size[1]), nil
				}
				return nil, fmt.Errorf("no image %q", uri)
			})

			doc, err := html.Parse(`<!doctype html><body>` + tc.outer + `</body>`)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			engine.Layout(doc)

			if !slices.Contains(fetchedImgs, tc.want) {
				t.Errorf("image fetches %q do not include the completed URI %q", fetchedImgs, tc.want)
			}
			if tc.bareMustNotBeFetched {
				bare := tc.want[strings.LastIndex(tc.want, "/")+1:]
				if slices.Contains(fetchedImgs, bare) {
					t.Errorf("image fetches %q include the uncompleted name %q", fetchedImgs, bare)
				}
			}
			for _, uri := range slices.Concat(fetchedImgs, fetchedDocs) {
				for _, doubled := range []string{"a/a/", "b/b/", "a/b/a/", "r/r/"} {
					if strings.Contains(uri, doubled) {
						t.Errorf("fetched %q: base joined twice (%q)", uri, doubled)
					}
				}
			}
		})
	}
}
