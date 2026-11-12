package css

import (
	"math"
	"reflect"
	"testing"
)

// TestConsumeInteger checks <integer> parsing against Blink's
// ConsumeInteger (core/css/properties/css_parsing_utils.cc:1154-1198) and
// CSSMathFunctionValue::ClampToPermittedRange
// (core/css/css_math_function_value.cc:190-204) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b: an integer literal no smaller
// than the minimum, or a calc() of category number, rounded half toward
// +infinity and clamped to the minimum (LOU-863).
func TestConsumeInteger(t *testing.T) {
	noMin := math.Inf(-1)
	tests := []struct {
		tok    string
		min    float64
		want   int
		wantOK bool
	}{
		{"8", noMin, 8, true},
		{"-3", noMin, -3, true},
		{"calc(3 + 5)", noMin, 8, true},
		{"calc(2.5)", noMin, 3, true},
		{"calc(-2.5)", noMin, -2, true},
		{"calc(-1)", 0, 0, true},
		{"-1", 0, 0, false},
		{"5.0", noMin, 0, false},
		{"calc(3px)", noMin, 0, false},
		{"calc(50%)", noMin, 0, false},
		{"a", noMin, 0, false},
	}
	for _, tt := range tests {
		got, ok := consumeInteger(tt.tok, tt.min)
		if ok != tt.wantOK || (ok && got != tt.want) {
			t.Errorf("consumeInteger(%q, %v) = %d, %v; want %d, %v", tt.tok, tt.min, got, ok, tt.want, tt.wantOK)
		}
	}
}

// TestCounterValuesTakeCalc checks counter-reset / counter-increment
// integers go through ConsumeInteger (ConsumeCounters,
// css_parsing_utils.cc:6159), so calc() is an integer, not a counter name.
func TestCounterValuesTakeCalc(t *testing.T) {
	got := parseCounterPropertyList("a calc(3 + 5) b calc(2 * 2)", 0)
	want := []parsedCounterProperty{
		{name: "a", value: 8, hasExplicitValue: true},
		{name: "b", value: 4, hasExplicitValue: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestAdditiveSymbols checks additive-symbols against Blink's
// ConsumeCounterStyleAdditiveSymbols
// (core/css/parser/at_rule_counter_style_descriptor_parser.cc:230-279):
// [ <integer [0,∞]> && <symbol> ]#, in strictly descending weight, else the
// descriptor is invalid.
func TestAdditiveSymbols(t *testing.T) {
	tests := []struct {
		val  string
		want []AdditiveSymbol
	}{
		{`calc(-1) "X"`, []AdditiveSymbol{{0, "X"}}},
		{`5 "V", "I" 1`, []AdditiveSymbol{{5, "V"}, {1, "I"}}},
		{`calc(2 + 3) "V", 1 "I"`, []AdditiveSymbol{{5, "V"}, {1, "I"}}},
		{`1 "I", 5 "V"`, nil},
		{`-1 "X"`, nil},
		{`5 "V" "W"`, nil},
	}
	for _, tt := range tests {
		if got := parseAdditiveSymbols(tt.val); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseAdditiveSymbols(%q) = %+v, want %+v", tt.val, got, tt.want)
		}
	}
}
