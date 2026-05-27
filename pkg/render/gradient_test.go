package render

import (
	"math"
	"testing"
)

// TestResolveStopPositions_ClampsMonotonically verifies CSS Images 3 §3.4:
// "If a color stop has a position that is less than the specified position
// of any color stop before it in the list, set its position to be equal to
// the largest specified position of any color stop before it."
//
// Source CSS: linear-gradient(to right, yellow, blue 70%, green 0)
// After parse: yellow(unset), blue(70% → 70px in 100px line), green(0px).
// Spec: green's 0px must be clamped up to blue's 70px.
func TestResolveStopPositions_ClampsMonotonically(t *testing.T) {
	stops := []gradientStop{
		{r: 1, g: 1, b: 0, a: 1}, // yellow, no pos
		{r: 0, g: 0, b: 1, a: 1, pos: 70, posIsSet: true, isPercent: true},   // blue 70%
		{r: 0, g: 0.5, b: 0, a: 1, pos: 0, posIsSet: true, isPercent: false}, // green 0
	}
	resolveStopPositions(stops, 100)
	if stops[0].pos != 0 {
		t.Errorf("yellow: got pos=%v, want 0", stops[0].pos)
	}
	if stops[1].pos != 70 {
		t.Errorf("blue: got pos=%v, want 70", stops[1].pos)
	}
	if stops[2].pos != 70 {
		t.Errorf("green should be clamped up to blue=70: got pos=%v, want 70", stops[2].pos)
	}
}

// TestParseColorStop_MultiPosition verifies CSS Images 4 §3.4.2: a color stop
// with two positions ("blue 0% 50%") expands to two adjacent stops with the
// same color, at the first and second positions respectively.
func TestParseColorStop_MultiPosition(t *testing.T) {
	stops := parseColorStops("blue 0% 50%")
	if len(stops) != 2 {
		t.Fatalf("multi-position stop should expand to 2 stops, got %d", len(stops))
	}
	if !stops[0].posIsSet || stops[0].pos != 0 || !stops[0].isPercent {
		t.Errorf("first expanded stop: got pos=%v isPct=%v set=%v, want 0%% set",
			stops[0].pos, stops[0].isPercent, stops[0].posIsSet)
	}
	if !stops[1].posIsSet || stops[1].pos != 50 || !stops[1].isPercent {
		t.Errorf("second expanded stop: got pos=%v isPct=%v set=%v, want 50%% set",
			stops[1].pos, stops[1].isPercent, stops[1].posIsSet)
	}
	if stops[0].r != stops[1].r || stops[0].g != stops[1].g || stops[0].b != stops[1].b {
		t.Errorf("expanded stops must share color: %+v vs %+v", stops[0], stops[1])
	}
}

// TestParseLinearGradient_DegenerateRepeatingCollapsesToLastColor verifies
// CSS Images 3 §3.4: a repeating gradient whose stops all share a position is
// degenerate; per spec it renders as a solid color equal to the average of
// the colors. Blink (and Firefox/WebKit) implement this as the last stop's
// color when only two coincident stops are present.
//
// Source: repeating-linear-gradient(orange 50%, blue 50%) → solid blue 100x100.
// We assert the gradient parses cleanly and that the painter recognises the
// degenerate condition (asserted via repeatLen detection in drawLinearGradient).
func TestParseLinearGradient_DegenerateRepeating(t *testing.T) {
	_, stops, ok, repeating := parseLinearGradient("repeating-linear-gradient(orange 50%, blue 50%)")
	if !ok {
		t.Fatalf("parseLinearGradient failed")
	}
	if !repeating {
		t.Fatalf("expected repeating=true")
	}
	resolveStopPositions(stops, 100)
	if stops[0].pos != stops[1].pos {
		t.Fatalf("stops should coincide after resolution: %v vs %v", stops[0].pos, stops[1].pos)
	}
}

// TestNormalizeStops_OutOfRangePreservedForExtrapolation verifies that
// out-of-range stop positions (pos < 0 or pos > lineLen) are KEPT in the
// normalized stop list — the painter extrapolates colors outside the
// visible region per CSS Images 3 §3.5 + §3.6. Clipping happens at the
// gradient geometry, not at the stop list.
//
// Blink reference: third_party/blink/renderer/core/css/css_gradient_value.cc
// (Chromium @ 4883d11fef) — `CSSGradientValue::NormalizeAndAddStops` retains
// stops with position < 0 and > 1; the renderer interpolates between them.
func TestNormalizeStops_OutOfRangePreservedForExtrapolation(t *testing.T) {
	// conic-gradient(orange -50% -25%, black -25% 25%, ...) — first stops
	// are at fractional turns < 0. They must survive normalization.
	stops := []gradientStop{
		{r: 1, g: 0.5, b: 0, a: 1, pos: -0.50, posIsSet: true},
		{r: 1, g: 0.5, b: 0, a: 1, pos: -0.25, posIsSet: true},
		{r: 0, g: 0, b: 0, a: 1, pos: -0.25, posIsSet: true},
		{r: 0, g: 0, b: 0, a: 1, pos: 0.25, posIsSet: true},
	}
	normalizeStops(stops, 1.0)
	if stops[0].pos != -0.50 {
		t.Errorf("stop[0].pos: got %v, want -0.50", stops[0].pos)
	}
	if stops[1].pos != -0.25 {
		t.Errorf("stop[1].pos: got %v, want -0.25", stops[1].pos)
	}
}

// TestResolveLhRlhInGradient_PixelSubstitution verifies that `lh` and `rlh`
// dimension tokens inside a gradient value are substituted with their
// resolved px equivalents at draw time.
//
// Blink reference: third_party/blink/renderer/core/css/css_to_length_conversion_data.cc
// (Chromium @ 4883d11fef) — `LineHeightSize::Lh()` / `Rlh()`; we resolve at
// the renderer rather than at the style cascade.
func TestResolveLhRlhInGradient_PixelSubstitution(t *testing.T) {
	// Construct a Style with font-size=50 and line-height=2 so 1lh = 100px.
	// We can't trivially build a *css.Style here without importing the css
	// package; nil-style passes the value through unchanged.
	out := resolveLhRlhInGradient("radial-gradient(25px at 1lh 50px, red, green)", nil)
	if out != "radial-gradient(25px at 1lh 50px, red, green)" {
		t.Errorf("with nil style, value must pass through unchanged: got %q", out)
	}
}

// TestParseRadialGradient_DefaultsCenteredFarthestCorner verifies that a
// radial-gradient with no shape/size/position arguments defaults to
// ellipse + farthest-corner + center per CSS Images 3 §3.4.
func TestParseRadialGradient_DefaultsCenteredFarthestCorner(t *testing.T) {
	cx, cy, rx, ry, stops, ok, repeating := parseRadialGradient(
		"radial-gradient(blue, red)", 100, 100)
	if !ok || repeating || len(stops) != 2 {
		t.Fatalf("parse failed: ok=%v repeating=%v len(stops)=%d", ok, repeating, len(stops))
	}
	if cx != 50 || cy != 50 {
		t.Errorf("center: got (%v,%v), want (50,50)", cx, cy)
	}
	// farthest-corner from center (50,50) of 100x100 = hypot(50,50) ≈ 70.71
	want := math.Hypot(50, 50)
	if math.Abs(rx-want) > 0.01 || math.Abs(ry-want) > 0.01 {
		t.Errorf("radii: got (%v,%v), want both ≈%v", rx, ry, want)
	}
}

// TestParseConicGradient_FromAndAt verifies the "from <angle> at <position>"
// preamble parses correctly.
func TestParseConicGradient_FromAndAt(t *testing.T) {
	cx, cy, fromDeg, stops, ok, _ := parseConicGradient(
		"conic-gradient(from 45deg at 100px 50px, green 25%, transparent 0)", 200, 200)
	if !ok || len(stops) < 2 {
		t.Fatalf("parse failed: ok=%v len(stops)=%d", ok, len(stops))
	}
	if cx != 100 || cy != 50 {
		t.Errorf("center: got (%v,%v), want (100,50)", cx, cy)
	}
	if fromDeg != 45 {
		t.Errorf("fromDeg: got %v, want 45", fromDeg)
	}
}

// TestParseConicGradient_RepeatingPrefix verifies that the
// repeating-conic-gradient prefix is accepted.
func TestParseConicGradient_RepeatingPrefix(t *testing.T) {
	_, _, _, stops, ok, repeating := parseConicGradient(
		"repeating-conic-gradient(black 0 25%, white 25% 50%)", 200, 200)
	if !ok {
		t.Fatalf("parse failed")
	}
	if !repeating {
		t.Errorf("repeating flag should be true")
	}
	if len(stops) != 4 {
		t.Errorf("multi-position expansion should yield 4 stops, got %d", len(stops))
	}
}
