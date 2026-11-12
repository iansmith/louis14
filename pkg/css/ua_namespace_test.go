package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestUserAgentStylesApplyOnlyToHTMLElements: the HTML UA stylesheet
// declares `@namespace "http://www.w3.org/1999/xhtml"`, so its rules match
// only HTML elements; an element of another namespace with an HTML tag name
// (createElementNS('not-html', 'button')) gets none of them
// (core/html/resources/html.css @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b;
// WPT css-ui/appearance-auto-non-html-namespace-001).
func TestUserAgentStylesApplyOnlyToHTMLElements(t *testing.T) {
	for _, tag := range []string{"button", "input", "select", "textarea", "meter", "progress", "p", "ul"} {
		htmlStyle := ComputeStyle(&html.Node{Type: html.ElementNode, TagName: tag}, nil, 800, 600, nil)
		foreign := &html.Node{Type: html.ElementNode, TagName: tag}
		foreign.SetNamespaceURI("not-html")
		st := ComputeStyle(foreign, nil, 800, 600, nil)
		if len(htmlStyle.Properties) == 0 {
			continue // no UA rule for this tag to leave out
		}
		for prop, v := range st.Properties {
			t.Errorf("<%s> in a non-HTML namespace gets UA %s: %q", tag, prop, v)
		}
	}
}
