package layout

import (
	"testing"

	"louis14/pkg/html"
)

// LayoutVideo::GetDisplayMode (layout_video.cc:121-135 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): without a media player a video
// shows its poster whenever it has one.
func TestGetVideoDisplayMode(t *testing.T) {
	withPoster := &html.Node{Type: html.ElementNode, TagName: "video", Attributes: map[string]string{"poster": "p.png"}}
	without := &html.Node{Type: html.ElementNode, TagName: "video", Attributes: map[string]string{"src": "m.webm", "poster": " "}}
	if GetVideoDisplayMode(withPoster) != VideoDisplayModePoster {
		t.Error("video with a poster should be in poster mode")
	}
	if GetVideoDisplayMode(without) != VideoDisplayModeVideo {
		t.Error("video with a blank poster should be in video mode")
	}
}

func TestLayoutImageSource(t *testing.T) {
	el := func(tag string, attrs map[string]string) *html.Node {
		return &html.Node{Type: html.ElementNode, TagName: tag, Attributes: attrs}
	}
	for _, tc := range []struct {
		n      *html.Node
		url    string
		layout bool
	}{
		{el("video", map[string]string{"poster": " p.png "}), "p.png", true},
		{el("video", nil), "", true},
		{el("img", map[string]string{"src": "a.png"}), "a.png", true},
		{el("object", map[string]string{"data": "b.png"}), "b.png", true},
		{el("object", map[string]string{"data": "b.html"}), "", false},
		{el("div", nil), "", false},
	} {
		if url, ok := LayoutImageSource(tc.n); url != tc.url || ok != tc.layout {
			t.Errorf("<%s %v>: (%q, %v), want (%q, %v)", tc.n.TagName, tc.n.Attributes, url, ok, tc.url, tc.layout)
		}
	}
}
