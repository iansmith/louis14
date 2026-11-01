package layout

import (
	"fmt"
	"testing"

	"louis14/pkg/html"
)

// TestFrameOwner_LoadsNestedDocumentOnceAndReusesIt pins that an <iframe> /
// <object> loads its content document once and later layout passes re-style
// and re-lay-out that same document instead of re-fetching and re-parsing
// it, so mutations made to it (iframe.contentDocument) survive. Blink ties
// the load to DOM insertion, not layout
// (HTMLFrameElementBase::DidNotifySubtreeInsertionsToDocument,
// core/html/html_frame_element_base.cc:185-196), and re-attaching the
// layout tree re-uses the existing content frame
// (HTMLFrameElementBase::AttachLayoutTree, :198-203;
// HTMLPlugInElement::AttachLayoutTree, core/html/html_plugin_element.cc:209-237:
// "We do not want to reload the frame if we don't have to"). All @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
//
// Each pass uses a fresh LayoutEngine, as the visualtest harness does after
// scripts run, and the owner's display changes between passes (the
// css-display/display-change-*iframe.html scenario).
func TestFrameOwner_LoadsNestedDocumentOnceAndReusesIt(t *testing.T) {
	const child = `<!doctype html><style>body { margin: 0 }</style><div id="square" style="width:100px; height:100px"></div>`
	for _, tc := range []struct{ name, owner string }{
		{"iframe", `<iframe id="victim" src="child.html" style="width:200px; height:200px; border:none"></iframe>`},
		{"object", `<object id="victim" data="child.html" style="width:200px; height:200px"></object>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetches := 0
			layoutPass := func(doc *html.Document) map[*html.Node]*Box {
				e := NewLayoutEngine(400, 400)
				e.SetDocumentFetcher(func(uri string) (string, error) {
					if uri != "child.html" {
						return "", fmt.Errorf("no document %q", uri)
					}
					fetches++
					return child, nil
				})
				e.Layout(doc)
				return e.NodeBoxes()
			}

			doc, err := html.Parse(`<!doctype html><body>` + tc.owner + `</body>`)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			owner := elementByID(doc.Root, "victim")
			layoutPass(doc)
			nested := owner.NestedDocument
			if nested == nil {
				t.Fatal("first layout left no NestedDocument on the owner")
			}

			// Script-style mutation of the content document, then a
			// display change on the owner (a layout-tree re-attach).
			square := elementByID(nested.Root, "square")
			square.Attributes["style"] = "width:50px; height:100px"
			owner.Attributes["style"] += "; display:block"

			boxes := layoutPass(doc)
			if fetches != 1 {
				t.Errorf("content document fetched %d times across two layouts, want 1", fetches)
			}
			if owner.NestedDocument != nested {
				t.Error("second layout replaced the owner's NestedDocument")
			}
			if box := boxes[elementByID(owner.NestedDocument.Root, "square")]; box == nil {
				t.Error("second layout produced no box for the content document's #square")
			} else if box.Width != 50 {
				t.Errorf("#square width = %v after mutation + re-layout, want 50", box.Width)
			}
		})
	}
}

// TestFrameOwner_ReloadsWhenSourceChanges pins the other half: a changed
// src / srcdoc loads a new content document, as
// HTMLFrameElementBase::ParseAttribute → SetLocation → OpenURL does
// (core/html/html_frame_element_base.cc:90-118, :205-210 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFrameOwner_ReloadsWhenSourceChanges(t *testing.T) {
	docs := map[string]string{
		"a.html": `<div id="a"></div>`,
		"b.html": `<div id="b"></div>`,
	}
	layoutPass := func(doc *html.Document) {
		e := NewLayoutEngine(400, 400)
		e.SetDocumentFetcher(func(uri string) (string, error) {
			if c, ok := docs[uri]; ok {
				return c, nil
			}
			return "", fmt.Errorf("no document %q", uri)
		})
		e.Layout(doc)
	}
	doc, err := html.Parse(`<!doctype html><body><iframe id="victim" src="a.html"></iframe></body>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	owner := elementByID(doc.Root, "victim")
	for _, step := range []struct {
		attr, value, wantID string
	}{
		{"", "", "a"},
		{"src", "b.html", "b"},
		{"srcdoc", `<div id="s1"></div>`, "s1"},
		{"srcdoc", `<div id="s2"></div>`, "s2"},
	} {
		if step.attr != "" {
			owner.Attributes[step.attr] = step.value
		}
		layoutPass(doc)
		if owner.NestedDocument == nil || elementByID(owner.NestedDocument.Root, step.wantID) == nil {
			t.Errorf("after setting %s=%q: content document lacks #%s", step.attr, step.value, step.wantID)
		}
	}
}

// elementByID returns the first element under root whose id is id, or nil.
func elementByID(root *html.Node, id string) *html.Node {
	if root == nil {
		return nil
	}
	if v, _ := root.GetAttribute("id"); v == id && root.Type == html.ElementNode {
		return root
	}
	for _, c := range root.Children {
		if n := elementByID(c, id); n != nil {
			return n
		}
	}
	return nil
}
