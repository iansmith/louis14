package js

import (
	"slices"
	"testing"

	"louis14/pkg/html"
)

func styleSheetTexts(doc *html.Document) []string {
	out := make([]string, len(doc.Stylesheets))
	for i, s := range doc.Stylesheets {
		out[i] = s.Text
	}
	return out
}

// TestStyleElementIsScriptable pins that a parsed <style> is a scriptable
// HTMLStyleElement: reachable by getElementById, querySelector and window
// named access, and accepting appendChild (display-first-line-002.html
// appends a display:none <div> into its <style id=styleElm>).
func TestStyleElementIsScriptable(t *testing.T) {
	doc := parseHTML(t, `<!DOCTYPE html><style id="styleElm">p{}</style><div id="child"></div>`)
	doc.Scripts = append(doc.Scripts, `
		if (document.getElementById("styleElm") === null) throw new Error("getElementById");
		if (document.querySelector("style") === null) throw new Error("querySelector");
		if (styleElm.tagName !== "STYLE") throw new Error("tagName " + styleElm.tagName);
		styleElm.appendChild(child);
	`)
	if err := New().Execute(doc); err != nil {
		t.Fatal(err)
	}
	// An element child is not part of the sheet text (TextFromChildren).
	if got := styleSheetTexts(doc); !slices.Equal(got, []string{"p{}"}) {
		t.Errorf("Stylesheets = %q, want [\"p{}\"]", got)
	}
}

// TestStyleElementMutationReprocessesSheet pins that script mutations of
// <style> elements re-derive the author sheets, mirroring
// HTMLStyleElement::InsertedInto / RemovedFrom / ChildrenChanged →
// StyleElement::ProcessStyleSheet / RemovedFrom / ChildrenChanged
// (core/html/html_style_element.cc:71-96 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestStyleElementMutationReprocessesSheet(t *testing.T) {
	doc := parseHTML(t, `<!DOCTYPE html><style id="a">a{}</style><style id="b">b{}</style><div></div>`)
	doc.Scripts = append(doc.Scripts, `
		document.getElementById("a").appendChild(document.createTextNode(" a2{}"));
		document.getElementById("b").remove();
		var s = document.createElement("style");
		s.textContent = "c{}";
		document.head.appendChild(s);
	`)
	if err := New().Execute(doc); err != nil {
		t.Fatal(err)
	}
	want := []string{"a{} a2{}", "c{}"}
	if got := styleSheetTexts(doc); !slices.Equal(got, want) {
		t.Errorf("Stylesheets = %q, want %q", got, want)
	}
}
