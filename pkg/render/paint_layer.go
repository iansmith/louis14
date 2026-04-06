package render

import (
	"math"
	"sort"
	"strconv"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// PaintLayer represents a node in the pre-paint tree.
// Built from the layout Box tree before painting, PaintLayers pre-sort
// children by CSS 2.1 Appendix E stacking order and pre-compute
// all paint-relevant properties so draw methods never access Style.
//
// Boxes without Style (line boxes, text runs) do NOT get PaintLayers.
// They remain reachable via their parent PaintLayer's Box.Children
// and are painted inline during the parent's paint.
//
// Mirrors Blink's PaintLayer / PrePaintTreeWalk.
type PaintLayer struct {
	Box      *layout.Box
	Position css.PositionType // Always set (PositionStatic for unstyled)
	ZIndex   int

	// Stacking children (only populated for stacking context roots):
	NegativeZ []*PaintLayer // z < 0, sorted ascending
	AutoZero  []*PaintLayer // z-index:auto positioned + z-index:0 SCs, tree order
	PositiveZ []*PaintLayer // z > 0, sorted ascending

	// Non-positioned children in DOM order (Appendix E steps 3-5):
	FlowChildren []*PaintLayer

	// Overflow clip (pre-computed from Style):
	HasClip  bool
	ClipRect [4]float64 // x, y, w, h of padding box

	// CSS clip: rect() (purely physical, per CSS Writing Modes §7.6):
	HasCSSClip  bool
	CSSClipRect [4]float64 // x, y, w, h of clip region

	// Pre-computed paint properties — no Style access needed during paint.

	// Compositing:
	Visible bool    // false = visibility:hidden, skip subtree
	Opacity float64 // 0.0..1.0; 1.0 = fully opaque

	// Image (for <img> replaced elements):
	ImageSrc       string         // src attribute value; empty if not an img element
	ObjectFit      css.ObjectFit  // fill, contain, cover, none, scale-down
	ObjectPosition [2]float64     // x%, y% in range [0,1]; default (0.5, 0.5)

	// Background:
	BackgroundColor    css.Color               // A==0 means no background
	BackgroundImage    string                  // URL from background-image; empty if none
	BackgroundGradient string                  // raw gradient value (linear-gradient(...) etc); empty if none
	BackgroundRepeat   css.BackgroundRepeatType // repeat mode
	BackgroundPosition css.BackgroundPosition   // position offsets
	BackgroundSize     css.BackgroundSize       // background-size (cover/contain/explicit)
	BackgroundClip     css.BackgroundClipType   // border-box, padding-box, content-box

	// Borders: indices 0=Top, 1=Right, 2=Bottom, 3=Left
	BorderColors [4]css.Color
	BorderStyles [4]css.BorderStyle
	BorderRadius [4]float64 // TopLeft, TopRight, BottomRight, BottomLeft (px, circular)

	// Box shadows (outset and inset):
	BoxShadows []css.BoxShadow

	// Outline (doesn't affect layout, drawn outside border-box):
	OutlineStyle  string  // none, solid, dashed, dotted, double
	OutlineWidth  float64
	OutlineColor  css.Color
	OutlineOffset float64

	// Text:
	TextColor     css.Color
	FontSize      float64
	FontBold      bool
	FontItalic    bool
	FontMono      bool
	FontAhem       bool
	FontFamily     string
	LetterSpacing  float64
	WordSpacing    float64
	IsVerticalText bool
	IsSidewaysLR   bool
	IsSidewaysRL   bool

	// Text decoration (underline, overline, line-through):
	TextDecoration          css.TextDecoration
	TextDecorationColor     css.Color  // defaults to TextColor (currentColor)
	TextDecorationThickness float64    // defaults to ~1px
	TextDecorationStyle     string     // solid, double, dotted, dashed, wavy

	// Text shadows:
	TextShadows []css.TextShadow

	// CSS text-transform (uppercase, lowercase, capitalize):
	TextTransform css.TextTransform

	// CSS text-overflow (clip or ellipsis):
	TextOverflow css.TextOverflowType

	// List markers:
	IsListItem    bool
	ListStyleType css.ListStyleType
	ListItemIndex int // 1-based ordinal for ordered lists

	// CSS Transforms:
	Transforms      []css.Transform
	TransformOrigin [2]float64 // resolved to px: (origin-x, origin-y)
	HasTransform    bool

	// CSS Filters:
	Filters   []css.FilterFunction
	HasFilter bool

	// CSS clip-path:
	ClipPath *css.ClipPath // nil = no clip-path

	// CSS mix-blend-mode:
	BlendMode css.MixBlendMode

	// PaintsCanvasBackground is true for the root element (or body when
	// background propagates). Per CSS 2.1 §14.2, the root element's background
	// paints the entire canvas, not just its own box.
	PaintsCanvasBackground bool
}

// BuildPaintTree constructs a PaintLayer tree from a layout Box tree.
// The root box is always treated as a stacking context root
// (CSS 2.1 Appendix E: root element establishes the initial stacking context).
func BuildPaintTree(root *layout.Box) *PaintLayer {
	if root == nil {
		return nil
	}
	rootLayer := newPaintLayer(root)
	buildPaintSubtree(root, rootLayer, rootLayer)
	rootLayer.sortZLists()
	return rootLayer
}

func newPaintLayer(box *layout.Box) *PaintLayer {
	layer := &PaintLayer{
		Box:      box,
		Position: css.PositionStatic,
		Visible:  true,
		Opacity:  1.0,
	}
	s := box.Style
	if s == nil {
		// Unstyled root (rare): no paint properties to compute.
		return layer
	}

	layer.Position = box.Position
	if layer.Position == "" {
		layer.Position = css.PositionStatic
	}
	layer.ZIndex = box.ZIndex

	// Overflow clip.
	overflow := s.GetOverflow()
	if overflow == css.OverflowHidden || overflow == css.OverflowScroll || overflow == css.OverflowAuto {
		layer.HasClip = true
		clipW := math.Floor(box.Width - box.Border.Left - box.Border.Right)
		clipH := math.Floor(box.Height - box.Border.Top - box.Border.Bottom)
		if clipW < 0 {
			clipW = 0
		}
		if clipH < 0 {
			clipH = 0
		}
		layer.ClipRect = [4]float64{
			box.X + box.Border.Left,
			box.Y + box.Border.Top,
			clipW,
			clipH,
		}
	}

	// CSS clip: rect() — applies to absolutely positioned elements (CSS 2.1 §11.1.2).
	// Values are physical offsets from the element's border-box top-left corner.
	if cr := s.GetClipRect(); cr != nil {
		layer.HasCSSClip = true
		clipRight := cr.Right
		if clipRight < 0 {
			clipRight = box.Width // "auto" sentinel
		}
		clipBottom := cr.Bottom
		if clipBottom < 0 {
			clipBottom = box.Height // "auto" sentinel
		}
		layer.CSSClipRect = [4]float64{
			box.X + cr.Left,
			box.Y + cr.Top,
			clipRight - cr.Left,
			clipBottom - cr.Top,
		}
	}

	// Compositing.
	if vis, ok := s.Get("visibility"); ok && vis == "hidden" {
		layer.Visible = false
	}
	layer.Opacity = s.GetOpacity()

	// Replaced element (img): capture src for paint-time image loading.
	if box.Node != nil && box.Node.TagName == "img" {
		if src, ok := box.Node.GetAttribute("src"); ok {
			layer.ImageSrc = src
		}
		layer.ObjectFit = s.GetObjectFit()
		opX, opY := s.GetObjectPosition()
		layer.ObjectPosition = [2]float64{opX, opY}
	}

	// Background color.
	if bg, ok := s.Get("background-color"); ok {
		if c, ok := css.ParseColor(bg); ok {
			layer.BackgroundColor = c
		}
	}

	// Background image, repeat, position.
	if url, ok := s.GetBackgroundImage(); ok {
		layer.BackgroundImage = url
		layer.BackgroundRepeat = s.GetBackgroundRepeat()
		layer.BackgroundPosition = s.GetBackgroundPosition()
		layer.BackgroundSize = s.GetBackgroundSize()
	} else if val, ok := s.Get("background-image"); ok && isGradientValue(val) {
		layer.BackgroundGradient = val
	}
	layer.BackgroundClip = s.GetBackgroundClip()

	// Border colors: currentColor fallback.
	currentColor := css.Color{R: 0, G: 0, B: 0, A: 1.0}
	if cv, ok := s.Get("color"); ok {
		if c, ok := css.ParseColor(cv); ok {
			currentColor = c
		}
	}
	sides := [4]string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"}
	for i, prop := range sides {
		if val, ok := s.Get(prop); ok {
			if c, ok := css.ParseColor(val); ok {
				layer.BorderColors[i] = c
				continue
			}
		}
		layer.BorderColors[i] = currentColor
	}

	// Border styles.
	bs := s.GetBorderStyle()
	layer.BorderStyles = [4]css.BorderStyle{bs.Top, bs.Right, bs.Bottom, bs.Left}

	// Border radius.
	corners := s.GetBorderRadiusCorners()
	layer.BorderRadius = [4]float64{corners.TopLeft, corners.TopRight, corners.BottomRight, corners.BottomLeft}
	// CSS Backgrounds §5.5: corner overlap reduction.
	if layer.BorderRadius != [4]float64{} {
		reduceBorderRadii(&layer.BorderRadius, box.Width, box.Height)
	}

	// Box shadows.
	layer.BoxShadows = s.GetBoxShadow()

	// Outline.
	layer.OutlineStyle = s.GetOutlineStyle()
	if layer.OutlineStyle != "none" {
		layer.OutlineWidth = s.GetOutlineWidth()
		r, g, b, a := s.GetOutlineColor()
		layer.OutlineColor = css.Color{R: r, G: g, B: b, A: a}
		layer.OutlineOffset = s.GetOutlineOffset()
	}

	// Text.
	layer.TextColor = currentColor // default: currentColor
	layer.FontSize = s.GetFontSize()
	if layer.FontSize <= 0 {
		layer.FontSize = 16
	}
	layer.FontBold = s.GetFontWeight() == css.FontWeightBold
	layer.FontItalic = s.GetFontStyle() == css.FontStyleItalic
	layer.FontMono = s.IsMonospaceFamily()
	layer.FontAhem = s.IsAhemFamily()
	if family, ok := s.Get("font-family"); ok {
		layer.FontFamily = family
	}
	layer.LetterSpacing = s.GetLetterSpacing()
	layer.WordSpacing = s.GetWordSpacing()
	layer.IsVerticalText = box.IsVerticalText
	layer.IsSidewaysLR = box.IsSidewaysLR
	layer.IsSidewaysRL = box.IsSidewaysRL

	// Text decoration.
	layer.TextDecoration = s.GetTextDecoration()
	if decColor, ok := s.GetTextDecorationColor(); ok {
		layer.TextDecorationColor = decColor
	} else {
		layer.TextDecorationColor = currentColor
	}
	layer.TextDecorationThickness = s.GetTextDecorationThickness()
	layer.TextDecorationStyle = s.GetTextDecorationStyle()
	layer.TextShadows = s.GetTextShadow()
	layer.TextTransform = s.GetTextTransform()
	layer.TextOverflow = s.GetTextOverflow()

	// List markers.
	if s.GetDisplay() == css.DisplayListItem {
		layer.IsListItem = true
		layer.ListStyleType = s.GetListStyleType()
		layer.ListItemIndex = computeListItemIndex(box)
	}

	// CSS Transforms.
	transforms := s.GetTransforms()
	if len(transforms) > 0 {
		layer.HasTransform = true
		origin := s.GetTransformOrigin()
		// Resolve percentage origin to px relative to element's border box.
		layer.TransformOrigin = [2]float64{
			origin.X * box.Width,
			origin.Y * box.Height,
		}
		// Resolve percentage translate values against element's own dimensions.
		// parseTransformValue() uses negative values as a sentinel for percentages.
		resolved := make([]css.Transform, len(transforms))
		for i, t := range transforms {
			resolved[i] = css.Transform{Type: t.Type, Values: make([]float64, len(t.Values))}
			copy(resolved[i].Values, t.Values)
			if t.Type == "translate" {
				// Values[0] is X (percentage relative to width)
				// Values[1] is Y (percentage relative to height)
				if len(resolved[i].Values) > 0 && resolved[i].Values[0] < 0 {
					resolved[i].Values[0] = (-resolved[i].Values[0] / 100) * box.Width
				}
				if len(resolved[i].Values) > 1 && resolved[i].Values[1] < 0 {
					resolved[i].Values[1] = (-resolved[i].Values[1] / 100) * box.Height
				}
			}
		}
		layer.Transforms = resolved
	}

	// CSS Filters.
	filters := s.GetFilter()
	if len(filters) > 0 {
		layer.Filters = filters
		layer.HasFilter = true
	}

	// CSS clip-path.
	layer.ClipPath = s.GetClipPath()

	// CSS mix-blend-mode.
	layer.BlendMode = s.GetMixBlendMode()

	return layer
}

// computeListItemIndex returns the 1-based ordinal for a list item,
// respecting <ol start="N"> and counting preceding element siblings.
func computeListItemIndex(box *layout.Box) int {
	if box.LayoutNode == nil || box.LayoutNode.DOMNode == nil {
		return 1
	}
	node := box.LayoutNode.DOMNode
	if node.Parent == nil {
		return 1
	}
	// Check for <ol start="N">
	start := 1
	if val, ok := node.Parent.GetAttribute("start"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			start = n
		}
	}
	// Count preceding list-item siblings
	idx := start
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			break
		}
		if sibling.Type == html.ElementNode {
			idx++
		}
	}
	return idx
}

// domOrderedChildren returns the children of box in DOM tree order.
//
// In-flow children (block, inline, anonymous) are already laid out in DOM
// order and appear in box.Children in that order. Out-of-flow (abs-pos/fixed)
// children are appended at the end of box.Children by OutOfFlowLayoutPart,
// regardless of their DOM position. This function re-inserts out-of-flow
// children at their correct DOM position while keeping in-flow children
// (including anonymous boxes) in their original order.
func domOrderedChildren(box *layout.Box) []*layout.Box {
	lin := box.LayoutNode
	if lin == nil {
		return box.Children
	}

	// Identify out-of-flow (abs-pos/fixed) children. These are appended at
	// the end of box.Children by OutOfFlowLayoutPart out of DOM order.
	oofSet := make(map[*layout.Box]bool)
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			oofSet[child] = true
		}
	}

	if len(oofSet) == 0 {
		// No out-of-flow children — in-flow order is already correct.
		return box.Children
	}

	// Build LIN → Box map for out-of-flow children only.
	byLIN := make(map[*layout.LayoutInputNode]*layout.Box, len(oofSet))
	for _, child := range box.Children {
		if oofSet[child] && child.LayoutNode != nil {
			byLIN[child.LayoutNode] = child
		}
	}

	// Collect in-flow children in their original (already DOM-correct) order.
	inFlow := make([]*layout.Box, 0, len(box.Children)-len(oofSet))
	for _, child := range box.Children {
		if !oofSet[child] {
			inFlow = append(inFlow, child)
		}
	}

	// Walk lin.Children() (DOM order) to interleave out-of-flow boxes at their
	// correct positions. Each non-OOF LIN child corresponds to one in-flow box
	// (block, anonymous block, or continuation). OOF LIN children are inserted
	// at their DOM position without consuming an in-flow slot.
	result := make([]*layout.Box, 0, len(box.Children))
	inserted := make(map[*layout.Box]bool)
	inFlowIdx := 0
	for _, linChild := range lin.Children() {
		if cb, ok := byLIN[linChild]; ok {
			// Out-of-flow child: insert at this DOM position.
			result = append(result, cb)
			inserted[cb] = true
		} else if linChild.IsText() {
			// Text node: block layout never emits a box child for text nodes
			// (they are handled by inline layout inside anonymous blocks).
			// Skip without consuming an inFlow slot.
			continue
		} else {
			// In-flow (or anonymous) child: emit next in-flow box in order.
			if inFlowIdx < len(inFlow) {
				result = append(result, inFlow[inFlowIdx])
				inFlowIdx++
			}
		}
	}
	// Emit any remaining in-flow boxes (e.g. line boxes with no LIN).
	for ; inFlowIdx < len(inFlow); inFlowIdx++ {
		result = append(result, inFlow[inFlowIdx])
	}
	// Append OOF children that propagated up from descendants (not direct
	// DOM children of this box). These weren't found during the DOM walk
	// above because their LayoutInputNode lives deeper in the tree.
	for _, child := range box.Children {
		if oofSet[child] && !inserted[child] {
			result = append(result, child)
		}
	}
	return result
}

// buildPaintSubtree walks the Box tree, creating PaintLayers and assigning
// them to the correct parent/stacking-context lists.
//
// parentLayer: the PaintLayer that owns FlowChildren at this level.
// currentSC:   the nearest ancestor stacking context's PaintLayer.
func buildPaintSubtree(box *layout.Box, parentLayer, currentSC *PaintLayer) {
	for _, child := range domOrderedChildren(box) {
		if child.Style == nil {
			// Unstyled box (line box, text run) — no PaintLayer.
			// Recurse to find any styled descendants.
			buildPaintSubtree(child, parentLayer, currentSC)
			continue
		}

		childLayer := newPaintLayer(child)
		isPositioned := child.Position != css.PositionStatic && child.Position != ""

		if !isPositioned {
			parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
			buildPaintSubtree(child, childLayer, currentSC)
			continue
		}

		// Positioned child. Check if contained by parent's overflow clip.
		// Contained children stay in DOM-order painting (FlowChildren).
		if isContainedByOverflow(child, box) {
			parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
			buildPaintSubtree(child, childLayer, currentSC)
			continue
		}

		// Positioned and not contained — assign to stacking context z-lists.
		if child.CreatesStackingContext() {
			z := child.ZIndex
			switch {
			case z < 0:
				currentSC.NegativeZ = append(currentSC.NegativeZ, childLayer)
			case z > 0:
				currentSC.PositiveZ = append(currentSC.PositiveZ, childLayer)
			default:
				currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
			}
			// New stacking context — descendants collected by childLayer.
			buildPaintSubtree(child, childLayer, childLayer)
		} else {
			// Positioned, z-index:auto — participates at Appendix E step 6.
			currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
			if hasOverflowClipping(child) {
				// Overflow containment boundary — positioned descendants
				// stay within this subtree.
				buildPaintSubtree(child, childLayer, childLayer)
			} else {
				buildPaintSubtree(child, childLayer, currentSC)
			}
		}
	}
}

// sortZLists sorts NegativeZ and PositiveZ by z-index (ascending),
// then recurses into all child layers.
func (layer *PaintLayer) sortZLists() {
	sort.SliceStable(layer.NegativeZ, func(i, j int) bool {
		return layer.NegativeZ[i].ZIndex < layer.NegativeZ[j].ZIndex
	})
	sort.SliceStable(layer.PositiveZ, func(i, j int) bool {
		return layer.PositiveZ[i].ZIndex < layer.PositiveZ[j].ZIndex
	})
	for _, child := range layer.NegativeZ {
		child.sortZLists()
	}
	for _, child := range layer.AutoZero {
		child.sortZLists()
	}
	for _, child := range layer.PositiveZ {
		child.sortZLists()
	}
	for _, child := range layer.FlowChildren {
		child.sortZLists()
	}
}

// reduceBorderRadii applies CSS Backgrounds §5.5 corner overlap reduction.
// If the sum of adjacent radii exceeds the box dimension, all radii are
// scaled down proportionally.
func reduceBorderRadii(radii *[4]float64, w, h float64) {
	if w <= 0 || h <= 0 {
		*radii = [4]float64{}
		return
	}
	f := 1.0
	// Top edge: TL + TR
	if s := radii[0] + radii[1]; s > 0 && w/s < f {
		f = w / s
	}
	// Bottom edge: BL + BR
	if s := radii[3] + radii[2]; s > 0 && w/s < f {
		f = w / s
	}
	// Left edge: TL + BL
	if s := radii[0] + radii[3]; s > 0 && h/s < f {
		f = h / s
	}
	// Right edge: TR + BR
	if s := radii[1] + radii[2]; s > 0 && h/s < f {
		f = h / s
	}
	if f < 1 {
		for i := range radii {
			radii[i] *= f
		}
	}
}
