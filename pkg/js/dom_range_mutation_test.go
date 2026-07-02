package js

import (
	"testing"

	"louis14/pkg/html"
)

// TestCharacterDataInsertData — LOU-359 Category E red test for WPT
// first-letter-punctuation-dynamic.html: `target.firstChild.insertData(0,
// 'A')` prepends to the text node's data. Mirrors CharacterData::insertData
// (WHATWG DOM §4.10; Blink core/dom/character_data.cc @ Chromium
// 906a32d8634edf17db094507908f439bd9b52de5). The mutation must also keep
// html.Node.RawText coherent — after a script mutation the DOM data IS the
// original text (Blink has no separate parser-raw copy), and the
// ::first-letter scanner reads RawText in preserved-white-space contexts.
func TestCharacterDataInsertData(t *testing.T) {
	doc := parseHTML(t, `<div id="target"> .<span>B</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var target = document.getElementById("target");
		target.firstChild.insertData(0, 'A');
		if (target.firstChild.data !== 'A .') throw new Error("data: " + JSON.stringify(target.firstChild.data));
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	target := getElementById(doc.Root, "target")
	txt := target.Children[0]
	if txt.Type != html.TextNode || txt.Text != "A ." {
		t.Errorf("Go-side Text = %q, want %q", txt.Text, "A .")
	}
	if txt.RawText != "A ." {
		t.Errorf("Go-side RawText = %q, want %q (mutation must keep RawText coherent)", txt.RawText, "A .")
	}
}

// TestSelectionSurroundContentsAfterNodeRemoval — LOU-359 Category E red
// test for WPT first-letter-of-html-root-refcrash.html. Exercises:
//   - Selection#selectAllChildren (Blink core/editing/dom_selection.cc
//     DOMSelection::selectAllChildren @ 906a32d8...)
//   - live-range boundary adjustment when a node is removed (WHATWG DOM
//     §4.2.3 "remove" steps 12-16; Blink Document::NodeWillBeRemoved →
//     Range boundary fixup, core/dom/document.cc + core/dom/range.cc @
//     906a32d8...)
//   - Range#surroundContents (WHATWG DOM §4.10; Blink Range::surroundContents,
//     core/dom/range.cc @ 906a32d8...)
//
// The WPT flow: select all children of <html> (head+body), remove body (range
// end must shrink 2 -> 1), append a "PASS" text node to <html>, then
// surroundContents(styleEl) must wrap ONLY the head into the style element,
// leaving "PASS" as a rendered child of <html>.
// Note: louis14's parser extracts <style> elements into doc.Stylesheets
// rather than keeping them as DOM nodes, so the WPT file's `sheet` element
// is not scriptable here; a synthetic <div> child of <head> stands in as the
// wrap target — same shape (newParent lives INSIDE the extracted subtree).
func TestSelectionSurroundContentsAfterNodeRemoval(t *testing.T) {
	doc := parseHTML(t, `<html><head></head><body>FAIL</body></html>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var head = document.head;
		var wrapper = document.createElement("div");
		wrapper.appendChild(document.createTextNode("css text placeholder"));
		head.appendChild(wrapper);
		var sel = window.getSelection();
		sel.selectAllChildren(document.documentElement);
		var range = sel.getRangeAt(0);
		document.body.remove();
		document.documentElement.appendChild(document.createTextNode("PASS"));
		range.surroundContents(wrapper);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	// Find the html element.
	var htmlEl *html.Node
	var findHTML func(n *html.Node)
	findHTML = func(n *html.Node) {
		if htmlEl != nil {
			return
		}
		if n.Type == html.ElementNode && n.TagName == "html" {
			htmlEl = n
			return
		}
		for _, c := range n.Children {
			findHTML(c)
		}
	}
	findHTML(doc.Root)
	if htmlEl == nil {
		t.Fatal("no html element")
	}

	if len(htmlEl.Children) != 2 {
		var tags []string
		for _, c := range htmlEl.Children {
			tags = append(tags, c.TagName+"/"+c.Text)
		}
		t.Fatalf("html children = %d (%v), want 2 (wrapper div, PASS-text)", len(htmlEl.Children), tags)
	}
	wrapEl := htmlEl.Children[0]
	if wrapEl.TagName != "div" {
		t.Errorf("first child = %q, want the wrapper div", wrapEl.TagName)
	}
	// surroundContents empties the wrapper (its placeholder text goes away)
	// and re-parents the extracted head (which held the wrapper before)
	// into it.
	if len(wrapEl.Children) != 1 || wrapEl.Children[0].TagName != "head" {
		t.Errorf("wrapper children = %v, want the extracted head", wrapEl.Children)
	}
	if wrapEl.Children[0].Parent != wrapEl {
		t.Error("extracted head's Parent pointer not re-targeted to the wrapper")
	}
	passText := htmlEl.Children[1]
	if passText.Type != html.TextNode || passText.Text != "PASS" {
		t.Errorf("second child = %q/%q, want PASS text node", passText.TagName, passText.Text)
	}
}

// TestInsertDataRejectsOutOfRangeOffsets — CodeRabbit PR#160 finding:
// CharacterData.insertData must reject offsets outside [0, length] (WHATWG
// DOM §4.10 "replace data" step 2 throws IndexSizeError; louis14's accessor
// convention is a no-op return instead of throwing). Without the guard,
// UTF16OffsetToByteOffset's clamping turned insertData(-1,…) into a prepend
// and insertData(999,…) into an append.
func TestInsertDataRejectsOutOfRangeOffsets(t *testing.T) {
	doc := parseHTML(t, `<div id="target">ab</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var txt = document.getElementById("target").firstChild;
		txt.insertData(99, 'X');
		if (txt.data !== 'ab') throw new Error("offset 99 mutated: " + txt.data);
		txt.insertData(-1, 'X');
		if (txt.data !== 'ab') throw new Error("offset -1 mutated: " + txt.data);
		txt.insertData(2, 'X'); // == length: valid append point
		if (txt.data !== 'abX') throw new Error("offset 2 failed: " + txt.data);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestLiveRangeAdjustedOnceWhenAlsoSelection — CodeRabbit PR#160 finding: a
// Range created via document.createRange() is registered in
// Document.LiveRanges; Selection#addRange stores the SAME pointer in
// Document.Selection. Node removal must adjust that range exactly once —
// the double walk decremented parent-boundary offsets twice.
func TestLiveRangeAdjustedOnceWhenAlsoSelection(t *testing.T) {
	// Three children under #c so the range end sits TWO positions past the
	// removed child — a single decrement (2->... wait, 3->2) is correct and
	// a double decrement (3->1) is the bug. (With the end just one past the
	// removed index the second pass is masked: offset == idx stops it.)
	doc := parseHTML(t, `<div id="c"><span>a</span><span>b</span><span>d</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var c = document.getElementById("c");
		var range = document.createRange();
		range.selectNodeContents(c);    // (c,0)-(c,3)
		getSelection().addRange(range); // same pointer now in Selection too
		c.firstChild.remove();          // idx 0: end must become (c,2)
		if (range.endOffset !== 2) throw new Error("endOffset: " + range.endOffset);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestSurroundContentsRejectsInvalidOffsets — CodeRabbit PR#160 finding:
// out-of-range boundary offsets must not be clamped into a different valid
// range and then mutated; the unsupported-shape convention is a no-op.
func TestSurroundContentsRejectsInvalidOffsets(t *testing.T) {
	doc := parseHTML(t, `<div id="c"><span id="a">x</span><i id="b">y</i></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var c = document.getElementById("c");
		var range = document.createRange();
		range.setStart(c, 0);
		range.setEnd(c, 99); // invalid: c has 2 children
		var w = document.createElement("div");
		range.surroundContents(w);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	c := getElementById(doc.Root, "c")
	if len(c.Children) != 2 || c.Children[0].TagName != "span" || c.Children[1].TagName != "i" {
		t.Fatalf("invalid-offset surroundContents mutated the tree: %d children", len(c.Children))
	}
}

// TestSurroundContentsClearsEmptiedChildParents — CodeRabbit PR#160 finding:
// the "empty newParent" step drops its old children; their Parent pointers
// must be cleared (DOM "replace all" leaves removed children parentless),
// not left dangling at newParent.
func TestSurroundContentsClearsEmptiedChildParents(t *testing.T) {
	doc := parseHTML(t, `<div id="c"><span id="w"><b id="old">x</b></span><i id="keep">y</i></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var c = document.getElementById("c");
		var w = document.getElementById("w");
		var range = document.createRange();
		range.setStart(c, 1);
		range.setEnd(c, 2); // contains only <i id=keep>
		range.surroundContents(w);
	`)
	// Capture the to-be-emptied child's node BEFORE the script drops it
	// from the tree (afterwards it is unreachable by tree walk).
	old := getElementById(doc.Root, "old")
	if old == nil {
		t.Fatal("setup: #old not found")
	}
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	w := getElementById(doc.Root, "w")
	if w == nil {
		t.Fatal("wrapper not in tree after surroundContents")
	}
	if len(w.Children) != 1 || w.Children[0].TagName != "i" {
		t.Fatalf("wrapper children = %v, want the extracted <i>", w.Children)
	}
	if old.Parent != nil {
		t.Errorf("emptied child's Parent = <%s>, want nil (stale pointer)", old.Parent.TagName)
	}
}
