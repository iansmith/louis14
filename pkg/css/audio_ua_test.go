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
