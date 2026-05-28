package css

import (
	"reflect"
	"testing"
)

func TestParseFontFeatureValuesRule_Simple(t *testing.T) {
	rule := parseFontFeatureValuesRule(`@font-feature-values fwf {
		@stylistic { foo: 1; bar: 2; baz: 3; }
		@styleset { a: 1; b: 2 4; }
		@character-variant { foo: 1; bar: 1 3; }
		@swash { sw: 7; }
		@ornaments { o: 4; }
		@annotation { a: 9; }
	}`)
	if rule == nil {
		t.Fatal("parseFontFeatureValuesRule returned nil")
	}
	if !reflect.DeepEqual(rule.Families, []string{"fwf"}) {
		t.Errorf("families = %v, want [fwf]", rule.Families)
	}
	if !reflect.DeepEqual(rule.Stylistic["foo"], []int{1}) {
		t.Errorf("stylistic[foo] = %v, want [1]", rule.Stylistic["foo"])
	}
	if !reflect.DeepEqual(rule.Stylistic["bar"], []int{2}) {
		t.Errorf("stylistic[bar] = %v, want [2]", rule.Stylistic["bar"])
	}
	if !reflect.DeepEqual(rule.Styleset["b"], []int{2, 4}) {
		t.Errorf("styleset[b] = %v, want [2 4]", rule.Styleset["b"])
	}
	if !reflect.DeepEqual(rule.CharacterVariant["bar"], []int{1, 3}) {
		t.Errorf("character-variant[bar] = %v, want [1 3]", rule.CharacterVariant["bar"])
	}
	if !reflect.DeepEqual(rule.Swash["sw"], []int{7}) {
		t.Errorf("swash[sw] = %v, want [7]", rule.Swash["sw"])
	}
	if !reflect.DeepEqual(rule.Ornaments["o"], []int{4}) {
		t.Errorf("ornaments[o] = %v, want [4]", rule.Ornaments["o"])
	}
	if !reflect.DeepEqual(rule.Annotation["a"], []int{9}) {
		t.Errorf("annotation[a] = %v, want [9]", rule.Annotation["a"])
	}
}

func TestParseFontFeatureValuesRule_FamilyList(t *testing.T) {
	rule := parseFontFeatureValuesRule(`@font-feature-values "A B", fontC {
		@stylistic { x: 1; }
	}`)
	if rule == nil {
		t.Fatal("nil rule")
	}
	if !reflect.DeepEqual(rule.Families, []string{"A B", "fontC"}) {
		t.Errorf("families = %v, want [A B fontC]", rule.Families)
	}
}

func TestParseFontFeatureValuesRule_RejectsTooManyIndexes(t *testing.T) {
	// @stylistic / @swash / @ornaments / @annotation require exactly one index;
	// @styleset accepts unlimited; @character-variant accepts 1 or 2.
	rule := parseFontFeatureValuesRule(`@font-feature-values fwf {
		@stylistic { bad: 1 2; ok: 3; }
		@swash { bad: 1 2; ok: 5; }
		@character-variant { bad: 1 2 3; ok: 1 2; }
	}`)
	if rule == nil {
		t.Fatal("nil rule")
	}
	if _, ok := rule.Stylistic["bad"]; ok {
		t.Error("stylistic[bad] should be rejected (2 indexes)")
	}
	if !reflect.DeepEqual(rule.Stylistic["ok"], []int{3}) {
		t.Errorf("stylistic[ok] = %v, want [3]", rule.Stylistic["ok"])
	}
	if _, ok := rule.Swash["bad"]; ok {
		t.Error("swash[bad] should be rejected (2 indexes)")
	}
	if _, ok := rule.CharacterVariant["bad"]; ok {
		t.Error("character-variant[bad] should be rejected (3 indexes)")
	}
}

func TestLookupFontFeatureValues_LayerMerge(t *testing.T) {
	// Mirrors the alternates-layers WPT test: same family, two rules,
	// later wins on per-name collisions but preserves non-conflicting names.
	rules := []FontFeatureValuesRule{
		{
			Families: []string{"fwf"},
			Styleset: map[string][]int{"foo": {1}, "bar": {1}},
		},
		{
			Families: []string{"fwf"},
			Styleset: map[string][]int{"foo": {2}, "bar": {2}, "baz": {2}},
		},
		{
			Families: []string{"fwf"},
			Styleset: map[string][]int{"baz": {3}},
		},
	}
	merged := LookupFontFeatureValues(rules, "fwf")
	if !reflect.DeepEqual(merged.Styleset["foo"], []int{2}) {
		t.Errorf("styleset[foo] = %v, want [2]", merged.Styleset["foo"])
	}
	if !reflect.DeepEqual(merged.Styleset["bar"], []int{2}) {
		t.Errorf("styleset[bar] = %v, want [2]", merged.Styleset["bar"])
	}
	if !reflect.DeepEqual(merged.Styleset["baz"], []int{3}) {
		t.Errorf("styleset[baz] = %v, want [3]", merged.Styleset["baz"])
	}
}

func TestLookupFontFeatureValues_FamilyMismatch(t *testing.T) {
	rules := []FontFeatureValuesRule{
		{
			Families: []string{"other"},
			Styleset: map[string][]int{"foo": {1}},
		},
	}
	merged := LookupFontFeatureValues(rules, "fwf")
	if len(merged.Styleset) != 0 {
		t.Errorf("styleset = %v, want empty", merged.Styleset)
	}
}
