package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestUserAgentInlineDisplayForElementsHTMLCSSLeavesAlone: html.css sets no
// display for these HTML elements, so they take display's initial value,
// inline, not louis14's block fallback for an element with no display
// (core/html/resources/html.css @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b:
// bdo :1812, bdi :1808, ins :1523, del/strike :1564, tt :1539, big :1556,
// nobr :1578 set no display; data, font, acronym, picture, applet and
// keygen appear in no rule). WPT css-ui/text-overflow-028 lays out
// `<bdo dir=rtl>` inline.
func TestUserAgentInlineDisplayForElementsHTMLCSSLeavesAlone(t *testing.T) {
	for _, tag := range []string{"bdo", "bdi", "data", "ins", "del", "tt", "big", "nobr", "strike", "font", "acronym", "picture", "applet", "keygen"} {
		st := ComputeStyle(&html.Node{Type: html.ElementNode, TagName: tag}, nil, 800, 600, nil)
		if got := st.GetDisplay(); got != DisplayInline {
			t.Errorf("<%s> display = %v, want inline", tag, got)
		}
	}
}
