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
