package css

import (
	"testing"

	"louis14/pkg/html"
)

// Blink's html.css gives <audio> two rules and otherwise the initial
// display: inline (html/resources/html.css:160-173 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//
//	audio:not([controls]) { display: none !important; }
//	audio { width: 300px; height: 54px; }
//
// The !important rule beats author CSS.
func TestAudioUserAgentStyle(t *testing.T) {
	author, err := ParseStylesheet("audio.b { display: block; }", nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		attrs                 map[string]string
		wantDisplay           DisplayType
		wantWidth, wantHeight string
	}{
		{map[string]string{"controls": ""}, DisplayInline, "300px", "54px"},
		{map[string]string{}, DisplayNone, "300px", "54px"},
		{map[string]string{"class": "b"}, DisplayNone, "300px", "54px"},
		{map[string]string{"class": "b", "controls": ""}, DisplayBlock, "300px", "54px"},
	}
	for _, tc := range cases {
		n := &html.Node{Type: html.ElementNode, TagName: "audio", Attributes: tc.attrs}
		st := ComputeStyle(n, []*Stylesheet{author}, 800, 600, nil)
		w, _ := st.Get("width")
		h, _ := st.Get("height")
		if st.GetDisplay() != tc.wantDisplay || w != tc.wantWidth || h != tc.wantHeight {
			t.Errorf("<audio %v>: display %q %s x %s, want %q %s x %s", tc.attrs, st.GetDisplay(), w, h, tc.wantDisplay, tc.wantWidth, tc.wantHeight)
		}
	}
}

// A user-agent !important declaration outranks the animation origin (CSS
// Cascade 5 §6.1), so a keyframe cannot bring back a controls-less <audio>
// or unclip a frame owner.
func TestUserAgentImportantBeatsAnimations(t *testing.T) {
	ss, err := ParseStylesheet(`
		@keyframes show { from { display: block } to { display: block } }
		@keyframes unclip { from { overflow: visible } to { overflow: visible } }
		audio { animation: show 1s both; }
		iframe { animation: unclip 1s both; }`, nil)
	if err != nil {
		t.Fatal(err)
	}
	audio := &html.Node{Type: html.ElementNode, TagName: "audio", Attributes: map[string]string{}}
	if d := ComputeStyle(audio, []*Stylesheet{ss}, 800, 600, nil).GetDisplay(); d != DisplayNone {
		t.Errorf("animated controls-less <audio>: display %q, want none", d)
	}
	iframe := &html.Node{Type: html.ElementNode, TagName: "iframe", Attributes: map[string]string{}}
	if o, _ := ComputeStyle(iframe, []*Stylesheet{ss}, 800, 600, nil).Get("overflow-x"); o != "clip" {
		t.Errorf("animated <iframe>: overflow-x %q, want clip", o)
	}
}

// The audio UA rules match the HTML <audio> only: an <audio> element in the
// SVG namespace gets neither (html.css selectors match HTML-namespace
// elements by tag; Node::HasTagName).
func TestAudioUserAgentStyleIsHTMLOnly(t *testing.T) {
	doc, err := html.Parse(`<!DOCTYPE html><svg><audio id=a></audio></svg>`)
	if err != nil {
		t.Fatal(err)
	}
	var a *html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if v, _ := n.GetAttribute("id"); n.Type == html.ElementNode && v == "a" {
			a = n
		}
		for _, c := range n.Children {
			find(c)
		}
	}
	find(doc.Root)
	if a == nil || a.NamespaceURI() == html.XHTMLNamespaceURI {
		t.Fatalf("expected an SVG-namespace <audio>, got %+v", a)
	}
	st := ComputeStyle(a, nil, 800, 600, nil)
	if w, ok := st.Get("width"); ok || st.GetDisplay() == DisplayNone {
		t.Errorf("SVG <audio>: width %q (set %v), display %q; want no audio UA rules", w, ok, st.GetDisplay())
	}
	if html.IsReplacedElement(a) {
		t.Error("SVG <audio> must not be a replaced element")
	}
}

// Replaced elements take the initial display: inline, as Blink's html.css
// sets none of video, canvas, embed or iframe (html/resources/html.css @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestReplacedElementsDefaultInline(t *testing.T) {
	for _, tag := range []string{"video", "canvas", "embed", "iframe", "audio"} {
		attrs := map[string]string{"controls": ""}
		n := &html.Node{Type: html.ElementNode, TagName: tag, Attributes: attrs}
		if d := ComputeStyle(n, nil, 800, 600, nil).GetDisplay(); d != DisplayInline {
			t.Errorf("<%s>: display %q, want inline", tag, d)
		}
	}
}
