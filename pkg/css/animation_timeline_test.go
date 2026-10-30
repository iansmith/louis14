package css

import (
	"math"
	"strconv"
	"testing"
	"time"

	"louis14/pkg/html"
)

// LOU-432: CSS animations against the document timeline.
//
// Each test runs the two style passes the render pipelines run around script
// execution: a first ApplyStylesToDocument with the AnimationClock at 0, a
// DOM change, then a second pass after the clock has (or has not) advanced.
// The expected values follow louis14's decided timing rule (an animation
// first started while the clock reads > 0 has start time = timeline zero, so
// its local time is the clock's current time; one started at clock 0 is held
// at local time 0) plus CSS linear timing math. They are not measurements
// from Chrome, which resolves a new animation's start time at the next frame.

const timelineTestCSS = `<style>
@keyframes display-animation { 0% { display: none; } 100% { display: block; } }
@keyframes fade { from { opacity: 0; } to { opacity: 1; } }
#target { display: none; }
#target.animate { animation: display-animation 1s; display: block; }
@keyframes stay-displayed { from { display: block; } to { display: block; } }
#target.go { animation: fade 1s linear; display: block; }
#target.held { animation: fade 1s linear; animation-play-state: paused; display: block; }
#target.hidden-go { animation: fade 1s linear; }
#target.exit { animation: stay-displayed 1s; }
#target.hidden-animate { animation: display-animation 1s; }
.parent-hidden { display: none; }
#child { animation: fade 1s linear; }
</style>`

// timelineTarget returns the single <div> in doc.
func timelineTarget(t *testing.T, doc *html.Document) *html.Node {
	t.Helper()
	var found *html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.TagName == "div" {
			found = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	if found == nil {
		t.Fatal("no <div> in document")
	}
	return found
}

// restyleAfterFrames runs pass 1 at clock 0 with the div's class set to
// classAtLoad, then sets the class to classAfter, advances the clock by
// frames animation frames, and returns the div's style from pass 2.
func restyleAfterFrames(t *testing.T, classAtLoad, classAfter string, frames int) *Style {
	t.Helper()
	src := timelineTestCSS + `<div id="target"`
	if classAtLoad != "" {
		src += ` class="` + classAtLoad + `"`
	}
	src += `></div>`
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	target := timelineTarget(t, doc)
	ApplyStylesToDocument(doc, 800, 600)
	target.Attributes["class"] = classAfter
	doc.AnimationClock.UpdateTime(time.Duration(frames) * html.ApproximateFrameTime)
	styles := ApplyStylesToDocument(doc, 800, 600)
	style := styles[target]
	if style == nil {
		t.Fatal("no style for target after pass 2")
	}
	return style
}

func opacityOf(t *testing.T, s *Style) float64 {
	t.Helper()
	v, ok := s.Get("opacity")
	if !ok {
		return 1
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		t.Fatalf("opacity %q: %v", v, err)
	}
	return f
}

func TestCSSAnimationStartedAfterFramesSamplesTimelineTime(t *testing.T) {
	// (a) display-none-to-display-block.html's CSS: three frames after the
	// class change the animation's local time is > 0, so none->block gives
	// block (css_display_interpolation_type.cc:29-40 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520).
	s := restyleAfterFrames(t, "", "animate", 3)
	if got, _ := s.Get("display"); got != "block" {
		t.Errorf("(a) display = %q, want \"block\"", got)
	}
	// (b) a linear 1s opacity animation samples at the clock time.
	s = restyleAfterFrames(t, "", "go", 3)
	want := (3 * html.ApproximateFrameTime).Seconds()
	if got := opacityOf(t, s); math.Abs(got-want) > 1e-9 {
		t.Errorf("(b) opacity = %v, want %v", got, want)
	}
}

func TestCSSAnimationPresentAtLoadHeldAtZero(t *testing.T) {
	// A ticket-scope divergence from Blink, not Blink behaviour: an
	// animation already running at the clock-0 style pass stays at local
	// time 0, keeping the established t=0 model for load-time animations.
	s := restyleAfterFrames(t, "go", "go", 3)
	if got := opacityOf(t, s); got != 0 {
		t.Errorf("opacity = %v, want 0", got)
	}
}

func TestCSSAnimationNewPausedHeldAtZero(t *testing.T) {
	// A new animation with animation-play-state: paused is held at time 0
	// (Blink is_paused css_animations.cc:1885, pause() :2404, new animation
	// at inherited time 0 :325-327 @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
	s := restyleAfterFrames(t, "", "held", 3)
	if got := opacityOf(t, s); got != 0 {
		t.Errorf("opacity = %v, want 0", got)
	}
}

func TestCSSAnimationStartedWithoutFramesSamplesZero(t *testing.T) {
	// A class change with no frame after it samples at time 0, like Blink's
	// first frame after the style change (CSSAnimationProxy::
	// CalculateInheritedTime returns 0, css_animations.cc:325-327; Display(0)
	// gives none, css_display_interpolation_type.cc:34 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520).
	s := restyleAfterFrames(t, "", "animate", 0)
	if got, _ := s.Get("display"); got != "none" {
		t.Errorf("display = %q, want \"none\"", got)
	}
	s = restyleAfterFrames(t, "", "go", 0)
	if got := opacityOf(t, s); got != 0 {
		t.Errorf("opacity = %v, want 0", got)
	}
}

func TestCSSAnimationRemovedThenReaddedRestarts(t *testing.T) {
	// An animation-name dropped from the style cancels the running animation
	// (Blink CalculateAnimationUpdate cancels every running animation left
	// unmatched, update.CancelAnimation at css_animations.cc:2050-2053 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520); naming it again later
	// starts a new one. Here the
	// load-time animation is held at 0, removed once the clock has advanced,
	// then re-added: the new animation samples at the timeline time.
	doc, err := html.Parse(timelineTestCSS + `<div id="target" class="go"></div>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	target := timelineTarget(t, doc)
	ApplyStylesToDocument(doc, 800, 600)
	doc.AnimationClock.UpdateTime(3 * html.ApproximateFrameTime)
	target.Attributes["class"] = ""
	ApplyStylesToDocument(doc, 800, 600)
	target.Attributes["class"] = "go"
	s := ApplyStylesToDocument(doc, 800, 600)[target]
	want := (3 * html.ApproximateFrameTime).Seconds()
	if got := opacityOf(t, s); math.Abs(got-want) > 1e-9 {
		t.Errorf("opacity = %v, want %v", got, want)
	}
}

// The display:none tests below follow CSS Animations 1 §2: setting display to
// none terminates the running animations of the element and its descendants,
// and making it displayed again starts the ones its style names. In Blink,
// CalculateAnimationUpdate walks the animation-name list only when the new
// base display or the previous computed style's display is not none
// (css_animations.cc:1864-1866), and Element::RecalcOwnStyle cancels the
// element's CSS animations when its base display is none and no style is
// stored (element.cc:5572-5583), all @ d4ecdfed88f962439247c2ad36b8fe47805b1520.

func TestCSSAnimationDisplayNoneAtLoadStartsWhenShown(t *testing.T) {
	// The clock-0 pass names the animation while the element is display:none
	// and has no previous style, so the animation does not start then. It
	// starts in the pass that displays the element, after 3 frames, and so
	// samples at the timeline time.
	s := restyleAfterFrames(t, "hidden-go", "go", 3)
	want := (3 * html.ApproximateFrameTime).Seconds()
	if got := opacityOf(t, s); math.Abs(got-want) > 1e-9 {
		t.Errorf("opacity = %v, want %v", got, want)
	}
}

func TestCSSAnimationInDisplayNoneSubtreeStartsWhenShown(t *testing.T) {
	// A displayed grandchild of a display:none element is in a display:none
	// subtree: its animation starts only when the ancestor is displayed.
	doc, err := html.Parse(timelineTestCSS + `<section id="parent" class="parent-hidden"><div><p id="child">x</p></div></section>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var parent, child *html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Attributes["id"] {
		case "parent":
			parent = n
		case "child":
			child = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	if parent == nil || child == nil {
		t.Fatal("parent or child missing")
	}
	ApplyStylesToDocument(doc, 800, 600)
	doc.AnimationClock.UpdateTime(3 * html.ApproximateFrameTime)
	parent.Attributes["class"] = ""
	s := ApplyStylesToDocument(doc, 800, 600)[child]
	want := (3 * html.ApproximateFrameTime).Seconds()
	if got := opacityOf(t, s); math.Abs(got-want) > 1e-9 {
		t.Errorf("grandchild opacity = %v, want %v", got, want)
	}
}

func TestCSSAnimationCancelledByDisplayNoneRestarts(t *testing.T) {
	// The load-time animation is held at 0. Hiding the element cancels it
	// (element.cc:5572-5583); showing it again starts a new animation, which
	// samples at the timeline time.
	doc, err := html.Parse(timelineTestCSS + `<div id="target" class="go"></div>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	target := timelineTarget(t, doc)
	ApplyStylesToDocument(doc, 800, 600)
	doc.AnimationClock.UpdateTime(3 * html.ApproximateFrameTime)
	target.Attributes["class"] = "hidden-go"
	ApplyStylesToDocument(doc, 800, 600)
	target.Attributes["class"] = "go"
	s := ApplyStylesToDocument(doc, 800, 600)[target]
	want := (3 * html.ApproximateFrameTime).Seconds()
	if got := opacityOf(t, s); math.Abs(got-want) > 1e-9 {
		t.Errorf("opacity = %v, want %v", got, want)
	}
}

func TestCSSAnimationStartedAsElementIsHiddenKeepsItDisplayed(t *testing.T) {
	// The element was displayed in the previous pass, so the pass that sets
	// its base display to none still starts the animations it names
	// (css_animations.cc:1864-1866 checks the old style's display). An
	// animation that interpolates display to block keeps it displayed and
	// running, so it is not cancelled and holds display: block in a later
	// pass too.
	doc, err := html.Parse(timelineTestCSS + `<div id="target" class="go"></div>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	target := timelineTarget(t, doc)
	ApplyStylesToDocument(doc, 800, 600)
	doc.AnimationClock.UpdateTime(3 * html.ApproximateFrameTime)
	target.Attributes["class"] = "exit"
	for pass := 1; pass <= 2; pass++ {
		s := ApplyStylesToDocument(doc, 800, 600)[target]
		if got, _ := s.Get("display"); got != "block" {
			t.Errorf("pass %d: display = %q, want \"block\"", pass, got)
		}
	}
}

func TestCSSAnimationNamedWhileStayingHiddenDoesNotStart(t *testing.T) {
	// The element was display:none in the previous pass and its base display
	// stays none, so the newly named none->block animation does not start
	// (css_animations.cc:1864-1866) and the element stays display:none.
	s := restyleAfterFrames(t, "", "hidden-animate", 3)
	if got, _ := s.Get("display"); got != "none" {
		t.Errorf("display = %q, want \"none\"", got)
	}
}
