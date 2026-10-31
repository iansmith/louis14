package layout

import (
	"strings"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
)

// TestCollectTextNode_VerticalScriptCloneUpdatesFontOrientation (LOU-425
// review): an all-vertical-script run is collected with a clone of its style
// set to text-orientation: sideways, and the clone's font must follow the new
// orientation. Since LOU-425 the "0" advance is set at font time
// (SimpleFontData::ZeroInlineSize reads the font's orientation), so the clone
// re-builds its font, as Blink's ComputedStyleBuilder::UpdateFontOrientation
// re-creates the FontDescription when the orientation changes
// (core/style/computed_style.cc:3274-3289 @
// 5992657f556bcd9d207a4aca187488462f027cab). Upright serif at 20px has
// ZeroWidth 1em (20); rotated, it is the horizontal "0" advance (under 20).
func TestCollectTextNode_VerticalScriptCloneUpdatesFontOrientation(t *testing.T) {
	parent := css.NewStyle()
	parent.Set("font-family", "serif")
	parent.Set("font-size", "20px")
	parent.Set("writing-mode", "vertical-rl")
	parent.Set("text-orientation", "upright")
	parent.UpdateFont(text.NewCSSFontSelector(text.DefaultFontConfig()))
	if parent.ChWidth != 20 {
		t.Fatalf("upright parent ZeroWidth = %v, want 20 (1em)", parent.ChWidth)
	}

	data := &InlineItemsData{}
	var b strings.Builder
	collectTextNode(&html.Node{Type: html.TextNode, Text: "ᠮᠣᠩᠭᠣᠯ"}, parent, data, &b, nil)

	var got *css.Style
	for _, it := range data.Items {
		if it.Style != nil && it.Style != parent {
			got = it.Style
		}
	}
	if got == nil {
		t.Fatal("no cloned text style collected for the all-vertical-script run")
	}
	if to, _ := got.Get("text-orientation"); to != "sideways" {
		t.Fatalf("clone text-orientation = %q, want sideways", to)
	}
	if got.ChWidth <= 0 || got.ChWidth >= 20 {
		t.Errorf("sideways clone ZeroWidth = %v, want the rotated horizontal \"0\" advance (0 < w < 20), not upright 1em", got.ChWidth)
	}
}
