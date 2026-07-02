package js

import (
	"testing"

	"louis14/pkg/html"
)

// findElement returns the first element with the given tag name.
func findElement(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && n.TagName == tag {
		return n
	}
	for _, c := range n.Children {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// TestAnimatePseudoElementMarker is the red test for LOU-358 category C
// (WPT css-pseudo/marker-animate-002.html). Element.animate with
// {pseudoElement: '::marker'} must (a) accept the property-indexed keyframe
// object form ({opacity: [...], color: [...]}, Web Animations §5.2 —
// Blink EffectInput::ParseKeyframesArgument's object branch), and (b) sample
// the playing animation at t=0 into the originating node's
// MarkerAnimatedStyle slot, UNFILTERED — the ::marker validity filter is
// applied later at style resolution, mirroring Blink adding
// CSSProperty::kValidForMarker to the cascade filter only when interpolations
// are applied (StyleResolver::ApplyAnimatedStyle, style_resolver.cc:2626-2636
// @ 43cee02dc59fdad798675a735737510ecf0c9064).
func TestAnimatePseudoElementMarker(t *testing.T) {
	doc := parseHTML(t, `<ul><li>Item</li></ul>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.querySelector('li').animate({
			opacity: [0.1, 0.1],
			color: ['green', 'green']
		}, {
			pseudoElement: '::marker',
			duration: Infinity
		});
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	li := findElement(doc.Root, "li")
	if li == nil {
		t.Fatalf("li not found")
	}
	if li.MarkerAnimatedStyle == nil {
		t.Fatalf("MarkerAnimatedStyle not populated by animate(pseudoElement: '::marker') at t=0")
	}
	if got := li.MarkerAnimatedStyle["color"]; got != "green" {
		t.Errorf("MarkerAnimatedStyle[color] = %q, want %q", got, "green")
	}
	if got := li.MarkerAnimatedStyle["opacity"]; got != "0.1" {
		t.Errorf("MarkerAnimatedStyle[opacity] = %q, want %q (stored unfiltered; validity filtering happens at style resolution)", got, "0.1")
	}
	// The element's own inline style must NOT receive the pseudo-element
	// animation's values.
	if style := li.Attributes["style"]; style != "" {
		t.Errorf("li inline style = %q, want empty (animation targets ::marker, not the element)", style)
	}
}

// TestAnimatePropertyIndexedScalarString covers the scalar-value form of
// property-indexed keyframes: {color: 'green'} is ONE value (a to-frame), not
// an iterable. A goja string object exposes a length property, so a bare
// length check mistakes it for an array and splits it into per-character
// keyframes ("g","r","e","e","n") — CodeRabbit finding on PR #161. Web
// Animations §5.2 (Blink EffectInput::ParseKeyframesArgument's object branch)
// treats a DOMString as a single-value list.
func TestAnimatePropertyIndexedScalarString(t *testing.T) {
	doc := parseHTML(t, `<ul><li>Item</li></ul>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.querySelector('li').animate({
			color: 'green'
		}, {
			pseudoElement: '::marker',
			duration: Infinity
		});
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	li := findElement(doc.Root, "li")
	if li == nil {
		t.Fatalf("li not found")
	}
	if got := li.MarkerAnimatedStyle["color"]; got != "green" {
		t.Errorf("MarkerAnimatedStyle[color] = %q, want %q (scalar string must not be split per character)", got, "green")
	}
}

// TestAnimateElementWithoutCurrentTimeStaysInert pins the existing
// element-level contract: an animate() call WITHOUT pseudoElement and without
// a currentTime assignment does not touch the element (the static harness
// applies element-level animations only on explicit currentTime writes).
func TestAnimateElementWithoutCurrentTimeStaysInert(t *testing.T) {
	doc := parseHTML(t, `<div>x</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		document.querySelector('div').animate([
			{ opacity: 0 }, { opacity: 1 }
		], 1000);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	div := findElement(doc.Root, "div")
	if div == nil {
		t.Fatalf("div not found")
	}
	if style := div.Attributes["style"]; style != "" {
		t.Errorf("div inline style = %q, want empty (no currentTime was set)", style)
	}
	if div.MarkerAnimatedStyle != nil {
		t.Errorf("MarkerAnimatedStyle = %v, want nil for an element-level animation", div.MarkerAnimatedStyle)
	}
}
