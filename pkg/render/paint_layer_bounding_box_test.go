package render

import (
	"fmt"
	"image"
	"testing"
)

// layerWithID returns the paint layer whose box is the element with id.
func layerWithID(t *testing.T, root *PaintLayer, id string) *PaintLayer {
	t.Helper()
	var found *PaintLayer
	forEachLayer(root, nil, func(l *PaintLayer, _ []*PaintLayer) {
		if l.Box != nil && l.Box.Node != nil {
			if got, _ := l.Box.Node.GetAttribute("id"); got == id && found == nil {
				found = l
			}
		}
	})
	if found == nil {
		t.Fatalf("no layer for #%s", id)
	}
	return found
}

// boundingBoxOf returns the enclosed
// LocalBoundingBoxIncludingSelfPaintingDescendants of the layer for #id.
func boundingBoxOf(t *testing.T, page, id string) image.Rectangle {
	t.Helper()
	root, _ := layoutAndBuildPaintTree(t, page)
	r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 800, 600)))
	return snapRectFToDevice(r.localBoundingBoxIncludingSelfPaintingDescendants(layerWithID(t, root, id)).ToRectF())
}

// TestLocalBoundingBox_SelfPaintingUnderNonSelfPaintingChild — a float inside a
// plain block child (a louis14 layer that is not self-painting) is left out of the visual overflow because it paints as a
// layer of its own, so the walk must still reach it through the block that is
// not self-painting (PaintLayer::ExpandRectForSelfPaintingDescendants,
// core/paint/paint_layer.cc:2181-2223 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestLocalBoundingBox_SelfPaintingUnderNonSelfPaintingChild(t *testing.T) {
	const page = `<!DOCTYPE html><body style="margin:0">` +
		`<div id="f" style="filter:drop-shadow(5px 5px 0 black);width:100px;height:20px">` +
		`<div><div style="float:left;width:50px;height:200px"></div></div></div>`
	if got, want := boundingBoxOf(t, page, "f"), image.Rect(0, 0, 100, 200); got != want {
		t.Errorf("bounding box = %v, want %v", got, want)
	}
}

// TestLocalBoundingBox_ClipContainsAbsoluteChild — a self-painting
// overflow:hidden child that is its absolute child's containing block clips
// it, so KnownToClipSubtreeToPaddingBox holds there and the absolute box adds
// nothing. An opacity layer is not a containing block, so the absolute box
// escapes its clip and widens the bounding box (paint_layer.cc:2225-2242,
// 394-395, 426-429 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestLocalBoundingBox_ClipContainsAbsoluteChild(t *testing.T) {
	const page = `<!DOCTYPE html><body style="margin:0">` +
		`<div id="f" style="filter:blur(0);position:relative;width:100px;height:100px">` +
		`<div style="overflow:hidden;width:100px;height:100px;%s">` +
		`<div style="position:absolute;left:150px;top:0;width:50px;height:50px"></div></div></div>`
	if got, want := boundingBoxOf(t, fmt.Sprintf(page, "position:relative;z-index:0"), "f"), image.Rect(0, 0, 100, 100); got != want {
		t.Errorf("clip is the containing block: bounding box = %v, want %v", got, want)
	}
	if got, want := boundingBoxOf(t, fmt.Sprintf(page, "opacity:0.5"), "f"), image.Rect(0, 0, 200, 100); got != want {
		t.Errorf("clip is not the containing block: bounding box = %v, want %v", got, want)
	}
}

// TestBoxPaintBounds_MemberExcludesMemberChildren — a 3D context member's
// polygon covers its own content; a member child is a unit with its own
// polygon (PendingLayer bounds, platform/graphics/compositing/
// pending_layer.cc:44-49 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBoxPaintBounds_MemberExcludesMemberChildren(t *testing.T) {
	const page = `<!DOCTYPE html><body style="margin:0"><div style="perspective:500px">` +
		`<div id="m" style="transform-style:preserve-3d;transform:rotateY(10deg);width:100px;height:100px">` +
		`<div style="transform:rotateX(60deg) translateZ(100px);width:300px;height:300px"></div></div></div>`
	root, _ := layoutAndBuildPaintTree(t, page)
	r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 800, 600)))
	if got, want := r.boxPaintBounds(layerWithID(t, root, "m"), true), rectFFromInts(0, 0, 100, 100); got != want {
		t.Errorf("member bounds = %v, want %v", got, want)
	}
}

// TestLocalBoundingBox_OverflowClipMargin — an overflow:clip box with
// overflow-clip-margin keeps the content inside the margin in its visual
// overflow: the clip edge is the border box expanded by
// BorderOutsetsForClipping (LayoutBox::VisualOverflowRect,
// core/layout/layout_box.cc:3702-3716, 3668-3688 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The clip box has no padding, so the
// padding-box margin edge is 20px outside the border box.
func TestLocalBoundingBox_OverflowClipMargin(t *testing.T) {
	const page = `<!DOCTYPE html><body style="margin:0">` +
		`<div id="f" style="position:absolute;left:100px;top:100px;filter:blur(0)">` +
		`<div style="overflow:clip;overflow-clip-margin:20px;width:100px;height:100px">` +
		`<div style="margin-left:-50px;width:200px;height:150px"></div></div></div>`
	if got, want := boundingBoxOf(t, page, "f"), image.Rect(80, 100, 220, 220); got != want {
		t.Errorf("bounding box = %v, want %v", got, want)
	}
}

// TestBoxPaintBounds_NoBox — a layer with no box has empty bounds.
func TestBoxPaintBounds_NoBox(t *testing.T) {
	r := NewRendererForImage(image.NewRGBA(image.Rect(0, 0, 10, 10)))
	for _, member := range []bool{false, true} {
		if got := r.boxPaintBounds(&PaintLayer{}, member); !got.IsEmpty() {
			t.Errorf("member=%v: bounds = %v, want empty", member, got)
		}
	}
}
