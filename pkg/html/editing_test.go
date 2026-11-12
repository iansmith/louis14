package html

import "testing"

func editingTestDoc(t *testing.T, src string) *Document {
	t.Helper()
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

// TestUpdateSelectionOnFocus checks that focusing a root editable element
// collapses the selection at its first position, and that focusing anything
// else leaves the selection alone. Mirrors Element::UpdateSelectionOnFocus
// (core/dom/element.cc:8559-8600 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// SetSelection(Collapse(FirstPositionInOrBeforeNode(*this))) for a root
// editable element, unless the selection is already in it.
func TestUpdateSelectionOnFocus(t *testing.T) {
	doc := editingTestDoc(t, `<div id=plain>a</div>`+
		`<div id=root contenteditable>test<span id=inner>x</span></div>`+
		`<div id=off contenteditable=false>b</div>`)
	root := doc.Root.ElementByID("root")
	inner := doc.Root.ElementByID("inner")

	doc.UpdateSelectionOnFocus(doc.Root.ElementByID("plain"))
	if doc.Selection != nil {
		t.Fatalf("focusing a non-editable element set the selection to %+v", *doc.Selection)
	}
	doc.UpdateSelectionOnFocus(doc.Root.ElementByID("off"))
	if doc.Selection != nil {
		t.Fatalf("focusing a contenteditable=false element set the selection to %+v", *doc.Selection)
	}

	doc.UpdateSelectionOnFocus(root)
	want := Range{StartContainer: root, StartOffset: 0, EndContainer: root, EndOffset: 0}
	if doc.Selection == nil || *doc.Selection != want {
		t.Fatalf("selection after focusing the root editable = %+v, want collapsed at (root, 0)", doc.Selection)
	}

	// A selection already in the root editable element is kept.
	kept := &Range{StartContainer: inner.Children[0], StartOffset: 1, EndContainer: inner.Children[0], EndOffset: 1}
	doc.Selection = kept
	doc.UpdateSelectionOnFocus(root)
	if doc.Selection != kept {
		t.Errorf("focusing the root editable replaced a selection already in it")
	}

	// A nested editable element is not a root editable element.
	doc.Selection = nil
	doc.UpdateSelectionOnFocus(inner)
	if doc.Selection != nil {
		t.Errorf("focusing a non-root editable element set the selection")
	}
}
