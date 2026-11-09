package svg

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGImage is an SVG <image>: a leaf whose box is its x, y, width and
// height. Mirrors Blink's LayoutSVGImage (core/layout/svg/
// layout_svg_image.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). It does
// not yet draw its image (LOU-797 follow-up): SVGImagePainter's early
// returns and the box are what it carries.
type SVGImage struct {
	// Element is the <image> element.
	Element ElementAdapter
	// Style is the element's computed style.
	Style *css.Style
	// Viewport is the size of the nearest SVG viewport, the reference box
	// of the element's CSS transform for transform-box: view-box.
	Viewport geometry.SizeF
	// Href is the image URL ("" when there is none).
	Href string

	box       geometry.RectF
	transform geometry.AffineTransform
}

// NewSVGImage builds an SVGImage from an <image> element with its computed
// style, resolving its x, y, width and height against lengthCtx
// (LayoutSVGImage::UpdateBoundingBox, layout_svg_image.cc:140-153 @
// a9f50e52). They are the computed CSS x, y, width and height, which the
// attributes map to (svg_image_element.cc:41-64); without a style (only a
// tree built with no style resolver), the attributes. An auto or missing
// width or height is 0 (Resolve gives 0 for both): louis14 does not
// load the image, so it has no natural size to take (CalculateObjectSize's
// no-image branch, layout_svg_image.cc:110-112, uses the style size as is).
func NewSVGImage(elt ElementAdapter, style *css.Style, lengthCtx SVGLengthContext) *SVGImage {
	if elt == nil {
		return nil
	}
	img := &SVGImage{Element: elt, Style: style, Viewport: lengthCtx.ViewportSize(), Href: ElementHref(elt)}
	length := func(name string, mode SVGLengthMode) float64 {
		if style == nil {
			v, _ := elt.Attribute(name)
			return lengthCtx.Resolve(v, mode)
		}
		// A CSS <length-percentage>, units and math functions included,
		// its percentages of the SVG viewport (SVGViewportResolver): width
		// for x and width, height for y and height. Auto is 0.
		base := img.Viewport.Width
		if mode == LengthModeHeight {
			base = img.Viewport.Height
		}
		v, _ := style.ResolveLengthPercentage(name, base)
		return v
	}
	img.box = geometry.NewRectF(
		length("x", LengthModeWidth), length("y", LengthModeHeight),
		max(length("width", LengthModeWidth), 0), max(length("height", LengthModeHeight), 0))
	v, _ := elt.Attribute("transform")
	img.transform = ParseSVGTransform(v)
	return img
}

// HasImage reports whether the element has an image resource: one is made
// for any href, and stays when it fails to load (LayoutImageResource::
// HasImage, core/layout/layout_image_resource.h:55 @ a9f50e52).
func (s *SVGImage) HasImage() bool { return s.Href != "" }

// ObjectBoundingBox returns the image's box.
func (s *SVGImage) ObjectBoundingBox() geometry.RectF { return s.box }

// StrokeBoundingBox returns the image's box: an image has no stroke
// (LayoutSVGImage::StrokeBoundingBox, layout_svg_image.h:86-89 @ a9f50e52).
func (s *SVGImage) StrokeBoundingBox() geometry.RectF { return s.box }

// LocalTransform returns the parsed `transform` attribute; a specified CSS
// transform replaces it at paint time (ElementTransform).
func (s *SVGImage) LocalTransform() geometry.AffineTransform { return s.transform }

// UpdateSVGLayout is a no-op: the box is resolved at construction, as for
// SVGShape.
func (s *SVGImage) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	return SVGLayoutResult{}
}

// Paint is a no-op: pkg/render's SVG image painter paints it.
func (s *SVGImage) Paint(ctx *SVGPaintContext) {}

// ElementHref returns elt's href, trimmed: the SVG 2 `href` attribute when it
// is specified, even empty, else the SVG 1.1 `xlink:href`; "" when neither
// is (SVGAnimatedHref::BackingString / UseXLink,
// core/svg/svg_animated_href.cc:71-81 @ a9f50e52).
func ElementHref(elt ElementAdapter) string {
	v, ok := elt.Attribute("href")
	if !ok {
		v, _ = elt.Attribute("xlink:href")
	}
	return strings.TrimSpace(v)
}

// newSVGLeaf builds the layout object of a leaf element whose geometry is
// its own: a basic shape or <path> (SVGShape), or an <image> (SVGImage),
// with its computed style. ok is false for any other tag. Shared by both
// tree builders.
func newSVGLeaf(elt ElementAdapter, lengthCtx SVGLengthContext, styleResolver StyleResolver) (node SVGNode, ok bool) {
	tag := elt.TagName()
	switch {
	case svgShapeTags[tag]:
		shape := NewSVGShape(elt, lengthCtx)
		if shape == nil {
			return nil, true
		}
		if styleResolver != nil {
			shape.Style = styleResolver(elt)
		}
		return shape, true
	case tag == "image":
		var style *css.Style
		if styleResolver != nil {
			style = styleResolver(elt)
		}
		return NewSVGImage(elt, style, lengthCtx), true
	}
	return nil, false
}
