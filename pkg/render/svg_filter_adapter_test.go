package render

import (
	"image"
	"testing"

	"louis14/pkg/graphics/filters"
	"louis14/pkg/layout/svg"
)

// fakeFilterPrimAdapter is a minimal svg.ElementAdapter for testing
// svgFilterPrimitiveAdapter without pulling in the full html.Node
// machinery.
type fakeFilterPrimAdapter struct {
	tag   string
	attrs map[string]string
}

func (f *fakeFilterPrimAdapter) TagName() string { return f.tag }
func (f *fakeFilterPrimAdapter) Attribute(name string) (string, bool) {
	if f.attrs == nil {
		return "", false
	}
	v, ok := f.attrs[name]
	return v, ok
}
func (f *fakeFilterPrimAdapter) SVGChildren() []svg.ElementAdapter { return nil }
func (f *fakeFilterPrimAdapter) TextContent() string               { return "" }

// TestSvgFilterPrimitiveAdapter_FloodCurrentColorTaints asserts the
// origin-tainting source recognition for the bucket-J -002 cluster:
// `<feFlood flood-color="currentcolor">` marks the primitive tainted
// because `currentcolor` resolves to the element's CSS `color`
// property, which can be set by :visited link styling — a known
// browser-history fingerprinting side channel.
//
// The tainted bit propagates through SVGFilterBuilder.BuildGraph
// (svg_filter_builder.go:240-243) so a downstream feDisplacementMap
// observes it as InputEffect(1)->OriginTainted() and short-circuits
// to a pass-through (see FEDisplacementMap.ApplyEffect mirror of
// Blink fe_displacement_map.cc::CreateImageFilter).
//
// Pre-fix: TaintsOrigin() is hardcoded `return false` for all
// primitives. Test fails because the currentcolor case returns false.
// Post-fix: feFlood with flood-color matching currentcolor (case-
// insensitive) returns true; everything else returns false.
//
// Per the Phase 1.4 spec, this test does NOT cover feImage's tainting
// (Phase 4.2) nor the CSS ParseColor currentcolor resolution itself
// (Phase 6 cleanup).
func TestSvgFilterPrimitiveAdapter_FloodCurrentColorTaints(t *testing.T) {
	cases := []struct {
		name       string
		tag        string
		attrs      map[string]string
		wantTaints bool
	}{
		{
			name:       "feFlood currentcolor lowercase",
			tag:        "feflood",
			attrs:      map[string]string{"flood-color": "currentcolor"},
			wantTaints: true,
		},
		{
			name:       "feFlood CurrentColor mixed case",
			tag:        "feflood",
			attrs:      map[string]string{"flood-color": "CurrentColor"},
			wantTaints: true,
		},
		{
			name:       "feFlood currentColor camel",
			tag:        "feflood",
			attrs:      map[string]string{"flood-color": "currentColor"},
			wantTaints: true,
		},
		{
			name:       "feFlood with surrounding whitespace",
			tag:        "feflood",
			attrs:      map[string]string{"flood-color": "  currentcolor  "},
			wantTaints: true,
		},
		{
			name:       "feFlood with named color (red)",
			tag:        "feflood",
			attrs:      map[string]string{"flood-color": "red"},
			wantTaints: false,
		},
		{
			name:       "feFlood with no flood-color attribute (SVG default = black)",
			tag:        "feflood",
			attrs:      nil,
			wantTaints: false,
		},
		{
			name:       "feFlood with rgb() color",
			tag:        "feflood",
			attrs:      map[string]string{"flood-color": "rgb(0, 0, 0)"},
			wantTaints: false,
		},
		{
			name:       "non-feFlood (feBlend) with currentcolor attr is irrelevant",
			tag:        "feblend",
			attrs:      map[string]string{"flood-color": "currentcolor"},
			wantTaints: false,
		},
		{
			name:       "non-feFlood (feGaussianBlur)",
			tag:        "fegaussianblur",
			attrs:      nil,
			wantTaints: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			elt := &fakeFilterPrimAdapter{tag: tc.tag, attrs: tc.attrs}
			adapter := &svgFilterPrimitiveAdapter{elt: elt}
			got := adapter.TaintsOrigin()
			if got != tc.wantTaints {
				t.Errorf("TaintsOrigin() = %v; want %v", got, tc.wantTaints)
			}
		})
	}
}

// TestSvgFilterPrimitiveAdapter_TaintsOriginNilElement guards against
// crashes when the adapter wraps a nil element (defensive — should not
// happen in production).
func TestSvgFilterPrimitiveAdapter_TaintsOriginNilElement(t *testing.T) {
	adapter := &svgFilterPrimitiveAdapter{elt: nil}
	if adapter.TaintsOrigin() {
		t.Error("TaintsOrigin() on nil element should be false")
	}
}

// TestSvgFilterPrimitiveAdapter_LightingCurrentColorTaints asserts
// that `<feDiffuseLighting>` / `<feSpecularLighting>` with
// `lighting-color="currentcolor"` mark the primitive tainted. Mirrors
// the same `:visited`-fingerprinting concern that drove the feFlood
// currentcolor taint (since lighting-color resolves through the CSS
// cascade the same way flood-color does). Spec rule per Filter
// Effects 1 §"tainted filter primitives" + Blink
// SVGFEDiffuseLightingElement::TaintsOrigin /
// SVGFESpecularLightingElement::TaintsOrigin.
//
// Pre-implementation: TaintsOrigin() only checks feFlood — both
// lighting-color cases return false → test fails. Post-implementation
// the switch handles the lighting tags as well.
func TestSvgFilterPrimitiveAdapter_LightingCurrentColorTaints(t *testing.T) {
	cases := []struct {
		name       string
		tag        string
		attrs      map[string]string
		wantTaints bool
	}{
		{
			name:       "fediffuselighting currentcolor",
			tag:        "fediffuselighting",
			attrs:      map[string]string{"lighting-color": "currentcolor"},
			wantTaints: true,
		},
		{
			name:       "fediffuselighting CurrentColor mixed case",
			tag:        "fediffuselighting",
			attrs:      map[string]string{"lighting-color": "CurrentColor"},
			wantTaints: true,
		},
		{
			name:       "fediffuselighting with red lighting-color",
			tag:        "fediffuselighting",
			attrs:      map[string]string{"lighting-color": "red"},
			wantTaints: false,
		},
		{
			name:       "fediffuselighting with no lighting-color (default white)",
			tag:        "fediffuselighting",
			attrs:      nil,
			wantTaints: false,
		},
		{
			name:       "fespecularlighting currentcolor",
			tag:        "fespecularlighting",
			attrs:      map[string]string{"lighting-color": "currentcolor"},
			wantTaints: true,
		},
		{
			name:       "fespecularlighting with rgb",
			tag:        "fespecularlighting",
			attrs:      map[string]string{"lighting-color": "rgb(0, 100, 50)"},
			wantTaints: false,
		},
		{
			name:       "feflood with lighting-color attr is irrelevant",
			tag:        "feflood",
			attrs:      map[string]string{"lighting-color": "currentcolor"},
			wantTaints: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			elt := &fakeFilterPrimAdapter{tag: tc.tag, attrs: tc.attrs}
			adapter := &svgFilterPrimitiveAdapter{elt: elt}
			got := adapter.TaintsOrigin()
			if got != tc.wantTaints {
				t.Errorf("TaintsOrigin() = %v; want %v", got, tc.wantTaints)
			}
		})
	}
}

// TestSvgFilterPrimitiveAdapter_InlineStyleTaints asserts that the
// origin-tainting detection also honors the `style` HTML attribute on
// filter primitives, not just the presentation attribute.
//
// Why this exists: WPT's `tainting-fe{flood,diffuselighting}-dynamic`
// tests mutate the color via JS (`element.style.floodColor =
// 'currentcolor'`). Our JS engine writes that into the element's
// `style` HTML attribute. Without parsing inline style here, the
// renderer keeps reading the stale presentation attribute and the
// post-mutation rendering still shows the BEFORE state.
//
// Real browsers resolve this via the CSS cascade (which folds inline
// style into the computed style). We have no end-to-end resolveStyle
// wiring from layout to renderer for filter primitives yet (see
// FilterEffectBuilder.ResolveStyle — declared but never set in
// render.go). Until that lands, parsing the `style` attribute locally
// in readColorAttr is the minimum-viable fix that lets the dynamic
// tests pass and matches what flood-color / lighting-color do when
// authored statically with `style="flood-color: ..."`.
//
// Pre-fix: inline-style `flood-color: currentcolor` is ignored when no
// presentation attribute is set (or when the attribute disagrees with
// the inline style). Post-fix: inline style takes precedence over the
// presentation attribute, matching the CSS cascade rule "user-agent
// presentation attribute < author inline style".
func TestSvgFilterPrimitiveAdapter_InlineStyleTaints(t *testing.T) {
	cases := []struct {
		name       string
		tag        string
		attrs      map[string]string
		wantTaints bool
	}{
		{
			// Direct mirror of the dynamic-test post-mutation state.
			name: "feFlood inline style flood-color currentcolor (no attr)",
			tag:  "feflood",
			attrs: map[string]string{
				"style": "flood-color: currentcolor",
			},
			wantTaints: true,
		},
		{
			// Dynamic test exact shape: pre-existing color: ... plus
			// JS-appended flood-color: currentcolor, original
			// flood-color presentation attribute still present.
			name: "feFlood inline style overrides presentation attribute",
			tag:  "feflood",
			attrs: map[string]string{
				"flood-color": "rgb(0%, 100%, 50%)",
				"style":       "color: rgb(0%, 100%, 50%); flood-color: currentcolor",
			},
			wantTaints: true,
		},
		{
			// Inline style with concrete color overrides currentcolor
			// from attribute (the cascade rule cuts both ways).
			name: "feFlood inline style concrete color overrides currentcolor attr",
			tag:  "feflood",
			attrs: map[string]string{
				"flood-color": "currentcolor",
				"style":       "flood-color: red",
			},
			wantTaints: false,
		},
		{
			// Lighting variant — same rule applies for lighting-color.
			name: "feDiffuseLighting inline style lighting-color currentcolor",
			tag:  "fediffuselighting",
			attrs: map[string]string{
				"style": "lighting-color: currentcolor",
			},
			wantTaints: true,
		},
		{
			name: "feSpecularLighting inline style lighting-color currentcolor",
			tag:  "fespecularlighting",
			attrs: map[string]string{
				"style": "lighting-color: currentcolor",
			},
			wantTaints: true,
		},
		{
			// Non-color property in inline style must not be confused
			// with the relevant color property.
			name: "feFlood inline style with unrelated property (color only)",
			tag:  "feflood",
			attrs: map[string]string{
				"style": "color: red",
			},
			wantTaints: false,
		},
		{
			// Whitespace + case variations in the inline style value.
			name: "feFlood inline style with whitespace and mixed case",
			tag:  "feflood",
			attrs: map[string]string{
				"style": "  flood-color  :   CurrentColor  ",
			},
			wantTaints: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			elt := &fakeFilterPrimAdapter{tag: tc.tag, attrs: tc.attrs}
			adapter := &svgFilterPrimitiveAdapter{elt: elt}
			got := adapter.TaintsOrigin()
			if got != tc.wantTaints {
				t.Errorf("TaintsOrigin() = %v; want %v", got, tc.wantTaints)
			}
		})
	}
}

// TestSvgFilterPrimitiveAdapter_DropShadowCurrentColorTaints captures
// the LOU-129 Bug 1 second root cause: `<feDropShadow>` with
// `flood-color="currentcolor"` must mark the primitive tainted just
// like `<feFlood>` does. Per Blink
// SVGFEDropShadowElement::TaintsOrigin
// (third_party/blink/renderer/core/svg/svg_fe_drop_shadow_element.cc)
// which checks style->FloodColor().DependsOnCurrentColor().
//
// Without this, tainting-fedropshadow-002.html (currentcolor
// flood-color) leaks the colour through the downstream
// feDisplacementMap — its in2 is the offset-shifted shadow buffer,
// which would have been pass-through suppressed if the tainted bit
// had propagated.
//
// Pre-fix: TaintsOrigin() only switches on feflood — feDropShadow
// case returns false → reftest fails 7500 px.
// Post-fix: feDropShadow case mirrors feFlood; reftest at 0 px.
func TestSvgFilterPrimitiveAdapter_DropShadowCurrentColorTaints(t *testing.T) {
	cases := []struct {
		name       string
		tag        string
		attrs      map[string]string
		wantTaints bool
	}{
		{
			name:       "feDropShadow flood-color=currentcolor lowercase",
			tag:        "fedropshadow",
			attrs:      map[string]string{"flood-color": "currentcolor"},
			wantTaints: true,
		},
		{
			name:       "feDropShadow flood-color=CurrentColor mixed case",
			tag:        "fedropshadow",
			attrs:      map[string]string{"flood-color": "CurrentColor"},
			wantTaints: true,
		},
		{
			// Direct mirror of tainting-fedropshadow-002.html:
			//   <feDropShadow flood-color="currentcolor" style="color: rgb(...)" .../>
			name: "feDropShadow inline style flood-color currentcolor",
			tag:  "fedropshadow",
			attrs: map[string]string{
				"style": "flood-color: currentcolor",
			},
			wantTaints: true,
		},
		{
			name:       "feDropShadow flood-color=rgb does NOT taint",
			tag:        "fedropshadow",
			attrs:      map[string]string{"flood-color": "rgb(0, 100, 50)"},
			wantTaints: false,
		},
		{
			name:       "feDropShadow with no flood-color (default black) does NOT taint",
			tag:        "fedropshadow",
			attrs:      nil,
			wantTaints: false,
		},
		{
			name:       "feDropShadow with named color (red) does NOT taint",
			tag:        "fedropshadow",
			attrs:      map[string]string{"flood-color": "red"},
			wantTaints: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			elt := &fakeFilterPrimAdapter{tag: tc.tag, attrs: tc.attrs}
			adapter := &svgFilterPrimitiveAdapter{elt: elt}
			got := adapter.TaintsOrigin()
			if got != tc.wantTaints {
				t.Errorf("TaintsOrigin() = %v; want %v", got, tc.wantTaints)
			}
		})
	}
}

// TestResolveImageSource_ConvertsToOperatingSpace asserts that a
// `<feImage>` resolves its source image INTO the filter's interpolation
// space. SVG filters default to linearRGB
// (color-interpolation-filters:linearRGB), and a sourced image is sRGB
// device content — Blink's FilterEffect framework converts the FEImage
// result into the operating space (TransformResultIfNeeded on the
// sourced image, svg_fe_image.cc). Without that conversion the raw sRGB
// bytes are mislabelled linearRGB and the final composite double-applies
// a linear→sRGB transfer, darkening every mid-tone (the systematic shift
// seen in effect-reference-feimage-001: a whole-image colour error).
//
// Setup: a data:image/svg+xml source whose only child is a mid-grey rect
// (sRGB rgb(128,128,128)). Resolved into a linearRGB filter, the output
// pixel must be the linear-light encoding of sRGB 128 (≈ 55), NOT 128.
// A pure-channel colour (0 or 255) would be invariant under the transfer
// and could not distinguish "converted" from "not converted", so the
// test deliberately uses a mid-tone.
func TestResolveImageSource_ConvertsToOperatingSpace(t *testing.T) {
	const href = "data:image/svg+xml," +
		"<svg xmlns='http://www.w3.org/2000/svg'>" +
		"<rect width='10' height='10' fill='rgb(128,128,128)'/></svg>"
	elt := &fakeFilterPrimAdapter{
		tag:   "feimage",
		attrs: map[string]string{"href": href},
	}
	target := image.Rect(0, 0, 10, 10)

	cases := []struct {
		name      string
		space     filters.InterpolationSpace
		wantByte  uint8
		tolerance int
	}{
		{
			name:      "linearRGB filter converts sRGB source to linear",
			space:     filters.InterpolationSpaceLinearRGB,
			wantByte:  55, // linearToByte(srgbToLinear(128/255)) ≈ 55
			tolerance: 2,
		},
		{
			name:      "sRGB filter leaves the source untouched",
			space:     filters.InterpolationSpaceSRGB,
			wantByte:  128,
			tolerance: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &svgFilterPrimitiveAdapter{elt: elt, space: tc.space}
			src, _, _ := adapter.ResolveImageSource(target, target)
			if src == nil {
				t.Fatalf("ResolveImageSource returned nil source image")
			}
			got := src.Pix[0] // R channel of pixel (0,0); rect is opaque so premul==straight
			diff := int(got) - int(tc.wantByte)
			if diff < 0 {
				diff = -diff
			}
			if diff > tc.tolerance {
				t.Errorf("source R = %d; want %d (±%d)", got, tc.wantByte, tc.tolerance)
			}
		})
	}
}
