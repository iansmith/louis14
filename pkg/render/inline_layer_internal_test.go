package render

import "testing"

// inlineGroupLayer returns the first inline paint group layer in the tree.
func inlineGroupLayer(t *testing.T, root *PaintLayer) *PaintLayer {
	t.Helper()
	var found *PaintLayer
	forEachLayer(root, nil, func(l *PaintLayer, _ []*PaintLayer) {
		if found == nil && l.inlinePaintGroup != nil {
			found = l
		}
	})
	if found == nil {
		t.Fatal("no inline paint group layer")
	}
	return found
}

// will-change: transform on an inline with a paint layer gives the layer's
// box a transform node (CompositingReasonFinder adds will-change reasons to
// an object with a paint layer, compositing_reason_finder.cc:411-419 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestInlineLayer_WillChangeTransformGivesGroupANode(t *testing.T) {
	for _, tc := range []struct {
		style string
		want  bool
	}{
		{"position:relative;will-change:transform", true},
		{"position:relative", false},
	} {
		root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html><span style="`+tc.style+`">x</span>`)
		if got := needsTransformNode(inlineGroupLayer(t, root).Box); got != tc.want {
			t.Errorf("%s: the inline's layer box has a transform node = %v, want %v", tc.style, got, tc.want)
		}
	}
}

// The box standing in for an inline's border box carries the inline's
// border and padding, which mask-clip and content-box derivations inset by.
func TestInlineLayer_LinesBoxCarriesBorderAndPadding(t *testing.T) {
	root, _ := layoutAndBuildPaintTree(t, `<!DOCTYPE html>
<span style="position:relative;border:5px solid;padding:3px">x</span>`)
	b := inlineGroupLayer(t, root).linesOrBorderBox()
	if b.Border.Left != 5 || b.Border.Top != 5 || b.Padding.Left != 3 || b.Padding.Bottom != 3 {
		t.Errorf("border %+v, padding %+v; want 5 and 3 on every side", b.Border, b.Padding)
	}
}
