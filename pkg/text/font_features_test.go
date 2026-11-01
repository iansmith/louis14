package text

import (
	"slices"
	"testing"

	"louis14/pkg/css"

	"mazarin/textshape"
)

func styleWith(props ...string) *css.Style {
	s := css.NewStyle()
	for i := 0; i+1 < len(props); i += 2 {
		s.Properties[props[i]] = props[i+1]
	}
	return s
}

// TestGetFontFeatures: the face-independent list, in FromFontDescription's
// order with the caps overlay first and font-feature-settings last.
func TestGetFontFeatures(t *testing.T) {
	if got := GetFontFeatures(styleWith()); got != nil {
		t.Errorf("initial values: got %v, want nil (HarfBuzz defaults)", got)
	}
	if got := GetFontFeatures(nil); got != nil {
		t.Errorf("nil style: got %v, want nil", got)
	}
	got := GetFontFeatures(styleWith(
		"font-variant-caps", "small-caps",
		"font-kerning", "none",
		"font-variant-numeric", "tabular-nums",
		"font-feature-settings", `"kern" 1`,
	))
	want := []textshape.FontFeature{
		ff("smcp", 1), ff("kern", 0), ff("vkrn", 0),
		ff("tnum", 1), ff("kern", 1),
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestMeasureText_FeaturesShapeAndKeyTheCache: features reach the shaper
// and are part of the measure cache key, so a kern-off measurement of a
// kerning pair is not served the cached kerned width (or vice versa).
func TestMeasureText_FeaturesShapeAndKeyTheCache(t *testing.T) {
	path := DefaultFontConfig().Regular
	kernOff := []textshape.FontFeature{ff("kern", 0)}
	a := measureWidth("A", 40, path, nil)
	v := measureWidth("V", 40, path, nil)
	kerned := measureWidth("AV", 40, path, nil)
	if kerned >= a+v {
		t.Fatalf("precondition: AV kerned %v, want < A+V %v", kerned, a+v)
	}
	if got := measureWidth("AV", 40, path, kernOff); got != a+v {
		t.Errorf("AV with kern=0: %v, want A+V %v", got, a+v)
	}
	if got := measureWidth("AV", 40, path, nil); got != kerned {
		t.Errorf("AV with no features after a kern=0 measurement: %v, want kerned %v", got, kerned)
	}
	cum, ok := shapeAdvancesFloat("AV", 40, path, kernOff)
	if !ok || cum[2] != a+v {
		t.Errorf("shapeAdvancesFloat(AV, kern=0) = %v ok=%v, want end %v", cum, ok, a+v)
	}
	cum, ok = shapeAdvancesMixedFloat("AV", []DirectionRun{{Start: 0, End: 2}}, 40, path, kernOff)
	if !ok || cum[2] != a+v {
		t.Errorf("shapeAdvancesMixedFloat(AV, kern=0) = %v ok=%v, want end %v", cum, ok, a+v)
	}
}
