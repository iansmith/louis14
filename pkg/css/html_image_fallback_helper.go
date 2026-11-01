package css

import (
	"strings"

	"louis14/pkg/html"
)

// Port of Blink's HTMLImageFallbackHelper style callbacks
// (third_party/blink/renderer/core/html/html_image_fallback_helper.cc @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). An <img> whose layout
// disposition is fallback content (html.LayoutDispositionFallbackContent)
// shows a UA shadow tree — span#alttext-container > [img#alttext-image,
// span#alttext > alt text] — and these functions compute the styles that
// Blink's AdjustStyle callbacks give the host and the two shadow elements.
//
// The file lives in pkg/css (not pkg/html, where Blink's helper lives)
// because pkg/html cannot import pkg/css.
//
// Standards mode only: louis14 tracks no document quirks mode (the parser
// drops the DOCTYPE), so the InQuirksMode branches — TreatImageAsReplaced's
// quirks term, AdjustHostStyle's symmetric dimensions (:254-264) and the
// container's AlignToBaseline (:80-83) — have no louis14 analog. Neither has
// ShowBrokenImageIcon's IsInInlinifyingDisplay (ruby) branch (:194-198).

// StyleUAShadowHostData carries the fallback host's sizing and alt/src
// values down to its shadow elements, which need them to build styles that
// plain CSS cannot express. Mirrors Blink's StyleUAShadowHostData
// (core/style/style_ua_shadow_host_data.h:20-52 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). Lengths are computed values as
// CSS strings: "auto" (or "") for auto, an absolute length as "<n>px",
// anything else (percentages, calc with %) as specified.
type StyleUAShadowHostData struct {
	Width, Height, MaxWidth, MaxHeight string
	// AspectRatio is the host's computed aspect-ratio ("" or "auto" = auto).
	AspectRatio string
	// AltText is HTMLImageElement::AltText(); AltTextIsSet is false when it
	// is Blink's null String (neither alt nor title present).
	AltText      string
	AltTextIsSet bool
	// AltAttribute and SrcAttribute are the raw attribute values ("" when
	// absent, as Blink's null AtomicString compares empty).
	AltAttribute string
	SrcAttribute string
}

// kPixelsForAltImage: 16px for the broken-image icon plus 2px for its
// top/left border and padding offset (html_image_fallback_helper.cc:36-37).
const kPixelsForAltImage = 18

func isAutoLength(v string) bool { return v == "" || v == "auto" }

// fixedPixels returns the pixel value of a computed absolute length; ok is
// false for auto, percentages and anything else Blink's Length::IsFixed
// rejects.
func fixedPixels(v string) (float64, bool) {
	if isAutoLength(v) || strings.Contains(v, "%") {
		return 0, false
	}
	return ParseLength(v)
}

// imageRepresentsNothing mirrors ImageRepresentsNothing (:21-32).
func imageRepresentsNothing(h *StyleUAShadowHostData) bool {
	altIsEmpty := h.AltTextIsSet && h.AltText == ""
	srcIsSet := h.SrcAttribute != ""
	if srcIsSet && altIsEmpty {
		return true
	}
	return !srcIsSet && (!h.AltTextIsSet || altIsEmpty)
}

// imageSmallerThanAltImage mirrors ImageSmallerThanAltImage (:34-48).
func imageSmallerThanAltImage(width, height string) bool {
	w, wFixed := fixedPixels(width)
	ht, hFixed := fixedPixels(height)
	if !wFixed && !hFixed {
		return false
	}
	if hFixed && ht < kPixelsForAltImage {
		return true
	}
	return wFixed && w < kPixelsForAltImage
}

// treatImageAsReplaced mirrors TreatImageAsReplaced (:50-62) in standards
// mode: the image keeps a replaced-like box when it has dimensions and no
// (or an empty) alt attribute.
func treatImageAsReplaced(h *StyleUAShadowHostData) bool {
	hasIntrinsicDimensions := !isAutoLength(h.Width) && !isAutoLength(h.Height)
	hasDimensionsFromAR := !isAutoLength(h.AspectRatio) &&
		(!isAutoLength(h.Width) || !isAutoLength(h.Height))
	hasNoAltAttribute := h.AltAttribute == ""
	return (hasIntrinsicDimensions || hasDimensionsFromAR) && hasNoAltAttribute
}

// computedLength absolutizes a length against s (Blink stores computed
// lengths in StyleUAShadowHostData), leaving auto and percentage-bearing
// values as specified.
func computedLength(s *Style, v string) string {
	if isAutoLength(v) || strings.Contains(v, "%") {
		return v
	}
	if px, ok := s.GetLengthForVal(v); ok {
		return formatPx(px)
	}
	return v
}

// blockFlowDisplay maps a fallback host's display to one louis14 lays out
// with the block-flow algorithm. No Blink analog: Blink keeps the computed
// display and instead picks the layout object class — a fallback <img> is
// always a LayoutBlockFlow (HTMLImageElement::CreateLayoutObject →
// LayoutObject::CreateBlockFlowOrListItem, html_image_element.cc:525-526,
// layout_object.cc:445-458 @ d4ecdfed88f962439247c2ad36b8fe47805b1520), which
// is an atomic inline when inline-level. louis14 selects the layout
// algorithm from display, so inline-level displays become inline-block and
// block-level non-flow displays become block — the same move as the
// orthogonal-flow inline → inline-block remap (AdjustStyleForDisplay,
// mirroring style_adjuster.cc:819-824).
func blockFlowDisplay(d DisplayType) DisplayType {
	switch d {
	case DisplayInline, DisplayInlineFlex, DisplayInlineGrid, DisplayInlineTable, DisplayRuby:
		return DisplayInlineBlock
	case DisplayFlex, DisplayGrid, DisplayTable, DisplayBlockRuby, DisplayRubyText,
		DisplayTableRow, DisplayTableCell, DisplayTableHeaderGroup,
		DisplayTableRowGroup, DisplayTableFooterGroup, DisplayTableCaption:
		return DisplayBlock
	}
	return d
}

// AdjustHostStyle is the style callback of an <img> showing fallback content
// (HTMLImageElement::AdjustStyle → HTMLImageFallbackHelper::AdjustHostStyle,
// html_image_element.cc:1173-1176 and html_image_fallback_helper.cc:244-282
// @ d4ecdfed88f962439247c2ad36b8fe47805b1520). It records the host's final
// sizing and alt/src values in s.UAShadowHostData, then, unless the image is
// treated as replaced, resets width/height/aspect-ratio of a display:inline
// host to auto so it shrinks to fit its alt text (:274-281). Finally it maps
// display onto a block-flow display (blockFlowDisplay).
//
// Must run after ResolveLogicalPropertiesInTree, which can still write
// width/height from logical properties, and after
// ResolveAppliedTextDecorations: a display:inline host is not a
// text-decoration propagation boundary in Blink, so its decorations are
// resolved from its pre-remap display.
func AdjustHostStyle(node *html.Node, s *Style) {
	altText, altSet := node.AltText()
	altAttr, _ := node.GetAttribute("alt")
	srcAttr, _ := node.GetAttribute("src")
	width, _ := s.Get("width")
	height, _ := s.Get("height")
	maxWidth, _ := s.Get("max-width")
	maxHeight, _ := s.Get("max-height")
	aspectRatio, _ := s.Get("aspect-ratio")
	s.UAShadowHostData = &StyleUAShadowHostData{
		Width:        computedLength(s, width),
		Height:       computedLength(s, height),
		MaxWidth:     computedLength(s, maxWidth),
		MaxHeight:    computedLength(s, maxHeight),
		AspectRatio:  aspectRatio,
		AltText:      altText,
		AltTextIsSet: altSet,
		AltAttribute: altAttr,
		SrcAttribute: srcAttr,
	}

	display := s.GetDisplay()
	if display == DisplayNone || display == DisplayContents {
		return
	}
	if !treatImageAsReplaced(s.UAShadowHostData) && display == DisplayInline {
		s.Set("width", "auto")
		s.Set("height", "auto")
		delete(s.Properties, "aspect-ratio")
	}
	if bd := blockFlowDisplay(display); bd != s.GetDisplay() {
		s.Set("display", string(bd))
	}
}

// AdjustImageFallbackHostStylesInTree runs AdjustHostStyle for every <img>
// under root whose layout disposition is fallback content.
func AdjustImageFallbackHostStylesInTree(root *html.Node, styles map[*html.Node]*Style) {
	if root.Type == html.ElementNode && root.TagName == "img" &&
		root.LayoutDisposition == html.LayoutDispositionFallbackContent {
		if s := styles[root]; s != nil {
			AdjustHostStyle(root, s)
		}
	}
	for _, c := range root.Children {
		AdjustImageFallbackHostStylesInTree(c, styles)
	}
}

// AdjustAltTextContainerStyle is HTMLAltTextContainerElement::AdjustStyle
// (html_image_fallback_helper.cc:73-100): when the image is treated as
// replaced, the container takes the host's size as a clipped flow-root
// (ShowAsReplaced, :103-116) and, unless smaller than the alt image, a 1px
// silver border with 1px padding (ShowBorder, :118-144).
func AdjustAltTextContainerStyle(s *Style, h *StyleUAShadowHostData) {
	if h == nil || !treatImageAsReplaced(h) {
		return
	}
	s.Set("overflow-x", "hidden")
	s.Set("overflow-y", "hidden")
	s.Set("display", string(DisplayFlowRoot))
	s.Set("pointer-events", "none")
	// An unset host value is the property's initial value.
	for _, p := range [...]struct{ prop, value, initial string }{
		{"max-height", h.MaxHeight, "none"}, {"height", h.Height, "auto"},
		{"width", h.Width, "auto"}, {"max-width", h.MaxWidth, "none"},
	} {
		if p.value == "" {
			p.value = p.initial
		}
		s.Set(p.prop, p.value)
	}
	// Text decorations must be reset for an inline-block (Blink:
	// SetBaseTextDecorationData(nullptr), see StopPropagateTextDecorations).
	s.AppliedTextDecorations = nil

	if imageSmallerThanAltImage(h.Width, h.Height) {
		return
	}
	// ShowBorder. Blink scales the 1px by EffectiveZoom; louis14 has no zoom.
	for _, side := range []string{"top", "right", "bottom", "left"} {
		s.Set("border-"+side+"-width", "1px")
		s.Set("border-"+side+"-style", "solid")
		s.Set("border-"+side+"-color", "silver")
		s.Set("padding-"+side, "1px")
	}
	s.Set("box-sizing", "border-box")
}

// AdjustAltTextImageStyle is HTMLAltTextImageElement::AdjustStyle
// (html_image_fallback_helper.cc:158-190): the broken-image icon is shown
// as a start-side float (ShowBrokenImageIcon, :192-207) unless the replaced
// box is too small for it or the image represents nothing, in which case it
// is hidden (HideBrokenImageIcon, :209-211).
func AdjustAltTextImageStyle(s *Style, h *StyleUAShadowHostData) {
	if h == nil {
		return
	}
	var show bool
	if treatImageAsReplaced(h) {
		show = !imageSmallerThanAltImage(h.Width, h.Height)
	} else {
		show = !imageRepresentsNothing(h)
	}
	if !show {
		s.Set("display", string(DisplayNone))
		return
	}
	s.Set("display", string(DisplayFlowRoot))
	if s.GetDirection() == DirectionLTR {
		s.Set("float", string(FloatLeft))
	} else {
		s.Set("float", string(FloatRight))
	}
}
