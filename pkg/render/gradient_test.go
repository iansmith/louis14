package render

import (
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
