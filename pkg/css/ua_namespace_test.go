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
			// display: inline is the CSS initial value, not an html.css
			// rule; louis14 stamps it because GetDisplay falls back to
			// block (LOU-846).
			if prop == "display" && v == "inline" {
				continue
			}
			t.Errorf("<%s> in a non-HTML namespace gets UA %s: %q", tag, prop, v)
		}
	}
}

// TestForeignElementsKeepTheInitialDisplay: an SVG or MathML element (or
// any element outside the HTML namespace) takes display's initial value,
// inline, like any element html.css doesn't style: inline <svg> and <math>
// sit on the line (ComputedStyleInitialValues::InitialDisplay @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestForeignElementsKeepTheInitialDisplay(t *testing.T) {
	for _, tc := range []struct{ tag, ns string }{
		{"svg", html.SVGNamespaceURI}, {"rect", html.SVGNamespaceURI},
		{"math", html.MathMLNamespaceURI}, {"mi", html.MathMLNamespaceURI},
		{"foo", "not-html"}, {"div", "not-html"},
	} {
		n := &html.Node{Type: html.ElementNode, TagName: tc.tag}
		n.SetNamespaceURI(tc.ns)
		if got := ComputeStyle(n, nil, 800, 600, nil).GetDisplay(); got != DisplayInline {
			t.Errorf("<%s> in %s: display = %v, want inline", tc.tag, tc.ns, got)
		}
	}
}
