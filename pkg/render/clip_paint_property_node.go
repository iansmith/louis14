package render

import (
	"mazarin/textshape"

	"louis14/pkg/graphics"
	"louis14/pkg/layout"
)

// clipNodeKind is the property a ClipPaintPropertyNode comes from.
type clipNodeKind int

const (
	// clipNodeCssClip is the CSS `clip` rect (UpdateCssClip).
	clipNodeCssClip clipNodeKind = iota
	// clipNodeOverflowClip is the overflow clip of a box's contents
	// (UpdateOverflowClip).
	clipNodeOverflowClip
)

// ClipPaintPropertyNode is a clip in the clip tree. Mirrors the subset of
// blink::ClipPaintPropertyNode (platform/graphics/paint/
// clip_paint_property_node.h) that FragmentPaintPropertyTreeBuilder builds
// in UpdateCssClip and UpdateOverflowClip
// (core/paint/paint_property_tree_builder.cc:2560-2575, :3039-3139 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The clip shape is the owner
// layer's (CSSClipRect / applyOverflowClip, rounded corners included); its
// coordinates are in the owner's transform space.
type ClipPaintPropertyNode struct {
	Layer  *PaintLayer
	Kind   clipNodeKind
	Parent *ClipPaintPropertyNode
	// applied counts this node's entries on the Renderer's clipStack.
	applied int
}

// clipTreeBuilder builds the clip tree for one paint, lazily per box: the
// FragmentPaintPropertyTreeBuilder walk that sets context_.current.clip and
// records each fragment's LocalBorderBoxProperties clip
// (UpdateLocalBorderBoxContext, :2756-2779). The box tree parents an
// absolutely or fixed positioned box under its containing block, so walking
// box parents follows Blink's absolute_position / fixed_position clip
// contexts.
type clipTreeBuilder struct {
	layerByBox map[*layout.Box]*PaintLayer
	contents   map[*layout.Box]*ClipPaintPropertyNode // memo; nil = no clip
}

func newClipTreeBuilder(root *PaintLayer) *clipTreeBuilder {
	b := &clipTreeBuilder{
		layerByBox: make(map[*layout.Box]*PaintLayer),
		contents:   make(map[*layout.Box]*ClipPaintPropertyNode),
	}
	var index func(*PaintLayer)
	index = func(l *PaintLayer) {
		if l == nil {
			return
		}
		if l.Box != nil {
			if _, ok := b.layerByBox[l.Box]; !ok {
				b.layerByBox[l.Box] = l
			}
		}
		for _, list := range [][]*PaintLayer{l.NegativeZ, l.FlowChildren, l.FloatChildren, l.AutoZero, l.PositiveZ} {
			for _, c := range list {
				index(c)
			}
		}
	}
	index(root)
	return b
}

// localBorderBoxClip returns the clip box's painting enters under: the
// contents clip of its containing-block parent. This is the clip of
// FragmentData::LocalBorderBoxProperties without the box's own CssClip,
// which paintLayerContent installs under the box's transform as Blink
// orders it (UpdateCssClip follows UpdateTransform in UpdateForSelf).
func (b *clipTreeBuilder) localBorderBoxClip(box *layout.Box) *ClipPaintPropertyNode {
	if b == nil || box == nil || box.Parent == nil {
		return nil
	}
	return b.contentsClip(box.Parent)
}

// contentsClip returns the clip box's descendants paint under: its CSS clip
// and its overflow clip, in that order, over its own LocalBorderBoxProperties
// clip.
func (b *clipTreeBuilder) contentsClip(box *layout.Box) *ClipPaintPropertyNode {
	if clip, ok := b.contents[box]; ok {
		return clip
	}
	clip := b.localBorderBoxClip(box)
	if layer := b.layerByBox[box]; layer != nil && box.Style != nil {
		if layer.HasCSSClip {
			clip = &ClipPaintPropertyNode{Layer: layer, Kind: clipNodeCssClip, Parent: clip}
		}
		if layer.HasClip {
			clip = &ClipPaintPropertyNode{Layer: layer, Kind: clipNodeOverflowClip, Parent: clip}
		}
	}
	b.contents[box] = clip
	return clip
}

// ownClipNode returns the node of the given kind owned by layer, which
// contentsClip created, or nil for a layer outside the indexed tree.
func (b *clipTreeBuilder) ownClipNode(layer *PaintLayer, kind clipNodeKind) *ClipPaintPropertyNode {
	if b == nil || layer.Box == nil {
		return nil
	}
	for n := b.contentsClip(layer.Box); n != nil && n.Layer == layer; n = n.Parent {
		if n.Kind == kind {
			return n
		}
	}
	return nil
}

// applyClipNode installs the clip of the given kind owned by layer on the
// draw context; the caller has Push()ed.
func (r *Renderer) applyClipNode(layer *PaintLayer, kind clipNodeKind) {
	if kind == clipNodeOverflowClip {
		r.applyOverflowClip(layer)
		return
	}
	// The CSS clip rect, in physical coordinates (CSS Writing Modes §7.6).
	cx, cy, cw, ch := pixelSnap(layer.CSSClipRect[0], layer.CSSClipRect[1],
		layer.CSSClipRect[2], layer.CSSClipRect[3])
	r.dc.DrawRectangle(cx, cy, cw, ch)
	// A clip node becomes an anti-aliased ClipRectOp
	// (paint_chunks_to_cc_layer.cc:573-579 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	clipPath(r.dc, graphics.AntiAliased, textshape.ClipOpIntersect)
}

// pushClipNode applies layer's clip of the given kind and records its node
// as the innermost applied clip. Pair with popClipNode.
func (r *Renderer) pushClipNode(layer *PaintLayer, kind clipNodeKind) {
	r.pushClip(layer, kind, r.clipTree.ownClipNode(layer, kind))
}

// pushClip applies layer's clip of the given kind and pushes node.
func (r *Renderer) pushClip(layer *PaintLayer, kind clipNodeKind, node *ClipPaintPropertyNode) {
	r.dc.Push()
	r.applyClipNode(layer, kind)
	r.clipStack = append(r.clipStack, node)
	if node != nil {
		node.applied++
	}
}

// popClipNode undoes pushClipNode.
func (r *Renderer) popClipNode() {
	if node := r.clipStack[len(r.clipStack)-1]; node != nil {
		node.applied--
	}
	r.clipStack = r.clipStack[:len(r.clipStack)-1]
	r.dc.Pop()
}

// enterLocalBorderBoxClip switches the draw context to the clip layer's
// painting enters under — its LocalBorderBoxProperties clip — wherever in
// paint order layer paints. Blink paints each chunk in its own property
// tree state and the compositor walks from the current clip to it
// (PaintChunker / GeometryMapper::SourceToDestinationClipRect); louis14
// paints immediately, so it applies the nodes between the innermost
// applied ancestor and layer's clip, outermost first. A stacking context's
// z-order lists hold layers whose containing-block chain passes through
// clips the context's own brackets did not apply; those are the nodes this
// adds. Clips are applied before layer's transform, in the parent's space.
// Returns the number applied, for exitClipNodes.
func (r *Renderer) enterLocalBorderBoxClip(layer *PaintLayer) int {
	var missing []*ClipPaintPropertyNode
	for n := r.clipTree.localBorderBoxClip(layer.Box); n != nil && n.applied == 0; n = n.Parent {
		missing = append(missing, n)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		r.pushClip(missing[i].Layer, missing[i].Kind, missing[i])
	}
	return len(missing)
}

// exitClipNodes pops n clips enterLocalBorderBoxClip applied.
func (r *Renderer) exitClipNodes(n int) {
	for ; n > 0; n-- {
		r.popClipNode()
	}
}

// localBorderBoxClipContains reports whether node is on the clip chain box's
// painting enters under (its LocalBorderBoxProperties clip and the clips
// above it).
func (b *clipTreeBuilder) localBorderBoxClipContains(box *layout.Box, node *ClipPaintPropertyNode) bool {
	for n := b.localBorderBoxClip(box); n != nil; n = n.Parent {
		if n == node {
			return true
		}
	}
	return false
}

// paintZOrderChild paints a z-order child of layer while layer's overflow clip
// node overflowClip is the innermost applied clip (paintLayerContent's
// bracket; nil when layer does not clip). In Blink each
// paint chunk carries its own clip state, and an out-of-flow box takes its
// clip from its containing block (UpdateOutOfFlowContext and
// SwitchToOOFContext, core/paint/paint_property_tree_builder.cc:3617-3622,
// 3750-3754 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); a stacking context's
// z-order lists can hold such a box whose containing block is outside it. When
// the child's clip chain does not pass through layer's overflow clip, the
// clip is lifted while the child paints. The child stays inside layer's effect
// (opacity, filter), which is not a clip.
func (r *Renderer) paintZOrderChild(layer, child *PaintLayer, overflowClip *ClipPaintPropertyNode) {
	if overflowClip != nil && child.Box != nil && !r.clipTree.localBorderBoxClipContains(child.Box, overflowClip) {
		r.popClipNode()
		defer r.pushClip(layer, clipNodeOverflowClip, overflowClip)
	}
	r.paintLayer(child)
}
