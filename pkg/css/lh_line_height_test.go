package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestLhUnit_LineHeightResolvesAgainstParent: lh in line-height resolves
// against the parent's computed line-height (CSS Values 4 §6.1.1), as in
// font-size. Blink builds CSSToLengthConversionData's LineHeightSize from the
// parent style and only switches to the element's own after applying
// line-height (StyleCascade::Apply → StyleResolverState::UpdateLineHeight,
// style_cascade.cc:309-315 @ 5992657f556bcd9d207a4aca187488462f027cab).
// WPT css-values/lh-unit-001.html.
func TestLhUnit_LineHeightResolvesAgainstParent(t *testing.T) {
	doc, _ := html.Parse(`
		<style>
			div { font-size: 50px; line-height: 1; }
			aside { font-size: 42px; line-height: 2lh; }
			span { font-size: 10px; }
		</style>
		<div><aside><span>x</span></aside></div>
	`)
	styles := ApplyStylesToDocument(doc, 800, 600)

	for node, style := range styles {
		switch node.TagName {
		case "aside", "span":
			// span inherits aside's computed (absolute) line-height.
			if got := style.GetLineHeight(); got != 100 {
				t.Errorf("<%s> line-height = %v, want 100 (2 × parent's 50px)", node.TagName, got)
			}
		}
	}
}
