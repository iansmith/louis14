package css

import (
	"testing"

	"louis14/pkg/html"
)

// LOU-359 Category F — CSS Transitions, first-frame semantics. louis14 paints
// exactly one frame after script execution, so a transition's contribution is
// its value at local time 0: during the delay the before-change value shows;
// with steps(N, start) the first jump has already happened (transformed
// progress 1/N); with a continuous easing f(0)=0 the before-change value
// shows. Mirrors Blink CSSAnimations::CalculateTransitionUpdate /
// CalculateTransitionUpdateForStandardProperty (core/animation/css/
// css_animations.cc:2806-2954 @ Chromium
// 906a32d8634edf17db094507908f439bd9b52de5): a transition starts when the
// before- and after-change computed values differ for a property in
// transition-property with a nonzero combined duration, and the running
// effect samples the timing function at the current time.
//
// WPT: css-pseudo/first-line-inherited-with-transition.html — black -> lime
// under `transition: background-color 1000s steps(2, start)` must paint the
// half-way blend rgb(0, 128, 0) (the reference's `green`).
func TestApplyTransitionsFirstFrame(t *testing.T) {
	mk := func(props map[string]string) *Style {
		s := NewStyle()
		for k, v := range props {
			s.Properties[k] = v
		}
		return s
	}
	build := func(oldProps, newProps map[string]string) (*html.Node, map[*html.Node]*Style, map[*html.Node]*Style) {
		n := &html.Node{Type: html.ElementNode, TagName: "span"}
		return n, map[*html.Node]*Style{n: mk(newProps)}, map[*html.Node]*Style{n: mk(oldProps)}
	}

	t.Run("steps-start paints the mid-transition blend", func(t *testing.T) {
		n, styles, prev := build(
			map[string]string{"background-color": "black"},
			map[string]string{"background-color": "lime",
				"transition": "background-color 1000s steps(2, start)"})
		ApplyTransitionsFirstFrame(n, styles, prev)
		if got := styles[n].Properties["background-color"]; got != "rgb(0, 128, 0)" {
			t.Errorf("background-color = %q, want %q", got, "rgb(0, 128, 0)")
		}
	})

	t.Run("continuous easing holds the before-change value at t=0", func(t *testing.T) {
		n, styles, prev := build(
			map[string]string{"background-color": "black"},
			map[string]string{"background-color": "lime",
				"transition": "background-color 1000s linear"})
		ApplyTransitionsFirstFrame(n, styles, prev)
		if got := styles[n].Properties["background-color"]; got != "black" {
			t.Errorf("background-color = %q, want before-change black", got)
		}
	})

	t.Run("delay holds the before-change value", func(t *testing.T) {
		n, styles, prev := build(
			map[string]string{"background-color": "black"},
			map[string]string{"background-color": "lime",
				"transition": "background-color 1000s steps(2, start) 500s"})
		ApplyTransitionsFirstFrame(n, styles, prev)
		if got := styles[n].Properties["background-color"]; got != "black" {
			t.Errorf("background-color = %q, want before-change black (delay phase)", got)
		}
	})

	t.Run("untransitioned property is untouched", func(t *testing.T) {
		n, styles, prev := build(
			map[string]string{"background-color": "black"},
			map[string]string{"background-color": "lime",
				"transition": "color 1000s steps(2, start)"})
		ApplyTransitionsFirstFrame(n, styles, prev)
		if got := styles[n].Properties["background-color"]; got != "lime" {
			t.Errorf("background-color = %q, want after-change lime (not in transition-property)", got)
		}
	})

	t.Run("no before-change style means no transition", func(t *testing.T) {
		n := &html.Node{Type: html.ElementNode, TagName: "span"}
		styles := map[*html.Node]*Style{n: mk(map[string]string{
			"background-color": "lime",
			"transition":       "background-color 1000s steps(2, start)"})}
		ApplyTransitionsFirstFrame(n, styles, map[*html.Node]*Style{})
		if got := styles[n].Properties["background-color"]; got != "lime" {
			t.Errorf("background-color = %q, want lime", got)
		}
	})

	t.Run("transition longhands work without the shorthand", func(t *testing.T) {
		n, styles, prev := build(
			map[string]string{"opacity": "0"},
			map[string]string{"opacity": "1",
				"transition-property":        "opacity",
				"transition-duration":        "100s",
				"transition-timing-function": "steps(4, start)"})
		ApplyTransitionsFirstFrame(n, styles, prev)
		if got := styles[n].Properties["opacity"]; got != "0.25" {
			t.Errorf("opacity = %q, want 0.25 (first of 4 steps)", got)
		}
	})

	t.Run("nil-CascadedProps node neither blocks nor truncates propagation", func(t *testing.T) {
		// CodeRabbit PR#160 finding 1: Style.CascadedProps == nil means the
		// declaration record was not captured (style built outside
		// ComputeStyle, e.g. the synthetic root style); the transition
		// propagation walk must treat it as "declares nothing" — override
		// its inherited value AND keep recursing into its subtree.
		parent := &html.Node{Type: html.ElementNode, TagName: "div"}
		mid := &html.Node{Type: html.ElementNode, TagName: "span", Parent: parent}
		leaf := &html.Node{Type: html.ElementNode, TagName: "b", Parent: mid}
		parent.Children = []*html.Node{mid}
		mid.Children = []*html.Node{leaf}
		parentNew := mk(map[string]string{"color": "black",
			"transition": "color 1000s steps(2, start)"})
		midNew := mk(map[string]string{"color": "black"}) // nil CascadedProps
		leafNew := mk(map[string]string{"color": "black"})
		leafNew.CascadedProps = map[string]bool{}
		styles := map[*html.Node]*Style{parent: parentNew, mid: midNew, leaf: leafNew}
		prev := map[*html.Node]*Style{parent: mk(map[string]string{"color": "lime"})}
		ApplyTransitionsFirstFrame(parent, styles, prev)
		if got := midNew.Properties["color"]; got != "rgb(0, 128, 0)" {
			t.Errorf("nil-CascadedProps mid color = %q, want propagated %q", got, "rgb(0, 128, 0)")
		}
		if got := leafNew.Properties["color"]; got != "rgb(0, 128, 0)" {
			t.Errorf("leaf below nil-CascadedProps mid: color = %q, want propagated %q", got, "rgb(0, 128, 0)")
		}
	})

	t.Run("inherited transitioned value propagates to inheriting children", func(t *testing.T) {
		parent := &html.Node{Type: html.ElementNode, TagName: "div"}
		child := &html.Node{Type: html.ElementNode, TagName: "span", Parent: parent}
		parent.Children = []*html.Node{child}
		parentNew := mk(map[string]string{"color": "black",
			"transition": "color 1000s steps(2, start)"})
		childNew := mk(map[string]string{"color": "black"}) // inherited
		childNew.CascadedProps = map[string]bool{}          // declares nothing
		styles := map[*html.Node]*Style{parent: parentNew, child: childNew}
		prev := map[*html.Node]*Style{parent: mk(map[string]string{"color": "lime"})}
		// old lime -> new black, steps(2,start) => halfway rgb(0, 128, 0)
		ApplyTransitionsFirstFrame(parent, styles, prev)
		if got := parentNew.Properties["color"]; got != "rgb(0, 128, 0)" {
			t.Fatalf("parent color = %q, want %q", got, "rgb(0, 128, 0)")
		}
		if got := childNew.Properties["color"]; got != "rgb(0, 128, 0)" {
			t.Errorf("child color = %q, want inherited transitioned %q", got, "rgb(0, 128, 0)")
		}
	})
}
