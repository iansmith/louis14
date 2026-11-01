package render

import (
	"math"
	"sort"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/geometry/layoutunit"
)

// This file ports Blink's BoxBorderPainter (core/paint/box_border_painter.{h,cc}
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and BorderEdge
// (core/style/border_edge.{h,cc} @ a9f50e52).
//
// Scope of the port:
//   - Every corner has round curvature (louis14 has no `corner-shape`), so
//     ClipBorderSidePolygonCloseToEdges (:1965-2036), which serves only
//     non-hyperellipse corners, is not ported.
//   - css.BorderStyle carries none/hidden/solid/dashed/dotted/double only:
//     pkg/css folds groove/ridge/inset/outset into solid, so their painting
//     (DrawRidgeOrGrooveBoxSide, CalculateInsetOutsetColor,
//     DrawCurvedRidgeGrooveBoxSide) is unreachable and not ported.
//   - Straight dashed/dotted sides paint through louis14's drawDashedLine /
//     drawDottedLine stand-ins for DrawLineWithStyle + StyledStrokeData
//     (:501-598); curved dashed/dotted sides fill like solid ones in place of
//     DrawCurvedDashedDottedBoxSide's dashed centerline stroke (:1827-1861).
//   - No bleed avoidance: louis14 always paints with Blink's
//     kBackgroundBleedNone, so DrawBleedAdjustedDRRect is a plain FillDRRect.
//   - PaintSingleRectOutline serves double outlines only (drawOutline keeps
//     its own solid/dashed/dotted painting); PaintBorderArea is not ported.

// BoxSide names a physical box side. Mirrors Blink's BoxSide
// (core/layout/geometry/box_sides.h @ a9f50e52); the values index
// [top, right, bottom, left] arrays (box_border_painter.cc:343-350).
type BoxSide int

const (
	BoxSideTop BoxSide = iota
	BoxSideRight
	BoxSideBottom
	BoxSideLeft
)

// borderEdgeFlags is a set of BoxSides (BorderEdgeFlags,
// box_border_painter.cc:35-50 @ a9f50e52).
type borderEdgeFlags uint

const allBorderEdges borderEdgeFlags = 1<<BoxSideTop | 1<<BoxSideRight | 1<<BoxSideBottom | 1<<BoxSideLeft

func edgeFlagForSide(side BoxSide) borderEdgeFlags { return 1 << side }

func includesEdge(flags borderEdgeFlags, side BoxSide) bool { return flags&edgeFlagForSide(side) != 0 }

// includesAdjacentEdges reports whether flags hold at least one horizontal
// and one vertical side (box_border_painter.cc:52-57).
func includesAdjacentEdges(flags borderEdgeFlags) bool {
	return flags&(1<<BoxSideTop|1<<BoxSideBottom) != 0 && flags&(1<<BoxSideLeft|1<<BoxSideRight) != 0
}

// BorderEdge is one side's used border: width, color and effective style.
// Mirrors Blink's BorderEdge (core/style/border_edge.{h,cc} @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BorderEdge struct {
	width     int
	color     css.Color
	style     css.BorderStyle
	isPresent bool
}

func newBorderEdge(width int, color css.Color, style css.BorderStyle, isPresent bool) BorderEdge {
	return BorderEdge{width: width, color: color, style: effectiveBorderStyle(style, width), isPresent: isPresent}
}

// effectiveBorderStyle paints a double border narrower than 3px as solid
// (BorderEdge::EffectiveStyle, border_edge.cc:23-30).
func effectiveBorderStyle(style css.BorderStyle, width int) css.BorderStyle {
	if style == css.BorderStyleDouble && width < 3 {
		return css.BorderStyleSolid
	}
	return style
}

// HasVisibleColorAndStyle (border_edge.cc:32-34).
func (e BorderEdge) HasVisibleColorAndStyle() bool {
	return e.style != css.BorderStyleNone && e.style != css.BorderStyleHidden && e.color.A > 0
}

// ShouldRender (border_edge.cc:36-38).
func (e BorderEdge) ShouldRender() bool {
	return e.isPresent && e.width != 0 && e.HasVisibleColorAndStyle()
}

// PresentButInvisible (border_edge.cc:40-42).
func (e BorderEdge) PresentButInvisible() bool {
	return e.UsedWidth() != 0 && !e.HasVisibleColorAndStyle()
}

// UsedWidth (border_edge.cc:69-71).
func (e BorderEdge) UsedWidth() int {
	if e.isPresent {
		return e.width
	}
	return 0
}

// GetDoubleBorderStripeWidth returns the width from the border edge to the
// outer stripe's inner edge (outer) or to the inner stripe's outer edge
// (inner) (border_edge.cc:73-80).
func (e BorderEdge) GetDoubleBorderStripeWidth(outerStripe bool) int {
	if outerStripe {
		return int(math.Round(float64(e.UsedWidth()) / 3))
	}
	return int(math.Round(float64(e.UsedWidth()) * 2 / 3))
}

// SharesColorWith (border_edge.cc:82-84).
func (e BorderEdge) SharesColorWith(o BorderEdge) bool { return e.color == o.color }

// ClampWidth (border_edge.cc:86-91).
func (e *BorderEdge) ClampWidth(maxWidth int) {
	if e.width > maxWidth {
		e.width = maxWidth
		e.style = effectiveBorderStyle(e.style, e.width)
	}
}

// borderEdgesForLayer reads the four used border edges of layer's box in
// borderRect (ComputedStyle::GetBorderEdgeInfo, computed_style.cc:2720-2742
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), with whole-pixel widths
// (snappedBorderWidths); louis14 already zeroes the width of a none/hidden
// side.
func borderEdgesForLayer(layer *PaintLayer, borderRect geometry.PhysicalRect) [4]BorderEdge {
	widths := snappedBorderWidths(borderRect, toLayoutStrut(layer.Box.Border))
	var edges [4]BorderEdge
	for i := range edges {
		edges[i] = newBorderEdge(widths[i], layer.BorderColors[i], layer.BorderStyles[i], true)
	}
	return edges
}

// miterType is how a side's end meets its neighbour (BoxBorderPainter::
// MiterType, box_border_painter.h @ a9f50e52). louis14's rasterizer has no
// anti-aliasing switch, so soft and hard miters clip identically; the
// distinction still drives which sides clip at all.
type miterType int

const (
	noMiter miterType = iota
	softMiter
	hardMiter
)

// sideType selects straight (rect/quad fill) or curved (clip + fill)
// painting of a side (BoxBorderPainter::SideType).
type sideType int

const (
	sideStraight sideType = iota
	sideCurved
)

// borderStyleRequiresClipPolygon: stroked styles need a clip for corner
// miters (box_border_painter.cc:59-62).
func borderStyleRequiresClipPolygon(style css.BorderStyle) bool {
	return style == css.BorderStyleDotted || style == css.BorderStyleDashed
}

// borderStyleFillsBorderArea (box_border_painter.cc:64-67).
func borderStyleFillsBorderArea(style css.BorderStyle) bool {
	return style != css.BorderStyleDotted && style != css.BorderStyleDashed && style != css.BorderStyleDouble
}

// borderStyleHasInnerDetail (box_border_painter.cc:69-72); groove and ridge
// are not representable in css.BorderStyle.
func borderStyleHasInnerDetail(style css.BorderStyle) bool {
	return style == css.BorderStyleDouble
}

// borderStyleIsDottedOrDashed (box_border_painter.cc:74-76).
func borderStyleIsDottedOrDashed(style css.BorderStyle) bool {
	return style == css.BorderStyleDotted || style == css.BorderStyleDashed
}

// borderWillArcInnerEdge (box_border_painter.cc:99-102).
func borderWillArcInnerEdge(first, second css.EllipticalRadius) bool {
	return !first.IsZero() || !second.IsZero()
}

// willOverdraw reports whether a still-unpainted side will paint over the
// current side's end (box_border_painter.cc:104-115).
func willOverdraw(side BoxSide, style css.BorderStyle, completed borderEdgeFlags) bool {
	if includesEdge(completed, side) {
		return false
	}
	return borderStyleFillsBorderArea(style)
}

// borderStylesRequireMiter (box_border_painter.cc:117-135). The
// unmatched-colors-at-corner clause concerns inset/outset/groove/ridge,
// which css.BorderStyle cannot hold.
func borderStylesRequireMiter(style, adjacentStyle css.BorderStyle) bool {
	if style == css.BorderStyleDouble || adjacentStyle == css.BorderStyleDouble {
		return true
	}
	if borderStyleIsDottedOrDashed(style) != borderStyleIsDottedOrDashed(adjacentStyle) {
		return true
	}
	return style != adjacentStyle
}

// sideRect narrows rect to the edgeWidth-thick strip along side
// (SetToRightSideRect / SetToBottomSideRect / CalculateSideRect,
// box_border_painter.cc:137-168).
type intRect struct{ x, y, w, h int }

func (r intRect) right() int  { return r.x + r.w }
func (r intRect) bottom() int { return r.y + r.h }

func toIntRect(r geometry.RectF) intRect {
	return intRect{int(math.Round(r.X())), int(math.Round(r.Y())), int(math.Round(r.Width())), int(math.Round(r.Height()))}
}

func sideRect(r intRect, side BoxSide, edgeWidth int) intRect {
	switch side {
	case BoxSideTop:
		r.h = edgeWidth
	case BoxSideBottom:
		r.y = r.bottom() - edgeWidth
		r.h = edgeWidth
	case BoxSideLeft:
		r.w = edgeWidth
	case BoxSideRight:
		r.x = r.right() - edgeWidth
		r.w = edgeWidth
	}
	return r
}

// calculateAdjustedInnerBorder grows a non-renderable inner border, for one
// side, into a rounded rect whose radii fit: the arc is shifted toward the
// zero-radius corner, and the far corners are squared. Mirrors
// CalculateAdjustedInnerBorder (box_border_painter.cc:170-260 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for round (hyperellipse)
// curvature.
func calculateAdjustedInnerBorder(inner FloatRoundedRect, side BoxSide) FloatRoundedRect {
	rd := inner.Radii
	x, y, w, h := inner.Rect.X(), inner.Rect.Y(), inner.Rect.Width(), inner.Rect.Height()
	tl, tr, br, bl := &rd[0], &rd[1], &rd[2], &rd[3]
	switch side {
	case BoxSideTop:
		if overshoot := tl.Rx + tr.Rx - w; overshoot > 0.1 {
			w += overshoot
			if tl.Rx == 0 {
				x -= overshoot
			}
		}
		*bl, *br = css.EllipticalRadius{}, css.EllipticalRadius{}
		if m := math.Max(tl.Ry, tr.Ry); m > h {
			h = m
		}
	case BoxSideBottom:
		if overshoot := bl.Rx + br.Rx - w; overshoot > 0.1 {
			w += overshoot
			if bl.Rx == 0 {
				x -= overshoot
			}
		}
		*tl, *tr = css.EllipticalRadius{}, css.EllipticalRadius{}
		if m := math.Max(bl.Ry, br.Ry); m > h {
			y += h - m
			h = m
		}
	case BoxSideLeft:
		if overshoot := tl.Ry + bl.Ry - h; overshoot > 0.1 {
			h += overshoot
			if tl.Ry == 0 {
				y -= overshoot
			}
		}
		*tr, *br = css.EllipticalRadius{}, css.EllipticalRadius{}
		if m := math.Max(tl.Rx, bl.Rx); m > w {
			w = m
		}
	case BoxSideRight:
		if overshoot := tr.Ry + br.Ry - h; overshoot > 0.1 {
			h += overshoot
			if tr.Ry == 0 {
				y -= overshoot
			}
		}
		*tl, *bl = css.EllipticalRadius{}, css.EllipticalRadius{}
		if m := math.Max(tr.Rx, br.Rx); m > w {
			x += w - m
			w = m
		}
	}
	return FloatRoundedRect{Rect: geometry.NewRectF(x, y, w, h), Radii: rd}
}

// kStylePriority: non-solid edges paint before solid ones to maximise
// overdraw (box_border_painter.cc:352-367).
func borderStylePriority(style css.BorderStyle) int {
	switch style {
	case css.BorderStyleDotted, css.BorderStyleDashed, css.BorderStyleDouble:
		return 1
	case css.BorderStyleSolid:
		return 3
	}
	return 0
}

// kSidePriority: same-style sides paint in non-adjacent order to minimise
// miters (box_border_painter.cc:369-376).
var sidePriority = [4]int{0, 2, 1, 3}

// opacityGroup is a set of edges sharing one alpha (OpacityGroup,
// box_border_painter.cc:378-388).
type opacityGroup struct {
	sides     []BoxSide
	edgeFlags borderEdgeFlags
	alpha     float64
}

// complexBorderInfo holds the visible edges grouped by opacity and sorted
// in paint order (BoxBorderPainter::ComplexBorderInfo,
// box_border_painter.cc:1164-1236 @ a9f50e52).
type complexBorderInfo struct {
	opacityGroups []opacityGroup
}

func newComplexBorderInfo(p *BoxBorderPainter) complexBorderInfo {
	var sorted []BoxSide
	for i := p.firstVisibleEdge; i < 4; i++ {
		if includesEdge(p.visibleEdgeSet, BoxSide(i)) {
			sorted = append(sorted, BoxSide(i))
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := p.edges[sorted[i]], p.edges[sorted[j]]
		if a.color.A != b.color.A {
			return a.color.A < b.color.A
		}
		if pa, pb := borderStylePriority(a.style), borderStylePriority(b.style); pa != pb {
			return pa < pb
		}
		return sidePriority[sorted[i]] < sidePriority[sorted[j]]
	})
	var info complexBorderInfo
	currentAlpha := 0.0
	for _, side := range sorted {
		alpha := p.edges[side].color.A
		if alpha != currentAlpha {
			info.opacityGroups = append(info.opacityGroups, opacityGroup{alpha: alpha})
			currentAlpha = alpha
		}
		g := &info.opacityGroups[len(info.opacityGroups)-1]
		g.sides = append(g.sides, side)
		g.edgeFlags |= edgeFlagForSide(side)
	}
	return info
}

// BoxBorderPainter paints a box's borders. Mirrors Blink's BoxBorderPainter
// (core/paint/box_border_painter.{h,cc} @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BoxBorderPainter struct {
	r          *Renderer
	radii      css.EllipticalRadii
	borderRect geometry.PhysicalRect
	// outerOutsets is zero for a border; an outline's outer edge outsets
	// (outer_outsets_).
	outerOutsets geometry.PhysicalBoxStrut

	outer, inner FloatRoundedRect
	edges        [4]BorderEdge

	visibleEdgeCount int
	firstVisibleEdge int
	visibleEdgeSet   borderEdgeFlags

	isUniformStyle  bool
	isUniformWidth  bool
	isUniformColor  bool
	isRounded       bool
	hasTransparency bool
}

// PaintBorder paints layer's borders in rect, its border box
// (BoxBorderPainter::PaintBorder, box_border_painter.h @ a9f50e52).
func (r *Renderer) PaintBorder(layer *PaintLayer, rect geometry.PhysicalRect) {
	newBoxBorderPainter(r, layer, rect).Paint()
}

// newBoxBorderPainter mirrors the PaintBorder constructor and InitFromEdges
// (box_border_painter.cc:1336-1372).
func newBoxBorderPainter(r *Renderer, layer *PaintLayer, rect geometry.PhysicalRect) *BoxBorderPainter {
	p := &BoxBorderPainter{
		r:              r,
		radii:          layer.BorderRadius,
		borderRect:     rect,
		edges:          borderEdgesForLayer(layer, rect),
		isUniformStyle: true,
		isUniformWidth: true,
		isUniformColor: true,
	}
	p.computeBorderProperties()
	if p.visibleEdgeSet == 0 {
		return p
	}
	p.outer = PixelSnappedContouredBorder(p.radii, rect)
	p.inner = PixelSnappedContouredInnerBorder(p.radii, rect, toLayoutStrut(layer.Box.Border))

	// The border width may not exceed the border box, which can pixel
	// snap smaller.
	maxW, maxH := int(p.outer.Rect.Width()), int(p.outer.Rect.Height())
	p.edges[BoxSideTop].ClampWidth(maxH)
	p.edges[BoxSideRight].ClampWidth(maxW)
	p.edges[BoxSideBottom].ClampWidth(maxH)
	p.edges[BoxSideLeft].ClampWidth(maxW)

	p.isRounded = p.outer.IsRounded()
	return p
}

// paintSingleRectOutline paints a non-auto outline around rect as a border
// of width outside rect outset by innerOutsets
// (BoxBorderPainter::PaintSingleRectOutline and its constructor,
// box_border_painter.h, box_border_painter.cc:1400-1427 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) paintSingleRectOutline(radii css.EllipticalRadii, rect geometry.PhysicalRect, width int, innerOutsets geometry.PhysicalBoxStrut, c css.Color, style css.BorderStyle) {
	w := layoutunit.New(width)
	p := &BoxBorderPainter{
		r:              r,
		radii:          radii,
		borderRect:     rect,
		outerOutsets:   innerOutsets.Add(geometry.PhysicalBoxStrut{Top: w, Right: w, Bottom: w, Left: w}),
		isUniformStyle: true,
		isUniformWidth: true,
		isUniformColor: true,
	}
	for i := range p.edges {
		p.edges[i] = newBorderEdge(width, c, style, true)
	}
	p.computeBorderProperties()
	p.outer = PixelSnappedContouredBorderWithOutsets(radii, rect, p.outerOutsets)
	p.isRounded = p.outer.IsRounded()
	p.inner = PixelSnappedContouredBorderWithOutsets(radii, rect, innerOutsets)
	p.Paint()
}

// computeBorderProperties (box_border_painter.cc:1429-1461).
func (p *BoxBorderPainter) computeBorderProperties() {
	for i, edge := range p.edges {
		if !edge.ShouldRender() {
			if edge.PresentButInvisible() {
				p.isUniformWidth = false
				p.isUniformColor = false
			}
			continue
		}
		p.visibleEdgeCount++
		p.visibleEdgeSet |= edgeFlagForSide(BoxSide(i))
		if edge.color.A < 1 {
			p.hasTransparency = true
		}
		if p.visibleEdgeCount == 1 {
			p.firstVisibleEdge = i
			continue
		}
		first := p.edges[p.firstVisibleEdge]
		p.isUniformStyle = p.isUniformStyle && edge.style == first.style
		p.isUniformWidth = p.isUniformWidth && edge.width == first.width
		p.isUniformColor = p.isUniformColor && edge.SharesColorWith(first)
	}
}

func (p *BoxBorderPainter) firstEdge() BorderEdge { return p.edges[p.firstVisibleEdge] }

// Paint (box_border_painter.cc:1471-1495).
func (p *BoxBorderPainter) Paint() {
	if p.visibleEdgeCount == 0 || p.outer.Rect.IsEmpty() {
		return
	}
	if p.paintBorderFastPath() {
		return
	}
	info := newComplexBorderInfo(p)
	if !p.outer.IsRounded() {
		p.paintOpacityGroup(info, 0, 1)
		return
	}
	p.r.dc.Push()
	defer p.r.dc.Pop()
	p.clipRoundedRect(p.outer)
	if p.inner.IsRenderable() && !p.inner.IsEmpty() {
		p.r.clipOutContouredRect(p.inner)
	}
	p.paintOpacityGroup(info, 0, 1)
}

// paintBorderFastPath paints uniform solid/double borders with one or two
// FillDRRect calls (BoxBorderPainter::PaintBorderFastPath,
// box_border_painter.cc:1276-1334).
func (p *BoxBorderPainter) paintBorderFastPath() bool {
	if !p.isUniformColor || !p.isUniformStyle || !p.inner.IsRenderable() {
		return false
	}
	first := p.firstEdge()
	if first.style != css.BorderStyleSolid && first.style != css.BorderStyleDouble {
		return false
	}
	if p.visibleEdgeSet == allBorderEdges {
		if first.style == css.BorderStyleSolid {
			if p.isUniformWidth && !p.outer.IsRounded() {
				// DrawSolidBorderRect (:262-280): a stroked rect of the
				// border width covers exactly outer minus outer-inset-width.
				w := float64(first.width)
				o := p.outer.Rect
				in := FloatRoundedRect{Rect: geometry.NewRectF(o.X()+w, o.Y()+w, math.Max(0, o.Width()-2*w), math.Max(0, o.Height()-2*w))}
				p.fillDRRect(p.outer, in, first.color)
			} else {
				p.fillDRRect(p.outer, p.inner, first.color)
			}
		} else {
			p.drawDoubleBorder()
		}
		return true
	}
	// Translucent solid rectangular border with missing sides: one path
	// fill, avoiding a transparency layer.
	if first.style == css.BorderStyleSolid && !p.outer.IsRounded() && p.hasTransparency {
		outer := toIntRect(p.outer.Rect)
		for side := BoxSideTop; side <= BoxSideLeft; side++ {
			if e := p.edges[side]; e.ShouldRender() {
				s := sideRect(outer, side, e.width)
				p.r.dc.DrawRectangle(float64(s.x), float64(s.y), float64(s.w), float64(s.h))
			}
		}
		p.r.setColor(first.color)
		p.r.dc.Fill()
		return true
	}
	return false
}

// doubleStripeOutsets (box_border_painter.cc:2467-2475).
func (p *BoxBorderPainter) doubleStripeOutsets(outerStripe bool) geometry.PhysicalBoxStrut {
	o := [4]layoutunit.LayoutUnit{p.outerOutsets.Top, p.outerOutsets.Right, p.outerOutsets.Bottom, p.outerOutsets.Left}
	w := func(s BoxSide) layoutunit.LayoutUnit {
		return o[s].Sub(layoutunit.New(p.edges[s].GetDoubleBorderStripeWidth(outerStripe)))
	}
	return geometry.PhysicalBoxStrut{Top: w(BoxSideTop), Right: w(BoxSideRight), Bottom: w(BoxSideBottom), Left: w(BoxSideLeft)}
}

// drawDoubleBorder paints a uniform double border as two nested stripe
// rings (BoxBorderPainter::DrawDoubleBorder, box_border_painter.cc:1238-1274).
func (p *BoxBorderPainter) drawDoubleBorder() {
	c := p.firstEdge().color
	forceRectangular := !p.outer.IsRounded() && !p.inner.IsRounded()

	outerThird := PixelSnappedContouredBorderWithOutsets(p.radii, p.borderRect, p.doubleStripeOutsets(true))
	if forceRectangular {
		outerThird.Radii = css.EllipticalRadii{}
	}
	p.fillDRRect(p.outer, outerThird, c)

	innerThird := PixelSnappedContouredBorderWithOutsets(p.radii, p.borderRect, p.doubleStripeOutsets(false))
	if forceRectangular {
		innerThird.Radii = css.EllipticalRadii{}
	}
	p.fillDRRect(innerThird, p.inner, c)
}

// paintOpacityGroup paints the opacity groups from most to least opaque
// layer, each translucent group in its own transparency layer, so opaque
// paints can overdraw corner seams (BoxBorderPainter::PaintOpacityGroup,
// box_border_painter.cc:1541-1599).
func (p *BoxBorderPainter) paintOpacityGroup(info complexBorderInfo, index int, effectiveOpacity float64) borderEdgeFlags {
	n := len(info.opacityGroups)
	if index >= n {
		// Missing or transparent edges count as completed for overdraw.
		return ^p.visibleEdgeSet & allBorderEdges
	}
	group := info.opacityGroups[n-index-1]
	paintAlpha := group.alpha / effectiveOpacity
	needsLayer := group.alpha != 1 && (includesAdjacentEdges(group.edgeFlags) || index+1 < n)
	layerAlpha := 1.0
	if needsLayer {
		layerAlpha = group.alpha / effectiveOpacity
		p.r.dc.PushGroup()
		effectiveOpacity = group.alpha
		paintAlpha = 1
	}
	completed := p.paintOpacityGroup(info, index+1, effectiveOpacity)
	for _, side := range group.sides {
		p.paintSide(side, paintAlpha, completed)
		completed |= edgeFlagForSide(side)
	}
	if needsLayer {
		p.r.dc.PopGroupWithAlpha(layerAlpha)
	}
	return completed
}

// adjacentSides returns the two sides meeting side's ends, in Blink's
// PaintSide argument order (box_border_painter.cc:1601-1681).
func adjacentSides(side BoxSide) (BoxSide, BoxSide) {
	if side == BoxSideTop || side == BoxSideBottom {
		return BoxSideLeft, BoxSideRight
	}
	return BoxSideTop, BoxSideBottom
}

// innerArcRadii returns the inner radii at side's two corners, in the
// order PaintSide passes them to BorderWillArcInnerEdge.
func (p *BoxBorderPainter) innerArcRadii(side BoxSide) (css.EllipticalRadius, css.EllipticalRadius) {
	rd := p.inner.Radii
	switch side {
	case BoxSideTop:
		return rd[0], rd[1]
	case BoxSideBottom:
		return rd[3], rd[2]
	case BoxSideLeft:
		return rd[3], rd[0]
	}
	return rd[2], rd[1]
}

// paintSide (BoxBorderPainter::PaintSide, box_border_painter.cc:1601-1681).
func (p *BoxBorderPainter) paintSide(side BoxSide, alpha float64, completed borderEdgeFlags) {
	edge := p.edges[side]
	c := edge.color
	c.A = alpha
	a, b := p.innerArcRadii(side)
	isCurved := p.isRounded && (borderStyleHasInnerDetail(edge.style) || borderWillArcInnerEdge(a, b))
	rect := toIntRect(p.outer.Rect)
	st := sideCurved
	if !isCurved {
		rect = sideRect(rect, side, edge.width)
		st = sideStraight
	}
	adj1, adj2 := adjacentSides(side)
	p.paintOneBorderSide(rect, side, adj1, adj2, st, c, completed)
}

// colorsMatchAtCorner (box_border_painter.cc:2485-2496).
func (p *BoxBorderPainter) colorsMatchAtCorner(side, adjacent BoxSide) bool {
	return p.edges[adjacent].ShouldRender() && p.edges[side].SharesColorWith(p.edges[adjacent])
}

// computeMiter (BoxBorderPainter::ComputeMiter, box_border_painter.cc:1683-1712).
func (p *BoxBorderPainter) computeMiter(side, adjacent BoxSide, completed borderEdgeFlags) miterType {
	adj := p.edges[adjacent]
	if adj.UsedWidth() == 0 {
		return noMiter
	}
	if willOverdraw(adjacent, adj.style, completed) {
		return noMiter
	}
	if !p.colorsMatchAtCorner(side, adjacent) {
		return softMiter
	}
	if borderStylesRequireMiter(p.edges[side].style, adj.style) {
		return hardMiter
	}
	return noMiter
}

// mitersRequireClipping (box_border_painter.cc:1714-1726).
func mitersRequireClipping(m1, m2 miterType, style css.BorderStyle) bool {
	return m1 == hardMiter || m2 == hardMiter ||
		((m1 != noMiter || m2 != noMiter) && borderStyleRequiresClipPolygon(style))
}

// paintOneBorderSide (BoxBorderPainter::PaintOneBorderSide,
// box_border_painter.cc:1728-1783).
func (p *BoxBorderPainter) paintOneBorderSide(rect intRect, side, adj1, adj2 BoxSide, st sideType, c css.Color, completed borderEdgeFlags) {
	edge := p.edges[side]
	e1, e2 := p.edges[adj1], p.edges[adj2]
	if st == sideCurved {
		m1, m2 := softMiter, softMiter
		if p.colorsMatchAtCorner(side, adj1) {
			m1 = hardMiter
		}
		if p.colorsMatchAtCorner(side, adj2) {
			m2 = hardMiter
		}
		p.r.dc.Push()
		defer p.r.dc.Pop()
		p.clipBorderSidePolygon(side, m1, m2)
		if !p.inner.IsRenderable() {
			if adjusted := calculateAdjustedInnerBorder(p.inner, side); !adjusted.IsEmpty() {
				p.r.clipOutContouredRect(adjusted)
			}
		}
		p.drawCurvedBoxSide(edge.width, side, c, edge.style)
		return
	}
	m1 := p.computeMiter(side, adj1, completed)
	m2 := p.computeMiter(side, adj2, completed)
	if mitersRequireClipping(m1, m2, edge.style) {
		p.r.dc.Push()
		defer p.r.dc.Pop()
		p.clipBorderSidePolygon(side, m1, m2)
		// Miters are applied via clipping; no need to draw them.
		m1, m2 = noMiter, noMiter
	}
	w1, w2 := 0, 0
	if m1 != noMiter {
		w1 = e1.width
	}
	if m2 != noMiter {
		w2 = e2.width
	}
	p.r.drawLineForBoxSide(rect.x, rect.y, rect.right(), rect.bottom(), side, c, edge.style, w1, w2)
}

// drawCurvedBoxSide paints a curved side through the side clip set up by
// paintOneBorderSide (BoxBorderPainter::DrawCurvedBoxSide,
// box_border_painter.cc:1785-1825). Dashed/dotted sides fill like solid
// ones (see the file comment).
func (p *BoxBorderPainter) drawCurvedBoxSide(thickness int, side BoxSide, c css.Color, style css.BorderStyle) {
	if thickness <= 0 {
		return
	}
	if style == css.BorderStyleDouble {
		p.drawCurvedDoubleBoxSide(c)
		return
	}
	p.r.fillIntRect(toIntRect(p.outer.Rect), c)
}

// drawCurvedDoubleBoxSide (BoxBorderPainter::DrawCurvedDoubleBoxSide,
// box_border_painter.cc:1863-1899).
func (p *BoxBorderPainter) drawCurvedDoubleBoxSide(c css.Color) {
	rect := toIntRect(p.outer.Rect)
	// Inner stripe: inside the inner stripe's outer edge (the inner border
	// was clipped out by Paint).
	p.r.dc.Push()
	p.clipRoundedRect(PixelSnappedContouredBorderWithOutsets(p.radii, p.borderRect, p.doubleStripeOutsets(false)))
	p.r.fillIntRect(rect, c)
	p.r.dc.Pop()
	// Outer stripe: outside the outer stripe's inner edge.
	p.r.dc.Push()
	p.r.clipOutContouredRect(PixelSnappedContouredBorderWithOutsets(p.radii, p.borderRect, p.doubleStripeOutsets(true)))
	p.r.fillIntRect(rect, c)
	p.r.dc.Pop()
}

// clipRoundedRect intersects the clip with rr (ClipContouredRect,
// box_border_painter.cc:1463-1465).
func (p *BoxBorderPainter) clipRoundedRect(rr FloatRoundedRect) {
	rr.addPath(p.r.dc)
	p.r.dc.Clip()
}

// clipOutContouredRect subtracts rr from the clip, anti-aliased
// (BoxBorderPainter::ClipOutContouredRect, box_border_painter.cc:1467-1469,
// → GraphicsContext::ClipOutContouredRect, graphics_context.cc:931-933,
// which clips with SkClipOp::kDifference and the kAntiAliased default of
// ClipContouredRect, graphics_context.h:379-381 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Its edge pixels keep exactly
// the coverage a same-rrect ClipContouredRect leaves out, so a box's
// background (clipped to rr) and border (clipped out of rr) split them.
func (r *Renderer) clipOutContouredRect(rr FloatRoundedRect) {
	rr.addPath(r.dc)
	r.dc.ClipOut()
}

// fillDRRect fills outer minus inner (GraphicsContext::FillDRRect,
// graphics_context.cc:822-854, via DrawBleedAdjustedDRRect's default
// branch, box_border_painter.cc:282-316 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): Skia's anti-aliased
// drawDRRect. The inner rrect wound backwards cuts the hole under the
// nonzero rule, and its anti-aliased edge coverage is subtracted from the
// outer's.
func (p *BoxBorderPainter) fillDRRect(outer, inner FloatRoundedRect, c css.Color) {
	p.r.setColor(c)
	outer.addPath(p.r.dc)
	if !inner.IsEmpty() {
		inner.addReversePath(p.r.dc)
	}
	p.r.dc.Fill()
}

// addPolygonPath appends the closed polygon pts.
func addPolygonPath(dc pathBuilder, pts []geometry.PointF) {
	dc.MoveTo(pts[0].X, pts[0].Y)
	for _, pt := range pts[1:] {
		dc.LineTo(pt.X, pt.Y)
	}
	dc.ClosePath()
}

// clipPolygon (ClipPolygon, box_border_painter.cc:390-400 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) clips anti-aliased only when
// antialiased: ClipBorderSidePolygon passes true for soft miters (a color
// change at the corner) and false for hard miters, so same-colored sides
// meeting along a miter overlap rather than leave an anti-aliased seam.
func (p *BoxBorderPainter) clipPolygon(pts []geometry.PointF, antialiased bool) {
	dc := p.r.dc
	was := dc.ShouldAntialias()
	dc.SetShouldAntialias(antialiased)
	addPolygonPath(dc, pts)
	dc.Clip()
	dc.SetShouldAntialias(was)
}

// findIntersection sets *out to the intersection of the infinite lines
// p1-p2 and d1-d2, leaving it unchanged when they are parallel
// (FindIntersection, box_border_painter.cc:990-997).
func findIntersection(p1, p2, d1, d2 geometry.PointF, out *geometry.PointF) {
	pdx, pdy := p2.X-p1.X, p2.Y-p1.Y
	ddx, ddy := d2.X-d1.X, d2.Y-d1.Y
	denom := pdx*ddy - pdy*ddx
	if denom == 0 {
		return
	}
	t := ((d1.X-p1.X)*ddy - (d1.Y-p1.Y)*ddx) / denom
	*out = geometry.PointF{X: p1.X + t*pdx, Y: p1.Y + t*pdy}
}

func addPt(a geometry.PointF, dx, dy float64) geometry.PointF {
	return geometry.PointF{X: a.X + dx, Y: a.Y + dy}
}

// clipBorderSidePolygon clips to the quad (or pentagon) of side that may
// paint, including the half corners up to the miters
// (BoxBorderPainter::ClipBorderSidePolygon, box_border_painter.cc:2038-2465
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *BoxBorderPainter) clipBorderSidePolygon(side BoxSide, firstMiter, secondMiter miterType) {
	ir, or := p.inner.Rect, p.outer.Rect
	rd := p.inner.Radii
	pt := func(x, y float64) geometry.PointF { return geometry.PointF{X: x, Y: y} }
	innerPts := [4]geometry.PointF{pt(ir.X(), ir.Y()), pt(ir.Right(), ir.Y()), pt(ir.Right(), ir.Bottom()), pt(ir.X(), ir.Bottom())}
	outerPts := [4]geometry.PointF{pt(or.X(), or.Y()), pt(or.Right(), or.Y()), pt(or.Right(), or.Bottom()), pt(or.X(), or.Bottom())}

	const extensionLength = 1e-1
	var q [4]geometry.PointF
	var pentagon []geometry.PointF
	var bound1, bound2 geometry.PointF
	var extX, extY float64

	switch side {
	case BoxSideTop:
		q = [4]geometry.PointF{outerPts[0], innerPts[0], innerPts[1], outerPts[1]}
		bound1, bound2 = pt(q[0].X, q[1].Y), pt(q[3].X, q[2].Y)
		extX = -extensionLength
		if !rd[0].IsZero() {
			findIntersection(q[0], q[1], pt(q[1].X+rd[0].Rx, q[1].Y), pt(q[1].X, q[1].Y+rd[0].Ry), &q[1])
			bound1.Y, bound2.Y = q[1].Y, q[1].Y
			if q[1].Y > innerPts[2].Y {
				findIntersection(q[0], q[1], innerPts[3], innerPts[2], &q[1])
			}
			if q[1].X > innerPts[2].X {
				findIntersection(q[0], q[1], innerPts[1], innerPts[2], &q[1])
			}
			if q[2].Y < q[1].Y && q[2].X > q[1].X {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[2].X, q[1].Y), q[2], q[3]}
			}
		}
		if !rd[1].IsZero() {
			findIntersection(q[3], q[2], pt(q[2].X-rd[1].Rx, q[2].Y), pt(q[2].X, q[2].Y+rd[1].Ry), &q[2])
			if bound1.Y < q[2].Y {
				bound1.Y, bound2.Y = q[2].Y, q[2].Y
			}
			if q[2].Y > innerPts[3].Y {
				findIntersection(q[3], q[2], innerPts[3], innerPts[2], &q[2])
			}
			if q[2].X < innerPts[3].X {
				findIntersection(q[3], q[2], innerPts[0], innerPts[3], &q[2])
			}
			if q[2].Y > q[1].Y && q[2].X > q[1].X {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[1].X, q[2].Y), q[2], q[3]}
			}
		}

	case BoxSideLeft:
		firstMiter, secondMiter = secondMiter, firstMiter
		q = [4]geometry.PointF{outerPts[3], innerPts[3], innerPts[0], outerPts[0]}
		bound1, bound2 = pt(q[1].X, q[0].Y), pt(q[2].X, q[3].Y)
		extY = extensionLength
		if !rd[0].IsZero() {
			findIntersection(q[3], q[2], pt(q[2].X+rd[0].Rx, q[2].Y), pt(q[2].X, q[2].Y+rd[0].Ry), &q[2])
			bound1.X, bound2.X = q[2].X, q[2].X
			if q[2].Y > innerPts[2].Y {
				findIntersection(q[3], q[2], innerPts[3], innerPts[2], &q[2])
			}
			if q[2].X > innerPts[2].X {
				findIntersection(q[3], q[2], innerPts[1], innerPts[2], &q[2])
			}
			if q[2].Y < q[1].Y && q[2].X > q[1].X {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[2].X, q[1].Y), q[2], q[3]}
			}
		}
		if !rd[3].IsZero() {
			findIntersection(q[0], q[1], pt(q[1].X+rd[3].Rx, q[1].Y), pt(q[1].X, q[1].Y-rd[3].Ry), &q[1])
			if bound1.X < q[1].X {
				bound1.X, bound2.X = q[1].X, q[1].X
			}
			if q[1].Y < innerPts[1].Y {
				findIntersection(q[0], q[1], innerPts[0], innerPts[1], &q[1])
			}
			if q[1].X > innerPts[1].X {
				findIntersection(q[0], q[1], innerPts[1], innerPts[2], &q[1])
			}
			if q[2].Y < q[1].Y && q[2].X < q[1].X {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[1].X, q[2].Y), q[2], q[3]}
			}
		}

	case BoxSideBottom:
		firstMiter, secondMiter = secondMiter, firstMiter
		q = [4]geometry.PointF{outerPts[2], innerPts[2], innerPts[3], outerPts[3]}
		bound1, bound2 = pt(q[0].X, q[1].Y), pt(q[3].X, q[2].Y)
		extX = extensionLength
		if !rd[3].IsZero() {
			findIntersection(q[3], q[2], pt(q[2].X+rd[3].Rx, q[2].Y), pt(q[2].X, q[2].Y-rd[3].Ry), &q[2])
			bound1.Y, bound2.Y = q[2].Y, q[2].Y
			if q[2].Y < innerPts[1].Y {
				findIntersection(q[3], q[2], innerPts[0], innerPts[1], &q[2])
			}
			if q[2].X > innerPts[1].X {
				findIntersection(q[3], q[2], innerPts[1], innerPts[2], &q[2])
			}
			if q[2].Y < q[1].Y && q[2].X < q[1].X {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[1].X, q[2].Y), q[2], q[3]}
			}
		}
		if !rd[2].IsZero() {
			findIntersection(q[0], q[1], pt(q[1].X-rd[2].Rx, q[1].Y), pt(q[1].X, q[1].Y-rd[2].Ry), &q[1])
			if bound1.Y > q[1].Y {
				bound1.Y, bound2.Y = q[1].Y, q[1].Y
			}
			if q[1].Y < innerPts[0].Y {
				findIntersection(q[0], q[1], innerPts[0], innerPts[1], &q[1])
			}
			if q[1].X < innerPts[0].X {
				findIntersection(q[0], q[1], innerPts[0], innerPts[3], &q[1])
			}
			if q[2].X < q[1].X && q[2].Y > q[1].Y {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[2].X, q[1].Y), q[2], q[3]}
			}
		}

	case BoxSideRight:
		q = [4]geometry.PointF{outerPts[1], innerPts[1], innerPts[2], outerPts[2]}
		bound1, bound2 = pt(q[1].X, q[0].Y), pt(q[2].X, q[3].Y)
		extY = -extensionLength
		if !rd[1].IsZero() {
			findIntersection(q[0], q[1], pt(q[1].X-rd[1].Rx, q[1].Y), pt(q[1].X, q[1].Y+rd[1].Ry), &q[1])
			bound1.X, bound2.X = q[1].X, q[1].X
			if q[1].Y > innerPts[3].Y {
				findIntersection(q[0], q[1], innerPts[3], innerPts[2], &q[1])
			}
			if q[1].X < innerPts[3].X {
				findIntersection(q[0], q[1], innerPts[0], innerPts[3], &q[1])
			}
			if q[2].Y > q[1].Y && q[2].X > q[1].X {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[1].X, q[2].Y), q[2], q[3]}
			}
		}
		if !rd[2].IsZero() {
			findIntersection(q[3], q[2], pt(q[2].X-rd[2].Rx, q[2].Y), pt(q[2].X, q[2].Y-rd[2].Ry), &q[2])
			if bound1.X > q[2].X {
				bound1.X, bound2.X = q[2].X, q[2].X
			}
			if q[2].Y < innerPts[0].Y {
				findIntersection(q[3], q[2], innerPts[0], innerPts[1], &q[2])
			}
			if q[2].X < innerPts[0].X {
				findIntersection(q[3], q[2], innerPts[0], innerPts[3], &q[2])
			}
			if q[2].X < q[1].X && q[2].Y > q[1].Y {
				pentagon = []geometry.PointF{q[0], q[1], pt(q[2].X, q[1].Y), q[2], q[3]}
			}
		}
	}

	if firstMiter == secondMiter {
		if pentagon != nil && !p.inner.IsRenderable() {
			p.clipPolygon(pentagon, firstMiter == softMiter)
			return
		}
		p.clipPolygon(q[:], firstMiter == softMiter)
		return
	}

	// Different miters at the two ends: two clips, each built from three
	// sides of the quad's rectilinear bounding box and the miter edge,
	// extended along the miter so the clips overlap.
	if firstMiter != noMiter {
		var c1 geometry.PointF
		findIntersection(q[0], q[1], bound1, bound2, &c1)
		p.clipPolygon([]geometry.PointF{addPt(q[0], extX, extY), addPt(c1, extX, extY), bound2, q[3]}, firstMiter == softMiter)
	}
	if secondMiter != noMiter {
		var c2 geometry.PointF
		findIntersection(q[2], q[3], bound1, bound2, &c2)
		p.clipPolygon([]geometry.PointF{q[0], bound1, addPt(c2, -extX, -extY), addPt(q[3], -extX, -extY)}, secondMiter == softMiter)
	}
}

// fillIntRect fills an integer rect with c (GraphicsContext::FillRect).
func (r *Renderer) fillIntRect(rect intRect, c css.Color) {
	if rect.w <= 0 || rect.h <= 0 {
		return
	}
	r.setColor(c)
	r.dc.DrawRectangle(float64(rect.x), float64(rect.y), float64(rect.w), float64(rect.h))
	r.dc.Fill()
}

// fillQuad fills the quadrilateral p1-p4 (FillQuad, box_border_painter.cc:845-860).
func (r *Renderer) fillQuad(pts [4]geometry.PointF, c css.Color) {
	r.setColor(c)
	addPolygonPath(r.dc, pts[:])
	r.dc.Fill()
}

// DrawBoxSide paints one straight border side filling snappedEdgeRect.
// Mirrors BoxBorderPainter::DrawBoxSide (box_border_painter.cc:2497-2509 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) DrawBoxSide(snappedEdgeRect intRect, side BoxSide, c css.Color, style css.BorderStyle) {
	if style == css.BorderStyleNone || style == css.BorderStyleHidden {
		return
	}
	r.drawLineForBoxSide(snappedEdgeRect.x, snappedEdgeRect.y, snappedEdgeRect.right(), snappedEdgeRect.bottom(), side, c, style, 0, 0)
}

// drawLineForBoxSide paints one straight side in the rect (x1,y1)-(x2,y2);
// adjacentWidth1/2 are the neighbouring sides' widths when that end needs
// a drawn miter, else 0 (DrawLineForBoxSide, box_border_painter.cc:921-980
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r *Renderer) drawLineForBoxSide(x1, y1, x2, y2 int, side BoxSide, c css.Color, style css.BorderStyle, adjacentWidth1, adjacentWidth2 int) {
	thickness, length := y2-y1, x2-x1
	if side == BoxSideLeft || side == BoxSideRight {
		thickness, length = x2-x1, y2-y1
	}
	if length <= 0 || thickness <= 0 {
		return
	}
	style = effectiveBorderStyle(style, thickness)
	switch style {
	case css.BorderStyleDotted, css.BorderStyleDashed:
		r.drawDashedOrDottedBoxSide(x1, y1, x2, y2, side, c, thickness, style)
	case css.BorderStyleDouble:
		r.drawDoubleBoxSide(x1, y1, x2, y2, length, side, c, thickness, adjacentWidth1, adjacentWidth2)
	default:
		r.drawSolidBoxSide(x1, y1, x2, y2, side, c, adjacentWidth1, adjacentWidth2)
	}
}

// drawDashedOrDottedBoxSide strokes the side's centerline
// (DrawDashedOrDottedBoxSide, box_border_painter.cc:562-598) through
// louis14's dash/dot stand-ins for DrawLineWithStyle.
func (r *Renderer) drawDashedOrDottedBoxSide(x1, y1, x2, y2 int, side BoxSide, c css.Color, thickness int, style css.BorderStyle) {
	r.setColor(c)
	t := float64(thickness)
	draw := r.drawDashedLine
	if style == css.BorderStyleDotted {
		draw = r.drawDottedLine
	}
	if side == BoxSideTop || side == BoxSideBottom {
		mid := float64(y1) + t/2
		draw(float64(x1), mid, float64(x2), mid, t)
		return
	}
	mid := float64(x1) + t/2
	draw(mid, float64(y1), mid, float64(y2), t)
}

// drawDoubleBoxSide paints a straight double side as two thirds-thick
// stripes, mitered toward the adjacent sides when they are given
// (DrawDoubleBoxSide, box_border_painter.cc:666-771).
func (r *Renderer) drawDoubleBoxSide(x1, y1, x2, y2, length int, side BoxSide, c css.Color, thickness, adj1, adj2 int) {
	third := (thickness + 1) / 3
	if adj1 == 0 && adj2 == 0 {
		switch side {
		case BoxSideTop, BoxSideBottom:
			r.fillIntRect(intRect{x1, y1, length, third}, c)
			r.fillIntRect(intRect{x1, y2 - third, length, third}, c)
		default:
			r.fillIntRect(intRect{x1, y1, third, length}, c)
			r.fillIntRect(intRect{x2 - third, y1, third, length}, c)
		}
		return
	}
	bigThird := func(w int) int {
		if w > 0 {
			return (w + 1) / 3
		}
		return (w - 1) / 3
	}
	a1, a2 := bigThird(adj1), bigThird(adj2)
	solid := css.BorderStyleSolid
	neg1, neg2 := max((-adj1*2+1)/3, 0), max((-adj2*2+1)/3, 0)
	pos1, pos2 := max((adj1*2+1)/3, 0), max((adj2*2+1)/3, 0)
	switch side {
	case BoxSideTop:
		r.drawLineForBoxSide(x1+neg1, y1, x2-neg2, y1+third, side, c, solid, a1, a2)
		r.drawLineForBoxSide(x1+pos1, y2-third, x2-pos2, y2, side, c, solid, a1, a2)
	case BoxSideLeft:
		r.drawLineForBoxSide(x1, y1+neg1, x1+third, y2-neg2, side, c, solid, a1, a2)
		r.drawLineForBoxSide(x2-third, y1+pos1, x2, y2-pos2, side, c, solid, a1, a2)
	case BoxSideBottom:
		r.drawLineForBoxSide(x1+pos1, y1, x2-pos2, y1+third, side, c, solid, a1, a2)
		r.drawLineForBoxSide(x1+neg1, y2-third, x2-neg2, y2, side, c, solid, a1, a2)
	case BoxSideRight:
		r.drawLineForBoxSide(x1, y1+pos1, x1+third, y2-pos2, side, c, solid, a1, a2)
		r.drawLineForBoxSide(x2-third, y1+neg1, x2, y2-neg2, side, c, solid, a1, a2)
	}
}

// drawSolidBoxSide fills a straight side: a rect, or a quad mitered toward
// the adjacent sides (DrawSolidBoxSide, box_border_painter.cc:862-919).
func (r *Renderer) drawSolidBoxSide(x1, y1, x2, y2 int, side BoxSide, c css.Color, adj1, adj2 int) {
	if adj1 == 0 && adj2 == 0 {
		r.fillIntRect(intRect{x1, y1, x2 - x1, y2 - y1}, c)
		return
	}
	pt := func(x, y int) geometry.PointF { return geometry.PointF{X: float64(x), Y: float64(y)} }
	var q [4]geometry.PointF
	switch side {
	case BoxSideTop:
		q = [4]geometry.PointF{pt(x1+max(-adj1, 0), y1), pt(x1+max(adj1, 0), y2), pt(x2-max(adj2, 0), y2), pt(x2-max(-adj2, 0), y1)}
	case BoxSideBottom:
		q = [4]geometry.PointF{pt(x1+max(adj1, 0), y1), pt(x1+max(-adj1, 0), y2), pt(x2-max(-adj2, 0), y2), pt(x2-max(adj2, 0), y1)}
	case BoxSideLeft:
		q = [4]geometry.PointF{pt(x1, y1+max(-adj1, 0)), pt(x1, y2-max(-adj2, 0)), pt(x2, y2-max(adj2, 0)), pt(x2, y1+max(adj1, 0))}
	case BoxSideRight:
		q = [4]geometry.PointF{pt(x1, y1+max(adj1, 0)), pt(x1, y2-max(adj2, 0)), pt(x2, y2-max(-adj2, 0)), pt(x2, y1+max(-adj1, 0))}
	}
	r.fillQuad(q, c)
}
