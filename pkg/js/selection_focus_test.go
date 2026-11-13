package js

import "testing"

// TestSelectionFocusesEditableRoot pins FrameSelection::SetFocusedNodeIfNeeded
// (core/editing/frame_selection.cc:1221-1246, called for a non-empty
// selection from DidSetSelectionDeprecated :360-361 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a selection the Selection API
// sets (DomSelection::UpdateFrameSelection passes no DoNotSetFocus,
// core/editing/dom_selection.cc:76-95, same SHA) focuses its root editable
// element. A selection outside any editable element leaves focus alone.
func TestSelectionFocusesEditableRoot(t *testing.T) {
	doc := parseHTML(t, `<body><div id="plain">plain</div><div id="ed" contenteditable="true">edit <b id="inner">me</b></div></body>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var ed = document.getElementById("ed"), plain = document.getElementById("plain");
		var r = document.createRange();
		r.selectNodeContents(plain);
		window.getSelection().addRange(r);
		if (document.activeElement !== document.body) throw new Error("a selection outside an editable root moved focus to " + document.activeElement.tagName);
		var r2 = document.createRange();
		r2.selectNodeContents(document.getElementById("inner"));
		window.getSelection().removeAllRanges();
		window.getSelection().addRange(r2);
		if (document.activeElement !== ed) throw new Error("addRange inside a contenteditable did not focus its root");
		if (!ed.matches(":focus")) throw new Error("the editable root does not match :focus");
		ed.blur();
		window.getSelection().collapse(document.getElementById("inner").firstChild, 1);
		if (document.activeElement !== ed) throw new Error("collapse inside a contenteditable did not focus its root");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
