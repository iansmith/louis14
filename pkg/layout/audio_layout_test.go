package layout

import "testing"

// <audio> is a replaced element (LayoutMedia, a LayoutReplaced) and inline
// by default, so it sits on the line beside a canvas. Size containment
// zeroes its natural size: with width/height auto and a 3px border it is 6x6
// rather than a block stretched across the container. Mirrors WPT
// css-contain/contain-size-replaced-003a.
func TestAudioIsInlineReplaced(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="width:400px">`+
		`<audio id=a controls style="contain:size; width:auto; height:auto; border:3px solid"></audio>`+
		`<canvas id=c style="contain:size; width:auto; height:auto; border:3px solid"></canvas></div></body>`, nil)
	a, c := findBox(fl.root, byID("a")), findBox(fl.root, byID("c"))
	if a == nil || c == nil {
		t.Fatalf("missing boxes: audio %s canvas %s", boxDims(a), boxDims(c))
	}
	if a.Width != 6 || a.Height != 6 {
		t.Errorf("size-contained audio: %s, want 6x6", boxDims(a))
	}
	if a.Y != c.Y || c.X <= a.X {
		t.Errorf("audio %s and canvas %s should share a line, audio first", boxDims(a), boxDims(c))
	}
}

// With no author sizing an <audio controls> takes the UA 300x54 size.
func TestAudioDefaultSize(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><audio id=a controls></audio></body>`, nil)
	if a := findBox(fl.root, byID("a")); a == nil || a.Width != 300 || a.Height != 54 {
		t.Errorf("audio controls: %s, want 300x54", boxDims(a))
	}
}

// With the UA size overridden, <audio> takes the LayoutReplaced default
// natural size, 300x150 with no ratio.
func TestAudioNaturalSize(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><audio id=a controls style="width:auto; height:auto"></audio></body>`, nil)
	if a := findBox(fl.root, byID("a")); a == nil || a.Width != 300 || a.Height != 150 {
		t.Errorf("audio with auto size: %s, want 300x150", boxDims(a))
	}
}

// LayoutMedia renders none of the element's DOM children, so the fallback
// content of <audio> neither lays out nor paints.
func TestAudioFallbackContentNotRendered(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><audio id=a controls><source src="a.ogg">fallback text</audio></body>`, nil)
	if texts := collectTextBoxes(fl.root, nil); len(texts) != 0 {
		t.Errorf("audio fallback content rendered: %d text boxes, first %q", len(texts), texts[0].Text)
	}
	if a := findBox(fl.root, byID("a")); a == nil || a.Width != 300 || a.Height != 54 {
		t.Errorf("audio: %s, want 300x54", boxDims(a))
	}
}
