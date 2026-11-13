package css

import (
	"strings"
	"testing"

	"louis14/pkg/html"
)

// A pseudo-element's style is matched with its originating element's
// StyleScopeFrame, as Blink recalcs a pseudo-element within that element's
// frame (Element::RecalcStyle, core/dom/element.cc:5126-5167 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So computing every element's
// ::before in a chain reuses the activations cached on the ancestors'
// frames: activation computations grow linearly with the depth, not as its
// square. Counts runs, not time (as TestTableCellFinalLayoutNestedTablesStayLinear).
func TestPseudoElementScopeMatchingReusesFrames(t *testing.T) {
	counts := map[int]int64{}
	for _, depth := range []int{10, 20, 40} {
		src := `<style>@scope (.s) { .t::before { content: "x"; color: blue } }</style><div class="s">` +
			strings.Repeat(`<div class="t">`, depth) + strings.Repeat(`</div>`, depth) + `</div>`
		doc, err := html.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		styles := ApplyStylesToDocument(doc, 800, 600)
		sheets := ParseDocumentStylesheets(doc)
		chain := elementsByTag(doc, "div")[1:] // the .t chain inside .s
		if len(chain) != depth {
			t.Fatalf("depth %d: found %d .t elements", depth, len(chain))
		}
		before := activationsComputed.Load()
		for _, n := range chain {
			if c, _ := ComputePseudoElementStyle(n, "before", sheets, 800, 600, styles[n]).Get("color"); c != "blue" {
				t.Fatalf("depth %d: ::before color = %q, want blue (scoped rule applies)", depth, c)
			}
		}
		counts[depth] = activationsComputed.Load() - before
	}
	if counts[40]-counts[20] > 2*(counts[20]-counts[10])+2 {
		t.Errorf("activation computations for depths 10, 20, 40 = %d, %d, %d: grow faster than linearly", counts[10], counts[20], counts[40])
	}
}
