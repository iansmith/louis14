package layout

import (
	"testing"

	"louis14/pkg/html"
)

// TestBackgroundTransfersToView pins LayoutBoxModelObject::
// BackgroundTransfersToView (layout_box_model_object.cc:1095-1125 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the document element always
// transfers; the first body transfers only when the html root has no
// background and containment applies to neither.
func TestBackgroundTransfersToView(t *testing.T) {
	for _, tc := range []struct {
		name               string
		src                string
		wantRoot, wantBody bool
	}{
		{"body propagates", `<html><body style="background:green">x</body></html>`, true, true},
		{"root has background", `<html style="background:red"><body style="background:green">x</body></html>`, true, false},
		{"root contained", `<html style="contain:paint"><body style="background:green">x</body></html>`, true, false},
		{"body contained", `<html><body style="contain:layout;background:green">x</body></html>`, true, false},
		{"inline body", `<html><body style="display:inline;background:green"><p>x</p></body></html>`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse("<!DOCTYPE html>" + tc.src)
			if err != nil {
				t.Fatal(err)
			}
			root := NewLayoutEngine(200, 200).Layout(doc)[0].LayoutNode
			body := root.FirstBodyElement()
			if body == nil {
				t.Fatal("no body layout object")
			}
			if got := root.BackgroundTransfersToView(); got != tc.wantRoot {
				t.Errorf("root transfers = %v, want %v", got, tc.wantRoot)
			}
			if got := body.BackgroundTransfersToView(); got != tc.wantBody {
				t.Errorf("body transfers = %v, want %v", got, tc.wantBody)
			}
		})
	}
}
