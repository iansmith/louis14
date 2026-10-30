package css

import (
	"testing"

	"louis14/pkg/html"
)

// xhtmlDefaultNamespaceRule declares the XHTML namespace as the sheet's
// default namespace, exactly as selectors/is-default-ns-003.html does.
const xhtmlDefaultNamespaceRule = `@namespace "http://www.w3.org/1999/xhtml";`

// svgContainerDoc is the markup of selectors/is-default-ns-003.html and
// not-default-ns-003.html: an SVG <text class=a> under an SVG
// <g class=container>, plus a <rect class=p> sibling for the :has() rows.
const svgContainerDoc = `<svg><g class=container><rect class=p></rect><text class=a>t</text></g></svg>`

// TestMatchesSelector_NonSubjectCompoundTakesDefaultNamespace pins the
// is-default-ns-003 rule: inside :is()/:where() a compound that is not the
// subject keeps the default namespace even without a type selector, as the
// implicit `ns|*` Blink's PrependTypeSelectorIfNeeded prepends
// (core/css/parser/css_selector_parser.cc:2270-2290 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520); ignore_default_namespace_
// covers only the subject compound (:1310-1322 @ same SHA). So under an
// XHTML default `.container` is `xhtml|*.container` and does not match the
// SVG <g>. The HTML row is the control.
func TestMatchesSelector_NonSubjectCompoundTakesDefaultNamespace(t *testing.T) {
	svgText := firstElement(t, svgContainerDoc, "text")
	htmlSpan := firstElement(t, `<div class=container><span class=a></span></div>`, "span")
	cases := []struct {
		name string
		sel  string
		node *html.Node
		want bool
	}{
		{"is, SVG container", "*|*:is(.container .a)", svgText, false},
		{"where, SVG container", "*|*:where(.container .a)", svgText, false},
		{"is, HTML container", "*|*:is(.container .a)", htmlSpan, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchesSelector(tc.node, sheetSelector(t, xhtmlDefaultNamespaceRule, tc.sel)); got != tc.want {
				t.Errorf("%s matches <%s> = %v, want %v", tc.sel, tc.node.TagName, got, tc.want)
			}
		})
	}
}

// TestMatchesSelector_TypelessTopLevelCompoundTakesDefaultNamespace pins
// PrependTypeSelectorIfNeeded outside any functional pseudo-class
// (css_selector_parser.cc:2276-2290 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): with a default namespace, `.a`
// is `ns|*.a`, so under an XHTML default it matches an HTML element but not
// an SVG one; `*|*.a` matches both; with no @namespace (DefaultNamespace()
// is `*`, :2244-2249) `.a` matches both.
func TestMatchesSelector_TypelessTopLevelCompoundTakesDefaultNamespace(t *testing.T) {
	svgText := firstElement(t, svgContainerDoc, "text")
	htmlSpan := firstElement(t, `<span class=a></span>`, "span")
	cases := []struct {
		prelude, sel      string
		wantSVG, wantHTML bool
	}{
		{xhtmlDefaultNamespaceRule, ".a", false, true},
		{xhtmlDefaultNamespaceRule, "*|*.a", true, true},
		{"", ".a", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.prelude+" "+tc.sel, func(t *testing.T) {
			sel := sheetSelector(t, tc.prelude, tc.sel)
			if got := MatchesSelector(svgText, sel); got != tc.wantSVG {
				t.Errorf("matches SVG <text class=a> = %v, want %v", got, tc.wantSVG)
			}
			if got := MatchesSelector(htmlSpan, sel); got != tc.wantHTML {
				t.Errorf("matches HTML <span class=a> = %v, want %v", got, tc.wantHTML)
			}
		})
	}
}

// TestMatchesSelector_ResistDefaultNamespaceSites pins one row per place
// Blink sets resist_default_namespace_ (css_selector_parser.cc :is :1627,
// :where :1642, :has :1686, :not :1712 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): there the type-less subject
// compound `.a` ignores the XHTML default namespace and so matches the SVG
// element. :nth-child(of) sets no resist of its own (ConsumeNthChildOfSelectors,
// :2226): at top level its `.a` keeps the default namespace, inside :is()
// it inherits the resist. Rows avoid non-rightmost compounds inside :has()
// and :not(), which louis14 does not yet match (LOU-439).
func TestMatchesSelector_ResistDefaultNamespaceSites(t *testing.T) {
	doc, err := html.Parse(svgContainerDoc)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		sel  string
		node string // tag of the element matched against
		want bool
	}{
		{"*|*:is(.a)", "text", true},
		{"*|*:where(.a)", "text", true},
		{"*|*:not(.a)", "text", false},
		{"*|g:has(.a)", "g", true},
		{"*|g:has(> .a)", "g", true},
		{"*|rect:has(+ .a)", "rect", true},
		{"*|rect:has(~ .a)", "rect", true},
		{"*|*:nth-child(1 of .a)", "text", false},
		{"*|*:is(:nth-child(1 of .a))", "text", true},
	}
	for _, tc := range cases {
		t.Run(tc.sel, func(t *testing.T) {
			node := elementsByTag(doc, tc.node)[0]
			if got := MatchesSelector(node, sheetSelector(t, xhtmlDefaultNamespaceRule, tc.sel)); got != tc.want {
				t.Errorf("%s matches SVG <%s> = %v, want %v", tc.sel, tc.node, got, tc.want)
			}
		})
	}
}
