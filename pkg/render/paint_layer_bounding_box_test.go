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
// plain block child is left out of the visual overflow because it paints as a
// layer of its own, so the walk must still reach it through the block that is
// not self-painting (PaintLayer::ExpandRectForSelfPaintingDescendants,
// core/paint/paint_layer.cc:2181-2223 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestLocalBoundingBox_SelfPaintingUnderNonSelfPaintingChild(t *testing.T) {
	const page = `<!DOCTYPE html><body style="margin:0">` +
		`<div id="f" style="filter:drop-shadow(5px 5px 0 black);width:100px;height:20px">` +
		`<div style="overflow:hidden"><div style="float:left;width:50px;height:200px"></div></div></div>`
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

// TestLocalBoundingBox_ChildTransformExcludesParentPerspective — a child is
// mapped through its own Transform(), not its parent's perspective; and a
// corner with w < 0 divides by w as gfx::Transform::MapRect does
// (paint_layer.cc:2213-2216 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b;
// ui/gfx/geometry/transform.cc:775-786, 1022-1036 @ a9f50e52).
func TestLocalBoundingBox_ChildTransformExcludesParentPerspective(t *testing.T) {
	const page = `<!DOCTYPE html><body style="margin:0"><div style="position:absolute;left:200px;top:200px">` +
		`<div id="f" style="filter:blur(0);perspective:100px;width:100px;height:100px">` +
		`<div style="width:100px;height:100px;transform:%s"></div></div></div>`
	if got, want := boundingBoxOf(t, fmt.Sprintf(page, "translateZ(50px)"), "f"), image.Rect(200, 200, 300, 300); got != want {
		t.Errorf("translateZ under perspective: bounding box = %v, want %v", got, want)
	}
	// w = -1 about origin (200,200): x maps to 400 - x, so the child lands on
	// (100,100)-(200,200).
	if got, want := boundingBoxOf(t, fmt.Sprintf(page, "matrix3d(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,-1);transform-origin:0 0"), "f"), image.Rect(100, 100, 300, 300); got != want {
		t.Errorf("w < 0: bounding box = %v, want %v", got, want)
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
