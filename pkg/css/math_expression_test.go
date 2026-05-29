package css

import (
	"math"
	"testing"
)

// TestEvalMathFunction_Basic exercises the four math-function heads with
// the simplest valid form of each. These are the falsifiability anchors
// for C114 — a regression in any of them indicates a tokenizer or
// dispatch failure, not a numeric edge case.
func TestEvalMathFunction_Basic(t *testing.T) {
	cases := []struct {
		name string
		expr string
		ctx  calcContext
		want float64
	}{
		{
			name: "calc plain addition",
			expr: "calc(2px + 3px)",
			ctx:  calcContext{fontSize: 16, chScale: 0.5},
			want: 5,
		},
		{
			name: "calc percent against base",
			expr: "calc(50%)",
			ctx:  calcContext{fontSize: 16, percentBase: 200, chScale: 0.5},
			want: 100,
		},
		{
			name: "min picks smaller",
			expr: "min(10px, 20px)",
			ctx:  calcContext{fontSize: 16, chScale: 0.5},
			want: 10,
		},
		{
			name: "max picks larger",
			expr: "max(10px, 20px)",
			ctx:  calcContext{fontSize: 16, chScale: 0.5},
			want: 20,
		},
		{
			name: "clamp prefers preferred",
			expr: "clamp(5px, 15px, 25px)",
			ctx:  calcContext{fontSize: 16, chScale: 0.5},
			want: 15,
		},
		{
			name: "clamp clamps to min",
			expr: "clamp(20px, 5px, 30px)",
			ctx:  calcContext{fontSize: 16, chScale: 0.5},
			want: 20,
		},
		{
			name: "clamp clamps to max",
			expr: "clamp(5px, 40px, 25px)",
			ctx:  calcContext{fontSize: 16, chScale: 0.5},
			want: 25,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := EvalMathFunction(tc.expr, tc.ctx)
			if !ok {
				t.Fatalf("EvalMathFunction(%q) returned !ok", tc.expr)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("EvalMathFunction(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEvalMathFunction_Nested guards the calc-in-calc and calc-in-max
// failure modes — both at 100% pixel diff before C114, both diagnosed
// as "outer evaluator never recurses into the inner function-call head".
func TestEvalMathFunction_Nested(t *testing.T) {
	ctx := calcContext{
		fontSize:       16,
		percentBase:    200,
		viewportWidth:  800,
		viewportHeight: 600,
		chScale:        0.5,
	}
	cases := []struct {
		name string
		expr string
		want float64
	}{
		{"calc-in-calc 100%", "calc(calc(100%))", 200},
		{"calc-in-max single arg", "max(calc(100%))", 200},
		{"calc-in-min single arg", "min(calc(50%))", 100},
		{"min-in-calc", "calc(min(50px, 100px) + 10px)", 60},
		{"max-in-calc", "calc(max(50px, 100px) + 10px)", 110},
		{"clamp-in-calc", "calc(clamp(0px, 50px, 100px) + 10px)", 60},
		{"calc with vw + percent", "calc(100vw + 50%)", 900}, // 800 + 100
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := EvalMathFunction(tc.expr, ctx)
			if !ok {
				t.Fatalf("EvalMathFunction(%q) returned !ok", tc.expr)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("EvalMathFunction(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEvalMathFunction_UnitlessZeroInvalid asserts the §10.10 type rule
// from css-values/max-unitless-zero-invalid: a unitless 0 cannot be mixed
// with a length-percentage argument.
//
// Spec: https://drafts.csswg.org/css-values/#calc-type-checking
//
// `min(100%)` is valid (single LP arg). `min(0, 100%)` is invalid (number
// vs LP) and must return !ok so the declaration is dropped at parse time.
func TestEvalMathFunction_UnitlessZeroInvalid(t *testing.T) {
	ctx := calcContext{
		fontSize:    16,
		percentBase: 100,
		chScale:     0.5,
	}
	if v, ok := EvalMathFunction("min(100%)", ctx); !ok || v != 100 {
		t.Fatalf("min(100%%) should resolve to 100, got (%v, %v)", v, ok)
	}
	if _, ok := EvalMathFunction("min(0, 100%)", ctx); ok {
		t.Fatal("min(0, 100%%) should be invalid (unitless 0 mixed with LP)")
	}
	if _, ok := EvalMathFunction("max(0, 100%)", ctx); ok {
		t.Fatal("max(0, 100%%) should be invalid (unitless 0 mixed with LP)")
	}
	if _, ok := EvalMathFunction("max(0, 100px)", ctx); ok {
		t.Fatal("max(0, 100px) should be invalid (unitless 0 mixed with length)")
	}
}

// TestEvalMathFunction_PercentPropagation is the C114 fix's core
// regression guard: when min/max/clamp wrap a percent term, the percent
// base from the layout layer must thread through. Previously
// `parseMathArg` always resolved % against viewport-width, ignoring the
// caller's percentBase.
func TestEvalMathFunction_PercentPropagation(t *testing.T) {
	ctx := calcContext{
		fontSize:      16,
		percentBase:   400,
		viewportWidth: 800, // intentionally different from percentBase
		chScale:       0.5,
	}
	// max(50%) — should resolve against percentBase (400 × 0.5 = 200),
	// not viewportWidth (800 × 0.5 = 400).
	v, ok := EvalMathFunction("max(50%)", ctx)
	if !ok {
		t.Fatal("max(50%%) returned !ok")
	}
	if math.Abs(v-200) > 1e-9 {
		t.Fatalf("max(50%%) with percentBase=400 = %v, want 200 (viewportWidth would give 400)", v)
	}
	// clamp(10px, 50%, 300px) — preferred 200 lies between 10 and 300.
	v, ok = EvalMathFunction("clamp(10px, 50%, 300px)", ctx)
	if !ok {
		t.Fatal("clamp(10px, 50%%, 300px) returned !ok")
	}
	if math.Abs(v-200) > 1e-9 {
		t.Fatalf("clamp(10px, 50%%, 300px) with percentBase=400 = %v, want 200", v)
	}
}

// TestEvalMathFunction_CalcWithPercentLayoutCase is the calc-min-height-001 /
// calc-max-width-001 family regression guard: `calc(50% - 3px)` must resolve
// the % against the layout-supplied percent base, not 0.
func TestEvalMathFunction_CalcWithPercentLayoutCase(t *testing.T) {
	ctx := calcContext{
		fontSize:    16,
		percentBase: 200,
		chScale:     0.5,
	}
	v, ok := EvalMathFunction("calc(50% - 3px)", ctx)
	if !ok {
		t.Fatal("calc(50%% - 3px) returned !ok")
	}
	if math.Abs(v-97) > 1e-9 {
		t.Fatalf("calc(50%% - 3px) with percentBase=200 = %v, want 97", v)
	}
}

// TestIsMathFunction guards the head detection — the cheap test that
// callers like fragment_geometry's ResolveMin/MaxInlineSize use to decide
// whether to thread a percent base.
func TestIsMathFunction(t *testing.T) {
	yes := []string{
		"calc(100%)",
		"min(10px, 20px)",
		"max(10px)",
		"clamp(1px, 5%, 10px)",
		"calc(calc(100%))",
		" calc(1px + 2px) ", // trimmed
	}
	no := []string{
		"100px",
		"50%",
		"auto",
		"",
		"calc",                  // missing parens
		"calc(",                 // unterminated
		"calculation(100px)",    // not a math head
		"min-content",           // not a function
		"radial-gradient(blah)", // not a math head
	}
	for _, s := range yes {
		if !IsMathFunction(s) {
			t.Errorf("IsMathFunction(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsMathFunction(s) {
			t.Errorf("IsMathFunction(%q) = true, want false", s)
		}
	}
}
