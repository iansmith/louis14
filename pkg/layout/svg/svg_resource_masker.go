package svg

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGMaskType enumerates the per-pixel mask-value extraction rule for
// an SVG `<mask>` resource. Mirrors Blink's EMaskType
// (core/style/style_svg_data.h).
//
//   - SVGMaskTypeLuminance (default per SVG 1.1 §14.4): the mask
//     value at pixel P is the luminance of the rendered mask subtree
//     at P, modulated by the mask alpha. Computed as
//     0.2125*R + 0.7154*G + 0.0721*B (Rec.709 weights, per SVG 1.1).
//   - SVGMaskTypeAlpha (CSS Masking 1 addition): the mask value at
//     pixel P is the rendered mask subtree's alpha at P.
//
// `mask-type` is a presentation attribute AND a CSS property on the
// `<mask>` element itself, per CSS Masking 1 §6.3.
type SVGMaskType int

const (
	SVGMaskTypeLuminance SVGMaskType = iota
	SVGMaskTypeAlpha
)

// SVGResourceMasker is the layout node for a `<mask>` resource element.
// Mirrors Blink's LayoutSVGResourceMasker
// (core/layout/svg/layout_svg_resource_masker.{h,cc}).
//
// Per SVG 1.1 §14.4 a `<mask>` defines a rectangular mask region (x,
// y, width, height) and a child subtree rendered into that region;
// pixel-wise luminance (or alpha) of the rendered subtree becomes the
// mask values that gate-modulate the alpha of the referencing
// element's content during compositing (kDstIn: dst.A *= mask.A).
//
// Two coordinate spaces:
//
//   - MaskUnits (default objectBoundingBox) decides how x/y/width/
//     height are interpreted: fractions of the referencing element's
//     bbox (objectBoundingBox) or absolute user-space coords
//     (userSpaceOnUse).
//   - MaskContentUnits (default userSpaceOnUse — REVERSE of MaskUnits)
//     decides how the children's coords are interpreted within the
//     mask region: absolute user-space (userSpaceOnUse) or normalized
//     to a (0..1, 0..1) bbox-aligned coordinate system.
//
// Phase 5 implements Rasterize() which returns an *image.Alpha buffer
// representing the resolved mask alpha at the target's device-pixel
// resolution, ready to multiply pixel-by-pixel against the
// referencing element's buffer.
type SVGResourceMasker struct {
	// ResourceID is the mask's `id` attribute.
	ResourceID string

	// MaskUnits is the coordinate-space mode for the mask region's
	// x/y/width/height attributes. Default objectBoundingBox per
	// SVG 1.1 §14.4.
	MaskUnits SVGUnitType

	// MaskContentUnits is the coordinate-space mode for the mask's
	// child subtree's positional attributes. Default userSpaceOnUse
	// (reversed from MaskUnits).
	MaskContentUnits SVGUnitType

	// X, Y, Width, Height are the mask region's bounding box. Stored
	// unresolved because their resolution depends on MaskUnits and
	// (for objectBoundingBox) the referencing element's bbox at paint
	// time. Defaults per SVG 1.1 §14.4 are -10%, -10%, 120%, 120%
	// (objectBoundingBox); userSpaceOnUse has no spec default for
	// X/Y/W/H — the spec leaves them unspecified, which Blink treats
	// as "use the spec default values resolved as objectBoundingBox
	// fractions then projected onto the bbox" (i.e. the -10%/-10%/
	// 120%/120% defaults apply uniformly when the attribute is
	// missing). Phase 5 mirrors that.
	X, Y, Width, Height SVGGradientLength

	// MaskType is the luminance-vs-alpha discrimination from SVG 1.1
	// `mask-type` attribute / CSS `mask-type` property.
	MaskType SVGMaskType

	// Children are the SVG children of the `<mask>` element in DOM
	// order, rasterized at paint time to produce the mask buffer.
	Children []SVGNode

	// elementStyle is the mask element's resolved style.
	elementStyle *css.Style
}

// ID implements the SVG resource interface (registry lookup key).
func (m *SVGResourceMasker) ID() string { return m.ResourceID }

// ObjectBoundingBox returns an empty bbox.
func (m *SVGResourceMasker) ObjectBoundingBox() geometry.RectF { return geometry.RectF{} }

// LocalTransform returns identity.
func (m *SVGResourceMasker) LocalTransform() geometry.AffineTransform { return geometry.Identity() }

// UpdateSVGLayout is a no-op for mask resource elements.
func (m *SVGResourceMasker) UpdateSVGLayout(SVGLayoutInfo) SVGLayoutResult {
	return SVGLayoutResult{}
}

// Paint is a no-op — render-side svg_mask_painter walks the children.
func (m *SVGResourceMasker) Paint(*SVGPaintContext) {}

// Style returns the mask element's resolved style (or nil).
func (m *SVGResourceMasker) Style() *css.Style { return m.elementStyle }

// ResourceBoundingBox returns the mask region's user-space rect for
// the given referencing target. Mirrors Blink's
// LayoutSVGResourceMasker::ResourceBoundingBox.
//
// Resolution rule (SVG 1.1 §14.4 + CSS Masking 1 §6.4):
//   - In objectBoundingBox mode: x/y/width/height are fractions of
//     `target`; default -10%/-10%/120%/120% if attributes are absent.
//     The returned rect is in `target`'s user-space.
//   - In userSpaceOnUse mode: attributes are user-space lengths; the
//     defaults still mirror the objectBoundingBox proportions, applied
//     against `target` for back-compat with what Blink does when the
//     attributes are missing in userSpaceOnUse.
//
// `lengthCtx` is the length context active in the mask's declaration
// scope — used for percent resolution in userSpaceOnUse mode.
func (m *SVGResourceMasker) ResourceBoundingBox(target geometry.RectF, lengthCtx SVGLengthContext) geometry.RectF {
	// Default values per SVG 1.1 §14.4.
	defX := -0.10
	defY := -0.10
	defW := 1.20
	defH := 1.20

	switch m.MaskUnits {
	case SVGUnitObjectBoundingBox:
		x := m.X.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeWidth, lengthCtx, defX)
		y := m.Y.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeHeight, lengthCtx, defY)
		w := m.Width.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeWidth, lengthCtx, defW)
		h := m.Height.ResolveAgainst(SVGUnitObjectBoundingBox, LengthModeHeight, lengthCtx, defH)
		return geometry.NewRectF(
			target.X()+x*target.Width(),
			target.Y()+y*target.Height(),
			w*target.Width(),
			h*target.Height(),
		)
	case SVGUnitUserSpaceOnUse:
		// userSpaceOnUse: attribute values are in user units. Defaults
		// per Blink: -10%/-10%/120%/120% of `target`, projected.
		x := target.X() + defX*target.Width()
		y := target.Y() + defY*target.Height()
		w := defW * target.Width()
		h := defH * target.Height()
		if m.X.HasValue {
			x = m.X.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeWidth, lengthCtx, x)
		}
		if m.Y.HasValue {
			y = m.Y.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeHeight, lengthCtx, y)
		}
		if m.Width.HasValue {
			w = m.Width.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeWidth, lengthCtx, w)
		}
		if m.Height.HasValue {
			h = m.Height.ResolveAgainst(SVGUnitUserSpaceOnUse, LengthModeHeight, lengthCtx, h)
		}
		return geometry.NewRectF(x, y, w, h)
	}
	return target
}

// NewSVGResourceMasker builds an SVGResourceMasker from a `<mask>`
// ElementAdapter. The child subtree is built lazily — Phase 5 stores
// the ElementAdapter form so the painter can re-resolve coordinates
// when the target bbox is known. Mirrors Blink's
// LayoutSVGResourceMasker::UpdateSVGLayout.
func NewSVGResourceMasker(elt ElementAdapter, parentCtx SVGLengthContext, styleResolver StyleResolver) *SVGResourceMasker {
	if elt == nil {
		return nil
	}
	m := &SVGResourceMasker{
		MaskUnits:        SVGUnitObjectBoundingBox, // SVG 1.1 §14.4 default
		MaskContentUnits: SVGUnitUserSpaceOnUse,    // SVG 1.1 §14.4 default
		MaskType:         SVGMaskTypeLuminance,     // SVG 1.1 §14.4 default
	}
	if id, ok := elt.Attribute("id"); ok {
		m.ResourceID = strings.TrimSpace(id)
	}
	if v, ok := elt.Attribute("x"); ok {
		m.X = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("y"); ok {
		m.Y = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("width"); ok {
		m.Width = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("height"); ok {
		m.Height = parseGradientLength(v)
	}
	if v, ok := elt.Attribute("maskUnits"); ok {
		m.MaskUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	} else if v, ok := elt.Attribute("maskunits"); ok {
		m.MaskUnits = ParseGradientUnits(v, SVGUnitObjectBoundingBox)
	}
	if v, ok := elt.Attribute("maskContentUnits"); ok {
		m.MaskContentUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	} else if v, ok := elt.Attribute("maskcontentunits"); ok {
		m.MaskContentUnits = ParseGradientUnits(v, SVGUnitUserSpaceOnUse)
	}
	// mask-type may live on the element as an attribute or in the
	// style attribute. The style resolver result wins (CSS prevails
	// over presentation attribute per the SVG 2 cascade), but the
	// SVG-attribute fallback is checked first.
	if v, ok := elt.Attribute("mask-type"); ok {
		m.MaskType = parseMaskType(v, m.MaskType)
	}
	if styleResolver != nil {
		m.elementStyle = styleResolver(elt)
		if m.elementStyle != nil {
			if v, ok := m.elementStyle.Get("mask-type"); ok {
				m.MaskType = parseMaskType(v, m.MaskType)
			}
		}
	}
	for _, child := range elt.SVGChildren() {
		if node := BuildSVGTree(child, parentCtx, styleResolver); node != nil {
			m.Children = append(m.Children, node)
		}
	}
	return m
}

// parseMaskType parses a `mask-type` value. Unknown / missing values
// fall back to `def`.
func parseMaskType(s string, def SVGMaskType) SVGMaskType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "luminance":
		return SVGMaskTypeLuminance
	case "alpha":
		return SVGMaskTypeAlpha
	}
	return def
}
