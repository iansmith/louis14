package js

import "testing"

// TestSelectNodeContentsAddRange mirrors the WPT idiom shared by all 26
// LOU-344 target tests (see support/selections.js's selectNodeContents,
// vendored from web-platform-tests/wpt):
//
//	const range = document.createRange();
//	range.selectNodeContents(node);
//	getSelection().addRange(range);
//
// After running, the engine's current selection should span all of node's
// children — Blink's Range::selectNodeContents sets startContainer/
// startOffset to (node, 0) and endContainer/endOffset to (node,
// node.childNodes.length) (core/dom/range.cc SelectNodeContents).
func TestSelectNodeContentsAddRange(t *testing.T) {
	doc := parseHTML(t, `<div id="test">Selected Text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var node = document.getElementById("test");
		var range = document.createRange();
		range.selectNodeContents(node);
		window.getSelection().addRange(range);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	sel := engine.CurrentSelection()
	if sel == nil {
		t.Fatal("CurrentSelection() returned nil after addRange")
	}
	testNode := getElementById(doc.Root, "test")
	if sel.StartContainer != testNode {
		t.Errorf("StartContainer = %v, want the #test div", sel.StartContainer)
	}
	if sel.StartOffset != 0 {
		t.Errorf("StartOffset = %d, want 0", sel.StartOffset)
	}
	if sel.EndContainer != testNode {
		t.Errorf("EndContainer = %v, want the #test div", sel.EndContainer)
	}
	if sel.EndOffset != len(testNode.Children) {
		t.Errorf("EndOffset = %d, want %d (child count)", sel.EndOffset, len(testNode.Children))
	}
}

// TestRangeConstructor covers `new Range()` (selection-text-decoration-currentcolor.html
// uses this form instead of document.createRange()) plus explicit
// setStart/setEnd on a text node, mirroring createRangeForTextOnly in the
// vendored support/selections.js.
func TestRangeConstructor(t *testing.T) {
	doc := parseHTML(t, `<p id="target">Selected text</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var range = new Range();
		var el = document.getElementById("target");
		var textNode = el.firstChild;
		range.setStart(textNode, 2);
		range.setEnd(textNode, 5);
		getSelection().addRange(range);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	sel := engine.CurrentSelection()
	if sel == nil {
		t.Fatal("CurrentSelection() returned nil after addRange")
	}
	target := getElementById(doc.Root, "target")
	textNode := target.Children[0]
	if sel.StartContainer != textNode || sel.StartOffset != 2 {
		t.Errorf("start = (%v, %d), want (text node, 2)", sel.StartContainer, sel.StartOffset)
	}
	if sel.EndContainer != textNode || sel.EndOffset != 5 {
		t.Errorf("end = (%v, %d), want (text node, 5)", sel.EndContainer, sel.EndOffset)
	}
}

// TestRemoveAllRanges mirrors selectRangeWith's removeAllRanges() call before
// adding a fresh range — the engine must clear any prior selection so a
// second addRange fully replaces rather than appending (louis14 only
// supports a single Range per Selection, matching every target test's usage,
// not full multi-range Selection — see Selection::addRange in
// third_party/blink/renderer/core/editing/frame_selection.h, which this
// ticket deliberately doesn't fully mirror; see LOU-344 ticket "Theory of
// root cause" for the single-highlight-type scope cut).
func TestRemoveAllRanges(t *testing.T) {
	doc := parseHTML(t, `<div id="a">AAA</div><div id="b">BBB</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var selection = getSelection();
		var r1 = document.createRange();
		r1.selectNodeContents(document.getElementById("a"));
		selection.addRange(r1);
		selection.removeAllRanges();
		var r2 = document.createRange();
		r2.selectNodeContents(document.getElementById("b"));
		selection.addRange(r2);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	sel := engine.CurrentSelection()
	if sel == nil {
		t.Fatal("CurrentSelection() returned nil")
	}
	bNode := getElementById(doc.Root, "b")
	if sel.StartContainer != bNode {
		t.Errorf("StartContainer = %v, want #b (removeAllRanges should have cleared r1)", sel.StartContainer)
	}
}

// TestActiveElementDefaultsToBody covers document.activeElement, which
// support/selections.js's selectNodeContents reads to decide whether to
// blur() after selecting. Per HTML5 §6.5.1 "the activeElement attribute",
// it defaults to the body element when nothing has focus.
func TestActiveElementDefaultsToBody(t *testing.T) {
	doc := parseHTML(t, `<body><div id="test">hi</div></body>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		if (document.activeElement === null || document.activeElement === undefined) {
			throw new Error("activeElement should default to body, got " + document.activeElement);
		}
		if (document.activeElement.tagName !== "BODY") {
			throw new Error("activeElement.tagName = " + document.activeElement.tagName);
		}
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestActiveElementTracksFocus confirms document.activeElement reflects the
// last .focus()ed element, and reverts once .blur() is called — mirrors
// Blink's Document::FocusedElement(), which louis14 already maintains via
// doc.FocusedElement (see Element.focus()/.blur() bindings in dom.go).
func TestActiveElementTracksFocus(t *testing.T) {
	doc := parseHTML(t, `<body><div id="test" tabindex="0">hi</div></body>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("test");
		el.focus();
		if (document.activeElement !== el) {
			throw new Error("activeElement did not track focus()");
		}
		el.blur();
		if (document.activeElement.tagName !== "BODY") {
			throw new Error("activeElement did not revert to body after blur(): " + document.activeElement.tagName);
		}
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
