package layout

import "testing"

// TestFragmentToBox_MarksLineBoxes: a text box's parent is the line box
// fragment it sits in (Blink's PhysicalLineBoxFragment, kFragmentLineBox,
// core/layout/inline/physical_line_box_fragment.cc:41-46 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the box says so, so paint
// can find a text fragment's containing line as InlineCursor::
// MoveToContainingLine does.
func TestFragmentToBox_MarksLineBoxes(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div style="font: 16px serif; line-height: 40px">ab<br>cd</div>`)
	for _, b := range collectTextBoxes(root, nil) {
		if b.Parent == nil || !b.Parent.IsLineBox {
			t.Errorf("%q: parent is not marked as a line box", b.Text)
		} else if b.Parent.Height != 40 {
			t.Errorf("%q: line box height = %v, want 40", b.Text, b.Parent.Height)
		}
	}
}
