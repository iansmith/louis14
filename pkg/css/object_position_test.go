package css

import (
	"testing"

	"louis14/pkg/geometry/layoutunit"
)

// LOU-590: object-position computes to a LengthPoint through ConsumePosition
// (no three-value form) and StyleBuilderConverter::ConvertPosition
// (style_builder_converter.cc:2471; ConvertPositionLength,
// style_builder_converter.h:542 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestObjectPosition_ComputedLengthPoint(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  LengthPoint
	}{
		{"", InitialObjectPosition},
		{"top right", LengthPoint{X: Length{Percent: 100}, Y: Length{Percent: 0}}},
		{"bottom left", LengthPoint{X: Length{Percent: 0}, Y: Length{Percent: 100}}},
		{"center", LengthPoint{X: Length{Percent: 50}, Y: Length{Percent: 50}}},
		{"top", LengthPoint{X: Length{Percent: 50}, Y: Length{Percent: 0}}},
		{"bottom 1px right 2px", LengthPoint{X: Length{Fixed: -2, Percent: 100}, Y: Length{Fixed: -1, Percent: 100}}},
		{"top 3px left 50%", LengthPoint{X: Length{Percent: 50}, Y: Length{Fixed: 3}}},
		{"top 50% right 25%", LengthPoint{X: Length{Percent: 75}, Y: Length{Percent: 50}}},
		{"50% -7%", LengthPoint{X: Length{Percent: 50}, Y: Length{Percent: -7}}},
		{"10px calc(100% - 4px)", LengthPoint{X: Length{Fixed: 10}, Y: Length{Fixed: -4, Percent: 100}}},
	} {
		s := NewStyle()
		if tc.value != "" {
			s.Set("object-position", tc.value)
		}
		if got := s.ObjectPosition(); got != tc.want {
			t.Errorf("object-position: %q = %+v, want %+v", tc.value, got, tc.want)
		}
	}
}

// ObjectPosition::ParseSingleValue rejects three-value positions, unitless
// numbers and trailing junk; the earlier valid declaration survives.
func TestObjectPosition_InvalidDeclarationKeepsEarlier(t *testing.T) {
	for _, bad := range []string{"left 10px top", "right 5% bottom", "10 20", "left right", "top 1px left", "center center center", "right 25% bottom"} {
		ss, err := ParseStylesheet("img { object-position: right bottom; object-position: "+bad+"; }", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := ss.Rules[0].Declarations["object-position"]; got != "right bottom" {
			t.Errorf("object-position: %s should be invalid; got %q", bad, got)
		}
	}
	for _, good := range []string{"left 10px top 5px", "right 25% bottom 0", "inherit", "var(--p)"} {
		ss, err := ParseStylesheet("img { object-position: right bottom; object-position: "+good+"; }", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := ss.Rules[0].Declarations["object-position"]; got != good {
			t.Errorf("object-position: %s should be valid; got %q", good, got)
		}
	}
}

// background-position keeps the three-value form
// (WebFeature::kThreeValuedPositionBackground).
func TestConsumePosition_ThreeValueOnlyForBackground(t *testing.T) {
	if _, _, ok := consumePosition(newComponentStream("left 10px top"), false, true); !ok {
		t.Fatal("background three-value position rejected")
	}
	s := newComponentStream("left 10px top")
	x, y, ok := consumePosition(s, false, false)
	if !ok || x.edge != "left" || x.offset != "" || y.edge != "center" || s.atEnd() {
		t.Errorf("object-position three-value: x=%+v y=%+v atEnd=%v, want one-value left with the rest unconsumed", x, y, s.atEnd())
	}
}

func TestLength_MinimumValueForLength(t *testing.T) {
	max := layoutunit.New(10)
	for _, tc := range []struct {
		l    Length
		want float64
	}{
		{Length{Fixed: 3}, 3},
		{Length{Percent: 50}, 5},
		{Length{Percent: -7}, -0.6875}, // -0.7 truncated toward zero to 1/64
		{Length{Fixed: -2, Percent: 100}, 8},
	} {
		if got := tc.l.MinimumValueForLength(max).Float64(); got != tc.want {
			t.Errorf("%+v against 10px = %v, want %v", tc.l, got, tc.want)
		}
	}
	if got := (Length{Fixed: 2, Percent: 30}).SubtractFromOneHundredPercent(); got != (Length{Fixed: -2, Percent: 70}) {
		t.Errorf("SubtractFromOneHundredPercent = %+v", got)
	}
}
