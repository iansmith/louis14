package css

import (
	"reflect"
	"testing"
)

// Phase 0 red tests for LOU-241 (transform interpolation) and LOU-242
// (viewport-unit / percent length interpolation) in the animation keyframe
// sampling path. The contract under test is ResolveKeyframeStyle: a
// two-keyframe effect sampled mid-segment must return the interpolated value,
// not a discrete endpoint.
//
// Transform semantics mirror Blink TransformOperations::Blend
// (platform/transforms/transform_operations.cc:179) and MatchingPrefixLength
// identity padding (:124) @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
// Length semantics mirror Blink InterpolableLength::MaybeConvertCSSValue /
// Interpolate (core/animation/interpolable_length.cc:58, :651) @ same SHA:
// a (pixels, percent) component pair blended componentwise, with viewport
// units resolved to pixels at conversion time.

// sampleKeyframes builds a from/to effect for one property and samples it at
// the given progress with a linear timing function.
func sampleKeyframes(t *testing.T, prop, from, to string, progress float64, base *Style) string {
	t.Helper()
	eff := BuildKeyframeEffect([]KeyframeRule{
		{Stop: "from", Declarations: map[string]string{prop: from}},
		{Stop: "to", Declarations: map[string]string{prop: to}},
	}, nil)
	out := ResolveKeyframeStyle(eff, progress, base)
	v, ok := out[prop]
	if !ok {
		t.Fatalf("ResolveKeyframeStyle returned no value for %q (from=%q to=%q progress=%v)", prop, from, to, progress)
	}
	return v
}

// assertTransformsEqual compares two transform value strings by their parsed
// operation lists so tests are robust to serialization choices.
func assertTransformsEqual(t *testing.T, got, want string) {
	t.Helper()
	gotOps := parseTransforms(got)
	wantOps := parseTransforms(want)
	if !reflect.DeepEqual(gotOps, wantOps) {
		t.Errorf("transform mismatch:\n  got  %q -> %#v\n  want %q -> %#v", got, gotOps, want, wantOps)
	}
}

// --- LOU-241: transform interpolation ---------------------------------------

func TestKeyframeTransform_TranslateXPxLerp(t *testing.T) {
	got := sampleKeyframes(t, "transform", "translateX(0px)", "translateX(1000px)", 0.5, nil)
	assertTransformsEqual(t, got, "translateX(500px)")
}

func TestKeyframeTransform_MatchingMultiOpList(t *testing.T) {
	got := sampleKeyframes(t, "transform", "translateX(0px) scale(1)", "translateX(100px) scale(3)", 0.5, nil)
	assertTransformsEqual(t, got, "translateX(50px) scale(2)")
}

func TestKeyframeTransform_IdentityPaddingForShorterList(t *testing.T) {
	// css-transforms §interpolation-of-transforms: when the shorter list is a
	// matching prefix of the longer, it is padded with identity operations.
	got := sampleKeyframes(t, "transform", "translateX(0px)", "translateX(100px) scale(3)", 0.5, nil)
	assertTransformsEqual(t, got, "translateX(50px) scale(2)")
}

func TestKeyframeTransform_NoneEndpointIsIdentityList(t *testing.T) {
	got := sampleKeyframes(t, "transform", "none", "translateX(100px)", 0.5, nil)
	assertTransformsEqual(t, got, "translateX(50px)")
}

func TestKeyframeTransform_PercentTranslateLerp(t *testing.T) {
	got := sampleKeyframes(t, "transform", "translateX(0%)", "translateX(50%)", 0.5, nil)
	assertTransformsEqual(t, got, "translateX(25%)")
}

func TestKeyframeTransform_MismatchedListsFallDiscrete(t *testing.T) {
	// Mismatched function lists without matrix decomposition fall back to the
	// discrete endpoints, mirroring Blink's matrix-interpolation failure path
	// (transform_operations.cc:210-212: progress < 0.5 ? from : to).
	gotLow := sampleKeyframes(t, "transform", "rotate(0deg)", "translateX(100px)", 0.25, nil)
	assertTransformsEqual(t, gotLow, "rotate(0deg)")
	gotHigh := sampleKeyframes(t, "transform", "rotate(0deg)", "translateX(100px)", 0.75, nil)
	assertTransformsEqual(t, gotHigh, "translateX(100px)")
}

// --- LOU-242: length interpolation with viewport units and percentages ------

func viewportBase() *Style {
	s := NewStyle()
	s.ViewportWidth = 800
	s.ViewportHeight = 600
	return s
}

func TestKeyframeLength_VhResolvesAgainstBaseViewport(t *testing.T) {
	got := sampleKeyframes(t, "height", "0px", "200vh", 0.5, viewportBase())
	if got != "600px" { // 200vh = 1200px at 600px viewport height; midpoint 600px
		t.Errorf("height 0px->200vh @0.5 = %q, want \"600px\"", got)
	}
}

func TestKeyframeLength_VwResolvesAgainstBaseViewport(t *testing.T) {
	got := sampleKeyframes(t, "width", "0px", "200vw", 0.5, viewportBase())
	if got != "800px" { // 200vw = 1600px at 800px viewport width; midpoint 800px
		t.Errorf("width 0px->200vw @0.5 = %q, want \"800px\"", got)
	}
}

func TestKeyframeLength_PurePercentLerp(t *testing.T) {
	got := sampleKeyframes(t, "width", "0%", "50%", 0.5, viewportBase())
	if got != "25%" {
		t.Errorf("width 0%%->50%% @0.5 = %q, want \"25%%\"", got)
	}
}

func TestKeyframeLength_ZeroPercentToVwIsPurePixels(t *testing.T) {
	// (0px, 0%) -> (1600px, 0%): percent components are both zero, so the
	// result collapses to a pure pixel length.
	got := sampleKeyframes(t, "width", "0%", "200vw", 0.5, viewportBase())
	if got != "800px" {
		t.Errorf("width 0%%->200vw @0.5 = %q, want \"800px\"", got)
	}
}

func TestKeyframeLength_MixedPxPercentEmitsCalc(t *testing.T) {
	// (100px, 0%) -> (0px, 50%) at 0.5 -> (50px, 25%), emitted as calc().
	// Mirrors InterpolableLength's pixels+percent component blend.
	got := sampleKeyframes(t, "width", "100px", "50%", 0.5, viewportBase())
	if got != "calc(50px + 25%)" {
		t.Errorf("width 100px->50%% @0.5 = %q, want \"calc(50px + 25%%)\"", got)
	}
}

func TestKeyframeLength_NilBaseViewportUnitStaysDiscrete(t *testing.T) {
	// Without a base style there is no viewport to resolve vh against; the
	// value falls back to the discrete from-endpoint (existing behavior).
	got := sampleKeyframes(t, "height", "0px", "200vh", 0.5, nil)
	if got != "0px" {
		t.Errorf("height 0px->200vh @0.5 with nil base = %q, want \"0px\"", got)
	}
}

func TestKeyframeBareNumber_OpacityStillLerps(t *testing.T) {
	// Regression guard: widening the length branch must not capture bare
	// numbers (parseLengthFullWithCh rejects unitless non-zero values).
	got := sampleKeyframes(t, "opacity", "0", "1", 0.5, nil)
	if got != "0.5" {
		t.Errorf("opacity 0->1 @0.5 = %q, want \"0.5\"", got)
	}
}
