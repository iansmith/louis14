package css

// ViewportStyle is the part of the LayoutView's ComputedStyle that
// StyleResolver::PropagateStyleToViewport fills from the document element
// and body: louis14 ports its background block only (writing mode,
// direction and overflow propagate through their own paths). Read by the
// view painter (pkg/render/view_painter.go).
type ViewportStyle struct {
	// BackgroundColor is the propagated used background-color
	// (VisitedDependentColor(background-color)).
	BackgroundColor Color
	// BackgroundLayers is a copy of the propagated fill layers, each with
	// its clip forced to border-box and `scroll` attachment turned into
	// `local`; nil when the propagated style has no image layers.
	BackgroundLayers *FillLayer
	// ImageRendering is the propagated image-rendering value ("" = auto).
	ImageRendering string
}

// HasBackground reports whether the style has a visible background: a
// non-transparent background-color (currentcolor resolved against the
// element's color) or any fill layer with an image, gradients included.
// Mirrors ComputedStyle::HasBackground (core/style/computed_style.cc:
// 2486-2504 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 has no
// compositor background-color animations.
func (s *Style) HasBackground() bool {
	if s == nil {
		return false
	}
	if s.UsedBackgroundColor().A > 0 {
		return true
	}
	return s.GetBackgroundLayers().AnyLayerHasImage()
}

// UsedBackgroundColor returns the used background-color, transparent when
// it is unset or unparseable (its initial value is transparent, not
// currentcolor, so VisitedDependentColor's fallback does not apply).
func (s *Style) UsedBackgroundColor() Color {
	if bg, ok := s.Get("background-color"); ok && bg != "" && bg != "transparent" {
		if c, ok := s.ResolvedColor(bg); ok {
			return c
		}
	}
	return Color{}
}

// PropagateStyleToViewport computes the viewport's background from the
// document element's style and the first body's style. bodyStyle is nil
// when body propagation is stopped (StyleResolver::
// ShouldStopBodyPropagation, ported as layout.ShouldStopBodyPropagation
// because it reads the layout objects). Mirrors the background block of
// StyleResolver::PropagateStyleToViewport (core/css/resolver/
// style_resolver.cc:3416-3499 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the root's background, or the body's when the root has none (:3447-3454);
// every propagated layer clips to the border box (:3474-3477) and a
// `scroll` attachment becomes `local` (:3479-3485).
func PropagateStyleToViewport(documentElementStyle, bodyStyle *Style) ViewportStyle {
	backgroundStyle := documentElementStyle
	if bodyStyle != nil && !backgroundStyle.HasBackground() {
		backgroundStyle = bodyStyle
	}
	var vs ViewportStyle
	if backgroundStyle == nil {
		return vs
	}
	vs.BackgroundColor = backgroundStyle.UsedBackgroundColor()
	var prev *FillLayer
	for cur := backgroundStyle.GetBackgroundLayers(); cur != nil; cur = cur.Next {
		layer := *cur
		layer.Next = nil
		layer.Clip = BackgroundClipBorderBox
		if layer.Attachment == BackgroundAttachmentScroll {
			layer.Attachment = BackgroundAttachmentLocal
		}
		if prev == nil {
			vs.BackgroundLayers = &layer
		} else {
			prev.Next = &layer
		}
		prev = &layer
	}
	vs.ImageRendering, _ = backgroundStyle.Get("image-rendering")
	return vs
}
