package css

import (
	"testing"

	"louis14/pkg/html"
)

// <img> and <video> map their width and height attributes to
// `aspect-ratio: auto w / h` when both are absolute dimension values
// (HTMLImageElement / HTMLVideoElement::CollectStyleForPresentationAttribute →
// HTMLElement::ApplyAspectRatioToStyle, html_image_element.cc:183-198,
// html_video_element.cc:312-330, html_element.cc:1056-1098 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestPresentationalAspectRatioHint(t *testing.T) {
	cases := []struct {
		tag, width, height string
		want               string // "" = no hint
	}{
		{"img", "60", "60", "auto 60 / 60"},
		{"img", "60.5", "30", "auto 60.5 / 30"},
		{"img", "60%", "30", ""},
		{"img", "60", "", ""},
		{"video", "320", "180", "auto 320 / 180"},
		{"video", "320", "x", ""},
	}
	for _, tc := range cases {
		n := &html.Node{Type: html.ElementNode, TagName: tc.tag, Attributes: map[string]string{}}
		n.Attributes["width"] = tc.width
		if tc.height != "" {
			n.Attributes["height"] = tc.height
		}
		st := NewStyle()
		applyPresentationalAttributes(n, st)
		got, _ := st.Get("aspect-ratio")
		if got != tc.want {
			t.Errorf("<%s width=%q height=%q>: aspect-ratio %q, want %q", tc.tag, tc.width, tc.height, got, tc.want)
		}
	}
}

// <img> width/height attributes map to CSS width/height through the same
// dimension parser as the aspect-ratio hint (AddHTMLLengthToStyle →
// ParseDimensionValue, html_element.cc:3810-3830 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): leading spaces are skipped and a
// garbage suffix is ignored.
func TestImageAttributeLengths(t *testing.T) {
	cases := []struct{ width, height, wantW, wantH string }{
		{" 60", "60abc", "60px", "60px"},
		{"50%", "12.5", "50%", "12.5px"},
		{"abc", "-4", "", ""},
	}
	for _, tc := range cases {
		n := &html.Node{Type: html.ElementNode, TagName: "img", Attributes: map[string]string{"width": tc.width, "height": tc.height}}
		st := NewStyle()
		applyPresentationalAttributes(n, st)
		w, _ := st.Get("width")
		h, _ := st.Get("height")
		if w != tc.wantW || h != tc.wantH {
			t.Errorf("<img width=%q height=%q>: width %q height %q, want %q %q", tc.width, tc.height, w, h, tc.wantW, tc.wantH)
		}
	}
}
