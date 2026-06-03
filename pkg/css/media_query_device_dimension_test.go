package css

import "testing"

// TestMediaDeviceDimensionEvaluation covers the legacy device-* media features
// (device-width / device-height and their min-/max- variants) required by CSS
// Media Queries 4 §4.1 and WPT mediaqueries/mq-invalid-media-type-006.html.
//
// Per the deprecated device-* features, louis14 approximates the output device
// dimensions with the viewport dimensions (see the comment at the device-* case
// in evaluateMediaFeature). The feature must therefore COMPARE the requested
// length against the (approximated) device dimension, exactly like width/height
// do — not return a blanket True.
//
// Bug: the device-* case returned MediaQueryTrue unconditionally, so
//
//	`@media screen and (min-width: 480px) and (device-width: 768px)`
//
// matched at the 800x600 reftest viewport and painted #invalid2 red, even
// though device-width:768px should be False (800 != 768).
//
// These tests are Phase-0 RED: they fail on the blanket-True code and turn
// green once device-* features compare against the viewport approximation.
//
// Mirrors Blink's MediaQueryEvaluator::EvalDeviceWidth / EvalDeviceHeight in
// third_party/blink/renderer/core/css/media_query_evaluator.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which compares the requested length
// against the actual device dimension rather than always matching.
func TestMediaDeviceDimensionEvaluation(t *testing.T) {
	const vw, vh = 800.0, 600.0

	cases := []struct {
		name string
		mq   string
		want MediaQueryResult
	}{
		// The exact failing query from mq-invalid-media-type-006 (#invalid2).
		// device-width:768px against an 800px viewport → 800 != 768 → False,
		// so the whole AND is False and the rule must NOT apply.
		{
			name: "screen and (min-width:480px) and (device-width:768px) → False at 800px",
			mq:   "@media screen and (min-width: 480px) and (device-width: 768px)",
			want: MediaQueryFalse,
		},
		// device-width matching the viewport approximation → True.
		{
			name: "(device-width:800px) → True at 800px",
			mq:   "@media (device-width: 800px)",
			want: MediaQueryTrue,
		},
		// min-device-width below the viewport → True.
		{
			name: "(min-device-width:480px) → True at 800px",
			mq:   "@media (min-device-width: 480px)",
			want: MediaQueryTrue,
		},
		// min-device-width above the viewport → False.
		{
			name: "(min-device-width:1000px) → False at 800px",
			mq:   "@media (min-device-width: 1000px)",
			want: MediaQueryFalse,
		},
		// max-device-height at/above the viewport height → True.
		{
			name: "(max-device-height:600px) → True at 600px",
			mq:   "@media (max-device-height: 600px)",
			want: MediaQueryTrue,
		},
		// device-height not equal to viewport height → False.
		{
			name: "(device-height:768px) → False at 600px",
			mq:   "@media (device-height: 768px)",
			want: MediaQueryFalse,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := parseMediaQueryList(tc.mq[len("@media"):])
			got := EvaluateMediaQueryList(list, vw, vh)
			if got != tc.want {
				t.Errorf("EvaluateMediaQueryList(%q) = %v, want %v", tc.mq, got, tc.want)
			}
		})
	}
}
