package css

import "testing"

// TestMediaQuerySignFunction is the Phase-0 RED specification for LOU-227:
// WPT mediaqueries/mq-calc-sign-function-001..005. Each target wraps a calc()
// containing sign() inside a media feature, exercised either through the
// range syntax (`width > calc(...)`, `aspect-ratio > <ratio>`) or the
// colon form (`grid: calc(...)`), at the WPT default 800x600 viewport with
// the initial 16px root font size (1rem = 16px in MQ context per CSS Values 4
// §10 / Media Queries 4 §1.3).
//
// All cases fail on current code:
//   - 001-004 use range syntax, which parseFeatureNode does not parse → the
//     whole `width > calc(...)` becomes a bare unknown Feature → Unknown.
//   - 005 reaches the grid arm but the value is the raw calc() string, never
//     evaluated → grid != "0" → False; the test wants True.
//
// Mirrors Blink's MediaQueryExpComparison range parsing
// (CSSMathExpressionNode::ParseMathFunction kSign arm) in
// third_party/blink/renderer/core/css/css_math_expression_node.cc and
// media_query_parser.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// sign(0)=0, sign(>0)=+1, sign(<0)=-1 (CSS Values 4 §10.7.3 sign()).
func TestMediaQuerySignFunction(t *testing.T) {
	const vw, vh = 800.0, 600.0

	cases := []struct {
		name string
		mq   string
		want MediaQueryResult
	}{
		// 001: width > calc(1px * (1 + sign(16px - 1rem)))
		// sign(16-16)=0 → 1px*(1+0)=1px → 800 > 1 → True.
		{
			name: "001 width > calc with sign(0) → True",
			mq:   "@media (width > calc(1px * (1 + sign(16px - 1rem))))",
			want: MediaQueryTrue,
		},
		// 002: width > calc(-1px * sign(15px - 1rem))
		// sign(15-16)=-1 → -1px*-1=1px → 800 > 1 → True.
		{
			name: "002 width > calc with sign(<0) → True",
			mq:   "@media (width > calc(-1px * sign(15px - 1rem)))",
			want: MediaQueryTrue,
		},
		// 003: screen and (aspect-ratio > calc(sign(17px-1rem)*59) / calc(79*sign(17px-1rem)))
		// sign(17-16)=+1 → 59/79. 800/600 = 4/3 ≈ 1.333 > 0.747 → True.
		{
			name: "003 aspect-ratio > calc-ratio with sign(>0) → True",
			mq:   "@media screen and (aspect-ratio > calc(sign(17px - 1rem) * 59) / calc(79 * sign(17px - 1rem)))",
			want: MediaQueryTrue,
		},
		// 004: resolution > calc(-1dppx * sign(17px - 1rem))
		// sign(17-16)=+1 → -1dppx*1 = -1dppx. Our resolution (96dpi = 1dppx)
		// > -1dppx → True.
		{
			name: "004 resolution > calc(negative dppx) → True",
			mq:   "@media (resolution > calc(-1dppx * sign(17px - 1rem)))",
			want: MediaQueryTrue,
		},
		// 005: grid: calc(2 * sign(16px - 1rem))
		// sign(16-16)=0 → 2*0=0 → grid: 0 → True (bitmap display has 0 grid bits).
		{
			name: "005 grid: calc evaluating to 0 → True",
			mq:   "@media (grid: calc(2 * sign(16px - 1rem)))",
			want: MediaQueryTrue,
		},
		// 006 (already passing — regression guard): grid: calc(2 * sign(17px-1rem))
		// sign(17-16)=+1 → 2*1=2 → grid: 2 → False (must NOT match; test wants
		// the green default preserved).
		{
			name: "006 grid: calc evaluating to non-zero → False",
			mq:   "@media (grid: calc(2 * sign(17px - 1rem)))",
			want: MediaQueryFalse,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mq := parseMediaQuery(c.mq)
			got := EvaluateMediaQuery(mq, vw, vh)
			if got != c.want {
				t.Fatalf("EvaluateMediaQuery(%q) = %v, want %v",
					c.mq, got, c.want)
			}
		})
	}
}

// TestEvalMathFunctionSign is the Phase-0 RED unit spec for the sign() and
// abs() math primitives that LOU-227 adds to the calc() atom evaluator.
// Mirrors Blink's CSSMathExpressionNode::ParseMathFunction kSign/kAbs arms
// and CSSMathExpressionOperation::Evaluate in
// third_party/blink/renderer/core/css/css_math_expression_node.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
func TestEvalMathFunctionSign(t *testing.T) {
	ctx := calcContext{fontSize: 16, chScale: 0.5}

	cases := []struct {
		expr string
		want float64
	}{
		{"calc(sign(5px))", 1},
		{"calc(sign(-5px))", -1},
		{"calc(sign(0px))", 0},
		{"calc(sign(16px - 1rem))", 0},  // 16 - 16 = 0
		{"calc(sign(17px - 1rem))", 1},  // 17 - 16 > 0
		{"calc(sign(15px - 1rem))", -1}, // 15 - 16 < 0
		{"calc(abs(-5px))", 5},
		{"calc(abs(5px))", 5},
		{"calc(2 * sign(16px - 1rem))", 0},
		{"calc(-1px * sign(15px - 1rem))", 1},
		{"calc(1px * (1 + sign(16px - 1rem)))", 1},
	}

	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			got, ok := EvalMathFunction(c.expr, ctx)
			if !ok {
				t.Fatalf("EvalMathFunction(%q) returned ok=false, want %v", c.expr, c.want)
			}
			if got != c.want {
				t.Fatalf("EvalMathFunction(%q) = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}
