package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestOverflowShorthandTwoValueDeterministic reproduces LOU-301: the parser
// pre-expands `overflow: clip visible` into rule.Declarations alongside a
// shorthand remnant, and the cascade re-expands the map entries in Go's
// random iteration order. A lossy remnant ("clip" instead of "clip visible")
// then clobbers overflow-y whenever it iterates after the longhand.
//
// The cascade must produce overflow-x:clip / overflow-y:visible on EVERY
// run, per CSS Overflow 3 §3.1 (two-value shorthand) and §3.2 (visible
// stays visible when paired with clip).
func TestOverflowShorthandTwoValueDeterministic(t *testing.T) {
	const doc = `<!DOCTYPE html>
<style>.container { overflow: clip visible; }</style>
<div class="container"></div>`

	for run := 0; run < 50; run++ {
		d, err := html.Parse(doc)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		styles := ApplyStylesToDocument(d, 800, 600)
		checked := false
		for node, s := range styles {
			if node.Type != html.ElementNode {
				continue
			}
			if cls, _ := node.GetAttribute("class"); cls != "container" {
				continue
			}
			checked = true
			if got := s.GetOverflowX(); got != OverflowClip {
				t.Fatalf("run %d: GetOverflowX() = %v, want clip", run, got)
			}
			if got := s.GetOverflowY(); got != OverflowVisible {
				t.Fatalf("run %d: GetOverflowY() = %v, want visible", run, got)
			}
		}
		if !checked {
			t.Fatal("container element not found in computed styles")
		}
	}
}
