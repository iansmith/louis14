package render

import (
	"testing"

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
