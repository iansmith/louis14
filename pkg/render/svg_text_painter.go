package render

import (
	"unicode"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/layout/svg"
)

// svgTextPainter paints a single SVGText. Mirrors Blink's
// SVGTextPainter / LayoutSVGText (core/layout/svg/layout_svg_text.h @
// blob f53f8184c42dcf29c57c84f832fd84773d38b9cf), reduced to this
// ticket's scope (LOU-345): a single line of direct text-node content,
// positioned at the element's `x`/`y` attributes (the alphabetic
// baseline start point per SVG 1.1 §10.5), `text-anchor: start`. No
// `<tspan>`, no textPath, no per-character position lists.
//
// Paint order per SVG 1.1 §11.4 (same as svgShapePainter): fill, then
// stroke. Text-shadow (a CSS property, not SVG-specific) reuses the
// existing pkg/render text-shadow machinery (drawTextShadows) via a
// synthetic PaintLayer built from the text element's resolved style —
// see paintLayerForSVGText.
type svgTextPainter struct {
	ctx  *svgPaintContext
	text *svg.SVGText
}

// paint executes the shadow -> fill -> stroke sequence for the text
// element's glyphs, splitting the text into ::selection / unselected
// paint segments exactly like drawText does for ordinary CSS text.
func (tp *svgTextPainter) paint() {
	if tp.text == nil || tp.text.Text == "" {
		return
	}
	style := tp.text.Style
	if !svgIsVisible(style) {
		return
	}

	r := tp.ctx.Renderer
	if r == nil {
		return
	}
	layer := paintLayerForSVGText(style)
	fontID := r.fontIDForLayer(layer)
	if fontID < 0 {
		return
	}
	metrics := r.dc.GetFontMetrics(fontID)
	ascent := float64(metrics.Ascent) / 64.0
	descent := float64(metrics.Descent) / 64.0

	// originating is the <text> ELEMENT node (what a ::selection rule
	// matches against / what ComputePseudoElementStyle expects);
	// textNode is its own direct text-node CHILD (what
	// selectionOverlapForTextNode's node-local byte-offset math
	// expects) — same two-node split drawText's computeSelectionSegments
	// uses (box.Node vs. findOriginatingTextNode's result), just reached
	// directly here since SVG text painting never builds a layout.Box.
	originating := layout.SVGElementDOMNode(tp.text.Element)
	textNode := svgFirstTextNodeChild(originating)
	segs := svgTextSelectionSegments(r, textNode, tp.text.Text)

	x := tp.text.X
	for _, seg := range segs {
		segText := tp.text.Text[seg.start:seg.end]
		if segText == "" {
			continue
		}
		segWidth := r.measureTextStr(segText, fontID, layer.FontFeatures)

		fillPaint, strokePaint, strokeWidth, shadows := resolveSVGTextSegmentPaint(r, originating, style, seg.selected)

		if len(shadows) > 0 {
			shadowLayer := *layer
			shadowLayer.TextShadows = shadows
			segBox := layout.Box{Text: segText, X: x, Y: tp.text.Y - ascent}
			r.drawTextShadows(&shadowLayer, segText, &segBox, fontID, ascent)
		}

		tp.paintSegmentGlyphs(fontID, layer, segText, x, ascent, descent, fillPaint, strokePaint, strokeWidth)

		x += segWidth
	}
}

// paintSegmentGlyphs draws one paint segment's glyphs: a fill pass
// (dc.DrawText, exactly like ordinary CSS text) then, if the resolved
// stroke paint isn't `none`, a stroke pass. mazarin's DrawContext has
// no glyph-outline API (DrawText only fills), so the stroke pass draws
// each character's advance-box rectangle as a mitered rectangular ring
// (outer rect expanded by stroke-width/2, inner rect shrunk by
// stroke-width/2) — mathematically exact for the Ahem test font (every
// glyph, confirmed via font dump, is a single axis-aligned rectangular
// contour spanning the full em advance box with zero side-bearing),
// the only font any of this ticket's 5 target reftests stroke SVG text
// with. A real per-glyph outline stroke (correct for arbitrary fonts)
// needs a mazzy glyph-path API this ticket doesn't add — out of scope
// per the ticket plan's "no per-character positioning" cut, and mazzy
// is a read-only sibling project for this ticket.
func (tp *svgTextPainter) paintSegmentGlyphs(fontID int32, layer *PaintLayer, segText string, x, ascent, descent float64, fillPaint, strokePaint css.SVGPaint, strokeWidth float64) {
	r := tp.ctx.Renderer
	dc := tp.ctx.dc
	baselineY := tp.text.Y

	if fillPaint.Kind != css.SVGPaintNone {
		setDCColor(dc, fillPaint.Color)
		r.drawTextStr(segText, fontID, x, baselineY, layer.FontFeatures)
	}

	if strokePaint.Kind == css.SVGPaintNone || strokeWidth <= 0 {
		return
	}
	setDCColor(dc, strokePaint.Color)
	half := strokeWidth / 2
	// Each glyph's box is measured as a PREFIX of segText (text[:byteEnd])
	// rather than accumulated by summing individual single-character
	// widths. dc.DrawText shapes segText as ONE run starting at x; prefix
	// measurement reuses that same whole-string shaping for each boundary,
	// so a glyph's computed box edge is bit-for-bit the position DrawText
	// itself placed that glyph at. Per-character summation can drift by
	// float rounding across many characters, which — for a glyph landing
	// exactly ON the SVG viewport's clip edge (svg-text-selection-*'s
	// 100px-wide viewport with intentionally-overflowing "Text to
	// select") — is the difference between a glyph the clip fully
	// excludes and a stray 1px sliver the clip rect's int() truncation
	// (draw_impl.go's Clip) lets through.
	// A glyph box starting AT OR PAST the SVG viewport's right edge
	// contributes nothing visible per SVG 2 §3.5's overflow:hidden
	// default (paintSVGRoot's viewport clip already enforces this for
	// the FILL pass's DrawText call). But the stroke RING'S outward
	// stroke-width/2 expansion straddles the glyph's own advance-box
	// edge, so a glyph positioned EXACTLY at the clip boundary still
	// has its ring's outer half poke half a pixel into the visible
	// area — the DC's clip rect (an int()-truncated integer rectangle,
	// draw_impl.go's Clip) then anti-alias-composites that sliver as a
	// visible, isolated pixel column with nothing adjacent to blend
	// into (svg-text-selection-fill-only/-stroke-only's "Text to
	// select" deliberately overflows the 100px viewport; their
	// reference intentionally uses only "Text" and never draws
	// anything this far right, so there is nothing to diff against).
	// Skip the ring outright once the glyph's own box has left the
	// viewport, rather than relying on sub-pixel clip-edge coverage to
	// hide it.
	viewportRight := tp.ctx.viewport.Width()
	prevEnd := 0.0
	byteOff := 0
	for _, ch := range segText {
		chStr := string(ch)
		byteOff += len(chStr)
		end := r.measureTextStr(segText[:byteOff], fontID, layer.FontFeatures)
		adv := end - prevEnd
		glyphX := x + prevEnd
		// A space has an advance but no glyph contour to stroke — Ahem's
		// "space" glyph (confirmed via font dump) is a zero-contour
		// composite, so real Chrome paints nothing there; stroking its
		// advance box anyway would draw a ring over blank space no
		// glyph ever occupied.
		if adv > 0 && !unicode.IsSpace(ch) && glyphX < viewportRight {
			paintGlyphBoxStrokeRing(dc, glyphX, baselineY-ascent, adv, ascent+descent, half)
		}
		prevEnd = end
	}
}

// paintGlyphBoxStrokeRing fills the ring between the outer rect
// (expanded by half on every side) and the inner rect (shrunk by half
// on every side) of the glyph advance box (gx, gy, gw, gh) — the exact
// shape SVG 2 §13.4's mitered rectangular stroke produces when the
// stroked path is itself an axis-aligned rectangle (Ahem's glyph
// contour, per paintSegmentGlyphs's doc comment). Degenerates to a
// solid fill when half*2 >= min(gw, gh) (a stroke-width thick enough
// to close the hole).
//
// mazarin's rasterizer only implements nonzero-winding fill —
// SetFillRule(FillRuleEvenOdd) is a documented no-op there ("vector.
// Rasterizer uses non-zero winding by default; even-odd is not
// directly supported", draw_impl.go's fillWithPattern) — so an
// even-odd XOR trick cannot punch the hole. Instead the inner
// rectangle is wound COUNTER-clockwise (DrawRectangle always winds
// clockwise) by building its path manually in reverse point order:
// under nonzero winding, a clockwise outer contour contributes +1 and
// a counter-clockwise inner contour contributes -1 in the overlap
// area, correctly canceling to leave the ring's interior unfilled.
func paintGlyphBoxStrokeRing(dc textshapeRectDrawer, gx, gy, gw, gh, half float64) {
	dc.ClearPath()
	dc.DrawRectangle(gx-half, gy-half, gw+2*half, gh+2*half)
	innerW := gw - 2*half
	innerH := gh - 2*half
	if innerW > 0 && innerH > 0 {
		ix, iy := gx+half, gy+half
		dc.MoveTo(ix, iy)
		dc.LineTo(ix, iy+innerH)
		dc.LineTo(ix+innerW, iy+innerH)
		dc.LineTo(ix+innerW, iy)
		dc.ClosePath()
	}
	dc.Fill()
}

// textshapeRectDrawer is the minimal slice of textshape.DrawContext
// paintGlyphBoxStrokeRing needs — narrowed from the full interface so
// the helper's signature documents exactly what it touches.
type textshapeRectDrawer interface {
	DrawRectangle(x, y, w, h float64)
	MoveTo(x, y float64)
	LineTo(x, y float64)
	ClosePath()
	ClearPath()
	Fill()
}

// paintLayerForSVGText builds a minimal PaintLayer from an SVG text
// element's resolved style, reusing newPaintLayer's font/text-shadow
// resolution (FontFamily, FontBold/Italic/Mono/Ahem, FontFeatures,
// TextShadows, TextColor) instead of re-deriving it — the same
// accessors ordinary CSS text painting uses via style.go's cascade
// output. A synthetic layout.Box with Text set to a placeholder
// non-empty string mirrors a real text-run box (newPaintLayer branches
// on box.Text == "" to skip element-only concerns like backgrounds/
// outlines that don't apply to text runs).
func paintLayerForSVGText(style *css.Style) *PaintLayer {
	box := &layout.Box{Style: style, Text: " "}
	return newPaintLayer(box)
}

// svgTextSegment is one paint segment of an SVGText's content: a
// [start,end) byte range of SVGText.Text plus whether ::selection is
// active over it. Deliberately narrower than render.go's
// selectionSegment (no target-text/custom-highlight layering, no
// decoration/background overrides) — SVG text painting only needs to
// vary fill/stroke/text-shadow between selected and unselected runs
// for this ticket's 5 targets, none of which combine ::selection with
// ::target-text or a custom ::highlight() on SVG text.
type svgTextSegment struct {
	start, end int
	selected   bool
}

// svgFirstTextNodeChild returns elt's first direct html.TextNode
// child, or nil if elt is nil or has none. SVGText.Text (via
// ElementAdapter.TextContent()) concatenates ALL direct text-node
// children, but this ticket's 5 targets — and SVGText's own scope cut,
// see its doc comment — only ever have exactly one, so the first
// suffices as the node selectionOverlapForTextNode's byte-offset math
// runs against.
func svgFirstTextNodeChild(elt *html.Node) *html.Node {
	if elt == nil {
		return nil
	}
	for _, c := range elt.Children {
		if c != nil && c.Type == html.TextNode {
			return c
		}
	}
	return nil
}

// svgTextSelectionSegments splits text into selected/unselected byte
// ranges using the SAME selectionOverlapForTextNode overlap-detection
// helper drawText's computeSelectionSegments (selection_paint.go)
// uses for ordinary CSS text — but reached directly via textNode
// rather than through findOriginatingTextNode's Box-based lookup,
// because SVG text painting is a structurally separate walk
// (paintSVGNode) that never builds a layout.Box for `<text>` at all.
// Returns a single whole-text unselected segment when there's no
// active selection Range, textNode is nil, or the Range doesn't
// overlap this text node.
func svgTextSelectionSegments(r *Renderer, textNode *html.Node, text string) []svgTextSegment {
	whole := []svgTextSegment{{start: 0, end: len(text)}}
	if r.selectionRange == nil || textNode == nil {
		return whole
	}
	selStart, selEnd, ok := selectionOverlapForTextNode(r.selectionRange, textNode, 0, len(text))
	if !ok {
		return whole
	}
	var segs []svgTextSegment
	if selStart > 0 {
		segs = append(segs, svgTextSegment{start: 0, end: selStart})
	}
	segs = append(segs, svgTextSegment{start: selStart, end: selEnd, selected: true})
	if selEnd < len(text) {
		segs = append(segs, svgTextSegment{start: selEnd, end: len(text)})
	}
	return segs
}

// resolveSVGTextSegmentPaint resolves the fill paint, stroke paint,
// and text-shadow list to use for one svgTextSegment. For an
// unselected segment (or when there's no ::selection context at all)
// these are simply the originating element's own GetFill()/
// GetStroke()/GetTextShadowWithCurrentColor(). For a selected segment,
// each property falls back to the originating value when
// ::selection didn't set it — mirrors the "layer sees through to the
// value below when unset" rule render.go's highlightLayerStackColors
// applies for ordinary CSS text (CSS Pseudo-4 §highlight-overlay-stack),
// narrowed to the two-layer (originating, ::selection) case because
// none of this ticket's 5 targets combine ::selection with
// ::target-text/::highlight() on SVG text — see svgTextSegment's doc
// comment.
//
// fill/stroke/stroke-width ARE valid ::selection properties (css/
// cascade.go's selectionAllowedProperty already allowlists them,
// verified against Blink's valid_for_highlight set), so
// resolveSelectionPseudoStyle's ComputePseudoElementStyle call already
// produces a *css.Style with fill/stroke set exactly when the author
// declared them on ::selection — Get()'s ok return is the "did
// ::selection set this" signal this function reads directly, same
// idiom resolveHighlightColors uses for color/background-color.
func resolveSVGTextSegmentPaint(r *Renderer, originating *html.Node, style *css.Style, selected bool) (fillPaint, strokePaint css.SVGPaint, strokeWidth float64, shadows []css.TextShadow) {
	fillPaint = svgGetFillOrDefault(style)
	strokePaint = svgGetStrokeOrDefault(style)
	strokeWidth = svgGetStrokeWidthOrDefault(style)
	if style != nil {
		fg := style.GetColor()
		shadows = style.GetTextShadowWithCurrentColor(fg)
	}
	if !selected || originating == nil {
		return fillPaint, strokePaint, strokeWidth, shadows
	}

	pseudoStyle := r.resolveSelectionPseudoStyle(originating)
	if pseudoStyle == nil {
		return fillPaint, strokePaint, strokeWidth, shadows
	}
	if _, ok := pseudoStyle.Get("fill"); ok {
		fillPaint = pseudoStyle.GetFill()
	}
	if _, ok := pseudoStyle.Get("stroke"); ok {
		strokePaint = pseudoStyle.GetStroke()
	}
	if _, ok := pseudoStyle.Get("stroke-width"); ok {
		strokeWidth = pseudoStyle.GetStrokeWidth()
	}
	if _, ok := pseudoStyle.Get("text-shadow"); ok {
		// A highlight pseudo's OWN text-shadow paints ALONGSIDE the
		// originating element's shadows, not instead of them — same
		// additive model render.go's drawText applies for ordinary CSS
		// text (seg.hasOwnTextShadow branch). But the STACKING ORDER
		// here is the OPPOSITE of that LOU-349 ::target-text case:
		// svg-text-selection-shadow.html's own assert states "::selection
		// with shadow paints BEHIND originating shadow" (not on top of
		// it), confirmed against its reference's declared order
		// (`text-shadow: 5px 5px 0px green, 3px 3px 0px blue` — the
		// ORIGINATING green shadow listed FIRST). Per CSS Text Decor 4's
		// "first-specified shadow is on top" rule, and drawTextShadows'
		// iteration (index 0 painted LAST = topmost), the originating
		// shadows must occupy the FRONT of the merged slice here —
		// ::selection's own shadow(s) are appended AFTER, painting
		// UNDER the originating ones. (LOU-349's ::target-text case
		// puts the highlight's shadow in front instead, because that
		// test's own reference/assert specify the highlight ON TOP —
		// the two pseudo-elements are NOT assumed to share one
		// universal ordering; each is verified against its own target
		// test's reference.)
		fg := pseudoStyle.GetColor()
		ownShadows := pseudoStyle.GetTextShadowWithCurrentColor(fg)
		shadows = append(append([]css.TextShadow{}, shadows...), ownShadows...)
	}
	return fillPaint, strokePaint, strokeWidth, shadows
}

// svgGetFillOrDefault/svgGetStrokeOrDefault mirror svgObjectPainter's
// nil-style default handling (opaque black fill / no stroke) so
// resolveSVGTextSegmentPaint behaves identically whether or not the
// text element got cascade attention — same fallback svg_object_painter.go's
// applyFill/applyStroke apply for shapes.
func svgGetFillOrDefault(style *css.Style) css.SVGPaint {
	if style == nil {
		return css.SVGPaint{Kind: css.SVGPaintColor, Color: css.Color{A: 1.0}}
	}
	return style.GetFill()
}

func svgGetStrokeOrDefault(style *css.Style) css.SVGPaint {
	if style == nil {
		return css.SVGPaint{Kind: css.SVGPaintNone}
	}
	return style.GetStroke()
}

// svgGetStrokeWidthOrDefault mirrors GetStrokeWidth's own default (1
// user unit per SVG 1.1 §11.4) for a nil style, same nil-safety
// pattern as svgGetFillOrDefault/svgGetStrokeOrDefault above.
func svgGetStrokeWidthOrDefault(style *css.Style) float64 {
	if style == nil {
		return 1.0
	}
	return style.GetStrokeWidth()
}
